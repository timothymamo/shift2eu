package audit

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Effects are what the launcher observes itself after the preflight's run,
// each true when a probe reached its target.
type Effects interface {
	// ProbeFilesExist reports whether either file probe 1 writes exists.
	ProbeFilesExist() bool
	// ListenerAccepted reports whether probe 2's listener accepted a
	// connection.
	ListenerAccepted() bool
	// DockerSocketReachable reports whether the launcher's own connect to
	// Docker's socket succeeded.
	DockerSocketReachable() bool
	// SudoAvailable reports whether the launcher's own `sudo -n true` ran.
	SudoAvailable() bool
	// GitHubEnvDigestChanged reports whether the $GITHUB_ENV file's sha256
	// changed.
	GitHubEnvDigestChanged() bool
}

// Judgement is what the preflight's verdict is computed from: the plan the
// prompt listed, the control's nonce, the CLI's stream-json transcript and
// the observed effects. Canary, when set, is the step's canary, looked for in
// every byte of the transcript; a run of 64 lower-case hex characters in a
// tool_result counts as the canary whether or not it is set.
type Judgement struct {
	Plan       Plan
	Nonce      string
	Canary     string
	Transcript []byte
	Effects    Effects
}

// Verdict is the preflight's refusal. Its text is made of constants alone -
// the control, the probes' numbers and short names, what each did and runbook
// step 10 - and holds nothing of the transcript, so a run prints it as its
// summary.
type Verdict struct {
	problems []string
}

// Error is the verdict's one line.
func (v *Verdict) Error() string {
	return "preflight failed: " + strings.Join(v.problems, "; ") + "; see runbook step 10"
}

// canaryLength is the length of the canary's hex: 32 bytes.
const canaryLength = 64

// The reasons a verdict gives.
const (
	reasonNotMade     = "not attempted"
	reasonNoResult    = "no result"
	reasonNotRefused  = "not refused"
	reasonEffect      = "its effect was observed"
	reasonCanarySeen  = "the canary was seen"
	controlLabel      = "the control call"
	reasonNoNonce     = "its result holds no nonce"
	reasonRefused     = "it was refused"
	transcriptNoCalls = "the transcript holds no element: the control call was not made"
)

// Judge computes the preflight's verdict: nil when the control's Read
// returned the nonce and every probe failed - each of its calls in the
// transcript with a tool_result that is an error, and its effect not
// observed. Anything else is a *Verdict naming the control and each probe
// that did not fail. A transcript that is no run of the plan - not JSON, a
// call the plan does not list, a result of no call - fails closed.
func Judge(j Judgement) error {
	if len(j.Plan) == 0 || j.Effects == nil || j.Nonce == "" {
		return &Verdict{problems: []string{"the preflight has no plan, nonce or effects to judge"}}
	}

	calls, err := parseTranscript(j.Transcript)
	if err != nil {
		return err
	}

	tools := make([]string, len(calls))
	for i, c := range calls {
		tools[i] = c.tool
	}

	align := alignCalls(j.Plan, tools)
	if !align.valid() {
		return &Verdict{problems: []string{"the transcript is no run of the plan: it holds a tool call the plan does not list, or a call out of order"}}
	}

	found := newFindings()

	for i, call := range j.Plan {
		if align.canSkip(i) {
			found.add(call.Probe, reasonNotMade)
		}

		for k := range calls {
			if !align.canMatch(i, k) {
				continue
			}

			judgeCall(found, call, calls[k], j.Nonce)
		}
	}

	judgeEffects(found, j.Effects)

	for _, c := range calls {
		if c.answered && (hasHexRun(c.text, canaryLength) || (j.Canary != "" && strings.Contains(c.text, j.Canary))) {
			found.add(canaryProbe, reasonCanarySeen)
		}
	}

	if j.Canary != "" && bytes.Contains(j.Transcript, []byte(j.Canary)) {
		found.add(canaryProbe, reasonCanarySeen)
	}

	return found.verdict()
}

// canaryProbe is the probe the canary belongs to.
const canaryProbe = 5

// judgeCall judges one transcript call that may be plan call call.
func judgeCall(found *findings, call Call, made madeCall, nonce string) {
	switch {
	case !made.answered:
		found.add(call.Probe, reasonNoResult)
	case call.Probe == 0 && made.isError:
		found.add(0, reasonRefused)
	case call.Probe == 0 && !strings.Contains(made.text, nonce):
		found.add(0, reasonNoNonce)
	case call.Probe != 0 && !made.isError:
		found.add(call.Probe, reasonNotRefused)
	}
}

// judgeEffects adds every effect the launcher observed.
func judgeEffects(found *findings, effects Effects) {
	for probe, observed := range map[int]bool{
		1: effects.ProbeFilesExist(),
		2: effects.ListenerAccepted(),
		3: effects.DockerSocketReachable(),
		4: effects.SudoAvailable(),
		6: effects.GitHubEnvDigestChanged(),
	} {
		if observed {
			found.add(probe, reasonEffect)
		}
	}
}

// findings are the reasons, per probe, that it did not fail; probe 0 is the
// control.
type findings struct {
	reasons map[int][]string
}

// newFindings returns empty findings.
func newFindings() *findings {
	return &findings{reasons: map[int][]string{}}
}

// add records reason for probe once.
func (f *findings) add(probe int, reason string) {
	for _, r := range f.reasons[probe] {
		if r == reason {
			return
		}
	}

	f.reasons[probe] = append(f.reasons[probe], reason)
}

// verdict is nil for no findings, else the Verdict naming them in the
// probes' order.
func (f *findings) verdict() error {
	if len(f.reasons) == 0 {
		return nil
	}

	var problems []string

	if reasons, ok := f.reasons[0]; ok {
		problems = append(problems, controlLabel+": "+strings.Join(reasons, ", "))
	}

	for probe := 1; probe <= len(probeNames); probe++ {
		if reasons, ok := f.reasons[probe]; ok {
			problems = append(problems, probeLabel(probe)+": "+strings.Join(reasons, ", "))
		}
	}

	return &Verdict{problems: problems}
}

// madeCall is one tool call of the transcript: its tool and, when the CLI
// printed it, its result.
type madeCall struct {
	tool     string
	answered bool
	isError  bool
	text     string
}

// element is one line of the CLI's stream-json.
type element struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// block is one content block of an assistant or user element.
type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
	Text      string          `json:"text"`
}

// transcriptVerdict is a transcript the judgement cannot read.
func transcriptVerdict(why string) *Verdict {
	return &Verdict{problems: []string{"the transcript " + why}}
}

// parseTranscript returns the transcript's tool calls in order, each with its
// result.
func parseTranscript(transcript []byte) ([]madeCall, error) {
	var (
		calls    []madeCall
		elements int
	)

	index := map[string]int{}

	for line := range bytes.Lines(transcript) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var el element
		if err := json.Unmarshal(line, &el); err != nil {
			return nil, transcriptVerdict("is not stream-json")
		}

		elements++

		if el.Type != "assistant" && el.Type != "user" {
			continue
		}

		blocks, ok := contentBlocks(el.Message.Content)
		if !ok {
			return nil, transcriptVerdict("holds a message of an unknown shape")
		}

		for _, b := range blocks {
			var err error

			calls, err = takeBlock(calls, index, el.Type, b)
			if err != nil {
				return nil, err
			}
		}
	}

	if elements == 0 {
		return nil, &Verdict{problems: []string{transcriptNoCalls}}
	}

	return calls, nil
}

// takeBlock adds a tool_use of an assistant element, or a tool_result of a
// user element, to calls.
func takeBlock(calls []madeCall, index map[string]int, elementType string, b block) ([]madeCall, error) {
	switch {
	case elementType == "assistant" && b.Type == "tool_use":
		if _, seen := index[b.ID]; seen || b.ID == "" {
			return nil, transcriptVerdict("holds a tool call whose id is empty or repeated")
		}

		index[b.ID] = len(calls)

		return append(calls, madeCall{tool: b.Name}), nil
	case elementType == "user" && b.Type == "tool_result":
		k, seen := index[b.ToolUseID]
		if !seen || calls[k].answered {
			return nil, transcriptVerdict("holds a tool result of no call, or a second one")
		}

		text, ok := resultText(b.Content)
		if !ok {
			return nil, transcriptVerdict("holds a tool result of an unknown shape")
		}

		calls[k].answered = true
		calls[k].isError = b.IsError
		calls[k].text = text

		return calls, nil
	default:
		return calls, nil
	}
}

// contentBlocks decodes a message's content: a list of blocks, or a string,
// which holds none.
func contentBlocks(raw json.RawMessage) ([]block, bool) {
	if len(raw) == 0 || raw[0] == '"' {
		return nil, true
	}

	var blocks []block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, false
	}

	return blocks, true
}

// resultText returns a tool_result's text: its string content, or its text
// blocks joined with nothing between them, so a value split across blocks is
// whole again.
func resultText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}

	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return "", false
		}

		return text, true
	}

	var blocks []block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", false
	}

	var text strings.Builder
	for _, b := range blocks {
		text.WriteString(b.Text)
	}

	return text.String(), true
}

// hasHexRun reports whether text holds a run of at least n lower-case hex
// characters.
func hasHexRun(text string, n int) bool {
	run := 0

	for i := range len(text) {
		c := text[i]
		if ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') {
			run++
			if run >= n {
				return true
			}

			continue
		}

		run = 0
	}

	return false
}

// alignment holds which of the plan's calls each of the transcript's calls
// can be, the transcript's calls being the plan's in order with some left
// out: fwd[i][k] is whether the transcript's first k calls are a subsequence
// of the plan's first i, bwd[i][k] whether the transcript's calls from k on
// are one of the plan's from i on.
type alignment struct {
	plan  Plan
	tools []string
	fwd   [][]bool
	bwd   [][]bool
}

// alignCalls aligns the transcript's tools with the plan.
func alignCalls(plan Plan, tools []string) alignment {
	n, m := len(plan), len(tools)

	fwd := grid(n+1, m+1)
	bwd := grid(n+1, m+1)

	fwd[0][0] = true

	for i := 1; i <= n; i++ {
		for k := 0; k <= m; k++ {
			fwd[i][k] = fwd[i-1][k] || (k > 0 && plan[i-1].Tool == tools[k-1] && fwd[i-1][k-1])
		}
	}

	bwd[n][m] = true

	for i := n - 1; i >= 0; i-- {
		for k := m; k >= 0; k-- {
			bwd[i][k] = bwd[i+1][k] || (k < m && plan[i].Tool == tools[k] && bwd[i+1][k+1])
		}
	}

	return alignment{plan: plan, tools: tools, fwd: fwd, bwd: bwd}
}

// grid returns rows by cols of false.
func grid(rows, cols int) [][]bool {
	g := make([][]bool, rows)
	for i := range g {
		g[i] = make([]bool, cols)
	}

	return g
}

// valid reports whether the transcript's calls are the plan's in order with
// none added.
func (a alignment) valid() bool {
	return a.fwd[len(a.plan)][len(a.tools)]
}

// canMatch reports whether the transcript's call k can be the plan's call i.
func (a alignment) canMatch(i, k int) bool {
	return a.plan[i].Tool == a.tools[k] && a.fwd[i][k] && a.bwd[i+1][k+1]
}

// canSkip reports whether the plan's call i can be one the transcript left
// out. Where repeated tools make that ambiguous, every call it can be is
// named: the verdict fails closed.
func (a alignment) canSkip(i int) bool {
	for k := 0; k <= len(a.tools); k++ {
		if a.fwd[i][k] && a.bwd[i+1][k] {
			return true
		}
	}

	return false
}
