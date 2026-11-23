package architecture_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// auditPublishPackage is the audit's publish core, the package whose one
	// gap constant the check reads (PRD #175 milestone 2, ADR 0040).
	auditPublishPackage = "ci/auditpublish/internal/publish"

	// auditWorkflow is the audit's workflow, which carries the schedule
	// trigger from milestone 3 on.
	auditWorkflow = ".github/workflows/scheduled-security-audit.yml"

	// auditPublishProgram is the audit's publish program's package: a workflow
	// that names it runs the program, with go run or any other command.
	auditPublishProgram = "ci/auditpublish"

	// gapThresholdName is the one constant the report's gap line is stated in.
	gapThresholdName = "GapThresholdDays"

	// gapThresholdDays is the value the constant must have while the workflow
	// has no schedule trigger: the longest month, 31 days, plus 7 days of
	// slack, for the monthly cron the ADR records.
	gapThresholdDays = 38
)

// scheduleWord matches the schedule trigger's name as a whole word inside an
// inline on: value, so a flow mapping, a flow sequence and a scalar are read,
// and a name that merely starts with schedule is not.
var scheduleWord = regexp.MustCompile(`(^|[^A-Za-z0-9_-])schedule([^A-Za-z0-9_-]|$)`)

// hasScheduleTrigger reports whether a workflow holds a schedule trigger under
// its top-level on: key, in the block or the flow style GitHub accepts; the
// key and the trigger may be bare, double-quoted or single-quoted, and the
// trigger may carry a trailing comment. It reads no YAML: the check needs one
// key of the document and the module has no YAML package. A line inside a
// comment or a run: block is never a trigger, since only a nested line of a
// top-level on: mapping is read.
func hasScheduleTrigger(workflow []byte) bool {
	onBlock := false

	for _, line := range strings.Split(string(workflow), "\n") {
		line = strings.TrimRight(line, " \t\r")

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indented := len(line) - len(strings.TrimLeft(line, " "))

		if onBlock && indented > 0 {
			if isScheduleKey(trimmed) {
				return true
			}

			continue
		}

		// A line at the top level ends the on: mapping, if one is open.
		onBlock = false

		if indented != 0 {
			continue
		}

		value, ok := topLevelOn(line)
		if !ok {
			continue
		}

		if value == "" {
			onBlock = true

			continue
		}

		if scheduleWord.MatchString(value) {
			return true
		}
	}

	return false
}

// topLevelOn returns the value after a line's top-level on: key, and whether
// the line opens one: the key is on:, "on": or 'on':, a trailing comment
// removed.
func topLevelOn(line string) (string, bool) {
	for _, key := range []string{"on:", `"on":`, `'on':`} {
		if value, ok := strings.CutPrefix(line, key); ok {
			return inlineValue(value), true
		}
	}

	return "", false
}

// isScheduleKey reports whether a trimmed line is a schedule key, bare or
// quoted: schedule: with a value, a comment or nothing after it.
func isScheduleKey(line string) bool {
	for _, key := range []string{"schedule:", `"schedule":`, `'schedule':`} {
		if strings.HasPrefix(line, key) {
			return true
		}
	}

	return false
}

// inlineValue returns an inline YAML value with its trailing comment removed
// and its spaces trimmed: a comment starts at a # that follows a space, so a
// bare on: is the empty value.
func inlineValue(value string) string {
	if i := strings.Index(value, " #"); i >= 0 {
		value = value[:i]
	}

	return strings.TrimSpace(value)
}

// gapDeclaration is one declaration of the constant the check reads.
type gapDeclaration struct {
	file string
	line int
	// kind is the declaration's keyword, const or var.
	kind string
	// literal is the integer literal the declaration assigns, empty when the
	// value is no integer literal.
	literal string
}

// TestGapThresholdIsPinnedOnce reads the audit's publish core and fails unless
// GapThresholdDays is declared exactly once, as an integer literal, and equals
// the threshold the ADR records (PRD #175 milestone 2, ADR 0040). The report's
// gap line and the staleness check of the release-tag and dispatch skills are
// stated in that one number, so a second declaration, a computed value or
// another value would let the two drift apart unseen.
func TestGapThresholdIsPinnedOnce(t *testing.T) {
	t.Parallel()

	declarations, files := gapDeclarations(t)

	if files == 0 {
		t.Fatalf("no Go file of %s was read, so this check would hold nothing; add the package and the constant with it", auditPublishPackage)
	}

	if len(declarations) != 1 {
		t.Fatalf("%s is declared %d times in %s, want exactly once, as an integer literal %d:\n%s",
			gapThresholdName, len(declarations), auditPublishPackage, gapThresholdDays, formatDeclarations(declarations))
	}

	declaration := declarations[0]

	if declaration.kind != "const" {
		t.Errorf("%s:%d: %s is declared with %s, want const: the threshold is one pinned constant",
			declaration.file, declaration.line, gapThresholdName, declaration.kind)
	}

	if declaration.literal != strconv.Itoa(gapThresholdDays) {
		t.Errorf("%s:%d: %s is declared as %q, want the integer literal %d: a computed value or another number would drift from the skills' staleness check and the ADR",
			declaration.file, declaration.line, gapThresholdName, declaration.literal, gapThresholdDays)
	}
}

// TestAuditWorkflowHasNoScheduleTrigger pins the other half of the constant's
// check: while the audit's workflow has no schedule trigger, the threshold is
// the literal above; milestone 3, which adds the trigger, replaces it with the
// cron-derived check the PRD states. It also pins that no other workflow runs
// the audit's publish program. The workflow does not exist yet, which is the
// "no trigger" case: the PRD's fifth pull request adds it.
func TestAuditWorkflowHasNoScheduleTrigger(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	workflowsDir := filepath.Join(root, ".github", "workflows")

	workflow, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(auditWorkflow)))

	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		return
	default:
		t.Fatalf("read %s: %v", auditWorkflow, err)
	}

	if hasScheduleTrigger(workflow) {
		t.Errorf("%s holds a schedule trigger, want none while %s is the literal %d: milestone 3 adds the trigger and changes this check to the cron's longest gap plus 7 days",
			auditWorkflow, gapThresholdName, gapThresholdDays)
	}

	// The audit's report is published by the audit's own workflow alone: one
	// that ran the same program with no schedule behind it would publish a
	// report the gap check never sees (ADR 0040).
	for _, name := range workflowsRunningThePublishProgram(t, workflowsDir) {
		t.Errorf(".github/workflows/%s runs the audit's publish program (it names %s), want %s alone to run it: a report published with no schedule behind it skips the gap check",
			name, auditPublishProgram, auditWorkflow)
	}
}

// workflowsRunningThePublishProgram returns, in walk order, the workflow files
// under dir, other than the audit's own workflow, that name the audit's publish
// program.
func workflowsRunningThePublishProgram(t *testing.T, dir string) []string {
	t.Helper()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}

	t.Cleanup(func() { _ = root.Close() })

	var found []string

	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		name := filepath.ToSlash(relative)
		if name == filepath.Base(auditWorkflow) {
			return nil
		}

		data, err := root.ReadFile(relative)
		if err != nil {
			return err
		}

		if strings.Contains(string(data), auditPublishProgram) {
			found = append(found, name)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	return found
}

// TestWorkflowsRunningThePublishProgram pins the scan that keeps the publish
// program the audit workflow's own: another workflow that names it is found
// wherever it sits, the audit's own is not, and a workflow that does not name
// it is no hit.
func TestWorkflowsRunningThePublishProgram(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"ci.yml":                       "jobs:\n  build:\n    steps:\n      - run: make check\n",
		"other.yml":                    "jobs:\n  publish:\n    steps:\n      - run: go run ./ci/auditpublish\n",
		"scheduled-security-audit.yml": "jobs:\n  publish:\n    steps:\n      - run: go run ./ci/auditpublish\n",
		"nested/deep.yml":              "jobs:\n  publish:\n    steps:\n      - run: go run ./ci/auditpublish\n",
	}

	dir := t.TempDir()

	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))

		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("make %s: %v", filepath.Dir(path), err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	got := workflowsRunningThePublishProgram(t, dir)
	slices.Sort(got)

	want := []string{"nested/deep.yml", "other.yml"}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the workflows that run the publish program mismatch (-want +got):\n%s", diff)
	}
}

// TestHasScheduleTrigger pins the matcher the workflow check reads: every form
// GitHub accepts for a schedule trigger, and none of the texts that merely
// name it. The workflow check's own regexp was a two-space block-style line,
// so a flow-style or a four-space trigger would have passed unseen.
func TestHasScheduleTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		workflow string
		want     bool
	}{
		{
			name:     "a block-style schedule key",
			workflow: "on:\n  schedule:\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			name:     "a schedule key at another indent",
			workflow: "on:\n    schedule:\n        - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			name:     "a schedule key with a trailing comment",
			workflow: "on:\n  schedule: # the monthly cron, milestone 3\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			name:     "a double-quoted schedule key",
			workflow: "on:\n  \"schedule\":\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			name:     "a single-quoted schedule key",
			workflow: "on:\n  'schedule':\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			name:     "a quoted on key",
			workflow: "\"on\":\n  schedule:\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			name:     "a flow mapping",
			workflow: "on: {push: {}, schedule: [{cron: '17 4 3 * *'}]}\n",
			want:     true,
		},
		{
			name:     "a flow sequence",
			workflow: "on: [schedule, workflow_dispatch]\n",
			want:     true,
		},
		{
			name:     "a scalar trigger",
			workflow: "on: schedule\njobs:\n  audit:\n    runs-on: ubuntu-latest\n",
			want:     true,
		},
		{
			name:     "another trigger only",
			workflow: "on: workflow_dispatch\njobs:\n  audit:\n    runs-on: ubuntu-latest\n",
			want:     false,
		},
		{
			name:     "two other triggers only",
			workflow: "on:\n  push:\n    branches: [main]\n  workflow_dispatch:\n",
			want:     false,
		},
		{
			name:     "a schedule in a step's run text",
			workflow: "on:\n  workflow_dispatch:\njobs:\n  audit:\n    steps:\n      - run: |\n          echo schedule\n",
			want:     false,
		},
		{
			name:     "a schedule line in a run block",
			workflow: "on:\n  workflow_dispatch:\njobs:\n  audit:\n    steps:\n      - run: |\n          on:\n            schedule:\n",
			want:     false,
		},
		{
			name:     "a schedule line in a comment",
			workflow: "on:\n  # schedule:\n  workflow_dispatch:\n",
			want:     false,
		},
		{
			name:     "a schedule mention in a trailing comment",
			workflow: "name: audit # on: [schedule]\non:\n  workflow_dispatch:\n",
			want:     false,
		},
		{
			name:     "a job named schedule-x",
			workflow: "on:\n  workflow_dispatch:\njobs:\n  schedule-x:\n    runs-on: ubuntu-latest\n",
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := hasScheduleTrigger([]byte(tc.workflow)); got != tc.want {
				t.Errorf("hasScheduleTrigger(%q) = %v, want %v", tc.workflow, got, tc.want)
			}
		})
	}
}

// gapDeclarations returns every declaration of GapThresholdDays in the publish
// core's Go files, and how many files were read: a declaration with no value,
// or a value that is no integer literal, is one the check reports with an empty
// literal.
func gapDeclarations(t *testing.T) ([]gapDeclaration, int) {
	t.Helper()

	dir := filepath.Join(repositoryRoot(t), filepath.FromSlash(auditPublishPackage))

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", auditPublishPackage, err)
	}

	var (
		declarations []gapDeclaration
		read         int
	)

	fset := token.NewFileSet()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}

		name := filepath.Join(dir, entry.Name())

		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		read++

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST && gen.Tok != token.VAR {
				continue
			}

			for _, spec := range gen.Specs {
				values, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}

				for i, ident := range values.Names {
					if ident.Name != gapThresholdName {
						continue
					}

					literal := ""

					if i < len(values.Values) {
						if lit, ok := values.Values[i].(*ast.BasicLit); ok && lit.Kind == token.INT {
							literal = lit.Value
						} else {
							literal = "an expression"
						}
					}

					declarations = append(declarations, gapDeclaration{
						file:    auditPublishPackage + "/" + entry.Name(),
						line:    fset.Position(ident.Pos()).Line,
						kind:    gen.Tok.String(),
						literal: literal,
					})
				}
			}
		}
	}

	return declarations, read
}

// formatDeclarations returns one line per declaration, for a failure message.
func formatDeclarations(declarations []gapDeclaration) string {
	if len(declarations) == 0 {
		return "  (none)"
	}

	lines := make([]string, 0, len(declarations))

	for _, d := range declarations {
		lines = append(lines, "  "+d.file+":"+strconv.Itoa(d.line)+": "+d.kind+" "+gapThresholdName+" "+d.literal)
	}

	return strings.Join(lines, "\n")
}
