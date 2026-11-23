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
const usageText = "usage: auditpublish artifacts -job model|publish -gh <path> -tmp <dir>\n"

// errUsage is a command line the run cannot use: the configuration's outcome,
// with usageText on stderr.
var errUsage = errors.New("usage")

// run is the entry point with its arguments, the step's environment as a
// lookup function, the clock the run's UTC date is read from, and where its
// one line goes; it returns the exit code. A test passes its own lookup and
// clock. It reads exactly the variables the package's documentation names.
//
// It first asks for AUDIT_APP_PRIVATE_KEY, AUDIT_MODEL_PRIVATE_KEY,
// AUDIT_PUBLISH_PRIVATE_KEY and CLAUDE_CODE_OAUTH_TOKEN, and refuses a run in
// whose environment any is set at all (configuration invalid, exit 9) before
// it reads anything else. Then it opens the step summary and runs the one
// subcommand, artifacts (PRD #175 milestone 2, PR 4a; publish arrives with PR
// 4b and is a usage error until then). Its one line goes to stdout and is
// appended to the step summary, whatever the outcome (finish).
func run(ctx context.Context, args []string, lookup func(string) (string, bool), now func() time.Time, stdout, stderr io.Writer) int {
	for _, name := range secretVariables {
		if _, set := lookup(name); set {
			return finish(stdout, openSummary(lookup), publish.ErrConfig)
		}
	}

	summary := openSummary(lookup)

	err := artifacts(ctx, args, lookup, now)
	if errors.Is(err, errUsage) {
		_, _ = io.WriteString(stderr, usageText)
		err = publish.ErrConfig
	}

	return finish(stdout, summary, err)
}

// artifacts is the artifacts subcommand: it parses its command line, reads
// the step's environment, and checks the run's artifacts against the job's.
// Its error carries a class alone (errUsage, publish.ErrConfig or one of
// CheckArtifacts').
func artifacts(ctx context.Context, args []string, lookup func(string) (string, bool), now func() time.Time) error {
	if len(args) == 0 || args[0] != "artifacts" {
		return errUsage
	}

	fs := flag.NewFlagSet("artifacts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	jobName := fs.String("job", "", "model or publish")
	ghPath := fs.String("gh", "", "gh's absolute path")
	tempDir := fs.String("tmp", "", "an existing absolute temporary directory")

	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
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

	client, err := github.NewClient(github.ClientConfig{
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

// finish writes the one line of the run's error (github.ArtifactsOutcome) to
// stdout and appends it to the step summary, and returns its code. A summary
// that could not be opened, written or closed fails the run closed: a run that would have passed prints the
// configuration's line instead and exits 9 (configuration invalid), and a run
// that already failed keeps its own line and code. Either way the line on
// stdout is the run's only one.
func finish(stdout io.Writer, summary *os.File, err error) int {
	line, code := github.ArtifactsOutcome(err)

	if summary == nil {
		if code == 0 {
			line, code = github.ArtifactsOutcome(publish.ErrConfig)
		}

		_, _ = io.WriteString(stdout, line+"\n")

		return code
	}

	_, writeErr := io.WriteString(summary, line+"\n")
	closeErr := summary.Close()

	if (writeErr != nil || closeErr != nil) && code == 0 {
		line, code = github.ArtifactsOutcome(publish.ErrConfig)
	}

	_, _ = io.WriteString(stdout, line+"\n")

	return code
}
