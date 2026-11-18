package assessment_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
)

// monthlyCost is an invented monthly cost, as the source reports it.
func monthlyCost(amount float64, currency string) *inventory.Cost {
	return &inventory.Cost{Amount: f(amount), Currency: currency}
}

// workloadResources is one inventory holding everything the per-workload part of
// an assessment reads (PRD #190, ADR 0028): the workloads "blog" and "shop", a
// resource that names none, a relation onto a resource the priced fixture blocks,
// and the source's own monthly costs, in two currencies and missing on one
// resource. Every ID, workload and amount is invented.
//
// Against the priced fixture it assesses to three direct, one partial and one
// blocked resource, so one group holds every fit, and to EUR 37.50 and USD 3.00
// of the source's cost.
func workloadResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:       "bucket-loose",
			RawType:  "AWS::S3::Bucket",
			Category: taxonomy.StorageObject,
			Region:   "eu-west-1",
			Size:     &inventory.Size{StorageGiB: f(1000)},
		},
		{
			ID:          "cache-blog",
			RawType:     "AWS::EC2::Instance",
			Category:    taxonomy.ComputeVM,
			Region:      "eu-west-1",
			Size:        &inventory.Size{VCPUs: f(1), MemoryGiB: f(2)},
			Workload:    "blog",
			MonthlyCost: monthlyCost(3, "USD"),
		},
		{
			ID:          "db-shop",
			RawType:     "AWS::RDS::DBInstance",
			Category:    taxonomy.DatabasePostgreSQL,
			Region:      "eu-west-1",
			Workload:    "shop",
			MonthlyCost: monthlyCost(12.5, "EUR"),
		},
		{
			ID:          "web-1",
			RawType:     "AWS::EC2::Instance",
			Category:    taxonomy.ComputeVM,
			Region:      "eu-west-1",
			Size:        &inventory.Size{VCPUs: f(2), MemoryGiB: f(4)},
			Workload:    "shop",
			Relations:   []inventory.Relation{{Type: inventory.DependsOn, Target: "db-shop"}},
			MonthlyCost: monthlyCost(25, "EUR"),
		},
		{
			ID:           "web-2",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.Snapshots},
			Size:         &inventory.Size{VCPUs: f(2), MemoryGiB: f(4)},
			Workload:     "shop",
		},
	}
}

// workloadlessResources is workloadResources without its workloads, relations and
// monthly costs: the same resources as an inventory file without the version 2
// fields holds them (ADR 0025).
func workloadlessResources() []inventory.Resource {
	out := workloadResources()
	for i := range out {
		out[i].Workload = ""
		out[i].Relations = nil
		out[i].MonthlyCost = nil
	}

	return out
}

// groupLabel names a workload group in a failure message: its name, or the label
// a renderer gives the "No workload" group (ADR 0028).
func groupLabel(w assessment.WorkloadSummary) string {
	if w.NoWorkload {
		return "No workload"
	}

	return w.Name
}

// workloadGroup returns the group of that workload, failing the test when the
// assessment holds none: a group is absent only when the grouping is wrong.
func workloadGroup(t *testing.T, workloads []assessment.WorkloadSummary, name string) assessment.WorkloadSummary {
	t.Helper()

	for _, w := range workloads {
		if w.Name == name {
			return w
		}
	}

	labels := make([]string, 0, len(workloads))
	for _, w := range workloads {
		labels = append(labels, groupLabel(w))
	}

	t.Fatalf("no workload group %q, want one: the assessment holds %v", name, labels)

	return assessment.WorkloadSummary{}
}

// TestWorkloadGroups pins the grouping: one group per workload, by workload name
// in byte order, the "No workload" group last and only when a resource names no
// workload, no empty group, and no group at all for an inventory without
// resources (ADR 0028).
func TestWorkloadGroups(t *testing.T) {
	t.Parallel()

	type group struct {
		Name        string
		NoWorkload  bool
		ResourceIDs []string
	}

	tests := []struct {
		name      string
		resources []inventory.Resource
		want      []group
	}{
		{
			name:      "one group per workload, by name, the No workload group last",
			resources: workloadResources(),
			want: []group{
				{Name: "blog", ResourceIDs: []string{"cache-blog"}},
				{Name: "shop", ResourceIDs: []string{"db-shop", "web-1", "web-2"}},
				{NoWorkload: true, ResourceIDs: []string{"bucket-loose"}},
			},
		},
		{
			name:      "an inventory without resources has no group",
			resources: nil,
		},
		{
			name: "every resource names a workload, so there is no No workload group",
			resources: []inventory.Resource{
				{ID: "a-1", RawType: "AWS::EC2::Instance", Category: taxonomy.ComputeVM, Region: "eu-west-1", Workload: "shop"},
				{ID: "b-2", RawType: "AWS::EC2::Instance", Category: taxonomy.ComputeVM, Region: "eu-west-1", Workload: "shop"},
			},
			want: []group{
				{Name: "shop", ResourceIDs: []string{"a-1", "b-2"}},
			},
		},
		{
			// A report collapses runs of spaces, so names that differ only in
			// one are one workload; a no-break space is white space too.
			name: "runs of white space are one ASCII space",
			resources: []inventory.Resource{
				{ID: "a-1", RawType: "AWS::EC2::Instance", Category: taxonomy.ComputeVM, Region: "eu-west-1", Workload: "shop  web"},
				{ID: "b-2", RawType: "AWS::EC2::Instance", Category: taxonomy.ComputeVM, Region: "eu-west-1", Workload: "shop web"},
				{ID: "c-3", RawType: "AWS::EC2::Instance", Category: taxonomy.ComputeVM, Region: "eu-west-1", Workload: "shop\u00a0web"},
			},
			want: []group{
				{Name: "shop web", ResourceIDs: []string{"a-1", "b-2", "c-3"}},
			},
		},
		{
			// Case is visible and may be meant, so it is not folded.
			name: "case is not folded",
			resources: []inventory.Resource{
				{ID: "a-1", RawType: "AWS::EC2::Instance", Category: taxonomy.ComputeVM, Region: "eu-west-1", Workload: "Shop"},
				{ID: "b-2", RawType: "AWS::EC2::Instance", Category: taxonomy.ComputeVM, Region: "eu-west-1", Workload: "shop"},
			},
			want: []group{
				{Name: "Shop", ResourceIDs: []string{"a-1"}},
				{Name: "shop", ResourceIDs: []string{"b-2"}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := assessment.Assess(newInventory(t, tc.resources), pricedCatalog(t))

			var groups []group

			for _, w := range got.Workloads {
				if len(w.ResourceIDs) == 0 {
					t.Errorf("workload group %q holds no resource, want no empty group", groupLabel(w))
				}

				groups = append(groups, group{Name: w.Name, NoWorkload: w.NoWorkload, ResourceIDs: w.ResourceIDs})
			}

			if diff := cmp.Diff(tc.want, groups); diff != "" {
				t.Errorf("workload groups mismatch (-want +got):\n%s", diff)
			}

			if len(tc.resources) == 0 && got.Workloads != nil {
				t.Errorf("Workloads = %v, want nil for an inventory without resources", got.Workloads)
			}
		})
	}
}

// TestWorkloadGroupCounts pins what a group counts: its resources in verdict (ID)
// order, its fits per Summary, and its own subset of Assessment.Blockers in the
// assessment's order (ADR 0028).
func TestWorkloadGroupCounts(t *testing.T) {
	t.Parallel()

	got := assessment.Assess(newInventory(t, workloadResources()), pricedCatalog(t))

	tests := []struct {
		name         string
		workload     string
		wantIDs      []string
		wantSummary  assessment.Summary
		wantBlockers []assessment.Blocker
	}{
		{
			name:        "a group of one direct resource",
			workload:    "blog",
			wantIDs:     []string{"cache-blog"},
			wantSummary: assessment.Summary{Total: 1, Direct: 1},
		},
		{
			name:        "a group with every fit and a blocker",
			workload:    "shop",
			wantIDs:     []string{"db-shop", "web-1", "web-2"},
			wantSummary: assessment.Summary{Total: 3, Direct: 1, Partial: 1, None: 1},
			// The priced fixture sells neither database, so the database is the
			// group's only blocker.
			wantBlockers: []assessment.Blocker{{
				ResourceID: "db-shop",
				Reason: mapping.Reason{
					Code:     mapping.NoOffering,
					Target:   "Example Cloud",
					Category: taxonomy.DatabasePostgreSQL,
				},
			}},
		},
		{
			name:        "the No workload group",
			workload:    "",
			wantIDs:     []string{"bucket-loose"},
			wantSummary: assessment.Summary{Total: 1, Direct: 1},
		},
	}

	total := assessment.Summary{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := workloadGroup(t, got.Workloads, tc.workload)

			if diff := cmp.Diff(tc.wantIDs, w.ResourceIDs); diff != "" {
				t.Errorf("ResourceIDs mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantSummary, w.Summary); diff != "" {
				t.Errorf("Summary mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantBlockers, w.Blockers); diff != "" {
				t.Errorf("Blockers mismatch (-want +got):\n%s", diff)
			}
		})

		total.Total += tc.wantSummary.Total
		total.Direct += tc.wantSummary.Direct
		total.Partial += tc.wantSummary.Partial
		total.None += tc.wantSummary.None
	}

	// The groups partition the assessment: their counts are its counts, and
	// their blockers are its blockers, each blocker in the group of its
	// resource.
	if diff := cmp.Diff(got.Summary, total); diff != "" {
		t.Errorf("the groups' counts do not add up to Assessment.Summary (-groups +assessment):\n%s", diff)
	}

	blockers := 0

	for _, w := range got.Workloads {
		blockers += len(w.Blockers)

		var want []assessment.Blocker

		for _, b := range got.Blockers {
			if holdsID(w.ResourceIDs, b.ResourceID) {
				want = append(want, b)
			}
		}

		if diff := cmp.Diff(want, w.Blockers); diff != "" {
			t.Errorf("the Blockers of workload group %q are not the assessment's those resources (-want +got):\n%s",
				groupLabel(w), diff)
		}
	}

	if blockers != len(got.Blockers) {
		t.Errorf("the groups hold %d blockers, want the assessment's %d", blockers, len(got.Blockers))
	}
}

// TestWorkloadGroupTotal pins the group's Total: estimate.Sum over the group's
// verdicts, with the resources that have no estimate counted and never added as
// 0 EUR, and nil when estimates are off or the inventory is assessed by Assess
// (ADR 0028).
func TestWorkloadGroupTotal(t *testing.T) {
	t.Parallel()

	type assess func(inv inventory.Inventory, cat catalog.Catalog) assessment.Assessment

	tests := []struct {
		name   string
		assess assess
		// resources is the inventory assessed, nil for workloadResources().
		resources []inventory.Resource
		// groups is how many workload groups that inventory assesses to.
		groups int
		// want is nil when the assessment estimates nothing: every group's Total
		// is nil then.
		want map[string]*estimate.Total
	}{
		{
			name:   "Assess estimates nothing",
			assess: assessment.Assess,
			groups: 3,
		},
		{
			name: "AssessWith with estimates off",
			assess: func(inv inventory.Inventory, cat catalog.Catalog) assessment.Assessment {
				return assessment.AssessWith(inv, cat, assessment.Options{})
			},
			groups: 3,
		},
		{
			name: "AssessWith with estimates on",
			assess: func(inv inventory.Inventory, cat catalog.Catalog) assessment.Assessment {
				return assessment.AssessWith(inv, cat, assessment.Options{Estimate: true})
			},
			groups: 3,
			// The blocked database is counted in Resources, its source cost is
			// not added to the estimate, and the free-estimated resource is
			// still an estimate. cache-blog is priced on a plan that includes no
			// storage, on an offering that prices storage, and gives no storage
			// size, so its estimate leaves out a boot volume (ADR 0032).
			want: map[string]*estimate.Total{
				"blog": {Monthly: estimate.FromAmount(120000), Estimated: 1, Resources: 1, StorageNotIncluded: 1},
				"shop": {Monthly: estimate.FromAmount(500000), Estimated: 2, Resources: 3},
				"":     {Monthly: estimate.FromAmount(150000), Estimated: 1, Resources: 1},
			},
		},
		{
			// Every resource of the group is unestimated, and the group still
			// has a Total: none was estimated and nothing was added, so a report
			// can print "not estimated" instead of 0.00 EUR (ADR 0028).
			name: "AssessWith with estimates on, a group of unestimated resources only",
			assess: func(inv inventory.Inventory, cat catalog.Catalog) assessment.Assessment {
				return assessment.AssessWith(inv, cat, assessment.Options{Estimate: true})
			},
			resources: []inventory.Resource{
				{
					ID: "db-shop", RawType: "AWS::RDS::DBInstance",
					Category: taxonomy.DatabasePostgreSQL, Region: "eu-west-1",
					Workload: "shop", MonthlyCost: monthlyCost(12.5, "EUR"),
				},
			},
			groups: 1,
			want: map[string]*estimate.Total{
				"shop": {Estimated: 0, Resources: 1},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resources := tc.resources
			if resources == nil {
				resources = workloadResources()
			}

			got := tc.assess(newInventory(t, resources), pricedCatalog(t))

			if len(got.Workloads) != tc.groups {
				t.Fatalf("Workloads holds %d groups, want %d", len(got.Workloads), tc.groups)
			}

			for _, w := range got.Workloads {
				want, ok := tc.want[w.Name]
				if len(tc.want) > 0 && !ok {
					t.Errorf("no expectation for workload group %q, want one", groupLabel(w))

					continue
				}

				if diff := cmp.Diff(want, w.Total); diff != "" {
					t.Errorf("the Total of workload group %q mismatch (-want +got):\n%s", groupLabel(w), diff)
				}
			}
		})
	}
}

// TestAssessmentSourceCost pins the source's own cost, per workload and in the
// assessment's total: estimate.SumSourceCost over the matching resources, in
// currency order, whether estimates are on or off (ADR 0028).
func TestAssessmentSourceCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts assessment.Options
	}{
		{name: "estimates off"},
		{name: "estimates on", opts: assessment.Options{Estimate: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inv := newInventory(t, workloadResources())
			got := assessment.AssessWith(inv, pricedCatalog(t), tc.opts)

			want := estimate.SourceCost{
				Currencies: []estimate.CurrencyCost{
					{Currency: "EUR", Monthly: estimate.Ratio(75, 2), Resources: 2},
					{Currency: "USD", Monthly: estimate.Ratio(3, 1), Resources: 1},
				},
				Covered:   3,
				Resources: 5,
			}

			if diff := cmp.Diff(want, got.SourceCost); diff != "" {
				t.Errorf("Assessment.SourceCost mismatch (-want +got):\n%s", diff)
			}

			// The assessment's cost is the sum over every resource.
			if diff := cmp.Diff(estimate.SumSourceCost(inv.Resources()), got.SourceCost); diff != "" {
				t.Errorf("Assessment.SourceCost is not SumSourceCost over the inventory (-want +got):\n%s", diff)
			}

			wantGroups := map[string]estimate.SourceCost{
				"blog": {
					Currencies: []estimate.CurrencyCost{{Currency: "USD", Monthly: estimate.Ratio(3, 1), Resources: 1}},
					Covered:    1,
					Resources:  1,
				},
				"shop": {
					Currencies: []estimate.CurrencyCost{{Currency: "EUR", Monthly: estimate.Ratio(75, 2), Resources: 2}},
					Covered:    2,
					Resources:  3,
				},
				"": {Resources: 1},
			}

			for _, w := range got.Workloads {
				want, ok := wantGroups[w.Name]
				if !ok {
					t.Errorf("no expectation for workload group %q, want one", groupLabel(w))

					continue
				}

				if diff := cmp.Diff(want, w.SourceCost); diff != "" {
					t.Errorf("the SourceCost of workload group %q mismatch (-want +got):\n%s", groupLabel(w), diff)
				}

				delete(wantGroups, w.Name)
			}

			if len(wantGroups) > 0 {
				t.Errorf("the assessment holds no group for %v", wantGroups)
			}
		})
	}
}

// TestShowWorkloads pins when a report shows a per-workload section: only when at
// least one resource names a workload, while Workloads is filled either way
// (ADR 0028).
func TestShowWorkloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources []inventory.Resource
		want      bool
		// groups is how many groups the assessment holds.
		groups int
	}{
		{
			// A version 1 inventory can hold no workload: its one group repeats
			// the totals a report already shows.
			name:      "no resource names a workload",
			resources: workloadlessResources(),
			groups:    1,
		},
		{
			name:   "no resource at all",
			groups: 0,
		},
		{
			name: "one named workload is enough",
			resources: []inventory.Resource{
				{ID: "bucket-loose", RawType: "AWS::S3::Bucket", Category: taxonomy.StorageObject, Region: "eu-west-1"},
				{ID: "web-1", RawType: "AWS::EC2::Instance", Category: taxonomy.ComputeVM, Region: "eu-west-1", Workload: "shop"},
			},
			want:   true,
			groups: 2,
		},
		{
			name:      "every resource of the fixture",
			resources: workloadResources(),
			want:      true,
			groups:    3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := assessment.Assess(newInventory(t, tc.resources), pricedCatalog(t))

			if got.ShowWorkloads() != tc.want {
				t.Errorf("ShowWorkloads() = %t, want %t", got.ShowWorkloads(), tc.want)
			}

			if len(got.Workloads) != tc.groups {
				t.Errorf("Workloads holds %d groups, want %d", len(got.Workloads), tc.groups)
			}

			// Without a named workload the one group is the "No workload" group,
			// filled with every resource.
			if !tc.want && tc.groups > 0 {
				w := got.Workloads[0]

				if !w.NoWorkload {
					t.Errorf("the only group is %q, want the No workload group", groupLabel(w))
				}

				if diff := cmp.Diff(verdictIDs(got.Verdicts), w.ResourceIDs); diff != "" {
					t.Errorf("the No workload group's ResourceIDs mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestAssessFillsWorkloadsRelationsAndCost pins that Assess and AssessWith fill
// the workload groups, the blocker notes and the source's cost, and that an
// inventory's workloads, relations and costs change no fit, no blocker, no
// summary count and no total (ADR 0028: no fit changes).
func TestAssessFillsWorkloadsRelationsAndCost(t *testing.T) {
	t.Parallel()

	type assess func(inv inventory.Inventory, cat catalog.Catalog) assessment.Assessment

	tests := []struct {
		name   string
		assess assess
	}{
		{name: "Assess", assess: assessment.Assess},
		{
			name: "AssessWith with estimates off",
			assess: func(inv inventory.Inventory, cat catalog.Catalog) assessment.Assessment {
				return assessment.AssessWith(inv, cat, assessment.Options{})
			},
		},
		{
			name: "AssessWith with estimates on",
			assess: func(inv inventory.Inventory, cat catalog.Catalog) assessment.Assessment {
				return assessment.AssessWith(inv, cat, assessment.Options{Estimate: true})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := pricedCatalog(t)
			got := tc.assess(newInventory(t, workloadResources()), cat)
			want := tc.assess(newInventory(t, workloadlessResources()), cat)

			// The workloads, the notes and the source's cost are filled.
			if len(got.Workloads) != 3 {
				t.Errorf("Workloads holds %d groups, want one per workload and one for no workload (3)",
					len(got.Workloads))
			}

			if diff := cmp.Diff([]string{"db-shop"}, verdictFor(t, got.Verdicts, "web-1").AffectedBy); diff != "" {
				t.Errorf("web-1's AffectedBy mismatch (-want +got):\n%s", diff)
			}

			if got.SourceCost.Covered != 3 || got.SourceCost.Resources != 5 {
				t.Errorf("SourceCost covers %d of %d resources, want 3 of 5",
					got.SourceCost.Covered, got.SourceCost.Resources)
			}

			// And they move no verdict: without them the same inventory
			// assesses to the same fits, blockers, counts and totals.
			if diff := cmp.Diff(want.Summary, got.Summary); diff != "" {
				t.Errorf("Summary mismatch (-without workloads +with):\n%s", diff)
			}

			if diff := cmp.Diff(want.Blockers, got.Blockers); diff != "" {
				t.Errorf("Blockers mismatch (-without workloads +with):\n%s", diff)
			}

			if diff := cmp.Diff(want.Total, got.Total); diff != "" {
				t.Errorf("Total mismatch (-without workloads +with):\n%s", diff)
			}

			if len(want.Verdicts) != len(got.Verdicts) {
				t.Fatalf("got %d verdicts, want the %d of the same inventory", len(got.Verdicts), len(want.Verdicts))
			}

			for i, w := range want.Verdicts {
				g := got.Verdicts[i]

				for _, diff := range []string{
					cmp.Diff(w.Fit, g.Fit),
					cmp.Diff(w.BestOffering, g.BestOffering),
					cmp.Diff(w.Candidates, g.Candidates),
					cmp.Diff(w.Reasons, g.Reasons),
					cmp.Diff(w.RequiredChanges, g.RequiredChanges),
					cmp.Diff(w.Estimate, g.Estimate),
				} {
					if diff != "" {
						t.Errorf("the verdict of %q differs with the inventory's workload, relations or cost "+
							"(-without +with):\n%s", g.Resource.ID, diff)
					}
				}
			}
		})
	}
}

// holdsID reports whether ids holds id.
func holdsID(ids []string, id string) bool {
	for _, have := range ids {
		if have == id {
			return true
		}
	}

	return false
}
