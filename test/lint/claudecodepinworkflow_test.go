package lint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// claudeCodePinWorkflowPath is the workflow that runs the Claude Code pin check
// (issue #276, ADR 0033), and ciWorkflowPath the CI workflow whose action pins it
// shares, so Renovate bumps them together.
const (
	claudeCodePinWorkflowPath = ".github/workflows/claude-code-pin.yml"
	ciWorkflowPath            = ".github/workflows/ci.yml"
)

// claudeCodePinCancel is the one concurrency cancel-in-progress: the pin check
// may have: cancel a pull request's superseded run, never a main run in
// progress, so the latest merge is always re-checked (GitHub may still replace
// a queued main run with a newer one).
const claudeCodePinCancel = "${{ github.event_name == 'pull_request' }}"

// claudeCodePinPaths are the paths the pin check watches: the pinned workflow,
// the installer that consumes the pin, the check's own workflow, the check's
// code and go.mod, whose go line names the toolchain the check is built with. A
// push to main filters on them; a pull request runs the job on every change, and
// the changes step decides whether the pin's paths are among them (issue #452).
var claudeCodePinPaths = []string{
	".github/workflows/ai-review.yml",
	".github/scripts/install-claude-code.sh",
	claudeCodePinWorkflowPath,
	"ci/claudecodepin/**",
	"go.mod",
}

// ciUsesLine matches one uses: line of a workflow with its action, its commit and
// its version comment.
var ciUsesLine = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*([^\s@]+)@([0-9a-f]{40}) # (\S+)$`)

// The changes step the always-run job needs (issue #452).
const (
	// claudeCodePinChangesID is the id of the step that decides whether the
	// change touches the pin's paths: the two steps after it run unless it says
	// the pin is unchanged.
	claudeCodePinChangesID = "changes"

	// claudeCodePinGuard is the if: condition the setup-go step and the check
	// itself carry. A missing or garbled output of the changes step is no
	// "false", so it runs the check rather than skipping it.
	claudeCodePinGuard = "steps.changes.outputs.pin != 'false'"
)

// TestClaudeCodePinWorkflowIsClosed pins the shape of
// .github/workflows/claude-code-pin.yml: it runs the pull request's own copy of
// the check on pull_request and on a push to main, never on pull_request_target,
// with a token that reads contents only, no secret and no environment, every
// action pinned by commit to the pin ci.yml uses, no credential left in
// .git/config, a checkout two commits deep, Go from go.mod, a changes step that
// decides whether the change touches the pin's paths, and one step that runs
// go run ./ci/claudecodepin unless that step says the pin is unchanged. Only the
// push filters on the pin's paths: a pull request runs the job on every change,
// so the maintainer can mark that one check required (issue #452).
func TestClaudeCodePinWorkflowIsClosed(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	src, err := os.ReadFile(filepath.Join(root, claudeCodePinWorkflowPath))
	if err != nil {
		t.Fatalf("read %s: %v; issue #276 runs the Claude Code pin check there", claudeCodePinWorkflowPath, err)
	}

	ci, err := os.ReadFile(filepath.Join(root, ciWorkflowPath))
	if err != nil {
		t.Fatalf("read %s: %v", ciWorkflowPath, err)
	}

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		t.Fatalf("%s: read the workflow: %v", claudeCodePinWorkflowPath, err)
	}

	// raw are the source lines, which a literal block scalar is read from.
	raw := strings.Split(string(src), "\n")

	// Content lines only: the comments may name what the workflow never does.
	for _, line := range lines {
		for _, banned := range []string{"pull_request_target", "secrets.", "environment:", "github.event."} {
			if strings.Contains(line.Text, banned) {
				t.Errorf("%s: line %d holds %q: the check runs with no secret, no environment and no expression of the event (issue #276)", claudeCodePinWorkflowPath, line.Num, banned)
			}
		}
	}

	t.Run("a push to main never cancels an earlier main run", func(t *testing.T) {
		t.Parallel()

		concurrency, ok := aiReviewEntry(t, lines, "concurrency")
		if !ok || concurrency.Value != "" {
			t.Fatalf("%s: want a top-level concurrency: block, so the check's runs share one group (issue #276)", claudeCodePinWorkflowPath)
		}

		fields := aiReviewFields(t, concurrency.Block, "concurrency:")

		cancel, ok := aiReviewFind(fields, "cancel-in-progress")
		if !ok {
			t.Fatalf("%s: concurrency: names no cancel-in-progress:, want %q", claudeCodePinWorkflowPath, claudeCodePinCancel)
		}

		if got := aiReviewScalar(t, cancel); got != claudeCodePinCancel {
			t.Errorf("%s: line %d: cancel-in-progress is %q, want %q: a new pull request cancels the run in progress, but a push to main re-checks what was merged without cancelling an earlier main run (issue #276)", claudeCodePinWorkflowPath, cancel.Num, got, claudeCodePinCancel)
		}
	})

	t.Run("pull_request on every change, push to main filtered to the pin's paths", func(t *testing.T) {
		t.Parallel()

		on, ok := aiReviewEntry(t, lines, "on")
		if !ok || on.Value != "" {
			t.Fatalf("%s: want an on: block of triggers", claudeCodePinWorkflowPath)
		}

		triggers := aiReviewFields(t, on.Block, "on:")

		var names []string
		for _, trigger := range triggers {
			names = append(names, trigger.Key)

			fields := aiReviewFields(t, trigger.Block, "on: "+trigger.Key+":")

			branches, ok := aiReviewFind(fields, "branches")
			if !ok {
				t.Errorf("%s: on: %s: names no branches:, want [main]", claudeCodePinWorkflowPath, trigger.Key)
			} else if got, err := yamlread.List(branches); err != nil || !slices.Equal(got, []string{"main"}) {
				t.Errorf("%s: on: %s: branches: %v (%v), want [main]", claudeCodePinWorkflowPath, trigger.Key, got, err)
			}

			paths, ok := aiReviewFind(fields, "paths")

			switch trigger.Key {
			case "push":
				if !ok {
					t.Errorf("%s: on: push: names no paths:, want exactly %v: a push to main re-checks what it merged only when the pin changed", claudeCodePinWorkflowPath, claudeCodePinPaths)

					continue
				}

				got, err := yamlread.List(paths)
				if err != nil {
					t.Errorf("%s: on: push: read paths:: %v", claudeCodePinWorkflowPath, err)

					continue
				}

				slices.Sort(got)
				want := slices.Clone(claudeCodePinPaths)
				slices.Sort(want)

				if !slices.Equal(got, want) {
					t.Errorf("%s: on: push: paths: %v, want exactly %v: a push to main re-checks the pin only when it changed, so an npm outage never fails an unrelated merge", claudeCodePinWorkflowPath, got, want)
				}
			case "pull_request":
				// An always-run job can be a required check; a filtered one
				// cannot, because GitHub would wait for a run that never
				// comes on every other pull request (issue #452).
				if ok {
					t.Errorf("%s: on: pull_request: line %d filters the paths: %v, want no paths: filter: the job runs on every pull request and the changes step decides whether the pin's paths changed", claudeCodePinWorkflowPath, paths.Num, aiReviewText(paths))
				}
			}
		}

		slices.Sort(names)
		if !slices.Equal(names, []string{"pull_request", "push"}) {
			t.Errorf("%s: on: %v, want exactly pull_request and push", claudeCodePinWorkflowPath, names)
		}
	})

	jobs, ok := aiReviewEntry(t, lines, "jobs")
	if !ok {
		t.Fatalf("%s: the workflow has no jobs:", claudeCodePinWorkflowPath)
	}

	jobFields := aiReviewFields(t, jobs.Block, "jobs:")
	if len(jobFields) != 1 {
		t.Fatalf("%s: jobs: holds %d jobs, want the one check", claudeCodePinWorkflowPath, len(jobFields))
	}

	job := jobFields[0]

	t.Run("permissions read contents only", func(t *testing.T) {
		t.Parallel()

		top, ok := aiReviewEntry(t, lines, "permissions")
		if !ok || top.Value != "{}" {
			t.Errorf("%s: the top-level permissions: is not {}, want nothing granted outside the job", claudeCodePinWorkflowPath)
		}

		perms, ok := aiReviewEntry(t, job.Block, "permissions")
		if !ok {
			t.Fatalf("%s: job %s declares no permissions:", claudeCodePinWorkflowPath, job.Key)
		}

		scopes := aiReviewFields(t, perms.Block, "the job's permissions:")
		if len(scopes) != 1 || scopes[0].Key != "contents" || aiReviewScalar(t, scopes[0]) != "read" {
			t.Errorf("%s: job %s: permissions: %v, want contents: read alone", claudeCodePinWorkflowPath, job.Key, scopes)
		}
	})

	t.Run("named Claude Code pin", func(t *testing.T) {
		t.Parallel()

		name, ok := aiReviewEntry(t, job.Block, "name")
		if !ok || aiReviewScalar(t, name) != "Claude Code pin" {
			t.Errorf("%s: job %s is not named \"Claude Code pin\", the check name the Renovate note and docs/dev-environment.md give", claudeCodePinWorkflowPath, job.Key)
		}
	})

	steps, ok := aiReviewEntry(t, job.Block, "steps")
	if !ok {
		t.Fatalf("%s: job %s has no steps:", claudeCodePinWorkflowPath, job.Key)
	}

	items, err := yamlread.Sequence(steps.Block)
	if err != nil {
		t.Fatalf("%s: read steps:: %v", claudeCodePinWorkflowPath, err)
	}

	t.Run("actions pinned as ci.yml pins them, checkout two commits deep without credentials, Go from go.mod", func(t *testing.T) {
		t.Parallel()

		ciPins := map[string][]string{}
		for _, m := range ciUsesLine.FindAllStringSubmatch(string(ci), -1) {
			pin := m[2] + " # " + m[3]
			if !slices.Contains(ciPins[m[1]], pin) {
				ciPins[m[1]] = append(ciPins[m[1]], pin)
			}
		}

		uses := map[string]bool{}
		for _, m := range ciUsesLine.FindAllStringSubmatch(string(src), -1) {
			uses[m[1]] = true

			if pins := ciPins[m[1]]; len(pins) != 1 || pins[0] != m[2]+" # "+m[3] {
				t.Errorf("%s: %s@%s # %s, want the one pin %s uses, %v, so Renovate bumps them together", claudeCodePinWorkflowPath, m[1], m[2], m[3], ciWorkflowPath, pins)
			}
		}

		var checkout, setupGo bool
		for _, step := range items {
			fields := aiReviewStepFields(t, step)

			action, ok := aiReviewUses(t, fields)
			if !ok {
				continue
			}

			if !aiReviewPinnedAction.MatchString(action) {
				t.Errorf("%s: line %d: uses: %s is not pinned to a commit", claudeCodePinWorkflowPath, step.Num, action)
			}

			name, _, _ := strings.Cut(action, "@")
			if !uses[name] {
				t.Errorf("%s: line %d: uses: %s has no version comment", claudeCodePinWorkflowPath, step.Num, action)
			}

			switch name {
			case "actions/checkout":
				checkout = true

				if v, ok := aiReviewWithScalar(t, fields, "persist-credentials"); !ok || v != "false" {
					t.Errorf("%s: line %d: the checkout does not set persist-credentials: false", claudeCodePinWorkflowPath, step.Num)
				}

				// The merge commit and its first parent: the two the changes
				// step diffs, so the job needs no file list from the API and no
				// pull-requests: read token (issue #452).
				if v, ok := aiReviewWithScalar(t, fields, "fetch-depth"); !ok || v != "2" {
					t.Errorf("%s: line %d: the checkout sets fetch-depth: %s, want 2: the changes step diffs the merge commit against its first parent", claudeCodePinWorkflowPath, step.Num, v)
				}
			case "actions/setup-go":
				setupGo = true

				if v, ok := aiReviewWithScalar(t, fields, "go-version-file"); !ok || v != "go.mod" {
					t.Errorf("%s: line %d: setup-go does not read go-version-file: go.mod", claudeCodePinWorkflowPath, step.Num)
				}
			default:
				t.Errorf("%s: line %d: uses %s, want actions/checkout and actions/setup-go alone", claudeCodePinWorkflowPath, step.Num, name)
			}
		}

		if !checkout || !setupGo {
			t.Errorf("%s: checkout %t, setup-go %t, want both", claudeCodePinWorkflowPath, checkout, setupGo)
		}
	})

	t.Run("one step runs the check", func(t *testing.T) {
		t.Parallel()

		const check = "go run ./ci/claudecodepin"

		var runs []string
		checks := 0

		for _, step := range items {
			run, ok := aiReviewFind(aiReviewStepFields(t, step), "run")
			if !ok {
				continue
			}

			script := claudeCodePinRunText(t, raw, run)
			runs = append(runs, script)

			if script == check {
				checks++
			}
		}

		if checks != 1 {
			t.Errorf("%s: the steps run %q, want exactly one step to run exactly %s (issue #276)", claudeCodePinWorkflowPath, runs, check)
		}
	})

	t.Run("a changes step decides, and the check runs unless it says the pin is unchanged", func(t *testing.T) {
		t.Parallel()

		changes := 0
		for _, step := range items {
			fields := aiReviewStepFields(t, step)

			id, ok := aiReviewFind(fields, "id")
			if !ok || aiReviewScalar(t, id) != claudeCodePinChangesID {
				continue
			}

			changes++

			run, ok := aiReviewFind(fields, "run")
			if !ok {
				t.Errorf("%s: line %d: the step with id: %s has no run:, so it cannot decide whether this change touches the Claude Code pin (issue #452)", claudeCodePinWorkflowPath, step.Num, claudeCodePinChangesID)

				continue
			}

			// The event reaches the script through GITHUB_EVENT_NAME, so no
			// expression of the event is ever spliced into it.
			if script := claudeCodePinRunText(t, raw, run); strings.Contains(script, "${{") {
				t.Errorf("%s: line %d: the script of the step with id: %s holds a ${{ }} expression, want none: the event is read from GITHUB_EVENT_NAME (issue #452)", claudeCodePinWorkflowPath, run.Num, claudeCodePinChangesID)
			}
		}

		if changes != 1 {
			t.Errorf("%s: the steps declare %d steps with id: %s, want one: the job runs on every pull request, so a step must decide whether the pin's paths changed (issue #452)", claudeCodePinWorkflowPath, changes, claudeCodePinChangesID)
		}

		gated := map[string]string{}
		for _, step := range items {
			fields := aiReviewStepFields(t, step)

			cond, ok := claudeCodePinIf(t, fields)
			if !ok {
				continue
			}

			gated[claudeCodePinStepLabel(t, raw, fields)] = cond
		}

		want := []string{"actions/setup-go", "go run ./ci/claudecodepin"}

		var labels []string
		for label := range gated {
			labels = append(labels, label)
		}

		slices.Sort(labels)
		if !slices.Equal(labels, want) {
			t.Errorf("%s: the steps that carry an if: are %q, want exactly %q: the two steps that need the registry run unless the changes step says the pin is unchanged (issue #452)", claudeCodePinWorkflowPath, labels, want)
		}

		for _, label := range want {
			// GitHub runs the step unless the condition is false, and takes the
			// condition with or without ${{ }}; either form is the same guard.
			if got := gated[label]; got != claudeCodePinGuard && got != "${{ "+claudeCodePinGuard+" }}" {
				t.Errorf("%s: the %s step carries if: %q, want %q: a missing or garbled output of the changes step runs the check, never skips it (issue #452)", claudeCodePinWorkflowPath, label, got, claudeCodePinGuard)
			}
		}
	})
}

// claudeCodePinPullRequest and claudeCodePinPush are the values of
// GITHUB_EVENT_NAME the changes step decides from, spelled as GitHub sets them.
const (
	claudeCodePinPullRequest = "pull_request"
	claudeCodePinPush        = "push"
)

// claudeCodePinBase is the tree the pull request's branch starts from: the pin's
// paths, which every case leaves alone but the ones that change or rename one,
// and a file no case touches.
var claudeCodePinBase = []string{
	"README.md",
	".github/workflows/ai-review.yml",
	".github/scripts/install-claude-code.sh",
	claudeCodePinWorkflowPath,
	"ci/claudecodepin/main.go",
	"go.mod",
}

// claudeCodePinGitConfig keeps git out of the host's configuration and clock: no
// global or system config (so no hook and no template of the machine), a fixed
// identity and one fixed date, so the histories the changes step reads are the
// same everywhere, and no network.
var claudeCodePinGitConfig = []string{
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_CONFIG_SYSTEM=/dev/null",
	"TZ=UTC",
	"GIT_AUTHOR_NAME=shift2eu test",
	"GIT_AUTHOR_EMAIL=test@example.com",
	"GIT_COMMITTER_NAME=shift2eu test",
	"GIT_COMMITTER_EMAIL=test@example.com",
	"GIT_AUTHOR_DATE=2024-01-02T03:04:05+0000",
	"GIT_COMMITTER_DATE=2024-01-02T03:04:05+0000",
}

// claudeCodePinDecision is what the changes step must decide for one history.
type claudeCodePinDecision int

const (
	// claudeCodePinSkipped: the step writes pin=false, so the job passes
	// without contacting the registry.
	claudeCodePinSkipped claudeCodePinDecision = iota
	// claudeCodePinRuns: the step writes pin=true, so the job runs the check.
	claudeCodePinRuns
	// claudeCodePinNeverSkipped: for an event or a history the step cannot
	// decide from, it writes pin=true or fails; it never writes pin=false.
	claudeCodePinNeverSkipped
)

// claudeCodePinCase is one history the changes step runs in, and what it must
// decide.
type claudeCodePinCase struct {
	name string
	// event is the GITHUB_EVENT_NAME of the run.
	event string
	// changed are the paths the pull request's branch adds or changes, each
	// with content the base's copy does not have.
	changed []string
	// renamed is a path the branch renames, and its new name.
	renamed [2]string
	// singleParent checks out the branch's tip, which has one parent, instead of
	// a merge commit: the history a checkout without the pull request's merge
	// ref gives.
	singleParent bool
	// decision is what the step must write for the history.
	decision claudeCodePinDecision
}

// TestClaudeCodePinChangesStepDecides pins the decision the workflow's changes
// step makes on its own (issue #452). The job runs on every pull request, so the
// step reads what merging the pull request changes on main: with fetch-depth 2
// actions/checkout has the merge commit refs/pull/<n>/merge and its first
// parent, main before the merge, and git diff --no-renames --name-only -z HEAD^1
// HEAD is exactly that change, with no token beyond contents: read, no
// pagination and no file list of the API.
//
// The step writes pin=false only when the event is pull_request, HEAD has
// exactly two parents, the diff succeeded and no changed path is one of the
// pin's; every other case writes pin=true or fails, so a run the step cannot
// decide never skips the check. --no-renames lists the old path of a rename, and
// -z ends each name with NUL, so neither a rename away from a pin path nor a
// quote or newline in a name can hide one.
//
// The test runs the step's script as the workflow writes it, with bash -e in
// temporary git repositories: each a base commit on main, the pull request's
// branch a commit on top of it, and a real merge commit with two parents. The
// environment is the step's own: GITHUB_EVENT_NAME, GITHUB_OUTPUT, no git
// configuration of the machine (GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM point at
// /dev/null), a fixed identity and date, and no network.
func TestClaudeCodePinChangesStepDecides(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	src, err := os.ReadFile(filepath.Join(root, claudeCodePinWorkflowPath))
	if err != nil {
		t.Fatalf("read %s: %v; issue #452 makes the pin check an always-run job there", claudeCodePinWorkflowPath, err)
	}

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		t.Fatalf("%s: read the workflow: %v", claudeCodePinWorkflowPath, err)
	}

	script := claudeCodePinChangesScript(t, lines, strings.Split(string(src), "\n"))

	// The step runs in a shell with git and nothing else: skip where the machine
	// has no such shell rather than fail its repository's checks.
	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH, so the changes step cannot run here: %v", tool, err)
		}
	}

	cases := []claudeCodePinCase{
		{
			name:     "a pull request that touches no pin path skips the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{"README.md", "internal/cli/cli.go"},
			decision: claudeCodePinSkipped,
		},
		{
			name:     "a pull request that changes the reviewer's workflow runs the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{".github/workflows/ai-review.yml"},
			decision: claudeCodePinRuns,
		},
		{
			name:     "a pull request that changes the install script runs the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{".github/scripts/install-claude-code.sh"},
			decision: claudeCodePinRuns,
		},
		{
			name:     "a pull request that changes this workflow runs the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{claudeCodePinWorkflowPath},
			decision: claudeCodePinRuns,
		},
		{
			name:     "a pull request that changes the check's code runs the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{"ci/claudecodepin/x.go"},
			decision: claudeCodePinRuns,
		},
		{
			name:     "a pull request that changes go.mod runs the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{"go.mod"},
			decision: claudeCodePinRuns,
		},
		{
			name:     "a pull request that renames the reviewer's workflow runs the check",
			event:    claudeCodePinPullRequest,
			renamed:  [2]string{".github/workflows/ai-review.yml", ".github/workflows/review.yml"},
			decision: claudeCodePinRuns,
		},
		{
			name:     "a path that only starts like a pin path is no pin path",
			event:    claudeCodePinPullRequest,
			changed:  []string{"ci/claudecodepinx/a", "go.mod.bak", ".github/workflows/ai-review.yml.bak"},
			decision: claudeCodePinSkipped,
		},
		{
			name:         "a commit that is no merge does not skip the check",
			event:        claudeCodePinPullRequest,
			changed:      []string{"README.md"},
			singleParent: true,
			decision:     claudeCodePinNeverSkipped,
		},
		{
			name:     "a push to main does not skip the check",
			event:    claudeCodePinPush,
			changed:  []string{"README.md"},
			decision: claudeCodePinNeverSkipped,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := claudeCodePinRepo(t, tc)
			pin, out, err := claudeCodePinRun(t, dir, tc.event, script)

			want := "true"
			if tc.decision == claudeCodePinSkipped {
				want = "false"
			}

			if err != nil {
				if tc.decision == claudeCodePinNeverSkipped {
					// A failed step fails the job, which reports no pass the
					// check did not make: as fail-closed as pin=true.
					t.Logf("the changes step failed in the repository of %q, want pin=%s or a failed step (issue #452): %v\n%s", tc.name, want, err, out)

					return
				}

				t.Fatalf("the changes step failed for %q, want pin=%s (issue #452): %v\n%s", tc.name, want, err, out)
			}

			if pin != want {
				t.Errorf("the changes step wrote pin=%q for %q, want pin=%s (issue #452)\n%s", pin, tc.name, want, out)
			}
		})
	}
}

// claudeCodePinChangesScript returns the script of the step with id: changes,
// read from the claude-code-pin job's run: literal block, and fails when the
// workflow has no such step: the always-run job needs it to decide whether this
// change touches the pin (issue #452).
func claudeCodePinChangesScript(t *testing.T, lines []yamlread.Line, raw []string) string {
	t.Helper()

	jobs, ok := aiReviewEntry(t, lines, "jobs")
	if !ok {
		t.Fatalf("%s: the workflow has no jobs:", claudeCodePinWorkflowPath)
	}

	job, ok := aiReviewEntry(t, jobs.Block, "claude-code-pin")
	if !ok {
		t.Fatalf("%s: jobs: names no claude-code-pin job", claudeCodePinWorkflowPath)
	}

	steps, ok := aiReviewEntry(t, job.Block, "steps")
	if !ok {
		t.Fatalf("%s: job claude-code-pin has no steps:", claudeCodePinWorkflowPath)
	}

	items, err := yamlread.Sequence(steps.Block)
	if err != nil {
		t.Fatalf("%s: read steps:: %v", claudeCodePinWorkflowPath, err)
	}

	for _, step := range items {
		fields := aiReviewStepFields(t, step)

		id, ok := aiReviewFind(fields, "id")
		if !ok || aiReviewScalar(t, id) != claudeCodePinChangesID {
			continue
		}

		run, ok := aiReviewFind(fields, "run")
		if !ok {
			t.Fatalf("%s: line %d: the step with id: %s has no run:, so the job cannot decide whether this change touches the Claude Code pin (issue #452)", claudeCodePinWorkflowPath, step.Num, claudeCodePinChangesID)
		}

		return claudeCodePinRunText(t, raw, run)
	}

	t.Fatalf("%s: no step declares id: %s, so the always-run job cannot decide whether this change touches the Claude Code pin, and the check cannot be a required one (issue #452)", claudeCodePinWorkflowPath, claudeCodePinChangesID)

	return ""
}

// claudeCodePinRunText returns the script of one step's run:: the inline value,
// or the lines of its literal block joined by newlines.
func claudeCodePinRunText(t *testing.T, raw []string, run yamlread.Entry) string {
	t.Helper()

	if !aiReviewBlockIndicator(run.Value) {
		return strings.TrimSpace(aiReviewText(run))
	}

	script, err := yamlread.LiteralBlock(raw, run)
	if err != nil {
		t.Fatalf("%s: line %d: read the run: script: %v", claudeCodePinWorkflowPath, run.Num, err)
	}

	return script
}

// claudeCodePinIf returns the if: condition one step carries, and false when it
// carries none.
func claudeCodePinIf(t *testing.T, fields []yamlread.Entry) (string, bool) {
	t.Helper()

	cond, ok := aiReviewFind(fields, "if")
	if !ok {
		return "", false
	}

	return aiReviewScalar(t, cond), true
}

// claudeCodePinStepLabel names one step in a failure message: the action it
// uses, without its commit, or the script its run: holds.
func claudeCodePinStepLabel(t *testing.T, raw []string, fields []yamlread.Entry) string {
	t.Helper()

	if action, ok := aiReviewUses(t, fields); ok {
		name, _, _ := strings.Cut(action, "@")

		return name
	}

	run, ok := aiReviewFind(fields, "run")
	if !ok {
		return "step with no uses: and no run:"
	}

	return claudeCodePinRunText(t, raw, run)
}

// claudeCodePinRepo builds the history tc describes in a temporary git
// repository and returns its directory: a base commit on main, the pull
// request's branch a commit on top of it, and then either the branch's tip, one
// parent, or a merge commit whose first parent is main and whose second is the
// branch, as actions/checkout gives it on a pull request.
func claudeCodePinRepo(t *testing.T, tc claudeCodePinCase) string {
	t.Helper()

	dir := t.TempDir()

	claudeCodePinGit(t, dir, "init", "--quiet", "--initial-branch=main")

	for _, path := range claudeCodePinBase {
		claudeCodePinWrite(t, dir, path, "base\n")
	}

	claudeCodePinGit(t, dir, "add", "--all")
	claudeCodePinGit(t, dir, "commit", "--quiet", "--message=base")

	claudeCodePinGit(t, dir, "checkout", "--quiet", "-b", "pull-request")

	for _, path := range tc.changed {
		claudeCodePinWrite(t, dir, path, "changed by the pull request\n")
	}

	if tc.renamed != [2]string{} {
		claudeCodePinGit(t, dir, "mv", tc.renamed[0], tc.renamed[1])
	}

	claudeCodePinGit(t, dir, "add", "--all")
	claudeCodePinGit(t, dir, "commit", "--quiet", "--message=pull request")

	if tc.singleParent {
		return dir
	}

	claudeCodePinGit(t, dir, "checkout", "--quiet", "main")
	claudeCodePinGit(t, dir, "merge", "--quiet", "--no-ff", "--message=merge", "pull-request")

	return dir
}

// claudeCodePinGit runs git args in dir as the test's fixed author and
// committer, with the host's configuration out of the way, and fails when git
// does.
func claudeCodePinGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = childEnv(claudeCodePinGitConfig...)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v:\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// claudeCodePinWrite writes value to the file at path under dir, with the
// directories above it.
func claudeCodePinWrite(t *testing.T, dir, path, value string) {
	t.Helper()

	abs := filepath.Join(dir, filepath.FromSlash(path))

	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("create the directory of %s: %v", path, err)
	}

	if err := os.WriteFile(abs, []byte(value), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// claudeCodePinRun runs the changes step's script in the repository dir with the
// event the history belongs to, and returns what the step wrote for pin, its
// output and its error. bash -e is the shell GitHub gives a run: step on Linux;
// the script itself sets -euo pipefail.
func claudeCodePinRun(t *testing.T, dir, event, script string) (string, []byte, error) {
	t.Helper()

	output := filepath.Join(t.TempDir(), "github-output")

	if err := os.WriteFile(output, nil, 0o600); err != nil {
		t.Fatalf("create the step's GITHUB_OUTPUT file: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", script)
	cmd.Dir = dir
	cmd.Env = childEnv(slices.Concat(claudeCodePinGitConfig, []string{
		"GITHUB_EVENT_NAME=" + event,
		"GITHUB_OUTPUT=" + output,
	})...)

	out, err := cmd.CombinedOutput()

	return claudeCodePinPin(t, output), out, err
}

// claudeCodePinPin returns the value the step wrote for pin to its GITHUB_OUTPUT
// file, the last one it wrote, and "" when it wrote none.
func claudeCodePinPin(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the step's GITHUB_OUTPUT file: %v", err)
	}

	value := ""
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "pin="); ok {
			value = rest
		}
	}

	return value
}
