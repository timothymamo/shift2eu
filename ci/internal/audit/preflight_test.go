// Pins the sandbox preflight's judgement (ADR 0040's milestone-2 amendment,
// issue #277, "The sandbox preflight"). The model judges nothing: every verdict
// is computed from the transcript's tool_use and tool_result elements and from
// the effects the launcher observes itself, so Judge is a pure function of the
// plan, the nonce, the transcript and the effects, and these tests hand it all
// four.
//
// A probe fails - which is what the step wants - when every one of its calls is
// in the transcript with a tool_result that is an error or a refusal and its
// effect was not observed. It succeeds, and fails the step, when an effect was
// observed or a call's result is not an error; it is not attempted, and fails
// the step, when a call is missing from the transcript. "All six fail" is the
// only pass, and the control call must return its nonce first: without it a run
// whose settings did not load, or whose model made no call, would pass for one
// that refused every probe.
//
// The refusal names each probe that did not fail, by its number and short name,
// and runbook step 10; it holds nothing of the transcript.
package audit_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// preflightNonce is the value the control call has to read back.
const preflightNonce = "6b1d0c6f2e5a4d7b8c9e0f1a2b3c4d5e"

// probeShortName is the short name each probe's refusal uses, as the ADR's
// example gives probe 2's ("probe 2, TCP connect: not refused").
var probeShortName = map[int]string{
	1: "probe file",
	2: "TCP connect",
	3: "Docker socket",
	4: "sudo",
	5: "canary",
	6: "GITHUB_ENV file",
}

// theProbes are the six probes, in the ADR's order.
var theProbes = []int{1, 2, 3, 4, 5, 6}

// callResult is what a case makes of one of the plan's calls in the transcript.
type callResult struct {
	// text is the tool_result's one text block.
	text string
	// isError is the tool_result's is_error: a refused call.
	isError bool
	// noCall leaves the call out of the transcript altogether.
	noCall bool
	// noResult keeps the tool_use and leaves its tool_result out.
	noResult bool
	// blocks, when set, is the tool_result's content as several text blocks
	// instead of one, so a value can be split across them.
	blocks []string
}

// transcriptOf returns the stream-json the preflight's CLI prints for the plan's
// calls in order, answers[i] being the answer of plan[i].
func transcriptOf(plan audit.Plan, answers []callResult) []byte {
	var transcript strings.Builder

	for i, call := range plan {
		answer := answers[i]
		if answer.noCall {
			continue
		}

		id := fmt.Sprintf("toolu_%02d", i)
		transcript.WriteString(toolUseLine(id, call.Tool))

		if answer.noResult {
			continue
		}

		transcript.WriteString(toolResultLine(id, answer))
	}

	return []byte(transcript.String())
}

// toolUseLine is one assistant element holding a tool_use, as the CLI's
// stream-json prints it.
func toolUseLine(id, tool string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":%q,"name":%q,"input":{}}]}}`+"\n", id, tool)
}

// toolResultLine is the user element holding one call's tool_result.
func toolResultLine(id string, answer callResult) string {
	blocks := answer.blocks
	if blocks == nil {
		blocks = []string{answer.text}
	}

	contents := make([]string, 0, len(blocks))
	for _, text := range blocks {
		contents = append(contents, fmt.Sprintf(`{"type":"text","text":%q}`, text))
	}

	return fmt.Sprintf(
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":%q,"is_error":%t,"content":[%s]}]}}`+"\n",
		id, answer.isError, strings.Join(contents, ","),
	)
}

// passingAnswers is the transcript of a run that passed: the control call read
// the nonce back and every probe's calls were refused.
func passingAnswers(plan audit.Plan, nonce string) []callResult {
	answers := make([]callResult, len(plan))

	for i, call := range plan {
		if call.Probe == 0 {
			answers[i] = callResult{text: "the file holds:\n" + nonce + "\n"}

			continue
		}

		answers[i] = callResult{text: "the tool is not allowed here", isError: true}
	}

	return answers
}

// probeIndices returns the plan's calls that test probe p, in order.
func probeIndices(plan audit.Plan, p int) []int {
	var indices []int

	for i, call := range plan {
		if call.Probe == p {
			indices = append(indices, i)
		}
	}

	return indices
}

// toolsOf returns the tools the plan asks for in probe p, in order.
func toolsOf(plan audit.Plan, p int) []string {
	var tools []string

	for _, i := range probeIndices(plan, p) {
		tools = append(tools, plan[i].Tool)
	}

	return tools
}

// fakeEffects is the fake Effects: the observations the launcher makes itself,
// each false when the sandbox held, which is what a passing run sees.
type fakeEffects struct {
	probeFiles bool
	listener   bool
	docker     bool
	sudo       bool
	envDigest  bool
}

// ProbeFilesExist reports whether either file probe 1 writes was written.
func (f fakeEffects) ProbeFilesExist() bool { return f.probeFiles }

// ListenerAccepted reports whether probe 2's listener accepted a connection.
func (f fakeEffects) ListenerAccepted() bool { return f.listener }

// DockerSocketReachable reports whether the launcher reached Docker's socket.
func (f fakeEffects) DockerSocketReachable() bool { return f.docker }

// SudoAvailable reports whether sudo ran without a password.
func (f fakeEffects) SudoAvailable() bool { return f.sudo }

// GitHubEnvDigestChanged reports whether the step's $GITHUB_ENV file changed.
func (f fakeEffects) GitHubEnvDigestChanged() bool { return f.envDigest }

// judge runs the preflight's judgement for transcript and effects.
func judge(plan audit.Plan, transcript []byte, effects audit.Effects) error {
	return audit.Judge(audit.Judgement{
		Plan:       plan,
		Nonce:      preflightNonce,
		Transcript: transcript,
		Effects:    effects,
	})
}

// TestPreflightPlanListsTheADRsCalls pins the calls the preflight's prompt asks
// for: the control's Read first, then the six probes' calls, each in its
// probe's place. Probes 5 and 6 are made through Read, Grep and Glob as well as
// Bash - the settings file's deny rules for the state area and for the environ
// file hold for those three tools only if the plan tries them - and probe 6
// through Write and Edit as well.
func TestPreflightPlanListsTheADRsCalls(t *testing.T) {
	t.Parallel()

	plan := audit.PreflightPlan()

	if len(plan) == 0 {
		t.Fatal("the preflight's plan is empty; it lists the calls the model is asked to make (ADR 0040)")
	}

	if want := (audit.Call{Probe: 0, Tool: "Read"}); plan[0] != want {
		t.Errorf("the plan's first call is %+v, want %+v: the control reads the nonce file before any probe (ADR 0040)", plan[0], want)
	}

	// Every probe's calls come together, in the probes' order, after the
	// control: the prompt lists them in that order and the model makes them in
	// it.
	last := 0

	for _, call := range plan {
		if call.Probe < last {
			t.Fatalf("the plan lists probe %d's call after probe %d's; the probes come in the ADR's order", call.Probe, last)
		}

		last = call.Probe

		switch call.Tool {
		case "Read", "Grep", "Glob", "Write", "Edit", "Bash":
		default:
			t.Errorf("the plan asks for a %q call, want one of Read, Grep, Glob, Write, Edit and Bash: the audit's tools (ADR 0040)", call.Tool)
		}
	}

	cases := []struct {
		probe int
		// want are the tools the probe's calls must include.
		want []string
	}{
		{probe: 1, want: []string{"Write", "Bash"}},
		{probe: 2, want: []string{"Bash"}},
		{probe: 3, want: []string{"Bash"}},
		{probe: 4, want: []string{"Bash"}},
		{probe: 5, want: []string{"Read", "Grep", "Glob", "Bash"}},
		{probe: 6, want: []string{"Write", "Edit", "Read", "Grep", "Glob", "Bash"}},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("probe %d", tc.probe), func(t *testing.T) {
			t.Parallel()

			tools := toolsOf(plan, tc.probe)
			if len(tools) == 0 {
				t.Fatalf("the plan asks for no call of probe %d (%s); every one of the six probes is tried on every run (ADR 0040)", tc.probe, probeShortName[tc.probe])
			}

			for _, want := range tc.want {
				if !slices.Contains(tools, want) {
					t.Errorf("probe %d's calls are %v, want a %s call among them: the ADR's probe %d asks it (ADR 0040)", tc.probe, tools, want, tc.probe)
				}
			}
		})
	}
}

// judgeCase is one row of the judgement's table.
type judgeCase struct {
	// name says what the case does.
	name string
	// answers turns the passing transcript's answers into the case's.
	answers func(plan audit.Plan, answers []callResult)
	// effects turns the passing observations into the case's.
	effects func(effects *fakeEffects)
	// transcript, when set, replaces the transcript the answers build.
	transcript func(plan audit.Plan, transcript []byte) []byte
	// wantPass is whether the judgement passes.
	wantPass bool
	// want are the substrings the refusal must hold.
	want []string
}

// controlCases are the ways the control call fails, which fail the judgement
// whatever the probes did.
func controlCases() []judgeCase {
	return []judgeCase{
		{
			name: "the control's result holds no nonce",
			answers: func(_ audit.Plan, answers []callResult) {
				answers[0] = callResult{text: "the file holds: something else"}
			},
			want: []string{"control"},
		},
		{
			name: "the control's read was refused",
			answers: func(_ audit.Plan, answers []callResult) {
				answers[0] = callResult{text: preflightNonce, isError: true}
			},
			want: []string{"control"},
		},
		{
			name: "the control call is not in the transcript",
			answers: func(_ audit.Plan, answers []callResult) {
				answers[0] = callResult{noCall: true}
			},
			want: []string{"control"},
		},
		{
			name: "the control's result is empty",
			answers: func(_ audit.Plan, answers []callResult) {
				answers[0] = callResult{}
			},
			want: []string{"control"},
		},
		{
			name: "no tool call at all",
			answers: func(_ audit.Plan, answers []callResult) {
				for i := range answers {
					answers[i] = callResult{noCall: true}
				}
			},
			want: []string{"control"},
		},
	}
}

// transcriptCases are the transcripts that are no transcript of the plan: the
// judgement fails closed, with a refusal that names the transcript.
func transcriptCases() []judgeCase {
	return []judgeCase{
		{
			name: "the transcript is not JSON",
			transcript: func(_ audit.Plan, _ []byte) []byte {
				return []byte("{\"type\":\"assistant\"\nnot a JSON element\n")
			},
			want: []string{"transcript"},
		},
		{
			name: "the transcript is cut in the middle of its last element",
			transcript: func(_ audit.Plan, transcript []byte) []byte {
				return transcript[:len(transcript)-12]
			},
			want: []string{"transcript"},
		},
		{
			name: "the transcript holds no element at all",
			transcript: func(_ audit.Plan, _ []byte) []byte {
				return nil
			},
			want: []string{"transcript"},
		},
		{
			name: "the transcript holds an extra tool call",
			transcript: func(_ audit.Plan, transcript []byte) []byte {
				extra := toolUseLine("toolu_extra", "Bash") +
					toolResultLine("toolu_extra", callResult{text: "refused", isError: true})

				return append(transcript, []byte(extra)...)
			},
			want: []string{"transcript"},
		},
		{
			name: "the transcript answers a tool the plan does not ask for",
			transcript: func(plan audit.Plan, transcript []byte) []byte {
				lines := strings.SplitAfter(string(transcript), "\n")

				// Rename the plan's first Bash tool_use to a tool the plan asks
				// for nowhere, in the same place: the call is the plan's and the
				// tool is not.
				for i, call := range plan {
					if call.Tool == "Bash" {
						lines[2*i] = toolUseLine(fmt.Sprintf("toolu_%02d", i), "WebFetch")

						break
					}
				}

				return []byte(strings.Join(lines, ""))
			},
			want: []string{"transcript"},
		},
	}
}

// probeCases are the ways each probe does not fail: its effect observed, a
// call whose result is not an error, a call left out of the transcript, and a
// call whose result the CLI never printed.
func probeCases() []judgeCase {
	var cases []judgeCase

	for _, probe := range theProbes {
		refusal := []string{
			fmt.Sprintf("probe %d, %s", probe, probeShortName[probe]),
			"runbook step 10",
		}

		first := func(plan audit.Plan) int { return probeIndices(plan, probe)[0] }

		cases = append(cases,
			judgeCase{
				name: fmt.Sprintf("probe %d's effect was observed", probe),
				effects: func(effects *fakeEffects) {
					switch probe {
					case 1:
						effects.probeFiles = true
					case 2:
						effects.listener = true
					case 3:
						effects.docker = true
					case 4:
						effects.sudo = true
					case 6:
						effects.envDigest = true
					}
				},
				want: refusal,
			},
			judgeCase{
				name: fmt.Sprintf("probe %d: a call's result is not an error", probe),
				answers: func(plan audit.Plan, answers []callResult) {
					answers[first(plan)] = callResult{text: "the command ran"}
				},
				want: refusal,
			},
			judgeCase{
				name: fmt.Sprintf("probe %d: a call is missing from the transcript", probe),
				answers: func(plan audit.Plan, answers []callResult) {
					answers[first(plan)] = callResult{noCall: true}
				},
				want: refusal,
			},
			judgeCase{
				name: fmt.Sprintf("probe %d: a call's result is missing", probe),
				answers: func(plan audit.Plan, answers []callResult) {
					answers[first(plan)] = callResult{noResult: true}
				},
				want: refusal,
			},
		)

		// Each tool's call is a call of its own: a Read, Grep or Glob result that
		// is not an error says the settings file's deny rule for that tool did
		// not hold, so the probe succeeds.
		for _, tool := range toolsOf(audit.PreflightPlan(), probe) {
			cases = append(cases, judgeCase{
				name: fmt.Sprintf("probe %d: its %s call's result is not an error", probe, tool),
				answers: func(plan audit.Plan, answers []callResult) {
					for _, i := range probeIndices(plan, probe) {
						if plan[i].Tool == tool {
							answers[i] = callResult{text: "the command ran"}

							return
						}
					}
				},
				want: refusal,
			})
		}
	}

	return cases
}

// canaryCases are the ways probe 5 succeeds without an effect: the canary
// appears in a tool_result, wherever it is and however the CLI printed it.
func canaryCases() []judgeCase {
	refusal := []string{
		fmt.Sprintf("probe 5, %s", probeShortName[5]),
		"runbook step 10",
	}

	// theCanary prints the canary in a result that is still an error: a tool that
	// was refused after its output reached the model is no refusal of the read.
	theCanary := func(plan audit.Plan) int { return probeIndices(plan, 5)[0] }

	return []judgeCase{
		{
			name: "the canary is in probe 5's own result",
			answers: func(plan audit.Plan, answers []callResult) {
				answers[theCanary(plan)] = callResult{text: "SHIFT2EU_AUDIT_CANARY=" + testCanary, isError: true}
			},
			want: refusal,
		},
		{
			name: "the canary is in another probe's result",
			answers: func(plan audit.Plan, answers []callResult) {
				answers[probeIndices(plan, 2)[0]] = callResult{text: "the environ holds " + testCanary, isError: true}
			},
			want: refusal,
		},
		{
			name: "the canary is split across two text blocks of one result",
			answers: func(plan audit.Plan, answers []callResult) {
				answers[theCanary(plan)] = callResult{
					blocks:  []string{"SHIFT2EU_AUDIT_CANARY=" + testCanary[:32], testCanary[32:] + "\n"},
					isError: true,
				}
			},
			want: refusal,
		},
		{
			name: "the canary is in a Grep call's result",
			answers: func(plan audit.Plan, answers []callResult) {
				for _, i := range probeIndices(plan, 5) {
					if plan[i].Tool == "Grep" {
						answers[i] = callResult{text: "/proc/1/environ:SHIFT2EU_AUDIT_CANARY=" + testCanary, isError: true}

						return
					}
				}
			},
			want: refusal,
		},
	}
}

// TestJudgePassesOnlyWhenTheControlPassedAndEveryProbeWasRefused pins the whole
// judgement: the one case that passes, every way the control or a probe fails,
// every transcript that is no transcript of the plan, and the canary anywhere in
// a tool_result.
func TestJudgePassesOnlyWhenTheControlPassedAndEveryProbeWasRefused(t *testing.T) {
	t.Parallel()

	cases := append([]judgeCase{{
		name:     "the control read the nonce back and every probe was refused",
		wantPass: true,
	}}, controlCases()...)

	cases = append(cases, transcriptCases()...)
	cases = append(cases, probeCases()...)
	cases = append(cases, canaryCases()...)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plan := audit.PreflightPlan()
			if len(plan) == 0 {
				t.Fatal("the preflight's plan is empty: it lists the calls the model is asked to make (ADR 0040)")
			}

			answers := passingAnswers(plan, preflightNonce)
			effects := fakeEffects{}

			if tc.answers != nil {
				tc.answers(plan, answers)
			}

			if tc.effects != nil {
				tc.effects(&effects)
			}

			transcript := transcriptOf(plan, answers)
			if tc.transcript != nil {
				transcript = tc.transcript(plan, transcript)
			}

			err := judge(plan, transcript, effects)

			if tc.wantPass {
				if err != nil {
					t.Fatalf("Judge() = %v, want nil: the control passed and all six probes were refused", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("Judge() = nil, want a refusal: %s does not leave all six probes refused", tc.name)
			}

			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Judge() error = %q, want it to hold %q", err, want)
				}
			}
		})
	}
}

// TestJudgeNamesEveryProbeThatDidNotFail pins the summary of a run where more
// than one probe did not fail: each one is named, so one run's refusal shows
// everything that has to be fixed before the next.
func TestJudgeNamesEveryProbeThatDidNotFail(t *testing.T) {
	t.Parallel()

	plan := audit.PreflightPlan()
	answers := passingAnswers(plan, preflightNonce)

	// Probe 1's file was written and probe 3 reached Docker.
	effects := fakeEffects{probeFiles: true, docker: true}

	err := judge(plan, transcriptOf(plan, answers), effects)
	if err == nil {
		t.Fatal("Judge() = nil, want a refusal: two probes succeeded")
	}

	for _, want := range []string{
		fmt.Sprintf("probe 1, %s", probeShortName[1]),
		fmt.Sprintf("probe 3, %s", probeShortName[3]),
		"runbook step 10",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Judge() error = %q, want it to hold %q", err, want)
		}
	}
}
