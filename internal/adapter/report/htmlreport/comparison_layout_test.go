package htmlreport_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file pins item 05 of the maintainer's third round of feedback on the
// redesigned page (issue #373): a comparison of two or three targets shows each
// target's own sections and nothing else. The combined block the fragment used
// to write above them -- the section headed "Comparison", its resources table
// with a column per target and its "Blockers on every target" list -- is gone,
// whatever the number of targets.

// comparisonTarget is one target of the comparison a check renders: its key in
// the comparison and the fixture catalog it is assessed against.
type comparisonTarget struct {
	key     string
	catalog func(testing.TB) catalog.Catalog
}

// comparisonTargets are the three targets the site offers, by the key the
// comparison sorts them by.
var comparisonTargets = map[string]comparisonTarget{
	"ovhcloud": {key: "ovhcloud", catalog: testutil.OVHcloudCatalog},
	"scaleway": {key: "scaleway", catalog: testutil.ScalewayCatalog},
	"upcloud":  {key: "upcloud", catalog: testutil.UpCloudCatalog},
}

// TestReportComparisonShowsOnlyThePerTargetSections pins the comparison of
// several targets (issue #373, item 05): every target's own sections are there,
// one per target, and the fragment holds none of the combined block's markers --
// no part-comparison section, no "Comparison" heading, no resources table with a
// column per target and no "Blockers on every target" list. The maintainer's
// screenshot of a comparison showed the combined block above the per-target
// sections, for two targets as well as for three.
func TestReportComparisonShowsOnlyThePerTargetSections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		targets []string
	}{
		{name: "OVHcloud and Scaleway", targets: []string{"ovhcloud", "scaleway"}},
		{name: "OVHcloud and UpCloud", targets: []string{"ovhcloud", "upcloud"}},
		{name: "Scaleway and UpCloud", targets: []string{"scaleway", "upcloud"}},
		{name: "OVHcloud, Scaleway and UpCloud", targets: []string{"ovhcloud", "scaleway", "upcloud"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inv := newInventory(t, sourceName, comparisonResources())

			assessed := make([]assessment.TargetAssessment, 0, len(tc.targets))
			for _, key := range tc.targets {
				target := comparisonTargets[key]

				assessed = append(assessed, assessment.TargetAssessment{
					Key:        target.key,
					Assessment: assessment.Assess(inv, target.catalog(t)),
				})
			}

			c := assessment.Compare(assessed)
			fragment := string(renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName}))
			elements := fragmentElements(t, fragment)

			// Every target's own section is there, in the comparison's order, and
			// each one is headed by its own name.
			sections := elementsWithClass(t, elements, "part-target")
			if len(sections) != len(c.Targets) {
				t.Fatalf("the comparison holds %d per-target sections, want %d, one per target (issue #373, item 05)",
					len(sections), len(c.Targets))
			}

			want := make([]string, 0, len(c.Targets))
			for _, target := range c.Targets {
				want = append(want, target.Assessment.Target)
			}

			got := make([]string, 0, len(sections))
			for _, section := range sections {
				got = append(got, firstHeadingText(elements, section))
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("the per-target sections' headings mismatch (-want +got):\n%s", diff)
			}

			// None of the combined block's markers is left.
			if combined := elementsWithClass(t, elements, "part-comparison"); len(combined) != 0 {
				t.Errorf("the comparison holds %d sections carrying the class \"part-comparison\", want none: the combined block is gone and each target's section stands alone (issue #373, item 05)",
					len(combined))
			}

			if headings := headingTexts(elements); slices.Contains(headings, "Comparison") {
				t.Errorf("the comparison holds the heading \"Comparison\", want none: the combined block's own heading is gone (issue #373, item 05): %v",
					headings)
			}

			if text := textOf(fragment); strings.Contains(text, "Blockers on every target") {
				t.Errorf("the comparison holds a \"Blockers on every target\" list, want none: the combined block is gone (issue #373, item 05): …%s…",
					snippet(text, strings.Index(text, "Blockers on every target")))
			}

			for _, name := range want {
				for _, header := range tableHeaders(t, elements) {
					if slices.Contains(header, name+" fit") || slices.Contains(header, name+" best offering") {
						t.Errorf("the comparison holds the combined resources table's header %q, want no table with a column per target: each target's resources table is in its own section (issue #373, item 05)",
							header)
					}
				}
			}

			// Each target's section still holds that target's resources table.
			tables := 0

			for _, header := range tableHeaders(t, elements) {
				if len(header) > 0 && header[0] == "ID" {
					tables++
				}
			}

			if tables != len(c.Targets) {
				t.Errorf("the comparison holds %d resources tables, want %d, one in each target's own section (issue #373, item 05)",
					tables, len(c.Targets))
			}
		})
	}
}

// firstHeadingText returns the text of the first heading inside the element
// index, or "" when it holds none: a part section opens with its own heading.
func firstHeadingText(elements []fragElement, index int) string {
	for _, descendant := range descendants(elements, index) {
		switch elements[descendant].tag {
		case "h2", "h3", "h4", "h5":
			return textOfElement(elements, descendant)
		}
	}

	return ""
}

// headingTexts returns the text of every heading the fragment holds, in document
// order.
func headingTexts(elements []fragElement) []string {
	var out []string

	for i := range elements {
		switch elements[i].tag {
		case "h2", "h3", "h4", "h5":
			out = append(out, textOfElement(elements, i))
		}
	}

	return out
}

// tableHeaders returns the header cells of every table the fragment holds, in
// document order.
func tableHeaders(t *testing.T, elements []fragElement) [][]string {
	t.Helper()

	var out [][]string

	for i, element := range elements {
		if element.tag != "table" {
			continue
		}

		out = append(out, headerCells(t, elements, i))
	}

	return out
}
