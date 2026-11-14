package jsonreport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The JSON reporter renders the same content as the Markdown report, so it must
// be usable wherever the use cases expect a reporter: the goldens pin what it
// writes, this pins the ports it implements. It implements both, because a run
// with several targets writes a comparison document (docs/adr/0016).
var (
	_ analysis.Reporter           = jsonreport.New()
	_ analysis.ComparisonReporter = jsonreport.New()
)

const (
	// inventoryFileName is the base name of the fixture inventory file, and what
	// a document shows as inventoryName.
	inventoryFileName = "inventory.json"
	// sourceName is the source the ordinary fixture inventory was taken from.
	sourceName = "aws"
	// scalewayTarget is the display name of the fixture catalog.
	scalewayTarget = "Scaleway"
	// scalewaySourceURL is the source URL of the fixture catalog's snapshot.
	scalewaySourceURL = "https://www.scaleway.com/en/pricing/"
	// scalewayRetrievedOn is the retrieval date of the fixture catalog's
	// snapshot, as the document prints it.
	scalewayRetrievedOn = "2026-11-01"
)

// awsResources is one inventory covering every fit: direct, partial, none for a
// category no offering serves, and unclassified. It is the inventory of the
// Markdown report's report.golden, so the two documents carry the same content.
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
			ID:           "i-0123456789abcdef0",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		},
		{
			ID:       "misc-example",
			RawType:  "AWS::CloudFormation::CustomResource",
			Category: taxonomy.Unclassified,
			Region:   "eu-west-1",
		},
		{
			// Two missing capabilities, so the resource is partial and lists two
			// required changes.
			ID:           "partial-example",
			RawType:      "AWS::EC2::Instance",
			Category:     taxonomy.ComputeVM,
			Region:       "eu-west-1",
			Capabilities: []taxonomy.Capability{taxonomy.NodeHighAvailability, taxonomy.Snapshots},
		},
	}
}

// newInventory builds the only kind of inventory the domain lets through.
func newInventory(t *testing.T, source string, resources []inventory.Resource) inventory.Inventory {
	t.Helper()

	inv, err := inventory.New(source, resources)
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// render writes the document of a and meta and returns it.
func render(tb testing.TB, a assessment.Assessment, meta analysis.ReportMeta) []byte {
	tb.Helper()

	var out bytes.Buffer
	if err := jsonreport.New().Report(tb.Context(), &out, a, meta); err != nil {
		tb.Fatalf("Report() returned unexpected error: %v", err)
	}

	return out.Bytes()
}

// TestReport pins the whole document against the Scaleway fixture catalog: the
// version and kind, the metadata, the summary, one resource per verdict in the
// assessment's order, and the blockers, with the same content as the Markdown
// report's report.golden.
func TestReport(t *testing.T) {
	t.Parallel()

	scaleway := testutil.ScalewayCatalog(t)

	tests := []struct {
		name       string
		inventory  inventory.Inventory
		meta       analysis.ReportMeta
		goldenFile string
	}{
		{
			name:       "every fit with the inventory file name",
			inventory:  newInventory(t, sourceName, awsResources()),
			meta:       analysis.ReportMeta{InventoryName: inventoryFileName},
			goldenFile: "testdata/report.golden",
		},
		{
			name:       "no inventory file name writes an empty name",
			inventory:  newInventory(t, sourceName, awsResources()),
			meta:       analysis.ReportMeta{},
			goldenFile: "testdata/report-no-inventory-name.golden",
		},
		{
			name:       "empty inventory keeps empty arrays and no blockers",
			inventory:  newInventory(t, sourceName, nil),
			meta:       analysis.ReportMeta{InventoryName: inventoryFileName},
			goldenFile: "testdata/report-empty.golden",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := render(t, assessment.Assess(test.inventory, scaleway), test.meta)
			testutil.Golden(t, test.goldenFile, got)

			checkDocument(t, got)
		})
	}
}

// checkDocument asserts the properties every document of the format has: it is
// valid JSON, it is valid UTF-8, and it ends with exactly one newline.
func checkDocument(t *testing.T, out []byte) {
	t.Helper()

	if !json.Valid(out) {
		t.Errorf("the document is not valid JSON:\n%s", out)
	}

	if !utf8.Valid(out) {
		t.Errorf("the document is not valid UTF-8:\n%q", out)
	}

	if !bytes.HasSuffix(out, []byte("\n")) || bytes.HasSuffix(out, []byte("\n\n")) {
		t.Errorf("the document must end with exactly one newline, it ends with %q",
			out[max(0, len(out)-2):])
	}
}

// snapshotDate is the date the document writes for a snapshot, as the Markdown
// report prints it: the retrieval date in UTC, YYYY-MM-DD.
func snapshotDate(snapshot catalog.Snapshot) string {
	return snapshot.RetrievedOn.UTC().Format("2006-01-02")
}

// TestReportSnapshotDateIsUTC pins that the retrieval date is the UTC date, as
// the Markdown report prints it: a snapshot taken just after midnight in a zone
// east of UTC must not show the previous day.
func TestReportSnapshotDateIsUTC(t *testing.T) {
	t.Parallel()

	a := assessment.Assessment{
		Target: scalewayTarget,
		Snapshot: catalog.Snapshot{
			SourceURL:   scalewaySourceURL,
			RetrievedOn: time.Date(2026, 11, 2, 0, 30, 0, 0, time.FixedZone("CET", 60*60)),
		},
		Source: sourceName,
	}

	want := snapshotDate(a.Snapshot)
	if want != "2026-11-01" {
		t.Fatalf("snapshotDate() = %q for a date just after midnight CET, want the previous UTC day", want)
	}

	document := decodeJSON(t, render(t, a, analysis.ReportMeta{}))

	if got := valueAt(t, document, "target", "snapshot", "retrieved"); got != want {
		t.Errorf("target.snapshot.retrieved = %v, want the UTC date %q", got, want)
	}
}

// TestReportDocumentShape pins the document's schema without a golden file: the
// schema version and kind, the metadata, the summary, the resources in the
// assessment's order with the ties and required changes the Markdown report
// shows, the blockers, and the exact key set of every object, because the
// schemas allow no key the format does not define.
func TestReportDocumentShape(t *testing.T) {
	t.Parallel()

	scaleway := testutil.ScalewayCatalog(t)
	a := assessment.Assess(newInventory(t, sourceName, awsResources()), scaleway)

	document := decodeJSON(t, render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	t.Run("fields", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			path []any
			want any
		}{
			// encoding/json decodes every JSON number into a float64.
			{name: "schema version", path: []any{"schemaVersion"}, want: float64(currentSchemaVersion)},
			{name: "kind", path: []any{"kind"}, want: "assessment"},
			{name: "inventory name", path: []any{"inventoryName"}, want: inventoryFileName},
			{name: "source", path: []any{"source"}, want: sourceName},
			{name: "target name", path: []any{"target", "name"}, want: scalewayTarget},
			{name: "snapshot source", path: []any{"target", "snapshot", "source"}, want: scalewaySourceURL},
			{
				name: "snapshot retrieval date is the date the Markdown report prints",
				path: []any{"target", "snapshot", "retrieved"},
				want: scalewayRetrievedOn,
			},
			{name: "summary direct", path: []any{"summary", "direct"}, want: float64(3)},
			{name: "summary partial", path: []any{"summary", "partial"}, want: float64(1)},
			{name: "summary none", path: []any{"summary", "none"}, want: float64(2)},
			{name: "summary total", path: []any{"summary", "total"}, want: float64(6)},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				if diff := cmp.Diff(test.want, valueAt(t, document, test.path...)); diff != "" {
					t.Errorf("%v mismatch (-want +got):\n%s", test.path, diff)
				}
			})
		}
	})

	t.Run("object keys", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			path []any
			want []string
		}{
			{
				name: "the document",
				path: nil,
				want: []string{
					"blockers", "inventoryName", "kind", "resources", "schemaVersion", "source", "summary",
					"target", "workloads",
				},
			},
			{name: "the target", path: []any{"target"}, want: []string{"name", "snapshot"}},
			{
				name: "the snapshot",
				path: []any{"target", "snapshot"},
				want: []string{"retrieved", "source"},
			},
			{
				name: "the summary",
				path: []any{"summary"},
				want: []string{"direct", "none", "partial", "total"},
			},
			{
				name: "a direct resource",
				path: []any{"resources", 0},
				want: []string{
					"affectedBy", "bestOffering", "category", "fit", "id", "rawType", "reasons", "region",
					"relations", "requiredChanges", "ties",
				},
			},
			{
				name: "a reason that sets only code and offering",
				path: []any{"resources", 0, "reasons", 0},
				want: []string{"code", "offering", "text"},
			},
			{
				name: "a reason that sets code, offering and capability",
				path: []any{"resources", 5, "reasons", 0},
				want: []string{"capability", "code", "offering", "text"},
			},
			{
				name: "a reason that sets nothing but its code",
				path: []any{"blockers", 1, "reason"},
				want: []string{"code", "text"},
			},
			{
				name: "a reason that sets code, target and category",
				path: []any{"blockers", 0, "reason"},
				want: []string{"category", "code", "target", "text"},
			},
			{name: "a blocker", path: []any{"blockers", 0}, want: []string{"reason", "resourceId"}},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				if diff := cmp.Diff(test.want, objectKeysAt(t, document, test.path...)); diff != "" {
					t.Errorf("%v keys mismatch (-want +got):\n%s", test.path, diff)
				}
			})
		}
	})

	t.Run("resources", func(t *testing.T) {
		t.Parallel()

		// The expected content of the fixture's resources, in resource ID order,
		// as the Markdown report's report.golden shows it. ties and
		// requiredChanges are always set, to an empty slice where the resource has
		// none, because the format writes [] and never null: arrayAt above fails a
		// document that writes null for either.
		tests := []struct {
			id              string
			fit             string
			bestOffering    string
			ties            []string
			requiredChanges []string
			reasonCodes     []string
			reasonTexts     []string
		}{
			{
				id:              "bucket-example",
				fit:             "direct",
				bestOffering:    "Object Storage",
				ties:            []string{},
				requiredChanges: []string{},
				reasonCodes:     []string{"all-capabilities"},
				reasonTexts:     []string{"Object Storage offers every capability the resource uses"},
			},
			{
				id:              "db-example",
				fit:             "direct",
				bestOffering:    "Managed Database for PostgreSQL",
				ties:            []string{"Managed Database for PostgreSQL and MySQL"},
				requiredChanges: []string{},
				reasonCodes:     []string{"all-capabilities"},
				reasonTexts:     []string{"Managed Database for PostgreSQL offers every capability the resource uses"},
			},
			{
				id:              "fn-example",
				fit:             "none",
				ties:            []string{},
				requiredChanges: []string{},
				reasonCodes:     []string{"no-offering"},
				reasonTexts:     []string{"no offering in the Scaleway catalog for serverless.function"},
			},
			{
				id:              "i-0123456789abcdef0",
				fit:             "direct",
				bestOffering:    "Instances",
				ties:            []string{},
				requiredChanges: []string{},
				reasonCodes:     []string{"all-capabilities"},
				reasonTexts:     []string{"Instances offers every capability the resource uses"},
			},
			{
				id:              "misc-example",
				fit:             "none",
				ties:            []string{},
				requiredChanges: []string{},
				reasonCodes:     []string{"not-classified"},
				reasonTexts:     []string{"not classified"},
			},
			{
				id:              "partial-example",
				fit:             "partial",
				bestOffering:    "Instances",
				ties:            []string{},
				requiredChanges: []string{"node-high-availability", "snapshots"},
				reasonCodes:     []string{"missing-capability", "missing-capability"},
				reasonTexts: []string{
					"Instances does not offer node-high-availability",
					"Instances does not offer snapshots",
				},
			},
		}

		resources := arrayAt(t, document, "resources")
		if len(resources) != len(a.Verdicts) {
			t.Fatalf("the document has %d resources, the assessment has %d verdicts",
				len(resources), len(a.Verdicts))
		}

		if len(tests) != len(a.Verdicts) {
			t.Fatalf("the test lists %d resources, the assessment has %d verdicts",
				len(tests), len(a.Verdicts))
		}

		for i, test := range tests {
			t.Run(test.id, func(t *testing.T) {
				t.Parallel()

				at := []any{"resources", i}

				// The order is the assessment's verdict order.
				if got := valueAt(t, document, extend(at, "id")...); got != a.Verdicts[i].Resource.ID {
					t.Errorf("resources[%d].id = %v, want the assessment's verdict %q",
						i, got, a.Verdicts[i].Resource.ID)
				}

				if got := valueAt(t, document, extend(at, "fit")...); got != test.fit {
					t.Errorf("%q fit = %v, want %q", test.id, got, test.fit)
				}

				if got := valueAt(t, document, extend(at, "bestOffering")...); got != test.bestOffering {
					t.Errorf("%q bestOffering = %v, want %q", test.id, got, test.bestOffering)
				}

				if diff := cmp.Diff(test.ties, stringsAt(t, document, extend(at, "ties")...)); diff != "" {
					t.Errorf("%q ties mismatch (-want +got):\n%s", test.id, diff)
				}

				changes := stringsAt(t, document, extend(at, "requiredChanges")...)
				if diff := cmp.Diff(test.requiredChanges, changes); diff != "" {
					t.Errorf("%q requiredChanges mismatch (-want +got):\n%s", test.id, diff)
				}

				var codes, texts []string

				for j := range arrayAt(t, document, extend(at, "reasons")...) {
					atReason := extend(at, "reasons", j)

					code, ok := valueAt(t, document, extend(atReason, "code")...).(string)
					if !ok {
						t.Fatalf("%q reasons[%d].code is not a string", test.id, j)
					}

					text, ok := valueAt(t, document, extend(atReason, "text")...).(string)
					if !ok {
						t.Fatalf("%q reasons[%d].text is not a string", test.id, j)
					}

					codes = append(codes, code)
					texts = append(texts, text)
				}

				if diff := cmp.Diff(test.reasonCodes, codes); diff != "" {
					t.Errorf("%q reason codes mismatch (-want +got):\n%s", test.id, diff)
				}

				if diff := cmp.Diff(test.reasonTexts, texts); diff != "" {
					t.Errorf("%q reason texts mismatch (-want +got):\n%s", test.id, diff)
				}
			})
		}
	})

	t.Run("blockers", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			resourceID string
			code       string
			text       string
			target     string
			category   string
		}{
			{
				resourceID: "fn-example",
				code:       "no-offering",
				text:       "no offering in the Scaleway catalog for serverless.function",
				target:     scalewayTarget,
				category:   "serverless.function",
			},
			{resourceID: "misc-example", code: "not-classified", text: "not classified"},
		}

		blockers := arrayAt(t, document, "blockers")
		if len(blockers) != len(tests) {
			t.Fatalf("the document has %d blockers, want %d", len(blockers), len(tests))
		}

		for i, test := range tests {
			t.Run(test.resourceID, func(t *testing.T) {
				t.Parallel()

				at := []any{"blockers", i}
				if got := valueAt(t, document, extend(at, "resourceId")...); got != test.resourceID {
					t.Errorf("blockers[%d].resourceId = %v, want %q", i, got, test.resourceID)
				}

				reason := extend(at, "reason")
				if got := valueAt(t, document, extend(reason, "code")...); got != test.code {
					t.Errorf("%q blocker code = %v, want %q", test.resourceID, got, test.code)
				}

				if got := valueAt(t, document, extend(reason, "text")...); got != test.text {
					t.Errorf("%q blocker text = %v, want %q", test.resourceID, got, test.text)
				}

				// The target and the category are written only when the reason
				// sets them, which the key set above pins.
				if test.target != "" {
					if got := valueAt(t, document, extend(reason, "target")...); got != test.target {
						t.Errorf("%q blocker target = %v, want %q", test.resourceID, got, test.target)
					}
				}

				if test.category != "" {
					if got := valueAt(t, document, extend(reason, "category")...); got != test.category {
						t.Errorf("%q blocker category = %v, want %q", test.resourceID, got, test.category)
					}
				}
			})
		}
	})

	t.Run("an empty assessment keeps empty arrays", func(t *testing.T) {
		t.Parallel()

		empty := assessment.Assess(newInventory(t, sourceName, nil), scaleway)
		emptyDocument := decodeJSON(t, render(t, empty, analysis.ReportMeta{InventoryName: inventoryFileName}))

		for _, name := range []string{"resources", "blockers"} {
			array, ok := valueAt(t, emptyDocument, name).([]any)
			if !ok {
				t.Errorf("%q is not an array; an array is never null:\n%s", name, render(t, empty, analysis.ReportMeta{}))
			}

			if len(array) != 0 {
				t.Errorf("%q holds %d elements, want an empty array", name, len(array))
			}
		}

		want := map[string]any{
			"direct":  float64(0),
			"partial": float64(0),
			"none":    float64(0),
			"total":   float64(0),
		}
		if diff := cmp.Diff(want, valueAt(t, emptyDocument, "summary")); diff != "" {
			t.Errorf("the empty assessment's summary mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestReportWritesEmptyValues pins the fields the format writes even when they
// hold nothing: an inventory that did not come from a file, a resource with no
// raw type and no region, a verdict without a best offering, and every array
// present and empty rather than null.
func TestReportWritesEmptyValues(t *testing.T) {
	t.Parallel()

	// The verdict holds no reasons, so the resources[0].reasons array the loop
	// below checks really is empty; the blocker still carries the reason.
	reason := mapping.Reason{Code: mapping.NotClassified}
	a := assessment.Assessment{
		Target:   scalewayTarget,
		Snapshot: testutil.ScalewaySnapshot(),
		Source:   sourceName,
		Verdicts: []assessment.Verdict{{
			Resource: inventory.Resource{
				ID:       "global-example",
				Category: taxonomy.StorageObject,
			},
			Fit: mapping.None,
		}},
		Blockers: []assessment.Blocker{{ResourceID: "global-example", Reason: reason}},
		Summary:  assessment.Summary{Total: 1, None: 1},
	}

	document := decodeJSON(t, render(t, a, analysis.ReportMeta{}))

	tests := []struct {
		name string
		path []any
		want string
	}{
		{name: "an inventory that is not a file has an empty name", path: []any{"inventoryName"}, want: ""},
		{name: "a resource without a raw type keeps the key", path: []any{"resources", 0, "rawType"}, want: ""},
		{name: "a resource without a region keeps the key", path: []any{"resources", 0, "region"}, want: ""},
		{name: "a verdict without a candidate has an empty best offering", path: []any{"resources", 0, "bestOffering"}, want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(test.want, valueAt(t, document, test.path...)); diff != "" {
				t.Errorf("%v mismatch (-want +got):\n%s", test.path, diff)
			}
		})
	}

	for _, name := range []string{"ties", "requiredChanges", "reasons"} {
		array, ok := valueAt(t, document, "resources", 0, name).([]any)
		if !ok {
			t.Errorf("resources[0].%s is not an array; an array is never null", name)

			continue
		}

		if len(array) != 0 {
			t.Errorf("resources[0].%s holds %d elements, want an empty array", name, len(array))
		}
	}
}

// The hostile fixture: every value the document shows carries a quote, a
// backslash, a control character, a line ending, a tab, a line separator, a
// paragraph separator, an HTML end tag, an ampersand and a byte that is not valid
// UTF-8, so a report that concatenated strings instead of JSON-encoding them
// would not be valid JSON. The domain constructors refuse several of these
// values, so the assessment is built by hand, as the Markdown reporter's escaping
// tests build theirs.
var (
	// hostileInventoryName is the inventory's base name as the run reports it.
	hostileInventoryName = "inv\"\\\n\t\x00\x1b\u2028</script>\xff.json"
	// hostileSource is the inventory's source.
	hostileSource = "aws\"\\\n\t\x00\x1b\u2028</script>\xff"
	// hostileTarget is the catalog's display name.
	hostileTarget = "Evil \"target\" \\ \t\x00\x1b\u2028</script>\xff"
	// hostileSourceURL is the catalog snapshot's source.
	hostileSourceURL = "https://example.com/a\"b\\c\nd\te\x00\x1b\u2028</script>\xff"
	// hostileID, hostileRawType and hostileRegion are the direct resource's
	// values. hostileID also carries a paragraph separator and an ampersand, the
	// characters JSON escapes besides < and >.
	hostileID      = "i-\"1\"\\x\n\t\x00\x1b\u2028\u2029</script>&x\xff"
	hostileRawType = "AWS::\"X\"\\Y\t\x00\x1b\u2028</script>\xff"
	hostileRegion  = "eu-\"west\"\\1\n\x00\x1b\u2028</script>\xff"
	// hostileOffering is the best offering's name, and hostileTie the name of the
	// other candidate the direct verdict ties with.
	hostileOffering = "Off\"er\\ing\x00\x1b\u2028</script>\xff"
	hostileTie      = "Tie\"\\\n\t\x00\x1b\u2028</script>\xff"
	// hostileCapability is the capability the partial verdict's offering lacks.
	hostileCapability = taxonomy.Capability("cap\"a\\b\n\t\x00\x1b\u2028</script>\xff")
	// hostileReasonCode is a code the domain does not know, so the reason's text
	// is the code itself.
	hostileReasonCode = mapping.ReasonCode("reason\"\\\n\t\x00\x1b\u2028</script>\xff")
	// hostileBlockedID is the blocked resource's ID, which the blocker repeats.
	hostileBlockedID = "fn\"\\\n\t\x00\x1b\u2028</script>\xff"
	// hostileWorkload is a workload name, which a report shows as a heading in
	// Markdown and as the name of a workload object in JSON.
	hostileWorkload = "ws|`x`[a](b)\"\\\n\t\x00\x1b\u2028</script>\xff"
	// hostileRelationTarget is the ID a relation points at, which a report shows
	// in a table cell in Markdown and as the target of a relation object in JSON.
	hostileRelationTarget = "db-\"rel\"\\\n\t\x00\x1b\u2028</script>\xff"
)

// hostileCategory is the category of the hostile resources: a value the
// document writes as given.
func hostileCategory() taxonomy.Category {
	return taxonomy.Category("cat\"\\\n\t\x00\x1b\u2028</script>\xff")
}

// hostileMetadata is the metadata of the hostile document.
func hostileMetadata() analysis.ReportMeta {
	return analysis.ReportMeta{InventoryName: hostileInventoryName}
}

// hostileAssessment is an assessment whose every text value is hostile: a direct
// verdict with two tied candidates, a partial verdict with a missing capability,
// and a blocked verdict with an unknown reason code and a no-offering reason
// that repeats the hostile target and category. Its blockers repeat the blocked
// resource's hostile ID, its direct resource names a hostile workload and
// relates to a hostile ID, and one workload group carries that name.
func hostileAssessment() assessment.Assessment {
	category := hostileCategory()

	noOffering := mapping.Reason{Code: mapping.NoOffering, Target: hostileTarget, Category: category}

	return assessment.Assessment{
		Target:   hostileTarget,
		Snapshot: catalog.Snapshot{SourceURL: hostileSourceURL, RetrievedOn: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)},
		Source:   hostileSource,
		Verdicts: []assessment.Verdict{
			{
				Resource: inventory.Resource{
					ID:           hostileID,
					RawType:      hostileRawType,
					Category:     category,
					Region:       hostileRegion,
					Capabilities: []taxonomy.Capability{hostileCapability},
					Workload:     hostileWorkload,
					Relations:    []inventory.Relation{{Type: inventory.DependsOn, Target: hostileRelationTarget}},
				},
				Fit:          mapping.Direct,
				BestOffering: hostileOffering,
				Candidates: []mapping.Candidate{
					{
						Offering: catalog.Offering{Name: hostileOffering, Categories: []taxonomy.Category{category}},
						Fit:      mapping.Direct,
					},
					{
						Offering: catalog.Offering{Name: hostileTie, Categories: []taxonomy.Category{category}},
						Fit:      mapping.Direct,
					},
				},
				Reasons: []mapping.Reason{{Code: mapping.AllCapabilities, Offering: hostileOffering}},
			},
			{
				Resource: inventory.Resource{
					ID:       "partial-example",
					RawType:  "AWS::EC2::Instance",
					Category: category,
				},
				Fit:          mapping.Partial,
				BestOffering: hostileOffering,
				Reasons: []mapping.Reason{{
					Code:       mapping.MissingCapability,
					Offering:   hostileOffering,
					Capability: hostileCapability,
				}},
				RequiredChanges: []taxonomy.Capability{hostileCapability},
			},
			{
				Resource: inventory.Resource{
					ID:       hostileBlockedID,
					RawType:  hostileRawType,
					Category: category,
					Region:   hostileRegion,
				},
				Fit:     mapping.None,
				Reasons: []mapping.Reason{{Code: hostileReasonCode}, noOffering},
			},
		},
		Blockers: []assessment.Blocker{
			{ResourceID: hostileBlockedID, Reason: mapping.Reason{Code: hostileReasonCode}},
			{ResourceID: hostileBlockedID, Reason: noOffering},
		},
		Summary: assessment.Summary{Total: 3, Direct: 1, Partial: 1, None: 1},
		Workloads: []assessment.WorkloadSummary{{
			Name:        hostileWorkload,
			ResourceIDs: []string{hostileID},
			Summary:     assessment.Summary{Total: 1, Direct: 1},
		}},
	}
}

// TestReportHostileValues pins that no value reaches the document unencoded: the
// output is valid JSON and valid UTF-8, and decoding it gives every value back,
// with each invalid UTF-8 byte replaced by U+FFFD as encoding/json does.
func TestReportHostileValues(t *testing.T) {
	t.Parallel()

	got := render(t, hostileAssessment(), hostileMetadata())

	testutil.Golden(t, "testdata/report-hostile.golden", got)
	checkDocument(t, got)
	checkJSONEscapes(t, got)

	document := decodeJSON(t, got)

	// want is the string the document holds before JSON encoding: the hostile
	// value as the assessment carries it, or the sentence the format builds from
	// it.
	tests := []struct {
		name string
		path []any
		want string
	}{
		{name: "inventory name", path: []any{"inventoryName"}, want: hostileInventoryName},
		{name: "source", path: []any{"source"}, want: hostileSource},
		{name: "target name", path: []any{"target", "name"}, want: hostileTarget},
		{name: "snapshot source", path: []any{"target", "snapshot", "source"}, want: hostileSourceURL},
		{name: "resource ID", path: []any{"resources", 0, "id"}, want: hostileID},
		{
			name: "relation type",
			path: []any{"resources", 0, "relations", 0, "type"},
			want: string(inventory.DependsOn),
		},
		{
			name: "relation target",
			path: []any{"resources", 0, "relations", 0, "target"},
			want: hostileRelationTarget,
		},
		{name: "workload name", path: []any{"workloads", 0, "name"}, want: hostileWorkload},
		{name: "raw type", path: []any{"resources", 0, "rawType"}, want: hostileRawType},
		{name: "region", path: []any{"resources", 0, "region"}, want: hostileRegion},
		{name: "category", path: []any{"resources", 0, "category"}, want: string(hostileCategory())},
		{name: "best offering", path: []any{"resources", 0, "bestOffering"}, want: hostileOffering},
		{name: "tie", path: []any{"resources", 0, "ties", 0}, want: hostileTie},
		{
			name: "required change",
			path: []any{"resources", 1, "requiredChanges", 0},
			want: string(hostileCapability),
		},
		{
			name: "missing capability reason text",
			path: []any{"resources", 1, "reasons", 0, "text"},
			want: hostileOffering + " does not offer " + string(hostileCapability),
		},
		{
			name: "unknown reason code",
			path: []any{"resources", 2, "reasons", 0, "code"},
			want: string(hostileReasonCode),
		},
		{
			name: "unknown reason text is the code",
			path: []any{"resources", 2, "reasons", 0, "text"},
			want: string(hostileReasonCode),
		},
		{
			name: "no-offering reason text",
			path: []any{"resources", 2, "reasons", 1, "text"},
			want: "no offering in the " + hostileTarget + " catalog for " + string(hostileCategory()),
		},
		{
			name: "no-offering reason target",
			path: []any{"resources", 2, "reasons", 1, "target"},
			want: hostileTarget,
		},
		{
			name: "no-offering reason category",
			path: []any{"resources", 2, "reasons", 1, "category"},
			want: string(hostileCategory()),
		},
		{name: "blocker resource ID", path: []any{"blockers", 0, "resourceId"}, want: hostileBlockedID},
		{
			name: "blocker reason text",
			path: []any{"blockers", 0, "reason", "text"},
			want: string(hostileReasonCode),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := valueAt(t, document, test.path...).(string)
			if !ok {
				t.Fatalf("%v is not a string", test.path)
			}

			if want := strings.ToValidUTF8(test.want, "\uFFFD"); got != want {
				t.Errorf("%v = %q, want %q", test.path, got, want)
			}
		})
	}
}

// TestReportIsDeterministic pins that the same assessment always renders the
// same bytes: a document never depends on the clock, the environment or map
// iteration.
func TestReportIsDeterministic(t *testing.T) {
	t.Parallel()

	scaleway := testutil.ScalewayCatalog(t)
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	first := render(t, assessment.Assess(newInventory(t, sourceName, awsResources()), scaleway), meta)
	second := render(t, assessment.Assess(newInventory(t, sourceName, awsResources()), scaleway), meta)

	if !bytes.Equal(first, second) {
		t.Errorf("two documents of the same assessment differ:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// failingWriter is a writer that always fails, so a test can see that Report
// reports a write failure instead of swallowing it.
type failingWriter struct {
	err error
}

// Write returns the writer's error and writes nothing.
func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

// TestReportReturnsWriteError pins that a failing writer is reported to the
// caller, wrapped or as it came, never ignored: the driving adapter decides what
// to do with a partial document.
func TestReportReturnsWriteError(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("disk is full")
	a := assessment.Assess(newInventory(t, sourceName, awsResources()), testutil.ScalewayCatalog(t))

	err := jsonreport.New().Report(t.Context(), failingWriter{err: writeErr}, a,
		analysis.ReportMeta{InventoryName: inventoryFileName})
	if !errors.Is(err, writeErr) {
		t.Fatalf("Report() error = %v, want an error wrapping %v", err, writeErr)
	}
}

// TestReportsCancelledContext pins that a cancelled context stops both documents
// before anything is encoded: the caller gets the cancellation, and the writer
// never sees a report the caller has stopped waiting for.
func TestReportsCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	a := assessment.Assess(newInventory(t, sourceName, awsResources()), testutil.ScalewayCatalog(t))
	c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	tests := []struct {
		name  string
		write func(io.Writer) error
	}{
		{
			name: "the single-target document",
			write: func(w io.Writer) error {
				return jsonreport.New().Report(ctx, w, a, meta)
			},
		},
		{
			name: "the comparison document",
			write: func(w io.Writer) error {
				return jsonreport.New().ReportComparison(ctx, w, c, meta)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			if err := tc.write(&out); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s: error = %v, want an error wrapping context.Canceled", tc.name, err)
			}

			if out.Len() != 0 {
				t.Errorf("%s wrote %d bytes to the writer, want none:\n%s", tc.name, out.Len(), out.String())
			}
		})
	}
}

// checkJSONEscapes asserts that the document holds the escaped form of the
// characters a reader could act on and never the raw character: the golden is
// read by a browser and a log reader as well as by a JSON parser, so U+2029 and &
// must reach it as the escapes encoding/json writes.
func checkJSONEscapes(t *testing.T, out []byte) {
	t.Helper()

	for _, escaped := range []string{`\u2029`, `\u0026`} {
		if !bytes.Contains(out, []byte(escaped)) {
			t.Errorf("the document does not hold the escape %s:\n%s", escaped, out)
		}
	}

	for _, raw := range []string{"\u2029", "&"} {
		if bytes.Contains(out, []byte(raw)) {
			t.Errorf("the document holds the raw character %q:\n%s", raw, out)
		}
	}
}

// decodeJSON decodes data into the generic form encoding/json produces, which is
// how a test sees the shape of the document itself rather than of a Go struct.
func decodeJSON(tb testing.TB, data []byte) map[string]any {
	tb.Helper()

	if !json.Valid(data) {
		tb.Fatalf("the document is not valid JSON:\n%s", data)
	}

	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		tb.Fatalf("the document is not a JSON object: %v\n%s", err, data)
	}

	return document
}

// valueAt returns the value at path in document, where a string is an object key
// and an int an array index. It fails the test when the path is missing, because
// a document without a field the format promises is a failure of its own.
func valueAt(tb testing.TB, document any, path ...any) any {
	tb.Helper()

	at := "$"

	for _, step := range path {
		switch key := step.(type) {
		case string:
			object, ok := document.(map[string]any)
			if !ok {
				tb.Fatalf("%s is %T, not an object, so %q cannot be read from it", at, document, key)
			}

			value, ok := object[key]
			if !ok {
				tb.Fatalf("%s holds no %q key", at, key)
			}

			document = value
			at += "." + key
		case int:
			array, ok := document.([]any)
			if !ok {
				tb.Fatalf("%s is %T, not an array, so index %d cannot be read from it", at, document, key)
			}

			if key < 0 || key >= len(array) {
				tb.Fatalf("%s holds %d elements, so index %d cannot be read from it", at, len(array), key)
			}

			document = array[key]
			at += "[" + strconv.Itoa(key) + "]"
		default:
			tb.Fatalf("unsupported path step %T", step)
		}
	}

	return document
}

// arrayAt returns the array at path in document.
func arrayAt(tb testing.TB, document any, path ...any) []any {
	tb.Helper()

	value := valueAt(tb, document, path...)

	array, ok := value.([]any)
	if !ok {
		tb.Fatalf("%v is %T, want an array", path, value)
	}

	return array
}

// stringsAt returns the string array at path in document.
func stringsAt(tb testing.TB, document any, path ...any) []string {
	tb.Helper()

	array := arrayAt(tb, document, path...)
	out := make([]string, 0, len(array))

	for i, element := range array {
		str, ok := element.(string)
		if !ok {
			tb.Fatalf("%v[%d] is %T, want a string", path, i, element)
		}

		out = append(out, str)
	}

	return out
}

// objectKeysAt returns the keys of the object at path in document, sorted, so a
// test can assert the key set the format defines.
func objectKeysAt(tb testing.TB, document any, path ...any) []string {
	tb.Helper()

	object, ok := valueAt(tb, document, path...).(map[string]any)
	if !ok {
		tb.Fatalf("%v is not an object", path)
	}

	return slices.Sorted(maps.Keys(object))
}

// extend returns base with steps appended, so a subtest can build a path from one
// it shares without changing that one.
func extend(base []any, steps ...any) []any {
	return append(slices.Clone(base), steps...)
}

// The report fixture's target, and the snapshot its catalog says the prices were
// taken from: invented values, so a test can state each estimate exactly.
const (
	reportTarget      = "Example Cloud"
	reportSourceURL   = "https://www.example.com/pricing/"
	reportRetrievedOn = "2026-11-13"
)

// sized is a given float64, as an inventory size and a monthly cost carry it.
func sized(v float64) *float64 { return &v }

// estimateResources is the inventory the estimate tests write. It is the one
// inventory a document has to tell every estimate state apart in:
//
//   - bucket-storage is storage.object, priced by the stored GiB;
//   - cluster-small is kubernetes.managed with a fixed fee;
//   - server-small and worker-jobs are compute.vm on the smallest plan;
//   - server-huge is compute.vm larger than the largest plan;
//   - legacy-nosize is compute.vm without a size;
//   - function-orders is serverless.function, which is priced by usage;
//   - queue-jobs is messaging.queue, which no offering serves, so it is blocked,
//     and worker-jobs depends on it and is a replica of server-small, so it
//     carries relations and an "affected by" note.
//
// Three workloads are named, one of them without a source cost for all of its
// resources, and the resources without a workload are the "No workload" group.
// The monthly costs are in two currencies, so the source cost is never
// converted.
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

// assessEstimates assesses the estimate fixture with estimates on, in the
// fixture's default region unless region is given.
func assessEstimates(t *testing.T, region string) assessment.Assessment {
	t.Helper()

	return assessment.AssessWith(newInventory(t, sourceName, estimateResources()), testutil.ReportCatalog(t),
		assessment.Options{Region: region, Estimate: true})
}

// TestReportWithEstimates pins the schema version 2 document of a sized
// inventory with estimates on: the estimate basis and its total, the plan, the
// estimate and its breakdown, the relations and the "affected by" notes per
// resource, the source cost per currency and the workloads.
func TestReportWithEstimates(t *testing.T) {
	t.Parallel()

	got := render(t, assessEstimates(t, ""), analysis.ReportMeta{InventoryName: inventoryFileName})
	checkDocument(t, got)

	document := decodeJSON(t, got)

	t.Run("the document", func(t *testing.T) {
		t.Parallel()

		if diff := cmp.Diff(float64(2), valueAt(t, document, "schemaVersion")); diff != "" {
			t.Errorf("schemaVersion mismatch (-want +got):\n%s", diff)
		}

		if diff := cmp.Diff("assessment", valueAt(t, document, "kind")); diff != "" {
			t.Errorf("kind mismatch (-want +got):\n%s", diff)
		}

		wantKeys := []string{
			"blockers", "estimate", "inventoryName", "kind", "resources", "schemaVersion", "source",
			"sourceCost", "summary", "target", "workloads",
		}
		if diff := cmp.Diff(wantKeys, objectKeysAt(t, document)); diff != "" {
			t.Errorf("document keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the estimate basis and its total", func(t *testing.T) {
		t.Parallel()

		wantKeys := []string{
			"currency", "excluded", "hoursBasis", "priceDate", "region", "regionChosen", "total",
		}
		if diff := cmp.Diff(wantKeys, objectKeysAt(t, document, "estimate")); diff != "" {
			t.Errorf("estimate keys mismatch (-want +got):\n%s", diff)
		}

		tests := []struct {
			name string
			path []any
			want any
		}{
			{name: "the region the prices are for", path: []any{"estimate", "region"}, want: "xx-par"},
			{name: "no region was chosen", path: []any{"estimate", "regionChosen"}, want: false},
			{name: "the price date", path: []any{"estimate", "priceDate"}, want: reportRetrievedOn},
			{name: "the currency", path: []any{"estimate", "currency"}, want: "EUR"},
			{name: "the hours basis", path: []any{"estimate", "hoursBasis"}, want: float64(730)},
			{name: "the total", path: []any{"estimate", "total", "amount"}, want: "2535.00"},
			{name: "how many resources were estimated", path: []any{"estimate", "total", "estimated"}, want: float64(4)},
			{name: "how many resources there are", path: []any{"estimate", "total", "resources"}, want: float64(8)},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				if diff := cmp.Diff(test.want, valueAt(t, document, test.path...)); diff != "" {
					t.Errorf("%v mismatch (-want +got):\n%s", test.path, diff)
				}
			})
		}

		if diff := cmp.Diff([]string{"VAT", "discounts"}, stringsAt(t, document, "estimate", "excluded")); diff != "" {
			t.Errorf("estimate.excluded mismatch (-want +got):\n%s", diff)
		}

		if diff := cmp.Diff([]string{"amount", "estimated", "resources"},
			objectKeysAt(t, document, "estimate", "total")); diff != "" {
			t.Errorf("estimate.total keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a resource with a plan", func(t *testing.T) {
		t.Parallel()

		at := resourceIndex(t, document, "worker-jobs")

		wantKeys := []string{
			"affectedBy", "bestOffering", "category", "estimate", "fit", "id", "plan", "rawType", "reasons",
			"region", "relations", "requiredChanges", "ties",
		}
		if diff := cmp.Diff(wantKeys, objectKeysAt(t, document, extend(at)...)); diff != "" {
			t.Errorf("resource keys mismatch (-want +got):\n%s", diff)
		}

		plan := extend(at, "plan")
		if diff := cmp.Diff([]string{"includedStorageGiB", "memoryGiB", "name", "price", "vcpus"},
			objectKeysAt(t, document, plan...)); diff != "" {
			t.Errorf("plan keys mismatch (-want +got):\n%s", diff)
		}

		tests := []struct {
			path []any
			want any
		}{
			{path: extend(plan, "name"), want: "EX-S"},
			{path: extend(plan, "vcpus"), want: float64(1)},
			{path: extend(plan, "memoryGiB"), want: "2"},
			{path: extend(plan, "includedStorageGiB"), want: float64(20)},
			{path: extend(plan, "price"), want: "10.00"},
			{path: extend(at, "estimate", "estimated"), want: true},
			{path: extend(at, "estimate", "amount"), want: "10.00"},
			{path: extend(at, "estimate", "breakdown", "plan"), want: "10.00"},
			{path: extend(at, "estimate", "breakdown", "storage"), want: "0.00"},
			{path: extend(at, "estimate", "breakdown", "fixedFee"), want: "0.00"},
			// The resource gives no count and no hours, so the estimate says it
			// priced one instance running 730 hours (ADR 0025).
			{path: extend(at, "estimate", "countAssumed"), want: true},
			{path: extend(at, "estimate", "hoursAssumed"), want: true},
		}

		for _, test := range tests {
			if diff := cmp.Diff(test.want, valueAt(t, document, test.path...)); diff != "" {
				t.Errorf("%v mismatch (-want +got):\n%s", test.path, diff)
			}
		}

		// The relations are the inventory's, in its order, and the one blocked
		// target is the one affected note.
		wantRelations := []map[string]any{
			{"type": "depends-on", "target": "queue-jobs"},
			{"type": "replica-of", "target": "server-small"},
		}

		var relations []map[string]any

		for i := range arrayAt(t, document, extend(at, "relations")...) {
			relations = append(relations, map[string]any{
				"type":   valueAt(t, document, extend(at, "relations", i, "type")...),
				"target": valueAt(t, document, extend(at, "relations", i, "target")...),
			})
		}

		if diff := cmp.Diff(wantRelations, relations); diff != "" {
			t.Errorf("relations mismatch (-want +got):\n%s", diff)
		}

		if diff := cmp.Diff([]string{"queue-jobs"}, stringsAt(t, document, extend(at, "affectedBy")...)); diff != "" {
			t.Errorf("affectedBy mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a fixed fee is part of the estimate", func(t *testing.T) {
		t.Parallel()

		at := resourceIndex(t, document, "cluster-small")

		if diff := cmp.Diff([]string{"amount", "breakdown", "countAssumed", "estimated", "hoursAssumed"},
			objectKeysAt(t, document, extend(at, "estimate")...)); diff != "" {
			t.Errorf("estimate keys mismatch (-want +got), an estimated resource says what it assumed:\n%s", diff)
		}

		tests := []struct {
			path []any
			want any
		}{
			{path: extend(at, "estimate", "amount"), want: "15.00"},
			{path: extend(at, "estimate", "breakdown", "plan"), want: "10.00"},
			{path: extend(at, "estimate", "breakdown", "fixedFee"), want: "5.00"},
			{path: extend(at, "plan", "name"), want: "KN-S"},
			{path: extend(at, "estimate", "countAssumed"), want: true},
			{path: extend(at, "estimate", "hoursAssumed"), want: true},
		}

		for _, test := range tests {
			if diff := cmp.Diff(test.want, valueAt(t, document, test.path...)); diff != "" {
				t.Errorf("%v mismatch (-want +got):\n%s", test.path, diff)
			}
		}
	})

	t.Run("a resource priced by storage has no plan", func(t *testing.T) {
		t.Parallel()

		at := resourceIndex(t, document, "bucket-storage")

		if diff := cmp.Diff([]string{"amount", "breakdown", "countAssumed", "estimated", "hoursAssumed"},
			objectKeysAt(t, document, extend(at, "estimate")...)); diff != "" {
			t.Errorf("estimate keys mismatch (-want +got):\n%s", diff)
		}

		tests := []struct {
			path []any
			want any
		}{
			{path: extend(at, "estimate", "amount"), want: "2500.00"},
			{path: extend(at, "estimate", "breakdown", "plan"), want: "0.00"},
			{path: extend(at, "estimate", "breakdown", "storage"), want: "2500.00"},
			{path: extend(at, "estimate", "breakdown", "fixedFee"), want: "0.00"},
			// Object storage is priced by the GiB stored, so it assumes neither a
			// count nor a number of hours.
			{path: extend(at, "estimate", "countAssumed"), want: false},
			{path: extend(at, "estimate", "hoursAssumed"), want: false},
		}

		for _, test := range tests {
			if diff := cmp.Diff(test.want, valueAt(t, document, test.path...)); diff != "" {
				t.Errorf("%v mismatch (-want +got):\n%s", test.path, diff)
			}
		}
	})

	t.Run("a resource that is not estimated says why and has no amount", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			id     string
			reason string
		}{
			{id: "function-orders", reason: "usage-priced"},
			{id: "legacy-nosize", reason: "no-size"},
			{id: "queue-jobs", reason: "no-fit"},
			{id: "server-huge", reason: "no-plan-large-enough"},
		}

		for _, test := range tests {
			t.Run(test.id, func(t *testing.T) {
				t.Parallel()

				at := resourceIndex(t, document, test.id)

				if diff := cmp.Diff([]string{"estimated", "reason"},
					objectKeysAt(t, document, extend(at, "estimate")...)); diff != "" {
					t.Errorf("%q estimate keys mismatch (-want +got), a resource that is not estimated "+
						"has no amount and no breakdown:\n%s", test.id, diff)
				}

				if diff := cmp.Diff(false, valueAt(t, document, extend(at, "estimate", "estimated")...)); diff != "" {
					t.Errorf("%q estimate.estimated mismatch (-want +got):\n%s", test.id, diff)
				}

				if diff := cmp.Diff(test.reason, valueAt(t, document, extend(at, "estimate", "reason")...)); diff != "" {
					t.Errorf("%q estimate.reason mismatch (-want +got):\n%s", test.id, diff)
				}
			})
		}
	})

	t.Run("the plan a resource has no plan large enough for", func(t *testing.T) {
		t.Parallel()

		at := resourceIndex(t, document, "server-huge")

		tests := []struct {
			path []any
			want any
		}{
			{path: extend(at, "plan", "name"), want: "EX-L"},
			{path: extend(at, "plan", "vcpus"), want: float64(4)},
			{path: extend(at, "plan", "memoryGiB"), want: "8"},
			{path: extend(at, "plan", "price"), want: "40.00"},
		}

		for _, test := range tests {
			if diff := cmp.Diff(test.want, valueAt(t, document, test.path...)); diff != "" {
				t.Errorf("%v mismatch (-want +got):\n%s", test.path, diff)
			}
		}
	})

	t.Run("a resource without a plan keeps no plan key", func(t *testing.T) {
		t.Parallel()

		for _, id := range []string{"function-orders", "legacy-nosize", "queue-jobs"} {
			at := resourceIndex(t, document, id)

			if keys := objectKeysAt(t, document, extend(at)...); slices.Contains(keys, "plan") {
				t.Errorf("%q holds a plan key, want none: %q", id, keys)
			}
		}
	})

	t.Run("the source cost per currency", func(t *testing.T) {
		t.Parallel()

		if diff := cmp.Diff([]string{"covered", "currencies", "resources"},
			objectKeysAt(t, document, "sourceCost")); diff != "" {
			t.Errorf("sourceCost keys mismatch (-want +got):\n%s", diff)
		}

		tests := []struct {
			path []any
			want any
		}{
			{path: []any{"sourceCost", "covered"}, want: float64(5)},
			{path: []any{"sourceCost", "resources"}, want: float64(8)},
			{path: []any{"sourceCost", "currencies", 0, "currency"}, want: "EUR"},
			{path: []any{"sourceCost", "currencies", 0, "amount"}, want: "2415.00"},
			{path: []any{"sourceCost", "currencies", 0, "resources"}, want: float64(2)},
			{path: []any{"sourceCost", "currencies", 1, "currency"}, want: "USD"},
			{path: []any{"sourceCost", "currencies", 1, "amount"}, want: "1440.00"},
			{path: []any{"sourceCost", "currencies", 1, "resources"}, want: float64(3)},
		}

		for _, test := range tests {
			if diff := cmp.Diff(test.want, valueAt(t, document, test.path...)); diff != "" {
				t.Errorf("%v mismatch (-want +got):\n%s", test.path, diff)
			}
		}

		if diff := cmp.Diff([]string{"amount", "currency", "resources"},
			objectKeysAt(t, document, "sourceCost", "currencies", 0)); diff != "" {
			t.Errorf("currency cost keys mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the workloads", func(t *testing.T) {
		t.Parallel()

		workloads := arrayAt(t, document, "workloads")
		if len(workloads) != 3 {
			t.Fatalf("the document has %d workloads, want 3", len(workloads))
		}

		wantKeys := []string{"blockers", "estimate", "name", "noWorkload", "resources", "sourceCost", "summary"}
		if diff := cmp.Diff(wantKeys, objectKeysAt(t, document, "workloads", 0)); diff != "" {
			t.Errorf("workload keys mismatch (-want +got):\n%s", diff)
		}

		tests := []struct {
			name        string
			noWorkload  bool
			resources   float64
			direct      float64
			none        float64
			amount      string
			estimated   float64
			sourceCosts []map[string]any
			blockers    []string
		}{
			{
				name:      "orders",
				direct:    3,
				none:      1,
				amount:    "10.00",
				resources: 4, estimated: 1,
				sourceCosts: []map[string]any{{"currency": "USD", "amount": "1200.00", "resources": float64(1)}},
				blockers:    []string{"queue-jobs"},
			},
			{
				name:      "shop",
				direct:    3,
				amount:    "2525.00",
				resources: 3, estimated: 3,
				sourceCosts: []map[string]any{
					{"currency": "EUR", "amount": "2415.00", "resources": float64(2)},
					{"currency": "USD", "amount": "40.00", "resources": float64(1)},
				},
				blockers: []string{},
			},
			{
				name:       "No workload",
				noWorkload: true,
				direct:     1,
				amount:     "0.00",
				resources:  1, estimated: 0,
				sourceCosts: []map[string]any{{"currency": "USD", "amount": "200.00", "resources": float64(1)}},
				blockers:    []string{},
			},
		}

		for i, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				at := []any{"workloads", i}

				if test.noWorkload {
					if diff := cmp.Diff("", valueAt(t, document, extend(at, "name")...)); diff != "" {
						t.Errorf("name mismatch (-want +got):\n%s", diff)
					}

					if diff := cmp.Diff(true, valueAt(t, document, extend(at, "noWorkload")...)); diff != "" {
						t.Errorf("noWorkload mismatch (-want +got):\n%s", diff)
					}
				} else if diff := cmp.Diff(test.name, valueAt(t, document, extend(at, "name")...)); diff != "" {
					t.Errorf("name mismatch (-want +got):\n%s", diff)
				}

				tests := []struct {
					path []any
					want any
				}{
					{path: extend(at, "resources"), want: test.resources},
					{path: extend(at, "summary", "direct"), want: test.direct},
					{path: extend(at, "summary", "none"), want: test.none},
					{path: extend(at, "estimate", "amount"), want: test.amount},
					{path: extend(at, "estimate", "estimated"), want: test.estimated},
					{path: extend(at, "estimate", "resources"), want: test.resources},
					{path: extend(at, "sourceCost", "resources"), want: test.resources},
				}

				for _, check := range tests {
					if diff := cmp.Diff(check.want, valueAt(t, document, check.path...)); diff != "" {
						t.Errorf("%v mismatch (-want +got):\n%s", check.path, diff)
					}
				}

				// A group without blockers writes [], so the collected list is
				// empty rather than nil.
				blocked := []string{}

				for j := range arrayAt(t, document, extend(at, "blockers")...) {
					id, ok := valueAt(t, document, extend(at, "blockers", j, "resourceId")...).(string)
					if !ok {
						t.Fatalf("%q blockers[%d].resourceId is not a string", test.name, j)
					}

					blocked = append(blocked, id)
				}

				if diff := cmp.Diff(test.blockers, blocked); diff != "" {
					t.Errorf("%q blockers mismatch (-want +got):\n%s", test.name, diff)
				}

				var costs []map[string]any

				for j := range arrayAt(t, document, extend(at, "sourceCost", "currencies")...) {
					cost := extend(at, "sourceCost", "currencies", j)
					costs = append(costs, map[string]any{
						"currency":  valueAt(t, document, extend(cost, "currency")...),
						"amount":    valueAt(t, document, extend(cost, "amount")...),
						"resources": valueAt(t, document, extend(cost, "resources")...),
					})
				}

				if diff := cmp.Diff(test.sourceCosts, costs); diff != "" {
					t.Errorf("%q source costs mismatch (-want +got):\n%s", test.name, diff)
				}
			})
		}
	})

	testutil.Golden(t, "testdata/report-estimates.golden", got)
}

// TestReportEstimateAssumptionFlags pins that an estimated resource says in the
// document itself what the estimate assumed: whether it priced a count the
// resource does not give as one, and whether it priced a resource that gives no
// hoursPerMonth as running 730 (ADR 0025). Both flags are always written on an
// estimated resource, false when nothing was assumed, so a consumer can read
// them without a default.
func TestReportEstimateAssumptionFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		size         *inventory.Size
		usage        *inventory.Usage
		countAssumed bool
		hoursAssumed bool
	}{
		{
			name:  "a count and hours the resource gives",
			size:  &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2), Count: counted(2)},
			usage: &inventory.Usage{HoursPerMonth: sized(730)},
		},
		{
			name:         "no count, so one instance is assumed",
			size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
			usage:        &inventory.Usage{HoursPerMonth: sized(730)},
			countAssumed: true,
		},
		{
			name:         "no hours, so 730 are assumed",
			size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2), Count: counted(2)},
			hoursAssumed: true,
		},
		{
			name:         "neither a count nor hours, so both are assumed",
			size:         &inventory.Size{VCPUs: sized(1), MemoryGiB: sized(2)},
			countAssumed: true,
			hoursAssumed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			inv := newInventory(t, sourceName, []inventory.Resource{{
				ID:       "server-small",
				RawType:  "AWS::EC2::Instance",
				Category: taxonomy.ComputeVM,
				Region:   "eu-west-1",
				Size:     test.size,
				Usage:    test.usage,
			}})

			got := render(t, assessment.AssessWith(inv, testutil.ReportCatalog(t),
				assessment.Options{Estimate: true}), analysis.ReportMeta{InventoryName: inventoryFileName})
			checkDocument(t, got)

			document := decodeJSON(t, got)
			at := resourceIndex(t, document, "server-small")

			if diff := cmp.Diff([]string{"amount", "breakdown", "countAssumed", "estimated", "hoursAssumed"},
				objectKeysAt(t, document, extend(at, "estimate")...)); diff != "" {
				t.Errorf("estimate keys mismatch (-want +got), an estimated resource writes both flags:\n%s", diff)
			}

			checks := []struct {
				path []any
				want any
			}{
				{path: extend(at, "estimate", "countAssumed"), want: test.countAssumed},
				{path: extend(at, "estimate", "hoursAssumed"), want: test.hoursAssumed},
			}

			for _, check := range checks {
				if diff := cmp.Diff(check.want, valueAt(t, document, check.path...)); diff != "" {
					t.Errorf("%v mismatch (-want +got):\n%s", check.path, diff)
				}
			}
		})
	}
}

// TestReportBillingCapHours pins that an estimate priced by plan on an offering
// that bills a plan for at most a number of hours a month says so in the
// document itself (billingCapHours, ADR 0029), and that no other estimate
// writes the key: a consumer that reads it learns of a cap and never of the
// absence of one.
func TestReportBillingCapHours(t *testing.T) {
	t.Parallel()

	got := render(t, assessment.AssessWith(newInventory(t, sourceName, estimateResources()),
		testutil.ReportCappedCatalog(t), assessment.Options{Estimate: true}),
		analysis.ReportMeta{InventoryName: inventoryFileName})
	checkDocument(t, got)

	document := decodeJSON(t, got)

	// server-small and worker-jobs are the compute.vm resources, which this
	// catalog prices by plan on its capped offering.
	for _, id := range []string{"server-small", "worker-jobs"} {
		at := extend(resourceIndex(t, document, id), "estimate")

		if diff := cmp.Diff([]string{"amount", "billingCapHours", "breakdown", "countAssumed", "estimated", "hoursAssumed"},
			objectKeysAt(t, document, at...)); diff != "" {
			t.Errorf("%q estimate keys mismatch (-want +got):\n%s", id, diff)
		}

		if diff := cmp.Diff(float64(testutil.ReportCapHours), valueAt(t, document, extend(at, "billingCapHours")...)); diff != "" {
			t.Errorf("%q billingCapHours mismatch (-want +got):\n%s", id, diff)
		}
	}

	// No other resource's estimate carries the key: bucket-storage is priced by
	// storage, cluster-small is on the fixture's uncapped Kubernetes offering,
	// and server-huge is not estimated at all.
	for _, id := range []string{"bucket-storage", "cluster-small", "server-huge"} {
		keys := objectKeysAt(t, document, extend(resourceIndex(t, document, id), "estimate")...)
		if slices.Contains(keys, "billingCapHours") {
			t.Errorf("%q estimate keys = %q, want none of them billingCapHours", id, keys)
		}
	}
}

// counted is a given int, as an inventory count carries it.
func counted(v int) *int { return &v }

// TestReportNamesTheChosenRegion pins that a document names the region a run
// chose, at the top level, with or without estimates (PRD #190 milestone 8: the
// report header names the region per target), and names none when no region was
// chosen: the estimate basis is not in a document without estimates, so the
// top-level key is all such a document has to say about the region.
func TestReportNamesTheChosenRegion(t *testing.T) {
	t.Parallel()

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	tests := []struct {
		name string
		opts assessment.Options
		// want is the region the document must name, and "" for no region key.
		want string
	}{
		{
			name: "a chosen region without estimates",
			opts: assessment.Options{Region: testutil.ReportSecondRegion},
			want: testutil.ReportSecondRegion,
		},
		{
			name: "no chosen region without estimates",
			opts: assessment.Options{},
			want: "",
		},
		{
			name: "a chosen region with estimates",
			opts: assessment.Options{Region: testutil.ReportSecondRegion, Estimate: true},
			want: testutil.ReportSecondRegion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := assessment.AssessWith(newInventory(t, sourceName, estimateResources()), testutil.ReportCatalog(t), tc.opts)
			got := render(t, a, meta)
			checkDocument(t, got)

			document := decodeJSON(t, got)

			if _, ok := document["region"]; ok != (tc.want != "") {
				t.Errorf("the document holds a region key = %t, want %t: %q",
					ok, tc.want != "", objectKeysAt(t, document))
			}

			if tc.want != "" {
				if diff := cmp.Diff(tc.want, valueAt(t, document, "region")); diff != "" {
					t.Errorf("region mismatch (-want +got):\n%s", diff)
				}
			}

			// The document must stay one the schema file describes: a key it
			// declares no property for fails closed objects.
			s := readSchema(t, filepath.Join("schema", schemaFileFor("assessment", currentSchemaVersion)))

			for _, problem := range s.validate(document, "$") {
				t.Errorf("the document does not match the schema: %s", problem)
			}
		})
	}
}

// TestReportWithoutEstimateKeys pins that a document without estimates holds no
// estimate key at all: no basis, no total, no plan, no amount and no source cost,
// while the relations and the workloads, which are not prices, stay.
func TestReportWithoutEstimateKeys(t *testing.T) {
	t.Parallel()

	scaleway := testutil.ScalewayCatalog(t)
	got := render(t, assessment.AssessWith(newInventory(t, sourceName, awsResources()), scaleway,
		assessment.Options{}), analysis.ReportMeta{InventoryName: inventoryFileName})
	checkDocument(t, got)

	document := decodeJSON(t, got)

	if diff := cmp.Diff(float64(2), valueAt(t, document, "schemaVersion")); diff != "" {
		t.Errorf("schemaVersion mismatch (-want +got):\n%s", diff)
	}

	wantKeys := []string{
		"blockers", "inventoryName", "kind", "resources", "schemaVersion", "source", "summary", "target",
		"workloads",
	}
	if diff := cmp.Diff(wantKeys, objectKeysAt(t, document)); diff != "" {
		t.Errorf("document keys mismatch (-want +got), a document without estimates holds no estimate "+
			"and no source cost:\n%s", diff)
	}

	wantResourceKeys := []string{
		"affectedBy", "bestOffering", "category", "fit", "id", "rawType", "reasons", "region", "relations",
		"requiredChanges", "ties",
	}
	if diff := cmp.Diff(wantResourceKeys, objectKeysAt(t, document, "resources", 0)); diff != "" {
		t.Errorf("resource keys mismatch (-want +got), a resource without estimates holds no plan and "+
			"no estimate:\n%s", diff)
	}

	// Nothing anywhere in the document is a price.
	for _, name := range []string{"estimate", "plan", "amount", "breakdown", "price", "sourceCost", "currency"} {
		if keys := allKeys(t, document); slices.Contains(keys, name) {
			t.Errorf("a document without estimates holds a %q key: %q", name, keys)
		}
	}

	// The workloads still report their fits and blockers.
	workload := objectKeysAt(t, document, "workloads", 0)
	if !slices.Contains(workload, "summary") || !slices.Contains(workload, "blockers") {
		t.Errorf("a workload holds no summary or no blockers: %q", workload)
	}
}

// resourceIndex returns the path of the resource with ID id, failing the test
// when the document holds no such resource.
func resourceIndex(tb testing.TB, document any, id string) []any {
	tb.Helper()

	resources := arrayAt(tb, document, "resources")

	for i := range resources {
		if got := valueAt(tb, document, "resources", i, "id"); got == id {
			return []any{"resources", i}
		}
	}

	tb.Fatalf("the document holds no resource %q", id)

	return nil
}

// allKeys returns every object key of document, once each, sorted: a test can
// assert that a key a document without estimates must not write appears nowhere.
func allKeys(tb testing.TB, document any) []string {
	tb.Helper()

	seen := make(map[string]struct{})

	var walk func(value any)

	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, member := range v {
				seen[key] = struct{}{}

				walk(member)
			}
		case []any:
			for _, member := range v {
				walk(member)
			}
		}
	}

	walk(document)

	return slices.Sorted(maps.Keys(seen))
}

// TestReportIsDeterministicWithEstimates pins that a document with estimates is
// byte-identical however often it is rendered, whatever order the inventory was
// given in.
func TestReportIsDeterministicWithEstimates(t *testing.T) {
	t.Parallel()

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	first := render(t, assessEstimates(t, ""), meta)
	second := render(t, assessEstimates(t, ""), meta)
	if !bytes.Equal(first, second) {
		t.Errorf("two documents of the same assessment differ:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	shuffled := estimateResources()
	slices.Reverse(shuffled)

	reordered := render(t, assessment.AssessWith(newInventory(t, sourceName, shuffled), testutil.ReportCatalog(t),
		assessment.Options{Estimate: true}), meta)
	if !bytes.Equal(first, reordered) {
		t.Errorf("the document depends on the order resources were given in:\nfirst:\n%s\nreordered:\n%s",
			first, reordered)
	}
}
