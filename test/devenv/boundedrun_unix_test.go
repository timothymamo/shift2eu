//go:build unix

// Pins runBounded, the bounded runner runMakeIn uses: a make that never ends, or a grandchild that keeps its
// stdout and stderr open after make itself exited, must fail the test with the command, its directory and what
// it wrote named, instead of blocking the subtest until go test's package timeout names nothing (issue #512).
// A command that leaves a background child holding its stdout open and then blocks pins the group kill: only a
// kill of the whole process group closes those pipes within the bound, a kill of the command alone leaves them
// open until the wait delay mops up seconds later. Unix only: the cases run /bin/sh and the runner kills the
// command's process group; lint type-checks the package for the windows release targets too, where no /bin/sh
// exists.
package devenv_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// boundedCeilingMargin is how long past a case's maxElapsed the hard ceiling waits before it fails the subtest.
// It is small: the cases a group kill has to close fast carry a maxElapsed well below boundedWaitDelay, so the
// ceiling fails a runBounded that waits that delay out instead of returning as fast as a kill of the whole
// process group does.
const boundedCeilingMargin = time.Second

// errKind is what a case's err must be.
type errKind int

const (
	// errNone: the command exits 0 and runBounded returns no error.
	errNone errKind = iota
	// errExit: an error errors.As-matching *exec.ExitError with the case's exit code, and not errCommandTimedOut.
	errExit
	// errTimeout: an error errors.Is-matching errCommandTimedOut, naming the bound, the command line, the
	// directory and what the command wrote.
	errTimeout
	// errUnpinned: a case whose error the contract leaves open, because a grandchild may leave nil or wrap
	// exec.ErrWaitDelay.
	errUnpinned
)

// TestRunBounded pins the bounded runner's contract: what it returns for a command that exits 0, one that exits
// non-zero, one that never finishes within its bound, and one whose grandchild keeps its stdout open after the
// command itself exited.
func TestRunBounded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// script is the shell script the case runs under /bin/sh -c.
		script string
		// timeout is the bound runBounded gets.
		timeout time.Duration
		// wantStdout and wantStderr, when not empty, are exactly what the command wrote.
		wantStdout string
		wantStderr string
		// wantStdoutContains, when not empty, is what stdout must hold instead of an exact value.
		wantStdoutContains string
		// wantErr is what the error must be.
		wantErr errKind
		// wantExitCode is the exit code err's *exec.ExitError must carry, for wantErr errExit.
		wantExitCode int
		// wantMessage holds the substrings err's message must name, beyond the directory every timeout names.
		wantMessage []string
		// maxElapsed bounds how long runBounded may take, and how long the hard ceiling waits for it to return at
		// all: a runBounded that returns after it fails the case, and one that has not returned by
		// maxElapsed+boundedCeilingMargin fails it too, naming the case, rather than block the package.
		maxElapsed time.Duration
	}{
		{
			name:       "it exits 0",
			script:     `printf out; printf err >&2`,
			timeout:    30 * time.Second,
			wantStdout: "out",
			wantStderr: "err",
			wantErr:    errNone,
			maxElapsed: 10 * time.Second,
		},
		{
			name:         "it exits non-zero",
			script:       `printf partial; exit 3`,
			timeout:      30 * time.Second,
			wantStdout:   "partial",
			wantErr:      errExit,
			wantExitCode: 3,
			maxElapsed:   10 * time.Second,
		},
		{
			name:    "it does not finish within the bound",
			script:  `printf started; printf warming >&2; sleep 60`,
			timeout: 300 * time.Millisecond,
			wantErr: errTimeout,
			wantMessage: []string{
				"300ms",
				"sleep 60",
				"started",
				"warming",
			},
			maxElapsed: 10 * time.Second,
		},
		{
			name: "a background child holds stdout open until the group is killed",
			// The command's own shell blocks on the second sleep while the first holds the write end of
			// runBounded's stdout pipe: a kill of the command's process alone leaves that pipe open until the
			// wait delay gives up on it, seconds after the bound, so maxElapsed sits well below
			// boundedWaitDelay and only a kill of the whole group passes this case.
			script:  `printf started; sleep 30 & sleep 30`,
			timeout: 300 * time.Millisecond,
			wantErr: errTimeout,
			wantMessage: []string{
				"300ms",
				"sleep 30",
				"started",
			},
			maxElapsed: 3 * time.Second,
		},
		{
			name:               "a grandchild keeps stdout open",
			script:             `sleep 20 & printf done`,
			timeout:            30 * time.Second,
			wantStdoutContains: "done",
			wantErr:            errUnpinned,
			maxElapsed:         15 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.Command("/bin/sh", "-c", tc.script)
			cmd.Dir = t.TempDir()
			cmd.Env = []string{"PATH=/usr/bin:/bin"}

			type result struct {
				stdout, stderr string
				err            error
			}

			returned := make(chan result, 1)

			start := time.Now()

			go func() {
				stdout, stderr, err := runBounded(cmd, tc.timeout)
				returned <- result{stdout: stdout, stderr: stderr, err: err}
			}()

			// The hard ceiling: a runBounded that never returns fails this case, naming it, instead of blocking
			// the subtest and with it the whole package until go test's timeout names nothing (issue #512).
			ceiling := time.NewTimer(tc.maxElapsed + boundedCeilingMargin)
			defer ceiling.Stop()

			var got result

			select {
			case got = <-returned:
			case <-ceiling.C:
				t.Fatalf("runBounded had not returned for %q with a %s bound after %s, more than the %s the case allows: the case fails here rather than blocking the package until go test's timeout names nothing (issue #512)",
					tc.script, tc.timeout, tc.maxElapsed+boundedCeilingMargin, tc.maxElapsed)
			}

			stdout, stderr, err := got.stdout, got.stderr, got.err

			elapsed := time.Since(start)

			if elapsed > tc.maxElapsed {
				t.Fatalf("runBounded took %s for %q with a %s bound, more than %s: a command, or a grandchild holding its stdout open, that never ends must fail the test rather than block it until go test's package timeout (issue #512)",
					elapsed, tc.script, tc.timeout, tc.maxElapsed)
			}

			switch {
			case tc.wantStdout != "":
				if stdout != tc.wantStdout {
					t.Errorf("runBounded returned stdout %q for %q, want %q", stdout, tc.script, tc.wantStdout)
				}
			case tc.wantStdoutContains != "":
				if !strings.Contains(stdout, tc.wantStdoutContains) {
					t.Errorf("runBounded returned stdout %q for %q, want it to hold %q", stdout, tc.script, tc.wantStdoutContains)
				}
			}

			if tc.wantStderr != "" && stderr != tc.wantStderr {
				t.Errorf("runBounded returned stderr %q for %q, want %q", stderr, tc.script, tc.wantStderr)
			}

			switch tc.wantErr {
			case errNone:
				if err != nil {
					t.Errorf("runBounded returned %v for %q, which exits 0, want no error", err, tc.script)
				}
			case errExit:
				var exit *exec.ExitError

				if !errors.As(err, &exit) {
					t.Fatalf("runBounded returned %v for %q, which exits %d, want an error errors.As-matching *exec.ExitError",
						err, tc.script, tc.wantExitCode)
				}

				if code := exit.ExitCode(); code != tc.wantExitCode {
					t.Errorf("runBounded's error carries exit code %d for %q, want %d", code, tc.script, tc.wantExitCode)
				}

				if errors.Is(err, errCommandTimedOut) {
					t.Errorf("runBounded's error for %q, which exited %d within its bound, wraps errCommandTimedOut: a non-zero exit is not a timeout", tc.script, tc.wantExitCode)
				}
			case errTimeout:
				if !errors.Is(err, errCommandTimedOut) {
					t.Fatalf("runBounded returned %v for %q, which outlives its %s bound, want an error errors.Is-matching errCommandTimedOut",
						err, tc.script, tc.timeout)
				}

				for _, want := range append(tc.wantMessage, cmd.Dir) {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("runBounded's timeout error for %q does not name %q, so the blocked command cannot be told apart from another and what it wrote is lost:\n%s",
							tc.script, want, err.Error())
					}
				}
			case errUnpinned:
				var exit *exec.ExitError

				switch {
				case err == nil:
				case errors.Is(err, exec.ErrWaitDelay):
				default:
					t.Fatalf("runBounded returned %v for %q, whose own command exits 0 and whose grandchild only holds its stdout open, want no error or an error errors.Is-matching exec.ErrWaitDelay from a Wait that stopped waiting for the pipes",
						err, tc.script)
				}

				if errors.Is(err, errCommandTimedOut) {
					t.Errorf("runBounded's error for %q, which finished within its bound and only left a grandchild behind, wraps errCommandTimedOut: a timeout is not a warning about the pipes",
						tc.script)
				}

				if errors.As(err, &exit) {
					t.Errorf("runBounded's error for %q, which exits 0, errors.As-matches *exec.ExitError (%v): the grandchild's survival is no non-zero exit",
						tc.script, exit)
				}
			}
		})
	}
}
