package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

func main() {
	// SIGINT cancels the run's context and so every running gh call and
	// gitleaks scan. SIGTERM would need syscall, which no audit package may
	// import; every call and scan is bounded by its own deadline instead (ADR
	// 0040, "Deadlines and signals").
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	code := run(ctx, os.Args[1:], os.LookupEnv, time.Now, os.Stdout, os.Stderr)

	stop()
	os.Exit(code)
}

// secretVariables are the names whose presence alone, even set to nothing,
// refuses the run: none belongs in the publish program's environment (ADR
// 0040).
var secretVariables = []string{
	"AUDIT_APP_PRIVATE_KEY",
	"AUDIT_MODEL_PRIVATE_KEY",
	"AUDIT_PUBLISH_PRIVATE_KEY",
	"CLAUDE_CODE_OAUTH_TOKEN",
}

// usageText is what a command line the run cannot parse writes to stderr: a
// constant, which never holds a value of the command line or the environment.
const usageText = "usage: auditpublish artifacts -job model|publish -gh <path> -tmp <dir>\n" +
	"       auditpublish image -result <path> -gh <path> -tmp <dir>\n" +
	"       auditpublish publish -result <path> -reports <dir> -gh <path> -gitleaks <path> -tmp <dir>\n"

// publishServerURL is the one GITHUB_SERVER_URL the image and publish
// subcommands accept: the report, the commit message and the pull request
// name the run's URL on it, and an attestation's workflow names it.
const publishServerURL = "https://github.com"

// errUsage is a command line the run cannot use: the configuration's outcome,
// with usageText on stderr.
var errUsage = errors.New("usage")

// outcomeFunc maps a subcommand's error to its one line and exit code.
type outcomeFunc func(error) (string, int)

// run is the entry point with its arguments, the step's environment as a
// lookup function, the clock the run's UTC date is read from, and where its
// one line goes; it returns the exit code. A test passes its own lookup and
// clock. It reads exactly the variables the package's documentation names.
//
// It first asks for AUDIT_APP_PRIVATE_KEY, AUDIT_MODEL_PRIVATE_KEY,
// AUDIT_PUBLISH_PRIVATE_KEY and CLAUDE_CODE_OAUTH_TOKEN, and refuses a run in
// whose environment any is set at all (configuration invalid, exit 9) before
// it reads anything else. Then it opens the step summary and runs the one
// subcommand its first argument names: artifacts, image or publish. Its one
// line goes to stdout and is appended to the step summary, whatever the
// outcome (finish).
func run(ctx context.Context, args []string, lookup func(string) (string, bool), now func() time.Time, stdout, stderr io.Writer) int {
	for _, name := range secretVariables {
		if _, set := lookup(name); set {
			return finish(stdout, openSummary(lookup), github.ArtifactsOutcome, publish.ErrConfig)
		}
	}

	summary := openSummary(lookup)

	var (
		outcome outcomeFunc = github.ArtifactsOutcome
		err     error
	)

	switch {
	case len(args) > 0 && args[0] == "artifacts":
		err = artifacts(ctx, args[1:], lookup, now)
	case len(args) > 0 && args[0] == "image":
		outcome = github.ImageOutcome
		err = image(ctx, args[1:], lookup, now)
	case len(args) > 0 && args[0] == "publish":
		var p github.Published

		p, err = publishRun(ctx, args[1:], lookup, now)
		outcome = func(e error) (string, int) {
			o := publish.OutcomeOf(e).WithCounts(p.Public, p.Private)

			return o.Summary(), o.Code()
		}
	default:
		err = errUsage
	}

	if errors.Is(err, errUsage) {
		_, _ = io.WriteString(stderr, usageText)
		err = publish.ErrConfig
	}

	return finish(stdout, summary, outcome, err)
}

// parseFlags parses a subcommand's flags, each of which must be given, and
// refuses (errUsage) an unknown flag, an argument after them and a flag left
// empty.
func parseFlags(name string, args []string, names ...string) (map[string]string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	values := make(map[string]*string, len(names))
	for _, n := range names {
		values[n] = fs.String(n, "", "")
	}

	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return nil, errUsage
	}

	out := make(map[string]string, len(names))

	for _, n := range names {
		if *values[n] == "" {
			return nil, errUsage
		}

		out[n] = *values[n]
	}

	return out, nil
}

// artifacts is the artifacts subcommand: it parses its command line, reads
// the step's environment, and checks the run's artifacts against the job's.
// Its error carries a class alone (errUsage, publish.ErrConfig or one of
// CheckArtifacts').
func artifacts(ctx context.Context, args []string, lookup func(string) (string, bool), now func() time.Time) error {
	fs := flag.NewFlagSet("artifacts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	jobName := fs.String("job", "", "model or publish")
	ghPath := fs.String("gh", "", "gh's absolute path")
	tempDir := fs.String("tmp", "", "an existing absolute temporary directory")

	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errUsage
	}

	var job github.Job

	switch *jobName {
	case "model":
		job = github.JobModel
	case "publish":
		job = github.JobPublish
	default:
		return errUsage
	}

	token, ok := lookup("GH_TOKEN")
	if !ok {
		return publish.ErrConfig
	}

	serverURL, _ := lookup("GITHUB_SERVER_URL")
	repository, _ := lookup("GITHUB_REPOSITORY")
	runID, _ := lookup("GITHUB_RUN_ID")

	date := now().UTC().Format(time.DateOnly)

	if _, err := publish.NewRun(serverURL, repository, runID, date); err != nil {
		return publish.ErrConfig
	}

	// The artifact check reads one listing: its client is read-only by
	// construction and writes nothing.
	client, err := github.NewArtifactsClient(github.ClientConfig{
		GHPath:  *ghPath,
		Token:   github.NewToken(token),
		TempDir: *tempDir,
		Scope:   github.Scope{Repository: repository, RunID: runID, Date: date},
	})
	if err != nil {
		return err
	}

	checkErr := github.CheckArtifacts(ctx, client, job)

	return errors.Join(checkErr, client.Close())
}

// reportEnv is what the image and publish subcommands read of the step's
// environment besides the step summary.
type reportEnv struct {
	token  github.Token
	run    publish.Run
	commit string
}

// readReportEnv reads GH_TOKEN, GITHUB_SERVER_URL, GITHUB_REPOSITORY,
// GITHUB_RUN_ID and GITHUB_SHA, and refuses (publish.ErrConfig) a missing
// token, a server URL other than exactly https://github.com, a run NewRun
// refuses and a GITHUB_SHA that is not 40 lowercase hex.
func readReportEnv(lookup func(string) (string, bool), now func() time.Time) (reportEnv, error) {
	token, ok := lookup("GH_TOKEN")
	serverURL, _ := lookup("GITHUB_SERVER_URL")
	repository, _ := lookup("GITHUB_REPOSITORY")
	runID, _ := lookup("GITHUB_RUN_ID")
	commit, _ := lookup("GITHUB_SHA")

	if !ok || serverURL != publishServerURL || !isCommit(commit) {
		return reportEnv{}, publish.ErrConfig
	}

	r, err := publish.NewRun(serverURL, repository, runID, now().UTC().Format(time.DateOnly))
	if err != nil {
		return reportEnv{}, publish.ErrConfig
	}

	return reportEnv{token: github.NewToken(token), run: r, commit: commit}, nil
}

// isCommit reports whether s is 40 lowercase hex.
func isCommit(s string) bool {
	if len(s) != 40 {
		return false
	}

	for i := range len(s) {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}

// scopeOf is the GitHub side's scope of a report subcommand's run.
func (e reportEnv) scopeOf() github.Scope {
	return github.Scope{Repository: e.run.Repository, RunID: e.run.ID, Date: e.run.Date, Commit: e.commit}
}

// image is the image subcommand: it reads the result and checks, on a
// read-only client with the job's token, that the result's tag is a release
// and its image digest has the release workflow's provenance attestation
// (PRD #175, maintainer decision 2). Its error carries a class alone.
func image(ctx context.Context, args []string, lookup func(string) (string, bool), now func() time.Time) error {
	flags, err := parseFlags("image", args, "result", "gh", "tmp")
	if err != nil {
		return err
	}

	env, err := readReportEnv(lookup, now)
	if err != nil {
		return err
	}

	result, err := github.LoadResult(flags["result"], env.commit)
	if err != nil {
		return err
	}

	client, err := github.NewImageClient(github.ClientConfig{
		GHPath:  flags["gh"],
		Token:   env.token,
		TempDir: flags["tmp"],
		Scope:   env.scopeOf(),
	})
	if err != nil {
		return err
	}

	checkErr := github.CheckRelease(ctx, client, result.ReleaseTag)
	if checkErr == nil {
		checkErr = github.CheckImageDigest(ctx, client, result.ReleaseTag, result.ImageDigest)
	}

	return errors.Join(checkErr, client.Close())
}

// publishRun is the publish subcommand: it reads its flags and the step's
// environment, AUDIT_FINGERPRINT_KEY included, builds the publish client and
// the gitleaks scanner, and runs github.Publish. Its error carries a class
// alone.
func publishRun(ctx context.Context, args []string, lookup func(string) (string, bool), now func() time.Time) (github.Published, error) {
	flags, err := parseFlags("publish", args, "result", "reports", "gh", "gitleaks", "tmp")
	if err != nil {
		return github.Published{}, err
	}

	env, err := readReportEnv(lookup, now)
	if err != nil {
		return github.Published{}, err
	}

	keyText, _ := lookup("AUDIT_FINGERPRINT_KEY")

	key, err := publish.NewFingerprintKey([]byte(keyText))
	if err != nil {
		return github.Published{}, publish.ErrConfig
	}

	client, err := github.NewPublishClient(github.ClientConfig{
		GHPath:  flags["gh"],
		Token:   env.token,
		TempDir: flags["tmp"],
		Scope:   env.scopeOf(),
	})
	if err != nil {
		return github.Published{}, err
	}

	scanner, err := github.NewGitleaks(github.GitleaksConfig{Path: flags["gitleaks"], TempDir: flags["tmp"]})
	if err != nil {
		return github.Published{}, errors.Join(err, client.Close())
	}

	published, publishErr := github.Publish(ctx, github.PublishConfig{
		ResultPath: flags["result"],
		ReportsDir: flags["reports"],
		Run:        env.run,
		Key:        key,
		Client:     client,
		Gitleaks:   scanner,
	})

	if err := errors.Join(publishErr, scanner.Close(), client.Close()); err != nil {
		return github.Published{}, err
	}

	return published, nil
}

// openSummary opens GITHUB_STEP_SUMMARY for appending, and returns nil when
// it is unset, not an absolute path or cannot be opened: the runner makes the
// file, so the run never creates one.
func openSummary(lookup func(string) (string, bool)) *os.File {
	path, ok := lookup("GITHUB_STEP_SUMMARY")
	if !ok || path == "" || path[0] != '/' {
		return nil
	}

	//nolint:gosec // G304: the runner's step summary, an absolute path; opened to append the run's one line, never read
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil
	}

	return f
}

// finish writes the one line of the run's error (outcome) to stdout and
// appends it to the step summary, and returns its code. A summary that could
// not be opened, written or closed fails the run closed: a run that would
// have passed prints the configuration's line instead and exits 9
// (configuration invalid), and a run that already failed keeps its own line
// and code. Either way the line on stdout is the run's only one.
func finish(stdout io.Writer, summary *os.File, outcome outcomeFunc, err error) int {
	line, code := outcome(err)

	if summary == nil {
		if code == 0 {
			line, code = outcome(publish.ErrConfig)
		}

		_, _ = io.WriteString(stdout, line+"\n")

		return code
	}

	_, writeErr := io.WriteString(summary, line+"\n")
	closeErr := summary.Close()

	if (writeErr != nil || closeErr != nil) && code == 0 {
		line, code = outcome(publish.ErrConfig)
	}

	_, _ = io.WriteString(stdout, line+"\n")

	return code
}
