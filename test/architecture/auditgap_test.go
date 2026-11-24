package architecture_test

import (
	"errors"
	"fmt"
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

// scheduleWord matches the schedule trigger's name as a whole word inside a
// line of the on: value, so a block key, a flow mapping, a flow sequence and a
// scalar are read, and a name that merely starts with schedule is not.
var scheduleWord = regexp.MustCompile(`(^|[^A-Za-z0-9_-])schedule([^A-Za-z0-9_-]|$)`)

// hasScheduleTrigger reports whether a workflow may hold a schedule trigger
// under its top-level on: key. It reads no YAML (the check needs one key of
// the document and the module has no YAML package) and fails closed by
// construction: it never decides what form the on: value takes, it matches
// the schedule word against every line of that value, so no form of the key
// or the value GitHub accepts can hide a schedule.
//
// A leading byte order mark is dropped. The root key column is the indent of
// the document's first content line (no blank, no comment), read afresh after
// each document marker (--- or ... at column 0), so a root mapping indented as
// a whole is read like one at column 0. The top-level on key is a line at the
// root column that reads on, "on" or 'on', after an optional explicit-key
// `? ` and before optional blanks and a colon (the colon may be missing only
// after `? `). Its value is every line from the key's line up to the next line
// at or left of the root column, the next top-level key, or the end of the
// document; a line there that starts with `:` (an explicit key's value), `}`
// or `]`, and a comment, ends nothing. On the key's line the value after the
// colon is matched, its comment removed; below it every line that is no
// comment is matched, whatever the value starts with: a block mapping, an
// anchor or a tag, a scalar, or a flow mapping or sequence on the key's line
// or the next. Lines under jobs: are never read, since jobs: is the next
// top-level key, so a run: script naming schedule is no trigger, and neither
// is a comment that names it.
//
// A flow mapping or sequence (the value, after its anchor or tag, starts with
// `{` or `[`) is read stricter still: the key's whole line is matched, its
// comment included, and so is every line of the value, comments included.
// Its bracket depth is counted only to tell a malformed value: a quote opens
// a quoted scalar only where a scalar starts (the value's start, or after a
// `{`, a `[`, a `,` or a `:` and blanks), so an apostrophe inside a plain
// scalar such as what's is text, and a # after a blank outside a quoted scalar
// starts a comment. A flow value never closed before the next top-level key or
// the end of the document, or closed with a further non-comment line before
// it, is reported as a schedule, since nothing can tell what triggers such a
// document holds and no unbalanced bracket may make the gap check pass.
func hasScheduleTrigger(workflow []byte) bool {
	// The byte order mark U+FEFF, in UTF-8.
	text := strings.TrimPrefix(string(workflow), "\xef\xbb\xbf")

	// rootColumn is the indent of the document's top-level keys, -1 until
	// its first content line.
	rootColumn := -1

	// inOn is set while the lines of the top-level on: key's value are read;
	// valueStarted once the value's first content is read, so that a flow
	// value is told apart from a block one.
	inOn, valueStarted := false, false

	// flow is the on: key's flow value while its lines are read, nil
	// otherwise: its bracket depth, quote state and whether it closed.
	var flow *flowValue

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t\r")

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		comment := strings.HasPrefix(trimmed, "#")

		if indent == 0 && isDocumentMarker(trimmed) {
			// The document ends, and the on: value with it.
			if flow != nil && !flow.closed {
				return true
			}

			rootColumn, inOn, valueStarted, flow = -1, false, false, nil

			continue
		}

		if rootColumn < 0 {
			if comment {
				continue
			}

			rootColumn = indent
		}

		if inOn && endsTheOnValue(trimmed, indent, rootColumn) {
			// The next top-level key: an unclosed flow value is malformed.
			if flow != nil && !flow.closed {
				return true
			}

			inOn, flow = false, nil
		}

		if inOn {
			if flow != nil {
				if scheduleWord.MatchString(trimmed) {
					return true
				}

				switch {
				case flow.closed && !comment:
					// A line after the closed value that is no top-level
					// key: malformed.
					return true
				case flow.closed || (comment && !flow.inQuote()):
				default:
					flow.closed = flow.add(line)
				}

				continue
			}

			if comment {
				continue
			}

			if scheduleWord.MatchString(trimmed) {
				return true
			}

			if !valueStarted {
				// The value's first content below the key: an explicit
				// key's `:`, an anchor and a tag are no part of it.
				rest := stripProperties(inlineValue(strings.TrimPrefix(trimmed, ":")))
				if rest == "" {
					continue
				}

				valueStarted = true

				if startsAFlowValue(rest) {
					flow = &flowValue{}
					flow.closed = flow.add(rest)
				}
			}

			continue
		}

		if comment || indent != rootColumn {
			continue
		}

		value, ok := topLevelOn(trimmed)
		if !ok {
			continue
		}

		inOn, valueStarted = true, false

		value = stripProperties(value)

		switch {
		case value == "":
		case startsAFlowValue(value):
			// The whole on: line is matched, its comment included: fail
			// closed.
			if scheduleWord.MatchString(line) {
				return true
			}

			valueStarted = true
			flow = &flowValue{}
			flow.closed = flow.add(value)
		default:
			if scheduleWord.MatchString(value) {
				return true
			}

			valueStarted = true
		}
	}

	// The file ended inside the on: key's flow value: fail closed.
	return flow != nil && !flow.closed
}

// isDocumentMarker reports whether a trimmed line at column 0 starts or ends
// a YAML document: ---, or ..., alone or before a blank.
func isDocumentMarker(trimmed string) bool {
	for _, marker := range []string{"---", "..."} {
		if trimmed == marker || strings.HasPrefix(trimmed, marker+" ") || strings.HasPrefix(trimmed, marker+"\t") {
			return true
		}
	}

	return false
}

// endsTheOnValue reports whether a line ends the top-level on: key's value:
// a line at or left of the root column that is no comment and does not start
// with `:` (an explicit key's value), `}` or `]` (a flow value's close).
func endsTheOnValue(trimmed string, indent, rootColumn int) bool {
	if indent > rootColumn || strings.HasPrefix(trimmed, "#") {
		return false
	}

	return !strings.HasPrefix(trimmed, ":") && !strings.HasPrefix(trimmed, "}") && !strings.HasPrefix(trimmed, "]")
}

// startsAFlowValue reports whether a value, its anchor and tag removed, is a
// flow mapping or sequence.
func startsAFlowValue(value string) bool {
	return strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[")
}

// stripProperties returns a value with the anchors (&name) and tags (!tag,
// !!map) before it removed: each runs up to the next blank, and a value that
// is properties only is empty.
func stripProperties(value string) string {
	value = strings.TrimSpace(value)

	for strings.HasPrefix(value, "&") || strings.HasPrefix(value, "!") {
		i := strings.IndexAny(value, " \t")
		if i < 0 {
			return ""
		}

		value = strings.TrimSpace(value[i:])
	}

	return value
}

// flowValue is a top-level on: key's flow mapping or sequence read line by
// line: the depth of the brackets it holds open, the quoted scalar it is
// inside and whether it closed. A bracket inside a quoted scalar or a comment
// counts for nothing.
type flowValue struct {
	depth int
	// quote is the quote character of the scalar the text read so far ends
	// inside, 0 when it ends outside one.
	quote byte
	// last is the last byte read outside a quoted scalar that is no blank, 0
	// before the first: a quote opens a scalar only after a byte that starts
	// one.
	last byte
	// closed is set once every bracket the value opened is closed.
	closed bool
}

// inQuote reports whether the text read so far ends inside a quoted scalar.
func (f *flowValue) inQuote() bool { return f.quote != 0 }

// add reads one more line of the value and reports whether the value is
// closed: every bracket it opened is closed, outside a quoted scalar. A quote
// opens a quoted scalar only where a scalar starts, after nothing yet or after
// a `{`, a `[`, a `,` or a `:` and blanks; elsewhere, as in what's, it is a
// byte of a plain scalar. A # that starts the line or follows a blank outside a
// quoted scalar starts a comment, which ends the line.
func (f *flowValue) add(line string) bool {
	for i := 0; i < len(line); i++ {
		c := line[i]

		switch {
		case f.quote == '"' && c == '\\':
			i++

			continue
		case f.quote == '\'' && c == '\'' && i+1 < len(line) && line[i+1] == '\'':
			// '' is an escaped quote inside a single-quoted scalar.
			i++

			continue
		case f.quote != 0:
			if c == f.quote {
				f.quote = 0
				f.last = c
			}

			continue
		case c == ' ' || c == '\t':
			continue
		case (c == '"' || c == '\'') && startsAScalar(f.last):
			f.quote = c

			continue
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return f.depth == 0
		case c == '{' || c == '[':
			f.depth++
		case c == '}' || c == ']':
			f.depth--
		}

		f.last = c

		if f.depth == 0 {
			return true
		}
	}

	return f.depth == 0 && f.quote == 0
}

// startsAScalar reports whether a scalar may start after the byte last, the
// last one read outside a quoted scalar that is no blank: at the start of the
// value, or after a flow indicator or a key's colon.
func startsAScalar(last byte) bool {
	switch last {
	case 0, '{', '[', ',', ':':
		return true
	}

	return false
}

// onKey matches a trimmed line that opens the top-level on key, its trailing
// comment removed: an optional explicit-key `? `, the key bare or quoted,
// optional blanks, and a colon with the value after it. The colon may be
// missing only after `? `, whose value follows on a `:` line.
var onKey = regexp.MustCompile(`^(\?[ \t]+)?(?:on|"on"|'on')[ \t]*(:(.*))?$`)

// topLevelOn returns the value after a trimmed line's on key, its trailing
// comment removed and its blanks trimmed, and whether the line opens one.
func topLevelOn(trimmed string) (string, bool) {
	m := onKey.FindStringSubmatch(inlineValue(trimmed))
	if m == nil || (m[1] == "" && m[2] == "") {
		return "", false
	}

	return strings.TrimSpace(m[3]), true
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

	workflowsDir := filepath.Join(repositoryRoot(t), ".github", "workflows")

	if err := checkAuditWorkflows(t, workflowsDir); err != nil {
		t.Error(err)
	}
}

// TestCheckAuditWorkflows pins the wiring of the audit's workflow check: the
// rule over the workflow the audit is published by is stated for a workflows
// directory, and read whatever the directory holds. The named workflow's
// absence is the "no trigger" case only for that workflow; it is no reason to
// stop before the workflows that run the publish program, since a report
// published by a workflow the check never reads skips the gap check (ADR 0040).
func TestCheckAuditWorkflows(t *testing.T) {
	t.Parallel()

	const (
		// publishing is a workflow that runs the audit's publish program, the
		// shape the audit's own workflow has from milestone 3 on.
		publishing = "jobs:\n  publish:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go run ./ci/auditpublish\n"

		// unrelated runs no part of the audit.
		unrelated = "on:\n  push:\n    branches: [main]\njobs:\n  build:\n    steps:\n      - run: make check\n"
	)

	scheduledPublishing := "on:\n  schedule:\n    - cron: '17 4 3 * *'\n" + publishing

	tests := []struct {
		name string
		// workflows are the directory's files, keyed by their slash-separated
		// names.
		workflows map[string]string
		// wantErrContains is the base name of the workflow the error must
		// name, empty when no error is wanted.
		wantErrContains string
	}{
		{
			name:            "another workflow runs the publish program on a schedule",
			workflows:       map[string]string{"other.yml": scheduledPublishing},
			wantErrContains: "other.yml",
		},
		{
			// The check's existing rule, which this wiring must keep: any
			// workflow but the audit's own that names the publish program is a
			// failure, whether or not a schedule sits behind it.
			name:            "another workflow runs the publish program with no schedule",
			workflows:       map[string]string{"other.yml": "on:\n  workflow_dispatch:\n" + publishing},
			wantErrContains: "other.yml",
		},
		{
			name:            "the audit's workflow holds a schedule trigger",
			workflows:       map[string]string{filepath.Base(auditWorkflow): scheduledPublishing},
			wantErrContains: filepath.Base(auditWorkflow),
		},
		{
			name:      "the audit's workflow holds no schedule trigger",
			workflows: map[string]string{filepath.Base(auditWorkflow): "on:\n  workflow_dispatch:\n" + publishing},
		},
		{
			name:      "no workflows at all",
			workflows: map[string]string{},
		},
		{
			name:      "an unrelated workflow alone",
			workflows: map[string]string{"ci.yml": unrelated},
		},
		{
			name: "the audit's workflow with no schedule trigger and an unrelated workflow",
			workflows: map[string]string{
				filepath.Base(auditWorkflow): "on:\n  workflow_dispatch:\n" + publishing,
				"ci.yml":                     unrelated,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := checkAuditWorkflows(t, writeWorkflows(t, tc.workflows))

			switch {
			case tc.wantErrContains == "":
				if err != nil {
					t.Fatalf("checkAuditWorkflows() = %v, want no error", err)
				}
			case err == nil:
				t.Fatalf("checkAuditWorkflows() = nil, want an error naming %s", tc.wantErrContains)
			case !strings.Contains(err.Error(), tc.wantErrContains):
				t.Errorf("checkAuditWorkflows() = %v, want an error naming the offending workflow %s", err, tc.wantErrContains)
			}
		})
	}
}

// checkAuditWorkflows checks a workflows directory against the rule over the
// workflow the audit is published by, and returns every breach joined, nil
// when there is none: the audit's own workflow holds no schedule trigger while
// GapThresholdDays is the literal, and no other workflow names the publish
// program, whether or not a schedule sits behind it. A missing audit workflow
// is the "no trigger" case for that file alone: the other workflows are read
// all the same, since a report published by a workflow the check never reads
// skips the gap check (ADR 0040).
func checkAuditWorkflows(t *testing.T, dir string) error {
	t.Helper()

	auditName := filepath.Base(auditWorkflow)

	var breaches []error

	workflow, err := os.ReadFile(filepath.Join(dir, auditName))

	switch {
	case err == nil:
		if hasScheduleTrigger(workflow) {
			breaches = append(breaches, fmt.Errorf("%s holds a schedule trigger, want none while %s is the literal %d: milestone 3 adds the trigger and changes this check to the cron's longest gap plus 7 days",
				auditName, gapThresholdName, gapThresholdDays))
		}
	case errors.Is(err, fs.ErrNotExist):
		// No audit workflow yet: no trigger, and the scan below still runs.
	default:
		t.Fatalf("read %s: %v", auditName, err)
	}

	// The audit's report is published by the audit's own workflow alone: one
	// that ran the same program with no schedule behind it would publish a
	// report the gap check never sees (ADR 0040).
	for _, name := range workflowsRunningThePublishProgram(t, dir) {
		breaches = append(breaches, fmt.Errorf("%s runs the audit's publish program (it names %s), want %s alone to run it: a report published with no schedule behind it skips the gap check",
			name, auditPublishProgram, auditName))
	}

	return errors.Join(breaches...)
}

// writeWorkflows writes the workflows into a fresh temporary directory and
// returns it: the keys are the files' slash-separated names.
func writeWorkflows(t *testing.T, files map[string]string) string {
	t.Helper()

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

	return dir
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

	dir := writeWorkflows(t, files)

	got := workflowsRunningThePublishProgram(t, dir)
	slices.Sort(got)

	want := []string{"nested/deep.yml", "other.yml"}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the workflows that run the publish program mismatch (-want +got):\n%s", diff)
	}
}

// TestHasScheduleTrigger pins the matcher the workflow check reads: every form
// GitHub accepts for a schedule trigger, flow mappings and sequences broken over
// several lines among them, and none of the texts that merely name it. A `{` or a
// `[` the document never closes is malformed YAML, and the matcher fails closed
// on it, reading the file as one that holds a schedule: no unbalanced bracket
// makes the gap check pass a workflow whose triggers nothing can read. The
// workflow check's own regexp was a two-space block-style line, so a flow-style
// or a four-space trigger would have passed unseen.
//
// A quote opens a quoted scalar only where a scalar starts: an apostrophe in the
// middle of a plain scalar, as in `what's run`, opens nothing, and a comment is
// no scalar of the flow mapping at all, so neither its apostrophe nor its braces
// may be read as one.
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
			// A flow mapping may be broken over lines, as a workflow reformatted
			// by hand or by a tool is: the schedule sits on a line of its own,
			// indented inside the braces, and is no less a trigger for it.
			name:     "a flow mapping over several lines",
			workflow: "on: {\n  workflow_dispatch: {},\n  schedule: [{cron: '0 3 * * 1'}]\n}\njobs:\n  audit:\n    runs-on: ubuntu-latest\n",
			want:     true,
		},
		{
			name:     "a quoted on key with a flow mapping over several lines",
			workflow: "\"on\": {\n  workflow_dispatch: {},\n  'schedule': [{cron: '0 3 * * 1'}]\n}\n",
			want:     true,
		},
		{
			// An apostrophe in the middle of a plain scalar is no quote: YAML opens
			// a quoted scalar where a scalar starts, after a `{`, a `[`, a `,` or a
			// `:` and blanks, and leaves an apostrophe inside a plain scalar alone.
			// Reading `what's` as an opening quote swallowed the brackets after it,
			// the flow mapping ended at the wrong brace, and the schedule on the
			// line below was never read: the workflow passed as one with no trigger
			// while GitHub runs it on a schedule.
			name: "a flow mapping over several lines with apostrophes in plain scalars",
			workflow: `on: {
  workflow_dispatch: {inputs: {mode: {description: what's run, type: choice, options: [full, what's new]}}},
  schedule: [{cron: '0 3 * * 1'}]
}
jobs:
  audit:
    runs-on: ubuntu-latest
`,
			want: true,
		},
		{
			// The same reading of a plain scalar, with the bracket of a later flow
			// sequence behind its apostrophe: the quote the apostrophe opened took
			// the sequence for quoted text, so the depth it closes was never counted.
			name: "a flow mapping over several lines with an apostrophe and a later bracket",
			workflow: `on: {
  workflow_dispatch: {inputs: {mode: {description: what's new, options: [full, fast]}}},
  schedule: [{cron: '0 3 * * 1'}]
}
jobs:
  audit:
    runs-on: ubuntu-latest
`,
			want: true,
		},
		{
			// A comment is no scalar, so the apostrophe and the closing braces in
			// it are neither a quote nor brackets of the flow mapping, and the
			// schedule behind the comment is still a trigger.
			name: "a comment with an apostrophe and braces inside a flow mapping",
			workflow: `on: {
  workflow_dispatch: {} # it's done }}}
  schedule: [{cron: '0 3 * * 1'}]
}
jobs:
  audit:
    runs-on: ubuntu-latest
`,
			want: true,
		},
		{
			// A plain scalar's apostrophe is no reason to report a schedule: the
			// mapping holds none, and reading every flow value that holds an
			// apostrophe as one would pass a workflow nothing triggers on a
			// schedule.
			name:     "a flow mapping over several lines with an apostrophe and no schedule",
			workflow: "on: {\n  workflow_dispatch: {inputs: {mode: {description: what's run}}}\n}\njobs:\n  audit:\n    runs-on: ubuntu-latest\n",
			want:     false,
		},
		{
			// The same form with no schedule: the trigger is read from the keys
			// the flow mapping holds, not from its braces.
			name:     "a flow mapping over several lines without a schedule",
			workflow: "on: {\n  push: {branches: [main]},\n  workflow_dispatch: {}\n}\njobs:\n  audit:\n    runs-on: ubuntu-latest\n",
			want:     false,
		},
		{
			// A `{` or a `[` that the document never closes is malformed YAML, so
			// nothing can tell what triggers it holds: the check fails closed and
			// reads the file as one that has a schedule, which the workflow check
			// reports rather than passes. A gap check bypassed by one unbalanced
			// bracket would be no check at all.
			name:     "an unclosed flow mapping",
			workflow: "on: {\n  workflow_dispatch: {}\n",
			want:     true,
		},
		{
			name:     "an unclosed flow mapping ended by the file",
			workflow: "on: {workflow_dispatch: {}",
			want:     true,
		},
		{
			// The next top-level key ends the on: value while a brace is still
			// open: malformed just the same, and read just as failing closed.
			name:     "an unclosed flow mapping ended by the next top-level key",
			workflow: "on: {\n  workflow_dispatch: {}\njobs:\n  audit:\n    runs-on: ubuntu-latest\n",
			want:     true,
		},
		{
			name:     "an unclosed flow sequence",
			workflow: "on: [workflow_dispatch\n",
			want:     true,
		},
		{
			name:     "an unclosed flow sequence ended by the next top-level key",
			workflow: "on: [workflow_dispatch\njobs:\n  audit:\n    runs-on: ubuntu-latest\n",
			want:     true,
		},
		{
			name:     "an unclosed flow mapping that holds a schedule anyway",
			workflow: "on: {\n  schedule: [{cron: '0 3 * * 1'}]\n",
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
		{
			// The value of the on: key may carry an anchor before the block
			// mapping it holds, which is what GitHub reads: the trigger is the
			// mapping the anchor names, and the schedule sits in it.
			name:     "an anchored on key",
			workflow: "on: &triggers\n  schedule:\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			// A tag on the value is no trigger of its own either: the mapping
			// tagged as one is the mapping the on: key holds.
			name:     "a tagged on key",
			workflow: "on: !!map\n  schedule:\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			// YAML lets blanks sit between a block mapping's key and its colon,
			// and GitHub reads the key the same: schedule : is schedule:.
			name:     "a blank before the nested schedule key's colon",
			workflow: "on:\n  schedule :\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			name:     "a blank before the top-level on key's colon",
			workflow: "on :\n  schedule:\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			// An explicit key, ? schedule, is the same key as schedule:, and the
			// value after it may open on its own line.
			name:     "an explicit nested schedule key",
			workflow: "on:\n  ? schedule\n  : - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			name:     "an explicit top-level on key",
			workflow: "? on\n:\n  schedule:\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			// A byte order mark is no part of the first key: YAML strips it
			// before the document is read, and a reader that keeps it sees no
			// on: key at all.
			name:     "a byte order mark before the on key",
			workflow: "\ufeffon:\n  schedule:\n    - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			// A flow mapping may be the value of a bare on: key and start on
			// the line after it, indented: the nested line is the value, not a
			// nested key of a block mapping.
			name:     "a flow mapping on the line after a bare on key",
			workflow: "on:\n  {schedule: [{cron: '0 3 * * 1'}]}\n",
			want:     true,
		},
		{
			name:     "a flow sequence on the line after a bare on key",
			workflow: "on:\n  [schedule, push]\n",
			want:     true,
		},
		{
			// A document may be indented as a whole: the top-level keys sit at
			// column 2 and the on: mapping under them, which is the same document
			// as one written at column 0.
			name:     "a root mapping indented as a whole",
			workflow: "  on:\n    schedule:\n      - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			// A document start marker may be followed by an indented document,
			// whose first content line is then no top-level key at column 0.
			name:     "an indented document after the document start marker",
			workflow: "---\n  on:\n    schedule:\n      - cron: '17 4 3 * *'\n",
			want:     true,
		},
		{
			// The words a schedule would be found by sit in the job's script, not
			// in a trigger: reading the file for them would pass a workflow no
			// schedule runs, so the block's other triggers and the script under
			// jobs: stay no schedule.
			name:     "other triggers and a schedule word in a step's run text",
			workflow: "on:\n  push:\n    branches: [main]\n  workflow_dispatch:\njobs:\n  audit:\n    steps:\n      - run: |\n          echo schedule\n",
			want:     false,
		},
		{
			// The word in a comment on the on: key or in its block is no
			// trigger: a comment is no key of the mapping, whatever it names.
			name:     "a comment that names a schedule on the on key",
			workflow: "on: # no schedule here\n  # no schedule here either\n  workflow_dispatch:\njobs:\n  audit:\n    runs-on: ubuntu-latest\n",
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
