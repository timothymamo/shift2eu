package lint_test

import (
	"os"
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

// claudeCodePinPaths are the paths every trigger of the pin check filters on:
// the pinned workflow, the installer that consumes the pin, the check's own
// workflow, the check's code and go.mod, whose go line names the toolchain the
// check is built with.
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

// TestClaudeCodePinWorkflowIsClosed pins the shape of
// .github/workflows/claude-code-pin.yml: it runs the pull request's own copy of
// the check on pull_request and on a push to main, never on pull_request_target,
// filtered to the pin's paths, with a token that reads contents only, no secret
// and no environment, every action pinned by commit to the pin ci.yml uses, no
// credential left in .git/config, Go from go.mod, and one step that runs
// go run ./ci/claudecodepin.
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

	t.Run("pull_request and push to main, filtered to the pin's paths", func(t *testing.T) {
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
			if !ok {
				t.Errorf("%s: on: %s: names no paths:, want %v", claudeCodePinWorkflowPath, trigger.Key, claudeCodePinPaths)

				continue
			}

			got, err := yamlread.List(paths)
			if err != nil {
				t.Errorf("%s: on: %s: read paths:: %v", claudeCodePinWorkflowPath, trigger.Key, err)

				continue
			}

			for _, want := range claudeCodePinPaths {
				if !slices.Contains(got, want) {
					t.Errorf("%s: on: %s: paths: %v leaves out %s, so a change to it would not run the check", claudeCodePinWorkflowPath, trigger.Key, got, want)
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

	t.Run("actions pinned as ci.yml pins them, checkout without credentials, Go from go.mod", func(t *testing.T) {
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

		var runs []string
		for _, step := range items {
			run, ok := aiReviewFind(aiReviewStepFields(t, step), "run")
			if ok {
				runs = append(runs, strings.TrimSpace(aiReviewText(run)))
			}
		}

		if !slices.Equal(runs, []string{"go run ./ci/claudecodepin"}) {
			t.Errorf("%s: the steps run %q, want exactly go run ./ci/claudecodepin", claudeCodePinWorkflowPath, runs)
		}
	})
}
