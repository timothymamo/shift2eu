package review

import "context"

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
	_, _, _, _ = ctx, gh, model, cfg // the skeleton's stub (step 1)

	return Result{}, errNotImplemented
}

// DismissalsFor returns the reviews to dismiss after a review with findings:
// none when a finding is blocking; otherwise every review of reviews whose
// Login is exactly botLogin and whose State is exactly CHANGES_REQUESTED, in
// the order given. Another login's review, or one of the reviewer's in another
// state, is never returned; an empty botLogin returns none.
func DismissalsFor(reviews []PostedReview, botLogin string, findings []Finding) []PostedReview {
	_, _, _ = reviews, botLogin, findings // the skeleton's stub (step 1)

	return nil
}
