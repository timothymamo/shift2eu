package markdown_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file pins the global-services mark of the Markdown reports (PRD #581, The
// region model, Reports): "(global)" after the name of a global offering, in the
// best-offering cell of a resource with its ties and in the "(not offered by X)"
// of the required changes, and the one note that says what the mark means, under
// the first table that shows a global offering.
//
// The note is the report's own words, so a golden pins its exact wording: this
// constant is the same sentence, for the assertions that count it. A report that
// shows no global offering says neither the mark nor the note.
const globalNote = "`(global)` means not bound to a region you choose: the provider serves the " +
	"offering from its own locations, which the target's `SOURCES.md` names."

// globalMark is what a report writes after the name of a global offering.
const globalMark = "(global)"

// dnsResources is the inventory the global-services goldens assess: three public
// DNS zones with no region (PRD #581). Mapped onto testutil.DNSCatalog they give
// one verdict whose best offering is global and tied with another global one, one
// whose best offering is regional with two global ties, and one partial verdict
// whose required change names a global offering.
func dnsResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:           "zone-dnssec",
			RawType:      "AWS::Route53::HostedZone",
			Category:     taxonomy.NetworkDNS,
			Capabilities: []taxonomy.Capability{taxonomy.DNSSEC},
		},
		{
			ID:           "zone-example",
			RawType:      "AWS::Route53::HostedZone",
			Category:     taxonomy.NetworkDNS,
			Capabilities: []taxonomy.Capability{taxonomy.DNSRouting},
		},
		{
			ID:           "zone-ipv6",
			RawType:      "AWS::Route53::HostedZone",
			Category:     taxonomy.NetworkDNS,
			Capabilities: []taxonomy.Capability{taxonomy.DNSRouting, taxonomy.IPv6},
		},
	}
}

// dnsInventory is the inventory the global-services goldens assess.
func dnsInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	return newInventory(t, sourceName, dnsResources())
}

// globalBestCell is the best-offering cell of a verdict whose best offering is
// the global "DNS Zone", tied with the global "DNS Zone Premium".
const globalBestCell = "`DNS Zone` (global) (tie: `DNS Zone Premium` (global))"

// TestReportGlobalOfferings pins the mark and the note of the single-target
// report (PRD #581): the whole report against testutil.DNSCatalog, in which the
// best-offering cell marks the best offering and each global tie, the required
// changes mark a global offering after "(not offered by", and one note under the
// Resources table says what "(global)" means.
func TestReportGlobalOfferings(t *testing.T) {
	t.Parallel()

	got := render(t, assessment.Assess(dnsInventory(t), testutil.DNSCatalog(t)),
		analysis.ReportMeta{InventoryName: inventoryFileName})

	testutil.Golden(t, "testdata/report-global.golden", got)

	checkTableShape(t, got)
	checkHeadings(t, got, "# Migration assessment: `OVHcloud`", analysisSections()...)

	t.Run("the best offering and each global tie are marked", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			id   string
			want string
		}{
			{
				name: "a global best offering with a global tie",
				id:   "`zone-example`",
				want: globalBestCell,
			},
			{
				name: "a partial global best offering with a global tie",
				id:   "`zone-ipv6`",
				want: globalBestCell,
			},
			{
				name: "a regional best offering marks only its global ties",
				id:   "`zone-dnssec`",
				want: "`Apex DNS Zone` (tie: `DNS Zone` (global), `DNS Zone Premium` (global))",
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				cells := tableRow(t, got, test.id)
				if len(cells) != 6 {
					t.Fatalf("the resource row has %d cells, want 6: %q", len(cells), cells)
				}

				if cells[4] != test.want {
					t.Errorf("Best offering cell = %q, want %q", cells[4], test.want)
				}
			})
		}
	})

	t.Run("the required changes mark the global offering", func(t *testing.T) {
		t.Parallel()

		want := []string{"- `zone-ipv6`: `ipv6` (not offered by `DNS Zone` (global))"}

		if lines := requiredChangeLines(t, got); !slices.Equal(lines, want) {
			t.Errorf("required changes = %q, want %q", lines, want)
		}
	})

	t.Run("the note is said once, under the Resources table", func(t *testing.T) {
		t.Parallel()

		if count := strings.Count(string(got), globalNote); count != 1 {
			t.Errorf("the report says what (global) means %d times, want once:\n%s", count, got)
		}

		// The note follows the Resources table, whose last row is the last
		// resource's, and precedes the Required changes heading.
		lines := strings.Split(string(got), "\n")
		note := indexOfLine(lines, globalNote)

		if note < 0 {
			t.Fatalf("the report has no note line %q:\n%s", globalNote, got)
		}

		if last := lastTableRowLine(lines); last > note {
			t.Errorf("the note is on line %d, before the last table row on line %d, want it under the "+
				"table that shows the global offering:\n%s", note+1, last+1, got)
		}
	})
}

// TestReportNoGlobalOfferingSaysNeither pins that a report whose best offerings
// and ties are all regional says neither the mark nor the note: a global offering
// the catalog holds but the report never shows is not marked, and the note is
// only written when a table shows one.
func TestReportNoGlobalOfferingSaysNeither(t *testing.T) {
	t.Parallel()

	// "DNS Zone" is global but fits worse than the regional "Apex DNS Zone" for
	// the resource below, so neither the best offering nor a tie is global.
	cat := newCatalog(t, []catalog.Offering{
		{
			Name:         "Apex DNS Zone",
			Categories:   []taxonomy.Category{taxonomy.NetworkDNS},
			Capabilities: []taxonomy.Capability{taxonomy.DNSSEC},
			Regions:      []string{"fr-par"},
		},
		{
			Name:         "DNS Zone",
			Categories:   []taxonomy.Category{taxonomy.NetworkDNS},
			Capabilities: []taxonomy.Capability{taxonomy.DNSRouting},
			Global:       true,
		},
	})

	inv := newInventory(t, sourceName, []inventory.Resource{
		{
			ID:           "zone-example",
			RawType:      "AWS::Route53::HostedZone",
			Category:     taxonomy.NetworkDNS,
			Capabilities: []taxonomy.Capability{taxonomy.DNSSEC},
		},
	})

	got := string(render(t, assessment.Assess(inv, cat), analysis.ReportMeta{InventoryName: inventoryFileName}))

	if strings.Contains(got, globalMark) {
		t.Errorf("the report marks an offering global although it shows none:\n%s", got)
	}

	if strings.Contains(got, globalNote) {
		t.Errorf("the report says what (global) means although it shows no global offering:\n%s", got)
	}

	if cells := tableRow(t, []byte(got), "`zone-example`"); cells[4] != "`Apex DNS Zone`" {
		t.Errorf("Best offering cell = %q, want the regional offering %q", cells[4], "`Apex DNS Zone`")
	}
}

// TestReportComparisonGlobalOfferings pins the mark and the note of the Markdown
// comparison report (PRD #581): a target whose best offering is global is marked
// in the comparison table, which names no ties, and its own section marks the
// global ties too; the note is said once, under the first table that shows a
// global offering. The other target has no DNS zone offering at all, so its
// blockers' "no offering in the <target> catalog for <category>" reason names no
// offering and gains no mark.
func TestReportComparisonGlobalOfferings(t *testing.T) {
	t.Parallel()

	inv := dnsInventory(t)

	c := assessment.Compare([]assessment.TargetAssessment{
		{Key: ovhcloudKey, Assessment: assessment.Assess(inv, testutil.DNSCatalog(t))},
		{Key: upcloudKey, Assessment: assessment.Assess(inv, testutil.UpCloudCatalog(t))},
	})

	got := renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName})

	testutil.Golden(t, "testdata/comparison-global.golden", got)

	checkTableShape(t, got)

	t.Run("the comparison table marks a global best offering", func(t *testing.T) {
		t.Parallel()

		cells := tableRow(t, got, "`zone-example`")
		if len(cells) != 6 {
			t.Fatalf("the comparison row has %d cells, want 6: %q", len(cells), cells)
		}

		// The comparison table states each target's best offering alone: no tie
		// note, and the mark of a global best offering.
		if want := "`DNS Zone` (global)"; cells[3] != want {
			t.Errorf("the OVHcloud best offering cell = %q, want %q", cells[3], want)
		}

		if strings.Contains(cells[3], comparisonTieNote) {
			t.Errorf("the comparison best offering cell holds the tie note %q: %q", comparisonTieNote, cells[3])
		}
	})

	t.Run("a target's section marks the global ties", func(t *testing.T) {
		t.Parallel()

		rows := linesBetween(t, sectionLines(t, got, targetSectionHeading("OVHcloud")),
			"### Resources", "### Required changes")

		if want := "| `zone-dnssec` | `AWS::Route53::HostedZone` | `network.dns` | direct | " +
			"`Apex DNS Zone` (tie: `DNS Zone` (global), `DNS Zone Premium` (global)) | " +
			"`Apex DNS Zone` offers every capability the resource uses |"; !containsLine(rows, want) {
			t.Errorf("the OVHcloud section holds no row %q: %q", want, rows)
		}
	})

	t.Run("the note is said once", func(t *testing.T) {
		t.Parallel()

		if count := strings.Count(string(got), globalNote); count != 1 {
			t.Errorf("the report says what (global) means %d times, want once:\n%s", count, got)
		}
	})

	t.Run("a blocker names no offering and gains no mark", func(t *testing.T) {
		t.Parallel()

		lines := linesWithPrefix(t, got, "- `zone-example`: ")
		if len(lines) != 1 {
			t.Fatalf("the report has %d blocker lines for zone-example, want 1: %q", len(lines), lines)
		}

		want := "- `zone-example`: no offering in the `UpCloud` catalog for `network.dns`"
		if lines[0] != want {
			t.Errorf("blocker line = %q, want %q", lines[0], want)
		}
	})
}

// containsLine reports whether lines holds want.
func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}

	return false
}

// indexOfLine returns the index of the line equal to want, or -1.
func indexOfLine(lines []string, want string) int {
	for i, line := range lines {
		if line == want {
			return i
		}
	}

	return -1
}

// lastTableRowLine returns the index of the last table row of the report.
func lastTableRowLine(lines []string) int {
	last := -1

	for i, line := range lines {
		if strings.HasPrefix(line, "|") {
			last = i
		}
	}

	return last
}
