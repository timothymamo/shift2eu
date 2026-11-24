package publish_test

// The core of PR 4b's report side (PRD #175 milestone 2, "Report and triage";
// ADR 0040): the previous report's findings table as the issues' planner reads
// it, and the one issue a merged public finding is filed as. The issue holds
// the finding's location, scenario and fix and never its evidence, and every
// value the findings table's vocabulary does not name is refused.

import (
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// issueFinding returns a previous finding of the findings table, as
// PublicFindings reads one.
func issueFinding() publish.PreviousFinding {
	return publish.PreviousFinding{
		Fingerprint: "fp-0123456789ab",
		Severity:    "low",
		Category:    "hardening",
		Status:      "new",
		Location:    "internal/cli/run.go:12",
		Scenario:    "the scenario of the finding",
		Fix:         "the fix of the finding",
		Report:      "docs/security/audits/2026-11-01-1.md",
	}
}

// issueBodyText is the body RenderIssue must write, built from the finding it
// is rendered for and the run's URL.
func issueBodyText(f publish.PreviousFinding, run publish.Run) string {
	return "Security audit finding " + string(f.Fingerprint) + ", " + f.Status + " in the scheduled security audit's report.\n" +
		"\n" +
		"Severity: " + f.Severity + "\n" +
		"Category: " + f.Category + "\n" +
		"\n" +
		"Location:\n" +
		"\n" + publish.CodeBlock(f.Location) + "\n" +
		"\n" +
		"Scenario:\n" +
		"\n" + publish.CodeBlock(f.Scenario) + "\n" +
		"\n" +
		"Recommended fix:\n" +
		"\n" + publish.CodeBlock(f.Fix) + "\n" +
		"\n" +
		"Report: " + f.Report + "\n" +
		"Run: " + run.URL() + "\n"
}

// TestRenderIssue pins one issue of a merged public finding: its title names
// the fingerprint, the category and the severity, and its body is the fixed
// text of the finding's three model cells, its report's path and the run's URL
// - never the finding's evidence.
func TestRenderIssue(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"new", "open"} {
		t.Run("a "+status+" finding", func(t *testing.T) {
			t.Parallel()

			f := issueFinding()
			f.Status = status

			run := testRun(t, testDate, testRunID)

			issue, err := publish.RenderIssue(f, run)
			if err != nil {
				t.Fatalf("RenderIssue = %v, want no error", err)
			}

			if want := "Security audit finding fp-0123456789ab: hardening (low)"; issue.Title != want {
				t.Errorf("the issue's title is %q, want %q", issue.Title, want)
			}

			if diff := cmp.Diff(issueBodyText(f, run), issue.Body); diff != "" {
				t.Errorf("the issue's body (-want +got):\n%s", diff)
			}

			wantFields := []string{
				publish.CodeBlock(f.Location),
				publish.CodeBlock(f.Scenario),
				publish.CodeBlock(f.Fix),
			}

			if diff := cmp.Diff(wantFields, issue.ModelFields); diff != "" {
				t.Errorf("the issue's model fields (-want +got):\n%s", diff)
			}

			if issue.Body != strings.TrimSuffix(issue.Body, "\n")+"\n" {
				t.Errorf("the issue's body does not end with one LF: %q", issue.Body)
			}

			for _, hidden := range []string{"the evidence", "Evidence:", "| Fingerprint |"} {
				if strings.Contains(issue.Body, hidden) || strings.Contains(issue.Title, hidden) {
					t.Errorf("the issue holds %q, want the public fields alone", hidden)
				}
			}
		})
	}
}

// TestRenderIssueRefuses pins the issue's refusals: a fingerprint, a severity,
// a category or a status outside the findings table's vocabulary is no previous
// finding, and a run NewRun refuses is no configuration.
func TestRenderIssueRefuses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		change  func(*publish.PreviousFinding)
		run     func(t *testing.T) publish.Run
		wantErr error
	}{
		{name: "a fingerprint that is no fingerprint", change: func(f *publish.PreviousFinding) { f.Fingerprint = "fp-0123" }, wantErr: publish.ErrPreviousReport},
		{name: "a severity outside the public ones", change: func(f *publish.PreviousFinding) { f.Severity = sevMedium }, wantErr: publish.ErrPreviousReport},
		{name: "a category outside the public ones", change: func(f *publish.PreviousFinding) { f.Category = catInjection }, wantErr: publish.ErrPreviousReport},
		{name: "a status the table fixed", change: func(f *publish.PreviousFinding) { f.Status = "fixed" }, wantErr: publish.ErrPreviousReport},
		{name: "a status the table does not name", change: func(f *publish.PreviousFinding) { f.Status = "closed" }, wantErr: publish.ErrPreviousReport},
		{
			name: "a run NewRun refuses",
			run: func(*testing.T) publish.Run {
				return publish.Run{ServerURL: "http://github.com", Repository: testRepository, ID: testRunID, Date: testDate}
			},
			wantErr: publish.ErrConfig,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := issueFinding()

			if tc.change != nil {
				tc.change(&f)
			}

			run := testRun(t, testDate, testRunID)
			if tc.run != nil {
				run = tc.run(t)
			}

			issue, err := publish.RenderIssue(f, run)

			switch {
			case tc.wantErr == nil:
				if err != nil {
					t.Errorf("RenderIssue = %v, want no error", err)
				}
			case !errors.Is(err, tc.wantErr):
				t.Errorf("RenderIssue = %v, want an error wrapping %v", err, tc.wantErr)
			}

			if tc.wantErr != nil && (issue.Title != "" || issue.Body != "" || issue.ModelFields != nil) {
				t.Errorf("RenderIssue returned %+v beside its error, want nothing", issue)
			}
		})
	}
}

// TestPublicFindingsReadsTheTableBack pins the previous report's findings table
// as the planner reads it: every row in the table's order, a fixed row with no
// model text, its cells unescaped, and the report the row was read from.
func TestPublicFindingsReadsTheTableBack(t *testing.T) {
	t.Parallel()

	open := finding("G301", sevLow, true, catHardening)
	open.Scenario = "a scenario with a | pipe in it"
	open.Fix = "a fix with a `backtick` in it"

	fixed := finding("G302", sevInformational, true, catConfiguration)

	first := previousReport(t, fixtureResult(open, fixed), publish.PrivateNone, "2026-12-01", "1")

	second := renderAndParse(t, publish.ReportInput{
		Result:   fixtureResult(open),
		Run:      testRun(t, "2026-12-02", "2"),
		Private:  publish.PrivateNone,
		Previous: first,
	})

	want := []publish.PreviousFinding{
		{
			Fingerprint: publish.PlainFingerprint(open.Rule, open.File, open.Sink),
			Severity:    sevLow,
			Category:    catHardening,
			Status:      "open",
			Location:    open.File + ":12",
			Scenario:    open.Scenario,
			Fix:         open.Fix,
			Report:      "docs/security/audits/2026-12-02-2.md",
		},
		{
			Fingerprint: publish.PlainFingerprint(fixed.Rule, fixed.File, fixed.Sink),
			Severity:    sevInformational,
			Category:    catConfiguration,
			Status:      "fixed",
			Report:      "docs/security/audits/2026-12-02-2.md",
		},
	}

	if diff := cmp.Diff(want, second.PublicFindings()); diff != "" {
		t.Errorf("the public findings (-want +got):\n%s", diff)
	}
}

// TestPublicFindingsOfNoFinding pins the empty answers: no previous report and
// a previous report with no public finding.
func TestPublicFindingsOfNoFinding(t *testing.T) {
	t.Parallel()

	var noPrevious *publish.Previous
	if findings := noPrevious.PublicFindings(); findings != nil {
		t.Errorf("PublicFindings(nil) = %+v, want nothing", findings)
	}

	empty := previousReport(t, fixtureResult(), publish.PrivateNone, "2026-12-01", "1")
	if findings := empty.PublicFindings(); findings != nil {
		t.Errorf("a report with no public finding = %+v, want nothing", findings)
	}
}

// TestPublicFindingsReadsTheNoneCellAsEmpty pins the tolerated cell: a cell
// the core takes but does not write, none, is no model text.
func TestPublicFindingsReadsTheNoneCellAsEmpty(t *testing.T) {
	t.Parallel()

	open := finding("G301", sevLow, true, catHardening)

	rendered := renderReport(t, publish.ReportInput{
		Result:  fixtureResult(open),
		Run:     testRun(t, "2026-12-01", "1"),
		Private: publish.PrivateNone,
	})

	body := strings.Replace(rendered.Body, publish.CodeSpan(open.File+":12"), "none", 1)
	if body == rendered.Body {
		t.Fatalf("the report holds no location cell to replace:\n%s", rendered.Body)
	}

	previous, err := publish.ParsePreviousReport(path.Base(rendered.Path), []byte(body))
	if err != nil {
		t.Fatalf("ParsePreviousReport on the report with a none cell: %v", err)
	}

	findings := previous.PublicFindings()
	if len(findings) != 1 {
		t.Fatalf("the previous report holds %d finding(s), want 1", len(findings))
	}

	if findings[0].Location != "" {
		t.Errorf("the none cell read as %q, want the empty text", findings[0].Location)
	}
}
