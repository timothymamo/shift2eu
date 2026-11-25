package check

import (
	"errors"
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

	// nodeVersionPattern is the form of dev/lima.yaml's nodeVersion line, the
	// one pin of the Node the VM installs for pi: two spaces and the double
	// quotes, the version in the one group. It is written so that sed -E and
	// Go's regexp read it alike, and both the workflows' sed (nodeVersionRead)
	// and PinnedNodeVersion, through which pienvdenyload_test.go's
	// piNodeVersionPin parses the same line, are built from it.
	nodeVersionPattern = `^  nodeVersion: "([^"]*)"$`

	// nodeVersionRead is the line that makes a step the read of dev/lima.yaml's
	// nodeVersion.
	nodeVersionRead = `version="$(sed -nE 's/` + nodeVersionPattern + `/\1/p' dev/lima.yaml)"`

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

// nodeVersionLine reads dev/lima.yaml's nodeVersion line as the workflows' sed
// does, from the same pattern.
var nodeVersionLine = regexp.MustCompile(`(?m)` + nodeVersionPattern)

// PinnedNodeVersion returns the Node release the dev/lima.yaml document lima
// pins, read from its nodeVersion line as ci.yml's and release.yml's Test jobs
// read it (issue #554), and an error when it holds no such line or pins an
// empty version. It does not check the X.Y.Z form: the workflows' read step
// does that before it installs the version.
func PinnedNodeVersion(lima []byte) (string, error) {
	match := nodeVersionLine.FindSubmatch(lima)
	if match == nil {
		return "", errors.New(`dev/lima.yaml holds no line   nodeVersion: "X", two spaces and the double quotes`)
	}

	if len(match[1]) == 0 {
		return "", errors.New("dev/lima.yaml's nodeVersion is empty")
	}

	return string(match[1]), nil
}

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

// ciKeys is a closed key set of ci.yml: the keys a mapping may hold, and why
// the ones an edit is most likely to add are refused by name.
type ciKeys struct {
	// where names the mapping in a message: "test job" or "top level".
	where string
	// allowed are the only keys the mapping may hold, in the order a message
	// lists them.
	allowed []string
	// why says, for a refused key, what it would hide; a key not in why is
	// refused with otherWhy.
	why      map[string]string
	otherWhy string
}

// ciTestJobKeys closes ci.yml's test job to the keys it holds: name, runs-on
// and steps (issue #554). Any other key decides whether the job runs, whether
// its failure fails the run, or what its run: steps run in: if: and needs: can
// skip it, continue-on-error: lets it fail unseen, and defaults: (a shell such
// as true {0}) or env: (BASH_ENV, ENV) can make every run: step a no-op while
// CI stays green.
var ciTestJobKeys = ciKeys{
	where:   "test job",
	allowed: []string{"name", "runs-on", "steps"},
	why: map[string]string{
		"if":                "a condition on the job can turn it off, and CI passes with TestPiEnvDenyExtensionLoads never run",
		"continue-on-error": "a Test job that fails without failing the run leaves CI green with the tests broken",
		"needs":             "a job it needs can be skipped or fail, and the Test job is skipped with it while CI stays green",
		"defaults":          "a default shell such as true {0} makes every run: step a no-op while CI stays green",
		"env":               "BASH_ENV or ENV name a script the shell sources before every run: step, which can make the steps no-ops",
	},
	otherWhy: "a key beyond these can skip the job or change what its run: steps run in, and this check does not read it",
}

// ciTopLevelKeys closes ci.yml's top level to the keys it holds (issue
// #554): a defaults: or an env: there reaches the Test job's steps whatever
// the job itself declares.
var ciTopLevelKeys = ciKeys{
	where:   "top level",
	allowed: []string{"name", "on", "permissions", "concurrency", "jobs"},
	why: map[string]string{
		"defaults": "a default shell such as true {0} reaches every job's run: steps, the Test job's among them, and makes them no-ops while CI stays green",
		"env":      "BASH_ENV or ENV there name a script the shell sources before every run: step of every job, the Test job's among them",
	},
	otherWhy: "a key beyond these can change how every job runs, and this check does not read it",
}

// ciStep is one step of ci.yml's test job, as CITestJobNode reads it.
type ciStep struct {
	// item is the step as the steps: list holds it, which schemaReader.step
	// compares with the declaration.
	item yamlread.Item
	// num is the line the step's item starts on.
	num int
	// entries are the step's keys, in the order written.
	entries []yamlread.Entry
	// uses is the action the step runs, without the version comment beside
	// it; "" for a run: step.
	uses string
	// run is the step's command as written, a block's lines joined by
	// newlines; "" for none. It recognises the step and feeds the named
	// diagnostics; schemaReader.step reads it exactly.
	run string
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
// that order, the tests after the check. The comparison is schemaReader.step's,
// the one release.yml's steps go through, with a run: written as |- or |+ read
// as |. Beside it, each part an edit could break is named on its own, the
// messages the cases pin: the steps' keys beyond the declaration's; the read's
// id, its X.Y.Z check
// and its output; setup-node's pin by commit, its version from the read's
// with: mapping and its cache off; the check's exit 1 inside its if and its
// NODE_VERSION from the read's output; and SHIFT2EU_REQUIRE_NODE: "1" on the
// tests. The job itself holds only name, runs-on and steps, and the workflow's
// top level only name, on, permissions, concurrency and jobs, so no if:,
// needs:, continue-on-error:, defaults: or env: decides whether the job runs
// or turns its run: steps into no-ops; both key sets are read with quotes
// stripped and whatever form the value takes (issue #554). Unlike
// release.yml's, ci.yml's steps are not a closed schema: the job holds the
// other steps every pull request runs.
func CITestJobNode(src []byte) []string {
	r := &schemaReader{raw: rawLines(src), chompedRun: true}

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", ciFile, err)}
	}

	r.ciClosedKeys(lines, ciTopLevelKeys)

	jobsBlock, err := yamlread.Walk(lines, "jobs")
	if err != nil {
		r.fail("%s: %v", ciFile, err)

		return r.messages
	}

	jobs, err := yamlread.Mapping(jobsBlock)
	if err != nil {
		r.fail("%s: jobs: %v", ciFile, err)

		return r.messages
	}

	i := slices.IndexFunc(jobs, func(e yamlread.Entry) bool { return e.Key == ciTestJob })
	if i < 0 {
		r.fail("%s holds no test job under jobs:, so no CI run runs the tests the load check is in (issue #548)", ciFile)

		return r.messages
	}

	job := jobs[i]

	r.ciClosedKeys(job.Block, ciTestJobKeys)

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

// ciClosedKeys refuses every key of the mapping lines that keys does not
// allow. It reads every line at the indentation of the mapping's own keys, a
// quoted key with its quotes stripped and a key in whatever form its value
// takes, on its line, in flow form or in a block below: the mapping reader
// refuses a quoted key as one it does not understand, and would not name the
// key it hides. A line at that indentation it cannot read as a key is refused
// whole, so what it does not understand fails rather than passes.
func (r *schemaReader) ciClosedKeys(lines []yamlread.Line, keys ciKeys) {
	if len(lines) == 0 {
		return
	}

	indent := lines[0].Indent
	allowed := strings.Join(keys.allowed, ", ")

	for _, l := range lines {
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

		if slices.Contains(keys.allowed, name) {
			continue
		}

		why, ok := keys.why[name]
		if !ok {
			why = keys.otherWhy
		}

		r.fail("ci.yml's %s holds %s: (line %d), want only %s: %s (issues #548, #554)", keys.where, name, l.Num, allowed, why)
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

		step := ciStep{item: it, num: it.Num, entries: entries}
		step.uses = step.scalar("uses")

		if e, ok := step.entry("run"); ok {
			step.run = ciRunText(e)
		}

		steps = append(steps, step)
	}

	return steps, true
}

// ciRunText returns a step's run: as written, for the step to be recognised
// and its named diagnostics read: a block's lines joined by newlines, whatever
// its indicator, or the one scalar on the key's line. It reads nothing
// exactly: schemaReader.step compares the run: with the declaration, and
// refuses a value it cannot read as the script it stands for.
func ciRunText(e yamlread.Entry) string {
	if len(e.Block) > 0 {
		texts := make([]string, 0, len(e.Block))
		for _, l := range e.Block {
			texts = append(texts, l.Text)
		}

		return strings.Join(texts, "\n")
	}

	if value, err := yamlread.Single(e); err == nil {
		return value
	}

	return e.Value
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
// Each step it finds goes through schemaReader.step, the comparison
// release.yml's steps go through, and then through its named diagnostics.
func (r *schemaReader) ciNodeSteps(steps []ciStep, want []releaseStep) {
	readWant, setupWant, checkWant, testsWant := want[0], want[1], want[2], want[3]

	// compare holds steps[i] to w as release.yml's schema holds its steps.
	compare := func(i int, w releaseStep) {
		r.step(steps[i].item, fmt.Sprintf("%s: jobs: %s: step %d", ciFile, ciTestJob, i+1), w)
	}

	read := r.ciOne(steps, "whose run reads dev/lima.yaml's nodeVersion, the one pin of the Node the VM installs for pi",
		func(s ciStep) bool { return strings.Contains(s.run, nodeVersionRead) })
	if read >= 0 {
		compare(read, readWant)
		r.ciRead(steps[read], readWant)
	}

	setup := r.ciOne(steps, "that run actions/setup-node, the action that installs the pinned Node",
		func(s ciStep) bool { return strings.HasPrefix(s.uses, "actions/setup-node@") })
	if setup >= 0 {
		compare(setup, setupWant)
		r.ciSetupNode(steps[setup], setupWant)
	}

	nodeCheck := r.ciOne(steps, "whose run checks that node on the PATH is the pinned one",
		func(s ciStep) bool { return strings.Contains(s.run, nodeCheckPrefix) })
	if nodeCheck >= 0 {
		compare(nodeCheck, checkWant)
		r.ciNodeCheck(steps[nodeCheck], checkWant)
	}

	tests := r.ciOne(steps, "that run "+testsWant.run+", the step the tests run in",
		func(s ciStep) bool { return slices.Contains(s.runLines(), testsWant.run) })
	if tests >= 0 {
		compare(tests, testsWant)
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

// ciExtraKeys names every key of step, which is what, beyond want's: if: and
// continue-on-error: above all turn the step off or let it fail unseen. The
// keys themselves are schemaReader.step's to compare; this is the named
// diagnostic the cases pin, which says why such a key is refused.
func (r *schemaReader) ciExtraKeys(what string, step ciStep, want releaseStep) {
	keys := want.keys()

	for _, e := range step.entries {
		if !slices.Contains(keys, e.Key) {
			r.fail("ci.yml's test job's %s (line %d) holds the key %q, want exactly %s: a key beyond these, if: and continue-on-error: above all, turns the step off or lets it fail unseen (issue #548)", what, step.num, e.Key, strings.Join(keys, ", "))
		}
	}
}

// ciRead names the parts of the step that reads dev/lima.yaml's nodeVersion
// an edit is likely to break. Its X.Y.Z range check and its output line
// check refuse nothing the exact run comparison of schemaReader.step does not
// refuse already: they are only named diagnostics, which say what the missing
// line was for. The id check is one too, beside step's exact id.
func (r *schemaReader) ciRead(step ciStep, want releaseStep) {
	r.ciExtraKeys("step that reads dev/lima.yaml's nodeVersion", step, want)

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

// ciSetupNode names the parts of the actions/setup-node step an edit is
// likely to break: its pin by a commit, its with: mapping's keys and its
// package-manager cache off. The version taken from the read's output is
// schemaReader.step's to compare.
func (r *schemaReader) ciSetupNode(step ciStep, want releaseStep) {
	r.ciExtraKeys("actions/setup-node step", step, want)

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
		if _, ok := values[key]; !ok {
			r.fail("actions/setup-node (line %d): with: holds no %s, want exactly %s (issue #548)", step.num, key, strings.Join(withKeys, " and "))
		}
	}

	if w, ok := values["package-manager-cache"]; ok {
		if got, err := yamlread.Single(w); err == nil && got != packageManagerCache {
			r.fail("actions/setup-node (line %d): with: package-manager-cache is %q, want %q: no package manager runs in this job, so its cache would save and restore what nothing reads (issue #548)", w.Num, got, packageManagerCache)
		}
	}
}

// ciNodeCheck checks the step that compares node --version with the version
// setup-node installed, and fails when they differ.
func (r *schemaReader) ciNodeCheck(step ciStep, want releaseStep) {
	r.ciExtraKeys("step that checks node", step, want)

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
	r.ciExtraKeys("step that runs "+want.run, step, want)

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
