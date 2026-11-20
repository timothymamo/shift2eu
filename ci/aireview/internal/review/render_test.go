package review_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
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
// is over 65,000 characters, counted in UTF-16 code units, so a character
// outside the BMP counts twice; Render caps both and says in the body that it
// truncated it, while the review still names the head it reviewed and still says
// it is advice and never an approval (ADR 0033). A normal-sized answer is
// rendered whole, with no word of truncation, and so is one whose body is
// exactly 65,000 characters: the cap refuses a longer review only, as the diff
// limit refuses a longer diff.
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

	// atTheLimit is an answer whose rendered body is exactly gitHubTextLimit
	// characters: the text Render puts around a summary has a fixed size, so a
	// one-character summary measures it.
	atTheLimit := func() review.Answer {
		probe := review.Render(headSHA, review.Answer{Summary: "s"}, review.EventComment, "")

		return review.Answer{Summary: strings.Repeat("s", gitHubTextLimit-(utf8.RuneCountInString(probe.Body)-1))}
	}()

	cases := []struct {
		name          string
		answer        review.Answer
		diff          string
		wantTruncated bool
		wantAtLimit   bool
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
			name:          "a summary of characters outside the BMP is cut to GitHub's count",
			answer:        review.Answer{Summary: strings.Repeat("\U0001F600", 40_000)},
			wantTruncated: true,
		},
		{
			name:   "a normal answer is rendered whole",
			answer: review.Answer{Summary: renderSummary, Findings: []review.Finding{renderAddedLine, renderOutsideDiff, renderNoPosition}},
			diff:   renderDiff,
		},
		{
			name:        "a body exactly at the limit is rendered whole",
			answer:      atTheLimit,
			wantAtLimit: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.Render(headSHA, tc.answer, review.EventComment, tc.diff)

			if n := utf8.RuneCountInString(got.Body); n > gitHubTextLimit {
				t.Errorf("the review's body is %d characters, want at most %d: GitHub refuses a longer review, and the model call that wrote it is spent", n, gitHubTextLimit)
			}

			// GitHub counts a body in UTF-16 code units, so a character outside
			// the BMP counts twice: a body of 40,000 emoji is 80,000 there.
			if n := len(utf16.Encode([]rune(got.Body))); n > gitHubTextLimit {
				t.Errorf("the review's body is %d UTF-16 code units, want at most %d: GitHub counts a character outside the BMP as two, and refuses a longer review", n, gitHubTextLimit)
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

			if tc.wantAtLimit {
				if n := utf8.RuneCountInString(got.Body); n != gitHubTextLimit {
					t.Errorf("the review's body is %d characters, want exactly %d: an answer that fits GitHub's limit is rendered whole, not cut short of it", n, gitHubTextLimit)
				}
			}

			if tc.diff != "" && len(got.Comments) == 0 {
				t.Fatal("the review holds no inline comment, so no comment body is rendered: a finding of the answer is on a line the diff shows")
			}

			for _, comment := range got.Comments {
				if n := utf8.RuneCountInString(comment.Body); n > gitHubTextLimit {
					t.Errorf("the inline comment on %s:%d is %d characters, want at most %d: GitHub refuses a longer comment, and the model call that wrote it is spent", comment.Path, comment.Line, n, gitHubTextLimit)
				}

				if n := len(utf16.Encode([]rune(comment.Body))); n > gitHubTextLimit {
					t.Errorf("the inline comment on %s:%d is %d UTF-16 code units, want at most %d: GitHub counts a character outside the BMP as two, and refuses a longer comment", comment.Path, comment.Line, n, gitHubTextLimit)
				}
			}
		})
	}
}

// TestRenderClosesACodeFenceItCuts pins what a cut leaves a code block looking
// like. A summary that opens a ``` fence and is longer than GitHub accepts is
// cut inside the fence, and so is an inline comment whose finding does: left
// open, the fence would render the note that the text was truncated, and the
// body's closing statement that the review is advice and never an approval, as
// code, so Render closes the fence before them. A ~~~ fence opens a code block
// just the same, and a fence the text closed before the cut is closed already:
// Render adds no line to it.
func TestRenderClosesACodeFenceItCuts(t *testing.T) {
	t.Parallel()

	// fenceAndPatch opens a diff fence and fills the text past GitHub's limit, so
	// the cut lands inside the fence.
	fenceAndPatch := "```diff\n" + strings.Repeat("- removed a line\n+ added a line\n", 4_000)

	// prose fills the text past GitHub's limit with no fence in it, so the cut
	// lands well after anything a case puts before it.
	prose := strings.Repeat("A paragraph of ordinary prose.\n", 3_000)

	fencedFinding := review.Finding{
		Severity: review.SeveritySuggestion,
		Category: review.CategoryReadability,
		Path:     "internal/x/y.go",
		Line:     2,
		Title:    "The patch, at length",
		Body:     fenceAndPatch,
	}

	cases := []struct {
		name         string
		answer       review.Answer
		diff         string
		wantComments int
	}{
		{
			name:   "a body the cut left inside the summary's fence closes it",
			answer: review.Answer{Summary: fenceAndPatch},
		},
		{
			name:         "an inline comment the cut left inside its finding's fence closes it",
			answer:       review.Answer{Summary: renderSummary, Findings: []review.Finding{fencedFinding}},
			diff:         renderDiff,
			wantComments: 1,
		},
		{
			name:   "a body the cut left inside the summary's ~~~ fence closes it",
			answer: review.Answer{Summary: "~~~\n" + prose},
		},
		{
			name:   "a fence the summary closed before the cut gains no closer",
			answer: review.Answer{Summary: "```go\npackage x\n```\n" + prose},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.Render(headSHA, tc.answer, review.EventComment, tc.diff)

			if len(got.Comments) != tc.wantComments {
				t.Fatalf("the review holds %d inline comments, want %d: the finding is on a line the diff shows only when a comment is wanted", len(got.Comments), tc.wantComments)
			}

			if tc.wantComments == 0 {
				assertFenceClosedAfterCut(t, "the review's body", got.Body)

				return
			}

			assertFenceClosedAfterCut(t, fmt.Sprintf("the inline comment on %s:%d", got.Comments[0].Path, got.Comments[0].Line), got.Comments[0].Body)
		})
	}
}

// assertFenceClosedAfterCut pins what a text Render cut must leave: a note that
// it was truncated, an even number of lines that start a code fence (``` or
// ~~~), and that note and the body's closing statement after the last of those
// lines.
func assertFenceClosedAfterCut(t *testing.T, where, text string) {
	t.Helper()

	truncated := regexp.MustCompile(`(?i)truncated`)
	if !truncated.MatchString(text) {
		t.Fatalf("%s holds no note that it was truncated, so nothing was cut and no fence was left open; text:\n%s", where, text)
	}

	var (
		fences int
		lastAt int
		at     int
	)

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fences++
			lastAt = at
		}

		at += len(line) + 1
	}

	if fences%2 != 0 {
		t.Errorf("%s holds %d lines that start a code fence, want an even number: a fence the cut left open renders the rest of it as code; text:\n%s", where, fences, text)
	}

	if fences == 0 {
		return
	}

	if loc := truncated.FindStringIndex(text); loc != nil && loc[0] < lastAt {
		t.Errorf("%s puts the note that it was truncated before the last line of the code fence, so the fence renders the note as code; text:\n%s", where, text)
	}

	if idx := strings.Index(text, "This review is advice for the maintainer"); idx != -1 && idx < lastAt {
		t.Errorf("%s puts the review's closing statement before the last line of the code fence, so the fence renders it as code; text:\n%s", where, text)
	}
}

// TestRenderDefusesMentionsInTheModelsText pins what a mention in the model's
// text is allowed to do: notify nobody. The summary, the findings and a
// finding's path are text the model wrote, and a review that carries @someone
// would ping a user or a team of the repository, so Render defuses every
// mention (an @ followed by a letter or a digit) while the handle text itself
// stays readable (ADR 0033). A mention written as an HTML character reference
// is the same mention to GitHub, which renders the reference as the @ it names,
// so Render defuses those too.
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

	// Render refuses nothing, so a finding can reach it from somewhere other than
	// ParseAnswer: the path is text the model wrote too, and a line break in it
	// gives the rest of the path a line of its own, where an @ notifies.
	t.Run("a mention in a finding's path is defused too", func(t *testing.T) {
		t.Parallel()

		finding := review.Finding{
			Severity: review.SeverityNit,
			Category: review.CategoryReadability,
			Path:     "x.go\n\n@someone please look",
			Line:     99,
			Title:    "Unreachable branch",
			Body:     "Nothing reaches line 99.",
		}

		got := review.Render(headSHA, review.Answer{Summary: renderSummary, Findings: []review.Finding{finding}}, review.EventComment, renderDiff)

		if found := mention.FindString(got.Body); found != "" {
			t.Errorf("the review's body holds the mention %q, which the finding's path carries: Render defuses the path too; body:\n%s", found, got.Body)
		}

		if !strings.Contains(got.Body, "x.go") {
			t.Errorf("the review's body drops the path of the finding %q, which is on no line the diff shows, so the body lists it; body:\n%s", finding.Title, got.Body)
		}

		// The path stays on its one line: the line break the model wrote in it
		// would otherwise put the rest of the path on a line of its own, and the
		// finding's own text would read as if it were part of the path.
		var listed []string

		for _, line := range strings.Split(got.Body, "\n") {
			if strings.Contains(line, "x.go") {
				listed = append(listed, line)
			}
		}

		if len(listed) != 1 || !strings.Contains(listed[0], "please look") {
			t.Errorf("the review's body lists the path %q on %d lines (%q), want its whole text on one line: a listed path stays on its one line, with the control characters in it turned to spaces", finding.Path, len(listed), listed)
		}
	})

	// GitHub renders an HTML character reference in a body as the character it
	// names, so &#64;octocat, &#x40;octocat, &#X40;octocat, &#064;octocat and
	// &commat;octocat are all a live @octocat, and defusing the literal @ alone
	// notifies nobody only by luck.
	t.Run("a mention written as an HTML character reference is defused too", func(t *testing.T) {
		t.Parallel()

		reference := regexp.MustCompile(`(?i)&(#0*64|#x0*40|commat);[a-z0-9]`)
		references := []string{"&#64;octocat", "&#x40;octocat", "&#X40;octocat", "&#064;octocat", "&commat;octocat"}

		answer := review.Answer{
			Summary: "Thanks to " + strings.Join(references, ", ") + " for the report.",
			Findings: []review.Finding{{
				Severity: review.SeveritySuggestion,
				Category: review.CategoryReadability,
				Path:     "internal/x/y.go",
				Line:     2,
				Title:    "Credit &#64;octocat",
				Body:     "&#x40;octocat disagrees, and so does &commat;octocat.",
			}},
		}

		got := review.Render(headSHA, answer, review.EventComment, renderDiff)

		if len(got.Comments) != 1 {
			t.Fatalf("the review holds %d inline comments, want 1: the finding is on a line the diff shows", len(got.Comments))
		}

		for _, rendered := range []struct{ where, text string }{
			{where: "the review's body", text: got.Body},
			{where: "the inline comment on internal/x/y.go:2", text: got.Comments[0].Body},
		} {
			if found := reference.FindString(rendered.text); found != "" {
				t.Errorf("%s holds the character reference %q, which GitHub renders as a live @: text the model wrote cannot notify anybody; text:\n%s", rendered.where, found, rendered.text)
			}

			if !strings.Contains(rendered.text, "octocat") {
				t.Errorf("%s drops the handle text %q of the mention: the finding stays readable, only the @ goes; text:\n%s", rendered.where, "octocat", rendered.text)
			}
		}
	})

	// Only a character reference to an @ is a mention. Every other reference
	// names a character that notifies nobody, and escaping it would show the
	// reference as written where the model meant the character it names.
	t.Run("a character reference to anything but an @ is left as written", func(t *testing.T) {
		t.Parallel()

		written := []string{"&#65;", "&#x2603;", "&#38;", "&amp;", "&#0640;"}

		got := review.Render(headSHA, review.Answer{Summary: "The build prints " + strings.Join(written, ", ") + "."}, review.EventComment, "")

		for _, text := range written {
			if !strings.Contains(got.Body, text) {
				t.Errorf("the review's body does not hold %q as the model wrote it: only a reference to an @ is a mention, the others name a character that notifies nobody; body:\n%s", text, got.Body)
			}
		}
	})
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

// TestRenderCountsFindingsWithCorrectSingularAndPlural pins the summary's
// wording (issue #291): the event reason, the sentence about the findings that
// are inline comments and the sentence about the ones that follow in the body
// each say "1 finding" for one and "N findings" for more, so a review of one
// inline and one body finding never reads as if a finding went missing. The
// rendered body never says "1 findings".
func TestRenderCountsFindingsWithCorrectSingularAndPlural(t *testing.T) {
	t.Parallel()

	inline := func(title string, line int) review.Finding {
		return review.Finding{
			Severity: review.SeveritySuggestion,
			Category: review.CategoryReadability,
			Path:     "internal/x/y.go",
			Line:     line,
			Title:    title,
			Body:     "An inline finding, " + title + ".",
		}
	}

	blockingInline := func(title string, line int) review.Finding {
		finding := inline(title, line)
		finding.Severity = review.SeverityBlocker

		return finding
	}

	inBody := func(title string) review.Finding {
		return review.Finding{
			Severity: review.SeveritySuggestion,
			Category: review.CategoryCorrectness,
			Title:    title,
			Body:     "A finding in the body, " + title + ".",
		}
	}

	blockingInBody := func(title string) review.Finding {
		finding := inBody(title)
		finding.Severity = review.SeverityBlocker

		return finding
	}

	cases := []struct {
		name       string
		findings   []review.Finding
		event      review.Event
		want       []string
		wantAbsent []string
	}{
		{
			name:     "one finding, none blocking, inline only",
			findings: []review.Finding{inline("One", 2)},
			event:    review.EventComment,
			want:     []string{"1 finding, none blocking.", "1 finding is an inline comment on the diff."},
			wantAbsent: []string{
				"### Findings",
				"### Other findings",
				"not on a line of it",
			},
		},
		{
			name:     "two findings, none blocking, inline only",
			findings: []review.Finding{inline("One", 1), inline("Two", 2)},
			event:    review.EventComment,
			want:     []string{"2 findings, none blocking.", "2 findings are inline comments on the diff."},
			wantAbsent: []string{
				"### Findings",
				"### Other findings",
			},
		},
		{
			name:     "one inline finding and one in the body",
			findings: []review.Finding{inline("One", 2), inBody("Two")},
			event:    review.EventComment,
			want: []string{
				"2 findings, none blocking.",
				"### Other findings",
				"1 finding is an inline comment on the diff. The finding below is not on a line of it.",
			},
		},
		{
			name:     "two inline findings and three in the body",
			findings: []review.Finding{inline("One", 1), inline("Two", 2), inBody("Three"), inBody("Four"), inBody("Five")},
			event:    review.EventComment,
			want: []string{
				"5 findings, none blocking.",
				"### Other findings",
				"2 findings are inline comments on the diff. The 3 findings below are not on a line of it.",
			},
		},
		{
			name:     "the Other findings block as a whole",
			findings: []review.Finding{inline("One", 2), inBody("Two")},
			event:    review.EventComment,
			want: []string{
				"\n### Other findings\n\n1 finding is an inline comment on the diff. The finding below is not on a line of it.\n",
			},
		},
		{
			name:     "one inline finding and two in the body",
			findings: []review.Finding{inline("One", 2), inBody("Two"), inBody("Three")},
			event:    review.EventComment,
			want: []string{
				"1 finding is an inline comment on the diff. The 2 findings below are not on a line of it.",
			},
		},
		{
			name:     "two inline findings and one in the body",
			findings: []review.Finding{inline("One", 1), inline("Two", 2), inBody("Three")},
			event:    review.EventComment,
			want: []string{
				"2 findings are inline comments on the diff. The finding below is not on a line of it.",
			},
		},
		{
			name:     "one finding in the body only",
			findings: []review.Finding{inBody("One")},
			event:    review.EventComment,
			want:     []string{"1 finding, none blocking.", "### Findings"},
			wantAbsent: []string{
				"inline comment",
				"### Other findings",
			},
		},
		{
			name:     "three findings in the body only",
			findings: []review.Finding{inBody("One"), inBody("Two"), inBody("Three")},
			event:    review.EventComment,
			want:     []string{"3 findings, none blocking.", "### Findings"},
			wantAbsent: []string{
				"inline comment",
				"### Other findings",
			},
		},
		{
			name:     "one blocking finding with the switch on",
			findings: []review.Finding{blockingInline("One", 2)},
			event:    review.EventRequestChanges,
			want:     []string{"1 finding, 1 blocking: this review requests changes."},
		},
		{
			name:     "three findings, two blocking, with the switch on",
			findings: []review.Finding{blockingInline("One", 2), blockingInBody("Two"), inBody("Three")},
			event:    review.EventRequestChanges,
			want:     []string{"3 findings, 2 blocking: this review requests changes."},
		},
		{
			name:     "one blocking finding with the switch off",
			findings: []review.Finding{blockingInline("One", 2)},
			event:    review.EventComment,
			want:     []string{"1 finding, 1 blocking. Change requests are off"},
		},
		{
			name:     "three findings, two blocking, with the switch off",
			findings: []review.Finding{blockingInline("One", 2), blockingInBody("Two"), inBody("Three")},
			event:    review.EventComment,
			want:     []string{"3 findings, 2 blocking. Change requests are off"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.Render(headSHA, review.Answer{Summary: renderSummary, Findings: tc.findings}, tc.event, renderDiff)

			for _, want := range tc.want {
				if !strings.Contains(got.Body, want) {
					t.Errorf("the review's body does not hold %q; body:\n%s", want, got.Body)
				}
			}

			for _, absent := range tc.wantAbsent {
				if strings.Contains(got.Body, absent) {
					t.Errorf("the review's body holds %q, which does not belong to these findings; body:\n%s", absent, got.Body)
				}
			}

			if strings.Contains(got.Body, "1 findings") {
				t.Errorf("the review's body says %q: one finding is singular, so the summary never says \"1 findings\"; body:\n%s", "1 findings", got.Body)
			}
		})
	}
}

// TestRenderSaysAFindingIsUncategorised pins what a review says when the model
// left a category out or wrote one the reviewer does not know (issue #292): the
// finding is kept and shown as uncategorised, and the body says so on its own
// line right after the event's reason, once for one finding and in the plural
// for more. The other findings stay as they were, and a known category adds no
// such note.
func TestRenderSaysAFindingIsUncategorised(t *testing.T) {
	t.Parallel()

	uncategorised := func(title string) review.Finding {
		return review.Finding{
			Severity: review.SeveritySuggestion,
			Category: review.CategoryUncategorised,
			Title:    title,
			Body:     "The model gave no category this finding could take.",
		}
	}

	known := review.Finding{
		Severity: review.SeveritySuggestion,
		Category: review.CategoryReadability,
		Title:    "Name it",
		Body:     "A clearer name would help.",
	}

	cases := []struct {
		name       string
		findings   []review.Finding
		event      review.Event
		want       []string
		wantAbsent []string
	}{
		{
			name:     "one uncategorised finding",
			findings: []review.Finding{uncategorised("Unknown category")},
			event:    review.EventComment,
			want: []string{
				"none blocking.\n1 finding had a missing or unrecognised category and is shown as uncategorised.",
				"(uncategorised)",
				"Unknown category",
			},
		},
		{
			name:     "two uncategorised findings",
			findings: []review.Finding{uncategorised("First"), uncategorised("Second")},
			event:    review.EventComment,
			want: []string{
				"none blocking.\n2 findings had a missing or unrecognised category and are shown as uncategorised.",
			},
		},
		{
			name:     "uncategorised findings among known ones keep the known ones",
			findings: []review.Finding{known, uncategorised("First"), uncategorised("Second")},
			event:    review.EventComment,
			want: []string{
				"none blocking.\n2 findings had a missing or unrecognised category and are shown as uncategorised.",
				"Name it",
			},
		},
		{
			name: "a blocking uncategorised finding still requests changes",
			findings: []review.Finding{{
				Severity: review.SeverityBlocker,
				Category: review.CategoryUncategorised,
				Title:    "Off by one",
				Body:     "The loop ends one short.",
			}},
			event: review.EventRequestChanges,
			want: []string{
				"this review requests changes.\n1 finding had a missing or unrecognised category and is shown as uncategorised.",
				"blocking",
			},
		},
		{
			name: "a blocking uncategorised finding with the switch off says it would have requested changes",
			findings: []review.Finding{{
				Severity: review.SeverityBlocker,
				Category: review.CategoryUncategorised,
				Title:    "Off by one",
				Body:     "The loop ends one short.",
			}},
			event: review.EventComment,
			want: []string{
				"would have requested changes.\n1 finding had a missing or unrecognised category and is shown as uncategorised.",
			},
		},
		{
			name:       "every category is known",
			findings:   []review.Finding{known},
			event:      review.EventComment,
			wantAbsent: []string{"had a missing or unrecognised category", "uncategorised"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.Render(headSHA, review.Answer{Summary: renderSummary, Findings: tc.findings}, tc.event, renderDiff)

			for _, want := range tc.want {
				if !strings.Contains(got.Body, want) {
					t.Errorf("the review's body does not hold %q; body:\n%s", want, got.Body)
				}
			}

			for _, absent := range tc.wantAbsent {
				if strings.Contains(got.Body, absent) {
					t.Errorf("the review's body holds %q, which does not belong to these findings; body:\n%s", absent, got.Body)
				}
			}
		})
	}
}

// TestRenderRendersAMisspeltPromptInjectionAnswerAsPromptInjection pins that a
// category the model misspelt still reaches the review as prompt-injection
// (issue #292): the category is read by ParseAnswer, which normalises it, so the
// finding is shown as prompt-injection and blocking, and the body's note about a
// missing or unrecognised category, which would tell the reader the finding
// fell into no category and never blocks, is absent. A category that names no
// injection stays uncategorised, and keeps that note.
func TestRenderRendersAMisspeltPromptInjectionAnswerAsPromptInjection(t *testing.T) {
	t.Parallel()

	// The answer of one finding whose category the model wrote as raw.
	answerWith := func(raw string) string {
		return `{"summary":"` + renderSummary + `","findings":[` +
			`{"severity":"suggestion","category":"` + raw + `","path":"internal/x/y.go","line":3,` +
			`"title":"Instruction in the diff","body":"The added line tells the reviewer to approve the pull request."}]}`
	}

	promptInjection := []string{"**suggestion** (prompt-injection), **blocking**"}

	cases := []struct {
		name       string
		category   string
		want       []string
		wantAbsent []string
	}{
		{
			name:       "the category in another case",
			category:   "Prompt-Injection",
			want:       promptInjection,
			wantAbsent: []string{"had a missing or unrecognised category", "uncategorised"},
		},
		{
			name:       "the category with an underscore",
			category:   "prompt_injection",
			want:       promptInjection,
			wantAbsent: []string{"had a missing or unrecognised category", "uncategorised"},
		},
		{
			name:       "the category with a space",
			category:   "prompt injection",
			want:       promptInjection,
			wantAbsent: []string{"had a missing or unrecognised category", "uncategorised"},
		},
		{
			name:       "the category with no hyphen",
			category:   "promptinjection",
			want:       promptInjection,
			wantAbsent: []string{"had a missing or unrecognised category", "uncategorised"},
		},
		{
			name:       "a bare injection category",
			category:   "injection",
			want:       promptInjection,
			wantAbsent: []string{"had a missing or unrecognised category", "uncategorised"},
		},
		{
			name:       "an injection word in a longer category",
			category:   "Code Injection",
			want:       promptInjection,
			wantAbsent: []string{"had a missing or unrecognised category", "uncategorised"},
		},
		{
			name:       "a category that names no injection stays uncategorised",
			category:   "style",
			want:       []string{"**suggestion** (uncategorised)", "had a missing or unrecognised category"},
			wantAbsent: []string{"(prompt-injection)"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			answer, err := review.ParseAnswer(answerWith(tc.category))
			if err != nil {
				t.Fatalf("ParseAnswer of the category %q: %v", tc.category, err)
			}

			got := review.Render(headSHA, answer, review.EventComment, renderDiff)

			// The finding is on an added line of the diff, so the review makes an
			// inline comment of it: what the review says about it is the body
			// plus the comments.
			text := got.Body
			for _, comment := range got.Comments {
				text += "\n" + comment.Body
			}

			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("the review does not hold %q for the category %q; body:\n%s", want, tc.category, got.Body)
				}
			}

			for _, absent := range tc.wantAbsent {
				if strings.Contains(text, absent) {
					t.Errorf("the review holds %q for the category %q, which does not belong to it; body:\n%s", absent, tc.category, got.Body)
				}
			}
		})
	}
}

// TestRenderNamesTheFilesTheReviewDidNotRead pins the note the exclusion of
// issue #324 leaves the reader: the review names every file the reviewer left
// out of the diff, so the maintainer knows what the AI review did not read, and
// names it escaped as every other text of a review is (ADR 0033), because a
// file name is text a pull request chose: a mention in it notifies nobody, a
// backtick in it cannot end the code span it is shown in, and an HTML tag in it
// renders as text. A review that left nothing out has no such note, and its
// body is the body Render renders.
func TestRenderNamesTheFilesTheReviewDidNotRead(t *testing.T) {
	t.Parallel()

	mention := regexp.MustCompile(`@[A-Za-z0-9]`)

	// Each left-out file with the lines it adds (issue #342): the note shows the
	// count beside the path, so the maintainer knows how much of the change the
	// AI review did not read, not only that it did not read it.
	leftOut := review.LeftOut{Files: []review.LeftOutFile{
		{Path: "api/openapi.bundled.json", Added: 12},
		{Path: "testdata/comparison-v3.golden", Added: 3},
		{Path: "testdata/`code`.golden", Added: 1},
		{Path: "testdata/@octocat.golden", Added: 0},
		{Path: "testdata/<img src=x>.golden", Added: 2},
	}}

	answer := review.Answer{Summary: renderSummary, Findings: []review.Finding{renderAddedLine}}

	got := review.RenderFiltered(headSHA, answer, review.EventComment, renderDiff, leftOut)

	t.Run("every left-out file is named, escaped as the review escapes a path", func(t *testing.T) {
		t.Parallel()

		// A path is shown in a code span, as a finding's path in the body is,
		// with the backticks it holds turned into apostrophes.
		for _, want := range []string{
			"`api/openapi.bundled.json`",
			"`testdata/comparison-v3.golden`",
			"`testdata/'code'.golden`",
			"`testdata/<img src=x>.golden`",
		} {
			if !strings.Contains(got.Body, want) {
				t.Errorf("the review does not name %q among the files it did not read; body:\n%s", want, got.Body)
			}
		}

		// Every left-out file shows how many lines it adds (issue #342), the
		// count beside the path it belongs to, in the singular when it is one
		// and with no lines at all for a file the change only deletes.
		for _, want := range []string{
			"`api/openapi.bundled.json` (+12 lines)",
			"`testdata/comparison-v3.golden` (+3 lines)",
			"`testdata/'code'.golden` (+1 line)",
			"`testdata/<img src=x>.golden` (+2 lines)",
			"`testdata/@\u200boctocat.golden` (+0 lines)",
		} {
			if !strings.Contains(got.Body, want) {
				t.Errorf("the note does not show %q: every left-out file's added-line count sits beside its path, singular for one line (issue #342); body:\n%s", want, got.Body)
			}
		}

		if strings.Contains(got.Body, "testdata/`code`.golden") {
			t.Errorf("the review names the path testdata/`code`.golden with its backticks whole, which closes the code span it is shown in; body:\n%s", got.Body)
		}

		if found := mention.FindString(got.Body); found != "" {
			t.Errorf("the review's body holds the mention %q, which a left-out file's name carries: a file name notifies nobody either; body:\n%s", found, got.Body)
		}

		if !strings.Contains(got.Body, "octocat.golden") {
			t.Errorf("the review drops the handle text of a left-out file's name: the name stays readable, only the @ goes; body:\n%s", got.Body)
		}

		if !strings.Contains(strings.ToLower(got.Body), "not reviewed") {
			t.Errorf("the review's list of left-out files does not say they were not reviewed, so the reader cannot tell what the AI review skipped; body:\n%s", got.Body)
		}

		// The note is the body's, not a finding's: no finding of the answer is
		// on a left-out file, so every finding stays where it was.
		if len(got.Comments) != 1 {
			t.Errorf("the review holds %d inline comments, want 1: the note changes no finding", len(got.Comments))
		}
	})

	t.Run("a review that left nothing out has no note", func(t *testing.T) {
		t.Parallel()

		plain := review.RenderFiltered(headSHA, answer, review.EventComment, renderDiff, review.LeftOut{})

		if strings.Contains(strings.ToLower(plain.Body), "not reviewed") {
			t.Errorf("the review of a diff it read whole says a file was not reviewed; body:\n%s", plain.Body)
		}

		if diff := cmp.Diff(review.Render(headSHA, answer, review.EventComment, renderDiff), plain); diff != "" {
			t.Errorf("the review of a diff it read whole differs from Render's (-Render +RenderFiltered):\n%s", diff)
		}
	})

	// A left-out path is a name a pull request chose, so it is escaped as every
	// other text of a review is (ADR 0033), and the cut of issue #341 must not
	// undo that: wherever the cut lands relative to an @ and its handle, the
	// note holds no live @name. The cut comes after the escaping, so it can only
	// shorten a defused mention or reference, never revive one.
	t.Run("a cut path holds no live mention", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name string
			path string
		}{
			{
				name: "a mention inside a path the note does not cut",
				path: "testdata/@octocat.golden",
			},
			{
				name: "a mention right after the cut point, which the cut drops whole",
				path: strings.Repeat("a", listedPathUnits) + "@octocat.golden",
			},
			{
				name: "a mention that just fits before the ellipsis",
				path: strings.Repeat("a", 290) + "@octocat" + strings.Repeat("b", 20),
			},
			{
				name: "a cut right after an @, leaving no handle to notify",
				path: strings.Repeat("a", 291) + "@octocat" + strings.Repeat("b", 20),
			},
			{
				name: "an HTML character reference to an @ that fits before the ellipsis",
				path: strings.Repeat("a", 290) + "&#64;octocat" + strings.Repeat("b", 20),
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				cut := review.RenderFiltered(headSHA, answer, review.EventComment, "",
					review.LeftOut{Files: []review.LeftOutFile{{Path: tc.path, Added: 1}}})

				if found := mention.FindString(cut.Body); found != "" {
					t.Errorf("the note holds the live mention %q for the path %q: a path notifies nobody, and the cut does not undo the escaping (ADR 0033, issue #341); body:\n%s", found, tc.path, cut.Body)
				}

				if strings.Contains(cut.Body, "&#64;") {
					t.Errorf("the note holds the HTML character reference &#64; for the path %q, which GitHub renders as a live @: the & of the reference is escaped, and the cut does not undo it (issue #341); body:\n%s", tc.path, cut.Body)
				}

				if !utf8.ValidString(cut.Body) {
					t.Errorf("the note is not valid UTF-8 for the path %q: GitHub sends invalid input on as U+FFFD; body:\n%q", tc.path, cut.Body)
				}
			})
		}
	})

	// The note is the reader's only sign of what the AI review did not read, so
	// it is rendered outside the part Render cuts at GitHub's limit: a long
	// review loses findings, never the note (reviewer M1, auditor L2).
	t.Run("the note survives a body the limit cuts", func(t *testing.T) {
		t.Parallel()

		findings := make([]review.Finding, 0, 20)
		for i := range 20 {
			findings = append(findings, review.Finding{
				Severity: review.SeveritySuggestion,
				Category: review.CategoryReadability,
				Title:    fmt.Sprintf("Finding %d", i),
				Body:     strings.Repeat("b", 10_000),
			})
		}

		notRead := []review.LeftOutFile{
			{Path: "api/openapi.bundled.json", Added: 1_000},
			{Path: "internal/x/testdata/a.golden", Added: 300},
			{Path: "testdata/b.golden", Added: 0},
		}

		got := review.RenderFiltered(headSHA,
			review.Answer{Summary: renderSummary, Findings: findings}, review.EventComment, "",
			review.LeftOut{Files: notRead})

		if !strings.Contains(strings.ToLower(got.Body), "truncated") {
			t.Fatalf("the review's body says nothing of truncation, so nothing was cut and the test pins nothing: findings of %d characters each are over the limit; body length %d",
				len(findings[0].Body), utf8.RuneCountInString(got.Body))
		}

		if !strings.Contains(got.Body, "### Not reviewed") {
			t.Errorf("a review cut at GitHub's limit drops the note that names the files it did not read: the note is rendered outside the cut; body:\n%s", tail(got.Body, 2_000))
		}

		for _, file := range notRead {
			if !strings.Contains(got.Body, "`"+file.Path+"`") {
				t.Errorf("a review cut at GitHub's limit does not name the left-out file %q: the note is rendered outside the cut; body:\n%s", file.Path, tail(got.Body, 2_000))
			}
		}

		assertBodyWithinLimit(t, got.Body)
	})

	// A long list of left-out files must not push the body over GitHub's limit
	// either: the note names the first paths and says how many more there were,
	// and always states the total (reviewer M1, auditor L2). The number of paths
	// the note lists is the coder's constant (proposed name: MaxListedExcluded);
	// this test assumes 50.
	t.Run("a long list of left-out files is capped and still counted", func(t *testing.T) {
		t.Parallel()

		const (
			listed = 50
			total  = 5_000
		)

		many := make([]review.LeftOutFile, 0, total)
		for i := range total {
			many = append(many, review.LeftOutFile{Path: fmt.Sprintf("testdata/file-%04d.golden", i), Added: 1})
		}

		got := review.RenderFiltered(headSHA, answer, review.EventComment, renderDiff, review.LeftOut{Files: many})

		assertBodyWithinLimit(t, got.Body)

		if !strings.Contains(strings.ToLower(got.Body), "not reviewed") {
			t.Errorf("the review of %d left-out files has no note that names them; body:\n%s", total, tail(got.Body, 2_000))
		}

		// The note keeps the list's sorted order, so the same diff renders the
		// same note.
		previous := -1

		for i := range listed {
			path := "`" + fmt.Sprintf("testdata/file-%04d.golden", i) + "`"
			at := strings.Index(got.Body, path)

			if at < 0 {
				t.Fatalf("the note does not name %s, which is among the first %d of the %d left-out files; body:\n%s", path, listed, total, tail(got.Body, 2_000))
			}

			if at < previous {
				t.Errorf("the note names %s after a path that sorts after it: the list keeps its sorted order; body:\n%s", path, tail(got.Body, 2_000))
			}

			previous = at
		}

		for _, unlisted := range []string{"testdata/file-0050.golden", "testdata/file-4999.golden"} {
			if strings.Contains(got.Body, unlisted) {
				t.Errorf("the note names %q too: the list is capped, so the review stays under GitHub's limit whatever the pull request changes; body length %d",
					unlisted, utf8.RuneCountInString(got.Body))
			}
		}

		// The note says how many files it left out of the list, and the total
		// count, so a capped list never reads as the whole list.
		if !hasNumber(got.Body, total-listed) {
			t.Errorf("the note does not say how many of the %d left-out files it does not name (want %d); body:\n%s", total, total-listed, tail(got.Body, 2_000))
		}

		if !hasNumber(got.Body, total) {
			t.Errorf("the note does not state the total of %d left-out files; body:\n%s", total, tail(got.Body, 2_000))
		}

		// The cap takes its own room off what the review may hold; it never
		// costs the review itself.
		if !strings.Contains(got.Body, renderSummary) {
			t.Errorf("the review's body drops its summary: a capped note is no reason to cut the review; body:\n%s", tail(got.Body, 2_000))
		}
	})
}

// listedPathUnits is the cap ADR 0033 puts on each left-out path the "Not
// reviewed" note and the prompt's left_out_of_diff list write, in UTF-16 code
// units (issue #324): two for a character outside the BMP, one for any byte
// that is no valid UTF-8, which is sent as U+FFFD.
const listedPathUnits = 300

// TestTheListedPathsAreCutTo300Units pins the cut of issue #341 in both places a
// left-out path is written: the review's "Not reviewed" note and the prompt's
// left_out_of_diff list. A path of exactly the limit stays whole, and a longer
// one is cut to 300 UTF-16 code units and ends with an ellipsis, on a character
// boundary: a cut that counted bytes would let a multi-byte character pass the
// limit, one that cut inside a surrogate pair would send half a character (a
// lone surrogate, or U+FFFD) or split a rune, and one that counted a character
// outside the BMP as one unit would let 600 bytes through. A path may hold any
// byte, and an invalid one is sent as U+FFFD, as GitHub reads it and as
// utf16Len counts it, so the cut neither keeps invalid UTF-8 nor panics on it.
func TestTheListedPathsAreCutTo300Units(t *testing.T) {
	t.Parallel()

	const (
		// ellipsis ends a path the cut shortens.
		ellipsis = "…"
		// replacement is what an invalid byte is sent as.
		replacement = "\uFFFD"
	)

	cases := []struct {
		name string
		path string
		// want is the path as both places write it.
		want string
		// wantReplacement says the cut's output holds U+FFFD for an invalid
		// byte of the path, and no case without one may introduce it: a U+FFFD
		// in a case that has no invalid byte is the mark of a cut that split a
		// surrogate pair or a rune.
		wantReplacement bool
	}{
		{
			name: "a path below the limit is not cut",
			path: strings.Repeat("a", listedPathUnits-1),
			want: strings.Repeat("a", listedPathUnits-1),
		},
		{
			name: "a path of exactly the limit is not cut",
			path: strings.Repeat("a", listedPathUnits),
			want: strings.Repeat("a", listedPathUnits),
		},
		{
			name: "an ASCII path longer than the limit is cut and ends with an ellipsis",
			path: strings.Repeat("a", listedPathUnits+1),
			want: strings.Repeat("a", listedPathUnits-1) + ellipsis,
		},
		{
			// U+1F600 is a surrogate pair: two UTF-16 code units. The cut keeps
			// what fits under the limit with room for the ellipsis, so the pair
			// is dropped whole, never cut in half.
			name: "an astral character at the boundary is never split",
			path: strings.Repeat("a", 298) + "😀" + strings.Repeat("b", 10),
			want: strings.Repeat("a", 298) + ellipsis,
		},
		{
			// "é" is two bytes and "€" three, each one UTF-16 code unit: a cut
			// that counted bytes would stop inside the € and send invalid UTF-8.
			name: "multi-byte runes at the boundary are cut on a character boundary",
			path: strings.Repeat("a", 297) + "é" + "€" + strings.Repeat("b", 10),
			want: strings.Repeat("a", 297) + "é" + "€" + ellipsis,
		},
		{
			name: "a multi-byte path of exactly the limit is not cut",
			path: strings.Repeat("a", 298) + "é" + "€",
			want: strings.Repeat("a", 298) + "é" + "€",
		},
		{
			name:            "invalid UTF-8 at the boundary is sent as U+FFFD",
			path:            strings.Repeat("a", 290) + "\xff" + strings.Repeat("b", 20),
			want:            strings.Repeat("a", 290) + replacement + strings.Repeat("b", 8) + ellipsis,
			wantReplacement: true,
		},
		{
			name:            "a path of invalid UTF-8 only is cut and stays valid UTF-8",
			path:            strings.Repeat("\xff", listedPathUnits+1),
			want:            strings.Repeat(replacement, listedPathUnits-1) + ellipsis,
			wantReplacement: true,
		},
	}

	answer := review.Answer{Summary: renderSummary}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The note of the review the program posts: the path in a code span,
			// then the lines it adds.
			note := review.RenderFiltered(headSHA, answer, review.EventComment, "",
				review.LeftOut{Files: []review.LeftOutFile{{Path: tc.path, Added: 1}}})

			want := "- `" + tc.want + "` (+1 line)"
			if !strings.Contains(note.Body, want) {
				t.Errorf("the note holds no %q for the path %q: each left-out path is cut to %d UTF-16 code units and ends with an ellipsis when longer (issue #341); body:\n%q",
					want, tc.path, listedPathUnits, note.Body)
			}

			if !utf8.ValidString(note.Body) {
				t.Errorf("the note is not valid UTF-8 for the path %q: invalid input is sent as U+FFFD, and no cut splits a rune (issue #341); body:\n%q", tc.path, note.Body)
			}

			if !tc.wantReplacement && strings.Contains(note.Body, replacement) {
				t.Errorf("the note holds U+FFFD for the path %q, which holds no invalid byte: the cut split a surrogate pair or a rune (issue #341); body:\n%q", tc.path, note.Body)
			}

			if units := utf16Units(tc.want); units > listedPathUnits {
				t.Errorf("the test's own expectation for the path %q is %d UTF-16 code units, over the %d of ADR 0033", tc.path, units, listedPathUnits)
			}

			// The prompt's list of left-out paths: the same cut, as the model
			// reads it in the untrusted document.
			prompt := review.BuildPrompt(review.Instructions{}, review.PullRequest{Number: 7}, nil, "", []string{tc.path})

			if !utf8.ValidString(prompt.User) {
				t.Errorf("the prompt is not valid UTF-8 for the path %q: the untrusted document is JSON, which sends invalid input as U+FFFD (issue #341); user message:\n%q", tc.path, prompt.User)
			}

			if diff := cmp.Diff([]string{tc.want}, leftOutOfPrompt(t, prompt.User)); diff != "" {
				t.Errorf("the prompt's left_out_of_diff holds another path than the note does for %q (-want +got):\n%s", tc.path, diff)
			}
		})
	}
}

// utf16Units is n in UTF-16 code units, as GitHub counts a text: two for a
// character outside the BMP, one for any other.
func utf16Units(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// assertBodyWithinLimit fails when body is over MaxTextUnits UTF-16 code units,
// which is how GitHub counts a review's body.
func assertBodyWithinLimit(t *testing.T, body string) {
	t.Helper()

	if n := len(utf16.Encode([]rune(body))); n > review.MaxTextUnits {
		t.Errorf("the review's body is %d UTF-16 code units, want at most %d: GitHub refuses a longer review", n, review.MaxTextUnits)
	}

	if n := utf8.RuneCountInString(body); n > review.MaxTextUnits {
		t.Errorf("the review's body is %d characters, want at most %d: GitHub refuses a longer review", n, review.MaxTextUnits)
	}
}

// hasNumber reports whether text holds n, written with or without thousands
// separators.
func hasNumber(text string, n int) bool {
	plain := strconv.Itoa(n)

	if strings.Contains(text, plain) {
		return true
	}

	var grouped strings.Builder

	for i, r := range plain {
		if i > 0 && (len(plain)-i)%3 == 0 {
			grouped.WriteByte(',')
		}

		grouped.WriteRune(r)
	}

	return strings.Contains(text, grouped.String())
}

// tail returns the last n characters of text, for an error message about a body
// too long to print whole.
func tail(text string, n int) string {
	if len(text) <= n {
		return text
	}

	return "..." + text[len(text)-n:]
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
