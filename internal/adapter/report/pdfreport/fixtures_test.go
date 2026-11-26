package pdfreport_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The reporter must be usable wherever the use cases expect the two reporter
// ports: the golden files pin its output, these pin the shape of what it
// renders.
var (
	_ analysis.Reporter           = pdfreport.New(versionLine)
	_ analysis.ComparisonReporter = pdfreport.New(versionLine)
)

const (
	// versionLine is what the composition roots pass New: the line shift2eu
	// version prints (internal/version.String). The PDF shows it in every page
	// footer and in About this report, never from a clock or a build stamp of
	// its own.
	versionLine = "shift2eu 0.15.0 (commit 1a2b3c4d5e6f, built 2026-11-25T00:00:00Z)"
	// otherVersionLine is a second version line, so a test can see that the
	// line the PDF shows is the one the constructor took.
	otherVersionLine = "shift2eu 9.9.9 (commit 000000000000, built 2020-01-01T00:00:00Z)"

	// inventoryFileName is the base name of the fixture inventory file.
	inventoryFileName = "inventory.json"
	// sourceName is the source the ordinary fixture inventories were taken from.
	sourceName = "aws"

	// reportTarget, reportSourceURL and reportRetrievedOn are testutil.ReportCatalog's
	// target, its snapshot's source URL and its retrieval date as a UTC calendar
	// date: invented values, so a test can state each of them exactly.
	reportTarget      = "Example Cloud"
	reportSourceURL   = "https://www.example.com/pricing/"
	reportRetrievedOn = "2026-11-13"

	// The keys the comparison fixtures select their targets with: the CLI's keys,
	// lower case and one word, not the catalogs' display names.
	ovhcloudKey = "ovhcloud"
	scalewayKey = "scaleway"
	upcloudKey  = "upcloud"

	// longResourcesCount is how many resources the long-report fixture holds: far
	// more than one A4 page of the resources table holds, so the table breaks
	// inside itself and has to repeat its header row.
	longResourcesCount = 120

	// hostileTarget and hostileOtherTarget are catalog display names full of the
	// PDF's own syntax: unbalanced parentheses, a backslash, an operator that
	// would close a text run, a stream end and a name.
	hostileTarget      = "Evil ) Tj ET (x"
	hostileOtherTarget = "Evil /JavaScript \\ obj"
	// hostileSource is an inventory source full of the same syntax.
	hostileSource = "aws) Tj ET (x"
	// hostileInventoryName is an inventory file name that spells PDF syntax.
	hostileInventoryName = "inv(1) ) Tj ET endstream.pdf"
	// hostileSourceURL is a catalog source URL with the metacharacters the
	// hardened domain still accepts: parentheses, a percent escape that decodes
	// to ordinary text, and a query string. It is shown as text, never as a link.
	hostileSourceURL = "https://example.com/a(b)*c*_d_~e~f?x=1&y=2%60%5B"
	// hostileRegion is a target region that spells PDF syntax.
	hostileRegion = "eu(west)-1"
)

// sized is a given float64, as an inventory size and a monthly cost carry it.
func sized(v float64) *float64 { return &v }

// newInventory builds the only kind of inventory the domain lets through.
func newInventory(tb testing.TB, source string, resources []inventory.Resource) inventory.Inventory {
	tb.Helper()

	inv, err := inventory.New(source, resources)
	if err != nil {
		tb.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// renderReport writes the PDF report of a and meta and returns its bytes.
func renderReport(tb testing.TB, a assessment.Assessment, meta analysis.ReportMeta) []byte {
	tb.Helper()

	var out bytes.Buffer
	if err := pdfreport.New(versionLine).Report(tb.Context(), &out, a, meta); err != nil {
		tb.Fatalf("Report() returned unexpected error: %v", err)
	}

	return out.Bytes()
}

// renderReportWithVersion writes the report with the version line given.
func renderReportWithVersion(tb testing.TB, a assessment.Assessment, meta analysis.ReportMeta, line string) []byte {
	tb.Helper()

	var out bytes.Buffer
	if err := pdfreport.New(line).Report(tb.Context(), &out, a, meta); err != nil {
		tb.Fatalf("Report() returned unexpected error: %v", err)
	}

	return out.Bytes()
}

// renderComparison writes the PDF comparison report of c and meta and returns
// its bytes.
func renderComparison(tb testing.TB, c assessment.Comparison, meta analysis.ReportMeta) []byte {
	tb.Helper()

	var out bytes.Buffer
	if err := pdfreport.New(versionLine).ReportComparison(tb.Context(), &out, c, meta); err != nil {
		tb.Fatalf("ReportComparison() returned unexpected error: %v", err)
	}

	return out.Bytes()
}

// estimateResources is the inventory the estimate fixtures assess, the one
// inventory a report has to tell every estimate state apart in:
//
//   - bucket-storage is storage.object, priced by the stored GiB;
//   - cluster-small is kubernetes.managed with a plan and a fixed fee;
//   - server-small and worker-jobs are compute.vm on the smallest plan;
//   - server-huge is compute.vm larger than the largest plan, so it has no plan
//     large enough;
//   - legacy-nosize is compute.vm without a size, so its size is unknown;
//   - function-orders is serverless.function, which is priced by usage;
//   - queue-jobs is messaging.queue, which no offering serves, so it is blocked
//     and has no estimate, and worker-jobs depends on it and is a replica of
//     server-small, so it carries relations and an "affected by" note.
//
// Three workloads are named, one of them without a source cost for all of its
// resources, and the resources without a workload are the "No workload" group.
// The monthly costs are in two currencies, so no report converts one into the
// other.
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
			ID:       "function-orders",
			RawType:  "AWS::Lambda::Function",
			Category: taxonomy.ServerlessFunction,
			Region:   "eu-west-1",
			Workload: "orders",
		},
		{
			ID:           "legacy-nosize",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Workload:     "orders",
			MonthlyCost:  &inventory.Cost{Amount: sized(1200), Currency: "USD"},
		},
		{
			ID:       "queue-jobs",
			RawType:  "AWS::SQS::Queue",
			Category: taxonomy.MessagingQueue,
			Region:   "eu-west-1",
			Workload: "orders",
		},
		{
			ID:           "server-huge",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Size:         &inventory.Size{VCPUs: sized(16), MemoryGiB: sized(64)},
			MonthlyCost:  &inventory.Cost{Amount: sized(200), Currency: "USD"},
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
			ID:           "worker-jobs",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
			Workload:     "orders",
			Relations: []inventory.Relation{
				{Type: inventory.DependsOn, Target: "queue-jobs"},
				{Type: inventory.ReplicaOf, Target: "server-small"},
			},
		},
	}
}

// estimateAssessment assesses the estimate fixture against the report fixture
// catalog, with estimates on or off, in the region given (empty for the
// catalog's default for estimates).
func estimateAssessment(tb testing.TB, region string, estimate bool) assessment.Assessment {
	tb.Helper()

	return assessment.AssessWith(newInventory(tb, sourceName, estimateResources()),
		testutil.ReportCatalog(tb), assessment.Options{Region: region, Estimate: estimate})
}

// workloadResources is the inventory the workloads fixture assesses: two named
// workloads plus the resources that name none, so the report has a Workloads
// section with a "No workload" group.
func workloadResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:       "orders-1",
			RawType:  "AWS::EC2::Instance",
			Category: taxonomy.ComputeVM,
			Region:   "eu-west-1",
			Workload: "orders",
		},
		{
			ID:       "orders-2",
			RawType:  "AWS::SQS::Queue",
			Category: taxonomy.MessagingQueue,
			Region:   "eu-west-1",
			Workload: "orders",
		},
		{
			ID:       "shop-1",
			RawType:  "AWS::EC2::Instance",
			Category: taxonomy.ComputeVM,
			Region:   "eu-west-1",
			Workload: "shop",
		},
		{
			ID:       "spare-1",
			RawType:  "AWS::CloudFormation::CustomResource",
			Category: taxonomy.Unclassified,
			Region:   "eu-west-1",
		},
	}
}

// longResources is the inventory of the long report: resources of one compute
// category with IDs that sort into the drawing order, enough of them to break
// the resources table across several pages.
func longResources() []inventory.Resource {
	resources := make([]inventory.Resource, 0, longResourcesCount)

	for i := range longResourcesCount {
		resources = append(resources, inventory.Resource{
			ID:           fmt.Sprintf("instance-%c-%04d", 'a'+i%26, i),
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
		})
	}

	return resources
}

// hostileResources is an inventory whose every text field spells PDF syntax:
// resource IDs, raw types, regions and workloads with unbalanced parentheses, a
// backslash, an operator that would close a text run, a stream end, an end of
// file marker, a name, a dictionary and an object keyword, a non-Latin letter,
// a character above U+FFFF, the WinAnsi look-alikes, a no-break space, a soft
// hyphen and a value of text.MaxLen bytes of two-byte runes (the wrap and row
// split case of the PRD's Tables and page breaks).
func hostileResources() []inventory.Resource {
	return []inventory.Resource{
		{
			ID:           "id(1)",
			RawType:      "endstream",
			Category:     taxonomy.ComputeVM,
			Region:       hostileRegion,
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Workload:     "ws) Tj ET",
		},
		{
			ID:           "id) Tj ET",
			RawType:      "%%EOF",
			Category:     taxonomy.StorageObject,
			Region:       hostileRegion,
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		},
		{
			ID:           `id\path`,
			RawType:      "/JavaScript",
			Category:     taxonomy.ComputeVM,
			Region:       hostileRegion,
			Capabilities: []taxonomy.Capability{taxonomy.Snapshots},
		},
		{
			ID:       "obj << >>",
			RawType:  "trailer",
			Category: taxonomy.Unclassified,
			Region:   hostileRegion,
		},
		{
			ID:           "ЖЖ ж",
			RawType:      "é€‘’“”",
			Category:     taxonomy.ComputeVM,
			Region:       hostileRegion,
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		},
		{
			ID:       "😀 sm:)e",
			RawType:  "x\u00a0y",
			Category: taxonomy.StorageObject,
			Region:   hostileRegion,
		},
		{
			ID:           "id((x))",
			RawType:      "(( )) )) (",
			Category:     taxonomy.ComputeVM,
			Region:       hostileRegion,
			Capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability},
		},
		{
			ID:       strings.Repeat("Ж", textMaxLen/2),
			RawType:  "a(b",
			Category: taxonomy.StorageObject,
			Region:   hostileRegion,
		},
	}
}

// textMaxLen is text.MaxLen, spelled out so the fixtures state the byte count
// the PRD's Tables and page breaks names.
const textMaxLen = 2048

// hostileValues returns every value the hostile single-target report puts in a
// field the PDF shows, so a test can require each of them to be shown as its
// escaped form: the target, the source, the inventory file name, the snapshot's
// source URL, the region, every offering name, and every resource field.
func hostileValues() []string {
	return []string{
		hostileTarget,
		hostileSource,
		hostileInventoryName,
		hostileSourceURL,
		hostileRegion,
		"endstream",
		"%%EOF",
		"a(b)c \\ obj",
		"id(1)",
		"ws) Tj ET",
		"id) Tj ET",
		`id\path`,
		"/JavaScript",
		"obj << >>",
		"trailer",
		"ЖЖ ж",
		"é€‘’“”",
		"😀 sm:)e",
		"x\u00a0y",
		"id((x))",
		"(( )) )) (",
		strings.Repeat("Ж", textMaxLen/2),
		"a(b",
	}
}

// hostileComparisonValues is every value the hostile comparison shows: the
// single-target report's values in each target's section, plus the second
// target's display name.
func hostileComparisonValues() []string {
	return append(hostileValues(), hostileOtherTarget)
}

// hostileCatalog returns a catalog of the target given whose offerings, regions
// and snapshot all spell PDF syntax. Its retrieval date is just after midnight
// in a zone east of UTC, so a report that forgets to format it in UTC shows the
// day after.
func hostileCatalog(tb testing.TB, target string) catalog.Catalog {
	tb.Helper()

	cat, err := catalog.New(target, catalog.Snapshot{
		SourceURL:     hostileSourceURL,
		RetrievedOn:   time.Date(2026, 11, 2, 0, 30, 0, 0, time.FixedZone("CET", 60*60)),
		DefaultRegion: hostileRegion,
	}, []catalog.Offering{
		{
			Name:         "endstream",
			Categories:   []taxonomy.Category{taxonomy.ComputeVM},
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Regions:      []string{hostileRegion},
		},
		{
			Name:         "%%EOF",
			Categories:   []taxonomy.Category{taxonomy.StorageObject},
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			Regions:      []string{hostileRegion},
		},
		{
			Name:       "a(b)c \\ obj",
			Categories: []taxonomy.Category{taxonomy.ComputeVM},
			Regions:    []string{hostileRegion},
		},
	})
	if err != nil {
		tb.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

// comparisonResources is the inventory the three-target comparison golden
// assesses: a direct fit, a partial fit on one target only, and resources
// blocked on every target, so the comparison has shared blockers to report.
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
			ID:       "misc-example",
			RawType:  "AWS::CloudFormation::CustomResource",
			Category: taxonomy.Unclassified,
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

// twoTargetResources is the inventory the two-target comparison golden assesses:
// OVHcloud and Scaleway serve everything in it, so the comparison has no blocker
// on every target and its shared-blocker section says "None.".
func twoTargetResources() []inventory.Resource {
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
			ID:           "partial-example",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.Snapshots},
		},
	}
}

// comparisonFixtureCatalog returns the fixture catalog of one target key.
func comparisonFixtureCatalog(tb testing.TB, key string) catalog.Catalog {
	tb.Helper()

	switch key {
	case ovhcloudKey:
		return testutil.OVHcloudCatalog(tb)
	case scalewayKey:
		return testutil.ScalewayCatalog(tb)
	case upcloudKey:
		return testutil.UpCloudCatalog(tb)
	default:
		tb.Fatalf("no fixture catalog for target key %q", key)

		return catalog.Catalog{}
	}
}

// assessTargets assesses inv against the fixture catalog of each key.
func assessTargets(tb testing.TB, inv inventory.Inventory, keys []string) []assessment.TargetAssessment {
	tb.Helper()

	targets := make([]assessment.TargetAssessment, 0, len(keys))
	for _, key := range keys {
		targets = append(targets, assessment.TargetAssessment{
			Key:        key,
			Assessment: assessment.Assess(inv, comparisonFixtureCatalog(tb, key)),
		})
	}

	return targets
}

// threeTargetKeys is the target list the three-target comparison golden compares.
func threeTargetKeys() []string { return []string{ovhcloudKey, scalewayKey, upcloudKey} }

// hostileComparisonTargets is the comparison of the hostile inventory against two
// hostile catalogs, whose keys spell PDF syntax too, as the other reporters'
// hostile comparison fixtures do.
func hostileComparisonTargets(tb testing.TB) []assessment.TargetAssessment {
	tb.Helper()

	inv := newInventory(tb, hostileSource, hostileResources())

	return []assessment.TargetAssessment{
		{Key: "ovh(cloud", Assessment: assessment.Assess(inv, hostileCatalog(tb, hostileTarget))},
		{Key: "scaleway) Tj ET", Assessment: assessment.Assess(inv, hostileCatalog(tb, hostileOtherTarget))},
	}
}
