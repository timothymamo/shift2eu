//go:build unix

// Pins what the agents App's mint does with a ~/.gitconfig.lock that is not the regular file git itself creates
// (issue #241). Unix only: the fake lock is a FIFO and the mint under test runs in a process group the test kills
// as a whole, and only a unix host has syscall.Mkfifo, SysProcAttr.Setpgid and syscall.Kill. Lint type-checks this
// package for the windows release targets too, where those names do not exist.
//
// It also pins the refresher lock's moved-aside cleanup (issue #396, item 2): what a refresh round does when the
// reap directory cannot be removed, whose entry names and state live beside the lock tests of refresh_test.go.
package devenv_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

// plantingLNShim is an ln for the VM that plants an entry at ~/.gitconfig.lock just before the mint's own link runs,
// the way a third contender or another git writer can in the window between the mint's pre-check of the lock and
// its `ln -T`, which the check cannot cover (issue #264). It plants only when nothing is there yet, and then hands
// the call to the machine's ln, which refuses any existing entry, as link(2) does. The plant itself is the test's:
// it is the entry that appears in that window, and the argument is a shell line the shim runs first.
const plantingLNShim = `#!/bin/sh
if [ "$#" -eq 3 ] && [ "$1" = -T ] && [ "$3" = "${HOME:?}/.gitconfig.lock" ] &&
  [ ! -e "$3" ] && [ ! -L "$3" ]; then
  %s
fi
exec %s "$@"
`

// TestAgentsAppTokenMintRefusesAnEntryPlantedAfterTheLockPreCheck pins the second half of issue #264: the VM's
// script checks ~/.gitconfig.lock before it hard-links its own lock there with `ln -T`, and an entry that appears
// in between is exactly what link(2) refuses. The script must read the failure the way it reads the pre-check's:
// a symlink or another non-regular file is no lock git wrote and is refused with status 3 (never followed, opened
// or removed), and a regular file belongs to another git writer, which the script leaves alone and refuses with
// status 4, the status the host reads as transient. The test plants each entry from an ln shim, on the very call
// that takes the lock, so the window is the shim's and not the runner's speed.
//
// ln -T is GNU-only, so this runs in the VM and CI (Linux) as the other lock tests do, and skips elsewhere.
func TestAgentsAppTokenMintRefusesAnEntryPlantedAfterTheLockPreCheck(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("ln -T is GNU-only: the mint's own lock link, and this test's plant on that call, need it (issue #264)")
	}

	const (
		maintainerName  = "Example Maintainer"
		maintainerEmail = "maintainer@example.com"

		lockBody = "a git config another writer runs in the VM holds this lock\n"

		// The mint reads the lock's entry and a mint that took a FIFO for it would wait for a writer that never
		// comes: the timeout turns that hang into a failed test (issue #241).
		mintTimeout = 20 * time.Second
	)

	lnTool := realTool(t, "ln")

	cases := []struct {
		name string
		// plant is the shell line the ln shim runs just before the mint's own link, and wantLockMessage the VM's
		// word for the entry it finds; the two tell the statuses 3 and 4 apart on the host's stderr.
		plant           string
		wantStatus      string
		wantLockMessage string
		// check proves the entry is still exactly what the window planted.
		check func(t *testing.T, f *mintFixture, lock string)
	}{
		{
			name:            "a symlink appears in the window",
			plant:           fmt.Sprintf("%s -s \"$HOME/planted-lock-target\" \"$3\"", strconv.Quote(lnTool)),
			wantStatus:      "refused to write the",
			wantLockMessage: "is a symlink or not a regular file",
			check: func(t *testing.T, f *mintFixture, lock string) {
				t.Helper()

				info, err := os.Lstat(lock)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Errorf("the refused mint removed or replaced %s, a symlink planted between its pre-check and its ln -T: an entry that is no regular file is no lock git wrote, so the mint follows it nowhere and removes it never (%v, issue #264)",
						lock, err)

					return
				}

				if target, err := os.Readlink(lock); err != nil || target != filepath.Join(f.sb.vmHome, "planted-lock-target") {
					t.Errorf("%s points at %q after the refused mint, want the target the window planted: the mint leaves the entry it found exactly as it found it (issue #264)",
						lock, target)
				}
			},
		},
		{
			name:            "a regular lock file appears in the window",
			plant:           `printf '%s\n' ` + strconv.Quote(strings.TrimRight(lockBody, "\n")) + ` > "$3"`,
			wantStatus:      "did not write the",
			wantLockMessage: "exists: another git writer holds the lock on",
			check: func(t *testing.T, _ *mintFixture, lock string) {
				t.Helper()

				body, err := os.ReadFile(lock)
				if err != nil {
					t.Fatalf("read %s after the refused mint: %v", lock, err)
				}

				if string(body) != lockBody {
					t.Errorf("%s holds %q after the refused mint, want the %q the window planted: a regular file there belongs to another git writer, and the mint leaves it exactly as it found it (issue #264)",
						lock, body, lockBody)
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

			lock := filepath.Join(f.sb.vmHome, ".gitconfig.lock")

			shim := fmt.Sprintf(plantingLNShim, tc.plant, strconv.Quote(lnTool))
			writeFileMode(t, filepath.Join(f.sb.binDir, "ln"), []byte(shim), 0o700)

			stdout, stderr, err := f.mintWithin(t, mintTimeout)
			if err == nil {
				t.Fatalf("mint exited 0 although %s, an entry the mint's pre-check could not see and its ln -T must refuse: the mint's write landed in whatever is behind it (issue #264)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if !strings.Contains(stderr, tc.wantLockMessage) {
				t.Errorf("the refused mint does not say %q, the VM's own word for the entry that appeared between its pre-check and its ln -T, so a maintainer cannot tell what to remove (issue #264)\nstdout:\n%s\nstderr:\n%s",
					tc.wantLockMessage, stdout, stderr)
			}

			// The host tells the VM's statuses 3 and 4 apart: 3 is permanent (no lock git wrote), 4 is transient
			// (another git writer holds it, and the next mint tries again).
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("mint exited with %v although the VM refused the lock, want status 1, the status of a failed mint (issue #264)\nstderr: %s", err, stderr)
			}

			if !strings.Contains(stderr, tc.wantStatus) {
				t.Errorf("the mint's word for the VM's refusal does not start %q, so a maintainer cannot tell the entry no lock git wrote (status 3) from another writer's lock (status 4) (issue #264)\nstderr:\n%s",
					tc.wantStatus, stderr)
			}

			// The token is not pinned here: the mint writes it before it knows the bot user, and a failed identity
			// write keeps it by design (TestAgentsAppTokenMintExitsOneWhenTheBotUserLookupFails). The identity file
			// is what this refusal is about, and it must not land.
			if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
				t.Errorf("the mint wrote %s although %s made it refuse the lock: it must refuse before it writes the identity (issue #264)",
					agentsIdentityFile, tc.name)
			}

			after, err := os.ReadFile(gitconfig)
			if err != nil {
				t.Fatalf("read the fake VM's .gitconfig after the refused mint: %v", err)
			}

			if !bytes.Equal(after, before) {
				t.Errorf("the refused mint changed the fake VM's .gitconfig although %s:\nbefore:\n%s\nafter:\n%s",
					tc.name, before, after)
			}

			// The mint's own temporary file beside ~/.gitconfig must not outlive it: a home that mints twice a day
			// fills with them (issue #241).
			if leftovers, err := filepath.Glob(filepath.Join(f.sb.vmHome, ".gitconfig.agents-token.*")); err != nil {
				t.Fatalf("look for what the refused mint left beside the fake VM's .gitconfig: %v", err)
			} else if len(leftovers) != 0 {
				t.Errorf("the refused mint left %v in the fake VM's home: the temporary file it links the lock from must not outlive it (issue #264)", leftovers)
			}

			tc.check(t, f, lock)
			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// failingReapRemove is an rm for the sandbox that refuses every path of a reap directory and runs the machine's
// own rm for everything else: the round a refresh clears its stale entries in starts by removing its own
// .agents-token-refresh-<instance>.lock.reap.<pid>, and a remove that fails is what issue #396's item 2 is about.
// %s is the machine's rm, quoted.
const failingReapRemove = `#!/bin/sh
for a in "$@"; do
  case "$a" in
  *.lock.reap.*)
    printf 'rm: %%s: Device busy\n' "$a" >&2
    exit 1
    ;;
  esac
done
exec %s "$@"
`

// TestAgentsAppTokenRefreshSkipsAReapingRoundWhenTheReapDirectoryCannotBeRemoved pins issue #396's item 2: a round
// of clear_stale_asides starts by removing this start's own .reap.<pid> directory, and that removal can fail (a
// directory another account owns, a full disk, a file system that refuses it). The helper then went on reaping and
// renamed a stale moved-aside lock *to* the directory that is still there, or moved it into it, which is exactly
// the copy nobody removes that the reap directory exists to prevent, and the log said only "could not remove". The
// round must instead reap nothing once that removal failed - no later mv, so every stale entry stays where it is -
// and the log must say the round skipped reaping because the removal failed, naming the directory.
func TestAgentsAppTokenRefreshSkipsAReapingRoundWhenTheReapDirectoryCannotBeRemoved(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEnv()

	rmTool := realTool(t, "rm")
	writeFileMode(t, filepath.Join(f.sb.binDir, "rm"), []byte(fmt.Sprintf(failingReapRemove, strconv.Quote(rmTool))), 0o700)

	// Two stale moved-aside locks, each with a mover and a recorded owner that run no refresh: a round that reaps
	// renames the first to its own .reap.<pid> name and then nests the second inside it, both of which this test
	// reads in the state directory afterwards.
	first := f.sb.movedAsideLock("shift2eu", deadPid(t))
	writeMovedAsideLock(t, first, deadPid(t))

	second := f.sb.movedAsideLock("shift2eu", deadPid(t))
	writeMovedAsideLock(t, second, deadPid(t))

	if first == second {
		t.Fatalf("both stale entries are %s: the test needs two distinct ones", first)
	}

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	reap := f.sb.reapedAsideLock("shift2eu", refresh.pid())

	var skipped string

	if !poll(15*time.Second, func() bool {
		skipped = skippedReapingLine(refresh.errOut(), filepath.Base(reap))

		return skipped != ""
	}) {
		t.Fatalf("the refresh never says it skipped reaping although its own reap directory %s could not be removed, so a maintainer reads nothing about the stale entries that stay, and the next round tries the same removal that failed (issue #396)\nstate directory:\n%s\nstderr:\n%s",
			filepath.Base(reap), describeTree(f.sb.stateDir()), refresh.errOut())
	}

	// The rest of the round reaped nothing: both stale entries are exactly where they were, with their pids, and
	// the reap directory that could not be removed holds no moved-in copy of either.
	for _, aside := range []string{first, second} {
		movedAsideOwner(t, aside)
	}

	if entries, err := os.ReadDir(reap); err == nil {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}

		t.Errorf("the refresh moved %v into %s although removing that directory failed, so the stale entries now sit in the one place no start removes by name: the round must reap nothing once its own remove failed (issue #396)\nstate directory:\n%s",
			names, reap, describeTree(f.sb.stateDir()))
	}

	// Skipping the reaping is not the round failing: the refresh takes its lock, mints and keeps running.
	waitFor(t, 30*time.Second, "the refresh to write the VM's token although it skipped reaping", func() bool {
		line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

		return ok && line != ""
	})

	if !refresh.running() {
		t.Fatalf("the refresh exited after skipping the reaping of one round instead of refreshing the VM's token (issue #396)\nstderr:\n%s", refresh.errOut())
	}

	refresh.signal(t, syscall.SIGTERM)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
}

// skippedReapingLine returns the line of text that says the round skipped reaping because its own reap directory
// could not be removed, naming that directory, or "" when there is none. The wording is free; the line has to say
// that reaping was skipped, that it was the removal that failed, and which directory it was, since the name carries
// the pid of the start whose removal failed (issue #396).
func skippedReapingLine(text, reapName string) string {
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "skipped") && strings.Contains(lower, "remove") && strings.Contains(line, reapName) {
			return line
		}
	}

	return ""
}

// cleanupHelperNames are the shell variables clear_stale_asides, reap_entry and forget_unremovable assign while
// they work: theirs alone, so their caller keeps whatever it had under those names (issue #396, item 1).
// unremovable is not among them: that one is the helpers' bookkeeping for the caller and must outlive the call.
// found and list are in the list for the day a shell traces its for loops' and declarations' variables too; a sh
// traces the assignment of a loop variable not at all, so a caller's found is only covered by the declaration a
// fix writes.
var cleanupHelperNames = []string{"reap", "prefix", "found", "mover", "stale", "recorded", "list", "was_unremovable"}

// tracedRefresh starts dev/agents-app-token.sh refresh with the shell's command trace on (`sh -x`), so the test
// reads every command the script runs, its variable assignments and their expansions, from its stderr. The
// trace is the only way a black-box test sees a shell variable of the running script: it is the shell's own
// output, not a hook the script offers.
//
// The trace prints every value the script puts into a variable, the token and the JWT among them, so a test that
// uses it reads only the assignment lines it is about and never hands the whole trace to assertNoLeaks or a
// failure message.
func tracedRefresh(t *testing.T, f *mintFixture, env map[string]string) *runningScript {
	t.Helper()

	// A wrapper, because the sandbox starts the script as `sh <script> <args>`: the trace has to be the inner
	// shell's, and its command line keeps the script's name, which the script's own is_refresh reads.
	wrapper := filepath.Join(f.sb.root, "traced-refresh")
	body := "#!/bin/sh\nexec sh -x " + strconv.Quote(scriptPath(t, agentsMintScript)) + " \"$@\"\n"

	writeFileMode(t, wrapper, []byte(body), 0o755)

	return f.sb.start(t, wrapper, []string{"refresh"}, env)
}

// unscopedAssignments returns the lines of a `sh -x` trace that assign one of names in the trace's own scope: a
// plain `name=value` command, and not a `local`, `export`, `readonly` or `typeset` declaration, which scopes the
// variable to the function it runs in when the shell supports it. The shell prefixes each command it traces with
// `+`, and a command whose expansion holds newlines - the VM's writer, a here-document - continues on lines of its
// own: those lines are no commands of the trace's shell, and a `found=0` inside the writer's text is the VM's
// script, never this one's variable.
func unscopedAssignments(trace string, names []string) []string {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}

	var offenders []string

	for _, line := range strings.Split(trace, "\n") {
		if !strings.HasPrefix(line, "+") {
			continue
		}

		line = strings.TrimLeft(strings.TrimLeft(line, "+"), " \t")

		first := line
		if at := strings.IndexAny(line, " \t"); at >= 0 {
			first = line[:at]
		}

		switch first {
		case "", "local", "export", "readonly", "typeset":
			continue
		}

		if name, _, ok := strings.Cut(first, "="); ok && wanted[name] {
			offenders = append(offenders, line)
		}
	}

	return offenders
}

// TestAgentsAppTokenRefreshCleanupHelpersLeaveTheirCallersVariablesAlone pins issue #396's item 1: clear_stale_asides
// and the helpers it calls are pieces of one shell, whose variables are all global, POSIX sh knowing no `local`. Their
// working names - reap, prefix, the loop's found and mover, stale, recorded, the unremovable bookkeeping of one name -
// are therefore the caller's too: a caller that keeps anything under one of those names, or a future edit that does,
// reads the helper's value afterwards. The cleanup must keep its own names to itself, which `sh -x` shows: every
// assignment to one of them inside a function gives it its function's prefix (sweep_, reap_, forget_), or the name is
// a new one no caller of the script has.
func TestAgentsAppTokenRefreshCleanupHelpersLeaveTheirCallersVariablesAlone(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEnv()

	// A stale moved-aside lock whose two pids run no refresh: the start reaps it, which runs every line of
	// reap_entry the successful rename and removal run.
	aside := f.sb.movedAsideLock("shift2eu", deadPid(t))
	writeMovedAsideLock(t, aside, deadPid(t))

	refresh := tracedRefresh(t, f, env)

	if !poll(30*time.Second, func() bool {
		_, err := os.Stat(aside)

		return errors.Is(err, fs.ErrNotExist)
	}) {
		t.Fatalf("the refresh did not reap the stale moved-aside lock %s, so its cleanup never ran and this test cannot see the names it assigns (issue #396)\nstate directory:\n%s\nstderr:\n%s",
			aside, describeTree(f.sb.stateDir()), refresh.errOut())
	}

	refresh.signal(t, syscall.SIGTERM)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the traced refresh did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
	}

	if offenders := unscopedAssignments(refresh.errOut(), cleanupHelperNames); len(offenders) != 0 {
		t.Errorf("the refresher's cleanup assigns %v without scoping %s, so the cleanup overwrites those variables of whoever called it: a caller of clear_stale_asides, reap_entry or forget_unremovable that keeps its own reap, prefix, found, mover, stale, recorded, list or was_unremovable reads the helper's value afterwards, and the next edit that gives the caller such a name breaks it silently. Give each one its function's prefix (sweep_, reap_, forget_) or another name the caller does not use (issue #396, item 1)\nstate directory:\n%s",
			offenders, strings.Join(cleanupHelperNames, ", "), describeTree(f.sb.stateDir()))
	}
}
