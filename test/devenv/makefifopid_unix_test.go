//go:build unix

// Pins what make agents-token-refresh-start does with a pid entry that is no regular file (issue #335, the
// reviewer's S1 and the auditor's LOW-A). Unix only: the entry is a FIFO, and the start runs in a process group
// the test kills as a whole when the read of that entry never returns; lint type-checks the package for the
// windows release targets too, where neither a FIFO nor a process group exists.
package devenv_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// runMakeWithin runs make in the repository root with args and env alone, never the test process's own
// environment, and gives up on it after timeout: a recipe that reads a FIFO nobody writes to waits for a writer
// that never comes, and a test must fail on that instead of blocking the package. The whole process group goes on
// the timeout, because the shell make runs the recipe with is the process that blocks there.
func runMakeWithin(t *testing.T, args []string, env map[string]string, timeout time.Duration) (string, string, error, bool) {
	t.Helper()

	if _, err := exec.LookPath("make"); err != nil {
		t.Fatalf("make is not installed: the repository's commands, and this test, need it")
	}

	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	cmd := exec.Command("make", append([]string{"-C", root, "--no-print-directory"}, args...)...)
	cmd.Env = envList(env)
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start make %v: %v", args, err)
	}

	// The buffers are read only after done is closed, which orders that read after Wait finished writing them.
	var status error

	done := make(chan struct{})

	go func() {
		status = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return stdout.String(), stderr.String(), status, false
	case <-time.After(timeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done

		return stdout.String(), stderr.String(), status, true
	}
}

// TestMakeAgentsTokenRefreshStartDoesNotHangOnAFifoPid pins the reviewer's S1 and the auditor's LOW-A for issue
// #335: the start recipe reads the refresh's pid file and the lock's pid to find out whether a refresher already
// runs, and a cat of a FIFO nobody writes to blocks for ever. The state directory is a directory on the Mac that
// anything may have been put in, so a start that took such an entry for a pid never starts a refresher and never
// says why: make, and the maintainer watching it, wait. The entry holds no pid, as one that is no regular file
// never does, so the start goes on and leaves a refresher running.
func TestMakeAgentsTokenRefreshStartDoesNotHangOnAFifoPid(t *testing.T) {
	t.Parallel()

	// The bound: make starts its recipe and the fake tools within seconds, and the FIFO never answers at all.
	const makeTimeout = 30 * time.Second

	cases := []struct {
		name string
		// plant writes the entry that is a FIFO and returns its path.
		plant func(t *testing.T, f *tokenFixture) string
	}{
		{
			// The lock the refresh holds, whose pid the recipe reads beside the pid file.
			name: "the lock's pid is a fifo",
			plant: func(t *testing.T, f *tokenFixture) string {
				t.Helper()

				lock := f.sb.refreshLockDir("shift2eu")

				if err := os.MkdirAll(lock, 0o700); err != nil {
					t.Fatalf("create the lock %s: %v", lock, err)
				}

				makeFifo(t, filepath.Join(lock, "pid"))

				return filepath.Join(lock, "pid")
			},
		},
		{
			// The refresh's pid file itself: read before the lock's, and written again by the start once it has
			// decided to start a refresher.
			name: "the pid file is a fifo",
			plant: func(t *testing.T, f *tokenFixture) string {
				t.Helper()

				if err := os.MkdirAll(f.state, 0o700); err != nil {
					t.Fatalf("create the state directory %s: %v", f.state, err)
				}

				pidFile := f.statePath("agents-token-refresh-shift2eu.pid")

				makeFifo(t, pidFile)

				return pidFile
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, "shift2eu")
			planted := tc.plant(t, f)

			// The fixture stops the refresher a target started by reading every *.pid entry of the state
			// directory, which a FIFO still standing there would block too.
			t.Cleanup(func() {
				info, err := os.Lstat(planted)
				if err == nil && info.Mode()&os.ModeNamedPipe != 0 {
					if err := os.Remove(planted); err != nil {
						t.Errorf("remove the fifo %s the test planted: %v", planted, err)
					}
				}
			})

			stdout, stderr, status, hung := runMakeWithin(t, []string{"agents-token-refresh-start"}, f.env, makeTimeout)

			if hung {
				t.Fatalf("make agents-token-refresh-start was still running %s after it started, with the fifo %s in place: the recipe reads that entry with cat before it decides whether a refresher runs, and writes the pid file again when it starts one, and opening a fifo at either end waits for the other for ever, so the start finishes nothing and says nothing about it (issue #335, the review's S1 and the audit's LOW-A)\nstdout:\n%s\nstderr:\n%s",
					makeTimeout, planted, stdout, stderr)
			}

			if status != nil {
				t.Fatalf("make agents-token-refresh-start failed with the fifo %s in place, where nothing but that entry stood in its way: an entry that is no regular file holds no pid (read_pid, dev/agents-app-token.sh), so the start goes on and starts a refresher rather than refusing (issue #335, the review's S1 and the audit's LOW-A)%v\nstdout:\n%s\nstderr:\n%s",
					planted, status, stdout, stderr)
			}

			pid := readPid(t, f.statePath("agents-token-refresh-shift2eu.pid"))

			if !pidAlive(pid) {
				t.Errorf("the pid file holds %d after make agents-token-refresh-start got past the fifo %s, but no such process runs: the start must leave a refresher running, as it does for an entry that holds no pid (issue #335)\nstdout:\n%s\nstderr:\n%s",
					pid, planted, stdout, stderr)
			}
		})
	}
}
