package publish_test

// Group C of PR 4c: the draft advisories' core (PRD #175 milestone 2, "Only
// what the App wrote", "It tracks private findings by fingerprint"; ADR 0040,
// rows 14-16). A private finding's advisory is fixed text: its fingerprint's
// summary, no model text, every model value inside a code span or a fenced code
// block, the status line last so a model-written one can never be read as the
// script's, and the evidence in the advisory and nothing else. A history secret
// names the commit and the rule only: never its sink, its scenario, its
// evidence or its fix.

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// The advisory fixtures' invented values: a fingerprint of the shape a keyed
// one has, which no HMAC over these fixtures produces, so a test that reads it
// back shows the renderer formats the fingerprint it is given.
const advisoryFingerprint = publish.Fingerprint("fp-0123456789ab")

// severities is the severity vocabulary (ADR 0040), in the findings tables'
// order.
var severities = []string{sevCritical, sevHigh, sevMedium, sevLow, sevInformational}

// knownCategories is the category vocabulary (ADR 0040).
var knownCategories = []string{
	"injection",
	"authentication bypass",
	"credential exposure",
	"remote code execution",
	"supply chain",
	"dependency vulnerability",
	"configuration",
	"hardening",
	"denial of service",
	"information disclosure",
}

// advisoryFinding returns the private finding most of the advisory tests
// render: a confirmed, model-flagged private finding of the tree review, its
// model values each holding a marker, so a value the description must not hold
// is a marker the test looks for.
func advisoryFinding() publish.Finding {
	return publish.Finding{
		Check:     "tree-review",
		Rule:      "G309",
		File:      "internal/cli/run.go",
		Line:      12,
		Sink:      "os.Open",
		Severity:  sevMedium,
		Confirmed: true,
		Category:  catInjection,
		Private:   true,
		Scenario:  "the scenario marker",
		Evidence:  "the evidence marker",
		Fix:       "the fix marker",
	}
}

// advisoryPrivateFinding returns the fixture finding with the invented
// fingerprint.
func advisoryPrivateFinding() publish.PrivateFinding {
	return publish.PrivateFinding{Finding: advisoryFinding(), Fingerprint: advisoryFingerprint}
}

// historySecretFinding returns a private finding of gitleaks on the history: its
// file is the commit, and every other model value holds a marker the advisory
// must not hold.
func historySecretFinding() publish.Finding {
	return publish.Finding{
		Check:     "history-secrets",
		Rule:      "gitleaks:generic-api-key",
		File:      "0123456789abcdef0123456789abcdef01234567",
		Line:      0,
		Sink:      "the sink marker",
		Severity:  sevHigh,
		Confirmed: false,
		Category:  catConfiguration,
		Scenario:  "the scenario marker",
		Evidence:  "the evidence marker",
		Fix:       "the fix marker",
	}
}

// unknownWord returns v when it is one of words, and unknown otherwise.
func unknownWord(v string, words []string) string {
	if slices.Contains(words, v) {
		return v
	}

	return "unknown"
}

// checkField returns the description's check: a check id as it is, since check
// ids are a fixed vocabulary, and any other value as a code span.
func checkField(id string) string {
	if slices.Contains(checkIDs, id) {
		return id
	}

	return publish.CodeSpan(id)
}

// wantAdvisorySummary returns the summary RenderAdvisory must render: ADR
// 0040's template, unknown in place of a value outside the vocabulary.
func wantAdvisorySummary(fp publish.Fingerprint, severity, category string) string {
	return "Audit finding " + string(fp) + " (" +
		unknownWord(severity, severities) + ", " + unknownWord(category, knownCategories) + ")"
}

// wantAdvisoryDescription returns the description RenderAdvisory must render,
// written from the design's templates: every model value in a code span or a
// fenced code block, and the status line and the run's URL last. A history
// secret's description holds the commit and the rule only.
func wantAdvisoryDescription(f publish.Finding, fp publish.Fingerprint, status, runURL string) string {
	var b strings.Builder

	b.WriteString("Security audit finding " + string(fp) + ", found by the scheduled security audit.\n")
	b.WriteString("\n")

	if f.Check == "history-secrets" {
		b.WriteString("A secret in the repository's history: unconfirmed: maintainer to check.\n")
		b.WriteString("It was not tried against its service.\n")
		b.WriteString("\n")
		b.WriteString("Commit: " + publish.CodeSpan(f.File) + "\n")
		b.WriteString("Rule: " + publish.CodeSpan(f.Rule) + "\n")
	} else {
		confirmed := "no: unconfirmed"
		if f.Confirmed {
			confirmed = "yes"
		}

		location := publish.CodeSpan(f.File)
		if f.Line > 0 {
			location += ":" + strconv.Itoa(f.Line)
		}

		b.WriteString("Severity: " + unknownWord(f.Severity, severities) + "\n")
		b.WriteString("Category: " + unknownWord(f.Category, knownCategories) + "\n")
		b.WriteString("Confirmed: " + confirmed + "\n")
		b.WriteString("Check: " + checkField(f.Check) + "\n")
		b.WriteString("Rule: " + publish.CodeSpan(f.Rule) + "\n")
		b.WriteString("Location: " + location + "\n")
		b.WriteString("\n")
		b.WriteString("Sink:\n")
		b.WriteString("\n" + publish.CodeBlock(f.Sink) + "\n")
		b.WriteString("\n")
		b.WriteString("Scenario:\n")
		b.WriteString("\n" + publish.CodeBlock(f.Scenario) + "\n")
		b.WriteString("\n")
		b.WriteString("Evidence:\n")
		b.WriteString("\n" + publish.CodeBlock(f.Evidence) + "\n")
		b.WriteString("\n")
		b.WriteString("Recommended fix:\n")
		b.WriteString("\n" + publish.CodeBlock(f.Fix) + "\n")
	}

	b.WriteString("\n")
	b.WriteString("Audit status: " + status + "\n")
	b.WriteString("Run: " + runURL + "\n")

	return b.String()
}

// TestRenderAdvisorySummary pins the summary's two vocabulary words: the
// finding's own when it is in the vocabulary, unknown when it is not, and no
// model text at all.
func TestRenderAdvisorySummary(t *testing.T) {
	t.Parallel()

	run := testRun(t, testDate, testRunID)

	for _, tc := range []struct {
		name               string
		severity, category string
		want               string
	}{
		{
			name:     "a known severity and a known category",
			severity: sevHigh,
			category: catInjection,
			want:     "Audit finding " + string(advisoryFingerprint) + " (high, injection)",
		},
		{
			name:     "an unknown severity",
			severity: "severe",
			category: catInjection,
			want:     "Audit finding " + string(advisoryFingerprint) + " (unknown, injection)",
		},
		{
			name:     "an unknown category",
			severity: sevMedium,
			category: "a category nobody names",
			want:     "Audit finding " + string(advisoryFingerprint) + " (medium, unknown)",
		},
		{
			name:     "an unknown severity and an unknown category",
			severity: "",
			category: "",
			want:     "Audit finding " + string(advisoryFingerprint) + " (unknown, unknown)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := advisoryFinding()
			f.Severity, f.Category = tc.severity, tc.category

			text, err := publish.RenderAdvisory(publish.PrivateFinding{Finding: f, Fingerprint: advisoryFingerprint}, publish.AdvisoryNew, run)
			if err != nil {
				t.Fatalf("RenderAdvisory = %v, want no error", err)
			}

			if text.Summary != tc.want {
				t.Errorf("the advisory's summary is %q, want %q", text.Summary, tc.want)
			}
		})
	}
}

// TestRenderAdvisoryDescription pins the description's fixed text for a
// confirmed private finding, an unconfirmed one, one without a line, and one
// whose check id is outside the vocabulary: every model value in a code span or
// a fenced code block, and the status line after them all.
func TestRenderAdvisoryDescription(t *testing.T) {
	t.Parallel()

	run := testRun(t, testDate, testRunID)

	unconfirmed := advisoryFinding()
	unconfirmed.Confirmed = false

	noLine := advisoryFinding()
	noLine.Line = 0

	unknownCheck := advisoryFinding()
	unknownCheck.Check = "a check the audit does not know"

	withBackticks := advisoryFinding()
	withBackticks.Sink = "a sink with `` two backticks"
	withBackticks.Evidence = "a trace with ``` three backticks"

	for _, tc := range []struct {
		name    string
		finding publish.Finding
	}{
		{name: "a confirmed finding", finding: advisoryFinding()},
		{name: "an unconfirmed finding", finding: unconfirmed},
		{name: "a finding without a line", finding: noLine},
		{name: "a check id outside the vocabulary", finding: unknownCheck},
		{name: "model values holding backticks", finding: withBackticks},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			text, err := publish.RenderAdvisory(publish.PrivateFinding{Finding: tc.finding, Fingerprint: advisoryFingerprint}, publish.AdvisoryNew, run)
			if err != nil {
				t.Fatalf("RenderAdvisory = %v, want no error", err)
			}

			if want := wantAdvisoryDescription(tc.finding, advisoryFingerprint, "new", run.URL()); text.Description != want {
				t.Errorf("the advisory's description is\n%s\nwant\n%s", text.Description, want)
			}

			if want := wantAdvisorySummary(advisoryFingerprint, tc.finding.Severity, tc.finding.Category); text.Summary != want {
				t.Errorf("the advisory's summary is %q, want %q", text.Summary, want)
			}
		})
	}
}

// TestRenderAdvisoryHistorySecret pins a secret in the repository's history: the
// commit and the rule only, "unconfirmed: maintainer to check", and nothing of
// the sink, the scenario, the evidence or the fix, any of which may hold the
// secret itself.
func TestRenderAdvisoryHistorySecret(t *testing.T) {
	t.Parallel()

	run := testRun(t, testDate, testRunID)
	f := historySecretFinding()

	text, err := publish.RenderAdvisory(publish.PrivateFinding{Finding: f, Fingerprint: advisoryFingerprint}, publish.AdvisoryNew, run)
	if err != nil {
		t.Fatalf("RenderAdvisory = %v, want no error", err)
	}

	if want := wantAdvisoryDescription(f, advisoryFingerprint, "new", run.URL()); text.Description != want {
		t.Errorf("the history secret's description is\n%s\nwant\n%s", text.Description, want)
	}

	if !strings.Contains(text.Description, "unconfirmed: maintainer to check") {
		t.Errorf("the history secret's description holds no maintainer to check line:\n%s", text.Description)
	}

	for _, hidden := range []string{"the sink marker", "the scenario marker", "the evidence marker", "the fix marker", "Sink:", "Scenario:", "Evidence:", "Recommended fix:"} {
		if strings.Contains(text.Description, hidden) {
			t.Errorf("the history secret's description holds %q, want the commit and the rule only:\n%s", hidden, text.Description)
		}
	}

	// The summary is any finding's: the severity and the category are no
	// secret.
	if want := wantAdvisorySummary(advisoryFingerprint, f.Severity, f.Category); text.Summary != want {
		t.Errorf("the history secret's summary is %q, want %q", text.Summary, want)
	}
}

// TestRenderAdvisoryStatus pins the three statuses a run renders, the run's URL
// below the status line, and every refusal: not reproduced (SetAdvisoryStatus
// writes it), no status at all, one beyond the four, a public finding, a
// needs-investigation one, a fingerprint that is none, and a run NewRun refuses.
func TestRenderAdvisoryStatus(t *testing.T) {
	t.Parallel()

	public := advisoryFinding()
	public.Severity, public.Category, public.Private = sevLow, catHardening, false

	investigation := advisoryFinding()
	investigation.Severity, investigation.Category, investigation.Private = sevLow, catDenialOfSvc, false
	investigation.Confirmed = false

	experimental := advisoryFinding()
	experimental.Severity, experimental.Category = "severe", "a category nobody names"

	for _, tc := range []struct {
		name      string
		finding   publish.Finding
		fp        publish.Fingerprint
		status    publish.AdvisoryStatus
		run       publish.Run
		want      string
		wantError error
	}{
		{name: "a new draft", finding: advisoryFinding(), fp: advisoryFingerprint, status: publish.AdvisoryNew, run: testRun(t, testDate, testRunID), want: "new"},
		{name: "an open draft", finding: advisoryFinding(), fp: advisoryFingerprint, status: publish.AdvisoryOpen, run: testRun(t, testDate, testRunID), want: "open"},
		{name: "a regressed draft", finding: advisoryFinding(), fp: advisoryFingerprint, status: publish.AdvisoryRegressed, run: testRun(t, testDate, testRunID), want: "regressed"},
		{
			name:    "a severity and a category outside the vocabulary",
			finding: experimental,
			fp:      advisoryFingerprint,
			status:  publish.AdvisoryNew,
			run:     testRun(t, testDate, testRunID),
			want:    "new",
		},
		{
			name:      "not reproduced",
			finding:   advisoryFinding(),
			fp:        advisoryFingerprint,
			status:    publish.AdvisoryNotReproduced,
			run:       testRun(t, testDate, testRunID),
			wantError: publish.ErrConfig,
		},
		{
			name:      "no status at all",
			finding:   advisoryFinding(),
			fp:        advisoryFingerprint,
			status:    0,
			run:       testRun(t, testDate, testRunID),
			wantError: publish.ErrConfig,
		},
		{
			name:      "one beyond the four",
			finding:   advisoryFinding(),
			fp:        advisoryFingerprint,
			status:    publish.AdvisoryRegressed + 1,
			run:       testRun(t, testDate, testRunID),
			wantError: publish.ErrConfig,
		},
		{
			name:      "a public finding",
			finding:   public,
			fp:        advisoryFingerprint,
			status:    publish.AdvisoryNew,
			run:       testRun(t, testDate, testRunID),
			wantError: publish.ErrConfig,
		},
		{
			name:      "a needs-investigation finding",
			finding:   investigation,
			fp:        advisoryFingerprint,
			status:    publish.AdvisoryNew,
			run:       testRun(t, testDate, testRunID),
			wantError: publish.ErrConfig,
		},
		{
			name:      "a fingerprint that is none",
			finding:   advisoryFinding(),
			fp:        "fp-0123456789ab00",
			status:    publish.AdvisoryNew,
			run:       testRun(t, testDate, testRunID),
			wantError: publish.ErrConfig,
		},
		{
			name:      "a run NewRun refuses",
			finding:   advisoryFinding(),
			fp:        advisoryFingerprint,
			status:    publish.AdvisoryNew,
			run:       publish.Run{},
			wantError: publish.ErrConfig,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			text, err := publish.RenderAdvisory(publish.PrivateFinding{Finding: tc.finding, Fingerprint: tc.fp}, tc.status, tc.run)

			if tc.wantError != nil {
				if !errors.Is(err, tc.wantError) {
					t.Fatalf("RenderAdvisory = %+v, %v, want an error wrapping %v", text, err, tc.wantError)
				}

				if text != (publish.AdvisoryText{}) {
					t.Errorf("the refused render returned %+v, want no text", text)
				}

				return
			}

			if err != nil {
				t.Fatalf("RenderAdvisory = %v, want no error", err)
			}

			if tail := "\nAudit status: " + tc.want + "\nRun: " + tc.run.URL() + "\n"; !strings.HasSuffix(text.Description, tail) {
				t.Errorf("the description does not end with %q:\n%s", tail, text.Description)
			}
		})
	}
}

// TestRenderAdvisorySeverity pins GitHub's severity field: the four severities
// as they are, informational low, and nothing for an unknown one, which the
// create body then leaves out.
func TestRenderAdvisorySeverity(t *testing.T) {
	t.Parallel()

	run := testRun(t, testDate, testRunID)

	for _, tc := range []struct {
		severity string
		want     string
	}{
		{severity: sevCritical, want: "critical"},
		{severity: sevHigh, want: "high"},
		{severity: sevMedium, want: "medium"},
		{severity: sevLow, want: "low"},
		{severity: sevInformational, want: "low"},
		{severity: "severe", want: ""},
		{severity: "", want: ""},
	} {
		t.Run("a "+tc.severity+" finding", func(t *testing.T) {
			t.Parallel()

			f := advisoryFinding()
			f.Severity = tc.severity

			text, err := publish.RenderAdvisory(publish.PrivateFinding{Finding: f, Fingerprint: advisoryFingerprint}, publish.AdvisoryNew, run)
			if err != nil {
				t.Fatalf("RenderAdvisory = %v, want no error", err)
			}

			if text.Severity != tc.want {
				t.Errorf("the advisory's severity is %q, want %q", text.Severity, tc.want)
			}
		})
	}
}

// TestRenderAdvisoryHoldsTheEvidenceOnly pins where a finding's evidence goes:
// into the draft advisory's description, and into no public text of the run.
func TestRenderAdvisoryHoldsTheEvidenceOnly(t *testing.T) {
	t.Parallel()

	const privateMarker = "the private evidence marker"
	const publicMarker = "the public evidence marker"

	private := advisoryFinding()
	private.Evidence = privateMarker

	public := advisoryFinding()
	public.Severity, public.Category, public.Private = sevLow, catHardening, false
	public.Evidence = publicMarker

	run := testRun(t, testDate, testRunID)
	in := publish.ReportInput{Result: fixtureResult(public, private), Run: run, Private: publish.PrivateHandled}

	text, err := publish.RenderAdvisory(publish.PrivateFinding{Finding: private, Fingerprint: advisoryFingerprint}, publish.AdvisoryNew, run)
	if err != nil {
		t.Fatalf("RenderAdvisory = %v, want no error", err)
	}

	if !strings.Contains(text.Description, publish.CodeBlock(privateMarker)) {
		t.Errorf("the advisory's description holds no block of the finding's evidence:\n%s", text.Description)
	}

	rendered := renderReport(t, in)

	for _, marker := range []string{privateMarker, publicMarker} {
		if strings.Contains(rendered.Body, marker) {
			t.Errorf("the report holds the evidence marker %q, want it in a draft advisory only:\n%s", marker, rendered.Body)
		}

		if strings.Contains(strings.Join(rendered.ModelFields, "\n"), marker) {
			t.Errorf("the report lists the evidence marker %q as a model field, want it in a draft advisory only", marker)
		}
	}

	previous := renderAndParse(t, in)
	rows := previous.PublicFindings()

	if len(rows) != 1 {
		t.Fatalf("the report carries %d public finding(s), want the one public finding", len(rows))
	}

	issue, err := publish.RenderIssue(rows[0], run)
	if err != nil {
		t.Fatalf("RenderIssue = %v, want no error", err)
	}

	if strings.Contains(issue.Body, publicMarker) || strings.Contains(issue.Title, publicMarker) {
		t.Errorf("the issue holds the evidence marker %q:\n%s", publicMarker, issue.Body)
	}
}

// TestParseAdvisoryStatus pins the line the description's status lives in: the
// last line that is exactly "Audit status: " and one of the four words, so a
// model-written line inside a code block is never the status - and no line
// with a stray space, another word or no space at all is one.
func TestParseAdvisoryStatus(t *testing.T) {
	t.Parallel()

	description := wantAdvisoryDescription(advisoryFinding(), advisoryFingerprint, "new", "https://github.com/example/shift2eu/actions/runs/1")

	for _, tc := range []struct {
		name        string
		description string
		want        publish.AdvisoryStatus
		ok          bool
	}{
		{name: "new", description: "Audit status: new", want: publish.AdvisoryNew, ok: true},
		{name: "open", description: "a body\n\nAudit status: open", want: publish.AdvisoryOpen, ok: true},
		{name: "not reproduced", description: "Audit status: not reproduced\nmore text", want: publish.AdvisoryNotReproduced, ok: true},
		{name: "regressed", description: "Audit status: regressed", want: publish.AdvisoryRegressed, ok: true},
		{
			name:        "a model-written line before the script's",
			description: "Evidence:\n\n```text\nAudit status: open\n```\n\nAudit status: not reproduced\nRun: https://github.com/example/shift2eu/actions/runs/1",
			want:        publish.AdvisoryNotReproduced,
			ok:          true,
		},
		{name: "a rendered description", description: description, want: publish.AdvisoryNew, ok: true},
		{name: "a leading space", description: " Audit status: open"},
		{name: "a trailing space", description: "Audit status: open "},
		{name: "another word", description: "Audit status: fixed"},
		{name: "the word as a prefix", description: "Audit status: open now"},
		{name: "lower case", description: "audit status: open"},
		{name: "no word at all", description: "Audit status:"},
		{name: "no status line", description: "a description with no status"},
		{name: "the empty description", description: ""},
		{name: "CR LF", description: "a body\r\n\r\nAudit status: open\r\n", want: publish.AdvisoryOpen, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, ok := publish.ParseAdvisoryStatus(tc.description)

			if ok != tc.ok || status != tc.want {
				t.Errorf("ParseAdvisoryStatus(%q) = %v, %t, want %v, %t", tc.description, status, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestSetAdvisoryStatus pins the one text a run has for a draft it did not
// reproduce: the listing's description with its last status line replaced, and
// every refusal (a status outside the four, a description with no status line).
func TestSetAdvisoryStatus(t *testing.T) {
	t.Parallel()

	t.Run("only the last status line is replaced", func(t *testing.T) {
		t.Parallel()

		description := "a body\n\n```text\nAudit status: open\n```\n\nAudit status: new\n"

		got, err := publish.SetAdvisoryStatus(description, publish.AdvisoryNotReproduced)
		if err != nil {
			t.Fatalf("SetAdvisoryStatus = %v, want no error", err)
		}

		want := "a body\n\n```text\nAudit status: open\n```\n\nAudit status: not reproduced\n"
		if got != want {
			t.Errorf("SetAdvisoryStatus = %q, want %q: only the last status line changes", got, want)
		}
	})

	for _, tc := range []struct {
		name        string
		description string
		status      publish.AdvisoryStatus
	}{
		{name: "a description with no status line", description: "a body with no status", status: publish.AdvisoryNotReproduced},
		{name: "a status beyond the four", description: "Audit status: open\n", status: publish.AdvisoryRegressed + 1},
		{name: "no status at all", description: "Audit status: open\n", status: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := publish.SetAdvisoryStatus(tc.description, tc.status)

			if !errors.Is(err, publish.ErrConfig) {
				t.Fatalf("SetAdvisoryStatus = %q, %v, want an error wrapping ErrConfig", got, err)
			}

			if got != "" {
				t.Errorf("the refused call returned %q, want no text", got)
			}
		})
	}

	t.Run("a round trip with ParseAdvisoryStatus", func(t *testing.T) {
		t.Parallel()

		for _, status := range []publish.AdvisoryStatus{
			publish.AdvisoryNew,
			publish.AdvisoryOpen,
			publish.AdvisoryNotReproduced,
			publish.AdvisoryRegressed,
		} {
			got, err := publish.SetAdvisoryStatus("a body\n\nAudit status: new\n", status)
			if err != nil {
				t.Fatalf("SetAdvisoryStatus(%v) = %v, want no error", status, err)
			}

			parsed, ok := publish.ParseAdvisoryStatus(got)
			if !ok || parsed != status {
				t.Errorf("ParseAdvisoryStatus(SetAdvisoryStatus(%v)) = %v, %t, want %v, true", status, parsed, ok, status)
			}
		}
	})
}

// TestAdvisoryFingerprint pins the summary's fingerprint: ADR 0040's template
// and nothing else. A reworded summary, an upper-case one, one hex character
// short or over, and one without the parenthesis after the fingerprint match no
// finding.
func TestAdvisoryFingerprint(t *testing.T) {
	t.Parallel()

	rendered := wantAdvisorySummary(advisoryFingerprint, sevMedium, catInjection)

	for _, tc := range []struct {
		name    string
		summary string
		want    publish.Fingerprint
		ok      bool
	}{
		{name: "the template", summary: rendered, want: advisoryFingerprint, ok: true},
		{name: "a summary at the end of the template", summary: "Audit finding fp-ffffff000000 (low, hardening)", want: "fp-ffffff000000", ok: true},
		{name: "a reworded summary", summary: "Audit finding " + string(advisoryFingerprint) + " in internal/cli/run.go"},
		{name: "upper-case hex", summary: "Audit finding fp-0123456789AB (high, injection)"},
		{name: "eleven hex characters", summary: "Audit finding fp-0123456789a (high, injection)"},
		{name: "thirteen hex characters", summary: "Audit finding fp-0123456789abc (high, injection)"},
		{name: "no parenthesis after the fingerprint", summary: "Audit finding fp-0123456789ab"},
		{name: "no template", summary: "fp-0123456789ab (high, injection)"},
		{name: "the empty summary", summary: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fp, ok := publish.AdvisoryFingerprint(tc.summary)

			if ok != tc.ok || fp != tc.want {
				t.Errorf("AdvisoryFingerprint(%q) = %q, %t, want %q, %t", tc.summary, fp, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestPrivateFindings pins which findings a draft advisory is filed for: the
// ones Place makes private, in the result's order, each with its keyed
// fingerprint. A key KeyedFingerprint refuses is ErrConfig, and two private
// findings with one fingerprint are ErrResultInvalid.
func TestPrivateFindings(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	public := advisoryFinding()
	public.Severity, public.Category, public.Private = sevLow, catHardening, false

	investigation := advisoryFinding()
	investigation.Rule = "G308"
	investigation.Severity, investigation.Category, investigation.Private = sevLow, catDenialOfSvc, false
	investigation.Confirmed = false

	confirmed := advisoryFinding()
	confirmed.Rule = "G309"

	unconfirmed := advisoryFinding()
	unconfirmed.Rule = "G310"
	unconfirmed.Confirmed = false

	secret := historySecretFinding()

	t.Run("every private finding, in the result's order", func(t *testing.T) {
		t.Parallel()

		result := fixtureResult(public, confirmed, investigation, unconfirmed, secret)

		got, err := publish.PrivateFindings(result, key)
		if err != nil {
			t.Fatalf("PrivateFindings = %v, want no error", err)
		}

		want := []publish.PrivateFinding{
			{Finding: confirmed, Fingerprint: keyedFingerprintOf(t, key, confirmed)},
			{Finding: unconfirmed, Fingerprint: keyedFingerprintOf(t, key, unconfirmed)},
			{Finding: secret, Fingerprint: keyedFingerprintOf(t, key, secret)},
		}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("the private findings (-want +got):\n%s", diff)
		}
	})

	t.Run("a public-only result", func(t *testing.T) {
		t.Parallel()

		got, err := publish.PrivateFindings(fixtureResult(public, investigation), key)
		if err != nil {
			t.Fatalf("PrivateFindings = %v, want no error", err)
		}

		if len(got) != 0 {
			t.Errorf("PrivateFindings = %+v, want none: no finding is private", got)
		}
	})

	t.Run("a key that was never set", func(t *testing.T) {
		t.Parallel()

		got, err := publish.PrivateFindings(fixtureResult(confirmed), publish.FingerprintKey{})

		if !errors.Is(err, publish.ErrConfig) {
			t.Fatalf("PrivateFindings with no key = %+v, %v, want an error wrapping ErrConfig", got, err)
		}

		if got != nil {
			t.Errorf("PrivateFindings with no key = %+v, want nothing", got)
		}
	})

	t.Run("two private findings with one fingerprint", func(t *testing.T) {
		t.Parallel()

		twin := advisoryFinding()

		got, err := publish.PrivateFindings(fixtureResult(confirmed, twin), key)

		if !errors.Is(err, publish.ErrResultInvalid) {
			t.Fatalf("PrivateFindings of two findings with one fingerprint = %+v, %v, want an error wrapping ErrResultInvalid", got, err)
		}

		if got != nil {
			t.Errorf("PrivateFindings of two findings with one fingerprint = %+v, want nothing", got)
		}
	})
}

// keyedFingerprintOf returns the keyed fingerprint PrivateFindings must give a
// finding: the core's own HMAC over the finding's rule, file and sink.
func keyedFingerprintOf(t *testing.T, key publish.FingerprintKey, f publish.Finding) publish.Fingerprint {
	t.Helper()

	fp, err := publish.KeyedFingerprint(key, f.Rule, f.File, f.Sink)
	if err != nil {
		t.Fatalf("KeyedFingerprint: %v", err)
	}

	return fp
}
