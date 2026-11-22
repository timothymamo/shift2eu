// Pins the shape of .github/workflows/audit-paths.yml, the workflow of PRD #175 milestone 2's required path
// check: the check that fails an audit pull request - one authored by shift2eu-audit[bot], one sent by that
// App, or one whose head branch is under security-audit/ - that touches anything outside docs/security/,
// and passes trivially for every other pull request.
//
// The check is required through a ruleset of its own on main (the PRD's runbook step 5), never through
// Protect main, and the maintainer names the job in that ruleset, so the job's name is part of the
// interface: "Audit report paths". A required check that never reports blocks the pull request it is
// required on, so the workflow runs on every pull request, into any branch, on pull_request_target alone
// with the types opened, synchronize, reopened and edited, and filters none of them off.
//
// It runs on pull_request_target, not on pull_request, and that trigger is the fence itself. pull_request
// runs the pull request's own copy of this workflow and of the script it calls: the audit App holds no
// Workflows permission, so it cannot change .github/workflows/, but it does hold contents write, so it can
// change .github/scripts/audit-paths.sh in its own pull request and make its own check pass. Under
// pull_request_target the workflow and the script are main's copies, the code this repository reviewed.
//
// So the job checks main out, with no ref: and no repository: and without persisting a credential, and it
// reaches the pull request's commits as git objects alone: one step before the check runs git fetch of the
// pull request's own ref, taking its number from env:, and no run: step ever checks those commits out,
// switches to them, works on them or resets to them. The check then diffs merge-base(base, head)..head and
// never executes a line of the pull request's tree. The repository is public, so that fetch needs no
// credential: the job holds contents: read, no secret and no environment.
//
// Neither the author nor either commit reaches a script through a ${{ }} expression spliced into it; they
// arrive through env:, so no value from the event can become a shell word.
//
// The check reads more of the event than the author. It reads the sender, because the App's contents write
// lets it push a commit onto another author's pull request; the head branch, because the App pushes its
// reports from security-audit/**; and the base branch, because an audit pull request into another branch
// that holds code could take the green check there and be retargeted to main afterwards. A retarget is an
// edited event, so the trigger's types: must hold opened, synchronize, reopened and edited, the activity
// types under which the fields the check reads can change. It filters no branch and no path: branches:
// would take a pull request into another branch off the trigger, and a required check that never reports
// blocks the pull request it is required on for good.
package lint_test

import (
	"os"
	"path/filepath"
	"slices"
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

	// auditPathsBaseExpr and auditPathsHeadExpr are the two commits the check compares: the base branch's
	// commit the pull request is based on, and the pull request's own head commit. Neither is github.sha,
	// which under pull_request_target is main's commit, the copy of the workflow and script the job runs: the
	// check would then diff main against main and pass every pull request.
	auditPathsBaseExpr = "${{ github.event.pull_request.base.sha }}"
	auditPathsHeadExpr = "${{ github.event.pull_request.head.sha }}"

	// auditPathsBaseRefExpr, auditPathsHeadRefExpr and auditPathsSenderExpr are what tells an audit pull
	// request from any other, and which branch one may land on: the base branch (which must be main), the
	// head branch (the App's are under security-audit/), and the sender (the account that pushed the head
	// commit, which the App can do on another author's pull request).
	auditPathsBaseRefExpr = "${{ github.event.pull_request.base.ref }}"
	auditPathsHeadRefExpr = "${{ github.event.pull_request.head.ref }}"
	auditPathsSenderExpr  = "${{ github.event.sender.login }}"

	// auditPathsPullRequestExprPrefix is what the env: of the step that fetches the pull request's commits
	// reads: the pull request's own number or head commit, never the base branch's.
	auditPathsPullRequestExprPrefix = "${{ github.event.pull_request."

	// The variables the check reads, which the step running it sets in env:.
	auditPathsAuthorEnv  = "AUDIT_PATHS_AUTHOR"
	auditPathsSenderEnv  = "AUDIT_PATHS_SENDER"
	auditPathsBaseEnv    = "AUDIT_PATHS_BASE"
	auditPathsHeadEnv    = "AUDIT_PATHS_HEAD"
	auditPathsBaseRefEnv = "AUDIT_PATHS_BASE_REF"
	auditPathsHeadRefEnv = "AUDIT_PATHS_HEAD_REF"
)

// auditPathsActivityTypes are the pull request activity types the trigger must hold, each with the change
// it reports: the ones under which a field the check reads can change.
var auditPathsActivityTypes = []struct {
	name string
	why  string
}{
	{"opened", "a pull request the App just opened is judged before it can land"},
	{"synchronize", "a push, the App's onto another author's pull request included, moves the head commit"},
	{"reopened", "a pull request reopened may have been changed while it was closed"},
	{"edited", "retargeting a pull request's base branch to main is an edited event, and the base branch is part of what the check decides"},
}

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
// for the check, runs on every pull request of the activity types that move what the check reads and no
// other event through pull_request_target alone, filters no branch or path, reads with contents: read
// alone, reports under the name the ruleset requires, pins every action by commit, checks main out with no
// ref: and no credential, fetches the pull request's commits without ever checking them out, splices no
// expression into a script, and hands the check its author, its sender, its two branches and its two
// commits through env:.
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

	t.Run("pull_request_target is the only trigger and no filter takes a pull request off it", func(t *testing.T) {
		t.Parallel()

		on, ok := auditPathsEntry(t, workflow.lines, "on")
		if !ok {
			t.Fatalf("%s: the workflow has no on: trigger", auditPathsWorkflowPath)
		}

		if on.Value != "" {
			t.Fatalf("%s: line %d: on: %s, want a block holding pull_request_target alone", auditPathsWorkflowPath, on.Num, on.Value)
		}

		triggers, err := yamlread.Mapping(on.Block)
		if err != nil {
			t.Fatalf("%s: read on:: %v", auditPathsWorkflowPath, err)
		}

		names := make([]string, 0, len(triggers))
		for _, trigger := range triggers {
			names = append(names, trigger.Key)
		}

		if len(triggers) != 1 || triggers[0].Key != "pull_request_target" {
			// pull_request runs the pull request's own copy of this workflow and of the script it calls:
			// the audit App cannot touch .github/workflows/, but it can change
			// .github/scripts/audit-paths.sh in its pull request and so make its own check pass.
			// pull_request_target runs main's copy of both. push and workflow_run report on no pull
			// request at all, and a required check that never reports blocks the pull request it is
			// required on.
			t.Fatalf("%s: on: holds %v, want pull_request_target alone: only it runs main's own copy of the check, and only it reports on every pull request", auditPathsWorkflowPath, names)
		}

		trigger := triggers[0]

		if trigger.Value != "" && trigger.Value != "{}" {
			t.Errorf("%s: line %d: pull_request_target: %s, want a block mapping or nothing", auditPathsWorkflowPath, trigger.Num, trigger.Value)
		}

		var types yamlread.Entry

		sawTypes := false

		for _, field := range auditPathsFields(t, trigger.Block, "on: pull_request_target:") {
			switch field.Key {
			case "paths", "paths-ignore", "branches-ignore", "branches":
				// A required check that the filter leaves out never reports, and a pull request waiting
				// for it is blocked for good. branches: filters on the base branch, which the check itself
				// decides: a pull request into any branch but main that it never sees is one that could
				// be retargeted to main after the check passed. The ruleset decides where the check is
				// required, and where it is, it reports.
				t.Errorf("%s: line %d: pull_request_target: filters on %s:, want no filter: the check must report on every pull request into any branch, or the ruleset that requires it blocks a pull request it never sees", auditPathsWorkflowPath, field.Num, field.Key)
			case "types":
				sawTypes, types = true, field
			default:
				t.Errorf("%s: line %d: pull_request_target: has %s:, want types: alone: no other filter may keep the check from reporting", auditPathsWorkflowPath, field.Num, field.Key)
			}
		}

		wantTypes := make([]string, 0, len(auditPathsActivityTypes))
		for _, activity := range auditPathsActivityTypes {
			wantTypes = append(wantTypes, activity.name)
		}

		if !sawTypes {
			t.Fatalf("%s: pull_request_target: has no types:, want a list holding %s: the check reads fields of the event that change under these activity types alone", auditPathsWorkflowPath, strings.Join(wantTypes, ", "))
		}

		got, err := yamlread.List(types)
		if err != nil {
			t.Fatalf("%s: read the trigger's types:: %v", auditPathsWorkflowPath, err)
		}

		for _, activity := range auditPathsActivityTypes {
			if !slices.Contains(got, activity.name) {
				t.Errorf("%s: line %d: pull_request_target: types %v do not hold %s: %s", auditPathsWorkflowPath, types.Num, got, activity.name, activity.why)
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

	t.Run("every action is pinned by commit and the checkout takes main and keeps no credential", func(t *testing.T) {
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

		checkout, checkouts := auditPathsCheckout(workflow.steps)
		if checkouts == 0 {
			t.Fatalf("%s: no step checks the repository out, so the job has no copy of %s to run", auditPathsWorkflowPath, auditPathsScript)
		}

		if checkouts > 1 {
			t.Errorf("%s: %d steps use actions/checkout, want one: the job checks main out once, and a second checkout is how a pull request's own tree could reach the run", auditPathsWorkflowPath, checkouts)
		}

		with, ok := auditPathsEntry(t, checkout.Lines, "with")
		if !ok {
			t.Fatalf("%s: the checkout has no with:, want persist-credentials: false", auditPathsWorkflowPath)
		}

		persist, ok := auditPathsEntry(t, with.Block, "persist-credentials")
		if !ok {
			t.Fatalf("%s: the checkout has no persist-credentials:, so the job's token stays in .git/config for every step after it", auditPathsWorkflowPath)
		}

		if got := auditPathsScalar(t, persist); got != "false" {
			t.Errorf("%s: line %d: the checkout sets persist-credentials: %s, want false: no step after it may read the token out of .git/config", auditPathsWorkflowPath, persist.Num, got)
		}

		depth, ok := auditPathsEntry(t, with.Block, "fetch-depth")
		if !ok {
			t.Fatalf("%s: the checkout has no fetch-depth:, want 0: a shallow checkout leaves the merge base of the two commits out and the check then fails closed on every audit pull request", auditPathsWorkflowPath)
		}

		if got := auditPathsScalar(t, depth); got != "0" {
			t.Errorf("%s: line %d: the checkout sets fetch-depth: %s, want 0: a shallow checkout leaves the merge base of the two commits out and the check then fails closed on every audit pull request", auditPathsWorkflowPath, depth.Num, got)
		}

		for _, field := range auditPathsFields(t, with.Block, "the checkout's with:") {
			switch field.Key {
			case "ref", "repository":
				// Under pull_request_target these two are how the pull request's own tree is checked
				// out in place of main's, and then the fetch below and every run: step would act on a
				// tree its author wrote.
				t.Errorf("%s: line %d: the checkout sets %s: %s, want neither: the job checks main out, whose copy of the workflow and the script runs, and reaches the pull request's commits as git objects alone", auditPathsWorkflowPath, field.Num, field.Key, auditPathsScalar(t, field))
			}
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

	t.Run("the pull request's commits are fetched, never checked out", func(t *testing.T) {
		t.Parallel()

		script, ok := auditPathsRunStep(t, workflow)
		if !ok {
			t.Fatalf("%s: no step runs %s, so the job reports nothing of the check", auditPathsWorkflowPath, auditPathsScript)
		}

		if _, ok := auditPathsFetch(t, workflow, script.Num); !ok {
			t.Fatalf("%s: no step before the one that runs %s runs git fetch of the pull request's ref, so the check has main's own history alone and passes every pull request", auditPathsWorkflowPath, auditPathsScript)
		}

		// The fetched commits are objects to diff and nothing else. A tree that is checked out, switched
		// to, worked on or reset to is a tree the pull request's author wrote, and the next run: step
		// executes it: pull_request_target runs main's copy of the workflow only while nothing in the job
		// moves the working tree off main.
		for _, step := range workflow.steps {
			for _, line := range auditPathsRunLines(t, step) {
				for _, command := range []string{"git checkout", "git switch", "git worktree", "git restore", "git reset --hard"} {
					if !strings.Contains(line, command) {
						continue
					}

					t.Errorf("%s: line %d: a run: step runs %q in %q, want no step to touch the working tree: the pull request's commits are git objects to diff, never a tree the job runs", auditPathsWorkflowPath, step.Num, command, strings.TrimSpace(line))
				}
			}
		}
	})

	t.Run("the check's step takes the author, the sender, the branches and the commits from env", func(t *testing.T) {
		t.Parallel()

		step, ok := auditPathsRunStep(t, workflow)
		if !ok {
			t.Fatalf("%s: no step runs %s, so the job reports nothing of the check", auditPathsWorkflowPath, auditPathsScript)
		}

		env, ok := auditPathsEntry(t, step.Lines, "env")
		if !ok {
			t.Fatalf("%s: the step that runs %s has no env:, want %s, %s, %s, %s, %s and %s", auditPathsWorkflowPath, auditPathsScript, auditPathsAuthorEnv, auditPathsSenderEnv, auditPathsBaseEnv, auditPathsHeadEnv, auditPathsBaseRefEnv, auditPathsHeadRefEnv)
		}

		for _, want := range []struct {
			name string
			expr string
			why  string
		}{
			{
				auditPathsAuthorEnv,
				auditPathsAuthorExpr,
				"the check applies to the pull request's author alone, and on a report pull request that author is the App",
			},
			{
				auditPathsSenderEnv,
				auditPathsSenderExpr,
				"the App can push a commit onto another author's pull request, where the author is no App at all, and the sender is who pushed",
			},
			{
				auditPathsBaseEnv,
				auditPathsBaseExpr,
				"the branch the pull request is based on is the commit the check diffs from",
			},
			{
				auditPathsHeadEnv,
				auditPathsHeadExpr,
				"the pull request's own head is the commit that would land, where github.sha is main's commit under pull_request_target",
			},
			{
				auditPathsBaseRefEnv,
				auditPathsBaseRefExpr,
				"an audit pull request lands on main alone, and the base commit below is no branch name: a pull request into another branch that holds code would otherwise take the green check there and be retargeted to main",
			},
			{
				auditPathsHeadRefEnv,
				auditPathsHeadRefExpr,
				"the App pushes its reports from security-audit/** branches, so the head branch says that a pull request another account opened is its own",
			},
		} {
			value, ok := auditPathsEnvValue(env, want.name)
			if !ok {
				t.Errorf("%s: the check's step sets no %s, so %s", auditPathsWorkflowPath, want.name, want.why)

				continue
			}

			if value != want.expr {
				t.Errorf("%s: line %d: %s is %q, want %q: %s", auditPathsWorkflowPath, env.Num, want.name, value, want.expr, want.why)
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

// auditPathsCheckout returns the step of steps that uses actions/checkout, and how many steps do: the job
// checks main out once, and every further checkout is a tree some other ref wrote.
func auditPathsCheckout(steps []yamlread.Item) (yamlread.Item, int) {
	var (
		found yamlread.Item
		count int
	)

	for _, step := range steps {
		for _, line := range step.Lines {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line.Text), "-"))

			uses, ok := strings.CutPrefix(text, "uses:")
			if ok && strings.Contains(uses, "actions/checkout@") {
				found, count = step, count+1

				break
			}
		}
	}

	return found, count
}

// auditPathsFetch returns the step of workflow that runs before line and fetches the pull request's commits:
// one whose run: runs git fetch of a refs/pull/ ref, on a line that itself names the variable the step's env:
// fills from the pull request's number or head commit, so a fetch of another ref - the base branch's, which
// would leave the check diffing main against itself and passing every pull request - is no fetch at all. It
// reports on a fetch that is there but takes its ref any other way, and false when no step runs git fetch.
func auditPathsFetch(t *testing.T, workflow auditPathsWorkflow, before int) (yamlread.Item, bool) {
	t.Helper()

	for _, step := range workflow.steps {
		if step.Num >= before {
			break
		}

		run := auditPathsRunLines(t, step)
		if !auditPathsRuns(run, "git fetch") {
			continue
		}

		env, ok := auditPathsEntry(t, step.Lines, "env")
		if !ok {
			t.Errorf("%s: line %d: the step that runs git fetch has no env:, want the pull request's number or head commit there: an expression in run: is a shell word of the event's author", auditPathsWorkflowPath, step.Num)

			continue
		}

		for _, variable := range auditPathsEnvVars(env) {
			if !strings.HasPrefix(variable.value, auditPathsPullRequestExprPrefix) {
				continue
			}

			if !strings.Contains(variable.value, ".number") && !strings.Contains(variable.value, ".head.sha") {
				continue
			}

			if auditPathsFetchLine(run, variable.name) {
				return step, true
			}
		}

		t.Errorf("%s: line %d: the step that runs git fetch fetches no refs/pull/ ref from a variable its env: fills from %s: the fetch must name the pull request's own number or head commit, or it brings main's commits in place of the pull request's", auditPathsWorkflowPath, step.Num, auditPathsPullRequestExprPrefix)
	}

	return yamlread.Item{}, false
}

// auditPathsFetchLine reports whether one line of a step's run: script fetches the pull request's own ref: the
// line runs git fetch, names a refs/pull/ ref and reads the variable named name, which is where the pull
// request's number or head commit comes in. A fetch of refs/heads/<branch> names neither.
func auditPathsFetchLine(lines []string, name string) bool {
	for _, line := range lines {
		if !strings.Contains(line, "git fetch") || !strings.Contains(line, "refs/pull/") {
			continue
		}

		if strings.Contains(line, name) {
			return true
		}
	}

	return false
}

// auditPathsRuns reports whether any of the lines of a step's run: script holds want.
func auditPathsRuns(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}

	return false
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

// auditPathsEnvVar is one variable a step's env: sets, as written.
type auditPathsEnvVar struct {
	name  string
	value string
}

// auditPathsEnvVars returns the variables env: sets, as written and without a trailing comment. A step's
// env: names its variables in upper case, which yamlread.Mapping refuses as keys, so this reads the block's
// lines as written.
func auditPathsEnvVars(env yamlread.Entry) []auditPathsEnvVar {
	variables := make([]auditPathsEnvVar, 0, len(env.Block))

	for _, line := range env.Block {
		name, value, ok := strings.Cut(line.Text, ":")
		if !ok {
			continue
		}

		variables = append(variables, auditPathsEnvVar{name: name, value: auditPathsWithoutComment(strings.TrimSpace(value))})
	}

	return variables
}

// auditPathsEnvValue returns the value env: sets for name, and false when it sets none.
func auditPathsEnvValue(env yamlread.Entry, name string) (string, bool) {
	for _, variable := range auditPathsEnvVars(env) {
		if variable.name == name {
			return variable.value, true
		}
	}

	return "", false
}
