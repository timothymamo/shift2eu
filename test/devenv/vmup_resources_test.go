// Pins make vm-up's optional VM_CPUS, VM_MEMORY, VM_DISK and VM_SSH_PORT (issue #228): on creation they
// reach limactl start in Lima's own flag form, unset the creation command line is byte for byte what it was,
// an invalid value fails before limactl runs, make vm-create hands the four on to make vm-up, and an existing
// VM - Stopped or Running - is started by its name without any of them, with a warning that they apply only
// when the VM is created. Since issue #254 that start is a limactl shell --start, which never creates the VM.
// vm-up also refuses a workspace that holds anything but the repository and its dispatch worktrees, and
// neither check may be bypassed by make -i or make -k. Every run uses the fake limactl and the throwaway clone
// of vmup_test.go, which also pins what vm-up does with an instance that is Broken (issue #237): no test
// starts a real VM.
package devenv_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The four variables make vm-up takes (issue #228). UBUNTU_MIRROR, the fifth, is ubuntuMirrorVar of
// limaapt_test.go (issue #498).
const (
	vmCPUsVar    = "VM_CPUS"
	vmMemoryVar  = "VM_MEMORY"
	vmDiskVar    = "VM_DISK"
	vmSSHPortVar = "VM_SSH_PORT"
)

// vmUpRealGit stands in for the real git the vm-up git shim hands its other calls to: it prints the arguments
// it got, so a test can tell whether the shim ran it and what it passed on.
const vmUpRealGit = `#!/bin/sh
printf 'real-git'
for a in "$@"; do printf ' %s' "$a"; done
printf '\n'
`

// sortedSettings renders the variables set in the environment as NAME=value, sorted by name, for a failure
// message that does not depend on map order.
func sortedSettings(env map[string]string) []string {
	settings := make([]string, 0, len(env))

	for name, value := range env {
		settings = append(settings, name+"="+value)
	}

	slices.Sort(settings)

	return settings
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
		vars []string          // the make arguments that set the variables
		env  map[string]string // the variables set in the environment instead of the command line
		want []string          // the flags they add, in this order
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
		{
			// The longest values vm-up accepts (issue #228 review, item B): the cap on the digits refuses
			// anything longer, and nothing shorter.
			name: "the longest CPU count and sizes it accepts",
			vars: []string{"VM_CPUS=999", "VM_MEMORY=99999GiB", "VM_DISK=99999GiB"},
			want: []string{"--cpus=999", "--memory=99999", "--disk=99999"},
		},
		// The variables are read from the environment too, the way a shell exports them before make runs,
		// so a maintainer can set them in a profile or with direnv rather than on the command line.
		{
			name: "VM_CPUS in the environment alone",
			env:  map[string]string{"VM_CPUS": "6"},
			want: []string{"--cpus=6"},
		},
		{
			name: "all four in the environment",
			env: map[string]string{
				"VM_CPUS": "6", "VM_MEMORY": "8GiB", "VM_DISK": "60GiB", "VM_SSH_PORT": "60023",
			},
			want: []string{"--cpus=6", "--memory=8", "--disk=60", "--ssh-port=60023"},
		},
		{
			name: "the environment and the command line together",
			vars: []string{"VM_MEMORY=10GiB"},
			env:  map[string]string{"VM_CPUS": "6", "VM_SSH_PORT": "60023"},
			want: []string{"--cpus=6", "--memory=10", "--ssh-port=60023"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")

			for name, value := range tc.env {
				f.env[name] = value
			}

			setting := strings.Join(tc.vars, " ")
			if setting == "" {
				setting = "with " + strings.Join(sortedSettings(tc.env), ", ") + " in the environment"
			}

			stdout, stderr, err := f.run(t, append([]string{"vm-up"}, tc.vars...)...)
			if err != nil {
				t.Fatalf("make vm-up %s: %v\nstdout: %s\nstderr: %s", setting, err, stdout, stderr)
			}

			want := f.creationWith(tc.want)
			got := f.creating(t)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("make vm-up %s creates the VM with the wrong limactl start argv (issue #228): the existing creation arguments stay as they are, the flags of the set variables follow them, and the template stays the last argument (-want +got):\n%s", setting, diff)
			}
		})
	}
}

// TestMakeVMUpRefusesAnInvalidVMResource pins issue #228's validation, and UBUNTU_MIRROR's with it (issue
// #498): a value that is not a positive whole number, not a whole GiB size, not a port Lima can forward or not
// an http:// or https:// mirror URL ends make vm-up with an error that names the variable, before limactl runs
// at all - and so before anything a value might carry could run as a shell command.
func TestMakeVMUpRefusesAnInvalidVMResource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		variable string
		value    string // {marker} is replaced with the fixture's marker path
		env      bool   // the variable is set in the environment rather than on the make command line
		target   string // the make target, vm-up when empty
	}{
		// VM_CPUS is a CPU count: a positive whole number.
		{name: "VM_CPUS 0", variable: vmCPUsVar, value: "0"},
		{name: "VM_CPUS -1", variable: vmCPUsVar, value: "-1"},
		{name: "VM_CPUS abc", variable: vmCPUsVar, value: "abc"},
		{name: "VM_CPUS 4.5", variable: vmCPUsVar, value: "4.5"},
		{name: "VM_CPUS a command after a semicolon", variable: vmCPUsVar, value: "4; touch {marker}"},
		{name: "VM_CPUS a command substitution", variable: vmCPUsVar, value: "$(touch {marker})"},
		{name: "VM_CPUS a backtick command", variable: vmCPUsVar, value: "`touch {marker}`"},
		{name: "VM_CPUS a number with a single quote", variable: vmCPUsVar, value: "4'"},
		{name: "VM_CPUS a quoted command", variable: vmCPUsVar, value: "4'; touch {marker}; echo '"},
		{name: "VM_CPUS a quoted command in the environment", variable: vmCPUsVar, value: "4'; touch {marker}; echo '", env: true},
		// make's own syntax is not shell syntax: the Makefile reads each variable raw ($(value ...)), so a
		// $(shell ...) in a value stays text - on the command line, in the environment, and on the way
		// through make vm-create (issue #228 review, item A).
		{name: "VM_CPUS make's shell function on the command line", variable: vmCPUsVar, value: "$(shell touch {marker})"},
		{name: "VM_CPUS make's shell function in the environment", variable: vmCPUsVar, value: "$(shell touch {marker})", env: true},
		{name: "VM_CPUS make's shell function through make vm-create", variable: vmCPUsVar, value: "$(shell touch {marker})", target: "vm-create"},
		{name: "VM_MEMORY make's shell function on the command line", variable: vmMemoryVar, value: "$(shell touch {marker})GiB"},
		// A number longer than the flag it feeds is refused before limactl, which would otherwise reject it or
		// wrap it: CPUs are at most three digits, a GiB size at most five (issue #228 review, item B).
		{name: "VM_CPUS 20 digits", variable: vmCPUsVar, value: "99999999999999999999"},
		{name: "VM_CPUS 4 digits", variable: vmCPUsVar, value: "1000"},
		{name: "VM_MEMORY 11 digits", variable: vmMemoryVar, value: "99999999999GiB"},
		{name: "VM_MEMORY 6 digits", variable: vmMemoryVar, value: "100000GiB"},
		{name: "VM_DISK 11 digits", variable: vmDiskVar, value: "99999999999GiB"},
		{name: "VM_DISK 6 digits", variable: vmDiskVar, value: "100000GiB"},
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
		{name: "VM_MEMORY a size with a single quote", variable: vmMemoryVar, value: "8GiB'"},
		{name: "VM_DISK a quoted command", variable: vmDiskVar, value: "8GiB'; touch {marker}; echo '"},
		// VM_SSH_PORT is a port Lima can forward: 1024 to 65535.
		{name: "VM_SSH_PORT 1023", variable: vmSSHPortVar, value: "1023"},
		{name: "VM_SSH_PORT 65536", variable: vmSSHPortVar, value: "65536"},
		{name: "VM_SSH_PORT 0", variable: vmSSHPortVar, value: "0"},
		{name: "VM_SSH_PORT abc", variable: vmSSHPortVar, value: "abc"},
		{name: "VM_SSH_PORT with a space", variable: vmSSHPortVar, value: "600 22"},
		{name: "VM_SSH_PORT a number with a single quote", variable: vmSSHPortVar, value: "60023'"},
		{name: "VM_SSH_PORT a quoted command in the environment", variable: vmSSHPortVar, value: "60023'; touch {marker}; echo '", env: true},
		// UBUNTU_MIRROR is the Ubuntu mirror the VM's provisioning fetches from (issue #498): an http:// or
		// https:// URL of letters, digits, '.', '-', '/', ':', '_' and '~'. Anything else - another scheme, a
		// bare host, a query string, a space, or a shell or make metacharacter - is refused before limactl runs,
		// so a value pasted from a mirror list never reaches a shell and never reaches the VM.
		{name: "UBUNTU_MIRROR over ftp", variable: ubuntuMirrorVar, value: "ftp://ftp.example.com/ubuntu"},
		{name: "UBUNTU_MIRROR without a scheme", variable: ubuntuMirrorVar, value: "archive.ubuntu.com/ubuntu"},
		{name: "UBUNTU_MIRROR with an empty scheme", variable: ubuntuMirrorVar, value: "//archive.ubuntu.com/ubuntu"},
		{name: "UBUNTU_MIRROR with no host", variable: ubuntuMirrorVar, value: "http://"},
		{name: "UBUNTU_MIRROR with a space", variable: ubuntuMirrorVar, value: "http://archive.ubuntu.com/ubuntu resolute"},
		{name: "UBUNTU_MIRROR with a query string", variable: ubuntuMirrorVar, value: "http://mirror.example.com/ubuntu?release=24.04"},
		{name: "UBUNTU_MIRROR a command after a semicolon", variable: ubuntuMirrorVar, value: "http://x; touch {marker}"},
		{name: "UBUNTU_MIRROR a command substitution", variable: ubuntuMirrorVar, value: "http://x$(touch {marker})"},
		{name: "UBUNTU_MIRROR a backtick command", variable: ubuntuMirrorVar, value: "http://x`touch {marker}`"},
		{name: "UBUNTU_MIRROR a pipe", variable: ubuntuMirrorVar, value: "http://x|touch {marker}"},
		{name: "UBUNTU_MIRROR an ampersand", variable: ubuntuMirrorVar, value: "http://x&touch {marker}"},
		{name: "UBUNTU_MIRROR a command after a comma", variable: ubuntuMirrorVar, value: "http://x,touch {marker}"},
		{name: "UBUNTU_MIRROR with an equals sign", variable: ubuntuMirrorVar, value: "http://x=y"},
		{name: "UBUNTU_MIRROR with a single quote", variable: ubuntuMirrorVar, value: "http://x'"},
		{name: "UBUNTU_MIRROR with a double quote", variable: ubuntuMirrorVar, value: "http://x\"y"},
		{name: "UBUNTU_MIRROR with a newline", variable: ubuntuMirrorVar, value: "http://x\nhttp://y"},
		{name: "UBUNTU_MIRROR make's shell function on the command line", variable: ubuntuMirrorVar, value: "$(shell touch {marker})"},
		{name: "UBUNTU_MIRROR make's shell function in the environment", variable: ubuntuMirrorVar, value: "$(shell touch {marker})", env: true},
		{name: "UBUNTU_MIRROR a quoted command in the environment", variable: ubuntuMirrorVar, value: "http://x'; touch {marker}; echo '", env: true},
		{name: "UBUNTU_MIRROR through make vm-create", variable: ubuntuMirrorVar, value: "ftp://ftp.example.com/ubuntu", target: "vm-create"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")
			target := tc.target
			if target == "" {
				target = "vm-up"
			}

			value := strings.ReplaceAll(tc.value, "{marker}", f.marker)
			setting := tc.variable + "=" + strconv.Quote(value) + " on the command line"
			args := []string{target, tc.variable + "=" + value}

			if tc.env {
				setting = tc.variable + "=" + strconv.Quote(value) + " in the environment"
				args = []string{target}
				f.env[tc.variable] = value
			}

			stdout, stderr, err := f.run(t, args...)
			if err == nil {
				t.Fatalf("make %s %s exited 0: vm-up must refuse a value that is not a CPU count, a whole GiB size, a port in 1024-65535 or an http(s) mirror URL, so limactl never sees it (issue #228, issue #498)\nstdout: %s\nstderr: %s", target, setting, stdout, stderr)
			}

			if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) != 0 {
				t.Errorf("make %s %s called limactl before it refused the value: validation must come first, so no value can reach limactl or a shell (issue #228):%s",
					target, setting, describeCalls(calls))
			}

			if _, err := os.Stat(f.marker); err == nil {
				t.Errorf("make %s %s created %s: a value the Makefile refuses must never be run by a shell (issue #228)", target, setting, f.marker)
			}

			if combined := stdout + stderr; !strings.Contains(combined, tc.variable) {
				t.Errorf("the refusal of %s by make %s does not name %s, so a maintainer cannot tell which variable to fix (issue #228):\nstdout: %s\nstderr: %s",
					setting, target, tc.variable, stdout, stderr)
			}
		})
	}
}

// TestMakeNeverExpandsMakeSyntaxInAVMResource pins the override block at the top of the Makefile (issue #228
// review, item A). make exports the VM variables - UBUNTU_MIRROR with them (issue #498) - to the shell of every
// recipe, expanding them on the way, so a $(shell ...) in one of them would run in any target when the Makefile
// read it late: each is read raw ($(value ...)) before anything else, so no target - not even one that has
// nothing to do with the VM - runs it.
func TestMakeNeverExpandsMakeSyntaxInAVMResource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		variable string   // the variable the value is set in; VM_CPUS when empty
		args     []string // the make arguments, without the value, which the test adds
		env      bool     // the value is set in the environment rather than on the make command line
	}{
		{name: "make help", args: []string{"help"}},
		{name: "make -n build", args: []string{"-n", "build"}},
		{name: "make help with the value in the environment", args: []string{"help"}, env: true},
		{name: "make help with UBUNTU_MIRROR", variable: ubuntuMirrorVar, args: []string{"help"}},
		{name: "make -n build with UBUNTU_MIRROR", variable: ubuntuMirrorVar, args: []string{"-n", "build"}},
		{name: "make help with UBUNTU_MIRROR in the environment", variable: ubuntuMirrorVar, args: []string{"help"}, env: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")
			value := "$(shell touch " + f.marker + ")"
			variable := tc.variable
			if variable == "" {
				variable = vmCPUsVar
			}

			args := slices.Clone(tc.args)
			setting := variable + "=" + strconv.Quote(value) + " on the command line"

			if tc.env {
				setting = variable + "=" + strconv.Quote(value) + " in the environment"
				f.env[variable] = value
			} else {
				args = append(args, variable+"="+value)
			}

			stdout, stderr, err := f.run(t, args...)
			if err != nil {
				t.Fatalf("make %s with %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), setting, err, stdout, stderr)
			}

			if _, err := os.Stat(f.marker); err == nil {
				t.Errorf("make %s with %s created %s: the Makefile reads each VM variable raw, so make never expands the syntax in one, whatever the target (issue #228 review, item A)",
					strings.Join(args, " "), setting, f.marker)
			}
		})
	}
}

// TestMakeVMUpRefusesAnInvalidValueWhateverMakeIsTold pins the auditor's L1 finding (issue #228 review, item
// C): make -i ignores a recipe line that failed and make -k carries on past it, so vm-up's validation and the
// limactl call it guards have to share one shell. However make is told to treat an error, limactl never gets a
// value vm-up has not validated. The same holds for vm-up's stray-workspace check, which must run in that one
// shell too: started with any of these flags, it must neither mount nor start anything.
func TestMakeVMUpRefusesAnInvalidValueWhateverMakeIsTold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		flags      []string
		cpus       string // VM_CPUS on the make command line, empty when it is unset
		stray      string // an entry beside the repository in the workspace, empty when it holds only the repository
		strayIsDir bool   // the stray entry is a directory rather than a file
		want       string // what the refusal must name, so a maintainer knows what to fix
	}{
		{name: "make -i", flags: []string{"-i"}, cpus: "abc", want: vmCPUsVar},
		{name: "make -k", flags: []string{"-k"}, cpus: "abc", want: vmCPUsVar},
		{name: "make -i -k", flags: []string{"-i", "-k"}, cpus: "abc", want: vmCPUsVar},
		// The workspace the VM mounts writable must hold only the repository and its dispatch worktrees. A
		// stray entry is refused before anything is mounted or started, whatever make is told about errors.
		{name: "make -i with a stray file in the workspace", flags: []string{"-i"}, stray: "stray-file", want: "stray-file"},
		{name: "make -k with a stray file in the workspace", flags: []string{"-k"}, stray: "stray-file", want: "stray-file"},
		{name: "make -i -k with a stray file in the workspace", flags: []string{"-i", "-k"}, stray: "stray-file", want: "stray-file"},
		{name: "make -i with a stray directory in the workspace", flags: []string{"-i"}, stray: "stray-dir", strayIsDir: true, want: "stray-dir"},
		{name: "make -k with a stray directory in the workspace", flags: []string{"-k"}, stray: "stray-dir", strayIsDir: true, want: "stray-dir"},
		// The order: a value that is not a CPU count is refused before the workspace is looked at, so the
		// maintainer fixes the value first. Either way limactl never runs.
		{name: "make -i with a stray file and VM_CPUS=abc", flags: []string{"-i"}, stray: "stray-file", cpus: "abc", want: vmCPUsVar},
		{name: "make -k with a stray file and VM_CPUS=abc", flags: []string{"-k"}, stray: "stray-file", cpus: "abc", want: vmCPUsVar},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")
			run := "make " + strings.Join(append(slices.Clone(tc.flags), "vm-up"), " ")
			args := append(slices.Clone(tc.flags), "vm-up")

			if tc.cpus != "" {
				args = append(args, vmCPUsVar+"="+tc.cpus)
				run += " " + vmCPUsVar + "=" + tc.cpus
			}

			if tc.stray != "" {
				path := filepath.Join(f.workspace(), tc.stray)

				if tc.strayIsDir {
					if err := os.MkdirAll(filepath.Join(path, "nested"), 0o750); err != nil {
						t.Fatalf("create the stray directory %s: %v", path, err)
					}
				} else if err := os.WriteFile(path, []byte("not the repository\n"), 0o600); err != nil {
					t.Fatalf("write the stray file %s: %v", path, err)
				}

				run += " with " + tc.stray + " in the workspace"
			}

			stdout, stderr, _ := f.run(t, args...)

			if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) != 0 {
				t.Errorf("%s called limactl although vm-up refuses before it starts or mounts anything: make -i ignores a failed recipe line and make -k carries on past it, so the stray-workspace check, the validation and the limactl calls they guard must be one shell that stops on the first failure (issue #228):%s\nstdout: %s\nstderr: %s",
					run, describeCalls(calls), stdout, stderr)
			}

			if combined := stdout + stderr; !strings.Contains(combined, tc.want) {
				t.Errorf("%s says nothing about %s: the refusal names the entry or the value a maintainer has to fix, rather than dropping it silently (issue #228):\nstdout: %s\nstderr: %s",
					run, tc.want, stdout, stderr)
			}
		})
	}
}

// exitStderr is the stderr an exec.Cmd.Output call kept for its failure message, "" for any other error: a
// command that failed to run says why there, such as sh's "Text file busy" for a script it could not exec.
func exitStderr(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return "\nstderr:\n" + string(exitErr.Stderr)
	}

	return ""
}

// TestVMUpGitShimRunsTheRealGitAtAnyPath pins the shim's quoting (issue #228 review, item D): the path of the
// real git is one single-quoted sh word, so a path that carries a $, a backtick, a space or a single quote
// reaches sh as it is. Quoted for Go instead, sh would expand part of the path, split it, or run a command in
// it, and every make vm-up would hand its git calls to something else than git.
func TestVMUpGitShimRunsTheRealGitAtAnyPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dir  string // the directory the real git sits in, under the test's temporary directory
	}{
		{name: "a plain path", dir: "plain"},
		{name: "a path with a space", dir: "with space"},
		{name: "a path with a dollar", dir: "with $HOME"},
		{name: "a path with a command substitution", dir: "with $(id)"},
		{name: "a path with a backtick", dir: "with `id`"},
		{name: "a path with a single quote", dir: "with 'quotes'"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), tc.dir)
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatalf("create %s: %v", dir, err)
			}

			real := filepath.Join(dir, "git")
			writeFileMode(t, real, []byte(vmUpRealGit), 0o700)

			shim := writeGitShim(t, t.TempDir(), real)

			out, err := exec.Command(shim, "rev-parse", "--show-toplevel").Output()
			if err != nil {
				t.Fatalf("the git shim of the real git at %s: %v%s", real, err, exitStderr(err))
			}

			want, got := "real-git rev-parse --show-toplevel\n", string(out)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("the git shim did not run the real git at %s with the arguments it got: sh expanded or split the path, so make's git calls in the vm-up tests reach the wrong command (issue #228 review, item D) (-want +got):\n%s",
					real, diff)
			}
		})
	}

	t.Run("git config core.hooksPath answers without the real git", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		real := filepath.Join(dir, "git")
		writeFileMode(t, real, []byte(vmUpRealGit), 0o700)

		shim := writeGitShim(t, dir, real)

		out, err := exec.Command(shim, "config", "core.hooksPath").Output()
		if err != nil {
			t.Fatalf("the git shim with git config core.hooksPath: %v%s", err, exitStderr(err))
		}

		if got := string(out); got != "" {
			t.Errorf("the git shim ran the real git for `git config core.hooksPath` and printed %q: that call reads the write make vm-up's git-hooks prerequisite makes into the checkout's .git/config, which the vm-up fixture must answer itself", got)
		}
	})
}

// TestMakeVMCreatePassesTheVMResourcesToVMUp pins issue #228's pass-through: make vm-create hands the four
// variables to the make vm-up it runs, so one command still creates the VM at the size the host needs.
func TestMakeVMCreatePassesTheVMResourcesToVMUp(t *testing.T) {
	t.Parallel()

	f := newVMUpFixture(t, "", "")

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
// only when the VM is created. The call that starts it is the limactl shell --start of issue #254, so the four
// flags of creation reach it no more than they reached the limactl start it replaces. A value that is invalid
// still fails first, so the variables are checked before vm-up looks at the instance at all. An instance that is
// Broken never reaches that path: issue #237 refuses it before the start, and the warning about the four
// variables is not printed for it, as there is nothing vm-up would start and so nothing that applies only at
// creation.
func TestMakeVMUpNeverResizesAnExistingVM(t *testing.T) {
	t.Parallel()

	vars := []string{"VM_CPUS=6", "VM_MEMORY=8GiB", "VM_DISK=60GiB", "VM_SSH_PORT=60023"}

	// Both statuses vm-up starts an existing instance in, because the warning and the start by name alone are
	// the same on either: a Running instance is not resized twice because it happens to be up already.
	for _, status := range []string{"Stopped", "Running"} {
		t.Run("the instance is started by name without the four flags: "+status, func(t *testing.T) {
			t.Parallel()

			// An instance vm-up may start: listed as Stopped or Running, with the lima.yaml an instance that
			// was started once has (issue #237). Without it vm-up refuses the start, so only the resize stays
			// unpinned.
			f := newVMUpFixture(t, vmUpInstance, status)
			f.writeInstanceLimaYaml(t)

			stdout, stderr, err := f.run(t, append([]string{"vm-up"}, vars...)...)
			if err != nil {
				t.Fatalf("make vm-up %s on a %s instance that exists: %v\nstdout: %s\nstderr: %s", strings.Join(vars, " "), status, err, stdout, stderr)
			}

			want := f.startExistingArgs()
			starts := f.starting(t)

			if len(starts) == 0 {
				t.Fatalf("make vm-up did not start the %s instance that exists, so the VM stays down; stdout: %s\nstderr: %s", status, stdout, stderr)
			}

			for _, got := range starts {
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("make vm-up %s resized or reconfigured an existing %s VM: an instance that exists is started by its name alone, with the limactl shell --start that never creates one, never with the flags that only apply at creation (issue #228, issue #254) (-want +got):\n%s",
						strings.Join(vars, " "), status, diff)
				}
			}

			combined := stdout + stderr

			for _, name := range []string{vmCPUsVar, vmMemoryVar, vmDiskVar, vmSSHPortVar} {
				if !strings.Contains(combined, name) {
					t.Errorf("make vm-up with %s set and a %s instance that exists says nothing about %s, so a maintainer expects a VM created at that size and gets the old one (issue #228):\n%s",
						strings.Join(vars, " "), status, name, combined)
				}
			}

			if lowered := strings.ToLower(combined); !strings.Contains(lowered, "creat") {
				t.Errorf("the warning says nothing about creation, so it does not tell a maintainer that %s apply only when the VM is created and an existing %s VM is never resized (issue #228):\n%s",
					strings.Join(vars, " "), status, combined)
			}
		})
	}

	t.Run("an invalid value fails before the instance is looked at", func(t *testing.T) {
		t.Parallel()

		// The instance is one vm-up would start, so the only reason this run may fail is the port.
		f := newVMUpFixture(t, vmUpInstance, "Stopped")
		f.writeInstanceLimaYaml(t)

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

	t.Run("a broken instance is refused before the warning", func(t *testing.T) {
		t.Parallel()

		// A Broken instance with no lima.yaml is what issue #237 refuses before any start: with the four
		// variables set, that refusal must still come, and the warning that they apply only when the VM is
		// created must not be printed - nothing is started, so nothing is ignored either.
		f := newVMUpFixture(t, vmUpInstance, "Broken")

		stdout, stderr, err := f.run(t, append([]string{"vm-up"}, vars...)...)
		if err == nil {
			t.Fatalf("make vm-up %s on a Broken instance exited 0: the issue #237 refusal comes before any start, whatever the four variables ask for (issue #228)\nstdout: %s\nstderr: %s",
				strings.Join(vars, " "), stdout, stderr)
		}

		if starts := f.starts(t); len(starts) != 0 {
			t.Errorf("make vm-up %s started a Broken instance: vm-up refuses it before limactl start (issue #228, issue #237):%s",
				strings.Join(vars, " "), describeCalls(starts))
		}

		if !strings.Contains(stderr, "Broken") {
			t.Errorf("make vm-up's stderr does not say %q on a Broken instance, so this case does not pin the issue #237 refusal (issue #228)\nstderr: %s",
				"Broken", stderr)
		}

		combined := stdout + stderr

		for _, w := range []string{"warning", "never resized"} {
			if strings.Contains(combined, w) {
				t.Errorf("make vm-up says %q with %s set on a Broken instance it refuses to start: the warning that the four variables apply only when the VM is created belongs to the path that starts an existing instance, and this case starts nothing (issue #228)\nstdout: %s\nstderr: %s",
					w, strings.Join(vars, " "), stdout, stderr)
			}
		}
	})
}
