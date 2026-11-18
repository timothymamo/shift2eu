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
const (
	gitHubTimeout = 30 * time.Second
	modelTimeout  = 10 * time.Minute
)

// usage is the program's one command line: the absolute path of the Claude
// Code CLI the workflow installed and verified (ADR 0033).
const usage = "usage: aireview <absolute path of the claude CLI>"

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

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
	})
	if err != nil {
		return fail(err)
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

// fail prints err as one workflow error annotation and returns the exit code 1.
func fail(err error) int {
	fmt.Fprintf(os.Stderr, "::error title=AI review failed::%s\n", review.AnnotationData(err.Error()))

	return 1
}
