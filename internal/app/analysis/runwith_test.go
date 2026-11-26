package analysis_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The region and estimate choices arrive at the application layer and must reach
// the assessment unchanged, with a bad region refused before anything is
// assessed or rendered (PRD #190, ADR 0027).

// f is a given float64, as an inventory size carries it.
func f(v float64) *float64 { return &v }

// pricedInventory is an inventory whose two resources carry sizes, so an
// assessment with estimates on has something to price: a compute.vm the priced
// fixture sells in both of its regions and a storage.object it sells in one.
func pricedInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	inv, err := inventory.New(sourceName, []inventory.Resource{
		{
			ID:       "bucket-example",
			RawType:  "AWS::S3::Bucket",
			Category: taxonomy.StorageObject,
			Region:   "eu-west-1",
			Size:     &inventory.Size{StorageGiB: f(1000)},
		},
		{
			ID:       "i-0123456789abcdef0",
			RawType:  "AWS::EC2::Instance",
			Category: taxonomy.ComputeVM,
			Region:   "eu-west-1",
			Size:     &inventory.Size{VCPUs: f(2), MemoryGiB: f(4)},
		},
	})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// pricedCatalog is the fixture with invented plans and prices: compute.vm in
// xx-ams and xx-par, storage.object in xx-par, whose snapshot defaults to xx-par.
func pricedCatalog(t *testing.T) catalog.Catalog {
	t.Helper()

	return testutil.PricedCatalog(t)
}

// TestRunWithPassesOptionsThrough pins that Analysis.RunWith hands the region
// choice and the estimate switch to the assessment, and that the reporter gets
// the assessment they produced.
func TestRunWithPassesOptionsThrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		opts               analysis.Options
		wantRegion         string
		wantEstimateRegion string
		wantTotal          *estimate.Total
	}{
		{
			// The zero Options is Run: every region, no estimates.
			name: "the zero options",
		},
		{
			name:               "estimates on, in the snapshot's default region",
			opts:               analysis.Options{Estimate: true},
			wantEstimateRegion: testutil.PricedDefaultRegion,
			wantTotal:          &estimate.Total{Monthly: estimate.FromAmount(400000), Estimated: 2, Resources: 2},
		},
		{
			name:               "a chosen region with estimates",
			opts:               analysis.Options{Region: "xx-ams", Estimate: true},
			wantRegion:         "xx-ams",
			wantEstimateRegion: "xx-ams",
			wantTotal:          &estimate.Total{Monthly: estimate.FromAmount(260000), Estimated: 1, Resources: 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inv := pricedInventory(t)
			cat := pricedCatalog(t)
			meta := analysis.ReportMeta{InventoryName: inventoryName}

			rep := &fakeReporter{body: reportBody}

			useCase, err := analysis.New(&fakeInventory{inv: inv}, &fakeCatalog{cat: cat}, rep)
			if err != nil {
				t.Fatalf("New() returned unexpected error: %v", err)
			}

			var buf bytes.Buffer

			if err := useCase.RunWith(context.Background(), &buf, meta, tc.opts); err != nil {
				t.Fatalf("RunWith() returned unexpected error: %v", err)
			}

			if rep.calls != 1 {
				t.Fatalf("Report() called %d times, want 1", rep.calls)
			}

			if diff := cmp.Diff(meta, rep.gotMeta); diff != "" {
				t.Errorf("Report() metadata mismatch (-want +got):\n%s", diff)
			}

			if got := buf.String(); got != reportBody {
				t.Errorf("RunWith() wrote %q, want the reporter's output %q", got, reportBody)
			}

			got := rep.gotAssessment

			if got.Region != tc.wantRegion {
				t.Errorf("Assessment.Region = %q, want %q", got.Region, tc.wantRegion)
			}

			if got.EstimateRegion != tc.wantEstimateRegion {
				t.Errorf("Assessment.EstimateRegion = %q, want %q", got.EstimateRegion, tc.wantEstimateRegion)
			}

			if diff := cmp.Diff(tc.wantTotal, got.Total); diff != "" {
				t.Errorf("Assessment.Total mismatch (-want +got):\n%s", diff)
			}

			// The zero options must still assess exactly as Run does.
			if !tc.opts.Estimate && tc.opts.Region == "" {
				if diff := cmp.Diff(assessment.Assess(inv, cat), got); diff != "" {
					t.Errorf("RunWith() with the zero options differs from Run() (-Run +RunWith):\n%s", diff)
				}
			}
		})
	}
}

// TestRunWithUnknownRegion pins that a region the catalog does not list stops the
// run before anything is assessed or rendered, with a *RegionError that names the
// region and the target's regions and wraps ErrUnknownRegion.
func TestRunWithUnknownRegion(t *testing.T) {
	t.Parallel()

	const unknown = "xx-zz"

	rep := &fakeReporter{body: reportBody}
	invFake := &fakeInventory{inv: pricedInventory(t)}

	useCase, err := analysis.New(invFake, &fakeCatalog{cat: pricedCatalog(t)}, rep)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	var buf bytes.Buffer

	err = useCase.RunWith(context.Background(), &buf,
		analysis.ReportMeta{InventoryName: inventoryName}, analysis.Options{Region: unknown, Estimate: true})
	if err == nil {
		t.Fatal("RunWith() returned no error for a region the catalog does not list")
	}

	if !errors.Is(err, analysis.ErrUnknownRegion) {
		t.Errorf("RunWith() error = %v, want one wrapping analysis.ErrUnknownRegion", err)
	}

	var regionErr *analysis.RegionError
	if !errors.As(err, &regionErr) {
		t.Fatalf("RunWith() error = %v, want a *analysis.RegionError", err)
	}

	if regionErr.Target != "" {
		t.Errorf("RegionError.Target = %q, want \"\" for a single-target run", regionErr.Target)
	}

	if regionErr.Region != unknown {
		t.Errorf("RegionError.Region = %q, want %q", regionErr.Region, unknown)
	}

	if diff := cmp.Diff([]string{"xx-ams", "xx-par"}, regionErr.Regions); diff != "" {
		t.Errorf("RegionError.Regions mismatch (-want +got):\n%s", diff)
	}

	// The error is one line naming the region and every region the target
	// lists, so a user can correct the choice.
	message := err.Error()
	if strings.ContainsAny(message, "\n\r") {
		t.Errorf("RegionError message = %q, want a single line", message)
	}

	for _, want := range []string{unknown, "xx-ams", "xx-par"} {
		if !strings.Contains(message, want) {
			t.Errorf("RegionError message %q does not name %q", message, want)
		}
	}

	if rep.calls != 0 {
		t.Errorf("Report() called %d times, want 0: a bad region is refused before anything is assessed", rep.calls)
	}

	if invFake.calls != 0 {
		t.Errorf("Inventory() called %d times, want 0: a bad region is refused before the inventory is read",
			invFake.calls)
	}

	if got := buf.String(); got != "" {
		t.Errorf("RunWith() wrote %q, want nothing for a bad region", got)
	}
}

// globalDNSInventory is an inventory with one public DNS zone, which uses DNS
// routing, the capability only the fixture catalog's global offerings provide
// (testutil.DNSOfferings): the zone is bound to no region (PRD #581).
func globalDNSInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	inv, err := inventory.New(sourceName, []inventory.Resource{
		{
			ID:           "zone-example",
			RawType:      "AWS::Route53::HostedZone",
			Category:     taxonomy.NetworkDNS,
			Capabilities: []taxonomy.Capability{taxonomy.DNSRouting},
		},
	})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// TestRunWithRegionAndGlobalOffering pins what a chosen region does to a global
// offering end to end (PRD #581, The region model): the CLI's
// `--region ovhcloud=gra` reaches a single-target run as Options.Region "gra"
// (internal/cli, newRunner), a region the catalog declares, so the region-scoped
// assessment keeps the global "DNS Zone" as the best offering of a DNS zone, fit
// direct and with no "not offered in <region>" reason. `--region
// ovhcloud=global` names no region the catalog declares, so the run is refused
// with an error wrapping ErrUnknownRegion before anything is assessed.
func TestRunWithRegionAndGlobalOffering(t *testing.T) {
	t.Parallel()

	t.Run("a declared region keeps the global offering", func(t *testing.T) {
		t.Parallel()

		// "gra" is a region the fixture catalog declares, both as its snapshot's
		// default region and as a region of the regional "Apex DNS Zone".
		const declared = "gra"

		inv := globalDNSInventory(t)
		cat := testutil.DNSCatalog(t)
		rep := &fakeReporter{body: reportBody}

		useCase, err := analysis.New(&fakeInventory{inv: inv}, &fakeCatalog{cat: cat}, rep)
		if err != nil {
			t.Fatalf("New() returned unexpected error: %v", err)
		}

		var buf bytes.Buffer

		if err := useCase.RunWith(context.Background(), &buf,
			analysis.ReportMeta{InventoryName: inventoryName}, analysis.Options{Region: declared}); err != nil {
			t.Fatalf("RunWith() returned unexpected error: %v", err)
		}

		if rep.calls != 1 {
			t.Fatalf("Report() called %d times, want 1", rep.calls)
		}

		if got := rep.gotAssessment.Region; got != declared {
			t.Errorf("Assessment.Region = %q, want %q", got, declared)
		}

		v := verdictOf(t, rep.gotAssessment.Verdicts, "zone-example")

		if v.Fit != mapping.Direct {
			t.Errorf("the DNS zone's fit = %q, want %q: a global offering is available in every region",
				v.Fit, mapping.Direct)
		}

		if v.BestOffering != "DNS Zone" {
			t.Errorf("the DNS zone's best offering = %q, want the global %q", v.BestOffering, "DNS Zone")
		}

		if !v.BestOfferingGlobal() {
			t.Errorf("BestOfferingGlobal() = false for the best offering %q, want true", v.BestOffering)
		}

		for _, reason := range v.Reasons {
			if reason.Code == mapping.NotOfferedInRegion {
				t.Errorf("the DNS zone's reason = %q, want none: the global %q is offered in %q",
					reason, "DNS Zone", declared)
			}
		}

		if diff := cmp.Diff(reportBody, buf.String()); diff != "" {
			t.Errorf("RunWith() wrote %q, want the reporter's output %q", buf.String(), reportBody)
		}
	})

	t.Run("global is no region the catalog declares", func(t *testing.T) {
		t.Parallel()

		const notARegion = "global"

		invFake := &fakeInventory{inv: globalDNSInventory(t)}
		rep := &fakeReporter{body: reportBody}

		useCase, err := analysis.New(invFake, &fakeCatalog{cat: testutil.DNSCatalog(t)}, rep)
		if err != nil {
			t.Fatalf("New() returned unexpected error: %v", err)
		}

		var buf bytes.Buffer

		err = useCase.RunWith(context.Background(), &buf,
			analysis.ReportMeta{InventoryName: inventoryName}, analysis.Options{Region: notARegion})
		if err == nil {
			t.Fatal("RunWith() returned no error for the region \"global\", which the catalog does not declare")
		}

		if !errors.Is(err, analysis.ErrUnknownRegion) {
			t.Errorf("RunWith() error = %v, want one wrapping analysis.ErrUnknownRegion", err)
		}

		var regionErr *analysis.RegionError
		if !errors.As(err, &regionErr) {
			t.Fatalf("RunWith() error = %v, want a *analysis.RegionError", err)
		}

		if diff := cmp.Diff([]string{"gra", "sbg"}, regionErr.Regions); diff != "" {
			t.Errorf("RegionError.Regions mismatch (-want +got):\n%s", diff)
		}

		if rep.calls != 0 {
			t.Errorf("Report() called %d times, want 0: a bad region is refused before anything is assessed", rep.calls)
		}

		if invFake.calls != 0 {
			t.Errorf("Inventory() called %d times, want 0: a bad region is refused before the inventory is read",
				invFake.calls)
		}

		if got := buf.String(); got != "" {
			t.Errorf("RunWith() wrote %q, want nothing for a bad region", got)
		}
	})
}

// TestComparisonRunWith pins that each target is assessed and estimated in its own
// region: the comparison holds a total per target, and the two targets that
// differ only in their region agree on every resource but the one whose offering
// the region does not serve.
func TestComparisonRunWith(t *testing.T) {
	t.Parallel()

	cat := pricedCatalog(t)

	targets := []analysis.Target{
		// The same invented catalog twice: only the region differs.
		{Key: "alpha", Catalog: &fakeCatalog{cat: cat}, Region: "xx-par"},
		{Key: "bravo", Catalog: &fakeCatalog{cat: cat}, Region: "xx-ams"},
	}

	rep := &fakeComparisonReporter{body: comparisonBody}

	comparison, err := analysis.NewComparison(&fakeInventory{inv: pricedInventory(t)}, targets, rep)
	if err != nil {
		t.Fatalf("NewComparison() returned unexpected error: %v", err)
	}

	var buf bytes.Buffer

	if err := comparison.RunWith(context.Background(), &buf,
		analysis.ReportMeta{InventoryName: inventoryName}, analysis.ComparisonOptions{Estimate: true}); err != nil {
		t.Fatalf("RunWith() returned unexpected error: %v", err)
	}

	if rep.calls != 1 {
		t.Fatalf("ReportComparison() called %d times, want 1", rep.calls)
	}

	if got := buf.String(); got != comparisonBody {
		t.Errorf("RunWith() wrote %q, want the reporter's output %q", got, comparisonBody)
	}

	got := rep.gotComparison
	if diff := cmp.Diff([]string{"alpha", "bravo"}, comparisonKeys(got)); diff != "" {
		t.Fatalf("the comparison's targets mismatch (-want +got):\n%s", diff)
	}

	alpha, bravo := got.Targets[0].Assessment, got.Targets[1].Assessment

	tests := []struct {
		name               string
		got                assessment.Assessment
		wantRegion         string
		wantTotal          estimate.Total
		wantBucketFit      mapping.Fit
		wantComputeMonthly string
	}{
		{
			name:               "the region every offering serves",
			got:                alpha,
			wantRegion:         "xx-par",
			wantTotal:          estimate.Total{Monthly: estimate.FromAmount(400000), Estimated: 2, Resources: 2},
			wantBucketFit:      mapping.Direct,
			wantComputeMonthly: "25.00",
		},
		{
			name:               "the region the object storage offering is not sold in",
			got:                bravo,
			wantRegion:         "xx-ams",
			wantTotal:          estimate.Total{Monthly: estimate.FromAmount(260000), Estimated: 1, Resources: 2},
			wantBucketFit:      mapping.None,
			wantComputeMonthly: "26.00",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.got.Region != tc.wantRegion {
				t.Errorf("Assessment.Region = %q, want %q", tc.got.Region, tc.wantRegion)
			}

			if tc.got.EstimateRegion != tc.wantRegion {
				t.Errorf("Assessment.EstimateRegion = %q, want %q", tc.got.EstimateRegion, tc.wantRegion)
			}

			if diff := cmp.Diff(&tc.wantTotal, tc.got.Total); diff != "" {
				t.Errorf("Assessment.Total mismatch (-want +got):\n%s", diff)
			}

			bucket := verdictOf(t, tc.got.Verdicts, "bucket-example")
			if bucket.Fit != tc.wantBucketFit {
				t.Errorf("the bucket's fit = %q, want %q", bucket.Fit, tc.wantBucketFit)
			}

			machines := verdictOf(t, tc.got.Verdicts, "i-0123456789abcdef0")
			if machines.Estimate == nil {
				t.Fatalf("the compute resource's verdict carries no estimate, want one")
			}

			if got := machines.Estimate.Monthly.String(); got != tc.wantComputeMonthly {
				t.Errorf("the compute resource's estimate = %s EUR per month, want %s",
					got, tc.wantComputeMonthly)
			}
		})
	}

	// Apart from the region and the bucket, the two targets agree: the compute
	// resource keeps its fit, its candidates and its reasons.
	alphaMachines := verdictOf(t, alpha.Verdicts, "i-0123456789abcdef0")
	bravoMachines := verdictOf(t, bravo.Verdicts, "i-0123456789abcdef0")

	if alphaMachines.Fit != bravoMachines.Fit {
		t.Errorf("the compute resource's fits differ between the regions: %q and %q",
			alphaMachines.Fit, bravoMachines.Fit)
	}

	if diff := cmp.Diff(alphaMachines.Reasons, bravoMachines.Reasons); diff != "" {
		t.Errorf("the compute resource's reasons differ between the regions (-xx-par +xx-ams):\n%s", diff)
	}

	if diff := cmp.Diff(candidateOfferingNames(alphaMachines), candidateOfferingNames(bravoMachines)); diff != "" {
		t.Errorf("the compute resource's candidates differ between the regions (-xx-par +xx-ams):\n%s", diff)
	}
}

// TestComparisonRegionErrors pins that a target's region is refused, per target,
// both by RunWith and by Run: the error names the target's key, and nothing is
// rendered.
func TestComparisonRegionErrors(t *testing.T) {
	t.Parallel()

	const (
		firstKey  = "alpha"
		secondKey = "charlie"
		badKey    = "bravo"
		bad       = "xx-zz"
	)

	cat := pricedCatalog(t)

	tests := []struct {
		name string
		run  func(context.Context, *analysis.Comparison, *bytes.Buffer) error
	}{
		{
			name: "RunWith with the estimate switch on",
			run: func(ctx context.Context, c *analysis.Comparison, buf *bytes.Buffer) error {
				return c.RunWith(ctx, buf, analysis.ReportMeta{InventoryName: inventoryName},
					analysis.ComparisonOptions{Estimate: true})
			},
		},
		{
			// Run is RunWith with the zero options, and still honours each
			// target's region.
			name: "Run",
			run: func(ctx context.Context, c *analysis.Comparison, buf *bytes.Buffer) error {
				return c.Run(ctx, buf, analysis.ReportMeta{InventoryName: inventoryName})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rep := &fakeComparisonReporter{body: comparisonBody}
			invFake := &fakeInventory{inv: pricedInventory(t)}

			// The bad region is the last target's: the run must refuse it before it
			// assesses any target, not after the ones before it.
			targets := []analysis.Target{
				{Key: firstKey, Catalog: &fakeCatalog{cat: cat}, Region: "xx-par"},
				{Key: secondKey, Catalog: &fakeCatalog{cat: cat}, Region: "xx-ams"},
				{Key: badKey, Catalog: &fakeCatalog{cat: cat}, Region: bad},
			}

			comparison, err := analysis.NewComparison(invFake, targets, rep)
			if err != nil {
				t.Fatalf("NewComparison() returned unexpected error: %v", err)
			}

			var buf bytes.Buffer

			err = tc.run(context.Background(), comparison, &buf)
			if err == nil {
				t.Fatalf("the run returned no error for target %s's region %q", badKey, bad)
			}

			if !errors.Is(err, analysis.ErrUnknownRegion) {
				t.Errorf("error = %v, want one wrapping analysis.ErrUnknownRegion", err)
			}

			var regionErr *analysis.RegionError
			if !errors.As(err, &regionErr) {
				t.Fatalf("error = %v, want a *analysis.RegionError", err)
			}

			if regionErr.Target != badKey {
				t.Errorf("RegionError.Target = %q, want the key of the target at fault, %q", regionErr.Target, badKey)
			}

			if regionErr.Region != bad {
				t.Errorf("RegionError.Region = %q, want %q", regionErr.Region, bad)
			}

			if diff := cmp.Diff([]string{"xx-ams", "xx-par"}, regionErr.Regions); diff != "" {
				t.Errorf("RegionError.Regions mismatch (-want +got):\n%s", diff)
			}

			for _, want := range []string{badKey, bad, "xx-ams", "xx-par"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("RegionError message %q does not name %q", err.Error(), want)
				}
			}

			if rep.calls != 0 {
				t.Errorf("ReportComparison() called %d times, want 0: a bad region renders nothing", rep.calls)
			}

			if invFake.calls != 0 {
				t.Errorf("Inventory() called %d times, want 0: a bad region is refused before the inventory is read",
					invFake.calls)
			}

			if got := buf.String(); got != "" {
				t.Errorf("the run wrote %q, want nothing for a bad region", got)
			}
		})
	}
}

// verdictOf returns that resource's verdict, failing the test when it was
// dropped.
func verdictOf(t *testing.T, verdicts []assessment.Verdict, id string) assessment.Verdict {
	t.Helper()

	for _, v := range verdicts {
		if v.Resource.ID == id {
			return v
		}
	}

	t.Fatalf("no verdict for resource %q: the resource was dropped", id)

	return assessment.Verdict{}
}

// candidateOfferingNames returns the names of the verdict's candidate offerings,
// in candidate order.
func candidateOfferingNames(v assessment.Verdict) []string {
	if len(v.Candidates) == 0 {
		return nil
	}

	names := make([]string, 0, len(v.Candidates))
	for _, c := range v.Candidates {
		names = append(names, c.Offering.Name)
	}

	return names
}

// workloadInventory is an inventory with workloads, relations and the source's
// own monthly costs, so a run has workload groups, a blocker note and a source
// cost to hand the reporter (PRD #190, ADR 0028). Against the priced fixture the
// database is blocked, and the object storage only in a region it is not sold in.
func workloadInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	inv, err := inventory.New(sourceName, []inventory.Resource{
		{
			ID: "bucket-example", RawType: "AWS::S3::Bucket",
			Category: taxonomy.StorageObject, Region: "eu-west-1",
			Size:        &inventory.Size{StorageGiB: f(1000)},
			MonthlyCost: &inventory.Cost{Amount: f(1), Currency: "EUR"},
		},
		{
			ID: "db-example", RawType: "AWS::RDS::DBInstance",
			Category: taxonomy.DatabasePostgreSQL, Region: "eu-west-1",
			Workload:    "shop",
			MonthlyCost: &inventory.Cost{Amount: f(10), Currency: "EUR"},
		},
		{
			ID: "web-1", RawType: "AWS::EC2::Instance",
			Category: taxonomy.ComputeVM, Region: "eu-west-1",
			Size:     &inventory.Size{VCPUs: f(2), MemoryGiB: f(4)},
			Workload: "shop",
			Relations: []inventory.Relation{
				{Type: inventory.DependsOn, Target: "bucket-example"},
				{Type: inventory.DependsOn, Target: "db-example"},
			},
			MonthlyCost: &inventory.Cost{Amount: f(20), Currency: "USD"},
		},
	})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// TestRunWithPassesWorkloadsRelationsAndCostThrough pins that the reporter gets
// exactly the assessment AssessWith makes, workload groups, blocker notes and the
// source's cost included: the application layer decides none of them.
func TestRunWithPassesWorkloadsRelationsAndCostThrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts analysis.Options
		// wantNotes is what the web server is affected by in that scope.
		wantNotes []string
	}{
		{
			name:      "estimates off",
			wantNotes: []string{"db-example"},
		},
		{
			name:      "estimates on",
			opts:      analysis.Options{Estimate: true},
			wantNotes: []string{"db-example"},
		},
		{
			// The region blocks the object storage too, so the web server that
			// depends on it is affected by two blockers.
			name:      "a chosen region the object storage is not sold in",
			opts:      analysis.Options{Region: "xx-ams", Estimate: true},
			wantNotes: []string{"bucket-example", "db-example"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inv := workloadInventory(t)
			cat := pricedCatalog(t)
			rep := &fakeReporter{body: reportBody}

			useCase, err := analysis.New(&fakeInventory{inv: inv}, &fakeCatalog{cat: cat}, rep)
			if err != nil {
				t.Fatalf("New() returned unexpected error: %v", err)
			}

			var buf bytes.Buffer

			if err := useCase.RunWith(context.Background(), &buf,
				analysis.ReportMeta{InventoryName: inventoryName}, tc.opts); err != nil {
				t.Fatalf("RunWith() returned unexpected error: %v", err)
			}

			want := assessment.AssessWith(inv, cat,
				assessment.Options{Region: tc.opts.Region, Estimate: tc.opts.Estimate})

			if diff := cmp.Diff(want, rep.gotAssessment); diff != "" {
				t.Errorf("RunWith() did not hand the reporter AssessWith's assessment (-want +got):\n%s", diff)
			}

			// The assessment that reaches the reporter holds the workloads, the
			// notes and the source's cost, so the pin is not vacuous.
			if len(want.Workloads) != 2 {
				t.Fatalf("the assessment holds %d workload groups, want one per workload and one for no workload (2)",
					len(want.Workloads))
			}

			// Every resource of that inventory gives a monthly cost, so the cost
			// covers all of them.
			if want.SourceCost.Covered != 3 || want.SourceCost.Resources != 3 {
				t.Errorf("the source's cost covers %d of %d resources, want 3 of 3",
					want.SourceCost.Covered, want.SourceCost.Resources)
			}

			if diff := cmp.Diff(tc.wantNotes, verdictOf(t, want.Verdicts, "web-1").AffectedBy); diff != "" {
				t.Errorf("the notes on web-1 mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestComparisonRunWithPassesWorkloadsRelationsAndCostThrough pins the comparison
// counterpart: every target's assessment reaches the reporter as AssessWith made
// it, with its own region's blockers, counts and notes and the inventory's
// workloads and source cost.
func TestComparisonRunWithPassesWorkloadsRelationsAndCostThrough(t *testing.T) {
	t.Parallel()

	cat := pricedCatalog(t)
	inv := workloadInventory(t)

	targets := []analysis.Target{
		{Key: "alpha", Catalog: &fakeCatalog{cat: cat}, Region: "xx-par"},
		{Key: "bravo", Catalog: &fakeCatalog{cat: cat}, Region: "xx-ams"},
	}

	rep := &fakeComparisonReporter{body: comparisonBody}

	comparison, err := analysis.NewComparison(&fakeInventory{inv: inv}, targets, rep)
	if err != nil {
		t.Fatalf("NewComparison() returned unexpected error: %v", err)
	}

	var buf bytes.Buffer

	if err := comparison.RunWith(context.Background(), &buf,
		analysis.ReportMeta{InventoryName: inventoryName}, analysis.ComparisonOptions{Estimate: true}); err != nil {
		t.Fatalf("RunWith() returned unexpected error: %v", err)
	}

	got := rep.gotComparison

	if diff := cmp.Diff([]string{"alpha", "bravo"}, comparisonKeys(got)); diff != "" {
		t.Fatalf("the comparison's targets mismatch (-want +got):\n%s", diff)
	}

	tests := []struct {
		region    string
		opts      assessment.Options
		wantNotes []string
	}{
		// The region that serves every offering blocks the database only.
		{region: "xx-par", opts: assessment.Options{Region: "xx-par", Estimate: true}, wantNotes: []string{"db-example"}},
		// The region without the object storage blocks both.
		{
			region:    "xx-ams",
			opts:      assessment.Options{Region: "xx-ams", Estimate: true},
			wantNotes: []string{"bucket-example", "db-example"},
		},
	}

	for i, tc := range tests {
		t.Run(tc.region, func(t *testing.T) {
			t.Parallel()

			reported := got.Targets[i].Assessment

			want := assessment.AssessWith(inv, cat, tc.opts)
			if diff := cmp.Diff(want, reported); diff != "" {
				t.Errorf("target %q did not get AssessWith's assessment (-want +got):\n%s", got.Targets[i].Key, diff)
			}

			if diff := cmp.Diff(tc.wantNotes, verdictOf(t, reported.Verdicts, "web-1").AffectedBy); diff != "" {
				t.Errorf("target %q's notes on web-1 mismatch (-want +got):\n%s", got.Targets[i].Key, diff)
			}
		})
	}
}
