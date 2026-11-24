package lint_test

import (
	"fmt"
	"io/fs"
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

// claudeCodePinPaths are the paths the pin check watches: the one pin file, the
// reviewer's workflow, which still installs the CLI from that file, the
// installer that consumes the pin, the workflow that runs it, the check's own
// workflow, the check's code and go.mod, whose go line names the toolchain the
// check is built with. A push to main filters on them; a pull request runs the
// job on every change, and the changes step decides whether the pin's paths are
// among them (issue #452, ADR 0040's model-job amendment, issue #277).
var claudeCodePinPaths = []string{
	claudeCodePinFile,
	aiReviewWorkflowPath,
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
	claudeCodePinFile,
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

// claudeCodePinEventHead names whose tip the event file the step reads reports
// as the pull request's head, and the one case in which the run has no event
// file at all.
type claudeCodePinEventHead int

const (
	// claudeCodePinEventHeadPRTip: the tip of the pull request's branch, the
	// second parent of the merge commit actions/checkout gives the step.
	claudeCodePinEventHeadPRTip claudeCodePinEventHead = iota
	// claudeCodePinEventHeadBase: another commit than that parent, the base the
	// branch starts from: a head the merge commit does not hold.
	claudeCodePinEventHeadBase
	// claudeCodePinEventNoFile: GITHUB_EVENT_PATH names no file, as on a run
	// whose event file the runner did not write.
	claudeCodePinEventNoFile
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
	// deleted are paths the branch deletes outright.
	deleted []string
	// mainChanged are the paths main gains a commit for before a merge into the
	// branch, which a merge needs to have anything to merge.
	mainChanged []string
	// mergeMainIntoBranch makes HEAD a merge of main into the pull request's
	// branch, so its first parent is the branch's tip and its second main's: the
	// parents in the other order than actions/checkout gives them.
	mergeMainIntoBranch bool
	// singleParent checks out the branch's tip, which has one parent, instead of
	// a merge commit: the history a checkout without the pull request's merge
	// ref gives.
	singleParent bool
	// head names whose tip the event file reports as the pull request's head.
	head claudeCodePinEventHead
	// decision is what the step must write for the history.
	decision claudeCodePinDecision
}

// claudeCodePinHistory is the repository one case is run in, with the two shas
// the event file and the merge commit are about.
type claudeCodePinHistory struct {
	// dir is the repository the step runs in.
	dir string
	// base is the sha of the commit on main the branch starts from.
	base string
	// prTip is the sha of the tip of the pull request's branch.
	prTip string
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
// The step trusts neither the parent count alone nor the order of the parents:
// a merge of main into the pull request's branch has the parents the other way
// round, so its diff HEAD^1 HEAD shows what main added and not what the pull
// request changes. So on pull_request the step reads the pull request's head sha
// from the event file the runner writes (jq -er .pull_request.head.sha
// "$GITHUB_EVENT_PATH") and skips only when that sha is HEAD's second parent;
// an event file that does not hold that sha, or is not there at all, runs the
// check.
//
// The test runs the step's script as the workflow writes it, with bash -e in
// temporary git repositories: each a base commit on main, the pull request's
// branch a commit on top of it, and a real merge commit with two parents. The
// environment is the step's own: GITHUB_EVENT_NAME, GITHUB_OUTPUT,
// GITHUB_EVENT_PATH pointing at a JSON file the test writes with the shas of its
// own repository, no git configuration of the machine (GIT_CONFIG_GLOBAL and
// GIT_CONFIG_SYSTEM point at /dev/null), a fixed identity and date, and no
// network.
//
// The test needs bash, git and jq on PATH, and fails rather than skips without
// them: the dev VM installs all three (dev/lima.yaml) and the job runs on
// ubuntu-latest, so their absence is a broken machine, and a skip would let
// every row below pass unrun.
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

	// The step runs in a shell with git and jq: a machine without any of the
	// three fails here rather than skips, because a skip would report the
	// rows below as run where no row ran.
	for _, tool := range []string{"bash", "git", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is not on PATH, and the changes step needs bash, git and jq: the dev VM installs all three (dev/lima.yaml) and the job runs on ubuntu-latest, so a machine without them fails here rather than skipping the rows below: %v", tool, err)
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
			name:     "a pull request that deletes the install script runs the check",
			event:    claudeCodePinPullRequest,
			deleted:  []string{aiReviewInstallScript},
			decision: claudeCodePinRuns,
		},
		{
			name:     "a pull request that adds a pin file whose name is not ASCII runs the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{"ci/claudecodepin/ä.go"},
			decision: claudeCodePinRuns,
		},
		{
			// The parents the other way round: HEAD^1 is the pull request's tip,
			// so HEAD^1's diff names what main added, never the ai-review.yml
			// this pull request changes. Only the head sha the event file holds
			// tells the step which parent is the pull request's.
			name:                "a merge whose first parent is the pull request's tip does not skip the check",
			event:               claudeCodePinPullRequest,
			changed:             []string{aiReviewWorkflowPath},
			mainChanged:         []string{"README.md"},
			mergeMainIntoBranch: true,
			decision:            claudeCodePinNeverSkipped,
		},
		{
			name:     "an event whose head is no parent of the merge commit does not skip the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{"README.md"},
			head:     claudeCodePinEventHeadBase,
			decision: claudeCodePinNeverSkipped,
		},
		{
			name:     "a run with no event file does not skip the check",
			event:    claudeCodePinPullRequest,
			changed:  []string{"README.md"},
			head:     claudeCodePinEventNoFile,
			decision: claudeCodePinNeverSkipped,
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

	// One case per path of the push filter, so a path added there and not to the
	// step's case fails here rather than letting a pull request that changes it
	// pass unopened.
	cases = append(cases, claudeCodePinPushFilterCases(t, lines)...)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			history := claudeCodePinRepo(t, tc)
			eventPath := claudeCodePinEventFile(t, history, tc.head)

			pin, out, err := claudeCodePinRun(t, history.dir, tc.event, eventPath, script)

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

// claudeCodePinPushFilterCases returns one case per path of the workflow's own
// on.push.paths, each a pull request that changes a file the filter names: a pin
// path added to the push filter and not to the changes step's case fails here,
// instead of every pull request that changes it passing as if the pin were
// untouched. The filter's own list is pinned by
// TestClaudeCodePinWorkflowIsClosed, so a path dropped from it fails there.
func claudeCodePinPushFilterCases(t *testing.T, lines []yamlread.Line) []claudeCodePinCase {
	t.Helper()

	on, ok := aiReviewEntry(t, lines, "on")
	if !ok {
		t.Fatalf("%s: the workflow has no on: block of triggers", claudeCodePinWorkflowPath)
	}

	push, ok := aiReviewEntry(t, on.Block, "push")
	if !ok {
		t.Fatalf("%s: on: names no push: trigger, whose paths: filter is the list of the pin's paths", claudeCodePinWorkflowPath)
	}

	paths, ok := aiReviewEntry(t, push.Block, "paths")
	if !ok {
		t.Fatalf("%s: on: push: names no paths:, so this test cannot derive one case per pin path from it", claudeCodePinWorkflowPath)
	}

	patterns, err := yamlread.List(paths)
	if err != nil {
		t.Fatalf("%s: on: push: read paths:: %v", claudeCodePinWorkflowPath, err)
	}

	cases := make([]claudeCodePinCase, 0, len(patterns))
	for _, pattern := range patterns {
		path := claudeCodePinFileIn(t, pattern)
		cases = append(cases, claudeCodePinCase{
			name:     "a pull request that changes " + path + " runs the check, as the push filter names " + pattern,
			event:    claudeCodePinPullRequest,
			changed:  []string{path},
			decision: claudeCodePinRuns,
		})
	}

	return cases
}

// claudeCodePinFileIn returns one file path the push filter's pattern matches:
// the pattern itself when it names a file, or a nested file below it when it
// ends in /**, the glob GitHub reads as every file below that directory.
func claudeCodePinFileIn(t *testing.T, pattern string) string {
	t.Helper()

	if dir, ok := strings.CutSuffix(pattern, "/**"); ok {
		return dir + "/internal/x/y.go"
	}

	if strings.ContainsAny(pattern, "*?[") {
		t.Fatalf("%s: on: push: paths: %q is a glob this test cannot name one file of, want a path or a /** directory", claudeCodePinWorkflowPath, pattern)
	}

	return pattern
}

// claudeCodePinRepo builds the history tc describes in a temporary git
// repository and returns it: a base commit on main, the pull request's branch a
// commit on top of it, and then either the branch's tip, one parent, or a merge
// commit whose first parent is main and whose second is the branch, as
// actions/checkout gives it on a pull request. A case that asks for
// mergeMainIntoBranch gets the parents the other way round: the branch merges
// main into itself, so the first parent is the branch's tip.
func claudeCodePinRepo(t *testing.T, tc claudeCodePinCase) claudeCodePinHistory {
	t.Helper()

	dir := t.TempDir()

	claudeCodePinGit(t, dir, "init", "--quiet", "--initial-branch=main")

	for _, path := range claudeCodePinBase {
		claudeCodePinWrite(t, dir, path, "base\n")
	}

	claudeCodePinGit(t, dir, "add", "--all")
	claudeCodePinGit(t, dir, "commit", "--quiet", "--message=base")

	history := claudeCodePinHistory{dir: dir, base: claudeCodePinRevParse(t, dir, "HEAD")}

	claudeCodePinGit(t, dir, "checkout", "--quiet", "-b", "pull-request")

	for _, path := range tc.changed {
		claudeCodePinWrite(t, dir, path, "changed by the pull request\n")
	}

	for _, path := range tc.deleted {
		claudeCodePinGit(t, dir, "rm", "--quiet", path)
	}

	if tc.renamed != [2]string{} {
		claudeCodePinGit(t, dir, "mv", tc.renamed[0], tc.renamed[1])
	}

	claudeCodePinGit(t, dir, "add", "--all")
	claudeCodePinGit(t, dir, "commit", "--quiet", "--message=pull request")

	history.prTip = claudeCodePinRevParse(t, dir, "HEAD")

	if tc.mergeMainIntoBranch {
		claudeCodePinGit(t, dir, "checkout", "--quiet", "main")

		for _, path := range tc.mainChanged {
			claudeCodePinWrite(t, dir, path, "changed by main\n")
		}

		claudeCodePinGit(t, dir, "add", "--all")
		claudeCodePinGit(t, dir, "commit", "--quiet", "--message=main moves on")

		claudeCodePinGit(t, dir, "checkout", "--quiet", "pull-request")
		claudeCodePinGit(t, dir, "merge", "--quiet", "--no-ff", "--message=merge", "main")

		return history
	}

	if tc.singleParent {
		return history
	}

	claudeCodePinGit(t, dir, "checkout", "--quiet", "main")
	claudeCodePinGit(t, dir, "merge", "--quiet", "--no-ff", "--message=merge", "pull-request")

	return history
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

// claudeCodePinRevParse returns the sha rev names in the repository dir.
func claudeCodePinRevParse(t *testing.T, dir, rev string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", rev)
	cmd.Dir = dir
	cmd.Env = childEnv(claudeCodePinGitConfig...)

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s in %s: %v", rev, dir, err)
	}

	return strings.TrimSpace(string(out))
}

// claudeCodePinEventFile writes the event file the runner gives the step, as
// GitHub writes the pull request event, and returns its path: the sha of the
// pull request's head that the history's event names, or the path of a file it
// never writes for a run with no event file at all.
func claudeCodePinEventFile(t *testing.T, history claudeCodePinHistory, head claudeCodePinEventHead) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "event.json")

	sha := ""
	switch head {
	case claudeCodePinEventHeadPRTip:
		sha = history.prTip
	case claudeCodePinEventHeadBase:
		sha = history.base
	case claudeCodePinEventNoFile:
		// The runner wrote no event there, or the run has none: jq -er fails
		// and the step still never skips the check.
		return path
	}

	doc := `{"pull_request":{"head":{"sha":"` + sha + `"}}}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write the step's GITHUB_EVENT_PATH file: %v", err)
	}

	return path
}

// claudeCodePinRun runs the changes step's script in the repository dir with the
// event the history belongs to and the event file's path, and returns what the
// step wrote for pin, its output and its error. bash -e is the shell GitHub
// gives a run: step on Linux; the script itself sets -euo pipefail.
func claudeCodePinRun(t *testing.T, dir, event, eventPath, script string) (string, []byte, error) {
	t.Helper()

	output := filepath.Join(t.TempDir(), "github-output")

	if err := os.WriteFile(output, nil, 0o600); err != nil {
		t.Fatalf("create the step's GITHUB_OUTPUT file: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", script)
	cmd.Dir = dir
	cmd.Env = childEnv(slices.Concat(claudeCodePinGitConfig, []string{
		"GITHUB_EVENT_NAME=" + event,
		"GITHUB_EVENT_PATH=" + eventPath,
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

// claudeCodePinCheckName is the name of the status check the maintainer can
// mark required, and claudeCodePinJobID the id of the job that reports it: a job
// with no name: reports its id instead.
const (
	claudeCodePinCheckName = "Claude Code pin"
	claudeCodePinJobID     = "claude-code-pin"
)

// TestNoOtherWorkflowReportsTheClaudeCodePinCheck pins that
// .github/workflows/claude-code-pin.yml is the only workflow of .github that
// reports a job as "Claude Code pin". A required status check is matched by
// name, so a same-named job in another workflow could post the green check of
// that name while the pin's own job is red, or post two checks of one name of
// which only one has to pass. A job without a name: reports the id of its job,
// so a job whose id is claude-code-pin is refused too (issue #452).
func TestNoOtherWorkflowReportsTheClaudeCodePinCheck(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	dir := filepath.Join(root, filepath.Dir(claudeCodePinWorkflowPath))

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	ours := filepath.Base(claudeCodePinWorkflowPath)

	workflows := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == ours {
			continue
		}

		if ext := filepath.Ext(name); ext != ".yml" && ext != ".yaml" {
			continue
		}

		workflows++

		path := filepath.Join(dir, name)
		claudeCodePinNoSameNamedJob(t, root, path)
	}

	if workflows == 0 {
		t.Fatalf("%s holds no other workflow, want the repository's workflows: the check's name has to be theirs alone", dir)
	}
}

// claudeCodePinNoSameNamedJob fails when the workflow at path has a job that
// reports the pin check's name, by its own name: or by its id.
func claudeCodePinNoSameNamedJob(t *testing.T, root, path string) {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("locate %s in the module: %v", path, err)
	}

	rel = filepath.ToSlash(rel)

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		t.Fatalf("%s: read the workflow: %v", rel, err)
	}

	top, err := yamlread.Mapping(lines)
	if err != nil {
		t.Fatalf("%s: read the workflow: %v", rel, err)
	}

	jobs, ok := claudeCodePinField(top, "jobs")
	if !ok {
		// A workflow without jobs reports no check at all.
		return
	}

	definitions, err := yamlread.Mapping(jobs.Block)
	if err != nil {
		t.Fatalf("%s: read jobs:: %v", rel, err)
	}

	for _, job := range definitions {
		if job.Key == claudeCodePinJobID {
			t.Errorf("%s: line %d: has a job %q, whose id reports a check of that name; %s alone reports %q, so no other workflow may name or be named it (issue #452)", rel, job.Num, job.Key, claudeCodePinWorkflowPath, claudeCodePinCheckName)

			continue
		}

		definition, ok := claudeCodePinField(definitions, job.Key)
		if !ok {
			continue
		}

		name, ok := claudeCodePinFieldIn(t, rel, definition.Block, "name")
		if !ok {
			continue
		}

		if got := claudeCodePinYAMLScalar(t, rel, name); got == claudeCodePinCheckName {
			t.Errorf("%s: line %d: job %q is named %q, the required check %s alone reports; a required check is matched by name, so a second job of that name could mask the pin's red one (issue #452)", rel, name.Num, job.Key, got, claudeCodePinWorkflowPath)
		}
	}
}

// claudeCodePinPinNamingFiles are the files of .github that may hold a string
// that names the Claude Code pin (issue #452):
//
//   - .github/claude-code.yaml, the one pin file, where the version under its #
//     renovate: comment and the integrity beside it live (ADR 0040's
//     model-job amendment, issue #277).
//   - .github/workflows/ai-review.yml, the reviewer's workflow, which installs
//     the CLI from that file: it names the installer, never a pin of its own.
//   - .github/scripts/install-claude-code.sh, the installer itself: its header
//     names its own file and the values the caller gives it.
//   - .github/workflows/claude-code-pin.yml, the check's workflow, which names
//     the pin file and the installer in its paths:, in the case of its changes
//     step and in the comment that explains them.
//
// Nothing else under .github/workflows, .github/scripts and .github/actions may
// hold them. .github/workflows/scheduled-security-audit.yml, the audit's
// workflow, whose model job installs the same CLI from the same file (PRD #175
// milestone 2, ADR 0040), is no entry of this list: it may name the pin only
// where a value of it reaches it from the step that reads the pin file, which
// claudeCodePinAuditWorkflowProblems reads line by line.
var claudeCodePinPinNamingFiles = []string{
	claudeCodePinFile,
	aiReviewWorkflowPath,
	aiReviewInstallScript,
	claudeCodePinWorkflowPath,
}

// claudeCodePinPinNamingStrings are the strings that name the Claude Code pin: a
// second install with a pin of its own under .github/ would be read by no check
// of this repository's, since ci/claudecodepin reads .github/claude-code.yaml
// alone. The two env: names are the ones the pin reaches the install step under,
// and the two beside them the older names a workflow could pin the CLI with
// instead (issue #277, round J).
var claudeCodePinPinNamingStrings = []string{
	"CLAUDE_CODE_INTEGRITY",
	"CLAUDE_CODE_PIN_INTEGRITY",
	"CLAUDE_CODE_PIN_VERSION",
	"install-claude-code.sh",
}

// claudeCodePinSearchedDirs are the directories of .github this test reads: its
// workflows, its scripts and, when the repository has one, its actions. A file
// at .github/ itself (CODEOWNERS, renovate.json5) runs no install.
var claudeCodePinSearchedDirs = []string{".github/workflows", ".github/scripts", ".github/actions"}

// TestClaudeCodePinLivesOnlyInThePinFile pins that the Claude Code CLI is
// installed from one pin, .github/claude-code.yaml: no workflow, script or
// action of .github mentions one of the pin's env: names or
// install-claude-code.sh but the reviewer's workflow, the installer itself, the
// pin file and the check's workflow. ci/claudecodepin reads the version and the
// integrity out of that one file alone, so a second install with a pin of its
// own would be checked by nothing, and would run whatever its pin says (issue
// #452, ADR 0040's model-job amendment, issue #277). The audit's workflow names
// the pin too, and is read line by line instead: it may name it only where a
// value of the pin reaches it from the step that reads the pin file
// (claudeCodePinAuditWorkflowProblems).
func TestClaudeCodePinLivesOnlyInThePinFile(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	scanned := 0
	for _, dir := range claudeCodePinSearchedDirs {
		abs := filepath.Join(root, filepath.FromSlash(dir))

		if _, err := os.Stat(abs); err != nil {
			if os.IsNotExist(err) {
				continue
			}

			t.Fatalf("look for %s: %v", dir, err)
		}

		walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			claudeCodePinScannedFile(t, filepath.ToSlash(rel), path)
			scanned++

			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", dir, walkErr)
		}
	}

	if scanned == 0 {
		t.Fatalf("read no file of %v, want the workflows and scripts that could install the CLI", claudeCodePinSearchedDirs)
	}
}

// TestClaudeCodeAuditWorkflowPinsOnlyWhereItReadsThem pins the narrowed
// exemption of the scheduled audit's workflow (PRD #175 milestone 2, ADR 0040):
// the workflow may name the installer and the pin's env: names where a value of
// .github/claude-code.yaml reaches it from the step that reads the file, and a
// pin written into another step of it is refused, the case a whole-file
// exemption let through.
func TestClaudeCodeAuditWorkflowPinsOnlyWhereItReadsThem(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	src, err := os.ReadFile(filepath.Join(root, claudeCodeAuditWorkflowPath))
	if err != nil {
		t.Fatalf("read %s: %v; the audit's workflow installs the CLI from %s", claudeCodeAuditWorkflowPath, err, claudeCodePinFile)
	}

	accepted := string(src)

	// rewrite returns the workflow with old replaced by new, and fails when old
	// does not stand in it the times the case expects.
	rewrite := func(old, new string, want int) []byte {
		t.Helper()

		if got := strings.Count(accepted, old); got != want {
			t.Fatalf("%s: %q stands in it %d times, want %d: the case rewrites the accepted workflow", claudeCodeAuditWorkflowPath, old, got, want)
		}

		return []byte(strings.ReplaceAll(accepted, old, new))
	}

	// integrity is an npm integrity of the pin file's shape, a pin a workflow
	// would install a CLI from that no check of this repository's reads.
	integrity := "sha512-" + strings.Repeat("A", 86) + "=="

	// copyStep is the step the cases anchor on: the copy of the checkout into the
	// audit directory, which reads no pin and installs no CLI.
	const copyStep = "      - name: Copy the checkout into the audit directory\n"

	tests := []struct {
		name string
		rel  string
		src  []byte
		// wantIn is a part of a problem the case must draw, and the accepted
		// workflow does not; "" means none at all.
		wantIn string
	}{
		{name: "the accepted workflow", rel: claudeCodeAuditWorkflowPath, src: []byte(accepted)},
		{
			name: "an integrity written into a step of its own",
			rel:  claudeCodeAuditWorkflowPath,
			src: rewrite(copyStep, "      - name: Pin the CLI from an integrity of its own\n"+
				"        env:\n"+
				"          CLAUDE_CODE_PIN_INTEGRITY: "+integrity+"\n"+
				copyStep, 1),
			wantIn: "outside the step that reads",
		},
		{
			name: "the pin's old env: name in a step of its own",
			rel:  claudeCodeAuditWorkflowPath,
			src: rewrite(copyStep, "      - name: Pin the CLI the old way\n"+
				"        env:\n"+
				"          CLAUDE_CODE_INTEGRITY: "+integrity+"\n"+
				copyStep, 1),
			wantIn: "an env: name a workflow pins the CLI under",
		},
		{
			name: "the installer in a step that reads no pin",
			rel:  claudeCodeAuditWorkflowPath,
			src: rewrite(copyStep, "      - name: Install the CLI\n"+
				"        run: |\n"+
				"          "+claudeCodeInstallScript+" \"$dir\" \"pkg\" \"\"\n"+
				copyStep, 1),
			wantIn: "outside the step that reads",
		},
		{
			// The step that installs the CLI and the model step bind the
			// integrity to the pin step's output: neither may take another
			// value, or the workflow installs and runs a CLI the pin file does
			// not name.
			name: "a pin name bound to another value",
			rel:  claudeCodeAuditWorkflowPath,
			src: rewrite("CLAUDE_CODE_PIN_INTEGRITY: ${{ steps.claude-code-pin.outputs.integrity }}",
				"CLAUDE_CODE_PIN_INTEGRITY: ${{ secrets.AUDIT_MODEL_PRIVATE_KEY }}", 2),
			wantIn: "sets CLAUDE_CODE_PIN_INTEGRITY to",
		},
		{
			name: "a pin literal on the line that installs the CLI",
			rel:  claudeCodeAuditWorkflowPath,
			src: rewrite(`"@anthropic-ai/claude-code-linux-x64@${CLAUDE_CODE_PIN_VERSION}"`,
				`"@anthropic-ai/claude-code-linux-x64@1.2.3"`, 1),
			wantIn: "beside a pin written into the workflow",
		},
		{
			// The installer takes exactly the pin step's version and integrity
			// as its two last arguments: a line that drops one installs a CLI
			// the pin file does not describe (review round 1, finding R4).
			name: "the installer without the pin's two arguments",
			rel:  claudeCodeAuditWorkflowPath,
			src: rewrite(`"@anthropic-ai/claude-code-linux-x64@${CLAUDE_CODE_PIN_VERSION}" "${CLAUDE_CODE_PIN_INTEGRITY}"`,
				`"@anthropic-ai/claude-code-linux-x64@${CLAUDE_CODE_PIN_VERSION}"`, 1),
			wantIn: "install-claude-code.sh",
		},
		{
			// The alias trick: a literal under another name on a line of its
			// own is still a pin written into the workflow.
			name:   "an integrity behind an alias",
			rel:    claudeCodeAuditWorkflowPath,
			src:    rewrite(`          .github/scripts/install-claude-code.sh`, "          i=\""+integrity+"\"\n          .github/scripts/install-claude-code.sh", 1),
			wantIn: "sha512-",
		},
		{
			// The model step's integrity is exactly the pin step's output: a
			// literal there runs a CLI the pin file does not name.
			name: "a literal -claude-integrity",
			rel:  claudeCodeAuditWorkflowPath,
			src: rewrite(`-claude-integrity "${CLAUDE_CODE_PIN_INTEGRITY}"`,
				`-claude-integrity "`+integrity+`"`, 1),
			wantIn: "-claude-integrity",
		},
		{
			// Every other workflow keeps the whole-file rule: it may name none
			// of the pin's strings at all.
			name:   "a workflow that may not name the pin at all",
			rel:    ".github/workflows/another.yml",
			src:    []byte("jobs:\n  pin:\n    steps:\n      - name: Pin\n        env:\n          CLAUDE_CODE_INTEGRITY: " + integrity + "\n"),
			wantIn: `names "CLAUDE_CODE_INTEGRITY"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := claudeCodePinProblems(tc.rel, tc.src)

			if tc.wantIn == "" {
				if len(problems) != 0 {
					t.Fatalf("claudeCodePinProblems(%s) = %q, want no problem: the workflow installs the CLI from %s, in the steps that take its values from the step that reads it", tc.rel, problems, claudeCodePinFile)
				}

				return
			}

			for _, problem := range problems {
				if strings.Contains(problem, tc.wantIn) {
					return
				}
			}

			t.Errorf("claudeCodePinProblems(%s) = %q, want a problem that holds %q: a pin written into the workflow is checked by nothing (PRD #175 milestone 2, ADR 0040)", tc.rel, problems, tc.wantIn)
		})
	}
}

// claudeCodePinScannedFile fails when the file at path holds a string that names
// the pin and is not one of the files that may, rel being its module-relative
// path for the failure message.
func claudeCodePinScannedFile(t *testing.T, rel, path string) {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}

	for _, problem := range claudeCodePinProblems(rel, src) {
		t.Errorf("%s: %s", rel, problem)
	}
}

// claudeCodePinProblems returns the reasons the file at rel, read from src, may
// not hold what it does: the pin-naming strings a file that is not one of
// claudeCodePinPinNamingFiles names. The audit's workflow names the pin too, so
// it is read line by line (claudeCodePinAuditWorkflowProblems) rather than
// exempted whole: a pin written into any other step of it would be seen by no
// test at all (PRD #175 milestone 2, ADR 0040).
func claudeCodePinProblems(rel string, src []byte) []string {
	if slices.Contains(claudeCodePinPinNamingFiles, rel) {
		return nil
	}

	if rel == claudeCodeAuditWorkflowPath {
		return claudeCodePinAuditWorkflowProblems(src)
	}

	var problems []string

	for _, marker := range claudeCodePinPinNamingStrings {
		if strings.Contains(string(src), marker) {
			problems = append(problems, fmt.Sprintf("names %q: the Claude Code pin lives in %s alone, and ci/claudecodepin reads it there, so a second install with its own pin would be checked by nothing (issue #452, issue #277)", marker, claudeCodePinFile))
		}
	}

	return problems
}

// claudeCodePinAuditWorkflowProblems returns the reasons the scheduled audit's
// workflow, read from src, may not hold what it does (PRD #175 milestone 2, ADR
// 0040). It installs the CLI from the one pin, so it may name the installer and
// the pin's env: names in the step that reads the pin file and in the steps
// that take a value of the pin from that step's outputs, and nowhere else: a
// step that carries a pin of its own is read by no check of this repository's,
// since ci/claudecodepin reads .github/claude-code.yaml alone. The comments are
// read with the steps: a pin in one of them is a pin in the checkout. A
// workflow this reader cannot follow draws the reason it cannot, never no
// message at all.
func claudeCodePinAuditWorkflowProblems(src []byte) []string {
	lines, err := yamlread.SplitLines(src)
	if err != nil {
		return []string{fmt.Sprintf("read the workflow: %v", err)}
	}

	allowed, problems := claudeCodePinAuditWorkflowPinLines(lines)

	for i, text := range strings.Split(string(src), "\n") {
		num := i + 1

		for _, marker := range claudeCodePinPinNamingStrings {
			if !strings.Contains(text, marker) {
				continue
			}

			switch {
			case slices.Contains(claudeCodePinEnvNames, marker):
				problems = append(problems, fmt.Sprintf("line %d names %q, an env: name a workflow pins the CLI under; the version and the integrity live in %s alone and reach a step as the pin step's outputs (CLAUDE.md rule 7, issue #277, round J)", num, marker, claudeCodePinFile))
			case !allowed[num]:
				problems = append(problems, fmt.Sprintf("line %d names %q outside the step that reads %s and the steps that take its values from that step's outputs: a pin written into another step of the audit's workflow is checked by nothing (PRD #175 milestone 2, ADR 0040)", num, marker, claudeCodePinFile))
			case claudeCodeWorkflowIntegrityLit.MatchString(text) || claudeCodeWorkflowVersionLit.MatchString(text):
				problems = append(problems, fmt.Sprintf("line %d names %q beside a pin written into the workflow; the version and the integrity reach it from %s, so Renovate's bump stays in the one file (CLAUDE.md rule 7, issue #277)", num, marker, claudeCodePinFile))
			}
		}
	}

	return problems
}

// claudeCodePinAuditWorkflowPinLines returns the source lines of the audit's
// workflow that may name the pin - those of the step that reads the pin file
// and of every step that takes a pin name from that step's outputs - and the
// problems the workflow's env: bindings draw. A step reads the outputs of the
// pin step when it sets one of the names of claudeCodePinOutputs to the value
// that step wrote: anything else is a pin of the workflow's own. The workflow's
// steps are read here rather than through claudeCodeWorkflowJobs, whose reader
// fails the test on a document it cannot follow; a workflow this reader cannot
// follow is a workflow that must draw a message.
func claudeCodePinAuditWorkflowPinLines(lines []yamlread.Line) (map[int]bool, []string) {
	allowed := map[int]bool{}

	mark := func(step yamlread.Item) {
		allowed[step.Num] = true

		for _, line := range step.Lines {
			allowed[line.Num] = true
		}
	}

	top, err := yamlread.Mapping(lines)
	if err != nil {
		return allowed, []string{fmt.Sprintf("read the workflow's keys: %v", err)}
	}

	jobs, ok := claudeCodePinField(top, "jobs")
	if !ok || len(jobs.Block) == 0 {
		return allowed, []string{"the workflow has no jobs: block, want the audit's model, publish and checks jobs"}
	}

	definitions, err := yamlread.Mapping(jobs.Block)
	if err != nil {
		return allowed, []string{fmt.Sprintf("read the workflow's jobs: %v", err)}
	}

	var problems []string

	for _, job := range definitions {
		if len(job.Block) == 0 {
			continue
		}

		fields, err := yamlread.Mapping(job.Block)
		if err != nil {
			problems = append(problems, fmt.Sprintf("read the keys of the job %s: %v", job.Key, err))

			continue
		}

		stepsEntry, ok := claudeCodePinField(fields, "steps")
		if !ok || len(stepsEntry.Block) == 0 {
			problems = append(problems, fmt.Sprintf("the job %s has no steps: block, so it reads no pin and installs no CLI", job.Key))

			continue
		}

		steps, err := yamlread.Sequence(stepsEntry.Block)
		if err != nil {
			problems = append(problems, fmt.Sprintf("read the steps of the job %s: %v", job.Key, err))

			continue
		}

		for _, step := range steps {
			stepProblems, reads := claudeCodePinAuditWorkflowStep(job.Key, step)

			problems = append(problems, stepProblems...)

			if reads {
				mark(step)
			}
		}
	}

	return allowed, problems
}

// claudeCodePinAuditWorkflowStep returns the problems one step of the audit's
// workflow draws, and whether it takes a value of the pin from the step that
// reads the pin file: it is that step itself, or a step whose env: sets one of
// claudeCodePinOutputs to the value it wrote.
func claudeCodePinAuditWorkflowStep(job string, step yamlread.Item) ([]string, bool) {
	fields, err := yamlread.Mapping(step.Lines)
	if err != nil {
		return []string{fmt.Sprintf("line %d: read the keys of a step of the job %s: %v", step.Num, job, err)}, false
	}

	reads := false

	if id, ok := claudeCodePinField(fields, "id"); ok {
		if value, err := yamlread.Single(id); err == nil && value == claudeCodePinStepName {
			reads = true
		}
	}

	env, ok := claudeCodePinField(fields, "env")
	if !ok {
		return nil, reads
	}

	var problems []string

	for _, line := range env.Block {
		name, value, ok := strings.Cut(line.Text, ":")
		if !ok {
			continue
		}

		name = strings.TrimSpace(name)

		want, ok := claudeCodePinOutputs[name]
		if !ok {
			continue
		}

		if got := strings.TrimSpace(value); got != want {
			problems = append(problems, fmt.Sprintf("line %d sets %s to %q, want %q, what the step that reads %s wrote; a value of the workflow's own is a pin no check holds (CLAUDE.md rule 7, issue #277, round L)", line.Num, name, got, want, claudeCodePinFile))

			continue
		}

		reads = true
	}

	return problems, reads
}

// claudeCodePinField returns the entry of fields for key, and false when
// fields holds none.
func claudeCodePinField(fields []yamlread.Entry, key string) (yamlread.Entry, bool) {
	for _, field := range fields {
		if field.Key == key {
			return field, true
		}
	}

	return yamlread.Entry{}, false
}

// claudeCodePinFieldIn returns the entry for key among the fields of lines, and
// false when they hold none; the file path rel names the workflow in its
// failures.
func claudeCodePinFieldIn(t *testing.T, rel string, lines []yamlread.Line, key string) (yamlread.Entry, bool) {
	t.Helper()

	fields, err := yamlread.Mapping(lines)
	if err != nil {
		t.Fatalf("%s: read the mapping that holds %s:: %v", rel, key, err)
	}

	return claudeCodePinField(fields, key)
}

// claudeCodePinYAMLScalar reads e as one scalar of the workflow at rel.
func claudeCodePinYAMLScalar(t *testing.T, rel string, e yamlread.Entry) string {
	t.Helper()

	value, err := yamlread.Single(e)
	if err != nil {
		t.Fatalf("%s: line %d: read %s:: %v", rel, e.Num, e.Key, err)
	}

	return value
}
