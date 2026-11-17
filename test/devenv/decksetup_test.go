// Pins the deck's setup on a new VM (issue #234): make deck-restart reloads the service's drop-ins before it
// restarts the daemon, so a changed drop-in takes effect, and make vm-env refuses to rebuild the VM's
// daemon.env unless every credential its VM_ENV names is in the environment, so a locked login Keychain or an
// unloaded direnv .envrc never wipes the credentials the agents need. A narrowed VM_ENV merges: it replaces
// only the names it lists and keeps every other line already in the file (decision recorded on issue #234).
//
// No test reaches the machine it runs on: the sandbox's fake limactl runs the VM side of every target on this
// machine with the fake VM user's HOME, and systemctl is a recording fake. No value in this file is a real
// credential: the tests invent every one of them (CLAUDE.md rule 3).
package devenv_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// vmEnvCredentials are the names make vm-env copies into the VM by default, in VM_ENV's order (Makefile).
var vmEnvCredentials = []string{"CLAUDE_CODE_OAUTH_TOKEN", "DEEPSEEK_API_KEY", "GITHUB_TOKEN"}

// fakeEnvValue is the invented value the tests give one credential: its name with a fake- prefix, so no
// literal in this file pairs a credential's name with a value (gosec G101) and none is a real credential
// (CLAUDE.md rule 3).
func fakeEnvValue(name string) string {
	return "fake-" + strings.ToLower(name) + "-value"
}

// seededDaemonEnv is what the VM's daemon.env holds before a case: the credentials a previous make vm-env
// wrote. A refused run leaves exactly these bytes behind.
const seededDaemonEnv = "CLAUDE_CODE_OAUTH_TOKEN=old-oauth\nDEEPSEEK_API_KEY=old-deepseek\nGITHUB_TOKEN=old-github\n"

// daemonEnvPath is the VM's ~/.config/dot-agent-deck/daemon.env, the file make vm-env writes and the
// credentials.conf drop-in reads at every start.
func (sb *sandbox) daemonEnvPath() string {
	return sb.vmPath(".config/dot-agent-deck/daemon.env")
}

// seedDaemonEnv writes the VM's daemon.env the way a previous make vm-env left it, and returns its bytes.
func (sb *sandbox) seedDaemonEnv(t *testing.T) []byte {
	t.Helper()

	return sb.seedDaemonEnvWith(t)
}

// seedDaemonEnvWith writes the VM's daemon.env as a previous make vm-env left it plus extra lines, the way a
// maintainer or an earlier target left a setting vm-env does not know about, and returns its bytes.
func (sb *sandbox) seedDaemonEnvWith(t *testing.T, extra ...string) []byte {
	t.Helper()

	body := seededDaemonEnv
	for _, line := range extra {
		body += line + "\n"
	}

	writeFileMode(t, sb.daemonEnvPath(), []byte(body), 0o600)

	return []byte(body)
}

// vmEnvEnv is the environment one run of make vm-env gets: the sandbox's PATH with the fake limactl first, a
// temporary HOME, and the three credentials with invented values. No other variable of the test process's own
// environment is passed on, so a real credential of the machine can never reach the VM (CLAUDE.md rule 3).
func (sb *sandbox) vmEnvEnv() map[string]string {
	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"

	for _, name := range vmEnvCredentials {
		env[name] = fakeEnvValue(name)
	}

	return env
}

// assertNoCredentialValues fails when text holds the value of any credential the tests invented. The failure
// names the variable, never its value (CLAUDE.md rule 3).
func assertNoCredentialValues(t *testing.T, where, text string) {
	t.Helper()

	secrets := make([]secret, 0, len(vmEnvCredentials))
	for _, name := range vmEnvCredentials {
		secrets = append(secrets, secret{kind: "value of " + name, value: fakeEnvValue(name)})
	}

	assertNoSecrets(t, where, text, secrets)
}

// sortedEnvLines returns the non-empty lines of a daemon.env, sorted: the order of the lines is not what the
// tests pin, the set of NAME=value lines is.
func sortedEnvLines(body string) []string {
	var lines []string

	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}

	slices.Sort(lines)

	return lines
}

// TestMakeDeckRestartReloadsDropInsBeforeTheRestart pins issue #234's second half: `make deck-restart` asks the
// VM's user manager to reload the units before it restarts dot-agent-deck.service. Without the reload, a
// drop-in dev/lima.yaml rewrote stays unread, and deck-restart warns that the unit changed on disk; with the
// restart and no reload, the daemon comes back with the old settings.
func TestMakeDeckRestartReloadsDropInsBeforeTheRestart(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.useFakeSystemctl(t)

	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"

	stdout, stderr, err := runMake(t, []string{"deck-restart"}, env)
	if err != nil {
		t.Fatalf("make deck-restart: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	var got []string

	for _, args := range invocations(sb.argvLogText(t), "systemctl") {
		got = append(got, "systemctl "+strings.Join(args, " "))
	}

	// The reload first, then the restart: a drop-in is read by the reload, and a reload after the restart would
	// leave the running daemon on the old settings until the next one.
	want := []string{
		"systemctl --user daemon-reload",
		"systemctl --user restart dot-agent-deck.service",
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("make deck-restart asks the VM's user manager for (-want +got):\n%s\nIt must run `systemctl --user daemon-reload` before `systemctl --user restart dot-agent-deck.service`, so a drop-in dev/lima.yaml rewrote takes effect (issue #234)", diff)
	}
}

// TestMakeVMEnvRefusesMissingCredentials pins issue #234's third half: make vm-env checks every name in VM_ENV
// before it writes anything, and refuses when one is unset or empty, naming it and leaving the VM's daemon.env
// byte for byte as it was. A locked login Keychain or an unloaded direnv .envrc in an SSH session used to write
// an empty file, and every agent started without CLAUDE_CODE_OAUTH_TOKEN after the next deck-restart.
func TestMakeVMEnvRefusesMissingCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		unset []string // names left out of make's environment entirely
		empty []string // names make sees as the empty string
		named []string // names the refusal must print
	}{
		{
			name:  "one credential unset",
			unset: []string{"GITHUB_TOKEN"},
			named: []string{"GITHUB_TOKEN"},
		},
		{
			name:  "one credential set to an empty string",
			empty: []string{"DEEPSEEK_API_KEY"},
			named: []string{"DEEPSEEK_API_KEY"},
		},
		{
			name:  "every credential unset",
			unset: vmEnvCredentials,
			named: vmEnvCredentials,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()

			for _, name := range tc.unset {
				delete(env, name)
			}

			for _, name := range tc.empty {
				env[name] = ""
			}

			seeded := sb.seedDaemonEnv(t)

			stdout, stderr, err := runMake(t, []string{"vm-env"}, env)
			if err == nil {
				t.Fatalf("make vm-env exited 0 with %s missing: it must refuse, so the VM keeps the credentials it has instead of getting a daemon.env without them (issue #234)\nstdout: %s\nstderr: %s",
					strings.Join(tc.named, ", "), stdout, stderr)
			}

			for _, name := range tc.named {
				if !strings.Contains(stdout+stderr, name) {
					t.Errorf("the refusal does not name %s, so a maintainer cannot tell which credential is missing:\nstdout: %s\nstderr: %s", name, stdout, stderr)
				}
			}

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make vm-env rewrote the VM's daemon.env although it refused: the file must be byte for byte what it was (issue #234) (-want +got):\n%s", diff)
			}

			// A refusal before the VM is touched at all: nothing handed limactl writes daemon.env.
			if log := sb.argvLogText(t); strings.Contains(log, "daemon.env") {
				t.Errorf("make vm-env handed limactl a command that writes ~/.config/dot-agent-deck/daemon.env although it refused, so the file may be wiped after all:\n%s", log)
			}

			assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
		})
	}
}

// TestMakeVMEnvRefusesANameThatIsNotAVariable pins the other refusal make vm-env makes before it writes
// anything (issue #234): a VM_ENV name that is not a shell identifier - it starts with a digit, or holds a
// character a variable name may not have - is refused instead of written into daemon.env, where the line
// would be meaningless and printenv could never have set it. Every credential is set, so the refusal is
// about the name and not about a missing credential, and the file stays byte for byte as it was.
func TestMakeVMEnvRefusesANameThatIsNotAVariable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		vmEnv  string // the invented name handed to VM_ENV; no variable of the test machine is ever named so
		reason string
	}{
		{
			name:   "the name starts with a digit",
			vmEnv:  "1INVENTED_TOKEN",
			reason: "a name may not start with a digit",
		},
		{
			name:   "the name holds a dash",
			vmEnv:  "INVENTED-TOKEN",
			reason: "a variable name never holds a dash",
		},
		{
			name:   "the name holds a dot",
			vmEnv:  "INVENTED.TOKEN",
			reason: "a variable name never holds a dot",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()

			// The credentials VM_ENV lists are all set, so a refusal can only be about tc.vmEnv.
			seeded := sb.seedDaemonEnv(t)

			stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=" + tc.vmEnv}, env)
			if err == nil {
				t.Fatalf("make vm-env VM_ENV=%s exited 0: it must refuse a name that is not a variable (%s), so no line printenv could never set reaches daemon.env (issue #234)\nstdout: %s\nstderr: %s",
					tc.vmEnv, tc.reason, stdout, stderr)
			}

			if !strings.Contains(stdout+stderr, tc.vmEnv) {
				t.Errorf("the refusal does not name %s, so a maintainer cannot tell which VM_ENV name vm-env refused (%s):\nstdout: %s\nstderr: %s",
					tc.vmEnv, tc.reason, stdout, stderr)
			}

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make vm-env rewrote the VM's daemon.env although it refused %s: the file must be byte for byte what it was (issue #234) (-want +got):\n%s",
					tc.vmEnv, diff)
			}

			// A refusal before the VM is touched at all: nothing handed limactl writes daemon.env.
			if log := sb.argvLogText(t); strings.Contains(log, "daemon.env") {
				t.Errorf("make vm-env handed limactl a command that writes ~/.config/dot-agent-deck/daemon.env although it refused %s:\n%s",
					tc.vmEnv, log)
			}

			assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
		})
	}
}

// TestMakeVMEnvRefusalSaysHowToFixIt pins that the refusal is actionable, in the order a maintainer needs it:
// unlock the login Keychain (a macOS SSH session, where the credentials come from the Keychain), re-enter the
// checkout so direnv reloads the .envrc (other hosts, where they come from direnv), then run make vm-env again.
func TestMakeVMEnvRefusalSaysHowToFixIt(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.vmEnvEnv()
	delete(env, "CLAUDE_CODE_OAUTH_TOKEN")

	stdout, stderr, err := runMake(t, []string{"vm-env"}, env)
	if err == nil {
		t.Fatalf("make vm-env exited 0 with CLAUDE_CODE_OAUTH_TOKEN missing: it must refuse (issue #234)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	text := stdout + stderr

	// The steps, in the order the refusal must name them: unlocking the Keychain first (the macOS case), then
	// re-entering the checkout so direnv reloads the .envrc, then the target again. The wording around them is
	// not pinned, the steps and their order are.
	steps := []string{
		"security unlock-keychain",
		"~/Library/Keychains/login.keychain-db",
		"cd ~ && cd",
		"make vm-env",
	}

	at := -1
	previous := ""

	for _, step := range steps {
		next := strings.Index(text, step)
		if next < 0 {
			t.Errorf("the refusal never mentions %q, so a maintainer whose Keychain is locked or whose .envrc is not loaded does not learn what to do (issue #234):\nstdout: %s\nstderr: %s", step, stdout, stderr)

			continue
		}

		if next < at {
			t.Errorf("the refusal mentions %q before %q: it must name the steps in the order they are done - unlock the login Keychain, re-enter the checkout so direnv reloads the .envrc, then run make vm-env again (issue #234):\nstdout: %s\nstderr: %s",
				step, previous, stdout, stderr)
		}

		at, previous = next, step
	}

	// The non-macOS host, where the credentials come from direnv.
	if !strings.Contains(text, ".envrc") {
		t.Errorf("the refusal does not name the direnv .envrc, the source of the credentials on a host without the login Keychain (issue #234):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	assertNoCredentialValues(t, "make vm-env's refusal", text)
}

// TestMakeVMEnvWritesTheCredentialsItNames pins the case that behaves as before (issue #234): with all three
// credentials set, make vm-env writes them to the VM's daemon.env, mode 0600, whether the VM has the file yet or
// not.
func TestMakeVMEnvWritesTheCredentialsItNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		seed bool // the VM already has a daemon.env, as after an earlier make vm-env
		// extra are the lines of an earlier daemon.env that VM_ENV does not name: the merge keeps them.
		extra []string
	}{
		{name: "the VM has no daemon.env yet, the first run"},
		{name: "the VM has a daemon.env already", seed: true},
		{
			name:  "a line VM_ENV does not name survives the merge",
			seed:  true,
			extra: []string{"UNRELATED_SETTING=kept-by-the-merge"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()

			if tc.seed {
				sb.seedDaemonEnvWith(t, tc.extra...)
			}

			stdout, stderr, err := runMake(t, []string{"vm-env"}, env)
			if err != nil {
				t.Fatalf("make vm-env with every credential set: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			info, statErr := os.Stat(sb.daemonEnvPath())
			if statErr != nil {
				t.Fatalf("make vm-env wrote no ~/.config/dot-agent-deck/daemon.env in the VM: %v", statErr)
			}

			if got := info.Mode().Perm(); got != 0o600 {
				t.Errorf("the VM's daemon.env is mode %04o, want 0600: it holds the agent credentials (issue #234, PRD #161 milestone 1)", got)
			}

			body, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env: %v", readErr)
			}

			want := []string{
				"CLAUDE_CODE_OAUTH_TOKEN=" + fakeEnvValue("CLAUDE_CODE_OAUTH_TOKEN"),
				"DEEPSEEK_API_KEY=" + fakeEnvValue("DEEPSEEK_API_KEY"),
				"GITHUB_TOKEN=" + fakeEnvValue("GITHUB_TOKEN"),
			}
			// A full VM_ENV replaces the names it lists and keeps every other line, so a setting vm-env does
			// not know about is never lost (the merge decision on issue #234).
			want = append(want, tc.extra...)

			if diff := cmp.Diff(sortedEnvLines(strings.Join(want, "\n")), sortedEnvLines(string(body))); diff != "" {
				t.Errorf("make vm-env wrote the wrong lines to the VM's daemon.env (-want +got):\n%s", diff)
			}

			assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
		})
	}
}

// TestMakeVMEnvNarrowedVMEnvKeepsTheOtherCredentials pins the decision recorded on issue #234: making vm-env
// write only a few names by narrowing VM_ENV merges - it replaces the names it lists and keeps every other line
// already in the file - so `make vm-env VM_ENV=GITHUB_TOKEN` never drops the OAuth token and the DeepSeek key.
// A narrowed name that is missing is refused like any other, and the file stays as it was.
func TestMakeVMEnvNarrowedVMEnvKeepsTheOtherCredentials(t *testing.T) {
	t.Parallel()

	t.Run("the name it lists is replaced and the others are kept", func(t *testing.T) {
		t.Parallel()

		sb := newSandbox(t)
		env := sb.vmEnvEnv()
		delete(env, "CLAUDE_CODE_OAUTH_TOKEN")
		delete(env, "DEEPSEEK_API_KEY")

		sb.seedDaemonEnv(t)

		stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=GITHUB_TOKEN"}, env)
		if err != nil {
			t.Fatalf("make vm-env VM_ENV=GITHUB_TOKEN with GITHUB_TOKEN set: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		body, readErr := os.ReadFile(sb.daemonEnvPath())
		if readErr != nil {
			t.Fatalf("read the VM's daemon.env: %v", readErr)
		}

		want := []string{
			"CLAUDE_CODE_OAUTH_TOKEN=old-oauth",
			"DEEPSEEK_API_KEY=old-deepseek",
			"GITHUB_TOKEN=" + fakeEnvValue("GITHUB_TOKEN"),
		}

		if diff := cmp.Diff(sortedEnvLines(strings.Join(want, "\n")), sortedEnvLines(string(body))); diff != "" {
			t.Errorf("make vm-env VM_ENV=GITHUB_TOKEN wrote the wrong lines to the VM's daemon.env: it must replace GITHUB_TOKEN and keep every other line already in the file (issue #234) (-want +got):\n%s", diff)
		}

		assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
	})

	t.Run("the name it lists is missing, so it is refused and the file is kept", func(t *testing.T) {
		t.Parallel()

		sb := newSandbox(t)
		env := sb.vmEnvEnv()
		for _, name := range vmEnvCredentials {
			delete(env, name)
		}

		seeded := sb.seedDaemonEnv(t)

		stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=GITHUB_TOKEN"}, env)
		if err == nil {
			t.Fatalf("make vm-env VM_ENV=GITHUB_TOKEN exited 0 with GITHUB_TOKEN missing: a narrowed VM_ENV must be refused like any other, so the VM keeps the credentials it has (issue #234)\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		if !strings.Contains(stdout+stderr, "GITHUB_TOKEN") {
			t.Errorf("the refusal does not name GITHUB_TOKEN, the one name VM_ENV lists:\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		got, readErr := os.ReadFile(sb.daemonEnvPath())
		if readErr != nil {
			t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
		}

		if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
			t.Errorf("make vm-env rewrote the VM's daemon.env although it refused: the file must be byte for byte what it was (issue #234) (-want +got):\n%s", diff)
		}
	})
}
