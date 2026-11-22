// Pins the sandbox's start check (ADR 0040's milestone-2 amendment, issue
// #277, "The runner's user-namespace restriction"): before the model runs, the
// launcher starts bubblewrap once with the invocation the ADR gives, and a
// bubblewrap that cannot start ends the run with exit 4 and one line naming
// runbook step 10 and its user-namespace note, whatever the cause. The command
// runner is a fake, so no sandbox is started here.
package audit_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// bwrapArgs is the invocation ADR 0040 fixes, its program's name aside:
// --unshare-all so no namespace of the runner's is shared, --die-with-parent so
// no sandbox outlives the launcher, and the whole filesystem read-only with
// /bin/true as the one command, so a start that answers proves bubblewrap can
// build the sandbox the CLI needs.
var bwrapArgs = []string{"--unshare-all", "--die-with-parent", "--ro-bind", "/", "/", "/bin/true"}

// runnerFake is the fake command runner: it records every invocation and
// answers with its own error.
type runnerFake struct {
	invocations [][]string
	err         error
}

// Check is the runner the check takes: the fake records the invocation it would
// have started. The method, like audit.Runner's, is named Check: test/lint's
// TestNoInterfaceDeclaresACloudWrite refuses an interface method named for a
// write verb, and Run is one.
func (r *runnerFake) Check(_ context.Context, argv []string) error {
	r.invocations = append(r.invocations, argv)

	return r.err
}

// TestCheckBubblewrapStartsTheADRsInvocation pins the one invocation the start
// check makes: bwrap, which the runner resolves on the fixed PATH, with the
// ADR's three flags, the read-only bind of / and /bin/true, and nothing else.
func TestCheckBubblewrapStartsTheADRsInvocation(t *testing.T) {
	t.Parallel()

	runner := &runnerFake{}

	if err := audit.CheckBubblewrap(t.Context(), runner); err != nil {
		t.Fatalf("CheckBubblewrap() error = %v, want none: bwrap started", err)
	}

	if len(runner.invocations) != 1 {
		t.Fatalf("the check started %d commands (%v), want exactly one: bwrap --unshare-all", len(runner.invocations), runner.invocations)
	}

	argv := runner.invocations[0]

	// The ADR writes the program as `bwrap`; a launcher may pass the absolute
	// path the fixed PATH resolves it to, so the name is checked as written and
	// the arguments exactly.
	if base := filepath.Base(argv[0]); base != "bwrap" {
		t.Errorf("the check started %q, want bwrap: the sandbox the CLI runs in is bubblewrap (ADR 0040)", argv[0])
	}

	if diff := cmp.Diff(bwrapArgs, argv[1:]); diff != "" {
		t.Errorf("the check started bwrap with the arguments (-want +got):\n%s", diff)
	}
}

// TestCheckBubblewrapReportsAStartThatFails pins the other side: a bwrap that
// exits non-zero, cannot be found or is refused its user namespace is an error,
// which the run turns into exit 4.
func TestCheckBubblewrapReportsAStartThatFails(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{name: "bwrap is not installed", err: errors.New("exec: \"bwrap\": executable file not found in $PATH")},
		{name: "the AppArmor profile is missing", err: errors.New("exit status 1")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runner := &runnerFake{err: tc.err}

			if err := audit.CheckBubblewrap(t.Context(), runner); err == nil {
				t.Fatalf("CheckBubblewrap() = nil, want an error: %v", tc.err)
			}

			if len(runner.invocations) != 1 {
				t.Errorf("the check started %d commands, want exactly one: it starts bwrap once and does not retry", len(runner.invocations))
			}
		})
	}
}

// TestRunReportsBubblewrapWithTheRunbooksStep pins the run's side of a sandbox
// that cannot start: exit 4 and the one line the ADR writes, which names runbook
// step 10 and its user-namespace note instead of the program's error. The
// wording is checked whole because the ADR gives it whole: the maintainer reads
// that line in a failed run and has to find the note it names.
func TestRunReportsBubblewrapWithTheRunbooksStep(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	runner := &runnerFake{err: fmt.Errorf("bwrap: setting up uid map: %w", errors.New("permission denied"))}

	recorder := &stepsRecorder{errs: map[string]error{}}

	steps := recorder.steps()
	steps.Bubblewrap = func(ctx context.Context) error {
		return audit.CheckBubblewrap(ctx, runner)
	}

	var out bytes.Buffer

	code := audit.Run{
		Root:   root,
		Inputs: testInputs(root),
		Steps:  steps,
		Out:    &out,
	}.Run(t.Context())

	if code != audit.ExitBubblewrap {
		t.Fatalf("the run's exit code is %d, want %d (audit.ExitBubblewrap)", code, audit.ExitBubblewrap)
	}

	if want := bubblewrapSentence; !strings.Contains(out.String(), want) {
		t.Errorf("the run printed %q, want it to hold %q", out.String(), want)
	}

	// The program's own error is not the summary: it says nothing of what to do.
	for _, unwanted := range []string{"uid map", "permission denied"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("the run printed %q, which holds bubblewrap's own words %q; the summary says what to fix (ADR 0040)", out.String(), unwanted)
		}
	}
}
