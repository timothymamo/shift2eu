// Pins the LaunchDaemon of the agents App token refresh (issue #229, ADR 0034): `sh dev/agents-token-launchd.sh
// plist` prints the property list launchd loads so a refresh that a reboot or a power cut killed comes back
// without anyone logged in. The job runs the host clone's own refresh script through /bin/sh as the maintainer,
// one label per VM instance, opens its own log as the maintainer (launchd would open StandardOutPath as root),
// and the plist holds the App ID, the installation ID, the key's path and XDG_STATE_HOME when it is set, but
// never the key, a token or a git pull, and keeps its logs out of the instance directory, which launchd would
// otherwise recreate after every removal (issue #229's note from #237). A value that cannot be written into
// the plist, XML markup or a control character included, is refused before anything is printed, with the
// offending variable's name and never the key's contents.
package devenv_test

import (
	"bytes"
	"encoding/xml"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// launchdScript is the plist generator issue #229 adds, next to the refresh it renders the job for.
var launchdScript = filepath.Join("dev", "agents-token-launchd.sh")

// launchdLabelPrefix is the job label's prefix: one LaunchDaemon per VM instance, so two VMs never share a job.
const launchdLabelPrefix = "io.github.timothymamo.shift2eu.agents-token-refresh."

// The sanitised App inputs the plist tests give the script, never a real App ID or installation ID
// (CLAUDE.md rule 2).
const (
	launchdAppID          = "123456"
	launchdInstallationID = "7890123"
)

// launchdFixture is one controlled environment for a run of dev/agents-token-launchd.sh: a sandbox with a
// temporary HOME, a state directory inside it, a bin directory first on PATH and the argv log, an invented
// key, and the four variables the script reads. The test process's own environment never reaches the script,
// so no App ID, key path or state directory of the machine can end up in a plist (CLAUDE.md rule 3).
type launchdFixture struct {
	sb       *sandbox
	key      inventedKey
	env      map[string]string
	instance string
}

// xdgStateHomeMode says how the fixture leaves XDG_STATE_HOME: unset, set to a directory, or set to the empty
// string, which the refresh reads as unset (${XDG_STATE_HOME:-$HOME/.local/state} in dev/agents-app-token.sh and
// in the Makefile).
type xdgStateHomeMode int

const (
	xdgStateHomeUnset xdgStateHomeMode = iota
	xdgStateHomeEmpty
	xdgStateHomeSet
)

// newLaunchdFixture builds the fixture for one VM instance; xdg says how XDG_STATE_HOME is set, so a test can
// pin where the log goes with it, without it, and with it empty.
func newLaunchdFixture(t *testing.T, instance string, xdg xdgStateHomeMode) *launchdFixture {
	t.Helper()

	sb := newSandbox(t)
	key := writeInventedKey(t, filepath.Join(sb.root, "keys", "agents-app.pem"), 0o600)

	env := sb.baseEnv()
	env["LIMA_INSTANCE"] = instance
	env["SHIFT2EU_AGENTS_APP_ID"] = launchdAppID
	env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = launchdInstallationID
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = key.path

	switch xdg {
	case xdgStateHomeSet:
		env["XDG_STATE_HOME"] = filepath.Join(sb.root, "state")
	case xdgStateHomeEmpty:
		env["XDG_STATE_HOME"] = ""
	case xdgStateHomeUnset:
		delete(env, "XDG_STATE_HOME")
	}

	return &launchdFixture{sb: sb, key: key, env: env, instance: instance}
}

// plist runs `sh dev/agents-token-launchd.sh plist` under the fixture's environment alone and returns its
// stdout, its stderr and its status.
func (f *launchdFixture) plist(t *testing.T) (string, string, error) {
	t.Helper()

	return f.sb.run(t, scriptPath(t, launchdScript), []string{"plist"}, f.env, "")
}

// stateDir is `${XDG_STATE_HOME:-$HOME/.local/state}`, where the refresh keeps its log: the same directory
// make agents-token-refresh-start writes it in, and an empty XDG_STATE_HOME counts as no XDG_STATE_HOME, as
// ${XDG_STATE_HOME:-...} says in the Makefile and in dev/agents-app-token.sh.
func (f *launchdFixture) stateDir() string {
	if state := f.env["XDG_STATE_HOME"]; state != "" {
		return state
	}

	return filepath.Join(f.env["HOME"], ".local", "state")
}

// assertNoLeaks fails when the invented key's text reached one of the given streams or a command line the
// script ran; the key's path may appear, its content never (ADR 0031, Secrets).
func (f *launchdFixture) assertNoLeaks(t *testing.T, streams ...string) {
	t.Helper()

	secrets := keySecrets(f.key.pem)

	for _, text := range streams {
		assertNoSecrets(t, "the plist generator's output", text, secrets)
	}

	assertNoSecrets(t, "the argv of the commands the script ran", f.sb.argvLogText(t), secrets)
}

// accountHome creates the account's home directory for a test, as /Users/<account> is on the Mac: the generator
// derives the clone from $0 and HOME from the environment the install hands it, so a home of the test's own
// stands in for the real one, which no test may create. It is 0700 from a root of its own, because t.TempDir is
// 0777 minus the umask: under this VM's umask that is group-writable, and the trusted path rule refuses the
// script's path inside it, rightly and not because of what the test pins (issue #227).
func accountHome(t *testing.T) string {
	t.Helper()

	home := filepath.Join(newSandboxRoot(t), "Users", agentsAccount)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create the account's home %s: %v", home, err)
	}

	return home
}

// launchdRepoRoot is the absolute path of the repository these tests run in, with symlinks resolved the way
// the script's own `cd ... && pwd -P` resolves the dev/ directory it lives in: the path the plist must name in
// ProgramArguments and WorkingDirectory.
func launchdRepoRoot(t *testing.T) string {
	t.Helper()

	abs, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("resolve the symlinks of %s: %v", abs, err)
	}

	return resolved
}

// currentUsername is the account the job runs as: launchd's UserName names the maintainer who runs
// make agents-token-refresh-install, which is the user these tests run as.
func currentUsername(t *testing.T) string {
	t.Helper()

	account, err := user.Current()
	if err != nil {
		t.Fatalf("read the account the tests run as: %v", err)
	}

	return account.Username
}

// TestAgentsTokenLaunchdPlistDescribesTheJob pins the whole of the generated job: its label per instance, the
// maintainer it runs as, the shell line that runs the host clone's own script and opens the log as the
// maintainer (never make and never git), the working directory, the environment it hands the refresh, the
// keep-alive settings and the two log paths.
func TestAgentsTokenLaunchdPlistDescribesTheJob(t *testing.T) {
	t.Parallel()

	repo := launchdRepoRoot(t)

	cases := []struct {
		name     string
		instance string
		xdg      xdgStateHomeMode
	}{
		{name: "the shift2eu instance", instance: "shift2eu", xdg: xdgStateHomeSet},
		{name: "another instance gets another label", instance: "other-repo", xdg: xdgStateHomeSet},
		{name: "an instance whose name holds a dot and an underscore", instance: "shift2eu.local_1", xdg: xdgStateHomeSet},
		{name: "no XDG_STATE_HOME", instance: "shift2eu", xdg: xdgStateHomeUnset},
		{name: "an empty XDG_STATE_HOME is no XDG_STATE_HOME", instance: "shift2eu", xdg: xdgStateHomeEmpty},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdFixture(t, tc.instance, tc.xdg)

			stdout, stderr, err := f.plist(t)
			if err != nil {
				t.Fatalf("sh %s plist exited %v with the four variables set\nstdout: %s\nstderr: %s", launchdScript, err, stdout, stderr)
			}

			dict := parsePlist(t, stdout)

			wantLabel := launchdLabelPrefix + tc.instance
			if got := plistString(t, dict, "Label"); got != wantLabel {
				t.Errorf("the plist's Label is %q, want %q: one LaunchDaemon per VM instance, so two VMs keep two refreshes and a warning about one cannot hide the other (issue #229)", got, wantLabel)
			}

			if got, want := plistString(t, dict, "UserName"), currentUsername(t); got != want {
				t.Errorf("the plist's UserName is %q, want %q: the job runs as the maintainer who installed it, at boot and with nobody logged in (issue #229)", got, want)
			}

			wantLog := filepath.Join(f.stateDir(), "shift2eu", "agents-token-refresh-"+tc.instance+".log")

			// The job opens its own log: launchd opens StandardOutPath as root, and the log lives in a
			// directory the maintainer writes, so a symlink planted there would make root write anywhere. The
			// shell line therefore opens the log as the maintainer, and the script and the log travel as two
			// arguments of their own, spliced into no shell text (issue #229, auditor's finding). It creates the
			// log's directory under umask 077, so a log the job recreates is the maintainer's alone, and the
			// exec still runs even when the directory was missing (second review).
			const openLog = `umask 077 && mkdir -p "${1%/*}" && exec /bin/sh "$0" refresh >>"$1" 2>&1`

			wantArgs := []string{"/bin/sh", "-c", openLog, filepath.Join(repo, "dev", "agents-app-token.sh"), wantLog}
			if diff := cmp.Diff(wantArgs, plistStrings(t, plistList(t, dict, "ProgramArguments"))); diff != "" {
				t.Errorf("the plist's ProgramArguments differ from what launchd would run (-want +got):\n%s\nthe job runs this repository's own refresh through /bin/sh, never make and never git, and the shell line opens the log as the maintainer, with the script and the log as arguments of their own (issue #229, ADR 0034)\nstdout:\n%s", diff, stdout)
			}

			if got := plistString(t, dict, "WorkingDirectory"); got != repo {
				t.Errorf("the plist's WorkingDirectory is %q, want the repository root %q: the job runs the host clone's own script from the host clone (issue #229)", got, repo)
			}

			env := plistMap(t, dict, "EnvironmentVariables")

			wantEnv := []string{
				"HOME",
				"LIMA_INSTANCE",
				"PATH",
				"SHIFT2EU_AGENTS_APP_ID",
				"SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
				"SHIFT2EU_AGENTS_APP_KEY_FILE",
			}

			// XDG_STATE_HOME joins them only when the host clone sets it to a directory: an empty one is no
			// XDG_STATE_HOME (${XDG_STATE_HOME:-...}), so the job then finds the log under $HOME/.local/state.
			if tc.xdg == xdgStateHomeSet {
				wantEnv = append(wantEnv, "XDG_STATE_HOME")
			}

			slices.Sort(wantEnv)

			var gotEnv []string

			for name := range env {
				gotEnv = append(gotEnv, name)
			}

			slices.Sort(gotEnv)

			if diff := cmp.Diff(wantEnv, gotEnv); diff != "" {
				t.Errorf("the plist's EnvironmentVariables hold other names than the ones the refresh reads (-want +got):\n%s\na launchd job inherits no shell environment, so the plist supplies these and nothing else: the six the refresh always needs, and XDG_STATE_HOME only when it names a directory, and no token variable may join them (issue #229)", diff)
			}

			wantValues := map[string]string{
				"LIMA_INSTANCE":                       tc.instance,
				"SHIFT2EU_AGENTS_APP_ID":              launchdAppID,
				"SHIFT2EU_AGENTS_APP_INSTALLATION_ID": launchdInstallationID,
				// The path of the key, never the key: launchd reads the plist as a world-readable file.
				"SHIFT2EU_AGENTS_APP_KEY_FILE": f.key.path,
				"HOME":                         f.env["HOME"],
			}

			if tc.xdg == xdgStateHomeSet {
				wantValues["XDG_STATE_HOME"] = f.env["XDG_STATE_HOME"]
			}

			for name, want := range wantValues {
				if got := plistString(t, env, name); got != want {
					t.Errorf("the plist's EnvironmentVariables %s is %q, want %q (issue #229)", name, got, want)
				}
			}

			// limactl comes from Homebrew, which a launchd job's default PATH does not hold: without its
			// directory the refresh would fail at every check and never reach the VM.
			path := plistString(t, env, "PATH")

			for _, want := range []string{"/opt/homebrew/bin", "/usr/bin"} {
				if !strings.Contains(path, want) {
					t.Errorf("the plist's PATH is %q and holds no %s: limactl comes from Homebrew, and a launchd job's PATH is the plist's (issue #229)", path, want)
				}
			}

			for _, key := range []string{"RunAtLoad", "KeepAlive"} {
				if got := plistKind(t, dict, key); got != "true" {
					t.Errorf("the plist's %s is a <%s>, want <true>: the refresh must start at boot and come back when it is killed (issue #229)", key, got)
				}
			}

			if got := plistKind(t, dict, "ThrottleInterval"); got != "integer" {
				t.Errorf("the plist's ThrottleInterval is a <%s>, want an <integer>: a refresh that keeps failing must not restart more often than that (issue #229)", got)
			}

			throttle, err := strconv.Atoi(plistText(t, dict, "ThrottleInterval"))
			if err != nil {
				t.Errorf("the plist's ThrottleInterval is %q, which is no number of seconds: launchd wants an integer (issue #229)", plistText(t, dict, "ThrottleInterval"))
			}

			// ADR 0034, the runbook and the install's warning ("trying again every 5 minutes") all promise five
			// minutes, so the plist pins exactly that: a shorter interval mints a token per restart against a GitHub
			// that may be the reason the refresh failed, and a longer one keeps a stopped job down longer than the
			// maintainer was told.
			if throttle != 300 {
				t.Errorf("the plist's ThrottleInterval is %d, want 300: launchd restarts a failing job at most once every five minutes, the interval ADR 0034, the runbook and the install's warning promise (issue #229)", throttle)
			}

			// launchd opens these as root, before the job's UserName applies, so nothing of the maintainer's
			// may be behind them: the job opens its own log as the maintainer instead. Losing the log there is
			// the price of not letting a planted symlink in the state directory aim root's write anywhere.
			limaInstanceDir := filepath.Join(f.env["HOME"], ".lima") + string(filepath.Separator)

			for _, key := range []string{"StandardOutPath", "StandardErrorPath"} {
				if got := plistString(t, dict, key); got != "/dev/null" {
					t.Errorf("the plist's %s is %q, want /dev/null: launchd opens it as root, and the log lives in a directory the maintainer writes, so a symlink planted there would make root write anywhere; the job opens the log itself (issue #229)\nstdout:\n%s", key, got, stdout)
				}
			}

			if strings.HasPrefix(wantLog, limaInstanceDir) {
				t.Errorf("the log the job opens is %q, inside the instance directory %s: launchd creates a job's log files there every time it runs it, so a deleted instance would come back (issue #229, the note from #237)", wantLog, limaInstanceDir)
			}

			// Rendering a plist reaches neither the VM nor GitHub.
			for _, tool := range []string{"limactl", "curl", "gh"} {
				if calls := invocations(f.sb.argvLogText(t), tool); len(calls) != 0 {
					t.Errorf("rendering the plist ran %s (%v): printing a plist touches neither the VM nor GitHub (issue #229)", tool, calls)
				}
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsTokenLaunchdPlistIsAPropertyList pins the document launchd is handed: an XML property list, with
// the declaration and the doctype, that encoding/xml parses whole and plutil -lint accepts where the machine
// has it. A Linux machine has no plutil, so the default tests pass there without it.
func TestAgentsTokenLaunchdPlistIsAPropertyList(t *testing.T) {
	t.Parallel()

	f := newLaunchdFixture(t, "shift2eu", xdgStateHomeSet)

	stdout, stderr, err := f.plist(t)
	if err != nil {
		t.Fatalf("sh %s plist exited %v with the four variables set\nstdout: %s\nstderr: %s", launchdScript, err, stdout, stderr)
	}

	for _, want := range []string{`<?xml`, `<!DOCTYPE plist`, `<plist version="1.0">`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the plist does not hold %q: launchd reads an XML property list, which starts with the declaration, the doctype and the plist element (issue #229)\nstdout:\n%s", want, stdout)
		}
	}

	// parsePlist fails the test on anything encoding/xml or this reader cannot make a dict of.
	parsePlist(t, stdout)

	t.Run("plutil -lint accepts it", func(t *testing.T) {
		t.Parallel()

		plutil, err := exec.LookPath("plutil")
		if err != nil {
			t.Skip("plutil is not installed: only macOS lints a property list with it, and the default tests pass on Linux without it")
		}

		path := filepath.Join(t.TempDir(), "agents-token-refresh.plist")
		writeFileMode(t, path, []byte(stdout), 0o600)

		cmd := exec.Command(plutil, "-lint", path)
		cmd.Env = []string{"PATH=" + toolDir(t, "plutil") + ":/usr/bin:/bin"}

		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Run(); err != nil {
			t.Errorf("plutil -lint refuses the generated plist (%v):\n%s\n%s", err, out.String(), stdout)
		}
	})
}

// TestAgentsTokenLaunchdPlistHoldsNoSecretAndPullsNothing pins what the plist must not carry: a pull of the
// host clone (launchd runs the clone's own code, and a pull would run code the tests never saw), a key or a
// token of any kind, and no key named like a token at all.
func TestAgentsTokenLaunchdPlistHoldsNoSecretAndPullsNothing(t *testing.T) {
	t.Parallel()

	f := newLaunchdFixture(t, "shift2eu", xdgStateHomeSet)

	stdout, stderr, err := f.plist(t)
	if err != nil {
		t.Fatalf("sh %s plist exited %v with the four variables set\nstdout: %s\nstderr: %s", launchdScript, err, stdout, stderr)
	}

	for _, forbidden := range []string{"git pull", "BEGIN", "PRIVATE KEY", "GITHUB_TOKEN", "GH_TOKEN", "ghs_", "token="} {
		for _, stream := range []string{stdout, stderr} {
			if strings.Contains(stream, forbidden) {
				t.Errorf("the plist generator's output holds %q: the job runs the refresh the host clone already holds and carries the App ID, the installation ID and the key's path, never a key, a token or a pull (issue #229)\n%s", forbidden, stream)
			}
		}
	}

	for _, key := range plistKeys(parsePlist(t, stdout)) {
		if strings.Contains(strings.ToUpper(key), "TOKEN") {
			t.Errorf("the plist has a key named %q: nothing in the plist is a token, and no key may look like one (issue #229)", key)
		}
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// launchdRefusal is one bad input, and the variable its refusal must name.
type launchdRefusal struct {
	name       string
	mutate     func(t *testing.T, f *launchdFixture)
	wantStderr string
}

// TestAgentsTokenLaunchdPlistRefusesBadInput pins the refusals: a missing, empty or malformed input, or a value
// an XML plist cannot hold, fails the run before it prints anything, and the message names the variable to fix
// without ever printing the key's contents.
func TestAgentsTokenLaunchdPlistRefusesBadInput(t *testing.T) {
	t.Parallel()

	cases := []launchdRefusal{
		{
			name:       "no LIMA_INSTANCE",
			mutate:     func(_ *testing.T, f *launchdFixture) { delete(f.env, "LIMA_INSTANCE") },
			wantStderr: "LIMA_INSTANCE",
		},
		{
			name:       "an empty LIMA_INSTANCE",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["LIMA_INSTANCE"] = "" },
			wantStderr: "LIMA_INSTANCE",
		},
		{
			name:       "no App ID",
			mutate:     func(_ *testing.T, f *launchdFixture) { delete(f.env, "SHIFT2EU_AGENTS_APP_ID") },
			wantStderr: "SHIFT2EU_AGENTS_APP_ID",
		},
		{
			name:       "an empty App ID",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["SHIFT2EU_AGENTS_APP_ID"] = "" },
			wantStderr: "SHIFT2EU_AGENTS_APP_ID",
		},
		{
			name:       "no installation ID",
			mutate:     func(_ *testing.T, f *launchdFixture) { delete(f.env, "SHIFT2EU_AGENTS_APP_INSTALLATION_ID") },
			wantStderr: "SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
		},
		{
			name:       "an empty installation ID",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = "" },
			wantStderr: "SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
		},
		{
			name:       "no key file",
			mutate:     func(_ *testing.T, f *launchdFixture) { delete(f.env, "SHIFT2EU_AGENTS_APP_KEY_FILE") },
			wantStderr: "SHIFT2EU_AGENTS_APP_KEY_FILE",
		},
		{
			name:       "an empty key file",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = "" },
			wantStderr: "SHIFT2EU_AGENTS_APP_KEY_FILE",
		},
		{
			// A label names a job, and launchd's own names carry no slash and no space: an instance name
			// that does is refused before it becomes half of a label.
			name:       "an instance with a slash",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["LIMA_INSTANCE"] = "a/b" },
			wantStderr: "LIMA_INSTANCE",
		},
		{
			name:       "an instance with a space",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["LIMA_INSTANCE"] = "a b" },
			wantStderr: "LIMA_INSTANCE",
		},
		{
			name:       "an App ID that is not all digits",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["SHIFT2EU_AGENTS_APP_ID"] = "12ab" },
			wantStderr: "SHIFT2EU_AGENTS_APP_ID",
		},
		{
			name:       "an installation ID that is not all digits",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = "78901x" },
			wantStderr: "SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
		},
		{
			// launchd starts a job from no working directory of its own, so a relative path would point
			// somewhere else.
			name:       "a relative key path",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = "keys/agents-app.pem" },
			wantStderr: "SHIFT2EU_AGENTS_APP_KEY_FILE",
		},
		{
			name: "an XDG_STATE_HOME with an XML metacharacter",
			mutate: func(_ *testing.T, f *launchdFixture) {
				f.env["XDG_STATE_HOME"] = filepath.Join(f.sb.root, "state&more")
			},
			wantStderr: "XDG_STATE_HOME",
		},
		{
			name:       "a HOME with an XML metacharacter",
			mutate:     func(_ *testing.T, f *launchdFixture) { f.env["HOME"] = filepath.Join(f.sb.root, "home'quoted") },
			wantStderr: "HOME",
		},
	}

	// A value that holds a character an XML plist uses as markup cannot be written into one unescaped, so
	// the generator refuses it instead of printing a plist that says something else. The subtest names spell
	// the character out, because a name is half of the sandbox path t.TempDir() builds (issue #227).
	for _, markup := range []struct {
		name      string
		character string
	}{
		{name: "a key path holding a less-than sign", character: "<"},
		{name: "a key path holding a greater-than sign", character: ">"},
		{name: "a key path holding an ampersand", character: "&"},
		{name: "a key path holding a double quote", character: `"`},
		{name: "a key path holding a single quote", character: "'"},
	} {
		cases = append(cases, launchdRefusal{
			name: markup.name,
			mutate: func(_ *testing.T, f *launchdFixture) {
				f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = filepath.Join(f.sb.root, "keys"+markup.character+"more", "agents-app.pem")
			},
			wantStderr: "SHIFT2EU_AGENTS_APP_KEY_FILE",
		})
	}

	// A control character in a path is unprintable and unreadable in a plist (auditor's finding): the
	// generator refuses every byte 0x01-0x1F and 0x7F, in all three paths it writes, not just the line break
	// and the tab it refused first. A value with one is a mistake in the .envrc, never something to install.
	controlCharacters := []struct {
		name string
		bite string
	}{
		{name: "0x01", bite: "\x01"},
		{name: "ESC 0x1b", bite: "\x1b"},
		{name: "0x7F the delete character", bite: "\x7f"},
	}

	for _, control := range controlCharacters {
		cases = append(cases,
			launchdRefusal{
				name: "a key path holding " + control.name,
				mutate: func(t *testing.T, f *launchdFixture) {
					t.Helper()

					// A real key file at that path: the only bad thing about the value is the control character.
					path := filepath.Join(f.sb.root, "keys"+control.bite+"more", "agents-app.pem")
					writeInventedKey(t, path, 0o600)

					f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = path
				},
				wantStderr: "SHIFT2EU_AGENTS_APP_KEY_FILE",
			},
			launchdRefusal{
				name: "a HOME holding " + control.name,
				mutate: func(_ *testing.T, f *launchdFixture) {
					f.env["HOME"] = filepath.Join(f.sb.root, "home"+control.bite+"more")
				},
				wantStderr: "HOME",
			},
			launchdRefusal{
				name: "an XDG_STATE_HOME holding " + control.name,
				mutate: func(_ *testing.T, f *launchdFixture) {
					f.env["XDG_STATE_HOME"] = filepath.Join(f.sb.root, "state"+control.bite+"more")
				},
				wantStderr: "XDG_STATE_HOME",
			},
		)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdFixture(t, "shift2eu", xdgStateHomeSet)
			tc.mutate(t, f)

			stdout, stderr, err := f.plist(t)
			if err == nil {
				t.Fatalf("sh %s plist exited 0 with %s; it must refuse it and exit non-zero\nstdout: %s", launchdScript, tc.name, stdout)
			}

			if stdout != "" {
				t.Errorf("the plist generator wrote to stdout although it refused %s: a refused run prints no plist, so make cannot install half of one:\n%s", tc.name, stdout)
			}

			if !strings.Contains(stderr, tc.wantStderr) {
				t.Errorf("the refusal does not name %s, so a maintainer cannot tell which value to fix: stderr: %s", tc.wantStderr, stderr)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsTokenLaunchdPlistRefusesToRunAsRoot pins the sixth review's finding: the plist's UserName is the
// account that renders it, so a plist rendered under `sudo -E make agents-token-refresh-install` would name root
// as the job's account, and the job would then run a script root can be told to run while the maintainer can
// still edit it. The generator therefore refuses before it prints anything, and says to run it as the maintainer
// and not under sudo (issue #229, sixth review).
func TestAgentsTokenLaunchdPlistRefusesToRunAsRoot(t *testing.T) {
	t.Parallel()

	f := newLaunchdFixture(t, "shift2eu", xdgStateHomeSet)
	f.sb.useFakeIDAsRoot(t)

	stdout, stderr, err := f.plist(t)
	if err == nil {
		t.Fatalf("sh %s plist exited 0 with id -u answering 0: the job's UserName comes from id -un, so a plist rendered as root would run the refresh as root, on a script the maintainer can still edit\nstdout: %s\nstderr: %s", launchdScript, stdout, stderr)
	}

	if stdout != "" {
		t.Errorf("the plist generator printed a plist although it refused to run as root, so make could install a job that runs as root:\n%s", stdout)
	}

	refusal := strings.ToLower(stderr)

	for _, want := range []string{"maintainer", "sudo"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not mention %q, so a maintainer who ran sudo -E make agents-token-refresh-install cannot tell how to run it instead:\nstderr: %s", want, stderr)
		}
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsTokenLaunchdPlistAccountMode pins the plist the account mode renders (ADR 0031, update of 2026-11-19,
// 2; ADR 0034, update of 2026-11-19, c): the generator runs as the account, from the account's own clone, so
// UserName is that account and ProgramArguments and WorkingDirectory are under its home; in SSH mode the four
// SHIFT2EU_AGENTS_VM_SSH_* variables go into EnvironmentVariables, and PATH is exactly /usr/bin:/bin:/usr/sbin:/sbin
// with no /opt/homebrew/bin, which the maintainer's uid owns and where a planted openssl, curl or ssh would run as
// the account with the key. A partial set of the four is refused, naming the missing ones, as the token script
// refuses it.
func TestAgentsTokenLaunchdPlistAccountMode(t *testing.T) {
	t.Parallel()

	home := accountHome(t)
	clone := filepath.Join(home, "shift2eu-host")
	script := filepath.Join(clone, launchdScript)
	writeFileMode(t, script, []byte(readRepoFile(t, launchdScript)), 0o755)

	secrets := filepath.Join(home, ".config", "shift2eu")
	sshKey := filepath.Join(secrets, "agents-vm-ssh-key")
	writeFileMode(t, sshKey, []byte("an invented ed25519 private key, never a real one\n"), 0o600)

	knownHosts := filepath.Join(secrets, "known-hosts-shift2eu")
	writeFileMode(t, knownHosts, []byte("shift2eu-vm-shift2eu ssh-ed25519 AAAA-invented-host-key\n"), 0o600)

	newAccountFixture := func(t *testing.T) *launchdFixture {
		t.Helper()

		f := newLaunchdFixture(t, "shift2eu", xdgStateHomeUnset)
		f.env["HOME"] = home
		f.env["SHIFT2EU_AGENTS_VM_SSH_PORT"] = sshModePort
		f.env["SHIFT2EU_AGENTS_VM_SSH_USER"] = sshModeUser
		f.env["SHIFT2EU_AGENTS_VM_SSH_KEY"] = sshKey
		f.env["SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS"] = knownHosts
		f.sb.useFakeIDName(t, f.env, agentsAccount)

		return f
	}

	t.Run("SSH mode renders the account's job", func(t *testing.T) {
		t.Parallel()

		f := newAccountFixture(t)

		stdout, stderr, err := f.sb.run(t, script, []string{"plist"}, f.env, "")
		if err != nil {
			t.Fatalf("sh %s plist as the account exited %v\nstdout: %s\nstderr: %s", launchdScript, err, stdout, stderr)
		}

		f.assertNoLeaks(t, stdout, stderr)

		dict := parsePlist(t, stdout)

		if got := plistString(t, dict, "UserName"); got != agentsAccount {
			t.Errorf("the plist's UserName is %q, want the account that rendered it, %q: in account mode the job runs as the account, which owns the key and the clone (ADR 0034, update of 2026-11-19, c)", got, agentsAccount)
		}

		wantArgs := []string{
			"/bin/sh",
			"-c",
			`umask 077 && mkdir -p "${1%/*}" && exec /bin/sh "$0" refresh >>"$1" 2>&1`,
			filepath.Join(clone, agentsMintScript),
			filepath.Join(home, ".local", "state", "shift2eu", "agents-token-refresh-shift2eu.log"),
		}

		if diff := cmp.Diff(wantArgs, plistStrings(t, plistList(t, dict, "ProgramArguments"))); diff != "" {
			t.Errorf("the plist's ProgramArguments do not name the account's own clone (-want +got):\n%s\non the Mac they are %s, which is /Users/%s/shift2eu-host (ADR 0034, update of 2026-11-19, c)", diff, filepath.Join(clone, agentsMintScript), agentsAccount)
		}

		if got := plistString(t, dict, "WorkingDirectory"); got != clone {
			t.Errorf("the plist's WorkingDirectory is %q, want the account's clone %q", got, clone)
		}

		env := plistMap(t, dict, "EnvironmentVariables")

		if got, want := plistString(t, env, "PATH"), "/usr/bin:/bin:/usr/sbin:/sbin"; got != want {
			t.Errorf("the plist's PATH is %q, want exactly %q: /opt/homebrew/bin belongs to the maintainer's uid, and a planted openssl, curl or ssh there would run as the account with the key (ADR 0034, update of 2026-11-19, c)", got, want)
		}

		wantEnv := map[string]string{
			"LIMA_INSTANCE":                       "shift2eu",
			"SHIFT2EU_AGENTS_APP_ID":              launchdAppID,
			"SHIFT2EU_AGENTS_APP_INSTALLATION_ID": launchdInstallationID,
			"SHIFT2EU_AGENTS_APP_KEY_FILE":        f.key.path,
			"SHIFT2EU_AGENTS_VM_SSH_PORT":         sshModePort,
			"SHIFT2EU_AGENTS_VM_SSH_USER":         sshModeUser,
			"SHIFT2EU_AGENTS_VM_SSH_KEY":          sshKey,
			"SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS":  knownHosts,
			"HOME":                                home,
		}

		for name, want := range wantEnv {
			if got := plistString(t, env, name); got != want {
				t.Errorf("the plist's EnvironmentVariables give %s=%q, want %q: a launchd job inherits no environment, so the plist supplies the SSH mode's variables too (ADR 0034, update of 2026-11-19, c)", name, got, want)
			}
		}

		if strings.Contains(stdout, "/opt/homebrew/bin") {
			t.Errorf("the account's plist names /opt/homebrew/bin, which the maintainer's uid owns (ADR 0034, update of 2026-11-19, c):\n%s", stdout)
		}
	})

	t.Run("a partial SSH configuration is refused", func(t *testing.T) {
		t.Parallel()

		f := newAccountFixture(t)
		delete(f.env, "SHIFT2EU_AGENTS_VM_SSH_KEY")

		stdout, stderr, err := f.sb.run(t, script, []string{"plist"}, f.env, "")
		if err == nil {
			t.Fatalf("sh %s plist exited 0 with three of the four SHIFT2EU_AGENTS_VM_SSH_* variables set, so the job would mint in neither mode: a partial set is refused, naming the missing ones, as the token script refuses it (ADR 0034, update of 2026-11-19, c)\nstdout: %s\nstderr: %s", launchdScript, stdout, stderr)
		}

		if !strings.Contains(stderr, "SHIFT2EU_AGENTS_VM_SSH_KEY") {
			t.Errorf("the refusal of a partial SSH configuration does not name the missing SHIFT2EU_AGENTS_VM_SSH_KEY:\n%s", stderr)
		}

		if strings.TrimSpace(stdout) != "" {
			t.Errorf("the generator printed a plist although it refused a partial SSH configuration:\n%s", stdout)
		}

		f.assertNoLeaks(t, stdout, stderr)
	})
}

// TestAgentsTokenLaunchdTrustedPathChecksACLs pins audit 3 and review 11 of issue #227 part 2 for the generator:
// it applies the same rule the refresh does, so a path whose ACL allows another account is refused at install time
// - naming the path and the ACL - and a deny-only ACL, which every macOS home carries, is accepted. The mode a
// Mac's ls ends with @ when the entry carries extended attributes beside the ACL is read too (the security
// audit's second pass).
func TestAgentsTokenLaunchdTrustedPathChecksACLs(t *testing.T) {
	t.Parallel()

	const (
		allowEntry = " 0: user:_shift2euagents allow read,write\n"
		denyEntry  = " 0: group:everyone deny delete\n"
	)

	home := accountHome(t)
	clone := filepath.Join(home, "shift2eu-host")
	script := filepath.Join(clone, launchdScript)
	writeFileMode(t, script, []byte(readRepoFile(t, launchdScript)), 0o755)

	secrets := filepath.Join(home, ".config", "shift2eu")
	sshKey := filepath.Join(secrets, "agents-vm-ssh-key")
	writeFileMode(t, sshKey, []byte("an invented ed25519 private key, never a real one\n"), 0o600)

	knownHosts := filepath.Join(secrets, "known-hosts-shift2eu")
	writeFileMode(t, knownHosts, []byte("shift2eu-vm-shift2eu ssh-ed25519 AAAA-invented-host-key\n"), 0o600)

	newAccountFixture := func(t *testing.T) *launchdFixture {
		t.Helper()

		f := newLaunchdFixture(t, "shift2eu", xdgStateHomeUnset)
		f.env["HOME"] = home
		f.env["SHIFT2EU_AGENTS_VM_SSH_PORT"] = sshModePort
		f.env["SHIFT2EU_AGENTS_VM_SSH_USER"] = sshModeUser
		f.env["SHIFT2EU_AGENTS_VM_SSH_KEY"] = sshKey
		f.env["SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS"] = knownHosts
		f.sb.useFakeIDName(t, f.env, agentsAccount)

		return f
	}

	cases := []struct {
		name     string
		mode     string // the trailer a Mac's ls prints: + for an ACL, @ for extended attributes beside it
		entries  string
		accepted bool
	}{
		{name: "an ACL that allows another account", mode: "+", entries: allowEntry},
		{name: "a deny-only ACL", mode: "+", entries: denyEntry, accepted: true},
		{name: "an extended-attribute mode over an ACL that allows another account", mode: "@", entries: allowEntry},
		{name: "an extended-attribute mode over a deny-only ACL", mode: "@", entries: denyEntry, accepted: true},
		{name: "an extended-attribute mode with no ACL", mode: "@", accepted: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newAccountFixture(t)
			aclProbe(t, f.sb, f.env, f.key.path, tc.mode, tc.entries)

			stdout, stderr, err := f.sb.run(t, script, []string{"plist"}, f.env, "")

			if tc.accepted {
				if err != nil {
					t.Fatalf("sh %s plist refused a path whose only ACL entry denies, exiting %d: a deny grants nothing, and every macOS home carries one, so the rule accepts it (issue #227 part 2, audit 3)\nstdout: %s\nstderr: %s",
						launchdScript, exitStatusOf(t, err), stdout, stderr)
				}

				if strings.TrimSpace(stdout) == "" {
					t.Errorf("the generator printed no plist for a path with a deny-only ACL")
				}

				f.assertNoLeaks(t, stdout, stderr)

				return
			}

			if err == nil {
				t.Fatalf("sh %s plist exited 0 although the App key's ACL allows another account: the generator applies the trusted path rule at install time, so the job is never rendered for a path another account controls (issue #227 part 2, audit 3)\nstdout: %s\nstderr: %s",
					launchdScript, stdout, stderr)
			}

			if !strings.Contains(stderr, "ACL") {
				t.Errorf("the refusal does not say the entry's ACL is what failed, so a maintainer cannot tell why the path is refused:\n%s", stderr)
			}

			if !mentionsOneOf(stderr, f.key.path, filepath.Dir(f.key.path)) {
				t.Errorf("the refusal names neither %s nor its directory, so a maintainer cannot tell which path to fix:\n%s", f.key.path, stderr)
			}

			if strings.TrimSpace(stdout) != "" {
				t.Errorf("the generator printed a plist although it refused a path the rule rejects:\n%s", stdout)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// plistValue is one value of a parsed property list: the kinds these tests read, and no more.
type plistValue struct {
	kind  string // "dict", "array", "string", "integer", or one of the self-closing booleans
	text  string
	dict  map[string]plistValue
	array []plistValue
}

// parsePlist reads the body of a property list into its top-level dict, failing the test on anything that is
// not an XML plist of that shape.
func parsePlist(t *testing.T, body string) map[string]plistValue {
	t.Helper()

	dec := xml.NewDecoder(strings.NewReader(body))

	// The declaration and the doctype are tokens of their own; the first element is the plist.
	nextStart(t, dec, "plist")
	nextStart(t, dec, "dict")

	return parseDict(t, dec)
}

// nextStart returns the next start element of the document, failing the test when the document ends first or
// when another element than name arrives.
func nextStart(t *testing.T, dec *xml.Decoder, name string) xml.StartElement {
	t.Helper()

	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("the plist ends where <%s> was expected: %v", name, err)
		}

		switch element := tok.(type) {
		case xml.StartElement:
			if element.Name.Local != name {
				t.Fatalf("the plist holds <%s>, want <%s>", element.Name.Local, name)
			}

			return element
		case xml.EndElement:
			t.Fatalf("the plist holds an empty <%s>", element.Name.Local)
		}
	}
}

// parseDict reads the dict whose start element the decoder has just passed.
func parseDict(t *testing.T, dec *xml.Decoder) map[string]plistValue {
	t.Helper()

	dict := make(map[string]plistValue)

	key, haveKey := "", false

	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("the plist's <dict> never ends: %v", err)
		}

		switch element := tok.(type) {
		case xml.EndElement:
			if element.Name.Local != "dict" {
				t.Fatalf("the plist's <dict> holds a stray </%s>", element.Name.Local)
			}

			if haveKey {
				t.Fatalf("the plist's <dict> holds the key %q with no value", key)
			}

			return dict
		case xml.StartElement:
			if element.Name.Local == "key" {
				if haveKey {
					t.Fatalf("the plist's <dict> holds the key %q twice", key)
				}

				key, haveKey = readText(t, dec, "key"), true

				continue
			}

			if !haveKey {
				t.Fatalf("the plist's <dict> holds a <%s> with no key", element.Name.Local)
			}

			if _, twice := dict[key]; twice {
				t.Fatalf("the plist's <dict> holds the key %q twice", key)
			}

			dict[key], haveKey = *parseValue(t, dec, element), false
		}
	}
}

// parseArray reads the array whose start element the decoder has just passed.
func parseArray(t *testing.T, dec *xml.Decoder) []plistValue {
	t.Helper()

	values := []plistValue{}

	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("the plist's <array> never ends: %v", err)
		}

		switch element := tok.(type) {
		case xml.EndElement:
			if element.Name.Local != "array" {
				t.Fatalf("the plist's <array> holds a stray </%s>", element.Name.Local)
			}

			return values
		case xml.StartElement:
			values = append(values, *parseValue(t, dec, element))
		}
	}
}

// parseValue reads the value whose start element the decoder has just passed.
func parseValue(t *testing.T, dec *xml.Decoder, start xml.StartElement) *plistValue {
	t.Helper()

	switch start.Name.Local {
	case "dict":
		return &plistValue{kind: "dict", dict: parseDict(t, dec)}
	case "array":
		return &plistValue{kind: "array", array: parseArray(t, dec)}
	case "string", "integer", "real", "date", "data", "true", "false":
		return &plistValue{kind: start.Name.Local, text: readText(t, dec, start.Name.Local)}
	default:
		t.Fatalf("the plist holds a <%s>, which these tests do not read", start.Name.Local)

		return nil
	}
}

// readText reads the text of the element whose start the decoder has just passed, up to its end element.
func readText(t *testing.T, dec *xml.Decoder, name string) string {
	t.Helper()

	var text strings.Builder

	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("the plist's <%s> never ends: %v", name, err)
		}

		switch element := tok.(type) {
		case xml.CharData:
			text.Write(element)
		case xml.EndElement:
			if element.Name.Local != name {
				t.Fatalf("the plist's <%s> ends with </%s>", name, element.Name.Local)
			}

			return text.String()
		case xml.StartElement:
			t.Fatalf("the plist's <%s> holds a <%s>", name, element.Name.Local)
		}
	}
}

// plistValueOf returns a dict's value of key, failing when the key is missing.
func plistValueOf(t *testing.T, dict map[string]plistValue, key string) plistValue {
	t.Helper()

	value, ok := dict[key]
	if !ok {
		t.Fatalf("the plist has no %s key", key)
	}

	return value
}

// plistString returns a dict's string value of key, failing when it is missing or of another kind.
func plistString(t *testing.T, dict map[string]plistValue, key string) string {
	t.Helper()

	value := plistValueOf(t, dict, key)
	if value.kind != "string" {
		t.Fatalf("the plist's %s is a <%s>, want a <string>", key, value.kind)
	}

	return value.text
}

// plistText returns the text of a dict's scalar value of key, whatever kind of scalar it is.
func plistText(t *testing.T, dict map[string]plistValue, key string) string {
	t.Helper()

	return plistValueOf(t, dict, key).text
}

// plistKind returns the kind of a dict's value of key.
func plistKind(t *testing.T, dict map[string]plistValue, key string) string {
	t.Helper()

	return plistValueOf(t, dict, key).kind
}

// plistMap returns a dict's dict value of key, failing when it is missing or of another kind.
func plistMap(t *testing.T, dict map[string]plistValue, key string) map[string]plistValue {
	t.Helper()

	value := plistValueOf(t, dict, key)
	if value.kind != "dict" {
		t.Fatalf("the plist's %s is a <%s>, want a <dict>", key, value.kind)
	}

	return value.dict
}

// plistList returns a dict's array value of key, failing when it is missing or of another kind.
func plistList(t *testing.T, dict map[string]plistValue, key string) []plistValue {
	t.Helper()

	value := plistValueOf(t, dict, key)
	if value.kind != "array" {
		t.Fatalf("the plist's %s is a <%s>, want an <array>", key, value.kind)
	}

	return value.array
}

// plistStrings returns the text of an array of strings, failing when an item is of another kind.
func plistStrings(t *testing.T, values []plistValue) []string {
	t.Helper()

	texts := make([]string, 0, len(values))

	for _, value := range values {
		if value.kind != "string" {
			t.Fatalf("the plist's array holds a <%s>, want a <string>", value.kind)
		}

		texts = append(texts, value.text)
	}

	return texts
}

// plistKeys returns every key of a dict and of the dicts and arrays inside it, sorted, so a test can check
// that no key anywhere in the plist is named like a secret.
func plistKeys(dict map[string]plistValue) []string {
	keys := make([]string, 0, len(dict))

	for key, value := range dict {
		keys = append(keys, key)
		keys = append(keys, nestedPlistKeys(value)...)
	}

	slices.Sort(keys)

	return keys
}

// nestedPlistKeys returns every key of the dicts inside a value.
func nestedPlistKeys(value plistValue) []string {
	switch value.kind {
	case "dict":
		return plistKeys(value.dict)
	case "array":
		var keys []string

		for _, item := range value.array {
			keys = append(keys, nestedPlistKeys(item)...)
		}

		return keys
	default:
		return nil
	}
}
