package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
// required; every other field is optional, and its zero value is the AI
// reviewer's launch (ADR 0033): no tools, no effort, settings file or
// permission mode, safe mode on, the system prompt replacing the CLI's own,
// fresh temporary directories, the default PATH and one JSON result.
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

	// Effort is --effort, one of low, medium, high, xhigh and max. Empty
	// passes no flag: the CLI's default.
	Effort string

	// Settings is the absolute path of the one settings file the CLI loads
	// (--settings). Empty passes no flag.
	Settings string

	// PermissionMode is --permission-mode, dontAsk or plan only: never
	// bypassPermissions or any other. Empty passes no flag.
	PermissionMode string

	// AppendSystemPrompt appends the prompt's System to the CLI's own system
	// prompt (--append-system-prompt-file) instead of replacing it
	// (--system-prompt-file).
	AppendSystemPrompt bool

	// SafeModeOff leaves --safe-mode out, so the settings file applies.
	SafeModeOff bool

	// WorkDir is the CLI's working directory, an absolute path to a directory
	// that exists. Empty is a fresh empty temporary directory, removed after
	// the run.
	WorkDir string

	// Home is the CLI's HOME, an absolute path to a directory that exists.
	// Empty is a fresh empty temporary directory, removed after the run.
	Home string

	// Env are extra NAME=VALUE entries of the CLI's environment, after the
	// launcher's own. Only the names of allowedEnv are accepted: TMPDIR,
	// CLAUDE_CODE_SUBPROCESS_ENV_SCRUB and SHIFT2EU_AUDIT_CANARY.
	Env []string

	// SearchPath is the CLI's PATH. Empty is cliPath.
	SearchPath string

	// SystemPromptDir is the directory the system prompt's file is written
	// into, an absolute path to a directory that exists, which the caller
	// controls; the file is removed after the run. Empty is a fresh temporary
	// directory of the launcher's own under os.TempDir(), removed after the
	// run (the reviewer's launch).
	SystemPromptDir string

	// Redact are values, beside the token, that the launcher's error never
	// quotes: each stands in for "[redacted]" in the excerpt of the CLI's
	// text. An empty value is ignored.
	Redact []string

	// Transcript, when set, gets every byte the CLI prints on stdout: the run
	// uses --output-format stream-json --verbose, and its answer is the last
	// result element of the stream. Nil is --output-format json, one result
	// kept in memory.
	Transcript io.Writer
}

// efforts are the values --effort takes.
var efforts = []string{"low", "medium", "high", "xhigh", "max"}

// permissionModes are the values of --permission-mode the launcher passes on:
// the two that never run a tool call no rule allows. bypassPermissions,
// acceptEdits and every other mode are refused.
var permissionModes = []string{"dontAsk", "plan"}

// allowedEnv are the only variables a config's Env may set (ADR 0040,
// amended 2026-11-23, issue #277 round F): the audit's TMPDIR, the switch
// that scrubs the CLI's subprocesses' environment, and the audit's canary.
// Every other name is refused, whatever it would do: an allow list, not a
// deny list of the names someone thought of, so a variable that points the
// CLI at another credential, endpoint, proxy or certificate, or makes it or
// its shells load or run other code, never reaches it.
var allowedEnv = []string{
	"TMPDIR",
	"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB",
	"SHIFT2EU_AUDIT_CANARY",
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
	cfg.Env = append([]string(nil), cfg.Env...)
	cfg.Redact = append([]string(nil), cfg.Redact...)

	return &CLI{cfg: cfg}
}

// cliResult is what Ask reads of the CLI's --output-format json answer.
type cliResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// Ask runs the CLI once and returns its result text. With every optional field
// of the config at its zero value (the AI reviewer's launch) that is:
//
//	claude -p --model <Model> --output-format json --safe-mode --tools <Tools>
//	  --setting-sources "" --strict-mcp-config --mcp-config {"mcpServers":{}}
//	  --disable-slash-commands --no-session-persistence --system-prompt-file <file>
//
// where <Tools> is the config's tools joined with "," and "" for none; the
// other fields add or swap the flags args lists, in ADR 0040's order.
// p.System is written to a file of a temporary directory of its own, which the
// flag names; p.User reaches the CLI on stdin alone. --safe-mode turns
// CLAUDE.md, skills, plugins, hooks, MCP servers and custom agents off, and
// --setting-sources "" skips the user, project and local settings files. The
// CLI runs in the config's working directory and HOME, or in an empty
// temporary one of each, with an environment of exactly the token, HOME, PATH,
// the switches that turn its updater, telemetry and error reports off and the
// config's Env entries: nothing of the runner's environment, an
// ANTHROPIC_API_KEY included, is inherited. With a Transcript, every byte the
// CLI prints on stdout is copied there, whatever the run's outcome, and the
// answer is the stream's last result element. It
// returns an error wrapping ErrModel when the config is refused (a relative
// path, a model or a tool name of the wrong shape, a field checkLaunch
// refuses, which the error names), the CLI cannot start, runs
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

	if err := c.checkLaunch(); err != nil {
		return "", err
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%w: cancelled before the Claude Code CLI ran: %w", ErrModel, err)
	}

	dirs, err := newCLIDirs(c.cfg.WorkDir == "", c.cfg.Home == "", c.cfg.SystemPromptDir == "")
	if err != nil {
		return "", err
	}
	defer dirs.remove()

	workDir, home := dirs.work, dirs.home
	if c.cfg.WorkDir != "" {
		workDir = c.cfg.WorkDir
	}

	if c.cfg.Home != "" {
		home = c.cfg.Home
	}

	systemFile, err := writeSystemPrompt(dirs.system, c.cfg.SystemPromptDir, p.System)
	if err != nil {
		return "", err
	}
	defer os.Remove(systemFile)

	runCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	var stdout cliOutput = &cappedBuffer{limit: maxCLIStdout}

	var stream *resultStream
	if c.cfg.Transcript != nil {
		stream = &resultStream{transcript: c.cfg.Transcript}
		stdout = stream
	}

	stderr := &cappedBuffer{limit: maxCLIStderr}

	// The path is the one the caller's workflow installed and verified, and
	// every argument is a constant, a checked model or tool name, or a file of
	// the launcher's own: no text of the prompt reaches the command line (ADR
	// 0033).
	cmd := exec.CommandContext(runCtx, c.cfg.Path, c.args(systemFile)...) //nolint:gosec // G204: a pinned, verified CLI with constant and checked arguments; the untrusted prompt goes to stdin
	cmd.Dir = workDir
	cmd.Env = c.env(home)
	cmd.Stdin = strings.NewReader(p.User)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = cliWaitDelay

	runErr := cmd.Run()
	if stream != nil {
		stream.flush()
	}

	// The run's own outcome comes first: a CLI that exited 0 with a successful
	// result just as the deadline passed has answered, and is not a timeout.
	// Every other outcome of a run whose context is done is reported as the
	// cancellation or the timeout it most likely is.
	text, err := c.answer(runErr, stdout, stderr)

	switch decideRunOutcome(err, ctx.Err(), runCtx.Err()) {
	case outcomeAnswer:
		// An answer whose transcript is incomplete is no answer: the caller
		// keeps the transcript to judge the run by (ADR 0040's amendment).
		if stream != nil && stream.writeErr != nil {
			return "", fmt.Errorf("%w: write the Claude Code CLI's transcript: %w", ErrModel, stream.writeErr)
		}

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

// checkLaunch refuses, before anything runs, a field of the config ADR 0040's
// amendment does not allow, naming the field: an effort outside the five, a
// permission mode other than dontAsk or plan, a relative settings path, a
// working directory or HOME that is not an absolute path to a directory that
// exists, and an Env entry that is no NAME=VALUE or sets a variable outside
// allowedEnv.
func (c *CLI) checkLaunch() error {
	if c.cfg.Effort != "" && !contains(efforts, c.cfg.Effort) {
		return fmt.Errorf("%w: the effort is not one of %s", ErrModel, strings.Join(efforts, ", "))
	}

	if c.cfg.PermissionMode != "" && !contains(permissionModes, c.cfg.PermissionMode) {
		return fmt.Errorf("%w: the permission mode %q is not one of %s", ErrModel, cliCode(c.cfg.PermissionMode), strings.Join(permissionModes, ", "))
	}

	if c.cfg.Settings != "" && !strings.HasPrefix(c.cfg.Settings, "/") {
		return fmt.Errorf("%w: the settings file's path is not absolute", ErrModel)
	}

	if c.cfg.SearchPath != "" && strings.ContainsAny(c.cfg.SearchPath, "\x00\n") {
		return fmt.Errorf("%w: the CLI's PATH holds a NUL or a newline", ErrModel)
	}

	for _, dir := range []struct{ name, path string }{
		{"working directory", c.cfg.WorkDir},
		{"HOME", c.cfg.Home},
		{"system prompt's directory", c.cfg.SystemPromptDir},
	} {
		if dir.path == "" {
			continue
		}

		if !strings.HasPrefix(dir.path, "/") {
			return fmt.Errorf("%w: the CLI's %s is not an absolute path", ErrModel, dir.name)
		}

		info, err := os.Stat(dir.path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("%w: the CLI's %s is not a directory that exists", ErrModel, dir.name)
		}
	}

	for _, entry := range c.cfg.Env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !validEnvName(name) {
			return fmt.Errorf("%w: an environment entry of the CLI is not NAME=VALUE", ErrModel)
		}

		if !contains(allowedEnv, name) {
			return fmt.Errorf("%w: the environment entry %s is not one of %s", ErrModel, name, strings.Join(allowedEnv, ", "))
		}
	}

	return nil
}

// validEnvName reports whether name reads as a variable's name: 1 to 64 bytes
// of ASCII letters, digits and '_', not starting with a digit.
func validEnvName(name string) bool {
	if name == "" || len(name) > 64 || (name[0] >= '0' && name[0] <= '9') {
		return false
	}

	for i := range len(name) {
		b := name[i]
		if !isASCIILetter(b) && (b < '0' || b > '9') && b != '_' {
			return false
		}
	}

	return true
}

// contains reports whether list holds s.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}

	return false
}

// emptyMCPConfig is the MCP configuration both callers pass: no server, so
// --strict-mcp-config reads exactly this and nothing of the checkout's.
const emptyMCPConfig = `{"mcpServers":{}}`

// args returns the CLI's arguments with the system prompt in systemFile, in
// the order ADR 0040's amendment fixes.
func (c *CLI) args(systemFile string) []string {
	args := []string{"-p", "--model", c.cfg.Model}

	if c.cfg.Effort != "" {
		args = append(args, "--effort", c.cfg.Effort)
	}

	if c.cfg.Transcript != nil {
		args = append(args, "--output-format", "stream-json", "--verbose")
	} else {
		args = append(args, "--output-format", "json")
	}

	if !c.cfg.SafeModeOff {
		args = append(args, "--safe-mode")
	}

	args = append(args,
		"--tools", strings.Join(c.cfg.Tools, ","),
		"--setting-sources", "",
	)

	if c.cfg.Settings != "" {
		args = append(args, "--settings", c.cfg.Settings)
	}

	if c.cfg.PermissionMode != "" {
		args = append(args, "--permission-mode", c.cfg.PermissionMode)
	}

	systemFlag := "--system-prompt-file"
	if c.cfg.AppendSystemPrompt {
		systemFlag = "--append-system-prompt-file"
	}

	return append(args,
		"--strict-mcp-config",
		"--mcp-config", emptyMCPConfig,
		"--disable-slash-commands",
		"--no-session-persistence",
		systemFlag, systemFile,
	)
}

// env returns the CLI's whole environment: its allow list, nothing inherited.
// HOME is the config's Home, or home when the config names none; PATH is the
// config's SearchPath, or cliPath; the config's Env entries come last.
func (c *CLI) env(home string) []string {
	if c.cfg.Home != "" {
		home = c.cfg.Home
	}

	path := cliPath
	if c.cfg.SearchPath != "" {
		path = c.cfg.SearchPath
	}

	return append([]string{
		"CLAUDE_CODE_OAUTH_TOKEN=" + c.cfg.Token,
		"HOME=" + home,
		"PATH=" + path,
		"DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_TELEMETRY=1",
		"DISABLE_ERROR_REPORTING=1",
	}, c.cfg.Env...)
}

// answer reads the CLI's result from what it printed and how it exited.
func (c *CLI) answer(runErr error, stdout cliOutput, stderr *cappedBuffer) (string, error) {
	if runErr != nil && !isExit(runErr) {
		return "", fmt.Errorf("%w: start the Claude Code CLI: %s", ErrModel, c.excerpt(runErr.Error()))
	}

	out, overflow := stdout.result()
	if overflow {
		return "", fmt.Errorf("%w: the Claude Code CLI printed more than %d bytes", ErrModel, maxCLIStdout)
	}

	var result cliResult
	if err := json.Unmarshal(bytes.TrimSpace(out), &result); err != nil {
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
	for _, secret := range append([]string{c.cfg.Token}, c.cfg.Redact...) {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, redacted)
		}
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

// result returns what the buffer kept, the CLI's one JSON result, and whether
// the CLI printed more than it keeps.
func (b *cappedBuffer) result() ([]byte, bool) {
	return b.buf.Bytes(), b.overflow
}

// cliOutput is where the CLI's stdout goes: a writer that, after the run,
// returns the bytes of the result element answer reads, and whether that
// element was cut.
type cliOutput interface {
	io.Writer
	result() ([]byte, bool)
}

// resultStream takes the CLI's --output-format stream-json stdout: it copies
// every byte to the transcript and keeps the last complete line that is a
// result element, each line capped at maxCLIStdout. A write to the transcript
// that fails is recorded, and the CLI is never blocked on it.
type resultStream struct {
	transcript io.Writer
	writeErr   error

	// line is the line being read, overLine whether it ran past the cap.
	line     []byte
	overLine bool

	// last is the last result element read; overflow whether a line after it
	// ran past the cap, so the last result may be one the stream lost.
	last     []byte
	overflow bool
}

// Write copies p to the transcript and reads its lines.
func (s *resultStream) Write(p []byte) (int, error) {
	n := len(p)

	if s.writeErr == nil {
		if _, err := s.transcript.Write(p); err != nil {
			s.writeErr = err
		}
	}

	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.take(p)

			break
		}

		s.take(p[:i])
		s.endLine()
		p = p[i+1:]
	}

	return n, nil
}

// take adds part of a line, up to the cap.
func (s *resultStream) take(part []byte) {
	if room := maxCLIStdout - len(s.line); len(part) > room {
		s.overLine = true
		part = part[:max(room, 0)]
	}

	s.line = append(s.line, part...)
}

// endLine reads the line taken so far and starts the next.
func (s *resultStream) endLine() {
	switch {
	case s.overLine:
		s.overflow = true
	case isResultElement(s.line):
		s.last = append(s.last[:0], s.line...)
		s.overflow = false
	}

	s.line = s.line[:0]
	s.overLine = false
}

// flush reads a last line the CLI ended without a newline.
func (s *resultStream) flush() {
	if len(s.line) > 0 || s.overLine {
		s.endLine()
	}
}

// result returns the last result element and whether a line after it was cut.
func (s *resultStream) result() ([]byte, bool) {
	return s.last, s.overflow
}

// isResultElement reports whether line is a JSON element of type "result".
func isResultElement(line []byte) bool {
	var element struct {
		Type string `json:"type"`
	}

	return json.Unmarshal(line, &element) == nil && element.Type == "result"
}

// cliDirs are the temporary directories of one run: the CLI's empty working
// directory and its empty HOME, each made only when the config names none, and
// the directory of the system prompt file, which is neither.
type cliDirs struct {
	work   string
	home   string
	system string
}

// writeSystemPrompt writes the system prompt into a file of its own: under
// callerDir when the config names one, with a fresh name, else in ownDir, the
// launcher's own temporary directory. It returns the file's path.
func writeSystemPrompt(ownDir, callerDir, system string) (string, error) {
	if callerDir == "" {
		path := ownDir + "/system-prompt.md"
		if err := os.WriteFile(path, []byte(system), 0o600); err != nil {
			return "", fmt.Errorf("%w: write the system prompt file: %w", ErrModel, err)
		}

		return path, nil
	}

	file, err := os.CreateTemp(callerDir, "system-prompt-*.md")
	if err != nil {
		return "", fmt.Errorf("%w: write the system prompt file: %w", ErrModel, err)
	}

	_, writeErr := file.WriteString(system)
	closeErr := file.Close()

	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(file.Name())

		return "", fmt.Errorf("%w: write the system prompt file: %w", ErrModel, err)
	}

	return file.Name(), nil
}

// newCLIDirs makes the run's directories, each fresh and empty: the working
// directory when work is set, HOME when home is and the system prompt's when
// system is.
func newCLIDirs(work, home, system bool) (cliDirs, error) {
	var dirs cliDirs

	for _, d := range []struct {
		into    *string
		pattern string
		wanted  bool
	}{
		{&dirs.work, "claudecode-cwd-", work},
		{&dirs.home, "claudecode-home-", home},
		{&dirs.system, "claudecode-system-", system},
	} {
		if !d.wanted {
			continue
		}

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
