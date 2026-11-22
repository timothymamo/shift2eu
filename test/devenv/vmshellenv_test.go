// Pins issue #326's item 2: every host target that runs limactl runs it without the agents App's variables.
//
// The host clone's direnv .envrc exports SHIFT2EU_AGENTS_APP_ID, SHIFT2EU_AGENTS_APP_INSTALLATION_ID and
// SHIFT2EU_AGENTS_APP_KEY_FILE, and make hands its environment to every recipe. Lima forwards no environment of
// its own, so today nothing of it reaches the VM - which is why PR #323 (issue #227 part 1, ADR 0031) took the
// stripping of deck, deck-restart, vm-claude-settings, vm-init and vm-shell as defence in depth, and issue #326 asks
// for the same on every other target that calls limactl. The list is not a list of target names here: the guard over
// the Makefile's rules (makerules_test.go, TestMakefileSetsVMShellEnvForEveryLimactlCall) wants $(VM_SHELL_ENV)
// before every limactl call, and this test runs the targets the issue names to prove that the shell the recipe gets
// really is without them: the fake limactl records which of those names were set in its own environment (names only,
// never a value, CLAUDE.md rule 3). No test reaches a real Lima or holds a real credential.
package devenv_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// agentsAppNames are the three variables the host clone's .envrc exports, the ones no process in the VM needs: the
// deck's agents mint the one-hour token themselves, through the wrapper the VM holds.
var agentsAppNames = []string{
	"SHIFT2EU_AGENTS_APP_ID",
	"SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
	"SHIFT2EU_AGENTS_APP_KEY_FILE",
}

// TestMakeRunsLimactlWithoutTheAgentsAppVariables pins the other half of issue #326's item 2: with the App's three
// variables in make's environment, as the host clone's direnv .envrc sets them, the limactl of vm-env, vm-gh-wrapper,
// vm-up, vm-stop and vm-delete sees none of them, and each of those targets does reach limactl - a target that
// stopped before its limactl call would otherwise pass this test by running nothing.
func TestMakeRunsLimactlWithoutTheAgentsAppVariables(t *testing.T) {
	t.Parallel()

	cases := []struct {
		target  string
		fixture func(t *testing.T) *limaTargetFixture
	}{
		{target: "vm-env", fixture: newLimaVMFixture},
		{target: "vm-gh-wrapper", fixture: newLimaMakeFixture},
		{target: "vm-up", fixture: newLimaVMFixture},
		{target: "vm-stop", fixture: newLimaMakeFixture},
		{target: "vm-delete", fixture: newLimaMakeFixture},
	}

	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()

			f := tc.fixture(t)

			// The three variables with invented values, the way the host clone's .envrc holds them. The key file
			// is a path in the sandbox, never one of the machine's.
			values := map[string]string{
				"SHIFT2EU_AGENTS_APP_ID":              "123456",
				"SHIFT2EU_AGENTS_APP_INSTALLATION_ID": "78901234",
				"SHIFT2EU_AGENTS_APP_KEY_FILE":        filepath.Join(f.sb.hostHome, "agents-app-key.pem"),
			}

			for _, name := range agentsAppNames {
				if values[name] == "" {
					t.Fatalf("%s is one of the App's variables and this test gives it no value: the list and the values have to stay in step", name)
				}

				f.env[name] = values[name]
			}

			// The run's own outcome is not what this test pins: vm-env and vm-gh-wrapper fail here because the
			// fake limactl answers nothing, and vm-stop and vm-delete succeed. The instance travels in make's
			// environment, as the sandbox's baseEnv sets it.
			stdout, stderr, _ := f.runFromEnv(t, tc.target, f.env["LIMA_INSTANCE"])

			log := f.sb.argvLogText(t)

			if len(invocations(log, "limactl")) == 0 {
				t.Fatalf("make %s with the App's variables in its environment ran no limactl at all, so this test pins nothing about what limactl sees: a target that stops before its limactl call has to fail the test, not pass it\nstdout: %s\nstderr: %s",
					tc.target, stdout, stderr)
			}

			for _, name := range envNamesSet(t, log) {
				if !strings.HasPrefix(name, agentsAppPrefix) {
					continue
				}

				t.Errorf("make %s with the App's variables in its environment left %s set in the environment of the limactl it ran: no process in the VM needs it, and the host clone's .envrc holds it only on the Mac (issue #326, ADR 0031)\nstdout: %s\nstderr: %s",
					tc.target, name, stdout, stderr)
			}
		})
	}
}
