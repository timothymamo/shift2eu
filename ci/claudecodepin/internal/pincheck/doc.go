// Package pincheck holds the Claude Code pin check (issue #276, ADR 0033): it
// reads the pin of the Claude Code CLI from the one pin file, names the npm
// registry document of exactly that version, and verifies that the document
// publishes the pinned integrity.
//
// .github/claude-code.yaml (ADR 0040's model-job amendment, issue #277) pins
// the CLI's linux-x64 platform package, @anthropic-ai/claude-code-linux-x64,
// for every workflow that installs it, by two top-level lines: version:,
// under the # renovate: comment Renovate's generic manager reads, and
// integrity:, the npm sha512 integrity of that version's tarball, which
// Renovate cannot bump. FromPinFile reads both, and refuses a file without
// exactly one of each, a version that is not MAJOR.MINOR.PATCH (so it never
// leaves the registry path or names a dist tag) and an integrity that is not
// sha512- and a base64 SHA-512 digest. Each value must be a plain, unquoted
// YAML scalar with no trailing comment; anything else fails closed.
// Pin.RegistryURL names the registry's document of that version, and Verify
// holds the document to the package, the version and the pinned integrity,
// naming the command that prints the published one when the integrity is
// stale.
//
// The check assumes the wiring test/lint's claudecodeinstall_test.go and
// aireviewworkflow_test.go pin: every workflow that runs
// .github/scripts/install-claude-code.sh reads this file in a step before it
// and hands the two values to the script, so the pair checked here is the
// pair installed.
//
// The package does no I/O of its own: it opens no file and no connection, and
// reads only the readers it is given. ci/claudecodepin reads the pin file and
// fetches the document. Text of the registry reaches an error quoted with %q,
// so it holds no raw newline or control character.
package pincheck
