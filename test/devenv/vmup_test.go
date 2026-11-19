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
// The fixture below is shared with vmup_resources_test.go, which pins make vm-up's VM_CPUS, VM_MEMORY, VM_DISK
// and VM_SSH_PORT (issue #228) against the same fake limactl and the same throwaway clone.
package devenv_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
const statusLimactl = `#!/bin/sh
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
if [ "${1:-}" = list ]; then
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
// answers the listing and the status its case names, and a clone of the Makefile in a workspace that holds
// nothing but the clone, so vm-up's workspace check and its git-hooks prerequisite pass without touching the
// repository the tests run from. The clone is a git repository of its own, so the Makefile resolves REPO_ROOT
// and WORKSPACE from it and the VM would mount a path inside the sandbox.
type vmUpFixture struct {
	sb   *sandbox
	env  map[string]string
	root string // the clone make runs in
	// limaInstance is the LIMA_INSTANCE make runs with: vmUpInstance unless a case picks another name.
	limaInstance string
	// marker is a file no run may create: what a value the Makefile got wrong would run (issue #228).
	marker string
}

// newVMUpFixture builds the fixture for one case: instance is what limactl list --quiet answers (empty for
// an instance that is not listed) and status what the status query answers.
func newVMUpFixture(t *testing.T, instance, status string) *vmUpFixture {
	t.Helper()

	sb := newSandbox(t)

	limactl := filepath.Join(sb.binDir, "limactl")

	writeFileMode(t, limactl, []byte(statusLimactl), 0o700)

	writeGitShim(t, sb.binDir, realTool(t, "git"))

	// vm-up depends on git-hooks, so make must run in a git repository, and its workspace may hold only
	// the repository itself: the clone sits alone in a directory of its own inside the sandbox.
	root := filepath.Join(sb.root, "ws", "shift2eu")
	writeFileMode(t, filepath.Join(root, makefilePath), []byte(readRepoFile(t, makefilePath)), 0o600)
	writeFileMode(t, filepath.Join(root, "dev", "lima.yaml"), []byte(readRepoFile(t, "dev/lima.yaml")), 0o600)

	init := exec.Command("git", "init", "--quiet", root)
	init.Env = envList(map[string]string{
		"PATH": "/usr/local/bin:/usr/bin:/bin",
		"HOME": sb.hostHome,
	})

	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", root, err, out)
	}

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

	return &vmUpFixture{
		sb:           sb,
		env:          env,
		root:         root,
		limaInstance: vmUpInstance,
		marker:       filepath.Join(sb.root, "marker-a-value-ran"),
	}
}

// workspace is the directory the run mounts: the clone's parent, which holds only the clone.
func (f *vmUpFixture) workspace() string {
	return filepath.Dir(f.root)
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

// run runs make with args in the clone, with the fixture's environment alone.
func (f *vmUpFixture) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	return runMakeIn(t, f.root, args, f.env)
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

// starts returns the argv of every limactl start the run made, in call order.
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
// Running and whose directory holds a lima.yaml still gets exactly one limactl start <name>, an instance that
// is not listed gets exactly the dev/lima.yaml command line with this repository's workspace mounted,
// unchanged, and a listed instance whose directory holds no lima.yaml is started by neither branch: limactl
// start <name> would read the name as a template and fall back to template:default (issue #237). A lima.yaml
// that is a dangling symlink is there for a Stopped or Running instance as well: the directory may still hold
// an intact disk, so that case stops too, pointing at limactl list instead of a removal by hand. The status
// of a listed instance is read by asking limactl for that instance by name, never by any other query, and an
// instance that is not listed gets no status query at all: there is no status to read.
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
		want         []string // the one limactl start vm-up must run; nil means vm-up must refuse to start
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
			want:        []string{"start", vmUpInstance},
			wantQueries: [][]string{statusQuery},
		},
		{
			name:        "an instance that is running",
			instance:    vmUpInstance,
			status:      "Running",
			limaYaml:    true,
			want:        []string{"start", vmUpInstance},
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

				if starts := f.starts(t); len(starts) != 0 {
					t.Errorf("make vm-up %s ran limactl start %v: a listed instance without a lima.yaml must never be started, or Lima falls back to template:default (issue #237)",
						tc.name, starts[0])
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

			starts := f.starts(t)
			if len(starts) != 1 {
				t.Fatalf("make vm-up %s ran %d limactl starts, want exactly one (%v)\nstdout: %s\nstderr: %s",
					tc.name, len(starts), tc.want, stdout, stderr)
			}

			if diff := cmp.Diff(want, starts[0]); diff != "" {
				t.Errorf("make vm-up %s ran the wrong limactl start (-want +got):\n%s", tc.name, diff)
			}

			if diff := cmp.Diff(tc.wantQueries, f.statusQueries(t)); diff != "" {
				t.Errorf("make vm-up %s read the status of the instance with the wrong limactl calls (-want +got), or read a status an instance that is not listed does not have:\n%s", tc.name, diff)
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
