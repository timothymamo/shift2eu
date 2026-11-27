package pdfreport_test

// The comparison report: the ten golden files' two comparison cases, the order
// of the sections, each target's section, the shared blockers, About this report
// covering every target, and the errors of a comparison with no target or with
// targets that assess different resources.

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// TestReportComparison pins the whole comparison PDF against its golden file and
// checks the structural invariants, the page header and footer, the title block
// and the order of the sections for a comparison of two targets and of three.
func TestReportComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inventory inventory.Inventory
		keys      []string
		golden    string
	}{
		{
			name:      "three targets with blockers every target shares",
			inventory: newInventory(t, sourceName, comparisonResources()),
			keys:      threeTargetKeys(),
			golden:    "testdata/comparison-three.golden.pdf",
		},
		{
			name:      "two targets without a shared blocker",
			inventory: newInventory(t, sourceName, twoTargetResources()),
			keys:      []string{ovhcloudKey, scalewayKey},
			golden:    "testdata/comparison-two.golden.pdf",
		},
	}

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := assessment.Compare(assessTargets(t, test.inventory, test.keys))

			out := renderComparison(t, c, meta)
			testutil.Golden(t, test.golden, out)

			file := checkPDFStructure(t, out)
			assertComparisonTitle(t, file, c)
			assertPageFurniture(t, file, fmt.Sprintf("comparison of %d targets", len(c.Targets)))
			assertComparisonSections(t, file, c)
		})
	}
}

// assertComparisonTitle checks the title block: it names every target's display
// name, in the comparison's order.
func assertComparisonTitle(tb testing.TB, file *testPDF, c assessment.Comparison) {
	tb.Helper()

	lines := file.lines()
	if len(lines) == 0 {
		tb.Fatalf("the PDF has no lines")
	}

	first := squash(lines[0].Text)
	if !strings.Contains(first, "Migration assessment") {
		tb.Fatalf("the PDF's first line is %q, want the title block", first)
	}

	for _, target := range c.Targets {
		shown := squash(pdfreport.EscapeValue(target.Assessment.Target))
		if !strings.Contains(first, shown) {
			tb.Errorf("the title block does not name the target %q; it is %q", shown, first)
		}
	}
}

// assertComparisonSections checks the order of a comparison's sections: the
// comparison first, then one section per target in the comparison's order, then
// How this report is worked out and About this report once, covering every
// target.
func assertComparisonSections(tb testing.TB, file *testPDF, c assessment.Comparison) {
	tb.Helper()

	wants := []string{"Comparison", "Summary", "Resources", "Blockers on every target"}
	for _, target := range c.Targets {
		wants = append(wants, pdfreport.EscapeValue(target.Assessment.Target))
	}

	wants = append(wants, "How this report is worked out", "About this report")

	assertOrder(tb, file, wants...)
}

// TestReportComparisonText checks the text extracted from the comparison: the
// summary table, the resources table with each target's fit and best offering,
// the blockers on every target, each target's own sections with that target's
// candidates, and About this report naming every target's snapshot.
func TestReportComparisonText(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, sourceName, comparisonResources())
	c := assessment.Compare(assessTargets(t, inv, threeTargetKeys()))

	file := checkPDFStructure(t, renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName}))

	// One row per resource, in the inventory's order, with its ID and category.
	for _, verdict := range c.Targets[0].Assessment.Verdicts {
		assertShown(t, file, verdict.Resource.ID, string(verdict.Resource.Category))
	}

	for _, target := range c.Targets {
		for _, verdict := range target.Assessment.Verdicts {
			assertShown(t, file, string(verdict.Fit))

			if verdict.BestOffering != "" {
				assertShown(t, file, verdict.BestOffering)
			}

			for _, candidate := range verdict.Candidates {
				assertShown(t, file, candidate.Offering.Name)

				for _, capability := range candidate.Missing {
					assertShown(t, file, string(capability))
				}
			}
		}

		for _, blocker := range target.Assessment.Blockers {
			assertShown(t, file, blocker.Reason.String())
		}
	}

	// The shared blockers: the resources blocked on every target, between the
	// resources table and the per-target sections.
	comparison := sectionText(t, file, "Comparison", c.Targets[0].Assessment.Target)
	for _, blocker := range c.SharedBlockers {
		if !strings.Contains(comparison, squash(blocker.ResourceID)) {
			t.Errorf("the comparison does not name the shared blocker %q; the section is:\n%s",
				blocker.ResourceID, comparison)
		}

		for _, target := range blocker.Targets {
			for _, reason := range target.Reasons {
				if !strings.Contains(comparison, squash(reason.String())) {
					t.Errorf("the comparison does not give %q's reason on %q; the section is:\n%s",
						blocker.ResourceID, target.Key, comparison)
				}
			}
		}
	}

	// About this report once, covering every target: the version line, each
	// target's source URL and retrieval date, and the options.
	about := sectionText(t, file, "About this report", "")
	for _, want := range []string{
		versionLine,
		inventoryFileName,
		sourceName,
		"Estimates: off",
	} {
		if !strings.Contains(about, squash(want)) {
			t.Errorf("About this report does not name %q; the section is:\n%s", want, about)
		}
	}

	for _, target := range c.Targets {
		for _, want := range []string{
			target.Assessment.Target,
			target.Assessment.Snapshot.SourceURL,
			target.Assessment.Snapshot.RetrievedOn.UTC().Format("2006-01-02"),
		} {
			if !strings.Contains(about, squash(want)) {
				t.Errorf("About this report does not name %q of target %q; the section is:\n%s", want, target.Key, about)
			}
		}
	}
}

// TestReportComparisonNoEstimates checks that a comparison without estimates
// shows no plan, no amount and no Cost estimate section, as the single-target
// report does.
func TestReportComparisonNoEstimates(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(assessTargets(t, newInventory(t, sourceName, comparisonResources()), threeTargetKeys()))
	file := checkPDFStructure(t, renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName}))

	assertNotShown(t, file, "Cost estimate", "\u20ac", "not estimated")

	for _, target := range c.Targets {
		for _, verdict := range target.Assessment.Verdicts {
			if verdict.Estimate != nil {
				t.Fatalf("the comparison fixture holds an estimate although it was assessed without them")
			}
		}
	}
}

// TestReportComparisonErrors checks that a comparison with no target, and one
// whose targets assess different resources, is an error and writes nothing, as
// the other comparison reporters' contract says they do.
func TestReportComparisonErrors(t *testing.T) {
	t.Parallel()

	first := assessment.Assess(newInventory(t, sourceName, comparisonResources()), testutil.OVHcloudCatalog(t))
	second := assessment.Assess(newInventory(t, sourceName, twoTargetResources()), testutil.ScalewayCatalog(t))

	tests := []struct {
		name       string
		comparison assessment.Comparison
	}{
		{name: "no target", comparison: assessment.Compare(nil)},
		{
			name: "targets that assess different resources",
			comparison: assessment.Compare([]assessment.TargetAssessment{
				{Key: ovhcloudKey, Assessment: first},
				{Key: scalewayKey, Assessment: second},
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := pdfreport.New(versionLine).ReportComparison(t.Context(), &out, test.comparison,
				analysis.ReportMeta{InventoryName: inventoryFileName})
			if err == nil {
				t.Fatalf("ReportComparison() returned no error for a comparison with %s", test.name)
			}

			if out.Len() != 0 {
				t.Errorf("ReportComparison() wrote %d bytes before the error %v", out.Len(), err)
			}
		})
	}
}
