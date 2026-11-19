package review_test

import (
	"fmt"
	"regexp"
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

	excluded := []string{
		"api/openapi.bundled.json",
		"testdata/comparison-v3.golden",
		"testdata/`code`.golden",
		"testdata/@octocat.golden",
		"testdata/<img src=x>.golden",
	}

	answer := review.Answer{Summary: renderSummary, Findings: []review.Finding{renderAddedLine}}

	got := review.RenderFiltered(headSHA, answer, review.EventComment, renderDiff, excluded)

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

		plain := review.RenderFiltered(headSHA, answer, review.EventComment, renderDiff, nil)

		if strings.Contains(strings.ToLower(plain.Body), "not reviewed") {
			t.Errorf("the review of a diff it read whole says a file was not reviewed; body:\n%s", plain.Body)
		}

		if diff := cmp.Diff(review.Render(headSHA, answer, review.EventComment, renderDiff), plain); diff != "" {
			t.Errorf("the review of a diff it read whole differs from Render's (-Render +RenderFiltered):\n%s", diff)
		}
	})
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
