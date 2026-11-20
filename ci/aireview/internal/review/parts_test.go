package review_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// splitInstructions are main's instructions for the tests of a diff the review
// splits in parts: distinctive markers, so a prompt that lost one fails.
var splitInstructions = review.Instructions{
	ClaudeMD:           "CLAUDE-MD-MARKER: the repository's rules",
	ReviewChangeSkill:  "REVIEW-CHANGE-MARKER: how to review a change",
	SecurityAuditSkill: "SECURITY-AUDIT-MARKER: what to audit",
}

// splitPR is the pull request of the tests of a diff the review splits in
// parts.
func splitPR() review.PullRequest {
	return review.PullRequest{
		Number:  7,
		Title:   "Add x",
		Body:    "It adds x.",
		Author:  "agent[bot]",
		HeadSHA: headSHA,
		BaseSHA: baseSHA,
	}
}

// diffSectionOfSize returns one file section of a unified diff, as git writes
// it, of exactly size bytes: the diff --git header, the two file headers, a
// hunk header and added lines long enough to fill the rest. The added lines
// start with "+", so no line of the section's content can start a section of
// its own, as a real diff's cannot.
func diffSectionOfSize(t *testing.T, path string, size int) string {
	t.Helper()

	header := "diff --git a/" + path + " b/" + path + "\n" +
		"--- a/" + path + "\n" +
		"+++ b/" + path + "\n"

	// One added line with its "+" and its line break: 66 bytes.
	line := "+" + strings.Repeat("x", 64) + "\n"

	// The hunk header states how many lines the hunk adds, so its own length
	// grows with that count: take the first count whose lines fit and pad the
	// last line to make the section exact to the byte.
	for n := 1; ; n++ {
		hunk := fmt.Sprintf("@@ -0,0 +1,%d @@\n", n)
		last := size - len(header) - len(hunk) - (n-1)*len(line)

		if last < 2 {
			t.Fatalf("no section of %d bytes fits the file %s", size, path)
		}

		if last > len(line) {
			continue
		}

		section := header + hunk + strings.Repeat(line, n-1) + "+" + strings.Repeat("x", last-2) + "\n"
		if len(section) != size {
			t.Fatalf("the section of %s is %d bytes, want %d", path, len(section), size)
		}

		return section
	}
}

// diffHeaderOf returns the header that names path on both sides of a section of
// a diff, which is how a test tells one part's section from another's: the pull
// request's file list names every path in every part's prompt, the diff's
// header names only the one its part holds.
func diffHeaderOf(path string) string {
	return "diff --git a/" + path + " b/" + path
}

// partModel is Run's Asker for a diff the review splits in parts (ADR 0033,
// issue #367): the parts run concurrently, so every field a call shares is
// guarded by a mutex, and each call is answered from the file its prompt holds
// rather than from the order the calls arrive in.
type partModel struct {
	// paths are the files of the diff, read without the lock: a test sets them
	// before the run and never changes them after.
	paths []string
	// answers are the answers a file's call gives, in turn, the last one again
	// once they are spent.
	answers map[string][]string
	// errs are the files whose call fails, and with which error.
	errs map[string]error
	// afterAsk runs once a call has been answered, with how many calls the run
	// has made in all, as the deadline tests use it to end the run's context.
	afterAsk func(calls int)

	mu       sync.Mutex
	asked    map[string]int
	prompts  map[string][]review.Prompt
	unplaced []review.Prompt
}

// newPartModel returns a partModel that answers for the files of paths.
func newPartModel(paths ...string) *partModel {
	return &partModel{
		paths:   paths,
		answers: map[string][]string{},
		errs:    map[string]error{},
		asked:   map[string]int{},
		prompts: map[string][]review.Prompt{},
	}
}

// fileOf returns the file of the diff whose section user holds, or "" when it
// holds none.
func (m *partModel) fileOf(user string) string {
	for _, path := range m.paths {
		if strings.Contains(user, diffHeaderOf(path)) {
			return path
		}
	}

	return ""
}

// Ask records the prompt and answers it as the file it holds was told to.
func (m *partModel) Ask(_ context.Context, p review.Prompt) (string, error) {
	path := m.fileOf(p.User)

	m.mu.Lock()

	m.asked[path]++

	m.prompts[path] = append(m.prompts[path], p)

	if path == "" {
		m.unplaced = append(m.unplaced, p)
	}

	asked := m.asked[path]
	err := m.errs[path]
	answers := m.answers[path]
	calls := 0

	for _, n := range m.asked {
		calls += n
	}

	m.mu.Unlock()

	if m.afterAsk != nil {
		m.afterAsk(calls)
	}

	if err != nil {
		return "", err
	}

	if len(answers) == 0 {
		return "", errors.New("the test gave the part no answer")
	}

	if asked > len(answers) {
		return answers[len(answers)-1], nil
	}

	return answers[asked-1], nil
}

// askedOf returns how many calls a file was asked in and the prompts it was
// asked, the prompts in a copy safe to read once the run is over.
func (m *partModel) askedOf(path string) (int, []review.Prompt) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.asked[path], slices.Clone(m.prompts[path])
}

// unplacedPrompts returns the prompts that held no file of the diff, which a
// run that splits the diff never gives the model. The slice is a copy.
func (m *partModel) unplacedPrompts() []review.Prompt {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.unplaced)
}

// TestSplitDiff pins the split of a filtered diff that is over the limit
// (issue #367, ADR 0033): the sections are cut at the "diff --git " headers
// FilterDiff already cuts at, so no file's section is ever split, packed
// greedily in the diff's order into parts of at most MaxDiffBytes, at most
// MaxDiffParts of them. The parts joined are the diff it was given, byte for
// byte. A diff at or under the limit is one part equal to the input, and an
// empty diff is one empty part. A section of its own over the limit, and a diff
// that needs more than MaxDiffParts parts, fail with ErrDiffTooLarge: a hunk cut
// short is no diff, and a part that held two files where one is too large would
// have to split one.
func TestSplitDiff(t *testing.T) {
	t.Parallel()

	const (
		half = review.MaxDiffBytes/2 + 1 // one byte over half, so the two make one over the limit
	)

	var (
		atLimitOneFile = diffSectionOfSize(t, "internal/x/one.go", review.MaxDiffBytes)
		firstHalf      = diffSectionOfSize(t, "internal/x/one.go", review.MaxDiffBytes/2)
		secondHalf     = diffSectionOfSize(t, "internal/x/two.go", half)
		hundredOne     = diffSectionOfSize(t, "internal/x/one.go", 100_000)
		hundredTwo     = diffSectionOfSize(t, "internal/x/two.go", 100_000)
		fifteen        = diffSectionOfSize(t, "internal/x/three.go", 150_000)
	)

	fullPart := func(path string) string { return diffSectionOfSize(t, path, review.MaxDiffBytes) }

	fourFullParts := fullPart("internal/x/four-a.go") + fullPart("internal/x/four-b.go") +
		fullPart("internal/x/four-c.go") + fullPart("internal/x/four-d.go")

	// Five sections of 200,000 bytes: every part holds one of them, since two
	// would need 400,000 bytes, so the diff needs five parts.
	fiveSections := diffSectionOfSize(t, "internal/x/five-a.go", 200_000) +
		diffSectionOfSize(t, "internal/x/five-b.go", 200_000) +
		diffSectionOfSize(t, "internal/x/five-c.go", 200_000) +
		diffSectionOfSize(t, "internal/x/five-d.go", 200_000) +
		diffSectionOfSize(t, "internal/x/five-e.go", 200_000)

	cases := []struct {
		name string
		diff string
		// wantParts is the split the diff must give, byte for byte.
		wantParts []string
		// wantTooLarge says the diff cannot be reviewed in parts.
		wantTooLarge bool
	}{
		{
			name:      "an empty diff is one empty part",
			diff:      "",
			wantParts: []string{""},
		},
		{
			name:      "a diff under the limit is one part",
			diff:      cleanDiff,
			wantParts: []string{cleanDiff},
		},
		{
			name:      "a diff of exactly the limit is one part",
			diff:      atLimitOneFile,
			wantParts: []string{atLimitOneFile},
		},
		{
			name:      "two sections a byte over the limit are two parts",
			diff:      firstHalf + secondHalf,
			wantParts: []string{firstHalf, secondHalf},
		},
		{
			name:      "sections are packed greedily in the diff's order",
			diff:      hundredOne + hundredTwo + fifteen,
			wantParts: []string{hundredOne + hundredTwo, fifteen},
		},
		{
			name:      "four full parts are the most a diff may take",
			diff:      fourFullParts,
			wantParts: []string{fullPart("internal/x/four-a.go"), fullPart("internal/x/four-b.go"), fullPart("internal/x/four-c.go"), fullPart("internal/x/four-d.go")},
		},
		{
			name:         "a diff that needs a fifth part is too large",
			diff:         fiveSections,
			wantTooLarge: true,
		},
		{
			name:         "a single section over the limit is too large",
			diff:         diffSectionOfSize(t, "internal/x/one.go", review.MaxDiffBytes+1),
			wantTooLarge: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parts, err := review.SplitDiff(tc.diff)

			if tc.wantTooLarge {
				if !errors.Is(err, review.ErrDiffTooLarge) {
					t.Fatalf("SplitDiff = %v, %v; want an error wrapping ErrDiffTooLarge: a section of its own over the limit and a diff of more than %d parts cannot be reviewed in parts (issue #367)",
						len(parts), err, review.MaxDiffParts)
				}

				return
			}

			if err != nil {
				t.Fatalf("SplitDiff: %v", err)
			}

			if diff := cmp.Diff(tc.wantParts, parts); diff != "" {
				t.Errorf("SplitDiff (-want +got):\n%s", diff)
			}

			if joined := strings.Join(parts, ""); joined != tc.diff {
				t.Errorf("the parts joined are %d bytes, the diff is %d: the parts of a diff joined are the diff it was given, byte for byte (issue #367)", len(joined), len(tc.diff))
			}

			if len(parts) > review.MaxDiffParts {
				t.Errorf("SplitDiff returned %d parts, more than the %d a diff may take (issue #367)", len(parts), review.MaxDiffParts)
			}

			for i, part := range parts {
				if len(part) > review.MaxDiffBytes {
					t.Errorf("part %d is %d bytes, over the limit of %d: one model call reads one part (issue #367)", i+1, len(part), review.MaxDiffBytes)
				}
			}
		})
	}
}

// TestBuildPartPrompt pins the prompt of one part (issue #367): the prompt of a
// diff the review does not split is today's prompt byte for byte, and a part of
// a split diff is asked with the whole trusted instructions and its own diff
// alone, its system prompt saying which part of how many it is and that the
// other parts are separate calls.
func TestBuildPartPrompt(t *testing.T) {
	t.Parallel()

	pr := splitPR()
	files := []review.ChangedFile{
		{Path: "internal/x/one.go", Status: "modified", Additions: 1},
		{Path: "internal/x/two.go", Status: "modified", Additions: 1},
	}

	parts := []struct {
		path string
		diff string
	}{
		{path: "internal/x/one.go", diff: diffSectionOfSize(t, "internal/x/one.go", 150_000)},
		{path: "internal/x/two.go", diff: diffSectionOfSize(t, "internal/x/two.go", 150_001)},
	}

	// A pull request the review does not split is asked today's prompt: the
	// split is invisible to it, byte for byte.
	whole := review.BuildPrompt(splitInstructions, pr, files, cleanDiff, nil)
	if diff := cmp.Diff(whole, review.BuildPartPrompt(splitInstructions, pr, files, cleanDiff, nil, 1, 1)); diff != "" {
		t.Errorf("BuildPartPrompt of a diff of one part differs from BuildPrompt (-want +got); a diff at or under the limit is asked what it was asked before issue #367:\n%s", diff)
	}

	for i, part := range parts {
		prompt := review.BuildPartPrompt(splitInstructions, pr, files, part.diff, nil, i+1, len(parts))

		wantSentence := fmt.Sprintf("part %d of %d", i+1, len(parts))
		if !strings.Contains(strings.ToLower(prompt.System), wantSentence) {
			t.Errorf("the part's system prompt does not say %q: the model is told which part of how many it reads, so a file whose diff is in another part is no missing file (issue #367)", wantSentence)
		}

		if !strings.Contains(prompt.User, diffHeaderOf(part.path)) {
			t.Errorf("the part's user message does not hold the section of %s, which is the diff this call reads (issue #367)", part.path)
		}

		other := parts[1-i]
		if strings.Contains(prompt.User, diffHeaderOf(other.path)) {
			t.Errorf("the part's user message holds the section of %s as well: each part is asked its own diff alone (issue #367)", other.path)
		}

		if !strings.Contains(prompt.System, splitInstructions.ClaudeMD) {
			t.Error("the part's system prompt does not hold main's CLAUDE.md: every part is asked the whole trusted instructions (issue #367)")
		}
	}
}

// TestRunReviewsADiffAtTheLimitInOneCall pins the side of the limit the split
// keeps as it was (issue #367): a filtered diff of exactly MaxDiffBytes — one
// file, or two that add up to it — is one model call, whose prompt is the
// prompt of today byte for byte and says nothing of parts, and one normal
// review.
func TestRunReviewsADiffAtTheLimitInOneCall(t *testing.T) {
	t.Parallel()

	pr := splitPR()
	files := []review.ChangedFile{
		{Path: "internal/x/one.go", Status: "modified", Additions: 1},
		{Path: "internal/x/two.go", Status: "modified", Additions: 1},
	}

	cases := []struct {
		name string
		diff string
	}{
		{
			name: "one file of exactly the limit",
			diff: diffSectionOfSize(t, "internal/x/one.go", review.MaxDiffBytes),
		},
		{
			name: "two files that add up to exactly the limit",
			diff: diffSectionOfSize(t, "internal/x/one.go", review.MaxDiffBytes/2) +
				diffSectionOfSize(t, "internal/x/two.go", review.MaxDiffBytes-review.MaxDiffBytes/2),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gh := &fakeGitHub{pr: pr, diff: tc.diff, files: files}
			model := &fakeModel{answer: cleanAnswer}

			result, err := review.Run(t.Context(), gh, model, review.Config{
				Number:       pr.Number,
				BotLogin:     testBotLogin,
				Instructions: splitInstructions,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			if model.asked != 1 {
				t.Fatalf("the model was asked %d times, want 1: a diff of at most %d bytes is one call (issue #367)", model.asked, review.MaxDiffBytes)
			}

			want := review.BuildPrompt(splitInstructions, pr, files, tc.diff, nil)
			if diff := cmp.Diff(want, model.prompts[0]); diff != "" {
				t.Errorf("the prompt of a diff of one part differs from BuildPrompt (-want +got):\n%s", diff)
			}

			if strings.Contains(strings.ToLower(model.prompts[0].System), "part 1 of") {
				t.Error("the prompt of an unsplit diff says it is a part of a split one: a diff at or under the limit is asked what it was asked before issue #367")
			}

			if result.Skip != nil {
				t.Errorf("Result.Skip = %+v, want nil: the diff fits one call (issue #367)", *result.Skip)
			}

			if len(gh.posts) != 1 {
				t.Fatalf("the run posted %d reviews, want 1", len(gh.posts))
			}

			if got := gh.posts[0].review.Event; got != review.EventComment {
				t.Errorf("the posted review's Event = %q, want %q", got, review.EventComment)
			}
		})
	}
}

// TestRunReviewsADiffOverTheLimitInParts pins the review of a diff over the
// limit (issue #367, ADR 0033): its sections are reviewed by one model call
// each, concurrently and each with its own section alone, and the findings of
// every part are posted as one combined review whose event is decided over all
// of them, with one summary per part. A blocking finding of any part keeps the
// reviewer from withdrawing an earlier change request; with nothing blocking,
// the reviewer's own earlier change request is still dismissed.
func TestRunReviewsADiffOverTheLimitInParts(t *testing.T) {
	t.Parallel()

	const (
		firstPath  = "internal/x/one.go"
		secondPath = "internal/x/two.go"
	)

	pr := splitPR()
	files := []review.ChangedFile{
		{Path: firstPath, Status: "modified", Additions: 1},
		{Path: secondPath, Status: "modified", Additions: 1},
	}

	// Two parts, one byte over the limit in all: the first section and the
	// second cannot share a part.
	diff := diffSectionOfSize(t, firstPath, 150_000) + diffSectionOfSize(t, secondPath, 150_001)

	earlierChangeRequest := []review.PostedReview{
		{ID: 21, Login: testBotLogin, State: "CHANGES_REQUESTED", CommitID: baseSHA},
	}

	cases := []struct {
		name string
		// answers are the answers each file's call gives.
		answers map[string][]string
		// requestChanges is the AI_REVIEW_REQUEST_CHANGES switch.
		requestChanges bool
		// reviews are the reviews the pull request already holds.
		reviews []review.PostedReview
		// wantEvent is the event of the combined review.
		wantEvent review.Event
		// wantFindings and wantBlocking are the combined review's counts.
		wantFindings int
		wantBlocking int
		// wantDismissed are the IDs of the reviews the run dismisses.
		wantDismissed []int64
		// wantText are texts the combined review's body must hold.
		wantText []string
	}{
		{
			name:           "a blocking finding of the second part requests changes over both",
			answers:        map[string][]string{firstPath: {cleanAnswer}, secondPath: {blockingAnswer}},
			requestChanges: true,
			wantEvent:      review.EventRequestChanges,
			wantFindings:   2,
			wantBlocking:   1,
			wantText:       []string{"Part 1 of 2", "Part 2 of 2", "Name it", "Off by one"},
		},
		{
			name:           "with the switch off the combined review is a comment",
			answers:        map[string][]string{firstPath: {cleanAnswer}, secondPath: {blockingAnswer}},
			requestChanges: false,
			wantEvent:      review.EventComment,
			wantFindings:   2,
			wantBlocking:   1,
			wantText:       []string{"Part 1 of 2", "Part 2 of 2"},
		},
		{
			name:           "a blocking finding of one part withdraws no earlier change request",
			answers:        map[string][]string{firstPath: {cleanAnswer}, secondPath: {blockingAnswer}},
			requestChanges: true,
			reviews:        earlierChangeRequest,
			wantEvent:      review.EventRequestChanges,
			wantFindings:   2,
			wantBlocking:   1,
			wantText:       []string{"Part 1 of 2", "Part 2 of 2"},
		},
		{
			name:           "with no finding of any part blocking the earlier change request is dismissed",
			answers:        map[string][]string{firstPath: {cleanAnswer}, secondPath: {cleanAnswer}},
			requestChanges: true,
			reviews:        earlierChangeRequest,
			wantEvent:      review.EventComment,
			wantFindings:   2,
			wantBlocking:   0,
			wantDismissed:  []int64{21},
			wantText:       []string{"Part 1 of 2", "Part 2 of 2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gh := &fakeGitHub{pr: pr, diff: diff, files: files, reviews: tc.reviews}

			model := newPartModel(firstPath, secondPath)
			model.answers = tc.answers

			result, err := review.Run(t.Context(), gh, model, review.Config{
				Number:         pr.Number,
				BotLogin:       testBotLogin,
				RequestChanges: tc.requestChanges,
				Instructions:   splitInstructions,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			if result.Skip != nil {
				t.Errorf("Result.Skip = %+v, want nil: a diff of two parts is reviewed (issue #367)", *result.Skip)
			}

			if unplaced := model.unplacedPrompts(); len(unplaced) != 0 {
				t.Errorf("%d prompt(s) held no file of the diff, want each part asked its own section alone (issue #367)", len(unplaced))
			}

			for i, path := range []string{firstPath, secondPath} {
				asked, prompts := model.askedOf(path)
				if asked != 1 {
					t.Errorf("the section of %s was asked %d times, want 1: one part is one model call (issue #367)", path, asked)

					continue
				}

				other := secondPath
				if i == 1 {
					other = firstPath
				}

				if !strings.Contains(prompts[0].User, diffHeaderOf(path)) {
					t.Errorf("the prompt of part %d does not hold the section of %s, which is the diff it reads (issue #367)", i+1, path)
				}

				if strings.Contains(prompts[0].User, diffHeaderOf(other)) {
					t.Errorf("the prompt of part %d holds the section of %s as well: each part is asked its own diff alone (issue #367)", i+1, other)
				}

				wantSentence := fmt.Sprintf("part %d of 2", i+1)
				if !strings.Contains(strings.ToLower(prompts[0].System), wantSentence) {
					t.Errorf("the system prompt of part %d does not say %q: a file whose diff is in another part is no missing file (issue #367)", i+1, wantSentence)
				}
			}

			if len(gh.posts) != 1 {
				t.Fatalf("the run posted %d reviews, want 1: the findings of every part go in one review (issue #367)", len(gh.posts))
			}

			post := gh.posts[0]

			if post.review.CommitID != headSHA {
				t.Errorf("the posted review's CommitID = %q, want the head SHA %q", post.review.CommitID, headSHA)
			}

			if post.review.Event != tc.wantEvent {
				t.Errorf("the posted review's Event = %q, want %q: the event is decided over every part's findings (issue #367)", post.review.Event, tc.wantEvent)
			}

			text := post.review.Body
			for _, comment := range post.review.Comments {
				text += "\n" + comment.Body
			}

			for _, want := range tc.wantText {
				if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
					t.Errorf("the posted review does not hold %q; body:\n%s", want, post.review.Body)
				}
			}

			var dismissed []int64

			for _, d := range gh.dismissals {
				dismissed = append(dismissed, d.id)
			}

			if !slices.Equal(tc.wantDismissed, dismissed) {
				t.Errorf("Run dismissed %v, want %v", dismissed, tc.wantDismissed)
			}

			if result.Findings != tc.wantFindings {
				t.Errorf("Result.Findings = %d, want %d: the count is over every part's findings", result.Findings, tc.wantFindings)
			}

			if result.Blocking != tc.wantBlocking {
				t.Errorf("Result.Blocking = %d, want %d", result.Blocking, tc.wantBlocking)
			}
		})
	}
}

// TestRunWhenOneOfPartsFails pins what a part does to the whole run (issue
// #367, ADR 0033): a part whose two answers are unusable lets the others end,
// and then the whole run takes the skipped outcome — a review that left part of
// the change out without saying so would read as a full review, so no part's
// finding is posted. A part that fails for real (a model error, here) cancels
// the others, posts nothing and fails the run, as one call's failure does.
func TestRunWhenOneOfPartsFails(t *testing.T) {
	t.Parallel()

	const (
		firstPath  = "internal/x/one.go"
		secondPath = "internal/x/two.go"
	)

	pr := splitPR()
	files := []review.ChangedFile{
		{Path: firstPath, Status: "modified", Additions: 1},
		{Path: secondPath, Status: "modified", Additions: 1},
	}

	diff := diffSectionOfSize(t, firstPath, 150_000) + diffSectionOfSize(t, secondPath, 150_001)

	// The answer a model writes when it reports nothing at all: the field is
	// missing, so the answer has the wrong shape and is asked once more
	// (issues #316, #376).
	unusable := `{"summary":"The change is fine."}`

	realFailure := fmt.Errorf("ask the model to review pull request 7: %w", review.ErrModel)

	cases := []struct {
		name string
		// answers are the answers each file's call gives.
		answers map[string][]string
		// errs are the files whose call fails.
		errs map[string]error
		// wantErr is the error the run must wrap, or nil for a skipped run.
		wantErr error
		// wantSkip is the cause the run skips with, when it skips.
		wantSkip *review.SkipReason
	}{
		{
			name:     "a part unusable twice skips the whole review",
			answers:  map[string][]string{firstPath: {cleanAnswer}, secondPath: {unusable}},
			wantSkip: skipReason(review.SkipNoUsableAnswer),
		},
		{
			name:    "a part's model error fails the run",
			answers: map[string][]string{firstPath: {cleanAnswer}, secondPath: {cleanAnswer}},
			errs:    map[string]error{secondPath: realFailure},
			wantErr: review.ErrModel,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gh := &fakeGitHub{pr: pr, diff: diff, files: files}

			model := newPartModel(firstPath, secondPath)
			model.answers = tc.answers
			model.errs = tc.errs

			result, err := review.Run(t.Context(), gh, model, review.Config{
				Number:       pr.Number,
				BotLogin:     testBotLogin,
				Instructions: splitInstructions,
			})

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Run = %v, want an error wrapping %v: a part that fails for real fails the whole run (issue #367)", err, tc.wantErr)
				}

				if result.Skip != nil {
					t.Errorf("Result.Skip = %+v, want nil: a real failure is no skipped outcome (issue #367)", *result.Skip)
				}

				if len(gh.posts) != 0 {
					t.Errorf("the run posted %d reviews, want none: a part's failure posts no part's finding (issue #367)", len(gh.posts))
				}

				return
			}

			if err != nil {
				t.Fatalf("Run: %v; want the skipped outcome, which is no failure (issue #367)", err)
			}

			if result.Skip == nil {
				t.Fatalf("Result.Skip = nil, want a skip: a part unusable twice skips the whole review (issue #367)")
			}

			if result.Skip.Reason != *tc.wantSkip {
				t.Errorf("Result.Skip.Reason = %q, want %q", result.Skip.Reason, *tc.wantSkip)
			}

			if result.Skip.Message == "" {
				t.Error("Result.Skip.Message is empty: the skip says why the review was skipped")
			}

			if len(gh.posts) != 1 {
				t.Fatalf("the run posted %d reviews, want 1: a skipped review says so on the pull request (issue #367)", len(gh.posts))
			}

			post := gh.posts[0]

			if post.review.Event != review.EventComment {
				t.Errorf("the skipped review's Event = %q, want %q", post.review.Event, review.EventComment)
			}

			if len(post.review.Comments) != 0 {
				t.Errorf("the skipped review holds %d inline comments, want none: no part's finding is posted (issue #367)", len(post.review.Comments))
			}

			text := strings.ToLower(post.review.Body)

			for _, want := range []string{"skipped", "no usable answer twice"} {
				if !strings.Contains(text, want) {
					t.Errorf("the skipped review does not say %q; body:\n%s", want, post.review.Body)
				}
			}

			if strings.Contains(text, strings.ToLower("Name it")) {
				t.Errorf("the skipped review holds the finding of the part that could be read, want no part's finding: a review that left part of the change out without saying so would read as a full review (issue #367); body:\n%s", post.review.Body)
			}

			if len(gh.dismissals) != 0 {
				t.Errorf("the skipped run dismissed %d review(s), want none: a skip is no finding, so nothing is withdrawn (issues #316, #367)", len(gh.dismissals))
			}
		})
	}
}

// TestRunSkipsADiffItCannotReview pins the size causes of the skipped outcome
// (issue #367, ADR 0033): a diff that packs into more than MaxDiffParts parts, a
// single file whose own section is over MaxDiffBytes, and a raw diff over
// MaxRawDiffBytes end in the skipped outcome — one COMMENT review with no
// finding and no inline comment that says the review was skipped and why, no
// model call at all, and no error, so the check is green rather than red. The
// review of a file too large names that file in a code span, as the "Not
// reviewed" note names a path.
func TestRunSkipsADiffItCannotReview(t *testing.T) {
	t.Parallel()

	const tooBigFile = "internal/x/huge.go"

	fiveFiles := diffSectionOfSize(t, "internal/x/five-a.go", 200_000) +
		diffSectionOfSize(t, "internal/x/five-b.go", 200_000) +
		diffSectionOfSize(t, "internal/x/five-c.go", 200_000) +
		diffSectionOfSize(t, "internal/x/five-d.go", 200_000) +
		diffSectionOfSize(t, "internal/x/five-e.go", 200_000)

	cases := []struct {
		name    string
		diff    string
		diffErr error
		// wantText are texts the skipped review's body must hold.
		wantText []string
		// wantFile is a path the skipped review must name in a code span.
		wantFile string
	}{
		{
			name:     "a diff that needs more than four parts",
			diff:     fiveFiles,
			wantText: []string{"skipped", "300,000", "4 parts"},
		},
		{
			name:     "a single file whose section is over the limit",
			diff:     diffSectionOfSize(t, tooBigFile, review.MaxDiffBytes+1),
			wantText: []string{"skipped"},
			wantFile: tooBigFile,
		},
		{
			name:     "a raw diff over the cap the client reads",
			diffErr:  fmt.Errorf("read the diff of pull request 7: %w", review.ErrDiffTooLarge),
			wantText: []string{"skipped"},
		},
	}

	pr := splitPR()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gh := &fakeGitHub{pr: pr, diff: tc.diff, diffErr: tc.diffErr}
			model := &fakeModel{answer: cleanAnswer}

			result, err := review.Run(t.Context(), gh, model, review.Config{
				Number:       pr.Number,
				BotLogin:     testBotLogin,
				Instructions: splitInstructions,
			})
			if err != nil {
				t.Fatalf("Run: %v; want the skipped outcome, which is no failing check (issue #367)", err)
			}

			if model.asked != 0 {
				t.Errorf("the model was asked %d times, want 0: a diff too large to review is never sent (issue #367)", model.asked)
			}

			if result.Skip == nil {
				t.Fatalf("Result.Skip = nil, want a skip: this diff cannot be reviewed (issue #367)")
			}

			if result.Skip.Reason != review.SkipTooLarge {
				t.Errorf("Result.Skip.Reason = %q, want %q", result.Skip.Reason, review.SkipTooLarge)
			}

			if result.Skip.Message == "" {
				t.Error("Result.Skip.Message is empty: the skip says why the review was skipped, the size and the limit")
			}

			if len(gh.posts) != 1 {
				t.Fatalf("the run posted %d reviews, want 1: a skipped review says so on the pull request (issue #367)", len(gh.posts))
			}

			post := gh.posts[0]

			if post.review.Event != review.EventComment {
				t.Errorf("the skipped review's Event = %q, want %q: a skip is never a change request", post.review.Event, review.EventComment)
			}

			if len(post.review.Comments) != 0 {
				t.Errorf("the skipped review holds %d inline comments, want none: a skip invents no finding (issue #367)", len(post.review.Comments))
			}

			for _, want := range tc.wantText {
				if !strings.Contains(strings.ToLower(post.review.Body), strings.ToLower(want)) {
					t.Errorf("the skipped review does not hold %q; body:\n%s", want, post.review.Body)
				}
			}

			if tc.wantFile != "" && !strings.Contains(post.review.Body, "`"+tc.wantFile+"`") {
				t.Errorf("the skipped review does not name the file %q in a code span, as the \"Not reviewed\" note names a path; body:\n%s", tc.wantFile, post.review.Body)
			}

			if len(gh.dismissals) != 0 {
				t.Errorf("the skipped run dismissed %d review(s), want none: a skip is no finding, so nothing is withdrawn (issues #316, #367)", len(gh.dismissals))
			}
		})
	}
}
