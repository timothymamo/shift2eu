package jsonreport_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

// The keys the comparison fixtures select their targets with: the CLI's keys,
// lower case and one word, not the catalogs' display names.
const (
	ovhcloudKey = "ovhcloud"
	scalewayKey = "scaleway"
	upcloudKey  = "upcloud"
)

// comparisonResources is the inventory the three-target document assesses, chosen
// so the comparison has to tell four things apart:
//
//   - bucket-example is direct on OVHcloud and Scaleway and partial on UpCloud,
//     whose object storage lacks versioning;
//   - db-example is direct on OVHcloud and Scaleway and blocked on UpCloud only,
//     which has no database offering;
//   - fn-example and misc-example are blocked on every target, one for a category
//     no target serves and one for being unclassified, so they are the shared
//     blockers;
//   - partial-example is partial on every target.
//
// The IDs sort into the order the resources must be written in, which is also the
// order the shared blockers must be listed in.
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

// twoTargetResources is the inventory the no-shared-blocker document assesses:
// OVHcloud and Scaleway both serve everything in it. It is the Markdown
// reporter's two-target inventory, db-example included, so both reporters assess
// the same resources.
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

// comparisonInventory is the three-target document's inventory.
func comparisonInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	return newInventory(t, sourceName, comparisonResources())
}

// comparisonFixtureCatalog returns the fixture catalog of one target key. Each
// catalog has its own target display name, source URL and retrieval date, so the
// document has to keep three targets apart.
func comparisonFixtureCatalog(t *testing.T, key string) catalog.Catalog {
	t.Helper()

	switch key {
	case ovhcloudKey:
		return testutil.OVHcloudCatalog(t)
	case scalewayKey:
		return testutil.ScalewayCatalog(t)
	case upcloudKey:
		return testutil.UpCloudCatalog(t)
	default:
		t.Fatalf("no fixture catalog for target key %q", key)

		return catalog.Catalog{}
	}
}

// assessTargets assesses inv against the fixture catalog of each key, in the
// order given, and returns the target assessments Compare takes.
func assessTargets(t *testing.T, inv inventory.Inventory, keys []string) []assessment.TargetAssessment {
	t.Helper()

	targets := make([]assessment.TargetAssessment, 0, len(keys))
	for _, key := range keys {
		targets = append(targets, assessment.TargetAssessment{
			Key:        key,
			Assessment: assessment.Assess(inv, comparisonFixtureCatalog(t, key)),
		})
	}

	return targets
}

// threeTargetKeys is the target list the three-target document compares.
func threeTargetKeys() []string {
	return []string{ovhcloudKey, scalewayKey, upcloudKey}
}

// renderComparison writes the comparison document of c and meta and returns it.
func renderComparison(tb testing.TB, c assessment.Comparison, meta analysis.ReportMeta) []byte {
	tb.Helper()

	var out bytes.Buffer
	if err := jsonreport.New().ReportComparison(tb.Context(), &out, c, meta); err != nil {
		tb.Fatalf("ReportComparison() returned unexpected error: %v", err)
	}

	return out.Bytes()
}

// TestReportComparison pins the whole comparison document against the fixture
// catalogs: the version and kind, the metadata, one entry per target in the
// comparison's order with that target's own heading, summary, resources and
// blockers, and the blockers every target shares.
func TestReportComparison(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))

	got := renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName})
	testutil.Golden(t, "testdata/comparison-three.golden", got)
	checkDocument(t, got)

	document := decodeJSON(t, got)

	if diff := cmp.Diff(float64(currentSchemaVersion), valueAt(t, document, "schemaVersion")); diff != "" {
		t.Errorf("schemaVersion mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff("comparison", valueAt(t, document, "kind")); diff != "" {
		t.Errorf("kind mismatch (-want +got):\n%s", diff)
	}

	// The targets keep the comparison's order, which Compare sorts by key.
	if diff := cmp.Diff(comparisonTargetKeys(c), documentTargetKeys(t, document)); diff != "" {
		t.Errorf("targets mismatch (-want +got):\n%s", diff)
	}

	// The shared blockers are the two resources blocked on every target, in
	// resource ID order, each naming every target's reasons.
	wantShared := []string{"fn-example", "misc-example"}

	shared := arrayAt(t, document, "sharedBlockers")
	if len(shared) != len(wantShared) {
		t.Fatalf("the document has %d shared blockers, want %d", len(shared), len(wantShared))
	}

	for i, resourceID := range wantShared {
		at := []any{"sharedBlockers", i}

		if diff := cmp.Diff(resourceID, valueAt(t, document, extend(at, "resourceId")...)); diff != "" {
			t.Errorf("sharedBlockers[%d].resourceId mismatch (-want +got):\n%s", i, diff)
		}

		var keys []string

		for j := range arrayAt(t, document, extend(at, "targets")...) {
			key, ok := valueAt(t, document, extend(at, "targets", j, "key")...).(string)
			if !ok {
				t.Fatalf("sharedBlockers[%d].targets[%d].key is not a string", i, j)
			}

			keys = append(keys, key)
		}

		if diff := cmp.Diff(threeTargetKeys(), keys); diff != "" {
			t.Errorf("%q shared blocker targets mismatch (-want +got):\n%s", resourceID, diff)
		}
	}
}

// TestReportComparisonShape pins the comparison document's schema and content
// without a golden file: the key set of every object (the schemas allow no other
// key), each target entry holding that target's own assessment in that
// assessment's order, and the shared blockers.
func TestReportComparisonShape(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))
	document := decodeJSON(t, renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName}))

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
					"inventoryName", "kind", "schemaVersion", "sharedBlockers", "source", "targets",
				},
			},
			{
				name: "a target entry",
				path: []any{"targets", 0},
				want: []string{"blockers", "key", "resources", "summary", "target", "workloads"},
			},
			{name: "a target's target", path: []any{"targets", 0, "target"}, want: []string{"name", "snapshot"}},
			{
				name: "a target's snapshot",
				path: []any{"targets", 0, "target", "snapshot"},
				want: []string{"retrieved", "source"},
			},
			{
				name: "a target's summary",
				path: []any{"targets", 0, "summary"},
				want: []string{"direct", "none", "partial", "total"},
			},
			{
				name: "a target's resource",
				path: []any{"targets", 0, "resources", 0},
				want: []string{
					"affectedBy", "bestOffering", "category", "fit", "id", "rawType", "reasons", "region",
					"relations", "requiredChanges", "ties",
				},
			},
			{
				name: "a target's blocker",
				path: []any{"targets", 0, "blockers", 0},
				want: []string{"reason", "resourceId"},
			},
			{
				name: "a shared blocker",
				path: []any{"sharedBlockers", 0},
				want: []string{"resourceId", "targets"},
			},
			{
				name: "a shared blocker's target",
				path: []any{"sharedBlockers", 0, "targets", 0},
				want: []string{"key", "reasons"},
			},
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

	t.Run("each target holds its own assessment", func(t *testing.T) {
		t.Parallel()

		targets := arrayAt(t, document, "targets")
		if len(targets) != len(c.Targets) {
			t.Fatalf("the document has %d targets, the comparison has %d", len(targets), len(c.Targets))
		}

		for i, target := range c.Targets {
			at := []any{"targets", i}

			if got := valueAt(t, document, extend(at, "key")...); got != target.Key {
				t.Errorf("targets[%d].key = %v, want %q", i, got, target.Key)
			}

			if got := valueAt(t, document, extend(at, "target", "name")...); got != target.Assessment.Target {
				t.Errorf("%q target name = %v, want %q", target.Key, got, target.Assessment.Target)
			}

			if got := valueAt(t, document, extend(at, "target", "snapshot", "source")...); got !=
				target.Assessment.Snapshot.SourceURL {
				t.Errorf("%q snapshot source = %v, want %q", target.Key, got, target.Assessment.Snapshot.SourceURL)
			}

			wantDate := snapshotDate(target.Assessment.Snapshot)
			if got := valueAt(t, document, extend(at, "target", "snapshot", "retrieved")...); got != wantDate {
				t.Errorf("%q snapshot retrieved = %v, want %q", target.Key, got, wantDate)
			}

			// The resources keep that target's verdict order.
			resources := arrayAt(t, document, extend(at, "resources")...)
			if len(resources) != len(target.Assessment.Verdicts) {
				t.Fatalf("%q holds %d resources, its assessment has %d verdicts",
					target.Key, len(resources), len(target.Assessment.Verdicts))
			}

			var ids []string

			for j := range resources {
				id, ok := valueAt(t, document, extend(at, "resources", j, "id")...).(string)
				if !ok {
					t.Fatalf("%q resources[%d].id is not a string", target.Key, j)
				}

				if want := target.Assessment.Verdicts[j].Resource.ID; id != want {
					t.Errorf("%q resources[%d].id = %q, want its verdict %q", target.Key, j, id, want)
				}

				ids = append(ids, id)
			}

			wantIDs := make([]string, 0, len(target.Assessment.Verdicts))
			for _, v := range target.Assessment.Verdicts {
				wantIDs = append(wantIDs, v.Resource.ID)
			}

			if diff := cmp.Diff(wantIDs, ids); diff != "" {
				t.Errorf("%q resource order mismatch (-want +got):\n%s", target.Key, diff)
			}
		}
	})

	t.Run("shared blocker reasons", func(t *testing.T) {
		t.Parallel()

		// The format writes every target's reasons for a shared blocker; the
		// reason object is the single document's reason object, so its text names
		// the catalog of the target it belongs to, not one catalog for them all,
		// as the Markdown comparison's comparison-three.golden shows.
		keys := threeTargetKeys()

		wantText := func(resourceID, key string) string {
			if resourceID == "misc-example" {
				return "not classified"
			}

			return "no offering in the " + comparisonFixtureCatalog(t, key).Target() +
				" catalog for serverless.function"
		}

		shared := arrayAt(t, document, "sharedBlockers")
		if len(shared) != 2 {
			t.Fatalf("the document has %d shared blockers, want 2", len(shared))
		}

		for i := range shared {
			at := []any{"sharedBlockers", i}

			resourceID, ok := valueAt(t, document, extend(at, "resourceId")...).(string)
			if !ok {
				t.Fatalf("sharedBlockers[%d].resourceId is not a string", i)
			}

			targets := arrayAt(t, document, extend(at, "targets")...)
			if len(targets) != len(keys) {
				t.Fatalf("%q shared blocker holds %d targets, want %d", resourceID, len(targets), len(keys))
			}

			for j, key := range keys {
				if got := valueAt(t, document, extend(at, "targets", j, "key")...); got != key {
					t.Errorf("%q shared blocker targets[%d].key = %v, want %q", resourceID, j, got, key)
				}

				if got := valueAt(t, document, extend(at, "targets", j, "reasons", 0, "code")...); got == "" {
					t.Errorf("%q shared blocker reason for target %q has no code", resourceID, key)
				}

				want := wantText(resourceID, key)
				if got := valueAt(t, document, extend(at, "targets", j, "reasons", 0, "text")...); got != want {
					t.Errorf("%q shared blocker reason text for target %q = %v, want %q",
						resourceID, key, got, want)
				}
			}
		}
	})
}

// TestReportComparisonNoSharedBlocker pins that a comparison without a resource
// blocked on every target writes an empty sharedBlocker array, never null.
func TestReportComparisonNoSharedBlocker(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(assessTargets(t, newInventory(t, sourceName, twoTargetResources()),
		[]string{ovhcloudKey, scalewayKey}))

	if len(c.SharedBlockers) != 0 {
		t.Fatalf("the fixture has %d shared blockers, want none", len(c.SharedBlockers))
	}

	document := decodeJSON(t, renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName}))

	shared, ok := valueAt(t, document, "sharedBlockers").([]any)
	if !ok {
		t.Errorf("sharedBlockers is not an array; an array is never null")
	}

	if len(shared) != 0 {
		t.Errorf("sharedBlockers holds %d entries, want an empty array", len(shared))
	}

	if got := valueAt(t, document, "kind"); got != "comparison" {
		t.Errorf("kind = %v, want %q", got, "comparison")
	}
}

// TestReportComparisonRefusesMalformedComparison pins that the document is
// written only for a comparison the domain accepts: the targets must assess the
// same resources in the same order, and every shared blocker must name only
// compared targets. A document written from such a comparison would hold a
// resource set the reader cannot line up across targets, or a blocker naming a
// target the document does not hold. Each is an error, and nothing is written.
func TestReportComparisonRefusesMalformedComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(t *testing.T, c *assessment.Comparison)
	}{
		{
			name: "the targets assess the same resources in another order",
			change: func(_ *testing.T, c *assessment.Comparison) {
				c.Targets[1].Assessment.Verdicts[0], c.Targets[1].Assessment.Verdicts[1] =
					c.Targets[1].Assessment.Verdicts[1], c.Targets[1].Assessment.Verdicts[0]
			},
		},
		{
			name: "a later target holds a verdict the first target lacks",
			change: func(t *testing.T, c *assessment.Comparison) {
				t.Helper()

				c.Targets[1].Assessment.Verdicts = append(slices.Clone(c.Targets[1].Assessment.Verdicts),
					assessment.Verdict{
						Resource: inventory.Resource{
							ID:       "extra-example",
							RawType:  "AWS::SQS::Queue",
							Category: taxonomy.Unclassified,
						},
						Fit:     mapping.None,
						Reasons: []mapping.Reason{{Code: mapping.NotClassified}},
					})
			},
		},
		{
			name: "a shared blocker names a target the comparison does not hold",
			change: func(t *testing.T, c *assessment.Comparison) {
				t.Helper()

				if len(c.SharedBlockers) == 0 {
					t.Fatal("precondition failed: the fixture comparison has no shared blocker")
				}

				c.SharedBlockers[0].Targets = append(c.SharedBlockers[0].Targets, assessment.TargetReasons{
					Key:     "missing",
					Reasons: []mapping.Reason{{Code: mapping.NotClassified}},
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))
			tc.change(t, &c)

			var out bytes.Buffer

			err := jsonreport.New().ReportComparison(t.Context(), &out, c,
				analysis.ReportMeta{InventoryName: inventoryFileName})
			if err == nil {
				t.Fatalf("ReportComparison() error = nil, want an error when %s", tc.name)
			}

			if out.Len() != 0 {
				t.Errorf("ReportComparison() wrote %d bytes to the writer, want none:\n%s", out.Len(), out.String())
			}
		})
	}
}

// TestReportComparisonNoTarget pins that a comparison of no target is an error a
// renderer reports, not an empty document: ReportComparison writes nothing and
// the caller decides what to show instead.
func TestReportComparisonNoTarget(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := jsonreport.New().ReportComparison(t.Context(), &out, assessment.Comparison{},
		analysis.ReportMeta{InventoryName: inventoryFileName})
	if err == nil {
		t.Fatalf("ReportComparison() error = nil for a comparison of no target, want an error")
	}

	if out.Len() != 0 {
		t.Errorf("ReportComparison() wrote %d bytes to the writer, want none:\n%s", out.Len(), out.String())
	}
}

// TestReportComparisonReturnsWriteError pins that a failing writer is reported to
// the caller, wrapped or as it came, never ignored: the driving adapter decides
// what to do with a partial document.
func TestReportComparisonReturnsWriteError(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("disk is full")

	c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))

	err := jsonreport.New().ReportComparison(t.Context(), failingWriter{err: writeErr}, c,
		analysis.ReportMeta{InventoryName: inventoryFileName})
	if !errors.Is(err, writeErr) {
		t.Fatalf("ReportComparison() error = %v, want an error wrapping %v", err, writeErr)
	}
}

// hostileComparisonKeys are the hostile comparison's target keys: the document
// writes each one as targets[].key and as a shared blocker target's key, so a key
// is a value like any other. The first also carries a paragraph separator and an
// ampersand, the characters JSON escapes besides < and >.
func hostileComparisonKeys() []string {
	return []string{
		"a\"\\\n\t\x00\x1b\u2028\u2029</script>&\xff",
		"k\"ey\x00\x1b\u2028</script>\xff",
		"z<z|`\n\x1b\xff",
	}
}

// hostileComparisonTargets builds the three target assessments of the hostile
// document. Each target has the same hostile display name and snapshot source and
// its own retrieval date; one resource is blocked on every target for a category
// no target serves, one is blocked on every target for being unclassified, and
// one is direct, partial and none on the three targets.
func hostileComparisonTargets() []assessment.TargetAssessment {
	keys := hostileComparisonKeys()
	retrieved := []time.Time{
		time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC),
	}

	targets := make([]assessment.TargetAssessment, 0, len(keys))

	for i, key := range keys {
		a := assessment.Assessment{
			Target:   hostileTarget,
			Snapshot: catalog.Snapshot{SourceURL: hostileSourceURL, RetrievedOn: retrieved[i]},
			Source:   hostileSource,
			Verdicts: []assessment.Verdict{
				hostileNoOfferingVerdict(),
				hostileUnclassifiedVerdict(),
				hostileVaryingVerdict(i),
			},
		}

		for _, v := range a.Verdicts {
			switch v.Fit {
			case mapping.Direct:
				a.Summary.Direct++
			case mapping.Partial:
				a.Summary.Partial++
			case mapping.None:
				a.Summary.None++

				for _, reason := range v.Reasons {
					a.Blockers = append(a.Blockers, assessment.Blocker{ResourceID: v.Resource.ID, Reason: reason})
				}
			}
		}

		a.Summary.Total = len(a.Verdicts)

		targets = append(targets, assessment.TargetAssessment{Key: key, Assessment: a})
	}

	return targets
}

// hostileNoOfferingVerdict is the verdict of the resource blocked on every target
// for a category no target serves.
func hostileNoOfferingVerdict() assessment.Verdict {
	category := hostileCategory()

	return assessment.Verdict{
		Resource: inventory.Resource{
			ID:       hostileID,
			RawType:  hostileRawType,
			Category: category,
			Region:   hostileRegion,
		},
		Fit:     mapping.None,
		Reasons: []mapping.Reason{{Code: mapping.NoOffering, Target: hostileTarget, Category: category}},
	}
}

// hostileUnclassifiedVerdict is the verdict of the second resource blocked on
// every target.
func hostileUnclassifiedVerdict() assessment.Verdict {
	return assessment.Verdict{
		Resource: inventory.Resource{
			ID:       hostileBlockedID,
			RawType:  hostileRawType,
			Category: hostileCategory(),
			Region:   hostileRegion,
		},
		Fit:     mapping.None,
		Reasons: []mapping.Reason{{Code: mapping.NotClassified}},
	}
}

// hostileVaryingVerdict is the verdict of the resource that is direct on the
// first target, partial on the second and none on the third, so the document
// holds a hostile offering, a hostile missing capability and a hostile literal
// reason.
func hostileVaryingVerdict(target int) assessment.Verdict {
	resource := inventory.Resource{
		ID:       hostileID + "-v",
		RawType:  hostileRawType,
		Category: taxonomy.ComputeVM,
		Region:   hostileRegion,
	}

	switch target {
	case 0:
		return assessment.Verdict{
			Resource:     resource,
			Fit:          mapping.Direct,
			BestOffering: hostileOffering,
			Candidates: []mapping.Candidate{{
				Offering: catalog.Offering{Name: hostileOffering, Categories: []taxonomy.Category{taxonomy.ComputeVM}},
				Fit:      mapping.Direct,
			}},
			Reasons: []mapping.Reason{{Code: mapping.AllCapabilities, Offering: hostileOffering}},
		}
	case 1:
		return assessment.Verdict{
			Resource:     resource,
			Fit:          mapping.Partial,
			BestOffering: hostileOffering,
			Reasons: []mapping.Reason{{
				Code:       mapping.MissingCapability,
				Offering:   hostileOffering,
				Capability: hostileCapability,
			}},
			RequiredChanges: []taxonomy.Capability{hostileCapability},
		}
	default:
		return assessment.Verdict{
			Resource: resource,
			Fit:      mapping.None,
			Reasons:  []mapping.Reason{{Code: hostileReasonCode}},
		}
	}
}

// TestReportComparisonHostileValues pins that no value reaches the comparison
// document unencoded: the output is valid JSON and valid UTF-8, and decoding it
// gives every value back, with each invalid UTF-8 byte replaced by U+FFFD.
func TestReportComparisonHostileValues(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(hostileComparisonTargets())
	got := renderComparison(t, c, analysis.ReportMeta{InventoryName: hostileInventoryName})

	testutil.Golden(t, "testdata/comparison-hostile.golden", got)
	checkDocument(t, got)
	checkJSONEscapes(t, got)

	document := decodeJSON(t, got)

	keys := documentTargetKeys(t, document)
	if len(keys) != len(c.Targets) {
		t.Fatalf("the document has %d targets, the comparison has %d", len(keys), len(c.Targets))
	}

	for i, key := range keys {
		if want := strings.ToValidUTF8(c.Targets[i].Key, "\uFFFD"); key != want {
			t.Errorf("targets[%d].key = %q, want %q", i, key, want)
		}
	}

	tests := []struct {
		name string
		path []any
		want string
	}{
		{name: "inventory name", path: []any{"inventoryName"}, want: hostileInventoryName},
		{name: "source", path: []any{"source"}, want: hostileSource},
		{name: "target name", path: []any{"targets", 0, "target", "name"}, want: hostileTarget},
		{
			name: "snapshot source",
			path: []any{"targets", 0, "target", "snapshot", "source"},
			want: hostileSourceURL,
		},
		{name: "snapshot retrieved", path: []any{"targets", 1, "target", "snapshot", "retrieved"}, want: "2026-11-01"},
		{name: "blocked resource ID", path: []any{"targets", 0, "resources", 0, "id"}, want: hostileID},
		{
			name: "blocked resource raw type",
			path: []any{"targets", 0, "resources", 0, "rawType"},
			want: hostileRawType,
		},
		{
			name: "no-offering reason text",
			path: []any{"targets", 0, "resources", 0, "reasons", 0, "text"},
			want: "no offering in the " + hostileTarget + " catalog for " + string(hostileCategory()),
		},
		{
			name: "unknown reason text is the code",
			path: []any{"targets", 2, "resources", 2, "reasons", 0, "text"},
			want: string(hostileReasonCode),
		},
		{
			name: "required change",
			path: []any{"targets", 1, "resources", 2, "requiredChanges", 0},
			want: string(hostileCapability),
		},
		// The shared blockers are in resource ID order: the unclassified one
		// first, then the one blocked for a category no target serves.
		{
			name: "shared blocker resource ID",
			path: []any{"sharedBlockers", 0, "resourceId"},
			want: hostileBlockedID,
		},
		{
			name: "shared blocker reason text",
			path: []any{"sharedBlockers", 0, "targets", 0, "reasons", 0, "text"},
			want: "not classified",
		},
		{
			name: "shared blocker target key",
			path: []any{"sharedBlockers", 1, "targets", 2, "key"},
			want: hostileComparisonKeys()[2],
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

// TestReportComparisonIsDeterministic pins that the same comparison always
// renders the same bytes, and that the order the targets were assessed in does
// not reach the document: Compare sorts them by key.
func TestReportComparisonIsDeterministic(t *testing.T) {
	t.Parallel()

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	first := renderComparison(t, assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys())), meta)
	second := renderComparison(t, assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys())), meta)

	if !bytes.Equal(first, second) {
		t.Errorf("two documents of the same comparison differ:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	reversed := threeTargetKeys()
	slices.Reverse(reversed)

	reordered := renderComparison(t, assessment.Compare(assessTargets(t, comparisonInventory(t), reversed)), meta)
	if !bytes.Equal(first, reordered) {
		t.Errorf("the document depends on the order the targets were given in:\nfirst:\n%s\nreordered:\n%s",
			first, reordered)
	}
}

// comparisonTargetKeys returns the target keys of c, in the comparison's order.
func comparisonTargetKeys(c assessment.Comparison) []string {
	keys := make([]string, 0, len(c.Targets))
	for _, target := range c.Targets {
		keys = append(keys, target.Key)
	}

	return keys
}

// documentTargetKeys returns the target keys the comparison document writes, in
// document order.
func documentTargetKeys(t *testing.T, document any) []string {
	t.Helper()

	var keys []string

	for i := range arrayAt(t, document, "targets") {
		key, ok := valueAt(t, document, "targets", i, "key").(string)
		if !ok {
			t.Fatalf("targets[%d].key is not a string", i)
		}

		keys = append(keys, key)
	}

	return keys
}

// snapshotDate is the date the document writes for a snapshot, as the Markdown
// report prints it: the retrieval date in UTC, YYYY-MM-DD.

// The two targets the estimate comparison compares: the report fixture's
// invented prices, with display names, snapshots and default regions that
// differ, so the document has to keep two regions and two estimates apart.
const (
	reportTwoTarget      = "Example Cloud North"
	reportTwoSourceURL   = "https://www.example.com/pricing/north/"
	reportTwoRetrievedOn = "2026-11-14"
	reportKey            = "example"
	reportNorthKey       = "example-north"
)

// reportTwoSnapshot is the north target's snapshot: the same invented catalog
// taken a day later, whose estimates are for the report fixture's second region,
// where its compute plans cost a fifth more.
func reportTwoSnapshot() catalog.Snapshot {
	return catalog.Snapshot{
		SourceURL:     reportTwoSourceURL,
		RetrievedOn:   time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
		DefaultRegion: testutil.ReportSecondRegion,
	}
}

// reportComparisonCatalog returns the priced fixture catalog of one target key.
func reportComparisonCatalog(t *testing.T, key string) catalog.Catalog {
	t.Helper()

	switch key {
	case reportKey:
		return testutil.ReportCatalog(t)
	case reportNorthKey:
		return testutil.ReportCatalogIn(t, reportTwoTarget, reportTwoSnapshot())
	default:
		t.Fatalf("no priced fixture catalog for target key %q", key)

		return catalog.Catalog{}
	}
}

// assessReportTargets assesses the estimate fixture with estimates on against
// each priced fixture catalog, in the order given.
func assessReportTargets(t *testing.T, keys []string) []assessment.TargetAssessment {
	t.Helper()

	inv := newInventory(t, sourceName, estimateResources())

	targets := make([]assessment.TargetAssessment, 0, len(keys))
	for _, key := range keys {
		targets = append(targets, assessment.TargetAssessment{
			Key: key,
			Assessment: assessment.AssessWith(inv, reportComparisonCatalog(t, key),
				assessment.Options{Estimate: true}),
		})
	}

	return targets
}

// TestReportComparisonNamesTheChosenRegions pins that every target entry of a
// comparison names the region a run chose for that target, with or without
// estimates, exactly as a single-target document does at its top level (PRD #190
// milestone 8).
func TestReportComparisonNamesTheChosenRegions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// region is the region chosen for the example target; the north target
		// always chooses its own default region.
		region   string
		estimate bool
	}{
		{name: "without estimates", region: testutil.ReportSecondRegion},
		{name: "with estimates", region: testutil.ReportSecondRegion, estimate: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inv := newInventory(t, sourceName, estimateResources())
			chosen := []string{tc.region, testutil.ReportSecondRegion}

			targets := make([]assessment.TargetAssessment, 0, len(chosen))

			for i, key := range []string{reportKey, reportNorthKey} {
				targets = append(targets, assessment.TargetAssessment{
					Key: key,
					Assessment: assessment.AssessWith(inv, reportComparisonCatalog(t, key),
						assessment.Options{Region: chosen[i], Estimate: tc.estimate}),
				})
			}

			got := renderComparison(t, assessment.Compare(targets), analysis.ReportMeta{InventoryName: inventoryFileName})
			checkDocument(t, got)

			document := decodeJSON(t, got)

			for i, region := range chosen {
				at := []any{"targets", i}

				if diff := cmp.Diff(region, valueAt(t, document, extend(at, "region")...)); diff != "" {
					t.Errorf("targets[%d].region mismatch (-want +got):\n%s", i, diff)
				}
			}

			s := readSchema(t, filepath.Join("schema", schemaFileFor("comparison", currentSchemaVersion)))

			for _, problem := range s.validate(document, "$") {
				t.Errorf("the document does not match the schema: %s", problem)
			}
		})
	}
}

// TestReportComparisonWithEstimates pins the schema version 2 comparison of two
// priced targets: every target entry carries its own estimate basis and total,
// its resources with their plans and estimates, its workloads and its source
// cost, and the two regions the estimates are for are kept apart.
func TestReportComparisonWithEstimates(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(assessReportTargets(t, []string{reportKey, reportNorthKey}))
	got := renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName})
	checkDocument(t, got)

	document := decodeJSON(t, got)

	if diff := cmp.Diff(float64(2), valueAt(t, document, "schemaVersion")); diff != "" {
		t.Errorf("schemaVersion mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff("comparison", valueAt(t, document, "kind")); diff != "" {
		t.Errorf("kind mismatch (-want +got):\n%s", diff)
	}

	wantDocumentKeys := []string{"inventoryName", "kind", "schemaVersion", "sharedBlockers", "source", "targets"}
	if diff := cmp.Diff(wantDocumentKeys, objectKeysAt(t, document)); diff != "" {
		t.Errorf("document keys mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff([]string{"blockers", "estimate", "key", "resources", "sourceCost", "summary", "target", "workloads"},
		objectKeysAt(t, document, "targets", 0)); diff != "" {
		t.Errorf("target entry keys mismatch (-want +got):\n%s", diff)
	}

	// The one resource blocked on both targets is still a shared blocker.
	shared := arrayAt(t, document, "sharedBlockers")
	if len(shared) != 1 || valueAt(t, document, "sharedBlockers", 0, "resourceId") != "queue-jobs" {
		t.Errorf("sharedBlockers = %v, want the one blocker on queue-jobs", shared)
	}

	tests := []struct {
		key        string
		region     string
		priceDate  string
		total      string
		workerCost string
		workload   string
	}{
		{
			key:        "example",
			region:     "xx-par",
			priceDate:  reportRetrievedOn,
			total:      "2535.00",
			workerCost: "10.00",
			workload:   "10.00",
		},
		{
			key:        "example-north",
			region:     testutil.ReportSecondRegion,
			priceDate:  reportTwoRetrievedOn,
			total:      "2539.00",
			workerCost: "12.00",
			workload:   "12.00",
		},
	}

	for i, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			t.Parallel()

			at := []any{"targets", i}

			if diff := cmp.Diff(test.key, valueAt(t, document, extend(at, "key")...)); diff != "" {
				t.Errorf("targets[%d].key mismatch (-want +got):\n%s", i, diff)
			}

			tests := []struct {
				path []any
				want any
			}{
				{path: extend(at, "estimate", "region"), want: test.region},
				{path: extend(at, "estimate", "regionChosen"), want: false},
				{path: extend(at, "estimate", "priceDate"), want: test.priceDate},
				{path: extend(at, "estimate", "currency"), want: "EUR"},
				{path: extend(at, "estimate", "hoursBasis"), want: float64(730)},
				{path: extend(at, "estimate", "total", "amount"), want: test.total},
				{path: extend(at, "estimate", "total", "estimated"), want: float64(4)},
				{path: extend(at, "estimate", "total", "resources"), want: float64(8)},
				{path: extend(at, "sourceCost", "covered"), want: float64(5)},
			}

			for _, check := range tests {
				if diff := cmp.Diff(check.want, valueAt(t, document, check.path...)); diff != "" {
					t.Errorf("%v mismatch (-want +got):\n%s", check.path, diff)
				}
			}

			// Every target carries its own estimates: the same resource costs the
			// target's own price, and the first workload's total is that target's.
			resource := resourceIndexAt(t, document, at, "worker-jobs")
			if diff := cmp.Diff(test.workerCost, valueAt(t, document, extend(resource, "estimate", "amount")...)); diff != "" {
				t.Errorf("%q worker-jobs estimate mismatch (-want +got):\n%s", test.key, diff)
			}

			if diff := cmp.Diff(test.workload, valueAt(t, document, extend(at, "workloads", 0, "estimate", "amount")...)); diff != "" {
				t.Errorf("%q first workload estimate mismatch (-want +got):\n%s", test.key, diff)
			}
		})
	}

	testutil.Golden(t, "testdata/comparison-estimates.golden", got)
}

// TestReportComparisonBillingCapHours pins that a cap belongs to a target's
// estimate in a comparison document too (billingCapHours, ADR 0029): the capped
// target's plan-priced estimates carry it, the uncapped target's do not, and the
// whole document matches the comparison schema, which declares the key.
func TestReportComparisonBillingCapHours(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, sourceName, estimateResources())

	targets := []assessment.TargetAssessment{
		{
			Key:        reportKey,
			Assessment: assessment.AssessWith(inv, testutil.ReportCatalog(t), assessment.Options{Estimate: true}),
		},
		{
			Key: reportNorthKey,
			Assessment: assessment.AssessWith(inv,
				testutil.ReportCappedCatalogIn(t, reportTwoTarget, reportTwoSnapshot()),
				assessment.Options{Estimate: true}),
		},
	}

	got := renderComparison(t, assessment.Compare(targets), analysis.ReportMeta{InventoryName: inventoryFileName})
	checkDocument(t, got)

	document := decodeJSON(t, got)

	// Compare sorts the targets by key, so example comes before example-north.
	tests := []struct {
		key  string
		want bool
	}{
		{key: reportKey, want: false},
		{key: reportNorthKey, want: true},
	}

	for i, test := range tests {
		at := extend(resourceIndexAt(t, document, []any{"targets", i}, "server-small"), "estimate")

		if !test.want {
			if keys := objectKeysAt(t, document, at...); slices.Contains(keys, "billingCapHours") {
				t.Errorf("%q estimate keys = %q, want none of them billingCapHours", test.key, keys)
			}

			continue
		}

		if diff := cmp.Diff(float64(testutil.ReportCapHours), valueAt(t, document, extend(at, "billingCapHours")...)); diff != "" {
			t.Errorf("%q billingCapHours mismatch (-want +got):\n%s", test.key, diff)
		}
	}

	s := readSchema(t, filepath.Join("schema", schemaFileFor("comparison", currentSchemaVersion)))

	for _, problem := range s.validate(document, "$") {
		t.Errorf("the document does not match the schema: %s", problem)
	}
}

// resourceIndexAt returns the path of the resource with ID id inside the target
// at path, failing the test when that target holds no such resource.
func resourceIndexAt(tb testing.TB, document any, path []any, id string) []any {
	tb.Helper()

	resources := arrayAt(tb, document, extend(path, "resources")...)

	for i := range resources {
		if got := valueAt(tb, document, extend(path, "resources", i, "id")...); got == id {
			return extend(path, "resources", i)
		}
	}

	tb.Fatalf("%v holds no resource %q", path, id)

	return nil
}
