package markdown_test

import (
	"fmt"
	"io"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The benchmarks below measure the Markdown renderer at the size the HTTP API
// can hand it (issue #310): an inventory at the resource cap, spread over
// thousands of workloads, with estimates on. The per-workload estimate lookup
// once cost one pass over every verdict per workload (issue #246), which this
// size makes visible; the many-workloads benchmark of the estimate domain
// measures the assessment, not the report.
//
// They generate their data, so they need no fixture file and no network, and
// make no assertion on time or memory: they run only under -bench, which make
// test does not pass.

// benchMaxResources is inventoryfile.MaxResources, the most resources an
// inventory file may hold and so the most a report can be asked to render. The
// Markdown adapter may not import the source adapter that declares it, so the
// number is written out here, as the estimate domain's benchmark does.
const benchMaxResources = 50000

// benchWorkloads is how many workloads the benchmark spreads benchMaxResources
// resources over: thousands, so the report renders thousands of workload
// sections and the per-workload estimate lookup is measured at that width.
const benchWorkloads = 2500

// benchResources returns the benchmark's inventory: n sized virtual machines in
// benchWorkloads workloads, round-robin, so each workload holds the same count.
// Every value is invented and the fixtures' documentation values are used where
// an inventory holds one: no account, subscription or project identifier and no
// address of a real one belongs in a test.
func benchResources(n int) []inventory.Resource {
	resources := make([]inventory.Resource, 0, n)

	for i := range n {
		vcpus := float64(i%4 + 1)
		memory := vcpus * 2

		resources = append(resources, inventory.Resource{
			ID:           fmt.Sprintf("i-%08d", i+1),
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Size:         &inventory.Size{VCPUs: &vcpus, MemoryGiB: &memory},
			Workload:     fmt.Sprintf("svc-%05d", i%benchWorkloads),
		})
	}

	return resources
}

// benchInventory is benchResources as the only constructor builds an inventory.
func benchInventory(b *testing.B) inventory.Inventory {
	b.Helper()

	inv, err := inventory.New(sourceName, benchResources(benchMaxResources))
	if err != nil {
		b.Fatalf("inventory.New() for %d resources returned unexpected error: %v", benchMaxResources, err)
	}

	return inv
}

// BenchmarkReportManyWorkloads measures rendering the report of an inventory at
// the resource cap, spread over benchWorkloads workloads, with estimates on. The
// assessment is made once, before the loop.
func BenchmarkReportManyWorkloads(b *testing.B) {
	a := assessment.AssessWith(benchInventory(b), testutil.ReportCatalog(b), assessment.Options{Estimate: true})
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}
	reporter := markdown.New()

	b.ReportAllocs()

	for b.Loop() {
		if err := reporter.Report(b.Context(), io.Discard, a, meta); err != nil {
			b.Fatalf("Report() returned unexpected error: %v", err)
		}
	}
}

// BenchmarkReportComparisonManyWorkloads measures rendering the comparison of
// the same inventory against two targets, each with its own default region, with
// estimates on. Both assessments are made once, before the loop.
func BenchmarkReportComparisonManyWorkloads(b *testing.B) {
	inv := benchInventory(b)

	// The comparison fixture's two targets: the same invented prices, with display
	// names, snapshots and default regions that differ, so the comparison renders
	// two regions and two estimates.
	c := assessment.Compare([]assessment.TargetAssessment{
		{
			Key:        reportKey,
			Assessment: assessment.AssessWith(inv, testutil.ReportCatalog(b), assessment.Options{Estimate: true}),
		},
		{
			Key: reportNorthKey,
			Assessment: assessment.AssessWith(inv, testutil.ReportCatalogIn(b, reportTwoTarget, reportTwoSnapshot()),
				assessment.Options{Estimate: true}),
		},
	})

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}
	reporter := markdown.New()

	b.ReportAllocs()

	for b.Loop() {
		if err := reporter.ReportComparison(b.Context(), io.Discard, c, meta); err != nil {
			b.Fatalf("ReportComparison() returned unexpected error: %v", err)
		}
	}
}
