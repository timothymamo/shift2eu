package pdfreport_test

// The hostile report and hostile comparison: values that spell the PDF's own
// syntax in every field the PDF shows. For each value the structural invariants
// hold, the value is drawn as exactly its escaped form, joined across its
// wrapped lines and page breaks, and each line's literal string holds (, ) and \
// escaped.

import (
	"bytes"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// TestReportHostile pins that no hostile value can become PDF syntax: the file
// parses, holds only the writer's objects, names, fonts, streams and operators,
// and shows every value as its escaped text.
func TestReportHostile(t *testing.T) {
	t.Parallel()

	a := assessment.Assess(newInventory(t, hostileSource, hostileResources()), hostileCatalog(t, hostileTarget))
	meta := analysis.ReportMeta{InventoryName: hostileInventoryName}

	out := renderReport(t, a, meta)
	testutil.Golden(t, "testdata/report-hostile.golden.pdf", out)

	file := checkPDFStructure(t, out)
	assertTitleBlock(t, file, hostileTarget)
	assertPageFurniture(t, file, hostileTarget)

	// The retrieval date is a UTC calendar date: the snapshot was taken just
	// after midnight in a zone east of UTC, so a report that forgets UTC shows
	// the day after.
	assertShown(t, file, "2026-11-01")
	assertNotShown(t, file, "2026-11-02")

	assertHostileValues(t, file, hostileValues())
}

// TestReportComparisonHostile pins the same for a comparison of two hostile
// catalogs: both target display names, and every value, are shown as text.
func TestReportComparisonHostile(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(hostileComparisonTargets(t))

	out := renderComparison(t, c, analysis.ReportMeta{InventoryName: hostileInventoryName})
	testutil.Golden(t, "testdata/comparison-hostile.golden.pdf", out)

	file := checkPDFStructure(t, out)
	assertComparisonTitle(t, file, c)
	assertPageFurniture(t, file, "comparison of 2 targets")

	assertHostileValues(t, file, hostileComparisonValues())
}

// assertHostileValues checks, for every hostile value, that the document draws
// its escaped form whole, and that every line of it is a literal string with (
// ), ) and \ escaped the way the writer writes them.
func assertHostileValues(tb testing.TB, file *testPDF, values []string) {
	tb.Helper()

	for _, value := range values {
		wrapped := checkWrappedValue(tb, file, value)

		for _, chunk := range wrapped.chunks {
			assertLiteralString(tb, file, chunk)
		}
	}
}

// assertLiteralString checks that some content stream holds text as the literal
// string the writer's escaping produces, so a value's ( ) and \ are escaped and
// never read as syntax.
func assertLiteralString(tb testing.TB, file *testPDF, text string) {
	tb.Helper()

	want := []byte(pdfreport.PDFString(text))

	for _, obj := range file.order {
		if obj.hasStream && bytes.Contains(obj.stream, want) {
			return
		}
	}

	tb.Errorf("no content stream holds the literal string %s for the text %q; a value with ( ) or \\ in it is not escaped the way the content stream needs",
		want, text)
}
