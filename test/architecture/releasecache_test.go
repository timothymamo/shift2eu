package architecture_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestReleaseWorkflowDisablesTheGoCache pins that every step of the release workflow that would read or
// write the repository's Actions cache turns it off: setup-go with cache: false, which turns its module
// and build cache off, and golangci-lint-action with skip-cache: true, the only input that action has for
// it. release.yml says why on its goreleaser job: the Go cache would carry state over from the test job or
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

	if read := len(goSetupSteps(src)); read != named {
		t.Errorf("%s names actions/setup-go@ %d times, but the reader recognised %d setup-go steps of it, want the same number: a step the reader does not recognise is a step no cache check reads", workflow, named, read)
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
// key outside the step's with:, or by nothing at all. Neither is another action's step, or a step whose
// run: block happens to hold a line that looks like one.
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

// cacheOffActions are the actions whose steps must turn the Actions cache off: the Go setup's module and
// build cache, and the lint action's, which read and write the same cache.
var cacheOffActions = []string{"actions/setup-go", "golangci/golangci-lint-action"}

// goCacheGap is one step of a workflow that would use the Actions cache and does not turn it off.
type goCacheGap struct {
	// line is the line the step's dash is on, counted from 1.
	line int
	// action is the action the step uses, without its pin.
	action string
}

// goCacheGaps returns, in the order they run, the steps of the workflow that use one of cacheOffActions
// and hold no cache: false. It reads the document by indentation, one step at a time: a step starts at a
// dash at or left of the open step's key column, holds the lines indented past it, and owns a cache: key
// at any depth inside it; the lines of a literal block (run: |) are a script, not a mapping, so a line of
// one that reads like a step or a cache key sets nothing. A cache: other than false, true and a missing
// one alike, leaves the step in the result: only so many words turn the cache off.
func goCacheGaps(workflow []byte) []goCacheGap {
	type step struct {
		line   int
		action string
		cache  bool
		// block is the indent of a key whose value is a literal block, -1 for none.
		block int
	}

	var (
		gaps    []goCacheGap
		current *step
		keyCol  int
	)

	flush := func() {
		if current != nil && current.action != "" && !current.cache {
			gaps = append(gaps, goCacheGap{line: current.line, action: current.action})
		}

		current = nil
	}

	for i, raw := range strings.Split(string(workflow), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		text := strings.TrimLeft(line, " ")
		indent := len(line) - len(text)

		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		if current != nil && current.block >= 0 {
			if indent > current.block {
				continue
			}

			current.block = -1
		}

		if startsAStep(text) && (current == nil || indent < keyCol) {
			flush()

			keyCol = indent + 2
			current = &step{line: i + 1, block: -1}
			text = strings.TrimSpace(strings.TrimPrefix(text, "-"))
			indent = keyCol

			if text == "" {
				continue
			}
		}

		if current == nil {
			continue
		}

		if indent < keyCol {
			flush()

			continue
		}

		key, value, ok := stepKey(text)
		if !ok {
			continue
		}

		if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
			current.block = indent
		}

		if indent > keyCol {
			if key == "cache" && inlineValue(value) == "false" {
				current.cache = true
			}

			continue
		}

		if key == "uses" {
			if action, ok := cacheOffAction(inlineValue(value)); ok {
				current.action = action
			}
		}
	}

	flush()

	return gaps
}

// startsAStep reports whether text starts a block sequence entry: a dash alone or followed by a space.
func startsAStep(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

// stepKey splits a line of a step into its plain key and the value after it, a value that is only a
// comment returned as empty, and reports whether the line is a mapping line at all.
func stepKey(text string) (key, value string, ok bool) {
	i := strings.Index(text, ":")
	if i <= 0 {
		return "", "", false
	}

	rest := text[i+1:]
	if rest != "" && !strings.HasPrefix(rest, " ") {
		return "", "", false
	}

	return text[:i], strings.TrimSpace(rest), true
}

// cacheOffAction returns the action one of cacheOffActions names a uses: value with, and whether it names
// one: the value is the action's repository, its pin, and the release it is as a comment.
func cacheOffAction(uses string) (string, bool) {
	name, _, _ := strings.Cut(uses, "@")

	if slices.Contains(cacheOffActions, name) {
		return name, true
	}

	return "", false
}
