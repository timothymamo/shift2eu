package check

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// The four steps through which the Test jobs of ci.yml and release.yml run
// TestPiEnvDenyExtensionLoads on the Node dev/lima.yaml pins (issue #548, ADR
// 0044), declared once for both workflows (issue #554): the read of
// dev/lima.yaml's nodeVersion, actions/setup-node installing exactly that
// version with its package-manager cache off, the check that the node on the
// PATH is that one, and the step that runs the tests with
// SHIFT2EU_REQUIRE_NODE: "1", so the load check fails rather than skips.
// release.yml's closed schema (testSteps) holds them as nodeSteps returns them;
// CITestJobNode holds ci.yml's Test job to the same steps.
const (
	// nodeStepID is the id: of the read step, through whose output the two
	// steps after it take the version.
	nodeStepID = "node"

	// nodeVersionExpr is the expression through which actions/setup-node and
	// the check take the version from the read step's output.
	nodeVersionExpr = "${{ steps." + nodeStepID + ".outputs.version }}"

	// nodeVersionRead is the line that makes a step the read of dev/lima.yaml's
	// nodeVersion, the one pin of the Node the VM installs for pi: two spaces
	// and the double quotes, exactly as pienvdenyload_test.go's
	// piNodeVersionPin parses the same line.
	nodeVersionRead = `version="$(sed -nE 's/^  nodeVersion: "([^"]*)"$/\1/p' dev/lima.yaml)"`

	// nodeVersionRange is the strict check that what the sed read is one X.Y.Z
	// release and nothing else.
	nodeVersionRange = `if ! [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then`

	// nodeVersionOutput writes the version to the read step's output, where
	// actions/setup-node takes it from.
	nodeVersionOutput = `echo "version=${version}" >> "${GITHUB_OUTPUT}"`

	// nodeCheckPrefix starts the line that makes a step the check that the
	// node on the PATH is the pinned one, and nodeCheckLine is that line whole.
	nodeCheckPrefix = `if [ "$(node --version)" !=`
	nodeCheckLine   = nodeCheckPrefix + ` "v${NODE_VERSION}" ]; then`

	// packageManagerCache is setup-node's package-manager-cache: off, as no
	// package manager runs in either job.
	packageManagerCache = "false"

	// requiredNodeName and requiredNodeValue are the variable of the step the
	// tests run in, and its value as both workflows write it: quoted.
	requiredNodeName  = "SHIFT2EU_REQUIRE_NODE"
	requiredNodeValue = `"1"`
)

// nodeSteps returns the four Node steps of the Test job of the workflow file,
// whose check names that file in its error, and whose tests run in the step
// named testsName that runs testsRun: the only parts in which ci.yml's and
// release.yml's steps differ.
func nodeSteps(file, testsName, testsRun string) []releaseStep {
	return []releaseStep{
		{
			what: "the read of dev/lima.yaml's nodeVersion", name: "The Node release dev/lima.yaml pins", id: nodeStepID,
			run: strings.Join([]string{
				`set -euo pipefail`,
				nodeVersionRead,
				nodeVersionRange,
				`  echo "::error file=dev/lima.yaml::nodeVersion is not one release like 1.2.3: '${version}'"`,
				`  exit 1`,
				`fi`,
				nodeVersionOutput,
			}, "\n"),
		},
		{
			what: "actions/setup-node", uses: setupNodeAction,
			with: map[string]*regexp.Regexp{
				"node-version":          exactly(nodeVersionExpr),
				"package-manager-cache": exactly(packageManagerCache),
			},
		},
		{
			what: "the check that node on the PATH is the pinned one", name: "The node on the PATH is the pinned one",
			env: []string{"NODE_VERSION: " + nodeVersionExpr},
			run: strings.Join([]string{
				`set -euo pipefail`,
				nodeCheckLine,
				`  echo "::error file=` + file + `::node on the PATH is $(node --version), want v${NODE_VERSION}"`,
				`  exit 1`,
				`fi`,
			}, "\n"),
		},
		{what: testsRun, name: testsName, env: []string{requiredNodeName + ": " + requiredNodeValue}, run: testsRun},
	}
}

// keys returns the keys a step holds exactly, those whose field is set, in the
// order a message lists them.
func (s releaseStep) keys() []string {
	var keys []string

	for _, k := range []struct {
		key string
		set bool
	}{
		{"name", s.name != ""},
		{"id", s.id != ""},
		{"uses", s.uses != nil},
		{"with", s.with != nil},
		{"env", s.env != nil},
		{"run", s.run != ""},
	} {
		if k.set {
			keys = append(keys, k.key)
		}
	}

	return keys
}

// The test job of ci.yml as CITestJobNode reads it (its key, ciTestJob, is
// dryrun.go's).
const (
	ciFile      = ".github/workflows/ci.yml"
	ciTestsName = "Unit and contract tests"
	ciTestsRun  = "make cover"
)

// ciTestJobKeys are the two keys ci.yml's test job itself must not hold:
// either one decides whether the job runs and whether its failure fails the
// run.
var ciTestJobKeys = []struct {
	key string
	// why says what the key would hide.
	why string
}{
	{"if", "a condition on the job can turn it off, and CI passes with TestPiEnvDenyExtensionLoads never run"},
	{"continue-on-error", "a Test job that fails without failing the run leaves CI green with the tests broken"},
}

// ciStep is one step of ci.yml's test job, as CITestJobNode reads it.
type ciStep struct {
	// num is the line the step's item starts on.
	num int
	// entries are the step's keys, in the order written.
	entries []yamlread.Entry
	// uses is the action the step runs, without the version comment beside
	// it; "" for a run: step.
	uses string
	// run is the step's command, a literal block's lines joined by newlines;
	// "" for none. runProblem says why it could not be read as one, "" when
	// it could.
	run, runProblem string
}

// entry returns the step's key, and whether it holds it.
func (s ciStep) entry(key string) (yamlread.Entry, bool) {
	i := slices.IndexFunc(s.entries, func(e yamlread.Entry) bool { return e.Key == key })
	if i < 0 {
		return yamlread.Entry{}, false
	}

	return s.entries[i], true
}

// scalar returns the value of the step's key, one scalar on its line; "" when
// it holds no such key or no such value.
func (s ciStep) scalar(key string) string {
	e, ok := s.entry(key)
	if !ok {
		return ""
	}

	value, err := yamlread.Single(e)
	if err != nil {
		return ""
	}

	return value
}

// runLines returns the step's command line by line, each without the
// indentation it is written at.
func (s ciStep) runLines() []string {
	lines := strings.Split(s.run, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}

	return lines
}

// CITestJobNode returns one message for every way the ci.yml document src's
// test job no longer runs TestPiEnvDenyExtensionLoads on the Node dev/lima.yaml
// pins, and nil when it does (issues #548, #554). It holds the job to the four
// Node steps nodeSteps declares for both workflows, as release.yml's schema
// holds that workflow's Test job: exactly one step of each, recognised by what
// it does (the read by its sed line, actions/setup-node by its uses:, the
// check by its compare line, the tests by their make cover), each with exactly
// the keys, name, id, uses, with, env and run the declaration lists, and in
// that order, the tests after the check. Beside the exact comparison, each
// part an edit could break is named on its own: the read's id, its X.Y.Z check
// and its output; setup-node's pin by commit, its version from the read's
// output and its cache off; the check's exit 1 inside its if and its
// NODE_VERSION from the read's output; and SHIFT2EU_REQUIRE_NODE: "1" on the
// tests. The job itself holds neither if: nor continue-on-error:, its keys
// read with their quotes stripped and whether their value sits on the key's
// line or below it. Unlike release.yml's, ci.yml's job is not a closed schema:
// it holds the other steps every pull request runs.
func CITestJobNode(src []byte) []string {
	r := &schemaReader{raw: rawLines(src)}

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", ciFile, err)}
	}

	jobsBlock, err := yamlread.Walk(lines, "jobs")
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", ciFile, err)}
	}

	jobs, err := yamlread.Mapping(jobsBlock)
	if err != nil {
		return []string{fmt.Sprintf("%s: jobs: %v", ciFile, err)}
	}

	i := slices.IndexFunc(jobs, func(e yamlread.Entry) bool { return e.Key == ciTestJob })
	if i < 0 {
		return []string{ciFile + " holds no test job under jobs:, so no CI run runs the tests the load check is in (issue #548)"}
	}

	job := jobs[i]

	r.ciJobKeys(job.Block)

	entries, err := yamlread.Mapping(job.Block)
	if err != nil {
		r.fail("ci.yml's test job: %v (issue #548)", err)

		return r.messages
	}

	j := slices.IndexFunc(entries, func(e yamlread.Entry) bool { return e.Key == "steps" })
	if j < 0 || entries[j].Value != "" || len(entries[j].Block) == 0 {
		r.fail("%s's test job holds no steps: list, so it runs no test (issue #548)", ciFile)

		return r.messages
	}

	steps, ok := r.ciSteps(entries[j].Block)
	if !ok {
		return r.messages
	}

	r.ciNodeSteps(steps, nodeSteps(ciFile, ciTestsName, ciTestsRun))

	return r.messages
}

// ciJobKeys refuses the job-level keys that decide whether ci.yml's test job
// runs and whether its failure fails the run. It reads every line at the
// indentation of the job's own keys, a quoted key with its quotes stripped:
// the mapping reader refuses a quoted key as one it does not understand, and
// would not name the key it hides.
func (r *schemaReader) ciJobKeys(job []yamlread.Line) {
	if len(job) == 0 {
		return
	}

	indent := job[0].Indent

	for _, l := range job {
		if l.Indent != indent {
			continue
		}

		name := l.Text
		if q := name[0]; q == '"' || q == '\'' {
			if end := strings.IndexByte(name[1:], q); end >= 0 {
				name = name[1 : 1+end]
			}
		} else {
			name, _, _ = strings.Cut(name, ":")
		}

		for _, k := range ciTestJobKeys {
			if name == k.key {
				r.fail("ci.yml's test job holds %s: (line %d), want none: %s (issue #548)", k.key, l.Num, k.why)
			}
		}
	}
}

// ciSteps reads the test job's steps: list.
func (r *schemaReader) ciSteps(block []yamlread.Line) ([]ciStep, bool) {
	items, err := yamlread.Sequence(block)
	if err != nil {
		r.fail("%s's test job's steps: %v", ciFile, err)

		return nil, false
	}

	steps := make([]ciStep, 0, len(items))

	for _, it := range items {
		entries, err := yamlread.Mapping(it.Lines)
		if err != nil {
			r.fail("%s's test job's step at line %d: %v", ciFile, it.Num, err)

			return nil, false
		}

		step := ciStep{num: it.Num, entries: entries}
		step.uses = step.scalar("uses")

		if e, ok := step.entry("run"); ok {
			step.run, step.runProblem = r.ciRun(e)
		}

		steps = append(steps, step)
	}

	return steps, true
}

// ciRun reads a step's run:: one scalar on its line, or a literal block, with
// or without a chomping indicator, whose lines are compared without the
// newline after the last one either way. A folded block is not read as the
// script it stands for: its lines are joined for the step to be recognised,
// and the problem is returned.
func (r *schemaReader) ciRun(e yamlread.Entry) (run, problem string) {
	switch {
	case e.Value == "|" || e.Value == "|-" || e.Value == "|+":
		e.Value = "|"

		value, err := yamlread.LiteralBlock(r.raw, e)
		if err == nil {
			return value, ""
		}

		problem = err.Error()
	case strings.HasPrefix(e.Value, "|") || strings.HasPrefix(e.Value, ">"):
		problem = fmt.Sprintf("line %d: run: is %q, want a literal block (|) or one line", e.Num, e.Value)
	default:
		value, err := yamlread.Single(e)
		if err != nil {
			return "", err.Error()
		}

		return value, ""
	}

	texts := make([]string, 0, len(e.Block))
	for _, l := range e.Block {
		texts = append(texts, l.Text)
	}

	return strings.Join(texts, "\n"), problem
}

// ciOne returns the index of the one step keep selects, and records a message
// when there is no such step or more than one: a second step of the same kind
// is a way to add one this check does not read. It returns -1 when it recorded
// a message.
func (r *schemaReader) ciOne(steps []ciStep, what string, keep func(ciStep) bool) int {
	var found []int

	for i, step := range steps {
		if keep(step) {
			found = append(found, i)
		}
	}

	if len(found) == 1 {
		return found[0]
	}

	r.fail("ci.yml's test job holds %d steps %s, want exactly 1 (issue #548)", len(found), what)

	return -1
}

// ciNodeSteps holds steps, ci.yml's test job's, to want, the four Node steps
// nodeSteps declares: the read, actions/setup-node, the check and the tests.
func (r *schemaReader) ciNodeSteps(steps []ciStep, want []releaseStep) {
	readWant, setupWant, checkWant, testsWant := want[0], want[1], want[2], want[3]

	read := r.ciOne(steps, "whose run reads dev/lima.yaml's nodeVersion, the one pin of the Node the VM installs for pi",
		func(s ciStep) bool { return strings.Contains(s.run, nodeVersionRead) })
	if read >= 0 {
		r.ciRead(steps[read], readWant)
	}

	setup := r.ciOne(steps, "that run actions/setup-node, the action that installs the pinned Node",
		func(s ciStep) bool { return strings.HasPrefix(s.uses, "actions/setup-node@") })
	if setup >= 0 {
		r.ciSetupNode(steps[setup], setupWant)
	}

	nodeCheck := r.ciOne(steps, "whose run checks that node on the PATH is the pinned one",
		func(s ciStep) bool { return strings.Contains(s.run, nodeCheckPrefix) })
	if nodeCheck >= 0 {
		r.ciNodeCheck(steps[nodeCheck], checkWant)
	}

	tests := r.ciOne(steps, "that run "+testsWant.run+", the step the tests run in",
		func(s ciStep) bool { return slices.Contains(s.runLines(), testsWant.run) })
	if tests >= 0 {
		r.ciTests(steps[tests], testsWant)
	}

	// The order the four steps depend on: setup-node takes its version from
	// the read's output, the check reads the node setup-node installed, and
	// the tests run on the node that was checked.
	for _, pair := range []struct {
		what string
		a, b int
	}{
		{"the step that reads dev/lima.yaml's nodeVersion (line %d) comes after actions/setup-node (line %d), want it before: setup-node takes the version from that step's output (issue #548)", read, setup},
		{"actions/setup-node (line %d) comes after the step that checks node (line %d), want it before: the check reads the node actions/setup-node installed (issue #548)", setup, nodeCheck},
		{"the step that checks node (line %d) comes after the step that runs " + testsWant.run + " (line %d), want it before: TestPiEnvDenyExtensionLoads runs on the node it checked (issue #548)", nodeCheck, tests},
	} {
		if pair.a >= 0 && pair.b >= 0 && pair.a > pair.b {
			r.fail(pair.what, steps[pair.a].num, steps[pair.b].num)
		}
	}
}

// ciStepExactly holds step, which is what, to want's keys, name, id, env and
// run exactly: a key beyond them, if: and continue-on-error: above all, turns
// the step off or lets it fail unseen.
func (r *schemaReader) ciStepExactly(what string, step ciStep, want releaseStep) {
	keys := want.keys()

	for _, e := range step.entries {
		if !slices.Contains(keys, e.Key) {
			r.fail("ci.yml's test job's %s (line %d) holds the key %q, want exactly %s: a key beyond these, if: and continue-on-error: above all, turns the step off or lets it fail unseen (issue #548)", what, step.num, e.Key, strings.Join(keys, ", "))
		}
	}

	for _, key := range keys {
		if _, ok := step.entry(key); !ok {
			r.fail("ci.yml's test job's %s (line %d) holds no %s, want exactly %s (issue #548)", what, step.num, key, strings.Join(keys, ", "))
		}
	}

	if got := step.scalar("name"); want.name != "" && got != want.name {
		r.fail("ci.yml's test job's %s (line %d) is named %q, want %q, as nodeSteps declares it for ci.yml and release.yml (issue #554)", what, step.num, got, want.name)
	}

	if e, ok := step.entry("env"); ok && want.env != nil {
		got := make([]string, 0, len(e.Block))
		for _, l := range e.Block {
			got = append(got, l.Text)
		}

		if e.Value != "" || !slices.Equal(got, want.env) {
			r.fail("ci.yml's test job's %s (line %d): env: holds %q, want exactly %q, so the step could run with an environment the check never lists (issue #554)", what, step.num, got, want.env)
		}
	}

	if _, ok := step.entry("run"); ok && want.run != "" {
		if step.runProblem != "" {
			r.fail("ci.yml's test job's %s (line %d): run: %s (issue #554)", what, step.num, step.runProblem)
		} else if step.run != want.run {
			r.fail("ci.yml's test job's %s (line %d): run: is %q, want exactly %q, as nodeSteps declares it for ci.yml and release.yml (issue #554)", what, step.num, step.run, want.run)
		}
	}
}

// ciRead checks the step that reads dev/lima.yaml's nodeVersion.
func (r *schemaReader) ciRead(step ciStep, want releaseStep) {
	r.ciStepExactly("step that reads dev/lima.yaml's nodeVersion", step, want)

	switch id := step.scalar("id"); {
	case id == "":
		r.fail("the step that reads dev/lima.yaml's nodeVersion (line %d) holds no id:, so actions/setup-node cannot take the version from its output (issue #548)", step.num)
	case id != want.id:
		r.fail("the step that reads dev/lima.yaml's nodeVersion (line %d) holds id: %q, want %q: the steps after it take the version from %s (issue #554)", step.num, id, want.id, nodeVersionExpr)
	}

	if !strings.Contains(step.run, nodeVersionRange) {
		r.fail("the step that reads dev/lima.yaml's nodeVersion (line %d) does not check that it is one X.Y.Z release (%s): a value the sed reads but dev/lima.yaml does not pin exactly would be installed, and the version a skip names would be no release (issue #548)", step.num, nodeVersionRange)
	}

	if !strings.Contains(step.run, nodeVersionOutput) {
		r.fail("the step that reads dev/lima.yaml's nodeVersion (line %d) does not write the version to its step output (%s), so actions/setup-node has nothing to take the version from (issue #548)", step.num, nodeVersionOutput)
	}
}

// ciSetupNode checks the actions/setup-node step: pinned by a commit, its
// with: mapping exactly the declaration's, the version taken from the read's
// output, and its package-manager cache off.
func (r *schemaReader) ciSetupNode(step ciStep, want releaseStep) {
	r.ciStepExactly("actions/setup-node step", step, want)

	if !want.uses.MatchString(step.uses) {
		r.fail("actions/setup-node (line %d) uses %q, want actions/setup-node@<40 hex digits>, the commit it is pinned to: a tag or a range can be moved under CI (issue #548)", step.num, step.uses)
	}

	withKeys := sortedKeys(want.with)

	e, ok := step.entry("with")
	if !ok || e.Value != "" || len(e.Block) == 0 {
		r.fail("actions/setup-node (line %d) holds no with: mapping, want exactly %s (issue #548)", step.num, strings.Join(withKeys, " and "))

		return
	}

	with, err := yamlread.Mapping(e.Block)
	if err != nil {
		r.fail("actions/setup-node (line %d): with: %v (issue #548)", step.num, err)

		return
	}

	values := map[string]yamlread.Entry{}

	for _, w := range with {
		values[w.Key] = w

		if !slices.Contains(withKeys, w.Key) {
			r.fail("actions/setup-node (line %d): with: holds the key %q, want exactly %s (issue #548)", step.num, w.Key, strings.Join(withKeys, " and "))
		}
	}

	for _, key := range withKeys {
		w, ok := values[key]
		if !ok {
			r.fail("actions/setup-node (line %d): with: holds no %s, want exactly %s (issue #548)", step.num, key, strings.Join(withKeys, " and "))

			continue
		}

		got, err := yamlread.Single(w)
		if err != nil {
			r.fail("actions/setup-node (line %d): with: %v (issue #548)", w.Num, err)

			continue
		}

		if want.with[key].MatchString(got) {
			continue
		}

		switch key {
		case "package-manager-cache":
			r.fail("actions/setup-node (line %d): with: package-manager-cache is %q, want %q: no package manager runs in this job, so its cache would save and restore what nothing reads (issue #548)", w.Num, got, packageManagerCache)
		case "node-version":
			r.fail("actions/setup-node (line %d): with: node-version is %q, want %q, the version the step that reads dev/lima.yaml wrote to its output: a literal is a version dev/lima.yaml does not pin, and the VM's pin must stay the only one (issue #548)", w.Num, got, nodeVersionExpr)
		default:
			r.fail("actions/setup-node (line %d): with: %s is %q, want one matching %s (issue #554)", w.Num, key, got, want.with[key])
		}
	}
}

// ciNodeCheck checks the step that compares node --version with the version
// setup-node installed, and fails when they differ.
func (r *schemaReader) ciNodeCheck(step ciStep, want releaseStep) {
	r.ciStepExactly("step that checks node", step, want)

	lines := step.runLines()

	compare := slices.Index(lines, nodeCheckLine)
	if compare < 0 {
		r.fail("the step that checks node (line %d) holds no line %s, want it to compare node --version with the version actions/setup-node installed, not with a literal and not with nothing (issue #548)", step.num, nodeCheckLine)
	}

	// After the compare, the branch has to fail: the exact line exit 1, and
	// the fi that closes the if straight after it. A substring match would
	// take an exit 10 and an exit 1 outside the branch.
	rest := lines[compare+1:]

	switch exit := slices.Index(rest, "exit 1"); {
	case exit < 0:
		r.fail("the step that checks node (line %d) never fails: it holds no line exit 1 after its compare line, so a node that is not the pinned one passes the check that was meant to catch it (issue #548)", step.num)
	case exit+1 >= len(rest) || rest[exit+1] != "fi":
		r.fail("the step that checks node (line %d) holds its exit 1 where the fi that closes the if does not follow it, so the exit runs outside the branch that watches node --version (issue #548)", step.num)
	}

	e, ok := step.entry("env")
	if !ok || e.Value != "" || len(e.Block) == 0 {
		r.fail("the step that checks node (line %d) holds no env: block, want NODE_VERSION: %s (issue #548)", step.num, nodeVersionExpr)

		return
	}

	switch got, ok := envValue(e.Block, "NODE_VERSION"); {
	case !ok:
		r.fail("the step that checks node (line %d) sets no NODE_VERSION in its env:, want the version actions/setup-node installed (issue #548)", step.num)
	case got != nodeVersionExpr:
		r.fail("the step that checks node (line %d) sets NODE_VERSION to %q, want %q, the version the step that reads dev/lima.yaml wrote to its output (issue #548)", step.num, got, nodeVersionExpr)
	}
}

// ciTests checks that the step the tests run in sets SHIFT2EU_REQUIRE_NODE:
// "1" exactly, as release.yml writes it: TestPiEnvDenyExtensionLoads fails
// rather than skips on 1, and the quoted form is the one the declaration
// holds, a bare 1 being turned into the same string by GitHub Actions.
func (r *schemaReader) ciTests(step ciStep, want releaseStep) {
	r.ciStepExactly("step that runs "+want.run, step, want)

	e, ok := step.entry("env")
	if !ok || e.Value != "" || len(e.Block) == 0 {
		r.fail("the step that runs %s (line %d) holds no env:, want %s: %s exactly: without it TestPiEnvDenyExtensionLoads skips, and CI passes with the extension never loaded (issue #548)", want.run, step.num, requiredNodeName, requiredNodeValue)

		return
	}

	switch got, ok := envValue(e.Block, requiredNodeName); {
	case !ok:
		r.fail("the step that runs %s (line %d) sets no %s in its env:, want %s exactly: without it TestPiEnvDenyExtensionLoads skips, and CI passes with the extension never loaded (issue #548)", want.run, step.num, requiredNodeName, requiredNodeValue)
	case got != requiredNodeValue:
		r.fail("the step that runs %s (line %d) sets %s to %s, want %s exactly, as release.yml writes it (issue #548)", want.run, step.num, requiredNodeName, got, requiredNodeValue)
	}
}

// envValue returns the value of name in env, an env: mapping's lines, as
// written on its line, quotes kept; its keys are upper case, which the mapping
// reader refuses.
func envValue(env []yamlread.Line, name string) (string, bool) {
	for _, l := range env {
		if key, value, ok := strings.Cut(l.Text, ":"); ok && key == name {
			return strings.TrimSpace(value), true
		}
	}

	return "", false
}
