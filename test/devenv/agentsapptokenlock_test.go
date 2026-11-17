//go:build unix

// Pins what the agents App's mint does with a ~/.gitconfig.lock that is not the regular file git itself creates
// (issue #241). Unix only: the fake lock is a FIFO and the mint under test runs in a process group the test kills
// as a whole, and only a unix host has syscall.Mkfifo, SysProcAttr.Setpgid and syscall.Kill. Lint type-checks this
// package for the windows release targets too, where those names do not exist.
package devenv_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// mintWithin runs one mint and fails the test when it is still running after timeout: a mint that took a FIFO for
// the lock git itself creates would wait for a writer that never comes, and a test must fail on that instead of
// blocking the package. The whole process group goes on the timeout, because the mint's own children hold the
// pipes the test reads (issue #241).
func (f *mintFixture) mintWithin(t *testing.T, timeout time.Duration) (string, string, error) {
	t.Helper()

	cmd := exec.Command("sh", scriptPath(t, agentsMintScript), "mint")
	cmd.Env = envList(f.env)
	cmd.Dir = f.sb.hostHome
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start the mint: %v", err)
	}

	// The status is read only after done is closed, which orders it after Wait wrote it.
	var status error

	done := make(chan struct{})

	go func() {
		status = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return stdout.String(), stderr.String(), status
	case <-time.After(timeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done

		t.Fatalf("the mint was still running after %s with a ~/.gitconfig.lock that is not a regular file: it must refuse such a lock, never follow it or wait on it, because nothing ever writes to it (issue #241)\nstdout:\n%s\nstderr:\n%s",
			timeout, stdout.String(), stderr.String())

		return "", "", nil
	}
}

// TestAgentsAppTokenMintRefusesALockThatIsNotARegularFile pins that a ~/.gitconfig.lock that is not the regular file
// git itself creates is refused, never followed, never removed and never waited on: whatever sits behind the entry
// is a file the mint must not write the identity through, and an entry it cannot tell from another writer's lock is
// no lock it may take over (issue #241).
func TestAgentsAppTokenMintRefusesALockThatIsNotARegularFile(t *testing.T) {
	t.Parallel()

	const (
		maintainerName  = "Example Maintainer"
		maintainerEmail = "maintainer@example.com"

		// The mint reads the lock's entry, and a mint that opened a FIFO for writing would wait for a writer that
		// never comes: the timeout turns that hang into a failed test instead of a blocked package (issue #241).
		mintTimeout = 20 * time.Second
	)

	cases := []struct {
		name string
		// makeLock plants the ~/.gitconfig.lock entry and returns the check that it is still exactly what it was.
		makeLock func(t *testing.T, f *mintFixture) func(t *testing.T, f *mintFixture)
	}{
		{
			name: "a symlink to a file in the VM home",
			makeLock: func(t *testing.T, f *mintFixture) func(t *testing.T, f *mintFixture) {
				t.Helper()

				target := filepath.Join(f.sb.vmHome, "stale-lock-target")
				writeFileMode(t, target, []byte(staleGitLockBody), 0o600)

				lock := filepath.Join(f.sb.vmHome, ".gitconfig.lock")
				if err := os.Symlink(target, lock); err != nil {
					t.Fatalf("symlink %s to %s: %v", lock, target, err)
				}

				return func(t *testing.T, _ *mintFixture) {
					t.Helper()

					info, err := os.Lstat(lock)
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Errorf("the refused mint removed or replaced %s, which is a symlink to a file it may neither follow nor remove; git's lock is a regular file, and an entry that is not one is no lock the mint may take over (%v)",
							lock, err)
					}

					body, err := os.ReadFile(target)
					if err != nil {
						t.Fatalf("read %s: %v", target, err)
					}

					if string(body) != staleGitLockBody {
						t.Errorf("%s holds %q after the refused mint, want %q: the mint wrote the identity through the lock's symlink, into a file that is no lock of git's (issue #241)",
							target, body, staleGitLockBody)
					}
				}
			},
		},
		{
			// A symlink to /dev/null is the entry a mint could take for a lock it created and then move over
			// ~/.gitconfig, which would turn the VM's git configuration into a link to nowhere (issue #241).
			name: "a symlink to /dev/null",
			makeLock: func(t *testing.T, f *mintFixture) func(t *testing.T, f *mintFixture) {
				t.Helper()

				lock := filepath.Join(f.sb.vmHome, ".gitconfig.lock")
				if err := os.Symlink(os.DevNull, lock); err != nil {
					t.Fatalf("symlink %s to %s: %v", lock, os.DevNull, err)
				}

				return func(t *testing.T, _ *mintFixture) {
					t.Helper()

					info, err := os.Lstat(lock)
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Errorf("the refused mint removed or replaced %s, a symlink to %s: an entry that is no regular file is no lock git wrote, so the mint follows it nowhere and removes it never (%v)",
							lock, os.DevNull, err)

						return
					}

					if target, err := os.Readlink(lock); err != nil || target != os.DevNull {
						t.Errorf("%s points at %q after the refused mint, want %q: the mint leaves the entry it found exactly as it found it (issue #241)",
							lock, target, os.DevNull)
					}
				}
			},
		},
		{
			// A directory is the entry a mint that created its lock *inside* the lock path, or that opened the path
			// for writing, would write the identity through; link(2) refuses any existing entry, a directory included
			// (issue #241).
			name: "a directory",
			makeLock: func(t *testing.T, f *mintFixture) func(t *testing.T, f *mintFixture) {
				t.Helper()

				lock := filepath.Join(f.sb.vmHome, ".gitconfig.lock")
				if err := os.Mkdir(lock, 0o700); err != nil {
					t.Fatalf("make %s a directory: %v", lock, err)
				}

				return func(t *testing.T, _ *mintFixture) {
					t.Helper()

					info, err := os.Lstat(lock)
					if err != nil || !info.IsDir() {
						t.Errorf("the refused mint removed or replaced %s, a directory: an entry that is no regular file is no lock git wrote, so the mint neither follows nor removes it (%v)",
							lock, err)

						return
					}

					inside, err := os.ReadDir(lock)
					if err != nil {
						t.Fatalf("read %s after the refused mint: %v", lock, err)
					}

					if len(inside) != 0 {
						names := make([]string, 0, len(inside))
						for _, entry := range inside {
							names = append(names, entry.Name())
						}

						t.Errorf("%s holds %v after the refused mint, want nothing: the mint takes its own lock beside ~/.gitconfig and writes nothing inside an entry it found at the lock's path (issue #241)",
							lock, names)
					}
				}
			},
		},
		{
			name: "a FIFO",
			makeLock: func(t *testing.T, f *mintFixture) func(t *testing.T, f *mintFixture) {
				t.Helper()

				lock := filepath.Join(f.sb.vmHome, ".gitconfig.lock")
				if err := syscall.Mkfifo(lock, 0o600); err != nil {
					t.Fatalf("make %s a FIFO: %v", lock, err)
				}

				return func(t *testing.T, _ *mintFixture) {
					t.Helper()

					info, err := os.Lstat(lock)
					if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
						t.Errorf("the refused mint removed or replaced %s, a FIFO nothing writes to: opening it for writing waits for a writer that never comes, so the mint must not open it at all (%v)",
							lock, err)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			f.setVMGitIdentity(t, maintainerName, maintainerEmail)

			gitconfig := filepath.Join(f.sb.vmHome, ".gitconfig")
			before, err := os.ReadFile(gitconfig)
			if err != nil {
				t.Fatalf("read the fake VM's .gitconfig before the mint: %v", err)
			}

			checkLock := tc.makeLock(t, f)

			stdout, stderr, err := f.mintWithin(t, mintTimeout)
			if err == nil {
				t.Fatalf("mint exited 0 although the fake VM's ~/.gitconfig.lock is %s, not the regular file git itself creates: the mint took an entry it cannot tell from a lock, and its write landed in whatever is behind it (issue #241)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if text := strings.ToLower(stdout + stderr); !strings.Contains(text, "lock") {
				t.Errorf("the mint refuses a ~/.gitconfig.lock that is %s, but no line mentions a lock, so a maintainer cannot tell what to look at (issue #241)\nstdout:\n%s\nstderr:\n%s",
					tc.name, stdout, stderr)
			}

			after, err := os.ReadFile(gitconfig)
			if err != nil {
				t.Fatalf("read the fake VM's .gitconfig after the refused mint: %v", err)
			}

			if !bytes.Equal(after, before) {
				t.Errorf("the refused mint changed the fake VM's .gitconfig although ~/.gitconfig.lock is %s:\nbefore:\n%s\nafter:\n%s",
					tc.name, before, after)
			}

			checkLock(t, f)
			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}
