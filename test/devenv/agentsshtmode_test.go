// Pins the SSH mode of dev/agents-app-token.sh, the transport of issue #227 part 2: the refresher runs as the
// separate macOS account `_shift2euagents`, which cannot reach the maintainer's ~/.lima, so it writes the token
// into the VM over the VM's pinned SSH port with a key of its own instead of through limactl
// (docs/adr/0031-agents-open-pull-requests-as-a-github-app-with-tokens-minted-on-the-mac.md, update of
// 2026-11-19, 3 to 5).
//
// Every run is a sandbox run: a copy of the script inside a sandbox "host clone", an invented App key, the fake
// GitHub API on 127.0.0.1 and a fake ssh that records its argv and its standard input and exits with a status a
// test sets. No test uses a real key, a real VM, the real ssh, ~/.lima, a LaunchDaemon or an account, and no test
// reaches the network (CLAUDE.md rule 3).
package devenv_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// The port and the VM user the SSH mode tests configure: a port below macOS's ephemeral range, as the ADR asks
// the maintainer to pin, and the VM user a Lima instance has by default.
const (
	sshModePort = "48022"
	sshModeUser = "shift2eu"
)

// sshModeFixture is one environment for a run of the SSH mode of dev/agents-app-token.sh: a mint fixture whose
// script is a copy inside a sandbox "host clone" - the trusted path rule wants a file the running uid owns, and
// the worktree the tests are checked out in may not satisfy it - the account's SSH key and known-hosts file
// beside it, the four SHIFT2EU_AGENTS_VM_SSH_* variables and the fake ssh (issue #227 part 2).
type sshModeFixture struct {
	*mintFixture

	script     string // the copy of dev/agents-app-token.sh the runs start
	sshKey     string // the account's SSH key to the VM
	knownHosts string // the VM's pinned host key
	ssh        *sshRecorder
}

// newSSHModeFixture builds a fixture in SSH mode: every run mints against the fake API and writes over the fake
// ssh, and the four variables name files the trusted path rule accepts.
func newSSHModeFixture(t *testing.T) *sshModeFixture {
	t.Helper()

	f := &sshModeFixture{mintFixture: newMintFixture(t)}

	clone := filepath.Join(f.sb.root, "host-clone")

	f.script = filepath.Join(clone, agentsMintScript)
	writeFileMode(t, f.script, []byte(readRepoFile(t, agentsMintScript)), 0o755)

	secrets := filepath.Join(f.sb.root, "account", ".config", "shift2eu")

	f.sshKey = filepath.Join(secrets, "agents-vm-ssh-key")
	writeFileMode(t, f.sshKey, []byte("an invented ed25519 private key, never a real one\n"), 0o600)

	f.knownHosts = filepath.Join(secrets, "known-hosts-shift2eu")
	writeFileMode(t, f.knownHosts, []byte("shift2eu-vm-shift2eu ssh-ed25519 AAAA-invented-host-key\n"), 0o600)

	f.ssh = f.sb.useFakeSSH(t, f.env)
	f.env["SHIFT2EU_AGENTS_VM_SSH_PORT"] = sshModePort
	f.env["SHIFT2EU_AGENTS_VM_SSH_USER"] = sshModeUser
	f.env["SHIFT2EU_AGENTS_VM_SSH_KEY"] = f.sshKey
	f.env["SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS"] = f.knownHosts

	return f
}

// sshArgvWant is the argv the ADR gives the ssh that writes into the VM, in its order, with nothing else on the
// command line (ADR 0031, update of 2026-11-19, 3).
func (f *sshModeFixture) sshArgvWant() []string {
	return []string{
		"-F", "/dev/null",
		"-T",
		"-p", sshModePort,
		"-l", sshModeUser,
		"-i", f.sshKey,
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + f.knownHosts,
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "HostKeyAlias=shift2eu-vm-shift2eu",
		"-o", "CheckHostIP=no",
		"-o", "UpdateHostKeys=no",
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"-o", "ForwardAgent=no",
		"-o", "ForwardX11=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "PermitLocalCommand=no",
		"-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
		"-o", "LogLevel=ERROR",
		"127.0.0.1", "agents-token-write",
	}
}

// sshRecorder is the fake ssh of one sandbox: it records its argv in the sandbox log and its standard input in a
// numbered file per call, and exits with the status its status file holds.
type sshRecorder struct {
	sb         *sandbox
	dir        string
	statusFile string
}

// calls returns the argv of every ssh call, in call order.
func (r *sshRecorder) calls(t *testing.T) [][]string {
	t.Helper()

	return invocations(r.sb.argvLogText(t), "ssh")
}

// stdinOf returns what reached the nth ssh call's standard input, counting from 0.
func (r *sshRecorder) stdinOf(t *testing.T, n int) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(r.dir, "stdin-"+strconv.Itoa(n)))
	if err != nil {
		t.Fatalf("read what reached ssh call %d's standard input: %v", n, err)
	}

	return string(body)
}

// setStatus makes the next ssh calls exit with status, so a test can let a VM that was down come back.
func (r *sshRecorder) setStatus(t *testing.T, status string) {
	t.Helper()

	writeFileMode(t, r.statusFile, []byte(status+"\n"), 0o600)
}

// exitStatusOf returns the status the run exited with, and fails the test when the run never started.
func exitStatusOf(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		return 0
	}

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("the run failed without an exit status: %v", err)
	}

	return exit.ExitCode()
}

// TestAgentsAppTokenSSHModeWritesTheTokenOverSSH pins the whole of one mint in SSH mode (ADR 0031, update of
// 2026-11-19, 3 and 5): every write into the VM runs the ssh that SHIFT2EU_AGENTS_SSH names - never the one PATH
// offers - with exactly the argv of the ADR and nothing else on it, the token, the expiry and the (still empty)
// bot identity go in on standard input, the token never reaches an argument, and no limactl runs at all.
func TestAgentsAppTokenSSHModeWritesTheTokenOverSSH(t *testing.T) {
	t.Parallel()

	f := newSSHModeFixture(t)

	stdout, stderr, err := f.sb.run(t, f.script, []string{"mint"}, f.env, "")
	if err != nil {
		t.Fatalf("mint in SSH mode exited %d: %v\nstdout: %s\nstderr: %s\nthe fake API refused: %s",
			exitStatusOf(t, err), err, stdout, stderr, strings.Join(f.api.recordedComplaints(), "; "))
	}

	f.assertNoLeaks(t, stdout, stderr)

	tokens := f.api.minted()
	if len(tokens) != 1 {
		t.Fatalf("the fake API minted %d tokens, want one: a mint writes one installation token", len(tokens))
	}

	calls := f.ssh.calls(t)
	if len(calls) == 0 {
		t.Fatalf("the mint called no ssh although all four SHIFT2EU_AGENTS_VM_SSH_* variables are set: in SSH mode the token reaches the VM over ssh, never through limactl (ADR 0031, update of 2026-11-19, 3)\nstderr: %s", stderr)
	}

	want := f.sshArgvWant()

	for i, args := range calls {
		if diff := cmp.Diff(want, args); diff != "" {
			t.Errorf("ssh call %d's argv is not the one the ADR gives (-want +got):\n%s", i, diff)
		}

		for _, secret := range []string{tokens[0], string(f.key.pem)} {
			if secret != "" && strings.Contains(strings.Join(args, " "), secret) {
				t.Errorf("ssh call %d's argv holds a secret: the token and the App's key reach the VM only on standard input and never on a command line", i)
			}
		}
	}

	if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) > 0 {
		t.Errorf("the mint in SSH mode ran limactl %v: the account cannot reach the maintainer's ~/.lima, so SSH mode runs no limactl at all (ADR 0031, update of 2026-11-19, 3)", calls[0])
	}

	// The four lines the VM writer reads arrive on ssh's standard input: the token, its expiry, and an empty
	// name and email, since the bot user is looked up after the token is written.
	lines := strings.Split(f.ssh.stdinOf(t, 0), "\n")
	if lines[0] != tokens[0] {
		t.Errorf("the first line of ssh's standard input is not the token the fake API minted, so the VM writer cannot write it")
	}

	if expiry, err := strconv.ParseInt(lines[1], 10, 64); err != nil || expiry < time.Now().Unix() {
		t.Errorf("the second line of ssh's standard input is %q, want the token's expiry as Unix seconds in the future: %v", lines[1], err)
	}

	if len(lines) < 4 || lines[2] != "" || lines[3] != "" {
		t.Errorf("ssh's standard input is %q, want four lines: the token, its expiry, and an empty git user.name and email, as the VM writer reads them",
			f.ssh.stdinOf(t, 0))
	}

	if _, err := os.Stat(f.sb.vmPath(agentsTokenFile)); err == nil {
		t.Errorf("%s is in the fake VM's home although no writer ran there: in SSH mode the token is written over ssh, not into a local VM", agentsTokenFile)
	}

	if !strings.Contains(stderr, "wrote") {
		t.Errorf("the mint's stderr does not say it wrote the token into the VM:\n%s", stderr)
	}
}

// TestAgentsAppTokenRefusesAPartialSSHConfiguration pins the mode choice (ADR 0031, update of 2026-11-19, 5): SSH
// mode is on when all four SHIFT2EU_AGENTS_VM_SSH_* variables are set and not empty, limactl mode when none is,
// and anything between is refused before the script mints, as a permanent failure that names the variables which
// are missing.
func TestAgentsAppTokenRefusesAPartialSSHConfiguration(t *testing.T) {
	t.Parallel()

	names := []string{
		"SHIFT2EU_AGENTS_VM_SSH_PORT",
		"SHIFT2EU_AGENTS_VM_SSH_USER",
		"SHIFT2EU_AGENTS_VM_SSH_KEY",
		"SHIFT2EU_AGENTS_VM_SSH_KNOWN_HOSTS",
	}

	for _, name := range names {
		t.Run(name+" is not set", func(t *testing.T) {
			t.Parallel()

			f := newSSHModeFixture(t)
			delete(f.env, name)

			assertPartialSSHRefused(t, f, name)
		})

		t.Run(name+" is set but empty", func(t *testing.T) {
			t.Parallel()

			f := newSSHModeFixture(t)
			f.env[name] = ""

			assertPartialSSHRefused(t, f, name)
		})
	}
}

// assertPartialSSHRefused fails unless the mint refused a partial SSH configuration as a permanent failure that
// names the variable which is missing, before it sent anything anywhere.
func assertPartialSSHRefused(t *testing.T, f *sshModeFixture, missing string) {
	t.Helper()

	stdout, stderr, err := f.sb.run(t, f.script, []string{"mint"}, f.env, "")

	if status := exitStatusOf(t, err); status != 1 {
		t.Errorf("mint with %s missing exited %d, want the permanent failure's status 1 (ADR 0031, update of 2026-11-19, 5)\nstdout: %s\nstderr: %s",
			missing, status, stdout, stderr)
	}

	for _, want := range []string{"SSH mode needs all of", "not set: " + missing} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal of a partial SSH configuration does not say %q, so a maintainer cannot tell which variable to set:\n%s", want, stderr)
		}
	}

	if calls := f.ssh.calls(t); len(calls) > 0 {
		t.Errorf("the refused run called ssh %v: a partial configuration is refused before anything is minted or sent", calls[0])
	}

	if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) > 0 {
		t.Errorf("the refused run called limactl %v: neither mode may run when the configuration is partial", calls[0])
	}

	if got := f.api.posted(); len(got) != 0 {
		t.Errorf("the refused run posted to %v: a partial SSH configuration is refused before the mint", got)
	}

	if _, err := os.Stat(f.sb.vmPath(agentsTokenFile)); err == nil {
		t.Errorf("the refused run wrote %s", agentsTokenFile)
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenSSHExitStatus pins what the writer's exit statuses mean through ssh (ADR 0031, update of
// 2026-11-19, 3): 3 is permanent and stops the refresh, 4 is transient and only retried, and 255 is ssh's own
// failure - transient too, with one line that names every cause the maintainer can act on and no value.
func TestAgentsAppTokenSSHExitStatus(t *testing.T) {
	t.Parallel()

	// The line the ADR gives for ssh's own failure, with the token it was writing and this instance's port: it
	// names the VM, the port and the exit status without a value, and each cause with the target that fixes it.
	sshFailed := "into the VM shift2eu over SSH (127.0.0.1:" + sshModePort + ", ssh exit 255): is it running (make vm-up)? " +
		"If ssh said `Host key verification failed` above, the VM's host key changed (a recreated VM): pin it again with make vm-agents-token-ssh; " +
		"if it said `Permission denied`, the VM does not authorise the account's key: run make vm-agents-token-ssh"

	cases := []struct {
		name   string
		status string
		// stopped says the status is permanent: the refresh stops with the stop line and status 1.
		stopped bool
		// want are the fragments the refresh's stderr must hold while it retries.
		want []string
	}{
		{
			name:    "status 3 is permanent and stops the refresh",
			status:  "3",
			stopped: true,
		},
		{
			name:   "status 4 is transient and the refresh retries",
			status: "4",
			want:   []string{"will try again"},
		},
		{
			name:   "status 255 is transient and names every cause",
			status: "255",
			want:   []string{"could not write the", sshFailed},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newSSHModeFixture(t)
			env := f.refreshEnv()
			f.ssh.setStatus(t, tc.status)

			refresh := f.sb.start(t, f.script, []string{"refresh"}, env)

			if tc.stopped {
				if !refresh.waitExit(30 * time.Second) {
					t.Fatalf("the refresh was still running 30s after ssh exited 3, a permanent failure the loop must stop on; stderr:\n%s", refresh.errOut())
				}

				if status := refresh.exitCode(t); status != 1 {
					t.Errorf("the refresh exited %d after ssh exited 3, want the stop status 1; stderr:\n%s", status, refresh.errOut())
				}

				if !strings.Contains(refresh.errOut(), "refused to write") {
					t.Errorf("the refresh's stderr does not say the VM refused to write the token:\n%s", refresh.errOut())
				}

				assertStopLineNamesWhereToRestart(t, refresh.errOut())

				return
			}

			waitFor(t, 30*time.Second, "the refresh's first ssh write", func() bool {
				return len(f.ssh.calls(t)) >= 1
			})

			waitFor(t, 30*time.Second, "the refresh to try a second ssh write", func() bool {
				return len(f.ssh.calls(t)) >= 2 || !refresh.running()
			})

			if !refresh.running() {
				t.Fatalf("the refresh exited after ssh exited %s, although a transient failure must never stop it; stderr:\n%s", tc.status, refresh.errOut())
			}

			if strings.Contains(refresh.errOut(), "agents App token refresh stopped") {
				t.Errorf("the refresh said it stopped after ssh exited %s: that status is transient, and the loop retries it; stderr:\n%s", tc.status, refresh.errOut())
			}

			for _, want := range tc.want {
				if !strings.Contains(refresh.errOut(), want) {
					t.Errorf("the refresh's stderr does not hold %q, so a maintainer cannot tell what to check:\n%s", want, refresh.errOut())
				}
			}

			// The VM comes back: the next check writes the token, without anyone restarting the refresh.
			f.ssh.setStatus(t, "0")

			waitFor(t, 30*time.Second, "the refresh to write the token once ssh succeeds", func() bool {
				return strings.Contains(refresh.errOut(), "wrote a token into the VM")
			})

			if !refresh.running() {
				t.Fatalf("the refresh exited after it recovered; stderr:\n%s", refresh.errOut())
			}

			refresh.signal(t, syscall.SIGTERM)

			if !refresh.waitExit(60 * time.Second) {
				t.Fatalf("the refresh did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
			}

			f.assertNoLeaks(t, refresh.out(), refresh.errOut())
		})
	}
}

// TestAgentsAppTokenWithoutSSHVariablesStaysInLimactlMode pins the other half of the mode choice (ADR 0031, update
// of 2026-11-19, 5 and 6): with no SHIFT2EU_AGENTS_VM_SSH_* variable set, the single-account mode is what runs, its
// limactl writes the token, and ssh is not called at all.
func TestAgentsAppTokenWithoutSSHVariablesStaysInLimactlMode(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	f.sb.useFakeSSH(t, f.env)

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err != nil {
		t.Fatalf("mint in limactl mode exited %d: %v\nstderr: %s", exitStatusOf(t, err), err, stderr)
	}

	if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) == 0 {
		t.Fatalf("the mint ran no limactl although no SHIFT2EU_AGENTS_VM_SSH_* variable is set: that is the single-account mode, which writes the token through limactl (ADR 0031, update of 2026-11-19, 5)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if calls := invocations(f.sb.argvLogText(t), "ssh"); len(calls) > 0 {
		t.Errorf("the mint called ssh %v although no SHIFT2EU_AGENTS_VM_SSH_* variable is set: limactl mode runs no ssh", calls[0])
	}

	if _, err := os.Stat(f.sb.vmPath(agentsTokenFile)); err != nil {
		t.Errorf("the mint wrote no %s through limactl: %v", agentsTokenFile, err)
	}
}

// TestAgentsAppTokenVMWriterPrintsTheWriter pins the single source of the VM's writer (ADR 0031, update of
// 2026-11-19, 4): `dev/agents-app-token.sh vm-writer` prints `#!/bin/sh`, a line, then today's vm_write text byte
// for byte and nothing else, so `make vm-agents-token-ssh` can install exactly the script this file mints with,
// and it mints nothing, reads no input and runs no tool.
func TestAgentsAppTokenVMWriterPrintsTheWriter(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)

	// The environment of a maintainer who has none of the App's variables: vm-writer needs none of them.
	stdout, stderr, err := sb.run(t, scriptPath(t, agentsMintScript), []string{"vm-writer"}, sb.baseEnv(), "")
	if err != nil {
		t.Fatalf("vm-writer exited %d: %v\nstdout: %s\nstderr: %s", exitStatusOf(t, err), err, stdout, stderr)
	}

	lines := strings.Split(stdout, "\n")
	if lines[0] != "#!/bin/sh" {
		t.Errorf("vm-writer's first line is %q, want the shebang #!/bin/sh: the writer is installed as an executable script in the VM (ADR 0031, update of 2026-11-19, 4)", lines[0])
	}

	if len(lines) < 2 || strings.TrimSpace(lines[1]) == "" {
		t.Fatalf("vm-writer printed no second line: the ADR gives `#!/bin/sh`, a line, then the writer's text\nstdout:\n%s", stdout)
	}

	rest, ok := strings.CutPrefix(stdout, "#!/bin/sh\n"+lines[1]+"\n")
	if !ok {
		t.Fatalf("vm-writer's output does not start with its shebang and one line\nstdout:\n%s", stdout)
	}

	if diff := cmp.Diff(vmWriteScript(t), strings.TrimSuffix(rest, "\n")); diff != "" {
		t.Errorf("vm-writer's output after its first two lines is not the vm_write text this file mints with, byte for byte (-want +got):\n%s", diff)
	}

	log := sb.argvLogText(t)

	for _, tool := range []string{"limactl", "ssh", "curl", "openssl"} {
		if calls := invocations(log, tool); len(calls) > 0 {
			t.Errorf("vm-writer ran %s %v: it prints the writer's text and does nothing else", tool, calls[0])
		}
	}
}

// TestAgentsAppTokenRefusesAPathTheTrustedRuleRejects pins the trusted path rule of the account mode (ADR 0031,
// update of 2026-11-19, 2): the App's key, the SSH key, the known-hosts file and the script itself must be owned by
// the running uid, with no group or other write bit on any directory above them, and the keys must have no group or
// other bit at all; the refresh refuses with the path and why before it mints. The last case is the positive
// control: a compliant layout is accepted and the token is written.
func TestAgentsAppTokenRefusesAPathTheTrustedRuleRejects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// change widens a mode of the layout, or does nothing for the control case.
		change func(t *testing.T, f *sshModeFixture)
		// reason is the word the refusal must hold, and paths the paths it may name.
		reason string
		paths  func(f *sshModeFixture) []string
		// accepted says the run must succeed: the compliant layout.
		accepted bool
	}{
		{
			name: "the App's key's directory is group-writable",
			change: func(t *testing.T, f *sshModeFixture) {
				t.Helper()
				chmodDir(t, filepath.Dir(f.key.path), 0o770)
			},
			reason: "group-writable",
			paths:  func(f *sshModeFixture) []string { return []string{filepath.Dir(f.key.path)} },
		},
		{
			name: "the App's key's directory is other-writable",
			change: func(t *testing.T, f *sshModeFixture) {
				t.Helper()
				chmodDir(t, filepath.Dir(f.key.path), 0o707)
			},
			reason: "other-writable",
			paths:  func(f *sshModeFixture) []string { return []string{filepath.Dir(f.key.path)} },
		},
		{
			name: "the SSH key's directory is group-writable",
			change: func(t *testing.T, f *sshModeFixture) {
				t.Helper()
				chmodDir(t, filepath.Dir(f.sshKey), 0o770)
			},
			reason: "group-writable",
			paths:  func(f *sshModeFixture) []string { return []string{filepath.Dir(f.sshKey)} },
		},
		{
			name: "the known-hosts file's directory is other-writable",
			change: func(t *testing.T, f *sshModeFixture) {
				t.Helper()
				chmodDir(t, filepath.Dir(f.knownHosts), 0o707)
			},
			reason: "other-writable",
			paths:  func(f *sshModeFixture) []string { return []string{filepath.Dir(f.knownHosts)} },
		},
		{
			name: "the script's directory is group-writable",
			change: func(t *testing.T, f *sshModeFixture) {
				t.Helper()
				chmodDir(t, filepath.Dir(f.script), 0o770)
			},
			reason: "group-writable",
			paths:  func(f *sshModeFixture) []string { return []string{filepath.Dir(f.script)} },
		},
		{
			name: "the SSH key is readable by the group",
			change: func(t *testing.T, f *sshModeFixture) {
				t.Helper()
				writeFileMode(t, f.sshKey, []byte("an invented ed25519 private key, never a real one\n"), 0o640)
			},
			reason: "group",
			paths:  func(f *sshModeFixture) []string { return []string{f.sshKey} },
		},
		{
			name: "the App's key is in a sticky directory the running account owns",
			change: func(t *testing.T, f *sshModeFixture) {
				t.Helper()

				// 1777 with the sticky bit, as /tmp is - but owned by the running account, not by root: only root
				// gets the exception a sticky directory carries, since root's entries are the ones another account
				// cannot rename away.
				shared := filepath.Join(filepath.Dir(f.key.path), "shared")
				if err := os.Mkdir(shared, 0o700); err != nil {
					t.Fatalf("create %s: %v", shared, err)
				}

				chmodDir(t, shared, 0o777|os.ModeSticky)

				info, err := os.Stat(shared)
				if err != nil {
					t.Fatalf("read the mode of %s: %v", shared, err)
				}

				if info.Mode()&os.ModeSticky == 0 || info.Mode()&os.ModeDir == 0 {
					t.Fatalf("%s has the mode %v, without the sticky bit the test needs: the refusal below would then pin something else (issue #227 part 2, review 5)", shared, info.Mode())
				}

				if info.Mode().Perm() != 0o777 {
					t.Fatalf("%s has the mode %v, want 1777: the test pins a sticky directory another account can write, as /tmp is (issue #227 part 2, review 5)", shared, info.Mode())
				}

				keyPath := filepath.Join(shared, "agents-app.pem")
				writeFileMode(t, keyPath, f.key.pem, 0o600)

				f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = keyPath
			},
			reason: "writable",
			paths:  func(f *sshModeFixture) []string { return []string{filepath.Join(filepath.Dir(f.key.path), "shared")} },
		},
		{
			name:     "a compliant layout is accepted",
			change:   func(*testing.T, *sshModeFixture) {},
			accepted: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newSSHModeFixture(t)
			tc.change(t, f)

			stdout, stderr, err := f.sb.run(t, f.script, []string{"mint"}, f.env, "")

			if tc.accepted {
				if err != nil {
					t.Fatalf("mint refused a layout the trusted path rule accepts, exiting %d; the rule must not stop the account from minting (ADR 0031, update of 2026-11-19, 2)\nstdout: %s\nstderr: %s",
						exitStatusOf(t, err), stdout, stderr)
				}

				if len(f.api.minted()) != 1 {
					t.Errorf("the accepted layout minted %d tokens, want one", len(f.api.minted()))
				}

				return
			}

			if status := exitStatusOf(t, err); status != 1 {
				t.Errorf("mint exited %d for %s, want the permanent failure's status 1: a path that fails the trusted path rule is refused before the mint\nstdout: %s\nstderr: %s",
					status, tc.name, stdout, stderr)
			}

			lower := strings.ToLower(stderr)

			if !strings.Contains(lower, tc.reason) {
				t.Errorf("the refusal of %s does not say %q, so a maintainer cannot tell why the path fails the trusted path rule (ADR 0031, update of 2026-11-19, 2):\n%s",
					tc.name, tc.reason, stderr)
			}

			if !mentionsOneOf(stderr, tc.paths(f)...) {
				t.Errorf("the refusal of %s names none of %v, so a maintainer cannot tell which path to fix (ADR 0031, update of 2026-11-19, 2):\n%s",
					tc.name, tc.paths(f), stderr)
			}

			if got := f.api.posted(); len(got) != 0 {
				t.Errorf("the run posted to %v before it refused %s: the rule is checked before the script mints", got, tc.name)
			}

			if calls := f.ssh.calls(t); len(calls) > 0 {
				t.Errorf("the run called ssh %v before it refused %s", calls[0], tc.name)
			}

			if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) > 0 {
				t.Errorf("the run called limactl %v before it refused %s", calls[0], tc.name)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// mentionsOneOf reports whether text holds any of the paths as it is.
func mentionsOneOf(text string, paths ...string) bool {
	for _, path := range paths {
		if path != "" && strings.Contains(text, path) {
			return true
		}
	}

	return false
}

// chmodDir sets a directory's mode, failing the test when it cannot.
func chmodDir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s to %v: %v", path, mode, err)
	}
}

// TestAgentsAppTokenTrustedPathChecksACLs pins audit 3 and review 11 of issue #227 part 2: a Mac's ls marks a mode
// that carries an ACL with a trailing +, so the rule reads the ACL with ls -lde and refuses the entry when a
// permission it grants is an allow for any account; a deny-only ACL, which every macOS home carries, grants
// nothing and is accepted. The refresh refuses before it mints, naming the path and the ACL.
func TestAgentsAppTokenTrustedPathChecksACLs(t *testing.T) {
	t.Parallel()

	const (
		allowEntry = " 0: user:_shift2euagents allow read,write\n"
		denyEntry  = " 0: group:everyone deny delete\n"
	)

	cases := []struct {
		name     string
		entries  string
		accepted bool
	}{
		{name: "an ACL that allows another account", entries: allowEntry},
		{name: "a deny-only ACL", entries: denyEntry, accepted: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newSSHModeFixture(t)
			aclProbe(t, f.sb, f.env, f.key.path, tc.entries)

			stdout, stderr, err := f.sb.run(t, f.script, []string{"mint"}, f.env, "")

			if tc.accepted {
				if err != nil {
					t.Fatalf("the mint refused a path whose only ACL entry denies, exiting %d: a deny grants nothing, and every macOS home carries one, so the rule accepts it (issue #227 part 2, audit 3)\nstdout: %s\nstderr: %s",
						exitStatusOf(t, err), stdout, stderr)
				}

				if len(f.api.minted()) != 1 {
					t.Errorf("the accepted path minted %d tokens, want one", len(f.api.minted()))
				}

				f.assertNoLeaks(t, stdout, stderr)

				return
			}

			if status := exitStatusOf(t, err); status != 1 {
				t.Errorf("the mint exited %d for a key whose directory ACL allows another account, want the permanent failure's status 1 (issue #227 part 2, audit 3)\nstdout: %s\nstderr: %s",
					status, stdout, stderr)
			}

			if !strings.Contains(stderr, "ACL") {
				t.Errorf("the refusal does not say the entry's ACL is what failed, so a maintainer cannot tell why the path is refused:\n%s", stderr)
			}

			if !mentionsOneOf(stderr, f.key.path, filepath.Dir(f.key.path)) {
				t.Errorf("the refusal names neither %s nor its directory, so a maintainer cannot tell which path to fix:\n%s", f.key.path, stderr)
			}

			if got := f.api.posted(); len(got) != 0 {
				t.Errorf("the run posted to %v before it refused the ACL: the rule is checked before the script mints", got)
			}

			if calls := f.ssh.calls(t); len(calls) > 0 {
				t.Errorf("the run called ssh %v before it refused the ACL", calls[0])
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}
