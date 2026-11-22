// Pins the shape of .github/workflows/audit-paths.yml, the workflow of PRD #175 milestone 2's required path
// check: the check that fails a pull request authored by shift2eu-audit[bot] that touches anything outside
// docs/security/, and passes trivially for every other author.
//
// The check is required through a ruleset of its own on main (the PRD's runbook step 5), never through
// Protect main, and the maintainer names the job in that ruleset, so the job's name is part of the
// interface: "Audit report paths". A required check that never reports blocks the pull request it is
// required on, so the workflow runs on every pull request into main and filters none of them off.
//
// The workflow runs the pull request's own copy of the check, whose script this repository reviews: it
// holds no secret, reads with contents: read alone, and pins every action by commit. Neither the author nor
// either commit reaches a script through a ${{ }} expression spliced into it; they arrive through env:,
// so no value from the event can become a shell word.
package lint_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

const (
	// auditPathsWorkflowPath is the workflow of the required path check (PRD #175 milestone 2).
	auditPathsWorkflowPath = ".github/workflows/audit-paths.yml"

	// auditPathsWorkflowName is the workflow's name, auditPathsJobID the id of its one job and
	// auditPathsJobName that job's name: the name the maintainer puts in the audit-paths ruleset (the
	// PRD's runbook step 5).
	auditPathsWorkflowName = "Audit paths"
	auditPathsJobID        = "audit-paths"
	auditPathsJobName      = "Audit report paths"

	// auditPathsScript is the check the job runs: the fence itself.
	auditPathsScript = ".github/scripts/audit-paths.sh"

	// auditPathsAuthorExpr is where the check's author comes from: the pull request's author login, which is
	// the App's login for a report pull request. It is read in env: alone, never in a run: line.
	auditPathsAuthorExpr = "${{ github.event.pull_request.user.login }}"

	// The variables the check reads, which the step running it sets in env:.
	auditPathsAuthorEnv = "AUDIT_PATHS_AUTHOR"
	auditPathsBaseEnv   = "AUDIT_PATHS_BASE"
	auditPathsHeadEnv   = "AUDIT_PATHS_HEAD"
)

// auditPathsWorkflow is the parsed workflow the subtests read: its source, its content lines, its one job
// and that job's steps.
type auditPathsWorkflow struct {
	src   []byte
	lines []yamlread.Line
	job   yamlread.Entry
	steps []yamlread.Item
}

// readAuditPathsWorkflow reads and parses the path check's workflow.
func readAuditPathsWorkflow(t *testing.T) auditPathsWorkflow {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(moduleRoot(t), auditPathsWorkflowPath))
	if err != nil {
		t.Fatalf("read %s: %v; PRD #175 milestone 2 puts the required path check there", auditPathsWorkflowPath, err)
	}

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		t.Fatalf("%s: read the workflow: %v", auditPathsWorkflowPath, err)
	}

	jobs, ok := auditPathsEntry(t, lines, "jobs")
	if !ok {
		t.Fatalf("%s: the workflow has no jobs:", auditPathsWorkflowPath)
	}

	fields, err := yamlread.Mapping(jobs.Block)
	if err != nil {
		t.Fatalf("%s: read jobs:: %v", auditPathsWorkflowPath, err)
	}

	if len(fields) != 1 {
		t.Fatalf("%s: jobs: holds %d jobs, want the one that reports the path check", auditPathsWorkflowPath, len(fields))
	}

	job := fields[0]

	steps, ok := auditPathsEntry(t, job.Block, "steps")
	if !ok {
		t.Fatalf("%s: job %s has no steps:", auditPathsWorkflowPath, job.Key)
	}

	items, err := yamlread.Sequence(steps.Block)
	if err != nil {
		t.Fatalf("%s: job %s: read steps:: %v", auditPathsWorkflowPath, job.Key, err)
	}

	if len(items) == 0 {
		t.Fatalf("%s: job %s: steps: holds no step", auditPathsWorkflowPath, job.Key)
	}

	return auditPathsWorkflow{src: src, lines: lines, job: job, steps: items}
}

// TestAuditPathsWorkflowIsAClosedFence pins the shape of .github/workflows/audit-paths.yml: it is named
// for the check, runs on every pull request and no other event, reads with contents: read alone, reports
// under the name the ruleset requires, pins every action by commit, leaves no credential in .git/config,
// splices no expression into a script, and hands the check its author and its two commits through env:.
func TestAuditPathsWorkflowIsAClosedFence(t *testing.T) {
	t.Parallel()

	workflow := readAuditPathsWorkflow(t)

	t.Run("the workflow is named for the check", func(t *testing.T) {
		t.Parallel()

		name, ok := auditPathsEntry(t, workflow.lines, "name")
		if !ok {
			t.Fatalf("%s: the workflow has no name:, want %q", auditPathsWorkflowPath, auditPathsWorkflowName)
		}

		if got := auditPathsScalar(t, name); got != auditPathsWorkflowName {
			t.Errorf("%s: line %d: name is %q, want %q", auditPathsWorkflowPath, name.Num, got, auditPathsWorkflowName)
		}
	})

	t.Run("pull_request is the only trigger and no filter takes a pull request off it", func(t *testing.T) {
		t.Parallel()

		on, ok := auditPathsEntry(t, workflow.lines, "on")
		if !ok {
			t.Fatalf("%s: the workflow has no on: trigger", auditPathsWorkflowPath)
		}

		if on.Value != "" {
			t.Fatalf("%s: line %d: on: %s, want a block of triggers, pull_request among them", auditPathsWorkflowPath, on.Num, on.Value)
		}

		triggers, err := yamlread.Mapping(on.Block)
		if err != nil {
			t.Fatalf("%s: read on:: %v", auditPathsWorkflowPath, err)
		}

		names := make([]string, 0, len(triggers))
		for _, trigger := range triggers {
			names = append(names, trigger.Key)
		}

		if len(triggers) != 1 || triggers[0].Key != "pull_request" {
			// pull_request_target, workflow_run and issue_comment run a copy of the workflow another ref
			// holds; only pull_request reports on the pull request's own merge, which is what a required
			// check reads.
			t.Fatalf("%s: on: holds %v, want pull_request alone: this check is a required check of pull requests", auditPathsWorkflowPath, names)
		}

		pullRequest := triggers[0]

		if pullRequest.Value != "" && pullRequest.Value != "{}" {
			t.Errorf("%s: line %d: pull_request: %s, want a block mapping or nothing", auditPathsWorkflowPath, pullRequest.Num, pullRequest.Value)
		}

		for _, field := range auditPathsFields(t, pullRequest.Block, "on: pull_request:") {
			switch field.Key {
			case "paths", "paths-ignore", "branches-ignore":
				// A required check that the filter leaves out never reports, and a pull request waiting
				// for it is blocked for good.
				t.Errorf("%s: line %d: pull_request: filters on %s:, want no filter: the check must report on every pull request into main", auditPathsWorkflowPath, field.Num, field.Key)
			case "branches", "types":
				// Narrowing the branches or the activity types keeps the check reporting on the pull
				// requests that matter, and the ruleset decides which ones it is required on.
			default:
				t.Errorf("%s: line %d: pull_request: has %s:, want branches: or types: at most: no other filter may keep the check from reporting", auditPathsWorkflowPath, field.Num, field.Key)
			}
		}
	})

	t.Run("the token reads contents only", func(t *testing.T) {
		t.Parallel()

		permissions, ok := auditPathsEntry(t, workflow.lines, "permissions")
		if !ok {
			t.Fatalf("%s: the workflow declares no top-level permissions:, want contents: read alone", auditPathsWorkflowPath)
		}

		if permissions.Value != "" {
			t.Fatalf("%s: line %d: permissions: %s, want a block of scope: value pairs", auditPathsWorkflowPath, permissions.Num, permissions.Value)
		}

		scopes := auditPathsFields(t, permissions.Block, "permissions:")
		if len(scopes) != 1 || scopes[0].Key != "contents" {
			keys := make([]string, 0, len(scopes))
			for _, scope := range scopes {
				keys = append(keys, scope.Key)
			}

			t.Errorf("%s: top-level permissions: grants %v, want contents: read alone: the check reads the pull request's two commits and writes nothing", auditPathsWorkflowPath, keys)
		}

		for _, scope := range scopes {
			if got := auditPathsScalar(t, scope); got != "read" {
				t.Errorf("%s: line %d: permissions: sets %s: %s, want read", auditPathsWorkflowPath, scope.Num, scope.Key, got)
			}
		}

		if job, ok := auditPathsEntry(t, workflow.job.Block, "permissions"); ok {
			for _, scope := range auditPathsFields(t, job.Block, "the job's permissions:") {
				if got := auditPathsScalar(t, scope); got != "read" {
					t.Errorf("%s: line %d: the job's permissions: sets %s: %s, want read or no job permissions: at all", auditPathsWorkflowPath, scope.Num, scope.Key, got)
				}
			}
		}
	})

	t.Run("one job reports under the required name", func(t *testing.T) {
		t.Parallel()

		if got := workflow.job.Key; got != auditPathsJobID {
			t.Errorf("%s: the one job's id is %q, want %q", auditPathsWorkflowPath, got, auditPathsJobID)
		}

		name, ok := auditPathsEntry(t, workflow.job.Block, "name")
		if !ok {
			t.Fatalf("%s: job %s has no name:, want %q: the ruleset requires the check by this name", auditPathsWorkflowPath, auditPathsJobID, auditPathsJobName)
		}

		if got := auditPathsScalar(t, name); got != auditPathsJobName {
			t.Errorf("%s: line %d: job %s is named %q, want %q: the audit-paths ruleset requires that name", auditPathsWorkflowPath, name.Num, auditPathsJobID, got, auditPathsJobName)
		}

		timeout, ok := auditPathsEntry(t, workflow.job.Block, "timeout-minutes")
		if !ok {
			t.Fatalf("%s: job %s has no timeout-minutes:, want a job that cannot hold a runner for hours", auditPathsWorkflowPath, auditPathsJobID)
		}

		minutes, err := strconv.Atoi(auditPathsScalar(t, timeout))
		if err != nil || minutes < 1 {
			t.Errorf("%s: line %d: job %s: timeout-minutes is %q, want a positive number of minutes", auditPathsWorkflowPath, timeout.Num, auditPathsJobID, timeout.Value)
		}
	})

	t.Run("every action is pinned by commit and the checkout keeps no credential", func(t *testing.T) {
		t.Parallel()

		uses := 0

		for _, line := range workflow.lines {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line.Text), "-"))

			action, ok := strings.CutPrefix(text, "uses:")
			if !ok {
				continue
			}

			uses++

			action = auditPathsWithoutComment(strings.TrimSpace(action))
			if !aiReviewPinnedAction.MatchString(action) {
				t.Errorf("%s: line %d: uses: %s, want an action pinned to a 40-hex commit SHA: a tag or a branch is a moving target (CLAUDE.md rule 7)", auditPathsWorkflowPath, line.Num, action)
			}
		}

		if uses == 0 {
			t.Fatalf("%s: the workflow uses no action, so this test checked no pin: the check needs a checkout", auditPathsWorkflowPath)
		}

		checkout, ok := auditPathsFind(workflow.steps, "uses", "actions/checkout@")
		if !ok {
			t.Fatalf("%s: no step checks the repository out, so the check has no commits to compare", auditPathsWorkflowPath)
		}

		with, ok := auditPathsEntry(t, checkout.Lines, "with")
		if !ok {
			t.Fatalf("%s: the checkout has no with:, want persist-credentials: false", auditPathsWorkflowPath)
		}

		persist, ok := auditPathsEntry(t, with.Block, "persist-credentials")
		if !ok {
			t.Fatalf("%s: the checkout has no persist-credentials:, so the job's token stays in .git/config for every step the pull request's tree runs", auditPathsWorkflowPath)
		}

		if got := auditPathsScalar(t, persist); got != "false" {
			t.Errorf("%s: line %d: the checkout sets persist-credentials: %s, want false: no step after it may read the token out of .git/config", auditPathsWorkflowPath, persist.Num, got)
		}
	})

	t.Run("no run line holds an expression", func(t *testing.T) {
		t.Parallel()

		for _, step := range workflow.steps {
			for _, line := range auditPathsRunLines(t, step) {
				if strings.Contains(line, "${{") {
					// A value spliced into a script is a shell word: the author of a pull request anyone
					// can open would decide how the check's commands are read.
					t.Errorf("%s: line %d: the run: script holds %q, want the step's env: to carry every value: an expression in a script is a shell word of the event's author", auditPathsWorkflowPath, step.Num, line)
				}
			}
		}
	})

	t.Run("the check's step takes the author and the two commits from env", func(t *testing.T) {
		t.Parallel()

		step, ok := auditPathsRunStep(t, workflow)
		if !ok {
			t.Fatalf("%s: no step runs %s, so the job reports nothing of the check", auditPathsWorkflowPath, auditPathsScript)
		}

		env, ok := auditPathsEntry(t, step.Lines, "env")
		if !ok {
			t.Fatalf("%s: the step that runs %s has no env:, want %s, %s and %s", auditPathsWorkflowPath, auditPathsScript, auditPathsAuthorEnv, auditPathsBaseEnv, auditPathsHeadEnv)
		}

		author, ok := auditPathsEnvValue(env, auditPathsAuthorEnv)
		if !ok {
			t.Errorf("%s: the check's step sets no %s, so the check cannot tell whose pull request it reads", auditPathsWorkflowPath, auditPathsAuthorEnv)
		} else if author != auditPathsAuthorExpr {
			t.Errorf("%s: line %d: %s is %q, want %q: the check applies to the pull request's author alone", auditPathsWorkflowPath, env.Num, auditPathsAuthorEnv, author, auditPathsAuthorExpr)
		}

		for _, name := range []string{auditPathsBaseEnv, auditPathsHeadEnv} {
			if value, ok := auditPathsEnvValue(env, name); !ok || value == "" {
				t.Errorf("%s: the check's step sets no %s, so the check has no two commits to compare", auditPathsWorkflowPath, name)
			}
		}
	})
}

// auditPathsEntry returns the entry of lines for key, and false when lines holds none.
func auditPathsEntry(t *testing.T, lines []yamlread.Line, key string) (yamlread.Entry, bool) {
	t.Helper()

	fields := auditPathsFields(t, lines, key)

	for _, field := range fields {
		if field.Key == key {
			return field, true
		}
	}

	return yamlread.Entry{}, false
}

// auditPathsFields returns the entries of the block mapping lines.
func auditPathsFields(t *testing.T, lines []yamlread.Line, where string) []yamlread.Entry {
	t.Helper()

	fields, err := yamlread.Mapping(lines)
	if err != nil {
		t.Fatalf("%s: read the mapping that holds %s: %v", auditPathsWorkflowPath, where, err)
	}

	return fields
}

// auditPathsRunStep returns the step that runs the path check, and false when no step does: the step
// whose script names .github/scripts/audit-paths.sh.
func auditPathsRunStep(t *testing.T, workflow auditPathsWorkflow) (yamlread.Item, bool) {
	t.Helper()

	for _, step := range workflow.steps {
		for _, line := range auditPathsRunLines(t, step) {
			if strings.Contains(line, auditPathsScript) {
				return step, true
			}
		}
	}

	return yamlread.Item{}, false
}

// auditPathsRunLines returns the lines of one step's run: script: the value on the key's own line, or the
// lines of its block scalar. A step that runs nothing has none.
func auditPathsRunLines(t *testing.T, step yamlread.Item) []string {
	t.Helper()

	run, ok := auditPathsEntry(t, step.Lines, "run")
	if !ok {
		return nil
	}

	if run.Value != "" && !aiReviewBlockIndicator(run.Value) {
		return []string{run.Value}
	}

	lines := make([]string, 0, len(run.Block))
	for _, line := range run.Block {
		lines = append(lines, line.Text)
	}

	return lines
}

// auditPathsFind returns the first item of items whose entry key holds want as a substring, and false when
// items holds none: the step that uses an action is found that way.
func auditPathsFind(items []yamlread.Item, key, want string) (yamlread.Item, bool) {
	for _, item := range items {
		for _, line := range item.Lines {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line.Text), "-"))

			value, ok := strings.CutPrefix(text, key+":")
			if ok && strings.Contains(value, want) {
				return item, true
			}
		}
	}

	return yamlread.Item{}, false
}

// auditPathsScalar reads the value of e as one scalar, with a trailing comment removed.
func auditPathsScalar(t *testing.T, e yamlread.Entry) string {
	t.Helper()

	value, err := yamlread.Single(e)
	if err != nil {
		t.Fatalf("%s: line %d: read %s:: %v", auditPathsWorkflowPath, e.Num, e.Key, err)
	}

	return value
}

// auditPathsWithoutComment returns value without a trailing comment, so the version comment after a pin is
// no part of the pin.
func auditPathsWithoutComment(value string) string {
	if i := strings.Index(value, " #"); i >= 0 {
		return strings.TrimSpace(value[:i])
	}

	return value
}

// auditPathsEnvValue returns the value env: sets for name, as written and without a trailing comment, and
// false when it sets none. A step's env: names its variables in upper case, which yamlread.Mapping refuses
// as keys, so this reads the block's lines as written.
func auditPathsEnvValue(env yamlread.Entry, name string) (string, bool) {
	for _, line := range env.Block {
		key, value, ok := strings.Cut(line.Text, ":")
		if !ok || key != name {
			continue
		}

		return auditPathsWithoutComment(strings.TrimSpace(value)), true
	}

	return "", false
}
