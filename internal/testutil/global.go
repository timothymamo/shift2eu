package testutil

import (
	"testing"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

// DNSOfferings is the fixture catalog's global-services offering set (PRD #581):
// OVHcloud's public DNS zones. Two offerings are global, served from the
// provider's own locations and bound to no region the user chooses, and one is
// regional, so a report of the fixture has to keep a global best offering, a
// global tie and a regional best offering with global ties apart.
//
// The names decide which offering is best for a resource, because candidates
// that fit alike are ordered by name: "Apex DNS Zone" comes first, so a resource
// that uses only DNSSEC, which all three offerings provide, is mapped onto the
// regional zone with the two global ones as its ties; a resource that uses
// DNSRouting, which the regional zone lacks, is mapped onto the global
// "DNS Zone", tied with the global "DNS Zone Premium".
//
// It returns a fresh slice on every call, so a test may mutate its result.
func DNSOfferings() []catalog.Offering {
	return []catalog.Offering{
		{
			Name:         "Apex DNS Zone",
			Categories:   []taxonomy.Category{taxonomy.NetworkDNS},
			Capabilities: []taxonomy.Capability{taxonomy.DNSSEC},
			Regions:      []string{"gra", "sbg"},
		},
		{
			Name:         "DNS Zone",
			Categories:   []taxonomy.Category{taxonomy.NetworkDNS},
			Capabilities: []taxonomy.Capability{taxonomy.DNSSEC, taxonomy.DNSRouting},
			Global:       true,
		},
		{
			Name:         "DNS Zone Premium",
			Categories:   []taxonomy.Category{taxonomy.NetworkDNS},
			Capabilities: []taxonomy.Capability{taxonomy.DNSSEC, taxonomy.DNSRouting},
			Global:       true,
		},
	}
}

// DNSCatalog is the OVHcloud fixture catalog of the global-services goldens: an
// OVHcloud catalog whose only offerings are DNSOfferings, so a report of an
// inventory of public DNS zones names a global offering and nothing else.
func DNSCatalog(tb testing.TB) catalog.Catalog {
	tb.Helper()

	cat, err := catalog.New(ovhcloudTarget, OVHcloudSnapshot(), DNSOfferings())
	if err != nil {
		tb.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}
