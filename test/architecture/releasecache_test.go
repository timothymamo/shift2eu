package architecture_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestReleaseWorkflowDisablesTheGoCache pins that every step of the release workflow that uses one of
// the two actions it knows, actions/setup-go and golangci/golangci-lint-action, turns the repository's
// Actions cache off: setup-go with cache: false, which turns its module and build cache off, and
// golangci-lint-action with skip-cache: true, the only input that action has for it. It checks those two
// actions only: a step of any other action that reads or writes the cache, such as actions/cache, is not
// checked. release.yml says why on its goreleaser job: the Go cache would carry state over from the test job or
// from CI on main into a job that builds what the release publishes, so every step that sets Go up there
// turns it off. A step added later that forgets it would bring that state back unseen, and a
// golangci-lint step writes the same cache, so both are held to the rule.
func TestReleaseWorkflowDisablesTheGoCache(t *testing.T) {
	t.Parallel()

	const workflow = ".github/workflows/release.yml"

	src := readRepositoryFile(t, workflow)

	// A check over the steps its reader never recognises would pass and hold nothing: today the workflow
	// sets Go up in every job that tests, builds the site's programs, lints or releases. The count is
	// quote-insensitive, so a uses: value an editor put in quotes names a step just the same, and every
	// setup-go step the file names must be a step the reader hands the check.
	named := strings.Count(string(src), "actions/setup-go@")
	if named < 5 {
		t.Fatalf("%s names actions/setup-go@ %d times, want at least 5: the release tests, lints, builds and releases in jobs of its own and sets Go up in each", workflow, named)
	}

	// Every action the cache guard knows is held to that backstop, setup-go and the lint action alike: a
	// golangci-lint step added to the workflow later writes the same cache, and a step of either action
	// the reader does not recognise is a step no cache check reads. The lint action has no step in the
	// workflow today, so its count is 0 and a step of it added later is one the reader must hand the
	// check from the day it lands.
	for _, action := range slices.Sorted(maps.Keys(cacheOffInputs)) {
		named := strings.Count(string(src), action+"@")

		if read := len(stepsUsing(src, action)); read != named {
			t.Errorf("%s names %s@ %d times, but the reader recognised %d steps of it, want the same number: a step the reader does not recognise is a step no cache check reads", workflow, action, named, read)
		}
	}

	for _, gap := range goCacheGaps(src) {
		t.Errorf("%s:%d: the %s step turns no cache off, want one: setup-go's cache would carry state over from the test job or from CI on main into the job that builds the release (the comment on the goreleaser job), and the Actions cache of a lint step is the same cache",
			workflow, gap.line, gap.action)
	}
}

// TestGoCacheGaps pins the read the check above is stated in: a step that uses actions/setup-go or
// golangci/golangci-lint-action and does not turn the Actions cache off is reported at the line of its
// dash, and a step that does turn it off is not: setup-go's input for it is cache: false, the lint
// action's is skip-cache: true, and neither step's cache is turned off by the other's word, by a cache:
// key outside the step's with:, or by nothing at all. The step is the same step in the block style, in the
// flow style, and with its uses: key quoted, which GitHub reads as one mapping. Neither is another
// action's step, or a step whose run: block happens to hold a line that looks like one.
func TestGoCacheGaps(t *testing.T) {
	t.Parallel()

	const (
		// pin is the action's repository pin alone, for the uses: value an editor quoted: the release
		// the action is then sits after the closing quote.
		pin = "@0123456789abcdef0123456789abcdef01234567"
		// pinned is the form every action in the workflow has: a repository, a commit and the release
		// it is, as a trailing comment.
		pinned = pin + " # v0.0.0"
	)

	tests := []struct {
		name     string
		workflow string
		want     []goCacheGap
	}{
		{
			name: "a setup-go step without cache",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n" +
				"        with:\n          go-version-file: go.mod\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			name: "a setup-go step with cache: true",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n" +
				"        with:\n          go-version-file: go.mod\n          cache: true\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			name:     "a setup-go step with no with: at all",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n",
			want:     []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			name: "a setup-go step with cache: false",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n" +
				"        with:\n          go-version-file: go.mod\n          cache: false\n",
		},
		{
			name: "a setup-go step with cache: false and a trailing comment",
			workflow: "jobs:\n  goreleaser:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n" +
				"        with:\n          go-version-file: go.mod\n          cache: false # see the comment on the goreleaser job\n",
		},
		{
			name: "a golangci-lint step without cache",
			workflow: "jobs:\n  lint:\n    steps:\n      - uses: actions/checkout" + pinned + "\n" +
				"      - uses: golangci/golangci-lint-action" + pinned + "\n" +
				"        with:\n          version: v0.0.0\n",
			want: []goCacheGap{{line: 5, action: "golangci/golangci-lint-action"}},
		},
		{
			// The uses: value of a step is a scalar like any other, and a quoted one names the same
			// action: a reader that only knows the bare form would pass a setup-go step whose cache is
			// on, which is the step this check exists for.
			name: "a double-quoted uses with no cache",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: \"actions/setup-go" + pin + "\" # v0.0.0\n" +
				"        with:\n          go-version-file: go.mod\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			name: "a single-quoted uses with no cache",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: 'actions/setup-go" + pin + "' # v0.0.0\n" +
				"        with:\n          go-version-file: go.mod\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			// The key of a mapping line is a scalar too, and a quoted one names the same key: a reader
			// that only knows the bare key would not see the step at all, and a step no check sees is
			// the step this check exists for.
			name: "a double-quoted uses key with no cache",
			workflow: "jobs:\n  test:\n    steps:\n      - \"uses\": actions/setup-go" + pin + "\n" +
				"        with:\n          go-version-file: go.mod\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			name: "a single-quoted uses key with no cache",
			workflow: "jobs:\n  test:\n    steps:\n      - 'uses': actions/setup-go" + pin + "\n" +
				"        with:\n          go-version-file: go.mod\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			name: "a double-quoted uses key with cache: false",
			workflow: "jobs:\n  test:\n    steps:\n      - \"uses\": actions/setup-go" + pin + "\n" +
				"        with:\n          go-version-file: go.mod\n          cache: false\n",
		},
		{
			// A step written in the flow style is a mapping like any other: the very step this check
			// exists for, written on one line, must still be the step it reports.
			name:     "a flow-style setup-go step without cache",
			workflow: "jobs:\n  test:\n    steps:\n      - {uses: actions/setup-go" + pin + ", with: {go-version-file: go.mod}}\n",
			want:     []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			name:     "a flow-style setup-go step with cache: false",
			workflow: "jobs:\n  test:\n    steps:\n      - {uses: actions/setup-go" + pin + ", with: {go-version-file: go.mod, cache: false}}\n",
		},
		{
			// skip-cache is the lint action's input, not setup-go's: a step that sets only that has the
			// Go cache on and is a gap.
			name: "a setup-go step with skip-cache: true only",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n" +
				"        with:\n          go-version-file: go.mod\n          skip-cache: true\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			// A step's env: holds its environment, not its inputs: a cache: key there is a variable
			// the step's script may read, and turns nothing off.
			name: "a setup-go step with cache: false under env:",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n" +
				"        env:\n          cache: false\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			name: "a setup-go step with cache: false in a flow env:",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n" +
				"        env: {cache: false}\n",
			want: []goCacheGap{{line: 4, action: "actions/setup-go"}},
		},
		{
			// The lint action has no cache input: cache: false is a word its inputs do not know, and
			// the Action's cache stays on. skip-cache: true is the input that turns it off.
			name: "a golangci-lint step with cache: false only",
			workflow: "jobs:\n  lint:\n    steps:\n      - uses: golangci/golangci-lint-action" + pinned + "\n" +
				"        with:\n          version: v0.0.0\n          cache: false\n",
			want: []goCacheGap{{line: 4, action: "golangci/golangci-lint-action"}},
		},
		{
			name: "a golangci-lint step with skip-cache: true",
			workflow: "jobs:\n  lint:\n    steps:\n      - uses: golangci/golangci-lint-action" + pinned + "\n" +
				"        with:\n          version: v0.0.0\n          skip-cache: true\n",
		},
		{
			name: "a golangci-lint step with skip-cache: false",
			workflow: "jobs:\n  lint:\n    steps:\n      - uses: golangci/golangci-lint-action" + pinned + "\n" +
				"        with:\n          version: v0.0.0\n          skip-cache: false\n",
			want: []goCacheGap{{line: 4, action: "golangci/golangci-lint-action"}},
		},
		{
			name:     "a flow-style golangci-lint step without skip-cache",
			workflow: "jobs:\n  lint:\n    steps:\n      - {uses: golangci/golangci-lint-action" + pin + ", with: {version: v0.0.0}}\n",
			want:     []goCacheGap{{line: 4, action: "golangci/golangci-lint-action"}},
		},
		{
			name:     "a flow-style golangci-lint step with skip-cache: true",
			workflow: "jobs:\n  lint:\n    steps:\n      - {uses: golangci/golangci-lint-action" + pin + ", with: {version: v0.0.0, skip-cache: true}}\n",
		},
		{
			name: "another action's step with no cache",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/checkout" + pinned + "\n" +
				"        with:\n          persist-credentials: false\n",
		},
		{
			name: "two offending steps, in the order they run",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pinned + "\n" +
				"        with:\n          go-version-file: go.mod\n" +
				"      - uses: golangci/golangci-lint-action" + pinned + "\n" +
				"        with:\n          version: v0.0.0\n",
			want: []goCacheGap{
				{line: 4, action: "actions/setup-go"},
				{line: 7, action: "golangci/golangci-lint-action"},
			},
		},
		{
			// A run: block is a script, not a mapping: a line of it that reads like a step with a Go setup
			// is no step, and the cache: line beside it is no cache: false of the step that runs it.
			name: "a step whose run block holds a step that sets Go up",
			workflow: "jobs:\n  test:\n    steps:\n      - name: Echo a step\n        run: |\n" +
				"          - uses: actions/setup-go" + pinned + "\n" +
				"          cache: false\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The gaps are values with no unexported state to read: two gaps are the same step when they
			// name the same line and action, which is what the reader is pinned on.
			if diff := cmp.Diff(tc.want, goCacheGaps([]byte(tc.workflow)), cmpopts.EquateComparable(goCacheGap{})); diff != "" {
				t.Errorf("goCacheGaps() mismatch (-want +got):\n%s\n%q", diff, tc.workflow)
			}
		})
	}
}

// TestGoSetupSteps pins the reader the release workflow's count is checked with: every step that uses
// actions/setup-go is a step it hands the cache check, whichever style the step is written in. A step it
// fails to recognise is a step no gap check reads, so the flow style and a quoted key, which GitHub reads
// as the same step, are read as one.
func TestGoSetupSteps(t *testing.T) {
	t.Parallel()

	// pin is the action's pin alone: a trailing comment, which the block form carries, would end a flow
	// mapping at the # and leave the step unclosed.
	const pin = "@0123456789abcdef0123456789abcdef01234567"

	tests := []struct {
		name     string
		workflow string
		want     int
	}{
		{
			name:     "a block-style step",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pin + "\n",
			want:     1,
		},
		{
			name:     "a flow-style step",
			workflow: "jobs:\n  test:\n    steps:\n      - {uses: actions/setup-go" + pin + ", with: {go-version-file: go.mod}}\n",
			want:     1,
		},
		{
			name:     "a double-quoted uses key",
			workflow: "jobs:\n  test:\n    steps:\n      - \"uses\": actions/setup-go" + pin + "\n",
			want:     1,
		},
		{
			name:     "a single-quoted uses key",
			workflow: "jobs:\n  test:\n    steps:\n      - 'uses': actions/setup-go" + pin + "\n",
			want:     1,
		},
		{
			// The step in every job counts once, whichever style it is written in: the count is what
			// the release workflow's setup-go steps are held to, so one dropped step is one gap check
			// short.
			name: "two steps, one in each style",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: actions/setup-go" + pin + "\n" +
				"  build:\n    steps:\n      - {uses: actions/setup-go" + pin + "}\n",
			want: 2,
		},
		{
			name:     "another action in the flow style",
			workflow: "jobs:\n  test:\n    steps:\n      - {uses: actions/checkout" + pin + "}\n",
			want:     0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := len(goSetupSteps([]byte(tc.workflow))); got != tc.want {
				t.Errorf("goSetupSteps() read %d setup-go steps, want %d:\n%q", got, tc.want, tc.workflow)
			}
		})
	}
}

// TestWorkflowStepsUsing pins the reader the workflow's count is checked with for every action the cache
// guard knows: the steps that use the action asked for, in document order, and no other action's step. A
// step it fails to recognise is a step no gap check reads, so the flow style and a quoted key, which
// GitHub reads as the same step, are read as one, and a line of a run: block is text, not a step.
func TestWorkflowStepsUsing(t *testing.T) {
	t.Parallel()

	const (
		// pin is an action's pin alone: a trailing comment, which the block form carries, would end a flow
		// mapping at the # and leave the step unclosed.
		pin = "@0123456789abcdef0123456789abcdef01234567"
		// lint is the action whose steps the cache guard reads besides the Go setup's.
		lint = "golangci/golangci-lint-action"
	)

	tests := []struct {
		name     string
		workflow string
		action   string
		want     int
	}{
		{
			name:     "a block-style lint step",
			workflow: "jobs:\n  lint:\n    steps:\n      - uses: " + lint + pin + "\n",
			action:   lint,
			want:     1,
		},
		{
			name:     "a flow-style lint step",
			workflow: "jobs:\n  lint:\n    steps:\n      - {uses: " + lint + pin + ", with: {version: v0.0.0}}\n",
			action:   lint,
			want:     1,
		},
		{
			name:     "a double-quoted uses key naming the lint action",
			workflow: "jobs:\n  lint:\n    steps:\n      - \"uses\": " + lint + pin + "\n",
			action:   lint,
			want:     1,
		},
		{
			name:     "a single-quoted uses key naming the lint action",
			workflow: "jobs:\n  lint:\n    steps:\n      - 'uses': " + lint + pin + "\n",
			action:   lint,
			want:     1,
		},
		{
			// The count is per action: a setup-go step is no lint step, and a step of the action asked
			// for alone is counted.
			name:     "a setup-go step when the lint action is asked for",
			workflow: "jobs:\n  test:\n    steps:\n      - uses: " + goSetupAction + pin + "\n",
			action:   lint,
			want:     0,
		},
		{
			name:     "a lint step when setup-go is asked for",
			workflow: "jobs:\n  lint:\n    steps:\n      - uses: " + lint + pin + "\n",
			action:   goSetupAction,
			want:     0,
		},
		{
			// Two lint steps in two jobs, in the two styles: every one counts, or the count backstop is
			// that many steps short.
			name: "two lint steps, one in each style",
			workflow: "jobs:\n  lint:\n    steps:\n      - uses: " + lint + pin + "\n" +
				"  lint-site:\n    steps:\n      - {uses: " + lint + pin + "}\n",
			action: lint,
			want:   2,
		},
		{
			// A run: block is a script, not a mapping: a line of it that reads like a lint step is no
			// step, and must not reach the count.
			name: "a run block that holds a lint step",
			workflow: "jobs:\n  lint:\n    steps:\n      - name: Echo a step\n        run: |\n" +
				"          - uses: " + lint + pin + "\n",
			action: lint,
			want:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := len(stepsUsing([]byte(tc.workflow), tc.action)); got != tc.want {
				t.Errorf("stepsUsing() read %d %s steps, want %d:\n%q", got, tc.action, tc.want, tc.workflow)
			}
		})
	}
}

// goSetupAction is the action that sets Go up, the step the release workflow's count is checked on.
const goSetupAction = "actions/setup-go"

// cacheOffInput is the input and the value that turn an action's Actions cache off.
type cacheOffInput struct {
	key   string
	value string
}

// cacheOffInputs are the actions whose steps must turn the Actions cache off, each with the one input
// that does: the Go setup's module and build cache goes off with cache: false, and the lint action, which
// reads and writes the same cache and has no cache input, turns its own off with skip-cache: true.
var cacheOffInputs = map[string]cacheOffInput{
	goSetupAction:                   {key: "cache", value: "false"},
	"golangci/golangci-lint-action": {key: "skip-cache", value: "true"},
}

// goCacheGap is one step of a workflow that would use the Actions cache and does not turn it off.
type goCacheGap struct {
	// line is the line the step's dash is on, counted from 1.
	line int
	// action is the action the step uses, without its pin.
	action string
}

// goCacheGaps returns, in the order they run, the steps of the workflow that use one of cacheOffInputs'
// actions and do not set that action's cache-off input to its value directly under the step's with:. A
// value other than the one that turns the cache off, the other action's input, a key under env: or
// nested deeper, and a missing with: alike leave the step in the result: only one word per action turns
// its cache off.
func goCacheGaps(workflow []byte) []goCacheGap {
	var gaps []goCacheGap

	for _, s := range workflowSteps(workflow) {
		input, ok := cacheOffInputs[s.action]
		if !ok {
			continue
		}

		if value, set := s.with[input.key]; !set || value != input.value {
			gaps = append(gaps, goCacheGap{line: s.line, action: s.action})
		}
	}

	return gaps
}

// goSetupSteps returns, in the order they run, every step of the workflow that uses actions/setup-go,
// whatever its cache setting: the steps the cache check reads, counted against the workflow's text.
func goSetupSteps(workflow []byte) []workflowStep {
	return stepsUsing(workflow, goSetupAction)
}

// stepsUsing returns, in document order, every step of the workflow whose action, the repository it
// uses without its pin, equals action, whatever its inputs: the steps of that action the cache check
// reads, counted against the workflow's text.
func stepsUsing(workflow []byte, action string) []workflowStep {
	var steps []workflowStep

	for _, s := range workflowSteps(workflow) {
		if s.action == action {
			steps = append(steps, s)
		}
	}

	return steps
}

// workflowStep is one step of a workflow as the cache check reads it.
type workflowStep struct {
	// line is the line the step's dash is on, counted from 1.
	line int
	// action is the repository of the action the step uses, without its pin, empty for none.
	action string
	// with holds the step's inputs, the keys directly under its with:, their values unquoted.
	with map[string]string
}

// workflowSteps reads every block sequence entry of a workflow as a step, in document order. It reads no
// YAML, by indentation: a step starts at a dash at or left of the open step's key column and holds the
// lines indented past it; its uses: and with: are the keys at its key column, bare or quoted, and its
// inputs the keys at the first indent under with: or the entries of a flow mapping after it. A step
// written in the flow style, - {uses: ..., with: {...}}, is read from its line, up to the brace that
// closes it or a # that starts a comment. The lines of a literal or folded block (run: |) are text, not
// a mapping, so a line of one that reads like a step or an input sets nothing.
func workflowSteps(workflow []byte) []workflowStep {
	var (
		steps []workflowStep
		// open is whether the last step still takes lines.
		open bool
		// keyCol is the open step's key column.
		keyCol int
		// section is the open step's key the deeper lines belong to.
		section string
		// withCol is the indent of the inputs under with:, -1 before the first.
		withCol = -1
		// block is the indent of a key whose value is a literal block, -1 for none.
		block = -1
	)

	for i, raw := range strings.Split(string(workflow), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		text := strings.TrimLeft(line, " ")
		indent := len(line) - len(text)

		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		if block >= 0 {
			if indent > block {
				continue
			}

			block = -1
		}

		if startsAStep(text) && (!open || indent < keyCol) {
			steps = append(steps, workflowStep{line: i + 1, with: map[string]string{}})
			open = true
			keyCol = indent + 2
			section = ""
			withCol = -1
			text = strings.TrimSpace(strings.TrimPrefix(text, "-"))
			indent = keyCol

			if text == "" {
				continue
			}

			if strings.HasPrefix(text, "{") {
				readFlowStep(&steps[len(steps)-1], text)

				open = false

				continue
			}
		}

		if !open {
			continue
		}

		if indent < keyCol {
			open = false

			continue
		}

		key, value, ok := mappingLine(text)
		if !ok {
			continue
		}

		if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
			block = indent
		}

		step := &steps[len(steps)-1]

		if indent == keyCol {
			section = key
			withCol = -1

			switch key {
			case "uses":
				step.action = actionName(unquoteScalar(inlineValue(value)))
			case "with":
				if strings.HasPrefix(value, "{") {
					for k, v := range flowMapping(value) {
						step.with[k] = v
					}
				}
			}

			continue
		}

		if section != "with" {
			continue
		}

		if withCol < 0 {
			withCol = indent
		}

		if indent == withCol {
			step.with[key] = unquoteScalar(inlineValue(value))
		}
	}

	return steps
}

// readFlowStep reads a step written as a flow mapping into step: its uses: and the entries of its with:.
func readFlowStep(step *workflowStep, text string) {
	entries := flowMapping(text)

	step.action = actionName(entries["uses"])

	if with := entries["with"]; strings.HasPrefix(with, "{") {
		for k, v := range flowMapping(with) {
			step.with[k] = v
		}
	}
}

// flowMapping returns the entries of the flow mapping text starts with, its keys and scalar values
// unquoted and a nested collection's value as its text: the mapping ends at the brace that closes it, at a
// # that follows a blank outside a quoted scalar, or at the end of the text, and an entry cut short keeps
// what it read. A quote opens a quoted scalar only where a scalar starts, as hasScheduleTrigger reads it,
// and a colon separates a key only when a blank, a comma or a brace follows it.
func flowMapping(text string) map[string]string {
	entries := map[string]string{}

	if !strings.HasPrefix(text, "{") {
		return entries
	}

	var (
		depth int
		quote byte
		last  byte
		start = 1
		colon = -1
	)

	entry := func(end int) {
		if colon < 0 {
			return
		}

		entries[unquoteScalar(text[start:colon])] = unquoteScalar(text[colon+1 : end])
	}

	for i := 0; i < len(text); i++ {
		c := text[i]

		switch {
		case quote == '"' && c == '\\':
			i++

			continue
		case quote != 0:
			if c == quote {
				quote = 0
				last = c
			}

			continue
		case c == ' ' || c == '\t':
			continue
		case (c == '"' || c == '\'') && startsAScalar(last):
			quote = c

			continue
		case c == '#' && (text[i-1] == ' ' || text[i-1] == '\t'):
			entry(i)

			return entries
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
			if depth == 0 {
				entry(i)

				return entries
			}
		case c == ',' && depth == 1:
			entry(i)

			start, colon = i+1, -1
		case c == ':' && depth == 1 && colon < 0 && (i+1 == len(text) || strings.IndexByte(" \t,}", text[i+1]) >= 0):
			colon = i
		}

		last = c
	}

	entry(len(text))

	return entries
}

// startsAStep reports whether text starts a block sequence entry: a dash alone or followed by a space.
func startsAStep(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

// mappingLine splits a line of a step into its key, unquoted when it is quoted, and the value after it,
// trimmed, and reports whether the line is a mapping line at all.
func mappingLine(text string) (key, value string, ok bool) {
	var rest string

	if q := text[0]; q == '"' || q == '\'' {
		end := strings.IndexByte(text[1:], q)
		if end < 0 {
			return "", "", false
		}

		key = text[1 : end+1]
		rest, ok = strings.CutPrefix(text[end+2:], ":")

		if !ok {
			return "", "", false
		}
	} else {
		key, rest, ok = strings.Cut(text, ":")
		if !ok || key == "" {
			return "", "", false
		}
	}

	if rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
		return "", "", false
	}

	return key, strings.TrimSpace(rest), true
}

// unquoteScalar returns a scalar with its blanks trimmed and, when it is double- or single-quoted,
// without its quotes.
func unquoteScalar(scalar string) string {
	scalar = strings.TrimSpace(scalar)

	if len(scalar) >= 2 && (scalar[0] == '"' || scalar[0] == '\'') && scalar[len(scalar)-1] == scalar[0] {
		return scalar[1 : len(scalar)-1]
	}

	return scalar
}

// actionName returns the repository a uses: value names, its @ pin removed.
func actionName(uses string) string {
	name, _, _ := strings.Cut(uses, "@")

	return name
}
