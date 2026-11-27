package architecture_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// coAuthorWorkflowPath is the workflow of the co-author check (ADR 0050).
	// GitHub reads its definition from the base branch's commit under
	// pull_request_target, never from the pull request, so a pull request cannot
	// change the run that checks its own commits.
	coAuthorWorkflowPath = ".github/workflows/coauthor-trailers.yml"

	// coAuthorCheckName is the job's name, which the maintainer makes a required
	// status check on main (ADR 0050, Maintainer steps).
	coAuthorCheckName = "Co-author trailers (base)"

	// ciWorkflowPath is the CI workflow that keeps the legacy co-author job during
	// the two-step rollout (issue #706): the follow-up PR removes that job once the
	// maintainer requires coAuthorCheckName, and the guard's exception with it.
	ciWorkflowPath = ".github/workflows/ci.yml"

	// legacyCoAuthorJobID and legacyCoAuthorCheckName are ci.yml's co-author job
	// and the check it reports, kept from before the move (issues #663 and #689) so
	// a co-author check still runs on every pull request until the maintainer
	// requires coAuthorCheckName (issue #706).
	legacyCoAuthorJobID     = "coauthor-trailers"
	legacyCoAuthorCheckName = "Co-author trailers"
)

// TestCICoAuthorTrailersRunsFromTheBaseBranch pins the co-author check's own
// workflow file (ADR 0050, issue #706). The check moved out of ci.yml because
// GitHub reads a pull_request job's steps from the pull request's own ci.yml: a
// pull request could rewrite the job (run its own test/lint, drop the range,
// exit 0) and loosen ci.yml's pin in the same diff, and both checks passed.
// Under pull_request_target GitHub reads the workflow from the base branch's
// commit instead, so the job definition, the test/lint that runs, its Go and the
// allow list all come from main.
//
// The file is read as a closed schema top to bottom. The trigger is what makes
// the check hold, so it is pinned alone: pull_request_target with exactly the
// four types whose events can change what the check reads, and no branches:,
// paths: or paths-ignore: filter - a required check that never reports blocks
// its pull request for good. The permissions:, the concurrency: group and the
// job's keys are pinned whole, and the string secrets. may not appear at all.
// The four steps, in order, are pinned each with a closed set of keys and its
// with:, env: and run: line by line: a pin that only asks for a checkout and a
// go test lets through a ref: onto the pull request's head, a checkout of its
// repository, an extra command beside the fetch, or a GOFLAGS that skips every
// test.
//
// Every event value reaches the shell through env: alone, so no ${{ }} may stand
// in a step outside its env: block, and no run: may hold one. The two action
// pins are ci.yml's, so one Renovate update keeps both workflows on the same
// actions.
func TestCICoAuthorTrailersRunsFromTheBaseBranch(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %q: %v", repoRoot, err)
	}

	doc := readWorkflow(t, root, coAuthorWorkflowPath)

	checkCoAuthorTrigger(t, doc)
	checkCoAuthorTrust(t, doc)
	checkCoAuthorJob(t, root, doc)
}

// TestCICoAuthorTrailersStaysOutOfCI pins the names the co-author check may be
// reported under (ADR 0050, issue #706). The move to pull_request_target is a
// two-step rollout: until the maintainer requires Co-author trailers (base),
// ci.yml keeps the legacy coauthor-trailers job, the only job named Co-author
// trailers, so a co-author check still runs on every pull request. No workflow
// but coauthor-trailers.yml may report a (base) check, a templated name no
// allow-list entry pins, or the legacy job's id.
//
// The guard is the residual risk the ADR records: the required check pins the
// integration, not the workflow, so any workflow can report a job of that name
// from the same integration, and a pull request could add such a job to its own
// ci.yml that always passes. The names are read as a check run reports them, not
// as YAML spells them: `name: "Co-author trailers (base)"`, `name: 'Co-author
// trailers (base)'` and `name: ${{ 'Co-author trailers (base)' }}` all report a
// run of that name, and a name is a job's id when the job holds none (the
// auditor's finding F3). A pull request could loosen this pin too, so it is a
// review aid, not a barrier. The trigger is the barrier.
func TestCICoAuthorTrailersStaysOutOfCI(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %q: %v", repoRoot, err)
	}

	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("list .github/workflows: %v", err)
	}

	if len(paths) == 0 {
		t.Fatalf("no workflow under .github/workflows, so this test would pin nothing")
	}

	jobs := 0
	var legacy []coAuthorJobName

	for _, path := range paths {
		if filepath.Base(path) == filepath.Base(coAuthorWorkflowPath) {
			continue
		}

		name := ".github/workflows/" + filepath.Base(path)
		names := workflowJobNames(t, name, readWorkflow(t, root, name))

		jobs += len(names)

		for _, n := range names {
			if n.value() == legacyCoAuthorCheckName {
				legacy = append(legacy, n)
			}
		}

		for _, problem := range coAuthorNameProblems(names) {
			t.Errorf("%s: %s, want no workflow but %s to report a check beside the co-author check's own: a required check pins the integration, not the workflow, so a job of that name elsewhere could report a check run beside the real one, however the name is quoted or templated (ADR 0050, issue #706)",
				name, problem, coAuthorWorkflowPath)
		}
	}

	if jobs == 0 {
		t.Fatalf("the workflows hold no job this test could read, so the name guard pins nothing: %v", paths)
	}

	// The rollout keeps exactly one job of the legacy name, ci.yml's
	// coauthor-trailers, beside the new check; the follow-up that removes it
	// changes this test with it.
	switch len(legacy) {
	case 0:
		t.Errorf("no workflow holds a job named %q, want exactly one, ci.yml's %s: the rollout keeps the old job, and so a co-author check, on every pull request until the maintainer requires %q (issue #706)",
			legacyCoAuthorCheckName, legacyCoAuthorJobID, coAuthorCheckName)
	case 1:
		if got := legacy[0]; got.path != ciWorkflowPath || got.id != legacyCoAuthorJobID {
			t.Errorf("%s: the %s job reports %q, want that name only on %s's %s job: the rollout keeps that one job beside the new check (issue #706)",
				got.path, got.id, legacyCoAuthorCheckName, ciWorkflowPath, legacyCoAuthorJobID)
		}
	default:
		t.Errorf("%d workflows hold a job named %q, want exactly one, ci.yml's %s: the rollout keeps that one job and no second way to report it (issue #706)",
			len(legacy), legacyCoAuthorCheckName, legacyCoAuthorJobID)
	}
}

// The rules coAuthorNameProblems refuses a job name by. A check run's name is the
// job's name: when it holds one, the job's id otherwise, so the guard reads the
// name the run reports rather than the YAML's spelling of it.
const (
	// ruleBaseName refuses every name holding (base), quoted or plain: the
	// required check's own name is Co-author trailers (base), so a job reporting
	// it, however it is spelled, reports a check run beside the real one.
	ruleBaseName = "names a (base) check"

	// ruleTemplated refuses a name holding a ${{ }} expression that no allow-list
	// entry pins: the expression expands to a name no literal comparison sees, the
	// co-author check's among them.
	ruleTemplated = "holds a ${{ }} expression no allow-list entry pins"

	// ruleLegacyName refuses the legacy job's name anywhere but on ci.yml's
	// coauthor-trailers job, and ruleLegacyJobID its id anywhere but in ci.yml: the
	// rollout keeps one legacy job and no second way to report it.
	ruleLegacyName  = "is the legacy check's name outside ci.yml's coauthor-trailers job"
	ruleLegacyJobID = "is the legacy job's id outside ci.yml"
)

// coAuthorTemplatedJobNames is the allow list of job names that may hold a
// ${{ }} expression: today's only one, ci.yml's lint matrix entry, whose name
// GitHub expands once per target. A templated name added to a workflow is a
// decision this test asks for, and a name on this list is pinned as it stands.
var coAuthorTemplatedJobNames = []string{
	"Lint ${{ matrix.goos }}/${{ matrix.goarch }} cgo=${{ matrix.cgo }}",
}

// coAuthorJobName is one job of a workflow: the file it sits in, its id, the line
// its name: sits on (0 when the job holds none) and the value as the workflow
// writes it.
type coAuthorJobName struct {
	path string
	id   string
	line int
	raw  string
}

// value returns the name the job's check run reports: a single- or
// double-quoted scalar unquoted, a plain value as it stands, and a job without a
// name: named by its id, which is the name GitHub reports for it.
func (n coAuthorJobName) value() string {
	if n.line == 0 {
		return n.id
	}

	return unquoteYAMLScalar(n.raw)
}

// unquoteYAMLScalar returns the text a single- or double-quoted YAML scalar
// holds and a plain value unchanged. A quoted spelling of a name is the same
// name to GitHub, so the guard reads it as one: `name: "Co-author trailers
// (base)"` may not slip past a comparison the plain spelling alone meets.
func unquoteYAMLScalar(raw string) string {
	if len(raw) < 2 {
		return raw
	}

	if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
		return raw[1 : len(raw)-1]
	}

	return raw
}

// workflowJobNames returns every job of the workflow document doc that path
// names, in the order of its id, with the name: it reports as a check.
func workflowJobNames(t *testing.T, path, doc string) []coAuthorJobName {
	t.Helper()

	jobs, err := workflowJobs(doc)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	names := make([]coAuthorJobName, 0, len(jobs))

	for _, id := range jobIDs(jobs) {
		name := coAuthorJobName{path: path, id: id}

		if num, raw, ok := ciScalar(jobs[id], "name"); ok {
			name.line, name.raw = num, raw
		}

		names = append(names, name)
	}

	return names
}

// coAuthorNameProblem is one job of a workflow that may report a status check
// beside the co-author check's own, and the rule that refuses the name.
type coAuthorNameProblem struct {
	job  coAuthorJobName
	rule string
}

// String renders the problem as the line its name: sits on, the job's id, the
// name it reports and the rule that refuses it.
func (p coAuthorNameProblem) String() string {
	return fmt.Sprintf("%d: %s: %q: %s", p.job.line, p.job.id, p.job.value(), p.rule)
}

// coAuthorNameProblems returns one problem per job of names whose name may
// report a check beside the co-author check's own. The caller reads every
// workflow but coauthor-trailers.yml, the one file allowed to report the new
// check, so ci.yml's legacy coauthor-trailers job is the one job the legacy name
// and id are allowed on (issue #706).
func coAuthorNameProblems(names []coAuthorJobName) []coAuthorNameProblem {
	var problems []coAuthorNameProblem

	for _, n := range names {
		value := n.value()

		if strings.Contains(value, "${{") {
			if !slices.Contains(coAuthorTemplatedJobNames, value) {
				problems = append(problems, coAuthorNameProblem{job: n, rule: ruleTemplated})
			}

			continue
		}

		if strings.Contains(value, "(base)") {
			problems = append(problems, coAuthorNameProblem{job: n, rule: ruleBaseName})
		}

		if value == legacyCoAuthorCheckName && (n.path != ciWorkflowPath || n.id != legacyCoAuthorJobID) {
			problems = append(problems, coAuthorNameProblem{job: n, rule: ruleLegacyName})
		}

		if n.id == legacyCoAuthorJobID && n.path != ciWorkflowPath {
			problems = append(problems, coAuthorNameProblem{job: n, rule: ruleLegacyJobID})
		}
	}

	return problems
}

// TestCICoAuthorTrailersNameGuardReadsEverySpelling pins the name guard's parser
// and rules on small workflow snippets (issue #706, the auditor's finding F3).
// The integration test above reads the repository's workflows, where a spelling
// no workflow uses goes untested: here each quoted, plain and templated variant
// of the refused names is read, and a name the guard must allow - the allow-listed
// matrix name, ci.yml's legacy job, a quoted name holding no (base) - stays
// allowed.
func TestCICoAuthorTrailersNameGuardReadsEverySpelling(t *testing.T) {
	t.Parallel()

	const otherWorkflowPath = ".github/workflows/other.yml"

	tests := []struct {
		name string
		// path is the workflow file the snippet stands in, since the guard reads
		// ci.yml's own job and no other as the legacy one.
		path string
		// doc is the YAML snippet of the workflow.
		doc string
		// wantNames is the name each job of the snippet reports, keyed by its id.
		wantNames map[string]string
		// wantProblems is the rendering of every problem the guard reports, in
		// order, and nil when it reports none.
		wantProblems []string
	}{
		{
			name:      "a plain name that reports no (base) check",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"build": "Build"},
		},
		{
			name:      "a double-quoted (base) name",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  build:\n    name: \"Co-author trailers (base)\"\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"build": "Co-author trailers (base)"},
			wantProblems: []string{
				`3: build: "Co-author trailers (base)": ` + ruleBaseName,
			},
		},
		{
			name:      "a single-quoted (base) name",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  build:\n    name: 'Co-author trailers (base)'\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"build": "Co-author trailers (base)"},
			wantProblems: []string{
				`3: build: "Co-author trailers (base)": ` + ruleBaseName,
			},
		},
		{
			name:      "a plain (base) name",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  build:\n    name: Co-author trailers (base)\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"build": "Co-author trailers (base)"},
			wantProblems: []string{
				`3: build: "Co-author trailers (base)": ` + ruleBaseName,
			},
		},
		{
			name:      "a (base) name spliced from an expression",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  build:\n    name: ${{ 'Co-author trailers (base)' }}\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"build": "${{ 'Co-author trailers (base)' }}"},
			wantProblems: []string{
				`3: build: "${{ 'Co-author trailers (base)' }}": ` + ruleTemplated,
			},
		},
		{
			name:      "a templated name no allow-list entry pins",
			path:      otherWorkflowPath,
			doc:       "jobs:\n  deploy:\n    name: Deploy ${{ github.ref }}\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"deploy": "Deploy ${{ github.ref }}"},
			wantProblems: []string{
				`3: deploy: "Deploy ${{ github.ref }}": ` + ruleTemplated,
			},
		},
		{
			name:      "the allow-listed matrix name",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  lint-target:\n    name: Lint ${{ matrix.goos }}/${{ matrix.goarch }} cgo=${{ matrix.cgo }}\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"lint-target": "Lint ${{ matrix.goos }}/${{ matrix.goarch }} cgo=${{ matrix.cgo }}"},
		},
		{
			name:      "a quoted name holding no (base)",
			path:      otherWorkflowPath,
			doc:       "jobs:\n  build:\n    name: \"Build (linux)\"\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"build": "Build (linux)"},
		},
		{
			name:      "ci.yml's legacy job reports Co-author trailers",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  coauthor-trailers:\n    name: Co-author trailers\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"coauthor-trailers": "Co-author trailers"},
		},
		{
			name:      "ci.yml's legacy job id with no name:",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"coauthor-trailers": "coauthor-trailers"},
		},
		{
			name:      "the legacy name on another job of ci.yml",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  lint:\n    name: Co-author trailers\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"lint": "Co-author trailers"},
			wantProblems: []string{
				`3: lint: "Co-author trailers": ` + ruleLegacyName,
			},
		},
		{
			name:      "the legacy name in another workflow",
			path:      otherWorkflowPath,
			doc:       "jobs:\n  coauthor-trailers:\n    name: Co-author trailers\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"coauthor-trailers": "Co-author trailers"},
			wantProblems: []string{
				`3: coauthor-trailers: "Co-author trailers": ` + ruleLegacyName,
				`3: coauthor-trailers: "Co-author trailers": ` + ruleLegacyJobID,
			},
		},
		{
			name:      "the legacy job id with no name: in another workflow",
			path:      otherWorkflowPath,
			doc:       "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"coauthor-trailers": "coauthor-trailers"},
			wantProblems: []string{
				`0: coauthor-trailers: "coauthor-trailers": ` + ruleLegacyJobID,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			names := workflowJobNames(t, tc.path, tc.doc)

			gotNames := make(map[string]string, len(names))
			for _, n := range names {
				gotNames[n.id] = n.value()
			}

			if diff := cmp.Diff(tc.wantNames, gotNames); diff != "" {
				t.Errorf("workflowJobNames(%q) reports the jobs' names (-want +got):\n%s\nWant each name as a check run reports it, a quoted scalar unquoted and a job without a name: named by its id", tc.doc, diff)
			}

			var got []string

			for _, problem := range coAuthorNameProblems(names) {
				got = append(got, problem.String())
			}

			if diff := cmp.Diff(tc.wantProblems, got); diff != "" {
				t.Errorf("coAuthorNameProblems(%q) (-want +got):\n%s\nWant every name that could report a check beside Co-author trailers (base) refused, however it is quoted or templated, and the allowed ones alone reported by none", tc.doc, diff)
			}
		})
	}
}

// checkCoAuthorTrigger pins the workflow's trigger (ADR 0050): pull_request_target
// alone, with exactly the four types under which a field the check reads can
// change, and no branch or path filter. pull_request, or any other trigger,
// would run the pull request's own copy of the workflow and of test/lint; a
// branch or path filter would leave a required check that never reports, which
// blocks its pull request for good. A push trigger has no meaning here: on main
// the old job only skipped.
func checkCoAuthorTrigger(t *testing.T, doc string) {
	t.Helper()

	lines := workflowLines(t, coAuthorWorkflowPath, doc)

	// The top level is a closed schema too: a workflow-level env: or defaults:
	// reaches every step of the job, the check's included.
	wantKeys(t, coAuthorWorkflowPath+" top level", lines, "name", "on", "permissions", "concurrency", "jobs")

	if num, name, ok := ciScalar(lines, "name"); !ok {
		t.Errorf("the top level of %s holds no name:, want Co-author trailers: the concurrency group is keyed on github.workflow, which is this name",
			coAuthorWorkflowPath)
	} else if name != "Co-author trailers" {
		t.Errorf("line %d: %s names the workflow %q, want %q", num, coAuthorWorkflowPath, name, "Co-author trailers")
	}

	on, ok := ciBlock(lines, "on")
	if !ok {
		t.Fatalf("%s holds no on: block, want pull_request_target alone: the trigger is what makes GitHub read this file, test/lint and the allow list from the base branch (ADR 0050)",
			coAuthorWorkflowPath)
	}

	wantKeys(t, "on: block of "+coAuthorWorkflowPath, on, "pull_request_target")

	target, ok := ciBlock(on, "pull_request_target")
	if !ok {
		t.Fatalf("%s holds no pull_request_target trigger, want that trigger alone: a pull_request trigger runs the pull request's own copy of this file (ADR 0050)",
			coAuthorWorkflowPath)
	}

	wantKeys(t, "pull_request_target trigger of "+coAuthorWorkflowPath, target, "types")

	wantExactLines(t, "pull_request_target trigger of "+coAuthorWorkflowPath, target, []string{
		// A push moves the head (synchronize), and retargeting the base branch is
		// an edited event, so a pull request retargeted to main is checked again
		// by main's copy.
		"types: [opened, synchronize, reopened, edited]",
	})
}

// checkCoAuthorTrust pins what the job may hold (ADR 0050): a read-only token at
// the workflow level with no job-level permissions: and no environment:, no
// secret anywhere in the file, and a concurrency group keyed on the pull
// request's number, so a push cancels only its own pull request's earlier run
// and not every pull request's.
func checkCoAuthorTrust(t *testing.T, doc string) {
	t.Helper()

	lines := workflowLines(t, coAuthorWorkflowPath, doc)

	permissions, ok := ciBlock(lines, "permissions")
	if !ok {
		t.Fatalf("%s holds no top-level permissions:, want contents: read: the job checks out the base branch and fetches the pull request's commits as objects, and needs nothing else",
			coAuthorWorkflowPath)
	}

	wantExactLines(t, "permissions: block of "+coAuthorWorkflowPath, permissions, []string{"contents: read"})

	concurrency, ok := ciBlock(lines, "concurrency")
	if !ok {
		t.Fatalf("%s holds no concurrency:, want a group keyed on the pull request's number: under pull_request_target github.ref is the base branch, shared by every pull request (ADR 0050)",
			coAuthorWorkflowPath)
	}

	wantKeys(t, "concurrency: block of "+coAuthorWorkflowPath, concurrency, "group", "cancel-in-progress")

	wantExactLines(t, "concurrency: block of "+coAuthorWorkflowPath, concurrency, []string{
		"group: ${{ github.workflow }}-${{ github.event.pull_request.number }}",
		"cancel-in-progress: true",
	})

	// The string is refused in the whole document, a comment included: the ADR
	// holds the file to using no secret at all, so a mention is as good a reason as
	// any for the reviewer to look.
	for i, raw := range strings.Split(doc, "\n") {
		if strings.Contains(raw, "secrets.") {
			t.Errorf("line %d: %s holds %q, want the string secrets. nowhere in the file: the repository is public, so the fetch needs no credential and the job uses none (ADR 0050)",
				i+1, coAuthorWorkflowPath, strings.TrimSpace(raw))
		}
	}
}

// checkCoAuthorJob pins the workflow's one job and its four steps (ADR 0050). The
// job's keys are a closed set, so no if:, permissions:, environment:, container:,
// services:, defaults:, env:, continue-on-error: or strategy: can excuse or
// redirect the run, and its name is the one the maintainer requires on main.
//
// The four steps, in order, are the checkout of the base branch, the fetch of
// the pull request's commits as git objects, setup-go over the checkout's go.mod,
// and the check. A step added, removed or moved is a decision this test asks for.
func checkCoAuthorJob(t *testing.T, root, doc string) {
	t.Helper()

	jobs, err := workflowJobs(doc)
	if err != nil {
		t.Fatalf("read %s: %v", coAuthorWorkflowPath, err)
	}

	if got := jobIDs(jobs); !slices.Equal(got, []string{"coauthor-trailers"}) {
		t.Fatalf("%s holds the jobs %v, want exactly one, coauthor-trailers: the check is a workflow of its own so that GitHub reads it from the base branch, and a second job of the file runs under the same trigger (ADR 0050)",
			coAuthorWorkflowPath, got)
	}

	job := jobs["coauthor-trailers"]

	wantKeys(t, "coauthor-trailers job of "+coAuthorWorkflowPath, job, "name", "runs-on", "timeout-minutes", "steps")

	for _, want := range []struct{ key, value string }{
		// The name the maintainer makes a required status check on main, so it is
		// not a label this file can change on its own.
		{"name", coAuthorCheckName},
		{"runs-on", "ubuntu-latest"},
		{"timeout-minutes", "10"},
	} {
		if num, got, ok := ciScalar(job, want.key); !ok {
			t.Errorf("the coauthor-trailers job holds no %s: key, want %q", want.key, want.value)
		} else if got != want.value {
			t.Errorf("line %d: the coauthor-trailers job's %s is %q, want %q", num, want.key, got, want.value)
		}
	}

	steps, ok := ciBlock(job, "steps")
	if !ok {
		t.Fatalf("the coauthor-trailers job holds no steps: list, so it checks nothing")
	}

	items, err := ciItems(steps)
	if err != nil {
		t.Fatalf("the coauthor-trailers job's steps: %v", err)
	}

	if len(items) != 4 {
		t.Fatalf("the coauthor-trailers job holds %d steps, want exactly four: the checkout of the base branch, the fetch of the pull request's commits as objects, setup-go over the checkout's go.mod and the check (ADR 0050)",
			len(items))
	}

	checkCoAuthorBaseCheckoutStep(t, root, items[0])
	checkCoAuthorBaseFetchStep(t, items[1])
	checkCoAuthorBaseSetupGoStep(t, root, items[2])
	checkCoAuthorBaseCheckStep(t, items[3])

	// No step may use another action: under a read-only token and no secret,
	// every value the job hands a third-party action would be one the pull
	// request's own commits choose, and the ADR names the two it trusts.
	for _, item := range items {
		if _, uses, ok := ciScalar(item.lines, "uses"); ok &&
			!strings.HasPrefix(uses, "actions/checkout@") && !strings.HasPrefix(uses, "actions/setup-go@") {
			t.Errorf("line %d: a step of the coauthor-trailers job uses %q, want actions/checkout@… and actions/setup-go@… only (ADR 0050)",
				item.num, uses)
		}

		// Every event value reaches the shell through env: alone: an expression
		// anywhere else in the step, a run: line or a with: value, is spliced into
		// the workflow's own text, where a shell or an action reads it as a word
		// of the event's author (issue #672). No key of this job needs one, so
		// there is no exception.
		inEnv := make(map[int]bool, len(item.lines))

		if env, ok := ciBlock(item.lines, "env"); ok {
			for _, l := range env {
				inEnv[l.num] = true
			}
		}

		for _, l := range item.lines {
			if strings.Contains(l.text, "${{") && !inEnv[l.num] {
				t.Errorf("line %d: %q splices a ${{ }} expression outside an env: value, want every value handed to the shell in env: (ADR 0050)",
					l.num, l.text)
			}
		}
	}
}

// checkCoAuthorBaseCheckoutStep pins the checkout (ADR 0050): under
// pull_request_target a checkout with no ref: and no repository: is the base
// branch's tip, the commit GitHub read the workflow from, so the test/lint that
// runs, the go.mod setup-go reads and the allow list the check reads all come
// from that one commit. fetch-depth: 0 brings the whole history, without which
// the pull request's head is no object of the run, and persist-credentials: false
// leaves no token in .git/config.
func checkCoAuthorBaseCheckoutStep(t *testing.T, root string, item ciItem) {
	t.Helper()

	// A closed schema: a ref: or a repository: would check out the pull request's
	// own tree, and a shell: or a working-directory: beside the uses: and the
	// with: would leave both byte-identical and still run the wrong tree.
	wantKeys(t, "coauthor-trailers checkout step", item.lines, "uses", "with")

	_, uses, ok := ciScalar(item.lines, "uses")
	if !ok || !strings.HasPrefix(uses, "actions/checkout@") {
		t.Fatalf("line %d: the coauthor-trailers job's first step uses %q, want actions/checkout@… at ci.yml's pin: without the checkout the run checks out nothing (ADR 0050)",
			item.num, uses)
	}

	if want := ciActionPin(t, root, "actions/checkout@"); uses != want {
		t.Errorf("line %d: the coauthor-trailers checkout uses %q, want ci.yml's pin %q: one Renovate update keeps both workflows on the same action",
			item.num, uses, want)
	}

	with, ok := ciBlock(item.lines, "with")
	if !ok {
		t.Fatalf("line %d: the coauthor-trailers checkout holds no with:, want the whole history and no credentials", item.num)
	}

	wantExactLines(t, fmt.Sprintf("with: block of the coauthor-trailers checkout on line %d", item.num), with, []string{
		"persist-credentials: false",
		"fetch-depth: 0",
	})
}

// checkCoAuthorBaseFetchStep pins the fetch (ADR 0050): the pull request's
// commits are git objects the check reads through git log and git show, and no
// step ever checks them out or runs a line of their tree. The number comes
// through env: and is held to ^[1-9][0-9]*$ before it becomes part of a refspec,
// so no value of the event can reach the shell as a word of its own; a number
// that is not one fails the check closed rather than fetching some other ref.
func checkCoAuthorBaseFetchStep(t *testing.T, item ciItem) {
	t.Helper()

	// A closed schema: a working-directory: or a shell: beside the name:, env:
	// and run: would keep those byte-identical and still fetch somewhere else,
	// and an `if` or a continue-on-error: would excuse a failed fetch.
	wantKeys(t, "coauthor-trailers fetch step", item.lines, "name", "env", "run")

	script, ok, err := ciRunScript(item.lines)
	if err != nil {
		t.Fatalf("the coauthor-trailers fetch step on line %d: %v", item.num, err)
	}

	if !ok {
		t.Fatalf("line %d: the coauthor-trailers job's second step holds no run:, want the fetch of the pull request's commits as objects: without it the check reads no commit it has to check (ADR 0050)",
			item.num)
	}

	env, ok := ciBlock(item.lines, "env")
	if !ok {
		t.Fatalf("line %d: the coauthor-trailers fetch step holds no env:, want the pull request's number mapped into PR_NUMBER and read from there (ADR 0050)",
			item.num)
	}

	wantExactLines(t, fmt.Sprintf("env: block of the coauthor-trailers fetch step on line %d", item.num), env, []string{
		"PR_NUMBER: ${{ github.event.pull_request.number }}",
	})

	wantExactScript(t, fmt.Sprintf("run: script of the coauthor-trailers fetch step on line %d", item.num), script, []string{
		`if ! [[ "${PR_NUMBER}" =~ ^[1-9][0-9]*$ ]]; then`,
		`echo >&2 "coauthor-trailers: the pull request number is not a number: the check fails closed"`,
		`exit 1`,
		`fi`,
		// The head SHA under refs/remotes/pull/head, never a checkout of it, and
		// --no-recurse-submodules so no submodule of the pull request is fetched.
		`git fetch --no-tags --no-recurse-submodules origin "+refs/pull/${PR_NUMBER}/head:refs/remotes/pull/head"`,
	})
}

// checkCoAuthorBaseSetupGoStep pins setup-go (ADR 0050): the checkout is the
// base branch's commit, so go.mod is the base's, and the base's test/lint builds
// under the base's Go and toolchain pin, never the pull request's - a pull
// request that raises go.mod's version would otherwise never check the base's
// copy. cache: false, because under pull_request_target a cache entry is saved in
// the base branch's scope, which every later run would restore.
func checkCoAuthorBaseSetupGoStep(t *testing.T, root string, item ciItem) {
	t.Helper()

	wantKeys(t, "coauthor-trailers setup-go step", item.lines, "uses", "with")

	_, uses, ok := ciScalar(item.lines, "uses")
	if !ok || !strings.HasPrefix(uses, "actions/setup-go@") {
		t.Fatalf("line %d: the coauthor-trailers job's third step uses %q, want actions/setup-go@… after the fetch and before the check: the base's own Go and toolchain pin build its test/lint (ADR 0050)",
			item.num, uses)
	}

	if want := ciActionPin(t, root, "actions/setup-go@"); uses != want {
		t.Errorf("line %d: the coauthor-trailers setup-go uses %q, want ci.yml's pin %q: one Renovate update keeps both workflows on the same action",
			item.num, uses, want)
	}

	with, ok := ciBlock(item.lines, "with")
	if !ok {
		t.Fatalf("line %d: the coauthor-trailers setup-go holds no with:, so it names no go.mod to read", item.num)
	}

	wantExactLines(t, fmt.Sprintf("with: block of the coauthor-trailers setup-go on line %d", item.num), with, []string{
		// The checkout's go.mod, which is the base branch's: setup-go reads a plain
		// go.mod, so the exception PR #703 made for its go-version-file is gone.
		"go-version-file: go.mod",
		"cache: false",
	})
}

// checkCoAuthorBaseCheckStep pins the step that runs the check (ADR 0050): the
// base branch's test/lint, with -run naming TestBranchCoAuthorTrailers alone, and
// the range github.sha..head.sha - the commits a merge would add. github.sha is
// the base branch's tip the checkout took, so the allow list is read at the same
// commit the check runs from, and SHIFT2EU_COAUTHOR_CHECK=require makes a range
// the run cannot read (a force-push while it ran) fail the check closed rather
// than skip it.
func checkCoAuthorBaseCheckStep(t *testing.T, item ciItem) {
	t.Helper()

	// A closed schema: a shell: would run the run: line under another shell, a
	// working-directory: would move the check out of the checkout, and an if: or
	// a continue-on-error: would excuse it.
	wantKeys(t, "coauthor-trailers check step", item.lines, "name", "run", "env")

	script, ok, err := ciRunScript(item.lines)
	if err != nil {
		t.Fatalf("the coauthor-trailers check step on line %d: %v", item.num, err)
	}

	if !ok {
		t.Fatalf("line %d: the coauthor-trailers job's fourth step holds no run:, so CI checks no co-author trailer", item.num)
	}

	wantExactScript(t, fmt.Sprintf("run: script of the coauthor-trailers check step on line %d", item.num), script, []string{
		`go test -count=1 -v -run '^TestBranchCoAuthorTrailers$' ./test/lint/`,
	})

	env, ok := ciBlock(item.lines, "env")
	if !ok {
		t.Fatalf("line %d: the coauthor-trailers check step holds no env:, so it names no range and requires none", item.num)
	}

	wantExactLines(t, fmt.Sprintf("env: block of the coauthor-trailers check step on line %d", item.num), env, []string{
		// The base branch's tip the checkout took, and the pull request's head: the
		// commits a merge would add, which the fetch above brought.
		`SHIFT2EU_COAUTHOR_RANGE: ${{ format('{0}..{1}', github.sha, github.event.pull_request.head.sha) }}`,
		// The workflow runs on pull requests only, so a range it cannot find fails
		// the check closed; the push's own run reports.
		"SHIFT2EU_COAUTHOR_CHECK: require",
	})
}

// readWorkflow returns the document of a workflow file under root.
func readWorkflow(t *testing.T, root, path string) string {
	t.Helper()

	doc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v, want it to exist: the co-author check runs from a workflow of its own whose pull_request_target definition GitHub reads from the base branch, so a pull request cannot change the run that checks it (ADR 0050)",
			path, err)
	}

	return string(doc)
}

// workflowLines returns the content lines of the workflow file name holds.
func workflowLines(t *testing.T, name, doc string) []ciLine {
	t.Helper()

	lines, err := ciLines(doc)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	return lines
}

// ciActionPin returns the pin ci.yml uses for the action prefix names, such as
// "actions/checkout@", and fails when ci.yml holds none or more than one: the
// co-author workflow's pins are ci.yml's, and a ci.yml that pins one action twice
// at different commits leaves no pin to hold with it.
func ciActionPin(t *testing.T, root, prefix string) string {
	t.Helper()

	lines := workflowLines(t, ".github/workflows/ci.yml", readWorkflow(t, root, ".github/workflows/ci.yml"))

	var pins []string

	for _, l := range lines {
		value, ok := strings.CutPrefix(strings.TrimPrefix(l.text, "- "), "uses: ")
		if !ok || !strings.HasPrefix(value, prefix) {
			continue
		}

		if !slices.Contains(pins, value) {
			pins = append(pins, value)
		}
	}

	if len(pins) != 1 {
		t.Fatalf(".github/workflows/ci.yml pins %s at %v, want exactly one pin: the coauthor-trailers workflow holds ci.yml's pins, and Renovate updates both",
			prefix, pins)
	}

	return pins[0]
}
