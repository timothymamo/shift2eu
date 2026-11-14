//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/testutil"
)

const (
	// ovhcloudKey, scalewayKey and upcloudKey are the --target keys the
	// composition root wires: the targets the binary offers.
	ovhcloudKey = "ovhcloud"
	scalewayKey = "scaleway"
	upcloudKey  = "upcloud"
	// errPrefix is how the binary writes an error to stderr.
	errPrefix = "shift2eu: "
	// maxErrorLine is the longest error line the binary may write: the cap on
	// the message, the prefix, a short truncation marker and the newline.
	maxErrorLine = 1024 + len(errPrefix) + 64 + 1
	// summaryHeading is the heading that starts the table of fits.
	summaryHeading = "## Summary"
	// resourcesHeading is the heading that starts the table of verdicts.
	resourcesHeading = "## Resources"
)

// targetCase is one target the binary offers: how its catalog names it, what
// its catalog snapshot declares, and the golden report the shared fixture
// renders for it.
type targetCase struct {
	// key is the value of --target.
	key string
	// name is the target as its catalog names it, which the report's title and
	// its Catalog line show.
	name string
	// sourceURL and retrievedOn are the snapshot's source and retrieval date as
	// the target's catalog declares them; the report's Catalog line shows both.
	sourceURL   string
	retrievedOn string
	// golden is the report the shared fixture renders for this target.
	golden string
}

// targets returns the targets the binary offers, in the order its usage error
// sorts them, with the golden report of the shared fixture for each. A target
// added later gets a row here and a golden file beside assess-scaleway.golden.
//
// The name, source and date are repeated here on purpose: -update rewrites a
// golden file from whatever the binary printed, so only an assertion of its own
// keeps a report from naming the wrong target or citing the wrong snapshot.
func targets() []targetCase {
	return []targetCase{
		{
			key:         ovhcloudKey,
			name:        "OVHcloud",
			sourceURL:   "https://www.ovhcloud.com/en-ie/public-cloud/prices/",
			retrievedOn: "2026-11-13",
			golden:      "assess-ovhcloud.golden",
		},
		{
			key:         scalewayKey,
			name:        "Scaleway",
			sourceURL:   "https://www.scaleway.com/en/pricing/",
			retrievedOn: "2026-11-13",
			golden:      "assess-scaleway.golden",
		},
		{
			key:         upcloudKey,
			name:        "UpCloud",
			sourceURL:   "https://upcloud.com/pricing/",
			retrievedOn: "2026-11-13",
			golden:      "assess-upcloud.golden",
		},
	}
}

// binaryPath is the shift2eu binary every test runs, built once by TestMain.
var binaryPath string

// serverBinaryPath is the shift2eu-server binary the server tests run, built beside
// shift2eu by TestMain. serverBuildErr holds why it could not be built, which the
// server tests report: the CLI's own tests say whether the CLI works even when
// the server does not build, so one binary's build failure cannot hide the
// other's results.
var (
	serverBinaryPath string
	serverBuildErr   error
)

// emptyEnv runs the binary with no environment at all. The assess command needs
// no credentials, no HOME and no TMPDIR: the inventory path comes from the
// argument, the catalog is embedded and --output writes next to its destination.
func emptyEnv() []string {
	return []string{}
}

// TestMain builds the binary once, into a directory of its own, and removes it
// afterwards. The go tool itself runs in the test's environment; only the
// binary runs without one, in emptyEnv.
func TestMain(m *testing.M) {
	code, err := buildAndRun(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	os.Exit(code)
}

// buildAndRun builds the binary and runs the tests, returning the exit code.
func buildAndRun(m *testing.M) (int, error) {
	dir, err := os.MkdirTemp("", "shift2eu-e2e-")
	if err != nil {
		return 0, fmt.Errorf("create the directory for the binary: %w", err)
	}

	defer os.RemoveAll(dir)

	binaryPath = filepath.Join(dir, "shift2eu")

	build := exec.Command("go", "build", "-trimpath", "-o", binaryPath, filepath.Join("..", "..", "cmd", "shift2eu"))
	if out, err := build.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("build shift2eu: %w\n%s", err, out)
	}

	serverBinaryPath = filepath.Join(dir, "shift2eu-server")

	server := exec.Command("go", "build", "-trimpath", "-o", serverBinaryPath, filepath.Join("..", "..", "cmd", "shift2eu-server"))
	if out, err := server.CombinedOutput(); err != nil {
		serverBuildErr = fmt.Errorf("build shift2eu-server: %w\n%s", err, out)
	}

	return m.Run(), nil
}

// result is one run of the binary.
type result struct {
	code   int
	stdout string
	stderr string
}

// run runs the binary with env and args and returns what it printed and the
// exit code it chose. A run the test cannot start at all fails the test.
func run(t *testing.T, env []string, args ...string) result {
	t.Helper()

	// exec.Cmd reads a nil Env as "inherit the parent environment", so a nil
	// here would run the binary with the test's own environment, the agents'
	// credentials included, and every test in this package would still pass.
	// Refuse it instead of quietly widening what the binary sees.
	if env == nil {
		t.Fatal("run needs an explicit environment; nil would inherit the test's")
	}

	var stdout, stderr bytes.Buffer

	cmd := exec.Command(binaryPath, args...)
	// A copy, so that no caller can change the environment of a run it already
	// handed over.
	cmd.Env = append([]string{}, env...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var exitErr *exec.ExitError

	switch {
	case err == nil:
		return result{code: 0, stdout: stdout.String(), stderr: stderr.String()}
	case errors.As(err, &exitErr):
		return result{code: exitErr.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
	default:
		t.Fatalf("running shift2eu %q: %v", args, err)

		return result{}
	}
}

// assess runs `shift2eu assess --inventory inventory --target target` with no
// environment and returns the result.
func assess(t *testing.T, target, inventory string, extra ...string) result {
	t.Helper()

	args := append([]string{"assess", "--inventory", inventory, "--target", target}, extra...)

	return run(t, emptyEnv(), args...)
}

// goldenPath names a golden file of this package.
func goldenPath(name string) string {
	return filepath.Join("testdata", name)
}

// TestAssessReport pins the whole path end to end: the built binary, run with no
// environment, turns the fixture inventory into the report in each target's
// golden file. A golden is the specification of the report for that target: it
// shows the target, the catalog snapshot, the inventory's base name, the
// summary, one row per resource with its fit and reasons, the required changes
// and the blockers. The fixture lists its resources in an order of its own, not
// in ID order, because the report orders the rows itself and must not depend on
// the file's order.
func TestAssessReport(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	for _, tc := range targets() {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			got := assess(t, tc.key, inventory)

			if got.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
			}

			if got.stderr != "" {
				t.Errorf("stderr = %q, want empty", got.stderr)
			}

			testutil.Golden(t, goldenPath(tc.golden), []byte(got.stdout))

			// The report names the target and cites the snapshot its catalog
			// declares, as the target's adapter holds them.
			wantLines := []string{
				"# Migration assessment: `" + tc.name + "`",
				"- Catalog: `" + tc.name + "` snapshot from `" + tc.sourceURL + "`, retrieved " + tc.retrievedOn,
			}

			for _, want := range wantLines {
				if !strings.Contains(got.stdout, want) {
					t.Errorf("the report does not hold %q:\n%s", want, got.stdout)
				}
			}
		})
	}
}

// TestAssessOutput pins --output: the report file holds exactly what stdout
// would have held, and stdout stays empty.
func TestAssessOutput(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	for _, tc := range targets() {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			dest := filepath.Join(t.TempDir(), "report.md")

			got := assess(t, tc.key, inventory, "--output", dest)

			if got.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
			}

			if got.stdout != "" {
				t.Errorf("stdout = %q, want empty with --output", got.stdout)
			}

			if got.stderr != "" {
				t.Errorf("stderr = %q, want empty", got.stderr)
			}

			written, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("the report file was not written: %v", err)
			}

			testutil.Golden(t, goldenPath(tc.golden), written)
		})
	}
}

// jsonAssessmentGolden and jsonComparisonGolden are the JSON documents the shared
// fixture renders: one target's assessment and the comparison of every target
// (PRD #103, milestone 1).
const (
	jsonAssessmentGolden = "assess-ovhcloud-json.golden"
	jsonComparisonGolden = "assess-all-json.golden"
	// jsonHostileGolden is the JSON document of the hostile inventory: the values
	// the Markdown golden shows, JSON-encoded instead of shown as text.
	jsonHostileGolden = "assess-hostile-json.golden"
	// jsonFormat is the --format value that selects the JSON document.
	jsonFormat = "json"
	// markdownFormat is the --format value that selects the Markdown report, which
	// is also what a run without --format writes.
	markdownFormat = "markdown"
)

// TestAssessJSONFormat pins --format json end to end: the built binary writes the
// versioned JSON document for one target and the comparison document for all of
// them, naming the shared inventory by its base name. The hostile inventory's
// document pins that the same escaping holds through the CLI: its values reach
// stdout as JSON string escapes, so the document is valid JSON and valid UTF-8
// and holds no raw <, > or &.
func TestAssessJSONFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		// inventory is the --inventory file, relative to this package, or empty
		// for the shared fixture.
		inventory string
		golden    string
		kind      string
		// inventoryName is the base name the document must name.
		inventoryName string
		// escapesHTML is true for the fixture that holds the characters JSON
		// escapes: <, > and &.
		escapesHTML bool
	}{
		{
			name:          "one target is an assessment",
			target:        ovhcloudKey,
			golden:        jsonAssessmentGolden,
			kind:          "assessment",
			inventoryName: "inventory.json",
		},
		{
			name:          "every target is a comparison",
			target:        allKey,
			golden:        jsonComparisonGolden,
			kind:          "comparison",
			inventoryName: "inventory.json",
		},
		{
			name:          "hostile values are escaped",
			target:        scalewayKey,
			inventory:     filepath.Join("testdata", "inventory-hostile.json"),
			golden:        jsonHostileGolden,
			kind:          "assessment",
			inventoryName: "inventory-hostile.json",
			escapesHTML:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inventory := tc.inventory
			if inventory == "" {
				inventory = filepath.Join("testdata", "inventory.json")
			}

			got := assess(t, tc.target, inventory, "--format", jsonFormat)

			if got.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
			}

			if got.stderr != "" {
				t.Errorf("stderr = %q, want empty", got.stderr)
			}

			if !json.Valid([]byte(got.stdout)) {
				t.Fatalf("stdout is not valid JSON:\n%s", got.stdout)
			}

			if !utf8.ValidString(got.stdout) {
				t.Fatalf("stdout is not valid UTF-8:\n%q", got.stdout)
			}

			testutil.Golden(t, goldenPath(tc.golden), []byte(got.stdout))

			var header struct {
				SchemaVersion int    `json:"schemaVersion"`
				Kind          string `json:"kind"`
				InventoryName string `json:"inventoryName"`
			}

			if err := json.Unmarshal([]byte(got.stdout), &header); err != nil {
				t.Fatalf("decoding the document: %v", err)
			}

			if header.SchemaVersion != 2 {
				t.Errorf("schemaVersion = %d, want 2 (docs/adr/0030)", header.SchemaVersion)
			}

			if header.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", header.Kind, tc.kind)
			}

			if header.InventoryName != tc.inventoryName {
				t.Errorf("inventoryName = %q, want the base name %q", header.InventoryName, tc.inventoryName)
			}

			if tc.escapesHTML {
				for _, raw := range []string{"<", ">", "&"} {
					if strings.Contains(got.stdout, raw) {
						t.Errorf("stdout holds the raw character %q, so a document could be read as markup:\n%s",
							raw, got.stdout)
					}
				}
			}

			if !strings.HasSuffix(got.stdout, "\n") || strings.HasSuffix(got.stdout, "\n\n") {
				t.Errorf("stdout must end with exactly one newline")
			}
		})
	}
}

// TestAssessMarkdownFormatIsDefault pins that --format markdown is what a run
// without --format writes, byte for byte, and that the output is that target's
// golden report: the flag changes the output only when it names json.
func TestAssessMarkdownFormatIsDefault(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	for _, tc := range targets() {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			byDefault := assess(t, tc.key, inventory)
			explicit := assess(t, tc.key, inventory, "--format", markdownFormat)

			if byDefault.code != 0 || explicit.code != 0 {
				t.Fatalf("exit codes = %d and %d, want 0; stderr = %q and %q",
					byDefault.code, explicit.code, byDefault.stderr, explicit.stderr)
			}

			if explicit.stdout != byDefault.stdout {
				t.Errorf("--format %s differs from the default output:\ndefault:\n%s\nexplicit:\n%s",
					markdownFormat, byDefault.stdout, explicit.stdout)
			}

			testutil.Golden(t, goldenPath(tc.golden), []byte(explicit.stdout))
		})
	}
}

// TestAssessEstimatesAreOnByDefault pins PRD #190 milestone 8's default on the
// real binary: a run without --no-estimate estimates, so the report carries the
// Plan and Monthly estimate columns and the cost summary, and the run that turns
// them off carries no price and no cost summary.
func TestAssessEstimatesAreOnByDefault(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	byDefault := assess(t, scalewayKey, inventory)
	if byDefault.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", byDefault.code, byDefault.stderr)
	}

	for _, want := range []string{"| Plan |", "| Monthly estimate |", "## Cost estimate"} {
		if !strings.Contains(byDefault.stdout, want) {
			t.Errorf("the report of a run without --no-estimate does not hold %q, so it does not "+
				"estimate by default:\n%s", want, byDefault.stdout)
		}
	}

	t.Run("--no-estimate", func(t *testing.T) {
		t.Parallel()

		got := assess(t, scalewayKey, inventory, "--no-estimate")
		if got.code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
		}

		if got.stderr != "" {
			t.Errorf("stderr = %q, want empty", got.stderr)
		}

		for _, unwanted := range []string{"€", "| Plan |", "| Monthly estimate |", "## Cost estimate"} {
			if strings.Contains(got.stdout, unwanted) {
				t.Errorf("the report of a run with --no-estimate holds %q:\n%s", unwanted, got.stdout)
			}
		}

		if !strings.Contains(got.stdout, resourcesHeading) {
			t.Errorf("the report holds no %q section:\n%s", resourcesHeading, got.stdout)
		}
	})
}

// TestAssessRegion pins --region end to end: a region the target lists is the
// region the report names, and a region it does not list, or one named for a
// target the run does not select, is a usage error (exit 2) whose one error line
// names the element at fault, with nothing on stdout and no report written.
func TestAssessRegion(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	t.Run("a region the target lists", func(t *testing.T) {
		t.Parallel()

		const region = "nl-ams"

		got := assess(t, scalewayKey, inventory, "--region", scalewayKey+"="+region)
		if got.code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
		}

		if got.stderr != "" {
			t.Errorf("stderr = %q, want empty", got.stderr)
		}

		var regionLines []string

		for line := range strings.SplitSeq(got.stdout, "\n") {
			if strings.HasPrefix(line, "- Region: ") {
				regionLines = append(regionLines, line)
			}
		}

		if len(regionLines) != 1 {
			t.Fatalf("the report holds %d region lines %q, want 1:\n%s", len(regionLines), regionLines,
				got.stdout)
		}

		if !strings.HasPrefix(regionLines[0], "- Region: `"+region+"`") {
			t.Errorf("the region line = %q, want it to name %q", regionLines[0], region)
		}
	})

	tests := []struct {
		name string
		// args are the arguments after the inventory and target of the run.
		args []string
		// want is what the error line must name.
		want []string
	}{
		{
			name: "a region the target does not list",
			args: []string{"--region", scalewayKey + "=xx-nope"},
			want: []string{"xx-nope", "fr-par"},
		},
		{
			name: "a region for a target the run does not select",
			args: []string{"--region", ovhcloudKey + "=GRA"},
			want: []string{ovhcloudKey},
		},
		{
			name: "a malformed pair",
			args: []string{"--region", scalewayKey},
			want: []string{scalewayKey},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dest := filepath.Join(t.TempDir(), "report.md")

			got := assess(t, scalewayKey, inventory, append(slices.Clone(tc.args), "--output", dest)...)
			if got.code != 2 {
				t.Errorf("exit code = %d, want 2 (a usage error); stderr = %q", got.code, got.stderr)
			}

			if got.stdout != "" {
				t.Errorf("stdout = %q, want empty", got.stdout)
			}

			firstLine, _, _ := strings.Cut(got.stderr, "\n")

			if !strings.HasPrefix(firstLine, errPrefix) {
				t.Errorf("the first line of stderr = %q, want it to start with %q", firstLine, errPrefix)
			}

			for _, want := range tc.want {
				if !strings.Contains(firstLine, want) {
					t.Errorf("the error line = %q, want it to name %q", firstLine, want)
				}
			}

			if _, err := os.Stat(dest); err == nil {
				t.Errorf("the report %q was written for a usage error", dest)
			}
		})
	}
}

// TestAssessFitCoverage pins that the shared fixture exercises every fit against
// the targets that serve no offering in a category it holds: a direct match, a
// partial match, a fit of none because the target serves no offering in the
// resource's category, and the unclassified resource. Without it a fixture edit
// could quietly stop exercising a fit and every other test here would still
// pass.
//
// Scaleway is left out: its catalog serves every category the fixture holds, so
// the fit of none from a category has nothing to pin there.
func TestAssessFitCoverage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key  string
		name string
		// unserved is a category the target serves no offering in: neither
		// OVHcloud (PRD #11) nor UpCloud (PRD #77) serves a category for AWS
		// Lambda functions.
		unserved string
	}{
		{key: ovhcloudKey, name: "OVHcloud", unserved: "serverless.function"},
		{key: upcloudKey, name: "UpCloud", unserved: "serverless.function"},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			got := assess(t, tc.key, filepath.Join("testdata", "inventory.json"))

			if got.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
			}

			rows := resourceRows(t, got.stdout)

			// The fits the report counts, as the summary table writes them.
			fits := make(map[string]int, len(rows))
			for _, row := range rows {
				fits[row.fit]++
			}

			for _, want := range []string{"direct", "partial", "none"} {
				if fits[want] == 0 {
					t.Errorf("no resource has fit %q against %s; the fixture must exercise every fit:\n%s",
						want, tc.name, got.stdout)
				}
			}

			var (
				// unserved is a row in a category the target serves no offering
				// in: the fixture's serverless.function resources.
				unserved bool
				// unclassified is the fixture's resource outside the taxonomy.
				unclassified bool
			)

			for _, row := range rows {
				switch row.category {
				case tc.unserved:
					unserved = true

					if row.fit != "none" {
						t.Errorf("the %s row has fit %q, want %q: %s serves no such category",
							row.id, row.fit, "none", tc.name)
					}

					// The reason says what is missing, never that the resource is
					// unclassified, and it names the target's own catalog.
					for _, want := range []string{"no offering in the", tc.name, tc.unserved} {
						if !strings.Contains(row.reason, want) {
							t.Errorf("the %s reason %q, want it to name %q", row.id, row.reason, want)
						}
					}
				case "unclassified":
					unclassified = true

					if row.fit != "none" {
						t.Errorf("the %s row has fit %q, want %q: it is not classified",
							row.id, row.fit, "none")
					}

					if !strings.Contains(row.reason, "not classified") {
						t.Errorf("the %s reason %q, want it to say the resource is not classified",
							row.id, row.reason)
					}
				}
			}

			if !unserved {
				t.Errorf("no %s row; the fixture must hold one for the none-from-category fit:\n%s",
					tc.unserved, got.stdout)
			}

			if !unclassified {
				t.Errorf("no unclassified row; the fixture must hold one:\n%s", got.stdout)
			}
		})
	}
}

// TestAssessReportShape pins that the shared fixture yields the same report
// skeleton for every target: the same preamble labels, the same headings in the
// same order, the same table header rows, the same Summary row labels and the
// same resource IDs in the same order. Only the target, its snapshot, the
// offerings, the fits, the counts and the reasons may differ, so a user who
// switches --target reads the same report in the same shape.
func TestAssessReportShape(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	type rendered struct {
		key   string
		shape reportShape
	}

	var renderedReports []rendered

	for _, tc := range targets() {
		got := assess(t, tc.key, inventory)

		if got.code != 0 {
			t.Errorf("%s: exit code = %d, want 0; stderr = %q", tc.key, got.code, got.stderr)

			continue
		}

		renderedReports = append(renderedReports, rendered{key: tc.key, shape: shapeOf(t, tc, got.stdout)})
	}

	// A report that failed to render has nothing to compare, and the failure is
	// already reported above.
	if len(renderedReports) != len(targets()) {
		return
	}

	want := renderedReports[0]

	for _, got := range renderedReports[1:] {
		if diff := cmp.Diff(want.shape, got.shape, cmp.AllowUnexported(reportShape{})); diff != "" {
			t.Errorf("the %s report shape differs from the %s report (-%s +%s):\n%s",
				got.key, want.key, want.key, got.key, diff)
		}
	}
}

// TestAssessReportsDoNotCrossName pins that a report names only its own target:
// no target's report says the name of another target, in any case. An adapter
// serving the wrong snapshot, or a sentence built from the wrong catalog, shows
// up here.
func TestAssessReportsDoNotCrossName(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	for _, tc := range targets() {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			got := assess(t, tc.key, inventory)

			if got.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
			}

			for _, other := range targets() {
				if other.key == tc.key {
					continue
				}

				if strings.Contains(strings.ToLower(got.stdout), strings.ToLower(other.name)) {
					t.Errorf("the %s report names %s:\n%s", tc.key, other.name, got.stdout)
				}
			}
		})
	}
}

// TestAssessHostileInventory pins that values from an inventory file reach the
// report as text: read by the real adapter, rendered by the real reporter, every
// value appears verbatim inside a code span, so no renderer can read it as a
// heading, a link, an image, math, a mention, a reference, an emoji, a task list
// or a table cell boundary.
func TestAssessHostileInventory(t *testing.T) {
	t.Parallel()

	got := assess(t, scalewayKey, filepath.Join("testdata", "inventory-hostile.json"))

	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
	}

	if got.stderr != "" {
		t.Errorf("stderr = %q, want empty", got.stderr)
	}

	testutil.Golden(t, goldenPath("assess-hostile.golden"), []byte(got.stdout))

	for _, value := range hostileValues() {
		if !hasCodeSpan(got.stdout, value) {
			t.Errorf("the report does not show %q inside a code span:\n%s", value, got.stdout)
		}
	}
}

// hostileValues returns every value of the hostile fixture that a renderer would
// otherwise rewrite, including the relation's target and the workload name,
// which reach the relations table cell and a workload heading.
func hostileValues() []string {
	return []string{
		"#1",
		"$\\Huge X$",
		"[ ] x",
		"@octocat",
		"a|b",
		"<b>",
		"tick`x",
		":x:",
		"ws|`x`[a](b) # h",
	}
}

// TestAssessErrors pins the input errors: exit code 1, one error line that names
// the file, and never a panic or a stack trace. The errors are read, not
// assessed, so they are the same for every target; each case runs against all
// of them. Input errors are raised before a catalog is loaded, so they name no
// target; the one error that names a target, a catalog load failure, is pinned
// per adapter in internal/adapter/target/ovhcloud/ovhcloud_test.go,
// internal/adapter/target/scaleway/scaleway_test.go and
// internal/adapter/target/upcloud/upcloud_test.go.
func TestAssessErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		inventory string
		// wantStderr lists the values stderr must hold besides the quoted file
		// name: the problem with the file, which tells the user what to fix.
		wantStderr []string
	}{
		{
			name:       "missing file",
			inventory:  filepath.Join("testdata", "no-such-file.json"),
			wantStderr: []string{"cannot read"},
		},
		{
			name:       "not JSON",
			inventory:  filepath.Join("testdata", "not-json.json"),
			wantStderr: []string{"malformed JSON"},
		},
		{
			// Versions 1 and 2 are read; a version 3 file is refused for its version.
			name:       "unsupported schema version",
			inventory:  filepath.Join("testdata", "schema-version-3.json"),
			wantStderr: []string{"unsupported schema version"},
		},
		{
			// A relation whose target names no resource is refused by path, in file
			// order, before any report is written.
			name:       "relation to a resource that is not in the inventory",
			inventory:  filepath.Join("testdata", "invalid-relation.json"),
			wantStderr: []string{"resources[1].relations[0].target"},
		},
		{
			name:       "unknown category",
			inventory:  filepath.Join("testdata", "unknown-category.json"),
			wantStderr: []string{"i-0123456789abcdef0", "compute.mv"},
		},
		{
			// PRD #14 milestone 4 retires high-availability: the value is refused
			// by name, never read as one of its replacements, and the error names
			// the resource, the value and both replacements so the user knows what
			// to change it to. The quoted forms keep the retired value apart from
			// `node-high-availability`, which holds it as a substring.
			name:      "retired high availability capability",
			inventory: filepath.Join("testdata", "retired-capability.json"),
			wantStderr: []string{
				"db-example",
				`"high-availability"`,
				`"node-high-availability"`,
				`"zone-high-availability"`,
			},
		},
	}

	for _, tc := range cases {
		for _, target := range targets() {
			t.Run(tc.name+"/"+target.key, func(t *testing.T) {
				t.Parallel()

				got := assess(t, target.key, tc.inventory)

				if got.code != 1 {
					t.Errorf("exit code = %d, want 1; stderr = %q", got.code, got.stderr)
				}

				if got.stdout != "" {
					t.Errorf("stdout = %q, want empty", got.stdout)
				}

				if !strings.HasPrefix(got.stderr, errPrefix) {
					t.Errorf("stderr = %q, want it to start with %q", got.stderr, errPrefix)
				}

				if !strings.HasSuffix(got.stderr, "\n") || strings.Count(got.stderr, "\n") != 1 {
					t.Errorf("stderr = %q, want exactly one line ending in a newline", got.stderr)
				}

				if len(got.stderr) > maxErrorLine {
					t.Errorf("stderr is %d bytes, want at most %d", len(got.stderr), maxErrorLine)
				}

				// The error names the file it failed on, quoted, as text.QuotePath shows
				// it.
				if want := strconv.Quote(tc.inventory); !strings.Contains(got.stderr, want) {
					t.Errorf("stderr = %q, want it to name the file as %s", got.stderr, want)
				}

				for _, want := range tc.wantStderr {
					if !strings.Contains(got.stderr, want) {
						t.Errorf("stderr = %q, want it to name %q", got.stderr, want)
					}
				}

				// Input errors are raised before a catalog is loaded, so they name
				// no target at all and this loop cannot fail today: it is a cheap
				// guard, not a pin on a message that mentions a target. The one
				// error that names a target, a catalog load failure, is pinned per
				// adapter in internal/adapter/target/ovhcloud/ovhcloud_test.go,
				// internal/adapter/target/scaleway/scaleway_test.go and
				// internal/adapter/target/upcloud/upcloud_test.go.
				for _, other := range targets() {
					if other.key == target.key {
						continue
					}

					if strings.Contains(strings.ToLower(got.stderr), strings.ToLower(other.name)) {
						t.Errorf("stderr = %q for target %s, want it to name no %s",
							got.stderr, target.key, other.name)
					}
				}

				if lower := strings.ToLower(got.stdout + got.stderr); strings.Contains(lower, "panic") ||
					strings.Contains(lower, "goroutine") {
					t.Errorf("the output holds a panic or a stack trace:\n%s\n%s", got.stdout, got.stderr)
				}
			})
		}
	}
}

// TestAssessUsage pins the usage errors of the binary: exit code 2, usage text on
// stderr, nothing on stdout, and every supported target named, sorted, for an
// unknown one. The usage text lists `ovhcloud, scaleway, upcloud` as valid values
// by design; that is a list of choices, not one target naming another.
func TestAssessUsage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		// wantStderr lists values stderr must hold besides the usage text.
		wantStderr []string
		// wantFirstLine lists values the first line, the error itself, must hold.
		wantFirstLine []string
	}{
		{
			name: "missing target",
			args: []string{
				"assess", "--inventory", filepath.Join("testdata", "inventory.json"),
			},
		},
		{
			name: "unknown target",
			args: []string{
				"assess", "--inventory", filepath.Join("testdata", "inventory.json"),
				"--target", "nope",
			},
			wantStderr:    []string{ovhcloudKey, scalewayKey, upcloudKey},
			wantFirstLine: []string{ovhcloudKey + ", " + scalewayKey + ", " + upcloudKey},
		},
		{
			name: "unknown flag",
			args: []string{
				"assess", "--inventory", filepath.Join("testdata", "inventory.json"),
				"--target", scalewayKey, "--bogus",
			},
		},
		{
			name: "unknown format",
			args: []string{
				"assess", "--inventory", filepath.Join("testdata", "inventory.json"),
				"--target", scalewayKey, "--format", "yaml",
			},
			wantStderr:    []string{"--format"},
			wantFirstLine: []string{`"yaml"`, jsonFormat, markdownFormat},
		},
		{
			name: "empty format",
			args: []string{
				"assess", "--inventory", filepath.Join("testdata", "inventory.json"),
				"--target", scalewayKey, "--format=",
			},
			wantStderr:    []string{"--format"},
			wantFirstLine: []string{"unknown format", jsonFormat, markdownFormat},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := run(t, emptyEnv(), tc.args...)

			if got.code != 2 {
				t.Errorf("exit code = %d, want 2; stderr = %q", got.code, got.stderr)
			}

			if got.stdout != "" {
				t.Errorf("stdout = %q, want empty", got.stdout)
			}

			if !strings.Contains(strings.ToLower(got.stderr), "usage") {
				t.Errorf("stderr = %q, want usage text", got.stderr)
			}

			for _, want := range tc.wantStderr {
				if !strings.Contains(got.stderr, want) {
					t.Errorf("stderr = %q, want it to name %q", got.stderr, want)
				}
			}

			// The error is the first line; the usage text follows it. A user who
			// mistypes a target learns the supported ones from that line.
			firstLine, _, _ := strings.Cut(got.stderr, "\n")
			for _, want := range tc.wantFirstLine {
				if !strings.Contains(firstLine, want) {
					t.Errorf("the first line %q, want it to name %q", firstLine, want)
				}
			}
		})
	}
}

// TestVersion pins that `shift2eu version` still works: one non-empty line, exit
// code 0, nothing on stderr.
func TestVersion(t *testing.T) {
	t.Parallel()

	got := run(t, emptyEnv(), "version")

	if got.code != 0 {
		t.Errorf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
	}

	if got.stderr != "" {
		t.Errorf("stderr = %q, want empty", got.stderr)
	}

	if strings.TrimSpace(got.stdout) == "" {
		t.Errorf("stdout = %q, want a version line", got.stdout)
	}

	if !strings.HasSuffix(got.stdout, "\n") || strings.Count(got.stdout, "\n") != 1 {
		t.Errorf("stdout = %q, want exactly one line ending in a newline", got.stdout)
	}
}

// TestAssessIsDeterministic pins that two runs on the same inventory give the
// same bytes for every target: nothing in the report depends on map order or the
// clock.
func TestAssessIsDeterministic(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	for _, tc := range targets() {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			first := assess(t, tc.key, inventory)
			second := assess(t, tc.key, inventory)

			if first.code != 0 || second.code != 0 {
				t.Fatalf("exit codes = %d and %d, want 0; stderr = %q and %q",
					first.code, second.code, first.stderr, second.stderr)
			}

			if first.stdout != second.stdout {
				t.Errorf("two runs differ:\nfirst:\n%s\nsecond:\n%s", first.stdout, second.stdout)
			}
		})
	}
}

// TestAssessPathHasNoNetworkOrCloudSDK pins the CLI criterion of the PRD: no
// package on the assess path imports a network, process or cloud SDK package, so
// nothing the command reaches can resolve a name, open a connection, start a
// process or talk to a provider. This is an import check: it says nothing about
// code that needs no import at all, such as os.StartProcess or a raw syscall
// call, which depguard refuses in the target adapters instead. It also requires
// every target adapter to be on that path, so the check covers them all instead
// of passing by absence.
func TestAssessPathHasNoNetworkOrCloudSDK(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}

	cmd := exec.Command("go", "list", "-deps", "./cmd/shift2eu")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go list -deps ./cmd/shift2eu: %v\n%s", err, exitErr.Stderr)
		}

		t.Fatalf("go list -deps ./cmd/shift2eu: %v", err)
	}

	deps := strings.Fields(string(out))

	for _, pkg := range deps {
		if forbiddenDependency(pkg) {
			t.Errorf("the assess path depends on %s; no package on it may resolve a name, open a "+
				"connection, start a process or use a cloud SDK", pkg)
		}
	}

	// The check above can only refuse what the binary imports, so the adapters
	// the CLI assesses against must be on the path: a target wired out of
	// cmd/shift2eu, or one the build drops, would pass by absence.
	for _, tc := range targets() {
		want := "github.com/timothymamo/shift2eu/internal/adapter/target/" + tc.key

		if !slices.Contains(deps, want) {
			t.Errorf("the assess path does not depend on %s: wire the %s target in cmd/shift2eu so "+
				"that the checks above cover it", want, tc.key)
		}
	}
}

// forbiddenDependency reports whether pkg is a network, process or cloud SDK
// package the assess path must not depend on.
//
// net itself resolves names and opens connections, and every net subpackage that
// does I/O is refused with it. The two exceptions are parsers and value types:
// net/url turns a string into a URL and net/netip holds an address, and neither
// reaches the network, so the domain catalog and text packages may use them. A
// TLS handshake, a Google API client and the Scaleway, OVHcloud and UpCloud SDKs
// belong to an adapter, never to the assess path.
func forbiddenDependency(pkg string) bool {
	if pureNetPackage(pkg) {
		return false
	}

	for _, denied := range []string{"net", "os/exec", "crypto/tls"} {
		if pkg == denied || strings.HasPrefix(pkg, denied+"/") {
			return true
		}
	}

	for _, denied := range []string{
		"github.com/aws/",
		"cloud.google.com/",
		"github.com/Azure/",
		"google.golang.org/api/",
		"github.com/scaleway/",
		"github.com/ovh/",
		"github.com/UpCloudLtd/",
	} {
		if strings.HasPrefix(pkg, denied) {
			return true
		}
	}

	return false
}

// pureNetPackage reports whether pkg is a net subpackage that parses text or
// holds a value and never opens a connection or resolves a name.
func pureNetPackage(pkg string) bool {
	return pkg == "net/url" || pkg == "net/netip"
}

// resourceRow is one row of a report's Resources table: the values the report
// shows, with the code spans and the escapes the renderer adds undone.
type resourceRow struct {
	id       string
	category string
	fit      string
	reason   string
}

// resourceRows returns the rows of report's Resources table, in report order,
// and fails the test when the report has no such table or an empty one. The
// columns are read by their names, not by their positions: a report gains the
// Plan, Monthly estimate and Relations columns with estimates and relations
// (PRD #190 milestone 8), and its ID, Category, Fit and Reasons columns stay the
// same ones.
func resourceRows(t *testing.T, report string) []resourceRow {
	t.Helper()

	columns := make(map[string]int)

	for i, name := range tableColumns(t, report, resourcesHeading) {
		columns[name] = i
	}

	want := []string{"ID", "Category", "Fit", "Reasons"}

	for _, name := range want {
		if _, ok := columns[name]; !ok {
			t.Fatalf("the Resources table has no %q column: %v", name, columns)
		}
	}

	var rows []resourceRow

	for _, cells := range tableDataRows(t, report, resourcesHeading) {
		if len(cells) != len(columns) {
			t.Fatalf("the Resources row %q has %d cells, the header has %d",
				strings.Join(cells, " | "), len(cells), len(columns))
		}

		rows = append(rows, resourceRow{
			id:       codeSpanText(cells[columns["ID"]]),
			category: codeSpanText(cells[columns["Category"]]),
			fit:      cells[columns["Fit"]],
			reason:   cells[columns["Reasons"]],
		})
	}

	return rows
}

// tableColumns returns the cells of the header row of the table below heading,
// in order, so a test can read a column by its name rather than by its position.
// It fails the test when the table has no header row.
func tableColumns(t *testing.T, report, heading string) []string {
	t.Helper()

	lines := strings.Split(report, "\n")

	start := slices.Index(lines, heading)
	if start < 0 {
		t.Fatalf("the report has no %q heading:\n%s", heading, report)
	}

	for i := start + 1; i+1 < len(lines); i++ {
		if isTableRow(lines[i]) && isSeparatorRow(lines[i+1]) {
			return tableCells(lines[i])
		}

		if lines[i] == "" && i > start+1 && !isTableRow(lines[i]) {
			break
		}
	}

	t.Fatalf("the report has no table below %q:\n%s", heading, report)

	return nil
}

// summaryLabels returns the first cell of every data row of report's Summary
// table, in report order: the fits the report counts and their total.
func summaryLabels(t *testing.T, report string) []string {
	t.Helper()

	var labels []string

	for _, cells := range tableDataRows(t, report, summaryHeading) {
		labels = append(labels, cells[0])
	}

	return labels
}

// tableDataRows returns the data rows of the table below heading, as cells, in
// report order: the rows after the header row and its separator, up to the
// blank line that ends the table. It fails the test when the report has no such
// heading, no table below it, or no data row.
func tableDataRows(t *testing.T, report, heading string) [][]string {
	t.Helper()

	lines := strings.Split(report, "\n")

	start := slices.Index(lines, heading)
	if start < 0 {
		t.Fatalf("the report has no %q heading:\n%s", heading, report)
	}

	// The table follows the heading: a header row, the separator row below it,
	// then one row per record.
	i := start + 1
	for i < len(lines) && !isTableRow(lines[i]) {
		i++
	}

	if i+1 >= len(lines) || !isSeparatorRow(lines[i+1]) {
		t.Fatalf("the report has no table below %q:\n%s", heading, report)
	}

	var rows [][]string

	for i += 2; i < len(lines) && isTableRow(lines[i]); i++ {
		rows = append(rows, tableCells(lines[i]))
	}

	if len(rows) == 0 {
		t.Fatalf("the table below %q has no rows:\n%s", heading, report)
	}

	return rows
}

// isTableRow reports whether line is a row of a Markdown table: it starts and
// ends with a boundary pipe.
func isTableRow(line string) bool {
	return strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|")
}

// isSeparatorRow reports whether line separates a table's header row from its
// rows: every cell of it is dashes, optionally with an alignment colon.
func isSeparatorRow(line string) bool {
	if !isTableRow(line) {
		return false
	}

	for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
		if strings.Trim(strings.TrimSpace(cell), ":-") != "" {
			return false
		}
	}

	return true
}

// tableCells splits a table row into its trimmed cells, with the backslash the
// renderer writes before a pipe inside a cell undone.
func tableCells(row string) []string {
	body := strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")

	var (
		cells []string
		cell  strings.Builder
	)

	for i := 0; i < len(body); i++ {
		switch {
		case body[i] == '\\' && i+1 < len(body) && body[i+1] == '|':
			cell.WriteByte('|')
			i++
		case body[i] == '|':
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteByte(body[i])
		}
	}

	return append(cells, strings.TrimSpace(cell.String()))
}

// codeSpanText returns the value a cell holds: the content of its code span,
// which is how the report writes every value it does not trust, or the cell
// itself when it holds none, as a fit does.
func codeSpanText(cell string) string {
	if spans := codeSpans(cell); len(spans) == 1 {
		return spans[0]
	}

	return cell
}

// reportShape is what the reports of one inventory for every target must share:
// the labels of the preamble that names the inventory, the source and the
// catalog, the headings in order, the header row of every table, the labels of
// the Summary rows in order, and the resource IDs of the Resources table in
// order. Everything else in a report depends on the target, its snapshot and the
// fits.
type reportShape struct {
	preambleLabels []string
	headings       []string
	tableHeaders   []string
	summaryLabels  []string
	resourceIDs    []string
}

// shapeOf returns the skeleton of a report for target. The title names the
// target, the one heading a report may not share with another target's, so it
// is normalised to a placeholder.
func shapeOf(t *testing.T, target targetCase, report string) reportShape {
	t.Helper()

	lines := strings.Split(report, "\n")

	var shape reportShape

	// The preamble is every line above the first "## " heading: a report labels
	// its inventory, its source and its catalog there.
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			break
		}

		// A preamble line reads `- Label: value`; only the label is shared.
		if strings.HasPrefix(line, "- ") {
			label, _, _ := strings.Cut(line, ":")
			shape.preambleLabels = append(shape.preambleLabels, label)
		}
	}

	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			shape.headings = append(shape.headings, strings.ReplaceAll(line, "`"+target.name+"`", "`TARGET`"))
			continue
		}

		// A table's header row is the row above its separator.
		if isTableRow(line) && i+1 < len(lines) && isSeparatorRow(lines[i+1]) {
			shape.tableHeaders = append(shape.tableHeaders, line)
		}
	}

	shape.summaryLabels = summaryLabels(t, report)

	for _, row := range resourceRows(t, report) {
		shape.resourceIDs = append(shape.resourceIDs, row.id)
	}

	return shape
}

// hasCodeSpan reports whether s holds a Markdown code span whose content is
// exactly value.
func hasCodeSpan(s, value string) bool {
	for _, content := range codeSpans(s) {
		if content == value {
			return true
		}
	}

	return false
}

// codeSpans returns the content of every Markdown code span in s, with the
// padding CommonMark strips and the table-cell pipe escape undone, so a test can
// check what a renderer would show.
func codeSpans(s string) []string {
	var spans []string

	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++

			continue
		}

		start := i
		for i < len(s) && s[i] == '`' {
			i++
		}

		fence := i - start
		content, next, ok := spanContent(s, i, fence)

		if !ok {
			return spans
		}

		spans = append(spans, content)
		i = next
	}

	return spans
}

// spanContent returns the content of the code span whose fence is fence
// backticks long and whose content starts at byte from, the byte after the
// closing fence, and whether there is such a span.
func spanContent(s string, from, fence int) (content string, next int, ok bool) {
	for j := from; j < len(s); {
		if s[j] != '`' {
			j++

			continue
		}

		k := j
		for k < len(s) && s[k] == '`' {
			k++
		}

		// A closing fence is exactly as long as the opening one.
		if k-j == fence {
			content = strings.TrimSuffix(strings.TrimPrefix(s[from:j], " "), " ")
			content = strings.ReplaceAll(content, `\|`, "|")

			return content, k, true
		}

		j = k
	}

	return "", 0, false
}

// allKey is the --target value that selects every supported target.
const allKey = "all"

// comparisonGolden is the report a --target all run of the shared fixture
// renders: the comparison of the three targets, then each target's section
// (PRD #89).
const comparisonGolden = "assess-all.golden"

// TestAssessAllTargets pins the multi-target report end to end: the built
// binary, run with no environment, compares the fixture inventory with every
// supported target and writes one comparison report. Its title names every
// target compared, in the key order the CLI compares them in.
//
// The golden is the specification of that report. It is written with -update by
// the change that adds the comparison output (PRD #89, milestone 3) and reviewed
// like any other golden file.
func TestAssessAllTargets(t *testing.T) {
	t.Parallel()

	got := assess(t, allKey, filepath.Join("testdata", "inventory.json"))

	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
	}

	if got.stderr != "" {
		t.Errorf("stderr = %q, want empty", got.stderr)
	}

	testutil.Golden(t, goldenPath(comparisonGolden), []byte(got.stdout))

	checkComparisonTitle(t, got.stdout)
}

// checkComparisonTitle fails the test unless the report's title names every
// target's display name, in the order the targets are compared.
func checkComparisonTitle(t *testing.T, report string) {
	t.Helper()

	lines := strings.Split(report, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "# ") {
		t.Fatalf("the report does not start with a title:\n%s", report)
	}

	title := lines[0]
	last := -1

	for _, tc := range targets() {
		i := strings.Index(title, tc.name)
		if i < 0 {
			t.Errorf("the report title %q does not name %q", title, tc.name)

			continue
		}

		if i < last {
			t.Errorf("the report title %q names %q out of target key order", title, tc.name)
		}

		last = i
	}
}

// TestAssessAllTargetsSectionsMatchSingleTargetReports pins that the comparison
// changes no assessment: each target's section is that target's single-target
// golden report, one heading level lower and without the title and header lines
// the comparison gives once, with the same content and the same row order. It
// compares with the golden file, not with a report the same binary renders at
// test time, so the comparison cannot agree with the single-target report by
// both being wrong in the same way: TestAssessReport holds each golden to what
// the binary prints.
func TestAssessAllTargetsSectionsMatchSingleTargetReports(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	all := assess(t, allKey, inventory)
	if all.code != 0 {
		t.Fatalf("exit code of --target %s = %d, want 0; stderr = %q", allKey, all.code, all.stderr)
	}

	for _, tc := range targets() {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			// The path is the calling test's own golden file, a constant in testdata,
			// never anything the test reads at run time.
			single, err := os.ReadFile(goldenPath(tc.golden))
			if err != nil {
				t.Fatalf("read the golden report of %s: %v", tc.key, err)
			}

			want := demotedReportLines(t, string(single))
			got := sectionLines(t, all.stdout, targetSectionHeading(tc.name))

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("the %q section is not that target's report one heading level lower (-want +got):\n%s",
					tc.key, diff)
			}
		})
	}
}

// TestAssessAllMatchesEveryTargetOrder pins that --target all and the same
// targets named in another order give the same report: the comparison does not
// depend on the order the targets were selected in.
func TestAssessAllMatchesEveryTargetOrder(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	all := assess(t, allKey, inventory)
	if all.code != 0 {
		t.Fatalf("exit code of --target %s = %d, want 0; stderr = %q", allKey, all.code, all.stderr)
	}

	named := assess(t, upcloudKey+","+ovhcloudKey+","+scalewayKey, inventory)
	if named.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", named.code, named.stderr)
	}

	if diff := cmp.Diff(strings.Split(all.stdout, "\n"), strings.Split(named.stdout, "\n")); diff != "" {
		t.Errorf("--target %s and the same targets named in another order differ (-all +named):\n%s",
			allKey, diff)
	}
}

// targetSectionHeading returns the heading that starts one target's section of a
// comparison: the target's display name inside a code span, one heading level
// below the comparison's own sections.
func targetSectionHeading(displayName string) string {
	return "## `" + displayName + "`"
}

// sectionLines returns the body of the comparison section that starts with
// heading: every line after the heading, up to the next heading of the same level
// or higher, with blank lines dropped, because the golden files pin the
// blank-line layout. The heading itself is left out: it is the title the
// comparison gives the target's report in place of the single-target title.
func sectionLines(t *testing.T, report, heading string) []string {
	t.Helper()

	lines := strings.Split(report, "\n")

	start := slices.Index(lines, heading)
	if start < 0 {
		t.Fatalf("the report has no %q section:\n%s", heading, report)
	}

	end := len(lines)

	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i

			break
		}
	}

	kept := make([]string, 0, end-start)

	for _, line := range lines[start+1 : end] {
		if line != "" {
			kept = append(kept, line)
		}
	}

	if len(kept) == 0 {
		t.Fatalf("the %q section is empty:\n%s", heading, report)
	}

	return kept
}

// demotedReportLines turns a single-target report into the body its comparison
// section must hold: the title and the header lines, which the comparison gives
// once for every target, are dropped, and every heading moves one level down.
// Blank lines are dropped, because the golden files pin the blank-line layout.
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

// TestAssessInventoryVersion2 pins that the built binary accepts an inventory
// format version 2, as it accepts its version 1 twin, and that the version 2
// fields change no verdict (PRD #190, milestones 1 and 8): both runs exit 0, and
// the two reports hold the same fits, summary, required changes and blockers. The
// reports themselves differ, because a version 2 inventory's relations, workloads
// and sizes reach them since milestone 8, so the twins are compared on the
// verdicts alone, never byte for byte. The twins sit in directories of their own,
// so both files have the same base name.
func TestAssessInventoryVersion2(t *testing.T) {
	t.Parallel()

	one := filepath.Join("testdata", "twin-v1", "inventory.json")
	two := filepath.Join("testdata", "twin-v2", "inventory.json")

	for _, tc := range targets() {
		for _, format := range []string{markdownFormat, jsonFormat} {
			t.Run(tc.key+"/"+format, func(t *testing.T) {
				t.Parallel()

				version1 := assess(t, tc.key, one, "--format", format)
				version2 := assess(t, tc.key, two, "--format", format)

				for name, got := range map[string]result{"version 1": version1, "version 2": version2} {
					if got.code != 0 {
						t.Fatalf("the %s inventory: exit code = %d, want 0; stderr = %q", name, got.code, got.stderr)
					}

					if got.stderr != "" {
						t.Errorf("the %s inventory: stderr = %q, want empty", name, got.stderr)
					}

					if got.stdout == "" {
						t.Fatalf("the %s inventory: stdout is empty, want a report", name)
					}
				}

				if format == jsonFormat {
					v1, v2 := decodeVerdicts(t, version1.stdout), decodeVerdicts(t, version2.stdout)

					if diff := cmp.Diff(v1.Summary, v2.Summary); diff != "" {
						t.Errorf("the version 2 inventory's summary differs from its version 1 twin (-v1 +v2):\n%s", diff)
					}

					if diff := cmp.Diff(v1.Resources, v2.Resources); diff != "" {
						t.Errorf("the version 2 fits and required changes differ from the version 1 twin's (-v1 +v2):\n%s",
							diff)
					}

					if diff := cmp.Diff(v1.Blockers, v2.Blockers); diff != "" {
						t.Errorf("the version 2 inventory's blockers differ from its version 1 twin (-v1 +v2):\n%s", diff)
					}

					return
				}

				sections1, sections2 := markdownSections(version1.stdout), markdownSections(version2.stdout)

				// A report without estimates keeps today's four sections; one with
				// them adds the workloads and the cost summary. Only the sections
				// both reports hold are compared, and the three that carry the
				// verdicts must be there.
				for _, heading := range []string{summaryHeading, "## Required changes", "## Blockers"} {
					body1, ok := sections1[heading]
					if !ok {
						t.Fatalf("the version 1 report holds no %q section:\n%s", heading, version1.stdout)
					}

					if diff := cmp.Diff(body1, sections2[heading]); diff != "" {
						t.Errorf("%q differs between the twins (-version 1 +version 2):\n%s", heading, diff)
					}
				}
			})
		}
	}
}

// verdicts are the parts of a JSON report two inventories must agree on: the
// verdict per resource, its required changes and its reasons, and the blockers -
// never the estimate, the relations or the workloads, which a version 2
// inventory adds.
type verdicts struct {
	Summary   json.RawMessage `json:"summary"`
	Resources []struct {
		ID              string          `json:"id"`
		Fit             string          `json:"fit"`
		RequiredChanges []string        `json:"requiredChanges"`
		Reasons         json.RawMessage `json:"reasons"`
	} `json:"resources"`
	Blockers json.RawMessage `json:"blockers"`
}

// decodeVerdicts decodes the verdict part of a JSON report, failing the test on a
// report that holds no resource.
func decodeVerdicts(t *testing.T, report string) verdicts {
	t.Helper()

	var v verdicts
	if err := json.Unmarshal([]byte(report), &v); err != nil {
		t.Fatalf("decoding the JSON report %q: %v", report, err)
	}

	if len(v.Resources) == 0 {
		t.Fatalf("the JSON report holds no resource:\n%s", report)
	}

	return v
}

// markdownSections splits a Markdown report into its "##" sections, keyed by the
// heading line and holding the lines below it up to the next "##" heading. The
// "###" subheadings of a section stay in its body.
func markdownSections(report string) map[string]string {
	sections := map[string]string{}

	heading := ""

	for line := range strings.SplitSeq(report, "\n") {
		if strings.HasPrefix(line, "## ") {
			heading = line

			continue
		}

		if heading != "" {
			sections[heading] += line + "\n"
		}
	}

	return sections
}

// TestAssessInvalidRelationWritesNoReport pins the other half of the version 2
// error contract, on the real binary: an inventory whose relation names no
// resource exits 1 with one error line that names the relation by path, and the
// report --output names is never written.
func TestAssessInvalidRelationWritesNoReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "report.md")

	got := assess(t, scalewayKey, filepath.Join("testdata", "invalid-relation.json"), "--output", dest)

	if got.code != 1 {
		t.Errorf("exit code = %d, want 1; stderr = %q", got.code, got.stderr)
	}

	if got.stdout != "" {
		t.Errorf("stdout = %q, want empty", got.stdout)
	}

	if !strings.HasPrefix(got.stderr, errPrefix) {
		t.Errorf("stderr = %q, want it to start with %q", got.stderr, errPrefix)
	}

	if !strings.HasSuffix(got.stderr, "\n") || strings.Count(got.stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line ending in a newline", got.stderr)
	}

	if want := "resources[1].relations[0].target"; !strings.Contains(got.stderr, want) {
		t.Errorf("stderr = %q, want it to name the relation by path %q", got.stderr, want)
	}

	if _, err := os.Stat(dest); err == nil {
		t.Errorf("the report %q was written for an inventory the reader refused", dest)
	}
}
