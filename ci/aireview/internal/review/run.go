package review

import (
	"context"
	"errors"
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

// Asker is what Run needs of the model; *ClaudeCLI implements it. (Not
// Complete: test/lint refuses an interface method named for a write verb.)
type Asker interface {
	Ask(ctx context.Context, p Prompt) (string, error)
}

// The production clients implement Run's interfaces.
var (
	_ PullRequests = (*GitHub)(nil)
	_ Asker        = (*ClaudeCLI)(nil)
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
// with ErrDraft), its diff, leaves the generated and golden files out of it
// (FilterDiff; what remains over MaxDiffBytes fails with ErrDiffTooLarge before
// the model is asked) and reads its files, builds the prompt of what remains
// (BuildPrompt), asks the model, parses the answer (ParseAnswer), decides the
// event (Decide), renders the review of the head SHA it read, naming the files
// it left out (RenderFiltered), and posts it. Then, only when no finding is
// blocking, it dismisses the reviews DismissalsFor picks. Any error before the
// post returns with nothing posted and nothing dismissed, with one exception
// (issue #316): an answer of the wrong shape is asked once more with the same
// prompt while ctx is not done, and when the second answer has the wrong shape
// too Run posts a COMMENT review with no finding that says the review could not
// be read, and still fails with an error wrapping ErrMalformedAnswer.
func Run(ctx context.Context, gh PullRequests, model Asker, cfg Config) (Result, error) {
	pr, err := gh.PullRequest(ctx, cfg.Number)
	if err != nil {
		return Result{}, err
	}

	if pr.Draft {
		return Result{}, fmt.Errorf("pull request %d: %w", cfg.Number, ErrDraft)
	}

	raw, err := gh.Diff(ctx, pr)
	if err != nil {
		return Result{}, err
	}

	diff, excluded := FilterDiff(raw)
	if len(diff) > MaxDiffBytes {
		return Result{}, fmt.Errorf("pull request %d: %w: more than %d bytes once generated and golden files are left out",
			cfg.Number, ErrDiffTooLarge, MaxDiffBytes)
	}

	files, err := gh.ChangedFiles(ctx, cfg.Number)
	if err != nil {
		return Result{}, err
	}

	answer, err := askForAnswer(ctx, model, BuildPrompt(cfg.Instructions, pr, files, diff), cfg.Number)
	if err != nil {
		if errors.Is(err, errUnreadableTwice) {
			// Two answers of the wrong shape: say so on the pull request, and
			// still fail the run, so the check stays red (issue #316).
			if postErr := gh.PostReview(ctx, cfg.Number, unreadableReview(pr.HeadSHA)); postErr != nil {
				return Result{}, fmt.Errorf("%w; then: %w", err, postErr)
			}
		}

		return Result{}, err
	}

	event := Decide(answer.Findings, cfg.RequestChanges)

	if err := gh.PostReview(ctx, cfg.Number, RenderFiltered(pr.HeadSHA, answer, event, diff, excluded)); err != nil {
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

	for _, earlier := range DismissalsFor(reviews, cfg.BotLogin, answer.Findings) {
		// The message names both commits, so the maintainer sees which change
		// request a review of which head withdrew (ADR 0033).
		message := fmt.Sprintf("The AI reviewer's review of %s found nothing blocking; "+
			"it withdraws its change request on %s.", pr.HeadSHA, earlier.CommitID)

		if err := gh.DismissReview(ctx, cfg.Number, earlier.ID, message); err != nil {
			return result, fmt.Errorf("posted the review, then: %w", err)
		}

		result.Dismissed = append(result.Dismissed, earlier.ID)
	}

	return result, nil
}

// errUnreadableTwice marks a run whose model answered twice in turn with an
// answer of the wrong shape (issue #316); Run then posts unreadableReview.
var errUnreadableTwice = errors.New("two answers in turn could not be read")

// askForAnswer asks model with prompt and parses its answer. An answer refused
// for its shape (shapeError) is asked once more with the same prompt, unless
// ctx is done by then: the retry stays inside the run's deadline. A second
// answer of the wrong shape gives an error wrapping errUnreadableTwice and
// ErrMalformedAnswer. A finding ParseAnswer refuses is not asked again (#295
// keeps those fields strict), and neither is a failed model call.
func askForAnswer(ctx context.Context, model Asker, prompt Prompt, number int) (Answer, error) {
	answer, err := askOnce(ctx, model, prompt, number)
	if err == nil || !isShapeError(err) {
		return answer, err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return Answer{}, fmt.Errorf("%w; not asked again: %w", err, ctxErr)
	}

	answer, err = askOnce(ctx, model, prompt, number)
	if err != nil && isShapeError(err) {
		return Answer{}, fmt.Errorf("%w, asked twice: %w", errUnreadableTwice, err)
	}

	return answer, err
}

// askOnce asks model once and parses its answer.
func askOnce(ctx context.Context, model Asker, prompt Prompt, number int) (Answer, error) {
	text, err := model.Ask(ctx, prompt)
	if err != nil {
		return Answer{}, fmt.Errorf("ask the model to review pull request %d: %w", number, err)
	}

	answer, err := ParseAnswer(text)
	if err != nil {
		return Answer{}, fmt.Errorf("read the model's review of pull request %d: %w", number, err)
	}

	return answer, nil
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
