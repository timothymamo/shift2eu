// Pins the account mode of the host targets and the VM setup target of issue #227 part 2
// (docs/adr/0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md, update of
// 2026-11-19; docs/adr/0034-run-the-agents-app-token-refresh-as-a-launchdaemon.md, its update of the same date).
//
// The App's key moves to the hidden macOS account `_shift2euagents`: SHIFT2EU_AGENTS_ACCOUNT turns the token
// targets over to it - the maintainer cannot mint any more, and the plist is rendered as the account with the
// account's own generator - and make vm-agents-token-ssh installs the one writer the account's SSH key may run.
//
// Every run is a sandbox run: a fake sudo that records every command and runs nothing of the machine's own, a
// fake launchctl, plutil and limactl, and, for the account's home under /Users - which no test can create - a fake
// sudo that answers for the files the targets read, render and write there. No test reaches /Library, a real
// launchd, a real account, a real VM or the network (CLAUDE.md rules 2 and 3).
package devenv_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// agentsAccount is the hidden role account of ADR 0031's update: a name with an underscore, as macOS's own
// daemons have, so the tests never name a real login account.
const agentsAccount = "_shift2euagents"

// agentsAccountHome is the account's home as the make targets derive it, /Users/<account> (ADR 0031, update of
// 2026-11-19, 1). No test creates it: the log-only sudo answers for the files there.
func agentsAccountHome() string {
	return "/Users/" + agentsAccount
}

// accountEnv turns a fixture's environment into the account mode of the token targets: the account, the VM's
// pinned SSH port and the VM user, which the host clone's .envrc holds beside the account's name.
func accountEnv(env map[string]string) map[string]string {
	env["SHIFT2EU_AGENTS_ACCOUNT"] = agentsAccount
	env["SHIFT2EU_AGENTS_VM_SSH_PORT"] = sshModePort
	env["SHIFT2EU_AGENTS_VM_SSH_USER"] = sshModeUser

	return env
}

// refusalTimeout is how long a target that must refuse an account-mode run may take before the test gives up: a
// target that runs instead of refusing (make agents-token-refresh loops until it is stopped) fails the test there
// instead of hanging it.
const refusalTimeout = 20 * time.Second

// runMakeForRefusal runs make like runMake does - in the repository, under env alone - in a process group of its
// own, and stops it after refusalTimeout with everything it started: the account-mode targets must refuse in
// milliseconds, and a target that offers to run instead would otherwise keep the suite waiting.
func runMakeForRefusal(t *testing.T, target string, env map[string]string) (string, string, error) {
	t.Helper()

	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	output := t.TempDir()
	stdoutPath, stderrPath := filepath.Join(output, "stdout"), filepath.Join(output, "stderr")

	cmd := exec.Command("make", "-C", root, "--no-print-directory", target)
	cmd.Env = envList(env)
	cmd.Dir = root

	group := newProcessGroup(t)
	t.Cleanup(group.end)
	group.join(cmd)

	stdout, stderr := createOutput(t, stdoutPath), createOutput(t, stderrPath)
	cmd.Stdout, cmd.Stderr = stdout, stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start make %s: %v", target, err)
	}

	// make holds its own descriptors now; the test reads the files by name.
	for _, file := range []*os.File{stdout, stderr} {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close %s: %v", file.Name(), closeErr)
		}
	}

	done := make(chan error, 1)

	go func() { done <- cmd.Wait() }()

	select {
	case runErr := <-done:
		return readOutput(stdoutPath), readOutput(stderrPath), runErr
	case <-time.After(refusalTimeout):
		t.Fatalf("make %s ran for %s without refusing, so the test stopped it and everything it started: in account mode it refuses before it runs anything (ADR 0031, update of 2026-11-19, 5)\nstdout: %s\nstderr: %s",
			target, refusalTimeout, readOutput(stdoutPath), readOutput(stderrPath))

		return "", "", nil
	}
}

// TestMakeTokenTargetsRefuseInAccountMode pins the first refusal of the account mode (ADR 0031, update of
// 2026-11-19, 5): with SHIFT2EU_AGENTS_ACCOUNT set, make agents-token, make agents-token-refresh and make
// agents-token-refresh-start refuse before they run anything, because the key is the account's and the
// maintainer's uid cannot read it any more; the message sends the maintainer to launchctl and the docs.
func TestMakeTokenTargetsRefuseInAccountMode(t *testing.T) {
	t.Parallel()

	want := []string{
		"the refresh runs as " + agentsAccount + ", which holds the App's key",
		"sudo launchctl kickstart -k system/" + launchdLabelPrefix + hostTargetInstance,
		"docs/dev-environment.md",
	}

	for _, target := range []string{"agents-token", "agents-token-refresh", "agents-token-refresh-start"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, hostTargetInstance)
			accountEnv(f.env)

			stdout, stderr, err := runMakeForRefusal(t, target, f.env)
			if err == nil {
				t.Fatalf("make %s exited 0 with SHIFT2EU_AGENTS_ACCOUNT set: the account holds the App's key, so the maintainer cannot mint any more (ADR 0031, update of 2026-11-19, 5)\nstdout: %s\nstderr: %s", target, stdout, stderr)
			}

			for _, fragment := range want {
				if !strings.Contains(stderr, fragment) {
					t.Errorf("make %s's refusal does not say %q, so a maintainer cannot tell why it refused or where to mint now (ADR 0031, update of 2026-11-19, 5)\nstdout: %s\nstderr: %s",
						target, fragment, stdout, stderr)
				}
			}

			if strings.TrimSpace(stdout) != "" {
				t.Errorf("make %s printed on stdout although it refused: the refusal comes before anything runs (ADR 0031, update of 2026-11-19, 5)\nstdout: %s", target, stdout)
			}

			for _, tool := range []string{"curl", "limactl", "launchctl", "ssh", "sudo"} {
				if calls := invocations(f.sb.argvLogText(t), tool); len(calls) > 0 {
					t.Errorf("make %s ran %s %v although it refused: in account mode the maintainer's shell mints nothing and loads nothing (ADR 0031, update of 2026-11-19, 5)",
						target, tool, calls[0])
				}
			}

			if minted := f.api.minted(); len(minted) != 0 {
				t.Errorf("make %s minted %d token(s) although it refused", target, len(minted))
			}
		})
	}
}

// TestMakeTokenTargetsRefuseABadAgentsAccount pins the name check of ADR 0031's update (item 1): the account's
// name becomes part of root's commands and of the job's paths, so a name that is empty, begins with a dash, with a
// dot or with a digit, holds a character outside letters, digits, . _ and -, or names root or the maintainer is
// refused (issue #227 part 2, security audit 5 and review 7).
func TestMakeTokenTargetsRefuseABadAgentsAccount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		account string
	}{
		{name: "a name beginning with a dash", account: "-shift2euagents"},
		{name: "a name of one dash and a letter", account: "-x"},
		{name: "a dot, which is no account name", account: "."},
		{name: "two dots, which is no account name", account: ".."},
		{name: "a name beginning with a dot", account: ".foo"},
		{name: "a name beginning with a digit", account: "1abc"},
		{name: "a name holding a space", account: "shift 2eu agents"},
		{name: "a name holding a slash", account: "agents/account"},
		{name: "a name holding a colon", account: "agents:account"},
		{name: "the root account", account: "root"},
		{name: "the maintainer's own account", account: currentUsername(t)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, hostTargetInstance)
			accountEnv(f.env)
			f.env["SHIFT2EU_AGENTS_ACCOUNT"] = tc.account

			stdout, stderr, err := runMakeForRefusal(t, "agents-token", f.env)
			if err == nil {
				t.Fatalf("make agents-token exited 0 with SHIFT2EU_AGENTS_ACCOUNT=%q: the name reaches root's commands and the job's paths, so a bad one is refused (ADR 0031, update of 2026-11-19, 1)\nstdout: %s\nstderr: %s",
					tc.account, stdout, stderr)
			}

			if !strings.Contains(stderr, "SHIFT2EU_AGENTS_ACCOUNT") {
				t.Errorf("the refusal of %q does not name SHIFT2EU_AGENTS_ACCOUNT, so a maintainer cannot tell which variable to fix:\n%s", tc.account, stderr)
			}

			for _, tool := range []string{"curl", "limactl", "launchctl", "ssh", "sudo"} {
				if calls := invocations(f.sb.argvLogText(t), tool); len(calls) > 0 {
					t.Errorf("make agents-token ran %s %v although it refused the account name %q", tool, calls[0], tc.account)
				}
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallRendersAsTheAccount pins step 3 of ADR 0034's update: in account mode the
// maintainer's clone installs the job but renders nothing with the key, so the plist comes from the account's own
// generator, run by one printed sudo with the account's environment and the derived paths and nothing inherited;
// a rendering that cannot run leaves nothing installed and still ends with sudo -k, so no sudo timestamp outlives
// the install.
func TestMakeAgentsTokenRefreshInstallRendersAsTheAccount(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, hostTargetInstance)
	accountEnv(f.env)

	home := agentsAccountHome()
	secrets := filepath.Join(home, ".config", "shift2eu")

	want := []string{
		"-u", agentsAccount,
		"-H", "/usr/bin/env", "-i",
		"HOME=" + home,
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"LIMA_INSTANCE=" + f.instance,
		"SHIFT2EU_AGENTS_APP_ID=" + launchdAppID,
		"SHIFT2EU_AGENTS_APP_INSTALLATION_ID=" + launchdInstallationID,
		"SHIFT2EU_AGENTS_APP_KEY_FILE=" + filepath.Join(secrets, "agents-app.pem"),
		"SHIFT2EU_AGENTS_VM_SSH_PORT=" + sshModePort,
		"SHIFT2EU_AGENTS_VM_SSH_USER=" + sshModeUser,
		"SHIFT2EU_AGENTS_VM_SSH_KEY=" + filepath.Join(secrets, "agents-vm-ssh-key"),
		"SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS=" + filepath.Join(secrets, "known-hosts-"+f.instance),
		"/bin/sh",
		filepath.Join(home, "shift2eu-host", "dev", "agents-token-launchd.sh"),
		"plist",
	}

	stdout, stderr, _ := f.run(t, "agents-token-refresh-install")

	sudo := f.sudoCommands(t)
	if len(sudo) == 0 {
		t.Fatalf("the install ran no sudo: in account mode the plist is rendered as the account with its own generator, so the job's UserName, HOME and paths are the account's (ADR 0034, update of 2026-11-19, c)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if diff := cmp.Diff(want, sudo[0]); diff != "" {
		t.Errorf("the install's first sudo is not the one the ADR gives, with the values on the command line and nothing inherited (-want +got):\n%s", diff)
	}

	if !strings.Contains(stdout, "+ sudo -u "+agentsAccount+" -H ") {
		t.Errorf("the install does not print the command it runs as the account with a + before it, as it prints every sudo:\nstdout: %s", stdout)
	}

	// The account's key is the derived path: a SHIFT2EU_AGENTS_APP_KEY_FILE of the maintainer's must not reach
	// the job (ADR 0031, update of 2026-11-19, 2 and 5).
	if strings.Contains(strings.Join(sudo[0], " "), f.key.path) {
		t.Errorf("the render hands the job the maintainer's key path %s: in account mode the make targets derive the account's own (ADR 0031, update of 2026-11-19, 2)", f.key.path)
	}

	for _, args := range sudo {
		if len(args) > 0 && (args[0] == "install" || args[0] == "launchctl") {
			t.Errorf("the install ran sudo %v although the plist could not be rendered as the account: nothing is installed unchecked", args)
		}
	}

	assertEndsWithSudoK(t, sudo)

	if _, err := os.Stat(f.dest()); err == nil {
		t.Errorf("%s is there although no plist was rendered: the install installs only what the account rendered", f.dest())
	}
}

// accountModePlist is the plist the account's own generator would render, from the clone under
// /Users/<account>/shift2eu-host that no test machine has: invented XML, since only the fake plutil reads it and the
// install compares the file it installed with the plist it was handed (CLAUDE.md rule 2).
const accountModePlist = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>io.github.timothymamo.shift2eu.agents-token-refresh.shift2eu</string>
</dict>
</plist>
`

// TestMakeAgentsTokenRefreshInstallAsTheAccountLoadsTheJob pins the account-mode install through to a loaded job
// (ADR 0034, update of 2026-11-19, c): once the account's own generator has rendered the plist - the one step no
// test can run, so the log-only sudo answers it with a fixture - the install lints that plist, installs it, loads
// the job and tells the maintainer how to check it, all through sudo, and creates nothing in the maintainer's state
// directory: in account mode the log, the state and the lock are the account's, and the install reads no lock at
// all. It still ends with sudo -k, so no timestamp outlives it.
func TestMakeAgentsTokenRefreshInstallAsTheAccountLoadsTheJob(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, hostTargetInstance)
	accountEnv(f.env)

	// The recording cat shows which files the recipe reads with it, the refresh's lock among them.
	f.sb.useRecordingCat(t)

	// The plist the account rendered, which the log-only sudo answers the render with: /Users/<account> is on no
	// test machine.
	rendered := filepath.Join(f.sb.root, "rendered-by-the-account.plist")
	writeFileMode(t, rendered, []byte(accountModePlist), 0o600)
	f.env["SHIFT2EU_TEST_ACCOUNT_PLIST_FILE"] = rendered

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err != nil {
		t.Fatalf("make agents-token-refresh-install exited %d although the account rendered the plist: the install lints, installs and loads what the account rendered (ADR 0034, update of 2026-11-19, c)\nstdout: %s\nstderr: %s",
			exitStatusOf(t, err), stdout, stderr)
	}

	accountHome := agentsAccountHome()
	accountState := filepath.Join(accountHome, ".local", "state", "shift2eu")
	accountScript := filepath.Join(accountHome, "shift2eu-host", "dev", "agents-app-token.sh")
	accountLog := filepath.Join(accountState, "agents-token-refresh-"+hostTargetInstance+".log")

	// The job launchd loads is the plist the account rendered: the file at the LaunchDaemon's path holds its bytes.
	installed, err := os.ReadFile(f.dest())
	if err != nil {
		t.Fatalf("the install loaded no plist at %s: %v\nstdout: %s\nstderr: %s", f.dest(), err, stdout, stderr)
	}

	if diff := cmp.Diff(accountModePlist, string(installed)); diff != "" {
		t.Errorf("the plist the install put at %s is not the one the account rendered (-want +got):\n%s", f.dest(), diff)
	}

	bootstraps := f.bootstrapCalls(t)
	if len(bootstraps) != 1 {
		t.Fatalf("the install ran `sudo launchctl bootstrap` %d time(s), want the one that loads the job: %v", len(bootstraps), bootstraps)
	}

	if diff := cmp.Diff([]string{"launchctl", "bootstrap", "system", f.dest()}, bootstraps[0]); diff != "" {
		t.Errorf("the install loads the job with a command that differs from `launchctl bootstrap system <plist>` (-want +got):\n%s", diff)
	}

	// The account mode's own last line: the maintainer checks the job and its log as the account, since neither is
	// the maintainer's.
	wantCheck := "agents-token-refresh-install: check it with: sudo launchctl print system/" + f.label() +
		" (state = running, and " + accountScript + " in its arguments), and its log: sudo tail -n 50 " + accountLog +
		"; then run sudo -k"
	if !strings.Contains(stdout, wantCheck) {
		t.Errorf("the install does not print the account mode's line\n%s\nso a maintainer is sent to the maintainer's own log, which the job never writes (ADR 0034, update of 2026-11-19, c)\nstdout: %s",
			wantCheck, stdout)
	}

	// Nothing of the account's install is the maintainer's: no state directory and no log, in either place the
	// recipe derives the maintainer's state from.
	for _, state := range []string{f.sb.stateDir(), filepath.Join(f.env["HOME"], ".local", "state", "shift2eu")} {
		if _, err := os.Stat(state); err == nil {
			t.Errorf("the install created the maintainer's state directory %s: in account mode the state and the log are the account's, %s (ADR 0034, update of 2026-11-19, c)", state, accountState)
		}

		log := filepath.Join(state, "agents-token-refresh-"+hostTargetInstance+".log")
		if _, err := os.Stat(log); err == nil {
			t.Errorf("the install created %s: in account mode the job logs into the account's own %s, never into the maintainer's state (ADR 0034, update of 2026-11-19, c)", log, accountLog)
		}
	}

	// No lock read: the install warns about a refresh started by hand from the lock of the instance it installs, and
	// in account mode that lock is the account's, which the maintainer's install never reads. The plist is printed
	// with cat, so the recorder shows that this check could fire.
	printedThePlist := false

	for _, args := range invocations(f.sb.argvLogText(t), "cat") {
		for _, arg := range args {
			if strings.Contains(filepath.Base(arg), "agents-token-refresh-plist.") {
				printedThePlist = true
			}

			if strings.Contains(arg, ".lock") {
				t.Errorf("the install read the refresh's lock through %s: in account mode the lock is the account's, %s, and the maintainer's install reads no lock at all (ADR 0034, update of 2026-11-19, c)",
					arg, filepath.Join(accountState, "agents-token-refresh-"+hostTargetInstance+".lock", "pid"))
			}
		}
	}

	if !printedThePlist {
		t.Fatalf("the recording cat saw no read of the rendered plist, so this test cannot tell a lock read from any other cat: the install prints the plist with cat before it installs it\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	assertEndsWithSudoK(t, f.sudoCommands(t))
}

// TestMakeAgentsTokenRefreshUninstallEndsWithSudoK pins the other half of ADR 0034's update: the uninstall works
// the same in both modes and, in account mode, ends with sudo -k too, so the sudo timestamp its commands started
// does not outlive it.
func TestMakeAgentsTokenRefreshUninstallEndsWithSudoK(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, hostTargetInstance)
	accountEnv(f.env)

	// The plist a loaded job was installed from, so the uninstall removes a file as it does on the Mac.
	writeFileMode(t, f.dest(), []byte("a plist an earlier install left\n"), 0o644)

	stdout, stderr, err := f.run(t, "agents-token-refresh-uninstall")
	if err != nil {
		t.Fatalf("make agents-token-refresh-uninstall in account mode: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	assertEndsWithSudoK(t, f.sudoCommands(t))

	if _, err := os.Stat(f.dest()); err == nil {
		t.Errorf("%s is still there after the uninstall", f.dest())
	}
}

// assertEndsWithSudoK fails unless the last command the recipe ran through sudo is `sudo -k`: the account-mode
// targets end with it, on success and on failure (ADR 0034, update of 2026-11-19, c).
func assertEndsWithSudoK(t *testing.T, sudo [][]string) {
	t.Helper()

	if len(sudo) == 0 {
		t.Fatalf("the run ran no sudo at all")
	}

	if diff := cmp.Diff([]string{"-k"}, sudo[len(sudo)-1]); diff != "" {
		t.Errorf("the last sudo the run made is not `sudo -k`, so a sudo timestamp outlives an account-mode target (ADR 0034, update of 2026-11-19, c) (-want +got):\n%s\nevery sudo the run made: %v", diff, sudo)
	}
}

// vmAgentsTokenSSHFixture is one environment for a run of make vm-agents-token-ssh (ADR 0031, update of
// 2026-11-19, 4): the account mode's variables, a fake limactl that answers the VM's pinned SSH port and stands in
// for the VM, a log-only sudo that answers for the account's public key and records the pinned host key, and the
// fake API's server, which this target never uses.
type vmAgentsTokenSSHFixture struct {
	sb         *sandbox
	env        map[string]string
	portFile   string // what `limactl list --format '{{.SSHLocalPort}}'` answers
	pubKeyFile string // what `sudo ... /bin/cat .../agents-vm-ssh-key.pub` answers
	pinCapture string // what the pin writes on standard input
	capture    string // everything the target sends the VM over limactl shell's standard input
}

// The VM's host key the fake limactl reports, and the account's public key the fake sudo answers with: invented
// base64, never a real key (CLAUDE.md rule 2).
const (
	fakeHostKey   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGxldHNpbnZlbnRhbmV4YW1wbGVrZXlibG9i"
	fakeVMPubKey  = fakeHostKey + " the-maintainer@example.com\n"
	sshWriterPath = "/usr/local/libexec/shift2eu/agents-token-write"
	sshRealGHTool = "/usr/local/libexec/shift2eu/gh"
)

// fakeVMPubKeyLine is the key as the generator receives it: the line the account's key file holds, without the
// line break that file ends with, since the make recipe reads it through a command substitution. Passing the line
// break instead is a second line to the generator, which refuses it.
var fakeVMPubKeyLine = strings.TrimSuffix(fakeVMPubKey, "\n")

// newVMAgentsTokenSSHFixture builds the fixture: the pinned port the ADR's example uses, the account's public key
// with a comment that must not reach the VM's keys file, and a fake limactl that records what it is sent.
func newVMAgentsTokenSSHFixture(t *testing.T) *vmAgentsTokenSSHFixture {
	t.Helper()

	sb := newSandbox(t)
	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"
	accountEnv(env)

	f := &vmAgentsTokenSSHFixture{
		sb:         sb,
		env:        env,
		portFile:   filepath.Join(sb.root, "lima-ssh-port"),
		pubKeyFile: filepath.Join(sb.root, "account-vm-ssh-key.pub"),
		pinCapture: filepath.Join(sb.root, "known-hosts-pinned"),
		capture:    filepath.Join(sb.root, "lima-stdin.log"),
	}

	f.setPort(t, sshModePort)
	writeFileMode(t, f.pubKeyFile, []byte(fakeVMPubKey), 0o600)

	writeFileMode(t, filepath.Join(sb.binDir, "limactl"), []byte(fakeAccountLimactl), 0o700)

	env["SHIFT2EU_TEST_LIMA_SSH_PORT_FILE"] = f.portFile
	env["SHIFT2EU_TEST_LIMA_LIST_NAME"] = hostTargetInstance
	env["SHIFT2EU_TEST_LIMA_CAPTURE"] = f.capture
	env["SHIFT2EU_TEST_ACCOUNT_HOME"] = agentsAccountHome()
	env["SHIFT2EU_TEST_ACCOUNT_PUBKEY_FILE"] = f.pubKeyFile
	env["SHIFT2EU_TEST_ACCOUNT_PIN_CAPTURE"] = f.pinCapture

	sb.useLogOnlySudo(t)

	return f
}

// setPort makes the fake limactl answer port for the instance's SSH local port.
func (f *vmAgentsTokenSSHFixture) setPort(t *testing.T, port string) {
	t.Helper()

	writeFileMode(t, f.portFile, []byte(port+"\n"), 0o600)
}

// setPubKey makes the fake sudo answer with what the account's public key file holds.
func (f *vmAgentsTokenSSHFixture) setPubKey(t *testing.T, line string) {
	t.Helper()

	writeFileMode(t, f.pubKeyFile, []byte(line), 0o600)
}

// run runs one target of the fixture in the repository, the host clone of these tests.
func (f *vmAgentsTokenSSHFixture) run(t *testing.T, target string) (string, string, error) {
	t.Helper()

	return runMake(t, []string{target}, f.env)
}

// shellCalls returns the argv of every `limactl shell` the target ran.
func (f *vmAgentsTokenSSHFixture) shellCalls(t *testing.T) [][]string {
	t.Helper()

	var calls [][]string

	for _, args := range invocations(f.sb.argvLogText(t), "limactl") {
		if len(args) > 0 && args[0] == "shell" {
			calls = append(calls, args)
		}
	}

	return calls
}

// pinned returns what the target wrote into the account's known-hosts file, or "" when it never ran the pin.
func (f *vmAgentsTokenSSHFixture) pinned(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(f.pinCapture)
	if err != nil {
		return ""
	}

	return string(body)
}

// setAccount names another account, as SHIFT2EU_AGENTS_ACCOUNT does: its home, and with it the public key the
// fake sudo answers with and the known-hosts file the pin writes, follow the name (ADR 0031, update of
// 2026-11-19, 1).
func (f *vmAgentsTokenSSHFixture) setAccount(t *testing.T, name string) {
	t.Helper()

	f.env["SHIFT2EU_AGENTS_ACCOUNT"] = name
	f.env["SHIFT2EU_TEST_ACCOUNT_HOME"] = "/Users/" + name
}

// setReply says what the fake VM prints back: the install's host key and done line (the default), the
// uninstall's done line, nothing at all, or the done line alone (issue #227 part 2, review 5).
func (f *vmAgentsTokenSSHFixture) setReply(t *testing.T, reply string) {
	t.Helper()

	f.env["SHIFT2EU_TEST_LIMA_REPLY"] = reply
}

// sudoCommands returns the argv of every sudo the target ran, in call order.
func (f *vmAgentsTokenSSHFixture) sudoCommands(t *testing.T) [][]string {
	t.Helper()

	return invocations(f.sb.argvLogText(t), "sudo")
}

// sent returns what the target sent the VM on its limactl shell calls, without the separator the fake limactl
// appends after each call.
func (f *vmAgentsTokenSSHFixture) sent(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(f.capture)
	if err != nil {
		t.Fatalf("read what the target sent the VM: %v", err)
	}

	return strings.TrimSuffix(string(body), "\n--- end of call ---\n")
}

// fakeAccountLimactl stands in for limactl in the vm-agents-token-ssh tests: it records its argv, answers the
// instance's SSH local port for the format the target pins the port with, records what the target sends the VM on
// standard input, and answers as the VM's script does - the host key as host-key=ssh-ed25519 <base64> and the done
// line the target insists on (ADR 0031, update of 2026-11-19, 4). SHIFT2EU_TEST_LIMA_REPLY says what it prints
// instead: uninstall prints the uninstall's done line, silent nothing at all (a connection that dropped after the
// stream arrived), and no-host-key the done line alone (issue #227 part 2, review 5).
// SHIFT2EU_TEST_LIMA_ESCAPE_STREAM (stdout or stderr) makes the VM print a terminal escape sequence there before
// its lines, the way code in the VM can (issue #227 part 2, the security audit's second pass), and
// SHIFT2EU_TEST_LIMA_CONTROL_STREAM (stdout or stderr) makes it print a C1 control and a tab there instead (issue
// #227 part 2, the security audit's third pass). SHIFT2EU_TEST_LIMA_FAIL makes it print that line on stderr and
// exit with SHIFT2EU_TEST_LIMA_STATUS (1 when unset) instead of its done lines, the way a script in the VM reports
// a step it could not do (issue #227 part 2, the closing review).
const fakeAccountLimactl = `#!/bin/sh
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
key="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGxldHNpbnZlbnRhbmV4YW1wbGVrZXlibG9i"
case "${1:-}" in
list)
  case "${3:-}" in
  *SSHLocalPort*)
    cat "${SHIFT2EU_TEST_LIMA_SSH_PORT_FILE:?set SHIFT2EU_TEST_LIMA_SSH_PORT_FILE}" || exit 1
    exit 0
    ;;
  esac
  printf '%s\n' "${SHIFT2EU_TEST_LIMA_LIST_NAME:?set SHIFT2EU_TEST_LIMA_LIST_NAME}"
  exit 0
  ;;
shell) ;;
*)
  echo "fake limactl: the tests answer list and shell only, not $*" >&2
  exit 64
  ;;
esac
copy="$(mktemp "${TMPDIR:-/tmp}/lima-stdin.XXXXXX")" || exit 1
cat > "${copy}" || exit 1
cat "${copy}" >> "${SHIFT2EU_TEST_LIMA_CAPTURE:?set SHIFT2EU_TEST_LIMA_CAPTURE}" || exit 1
printf '\n--- end of call ---\n' >> "${SHIFT2EU_TEST_LIMA_CAPTURE}" || exit 1
last=""
for a in "$@"; do last="$a"; done
if [ "${last}" = -s ]; then
  case "${SHIFT2EU_TEST_LIMA_ESCAPE_STREAM:-}" in
  stdout) printf '\033[2J\n' ;;
  stderr) printf '\033[2J\n' >&2 ;;
  '') ;;
  *)
    echo "fake limactl: unknown SHIFT2EU_TEST_LIMA_ESCAPE_STREAM: ${SHIFT2EU_TEST_LIMA_ESCAPE_STREAM}" >&2
    exit 64
    ;;
  esac
  case "${SHIFT2EU_TEST_LIMA_CONTROL_STREAM:-}" in
  stdout) printf 'vm: before\tafter\233mid\302\233end\n' ;;
  stderr) printf 'vm: before\tafter\233mid\302\233end\n' >&2 ;;
  '') ;;
  *)
    echo "fake limactl: unknown SHIFT2EU_TEST_LIMA_CONTROL_STREAM: ${SHIFT2EU_TEST_LIMA_CONTROL_STREAM}" >&2
    exit 64
    ;;
  esac
  if [ -n "${SHIFT2EU_TEST_LIMA_FAIL:-}" ]; then
    printf '%s\n' "${SHIFT2EU_TEST_LIMA_FAIL}" >&2
    rm -f "${copy}"
    exit "${SHIFT2EU_TEST_LIMA_STATUS:-1}"
  fi
  case "${SHIFT2EU_TEST_LIMA_REPLY:-install}" in
  install)
    printf 'host-key=%s\n' "${key}"
    printf 'vm-agents-token-ssh: done\n'
    ;;
  uninstall) printf 'vm-agents-token-ssh-uninstall: done\n' ;;
  no-host-key) printf 'vm-agents-token-ssh: done\n' ;;
  silent) ;;
  *)
    echo "fake limactl: unknown SHIFT2EU_TEST_LIMA_REPLY: ${SHIFT2EU_TEST_LIMA_REPLY}" >&2
    exit 64
    ;;
  esac
else
  printf '%s\n' "${key}"
fi
rm -f "${copy}"
exit 0
`

// TestMakeVMAgentsTokenSSHPinsThePort pins step 1 of ADR 0031's update (item 4): Lima picks a random SSH port at
// every start unless the instance pins it, so the target refuses unless limactl reports exactly the port the
// environment names, before it reads the key or sends anything to the VM.
func TestMakeVMAgentsTokenSSHPinsThePort(t *testing.T) {
	t.Parallel()

	f := newVMAgentsTokenSSHFixture(t)
	f.setPort(t, "48099")

	stdout, stderr, err := f.run(t, "vm-agents-token-ssh")
	if err == nil {
		t.Fatalf("make vm-agents-token-ssh exited 0 although the VM listens on 48099 and SHIFT2EU_AGENTS_VM_SSH_PORT names %s: the port must be pinned, or the job writes nowhere (ADR 0031, update of 2026-11-19, 3)\nstdout: %s\nstderr: %s",
			sshModePort, stdout, stderr)
	}

	if !strings.Contains(strings.ToLower(stderr), "port") {
		t.Errorf("the refusal does not say the VM's SSH port is what failed, so a maintainer cannot tell what to pin (ADR 0031, update of 2026-11-19, 3)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if calls := f.shellCalls(t); len(calls) > 0 {
		t.Errorf("the refused run sent the VM a script (%v): the port is checked before anything is written (ADR 0031, update of 2026-11-19, 4)", calls[0])
	}

	if pinned := f.pinned(t); pinned != "" {
		t.Errorf("the refused run pinned the host key %q although the port did not match", pinned)
	}
}

// TestMakeVMAgentsTokenSSHRefusesAMalformedPublicKey pins step 2 of ADR 0031's update (item 4): the account's
// public key reaches a root-owned keys file the VM's sshd reads, so the target refuses anything but one line
// `ssh-ed25519 <base64>` with an optional comment of letters, digits and @ . _ -: no options, quotes, commas or
// second line can reach that file.
func TestMakeVMAgentsTokenSSHRefusesAMalformedPublicKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
	}{
		{name: "a key of another type", line: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQ shift2eu-agents-token\n"},
		{name: "no base64 at all", line: "ssh-ed25519\n"},
		{name: "an option before the key", line: `command="/bin/sh" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 shift2eu-agents-token` + "\n"},
		{name: "a quoted comment", line: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 \"two words\"\n"},
		{name: "a comma in the comment", line: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 one,two\n"},
		{name: "a second line", line: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 one\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5 two\n"},
		{name: "no key at all", line: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMAgentsTokenSSHFixture(t)
			f.setPubKey(t, tc.line)

			stdout, stderr, err := f.run(t, "vm-agents-token-ssh")
			if err == nil {
				t.Fatalf("make vm-agents-token-ssh exited 0 with %s in the account's public key: anything but one `ssh-ed25519 <base64>` line is refused, or it reaches the VM's keys file as the sshd reads it (ADR 0031, update of 2026-11-19, 4)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if !strings.Contains(strings.ToLower(stderr), "key") {
				t.Errorf("the refusal of %s does not say the account's key is what failed:\n%s", tc.name, stderr)
			}

			if calls := f.shellCalls(t); len(calls) > 0 {
				t.Errorf("the refused run sent the VM a script (%v): the key is checked before anything is written (ADR 0031, update of 2026-11-19, 4)", calls[0])
			}

			if pinned := f.pinned(t); pinned != "" {
				t.Errorf("the refused run pinned the host key %q although the key line was malformed", pinned)
			}
		})
	}
}

// TestMakeVMAgentsTokenSSHInstallsTheWriter pins the happy path of ADR 0031's update (item 4): the target sends the
// VM one script over `limactl shell --workdir / <instance> sh -s` whose keys line authorises the account's key to
// run the root-owned writer and nothing else, with the comment replaced, requires the VM's
// `vm-agents-token-ssh: done` line, and pins the host key into the account's known-hosts file through the exact
// sudo of the ADR.
func TestMakeVMAgentsTokenSSHInstallsTheWriter(t *testing.T) {
	t.Parallel()

	f := newVMAgentsTokenSSHFixture(t)

	stdout, stderr, err := f.run(t, "vm-agents-token-ssh")
	if err != nil {
		t.Fatalf("make vm-agents-token-ssh: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "vm-agents-token-ssh: done") {
		t.Errorf("make vm-agents-token-ssh does not report the VM's `vm-agents-token-ssh: done` line, so a connection that dropped after the stream arrived reads as success (ADR 0031, update of 2026-11-19, 4):\nstdout: %s", stdout)
	}

	calls := f.shellCalls(t)
	if len(calls) == 0 {
		t.Fatalf("the target sent the VM no script: it installs the writer, the keys file and the sshd drop-in over limactl shell's standard input (ADR 0031, update of 2026-11-19, 4)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	wantArgs := []string{"shell", "--workdir", "/", hostTargetInstance, "sh", "-s"}
	if diff := cmp.Diff(wantArgs, calls[0]); diff != "" {
		t.Errorf("the script does not reach the VM the way make vm-gh-wrapper sends its own (-want +got):\n%s", diff)
	}

	sent, err := os.ReadFile(f.capture)
	if err != nil {
		t.Fatalf("read what the target sent the VM: %v", err)
	}

	// The one line the VM's keys file holds: only the account's key, and only for the writer, with the real gh as
	// its argument (ADR 0031, update of 2026-11-19, 4).
	keysLine := `restrict,command="` + sshWriterPath + " " + sshRealGHTool + `" ` + fakeHostKey + ` shift2eu-agents-token`
	if !strings.Contains(string(sent), keysLine) {
		t.Errorf("the script sent to the VM does not hold the keys line the ADR gives, so the account's key could log in as anything:\nwant it to hold: %s\nsent:\n%s", keysLine, sent)
	}

	if strings.Contains(string(sent), "the-maintainer@example.com") {
		t.Errorf("the script sent to the VM keeps the comment of the account's public key: the comment is replaced with shift2eu-agents-token (ADR 0031, update of 2026-11-19, 4)")
	}

	for _, want := range []string{sshWriterPath, "/etc/ssh/shift2eu-agents-token/", "/etc/ssh/sshd_config.d/50-shift2eu-agents-token.conf", "sshd -t", "vm-agents-token-ssh: done"} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("the script sent to the VM does not hold %q, so the VM misses a step of the install (ADR 0031, update of 2026-11-19, 4):\n%s", want, sent)
		}
	}

	// The pin: the host key under the instance's alias, written as the account through the exact sudo of the ADR,
	// with the line on standard input. The sudo passes no PATH of the maintainer's on: sudoers sets no
	// secure_path on macOS, so the pin's cat and mv are named by path and the environment is emptied first
	// (issue #227 part 2, security audit 1).
	knownHosts := filepath.Join(agentsAccountHome(), ".config", "shift2eu", "known-hosts-"+hostTargetInstance)
	wantPin := []string{
		"-u", agentsAccount, "-H", "/usr/bin/env", "-i", "PATH=/usr/bin:/bin",
		"/bin/sh", "-c",
		`umask 077 && /bin/cat >"$1.new" && /bin/mv -f "$1.new" "$1"`,
		"pin", knownHosts,
	}

	var pin []string

	for _, args := range invocations(f.sb.argvLogText(t), "sudo") {
		if isThePin(args) {
			pin = args

			break
		}
	}

	if pin == nil {
		t.Fatalf("the target pinned no host key: it writes the VM's host key into the account's %s, or every later ssh warns or fails (ADR 0031, update of 2026-11-19, 4)\nevery sudo it ran: %v", knownHosts, invocations(f.sb.argvLogText(t), "sudo"))
	}

	if diff := cmp.Diff(wantPin, pin); diff != "" {
		t.Errorf("the pin is not the sudo the ADR gives, with no PATH of the maintainer's on it (-want +got):\n%s", diff)
	}

	// The account's public key, read as the account: that sudo passes no PATH of the maintainer's on either
	// (issue #227 part 2, security audit 1).
	wantPubKeyRead := []string{
		"-u", agentsAccount, "-H", "/usr/bin/env", "-i", "PATH=/usr/bin:/bin",
		"/bin/cat", filepath.Join(agentsAccountHome(), ".config", "shift2eu", "agents-vm-ssh-key.pub"),
	}

	var pubKeyRead []string

	for _, args := range invocations(f.sb.argvLogText(t), "sudo") {
		if len(args) > 0 && args[len(args)-1] == filepath.Join(agentsAccountHome(), ".config", "shift2eu", "agents-vm-ssh-key.pub") {
			pubKeyRead = args

			break
		}
	}

	if pubKeyRead == nil {
		t.Fatalf("the target never read the account's public key as the account: it reads %s, or it cannot build the keys line (ADR 0031, update of 2026-11-19, 4, step 2)\nevery sudo it ran: %v",
			filepath.Join(agentsAccountHome(), ".config", "shift2eu", "agents-vm-ssh-key.pub"), invocations(f.sb.argvLogText(t), "sudo"))
	}

	if diff := cmp.Diff(wantPubKeyRead, pubKeyRead); diff != "" {
		t.Errorf("the public key read is not the sudo the ADR gives, with no PATH of the maintainer's on it (-want +got):\n%s", diff)
	}

	// The two commands run as the account end with sudo -k, on success and on failure (ADR 0034, update of
	// 2026-11-19, c).
	assertEndsWithSudoK(t, f.sudoCommands(t))

	wantPinned := "shift2eu-vm-" + hostTargetInstance + " " + fakeHostKey + "\n"
	if diff := cmp.Diff(wantPinned, f.pinned(t)); diff != "" {
		t.Errorf("the known-hosts line the target wrote is not the instance's alias and the VM's host key (-want +got):\n%s", diff)
	}

	if strings.Contains(strings.Join(pin, " "), fakeHostKey) {
		t.Errorf("the host key reached the pin's command line: the line goes in on standard input (ADR 0031, update of 2026-11-19, 4)")
	}
}

// TestMakeVMAgentsTokenSSHAcceptsAGoodAgentsAccount pins the other half of the name check (issue #227 part 2,
// security audit 5 and review 7): a name of letters, digits, ., _ and - that begins with a letter or an underscore
// passes it - the role account with its leading underscore as much as a standard account - and the target goes on
// to install the writer and pin the key.
func TestMakeVMAgentsTokenSSHAcceptsAGoodAgentsAccount(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"_shift2euagents", "shift2eu-agents"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newVMAgentsTokenSSHFixture(t)
			f.setAccount(t, name)

			stdout, stderr, err := f.run(t, "vm-agents-token-ssh")
			if err != nil {
				t.Fatalf("make vm-agents-token-ssh refused SHIFT2EU_AGENTS_ACCOUNT=%q, exiting %d: the name holds only letters, digits, ., _ and - and begins with a letter or _, so it is accepted and the target installs the writer and pins the key (issue #227 part 2, security audit 5)\nstdout: %s\nstderr: %s",
					name, exitStatusOf(t, err), stdout, stderr)
			}

			if !strings.Contains(stdout, "vm-agents-token-ssh: done") {
				t.Errorf("make vm-agents-token-ssh with SHIFT2EU_AGENTS_ACCOUNT=%q does not report the VM's done line:\nstdout: %s", name, stdout)
			}

			if pinned := f.pinned(t); pinned == "" {
				t.Errorf("make vm-agents-token-ssh with SHIFT2EU_AGENTS_ACCOUNT=%q pinned no host key in /Users/%s/.config/shift2eu: an accepted name reaches every step of the target", name, name)
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallChecksTheAppIDsSeparately pins review 8 of issue #227 part 2: in account mode
// the install checks SHIFT2EU_AGENTS_APP_ID and SHIFT2EU_AGENTS_APP_INSTALLATION_ID one by one, so its refusal
// names the variable the maintainer has to fix and never the other one, before it runs any sudo.
func TestMakeAgentsTokenRefreshInstallChecksTheAppIDsSeparately(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		appID          string
		installationID string
		broken         string
		fine           string
	}{
		{
			name:           "the App ID holds a colon",
			appID:          "1:2",
			installationID: "3",
			broken:         "SHIFT2EU_AGENTS_APP_ID",
			fine:           "SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
		},
		{
			name:           "the installation ID holds a colon",
			appID:          "3",
			installationID: "4:x",
			broken:         "SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
			fine:           "SHIFT2EU_AGENTS_APP_ID",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, hostTargetInstance)
			accountEnv(f.env)
			f.env["SHIFT2EU_AGENTS_APP_ID"] = tc.appID
			f.env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = tc.installationID

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 with %s=%q, which is no number: both IDs are checked one by one in account mode (issue #227 part 2, review 8)\nstdout: %s\nstderr: %s",
					tc.broken, tc.appID+"/"+tc.installationID, stdout, stderr)
			}

			if !strings.Contains(stderr, tc.broken) {
				t.Errorf("the refusal does not name %s, the variable that has to be fixed, so a maintainer cannot tell which one is wrong (issue #227 part 2, review 8):\nstderr: %s", tc.broken, stderr)
			}

			if strings.Contains(stderr, tc.fine) {
				t.Errorf("the refusal of a bad %s also names %s, which is fine: the two IDs are checked separately, so the message names only the one to fix (issue #227 part 2, review 8):\nstderr: %s",
					tc.broken, tc.fine, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) > 0 {
				t.Errorf("the install ran sudo %v although an App ID was refused: the check comes before anything runs as the account (issue #227 part 2, review 8)", calls[0])
			}

			if strings.Contains(stdout, "+ sudo") {
				t.Errorf("the install printed a sudo command although it refused an App ID:\nstdout: %s", stdout)
			}

			if _, err := os.Stat(f.dest()); err == nil {
				t.Errorf("%s is there although the install refused an App ID", f.dest())
			}
		})
	}
}

// TestMakeVMAgentsTokenSSHRefusesAnIncompleteVMAnswer pins review 5 of issue #227 part 2: make vm-agents-token-ssh
// fails when the VM never prints its done line (the installation may not have finished) and when it prints no
// host-key line (there is nothing to pin), pins nothing in either case, and still ends with sudo -k.
func TestMakeVMAgentsTokenSSHRefusesAnIncompleteVMAnswer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{name: "the VM prints nothing", reply: "silent", want: "vm-agents-token-ssh: done"},
		{name: "the VM prints no host-key line", reply: "no-host-key", want: "host-key"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMAgentsTokenSSHFixture(t)
			f.setReply(t, tc.reply)

			stdout, stderr, err := f.run(t, "vm-agents-token-ssh")
			if err == nil {
				t.Fatalf("make vm-agents-token-ssh exited 0 although %s: the target requires the VM's done line and a host-key line before it pins anything (ADR 0031, update of 2026-11-19, 4, steps 3 and 4)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if !strings.Contains(stderr, tc.want) {
				t.Errorf("%s, and the failure does not say %q, so a maintainer cannot tell what the VM did not answer (issue #227 part 2, review 5):\nstderr: %s", tc.name, tc.want, stderr)
			}

			if pinned := f.pinned(t); pinned != "" {
				t.Errorf("%s, and the target pinned %q anyway: a host key that was never read is pinned nowhere", tc.name, pinned)
			}

			for _, args := range f.sudoCommands(t) {
				if isThePin(args) {
					t.Errorf("%s, and the target ran the pin %v anyway (issue #227 part 2, review 5)", tc.name, args)
				}
			}

			assertEndsWithSudoK(t, f.sudoCommands(t))
		})
	}
}

// TestMakeVMAgentsTokenSSHUninstall pins review 5 of issue #227 part 2: make vm-agents-token-ssh-uninstall sends the
// VM exactly the script `sh dev/vm-agents-token-ssh.sh --uninstall <vm user>` prints, whose last line is the call
// that runs it, requires the VM's own done line, and runs no sudo on the Mac - the account's files stay as they
// are (ADR 0031, update of 2026-11-19, 4).
func TestMakeVMAgentsTokenSSHUninstall(t *testing.T) {
	t.Parallel()

	f := newVMAgentsTokenSSHFixture(t)
	f.setReply(t, "uninstall")

	wantScript, stderr, err := f.sb.run(t, scriptPath(t, vmAgentsTokenSSHScript), []string{"--uninstall", sshModeUser}, f.env, "")
	if err != nil {
		t.Fatalf("sh %s --uninstall %s exited %d: %v\nstdout: %s\nstderr: %s", vmAgentsTokenSSHScript, sshModeUser, exitStatusOf(t, err), err, wantScript, stderr)
	}

	stdout, stderr, err := f.run(t, "vm-agents-token-ssh-uninstall")
	if err != nil {
		t.Fatalf("make vm-agents-token-ssh-uninstall: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "vm-agents-token-ssh-uninstall: done") {
		t.Errorf("make vm-agents-token-ssh-uninstall does not report the VM's `vm-agents-token-ssh-uninstall: done` line, so a removal that did not finish reads as success (ADR 0031, update of 2026-11-19, 4):\nstdout: %s", stdout)
	}

	if lastLine(wantScript) != "vm_agents_token_ssh_uninstall" {
		t.Fatalf("the generator's script for the uninstall does not end with the call vm_agents_token_ssh_uninstall, so the target's whole-script check cannot recognise it:\n%s", wantScript)
	}

	calls := f.shellCalls(t)
	if len(calls) == 0 {
		t.Fatalf("the uninstall sent the VM no script: it removes the writer, the keys file and the drop-in over limactl shell's standard input (ADR 0031, update of 2026-11-19, 4)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if got := f.sent(t); got != wantScript {
		t.Errorf("the uninstall did not send the whole script the generator printed for `--uninstall %s` (-want +got):\n%s", sshModeUser, cmp.Diff(wantScript, got))
	}

	if sudo := f.sudoCommands(t); len(sudo) != 0 {
		t.Errorf("the uninstall ran sudo %v on the Mac: it removes the account's writer, keys file and drop-in in the VM and leaves the account's files alone (ADR 0031, update of 2026-11-19, 4)", sudo[0])
	}
}

// TestMakeVMAgentsTokenSSHUninstallRefusesAPartialScript pins the uninstall's whole-script check (issue #227 part
// 2, review 5): a generator that prints no call on its last line, or that fails, stops make before it hands
// anything to the VM.
func TestMakeVMAgentsTokenSSHUninstallRefusesAPartialScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		generator string
		want      string
	}{
		{
			name:      "the script ends without the call",
			generator: "#!/bin/sh\nprintf 'set -eu\\necho removed nothing\\n'\n",
			want:      "vm-agents-token-ssh-uninstall",
		},
		{
			name:      "the generator fails",
			generator: "#!/bin/sh\necho 'vm-agents-token-ssh: the VM user is not there' >&2\nexit 1\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			clone := writeClone(t, filepath.Join(sb.root, "clone"))
			writeFileMode(t, filepath.Join(clone, vmAgentsTokenSSHScript), []byte(tc.generator), 0o755)

			env := sb.baseEnv()
			env["SHIFT2EU_AGENTS_VM_SSH_USER"] = sshModeUser

			stdout, stderr, err := runMakeIn(t, clone, []string{"vm-agents-token-ssh-uninstall"}, env)
			if err == nil {
				t.Fatalf("make vm-agents-token-ssh-uninstall exited 0 although %s: the target sends the VM a script only when its last line is the call that runs it (issue #227 part 2, review 5)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if calls := invocations(sb.argvLogText(t), "limactl"); len(calls) > 0 {
				t.Errorf("the refused uninstall still sent the VM a script (%v): the whole script is checked before anything travels (issue #227 part 2, review 5)", calls[0])
			}

			if tc.want != "" && !strings.Contains(stderr, tc.want) {
				t.Errorf("the refusal does not name %q, so a maintainer cannot tell that the script never arrived whole:\nstderr: %s", tc.want, stderr)
			}
		})
	}
}

// isThePin reports whether args is the target's call of the known-hosts pin: the pin's command line ends with the
// account's known-hosts file, so `pin` is its second-to-last argument, not its last (issue #227 part 2, review 2).
func isThePin(args []string) bool {
	return len(args) > 2 && args[len(args)-2] == "pin"
}

// vmScriptRun is one sandbox run of the script make vm-agents-token-ssh sends the VM (issue #227 part 2, review 2):
// the generator prints it from a writer the test wrote, and the VM user runs it with a sudo that redirects root's
// paths into the sandbox, a fake sshd that answers with the effective configuration the test wrote, and the
// recording systemctl.
type vmScriptRun struct {
	sb  *sandbox
	env map[string]string
}

// newVMScriptRun builds the sandbox for one run of the VM's script: the redirecting fake sudo, the fake sshd with
// effective as the configuration it reads back, the recording systemctl, and a writer file of the test's own.
func newVMScriptRun(t *testing.T, effective string) *vmScriptRun {
	t.Helper()

	sb := newSandbox(t)
	env := sb.baseEnv()

	sb.useFakeVMSudo(t)
	sb.useFakeSSHD(t, env, effective)
	sb.useFakeSystemctl(t)

	writeFileMode(t, filepath.Join(sb.root, "writer"), []byte("#!/bin/sh\nprintf 'the writer the test wrote\\n'\n"), 0o700)

	// The VM has /etc/ssh/sshd_config.d already (Ubuntu ships it); the script writes its drop-in there.
	if err := os.MkdirAll(filepath.Join(sb.root, "etc", "ssh", "sshd_config.d"), 0o700); err != nil {
		t.Fatalf("create the VM's sshd_config.d in the sandbox: %v", err)
	}

	return &vmScriptRun{sb: sb, env: env}
}

// generated runs the generator the target runs and returns the script it prints, failing the test when it refuses
// the writer or the key line.
func (r *vmScriptRun) generated(t *testing.T) string {
	t.Helper()

	stdout, stderr, err := r.sb.run(t, scriptPath(t, vmAgentsTokenSSHScript),
		[]string{filepath.Join(r.sb.root, "writer"), fakeVMPubKeyLine, sshModeUser}, r.env, "")
	if err != nil {
		t.Fatalf("sh %s refused the writer and the key line the test gave it, exiting %d: %v\nstdout: %s\nstderr: %s",
			vmAgentsTokenSSHScript, exitStatusOf(t, err), err, stdout, stderr)
	}

	return stdout
}

// root is a path the VM's script writes through sudo, redirected into the sandbox by the fake sudo.
func (r *vmScriptRun) root(rel string) string {
	return filepath.Join(r.sb.root, filepath.FromSlash(rel))
}

// TestVMAgentsTokenSSHScriptChecksTheAuthorizedKeysFile pins review 2 of issue #227 part 2: once sshd -t accepts the
// configuration and sshd was reloaded, the VM's script asks sshd what it reads back for the VM user
// (sshd -T -C user=<vm user>,host=localhost,addr=127.0.0.1) and fails - removing the drop-in again, reloading sshd
// once more and printing no done line - unless the effective configuration's authorizedkeysfile names the account's
// keys file, so a setup that never loads it cannot look installed.
func TestVMAgentsTokenSSHScriptChecksTheAuthorizedKeysFile(t *testing.T) {
	t.Parallel()

	const (
		noAccountKeys = "authorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2\n"
		accountKeys   = "authorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2 /etc/ssh/shift2eu-agents-token/%u.keys\n"
	)

	dropin := "etc/ssh/sshd_config.d/50-shift2eu-agents-token.conf"
	keysFile := "etc/ssh/shift2eu-agents-token/" + sshModeUser + ".keys"
	checkArg := "user=" + sshModeUser + ",host=localhost,addr=127.0.0.1"

	cases := []struct {
		name      string
		effective string
		accepted  bool
	}{
		{name: "sshd reads the account's keys file the drop-in names", effective: accountKeys, accepted: true},
		{name: "sshd reads no keys file of the account", effective: noAccountKeys},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newVMScriptRun(t, tc.effective)

			stdout, stderr, err := r.sb.runStream(t, r.env, r.generated(t))

			wantChecks := [][]string{{"-t"}, {"-T", "-C", checkArg}}
			if diff := cmp.Diff(wantChecks, invocations(r.sb.argvLogText(t), "sshd")); diff != "" {
				t.Errorf("the script's sshd calls are not `sshd -t` and then `sshd -T -C %s` (-want +got):\n%s\nstdout: %s\nstderr: %s",
					checkArg, diff, stdout, stderr)
			}

			reloads := invocations(r.sb.argvLogText(t), "systemctl")

			if tc.accepted {
				if err != nil {
					t.Fatalf("the script refused a configuration sshd reads the account's keys file from, exiting %d: %v\nstdout: %s\nstderr: %s",
						exitStatusOf(t, err), err, stdout, stderr)
				}

				if !strings.Contains(stdout, "vm-agents-token-ssh: done") {
					t.Errorf("the script does not print `vm-agents-token-ssh: done` although sshd reads the account's keys file, so make believes no installation finished:\nstdout: %s", stdout)
				}

				if len(reloads) != 1 {
					t.Errorf("the script reloaded sshd %d times, want once after sshd -t passed: %v", len(reloads), reloads)
				}

				for _, path := range []string{r.root(dropin), r.root(keysFile)} {
					if _, err := os.Stat(path); err != nil {
						t.Errorf("the accepted configuration left no %s: %v", path, err)
					}
				}

				return
			}

			if err == nil {
				t.Fatalf("the script exited 0 although sshd reads no keys file of the account, so the account's key could not write the token and the installation has not happened (issue #227 part 2, review 2)\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if strings.Contains(stdout, "vm-agents-token-ssh: done") {
				t.Errorf("the script prints `vm-agents-token-ssh: done` although the account's keys file is not what sshd reads (issue #227 part 2, review 2):\nstdout: %s", stdout)
			}

			if !strings.Contains(strings.ToLower(stdout+stderr), "authorizedkeysfile") {
				t.Errorf("the failure does not say what sshd reads for authorizedkeysfile, so a maintainer cannot tell what to fix (issue #227 part 2, review 2):\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if _, err := os.Stat(r.root(dropin)); err == nil {
				t.Errorf("%s is still there although sshd -T did not read the account's keys file through it: the drop-in is removed again (issue #227 part 2, review 2)", r.root(dropin))
			}

			if len(reloads) != 2 {
				t.Fatalf("the script reloaded sshd %d times, want once after sshd -t and once again after the drop-in was removed: %v", len(reloads), reloads)
			}

			if diff := cmp.Diff(reloads[0], reloads[1]); diff != "" {
				t.Errorf("the second reload is not the command that reloaded sshd before (-want +got):\n%s", diff)
			}
		})
	}
}

// TestVMAgentsTokenSSHGeneratorRefusesABadWriterFile pins the writer checks of the generator (issue #227 part 2,
// review 5): the writer travels in a here-document, so it must start with #!/bin/sh and may not hold the line that
// ends that here-document. Either way the generator refuses before it prints anything.
func TestVMAgentsTokenSSHGeneratorRefusesABadWriterFile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		writer string
		want   string
	}{
		{
			name:   "the writer holds the here-document's delimiter line",
			writer: "#!/bin/sh\nSHIFT2EU_AGENTS_TOKEN_WRITE\n",
			want:   "SHIFT2EU_AGENTS_TOKEN_WRITE",
		},
		{
			name:   "the writer does not start with #!/bin/sh",
			writer: "printf 'not a script\\n'\n",
			want:   "#!/bin/sh",
		},
		{
			name:   "the writer names another shell",
			writer: "#!/bin/bash\necho hi\n",
			want:   "#!/bin/sh",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			writer := filepath.Join(sb.root, "writer")
			writeFileMode(t, writer, []byte(tc.writer), 0o700)

			stdout, stderr, err := sb.run(t, scriptPath(t, vmAgentsTokenSSHScript), []string{writer, fakeVMPubKeyLine, sshModeUser}, sb.baseEnv(), "")
			if err == nil {
				t.Fatalf("sh %s printed a script for %s: a writer that is no #!/bin/sh script, or that ends the here-document it travels in, is refused before anything is printed (issue #227 part 2, review 5)\nstdout: %s",
					vmAgentsTokenSSHScript, tc.name, stdout)
			}

			if strings.TrimSpace(stdout) != "" {
				t.Errorf("the generator printed a script although it refused %s:\nstdout: %s", tc.name, stdout)
			}

			if !strings.Contains(stderr, tc.want) {
				t.Errorf("the refusal of %s does not say %q, so a maintainer cannot tell what to fix (issue #227 part 2, review 5):\nstderr: %s", tc.name, tc.want, stderr)
			}
		})
	}
}

// assertAccountKindRefused fails unless the refusal of run names one of checks - the check of the account's kind
// that failed - and SHIFT2EU_AGENTS_ACCOUNT, the variable that holds the account, so a maintainer can tell what
// to fix (issue #227 part 2, the security audit's second pass).
func assertAccountKindRefused(t *testing.T, run string, checks []string, stdout, stderr string) {
	t.Helper()

	assertAccountKindNamed(t, run, "refusal", checks, stdout, stderr)
}

// assertAccountKindWarned fails unless the run warned about the account's kind on stderr and named one of checks
// and SHIFT2EU_AGENTS_ACCOUNT: the uninstall goes on and boots the job out, but a maintainer must learn that the
// account is no hidden role account (issue #227 part 2, the review of the account check).
func assertAccountKindWarned(t *testing.T, run string, checks []string, stdout, stderr string) {
	t.Helper()

	if !strings.Contains(strings.ToLower(stderr), "warning") {
		t.Errorf("%s printed no warning about the account's kind, so a maintainer never learns that the account is no hidden role account and the App's key may be in their reach (issue #227 part 2, the review of the account check)\nstdout: %s\nstderr: %s",
			run, stdout, stderr)
	}

	assertAccountKindNamed(t, run, "warning", checks, stdout, stderr)
}

// assertAccountKindNamed fails unless stderr holds the refusal or the warning of run and names one of checks and
// SHIFT2EU_AGENTS_ACCOUNT, the variable that holds the account (issue #227 part 2, the security audit's second
// pass).
func assertAccountKindNamed(t *testing.T, run, what string, checks []string, stdout, stderr string) {
	t.Helper()

	if !mentionsOneOf(stderr, checks...) {
		t.Errorf("%s's %s says none of %v, so a maintainer cannot tell which check of the account's kind failed (issue #227 part 2, the security audit's second pass)\nstdout: %s\nstderr: %s",
			run, what, checks, stdout, stderr)
	}

	if !strings.Contains(stderr, "SHIFT2EU_AGENTS_ACCOUNT") {
		t.Errorf("%s's %s does not name SHIFT2EU_AGENTS_ACCOUNT, so a maintainer cannot tell which variable to fix (issue #227 part 2, the security audit's second pass)\nstdout: %s\nstderr: %s",
			run, what, stdout, stderr)
	}
}

// assertBootoutRan fails unless the run booted the fixture's job out of launchd through sudo: the uninstall warns
// about the account's kind and boots the job out anyway, or the job keeps running for an account the check refused
// (issue #227 part 2, the review of the account check).
func assertBootoutRan(t *testing.T, f *launchdInstallFixture) {
	t.Helper()

	want := []string{"launchctl", "bootout", "system/" + f.label()}

	for _, args := range f.sudoCommands(t) {
		if cmp.Equal(want, args) {
			return
		}
	}

	t.Errorf("make agents-token-refresh-uninstall ran no `sudo %s`: it warns about the account's kind and then boots the job out, or the job of that account keeps running (issue #227 part 2, the review of the account check)\nevery sudo it ran: %v",
		strings.Join(want, " "), f.sudoCommands(t))
}

// assertNoSudo fails when the sandbox recorded a sudo call: the account's kind is checked before anything runs as
// root, so no recipe reaches sudo for an account that is no role account (issue #227 part 2, the security audit's
// second pass).
func assertNoSudo(t *testing.T, sb *sandbox) {
	t.Helper()

	if calls := invocations(sb.argvLogText(t), "sudo"); len(calls) > 0 {
		t.Errorf("the run called sudo %v although it refused the account's kind: the check comes before any sudo call, so nothing runs as root for an account that is no hidden role account (issue #227 part 2, the security audit's second pass)",
			calls[0])
	}
}

// kindCheckCase is one answer of the fake directory tools to the account check: what they say the account's kind
// is, and the checks the refusal must name. A case with no checks is one the check accepts (issue #227 part 2,
// the security audit's second pass).
type kindCheckCase struct {
	name   string
	env    map[string]string // what the fake dscl and dseditgroup answer
	checks []string          // one of them must appear in the refusal; empty: the account passes the check
}

// kindCheckCases are the account kinds the check must refuse, and the hidden role account it must accept:
// macOS gives a login account a UniqueID of 500 and above, and the role account of docs/dev-environment.md one
// below 500, hidden from the login window and no admin. The admin check fails closed: only dseditgroup's explicit
// no - its text, beginning "no ", with its non-zero status - passes, so an answer that is missing, an error, or
// the tool itself missing refuses the account (issue #227 part 2, the review of the account check). An answer that
// merely holds "no " somewhere does not pass either: the pattern is anchored, so a status 1 "error:no record" and
// a status 67 "x:no y" both refuse (issue #227 part 2, the closing review). The UniqueID edges guard the
// parser: 0 and 0450 keep a login account out of the role range, 499 is the last role UniqueID, 500 the first
// login one, 1000 a login account, and an empty or non-numeric answer is no UniqueID at all (issue #227 part 2,
// the audit of the account check).
func kindCheckCases() []kindCheckCase {
	return []kindCheckCase{
		{name: "a login account, UniqueID 501", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": "501"}, checks: []string{"UniqueID"}},
		{name: "an account that is an admin", env: map[string]string{"SHIFT2EU_TEST_DS_ADMIN": "1"}, checks: []string{"admin"}},
		{name: "an account dscl cannot read", env: map[string]string{"SHIFT2EU_TEST_DS_STATUS": "1"}, checks: []string{"UniqueID", "dscl"}},
		{name: "an admin check that exits 64 and answers nothing", env: map[string]string{"SHIFT2EU_TEST_DS_ADMIN": "error"}, checks: []string{"admin", "dseditgroup"}},
		{name: "no dseditgroup to run the admin check", env: map[string]string{"SHIFT2EU_TEST_DS_ADMIN": "missing"}, checks: []string{"admin", "dseditgroup"}},
		{
			name:   "an admin check whose answer holds :no but does not begin with no",
			env:    map[string]string{"SHIFT2EU_TEST_DS_ADMIN_ANSWER": "error:no record", "SHIFT2EU_TEST_DS_ADMIN_STATUS": "1"},
			checks: []string{"admin", "dseditgroup"},
		},
		{
			name:   "an admin check whose answer begins x:no y",
			env:    map[string]string{"SHIFT2EU_TEST_DS_ADMIN_ANSWER": "x:no y", "SHIFT2EU_TEST_DS_ADMIN_STATUS": "67"},
			checks: []string{"admin", "dseditgroup"},
		},
		{name: "a UniqueID of 0", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": "0"}, checks: []string{"UniqueID"}},
		{name: "a UniqueID of 0450", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": "0450"}, checks: []string{"UniqueID"}},
		{name: "a UniqueID of 500, the first login account", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": "500"}, checks: []string{"UniqueID"}},
		{name: "a UniqueID of 1000", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": "1000"}, checks: []string{"UniqueID"}},
		{name: "an empty UniqueID", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": ""}, checks: []string{"UniqueID"}},
		{name: "a UniqueID that is no number", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": "four-hundred-and-one"}, checks: []string{"UniqueID"}},
		{name: "a hidden role account", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": "401"}},
		{name: "a UniqueID of 499, the last role account", env: map[string]string{"SHIFT2EU_TEST_DS_UNIQUEID": "499"}},
		{name: "an explicit no of the admin check", env: map[string]string{"SHIFT2EU_TEST_DS_ADMIN": "no"}},
	}
}

// TestMakeTokenTargetsRefuseAnAgentsAccountThatIsNoRoleAccount pins the account's kind for the minting targets
// (issue #227 part 2, the security audit's second pass): the App's key sits in SHIFT2EU_AGENTS_ACCOUNT precisely
// because the maintainer's own uid cannot read it, so an account that is a login account (UniqueID 500 and
// above) or an admin puts the key back in silent reach, and the targets that run AGENTS_ACCOUNT_CHECK refuse it.
// The three minting targets refuse as well, before the refusal that tells the maintainer to mint through
// launchctl: there is no job for an account that is no role account.
func TestMakeTokenTargetsRefuseAnAgentsAccountThatIsNoRoleAccount(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"agents-token", "agents-token-refresh", "agents-token-refresh-start"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, hostTargetInstance)
			accountEnv(f.env)
			// 501 is the first UniqueID macOS gives a login account.
			f.env["SHIFT2EU_TEST_DS_UNIQUEID"] = "501"

			stdout, stderr, err := runMakeForRefusal(t, target, f.env)
			if err == nil {
				t.Fatalf("make %s exited 0 with SHIFT2EU_AGENTS_ACCOUNT naming an account whose UniqueID is 501: a login account is no hidden role account, and the maintainer can read the App's key from it, so the targets that run AGENTS_ACCOUNT_CHECK refuse it (issue #227 part 2, the security audit's second pass)\nstdout: %s\nstderr: %s",
					target, stdout, stderr)
			}

			assertAccountKindRefused(t, "make "+target, []string{"UniqueID"}, stdout, stderr)
			assertNoSudo(t, f.sb)
		})
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesAnAgentsAccountThatIsNoRoleAccount pins the account's kind for the two
// targets that load and remove the job (issue #227 part 2, the security audit's second pass and the review of the
// account check). The install refuses, before it runs a single sudo, an account whose UniqueID is out of the role
// range, one that is an admin, one dscl cannot read and one whose admin check cannot answer, naming the check and
// SHIFT2EU_AGENTS_ACCOUNT. The uninstall is the kill switch: it keeps the check of the account's name
// (TestMakeTokenTargetsRefuseABadAgentsAccount), but a kind check that fails only makes it warn, naming the check,
// and boot the job out anyway, so a job that is loaded for such an account can be removed without first fixing the
// account.
func TestMakeAgentsTokenRefreshInstallRefusesAnAgentsAccountThatIsNoRoleAccount(t *testing.T) {
	t.Parallel()

	for _, tc := range kindCheckCases() {
		if tc.checks == nil {
			continue
		}

		t.Run(tc.name+", the install refuses", func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, hostTargetInstance)
			accountEnv(f.env)

			for name, value := range tc.env {
				f.env[name] = value
			}

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 with SHIFT2EU_AGENTS_ACCOUNT naming %s: the App's key is the account's because the maintainer's uid cannot read it, and an account that is no hidden role account hands it back, so the target refuses before it runs any sudo (issue #227 part 2, the security audit's second pass)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			assertAccountKindRefused(t, "make agents-token-refresh-install", tc.checks, stdout, stderr)
			assertNoSudo(t, f.sb)
		})

		t.Run(tc.name+", the uninstall warns and boots the job out", func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, hostTargetInstance)
			accountEnv(f.env)

			for name, value := range tc.env {
				f.env[name] = value
			}

			// The plist a loaded job was installed from, so the uninstall removes a file as it does on the Mac.
			writeFileMode(t, f.dest(), []byte("a plist an earlier install left\n"), 0o644)

			stdout, stderr, err := f.run(t, "agents-token-refresh-uninstall")
			if err != nil {
				t.Fatalf("make agents-token-refresh-uninstall exited %d with SHIFT2EU_AGENTS_ACCOUNT naming %s: the uninstall is the kill switch, so it warns about the account's kind and boots the job out anyway, or the job of an account that is no role account cannot be removed before the account is fixed (issue #227 part 2, the review of the account check)\nstdout: %s\nstderr: %s",
					exitStatusOf(t, err), tc.name, stdout, stderr)
			}

			assertAccountKindWarned(t, "make agents-token-refresh-uninstall", tc.checks, stdout, stderr)
			assertBootoutRan(t, f)
			assertEndsWithSudoK(t, f.sudoCommands(t))

			if _, err := os.Stat(f.dest()); err == nil {
				t.Errorf("%s is still there after the uninstall, so the next boot loads the job again", f.dest())
			}
		})
	}
}

// TestMakeVMAgentsTokenSSHRefusesAnAgentsAccountThatIsNoRoleAccount pins the account's kind for the target that
// lets the account's key write into the VM (issue #227 part 2, the security audit's second pass): it refuses an
// account whose UniqueID is 501, one that is an admin, and one dscl cannot read, naming the check and
// SHIFT2EU_AGENTS_ACCOUNT, before it runs any sudo or sends the VM anything; the hidden role account of
// docs/dev-environment.md passes and the writer is installed.
func TestMakeVMAgentsTokenSSHRefusesAnAgentsAccountThatIsNoRoleAccount(t *testing.T) {
	t.Parallel()

	for _, tc := range kindCheckCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMAgentsTokenSSHFixture(t)

			for name, value := range tc.env {
				f.env[name] = value
			}

			stdout, stderr, err := f.run(t, "vm-agents-token-ssh")

			if tc.checks == nil {
				if err != nil {
					t.Fatalf("make vm-agents-token-ssh refused %s, exiting %d: the hidden role account of docs/dev-environment.md (a UniqueID below 500, no admin membership) is the account the App's key belongs to, so the target installs the writer and pins the key (issue #227 part 2, the security audit's second pass)\nstdout: %s\nstderr: %s",
						tc.name, exitStatusOf(t, err), stdout, stderr)
				}

				if pinned := f.pinned(t); pinned == "" {
					t.Errorf("make vm-agents-token-ssh accepted the hidden role account but pinned no host key: an account the check passes reaches every step of the target")
				}

				return
			}

			if err == nil {
				t.Fatalf("make vm-agents-token-ssh exited 0 with SHIFT2EU_AGENTS_ACCOUNT naming %s: an account that is no hidden role account hands the App's key back to the maintainer, so the target refuses before it runs any sudo (issue #227 part 2, the security audit's second pass)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			assertAccountKindRefused(t, "make vm-agents-token-ssh", tc.checks, stdout, stderr)
			assertNoSudo(t, f.sb)

			if calls := f.shellCalls(t); len(calls) > 0 {
				t.Errorf("the run sent the VM a script (%v) although it refused the account's kind: the check comes before anything is sent, or the VM's keys file would name a key an account the maintainer can read holds (issue #227 part 2, the security audit's second pass)",
					calls[0])
			}
		})
	}
}

// TestMakeVMAgentsTokenSSHEchoesNoEscapeFromTheVM pins the second and third pass of the security audit on what the
// VM's own code prints (issue #227 part 2): the script that runs in the VM is written by the agents, so a line it
// prints may carry a terminal escape sequence or a C1 control, on either stream, and make must strip it before it
// echoes the VM's output. The VM's done line is still printed, so nothing the maintainer needs is lost, and a tab
// and a line break of the VM's line survive.
func TestMakeVMAgentsTokenSSHEchoesNoEscapeFromTheVM(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		target string
		reply  string
		env    map[string]string
		// control is whether the VM writes a C1 control beside printable text, so the tab and the line break of
		// that line must survive the stripping.
		control bool
	}{
		{name: "the install, a terminal escape on stdout", target: "vm-agents-token-ssh", reply: "install", env: map[string]string{"SHIFT2EU_TEST_LIMA_ESCAPE_STREAM": "stdout"}},
		{name: "the install, a terminal escape on stderr", target: "vm-agents-token-ssh", reply: "install", env: map[string]string{"SHIFT2EU_TEST_LIMA_ESCAPE_STREAM": "stderr"}},
		{name: "the uninstall, a terminal escape on stdout", target: "vm-agents-token-ssh-uninstall", reply: "uninstall", env: map[string]string{"SHIFT2EU_TEST_LIMA_ESCAPE_STREAM": "stdout"}},
		{name: "the uninstall, a terminal escape on stderr", target: "vm-agents-token-ssh-uninstall", reply: "uninstall", env: map[string]string{"SHIFT2EU_TEST_LIMA_ESCAPE_STREAM": "stderr"}},
		{name: "the install, a C1 control on stdout", target: "vm-agents-token-ssh", reply: "install", env: map[string]string{"SHIFT2EU_TEST_LIMA_CONTROL_STREAM": "stdout"}, control: true},
		{name: "the install, a C1 control on stderr", target: "vm-agents-token-ssh", reply: "install", env: map[string]string{"SHIFT2EU_TEST_LIMA_CONTROL_STREAM": "stderr"}, control: true},
		{name: "the uninstall, a C1 control on stdout", target: "vm-agents-token-ssh-uninstall", reply: "uninstall", env: map[string]string{"SHIFT2EU_TEST_LIMA_CONTROL_STREAM": "stdout"}, control: true},
		{name: "the uninstall, a C1 control on stderr", target: "vm-agents-token-ssh-uninstall", reply: "uninstall", env: map[string]string{"SHIFT2EU_TEST_LIMA_CONTROL_STREAM": "stderr"}, control: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMAgentsTokenSSHFixture(t)
			f.setReply(t, tc.reply)

			for name, value := range tc.env {
				f.env[name] = value
			}

			stdout, stderr, err := f.run(t, tc.target)
			if err != nil {
				t.Fatalf("make %s exited %d although the VM printed its done line beside %s: it is stripped and the rest of the line is read as before (issue #227 part 2, the security audit's second and third pass)\nstdout: %s\nstderr: %s",
					tc.target, exitStatusOf(t, err), tc.name, stdout, stderr)
			}

			if !strings.Contains(stdout, tc.target+": done") {
				t.Errorf("make %s printed no %q line although the VM printed it: %s is stripped and the rest of the line kept (issue #227 part 2, the security audit's second and third pass)\nstdout: %s\nstderr: %s",
					tc.target, tc.target+": done", tc.name, stdout, stderr)
			}

			assertNoEscape(t, "make "+tc.target, stdout, stderr)
			assertNoC1Control(t, "make "+tc.target, stdout, stderr)

			if tc.control {
				assertTabAndNewlineSurvive(t, "make "+tc.target, stdout, stderr)
			}
		})
	}
}

// TestMakeVMAgentsTokenSSHSendsTheVMsFailureToStderr pins where the VM's output goes (issue #227 part 2, the
// closing review): the recipes of vm-agents-token-ssh and vm-agents-token-ssh-uninstall capture the VM's stdout and
// stderr together and echo the filtered result, so a VM that fails after printing an error line must leave that
// line on make's stderr - a failure a maintainer pipes away with make's stdout is a failure they never see - and
// make must exit non-zero. A VM that succeeds still prints its done line on make's stdout, as before.
func TestMakeVMAgentsTokenSSHSendsTheVMsFailureToStderr(t *testing.T) {
	t.Parallel()

	// The VM's own error line, behind a terminal escape sequence the recipe must strip wherever it prints it.
	const failure = "vm: could not write the keys file"

	cases := []struct {
		name   string
		target string
		reply  string // what the fake VM answers when it succeeds
		fail   string // what it prints before it exits non-zero; empty: the run succeeds
	}{
		{name: "the install, a VM that fails", target: "vm-agents-token-ssh", reply: "install", fail: "\x1b[2J" + failure},
		{name: "the uninstall, a VM that fails", target: "vm-agents-token-ssh-uninstall", reply: "uninstall", fail: "\x1b[2J" + failure},
		{name: "the install, a VM that succeeds", target: "vm-agents-token-ssh", reply: "install"},
		{name: "the uninstall, a VM that succeeds", target: "vm-agents-token-ssh-uninstall", reply: "uninstall"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMAgentsTokenSSHFixture(t)
			f.setReply(t, tc.reply)

			if tc.fail != "" {
				f.env["SHIFT2EU_TEST_LIMA_FAIL"] = tc.fail
				f.env["SHIFT2EU_TEST_LIMA_STATUS"] = "1"
			}

			stdout, stderr, err := f.run(t, tc.target)

			if tc.fail == "" {
				if err != nil {
					t.Fatalf("make %s exited %d although the VM printed its done line:\nstdout: %s\nstderr: %s",
						tc.target, exitStatusOf(t, err), stdout, stderr)
				}

				if !strings.Contains(stdout, tc.target+": done") {
					t.Errorf("make %s does not print the VM's done line on its stdout:\nstdout: %s\nstderr: %s", tc.target, stdout, stderr)
				}

				if strings.Contains(stderr, tc.target+": done") {
					t.Errorf("make %s prints the VM's done line on its stderr although the run succeeded: a successful run keeps printing to stdout (issue #227 part 2, the closing review)\nstdout: %s\nstderr: %s",
						tc.target, stdout, stderr)
				}

				return
			}

			if err == nil {
				t.Fatalf("make %s exited 0 although the VM printed %q and exited non-zero: a VM step that failed is no success (issue #227 part 2, the closing review)\nstdout: %q\nstderr: %q",
					tc.target, failure, stdout, stderr)
			}

			if !strings.Contains(stderr, failure) {
				t.Errorf("make %s exited %d and the VM's error line %q is not on make's stderr, so a maintainer who pipes make's stdout to a file never sees what the VM could not do (issue #227 part 2, the closing review)\nstdout: %q\nstderr: %q",
					tc.target, exitStatusOf(t, err), failure, stdout, stderr)
			}

			if strings.Contains(stdout, failure) {
				t.Errorf("make %s prints the VM's error line %q on its stdout, beside the failure make reports: the failure text goes to stderr (issue #227 part 2, the closing review)\nstdout: %q\nstderr: %q",
					tc.target, failure, stdout, stderr)
			}

			assertNoEscape(t, "make "+tc.target, stdout, stderr)
		})
	}
}
