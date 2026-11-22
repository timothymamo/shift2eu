package audit

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// The exit codes of an audit run (ADR 0040, "The audit's programs").
const (
	// ExitOK is a run whose audit finished and wrote the result file.
	ExitOK = 0
	// ExitFailed is a run that failed: the model, a probe not refused, no
	// result file.
	ExitFailed = 1
	// ExitUsage is a usage or input error: a flag, the token, the canary or
	// $GITHUB_ENV.
	ExitUsage = 2
	// ExitRefused is a run refused before the CLI ran: an API key, a managed
	// setting, a checksum mismatch, the audit directory not as laid out.
	ExitRefused = 3
	// ExitBubblewrap is a sandbox that cannot start.
	ExitBubblewrap = 4
)

// The lines a run prints: one per run, a constant class for every failure, so
// no text of a step's error - which can hold the CLI's output, the token or
// the canary - is ever printed.
const (
	lineFinished        = "finished"
	lineManagedSettings = "refused: a Claude Code managed-settings path exists (ADR 0040)"
	lineChecksum        = "refused: the Claude Code tarball does not match its pin, or holds more than its binary"
	lineLayout          = "refused: the audit directory is not as laid out (ADR 0040)"
	lineBubblewrap      = "bubblewrap cannot start: see runbook step 10, the user-namespace note"
	linePreflight       = "the preflight failed: see runbook step 10"
	lineAudit           = "the audit failed: the model's run or its result file"
	lineMissingStep     = "the run was not given all six steps"
)

// Inputs are what one run is handed by the model step: the two secrets, the
// two paths from its environment and the tarball the install step kept, with
// the integrity the one pin names.
type Inputs struct {
	// Token is the subscription's OAuth token.
	Token string
	// Canary is the step's canary, 64 lower-case hex characters: probe 5's
	// target.
	Canary string
	// GitHubEnv is the path of the step's $GITHUB_ENV file, probe 6's target.
	GitHubEnv string
	// StepSummary is the path of the step's $GITHUB_STEP_SUMMARY file, or "".
	StepSummary string
	// Tarball is the path of the verified tarball the install step kept.
	Tarball string
	// Integrity is the tarball's npm integrity, sha512-<base64>.
	Integrity string
}

// Steps are the six steps of a run, in the ADR's order. Each takes the root
// that stands in for /; the context-taking ones find the run's Inputs with
// InputsFrom.
type Steps struct {
	// ManagedSettings refuses a root that holds a managed-settings path.
	ManagedSettings func(root string) error
	// Checksum checks the tarball against the integrity and extracts the CLI.
	Checksum func(root string, in Inputs) error
	// Layout checks the audit directory.
	Layout func(root string) error
	// Bubblewrap checks that the sandbox starts.
	Bubblewrap func(ctx context.Context) error
	// Preflight runs the sandbox preflight; a failure that is a *Verdict is
	// printed as its own text, which names the probes.
	Preflight func(ctx context.Context, root string) error
	// Audit runs the audit.
	Audit func(ctx context.Context, root string) error
}

// Run is one audit run: its root, its inputs, its steps and where its one line
// goes.
type Run struct {
	Root   string
	Inputs Inputs
	Steps  Steps
	Out    io.Writer
}

// inputsKey is the context key the run's Inputs travel under.
type inputsKey struct{}

// InputsFrom returns the Inputs of the run whose step ctx was handed, and
// whether it holds any: Run puts them there before the steps that take a
// context, so a step built before the inputs were read still finds them.
func InputsFrom(ctx context.Context) (Inputs, bool) {
	in, ok := ctx.Value(inputsKey{}).(Inputs)

	return in, ok
}

// Run takes the six steps in order, stops at the first that fails, prints one
// line to Out and returns the run's exit code.
func (r Run) Run(ctx context.Context) int {
	ctx = context.WithValue(ctx, inputsKey{}, r.Inputs)

	code, line := r.steps(ctx)
	if r.Out != nil {
		_, _ = fmt.Fprintln(r.Out, line)
	}

	return code
}

// steps runs the steps and returns the exit code and the line to print.
// A run never skips a step, so one it was not given fails it before the first.
func (r Run) steps(ctx context.Context) (int, string) {
	s := r.Steps
	if s.ManagedSettings == nil || s.Checksum == nil || s.Layout == nil ||
		s.Bubblewrap == nil || s.Preflight == nil || s.Audit == nil {
		return ExitFailed, lineMissingStep
	}

	if s.ManagedSettings(r.Root) != nil {
		return ExitRefused, lineManagedSettings
	}

	if s.Checksum(r.Root, r.Inputs) != nil {
		return ExitRefused, lineChecksum
	}

	if s.Layout(r.Root) != nil {
		return ExitRefused, lineLayout
	}

	if s.Bubblewrap(ctx) != nil {
		return ExitBubblewrap, lineBubblewrap
	}

	if err := s.Preflight(ctx, r.Root); err != nil {
		// A verdict's text is made of constants alone: the probes' numbers
		// and short names, never the transcript.
		var verdict *Verdict
		if errors.As(err, &verdict) {
			return ExitFailed, verdict.Error()
		}

		return ExitFailed, linePreflight
	}

	if s.Audit(ctx, r.Root) != nil {
		return ExitFailed, lineAudit
	}

	return ExitOK, lineFinished
}
