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

// vmUpCalls is the whole sequence of limactl calls make vm-up makes, in order, for a case: the existence check, the
// status query of an instance that is listed, the call that starts or creates the VM, and, once that call has
// succeeded, the command that re-sets the git hooks in the VM (issue #326). A case whose start fails runs nothing
// after the call that failed, the hooks command least of all: a call that followed it would hide which guard
// broke. Listed and created differ only in the call that starts the VM, as they do in the Makefile.
func vmUpCalls(f *vmUpFixture, listed, startFails bool) [][]string {
	instance := f.env["LIMA_INSTANCE"]

	var calls [][]string

	if listed {
		calls = [][]string{
			{"list", "--quiet"},
			{"list", "--format", "{{.Status}}", instance},
			f.startExistingArgs(),
		}
	} else {
		calls = [][]string{
			{"list", "--quiet"},
			{
				"start", "--name=" + instance, "--mount-only", f.workspace() + ":w",
				"--tty=false", "dev/lima.yaml",
			},
		}
	}

	if startFails {
		return calls
	}

	return append(calls, vmUpHooksPathCall(f))
}

// vmUpHooksPathCall is the last call make vm-up makes once the instance runs, whatever started it: from inside the
// VM, it sets the checkout WORKSPACE_REPO names to use the repository's git hooks, where they refuse a commit or a
// push to main (issue #326).
func vmUpHooksPathCall(f *vmUpFixture) []string {
	return []string{
		"shell", "--workdir", "/", f.env["LIMA_INSTANCE"], "git", "-C", f.env["WORKSPACE_REPO"],
		"config", "core.hooksPath", ".githooks",
	}
}

// TestVMUpSetsTheGitHooksInTheVM pins the hooks guard of issue #326: make vm-up, for an instance that is already
// there and for one it creates, runs inside the VM a git command that sets core.hooksPath to .githooks on the
// main checkout the VM mounts, so a setting an agent unset comes back at the next start. The command runs in the
// VM, through limactl, never on the Mac: the hook set lives in the mounted .git/config and the Mac's own git
// configuration is not touched (issue #227).
//
// Every case diffs the whole limactl sequence, not only the hooks call: an existence check after the start, a
// status query for an instance nothing lists, or a call of another kind is a recipe that broke its order rather
// than its guard, and nothing else in this package would see it.
//
// The cases whose hooksFail is set pin the other half of the guard: that command is the recipe's last, so its
// failure is vm-up's failure. A run that ignored it would report success with the hooks still off.
//
// The cases whose startFail is set pin that the hooks command is not run over a VM that never came up: the call
// that starts the VM fails, so vm-up must exit non-zero with no limactl call after it - a hooks call before the
// instance runs would be a command no VM answered.
func TestVMUpSetsTheGitHooksInTheVM(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		instance string // what limactl list --quiet answers; empty means the instance is not listed
		status   string // what the status query answers, unread when the instance is not listed
		limaYaml bool   // the instance directory holds a lima.yaml
		// startFail makes the fake limactl fail the call that starts the VM - the shell --start of a listed instance,
		// the start of one it creates - as a VM that cannot boot does; vm-up must then exit non-zero and run nothing
		// after that call.
		startFail bool
		// hooksFail makes the fake limactl fail the shell that runs the git command setting the hooks, as a VM
		// that cannot run git in the checkout does; vm-up must then exit non-zero.
		hooksFail bool
	}{
		{name: "an instance that is stopped", instance: vmUpInstance, status: "Stopped", limaYaml: true},
		{name: "an instance that is running", instance: vmUpInstance, status: "Running", limaYaml: true},
		{name: "an instance that is not listed yet", instance: ""},
		{name: "an instance that is stopped and whose start fails", instance: vmUpInstance, status: "Stopped", limaYaml: true, startFail: true},
		{name: "an instance that is not listed yet and whose start fails", instance: "", startFail: true},
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

			if tc.startFail {
				f.env["SHIFT2EU_TEST_LIMA_START_FAIL"] = "1"
			}

			stdout, stderr, err := f.run(t, "vm-up")

			if diff := cmp.Diff(vmUpCalls(f, tc.instance != "", tc.startFail), invocations(f.sb.argvLogText(t), "limactl")); diff != "" {
				t.Errorf("make vm-up %s ran the wrong limactl calls, or ran them in another order (-want +got):\n%s\nstdout: %s\nstderr: %s",
					tc.name, diff, stdout, stderr)
			}

			checkout := f.env["WORKSPACE_REPO"]

			calls := vmGitHooksArgs(t, f.sb.argvLogText(t), checkout)

			if tc.startFail {
				if err == nil {
					t.Errorf("make vm-up %s exited 0 although the call that starts the VM failed: a run that reports success over a VM that never came up takes a broken environment for a working one (issue #326)\nstdout: %s\nstderr: %s",
						tc.name, stdout, stderr)
				}

				if len(calls) != 0 {
					t.Errorf("make vm-up %s ran the git command setting the hooks in the VM %d time(s) although the start failed: nothing may follow the call that failed, or the run's status stops being that call's (issue #326): limactl was asked for%s\nstdout: %s\nstderr: %s",
						tc.name, len(calls), describeCalls(invocations(f.sb.argvLogText(t), "limactl")), stdout, stderr)
				}

				return
			}

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
