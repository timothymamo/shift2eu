package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// gitHubAPI is the production GitHub API. A constant on purpose: no environment
// variable can send the App's token to another host (ADR 0033).
const gitHubAPI = "https://api.github.com"

// main's instructions, read from the working directory, main's checkout.
const (
	claudeMDPath      = "CLAUDE.md"
	reviewChangePath  = ".claude/skills/review-change/SKILL.md"
	securityAuditPath = ".claude/skills/security-audit/SKILL.md"
)

// The timeouts: GitHub's calls are quick, the model's answer can take minutes.
// runDeadline bounds the whole run, both model calls of a retry included: each
// call's modelTimeout is capped by what remains of it, so the run ends within
// the job's timeout-minutes of 20 (.github/workflows/ai-review.yml) with room
// for the steps before it and for the one post Run makes past it, the review
// saying the answer could not be read, bounded by 30 seconds (issue #316,
// auditor L3).
const (
	gitHubTimeout = 30 * time.Second
	modelTimeout  = 10 * time.Minute
	runDeadline   = 15 * time.Minute
)

// usage is the program's one command line: the absolute path of the Claude
// Code CLI the workflow installed and verified (ADR 0033).
const usage = "usage: aireview <absolute path of the claude CLI>"

func main() {
	os.Exit(run())
}

func run() int {
	signalled, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ctx, cancel := context.WithTimeout(signalled, runDeadline)
	defer cancel()

	if len(os.Args) != 2 || os.Args[1] == "" || os.Args[1][0] != '/' {
		return fail(fmt.Errorf("%s", usage))
	}

	claudePath := os.Args[1]

	settings, err := review.SettingsFromEnv(os.LookupEnv)
	if err != nil {
		return fail(err)
	}

	instructions, err := readInstructions()
	if err != nil {
		return fail(err)
	}

	gh := review.NewGitHub(gitHubAPI, settings.Repository, settings.GitHubToken, review.NewHTTPClient(gitHubTimeout))
	model := review.NewClaudeCLI(claudePath, settings.ModelToken, modelTimeout)

	result, err := review.Run(ctx, gh, model, review.Config{
		Number:         settings.Number,
		BotLogin:       settings.BotLogin,
		RequestChanges: settings.RequestChanges,
		Instructions:   instructions,
		// The job's log reads workflow commands from standard output (#343).
		Notices: os.Stdout,
	})
	if err != nil {
		return fail(err)
	}

	if result.Skip != nil {
		reportSkipped(os.Stdout, settings.StepSummary, *result.Skip)

		return 0
	}

	fmt.Printf("ai review: posted %s on %s with %d findings (%d blocking), dismissed %d earlier change requests\n",
		result.Event, result.HeadSHA, result.Findings, result.Blocking, len(result.Dismissed))

	return 0
}

// readInstructions reads main's CLAUDE.md and review skills.
// Each path is a constant, read by its own call.
func readInstructions() (review.Instructions, error) {
	claudeMD, err := os.ReadFile(claudeMDPath)
	if err != nil {
		return review.Instructions{}, fmt.Errorf("read %s from main's checkout: %w", claudeMDPath, err)
	}

	reviewChange, err := os.ReadFile(reviewChangePath)
	if err != nil {
		return review.Instructions{}, fmt.Errorf("read %s from main's checkout: %w", reviewChangePath, err)
	}

	securityAudit, err := os.ReadFile(securityAuditPath)
	if err != nil {
		return review.Instructions{}, fmt.Errorf("read %s from main's checkout: %w", securityAuditPath, err)
	}

	return review.Instructions{
		ClaudeMD:           string(claudeMD),
		ReviewChangeSkill:  string(reviewChange),
		SecurityAuditSkill: string(securityAudit),
	}, nil
}

// reportSkipped reports a run that ended in the skipped outcome (issues #367,
// #376, ADR 0033), whose review saying so Run has posted: one workflow warning
// on stdout (review.SkipAnnotation), then, when summaryPath is not "" (the
// absolute $GITHUB_STEP_SUMMARY), one line appended to the job summary
// (review.SkipSummaryLine). A summary that cannot be written is reported as a
// second warning, with its error escaped, and changes nothing else: the review
// on the pull request and the first warning already say the review was
// skipped, so the run still exits 0 rather than turning a skip into a failing
// check over a log line.
func reportSkipped(stdout *os.File, summaryPath string, skip review.Skip) {
	fmt.Fprintln(stdout, review.SkipAnnotation(skip))

	if summaryPath == "" {
		return
	}

	if err := appendLine(summaryPath, review.SkipSummaryLine(skip)); err != nil {
		fmt.Fprintf(stdout, "::warning title=AI review summary not written::%s\n", review.AnnotationData(err.Error()))
	}
}

// appendLine appends line and a line break to the file at path, creating it
// when it does not exist, as the runner's job summary file is appended to.
func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // the runner's absolute $GITHUB_STEP_SUMMARY, which SettingsFromEnv checked; written, never read
	if err != nil {
		return fmt.Errorf("open the job summary: %w", err)
	}

	if _, err := fmt.Fprintln(f, line); err != nil {
		_ = f.Close()

		return fmt.Errorf("append to the job summary: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close the job summary: %w", err)
	}

	return nil
}

// fail prints err as one workflow error annotation and returns the exit code 1.
func fail(err error) int {
	fmt.Fprintf(os.Stderr, "::error title=AI review failed::%s\n", review.AnnotationData(err.Error()))

	return 1
}
