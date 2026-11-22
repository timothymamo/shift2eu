// Pins make vm-up's decision about a Lima instance that is already listed (issue #237): an instance whose
// directory holds no lima.yaml is still listed, with the status Broken, and limactl start <name> then reads
// the name as a template and falls back to template:default - a VM that mounts the whole home directory
// instead of only the workspace. So an instance that is listed but neither Stopped nor Running stops vm-up
// before any limactl start, with a message naming the instance directory, its removal by hand and the launchd
// jobs that may recreate it; Stopped and Running still get limactl start <name>, and an instance that is not
// listed still gets the dev/lima.yaml command line, unchanged.
//
// Removing anything by hand is advice only for a Broken instance whose directory really holds no lima.yaml: a
// lima.yaml Lima cannot load keeps the VM, so that message must point at limactl list instead. Nor may a
// Stopped or Running instance without a lima.yaml be started - limactl start <name> falls back to
// template:default in that window too - and a status query that fails stops vm-up instead of being read as an
// unknown status.
//
// Three further details of the same decision are pinned here (issue #237): a lima.yaml that is a dangling
// symlink counts as present, because the directory may still hold the VM disk; a limactl list --quiet that
// fails stops vm-up before it creates anything, instead of being read as an instance that is not listed; and the
// instance name is matched literally, not as a grep pattern, so a name with a dot matches only itself.
//
// Two last details of the same decision (issue #237): a lima.yaml that is a dangling symlink is present for a
// Stopped or Running instance too, so vm-up neither starts it nor advises removing the directory; and an
// instance name that is not letters, digits, '.', '_' and '-', starting with a letter or digit, is refused
// before any limactl call at all - an empty name matches the empty line an empty listing prints, so it would
// take the branch of a listed instance and could send a maintainer off to remove all of ~/.lima by hand.
//
// An instance that is listed must never be started with limactl start <name>, whatever its status or its
// lima.yaml looks like at the moment of the call (issue #254): lima-vm/lima offers no flag that refuses to create
// an instance, and limactl start <name> falls back to template:default when the instance directory has gone since
// the status check - a VM that mounts the home directory, created without a prompt when there is no terminal. So
// a Stopped or Running instance is started with limactl shell --start, which starts a Stopped instance, only runs
// its command on a Running one and fails for an instance that is not there, and never creates a VM. The tests
// below pin that command line, that no recorded call starts with `start` for a listed instance, and that a shell
// which reports the instance as gone fails vm-up without a limactl call after it. The create line of an instance
// that is not listed stays byte for byte what it was.
//
// The fixture below is shared with vmup_resources_test.go, which pins make vm-up's VM_CPUS, VM_MEMORY, VM_DISK
// and VM_SSH_PORT (issue #228) against the same fake limactl and the same throwaway clone.
package devenv_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// vmUpInstance is the instance every case runs against: the Makefile's default LIMA_INSTANCE.
	vmUpInstance = "shift2eu"
	// vmUpWorkspaceMarker stands in for the directory the VM mounts in a case's expected argv; the fixture
	// only knows it once it has built the clone make runs in.
	vmUpWorkspaceMarker = "<workspace>"
	// vmUpInstanceDirMarker stands in for the directory Lima keeps the instance in, a case's expected stderr;
	// the fixture only knows it once the case has chosen LIMA_HOME.
	vmUpInstanceDirMarker = "<instance-dir>"
	// vmUpDottedInstance is an instance name whose dot grep reads as any character, and vmUpLookalikeInstance
	// is the name that pattern also matches: what limactl list --quiet answers for another instance, which
	// must not pass for vmUpDottedInstance (issue #237).
	vmUpDottedInstance    = "shift2eu.x"
	vmUpLookalikeInstance = "shift2euXx"
)

// statusLimactl stands in for limactl in the vm-up tests: it records its argv in the sandbox's log (a
// tool=limactl line and one arg= line per argument), answers the listing that decides whether the instance
// exists at all with the name SHIFT2EU_TEST_LIMA_INSTANCE (nothing for an instance that is not listed),
// answers any other listing - the status query - with SHIFT2EU_TEST_LIMA_STATUS (nothing for an empty
// status), and lets a start through without running anything: what these tests pin is which command lines
// vm-up runs, never what a VM on this machine would do. A status query answers only when it asks for this
// instance by name, as limactl does: a query about anything else gets nothing. SHIFT2EU_TEST_LIMA_STATUS_FAIL
// stands for a limactl that cannot read the status at all: it says so on stderr and exits non-zero.
// SHIFT2EU_TEST_LIMA_LIST_FAIL stands for a limactl whose existence listing fails: it says so on stderr and
// exits non-zero. A limactl shell answers without running anything either, draining what the recipe pipes into
// it, because make vm-create runs vm-env, vm-claude-settings and vm-init after its vm-up (issue #228).
// SHIFT2EU_TEST_LIMA_SHELL_MISSING stands for the instance the fake's status still reports but Lima no longer
// has: the shell says so on stderr and exits non-zero, as limactl shell does for an instance that is gone, and
// as limactl start <name> would not - it would create the VM from template:default instead (issue #254).
// It records which credentials are set in its own environment too (limactlEnvProbe), so a test can pin that
// neither the App's variables nor a credential reaches limactl at all (issue #326).
const statusLimactl = `#!/bin/sh
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
` + limactlEnvProbe + `if [ "${1:-}" = list ]; then
  # Any listing without --quiet is the status query, whatever spelling of --format it uses.
  for a in "$@"; do
    if [ "${a}" = --quiet ]; then
      if [ -n "${SHIFT2EU_TEST_LIMA_LIST_FAIL:-}" ]; then
        echo 'level=fatal msg="unable to list the instances"' >&2
        exit 1
      fi
      if [ -n "${SHIFT2EU_TEST_LIMA_INSTANCE:-}" ]; then printf '%s\n' "${SHIFT2EU_TEST_LIMA_INSTANCE}"; fi
      exit 0
    fi
  done
  last=''
  for a in "$@"; do last="${a}"; done
  if [ "${last}" != "${SHIFT2EU_TEST_LIMA_INSTANCE:-}" ]; then exit 0; fi
  if [ -n "${SHIFT2EU_TEST_LIMA_STATUS_FAIL:-}" ]; then
    echo 'level=fatal msg="unable to read the instance status"' >&2
    exit 1
  fi
  if [ -n "${SHIFT2EU_TEST_LIMA_STATUS:-}" ]; then printf '%s\n' "${SHIFT2EU_TEST_LIMA_STATUS}"; fi
  exit 0
fi
if [ "${1:-}" = shell ]; then
  if [ -n "${SHIFT2EU_TEST_LIMA_SHELL_MISSING:-}" ]; then
    printf 'instance "%s" does not exist, run limactl create to create a new instance\n' "${SHIFT2EU_TEST_LIMA_INSTANCE}" >&2
    exit 1
  fi
  # make vm-env, make vm-claude-settings and make vm-init pipe their work into limactl shell: drain it, so the
  # writer never meets a closed pipe.
  cat >/dev/null 2>&1 || true
  exit 0
fi
if [ "${1:-}" = start ]; then
  exit 0
fi
echo "fake limactl: unexpected call: $*" >&2
exit 64
`

// vmUpGitShim is the git make's recipes run in the vm-up tests: it answers git config core.hooksPath, the write
// make vm-up's git-hooks prerequisite makes into the checkout's .git/config, without touching a file, and hands
// every other call to the real git. That is defence in depth: no run writes into any .git/config, even if a test
// ever ran make in a real checkout, whose config every worktree shares. The real git's path is shQuote's
// single-quoted word, never a Go-quoted string: sh expands a $(...) or a backtick in a double-quoted path
// (issue #228 review, item D).
const vmUpGitShim = `#!/bin/sh
for a in "$@"; do
  if [ "$a" = core.hooksPath ]; then exit 0; fi
done
exec %s "$@"
`

// writeGitShim writes the git shim into dir, handing every call but git config core.hooksPath to realGit, and
// returns the shim's path.
func writeGitShim(t *testing.T, dir, realGit string) string {
	t.Helper()

	path := filepath.Join(dir, "git")

	writeFileMode(t, path, []byte(fmt.Sprintf(vmUpGitShim, shQuote(realGit))), 0o700)

	return path
}

// shQuote quotes s as one sh word: single quotes, with every ' inside s closed, backslash-escaped and reopened,
// and the whole wrapped in them. sh expands nothing inside it, so a path that carries a $, a backtick or a
// backslash reaches the command as it is.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// vmUpFixture is one throwaway environment for a run of make vm-up or make vm-create: a sandbox whose limactl
// answers the listing and the status its case names, and the maintainer's layout of issue #227 - make runs in a
// clone of the Makefile in the host clone, a clone outside the workspace, and the workspace it mounts holds
// nothing but the repository's own checkout, which WORKSPACE_REPO names. So vm-up's workspace check and the
// paths it hands limactl read the sandbox, and no run touches the repository the tests run from. Both clones
// are git repositories of their own, so the Makefile resolves REPO_ROOT from the host clone it runs in.
type vmUpFixture struct {
	sb   *sandbox
	env  map[string]string
	root string // the host clone make runs in
	// limaInstance is the LIMA_INSTANCE make runs with: vmUpInstance unless a case picks another name.
	limaInstance string
	// marker is a file no run may create: what a value the Makefile got wrong would run (issue #228).
	marker string
	// limaLaunchDaemonsDir is where Lima's own LaunchDaemon plist sits: the sandbox's directory, passed on make's
	// command line as LIMA_LAUNCHDAEMONS_DIR, so no test reads the machine's /Library/LaunchDaemons (issue #398).
	limaLaunchDaemonsDir string
}

// newVMUpFixture builds the fixture for one case: instance is what limactl list --quiet answers (empty for
// an instance that is not listed) and status what the status query answers.
func newVMUpFixture(t *testing.T, instance, status string) *vmUpFixture {
	t.Helper()

	sb := newSandbox(t)

	limactl := filepath.Join(sb.binDir, "limactl")

	writeFileMode(t, limactl, []byte(statusLimactl), 0o700)

	writeGitShim(t, sb.binDir, realTool(t, "git"))

	// make runs in the host clone, a clone outside the workspace (ADR 0031, issue #227), and the workspace holds
	// the repository's own checkout as WORKSPACE_REPO names it: both are git repositories, as the real ones are.
	root := writeClone(t, filepath.Join(sb.root, "host", "shift2eu"))
	writeClone(t, sb.workspaceRepo())

	env := sb.baseEnv()
	env["SHIFT2EU_TEST_LIMA_INSTANCE"] = instance
	env["SHIFT2EU_TEST_LIMA_STATUS"] = status

	// make vm-create runs make vm-env after its vm-up, and vm-env refuses a VM_ENV name that is unset or empty
	// (issue #234). The fixture's environment holds none of the test machine's own (CLAUDE.md rule 3), so the
	// VM_ENV names get invented values here, with the same helper the issue #234 tests use: obviously fake
	// ones, never a credential of this machine.
	for _, name := range vmEnvCredentials {
		env[name] = fakeEnvValue(name)
	}

	// vm-up asks launchctl about Lima's own autostart job of the instance (issue #315), so the sandbox's recording
	// launchctl stands in for the machine's, with no job loaded: no run of make vm-up reaches a real launchd, and
	// no job of the machine can make a warning appear (issue #398).
	sb.useFakeLaunchctl(t, env, "113")

	// Lima's own LaunchDaemon plist lives in the system LaunchDaemons directory, /Library/LaunchDaemons on the
	// Mac: a directory of the sandbox's own travels on make's command line, so no run reads the machine's (issue
	// #398).
	limaLaunchDaemonsDir := filepath.Join(sb.root, "LimaLaunchDaemons")
	if err := os.MkdirAll(limaLaunchDaemonsDir, 0o700); err != nil {
		t.Fatalf("create %s: %v", limaLaunchDaemonsDir, err)
	}

	return &vmUpFixture{
		sb:                   sb,
		env:                  env,
		root:                 root,
		limaInstance:         vmUpInstance,
		marker:               filepath.Join(sb.root, "marker-a-value-ran"),
		limaLaunchDaemonsDir: limaLaunchDaemonsDir,
	}
}

// workspace is the directory the run mounts: the parent of the checkout WORKSPACE_REPO names.
func (f *vmUpFixture) workspace() string {
	return filepath.Dir(f.env["WORKSPACE_REPO"])
}

// instanceDir is the directory Lima keeps the instance in, as a maintainer has to find it to remove it:
// $LIMA_HOME/<name> when LIMA_HOME is set, else $HOME/.lima/<name>.
func (f *vmUpFixture) instanceDir() string {
	if home := f.env["LIMA_HOME"]; home != "" {
		return filepath.Join(home, f.limaInstance)
	}

	return filepath.Join(f.env["HOME"], ".lima", f.limaInstance)
}

// writeInstanceLimaYaml gives the instance a lima.yaml, as an instance that was started once has one: it is
// what limactl start <name> loads, and without it the name is read as a template and falls back to
// template:default. Call it after the case has chosen LIMA_HOME.
func (f *vmUpFixture) writeInstanceLimaYaml(t *testing.T) {
	t.Helper()

	writeFileMode(t, filepath.Join(f.instanceDir(), "lima.yaml"), []byte("name: "+f.limaInstance+"\n"), 0o600)
}

// symlinkInstanceLimaYaml gives the instance directory a lima.yaml that is a dangling symlink: the entry Lima
// looks for is there, but nothing is behind it, as after a restore or a moved file. Call it after the case has
// chosen LIMA_HOME.
func (f *vmUpFixture) symlinkInstanceLimaYaml(t *testing.T) {
	t.Helper()

	dir := f.instanceDir()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}

	link, target := filepath.Join(dir, "lima.yaml"), filepath.Join(f.sb.root, "no-such-lima", "lima.yaml")

	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s to %s: %v", link, target, err)
	}
}

// run runs make with args in the clone, with the fixture's environment alone, and LIMA_LAUNCHDAEMONS_DIR on
// make's command line: the Makefile reads that test hook from nowhere else, and the sandbox's directory holds no
// plist of the machine's (issue #398).
func (f *vmUpFixture) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	makeArgs := append(slices.Clone(args), limaLaunchDaemonsDirVar+"="+f.limaLaunchDaemonsDir)

	return runMakeIn(t, f.root, makeArgs, f.env)
}

// statusQueries returns the argv of every limactl list that is not the existence check (--quiet), in call
// order: the status query vm-up makes about a listed instance.
func (f *vmUpFixture) statusQueries(t *testing.T) [][]string {
	t.Helper()

	var queries [][]string

	for _, args := range invocations(f.sb.argvLogText(t), "limactl") {
		if len(args) == 0 || args[0] != "list" || slices.Contains(args, "--quiet") {
			continue
		}

		queries = append(queries, args)
	}

	return queries
}

// vmUpStartExistingArgs is the argv make vm-up starts an instance that is listed and Stopped or Running with
// (issue #254): limactl shell --start runs the command in the VM, starting a Stopped instance first and doing
// nothing to a Running one, and fails with "instance ... does not exist" for an instance that is gone - limactl
// start <name> would create it from template:default instead. --workdir / keeps the command out of every mounted
// directory and true is the whole command.
func vmUpStartExistingArgs(instance string) []string {
	return []string{"shell", "--start", "--workdir", "/", instance, "true"}
}

// startExistingArgs is vmUpStartExistingArgs for the instance make in this fixture runs with.
func (f *vmUpFixture) startExistingArgs() []string {
	return vmUpStartExistingArgs(f.env["LIMA_INSTANCE"])
}

// starting returns the argv of every limactl call that starts a VM, in call order (issue #254): the
// dev/lima.yaml create line of an instance that is not listed, and the limactl shell --start of one that is
// listed. A limactl shell without --start (make vm-env, make vm-init) runs a command and starts nothing.
func (f *vmUpFixture) starting(t *testing.T) [][]string {
	t.Helper()

	var starting [][]string

	for _, args := range invocations(f.sb.argvLogText(t), "limactl") {
		if len(args) == 0 {
			continue
		}

		if args[0] == "start" || (args[0] == "shell" && slices.Contains(args, "--start")) {
			starting = append(starting, args)
		}
	}

	return starting
}

// starts returns the argv of every limactl start the run made, in call order. For an instance that is listed
// this must stay empty however the run ends, because limactl start <name> creates a VM from template:default
// when the instance has gone since the status check (issue #254).
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

// creating is the argv of the one limactl start that creates the VM from dev/lima.yaml (issue #228).
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
		"--mount-only", f.workspace() + ":w",
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

// TestVMUpRefusesAnInstanceThatIsNotStoppedOrRunning pins the refusal: with a listed instance whose status
// is Broken, Uninitialized, Installing, empty or one vm-up does not know, make vm-up exits non-zero and runs
// no limactl start at all, and a Broken instance's message says which directory to remove by hand and which
// launchd jobs may be recreating it (issue #237). A Broken instance whose directory does hold a lima.yaml
// keeps its VM: that message must not send a maintainer off to remove it by hand, and points at limactl list
// instead - and a lima.yaml that is a dangling symlink still counts as there, since the directory may hold an
// intact disk behind it. When the status query itself fails, vm-up stops rather than reading the empty answer
// as an unknown status.
func TestVMUpRefusesAnInstanceThatIsNotStoppedOrRunning(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		status       string
		limaHome     bool     // LIMA_HOME names the directory Lima keeps its instances in
		limaYaml     bool     // the instance directory holds a lima.yaml
		limaYamlLink bool     // the instance directory holds a lima.yaml that is a dangling symlink instead
		statusFail   bool     // the status query itself fails, whatever status the instance has
		broken       bool     // the message must name the directory, the removal by hand and the launchd jobs
		wantStatus   string   // the status the message must name; empty means nothing to look for
		want         []string // further substrings stderr must hold
		forbid       []string // substrings stderr must not hold
	}{
		{
			name:     "a broken instance whose directory holds no lima.yaml, with LIMA_HOME set",
			status:   "Broken",
			limaHome: true,
			broken:   true,
		},
		{
			name:   "a broken instance whose directory holds no lima.yaml, with LIMA_HOME unset",
			status: "Broken",
			broken: true,
		},
		{
			name:     "a broken instance whose lima.yaml is there",
			status:   "Broken",
			limaYaml: true,
			want:     []string{vmUpInstance, "Broken", "limactl list"},
			forbid:   []string{"by hand"},
		},
		{
			name:         "a broken instance whose lima.yaml is a dangling symlink, with LIMA_HOME set",
			status:       "Broken",
			limaHome:     true,
			limaYamlLink: true,
			want:         []string{vmUpInstance, "Broken", "limactl list"},
			forbid:       []string{"by hand"},
		},
		{
			name:       "an instance that is uninitialized",
			status:     "Uninitialized",
			wantStatus: "Uninitialized",
		},
		{
			name:       "an instance that is installing",
			status:     "Installing",
			wantStatus: "Installing",
		},
		{
			name:   "an instance whose status is empty",
			status: "",
		},
		{
			name:       "an instance whose status vm-up does not know",
			status:     "Weird",
			wantStatus: "Weird",
		},
		{
			name:       "an instance whose status query fails",
			status:     "Stopped",
			statusFail: true,
			want:       []string{"could not read the status"},
			// A failed query is not an unknown status: the empty answer must never reach the case that
			// names a status vm-up does not know.
			forbid: []string{"has the status"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, vmUpInstance, tc.status)

			if tc.limaHome {
				f.env["LIMA_HOME"] = filepath.Join(f.sb.root, "lima-home")
			}

			if tc.limaYaml {
				f.writeInstanceLimaYaml(t)
			}

			if tc.limaYamlLink {
				f.symlinkInstanceLimaYaml(t)
			}

			if tc.statusFail {
				f.env["SHIFT2EU_TEST_LIMA_STATUS_FAIL"] = "1"
			}

			stdout, stderr, err := f.run(t, "vm-up")
			if err == nil {
				t.Fatalf("make vm-up exited 0 with the instance listed as %q: it must stop before limactl start, which turns a name it cannot load into template:default, a VM that mounts the whole home directory (issue #237)\nstdout: %s\nstderr: %s",
					tc.status, stdout, stderr)
			}

			if starts := f.starts(t); len(starts) != 0 {
				t.Errorf("make vm-up ran limactl start %v with the instance listed as %q: no start may run before the refusal, or Lima falls back to template:default (issue #237)",
					starts[0], tc.status)
			}

			want := make([]string, 0, 9)
			if tc.broken {
				want = append(want,
					f.instanceDir(),
					"by hand",
					"launchctl print system | grep -i lima",
					"~/Library/LaunchAgents",
					"/Library/LaunchDaemons",
				)
			}

			if tc.wantStatus != "" {
				want = append(want, tc.wantStatus)
			}

			want = append(want, tc.want...)

			for _, w := range want {
				if !strings.Contains(stderr, w) {
					t.Errorf("make vm-up's stderr does not say %q with the instance listed as %q, so a maintainer cannot see why vm-up stopped or what to do about it (issue #237)\nstderr: %s",
						w, tc.status, stderr)
				}
			}

			for _, w := range tc.forbid {
				if strings.Contains(stderr, w) {
					t.Errorf("make vm-up's stderr says %q with the instance listed as %q, which this case forbids: a lima.yaml Lima cannot load is not a reason to send a maintainer off to remove the VM by hand, and a status query that fails is not an unknown status (issue #237)\nstderr: %s",
						w, tc.status, stderr)
				}
			}
		})
	}
}

// TestVMUpStopsWhenItCannotListInstances pins what the existence check does with a limactl list --quiet that
// fails (issue #237): the empty answer is not an instance that is not listed, so vm-up stops before it creates
// anything and says it could not list the instances - the dev/lima.yaml create line would otherwise build a VM
// on a name that may already exist, with a VM disk of its own.
func TestVMUpStopsWhenItCannotListInstances(t *testing.T) {
	t.Parallel()

	f := newVMUpFixture(t, vmUpInstance, "Stopped")
	f.env["SHIFT2EU_TEST_LIMA_LIST_FAIL"] = "1"

	stdout, stderr, err := f.run(t, "vm-up")
	if err == nil {
		t.Fatalf("make vm-up exited 0 although limactl list --quiet failed: a listing vm-up cannot read is not an instance that is not listed, and creating one on the strength of it may collide with the VM that is there (issue #237)\nstdout: %s\nstderr: %s",
			stdout, stderr)
	}

	if starts := f.starts(t); len(starts) != 0 {
		t.Errorf("make vm-up ran limactl start %v although limactl list --quiet failed: it must stop before creating or starting anything it cannot see (issue #237)",
			starts[0])
	}

	if !strings.Contains(stderr, "could not list") {
		t.Errorf("make vm-up's stderr does not say %q although limactl list --quiet failed, so a maintainer cannot see that the instance could not be looked up at all (issue #237)\nstderr: %s",
			"could not list", stderr)
	}
}

// TestVMUpStartsOrCreatesTheInstance pins the other half of issue #237: an instance that is Stopped or
// Running and whose directory holds a lima.yaml is started with exactly one limactl shell --start, an instance
// that is not listed gets exactly the dev/lima.yaml command line with this repository's workspace mounted,
// unchanged, and a listed instance whose directory holds no lima.yaml is started by neither branch: limactl
// start <name> would read the name as a template and fall back to template:default (issue #237). A lima.yaml
// that is a dangling symlink is there for a Stopped or Running instance as well: the directory may still hold
// an intact disk, so that case stops too, pointing at limactl list instead of a removal by hand. The status
// of a listed instance is read by asking limactl for that instance by name, never by any other query, and an
// instance that is not listed gets no status query at all: there is no status to read.
//
// The start of a listed instance is a limactl shell --start and never a limactl start <name> (issue #254):
// limactl start <name> creates the VM from template:default when the instance directory has gone since the
// status check, which no flag of Lima 2.x refuses, while limactl shell --start only ever starts an instance
// that is there. No call of a listed instance may therefore start with `start` as its first argument.
func TestVMUpStartsOrCreatesTheInstance(t *testing.T) {
	t.Parallel()

	// statusQuery is the one status query vm-up makes about a listed instance: limactl asked for this instance
	// by name, so a query about any other name reads nothing.
	statusQuery := []string{"list", "--format", "{{.Status}}", vmUpInstance}

	cases := []struct {
		name         string
		instance     string   // what limactl list --quiet answers; empty means the instance is not listed
		status       string   // what the status query answers, unread when the instance is not listed
		limaHome     bool     // LIMA_HOME names the directory Lima keeps its instances in
		limaYaml     bool     // the instance directory holds a lima.yaml
		limaYamlLink bool     // the instance directory holds a lima.yaml that is a dangling symlink instead
		limaInstance string   // the LIMA_INSTANCE make runs with; empty means vmUpInstance
		listed       bool     // limactl lists this instance: vm-up must start it without ever running limactl start
		want         []string // the one limactl call that starts the VM; nil means vm-up must refuse to start
		wantStderr   []string // substrings stderr must hold when vm-up refuses; <instance-dir> stands for the directory
		forbidStderr []string // substrings stderr must not hold when vm-up refuses
		// wantQueries is the status query vm-up must make, in call order: nil, the zero value, means it
		// must make none at all, as for an instance that is not listed and has no status to read.
		wantQueries [][]string
	}{
		{
			name:        "an instance that is stopped",
			instance:    vmUpInstance,
			status:      "Stopped",
			limaHome:    true,
			limaYaml:    true,
			listed:      true,
			want:        vmUpStartExistingArgs(vmUpInstance),
			wantQueries: [][]string{statusQuery},
		},
		{
			name:        "an instance that is running",
			instance:    vmUpInstance,
			status:      "Running",
			limaYaml:    true,
			listed:      true,
			want:        vmUpStartExistingArgs(vmUpInstance),
			wantQueries: [][]string{statusQuery},
		},
		{
			name:       "a stopped instance whose directory holds no lima.yaml",
			instance:   vmUpInstance,
			status:     "Stopped",
			wantStderr: []string{vmUpInstanceDirMarker, "lima.yaml"},
		},
		{
			name:       "a running instance whose directory holds no lima.yaml, with LIMA_HOME set",
			instance:   vmUpInstance,
			status:     "Running",
			limaHome:   true,
			wantStderr: []string{vmUpInstanceDirMarker, "lima.yaml"},
		},
		{
			// A dangling lima.yaml is an entry Lima looks for: the instance may hold an intact disk behind it,
			// so this case must not start it and must not advise a removal by hand either.
			name:         "a stopped instance whose lima.yaml is a dangling symlink",
			instance:     vmUpInstance,
			status:       "Stopped",
			limaYamlLink: true,
			wantStderr:   []string{"limactl list"},
			forbidStderr: []string{"by hand"},
		},
		{
			name:         "a running instance whose lima.yaml is a dangling symlink, with LIMA_HOME set",
			instance:     vmUpInstance,
			status:       "Running",
			limaHome:     true,
			limaYamlLink: true,
			wantStderr:   []string{"limactl list"},
			forbidStderr: []string{"by hand"},
		},
		{
			name:     "an instance that is not listed",
			instance: "",
			want: []string{
				"start",
				"--name=" + vmUpInstance,
				"--mount-only",
				vmUpWorkspaceMarker + ":w",
				"--tty=false",
				"dev/lima.yaml",
			},
		},
		{
			// A dotted name is a grep pattern that also matches a name with any character where the dot
			// is, so a listed look-alike must not pass for this instance: an instance that is not listed
			// gets the create line, and no status query reads the look-alike's status.
			name:         "an instance whose name holds a dot, when limactl lists a look-alike",
			instance:     vmUpLookalikeInstance,
			limaInstance: vmUpDottedInstance,
			want: []string{
				"start",
				"--name=" + vmUpDottedInstance,
				"--mount-only",
				vmUpWorkspaceMarker + ":w",
				"--tty=false",
				"dev/lima.yaml",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, tc.instance, tc.status)

			if tc.limaInstance != "" {
				f.limaInstance = tc.limaInstance
				f.env["LIMA_INSTANCE"] = tc.limaInstance
			}

			if tc.limaHome {
				f.env["LIMA_HOME"] = filepath.Join(f.sb.root, "lima-home")
			}

			if tc.limaYaml {
				f.writeInstanceLimaYaml(t)
			}

			if tc.limaYamlLink {
				f.symlinkInstanceLimaYaml(t)
			}

			stdout, stderr, err := f.run(t, "vm-up")

			if tc.want == nil {
				if err == nil {
					t.Fatalf("make vm-up %s exited 0, but this case must stop vm-up before any limactl start (issue #237): an instance whose lima.yaml is missing or cannot be loaded must not be started, or limactl start <name> reads the name as a template and falls back to template:default, a VM that mounts the whole home directory\nstdout: %s\nstderr: %s",
						tc.name, stdout, stderr)
				}

				if calls := f.starting(t); len(calls) != 0 {
					t.Errorf("make vm-up %s ran limactl %v: a listed instance without a lima.yaml must never be started, or Lima falls back to template:default (issue #237), and a limactl shell --start is a start too (issue #254)",
						tc.name, calls[0])
				}

				for _, w := range tc.wantStderr {
					w = strings.ReplaceAll(w, vmUpInstanceDirMarker, f.instanceDir())
					if !strings.Contains(stderr, w) {
						t.Errorf("make vm-up's stderr does not say %q for %s, so a maintainer cannot see which instance directory is at fault, what is missing from it, or where to look next (issue #237)\nstderr: %s",
							w, tc.name, stderr)
					}
				}

				for _, w := range tc.forbidStderr {
					if strings.Contains(stderr, w) {
						t.Errorf("make vm-up's stderr says %q for %s, which this case forbids: a lima.yaml that is there, even as a dangling symlink, is not a reason to send a maintainer off to remove the instance by hand, or to look for the reason somewhere else than limactl list (issue #237)\nstderr: %s",
							w, tc.name, stderr)
					}
				}

				return
			}

			if err != nil {
				t.Fatalf("make vm-up %s: %v\nstdout: %s\nstderr: %s", tc.name, err, stdout, stderr)
			}

			want := slices.Clone(tc.want)
			for i, arg := range want {
				want[i] = strings.ReplaceAll(arg, vmUpWorkspaceMarker, f.workspace())
			}

			if tc.listed {
				if starts := f.starts(t); len(starts) != 0 {
					t.Errorf("make vm-up %s ran limactl start %v for an instance that is listed: limactl start <name> creates a VM from template:default when the instance directory has gone since the status check, and no flag of Lima refuses that - a listed instance is started with limactl shell --start, which never creates one (issue #254)",
						tc.name, starts[0])
				}
			}

			starting := f.starting(t)
			if len(starting) != 1 {
				t.Fatalf("make vm-up %s ran %d limactl calls that start a VM, want exactly one (%v)\nstdout: %s\nstderr: %s",
					tc.name, len(starting), tc.want, stdout, stderr)
			}

			if diff := cmp.Diff(want, starting[0]); diff != "" {
				t.Errorf("make vm-up %s started the VM with the wrong limactl argv (issue #254) (-want +got):\n%s", tc.name, diff)
			}

			if diff := cmp.Diff(tc.wantQueries, f.statusQueries(t)); diff != "" {
				t.Errorf("make vm-up %s read the status of the instance with the wrong limactl calls (-want +got), or read a status an instance that is not listed does not have:\n%s", tc.name, diff)
			}
		})
	}
}

// vmUpStartGoneMessage is what the fake limactl's shell says for an instance the status still reports but
// Lima no longer has, in the words limactl shell uses.
const vmUpStartGoneMessage = `does not exist, run limactl create to create a new instance`

// TestVMUpFailsWhenTheInstanceIsGoneAtTheStart pins the window a shell check cannot close (issue #254):
// between vm-up's status query and the call that starts the VM, the instance directory can disappear - the
// launchd job of issue #229, a removal by hand, a concurrent make. limactl start <name> then reads the name as
// a template and creates template:default unattended, a VM that mounts the home directory; limactl shell --start
// fails instead. So the shell failing the way limactl shell does must fail make vm-up, and no limactl call may
// follow it: a later start would build the default VM the whole decision is meant to prevent.
func TestVMUpFailsWhenTheInstanceIsGoneAtTheStart(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"Stopped", "Running"} {
		t.Run("the instance is gone by the time it is started: "+status, func(t *testing.T) {
			t.Parallel()

			// The instance pass the #237 checks: listed, Stopped or Running, with the lima.yaml an
			// instance that was started once has. Only the start itself meets the instance gone (issue #254).
			f := newVMUpFixture(t, vmUpInstance, status)
			f.writeInstanceLimaYaml(t)
			f.env["SHIFT2EU_TEST_LIMA_SHELL_MISSING"] = "1"

			stdout, stderr, err := f.run(t, "vm-up")
			if err == nil {
				t.Fatalf("make vm-up exited 0 although the instance was gone when it started it: a start that reports success without a VM is the one that creates template:default, a VM that mounts the whole home directory (issue #254)\nstdout: %s\nstderr: %s",
					stdout, stderr)
			}

			if starts := f.starts(t); len(starts) != 0 {
				t.Errorf("make vm-up ran limactl start %v for an instance that is listed: the same call creates the VM from template:default when the instance directory is gone, the window this case opens (issue #254)",
					starts[0])
			}

			calls := invocations(f.sb.argvLogText(t), "limactl")
			if len(calls) == 0 {
				t.Fatalf("make vm-up ran no limactl call at all, so this case pins nothing (issue #254)\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if diff := cmp.Diff(f.startExistingArgs(), calls[len(calls)-1]); diff != "" {
				t.Errorf("make vm-up's last limactl call is not the start that failed (-want +got):\n%s\nit went on after the start reported the instance as gone, and anything it runs next can create the VM the decision is meant to prevent (issue #254)", diff)
			}

			if !strings.Contains(stderr, vmUpStartGoneMessage) {
				t.Errorf("make vm-up's stderr does not hold limactl shell's %q, so a maintainer cannot see why the run stopped (issue #254)\nstderr: %s",
					vmUpStartGoneMessage, stderr)
			}
		})
	}
}

// vmUpProbeMakefile answers the LIMA_INSTANCE make reads, with the same ?= the repository's Makefile uses, so a
// case can prove its empty name reached make as an empty value and not as the recipe's default.
const vmUpProbeMakefile = `LIMA_INSTANCE ?= shift2eu
probe:
	@printf 'LIMA_INSTANCE=[%s]\n' "$(LIMA_INSTANCE)"
`

// assertMakeSeesEmptyInstance fails unless make, run with the fixture's environment, reads LIMA_INSTANCE as
// defined and empty: an environment variable set to an empty value is defined for ?=, so the empty name is
// what the recipe sees. Without it the empty-name case would silently run as the default instance and pin
// nothing.
func assertMakeSeesEmptyInstance(t *testing.T, f *vmUpFixture) {
	t.Helper()

	probe := filepath.Join(f.sb.root, "probe.mk")
	writeFileMode(t, probe, []byte(vmUpProbeMakefile), 0o600)

	stdout, stderr, err := runMakeIn(t, f.root, []string{"-f", probe, "probe"}, f.env)
	if err != nil {
		t.Fatalf("make -f %s probe: %v\nstdout: %s\nstderr: %s", probe, err, stdout, stderr)
	}

	if !strings.Contains(stdout, "LIMA_INSTANCE=[]") {
		t.Fatalf("make reads LIMA_INSTANCE as %q although the test sets it to an empty value: a variable make does not see as defined stays the recipe's default, so the empty-name case would pin nothing (issue #237)\nstdout: %s",
			strings.TrimSpace(stdout), stdout)
	}
}

// TestVMUpRefusesAnInvalidInstanceName pins that vm-up checks the instance name before it calls limactl at all
// (issue #237): only letters, digits, '.', '_' and '-', starting with a letter or digit, name an instance.
// Anything else - an empty name, a leading dash limactl would read as an option, a slash that turns the
// instance directory into a path below ~/.lima, a space - stops vm-up with a message naming LIMA_INSTANCE, and
// no limactl call runs. An empty name in particular matches the single empty line an empty listing prints, so
// vm-up would take the branch of a listed instance, look in $HOME/.lima itself and could send a maintainer off
// to remove all of it by hand.
func TestVMUpRefusesAnInvalidInstanceName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		limaInstance string // the LIMA_INSTANCE make runs with, as an environment variable
		status       string // what the status query would answer, should vm-up make one
	}{
		{limaInstance: "", status: "Broken", name: "an empty name"},
		{limaInstance: "-x", status: "Stopped", name: "a name with a leading dash"},
		{limaInstance: "a/b", status: "Stopped", name: "a name with a slash"},
		{limaInstance: "a b", status: "Stopped", name: "a name with a space"},
		{limaInstance: "_x", status: "Stopped", name: "a name with a leading underscore"},
		{limaInstance: ".hidden", status: "Stopped", name: "a name with a leading dot"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Nothing is listed, as an empty listing answers: the empty name is the one that matches it.
			f := newVMUpFixture(t, "", tc.status)
			f.env["LIMA_INSTANCE"] = tc.limaInstance

			if tc.limaInstance == "" {
				assertMakeSeesEmptyInstance(t, f)
			}

			stdout, stderr, err := f.run(t, "vm-up")
			if err == nil {
				t.Fatalf("make vm-up exited 0 with LIMA_INSTANCE=%q: an invalid instance name must stop vm-up, or limactl reads it as an option, a path or nothing at all and the name that is started is not the one LIMA_INSTANCE asks for (issue #237)\nstdout: %s\nstderr: %s",
					tc.limaInstance, stdout, stderr)
			}

			if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) != 0 {
				t.Errorf("make vm-up ran limactl %v with LIMA_INSTANCE=%q: the name must be checked before any limactl call, or vm-up takes a name it cannot use for an instance that is listed and looks in a directory that is not one instance's (issue #237)",
					calls[0], tc.limaInstance)
			}

			if !strings.Contains(stderr, "LIMA_INSTANCE") {
				t.Errorf("make vm-up's stderr does not say %q with LIMA_INSTANCE=%q, so a maintainer cannot see which variable to fix (issue #237)\nstderr: %s",
					"LIMA_INSTANCE", tc.limaInstance, stderr)
			}
		})
	}
}

// TestVMUpWarnsAboutALimaAutostartJob pins issue #315: make vm-up warns when Lima's own autostart job starts this
// instance. `limactl autostart enable` writes io.lima-vm.autostart.<instance> in the user's domain and, with
// --condition=boot, io.lima-vm.daemon.<instance> as a LaunchDaemon, and both run
// `limactl start <name> --foreground` - the form that can fall back to template:default, a VM that mounts the home
// directory (issue #237). vm-up warns rather than refuses: the VM still starts, the exit status stays 0, and the
// warning names the job and `limactl autostart disable <instance>`, so a maintainer can remove it. The job is
// found loaded too, through `launchctl print`, and as the installed plist alone (issue #398): launchd loads that
// file at the next login or boot, so the warning names the file as well, since no `launchctl print` knows it.
// Each warning ends with the removal by hand, as a maintainer pastes it: the bootout of a loaded job and
// `rm -f <plist>`, with sudo in the system domain only. With neither job it says nothing, and on a host without
// launchctl - Linux - it still looks for the plist on disk and warns when there is one.
func TestVMUpWarnsAboutALimaAutostartJob(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// domain is the launchd domain Lima's job would be loaded in; load says the case loads one, installed
		// says it only writes the job's plist file (issues #315, #398).
		domain    jobDomain
		prefix    string
		load      bool
		installed bool
		// withoutLaunchctl runs make on a PATH that has no launchctl, as Linux CI has none.
		withoutLaunchctl bool
		wantWarn         bool
	}{
		{
			name:     "the user domain's loaded io.lima-vm.autostart job",
			domain:   userDomain,
			prefix:   vmBootLimaAutostartLabelPrefix,
			load:     true,
			wantWarn: true,
		},
		{
			name:     "the system domain's loaded io.lima-vm.daemon job",
			domain:   systemDomain,
			prefix:   vmBootLimaDaemonLabelPrefix,
			load:     true,
			wantWarn: true,
		},
		{
			name:      "the user domain's installed io.lima-vm.autostart plist, not loaded",
			domain:    userDomain,
			prefix:    vmBootLimaAutostartLabelPrefix,
			installed: true,
			wantWarn:  true,
		},
		{
			name:      "the system domain's installed io.lima-vm.daemon plist, not loaded",
			domain:    systemDomain,
			prefix:    vmBootLimaDaemonLabelPrefix,
			installed: true,
			wantWarn:  true,
		},
		{name: "no Lima autostart job"},
		{name: "no launchctl at all, as on Linux", withoutLaunchctl: true},
		{
			// A host without launchctl still looks for Lima's plist on disk: an SSH-only session has no gui/<uid>
			// domain to ask, and a plist nothing has loaded yet starts the VM at the next login or boot (issue
			// #398).
			name:             "no launchctl at all, and the user domain's installed plist on disk",
			domain:           userDomain,
			prefix:           vmBootLimaAutostartLabelPrefix,
			installed:        true,
			withoutLaunchctl: true,
			wantWarn:         true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// An instance that is listed, Stopped and holds a lima.yaml passes vm-up's checks, so the VM is
			// started and the warning is the only thing this case adds.
			f := newVMUpFixture(t, vmUpInstance, "Stopped")
			f.writeInstanceLimaYaml(t)

			jobs := newLaunchdJobs(t, f.sb)
			jobs.attach(f.env)

			label, plist := "", ""
			if tc.load || tc.installed {
				label = tc.prefix + vmUpInstance
				plist = limaAutostartPlistPath(tc.domain, f.env["HOME"], f.limaLaunchDaemonsDir, label)
			}

			switch {
			case tc.load:
				// Lima's job as `limactl autostart enable` loads it.
				jobs.add(t, tc.domain, label, loadedJob{args: []string{"/opt/homebrew/bin/limactl", "start", vmUpInstance, "--foreground"}})
			case tc.installed:
				// Lima's job's plist as `limactl autostart enable` writes it: launchd loads it at the next login
				// or boot, so it starts the VM then although nothing runs now (issue #398).
				writeFileMode(t, plist, []byte("Lima's own autostart job of "+vmUpInstance+"\n"), 0o600)
			}

			if tc.withoutLaunchctl {
				if err := os.Remove(filepath.Join(f.sb.binDir, "launchctl")); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("remove the fake launchctl from %s: %v", f.sb.binDir, err)
				}

				// /usr/local/bin is dropped too: a Mac keeps no launchctl there, but the PATH of the run must
				// hold nothing the recipe could find one in.
				f.env["PATH"] = f.sb.binDir + ":" + pathWithout(t, "launchctl")
			}

			stdout, stderr, err := f.run(t, "vm-up")
			if err != nil {
				t.Fatalf("make vm-up exited non-zero for %s: a Lima autostart job is a warning, never a refusal, and a host without launchctl has nothing to warn about: %v\nstdout: %s\nstderr: %s",
					tc.name, err, stdout, stderr)
			}

			// The VM is started either way: the warning changes nothing about the start (issue #315).
			if diff := cmp.Diff([][]string{vmUpStartExistingArgs(vmUpInstance)}, f.starting(t)); diff != "" {
				t.Errorf("make vm-up with %s did not start the VM the way it always does (-want +got):\n%s\nstdout: %s\nstderr: %s",
					tc.name, diff, stdout, stderr)
			}

			combined := stdout + stderr

			if !tc.wantWarn {
				for _, forbidden := range []string{"io.lima-vm.", "autostart"} {
					if strings.Contains(combined, forbidden) {
						t.Errorf("make vm-up warns about %q for %s although no Lima autostart job starts %s: a warning that fires when there is nothing to remove sends a maintainer looking for a job that is not there (issue #315)\nstdout: %s\nstderr: %s",
							forbidden, tc.name, vmUpInstance, stdout, stderr)
					}
				}

				return
			}

			wants := []string{label, "limactl autostart disable " + vmUpInstance}

			// The removal by hand, whole and as a maintainer pastes it: the bootout of a loaded job and
			// `rm -f <plist>`, with sudo in the system domain, where root owns the plist, and never in the user's,
			// where the plist is the maintainer's own (issues #315, #398).
			removal := "rm -f " + plist
			if tc.domain == systemDomain {
				removal = "sudo " + removal
			}

			if tc.load {
				if tc.domain == systemDomain {
					wants = append(wants, "sudo launchctl bootout system/"+label)
				} else {
					wants = append(wants, "launchctl bootout gui/"+strconv.Itoa(os.Getuid())+"/"+label)
				}
			}

			wants = append(wants, removal)

			if tc.installed {
				// No launchd has loaded this job, so no launchctl print knows it: the warning names the plist
				// file itself, or a maintainer cannot find what starts the VM twice (issue #398).
				wants = append(wants, plist)
			}

			for _, want := range wants {
				if !strings.Contains(combined, want) {
					t.Errorf("make vm-up does not warn with %q although %s starts %s: the warning must name the job, its plist where nothing has loaded it, the command that removes it the supported way and the removal by hand, or a maintainer cannot act on it (issues #315, #237, #398)\nstdout: %s\nstderr: %s",
						want, label, vmUpInstance, stdout, stderr)
				}
			}

			// The user's plist belongs to the maintainer: no advice may put sudo in front of removing it (issue
			// #398).
			if sudoRemoval := "sudo " + removal; tc.domain == userDomain && strings.Contains(combined, sudoRemoval) {
				t.Errorf("make vm-up warns with `%s` although Lima's autostart plist %s is the maintainer's own: only the system domain's plist is root's, and sudo would run the maintainer's own removal as root (issues #315, #398)\nstdout: %s\nstderr: %s",
					sudoRemoval, plist, stdout, stderr)
			}
		})
	}
}

// TestVMUpDoesNotRepeatAnUnsafeLimaPlistPath pins the security audit's L1 finding on #398 for make vm-up: the
// warning's removal by hand prints `rm -f <plist>` for Lima's user-domain job, whose plist lives under $HOME, and a
// maintainer's home may hold a space (/Users/John Doe) or a shell metacharacter. Pasted as it stands, that path
// splits the command into another one, expands a variable or names another file.
//
// The repository's rule for paths in pasteable advice is the one dev/agents-token-launchd.sh follows for a foreign
// job's plist and TestAgentsTokenLaunchdForeignJobsDoesNotRepeatAnUnsafePlistPath pins (auditor nit, issue #396):
// only an absolute path of / and [A-Za-z0-9._-], without a .. segment, is repeated, and the warning says where to
// see the path instead. The job is still named and the VM still starts: a Lima autostart job is a warning, never a
// refusal (issue #315).
func TestVMUpDoesNotRepeatAnUnsafeLimaPlistPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// home is the last component of the fixture's HOME: the sandbox's own directory, so the case adds the
		// character no path in this test is expected to hold and nothing else about the run changes.
		home string
		// loaded has launchd serve the job as loaded in the user's domain, as `limactl autostart enable` leaves
		// it: the warning then advises the bootout of a loaded job, whose plist path must be held back as well.
		loaded bool
	}{
		{
			// A space splits `rm -f /Users/John Doe/...` into two commands: rm removes /Users/John, and Doe is
			// run as a command of its own.
			name: "a HOME with a space",
			home: "host home",
		},
		{
			// A ; ends the command: what follows it runs as another one.
			name: "a HOME with a semicolon",
			home: "host;home",
		},
		{
			// A $ expands when the advice is pasted, so the command names another file than the plist.
			name: "a HOME with a dollar sign",
			home: "host$home",
		},
		{
			// The job launchd has loaded too: the warning advises its bootout as well as the removal by hand, and
			// the bootout names the job, never the plist under the unsafe home (issues #315, #398).
			name:   "a loaded job and a HOME with a space",
			home:   "host home",
			loaded: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// An instance that is listed, Stopped and holds a lima.yaml passes vm-up's checks, so the VM is
			// started and the warning is the only thing this case adds.
			f := newVMUpFixture(t, vmUpInstance, "Stopped")
			f.env["HOME"] = filepath.Join(f.sb.root, tc.home)
			f.writeInstanceLimaYaml(t)

			jobs := newLaunchdJobs(t, f.sb)
			jobs.attach(f.env)

			label := vmBootLimaAutostartLabelPrefix + vmUpInstance
			plist := limaAutostartPlistPath(userDomain, f.env["HOME"], f.limaLaunchDaemonsDir, label)

			// Lima's job's plist as `limactl autostart enable` writes it, into the home the case chose.
			writeFileMode(t, plist, []byte("Lima's own autostart job of "+vmUpInstance+"\n"), 0o600)

			state := "installed"
			if tc.loaded {
				// The same job, loaded in the user's domain as `limactl autostart enable` leaves it.
				jobs.add(t, userDomain, label, loadedJob{args: []string{"/opt/homebrew/bin/limactl", "start", vmUpInstance, "--foreground"}})
				state = "loaded in " + jobs.domainName(userDomain)
			}

			stdout, stderr, err := f.run(t, "vm-up")
			if err != nil {
				t.Fatalf("make vm-up exited non-zero for %s although Lima's autostart job %s is %s and only warns: the VM still starts, the exit status stays 0 (issues #315, #398): %v\nstdout: %s\nstderr: %s",
					tc.name, label, state, err, stdout, stderr)
			}

			// The VM is started either way: the warning changes nothing about the start (issue #315).
			if diff := cmp.Diff([][]string{vmUpStartExistingArgs(vmUpInstance)}, f.starting(t)); diff != "" {
				t.Errorf("make vm-up with %s did not start the VM the way it always does (-want +got):\n%s\nstdout: %s\nstderr: %s",
					tc.name, diff, stdout, stderr)
			}

			combined := stdout + stderr

			// The plist's path is no path the repository repeats in advice: it is not absolute with only
			// /[A-Za-z0-9._-], so no command it prints may name it (security audit L1, auditor nit #396).
			if strings.Contains(combined, "rm -f "+plist) {
				t.Errorf("the warning prints the ready-made command `rm -f %s` although that path is no absolute path of / and [A-Za-z0-9._-]: pasted by a maintainer, it removes another file or runs another command (security audit L1, auditor nit #396)\nstdout: %s\nstderr: %s",
					plist, stdout, stderr)
			}

			if strings.Contains(combined, plist) {
				t.Errorf("the warning prints the plist's path %q although it is no absolute path of / and [A-Za-z0-9._-] without a .. segment: the repository's rule (dev/agents-token-launchd.sh, foreign_jobs; auditor nit #396) repeats no such path to the terminal or into a command, and says where to see it instead (security audit L1)\nstdout: %s\nstderr: %s",
					plist, stdout, stderr)
			}

			// Holding the path back must not hold back the warning: it still names the job and how to remove it
			// the supported way, still reports it as installed, and still offers the removal by hand - as a
			// pointer to the plist, never as a command that names it.
			wants := []string{
				label,
				"is " + state,
				"limactl autostart disable " + vmUpInstance,
				"not shown",
				"remove its plist",
				// The pointer to the plist, which is all that may name it: a job loaded from an unsafe path is
				// still findable without printing that path (security audit L1, auditor nit #396).
				"ls ~/Library/LaunchAgents/" + label + ".plist",
			}

			// A loaded job is booted out of its domain before its plist is removed, and never through a command
			// that names the plist itself (issues #315, #398).
			if tc.loaded {
				wants = append(wants, "launchctl bootout gui/"+strconv.Itoa(os.Getuid())+"/"+label)
			}

			for _, want := range wants {
				if !strings.Contains(combined, want) {
					t.Errorf("make vm-up does not warn with %q although it holds the plist's path back: a maintainer still has to find the job, see that it starts the VM at every boot and remove it (security audit L1)\nstdout: %s\nstderr: %s",
						want, stdout, stderr)
				}
			}

			// Lima's own plist stays where it is: the warning removes nothing.
			if _, statErr := os.Stat(plist); statErr != nil {
				t.Errorf("make vm-up removed Lima's own plist %s although it only warns about it: removing Lima's job is the maintainer's decision (issues #315, #398): %v",
					plist, statErr)
			}
		})
	}
}

// TestVMUpRefusesAnUnsafeLIMAsLaunchDaemonsDir pins make vm-up's side of the check audit S1 asks for (issues #315,
// #398): a LIMA_LAUNCHDAEMONS_DIR on make's command line that is no absolute path or holds a character no
// LaunchDaemons path has stops vm-up before any limactl call. The check would look in a directory launchd never
// reads, so Lima's own autostart job - which starts the VM at every boot - would go unreported while vm-up said the
// VM is fine.
func TestVMUpRefusesAnUnsafeLIMAsLaunchDaemonsDir(t *testing.T) {
	t.Parallel()

	// A ; ends the path if any recipe ever expanded it unquoted, and no LaunchDaemons path holds one.
	const unsafeDir = "/a;b"

	// An instance that is listed, Stopped and holds a lima.yaml would pass every other check of vm-up, so the
	// hook's own check is what this case observes.
	f := newVMUpFixture(t, vmUpInstance, "Stopped")
	f.writeInstanceLimaYaml(t)

	stdout, stderr, err := runMakeIn(t, f.root, []string{limaLaunchDaemonsDirVar + "=" + unsafeDir, "vm-up"}, f.env)
	if err == nil {
		t.Fatalf("make vm-up exited 0 with LIMA_LAUNCHDAEMONS_DIR=%q on its command line: a directory launchd never reads hides Lima's own autostart job, so the VM is started twice at every boot without a word (issues #315, #398)\nstdout: %s\nstderr: %s",
			unsafeDir, stdout, stderr)
	}

	if !strings.Contains(stderr, limaLaunchDaemonsDirVar) {
		t.Errorf("the refusal does not name %s with the value %q, so a maintainer cannot tell which variable to fix:\nstdout: %s\nstderr: %s",
			limaLaunchDaemonsDirVar, unsafeDir, stdout, stderr)
	}

	if !strings.Contains(stderr, "may hold only letters, digits, /, ., _ and -") {
		t.Errorf("the refusal does not say what is wrong with LIMA_LAUNCHDAEMONS_DIR=%q, so a maintainer cannot tell which form the check takes (issues #315, #398)\nstdout: %s\nstderr: %s",
			unsafeDir, stdout, stderr)
	}

	// The check comes before anything runs: no limactl call, so nothing was created or started, and no sudo.
	for _, tool := range []string{"limactl", "sudo"} {
		if calls := invocations(f.sb.argvLogText(t), tool); len(calls) != 0 {
			t.Errorf("make vm-up ran %s with LIMA_LAUNCHDAEMONS_DIR=%q although the check of the hook comes before any Lima call: the VM must not be started when the warning could not be worked out (issues #315, #398): %v\nstdout: %s\nstderr: %s",
				tool, unsafeDir, calls, stdout, stderr)
		}
	}
}
