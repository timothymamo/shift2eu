package publish_test

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// finding builds a valid finding of the tree review, with a rule of its own so
// every finding of a fixture has a fingerprint of its own.
func finding(rule string, severity string, confirmed bool, category string) publish.Finding {
	return publish.Finding{
		Check:     "tree-review",
		Rule:      rule,
		File:      "internal/cli/run.go",
		Line:      12,
		Sink:      "os.Open",
		Severity:  severity,
		Confirmed: confirmed,
		Category:  category,
		Scenario:  "a scenario",
		Fix:       "a fix",
	}
}

// fingerprintOf returns the plain fingerprint of f: the one every public and
// needs-investigation finding carries.
func fingerprintOf(f publish.Finding) string {
	return string(publish.PlainFingerprint(f.Rule, f.File, f.Sink))
}

// renderAndParse renders one run's report and reads it back as the previous
// report a later run derives from: the core's own round trip, never a report
// written by hand.
func renderAndParse(t *testing.T, in publish.ReportInput) *publish.Previous {
	t.Helper()

	rendered := renderReport(t, in)

	previous, err := publish.ParsePreviousReport(path.Base(rendered.Path), []byte(rendered.Body))
	if err != nil {
		t.Fatalf("ParsePreviousReport(%q) on the report the core rendered: %v", path.Base(rendered.Path), err)
	}

	return &previous
}

// previousReport renders result as the first report of the given run and reads
// it back.
func previousReport(t *testing.T, result publish.Result, private publish.PrivateState, date, id string) *publish.Previous {
	t.Helper()

	return renderAndParse(t, publish.ReportInput{
		Result:  result,
		Run:     testRun(t, date, id),
		Private: private,
	})
}

// TestParsePreviousReportRoundTrips pins that a report the core rendered is a
// report the core reads back: one with no finding, one with a public finding, a
// suspected one and a false positive, and one with a private finding.
func TestParsePreviousReportRoundTrips(t *testing.T) {
	t.Parallel()

	fix := finding("G304", sevLow, true, catHardening)
	fix.Dismissal = "a test fixture, not a finding"

	suspected := finding("G305", sevLow, false, catDenialOfSvc)

	secret := publish.Finding{
		Check: "history-secrets", Rule: "gitleaks:generic-api-key", File: testCommit,
		Sink: "history", Severity: sevCritical, Category: "credential exposure",
		Scenario: "a history secret", Fix: "rotate it",
	}

	tests := []struct {
		name    string
		result  publish.Result
		private publish.PrivateState
	}{
		{"the first report, with no finding", fixtureResult(), publish.PrivateNone},
		{"a public and a suspected finding, and a false positive", fixtureResult(fix, suspected), publish.PrivateNone},
		{"a private finding", fixtureResult(fix, secret), publish.PrivateHandled},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rendered := renderReport(t, publish.ReportInput{
				Result:  tc.result,
				Run:     testRun(t, testDate, testRunID),
				Private: tc.private,
			})

			name := path.Base(rendered.Path)

			previous, err := publish.ParsePreviousReport(name, []byte(rendered.Body))
			if err != nil {
				t.Errorf("ParsePreviousReport(%q) on the report the core rendered: %v", name, err)
			}

			// The name is one a later run reads as its newest report, and what
			// it reads back is a previous report that run renders from.
			if newest, ok := publish.NewestReport([]string{name, "TEMPLATE.md"}); !ok || newest != name {
				t.Errorf("NewestReport([%q, TEMPLATE.md]) = %q, %t, want %q", name, newest, ok, name)
			}

			next := renderReport(t, publish.ReportInput{
				Result:   fixtureResult(),
				Run:      testRun(t, "2026-12-03", "18422000002"),
				Private:  publish.PrivateNone,
				Previous: &previous,
			})

			if changes := sectionText(t, next.Body, "Changes since the last audit"); strings.Contains(changes, "First report.") {
				t.Errorf("a run that has a previous report reads First report. in Changes, want the previous report's lines:\n%s", changes)
			}
		})
	}
}

// TestParsePreviousReportRefuses pins fail-closed reading: a report the core
// cannot read exactly as it renders one, however it was tampered with, is an
// error wrapping ErrPreviousReport, and the run stops rather than forget its
// history.
func TestParsePreviousReportRefuses(t *testing.T) {
	t.Parallel()

	fix := finding("G304", sevLow, true, catHardening)
	fix.Dismissal = "a test fixture, not a finding"

	suspected := finding("G305", sevLow, false, catDenialOfSvc)

	rendered := renderReport(t, publish.ReportInput{
		Result:  fixtureResult(fix, suspected),
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateNone,
	})

	body := rendered.Body
	name := path.Base(rendered.Path)

	tests := []struct {
		name string
		file string
		data func(*testing.T) []byte
	}{
		{
			name: "no data",
			file: name,
			data: func(*testing.T) []byte { return nil },
		},
		{
			name: "a page that is no report",
			file: name,
			data: func(*testing.T) []byte { return []byte("# Something else\n\nNo table here.\n") },
		},
		{
			name: "a name that is no report name",
			file: "TEMPLATE.md",
			data: func(*testing.T) []byte { return []byte(body) },
		},
		{
			// fp-0123456789ab is a well-formed fingerprint, so this row is
			// refused by the first report's own consistency rule: a
			// fingerprint that is none of the report's findings.
			name: "a fingerprint swapped for another",
			file: name,
			data: tampered(body, fingerprintOf(fix), "fp-0123456789ab"),
		},
		{
			// The suspected finding's cell is refused by the shape rule
			// alone: its row is in no other table of the report.
			name: "a fingerprint cell that is not a fingerprint",
			file: name,
			data: tampered(body, fingerprintOf(suspected), "fp-0123456789zz"),
		},
		{
			// The cell holds a tag character: invisible to the maintainer who
			// reviews the report, so it is no table cell the core may carry.
			name: "a model cell holding an invisible character",
			file: name,
			data: tampered(body, "` a fix `", "` a\uE0041 fix `"),
		},
		{
			// The cell holds a no-break space: it renders as the space beside
			// it, so a model can spell a token in which blank it picked.
			name: "a model cell holding a no-break space",
			file: name,
			data: tampered(body, "` a fix `", "` a\u00A0 fix `"),
		},
		{
			// The cell holds a Cyrillic a: it renders as the Latin a beside it,
			// so a model can spell a token in which alphabet it picked.
			name: "a model cell holding a lookalike letter",
			file: name,
			data: tampered(body, "` a fix `", "` a\u0430 fix `"),
		},
		{
			// The cell holds a combining acute accent: a mark with no glyph of
			// its own, so the maintainer who reviews the report reads a plain a.
			name: "a model cell holding a combining mark",
			file: name,
			data: tampered(body, "` a fix `", "` a\u0301 fix `"),
		},
		{
			// The cell holds a right single quotation mark: it renders as the
			// ASCII ' beside it, and the two are different bytes.
			name: "a model cell holding a typographic apostrophe",
			file: name,
			data: tampered(body, "` a fix `", "` a\u2019 fix `"),
		},
		{
			name: "a status cell outside the vocabulary",
			file: name,
			data: tampered(body, "| new |", "| gone |"),
		},
		{
			name: "a severity cell outside the vocabulary",
			file: name,
			data: tampered(body, "| low |", "| urgent |"),
		},
		{
			name: "a row with a cell too many",
			file: name,
			data: tampered(body, "| new |", "| new | extra |"),
		},
		{
			name: "a private-findings line outside the vocabulary",
			file: name,
			data: tampered(body, "private findings: none", "private findings: some"),
		},
		{
			name: "a heading renamed",
			file: name,
			data: tampered(body, "## Findings", "## Findings and more"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			previous, err := publish.ParsePreviousReport(tc.file, tc.data(t))
			if err == nil {
				t.Fatalf("ParsePreviousReport(%q) returned %+v, want an error wrapping ErrPreviousReport", tc.file, previous)
			}

			if !errors.Is(err, publish.ErrPreviousReport) {
				t.Errorf("ParsePreviousReport(%q): error %v does not wrap ErrPreviousReport", tc.file, err)
			}
		})
	}
}

// TestParsePreviousReportFirstReportIsAlone pins the Changes section's
// first-report rule: First report. is the section's only line before the
// Dependencies line, so a first report carrying another line, or carrying it
// after the dependencies, is refused.
func TestParsePreviousReportFirstReportIsAlone(t *testing.T) {
	t.Parallel()

	// A first report with no dependency, so the section reads First report. and
	// then the dependencies' own line.
	first := fixtureResult()
	first.Changes.Dependencies = nil

	rendered := renderReport(t, publish.ReportInput{
		Result:  first,
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateNone,
	})

	body := rendered.Body
	name := path.Base(rendered.Path)

	tests := []struct {
		name string
		data func(*testing.T) []byte
	}{
		{
			name: "First report. among other lines",
			data: tampered(body, "First report.\n", "First report.\n\n- fp-0123456789ab: still open\n"),
		},
		{
			name: "First report. after the dependencies",
			data: tampered(body,
				"First report.\n\nDependencies changed since the last audit: none.",
				"Dependencies changed since the last audit: none.\n\nFirst report."),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			previous, err := publish.ParsePreviousReport(name, tc.data(t))
			if err == nil {
				t.Fatalf("ParsePreviousReport returned %+v, want an error wrapping ErrPreviousReport", previous)
			}

			if !errors.Is(err, publish.ErrPreviousReport) {
				t.Errorf("ParsePreviousReport: error %v does not wrap ErrPreviousReport", err)
			}
		})
	}
}

// tampered returns a reader of a rendered report with one substring replaced,
// and fails the test when the substring is not there: a row that changes
// nothing would pass whatever the core did.
func tampered(body, old, replaced string) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()

		if !strings.Contains(body, old) {
			t.Fatalf("the rendered report does not hold %q, so this row cannot tamper with it:\n%s", old, body)
		}

		return []byte(strings.Replace(body, old, replaced, 1))
	}
}

// TestNewestReport pins which report a run reads: the newest name in
// docs/security/audits that matches YYYY-MM-DD-<run_id>.md, ordered by its date
// and then by its run ID as a number, and no other file of the directory.
func TestNewestReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names []string
		want  string
		ok    bool
	}{
		{
			name:  "no name",
			names: nil,
		},
		{
			name:  "the directory holds the template and a page only",
			names: []string{"TEMPLATE.md", "README.md"},
		},
		{
			name:  "one report",
			names: []string{"2026-11-03-18422000001.md"},
			want:  "2026-11-03-18422000001.md",
			ok:    true,
		},
		{
			name:  "the newest date wins",
			names: []string{"2026-10-03-18422000009.md", "2026-11-03-18422000001.md", "2026-09-03-18422000010.md"},
			want:  "2026-11-03-18422000001.md",
			ok:    true,
		},
		{
			name:  "one date, the run ID read as a number",
			names: []string{"2026-11-03-9.md", "2026-11-03-10.md", "2026-11-03-100.md"},
			want:  "2026-11-03-100.md",
			ok:    true,
		},
		{
			name:  "a run ID of 20 digits",
			names: []string{"2026-11-03-99999999999999999999.md", "2026-11-03-18422000001.md"},
			want:  "2026-11-03-99999999999999999999.md",
			ok:    true,
		},
		{
			name:  "names the pattern does not match are ignored",
			names: []string{"2026-11-3-1.md", "2026-11-03-01.md", "2026-11-03-0.md", "2026-11-03-1.md.bak", "2026-11-03-1.MD", "audit.md"},
		},
		{
			name:  "the date is ordered as the pattern's digits",
			names: []string{"2026-13-45-1.md", "2026-01-01-2.md"},
			want:  "2026-13-45-1.md",
			ok:    true,
		},
		{
			name:  "a run ID of 21 digits does not match the pattern",
			names: []string{"2026-11-03-123456789012345678901.md", "2026-11-03-1.md"},
			want:  "2026-11-03-1.md",
			ok:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := publish.NewestReport(tc.names)
			if got != tc.want || ok != tc.ok {
				t.Errorf("NewestReport(%v) = %q, %t, want %q, %t", tc.names, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestReportStatus pins the status each kept finding gets from the previous
// report, and the Changes line that goes with it: new when the previous report
// did not list it, open (still open) when that report listed it as new or open,
// open (regressed) when it listed it as fixed, and a fixed row with its
// fingerprint, severity, category and status when it listed the finding and
// this run does not find it.
func TestReportStatus(t *testing.T) {
	t.Parallel()

	always := finding("G304", sevLow, true, catHardening)
	suspected := finding("G305", sevLow, false, catDenialOfSvc)

	t.Run("new when the previous report did not list it", func(t *testing.T) {
		t.Parallel()

		body := renderReport(t, publish.ReportInput{
			Result:  fixtureResult(always),
			Run:     testRun(t, testDate, testRunID),
			Private: publish.PrivateNone,
		}).Body

		if row := rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(always)); row[3] != "new" {
			t.Errorf("the finding's status is %q, want new", row[3])
		}
	})

	t.Run("still open when the previous report listed it as new", func(t *testing.T) {
		t.Parallel()

		previous := previousReport(t, fixtureResult(always), publish.PrivateNone, "2026-10-03", "18422000001")

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(always),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: previous,
		}).Body

		if row := rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(always)); row[3] != "open" {
			t.Errorf("the finding's status is %q, want open", row[3])
		}

		if line := lineHolding(t, sectionText(t, body, "Changes since the last audit"), fingerprintOf(always)); !strings.Contains(line, "still open") {
			t.Errorf("the Changes line %q does not say still open", line)
		}
	})

	t.Run("still open when the previous report listed it as open", func(t *testing.T) {
		t.Parallel()

		first := previousReport(t, fixtureResult(always), publish.PrivateNone, "2026-09-03", "18422000001")
		second := renderAndParse(t, publish.ReportInput{
			Result:   fixtureResult(always),
			Run:      testRun(t, "2026-10-03", "18422000002"),
			Private:  publish.PrivateNone,
			Previous: first,
		})

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(always),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: second,
		}).Body

		if row := rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(always)); row[3] != "open" {
			t.Errorf("the finding's status is %q, want open", row[3])
		}

		if line := lineHolding(t, sectionText(t, body, "Changes since the last audit"), fingerprintOf(always)); !strings.Contains(line, "still open") {
			t.Errorf("the Changes line %q does not say still open", line)
		}
	})

	t.Run("regressed when the previous report listed it as fixed", func(t *testing.T) {
		t.Parallel()

		// The first report holds the finding, the second one does not, so the
		// second writes a fixed row, and the third finds the finding again.
		first := previousReport(t, fixtureResult(always), publish.PrivateNone, "2026-09-03", "18422000001")
		second := renderAndParse(t, publish.ReportInput{
			Result:   fixtureResult(),
			Run:      testRun(t, "2026-10-03", "18422000002"),
			Private:  publish.PrivateNone,
			Previous: first,
		})

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(always),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: second,
		}).Body

		if row := rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(always)); row[3] != "open" {
			t.Errorf("the finding's status is %q, want open", row[3])
		}

		if line := lineHolding(t, sectionText(t, body, "Changes since the last audit"), fingerprintOf(always)); !strings.Contains(line, "regressed") {
			t.Errorf("the Changes line %q does not say regressed", line)
		}
	})

	t.Run("a fixed row when the previous report listed it and this run does not", func(t *testing.T) {
		t.Parallel()

		previous := previousReport(t, fixtureResult(always), publish.PrivateNone, "2026-10-03", "18422000001")

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: previous,
		}).Body

		row := rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(always))

		want := []string{fingerprintOf(always), sevLow, catHardening, "fixed", "—", "—", "—"}
		if diff := diffCells(want, row); diff != "" {
			t.Errorf("the fixed row mismatch (-want +got):\n%s", diff)
		}

		if line := lineHolding(t, sectionText(t, body, "Changes since the last audit"), fingerprintOf(always)); !strings.Contains(line, "fixed") {
			t.Errorf("the Changes line %q does not say fixed", line)
		}
	})

	t.Run("a suspected finding is new, or open when either section holds it", func(t *testing.T) {
		t.Parallel()

		fresh := renderReport(t, publish.ReportInput{
			Result:  fixtureResult(suspected),
			Run:     testRun(t, testDate, testRunID),
			Private: publish.PrivateNone,
		}).Body

		if row := rowHolding(t, investigationTableOf(t, fresh), 0, fingerprintOf(suspected)); row[3] != "new" {
			t.Errorf("the suspected finding's status is %q, want new", row[3])
		}

		// The previous report listed the same finding as public: it is open,
		// and it gets no Changes line, since a suspected finding appears in its
		// status alone.
		publicPrevious := previousReport(t, fixtureResult(finding("G305", sevLow, true, catDenialOfSvc)), publish.PrivateNone, "2026-10-03", "18422000001")

		open := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(suspected),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: publicPrevious,
		}).Body

		if row := rowHolding(t, investigationTableOf(t, open), 0, fingerprintOf(suspected)); row[3] != "open" {
			t.Errorf("the suspected finding's status is %q, want open: the previous report's findings table holds it", row[3])
		}

		if changes := sectionText(t, open, "Changes since the last audit"); strings.Contains(changes, fingerprintOf(suspected)) {
			t.Errorf("the Changes section holds the suspected finding's fingerprint, want no line for it:\n%s", changes)
		}

		// The previous report listed it in its needs-investigation list: the
		// same finding is open again.
		suspectedPrevious := previousReport(t, fixtureResult(suspected), publish.PrivateNone, "2026-10-03", "18422000001")

		again := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(suspected),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: suspectedPrevious,
		}).Body

		if row := rowHolding(t, investigationTableOf(t, again), 0, fingerprintOf(suspected)); row[3] != "open" {
			t.Errorf("the suspected finding's status is %q, want open: the previous report's investigation list holds it", row[3])
		}
	})

	t.Run("a suspected finding that is gone gets no fixed row and no Changes line", func(t *testing.T) {
		t.Parallel()

		previous := previousReport(t, fixtureResult(suspected), publish.PrivateNone, "2026-10-03", "18422000001")

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: previous,
		}).Body

		for _, row := range findingsTableOf(t, body) {
			if row[0] == fingerprintOf(suspected) {
				t.Errorf("the findings table holds the suspected finding's row %v, want none: it gets no fixed row", row)
			}
		}

		if changes := sectionText(t, body, "Changes since the last audit"); strings.Contains(changes, fingerprintOf(suspected)) {
			t.Errorf("the Changes section holds the suspected finding's fingerprint, want no line for it:\n%s", changes)
		}
	})
}

// renderAndRead renders in and reads the report back as a later run's previous
// report, returning the rendered body as well.
func renderAndRead(t *testing.T, in publish.ReportInput) (string, *publish.Previous) {
	t.Helper()

	rendered := renderReport(t, in)

	previous, err := publish.ParsePreviousReport(path.Base(rendered.Path), []byte(rendered.Body))
	if err != nil {
		t.Fatalf("ParsePreviousReport(%q) on the report the core rendered: %v", path.Base(rendered.Path), err)
	}

	return rendered.Body, &previous
}

// TestPublicFindingThatTurnsPrivate pins the one disclosure a report must not
// make: a finding the previous report listed as public new or open, which this
// run places private, keeps the previous report's public row as open, with its
// cells carried byte for byte, and gets a still-open Changes line. Nothing of
// this run's private placement reaches the report, and the Changes section never
// says no public finding changed its status.
func TestPublicFindingThatTurnsPrivate(t *testing.T) {
	t.Parallel()

	public := finding("G304", sevLow, true, catHardening)
	public.File = "internal/cli/run.go"
	public.Line = 12
	public.Scenario = "a path is read without a check"
	public.Fix = "check the path"

	// The same rule, file and sink, re-rated medium this run: a private
	// placement, and the same plain fingerprint as the public finding.
	secret := public
	secret.Severity = sevMedium
	secret.Scenario = "the private scenario marker"
	secret.Fix = "the private fix marker"

	// check asserts that body keeps the finding's row as the previous report
	// rendered it, open, and that nothing of this run's placement leaks.
	check := func(t *testing.T, body, previousBody string) {
		t.Helper()

		previousRow := rowHolding(t, findingsTableOf(t, previousBody), 0, fingerprintOf(public))

		want := []string{
			fingerprintOf(public), previousRow[1], previousRow[2], "open",
			previousRow[4], previousRow[5], previousRow[6],
		}

		if diff := diffCells(want, rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(public))); diff != "" {
			t.Errorf("the carried row mismatch (-want +got):\n%s", diff)
		}

		changes := sectionText(t, body, "Changes since the last audit")

		if !strings.Contains(changes, "- "+fingerprintOf(public)+": still open") {
			t.Errorf("the Changes section holds no still-open line for the finding:\n%s", changes)
		}

		if strings.Contains(changes, "No public finding changed its status.") {
			t.Errorf("the Changes section says no public finding changed its status, but it carried a row:\n%s", changes)
		}

		for _, private := range []string{secret.Scenario, secret.Fix, sevMedium} {
			if strings.Contains(body, private) {
				t.Errorf("the report holds %q, which is this run's private placement", private)
			}
		}
	}

	t.Run("the previous report listed it as new", func(t *testing.T) {
		t.Parallel()

		previousBody, previous := renderAndRead(t, publish.ReportInput{
			Result:  fixtureResult(public),
			Run:     testRun(t, "2026-10-03", "18422000001"),
			Private: publish.PrivateNone,
		})

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(secret),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateHandled,
			Previous: previous,
		}).Body

		check(t, body, previousBody)
	})

	t.Run("the previous report listed it as open", func(t *testing.T) {
		t.Parallel()

		firstBody, first := renderAndRead(t, publish.ReportInput{
			Result:  fixtureResult(public),
			Run:     testRun(t, "2026-09-03", "18422000001"),
			Private: publish.PrivateNone,
		})

		if row := rowHolding(t, findingsTableOf(t, firstBody), 0, fingerprintOf(public)); row[3] != "new" {
			t.Fatalf("the first report's own status is %q, want new", row[3])
		}

		previousBody, previous := renderAndRead(t, publish.ReportInput{
			Result:   fixtureResult(public),
			Run:      testRun(t, "2026-10-03", "18422000002"),
			Private:  publish.PrivateNone,
			Previous: first,
		})

		if row := rowHolding(t, findingsTableOf(t, previousBody), 0, fingerprintOf(public)); row[3] != "open" {
			t.Fatalf("the previous report's own status is %q, want open", row[3])
		}

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(secret),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateHandled,
			Previous: previous,
		}).Body

		check(t, body, previousBody)
	})
}

// TestFixedRows pins ADR 0040's fixed-row rules: a finding this run places
// private gets no fixed row, a finding this run drops as a false positive gets
// none either, and a finding the previous report listed and also dismissed,
// which this run does not find, does get one.
func TestFixedRows(t *testing.T) {
	t.Parallel()

	public := finding("G304", sevLow, true, catHardening)
	public.File = "internal/cli/run.go"
	public.Line = 12
	public.Scenario = "a path is read without a check"
	public.Fix = "check the path"

	t.Run("a finding placed private this run gets no fixed row", func(t *testing.T) {
		t.Parallel()

		_, previous := renderAndRead(t, publish.ReportInput{
			Result:  fixtureResult(public),
			Run:     testRun(t, "2026-10-03", "18422000001"),
			Private: publish.PrivateNone,
		})

		secret := public
		secret.Severity = sevMedium

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(secret),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateHandled,
			Previous: previous,
		}).Body

		if row := rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(public)); row[3] != "open" {
			t.Errorf("the finding's status is %q, want open: a private placement gets no fixed row", row[3])
		}

		if changes := sectionText(t, body, "Changes since the last audit"); strings.Contains(changes, "- "+fingerprintOf(public)+": fixed") {
			t.Errorf("the Changes section marks the private finding fixed:\n%s", changes)
		}
	})

	t.Run("a finding dropped as a false positive gets no fixed row", func(t *testing.T) {
		t.Parallel()

		dismissed := public
		dismissed.Dismissal = "a test fixture, not a finding"

		_, previous := renderAndRead(t, publish.ReportInput{
			Result:  fixtureResult(dismissed),
			Run:     testRun(t, "2026-10-03", "18422000001"),
			Private: publish.PrivateNone,
		})

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(dismissed),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: previous,
		}).Body

		if holdsRow(findingsTableOf(t, body), fingerprintOf(public)) || holdsRow(investigationTableOf(t, body), fingerprintOf(public)) {
			t.Errorf("a findings table holds the dropped finding, want it dropped:\n%s", body)
		}

		if !holdsRow(tableOf(t, body, "False positives"), fingerprintOf(public)) {
			t.Errorf("the False positives table does not carry the previous report's row:\n%s", body)
		}

		if changes := sectionText(t, body, "Changes since the last audit"); strings.Contains(changes, fingerprintOf(public)) {
			t.Errorf("the Changes section holds a line for the dropped finding:\n%s", changes)
		}
	})

	t.Run("a finding listed and dismissed, not found this run, gets a fixed row", func(t *testing.T) {
		t.Parallel()

		dismissed := public
		dismissed.Dismissal = "a test fixture, not a finding"

		// The first report holds the finding in both tables: it is public, and
		// its dismissal adds a False positives row.
		previousBody, previous := renderAndRead(t, publish.ReportInput{
			Result:  fixtureResult(dismissed),
			Run:     testRun(t, "2026-10-03", "18422000001"),
			Private: publish.PrivateNone,
		})

		if !holdsRow(tableOf(t, previousBody, "False positives"), fingerprintOf(public)) {
			t.Fatalf("the previous report holds no False positives row for the finding:\n%s", previousBody)
		}

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: previous,
		}).Body

		want := []string{fingerprintOf(public), sevLow, catHardening, "fixed", "—", "—", "—"}
		if diff := diffCells(want, rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(public))); diff != "" {
			t.Errorf("the fixed row mismatch (-want +got):\n%s", diff)
		}

		if changes := sectionText(t, body, "Changes since the last audit"); !strings.Contains(changes, "- "+fingerprintOf(public)+": fixed") {
			t.Errorf("the Changes section holds no fixed line for the finding:\n%s", changes)
		}
	})
}

// TestFalsePositives pins the drop and the section: a public or
// needs-investigation finding whose plain fingerprint is a row of the previous
// report's False positives section counts nowhere, the previous rows are
// carried forward unchanged, a kept public finding with a dismissal adds a row,
// and a private finding's dismissal is never written.
func TestFalsePositives(t *testing.T) {
	t.Parallel()

	dismissed := finding("G304", sevLow, true, catHardening)
	dismissed.Dismissal = "a test fixture, not a finding"

	secret := finding("G308", sevMedium, true, catDependency)
	secret.Dismissal = "the advisory is closed"

	t.Run("a dismissed finding is carried forward and drops the finding", func(t *testing.T) {
		t.Parallel()

		previous := previousReport(t, fixtureResult(dismissed), publish.PrivateNone, "2026-10-03", "18422000001")

		in := publish.ReportInput{
			Result:   fixtureResult(dismissed),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: previous,
		}

		body := renderReport(t, in).Body

		for _, row := range findingsTableOf(t, body) {
			if row[0] == fingerprintOf(dismissed) {
				t.Errorf("the findings table holds the dropped finding's row %v, want none", row)
			}
		}

		if findings := strings.Join(sectionOf(t, body, "Findings").Lines, "\n"); !strings.Contains(findings, "No public findings.") {
			t.Errorf("the Findings section does not read No public findings., want the dropped finding counted nowhere:\n%s", findings)
		}

		// Its check reports run, not findings: a dropped finding is no public
		// finding.
		coverage := rowHolding(t, tableOf(t, body, "Coverage"), 0, "The security-audit checklist on the whole tree")
		if coverage[1] != "run" {
			t.Errorf("the dropped finding's check reports %q, want run", coverage[1])
		}

		// The row the previous report holds is carried forward with its reason.
		row := rowHolding(t, tableOf(t, body, "False positives"), 0, fingerprintOf(dismissed))
		if row[1] != "` a test fixture, not a finding `" {
			t.Errorf("the carried-forward row reads %q, want the previous report's reason", row[1])
		}

		// The total counts it nowhere.
		templates, err := publish.RenderTemplates(in)
		if err != nil {
			t.Fatalf("RenderTemplates: %v", err)
		}

		if !strings.Contains(templates.PRBody, "Public findings: 0") {
			t.Errorf("the pull request body does not say Public findings: 0, want the dropped finding counted nowhere:\n%s", templates.PRBody)
		}
	})

	t.Run("a suspected finding is dropped too, and a new dismissal adds a row", func(t *testing.T) {
		t.Parallel()

		settled := finding("G306", sevLow, false, catDenialOfSvc)
		settled.Dismissal = "the input is a constant"

		fresh := finding("G307", sevLow, true, catConfiguration)
		fresh.Dismissal = "by design, the path is a test fixture"

		previous := previousReport(t, fixtureResult(settled), publish.PrivateNone, "2026-10-03", "18422000001")

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(settled, fresh),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateNone,
			Previous: previous,
		}).Body

		if rows := investigationTableOf(t, body); holdsRow(rows, fingerprintOf(settled)) {
			t.Errorf("the investigation table holds the dropped suspected finding, want none: %v", rows)
		}

		falsePositives := tableOf(t, body, "False positives")

		if carried := rowHolding(t, falsePositives, 0, fingerprintOf(settled)); carried[1] != "` the input is a constant `" {
			t.Errorf("the carried-forward row reads %q, want the previous report's reason", carried[1])
		}

		if added := rowHolding(t, falsePositives, 0, fingerprintOf(fresh)); added[1] != "` by design, the path is a test fixture `" {
			t.Errorf("the new row reads %q, want the model's dismissal", added[1])
		}

		if len(falsePositives) != 3 {
			t.Errorf("the False positives table holds %d rows besides its header, want the carried-forward row and the new one", len(falsePositives)-1)
		}
	})

	t.Run("a private finding's dismissal is never written", func(t *testing.T) {
		t.Parallel()

		fresh := finding("G307", sevLow, true, catConfiguration)
		fresh.Dismissal = "by design, the path is a test fixture"

		previous := previousReport(t, fixtureResult(dismissed), publish.PrivateNone, "2026-10-03", "18422000001")

		body := renderReport(t, publish.ReportInput{
			Result:   fixtureResult(fresh, secret),
			Run:      testRun(t, testDate, testRunID),
			Private:  publish.PrivateHandled,
			Previous: previous,
		}).Body

		falsePositives := tableOf(t, body, "False positives")

		for _, row := range falsePositives {
			if row[0] == fingerprintOf(secret) {
				t.Errorf("the False positives table holds the private finding's row %v, want none", row)
			}

			if strings.Contains(strings.Join(row, " "), "the advisory is closed") {
				t.Errorf("the False positives table holds the private finding's dismissal: %v", row)
			}
		}

		if !holdsRow(falsePositives, fingerprintOf(fresh)) {
			t.Errorf("the False positives table holds no row for the kept public finding: %v", falsePositives)
		}

		if !holdsRow(falsePositives, fingerprintOf(dismissed)) {
			t.Errorf("the False positives table does not carry the previous report's row forward: %v", falsePositives)
		}
	})
}

// holdsRow reports whether a row of rows holds fingerprint in its first cell.
func holdsRow(rows [][]string, fingerprint string) bool {
	for _, row := range rows {
		if len(row) > 0 && row[0] == fingerprint {
			return true
		}
	}

	return false
}

// diffCells returns a readable difference between two table rows.
func diffCells(want, got []string) string {
	if len(want) != len(got) {
		return fmt.Sprintf("want %q, got %q", strings.Join(want, " | "), strings.Join(got, " | "))
	}

	var b strings.Builder

	for i := range want {
		if want[i] != got[i] {
			fmt.Fprintf(&b, "  cell %d: want %q, got %q\n", i, want[i], got[i])
		}
	}

	return b.String()
}
