// Pins make vm-up's decision about a Lima instance that is already listed (issue #237): an instance whose
// directory holds no lima.yaml is still listed, with the status Broken, and Limactl start <name> then reads
// the name as a template and falls back to template:default - a VM that mounts the whole home directory
// instead of only the workspace. So an instance that is listed but neither Stopped nor Running stops vm-up
// before any limactl start, with a message naming the instance directory, its removal by hand and the launchd
// jobs that may recreate it; Stopped and Running still get limactl start <name>, and an instance that is not
// listed still gets the dev/lima.yaml command line, unchanged.

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
)

// statusLimactl stands in for limactl in the vm-up tests: it records its argv in the sandbox's log (a
// tool=limactl line and one arg= line per argument), answers the listing that decides whether the instance
// exists at all with the name SHIFT2EU_TEST_LIMA_INSTANCE (nothing for an instance that is not listed),
// answers any other listing - the status query - with SHIFT2EU_TEST_LIMA_STATUS (nothing for an empty
// status), and lets a start through without running anything: what these tests pin is which command lines
// vm-up runs, never what a VM on this machine would do.
const statusLimactl = `#!/bin/sh
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
if [ "${1:-}" = list ]; then
  # Any listing without --quiet is the status query, whatever spelling of --format it uses.
  for a in "$@"; do
    if [ "${a}" = --quiet ]; then
      if [ -n "${SHIFT2EU_TEST_LIMA_INSTANCE:-}" ]; then printf '%s\n' "${SHIFT2EU_TEST_LIMA_INSTANCE}"; fi
      exit 0
    fi
  done
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

	return &vmUpFixture{sb: sb, env: env, root: root}
}

// workspace is the directory the run mounts: the clone's parent, which holds only the clone.
func (f *vmUpFixture) workspace() string {
	return filepath.Dir(f.root)
}

// instanceDir is the directory Lima keeps the instance in, as a maintainer has to find it to remove it:
// $LIMA_HOME/<name> when LIMA_HOME is set, else $HOME/.lima/<name>.
func (f *vmUpFixture) instanceDir() string {
	if home := f.env["LIMA_HOME"]; home != "" {
		return filepath.Join(home, vmUpInstance)
	}

	return filepath.Join(f.env["HOME"], ".lima", vmUpInstance)
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
// is Broken, Starting, empty or one vm-up does not know, make vm-up exits non-zero and runs no limactl start
// at all, and a Broken instance's message says which directory to remove by hand and which launchd jobs may
// be recreating it (issue #237).
func TestVMUpRefusesAnInstanceThatIsNotStoppedOrRunning(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     string
		limaHome   bool   // LIMA_HOME names the directory Lima keeps its instances in
		broken     bool   // the message must name the directory, the removal by hand and the launchd jobs
		wantStatus string // the status the message must name; empty means nothing to look for
	}{
		{
			name:     "a broken instance, with LIMA_HOME set",
			status:   "Broken",
			limaHome: true,
			broken:   true,
		},
		{
			name:   "a broken instance, with LIMA_HOME unset",
			status: "Broken",
			broken: true,
		},
		{
			name:       "an instance that is starting",
			status:     "Starting",
			wantStatus: "Starting",
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, vmUpInstance, tc.status)

			if tc.limaHome {
				f.env["LIMA_HOME"] = filepath.Join(f.sb.root, "lima-home")
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

			want := make([]string, 0, 6)
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

			for _, w := range want {
				if !strings.Contains(stderr, w) {
					t.Errorf("make vm-up's stderr does not say %q with the instance listed as %q, so a maintainer cannot see why vm-up stopped or what to do about it (issue #237)\nstderr: %s",
						w, tc.status, stderr)
				}
			}
		})
	}
}

// TestVMUpStartsOrCreatesTheInstance pins the other half of issue #237: an instance that is Stopped or
// Running still gets exactly one limactl start <name>, and an instance that is not listed gets exactly the
// dev/lima.yaml command line with this repository's workspace mounted, unchanged.
func TestVMUpStartsOrCreatesTheInstance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		instance string   // what limactl list --quiet answers; empty means the instance is not listed
		status   string   // what the status query answers, unread when the instance is not listed
		want     []string // the one limactl start vm-up must run
	}{
		{
			name:     "an instance that is stopped",
			instance: vmUpInstance,
			status:   "Stopped",
			want:     []string{"start", vmUpInstance},
		},
		{
			name:     "an instance that is running",
			instance: vmUpInstance,
			status:   "Running",
			want:     []string{"start", vmUpInstance},
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, tc.instance, tc.status)

			stdout, stderr, err := runMakeIn(t, f.root, []string{"vm-up"}, f.env)
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
		})
	}
}
