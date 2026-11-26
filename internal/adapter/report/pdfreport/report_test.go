package pdfreport_test

// The single-target report: the ten golden files, the sections each variant
// shows and does not show, the text extracted from each page, the page header
// and footer, and the header row a table repeats when it breaks.

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// analysisSections is the order of the sections every single-target report
// shows: the Markdown report's sections, then the detail sections the PDF adds
// (PRD, Single-target report). A report of an assessment with workloads adds the
// Workloads section, and one with estimates the Cost estimate section.
func analysisSections() []string {
	return []string{
		"Summary",
		"Resources",
		"Required changes",
		"Blockers",
		"Resource details",
		"How this report is worked out",
		"About this report",
	}
}

// estimateSections is analysisSections with the Workloads and Cost estimate
// sections, in the order the PRD puts them.
func estimateSections() []string {
	return []string{
		"Summary",
		"Resources",
		"Required changes",
		"Blockers",
		"Workloads",
		"Cost estimate",
		"Resource details",
		"How this report is worked out",
		"About this report",
	}
}

// workloadSections is analysisSections with the Workloads section and without the
// Cost estimate section, for an assessment with workloads but no estimates.
func workloadSections() []string {
	return []string{
		"Summary",
		"Resources",
		"Required changes",
		"Blockers",
		"Workloads",
		"Resource details",
		"How this report is worked out",
		"About this report",
	}
}

// TestReport pins the whole single-target PDF against its golden file and checks
// the structural invariants, the page header and footer and the title block of
// every variant: with estimates and without, with a region chosen, with
// workloads, and with an empty inventory.
func TestReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        assessment.Assessment
		meta     analysis.ReportMeta
		golden   string
		sections []string
	}{
		{
			name:     "estimates on",
			a:        estimateAssessment(t, "", true),
			meta:     analysis.ReportMeta{InventoryName: inventoryFileName},
			golden:   "testdata/report-estimates.golden.pdf",
			sections: estimateSections(),
		},
		{
			name:     "estimates off",
			a:        estimateAssessment(t, "", false),
			meta:     analysis.ReportMeta{InventoryName: inventoryFileName},
			golden:   "testdata/report-no-estimates.golden.pdf",
			sections: workloadSections(),
		},
		{
			name:     "region chosen with --region",
			a:        estimateAssessment(t, testutil.ReportSecondRegion, true),
			meta:     analysis.ReportMeta{InventoryName: inventoryFileName},
			golden:   "testdata/report-region.golden.pdf",
			sections: estimateSections(),
		},
		{
			name:     "with workloads",
			a:        assessment.Assess(newInventory(t, sourceName, workloadResources()), testutil.ReportCatalog(t)),
			meta:     analysis.ReportMeta{InventoryName: inventoryFileName},
			golden:   "testdata/report-workloads.golden.pdf",
			sections: workloadSections(),
		},
		{
			name:     "empty inventory",
			a:        assessment.AssessWith(newInventory(t, sourceName, nil), testutil.ReportCatalog(t), assessment.Options{Estimate: true}),
			meta:     analysis.ReportMeta{InventoryName: inventoryFileName},
			golden:   "testdata/report-empty.golden.pdf",
			sections: analysisSections(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			out := renderReport(t, test.a, test.meta)
			testutil.Golden(t, test.golden, out)

			file := checkPDFStructure(t, out)
			assertTitleBlock(t, file, test.a.Target)
			assertPageFurniture(t, file, test.a.Target)
			assertOrder(t, file, test.sections...)

			if test.meta.InventoryName != "" {
				assertShown(t, file, "Inventory file:", test.meta.InventoryName)
			} else {
				assertNotShown(t, file, "Inventory file:")
			}

			assertShown(t, file, "Source:", test.a.Source, "Catalog:", test.a.Snapshot.SourceURL, reportRetrievedOn)
		})
	}
}

// TestReportText checks the text extracted from the single-target report with
// estimates on: every resource's ID, fit and best offering, every candidate with
// its fit and missing capabilities, the blockers, the required changes, each
// estimate's breakdown, How this report is worked out, and About this report
// with the version line and the snapshot's source URL and retrieval date.
func TestReportText(t *testing.T) {
	t.Parallel()

	a := estimateAssessment(t, "", true)
	file := checkPDFStructure(t, renderReport(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	assertAssessmentShown(t, file, a, true)

	// The Cost estimate section gives the total, in the Markdown report's words.
	if a.Total == nil {
		t.Fatalf("the assessment with estimates on holds no total")
	}

	assertShown(t, file, euros(a.Total.Monthly))

	// Every candidate's fit and missing capabilities, and the resource's
	// required changes, are shown in Resource details.
	for _, v := range a.Verdicts {
		for _, candidate := range v.Candidates {
			assertShown(t, file, candidate.Offering.Name)
		}

		for _, change := range v.ChangesToList() {
			assertShown(t, file, string(change))
		}
	}

	// How this report is worked out explains the fits, the best offering and its
	// ties, required changes, blockers, workloads, estimates and the escapes, and
	// says that there is no readiness score.
	how := sectionText(t, file, "How this report is worked out", "About this report")
	for _, want := range []string{"direct", "partial", "tie", "required change", "blocker", "workload", "730", "Courier", "readiness score"} {
		if !strings.Contains(strings.ToLower(how), strings.ToLower(want)) {
			t.Errorf("How this report is worked out does not explain %q; the section is:\n%s", want, how)
		}
	}

	// About this report names the build and what produced the report: the version
	// line, the snapshot's source URL and retrieval date, the options and the
	// inventory.
	about := sectionText(t, file, "About this report", "")
	for _, want := range []string{
		versionLine,
		a.Snapshot.SourceURL,
		reportRetrievedOn,
		inventoryFileName,
		a.Source,
		a.Target,
		a.EstimateRegion,
		"Estimates: on",
	} {
		if !strings.Contains(about, squash(want)) {
			t.Errorf("About this report does not name %q; the section is:\n%s", want, about)
		}
	}
}

// assertAssessmentShown checks that the document shows every resource's ID, fit
// and best offering, its candidates with their missing capabilities, its
// blockers and its estimate.
func assertAssessmentShown(tb testing.TB, file *testPDF, a assessment.Assessment, estimated bool) {
	tb.Helper()

	for _, verdict := range a.Verdicts {
		assertShown(tb, file, verdict.Resource.ID, string(verdict.Fit))

		if verdict.BestOffering != "" {
			assertShown(tb, file, verdict.BestOffering)
		}

		for _, candidate := range verdict.Candidates {
			assertShown(tb, file, candidate.Offering.Name)

			for _, capability := range candidate.Missing {
				assertShown(tb, file, string(capability))
			}
		}

		for _, blocker := range verdict.Reasons {
			assertShown(tb, file, blocker.String())
		}

		if estimated {
			assertEstimateShown(tb, file, verdict.Estimate)
		}
	}

	for _, blocker := range a.Blockers {
		assertShown(tb, file, blocker.Reason.String())
	}
}

// assertEstimateShown checks that the document shows one estimate's breakdown:
// the plan or the largest plan, the parts and the monthly total, the assumed
// count and hours, or "not estimated" and the reason in plain words.
func assertEstimateShown(tb testing.TB, file *testPDF, e *estimate.Estimate) {
	tb.Helper()

	if e == nil {
		tb.Errorf("a verdict with estimates on holds no estimate")

		return
	}

	if !e.Estimated {
		assertShown(tb, file, "not estimated", plainReason(e))

		if e.Largest != nil {
			assertShown(tb, file, e.Largest.Name)
		}

		return
	}

	assertShown(tb, file, euros(e.Monthly))

	if e.Plan != nil {
		assertShown(tb, file, e.Plan.Name)
	}

	for _, part := range []estimate.Money{e.Breakdown.Plan, e.Breakdown.Storage, e.Breakdown.FixedFee} {
		if part.IsZero() {
			continue
		}

		assertShown(tb, file, euros(part))
	}

	if e.CountAssumed || e.HoursAssumed {
		assertShown(tb, file, "assumed")
	}
}

// plainReason is why an estimate was not made, in the plain words the Markdown
// report uses (internal/adapter/report/markdown/estimate.go), which the PDF
// plainReason is why an estimate was not made, in the plain words the Markdown
// report uses (internal/adapter/report/markdown/estimate.go), which the PDF
// follows.
func plainReason(e *estimate.Estimate) string {
	switch e.Reason {
	case estimate.NoFit:
		return "no fit"
	case estimate.UsagePriced:
		return "usage-priced"
	case estimate.NeverPriced:
		return "not priced"
	case estimate.NoSize:
		return "size unknown"
	case estimate.NoPlan:
		return "no plan"
	case estimate.NoPlanLargeEnough:
		return "no plan large enough"
	case estimate.NotOffered:
		return "not offered in " + e.Region
	case estimate.NoPrice:
		return "no price in " + e.Region
	case estimate.NoStoragePrice:
		return "no storage price in " + e.Region
	default:
		return string(e.Reason)
	}
}

// euros renders an amount as the Markdown report words it, which the PDF
// follows: the euro sign and a comma between each group of three digits, so
// 2500.00 is €2,500.00.
func euros(m estimate.Money) string {
	return "\u20ac" + thousands(m.String())
}

// thousands puts a comma between each group of three digits of a plain decimal,
// as estimate.Money.String writes it.
func thousands(decimal string) string {
	whole, fraction, found := strings.Cut(decimal, ".")

	var b strings.Builder

	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}

		b.WriteRune(digit)
	}

	if found {
		b.WriteByte('.')
		b.WriteString(fraction)
	}

	return b.String()
}

// TestReportWithoutEstimates checks the --no-estimate variant: no plan, no
// amount, no Cost estimate section and no breakdown, and the sentence that
// estimates were off.
func TestReportWithoutEstimates(t *testing.T) {
	t.Parallel()

	withEstimates := estimateAssessment(t, "", true)
	without := estimateAssessment(t, "", false)

	file := checkPDFStructure(t, renderReport(t, without, analysis.ReportMeta{InventoryName: inventoryFileName}))

	assertOrder(t, file, workloadSections()...)
	assertNotShown(t, file, "Cost estimate")
	assertNotShown(t, file, "not estimated")
	assertNotShown(t, file, "no plan large enough")
	assertNotShown(t, file, "usage-priced")
	assertNotShown(t, file, "assumed")
	assertNotShown(t, file, "\u20ac")

	for _, verdict := range withEstimates.Verdicts {
		e := verdict.Estimate
		if e == nil {
			continue
		}

		assertNotShown(t, file, thousands(e.Monthly.String()))

		if e.Plan != nil {
			assertNotShown(t, file, e.Plan.Name)
		}

		if e.Largest != nil {
			assertNotShown(t, file, e.Largest.Name)
		}
	}

	// The report says estimates were off, in one sentence, in How this report is
	// worked out.
	how := strings.ToLower(sectionText(t, file, "How this report is worked out", "About this report"))
	if !strings.Contains(how, "estimates were off") && !strings.Contains(how, "estimates are off") {
		t.Errorf("How this report is worked out does not say that estimates were off; the section is:\n%s", how)
	}

	about := sectionText(t, file, "About this report", "")
	if !strings.Contains(about, squash("Estimates: off")) {
		t.Errorf("About this report does not say that estimates were off; the section is:\n%s", about)
	}
}

// TestReportRegion checks the --region variant: the region line says "chosen
// with --region" and names the region, and About this report names it too.
func TestReportRegion(t *testing.T) {
	t.Parallel()

	a := estimateAssessment(t, testutil.ReportSecondRegion, true)
	file := checkPDFStructure(t, renderReport(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	assertShown(t, file, "chosen with --region", testutil.ReportSecondRegion)

	about := sectionText(t, file, "About this report", "")
	if !strings.Contains(about, testutil.ReportSecondRegion) {
		t.Errorf("About this report does not name the region chosen with --region (%q); the section is:\n%s",
			testutil.ReportSecondRegion, about)
	}
}

// TestReportVersionLineComesFromTheConstructor checks that the version line in
// every footer and in About this report is the line New took, never a constant of
// the writer and never a build stamp.
func TestReportVersionLineComesFromTheConstructor(t *testing.T) {
	t.Parallel()

	a := estimateAssessment(t, "", false)

	for _, line := range []string{versionLine, otherVersionLine} {
		out := renderReportWithVersion(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}, line)
		file := checkPDFStructure(t, out)

		if len(file.pages) == 0 {
			t.Fatalf("the PDF has no pages")
		}

		for page := range file.pages {
			text := squash(file.pageText(page))
			if !strings.Contains(text, squash(line)) {
				t.Errorf("page %d's footer does not carry the version line %q the constructor took; its text is:\n%s",
					page+1, line, text)
			}

			if strings.Contains(text, squash(otherVersionLine)) && line == versionLine {
				t.Errorf("page %d shows the other version line; its text is:\n%s", page+1, text)
			}
		}

		about := sectionText(t, file, "About this report", "")
		if !strings.Contains(about, squash(line)) {
			t.Errorf("About this report does not carry the version line %q; the section is:\n%s", line, about)
		}
	}
}

// TestReportLong pins the long report: a table longer than a page breaks inside
// itself and repeats its header row on every page it continues on.
func TestReportLong(t *testing.T) {
	t.Parallel()

	a := assessment.Assess(newInventory(t, sourceName, longResources()), testutil.ReportCatalog(t))
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	out := renderReport(t, a, meta)
	testutil.Golden(t, "testdata/report-long.golden.pdf", out)

	file := checkPDFStructure(t, out)
	assertTitleBlock(t, file, a.Target)
	assertPageFurniture(t, file, a.Target)
	assertOrder(t, file, analysisSections()...)

	if len(file.pages) < 2 {
		t.Fatalf("the long report has %d page(s), so it does not break inside its resources table", len(file.pages))
	}

	assertTableHeaderRepeats(t, file, "Resources", "Required changes", []string{"ID", "Category", "Best offering"})

	// Every resource of the long inventory is drawn, the first and the last too.
	assertShown(t, file, a.Verdicts[0].Resource.ID, a.Verdicts[len(a.Verdicts)-1].Resource.ID)
}

// TestReportEmptyInventory checks the empty inventory's text: no resources, no
// workload section and "None." where the lists are empty.
func TestReportEmptyInventory(t *testing.T) {
	t.Parallel()

	a := assessment.AssessWith(newInventory(t, sourceName, nil), testutil.ReportCatalog(t), assessment.Options{Estimate: true})
	file := checkPDFStructure(t, renderReport(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	assertOrder(t, file, analysisSections()...)
	assertShown(t, file, inventoryFileName, "None.")
}

// TestReportNoInventoryName checks that the inventory file line is left out when
// the metadata holds no name, exactly as the Markdown report leaves it out.
func TestReportNoInventoryName(t *testing.T) {
	t.Parallel()

	file := checkPDFStructure(t, renderReport(t, estimateAssessment(t, "", false), analysis.ReportMeta{}))

	assertNotShown(t, file, "Inventory file:")
	assertShown(t, file, "Source:", sourceName)
}
