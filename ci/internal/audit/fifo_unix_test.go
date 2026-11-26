//go:build unix

// Pins the digest's half of the reviewer's nit on the runner's file-command
// directory (issues #615, #614): an entry that is neither a regular file nor a
// symbolic link - a named pipe, say - is taken by its name and its type alone
// and never opened, so no digest waits for a writer that never comes, and a
// named pipe the audit's run creates in that directory is a change the run fails
// on like any other.
//
// It is here, behind the unix build tag, because a named pipe is made with
// syscall.Mkfifo, which windows and js/wasm do not have, and make lint
// type-checks every release target and js/wasm: the digest's other cases stay in
// system_test.go and run on every target.
package audit_test

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// fifoWait is how long a case waits for the audit step: a digest that opened a
// named pipe would wait for a writer for ever, and the step with it.
const fifoWait = 10 * time.Second

// TestAuditMeetsANamedPipeWithoutOpeningIt pins both halves of the digest's type
// handling (issues #615, #614): a named pipe the run creates is an entry of the
// directory like any other, so the run fails closed; and a named pipe that is
// there before the run and after it is no change, so the run passes - and
// neither digest opens the pipe, which is what a hash of its content would do,
// waiting for a writer that never comes.
func TestAuditMeetsANamedPipeWithoutOpeningIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// before, when set, lays the named pipe down in the runner's file-command
		// directory before the run.
		before func(t *testing.T, root string)
		// write is what the run leaves in the work area, and the pipe too when the
		// run is the one making it.
		write func(root string) error
		// wantRefused is whether the run fails with the audit's constant line.
		wantRefused bool
	}{
		{
			name: "the run created a named pipe beside the $GITHUB_ENV file",
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				return makeNamedPipe(fileCommandPath(root, "github-pipe"))
			},
			wantRefused: true,
		},
		{
			name: "the named pipe laid down before the run is left untouched",
			before: func(t *testing.T, root string) {
				t.Helper()

				if err := makeNamedPipe(fileCommandPath(root, "github-pipe")); err != nil {
					t.Fatalf("make a named pipe in the file-command directory: %v", err)
				}
			},
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, in := preflightTree(t)

			if tc.before != nil {
				tc.before(t, root)
			}

			code, out, result := runAuditStepWithin(t, fifoWait, root, in, (&launchFake{write: tc.write}).launch)

			if tc.wantRefused {
				if code != audit.ExitFailed {
					t.Errorf("the run's exit code is %d, want %d: the run created an entry of the runner's file-command directory, and a later step of the job reads it (issues #615, #614)",
						code, audit.ExitFailed)
				}

				if !strings.Contains(out, auditFailedSentence) {
					t.Errorf("the run printed %q, want %q", out, auditFailedSentence)
				}

				if len(result) != 0 {
					t.Errorf("the run handed on %q, want nothing: a run that failed hands on no result file", result)
				}

				return
			}

			if code != audit.ExitOK {
				t.Errorf("the run's exit code is %d, want %d: the named pipe was there before the digest and after it, and nothing opened it\nprinted: %s",
					code, audit.ExitOK, out)
			}

			if string(result) != resultFile {
				t.Errorf("the run handed on %q, want %q: the run's result file is the step's", result, resultFile)
			}
		})
	}
}

// runAuditStepWithin is runAuditStep under a deadline of its own: a case whose
// digest opened an entry that is not a regular file would hang on it, so the
// step runs in a goroutine of its own and the test fails rather than wait.
func runAuditStepWithin(t *testing.T, within time.Duration, root string, in audit.Inputs, launch audit.Launch) (int, string, []byte) {
	t.Helper()

	type stepRun struct {
		code   int
		out    string
		result []byte
	}

	done := make(chan stepRun, 1)

	go func() {
		code, out, result := runAuditStep(t, root, in, launch)
		done <- stepRun{code: code, out: out, result: result}
	}()

	select {
	case got := <-done:
		return got.code, got.out, got.result
	case <-time.After(within):
		t.Fatalf("the audit step did not return within %s: the digest opened an entry of the runner's file-command directory that is neither a regular file nor a symbolic link, and reading a named pipe waits for a writer that never comes (issues #615, #614)", within)

		return 0, "", nil
	}
}

// makeNamedPipe makes a named pipe at path: an entry of a type the digest takes
// by its name and type alone.
func makeNamedPipe(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
