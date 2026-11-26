// Command auditrun is the launcher the scheduled security audit's model step
// runs (ADR 0040's model-job amendment, issue #277, PRD #175 milestone 2):
//
//	auditrun -claude-tarball <path> -claude-integrity <sha512-...> -result-out <path>
//
// Once every step passed, it writes the result bytes the audit step read and
// checked, exactly those, to a new file at the -result-out path (O_EXCL, mode
// 0600; the workflow names $RUNNER_TEMP/result.json, out of the sandbox's
// reach), which the workflow's encrypt step reads: nothing reads the result
// file in the work area again by its path. A path that already exists fails
// the run with the audit's line and is left as it was; a failed run writes no
// file.
//
// It reads its other inputs from the step's environment through one
// os.LookupEnv: CLAUDE_CODE_OAUTH_TOKEN, SHIFT2EU_AUDIT_CANARY (64 lower-case
// hex characters), GITHUB_ENV, GITHUB_PATH, GITHUB_OUTPUT, GITHUB_STATE and
// GITHUB_STEP_SUMMARY, and ANTHROPIC_API_KEY only to refuse a step that holds
// it, which it checks first, before the command line and the other inputs, so
// such a step exits 3 whatever else is wrong. It reads no other variable. Of
// the runner's file commands it reads the paths only, never their content, and
// refuses with the usage exit a step where GITHUB_ENV, GITHUB_PATH,
// GITHUB_OUTPUT or GITHUB_STATE is unset, or where any of them, or the step
// summary when set, is not in the GITHUB_ENV file's own directory
// (filepath.Dir of the cleaned path): the audit digests that one directory
// across its run (ADR 0040, issues #615, #614), so a file command elsewhere
// would fall outside the digest. It then
// takes ci/internal/audit's six steps, the real ones with audit.Observe, under
// the root / (its run function takes the root, so a test runs the whole
// launcher under a temporary directory), prints the run's one
// line on stdout and appends it to the step summary, and exits with the
// library's exit code: 2 for a usage or input error, 3 for a run refused
// before the CLI ran. Neither secret is ever printed.
//
// It is no part of the product: a main package, which Go refuses to import,
// held by the strict depguard rule audit-main to the standard packages it
// imports and the audit's library.
package main
