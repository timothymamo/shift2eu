package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// The production APIs. Constants on purpose: no environment variable can send a
// token or key to another host (ADR 0033).
const (
	gitHubAPI    = "https://api.github.com"
	anthropicAPI = "https://api.anthropic.com"
)

// main's instructions, read from the working directory, main's checkout.
const (
	claudeMDPath      = "CLAUDE.md"
	reviewChangePath  = ".claude/skills/review-change/SKILL.md"
	securityAuditPath = ".claude/skills/security-audit/SKILL.md"
)

// The clients' timeouts: a model answer of MaxOutputTokens can take minutes.
const (
	gitHubTimeout = 30 * time.Second
	modelTimeout  = 10 * time.Minute
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	settings, err := review.SettingsFromEnv(os.LookupEnv)
	if err != nil {
		return fail(err)
	}

	instructions, err := readInstructions()
	if err != nil {
		return fail(err)
	}

	gh := review.NewGitHub(gitHubAPI, settings.Repository, settings.GitHubToken, review.NewHTTPClient(gitHubTimeout))
	model := review.NewAnthropic(anthropicAPI, settings.ModelKey, review.NewHTTPClient(modelTimeout))

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
