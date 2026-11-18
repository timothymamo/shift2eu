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

// aiReviewWorkflowPath is the AI reviewer's workflow (PRD #161 milestone 4,
// ADR 0033).
const aiReviewWorkflowPath = ".github/workflows/ai-review.yml"

// aiReviewPinnedAction matches a uses: that names an action pinned to a commit:
// the 40-hex-digit SHA every action of this repository's workflows is pinned with
// (CLAUDE.md rule 7). It reads the workflow with test/internal/yamlread, the
// strict line reader the other tests of this package read YAML with; values that
// hold a ${{ }} expression are read as written, since that reader takes no
// expression.
var aiReviewPinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*@[0-9a-f]{40}$`)

// aiReviewWorkflow is the parsed workflow the subtests of
// TestAIReviewWorkflowHoldsTheReviewersSecurityProperties read: its source, its
// content lines, its one job and that job's steps.
type aiReviewWorkflow struct {
	src   []byte
	lines []yamlread.Line
	job   yamlread.Entry
	steps []yamlread.Item
}

// readAIReviewWorkflow reads and parses the AI reviewer's workflow.
func readAIReviewWorkflow(t *testing.T) aiReviewWorkflow {
	t.Helper()

	root := moduleRoot(t)

	src, err := os.ReadFile(filepath.Join(root, aiReviewWorkflowPath))
	if err != nil {
		t.Fatalf("read %s: %v; ADR 0033 pins the workflow's trigger, permissions, environment, secrets and pins", aiReviewWorkflowPath, err)
	}

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		t.Fatalf("%s: read the workflow: %v", aiReviewWorkflowPath, err)
	}

	jobs, ok := aiReviewEntry(t, lines, "jobs")
	if !ok {
		t.Fatalf("%s: the workflow has no jobs:", aiReviewWorkflowPath)
	}

	jobsFields, err := yamlread.Mapping(jobs.Block)
	if err != nil {
		t.Fatalf("%s: read jobs:: %v", aiReviewWorkflowPath, err)
	}

	if len(jobsFields) != 1 {
		t.Fatalf("%s: jobs: holds %d jobs, want the one that reviews the pull request", aiReviewWorkflowPath, len(jobsFields))
	}

	job := jobsFields[0]

	steps, ok := aiReviewEntry(t, job.Block, "steps")
	if !ok {
		t.Fatalf("%s: job %s has no steps:", aiReviewWorkflowPath, job.Key)
	}

	items, err := yamlread.Sequence(steps.Block)
	if err != nil {
		t.Fatalf("%s: job %s: read steps:: %v", aiReviewWorkflowPath, job.Key, err)
	}

	if len(items) == 0 {
		t.Fatalf("%s: job %s: steps: holds no step", aiReviewWorkflowPath, job.Key)
	}

	return aiReviewWorkflow{src: src, lines: lines, job: job, steps: items}
}

// TestAIReviewWorkflowHoldsTheReviewersSecurityProperties pins the shape of
// .github/workflows/ai-review.yml, which holds two secrets and reads a pull
// request anyone with push access wrote (ADR 0033): main's copy of the workflow
// runs on pull_request_target for the pull request's events only, with a token
// that reads and an App token that writes, in an environment limited to main,
// skipping drafts, cancelling the run in progress, never checking out the pull
// request's code, with every action pinned, and with no pull request data or
// secret interpolated into a script.
func TestAIReviewWorkflowHoldsTheReviewersSecurityProperties(t *testing.T) {
	t.Parallel()

	workflow := readAIReviewWorkflow(t)

	t.Run("pull_request_target on main is the only trigger", func(t *testing.T) {
		t.Parallel()

		on, ok := aiReviewEntry(t, workflow.lines, "on")
		if !ok {
			t.Fatalf("%s: the workflow has no on: trigger", aiReviewWorkflowPath)
		}

		if on.Value != "" {
			t.Fatalf("%s: line %d: on: %s, want a block of triggers: a workflow that runs the pull request's copy runs the pull request's code", aiReviewWorkflowPath, on.Num, on.Value)
		}

		triggers, err := yamlread.Mapping(on.Block)
		if err != nil {
			t.Fatalf("%s: read on:: %v", aiReviewWorkflowPath, err)
		}

		if len(triggers) != 1 || triggers[0].Key != "pull_request_target" {
			keys := make([]string, 0, len(triggers))
			for _, trigger := range triggers {
				keys = append(keys, trigger.Key)
			}

			t.Fatalf("%s: on: holds %v, want exactly pull_request_target: only it runs main's copy of the workflow and can read the ai-review environment's secrets (ADR 0033)", aiReviewWorkflowPath, keys)
		}

		fields, err := yamlread.Mapping(triggers[0].Block)
		if err != nil {
			t.Fatalf("%s: read on: pull_request_target:: %v", aiReviewWorkflowPath, err)
		}

		var types, branches []string

		var sawTypes, sawBranches bool

		for _, field := range fields {
			switch field.Key {
			case "types":
				sawTypes = true

				types, err = yamlread.List(field)
				if err != nil {
					t.Fatalf("%s: read the trigger's types:: %v", aiReviewWorkflowPath, err)
				}
			case "branches":
				sawBranches = true

				branches, err = yamlread.List(field)
				if err != nil {
					t.Fatalf("%s: read the trigger's branches:: %v", aiReviewWorkflowPath, err)
				}
			default:
				t.Errorf("%s: line %d: pull_request_target: has %s:, want types: and branches: only: every pull request into main is reviewed", aiReviewWorkflowPath, field.Num, field.Key)
			}
		}

		if !sawTypes {
			t.Errorf("%s: pull_request_target: has no types:, want opened, reopened, ready_for_review and synchronize", aiReviewWorkflowPath)
		} else {
			want := []string{"opened", "ready_for_review", "reopened", "synchronize"}

			got := slices.Clone(types)
			slices.Sort(got)

			if !slices.Equal(want, got) {
				t.Errorf("%s: pull_request_target: types are %v, want %v: the reviewer reviews a pull request when it is opened, reopened, marked ready and pushed to (ADR 0033)", aiReviewWorkflowPath, types, want)
			}
		}

		if !sawBranches {
			t.Errorf("%s: pull_request_target: has no branches:, want branches: [main]", aiReviewWorkflowPath)
		} else if !slices.Equal([]string{"main"}, branches) {
			t.Errorf("%s: pull_request_target: branches are %v, want [main]", aiReviewWorkflowPath, branches)
		}
	})

	t.Run("GITHUB_TOKEN only reads", func(t *testing.T) {
		t.Parallel()

		// Both calls fail on a scope but contents and pull-requests and on a
		// value but read; the job alone needs both, the workflow level may
		// grant nothing.
		aiReviewPermissions(t, workflow.lines, "the workflow")

		jobScopes := aiReviewPermissions(t, workflow.job.Block, "job "+workflow.job.Key)

		for _, scope := range []string{"contents", "pull-requests"} {
			if jobScopes[scope] != "read" {
				t.Errorf("%s: job %s: permissions: does not set %s: read; the job token reads the repository and the pull request, and the App's token writes the review (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key, scope)
			}
		}
	})

	t.Run("the job runs in the ai-review environment", func(t *testing.T) {
		t.Parallel()

		environment, ok := aiReviewEntry(t, workflow.job.Block, "environment")
		if !ok {
			t.Fatalf("%s: job %s has no environment:, so it holds no secret of the ai-review environment (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key)
		}

		name := aiReviewScalar(t, environment)
		if name == "" {
			named, ok := aiReviewEntry(t, environment.Block, "name")
			if !ok {
				t.Fatalf("%s: job %s: environment: names no environment", aiReviewWorkflowPath, workflow.job.Key)
			}

			name = aiReviewScalar(t, named)
		}

		if name != "ai-review" {
			t.Errorf("%s: job %s runs in the environment %q, want ai-review: that environment is limited to main and holds the reviewer App's client ID and key, the model key and the switch (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key, name)
		}
	})

	t.Run("a draft pull request starts no review", func(t *testing.T) {
		t.Parallel()

		guard, ok := aiReviewEntry(t, workflow.job.Block, "if")
		if !ok {
			t.Fatalf("%s: job %s has no if:, so a draft pull request starts a review (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key)
		}

		condition := aiReviewScalar(t, guard)

		if !strings.Contains(condition, "draft") {
			t.Errorf("%s: job %s: if: %q does not skip drafts, so a draft pull request starts a review; marking it ready starts one (ADR 0033)", aiReviewWorkflowPath, workflow.job.Key, condition)
		}
	})

	t.Run("a new push cancels the review in progress", func(t *testing.T) {
		t.Parallel()

		concurrency, ok := aiReviewEntry(t, workflow.lines, "concurrency")
		if !ok {
			t.Fatalf("%s: the workflow has no concurrency:, so a new push reviews beside the run in progress and both spend the model budget (ADR 0033)", aiReviewWorkflowPath)
		}

		if concurrency.Value != "" {
			t.Fatalf("%s: line %d: concurrency: %s, want a block with group: and cancel-in-progress:", aiReviewWorkflowPath, concurrency.Num, concurrency.Value)
		}

		fields, err := yamlread.Mapping(concurrency.Block)
		if err != nil {
			t.Fatalf("%s: read concurrency:: %v", aiReviewWorkflowPath, err)
		}

		var group, cancel string

		for _, field := range fields {
			switch field.Key {
			case "group":
				group = aiReviewScalar(t, field)
			case "cancel-in-progress":
				cancel = aiReviewScalar(t, field)
			default:
				t.Errorf("%s: line %d: concurrency: has %s:, want group: and cancel-in-progress: only", aiReviewWorkflowPath, field.Num, field.Key)
			}
		}

		if !strings.Contains(group, "number") {
			t.Errorf("%s: concurrency: group: %q is not keyed on the pull request's number, so two pull requests cancel each other (ADR 0033)", aiReviewWorkflowPath, group)
		}

		if cancel != "true" {
			t.Errorf("%s: concurrency: cancel-in-progress: %q, want true: a new push cancels the review in progress (ADR 0033)", aiReviewWorkflowPath, cancel)
		}
	})

	t.Run("no step checks out the pull request", func(t *testing.T) {
		t.Parallel()

		checkouts := 0

		for _, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			uses, ok := aiReviewUses(t, fields)
			if !ok || !strings.HasPrefix(uses, "actions/checkout@") {
				continue
			}

			checkouts++

			with, ok := aiReviewFind(fields, "with")
			if !ok {
				continue
			}

			withFields, err := yamlread.Mapping(with.Block)
			if err != nil {
				t.Fatalf("%s: line %d: read the checkout's with:: %v", aiReviewWorkflowPath, with.Num, err)
			}

			for _, field := range withFields {
				if field.Key == "ref" || field.Key == "repository" {
					t.Errorf("%s: line %d: the checkout sets %s: %s; the workflow checks out main only, for the program and its instructions, never the pull request's code (ADR 0033)", aiReviewWorkflowPath, field.Num, field.Key, aiReviewScalar(t, field))
				}
			}
		}

		if checkouts == 0 {
			t.Errorf("%s: no step checks out main with actions/checkout, so the reviewer's program and its instructions are not there", aiReviewWorkflowPath)
		}
	})

	t.Run("every action is pinned to a commit", func(t *testing.T) {
		t.Parallel()

		pins := 0

		for _, step := range workflow.steps {
			action, ok := aiReviewUses(t, aiReviewStepFields(t, step))
			if !ok {
				continue
			}

			pins++

			if !aiReviewPinnedAction.MatchString(action) {
				t.Errorf("%s: line %d: uses: %s is not pinned to a 40-hex-digit commit; every action is pinned by SHA (CLAUDE.md rule 7, ADR 0033)", aiReviewWorkflowPath, step.Num, action)
			}
		}

		if pins == 0 {
			t.Errorf("%s: no step uses an action, so no step mints the reviewer App's token", aiReviewWorkflowPath)
		}
	})

	t.Run("the secrets and the switch are read, and no script holds pull request data", func(t *testing.T) {
		t.Parallel()

		doc := string(workflow.src)

		for _, want := range []string{
			"secrets.AI_REVIEWER_APP_CLIENT_ID",
			"secrets.AI_REVIEWER_APP_PRIVATE_KEY",
			"secrets.ANTHROPIC_API_KEY",
			"vars.AI_REVIEW_REQUEST_CHANGES",
		} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s: nothing reads %s; the ai-review environment holds the reviewer App's client ID and key, the model key and the switch, and no repository-level secret holds them (ADR 0033)", aiReviewWorkflowPath, want)
			}
		}

		for _, step := range workflow.steps {
			fields := aiReviewStepFields(t, step)

			run, ok := aiReviewFind(fields, "run")
			if !ok {
				continue
			}

			script := run.Value
			for _, line := range run.Block {
				script += "\n" + line.Text
			}

			for _, refused := range []string{
				"github.event.pull_request.title",
				"github.event.pull_request.body",
				"${{ secrets.",
			} {
				if strings.Contains(script, refused) {
					t.Errorf("%s: line %d: the run: script holds %s; pull request data and secrets reach a script through env: only, never through an expression (ADR 0033)", aiReviewWorkflowPath, run.Num, refused)
				}
			}
		}
	})
}

// aiReviewEntry returns the entry of the mapping lines for key, and false when
// the mapping holds none.
func aiReviewEntry(t *testing.T, lines []yamlread.Line, key string) (yamlread.Entry, bool) {
	t.Helper()

	return aiReviewFind(aiReviewFields(t, lines, key), key)
}

// aiReviewFields returns the entries of the block mapping lines.
func aiReviewFields(t *testing.T, lines []yamlread.Line, where string) []yamlread.Entry {
	t.Helper()

	fields, err := yamlread.Mapping(lines)
	if err != nil {
		t.Fatalf("%s: read the mapping that holds %s: %v", aiReviewWorkflowPath, where, err)
	}

	return fields
}

// aiReviewFind returns the entry of fields for key, and false when fields holds
// none.
func aiReviewFind(fields []yamlread.Entry, key string) (yamlread.Entry, bool) {
	for _, field := range fields {
		if field.Key == key {
			return field, true
		}
	}

	return yamlread.Entry{}, false
}

// aiReviewStepFields returns the keys of one step.
func aiReviewStepFields(t *testing.T, step yamlread.Item) []yamlread.Entry {
	t.Helper()

	return aiReviewFields(t, step.Lines, "a step's keys")
}

// aiReviewUses returns the action one step uses, as written, and false when the
// step uses none. The value is read as a scalar, so a version comment after the
// pin (" # v7.0.1") is no part of it.
func aiReviewUses(t *testing.T, fields []yamlread.Entry) (string, bool) {
	t.Helper()

	uses, ok := aiReviewFind(fields, "uses")
	if !ok {
		return "", false
	}

	return aiReviewScalar(t, uses), true
}

// aiReviewScalar reads the value of e as one scalar, with a trailing comment
// removed.
func aiReviewScalar(t *testing.T, e yamlread.Entry) string {
	t.Helper()

	value, err := yamlread.Single(e)
	if err != nil {
		t.Fatalf("%s: line %d: read %s:: %v", aiReviewWorkflowPath, e.Num, e.Key, err)
	}

	return value
}

// aiReviewPermissions returns the permissions the mapping lines declare at
// where, and fails on a scope but contents and pull-requests and on a value but
// read, so no step of the workflow can write with GITHUB_TOKEN.
func aiReviewPermissions(t *testing.T, lines []yamlread.Line, where string) map[string]string {
	t.Helper()

	entry, ok := aiReviewEntry(t, lines, "permissions")
	if !ok {
		t.Errorf("%s: %s declares no permissions:, so it takes the repository's default token permissions", aiReviewWorkflowPath, where)

		return nil
	}

	scopes := map[string]string{}

	switch {
	case entry.Value == "{}":
		// Declared and empty: the workflow grants nothing.
		return scopes
	case entry.Value != "":
		t.Errorf("%s: line %d: %s: permissions: %q is not a block of scope: value pairs", aiReviewWorkflowPath, entry.Num, where, entry.Value)

		return scopes
	}

	for _, scope := range aiReviewFields(t, entry.Block, where+": permissions:") {
		if scope.Key != "contents" && scope.Key != "pull-requests" {
			t.Errorf("%s: line %d: %s: permissions: grants %s: %s; the job token reads the repository and the pull request, and nothing else (ADR 0033)", aiReviewWorkflowPath, scope.Num, where, scope.Key, scope.Value)

			continue
		}

		if scope.Value != "read" {
			t.Errorf("%s: line %d: %s: permissions: sets %s: %s, want read: the review is written with the App's token, never with GITHUB_TOKEN (ADR 0033)", aiReviewWorkflowPath, scope.Num, where, scope.Key, aiReviewScalar(t, scope))
		}

		scopes[scope.Key] = aiReviewScalar(t, scope)
	}

	return scopes
}
