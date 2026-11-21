// Package pincheck holds the Claude Code pin check (issue #276, ADR 0033): it
// reads the pin of the Claude Code CLI from the AI review workflow, names the
// npm registry document of exactly that version, and verifies that the
// document publishes the pinned integrity.
//
// .github/workflows/ai-review.yml pins the CLI's linux-x64 platform package,
// @anthropic-ai/claude-code-linux-x64, by two lines of its install step's env:
// CLAUDE_CODE_VERSION, which Renovate bumps, and CLAUDE_CODE_INTEGRITY, the
// npm sha512 integrity of that version's tarball, which Renovate cannot bump.
// FromWorkflow reads both, and refuses a file without exactly one of each, a
// version that is not MAJOR.MINOR.PATCH (so it never leaves the registry path
// or names a dist tag) and an integrity that is not sha512- and a base64
// SHA-512 digest. Pin.RegistryURL names the registry's document of that
// version, and Verify holds the document to the package, the version and the
// pinned integrity, naming the command that prints the published one when
// the integrity is stale.
//
// The check assumes the wiring test/lint's aireviewworkflow_test.go pins: the
// same install step runs .github/scripts/install-claude-code.sh with
// "@anthropic-ai/claude-code-linux-x64@${CLAUDE_CODE_VERSION}" and
// "${CLAUDE_CODE_INTEGRITY}", so the pair checked here is the pair installed.
//
// The package does no I/O of its own: it opens no file and no connection, and
// reads only the readers it is given. ci/claudecodepin reads the workflow and
// fetches the document. Text of the registry reaches an error quoted with %q,
// so it holds no raw newline or control character.
package pincheck
