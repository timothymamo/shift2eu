package review_test

import (
	"strings"
	"testing"

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
