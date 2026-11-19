// The LIMA_INSTANCE check of every Makefile target that reads it (issue #252).
//
// make vm-up must refuse a LIMA_INSTANCE that is not letters, digits, '.', '_' and '-', starting with a
// letter or digit, before it calls limactl at all: an empty name matches the empty line an empty listing
// prints, a leading dash reads as an option, a slash as a path. Every other target that reads LIMA_INSTANCE
// (vm-shell, vm-env, vm-claude-settings, vm-init, vm-gh-wrapper, deck, deck-restart, vm-stop, vm-delete and
// the agents-token* targets) must refuse it the same way, each in its own first recipe line and through the
// shared check: some of them read the value in make-level $(...) expansions as well, so a value that is not
// refused first is a value that runs its command or reaches limactl as an option. Issue #252 found the
// targets that interpolated it unchecked; these tests keep every one of them refusing.
//
// These tests run make against the repository's own Makefile, in a clone inside a sandbox, with fakes that
// record every command they are asked to run and run nothing: an invalid value must stop the target before
// any of those is reached. The refusal is pinned on two substrings only - "invalid LIMA_INSTANCE" and the
// rule itself - so one shared check may serve every target and the target's own prefix stays free. A value
// reaches make on its command line, the way a maintainer types it, or in make's environment alone, the way
// direnv exports it on the host: both must be refused, and both are pinned.
//
// make vm-up has refused the plain invalid names since issue #237, and make vm-create does too, in its own
// first recipe line and so before the vm-env-check sub-make (issues #252, #258). Neither may pass the values
// that carry a command: make expands them in its own $(...) expansions before any recipe sees them, so the
// raw value has to be what is checked. Nor may any target pass a name holding a control character, whatever
// the host's make does with whitespace: a space, a tab, a newline, a carriage return, a vertical tab and a
// form feed are all refused (issue #252 review). vm-env-check is not in the list: it reads no LIMA_INSTANCE
// and calls no limactl.
package devenv_test

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// limaInstanceRule is the sentence every refusal has to carry, whichever target it comes from: the wording
// make vm-up has used since issue #237. The check may name the target in front of "invalid LIMA_INSTANCE",
// but the rule it states is the same one everywhere.
const limaInstanceRule = "a Lima instance name is letters, digits, '.', '_' and '-', starting with a letter or digit"

// limaInstanceMarker is the file a value that reaches a shell, or that make expands itself, creates: the
// recipes that interpolate LIMA_INSTANCE in double quotes make $(touch ...) a command substitution, and a
// value make expands runs it while the expansion stands.
const limaInstanceMarker = "PWNED"

// invalidLimaInstances are the values no target may accept, each named for what it is. Three of them carry
// a command on purpose: LIMA_INSTANCE is spliced into shell text and some recipes read it in make's own
// $(...) expansions, so a value that is not validated before either is a value that runs its command. The
// control characters are there for the same reason: whitespace decides whether make counts a value as one
// word, and $(if) strips more of it than $(words) splits on in some makes, so a name holding one must be
// refused by the rule itself, never by what the host's make happens to do with it.
var invalidLimaInstances = []struct {
	name  string
	value string
}{
	{name: "an empty name", value: ""},
	{name: "a name that reads as an option", value: "-rf"},
	{name: "a name that asks for help", value: "--help"},
	{name: "a name with a space", value: "a b"},
	{name: "a name with a tab", value: "a\tb"},
	{name: "a name with a newline", value: "a\nb"},
	{name: "a name with a carriage return", value: "a\rb"},
	{name: "a name with a vertical tab", value: "a\vb"},
	{name: "a name with a form feed", value: "a\fb"},
	{name: "a name with a single quote", value: "x'y"},
	{name: "a name with a slash", value: "a/b"},
	{name: "a name with a leading dot", value: ".hidden"},
	{name: "a name make expands to the empty string", value: "x$(touch " + limaInstanceMarker + ")"},
	{name: "a name the shell would run", value: "x$$(touch " + limaInstanceMarker + ")"},
	{name: "a name make would run", value: "x$(shell touch " + limaInstanceMarker + ")"},
}

// TestMakeRefusesAnInvalidLimaInstanceOnEveryTarget pins issue #252: every target that reads LIMA_INSTANCE
// refuses a value that is not an instance name before it runs anything. The run exits non-zero, says
// "invalid LIMA_INSTANCE" and the rule on stderr, records no command in the sandbox's argv log (limactl, and
// the launchctl, sudo and scripts of the token targets too) and creates no marker file, so the value never
// reached a shell and no limactl read it as an option, a path or nothing at all. The value reaches make on
// its command line, which is how a maintainer types it; vm-stop and agents-token-refresh-install are run a
// second time with it in make's environment alone, which is how direnv exports it on the host, because the
// host path must not be the one that skips the check (issue #252 review).
func TestMakeRefusesAnInvalidLimaInstanceOnEveryTarget(t *testing.T) {
	t.Parallel()

	targets := []struct {
		name    string
		target  string
		fixture func(t *testing.T) *limaTargetFixture
		fromEnv bool // also run every invalid value with LIMA_INSTANCE in make's environment alone
	}{
		{name: "vm-up", target: "vm-up", fixture: newLimaVMFixture},
		// vm-create refuses an invalid name in its own first recipe line, before the vm-env-check sub-make
		// and so before any limactl call (issues #252, #258).
		{name: "vm-create", target: "vm-create", fixture: newLimaVMFixture},
		{name: "vm-env", target: "vm-env", fixture: newLimaVMFixture},
		{name: "vm-shell", target: "vm-shell", fixture: newLimaMakeFixture},
		{name: "vm-claude-settings", target: "vm-claude-settings", fixture: newLimaMakeFixture},
		{name: "vm-init", target: "vm-init", fixture: newLimaMakeFixture},
		{name: "vm-gh-wrapper", target: "vm-gh-wrapper", fixture: newLimaMakeFixture},
		{name: "deck", target: "deck", fixture: newLimaMakeFixture},
		{name: "deck-restart", target: "deck-restart", fixture: newLimaMakeFixture},
		{name: "vm-stop", target: "vm-stop", fixture: newLimaMakeFixture, fromEnv: true},
		{name: "vm-delete", target: "vm-delete", fixture: newLimaMakeFixture},
		{name: "agents-token", target: "agents-token", fixture: newLimaMintingTokenFixture},
		// The refresh runs in the foreground, so the fixture's fake GitHub refuses the mint: a run that is
		// not refused first stops on that refusal instead of minting until the test ends.
		{name: "agents-token-refresh", target: "agents-token-refresh", fixture: newLimaRefusedMintTokenFixture},
		{name: "agents-token-refresh-start", target: "agents-token-refresh-start", fixture: newLimaMintingTokenFixture},
		// The install is the second target run from make's environment too: it is the one that runs sudo and
		// launchctl, so the host path has to reach the same refusal as the command-line one.
		{name: "agents-token-refresh-install", target: "agents-token-refresh-install", fixture: newLimaLaunchdFixture, fromEnv: true},
		{name: "agents-token-refresh-uninstall", target: "agents-token-refresh-uninstall", fixture: newLimaLaunchdFixture},
	}

	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := tc.fixture(t)

			deliveries := []limaInstanceDelivery{onTheCommandLine}
			if tc.fromEnv {
				deliveries = append(deliveries, inTheEnvironment)
			}

			for _, delivery := range deliveries {
				// The values share the fixture and run one after the other: each of them has to stop before
				// anything runs, so none of them leaves state that another could trip over.
				for _, bad := range invalidLimaInstances {
					assertLimaInstanceRefused(t, f, tc.target, delivery, bad.name, bad.value)
				}
			}
		})
	}
}

// TestMakeRunsLimactlForAValidLimaInstance pins the other half of issue #252: a name of the rule's own shape
// still reaches limactl, unchanged, on a cheap target that runs nothing else. The check must refuse the
// values no instance can carry, not the ones it can: '.' and '_' are part of a name.
func TestMakeRunsLimactlForAValidLimaInstance(t *testing.T) {
	t.Parallel()

	const instance = "shift2eu-test_1.2"

	cases := []struct {
		name     string
		target   string
		delivery limaInstanceDelivery
		want     []string
	}{
		{name: "vm-stop on the command line", target: "vm-stop", delivery: onTheCommandLine, want: []string{"stop", instance}},
		{name: "vm-stop from the environment", target: "vm-stop", delivery: inTheEnvironment, want: []string{"stop", instance}},
		{name: "vm-delete on the command line", target: "vm-delete", delivery: onTheCommandLine, want: []string{"delete", "--force", instance}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLimaMakeFixture(t)

			stdout, stderr, err := tc.delivery.run(t, f, tc.target, instance)
			if err != nil {
				t.Fatalf("make %s with LIMA_INSTANCE %s set to %q: %v\nstdout: %s\nstderr: %s", tc.target, tc.delivery.how, instance, err, stdout, stderr)
			}

			if diff := cmp.Diff([][]string{tc.want}, invocations(f.sb.argvLogText(t), "limactl")); diff != "" {
				t.Errorf("make %s with LIMA_INSTANCE %s set to %q ran the wrong limactl calls (-want +got):\n%s\nan instance name the rule allows must still reach limactl, unchanged (issue #252)", tc.target, tc.delivery.how, instance, diff)
			}
		})
	}
}

// limaInstanceDelivery is how a test hands an instance to make: as a command-line assignment, which is how a
// maintainer types it, or in make's environment alone, which is how direnv exports it on the host. Both paths
// must reach the same check, so both are pinned.
type limaInstanceDelivery struct {
	// how names the delivery in a failure message, after "LIMA_INSTANCE set".
	how string
	run func(t *testing.T, f *limaTargetFixture, target, instance string) (string, string, error)
}

var (
	onTheCommandLine = limaInstanceDelivery{
		how: "on make's command line",
		run: func(t *testing.T, f *limaTargetFixture, target, instance string) (string, string, error) {
			t.Helper()

			return f.run(t, target, instance)
		},
	}
	inTheEnvironment = limaInstanceDelivery{
		how: "in make's environment",
		run: func(t *testing.T, f *limaTargetFixture, target, instance string) (string, string, error) {
			t.Helper()

			return f.runFromEnv(t, target, instance)
		},
	}
)

// assertLimaInstanceRefused fails unless make refused instance for target, naming the run by value, what the
// value stands for and how it reached make: a non-zero exit, "invalid LIMA_INSTANCE" and the rule on stderr,
// no command in the sandbox's argv log, and no marker file in the directory the recipe ran in.
func assertLimaInstanceRefused(t *testing.T, f *limaTargetFixture, target string, delivery limaInstanceDelivery, name, instance string) {
	t.Helper()

	f.clearMarker(t)

	// The values of one target share its fixture, so only what this run appended to the argv log is
	// this run's: the values before it may have recorded the commands they let through.
	before := f.sb.argvLogText(t)

	stdout, stderr, err := delivery.run(t, f, target, instance)
	if err == nil {
		t.Errorf("make %s with LIMA_INSTANCE set %s to %s (%q) exited 0: an invalid instance name must stop the target before it runs anything, or limactl reads it as an option, a path or nothing at all, and the name that is used is not the one LIMA_INSTANCE asks for (issue #252)\nstdout: %s\nstderr: %s",
			target, delivery.how, name, instance, stdout, stderr)
	}

	for _, want := range []string{"invalid LIMA_INSTANCE", limaInstanceRule} {
		if !strings.Contains(stderr, want) {
			t.Errorf("make %s with LIMA_INSTANCE set %s to %s (%q) does not say %q on stderr, so a maintainer cannot tell which variable to fix nor what a Lima instance name may hold (issue #252)\nstdout: %s\nstderr: %s",
				target, delivery.how, name, instance, want, stdout, stderr)
		}
	}

	if after := f.sb.argvLogText(t); after != before {
		t.Errorf("make %s with LIMA_INSTANCE set %s to %s (%q) ran a command although it must refuse the name first: every command the recipe runs goes through a fake that records it, and this run recorded these (issue #252)\n%s",
			target, delivery.how, name, instance, strings.TrimPrefix(after, before))
	}

	if _, statErr := os.Stat(f.marker()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("make %s with LIMA_INSTANCE set %s to %s (%q) created %s: the value reached a shell or a make expansion before it was checked, so a name that carries a command runs it (issue #252) (stat: %v)",
			target, delivery.how, name, instance, f.marker(), statErr)
	}
}

// limaTargetFixture is one environment for a run of one target that reads LIMA_INSTANCE: where make runs, the
// environment it runs with, the arguments the target needs besides the instance, and the sandbox whose argv
// log records every command the recipe ran.
type limaTargetFixture struct {
	sb   *sandbox
	env  map[string]string
	dir  string   // where make runs: a clone of the Makefile inside the sandbox
	args []string // make arguments besides the target, such as a fixture's AGENTS_TOKEN_LAUNCHD_DIR
}

// run runs target with instance on make's command line, the way a maintainer passes it, under the fixture's
// environment alone.
func (f *limaTargetFixture) run(t *testing.T, target, instance string) (string, string, error) {
	t.Helper()

	args := append(slices.Clone(f.args), "LIMA_INSTANCE="+instance, target)

	return runMakeIn(t, f.dir, args, f.env)
}

// runFromEnv runs target with instance in make's environment alone, the way direnv exports it on the host,
// and no command-line assignment, so make reads the value it inherits. make runs with the fixture's
// environment and never the test process's own (runMakeIn, envList), so the value set here is the only
// LIMA_INSTANCE it sees: nothing of this machine's can stand in for it.
func (f *limaTargetFixture) runFromEnv(t *testing.T, target, instance string) (string, string, error) {
	t.Helper()

	env := make(map[string]string, len(f.env)+1)
	for name, value := range f.env {
		env[name] = value
	}

	env["LIMA_INSTANCE"] = instance

	args := append(slices.Clone(f.args), target)

	return runMakeIn(t, f.dir, args, env)
}

// marker is the file a value that reached a shell, or that make expanded, would create: the recipe runs in
// dir, so a substitution it makes lands here.
func (f *limaTargetFixture) marker() string {
	return filepath.Join(f.dir, limaInstanceMarker)
}

// clearMarker removes the marker an earlier value of the same target left behind, so the next one is charged
// only with what its own run created.
func (f *limaTargetFixture) clearMarker(t *testing.T) {
	t.Helper()

	if err := os.Remove(f.marker()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("remove %s left by an earlier value: %v", f.marker(), err)
	}
}

// recordingLimactl stands in for limactl in these tests: it records its argv and exits 0 without running
// anything, so a test pins which command lines a recipe runs, never what a VM on this machine would do.
const recordingLimactl = `#!/bin/sh
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
exit 0
`

// newLimaMakeFixture builds one environment for a target that reads LIMA_INSTANCE and needs nothing but
// limactl to run: a clone of the Makefile and the two scripts make vm-gh-wrapper generates its work from
// inside the sandbox, the recording limactl first on PATH, and no credential of this machine in the
// environment. The clone is the host clone the target must run from (issue #227): it lies outside the sandbox's
// workspace, whose checkout WORKSPACE_REPO names.
func newLimaMakeFixture(t *testing.T) *limaTargetFixture {
	t.Helper()

	sb := newSandbox(t)
	writeFileMode(t, filepath.Join(sb.binDir, "limactl"), []byte(recordingLimactl), 0o700)

	dir := filepath.Join(sb.root, "clone")
	writeFileMode(t, filepath.Join(dir, makefilePath), []byte(readRepoFile(t, makefilePath)), 0o600)
	writeFileMode(t, filepath.Join(dir, vmGHWrapperScript), []byte(readRepoFile(t, vmGHWrapperScript)), 0o755)
	writeFileMode(t, filepath.Join(dir, ghWrapperScript), []byte(readRepoFile(t, ghWrapperScript)), 0o755)

	return &limaTargetFixture{sb: sb, env: sb.baseEnv(), dir: dir}
}

// newLimaVMFixture builds the fixture the vm-up tests use: the throwaway clone in its own git repository,
// the fake limactl that answers a listing and a status, and the fake VM_ENV credentials the prerequisite
// vm-env-check of vm-env and vm-create needs to pass.
func newLimaVMFixture(t *testing.T) *limaTargetFixture {
	t.Helper()

	f := newVMUpFixture(t, "", "Stopped")

	return &limaTargetFixture{sb: f.sb, env: f.env, dir: f.root}
}

// newLimaMintingTokenFixture builds the token fixture with a fake GitHub that mints.
func newLimaMintingTokenFixture(t *testing.T) *limaTargetFixture {
	t.Helper()

	return newLimaTokenFixture(t, 0)
}

// newLimaRefusedMintTokenFixture builds the token fixture with a fake GitHub that refuses every mint as a
// permanent failure (401): a refresh that is not refused first stops on it instead of minting in a loop
// until the test ends.
func newLimaRefusedMintTokenFixture(t *testing.T) *limaTargetFixture {
	t.Helper()

	return newLimaTokenFixture(t, http.StatusUnauthorized)
}

// newLimaTokenFixture builds one environment for the agents-token targets: the sandbox's token fixture with
// the invented App key, the fake GitHub API and its state directory, run in a clone of the Makefile beside
// the scripts those targets run, so a value that reached a shell creates its marker inside the sandbox and
// nothing of the repository is touched.
func newLimaTokenFixture(t *testing.T, mintStatus int) *limaTargetFixture {
	t.Helper()

	f := newTokenFixture(t, "shift2eu")
	f.api.refuseMintsWith(mintStatus)

	dir := filepath.Join(f.sb.root, "clone")
	launchdClone(t, dir)

	return &limaTargetFixture{sb: f.sb, env: f.env, dir: dir}
}

// newLimaLaunchdFixture builds one environment for agents-token-refresh-install and -uninstall: the launchd
// fixture with the fake launchctl, plutil, log-only sudo and limactl, the invented App key and the LaunchDaemons
// directory inside the sandbox, run in a clone inside that sandbox.
func newLimaLaunchdFixture(t *testing.T) *limaTargetFixture {
	t.Helper()

	f := newLaunchdInstallFixture(t, "shift2eu", inAClone(func(fixture *launchdInstallFixture) string {
		return filepath.Join(fixture.sb.root, "clone")
	}))

	return &limaTargetFixture{
		sb:   f.sb,
		env:  f.env,
		dir:  f.dir,
		args: []string{"AGENTS_TOKEN_LAUNCHD_DIR=" + f.launchdDir},
	}
}
