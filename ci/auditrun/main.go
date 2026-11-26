package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// The variables auditrun reads, and the one it refuses.
const (
	envToken       = "CLAUDE_CODE_OAUTH_TOKEN" //nolint:gosec // G101: the variable's name, not a credential
	envCanary      = "SHIFT2EU_AUDIT_CANARY"
	envGitHubEnv   = "GITHUB_ENV"
	envGitHubPath  = "GITHUB_PATH"
	envGitHubOut   = "GITHUB_OUTPUT"
	envGitHubState = "GITHUB_STATE"
	envStepSummary = "GITHUB_STEP_SUMMARY"
	envAPIKey      = "ANTHROPIC_API_KEY" //nolint:gosec // G101: the variable's name, not a credential
)

// canaryLength is the canary's length: 32 bytes in hex.
const canaryLength = 64

// summaryMode is the mode a step summary the run creates gets.
const summaryMode os.FileMode = 0o600

// resultMode is the mode of the result file -result-out names.
const resultMode os.FileMode = 0o600

// lineAPIKey is the line of a step that holds an API key.
const lineAPIKey = "refused: ANTHROPIC_API_KEY is set; the audit runs on the subscription's OAuth token alone" //nolint:gosec // G101: a sentence naming the variable, not a credential

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	code := runContext(ctx, os.Args[1:], os.LookupEnv, audit.SystemSteps(os.Getpid(), audit.Observe), "/", os.Stdout, os.Stderr)

	stop()
	os.Exit(code)
}

// run is the entry point's logic with the step's arguments, its environment
// as a lookup function, the steps, the root that stands in for / (main's is
// /, a test's a temporary directory) and where its lines go. It is the tests'
// entry, runContext with a background context; main calls runContext itself,
// with the context its interrupt signal cancels.
func run(args []string, getenv func(string) (string, bool), steps audit.Steps, root string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, getenv, steps, root, stdout, stderr)
}

// runContext is run under ctx. The run's Result writes the bytes the audit
// step read and checked to the -result-out file, the one the workflow's
// encrypt step reads: nothing reads the result file in the work area again by
// its path (ADR 0040).
//
// A step that holds ANTHROPIC_API_KEY, set even to nothing, is refused first,
// before the command line and the other inputs are read: it exits 3 with its
// line whatever else is wrong.
func runContext(ctx context.Context, args []string, getenv func(string) (string, bool), steps audit.Steps, root string, stdout, stderr io.Writer) int {
	if _, set := getenv(envAPIKey); set {
		stepSummary, _ := getenv(envStepSummary)

		out, closeSummary := summaryWriter(stdout, stderr, stepSummary)
		defer closeSummary()

		_, _ = fmt.Fprintln(out, lineAPIKey)

		return audit.ExitRefused
	}

	in, opts, err := inputs(args, getenv, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "auditrun:", err)

		return audit.ExitUsage
	}

	summary, closeSummary := openSummary(stderr, in.StepSummary)
	defer closeSummary()

	// The file commands are recorded once the summary exists, before any step
	// runs: auditrun is their only legitimate writer, and it writes only its
	// summary line, through out (issues #615, #614).
	commands := audit.RecordFileCommands(opts.fileCommands)
	defer commands.Close()

	for _, name := range commands.Unopened() {
		_, _ = fmt.Fprintf(stderr, "auditrun: the file command %s is no regular file the run can open; only its path is checked after the steps\n", name)
	}

	out := stdout
	if summary != nil {
		out = io.MultiWriter(stdout, summary, commands.Appended(envStepSummary))
	}

	code := audit.Run{Root: root, Inputs: in, Steps: steps, Out: out, Result: resultWriter(opts.resultOut)}.Run(ctx)

	return repairFileCommands(code, commands, opts.resultOut, stderr)
}

// repairFileCommands empties every file command the steps changed
// (audit.FileCommands.Repair, issues #615, #614): the runner applies
// $GITHUB_ENV and $GITHUB_PATH after a failed step too, and publishes the step
// summary, so nothing a step wrote there may outlive the run. A repair names
// the files it emptied on stderr and fails a passing run with the audit's
// exit, removing the result file that run wrote, so the encrypt step gets
// nothing; a run that already failed keeps its exit code and wrote no result
// file. A repair that fails says the posted summary or the job's environment
// may be hostile, and fails the run.
func repairFileCommands(code int, commands *audit.FileCommands, resultOut string, stderr io.Writer) int {
	emptied, err := commands.Repair()
	if len(emptied) == 0 && err == nil {
		return code
	}

	for _, name := range emptied {
		_, _ = fmt.Fprintf(stderr, "auditrun: the steps changed the file command %s; it is left as an empty file\n", name)
	}

	if err != nil {
		_, _ = fmt.Fprintln(stderr, "auditrun: a file command the steps changed could not be emptied; the posted step summary or the job's environment may be hostile:", err)
	}

	if code == audit.ExitOK {
		// A failed run leaves no result file for the encrypt step.
		_ = os.Remove(resultOut)

		return audit.ExitFailed
	}

	return code
}

// resultWriter is the run's Result: it writes the bytes the audit step read
// and checked, exactly those, to a new file at path, mode 0600. A path that
// already exists is refused, never overwritten (O_EXCL), so the file holds the
// checked bytes and nothing an earlier writer left; the run then fails with
// the audit's line. The path is taken as given, not under the run's root: it
// is the model step's own $RUNNER_TEMP, out of the sandbox's reach.
func resultWriter(path string) func([]byte) error {
	return func(body []byte) error {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, resultMode) //nolint:gosec // G304: the step's own -result-out path under $RUNNER_TEMP
		if err != nil {
			return fmt.Errorf("open the result file: %w", err)
		}

		return writeResult(path, file, body)
	}
}

// writeResult writes body to the result file resultWriter opened at path and
// closes it. A write or a close that fails removes the file and returns the
// error, so a failed run leaves no partial result for an encrypt step to read
// (doc.go).
func writeResult(path string, file io.WriteCloser, body []byte) error {
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return fmt.Errorf("write the result file: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(path)

		return fmt.Errorf("close the result file: %w", err)
	}

	return nil
}

// options is what the run takes besides its Inputs: the -result-out path, and
// the step's file commands, set and checked, which it records before the
// steps and repairs after them.
type options struct {
	resultOut    string
	fileCommands []audit.FileCommandPath
}

// inputs reads the command line and the step's environment, and returns the
// run's inputs and its options. Its errors name what is wrong, never a value
// of the environment's.
func inputs(args []string, getenv func(string) (string, bool), stderr io.Writer) (audit.Inputs, options, error) {
	flags := flag.NewFlagSet("auditrun", flag.ContinueOnError)
	flags.SetOutput(stderr)

	tarball := flags.String("claude-tarball", "", "the Claude Code tarball the install step kept")
	integrity := flags.String("claude-integrity", "", "the tarball's npm integrity, sha512-<base64>, from the one pin")
	resultOut := flags.String("result-out", "", "where the checked result is written, a new file under the step's $RUNNER_TEMP")

	if err := flags.Parse(args); err != nil {
		return audit.Inputs{}, options{}, fmt.Errorf("usage: %w", err)
	}

	if flags.NArg() != 0 {
		return audit.Inputs{}, options{}, fmt.Errorf("usage: auditrun takes no arguments, got %d", flags.NArg())
	}

	if *tarball == "" {
		return audit.Inputs{}, options{}, fmt.Errorf("usage: -claude-tarball is required")
	}

	if _, err := audit.ParseIntegrity(*integrity); err != nil {
		return audit.Inputs{}, options{}, fmt.Errorf("usage: -claude-integrity: %w", err)
	}

	if *resultOut == "" {
		return audit.Inputs{}, options{}, fmt.Errorf("usage: -result-out is required")
	}

	token, _ := getenv(envToken)
	canary, _ := getenv(envCanary)
	gitHubEnv, _ := getenv(envGitHubEnv)
	stepSummary, _ := getenv(envStepSummary)

	switch {
	case token == "":
		return audit.Inputs{}, options{}, fmt.Errorf("%s is not set", envToken)
	case !isCanary(canary):
		return audit.Inputs{}, options{}, fmt.Errorf("%s is not %d lower-case hex characters", envCanary, canaryLength)
	case gitHubEnv == "":
		return audit.Inputs{}, options{}, fmt.Errorf("%s is not set", envGitHubEnv)
	}

	fileCommands, err := checkFileCommands(getenv, gitHubEnv, stepSummary)
	if err != nil {
		return audit.Inputs{}, options{}, err
	}

	return audit.Inputs{
		Token:       token,
		Canary:      canary,
		GitHubEnv:   gitHubEnv,
		StepSummary: stepSummary,
		Tarball:     *tarball,
		Integrity:   *integrity,
	}, options{resultOut: *resultOut, fileCommands: fileCommands}, nil
}

// checkFileCommands refuses a step whose $GITHUB_PATH, $GITHUB_OUTPUT or
// $GITHUB_STATE is unset, or whose file commands - $GITHUB_ENV, those three
// and the step summary, when set - are not all clean absolute paths
// (audit.IsCleanAbsolute) in the $GITHUB_ENV file's own directory
// (audit.FileCommandDir): the audit digests that directory alone, so a file
// command elsewhere would fall outside the digest (issues #615, #614). It reads
// the paths only, never the files' content, and returns the file commands it
// checked, the step summary last when set.
func checkFileCommands(getenv func(string) (string, bool), gitHubEnv, stepSummary string) ([]audit.FileCommandPath, error) {
	if !audit.IsCleanAbsolute(gitHubEnv) {
		return nil, fmt.Errorf("%s is not a clean absolute path", envGitHubEnv)
	}

	dir := audit.FileCommandDir(gitHubEnv)
	commands := []audit.FileCommandPath{{Name: envGitHubEnv, Path: gitHubEnv}}

	for _, name := range []string{envGitHubPath, envGitHubOut, envGitHubState} {
		path, _ := getenv(name)
		if path == "" {
			return nil, fmt.Errorf("%s is not set", name)
		}

		if err := checkFileCommand(name, path, dir); err != nil {
			return nil, err
		}

		commands = append(commands, audit.FileCommandPath{Name: name, Path: path})
	}

	if stepSummary != "" {
		if err := checkFileCommand(envStepSummary, stepSummary, dir); err != nil {
			return nil, err
		}

		commands = append(commands, audit.FileCommandPath{Name: envStepSummary, Path: stepSummary})
	}

	return commands, nil
}

// checkFileCommand refuses the file command name's path unless it is a clean
// absolute path (audit.IsCleanAbsolute, issue #614) in the directory dir.
func checkFileCommand(name, path, dir string) error {
	if !audit.IsCleanAbsolute(path) {
		return fmt.Errorf("%s is not a clean absolute path", name)
	}

	if audit.FileCommandDir(path) != dir {
		return fmt.Errorf("%s is not in the %s file's directory", name, envGitHubEnv)
	}

	return nil
}

// isCanary reports whether s is 64 lower-case hex characters.
func isCanary(s string) bool {
	if len(s) != canaryLength {
		return false
	}

	for i := range len(s) {
		c := s[i]
		if ('0' > c || c > '9') && ('a' > c || c > 'f') {
			return false
		}
	}

	return true
}

// summaryWriter returns stdout and, when the step has a summary file, that
// file too, appended to, and a function that closes it. A summary that cannot
// be opened leaves stdout alone, with a note on stderr.
func summaryWriter(stdout, stderr io.Writer, path string) (io.Writer, func()) {
	file, closeFile := openSummary(stderr, path)
	if file == nil {
		return stdout, closeFile
	}

	return io.MultiWriter(stdout, file), closeFile
}

// openSummary opens the step summary at path for appending, creating it, and
// returns it with a function that closes it; nil when the step has no summary,
// or, with a note on stderr, when it cannot be opened.
func openSummary(stderr io.Writer, path string) (*os.File, func()) {
	if path == "" {
		return nil, func() {}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, summaryMode) //nolint:gosec // G304: the runner's own step-summary path
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "auditrun: the step summary cannot be opened; the run's line is on stdout alone")

		return nil, func() {}
	}

	return file, func() { _ = file.Close() }
}
