package htmlreport_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file pins the global-services mark of the site's HTML report (PRD #581,
// The region model, Reports): "(global)" after the name of a global offering, in
// the best-offering cell of a resource with its ties and in the "(not offered by
// X)" of the required changes, and the one note that says what the mark means,
// under the first table that shows a global offering. The fragment marks the same
// offerings as the other two report formats do.
const (
	// globalMark is what the report writes after the name of a global offering.
	globalMark = "(global)"
	// globalNote is the note the report says once: the report's own words with
	// the mark and SOURCES.md as code, and the apostrophe escaped as the reporter
	// escapes every text character.
	globalNote = "<p><code>(global)</code> means not bound to a region you choose, including the one " +
		"<code>--region</code> names: the provider serves the offering from its own locations, which may " +
		"be outside the EU and which the target&#39;s <code>SOURCES.md</code> names.</p>"
	// globalBestCell is the best-offering cell of a verdict whose best offering is
	// the global "DNS Zone", tied with the global "DNS Zone Premium".
	globalBestCell = "<td><code>DNS Zone</code> (global) (tie: <code>DNS Zone Premium</code> (global))</td>"
)

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

// TestReportGlobalOfferings pins the mark and the note of the single-target
// fragment (PRD #581): the whole fragment against testutil.DNSCatalog, in which
// the best-offering cell marks the best offering and each global tie, the
// required changes mark a global offering after "(not offered by", and one note
// under the Resources table says what "(global)" means.
func TestReportGlobalOfferings(t *testing.T) {
	t.Parallel()

	a := assessment.Assess(newInventory(t, sourceName, dnsResources()), testutil.DNSCatalog(t))
	got := string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	checkFragment(t, []byte(got))
	checkValuesAreEscaped(t, []byte(got), resourceIDs(dnsResources()))

	testutil.Golden(t, "testdata/report-global.golden", []byte(got))

	t.Run("the best offering and each global tie are marked", func(t *testing.T) {
		t.Parallel()

		if count := strings.Count(got, globalBestCell); count != 2 {
			t.Errorf("the fragment holds the global best-offering cell %q %d times, want twice:\n%s",
				globalBestCell, count, got)
		}

		regional := "<td><code>Apex DNS Zone</code> (tie: <code>DNS Zone</code> (global), " +
			"<code>DNS Zone Premium</code> (global))</td>"
		if !strings.Contains(got, regional) {
			t.Errorf("the fragment holds no regional best-offering cell %q:\n%s", regional, got)
		}
	})

	t.Run("the required changes mark the global offering", func(t *testing.T) {
		t.Parallel()

		want := "<li><code>zone-ipv6</code>: <code>ipv6</code> (not offered by <code>DNS Zone</code> " +
			"(global))</li>"
		if !strings.Contains(got, want) {
			t.Errorf("the fragment holds no required-change item %q:\n%s", want, got)
		}
	})

	t.Run("the note is said once", func(t *testing.T) {
		t.Parallel()

		if count := strings.Count(got, globalNote); count != 1 {
			t.Errorf("the fragment says what (global) means %d times, want once:\n%s", count, got)
		}
	})
}

// TestReportNoGlobalOfferingSaysNeither pins that a fragment whose best offerings
// and ties are all regional says neither the mark nor the note, even when a global
// offering is a candidate: a global offering the report never shows is not
// marked, and the note is only written when a table shows one.
func TestReportNoGlobalOfferingSaysNeither(t *testing.T) {
	t.Parallel()

	// "DNS Zone" is global but fits worse than the regional "Apex DNS Zone" for
	// the resource below, so it is a candidate that is neither the best offering
	// nor a tie: the fragment shows the regional offering alone.
	cat := catalogFor(t, "Scaleway", testutil.ScalewaySnapshot(), []catalog.Offering{
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

	a := assessment.Assess(newInventory(t, sourceName, []inventory.Resource{
		{
			ID:           "zone-example",
			RawType:      "AWS::Route53::HostedZone",
			Category:     taxonomy.NetworkDNS,
			Capabilities: []taxonomy.Capability{taxonomy.DNSSEC},
		},
	}), cat)
	got := string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	if strings.Contains(got, globalMark) {
		t.Errorf("the fragment marks an offering global although it shows none:\n%s", got)
	}

	if strings.Contains(got, globalNote) {
		t.Errorf("the fragment says what (global) means although it shows no global offering:\n%s", got)
	}

	if want := "<td><code>Apex DNS Zone</code></td>"; !strings.Contains(got, want) {
		t.Errorf("the fragment holds no regional best-offering cell %q:\n%s", want, got)
	}
}

// TestReportComparisonGlobalOfferings pins the mark and the note of a comparison
// fragment (PRD #581): each target's own section marks its best offering and its
// global ties, the note is said once under the first table that shows a global
// offering, and the other target, which serves no DNS zone, names no offering in
// its blockers and gains no mark.
func TestReportComparisonGlobalOfferings(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, sourceName, dnsResources())

	got := string(renderComparison(t, assessment.Compare([]assessment.TargetAssessment{
		{Key: "ovhcloud", Assessment: assessment.Assess(inv, testutil.DNSCatalog(t))},
		{Key: "upcloud", Assessment: assessment.Assess(inv, testutil.UpCloudCatalog(t))},
	}), analysis.ReportMeta{InventoryName: inventoryFileName}))

	checkFragment(t, []byte(got))
	checkValuesAreEscaped(t, []byte(got), resourceIDs(dnsResources()))

	testutil.Golden(t, "testdata/comparison-global.golden", []byte(got))

	t.Run("a target's section marks the global best offering and its ties", func(t *testing.T) {
		t.Parallel()

		if count := strings.Count(got, globalBestCell); count != 2 {
			t.Errorf("the fragment holds the global best-offering cell %q %d times, want twice:\n%s",
				globalBestCell, count, got)
		}
	})

	t.Run("the note is said once", func(t *testing.T) {
		t.Parallel()

		if count := strings.Count(got, globalNote); count != 1 {
			t.Errorf("the fragment says what (global) means %d times, want once:\n%s", count, got)
		}
	})

	t.Run("a blocker names no offering and gains no mark", func(t *testing.T) {
		t.Parallel()

		want := "<li><code>zone-example</code>: no offering in the <code>UpCloud</code> catalog for " +
			"<code>network.dns</code></li>"
		if !strings.Contains(got, want) {
			t.Errorf("the fragment holds no blocker item %q:\n%s", want, got)
		}
	})
}
