package htmlreport_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/timothymamo/shift2eu/internal/adapter/report/htmlreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The reporter must be usable wherever the use cases expect a Reporter and a
// ComparisonReporter: the golden files pin its output, this pins the shape of
// the two ports it renders (docs/adr/0016).
var (
	_ analysis.Reporter           = htmlreport.New()
	_ analysis.ComparisonReporter = htmlreport.New()
)

const (
	// inventoryFileName is what a report shows above the summary: the base name
	// of the file the inventory was read from, as the CLI's --inventory names it.
	inventoryFileName = "inventory.json"
	// sourceName is the source the ordinary fixture inventories were taken from.
	sourceName = "aws"
	// hostileInventoryName is a file name the report must show as text.
	hostileInventoryName = `inv "report" <script>alert('name')</script>.json`
)

// sized is a given float64, as an inventory size carries it.
func sized(v float64) *float64 { return &v }

// newInventory builds the inventory of source from resources, failing the test
// when the domain refuses them.
func newInventory(tb testing.TB, source string, resources []inventory.Resource) inventory.Inventory {
	tb.Helper()

	inv, err := inventory.New(source, resources)
	if err != nil {
		tb.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// awsResources is one inventory covering every fit: direct, partial, none for a
// category no offering serves, and unclassified. The IDs sort into the order the
// report's table and blockers section must use.
func awsResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:           "bucket-example",
			RawType:      "AWS::S3::Bucket",
			Category:     taxonomy.StorageObject,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6, taxonomy.ObjectVersioning},
		},
		{
			ID:           "db-example",
			RawType:      "AWS::RDS::DBInstance",
			Category:     taxonomy.DatabasePostgreSQL,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.PointInTimeRestore, taxonomy.ReadReplicas},
		},
		{
			ID:       "fn-example",
			RawType:  "AWS::Lambda::Function",
			Category: taxonomy.ServerlessFunction,
			Region:   "eu-west-1",
		},
		{
			// Two missing capabilities, so the required-changes list and the
			// reason cell show more than one.
			ID:           "partial-example",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.Snapshots},
		},
		{
			ID:       "misc-example",
			RawType:  "AWS::CloudFormation::CustomResource",
			Category: taxonomy.Unclassified,
			Region:   "eu-west-1",
		},
	}
}

// estimateResources is a sized inventory the report fixture catalog can price,
// so an assessment with estimates on has plans and amounts to show.
func estimateResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:           "bucket-storage",
			RawType:      "AWS::S3::Bucket",
			Category:     taxonomy.StorageObject,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Size:         &inventory.Size{StorageGiB: sized(50000)},
			Workload:     "shop",
			MonthlyCost:  &inventory.Cost{Amount: sized(2400), Currency: "EUR"},
		},
		{
			ID:           "cluster-small",
			RawType:      "Scaleway::Kubernetes::Cluster",
			Category:     taxonomy.KubernetesManaged,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.Autoscaling},
			Size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
			Workload:     "shop",
			MonthlyCost:  &inventory.Cost{Amount: sized(40), Currency: "USD"},
		},
		{
			ID:       "queue-jobs",
			RawType:  "AWS::SQS::Queue",
			Category: taxonomy.MessagingQueue,
			Region:   "eu-west-1",
			Workload: "orders",
		},
		{
			ID:           "server-small",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
			Workload:     "shop",
			MonthlyCost:  &inventory.Cost{Amount: sized(15), Currency: "EUR"},
		},
		{
			ID:       "worker-jobs",
			RawType:  "AWS::EC2::Instance",
			Category: taxonomy.ComputeVM,
			Region:   "eu-west-1",
			Capabilities: []taxonomy.Capability{
				taxonomy.IPv6,
			},
			Size:     &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
			Workload: "orders",
			Relations: []inventory.Relation{
				{Type: inventory.DependsOn, Target: "queue-jobs"},
			},
		},
	}
}

// comparisonResources is the inventory the comparison golden assesses: a bucket
// every target serves, a PostgreSQL database UpCloud does not, an unclassified
// resource no target serves, and a partial example.
func comparisonResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:           "bucket-example",
			RawType:      "AWS::S3::Bucket",
			Category:     taxonomy.StorageObject,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.ObjectVersioning},
		},
		{
			ID:           "db-example",
			RawType:      "AWS::RDS::DBInstance",
			Category:     taxonomy.DatabasePostgreSQL,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.PointInTimeRestore, taxonomy.ReadReplicas},
		},
		{
			ID:       "fn-example",
			RawType:  "AWS::Lambda::Function",
			Category: taxonomy.ServerlessFunction,
			Region:   "eu-west-1",
		},
		{
			ID:           "partial-example",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.Snapshots},
		},
	}
}

// hostileResources is an inventory whose text values would be markup in a page
// if the reporter wrote them unescaped. They are valid free text, so the domain
// takes them as they are and the reporter is what must escape them.
//
// The last resource is named after the page's module global and the glue's
// function (PRD #297, Layer 2): a report shows those names as text and writes no
// id or name attribute with them, so no report element can shadow the global or
// a name the glue reads.
func hostileResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:       `<script>alert(1)</script>`,
			RawType:  `<img src=x onerror=alert(1)>`,
			Category: taxonomy.StorageObject,
			Region:   `eu"west`,
			Workload: `ws <script>alert('workload')</script>`,
		},
		{
			ID:       `onerror=alert(1) x=`,
			RawType:  `" onmouseover="alert(1)`,
			Category: taxonomy.Unclassified,
			Region:   `eu-west-1`,
		},
		{
			ID:       `a"b'c&d`,
			RawType:  `javascript:alert(1)`,
			Category: taxonomy.ComputeVM,
			Region:   `eu-west-1`,
			Capabilities: []taxonomy.Capability{
				taxonomy.Snapshots,
			},
		},
		{
			ID:       `__shift2euModule`,
			RawType:  `AWS::EC2::Instance`,
			Category: taxonomy.ComputeVM,
			Region:   `eu-west-1`,
			Workload: `shift2euAssess`,
		},
	}
}

// hostileComparisonResources is the inventory the hostile comparison golden
// assesses: the values of hostileResources, this time sized and grouped by
// workload, so the comparison's estimates and workload sections carry hostile
// plan, offering and workload names too, and one relation, so a blocker on one
// resource reaches another.
func hostileComparisonResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:          `<script>alert(1)</script>`,
			RawType:     `<img src=x onerror=alert(1)>`,
			Category:    taxonomy.StorageObject,
			Region:      `eu"west`,
			Size:        &inventory.Size{StorageGiB: sized(500)},
			Workload:    `ws <script>alert('workload')</script>`,
			MonthlyCost: &inventory.Cost{Amount: sized(12), Currency: "EUR"},
		},
		{
			ID:       `onerror=alert(1) x=`,
			RawType:  `" onmouseover="alert(1)`,
			Category: taxonomy.Unclassified,
			Region:   `eu-west-1`,
			Workload: `shift2euAssess`,
		},
		{
			ID:           `a"b'c&d`,
			RawType:      `javascript:alert(1)`,
			Category:     taxonomy.ComputeVM,
			Region:       `eu-west-1`,
			Capabilities: []taxonomy.Capability{taxonomy.Snapshots},
			Size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
			Workload:     `orders<script>alert('orders')</script>`,
			Relations: []inventory.Relation{
				{Type: inventory.DependsOn, Target: `onerror=alert(1) x=`},
			},
		},
		{
			// A capability the hostile catalogs lack, so the resource is partial
			// and its required change names a hostile offering.
			ID:           `__shift2euModule`,
			RawType:      `AWS::EC2::Instance`,
			Category:     taxonomy.ComputeVM,
			Region:       `eu-west-1`,
			Capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.Snapshots},
			Size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
			Workload:     `shift2euAssess`,
		},
	}
}

// catalogFor builds a catalog through catalog.New, failing the test when the
// domain refuses it.
func catalogFor(tb testing.TB, target string, snapshot catalog.Snapshot, offerings []catalog.Offering) catalog.Catalog {
	tb.Helper()

	cat, err := catalog.New(target, snapshot, offerings)
	if err != nil {
		tb.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

// hostileCatalog is a catalog whose target and offering names carry the same
// characters as hostileResources.
func hostileCatalog(tb testing.TB) catalog.Catalog {
	tb.Helper()

	return catalogFor(tb, `Evil "Target" & <b>`, testutil.ScalewaySnapshot(), []catalog.Offering{
		{
			Name:       `Object <script>alert('offering')</script> Storage`,
			Categories: []taxonomy.Category{taxonomy.StorageObject},
			Regions:    []string{"fr-par"},
		},
	})
}

// hostilePricedOfferings is the offerings of one hostile target: storage.object
// priced by the GiB and compute.vm with one plan, both offered in region. Each
// name is target's own hostile name with a hostile name of its part, so two
// targets' offerings and plans stay apart in the golden.
func hostilePricedOfferings(region, target string) []catalog.Offering {
	return []catalog.Offering{
		{
			Name:          target + ` Object <script>alert('offering')</script> Storage`,
			Categories:    []taxonomy.Category{taxonomy.StorageObject},
			Capabilities:  []taxonomy.Capability{taxonomy.IPv6},
			Regions:       []string{region},
			StoragePrices: []catalog.Price{{Amount: 500, Currency: catalog.EUR}},
		},
		{
			Name:         target + ` Compute "Tier" & <i>`,
			Categories:   []taxonomy.Category{taxonomy.ComputeVM},
			Capabilities: []taxonomy.Capability{taxonomy.Snapshots},
			Regions:      []string{region},
			Plans: []catalog.Plan{
				{
					Name:           target + ` Plan <b>Small</b>`,
					VCPUs:          1,
					MemoryMilliGiB: 2000,
					Prices:         []catalog.Price{{Amount: 100000, Currency: catalog.EUR}},
				},
			},
		},
	}
}

// render writes the report of a and meta and returns it.
func render(tb testing.TB, a assessment.Assessment, meta analysis.ReportMeta) []byte {
	tb.Helper()

	var out bytes.Buffer
	if err := htmlreport.New().Report(tb.Context(), &out, a, meta); err != nil {
		tb.Fatalf("Report() returned unexpected error: %v", err)
	}

	return out.Bytes()
}

// renderComparison writes the comparison report of c and meta and returns it.
func renderComparison(tb testing.TB, c assessment.Comparison, meta analysis.ReportMeta) []byte {
	tb.Helper()

	var out bytes.Buffer
	if err := htmlreport.New().ReportComparison(tb.Context(), &out, c, meta); err != nil {
		tb.Fatalf("ReportComparison() returned unexpected error: %v", err)
	}

	return out.Bytes()
}

// TestReport pins the whole fragment against the Scaleway fixture catalog, with
// estimates off and on, and the comparison of three targets against their
// fixture catalogs. The fragment's bytes depend on the assessment and the
// metadata only.
func TestReport(t *testing.T) {
	t.Parallel()

	scaleway := testutil.ScalewayCatalog(t)
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	tests := []struct {
		name       string
		report     func(t *testing.T) []byte
		ids        []string
		goldenFile string
	}{
		{
			name: "every fit, without estimates",
			report: func(t *testing.T) []byte {
				t.Helper()

				return render(t, assessment.Assess(newInventory(t, sourceName, awsResources()), scaleway), meta)
			},
			ids:        resourceIDs(awsResources()),
			goldenFile: "testdata/report.golden",
		},
		{
			name: "with estimates",
			report: func(t *testing.T) []byte {
				t.Helper()

				return render(t, assessment.AssessWith(newInventory(t, sourceName, estimateResources()),
					testutil.ReportCatalog(t), assessment.Options{Estimate: true}), meta)
			},
			ids:        resourceIDs(estimateResources()),
			goldenFile: "testdata/report-estimates.golden",
		},
		{
			name: "a comparison of three targets",
			report: func(t *testing.T) []byte {
				t.Helper()

				inv := newInventory(t, sourceName, comparisonResources())

				return renderComparison(t, assessment.Compare([]assessment.TargetAssessment{
					{Key: "ovhcloud", Assessment: assessment.Assess(inv, testutil.OVHcloudCatalog(t))},
					{Key: "scaleway", Assessment: assessment.Assess(inv, testutil.ScalewayCatalog(t))},
					{Key: "upcloud", Assessment: assessment.Assess(inv, testutil.UpCloudCatalog(t))},
				}), meta)
			},
			ids:        resourceIDs(comparisonResources()),
			goldenFile: "testdata/comparison.golden",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.report(t)

			checkFragment(t, got)
			checkValuesAreEscaped(t, got, tc.ids)

			testutil.Golden(t, tc.goldenFile, got)
		})
	}
}

// TestReportHostileValues pins that every value a report takes from an inventory
// or a catalog reaches the fragment as text: the golden shows the escaping, and
// the assertions below fail when any of the values arrives as markup.
func TestReportHostileValues(t *testing.T) {
	t.Parallel()

	a := assessment.Assess(newInventory(t, hostileSourceName, hostileResources()), hostileCatalog(t))
	got := render(t, a, analysis.ReportMeta{InventoryName: hostileInventoryName})

	checkFragment(t, got)
	checkNoInjectedMarkup(t, got)
	checkNoIDOrNameAttribute(t, got)

	checkValuesShownAsText(t, got, hostileValues())

	testutil.Golden(t, "testdata/report-hostile.golden", got)
}

// TestReportHostileComparison pins that a comparison whose inventory values,
// target names, offering names and plan names would all be markup in a page
// reaches the fragment as text. Estimates are on and the resources are grouped
// by workload, so the comparison's own sections, the shared blockers and each
// target's estimate and workload sections are covered with hostile names too.
func TestReportHostileComparison(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, hostileSourceName, hostileComparisonResources())

	// Two targets, each with offerings named after it, so the golden shows two
	// different hostile names in every comparison section.
	targets := []struct {
		key       string
		target    string
		snapshot  catalog.Snapshot
		offerings []catalog.Offering
	}{
		{
			key:       "ovhcloud",
			target:    `Evil "Target" & <b>`,
			snapshot:  testutil.OVHcloudSnapshot(),
			offerings: hostilePricedOfferings("gra", `Evil "Target" & <b>`),
		},
		{
			key:       "scaleway",
			target:    `<script>Target</script> "Two"`,
			snapshot:  testutil.ScalewaySnapshot(),
			offerings: hostilePricedOfferings("fr-par", `<script>Target</script> "Two"`),
		},
	}

	assessed := make([]assessment.TargetAssessment, 0, len(targets))
	values := make([]string, 0, len(targets)*3)

	for _, target := range targets {
		assessed = append(assessed, assessment.TargetAssessment{
			Key: target.key,
			Assessment: assessment.AssessWith(inv, catalogFor(t, target.target, target.snapshot, target.offerings),
				assessment.Options{Estimate: true}),
		})

		values = append(values, target.target)

		for _, offering := range target.offerings {
			values = append(values, offering.Name)

			for _, plan := range offering.Plans {
				values = append(values, plan.Name)
			}
		}
	}

	got := renderComparison(t, assessment.Compare(assessed), analysis.ReportMeta{InventoryName: hostileInventoryName})

	checkFragment(t, got)
	checkNoInjectedMarkup(t, got)
	checkNoIDOrNameAttribute(t, got)
	checkValuesAreEscaped(t, got, resourceIDs(hostileComparisonResources()))
	checkValuesShownAsText(t, got, append(values, hostileComparisonWorkloads()...))

	testutil.Golden(t, "testdata/comparison-hostile.golden", got)
}

// hostileComparisonWorkloads returns the workloads of the hostile comparison
// inventory, which the golden shows as the headings of its workload sections.
func hostileComparisonWorkloads() []string {
	var workloads []string

	for _, r := range hostileComparisonResources() {
		if r.Workload != "" {
			workloads = append(workloads, r.Workload)
		}
	}

	return workloads
}

// TestReportControlCharacters pins that a value the domain's constructors would
// refuse, but which reaches the reporter anyway, neither breaks the fragment's
// UTF-8 nor injects markup: control characters and invalid UTF-8 come from a
// catalog built by hand, since catalog.New refuses them, and the reporter is
// the last line of defence.
func TestReportControlCharacters(t *testing.T) {
	t.Parallel()

	a := assessment.Assessment{
		Target: "T\x00\x01\x1b[31m",
		Snapshot: catalog.Snapshot{
			SourceURL:     "https://example.com/\xff\xfe<script>",
			RetrievedOn:   time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			DefaultRegion: "xx-par",
		},
		Source: "aws\xff\x00",
		Verdicts: []assessment.Verdict{
			{
				Resource: inventory.Resource{
					ID:       "id\xff\x00",
					RawType:  "AWS::X\x01<script>",
					Category: taxonomy.Unclassified,
					Region:   "eu\x7f-west",
				},
				Fit:     mapping.None,
				Reasons: []mapping.Reason{{Code: mapping.NotClassified}},
			},
		},
		Summary:  assessment.Summary{Total: 1, None: 1},
		Blockers: []assessment.Blocker{{ResourceID: "id\xff\x00", Reason: mapping.Reason{Code: mapping.NotClassified}}},
	}

	got := render(t, a, analysis.ReportMeta{InventoryName: "inv\x00\xff.json"})

	checkFragment(t, got)
	checkNoInjectedMarkup(t, got)

	if !utf8.Valid(got) {
		t.Errorf("the fragment is not valid UTF-8, so a value's invalid bytes reached it:\n%q", got)
	}

	if !bytes.Contains(got, []byte("\uFFFD")) {
		t.Errorf("the fragment holds no U+FFFD, so the invalid UTF-8 of its values was not replaced:\n%s", got)
	}
}

// TestReportRefusesAnInvalidComparison pins that a comparison the Markdown
// reporter refuses is refused here too, with nothing written: the comparison
// port's contract, so a page shows an error rather than half a report.
func TestReportRefusesAnInvalidComparison(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := htmlreport.New().ReportComparison(t.Context(), &out, assessment.Comparison{},
		analysis.ReportMeta{InventoryName: inventoryFileName})
	if err == nil {
		t.Fatal("ReportComparison() of a comparison without a target returned no error, want it refused")
	}

	if out.Len() != 0 {
		t.Errorf("ReportComparison() refused the comparison and wrote %q, want nothing", out.String())
	}
}

// TestReportHonoursACancelledContext pins that a cancelled context returns
// ctx.Err() and writes nothing, for both ports.
func TestReportHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	a := assessment.Assess(newInventory(t, sourceName, awsResources()), testutil.ScalewayCatalog(t))
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	tests := []struct {
		name string
		run  func(ctx context.Context, w *bytes.Buffer) error
	}{
		{
			name: "Report",
			run: func(ctx context.Context, w *bytes.Buffer) error {
				return htmlreport.New().Report(ctx, w, a, meta)
			},
		},
		{
			name: "ReportComparison",
			run: func(ctx context.Context, w *bytes.Buffer) error {
				return htmlreport.New().ReportComparison(ctx, w, assessment.Compare([]assessment.TargetAssessment{
					{Key: "scaleway", Assessment: a},
				}), meta)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			var out bytes.Buffer

			err := tc.run(ctx, &out)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("%s() with a cancelled context returned %v, want context.Canceled", tc.name, err)
			}

			if out.Len() != 0 {
				t.Errorf("%s() with a cancelled context wrote %q, want nothing", tc.name, out.String())
			}
		})
	}
}

// TestIDOrNameAttributeCheck pins the check the hostile tests rely on. A value
// that reaches the fragment as an id or a name attribute must be caught however
// it is written (any case, any quoting or none, an attribute after a newline),
// because a browser reads all of those; the same text outside a tag, which is
// what the reporter's escaping leaves, must pass.
func TestIDOrNameAttributeCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fragment string
		want     []string
	}{
		{
			name:     "no attribute at all",
			fragment: `<p class="wide">x</p><td>y</td>`,
		},
		{
			name:     "the same text outside a tag",
			fragment: `&lt;p id=&#34;x&#34;&gt;`,
		},
		{
			name:     "the same text between tags",
			fragment: `<p>id="x" name='y'</p>`,
		},
		{
			name:     "a data attribute whose name ends in id",
			fragment: `<p data-id="x" data-name="y">z</p>`,
		},
		{
			name:     "double quoted id",
			fragment: `<p id="x">`,
			want:     []string{"id"},
		},
		{
			name:     "single quoted id",
			fragment: `<p id='x'>`,
			want:     []string{"id"},
		},
		{
			name:     "unquoted id",
			fragment: `<p id=x>`,
			want:     []string{"id"},
		},
		{
			name:     "upper case attribute name",
			fragment: `<td ID=x>`,
			want:     []string{"id"},
		},
		{
			name:     "attribute after a newline",
			fragment: "<li\nname=\"y\">",
			want:     []string{"name"},
		},
		{
			name:     "attribute after another one",
			fragment: `<p class="wide" id="x">`,
			want:     []string{"id"},
		},
		{
			name:     "name attribute with spaces around the equals sign",
			fragment: "<input NAME = 'y'>",
			want:     []string{"name"},
		},
		{
			name:     "both attributes, in the order they appear",
			fragment: `<a name="n" href="#x" id="i">x</a>`,
			want:     []string{"name", "id"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := strings.Join(idOrNameAttributes([]byte(tc.fragment)), ",")
			if want := strings.Join(tc.want, ","); got != want {
				t.Errorf("idOrNameAttributes(%q) = %q, want %q", tc.fragment, got, want)
			}
		})
	}
}

// hostileSourceName is the source of the hostile inventory. It is a valid
// source, so the reporter is what keeps it from being read as markup.
const hostileSourceName = "aws"

// hostileValues returns every hostile fixture value a report must show as text.
func hostileValues() []string {
	return []string{
		`__shift2euModule`,
		`shift2euAssess`,
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`eu"west`,
		`ws <script>alert('workload')</script>`,
		`onerror=alert(1) x=`,
		`" onmouseover="alert(1)`,
		`a"b'c&d`,
		`javascript:alert(1)`,
		`Evil "Target" & <b>`,
		`Object <script>alert('offering')</script> Storage`,
		hostileInventoryName,
	}
}

// escapedText returns s as html/template writes it in text and attribute
// content, so a test states the escaped form without a renderer.
func escapedText(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&#34;",
		"'", "&#39;",
	)

	return replacer.Replace(s)
}

// resourceIDs returns the IDs of resources, in the order they are given.
func resourceIDs(resources []inventory.Resource) []string {
	ids := make([]string, 0, len(resources))

	for _, r := range resources {
		ids = append(ids, r.ID)
	}

	return ids
}

// checkValuesAreEscaped fails when a value of the inventory does not reach the
// fragment as text.
func checkValuesAreEscaped(t *testing.T, got []byte, ids []string) {
	t.Helper()

	for _, id := range ids {
		if want := escapedText(id); !strings.Contains(string(got), want) {
			t.Errorf("the fragment does not show the resource ID %q as text (%q):\n%s", id, want, got)
		}
	}
}

// checkNoIDOrNameAttribute fails when the fragment writes an id or a name
// attribute. A report holds none: a value is never written into a tag, so no
// report element can shadow the page's global or a name the glue reads through
// DOM named access (PRD #297, Layer 2). The hostile goldens carry inventories
// whose names equal those globals, so a reporter that started writing one into an
// id would be caught here and in the golden's diff.
func checkNoIDOrNameAttribute(t *testing.T, got []byte) {
	t.Helper()

	for _, attribute := range idOrNameAttributes(got) {
		t.Errorf("the fragment writes the %s attribute, want no id or name attribute at all:\n%s",
			attribute, got)
	}
}

// markupTag matches a tag, so the attribute check looks only inside one and the
// same text between tags, which is what the reporter's escaping leaves, passes.
var markupTag = regexp.MustCompile(`<[^>]*>`)

// idOrNameAttribute matches an id or a name attribute inside a tag, in any case,
// with any quoting or none and whatever separates it from the tag name: a
// browser reads all of those as an attribute, so a value that reached the
// fragment as one is a value that can address the document (PRD #297).
var idOrNameAttribute = regexp.MustCompile(`(?i)\s(id|name)\s*=`)

// idOrNameAttributes returns the names of the id and name attributes the
// fragment's markup holds, without repetition, in the order they first appear.
func idOrNameAttributes(got []byte) []string {
	var found []string

	seen := make(map[string]bool)

	for _, tag := range markupTag.FindAll(got, -1) {
		for _, match := range idOrNameAttribute.FindAllSubmatch(tag, -1) {
			name := strings.ToLower(string(match[1]))
			if !seen[name] {
				seen[name] = true

				found = append(found, name)
			}
		}
	}

	return found
}

// checkValuesShownAsText fails when a value the report was given does not reach
// the fragment as text.
func checkValuesShownAsText(t *testing.T, got []byte, values []string) {
	t.Helper()

	for _, value := range values {
		if want := escapedText(value); !strings.Contains(string(got), want) {
			t.Errorf("the fragment does not show %q as text (%q):\n%s", value, want, got)
		}
	}
}

// checkFragment fails when got is not the semantic HTML fragment the port
// promises: no whole document and nothing that runs, loads or leaves the page,
// and headings, a table with header cells and lists for what a report shows.
func checkFragment(t *testing.T, got []byte) {
	t.Helper()

	if len(got) == 0 {
		t.Fatal("the reporter wrote no fragment")
	}

	if !utf8.Valid(got) {
		t.Errorf("the fragment is not valid UTF-8")
	}

	forbidden := []string{
		"<html",
		"</html",
		"<!doctype",
		"<head",
		"<body",
		"<script",
		"<style",
		"<img",
		"<picture",
		"<form",
		"<iframe",
		"<object",
		"<embed",
		"<svg",
		"<link",
	}

	lower := strings.ToLower(string(got))

	for _, want := range forbidden {
		if strings.Contains(lower, want) {
			t.Errorf("the fragment holds %q, want a fragment that neither runs, loads nor leaves the page:\n%s",
				want, got)
		}
	}

	for _, want := range []string{"<table", "<th", "<ul", "<li"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the fragment holds no %q, want semantic HTML for what the report shows:\n%s", want, got)
		}
	}

	// A report shows a title and its sections as headings at two levels at
	// least, whichever element holds the fragment's top level.
	headings := 0
	levels := 0

	for _, tag := range []string{"<h1", "<h2", "<h3", "<h4", "<h5", "<h6"} {
		if count := strings.Count(lower, tag); count > 0 {
			headings += count
			levels++
		}
	}

	if headings < 3 || levels < 2 {
		t.Errorf("the fragment holds %d headings at %d levels, want the report's title and sections as headings at two levels at least:\n%s",
			headings, levels, got)
	}
}

// knownTags are the elements an HTML reporter may write: the semantic HTML of a
// report, and nothing that runs, loads or leaves the page.
var knownTags = map[string]bool{
	"a": true, "abbr": true, "b": true, "blockquote": true, "br": true, "caption": true,
	"code": true, "col": true, "colgroup": true, "dd": true, "div": true, "dl": true,
	"dt": true, "em": true, "footer": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "header": true, "hr": true, "i": true, "li": true, "mark": true,
	"ol": true, "p": true, "pre": true, "section": true, "small": true, "span": true,
	"strong": true, "sub": true, "sup": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "time": true, "tr": true, "ul": true,
}

// checkNoInjectedMarkup fails when the fragment holds a tag or an attribute no
// reporter writes: an escaped value can carry neither, so one that is not
// escaped shows up here whatever element it tries to open.
func checkNoInjectedMarkup(t *testing.T, got []byte) {
	t.Helper()

	lower := strings.ToLower(string(got))

	for i := 0; i < len(lower); i++ {
		if lower[i] != '<' {
			continue
		}

		rest := lower[i+1:]
		rest = strings.TrimPrefix(rest, "/")

		end := strings.IndexFunc(rest, func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9')
		})
		if end < 0 {
			end = len(rest)
		}

		if name := rest[:end]; !knownTags[name] {
			t.Errorf("the fragment opens the element <%s>, which no report writes, so a value reached it as markup:\n%s",
				name, got)

			return
		}
	}

	for _, want := range []string{"onerror=\"", "onerror='", "onload=\"", "onmouseover=\"", "href=\"javascript:"} {
		if strings.Contains(lower, want) {
			t.Errorf("the fragment holds the injected attribute %q:\n%s", want, got)
		}
	}
}
