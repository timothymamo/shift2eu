// Package scaleway_test pins the Scaleway catalog adapter: the target contract in
// internal/adapter/target/targettest, the snapshot header the catalog reports, the
// plans, prices and fixed monthly fees the snapshot records and the default region
// they estimate in, a golden change detector for the parsed catalog, the declared
// EU regions, that a cancelled run reads nothing, and the verdicts PRD 14 asks of
// the data: high availability scoped to a node or a zone, one offering per tier,
// and a capability claimed only when the documentation meets it in full. The
// snapshot document format itself, its strictness and its errors are pinned once,
// in the shared reader's own tests (internal/adapter/target/snapshot). It is
// black-box: only the package's exported API is used.
package scaleway_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot"
	"github.com/timothymamo/shift2eu/internal/adapter/target/targettest"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The adapter implements the catalog source port: pin that at compile time.
var _ analysis.CatalogSource = scaleway.New()

// The catalog the adapter embeds, and the golden file its parsed form renders to.
// Both are relative to this package's directory.
const (
	catalogFile = "catalog.json"
	goldenFile  = "testdata/catalog.golden"
)

// wantRegions is the EU region list catalog.json declares, written out here so
// the test cannot agree with a wrong declaration by construction: the contract
// expectations below and TestRegions compare against this list, never against
// what Regions() returns.
func wantRegions() []string {
	return []string{"fr-par", "it-mil", "nl-ams", "pl-waw"}
}

// TestCatalogContract runs the target contract suite every target adapter must
// pass: the display name, every offering only in a declared EU region, the four
// categories PRD 2 requires, the same catalog on every call, and cancellation.
func TestCatalogContract(t *testing.T) {
	t.Parallel()

	targettest.Run(t, scaleway.New(), targettest.Expectations{
		Target:        "Scaleway",
		Regions:       wantRegions(),
		DefaultRegion: "fr-par",
		Categories: []taxonomy.Category{
			taxonomy.ComputeVM,
			taxonomy.KubernetesManaged,
			taxonomy.StorageObject,
			taxonomy.DatabasePostgreSQL,
		},
	})
}

// catalogOf returns the catalog a fresh source serves.
func catalogOf(t *testing.T) catalog.Catalog {
	t.Helper()

	cat, err := scaleway.New().Catalog(t.Context())
	if err != nil {
		t.Fatalf("Catalog() returned unexpected error: %v", err)
	}

	return cat
}

// TestCatalogSnapshotHeader pins that the snapshot comes from the file header: the
// documentation root the data was compiled from, and its retrieval date as UTC
// midnight. A report states both, so neither may be invented by the adapter.
func TestCatalogSnapshotHeader(t *testing.T) {
	t.Parallel()

	snapshot := catalogOf(t).Snapshot()

	// The prices page: it carries the monthly EUR amounts the snapshot records, and
	// it becomes the source once plans and prices are in it (ADR 0024).
	const wantSourceURL = "https://www.scaleway.com/en/pricing/"
	if snapshot.SourceURL != wantSourceURL {
		t.Errorf("Snapshot().SourceURL = %s, want %s", text.Quote(snapshot.SourceURL), text.Quote(wantSourceURL))
	}

	wantRetrievedOn := time.Date(2026, time.November, 13, 0, 0, 0, 0, time.UTC)
	if !snapshot.RetrievedOn.Equal(wantRetrievedOn) {
		t.Errorf("Snapshot().RetrievedOn = %s, want %s",
			snapshot.RetrievedOn.Format(time.RFC3339), wantRetrievedOn.Format(time.RFC3339))
	}

	if got := snapshot.RetrievedOn.Location(); got != time.UTC {
		t.Errorf("Snapshot().RetrievedOn location = %v, want time.UTC", got)
	}
}

// TestCatalogPlansAndPrices pins PRD #190 milestone 4: the snapshot records what an
// estimate needs (ADR 0024). Every offering of a category priced by plan sells at
// least one plan, every offering of a category priced per GiB of storage lists at
// least one storage price, every offering of a category priced by plan whose plans
// include no storage prices the storage beyond the plan, every price is a positive
// monthly amount in EUR, and every offering available in the default region can be
// priced there, so an estimate for that region never borrows another region's price
// and never reads a missing price as zero. leftOut names the offerings the coverage
// rules below are waived for, with the reason, because Scaleway publishes no price
// for them.
func TestCatalogPlansAndPrices(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// The region estimates use when the user names none (ADR 0024): Paris is the
	// region hosting every offering of the snapshot, and the existing default.
	const wantRegion = "fr-par"
	if got := cat.Snapshot().DefaultRegion; got != wantRegion {
		t.Errorf("Snapshot().DefaultRegion = %s, want %s", text.Quote(got), text.Quote(wantRegion))
	}

	// planCategories are the categories PRD #190 prices by plan: a server or a
	// managed cluster runs on one, and so does a managed database node.
	planCategories := []taxonomy.Category{
		taxonomy.ComputeVM,
		taxonomy.ComputeBareMetal,
		taxonomy.KubernetesManaged,
		taxonomy.DatabasePostgreSQL,
		taxonomy.DatabaseMySQL,
	}

	// storageCategories are the categories PRD #190 prices per GiB of storage
	// kept: block volumes, objects, and the storage a managed database uses.
	storageCategories := []taxonomy.Category{
		taxonomy.StorageBlock,
		taxonomy.StorageObject,
		taxonomy.DatabasePostgreSQL,
		taxonomy.DatabaseMySQL,
	}

	// leftOut exempts an offering from the two coverage rules, never from the
	// price rules, and gives the reason. An entry is a finding to explain in this
	// test and in SOURCES.md, not a way past a refresh.
	leftOut := map[string]string{}

	for _, o := range cat.Offerings() {
		reason, exempt := leftOut[o.Name]
		if exempt {
			t.Logf("offering %s is left out of the plan and storage-price rules: %s", text.Quote(o.Name), reason)
		}

		if !exempt && servesAny(o, planCategories) && len(o.Plans) == 0 {
			t.Errorf("offering %s serves a category priced by plan but lists no plan, want at least one",
				text.Quote(o.Name))
		}

		if !exempt && servesAny(o, storageCategories) && len(o.StoragePrices) == 0 {
			t.Errorf("offering %s serves a category priced per GiB of storage but lists no storage price, want at least one",
				text.Quote(o.Name))
		}

		// ADR 0029: a plan billing cap is UpCloud's alone. Scaleway publishes every
		// recorded price per hour and calls its monthly figure that price × 730,
		// with no maximum, so no offering may cap the hours it bills a plan for.
		if o.PlanBillingCapHours != 0 {
			t.Errorf("offering %s has PlanBillingCapHours %d, want 0: Scaleway records no monthly billing cap",
				text.Quote(o.Name), o.PlanBillingCapHours)
		}

		// A plan that includes no storage leaves every GiB of the resource to be
		// priced on its own: under ADR 0024 an empty storagePrices is not estimated,
		// never zero, so a sized resource on such a plan could not be estimated at
		// all. Scaleway sells Instances and Kapsule nodes without a local disk and
		// bills their volumes as Block Storage, so those offerings must price it.
		// Elastic Metal is not hit: its plans include their disks.
		if !exempt && servesAny(o, planCategories) && noPlanIncludesStorage(o) && len(o.StoragePrices) == 0 {
			t.Errorf("offering %s serves a category priced by plan and a plan of it includes no storage but it lists no storage price, want at least one: storage beyond the plan cannot be priced",
				text.Quote(o.Name))
		}

		for _, p := range o.Plans {
			for i, price := range p.Prices {
				checkPrice(t, o, fmt.Sprintf("plan %s prices[%d]", text.Quote(p.Name), i), price)
			}

			if slices.Contains(o.Regions, wantRegion) && !anyPriceAppliesIn(p.Prices, wantRegion) {
				t.Errorf("offering %s plan %s lists no price for the default region %s, want one applying there: its prices apply to %s",
					text.Quote(o.Name), text.Quote(p.Name), text.Quote(wantRegion), regionsListed(p.Prices))
			}
		}

		for i, price := range o.StoragePrices {
			checkPrice(t, o, fmt.Sprintf("storagePrices[%d]", i), price)
		}

		if slices.Contains(o.Regions, wantRegion) && len(o.StoragePrices) > 0 && !anyPriceAppliesIn(o.StoragePrices, wantRegion) {
			t.Errorf("offering %s lists storage prices but none for the default region %s, want one applying there: they apply to %s",
				text.Quote(o.Name), text.Quote(wantRegion), regionsListed(o.StoragePrices))
		}
	}
}

// TestCatalogFixedFees pins the fixed monthly fee per Kubernetes cluster the
// snapshot records (ADR 0026). Scaleway's mutualized Kapsule control plane is free:
// the offering stands for it and carries a fee of 0.00 in every region, which is a
// fee and not "no fee". The dedicated control plane is billed by the node size it
// runs on, 0.11 EUR an hour for Dedicated-4, which is 80.30 EUR a month at 730
// hours; Dedicated-4 is the smallest paid size, so that is a lower bound and not
// the only fee (Dedicated-8 costs 131.40 and Dedicated-16 255.50 a month). Every
// other offering charges no fixed fee, so no estimate adds one where Scaleway
// publishes none: the boolean of FixedFee tells a free region from no fee.
func TestCatalogFixedFees(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// feePerRegion is the control-plane fee each offering charges in each of its
	// regions: 0.00 where the offering stands for the free mutualized control
	// plane, and the Dedicated-4 control plane's 0.11 EUR an hour where it stands
	// for a dedicated one.
	const (
		mutualizedControlPlane = catalog.Amount(0)
		dedicatedControlPlane  = catalog.Amount(803000)
	)

	feePerRegion := map[string]map[string]catalog.Amount{
		// The mutualized control plane is included in the cluster's price.
		"Kubernetes Kapsule": {
			"fr-par": mutualizedControlPlane,
			"it-mil": mutualizedControlPlane,
			"nl-ams": mutualizedControlPlane,
			"pl-waw": mutualizedControlPlane,
		},
		// The dedicated control plane is billed by the size of its node.
		// Dedicated-4 is the smallest paid size, so its 80.30 EUR a month is a
		// lower bound: Dedicated-8 charges 131.40 and Dedicated-16 255.50.
		"Kubernetes Kapsule Dedicated": {
			"fr-par": dedicatedControlPlane,
			"nl-ams": dedicatedControlPlane,
			"pl-waw": dedicatedControlPlane,
		},
	}

	for _, o := range cat.Offerings() {
		want, charges := feePerRegion[o.Name]

		if !charges {
			if o.FixedFees != nil {
				t.Errorf("offering %s lists fixed fees %v, want none: Scaleway publishes a control-plane fee for its Kubernetes offerings only",
					text.Quote(o.Name), o.FixedFees)
			}

			for _, region := range o.Regions {
				if fee, ok := o.FixedFee(region); ok {
					t.Errorf("offering %s reports a fixed fee %s in region %s, want none",
						text.Quote(o.Name), fee, text.Quote(region))
				}
			}

			continue
		}

		for _, region := range o.Regions {
			wantFee, ok := want[region]
			if !ok {
				t.Errorf("offering %s: the expectation has no fee for its region %s",
					text.Quote(o.Name), text.Quote(region))

				continue
			}

			got, ok := o.FixedFee(region)
			if !ok {
				t.Errorf("offering %s reports no fixed fee in region %s, want %s",
					text.Quote(o.Name), text.Quote(region), wantFee)

				continue
			}

			if got != wantFee {
				t.Errorf("offering %s: FixedFee(%s) = %s, want %s",
					text.Quote(o.Name), text.Quote(region), got, wantFee)
			}
		}

		for region := range want {
			if !slices.Contains(o.Regions, region) {
				t.Errorf("offering %s: the expectation has a fee for region %s, which is not one of its regions %v",
					text.Quote(o.Name), text.Quote(region), o.Regions)
			}
		}
	}
}

// checkPrice pins what every price of the snapshot must be, wherever it appears: a
// positive monthly amount in EUR, applying only to regions its offering is
// available in (ADR 0024). where names the price inside its offering.
func checkPrice(t *testing.T, o catalog.Offering, where string, price catalog.Price) {
	t.Helper()

	if price.Currency != catalog.EUR {
		t.Errorf("offering %s %s: currency = %s, want %s",
			text.Quote(o.Name), where, text.Quote(price.Currency), text.Quote(catalog.EUR))
	}

	if price.Amount <= 0 {
		t.Errorf("offering %s %s: amount = %s, want more than 0: a price is never zero for unknown",
			text.Quote(o.Name), where, price.Amount)
	}

	for _, region := range price.Regions {
		if !slices.Contains(o.Regions, region) {
			t.Errorf("offering %s %s: applies to region %s, want one of the offering's regions %v",
				text.Quote(o.Name), where, text.Quote(region), o.Regions)
		}
	}
}

// noPlanIncludesStorage reports whether one of o's plans includes no storage, which
// leaves the storage the resource needs to be priced per GiB.
func noPlanIncludesStorage(o catalog.Offering) bool {
	return slices.ContainsFunc(o.Plans, func(p catalog.Plan) bool {
		return p.IncludedStorageGiB == 0
	})
}

// servesAny reports whether o serves one of categories.
func servesAny(o catalog.Offering, categories []taxonomy.Category) bool {
	for _, c := range categories {
		if slices.Contains(o.Categories, c) {
			return true
		}
	}

	return false
}

// anyPriceAppliesIn reports whether one of prices applies in region: a price that
// lists no region applies to every region of its offering (ADR 0024).
func anyPriceAppliesIn(prices []catalog.Price, region string) bool {
	return slices.ContainsFunc(prices, func(p catalog.Price) bool {
		return len(p.Regions) == 0 || slices.Contains(p.Regions, region)
	})
}

// regionsListed formats the regions prices apply to, one entry per price, for an
// error message.
func regionsListed(prices []catalog.Price) string {
	listed := make([]string, len(prices))
	for i, p := range prices {
		if len(p.Regions) == 0 {
			listed[i] = "every region"

			continue
		}

		listed[i] = strings.Join(p.Regions, ", ")
	}

	return strings.Join(listed, "; ")
}

// TestCatalogGolden pins the whole parsed catalog against a hand-written golden
// file: the snapshot header and its default region, then every offering with its
// categories, capabilities, regions, plans and prices, in the order and canonical
// form the domain gives them. It is a change detector for the snapshot data and
// for the parsing, so a refresh of catalog.json has to update this file in the
// same review, and a price may not change unnoticed.
func TestCatalogGolden(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	testutil.Golden(t, goldenFile, dump(cat))

	// The embed and the shared reader must agree: the golden file is the
	// specification of both, and the analysis may reach the catalog either way.
	data, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatalf("reading the embedded catalog %s: %v", text.Quote(catalogFile), err)
	}

	parsed, err := snapshot.Parse(data)
	if err != nil {
		t.Fatalf("snapshot.Parse() of %s returned unexpected error: %v", text.Quote(catalogFile), err)
	}

	if diff := cmp.Diff(string(dump(cat)), string(dump(parsed.Catalog))); diff != "" {
		t.Errorf("snapshot.Parse() of the embedded file differs from Catalog() (-Catalog +Parse):\n%s", diff)
	}
}

// dump renders cat as the golden file's lines: a header with where and when the
// data was taken and the region estimates use, then one block per offering,
// sorted by name: the offering's line, then one line per plan and one per storage
// price. Every list is comma-separated and an empty one is shown as "-", the
// plans and storage prices included. Lines below an offering are indented so the
// file reads as blocks. Everything comes from slices in the domain's canonical
// order, so the file is the same on every run.
func dump(cat catalog.Catalog) []byte {
	var b strings.Builder

	snapshot := cat.Snapshot()

	fmt.Fprintf(&b, "target: %s\n", cat.Target())
	fmt.Fprintf(&b, "sourceURL: %s\n", snapshot.SourceURL)
	fmt.Fprintf(&b, "retrievedOn: %s\n", snapshot.RetrievedOn.UTC().Format(time.DateOnly))
	fmt.Fprintf(&b, "defaultRegion: %s\n", snapshot.DefaultRegion)

	for _, o := range cat.Offerings() {
		fmt.Fprintf(&b, "%s | %s | %s | %s\n",
			o.Name, join(o.Categories), join(o.Capabilities), join(o.Regions))

		if len(o.Plans) == 0 {
			b.WriteString("  plans: -\n")
		}

		for _, p := range o.Plans {
			fmt.Fprintf(&b, "  plan: %s | %d vcpu | %s GiB | %d GiB storage | %s\n",
				p.Name, p.VCPUs, gib(p.MemoryMilliGiB), p.IncludedStorageGiB, joinPrices(p.Prices))
		}

		if len(o.StoragePrices) == 0 {
			b.WriteString("  storage prices: -\n")
		}

		for _, p := range o.StoragePrices {
			fmt.Fprintf(&b, "  storage price: %s\n", formatPrice(p))
		}
	}

	return []byte(b.String())
}

// joinPrices formats prices as the golden file shows them on a plan's line: each
// price as formatPrice renders it, in the domain's order, separated by "; ", or
// "-" when there are none.
func joinPrices(prices []catalog.Price) string {
	if len(prices) == 0 {
		return "-"
	}

	rendered := make([]string, len(prices))
	for i, p := range prices {
		rendered[i] = formatPrice(p)
	}

	return strings.Join(rendered, "; ")
}

// formatPrice renders a monthly list price as "<amount> <currency> @ <regions>",
// with the amount as catalog.Amount.String formats it and the regions
// comma-separated. A price that lists no region applies to every region of its
// offering, and the "-" says so.
func formatPrice(p catalog.Price) string {
	return fmt.Sprintf("%s %s @ %s", p.Amount, p.Currency, join(p.Regions))
}

// gib formats milliGiB, a memory size in thousandths of a GiB as the domain holds
// it, as a decimal number of GiB with trailing zeros dropped from the fraction:
// 8000 is "8", 7500 is "7.5". A plan's memory is always positive: the domain
// refuses anything below one thousandth of a GiB.
func gib(milliGiB int64) string {
	whole, frac := milliGiB/1_000, milliGiB%1_000

	digits := strings.TrimRight(fmt.Sprintf("%03d", frac), "0")
	if digits == "" {
		return fmt.Sprint(whole)
	}

	return fmt.Sprintf("%d.%s", whole, digits)
}

// join formats values as a comma-separated list, or "-" when there are none.
func join[S ~string](values []S) string {
	if len(values) == 0 {
		return "-"
	}

	strs := make([]string, len(values))
	for i, v := range values {
		strs[i] = string(v)
	}

	return strings.Join(strs, ", ")
}

// TestRegions pins the declared EU region list the reviewer checks against
// Scaleway's published regions, and that callers get a copy: a driving adapter
// cannot change what the adapter declares.
func TestRegions(t *testing.T) {
	t.Parallel()

	want := wantRegions()

	got := scaleway.Regions()
	if !slices.Equal(got, want) {
		t.Fatalf("Regions() = %v, want %v", got, want)
	}

	for i := range got {
		got[i] = "tampered"
	}

	if again := scaleway.Regions(); !slices.Equal(again, want) {
		t.Errorf("Regions() after changing the returned slice = %v, want %v: the result is not a copy", again, want)
	}
}

// TestComputeCategoriesSeparateVirtualMachinesFromBareMetal pins the taxonomy split
// issue #187 asks for: compute.vm is a virtual machine only, and a dedicated
// physical server is compute.baremetal. Elastic Metal is a bare-metal server, so
// it must not tie with Instances for a VM workload and win the tie by name. The
// category values are written out as catalog.json holds them: an offering's
// categories are snapshot data, not a reference to a Go constant.
func TestComputeCategoriesSeparateVirtualMachinesFromBareMetal(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	tests := []struct {
		name     string
		category taxonomy.Category
		want     []string
	}{
		{
			name:     "virtual machines",
			category: taxonomy.Category("compute.vm"),
			want:     []string{"Instances"},
		},
		{
			name:     "bare metal servers",
			category: taxonomy.Category("compute.baremetal"),
			want:     []string{"Elastic Metal"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got []string

			for _, o := range cat.Offerings() {
				if slices.Contains(o.Categories, tc.category) {
					got = append(got, o.Name)
				}
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("offerings serving %s mismatch (-want +got):\n%s", text.Quote(string(tc.category)), diff)
			}
		})
	}
}

// newResource builds r the way an inventory holds it, so the domain gets the
// canonical form: an inventory validates and normalises its resources, and
// mapping.Map expects one that went through it.
func newResource(t *testing.T, category taxonomy.Category, capabilities ...taxonomy.Capability) inventory.Resource {
	t.Helper()

	r := inventory.Resource{
		ID:           "res-example",
		RawType:      "AWS::Example::Resource",
		Category:     category,
		Region:       "eu-west-1",
		Capabilities: capabilities,
	}

	inv, err := inventory.New("aws", []inventory.Resource{r})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv.Resources()[0]
}

// candidateExpectation is what the tests below pin about one candidate: its
// name, its fit, and the capabilities it lacks. Its fields are exported because
// cmp cannot compare a struct with unexported ones.
type candidateExpectation struct {
	Name    string
	Fit     mapping.Fit
	Missing []taxonomy.Capability
}

// candidatesOf lists the candidates a mapping returned, best first. Ties are not
// a field of mapping's answer: two candidates tie when they have the same fit and
// the same number of missing capabilities, and mapping lists them in offering
// name order. Pinning the whole ordered list therefore pins both the ranking and
// the ties.
func candidatesOf(candidates []mapping.Candidate) []candidateExpectation {
	out := make([]candidateExpectation, len(candidates))
	for i, c := range candidates {
		out[i] = candidateExpectation{Name: c.Offering.Name, Fit: c.Fit, Missing: c.Missing}
	}

	return out
}

// checkMapping fails unless got has the wanted fit and exactly the wanted
// candidates, in order.
func checkMapping(t *testing.T, got mapping.Result, wantFit mapping.Fit, want []candidateExpectation) {
	t.Helper()

	r := got.Resource

	if got.Fit != wantFit {
		t.Errorf("Map() of %s with capabilities %v: fit = %q, want %q",
			text.Quote(string(r.Category)), r.Capabilities, got.Fit, wantFit)
	}

	if diff := cmp.Diff(want, candidatesOf(got.Candidates)); diff != "" {
		t.Errorf("Map() of %s with capabilities %v: candidates mismatch (-want +got):\n%s",
			text.Quote(string(r.Category)), r.Capabilities, diff)
	}
}

// TestDatabaseHighAvailabilityScope pins PRD 14 milestone 2's split of high
// availability for Scaleway's Managed Database: the base offering is the
// Standalone tier and lacks high availability, and the High Availability option
// is a variant named after it. The variant survives the loss of a node only, so
// a resource needing zone level is partial on both offerings and pointed at
// neither.
func TestDatabaseHighAvailabilityScope(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const (
		standalone       = "Managed Database for PostgreSQL and MySQL"
		highAvailability = "Managed Database for PostgreSQL and MySQL High Availability"
	)

	tests := []struct {
		name         string
		capabilities []taxonomy.Capability
		wantFit      mapping.Fit
		want         []candidateExpectation
	}{
		{
			name:         "zone level is offered by neither the node-level variant nor the base offering",
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: standalone, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
				{Name: highAvailability, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
			},
		},
		{
			name:         "node level is the High Availability variant only",
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: highAvailability, Fit: mapping.Direct},
				{Name: standalone, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkMapping(t, mapping.Map(newResource(t, taxonomy.DatabasePostgreSQL, tc.capabilities...), cat), tc.wantFit, tc.want)
		})
	}
}

// TestKubernetesKapsuleTiers pins PRD 14 milestone 2's tier variant for
// Scaleway's managed Kubernetes: Mutualized is the default tier and the only
// offering for a resource that needs nothing above autoscaling, and the Dedicated
// control plane is a variant named after it that adds node-level high
// availability. Scaleway's control plane is unreachable if the primary zone
// fails, so neither offering claims zone level.
func TestKubernetesKapsuleTiers(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const (
		mutualized = "Kubernetes Kapsule"
		dedicated  = "Kubernetes Kapsule Dedicated"
	)

	tests := []struct {
		name         string
		capabilities []taxonomy.Capability
		wantFit      mapping.Fit
		want         []candidateExpectation
	}{
		{
			name:         "node level is the Dedicated variant only",
			capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
			wantFit:      mapping.Direct,
			want: []candidateExpectation{
				{Name: dedicated, Fit: mapping.Direct},
				{Name: mutualized, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.NodeHighAvailability}},
			},
		},
		{
			name:    "no capabilities leaves the default tier and its variant tied",
			wantFit: mapping.Direct,
			want: []candidateExpectation{
				{Name: mutualized, Fit: mapping.Direct},
				{Name: dedicated, Fit: mapping.Direct},
			},
		},
		{
			name:         "zone level is offered by neither tier",
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			wantFit:      mapping.Partial,
			want: []candidateExpectation{
				{Name: mutualized, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
				{Name: dedicated, Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.ZoneHighAvailability}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkMapping(t, mapping.Map(newResource(t, taxonomy.KubernetesManaged, tc.capabilities...), cat), tc.wantFit, tc.want)
		})
	}
}

// TestServerlessFunctionsIPv6 pins PRD 14's strict reading of ipv6: it means
// connectivity in both directions, and Scaleway documents Serverless Functions as
// reachable over IPv6 only. The offering therefore lists ipv6 for neither
// direction, and a function that depends on it is partial.
func TestServerlessFunctionsIPv6(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	got := mapping.Map(newResource(t, taxonomy.ServerlessFunction, taxonomy.IPv6), cat)

	checkMapping(t, got, mapping.Partial, []candidateExpectation{
		{Name: "Serverless Functions", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.IPv6}},
	})
}

// TestNoServerlessSQLDatabaseOffering pins PRD 14's decision that Scaleway
// Serverless SQL Database leaves the snapshot: it is PostgreSQL-compatible with
// documented differences, so it does not fully serve database.postgresql and must
// not stand in for a PostgreSQL database, even one whose only capability is
// autoscaling. Managed Database does not autoscale, so that database is partial on
// the two tiers that do serve the category.
func TestNoServerlessSQLDatabaseOffering(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	const removed = "Serverless SQL Database"

	for _, o := range cat.Offerings() {
		if o.Name == removed {
			t.Errorf("the catalog lists an offering called %s, want none: it is not fully compatible with %s",
				text.Quote(o.Name), text.Quote(string(taxonomy.DatabasePostgreSQL)))
		}
	}

	got := mapping.Map(newResource(t, taxonomy.DatabasePostgreSQL, taxonomy.Autoscaling), cat)

	checkMapping(t, got, mapping.Partial, []candidateExpectation{
		{Name: "Managed Database for PostgreSQL and MySQL", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.Autoscaling}},
		{Name: "Managed Database for PostgreSQL and MySQL High Availability", Fit: mapping.Partial, Missing: []taxonomy.Capability{taxonomy.Autoscaling}},
	})

	for _, c := range got.Candidates {
		if c.Offering.Name == removed {
			t.Errorf("Map() lists %s as a candidate, want none: it does not fully serve the category",
				text.Quote(c.Offering.Name))
		}
	}
}

// TestHighAvailabilityIsScopedAndStrictlyClaimed pins PRD 14's strict-definition
// rule: Object Storage claims neither high-availability level, because Scaleway
// documents that it rebuilds the data after a whole-zone failure, which is
// recovery, not continuity without manual failover. That every offering is scoped
// is no longer this test's job: ADR 0011 retired the unscoped value, and
// catalog.New refuses a snapshot that lists it (pinned in
// internal/domain/catalog/catalog_test.go), so no offering here can carry it.
func TestHighAvailabilityIsScopedAndStrictlyClaimed(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	var objectStorage catalog.Offering
	for _, o := range cat.Offerings() {
		if o.Name == "Object Storage" {
			objectStorage = o
		}
	}

	if objectStorage.Name == "" {
		t.Fatalf("the catalog has no offering called %s: the strict-definition rule below assumes it", text.Quote("Object Storage"))
	}

	for _, capability := range []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.ZoneHighAvailability} {
		if objectStorage.Has(capability) {
			t.Errorf("offering %s lists %s, want neither high-availability level: rebuilding the data after a zone failure is recovery, not continuity",
				text.Quote(objectStorage.Name), text.Quote(string(capability)))
		}
	}
}

// TestCatalogHonoursCancelledContext pins that a cancelled run reads nothing.
func TestCatalogHonoursCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := scaleway.New().Catalog(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Catalog() with a cancelled context returned error %v, want one wrapping %v", err, context.Canceled)
	}

	if strings.ContainsAny(err.Error(), "\r\n") {
		t.Errorf("Catalog() error = %q, want a single line", err.Error())
	}

	// PRD 11: no error says "OVHcloud" or "UpCloud" when the target is Scaleway.
	// The other adapters' own tests pin the other directions.
	for _, other := range []string{"OVHcloud", "UpCloud"} {
		if strings.Contains(err.Error(), other) {
			t.Errorf("Catalog() error = %q, want no other target's name in it, but it names %q", err.Error(), other)
		}
	}
}
