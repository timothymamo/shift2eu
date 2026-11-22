// Pins the two steps that run the model (ADR 0040's milestone-2 amendment,
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

// observerFake is the fake Observer: it records the context it was given and
// what it was told, and answers with its effects.
type observerFake struct {
	called         bool
	ctxErr         error
	ctxHasDeadline bool
	accepted       bool
	effects        audit.Effects
}

// observe is the Observer the preflight takes.
func (o *observerFake) observe(ctx context.Context, _, _ string, _ [32]byte, accepted bool) audit.Effects {
	o.called = true
	o.ctxErr = ctx.Err()
	_, o.ctxHasDeadline = ctx.Deadline()
	o.accepted = accepted

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

		return transcriptOf(thePlan, passingAnswers(thePlan, nonce))
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
// leaves: no verdict - there is no transcript to judge - and one line, the
// preflight's constant class, in which nothing of the run's own error reaches the
// log, the token and the canary included.
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

		return append(transcriptOf(thePlan, passingAnswers(thePlan, nonce)), []byte(telling)...)
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

// launchFake is the fake launch of the audit's one CLI run: it records that it
// ran, and writes what the case asks for into the work area.
type launchFake struct {
	called bool
	// write, when set, is what the run leaves in the work area.
	write func(root string) error
}

// launch is the Launch the audit takes.
func (l *launchFake) launch(_ context.Context, root string, _ claudecode.Config, _ claudecode.Prompt) error {
	l.called = true

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

// workPath is a path of the audit's work area under root.
func workPath(root string, parts ...string) string {
	return filepath.Join(append([]string{root, "srv", "audit", "work"}, parts...)...)
}

// resultFile is the result the audit's model writes.
const resultFile = `{"findings":[]}`

// pinnedResult is a result that quotes a SHA-256 pin: 64 lower-case hex
// characters the judgement's 64-hex rule would refuse, and a finding here.
var pinnedResult = `{"pin":"` + strings.Repeat("0f", 32) + `"}`

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
// transcript is not applied here.
func TestAuditTakesOnlyAResultFileTheRunWrote(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// before, when set, lays something in the work area before the run.
		before func(t *testing.T, root string)
		// write, when set, is what the run leaves in the work area.
		write func(root string) error
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

// ran names a step a case did or did not expect.
func ran(called bool) string {
	if called {
		return "ran"
	}

	return "did not run"
}
