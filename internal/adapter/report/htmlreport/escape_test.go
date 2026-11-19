package htmlreport_test

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/internal/adapter/report/htmlreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

// The reporter does not use html/template: its reflection keeps the site's
// module over the size budget (docs/adr/0035). It writes constant tags itself
// and escapes every value with a strings.Replacer that must be html/template's
// replacement for element text, character for character (fragment.go,
// textEscaper), after invalid UTF-8 and control characters have become U+FFFD
// (fragment.go, clean). These tests hold that claim: the fragment shows a value
// exactly as html/template renders the cleaned value in the one context a value
// reaches, the text of a cell, so the two cannot drift apart. html/template is
// imported by this file alone, so it never reaches the module.

// cellTemplate renders a value where a report puts one: in the text of a table
// cell, not in a tag, an attribute, a URL, a script or a style.
var cellTemplate = template.Must(template.New("cell").Parse("<td>{{.}}</td>"))

// escapingValues are the values both tests hold the reporter to. They are the
// characters and constructs that escape a value's context in a page, the ones
// that must not survive as themselves, and the shapes that break an escaper:
// invalid UTF-8, lone control characters, line separators, an astral rune and a
// long run of markup characters.
var escapingValues = []struct {
	name  string
	value string
}{
	{name: "a plain value", value: "bucket-example"},
	{name: "a double quote", value: `a"b`},
	{name: "an ampersand", value: `a&b`},
	{name: "an apostrophe", value: `a'b`},
	{name: "a plus", value: `a+b`},
	{name: "a script element", value: `<script>alert(1)</script>`},
	{name: "a closing cell", value: `</td>`},
	{name: "an event handler attribute", value: `onerror=alert(1)`},
	{name: "a javascript URL", value: `javascript:alert(1)`},
	{name: "a quoted attribute injection", value: `" onmouseover="alert(1)`},
	{name: "a NUL", value: "a\x00b"},
	{name: "a C0 control character", value: "a\x01b"},
	{name: "a C1 control character", value: "a\u0085b"},
	{name: "DEL", value: "a\u007fb"},
	{name: "a tab and line endings", value: "a\tb\nc\r\nd"},
	{name: "invalid UTF-8", value: "a\xff\xfeb"},
	{name: "line and paragraph separators", value: "a\u2028b\u2029c"},
	{name: "an astral emoji", value: "a\U0001F600b"},
	{name: "a long run of markup characters", value: strings.Repeat(`<b>&"' +`, 200)},
	{name: "everything at once", value: "<script>\x00\xff\"'&+</td>\u2028\U0001F600\u0085"},
}

// TestReportEscapingMatchesHTMLTemplate pins that a value reaches the fragment
// escaped exactly as html/template escapes it in cell text, for every value a
// page must not take as markup.
func TestReportEscapingMatchesHTMLTemplate(t *testing.T) {
	t.Parallel()

	for _, tc := range escapingValues {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkEscaping(t, tc.value)
		})
	}
}

// FuzzReportEscaping runs the same check on arbitrary values: the reporter's
// escaping must be html/template's for any string, not only for the values
// above. Under make test it runs its seed corpus only, so it stays
// deterministic.
func FuzzReportEscaping(f *testing.F) {
	for _, tc := range escapingValues {
		f.Add(tc.value)
	}

	f.Fuzz(func(t *testing.T, value string) {
		checkEscaping(t, value)
	})
}

// checkEscaping fails unless the report of an assessment whose every text value
// is value shows that value escaped as html/template escapes its cleaned form,
// and holds no markup but the reporter's own tags.
func checkEscaping(t *testing.T, value string) {
	t.Helper()

	a, meta := escapingAssessment(value)

	var out bytes.Buffer
	if err := htmlreport.New().Report(t.Context(), &out, a, meta); err != nil {
		t.Fatalf("Report() with the value %q returned unexpected error: %v", value, err)
	}

	got := out.String()

	// An escaped value can open no element and carry no attribute, so anything
	// that is not one of the reporter's own tags reached the fragment unescaped.
	checkNoInjectedMarkup(t, out.Bytes())

	// What the fragment must show for value: html/template's rendering of the
	// cleaned value, in the cell context the fixture puts it in. Cleaning is
	// what makes the comparison possible at all, since html/template leaves a
	// control character other than NUL untouched, and the reporter cleans them
	// first; see cleanedAsTheReporterCleans.
	want := templateText(t, cleanedAsTheReporterCleans(value))
	if want == "" {
		// Only the empty value cleans to nothing, and an empty value renders as
		// an empty cell: the reporter writes no code element for it (fragment.go,
		// rich.code), so there is no escaping to compare. The markup check above
		// still holds the report.
		return
	}

	if got := resourceIDCell(t, out.String()); got != want {
		t.Errorf("the fragment shows the value %q as %q in its ID cell, want %q, which is how html/template "+
			"renders it in cell text:\n%s", value, got, want, out.String())
	}

	// The value reaches the fragment in a heading, a list item and several cells
	// of the same report, each alone in its element, so its escaped form stands
	// on its own at every one of them.
	if !strings.Contains(got, want) {
		t.Errorf("the fragment does not show the value %q as %q anywhere:\n%s", value, want, got)
	}
}

// templateText returns value as html/template writes it in cell text.
func templateText(t *testing.T, value string) string {
	t.Helper()

	var b strings.Builder
	if err := cellTemplate.Execute(&b, value); err != nil {
		t.Fatalf("html/template.Execute() with the value %q returned unexpected error: %v", value, err)
	}

	cell, ok := strings.CutPrefix(b.String(), "<td>")
	if !ok {
		t.Fatalf("html/template wrote %q, want a cell", b.String())
	}

	cell, ok = strings.CutSuffix(cell, "</td>")
	if !ok {
		t.Fatalf("html/template wrote %q, want a cell", b.String())
	}

	return cell
}

// cleanedAsTheReporterCleans returns value as the reporter cleans it before
// escaping, the rule this test compares against (fragment.go, clean): each run
// of invalid UTF-8 bytes becomes one U+FFFD (strings.ToValidUTF8), then a tab, a
// line feed and a carriage return each become a space and every other C0 control
// character, every C1 control character and DEL becomes U+FFFD. The report
// writes the cleaned value, so comparing against this form is what states that
// no control character and no invalid byte survives into the page, while the
// escaping itself is html/template's.
func cleanedAsTheReporterCleans(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			return '\uFFFD'
		default:
			return r
		}
	}, strings.ToValidUTF8(value, "\uFFFD"))
}

// resourceIDCell returns the text of the first data cell of the report's
// resources table: the ID of the first resource, which is a value and is shown
// as code.
func resourceIDCell(t *testing.T, got string) string {
	t.Helper()

	const (
		open  = "<tr><td><code>"
		close = "</code></td>"
	)

	// The summary table's cells are the report's own words, never code, so the
	// first code cell of a row is the resources table's first ID.
	start := strings.Index(got, open)
	if start < 0 {
		t.Fatalf("the fragment holds no %q, so the resources table has no ID cell:\n%s", open, got)
	}

	rest := got[start+len(open):]

	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("the fragment's ID cell does not end with %q:\n%s", close, got)
	}

	return rest[:end]
}

// escapingAssessment builds an assessment whose every text value is value: the
// target, the source, the catalog's URL and default region, the inventory file
// name, one resource's ID, raw type, region and workload, its best offering,
// the reasons and blockers that name it, and its workload group. Whatever the
// reporter's path, the value it writes came from one of these.
func escapingAssessment(value string) (assessment.Assessment, analysis.ReportMeta) {
	reason := mapping.Reason{Code: mapping.NoOffering, Target: value, Category: taxonomy.ComputeVM}
	blocker := assessment.Blocker{ResourceID: value, Reason: reason}
	summary := assessment.Summary{None: 1, Total: 1}

	a := assessment.Assessment{
		Target: value,
		Snapshot: catalog.Snapshot{
			SourceURL:     value,
			RetrievedOn:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			DefaultRegion: value,
		},
		Source: value,
		Verdicts: []assessment.Verdict{
			{
				Resource: inventory.Resource{
					ID:       value,
					RawType:  value,
					Category: taxonomy.ComputeVM,
					Region:   value,
					Workload: value,
				},
				Fit:          mapping.None,
				BestOffering: value,
				Reasons:      []mapping.Reason{reason},
			},
		},
		Blockers: []assessment.Blocker{blocker},
		Summary:  summary,
		Region:   value,
		Workloads: []assessment.WorkloadSummary{
			{
				Name:     value,
				Summary:  summary,
				Blockers: []assessment.Blocker{blocker},
				ResourceIDs: []string{
					value,
				},
			},
		},
	}

	return a, analysis.ReportMeta{InventoryName: value}
}
