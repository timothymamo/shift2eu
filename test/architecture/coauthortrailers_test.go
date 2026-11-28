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

	// coAuthorWorkflowYAMLPath is the other spelling GitHub reads the workflow
	// under: a copy at that path would define the co-author check a second time,
	// so the guard refuses it (issue #706, the auditor's finding L3).
	coAuthorWorkflowYAMLPath = ".github/workflows/coauthor-trailers.yaml"

	// ciWorkflowPath is the CI workflow whose action pins the co-author workflow
	// holds, and which may hold no co-author job of its own (issue #723).
	ciWorkflowPath = ".github/workflows/ci.yml"

	// legacyCoAuthorJobID and legacyCoAuthorCheckName are the id and the check
	// name of ci.yml's retired co-author job (issues #663 and #689), removed once
	// the maintainer required coAuthorCheckName (issue #723). The name guard
	// refuses both on every workflow but coauthor-trailers.yml, which holds the
	// one job of that id.
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
// Every event value reaches the shell through a step's env: alone, so no ${{ }}
// may stand anywhere in the job's whole block outside a step's env: block: not in
// a run: or a with:, nor in a job-level key such as if:, runs-on: or a job-level
// env: (issue #723). The two action
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
// reported under (ADR 0050, issues #706 and #723). The rollout is done: the
// maintainer requires Co-author trailers (base), which coauthor-trailers.yml
// reports from the base branch, and ci.yml's legacy coauthor-trailers job is
// gone, Lint's need of it with it. No workflow but coauthor-trailers.yml may
// report a (base) check, a templated name no allow-list entry pins, the legacy
// name Co-author trailers or the legacy job's id coauthor-trailers.
//
// The guard is the residual risk the ADR records: the required check pins the
// integration, not the workflow, so any workflow can report a job of that name
// from the same integration, and a pull request could add such a job to its own
// ci.yml that always passes. The names are read as a check run reports them, not
// as YAML spells them: `name: "Co-author trailers (base)"`, `name: 'Co-author
// trailers (base)'` and `name: ${{ 'Co-author trailers (base)' }}` all report a
// run of that name, and a name is a job's id when the job holds none (the
// auditor's finding F3). Every workflow GitHub runs is read, of both spellings -
// .yml and .yaml - and a job the reader cannot read as one name, such as a block
// scalar or a quoted key, fails this test closed rather than being skipped (the
// auditor's finding L3). A pull request could loosen this pin too, so it is a
// review aid, not a barrier. The trigger is the barrier.
func TestCICoAuthorTrailersStaysOutOfCI(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %q: %v", repoRoot, err)
	}

	paths, err := workflowPaths(root)
	if err != nil {
		t.Fatalf("list .github/workflows: %v", err)
	}

	if len(paths) == 0 {
		t.Fatalf("no workflow under .github/workflows, so this test would pin nothing")
	}

	// One source of the check: GitHub runs a workflow of either spelling, so a
	// coauthor-trailers.yaml beside the .yml would define the co-author check a
	// second time, under a trigger and steps this guard never reads.
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(coAuthorWorkflowYAMLPath))); err == nil {
		t.Errorf("%s exists beside %s, want the .yml alone: GitHub reads both spellings, so the copy defines the co-author check a second time (ADR 0050, issue #706)",
			coAuthorWorkflowYAMLPath, coAuthorWorkflowPath)
	}

	jobs := 0

	var named, byID []coAuthorJobName

	for _, name := range paths {
		if filepath.Base(name) == filepath.Base(coAuthorWorkflowPath) {
			continue
		}

		names, err := workflowJobNames(name, readWorkflow(t, root, name))
		if err != nil {
			t.Errorf("%s: %v, want every job of every workflow under .github/workflows read as a check run reports it: a job this reader cannot read could report any name, the co-author check's among them (ADR 0050, issue #706)",
				name, err)

			continue
		}

		jobs += len(names)

		for _, n := range names {
			if n.value() == legacyCoAuthorCheckName {
				named = append(named, n)
			}

			if n.id == legacyCoAuthorJobID {
				byID = append(byID, n)
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

	// The rollout is done (issue #723): ci.yml's legacy job is gone, so neither
	// its name nor its id may survive in any other workflow. A job of the name
	// reports a co-author check beside the required one; a job of the id is a
	// retired check a pull request could make pass.
	for _, got := range named {
		t.Errorf("%s: the %s job reports %q, want no workflow but %s to hold a job of that name: ci.yml's legacy job is gone with the rollout, and a second job of the name reports a co-author check beside %q (issue #723)",
			got.path, got.id, legacyCoAuthorCheckName, coAuthorWorkflowPath, coAuthorCheckName)
	}

	for _, got := range byID {
		t.Errorf("%s: the %s job holds the id %q, want no job outside %s to hold it: the legacy job's id is retired with the rollout, and a job of that id is a co-author check no review could tell from the one it replaces (issue #723)",
			got.path, got.id, legacyCoAuthorJobID, coAuthorWorkflowPath)
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

	// ruleLegacyName refuses the retired legacy check's name, and ruleLegacyJobID
	// its job's id, on every workflow but coauthor-trailers.yml: ci.yml's legacy
	// job is gone with the rollout (issue #723), and a job of either reports a
	// co-author check beside the required one.
	ruleLegacyName  = "is the retired legacy check's name"
	ruleLegacyJobID = "is the retired legacy job's id outside " + coAuthorWorkflowPath
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

// value returns the name the job's check run reports: the scalar the guard read
// from name:, unquoted there, and a job without a name: named by its id, which
// is the name GitHub reports for it.
func (n coAuthorJobName) value() string {
	if n.line == 0 {
		return n.id
	}

	return n.raw
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

// workflowPaths returns the paths, relative to root, of every workflow under
// .github/workflows that GitHub runs, sorted: both spellings, .yml and .yaml. A
// guard that globbed *.yml alone would never read a workflow added as
// spoof.yaml, and so never refuse the job name it reports (issue #706, the
// auditor's finding L3).
func workflowPaths(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		return nil, err
	}

	var paths []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if ext := filepath.Ext(entry.Name()); ext != ".yml" && ext != ".yaml" {
			continue
		}

		paths = append(paths, ".github/workflows/"+entry.Name())
	}

	slices.Sort(paths)

	return paths, nil
}

// workflowJobNames returns every job of the workflow document doc that path
// names, in the order of its id, with the name: it reports as a check. It fails
// closed: a job that is not a block mapping, and a job whose name: key or value
// is not one this guard reads as the single name GitHub reports, are errors, so
// a spelling it cannot read is refused rather than read as a job with no name:
// (issue #706, the auditor's finding L3).
func workflowJobNames(path, doc string) ([]coAuthorJobName, error) {
	jobs, err := workflowJobs(doc)
	if err != nil {
		return nil, err
	}

	names := make([]coAuthorJobName, 0, len(jobs))

	for _, id := range jobIDs(jobs) {
		name, err := coAuthorJobNameOf(id, jobs[id])
		if err != nil {
			return nil, err
		}

		name.path = path

		names = append(names, name)
	}

	return names, nil
}

// coAuthorJobNameOf returns the job id of a workflow with the name: its check
// run reports, read from the block of the job's keys. It fails closed on every
// spelling but a block mapping holding `name: <value>`, the value a single-line
// plain or quoted scalar without a backslash or a continuation line: a job whose
// body is a flow collection, a quoted name key, a value on another line, a value
// folded from the lines below it, a value with a leading space or a trailing
// comment and a block scalar all report a name to GitHub that a line-wise reader
// of `name: ` alone never compares (issue #706, the auditor's findings L1 and
// L3).
func coAuthorJobNameOf(id string, block []ciLine) (coAuthorJobName, error) {
	name := coAuthorJobName{id: id}

	if len(block) == 0 {
		return name, nil
	}

	indent := ciKeyIndent(block)

	for i, l := range block {
		if l.indent != indent {
			continue
		}

		// A job is a block mapping: a flow collection spells the same job to
		// GitHub, and holds a name: this line-wise reader never sees.
		if strings.HasPrefix(l.text, "{") || strings.HasPrefix(l.text, "[") {
			return name, fmt.Errorf("line %d: the job %s is a flow collection, want a block mapping of its keys: the reader sees no name: in it, so a name it reports would slip past every rule",
				l.num, id)
		}

		key, _, ok := strings.Cut(l.text, ":")
		if !ok || unquoteYAMLScalar(strings.TrimSpace(key)) != "name" {
			continue
		}

		if l.text == "name:" {
			return name, fmt.Errorf("line %d: the job %s holds its name value on the lines below the key, want a single-line plain or quoted scalar after `name: `: a value that does not sit on the key's line is a name this reader never compares",
				l.num, id)
		}

		if !strings.HasPrefix(l.text, "name: ") {
			return name, fmt.Errorf("line %d: the job %s spells its name key %q, want `name: <value>`: a quoted key, or a space before the colon, reads the same name to GitHub, so a job of that name could report a check beside the co-author check's own",
				l.num, id, l.text)
		}

		value, err := coAuthorNameScalar(l.num, id, strings.TrimPrefix(l.text, "name: "))
		name.line, name.raw = l.num, value

		if err != nil {
			return name, err
		}

		// A plain scalar continues on the lines below it, and YAML folds them into
		// one name: `name: Co-author` with `trailers (base)` under it reports
		// Co-author trailers (base), while a reader of the key's own line compares
		// Co-author, which no rule refuses (issue #706, the auditor's finding L1).
		if i+1 < len(block) && block[i+1].indent > l.indent {
			return name, fmt.Errorf("line %d: the job %s holds a name value that continues on the lines below, the next of them line %d deeper than the key, want a single-line plain or quoted scalar: YAML folds those lines into the name GitHub reports, so this reader compares a name no check reports",
				l.num, id, block[i+1].num)
		}

		return name, nil
	}

	return name, nil
}

// coAuthorNameScalar returns the name the name: value raw reports, and fails on
// every value it cannot read as the one name GitHub reports: a value with a
// leading space or a trailing comment, a block scalar's marker, a flow
// collection, a YAML indicator, a quoted scalar that does not close on the same
// line, and any value holding a backslash, whose escape spells the name in
// characters no literal comparison sees (issue #706, the auditor's finding L3).
func coAuthorNameScalar(num int, id, raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("line %d: the job %s holds an empty name value, want a single-line plain or quoted scalar: GitHub reports no name for it, so every rule compares nothing",
			num, id)
	}

	// YAML drops the spaces before a plain scalar: `name:  Co-author trailers` is
	// the name GitHub reports, while a reader of the key's line compares a value
	// with the extra space, which no rule sees.
	if raw[0] == ' ' || raw[0] == '\t' {
		return "", fmt.Errorf("line %d: the job %s holds a name value with a leading space, %q, want the value to start at the first character after `name: `: YAML reads a single space there as the separator and the rest as the name GitHub reports, so a comparison of this value meets no name a check reports",
			num, id, raw)
	}

	switch raw[0] {
	case '>', '|':
		return "", fmt.Errorf("line %d: the job %s holds the block scalar marker %q for its name, want a single-line plain or quoted scalar: the lines below the marker are the name GitHub reports, and this reader compares the marker",
			num, id, raw)
	case '[', '{':
		return "", fmt.Errorf("line %d: the job %s holds a flow collection %q for its name, want a single-line plain or quoted scalar: GitHub reports the name the collection holds",
			num, id, raw)
	case '&', '*', '!', '%', '@', '`':
		return "", fmt.Errorf("line %d: the job %s holds the YAML indicator %q in its name value, want a single-line plain or quoted scalar",
			num, id, raw)
	}

	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("line %d: the job %s holds a backslash in its name value %q, want a single-line plain or quoted scalar with none: an escape spells the name GitHub reports in characters no literal comparison sees",
			num, id, raw)
	}

	if raw[0] == '"' || raw[0] == '\'' {
		if len(raw) < 2 || raw[len(raw)-1] != raw[0] {
			return "", fmt.Errorf("line %d: the job %s holds the quoted name value %q, want one quoted scalar on the key's line: a quote that runs on reports a name this reader never compares",
				num, id, raw)
		}

		return raw[1 : len(raw)-1], nil
	}

	// A plain scalar ends where a comment starts, so the comment is no part of the
	// name GitHub reports; a reader that keeps it compares a name no check
	// reports. Only a plain value is refused here: a quoted one holding " #" is
	// read through its quotes above, and refused there if they do not close.
	// YAML starts a comment at a # after any white space, a tab as well as a
	// space (issue #723, point 3).
	for _, sep := range []string{" #", "\t#"} {
		if before, comment, ok := strings.Cut(raw, sep); ok {
			return "", fmt.Errorf("line %d: the job %s holds the plain name value %q with a trailing comment, %q, want a single-line plain or quoted scalar with none: YAML ends the plain scalar where the comment starts, so GitHub reports %q, a name this reader compares only with the comment kept",
				num, id, raw, "#"+comment, strings.TrimSpace(before))
		}
	}

	return raw, nil
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
// workflow but coauthor-trailers.yml, the one file allowed to report the check
// and to hold a job of the id coauthor-trailers, so every job it is handed may
// hold neither the legacy name nor that id, ci.yml's included (issues #706 and
// #723).
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

		if value == legacyCoAuthorCheckName {
			problems = append(problems, coAuthorNameProblem{job: n, rule: ruleLegacyName})
		}

		if n.id == legacyCoAuthorJobID {
			problems = append(problems, coAuthorNameProblem{job: n, rule: ruleLegacyJobID})
		}
	}

	return problems
}

// TestCICoAuthorTrailersNameGuardReadsEverySpelling pins the name guard's parser
// and rules on small workflow snippets (issue #706, the auditor's findings F3 and
// L3). The integration test above reads the repository's workflows, where a
// spelling no workflow uses goes untested: here each quoted, plain and templated
// variant of the refused names is read, and a name the guard must allow - the
// allow-listed matrix name, a quoted name holding no (base) - stays allowed.
// Since issue #723 the legacy name and id are refused on every workflow but the
// co-author one, ci.yml's own jobs included: the guard holds no exception for
// the legacy job it once kept there.
//
// The guard reads no spelling it cannot read as the single name a check run
// reports. A block scalar, a value on the lines below the key, a backslash
// escape, a quoted name key, a flow collection and a job that is not a block
// mapping are each refused with an error, so a name that would report a check
// beside the co-author check's own fails the test instead of passing it unseen.
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
		// wantErr is the text the guard's refusal holds, and empty when the guard
		// must read the snippet.
		wantErr string
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
			name:      "the legacy name and id on a job of ci.yml",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  coauthor-trailers:\n    name: Co-author trailers\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"coauthor-trailers": "Co-author trailers"},
			wantProblems: []string{
				`3: coauthor-trailers: "Co-author trailers": ` + ruleLegacyName,
				`3: coauthor-trailers: "Co-author trailers": ` + ruleLegacyJobID,
			},
		},
		{
			name:      "the legacy job id with no name: in ci.yml",
			path:      ciWorkflowPath,
			doc:       "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"coauthor-trailers": "coauthor-trailers"},
			wantProblems: []string{
				`0: coauthor-trailers: "coauthor-trailers": ` + ruleLegacyJobID,
			},
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
		{
			name:      "a (base) name in a .yaml workflow",
			path:      ".github/workflows/spoof.yaml",
			doc:       "jobs:\n  build:\n    name: Co-author trailers (base)\n    runs-on: ubuntu-latest\n",
			wantNames: map[string]string{"build": "Co-author trailers (base)"},
			wantProblems: []string{
				`3: build: "Co-author trailers (base)": ` + ruleBaseName,
			},
		},
		{
			name:    "a block-scalar (base) name",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name: >-\n      Co-author trailers (base)\n    runs-on: ubuntu-latest\n",
			wantErr: "block scalar marker",
		},
		{
			name:    "a literal block-scalar (base) name",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name: |\n      Co-author trailers (base)\n    runs-on: ubuntu-latest\n",
			wantErr: "block scalar marker",
		},
		{
			name:    "a (base) name on the lines below the key",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name:\n      Co-author trailers (base)\n    runs-on: ubuntu-latest\n",
			wantErr: "on the lines below the key",
		},
		{
			name:    "a plain name that folds with the lines below the key",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name: Co-author\n      trailers (base)\n    runs-on: ubuntu-latest\n",
			wantErr: "continues on the lines below",
		},
		{
			name:    "a plain name whose second word sits on the line below",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name: Co-author trailers\n      (base)\n    runs-on: ubuntu-latest\n",
			wantErr: "continues on the lines below",
		},
		{
			name:    "a name value with a leading space",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name:  Co-author trailers (base)\n    runs-on: ubuntu-latest\n",
			wantErr: "leading space",
		},
		{
			name:    "a plain name value with a trailing comment",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name: Co-author trailers (base) # the check\n    runs-on: ubuntu-latest\n",
			wantErr: "trailing comment",
		},
		{
			name:    "a plain name value with a tab before its trailing comment",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name: Co-author trailers (base)\t# the check\n    runs-on: ubuntu-latest\n",
			wantErr: "trailing comment",
		},
		{
			name:    "a plain name with no (base) and a tab before its trailing comment",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name: Build\t# the build job\n    runs-on: ubuntu-latest\n",
			wantErr: "trailing comment",
		},
		{
			name:    "a double-quoted (base) name spelled with escapes",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    name: \"Co-author trailers \\u0028base\\u0029\"\n    runs-on: ubuntu-latest\n",
			wantErr: "backslash",
		},
		{
			name:    "a double-quoted name key",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    \"name\": Co-author trailers (base)\n    runs-on: ubuntu-latest\n",
			wantErr: "spells its name key",
		},
		{
			name:    "a single-quoted name key",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    'name': Co-author trailers (base)\n    runs-on: ubuntu-latest\n",
			wantErr: "spells its name key",
		},
		{
			name:    "a flow-mapping job",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build: {name: \"Co-author trailers (base)\", runs-on: ubuntu-latest}\n",
			wantErr: "is not a job id ending in a colon",
		},
		{
			name:    "a job whose body is a flow mapping",
			path:    ciWorkflowPath,
			doc:     "jobs:\n  build:\n    {name: \"Co-author trailers (base)\", runs-on: ubuntu-latest}\n",
			wantErr: "flow collection",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			names, err := workflowJobNames(tc.path, tc.doc)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("workflowJobNames(%q) reads %v, want an error holding %q: a spelling the guard cannot read as the one name a check run reports is a name it compares with no rule, so a job of the co-author check's name could slip past it (ADR 0050, issue #706)",
						tc.doc, names, tc.wantErr)
				}

				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("workflowJobNames(%q) fails with %q, want an error holding %q", tc.doc, err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("workflowJobNames(%q): %v, want the snippet read: the real workflows hold only the spellings the guard reads, so it must not refuse them",
					tc.doc, err)
			}

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

// TestCICoAuthorTrailersJobRefusesAnExpressionOutsideAStepEnv pins the rule
// that every event value reaches the shell through a step's env: alone (issues
// #672 and #706, ADR 0050), read over the coauthor-trailers job's whole block
// and not over its steps only: an expression spliced into a job-level key - an
// if:, a timeout-minutes:, a runs-on: or a job-level env: - reaches as much of
// the run as one spliced into a run: line, and a reader of the step lines alone
// never compares it (issue #723, the reviewer's point 2.3).
//
// The rule is pinned here over snippets, one per place an expression may stand,
// because the real file holds none of the refused spellings: a step's env: is
// the one place an expression is read, and every other line with one is refused
// with its line number.
func TestCICoAuthorTrailersJobRefusesAnExpressionOutsideAStepEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// doc is the YAML snippet of a workflow holding one coauthor-trailers
		// job.
		doc string
		// want is every line of the job the rule refuses, rendered as its line
		// number and text, and nil when it refuses none.
		want []string
	}{
		{
			name: "an expression in a step's env:",
			doc: "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n    steps:\n      - name: the check\n" +
				"        env:\n          RANGE: ${{ github.sha }}\n        run: go test ./test/lint/\n",
		},
		{
			name: "an expression in a step's run:",
			doc: "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n    steps:\n      - name: the check\n" +
				"        env:\n          RANGE: ${{ github.sha }}\n        run: echo ${{ github.sha }} >&2\n",
			want: []string{`8: run: echo ${{ github.sha }} >&2`},
		},
		{
			name: "an expression in a step's with:",
			doc: "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n    steps:\n" +
				"      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1\n        with:\n          ref: ${{ github.head_ref }}\n",
			want: []string{`7: ref: ${{ github.head_ref }}`},
		},
		{
			name: "an expression in the job's if:",
			doc: "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n    if: ${{ !cancelled() }}\n    steps:\n" +
				"      - name: the check\n        run: go test ./test/lint/\n",
			want: []string{`4: if: ${{ !cancelled() }}`},
		},
		{
			name: "an expression in the job's timeout-minutes:",
			doc: "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n    timeout-minutes: ${{ fromJSON(vars.TIMEOUT) }}\n    steps:\n" +
				"      - name: the check\n        run: go test ./test/lint/\n",
			want: []string{`4: timeout-minutes: ${{ fromJSON(vars.TIMEOUT) }}`},
		},
		{
			name: "an expression in the job's runs-on:",
			doc: "jobs:\n  coauthor-trailers:\n    runs-on: ${{ matrix.os }}\n    steps:\n" +
				"      - name: the check\n        run: go test ./test/lint/\n",
			want: []string{`3: runs-on: ${{ matrix.os }}`},
		},
		{
			name: "an expression in a job-level env:",
			doc: "jobs:\n  coauthor-trailers:\n    runs-on: ubuntu-latest\n    env:\n      RANGE: ${{ github.sha }}\n    steps:\n" +
				"      - name: the check\n        run: go test ./test/lint/\n",
			want: []string{`5: RANGE: ${{ github.sha }}`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			jobs, err := workflowJobs(tc.doc)
			if err != nil {
				t.Fatalf("read the snippet %q: %v, want it read: the test pins the rule on the job's own lines", tc.doc, err)
			}

			job, ok := jobs["coauthor-trailers"]
			if !ok {
				t.Fatalf("the snippet %q holds no coauthor-trailers job, want one: it holds %v", tc.doc, jobIDs(jobs))
			}

			var got []string

			for _, l := range coAuthorExpressionsOutsideStepEnv(job) {
				got = append(got, fmt.Sprintf("%d: %s", l.num, l.text))
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("coAuthorExpressionsOutsideStepEnv(%q) (-want +got):\n%s\nWant every line of the job holding a ${{ }} expression refused but a step's env: values: an expression spliced into a job-level key or a step's run: or with: reaches the shell as a word of the event's author, so the reader must compare the job's whole block (ADR 0050, issues #672 and #723)",
					tc.doc, diff)
			}
		})
	}
}

// TestCICoAuthorTrailersReadsBothWorkflowSpellings pins that the guard's reader
// takes every workflow file GitHub runs under .github/workflows: both *.yml and
// *.yaml, and neither a directory nor a file of another extension (issue #706,
// the auditor's finding L3). A glob of *.yml alone left a workflow added as
// spoof.yaml unread, and so its job names unrefused.
func TestCICoAuthorTrailersReadsBothWorkflowSpellings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")

	if err := os.MkdirAll(filepath.Join(dir, "nested.yaml"), 0o750); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}

	for _, name := range []string{"ci.yml", "spoof.yaml", "notes.txt", "workflow.yaml.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("jobs: {}\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	paths, err := workflowPaths(root)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}

	want := []string{".github/workflows/ci.yml", ".github/workflows/spoof.yaml"}

	if diff := cmp.Diff(want, paths); diff != "" {
		t.Errorf("workflowPaths(%q) (-want +got):\n%s\nWant both spellings GitHub runs, .yml and .yaml, and no other file: a workflow added under one spelling the reader skips is a job name the guard never reads",
			root, diff)
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
	}

	// Every event value reaches the shell through a step's env: alone, read over
	// the job's whole block: an expression anywhere else - a run: line, a with:
	// value, a job-level if:, runs-on:, timeout-minutes: or env: - is spliced
	// into the workflow's own text, where a shell or an action reads it as a word
	// of the event's author (issues #672 and #723). No key of this job needs one,
	// so there is no exception.
	for _, l := range coAuthorExpressionsOutsideStepEnv(job) {
		t.Errorf("line %d: %q splices a ${{ }} expression outside a step's env: value, want every value handed to the shell in a step's env: (ADR 0050)",
			l.num, l.text)
	}
}

// coAuthorExpressionsOutsideStepEnv returns every line of the job block job, as
// workflowJobs returns it, that holds a ${{ }} expression outside a step's env:
// block, in document order. Only the env: key of an item of the job's steps:
// list counts: a job-level env: reaches every step, and so is refused like any
// other job-level key. A steps: list this reader cannot split is read as holding
// no env:, so every expression in it is refused: the rule fails closed.
func coAuthorExpressionsOutsideStepEnv(job []ciLine) []ciLine {
	inStepEnv := make(map[int]bool)

	if steps, ok := ciKeyBlock(job, "steps"); ok {
		if items, err := ciItems(steps); err == nil {
			for _, item := range items {
				env, ok := ciKeyBlock(item.lines, "env")
				if !ok {
					continue
				}

				for _, l := range env {
					inStepEnv[l.num] = true
				}
			}
		}
	}

	var out []ciLine

	for _, l := range job {
		if strings.Contains(l.text, "${{") && !inStepEnv[l.num] {
			out = append(out, l)
		}
	}

	return out
}

// ciKeyBlock returns the lines below key in block, read only at block's own key
// indentation, so a key of the same name deeper in a value is never taken for
// it, and whether block holds the key.
func ciKeyBlock(block []ciLine, key string) ([]ciLine, bool) {
	if len(block) == 0 {
		return nil, false
	}

	indent := ciKeyIndent(block)

	for i, l := range block {
		if l.indent != indent || (l.text != key+":" && !strings.HasPrefix(l.text, key+": ")) {
			continue
		}

		var below []ciLine

		for _, b := range block[i+1:] {
			if b.indent <= l.indent {
				break
			}

			below = append(below, b)
		}

		return below, true
	}

	return nil, false
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

	lines := workflowLines(t, ciWorkflowPath, readWorkflow(t, root, ciWorkflowPath))

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
		t.Fatalf("%s pins %s at %v, want exactly one pin: the coauthor-trailers workflow holds ci.yml's pins, and Renovate updates both",
			ciWorkflowPath, prefix, pins)
	}

	return pins[0]
}
