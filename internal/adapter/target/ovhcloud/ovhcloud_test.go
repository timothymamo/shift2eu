// Package ovhcloud_test pins the OVHcloud catalog adapter: the target contract in
// internal/adapter/target/targettest, the snapshot header the catalog reports, the
// plans, prices and fixed monthly fees the snapshot records and the default region
// they estimate in, a
// golden change detector for the parsed catalog, the declared EU regions, that a
// cancelled run reads nothing, that no error of one target names the other, and
// the two verdicts PRD 11 asks of the data: categories OVHcloud does not serve
// are a blocker, and a resource needing high availability is pointed at the
// offering whose tier provides it. Since ADR 0011 that tier is the 3-AZ variant
// for zone level and a multi-node plan for the databases, and a capability is
// listed only where the documentation states it in full, which puts read replicas
// on the Advanced database plan alone. The snapshot document
// format itself, its strictness, its errors and
// the SOURCES.md rule are pinned once, in the shared reader's own tests
// (internal/adapter/target/snapshot). It is black-box: only the package's
// exported API is used.
package ovhcloud_test

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

	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot"
	"github.com/timothymamo/shift2eu/internal/adapter/target/targettest"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The adapter implements the catalog source port: pin that at compile time.
var _ analysis.CatalogSource = ovhcloud.New()

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
	return []string{"GRA", "LIM", "MIL", "PAR", "RBX", "SBG", "WAW"}
}

// threeAZRegions are the OVHcloud regions where Paris and Milan sit: each holds
// three availability zones, so a product deployed there can be highly available
// across zones. PRD 11 lists the 3-AZ variants of such a product in these regions
// only.
func threeAZRegions() []string {
	return []string{"MIL", "PAR"}
}

// nonEURegions are OVHcloud region codes outside the EU: the United Kingdom,
// Canada, the United States and Asia-Pacific. PRD 11 keeps the catalog to EU
// member states, so no declared region may be one of these. The guard is
// readable on purpose: it names what a refresh must not add.
var nonEURegions = []string{"ERI", "UK", "LON", "BHS", "TOR", "HIL", "VIN", "SGP", "SYD", "MUM", "YNM"}

// TestCatalogContract runs the target contract suite every target adapter must
// pass: the display name, every offering only in a declared EU region, the six
// categories PRD 11 requires, the same catalog on every call, and cancellation.
func TestCatalogContract(t *testing.T) {
	t.Parallel()

	targettest.Run(t, ovhcloud.New(), targettest.Expectations{
		Target:        "OVHcloud",
		Regions:       wantRegions(),
		DefaultRegion: "GRA",
		Categories: []taxonomy.Category{
			taxonomy.ComputeVM,
			taxonomy.KubernetesManaged,
			taxonomy.StorageObject,
			taxonomy.StorageBlock,
			taxonomy.DatabasePostgreSQL,
			taxonomy.DatabaseMySQL,
		},
	})
}

// catalogOf returns the catalog a fresh source serves.
func catalogOf(t *testing.T) catalog.Catalog {
	t.Helper()

	cat, err := ovhcloud.New().Catalog(t.Context())
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

	// The English EUR edition of the prices page: the /en/ edition shows USD and
	// this one excludes VAT, so an estimate is in the currency ADR 0024 expects.
	const wantSourceURL = "https://www.ovhcloud.com/en-ie/public-cloud/prices/"
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

// TestCatalogPlansAndPrices pins PRD #190 milestone 3: the snapshot records what an
// estimate needs (ADR 0024). Every offering of a category priced by plan sells at
// least one plan, every offering of a category priced per GiB of storage lists at
// least one storage price, every price is a positive monthly amount in EUR, and
// every offering available in the default region can be priced there, so an
// estimate for that region never borrows another region's price and never reads a
// missing price as zero. leftOut names the offerings the two coverage rules below
// are waived for, with the reason, because OVHcloud publishes no price for them.
func TestCatalogPlansAndPrices(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// The region estimates use when the user names none: it is among the regions
	// hosting the most of the snapshot's offerings (12, tied with LIM and SBG,
	// against 11 in RBX and WAW and 9 in PAR and MIL) and is the existing default.
	const wantRegion = "GRA"
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
	leftOut := map[string]string{
		"Block Storage LUKS":       "OVHcloud publishes no price for encrypted (LUKS) volumes; the prices page lists only the unencrypted classes",
		"Block Storage LUKS Zonal": "OVHcloud publishes no price for encrypted (LUKS) volumes; the prices page lists only the unencrypted classes",
	}

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

		// ADR 0029: a plan billing cap is UpCloud's alone. OVHcloud publishes an
		// hourly price for every plan and calls its monthly figure that price ×
		// 730, with no maximum, so no offering may cap the hours it bills a plan
		// for.
		if o.PlanBillingCapHours != 0 {
			t.Errorf("offering %s has PlanBillingCapHours %d, want 0: OVHcloud records no monthly billing cap",
				text.Quote(o.Name), o.PlanBillingCapHours)
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
// snapshot records (ADR 0026): OVHcloud charges the Standard control plane
// 0.09 EUR an hour in RBX, PAR and MIL, which is 65.70 EUR a month at 730 hours,
// and includes it in GRA, LIM, SBG and WAW, where the offering stands for the
// Free plan. Every other offering charges no fixed fee, so no estimate adds one
// where OVHcloud publishes none. One cluster on Managed Kubernetes Service in RBX
// therefore carries 65.70 EUR, and the free regions carry 0.00 and not "no fee":
// the boolean of FixedFee tells the two apart.
func TestCatalogFixedFees(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// feePerRegion is the control-plane fee each offering charges in each of its
	// regions: the Standard plan's 65.70 EUR where it is the plan the offering
	// stands for, and 0.00 where OVHcloud includes the control plane.
	const standardControlPlane = catalog.Amount(657000)

	feePerRegion := map[string]map[string]catalog.Amount{
		"Managed Kubernetes Service": {
			"GRA": 0,
			"LIM": 0,
			"RBX": standardControlPlane,
			"SBG": 0,
			"WAW": 0,
		},
		// The 3-AZ offering sells the Standard plan only, so it charges its
		// control plane in both of its regions.
		"Managed Kubernetes Service 3-AZ": {
			"MIL": standardControlPlane,
			"PAR": standardControlPlane,
		},
	}

	for _, o := range cat.Offerings() {
		want, charges := feePerRegion[o.Name]

		if !charges {
			if o.FixedFees != nil {
				t.Errorf("offering %s lists fixed fees %v, want none: OVHcloud publishes a control-plane fee for its Kubernetes offerings only",
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
// OVHcloud's published regions page, and that callers get a copy: a driving
// adapter cannot change what the adapter declares. The order is the file's, since
// the snapshot declares the regions in the order reports list them.
func TestRegions(t *testing.T) {
	t.Parallel()

	want := wantRegions()

	got := ovhcloud.Regions()
	if !slices.Equal(got, want) {
		t.Fatalf("Regions() = %v, want %v", got, want)
	}

	for _, region := range got {
		if slices.Contains(nonEURegions, region) {
			t.Errorf("Regions() declares %s, an OVHcloud region outside the EU: PRD 11 keeps the catalog to EU member states",
				text.Quote(region))
		}
	}

	for i := range got {
		got[i] = "tampered"
	}

	if again := ovhcloud.Regions(); !slices.Equal(again, want) {
		t.Errorf("Regions() after changing the returned slice = %v, want %v: the result is not a copy", again, want)
	}
}

// TestComputeCategoriesSeparateVirtualMachinesFromBareMetal pins the taxonomy split
// issue #187 asks for: compute.vm is a virtual machine only, and a dedicated
// physical server is compute.baremetal. Metal Instances is a bare-metal server, so
// it must not tie with Instances for a VM workload. The category values are
// written out as catalog.json holds them: an offering's categories are snapshot
// data, not a reference to a Go constant.
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
			want:     []string{"Metal Instances"},
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

// TestCatalogHonoursCancelledContext pins that a cancelled run reads nothing, and
// that the error names the embedded OVHcloud catalog and its file on one line.
func TestCatalogHonoursCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := ovhcloud.New().Catalog(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Catalog() with a cancelled context returned error %v, want one wrapping %v", err, context.Canceled)
	}

	if strings.ContainsAny(err.Error(), "\r\n") {
		t.Errorf("Catalog() error = %q, want a single line", err.Error())
	}

	for _, want := range []string{"embedded OVHcloud catalog", catalogFile} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Catalog() error = %q, want it to name %q", err.Error(), want)
		}
	}
}

// TestCatalogErrorNamesNoOtherTarget pins PRD 11's rule that no error says
// "Scaleway" or "UpCloud" when the target is OVHcloud. The other adapters' own
// tests pin the other directions.
func TestCatalogErrorNamesNoOtherTarget(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := ovhcloud.New().Catalog(ctx)
	if err == nil {
		t.Fatalf("Catalog() with a cancelled context returned no error, want one naming the OVHcloud catalog")
	}

	for _, other := range []string{"Scaleway", "UpCloud"} {
		if strings.Contains(err.Error(), other) {
			t.Errorf("Catalog() error = %q, want no other target's name in it, but it names %q", err.Error(), other)
		}
	}
}

// newResource builds r the way an inventory holds it, so the domain gets the
// canonical form: an inventory validates and normalises its resources, and
// mapping.Map expects one that went through it.
func newResource(t *testing.T, id string, category taxonomy.Category, capabilities ...taxonomy.Capability) inventory.Resource {
	t.Helper()

	r := inventory.Resource{
		ID:           id,
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

// TestNoOfferingForUnservedCategories pins PRD 11's criterion that a resource of
// a category OVHcloud does not serve gets fit none, the target-neutral reason
// naming OVHcloud and the category, and is listed as a blocker. The snapshot
// serves neither serverless functions nor message queues.
func TestNoOfferingForUnservedCategories(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// PRD 11 expects no generally available OVHcloud offering to serve these. A
	// milestone-2 change that adds one updates the PRD with the snapshot, so this
	// test fails first and the review sees it.
	unserved := []taxonomy.Category{taxonomy.ServerlessFunction, taxonomy.MessagingQueue}

	for _, o := range cat.Offerings() {
		for _, category := range o.Categories {
			if slices.Contains(unserved, category) {
				t.Errorf("offering %s serves %s, want no OVHcloud offering to serve it",
					text.Quote(o.Name), text.Quote(string(category)))
			}
		}
	}

	for _, category := range unserved {
		t.Run(string(category), func(t *testing.T) {
			t.Parallel()

			r := newResource(t, "res-example", category)

			got := mapping.Map(r, cat)
			if got.Fit != mapping.None {
				t.Errorf("Map() fit = %q, want %q: OVHcloud serves no %s",
					got.Fit, mapping.None, text.Quote(string(category)))
			}

			wantReasons := []mapping.Reason{{Code: mapping.NoOffering, Target: "OVHcloud", Category: category}}
			if diff := cmp.Diff(wantReasons, got.Reasons); diff != "" {
				t.Errorf("Map() reasons mismatch (-want +got):\n%s", diff)
			}

			wantSentence := "no offering in the OVHcloud catalog for " + string(category)
			if reasons := got.Reasons; len(reasons) == 1 && reasons[0].String() != wantSentence {
				t.Errorf("reason = %q, want %q", reasons[0].String(), wantSentence)
			}

			inv, err := inventory.New("aws", []inventory.Resource{r})
			if err != nil {
				t.Fatalf("inventory.New() returned unexpected error: %v", err)
			}

			gotBlockers := assessment.Assess(inv, cat).Blockers
			wantBlockers := []assessment.Blocker{{ResourceID: r.ID, Reason: wantReasons[0]}}
			if diff := cmp.Diff(wantBlockers, gotBlockers); diff != "" {
				t.Errorf("Assess() blockers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// offeringNamed returns the catalog's offering called name, and fails the test if
// there is none.
func offeringNamed(t *testing.T, cat catalog.Catalog, name string) catalog.Offering {
	t.Helper()

	for _, o := range cat.Offerings() {
		if o.Name == name {
			return o
		}
	}

	t.Fatalf("the catalog has no offering called %s: the 3-AZ rule below assumes it", text.Quote(name))

	return catalog.Offering{}
}

// candidateFit pairs the name of a candidate with its fit, which is what the
// tests below pin without copying the snapshot's capability lists into them. Its
// fields are exported because cmp cannot compare a struct with unexported ones.
type candidateFit struct {
	Name string
	Fit  mapping.Fit
}

// fits returns candidates' names and fits, best first.
func fits(candidates []mapping.Candidate) []candidateFit {
	out := make([]candidateFit, len(candidates))
	for i, c := range candidates {
		out[i] = candidateFit{Name: c.Offering.Name, Fit: c.Fit}
	}

	return out
}

// TestThreeAZVariantsServeHighAvailability pins PRD 11's criterion that a
// resource needing high availability is pointed at the offering's 3-AZ variant,
// at the level ADR 0011 gives it: surviving the loss of a zone. OVHcloud makes
// Object Storage, Block Storage, Managed Kubernetes and its databases available
// across zones only in Paris and Milan, so the snapshot lists a 3-AZ variant there
// beside the single-zone offering, and that variant is the zone-level one. It also
// pins the structure behind that verdict, so a refresh of the data has to keep
// both halves true.
func TestThreeAZVariantsServeHighAvailability(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	// The products OVHcloud makes highly available across zones only in its 3-AZ
	// regions, and the variant the snapshot lists for them there. For the databases
	// the tier that reaches zone level is the multi-node plan; for Block Storage it
	// is the Regional Classic volume class. The databases carry the tier before the
	// 3-AZ suffix so that the 1-AZ multi-node offering, which is a prefix of them,
	// wins a tie by name.
	variants := []struct {
		category taxonomy.Category
		name     string
		// secondZoneLevelTier says the product reaches zone level in more than one
		// tier: the databases offer it on the Business multi-node plan and on the
		// Advanced one. The Advanced plan's name sorts behind the Business plan's,
		// which stays the best candidate, but it is a Direct candidate too, and it
		// must still be 3-AZ only.
		secondZoneLevelTier bool
	}{
		{category: taxonomy.DatabaseMySQL, name: "Managed MySQL Multi-node 3-AZ", secondZoneLevelTier: true},
		{category: taxonomy.DatabasePostgreSQL, name: "Managed PostgreSQL Multi-node 3-AZ", secondZoneLevelTier: true},
		{category: taxonomy.KubernetesManaged, name: "Managed Kubernetes Service 3-AZ"},
		{category: taxonomy.StorageBlock, name: "Block Storage 3-AZ Regional Classic"},
		{category: taxonomy.StorageObject, name: "Object Storage 3-AZ"},
	}

	for _, tc := range variants {
		t.Run(string(tc.category), func(t *testing.T) {
			t.Parallel()

			o := offeringNamed(t, cat, tc.name)

			if !o.Has(taxonomy.ZoneHighAvailability) {
				t.Errorf("offering %s does not offer %s, want the 3-AZ variant to",
					text.Quote(o.Name), text.Quote(string(taxonomy.ZoneHighAvailability)))
			}

			if !slices.Contains(o.Categories, tc.category) {
				t.Errorf("offering %s categories = %v, want them to include %s",
					text.Quote(o.Name), o.Categories, text.Quote(string(tc.category)))
			}

			for _, region := range o.Regions {
				if !slices.Contains(threeAZRegions(), region) {
					t.Errorf("offering %s is available in region %s, want only the 3-AZ regions %v",
						text.Quote(o.Name), text.Quote(region), threeAZRegions())
				}
			}

			if len(o.Regions) == 0 {
				t.Errorf("offering %s lists no region, want the 3-AZ regions %v", text.Quote(o.Name), threeAZRegions())
			}

			r := newResource(t, "res-example", tc.category, taxonomy.ZoneHighAvailability)

			got := mapping.Map(r, cat)
			if got.Fit != mapping.Direct {
				t.Errorf("Map() fit = %q, want %q for a resource needing only %s",
					got.Fit, mapping.Direct, text.Quote(string(taxonomy.ZoneHighAvailability)))
			}

			best, ok := got.Best()
			if !ok {
				t.Fatalf("Map() returned no candidate, want offering %s to be the best one", text.Quote(tc.name))
			}

			if best.Offering.Name != tc.name {
				t.Errorf("Map() best offering = %s, want %s: a resource needing %s must be pointed at the 3-AZ variant",
					text.Quote(best.Offering.Name), text.Quote(tc.name), text.Quote(string(taxonomy.ZoneHighAvailability)))
			}

			for _, c := range got.Candidates[min(1, len(got.Candidates)):] {
				if c.Fit != mapping.Direct {
					continue
				}

				if !tc.secondZoneLevelTier {
					t.Errorf("Map() also fits %s directly, want %s to be the only direct candidate: no 1-AZ offering meets %s",
						text.Quote(c.Offering.Name), text.Quote(tc.name), text.Quote(string(taxonomy.ZoneHighAvailability)))

					continue
				}

				for _, region := range c.Offering.Regions {
					if !slices.Contains(threeAZRegions(), region) {
						t.Errorf("Map() also fits %s directly, and it is available in region %s, want every direct candidate beside %s to be 3-AZ only, in %v",
							text.Quote(c.Offering.Name), text.Quote(region), text.Quote(tc.name), threeAZRegions())
					}
				}
			}
		})
	}
}

// TestDatabaseMultiNodeVariantsServeHighAvailability pins ADR 0011's tier rule for
// OVHcloud's databases. A multi-node plan adds high availability, so the snapshot
// lists a Multi-node variant beside each base offering, at node level in the 1-AZ
// regions and at zone level in the 3-AZ ones. The base offering is the Discovery
// plan, one node, which is highly available at neither level. ADR 0011 splits the
// multi-node plan further by tier: the Business plan (`Multi-node`) and the
// Advanced one (`Multi-node Advanced`) both survive the loss of a node, and both
// have a 3-AZ variant that survives the loss of a zone. The 3-AZ variants carry
// the tier before the 3-AZ suffix, so a resource that needs only node-level high
// availability is pointed at the 1-AZ multi-node plan, which exists in every
// region, rather than at a 3-AZ one that exists only in MIL and PAR. For a
// resource that needs zone level, the Business plan's 3-AZ variant wins the tie
// against the Advanced one by name.
//
// It replaces TestDatabaseVariantsBothServeHighAvailability, whose name no longer
// fits: the two variants no longer both serve high availability at the level the
// resource may need, so exactly one of them is the answer.
func TestDatabaseMultiNodeVariantsServeHighAvailability(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	tests := []struct {
		name       string
		category   taxonomy.Category
		capability taxonomy.Capability
		want       []candidateFit
	}{
		{
			name:       "postgresql/node-high-availability",
			category:   taxonomy.DatabasePostgreSQL,
			capability: taxonomy.NodeHighAvailability,
			want: []candidateFit{
				{Name: "Managed PostgreSQL Multi-node", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL Multi-node 3-AZ", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL Multi-node Advanced", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL Multi-node Advanced 3-AZ", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL", Fit: mapping.Partial},
			},
		},
		{
			name:       "postgresql/zone-high-availability",
			category:   taxonomy.DatabasePostgreSQL,
			capability: taxonomy.ZoneHighAvailability,
			want: []candidateFit{
				{Name: "Managed PostgreSQL Multi-node 3-AZ", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL Multi-node Advanced 3-AZ", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL", Fit: mapping.Partial},
				{Name: "Managed PostgreSQL Multi-node", Fit: mapping.Partial},
				{Name: "Managed PostgreSQL Multi-node Advanced", Fit: mapping.Partial},
			},
		},
		{
			name:       "mysql/node-high-availability",
			category:   taxonomy.DatabaseMySQL,
			capability: taxonomy.NodeHighAvailability,
			want: []candidateFit{
				{Name: "Managed MySQL Multi-node", Fit: mapping.Direct},
				{Name: "Managed MySQL Multi-node 3-AZ", Fit: mapping.Direct},
				{Name: "Managed MySQL Multi-node Advanced", Fit: mapping.Direct},
				{Name: "Managed MySQL Multi-node Advanced 3-AZ", Fit: mapping.Direct},
				{Name: "Managed MySQL", Fit: mapping.Partial},
			},
		},
		{
			name:       "mysql/zone-high-availability",
			category:   taxonomy.DatabaseMySQL,
			capability: taxonomy.ZoneHighAvailability,
			want: []candidateFit{
				{Name: "Managed MySQL Multi-node 3-AZ", Fit: mapping.Direct},
				{Name: "Managed MySQL Multi-node Advanced 3-AZ", Fit: mapping.Direct},
				{Name: "Managed MySQL", Fit: mapping.Partial},
				{Name: "Managed MySQL Multi-node", Fit: mapping.Partial},
				{Name: "Managed MySQL Multi-node Advanced", Fit: mapping.Partial},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newResource(t, "res-example", tc.category, tc.capability)

			got := mapping.Map(r, cat)
			if got.Fit != mapping.Direct {
				t.Errorf("Map() fit = %q, want %q: the Multi-node 3-AZ variant offers %s",
					got.Fit, mapping.Direct, text.Quote(string(tc.capability)))
			}

			if diff := cmp.Diff(tc.want, fits(got.Candidates)); diff != "" {
				t.Errorf("Map() candidates mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestOnlyAdvancedDatabaseVariantsServeReadReplicas pins ADR 0011's target rule
// that a capability is listed only where the documentation states it in full.
// OVHcloud's PostgreSQL and MySQL product pages name read replicas on the Advanced
// plan only, not on the Business one, so the snapshot lists read-replicas on
// `Multi-node Advanced` and on `Multi-node Advanced 3-AZ` alone. A resource that
// needs read replicas is therefore pointed at the 1-AZ Advanced plan, which exists
// in every region, while the Business plan's 3-AZ variant is only Partial.
func TestOnlyAdvancedDatabaseVariantsServeReadReplicas(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	tests := []struct {
		name                string
		category            taxonomy.Category
		withoutReadReplicas []string
		want                []candidateFit
	}{
		{
			name:     "postgresql",
			category: taxonomy.DatabasePostgreSQL,
			withoutReadReplicas: []string{
				"Managed PostgreSQL Multi-node",
				"Managed PostgreSQL Multi-node 3-AZ",
			},
			want: []candidateFit{
				{Name: "Managed PostgreSQL Multi-node Advanced", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL Multi-node Advanced 3-AZ", Fit: mapping.Direct},
				{Name: "Managed PostgreSQL", Fit: mapping.Partial},
				{Name: "Managed PostgreSQL Multi-node", Fit: mapping.Partial},
				{Name: "Managed PostgreSQL Multi-node 3-AZ", Fit: mapping.Partial},
			},
		},
		{
			name:     "mysql",
			category: taxonomy.DatabaseMySQL,
			withoutReadReplicas: []string{
				"Managed MySQL Multi-node",
				"Managed MySQL Multi-node 3-AZ",
			},
			want: []candidateFit{
				{Name: "Managed MySQL Multi-node Advanced", Fit: mapping.Direct},
				{Name: "Managed MySQL Multi-node Advanced 3-AZ", Fit: mapping.Direct},
				{Name: "Managed MySQL", Fit: mapping.Partial},
				{Name: "Managed MySQL Multi-node", Fit: mapping.Partial},
				{Name: "Managed MySQL Multi-node 3-AZ", Fit: mapping.Partial},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, name := range tc.withoutReadReplicas {
				o := offeringNamed(t, cat, name)
				if o.Has(taxonomy.ReadReplicas) {
					t.Errorf("offering %s lists %s, want only the Advanced multi-node plan to list it: OVHcloud states it on the Advanced plan alone",
						text.Quote(o.Name), text.Quote(string(taxonomy.ReadReplicas)))
				}
			}

			r := newResource(t, "res-example", tc.category, taxonomy.ReadReplicas)

			got := mapping.Map(r, cat)
			if got.Fit != mapping.Direct {
				t.Errorf("Map() fit = %q, want %q: the Advanced multi-node plan offers %s",
					got.Fit, mapping.Direct, text.Quote(string(taxonomy.ReadReplicas)))
			}

			if diff := cmp.Diff(tc.want, fits(got.Candidates)); diff != "" {
				t.Errorf("Map() candidates mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestKubernetesTierVariantIsBestOffering pins ADR 0011's tier rule for Managed
// Kubernetes Service: high availability exists only in the 3-AZ regions, where
// every cluster runs on the Standard plan, so the zone-level variant is the answer
// for a cluster that needs it. A cluster that needs nothing the tier adds is
// pointed at the base offering, with the variant tied behind it by name.
func TestKubernetesTierVariantIsBestOffering(t *testing.T) {
	t.Parallel()

	cat := catalogOf(t)

	tests := []struct {
		name         string
		capabilities []taxonomy.Capability
		want         []candidateFit
	}{
		{
			name:         "zone-high-availability",
			capabilities: []taxonomy.Capability{taxonomy.ZoneHighAvailability},
			want: []candidateFit{
				{Name: "Managed Kubernetes Service 3-AZ", Fit: mapping.Direct},
				{Name: "Managed Kubernetes Service", Fit: mapping.Partial},
			},
		},
		{
			name: "no capabilities",
			want: []candidateFit{
				{Name: "Managed Kubernetes Service", Fit: mapping.Direct},
				{Name: "Managed Kubernetes Service 3-AZ", Fit: mapping.Direct},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newResource(t, "res-example", taxonomy.KubernetesManaged, tc.capabilities...)

			got := mapping.Map(r, cat)
			if got.Fit != mapping.Direct {
				t.Errorf("Map() fit = %q, want %q", got.Fit, mapping.Direct)
			}

			if diff := cmp.Diff(tc.want, fits(got.Candidates)); diff != "" {
				t.Errorf("Map() candidates mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
