package htmlreport_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file pins item 04 of the maintainer's third round of feedback on the
// redesigned page (issue #373), as the maintainer settled it on 2026-11-22:
// the HTML report names the source's cost nowhere -- not in the workload boxes,
// which said "source cost" and the amount, and not in the cost estimate, whose
// closing line said "Source cost: €2,415.00 for 2 of 5, $40.00 for 1 of 5."
// The cost estimate itself stays: the target's monthly total, the basis it is
// of and the list-price sentence. The Markdown and JSON reports keep the source
// cost, so what changes is the HTML report only.

// sourceCostAmounts are the amounts a source cost reads as in the fixture
// inventory below (EUR 2,400.00 + EUR 15.00, and USD 40.00), which the fragment
// must not show once the source cost is gone. They are no target's estimate:
// the fixture's assessments total €2,535.00.
var sourceCostAmounts = []string{"€2,415.00", "$40.00"}

// snippet returns the text around the byte offset at, so a failure names the
// report's own words rather than the whole fragment. It cuts on rune boundaries,
// so an amount's sign is never split.
func snippet(text string, at int) string {
	const context = 60

	runes := []rune(text)
	// at is a byte offset; the runes before it are the runes of its prefix.
	pos := len([]rune(text[:at]))

	return string(runes[max(0, pos-context):min(len(runes), pos+context)])
}

// TestReportNeverShowsTheSourceCost pins that the fragment holds no source cost
// at all (issue #373, item 04, decision of 2026-11-22): not the words "source
// cost" or "no source cost", and not the source's amounts, in a single report
// or in a comparison. The cost estimate remains, with the target's monthly
// total, the basis it is of and the list-price sentence.
func TestReportNeverShowsTheSourceCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		assessments []assessment.Assessment
	}{
		{name: "one target", assessments: []assessment.Assessment{estimatesAssessment(t)}},
		{
			name: "a comparison of two targets",
			assessments: []assessment.Assessment{
				assessment.AssessWith(newInventory(t, sourceName, estimateResources()),
					testutil.ReportCatalogIn(t, "OVHcloud", testutil.PricedSnapshot()), assessment.Options{Estimate: true}),
				assessment.AssessWith(newInventory(t, sourceName, estimateResources()),
					testutil.ReportCatalogIn(t, "Scaleway", testutil.PricedSnapshot()), assessment.Options{Estimate: true}),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The checks below assert an absence, so each of them would pass
			// vacuously if the assessment it renders carried no source cost to
			// leave out: the fixture inventory's costs are what makes the
			// absence observable. Fail before rendering when they are gone.
			for _, a := range tc.assessments {
				if len(a.SourceCost.Currencies) == 0 {
					t.Fatalf("the %q assessment carries no source cost (its SourceCost.Currencies is empty), want the fixture inventory's sums, one per currency: with no source cost to leave out the fragment's absence of one proves nothing, so this test would pass even if the report showed the source cost again (issue #373, item 04)",
						a.Target)
				}
			}

			meta := analysis.ReportMeta{InventoryName: inventoryFileName}

			var fragment string

			if len(tc.assessments) == 1 {
				fragment = string(render(t, tc.assessments[0], meta))
			} else {
				keys := []string{"ovhcloud", "scaleway"}

				targets := make([]assessment.TargetAssessment, 0, len(tc.assessments))
				for i, a := range tc.assessments {
					targets = append(targets, assessment.TargetAssessment{
						Key:        keys[i],
						Assessment: a,
					})
				}

				fragment = string(renderComparison(t, assessment.Compare(targets), meta))
			}

			text := textOf(fragment)
			elements := fragmentElements(t, fragment)

			if at := strings.Index(strings.ToLower(text), "source cost"); at >= 0 {
				t.Errorf("the fragment says %q at character %d of its text, want no mention of the source cost anywhere: the report shows the target's estimate alone (issue #373, item 04, decision of 2026-11-22): …%s…",
					text[at:min(at+len("source cost"), len(text))], at, snippet(text, at))
			}

			for _, amount := range sourceCostAmounts {
				if at := strings.Index(text, amount); at >= 0 {
					t.Errorf("the fragment shows the source cost's amount %q at character %d of its text, want no source amount at all: the report shows the target's estimate alone (issue #373, item 04, decision of 2026-11-22): …%s…",
						amount, at, snippet(text, at))
				}
			}

			// The cost estimate itself stays: one section per assessment, with
			// the target's monthly total, the basis it is of and the list-price
			// sentence.
			costs := elementsWithClass(t, elements, "part-cost")
			if len(costs) != len(tc.assessments) {
				t.Fatalf("the fragment holds %d cost-estimate sections, want %d, one per assessment: removing the source cost must not remove the estimate (issue #373, item 04)",
					len(costs), len(tc.assessments))
			}

			for i, cost := range costs {
				estimate := textOfElement(elements, cost)
				a := tc.assessments[i]

				note := "List prices in " + a.EstimateRegion + " on " + a.Snapshot.RetrievedOn.UTC().Format("2006-01-02") +
					", excluding VAT and discounts."

				for _, want := range []string{"€2,535.00", "per month, 4 of 5 resources", note} {
					if !strings.Contains(estimate, want) {
						t.Errorf("the cost estimate reads %q, want it to keep %q: only the source cost goes (issue #373, item 04, decision of 2026-11-22)",
							estimate, want)
					}
				}
			}
		})
	}
}
