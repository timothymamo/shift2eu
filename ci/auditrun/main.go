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

// runContext is run under ctx. It leaves the run's Result nil: the run checks
// the result file and keeps its bytes, but nothing publishes them yet - the
// publication job of ADR 0040's step 19, PR 5 of PRD #175 milestone 2, sets
// Result.
func runContext(ctx context.Context, args []string, getenv func(string) (string, bool), steps audit.Steps, root string, stdout, stderr io.Writer) int {
	in, err := inputs(args, getenv, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "auditrun:", err)

		return audit.ExitUsage
	}

	out, closeSummary := summaryWriter(stdout, stderr, in.StepSummary)
	defer closeSummary()

	if _, set := getenv(envAPIKey); set {
		_, _ = fmt.Fprintln(out, lineAPIKey)

		return audit.ExitRefused
	}

	return audit.Run{Root: root, Inputs: in, Steps: steps, Out: out}.Run(ctx)
}

// inputs reads the command line and the step's environment. Its errors name
// what is wrong, never a value of the environment's.
func inputs(args []string, getenv func(string) (string, bool), stderr io.Writer) (audit.Inputs, error) {
	flags := flag.NewFlagSet("auditrun", flag.ContinueOnError)
	flags.SetOutput(stderr)

	tarball := flags.String("claude-tarball", "", "the Claude Code tarball the install step kept")
	integrity := flags.String("claude-integrity", "", "the tarball's npm integrity, sha512-<base64>, from the one pin")

	if err := flags.Parse(args); err != nil {
		return audit.Inputs{}, fmt.Errorf("usage: %w", err)
	}

	if flags.NArg() != 0 {
		return audit.Inputs{}, fmt.Errorf("usage: auditrun takes no arguments, got %d", flags.NArg())
	}

	if *tarball == "" {
		return audit.Inputs{}, fmt.Errorf("usage: -claude-tarball is required")
	}

	if _, err := audit.ParseIntegrity(*integrity); err != nil {
		return audit.Inputs{}, fmt.Errorf("usage: -claude-integrity: %w", err)
	}

	token, _ := getenv(envToken)
	canary, _ := getenv(envCanary)
	gitHubEnv, _ := getenv(envGitHubEnv)
	stepSummary, _ := getenv(envStepSummary)

	switch {
	case token == "":
		return audit.Inputs{}, fmt.Errorf("%s is not set", envToken)
	case !isCanary(canary):
		return audit.Inputs{}, fmt.Errorf("%s is not %d lower-case hex characters", envCanary, canaryLength)
	case gitHubEnv == "":
		return audit.Inputs{}, fmt.Errorf("%s is not set", envGitHubEnv)
	}

	return audit.Inputs{
		Token:       token,
		Canary:      canary,
		GitHubEnv:   gitHubEnv,
		StepSummary: stepSummary,
		Tarball:     *tarball,
		Integrity:   *integrity,
	}, nil
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
