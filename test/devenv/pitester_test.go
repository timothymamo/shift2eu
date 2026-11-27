// Pins the tester's launch wrapper dev/pi-tester.sh (issue #662, ADR 0044): .dot-agent-deck.toml's tester role
// calls it instead of pi, so the tester starts with only what it needs - its DeepSeek credential and the
// non-secret basics - and never with another agent's key or a GitHub token (CLAUDE.md rule 3). The wrapper builds
// pi's environment from an ALLOW LIST, not a deny list: a variable it does not name is absent whatever its name,
// and SHIFT2EU_FAKE_SECRET_662 is the name no list of credentials knows, so its absence is what tells the two
// apart.
//
// The wrapper also fails closed. pi logs an extension that fails to load and carries on without it, so the
// env-printing refusal (ADR 0044) fails open on its own: before it starts pi, the wrapper checks the two files
// the refusal is made of in the directory it is installed in (issue #692), never the unit folder's copies, and
// exits non-zero naming the installed file when one is missing, empty or not JSON - pi is not started then at
// all.
//
// The wrapper also marks its process tree with SHIFT2EU_PI_TESTER=1. The VM's login profile
// (/etc/profile.d/shift2eu.sh, dev/lima.yaml) sources the agents' keys file with `set -a` into every login
// shell, and a `bash -l` from the tester's bash tool would put every key back in the tester's pane: the
// profile leaves the file unsourced when that marker is set and non-empty.
// TestLimaLoginProfileSkipsTheKeysOfTheTester runs the snippet dev/lima.yaml writes, against a fake HOME, and
// pins the guard by what a child shell inherits.
//
// The wrapper loads the refusal's two files with node before pi starts (round 2 of issue #662): the extension
// is a TypeScript module only a node that strips types can import, and its top level is checked by loading it,
// while every pattern of the list is compiled with `new RegExp`, so a file that throws, a pattern that is no
// regular expression and a machine without node each stop the tester instead of leaving it without the refusal
// (ADR 0044 fails open on its own). The cases that exercise that load run the machine's real node and skip with
// a reason where there is none; every other case hands the wrapper a stand-in node that exits 0, since the
// wrapper reads node's exit status alone and a CI runner without node must still pin the rest.
//
// Every value here is invented (CLAUDE.md rule 3): the wrapper's tests hand it an environment they build
// themselves, never the machine's, and the fake pi records the names of the variables it saw and the value of
// the non-secret marker, never a value of a credential.
package devenv_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// piTesterScript is the launch wrapper the VM installs root-owned, and the two project files the env-printing
// refusal (ADR 0044) is made of. The installed layout is pinned in pitesterinstalled_test.go; these are the
// repository's copies, which dev/lima.yaml embeds and the installed copies must equal.
const (
	piTesterScript    = "dev/pi-tester.sh"
	denyExtensionFile = ".pi/extensions/deny-env-printing.ts"
	denyListFile      = ".pi/deny-env-printing.json"
)

// deckConfigPath is the deck's configuration, whose tester role names the command the wrapper is for.
const deckConfigPath = ".dot-agent-deck.toml"

// The deck's tester role, by name, and the agent it names: the wrapper's launcher name tells the deck nothing
// about the agent behind it, so the role's agent line is the only place that says pi (issue #691).
const (
	deckTesterRole  = "tester"
	deckTesterAgent = "pi"
)

// piTesterKept are the variables the wrapper must hand pi when the parent has them: pi's DeepSeek credential
// (docs/adr/0003-pi-with-deepseek-for-the-tester-role.md), the non-secret basics, and the three deck variables
// `dot-agent-deck work-done` reads to accept the tester's report (its pane id, that pane's capability and the
// daemon's socket), which the wrapper must pass on or the tester cannot report at all.
var piTesterKept = []string{
	"DEEPSEEK_API_KEY",
	"HOME",
	"PATH",
	"TERM",
	"LANG",
	"DOT_AGENT_DECK_PANE_ID",
	"DOT_AGENT_DECK_PANE_CAPABILITY",
	"DOT_AGENT_DECK_SOCKET",
}

// piTesterDropped are the variables the wrapper must keep out of pi's environment even when the parent has them:
// another agent's key, both GitHub token variables, the OpenTelemetry headers that carry a backend's API key,
// the deck's own telemetry switch, and a name no list of credentials knows. The last one is the point: a deny
// list keeps the others out by naming them, and only an allow list keeps out a name it never heard of.
var piTesterDropped = []string{
	"GH_TOKEN",
	"GITHUB_TOKEN",
	"ANTHROPIC_API_KEY",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"OTEL_EXPORTER_OTLP_HEADERS",
	"DOT_AGENT_DECK_PI_TELEMETRY",
	"SHIFT2EU_FAKE_SECRET_662",
}

// piTesterMarker is the non-secret marker the wrapper sets and hands pi (issue #662). The VM's login profile
// reads it and leaves the agents' keys file unsourced: a `bash -l` in the tester's pane would otherwise source
// every key the wrapper just kept out.
const piTesterMarker = "SHIFT2EU_PI_TESTER"

// piTesterMarkerValue is the value the wrapper gives the marker. Any non-empty value makes the profile script
// skip the keys, and the wrapper sets this one, so the two halves of the guard agree on one spelling.
const piTesterMarkerValue = "1"

// piTesterRequireNodeEnv is the variable CI sets where it has installed the pinned Node
// (test/architecture/pienvdeny_test.go's SHIFT2EU_REQUIRE_NODE): with it, a node this check cannot use is a
// failure rather than a skip, so no runner silently drops the load cases.
const piTesterRequireNodeEnv = "SHIFT2EU_REQUIRE_NODE"

// piTesterNodeProbeScript says, in one line of JSON, what node calls itself and whether it strips TypeScript
// types on import: the extension is a .ts file, and only a node with type stripping on by default can import it
// without a loader (test/architecture/pienvdeny_test.go probes it the same way, in the package that pins the
// extension itself).
const piTesterNodeProbeScript = `
process.stdout.write(JSON.stringify({
	version: process.version,
	strips: Boolean(process.features?.typescript),
}));
`

// piTesterArgs are the arguments .dot-agent-deck.toml's tester command passes the wrapper, which must reach pi
// unchanged. The command carries no `--approve` since issue #702: the wrapper starts pi with `--no-approve`, and a
// command that still passes `--approve` (as a unit cut before that merge does) has it dropped with one line
// (TestPiTesterDropsTheApproveArguments).
var piTesterArgs = []string{"--provider", "deepseek", "--model", "deepseek-flash", "--thinking", "high"}

// piTesterExecLine is the wrapper's last line since ADR 0044's amendment "the tester loads no project extension or
// setting" (issue #702): pi starts with no project trust and no discovered extension, loads the installed refusal
// and the deck's extension by --extension and the unit folder's skills by --skill, and the wrapper's own flags come
// before the deck's arguments, so a `--` among those cannot turn them into messages. dev/vm-pi-tester.sh accepts
// exactly this line.
const piTesterExecLine = `allowlisted exec "${pi}" --no-approve --no-extensions --extension "${extension}" --extension "${deck}" --skill "${skills}" "$@"`

// piTesterOldExecLine is the wrapper's last line before issue #702, which dev/vm-pi-tester.sh now refuses like any
// other last line: it started pi with project trust, so pi discovered and loaded the unit folder's
// .pi/extensions and read its .pi/settings.json.
const piTesterOldExecLine = `allowlisted exec "${pi}" --extension "${extension}" "$@"`

// piTesterDeckDir returns where the deck writes its pi extension, as the wrapper names it: under HOME, never under
// PI_CODING_AGENT_DIR, which the wrapper's allow list does not hand pi (issue #702). It is spelled as HOME is, since
// the wrapper and its refusal name the path they built, so a HOME carrying a `.` or a `//` shows through.
func piTesterDeckDir(home string) string {
	return home + "/.pi/agent/extensions/dot-agent-deck"
}

// piTesterAgentDir returns the directory the wrapper refuses when pi's own agent directory could be the unit
// folder's - ${HOME}/.pi/agent, where pi keeps the settings, the models and the system prompt it always trusts -
// spelled as HOME is, since the refusal names the path it built (issue #702).
func piTesterAgentDir(home string) string {
	return home + "/.pi/agent"
}

// writePiTesterAgentDir makes ${HOME}/.pi/agent a real directory, the layout the deck writes and the wrapper
// requires before it checks the deck's extension (issue #702).
func writePiTesterAgentDir(t *testing.T, home string) {
	t.Helper()

	if err := os.MkdirAll(piTesterAgentDir(home), 0o700); err != nil {
		t.Fatalf("create pi's agent directory %s: %v", piTesterAgentDir(home), err)
	}
}

// piTesterSkillsDir returns the skills directory the wrapper passes pi: the unit folder's .claude/skills, the
// directory .pi/settings.json used to load before issue #702.
func piTesterSkillsDir(project string) string {
	return filepath.Join(project, ".claude", "skills")
}

// writePiTesterDeck writes the deck's pi extension where the deck writes it - an index.ts under HOME - and returns
// its directory, which the wrapper requires before it starts pi (issue #702).
func writePiTesterDeck(t *testing.T, home string) string {
	t.Helper()

	dir := piTesterDeckDir(home)
	writeFileMode(t, filepath.Join(dir, "index.ts"), []byte("// the deck's pi extension, invented for this test (issue #702)\n"), 0o644)

	return dir
}

// resolvePiTesterDir returns dir with its symbolic links resolved, which is the directory the wrapper's shell
// reports as PWD and so what its --skill argument holds: t.TempDir() hands back a path under a symlink on macOS
// (/var is a symlink to /private/var), and the shell's PWD is the resolved one.
func resolvePiTesterDir(t *testing.T, dir string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s, the directory the wrapper runs in, so a case can compare the argv with the path the shell reports (issue #702): %v", dir, err)
	}

	return resolved
}

// fakePiScript is the stand-in for pi: it records its argv, the value of the non-secret marker and the names of
// the variables of its environment - never a value of a credential, which these tests invent anyway (CLAUDE.md
// rule 3) - and exits 0. The record's path is written into it, because the wrapper's environment is an allow
// list and cannot carry a variable for the fake.
const fakePiScript = `#!/bin/sh
# Stands in for pi in the tests of the tester's launch wrapper (issue #662). The marker's value is recorded as it
# is: it is not a secret.
{
	for a in "$@"; do printf 'arg=%%s\n' "$a"; done
	printf 'marker=%%s\n' "${%s-}"
	printf '%s\n'
	env | cut -d= -f1 | sort
} > '%s'
`

// fakeNodeScript stands in for node in every case that does not exercise the wrapper's load of the refusal's
// files: the wrapper reads node's exit status alone, so a node that exits 0 at once says the files are fine
// without loading one. It records the NAMES of the variables of its environment - never a value, which these
// tests invent anyway (CLAUDE.md rule 3) - one run per piNodeRecordRunEnd line, since the wrapper runs node once
// for the extension's load and once for the list's patterns, and what a run inherits is the audit's finding L-A
// (issue #662). The record's path is written into the script, and the marker line is piNodeRecordRunEnd. A case
// that pins what the wrapper does when node refuses writes its own node instead (TestPiTesterChecksTheDenyFilesWithNode
// runs the machine's real node).
const fakeNodeScript = `#!/bin/sh
# Stands in for node in the tests of the tester's launch wrapper (issue #662).
{
	env | cut -d= -f1 | sort
	printf 'run-end\n'
} >> '%s'
`

// piNodeRecordRunEnd ends one run's names in the fake node's record.
const piNodeRecordRunEnd = "run-end"

// piRecordEnvMarker separates the fake pi's argv and the marker from the names of the variables it saw.
const piRecordEnvMarker = "env-names:"

// piRecordMarkerPrefix opens the line on which the fake pi records the value of the non-secret marker.
const piRecordMarkerPrefix = "marker="

// piFile is one of the two files the env-printing refusal is made of, as a case leaves it: present with content,
// present as a directory, or not written at all.
type piFile struct {
	present bool
	// directory leaves the path a directory instead of a file, which is what a checkout that lost the file and
	// kept the directory looks like.
	directory bool
	content   string
}

// piRecord is what the fake pi saw: its argv, the value of the non-secret marker and the names of the variables
// of its environment.
type piRecord struct {
	argv     []string
	marker   string
	envNames []string
}

// readPiRecord parses what the fake pi wrote, failing the test when the wrapper never started it.
func readPiRecord(t *testing.T, path string) piRecord {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the wrapper did not start pi: %v", err)
	}

	var (
		record piRecord
		inEnv  bool
	)

	for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		switch {
		case line == piRecordEnvMarker:
			inEnv = true
		case inEnv:
			record.envNames = append(record.envNames, line)
		case strings.HasPrefix(line, piRecordMarkerPrefix):
			record.marker = strings.TrimPrefix(line, piRecordMarkerPrefix)
		case strings.HasPrefix(line, "arg="):
			record.argv = append(record.argv, strings.TrimPrefix(line, "arg="))
		}
	}

	return record
}

// piTesterFarmTools are the tools a farm directory holds a link to: the shell tools the wrapper and the fake pi
// run, and jq, which checks the list, unless the case leaves it out. node is deliberately none of them, so a
// case whose PATH is a farm sees no node whatever the machine has installed.
var piTesterFarmTools = []string{
	"sh", "env", "cat", "cksum", "cmp", "cut", "diff", "head", "md5sum", "sha256sum", "shasum", "tail", "tr", "sed", "grep", "sort", "mkdir", "mktemp",
	"readlink", "dirname", "basename", "wc",
}

// writePiTesterFarm fills dir with links to the tools above and, when jqPath is not empty, to jq, and returns it.
// A case whose PATH holds this directory and the fake pi's own is sure of what the wrapper can find: no node, or
// no jq, whatever the machine the test runs on keeps in its own PATH.
func writePiTesterFarm(t *testing.T, dir, jqPath string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the farm %s: %v", dir, err)
	}

	if jqPath != "" {
		if err := os.Symlink(jqPath, filepath.Join(dir, "jq")); err != nil {
			t.Fatalf("link %s into the farm %s: %v", jqPath, dir, err)
		}
	}

	for _, name := range piTesterFarmTools {
		path, err := exec.LookPath(name)
		if err != nil {
			// A machine without one of these cannot run the wrapper at all; the case fails on the wrapper's own
			// message when what it needs is not there, never on a link this helper could not make.
			continue
		}

		if err := os.Symlink(path, filepath.Join(dir, name)); err != nil {
			t.Fatalf("link %s into the farm %s: %v", path, dir, err)
		}
	}

	return dir
}

// piTesterRealNode returns the path of the node on this machine, and skips the test with a reason where there is
// none or it cannot import a TypeScript module: such a node would fail every load case for a reason that is not
// the wrapper's (test/architecture/pienvdeny_test.go draws the same line for the extension's own load).
// SHIFT2EU_REQUIRE_NODE=1, which CI sets where it has installed the pinned Node, turns the skip into a failure.
func piTesterRealNode(t *testing.T) string {
	t.Helper()

	nodePath, err := exec.LookPath("node")
	trouble := "node is not on the PATH"

	if err == nil {
		version, strips, probeErr := piTesterProbeNode(t, nodePath)
		switch {
		case probeErr != nil:
			trouble = fmt.Sprintf("%s could not say what it supports, which is a broken install: %v", nodePath, probeErr)
		case strips:
			return nodePath
		default:
			trouble = fmt.Sprintf("node %s does not strip TypeScript types on import", version)
		}
	}

	reason := fmt.Sprintf("%s, so this case cannot load %s as pi does; install a Node that strips types on import, the version dev/lima.yaml pins (type stripping is on by default from Node 22.18 / 23.6), or set %s=1 to fail instead of skipping (issue #662, ADR 0044)",
		trouble, denyExtensionFile, piTesterRequireNodeEnv)

	if os.Getenv(piTesterRequireNodeEnv) == "1" {
		t.Fatal(reason)
	}

	t.Skip(reason)

	return ""
}

// piTesterProbeNode asks node what it calls itself and whether it strips TypeScript types on import, through the
// probe script above and under an environment built here, never the machine's (CLAUDE.md rule 3).
func piTesterProbeNode(t *testing.T, nodePath string) (string, bool, error) {
	t.Helper()

	cmd := exec.Command(nodePath, "-e", piTesterNodeProbeScript)
	cmd.Env = envList(map[string]string{
		"PATH": filepath.Dir(nodePath) + ":/usr/bin:/bin",
		"HOME": t.TempDir(),
	})

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", false, fmt.Errorf("it exited with %w: %s", err, out)
	}

	var said struct {
		Version string `json:"version"`
		Strips  bool   `json:"strips"`
	}

	if err := json.Unmarshal(out, &said); err != nil {
		return "", false, fmt.Errorf("it answered %q, which is no JSON: %w", out, err)
	}

	return said.Version, said.Strips, nil
}

// piTesterSecrets is the list of invented values no stream of the wrapper may carry: every value of the
// environment the wrapper was handed that is a credential or an invented name (CLAUDE.md rule 3).
func piTesterSecrets() []secret {
	secrets := make([]secret, 0, len(piTesterKept)+len(piTesterDropped))

	for _, name := range slices.Concat(piTesterKept, piTesterDropped) {
		secrets = append(secrets, secret{kind: "value of " + name, value: fakeEnvValue(name)})
	}

	return secrets
}

// writeFakePi writes the stand-in for pi first on the wrapper's PATH, recording into record.
func writeFakePi(t *testing.T, binDir, record string) {
	t.Helper()

	script := fmt.Sprintf(fakePiScript, piTesterMarker, piRecordEnvMarker, record)
	writeFileMode(t, filepath.Join(binDir, "pi"), []byte(script), 0o700)
}

// writeFakeNode writes the stand-in for node first on the wrapper's PATH, recording the names of each run's
// environment into record.
func writeFakeNode(t *testing.T, binDir, record string) {
	t.Helper()

	writeFileMode(t, filepath.Join(binDir, "node"), []byte(fmt.Sprintf(fakeNodeScript, record)), 0o700)
}

// piTesterNodeRecord is where the fake node records the names of its environment, beside the fake pi's own record
// so that a case names one path.
func piTesterNodeRecord(piRecord string) string {
	return piRecord + "-node-env"
}

// writePiTesterBin writes the stand-ins a case that does not exercise the node load needs: the fake pi, and a
// node that records the names of its environment and exits 0 at once (fakeNodeScript).
func writePiTesterBin(t *testing.T, binDir, record string) {
	t.Helper()

	writeFakePi(t, binDir, record)
	writeFakeNode(t, binDir, piTesterNodeRecord(record))
}

// piTesterEnv is the environment the wrapper is handed, and so the only one pi could inherit: the parent values
// of the variables the wrapper keeps, invented values of the ones it must drop, so their absence from pi's
// environment is the wrapper's doing and not the test machine's (CLAUDE.md rule 3), and the PATH the case needs,
// with the fake pi's directory first so the fake wins over anything the machine has.
//
// path is what follows that directory: piTesterBasePath's jq and the standard directories, or a farm of links
// that leaves one tool of the wrapper's out (writePiTesterFarm).
func piTesterEnv(binDir, homeDir, path string) map[string]string {
	env := make(map[string]string, len(piTesterKept)+len(piTesterDropped))

	for _, name := range slices.Concat(piTesterKept, piTesterDropped) {
		env[name] = fakeEnvValue(name)
	}

	// The four the wrapper is handed values a pane really has: the PATH the case builds, pi's home, and the
	// terminal and locale the deck's terminal drop-in sets.
	env["PATH"] = binDir + ":" + path
	env["HOME"] = homeDir
	env["TERM"] = "xterm-256color"
	env["LANG"] = "C.UTF-8"

	return env
}

// piTesterJqPath returns the path of jq on this machine, and skips the test when there is none: the wrapper
// checks the list with it. Where jq lives is never hard-coded, so a Mac whose Homebrew keeps it outside /usr/bin
// runs these cases too.
func piTesterJqPath(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("jq")
	if err != nil {
		t.Skipf("jq is not installed, and the wrapper checks the env-printing refusal's list with it: install jq (dev/lima.yaml installs one) and run this again")
	}

	return path
}

// piTesterBasePath returns what a case puts on the wrapper's PATH after the fake pi's directory: where jq is,
// found on this machine, and the standard directories the shell's own tools live in.
func piTesterBasePath(t *testing.T) string {
	t.Helper()

	return filepath.Dir(piTesterJqPath(t)) + ":/usr/local/bin:/usr/bin:/bin"
}

// runPiTester runs the wrapper under sh, from dir - the directory pi starts in, where it loads the project - under
// env alone, so no variable of the machine the test runs on reaches it.
func runPiTester(t *testing.T, script, dir string, args []string, env map[string]string) (string, string, error) {
	t.Helper()

	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = envList(env)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// writePiTesterProject creates the directory pi starts in and writes the project's copies of the files the
// installed layout holds - the wrapper, the extension and the list - as the case leaves the two deny files.
// The wrapper is always a copy of dev/pi-tester.sh, so a case that does not weaken it gets no line about a copy
// that differs.
func writePiTesterProject(t *testing.T, root string, extension, list piFile) string {
	t.Helper()

	return writePiTesterUnit(t, root, extension, list, piFile{present: true, content: readRepoFile(t, piTesterScript)})
}

// TestPiTesterHandsPiAnAllowListedEnvironment pins the first half of issue #662: every argument reaches pi
// unchanged, the DeepSeek credential and the non-secret basics reach it, and every other credential the wrapper
// was handed does not - the unknown name above all, which only an allow list keeps out.
func TestPiTesterHandsPiAnAllowListedEnvironment(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	binDir := filepath.Join(root, "bin")
	homeDir := filepath.Join(root, "home")
	record := filepath.Join(root, "pi-record")

	for _, dir := range []string{binDir, homeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	writePiTesterBin(t, binDir, record)
	writePiTesterDeck(t, homeDir)

	installed := writePiTesterInstalled(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	project := writePiTesterProject(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, piTesterEnv(binDir, homeDir, piTesterBasePath(t)))
	if err != nil {
		t.Fatalf("the wrapper exited non-zero with the env-printing refusal's two files in place: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	got := readPiRecord(t, record)

	if diff := cmp.Diff(piTesterWantArgv(t, installed, homeDir, project, piTesterArgs), got.argv); diff != "" {
		t.Errorf("the wrapper did not start pi with the installed refusal and the deck's extension, with no project trust and no discovered extension, or did not pass its arguments unchanged, so the tester runs with another provider, model or mode than the deck's command names, without the refusal, or with the unit folder's extensions (-want +got):\n%s", diff)
	}

	for _, name := range piTesterKept {
		if !slices.Contains(got.envNames, name) {
			t.Errorf("pi's environment holds no %s, although the wrapper was handed one: pi cannot reach its DeepSeek credential, the basics it starts with, or the deck's pane variables `dot-agent-deck work-done` needs (issue #662, CLAUDE.md rule 3)", name)
		}
	}

	for _, name := range piTesterDropped {
		if slices.Contains(got.envNames, name) {
			t.Errorf("pi's environment holds %s, although the wrapper was handed it: the tester must start with only what it needs, so no GitHub token, no other agent's key, no telemetry header and no variable the wrapper does not know reaches it (issue #662, CLAUDE.md rule 3)", name)
		}
	}

	// The marker is what keeps a login shell from sourcing the agents' keys file back into the pane, so it is
	// set by the wrapper itself and handed to pi even though nothing in the parent environment held it
	// (dev/lima.yaml's /etc/profile.d/shift2eu.sh, TestLimaLoginProfileSkipsTheKeysOfTheTester).
	if !slices.Contains(got.envNames, piTesterMarker) {
		t.Errorf("pi's environment holds no %s, which the wrapper has to set itself: a `bash -l` from the tester's bash tool then sources every key of the agents' keys file back into the pane (issue #662, CLAUDE.md rule 3)", piTesterMarker)
	} else if got.marker != piTesterMarkerValue {
		t.Errorf("pi's environment holds %s=%q, want %q: the VM's login profile skips the keys file only for a non-empty marker, and says which value means the tester (issue #662)", piTesterMarker, got.marker, piTesterMarkerValue)
	}

	// Neither stream may carry a value of the environment the wrapper was handed: a wrapper that echoes what it
	// drops writes a credential into the terminal's scrollback, a CI log or a screenshot (CLAUDE.md rule 3).
	secrets := piTesterSecrets()

	assertNoSecrets(t, "the wrapper's stdout", stdout, secrets)
	assertNoSecrets(t, "the wrapper's stderr", stderr, secrets)
}

// readPiNodeRecord parses what the fake node recorded: the names of the variables of each of its runs, one run per
// piNodeRecordRunEnd line. It fails the test when the wrapper never ran node, since a case that pins what node's
// environment holds has to see a run.
func readPiNodeRecord(t *testing.T, path string) [][]string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the wrapper did not run node, although it loads the env-printing refusal's two files with it before pi starts (issue #662): %v", err)
	}

	var (
		runs    [][]string
		current []string
	)

	for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		switch line {
		case piNodeRecordRunEnd:
			runs = append(runs, current)
			current = nil
		case "":
		default:
			current = append(current, line)
		}
	}

	if len(current) > 0 {
		runs = append(runs, current)
	}

	return runs
}

// piTesterAllowedNames returns the variables the wrapper's allow list names, read from dev/pi-tester.sh: the block
// between `allowed="` and its closing quote, one name to a line. It fails the test when the script holds no such
// block, since then nothing here can tell an inherited variable from an allow-listed one.
func piTesterAllowedNames(t *testing.T) []string {
	t.Helper()

	const open = `allowed="`

	text := readRepoFile(t, piTesterScript)

	start := strings.Index(text, open)
	if start < 0 {
		t.Fatalf("%s declares no allow list (`%s`), so nothing here can tell an inherited variable from an allow-listed one (issue #662)", piTesterScript, open)
	}

	rest := text[start+len(open):]

	end := strings.Index(rest, "\"\n")
	if end < 0 {
		t.Fatalf("%s opens its allow list with `%s` and never closes it, so no name of it can be read (issue #662)", piTesterScript, open)
	}

	names := strings.Fields(rest[:end])

	if len(names) == 0 {
		t.Fatalf("%s declares an empty allow list, so the wrapper hands pi and node nothing at all (issue #662)", piTesterScript)
	}

	return names
}

// TestPiTesterHandsNodeAnAllowListedEnvironment pins the audit's finding L-A (issue #662, ADR 0044): before pi
// starts, the wrapper loads the refusal's extension and compiles its patterns with node, and that run must not be
// handed the environment the wrapper was handed either. A node that inherits GH_TOKEN or another agent's key puts
// it one `process.env` away from the pane, which is the leak the wrapper exists to close; the marker is what a
// 'bash -l' from that same pane reads, so node gets it too.
//
// The names are pinned rather than the values: a record of values would write the credentials these tests invent
// into a temporary file for no reason, and absence is what the wrapper's allow list decides (CLAUDE.md rule 3).
func TestPiTesterHandsNodeAnAllowListedEnvironment(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	binDir := filepath.Join(root, "bin")
	homeDir := filepath.Join(root, "home")
	record := filepath.Join(root, "pi-record")

	for _, dir := range []string{binDir, homeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	writePiTesterBin(t, binDir, record)
	writePiTesterDeck(t, homeDir)

	installed := writePiTesterInstalled(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	project := writePiTesterProject(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, piTesterEnv(binDir, homeDir, piTesterBasePath(t)))
	if err != nil {
		t.Fatalf("the wrapper exited non-zero with the env-printing refusal's two files in place: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	runs := readPiNodeRecord(t, piTesterNodeRecord(record))

	// pi's own record is what the test above pins against the allow list, and the shell both stand-ins run under
	// adds a name or two of its own to a command's environment (PWD, and `_` or SHLVL on some shells), which says
	// nothing about what the wrapper handed them: a name pi saw too is therefore no finding here, and every other
	// name has to be one the wrapper's allow list holds, plus the marker the wrapper sets itself.
	pi := readPiRecord(t, record)
	allowed := append(piTesterAllowedNames(t), piTesterMarker)

	for i, names := range runs {
		for _, name := range names {
			if slices.Contains(allowed, name) || slices.Contains(pi.envNames, name) {
				continue
			}

			t.Errorf("run %d of node saw %s in its environment, which the wrapper's allow list does not name and pi's own run never saw: node loads the env-printing refusal before pi starts, and a variable the list does not hold must not reach it (issue #662, CLAUDE.md rule 3)", i+1, name)
		}

		for _, name := range piTesterDropped {
			if slices.Contains(names, name) {
				t.Errorf("run %d of node saw %s, although the wrapper was handed it: the load of the refusal runs under the same allow list as pi, so no GitHub token, no other agent's key, no telemetry header and no variable the wrapper does not know reaches it (issue #662, CLAUDE.md rule 3)", i+1, name)
			}
		}

		// The marker has to reach node as well: the wrapper sets it for every command it runs, and a login shell
		// started from anything the load leaves behind reads it (issue #662).
		if !slices.Contains(names, piTesterMarker) {
			t.Errorf("run %d of node saw no %s, which the wrapper sets itself: the marker is what the VM's login profile reads to leave the agents' keys file unsourced, and node is started under the same environment pi is (issue #662, CLAUDE.md rule 3)", i+1, piTesterMarker)
		}
	}

	assertNoSecrets(t, "the wrapper's stdout", stdout, piTesterSecrets())
	assertNoSecrets(t, "the wrapper's stderr", stderr, piTesterSecrets())
}

// TestPiTesterRefusesAPathWithAnEmptyOrRelativeEntry pins the audit's finding N-1 (issue #662): the wrapper copies
// PATH as it is, and an empty entry or a relative one is the working directory, which is the unit's folder. A
// command the wrapper or pi looks up then runs one from the folder the tester reads tasks from, whatever the VM's
// own PATH holds, so the wrapper refuses such a PATH before pi or node starts instead of running with it.
//
// Each case's PATH keeps the fake tools' directory, so the wrapper would find jq, node and pi and start the tester
// without the refusal: finding them is what makes a missing refusal visible, rather than a run that stops on a
// tool it could not find.
func TestPiTesterRefusesAPathWithAnEmptyOrRelativeEntry(t *testing.T) {
	t.Parallel()

	// path builds the case's PATH from the fake tools' directory and the base the wrapper's other tools come from,
	// inserting the entry the case is about.
	cases := []struct {
		name string
		path func(binDir, base string) string
	}{
		{name: "an empty entry first", path: func(binDir, base string) string { return ":" + binDir + ":" + base }},
		{name: "an empty entry in the middle", path: func(binDir, base string) string { return binDir + "::" + base }},
		{name: "an empty entry last", path: func(binDir, base string) string { return binDir + ":" + base + ":" }},
		{name: "a dot entry", path: func(binDir, base string) string { return binDir + ":.:" + base }},
		{name: "a relative entry", path: func(binDir, base string) string { return binDir + ":bin:" + base }},
	}

	base := piTesterBasePath(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			homeDir := filepath.Join(root, "home")
			record := filepath.Join(root, "pi-record")

			for _, dir := range []string{binDir, homeDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			writePiTesterBin(t, binDir, record)

			installed := writePiTesterInstalled(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			project := writePiTesterProject(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			env := piTesterEnv(binDir, homeDir, base)
			env["PATH"] = tc.path(binDir, base)

			stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, env)

			if err == nil {
				t.Fatalf("the wrapper exited 0 with PATH %q, which holds an entry the shell resolves to the working directory: a command it starts is then one from the unit's folder, so pi must not start and node must not run (issue #662)\nstdout: %s\nstderr: %s", env["PATH"], stdout, stderr)
			}

			if !strings.Contains(stderr, "PATH") {
				t.Errorf("the wrapper's stderr does not name PATH, the variable it refused, so a maintainer whose pane stopped cannot tell which one to fix (issue #662): %s", stderr)
			}

			if _, statErr := os.Stat(record); statErr == nil {
				t.Errorf("the wrapper started pi with PATH %q: the refusal is meant to stop the tester before pi runs, since pi's own bash tool would find the folder's commands on such a PATH (issue #662)\nstdout: %s\nstderr: %s", env["PATH"], stdout, stderr)
			}

			if _, statErr := os.Stat(piTesterNodeRecord(record)); statErr == nil {
				t.Errorf("the wrapper ran node with PATH %q: node loads the env-printing refusal, and a command found in the unit's folder is no part of that load (issue #662)\nstdout: %s\nstderr: %s", env["PATH"], stdout, stderr)
			}

			assertNoSecrets(t, "the wrapper's stdout", stdout, piTesterSecrets())
			assertNoSecrets(t, "the wrapper's stderr", stderr, piTesterSecrets())
		})
	}
}

// TestPiTesterPassesAwkwardArgumentsUnchanged pins that the words the deck's command passes reach pi as they are:
// an argument may hold spaces (a system prompt does) and one may be empty, and a wrapper that rebuilds its
// arguments splits the first and loses the second (issue #662).
func TestPiTesterPassesAwkwardArgumentsUnchanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
	}{
		{name: "an argument with spaces", args: []string{"--provider", "deepseek", "--append-system-prompt", "You are the tester. Read the task first."}},
		{name: "an empty argument", args: []string{"--provider", "", "--model", "deepseek-flash"}},
		{name: "an empty argument beside one with spaces and a tab", args: []string{"", "two words\tand a tab", ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			homeDir := filepath.Join(root, "home")
			record := filepath.Join(root, "pi-record")

			for _, dir := range []string{binDir, homeDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			writePiTesterBin(t, binDir, record)
			writePiTesterDeck(t, homeDir)

			installed := writePiTesterInstalled(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			project := writePiTesterProject(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			stdout, stderr, err := runPiTester(t, installed.script, project, tc.args, piTesterEnv(binDir, homeDir, piTesterBasePath(t)))
			if err != nil {
				t.Fatalf("the wrapper exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			got := readPiRecord(t, record)

			if diff := cmp.Diff(piTesterWantArgv(t, installed, homeDir, project, tc.args), got.argv); diff != "" {
				t.Errorf("the installed refusal, the deck's extension and the arguments did not reach pi unchanged, so the tester runs with words the deck's command did not pass, without the refusal, or with the unit folder's extensions (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPiTesterRefusesToStartPiWithoutTheDenyExtension pins the second half of issue #662: the check of the two
// files the env-printing refusal is made of, and that a failed check stops the tester instead of letting it run
// without the refusal (ADR 0044 fails open on its own, so the wrapper is what fails closed). A missing or empty
// extension and a missing or malformed list each exit non-zero, name the file, and never start pi.
func TestPiTesterRefusesToStartPiWithoutTheDenyExtension(t *testing.T) {
	t.Parallel()

	extension := piFile{present: true, content: readRepoFile(t, denyExtensionFile)}
	list := piFile{present: true, content: readRepoFile(t, denyListFile)}

	cases := []struct {
		name    string
		ext     piFile
		list    piFile
		refuses bool
		// wantFile is the file a refusal must name, so a maintainer reading the pane knows which one to fix.
		wantFile string
		// noJq leaves jq off the wrapper's PATH, which is the check the wrapper cannot make at all.
		noJq bool
	}{
		{name: "both files as the repository has them", ext: extension, list: list},
		{name: "the extension is missing", list: list, refuses: true, wantFile: "deny-env-printing.ts"},
		{name: "the extension is empty", ext: piFile{present: true}, list: list, refuses: true, wantFile: "deny-env-printing.ts"},
		{name: "the extension is a directory", ext: piFile{present: true, directory: true}, list: list, refuses: true, wantFile: "deny-env-printing.ts"},
		{name: "the list is missing", ext: extension, refuses: true, wantFile: "deny-env-printing.json"},
		{name: "the list is not JSON", ext: extension, list: piFile{present: true, content: `{"deny": [`}, refuses: true, wantFile: "deny-env-printing.json"},
		// An empty patterns array is a refusal document that refuses nothing, and an object without the key is
		// the shape a misspelt `pattern` leaves: both must stop the tester, as a list that cannot be read does.
		{name: "the list's patterns are empty", ext: extension, list: piFile{present: true, content: `{"patterns": []}`}, refuses: true, wantFile: "deny-env-printing.json"},
		{name: "the list is an object without patterns", ext: extension, list: piFile{present: true, content: `{}`}, refuses: true, wantFile: "deny-env-printing.json"},
		// jq is the only tool the check of the list needs, and without it the list is checked by nothing.
		{name: "jq is not on PATH", ext: extension, list: list, refuses: true, wantFile: "deny-env-printing.json", noJq: true},
	}

	// jq's own directory, found on this machine, for every case that has jq.
	withJq := piTesterBasePath(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			homeDir := filepath.Join(root, "home")
			record := filepath.Join(root, "pi-record")

			for _, dir := range []string{binDir, homeDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			writePiTesterBin(t, binDir, record)
			writePiTesterDeck(t, homeDir)

			// The bad files go in the installed layout, the one the wrapper checks; the unit folder keeps the
			// repository's copies, so the case pins what the installed file decides, not what the project has.
			installed := writePiTesterInstalled(t, root, tc.ext, tc.list)
			project := writePiTesterProject(t, root, extension, list)

			path := withJq
			if tc.noJq {
				// A PATH of this case's own, with every tool the wrapper uses but jq on it: whatever the machine
				// keeps in /usr/bin or a Homebrew prefix never provides the jq this check has to miss.
				path = writePiTesterFarm(t, filepath.Join(root, "farm"), "")
			}

			env := piTesterEnv(binDir, homeDir, path)

			stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, env)

			secrets := piTesterSecrets()

			if !tc.refuses {
				if err != nil {
					t.Fatalf("the wrapper exited non-zero with the env-printing refusal's two files in place: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
				}

				got := readPiRecord(t, record)

				if diff := cmp.Diff(piTesterWantArgv(t, installed, homeDir, project, piTesterArgs), got.argv); diff != "" {
					t.Errorf("the wrapper did not start pi with the installed refusal, the deck's extension and its arguments unchanged (-want +got):\n%s", diff)
				}

				return
			}

			if err == nil {
				t.Fatalf("the wrapper exited 0 with %s: pi must not start without the env-printing refusal, whose load fails open on its own (issue #662, ADR 0044)\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			if _, statErr := os.Stat(record); statErr == nil {
				t.Fatalf("the wrapper started pi with %s: the check of the refusal's files is meant to stop the tester, and pi logs an extension it cannot load and runs without it (issue #662, ADR 0044)\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			wantPath := installed.extension
			if tc.wantFile == filepath.Base(installed.list) {
				wantPath = installed.list
			}

			if !strings.Contains(stderr, wantPath) {
				t.Errorf("the wrapper's stderr does not name the installed file %s, the one it could not use: a message that names no file leaves the maintainer to guess which of the two broke (issue #662, issue #692): %s", wantPath, stderr)
			}

			assertNoSecrets(t, "the wrapper's stdout", stdout, secrets)
			assertNoSecrets(t, "the wrapper's stderr", stderr, secrets)
		})
	}
}

// piTesterAssertRefused asserts that the wrapper refused to start pi for the case named, named the file it could
// not use, and printed no value of the environment it was handed: the refusal is the whole point of the checks,
// since pi logs an extension that fails to load and runs without the refusal (ADR 0044, CLAUDE.md rule 3).
func piTesterAssertRefused(t *testing.T, name, record, wantFile, stdout, stderr string, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("the wrapper exited 0 with %s: pi must not start without a working env-printing refusal, whose load fails open on its own (issue #662, ADR 0044)\nstdout: %s\nstderr: %s", name, stdout, stderr)
	}

	if _, statErr := os.Stat(record); statErr == nil {
		t.Fatalf("the wrapper started pi with %s: the check of the refusal's two files is meant to stop the tester, and pi logs an extension it cannot load and runs without it (issue #662, ADR 0044)\nstdout: %s\nstderr: %s", name, stdout, stderr)
	}

	if !strings.Contains(stderr, wantFile) {
		t.Errorf("the wrapper's stderr does not name %s, the file it could not use: a message that names no file leaves the maintainer to guess which of the two broke (issue #662): %s", wantFile, stderr)
	}

	assertNoSecrets(t, "the wrapper's stdout", stdout, piTesterSecrets())
	assertNoSecrets(t, "the wrapper's stderr", stderr, piTesterSecrets())
}

// TestPiTesterChecksTheDenyFilesWithNode pins the check jq cannot make (issue #662, round 2): before pi starts, the
// wrapper loads the extension with node - a top level that throws fails the load, which pi logs and survives
// without the refusal - and compiles every pattern of the list with `new RegExp`, since a pattern that is no
// regular expression refuses nothing. Either failure stops the tester instead of leaving it without the refusal.
//
// These cases run the machine's real node: a stand-in that always exits 0 would pass whatever the files hold.
func TestPiTesterChecksTheDenyFilesWithNode(t *testing.T) {
	t.Parallel()

	extension := piFile{present: true, content: readRepoFile(t, denyExtensionFile)}
	list := piFile{present: true, content: readRepoFile(t, denyListFile)}

	// The extension's top level does nothing that can throw, so a statement that throws where it stands is the
	// shape an edit takes when it gets one of the extension's regular expressions wrong: node fails the import
	// the way pi would log it (test/architecture/pienvdeny_test.go loads the same copy).
	throwing := piFile{present: true, content: extension.content + "\nnew RegExp(\"(\");\n"}

	cases := []struct {
		name     string
		ext      piFile
		list     piFile
		refuses  bool
		wantFile string
	}{
		{name: "both files as the repository has them", ext: extension, list: list},
		{name: "the extension throws where node loads it", ext: throwing, list: list, refuses: true, wantFile: "deny-env-printing.ts"},
		{name: "a pattern of the list is not a regular expression", ext: extension, list: piFile{present: true, content: `{"reason": "invented for this test", "patterns": ["("]}`}, refuses: true, wantFile: "deny-env-printing.json"},
	}

	nodePath := piTesterRealNode(t)
	path := filepath.Dir(nodePath) + ":" + piTesterBasePath(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			homeDir := filepath.Join(root, "home")
			record := filepath.Join(root, "pi-record")

			for _, dir := range []string{binDir, homeDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			// Only pi stands in the first directory: the node the wrapper runs has to be the real one.
			writeFakePi(t, binDir, record)
			writePiTesterDeck(t, homeDir)

			// The bad file goes in the installed layout, the one the wrapper loads; the unit folder keeps the
			// repository's copies, so the case pins what the installed file decides (issue #692).
			installed := writePiTesterInstalled(t, root, tc.ext, tc.list)
			project := writePiTesterProject(t, root, extension, list)

			stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, piTesterEnv(binDir, homeDir, path))

			if !tc.refuses {
				if err != nil {
					t.Fatalf("the wrapper exited non-zero with the env-printing refusal's two files in place and node %s to load them: %v\nstdout: %s\nstderr: %s", nodePath, err, stdout, stderr)
				}

				got := readPiRecord(t, record)

				if diff := cmp.Diff(piTesterWantArgv(t, installed, homeDir, project, piTesterArgs), got.argv); diff != "" {
					t.Errorf("the wrapper did not start pi with the installed refusal, the deck's extension and its arguments unchanged (-want +got):\n%s", diff)
				}

				assertNoSecrets(t, "the wrapper's stdout", stdout, piTesterSecrets())
				assertNoSecrets(t, "the wrapper's stderr", stderr, piTesterSecrets())

				return
			}

			wantPath := installed.extension
			if tc.wantFile == filepath.Base(installed.list) {
				wantPath = installed.list
			}

			piTesterAssertRefused(t, tc.name, record, wantPath, stdout, stderr, err)
		})
	}
}

// TestPiTesterRefusesToStartPiWithoutNode pins the same check on a machine with no node at all: the wrapper
// cannot load the extension, so it must not start pi, which would run without a refusal nobody saw refuse.
//
// The wrapper's PATH is a farm of links to its own tools with no node on it, so the case holds whatever the
// machine the tests run on has installed in its own PATH.
func TestPiTesterRefusesToStartPiWithoutNode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	binDir := filepath.Join(root, "bin")
	homeDir := filepath.Join(root, "home")
	record := filepath.Join(root, "pi-record")

	for _, dir := range []string{binDir, homeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	writeFakePi(t, binDir, record)

	installed := writePiTesterInstalled(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	project := writePiTesterProject(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	// The farm holds jq, so the list is checked and the missing node is what stops the wrapper.
	path := writePiTesterFarm(t, filepath.Join(root, "farm"), piTesterJqPath(t))

	stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, piTesterEnv(binDir, homeDir, path))

	piTesterAssertRefused(t, "no node on PATH", record, installed.extension, stdout, stderr, err)
}

// TestPiTesterLaunchWrapperIsExecutable pins that the deck can run the wrapper by its path alone: the deck's
// command names dev/pi-tester.sh, and a script the checkout or an index entry holds without an execute bit fails
// the tester's pane before pi is reached (issue #662).
func TestPiTesterLaunchWrapperIsExecutable(t *testing.T) {
	t.Parallel()

	path := scriptPath(t, piTesterScript)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", piTesterScript, err)
	}

	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s has mode %v, with no execute bit: the deck's tester command runs the path itself, so the tester's pane never starts (issue #662)", piTesterScript, info.Mode().Perm())
	}

	// The checkout's mode is half of it: the repository's index is what a clone gets, and a mode committed as
	// 100644 comes back without the bit however the file looks here.
	git := realTool(t, "git")

	cmd := exec.Command(git, "ls-files", "-s", piTesterScript)
	cmd.Dir = moduleRoot
	cmd.Env = envList(map[string]string{
		"PATH": filepath.Dir(git) + ":/usr/bin:/bin",
		"HOME": t.TempDir(),
	})

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s ls-files -s %s: %v", git, piTesterScript, err)
	}

	if !strings.HasPrefix(string(out), "100755 ") {
		t.Errorf("git holds %s as %q, want 100755: a clone of the repository then has no execute bit on the wrapper the deck's tester command runs (issue #662)", piTesterScript, strings.TrimSpace(string(out)))
	}
}

// limaProfileTerminator ends the heredoc dev/lima.yaml writes /etc/profile.d/shift2eu.sh from.
const limaProfileTerminator = "PROFILE"

// limaProfileScript returns the lines dev/lima.yaml has the VM write to /etc/profile.d/shift2eu.sh: the body of
// the heredoc, dedented the way its provision script's YAML block scalar dedents it, which is what the login
// shell of the VM reads.
func limaProfileScript(t *testing.T) string {
	t.Helper()

	for _, script := range provisionScripts(t, readRepoFile(t, limaTemplateRepoPath)) {
		for i, line := range script.script {
			if !strings.Contains(line, "profile.d/shift2eu.sh") || !strings.Contains(line, "<<") {
				continue
			}

			var body []string

			for _, next := range script.script[i+1:] {
				if strings.TrimSpace(next) == limaProfileTerminator {
					return strings.Join(body, "\n")
				}

				body = append(body, next)
			}

			t.Fatalf("%s line %d opens the heredoc that writes /etc/profile.d/shift2eu.sh without a %s line to end it: the shell then never ends the heredoc and the rest of the template becomes the profile (issue #662)",
				limaTemplateRepoPath, script.header, limaProfileTerminator)
		}
	}

	t.Fatalf("%s has no provision script that writes /etc/profile.d/shift2eu.sh, so this test cannot pin what the VM's login shells source (issue #662)", limaTemplateRepoPath)

	return ""
}

// profileFakeKey is the invented variable the fake agents' keys file defines, and profileFakeValue its invented
// value (CLAUDE.md rule 3).
const (
	profileFakeKey   = "SHIFT2EU_FAKE_KEY_662"
	profileFakeValue = "fake"
)

// TestLimaLoginProfileSkipsTheKeysOfTheTester pins the login-profile guard of issue #662: the VM's
// /etc/profile.d/shift2eu.sh sources the agents' keys file with `set -a` into every login shell, which is what a
// `bash -l` from the tester's bash tool starts, and it must leave the file unsourced when the marker the wrapper
// sets is in the environment.
//
// The snippet is extracted from dev/lima.yaml and run by sh against a fake HOME whose keys file defines one
// invented variable, followed by a child shell: what the snippet merely sets stays in the shell it runs in, and
// what it exports reaches the child the way it would reach pi's bash tool.
func TestLimaLoginProfileSkipsTheKeysOfTheTester(t *testing.T) {
	t.Parallel()

	snippet := limaProfileScript(t)

	cases := []struct {
		name string
		// set places the marker in the login shell's environment, marker's value being what it holds.
		set          bool
		marker       string
		wantInherits bool
	}{
		{
			name:         "no marker, as a maintainer's login shell has none",
			wantInherits: true,
		},
		{
			name:   "the marker set to 1, as the wrapper sets it",
			set:    true,
			marker: piTesterMarkerValue,
		},
		{
			// The profile skips the keys for the tester only when the marker says something, and an empty marker
			// is what an unset one leaves behind: a shell that lost its environment must keep its credentials.
			name:         "the marker set but empty",
			set:          true,
			marker:       "",
			wantInherits: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			homeDir := filepath.Join(t.TempDir(), "home")

			writeFileMode(t, filepath.Join(homeDir, ".config", "dot-agent-deck", "daemon.env"),
				[]byte(profileFakeKey+"="+profileFakeValue+"\n"), 0o600)

			env := map[string]string{
				"HOME": homeDir,
				"PATH": "/usr/local/bin:/usr/bin:/bin",
			}

			if tc.set {
				env[piTesterMarker] = tc.marker
			}

			// The profile snippet, then the child shell that stands in for pi's bash tool.
			script := snippet + "\nsh -c 'if [ -n \"${" + profileFakeKey + "+x}\" ]; then printf inherited; else printf absent; fi'"

			cmd := exec.Command("sh", "-c", script)
			cmd.Env = envList(env)
			cmd.Dir = homeDir

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("the profile snippet dev/lima.yaml writes could not run: %v\n%s", err, out)
			}

			want := "absent"
			if tc.wantInherits {
				want = "inherited"
			}

			if got := strings.TrimSpace(string(out)); got != want {
				t.Errorf("the login shell %s the keys file's %s, want it %s: the VM's profile script has to leave the agents' keys unsourced when %s=%s is set, and source them otherwise (issue #662, CLAUDE.md rule 3)",
					got, profileFakeKey, want, piTesterMarker, piTesterMarkerValue)
			}
		})
	}
}

// deckRoleSettingLine matches a `key = "value"` line of a role block of the deck's configuration, and the value
// it holds.
func deckRoleSettingLine(key string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(key) + ` *= *"(.*)"$`)
}

// deckCommandLine matches the command line of a role block of the deck's configuration, and the command it holds.
var deckCommandLine = deckRoleSettingLine("command")

// deckAgentLine matches the agent line of a role block of the deck's configuration, and the agent it names.
var deckAgentLine = deckRoleSettingLine("agent")

// TestPiTesterCommandRunsTheLaunchWrapper pins the wiring: the deck's tester role runs the installed wrapper by
// its absolute, root-owned path, never the unit folder's copy, or a branch could weaken what the tester checks
// before it starts (issue #662, issue #692); the arguments are the ones the deck's command names, and the role
// names the agent the deck cannot see behind that wrapper (issue #691).
func TestPiTesterCommandRunsTheLaunchWrapper(t *testing.T) {
	t.Parallel()

	command := testerRoleCommand(t)
	words := strings.Fields(command)

	want := append([]string{piTesterInstalledScript}, piTesterArgs...)

	if diff := cmp.Diff(want, words); diff != "" {
		t.Errorf("%s: the %s role's command is %q, want it to start with the installed wrapper %s and pass it %v: a command starting anywhere else runs a copy the unit folder can change, and the wrapper is what keeps the tester's environment to its own credential and stops it when the env-printing refusal cannot be loaded (-want +got):\n%s",
			deckConfigPath, deckTesterRole, command, piTesterInstalledScript, piTesterArgs, diff)
	}

	if agent := testerRoleAgent(t); agent != deckTesterAgent {
		t.Errorf("%s: the %s role's agent is %q, want %q: the role starts %s, a launcher, and the deck cannot see the agent behind a launcher, so the agent line is the only place the deck learns the tester runs pi (issue #691, ADR 0044)",
			deckConfigPath, deckTesterRole, agent, deckTesterAgent, piTesterInstalledScript)
	}
}

// testerRoleCommand returns the tester role's command from the deck's configuration.
func testerRoleCommand(t *testing.T) string {
	t.Helper()

	return testerRoleSetting(t, deckCommandLine)
}

// testerRoleAgent returns the tester role's agent from the deck's configuration.
func testerRoleAgent(t *testing.T) string {
	t.Helper()

	return testerRoleSetting(t, deckAgentLine)
}

// testerRoleSetting returns one setting of the tester role from the deck's configuration, or the empty string when
// the role block names it nowhere. The role blocks are split on their table header, and the block named `tester` is
// the one holder of the setting; a setting it does not hold is reported by the test that asked for it.
func testerRoleSetting(t *testing.T, line *regexp.Regexp) string {
	t.Helper()

	const roleHeader = "[[orchestrations.roles]]"

	blocks := strings.Split(readRepoFile(t, deckConfigPath), roleHeader)

	for _, block := range blocks[1:] {
		if !strings.Contains(block, "\nname = \""+deckTesterRole+"\"") {
			continue
		}

		for _, blockLine := range strings.Split(block, "\n") {
			if match := line.FindStringSubmatch(strings.TrimSpace(blockLine)); match != nil {
				return match[1]
			}
		}

		return ""
	}

	t.Fatalf("%s: no role block is named %q", deckConfigPath, deckTesterRole)

	return ""
}

// TestPiTesterLaunchWrapperEndsWithItsExecLine pins the wrapper's last line (ADR 0044's amendment "the tester loads
// no project extension or setting", issue #702): pi starts with no project trust and no discovered extension, loads
// the installed refusal and the deck's extension by --extension and the unit folder's skills by --skill, and the
// wrapper's own flags come before the deck's arguments. dev/vm-pi-tester.sh accepts exactly this line, so the two
// have to agree.
func TestPiTesterLaunchWrapperEndsWithItsExecLine(t *testing.T) {
	t.Parallel()

	lines := strings.Split(strings.TrimSuffix(readRepoFile(t, piTesterScript), "\n"), "\n")

	if got := lines[len(lines)-1]; got != piTesterExecLine {
		t.Errorf("%s ends with %q, want %q: the tester's pi starts with no project trust and no discovered extension, so no branch can run code or a setting of the unit folder inside pi's process (ADR 0044, issue #702)",
			piTesterScript, got, piTesterExecLine)
	}
}

// TestPiTesterStartsPiWithoutProjectTrustOrADiscoveredExtension pins ADR 0044's amendment (issue #702): pi starts
// with `--no-approve --no-extensions`, so neither the unit folder's `.pi/extensions` nor its `.pi/settings.json` is
// code pi runs or a setting pi reads; the installed refusal and the deck's extension come in by `--extension` and
// the skills by `--skill`, even though the unit folder holds no `.claude/skills` (a path pi cannot find is pi's own
// diagnostic, not this wrapper's refusal, and the skills are no part of it).
func TestPiTesterStartsPiWithoutProjectTrustOrADiscoveredExtension(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	binDir := filepath.Join(root, "bin")
	homeDir := filepath.Join(root, "home")
	record := filepath.Join(root, "pi-record")

	for _, dir := range []string{binDir, homeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	writePiTesterBin(t, binDir, record)

	deck := writePiTesterDeck(t, homeDir)

	installed := writePiTesterInstalled(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	project := writePiTesterProject(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	// What a branch can leave in the unit folder: an extension pi would discover, and a settings file whose keys run
	// commands (shellCommandPrefix, packages, npmCommand) once the project is trusted (ADR 0044, issue #702).
	plantedExtension := filepath.Join(project, ".pi", "extensions", "x.ts")
	writeFileMode(t, plantedExtension, []byte("// an extension a branch planted, which pi must not load (issue #702)\n"), 0o644)
	writeFileMode(t, filepath.Join(project, ".pi", "settings.json"),
		[]byte(`{"extensions": ["extensions/x.ts"], "packages": ["@example/invented"], "shellCommandPrefix": "true"}`), 0o644)

	if _, err := os.Stat(piTesterSkillsDir(project)); err == nil {
		t.Fatalf("%s exists, so this case cannot pin that the wrapper passes --skill even when the directory is missing (issue #702)", piTesterSkillsDir(project))
	}

	stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, piTesterEnv(binDir, homeDir, piTesterBasePath(t)))
	if err != nil {
		t.Fatalf("the wrapper exited non-zero with the installed files and the deck's extension in place: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	got := readPiRecord(t, record)

	if diff := cmp.Diff(piTesterWantArgv(t, installed, homeDir, project, piTesterArgs), got.argv); diff != "" {
		t.Errorf("pi's argv is not the wrapper's no-trust, no-discovery start: it has to be --no-approve --no-extensions, the installed refusal and the deck's extension by --extension, the unit folder's skills by --skill, and then the deck's arguments unchanged (-want +got):\n%s", diff)
	}

	// The two flags are pinned by their place too: every flag the wrapper adds has to come before the deck's own
	// arguments, or a `--` among those would turn the rest into messages pi reads.
	firstPassed := slices.Index(got.argv, piTesterArgs[0])

	for _, flag := range []string{"--no-approve", "--no-extensions"} {
		at := slices.Index(got.argv, flag)

		switch {
		case at < 0:
			t.Errorf("pi's argv holds no %s, so pi trusts the project and discovers the unit folder's extensions (ADR 0044, issue #702): %q", flag, got.argv)
		case at > firstPassed:
			t.Errorf("pi's argv holds %s at %d, after the deck's first argument %q at %d: the wrapper's flags come before the deck's arguments, so a `--` among those cannot turn them into messages (issue #702): %q",
				flag, at, piTesterArgs[0], firstPassed, got.argv)
		}
	}

	if !slices.Contains(got.argv, deck) {
		t.Errorf("pi's argv does not name the deck's extension directory %s, which the deck writes before it starts a pi agent: without it the pane gets no task and its work-done is not delivered (issue #702): %q", deck, got.argv)
	}

	// This loop cannot fail on the wrapper of before issue #702 either, which never named the project's extensions
	// path on pi's argv: it is a cheap guard, not the case's evidence. What the case pins is what the exact argv
	// diff above and the two flags' presence and place do: --no-approve and --no-extensions are there, before the
	// deck's own arguments, and the argv is the wrapper's whole start, unchanged arguments included.
	for _, arg := range got.argv {
		if arg == plantedExtension || strings.Contains(arg, filepath.Join(project, ".pi", "extensions")) {
			t.Errorf("pi's argv holds %q, which names the unit folder's .pi/extensions: a branch's extension then loads beside the installed refusal, in the same process, and could undo it (ADR 0044, issue #702)", arg)
		}
	}

	if got := strings.TrimSpace(stderr); got != "" {
		t.Errorf("the wrapper printed %q on stderr although the project copies are the installed ones and no argument was dropped (issue #702)", got)
	}
}

// piTesterDroppedArgumentNotice is the line the wrapper prints for each `--approve` or `-a` it drops: refusing them
// would stop a tester of a unit cut before issue #702's merge, whose .dot-agent-deck.toml still passes `--approve`.
const piTesterDroppedArgumentNotice = "pi-tester: argument %s dropped: the tester's pi never trusts the project, because a trusted .pi/settings.json can run commands the env-printing refusal does not see (shellCommandPrefix, packages, npmCommand) (issue #702)"

// TestPiTesterDropsTheApproveArguments pins what the wrapper does with the argument a unit cut before issue #702
// still passes: `--approve` and `-a` are dropped, each with one line on stderr, and pi starts without them, since
// either would undo `--no-approve` (pi keeps the last) and a trusted .pi/settings.json can run commands the
// env-printing refusal never sees. The argument is dropped wherever it stands, also after a `--`, and every other
// spelling reaches pi unchanged.
func TestPiTesterDropsTheApproveArguments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		// dropped are the arguments the wrapper must drop, in the order it drops them.
		dropped []string
		// passed are the arguments that must reach pi, unchanged and in their order.
		passed []string
	}{
		{
			name:    "the deck's old command, with --approve last",
			args:    []string{"--provider", "deepseek", "--model", "deepseek-flash", "--thinking", "high", "--approve"},
			dropped: []string{"--approve"},
			passed:  []string{"--provider", "deepseek", "--model", "deepseek-flash", "--thinking", "high"},
		},
		{name: "the short flag -a", args: []string{"-a"}, dropped: []string{"-a"}},
		{name: "--approve after a -- separator", args: []string{"--", "--approve"}, dropped: []string{"--approve"}, passed: []string{"--"}},
		{name: "both spellings, in their order", args: []string{"--provider", "--approve", "--model", "-a"}, dropped: []string{"--approve", "-a"}, passed: []string{"--provider", "--model"}},
		{name: "--approve with a value is no --approve", args: []string{"--approve=1"}, passed: []string{"--approve=1"}},
		{name: "-ab is no -a", args: []string{"-ab"}, passed: []string{"-ab"}},
		{name: "--no-approve is passed on", args: []string{"--no-approve"}, passed: []string{"--no-approve"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			homeDir := filepath.Join(root, "home")
			record := filepath.Join(root, "pi-record")

			for _, dir := range []string{binDir, homeDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			writePiTesterBin(t, binDir, record)
			writePiTesterDeck(t, homeDir)

			installed := writePiTesterInstalled(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			project := writePiTesterProject(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			stdout, stderr, err := runPiTester(t, installed.script, project, tc.args, piTesterEnv(binDir, homeDir, piTesterBasePath(t)))
			if err != nil {
				t.Fatalf("the wrapper exited non-zero with %v: a unit whose command still passes --approve must keep a working tester, so the wrapper drops the argument and starts pi (issue #702): %v\nstdout: %s\nstderr: %s",
					tc.args, err, stdout, stderr)
			}

			got := readPiRecord(t, record)

			if diff := cmp.Diff(piTesterWantArgv(t, installed, homeDir, project, tc.passed), got.argv); diff != "" {
				t.Errorf("pi did not start with exactly the arguments that are not --approve or -a, unchanged and in their order (-want +got):\n%s", diff)
			}

			lines := piTesterOutputLines(stderr)

			if len(lines) != len(tc.dropped) {
				t.Fatalf("the wrapper printed %d line(s) on stderr with %v, want one per dropped argument (%d): a unit whose tester command still passes --approve is told once, and a run with nothing to drop prints nothing (issue #702): %q",
					len(lines), tc.args, len(tc.dropped), lines)
			}

			for i, arg := range tc.dropped {
				if want := fmt.Sprintf(piTesterDroppedArgumentNotice, arg); lines[i] != want {
					t.Errorf("the wrapper's line %d is %q, want %q: the line names the argument it dropped and why, so a maintainer can drop it from .dot-agent-deck.toml's tester command (issue #702)", i+1, lines[i], want)
				}

				if slices.Contains(got.argv, arg) {
					t.Errorf("pi's argv holds %q, which the wrapper dropped with a line: an argument that reaches pi undoes --no-approve, since pi keeps the last of the two (issue #702): %q", arg, got.argv)
				}
			}
		})
	}
}

// TestPiTesterRefusesANonAbsoluteHome pins the check the deck's extension needs (issue #702): the wrapper names
// ${HOME}/.pi/agent/extensions/dot-agent-deck, so a HOME that is unset, empty or not absolute builds a path it cannot
// find. It exits 1 with one line that names HOME and never its value, and nothing runs - not the installed-file
// checks, which would otherwise report a file the wrapper cannot locate, and not pi.
func TestPiTesterRefusesANonAbsoluteHome(t *testing.T) {
	t.Parallel()

	const refusal = "pi-tester: HOME is not an absolute path, so the deck's pi extension cannot be found: the tester is not started (issue #702)"

	cases := []struct {
		name string
		// set false leaves HOME out of the wrapper's environment entirely; otherwise home is its value.
		set  bool
		home string
	}{
		{name: "HOME is unset"},
		{name: "HOME is empty", set: true},
		{name: "HOME is a relative path", set: true, home: "home/invented"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			homeDir := filepath.Join(root, "home")
			record := filepath.Join(root, "pi-record")

			for _, dir := range []string{binDir, homeDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			writePiTesterBin(t, binDir, record)

			installed := writePiTesterInstalled(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			project := writePiTesterProject(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			env := piTesterEnv(binDir, homeDir, piTesterBasePath(t))

			if tc.set {
				env["HOME"] = tc.home
			} else {
				delete(env, "HOME")
			}

			stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, env)

			if got := exitStatusOf(t, err); got != 1 {
				t.Errorf("the wrapper exited %d with HOME %s, want 1: a HOME that is no absolute path is a refusal with one line, not a crash and not a start (issue #702)\nstdout: %s\nstderr: %s",
					got, tc.name, stdout, stderr)
			}

			if got := strings.TrimSpace(stderr); got != refusal {
				t.Errorf("the wrapper's stderr is %q, want %q: the one line names the variable it refused, so a maintainer whose pane stopped knows what to fix, and names no value of it (issue #702)", got, refusal)
			}

			// The value the case handed must never reach the stream, whatever it was: a relative HOME names a folder of
			// the unit, which is a path a maintainer need not see, and the message is the same for all three cases.
			if tc.set && tc.home != "" {
				assertNoSecrets(t, "the wrapper's stderr", stderr, []secret{{kind: "the value of HOME", value: tc.home}})
			}

			if _, statErr := os.Stat(record); statErr == nil {
				t.Errorf("the wrapper started pi with HOME %s: the deck's extension cannot be found without an absolute HOME, so pi would start and never receive a task (issue #702)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if _, statErr := os.Stat(piTesterNodeRecord(record)); statErr == nil {
				t.Errorf("the wrapper ran node with HOME %s: the HOME check comes before the installed files' checks, which name a file the wrapper cannot locate without an absolute HOME (issue #702)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}
		})
	}
}

// piTesterDeckRefusal is the one line the wrapper prints when the deck's pi extension is not where the deck writes
// it (issue #702).
func piTesterDeckRefusal(deck, why string) string {
	return fmt.Sprintf("pi-tester: the deck's pi extension %s %s: the tester is not started, because without it the pane gets no task and its work-done is not delivered; run dot-agent-deck orchestrator setup, then restart the pane (issue #702)", deck, why)
}

// piTesterAgentRefusal is the one line the wrapper prints when pi's agent directory could be the unit folder's
// (issue #702).
func piTesterAgentRefusal(agent, why string) string {
	return fmt.Sprintf("pi-tester: pi's agent directory %s %s: the tester is not started, because pi trusts the settings and extensions it finds there (issue #702)", agent, why)
}

// TestPiTesterRefusesADeckExtensionItCannotUse pins the check the deck's extension needs (issue #702): without it the
// pane starts and never receives a task, a silent stall, so the wrapper fails closed and names the directory and
// `dot-agent-deck orchestrator setup` instead. The directory has to lie under HOME - never in the working directory,
// which a branch writes -, hold no symbolic link anywhere above it, which the deck never writes, and contain a
// regular, never a linked, index.ts, index.js or package.json, whichever the deck's release writes.
//
// Since round 1 of this issue's review, the wrapper checks pi's agent directory - ${HOME}/.pi/agent, which the deck's
// extension lies in - before the deck's own path, and a HOME spelled with `/./`, `//` or through a link reaches it
// as the unit folder's. So the two cases whose HOME is the unit folder or lies below it now stop at that refusal,
// while every other case here keeps pi's agent directory a real one under a HOME outside the unit folder.
//
// One case pins the other half: the deck's own check against the working directory's physical path. pi starts in a
// link to ${HOME}/.pi/agent/extensions, so the deck's directory lies in the unit folder physically - but the deck's
// spelling is no prefix of the shell's resolved PWD, whose HOME carries a `.`, and pi's agent directory is that
// folder's parent, so nothing before the physical check refuses it (issue #702, round 2).
func TestPiTesterRefusesADeckExtensionItCannotUse(t *testing.T) {
	t.Parallel()

	const (
		missing = "is missing or not a directory"
		// symlinked is the deck's own directory being a link; linkedAbove the extensions directory it lies in, which
		// the wrapper refuses too: it writes neither (issue #702).
		symlinked   = "is a symbolic link, which the deck never writes"
		linkedAbove = "is reached through a symbolic link, which the deck never writes"
		noIndex     = "has no index.ts, index.js or package.json"
		inUnit      = "lies in the working directory, the unit folder"
	)

	// deckFile writes one file of the deck's directory into home.
	deckFile := func(t *testing.T, home, name string) {
		t.Helper()

		writeFileMode(t, filepath.Join(piTesterDeckDir(home), name), []byte("// the deck's pi extension, invented for this test (issue #702)\n"), 0o644)
	}

	cases := []struct {
		name string
		// home returns the HOME the wrapper is handed, after laying the deck's path out as the case leaves it.
		home func(t *testing.T, root, project string) string
		// dir, when set, returns the directory pi starts in, when that is not the unit folder the test writes: the
		// deck's checks compare its path with the working directory's, which a case moves or reaches through a link.
		dir func(t *testing.T, root, home, project string) string
		// why is the condition the refusal line names; empty means the case starts pi.
		why string
		// agent says the refusal is pi's agent directory's, which the wrapper checks before the deck's extension: the
		// two lie one inside the other, so a HOME in the unit folder is refused there first (issue #702).
		agent bool
	}{
		{
			name: "the deck's directory is missing",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				writePiTesterAgentDir(t, home)

				return home
			},
			why: missing,
		},
		{
			name: "the deck's path is a regular file",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				writeFileMode(t, piTesterDeckDir(home), []byte("not a directory\n"), 0o644)

				return home
			},
			why: missing,
		},
		{
			name: "the deck's directory is a symbolic link to a whole one",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				target := filepath.Join(root, "written-by-the-deck")
				deckFile(t, target, "index.ts")

				if err := os.MkdirAll(filepath.Dir(piTesterDeckDir(home)), 0o700); err != nil {
					t.Fatalf("create %s: %v", filepath.Dir(piTesterDeckDir(home)), err)
				}

				if err := os.Symlink(target, piTesterDeckDir(home)); err != nil {
					t.Fatalf("link %s to %s: %v", piTesterDeckDir(home), target, err)
				}

				return home
			},
			why: symlinked,
		},
		{
			name: "the deck's directory is reached through a symbolic link above it",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				writePiTesterAgentDir(t, home)

				written := filepath.Join(root, "written-by-the-deck")
				writePiTesterDeck(t, written)

				if err := os.Symlink(filepath.Join(written, ".pi", "agent", "extensions"), filepath.Join(piTesterAgentDir(home), "extensions")); err != nil {
					t.Fatalf("link %s to %s: %v", filepath.Join(piTesterAgentDir(home), "extensions"), filepath.Join(written, ".pi", "agent", "extensions"), err)
				}

				return home
			},
			why: linkedAbove,
		},
		{
			name: "the deck's directory is empty",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")

				if err := os.MkdirAll(piTesterDeckDir(home), 0o700); err != nil {
					t.Fatalf("create %s: %v", piTesterDeckDir(home), err)
				}

				return home
			},
			why: noIndex,
		},
		{
			name: "the deck's directory lies in the working directory",
			home: func(t *testing.T, _, project string) string {
				t.Helper()

				home := resolvePiTesterDir(t, project)
				deckFile(t, home, "index.ts")

				return home
			},
			why:   inUnit,
			agent: true,
		},
		{
			name: "the deck's directory lies below the working directory",
			home: func(t *testing.T, _, project string) string {
				t.Helper()

				home := filepath.Join(resolvePiTesterDir(t, project), "home")
				deckFile(t, home, "index.ts")

				return home
			},
			why:   inUnit,
			agent: true,
		},
		{
			name: "pi's working directory is a link to the folder the deck's directory lies in",
			// Only the physical in-unit check reaches this one: pi starts in a link to the folder the deck's directory
			// lies in, and HOME's `.` keeps the deck's spelling off the shell's resolved working directory.
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				deckFile(t, home, "index.ts")

				if err := os.Symlink(filepath.Join(piTesterAgentDir(home), "extensions"), filepath.Join(root, "unit-link")); err != nil {
					t.Fatalf("link %s to %s, the folder the deck's directory lies in: %v", filepath.Join(root, "unit-link"), filepath.Join(piTesterAgentDir(home), "extensions"), err)
				}

				return filepath.Dir(home) + "/./" + filepath.Base(home)
			},
			dir: func(_ *testing.T, root, _, _ string) string {
				return filepath.Join(root, "unit-link")
			},
			why: inUnit,
		},
		{
			name: "the deck holds only index.ts",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				deckFile(t, home, "index.ts")

				return home
			},
		},
		{
			name: "the deck holds only index.js",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				deckFile(t, home, "index.js")

				return home
			},
		},
		{
			name: "the deck holds only package.json",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				deckFile(t, home, "package.json")

				return home
			},
		},
		{
			name: "the deck holds only a symbolic link to an index.ts",
			home: func(t *testing.T, root, _ string) string {
				t.Helper()

				home := filepath.Join(root, "home")
				target := filepath.Join(root, "an-index.ts")
				writeFileMode(t, target, []byte("// an extension the deck did not write (issue #702)\n"), 0o644)

				if err := os.MkdirAll(piTesterDeckDir(home), 0o700); err != nil {
					t.Fatalf("create %s: %v", piTesterDeckDir(home), err)
				}

				if err := os.Symlink(target, filepath.Join(piTesterDeckDir(home), "index.ts")); err != nil {
					t.Fatalf("link %s to %s: %v", filepath.Join(piTesterDeckDir(home), "index.ts"), target, err)
				}

				return home
			},
			why: noIndex,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			record := filepath.Join(root, "pi-record")

			if err := os.MkdirAll(binDir, 0o700); err != nil {
				t.Fatalf("create %s: %v", binDir, err)
			}

			writePiTesterBin(t, binDir, record)

			installed := writePiTesterInstalled(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			project := writePiTesterProject(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			home := tc.home(t, root, project)

			dir := project
			if tc.dir != nil {
				dir = tc.dir(t, root, home, project)
			}

			stdout, stderr, err := runPiTester(t, installed.script, dir, piTesterArgs, piTesterEnv(binDir, home, piTesterBasePath(t)))

			if tc.why == "" {
				if err != nil {
					t.Fatalf("the wrapper exited non-zero although the deck's directory is whole: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
				}

				got := readPiRecord(t, record)

				if diff := cmp.Diff(piTesterWantArgv(t, installed, home, project, piTesterArgs), got.argv); diff != "" {
					t.Errorf("pi did not start with the deck's extension by --extension and the rest of the wrapper's flags (-want +got):\n%s", diff)
				}

				return
			}

			if got := exitStatusOf(t, err); got != 1 {
				t.Errorf("the wrapper exited %d with %s, want 1: a deck extension it cannot use is a refusal with one line, not a crash and not a start (issue #702)\nstdout: %s\nstderr: %s",
					got, tc.name, stdout, stderr)
			}

			want := piTesterDeckRefusal(piTesterDeckDir(home), tc.why)
			if tc.agent {
				want = piTesterAgentRefusal(piTesterAgentDir(home), tc.why)
			}

			if got := strings.TrimSpace(stderr); got != want {
				t.Errorf("the wrapper's stderr is %q, want %q: the line names the directory it refused, why, and the step that writes it, so a maintainer whose pane exited can fix it (issue #702)", got, want)
			}

			if _, statErr := os.Stat(record); statErr == nil {
				t.Errorf("the wrapper started pi with %s: without the deck's extension the pane gets no task and its work-done is not delivered, a silent stall the refusal exists to replace (issue #702)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}
		})
	}
}

// TestPiTesterRefusesAnAgentDirectoryThatCouldBeTheUnitFolder pins pi's agent directory check (issue #702):
// ${HOME}/.pi/agent holds the settings, the models and the system prompt pi trusts whatever --no-approve and
// --no-extensions say, so a symlinked ~/.pi or ~/.pi/agent, a missing agent directory, or one that is the unit folder
// a branch writes must stop the tester. A string comparison against PWD alone misses the last one: a HOME spelled
// `<parent>/./<unit>/h`, `<parent>//<unit>/h` or `<parent>/beside/../<unit>/h` - the dots in or beside the unit
// folder's own path, which a string prefix of it passes - or a unit folder reached through a link, is physically the
// unit folder and textually not. The wrapper fails closed with one line and pi never starts.
func TestPiTesterRefusesAnAgentDirectoryThatCouldBeTheUnitFolder(t *testing.T) {
	t.Parallel()

	const (
		symlinked = "is reached through a symbolic link"
		missing   = "is missing or not a directory"
		inUnit    = "lies in the working directory, the unit folder"
	)

	cases := []struct {
		name string
		// layout lays pi's agent directory out as the case leaves it, and returns the directory pi starts in - the
		// unit folder, or a link to it - and the HOME the wrapper is handed, spelled as the case needs it.
		layout func(t *testing.T, root, project string) (dir, home string)
		// why is the condition the refusal line names.
		why string
	}{
		{
			name: "HOME's .pi is a symbolic link",
			layout: func(t *testing.T, root, project string) (string, string) {
				t.Helper()

				home := filepath.Join(root, "home")
				written := filepath.Join(root, "written-by-pi")
				writePiTesterDeck(t, written)

				if err := os.MkdirAll(home, 0o700); err != nil {
					t.Fatalf("create %s: %v", home, err)
				}

				if err := os.Symlink(filepath.Join(written, ".pi"), filepath.Join(home, ".pi")); err != nil {
					t.Fatalf("link %s to %s: %v", filepath.Join(home, ".pi"), filepath.Join(written, ".pi"), err)
				}

				return project, home
			},
			why: symlinked,
		},
		{
			name: "HOME's .pi/agent is a symbolic link",
			layout: func(t *testing.T, root, project string) (string, string) {
				t.Helper()

				home := filepath.Join(root, "home")
				written := filepath.Join(root, "written-by-pi")
				writePiTesterDeck(t, written)

				if err := os.MkdirAll(filepath.Join(home, ".pi"), 0o700); err != nil {
					t.Fatalf("create %s: %v", filepath.Join(home, ".pi"), err)
				}

				if err := os.Symlink(filepath.Join(written, ".pi", "agent"), piTesterAgentDir(home)); err != nil {
					t.Fatalf("link %s to %s: %v", piTesterAgentDir(home), filepath.Join(written, ".pi", "agent"), err)
				}

				return project, home
			},
			why: symlinked,
		},
		{
			name: "HOME's .pi/agent is missing",
			layout: func(t *testing.T, root, project string) (string, string) {
				t.Helper()

				home := filepath.Join(root, "home")
				if err := os.MkdirAll(home, 0o700); err != nil {
					t.Fatalf("create %s: %v", home, err)
				}

				return project, home
			},
			why: missing,
		},
		{
			name: "HOME is spelled with a . in the unit folder's own path",
			layout: func(t *testing.T, _, project string) (string, string) {
				t.Helper()

				writePiTesterDeck(t, filepath.Join(project, "h"))

				return project, filepath.Dir(project) + "/./" + filepath.Base(project) + "/h"
			},
			why: inUnit,
		},
		{
			name: "HOME is spelled with a // in the unit folder's own path",
			layout: func(t *testing.T, _, project string) (string, string) {
				t.Helper()

				writePiTesterDeck(t, filepath.Join(project, "h"))

				return project, filepath.Dir(project) + "//" + filepath.Base(project) + "/h"
			},
			why: inUnit,
		},
		{
			name: "HOME is spelled with a .. beside the unit folder",
			layout: func(t *testing.T, _, project string) (string, string) {
				t.Helper()

				writePiTesterDeck(t, filepath.Join(project, "h"))

				// The step out has to leave from a real directory beside the unit folder, or `cd -P` would fail on the
				// way and the wrapper would refuse for that reason instead of the physical one. Stepping out and back in
				// is what a string prefix of the unit folder passes: `<unit>/../<unit>/h` starts with `<unit>/`.
				beside := filepath.Join(filepath.Dir(project), "beside")
				if err := os.MkdirAll(beside, 0o700); err != nil {
					t.Fatalf("create %s, the directory the .. spelling steps out of: %v", beside, err)
				}

				return project, beside + "/../" + filepath.Base(project) + "/h"
			},
			why: inUnit,
		},
		{
			name: "the unit folder is reached through a symbolic link",
			layout: func(t *testing.T, root, project string) (string, string) {
				t.Helper()

				link := filepath.Join(root, "unit-link")
				if err := os.Symlink(project, link); err != nil {
					t.Fatalf("link %s to %s, the unit folder the shell started in resolves to: %v", link, project, err)
				}

				home := filepath.Join(link, "h")
				writePiTesterDeck(t, home)

				return link, home
			},
			why: inUnit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			record := filepath.Join(root, "pi-record")

			if err := os.MkdirAll(binDir, 0o700); err != nil {
				t.Fatalf("create %s: %v", binDir, err)
			}

			writePiTesterBin(t, binDir, record)

			installed := writePiTesterInstalled(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			project := writePiTesterProject(t, root,
				piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
				piFile{present: true, content: readRepoFile(t, denyListFile)})

			dir, home := tc.layout(t, root, project)

			stdout, stderr, err := runPiTester(t, installed.script, dir, piTesterArgs, piTesterEnv(binDir, home, piTesterBasePath(t)))

			if got := exitStatusOf(t, err); got != 1 {
				t.Errorf("the wrapper exited %d with %s, want 1: an agent directory pi always trusts is a refusal with one line, not a crash and not a start (issue #702)\nstdout: %s\nstderr: %s",
					got, tc.name, stdout, stderr)
			}

			if got, want := strings.TrimSpace(stderr), piTesterAgentRefusal(piTesterAgentDir(home), tc.why); got != want {
				t.Errorf("the wrapper's stderr is %q, want %q: the line names the agent directory it refused and why, so a maintainer whose pane exited knows what to fix, and it has to win over the deck extension's own checks, which come after it (issue #702)", got, want)
			}

			if _, statErr := os.Stat(record); statErr == nil {
				t.Errorf("the wrapper started pi with %s: pi trusts the settings and extensions of the agent directory it finds there, and a branch could write one inside the unit folder (issue #702)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}
		})
	}
}
