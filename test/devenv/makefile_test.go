// Pins the Makefile's part in the agents App's credential (PRD #161 milestone 1): VM_ENV names no
// SHIFT2EU_AGENTS_APP_* variable, make vm-env refuses one a maintainer adds rather than copying it into the
// VM, make agents-token / make agents-token-refresh run the mint and the refresh, make
// agents-token-refresh-start starts one refresher per VM in the background from the host clone, make deck
// warns about App variables in a workspace shell and starts nothing, and make vm-gh-wrapper installs the
// pinned wrapper an existing VM lacks, whole or not at all
// (.dot-agent-deck/prd161m1-fixes.md items 3, 7 and 8 and .dot-agent-deck/prd161m1-fixes2.md items 1 and 2).
package devenv_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// agentsAppPrefix is the prefix of the three inputs dev/agents-app-token.sh reads from the maintainer's
// direnv .envrc: the App ID, the installation ID and the path of the App's key. None may enter the VM.
const agentsAppPrefix = "SHIFT2EU_AGENTS_APP_"

func TestMakefileVMENVNamesNoAgentsAppVariable(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, makefilePath)

	value, ok := makeVariable(body, "VM_ENV")
	if !ok {
		t.Fatalf("%s has no 'VM_ENV ?=' assignment", makefilePath)
	}

	names := strings.Split(value, ",")
	if len(names) == 0 || strings.TrimSpace(value) == "" {
		t.Fatalf("VM_ENV's default is empty: vm-env copies the variables it lists from your shell into the VM")
	}

	for _, name := range names {
		name = strings.TrimSpace(name)
		if strings.HasPrefix(name, agentsAppPrefix) {
			t.Errorf("VM_ENV's default names %s: the App's key and its path stay on the Mac, and only the installation token enters the VM (PRD #161 milestone 1)", name)
		}
	}
}

// TestMakeVMEnvRefusesAnAgentsAppVariable pins the second half: a maintainer who adds an App variable to
// VM_ENV, or passes one on the command line, gets a refusal and not a copy of the key's path into the VM.
func TestMakeVMEnvRefusesAnAgentsAppVariable(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)

	env := sb.baseEnv()
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = "/home/tester/invented/agents-app.pem"

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=SHIFT2EU_AGENTS_APP_KEY_FILE"}, env)
	if err == nil {
		t.Fatalf("make vm-env exited 0 with VM_ENV=SHIFT2EU_AGENTS_APP_KEY_FILE: vm-env must refuse an App variable, so neither the key nor its path enters the VM\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if log := sb.argvLogText(t); log != "" {
		t.Errorf("make vm-env called limactl before it refused an App variable, so something may have entered the VM:\n%s", log)
	}

	if !strings.Contains(stdout+stderr, "SHIFT2EU_AGENTS_APP_KEY_FILE") {
		t.Errorf("the refusal does not name SHIFT2EU_AGENTS_APP_KEY_FILE, so a maintainer cannot tell what vm-env refused:\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if _, err := os.Stat(sb.vmPath(".config/dot-agent-deck/daemon.env")); err == nil {
		t.Errorf("make vm-env wrote the VM's ~/.config/dot-agent-deck/daemon.env although it refused an App variable")
	}
}

func TestMakefileHasTheAgentsTokenTargets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		target string
		want   string
	}{
		{target: "agents-token", want: "agents-app-token.sh mint"},
		{target: "agents-token-refresh", want: "agents-app-token.sh refresh"},
		// The background start the maintainer runs from the host clone (second review, item 1).
		{target: "agents-token-refresh-start", want: "agents-app-token.sh refresh"},
	}

	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, err := runMake(t, []string{"-n", tc.target}, makeEnv(t, nil))
			if err != nil {
				t.Fatalf("make -n %s: %v\nstderr: %s", tc.target, err, stderr)
			}

			if !strings.Contains(stdout, tc.want) {
				t.Errorf("make -n %s does not run %q:\n%s", tc.target, tc.want, stdout)
			}
		})
	}
}

// TestMakeVMGHWrapperInstallsThePinnedWrapper pins make vm-gh-wrapper (review item 3): a VM created before the
// wrapper existed has the real gh on /usr/local/bin/gh, so the target moves it off the PATH, installs the
// pinned wrapper above it and points the git credential helper at the wrapper, idempotently, with the wrapper
// going over standard input and never onto a command line.
func TestMakeVMGHWrapperInstallsThePinnedWrapper(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"

	capture := filepath.Join(sb.root, "lima-stdin.log")
	env["SHIFT2EU_TEST_LIMA_CAPTURE"] = capture

	t.Run("make -n shows the wrapper going over standard input", func(t *testing.T) {
		t.Parallel()

		stdout, stderr, err := runMake(t, []string{"-n", "vm-gh-wrapper"}, env)
		if err != nil {
			t.Fatalf("make -n vm-gh-wrapper: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "gh-app-token.sh") {
			t.Errorf("make -n vm-gh-wrapper does not read dev/%s, the wrapper every VM must run:\n%s", ghWrapperScript, stdout)
		}

		// An agent can read every command line in the VM: the wrapper's text must travel on stdin.
		for _, line := range strings.Split(readRepoFile(t, ghWrapperScript), "\n") {
			line = strings.TrimSpace(line)

			if line == "" || strings.HasPrefix(line, "#") || len(line) < 12 {
				continue
			}

			if strings.Contains(stdout, line) {
				t.Errorf("make -n vm-gh-wrapper puts the wrapper's line %q on a command line; the recipe must hand the wrapper over standard input (review item 3):\n%s", line, stdout)
			}
		}
	})

	t.Run("make vm-gh-wrapper installs the pinned wrapper and keeps doing so", func(t *testing.T) {
		t.Parallel()

		// The fake limactl runs nothing, so no provision command of this recipe touches the machine the
		// tests run on; what it records is what the VM would get.
		runOnce := func() string {
			t.Helper()

			if err := os.Remove(capture); err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("remove %s: %v", capture, err)
			}

			_, stderr, err := runMake(t, []string{"vm-gh-wrapper"}, env)
			if err != nil {
				t.Fatalf("make vm-gh-wrapper: %v\nstderr: %s", err, stderr)
			}

			body, err := os.ReadFile(capture)
			if err != nil {
				t.Fatalf("make vm-gh-wrapper called limactl nothing, or handed it no standard input: %v", err)
			}

			return string(body)
		}

		first := runOnce()
		second := runOnce()

		if first != second {
			t.Errorf("make vm-gh-wrapper gives the VM different input on a second run, so it is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
		}

		wrapper := wrapperBlock(t, first, "make vm-gh-wrapper")
		assertPinnedRealGH(t, "make vm-gh-wrapper", wrapper)

		for _, want := range []string{
			"/usr/local/libexec/shift2eu/gh",
			"/usr/local/bin/gh",
			"credential.https://github.com.helper",
			"auth git-credential",
		} {
			if !strings.Contains(first, want) {
				t.Errorf("make vm-gh-wrapper never mentions %q: it must move the real gh to /usr/local/libexec/shift2eu/gh, install the wrapper at /usr/local/bin/gh and point the git credential helper at it:\n%s", want, first)
			}
		}

		// And the wrapper's text never reaches a command line.
		argv := sb.argvLogText(t)

		for _, line := range strings.Split(wrapper, "\n") {
			line = strings.TrimSpace(line)

			if line == "" || strings.HasPrefix(line, "#") || len(line) < 12 {
				continue
			}

			if strings.Contains(argv, "arg="+line+"\n") {
				t.Errorf("the wrapper's line %q reached a limactl command line; it must go over standard input (review item 3)", line)
			}
		}
	})
}

// TestMakeDeckLeavesTheTokenPlumbingAloneWithoutTheAppVariables pins that make deck behaves as before on a Mac
// that has no App: without the three SHIFT2EU_AGENTS_APP_* variables it opens the deck and starts no refresher
// (review item 8).
func TestMakeDeckLeavesTheTokenPlumbingAloneWithoutTheAppVariables(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"
	env["SHIFT2EU_TEST_LIMA_CAPTURE"] = filepath.Join(sb.root, "lima-stdin.log")

	stdout, stderr, err := runMake(t, []string{"deck"}, env)
	if err != nil {
		t.Fatalf("make deck without the App's variables: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// The deck itself ran: the fake limactl was asked for it and ran nothing.
	var ranDeck bool

	for _, args := range invocations(sb.argvLogText(t), "limactl") {
		if slices.Contains(args, "dot-agent-deck") {
			ranDeck = true
		}
	}

	if !ranDeck {
		t.Errorf("make deck never opened the deck:\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// And nothing of the token plumbing happened: no pid file, no log, no state directory.
	entries, err := os.ReadDir(sb.stateDir())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read %s: %v", sb.stateDir(), err)
		}

		return
	}

	for _, entry := range entries {
		t.Errorf("make deck without the App's variables left %s in %s: it must start no agents App token refresh", entry.Name(), sb.stateDir())
	}
}

// TestMakeDeckWarnsAboutTheAppVariablesInTheWorkspaceShell pins the other half of second review item 1: when
// any SHIFT2EU_AGENTS_APP_* variable reaches make deck's environment, it says so, points at a direnv .envrc
// outside the workspace, and still opens the deck: the App's variables belong in the maintainer's host clone,
// not in a shell the agents' workspace can read.
func TestMakeDeckWarnsAboutTheAppVariablesInTheWorkspaceShell(t *testing.T) {
	t.Parallel()

	all := []string{
		"SHIFT2EU_AGENTS_APP_ID",
		"SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
		"SHIFT2EU_AGENTS_APP_KEY_FILE",
	}

	cases := []struct {
		name string
		set  []string
	}{
		{name: "one App variable set", set: []string{"SHIFT2EU_AGENTS_APP_KEY_FILE"}},
		{name: "the App ID alone", set: []string{"SHIFT2EU_AGENTS_APP_ID"}},
		{name: "all three set", set: all},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, "shift2eu")

			values := make(map[string]string, len(all))

			for _, name := range all {
				values[name] = f.env[name]
				delete(f.env, name)
			}

			for _, name := range tc.set {
				f.env[name] = values[name]
			}

			stdout, stderr, err := runMake(t, []string{"deck"}, f.env)
			if err != nil {
				t.Fatalf("make deck with an App variable in its environment: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			combined := stdout + stderr

			if !strings.Contains(combined, agentsAppPrefix) {
				t.Errorf("make deck with an App variable in its environment says nothing about %s, so a maintainer never learns the App's variables are visible in the workspace shell:\n%s", agentsAppPrefix, combined)
			}

			if !strings.Contains(combined, ".envrc") {
				t.Errorf("the warning does not name the direnv .envrc the App's variables belong in: outside the workspace, in the maintainer's host clone (second review, item 1):\n%s", combined)
			}

			// It warns and still opens the deck: a warning is no refusal.
			var ranDeck bool

			for _, args := range invocations(f.sb.argvLogText(t), "limactl") {
				if slices.Contains(args, "dot-agent-deck") {
					ranDeck = true
				}
			}

			if !ranDeck {
				t.Errorf("make deck did not open the deck when an App variable was set:\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			// And it runs nothing of the token plumbing: no refresher, no state file, no mint (second review,
			// item 1: the refresh belongs to make agents-token-refresh-start in the host clone).
			entries, err := os.ReadDir(f.state)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("read %s: %v", f.state, err)
				}
			}

			for _, entry := range entries {
				t.Errorf("make deck with an App variable set left %s in %s: it must start no agents App token refresh", entry.Name(), f.state)
			}

			if got := len(f.api.minted()); got != 0 {
				t.Errorf("make deck with an App variable set minted %d token(s): the refresh is not its job any more", got)
			}
		})
	}
}

// tokenFixture is one environment for a run of make agents-token-refresh-start, or of make deck, with the
// App's three variables set: a sandbox whose limactl records what it is given and runs nothing, the fake
// GitHub API, and a state directory inside the sandbox, so no test leaves a refresher or a state file behind.
type tokenFixture struct {
	sb    *sandbox
	api   *fakeGitHub
	env   map[string]string
	state string // ${XDG_STATE_HOME}/shift2eu
}

// newTokenFixture builds the fixture for one VM instance and stops the refresher a target starts with the test.
func newTokenFixture(t *testing.T, instance string) *tokenFixture {
	t.Helper()

	sb := newSandbox(t)
	key := writeInventedKey(t, filepath.Join(sb.root, "key", "agents-app.pem"), 0o600)
	api := newFakeGitHub(t, key.public)

	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"
	env["SHIFT2EU_AGENTS_APP_ID"] = api.appID
	env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = api.installationID
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = key.path
	env["SHIFT2EU_AGENTS_GITHUB_API_URL"] = api.url
	env["SHIFT2EU_VM_REAL_GH_PATH"] = filepath.Join(sb.root, "vm-real-gh")
	env["SHIFT2EU_TEST_LIMA_CAPTURE"] = filepath.Join(sb.root, "lima-stdin.log")
	env["SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS"] = "1"
	env["SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS"] = "60"
	env["LIMA_INSTANCE"] = instance

	f := &tokenFixture{sb: sb, api: api, env: env, state: sb.stateDir()}
	t.Cleanup(func() { f.stopRefresher(t) })

	return f
}

// statePath is a file in the state directory the start target keeps the refresh's pid and log in.
func (f *tokenFixture) statePath(name string) string {
	return filepath.Join(f.state, name)
}

// writeStateFile writes one file of the state directory, with the mode the start target gives it.
func (f *tokenFixture) writeStateFile(t *testing.T, name, content string, mode os.FileMode) {
	t.Helper()

	writeFileMode(t, f.statePath(name), []byte(content), mode)
}

// stopRefresher ends every refresher a target started in this fixture, so no test leaves a loop minting
// against a server the test has closed.
func (f *tokenFixture) stopRefresher(t *testing.T) {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(f.state, "*.pid"))
	if err != nil {
		t.Errorf("look for the pid files of %s: %v", f.state, err)

		return
	}

	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
		if err != nil || pid <= 1 {
			continue
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}

		_ = proc.Signal(syscall.SIGTERM)

		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && proc.Signal(syscall.Signal(0)) == nil {
			time.Sleep(25 * time.Millisecond)
		}

		_ = proc.Kill()
	}
}

// readPid is the pid the pid file holds.
func readPid(t *testing.T, path string) int {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is not there, so make deck started no refresher: %v", path, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("%s does not hold a pid: %q", path, strings.TrimSpace(string(body)))
	}

	return pid
}

// pidAlive reports whether a process with this pid is running.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return proc.Signal(syscall.Signal(0)) == nil
}

// deadPid is the pid of a process that has exited, for a pid file no process owns.
func deadPid(t *testing.T) int {
	t.Helper()

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run a process that exits at once: %v", err)
	}

	return cmd.Process.Pid
}

// TestMakeAgentsTokenRefreshStartKeepsOneRefresherPerVM pins second review item 1, and with it the
// per-instance pid and log files of the first review's item 7 and the lock's pid (N-new-2): make
// agents-token-refresh-start warns about a refresher that stopped and starts a new one, leaves a running one
// alone, honours a refresher that holds the lock without a pid file, says where the log is, and names the
// files after the VM instance, so two VMs never share a pid file.
func TestMakeAgentsTokenRefreshStartKeepsOneRefresherPerVM(t *testing.T) {
	t.Parallel()

	t.Run("a pid file whose process is dead warns and starts a new refresher", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")

		stale := deadPid(t)
		f.writeStateFile(t, "agents-token-refresh-shift2eu.pid", strconv.Itoa(stale)+"\n", 0o600)
		f.writeStateFile(t, "agents-token-refresh-shift2eu.log", "agents-token: wrote a token\n", 0o600)

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if combined := stdout + stderr; !strings.Contains(combined, strconv.Itoa(stale)) {
			t.Errorf("make deck says nothing about the refresher whose pid %d it found dead, so a maintainer cannot see that the last one stopped before the token ran out:\n%s", stale, combined)
		}

		pidPath := f.statePath("agents-token-refresh-shift2eu.pid")
		pid := readPid(t, pidPath)

		if pid == stale {
			t.Errorf("the pid file still holds the dead pid %d: make deck must start a refresher", pid)
		}

		if !pidAlive(pid) {
			t.Errorf("the pid file holds %d, which is not running: make deck must leave a refresher running", pid)
		}

		info, err := os.Stat(pidPath)
		if err != nil {
			t.Fatalf("stat %s: %v", pidPath, err)
		}

		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("the pid file is mode %04o, want 0600: it is written under umask 077 like the log (review item 7, N7)", got)
		}

		// A background start is only usable if it says where the log is: the refresher writes nothing else.
		if logPath := f.statePath("agents-token-refresh-shift2eu.log"); !strings.Contains(stdout+stderr, logPath) {
			t.Errorf("make agents-token-refresh-start does not print the log's path %s, so a maintainer cannot read what the refresher says:\nstdout: %s\nstderr: %s", logPath, stdout, stderr)
		}

		waitFor(t, 60*time.Second, "the refresher make agents-token-refresh-start started to mint a token", func() bool {
			return len(f.api.minted()) >= 1
		})
	})

	t.Run("a refresher that is running is left alone", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")

		if _, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env); err != nil {
			t.Fatalf("the first make agents-token-refresh-start: %v\nstderr: %s", err, stderr)
		}

		waitFor(t, 60*time.Second, "the refresher make agents-token-refresh-start started to mint a token", func() bool {
			return len(f.api.minted()) >= 1
		})

		pidPath := f.statePath("agents-token-refresh-shift2eu.pid")
		pid := readPid(t, pidPath)
		mints := len(f.api.minted())

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("the second make agents-token-refresh-start: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if combined := stdout + stderr; !strings.Contains(combined, strconv.Itoa(pid)) {
			t.Errorf("make agents-token-refresh-start does not say which refresher it found running (pid %d):\n%s", pid, combined)
		}

		if got := readPid(t, pidPath); got != pid {
			t.Errorf("make agents-token-refresh-start replaced the running refresher: the pid file held %d, now %d", pid, got)
		}

		if got := len(f.api.minted()); got != mints {
			t.Errorf("make agents-token-refresh-start started a second refresher: the fake API minted %d token(s) instead of %d", got, mints)
		}
	})

	t.Run("a refresher that holds the lock is left alone even with no pid file", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")

		// A refresher started by hand (make agents-token-refresh) holds the instance's lock and writes no pid
		// file, so the start target must read the lock's pid and not only its own pid file (review N-new-2).
		held := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, f.env)
		lock := f.sb.refreshLockDir("shift2eu")

		waitFor(t, 30*time.Second, "the refresh started by hand to take the lock", func() bool {
			_, err := os.Stat(lock)

			return err == nil
		})

		mints := len(f.api.minted())

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start while another refresher holds the lock: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if combined := stdout + stderr; !strings.Contains(combined, strconv.Itoa(held.pid())) {
			t.Errorf("make agents-token-refresh-start does not name the refresher that holds the lock (pid %d), so a maintainer cannot tell why nothing started:\n%s", held.pid(), combined)
		}

		if _, err := os.Stat(f.statePath("agents-token-refresh-shift2eu.pid")); err == nil {
			t.Errorf("make agents-token-refresh-start wrote a pid file although a refresher already holds the lock: it started a second one")
		}

		if !held.running() {
			t.Fatalf("the refresh that held the lock exited because the start target ran; stderr:\n%s", held.errOut())
		}

		if got := len(f.api.minted()); got != mints {
			t.Errorf("make agents-token-refresh-start minted %d token(s) although a refresher already holds the lock", got-mints)
		}

		held.signal(t, syscall.SIGTERM)

		if !held.waitExit(30 * time.Second) {
			t.Fatalf("the refresh that held the lock did not stop on SIGTERM; stderr:\n%s", held.errOut())
		}
	})

	t.Run("the pid and log files are named after the VM instance", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu-other")

		if _, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env); err != nil {
			t.Fatalf("make agents-token-refresh-start: %v\nstderr: %s", err, stderr)
		}

		for _, name := range []string{"agents-token-refresh-shift2eu-other.pid", "agents-token-refresh-shift2eu-other.log"} {
			if _, err := os.Stat(f.statePath(name)); err != nil {
				t.Errorf("make agents-token-refresh-start wrote no %s: the refresh's pid and log are named after the instance, so two VMs keep two loops (review item 7)", name)
			}
		}

		if _, err := os.Stat(f.statePath("agents-token-refresh-shift2eu.pid")); err == nil {
			t.Errorf("make agents-token-refresh-start named its pid file after another VM instance than shift2eu-other")
		}

		waitFor(t, 60*time.Second, "the refresher of the other instance to mint a token", func() bool {
			return len(f.api.minted()) >= 1
		})
	})
}

// runMake runs make in the repository root with args and env alone, never the test process's own
// environment.
func runMake(t *testing.T, args []string, env map[string]string) (string, string, error) {
	t.Helper()

	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	return runMakeIn(t, root, args, env)
}

// runMakeIn runs make in dir with args and env alone, never the test process's own environment: the
// vm-gh-wrapper tests copy the Makefile and the script it runs into a temporary directory, so one recipe can
// be run against a stand-in generator.
func runMakeIn(t *testing.T, dir string, args []string, env map[string]string) (string, string, error) {
	t.Helper()

	if _, err := exec.LookPath("make"); err != nil {
		t.Fatalf("make is not installed: the repository's commands, and this test, need it")
	}

	cmd := exec.Command("make", append([]string{"-C", dir, "--no-print-directory"}, args...)...)
	cmd.Env = envList(env)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// makeEnv is the environment make runs with in a test: a PATH of the machine's own and a temporary HOME, so
// the Makefile's $(shell git config ...) reads nothing of the maintainer's.
func makeEnv(t *testing.T, extra map[string]string) map[string]string {
	t.Helper()

	env := map[string]string{
		"PATH": "/usr/local/bin:/usr/bin:/bin",
		"HOME": t.TempDir(),
	}

	for name, value := range extra {
		env[name] = value
	}

	return env
}

// makeVariable returns the value a substitution assignment in the Makefile gives name.
func makeVariable(body, name string) (string, bool) {
	match := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*[?:]?=\s*(.*)$`).FindStringSubmatch(body)
	if match == nil {
		return "", false
	}

	return strings.TrimSpace(match[1]), true
}
