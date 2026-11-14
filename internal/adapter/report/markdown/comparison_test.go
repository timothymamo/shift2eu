package markdown_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The reporter must be usable wherever the comparison use case expects a
// ComparisonReporter: the golden files pin its output, this pins the shape of
// the port it renders. It is a second port (docs/adr/0016), so a reporter may
// implement Reporter without implementing this one.
var _ analysis.ComparisonReporter = markdown.New()

// The keys the comparison fixtures select their targets with: the CLI's keys,
// lower case and one word, not the catalogs' display names.
const (
	ovhcloudKey = "ovhcloud"
	scalewayKey = "scaleway"
	upcloudKey  = "upcloud"
)

// comparisonResources is the inventory the three-target golden assesses, chosen
// so the comparison has to tell four things apart:
//
//   - bucket-example is direct on OVHcloud and Scaleway and partial on UpCloud,
//     whose object storage lacks versioning;
//   - db-example is direct on OVHcloud and Scaleway, with a tie on Scaleway, and
//     blocked on UpCloud only, which has no database offering;
//   - fn-example and misc-example are blocked on every target, one for a category
//     no target serves and one for being unclassified, so they are the shared
//     blockers;
//   - partial-example is partial on every target.
//
// The IDs sort into the order the tables must use, which is also the order the
// shared blockers must be listed in.
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

// twoTargetResources is the inventory the two-target golden assesses: OVHcloud
// and Scaleway both serve everything in it, so the comparison has no blocker on
// every target and its shared-blocker section says "None.". It holds db-example
// so the Scaleway section keeps a tie note: both of that target's database
// offerings offer every capability the resource uses, which is the kind of tie
// ADR 0023 leaves after bare metal and virtual machines were split into separate
// categories.
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

// comparisonInventory is the three-target golden's inventory.
func comparisonInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	return newInventory(t, sourceName, comparisonResources())
}

// comparisonFixtureCatalog returns the fixture catalog of one target key. Each
// catalog has its own target display name, source URL and retrieval date, so the
// report has to keep three targets apart.
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

// threeTargetKeys is the target list the three-target golden compares.
func threeTargetKeys() []string {
	return []string{ovhcloudKey, scalewayKey, upcloudKey}
}

// renderComparison writes the comparison report of c and meta and returns it.
func renderComparison(tb testing.TB, c assessment.Comparison, meta analysis.ReportMeta) []byte {
	tb.Helper()

	var out bytes.Buffer
	if err := markdown.New().ReportComparison(tb.Context(), &out, c, meta); err != nil {
		tb.Fatalf("ReportComparison() returned unexpected error: %v", err)
	}

	return out.Bytes()
}

// TestReportComparison pins the whole comparison against the fixture catalogs:
// the title naming every target, one catalog line per target with that target's
// own source URL and retrieval date, the comparison (a summary table, one table
// row per resource with each target's fit and best offering, and the blockers
// every target shares) and then one section per target with that target's
// summary, resources, required changes and blockers.
//
// The tie note the single-target table appends is left out of the comparison
// table, which the Scaleway section keeps for db-example, and the
// shared-blocker section says "None." when no resource is blocked on every
// target.
func TestReportComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inventory  inventory.Inventory
		keys       []string
		goldenFile string
	}{
		{
			name:       "three targets with blockers every target shares",
			inventory:  comparisonInventory(t),
			keys:       threeTargetKeys(),
			goldenFile: "testdata/comparison-three.golden",
		},
		{
			name:       "two targets without a shared blocker say None",
			inventory:  newInventory(t, sourceName, twoTargetResources()),
			keys:       []string{ovhcloudKey, scalewayKey},
			goldenFile: "testdata/comparison-two.golden",
		},
	}

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := renderComparison(t, assessment.Compare(assessTargets(t, test.inventory, test.keys)), meta)
			testutil.Golden(t, test.goldenFile, got)

			checkTableShape(t, got)
			checkComparisonHasNoTieNote(t, got)
		})
	}
}

// comparisonTieNote is the text the single-target table appends to a best
// offering that has other candidates a reader could take: renderBestOffering
// writes " (tie: ...)" after the best offering's code span.
const comparisonTieNote = "(tie: "

// checkComparisonHasNoTieNote asserts that the comparison table states each
// target's best offering alone, while the Scaleway section keeps the tie notes
// of the single-target table it is the report's own section of.
func checkComparisonHasNoTieNote(t *testing.T, out []byte) {
	t.Helper()

	rows := linesBetween(t, sectionLines(t, out, "## Comparison"),
		"### Resources", "### Blockers on every target")

	for _, row := range rows {
		if strings.Contains(row, comparisonTieNote) {
			t.Errorf("the comparison table row holds the tie note %q: %q", comparisonTieNote, row)
		}
	}

	if len(rows) == 0 {
		t.Fatalf("the comparison holds no resources table, so the tie-note check saw no row:\n%s", out)
	}

	scalewayRows := linesBetween(t, sectionLines(t, out, targetSectionHeading("Scaleway")),
		"### Resources", "### Required changes")

	if !slices.ContainsFunc(scalewayRows, func(row string) bool {
		return strings.Contains(row, comparisonTieNote)
	}) {
		t.Errorf("the Scaleway resources table holds no tie note %q, although its best offerings have ties: %q",
			comparisonTieNote, scalewayRows)
	}
}

// linesBetween returns the lines that follow the start heading and precede the
// first line that starts with stop, both excluded. It fails the test when either
// heading is missing.
func linesBetween(t *testing.T, lines []string, start, stop string) []string {
	t.Helper()

	begin := slices.Index(lines, start)
	if begin < 0 {
		t.Fatalf("no %q heading among the section's lines: %q", start, lines)
	}

	for i, line := range lines[begin+1:] {
		if strings.HasPrefix(line, stop) {
			return lines[begin+1 : begin+1+i]
		}
	}

	t.Fatalf("no line starting with %q after the %q heading among the section's lines: %q",
		stop, start, lines)

	return nil
}

// TestReportComparisonNoInventoryName pins that the inventory file line is left
// out of the comparison header when the metadata holds no file name, exactly as
// the single-target report leaves it out.
func TestReportComparisonNoInventoryName(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))

	out := string(renderComparison(t, c, analysis.ReportMeta{}))
	if strings.Contains(out, "- Inventory file:") {
		t.Errorf("the comparison shows an inventory file line although the metadata holds no name:\n%s", out)
	}

	if got := lineWithPrefix(t, []byte(out), "- Source: "); got != "- Source: `aws`" {
		t.Errorf("source line = %q, want %q", got, "- Source: `aws`")
	}
}

// TestReportComparisonHostileValues pins that no value a renderer would read as
// syntax can end a line, a heading or a table row: every value the report shows
// (target display names, resource IDs, categories, raw types, offering names,
// reason text, the inventory file name, the source and the snapshot URLs) is a
// code span, and a line ending inside a code span is a space. Table cell counts
// are checked with checkTableShape, which a forged cell boundary would break.
//
// The target keys are hostile too, but no key reaches the output: the shared
// blocker list names a target by its display name, so the hostile keys only
// exercise the key-to-name lookup.
func TestReportComparisonHostileValues(t *testing.T) {
	t.Parallel()

	c := assessment.Compare(hostileComparisonTargets())

	out := renderComparison(t, c, analysis.ReportMeta{InventoryName: hostileComparisonInventoryName})
	testutil.Golden(t, "testdata/comparison-hostile.golden", out)

	checkTableShape(t, out)

	for _, value := range hostileComparisonValues() {
		if strings.Contains(string(out), value) {
			t.Errorf("the comparison contains the raw value %q, so a line ending was not escaped:\n%s", value, out)
		}
	}
}

// TestReportComparisonSectionsReuseReport pins that each target's section is
// that target's single-target report, rendered by Report with the same metadata,
// with the title and header lines the comparison already gave removed and every
// heading one level lower: same content, same row order. The expectation is
// derived from Report, not written down twice.
func TestReportComparisonSectionsReuseReport(t *testing.T) {
	t.Parallel()

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	targets := assessTargets(t, comparisonInventory(t), threeTargetKeys())

	// Compare sorts by key, so the comparison's sections do not depend on the
	// order the targets were handed over in.
	c := assessment.Compare([]assessment.TargetAssessment{targets[2], targets[0], targets[1]})

	out := renderComparison(t, c, meta)

	for _, target := range c.Targets {
		want := demotedReportLines(t, string(render(t, target.Assessment, meta)))
		got := sectionLines(t, out, targetSectionHeading(target.Assessment.Target))

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("the %q section is not that target's report one heading level lower (-want +got):\n%s",
				target.Key, diff)
		}
	}
}

// targetSectionHeading returns the comparison heading of one target's section.
// The fixture display names hold no Markdown metacharacter, so the code span of a
// name is the name between two backticks.
func targetSectionHeading(displayName string) string {
	return "## `" + displayName + "`"
}

// demotedReportLines turns a single-target report into the body its comparison
// section must hold: the title and the header lines are replaced by the
// comparison's own header, and every heading moves one level down. Blank lines
// are dropped, because the golden files pin the blank-line layout.
func demotedReportLines(t *testing.T, singleTargetReport string) []string {
	t.Helper()

	lines := strings.Split(singleTargetReport, "\n")

	// The body starts at the report's first section heading: everything before it
	// is the title and the header lines.
	start := slices.IndexFunc(lines, func(line string) bool {
		return strings.HasPrefix(line, "## ")
	})
	if start < 0 {
		t.Fatalf("the single-target report has no section heading:\n%s", singleTargetReport)
	}

	demoted := make([]string, 0, len(lines)-start)

	for _, line := range lines[start:] {
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			line = "#" + line
		}

		demoted = append(demoted, line)
	}

	return demoted
}

// TestReportComparisonHasNoRanking pins that a comparison states facts per target
// and does not rank them or recommend one: the words a ranking would use do not
// appear, while "best offering", a per-resource fact, stays.
//
// It runs with estimates off and on, because the words a ranking would use are
// the words a priced report is most tempting to use: the estimates-on case is a
// comparison with the fixture's prices in it, so "cheapest" and "best price" are
// checked against a report that shows amounts.
func TestReportComparisonHasNoRanking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// comparison is the comparison the reporter renders.
		comparison assessment.Comparison
		// prices is whether the report shows an amount, which the estimates-on
		// case must: the ranking words are checked against a priced report.
		prices bool
	}{
		{
			name:       "estimates off",
			comparison: assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys())),
		},
		{
			name:       "estimates on",
			comparison: assessment.Compare(assessReportTargets(t, []string{reportKey, reportNorthKey})),
			prices:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			out := renderComparison(t, test.comparison, analysis.ReportMeta{InventoryName: inventoryFileName})
			lowered := strings.ToLower(string(out))

			for _, word := range []string{"best target", "recommend", "winner", "cheapest", "best price"} {
				if strings.Contains(lowered, word) {
					t.Errorf("the comparison holds the ranking word %q:\n%s", word, out)
				}
			}

			if !strings.Contains(lowered, "best offering") {
				t.Errorf("the comparison does not name each target's best offering:\n%s", out)
			}

			if got := strings.Contains(lowered, "€"); got != test.prices {
				t.Errorf("the comparison shows an amount = %t, want %t: the ranking words are checked against "+
					"the report the case names:\n%s", got, test.prices, out)
			}
		})
	}
}

// TestReportComparisonIsDeterministic pins that the same comparison always
// renders the same bytes, and that the order the targets were assessed in does
// not reach the output: Compare sorts them by key.
func TestReportComparisonIsDeterministic(t *testing.T) {
	t.Parallel()

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	first := renderComparison(t, assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys())), meta)
	second := renderComparison(t, assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys())), meta)

	if !bytes.Equal(first, second) {
		t.Errorf("two renders of the same comparison differ:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	reversedKeys := threeTargetKeys()
	slices.Reverse(reversedKeys)

	reordered := renderComparison(t, assessment.Compare(assessTargets(t, comparisonInventory(t), reversedKeys)), meta)
	if !bytes.Equal(first, reordered) {
		t.Errorf("the comparison depends on the order the targets were given in:\nfirst:\n%s\nreordered:\n%s",
			first, reordered)
	}
}

// TestReportComparisonNoTarget pins that a comparison of no target is an error a
// renderer reports, not an empty document: ReportComparison writes nothing and
// the caller decides what to show instead.
func TestReportComparisonNoTarget(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := markdown.New().ReportComparison(t.Context(), &out, assessment.Comparison{},
		analysis.ReportMeta{InventoryName: inventoryFileName})
	if err == nil {
		t.Fatalf("ReportComparison() error = nil for a comparison of no target, want an error")
	}

	if out.Len() != 0 {
		t.Errorf("ReportComparison() wrote %d bytes to the writer, want none:\n%s", out.Len(), out.String())
	}
}

// TestReportComparisonSharedBlockerUnknownTarget pins that a shared blocker
// naming a target key the comparison does not hold is an error: the section
// would otherwise name an empty target, or the wrong one. Nothing is written.
func TestReportComparisonSharedBlockerUnknownTarget(t *testing.T) {
	t.Parallel()

	const unknownKey = "missing"

	c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))
	c.SharedBlockers[0].Targets = append(c.SharedBlockers[0].Targets, assessment.TargetReasons{
		Key:     unknownKey,
		Reasons: []mapping.Reason{{Code: mapping.NotClassified}},
	})

	var out bytes.Buffer

	err := markdown.New().ReportComparison(t.Context(), &out, c, analysis.ReportMeta{InventoryName: inventoryFileName})
	if err == nil {
		t.Fatalf("ReportComparison() error = nil, want an error for a shared blocker naming the unknown target key %q",
			unknownKey)
	}

	if out.Len() != 0 {
		t.Errorf("ReportComparison() wrote %d bytes to the writer, want none:\n%s", out.Len(), out.String())
	}
}

// TestReportComparisonMismatchedTargets pins that the targets of a comparison
// must report on the same resources in the same order, the order of the one
// inventory they all assessed: the comparison table is laid out from the first
// target's verdicts, so a target that reports on other resources, or on the same
// ones in another order, would be shown under the wrong rows. Each mismatch is
// an error, and nothing is written.
func TestReportComparisonMismatchedTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(c *assessment.Comparison)
	}{
		{
			name: "a later target has an extra verdict the first target lacks",
			change: func(c *assessment.Comparison) {
				c.Targets[1].Assessment.Verdicts = append(slices.Clone(c.Targets[1].Assessment.Verdicts),
					extraVerdict())
			},
		},
		{
			name: "a later target lacks a verdict the first target has",
			change: func(c *assessment.Comparison) {
				c.Targets[1].Assessment.Verdicts = c.Targets[1].Assessment.Verdicts[1:]
			},
		},
		{
			name: "the targets report on a different resource",
			change: func(c *assessment.Comparison) {
				c.Targets[1].Assessment.Verdicts[0] = extraVerdict()
			},
		},
		{
			name: "the targets report on the same resources in another order",
			change: func(c *assessment.Comparison) {
				c.Targets[1].Assessment.Verdicts[0], c.Targets[1].Assessment.Verdicts[1] =
					c.Targets[1].Assessment.Verdicts[1], c.Targets[1].Assessment.Verdicts[0]
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))
			test.change(&c)

			var out bytes.Buffer

			err := markdown.New().ReportComparison(t.Context(), &out, c,
				analysis.ReportMeta{InventoryName: inventoryFileName})
			if err == nil {
				t.Fatalf("ReportComparison() error = nil, want an error for targets that do not report on the same resources")
			}

			if out.Len() != 0 {
				t.Errorf("ReportComparison() wrote %d bytes to the writer, want none:\n%s", out.Len(), out.String())
			}
		})
	}
}

// extraVerdict is a verdict on a resource the comparison inventory does not
// hold, for the fixture targets that must report on other resources.
func extraVerdict() assessment.Verdict {
	return assessment.Verdict{
		Resource: inventory.Resource{
			ID:       "extra-example",
			RawType:  "AWS::SQS::Queue",
			Category: taxonomy.Unclassified,
		},
		Fit:     mapping.None,
		Reasons: []mapping.Reason{{Code: mapping.NotClassified}},
	}
}

// TestReportComparisonCancelledContext pins that a cancelled context stops
// ReportComparison before anything is rendered: the caller gets the cancellation,
// and the writer never sees a report the caller has stopped waiting for.
func TestReportComparisonCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))

	var out bytes.Buffer

	err := markdown.New().ReportComparison(ctx, &out, c, analysis.ReportMeta{InventoryName: inventoryFileName})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReportComparison() error = %v, want an error wrapping context.Canceled", err)
	}

	if out.Len() != 0 {
		t.Errorf("ReportComparison() wrote %d bytes to the writer, want none:\n%s", out.Len(), out.String())
	}
}

// TestReportComparisonReturnsWriteError pins that a failing writer is reported to
// the caller, wrapped or as it came, never ignored: the driving adapter decides
// what to do with a partial report.
func TestReportComparisonReturnsWriteError(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("disk is full")

	c := assessment.Compare(assessTargets(t, comparisonInventory(t), threeTargetKeys()))

	err := markdown.New().ReportComparison(t.Context(), failingWriter{err: writeErr}, c,
		analysis.ReportMeta{InventoryName: inventoryFileName})
	if !errors.Is(err, writeErr) {
		t.Fatalf("ReportComparison() error = %v, want an error wrapping %v", err, writeErr)
	}
}

// The hostile comparison fixture: every value the report shows would be syntax in
// a renderer, and each carries a pipe, an angle bracket, a backtick or a line
// ending. The domain constructors refuse a line ending in a value, so the
// assessments are built by hand, as the single-target escaping tests do.
const (
	// hostileComparisonInventoryName is a file name with a pipe, a backtick, a
	// link and a line ending.
	hostileComparisonInventoryName = "inv|`x`\n[y](z).json"
	// hostileComparisonSource is an inventory source that would start a heading if
	// it were not a code span, and carries a pipe, a backtick and a line ending.
	hostileComparisonSource = "aws|`src`\n# heading"
	// hostileComparisonName1, 2 and 3 are the targets' display names, and
	// hostileComparisonOffering is an offering name: each carries the characters
	// that end a table cell, start HTML, open a code span or end a line.
	hostileComparisonName1 = "Evil | `x` <b> [a](b) *s*"
	hostileComparisonName2 = "Tick\n`Name`"
	hostileComparisonName3 = "<img src=x> | `t`"
	// hostileComparisonOffering is the best offering of the direct verdict.
	hostileComparisonOffering = "`tick` [row](x) | Name"
	// hostileComparisonReason is a reason code the domain does not know, so it is
	// rendered as escaped literal text, with a pipe, a backtick and a line ending.
	hostileComparisonReason = "<b>x|`y`\nz"
)

// hostileComparisonCategory is a taxonomy category the fixture holds: it carries
// a pipe, a backtick, an angle bracket and a line ending.
func hostileComparisonCategory() taxonomy.Category {
	return taxonomy.Category("store|age`<x>\nz")
}

// hostileComparisonTargets builds the three fixed target assessments of the
// hostile golden, named by hostile keys, in key order.
//
// One resource is blocked on every target for a category no target serves, with
// a reason that names that target, one is blocked on every target for being
// unclassified, and one is direct, partial and none on the three targets, so the
// comparison has both a shared blocker list and a merely blocked resource.
func hostileComparisonTargets() []assessment.TargetAssessment {
	names := []string{hostileComparisonName1, hostileComparisonName2, hostileComparisonName3}
	keys := []string{"a|b", "k`ey\nx", "z<z"}
	urls := []string{
		"https://example.com/a|b`c`<d>\ne",
		"https://example.com/|`url`\n<x>",
		"https://example.com/<x>|`u`\nv",
	}
	retrieved := []time.Time{
		time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC),
	}

	targets := make([]assessment.TargetAssessment, 0, len(names))

	for i, name := range names {
		a := assessment.Assessment{
			Target:   name,
			Snapshot: catalog.Snapshot{SourceURL: urls[i], RetrievedOn: retrieved[i]},
			Source:   hostileComparisonSource,
			Verdicts: []assessment.Verdict{
				hostileNoOfferingVerdict(name),
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

		targets = append(targets, assessment.TargetAssessment{Key: keys[i], Assessment: a})
	}

	return targets
}

// hostileNoOfferingVerdict is the verdict of the resource that is blocked on
// target displayName for a category no target serves, with a hostile ID, raw type
// and category.
func hostileNoOfferingVerdict(displayName string) assessment.Verdict {
	return assessment.Verdict{
		Resource: inventory.Resource{
			ID:       "`tick`",
			RawType:  "AWS::X|<y>`\n",
			Category: hostileComparisonCategory(),
		},
		Fit:     mapping.None,
		Reasons: []mapping.Reason{{Code: mapping.NoOffering, Target: displayName, Category: hostileComparisonCategory()}},
	}
}

// hostileUnclassifiedVerdict is the verdict of the second resource blocked on
// every target.
func hostileUnclassifiedVerdict() assessment.Verdict {
	return assessment.Verdict{
		Resource: inventory.Resource{
			ID:       "a`b",
			RawType:  "AWS::Y",
			Category: taxonomy.Unclassified,
		},
		Fit:     mapping.None,
		Reasons: []mapping.Reason{{Code: mapping.NotClassified}},
	}
}

// hostileVaryingVerdict is the verdict of the resource that is direct on the
// first target, partial on the second and none on the third, so the comparison
// table holds a hostile offering name, a hostile missing capability and a hostile
// literal reason.
func hostileVaryingVerdict(target int) assessment.Verdict {
	resource := inventory.Resource{
		ID:       "i-1 | x",
		RawType:  "AWS::EC2::Instance",
		Category: taxonomy.ComputeVM,
	}

	switch target {
	case 0:
		return assessment.Verdict{
			Resource:     resource,
			Fit:          mapping.Direct,
			BestOffering: hostileComparisonOffering,
			Candidates: []mapping.Candidate{{
				Offering: catalog.Offering{
					Name:       hostileComparisonOffering,
					Categories: []taxonomy.Category{taxonomy.ComputeVM},
				},
				Fit: mapping.Direct,
			}},
			Reasons: []mapping.Reason{{Code: mapping.AllCapabilities, Offering: hostileComparisonOffering}},
		}
	case 1:
		return assessment.Verdict{
			Resource:        resource,
			Fit:             mapping.Partial,
			BestOffering:    "<b>Offering</b>",
			Reasons:         []mapping.Reason{{Code: mapping.MissingCapability, Offering: "<b>Offering</b>", Capability: taxonomy.Snapshots}},
			RequiredChanges: []taxonomy.Capability{taxonomy.Snapshots},
		}
	default:
		return assessment.Verdict{
			Resource: resource,
			Fit:      mapping.None,
			Reasons:  []mapping.Reason{{Code: mapping.ReasonCode(hostileComparisonReason)}},
		}
	}
}

// hostileComparisonValues returns every fixture value that carries a line ending,
// which no report may show raw: a value that kept one would end the line, the
// heading or the table row it is in.
func hostileComparisonValues() []string {
	return []string{
		hostileComparisonInventoryName,
		hostileComparisonSource,
		hostileComparisonName2,
		"k`ey\nx",
		"https://example.com/a|b`c`<d>\ne",
		"https://example.com/|`url`\n<x>",
		"https://example.com/<x>|`u`\nv",
		string(hostileComparisonCategory()),
		"AWS::X|<y>`\n",
		hostileComparisonReason,
	}
}

// The two targets the estimate comparison compares: the report fixture's
// invented prices, with display names, snapshots and default regions that
// differ, so the comparison has to keep two regions and two estimates apart.
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

// TestReportComparisonWithEstimates pins the comparison of two priced targets:
// the region line per target in the header, one estimate row per target in the
// summary, one estimate column per target in the resources table, and each
// target's own cost summary and per-workload estimates in its section.
func TestReportComparisonWithEstimates(t *testing.T) {
	t.Parallel()

	keys := []string{reportKey, reportNorthKey}

	c := assessment.Compare(assessReportTargets(t, keys))
	out := renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName})

	checkTableShape(t, out)

	t.Run("header names each target's region", func(t *testing.T) {
		t.Parallel()

		want := []string{
			"- Region: `Example Cloud`: `xx-par` (the target's default for estimates)",
			"- Region: `Example Cloud North`: `xx-ams` (the target's default for estimates)",
		}

		if diff := cmp.Diff(want, linesWithPrefix(t, out, "- Region: ")); diff != "" {
			t.Errorf("region lines mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("one estimate row per target in the summary", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			target string
			cells  []string
		}{
			{
				target: "`Example Cloud` estimate",
				cells:  []string{"`Example Cloud` estimate", "€2,535.00 per month for 4 of 8 resources", ""},
			},
			{
				target: "`Example Cloud North` estimate",
				cells:  []string{"`Example Cloud North` estimate", "", "€2,539.00 per month for 4 of 8 resources"},
			},
		}

		for _, test := range tests {
			t.Run(test.target, func(t *testing.T) {
				t.Parallel()

				if diff := cmp.Diff(test.cells, tableRow(t, out, test.target)); diff != "" {
					t.Errorf("summary row mismatch (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("one estimate column per target in the resources table", func(t *testing.T) {
		t.Parallel()

		got := tableRow(t, out, "ID")
		if len(got) != 8 {
			t.Fatalf("the resources table has %d columns, want 8: %q", len(got), got)
		}

		for _, column := range []string{"`Example Cloud` monthly estimate", "`Example Cloud North` monthly estimate"} {
			if !slices.Contains(got, column) {
				t.Errorf("the resources table has no %q column: %q", column, got)
			}
		}

		// The regions differ only in their prices: the same resource costs 10.00
		// in the default region and 12.00 in the second, and a resource with no
		// plan large enough says so in both. The cells say what the estimate
		// assumed of a resource that gives no count and no hours (ADR 0025).
		tests := []struct {
			id     string
			first  string
			second string
		}{
			{
				id:     "worker-jobs",
				first:  "€10.00 (count assumed 1) (730 hours assumed)",
				second: "€12.00 (count assumed 1) (730 hours assumed)",
			},
			{id: "server-huge", first: "not estimated: no plan large enough", second: "not estimated: no plan large enough"},
		}

		for _, test := range tests {
			cells := tableRow(t, out, "`"+test.id+"`")
			if got := []string{cells[4], cells[7]}; !slices.Equal(got, []string{test.first, test.second}) {
				t.Errorf("%q estimates = %q, want %q", test.id, got, []string{test.first, test.second})
			}
		}
	})

	t.Run("each target's own cost summary and workloads", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			target   string
			total    string
			workload string
		}{
			{
				target: "Example Cloud",
				total:  "- Total: €2,535.00 per month for 4 of 8 resources",
				workload: "- Monthly estimate: €2,525.00 per month for 3 of 3 resources, list prices on " +
					reportRetrievedOn + ", excluding VAT and discounts",
			},
			{
				target: "Example Cloud North",
				total:  "- Total: €2,539.00 per month for 4 of 8 resources",
				workload: "- Monthly estimate: €2,527.00 per month for 3 of 3 resources, list prices on " +
					reportTwoRetrievedOn + ", excluding VAT and discounts",
			},
		}

		for _, test := range tests {
			t.Run(test.target, func(t *testing.T) {
				t.Parallel()

				section := sectionLines(t, out, targetSectionHeading(test.target))

				for _, want := range []string{test.total, test.workload} {
					if !slices.Contains(section, want) {
						t.Errorf("the %q section does not hold %q: %q", test.target, want, section)
					}
				}
			})
		}
	})

	testutil.Golden(t, "testdata/comparison-estimates.golden", out)
}

// TestReportComparisonWithBillingCapHours pins that a billing cap belongs to a
// target, not to a comparison (ADR 0029): each target's own section carries its
// own hours line, with one clause per distinct cap of that target, ascending.
func TestReportComparisonWithBillingCapHours(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, sourceName, estimateResources())

	capped := testutil.ReportCappedCatalogIn(t, reportTwoTarget, reportTwoSnapshot())
	offerings := capped.Offerings()
	// The report fixture's second offering, the Kubernetes one, bills its node
	// plans for 336 hours a month too, so this target's line carries two caps,
	// out of ascending order in the catalog.
	offerings[1].PlanBillingCapHours = 336

	twoCaps, err := catalog.New(capped.Target(), capped.Snapshot(), offerings)
	if err != nil {
		t.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	c := assessment.Compare([]assessment.TargetAssessment{
		{
			Key:        reportKey,
			Assessment: assessment.AssessWith(inv, testutil.ReportCatalog(t), assessment.Options{Estimate: true}),
		},
		{
			Key:        reportNorthKey,
			Assessment: assessment.AssessWith(inv, twoCaps, assessment.Options{Estimate: true}),
		},
	})
	out := renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName})

	plain := "- Hours: plan price × count × hours / 730, 730 hours for a resource that gives none"
	cappedLine := plain + "; a plan billed for at most 336 hours a month costs plan price × count × " +
		"min(hours, 336) / 336; a plan billed for at most 672 hours a month costs plan price × count × " +
		"min(hours, 672) / 672"

	tests := []struct {
		target string
		want   string
	}{
		{target: reportTarget, want: plain},
		{target: reportTwoTarget, want: cappedLine},
	}

	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			t.Parallel()

			var found []string

			for _, line := range sectionLines(t, out, targetSectionHeading(test.target)) {
				if strings.HasPrefix(line, "- Hours: ") {
					found = append(found, line)
				}
			}

			if len(found) != 1 {
				t.Fatalf("the %q section has %d hours lines, want 1:\n%s", test.target, len(found), out)
			}

			if found[0] != test.want {
				t.Errorf("hours line of %q = %q, want %q", test.target, found[0], test.want)
			}
		})
	}
}
