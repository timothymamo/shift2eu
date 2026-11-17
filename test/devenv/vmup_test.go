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

package devenv_test

import (
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
// exits non-zero.
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
if [ "${1:-}" = start ]; then
  exit 0
fi
echo "fake limactl: unexpected call: $*" >&2
exit 64
`

// vmUpFixture is one throwaway environment for a run of make vm-up: a sandbox whose limactl answers the
// listing and the status its case names, and a clone of the Makefile in a workspace that holds nothing but
// the clone, so vm-up's workspace check and its git-hooks prerequisite pass without touching the worktree.
type vmUpFixture struct {
	sb   *sandbox
	env  map[string]string
	root string // the clone make runs in
	// limaInstance is the LIMA_INSTANCE make runs with: vmUpInstance unless a case picks another name.
	limaInstance string
}

// newVMUpFixture builds the fixture for one case: instance is what limactl list --quiet answers (empty for
// an instance that is not listed) and status what the status query answers.
func newVMUpFixture(t *testing.T, instance, status string) *vmUpFixture {
	t.Helper()

	sb := newSandbox(t)

	limactl := filepath.Join(sb.binDir, "limactl")

	//nolint:gosec // the fake limactl is a script the tests run, so it must be executable
	if err := os.WriteFile(limactl, []byte(statusLimactl), 0o700); err != nil {
		t.Fatalf("write %s: %v", limactl, err)
	}

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

	return &vmUpFixture{sb: sb, env: env, root: root, limaInstance: vmUpInstance}
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

			stdout, stderr, err := runMakeIn(t, f.root, []string{"vm-up"}, f.env)
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

	stdout, stderr, err := runMakeIn(t, f.root, []string{"vm-up"}, f.env)
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
// start <name> would read the name as a template and fall back to template:default (issue #237). The status
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
		limaInstance string   // the LIMA_INSTANCE make runs with; empty means vmUpInstance
		want         []string // the one limactl start vm-up must run; nil means vm-up must refuse to start
		wantStderr   []string // substrings stderr must hold when vm-up refuses; <instance-dir> stands for the directory
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

			stdout, stderr, err := runMakeIn(t, f.root, []string{"vm-up"}, f.env)

			if tc.want == nil {
				if err == nil {
					t.Fatalf("make vm-up %s exited 0: limactl start <name> on an instance whose directory holds no lima.yaml reads the name as a template and falls back to template:default, a VM that mounts the whole home directory (issue #237)\nstdout: %s\nstderr: %s",
						tc.name, stdout, stderr)
				}

				if starts := f.starts(t); len(starts) != 0 {
					t.Errorf("make vm-up %s ran limactl start %v: a listed instance without a lima.yaml must never be started, or Lima falls back to template:default (issue #237)",
						tc.name, starts[0])
				}

				for _, w := range tc.wantStderr {
					w = strings.ReplaceAll(w, vmUpInstanceDirMarker, f.instanceDir())
					if !strings.Contains(stderr, w) {
						t.Errorf("make vm-up's stderr does not say %q for %s, so a maintainer cannot see which instance directory is at fault or what is missing from it (issue #237)\nstderr: %s",
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
