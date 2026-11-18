package review_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// renderDiff is the unified diff the render tests review. internal/x/y.go has a
// context line at new line 1, a removed line, and two added ones at new lines 2
// and 3; internal/x/z.go has an added line at new line 11.
const renderDiff = `diff --git a/internal/x/y.go b/internal/x/y.go
index 1111111..2222222 100644
--- a/internal/x/y.go
+++ b/internal/x/y.go
@@ -1,3 +1,4 @@
 package y
-// Removed was old.
+// Added is new.
+// Added too.
 
diff --git a/internal/x/z.go b/internal/x/z.go
--- a/internal/x/z.go
+++ b/internal/x/z.go
@@ -10,1 +10,2 @@
 func a() {}
+func b() {}
`

// renderSummary is the summary of every answer the render tests render.
const renderSummary = "The change does what the issue asks."

// The findings the render tests render: one on an added line, one on a context
// line, one on an added line of another file, one on a line the diff does not
// show and one with no position at all.
var (
	renderAddedLine = review.Finding{
		Severity: review.SeverityBlocker,
		Category: review.CategoryCorrectness,
		Path:     "internal/x/y.go",
		Line:     2,
		Title:    "Off by one",
		Body:     "The loop ends one short.",
	}
	renderContextLine = review.Finding{
		Severity: review.SeveritySuggestion,
		Category: review.CategoryReadability,
		Path:     "internal/x/y.go",
		Line:     1,
		Title:    "Name the package",
		Body:     "y says nothing.",
	}
	renderOtherFile = review.Finding{
		Severity: review.SeverityNit,
		Category: review.CategoryArchitecture,
		Path:     "internal/x/z.go",
		Line:     11,
		Title:    "A second file",
		Body:     "z.go adds a function.",
	}
	renderOutsideDiff = review.Finding{
		Severity: review.SeverityNit,
		Category: review.CategoryCorrectness,
		Path:     "internal/x/y.go",
		Line:     99,
		Title:    "Unreachable branch",
		Body:     "Nothing reaches line 99.",
	}
	renderNoPosition = review.Finding{
		Severity: review.SeveritySuggestion,
		Category: review.CategoryTests,
		Title:    "No test covers it",
		Body:     "Add a test.",
	}
)

// TestRenderShowsEveryFindingAndCommentsOnTheDiffsNewSide pins the review the
// program posts (ADR 0033): the body names the head commit, holds the summary,
// says the review is advice and never an approval, and lists every finding with
// its severity and category, a blocking one marked blocking. A finding the diff
// shows on its new side becomes an inline comment, in the findings' order; any
// other finding is listed in the body. Render refuses nothing: the poster
// refuses the events, and CommitID and Event are handed on unchanged.
func TestRenderShowsEveryFindingAndCommentsOnTheDiffsNewSide(t *testing.T) {
	t.Parallel()

	all := []review.Finding{
		renderAddedLine,
		renderContextLine,
		renderOtherFile,
		renderOutsideDiff,
		renderNoPosition,
	}

	inlineEveryFinding := []review.InlineComment{
		{Path: "internal/x/y.go", Line: 2},
		{Path: "internal/x/y.go", Line: 1},
		{Path: "internal/x/z.go", Line: 11},
	}

	cases := []struct {
		name       string
		findings   []review.Finding
		event      review.Event
		diff       string
		wantEvent  review.Event
		wantInline []review.InlineComment
		wantBody   []string
		wantAbsent []string
	}{
		{
			name:       "a blocking finding with the switch off says it would have requested changes",
			findings:   all,
			event:      review.EventComment,
			diff:       renderDiff,
			wantEvent:  review.EventComment,
			wantInline: inlineEveryFinding,
			wantBody:   []string{headSHA, renderSummary, "would have requested changes"},
			wantAbsent: []string{renderAddedLine.Title, renderContextLine.Title, renderOtherFile.Title},
		},
		{
			name:       "a request for changes does not say it would have requested them",
			findings:   all,
			event:      review.EventRequestChanges,
			diff:       renderDiff,
			wantEvent:  review.EventRequestChanges,
			wantInline: inlineEveryFinding,
			wantBody:   []string{headSHA, renderSummary},
			wantAbsent: []string{"would have requested changes", renderAddedLine.Title, renderContextLine.Title, renderOtherFile.Title},
		},
		{
			name:      "a diff that shows no line of the findings lists them all in the body",
			findings:  all,
			event:     review.EventComment,
			diff:      "",
			wantEvent: review.EventComment,
			wantBody: []string{
				headSHA,
				renderSummary,
				renderAddedLine.Title,
				renderContextLine.Title,
				renderOtherFile.Title,
				renderOutsideDiff.Title,
				renderNoPosition.Title,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			answer := review.Answer{Summary: renderSummary, Findings: tc.findings}

			got := review.Render(headSHA, answer, tc.event, tc.diff)

			if got.CommitID != headSHA {
				t.Errorf("CommitID = %q, want %q: the review names the commit it reviewed", got.CommitID, headSHA)
			}

			if got.Event != tc.wantEvent {
				t.Errorf("Event = %q, want %q: Render passes the event on unchanged", got.Event, tc.wantEvent)
			}

			var inline []review.InlineComment
			for _, comment := range got.Comments {
				inline = append(inline, review.InlineComment{Path: comment.Path, Line: comment.Line})
			}

			if diff := cmp.Diff(tc.wantInline, inline); diff != "" {
				t.Errorf("the inline comments (-want +got):\n%s", diff)
			}

			for _, want := range tc.wantBody {
				if !strings.Contains(got.Body, want) {
					t.Errorf("the review's body does not hold %q; body:\n%s", want, got.Body)
				}
			}

			for _, absent := range tc.wantAbsent {
				if strings.Contains(got.Body, absent) {
					t.Errorf("the review's body holds %q, which belongs in an inline comment; body:\n%s", absent, got.Body)
				}
			}

			for _, want := range []string{"advice", "approv"} {
				if !strings.Contains(strings.ToLower(got.Body), want) {
					t.Errorf("the review's body does not say it is advice and never an approval (%q is missing); body:\n%s", want, got.Body)
				}
			}

			for _, finding := range tc.findings {
				text := renderedWith(t, got, finding)

				for _, want := range []string{string(finding.Severity), string(finding.Category), finding.Title} {
					if !strings.Contains(text, want) {
						t.Errorf("the rendering of %q does not hold %q: %q", finding.Title, want, text)
					}
				}

				if finding.Blocking() && !strings.Contains(strings.ToLower(text), "blocking") {
					t.Errorf("the blocking finding %q is not marked blocking: %q", finding.Title, text)
				}
			}
		})
	}
}

// gitHubTextLimit is GitHub's limit on a review's body and on each inline
// comment's body, in characters (runes): a longer text is refused with a 422 and
// the model call that wrote it is spent for nothing.
const gitHubTextLimit = 65_000

// TestRenderKeepsTheReviewUnderGitHubsLimits pins what a huge model answer costs
// the reviewer: GitHub refuses a review whose body or whose inline comment body
// is over 65,000 characters, so Render caps both and says in the body that it
// truncated it, while the review still names the head it reviewed and still says
// it is advice and never an approval (ADR 0033). A normal-sized answer is
// rendered whole, with no word of truncation.
func TestRenderKeepsTheReviewUnderGitHubsLimits(t *testing.T) {
	t.Parallel()

	hugeFindings := make([]review.Finding, 0, 20)
	for i := range 20 {
		hugeFindings = append(hugeFindings, review.Finding{
			Severity: review.SeveritySuggestion,
			Category: review.CategoryReadability,
			Title:    fmt.Sprintf("Finding %d", i),
			Body:     strings.Repeat("b", 10_000),
		})
	}

	hugeSummary := "The change does what the issue asks. " + strings.Repeat("s", 70_000)

	hugeInline := review.Finding{
		Severity: review.SeveritySuggestion,
		Category: review.CategoryReadability,
		Path:     "internal/x/y.go",
		Line:     2,
		Title:    "One long line",
		Body:     strings.Repeat("c", 70_000),
	}

	cases := []struct {
		name          string
		answer        review.Answer
		diff          string
		wantTruncated bool
	}{
		{
			name:          "a huge summary and twenty huge findings leave the body under the limit",
			answer:        review.Answer{Summary: hugeSummary, Findings: hugeFindings},
			wantTruncated: true,
		},
		{
			name:          "a huge inline finding leaves its comment body under the limit",
			answer:        review.Answer{Summary: hugeSummary, Findings: []review.Finding{hugeInline}},
			diff:          renderDiff,
			wantTruncated: true,
		},
		{
			name:   "a normal answer is rendered whole",
			answer: review.Answer{Summary: renderSummary, Findings: []review.Finding{renderAddedLine, renderOutsideDiff, renderNoPosition}},
			diff:   renderDiff,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.Render(headSHA, tc.answer, review.EventComment, tc.diff)

			if n := utf8.RuneCountInString(got.Body); n > gitHubTextLimit {
				t.Errorf("the review's body is %d characters, want at most %d: GitHub refuses a longer review, and the model call that wrote it is spent", n, gitHubTextLimit)
			}

			if !strings.Contains(got.Body, headSHA) {
				t.Errorf("the review's body does not name the head commit %q it reviewed, though the body is %d characters long", headSHA, utf8.RuneCountInString(got.Body))
			}

			for _, want := range []string{"advice", "approv"} {
				if !strings.Contains(strings.ToLower(got.Body), want) {
					t.Errorf("the review's body does not say it is advice and never an approval (%q is missing), though the body is %d characters long", want, utf8.RuneCountInString(got.Body))
				}
			}

			truncated := strings.Contains(strings.ToLower(got.Body), "truncated")
			if truncated != tc.wantTruncated {
				t.Errorf("the review's body says truncation %t, want %t: a body cut at the limit says so, and a body under it says nothing of truncation", truncated, tc.wantTruncated)
			}

			if tc.diff != "" && len(got.Comments) == 0 {
				t.Fatal("the review holds no inline comment, so no comment body is rendered: a finding of the answer is on a line the diff shows")
			}

			for _, comment := range got.Comments {
				if n := utf8.RuneCountInString(comment.Body); n > gitHubTextLimit {
					t.Errorf("the inline comment on %s:%d is %d characters, want at most %d: GitHub refuses a longer comment, and the model call that wrote it is spent", comment.Path, comment.Line, n, gitHubTextLimit)
				}
			}
		})
	}
}

// TestRenderDefusesMentionsInTheModelsText pins what a mention in the model's
// text is allowed to do: notify nobody. The summary and the findings are text
// the model wrote, and a review that carries @someone would ping a user or a
// team of the repository, so Render defuses every mention (an @ followed by a
// letter or a digit) while the handle text itself stays readable (ADR 0033).
func TestRenderDefusesMentionsInTheModelsText(t *testing.T) {
	t.Parallel()

	mention := regexp.MustCompile(`@[A-Za-z0-9]`)

	answer := review.Answer{
		Summary: "Thanks to @octocat and @some-org/some-team for the report; the address is dev@example.com.",
		Findings: []review.Finding{
			{
				Severity: review.SeveritySuggestion,
				Category: review.CategoryReadability,
				Path:     "internal/x/y.go",
				Line:     2,
				Title:    "Credit @octocat",
				Body:     "@some-org/some-team owns it and @octocat disagrees.",
			},
			{
				Severity: review.SeverityNit,
				Category: review.CategoryReadability,
				Title:    "Ask @octocat",
				Body:     "The nit is for @octocat.",
			},
		},
	}

	got := review.Render(headSHA, answer, review.EventComment, renderDiff)

	type renderedText struct {
		where string
		text  string
	}

	texts := []renderedText{{where: "the review's body", text: got.Body}}
	for _, comment := range got.Comments {
		texts = append(texts, renderedText{where: fmt.Sprintf("the inline comment on %s:%d", comment.Path, comment.Line), text: comment.Body})
	}

	for _, rendered := range texts {
		if found := mention.FindString(rendered.text); found != "" {
			t.Errorf("%s holds the mention %q: text the model wrote cannot notify anybody, so Render defuses every @; text:\n%s", rendered.where, found, rendered.text)
		}
	}

	for _, want := range []string{"octocat", "some-org/some-team"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("the review's body drops the handle text %q of a mention: the finding stays readable, only the @ goes; body:\n%s", want, got.Body)
		}
	}

	if !strings.Contains(got.Body, "example.com") {
		t.Errorf("the review's body drops the email address in the summary, which stays readable; body:\n%s", got.Body)
	}

	if len(got.Comments) != 1 {
		t.Fatalf("the review holds %d inline comments, want 1: the finding on internal/x/y.go:2 is on a line the diff shows", len(got.Comments))
	}

	if !strings.Contains(got.Comments[0].Body, "octocat") {
		t.Errorf("the inline comment drops the handle text of its mention; body:\n%s", got.Comments[0].Body)
	}
}

// TestRenderCommentsOnlyOnLinesTheDiffsNewSideShows pins the new-side detection
// Render reads a diff with, on the forms a real diff takes: a hunk header
// without counts ("@@ -1 +1 @@") still names the lines it holds, a line that
// starts with a backslash (the "No newline at end of file" marker) is no line
// of either side, and a deleted file
// ("+++ /dev/null") has no new side to comment on, so a finding on it is listed
// in the body instead of posted as an inline comment GitHub would refuse.
func TestRenderCommentsOnlyOnLinesTheDiffsNewSideShows(t *testing.T) {
	t.Parallel()

	const (
		hunkWithoutCounts = `diff --git a/internal/x/y.go b/internal/x/y.go
--- a/internal/x/y.go
+++ b/internal/x/y.go
@@ -1 +1 @@
-package x
+package y
`
		noNewlineAtEnd = `diff --git a/internal/x/y.go b/internal/x/y.go
--- a/internal/x/y.go
+++ b/internal/x/y.go
@@ -1,2 +1,2 @@
 package y
-// old
+// new
\ No newline at end of file
`
		deletedFile = `diff --git a/internal/x/y.go b/internal/x/y.go
deleted file mode 100644
--- a/internal/x/y.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package y
-// old
`
	)

	findingAt := func(line int) review.Finding {
		return review.Finding{
			Severity: review.SeveritySuggestion,
			Category: review.CategoryCorrectness,
			Path:     "internal/x/y.go",
			Line:     line,
			Title:    fmt.Sprintf("Line %d", line),
			Body:     "Something about this line.",
		}
	}

	cases := []struct {
		name       string
		diff       string
		finding    review.Finding
		wantInline bool
	}{
		{
			name:       "an added line of a hunk header without counts",
			diff:       hunkWithoutCounts,
			finding:    findingAt(1),
			wantInline: true,
		},
		{
			name:       "the added line before the no-newline marker",
			diff:       noNewlineAtEnd,
			finding:    findingAt(2),
			wantInline: true,
		},
		{
			name:    "a line the no-newline marker must not add",
			diff:    noNewlineAtEnd,
			finding: findingAt(3),
		},
		{
			name:    "a line of a deleted file, which has no new side",
			diff:    deletedFile,
			finding: findingAt(1),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.Render(headSHA, review.Answer{Summary: renderSummary, Findings: []review.Finding{tc.finding}}, review.EventComment, tc.diff)

			if tc.wantInline {
				if len(got.Comments) != 1 {
					t.Fatalf("the review holds %d inline comments, want the one on %s:%d: the diff shows that line on its new side: %+v", len(got.Comments), tc.finding.Path, tc.finding.Line, got.Comments)
				}

				if !strings.Contains(got.Comments[0].Body, tc.finding.Title) {
					t.Errorf("the inline comment on %s:%d does not hold the finding %q; body:\n%s", tc.finding.Path, tc.finding.Line, tc.finding.Title, got.Comments[0].Body)
				}

				return
			}

			if len(got.Comments) != 0 {
				t.Fatalf("the review holds %d inline comments, want none: the diff shows no such line on its new side: %+v", len(got.Comments), got.Comments)
			}

			if !strings.Contains(got.Body, tc.finding.Title) {
				t.Errorf("the review's body does not list the finding %q, which has no line on the new side to comment on; body:\n%s", tc.finding.Title, got.Body)
			}
		})
	}
}

// renderedWith returns the review's text that renders f: the body of the inline
// comment on f's line when the review makes one, the review's body otherwise.
func renderedWith(t *testing.T, got review.Review, f review.Finding) string {
	t.Helper()

	for _, comment := range got.Comments {
		if strings.Contains(comment.Body, f.Title) {
			return comment.Body
		}
	}

	if strings.Contains(got.Body, f.Title) {
		return got.Body
	}

	t.Fatalf("the review renders no finding titled %q: %+v", f.Title, got)

	return ""
}
