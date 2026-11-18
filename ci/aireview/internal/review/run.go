package review

import (
	"context"
	"fmt"
)

// PullRequests is what Run needs of GitHub; *GitHub implements it.
type PullRequests interface {
	PullRequest(ctx context.Context, number int) (PullRequest, error)
	Diff(ctx context.Context, pr PullRequest) (string, error)
	ChangedFiles(ctx context.Context, number int) ([]ChangedFile, error)
	Reviews(ctx context.Context, number int) ([]PostedReview, error)
	PostReview(ctx context.Context, number int, r Review) error
	DismissReview(ctx context.Context, number int, id int64, message string) error
}

// Asker is what Run needs of the model; *Anthropic implements it. (Not
// Complete: test/lint refuses an interface method named for a write verb.)
type Asker interface {
	Ask(ctx context.Context, p Prompt) (string, error)
}

// The production clients implement Run's interfaces.
var (
	_ PullRequests = (*GitHub)(nil)
	_ Asker        = (*Anthropic)(nil)
)

// Config is one run's input: the pull request, the reviewer's own login
// (<slug>[bot]), the AI_REVIEW_REQUEST_CHANGES switch and main's instructions.
type Config struct {
	Number         int
	BotLogin       string
	RequestChanges bool
	Instructions   Instructions
}

// Result is what a run did: the head it reviewed, the event it posted, how many
// findings and blocking findings the review held, and the IDs of the reviews it
// dismissed, in the order dismissed.
type Result struct {
	HeadSHA   string
	Event     Event
	Findings  int
	Blocking  int
	Dismissed []int64
}

// Run reviews pull request cfg.Number: it reads the pull request (a draft fails
// with ErrDraft), its diff (over MaxDiffBytes fails with ErrDiffTooLarge) and
// its files, builds the prompt (BuildPrompt), asks the model, parses the answer
// (ParseAnswer), decides the event (Decide), renders the review of the head SHA
// it read (Render) and posts it. Then, only when no finding is blocking, it
// dismisses the reviews DismissalsFor picks. Any error before the post returns
// with nothing posted and nothing dismissed.
func Run(ctx context.Context, gh PullRequests, model Asker, cfg Config) (Result, error) {
	pr, err := gh.PullRequest(ctx, cfg.Number)
	if err != nil {
		return Result{}, err
	}

	if pr.Draft {
		return Result{}, fmt.Errorf("pull request %d: %w", cfg.Number, ErrDraft)
	}

	diff, err := gh.Diff(ctx, pr)
	if err != nil {
		return Result{}, err
	}

	files, err := gh.ChangedFiles(ctx, cfg.Number)
	if err != nil {
		return Result{}, err
	}

	text, err := model.Ask(ctx, BuildPrompt(cfg.Instructions, pr, files, diff))
	if err != nil {
		return Result{}, fmt.Errorf("ask the model to review pull request %d: %w", cfg.Number, err)
	}

	answer, err := ParseAnswer(text)
	if err != nil {
		return Result{}, fmt.Errorf("read the model's review of pull request %d: %w", cfg.Number, err)
	}

	event := Decide(answer.Findings, cfg.RequestChanges)

	if err := gh.PostReview(ctx, cfg.Number, Render(pr.HeadSHA, answer, event, diff)); err != nil {
		return Result{}, err
	}

	result := Result{
		HeadSHA:  pr.HeadSHA,
		Event:    event,
		Findings: len(answer.Findings),
		Blocking: countBlocking(answer.Findings),
	}

	if result.Blocking > 0 {
		return result, nil
	}

	reviews, err := gh.Reviews(ctx, cfg.Number)
	if err != nil {
		return result, fmt.Errorf("posted the review, then: %w", err)
	}

	message := fmt.Sprintf("The AI reviewer's review of %s found nothing blocking; it withdraws its earlier change request.", pr.HeadSHA)

	for _, earlier := range DismissalsFor(reviews, cfg.BotLogin, answer.Findings) {
		if err := gh.DismissReview(ctx, cfg.Number, earlier.ID, message); err != nil {
			return result, fmt.Errorf("posted the review, then: %w", err)
		}

		result.Dismissed = append(result.Dismissed, earlier.ID)
	}

	return result, nil
}

// DismissalsFor returns the reviews to dismiss after a review with findings:
// none when a finding is blocking; otherwise every review of reviews whose
// Login is exactly botLogin and whose State is exactly CHANGES_REQUESTED, in
// the order given. Another login's review, or one of the reviewer's in another
// state, is never returned; an empty botLogin returns none.
func DismissalsFor(reviews []PostedReview, botLogin string, findings []Finding) []PostedReview {
	if botLogin == "" || countBlocking(findings) > 0 {
		return nil
	}

	var dismiss []PostedReview

	for _, r := range reviews {
		if r.Login == botLogin && r.State == "CHANGES_REQUESTED" {
			dismiss = append(dismiss, r)
		}
	}

	return dismiss
}
