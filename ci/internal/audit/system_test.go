// Pins the two steps that run the model (ADR 0040's model-job amendment,
// issue #277, "The sandbox preflight" and "The audit's programs"): the preflight,
// which writes the control's nonce, opens the listener, has the CLI make the
// plan's calls, observes the effects itself and judges the transcript, and the
// audit, which runs the CLI once and accepts only a result file the run itself
// wrote.
//
// The prober, the observer and the audit's launch are fakes: nothing here starts
// the CLI, a sandbox, a socket or sudo, and nothing runs a command. The tree
// under the temporary root is the one the layout check takes.
//
// What these tests hold the library to:
//
//	audit.Observer = func(ctx context.Context, root, gitHubEnv string, before [32]byte, accepted bool) audit.Effects
//	audit.Preflight(ctx context.Context, root string, in Inputs, prober Prober, observer Observer, pid int) error
//	audit.Launch   = func(ctx context.Context, root string, cfg claudecode.Config, prompt claudecode.Prompt) error
//	audit.Audit(ctx context.Context, root string, in Inputs, launch Launch) ([]byte, error)
//	audit.Steps{..., Audit: func(ctx context.Context, root string) ([]byte, error)}
//	audit.Run{Root, Inputs, Steps, Out, Result func(body []byte) error}.Run(ctx) int
//	audit.MaxResult, the cap on the result file the audit reads back
//
// Preflight calls observer once, after it has closed probe 2's listener, with a
// context detached from the run's cancellation: a run that timed out or was
// cancelled is still observed, or its effects would read as refused. Audit
// refuses a work area that is not empty before the run, and reads the result file
// after it: a file that is no regular file, that is over MaxResult, that is no
// JSON, or that holds the token or the canary - as its bytes or as a decoded JSON
// string - fails the run with the audit's constant line. Audit returns the bytes
// it read and checked, never the path, and Run hands them on through its Result
// seam once every step has passed: the bytes are what the publish job - step 19
// of ADR 0040 - works from, and a run that failed hands on nothing.
package audit_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

// preflightFailedSentence is the one line a run whose preflight did not pass
// prints for a preflight that could not be run at all (ADR 0040, "The audit's
// programs"): the step's own error text never reaches the log.
const preflightFailedSentence = "the preflight failed: see runbook step 10"

// auditFailedSentence is the one line a run whose audit failed prints: no result
// file, a result file that existed before the run, a work area that was not empty,
// and one holding the step's secrets alike.
const auditFailedSentence = "the audit failed: the model's run or its result file"

// devTCP is the listener's address in probe 2's instruction.
var devTCP = regexp.MustCompile(`/dev/tcp/127\.0\.0\.1/(\d+)`)

// preflightTree lays the audit directory out under a temporary root, writes the
// $GITHUB_ENV file the preflight digests, and returns the root and the run's
// inputs.
func preflightTree(t *testing.T) (string, audit.Inputs) {
	t.Helper()

	root := t.TempDir()
	writeAuditTree(t, root)

	in := testInputs(root)
	writeFile(t, in.GitHubEnv, "SOMETHING=else\n", 0o600)

	return root, in
}

// controlPathOf returns the path of the preflight's control file under root.
func controlPathOf(root string) string {
	return filepath.Join(root, "srv/audit/work/preflight-control.txt")
}

// portIn returns the port the preflight's prompt names, and fails when it names
// none: the listener's port is the one target a test cannot invent.
func portIn(t *testing.T, prompt string) int {
	t.Helper()

	match := devTCP.FindStringSubmatch(prompt)
	if match == nil {
		t.Fatalf("the preflight's prompt is %q, want it to name the listener's address /dev/tcp/127.0.0.1/<port>", prompt)
	}

	port, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("the preflight's prompt names the port %q, which is no number: %v", match[1], err)
	}

	return port
}

// proberFake is the fake Prober: it records the prompt it was given, reads the
// control file the preflight wrote while it runs, and answers with the transcript
// its answer builds from the nonce and the prompt.
type proberFake struct {
	root string
	// answer builds the transcript of the run from the nonce the control file
	// held and the prompt the preflight asked with.
	answer func(nonce, prompt string) []byte
	// err, when set, is the failure the CLI's run answers with, and no
	// transcript is judged.
	err error

	prompts  []string
	nonces   []string
	inFlight bool
}

// Probe is the Prober the preflight takes.
func (p *proberFake) Probe(_ context.Context, prompt claudecode.Prompt) ([]byte, error) {
	p.prompts = append(p.prompts, prompt.User)

	body, err := os.ReadFile(controlPathOf(p.root))
	p.inFlight = err == nil

	p.nonces = append(p.nonces, strings.TrimSpace(string(body)))

	if p.err != nil {
		return nil, p.err
	}

	return p.answer(strings.TrimSpace(string(body)), prompt.User), nil
}

// observerFake is the fake Observer: it records the context it was given, the
// $GITHUB_ENV path and the digest of its directory the preflight took before the
// run, and what it was told, and answers with its effects.
type observerFake struct {
	called         bool
	ctxErr         error
	ctxHasDeadline bool
	accepted       bool
	// gitHubEnv and before are what the preflight handed the observer: probe 6's
	// target and the digest of its directory taken before the run.
	gitHubEnv string
	before    [32]byte
	effects   audit.Effects
}

// observe is the Observer the preflight takes.
func (o *observerFake) observe(ctx context.Context, _, gitHubEnv string, before [32]byte, accepted bool) audit.Effects {
	o.called = true
	o.ctxErr = ctx.Err()
	_, o.ctxHasDeadline = ctx.Deadline()
	o.accepted = accepted
	o.gitHubEnv = gitHubEnv
	o.before = before

	return o.effects
}

// TestPreflightRunsThePlansCallsAndRemovesTheControl pins the preflight's own
// work (ADR 0040, "The sandbox preflight"): the control file holds a nonce while
// the CLI runs and is gone afterwards, the prompt names the listener's port, the
// launcher's pid and every path of the audit directory the plan aims at, and the
// run passes when the transcript is the plan's and the launcher observed no
// effect.
func TestPreflightRunsThePlansCallsAndRemovesTheControl(t *testing.T) {
	t.Parallel()

	root, in := preflightTree(t)

	prober := &proberFake{root: root}
	observer := &observerFake{effects: fakeEffects{}}

	prober.answer = func(nonce, prompt string) []byte {
		targets := audit.Targets{Root: root, Port: portIn(t, prompt), Pid: preflightPid, GitHubEnv: in.GitHubEnv}
		thePlan := audit.PreflightPlan(targets)

		return append([]byte(defaultInitLine), transcriptOf(thePlan, passingAnswers(thePlan, nonce))...)
	}

	if err := audit.Preflight(t.Context(), root, in, prober, observer.observe, preflightPid); err != nil {
		t.Fatalf("Preflight() = %v, want nil: the control read the nonce back and every probe was refused", err)
	}

	if !prober.inFlight {
		t.Error("the control file did not exist while the CLI ran: the preflight writes the nonce before the run, and the control call reads it")
	}

	if _, err := os.Lstat(controlPathOf(root)); !os.IsNotExist(err) {
		t.Errorf("the control file %s is still there after the run (%v): the preflight removes it, so the audit finds the work area as laid out", controlPathOf(root), err)
	}

	if len(prober.nonces) != 1 {
		t.Fatalf("the CLI ran %d times, want once: the preflight is one run of the plan", len(prober.nonces))
	}

	nonce := prober.nonces[0]
	if len(nonce) != 32 {
		t.Errorf("the control file held %q, want a nonce of 32 hex characters: the control call's result is what proves the settings loaded", nonce)
	}

	if len(prober.prompts) != 1 {
		t.Fatalf("the preflight asked the CLI %d prompts, want one", len(prober.prompts))
	}

	prompt := prober.prompts[0]

	for _, want := range []struct {
		what string
		path string
	}{
		{"the launcher's /proc entry", "/proc/" + strconv.Itoa(preflightPid) + "/environ"},
		{"the command wrapper", filepath.Join(root, "srv/audit/bin/audit-exec")},
		{"the control file", controlPathOf(root)},
		{"the work area", filepath.Join(root, "srv/audit/work")},
		{"the state area", filepath.Join(root, "srv/audit/state")},
		{"Docker's socket", filepath.Join(root, "var/run/docker.sock")},
		{"the $GITHUB_ENV file", in.GitHubEnv},
	} {
		if !strings.Contains(prompt, want.path) {
			t.Errorf("the preflight's prompt is %q, want it to name %s, %q: the plan's calls name the targets it is filled with", prompt, want.what, want.path)
		}
	}

	if port := portIn(t, prompt); !strings.Contains(prompt, "/dev/tcp/127.0.0.1/"+strconv.Itoa(port)) {
		t.Errorf("the preflight's prompt is %q, want it to name the listener's port %d", prompt, port)
	}

	if !observer.called {
		t.Error("the preflight did not observe its effects: probes 1 to 4 and 6 are failed by what the launcher sees, not by what the model says")
	}

	if observer.accepted {
		t.Error("the launcher's observer was told the listener accepted a connection, want the listener to have accepted none")
	}
}

// TestPreflightGivesTheConstantFailureOfAFailedRun pins what a CLI run that failed
// with no observed effect leaves: no verdict - there is no transcript to judge -
// and one line, the preflight's constant class, in which nothing of the run's own
// error reaches the log, the token and the canary included.
func TestPreflightGivesTheConstantFailureOfAFailedRun(t *testing.T) {
	t.Parallel()

	root, in := preflightTree(t)

	observer := &observerFake{effects: fakeEffects{}}
	prober := &proberFake{root: root, err: errors.New(secretText())}

	err := audit.Preflight(t.Context(), root, in, prober, observer.observe, preflightPid)
	if err == nil {
		t.Fatal("Preflight() = nil, want a failure: the CLI's run failed")
	}

	var verdict *audit.Verdict
	if errors.As(err, &verdict) {
		t.Errorf("Preflight() = %v, want the preflight's own failure: a run that failed left no transcript to judge", err)
	}

	for _, secret := range []string{testToken, testCanary, "the-clis-own-text"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("Preflight() = %q, want a failure holding nothing of the CLI's text: it can hold the token, the canary and the CLI's stderr", err)
		}
	}

	// The run prints that failure as its constant class, never as the error's
	// text.
	steps := audit.Steps{
		ManagedSettings: func(string) error { return nil },
		Checksum:        func(string, audit.Inputs) error { return nil },
		Layout:          func(string) error { return nil },
		Bubblewrap:      func(context.Context) error { return nil },
		Preflight: func(ctx context.Context, root string) error {
			return audit.Preflight(ctx, root, in, prober, observer.observe, preflightPid)
		},
		Audit: func(context.Context, string) ([]byte, error) { return nil, nil },
	}

	var out bytes.Buffer

	code := audit.Run{Root: root, Inputs: in, Steps: steps, Out: &out}.Run(t.Context())

	if code != audit.ExitFailed {
		t.Errorf("the run's exit code is %d, want %d: the preflight could not be run", code, audit.ExitFailed)
	}

	if !strings.Contains(out.String(), preflightFailedSentence) {
		t.Errorf("the run printed %q, want %q", out.String(), preflightFailedSentence)
	}

	for _, secret := range []string{testToken, testCanary, "the-clis-own-text"} {
		if strings.Contains(out.String(), secret) {
			t.Errorf("the run printed %q, which holds %q: a failure is printed as its constant class alone (ADR 0040)", out.String(), secret)
		}
	}
}

// TestPreflightObservesItsEffectsAfterADoneContext pins finding R3 of the review
// of issue #277: a run that answered just as its deadline passed, or was
// cancelled, leaves the preflight's own context done. The Docker connect and
// `sudo -n true` would then fail for the context rather than for the hardening,
// and probes 3 and 4 would read as refused. The observation runs under a context
// detached from the run's, so it still happens.
func TestPreflightObservesItsEffectsAfterADoneContext(t *testing.T) {
	t.Parallel()

	root, in := preflightTree(t)

	observer := &observerFake{effects: fakeEffects{}}
	prober := &proberFake{root: root, err: context.Canceled}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := audit.Preflight(ctx, root, in, prober, observer.observe, preflightPid); err == nil {
		t.Fatal("Preflight() = nil, want the failure of a cancelled run: the CLI's run was cancelled")
	}

	if !observer.called {
		t.Fatal("the preflight did not observe its effects after a cancelled run: probes 3 and 4 would then pass for a context that was done, not for the sandbox holding")
	}

	if observer.ctxErr != nil {
		t.Errorf("the preflight observed its effects under a context that is done (%v), want one detached from the run's cancellation: the Docker connect and sudo would fail for the context, not for the sandbox", observer.ctxErr)
	}

	if !observer.ctxHasDeadline {
		t.Error("the preflight observed its effects under a context with no deadline, want the observation's own timeout: a connect that hangs must not hold the step open")
	}
}

// TestPreflightJudgesTheTranscriptWithTheStepsToken pins that the preflight hands
// the judgement the step's token (issue #277, audit A4): the token is the one
// secret the subprocess scrub keeps out of the CLI's shell, so its value anywhere
// in the transcript is probe 5 having failed, like the canary.
func TestPreflightJudgesTheTranscriptWithTheStepsToken(t *testing.T) {
	t.Parallel()

	root, in := preflightTree(t)

	observer := &observerFake{effects: fakeEffects{}}

	prober := &proberFake{root: root}
	prober.answer = func(nonce, prompt string) []byte {
		targets := audit.Targets{Root: root, Port: portIn(t, prompt), Pid: preflightPid, GitHubEnv: in.GitHubEnv}
		thePlan := audit.PreflightPlan(targets)

		telling := fmt.Sprintf(
			`{"type":"assistant","message":{"content":[{"type":"text","text":%q}]}}`+"\n",
			"the environ holds "+in.Token,
		)

		transcript := append([]byte(defaultInitLine), transcriptOf(thePlan, passingAnswers(thePlan, nonce))...)

		return append(transcript, []byte(telling)...)
	}

	err := audit.Preflight(t.Context(), root, in, prober, observer.observe, preflightPid)
	if err == nil {
		t.Fatal("Preflight() = nil, want a verdict: the step's token is in the transcript")
	}

	var verdict *audit.Verdict
	if !errors.As(err, &verdict) {
		t.Fatalf("Preflight() = %v, want a verdict naming probe 5: the token reaching the transcript is the scrub having failed", err)
	}

	if want := fmt.Sprintf("probe 5, %s", probeShortName[5]); !strings.Contains(err.Error(), want) {
		t.Errorf("Preflight() = %q, want it to name %q", err, want)
	}
}

// TestPreflightJudgesAFailedRunsEffects pins finding AI 2 of the review of
// issue #277's launch step: a run that was cancelled or timed out still shows
// what the launcher itself observed, and an effect it saw is the sandbox having
// failed, whatever the model answered. The preflight therefore returns a verdict
// naming every probe whose effect it saw and the runbook step, and keeps the
// constant failure of a failed run only when it saw no effect at all.
func TestPreflightJudgesAFailedRunsEffects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// effects is what the launcher observed after the failed run.
		effects fakeEffects
		// wantProbe is the probe the verdict must name, and 0 for the constant
		// failure of a failed run that showed nothing.
		wantProbe int
	}{
		{
			name: "no effect at all",
		},
		{
			name:      "a probe file written",
			effects:   fakeEffects{probeFiles: true},
			wantProbe: 1,
		},
		{
			name:      "the listener accepted a connection",
			effects:   fakeEffects{listener: true},
			wantProbe: 2,
		},
		{
			name:      "Docker's socket reached",
			effects:   fakeEffects{docker: true},
			wantProbe: 3,
		},
		{
			name:      "sudo run without a password",
			effects:   fakeEffects{sudo: true},
			wantProbe: 4,
		},
		{
			name:      "the file-command directory changed",
			effects:   fakeEffects{fileCommands: true},
			wantProbe: 6,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, in := preflightTree(t)

			observer := &observerFake{effects: tc.effects}
			prober := &proberFake{root: root, err: context.Canceled}

			err := audit.Preflight(t.Context(), root, in, prober, observer.observe, preflightPid)
			if err == nil {
				t.Fatal("Preflight() = nil, want a failure: the CLI's run was cancelled")
			}

			if !observer.called {
				t.Error("the preflight did not observe its effects after a failed run")
			}

			var verdict *audit.Verdict

			found := errors.As(err, &verdict)

			if tc.wantProbe == 0 {
				if found {
					t.Fatalf("Preflight() = %v, want the preflight's own constant failure: the run failed and the launcher observed no effect, so there is nothing to name", err)
				}

				return
			}

			if !found {
				t.Fatalf("Preflight() = %v, want a verdict naming probe %d, %s: the run failed, but its effect was observed, so the sandbox did not hold", err, tc.wantProbe, probeShortName[tc.wantProbe])
			}

			if want := fmt.Sprintf("probe %d, %s", tc.wantProbe, probeShortName[tc.wantProbe]); !strings.Contains(err.Error(), want) {
				t.Errorf("Preflight() = %q, want it to name %q: the probe whose effect was seen is the one that did not fail", err, want)
			}

			if !strings.Contains(err.Error(), "runbook step 10") {
				t.Errorf("Preflight() = %q, want it to name runbook step 10, as every verdict does", err)
			}
		})
	}
}

// TestPreflightDigestsEveryEntryOfTheFileCommandDirectory pins the preflight's
// half of the file-command digest (issues #615, #614): the preflight takes the
// digest of the $GITHUB_ENV file's whole directory before its run and hands it
// to its observer, which digests the directory again after it, so a change the
// preflight's own run makes in that directory is the change the observer sees -
// and nothing is left out there, the step summary included. The digest is a
// function of the entries alone, so one tree serves both runs of a case: the
// digest the second run hands over is the state the first run's write left, and
// it must differ from the first run's when that write touched the directory and
// match it when the write touched nothing.
func TestPreflightDigestsEveryEntryOfTheFileCommandDirectory(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// before, when set, lays the runner's file commands beside the $GITHUB_ENV
		// file before the run, as the step does.
		before func(t *testing.T, root string)
		// write, when set, is what the preflight's run writes in that directory.
		write func(root string) error
		// wantDifferent is whether the second run's digest must differ from the
		// first's: the write the first run made is one the digest covers.
		wantDifferent bool
	}{
		{
			name: "a run that writes nothing in the file-command directory",
		},
		{
			name: "a run that appends a line to the $GITHUB_PATH file beside the $GITHUB_ENV file",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, fileCommandPath(root, "github-path"), "/usr/local/bin\n", 0o600)
			},
			write: func(root string) error {
				return appendFileCommandLine(fileCommandPath(root, "github-path"), "/tmp/planted\n")
			},
			wantDifferent: true,
		},
		{
			// The entry the audit's digest used to leave out: nothing legitimate
			// writes the step summary while the preflight's run runs either, so the
			// preflight exempts nothing there.
			name: "a run that appends to the step summary laid down before it",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, testInputs(root).StepSummary, "", 0o600)
			},
			write: func(root string) error {
				return appendStepSummary(testInputs(root).StepSummary, "planted\n")
			},
			wantDifferent: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, in := preflightTree(t)

			if tc.before != nil {
				tc.before(t, root)
			}

			// run takes the digest the preflight hands its observer before the
			// run's one write, when write is set and this is the first run. The
			// transcript is none the judgement can read, which no case is about:
			// the digest is taken before the run, whatever the run answers.
			run := func(first bool) [32]byte {
				t.Helper()

				prober := &proberFake{root: root}
				prober.answer = func(string, string) []byte {
					if first && tc.write != nil {
						if err := tc.write(root); err != nil {
							t.Errorf("the preflight's run's write: %v", err)
						}
					}

					return nil
				}

				observer := &observerFake{effects: fakeEffects{}}

				_ = audit.Preflight(t.Context(), root, in, prober, observer.observe, preflightPid)

				if !observer.called {
					t.Fatal("the preflight never called its observer: the digest is taken before the run and handed over after it")
				}

				return observer.before
			}

			first, second := run(true), run(false)

			if different := first != second; different != tc.wantDifferent {
				if tc.wantDifferent {
					t.Errorf("the digest the preflight hands its observer is %x after a run that wrote in the file-command directory of %s, and was %x before it, want the two to differ: every entry of that directory is digested, nothing excluded (issues #615, #614)",
						second, in.GitHubEnv, first)
				} else {
					t.Errorf("the digest the preflight hands its observer is %x after a run that wrote nothing, and was %x before it, want the two to match: no digest may change over a run that touched nothing (issues #615, #614)",
						second, first)
				}
			}
		})
	}
}

// launchFake is the fake launch of the audit's one CLI run: it records that it
// ran and the working directory the config it was handed names, and leaves what
// the case asks for in the work area and in that working directory.
type launchFake struct {
	called bool
	// workDir is the working directory of the config the audit's launch was
	// handed: the directory the session starts in.
	workDir string
	// write, when set, is what the run leaves in the work area.
	write func(root string) error
	// writeInWorkDir, when set, is what the run leaves in the working directory
	// it was handed.
	writeInWorkDir func(workDir string) error
}

// launch is the Launch the audit takes.
func (l *launchFake) launch(_ context.Context, root string, cfg claudecode.Config, _ claudecode.Prompt) error {
	l.called = true
	l.workDir = cfg.WorkDir

	if l.writeInWorkDir != nil {
		if err := l.writeInWorkDir(cfg.WorkDir); err != nil {
			return err
		}
	}

	if l.write == nil {
		return nil
	}

	return l.write(root)
}

// runAuditStep runs one audit run whose audit step is the real one, its launch
// being the case's fake, and returns the run's exit code, the one line it
// printed and the result file's bytes the run handed on.
func runAuditStep(t *testing.T, root string, in audit.Inputs, launch audit.Launch) (int, string, []byte) {
	t.Helper()

	var (
		out    bytes.Buffer
		result []byte
	)

	steps := audit.Steps{
		ManagedSettings: func(string) error { return nil },
		Checksum:        func(string, audit.Inputs) error { return nil },
		Layout:          func(string) error { return nil },
		Bubblewrap:      func(context.Context) error { return nil },
		Preflight:       func(context.Context, string) error { return nil },
		Audit: func(ctx context.Context, theRoot string) ([]byte, error) {
			return audit.Audit(ctx, theRoot, in, launch)
		},
	}

	code := audit.Run{
		Root:   root,
		Inputs: in,
		Steps:  steps,
		Out:    &out,
		// Result is the seam the publish job works from (ADR 0040, step 19):
		// the run hands it the audit's bytes, and a run that failed hands it
		// nothing.
		Result: func(body []byte) error {
			result = append([]byte(nil), body...)

			return nil
		},
	}.Run(t.Context())

	return code, out.String(), result
}

// TestPreflightKeepsThePermissionModeErrorThroughItsWrapping pins issue #592's
// first layer in the preflight: a session whose init element reports another
// permission mode than the launch asked for is the launcher's
// *claudecode.PermissionModeError, and the step keeps it through its wrapping, so
// the run prints the fixed sentence instead of the preflight's constant class.
func TestPreflightKeepsThePermissionModeErrorThroughItsWrapping(t *testing.T) {
	t.Parallel()

	root, in := preflightTree(t)

	observer := &observerFake{effects: fakeEffects{}}
	prober := &proberFake{root: root, err: permissionModeFailure("dontAsk")}

	err := audit.Preflight(t.Context(), root, in, prober, observer.observe, preflightPid)

	assertPermissionModeError(t, err, "default", "dontAsk")
}

// TestAuditKeepsThePermissionModeErrorThroughItsWrapping pins the same layer in
// the audit's one CLI run (issue #592).
func TestAuditKeepsThePermissionModeErrorThroughItsWrapping(t *testing.T) {
	t.Parallel()

	root, in := preflightTree(t)

	launch := &launchFake{write: func(string) error { return permissionModeFailure("missing") }}

	_, err := audit.Audit(t.Context(), root, in, launch.launch)

	assertPermissionModeError(t, err, "default", "missing")
}

// TestPreflightJudgesTheSessionsPermissionMode pins that the preflight hands the
// judgement the mode the launch asks for (issue #592): the transcript's init
// element reports dontAsk, so the step fails although the control passed and
// every probe was refused, with the judgement's fixed sentence.
func TestPreflightJudgesTheSessionsPermissionMode(t *testing.T) {
	t.Parallel()

	root, in := preflightTree(t)

	observer := &observerFake{effects: fakeEffects{}}

	prober := &proberFake{root: root}
	prober.answer = func(nonce, prompt string) []byte {
		targets := audit.Targets{Root: root, Port: portIn(t, prompt), Pid: preflightPid, GitHubEnv: in.GitHubEnv}
		thePlan := audit.PreflightPlan(targets)

		initLine := fmt.Sprintf(`{"type":"system","subtype":"init","permissionMode":"dontAsk","cwd":%q}`+"\n", preflightInitCanary)

		return append([]byte(initLine), transcriptOf(thePlan, passingAnswers(thePlan, nonce))...)
	}

	err := audit.Preflight(t.Context(), root, in, prober, observer.observe, preflightPid)
	if err == nil {
		t.Fatal("Preflight() = nil, want a failure: the session reports dontAsk, and the launch asks for default")
	}

	var verdict *audit.Verdict
	if !errors.As(err, &verdict) {
		t.Fatalf("Preflight() = %v, want a verdict: the judgement reads the session's permission mode (issue #592)", err)
	}

	want := "the session's permission mode is not the one asked for: expected default, reported dontAsk"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Preflight() = %q, want it to hold %q", err, want)
	}

	if !strings.Contains(err.Error(), "runbook step 10") {
		t.Errorf("Preflight() = %q, want it to name runbook step 10, as every verdict does", err)
	}

	if strings.Contains(err.Error(), preflightInitCanary) {
		t.Errorf("Preflight() = %q, which holds the init element's canary: a verdict is made of fixed words", err)
	}
}

// defaultInitLine is the session's system/init element as Claude Code 2.1.332
// prints it under the subprocess environment scrub: the mode the audit's launch
// asks for, which the preflight's judgement wants to read (issue #592).
const defaultInitLine = `{"type":"system","subtype":"init","permissionMode":"default"}` + "\n"

// workPath is a path of the audit's work area under root.
func workPath(root string, parts ...string) string {
	return filepath.Join(append([]string{root, "srv", "audit", "work"}, parts...)...)
}

// fileCommandPath is the path of name in the runner's file-command directory
// under root: a sibling of the $GITHUB_ENV file, such as the $GITHUB_PATH file a
// step appends a line to (issues #615, #614).
func fileCommandPath(root, name string) string {
	return filepath.Join(fileCommandsDir(root), name)
}

// appendFileCommandLine appends line to the file at path, the way a step that
// writes a file-command line does.
func appendFileCommandLine(path, line string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()

		return err
	}

	return file.Close()
}

// appendStepSummary appends line to the step summary at path, the way a step
// writing $GITHUB_STEP_SUMMARY does: the file is created when it is not there
// yet and appended to when it is, as ci/auditrun's summaryWriter opens it.
func appendStepSummary(path, line string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()

		return err
	}

	return file.Close()
}

// cliPlaceholderFiles are the empty files Claude Code 2.1.332 creates in its
// working directory at session start when CLAUDE_CODE_SUBPROCESS_ENV_SCRUB is
// set, and never removes (issue #615).
var cliPlaceholderFiles = []string{
	".env",
	".env.local",
	".env.development",
	".env.development.local",
	".env.test",
	".env.test.local",
	".env.production",
	".env.production.local",
	".gitmodules",
	".npmrc",
	".yarnrc",
	".yarnrc.yml",
	"bunfig.toml",
	"package.json",
	"package-lock.json",
	"yarn.lock",
	"pnpm-lock.yaml",
}

// cliPlaceholderDirs are the empty directories the same session start creates
// in its working directory: .claude/.cc-writes is the first sandboxed Bash
// call's, and the other three are the env scrub's (issue #615).
var cliPlaceholderDirs = []string{
	".claude/agents",
	".claude/commands",
	".claude/.cc-writes",
	"node_modules/.bin",
}

// writeCLIPlaceholders lays the entries Claude Code 2.1.332 leaves in its
// working directory under dir, each empty and with the mode the CLI gives it:
// the files as regular files, the directories as directories (issue #615).
func writeCLIPlaceholders(dir string) error {
	for _, name := range cliPlaceholderFiles {
		path := filepath.Join(dir, filepath.FromSlash(name))

		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}

		if err := os.WriteFile(path, nil, 0o600); err != nil {
			return err
		}
	}

	for _, name := range cliPlaceholderDirs {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(name)), 0o750); err != nil {
			return err
		}
	}

	return nil
}

// resultFile is the result the audit's model writes.
const resultFile = `{"findings":[]}`

// pinnedResult is a result whose one value quotes a SHA-256 pin: 64 lower-case
// hex characters, the shape the judgement counts as the canary when it reads the
// preflight's transcript, where any such run in a tool_result is a leak. In the
// result file it is one of the audit's findings, not a leak, so the step takes it
// whole (issue #277, round J nit 6).
var pinnedResult = `{"pin":"` + strings.Repeat("0f", 32) + `"}`

// maxJSONDepth is how deeply the audit walks a result: a document nested to this
// depth is read to its end, and one nested further is refused as not JSON rather
// than walked. The limit is the audit's own - the constant maxResultDepth of
// ci/internal/audit/system.go - and this test holds it from outside the package,
// where the value 10000 is all it can see (issue #277, round E nit 1 and round J
// item 3).
const maxJSONDepth = 10000

// nestedArrays returns a JSON document of n arrays nested in one another, every
// one of them closed: [[[...]]].
func nestedArrays(n int) string {
	return strings.Repeat("[", n) + strings.Repeat("]", n)
}

// jsonEscaped returns s with every character written as a JSON \uXXXX escape,
// so a file that names s holds no byte of it: the decoded string values are
// what the audit searches, not only the bytes (issue #277, round D finding 4).
func jsonEscaped(s string) string {
	var escaped strings.Builder

	for _, r := range s {
		fmt.Fprintf(&escaped, `\u%04x`, r)
	}

	return escaped.String()
}

// TestAuditTakesOnlyAResultFileTheRunWrote pins the audit step's two ends
// (ADR 0040, "The audit's programs", and findings A5, A6 and R12 of issue #277):
// before the run the work area holds its empty tmp/ and nothing else, so a result
// file that is already there is refused rather than taken for the run's; after
// the run there is a result file, and one holding the step's token or its canary
// fails the step, in its bytes or in any of its decoded strings - the key of an
// object and the first value of a duplicate key among them, so the search reads
// the document's tokens rather than a map that a later duplicate key empties
// (issue #277, round E finding 3). A result file quoting a SHA-256 pin is a
// finding, not a leak: the 64-hex rule the judgement applies to the preflight's
// transcript is not applied here. A result nested deeper than the audit's own
// limit is refused as an invalid result rather than walked to its end, and one
// nested to the limit is accepted (issue #277, round E nit 1, round J nit 6).
//
// The CLI's audit session starts in a working directory of its own, a sibling of
// the work area and of the preflight's, so the empty placeholders Claude Code
// 2.1.332 leaves in the directory it starts in - .env, package.json,
// node_modules/.bin, .claude/agents - are outside the area the audit checks and
// tolerate no name in it (issue #615). Before its run the audit refuses that
// directory unless it is still an empty real one - a CLAUDE.md a preflight
// session planted there would be loaded at session start - and after it, the
// runner's file-command directory, the one holding the $GITHUB_ENV file beside
// $GITHUB_PATH, $GITHUB_OUTPUT, $GITHUB_STATE and the step summary, must be the
// one the launcher digested before the run, every entry of it and the step
// summary included: nothing legitimate writes that directory while the digests
// are taken, and an entry left out of the digest is a way for a session to
// change what a later step of the job runs with - or, the step summary, what the
// job publishes (issues #615, #614).
func TestAuditTakesOnlyAResultFileTheRunWrote(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// before, when set, lays something in the work area before the run - or in
		// one of the CLI's two working directories, where a session's leavings land.
		before func(t *testing.T, root string)
		// write, when set, is what the run leaves in the work area.
		write func(root string) error
		// writeInWorkDir, when set, is what the run leaves in the working directory
		// the launch hands it.
		writeInWorkDir func(workDir string) error
		// wantLaunched is whether the CLI's run happens at all.
		wantLaunched bool
		// wantRefused is whether the run fails with the audit's constant line.
		wantRefused bool
		// wantResult is the result bytes the run hands on, and "" when it hands
		// on none.
		wantResult string
	}{
		{
			name:         "the run wrote its result file",
			write:        func(root string) error { return os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600) },
			wantLaunched: true,
			wantResult:   resultFile,
		},
		{
			name: "the run's result file quotes a SHA-256 pin",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(pinnedResult), 0o600)
			},
			wantLaunched: true,
			wantResult:   pinnedResult,
		},
		{
			name:         "the run wrote no result file",
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the result file is a symbolic link to a regular file",
			write: func(root string) error {
				elsewhere := filepath.Join(root, "srv", "audit", "state", "out", "result.json")
				if err := os.WriteFile(elsewhere, []byte(resultFile), 0o600); err != nil {
					return err
				}

				return os.Symlink(elsewhere, workPath(root, "result.json"))
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the result file is over its cap",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), make([]byte, audit.MaxResult+1), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the result file is no JSON",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(`{"findings":[`), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			// Well-formed JSON, nested one level past the audit's limit: the
			// walk refuses it rather than growing its stack with it.
			name: "the result file is nested deeper than the audit's limit",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(nestedArrays(maxJSONDepth+1)), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the result file is nested to the audit's limit",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(nestedArrays(maxJSONDepth)), 0o600)
			},
			wantLaunched: true,
			wantResult:   nestedArrays(maxJSONDepth),
		},
		{
			name: "the result file holds the step's token as JSON escapes",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(`{"note":"`+jsonEscaped(testToken)+`"}`), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the result file holds the step's canary as JSON escapes",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(`{"note":"`+jsonEscaped(testCanary)+`"}`), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the result file holds the step's token",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(`{"note":"`+testToken+`"}`), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the result file holds the step's canary",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(`{"note":"`+testCanary+`"}`), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			// A duplicate key is legal JSON, and a document decoded into a map
			// keeps its last value alone: the first one's escapes would never
			// reach the search.
			name: "the result file holds the step's token as JSON escapes under a duplicate key",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(`{"note":"`+jsonEscaped(testToken)+`","note":"the value a map keeps"}`), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the result file holds the step's canary as JSON escapes under a duplicate key",
			write: func(root string) error {
				return os.WriteFile(workPath(root, "result.json"), []byte(`{"note":"`+jsonEscaped(testCanary)+`","note":"the value a map keeps"}`), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "a result file is already there before the run",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, workPath(root, "result.json"), resultFile, 0o600)
			},
			wantRefused: true,
		},
		{
			name: "the work area holds an entry of its own",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, workPath(root, "leftover.txt"), "the preflight's model wrote this\n", 0o600)
			},
			wantRefused: true,
		},
		{
			name: "the work area's tmp is not empty",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, workPath(root, "tmp", "leftover"), "the preflight left this behind\n", 0o600)
			},
			wantRefused: true,
		},
		{
			// The CLI starts in the working directory the launch hands it, and the
			// placeholders it leaves there are outside the area the audit checks:
			// the run passes and its result file is taken as the other cases' is
			// (issue #615). They are left where the handed config points, never at
			// a path this test invents: a run that started elsewhere would leave
			// them where nothing looks, and the case would pass for the wrong
			// reason (issue #615, the reviewer's N2).
			name:           "the run left the CLI's placeholders in the working directory it was handed",
			writeInWorkDir: writeCLIPlaceholders,
			write:          func(root string) error { return os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600) },
			wantLaunched:   true,
			wantResult:     resultFile,
		},
		{
			// The preflight's session leaves the placeholders the scrub creates,
			// and a CLAUDE.md, in the directory it starts in - the preflight's own
			// - and the audit's session starts in another one: what the preflight
			// left is outside everything the audit checks, and the run passes with
			// its result file taken (issues #615, #614).
			name: "the preflight left the CLI's placeholders and a CLAUDE.md in its own working directory",
			before: func(t *testing.T, root string) {
				t.Helper()
				leaveCLIPlaceholders(t, pathOf(root, cwdPreflight))
			},
			write:        func(root string) error { return os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600) },
			wantLaunched: true,
			wantResult:   resultFile,
		},
		{
			// The CLI loads a CLAUDE.md from the directory it starts in, so an
			// entry of the audit's working directory is what a preflight session
			// could plant there: the audit refuses it before its run, as it
			// refuses a work area that is not empty (issues #615, #614).
			name: "the audit's working directory holds a CLAUDE.md",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(pathOf(root, cwdAudit), "CLAUDE.md"), "# planted by the preflight's session\n", 0o600)
			},
			wantRefused: true,
		},
		{
			// The audit's working directory is an entry of the layout like any
			// other: a symbolic link there is refused rather than followed, and it
			// is no longer the empty real directory the audit's run starts in
			// (issues #615, #614).
			name:        "the audit's working directory is a symbolic link",
			before:      linkElsewhere(cwdAudit),
			wantRefused: true,
		},
		{
			// The $GITHUB_ENV file decides what a later step of the job runs with,
			// and the audit's session runs Bash: the launcher digests the file
			// before the audit's run and after it, and a change is the run having
			// written to a file it must not touch. The result file is written, so
			// the change is the only reason this run fails (issue #614).
			name: "the run overwrote the $GITHUB_ENV file",
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				return os.WriteFile(testInputs(root).GitHubEnv, []byte("SOMETHING=else\nANTHROPIC_BASE_URL=https://api.example.invalid\n"), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		// The $GITHUB_ENV file is one of the runner's file commands, and it sits
		// in the runner's file-command directory beside $GITHUB_PATH,
		// $GITHUB_OUTPUT and $GITHUB_STATE. A step that writes any of them decides
		// what the job's later steps run with - a line appended to $GITHUB_PATH
		// changes the PATH of the next step, so a later step's tool is no longer
		// the one the job installed and checked - so the launcher digests that
		// directory, and every entry of it, across the audit's run (issues #615,
		// #614).
		{
			name: "the run appended a line to the $GITHUB_PATH file beside the $GITHUB_ENV file",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, fileCommandPath(root, "github-path"), "/usr/local/bin\n", 0o600)
			},
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				return appendFileCommandLine(fileCommandPath(root, "github-path"), "/tmp/planted\n")
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the run created a file in the $GITHUB_ENV file's directory",
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				return os.WriteFile(fileCommandPath(root, "github-output"), []byte("result=planted\n"), 0o600)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the run removed a file in the $GITHUB_ENV file's directory",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, fileCommandPath(root, "github-path"), "/usr/local/bin\n", 0o600)
			},
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				return os.Remove(fileCommandPath(root, "github-path"))
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			// The link points at a file outside the digested directory whose
			// bytes are the ones the entry held, so a digest that follows the link
			// and hashes what it reads sees no change: only the entry's own type
			// tells the two apart, and the launcher takes it as it is (Lstat, no
			// link followed) and fails the run (issues #615, #614).
			name: "the run replaced a file in the $GITHUB_ENV file's directory with a symbolic link",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, fileCommandPath(root, "github-path"), "/usr/local/bin\n", 0o600)
			},
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				target := filepath.Join(root, "github-path-outside")
				if err := os.WriteFile(target, []byte("/usr/local/bin\n"), 0o600); err != nil {
					return err
				}

				if err := os.Remove(fileCommandPath(root, "github-path")); err != nil {
					return err
				}

				return os.Symlink(target, fileCommandPath(root, "github-path"))
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			// A link is digested by the text of its own target, never by what it
			// points at, so one retargeted during the run is a change although both
			// targets hold the same bytes (issues #615, #614).
			name: "the run retargeted a symbolic link in the $GITHUB_ENV file's directory",
			before: func(t *testing.T, root string) {
				t.Helper()

				first := filepath.Join(root, "github-path-first")
				writeFile(t, first, "/usr/local/bin\n", 0o600)

				if err := os.Symlink(first, fileCommandPath(root, "github-path")); err != nil {
					t.Fatalf("link %s: %v", fileCommandPath(root, "github-path"), err)
				}
			},
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				second := filepath.Join(root, "github-path-second")
				if err := os.WriteFile(second, []byte("/usr/local/bin\n"), 0o600); err != nil {
					return err
				}

				if err := os.Remove(fileCommandPath(root, "github-path")); err != nil {
					return err
				}

				return os.Symlink(second, fileCommandPath(root, "github-path"))
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			// A directory is an entry of the directory like any other: its name and
			// its type are digested, so one the run creates is a change.
			name: "the run created a subdirectory in the $GITHUB_ENV file's directory",
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				return os.Mkdir(fileCommandPath(root, "planted"), 0o700)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		// The directory the launcher digests is capped, in entries and in bytes
		// (issues #615, #614). The caps are the launcher's to pick: this case
		// assumes a cap of 64 entries and 4 MiB of content, and holds the launcher
		// to refusing, before its run, a directory well past them - 1024 entries,
		// and 32 MiB over 8 files - rather than digesting it whole. Both
		// directories are built before the run, so a launcher that digests them
		// anyway meets no change and would take the result file.
		{
			name: "the $GITHUB_ENV file's directory holds more entries than its cap",
			before: func(t *testing.T, root string) {
				t.Helper()

				for i := range 1024 {
					writeFile(t, fileCommandPath(root, fmt.Sprintf("github-file-%d", i)), "", 0o600)
				}
			},
			write:        func(root string) error { return os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600) },
			wantLaunched: false,
			wantRefused:  true,
		},
		{
			name: "the $GITHUB_ENV file's directory holds more bytes than its cap",
			before: func(t *testing.T, root string) {
				t.Helper()

				for i := range 8 {
					writeFile(t, fileCommandPath(root, fmt.Sprintf("github-file-%d", i)), strings.Repeat("x", 4<<20), 0o600)
				}
			},
			write:        func(root string) error { return os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600) },
			wantLaunched: false,
			wantRefused:  true,
		},
		// The step summary lies in the runner's file-command directory beside
		// $GITHUB_ENV, and it is digested with every other entry of it: no entry is
		// left out (issues #615, #614). The exclusion this test used to grant it was
		// a hole - a session that can write the directory could swap the summary for
		// a link to a readable file outside the sandbox, and the runner would post
		// that file as the job's public summary - and it buys nothing: the step's own
		// summaryWriter (ci/auditrun/main.go) creates the summary before the audit's
		// run, and the run's one line is written to it after the steps return, so
		// nothing legitimate writes it while the digests are taken.
		{
			// What a real run finds: the summary the step laid down before the audit,
			// empty, and the run's own writes leaving it as it was.
			name: "the step summary the step laid down is left untouched by the run",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, testInputs(root).StepSummary, "", 0o600)
			},
			write:        func(root string) error { return os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600) },
			wantLaunched: true,
			wantResult:   resultFile,
		},
		{
			name: "the run appended to the step summary laid down before it",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, testInputs(root).StepSummary, "", 0o600)
			},
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				return appendStepSummary(testInputs(root).StepSummary, "the audit ran\n")
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			// The hole the exclusion left: the summary's own entry swapped for a link
			// to a file outside the directory, whose bytes the job would publish.
			name: "the run replaced the step summary with a symbolic link to a file outside the directory",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, testInputs(root).StepSummary, "", 0o600)
			},
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				target := filepath.Join(root, "summary-outside-the-sandbox.md")
				if err := os.WriteFile(target, []byte("# what the session wants the job to publish\n"), 0o600); err != nil {
					return err
				}

				if err := os.Remove(testInputs(root).StepSummary); err != nil {
					return err
				}

				return os.Symlink(target, testInputs(root).StepSummary)
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			name: "the run created its step summary in the $GITHUB_ENV file's directory",
			write: func(root string) error {
				if err := os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600); err != nil {
					return err
				}

				return appendStepSummary(testInputs(root).StepSummary, "the audit ran\n")
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		// A run that failed says nothing about the files it wrote on its way
		// there: the change to the file-command directory is reported whatever the
		// run did, so a failed launch that also wrote a file command is no quieter
		// than one that did not (issues #615, #614).
		{
			name: "the run failed and also overwrote the $GITHUB_ENV file",
			write: func(root string) error {
				if err := os.WriteFile(testInputs(root).GitHubEnv, []byte("SOMETHING=else\nANTHROPIC_BASE_URL=https://api.example.invalid\n"), 0o600); err != nil {
					return err
				}

				return errors.New("the CLI's run failed")
			},
			wantLaunched: true,
			wantRefused:  true,
		},
		{
			// The other half: a run that leaves the file as it was changes
			// nothing, and its result file is taken (issue #614).
			name:         "the run left the $GITHUB_ENV file as it was",
			write:        func(root string) error { return os.WriteFile(workPath(root, "result.json"), []byte(resultFile), 0o600) },
			wantLaunched: true,
			wantResult:   resultFile,
		},
		// The other half of the placeholders decision above: no placeholder is
		// tolerated in the work area itself, where the launcher's check would meet
		// one of the prefixes the CLI's own working directory holds (issue #615).
		{
			name: "the work area holds the CLI's .env",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, workPath(root, ".env"), "", 0o600)
			},
			wantRefused: true,
		},
		{
			name: "the work area holds the CLI's package.json",
			before: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, workPath(root, "package.json"), "", 0o600)
			},
			wantRefused: true,
		},
		{
			name: "the work area holds the CLI's node_modules/.bin",
			before: func(t *testing.T, root string) {
				t.Helper()
				makeDir(t, workPath(root, "node_modules", ".bin"))
			},
			wantRefused: true,
		},
		{
			name: "the work area holds the CLI's .claude/agents",
			before: func(t *testing.T, root string) {
				t.Helper()
				makeDir(t, workPath(root, ".claude", "agents"))
			},
			wantRefused: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, in := preflightTree(t)

			if tc.before != nil {
				tc.before(t, root)
			}

			launch := &launchFake{write: tc.write}

			code, out, result := runAuditStep(t, root, in, launch.launch)

			if launch.called != tc.wantLaunched {
				t.Errorf("the CLI's run %s, want it %s: the work area is checked before the run, so a result file that is already there never counts as the run's", ran(launch.called), ran(tc.wantLaunched))
			}

			// The audit's session starts in the audit's own working directory, a
			// sibling of the work area and of the preflight's directory: the
			// placeholders the CLI leaves there are outside the checked area, and
			// nothing a preflight session leaves in its own directory reaches this
			// one (issues #615, #614).
			if want := pathOf(root, cwdAudit); launch.called && launch.workDir != want {
				t.Errorf("the audit's session started in %q, want the audit's own working directory %q", launch.workDir, want)
			}

			// The run hands the publish job the bytes it read and checked, never
			// the path (issue #277, round D finding 4): a run that passed hands on
			// the file's own bytes, and a run that failed hands on nothing.
			if string(result) != tc.wantResult {
				t.Errorf("the run handed on %q, want %q: the bytes are the audit's, and a run that failed hands on none", result, tc.wantResult)
			}

			if tc.wantRefused {
				if code != audit.ExitFailed {
					t.Errorf("the run's exit code is %d, want %d: the audit failed", code, audit.ExitFailed)
				}

				if !strings.Contains(out, auditFailedSentence) {
					t.Errorf("the run printed %q, want %q", out, auditFailedSentence)
				}
			} else {
				if code != audit.ExitOK {
					t.Errorf("the run's exit code is %d, want %d: the run's result file is the step's", code, audit.ExitOK)
				}

				if !strings.Contains(out, "finished") {
					t.Errorf("the run printed %q, want %q", out, "finished")
				}
			}

			for _, secret := range []string{testToken, testCanary} {
				if strings.Contains(out, secret) {
					t.Errorf("the run printed %q, which holds %q: a failure is printed as its constant class alone (ADR 0040)", out, secret)
				}
			}
		})
	}
}

// leaveCLIPlaceholders lays what one CLI session leaves in the working directory
// it starts in: the placeholders Claude Code 2.1.332 creates there under the
// subprocess environment scrub, and a CLAUDE.md, which the CLI loads from that
// directory, so a session that writes one changes what a session starting there
// reads (issues #615, #614).
func leaveCLIPlaceholders(t *testing.T, dir string) {
	t.Helper()

	if err := writeCLIPlaceholders(dir); err != nil {
		t.Fatalf("leave the CLI's placeholders in %s: %v", dir, err)
	}

	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "# planted by a CLI session\n", 0o600)
}

// TestAuditNamesAnOversizedResultOfItsOwn pins the other half of finding 4 of
// round D (issue #277): a result file over the cap is a failure of its own, and
// not the one of a run that wrote no result file, so the runbook - and PR 5's
// publication - can tell the two apart.
func TestAuditNamesAnOversizedResultOfItsOwn(t *testing.T) {
	t.Parallel()

	// auditOnce runs the audit step alone under a fresh tree, its run leaving
	// what write leaves in the work area, and returns its failure.
	auditOnce := func(t *testing.T, write func(root string) error) error {
		t.Helper()

		root, in := preflightTree(t)

		_, err := audit.Audit(t.Context(), root, in, (&launchFake{write: write}).launch)
		if err == nil {
			t.Fatal("Audit() = nil, want a failure: the step accepts one result file, and this run left none it takes")
		}

		return err
	}

	missing := auditOnce(t, nil)

	oversized := auditOnce(t, func(root string) error {
		return os.WriteFile(workPath(root, "result.json"), make([]byte, audit.MaxResult+1), 0o600)
	})

	if oversized.Error() == missing.Error() {
		t.Errorf("Audit() = %q for a result file over %d bytes, want a failure of its own: %q is the failure of a run that wrote no result file", oversized, audit.MaxResult, missing)
	}

	if want := strconv.FormatInt(audit.MaxResult, 10); !strings.Contains(oversized.Error(), want) {
		t.Errorf("Audit() = %q for a result file over its cap, want it to name the cap, %s bytes", oversized, want)
	}
}

// TestAuditReportsTheFileCommandChangeOverAFailedRun pins what a launch that
// failed and also wrote a file command reports (issues #615, #614): the change
// to the runner's file-command directory is the failure the audit reports, not
// the launch's own, since that directory is what the job's later steps run with.
// A run that changed nothing but failed is the launch's failure, so the two are
// told apart by the step's error, whose text the step's line never prints.
func TestAuditReportsTheFileCommandChangeOverAFailedRun(t *testing.T) {
	t.Parallel()

	// failure runs the audit step once under a fresh tree, its launch leaving
	// what write leaves before it returns, the run failing when failed is set,
	// and returns the step's error.
	failure := func(t *testing.T, write func(root string) error, failed bool) error {
		t.Helper()

		root, in := preflightTree(t)

		launch := func(_ context.Context, _ string, _ claudecode.Config, _ claudecode.Prompt) error {
			if write != nil {
				if err := write(root); err != nil {
					t.Fatalf("the run's leavings: %v", err)
				}
			}

			if failed {
				return errors.New("the CLI's run failed")
			}

			return nil
		}

		_, err := audit.Audit(t.Context(), root, in, launch)
		if err == nil {
			t.Fatal("Audit() = nil, want a failure: the run left a file-command change, or failed, or both")
		}

		return err
	}

	change := func(root string) error {
		return os.WriteFile(testInputs(root).GitHubEnv, []byte("SOMETHING=else\nANTHROPIC_BASE_URL=https://api.example.invalid\n"), 0o600)
	}

	changed := failure(t, change, false)
	both := failure(t, change, true)
	failed := failure(t, nil, true)

	if both.Error() != changed.Error() {
		t.Errorf("Audit() = %q for a run that failed and changed the $GITHUB_ENV file, want %q, the failure of a run that changed it: the file-command change is reported whatever the run did (issues #615, #614)",
			both, changed)
	}

	if both.Error() == failed.Error() {
		t.Errorf("Audit() = %q for a run that failed and changed the $GITHUB_ENV file, and %q for one that only failed: the change is a failure of its own, never the launch's (issues #615, #614)",
			both, failed)
	}
}

// ran names a step a case did or did not expect.
func ran(called bool) string {
	if called {
		return "ran"
	}

	return "did not run"
}
