// Command claudecodepin is the Claude Code pin check (issue #276, ADR 0033):
// it fails a pull request whose pin file pins a Claude Code CLI integrity the
// npm registry does not publish for the pinned version.
//
//	go run ./ci/claudecodepin [-pin .github/claude-code.yaml]
//
// .github/claude-code.yaml is the one pin of the CLI, which every workflow
// that installs it reads (ADR 0040's milestone-2 amendment, issue #277).
// Renovate bumps its version: line but cannot bump its integrity: line, and a
// bump's own AI review runs main's copy of the workflow, so a stale integrity
// would show only after the merge. The workflow
// .github/workflows/claude-code-pin.yml runs this program on every pull
// request that changes the pin.
//
// It reads the pin with pincheck.FromPinFile, fetches the registry's document
// of exactly that version (pincheck.Pin.RegistryURL) over HTTPS with a
// timeout, no redirect and a bounded body, and holds it to the pin with
// pincheck.Verify. It sends no credential and no header but Accept, reads no
// environment variable and writes nothing but its one line of output. On a
// pass it prints one line to standard output and exits 0. Every failure, a
// registry that cannot be reached or answers anything but 200 included, is a
// failure, never a pass: it prints one ::error workflow command to standard
// error, whose message is escaped (%, CR and LF) so that no text of the
// registry or the pin file can end it and start another command, and exits 1.
//
// It runs in CI only; the default tests fake the registry and open no
// connection.
package main
