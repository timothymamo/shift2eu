// Pins the Makefile's part in the agents App's credential (PRD #161 milestone 1): VM_ENV names no
// SHIFT2EU_AGENTS_APP_* variable, make vm-env refuses one a maintainer adds rather than copying it into the
// VM, make agents-token / make agents-token-refresh run the mint and the refresh, make
// agents-token-refresh-start starts one refresher per VM in the background from the host clone and warns about
// the instance's LaunchDaemon - and about a loaded job that runs another clone's script - make
// agents-token-refresh-install installs the LaunchDaemon after checking the instance's mounts and linting the
// plist, make agents-token-refresh-uninstall removes it, make deck warns about App variables in a workspace
// shell and starts nothing, and make vm-gh-wrapper installs the pinned wrapper an existing VM lacks, whole or
// not at all (.dot-agent-deck/prd161m1-fixes.md items 3, 7 and 8 and .dot-agent-deck/prd161m1-fixes2.md items 1
// and 2, issue #229).
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

	"github.com/google/go-cmp/cmp"
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

		// The refresher takes the lock before it mints (take it after and two starters could both mint),
		// so the lock alone does not say it has a token yet: wait for the mint it makes under the lock and
		// only then take the baseline this case counts from.
		waitFor(t, 60*time.Second, "the refresh started by hand to mint its first token", func() bool {
			return len(f.api.minted()) >= 1
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

	// Issue #229: the LaunchDaemon runs one refresh per instance, so a second one started by hand beside it is a
	// second loop minting for the same VM. Once `launchctl print` answers for the instance's label, the target
	// warns and starts nothing. The fake launchctl answers that print, so these two cases pin both halves on a
	// machine without launchd, which is what Linux CI has.
	t.Run("a loaded LaunchDaemon is warned about and no refresher starts", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")
		f.sb.useFakeLaunchctl(t, f.env, "0")

		label := launchdLabelPrefix + "shift2eu"

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start with the LaunchDaemon %s loaded: %v\nstdout: %s\nstderr: %s", label, err, stdout, stderr)
		}

		for _, want := range []string{"LaunchDaemon", label} {
			if !strings.Contains(stderr, want) {
				t.Errorf("make agents-token-refresh-start does not name %q on stderr although launchd runs the refresh as %s: a maintainer would start a second loop and never learn why not to (issue #229)\nstdout: %s\nstderr: %s", want, label, stdout, stderr)
			}
		}

		// It asked launchd about this instance's job, so the warning is about the job that runs this VM's
		// refresh and not about another instance's. launchctl takes the service target as one argument,
		// `launchctl print system/<label>`, so the same invocation must carry `print` and that target.
		var askedLabel bool

		for _, args := range invocations(f.sb.argvLogText(t), "launchctl") {
			if slices.Contains(args, "print") && slices.Contains(args, "system/"+label) {
				askedLabel = true
			}
		}

		if !askedLabel {
			t.Errorf("make agents-token-refresh-start never ran launchctl print system/%s, so it cannot know the LaunchDaemon runs the refresh:\n%s", label, f.sb.argvLogText(t))
		}

		// The warning is a refusal to start a second loop: no pid file, no log, and no mint.
		entries, err := os.ReadDir(f.state)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read %s: %v", f.state, err)
		}

		for _, entry := range entries {
			t.Errorf("make agents-token-refresh-start left %s in %s although the LaunchDaemon runs the refresh: it must start nothing beside the job (issue #229)", entry.Name(), f.state)
		}

		if got := len(f.api.minted()); got != 0 {
			t.Errorf("make agents-token-refresh-start started a refresher beside the LaunchDaemon: the fake API minted %d token(s) (issue #229)", got)
		}
	})

	t.Run("no loaded LaunchDaemon leaves the start target as it was", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")
		// 113 is what launchctl returns for a job it does not know: the instance's LaunchDaemon is not
		// loaded, so the refresh is still the maintainer's to start by hand.
		f.sb.useFakeLaunchctl(t, f.env, "113")

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start with no LaunchDaemon loaded: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if combined := stdout + stderr; strings.Contains(combined, "LaunchDaemon") {
			t.Errorf("make agents-token-refresh-start warns about a LaunchDaemon although launchctl knows no job for this instance (issue #229):\n%s", combined)
		}

		pid := readPid(t, f.statePath("agents-token-refresh-shift2eu.pid"))

		if !pidAlive(pid) {
			t.Errorf("the pid file holds %d, which is not running: with no LaunchDaemon loaded the target still starts the refresher it always started (issue #229)", pid)
		}
	})
}

// TestMakefileHasTheAgentsTokenLaunchDaemonTargets pins the install target's shape only: it renders the
// instance's plist through dev/agents-token-launchd.sh, which launchdplist_test.go pins in full. Installing
// and uninstalling need sudo and a real launchctl, so neither runs here.
func TestMakefileHasTheAgentsTokenLaunchDaemonTargets(t *testing.T) {
	t.Parallel()

	// The plist goes where launchd reads LaunchDaemons, and a variable holds that directory: a test points it
	// at its own tree, so no test writes into /Library (issue #229, second review).
	if dir, ok := makeVariable(readRepoFile(t, makefilePath), "AGENTS_TOKEN_LAUNCHD_DIR"); !ok || dir != "/Library/LaunchDaemons" {
		t.Errorf("the Makefile's AGENTS_TOKEN_LAUNCHD_DIR is %q (declared: %v), want the default /Library/LaunchDaemons: the install and the uninstall write and remove the job's plist under that directory, and a test must be able to point it at its own tree (issue #229, second review)", dir, ok)
	}

	stdout, stderr, err := runMake(t, []string{"-n", "agents-token-refresh-install"}, makeEnv(t, nil))
	if err != nil {
		t.Fatalf("make -n agents-token-refresh-install: %v\nstderr: %s", err, stderr)
	}

	for _, want := range []string{"agents-token-launchd.sh", "plist"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("make -n agents-token-refresh-install does not mention %q: the target installs the plist dev/agents-token-launchd.sh renders for this VM instance, so one generator and one test cover every target that writes one (issue #229)\n%s", want, stdout)
		}
	}
}

// launchdMountsFormat is the limactl --format argument the install reads the instance's name and mount
// locations with: the instance's name on the first line and one mount location per line after it. The install
// needs the name to tell that limactl answered about the instance it asked about, and the locations to tell
// whether the clone it runs from is one of the directories the VM mounts and can edit (issue #229). The fake
// limactl answers the same shape.
const launchdMountsFormat = `{{.Name}}{{"\n"}}{{range .Config.Mounts}}{{.Location}}{{"\n"}}{{end}}`

// launchdInstallFixture is one environment for a run of make agents-token-refresh-install or
// agents-token-refresh-uninstall: a sandbox with a temporary HOME, a state directory inside it, the log-only
// fake sudo and the fake launchctl, plutil and limactl first on PATH, an invented key, the three App variables
// and a LaunchDaemons directory inside the sandbox. No test reaches the machine's sudo, launchctl or plutil, and
// nothing writes into /Library.
type launchdInstallFixture struct {
	sb       *sandbox
	key      inventedKey
	env      map[string]string
	instance string
	repo     string // the clone make runs in, symlinks resolved: what the plist names and the mount check reads
	dir      string // where make runs: the repository, or a clone a test grew inside its sandbox
}

// launchdInstallOption changes the environment an install fixture runs make with, before the test uses it.
type launchdInstallOption func(t *testing.T, f *launchdInstallFixture)

// withoutOnPath leaves tool off the fixture's PATH, for a subtest that pins what a recipe does on a host that
// has no such tool, such as Linux CI, which has no launchctl (issue #229). The fake tool the fixture installed
// in binDir is removed, so it cannot be found there either, and every other fake tool stays reachable: the
// log-only sudo included, so a refusal that runs it would still be caught.
func withoutOnPath(tool string) launchdInstallOption {
	return func(t *testing.T, f *launchdInstallFixture) {
		t.Helper()

		path := filepath.Join(f.sb.binDir, tool)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove the fake %s from %s: %v", tool, f.sb.binDir, err)
		}

		f.env["PATH"] = f.sb.binDir + ":" + pathWithout(t, tool)
	}
}

// launchdClone writes a clone of the repository at dir: the Makefile and the two scripts an install reads. A
// test that needs the clone somewhere of its own - inside its HOME, for the mount locations the install expands
// `~` into - grows one here, and the real recipe still runs, from the sandbox (issue #229).
func launchdClone(t *testing.T, dir string) string {
	t.Helper()

	writeFileMode(t, filepath.Join(dir, makefilePath), []byte(readRepoFile(t, makefilePath)), 0o600)

	for _, script := range []string{launchdScript, agentsMintScript} {
		writeFileMode(t, filepath.Join(dir, script), []byte(readRepoFile(t, script)), 0o755)
	}

	return dir
}

// resolvedPath returns path with its symlinks resolved, the way the install's own pwd -P resolves the clone it
// runs from, so a test can compare the refusal with the path the install prints.
func resolvedPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve the symlinks of %s: %v", path, err)
	}

	return resolved
}

// inAClone makes make run in a clone grown inside the sandbox instead of in the repository, so the mount check
// reads the clone a test built rather than this worktree.
func inAClone(where func(f *launchdInstallFixture) string) launchdInstallOption {
	return func(t *testing.T, f *launchdInstallFixture) {
		t.Helper()

		clone := launchdClone(t, where(f))
		f.dir, f.repo = clone, resolvedPath(t, clone)
	}
}

// inACloneInsideHome grows the clone under the fixture's HOME, so a mount location `~` or `~/clone` names a
// directory that really holds the clone (issue #229, second review).
func inACloneInsideHome() launchdInstallOption {
	return inAClone(func(f *launchdInstallFixture) string { return filepath.Join(f.env["HOME"], "clone") })
}

// newLaunchdInstallFixture builds the fixture for one VM instance, with a VM that mounts nothing near the clone.
func newLaunchdInstallFixture(t *testing.T, instance string, opts ...launchdInstallOption) *launchdInstallFixture {
	t.Helper()

	sb := newSandbox(t)
	key := writeInventedKey(t, filepath.Join(sb.root, "keys", "agents-app.pem"), 0o600)

	env := sb.baseEnv()
	env["LIMA_INSTANCE"] = instance
	env["SHIFT2EU_AGENTS_APP_ID"] = launchdAppID
	env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = launchdInstallationID
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = key.path

	// Where launchd reads LaunchDaemons from, pointed at the sandbox: the recipe writes the plist under this
	// directory and no test may reach the real /Library (issue #229).
	launchdDir := filepath.Join(sb.root, "LaunchDaemons")
	if err := os.MkdirAll(launchdDir, 0o700); err != nil {
		t.Fatalf("create %s: %v", launchdDir, err)
	}

	env["AGENTS_TOKEN_LAUNCHD_DIR"] = launchdDir

	sb.useLogOnlySudo(t)
	// 113 is what launchctl returns for a job it does not know: no job of this instance is loaded yet.
	sb.useFakeLaunchctl(t, env, "113")
	sb.useFakePlutil(t, env, "0")
	sb.useFakeLimactlMounts(t, env, nil)

	dir, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	f := &launchdInstallFixture{sb: sb, key: key, env: env, instance: instance, repo: launchdRepoRoot(t), dir: dir}

	for _, opt := range opts {
		opt(t, f)
	}

	return f
}

// label is the LaunchDaemon's label of the fixture's instance.
func (f *launchdInstallFixture) label() string { return launchdLabelPrefix + f.instance }

// dest is the file launchd reads the job from: inside the sandbox, since the recipe writes it under
// AGENTS_TOKEN_LAUNCHD_DIR and nothing here may reach /Library (issue #229).
func (f *launchdInstallFixture) dest() string {
	return filepath.Join(f.env["AGENTS_TOKEN_LAUNCHD_DIR"], f.label()+".plist")
}

// run runs one target in the fixture's clone with the fixture's environment, never the test process's own.
func (f *launchdInstallFixture) run(t *testing.T, target string) (string, string, error) {
	t.Helper()

	return runMakeIn(t, f.dir, []string{target}, f.env)
}

// sudoCommands returns the argv of every command the recipe ran through sudo, in call order.
func (f *launchdInstallFixture) sudoCommands(t *testing.T) [][]string {
	t.Helper()

	return invocations(f.sb.argvLogText(t), "sudo")
}

// bootstrapCalls returns the argv of every `sudo launchctl bootstrap` the recipe ran, in call order.
func (f *launchdInstallFixture) bootstrapCalls(t *testing.T) [][]string {
	t.Helper()

	var calls [][]string

	for _, args := range f.sudoCommands(t) {
		if len(args) > 1 && args[0] == "launchctl" && args[1] == "bootstrap" {
			calls = append(calls, args)
		}
	}

	return calls
}

// script is the host clone's refresh script the installed job runs.
func (f *launchdInstallFixture) script() string {
	return filepath.Join(f.repo, "dev", "agents-app-token.sh")
}

// subCommand returns the first sudo invocation whose command is name, failing the test when there is none.
func (f *launchdInstallFixture) subCommand(t *testing.T, name string) []string {
	t.Helper()

	for _, args := range invocations(f.sb.argvLogText(t), "sudo") {
		if len(args) > 0 && args[0] == name {
			return args
		}
	}

	t.Fatalf("the install ran no `sudo %s`; the commands it did run as root: %v", name, invocations(f.sb.argvLogText(t), "sudo"))

	return nil
}

// TestMakeAgentsTokenRefreshInstallRefusesACloneInsideTheVMsMounts pins the reviewer's finding: the clone the
// job runs from must not be one of the directories the VM mounts, and no mount may reach into the clone either.
// The agents write in the mounted workspace, so a job that ran a mounted clone would run their code at every
// boot, as the maintainer, with the App's key in reach. The same goes for a mount the clone itself holds: it
// lets the agents edit the clone's dev/. The install reads the instance's name and mounts with limactl, expands
// `~` and resolves a symlinked location, and refuses before any sudo.
func TestMakeAgentsTokenRefreshInstallRefusesACloneInsideTheVMsMounts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		opts          []launchdInstallOption
		mounts        func(t *testing.T, f *launchdInstallFixture) []string
		wantInstalled bool // the install proceeds and installs the job
	}{
		{
			name:   "the clone itself is a mount of the VM",
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string { return []string{f.repo} },
		},
		{
			name:   "the clone sits under a mount of the VM",
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string { return []string{filepath.Dir(f.repo)} },
		},
		{
			name: "one of several mounts holds the clone",
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string {
				return []string{filepath.Join(f.sb.root, "unrelated"), filepath.Dir(f.repo)}
			},
		},
		{
			name: "the VM mounts nothing near the clone",
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string {
				return []string{filepath.Join(f.sb.root, "unrelated")}
			},
			wantInstalled: true,
		},
		{
			// A mount inside the clone is as bad as a mount above it: through the VM the agents can edit the
			// clone's own dev/agents-app-token.sh, which the job runs at every boot (issue #229, second
			// review).
			name:   "a mount inside the clone",
			opts:   []launchdInstallOption{inACloneInsideHome()},
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string { return []string{filepath.Join(f.repo, "dev")} },
		},
		{
			name:   "a mount inside the clone, written with ~",
			opts:   []launchdInstallOption{inACloneInsideHome()},
			mounts: func(_ *testing.T, _ *launchdInstallFixture) []string { return []string{"~/clone/dev"} },
		},
		{
			// A bare ~ is HOME, and HOME holds the clone: the install must expand it before it compares.
			name:   "a mount that is HOME, which holds the clone",
			opts:   []launchdInstallOption{inACloneInsideHome()},
			mounts: func(_ *testing.T, _ *launchdInstallFixture) []string { return []string{"~"} },
		},
		{
			name:   "a mount HOME holds the clone in, written with ~",
			opts:   []launchdInstallOption{inACloneInsideHome()},
			mounts: func(_ *testing.T, _ *launchdInstallFixture) []string { return []string{"~/clone"} },
		},
		{
			// A mount location that is a symlink to a directory holding the clone: the install resolves it,
			// as the VM does when it mounts the directory the link names.
			name: "a mount location that is a symlink to a directory holding the clone",
			opts: []launchdInstallOption{inACloneInsideHome()},
			mounts: func(t *testing.T, f *launchdInstallFixture) []string {
				t.Helper()

				link := filepath.Join(f.sb.root, "home-link")
				if err := os.Symlink(f.env["HOME"], link); err != nil {
					t.Fatalf("symbolic link %s to %s: %v", link, f.env["HOME"], err)
				}

				return []string{link}
			},
		},
		{
			// A mount beside the clone whose path begins with the clone's: a common text prefix is no parent,
			// and the install must not refuse a clone that merely shares a beginning with a mount.
			name:          "a mount beside the clone whose path begins with the clone's",
			opts:          []launchdInstallOption{inACloneInsideHome()},
			mounts:        func(_ *testing.T, f *launchdInstallFixture) []string { return []string{f.repo + "-sibling"} },
			wantInstalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu", tc.opts...)
			f.sb.useFakeLimactlMounts(t, f.env, tc.mounts(t, f))

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")

			// The install asks this instance for its name and mounts, with a format whose output is the
			// instance's name on the first line and one location per line after it: anything else and it cannot
			// tell that limactl answered about this instance, nor whether the clone is among the mounts.
			wantLimactl := []string{"list", "--format", launchdMountsFormat, "shift2eu"}
			if diff := cmp.Diff([][]string{wantLimactl}, invocations(f.sb.argvLogText(t), "limactl")); diff != "" {
				t.Errorf("the install's limactl calls differ from `limactl list --format ... shift2eu`, which prints the instance's name and then one mount location per line (-want +got):\n%s", diff)
			}

			if tc.wantInstalled {
				if err != nil {
					t.Fatalf("make agents-token-refresh-install refused with no mount holding the clone: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
				}

				if args := f.subCommand(t, "install"); args[len(args)-1] != f.dest() {
					t.Errorf("the install put the plist at %q, want %q (issue #229)", args[len(args)-1], f.dest())
				}

				return
			}

			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 although a mount of the VM reaches the clone it runs from: the agents can edit that clone through the mount, and the job would run their code at every boot, as the maintainer, with the App's key in reach (issue #229)\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make agents-token-refresh-install ran sudo although a mount of the VM reaches the clone, and every root command it runs must come after that check: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
			}

			// The refusal has to say what to do, and where: the host clone. A maintainer who reads only
			// "refused" would have no idea where to run it instead.
			if !strings.Contains(stderr, "host clone") {
				t.Errorf("the refusal does not say to install the job from the host clone, so a maintainer cannot tell where to run it instead:\nstderr: %s", stderr)
			}

			if !strings.Contains(stderr, f.repo) {
				t.Errorf("the refusal does not name the clone %s it refused, so a maintainer cannot tell which directory the VM mounts:\nstderr: %s", f.repo, stderr)
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesAListOfAnotherInstance pins the second review's finding: the install
// must know that limactl answered about the instance it asked about. The format prints the instance's name on
// the first line, so a limactl that fails, answers nothing at all (an instance it does not know) or names
// another instance leaves the install unable to check the clone: it refuses before any sudo, and says which
// instance it could not check.
func TestMakeAgentsTokenRefreshInstallRefusesAListOfAnotherInstance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		answer func(t *testing.T, f *launchdInstallFixture)
	}{
		{
			name: "limactl fails",
			answer: func(t *testing.T, f *launchdInstallFixture) {
				t.Helper()

				f.sb.useFakeLimactlMounts(t, f.env, nil)
				f.sb.useFakeLimactlListFailing(f.env)
			},
		},
		{
			name: "limactl answers nothing, as for an instance it does not know",
			answer: func(t *testing.T, f *launchdInstallFixture) {
				t.Helper()

				f.sb.useFakeLimactlMounts(t, f.env, nil)
				f.sb.useFakeLimactlListEmpty(f.env)
			},
		},
		{
			// The mounts would hold the clone, but they are another instance's: trusting them would let a VM
			// the maintainer never asked about decide whether the install proceeds.
			name: "limactl answers about another instance",
			answer: func(t *testing.T, f *launchdInstallFixture) {
				t.Helper()

				f.sb.useFakeLimactlMounts(t, f.env, []string{filepath.Dir(f.repo)})
				f.sb.useFakeLimactlListNamed(f.env, "another-vm")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")
			tc.answer(t, f)

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 although limactl gave it no list of this instance's mounts: it cannot tell whether the clone it runs from is one the VM can edit, so it must refuse\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make agents-token-refresh-install ran sudo although it never got a usable list of the instance's mounts, and every root command must come after that check: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
			}

			for _, want := range []string{"limactl", f.instance, "host clone"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("the refusal does not name %q, so a maintainer cannot tell which instance's mounts were unreadable nor where to run the install: \nstderr: %s", want, stderr)
				}
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesABadPlistBeforeSudo pins that a plist the generator refuses reaches no
// sudo: root must never be handed a half-rendered or bad job.
func TestMakeAgentsTokenRefreshInstallRefusesABadPlistBeforeSudo(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// launchd starts the job from no directory of its own, so a relative key path is refused.
	f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = filepath.Join("keys", "agents-app.pem")

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 although the plist generator refuses the relative key path %q\nstdout: %s\nstderr: %s", f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"], stdout, stderr)
	}

	if calls := invocations(f.sb.argvLogText(t), "sudo"); len(calls) != 0 {
		t.Errorf("make agents-token-refresh-install ran sudo although the plist generator refused its input, so root got a job the tests never saw whole: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
	}

	if !strings.Contains(stderr, "SHIFT2EU_AGENTS_APP_KEY_FILE") {
		t.Errorf("the refusal does not name SHIFT2EU_AGENTS_APP_KEY_FILE, so a maintainer cannot tell which value to fix:\nstderr: %s", stderr)
	}
}

// TestMakeAgentsTokenRefreshInstallLintsThePlistBeforeSudo pins that the rendered plist is linted, and the very
// file that lint accepted is the one root installs: a plist launchd cannot parse would load no job, and a
// maintainer would believe the refresh runs at boot when nothing does.
func TestMakeAgentsTokenRefreshInstallLintsThePlistBeforeSudo(t *testing.T) {
	t.Parallel()

	t.Run("the linter sees the plist root installs", func(t *testing.T) {
		t.Parallel()

		f := newLaunchdInstallFixture(t, "shift2eu")

		stdout, stderr, err := f.run(t, "agents-token-refresh-install")
		if err != nil {
			t.Fatalf("make agents-token-refresh-install with a plist plutil accepts: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		lints := invocations(f.sb.argvLogText(t), "plutil")
		if len(lints) != 1 {
			t.Fatalf("the install ran plutil %d time(s), want exactly one `plutil -lint <plist>`: %v", len(lints), lints)
		}

		if len(lints[0]) != 2 || lints[0][0] != "-lint" {
			t.Fatalf("the install ran plutil as %v, want `plutil -lint <plist>`: only -lint checks what launchd will parse", lints[0])
		}

		install := f.subCommand(t, "install")
		if len(install) != 9 {
			t.Fatalf("the install ran `sudo %s`, want `sudo install -m 0644 -o root -g wheel <plist> %s`", strings.Join(install, " "), f.dest())
		}

		if install[7] != lints[0][1] {
			t.Errorf("the install lints %q but installs %q: root must install the very plist the linter accepted", lints[0][1], install[7])
		}

		logText := f.sb.argvLogText(t)
		if linted, firstSudo := strings.Index(logText, "tool=plutil"), strings.Index(logText, "tool=sudo"); linted < 0 || firstSudo < 0 || linted > firstSudo {
			t.Errorf("the plist is not linted before the first sudo command: the plutil call is at %d and the first sudo call at %d in the argv log, want plutil first (issue #229)\n%s", linted, firstSudo, logText)
		}
	})

	t.Run("a plist the linter refuses is not installed", func(t *testing.T) {
		t.Parallel()

		f := newLaunchdInstallFixture(t, "shift2eu")
		f.sb.useFakePlutil(t, f.env, "1")

		stdout, stderr, err := f.run(t, "agents-token-refresh-install")
		if err == nil {
			t.Fatalf("make agents-token-refresh-install exited 0 although plutil -lint refused the plist: launchd would load no job, and the maintainer would believe the refresh runs at boot\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		if calls := invocations(f.sb.argvLogText(t), "sudo"); len(calls) != 0 {
			t.Errorf("make agents-token-refresh-install ran sudo although plutil refused the plist, so root installed a job launchd cannot read: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
		}
	})
}

// TestMakeAgentsTokenRefreshInstallPrintsItsSudoCommands pins what root is asked to do, exactly, and that the
// maintainer sees each of those command lines: an install that changes /Library without showing what it runs
// leaves no way to tell what happened.
func TestMakeAgentsTokenRefreshInstallPrintsItsSudoCommands(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err != nil {
		t.Fatalf("make agents-token-refresh-install: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// No job of the instance is loaded, so the install has nothing to boot out first: exactly two root
	// commands, the install of the plist and the bootstrap of the job.
	calls := invocations(f.sb.argvLogText(t), "sudo")
	if len(calls) != 2 {
		t.Fatalf("the install ran %d sudo command(s), want the two that install and load the job: %v", len(calls), calls)
	}

	install := f.subCommand(t, "install")
	if len(install) != 9 {
		t.Fatalf("the install ran `sudo %s`, want `sudo install -m 0644 -o root -g wheel <plist> %s`: the plist is root's and world-readable, as launchd requires of a LaunchDaemon", strings.Join(install, " "), f.dest())
	}

	if diff := cmp.Diff([]string{"install", "-m", "0644", "-o", "root", "-g", "wheel"}, install[:7]); diff != "" {
		t.Errorf("the install's options differ from the ones a LaunchDaemon needs (-want +got):\n%s", diff)
	}

	tmpDir := filepath.Join(f.sb.root, "tmp")
	if !strings.HasPrefix(install[7], tmpDir+string(filepath.Separator)) {
		t.Errorf("the install copies %q, want the plist rendered into %s: nothing else may become the job", install[7], tmpDir)
	}

	if install[8] != f.dest() {
		t.Errorf("the install puts the plist at %q, want %q (issue #229)", install[8], f.dest())
	}

	if diff := cmp.Diff([]string{"launchctl", "bootstrap", "system", f.dest()}, f.subCommand(t, "launchctl")); diff != "" {
		t.Errorf("the install loads the job with a command that differs from `launchctl bootstrap system <plist>` (-want +got):\n%s\nsystem is the domain of a LaunchDaemon: a job bootstrapped into a user's domain starts only when that user logs in (issue #229)", diff)
	}

	// And each of those command lines is printed, exactly as it runs.
	for _, args := range calls {
		want := "+ sudo " + strings.Join(args, " ")
		if !strings.Contains(stdout, want) {
			t.Errorf("the install does not print %q before it runs it, so a maintainer never sees what changed /Library:\nstdout: %s\nstderr: %s", want, stdout, stderr)
		}
	}
}

// TestMakeAgentsTokenRefreshInstallSaysWhichScriptTheJobRuns pins the reviewer's finding: the install says,
// before it runs any sudo command, which script the job will run and that it must be the host clone's. The job
// runs that script at every boot, as the maintainer, with the App's key in reach, and the plist names it, but
// the plist is a wall of XML a maintainer does not read before typing their password.
func TestMakeAgentsTokenRefreshInstallSaysWhichScriptTheJobRuns(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err != nil {
		t.Fatalf("make agents-token-refresh-install: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	firstSudo := strings.Index(stdout, "+ sudo install")

	var named bool

	for _, line := range strings.Split(stdout, "\n") {
		if !strings.Contains(line, f.script()) || !strings.Contains(strings.ToLower(line), "host clone") {
			continue
		}

		named = true

		if at := strings.Index(stdout, line); firstSudo >= 0 && at > firstSudo {
			t.Errorf("the install names the script the job runs only after it printed the first sudo command, so a maintainer reads it too late:\nstdout: %s", stdout)
		}
	}

	if !named {
		t.Errorf("the install never says, before the first sudo command, that the job runs %s from this host clone: the job runs that script at every boot, as the maintainer, with the App's key in reach (issue #229)\nstdout: %s\nstderr: %s", f.script(), stdout, stderr)
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesAPlistRootChanged pins the second review's finding: when the file
// root installed is not the plist the install printed, the install fails closed. It removes the plist again,
// loads nothing, and says both, so a maintainer never ends up with a job, or a file named like one, that
// launchd was never asked about - and never with a stale job the next boot would run.
func TestMakeAgentsTokenRefreshInstallRefusesAPlistRootChanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		install string // what the fake root install leaves behind
	}{
		{
			// A file that changed between the hash check and the install: another writer, or a symlink rooted
			// somewhere else, replaced the plist root was handed.
			name:    "root installed other bytes than the plist it was handed",
			install: "different",
		},
		{
			// Nothing readable landed there at all: no file to compare, and none to load.
			name:    "root installed no file at all",
			install: "nothing",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")
			f.env["SHIFT2EU_TEST_SUDO_INSTALL"] = tc.install

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 although %s: launchd would load a job the maintainer never saw as the printed plist (issue #229)\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			if line := "+ sudo rm -f " + f.dest(); !strings.Contains(stdout, line) {
				t.Errorf("the install does not print %q before it runs it, so a maintainer never sees that the plist it could not check was removed again (issue #229, second review)\nstdout: %s\nstderr: %s", line, stdout, stderr)
			}

			if _, statErr := os.Stat(f.dest()); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("the plist %s is still there after the install refused it (stat: %v): the file launchd reads must not stay behind, or the next boot runs a job nobody loaded and nobody checked (issue #229, second review)", f.dest(), statErr)
			}

			if calls := f.bootstrapCalls(t); len(calls) != 0 {
				t.Errorf("the install loaded the job anyway (%v): a plist it could not check must never become a loaded job (issue #229, second review)", calls)
			}

			lower := strings.ToLower(stderr)

			if strings.Contains(lower, "warning") {
				t.Errorf("the install only warns that it cannot check %s: a plist it cannot verify must fail the install, not leave a maybe-job behind:\nstderr: %s", f.dest(), stderr)
			}

			for _, want := range []string{"removed", "no token refresh job", f.instance} {
				if !strings.Contains(lower, strings.ToLower(want)) {
					t.Errorf("the failure does not say %q: a maintainer has to learn that the plist was removed again and that no job of the instance is loaded now (issue #229, second review)\nstderr: %s", want, stderr)
				}
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallReplacesALoadedJob pins the reinstall: when the instance's job is loaded
// already, the install boots it out, installs the new plist and bootstraps it again, in that order, and the
// wait for the old job to unload ends as soon as launchctl reports it gone.
func TestMakeAgentsTokenRefreshInstallReplacesALoadedJob(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// The job of this instance is loaded now, and a bootout really unloads it: every launchctl print before
	// the bootout succeeds, every one after it fails, as it does for a job that is gone.
	f.sb.useFakeLaunchctlLoaded(t, f.env, false)
	f.sb.useFakeSleep(t)

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err != nil {
		t.Fatalf("make agents-token-refresh-install over a loaded job: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	want := [][]string{
		{"launchctl", "bootout", "system/" + f.label()},
		f.subCommand(t, "install"),
		{"launchctl", "bootstrap", "system", f.dest()},
	}

	if diff := cmp.Diff(want, f.sudoCommands(t)); diff != "" {
		t.Errorf("the install's sudo commands over a loaded job differ from unloading the old job, installing the new plist and loading it again, in that order (-want +got):\n%s\nloading a plist over a loaded label fails, and the old job would keep running the plist the maintainer replaced (issue #229)", diff)
	}

	install := f.subCommand(t, "install")
	if len(install) != 9 || install[8] != f.dest() {
		t.Errorf("the install ran `sudo %s`, want `sudo install -m 0644 -o root -g wheel <plist> %s`", strings.Join(install, " "), f.dest())
	}

	for _, args := range want {
		if line := "+ sudo " + strings.Join(args, " "); !strings.Contains(stdout, line) {
			t.Errorf("the install does not print %q before it runs it, so a maintainer never sees what changed /Library:\nstdout: %s", line, stdout)
		}
	}

	// The old job unloaded at the first check after the bootout, so nothing had to be waited for.
	if waits := invocations(f.sb.argvLogText(t), "sleep"); len(waits) != 0 {
		t.Errorf("the install waited %d time(s) although the bootout had already unloaded the old job: the wait must end as soon as launchctl reports it gone (issue #229, second review): %v", len(waits), waits)
	}

	if body, readErr := os.ReadFile(f.dest()); readErr != nil || !strings.Contains(string(body), "<plist") {
		t.Errorf("the install left no plist at %s after replacing the loaded job (read: %v): a bootout without a new file leaves the instance with no job at all (issue #229)", f.dest(), readErr)
	}
}

// TestMakeAgentsTokenRefreshInstallStopsWhenTheOldJobStaysLoaded pins the second review's finding: a job that
// never unloads must stop the install before it touches the new plist, after a bounded wait. A second plist
// installed while the old job still runs is a job launchd never loads, and a message that claims no job is
// loaded would be a lie.
func TestMakeAgentsTokenRefreshInstallStopsWhenTheOldJobStaysLoaded(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// The old job is loaded and a bootout leaves it loaded: every launchctl print keeps succeeding.
	f.sb.useFakeLaunchctlLoaded(t, f.env, true)
	f.sb.useFakeSleep(t)

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 although the old job never unloaded: it would install a plist launchd loads over a loaded label, if at all, and report a job the maintainer cannot see running (issue #229, second review)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	want := [][]string{{"launchctl", "bootout", "system/" + f.label()}}
	if diff := cmp.Diff(want, f.sudoCommands(t)); diff != "" {
		t.Errorf("the install ran more root commands than the bootout of the old job although that job stayed loaded (-want +got):\n%s\nnothing may be installed or loaded while the old job still runs (issue #229, second review)\nstdout: %s", diff, stdout)
	}

	if _, statErr := os.Stat(f.dest()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("the install put a plist at %s although the old job stayed loaded: launchd loads the file when the job next starts, so the maintainer would run a plist nothing ever checked was loaded (issue #229, second review)", f.dest())
	}

	// The wait is bounded: a bootout that never unloads must not hang the install.
	if waits := len(invocations(f.sb.argvLogText(t), "sleep")); waits == 0 || waits > 10 {
		t.Errorf("the install waited %d time(s) for the old job to unload, want between 1 and 10: the wait must be bounded, and it must happen at all (issue #229, second review)", waits)
	}

	lower := strings.ToLower(stderr)

	if !strings.Contains(lower, "still loaded") {
		t.Errorf("the failure does not say that the old job is still loaded, so a maintainer cannot tell why nothing was installed:\nstderr: %s", stderr)
	}

	if strings.Contains(lower, "no token refresh job") {
		t.Errorf("the failure says no token refresh job is loaded although the old one still is: the maintainer would stop looking for a job that is running (issue #229, second review)\nstderr: %s", stderr)
	}
}

// TestMakeAgentsTokenRefreshInstallRetriesTheBootstrapOnce pins the second review's finding: a bootstrap that
// fails is retried once, never more, and a failure both times says that no job is loaded now and names both
// ways to restore one. The VM's token lives an hour, so a silent failure costs the agents their credentials.
func TestMakeAgentsTokenRefreshInstallRetriesTheBootstrapOnce(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// launchctl bootstrap fails: the job does not load, whatever the plist says.
	f.env["SHIFT2EU_TEST_LAUNCHCTL_BOOTSTRAP_STATUS"] = "1"
	f.sb.useFakeSleep(t)

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 although launchctl bootstrap failed: the maintainer would believe the refresh runs at boot while the VM's token expires within the hour (issue #229)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if calls := f.bootstrapCalls(t); len(calls) != 2 {
		t.Errorf("the install ran launchctl bootstrap %d time(s) with a launchctl that fails, want exactly two, one retry: %v\nstdout: %s\nstderr: %s", len(calls), calls, stdout, stderr)
	}

	lower := strings.ToLower(stderr)

	for _, want := range []string{"no token refresh job", "agents-token-refresh-install", "agents-token-refresh-start"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the failure does not name %q: a maintainer has to learn that no token refresh job is loaded now, and both ways to restore one - run the install again, or start a refresh by hand (issue #229)\nstderr: %s", want, stderr)
		}
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesASymlinkedRefreshScript pins the second review's finding: the job runs
// <clone>/dev/agents-app-token.sh at every boot, as the maintainer, with the App's key in reach. A dev/ that is
// a symlink, or a script that is one, points the job at a file the install never checked, so the install refuses
// both before any sudo.
func TestMakeAgentsTokenRefreshInstallRefusesASymlinkedRefreshScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		symlink string                           // the path inside the clone that is a symlink
		build   func(t *testing.T, clone string) // the rest of the clone, on top of the Makefile
		want    string                           // the symlink the refusal must name, below the clone
	}{
		{
			name: "the refresh script is a symlink",
			build: func(t *testing.T, clone string) {
				t.Helper()

				writeFileMode(t, filepath.Join(clone, launchdScript), []byte(readRepoFile(t, launchdScript)), 0o755)

				if err := os.Symlink(scriptPath(t, agentsMintScript), filepath.Join(clone, agentsMintScript)); err != nil {
					t.Fatalf("symbolic link %s: %v", filepath.Join(clone, agentsMintScript), err)
				}
			},
			want: filepath.Join("dev", "agents-app-token.sh"),
		},
		{
			name: "the clone's dev directory is a symlink",
			build: func(t *testing.T, clone string) {
				t.Helper()

				if err := os.Symlink(filepath.Dir(scriptPath(t, agentsMintScript)), filepath.Join(clone, "dev")); err != nil {
					t.Fatalf("symbolic link %s: %v", filepath.Join(clone, "dev"), err)
				}
			},
			want: "dev",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")

			clone := filepath.Join(f.sb.root, "clone")
			writeFileMode(t, filepath.Join(clone, makefilePath), []byte(readRepoFile(t, makefilePath)), 0o600)
			tc.build(t, clone)

			f.dir, f.repo = clone, resolvedPath(t, clone)

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 although %s is a symlink: the job would run a file outside the clone at every boot, as the maintainer, with the App's key in reach (issue #229, second review)\nstdout: %s\nstderr: %s", tc.want, stdout, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make agents-token-refresh-install ran sudo although %s is a symlink, and every root command must come after that check: %v\nstdout: %s\nstderr: %s", tc.want, calls, stdout, stderr)
			}

			lower := strings.ToLower(stderr)

			for _, want := range []string{"symlink", f.repo} {
				if !strings.Contains(lower, strings.ToLower(want)) {
					t.Errorf("the refusal does not name %q: a maintainer has to learn that the clone's own path is a symlink, and which clone was refused (issue #229, second review)\nstderr: %s", want, stderr)
				}
			}
		})
	}
}

// TestMakeAgentsTokenRefreshUninstall pins that uninstalling needs a launchctl - the LaunchDaemon is a macOS
// thing, and removing the file without booting the job out of launchd leaves it running - and that it boots the
// job out and removes its file through sudo, one label per instance.
func TestMakeAgentsTokenRefreshUninstall(t *testing.T) {
	t.Parallel()

	t.Run("no launchctl: a refusal, before any sudo", func(t *testing.T) {
		t.Parallel()

		// The fake launchctl is not on this fixture's PATH, so make agents-token-refresh-uninstall runs on a
		// host that has none, as Linux CI does; the log-only sudo stays reachable.
		f := newLaunchdInstallFixture(t, "shift2eu", withoutOnPath("launchctl"))

		stdout, stderr, err := f.run(t, "agents-token-refresh-uninstall")
		if err == nil {
			t.Fatalf("make agents-token-refresh-uninstall exited 0 on a host with no launchctl: the LaunchDaemon is for the Mac, and the target must say so and run nothing\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		if calls := invocations(f.sb.argvLogText(t), "sudo"); len(calls) != 0 {
			t.Errorf("make agents-token-refresh-uninstall ran sudo on a host with no launchctl: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
		}

		for _, want := range []string{"LaunchDaemon", "Mac"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("the refusal does not name %q, so a maintainer on another host cannot tell why nothing was removed:\nstderr: %s", want, stderr)
			}
		}
	})

	t.Run("with launchctl: boot the job out and remove its plist through sudo", func(t *testing.T) {
		t.Parallel()

		f := newLaunchdInstallFixture(t, "shift2eu")

		stdout, stderr, err := f.run(t, "agents-token-refresh-uninstall")
		if err != nil {
			t.Fatalf("make agents-token-refresh-uninstall: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		want := [][]string{
			{"launchctl", "bootout", "system/" + f.label()},
			{"rm", "-f", f.dest()},
		}

		if diff := cmp.Diff(want, invocations(f.sb.argvLogText(t), "sudo")); diff != "" {
			t.Errorf("the uninstall's sudo commands differ from booting the instance's job out of the system domain and removing its plist (-want +got):\n%s\nbootout takes the job's service target, system/<label>, so the job that stops is this instance's, and rm removes only the plist this instance's label names: another instance's job and file stay (issue #229)", diff)
		}

		for _, args := range want {
			if line := "+ sudo " + strings.Join(args, " "); !strings.Contains(stdout, line) {
				t.Errorf("the uninstall does not print %q before it runs it:\nstdout: %s\nstderr: %s", line, stdout, stderr)
			}
		}
	})
}

// launchctlPrintBlock renders what `launchctl print system/<label>` prints for a loaded job: a `program` line
// and an `arguments` block that holds one argument per line, tab-indented, the way the real command prints a
// job's arguments. The start target reads the script the job runs from that block.
func launchctlPrintBlock(label, script, log string) string {
	return "system/" + label + " = {\n" +
		"\tactive count = 1\n" +
		"\tpath = /Library/LaunchDaemons/" + label + ".plist\n" +
		"\ttype = LaunchDaemon\n" +
		"\tstate = running\n" +
		"\n" +
		"\tprogram = /bin/sh\n" +
		"\targuments = {\n" +
		"\t\t/bin/sh\n" +
		"\t\t-c\n" +
		"\t\tumask 077 && mkdir -p \"${1%/*}\" && exec /bin/sh \"$0\" refresh >>\"$1\" 2>&1\n" +
		"\t\t" + script + "\n" +
		"\t\t" + log + "\n" +
		"\t}\n" +
		"}\n"
}

// TestMakeAgentsTokenRefreshStartWarnsAboutAnotherClonesJob pins the reviewer's finding: launchctl print says
// which script the loaded job runs, and when that is not this clone's dev/agents-app-token.sh the warning says
// so and names the script. Otherwise a maintainer reads "a LaunchDaemon runs this VM's refresh" and believes it
// runs the clone they edit.
func TestMakeAgentsTokenRefreshStartWarnsAboutAnotherClonesJob(t *testing.T) {
	t.Parallel()

	label := launchdLabelPrefix + "shift2eu"
	repo := launchdRepoRoot(t)
	otherScript := filepath.Join(t.TempDir(), "another-clone", "dev", "agents-app-token.sh")

	cases := []struct {
		name       string
		script     string
		wantWarned bool
	}{
		{name: "the job runs another clone's script", script: otherScript, wantWarned: true},
		{name: "the job runs this clone's script", script: filepath.Join(repo, "dev", "agents-app-token.sh")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, "shift2eu")
			log := filepath.Join(f.state, "agents-token-refresh-shift2eu.log")
			f.sb.useFakeLaunchctlPrint(t, f.env, "0", launchctlPrintBlock(label, tc.script, log))

			stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
			if err != nil {
				t.Fatalf("make agents-token-refresh-start with the LaunchDaemon %s loaded: %v\nstdout: %s\nstderr: %s", label, err, stdout, stderr)
			}

			// The warning about the loaded job is there either way, and it still starts nothing.
			for _, want := range []string{"LaunchDaemon", label} {
				if !strings.Contains(stderr, want) {
					t.Errorf("make agents-token-refresh-start does not name %q on stderr although launchd runs the refresh as %s (issue #229)\nstdout: %s\nstderr: %s", want, label, stdout, stderr)
				}
			}

			if tc.wantWarned {
				var named bool

				for _, line := range strings.Split(stderr, "\n") {
					if !strings.Contains(line, tc.script) {
						continue
					}

					named = true

					if !strings.Contains(strings.ToLower(line), "another clone") {
						t.Errorf("the warning names %s but does not say it is another clone's script, so a maintainer still believes the job runs this clone's:\nstderr: %s", tc.script, stderr)
					}
				}

				if !named {
					t.Errorf("make agents-token-refresh-start does not name %s on stderr, the script the loaded LaunchDaemon runs, so a maintainer edits one clone while the job runs another's (issue #229)\nstdout: %s\nstderr: %s", tc.script, stdout, stderr)
				}
			} else if strings.Contains(strings.ToLower(stderr), "another clone") {
				t.Errorf("make agents-token-refresh-start warns about another clone's script although the loaded job runs this clone's %s:\nstderr: %s", tc.script, stderr)
			}

			if _, err := os.Stat(f.statePath("agents-token-refresh-shift2eu.pid")); err == nil {
				t.Errorf("make agents-token-refresh-start started a second refresher beside the LaunchDaemon: it wrote a pid file (issue #229)")
			}
		})
	}
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
