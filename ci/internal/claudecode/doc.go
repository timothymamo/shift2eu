// Package claudecode is the launcher of the Claude Code CLI that the CI tools
// ask a model through (ADR 0033, ADR 0040's milestone 2 amendment, issue
// #277): one run of `claude -p` on the maintainer's Claude subscription, with
// the model, the built-in tools, the effort, a settings file, a permission mode
// (dontAsk or plan only), an appended system prompt, safe mode, the working
// directory, HOME, extra environment entries, PATH and a streamed transcript
// as parameters. Every one's zero value is the AI reviewer's launch: no tools.
//
// By default a run gets an empty working directory and an empty HOME of its
// own; the trusted system prompt is always in a file of a temporary directory
// of its own, the untrusted user prompt on stdin, every setting source, MCP
// server (an empty --mcp-config) and slash command off, and an explicit
// environment that inherits nothing of the runner's: a variable the launcher
// sets itself is never a config's to set. A config of a shape the ADR does not
// allow is refused, naming the field, before anything runs. Writing those
// temporary files, checking that a directory it was given exists and starting
// that one process are the only things it does with os and os/exec. Its errors
// wrap ErrModel, quote at most a short, capped excerpt of the CLI's text with
// the token redacted, and name an error result by a constant class and its
// subtype alone (issue #374).
//
// It is no part of the product: it sits under ci/internal, so Go's internal
// rule lets only packages under ci/ import it, and the depguard rule
// claude-code-launcher-importers names the ones that may. It reads no
// environment variable and no file; its callers hand it the CLI's path, the
// token, the model and the tools.
package claudecode
