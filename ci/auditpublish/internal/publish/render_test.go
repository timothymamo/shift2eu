package publish_test

import (
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// severityOrder is the order the findings table sorts by: the schema's, most
// severe first.
const severityOrder = "critical, high, medium, low, informational"

// wantFindingRow returns the findings table row a kept finding must get: its
// fingerprint, severity, category, status, location and its three model cells,
// each escaped as a table cell. The location is model text too, so it is a
// TableCell like the scenario and the fix.
func wantFindingRow(f publish.Finding, status string) []string {
	location := f.File
	if f.Line > 0 {
		location += ":" + strconv.Itoa(f.Line)
	}

	return []string{
		fingerprintOf(f),
		f.Severity,
		f.Category,
		status,
		publish.TableCell(location),
		publish.TableCell(f.Scenario),
		publish.TableCell(f.Fix),
	}
}

// richFindings are the findings most of the rendering tests read: two public low
// findings, one public informational one, one suspected finding and one private
// medium finding, with a pipe, a backtick and Markdown in the model's text. The
// first finding's scenario, fix and dismissal each hold a pipe, so the table
// cells it renders as cannot be mistaken for a code span.
func richFindings() []publish.Finding {
	first := finding("G304", sevLow, true, catHardening)
	first.File = "internal/cli/run.go"
	first.Line = 12
	first.Scenario = "a path | is read `without` a check"
	first.Evidence = "the trace holds the marker evidence-marker"
	first.Fix = "check the path | reject it"
	first.Dismissal = "the path is a test fixture | by design"

	second := finding("G305", sevLow, true, catConfiguration)
	second.File = "internal/cli/open.go"
	second.Line = 0
	second.Scenario = "<b>a second low finding</b>"
	second.Fix = "close the file"

	informational := finding("G307", sevInformational, true, catSupplyChain)
	informational.File = "internal/cli/version.go"
	informational.Line = 3
	informational.Scenario = "a release without an attestation"
	informational.Fix = "attest the archives"

	suspected := finding("G308", sevLow, false, catDenialOfSvc)
	suspected.File = "internal/cli/parse.go"
	suspected.Line = 7
	suspected.Scenario = "a hostile inventory of 10 GiB"
	suspected.Fix = "bound the input"

	private := finding("G309", sevMedium, true, catDependency)
	private.File = "go.mod"
	private.Line = 0
	private.Scenario = "the private scenario marker"
	private.Fix = "upgrade it"

	return []publish.Finding{first, second, informational, suspected, private}
}

// richInput is the render input of richFindings.
func richInput(t *testing.T) publish.ReportInput {
	t.Helper()

	return publish.ReportInput{
		Result:  fixtureResult(richFindings()...),
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateHandled,
	}
}

// TestRenderReportPathAndTitle pins the report's file name and its one title:
// docs/security/audits/YYYY-MM-DD-<run_id>.md, and its first line the report's
// # heading.
func TestRenderReportPathAndTitle(t *testing.T) {
	t.Parallel()

	rendered := renderReport(t, richInput(t))

	if rendered.Path != testReportPath {
		t.Errorf("RenderReport's path is %q, want %q", rendered.Path, testReportPath)
	}

	lines := strings.Split(rendered.Body, "\n")

	want := "# Security audit report, " + testDate
	if lines[0] != want {
		t.Errorf("the report's first line is %q, want %q", lines[0], want)
	}

	if got := titleLines(rendered.Body); len(got) != 1 || got[0] != 1 {
		t.Errorf("the report's # headings are on lines %v, want the one title on line 1", got)
	}
}

// TestRenderReportSections pins the report's sections: the template's, in its
// order, each a ## heading, with the needs-investigation list under Findings.
func TestRenderReportSections(t *testing.T) {
	t.Parallel()

	body := renderReport(t, richInput(t)).Body

	want := []string{
		"Summary",
		"Scope",
		"Findings",
		"Private findings",
		"Coverage",
		"Changes since the last audit",
		"False positives",
		"Limits",
	}

	var got []string

	for _, s := range parseSections(t, body) {
		got = append(got, s.Name)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the report's sections mismatch (-want +got):\n%s", diff)
	}

	findings := sectionOf(t, body, "Findings").Lines

	investigation := -1

	for i, line := range findings {
		if strings.TrimSpace(line) == investigationHeading {
			investigation = i
		}
	}

	if investigation < 0 {
		t.Fatalf("the Findings section holds no %q heading:\n%s", investigationHeading, strings.Join(findings, "\n"))
	}

	if rows := findingsTableOf(t, body); len(rows) != 4 {
		t.Errorf("the findings table holds %d rows, want the header and the three public findings", len(rows))
	}

	if rows := investigationTableOf(t, body); len(rows) != 2 {
		t.Errorf("the needs-investigation table holds %d rows, want the header and the suspected finding", len(rows))
	}
}

// TestRenderReportEmptySections pins what each section says when it holds
// nothing: No public findings. for the findings table, None. for the lists, and
// First report. for the changes when no previous report exists.
func TestRenderReportEmptySections(t *testing.T) {
	t.Parallel()

	empty := fixtureResult()
	empty.Limits = nil

	body := renderReport(t, publish.ReportInput{
		Result:  empty,
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateNone,
	}).Body

	tests := []struct {
		section string
		want    string
	}{
		{"Findings", "No public findings."},
		{"Private findings", "private findings: none"},
		{"Changes since the last audit", "First report."},
		{"False positives", "None."},
		{"Limits", "None."},
	}

	for _, tc := range tests {
		t.Run(tc.section, func(t *testing.T) {
			t.Parallel()

			text := sectionText(t, body, tc.section)
			if !strings.Contains(text, tc.want) {
				t.Errorf("the %s section reads:\n%s\nwant it to hold %q", tc.section, text, tc.want)
			}
		})
	}

	if text := sectionText(t, body, "Findings"); !strings.Contains(text, "None.") {
		t.Errorf("the needs-investigation list of an empty result reads:\n%s\nwant None.", text)
	}
}

// TestRenderReportFindings pins the findings table: one row per kept public
// finding, sorted by severity and then fingerprint, its location file:line or
// file when the line is 0, and every model cell escaped as a table cell.
func TestRenderReportFindings(t *testing.T) {
	t.Parallel()

	first := finding("G304", sevLow, true, catHardening)
	first.File = "internal/cli/run.go"
	first.Line = 12
	first.Scenario = "a path | is read"
	first.Fix = "check the path"

	second := finding("G305", sevLow, true, catConfiguration)
	second.File = "internal/cli/open.go"
	second.Line = 0
	second.Scenario = "a file | is left open"
	second.Fix = "close the file"

	informational := finding("G307", sevInformational, true, catSupplyChain)
	informational.File = "internal/cli/version.go"
	informational.Line = 3
	informational.Scenario = "a release | without an attestation"
	informational.Fix = "attest it"

	body := renderReport(t, publish.ReportInput{
		Result:  fixtureResult(first, second, informational),
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateNone,
	}).Body

	rows := tableOf(t, body, "Findings")

	wantHeader := []string{"Fingerprint", "Severity", "Category", "Status", "Location", "Scenario", "Recommended fix"}
	if diff := cmp.Diff(wantHeader, rows[0]); diff != "" {
		t.Errorf("the findings table's header mismatch (-want +got):\n%s", diff)
	}

	// The two low findings, by fingerprint, then the informational one: most
	// severe first, and within one severity by fingerprint.
	wantRows := [][]string{
		wantFindingRow(first, "new"),
		wantFindingRow(second, "new"),
		wantFindingRow(informational, "new"),
	}

	slices.SortFunc(wantRows[:2], func(a, b []string) int { return strings.Compare(a[0], b[0]) })

	if diff := cmp.Diff(wantRows, rows[1:]); diff != "" {
		t.Errorf("the findings table mismatch (-want +got):\n%s\nseverities come in the order %s", diff, severityOrder)
	}

	// Nothing is suspected, so the second list says so.
	if text := sectionText(t, body, "Findings"); !strings.Contains(text, "None.") {
		t.Errorf("the needs-investigation list reads:\n%s\nwant None.", text)
	}
}

// TestRenderReportSuspectedFindings pins that an unconfirmed low finding is in
// the needs-investigation list, with the same columns, and not in the findings
// table.
func TestRenderReportSuspectedFindings(t *testing.T) {
	t.Parallel()

	suspected := finding("G308", sevLow, false, catDenialOfSvc)
	suspected.File = "internal/cli/parse.go"
	suspected.Line = 7

	body := renderReport(t, publish.ReportInput{
		Result:  fixtureResult(suspected),
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateNone,
	}).Body

	rows := investigationTableOf(t, body)

	wantHeader := []string{"Fingerprint", "Severity", "Category", "Status", "Location", "Scenario", "Recommended fix"}
	if diff := cmp.Diff(wantHeader, rows[0]); diff != "" {
		t.Errorf("the needs-investigation table's header mismatch (-want +got):\n%s", diff)
	}

	want := wantFindingRow(suspected, "new")
	if diff := cmp.Diff([][]string{want}, rows[1:]); diff != "" {
		t.Errorf("the needs-investigation table mismatch (-want +got):\n%s", diff)
	}

	if holdsRow(findingsTableOf(t, body), fingerprintOf(suspected)) {
		t.Errorf("the findings table holds the suspected finding, want it in the needs-investigation list alone")
	}
}

// TestRenderReportFingerprintIgnoresTheLine pins that a finding's line is no
// input of its fingerprint: moving a finding down a file keeps its fingerprint
// and changes only its location.
func TestRenderReportFingerprintIgnoresTheLine(t *testing.T) {
	t.Parallel()

	first := finding("G304", sevLow, true, catHardening)
	first.Line = 12

	moved := first
	moved.Line = 99

	render := func(f publish.Finding) []string {
		body := renderReport(t, publish.ReportInput{
			Result:  fixtureResult(f),
			Run:     testRun(t, testDate, testRunID),
			Private: publish.PrivateNone,
		}).Body

		return rowHolding(t, findingsTableOf(t, body), 0, fingerprintOf(f))
	}

	before, after := render(first), render(moved)

	if before[0] != after[0] {
		t.Errorf("the fingerprint is %q at line 12 and %q at line 99, want one fingerprint: the line is no input", before[0], after[0])
	}

	if before[4] == after[4] {
		t.Errorf("both locations read %q, want the location to follow the line", before[4])
	}
}

// TestRenderReportScope pins the scope: the commit, the release and its image
// digest, one item per tool, the core's model constants and the run's URL and
// date.
func TestRenderReportScope(t *testing.T) {
	t.Parallel()

	in := richInput(t)
	scope := sectionText(t, renderReport(t, in).Body, "Scope")

	for _, want := range []string{
		testCommit,
		testTag,
		testDigest,
		in.Run.Date,
		in.Run.URL(),
		publish.ModelName,
		publish.ModelEffort,
		"go",
		"1.27.4",
		"gitleaks",
		"v8.29.0",
	} {
		if !strings.Contains(scope, want) {
			t.Errorf("the Scope section does not hold %q:\n%s", want, scope)
		}
	}

	if publish.ModelName != "claude-fable-5-2" || publish.ModelEffort != "high" {
		t.Errorf("the model constants are %q and %q, want claude-fable-5-2 and high", publish.ModelName, publish.ModelEffort)
	}
}

// TestRenderReportSummary pins what the Summary section holds: the audited
// commit, the release, the public findings by severity with their counts, the
// needs-investigation count and the private-findings line.
func TestRenderReportSummary(t *testing.T) {
	t.Parallel()

	t.Run("the public findings by severity", func(t *testing.T) {
		t.Parallel()

		body := renderReport(t, publish.ReportInput{
			Result: fixtureResult(
				finding("G304", sevLow, true, catHardening),
				finding("G305", sevLow, true, catConfiguration),
				finding("G307", sevInformational, true, catSupplyChain),
			),
			Run:     testRun(t, testDate, testRunID),
			Private: publish.PrivateNone,
		}).Body

		summary := sectionText(t, body, "Summary")

		for _, want := range []string{
			testCommit,
			testTag,
			"private findings: none",
			"2 low",
			"1 informational",
		} {
			if !strings.Contains(summary, want) {
				t.Errorf("the Summary section does not hold %q:\n%s", want, summary)
			}
		}
	})

	t.Run("the needs-investigation count", func(t *testing.T) {
		t.Parallel()

		body := renderReport(t, publish.ReportInput{
			Result:  fixtureResult(finding("G308", sevLow, false, catDenialOfSvc)),
			Run:     testRun(t, testDate, testRunID),
			Private: publish.PrivateNone,
		}).Body

		line := lineHolding(t, sectionText(t, body, "Summary"), "needs investigation")

		if !strings.Contains(line, "1") {
			t.Errorf("the Summary line %q does not hold the needs-investigation count 1", line)
		}
	})

	t.Run("a private finding is a flag and no count", func(t *testing.T) {
		t.Parallel()

		private := finding("G309", sevMedium, true, catDependency)
		private.Scenario = "the private scenario marker"
		private.Fix = "the private fix marker"

		body := renderReport(t, publish.ReportInput{
			Result:  fixtureResult(private),
			Run:     testRun(t, testDate, testRunID),
			Private: publish.PrivateHandled,
		}).Body

		if summary := sectionText(t, body, "Summary"); !strings.Contains(summary, "private findings: handled privately") {
			t.Errorf("the Summary does not hold the private-findings line:\n%s", summary)
		}

		// Nothing of the private finding's own reaches the report.
		for _, want := range []string{"the private scenario marker", "the private fix marker", "G309"} {
			if strings.Contains(body, want) {
				t.Errorf("the report holds %q, which belongs to the private finding", want)
			}
		}
	})
}

// TestRenderReportPrivateState pins the private-findings line and its errors: a
// report with a private placement and PrivateNone, or one of the other two
// states with none, is an error.
func TestRenderReportPrivateState(t *testing.T) {
	t.Parallel()

	private := finding("G309", sevMedium, true, catDependency)
	public := finding("G304", sevLow, true, catHardening)

	tests := []struct {
		name    string
		result  publish.Result
		private publish.PrivateState
		want    string
		wantErr bool
	}{
		{
			name:    "none, with no private finding",
			result:  fixtureResult(public),
			private: publish.PrivateNone,
			want:    "private findings: none",
		},
		{
			name:    "handled privately",
			result:  fixtureResult(public, private),
			private: publish.PrivateHandled,
			want:    "private findings: handled privately",
		},
		{
			name:    "not filed",
			result:  fixtureResult(public, private),
			private: publish.PrivateNotFiled,
			want:    "private findings: not filed",
		},
		{
			name:    "none with a private finding",
			result:  fixtureResult(public, private),
			private: publish.PrivateNone,
			wantErr: true,
		},
		{
			name:    "handled privately with no private finding",
			result:  fixtureResult(public),
			private: publish.PrivateHandled,
			wantErr: true,
		},
		{
			name:    "not filed with no private finding",
			result:  fixtureResult(public),
			private: publish.PrivateNotFiled,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := publish.RenderReport(publish.ReportInput{
				Result:  tc.result,
				Run:     testRun(t, testDate, testRunID),
				Private: tc.private,
			})

			if tc.wantErr {
				if err == nil {
					t.Fatalf("RenderReport returned the report %q, want an error: the private-findings line would be a lie", rendered.Body)
				}

				return
			}

			if err != nil {
				t.Fatalf("RenderReport: %v", err)
			}

			text := sectionText(t, rendered.Body, "Private findings")

			if !strings.Contains(text, tc.want) {
				t.Errorf("the Private findings section reads:\n%s\nwant it to hold %q", text, tc.want)
			}

			for _, other := range []string{"private findings: none", "private findings: handled privately", "private findings: not filed"} {
				if other != tc.want && strings.Contains(rendered.Body, other) {
					t.Errorf("the report holds %q beside %q, want exactly one private-findings line", other, tc.want)
				}
			}
		})
	}
}

// TestRenderReportModelFields pins the fragments the renderer hands the scanner:
// every model-written field it renders, escaped as the report holds it, and
// neither the script's own values nor a finding's evidence.
func TestRenderReportModelFields(t *testing.T) {
	t.Parallel()

	in := richInput(t)
	rendered := renderReport(t, in)

	fields := strings.Join(rendered.ModelFields, "\n")

	var want []string

	for _, f := range in.Result.Findings {
		if publish.Place(f).Private() {
			continue
		}

		want = append(want, publish.TableCell(f.Scenario), publish.TableCell(f.Fix))

		if f.Dismissal != "" {
			want = append(want, publish.TableCell(f.Dismissal))
		}
	}

	want = append(want, publish.CodeSpan(in.Result.ImageDigest))

	// A tool's version is model text too, in a code span; its name is the
	// script's, from the closed list.
	for _, tool := range in.Result.Tools {
		want = append(want, publish.CodeSpan(tool.Version))
	}

	// The narrative, the dependencies and the limits are list items and
	// paragraphs, not table cells: render.go writes each as a code span, so a
	// pipe stays a pipe.
	want = append(want, publish.CodeSpan(in.Result.Changes.Narrative))

	for _, dependency := range in.Result.Changes.Dependencies {
		want = append(want, publish.CodeSpan(dependency))
	}

	for _, limit := range in.Result.Limits {
		want = append(want, publish.CodeSpan(limit))
	}

	for _, fragment := range want {
		if !strings.Contains(fields, fragment) {
			t.Errorf("the renderer's model fields do not hold %q:\n%s", fragment, fields)
		}

		if !strings.Contains(rendered.Body, fragment) {
			t.Errorf("the report does not hold the model fragment %q it lists for the scanner", fragment)
		}
	}

	// The script's own values are no model field.
	for _, value := range []string{testCommit, testTag, in.Run.URL(), fingerprintOf(in.Result.Findings[0])} {
		if strings.Contains(fields, value) {
			t.Errorf("the renderer's model fields hold %q, a value the script wrote", value)
		}
	}

	// Evidence is for an advisory or an issue only, never for the report.
	evidence := "the trace holds the marker evidence-marker"

	if strings.Contains(rendered.Body, evidence) || strings.Contains(fields, evidence) {
		t.Errorf("the report holds the finding's evidence, want it in an advisory or an issue only:\n%s", rendered.Body)
	}
}

// TestRenderReportPassesTheDocsChecks pins that a rendered report is a page the
// repository's own checks accept: one # title on its first line, and no link
// but Security's ../README.md, which resolves from the audits directory, and
// the run's URL.
func TestRenderReportPassesTheDocsChecks(t *testing.T) {
	t.Parallel()

	in := richInput(t)
	body := renderReport(t, in).Body

	if got := titleLines(body); len(got) != 1 || got[0] != 1 {
		t.Errorf("the report's # headings are on lines %v, want one title on line 1", got)
	}

	allowed := map[string]bool{
		"../README.md": true,
		in.Run.URL():   true,
	}

	links := linksIn(body)

	if !slices.Contains(links, "../README.md") {
		t.Errorf("the report's links are %v, want the summary's [Security](../README.md)", links)
	}

	for _, link := range links {
		if !allowed[link] {
			t.Errorf("the report links %q, want only ../README.md and the run's URL", link)
		}
	}

	// The one relative link resolves from docs/security/audits: the page it
	// names is the Security page, four directories above this package.
	page := "../../../../docs/security/" + strings.TrimPrefix("../README.md", "../")

	if _, err := os.ReadFile(page); err != nil {
		t.Errorf("the report's [Security](../README.md) does not resolve from docs/security/audits: %v", err)
	}
}

// TestRenderReportImageDigestIsModelText pins that the model chooses the image
// digest, so the core treats it as model text: the Scope renders it as a code
// span, the renderer lists it among the model fields, and the scan therefore
// refuses a digest whose hex decodes to printable ASCII by the hex rule, while
// the fixture's own digest is published.
func TestRenderReportImageDigestIsModelText(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	tests := []struct {
		name        string
		digest      string
		wantRefused bool
	}{
		{
			name:   "the fixture's digest is published",
			digest: testDigest,
		},
		{
			name:        "a digest whose hex decodes to text is refused",
			digest:      "sha256:" + hex.EncodeToString([]byte(strings.Repeat("A", 32))),
			wantRefused: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := richInput(t)
			in.Result.ImageDigest = tc.digest

			rendered := renderReport(t, in)

			if scope := sectionText(t, rendered.Body, "Scope"); !strings.Contains(scope, publish.CodeSpan(tc.digest)) {
				t.Errorf("the Scope section does not hold the digest as the code span %q:\n%s", publish.CodeSpan(tc.digest), scope)
			}

			if !slices.Contains(rendered.ModelFields, publish.CodeSpan(tc.digest)) {
				t.Errorf("the renderer's model fields do not list the digest %q: %v", publish.CodeSpan(tc.digest), rendered.ModelFields)
			}

			err := publish.ScanPublic(rendered.Body, rendered.ModelFields, key)

			if tc.wantRefused {
				wantRefused(t, err, tc.digest)

				return
			}

			wantAllowed(t, err)
		})
	}
}

// TestRenderReportRefusesAZeroPrevious pins that RenderReport renders only
// from a previous report ParsePreviousReport returned: a zero Previous carries
// no name and no date a parsed report has, and the internal test pins the
// hand-built one that carries both.
func TestRenderReportRefusesAZeroPrevious(t *testing.T) {
	t.Parallel()

	zero := &publish.Previous{}

	rendered, err := publish.RenderReport(publish.ReportInput{
		Result:   fixtureResult(),
		Run:      testRun(t, testDate, testRunID),
		Private:  publish.PrivateNone,
		Previous: zero,
	})
	if err == nil {
		t.Fatalf("RenderReport with a zero Previous returned the report %q, want an error wrapping ErrPreviousReport", rendered.Body)
	}

	if !errors.Is(err, publish.ErrPreviousReport) {
		t.Errorf("RenderReport with a zero Previous: error %v does not wrap ErrPreviousReport", err)
	}
}

// TestRenderTemplates pins the three fixed templates byte for byte: the branch,
// the pull request's title and body, and the commit message. No model text
// reaches them, and the total counts the kept public findings alone.
func TestRenderTemplates(t *testing.T) {
	t.Parallel()

	in := publish.ReportInput{
		Result: fixtureResult(
			finding("G304", sevLow, true, catHardening),
			finding("G305", sevInformational, true, catConfiguration),
			finding("G309", sevMedium, true, catDependency),
		),
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateHandled,
	}

	templates, err := publish.RenderTemplates(in)
	if err != nil {
		t.Fatalf("RenderTemplates: %v", err)
	}

	runURL := in.Run.URL()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "the branch",
			got:  templates.Branch,
			want: "security-audit/" + testDate + "-" + testRunID,
		},
		{
			name: "the pull request's title",
			got:  templates.PRTitle,
			want: "Security audit report, " + testDate + " (run " + testRunID + ")",
		},
		{
			name: "the pull request's body",
			got:  templates.PRBody,
			want: "Security audit report of run " + testRunID + ", " + testDate + " (UTC).\n" +
				"\n" +
				"Commit: " + testCommit + "\n" +
				"Release: " + testTag + "\n" +
				"Public findings: 2\n" +
				"private findings: handled privately\n" +
				"Run: " + runURL + "\n" +
				"\n" +
				"The report adds " + testReportPath + " and nothing else.",
		},
		{
			name: "the commit message",
			got:  templates.CommitMessage,
			want: "docs(security): audit report " + testDate + ", run " + testRunID + "\n" +
				"\n" +
				"Commit " + testCommit + ", release " + testTag + ", public findings 2, private findings: handled privately.\n" +
				"Run: " + runURL,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, tc.got); diff != "" {
				t.Errorf("%s mismatch (-want +got):\n%s", tc.name, diff)
			}
		})
	}
}

// TestRenderTemplatesCountTheSuspectedFindings pins the total the templates
// carry: the kept findings placed public or needing investigation.
func TestRenderTemplatesCountTheSuspectedFindings(t *testing.T) {
	t.Parallel()

	templates, err := publish.RenderTemplates(publish.ReportInput{
		Result: fixtureResult(
			finding("G304", sevLow, true, catHardening),
			finding("G305", sevLow, false, catDenialOfSvc),
			finding("G307", sevInformational, true, catSupplyChain),
		),
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateNone,
	})
	if err != nil {
		t.Fatalf("RenderTemplates: %v", err)
	}

	if !strings.Contains(templates.PRBody, "Public findings: 3") {
		t.Errorf("the pull request's body does not count the suspected finding:\n%s", templates.PRBody)
	}
}

// TestRenderTemplatesPrivateState pins that the templates refuse the same
// private-finding states the report does: they carry the line too.
func TestRenderTemplatesPrivateState(t *testing.T) {
	t.Parallel()

	_, err := publish.RenderTemplates(publish.ReportInput{
		Result:  fixtureResult(finding("G309", sevMedium, true, catDependency)),
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateNone,
	})
	if err == nil {
		t.Fatalf("RenderTemplates with a private finding and PrivateNone: want an error")
	}
}

// TestNewRun pins the run's identity: its URL, and every refusal of a server
// URL, a repository, a run ID or a date the workflow could not have handed it.
func TestNewRun(t *testing.T) {
	t.Parallel()

	t.Run("the run's URL", func(t *testing.T) {
		t.Parallel()

		run := testRun(t, testDate, testRunID)

		want := testServerURL + "/" + testRepository + "/actions/runs/" + testRunID
		if got := run.URL(); got != want {
			t.Errorf("Run.URL() = %q, want %q", got, want)
		}

		if run.Date != testDate || run.ID != testRunID || run.Repository != testRepository || run.ServerURL != testServerURL {
			t.Errorf("Run = %+v, want the values it was built from", run)
		}
	})

	tests := []struct {
		name       string
		serverURL  string
		repository string
		runID      string
		date       string
		wantErr    bool
	}{
		{"the workflow's own values", testServerURL, testRepository, testRunID, testDate, false},
		{"a GitHub Enterprise host", "https://github.example.com", "example/shift2eu", "1", "2026-01-01", false},
		{"a leap day", testServerURL, testRepository, "1", "2024-02-29", false},

		{"a server without https", "http://github.com", testRepository, testRunID, testDate, true},
		{"a server with no scheme", "github.com", testRepository, testRunID, testDate, true},
		{"a server with no host", "https://", testRepository, testRunID, testDate, true},
		{"an empty server", "", testRepository, testRunID, testDate, true},
		{"a server with a path only", "https:///example", testRepository, testRunID, testDate, true},

		{"a repository with no owner", "https://github.com", "shift2eu", testRunID, testDate, true},
		{"a repository with two slashes", "https://github.com", "example/shift2eu/extra", testRunID, testDate, true},
		{"a repository with a space", "https://github.com", "example/shift 2eu", testRunID, testDate, true},
		{"a repository with a byte the pattern does not allow", "https://github.com", "example/shift2eu!", testRunID, testDate, true},
		{"an empty repository", "https://github.com", "", testRunID, testDate, true},

		{"a run ID of 0", testServerURL, testRepository, "0", testDate, true},
		{"a run ID with a leading zero", testServerURL, testRepository, "0123", testDate, true},
		{"a run ID of 21 digits", testServerURL, testRepository, "123456789012345678901", testDate, true},
		{"a run ID that is not a number", testServerURL, testRepository, "12a", testDate, true},
		{"a negative run ID", testServerURL, testRepository, "-1", testDate, true},
		{"an empty run ID", testServerURL, testRepository, "", testDate, true},

		{"a date that is no date", testServerURL, testRepository, testRunID, "2026-02-30", true},
		{"a date without its padding", testServerURL, testRepository, testRunID, "2026-1-1", true},
		{"a month past 12", testServerURL, testRepository, testRunID, "2026-13-01", true},
		{"a timestamp", testServerURL, testRepository, testRunID, "2026-11-23T00:00:00Z", true},
		{"an empty date", testServerURL, testRepository, testRunID, "", true},
		{"a date that is no leap day", testServerURL, testRepository, testRunID, "2026-02-29", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run, err := publish.NewRun(tc.serverURL, tc.repository, tc.runID, tc.date)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewRun(%q, %q, %q, %q) = %+v, want an error wrapping ErrConfig", tc.serverURL, tc.repository, tc.runID, tc.date, run)
				}

				if !errors.Is(err, publish.ErrConfig) {
					t.Errorf("NewRun: error %v does not wrap ErrConfig", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("NewRun(%q, %q, %q, %q): %v", tc.serverURL, tc.repository, tc.runID, tc.date, err)
			}

			want := tc.serverURL + "/" + tc.repository + "/actions/runs/" + tc.runID
			if run.URL() != want {
				t.Errorf("Run.URL() = %q, want %q", run.URL(), want)
			}
		})
	}
}

// TestRenderReportIsDeterministic pins that two renders of one input agree: a
// report a run cannot reproduce byte for byte is no report.
func TestRenderReportIsDeterministic(t *testing.T) {
	t.Parallel()

	first := renderReport(t, richInput(t))
	second := renderReport(t, richInput(t))

	if diff := cmp.Diff(first.Body, second.Body); diff != "" {
		t.Errorf("two renders of one input differ (-first +second):\n%s", diff)
	}

	if diff := cmp.Diff(first.Path, second.Path); diff != "" {
		t.Errorf("two renders of one input name different paths (-first +second):\n%s", diff)
	}

	if diff := cmp.Diff(first.ModelFields, second.ModelFields); diff != "" {
		t.Errorf("two renders of one input list different model fields (-first +second):\n%s", diff)
	}
}
