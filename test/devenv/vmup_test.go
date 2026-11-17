// Pins make vm-up's optional VM_CPUS, VM_MEMORY, VM_DISK and VM_SSH_PORT (issue #228): on creation they reach
// limactl start in Lima's own flag form, unset the creation command line is byte for byte what it was, an
// invalid value fails before limactl runs, make vm-create hands the four on to make vm-up, and an existing VM -
// Stopped or Running - is started by its name without any of them, with a warning that they apply only when
// the VM is created. Every run uses a fake limactl on PATH: no test starts a real VM.
package devenv_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The four variables make vm-up takes (issue #228).
const (
	vmCPUsVar    = "VM_CPUS"
	vmMemoryVar  = "VM_MEMORY"
	vmDiskVar    = "VM_DISK"
	vmSSHPortVar = "VM_SSH_PORT"
)

// vmUpLimactl stands in for limactl in the vm-up tests: it records its argv in the sandbox's log, answers
// limactl list with the instances SHIFT2EU_TEST_VM_INSTANCES names, and answers limactl start with success. It
// creates and starts nothing. The shared fake in sandbox_test.go runs a command inside a VM, which vm-up never
// asks for, so these tests write their own.
const vmUpLimactl = `#!/bin/sh
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
# make vm-env, make vm-claude-settings and make vm-init pipe their work into limactl shell: drain it, so the
# writer never meets a closed pipe.
if [ ! -t 0 ]; then cat >/dev/null 2>&1 || true; fi
if [ "$1" = list ]; then
  # An instance that exists, whatever its status: vm-up only asks for the names.
  if [ -n "${SHIFT2EU_TEST_VM_INSTANCES:-}" ]; then printf '%s\n' "${SHIFT2EU_TEST_VM_INSTANCES}"; fi
  exit 0
fi
exit 0
`

// vmUpGitShim is the git make's recipes run in the vm-up tests: it answers git config core.hooksPath, the write
// make vm-up's git-hooks prerequisite makes into the checkout's .git/config, without touching a file, and hands
// every other call to the real git. That config is shared by every worktree, so tests that run make in parallel
// would each wait on the other's lock, and no test writes into the repository it runs from.
const vmUpGitShim = `#!/bin/sh
for a in "$@"; do
  if [ "$a" = core.hooksPath ]; then exit 0; fi
done
exec %s "$@"
`

// vmUpFixture is one environment for a run of make vm-up or make vm-create: a sandbox whose limactl records its
// argv, and a workspace of the sandbox's own, which holds only the repository's name, so vm-up's check for
// stray files passes whatever the machine the tests run on holds and the VM would mount a path inside the
// sandbox.
type vmUpFixture struct {
	sb        *sandbox
	env       map[string]string
	repoRoot  string // the stand-in REPO_ROOT, <workspace>/shift2eu
	workspace string // the stand-in WORKSPACE the VM mounts
	marker    string // a file no run may create: what a value the Makefile got wrong would run
}

// newVMUpFixture builds the fixture for one instance, which exists when instanceExists is set. The instance is
// the sandbox's LIMA_INSTANCE.
func newVMUpFixture(t *testing.T, instanceExists bool) *vmUpFixture {
	t.Helper()

	sb := newSandbox(t)
	fake := filepath.Join(sb.binDir, "limactl")

	//nolint:gosec // the fake limactl is a script the tests run, so it must be executable
	if err := os.WriteFile(fake, []byte(vmUpLimactl), 0o700); err != nil {
		t.Fatalf("write %s: %v", fake, err)
	}

	git := filepath.Join(sb.binDir, "git")

	//nolint:gosec // the git shim is a script the tests run, so it must be executable
	if err := os.WriteFile(git, []byte(fmt.Sprintf(vmUpGitShim, strconv.Quote(realTool(t, "git")))), 0o700); err != nil {
		t.Fatalf("write %s: %v", git, err)
	}

	workspace := filepath.Join(sb.root, "workspace")
	repoRoot := filepath.Join(workspace, "shift2eu")

	if err := os.MkdirAll(repoRoot, 0o750); err != nil {
		t.Fatalf("create %s: %v", repoRoot, err)
	}

	env := sb.baseEnv()
	env["SHIFT2EU_TEST_VM_INSTANCES"] = ""

	if instanceExists {
		env["SHIFT2EU_TEST_VM_INSTANCES"] = env["LIMA_INSTANCE"]
	}

	return &vmUpFixture{
		sb:        sb,
		env:       env,
		repoRoot:  repoRoot,
		workspace: workspace,
		marker:    filepath.Join(sb.root, "marker-a-value-ran"),
	}
}

// run runs make with args in the repository's root, with the fixture's stand-in workspace as the one the VM
// would mount.
func (f *vmUpFixture) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	return runMake(t, append(args, "REPO_ROOT="+f.repoRoot), f.env)
}

// starts is the argv of every limactl start a run made.
func (f *vmUpFixture) starts(t *testing.T) [][]string {
	t.Helper()

	var starts [][]string

	for _, args := range invocations(f.sb.argvLogText(t), "limactl") {
		if len(args) > 0 && args[0] == "start" {
			starts = append(starts, args)
		}
	}

	return starts
}

// creating is the argv of the one limactl start that creates the VM from dev/lima.yaml.
func (f *vmUpFixture) creating(t *testing.T) []string {
	t.Helper()

	var creating [][]string

	for _, args := range f.starts(t) {
		if slices.Contains(args, "dev/lima.yaml") {
			creating = append(creating, args)
		}
	}

	if len(creating) != 1 {
		t.Fatalf("make ran %d limactl start call(s) creating the VM from dev/lima.yaml, want exactly one; the start calls were:%s",
			len(creating), describeCalls(f.starts(t)))
	}

	return creating[0]
}

// creationArgs is the argv limactl start creates the VM with, without issue #228's four flags: today's command
// line, byte for byte.
func (f *vmUpFixture) creationArgs() []string {
	return []string{
		"start",
		"--name=" + f.env["LIMA_INSTANCE"],
		"--mount-only", f.workspace + ":w",
		"--tty=false",
		"dev/lima.yaml",
	}
}

// creationWith is creationArgs with flags added after the existing creation arguments, the template staying the
// last argument, the way limactl documents its start flags.
func (f *vmUpFixture) creationWith(flags []string) []string {
	args := f.creationArgs()
	want := slices.Clone(args[:len(args)-1])
	want = append(want, flags...)

	return append(want, args[len(args)-1])
}

// describeCalls renders start calls for a failure message.
func describeCalls(calls [][]string) string {
	var b strings.Builder

	for _, args := range calls {
		b.WriteString("\n  limactl " + strings.Join(args, " "))
	}

	return b.String()
}

// TestMakeVMUpPassesTheVMResourcesToLimactlStart pins issue #228's first two acceptances: a VM that does not
// exist yet is created with exactly the four flags the set variables name, and with no variable set the
// creation command line is the one the Makefile ran before the variables existed.
func TestMakeVMUpPassesTheVMResourcesToLimactlStart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		vars []string // the make arguments that set the variables
		want []string // the flags they add, in this order
	}{
		{
			name: "none set",
			vars: nil,
			want: nil,
		},
		{
			name: "VM_CPUS alone",
			vars: []string{"VM_CPUS=6"},
			want: []string{"--cpus=6"},
		},
		{
			name: "VM_MEMORY alone",
			vars: []string{"VM_MEMORY=8GiB"},
			want: []string{"--memory=8"},
		},
		{
			name: "VM_DISK alone",
			vars: []string{"VM_DISK=60GiB"},
			want: []string{"--disk=60"},
		},
		{
			name: "VM_SSH_PORT alone",
			vars: []string{"VM_SSH_PORT=60023"},
			want: []string{"--ssh-port=60023"},
		},
		{
			name: "all four",
			vars: []string{"VM_CPUS=6", "VM_MEMORY=8GiB", "VM_DISK=60GiB", "VM_SSH_PORT=60023"},
			want: []string{"--cpus=6", "--memory=8", "--disk=60", "--ssh-port=60023"},
		},
		{
			name: "the SSH port at its lowest",
			vars: []string{"VM_SSH_PORT=1024"},
			want: []string{"--ssh-port=1024"},
		},
		{
			name: "the SSH port at its highest",
			vars: []string{"VM_SSH_PORT=65535"},
			want: []string{"--ssh-port=65535"},
		},
		{
			name: "the smallest CPU count and disk",
			vars: []string{"VM_CPUS=1", "VM_DISK=1GiB"},
			want: []string{"--cpus=1", "--disk=1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, false)

			stdout, stderr, err := f.run(t, append([]string{"vm-up"}, tc.vars...)...)
			if err != nil {
				t.Fatalf("make vm-up %s: %v\nstdout: %s\nstderr: %s", strings.Join(tc.vars, " "), err, stdout, stderr)
			}

			want := f.creationWith(tc.want)
			got := f.creating(t)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("make vm-up %s creates the VM with the wrong limactl start argv (issue #228): the existing creation arguments stay as they are, the flags of the set variables follow them, and the template stays the last argument (-want +got):\n%s", strings.Join(tc.vars, " "), diff)
			}
		})
	}
}

// TestMakeVMUpRefusesAnInvalidVMResource pins issue #228's validation: a value that is not a positive whole
// number, not a whole GiB size or not a port Lima can forward ends make vm-up with an error that names the
// variable, before limactl runs at all - and so before anything a value might carry could run as a shell
// command.
func TestMakeVMUpRefusesAnInvalidVMResource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		variable string
		value    string // {marker} is replaced with the fixture's marker path
	}{
		// VM_CPUS is a CPU count: a positive whole number.
		{name: "VM_CPUS 0", variable: vmCPUsVar, value: "0"},
		{name: "VM_CPUS -1", variable: vmCPUsVar, value: "-1"},
		{name: "VM_CPUS abc", variable: vmCPUsVar, value: "abc"},
		{name: "VM_CPUS 4.5", variable: vmCPUsVar, value: "4.5"},
		{name: "VM_CPUS a command after a semicolon", variable: vmCPUsVar, value: "4; touch {marker}"},
		{name: "VM_CPUS a command substitution", variable: vmCPUsVar, value: "$(touch {marker})"},
		{name: "VM_CPUS a backtick command", variable: vmCPUsVar, value: "`touch {marker}`"},
		// VM_MEMORY and VM_DISK are a size in GiB: a positive whole number and the GiB suffix.
		{name: "VM_MEMORY without its unit", variable: vmMemoryVar, value: "8"},
		{name: "VM_MEMORY in GB", variable: vmMemoryVar, value: "8GB"},
		{name: "VM_MEMORY in G", variable: vmMemoryVar, value: "8G"},
		{name: "VM_MEMORY 0GiB", variable: vmMemoryVar, value: "0GiB"},
		{name: "VM_MEMORY abcGiB", variable: vmMemoryVar, value: "abcGiB"},
		{name: "VM_MEMORY a command after a semicolon", variable: vmMemoryVar, value: "8GiB; touch {marker}"},
		{name: "VM_DISK without its unit", variable: vmDiskVar, value: "8"},
		{name: "VM_DISK in GB", variable: vmDiskVar, value: "8GB"},
		{name: "VM_DISK in G", variable: vmDiskVar, value: "8G"},
		{name: "VM_DISK 0GiB", variable: vmDiskVar, value: "0GiB"},
		{name: "VM_DISK abcGiB", variable: vmDiskVar, value: "abcGiB"},
		{name: "VM_DISK a command after a semicolon", variable: vmDiskVar, value: "8GiB; touch {marker}"},
		// VM_SSH_PORT is a port Lima can forward: 1024 to 65535.
		{name: "VM_SSH_PORT 1023", variable: vmSSHPortVar, value: "1023"},
		{name: "VM_SSH_PORT 65536", variable: vmSSHPortVar, value: "65536"},
		{name: "VM_SSH_PORT 0", variable: vmSSHPortVar, value: "0"},
		{name: "VM_SSH_PORT abc", variable: vmSSHPortVar, value: "abc"},
		{name: "VM_SSH_PORT with a space", variable: vmSSHPortVar, value: "600 22"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, false)
			value := strings.ReplaceAll(tc.value, "{marker}", f.marker)

			stdout, stderr, err := f.run(t, "vm-up", tc.variable+"="+value)
			if err == nil {
				t.Fatalf("make vm-up %s=%q exited 0: vm-up must refuse a value that is not a CPU count, a whole GiB size or a port in 1024-65535, so limactl never sees it (issue #228)\nstdout: %s\nstderr: %s", tc.variable, value, stdout, stderr)
			}

			if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) != 0 {
				t.Errorf("make vm-up %s=%q called limactl before it refused the value: validation must come first, so no value can reach limactl or a shell (issue #228):%s",
					tc.variable, value, describeCalls(calls))
			}

			if _, err := os.Stat(f.marker); err == nil {
				t.Errorf("make vm-up %s=%q created %s: a value the Makefile refuses must never be run by a shell (issue #228)", tc.variable, value, f.marker)
			}

			if combined := stdout + stderr; !strings.Contains(combined, tc.variable) {
				t.Errorf("the refusal of %s=%q does not name %s, so a maintainer cannot tell which variable to fix (issue #228):\nstdout: %s\nstderr: %s",
					tc.variable, value, tc.variable, stdout, stderr)
			}
		})
	}
}

// TestMakeVMCreatePassesTheVMResourcesToVMUp pins issue #228's pass-through: make vm-create hands the four
// variables to the make vm-up it runs, so one command still creates the VM at the size the host needs.
func TestMakeVMCreatePassesTheVMResourcesToVMUp(t *testing.T) {
	t.Parallel()

	f := newVMUpFixture(t, false)

	vars := []string{"VM_CPUS=5", "VM_MEMORY=7GiB", "VM_DISK=58GiB", "VM_SSH_PORT=60022"}
	stdout, stderr, err := f.run(t, append([]string{"vm-create"}, vars...)...)
	if err != nil {
		t.Fatalf("make vm-create %s: %v\nstdout: %s\nstderr: %s", strings.Join(vars, " "), err, stdout, stderr)
	}

	want := f.creationWith([]string{"--cpus=5", "--memory=7", "--disk=58", "--ssh-port=60022"})
	got := f.creating(t)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("make vm-create %s does not hand the four variables to its make vm-up, so the VM is created at the template's size instead (issue #228) (-want +got):\n%s",
			strings.Join(vars, " "), diff)
	}
}

// TestMakeVMUpNeverResizesAnExistingVM pins issue #228's third acceptance: with the four variables set, an
// instance that already exists is started by its name alone - never resized - and vm-up says that they apply
// only when the VM is created. A value that is invalid still fails first, so the variables are checked before
// vm-up looks at the instance at all.
func TestMakeVMUpNeverResizesAnExistingVM(t *testing.T) {
	t.Parallel()

	vars := []string{"VM_CPUS=6", "VM_MEMORY=8GiB", "VM_DISK=60GiB", "VM_SSH_PORT=60023"}

	t.Run("the instance is started by name without the four flags", func(t *testing.T) {
		t.Parallel()

		f := newVMUpFixture(t, true)

		stdout, stderr, err := f.run(t, append([]string{"vm-up"}, vars...)...)
		if err != nil {
			t.Fatalf("make vm-up %s on an instance that exists: %v\nstdout: %s\nstderr: %s", strings.Join(vars, " "), err, stdout, stderr)
		}

		want := []string{"start", f.env["LIMA_INSTANCE"]}
		starts := f.starts(t)

		if len(starts) == 0 {
			t.Fatalf("make vm-up did not start the instance that exists, so the VM stays down; stdout: %s\nstderr: %s", stdout, stderr)
		}

		for _, got := range starts {
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("make vm-up %s resized or reconfigured an existing VM: an instance that exists is started by its name alone, never with the flags that only apply at creation (issue #228) (-want +got):\n%s",
					strings.Join(vars, " "), diff)
			}
		}

		combined := stdout + stderr

		for _, name := range []string{vmCPUsVar, vmMemoryVar, vmDiskVar, vmSSHPortVar} {
			if !strings.Contains(combined, name) {
				t.Errorf("make vm-up with %s set and an instance that exists says nothing about %s, so a maintainer expects a VM created at that size and gets the old one (issue #228):\n%s",
					strings.Join(vars, " "), name, combined)
			}
		}

		if lowered := strings.ToLower(combined); !strings.Contains(lowered, "creat") {
			t.Errorf("the warning says nothing about creation, so it does not tell a maintainer that %s apply only when the VM is created and an existing VM is never resized (issue #228):\n%s",
				strings.Join(vars, " "), combined)
		}
	})

	t.Run("an invalid value fails before the instance is looked at", func(t *testing.T) {
		t.Parallel()

		f := newVMUpFixture(t, true)

		stdout, stderr, err := f.run(t, "vm-up", "VM_SSH_PORT=70000")
		if err == nil {
			t.Fatalf("make vm-up VM_SSH_PORT=70000 on an instance that exists exited 0: the value is refused whatever the instance is (issue #228)\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		if starts := f.starts(t); len(starts) != 0 {
			t.Errorf("make vm-up VM_SSH_PORT=70000 started the existing instance although the value is invalid: validation must come first (issue #228):%s", describeCalls(starts))
		}

		if combined := stdout + stderr; !strings.Contains(combined, vmSSHPortVar) {
			t.Errorf("the refusal of VM_SSH_PORT=70000 does not name VM_SSH_PORT (issue #228):\nstdout: %s\nstderr: %s", stdout, stderr)
		}
	})
}
