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

	in, resultOut, err := inputs(args, getenv, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "auditrun:", err)

		return audit.ExitUsage
	}

	out, closeSummary := summaryWriter(stdout, stderr, in.StepSummary)
	defer closeSummary()

	return audit.Run{Root: root, Inputs: in, Steps: steps, Out: out, Result: resultWriter(resultOut)}.Run(ctx)
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

		if _, err := file.Write(body); err != nil {
			_ = file.Close()

			return fmt.Errorf("write the result file: %w", err)
		}

		if err := file.Close(); err != nil {
			return fmt.Errorf("close the result file: %w", err)
		}

		return nil
	}
}

// inputs reads the command line and the step's environment, and returns the
// run's inputs and the -result-out path. Its errors name what is wrong, never
// a value of the environment's.
func inputs(args []string, getenv func(string) (string, bool), stderr io.Writer) (audit.Inputs, string, error) {
	flags := flag.NewFlagSet("auditrun", flag.ContinueOnError)
	flags.SetOutput(stderr)

	tarball := flags.String("claude-tarball", "", "the Claude Code tarball the install step kept")
	integrity := flags.String("claude-integrity", "", "the tarball's npm integrity, sha512-<base64>, from the one pin")
	resultOut := flags.String("result-out", "", "where the checked result is written, a new file under the step's $RUNNER_TEMP")

	if err := flags.Parse(args); err != nil {
		return audit.Inputs{}, "", fmt.Errorf("usage: %w", err)
	}

	if flags.NArg() != 0 {
		return audit.Inputs{}, "", fmt.Errorf("usage: auditrun takes no arguments, got %d", flags.NArg())
	}

	if *tarball == "" {
		return audit.Inputs{}, "", fmt.Errorf("usage: -claude-tarball is required")
	}

	if _, err := audit.ParseIntegrity(*integrity); err != nil {
		return audit.Inputs{}, "", fmt.Errorf("usage: -claude-integrity: %w", err)
	}

	if *resultOut == "" {
		return audit.Inputs{}, "", fmt.Errorf("usage: -result-out is required")
	}

	token, _ := getenv(envToken)
	canary, _ := getenv(envCanary)
	gitHubEnv, _ := getenv(envGitHubEnv)
	stepSummary, _ := getenv(envStepSummary)

	switch {
	case token == "":
		return audit.Inputs{}, "", fmt.Errorf("%s is not set", envToken)
	case !isCanary(canary):
		return audit.Inputs{}, "", fmt.Errorf("%s is not %d lower-case hex characters", envCanary, canaryLength)
	case gitHubEnv == "":
		return audit.Inputs{}, "", fmt.Errorf("%s is not set", envGitHubEnv)
	}

	return audit.Inputs{
		Token:       token,
		Canary:      canary,
		GitHubEnv:   gitHubEnv,
		StepSummary: stepSummary,
		Tarball:     *tarball,
		Integrity:   *integrity,
	}, *resultOut, nil
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
	if path == "" {
		return stdout, func() {}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, summaryMode) //nolint:gosec // G304: the runner's own step-summary path
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "auditrun: the step summary cannot be opened; the run's line is on stdout alone")

		return stdout, func() {}
	}

	return io.MultiWriter(stdout, file), func() { _ = file.Close() }
}
