package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
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
// Run asks it concurrently, once per part of a split diff (issue #367).
type Asker interface {
	Ask(ctx context.Context, p Prompt) (string, error)
}

// The production clients implement Run's interfaces.
var (
	_ PullRequests = (*GitHub)(nil)
	_ Asker        = (*ClaudeCLI)(nil)
)

// Config is one run's input: the pull request, the reviewer's own login
// (<slug>[bot]), the AI_REVIEW_REQUEST_CHANGES switch, main's instructions and
// where the run writes its GitHub workflow notices (nil discards them;
// ci/aireview's main passes its standard output, issue #343).
type Config struct {
	Number         int
	BotLogin       string
	RequestChanges bool
	Instructions   Instructions
	Notices        io.Writer
}

// Result is what a run did: the head it reviewed, the event it posted, how many
// findings and blocking findings the review held, and the IDs of the reviews it
// dismissed, in the order dismissed. Skip is set, and the counts are 0, when
// the run ended in the skipped outcome (issues #367, #376): it posted a
// COMMENT review that says why, and nothing else.
type Result struct {
	HeadSHA   string
	Event     Event
	Findings  int
	Blocking  int
	Dismissed []int64
	Skip      *Skip
}

// Run reviews pull request cfg.Number: it reads the pull request (a draft fails
// with ErrDraft), its diff, leaves the generated and golden files out of it
// (FilterDiff) and splits what remains into at most MaxDiffParts parts of at
// most MaxDiffBytes at file boundaries (SplitDiff, issue #367), reads its
// files, and asks the model about each part in a call of its own, concurrently,
// with the prompt of that part, naming the files it left out (BuildPartPrompt;
// a diff of one part is BuildPrompt's prompt). It parses each answer
// (ParseAnswer), decides the event over every part's findings (Decide), renders
// one review of the head SHA it read, with one summary per part, naming the
// files it left out with the lines each adds and what the model-free scan of
// those lines found (ScanLeftOut, RenderFiltered; never part of the prompt,
// the findings or the event), and posts it. Then, only when no finding is
// blocking, it dismisses the reviews DismissalsFor picks.
//
// An answer of the wrong shape is asked once more with the same prompt while
// ctx is not done, announced by a notice to cfg.Notices that names the shape
// check and, for a split diff, the part (issues #316, #343). A part that fails
// otherwise (a model error, a usage limit, a deadline, a cancellation) cancels
// the other parts, and the run fails with nothing posted.
//
// Two causes end in the skipped outcome instead (issues #367, #376, ADR 0033):
// a diff too large to review (a raw diff the client refuses with
// ErrDiffTooLarge, a single file's section over MaxDiffBytes, or more than
// MaxDiffParts parts; the model is not asked), and a model that answered twice
// in turn with an answer of the wrong shape, for the diff or for one of its
// parts once the others have ended. Run then posts one COMMENT review on the
// head it read that says the review was skipped and why, with no finding and
// no inline comment, dismisses nothing, and returns Result.Skip and no error.
// Neither the review nor Skip.Message holds text of the model's answer or the
// decoder's message (issue #366). The review of two unusable answers is posted
// on a context detached from ctx's cancellation, bounded by
// UnreadablePostTimeout, so it is posted even when ctx ends right after the
// second answer; a failed post of a skipped review fails the run. Every other
// error before the post returns with nothing posted and nothing dismissed. Run
// sets no deadline of its own: ctx carries the run's deadline, which
// ci/aireview's main sets (runDeadline), and every call but that one post ends
// with it.
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
		if s, ok := skipTooLarge(err); ok {
			return postSkipped(ctx, gh, cfg.Number, pr.HeadSHA, s)
		}

		return Result{}, err
	}

	diff, excluded := FilterDiff(raw)

	parts, err := SplitDiff(diff)
	if err != nil {
		if s, ok := skipTooLarge(err); ok {
			return postSkipped(ctx, gh, cfg.Number, pr.HeadSHA, s)
		}

		return Result{}, fmt.Errorf("pull request %d: %w", cfg.Number, err)
	}

	files, err := gh.ChangedFiles(ctx, cfg.Number)
	if err != nil {
		return Result{}, err
	}

	notices := cfg.Notices
	if notices == nil {
		notices = io.Discard
	}

	answers, err := askParts(ctx, model, partsRequest{cfg: cfg, pr: pr, files: files, parts: parts, excluded: excluded}, notices)
	if err != nil {
		var unusable *noUsableAnswerError
		if errors.As(err, &unusable) {
			// The model calls are spent, so the review that says so is posted
			// even when the run's context ended right after the second answer:
			// on a context its cancellation does not reach, with a short
			// timeout of its own (issue #316, reviewer m3).
			postCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), UnreadablePostTimeout)
			defer cancel()

			return postSkipped(postCtx, gh, cfg.Number, pr.HeadSHA, skipNoUsableAnswer(unusable.check, unusable.index, unusable.count, unusable.usable))
		}

		return Result{}, err
	}

	answer := combineAnswers(answers)
	event := Decide(answer.Findings, cfg.RequestChanges)

	// The scan of the left-out files reaches the review's note alone: never
	// the prompt, the findings or the event (issue #342).
	posted := RenderFiltered(pr.HeadSHA, answer, event, diff, ScanLeftOut(raw))

	if err := gh.PostReview(ctx, cfg.Number, posted); err != nil {
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

// postSkipped posts the review of the skipped outcome s on headSHA and returns
// its Result, or the post's error, which fails the run: a skip nobody can see
// on the pull request would be a silent pass.
func postSkipped(ctx context.Context, gh PullRequests, number int, headSHA string, s skipped) (Result, error) {
	if err := gh.PostReview(ctx, number, skippedReview(headSHA, s)); err != nil {
		return Result{}, fmt.Errorf("post the review that says the AI review of pull request %d was skipped: %w", number, err)
	}

	skip := s.skip

	return Result{HeadSHA: headSHA, Event: EventComment, Skip: &skip}, nil
}

// UnreadablePostTimeout bounds the detached post of the review that says the
// AI review was skipped because the model gave no usable answer twice, which
// Run makes on a context detached from the run's cancellation (issues #316,
// #376, reviewer m3): it is the one call that may outlive the run's deadline,
// and by no more than this. ci/aireview's deadline_test.go checks that the
// run's deadline plus this still fits the workflow's job timeout.
const UnreadablePostTimeout = 30 * time.Second

// errUnreadableTwice marks a run whose model answered twice in turn with an
// answer of the wrong shape (issue #316); Run then takes the skipped outcome.
var errUnreadableTwice = errors.New("the model was asked twice and neither answer could be read")

// noUsableAnswerError is askParts' error for part index of count, whose model
// answered twice in turn with an answer of the wrong shape, the second failing
// the shape check check (issue #376). It wraps errUnreadableTwice and
// ErrMalformedAnswer, and its text holds no text of the answer. usable is how
// many other parts askParts got a usable answer for, whose findings the
// skipped review leaves out (0 for a diff of one part).
type noUsableAnswerError struct {
	check        string
	index, count int
	usable       int
}

func (e *noUsableAnswerError) Error() string {
	return fmt.Sprintf("%s: part %d of %d: %s: %s", errUnreadableTwice, e.index, e.count, ErrMalformedAnswer, e.check)
}

func (e *noUsableAnswerError) Unwrap() []error {
	return []error{errUnreadableTwice, ErrMalformedAnswer}
}

// partsRequest is what askParts asks about: the run's config, the pull
// request, its files, the parts of its filtered diff and the files left out.
type partsRequest struct {
	cfg      Config
	pr       PullRequest
	files    []ChangedFile
	parts    []string
	excluded []string
}

// partResult is one part's outcome: its index in the parts, its answer or its
// error, and the notices it wrote.
type partResult struct {
	index   int
	answer  Answer
	err     error
	notices []byte
}

// askParts asks model about every part of req, each in a goroutine of its own
// with BuildPartPrompt's prompt (a diff of one part is asked on the caller's
// goroutine), and returns the answers in the parts' order. Each part keeps its
// own retry of an answer of the wrong shape (askForAnswer). Each part writes
// its notices to a buffer of its own, which askParts copies to notices in the
// parts' order once every part has ended, on its own goroutine alone, so
// notices needs no lock and reads the same whatever order the parts end in.
//
// A part that fails for any reason but two unusable answers cancels the
// others, and askParts returns the first such failure once every part has
// ended. Otherwise a part with two unusable answers, the first in the parts'
// order, gives a *noUsableAnswerError once every part has ended: the others
// are let end, since one of them may still fail for real, and a real failure
// fails the run rather than skip it (ADR 0033).
func askParts(ctx context.Context, model Asker, req partsRequest, notices io.Writer) ([]Answer, error) {
	count := len(req.parts)

	if count == 1 {
		prompt := BuildPrompt(req.cfg.Instructions, req.pr, req.files, req.parts[0], req.excluded)

		answer, err := askForAnswer(ctx, model, prompt, req.cfg.Number, notices, 1, 1)
		if err != nil {
			return nil, partError(err, 1, 1)
		}

		return []Answer{answer}, nil
	}

	partsCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered for every part, so no goroutine blocks on its send once
	// askParts has stopped reading: every one ends, whatever happens here.
	results := make(chan partResult, count)

	for i, part := range req.parts {
		prompt := BuildPartPrompt(req.cfg.Instructions, req.pr, req.files, part, req.excluded, i+1, count)

		go func() {
			var written bytes.Buffer

			answer, err := askForAnswer(partsCtx, model, prompt, req.cfg.Number, &written, i+1, count)
			results <- partResult{index: i, answer: answer, err: err, notices: written.Bytes()}
		}()
	}

	var (
		answers  = make([]Answer, count)
		written  = make([][]byte, count)
		failure  error
		unusable *noUsableAnswerError
		usable   int
	)

	for range count {
		r := <-results

		written[r.index] = r.notices

		var part *noUsableAnswerError

		switch err := partError(r.err, r.index+1, count); {
		case err == nil:
			answers[r.index] = r.answer
			usable++
		case errors.As(err, &part):
			if unusable == nil || part.index < unusable.index {
				unusable = part
			}
		case failure == nil:
			failure = err

			cancel()
		}
	}

	// Every part has ended: its notices are written in the parts' order, so
	// the log reads the same whatever order the parts ended in. The notice is
	// best effort: a log that cannot be written costs no review.
	for _, w := range written {
		_, _ = notices.Write(w)
	}

	if failure != nil {
		return nil, failure
	}

	if unusable != nil {
		// Counted on this goroutine alone, from the results every part sent,
		// so the skipped review can say what it leaves out (issue #367).
		unusable.usable = usable

		return nil, unusable
	}

	return answers, nil
}

// partError returns err of part index of count as askParts returns it: nil for
// nil, a *noUsableAnswerError for two unusable answers, and otherwise err,
// naming the part when the diff has more than one.
func partError(err error, index, count int) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errUnreadableTwice):
		check, _ := shapeCheck(err)

		return &noUsableAnswerError{check: check, index: index, count: count}
	case count > 1:
		return fmt.Errorf("part %d of %d: %w", index, count, err)
	default:
		return err
	}
}

// combineAnswers returns the answer of every part as one: a diff of one part's
// answer unchanged, and otherwise one summary per part, "Part i of n:" before
// each, and every part's findings in the parts' order (issue #367).
func combineAnswers(answers []Answer) Answer {
	if len(answers) == 1 {
		return answers[0]
	}

	var (
		summaries = make([]string, 0, len(answers))
		findings  []Finding
	)

	for i, a := range answers {
		summaries = append(summaries, fmt.Sprintf("**Part %d of %d:** %s", i+1, len(answers), strings.TrimSpace(a.Summary)))
		findings = append(findings, a.Findings...)
	}

	if findings == nil {
		findings = []Finding{}
	}

	return Answer{Summary: strings.Join(summaries, "\n\n"), Findings: findings}
}

// askForAnswer asks model with prompt and parses its answer, for part index of
// count of the diff. An answer refused for its shape (shapeError) is asked
// once more with the same prompt, unless ctx is done by then: the retry stays
// inside the run's deadline, which also caps the retry's own model timeout. A
// second answer of the wrong shape gives an error wrapping errUnreadableTwice
// and ErrMalformedAnswer. A finding ParseAnswer refuses is not asked again
// (#295 keeps those fields strict), and neither is a failed model call. Before
// the retry it writes one GitHub notice to notices that names the pull
// request, the part when count is above 1, and the shape check that failed,
// never any text of the answer (issue #343). The error it returns for an
// answer of the wrong shape, once or twice, holds no text of the answer
// either: a shapeError's text is the constant phrase of its check alone, never
// the decoder's message (issue #366).
func askForAnswer(ctx context.Context, model Asker, prompt Prompt, number int, notices io.Writer, index, count int) (Answer, error) {
	answer, err := askOnce(ctx, model, prompt, number)

	check, isShape := shapeCheck(err)
	if err == nil || !isShape {
		return answer, err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return Answer{}, fmt.Errorf("%w; not asked again: %w", err, ctxErr)
	}

	part := ""
	if count > 1 {
		part = fmt.Sprintf("part %d of %d of ", index, count)
	}

	// The notice is best effort: a log that cannot be written costs no review.
	_, _ = fmt.Fprintf(notices, "::notice title=AI review retried::%s\n", AnnotationData(fmt.Sprintf(
		"The model's answer for %spull request #%d failed the shape check (%s), so it was asked once more (issue #316).",
		part, number, check)))

	answer, err = askOnce(ctx, model, prompt, number)
	if err != nil && isShapeError(err) {
		return Answer{}, fmt.Errorf("%w: %w", errUnreadableTwice, err)
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
