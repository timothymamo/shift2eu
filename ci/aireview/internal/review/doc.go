// Package review is the AI reviewer of PRD #161 milestone 4 (ADR 0033): it
// reads a pull request through the GitHub API as data, asks Claude Opus 5.5
// for findings with main's CLAUDE.md and review skills and no tools, decides
// the review's event itself from those findings, posts one review and
// dismisses its own earlier change requests once nothing blocking remains.
//
// The model call is one run of the Claude Code CLI, `claude -p`, on the
// maintainer's Claude subscription (ClaudeCLI): every tool, setting source,
// MCP server and slash command off, the trusted instructions in a file of a
// temporary directory, the untrusted pull request on stdin, an empty working
// directory and an empty HOME of its own, and an explicit environment that
// inherits nothing of the runner's. Writing those temporary files and starting
// that one process are the only things it does with os and os/exec.
//
// It is no part of the product: only ci/aireview imports it (Go's internal
// rule), and neither release binary can reach it. It reads no environment
// variable and no file of the repository; ci/aireview hands it the settings,
// main's instructions, the HTTP client and the CLI's path, and the tests hand
// it httptest servers as base URLs and a fake CLI, so no test reaches the
// network.
//
// The model reads the pull request's diff without the generated and golden
// files of a list fixed in this package (FilterDiff, issue #324), and the size
// limit measures what remains; a symbolic link, a submodule or a mode change is
// never left out. The prompt lists the left-out paths as untrusted data, and the
// review names them, each with the lines it adds (a binary file as not scanned,
// a rename's old path as renamed), in a note its truncation never cuts. A
// model-free scan of those added lines (ScanLeftOut, issue #342) looks for
// account IDs, IPv4 addresses, host names (a provider's domain allowed only as
// its apex or a service one label below it) and key-like strings by fixed
// rules; the paths themselves are shown escaped and cut, never scanned. The
// note lists its first MaxScanFindings findings by file, line and
// kind, never the value, and the scan touches neither the prompt nor the
// findings nor the event. An answer of the wrong shape is asked once more,
// announced by a workflow notice that names the pull request and the failed
// shape check (Config.Notices, issue #343), and two in turn post a comment that
// says the review could not be read, on a context the run's cancellation does
// not reach, while the run still fails (issue #316). Run takes its deadline
// from its context.
//
// The poster supports exactly two events, COMMENT and REQUEST_CHANGES, and
// refuses any other before it makes an HTTP call: the reviewer never
// approves. Everything taken from the pull request is untrusted data, kept
// out of the system prompt and delimited in the user message.
package review
