package mapping_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// newResource is the only way to build a Resource, so a test hands Map the same
// canonical form the inventory does.
func newResource(t *testing.T, r inventory.Resource) inventory.Resource {
	t.Helper()

	inv, err := inventory.New("aws", []inventory.Resource{r})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv.Resources()[0]
}

// newCatalog is the only way to build a Catalog, so a test hands Map the same
// canonical form the catalog does.
func newCatalog(t *testing.T, offerings []catalog.Offering) catalog.Catalog {
	t.Helper()

	cat, err := catalog.New("Scaleway", testutil.ScalewaySnapshot(), offerings)
	if err != nil {
		t.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

// computeOffering is a compute.vm offering sold in fr-par only, which is also how
// the region test shows that a source region is not compared with a target one.
func computeOffering(name string, capabilities ...taxonomy.Capability) catalog.Offering {
	return catalog.Offering{
		Name:         name,
		Categories:   []taxonomy.Category{taxonomy.ComputeVM},
		Capabilities: capabilities,
		Regions:      []string{"fr-par"},
	}
}

// computeResource is a resource of category compute.vm.
func computeResource(id string, capabilities ...taxonomy.Capability) inventory.Resource {
	return inventory.Resource{
		ID:           id,
		RawType:      "AWS::EC2::Instance",
		Category:     taxonomy.ComputeVM,
		Region:       "eu-west-1",
		Capabilities: capabilities,
	}
}

// categoryOffering is an offering that serves one category in fr-par, with the
// capabilities the candidate ordering compares.
func categoryOffering(name string, category taxonomy.Category, capabilities ...taxonomy.Capability) catalog.Offering {
	return catalog.Offering{
		Name:         name,
		Categories:   []taxonomy.Category{category},
		Capabilities: capabilities,
		Regions:      []string{"fr-par"},
	}
}

// categoryResource is a resource of one category, with the capabilities it uses.
func categoryResource(id, rawType string, category taxonomy.Category, capabilities ...taxonomy.Capability) inventory.Resource {
	return inventory.Resource{
		ID:           id,
		RawType:      rawType,
		Category:     category,
		Region:       "eu-west-1",
		Capabilities: capabilities,
	}
}

func TestMap(t *testing.T) {
	t.Parallel()

	scaleway := testutil.ScalewayOfferings()

	instances := scaleway[1]
	elasticMetal := scaleway[2]
	postgresMySQL := scaleway[4]

	// Direct before a partial whose name sorts first.
	zuluDirect := computeOffering("Zulu Compute", taxonomy.IPv6)
	alphaPartial := computeOffering("Alpha Compute")

	// Two direct candidates, given out of name order.
	alphaDirect := computeOffering("Alpha Compute", taxonomy.IPv6)
	zuluDirectAgain := computeOffering("Zulu Compute", taxonomy.IPv6)

	// Two partial candidates where name order and missing-capability count
	// disagree: Alpha lacks three capabilities, Zulu one.
	alphaNoCapabilities := computeOffering("Alpha Compute")
	zuluTwoCapabilities := computeOffering("Zulu Compute", taxonomy.Autoscaling, taxonomy.IPv6)

	// Two partial candidates missing exactly one capability each, so the counts
	// tie and only the name can decide.
	alphaOneCapability := computeOffering("Alpha Compute", taxonomy.IPv6)
	zuluOneCapability := computeOffering("Zulu Compute", taxonomy.Autoscaling)

	// High availability has two scopes, and the implication between them is data
	// in the taxonomy that catalog.New applies to offerings only. A zone-level
	// offering is therefore node-level too, so the offering Map sees is the
	// expanded one; a node-level offering never becomes zone-level. A resource is
	// never expanded: it uses exactly the capabilities its inventory lists.
	zoneOffering := computeOffering("Zulu Compute", taxonomy.IPv6, taxonomy.ZoneHighAvailability)
	zoneOfferingExpanded := computeOffering("Zulu Compute", taxonomy.IPv6,
		taxonomy.NodeHighAvailability, taxonomy.ZoneHighAvailability)
	nodeOffering := computeOffering("Alpha Compute", taxonomy.IPv6, taxonomy.NodeHighAvailability)

	tests := []struct {
		name      string
		resource  inventory.Resource
		offerings []catalog.Offering
		wantFit   mapping.Fit
		wantCands []mapping.Candidate
		wantReas  []mapping.Reason
	}{
		{
			// An unclassified resource is a fact about the resource, not a
			// fallback: nothing in the catalog can change it.
			name: "unclassified resource",
			resource: inventory.Resource{
				ID:           "misc-example",
				RawType:      "AWS::EC2::Instance",
				Category:     taxonomy.Unclassified,
				Region:       "eu-west-1",
				Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			},
			offerings: scaleway,
			wantFit:   mapping.None,
			wantCands: nil,
			wantReas:  []mapping.Reason{{Code: mapping.NotClassified}},
		},
		{
			name: "no offering serves the category",
			resource: inventory.Resource{
				ID:       "fn-example",
				RawType:  "AWS::Lambda::Function",
				Category: taxonomy.ServerlessFunction,
				Region:   "eu-west-1",
			},
			offerings: scaleway,
			wantFit:   mapping.None,
			wantCands: nil,
			wantReas: []mapping.Reason{{
				Code:     mapping.NoOffering,
				Target:   "Scaleway",
				Category: taxonomy.ServerlessFunction,
			}},
		},
		{
			name:      "every capability offered is direct",
			resource:  computeResource("i-0123456789abcdef0", taxonomy.IPv6),
			offerings: scaleway,
			wantFit:   mapping.Direct,
			wantCands: []mapping.Candidate{{Offering: instances, Fit: mapping.Direct}},
			wantReas:  []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Instances"}},
		},
		{
			// A resource that uses no capabilities is direct with any offering
			// that serves its category. The fixture's other compute offering,
			// Elastic Metal, is a bare-metal server, so it is not a candidate for
			// a virtual machine (issue #187, ADR 0023).
			name:      "resource using no capabilities is direct",
			resource:  computeResource("i-0123456789abcdef0"),
			offerings: scaleway,
			wantFit:   mapping.Direct,
			wantCands: []mapping.Candidate{{Offering: instances, Fit: mapping.Direct}},
			wantReas:  []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Instances"}},
		},
		{
			// A capability the only VM offering lacks makes the fit partial, and
			// the reason names that one missing capability.
			name:      "missing capabilities make it partial",
			resource:  computeResource("i-0123456789abcdef0", taxonomy.Autoscaling, taxonomy.IPv6, taxonomy.Snapshots),
			offerings: scaleway,
			wantFit:   mapping.Partial,
			wantCands: []mapping.Candidate{{
				Offering: instances,
				Fit:      mapping.Partial,
				Missing:  []taxonomy.Capability{taxonomy.Snapshots},
			}},
			wantReas: []mapping.Reason{
				{Code: mapping.MissingCapability, Offering: "Instances", Capability: taxonomy.Snapshots},
			},
		},
		{
			// The category split is symmetric: a bare-metal resource is served
			// only by the bare-metal offering, never by a virtual machine.
			name: "bare metal resource is served by the bare metal offering",
			resource: inventory.Resource{
				ID:           "i-0123456789abcdef0",
				RawType:      "AWS::EC2::Instance",
				Category:     taxonomy.ComputeBareMetal,
				Region:       "eu-west-1",
				Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			},
			offerings: scaleway,
			wantFit:   mapping.Direct,
			wantCands: []mapping.Candidate{{Offering: elasticMetal, Fit: mapping.Direct}},
			wantReas:  []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Elastic Metal"}},
		},
		{
			name:      "direct comes before a partial that sorts first",
			resource:  computeResource("i-0123456789abcdef0", taxonomy.IPv6),
			offerings: []catalog.Offering{zuluDirect, alphaPartial},
			wantFit:   mapping.Direct,
			wantCands: []mapping.Candidate{
				{Offering: zuluDirect, Fit: mapping.Direct},
				{Offering: alphaPartial, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.IPv6}},
			},
			wantReas: []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Zulu Compute"}},
		},
		{
			name:      "two direct candidates are ordered by name",
			resource:  computeResource("i-0123456789abcdef0", taxonomy.IPv6),
			offerings: []catalog.Offering{zuluDirectAgain, alphaDirect},
			wantFit:   mapping.Direct,
			wantCands: []mapping.Candidate{
				{Offering: alphaDirect, Fit: mapping.Direct},
				{Offering: zuluDirectAgain, Fit: mapping.Direct},
			},
			wantReas: []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Alpha Compute"}},
		},
		{
			// The number of missing capabilities comes before the name: Zulu
			// lacks one, Alpha three.
			name:      "fewer missing capabilities comes before the name",
			resource:  computeResource("i-0123456789abcdef0", taxonomy.Autoscaling, taxonomy.IPv6, taxonomy.Snapshots),
			offerings: []catalog.Offering{zuluTwoCapabilities, alphaNoCapabilities},
			wantFit:   mapping.Partial,
			wantCands: []mapping.Candidate{
				{
					Offering: zuluTwoCapabilities,
					Fit:      mapping.Partial,
					Missing:  []taxonomy.Capability{taxonomy.Snapshots},
				},
				{
					Offering: alphaNoCapabilities,
					Fit:      mapping.Partial,
					Missing:  []taxonomy.Capability{taxonomy.Autoscaling, taxonomy.IPv6, taxonomy.Snapshots},
				},
			},
			wantReas: []mapping.Reason{
				{Code: mapping.MissingCapability, Offering: "Zulu Compute", Capability: taxonomy.Snapshots},
			},
		},
		{
			// One missing capability each: the counts tie, so the name decides.
			name:      "equally many missing capabilities are ordered by name",
			resource:  computeResource("i-0123456789abcdef0", taxonomy.Autoscaling, taxonomy.IPv6),
			offerings: []catalog.Offering{zuluOneCapability, alphaOneCapability},
			wantFit:   mapping.Partial,
			wantCands: []mapping.Candidate{
				{
					Offering: alphaOneCapability,
					Fit:      mapping.Partial,
					Missing:  []taxonomy.Capability{taxonomy.Autoscaling},
				},
				{
					Offering: zuluOneCapability,
					Fit:      mapping.Partial,
					Missing:  []taxonomy.Capability{taxonomy.IPv6},
				},
			},
			wantReas: []mapping.Reason{
				{Code: mapping.MissingCapability, Offering: "Alpha Compute", Capability: taxonomy.Autoscaling},
			},
		},
		{
			// A resource that must keep serving through the loss of one node is
			// direct with an offering that keeps serving through the loss of a
			// whole zone.
			name:      "node level need is met by a zone level offering",
			resource:  computeResource("i-0123456789abcdef0", taxonomy.IPv6, taxonomy.NodeHighAvailability),
			offerings: []catalog.Offering{zoneOffering},
			wantFit:   mapping.Direct,
			wantCands: []mapping.Candidate{{Offering: zoneOfferingExpanded, Fit: mapping.Direct}},
			wantReas:  []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Zulu Compute"}},
		},
		{
			// The implication runs downwards only: a node-level offering does not
			// keep serving through the loss of a zone, so a zone-level need is
			// partial, and the one missing capability is the zone level.
			name:      "zone level need is partial on a node level offering",
			resource:  computeResource("db-example", taxonomy.ZoneHighAvailability),
			offerings: []catalog.Offering{nodeOffering},
			wantFit:   mapping.Partial,
			wantCands: []mapping.Candidate{{
				Offering: nodeOffering,
				Fit:      mapping.Partial,
				Missing:  []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			}},
			wantReas: []mapping.Reason{{
				Code:       mapping.MissingCapability,
				Offering:   "Alpha Compute",
				Capability: taxonomy.ZoneHighAvailability,
			}},
		},
		{
			// A resource is never expanded: a zone-level resource misses exactly
			// one capability even from an offering without high availability, so
			// Required changes names the zone level alone, not the node level too.
			name:      "a zone level resource misses exactly one capability",
			resource:  computeResource("db-example", taxonomy.ZoneHighAvailability),
			offerings: []catalog.Offering{computeOffering("Alpha Compute")},
			wantFit:   mapping.Partial,
			wantCands: []mapping.Candidate{{
				Offering: computeOffering("Alpha Compute"),
				Fit:      mapping.Partial,
				Missing:  []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			}},
			wantReas: []mapping.Reason{{
				Code:       mapping.MissingCapability,
				Offering:   "Alpha Compute",
				Capability: taxonomy.ZoneHighAvailability,
			}},
		},
		{
			// MySQL is served by the one offering whose category list has it.
			name: "mysql served by a multi-category offering",
			resource: inventory.Resource{
				ID:           "db-example",
				RawType:      "AWS::RDS::DBInstance",
				Category:     taxonomy.DatabaseMySQL,
				Region:       "eu-west-1",
				Capabilities: []taxonomy.Capability{taxonomy.ReadReplicas},
			},
			offerings: scaleway,
			wantFit:   mapping.Direct,
			wantCands: []mapping.Candidate{{Offering: postgresMySQL, Fit: mapping.Direct}},
			wantReas: []mapping.Reason{
				{Code: mapping.AllCapabilities, Offering: "Managed Database for PostgreSQL and MySQL"},
			},
		},
		{
			// The source region is never compared with the target regions.
			name:      "region is ignored",
			resource:  computeResource("i-0123456789abcdef0", taxonomy.IPv6),
			offerings: []catalog.Offering{computeOffering("Instances", taxonomy.IPv6)},
			wantFit:   mapping.Direct,
			wantCands: []mapping.Candidate{{Offering: computeOffering("Instances", taxonomy.IPv6), Fit: mapping.Direct}},
			wantReas:  []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Instances"}},
		},
		// PRD #552: udp is a capability of a load balancer, and sharding of a
		// cache. An offering that does not list it accepts the resource partially
		// and the reason names the capability, a UDP load balancer on Scaleway.
		{
			name: "a UDP load balancer is partial on an offering without udp",
			resource: categoryResource("lb-example", "AWS::ElasticLoadBalancingV2::LoadBalancer",
				taxonomy.NetworkLoadBalancer, taxonomy.UDP),
			offerings: []catalog.Offering{categoryOffering("Load Balancer", taxonomy.NetworkLoadBalancer)},
			wantFit:   mapping.Partial,
			wantCands: []mapping.Candidate{{
				Offering: categoryOffering("Load Balancer", taxonomy.NetworkLoadBalancer),
				Fit:      mapping.Partial,
				Missing:  []taxonomy.Capability{taxonomy.UDP},
			}},
			wantReas: []mapping.Reason{{
				Code:       mapping.MissingCapability,
				Offering:   "Load Balancer",
				Capability: taxonomy.UDP,
			}},
		},
		{
			name: "a UDP load balancer is direct on an offering that lists udp",
			resource: categoryResource("lb-example", "AWS::ElasticLoadBalancingV2::LoadBalancer",
				taxonomy.NetworkLoadBalancer, taxonomy.UDP),
			offerings: []catalog.Offering{
				categoryOffering("Load Balancer", taxonomy.NetworkLoadBalancer, taxonomy.UDP),
			},
			wantFit: mapping.Direct,
			wantCands: []mapping.Candidate{{
				Offering: categoryOffering("Load Balancer", taxonomy.NetworkLoadBalancer, taxonomy.UDP),
				Fit:      mapping.Direct,
			}},
			wantReas: []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Load Balancer"}},
		},
		{
			name: "a sharded cache is partial on an offering without sharding",
			resource: categoryResource("cache-example", "AWS::ElastiCache::ReplicationGroup",
				taxonomy.CacheRedis, taxonomy.Sharding),
			offerings: []catalog.Offering{categoryOffering("Managed Valkey", taxonomy.CacheRedis)},
			wantFit:   mapping.Partial,
			wantCands: []mapping.Candidate{{
				Offering: categoryOffering("Managed Valkey", taxonomy.CacheRedis),
				Fit:      mapping.Partial,
				Missing:  []taxonomy.Capability{taxonomy.Sharding},
			}},
			wantReas: []mapping.Reason{{
				Code:       mapping.MissingCapability,
				Offering:   "Managed Valkey",
				Capability: taxonomy.Sharding,
			}},
		},
		{
			name: "a sharded cache is direct on an offering that lists sharding",
			resource: categoryResource("cache-example", "AWS::ElastiCache::ReplicationGroup",
				taxonomy.CacheRedis, taxonomy.Sharding),
			offerings: []catalog.Offering{
				categoryOffering("Managed Valkey", taxonomy.CacheRedis, taxonomy.Sharding),
			},
			wantFit: mapping.Direct,
			wantCands: []mapping.Candidate{{
				Offering: categoryOffering("Managed Valkey", taxonomy.CacheRedis, taxonomy.Sharding),
				Fit:      mapping.Direct,
			}},
			wantReas: []mapping.Reason{{Code: mapping.AllCapabilities, Offering: "Managed Valkey"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resource := newResource(t, tc.resource)
			cat := newCatalog(t, tc.offerings)

			got := mapping.Map(resource, cat)

			if diff := cmp.Diff(tc.wantFit, got.Fit); diff != "" {
				t.Errorf("Fit mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantCands, got.Candidates); diff != "" {
				t.Errorf("Candidates mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantReas, got.Reasons); diff != "" {
				t.Errorf("Reasons mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(resource, got.Resource); diff != "" {
				t.Errorf("Result.Resource mismatch (-want +got):\n%s", diff)
			}

			best, ok := got.Best()
			if ok != (len(got.Candidates) > 0) {
				t.Fatalf("Best() ok = %t with %d candidates", ok, len(got.Candidates))
			}

			if ok && best.Fit != got.Fit {
				t.Errorf("Best().Fit = %q, want Result.Fit %q", best.Fit, got.Fit)
			}
		})
	}
}

// Map only reads: neither the resource nor the catalog may change, because the
// inventory and the catalog are shared with the report.
func TestMapDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	resource := newResource(t, computeResource("i-0123456789abcdef0", taxonomy.Autoscaling, taxonomy.IPv6))
	cat := newCatalog(t, testutil.ScalewayOfferings())

	wantResource := resource
	wantResource.Capabilities = slices.Clone(resource.Capabilities)

	wantOfferings := cat.Offerings()

	_ = mapping.Map(resource, cat)

	if diff := cmp.Diff(wantResource, resource); diff != "" {
		t.Errorf("resource changed by Map (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(wantOfferings, cat.Offerings()); diff != "" {
		t.Errorf("catalog changed by Map (-want +got):\n%s", diff)
	}
}

// Best is the first candidate: Map has already ordered them best first, so Best
// must not search again.
func TestResultBest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   mapping.Result
		want mapping.Candidate
		ok   bool
	}{
		{name: "no candidates", in: mapping.Result{Fit: mapping.None}, want: mapping.Candidate{}, ok: false},
		{
			// A Result Map produces: Direct candidates come before Partial ones.
			name: "first candidate of a mapped result",
			in: mapping.Result{Fit: mapping.Direct, Candidates: []mapping.Candidate{
				{Fit: mapping.Direct, Offering: catalog.Offering{Name: "Alpha Compute"}},
				{
					Fit:      mapping.Partial,
					Offering: catalog.Offering{Name: "Zulu Compute"},
					Missing:  []taxonomy.Capability{taxonomy.IPv6},
				},
			}},
			want: mapping.Candidate{Fit: mapping.Direct, Offering: catalog.Offering{Name: "Alpha Compute"}},
			ok:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tc.in.Best()
			if ok != tc.ok {
				t.Fatalf("Best() ok = %t, want %t", ok, tc.ok)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Best() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Every reason a report shows is one of these sentences, so they are pinned
// exactly. They are plain text: how to escape one for a report format is the
// renderer's job, not the domain's.
func TestReasonString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   mapping.Reason
		want string
	}{
		{
			name: "not classified",
			in:   mapping.Reason{Code: mapping.NotClassified},
			want: "not classified",
		},
		{
			name: "no offering",
			in: mapping.Reason{
				Code:     mapping.NoOffering,
				Target:   "Scaleway",
				Category: taxonomy.ServerlessFunction,
			},
			want: "no offering in the Scaleway catalog for serverless.function",
		},
		{
			name: "all capabilities",
			in:   mapping.Reason{Code: mapping.AllCapabilities, Offering: "Instances"},
			want: "Instances offers every capability the resource uses",
		},
		{
			name: "missing capability",
			in: mapping.Reason{
				Code:       mapping.MissingCapability,
				Offering:   "Elastic Metal",
				Capability: taxonomy.PointInTimeRestore,
			},
			want: "Elastic Metal does not offer point-in-time-restore",
		},
		{
			// A region the assessment is scoped to that no offering serving the
			// resource is available in (MapIn).
			name: "not offered in region",
			in: mapping.Reason{
				Code:     mapping.NotOfferedInRegion,
				Target:   "Scaleway",
				Category: taxonomy.ComputeVM,
				Region:   "fr-par",
			},
			want: "not offered in fr-par",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.in.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A reason is rendered from parts, so a reporter can style or escape the names a
// reason mentions without parsing a sentence: the text is split exactly at the
// names, and String is the concatenation of the part texts.
func TestReasonParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   mapping.Reason
		want []mapping.Part
	}{
		{
			name: "not classified",
			in:   mapping.Reason{Code: mapping.NotClassified},
			want: []mapping.Part{{Kind: mapping.PartLiteral, Text: "not classified"}},
		},
		{
			name: "no offering",
			in: mapping.Reason{
				Code:     mapping.NoOffering,
				Target:   "Scaleway",
				Category: taxonomy.ServerlessFunction,
			},
			want: []mapping.Part{
				{Kind: mapping.PartLiteral, Text: "no offering in the "},
				{Kind: mapping.PartTarget, Text: "Scaleway"},
				{Kind: mapping.PartLiteral, Text: " catalog for "},
				{Kind: mapping.PartCategory, Text: "serverless.function"},
			},
		},
		{
			name: "all capabilities",
			in:   mapping.Reason{Code: mapping.AllCapabilities, Offering: "Instances"},
			want: []mapping.Part{
				{Kind: mapping.PartOffering, Text: "Instances"},
				{Kind: mapping.PartLiteral, Text: " offers every capability the resource uses"},
			},
		},
		{
			name: "missing capability",
			in: mapping.Reason{
				Code:       mapping.MissingCapability,
				Offering:   "Elastic Metal",
				Capability: taxonomy.PointInTimeRestore,
			},
			want: []mapping.Part{
				{Kind: mapping.PartOffering, Text: "Elastic Metal"},
				{Kind: mapping.PartLiteral, Text: " does not offer "},
				{Kind: mapping.PartCapability, Text: "point-in-time-restore"},
			},
		},
		{
			// The region is a name a reporter may style or escape, so it is its
			// own part, of a kind of its own.
			name: "not offered in region",
			in: mapping.Reason{
				Code:     mapping.NotOfferedInRegion,
				Target:   "Scaleway",
				Category: taxonomy.ComputeVM,
				Region:   "xx-ams",
			},
			want: []mapping.Part{
				{Kind: mapping.PartLiteral, Text: "not offered in "},
				{Kind: mapping.PartRegion, Text: "xx-ams"},
			},
		},
		{
			// A reason whose code the domain does not know is one literal part
			// holding the code, never a name: a renderer escapes it as text.
			name: "unknown code is one literal part",
			in:   mapping.Reason{Code: "x"},
			want: []mapping.Part{{Kind: mapping.PartLiteral, Text: "x"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.in.Parts()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Parts() mismatch (-want +got):\n%s", diff)
			}

			if want := joinParts(tc.want); tc.in.String() != want {
				t.Errorf("String() = %q, want the concatenation of the part texts %q", tc.in.String(), want)
			}
		})
	}
}

// joinParts is what String must be: every part text, in order, with nothing
// between them.
func joinParts(parts []mapping.Part) string {
	var b strings.Builder

	for _, p := range parts {
		b.WriteString(p.Text)
	}

	return b.String()
}

// The domain never escapes the names it renders: a name full of markup reaches a
// reporter unchanged in one part, so the reporter can escape it for its own
// format.
func TestReasonPartsDoNotEscapeNames(t *testing.T) {
	t.Parallel()

	const name = "a|b `c` <x>"

	reason := mapping.Reason{
		Code:       mapping.MissingCapability,
		Offering:   name,
		Capability: taxonomy.Snapshots,
	}

	want := []mapping.Part{
		{Kind: mapping.PartOffering, Text: name},
		{Kind: mapping.PartLiteral, Text: " does not offer "},
		{Kind: mapping.PartCapability, Text: "snapshots"},
	}

	if diff := cmp.Diff(want, reason.Parts()); diff != "" {
		t.Errorf("Parts() mismatch (-want +got):\n%s", diff)
	}

	if got := reason.String(); !strings.Contains(got, name) {
		t.Errorf("String() = %q, want it to contain the offering name %q verbatim", got, name)
	}
}
