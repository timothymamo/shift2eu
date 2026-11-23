// Pins the whole launcher, from ci/auditrun's entry point to the real Claude Code
// launch, under a temporary root (ADR 0040's model-job amendment, issue #277,
// "The audit's programs"): the run's six steps are the real ones, so the tarball's
// integrity is checked, the binary is extracted, the audit directory is checked
// and the preflight starts the CLI - a fake `claude` this test packs, which records
// the environment, the arguments and the system prompt file it was given. A
// managed-settings file under the root stops the run with exit 3 before the fake
// ever runs.
//
// One of those runs is a subprocess: the test binary re-executes itself with the
// four variables an earlier step could set through $GITHUB_ENV, and a sentinel
// beside them, in its own environment, and the fake CLI that run reaches
// inherits none of them.
//
// Bubblewrap and the Observer are the fakes: the start check runs a real `bwrap`,
// and a default test starts no sandbox; the Observer the preflight takes starts no
// `sudo` and dials no socket, so a default test probes nothing of the runner's
// either. Nothing here reaches a cloud, needs a credential or runs a command of
// the runner's: the CLI is the test's own script.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// The audit directory's paths under the root that stands in for /, as ADR 0040
// lays them out.
var auditDirs = [][]string{
	{"srv"},
	{"srv", "audit"},
	{"srv", "audit", "checkout"},
	{"srv", "audit", "checkout", ".git"},
	{"srv", "audit", "checks"},
	{"srv", "audit", "work"},
	{"srv", "audit", "work", "tmp"},
	{"srv", "audit", "state"},
	{"srv", "audit", "state", "home"},
	{"srv", "audit", "state", "tmp"},
	{"srv", "audit", "state", "bin"},
	{"srv", "audit", "state", "out"},
	{"srv", "audit", "bin"},
}

// launcherVariables are the names the launcher sets in the CLI's environment: the
// ADR's allow list and the three entries the audit adds.
var launcherVariables = map[string]bool{
	"CLAUDE_CODE_OAUTH_TOKEN": true,
	"HOME":                    true,
	"PATH":                    true,
	"DISABLE_AUTOUPDATER":     true,
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": true,
	"DISABLE_TELEMETRY":                        true,
	"DISABLE_ERROR_REPORTING":                  true,
	"TMPDIR":                                   true,
	"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB":         true,
	"SHIFT2EU_AUDIT_CANARY":                    true,
}

// shellVariables are the names a POSIX shell sets for itself in the environment it
// hands the commands it runs, beside the ones it inherited.
var shellVariables = map[string]bool{"PWD": true, "SHLVL": true, "_": true, "OLDPWD": true}

// stepsEnvironment are the variables an earlier job step can set through
// $GITHUB_ENV, one per line of that file: each would change what the CLI does,
// and none may reach it. The values are invented, and name documentation hosts
// and paths (CLAUDE.md rule 2).
var stepsEnvironment = []struct {
	name  string
	value string
}{
	{"ANTHROPIC_BASE_URL", "https://api.example.invalid"},
	{"HTTPS_PROXY", "http://192.0.2.10:3128"},
	{"NODE_OPTIONS", "--require /srv/audit/work/invented-node-options.js"},
	{"LD_PRELOAD", "/srv/audit/work/invented-preload.so"},
}

// The sentinel the subprocess test sets beside the four: a name the launcher
// never sets, so its presence in the CLI's environment would prove the CLI was
// handed the launcher's own environment and not the allow list.
const (
	sentinelName  = "SHIFT2EU_AUDIT_SENTINEL"
	sentinelValue = "an-invented-sentinel-value-never-a-credential"
)

// observed is the fake Effects: a run that reached none of a probe's targets,
// which is what a sandbox that holds leaves behind.
type observed struct{}

// ProbeFilesExist reports whether either file probe 1 writes was written.
func (observed) ProbeFilesExist() bool { return false }

// ListenerAccepted reports whether probe 2's listener accepted a connection.
func (observed) ListenerAccepted() bool { return false }

// DockerSocketReachable reports whether the launcher reached Docker's socket.
func (observed) DockerSocketReachable() bool { return false }

// SudoAvailable reports whether `sudo -n true` ran.
func (observed) SudoAvailable() bool { return false }

// GitHubEnvDigestChanged reports whether the $GITHUB_ENV file changed.
func (observed) GitHubEnvDigestChanged() bool { return false }

// observe is the Observer the run's preflight takes: it starts no sudo and dials
// no socket, so a default test runs neither.
func observe(context.Context, string, string, [32]byte, bool) audit.Effects { return observed{} }

// claudeFakeScript returns the fake CLI: it records its environment, its
// arguments and the file either system prompt flag names, prints nothing and exits
// 0. A run that printed no transcript is a preflight whose CLI run failed, which is
// what the run's line must say.
func claudeFakeScript(record string) string {
	return fmt.Sprintf(`#!/bin/sh
record=%q
env > "$record/env"
for a in "$@"; do printf '%%s\n' "$a"; done > "$record/args"
prev=
for a in "$@"; do
	case "$prev" in
	--system-prompt-file|--append-system-prompt-file) printf '%%s\n' "$a" > "$record/system-path" ;;
	esac
	prev="$a"
done
exit 0
`, record)
}

// packClaude packs script as the tarball's only entry, package/claude, and returns
// the tarball's path and the npm integrity the pin names.
func packClaude(t *testing.T, dir, script string) (string, string) {
	t.Helper()

	var packed bytes.Buffer

	gzipWriter := gzip.NewWriter(&packed)
	tarWriter := tar.NewWriter(gzipWriter)

	body := []byte(script)

	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     "package/claude",
		Mode:     0o755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write the tarball's entry: %v", err)
	}

	if _, err := tarWriter.Write(body); err != nil {
		t.Fatalf("write the tarball's binary: %v", err)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close the tarball: %v", err)
	}

	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close the tarball's gzip stream: %v", err)
	}

	path := filepath.Join(dir, "claude-code.tgz")

	if err := os.WriteFile(path, packed.Bytes(), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	sum := sha512.Sum512(packed.Bytes())

	return path, "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// writeAuditTree lays the audit directory out under root as the model-free step
// leaves it: every directory of the ADR's table, the wrapper at bin/audit-exec,
// and a checkout copy, checks' results and bin/ with every write bit removed,
// the checkout's own git configuration included.
func writeAuditTree(t *testing.T, root string) {
	t.Helper()

	for _, parts := range auditDirs {
		path := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(path, 0o750); err != nil {
			t.Fatalf("make %s: %v", path, err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "srv", "audit", "bin", "audit-exec"), []byte("#!/bin/sh\nexit 126\n"), 0o555); err != nil { //nolint:gosec // G306: the wrapper's mode is the ADR's 0555, runnable and writable by nobody
		t.Fatalf("write the wrapper: %v", err)
	}

	gitConfig := filepath.Join(root, "srv", "audit", "checkout", ".git", "config")

	benign := "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n"

	if err := os.WriteFile(gitConfig, []byte(benign), 0o444); err != nil { //nolint:gosec // G306: the checkout copy is read-only
		t.Fatalf("write %s: %v", gitConfig, err)
	}

	// The checkout copy and the checks' results are read-only to the model,
	// their contents too: the model-free step removes every write bit, the
	// checkout's own .git included, and CheckLayout holds the whole tree to it.
	// bin/ is read-only as well, and holds the wrapper alone: the wrapper is
	// written above, before the write bit goes.
	for _, tree := range []string{
		filepath.Join(root, "srv", "audit", "checkout"),
		filepath.Join(root, "srv", "audit", "checks"),
		filepath.Join(root, "srv", "audit", "bin"),
	} {
		makeReadOnly(t, tree)
	}

	// A read-only directory cannot be emptied: the test puts the write bits
	// back before the temporary directory is removed.
	t.Cleanup(func() { makeWritable(t, root) })
}

// makeReadOnly removes every write bit from the tree at path, a symbolic link
// followed nowhere: the mode the model-free step leaves the checkout copy and
// the checks' results in, every directory and file under them included.
func makeReadOnly(t *testing.T, path string) {
	t.Helper()

	err := filepath.WalkDir(path, func(entry string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return os.Chmod(entry, info.Mode().Perm()&^0o222) //nolint:gosec // G122: the tree is this test's own temporary directory, walked so that every entry's mode is the one the layout expects
	})
	if err != nil {
		t.Fatalf("remove the write bits under %s: %v", path, err)
	}
}

// makeWritable gives every entry of the tree at path a write bit, a symbolic link
// followed nowhere.
func makeWritable(t *testing.T, path string) {
	t.Helper()

	err := filepath.WalkDir(path, func(entry string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return os.Chmod(entry, info.Mode().Perm()|0o200) //nolint:gosec // G302: the owner's write bit, put back so the test's tree can be removed
	})
	if err != nil {
		t.Errorf("make the tree under %s writable: %v", path, err)
	}
}

// runTree returns a temporary root laid out as the model-free step leaves it, the
// model step's environment and the run's arguments, with the fake CLI packed into
// the tarball the install step kept.
func runTree(t *testing.T, script string) (root string, env map[string]string, args []string) {
	t.Helper()

	root = t.TempDir()
	writeAuditTree(t, root)

	tarball, integrity := packClaude(t, root, script)

	step := t.TempDir()
	env = stepEnvironment(t, step)
	args = []string{"-claude-tarball", tarball, "-claude-integrity", integrity}

	return root, env, args
}

// runStepsAt runs the entry point under root with the real steps, bubblewrap
// being the fake a default test gives it, and returns its exit code and what it
// printed.
func runStepsAt(t *testing.T, root string, args []string, env map[string]string) runResult {
	t.Helper()

	lookup := &environ{values: env}

	var stdout, stderr bytes.Buffer

	steps := audit.SystemSteps(os.Getpid(), observe)
	steps.Bubblewrap = func(context.Context) error { return nil }

	code := runContext(t.Context(), args, lookup.lookup, steps, root, &stdout, &stderr)

	return runResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// recordedEnv returns the environment the fake CLI ran with, as name → value. A
// failure message of this file names a variable, never its value: a regression
// could put a credential of the caller's in one, and a test log is no place for
// it (CLAUDE.md rule 3, issue #277, round F finding 4).
func recordedEnv(t *testing.T, record string) map[string]string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(record, "env"))
	if err != nil {
		t.Fatalf("read the fake CLI's environment: %v; the run never reached it", err)
	}

	values := map[string]string{}

	for _, line := range strings.Split(string(body), "\n") {
		if line == "" {
			continue
		}

		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("the fake CLI's environment holds a line for %q that is no name=value pair, want name=value", name)
		}

		values[name] = value
	}

	return values
}

// TestRunReachesTheFakeCLIUnderATemporaryRoot pins the ADR's whole-launcher test:
// the real steps carry a run from the tarball's integrity to the CLI's launch
// under a temporary root, and the environment the fake CLI was given is the
// launcher's allow list and the audit's three entries alone - nothing of the
// caller's, the token where the CLI expects it and the canary where probe 5 looks
// for it.
func TestRunReachesTheFakeCLIUnderATemporaryRoot(t *testing.T) {
	t.Parallel()

	record := t.TempDir()
	root, env, args := runTree(t, claudeFakeScript(record))

	result := runStepsAt(t, root, args, env)

	// The fake prints no transcript, so the preflight's run fails: what the run's
	// exit code says is that it got as far as the CLI, not that its own inputs or
	// its tree were refused.
	if result.code != audit.ExitFailed {
		t.Fatalf("the run's exit code is %d, want %d (audit.ExitFailed): the fake CLI printed no result\nstdout: %s\nstderr: %s",
			result.code, audit.ExitFailed, result.stdout, result.stderr)
	}

	assertTheAllowListAlone(t, recordedEnv(t, record), root)

	// The audit's system prompt is the checkout copy's skill, and the file that
	// carries it lies in the state area's tmp/, which the caller names, not in
	// whatever temporary directory the process's TMPDIR pointed at (issue #277,
	// review R11).
	systemPath, err := os.ReadFile(filepath.Join(record, "system-path"))
	if err != nil {
		t.Fatalf("read the system prompt's path from the fake CLI: %v", err)
	}

	systemFile := strings.TrimSpace(string(systemPath))
	if systemFile == "" {
		t.Fatalf("the fake CLI was given no --append-system-prompt-file, so the skill never reached it")
	}

	if want := filepath.Join(root, "srv", "audit", "state", "tmp"); filepath.Dir(systemFile) != want {
		t.Errorf("the system prompt file is %q, want it in %q: the audit's launch names the directory (issue #277)", systemFile, want)
	}

	assertNoSecret(t, result)
}

// assertTheAllowListAlone fails when the environment the fake CLI recorded is not
// the launcher's allow list: a name of neither the list nor the shell's own, an
// entry of the list that is missing, and an entry at a value the audit's launch
// does not fix.
func assertTheAllowListAlone(t *testing.T, got map[string]string, root string) {
	t.Helper()

	for name := range got {
		if !launcherVariables[name] && !shellVariables[name] {
			t.Errorf("the CLI's environment holds %s, which is neither the launcher's nor the shell's own: the launcher passes its allow list and inherits nothing (CLAUDE.md rule 3, ADR 0040)", name)
		}
	}

	for _, want := range []struct {
		name  string
		value string
	}{
		{"CLAUDE_CODE_OAUTH_TOKEN", auditToken},
		{"HOME", filepath.Join(root, "srv", "audit", "state", "home")},
		{"PATH", "/usr/bin:/bin"},
		{"TMPDIR", filepath.Join(root, "srv", "audit", "state", "tmp")},
		{"SHIFT2EU_AUDIT_CANARY", auditCanary},
		{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB", "1"},
	} {
		if got[want.name] != want.value {
			t.Errorf("the CLI's %s is not the value the audit's launch fixes, want %q (set: %t): the audit's launch decides it (ADR 0040)", want.name, want.value, got[want.name] != "")
		}
	}
}

// TestRunStopsAtAManagedSettingsFile pins the other half of the ADR's test: a
// managed-settings directory under the root stops the run with exit 3 before the
// CLI starts, so the fake records nothing.
func TestRunStopsAtAManagedSettingsFile(t *testing.T) {
	t.Parallel()

	record := t.TempDir()
	root, env, args := runTree(t, claudeFakeScript(record))

	managedSettings := filepath.Join(root, "etc", "claude-code")
	if err := os.MkdirAll(managedSettings, 0o750); err != nil {
		t.Fatalf("make %s: %v", managedSettings, err)
	}

	result := runStepsAt(t, root, args, env)

	if result.code != audit.ExitRefused {
		t.Errorf("the run's exit code is %d, want %d (audit.ExitRefused): a managed-settings directory exists under the root\nstdout: %s\nstderr: %s",
			result.code, audit.ExitRefused, result.stdout, result.stderr)
	}

	if _, err := os.Lstat(filepath.Join(record, "env")); !os.IsNotExist(err) {
		t.Errorf("the fake CLI ran (%v), want it never to start: the managed settings refuse the run before the CLI does", err)
	}

	assertNoSecret(t, result)
}

// TestRunInheritsNoneOfTheStepsEnvironment pins the other half of the ADR's
// launcher test: the launcher runs as a subprocess whose own environment holds the
// four variables an earlier step can set through $GITHUB_ENV, and a sentinel
// beside them, and the fake CLI that run reaches inherits none of them - its
// environment is the launcher's allow list and the shell's own names alone.
func TestRunInheritsNoneOfTheStepsEnvironment(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAuditTree(t, root)

	record := t.TempDir()
	tarball, integrity := packClaude(t, root, claudeFakeScript(record))

	dir := t.TempDir()
	params := filepath.Join(dir, "params.json")
	writeLauncherParams(t, params, launcherParams{
		Root:      root,
		Tarball:   tarball,
		Integrity: integrity,
		Step:      stepEnvironment(t, dir),
	})

	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary to re-execute as the launcher: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), binary, "-test.run=^TestLauncherSubprocessEntryPoint$", params)

	// The subprocess's whole environment: the four variables and the sentinel,
	// and nothing of this test's (CLAUDE.md rule 3). The subprocess is this
	// test's own, started and waited for here (rule 10).
	cmd.Env = make([]string, 0, len(stepsEnvironment)+1)
	for _, variable := range stepsEnvironment {
		cmd.Env = append(cmd.Env, variable.name+"="+variable.value)
	}

	cmd.Env = append(cmd.Env, sentinelName+"="+sentinelValue)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("run the launcher in a subprocess: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	got := recordedEnv(t, record)

	for _, variable := range stepsEnvironment {
		if _, ok := got[variable.name]; ok {
			t.Errorf("the CLI's environment holds %s, which the launcher's own environment set: a variable an earlier step wrote to $GITHUB_ENV must not reach the CLI (CLAUDE.md rule 3, ADR 0040)", variable.name)
		}
	}

	if _, ok := got[sentinelName]; ok {
		t.Errorf("the CLI's environment holds the sentinel %s: the CLI was handed the launcher's own environment, not its allow list (ADR 0040)", sentinelName)
	}

	assertTheAllowListAlone(t, got, root)
}

// launcherParams is what the test above hands the re-executed test binary: the
// temporary root the real steps run under, the tarball and its integrity, and the
// step's own four inputs.
type launcherParams struct {
	Root      string            `json:"root"`
	Tarball   string            `json:"tarball"`
	Integrity string            `json:"integrity"`
	Step      map[string]string `json:"step"`
}

// writeLauncherParams writes params to path as JSON, for the subprocess to read.
func writeLauncherParams(t *testing.T, path string, params launcherParams) {
	t.Helper()

	body, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode the subprocess's params: %v", err)
	}

	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readLauncherParams reads the params the test above wrote.
func readLauncherParams(t *testing.T, path string) launcherParams {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the subprocess's params at %s: %v", path, err)
	}

	var params launcherParams
	if err := json.Unmarshal(body, &params); err != nil {
		t.Fatalf("decode the subprocess's params at %s: %v", path, err)
	}

	return params
}

// TestLauncherSubprocessEntryPoint is the launcher the test above re-executes the
// test binary as: the real steps under a temporary root, with the environment the
// subprocess was started in setting none of its inputs (they come from the params
// file, a lookup function of the test's own). Run without that file, by the
// package's ordinary test run, there is nothing to launch and it skips.
func TestLauncherSubprocessEntryPoint(t *testing.T) {
	if len(flag.Args()) != 1 {
		t.Skip("the launcher's subprocess entry point: TestRunInheritsNoneOfTheStepsEnvironment starts it with its params file")
	}

	params := readLauncherParams(t, flag.Args()[0])

	steps := audit.SystemSteps(os.Getpid(), observe)
	steps.Bubblewrap = func(context.Context) error { return nil }

	var stdout, stderr bytes.Buffer

	code := runContext(t.Context(),
		[]string{"-claude-tarball", params.Tarball, "-claude-integrity", params.Integrity},
		(&environ{values: params.Step}).lookup, steps, params.Root, &stdout, &stderr)

	// The fake prints no transcript, so the preflight's CLI run fails: reaching
	// it is what this run is for.
	if code != audit.ExitFailed {
		t.Fatalf("the run's exit code is %d, want %d (audit.ExitFailed): the fake CLI printed no result\nstdout: %s\nstderr: %s",
			code, audit.ExitFailed, stdout.String(), stderr.String())
	}
}
