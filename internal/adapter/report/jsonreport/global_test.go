package jsonreport_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file pins the global-services keys of the JSON report (PRD #581, The
// region model, Reports): schemaVersion 3 writes "bestOfferingGlobal": true on a
// resource whose best offering is global and "globalTies", the names of the ties
// that are global in the order of "ties", each key absent when it would be false
// or empty. SchemaVersion 2 writes neither, and the version 3 schema files
// declare both.
const (
	// bestOfferingGlobalKey is the key a document writes on a resource whose best
	// offering is global.
	bestOfferingGlobalKey = "bestOfferingGlobal"
	// globalTiesKey is the key a document writes with the names of a resource's
	// ties that are global.
	globalTiesKey = "globalTies"
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

// TestReportGlobalOfferings pins the global-services keys of the single-target
// document (PRD #581): in schemaVersion 3, "bestOfferingGlobal" on a resource
// whose best offering is global, "globalTies" with the global ties' names in the
// order of "ties", and neither key on a resource whose best offering is regional
// and whose ties are not global; in schemaVersion 2, neither key anywhere.
func TestReportGlobalOfferings(t *testing.T) {
	t.Parallel()

	a := assessment.Assess(newInventory(t, sourceName, dnsResources()), testutil.DNSCatalog(t))
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	v2 := renderVersion(t, a, meta, jsonreport.Version2)
	testutil.Golden(t, "testdata/report-global.golden", v2)
	checkDocument(t, v2)

	v3 := renderVersion(t, a, meta, jsonreport.Version3)
	testutil.Golden(t, "testdata/report-global-v3.golden", v3)
	checkDocument(t, v3)

	document := decodeJSON(t, v3)

	t.Run("version 3 marks a global best offering", func(t *testing.T) {
		t.Parallel()

		for _, id := range []string{"zone-example", "zone-ipv6"} {
			if got := valueAt(t, document, extend(resourceIndex(t, document, id), bestOfferingGlobalKey)...); got != true {
				t.Errorf("%s.%s = %v, want true", id, bestOfferingGlobalKey, got)
			}
		}
	})

	t.Run("version 3 writes the global ties in the order of the ties", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			id   string
			want []string
		}{
			{
				name: "a global best offering with a global tie",
				id:   "zone-example",
				want: []string{"DNS Zone Premium"},
			},
			{
				name: "a partial global best offering with a global tie",
				id:   "zone-ipv6",
				want: []string{"DNS Zone Premium"},
			},
			{
				name: "a regional best offering whose ties are global",
				id:   "zone-dnssec",
				want: []string{"DNS Zone", "DNS Zone Premium"},
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				if diff := cmp.Diff(test.want,
					stringsAt(t, document, extend(resourceIndex(t, document, test.id), globalTiesKey)...)); diff != "" {
					t.Errorf("%s.%s mismatch (-want +got):\n%s", test.id, globalTiesKey, diff)
				}
			})
		}
	})

	t.Run("a resource whose best offering is regional writes no global flag", func(t *testing.T) {
		t.Parallel()

		keys := objectKeysAt(t, document, resourceIndex(t, document, "zone-dnssec")...)
		if slices.Contains(keys, bestOfferingGlobalKey) {
			t.Errorf("a resource whose best offering is regional holds %q, want it left out: %q",
				bestOfferingGlobalKey, keys)
		}
	})

	t.Run("the keys sit beside bestOffering and ties", func(t *testing.T) {
		t.Parallel()

		assertKeyOrder(t, resourceObject(t, string(v3), "zone-example"),
			"bestOffering", bestOfferingGlobalKey, "ties", globalTiesKey)
	})

	t.Run("version 2 writes neither key", func(t *testing.T) {
		t.Parallel()

		keys := allKeys(t, decodeJSON(t, v2))
		for _, key := range []string{bestOfferingGlobalKey, globalTiesKey} {
			if slices.Contains(keys, key) {
				t.Errorf("the schemaVersion 2 document holds %q, want it left out: %q", key, keys)
			}
		}

		// The ties themselves are version 2's, unchanged: a document of the
		// global fixture writes the same offering names it always did.
		resource := resourceIndex(t, decodeJSON(t, v2), "zone-dnssec")
		want := []string{"DNS Zone", "DNS Zone Premium"}

		if diff := cmp.Diff(want, stringsAt(t, decodeJSON(t, v2), extend(resource, "ties")...)); diff != "" {
			t.Errorf("zone-dnssec.ties mismatch (-want +got):\n%s", diff)
		}
	})
}

// resourceObject returns the text of the JSON object of the resource whose id is
// id, from its opening brace to its matching one, so a test can pin the order of
// the keys the document writes for it.
func resourceObject(tb testing.TB, document, id string) string {
	tb.Helper()

	at := strings.Index(document, `"id": "`+id+`"`)
	if at < 0 {
		tb.Fatalf("the document holds no resource %q", id)
	}

	open := strings.LastIndex(document[:at], "{")
	if open < 0 {
		tb.Fatalf("the object of resource %q is not opened", id)
	}

	return objectFrom(tb, document, open)
}

// TestReportComparisonGlobalOfferings pins the global-services keys of the
// comparison document (PRD #581): each target's resources carry the keys of the
// single-target document, so a target whose best offering is global marks it and
// writes its global ties, and a target that serves no DNS zone writes neither.
func TestReportComparisonGlobalOfferings(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, sourceName, dnsResources())

	c := assessment.Compare([]assessment.TargetAssessment{
		{Key: ovhcloudKey, Assessment: assessment.Assess(inv, testutil.DNSCatalog(t))},
		{Key: upcloudKey, Assessment: assessment.Assess(inv, testutil.UpCloudCatalog(t))},
	})
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	v2 := renderComparisonVersion(t, c, meta, jsonreport.Version2)
	testutil.Golden(t, "testdata/comparison-global.golden", v2)
	checkDocument(t, v2)

	v3 := renderComparisonVersion(t, c, meta, jsonreport.Version3)
	testutil.Golden(t, "testdata/comparison-global-v3.golden", v3)
	checkDocument(t, v3)

	document := decodeJSON(t, v3)
	ovhcloud := []any{"targets", 0}

	if got := valueAt(t, document, extend(ovhcloud, "key")...); got != ovhcloudKey {
		t.Fatalf("targets[0].key = %v, want %q: the comparison sorts its targets by key", got, ovhcloudKey)
	}

	t.Run("version 3 marks the global best offering and its global ties", func(t *testing.T) {
		t.Parallel()

		for _, test := range []struct {
			id          string
			wantFlag    bool
			wantGlobalT []string
		}{
			{id: "zone-example", wantFlag: true, wantGlobalT: []string{"DNS Zone Premium"}},
			{id: "zone-dnssec", wantFlag: false, wantGlobalT: []string{"DNS Zone", "DNS Zone Premium"}},
		} {
			at := resourceIndexAt(t, document, ovhcloud, test.id)

			if test.wantFlag {
				if diff := cmp.Diff(true, valueAt(t, document, extend(at, bestOfferingGlobalKey)...)); diff != "" {
					t.Errorf("%s.%s mismatch (-want +got):\n%s", test.id, bestOfferingGlobalKey, diff)
				}
			}

			if diff := cmp.Diff(test.wantGlobalT, stringsAt(t, document, extend(at, globalTiesKey)...)); diff != "" {
				t.Errorf("%s.%s mismatch (-want +got):\n%s", test.id, globalTiesKey, diff)
			}
		}
	})

	t.Run("the target that serves no DNS zone writes neither key", func(t *testing.T) {
		t.Parallel()

		at := resourceIndexAt(t, document, []any{"targets", 1}, "zone-example")

		if keys := objectKeysAt(t, document, at...); slices.Contains(keys, bestOfferingGlobalKey) ||
			slices.Contains(keys, globalTiesKey) {
			t.Errorf("the UpCloud resource holds a global-services key, want none: %q", keys)
		}
	})

	t.Run("version 2 writes neither key", func(t *testing.T) {
		t.Parallel()

		keys := allKeys(t, decodeJSON(t, v2))
		for _, key := range []string{bestOfferingGlobalKey, globalTiesKey} {
			if slices.Contains(keys, key) {
				t.Errorf("the schemaVersion 2 comparison holds %q, want it left out: %q", key, keys)
			}
		}
	})
}
