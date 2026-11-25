package audit

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

// The exit codes of an audit run (ADR 0040, "The audit's programs").
const (
	// ExitOK is a run whose audit finished and wrote the result file.
	ExitOK = 0
	// ExitFailed is a run that failed: the model, a probe not refused, no
	// result file.
	ExitFailed = 1
	// ExitUsage is a usage or input error: a flag, the token, the canary or
	// $GITHUB_ENV. Run never returns it: ci/auditrun returns it when it
	// cannot read its inputs, before any step runs.
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
	// linePermissionMode starts the line of a session whose init element
	// reports another permission mode than the launch asked for; the
	// launcher's fixed sentence follows (issue #592).
	linePermissionMode = "refused: "
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
	// Audit runs the audit and returns the result file's bytes it read and
	// checked, never the file's path.
	Audit func(ctx context.Context, root string) ([]byte, error)
}

// Run is one audit run: its root, its inputs, its steps, where its one line
// goes and who takes the audit's result.
type Run struct {
	Root   string
	Inputs Inputs
	Steps  Steps
	Out    io.Writer
	// Result, when set, is handed the result file's bytes the audit step read
	// and checked, exactly those, once, after every step passed; a run that
	// failed never calls it. It is the seam the publication (ADR 0040, step
	// 19) works from, so nothing reads the result file again by its path. A
	// Result that fails fails the run with the audit's line.
	Result func(body []byte) error
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
		if line, ok := modeRefusal(err); ok {
			return ExitFailed, line
		}

		// A verdict's text is made of constants alone: the probes' numbers
		// and short names, never the transcript.
		var verdict *Verdict
		if errors.As(err, &verdict) {
			return ExitFailed, verdict.Error()
		}

		return ExitFailed, linePreflight
	}

	body, err := s.Audit(ctx, r.Root)
	if err != nil {
		if line, ok := modeRefusal(err); ok {
			return ExitFailed, line
		}

		return ExitFailed, lineAudit
	}

	if r.Result != nil && r.Result(body) != nil {
		return ExitFailed, lineAudit
	}

	return ExitOK, lineFinished
}

// modeRefusal returns the line of a step's failure that holds the launcher's
// *claudecode.PermissionModeError (issue #592): linePermissionMode and the
// error's fixed sentence, which names the expected mode and a known mode or a
// class, never a byte of the session's element.
func modeRefusal(err error) (string, bool) {
	var modeErr *claudecode.PermissionModeError
	if !errors.As(err, &modeErr) {
		return "", false
	}

	return linePermissionMode + modeErr.Error(), true
}
