package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/cli"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The target keys a multi-target harness supports: the keys the composition root
// wires (cmd/shift2eu), lower case and one word.
const (
	ovhcloudKey = "ovhcloud"
	scalewayKey = "scaleway"
	upcloudKey  = "upcloud"
)

// allKey is the --target value that selects every supported target.
const allKey = "all"

// supportedTargets is the supported-target list the CLI gives in a usage error
// and in the --target line of its usage text, sorted and spelled as it joins
// them.
const supportedTargets = ovhcloudKey + ", " + scalewayKey + ", " + upcloudKey

// fakeComparisonReporter is an analysis.ComparisonReporter that writes one line
// naming the targets it was given, in order, and then returns a fixed error. It
// records the comparison and the metadata it was handed, so a test can check
// which targets a comparison covered, in which order, and what the CLI told the
// reporter about the run.
type fakeComparisonReporter struct {
	err error
	// meta and comparison are what the last call was given.
	meta       analysis.ReportMeta
	comparison assessment.Comparison
	// calls is the number of times ReportComparison was called.
	calls int
}

func (r *fakeComparisonReporter) ReportComparison(ctx context.Context, w io.Writer, c assessment.Comparison,
	meta analysis.ReportMeta,
) error {
	r.calls++
	r.comparison = c
	r.meta = meta

	if err := ctx.Err(); err != nil {
		return err
	}

	// The body is written before a failure, as a reporter may write part of its
	// report and then fail: the CLI must not let that reach the user.
	if _, err := io.WriteString(w, comparisonBody(comparisonKeys(c))); err != nil {
		return err
	}

	return r.err
}

// comparisonBody is what fakeComparisonReporter writes for targets, in order, so
// a test can see which targets the comparison covered and in which order.
func comparisonBody(keys []string) string {
	return "# comparison: " + strings.Join(keys, ", ") + "\n"
}

// comparisonKeys returns the target keys of c, in order.
func comparisonKeys(c assessment.Comparison) []string {
	keys := make([]string, 0, len(c.Targets))
	for _, target := range c.Targets {
		keys = append(keys, target.Key)
	}

	return keys
}

// targetListHarness holds the fakes a --target list test runs the CLI with: one
// fake catalog source per supported target, a fake inventory source, and both
// reporters of docs/adr/0016.
type targetListHarness struct {
	inventory  *fakeInventorySource
	catalogs   map[string]*fakeCatalogSource
	reporter   *fakeReporter
	comparison *fakeComparisonReporter
	// jsonReporter and jsonComparison are the reporters of the "json" format,
	// so a test can tell which format a run picked.
	jsonReporter   *fakeReporter
	jsonComparison *fakeComparisonReporter
	// paths is every --inventory value the CLI passed to NewInventorySource.
	paths []string
	// built is every target key the CLI built a catalog source for, in order.
	built []string
}

// newTargetListHarness returns a harness whose fakes succeed, with the fixture
// catalog of each supported target.
func newTargetListHarness(t *testing.T) *targetListHarness {
	t.Helper()

	inv, err := inventory.New("aws", []inventory.Resource{{
		ID:       "i-0123456789abcdef0",
		RawType:  "AWS::EC2::Instance",
		Category: taxonomy.ComputeVM,
		Region:   "eu-west-1",
	}})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return &targetListHarness{
		inventory: &fakeInventorySource{inv: inv},
		catalogs: map[string]*fakeCatalogSource{
			ovhcloudKey: {cat: testutil.OVHcloudCatalog(t)},
			scalewayKey: {cat: testutil.ScalewayCatalog(t)},
			upcloudKey:  {cat: testutil.UpCloudCatalog(t)},
		},
		reporter:       &fakeReporter{body: []byte(reportBody)},
		comparison:     &fakeComparisonReporter{},
		jsonReporter:   &fakeReporter{body: []byte(jsonReportBody)},
		jsonComparison: &fakeComparisonReporter{},
	}
}

// deps returns the Deps the CLI is run with: every supported target, the
// single-target reporter and, beside it, the reporter that renders a comparison
// (docs/adr/0016). The CLI picks one use case or the other by the number of
// targets selected.
func (h *targetListHarness) deps() cli.Deps {
	targets := make(map[string]func() analysis.CatalogSource, len(h.catalogs))

	for key, source := range h.catalogs {
		targets[key] = func() analysis.CatalogSource {
			h.built = append(h.built, key)

			return source
		}
	}

	return cli.Deps{
		NewInventorySource: func(path string) analysis.InventorySource {
			h.paths = append(h.paths, path)

			return h.inventory
		},
		Targets: targets,
		Formats: map[string]cli.Format{
			markdownFormat: {
				NewReporter:           func(int) analysis.Reporter { return h.reporter },
				NewComparisonReporter: func(int) analysis.ComparisonReporter { return h.comparison },
			},
			jsonFormat: {
				NewReporter:           func(int) analysis.Reporter { return h.jsonReporter },
				NewComparisonReporter: func(int) analysis.ComparisonReporter { return h.jsonComparison },
			},
		},
	}
}

// run runs the CLI with args and returns the exit code, stdout and stderr.
func (h *targetListHarness) run(ctx context.Context, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer

	code = cli.Run(ctx, args, &out, &errOut, h.deps())

	return code, out.String(), errOut.String()
}

// catalogCalls returns the number of times the catalog of every supported target
// was loaded, added up.
func (h *targetListHarness) catalogCalls() int {
	total := 0

	for _, source := range h.catalogs {
		total += source.calls
	}

	return total
}

// checkUnselectedCatalogsNotLoaded fails the test unless every supported target
// outside selected had its catalog left alone: a run must load only the catalogs
// of the targets it compares.
func (h *targetListHarness) checkUnselectedCatalogsNotLoaded(t *testing.T, selected []string) {
	t.Helper()

	for key, source := range h.catalogs {
		if slicesContain(selected, key) {
			continue
		}

		if source.calls != 0 {
			t.Errorf("the %s catalog was loaded %d times, want 0: it was not selected",
				key, source.calls)
		}
	}
}

// slicesContain reports whether values holds want.
func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

// TestRunAssessTargetList pins target selection: --target takes one key, a
// comma-separated list of keys, or all, and a multi-target run compares the
// selected targets in sorted key order, whatever order they were given in,
// reading the inventory once and rendering through the comparison reporter.
func TestRunAssessTargetList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// target is the --target value.
		target string
		// wantKeys is the target list the comparison must cover, in order.
		wantKeys []string
	}{
		{
			name:     "two targets given out of order",
			target:   scalewayKey + "," + ovhcloudKey,
			wantKeys: []string{ovhcloudKey, scalewayKey},
		},
		{
			name:     "the same two targets given in order",
			target:   ovhcloudKey + "," + scalewayKey,
			wantKeys: []string{ovhcloudKey, scalewayKey},
		},
		{
			name:     "all targets",
			target:   allKey,
			wantKeys: []string{ovhcloudKey, scalewayKey, upcloudKey},
		},
		{
			name:     "all three targets named",
			target:   upcloudKey + "," + ovhcloudKey + "," + scalewayKey,
			wantKeys: []string{ovhcloudKey, scalewayKey, upcloudKey},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newTargetListHarness(t)

			code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
				"--target", tc.target)

			if code != 0 {
				t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
			}

			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}

			// The comparison is handed the selected targets in sorted key order, so
			// the report does not depend on the order on the command line.
			if diff := cmp.Diff(tc.wantKeys, comparisonKeys(h.comparison.comparison)); diff != "" {
				t.Errorf("the comparison covers other targets than %q (-want +got):\n%s", tc.wantKeys, diff)
			}

			// stdout is what the comparison reporter wrote for the targets it was
			// handed, so two orderings on the command line give the same report.
			if want := comparisonBody(tc.wantKeys); stdout != want {
				t.Errorf("stdout = %q, want the comparison reporter's output %q", stdout, want)
			}

			if h.comparison.calls != 1 {
				t.Errorf("the comparison reporter was called %d times, want 1", h.comparison.calls)
			}

			// A multi-target run renders a comparison: the single-target reporter,
			// which would render one target only, is left alone.
			if h.reporter.calls != 0 {
				t.Errorf("the single-target reporter was called %d times, want 0 for a multi-target run",
					h.reporter.calls)
			}

			if got := h.comparison.meta.InventoryName; got != inventoryName {
				t.Errorf("ReportMeta.InventoryName = %q, want the base name %q", got, inventoryName)
			}

			// An inventory is read once per run, not once per target.
			if h.inventory.calls != 1 {
				t.Errorf("the inventory was read %d times, want 1 for a run of %d targets",
					h.inventory.calls, len(tc.wantKeys))
			}

			if len(h.paths) != 1 || h.paths[0] != inventoryPath {
				t.Errorf("NewInventorySource() was called with %q, want [%q]", h.paths, inventoryPath)
			}

			for _, key := range tc.wantKeys {
				if h.catalogs[key].calls != 1 {
					t.Errorf("the %s catalog was loaded %d times, want 1", key, h.catalogs[key].calls)
				}
			}

			h.checkUnselectedCatalogsNotLoaded(t, tc.wantKeys)

			// Only the selected targets get a catalog source at all.
			built := append([]string{}, h.built...)
			wantBuilt := append([]string{}, tc.wantKeys...)
			slices.Sort(built)
			slices.Sort(wantBuilt)

			if diff := cmp.Diff(wantBuilt, built); diff != "" {
				t.Errorf("the CLI built catalog sources for other targets than the selected ones (-want +got):\n%s",
					diff)
			}
		})
	}
}

// TestRunAssessSingleTargetIsUnchanged pins that selecting one target keeps the
// single-target path: the single-target reporter renders the report, only that
// target's catalog is loaded, and the comparison reporter is never built, so the
// output stays exactly what it is today.
func TestRunAssessSingleTargetIsUnchanged(t *testing.T) {
	t.Parallel()

	h := newTargetListHarness(t)

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
		"--target", scalewayKey)

	if code != 0 {
		t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
	}

	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	if stdout != reportBody {
		t.Errorf("stdout = %q, want the single-target reporter's output %q", stdout, reportBody)
	}

	if h.reporter.calls != 1 {
		t.Errorf("the single-target reporter was called %d times, want 1", h.reporter.calls)
	}

	if h.comparison.calls != 0 {
		t.Errorf("the comparison reporter was called %d times, want 0 for one target",
			h.comparison.calls)
	}

	if h.catalogs[scalewayKey].calls != 1 {
		t.Errorf("the %s catalog was loaded %d times, want 1", scalewayKey, h.catalogs[scalewayKey].calls)
	}

	h.checkUnselectedCatalogsNotLoaded(t, []string{scalewayKey})
}

// TestRunAssessFormatComparison pins --format for a comparison: the format's own
// comparison reporter renders the comparison of several targets, and the other
// formats' reporters are left alone.
func TestRunAssessFormatComparison(t *testing.T) {
	t.Parallel()

	target := ovhcloudKey + "," + scalewayKey

	tests := []struct {
		name string
		// format is the --format value, or empty when the run passes none.
		format string
		// wantJSON is true when the "json" format must have rendered the report.
		wantJSON bool
	}{
		{name: "no format is markdown"},
		{name: "markdown named explicitly", format: markdownFormat},
		{name: "json", format: jsonFormat, wantJSON: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newTargetListHarness(t)

			args := []string{"assess", "--inventory", inventoryPath, "--target", target}
			if tc.format != "" {
				args = append(args, "--format", tc.format)
			}

			code, stdout, stderr := h.run(t.Context(), args...)

			if code != 0 {
				t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
			}

			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}

			if want := comparisonBody([]string{ovhcloudKey, scalewayKey}); stdout != want {
				t.Errorf("stdout = %q, want the selected format's comparison output %q", stdout, want)
			}

			wantMarkdown, wantJSON := 1, 0
			if tc.wantJSON {
				wantMarkdown, wantJSON = 0, 1
			}

			if h.comparison.calls != wantMarkdown {
				t.Errorf("the markdown comparison reporter was called %d times, want %d",
					h.comparison.calls, wantMarkdown)
			}

			if h.jsonComparison.calls != wantJSON {
				t.Errorf("the json comparison reporter was called %d times, want %d",
					h.jsonComparison.calls, wantJSON)
			}

			// A comparison never renders through a single-target reporter.
			if h.reporter.calls != 0 || h.jsonReporter.calls != 0 {
				t.Errorf("a single-target reporter was called %d (markdown) and %d (json) times, want 0",
					h.reporter.calls, h.jsonReporter.calls)
			}
		})
	}
}

// TestRunAssessTargetListUsage pins every usage error of a --target list: exit
// code 2, an error line that names the bad element and lists the supported
// targets sorted, the usage text below it, nothing on stdout and nothing read.
func TestRunAssessTargetListUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// target is the --target value that is wrong.
		target string
		// wantFirstLine lists values the error line, the first line of stderr,
		// must hold besides the supported-target list.
		wantFirstLine []string
	}{
		{
			name:          "an unknown key",
			target:        ovhcloudKey + ",aws",
			wantFirstLine: []string{`"aws"`},
		},
		{
			name:          "a repeated key",
			target:        scalewayKey + "," + scalewayKey,
			wantFirstLine: []string{`"` + scalewayKey + `"`},
		},
		{
			name:          "an empty element in the middle",
			target:        ovhcloudKey + ",," + scalewayKey,
			wantFirstLine: []string{"empty"},
		},
		{
			name:          "a leading comma",
			target:        "," + ovhcloudKey,
			wantFirstLine: []string{"empty"},
		},
		{
			name:          "a trailing comma",
			target:        ovhcloudKey + ",",
			wantFirstLine: []string{"empty"},
		},
		{
			name:          "all mixed with a key",
			target:        allKey + "," + ovhcloudKey,
			wantFirstLine: []string{allKey},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newTargetListHarness(t)

			code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
				"--target", tc.target)

			if code != 2 {
				t.Errorf("Run() = %d, want 2; stderr = %q", code, stderr)
			}

			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}

			if !strings.Contains(strings.ToLower(stderr), "usage") {
				t.Errorf("stderr = %q, want usage text", stderr)
			}

			firstLine, _, _ := strings.Cut(stderr, "\n")

			// The list a user who mistypes a target needs is on the error line, in
			// the same sorted form the --target usage line gives it.
			if !strings.Contains(firstLine, supportedTargets) {
				t.Errorf("the first line %q, want it to list the supported targets as %q",
					firstLine, supportedTargets)
			}

			// The value in lowercase so that a message may start its sentence with
			// the bad element.
			lowered := strings.ToLower(firstLine)

			for _, want := range tc.wantFirstLine {
				if !strings.Contains(lowered, strings.ToLower(want)) {
					t.Errorf("the first line %q, want it to name the bad element %q", firstLine, want)
				}
			}

			// Nothing was read and no adapter was built.
			if len(h.paths) != 0 || len(h.built) != 0 {
				t.Errorf("a usage error built adapters: inventory paths %q, targets %q",
					h.paths, h.built)
			}

			if got := h.catalogCalls(); got != 0 {
				t.Errorf("a usage error loaded %d catalogs, want 0", got)
			}

			if h.inventory.calls != 0 || h.reporter.calls != 0 || h.comparison.calls != 0 {
				t.Errorf("a usage error read the inventory %d times and rendered %d single and %d "+
					"comparison reports, want 0 of each",
					h.inventory.calls, h.reporter.calls, h.comparison.calls)
			}
		})
	}
}

// TestRunAssessTargetFlagHelp pins that the usage text tells the user what
// --target accepts: one key, a comma-separated list of them, or all, and which
// keys it supports. Without that line the list form is undiscoverable.
func TestRunAssessTargetFlagHelp(t *testing.T) {
	t.Parallel()

	h := newTargetListHarness(t)

	code, stdout, stderr := h.run(t.Context(), "assess", "--help")

	if code != 0 {
		t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
	}

	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	// The --target flag's entry is its line and the continuation lines below it,
	// which the usage text wraps.
	targetFlag := flagHelp(t, stdout, "--target")

	lowered := strings.ToLower(targetFlag)

	for _, want := range []string{"comma", allKey} {
		if !strings.Contains(lowered, want) {
			t.Errorf("the --target usage entry %q, want it to mention %q", targetFlag, want)
		}
	}

	if !strings.Contains(targetFlag, supportedTargets) {
		t.Errorf("the --target usage entry %q, want it to list the supported targets as %q",
			targetFlag, supportedTargets)
	}
}

// flagHelp returns the usage entry of one assess flag: its line, and the
// continuation lines the usage text wraps it onto, joined with single spaces.
func flagHelp(t *testing.T, usage, name string) string {
	t.Helper()

	lines := strings.Split(usage, "\n")

	start := slices.IndexFunc(lines, func(line string) bool {
		return strings.HasPrefix(strings.TrimSpace(line), name)
	})
	if start < 0 {
		t.Fatalf("the usage text has no %s entry:\n%s", name, usage)
	}

	help := strings.TrimSpace(lines[start])

	for _, line := range lines[start+1:] {
		trimmed := strings.TrimSpace(line)

		// The entry ends at the next flag, at a blank line or at the next
		// section's heading.
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			break
		}

		help += " " + trimmed
	}

	return help
}

// TestRunAssessTargetListRuntimeError pins that a selected target that fails
// fails the whole run: exit code 1, one error line naming that target, nothing on
// stdout, no report file and no temporary file. A run with several targets must
// not show a comparison that silently lacks one of them.
func TestRunAssessTargetListRuntimeError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// target is the --target value.
		target string
		// failingCatalog is the key of the target whose catalog fails to load, or
		// empty when the comparison reporter fails instead.
		failingCatalog string
		// reporterFails makes the comparison reporter write part of its report and
		// then fail.
		reporterFails bool
	}{
		{name: "the first target of all", target: allKey, failingCatalog: ovhcloudKey},
		{name: "the middle target of all", target: allKey, failingCatalog: scalewayKey},
		{name: "the last target of a list", target: ovhcloudKey + "," + upcloudKey, failingCatalog: upcloudKey},
		{name: "the comparison report fails", target: allKey, reporterFails: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			dest := filepath.Join(dir, "report.md")

			h := newTargetListHarness(t)

			switch {
			case tc.failingCatalog != "":
				h.catalogs[tc.failingCatalog].err = errors.New("the catalog snapshot is malformed")
			case tc.reporterFails:
				h.comparison.err = errors.New("the comparison could not be rendered")
			}

			code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
				"--target", tc.target, "--output", dest)

			if code != 1 {
				t.Errorf("Run() = %d, want 1; stderr = %q", code, stderr)
			}

			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}

			checkErrorLine(t, stderr)

			if tc.failingCatalog != "" {
				firstLine, _, _ := strings.Cut(stderr, "\n")

				// The error names the target that failed, so a user knows which of
				// the compared targets to fix.
				if !strings.Contains(firstLine, tc.failingCatalog) {
					t.Errorf("the first line %q, want it to name the failing target %q",
						firstLine, tc.failingCatalog)
				}

				for _, other := range []string{ovhcloudKey, scalewayKey, upcloudKey} {
					if other == tc.failingCatalog {
						continue
					}

					if strings.Contains(firstLine, other) {
						t.Errorf("the first line %q names the target %q that did not fail",
							firstLine, other)
					}
				}
			}

			// The report is never written: no destination and no temporary file.
			checkOnlyFile(t, dir, "")
		})
	}
}
