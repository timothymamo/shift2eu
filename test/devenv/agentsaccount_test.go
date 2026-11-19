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
// sudo that answers for the two files the target reads and writes there. No test reaches /Library, a real
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
		if len(args) > 2 && args[len(args)-2] == "pin" {
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
				if len(args) > 0 && args[len(args)-1] == "pin" {
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

// vmScriptRun is one sandbox run of the script make vm-agents-token-ssh sends the VM (issue #227 part 2, review 2):// the generator prints it from a writer the test wrote, and the VM user runs it with a sudo that redirects root's
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
