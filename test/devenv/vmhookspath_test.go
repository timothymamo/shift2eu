// Pins issue #326's item 1: make vm-up sets the git hooks in the VM on every start.
//
// dev/vm-init.sh sets core.hooksPath on the workspace's main checkout once, when the VM is created. That is one
// run in the VM's life: an agent that unsets the setting keeps the hooks off for every commit and push after it,
// and nothing on the Mac notices, because make vm-up runs no git in the workspace at all (issue #227). So vm-up
// sets it again on every start, inside the VM and on the main checkout the VM mounts - the setting lives in the
// mounted .git/config, so it lasts until an agent unsets it again. The server-side protection of main stays the
// real gate; this is the convenience guard the maintainer expects when they see the hooks in the repository.
//
// The test runs make vm-up in a sandbox clone with the recording fake limactl of the vm-up tests, whose shell
// calls run nothing and record their argv: what it pins is the command vm-up asks the VM to run, never what a
// VM on this machine would do. No test reaches a real Lima (CLAUDE.md rule 3).
//
// The last case of the table pins that the guard is not best effort: when that git command fails in the VM,
// make vm-up exits non-zero, for an instance that is listed and for one it creates, because a run that says it
// succeeded while the hooks are still off leaves the maintainer with the setting the guard exists to restore.
package devenv_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// vmGitHooksArgs returns the argv of every limactl call that runs the git command setting the main checkout's
// hooks, in call order: a limactl shell whose command runs git with config core.hooksPath .githooks on checkout,
// whether it names the checkout with git -C or with limactl's --workdir.
func vmGitHooksArgs(t *testing.T, log, checkout string) [][]string {
	t.Helper()

	var calls [][]string

	for _, args := range invocations(log, "limactl") {
		if len(args) == 0 || args[0] != "shell" {
			continue
		}

		if !holdsEvery(args, "git", "config", "core.hooksPath", ".githooks", checkout) {
			continue
		}

		calls = append(calls, args)
	}

	return calls
}

// holdsEvery reports whether words holds every one of want.
func holdsEvery(words []string, want ...string) bool {
	for _, w := range want {
		if !slices.Contains(words, w) {
			return false
		}
	}

	return true
}

// TestVMUpSetsTheGitHooksInTheVM pins the hooks guard of issue #326: make vm-up, for an instance that is already
// there and for one it creates, runs inside the VM a git command that sets core.hooksPath to .githooks on the
// main checkout the VM mounts, so a setting an agent unset comes back at the next start. The command runs in the
// VM, through limactl, never on the Mac: the hook set lives in the mounted .git/config and the Mac's own git
// configuration is not touched (issue #227).
//
// The cases whose hooksFail is set pin the other half of the guard: that command is the recipe's last, so its
// failure is vm-up's failure. A run that ignored it would report success with the hooks still off.
func TestVMUpSetsTheGitHooksInTheVM(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		instance string // what limactl list --quiet answers; empty means the instance is not listed
		status   string // what the status query answers, unread when the instance is not listed
		limaYaml bool   // the instance directory holds a lima.yaml
		// hooksFail makes the fake limactl fail the shell that runs the git command setting the hooks, as a VM
		// that cannot run git in the checkout does; vm-up must then exit non-zero.
		hooksFail bool
	}{
		{name: "an instance that is stopped", instance: vmUpInstance, status: "Stopped", limaYaml: true},
		{name: "an instance that is running", instance: vmUpInstance, status: "Running", limaYaml: true},
		{name: "an instance that is not listed yet", instance: ""},
		{name: "an instance that is stopped and whose git command fails", instance: vmUpInstance, status: "Stopped", limaYaml: true, hooksFail: true},
		{name: "an instance that is not listed yet and whose git command fails", instance: "", hooksFail: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, tc.instance, tc.status)
			if tc.limaYaml {
				f.writeInstanceLimaYaml(t)
			}

			if tc.hooksFail {
				f.env["SHIFT2EU_TEST_LIMA_HOOKS_FAIL"] = "1"
			}

			stdout, stderr, err := f.run(t, "vm-up")

			checkout := f.env["WORKSPACE_REPO"]

			calls := vmGitHooksArgs(t, f.sb.argvLogText(t), checkout)
			if len(calls) == 0 {
				t.Fatalf("make vm-up %s runs no git command in the VM that sets core.hooksPath to .githooks on %s, so an agent that unsets the hooks keeps them off until the VM is recreated, and every commit and push to main after it goes through with nothing refusing it (issue #326): limactl was asked for%s\nstdout: %s\nstderr: %s",
					tc.name, checkout, describeCalls(invocations(f.sb.argvLogText(t), "limactl")), stdout, stderr)
			}

			if !tc.hooksFail {
				if err != nil {
					t.Fatalf("make vm-up %s: %v\nstdout: %s\nstderr: %s", tc.name, err, stdout, stderr)
				}

				return
			}

			if err == nil {
				t.Fatalf("make vm-up %s exited 0 although the git command setting the hooks failed in the VM: a run that reports success with the hooks still off is the one the guard exists to prevent, and a maintainer has no reason to look again (issue #326)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			// The VM was started and only the hooks call failed: the failure the case pins is that call's, not an
			// earlier one.
			if starting := f.starting(t); len(starting) != 1 {
				t.Errorf("make vm-up %s ran %d limactl calls that start a VM, want exactly one: the start itself must succeed here, so the failure that follows is the git command's (issue #326): limactl was asked for%s\nstdout: %s\nstderr: %s",
					tc.name, len(starting), describeCalls(invocations(f.sb.argvLogText(t), "limactl")), stdout, stderr)
			}

			all := invocations(f.sb.argvLogText(t), "limactl")
			if len(all) == 0 {
				t.Fatalf("make vm-up %s ran no limactl call at all, so this case pins nothing (issue #326)\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			if diff := cmp.Diff(calls[len(calls)-1], all[len(all)-1]); diff != "" {
				t.Errorf("make vm-up %s's last limactl call is not the git command setting the hooks that failed (-want +got):\n%s\nit went on after that command reported failure, and a later call that also fails the run hides which guard is broken (issue #326)",
					tc.name, diff)
			}
		})
	}
}
