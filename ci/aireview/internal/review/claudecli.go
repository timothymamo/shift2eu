package review

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

// cliPath is the PATH the Claude Code CLI runs with: the system's directories
// alone, never the runner's PATH (ADR 0033).
const cliPath = "/usr/local/bin:/usr/bin:/bin"

// The caps on what the reviewer keeps of the CLI's output. An answer of the
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

// ClaudeCLI asks the model through one run of the Claude Code CLI, `claude -p`,
// on the maintainer's Claude subscription (ADR 0033). It never puts the token
// in an error.
type ClaudeCLI struct {
	path    string
	token   string
	timeout time.Duration
}

// NewClaudeCLI returns an asker that runs the Claude Code CLI at path, an
// absolute path, with the subscription's OAuth token, and kills it after
// timeout.
func NewClaudeCLI(path, token string, timeout time.Duration) *ClaudeCLI {
	return &ClaudeCLI{path: path, token: token, timeout: timeout}
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
//	claude -p --model Model --output-format json --safe-mode --tools ""
//	  --setting-sources "" --strict-mcp-config --disable-slash-commands
//	  --no-session-persistence --system-prompt-file <file>
//
// p.System is written to a file of a temporary directory of its own, which the
// flag names; p.User, the untrusted pull request, reaches the CLI on stdin
// alone. --safe-mode turns CLAUDE.md, skills, plugins, hooks, MCP servers and
// custom agents off, and --setting-sources "" skips the user, project and
// local settings files. The CLI runs in an empty temporary directory, with an
// empty temporary HOME and an environment of exactly the token, HOME, a fixed
// PATH and the switches that turn its updater, telemetry and error reports off:
// nothing of the runner's environment, an ANTHROPIC_API_KEY included, is
// inherited. It returns an error wrapping ErrModel when the CLI cannot start,
// runs past the timeout, is cancelled or stopped by the run's deadline, prints no JSON result, reports an error,
// ends its turn other than with success, or prints an empty result; the error
// quotes at most maxCLIExcerpt bytes of the CLI's text, with the token
// redacted.
func (c *ClaudeCLI) Ask(ctx context.Context, p Prompt) (string, error) {
	if !strings.HasPrefix(c.path, "/") {
		return "", fmt.Errorf("%w: the Claude Code CLI's path %q is not absolute", ErrModel, c.path)
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

	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	stdout := &cappedBuffer{limit: maxCLIStdout}
	stderr := &cappedBuffer{limit: maxCLIStderr}

	// The path is the one ci/aireview's workflow installed and verified, and
	// every argument is a constant or a file of the reviewer's own: no text
	// of the pull request reaches the command line (ADR 0033).
	cmd := exec.CommandContext(runCtx, c.path, cliArgs(systemFile)...) //nolint:gosec // G204: a pinned, verified CLI with constant arguments; the untrusted prompt goes to stdin
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
		// The parent context ends either by a cancel or by the run's own
		// deadline (ci/aireview's runDeadline); a deadline is no cancellation,
		// and saying so sends the maintainer looking for a caller that does
		// not exist (issue #324).
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: the Claude Code CLI was stopped: the run's deadline passed: %w", ErrModel, ctx.Err())
		}

		return "", fmt.Errorf("%w: the Claude Code CLI was cancelled: %w", ErrModel, ctx.Err())
	case outcomeTimedOut:
		return "", fmt.Errorf("%w: the Claude Code CLI timed out after %s", ErrModel, c.timeout)
	default:
		return "", err
	}
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

// cliArgs returns the CLI's arguments with the system prompt in systemFile.
func cliArgs(systemFile string) []string {
	return []string{
		"-p",
		"--model", Model,
		"--output-format", "json",
		"--safe-mode",
		"--tools", "",
		"--setting-sources", "",
		"--strict-mcp-config",
		"--disable-slash-commands",
		"--no-session-persistence",
		"--system-prompt-file", systemFile,
	}
}

// env returns the CLI's whole environment: its allow list, nothing inherited.
func (c *ClaudeCLI) env(home string) []string {
	return []string{
		"CLAUDE_CODE_OAUTH_TOKEN=" + c.token,
		"HOME=" + home,
		"PATH=" + cliPath,
		"DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_TELEMETRY=1",
		"DISABLE_ERROR_REPORTING=1",
	}
}

// answer reads the CLI's result from what it printed and how it exited.
func (c *ClaudeCLI) answer(runErr error, stdout, stderr *cappedBuffer) (string, error) {
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

	switch {
	case result.Type != "result":
		return "", fmt.Errorf("%w: the Claude Code CLI printed a %q element, not the result", ErrModel, c.excerpt(result.Type))
	case result.IsError:
		return "", fmt.Errorf("%w: the Claude Code CLI reported an error (subtype %q%s): %s",
			ErrModel, c.excerpt(result.Subtype), exitNote(runErr), c.excerpt(result.Result))
	case result.Subtype != "success":
		return "", fmt.Errorf("%w: the model did not end its turn (subtype %q%s)", ErrModel, c.excerpt(result.Subtype), exitNote(runErr))
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
func (c *ClaudeCLI) stderrNote(stderr *cappedBuffer) string {
	text := strings.TrimSpace(stderr.buf.String())
	if text == "" {
		return ""
	}

	return "; stderr: " + c.excerpt(text)
}

// excerpt returns text with the token redacted, cut to maxCLIExcerpt bytes at
// a rune boundary.
func (c *ClaudeCLI) excerpt(text string) string {
	if c.token != "" {
		text = strings.ReplaceAll(text, c.token, redacted)
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
		{&dirs.work, "aireview-cwd-"},
		{&dirs.home, "aireview-home-"},
		{&dirs.system, "aireview-system-"},
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
