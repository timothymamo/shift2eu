package cli_test

// This file pins the two options of the assess command that PRD #190 milestone 8
// adds, --region and --no-estimate, through the real Markdown and JSON
// reporters: estimates are on by default, a region chosen on the command line
// reaches the report, and a bad --region value is a usage error that stops the
// run before any inventory is read. The catalogs are the invented fixture of
// internal/testutil, never a real provider's price list.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/cli"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The --target keys the estimate harness wires.
const (
	estimateTargetKey = "scaleway"
	secondTargetKey   = "ovhcloud"
)

// estimateInventory is the sized inventory the estimate tests assess: one
// virtual machine the fixture prices on its smallest plan, and one function,
// which the fixture prices by usage, so a report has one resource it can
// estimate and one it cannot.
func estimateInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	return newEstimateInventory(t, true)
}

// unsizedInventory is estimateInventory without its sizes: the same resources
// and the same fits, with nothing to match a plan against.
func unsizedInventory(t *testing.T) inventory.Inventory {
	t.Helper()

	return newEstimateInventory(t, false)
}

// newEstimateInventory builds estimateInventory, with or without the sizes.
func newEstimateInventory(t *testing.T, sized bool) inventory.Inventory {
	t.Helper()

	server := inventory.Resource{
		ID:       "server-small",
		RawType:  "AWS::EC2::Instance",
		Category: taxonomy.ComputeVM,
		Region:   "eu-west-1",
	}

	if sized {
		server.Size = &inventory.Size{VCPUs: estimateNumber(1), MemoryGiB: estimateNumber(2)}
	}

	inv, err := inventory.New("aws", []inventory.Resource{
		server,
		{
			ID:       "function-orders",
			RawType:  "AWS::Lambda::Function",
			Category: taxonomy.ServerlessFunction,
			Region:   "eu-west-1",
		},
	})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return inv
}

// estimateNumber is a given float64, as an inventory size carries it.
func estimateNumber(v float64) *float64 { return &v }

// estimateHarness is the CLI run with the real reporters over the invented
// catalog fixture: one catalog source per --target key, the fake inventory
// source, and both formats. It records which inventory the CLI asked for, so a
// test can see that a usage error reads none.
type estimateHarness struct {
	inventory *fakeInventorySource
	catalogs  map[string]*fakeCatalogSource
	// paths is every --inventory value the CLI passed to NewInventorySource.
	paths []string
}

// newEstimateHarness returns a harness whose every run would succeed: the sized
// inventory and the report fixture catalog.
func newEstimateHarness(t *testing.T) *estimateHarness {
	t.Helper()

	return newEstimateHarnessWith(t, estimateInventory(t), testutil.ReportCatalog(t))
}

// newEstimateHarnessWith returns a harness over inv, with cat as every target's
// catalog.
func newEstimateHarnessWith(t *testing.T, inv inventory.Inventory, cat catalog.Catalog) *estimateHarness {
	t.Helper()

	return &estimateHarness{
		inventory: &fakeInventorySource{inv: inv},
		catalogs: map[string]*fakeCatalogSource{
			estimateTargetKey: {cat: cat},
			secondTargetKey:   {cat: cat},
		},
	}
}

// deps returns the Deps the CLI is run with: both target keys, and each format
// with the real reporter of that format.
func (h *estimateHarness) deps() cli.Deps {
	targets := make(map[string]func() analysis.CatalogSource, len(h.catalogs))

	for key, source := range h.catalogs {
		targets[key] = func() analysis.CatalogSource { return source }
	}

	return cli.Deps{
		NewInventorySource: func(path string) analysis.InventorySource {
			h.paths = append(h.paths, path)

			return h.inventory
		},
		Targets: targets,
		Formats: map[string]cli.Format{
			markdownFormat: {
				NewReporter:           func() analysis.Reporter { return markdown.New() },
				NewComparisonReporter: func() analysis.ComparisonReporter { return markdown.New() },
			},
			jsonFormat: {
				NewReporter:           func() analysis.Reporter { return jsonreport.New() },
				NewComparisonReporter: func() analysis.ComparisonReporter { return jsonreport.New() },
			},
		},
	}
}

// run runs the CLI with args and returns the exit code, stdout and stderr.
func (h *estimateHarness) run(ctx context.Context, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer

	code = cli.Run(ctx, args, &out, &errOut, h.deps())

	return code, out.String(), errOut.String()
}

// assessArgs are the arguments every run of the tests below names: the
// inventory file and the one target.
func assessArgs(extra ...string) []string {
	return append([]string{"assess", "--inventory", inventoryPath, "--target", estimateTargetKey}, extra...)
}

// TestRunAssessEstimatesAreOnByDefault pins PRD #190 milestone 8's default: a
// plain assess of a sized inventory estimates, so the report holds the Plan and
// Monthly estimate columns and the cost summary, and the run that switches them
// off holds no price at all.
func TestRunAssessEstimatesAreOnByDefault(t *testing.T) {
	t.Parallel()

	t.Run("estimates on by default", func(t *testing.T) {
		t.Parallel()

		h := newEstimateHarness(t)

		code, stdout, stderr := h.run(t.Context(), assessArgs()...)
		if code != 0 {
			t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		// The estimate columns of the resources table, the cost summary and a price:
		// the fixture prices the virtual machine on its smallest plan.
		for _, want := range []string{"| Plan |", "| Monthly estimate |", "## Cost estimate", "- Total: ", "€"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("the report does not hold %q, so a run without --no-estimate does not estimate:\n%s",
					want, stdout)
			}
		}
	})

	t.Run("--no-estimate holds no price", func(t *testing.T) {
		t.Parallel()

		h := newEstimateHarness(t)

		code, stdout, stderr := h.run(t.Context(), assessArgs("--no-estimate")...)
		if code != 0 {
			t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		for _, unwanted := range []string{"€", "Plan", "Monthly estimate", "## Cost estimate", "Cost estimate"} {
			if strings.Contains(stdout, unwanted) {
				t.Errorf("a report rendered with --no-estimate holds %q:\n%s", unwanted, stdout)
			}
		}

		if !strings.Contains(stdout, estimateResourcesHeading) {
			t.Errorf("the report holds no %q section:\n%s", estimateResourcesHeading, stdout)
		}
	})
}

// resourcesHeading, regionLinePrefix and regionChosenMark are the report's own
// words the tests below read: the table of verdicts, the header line that names
// the region the estimates are for, and how that line says the region was
// chosen.
const (
	estimateResourcesHeading = "## Resources"
	regionLinePrefix         = "- Region: "
	regionChosenMark         = "chosen with"
	columnsNoEstimate        = "| ID | Raw type | Category | Fit | Best offering | Reasons |"
)

// regionLine returns the one line of report that starts with regionLinePrefix,
// failing the test when there is not exactly one.
func regionLine(t *testing.T, report string) string {
	t.Helper()

	var lines []string

	for line := range strings.SplitSeq(report, "\n") {
		if strings.HasPrefix(line, regionLinePrefix) {
			lines = append(lines, line)
		}
	}

	if len(lines) != 1 {
		t.Fatalf("the report holds %d region lines %q, want 1:\n%s", len(lines), lines, report)
	}

	return lines[0]
}

// TestRunAssessRegion pins --region: the chosen region reaches the report, with
// and without estimates, and the report says the region was chosen rather than
// defaulted.
func TestRunAssessRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// args are the extra arguments of the run.
		args []string
	}{
		{name: "with estimates", args: []string{"--region", estimateTargetKey + "=" + testutil.ReportSecondRegion}},
		{
			name: "without estimates",
			args: []string{"--region", estimateTargetKey + "=" + testutil.ReportSecondRegion, "--no-estimate"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newEstimateHarness(t)

			code, stdout, stderr := h.run(t.Context(), assessArgs(tc.args...)...)
			if code != 0 {
				t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
			}

			line := regionLine(t, stdout)

			if want := regionLinePrefix + "`" + testutil.ReportSecondRegion + "`"; !strings.HasPrefix(line, want) {
				t.Errorf("region line = %q, want it to start with %q", line, want)
			}

			if !strings.Contains(line, regionChosenMark) {
				t.Errorf("region line = %q, want it to say the region was chosen", line)
			}

			if tc.args[len(tc.args)-1] == "--no-estimate" && strings.Contains(stdout, "€") {
				t.Errorf("a report rendered with --no-estimate and --region holds a price:\n%s", stdout)
			}

			if tc.args[len(tc.args)-1] != "--no-estimate" {
				// The prices are the chosen region's: the fixture's compute plans
				// cost a fifth more there.
				if !strings.Contains(stdout, "€12.00") {
					t.Errorf("the report holds no estimate for the chosen region, want €12.00:\n%s", stdout)
				}
			}
		})
	}
}

// TestRunAssessFormatJSONHonoursTheOptions pins that --format json honours both
// options too: the document of a run without them holds the estimate basis and
// its total, and --no-estimate leaves out the estimate while --region still
// names the region the run chose.
func TestRunAssessFormatJSONHonoursTheOptions(t *testing.T) {
	t.Parallel()

	t.Run("estimates on by default", func(t *testing.T) {
		t.Parallel()

		h := newEstimateHarness(t)

		code, stdout, stderr := h.run(t.Context(), assessArgs("--format", jsonFormat,
			"--region", estimateTargetKey+"="+testutil.ReportSecondRegion)...)
		if code != 0 {
			t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		document := decodeDocument(t, stdout)

		for path, want := range map[string]any{
			"estimate.region":       testutil.ReportSecondRegion,
			"estimate.regionChosen": true,
		} {
			if diff := cmp.Diff(want, valueAtPath(t, document, path)); diff != "" {
				t.Errorf("%s mismatch (-want +got):\n%s", path, diff)
			}
		}
	})

	t.Run("--no-estimate", func(t *testing.T) {
		t.Parallel()

		h := newEstimateHarness(t)

		code, stdout, stderr := h.run(t.Context(), assessArgs("--format", jsonFormat,
			"--region", estimateTargetKey+"="+testutil.ReportSecondRegion, "--no-estimate")...)
		if code != 0 {
			t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		document := decodeDocument(t, stdout)

		if _, ok := document["estimate"]; ok {
			t.Errorf("a document rendered with --no-estimate holds an estimate: %v", document["estimate"])
		}

		if diff := cmp.Diff(testutil.ReportSecondRegion, document["region"]); diff != "" {
			t.Errorf("region mismatch (-want +got), a document without estimates still names the chosen "+
				"region:\n%s", diff)
		}
	})
}

// decodeDocument decodes a JSON report, failing the test on anything else.
func decodeDocument(t *testing.T, report string) map[string]any {
	t.Helper()

	var document map[string]any
	if err := json.Unmarshal([]byte(report), &document); err != nil {
		t.Fatalf("decoding the JSON report %q: %v", report, err)
	}

	return document
}

// valueAtPath returns the value at a dotted path in document, failing the test
// when the document does not hold it.
func valueAtPath(t *testing.T, document map[string]any, path string) any {
	t.Helper()

	var value any = document

	for _, step := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s: %q is not an object in the document", path, step)
		}

		value, ok = object[step]
		if !ok {
			t.Fatalf("the document holds no %q: %q", path, document)
		}
	}

	return value
}

// TestRunAssessRegionUsageError pins --region's validation: a value the run
// cannot use is a usage error (exit 2) whose one error line names the element at
// fault, and the run reads no inventory and writes no report.
func TestRunAssessRegionUsageError(t *testing.T) {
	t.Parallel()

	unreadable := errors.New("open dir/inv.json: no such file or directory")

	tests := []struct {
		name string
		args []string
		// want is what the error line must name.
		want []string
	}{
		{
			name: "a region the target does not list",
			args: []string{"--region", estimateTargetKey + "=xx-nope"},
			want: []string{"xx-nope", testutil.PricedDefaultRegion, testutil.ReportSecondRegion},
		},
		{
			name: "a target --target does not select",
			args: []string{"--region", secondTargetKey + "=" + testutil.PricedDefaultRegion},
			want: []string{secondTargetKey},
		},
		{
			name: "a target named twice",
			args: []string{"--region", estimateTargetKey + "=" + testutil.PricedDefaultRegion + "," +
				estimateTargetKey + "=" + testutil.ReportSecondRegion},
			want: []string{estimateTargetKey},
		},
		{
			name: "a pair with no region",
			args: []string{"--region", estimateTargetKey},
			want: []string{estimateTargetKey},
		},
		{
			name: "a pair with an empty target",
			args: []string{"--region", "=" + testutil.PricedDefaultRegion},
			want: []string{testutil.PricedDefaultRegion},
		},
		{
			name: "a pair with an empty region",
			args: []string{"--region", estimateTargetKey + "="},
			want: []string{estimateTargetKey},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newEstimateHarness(t)
			// A run that read the inventory would fail with this error and exit
			// 1, so the test can tell a usage error from a read error.
			h.inventory.err = unreadable

			dest := t.TempDir() + "/report.md"

			code, stdout, stderr := h.run(t.Context(), assessArgs(append(slices.Clone(tc.args),
				"--output", dest)...)...)
			if code != 2 {
				t.Errorf("Run() = %d, want 2 (a usage error); stderr = %q", code, stderr)
			}

			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}

			firstLine, _, _ := strings.Cut(stderr, "\n")
			if !strings.HasPrefix(firstLine, errPrefix) {
				t.Errorf("the first line = %q, want it to start with %q", firstLine, errPrefix)
			}

			for _, want := range tc.want {
				if !strings.Contains(firstLine, want) {
					t.Errorf("the error line = %q, want it to name %q", firstLine, want)
				}
			}

			if !strings.Contains(stderr, "Usage:") {
				t.Errorf("stderr = %q, want the usage text a usage error adds", stderr)
			}

			if h.inventory.calls != 0 {
				t.Errorf("the inventory was read %d times, want 0: a bad --region is a usage error",
					h.inventory.calls)
			}

			if _, err := os.Stat(dest); err == nil {
				t.Errorf("the report %q was written for a usage error", dest)
			}
		})
	}
}

// TestRunAssessNoEstimateWithSizes pins PRD #190 milestone 8's eighth
// requirement: with --no-estimate, a sized inventory gives the same fits as the
// same inventory with its sizes removed, and, since nothing of the size reaches
// a report without estimates, the same report, byte for byte.
func TestRunAssessNoEstimateWithSizes(t *testing.T) {
	t.Parallel()

	sized := newEstimateHarnessWith(t, estimateInventory(t), testutil.ReportCatalog(t))
	unsized := newEstimateHarnessWith(t, unsizedInventory(t), testutil.ReportCatalog(t))

	code, withSizes, stderr := sized.run(t.Context(), assessArgs("--no-estimate")...)
	if code != 0 {
		t.Fatalf("Run() with sizes = %d, want 0; stderr = %q", code, stderr)
	}

	code, withoutSizes, stderr := unsized.run(t.Context(), assessArgs("--no-estimate")...)
	if code != 0 {
		t.Fatalf("Run() without sizes = %d, want 0; stderr = %q", code, stderr)
	}

	if !strings.Contains(withSizes, columnsNoEstimate) {
		t.Errorf("the report does not hold the six columns of a report without estimates %q:\n%s",
			columnsNoEstimate, withSizes)
	}

	if withSizes != withoutSizes {
		t.Errorf("--no-estimate changes the report of a sized inventory:\nwith sizes:\n%s\nwithout sizes:\n%s",
			withSizes, withoutSizes)
	}
}

// TestRunAssessRegionChangesOnlyTheRegion pins that the region reaches the report
// and nothing else: two regions of one target give reports that differ in the
// region line and in the lines about the resources whose offerings differ
// between them, and in the summary counts that follow from those fits.
//
// The catalog is the invented fixture with one price list for every region, and
// with its usage-priced offering served in the second region only, so the two
// reports can differ in which region they name and in what that region serves,
// never in what anything costs.
func TestRunAssessRegionChangesOnlyTheRegion(t *testing.T) {
	t.Parallel()

	cat := estimateRegionalCatalog(t)

	tests := []struct {
		name   string
		region string
	}{
		{name: "the first region", region: testutil.PricedDefaultRegion},
		{name: "the second region", region: testutil.ReportSecondRegion},
	}

	reports := make(map[string]string, len(tests))

	for _, tc := range tests {
		h := newEstimateHarnessWith(t, estimateInventory(t), cat)

		code, stdout, stderr := h.run(t.Context(), assessArgs("--region", estimateTargetKey+"="+tc.region)...)
		if code != 0 {
			t.Fatalf("%s: Run() = %d, want 0; stderr = %q", tc.name, code, stderr)
		}

		if got := regionLine(t, stdout); !strings.HasPrefix(got, regionLinePrefix+"`"+tc.region+"`") {
			t.Errorf("%s: region line = %q, want it to name %q", tc.name, got, tc.region)
		}

		reports[tc.region] = stdout
	}

	first, second := reports[testutil.PricedDefaultRegion], reports[testutil.ReportSecondRegion]

	if first == second {
		t.Fatal("the two regions give the same report, so the region reaches nothing")
	}

	for _, line := range differingLines(first, second) {
		if isRegionOrServedLine(line) {
			continue
		}

		t.Errorf("the two regions differ in the line %q, which names no region and no resource whose "+
			"offering differs:\nfirst region:\n%s\nsecond region:\n%s", line, first, second)
	}
}

// differingLines returns every line of a or b the other does not hold, in a's
// order and then b's, once each.
func differingLines(a, b string) []string {
	inB := make(map[string]bool)
	for line := range strings.SplitSeq(b, "\n") {
		inB[line] = true
	}

	var differing []string

	for line := range strings.SplitSeq(a, "\n") {
		if !inB[line] && !slices.Contains(differing, line) {
			differing = append(differing, line)
		}
	}

	inA := make(map[string]bool)
	for line := range strings.SplitSeq(a, "\n") {
		inA[line] = true
	}

	for line := range strings.SplitSeq(b, "\n") {
		if !inA[line] && !slices.Contains(differing, line) {
			differing = append(differing, line)
		}
	}

	return differing
}

// servedOnlyInTheSecondRegion is the resource the regional catalog serves in
// testutil.ReportSecondRegion only.
const servedOnlyInTheSecondRegion = "function-orders"

// isRegionOrServedLine reports whether the two regions may differ in line: a
// line that names the region, a line about a resource whose offering differs
// between them, or a summary count of the fits, which such a resource changes.
func isRegionOrServedLine(line string) bool {
	switch {
	case strings.HasPrefix(line, regionLinePrefix), strings.HasPrefix(line, "- Prices in "):
		return true
	case strings.Contains(line, servedOnlyInTheSecondRegion):
		return true
	case strings.HasPrefix(line, "| direct |"), strings.HasPrefix(line, "| none |"):
		return true
	default:
		return false
	}
}

// estimateRegionalCatalog is the invented report fixture catalog with one price
// list for every region, and its usage-priced offering served in
// testutil.ReportSecondRegion only: the two regions differ in what they serve,
// never in what anything costs.
func estimateRegionalCatalog(t *testing.T) catalog.Catalog {
	t.Helper()

	offerings := testutil.ReportOfferings()

	for i := range offerings {
		offering := &offerings[i]

		for j := range offering.Plans {
			plan := &offering.Plans[j]

			for _, price := range plan.Prices {
				if slices.Contains(price.Regions, testutil.PricedDefaultRegion) {
					plan.Prices = []catalog.Price{{Amount: price.Amount, Currency: price.Currency}}
				}
			}
		}

		if slices.Contains(offering.Categories, taxonomy.ServerlessFunction) {
			offering.Regions = []string{testutil.ReportSecondRegion}
		}
	}

	cat, err := catalog.New(estimateTargetName, testutil.PricedSnapshot(), offerings)
	if err != nil {
		t.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

// estimateTargetName is the display name of the catalogs the estimate tests
// assess against.
const estimateTargetName = "Example Cloud"

// TestRunAssessRegionSpacesAfterCommas pins that the --region value a shell
// completes with a space after each comma is accepted: each <target>=<region>
// pair is trimmed, so both targets of a comparison name their region and the
// report says both were chosen, rather than reporting " ovhcloud" as a target
// --target does not select.
func TestRunAssessRegionSpacesAfterCommas(t *testing.T) {
	t.Parallel()

	h := newEstimateHarness(t)

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath, "--target", "all",
		"--region", estimateTargetKey+"="+testutil.ReportSecondRegion+", "+secondTargetKey+"="+
			testutil.PricedDefaultRegion)
	if code != 0 {
		t.Fatalf("Run() = %d, want 0: a space after the comma is not a target; stderr = %q", code, stderr)
	}

	// The comparison names the chosen region per target, in key order: ovhcloud
	// first, then scaleway.
	var got []string

	for line := range strings.SplitSeq(stdout, "\n") {
		if strings.HasPrefix(line, regionLinePrefix) {
			got = append(got, line)
		}
	}

	want := []string{
		regionLinePrefix + "`" + estimateTargetName + "`: `" + testutil.PricedDefaultRegion +
			"` (" + regionChosenMark + " `--region`)",
		regionLinePrefix + "`" + estimateTargetName + "`: `" + testutil.ReportSecondRegion +
			"` (" + regionChosenMark + " `--region`)",
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("region lines mismatch (-want +got):\n%s\nreport:\n%s", diff, stdout)
	}
}

// TestRunAssessIsDeterministic pins that the same inventory twice gives the same
// report, byte for byte, estimates included.
func TestRunAssessIsDeterministic(t *testing.T) {
	t.Parallel()

	h := newEstimateHarness(t)

	code, first, stderr := h.run(t.Context(), assessArgs()...)
	if code != 0 {
		t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
	}

	code, second, stderr := h.run(t.Context(), assessArgs()...)
	if code != 0 {
		t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
	}

	if first != second {
		t.Errorf("two runs of the same inventory differ:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
