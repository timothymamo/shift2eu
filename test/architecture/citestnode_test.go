// Pins ci.yml's Test job to the Node TestPiEnvDenyExtensionLoads has to run on
// (issue #548, ADR 0044).
//
// The load check in pienvdenyload_test.go cannot see the one failure that costs
// it its whole point: on a runner without the Node that strips TypeScript types
// it skips, and unless SHIFT2EU_REQUIRE_NODE=1 tells it to fail instead, CI
// passes with the extension never loaded. So the Test job must install the Node
// dev/lima.yaml pins, check that the node on the PATH is that one, and set
// SHIFT2EU_REQUIRE_NODE=1 on the step that runs the tests. release.yml is held
// to that by test/architecture/internal/check's closed schema
// (releaseworkflow.go); ci.yml's Node steps are held here, so an edit that drops
// one of them fails a check instead of silently skipping the load on every pull
// request.
package architecture_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The parts of ci.yml's Test job this pin holds, each as the workflow writes it.
const (
	// ciNodeVersionSed is the line that makes a step the read of dev/lima.yaml's
	// nodeVersion, the one pin of the Node the VM installs for pi: two spaces
	// and the double quotes, exactly as pienvdenyload_test.go's
	// piNodeVersionPin parses the same line.
	ciNodeVersionSed = `version="$(sed -nE 's/^  nodeVersion: "([^"]*)"$/\1/p' dev/lima.yaml)"`

	// ciNodeVersionRange is the strict check that what the sed read is one
	// X.Y.Z release and nothing else, which is what a skip or a failure names.
	ciNodeVersionRange = `if ! [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then`

	// ciNodeVersionOutput is the line that writes the version to the step's
	// output, where actions/setup-node takes it from.
	ciNodeVersionOutput = `echo "version=${version}" >> "${GITHUB_OUTPUT}"`

	// ciNodeCheckPrefix starts the line that makes a step the check that the
	// node on the PATH is the pinned one, and ciNodeCheckLine is that line
	// whole: the version it compares against comes from the read's output.
	ciNodeCheckPrefix = `if [ "$(node --version)" !=`
	ciNodeCheckLine   = `if [ "$(node --version)" != "v${NODE_VERSION}" ]; then`

	// ciTestsRun is the command of the step the tests run in: the one that
	// carries SHIFT2EU_REQUIRE_NODE=1.
	ciTestsRun = "make cover"
)

// ciSetupNodeWith is the whole with: mapping of actions/setup-node: the version
// the read step wrote to its output, and the package-manager cache off, since no
// package manager runs in the job.
var ciSetupNodeWith = []string{"node-version", "package-manager-cache"}

// The whole key set of each of the four steps, as ci.yml writes it. A key
// outside one of these - if: and continue-on-error: above all - turns the step
// off or lets it fail without the checks below reading either, so the set is
// closed rather than merely read key by key.
var (
	ciReadStepKeys      = []string{"name", "id", "run"}
	ciSetupNodeStepKeys = []string{"uses", "with"}
	ciNodeCheckStepKeys = []string{"name", "env", "run"}
	ciTestsStepKeys     = []string{"name", "env", "run"}
)

// ciTestJobKeys are the two keys the test job itself must not hold: either one
// decides whether the job runs and whether its failure fails the run.
var ciTestJobKeys = []struct {
	key string
	// why says what the key would hide.
	why string
}{
	{"if", "a condition on the job can turn it off, and CI passes with TestPiEnvDenyExtensionLoads never run"},
	{"continue-on-error", "a Test job that fails without failing the run leaves CI green with the tests broken"},
}

// ciSetupNodePin matches the reference actions/setup-node is pinned to: a commit
// and nothing else, so a moved tag or range cannot decide what CI runs.
var ciSetupNodePin = regexp.MustCompile(`^actions/setup-node@[0-9a-f]{40}$`)

// ciCITestStep is one step of ci.yml's Test job, as this pin reads it.
type ciCITestStep struct {
	// num is the line the step's item starts on.
	num int
	// id is the step's id:, "" for none.
	id string
	// uses is the action the step runs, without its commit and without the
	// version comment beside it, "" for a run: step.
	uses string
	// run is the step's command: its run: value when that is one line, or the
	// lines of its block scalar joined by newlines; "" for a step without one.
	run string
	// lines are the step's own content lines.
	lines []ciLine
}

// ciTestJobCheck collects what ci.yml's Test job gets wrong, one message per
// way the Node steps stop holding the load check.
type ciTestJobCheck struct {
	steps    []ciCITestStep
	messages []string
}

// fail records a message.
func (c *ciTestJobCheck) fail(format string, args ...any) {
	c.messages = append(c.messages, fmt.Sprintf(format, args...))
}

// one returns the index of the one step keep selects, and records a message
// when there is no such step or more than one: a second step of the same kind is
// a way to add one this pin does not read. It returns -1 when it recorded a
// message, so the caller checks the rest of that step only when there is one.
func (c *ciTestJobCheck) one(what string, keep func(ciCITestStep) bool) int {
	var found []int

	for i, step := range c.steps {
		if keep(step) {
			found = append(found, i)
		}
	}

	if len(found) == 1 {
		return found[0]
	}

	c.fail("ci.yml's test job holds %d steps %s, want exactly 1 (issue #548)", len(found), what)

	return -1
}

// ciTestJobMessages returns one message for every way ci.yml's test job no
// longer sets up the Node dev/lima.yaml pins for TestPiEnvDenyExtensionLoads and
// no longer makes that test fail rather than skip: a step that reads the
// nodeVersion out of dev/lima.yaml with a strict X.Y.Z check and writes it to its
// output; actions/setup-node, pinned by a commit, installing exactly that output
// with its package-manager cache off and nothing else; a step that checks the
// node on the PATH against it; and, in that order and not after the tests, the
// step that runs make cover with SHIFT2EU_REQUIRE_NODE: "1" in its env. Each of
// those steps' key sets is closed and the job holds neither if: nor
// continue-on-error:, so a key outside them fails here rather than turning a
// step off.
func ciTestJobMessages(doc []byte) []string {
	jobs, err := workflowJobs(string(doc))
	if err != nil {
		return []string{".github/workflows/ci.yml: " + err.Error()}
	}

	job, ok := jobs["test"]
	if !ok {
		return []string{".github/workflows/ci.yml holds no test job under jobs:, so no CI run runs the tests the load check is in (issue #548)"}
	}

	block, ok := ciBlock(job, "steps")
	if !ok {
		return []string{".github/workflows/ci.yml's test job holds no steps: list, so it runs no test (issue #548)"}
	}

	items, err := ciItems(block)
	if err != nil {
		return []string{fmt.Sprintf(".github/workflows/ci.yml's test job's steps: %v", err)}
	}

	steps := make([]ciCITestStep, 0, len(items))

	for _, item := range items {
		_, uses, _ := ciScalar(item.lines, "uses")

		// The version comment Renovate writes beside the commit is not part of
		// the reference the action runs at.
		action, _, _ := strings.Cut(uses, " #")

		_, id, _ := ciScalar(item.lines, "id")

		steps = append(steps, ciCITestStep{num: item.num, id: id, uses: action, run: ciStepRun(item.lines), lines: item.lines})
	}

	c := &ciTestJobCheck{steps: steps}

	c.jobKeys(job)

	read := c.one("whose run reads dev/lima.yaml's nodeVersion, the one pin of the Node the VM installs for pi",
		func(s ciCITestStep) bool { return strings.Contains(s.run, ciNodeVersionSed) })

	// versionExpr is the expression through which the steps below take the
	// version from the read's output. It is empty while the read step holds no
	// id:, which is reported once, and the steps that would read it are not
	// checked against an expression nobody can write.
	versionExpr := ""

	if read >= 0 {
		step := steps[read]

		c.stepKeys("step that reads dev/lima.yaml's nodeVersion", step, ciReadStepKeys...)

		if step.id == "" {
			c.fail("the step that reads dev/lima.yaml's nodeVersion (line %d) holds no id:, so actions/setup-node cannot take the version from its output (issue #548)", step.num)
		} else {
			versionExpr = "${{ steps." + step.id + ".outputs.version }}"
		}

		if !strings.Contains(step.run, ciNodeVersionRange) {
			c.fail("the step that reads dev/lima.yaml's nodeVersion (line %d) does not check that it is one X.Y.Z release (%s): a value the sed reads but dev/lima.yaml does not pin exactly would be installed, and the version a skip names would be no release (issue #548)", step.num, ciNodeVersionRange)
		}

		if !strings.Contains(step.run, ciNodeVersionOutput) {
			c.fail("the step that reads dev/lima.yaml's nodeVersion (line %d) does not write the version to its step output (%s), so actions/setup-node has nothing to take the version from (issue #548)", step.num, ciNodeVersionOutput)
		}
	}

	setup := c.one("that run actions/setup-node, the action that installs the pinned Node",
		func(s ciCITestStep) bool { return strings.HasPrefix(s.uses, "actions/setup-node@") })

	if setup >= 0 {
		c.setupNode(versionExpr, steps[setup])
	}

	check := c.one("whose run checks that node on the PATH is the pinned one",
		func(s ciCITestStep) bool { return strings.Contains(s.run, ciNodeCheckPrefix) })

	if check >= 0 {
		c.nodeCheck(versionExpr, steps[check])
	}

	tests := c.one("that run make cover, the step the tests run in",
		func(s ciCITestStep) bool {
			return slices.ContainsFunc(strings.Split(s.run, "\n"), func(line string) bool {
				return strings.TrimSpace(line) == ciTestsRun
			})
		})

	if tests >= 0 {
		c.requiredNode(steps[tests])
	}

	// The order the four steps depend on: setup-node takes its version from the
	// read's output, the check reads the node setup-node installed, and the
	// tests run on the node that was checked.
	for _, pair := range []struct {
		what string
		a, b int
	}{
		{"the step that reads dev/lima.yaml's nodeVersion (line %d) comes after actions/setup-node (line %d), want it before: setup-node takes the version from that step's output (issue #548)", read, setup},
		{"actions/setup-node (line %d) comes after the step that checks node (line %d), want it before: the check reads the node actions/setup-node installed (issue #548)", setup, check},
		{"the step that checks node (line %d) comes after the step that runs make cover (line %d), want it before: TestPiEnvDenyExtensionLoads runs on the node it checked (issue #548)", check, tests},
	} {
		if pair.a >= 0 && pair.b >= 0 && pair.a > pair.b {
			c.fail(pair.what, steps[pair.a].num, steps[pair.b].num)
		}
	}

	return c.messages
}

// jobKeys refuses the job-level keys that decide whether the test job runs and
// whether its failure fails the run.
func (c *ciTestJobCheck) jobKeys(job []ciLine) {
	for _, k := range ciTestJobKeys {
		if num, _, ok := ciScalar(job, k.key); ok {
			c.fail("ci.yml's test job holds %s: (line %d), want none: %s (issue #548)", k.key, num, k.why)
		}
	}
}

// stepKeys closes the key set of one step: it records a message for every key
// the step holds that is not in want, and for every wanted key it lacks. A step
// whose keys are read one by one passes an if: or a continue-on-error: it never
// looks at.
func (c *ciTestJobCheck) stepKeys(what string, step ciCITestStep, want ...string) {
	keys, err := ciKeys(step.lines)
	if err != nil {
		c.fail("ci.yml's test job's %s (line %d): %v", what, step.num, err)

		return
	}

	for _, key := range keys {
		if !slices.Contains(want, key.name) {
			c.fail("ci.yml's test job's %s (line %d) holds the key %q, want exactly %s: a key beyond these, if: and continue-on-error: above all, turns the step off or lets it fail unseen (issue #548)", what, step.num, key.name, strings.Join(want, ", "))
		}
	}

	for _, name := range want {
		if !slices.ContainsFunc(keys, func(key ciKey) bool { return key.name == name }) {
			c.fail("ci.yml's test job's %s (line %d) holds no %s, want exactly %s (issue #548)", what, step.num, name, strings.Join(want, ", "))
		}
	}
}

// setupNode checks the actions/setup-node step: pinned by a commit, its with:
// mapping exactly ciSetupNodeWith, the version taken from versionExpr, and its
// package-manager cache off.
func (c *ciTestJobCheck) setupNode(versionExpr string, step ciCITestStep) {
	c.stepKeys("actions/setup-node step", step, ciSetupNodeStepKeys...)

	if !ciSetupNodePin.MatchString(step.uses) {
		c.fail("actions/setup-node (line %d) uses %q, want actions/setup-node@<40 hex digits>, the commit it is pinned to: a tag or a range can be moved under CI (issue #548)", step.num, step.uses)
	}

	with, ok := ciBlock(step.lines, "with")
	if !ok {
		c.fail("actions/setup-node (line %d) holds no with: mapping, want exactly %s (issue #548)", step.num, strings.Join(ciSetupNodeWith, " and "))

		return
	}

	keys, err := ciKeys(with)
	if err != nil {
		c.fail("actions/setup-node (line %d): with: %v", step.num, err)
	}

	for _, key := range keys {
		if !slices.Contains(ciSetupNodeWith, key.name) {
			c.fail("actions/setup-node (line %d): with: holds the key %q, want exactly %s (issue #548)", step.num, key.name, strings.Join(ciSetupNodeWith, " and "))
		}
	}

	for _, name := range ciSetupNodeWith {
		if _, _, ok := ciScalar(with, name); !ok {
			c.fail("actions/setup-node (line %d): with: holds no %s, want exactly %s (issue #548)", step.num, name, strings.Join(ciSetupNodeWith, " and "))
		}
	}

	if num, got, ok := ciScalar(with, "package-manager-cache"); ok && got != "false" {
		c.fail("actions/setup-node (line %d): with: package-manager-cache is %q, want \"false\": no package manager runs in this job, so its cache would save and restore what nothing reads (issue #548)", num, got)
	}

	if num, got, ok := ciScalar(with, "node-version"); ok && versionExpr != "" && got != versionExpr {
		c.fail("actions/setup-node (line %d): with: node-version is %q, want %q, the version the step that reads dev/lima.yaml wrote to its output: a literal is a version dev/lima.yaml does not pin, and the VM's pin must stay the only one (issue #548)", num, got, versionExpr)
	}
}

// nodeCheck checks the step that compares node --version with the version
// setup-node installed, in versionExpr, and fails when they differ.
func (c *ciTestJobCheck) nodeCheck(versionExpr string, step ciCITestStep) {
	c.stepKeys("step that checks node", step, ciNodeCheckStepKeys...)

	if !strings.Contains(step.run, ciNodeCheckLine) {
		c.fail("the step that checks node (line %d) holds no line %s, want it to compare node --version with the version actions/setup-node installed, not with a literal and not with nothing (issue #548)", step.num, ciNodeCheckLine)
	}

	// After the compare, the branch has to fail: the exact line exit 1, and the
	// fi that closes the if straight after it, as release.yml's schema holds it.
	// A substring match would take an exit 10 and an exit 1 outside the branch.
	rest := strings.Split(step.run, "\n")

	if i := slices.Index(rest, ciNodeCheckLine); i >= 0 {
		rest = rest[i+1:]
	}

	switch exit := slices.Index(rest, "exit 1"); {
	case exit < 0:
		c.fail("the step that checks node (line %d) never fails: it holds no line exit 1 after its compare line, so a node that is not the pinned one passes the check that was meant to catch it (issue #548)", step.num)
	case exit+1 >= len(rest) || rest[exit+1] != "fi":
		c.fail("the step that checks node (line %d) holds its exit 1 where the fi that closes the if does not follow it, so the exit runs outside the branch that watches node --version (issue #548)", step.num)
	}

	env, ok := ciBlock(step.lines, "env")
	if !ok {
		c.fail("the step that checks node (line %d) holds no env: block, want NODE_VERSION: <the version setup-node installed> (issue #548)", step.num)

		return
	}

	num, got, ok := ciScalar(env, "NODE_VERSION")
	switch {
	case !ok:
		c.fail("the step that checks node (line %d) sets no NODE_VERSION in its env:, want the version actions/setup-node installed (issue #548)", step.num)
	case versionExpr != "" && got != versionExpr:
		c.fail("the step that checks node (line %d) sets NODE_VERSION to %q, want %q, the version the step that reads dev/lima.yaml wrote to its output (issue #548)", num, got, versionExpr)
	}
}

// requiredNode checks that the step the tests run in sets
// SHIFT2EU_REQUIRE_NODE: "1" exactly, as release.yml writes it:
// TestPiEnvDenyExtensionLoads fails rather than skips on 1, and the quoted form
// is the style the schema holds, a bare 1 being turned into the same string by
// GitHub Actions.
func (c *ciTestJobCheck) requiredNode(step ciCITestStep) {
	c.stepKeys("step that runs make cover", step, ciTestsStepKeys...)

	env, ok := ciBlock(step.lines, "env")
	if !ok {
		c.fail("the step that runs make cover (line %d) holds no env:, want SHIFT2EU_REQUIRE_NODE: \"1\" exactly: without it TestPiEnvDenyExtensionLoads skips, and CI passes with the extension never loaded (issue #548)", step.num)

		return
	}

	switch num, got, ok := ciScalar(env, "SHIFT2EU_REQUIRE_NODE"); {
	case !ok:
		c.fail("the step that runs make cover (line %d) sets no SHIFT2EU_REQUIRE_NODE in its env:, want \"1\" exactly: without it TestPiEnvDenyExtensionLoads skips, and CI passes with the extension never loaded (issue #548)", step.num)
	case got != `"1"`:
		c.fail("the step that runs make cover (line %d) sets SHIFT2EU_REQUIRE_NODE to %s, want \"1\" exactly, as release.yml writes it (issue #548)", num, got)
	}
}

// TestCITestJobPinsTheNodeTheExtensionLoadRuns pins that ci.yml's Test job sets
// up dev/lima.yaml's Node, checks that it is the one on the PATH, and runs the
// tests with SHIFT2EU_REQUIRE_NODE=1, in that order (issue #548). The cases
// break each of those in turn, as an edit could, and each names the message its
// break has to produce, so the check cannot pass the workflow by finding
// nothing.
func TestCITestJobPinsTheNodeTheExtensionLoadRuns(t *testing.T) {
	t.Parallel()

	ci := string(readRepositoryFile(t, ".github/workflows/ci.yml"))

	// in returns ci with old replaced by new, once. A replacement that no longer
	// matches fails the test: the case would otherwise pin the workflow as it
	// already is.
	in := func(old, new string) string {
		t.Helper()

		if !strings.Contains(ci, old) {
			t.Fatalf(".github/workflows/ci.yml holds no %q, so the case that changes it would pin nothing (issue #548)", old)
		}

		return strings.Replace(ci, old, new, 1)
	}

	// withoutStep returns ci with the step holding marker removed.
	withoutStep := func(marker string) string {
		return withoutCIStep(t, ci, marker)
	}

	// inStep returns ci with `key: value` added to the step holding marker, at
	// the indentation of the step's own keys: the case of an edit that gives the
	// step one more key.
	inStep := func(marker, key, value string) string {
		t.Helper()

		lines := strings.Split(ci, "\n")

		start, _ := ciStepRange(t, lines, marker)
		keyLine := strings.Repeat(" ", ciLineIndent(lines[start])+2) + key + ": " + value

		return strings.Join(slices.Concat(lines[:start+1], []string{keyLine}, lines[start+1:]), "\n")
	}

	// jobLines returns ci with lines added to the test job, each at the
	// indentation of the job's own keys, in the order given: the case of an
	// edit that gives the job keys ciScalar cannot read on one line, such as a
	// quoted key or a key whose value sits on the line below.
	jobLines := func(lines ...string) string {
		t.Helper()

		added := make([]string, 0, len(lines))
		for _, line := range lines {
			added = append(added, "    "+line)
		}

		return in("  test:\n", "  test:\n"+strings.Join(added, "\n")+"\n")
	}

	// jobKey returns ci with `key: value` added to the test job, at the
	// indentation of the job's own keys.
	jobKey := func(key, value string) string {
		t.Helper()

		return jobLines(key + ": " + value)
	}

	// movedAfter returns ci with the step holding marker moved after the step
	// holding anchor.
	movedAfter := func(marker, anchor string) string {
		t.Helper()

		return ciStepMovedAfter(t, ci, marker, anchor)
	}

	// setupNodeUses returns ci with the commit actions/setup-node is pinned to
	// replaced by ref, the case following an edit that took the pin back to a
	// tag, its version comment kept.
	setupNodeUses := func(ref string) string {
		t.Helper()

		uses, comment, ok := strings.Cut(ciSetupNodeLine(t, ci), " # ")
		if !ok {
			t.Fatalf(".github/workflows/ci.yml's actions/setup-node line holds no version comment, which an edit that re-pins it keeps (issue #421)")
		}

		if pinned := strings.TrimPrefix(uses, "uses: "); !ciSetupNodePin.MatchString(pinned) {
			t.Fatalf(".github/workflows/ci.yml pins actions/setup-node as %q, so the case that re-pins it would pin nothing (issue #548)", pinned)
		}

		return strings.Replace(ci, uses, "uses: "+ref+" # "+comment, 1)
	}

	const (
		// noEnv is the env: block of the step the tests run in.
		noEnv = "        env:\n          SHIFT2EU_REQUIRE_NODE: \"1\"\n"

		// nodeVersion is the with: line through which setup-node takes the
		// version from the read's output.
		nodeVersion = "          node-version: ${{ steps.node.outputs.version }}\n"

		// requiredNode is the line that makes the load check fail rather than
		// skip.
		requiredNode = `          SHIFT2EU_REQUIRE_NODE: "1"`
	)

	cases := []struct {
		name string
		// workflow is ci.yml, or a copy with one part broken.
		workflow string
		// wantIn is a part of a message the check has to return; "" means no
		// message at all.
		wantIn string
	}{
		{name: "ci.yml as it is"},
		{name: "CRLF line ends", workflow: strings.ReplaceAll(ci, "\n", "\r\n")},

		// The step that reads dev/lima.yaml's nodeVersion: the pin of the Node,
		// and the output the two steps after it take the version from.
		{
			name:     "no step reading dev/lima.yaml's nodeVersion",
			workflow: withoutStep(ciNodeVersionSed),
			wantIn:   "holds 0 steps whose run reads dev/lima.yaml's nodeVersion",
		},
		{
			name:     "the read without its id",
			workflow: in("        id: node\n", ""),
			wantIn:   "holds no id:, so actions/setup-node cannot take the version from its output",
		},
		{
			name:     "the read without its X.Y.Z check",
			workflow: in(ciNodeVersionRange, `if [ -z "${version}" ]; then`),
			wantIn:   "does not check that it is one X.Y.Z release",
		},
		{
			name:     "the read without its output",
			workflow: in(ciNodeVersionOutput, `echo "${version}"`),
			wantIn:   "does not write the version to its step output",
		},
		{
			// A block scalar's indicator may carry a chomping or an indentation
			// flag: the step is still the one whose run reads the nodeVersion.
			name:     "the read whose script strips its line ends",
			workflow: in("        id: node\n        run: |\n", "        id: node\n        run: |-\n"),
		},
		{
			name:     "the read with a key the pin does not read",
			workflow: inStep(ciNodeVersionSed, "timeout-minutes", "5"),
			wantIn:   `holds the key "timeout-minutes", want exactly name, id, run`,
		},

		// actions/setup-node: the pinned Node installed, from the read's output,
		// with no cache.
		{
			name:     "no actions/setup-node step",
			workflow: withoutStep("package-manager-cache: false"),
			wantIn:   "holds 0 steps that run actions/setup-node",
		},
		{
			name:     "actions/setup-node pinned by a tag",
			workflow: setupNodeUses("actions/setup-node@v7"),
			wantIn:   "want actions/setup-node@<40 hex digits>",
		},
		{
			name:     "actions/setup-node on a literal version",
			workflow: in(nodeVersion, "          node-version: lts/*\n"),
			wantIn:   `with: node-version is "lts/*"`,
		},
		{
			name:     "actions/setup-node with its package-manager cache on",
			workflow: in("          package-manager-cache: false\n", "          package-manager-cache: true\n"),
			wantIn:   `package-manager-cache is "true", want "false"`,
		},
		{
			name:     "actions/setup-node taking a version through the cache key too",
			workflow: in("          package-manager-cache: false\n", "          package-manager-cache: false\n          cache: npm\n"),
			wantIn:   `with: holds the key "cache"`,
		},
		{
			name:     "actions/setup-node without the cache key",
			workflow: in("          package-manager-cache: false\n", ""),
			wantIn:   "with: holds no package-manager-cache",
		},
		{
			name:     "actions/setup-node the job skips",
			workflow: inStep("actions/setup-node@", "if", "false"),
			wantIn:   `holds the key "if", want exactly uses, with`,
		},

		// The check that the node on the PATH is the pinned one.
		{
			name:     "no check of the node on the PATH",
			workflow: withoutStep(ciNodeCheckPrefix),
			wantIn:   "holds 0 steps whose run checks that node on the PATH is the pinned one",
		},
		{
			name:     "the check comparing another version",
			workflow: in(ciNodeCheckLine, `if [ "$(node --version)" != "v${GITHUB_REF}" ]; then`),
			wantIn:   "want it to compare node --version with the version actions/setup-node installed",
		},
		{
			name: "the check that never fails",
			workflow: in(
				"            echo \"::error file=.github/workflows/ci.yml::node on the PATH is $(node --version), want v${NODE_VERSION}\"\n            exit 1\n",
				"            echo \"::error file=.github/workflows/ci.yml::node on the PATH is $(node --version), want v${NODE_VERSION}\"\n            true\n"),
			wantIn: "never fails: it holds no line exit 1",
		},
		{
			name: "the check whose exit is a 10",
			workflow: in(
				"            echo \"::error file=.github/workflows/ci.yml::node on the PATH is $(node --version), want v${NODE_VERSION}\"\n            exit 1\n",
				"            echo \"::error file=.github/workflows/ci.yml::node on the PATH is $(node --version), want v${NODE_VERSION}\"\n            exit 10\n"),
			wantIn: "never fails: it holds no line exit 1",
		},
		{
			name:     "the check whose exit sits outside the if",
			workflow: in("            exit 1\n          fi\n      # SHIFT2EU_REQUIRE_NODE=1", "          fi\n          exit 1\n      # SHIFT2EU_REQUIRE_NODE=1"),
			wantIn:   "so the exit runs outside the branch",
		},
		{
			name:     "the check tolerating a failure",
			workflow: inStep(ciNodeCheckPrefix, "continue-on-error", "true"),
			wantIn:   `holds the key "continue-on-error", want exactly name, env, run`,
		},

		// The step the tests run in: SHIFT2EU_REQUIRE_NODE=1, so the load check
		// fails rather than skips.
		{
			name:     "the step the tests run in without its env",
			workflow: in(noEnv, ""),
			wantIn:   `holds no env:, want SHIFT2EU_REQUIRE_NODE: "1" exactly`,
		},
		{
			name:     "the step the tests run in with another variable alone",
			workflow: in(requiredNode, `          SHIFT2EU_TEST_OTHER: "1"`),
			wantIn:   "sets no SHIFT2EU_REQUIRE_NODE in its env:",
		},
		{
			name:     "the step the tests run in with SHIFT2EU_REQUIRE_NODE off",
			workflow: in("SHIFT2EU_REQUIRE_NODE: \"1\"\n        run: make cover\n", "SHIFT2EU_REQUIRE_NODE: \"0\"\n        run: make cover\n"),
			wantIn:   `sets SHIFT2EU_REQUIRE_NODE to "0", want "1" exactly`,
		},
		{
			name:     "the step the tests run in with SHIFT2EU_REQUIRE_NODE unquoted",
			workflow: in("SHIFT2EU_REQUIRE_NODE: \"1\"\n        run: make cover\n", "SHIFT2EU_REQUIRE_NODE: 1\n        run: make cover\n"),
			wantIn:   `sets SHIFT2EU_REQUIRE_NODE to 1, want "1" exactly, as release.yml writes it`,
		},
		{
			name:     "the step the tests run in that never runs",
			workflow: inStep("run: make cover", "if", "false"),
			wantIn:   `holds the key "if", want exactly name, env, run`,
		},
		{
			name:     "a tests job with a condition",
			workflow: jobKey("if", "false"),
			wantIn:   "ci.yml's test job holds if:",
		},
		{
			name:     "a tests job whose failure the run ignores",
			workflow: jobKey("continue-on-error", "true"),
			wantIn:   "ci.yml's test job holds continue-on-error:",
		},
		{
			// YAML lets a key's value sit on the line below it: the key is the
			// same one, and a job turned off this way must be refused just the
			// same.
			name:     "a tests job whose condition sits on the next line",
			workflow: jobLines("if:", "  false"),
			wantIn:   "ci.yml's test job holds if:",
		},
		{
			// A quoted key is the same key to YAML, and a key ciScalar does not
			// match: the pin reads the name with the quotes stripped.
			name:     "a tests job that quotes continue-on-error",
			workflow: jobLines(`"continue-on-error": true`),
			wantIn:   "ci.yml's test job holds continue-on-error:",
		},
		{
			name:     "no step running make cover",
			workflow: in("        run: make cover\n", "        run: make build\n"),
			wantIn:   "holds 0 steps that run make cover",
		},

		// The order the four depend on.
		{
			name:     "the read after actions/setup-node",
			workflow: movedAfter(ciNodeVersionSed, "package-manager-cache: false"),
			wantIn:   "want it before: setup-node takes the version from that step's output",
		},
		{
			name:     "actions/setup-node after the check",
			workflow: movedAfter("package-manager-cache: false", ciNodeCheckPrefix),
			wantIn:   "want it before: the check reads the node actions/setup-node installed",
		},
		{
			name:     "the check after the tests",
			workflow: movedAfter(ciNodeCheckPrefix, "run: make cover"),
			wantIn:   "want it before: TestPiEnvDenyExtensionLoads runs on the node it checked",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workflow := tc.workflow
			if workflow == "" {
				workflow = ci
			}

			messages := ciTestJobMessages([]byte(workflow))

			if tc.wantIn == "" {
				for _, message := range messages {
					t.Errorf("ciTestJobMessages(ci.yml) = %q, want no message: the Test job must set up dev/lima.yaml's Node for TestPiEnvDenyExtensionLoads and run the tests with SHIFT2EU_REQUIRE_NODE=1 (issue #548)", message)
				}

				return
			}

			if !slices.ContainsFunc(messages, func(message string) bool { return strings.Contains(message, tc.wantIn) }) {
				t.Errorf("ciTestJobMessages(the %s copy of ci.yml) = %q, want a message holding %q: this is the break the case makes, so a check that does not name it does not see it (issue #548)", tc.name, messages, tc.wantIn)
			}
		})
	}
}

// ciStepRun returns the command of a step: its run: value when that is one line,
// or the lines of its block scalar joined by newlines; "" for a step that runs
// none. Any value starting with | or > opens a block scalar, |-, |+, >- and >+
// as much as | and >.
func ciStepRun(lines []ciLine) string {
	if _, value, ok := ciScalar(lines, "run"); ok && !strings.HasPrefix(value, "|") && !strings.HasPrefix(value, ">") {
		return value
	}

	block, ok := ciBlock(lines, "run")
	if !ok {
		return ""
	}

	script := make([]string, 0, len(block))

	for _, l := range block {
		script = append(script, l.text)
	}

	return strings.Join(script, "\n")
}

// ciLineIndent returns the number of spaces before a raw line's text.
func ciLineIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// ciStepRange returns the first and the last+1 line of the step holding marker
// in lines: from the item's own line, the nearest line above that starts an item
// at the marker's indentation or less, to the next item or the first line
// shallower than the item. It fails the test when no line holds marker.
func ciStepRange(t *testing.T, lines []string, marker string) (start, end int) {
	t.Helper()

	hit := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, marker) })
	if hit < 0 {
		t.Fatalf("no line holds %q, so the case that moves or removes the step holding it would pin nothing (issue #548)", marker)
	}

	indent := ciLineIndent(lines[hit])

	start = hit
	for ; start > 0; start-- {
		line := lines[start]

		if line == "" {
			continue
		}

		if trimmed := strings.TrimLeft(line, " "); strings.HasPrefix(trimmed, "- ") && ciLineIndent(line) <= indent {
			break
		}
	}

	itemIndent := ciLineIndent(lines[start])

	end = len(lines)

	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}

		if ciLineIndent(line) < itemIndent {
			end = i

			break
		}

		if ciLineIndent(line) == itemIndent && strings.HasPrefix(strings.TrimLeft(line, " "), "- ") {
			end = i

			break
		}
	}

	return start, end
}

// withoutCIStep returns doc with the step holding marker removed: the case of an
// edit that drops it.
func withoutCIStep(t *testing.T, doc, marker string) string {
	t.Helper()

	lines := strings.Split(doc, "\n")

	start, end := ciStepRange(t, lines, marker)

	return strings.Join(slices.Concat(lines[:start], lines[end:]), "\n")
}

// ciStepMovedAfter returns doc with the step holding marker moved after the step
// holding anchor: the case of an edit that puts a step in the wrong place,
// without changing it.
func ciStepMovedAfter(t *testing.T, doc, marker, anchor string) string {
	t.Helper()

	lines := strings.Split(doc, "\n")

	start, end := ciStepRange(t, lines, marker)
	step := slices.Clone(lines[start:end])
	rest := slices.Concat(lines[:start], lines[end:])

	_, after := ciStepRange(t, rest, anchor)

	return strings.Join(slices.Concat(rest[:after], step, rest[after:]), "\n")
}

// ciSetupNodeLine returns the uses: part of the actions/setup-node line, from
// uses: on, with its version comment.
func ciSetupNodeLine(t *testing.T, doc string) string {
	t.Helper()

	for line := range strings.SplitSeq(doc, "\n") {
		if _, uses, ok := strings.Cut(line, "uses: actions/setup-node@"); ok {
			return strings.TrimSpace("uses: actions/setup-node@" + uses)
		}
	}

	t.Fatalf(".github/workflows/ci.yml holds no uses: actions/setup-node@…, so the case that re-pins it would pin nothing (issue #548)")

	return ""
}
