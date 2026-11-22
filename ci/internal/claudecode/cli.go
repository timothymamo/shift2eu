package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrModel is returned for a run that failed or did not end its turn: a CLI
// that could not start, timed out, was cancelled, reported an error such as a
// usage limit, or printed no result. A Config the launcher refuses fails with
// it too, before anything runs.
var ErrModel = errors.New("model error")

// Prompt is what one run is asked: System, the trusted instructions, reaches
// the CLI in a file its --system-prompt-file flag names; User, which may hold
// untrusted data, reaches it on stdin alone.
type Prompt struct {
	System string
	User   string
}

// cliPath is the PATH the Claude Code CLI runs with: the system's directories
// alone, never the runner's PATH (ADR 0033).
const cliPath = "/usr/local/bin:/usr/bin:/bin"

// The caps on what the launcher keeps of the CLI's output. An answer of a
// review's size is far below maxCLIStdout; stderr is only ever quoted in part.
const (
	maxCLIStdout = 4 << 20
	maxCLIStderr = 64 << 10
)

// maxCLIExcerpt is the most of a text of the CLI's an error quotes, in bytes:
// one failing run writes one short, readable annotation.
const maxCLIExcerpt = 500

// cliWaitDelay is how long Ask waits, after the CLI is killed, for its output
// pipes to close before it closes them itself: a child the CLI started and the
// kill did not reach cannot hold the run open.
const cliWaitDelay = time.Second

// redacted stands in for the subscription token wherever a text of the CLI's
// would show it.
const redacted = "[redacted]"

// cliErrorClass is the constant class the error of an is_error result names,
// with the CLI's subtype and never its result text (issue #374).
const cliErrorClass = "claude cli error"

// unrecognisedCode stands in for a type or subtype of the CLI's result that
// is not shaped like one of the CLI's codes (cliCode).
const unrecognisedCode = "unrecognised"

// maxCLICodeLen is the longest type or subtype cliCode shows.
const maxCLICodeLen = 40

// cliCode returns code, a type or subtype the CLI printed, when it reads as
// one of the CLI's own codes: 1 to maxCLICodeLen bytes, each a lower-case
// ASCII letter or "_" (such as "result" or "error_max_turns"). Any other
// value, which could carry text of a pull request or of the model, reads
// unrecognisedCode (issue #374, audit L2).
func cliCode(code string) string {
	if code == "" || len(code) > maxCLICodeLen {
		return unrecognisedCode
	}

	for i := range len(code) {
		if b := code[i]; (b < 'a' || b > 'z') && b != '_' {
			return unrecognisedCode
		}
	}

	return code
}

// Config is one launcher's settings. Path, Token, Model and Timeout are
// required; Tools is optional, and its zero value is no tools at all.
type Config struct {
	// Path is the CLI's absolute path, the binary the caller's workflow
	// installed and verified.
	Path string
	// Token is the subscription's OAuth token, handed to the CLI as
	// CLAUDE_CODE_OAUTH_TOKEN and never put in an error.
	Token string
	// Model is the model the CLI is told to ask with --model: letters,
	// digits, '.' and '-' only, never empty, so no run asks the CLI's own
	// default model.
	Model string
	// Timeout is the run's own deadline.
	Timeout time.Duration
	// Tools are the built-in tools the CLI makes available (--tools), each a
	// tool name of ASCII letters only (such as Read or Grep). Nil or empty
	// is no tools: --tools "".
	Tools []string
}

// CLI asks a model through one run of the Claude Code CLI per Ask, `claude
// -p`, on the maintainer's Claude subscription (ADR 0033). It never puts the
// token in an error.
type CLI struct {
	cfg Config
}

// New returns a launcher with cfg. The config is checked when Ask runs, so a
// bad one fails the run with ErrModel before anything starts.
func New(cfg Config) *CLI {
	cfg.Tools = append([]string(nil), cfg.Tools...)

	return &CLI{cfg: cfg}
}

// cliResult is what Ask reads of the CLI's --output-format json answer.
type cliResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// Ask runs the CLI once and returns its result text:
//
//	claude -p --model <Model> --output-format json --safe-mode --tools <Tools>
//	  --setting-sources "" --strict-mcp-config --disable-slash-commands
//	  --no-session-persistence --system-prompt-file <file>
//
// where <Tools> is the config's tools joined with "," and "" for none.
// p.System is written to a file of a temporary directory of its own, which the
// flag names; p.User reaches the CLI on stdin alone. --safe-mode turns
// CLAUDE.md, skills, plugins, hooks, MCP servers and custom agents off, and
// --setting-sources "" skips the user, project and local settings files. The
// CLI runs in an empty temporary directory, with an empty temporary HOME and
// an environment of exactly the token, HOME, a fixed PATH and the switches
// that turn its updater, telemetry and error reports off: nothing of the
// runner's environment, an ANTHROPIC_API_KEY included, is inherited. It
// returns an error wrapping ErrModel when the config is refused (a relative
// path, a model or a tool name of the wrong shape), the CLI cannot start, runs
// past the timeout, is cancelled or stopped by the caller's deadline, prints
// no JSON result, reports an error, ends its turn other than with success, or
// prints an empty result; the error quotes at most maxCLIExcerpt bytes of the
// CLI's text, with the token redacted. An error result (is_error) is named by
// the constant class cliErrorClass and the CLI's subtype alone: its result
// text is never quoted (issue #374). The CLI's type and subtype are shown only
// as cliCode allows.
func (c *CLI) Ask(ctx context.Context, p Prompt) (string, error) {
	if !strings.HasPrefix(c.cfg.Path, "/") {
		return "", fmt.Errorf("%w: the Claude Code CLI's path %q is not absolute", ErrModel, c.cfg.Path)
	}

	if !validModel(c.cfg.Model) {
		return "", fmt.Errorf("%w: the model %q is not a model name (letters, digits, '.' and '-')", ErrModel, cliCode(c.cfg.Model))
	}

	for _, tool := range c.cfg.Tools {
		if !validTool(tool) {
			return "", fmt.Errorf("%w: a tool of the Claude Code CLI is not a tool name (ASCII letters only)", ErrModel)
		}
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%w: cancelled before the Claude Code CLI ran: %w", ErrModel, err)
	}

	dirs, err := newCLIDirs()
	if err != nil {
		return "", err
	}
	defer dirs.remove()

	systemFile := dirs.system + "/system-prompt.md"
	if err := os.WriteFile(systemFile, []byte(p.System), 0o600); err != nil {
		return "", fmt.Errorf("%w: write the system prompt file: %w", ErrModel, err)
	}

	runCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	stdout := &cappedBuffer{limit: maxCLIStdout}
	stderr := &cappedBuffer{limit: maxCLIStderr}

	// The path is the one the caller's workflow installed and verified, and
	// every argument is a constant, a checked model or tool name, or a file of
	// the launcher's own: no text of the prompt reaches the command line (ADR
	// 0033).
	cmd := exec.CommandContext(runCtx, c.cfg.Path, c.args(systemFile)...) //nolint:gosec // G204: a pinned, verified CLI with constant and checked arguments; the untrusted prompt goes to stdin
	cmd.Dir = dirs.work
	cmd.Env = c.env(dirs.home)
	cmd.Stdin = strings.NewReader(p.User)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = cliWaitDelay

	runErr := cmd.Run()

	// The run's own outcome comes first: a CLI that exited 0 with a successful
	// result just as the deadline passed has answered, and is not a timeout.
	// Every other outcome of a run whose context is done is reported as the
	// cancellation or the timeout it most likely is.
	text, err := c.answer(runErr, stdout, stderr)

	switch decideRunOutcome(err, ctx.Err(), runCtx.Err()) {
	case outcomeAnswer:
		return text, nil
	case outcomeCancelled:
		// The parent context ends either by a cancel or by the caller's own
		// deadline (ci/aireview's runDeadline); a deadline is no cancellation,
		// and saying so sends the maintainer looking for a caller that does
		// not exist (issue #316).
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: the Claude Code CLI was stopped: the run's deadline passed: %w", ErrModel, ctx.Err())
		}

		return "", fmt.Errorf("%w: the Claude Code CLI was cancelled: %w", ErrModel, ctx.Err())
	case outcomeTimedOut:
		return "", fmt.Errorf("%w: the Claude Code CLI timed out after %s", ErrModel, c.cfg.Timeout)
	default:
		return "", err
	}
}

// validModel reports whether model reads as a model name: 1 to 64 bytes of
// ASCII letters, digits, '.' and '-'.
func validModel(model string) bool {
	if model == "" || len(model) > 64 {
		return false
	}

	for i := range len(model) {
		b := model[i]
		if !isASCIILetter(b) && (b < '0' || b > '9') && b != '.' && b != '-' {
			return false
		}
	}

	return true
}

// validTool reports whether tool reads as a built-in tool's name: 1 to 64
// ASCII letters. A permission rule such as Bash(git log:*) is no tool name;
// rules belong in a settings file, never in --tools.
func validTool(tool string) bool {
	if tool == "" || len(tool) > 64 {
		return false
	}

	for i := range len(tool) {
		if !isASCIILetter(tool[i]) {
			return false
		}
	}

	return true
}

// isASCIILetter reports whether b is an ASCII letter.
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// runOutcome is how one run of the Claude Code CLI ended, as decideRunOutcome
// decides it. The tests reach it through export_test.go (issue #302).
type runOutcome int

// The outcomes of a run of the Claude Code CLI. The zero value is
// outcomeFailed, so an outcome nobody decided fails closed: it never reads as
// an answer.
const (
	// outcomeFailed is a run with no usable result that failed on its own:
	// its error is reported as it is.
	outcomeFailed runOutcome = iota
	// outcomeAnswer is a run whose result was read: its answer is used.
	outcomeAnswer
	// outcomeCancelled is a run with no usable result whose caller cancelled.
	outcomeCancelled
	// outcomeTimedOut is a run with no usable result that reached its own
	// deadline.
	outcomeTimedOut
)

// String names the outcome, so a test failure says which one it got.
func (o runOutcome) String() string {
	switch o {
	case outcomeFailed:
		return "failed"
	case outcomeAnswer:
		return "answer"
	case outcomeCancelled:
		return "cancelled"
	case outcomeTimedOut:
		return "timed out"
	default:
		return fmt.Sprintf("runOutcome(%d)", int(o))
	}
}

// decideRunOutcome decides a run's outcome from plain values (issue #274):
// answerErr is the error reading the run's result, nil when it was read;
// parentErr is the caller's context's error; runErr is the run's own context's
// error (or an error wrapping it). The run's own result comes first: a CLI that
// exited 0 with a successful result just as the deadline passed or the caller
// cancelled has answered. A run with no usable result is a cancellation when
// the caller's context is done, then a timeout when the run's deadline passed,
// and otherwise a failure of its own.
func decideRunOutcome(answerErr, parentErr, runErr error) runOutcome {
	switch {
	case answerErr == nil:
		return outcomeAnswer
	case parentErr != nil:
		return outcomeCancelled
	case errors.Is(runErr, context.DeadlineExceeded):
		return outcomeTimedOut
	default:
		return outcomeFailed
	}
}

// args returns the CLI's arguments with the system prompt in systemFile.
func (c *CLI) args(systemFile string) []string {
	return []string{
		"-p",
		"--model", c.cfg.Model,
		"--output-format", "json",
		"--safe-mode",
		"--tools", strings.Join(c.cfg.Tools, ","),
		"--setting-sources", "",
		"--strict-mcp-config",
		"--disable-slash-commands",
		"--no-session-persistence",
		"--system-prompt-file", systemFile,
	}
}

// env returns the CLI's whole environment: its allow list, nothing inherited.
func (c *CLI) env(home string) []string {
	return []string{
		"CLAUDE_CODE_OAUTH_TOKEN=" + c.cfg.Token,
		"HOME=" + home,
		"PATH=" + cliPath,
		"DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_TELEMETRY=1",
		"DISABLE_ERROR_REPORTING=1",
	}
}

// answer reads the CLI's result from what it printed and how it exited.
func (c *CLI) answer(runErr error, stdout, stderr *cappedBuffer) (string, error) {
	if runErr != nil && !isExit(runErr) {
		return "", fmt.Errorf("%w: start the Claude Code CLI: %s", ErrModel, c.excerpt(runErr.Error()))
	}

	if stdout.overflow {
		return "", fmt.Errorf("%w: the Claude Code CLI printed more than %d bytes", ErrModel, maxCLIStdout)
	}

	var result cliResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.buf.Bytes()), &result); err != nil {
		if runErr != nil {
			return "", fmt.Errorf("%w: the Claude Code CLI failed (%w) and printed no result%s", ErrModel, runErr, c.stderrNote(stderr))
		}

		return "", fmt.Errorf("%w: the Claude Code CLI's output is not a JSON result%s", ErrModel, c.stderrNote(stderr))
	}

	// The type and the subtype are the CLI's words, but a CLI that echoes what
	// it was given could carry a prompt's text in them, and these errors can be
	// a job's public ::error line: each is shown only when it reads as one of
	// the CLI's codes (cliCode, issue #374, audit L2).
	switch {
	case result.Type != "result":
		return "", fmt.Errorf("%w: the Claude Code CLI printed a %q element, not the result", ErrModel, cliCode(result.Type))
	case result.IsError:
		// The result text is the model's or the CLI's own words, which a
		// prompt's untrusted data can steer: the error names the constant
		// class and the subtype alone (issue #374).
		return "", fmt.Errorf("%w: %s (subtype %q%s)", ErrModel, cliErrorClass, cliCode(result.Subtype), exitNote(runErr))
	case result.Subtype != "success":
		return "", fmt.Errorf("%w: the model did not end its turn (subtype %q%s)", ErrModel, cliCode(result.Subtype), exitNote(runErr))
	case runErr != nil:
		return "", fmt.Errorf("%w: the Claude Code CLI failed (%w) after a successful result", ErrModel, runErr)
	case result.Result == "":
		return "", fmt.Errorf("%w: the Claude Code CLI's result is empty", ErrModel)
	}

	return result.Result, nil
}

// isExit reports whether err is the CLI's exit with a non-zero status, as
// opposed to a failure to start it.
func isExit(err error) bool {
	var exitErr *exec.ExitError

	return errors.As(err, &exitErr)
}

// exitNote describes runErr, the CLI's non-zero exit, for an error message, or
// returns "" when the CLI exited 0.
func exitNote(runErr error) string {
	if runErr == nil {
		return ""
	}

	return ", " + runErr.Error()
}

// stderrNote quotes the CLI's stderr for an error message, capped and
// redacted, or returns "" when it printed nothing there.
func (c *CLI) stderrNote(stderr *cappedBuffer) string {
	text := strings.TrimSpace(stderr.buf.String())
	if text == "" {
		return ""
	}

	return "; stderr: " + c.excerpt(text)
}

// excerpt returns text with the token redacted, cut to maxCLIExcerpt bytes at
// a rune boundary.
func (c *CLI) excerpt(text string) string {
	if c.cfg.Token != "" {
		text = strings.ReplaceAll(text, c.cfg.Token, redacted)
	}

	text = strings.ToValidUTF8(text, "�")
	if len(text) <= maxCLIExcerpt {
		return text
	}

	cut := maxCLIExcerpt
	// A UTF-8 continuation byte is 10xxxxxx: step back to the rune's first.
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}

	return text[:cut] + " [cut]"
}

// cappedBuffer keeps the first limit bytes written to it and drops the rest,
// recording that it did.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

// Write keeps what fits under the limit and reports every byte written, so the
// CLI is never blocked on a full pipe.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.buf.Len()
	if len(p) > room {
		b.overflow = true
		b.buf.Write(p[:max(room, 0)])

		return len(p), nil
	}

	b.buf.Write(p)

	return len(p), nil
}

// cliDirs are the three temporary directories of one run: the CLI's empty
// working directory, its empty HOME, and the directory of the system prompt
// file, which is neither.
type cliDirs struct {
	work   string
	home   string
	system string
}

// newCLIDirs makes the run's three directories, each fresh and empty.
func newCLIDirs() (cliDirs, error) {
	var dirs cliDirs

	for _, d := range []struct {
		into    *string
		pattern string
	}{
		{&dirs.work, "claudecode-cwd-"},
		{&dirs.home, "claudecode-home-"},
		{&dirs.system, "claudecode-system-"},
	} {
		dir, err := os.MkdirTemp("", d.pattern)
		if err != nil {
			dirs.remove()

			return cliDirs{}, fmt.Errorf("%w: make a temporary directory for the Claude Code CLI: %w", ErrModel, err)
		}

		*d.into = dir
	}

	return dirs, nil
}

// remove deletes the run's directories and what the CLI wrote into them.
func (d cliDirs) remove() {
	for _, dir := range []string{d.work, d.home, d.system} {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}
}
