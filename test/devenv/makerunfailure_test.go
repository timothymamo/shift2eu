// Pins makeRunFailure, the classification runMakeIn fails a test on: a make run's result counts as make's own
// only when it is nil or an *exec.ExitError, and every other error (the bound's timeout, the wait delay's
// warning that a grandchild kept the pipes open, a run that never started) is a failure of the run, not a
// verdict on the recipe (issue #512).
package devenv_test

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// TestMakeRunFailure pins what runMakeIn treats as make's own result and what it treats as a failure of the run
// itself: only nil and an *exec.ExitError, however wrapped, are make's verdict on the recipe, so a make that
// succeeded, or one that never ran, cannot satisfy a refusal assertion that only checks err != nil.
func TestMakeRunFailure(t *testing.T) {
	t.Parallel()

	// exitError is the error runBounded returns for a make that exited non-zero: an *exec.ExitError, and make's
	// own verdict on the recipe. Its ProcessState is left unset: only the classification, errors.As, is under
	// test here, and TestRunBounded pins a real exit status end to end.
	exitError := &exec.ExitError{}

	cases := []struct {
		name string
		// err is the error runBounded returned for a make run.
		err error
		// wantFailure is whether runMakeIn's caller must fail the test on it.
		wantFailure bool
	}{
		{
			name: "make exited 0",
			err:  nil,
		},
		{
			name: "make exited non-zero",
			err:  exitError,
		},
		{
			name: "make's non-zero exit, wrapped",
			err:  fmt.Errorf("make -C %s test: %w", "/repo", exitError),
		},
		{
			name:        "the run outlived the bound",
			err:         fmt.Errorf("make -C /repo test: %w", errCommandTimedOut),
			wantFailure: true,
		},
		{
			name:        "a grandchild kept the pipes open past the wait delay",
			err:         exec.ErrWaitDelay,
			wantFailure: true,
		},
		{
			name:        "the run never started",
			err:         errors.New("create the stdin of a process group's holder: pipe: boom"),
			wantFailure: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := makeRunFailure(tc.err)

			if !tc.wantFailure {
				if got != nil {
					t.Fatalf("makeRunFailure(%v) = %v, want nil: an error errors.As-matching *exec.ExitError is make's own verdict on the recipe, not a failure of the run", tc.err, got)
				}

				return
			}

			if got == nil {
				t.Fatalf("makeRunFailure(%v) = nil, want a failure of the run: a test that expects a refusal would pass on %v, which says nothing about the recipe", tc.err, tc.err)
			}

			if !errors.Is(got, tc.err) && !strings.Contains(got.Error(), tc.err.Error()) {
				t.Errorf("makeRunFailure(%v) = %v, which names neither the error nor its message: the failure must say why the run failed, not only that it did", tc.err, got)
			}
		})
	}
}
