// Pins the tester's launch wrapper dev/pi-tester.sh (issue #662, ADR 0044): .dot-agent-deck.toml's tester role
// calls it instead of pi, so the tester starts with only what it needs - its DeepSeek credential and the
// non-secret basics - and never with another agent's key or a GitHub token (CLAUDE.md rule 3). The wrapper builds
// pi's environment from an ALLOW LIST, not a deny list: a variable it does not name is absent whatever its name,
// and SHIFT2EU_FAKE_SECRET_662 is the name no list of credentials knows, so its absence is what tells the two
// apart.
//
// The wrapper also fails closed. pi logs an extension that fails to load and carries on without it, so the
// env-printing refusal (ADR 0044) fails open on its own: before it starts pi, the wrapper checks the two files
// the refusal is made of, relative to the working directory pi loads the project from, and exits non-zero naming
// the file when one is missing, empty or not JSON - pi is not started then at all.
//
// Every value here is invented (CLAUDE.md rule 3): the wrapper's tests hand it an environment they build
// themselves, never the machine's, and the fake pi records the names of the variables it saw, never a value.
package devenv_test

import (
	"bytes"
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

// piTesterScript is the launch wrapper the tester role runs instead of pi, and the two files pi loads for the
// env-printing refusal (ADR 0044), relative to the directory pi starts in - the tester's working directory.
const (
	piTesterScript    = "dev/pi-tester.sh"
	denyExtensionFile = ".pi/extensions/deny-env-printing.ts"
	denyListFile      = ".pi/deny-env-printing.json"
)

// deckConfigPath is the deck's configuration, whose tester role names the command the wrapper is for.
const deckConfigPath = ".dot-agent-deck.toml"

// The deck's tester role, by name.
const deckTesterRole = "tester"

// piTesterKept are the variables the wrapper must hand pi when the parent has them: pi's DeepSeek credential
// (docs/adr/0003-pi-with-deepseek-for-the-tester-role.md) and the non-secret basics.
var piTesterKept = []string{"DEEPSEEK_API_KEY", "HOME", "PATH", "TERM", "LANG"}

// piTesterDropped are the variables the wrapper must keep out of pi's environment even when the parent has them:
// another agent's key, both GitHub token variables, and a name no list of credentials knows. The last one is the
// point: a deny list keeps these three out by naming them, and only an allow list keeps out a name it never heard
// of.
var piTesterDropped = []string{
	"GH_TOKEN",
	"GITHUB_TOKEN",
	"ANTHROPIC_API_KEY",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"SHIFT2EU_FAKE_SECRET_662",
}

// piTesterArgs are the arguments .dot-agent-deck.toml's tester command passes the wrapper, which must reach pi
// unchanged.
var piTesterArgs = []string{"--provider", "deepseek", "--model", "deepseek-flash", "--thinking", "high", "--approve"}

// fakePiScript is the stand-in for pi: it records its argv and the names of the variables of its environment -
// never a value, which these tests invent anyway (CLAUDE.md rule 3) - and exits 0. The record's path is written
// into it, because the wrapper's environment is an allow list and cannot carry a variable for the fake.
const fakePiScript = `#!/bin/sh
# Stands in for pi in the tests of the tester's launch wrapper (issue #662).
{
	for a in "$@"; do printf 'arg=%%s\n' "$a"; done
	printf '%s\n'
	env | cut -d= -f1 | sort
} > '%s'
`

// piRecordEnvMarker separates the fake pi's argv from the names of the variables it saw.
const piRecordEnvMarker = "env-names:"

// piFile is one of the two files the env-printing refusal is made of, as a case leaves it: present with content,
// or not written at all.
type piFile struct {
	present bool
	content string
}

// piRecord is what the fake pi saw: its argv and the names of the variables of its environment.
type piRecord struct {
	argv     []string
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
		case strings.HasPrefix(line, "arg="):
			record.argv = append(record.argv, strings.TrimPrefix(line, "arg="))
		}
	}

	return record
}

// writeFakePi writes the stand-in for pi first on the wrapper's PATH, recording into record.
func writeFakePi(t *testing.T, binDir, record string) {
	t.Helper()

	script := fmt.Sprintf(fakePiScript, piRecordEnvMarker, record)
	writeFileMode(t, filepath.Join(binDir, "pi"), []byte(script), 0o700)
}

// piTesterEnv is the environment the wrapper is handed, and so the only one pi could inherit: the parent values
// of the variables the wrapper keeps, and invented values of the ones it must drop, so their absence from pi's
// environment is the wrapper's doing and not the test machine's (CLAUDE.md rule 3).
func piTesterEnv(binDir, homeDir string) map[string]string {
	env := map[string]string{
		// The PATH of the VM's agents, with the fake pi first: every other tool the wrapper may need to check
		// the refusal's files (jq, which dev/lima.yaml installs, is in /usr/bin) is where the VM keeps it.
		"PATH": binDir + ":/usr/local/bin:/usr/bin:/bin",
		"HOME": homeDir,
		"TERM": "xterm-256color",
		"LANG": "C.UTF-8",
	}

	for _, name := range append(slices.Clone(piTesterDropped), "DEEPSEEK_API_KEY") {
		env[name] = fakeEnvValue(name)
	}

	return env
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

// writePiTesterProject creates the directory pi starts in, writes the two files the env-printing refusal is made
// of as the case leaves them, and returns the directory.
func writePiTesterProject(t *testing.T, root string, extension, list piFile) string {
	t.Helper()

	dir := filepath.Join(root, "project")

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}

	for _, file := range []struct {
		path string
		piFile
	}{
		{path: denyExtensionFile, piFile: extension},
		{path: denyListFile, piFile: list},
	} {
		if !file.present {
			continue
		}

		writeFileMode(t, filepath.Join(dir, file.path), []byte(file.content), 0o600)
	}

	return dir
}

// TestPiTesterHandsPiAnAllowListedEnvironment pins the first half of issue #662: every argument reaches pi
// unchanged, the DeepSeek credential and the non-secret basics reach it, and every other credential the wrapper
// was handed does not - the unknown name above all, which only an allow list keeps out.
func TestPiTesterHandsPiAnAllowListedEnvironment(t *testing.T) {
	t.Parallel()

	script := scriptPath(t, piTesterScript)
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

	project := writePiTesterProject(t, root,
		piFile{present: true, content: readRepoFile(t, denyExtensionFile)},
		piFile{present: true, content: readRepoFile(t, denyListFile)})

	stdout, stderr, err := runPiTester(t, script, project, piTesterArgs, piTesterEnv(binDir, homeDir))
	if err != nil {
		t.Fatalf("the wrapper exited non-zero with the env-printing refusal's two files in place: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	got := readPiRecord(t, record)

	if diff := cmp.Diff(piTesterArgs, got.argv); diff != "" {
		t.Errorf("the wrapper did not pass its arguments to pi unchanged, so the tester runs with another provider, model or mode than the deck's command names (-want +got):\n%s", diff)
	}

	for _, name := range piTesterKept {
		if !slices.Contains(got.envNames, name) {
			t.Errorf("pi's environment holds no %s, although the wrapper was handed one: pi cannot reach its DeepSeek credential or the basics it starts with (issue #662, CLAUDE.md rule 3)", name)
		}
	}

	for _, name := range piTesterDropped {
		if slices.Contains(got.envNames, name) {
			t.Errorf("pi's environment holds %s, although the wrapper was handed it: the tester must start with only what it needs, so no GitHub token, no other agent's key and no variable the wrapper does not know reaches it (issue #662, CLAUDE.md rule 3)", name)
		}
	}

	// Neither stream may carry a value of the environment the wrapper was handed: a wrapper that echoes what it
	// drops writes a credential into the terminal's scrollback, a CI log or a screenshot (CLAUDE.md rule 3).
	var secrets []secret

	for _, name := range append(slices.Clone(piTesterDropped), "DEEPSEEK_API_KEY") {
		secrets = append(secrets, secret{kind: "value of " + name, value: fakeEnvValue(name)})
	}

	assertNoSecrets(t, "the wrapper's stdout", stdout, secrets)
	assertNoSecrets(t, "the wrapper's stderr", stderr, secrets)
}

// TestPiTesterRefusesToStartPiWithoutTheDenyExtension pins the second half of issue #662: the check of the two
// files the env-printing refusal is made of, and that a failed check stops the tester instead of letting it run
// without the refusal (ADR 0044 fails open on its own, so the wrapper is what fails closed). A missing or empty
// extension and a missing or malformed list each exit non-zero, name the file, and never start pi.
func TestPiTesterRefusesToStartPiWithoutTheDenyExtension(t *testing.T) {
	t.Parallel()

	script := scriptPath(t, piTesterScript)

	extension := piFile{present: true, content: readRepoFile(t, denyExtensionFile)}
	list := piFile{present: true, content: readRepoFile(t, denyListFile)}

	cases := []struct {
		name    string
		ext     piFile
		list    piFile
		refuses bool
		// wantFile is the file a refusal must name, so a maintainer reading the pane knows which one to fix.
		wantFile string
	}{
		{name: "both files as the repository has them", ext: extension, list: list},
		{name: "the extension is missing", list: list, refuses: true, wantFile: "deny-env-printing.ts"},
		{name: "the extension is empty", ext: piFile{present: true}, list: list, refuses: true, wantFile: "deny-env-printing.ts"},
		{name: "the list is missing", ext: extension, refuses: true, wantFile: "deny-env-printing.json"},
		{name: "the list is not JSON", ext: extension, list: piFile{present: true, content: `{"deny": [`}, refuses: true, wantFile: "deny-env-printing.json"},
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

			writeFakePi(t, binDir, record)

			project := writePiTesterProject(t, root, tc.ext, tc.list)
			env := piTesterEnv(binDir, homeDir)

			stdout, stderr, err := runPiTester(t, script, project, piTesterArgs, env)

			var secrets []secret

			for _, name := range append(slices.Clone(piTesterDropped), "DEEPSEEK_API_KEY") {
				secrets = append(secrets, secret{kind: "value of " + name, value: fakeEnvValue(name)})
			}

			if !tc.refuses {
				if err != nil {
					t.Fatalf("the wrapper exited non-zero with the env-printing refusal's two files in place: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
				}

				got := readPiRecord(t, record)

				if diff := cmp.Diff(piTesterArgs, got.argv); diff != "" {
					t.Errorf("the wrapper did not pass its arguments to pi unchanged (-want +got):\n%s", diff)
				}

				return
			}

			if err == nil {
				t.Fatalf("the wrapper exited 0 with %s: pi must not start without the env-printing refusal, whose load fails open on its own (issue #662, ADR 0044)\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			if _, statErr := os.Stat(record); statErr == nil {
				t.Fatalf("the wrapper started pi with %s: the check of the refusal's files is meant to stop the tester, and pi logs an extension it cannot load and runs without it (issue #662, ADR 0044)\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			if !strings.Contains(stderr, tc.wantFile) {
				t.Errorf("the wrapper's stderr does not name %s, the file it could not use: a message that names no file leaves the maintainer to guess which of the two broke (issue #662): %s", tc.wantFile, stderr)
			}

			assertNoSecrets(t, "the wrapper's stdout", stdout, secrets)
			assertNoSecrets(t, "the wrapper's stderr", stderr, secrets)
		})
	}
}

// deckCommandLine matches the command line of a role block of the deck's configuration, and the command it holds.
var deckCommandLine = regexp.MustCompile(`^command *= *"(.*)"$`)

// TestPiTesterCommandRunsTheLaunchWrapper pins the wiring: the deck's tester role runs the wrapper, not pi, or the
// allow-listed environment and the fail-closed start above never happen (issue #662).
func TestPiTesterCommandRunsTheLaunchWrapper(t *testing.T) {
	t.Parallel()

	command := testerRoleCommand(t)

	if !strings.HasPrefix(command, piTesterScript) {
		t.Errorf("%s: the %s role's command is %q, want it to start with %q: the wrapper is what keeps the tester's environment to its own credential and stops it when the env-printing refusal cannot be loaded (issue #662, ADR 0044)",
			deckConfigPath, deckTesterRole, command, piTesterScript)
	}

	if strings.HasPrefix(command, "pi ") {
		t.Errorf("%s: the %s role's command is %q, which still starts bare pi: the tester then runs with every credential of the deck's environment, and a failed load of the env-printing refusal leaves it running without one (issue #662, ADR 0044)",
			deckConfigPath, deckTesterRole, command)
	}
}

// testerRoleCommand returns the tester role's command from the deck's configuration. The role blocks are split on
// their table header, and the block named `tester` is the one holder of the command.
func testerRoleCommand(t *testing.T) string {
	t.Helper()

	const roleHeader = "[[orchestrations.roles]]"

	blocks := strings.Split(readRepoFile(t, deckConfigPath), roleHeader)

	for _, block := range blocks[1:] {
		if !strings.Contains(block, "\nname = \""+deckTesterRole+"\"") {
			continue
		}

		for _, line := range strings.Split(block, "\n") {
			if match := deckCommandLine.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
				return match[1]
			}
		}

		t.Fatalf("%s: the %s role holds no command line", deckConfigPath, deckTesterRole)
	}

	t.Fatalf("%s: no role block is named %q", deckConfigPath, deckTesterRole)

	return ""
}
