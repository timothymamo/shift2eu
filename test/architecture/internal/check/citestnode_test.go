// Pins ci.yml's Test job to the Node TestPiEnvDenyExtensionLoads has to run on
// (issue #548, ADR 0044), as release.yml's Test job is pinned by
// releaseworkflow.go: both jobs set up the Node dev/lima.yaml pins, check that
// it is the node on the PATH, and run the tests with SHIFT2EU_REQUIRE_NODE=1.
//
// The load check in pienvdenyload_test.go cannot see the one failure that costs
// it its whole point: on a runner without the Node that strips TypeScript types
// it skips, and unless SHIFT2EU_REQUIRE_NODE=1 tells it to fail instead, CI
// passes with the extension never loaded. So ci.yml's Test job must install the
// Node dev/lima.yaml pins, check that the node on the PATH is that one, and set
// SHIFT2EU_REQUIRE_NODE=1 on the step that runs the tests (issue #548). One
// declaration of those steps holds both workflows (issue #554): this file pins
// the refusals of check.CITestJobNode against the real ci.yml and the edits an
// attack or a careless edit could make to it, and releaseworkflow_test.go pins
// the same refusals for release.yml.
package check_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// The parts of ci.yml's Test job the cases below break, each as the workflow
// writes it.
const (
	// ciNodeVersionSed is the line that makes a step the read of dev/lima.yaml's
	// nodeVersion, the one pin of the Node the VM installs for pi: two spaces
	// and the double quotes, exactly as pienvdenyload_test.go's
	// piNodeVersionPin parses the same line.
	ciNodeVersionSed = `version="$(sed -nE 's/^  nodeVersion: "([^"]*)"$/\1/p' dev/lima.yaml)"`

	// ciNodeCheckPrefix and ciNodeCheckLine start and make up the line that
	// makes a step the check that the node on the PATH is the pinned one.
	ciNodeCheckPrefix = `if [ "$(node --version)" !=`
	ciNodeCheckLine   = `if [ "$(node --version)" != "v${NODE_VERSION}" ]; then`

	// ciNodeCheckFailure is the error line and the exit 1 under the check's
	// compare: the branch has to fail, not warn.
	ciNodeCheckFailure = `            echo "::error file=.github/workflows/ci.yml::node on the PATH is $(node --version), want v${NODE_VERSION}"` + "\n" +
		"            exit 1\n"

	// ciTestsStepEnv is the env: of the step the tests run in: the one that
	// carries SHIFT2EU_REQUIRE_NODE=1.
	ciTestsStepEnv = "        env:\n          SHIFT2EU_REQUIRE_NODE: \"1\"\n"

	// ciRequiredNode is the line that makes the load check fail rather than
	// skip.
	ciRequiredNode = `          SHIFT2EU_REQUIRE_NODE: "1"`

	// ciNodeVersionLine is the with: line through which setup-node takes the
	// version from the read's output.
	ciNodeVersionLine = "          node-version: ${{ steps.node.outputs.version }}\n"

	// ciPackageManagerCache is the with: line that keeps setup-node's package
	// manager cache off.
	ciPackageManagerCache = "          package-manager-cache: false\n"

	// ciSetupNodeWith is the whole with: mapping of actions/setup-node.
	ciSetupNodeWith = "        with:\n" + ciNodeVersionLine + ciPackageManagerCache
)

// TestCITestJobNode pins that check.CITestJobNode refuses every edit that would
// leave ci.yml's Test job no longer setting up dev/lima.yaml's Node for
// TestPiEnvDenyExtensionLoads, no longer checking that it is the node on the
// PATH, or no longer running the tests with SHIFT2EU_REQUIRE_NODE=1 - and
// returns nothing for the workflow as ci.yml has it (issue #554). The cases
// break each part in turn, as an edit could, and each names the message its
// break has to produce, so the check cannot pass the workflow by finding
// nothing.
func TestCITestJobNode(t *testing.T) {
	t.Parallel()

	ci, _ := readWorkflows(t)
	doc := string(ci)

	// in returns the workflow with old replaced by new, once. A replacement
	// that no longer matches fails the test: the case would otherwise pin the
	// workflow as it already is.
	in := func(old, new string) string {
		t.Helper()

		if !strings.Contains(doc, old) {
			t.Fatalf(".github/workflows/ci.yml holds no %q, so the case that changes it would pin nothing (issue #554)", old)
		}

		return strings.Replace(doc, old, new, 1)
	}

	// withoutStep returns the workflow with the step holding marker removed.
	withoutStep := func(marker string) string {
		t.Helper()

		return ciDocWithoutStep(t, doc, marker)
	}

	// inStep returns the workflow with `key: value` added to the step holding
	// marker, at the indentation of the step's own keys: the case of an edit
	// that gives the step one more key.
	inStep := func(marker, key, value string) string {
		t.Helper()

		return ciDocStepWithKey(t, doc, marker, key, value)
	}

	// jobLines returns the workflow with lines added to the test job, each at
	// the indentation of the job's own keys, in the order given: the case of an
	// edit that gives the job keys a one-line reader cannot see, such as a
	// quoted key or a key whose value sits on the line below.
	jobLines := func(lines ...string) string {
		t.Helper()

		return ciDocJobLines(t, doc, lines...)
	}

	// jobKey returns the workflow with `key: value` added to the test job.
	jobKey := func(key, value string) string {
		t.Helper()

		return jobLines(key + ": " + value)
	}

	// movedAfter returns the workflow with the step holding marker moved after
	// the step holding anchor.
	movedAfter := func(marker, anchor string) string {
		t.Helper()

		return ciDocStepMovedAfter(t, doc, marker, anchor)
	}

	// setupNodeUses returns the workflow with the commit actions/setup-node is
	// pinned to replaced by ref, the version comment kept: the case following
	// an edit that took the pin back to a tag.
	setupNodeUses := func(ref string) string {
		t.Helper()

		line := ciSetupNodeLine(t, doc)

		_, comment, ok := strings.Cut(line, " # ")
		if !ok {
			t.Fatalf(".github/workflows/ci.yml's actions/setup-node line %q holds no version comment, which an edit that re-pins it keeps (issue #554)", line)
		}

		return in(line, "uses: "+ref+" # "+comment)
	}

	cases := []struct {
		name string
		// workflow is ci.yml, or a copy with one part broken; "" for ci.yml as
		// it is.
		workflow string
		// wantIn is a part of a message the check has to return; "" means no
		// message at all.
		wantIn string
	}{
		{name: "ci.yml as it is"},
		{name: "CRLF line ends", workflow: strings.ReplaceAll(doc, "\n", "\r\n")},

		// The job itself: a missing job, a missing steps: list, and the two keys
		// that decide whether it runs and whether its failure fails the run.
		{
			name:     "no test job",
			workflow: in("  test:\n", "  tests:\n"),
			wantIn:   "holds no test job under jobs:",
		},
		{
			name:     "a test job without its steps",
			workflow: in("  test:\n    name: Test\n    runs-on: ubuntu-latest\n    steps:\n", "  test:\n    name: Test\n    runs-on: ubuntu-latest\n"),
			wantIn:   "holds no steps: list, so it runs no test",
		},
		{
			name:     "a test job with a condition",
			workflow: jobKey("if", "false"),
			wantIn:   "ci.yml's test job holds if:",
		},
		{
			name:     "a test job whose failure the run ignores",
			workflow: jobKey("continue-on-error", "true"),
			wantIn:   "ci.yml's test job holds continue-on-error:",
		},
		{
			// YAML lets a key's value sit on the line below it: the key is the
			// same one, and a job turned off this way must be refused just the
			// same.
			name:     "a test job whose condition sits on the next line",
			workflow: jobLines("if:", "  false"),
			wantIn:   "ci.yml's test job holds if:",
		},
		{
			// A quoted key is the same key to YAML, and a key a one-line reader
			// does not match: the name with the quotes stripped is the one.
			name:     "a test job that quotes continue-on-error",
			workflow: jobLines(`"continue-on-error": true`),
			wantIn:   "ci.yml's test job holds continue-on-error:",
		},

		// The step that reads dev/lima.yaml's nodeVersion: the pin of the Node,
		// and the output the two steps after it take the version from.
		{
			name:     "no step reading dev/lima.yaml's nodeVersion",
			workflow: withoutStep(ciNodeVersionSed),
			wantIn:   "holds 0 steps whose run reads dev/lima.yaml's nodeVersion",
		},
		{
			name:     "the read of another file",
			workflow: in(ciNodeVersionSed, `version="$(sed -nE 's/^  nodeVersion: "([^"]*)"$/\1/p' dev/other.yaml)"`),
			wantIn:   "holds 0 steps whose run reads dev/lima.yaml's nodeVersion",
		},
		{
			name:     "the read with another sed pattern",
			workflow: in(ciNodeVersionSed, `version="$(sed -nE 's/^ nodeVersion: "([^"]*)"$/\1/p' dev/lima.yaml)"`),
			wantIn:   "holds 0 steps whose run reads dev/lima.yaml's nodeVersion",
		},
		{
			name:     "the read with a hard-coded version",
			workflow: in(ciNodeVersionSed, `version="1.2.3"`),
			wantIn:   "holds 0 steps whose run reads dev/lima.yaml's nodeVersion",
		},
		{
			name:     "the read without its id",
			workflow: in("        id: node\n", ""),
			wantIn:   "holds no id:, so actions/setup-node cannot take the version from its output",
		},
		{
			name:     "the read without its X.Y.Z check",
			workflow: in(`if ! [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then`, `if [ -z "${version}" ]; then`),
			wantIn:   "does not check that it is one X.Y.Z release",
		},
		{
			name:     "the read without its output",
			workflow: in(`echo "version=${version}" >> "${GITHUB_OUTPUT}"`, `echo "${version}"`),
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
		{
			name:     "the read the job skips",
			workflow: inStep(ciNodeVersionSed, "if", "false"),
			wantIn:   `holds the key "if", want exactly name, id, run`,
		},
		{
			name:     "the read tolerating a failure",
			workflow: inStep(ciNodeVersionSed, "continue-on-error", "true"),
			wantIn:   `holds the key "continue-on-error", want exactly name, id, run`,
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
			workflow: in(ciNodeVersionLine, "          node-version: lts/*\n"),
			wantIn:   `with: node-version is "lts/*"`,
		},
		{
			name:     "actions/setup-node with its package-manager cache on",
			workflow: in(ciPackageManagerCache, "          package-manager-cache: true\n"),
			wantIn:   `package-manager-cache is "true", want "false"`,
		},
		{
			name:     "actions/setup-node taking a version through the cache key too",
			workflow: in(ciPackageManagerCache, ciPackageManagerCache+"          cache: npm\n"),
			wantIn:   `with: holds the key "cache"`,
		},
		{
			name:     "actions/setup-node without the cache key",
			workflow: in(ciPackageManagerCache, ""),
			wantIn:   "with: holds no package-manager-cache",
		},
		{
			name:     "actions/setup-node without its with:",
			workflow: in(ciSetupNodeWith, ""),
			wantIn:   "holds no with: mapping",
		},
		{
			name:     "actions/setup-node the job skips",
			workflow: inStep("actions/setup-node@", "if", "false"),
			wantIn:   `holds the key "if", want exactly uses, with`,
		},
		{
			name:     "actions/setup-node tolerating a failure",
			workflow: inStep("actions/setup-node@", "continue-on-error", "true"),
			wantIn:   `holds the key "continue-on-error", want exactly uses, with`,
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
			name:     "the check that never fails",
			workflow: in(ciNodeCheckFailure, strings.TrimSuffix(ciNodeCheckFailure, "exit 1\n")+"true\n"),
			wantIn:   "never fails: it holds no line exit 1",
		},
		{
			name:     "the check whose exit is a 10",
			workflow: in(ciNodeCheckFailure, strings.TrimSuffix(ciNodeCheckFailure, "exit 1\n")+"exit 10\n"),
			wantIn:   "never fails: it holds no line exit 1",
		},
		{
			name:     "the check whose exit sits outside the if",
			workflow: in(ciNodeCheckFailure+"          fi\n", "          fi\n          exit 1\n"),
			wantIn:   "so the exit runs outside the branch",
		},
		{
			name:     "the check without its env",
			workflow: in("        env:\n          NODE_VERSION: ${{ steps.node.outputs.version }}\n", ""),
			wantIn:   "holds no env: block, want NODE_VERSION",
		},
		{
			name:     "the check against a literal version",
			workflow: in("          NODE_VERSION: ${{ steps.node.outputs.version }}\n", "          NODE_VERSION: 1.2.3\n"),
			wantIn:   "sets NODE_VERSION to",
		},
		{
			name:     "the check the job skips",
			workflow: inStep(ciNodeCheckPrefix, "if", "false"),
			wantIn:   `holds the key "if", want exactly name, env, run`,
		},
		{
			name:     "the check tolerating a failure",
			workflow: inStep(ciNodeCheckPrefix, "continue-on-error", "true"),
			wantIn:   `holds the key "continue-on-error", want exactly name, env, run`,
		},

		// The step the tests run in: SHIFT2EU_REQUIRE_NODE=1, so the load check
		// fails rather than skips.
		{
			name:     "no step running make cover",
			workflow: in("        run: make cover\n", "        run: make build\n"),
			wantIn:   "holds 0 steps that run make cover",
		},
		{
			name:     "the step the tests run in without its env",
			workflow: in(ciTestsStepEnv, ""),
			wantIn:   `holds no env:, want SHIFT2EU_REQUIRE_NODE: "1" exactly`,
		},
		{
			name:     "the step the tests run in with another variable alone",
			workflow: in(ciRequiredNode, `          SHIFT2EU_TEST_OTHER: "1"`),
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
			name:     "the step the tests run in tolerating a failure",
			workflow: inStep("run: make cover", "continue-on-error", "true"),
			wantIn:   `holds the key "continue-on-error", want exactly name, env, run`,
		},

		// The order the four steps depend on.
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
				workflow = doc
			}

			messages := check.CITestJobNode([]byte(workflow))

			if tc.wantIn == "" {
				for _, message := range messages {
					t.Errorf("CITestJobNode(ci.yml) = %q, want no message: the Test job must set up dev/lima.yaml's Node for TestPiEnvDenyExtensionLoads and run the tests with SHIFT2EU_REQUIRE_NODE=1 (issue #554)", message)
				}

				return
			}

			if !slices.ContainsFunc(messages, func(message string) bool { return strings.Contains(message, tc.wantIn) }) {
				t.Errorf("CITestJobNode(the %s copy of ci.yml) = %q, want a message holding %q: this is the break the case makes, so a check that does not name it does not see it (issue #554)", tc.name, messages, tc.wantIn)
			}
		})
	}
}

// ciDocIndent returns the number of spaces before a raw line's text.
func ciDocIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// ciDocStepRange returns the first and the last+1 line of the step holding
// marker in lines: from the item's own line, the nearest line above that starts
// an item at the marker's indentation or less, to the next item or the first
// line shallower than the item. It fails the test when no line holds marker.
func ciDocStepRange(t *testing.T, lines []string, marker string) (start, end int) {
	t.Helper()

	hit := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, marker) })
	if hit < 0 {
		t.Fatalf("no line holds %q, so the case that moves or removes the step holding it would pin nothing (issue #554)", marker)
	}

	indent := ciDocIndent(lines[hit])

	start = hit
	for ; start > 0; start-- {
		line := lines[start]

		if line == "" {
			continue
		}

		if trimmed := strings.TrimLeft(line, " "); strings.HasPrefix(trimmed, "- ") && ciDocIndent(line) <= indent {
			break
		}
	}

	itemIndent := ciDocIndent(lines[start])

	end = len(lines)

	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}

		if ciDocIndent(line) < itemIndent {
			end = i

			break
		}

		if ciDocIndent(line) == itemIndent && strings.HasPrefix(strings.TrimLeft(line, " "), "- ") {
			end = i

			break
		}
	}

	return start, end
}

// ciDocWithoutStep returns doc with the step holding marker removed: the case of
// an edit that drops it.
func ciDocWithoutStep(t *testing.T, doc, marker string) string {
	t.Helper()

	lines := strings.Split(doc, "\n")

	start, end := ciDocStepRange(t, lines, marker)

	return strings.Join(slices.Concat(lines[:start], lines[end:]), "\n")
}

// ciDocStepWithKey returns doc with `key: value` added to the step holding
// marker, at the indentation of the step's own keys: the case of an edit that
// gives the step one more key.
func ciDocStepWithKey(t *testing.T, doc, marker, key, value string) string {
	t.Helper()

	lines := strings.Split(doc, "\n")

	start, _ := ciDocStepRange(t, lines, marker)
	keyLine := strings.Repeat(" ", ciDocIndent(lines[start])+2) + key + ": " + value

	return strings.Join(slices.Concat(lines[:start+1], []string{keyLine}, lines[start+1:]), "\n")
}

// ciDocStepMovedAfter returns doc with the step holding marker moved after the
// step holding anchor: the case of an edit that puts a step in the wrong place,
// without changing it.
func ciDocStepMovedAfter(t *testing.T, doc, marker, anchor string) string {
	t.Helper()

	lines := strings.Split(doc, "\n")

	start, end := ciDocStepRange(t, lines, marker)
	step := slices.Clone(lines[start:end])
	rest := slices.Concat(lines[:start], lines[end:])

	_, after := ciDocStepRange(t, rest, anchor)

	return strings.Join(slices.Concat(rest[:after], step, rest[after:]), "\n")
}

// ciDocJobLines returns doc with lines added to ci.yml's Test job, each at the
// indentation of the job's own keys, in the order given.
func ciDocJobLines(t *testing.T, doc string, lines ...string) string {
	t.Helper()

	added := make([]string, 0, len(lines))
	for _, line := range lines {
		added = append(added, "    "+line)
	}

	if !strings.Contains(doc, "  test:\n") {
		t.Fatalf(".github/workflows/ci.yml holds no test job, so the case that gives it keys would pin nothing (issue #554)")
	}

	return strings.Replace(doc, "  test:\n", "  test:\n"+strings.Join(added, "\n")+"\n", 1)
}

// ciSetupNodeLine returns the uses: part of ci.yml's actions/setup-node line,
// from uses: on, with its version comment: the one substring a case that
// re-pins the action replaces.
func ciSetupNodeLine(t *testing.T, doc string) string {
	t.Helper()

	for line := range strings.SplitSeq(doc, "\n") {
		if _, uses, ok := strings.Cut(line, "uses: actions/setup-node@"); ok {
			return strings.TrimSpace("uses: actions/setup-node@" + uses)
		}
	}

	t.Fatalf(".github/workflows/ci.yml holds no uses: actions/setup-node@..., so the case that re-pins it would pin nothing (issue #554)")

	return ""
}
