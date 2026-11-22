// Command auditrun is the launcher the scheduled security audit's model step
// runs (ADR 0040's milestone-2 amendment, issue #277, PRD #175 milestone 2):
//
//	auditrun -claude-tarball <path> -claude-integrity <sha512-...>
//
// It reads its other inputs from the step's environment through one
// os.LookupEnv: CLAUDE_CODE_OAUTH_TOKEN, SHIFT2EU_AUDIT_CANARY (64 lower-case
// hex characters), GITHUB_ENV and GITHUB_STEP_SUMMARY, and ANTHROPIC_API_KEY
// only to refuse a step that holds it. It reads no other variable. It then
// takes ci/internal/audit's six steps under the root /, prints the run's one
// line on stdout and appends it to the step summary, and exits with the
// library's exit code: 2 for a usage or input error, 3 for a run refused
// before the CLI ran. Neither secret is ever printed.
//
// It is no part of the product: a main package, which Go refuses to import,
// held by the strict depguard rule audit-main to the standard packages it
// imports and the audit's library.
package main
