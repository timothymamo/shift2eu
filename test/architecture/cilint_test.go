package architecture_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
)

// ciMatrixEntry matches one entry of the lint-target matrix as the workflow
// writes it: a flow mapping on one line, with a quoted cgo value.
var ciMatrixEntry = regexp.MustCompile(`^- \{ goos: ([a-z0-9]+), goarch: ([a-z0-9]+), cgo: "([^"]*)" \}$`)

// ciLine is one content line of a workflow: neither blank nor only a comment,
// with the number of spaces before it.
type ciLine struct {
	// num is the line number, counted from 1.
	num int
	// indent is the number of spaces before text.
	indent int
	// text is the line, with white space trimmed from both ends.
	text string
}

// matrixEntry is one platform the lint-target matrix lints.
type matrixEntry struct {
	// num is the line number of the entry.
	num    int
	goos   string
	goarch string
	cgo    string
}

// ciKey is one key of a workflow mapping: its name, without the colon and the
// value, and the line it sits on.
type ciKey struct {
	num  int
	name string
}

// ciItem is one item of a workflow list: the line it starts on and the lines
// that belong to it, its first line read as a key of the item rather than as
// the marker of a list.
type ciItem struct {
	// num is the line number of the item's first line.
	num   int
	lines []ciLine
}

// TestCILintMatrixCoversEveryReleaseTarget pins the CI lint matrix to the
// release: .github/workflows/ci.yml lints every goos/goarch pair
// .goreleaser.yaml builds with cgo off, plus one native entry with cgo on, and
// keeps the one Lint job that is a required status check on main over the
// matrix. A target missing from the matrix ships with its files unlinted, and a
// Lint job renamed away leaves the check main requires unreported (issue #39).
func TestCILintMatrixCoversEveryReleaseTarget(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %q: %v", repoRoot, err)
	}

	// .goreleaser.yaml is the one source of the release platforms: the matrix
	// is checked against it rather than against a copy pinned here.
	goreleaser, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}

	targets, err := releasetarget.Read(goreleaser)
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so this test would pin nothing")
	}

	// wantRelease is the goos/goarch of every matrix entry with cgo "0", sorted:
	// the release platforms plus js/wasm, the site's target, which no release
	// build lists. An entry without it leaves the site's js && wasm files
	// unlinted in CI, so it is required here as an explicit extra target (PRD
	// #297).
	wantRelease := wantLintTargets(targets)

	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read .github/workflows/ci.yml: %v", err)
	}

	jobs, err := workflowJobs(string(ci))
	if err != nil {
		t.Fatalf("read .github/workflows/ci.yml: %v", err)
	}

	// The top level is a closed schema too: a workflow-level env: (GOFLAGS,
	// GOEXPERIMENT, ...) or defaults: reaches every step of every job, the
	// golangci-lint step's included, and decides what it lints.
	topLines, err := ciLines(string(ci))
	if err != nil {
		t.Fatalf("read .github/workflows/ci.yml: %v", err)
	}

	for _, l := range topLines {
		if l.indent != 0 {
			continue
		}

		for _, key := range []string{"env", "defaults"} {
			if l.text == key+":" || strings.HasPrefix(l.text, key+": ") {
				t.Errorf("line %d: .github/workflows/ci.yml holds a top-level %s:, which reaches the golangci-lint step and decides what it lints: set the step's own env: instead",
					l.num, key)
			}
		}
	}

	wantKeys(t, "top level of .github/workflows/ci.yml", topLines, "name", "on", "permissions", "concurrency", "jobs")

	job, ok := jobs["lint-target"]
	if !ok {
		t.Fatalf(".github/workflows/ci.yml has no lint-target job under jobs:, so no CI run lints a release target; jobs found: %s",
			strings.Join(jobIDs(jobs), ", "))
	}

	// The job is read as a closed schema: a key beyond the ones named here holds
	// a condition, an environment, a runner change or an excuse for a failing
	// entry that would decide what CI lints without this test seeing it.
	wantKeys(t, "lint-target job", job, "name", "runs-on", "strategy", "steps")

	// The cgo "1" matrix entry lints the runner's own platform, so the runner
	// must be the one that entry names.
	if num, got, ok := ciScalar(job, "runs-on"); !ok {
		t.Errorf("the lint-target job holds no runs-on:, so nothing pins the runner its cgo \"1\" entry assumes")
	} else if got != "ubuntu-latest" {
		t.Errorf("line %d: the lint-target job runs on %q, want %q: the cgo \"1\" entry is that runner's platform",
			num, got, "ubuntu-latest")
	}

	strategy, ok := ciBlock(job, "strategy")
	if !ok {
		t.Fatalf("the lint-target job holds no strategy: mapping, so it has no matrix of targets")
	}

	wantKeys(t, "lint-target job's strategy", strategy, "fail-fast", "matrix")

	// One failing target must not cancel the runs of the others: their results
	// are what tells a red Lint which platforms are clean.
	if num, got, ok := ciScalar(strategy, "fail-fast"); !ok {
		t.Errorf("the lint-target job's strategy: holds no fail-fast:, so one failing target may cancel the runs of the others")
	} else if got != "false" {
		t.Errorf("line %d: strategy: fail-fast is %q, want %q, so one failing target does not cancel the runs of the others",
			num, got, "false")
	}

	matrix, ok := ciBlock(strategy, "matrix")
	if !ok {
		t.Fatalf("the lint-target job's strategy: holds no matrix: mapping, so it has no list of targets")
	}

	// A base axis beside include: folds the two together, and a linted target
	// that the axis does not place drops out of the matrix.
	wantKeys(t, "lint-target job's strategy: matrix", matrix, "include")

	include, ok := ciBlock(matrix, "include")
	if !ok {
		t.Fatalf("the lint-target job's strategy: matrix: holds no include: list, so it lints no target")
	}

	if len(include) == 0 {
		t.Fatalf("the lint-target job's strategy: matrix: include: list is empty, so it lints no target")
	}

	entries := parseMatrixEntries(t, include)

	// An entry with cgo "0" is a release target; the one with cgo "1" is the
	// runner's native run, which make test's -race build compiles.
	var release []string

	var native []matrixEntry

	for _, entry := range entries {
		switch entry.cgo {
		case "0":
			release = append(release, entry.goos+"/"+entry.goarch)
		case "1":
			native = append(native, entry)
		default:
			t.Errorf("line %d: the %s/%s entry has cgo %q, want \"0\" for a release target or \"1\" for the native run",
				entry.num, entry.goos, entry.goarch, entry.cgo)
		}
	}

	slices.Sort(release)

	if diff := cmp.Diff(wantRelease, release); diff != "" {
		t.Errorf("the entries with cgo \"0\" (-want +got):\n%s\nThe release platforms come from .goreleaser.yaml; every one needs a matrix entry or it ships unlinted",
			diff)
	}

	if len(native) != 1 {
		t.Errorf("%d entries with cgo \"1\", want exactly one native entry for linux/amd64", len(native))
	} else if entry := native[0]; entry.goos != "linux" || entry.goarch != "amd64" {
		t.Errorf("line %d: the entry with cgo \"1\" is %s/%s, want linux/amd64, the runner's platform",
			entry.num, entry.goos, entry.goarch)
	}

	// The golangci-lint step passes the entry's platform to the linter, and
	// skips the action's cache, which keys on the runner OS alone and would
	// let one target's cache entry hide another's issues.
	wantJobLines := []string{
		"GOOS: ${{ matrix.goos }}",
		"GOARCH: ${{ matrix.goarch }}",
		"CGO_ENABLED: ${{ matrix.cgo }}",
		"skip-cache: true",
	}

	for _, want := range wantJobLines {
		if !slices.ContainsFunc(job, func(l ciLine) bool { return l.text == want }) {
			t.Errorf("the lint-target job holds no line %q, so its golangci-lint step would not lint the matrix entry's platform",
				want)
		}
	}

	// The steps are read as a closed schema too: the platform a run lints must
	// come from the one golangci-lint step's own env: block, and no other step
	// of the job may set it, skip a target or excuse a failure.
	steps, ok := ciBlock(job, "steps")
	if !ok {
		t.Fatalf("the lint-target job holds no steps: list, so it runs no golangci-lint")
	}

	items, err := ciItems(steps)
	if err != nil {
		t.Fatalf("the lint-target job's steps: %v", err)
	}

	lintStep := -1

	for i, item := range items {
		num, uses, ok := ciScalar(item.lines, "uses")
		if !ok || !strings.HasPrefix(uses, "golangci/golangci-lint-action@") {
			continue
		}

		if lintStep >= 0 {
			t.Errorf("line %d: a second step runs %q, want exactly one step running the linter", num, uses)

			continue
		}

		lintStep = i
	}

	if lintStep < 0 {
		t.Fatalf("no step of the lint-target job uses golangci/golangci-lint-action@…, so CI lints nothing")
	}

	for _, item := range items {
		keys, err := ciKeys(item.lines)
		if err != nil {
			t.Errorf("the lint-target job's steps: %v", err)

			continue
		}

		// Neither a skipped step nor an excused one may hide a target's
		// issues from the Lint check.
		for _, key := range keys {
			if key.name == "continue-on-error" || key.name == "if" {
				t.Errorf("line %d: a step of the lint-target job holds %q, want no step skipped or excused: one failing target must fail Lint",
					key.num, key.name)
			}
		}
	}

	for i, item := range items {
		if i == lintStep {
			continue
		}

		env, ok := ciBlock(item.lines, "env")
		if !ok {
			continue
		}

		keys, err := ciKeys(env)
		if err != nil {
			t.Errorf("line %d: the env: block of a step of the lint-target job: %v", item.num, err)

			continue
		}

		for _, key := range keys {
			if slices.Contains(lintStepSettings, key.name) {
				t.Errorf("line %d: the env: block of the step on line %d sets %s, want the platform set in the golangci-lint step's own env: block only",
					key.num, item.num, key.name)
			}
		}
	}

	// A run with args: of its own or an install-mode: would take itself out of
	// the platform the entry names, or out of the version CI pins.
	with, ok := ciBlock(items[lintStep].lines, "with")
	if !ok {
		t.Fatalf("line %d: the golangci-lint step holds no with: block, so its version floats",
			items[lintStep].num)
	}

	wantKeys(t, "golangci-lint step's with: block", with, "version", "skip-cache")

	if num, got, ok := ciScalar(with, "version"); !ok || got == "" {
		t.Errorf("line %d: the golangci-lint step's with: block holds no version, so the linter version floats", num)
	}

	if num, got, ok := ciScalar(with, "skip-cache"); !ok {
		t.Errorf("the golangci-lint step's with: block holds no skip-cache:, want true: the action's cache key names only the runner OS, so an entry of one target can hide another target's issues")
	} else if got != "true" {
		t.Errorf("line %d: the golangci-lint step's skip-cache is %q, want %q: the action's cache key names only the runner OS, so an entry of one target can hide another target's issues",
			num, got, "true")
	}

	env, ok := ciBlock(items[lintStep].lines, "env")
	if !ok {
		t.Fatalf("line %d: the golangci-lint step holds no env: block, so it would lint the runner's platform for every entry",
			items[lintStep].num)
	}

	// Beside the platform, the step pins the build settings make lint
	// exports, so CI and a local make lint type-check the same files.
	wantExactLines(t, fmt.Sprintf("env: block of the golangci-lint step on line %d", items[lintStep].num), env, []string{
		"GOOS: ${{ matrix.goos }}",
		"GOARCH: ${{ matrix.goarch }}",
		"CGO_ENABLED: ${{ matrix.cgo }}",
		"GOFLAGS: -mod=readonly",
		"GOAMD64: v1",
		"GOARM64: v8.0",
		`GOWORK: "off"`, // quoted: a YAML 1.1 reader takes a bare off for false
		`GOEXPERIMENT: ""`,
	})

	// The setup-go step skips its cache: its key names only the runner OS and
	// go.sum, so a matrix entry would save a cache shared with the Test job
	// that lacks the -race build output Test restores it for.
	setupGo := slices.IndexFunc(items, func(item ciItem) bool {
		_, uses, ok := ciScalar(item.lines, "uses")

		return ok && strings.HasPrefix(uses, "actions/setup-go@")
	})
	if setupGo < 0 {
		t.Fatalf("no step of the lint-target job uses actions/setup-go@…, so the go version the linter type-checks with floats")
	}

	setupWith, ok := ciBlock(items[setupGo].lines, "with")
	if !ok {
		t.Fatalf("line %d: the setup-go step holds no with: block, want go-version-file: go.mod and cache: false", items[setupGo].num)
	}

	wantExactLines(t, fmt.Sprintf("with: block of the setup-go step on line %d", items[setupGo].num), setupWith, []string{
		"go-version-file: go.mod",
		"cache: false",
	})

	// test/lint runs the forbidigo probes with the golangci-lint the step above
	// installs, and SHIFT2EU_LINT_PROBES=require makes a missing golangci-lint
	// fail them rather than skip them.
	probes := slices.IndexFunc(items, func(item ciItem) bool {
		_, run, ok := ciScalar(item.lines, "run")

		return ok && run == "go test -count=1 ./test/lint/"
	})
	if probes < 0 {
		t.Fatalf("no step of the lint-target job runs go test -count=1 ./test/lint/, so CI never runs the lint probes")
	}

	if probesEnv, ok := ciBlock(items[probes].lines, "env"); !ok {
		t.Errorf("line %d: the test/lint step holds no env: block, want SHIFT2EU_LINT_PROBES: require so the probes fail rather than skip",
			items[probes].num)
	} else if _, got, ok := ciScalar(probesEnv, "SHIFT2EU_LINT_PROBES"); !ok || got != "require" {
		t.Errorf("line %d: the test/lint step sets SHIFT2EU_LINT_PROBES to %q, want require so the probes fail rather than skip",
			items[probes].num, got)
	}

	// Lint is a required status check on main, and a matrix reports one check
	// per entry: the lint job reports the matrix under that one name.
	aggregate, ok := jobs["lint"]
	if !ok {
		t.Fatalf(".github/workflows/ci.yml has no lint job under jobs:, so the Lint status check main requires is never reported")
	}

	wantAggregateLines := []string{
		"name: Lint",
		"needs: [lint-target, lint-shell, coauthor-trailers]",
		"if: ${{ !cancelled() }}",
	}

	for _, want := range wantAggregateLines {
		if !slices.ContainsFunc(aggregate, func(l ciLine) bool { return l.text == want }) {
			t.Errorf("the lint job holds no line %q, want the matrix reported as the one Lint check main requires",
				want)
		}
	}

	// The aggregate job is read as a closed schema: a key of its own could
	// change what the required Lint check reports, or excuse a failing run.
	wantKeys(t, "lint job", aggregate, "name", "needs", "if", "runs-on", "steps")

	for _, want := range []struct{ key, value string }{
		{"name", "Lint"},
		// lint-shell runs shellcheck and actionlint once (issue #164): the
		// Lint check gates it as it gates the matrix, and coauthor-trailers
		// (issue #663) too, so the required check needs no new name.
		{"needs", "[lint-target, lint-shell, coauthor-trailers]"},
		// Not always(): a run cancelled by a newer push must not report a red
		// Lint, and a failed or skipped entry still runs this job, which fails.
		{"if", "${{ !cancelled() }}"},
	} {
		if num, got, ok := ciScalar(aggregate, want.key); !ok {
			t.Errorf("the lint job holds no %s: key, want %q", want.key, want.value)
		} else if got != want.value {
			t.Errorf("line %d: the lint job's %s is %q, want %q", num, want.key, got, want.value)
		}
	}

	aggregateSteps, ok := ciBlock(aggregate, "steps")
	if !ok {
		t.Fatalf("the lint job holds no steps: list, so it reports a result it never checks")
	}

	aggregateItems, err := ciItems(aggregateSteps)
	if err != nil {
		t.Fatalf("the lint job's steps: %v", err)
	}

	if len(aggregateItems) != 1 {
		t.Fatalf("the lint job holds %d steps, want exactly one step reading the matrix result", len(aggregateItems))
	}

	step := aggregateItems[0]

	for _, key := range []string{"continue-on-error", "if"} {
		if _, _, ok := ciScalar(step.lines, key); ok {
			t.Errorf("line %d: the lint job's step holds %q, want no step skipped or excused: the Lint check must fail whenever a target did",
				step.num, key)
		}
	}

	stepEnv, ok := ciBlock(step.lines, "env")
	if !ok {
		t.Fatalf("line %d: the lint job's step holds no env: block, so it reads no matrix result", step.num)
	}

	wantExactLines(t, fmt.Sprintf("env: block of the lint job's step on line %d", step.num), stepEnv, []string{
		"RESULT: ${{ needs.lint-target.result }}",
		"SHELL_RESULT: ${{ needs.lint-shell.result }}",
		"COAUTHOR_RESULT: ${{ needs.coauthor-trailers.result }}",
	})

	run, ok := ciBlock(step.lines, "run")
	if !ok {
		t.Fatalf("line %d: the lint job's step holds no run: script, so it fails on nothing", step.num)
	}

	// The script is pinned whole, in order: a check by substring lets
	// `... && false; then` or `exit 1 || true` through.
	if num, got, _ := ciScalar(step.lines, "run"); got != "|" {
		t.Errorf("line %d: the lint job's step opens its run: with %q, want a plain block scalar %q", num, got, "|")
	}

	script := make([]string, 0, len(run))
	for _, l := range run {
		script = append(script, l.text)
	}

	wantScript := []string{
		`if [ "$RESULT" != "success" ]; then`,
		`echo "lint-target finished with result '$RESULT', want 'success'" >&2`,
		`exit 1`,
		`fi`,
		`if [ "$SHELL_RESULT" != "success" ]; then`,
		`echo "lint-shell finished with result '$SHELL_RESULT', want 'success'" >&2`,
		`exit 1`,
		`fi`,
		`if [ "$COAUTHOR_RESULT" != "success" ]; then`,
		`echo "coauthor-trailers finished with result '$COAUTHOR_RESULT', want 'success'" >&2`,
		`exit 1`,
		`fi`,
	}

	if diff := cmp.Diff(wantScript, script); diff != "" {
		t.Errorf("the lint job's run: script on line %d (-want +got):\n%s\nWant a non-success result of the matrix or of lint-shell to fail the Lint check, and nothing else",
			step.num, diff)
	}

	checkLintShellJob(t, jobs)
	checkCoAuthorTrailersJob(t, jobs)
}

// checkCoAuthorTrailersJob pins the coauthor-trailers job the Lint check needs
// (issue #663), read as a closed schema: it is never skipped or excused, it
// checks out the whole history with no credentials, and its one test step runs
// TestBranchCoAuthorTrailers alone with an env: of exactly the pull request's
// base..head range and SHIFT2EU_COAUTHOR_CHECK=require on a pull request, so a
// range the run cannot read fails the check rather than skipping it.
func checkCoAuthorTrailersJob(t *testing.T, jobs map[string][]ciLine) {
	t.Helper()

	job, ok := jobs["coauthor-trailers"]
	if !ok {
		t.Fatalf(".github/workflows/ci.yml has no coauthor-trailers job under jobs:, so CI never checks a pull request's co-author trailers")
	}

	wantKeys(t, "coauthor-trailers job", job, "name", "runs-on", "steps")

	steps, ok := ciBlock(job, "steps")
	if !ok {
		t.Fatalf("the coauthor-trailers job holds no steps: list, so it checks nothing")
	}

	items, err := ciItems(steps)
	if err != nil {
		t.Fatalf("the coauthor-trailers job's steps: %v", err)
	}

	var checkout, test bool

	for _, item := range items {
		keys, err := ciKeys(item.lines)
		if err != nil {
			t.Errorf("the coauthor-trailers job's steps: %v", err)

			continue
		}

		for _, key := range keys {
			if key.name == "continue-on-error" || key.name == "if" {
				t.Errorf("line %d: a step of the coauthor-trailers job holds %q, want no step skipped or excused", key.num, key.name)
			}
		}

		if _, uses, ok := ciScalar(item.lines, "uses"); ok && strings.HasPrefix(uses, "actions/checkout@") {
			checkout = true

			with, ok := ciBlock(item.lines, "with")
			if !ok {
				t.Errorf("line %d: the coauthor-trailers checkout holds no with:, want the whole history and no credentials", item.num)

				continue
			}

			wantExactLines(t, fmt.Sprintf("with: block of the coauthor-trailers checkout on line %d", item.num), with, []string{
				"persist-credentials: false",
				"fetch-depth: 0",
			})
		}

		if _, run, ok := ciScalar(item.lines, "run"); ok && strings.Contains(run, "TestBranchCoAuthorTrailers") {
			test = true

			if want := `go test -count=1 -v -run '^TestBranchCoAuthorTrailers$' ./test/lint/`; run != want {
				t.Errorf("line %d: the coauthor-trailers step runs %q, want %q", item.num, run, want)
			}

			env, ok := ciBlock(item.lines, "env")
			if !ok {
				t.Errorf("line %d: the coauthor-trailers step holds no env:, so it names no range and requires none", item.num)

				continue
			}

			wantExactLines(t, fmt.Sprintf("env: block of the coauthor-trailers step on line %d", item.num), env, []string{
				"SHIFT2EU_COAUTHOR_RANGE: ${{ github.event_name == 'pull_request' && " +
					"format('{0}..{1}', github.event.pull_request.base.sha, github.event.pull_request.head.sha) || '' }}",
				"SHIFT2EU_COAUTHOR_CHECK: ${{ github.event_name == 'pull_request' && 'require' || '' }}",
			})
		}
	}

	if !checkout {
		t.Errorf("no step of the coauthor-trailers job uses actions/checkout@…, so it reads no commit")
	}

	if !test {
		t.Errorf("no step of the coauthor-trailers job runs TestBranchCoAuthorTrailers, so CI checks no co-author trailer")
	}
}

// checkLintShellJob pins the lint-shell job the Lint check needs (issue #164):
// it runs dev/shellcheck.sh and dev/actionlint.sh, make lint's shellcheck and
// actionlint steps, and neither the job nor a step of it may be skipped or
// excused. dev/actionlint.sh hands actionlint the shellcheck on PATH and fails
// without one, so CI cannot pass with actionlint's shellcheck check switched
// off; TestMakeLintRunsEveryReleaseTarget pins its arguments.
func checkLintShellJob(t *testing.T, jobs map[string][]ciLine) {
	t.Helper()

	job, ok := jobs["lint-shell"]
	if !ok {
		t.Fatalf(".github/workflows/ci.yml has no lint-shell job under jobs:, so CI runs neither shellcheck nor actionlint")
	}

	for _, key := range []string{"if", "continue-on-error"} {
		if num, _, ok := ciScalar(job, key); ok {
			t.Errorf("line %d: the lint-shell job holds %q, want it never skipped or excused: a finding must fail Lint", num, key)
		}
	}

	steps, ok := ciBlock(job, "steps")
	if !ok {
		t.Fatalf("the lint-shell job holds no steps: list, so it lints nothing")
	}

	items, err := ciItems(steps)
	if err != nil {
		t.Fatalf("the lint-shell job's steps: %v", err)
	}

	var shellcheck, actionlint bool

	for _, item := range items {
		keys, err := ciKeys(item.lines)
		if err != nil {
			t.Errorf("the lint-shell job's steps: %v", err)

			continue
		}

		for _, key := range keys {
			if key.name == "continue-on-error" || key.name == "if" {
				t.Errorf("line %d: a step of the lint-shell job holds %q, want no step skipped or excused", key.num, key.name)
			}
		}

		if _, run, ok := ciScalar(item.lines, "run"); ok && run == "dev/actionlint.sh" {
			actionlint = true
		}

		if block, ok := ciBlock(item.lines, "run"); ok {
			for _, l := range block {
				if l.text == "dev/shellcheck.sh" {
					shellcheck = true
				}
			}
		}
	}

	if !shellcheck {
		t.Errorf("no step of the lint-shell job runs dev/shellcheck.sh on a line of its own, so CI shellchecks no script")
	}

	if !actionlint {
		t.Errorf("no step of the lint-shell job runs dev/actionlint.sh, so CI checks no workflow, or checks it without shellcheck")
	}
}

// lintStepSettings are the variables only the golangci-lint step's own env:
// may set: the platform of the matrix entry and the build settings make lint
// exports.
var lintStepSettings = []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOAMD64", "GOARM64", "GOWORK", "GOEXPERIMENT"}

// parseMatrixEntries reads the include: list of the lint-target matrix. Every
// line must be one flow entry; anything else is reported and skipped.
func parseMatrixEntries(t *testing.T, lines []ciLine) []matrixEntry {
	t.Helper()

	entries := make([]matrixEntry, 0, len(lines))

	for _, l := range lines {
		match := ciMatrixEntry.FindStringSubmatch(l.text)
		if match == nil {
			t.Errorf("line %d: %q is not a one-line entry of the form `- { goos: X, goarch: Y, cgo: \"Z\" }`",
				l.num, l.text)

			continue
		}

		entries = append(entries, matrixEntry{num: l.num, goos: match[1], goarch: match[2], cgo: match[3]})
	}

	return entries
}

// workflowJobs returns the jobs of a workflow document: each id under the
// top-level jobs: mapping, with the content lines of its block. A job is the
// line at two spaces whose text ends in a colon; its block is every line below
// it indented deeper, up to the next job or the next top-level key. It fails
// closed: a document with no jobs: mapping, a line under jobs: that is not a
// job id, and a line between the two indents are errors.
func workflowJobs(doc string) (map[string][]ciLine, error) {
	lines, err := ciLines(doc)
	if err != nil {
		return nil, err
	}

	start := slices.IndexFunc(lines, func(l ciLine) bool { return l.indent == 0 && l.text == "jobs:" })
	if start < 0 {
		return nil, errors.New("no jobs: mapping at the top level")
	}

	jobs := make(map[string][]ciLine)

	const jobIndent = 2

	current := ""

	for _, l := range lines[start+1:] {
		switch {
		case l.indent < jobIndent:
			if l.indent != 0 {
				return nil, fmt.Errorf("line %d: indented %d spaces, want %d for a job or 0 for a top-level key",
					l.num, l.indent, jobIndent)
			}

			return jobs, nil
		case l.indent == jobIndent:
			id, ok := strings.CutSuffix(l.text, ":")
			if !ok || id == "" {
				return nil, fmt.Errorf("line %d: %q is not a job id ending in a colon", l.num, l.text)
			}

			current = id
			jobs[id] = nil
		default:
			if current == "" {
				return nil, fmt.Errorf("line %d: the lines under jobs: start inside no job", l.num)
			}

			jobs[current] = append(jobs[current], l)
		}
	}

	return jobs, nil
}

// ciBlock returns the lines below the first line of parent that is key: — the
// lines after it indented deeper, up to the next line at its indentation or
// less — and whether parent holds the key at all. A `key: value` line, a block
// scalar such as `run: |` included, holds the lines below it just the same.
func ciBlock(parent []ciLine, key string) ([]ciLine, bool) {
	for i, l := range parent {
		if l.text != key+":" && !strings.HasPrefix(l.text, key+": ") {
			continue
		}

		var block []ciLine

		for _, below := range parent[i+1:] {
			if below.indent <= l.indent {
				break
			}

			block = append(block, below)
		}

		return block, true
	}

	return nil, false
}

// ciLines returns the content lines of a workflow document: neither blank nor
// only a comment, with their indentation. A tab in the indentation is an error.
func ciLines(doc string) ([]ciLine, error) {
	var out []ciLine

	for i, raw := range strings.Split(doc, "\n") {
		num := i + 1
		raw = strings.TrimSuffix(raw, "\r")
		text := strings.TrimLeft(raw, " ")
		indent := len(raw) - len(text)

		if strings.HasPrefix(text, "\t") {
			return nil, fmt.Errorf("line %d: a tab in the indentation", num)
		}

		text = strings.TrimSpace(text)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		out = append(out, ciLine{num: num, indent: indent, text: text})
	}

	return out, nil
}

// jobIDs returns the ids of jobs, sorted, for an assertion message.
func jobIDs(jobs map[string][]ciLine) []string {
	ids := make([]string, 0, len(jobs))

	for id := range jobs {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	return ids
}

// ciKeyIndent returns the indentation of the keys of a block: the shallowest
// indentation in it, since every line deeper belongs to a key's own value.
func ciKeyIndent(block []ciLine) int {
	indent := block[0].indent

	for _, l := range block {
		if l.indent < indent {
			indent = l.indent
		}
	}

	return indent
}

// ciKeys returns the keys of a block, in order, each with its line number. It
// fails closed: a line at the keys' indentation that carries no key is an error.
func ciKeys(block []ciLine) ([]ciKey, error) {
	if len(block) == 0 {
		return nil, nil
	}

	indent := ciKeyIndent(block)

	keys := make([]ciKey, 0, len(block))

	for _, l := range block {
		if l.indent != indent {
			continue
		}

		name, _, ok := strings.Cut(l.text, ":")
		if !ok || name == "" {
			return nil, fmt.Errorf("line %d: %q is not a key, want `name:` or `name: value`", l.num, l.text)
		}

		keys = append(keys, ciKey{num: l.num, name: name})
	}

	return keys, nil
}

// ciScalar returns the value of the first `name: value` line of block, and its
// line number, and whether block holds the key at all.
func ciScalar(block []ciLine, name string) (int, string, bool) {
	if len(block) == 0 {
		return 0, "", false
	}

	indent := ciKeyIndent(block)

	for _, l := range block {
		if l.indent != indent {
			continue
		}

		if value, ok := strings.CutPrefix(l.text, name+": "); ok {
			return l.num, value, true
		}
	}

	return 0, "", false
}

// wantKeys asserts that block holds exactly want as its keys, in any order,
// naming every key that is not wanted and every wanted key that is missing.
func wantKeys(t *testing.T, what string, block []ciLine, want ...string) {
	t.Helper()

	keys, err := ciKeys(block)
	if err != nil {
		t.Errorf("the %s: %v", what, err)

		return
	}

	have := make([]string, 0, len(keys))

	for _, key := range keys {
		if !slices.Contains(want, key.name) {
			t.Errorf("line %d: the %s holds the key %q, want only %s",
				key.num, what, key.name, strings.Join(want, ", "))
		}

		have = append(have, key.name)
	}

	for _, name := range want {
		if !slices.Contains(have, name) {
			t.Errorf("the %s holds no %q key, want exactly %s", what, name, strings.Join(want, ", "))
		}
	}
}

// wantExactLines asserts that block is exactly want, as many lines, in any
// order, and reports the lines that differ.
func wantExactLines(t *testing.T, what string, block []ciLine, want []string) {
	t.Helper()

	got := make([]string, 0, len(block))
	for _, l := range block {
		got = append(got, l.text)
	}

	sortedWant := slices.Clone(want)

	slices.Sort(sortedWant)
	slices.Sort(got)

	if diff := cmp.Diff(sortedWant, got); diff != "" {
		t.Errorf("the %s (-want +got):\n%s", what, diff)
	}
}

// ciItems returns the items of a block that is a YAML list, each with the lines
// that belong to it and its first line read as a key at the item's own
// indentation. It fails closed: a line at the list's indentation that is not an
// item is an error, as is content before the first item.
func ciItems(block []ciLine) ([]ciItem, error) {
	var items []ciItem

	indent := -1

	for _, l := range block {
		rest, ok := strings.CutPrefix(l.text, "- ")
		if !ok {
			if len(items) == 0 {
				return nil, fmt.Errorf("line %d: %q comes before the list's first item", l.num, l.text)
			}

			if l.indent <= indent {
				return nil, fmt.Errorf("line %d: %q sits at the list's own indentation but is not an item", l.num, l.text)
			}

			items[len(items)-1].lines = append(items[len(items)-1].lines, l)

			continue
		}

		if indent < 0 {
			indent = l.indent
		} else if l.indent != indent {
			return nil, fmt.Errorf("line %d: the item is indented %d spaces, want %d as the items above it",
				l.num, l.indent, indent)
		}

		items = append(items, ciItem{
			num:   l.num,
			lines: []ciLine{{num: l.num, indent: l.indent + 2, text: rest}},
		})
	}

	return items, nil
}
