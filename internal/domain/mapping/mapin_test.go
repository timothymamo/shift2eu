package mapping_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// offeringIn is a compute.vm offering sold in exactly the given regions.
func offeringIn(name string, regions []string, capabilities ...taxonomy.Capability) catalog.Offering {
	return catalog.Offering{
		Name:         name,
		Categories:   []taxonomy.Category{taxonomy.ComputeVM},
		Capabilities: capabilities,
		Regions:      regions,
	}
}

// TestMapIn pins what a chosen target region changes in a mapping: an offering
// not available there is not a candidate, so a resource whose only offerings are
// elsewhere is "not offered in <region>" with fit none, while a resource with an
// offering in the region keeps the fit that offering's capabilities give it. The
// empty region is Map.
func TestMapIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		resource   inventory.Resource
		offerings  []catalog.Offering
		region     string
		wantSame   bool
		wantFit    mapping.Fit
		wantCands  []string
		wantBest   string
		wantMiss   []taxonomy.Capability
		wantReason []mapping.Reason
	}{
		{
			// A region of "" judges availability across all of the target's
			// regions: exactly the mapping Map gives today.
			name:      "the empty region is Map",
			resource:  computeResource("i-1", taxonomy.IPv6),
			offerings: []catalog.Offering{offeringIn("Alpha Compute", []string{"fr-par"}), offeringIn("Zulu Compute", []string{"nl-ams"}, taxonomy.IPv6)},
			region:    "",
			wantSame:  true,
		},
		{
			name:      "an offering in the region is a candidate",
			resource:  computeResource("i-1"),
			offerings: []catalog.Offering{offeringIn("Alpha Compute", []string{"fr-par"}), offeringIn("Zulu Compute", []string{"nl-ams"})},
			region:    "fr-par",
			wantFit:   mapping.Direct,
			wantCands: []string{"Alpha Compute"},
			wantBest:  "Alpha Compute",
			wantReason: []mapping.Reason{
				{Code: mapping.AllCapabilities, Offering: "Alpha Compute"},
			},
		},
		{
			// Map would make Zulu Compute, the direct one, the best candidate.
			// Not in the region, it is no candidate at all, and the fit comes
			// from the offering that is left.
			name:      "an offering elsewhere is not a candidate",
			resource:  computeResource("i-1", taxonomy.IPv6),
			offerings: []catalog.Offering{offeringIn("Alpha Compute", []string{"fr-par"}), offeringIn("Zulu Compute", []string{"nl-ams"}, taxonomy.IPv6)},
			region:    "fr-par",
			wantFit:   mapping.Partial,
			wantCands: []string{"Alpha Compute"},
			wantBest:  "Alpha Compute",
			wantMiss:  []taxonomy.Capability{taxonomy.IPv6},
			wantReason: []mapping.Reason{
				{Code: mapping.MissingCapability, Offering: "Alpha Compute", Capability: taxonomy.IPv6},
			},
		},
		{
			name:      "a resource whose only offerings are elsewhere is not offered",
			resource:  computeResource("i-1", taxonomy.IPv6),
			offerings: []catalog.Offering{offeringIn("Zulu Compute", []string{"nl-ams"}, taxonomy.IPv6)},
			region:    "fr-par",
			wantFit:   mapping.None,
			wantReason: []mapping.Reason{{
				Code:     mapping.NotOfferedInRegion,
				Target:   "Scaleway",
				Category: taxonomy.ComputeVM,
				Region:   "fr-par",
			}},
		},
		{
			// MapIn does not check the region against the target's regions: a
			// region no offering lists is the application layer's to refuse.
			name:      "a region no offering lists leaves every served category not offered",
			resource:  computeResource("i-1"),
			offerings: []catalog.Offering{offeringIn("Alpha Compute", []string{"fr-par"})},
			region:    "zz-zzz",
			wantFit:   mapping.None,
			wantReason: []mapping.Reason{{
				Code:     mapping.NotOfferedInRegion,
				Target:   "Scaleway",
				Category: taxonomy.ComputeVM,
				Region:   "zz-zzz",
			}},
		},
		{
			// A region filter does not invent an offering for a category the
			// catalog does not serve anywhere.
			name:     "a category no offering serves stays NoOffering",
			resource: inventory.Resource{ID: "queue-1", RawType: "AWS::SQS::Queue", Category: taxonomy.MessagingQueue, Region: "eu-west-1"},
			offerings: []catalog.Offering{
				offeringIn("Alpha Compute", []string{"fr-par"}),
			},
			region:  "fr-par",
			wantFit: mapping.None,
			wantReason: []mapping.Reason{{
				Code:     mapping.NoOffering,
				Target:   "Scaleway",
				Category: taxonomy.MessagingQueue,
			}},
		},
		{
			name:     "an unclassified resource stays not classified",
			resource: inventory.Resource{ID: "misc-1", RawType: "AWS::CloudFormation::CustomResource", Category: taxonomy.Unclassified, Region: "eu-west-1"},
			offerings: []catalog.Offering{
				offeringIn("Alpha Compute", []string{"fr-par"}),
			},
			region:     "fr-par",
			wantFit:    mapping.None,
			wantReason: []mapping.Reason{{Code: mapping.NotClassified}},
		},
		{
			name:     "candidates are ordered among the region's offerings",
			resource: computeResource("i-1", taxonomy.IPv6),
			offerings: []catalog.Offering{
				offeringIn("Alpha Compute", []string{"fr-par"}),
				offeringIn("Zulu Compute", []string{"fr-par"}, taxonomy.IPv6),
				// Direct, but not in the region: it would be the best candidate.
				offeringIn("Bravo Compute", []string{"nl-ams"}, taxonomy.IPv6),
			},
			region:    "fr-par",
			wantFit:   mapping.Direct,
			wantCands: []string{"Zulu Compute", "Alpha Compute"},
			wantBest:  "Zulu Compute",
			wantReason: []mapping.Reason{
				{Code: mapping.AllCapabilities, Offering: "Zulu Compute"},
			},
		},
		{
			name:     "any of the offering's own regions serves it",
			resource: computeResource("i-1", taxonomy.IPv6),
			offerings: []catalog.Offering{
				offeringIn("Alpha Compute", []string{"fr-par", "nl-ams"}, taxonomy.IPv6),
			},
			region:    "nl-ams",
			wantFit:   mapping.Direct,
			wantCands: []string{"Alpha Compute"},
			wantBest:  "Alpha Compute",
			wantReason: []mapping.Reason{
				{Code: mapping.AllCapabilities, Offering: "Alpha Compute"},
			},
		},
		{
			// A global offering is a candidate in every region: a zone is served by
			// the provider's own name servers, so a chosen region never makes it
			// "not offered in <region>" (PRD #581, The region model).
			name:      "a global offering is a candidate in a declared region",
			resource:  dnsResource("zone-1"),
			offerings: []catalog.Offering{globalDNSOffering()},
			region:    "fr-par",
			wantFit:   mapping.Direct,
			wantCands: []string{"DNS Zone"},
			wantBest:  "DNS Zone",
			wantReason: []mapping.Reason{
				{Code: mapping.AllCapabilities, Offering: "DNS Zone"},
			},
		},
		{
			// A region no offering lists is MapIn's to judge, never the map's to
			// invent: a global offering is still offered there, so the fit is the
			// one its capabilities give, not NotOfferedInRegion.
			name:      "a global offering is a candidate in a region no offering lists",
			resource:  dnsResource("zone-1", taxonomy.DNSSEC),
			offerings: []catalog.Offering{globalDNSOffering()},
			region:    "zz-zzz",
			wantFit:   mapping.Partial,
			wantCands: []string{"DNS Zone"},
			wantBest:  "DNS Zone",
			wantMiss:  []taxonomy.Capability{taxonomy.DNSSEC},
			wantReason: []mapping.Reason{
				{Code: mapping.MissingCapability, Offering: "DNS Zone", Capability: taxonomy.DNSSEC},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resource := newResource(t, tc.resource)
			cat := newCatalog(t, tc.offerings)

			got := mapping.MapIn(resource, cat, tc.region)

			if diff := cmp.Diff(resource, got.Resource); diff != "" {
				t.Errorf("MapIn() resource mismatch (-want +got):\n%s", diff)
			}

			if tc.wantSame {
				if diff := cmp.Diff(mapping.Map(resource, cat), got); diff != "" {
					t.Fatalf("MapIn() with no region differs from Map() (-Map +MapIn):\n%s", diff)
				}

				return
			}

			if got.Fit != tc.wantFit {
				t.Errorf("MapIn() fit = %q, want %q", got.Fit, tc.wantFit)
			}

			if diff := cmp.Diff(tc.wantCands, candidateNames(got.Candidates)); diff != "" {
				t.Errorf("MapIn() candidates mismatch (-want +got):\n%s", diff)
			}

			best, ok := got.Best()
			if tc.wantBest == "" {
				if ok {
					t.Errorf("MapIn() best candidate = %s, want none", text.Quote(best.Offering.Name))
				}
			} else {
				if !ok {
					t.Fatalf("MapIn() has no best candidate, want %s", text.Quote(tc.wantBest))
				}

				if best.Offering.Name != tc.wantBest {
					t.Errorf("MapIn() best candidate = %s, want %s",
						text.Quote(best.Offering.Name), text.Quote(tc.wantBest))
				}

				if diff := cmp.Diff(tc.wantMiss, best.Missing); diff != "" {
					t.Errorf("MapIn() missing capabilities mismatch (-want +got):\n%s", diff)
				}
			}

			if diff := cmp.Diff(tc.wantReason, got.Reasons); diff != "" {
				t.Errorf("MapIn() reasons mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// candidateNames returns the names of the candidates, in candidate order.
func candidateNames(candidates []mapping.Candidate) []string {
	if len(candidates) == 0 {
		return nil
	}

	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.Offering.Name)
	}

	return names
}

// dnsResource is a network.dns resource using the capabilities given: a public
// zone, written with no region, as a source writes a global resource.
func dnsResource(id string, capabilities ...taxonomy.Capability) inventory.Resource {
	return inventory.Resource{
		ID:           id,
		RawType:      "AWS::Route53::HostedZone",
		Category:     taxonomy.NetworkDNS,
		Capabilities: capabilities,
	}
}

// globalDNSOffering is the one global category's offering: a DNS zone, in no
// declared region, available in every one.
func globalDNSOffering() catalog.Offering {
	return catalog.Offering{
		Name:       "DNS Zone",
		Categories: []taxonomy.Category{taxonomy.NetworkDNS},
		Global:     true,
	}
}
