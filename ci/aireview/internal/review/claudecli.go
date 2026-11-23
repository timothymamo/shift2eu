package review

import (
	"context"
	"time"

	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

// ClaudeCLI asks the model through one run of the Claude Code CLI, `claude -p`,
// on the maintainer's Claude subscription (ADR 0033), with the launcher the AI
// reviewer shares with the scheduled audit (ci/internal/claudecode, issue
// #277). It never puts the token in an error.
type ClaudeCLI struct {
	cli *claudecode.CLI
}

// NewClaudeCLI returns an asker that runs the Claude Code CLI at path, an
// absolute path, with the subscription's OAuth token, asks Model with no tools
// at all, and kills the CLI after timeout.
func NewClaudeCLI(path, token string, timeout time.Duration) *ClaudeCLI {
	return &ClaudeCLI{cli: claudecode.New(claudecode.Config{
		Path:    path,
		Token:   token,
		Model:   Model,
		Timeout: timeout,
		// No Tools: the reviewer runs the CLI with --tools "" (ADR 0033).
	})}
}

// Ask runs the CLI once and returns its result text, as claudecode.CLI.Ask
// does:
//
//	claude -p --model Model --output-format json --safe-mode --tools ""
//	  --setting-sources "" --strict-mcp-config --mcp-config '{"mcpServers":{}}'
//	  --disable-slash-commands --no-session-persistence --system-prompt-file <file>
//
// p.System, the trusted instructions, reaches the CLI in that file; p.User,
// the untrusted pull request, on stdin alone. The CLI runs in an empty
// temporary directory, with an empty temporary HOME and an environment of
// exactly the token, HOME, a fixed PATH and the switches that turn its
// updater, telemetry and error reports off. Every error wraps ErrModel.
func (c *ClaudeCLI) Ask(ctx context.Context, p Prompt) (string, error) {
	return c.cli.Ask(ctx, p)
}
