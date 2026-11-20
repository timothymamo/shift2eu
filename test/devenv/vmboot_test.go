// Pins make vm-boot-install and make vm-boot-uninstall (issue #398): the host targets that install and remove the
// LaunchDaemon which starts the Lima VM of one instance at boot, with nobody logged in. They render the plist
// through dev/vm-boot-launchd.sh (vmbootplist_test.go pins its content), install it as root into
// VM_BOOT_LAUNCHD_DIR - /Library/LaunchDaemons on the Mac, a sandbox directory in these tests - and load it with
// `sudo launchctl bootstrap system <plist>`, printing every sudo command they run.
//
// Before anything is installed, the install refuses the case issue #398 is about: a Lima autostart job that
// already starts this instance. `limactl autostart enable` writes io.lima-vm.autostart.<instance> in the user's
// domain and, with --condition=boot, io.lima-vm.daemon.<instance> as a LaunchDaemon; both run
// `limactl start <name> --foreground`, the form that can fall back to template:default, a VM that mounts the home
// directory (issue #237). The job is found either loaded, through `launchctl print`, or as the plist file alone -
// launchd loads that at the next login or boot (LIMA_LAUNCHDAEMONS_DIR, /Library/LaunchDaemons on the Mac, is the
// test hook for the system directory). A job launchd has both loaded and a plist for is reported once, as loaded.
// The refusal names the job, its plist, `limactl autostart disable
// <instance>` and the removal by hand - the bootout of a loaded job and the `rm -f` of the plist, with sudo in the
// system domain - and installs nothing.
//
// The targets are host targets: they run from the host clone, never as root (they run sudo themselves), and
// VM_BOOT_LAUNCHD_DIR may come from make's command line or from the Makefile, never from the environment - a
// stale export would have root write the plist where launchd never looks at boot. LIMA_LAUNCHDAEMONS_DIR is only
// read, and only from make's command line or the Makefile too: a value from the environment is put back to the
// directory launchd really reads Lima's system job from.
//
// Every run goes through the sandbox's fakes (sandbox_test.go): the log-only sudo copies and removes files inside
// the sandbox and runs the fake launchctl, and no test reaches the machine's sudo, launchd, plutil or limactl.
package devenv_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// vmBootLaunchdDir is the make variable the install and uninstall read the LaunchDaemons directory from: the
// same hook AGENTS_TOKEN_LAUNCHD_DIR is for the refresh's job (issue #229), pointed into the sandbox.
const vmBootLaunchdDir = "VM_BOOT_LAUNCHD_DIR"

// limaLaunchDaemonsDirVar is the make variable the checks read the system LaunchDaemons directory from, where
// Lima's own LaunchDaemon plist io.lima-vm.daemon.<instance>.plist sits: /Library/LaunchDaemons on the Mac, a
// directory of the sandbox in these tests, passed on make's command line (issues #315, #398).
const limaLaunchDaemonsDirVar = "LIMA_LAUNCHDAEMONS_DIR"

// limaAutostartPlistPath is where Lima's own autostart job's plist lives, whether launchd has loaded it or not:
// the user domain's io.lima-vm.autostart.<instance> under the maintainer's ~/Library/LaunchAgents, the system
// domain's io.lima-vm.daemon.<instance> under the system LaunchDaemons directory (issues #315, #398).
func limaAutostartPlistPath(domain jobDomain, home, launchDaemonsDir, label string) string {
	if domain == systemDomain {
		return filepath.Join(launchDaemonsDir, label+".plist")
	}

	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// vmBootLimactl stands in for limactl in the vm-boot tests: it records its argv and exits 0, answering every
// listing with the instance's name. Nothing here needs a VM, an instance directory or Lima itself, and an install
// that asks limactl whether the instance exists must not fail for an answer the fake never gave.
// SHIFT2EU_TEST_LIMA_LIST_STATUS makes the listing fail instead, as a limactl that cannot read the instances does.
const vmBootLimactl = `#!/bin/sh
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
if [ "${1:-}" = list ]; then
  if [ "${SHIFT2EU_TEST_LIMA_LIST_STATUS:-0}" -ne 0 ]; then
    echo "fake limactl: cannot list the instances" >&2
    exit "${SHIFT2EU_TEST_LIMA_LIST_STATUS}"
  fi
  printf '%s\n' "${SHIFT2EU_TEST_LIMA_INSTANCE:-}"
fi
exit 0
`

// vmBootInstallFixture is one environment for a run of make vm-boot-install or vm-boot-uninstall: a sandbox with
// a temporary HOME, the recording limactl, the log-only fake sudo and the fake launchctl, plutil and id, the
// instance's name in the environment, and a LaunchDaemons directory inside the sandbox that travels on make's
// command line. The jobs the fake launchctl serves are the fixture's launchdJobs.
type vmBootInstallFixture struct {
	sb         *sandbox
	jobs       *launchdJobs
	env        map[string]string
	instance   string
	dir        string // where make runs: the repository, whose Makefile is what this test pins
	launchdDir string // where launchd reads LaunchDaemons from: the make command line's VM_BOOT_LAUNCHD_DIR
	// limaLaunchDaemonsDir is where Lima's own LaunchDaemon plist sits: the make command line's
	// LIMA_LAUNCHDAEMONS_DIR, a directory of the sandbox's own, never the boot job's (issues #315, #398).
	limaLaunchDaemonsDir string
}

// newVMBootInstallFixture builds the fixture for one VM instance, with no Lima autostart job loaded.
func newVMBootInstallFixture(t *testing.T, instance string) *vmBootInstallFixture {
	t.Helper()

	sb := newSandbox(t)
	writeFileMode(t, filepath.Join(sb.binDir, "limactl"), []byte(vmBootLimactl), 0o700)

	env := sb.baseEnv()
	env["LIMA_INSTANCE"] = instance
	env["SHIFT2EU_TEST_LIMA_INSTANCE"] = instance

	launchdDir := filepath.Join(sb.root, "LaunchDaemons")
	// Lima's own LaunchDaemon plist lives in a directory of its own, never beside the boot job's: the install's
	// own plist must never be taken for Lima's, and a refusal names the two paths apart (issues #315, #398).
	limaLaunchDaemonsDir := filepath.Join(sb.root, "LimaLaunchDaemons")

	for _, dir := range []string{launchdDir, limaLaunchDaemonsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	sb.useLogOnlySudo(t)
	sb.useFakePlutil(t, env, "0")

	// The fake launchctl the jobs fixture serves, with no job loaded until a test adds one.
	jobs := newLaunchdJobs(t, sb)
	jobs.attach(env)

	dir, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	return &vmBootInstallFixture{
		sb:                   sb,
		jobs:                 jobs,
		env:                  env,
		instance:             instance,
		dir:                  dir,
		launchdDir:           launchdDir,
		limaLaunchDaemonsDir: limaLaunchDaemonsDir,
	}
}

// label is the boot job's LaunchDaemon label of the fixture's instance.
func (f *vmBootInstallFixture) label() string { return vmBootLabelPrefix + f.instance }

// dest is the file launchd reads the job from: inside the sandbox, since the recipe writes it under
// VM_BOOT_LAUNCHD_DIR and nothing here may reach /Library (issue #398).
func (f *vmBootInstallFixture) dest() string {
	return filepath.Join(f.launchdDir, f.label()+".plist")
}

// limaPlistPath is where Lima's own autostart job's plist of the fixture's instance lives, loaded or not.
func (f *vmBootInstallFixture) limaPlistPath(domain jobDomain, label string) string {
	return limaAutostartPlistPath(domain, f.env["HOME"], f.limaLaunchDaemonsDir, label)
}

// run runs one target with VM_BOOT_LAUNCHD_DIR and LIMA_LAUNCHDAEMONS_DIR on make's command line, the only place
// the recipes read those test hooks from.
func (f *vmBootInstallFixture) run(t *testing.T, target string) (string, string, error) {
	t.Helper()

	return runMakeIn(t, f.dir, []string{
		vmBootLaunchdDir + "=" + f.launchdDir,
		limaLaunchDaemonsDirVar + "=" + f.limaLaunchDaemonsDir,
		target,
	}, f.env)
}

// runWithTheDirectoryInTheEnvironment runs one target with VM_BOOT_LAUNCHD_DIR exported in the environment and not
// on make's command line, the way a stale export in a maintainer's shell would reach it.
func (f *vmBootInstallFixture) runWithTheDirectoryInTheEnvironment(t *testing.T, target string) (string, string, error) {
	t.Helper()

	env := make(map[string]string, len(f.env)+1)
	for name, value := range f.env {
		env[name] = value
	}

	env[vmBootLaunchdDir] = f.launchdDir

	return runMakeIn(t, f.dir, []string{target}, env)
}

// runWithTheLimaDirectoryInTheEnvironment runs one target with LIMA_LAUNCHDAEMONS_DIR exported in the environment
// and not on make's command line, the way a stale export in a maintainer's shell would reach it: the hook is
// read from make's command line or the Makefile alone (issues #315, #398).
func (f *vmBootInstallFixture) runWithTheLimaDirectoryInTheEnvironment(t *testing.T, target string) (string, string, error) {
	t.Helper()

	env := make(map[string]string, len(f.env)+1)
	for name, value := range f.env {
		env[name] = value
	}

	env[limaLaunchDaemonsDirVar] = f.limaLaunchDaemonsDir

	return runMakeIn(t, f.dir, []string{vmBootLaunchdDir + "=" + f.launchdDir, target}, env)
}

// sudoCommands returns the argv of every command the recipe ran through sudo, in call order.
func (f *vmBootInstallFixture) sudoCommands(t *testing.T) [][]string {
	t.Helper()

	return invocations(f.sb.argvLogText(t), "sudo")
}

// bootstrapCalls returns the argv of every `sudo launchctl bootstrap` the recipe ran, in call order.
func (f *vmBootInstallFixture) bootstrapCalls(t *testing.T) [][]string {
	t.Helper()

	var calls [][]string

	for _, args := range f.sudoCommands(t) {
		if len(args) > 1 && args[0] == "launchctl" && args[1] == "bootstrap" {
			calls = append(calls, args)
		}
	}

	return calls
}

// installCalls returns the argv of every `sudo install` the recipe ran, in call order: the command that puts the
// plist where launchd loads it at the next boot.
func (f *vmBootInstallFixture) installCalls(t *testing.T) [][]string {
	t.Helper()

	var calls [][]string

	for _, args := range f.sudoCommands(t) {
		if len(args) > 0 && args[0] == "install" {
			calls = append(calls, args)
		}
	}

	return calls
}

// TestMakeVMBootInstallRefusesALimaAutostartJob pins the refusal issue #398 asks for: while Lima's own autostart
// job starts this instance, the install loads no job of its own - the two would race for the same VM, and Lima's
// runs `limactl start <name> --foreground`, which can fall back to template:default (issue #237). The refusal
// comes before anything is installed, in the user's domain (io.lima-vm.autostart.<instance>) and in the system
// domain (io.lima-vm.daemon.<instance>), for a job launchd has loaded and for one whose plist is only installed -
// launchd loads that file at the next login or boot, so the VM is started twice from then on although nothing runs
// at this moment. It names the job, the command that removes it the supported way
// (`limactl autostart disable <instance>`) and the removal by hand: the bootout of a loaded job, and the `rm -f`
// of the plist, with sudo in the system domain, where root owns it.
func TestMakeVMBootInstallRefusesALimaAutostartJob(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		domain jobDomain
		prefix string
		// loaded says the case has launchd run the job; installed says it only writes the job's plist, where
		// `limactl autostart enable` writes it and launchd loads it at the next login or boot (issue #398).
		loaded    bool
		installed bool
	}{
		{
			name:   "the user domain's loaded io.lima-vm.autostart job",
			domain: userDomain,
			prefix: vmBootLimaAutostartLabelPrefix,
			loaded: true,
		},
		{
			name:   "the system domain's loaded io.lima-vm.daemon job",
			domain: systemDomain,
			prefix: vmBootLimaDaemonLabelPrefix,
			loaded: true,
		},
		{
			name:      "the user domain's installed io.lima-vm.autostart plist, not loaded",
			domain:    userDomain,
			prefix:    vmBootLimaAutostartLabelPrefix,
			installed: true,
		},
		{
			name:      "the system domain's installed io.lima-vm.daemon plist, not loaded",
			domain:    systemDomain,
			prefix:    vmBootLimaDaemonLabelPrefix,
			installed: true,
		},
		{
			name:      "the user domain's io.lima-vm.autostart job, both loaded and installed",
			domain:    userDomain,
			prefix:    vmBootLimaAutostartLabelPrefix,
			loaded:    true,
			installed: true,
		},
		{
			name:      "the system domain's io.lima-vm.daemon job, both loaded and installed",
			domain:    systemDomain,
			prefix:    vmBootLimaDaemonLabelPrefix,
			loaded:    true,
			installed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMBootInstallFixture(t, hostTargetInstance)
			label := tc.prefix + f.instance
			plist := f.limaPlistPath(tc.domain, label)

			state := "installed at " + plist

			if tc.loaded {
				state = "loaded in the " + f.jobs.domainName(tc.domain) + " domain"
				if tc.installed {
					state += " and installed at " + plist
				}

				// Lima's job as `limactl autostart enable` loads it: limactl start <instance> --foreground.
				f.jobs.add(t, tc.domain, label, loadedJob{args: []string{"/opt/homebrew/bin/limactl", "start", f.instance, "--foreground"}})
			}

			if tc.installed {
				// Lima's job's plist as `limactl autostart enable` writes it, which launchd loads at the next
				// login or boot: nothing runs now, and the VM is started twice from the next boot on. A loaded
				// job keeps the plist launchd loaded it from, which is this file, so both can hold at once.
				writeFileMode(t, plist, []byte("Lima's own autostart job of "+f.instance+"\n"), 0o600)
			}

			stdout, stderr, err := f.run(t, "vm-boot-install")
			if err == nil {
				t.Fatalf("make vm-boot-install exited 0 although Lima's own autostart job %s is %s and starts %s: the two jobs would race for the same VM, and Lima's runs limactl start <name>, which can create template:default (issues #237, #398)\nstdout: %s\nstderr: %s",
					label, state, f.instance, stdout, stderr)
			}

			combined := stdout + stderr

			wants := []string{label, "limactl autostart disable " + f.instance}

			// The removal by hand: a loaded job is booted out of its domain and its plist removed, and a plist
			// launchd has not loaded yet only removed - with sudo in the system domain, where root owns it.
			rm := "rm -f " + plist
			if tc.domain == systemDomain {
				rm = "sudo " + rm
			}

			switch {
			case tc.loaded && tc.domain == systemDomain:
				wants = append(wants, "sudo launchctl bootout system/"+label, "rm -f")
			case tc.loaded:
				wants = append(wants, "launchctl bootout gui/"+strconv.Itoa(os.Getuid())+"/"+label, "rm -f")
			default:
				wants = append(wants, plist, rm)
			}

			for _, want := range wants {
				if !strings.Contains(combined, want) {
					t.Errorf("make vm-boot-install does not say %q although Lima's autostart job %s is %s and starts this instance: the refusal must name the job, its plist and how to remove it, or a maintainer cannot install the boot job at all (issues #315, #398)\nstdout: %s\nstderr: %s",
						want, label, state, stdout, stderr)
				}
			}

			// One job, one report: a job launchd has both loaded and a plist for is said to be loaded only,
			// since that is what runs now. A second report of the same label sends a maintainer to remove a
			// job that is not the one it names, and the commands differ between the two (issue #398).
			if n := strings.Count(combined, "Lima's autostart job "+label+" is "); n != 1 {
				t.Errorf("make vm-boot-install reports Lima's autostart job %s %d time(s) although it is %s, want exactly one report (issue #398)\nstdout: %s\nstderr: %s",
					label, n, state, stdout, stderr)
			}

			// Where launchctl exists - the fixture's fake is on PATH, as it is on the Mac - the check asks it
			// about the job's domain, so a plist on disk is reported as installed only because launchctl did
			// not show the job as loaded, not because launchctl was missing.
			printTarget := "system/" + label
			if tc.domain == userDomain {
				printTarget = "gui/" + strconv.Itoa(os.Getuid()) + "/" + label
			}

			asked := false

			for _, args := range invocations(f.sb.argvLogText(t), "launchctl") {
				if slices.Equal(args, []string{"print", printTarget}) {
					asked = true
				}
			}

			if !asked {
				t.Errorf("make vm-boot-install ran no `launchctl print %s` although launchctl is on PATH, so it cannot tell a loaded job from a plist launchd only loads at the next boot (issue #398)",
					printTarget)
			}

			if tc.loaded && strings.Contains(combined, "is installed at ") {
				t.Errorf("make vm-boot-install reports Lima's autostart job %s as installed although launchd has it loaded: a loaded job is reported as loaded, and one report alone (issue #398)\nstdout: %s\nstderr: %s",
					label, stdout, stderr)
			}

			if !tc.loaded && strings.Contains(combined, "is loaded in ") {
				t.Errorf("make vm-boot-install reports Lima's autostart job %s as loaded although launchctl does not list it: the plist alone must be reported as installed, not as loaded (issue #398)\nstdout: %s\nstderr: %s",
					label, stdout, stderr)
			}

			// Refusing means installing nothing: no plist for launchd to load at the next boot, and no sudo.
			if calls := f.installCalls(t); len(calls) != 0 {
				t.Errorf("make vm-boot-install ran `sudo install` although it refuses to install beside %s: a plist left in the LaunchDaemons directory is a job launchd loads at the next boot whatever this run decided (issue #398): %v\nstdout: %s\nstderr: %s",
					label, calls, stdout, stderr)
			}

			if calls := f.bootstrapCalls(t); len(calls) != 0 {
				t.Errorf("make vm-boot-install ran `sudo launchctl bootstrap` although it refuses to install beside %s (issue #398): %v\nstdout: %s\nstderr: %s",
					label, calls, stdout, stderr)
			}

			entries, readErr := os.ReadDir(f.launchdDir)
			if readErr != nil {
				t.Fatalf("read %s: %v", f.launchdDir, readErr)
			}

			if len(entries) != 0 {
				t.Errorf("make vm-boot-install left %d file(s) in %s although it refuses to install beside %s, so a refused run still leaves a job for launchd to load (issue #398): %v",
					len(entries), f.launchdDir, label, entries)
			}

			// Lima's own file stays where it is: the install refuses beside it and removes nothing of Lima's.
			if tc.installed {
				if _, statErr := os.Stat(plist); statErr != nil {
					t.Errorf("make vm-boot-install removed Lima's own plist %s although it only refuses to install beside it: removing Lima's job is the maintainer's decision, with limactl autostart disable or by hand (issues #315, #398): %v",
						plist, statErr)
				}
			}
		})
	}
}

// TestMakeVMBootInstallLoadsTheJob pins the happy path: with no Lima autostart job, the install writes the
// instance's plist under VM_BOOT_LAUNCHD_DIR, loads it with `sudo launchctl bootstrap system <plist>`, and prints
// every sudo command it runs, so the maintainer sees what runs as root.
func TestMakeVMBootInstallLoadsTheJob(t *testing.T) {
	t.Parallel()

	f := newVMBootInstallFixture(t, hostTargetInstance)

	stdout, stderr, err := f.run(t, "vm-boot-install")
	if err != nil {
		t.Fatalf("make vm-boot-install exited non-zero with no Lima autostart job loaded: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	installed, err := os.ReadFile(f.dest())
	if err != nil {
		t.Fatalf("make vm-boot-install did not write the plist %s: %v\nstdout: %s\nstderr: %s", f.dest(), err, stdout, stderr)
	}

	if !strings.Contains(string(installed), "<string>"+f.label()+"</string>") {
		t.Errorf("the installed plist %s does not hold the boot job's label %q: launchd reads the job's Label from this file (issue #398):\n%s", f.dest(), f.label(), installed)
	}

	wantBootstrap := []string{"launchctl", "bootstrap", "system", f.dest()}

	calls := f.bootstrapCalls(t)
	if len(calls) != 1 {
		t.Errorf("make vm-boot-install ran launchctl bootstrap %d time(s), want one: the new plist only runs at the next boot until launchd has loaded it (issue #398): %v\nstdout: %s\nstderr: %s",
			len(calls), calls, stdout, stderr)
	} else if diff := cmp.Diff(wantBootstrap, calls[0]); diff != "" {
		t.Errorf("make vm-boot-install loaded the job with the wrong command (-want +got):\n%s\nthe install loads this instance's plist in the system domain, where a LaunchDaemon runs at boot with nobody logged in (issue #398)", diff)
	}

	// Every command that runs as root is printed before it runs, as the refresh's install does (issue #229).
	for _, args := range f.sudoCommands(t) {
		if slices.Equal(args, []string{"-k"}) {
			// sudo -k forgets the timestamp and runs no command.
			continue
		}

		line := strings.Join(append([]string{"sudo"}, args...), " ")
		if !strings.Contains(stdout, line) {
			t.Errorf("make vm-boot-install ran `%s` without printing it: a maintainer runs the install from the host clone and must see every command that becomes root (issue #398)\nstdout:\n%s", line, stdout)
		}
	}
}

// TestMakeVMBootInstallRefusesAnInstanceLimaDoesNotList pins the last check before anything is installed (issue
// #398): the job only ever starts an existing instance - `limactl shell --start <instance> true` never creates
// one - so an instance `limactl list --quiet` does not name makes the install refuse before any sudo. A listing
// that fails is not an empty one: a limactl the install cannot read says nothing about whether the VM is there, so
// the install refuses it too instead of installing a job that could only fail at every boot.
func TestMakeVMBootInstallRefusesAnInstanceLimaDoesNotList(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// mutate the fixture so its limactl does not list the instance, or cannot list at all.
		mutate func(f *vmBootInstallFixture)
		// want is what the refusal must say beyond naming the target: the answer the listing gave.
		want []string
	}{
		{
			name:   "the listing names no instance",
			mutate: func(f *vmBootInstallFixture) { f.env["SHIFT2EU_TEST_LIMA_INSTANCE"] = "" },
			want:   []string{"Lima lists no instance " + hostTargetInstance, "make vm-create"},
		},
		{
			name:   "the listing names another instance",
			mutate: func(f *vmBootInstallFixture) { f.env["SHIFT2EU_TEST_LIMA_INSTANCE"] = "another-instance" },
			want:   []string{"Lima lists no instance " + hostTargetInstance},
		},
		{
			name:   "the listing fails",
			mutate: func(f *vmBootInstallFixture) { f.env["SHIFT2EU_TEST_LIMA_LIST_STATUS"] = "1" },
			want:   []string{"could not list the Lima instances"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMBootInstallFixture(t, hostTargetInstance)
			tc.mutate(f)

			stdout, stderr, err := f.run(t, "vm-boot-install")
			if err == nil {
				t.Fatalf("make vm-boot-install exited 0 although %s: the job starts an existing instance only, so a plist installed for a VM that is not there can only fail at every boot (issue #398)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if !strings.Contains(stderr, "vm-boot-install: refused") {
				t.Errorf("the refusal is not the install recipe's own, which starts with %q, so this case pins another check than the one it is named for:\nstdout: %s\nstderr: %s",
					"vm-boot-install: refused", stdout, stderr)
			}

			for _, want := range tc.want {
				if !strings.Contains(stderr, want) {
					t.Errorf("make vm-boot-install's refusal says nothing about %q when %s, so a maintainer cannot tell what limactl answered or what to do about it (issue #398)\nstdout: %s\nstderr: %s",
						want, tc.name, stdout, stderr)
				}
			}

			// Refusing means installing nothing: the check comes before the plist is installed or loaded, and
			// before any command runs as root.
			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make vm-boot-install ran sudo although %s: the check of the instance comes before anything is installed, so root is never asked for a job the VM cannot use (issue #398): %v\nstdout: %s\nstderr: %s",
					tc.name, calls, stdout, stderr)
			}

			if calls := f.installCalls(t); len(calls) != 0 {
				t.Errorf("make vm-boot-install ran `sudo install` although %s (issue #398): %v\nstdout: %s\nstderr: %s",
					tc.name, calls, stdout, stderr)
			}

			if calls := f.bootstrapCalls(t); len(calls) != 0 {
				t.Errorf("make vm-boot-install ran `sudo launchctl bootstrap` although %s (issue #398): %v\nstdout: %s\nstderr: %s",
					tc.name, calls, stdout, stderr)
			}

			entries, readErr := os.ReadDir(f.launchdDir)
			if readErr != nil {
				t.Fatalf("read %s: %v", f.launchdDir, readErr)
			}

			if len(entries) != 0 {
				t.Errorf("make vm-boot-install left %d file(s) in %s although %s, so a refused run still leaves a job for launchd to load at the next boot (issue #398): %v",
					len(entries), f.launchdDir, tc.name, entries)
			}
		})
	}
}

// TestMakeVMBootInstallLintsAndPrintsThePlist pins what the install does with the rendered plist before root sees
// it (issue #398): it lints it with `plutil -lint`, so a plist launchd cannot load is refused while it is still
// nobody's job, and it prints the plist with its SHA-256, so a maintainer can tell the file that ends up in the
// LaunchDaemons directory from the one that was printed. The hash the install prints is the hash of the plist it
// installed, which is what its own read-back compares against.
func TestMakeVMBootInstallLintsAndPrintsThePlist(t *testing.T) {
	t.Parallel()

	t.Run("plutil lints the plist and the install prints its SHA-256", func(t *testing.T) {
		t.Parallel()

		f := newVMBootInstallFixture(t, hostTargetInstance)

		stdout, stderr, err := f.run(t, "vm-boot-install")
		if err != nil {
			t.Fatalf("make vm-boot-install exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		installed, err := os.ReadFile(f.dest())
		if err != nil {
			t.Fatalf("read the installed plist %s: %v\nstdout: %s\nstderr: %s", f.dest(), err, stdout, stderr)
		}

		lint := invocations(f.sb.argvLogText(t), "plutil")

		switch {
		case len(lint) != 1:
			t.Errorf("make vm-boot-install ran plutil %d time(s), want one -lint of the rendered plist: launchd loads a plist it cannot parse and fails at the next boot, so the install lints it before root writes it (issue #398): %v\nstdout: %s\nstderr: %s",
				len(lint), lint, stdout, stderr)
		case len(lint[0]) != 2 || lint[0][0] != "-lint":
			t.Errorf("make vm-boot-install ran `plutil %s`, want `plutil -lint <plist>`: only the lint parses the plist the install is about to put in place (issue #398)\nstdout: %s\nstderr: %s",
				strings.Join(lint[0], " "), stdout, stderr)
		case !filepath.IsAbs(lint[0][1]):
			t.Errorf("make vm-boot-install linted %q, which is no absolute path (issue #398)\nstdout: %s\nstderr: %s", lint[0][1], stdout, stderr)
		}

		// The hash the install prints is the hash of the plist it installed: that is the value its own read-back
		// compares with the printed one, so a maintainer comparing the two reads the same file root wrote.
		sum := sha256.Sum256(installed)
		wantSum := hex.EncodeToString(sum[:])

		if !strings.Contains(stdout, wantSum) {
			t.Errorf("make vm-boot-install does not print the SHA-256 %s of the plist it installed, so a maintainer cannot tell the file in %s from the one that was printed (issue #398)\nstdout:\n%s\nstderr: %s",
				wantSum, f.launchdDir, stdout, stderr)
		}

		// The plist itself is printed too, so what root writes is what the maintainer read.
		if !strings.Contains(stdout, string(installed)) {
			t.Errorf("make vm-boot-install does not print the plist it installed into %s, so a maintainer runs a file root writes without having seen it (issue #398)\nstdout:\n%s\nstderr: %s",
				f.dest(), stdout, stderr)
		}

		// plutil runs before any sudo: root is never handed a plist no lint has read.
		log := f.sb.argvLogText(t)
		linted, sudoed := strings.Index(log, "tool=plutil"), strings.Index(log, "tool=sudo")

		if linted < 0 || sudoed < 0 || linted > sudoed {
			t.Errorf("make vm-boot-install ran sudo before plutil linted the plist (their records begin at %d and %d of the argv log): the lint must come first, or root installs a plist launchd cannot load (issue #398)\nstdout: %s\nstderr: %s",
				sudoed, linted, stdout, stderr)
		}
	})

	t.Run("a plist plutil refuses installs nothing", func(t *testing.T) {
		t.Parallel()

		f := newVMBootInstallFixture(t, hostTargetInstance)
		f.env["SHIFT2EU_TEST_PLUTIL_STATUS"] = "1"

		stdout, stderr, err := f.run(t, "vm-boot-install")
		if err == nil {
			t.Fatalf("make vm-boot-install exited 0 although plutil refused the rendered plist: launchd loads no plist it cannot parse, so the job would never start the VM (issue #398)\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		if !strings.Contains(stderr, "plutil") {
			t.Errorf("the refusal does not name plutil, so a maintainer cannot tell why the plist was refused (issue #398)\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		// The recipe's EXIT trap runs `sudo -k` to forget the timestamp; it runs no command as root.
		for _, args := range f.sudoCommands(t) {
			if slices.Equal(args, []string{"-k"}) {
				continue
			}

			t.Errorf("make vm-boot-install ran `sudo %s` although plutil refused the plist, so root was asked to install a file launchd cannot load (issue #398)\nstdout: %s\nstderr: %s",
				strings.Join(args, " "), stdout, stderr)
		}

		if _, statErr := os.Stat(f.dest()); statErr == nil {
			t.Errorf("make vm-boot-install wrote %s although plutil refused the plist: launchd loads it at the next boot whatever this run decided (issue #398)", f.dest())
		}
	})
}

// TestMakeVMBootRefusesToRunAsRoot pins that the install refuses a run under `sudo -E make vm-boot-install`: the
// plist's UserName is the account that renders it, so a run as root would install a job that starts the
// maintainer's VM as root at every boot. The refusal comes before any sudo, launchctl or limactl call, and before
// any plist is written.
func TestMakeVMBootRefusesToRunAsRoot(t *testing.T) {
	t.Parallel()

	f := newVMBootInstallFixture(t, hostTargetInstance)
	f.sb.useFakeIDAsRoot(t)

	stdout, stderr, err := f.run(t, "vm-boot-install")
	if err == nil {
		t.Fatalf("make vm-boot-install exited 0 with id -u answering 0: the job it installs would start the maintainer's VM as root at every boot\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if !strings.Contains(stderr, "vm-boot-install: refused") {
		t.Errorf("the refusal is not the install recipe's own, which starts with %q: the plist generator refuses a root run too, so a test that only looks for a refusal passes on the generator's, after the recipe's own check and the work before it ran as root (issues #229, #398)\nstdout: %s\nstderr: %s", "vm-boot-install: refused", stdout, stderr)
	}

	refusal := strings.ToLower(stderr)

	for _, want := range []string{"maintainer", "sudo"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not mention %q, so a maintainer who ran sudo -E make vm-boot-install cannot tell how to run it instead:\nstdout: %s\nstderr: %s", want, stdout, stderr)
		}
	}

	for _, tool := range []string{"sudo", "launchctl"} {
		if calls := invocations(f.sb.argvLogText(t), tool); len(calls) != 0 {
			t.Errorf("make vm-boot-install ran %s although it refuses to run as root, so the refusal comes after work root should never have been asked for (issue #398): %v\nstdout: %s\nstderr: %s",
				tool, calls, stdout, stderr)
		}
	}

	if _, statErr := os.Stat(f.dest()); statErr == nil {
		t.Errorf("make vm-boot-install wrote the plist %s although it refuses to run as root: a refused run would still leave a job for launchd to load at the next boot (issue #398)", f.dest())
	}
}

// TestMakeVMBootLaunchdDirComesOnlyFromTheCommandLine pins that VM_BOOT_LAUNCHD_DIR may come from make's command
// line or from the Makefile, never from the environment: a stale export would have root write the plist where
// launchd never looks at boot, and the VM would not come back after the next reboot. Both targets refuse such a
// run before any sudo, and the refusal names the variable and the environment it came from.
func TestMakeVMBootLaunchdDirComesOnlyFromTheCommandLine(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"vm-boot-install", "vm-boot-uninstall"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			f := newVMBootInstallFixture(t, hostTargetInstance)

			stdout, stderr, err := f.runWithTheDirectoryInTheEnvironment(t, target)
			if err == nil {
				t.Fatalf("make %s exited 0 although VM_BOOT_LAUNCHD_DIR came from the environment (%s): a stale export would install the job where launchd never looks at boot (issue #398)\nstdout: %s\nstderr: %s",
					target, f.launchdDir, stdout, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make %s ran sudo although VM_BOOT_LAUNCHD_DIR came from the environment, and the check of where root writes must come first (issue #398): %v\nstdout: %s\nstderr: %s",
					target, calls, stdout, stderr)
			}

			if !strings.Contains(stderr, vmBootLaunchdDir) {
				t.Errorf("the refusal does not name %s, so a maintainer cannot tell which value to fix:\nstderr: %s", vmBootLaunchdDir, stderr)
			}

			if !strings.Contains(strings.ToLower(stderr), "environment") {
				t.Errorf("the refusal does not say that the value came from the environment, so a maintainer would keep looking for it in the Makefile or on the command line:\nstderr: %s", stderr)
			}
		})
	}
}

// TestMakeVMBootIgnoresLIMAsLaunchDaemonsDirFromTheEnvironment pins that LIMA_LAUNCHDAEMONS_DIR is a test hook of
// make's command line and of the Makefile alone (issues #315, #398): a value exported in the environment is put
// back to /Library/LaunchDaemons, where launchd really reads Lima's system job from. A stale export would have
// the check look in a directory launchd never reads, so Lima's job - which starts the VM at every boot - would go
// unreported, and the install would go on beside it. The environment's directory holds that plist, and no report
// names it.
func TestMakeVMBootIgnoresLIMAsLaunchDaemonsDirFromTheEnvironment(t *testing.T) {
	t.Parallel()

	f := newVMBootInstallFixture(t, hostTargetInstance)

	limaLabel := vmBootLimaDaemonLabelPrefix + f.instance

	// Lima's system job's plist where the environment points: what the check must not look at.
	writeFileMode(t, f.limaPlistPath(systemDomain, limaLabel), []byte("Lima's own autostart job of "+f.instance+"\n"), 0o600)

	stdout, stderr, err := f.runWithTheLimaDirectoryInTheEnvironment(t, "vm-boot-install")

	combined := stdout + stderr

	if strings.Contains(combined, f.limaLaunchDaemonsDir) {
		t.Errorf("make vm-boot-install read LIMA_LAUNCHDAEMONS_DIR from the environment (%s, which holds Lima's job %s): the hook is a test hook of make's command line and the Makefile alone, so the environment's value goes back to the directory launchd really reads, like VM_BOOT_LAUNCHD_DIR (issues #315, #398)\nstdout: %s\nstderr: %s",
			f.limaLaunchDaemonsDir, limaLabel, stdout, stderr)
	}

	if strings.Contains(combined, limaLaunchDaemonsDirVar) {
		t.Errorf("make vm-boot-install names %s although it came from the environment, so a maintainer would look for a value to fix on the command line or in the Makefile instead of in the shell that exported it (issues #315, #398)\nstdout: %s\nstderr: %s",
			limaLaunchDaemonsDirVar, stdout, stderr)
	}

	if err != nil && !strings.Contains(combined, limaLabel) {
		t.Fatalf("make vm-boot-install failed before it looked at Lima's job %s: %v\nstdout: %s\nstderr: %s",
			limaLabel, err, stdout, stderr)
	}
}

// TestMakeVMBootUninstallRemovesTheJob pins the other half of issue #398: the uninstall boots the instance's job
// out of the system domain and removes exactly its own plist, leaving another instance's job and any other file of
// the LaunchDaemons directory where they are.
func TestMakeVMBootUninstallRemovesTheJob(t *testing.T) {
	t.Parallel()

	f := newVMBootInstallFixture(t, hostTargetInstance)

	writeFileMode(t, f.dest(), []byte("an installed boot job\n"), 0o600)

	otherInstance := filepath.Join(f.launchdDir, vmBootLabelPrefix+"another-instance.plist")
	writeFileMode(t, otherInstance, []byte("another instance's boot job\n"), 0o600)

	unrelated := filepath.Join(f.launchdDir, "something-else.plist")
	writeFileMode(t, unrelated, []byte("not this project's job\n"), 0o600)

	stdout, stderr, err := f.run(t, "vm-boot-uninstall")
	if err != nil {
		t.Fatalf("make vm-boot-uninstall exited non-zero with %s installed: %v\nstdout: %s\nstderr: %s", f.dest(), err, stdout, stderr)
	}

	wantBootout := []string{"launchctl", "bootout", "system/" + f.label()}

	bootedOut := false

	for _, args := range f.sudoCommands(t) {
		if slices.Equal(args, wantBootout) {
			bootedOut = true
		}
	}

	if !bootedOut {
		t.Errorf("make vm-boot-uninstall ran no `sudo %s`, so the loaded job keeps running until the next boot (issue #398): %v\nstdout: %s\nstderr: %s",
			strings.Join(wantBootout, " "), f.sudoCommands(t), stdout, stderr)
	}

	if !strings.Contains(stdout, strings.Join(wantBootout, " ")) {
		t.Errorf("make vm-boot-uninstall does not print `%s`, so a maintainer does not see the command that ran as root (issue #398)\nstdout:\n%s", strings.Join(append([]string{"sudo"}, wantBootout...), " "), stdout)
	}

	if _, statErr := os.Stat(f.dest()); !os.IsNotExist(statErr) {
		t.Errorf("make vm-boot-uninstall left %s in place, so launchd loads the job again at the next boot (issue #398): %v", f.dest(), statErr)
	}

	for _, kept := range []string{otherInstance, unrelated} {
		if _, statErr := os.Stat(kept); statErr != nil {
			t.Errorf("make vm-boot-uninstall removed %s, which is no plist of %s: the uninstall removes this instance's own job and nothing else (issue #398): %v", kept, f.label(), statErr)
		}
	}
}
