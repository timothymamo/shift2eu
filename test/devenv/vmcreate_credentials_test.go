// Pins issue #258: make vm-create checks the credentials before it creates the VM. make vm-env has refused a
// missing or empty VM_ENV credential since issue #234, but vm-create runs vm-up first, so on a macOS host whose
// login Keychain is locked in an SSH session - every unattended rebuild - the VM was created, minutes of
// provisioning ran, and only then did vm-env stop the rebuild and leave a half-set-up VM behind. vm-create now
// runs the same host-side check, with vm-env's message, before vm-up: a rebuild that cannot finish costs no VM.
//
// Every case runs against the fake limactl of the vm-up tests (vmUpFixture), so what a refusal must leave behind
// is pinned as "no limactl call at all" - not as "no VM was created", which would need a real Lima. The
// fixture's environment holds none of the test machine's own variables; the three VM_ENV credentials get
// obviously invented values here, and a case that leaves one unset deletes it from that environment instead of
// relying on the host's (CLAUDE.md rules 3 and 4). A run with every credential set is already pinned by
// TestMakeVMCreatePassesTheVMResourcesToVMUp (issue #228), which sets all three through the same fixture: it
// shows that vm-create still reaches limactl, so issue #258 does not need a second test for that.
package devenv_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// fakeNotRealValue is an obviously invented credential value, distinct from fakeEnvValue's, so a case can tell a
// value the run printed from one it never held: no literal in this file pairs a credential's name with a value
// (gosec G101), and none is a real credential (CLAUDE.md rule 3).
func fakeNotRealValue(label string) string {
	return "fake-not-real-" + label + "-value"
}

// vmCreateRefusalGuidance is what a refused make vm-create must say beyond the missing names: vm-env's own
// message, so a maintainer whose login Keychain is locked, or whose direnv .envrc is not loaded, learns what to
// do and that the target they ran is the one to run again (issue #258). The wording around these fragments is
// not pinned, the steps are.
var vmCreateRefusalGuidance = []string{
	"not set or empty:",
	"security unlock-keychain",
	"~/Library/Keychains/login.keychain-db",
	"cd ~ && cd",
	".envrc",
	"make vm-create",
}

// assertVMCreateRefusalSaysHowToFixIt fails when a refused make vm-create's output does not carry vm-env's
// guidance: the missing names alone leave a maintainer of a locked Keychain with no way out (issue #258).
func assertVMCreateRefusalSaysHowToFixIt(t *testing.T, stdout, stderr string) {
	t.Helper()

	var missing []string

	for _, want := range vmCreateRefusalGuidance {
		if !strings.Contains(stderr, want) {
			missing = append(missing, want)
		}
	}

	if diff := cmp.Diff([]string(nil), missing); diff != "" {
		t.Errorf("make vm-create's refusal does not carry vm-env's guidance (-want +missing):\n%s\nA missing credential is refused before vm-up, with the message a maintainer's locked login Keychain needs (issue #258):\nstdout: %s\nstderr: %s",
			diff, stdout, stderr)
	}
}

// assertNoLimactlCalls fails when make called limactl at all. vm-create checks the credentials before vm-up, so a
// refused run creates nothing: no VM, no provisioning (issue #258).
func assertNoLimactlCalls(t *testing.T, f *vmUpFixture, why string) {
	t.Helper()

	log := f.sb.argvLogText(t)

	if calls := invocations(log, "limactl"); len(calls) > 0 {
		t.Errorf("make vm-create called limactl %d time(s) although %s: the credential check runs before vm-up, so a rebuild that cannot finish costs no VM and no provisioning (issue #258):%s",
			len(calls), why, describeCalls(calls))
	}
}

// assertNoCredentialValuesOf fails when text holds the value of one of the VM_ENV credentials in env. The
// failure names the variable, never its value (CLAUDE.md rule 3).
func assertNoCredentialValuesOf(t *testing.T, where, text string, env map[string]string) {
	t.Helper()

	secrets := make([]secret, 0, len(vmEnvCredentials))
	for _, name := range vmEnvCredentials {
		secrets = append(secrets, secret{kind: "value of " + name, value: env[name]})
	}

	assertNoSecrets(t, where, text, secrets)
}

// TestMakeVMCreateRefusesMissingCredentialsBeforeLimactl pins issue #258's first acceptance: make vm-create with
// one or more VM_ENV credentials unset, or set to the empty string, exits non-zero before any limactl call,
// names every missing variable, and carries vm-env's guidance - the login Keychain, the re-entry into the
// checkout, and make vm-create itself as the target to run again. What it never prints is a value.
func TestMakeVMCreateRefusesMissingCredentialsBeforeLimactl(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		unset []string          // left out of make's environment entirely
		empty []string          // names make sees as the empty string
		set   map[string]string // names a case gives an invented value of its own
		named []string          // names the refusal must print
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
			name:  "two credentials unset",
			unset: []string{"CLAUDE_CODE_OAUTH_TOKEN", "GITHUB_TOKEN"},
			named: []string{"CLAUDE_CODE_OAUTH_TOKEN", "GITHUB_TOKEN"},
		},
		{
			name:  "every credential unset",
			unset: vmEnvCredentials,
			named: vmEnvCredentials,
		},
		{
			// The values are the point: a refusal may name a variable, never print what one holds.
			name:  "the values of the credentials that are set are never printed",
			unset: []string{"GITHUB_TOKEN"},
			set: map[string]string{
				"CLAUDE_CODE_OAUTH_TOKEN": fakeNotRealValue("oauth"),
				"DEEPSEEK_API_KEY":        fakeNotRealValue("deepseek"),
			},
			named: []string{"GITHUB_TOKEN"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")

			for _, name := range tc.unset {
				delete(f.env, name)
			}

			for _, name := range tc.empty {
				f.env[name] = ""
			}

			for name, value := range tc.set {
				f.env[name] = value
			}

			stdout, stderr, err := f.run(t, "vm-create")
			if err == nil {
				t.Fatalf("make vm-create exited 0 with %s unset or empty: it must refuse before vm-up, so a rebuild whose credentials are missing does not create and provision a VM it then abandons (issue #258)\nstdout: %s\nstderr: %s",
					strings.Join(tc.named, ", "), stdout, stderr)
			}

			for _, name := range tc.named {
				if !strings.Contains(stderr, name) {
					t.Errorf("make vm-create's refusal does not name %s, so a maintainer cannot tell which credential is missing (issue #258)\nstdout: %s\nstderr: %s", name, stdout, stderr)
				}
			}

			assertVMCreateRefusalSaysHowToFixIt(t, stdout, stderr)
			assertNoLimactlCalls(t, f, "a credential is unset or empty")
			assertNoCredentialValuesOf(t, "make vm-create's output", stdout+stderr, f.env)
		})
	}
}

// TestMakeVMCreateChecksANarrowedVMEnv pins that a VM_ENV given on the make command line is checked the same way
// (issue #258): it names the credentials a maintainer copies on purpose (issue #234), so `make vm-create
// VM_ENV=GITHUB_TOKEN` refuses before limactl when GITHUB_TOKEN is unset, and does not refuse for the two that
// very VM_ENV never named.
func TestMakeVMCreateChecksANarrowedVMEnv(t *testing.T) {
	t.Parallel()

	const narrowed = "VM_ENV=GITHUB_TOKEN"

	t.Run("the credential it names is unset", func(t *testing.T) {
		t.Parallel()

		f := newVMUpFixture(t, "", "")
		delete(f.env, "GITHUB_TOKEN")

		stdout, stderr, err := f.run(t, "vm-create", narrowed)
		if err == nil {
			t.Fatalf("make vm-create %s exited 0 with GITHUB_TOKEN unset: a VM_ENV narrowed on the command line is checked like the default one, before vm-up (issue #258)\nstdout: %s\nstderr: %s",
				narrowed, stdout, stderr)
		}

		if !strings.Contains(stderr, "GITHUB_TOKEN") {
			t.Errorf("make vm-create %s' refusal does not name GITHUB_TOKEN, so a maintainer cannot tell which credential the VM_ENV they narrowed names is missing (issue #258)\nstdout: %s\nstderr: %s",
				narrowed, stdout, stderr)
		}

		assertVMCreateRefusalSaysHowToFixIt(t, stdout, stderr)
		assertNoLimactlCalls(t, f, "GITHUB_TOKEN is unset")
		assertNoCredentialValuesOf(t, "make vm-create's output", stdout+stderr, f.env)
	})

	t.Run("the credentials it does not name are unset", func(t *testing.T) {
		t.Parallel()

		f := newVMUpFixture(t, "", "")
		delete(f.env, "CLAUDE_CODE_OAUTH_TOKEN")
		delete(f.env, "DEEPSEEK_API_KEY")

		stdout, stderr, err := f.run(t, "vm-create", narrowed)
		if err != nil {
			t.Fatalf("make vm-create %s exited non-zero with GITHUB_TOKEN set: the check covers the names VM_ENV holds, and this VM_ENV holds only GITHUB_TOKEN (issue #258)\nstdout: %s\nstderr: %s",
				narrowed, stdout, stderr)
		}

		if starts := f.starts(t); len(starts) == 0 {
			t.Errorf("make vm-create %s did not reach limactl at all, so a narrowed VM_ENV with its credential set no longer creates the VM (issue #258):\nstdout: %s\nstderr: %s",
				narrowed, stdout, stderr)
		}
	})
}

// TestMakeVMCreateRefusesAVMEnvTheHostCheckRefuses pins that every VM_ENV the vm-env check refuses on the host
// stops vm-create before any limactl call too (issue #258): a value with characters other than names, commas and
// spaces - which may be a credential pasted into VM_ENV by mistake, so the refusal never shows it (issue #234) -
// an empty VM_ENV, and one of the agents App's variables, whose key and path never enter the VM (PRD #161).
func TestMakeVMCreateRefusesAVMEnvTheHostCheckRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		vmEnv      string   // the VM_ENV handed to make on its command line
		want       []string // substrings the refusal must hold
		wantLower  []string // substrings the refusal must hold, compared case-insensitively
		reason     string
		neverShown bool // whether the value VM_ENV holds may not appear in the refusal at all
	}{
		{
			name:       "a value holding characters other than names, commas and spaces",
			vmEnv:      "GITHUB_TOKEN=" + fakeNotRealValue("token"),
			want:       []string{"VM_ENV", "not shown"},
			reason:     "a value may be a credential pasted into VM_ENV by mistake, so the refusal never shows it",
			neverShown: true,
		},
		{
			name:      "an empty VM_ENV",
			vmEnv:     "",
			want:      []string{"VM_ENV"},
			wantLower: []string{"empty"},
			reason:    "an empty VM_ENV names no credential at all",
		},
		{
			name:   "one of the agents App's variables",
			vmEnv:  "SHIFT2EU_AGENTS_APP_KEY",
			want:   []string{"SHIFT2EU_AGENTS_APP_KEY"},
			reason: "the agents App's key and its path stay on the host and only its tokens enter the VM",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")

			stdout, stderr, err := f.run(t, "vm-create", "VM_ENV="+tc.vmEnv)
			if err == nil {
				t.Fatalf("make vm-create VM_ENV=%s exited 0, but vm-env refuses that VM_ENV (%s), so vm-create must refuse it before vm-up creates the VM (issue #258)\nstdout: %s\nstderr: %s",
					tc.vmEnv, tc.reason, stdout, stderr)
			}

			text, lower := stdout+stderr, strings.ToLower(stdout+stderr)

			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("make vm-create VM_ENV=%s' refusal does not say %q, so a maintainer cannot tell why it stopped (%s) (issue #258)\nstdout: %s\nstderr: %s",
						tc.vmEnv, want, tc.reason, stdout, stderr)
				}
			}

			for _, want := range tc.wantLower {
				if !strings.Contains(lower, want) {
					t.Errorf("make vm-create VM_ENV=%s' refusal does not say %q, so a maintainer cannot tell why it stopped (%s) (issue #258)\nstdout: %s\nstderr: %s",
						tc.vmEnv, want, tc.reason, stdout, stderr)
				}
			}

			if tc.neverShown {
				for _, shown := range []string{tc.vmEnv, vmEnvInvalidTail(tc.vmEnv)} {
					if shown != "" && strings.Contains(text, shown) {
						t.Errorf("make vm-create VM_ENV=%s' refusal shows what VM_ENV held, but %s (issue #258, issue #234 audit L1)\nstdout: %s\nstderr: %s",
							tc.vmEnv, tc.reason, stdout, stderr)
					}
				}
			}

			assertNoLimactlCalls(t, f, "VM_ENV itself is refused")
			assertNoCredentialValuesOf(t, "make vm-create's output", text, f.env)
		})
	}
}
