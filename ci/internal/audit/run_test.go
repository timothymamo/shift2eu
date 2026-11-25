// Pins one audit run's order, exit codes and output (ADR 0040's model-job
// amendment, issue #277). The six steps run in the ADR's order; a step that
// fails stops the ones after it and ends the run with that step's exit code;
// and the step prints one line - "finished", a constant class, or the ADR's
// bubblewrap sentence - never a text a step's error carries, which can hold the
// CLI's output, the token or the canary.
//
// The contract these tests hold the library to:
//
//	audit.Run{Root, Inputs, Steps, Out}.Run(ctx) int
//
// returns audit.ExitOK (0), audit.ExitFailed (1), audit.ExitUsage (2),
// audit.ExitRefused (3) or audit.ExitBubblewrap (4), the numbers the ADR's
// exit-code list gives, and writes its one line to Out. Steps is the six steps
// as function fields, so a test hands fakes and reads which ran.
package audit_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

// The names of the six steps, in the order the ADR's "In order" list gives.
const (
	stepManagedSettings = "managed settings"
	stepChecksum        = "checksum"
	stepLayout          = "audit directory"
	stepBubblewrap      = "bubblewrap"
	stepPreflight       = "preflight"
	stepAudit           = "audit"
)

// bubblewrapSentence is the one line ADR 0040 fixes for a sandbox that cannot
// start: it names the runbook step that carries the user-namespace note, not
// the program's own error.
const bubblewrapSentence = "bubblewrap cannot start: see runbook step 10, the user-namespace note"

// stepOrder is the order the ADR fixes for the six steps.
var stepOrder = []string{
	stepManagedSettings,
	stepChecksum,
	stepLayout,
	stepBubblewrap,
	stepPreflight,
	stepAudit,
}

// stepsRecorder is the fake Steps: each step records that it ran, in order, and
// answers with the error a test gave it.
type stepsRecorder struct {
	ran  []string
	errs map[string]error
}

// steps is the recorder as the run's six steps.
func (r *stepsRecorder) steps() audit.Steps {
	answer := func(step string) error {
		r.ran = append(r.ran, step)

		return r.errs[step]
	}

	return audit.Steps{
		ManagedSettings: func(string) error { return answer(stepManagedSettings) },
		Checksum:        func(string, audit.Inputs) error { return answer(stepChecksum) },
		Layout:          func(string) error { return answer(stepLayout) },
		Bubblewrap:      func(context.Context) error { return answer(stepBubblewrap) },
		Preflight:       func(context.Context, string) error { return answer(stepPreflight) },
		Audit:           func(context.Context, string) ([]byte, error) { return nil, answer(stepAudit) },
	}
}

// runSteps runs one audit with steps under a temporary root, and returns the
// exit code and everything the run printed.
func runSteps(t *testing.T, root string, steps audit.Steps) (int, string) {
	t.Helper()

	var out bytes.Buffer

	code := audit.Run{
		Root:   root,
		Inputs: testInputs(root),
		Steps:  steps,
		Out:    &out,
	}.Run(t.Context())

	return code, out.String()
}

// TestRunTakesTheStepsInTheADRsOrder pins the order of the six steps, that a
// step which fails stops the ones after it, and the exit code of each failure:
// the ADR's 3 for a refusal before the CLI ran (a managed setting, a checksum
// mismatch, an audit directory not as laid out), 4 for a sandbox that cannot
// start, 1 for a run that failed (the preflight's probes, the audit), and 0 for
// a run that finished.
//
// Every failing step's error is the same text, holding both invented secrets,
// and the preflight's is its own verdict: the ADR prints "the failure's constant
// class", so no case may show a word of the error's text.
func TestRunTakesTheStepsInTheADRsOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// fail is the step that fails, "" for a run where every step passes.
		fail string
		// wantCode is the exit code the ADR gives that step's failure.
		wantCode int
		// wantOut are the substrings of the run's output the ADR fixes.
		wantOut []string
		// err, when set, is the failure the step answers with instead of the
		// one every other failing step gives.
		err func(t *testing.T) error
	}{
		{name: "every step passes", wantCode: audit.ExitOK, wantOut: []string{"finished"}},
		{name: "the managed settings refuse the run", fail: stepManagedSettings, wantCode: audit.ExitRefused},
		{name: "the checksum refuses the run", fail: stepChecksum, wantCode: audit.ExitRefused},
		{name: "the audit directory not as laid out refuses the run", fail: stepLayout, wantCode: audit.ExitRefused},
		{name: "bubblewrap cannot start", fail: stepBubblewrap, wantCode: audit.ExitBubblewrap, wantOut: []string{bubblewrapSentence}},
		{
			name:     "the preflight does not pass",
			fail:     stepPreflight,
			wantCode: audit.ExitFailed,
			// The one failure whose line the ADR spells out: the preflight's own
			// verdict, which names each probe that did not fail and runbook step
			// 10. Every other step's failure is its constant class alone.
			wantOut: []string{"probe 2, TCP connect", "runbook step 10"},
			err:     preflightVerdict,
		},
		{name: "the audit fails", fail: stepAudit, wantCode: audit.ExitFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &stepsRecorder{errs: map[string]error{}}
			if tc.fail != "" {
				recorder.errs[tc.fail] = errors.New(secretText())
			}

			if tc.err != nil {
				recorder.errs[tc.fail] = tc.err(t)
			}

			code, out := runSteps(t, t.TempDir(), recorder.steps())

			if code != tc.wantCode {
				t.Errorf("the run's exit code is %d, want %d", code, tc.wantCode)
			}

			// The steps that ran are the ones before the failure, the failure
			// itself included: a step after it never runs.
			ran := stepOrder
			if tc.fail != "" {
				for i, step := range stepOrder {
					if step == tc.fail {
						ran = stepOrder[:i+1]

						break
					}
				}
			}

			if diff := cmp.Diff(ran, recorder.ran); diff != "" {
				t.Errorf("the steps that ran, in order (-want +got):\n%s", diff)
			}

			for _, want := range tc.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("the run printed %q, want it to hold %q", out, want)
				}
			}

			// The step's failure is a constant class, never the text of its
			// error: that text can hold the CLI's output, the token and the
			// canary (ADR 0040, "The audit's programs").
			for _, secret := range []string{testToken, testCanary, "the-clis-own-text"} {
				if strings.Contains(out, secret) {
					t.Errorf("the run printed %q, which holds %q; a failure is printed as its constant class alone (ADR 0040)", out, secret)
				}
			}
		})
	}
}

// TestRunPrintsFinishedAlone pins the line a finished run prints: "finished" and
// nothing else. The step's output is the workflow's step summary, so a second
// line is a second thing the run says.
func TestRunPrintsFinishedAlone(t *testing.T) {
	t.Parallel()

	code, out := runSteps(t, t.TempDir(), (&stepsRecorder{errs: map[string]error{}}).steps())

	if code != audit.ExitOK {
		t.Fatalf("the run's exit code is %d, want %d", code, audit.ExitOK)
	}

	if want := "finished\n"; out != want {
		t.Errorf("the run printed %q, want %q", out, want)
	}
}

// preflightVerdict is the failure the preflight step answers with: the
// judgement of a run whose control returned the nonce and every probe was
// refused, probe 2's listener excepted.
func preflightVerdict(t *testing.T) error {
	t.Helper()

	plan := audit.PreflightPlan(preflightTargets())
	if len(plan) == 0 {
		t.Fatal("the preflight's plan is empty: it lists the calls the model is asked to make (ADR 0040)")
	}

	err := audit.Judge(audit.Judgement{
		Plan:       plan,
		Nonce:      preflightNonce,
		Transcript: transcriptOf(plan, passingAnswers(plan, preflightNonce)),
		Effects:    fakeEffects{listener: true},
	})
	if err == nil {
		t.Fatal("Judge() = nil for a run whose probe 2's listener accepted a connection")
	}

	return err
}

// permissionModeRefusal is the one line a run prints when the session reports a
// permission mode other than the one the launch asked for (issue #592).
func permissionModeRefusal(reported string) string {
	return "refused: the session's permission mode is not the one asked for: expected default, reported " + reported
}

// permissionModeFailure is the launcher's refusal of such a session, wrapped as a
// step wraps the error its CLI run left.
func permissionModeFailure(reported string) error {
	return fmt.Errorf("%w: %w", errors.New("the step's CLI run failed"), &claudecode.PermissionModeError{Expected: "default", Reported: reported})
}

// TestRunPrintsThePermissionModeRefusalAlone pins issue #592's line: a session
// whose init element reports a mode other than the one the launch asked for fails
// the run with ExitFailed and with the fixed sentence alone - the expected mode
// and the reported one, a known mode or a class - whichever of the two model
// steps ran into it. The step's own error text never reaches the log.
func TestRunPrintsThePermissionModeRefusalAlone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// fail is the step whose CLI run left the refusal.
		fail string
		// reported is the mode or the class the session reported.
		reported string
	}{
		{name: "the preflight's session reports dontAsk", fail: stepPreflight, reported: "dontAsk"},
		{name: "the audit's session reports auto", fail: stepAudit, reported: "auto"},
		{name: "the preflight's session prints no init element", fail: stepPreflight, reported: "missing"},
		{name: "the audit's session prints its init element twice", fail: stepAudit, reported: "two init lines"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &stepsRecorder{errs: map[string]error{tc.fail: permissionModeFailure(tc.reported)}}

			code, out := runSteps(t, t.TempDir(), recorder.steps())

			if code != audit.ExitFailed {
				t.Errorf("the run's exit code is %d, want %d: a session of another permission mode is a failed run", code, audit.ExitFailed)
			}

			if want := permissionModeRefusal(tc.reported) + "\n"; out != want {
				t.Errorf("the run printed %q, want %q: the refusal is one fixed line, holding the expected mode and a known mode or a class, never a byte of the element", out, want)
			}
		})
	}
}
