// Package claudecode is the launcher of the Claude Code CLI that the CI tools
// ask a model through (ADR 0033, ADR 0040's model-job amendment, issue
// #277): one run of `claude -p` on the maintainer's Claude subscription, with
// the model, the built-in tools, the effort, a settings file, a permission mode
// (default or dontAsk only), an appended system prompt, safe mode, the working
// directory, HOME, extra environment entries, PATH and a streamed transcript
// as parameters. Every one's zero value is the AI reviewer's launch: no tools.
//
// By default a run gets an empty working directory and an empty HOME of its
// own; the trusted system prompt is always in a file of its own - in a
// temporary directory of the launcher's, or in the directory the config names
// (the audit's state area) - removed after the run, the untrusted user prompt
// on stdin, every setting source, MCP server (an empty --mcp-config) and slash
// command off, and an explicit environment that inherits nothing of the
// runner's, where a config may add only TMPDIR,
// CLAUDE_CODE_SUBPROCESS_ENV_SCRUB and SHIFT2EU_AUDIT_CANARY (an allow list,
// issue #277 round F): any other name is refused. A config of a shape the ADR does not
// allow is refused, naming the field, before anything runs. Writing those
// temporary files, checking that a directory it was given exists and starting
// that one process are the only things it does with os and os/exec. Its errors
// wrap ErrModel, quote at most a short, capped excerpt of the CLI's text with
// the token and every value of the config's Redact redacted, and name an error result by a constant class and its
// subtype alone (issue #374).
//
// A config that asks for a permission mode without a transcript is refused
// with ErrModel before anything runs. A streamed run of one that asks for a
// permission mode is checked
// against the session's system/init element as the CLI prints it (ModeCheck,
// issue #592): an element of another mode, none before the session's turn, a
// second one, or one naming type, subtype or permissionMode twice or in
// another case stops the run at that line and fails it with a
// *PermissionModeError of fixed words, never a byte of the line. The stop is a
// backstop, not a barrier: it is asynchronous, so a later line's refusal may
// come after tool calls ran. The audit's judgement runs the same ModeCheck
// over the preflight's transcript.
//
// It is no part of the product: it sits under ci/internal, so Go's internal
// rule lets only packages under ci/ import it, and the depguard rule
// claude-code-launcher-importers names the ones that may. It reads no file
// and looks up no environment variable itself; its callers hand it the CLI's
// path, the token, the model and the tools. The one variable read on its
// behalf is TMPDIR: os.MkdirTemp uses it, through os.TempDir, for the
// temporary directories of a config that names none.
package claudecode
