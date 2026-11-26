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
// summary when set, is not a clean absolute path (audit.IsCleanAbsolute: the
// runner sets no other, and the directory check is lexical) or not in the
// GITHUB_ENV file's own directory (audit.FileCommandDir): the audit digests
// that one directory across its run (ADR 0040, issues #615, #614), so a file
// command elsewhere would fall outside the digest. It then takes
// ci/internal/audit's six steps, the real ones with audit.Observe, under the
// root / (its run function takes the root, so a test runs the whole launcher
// under a temporary directory), prints the run's one line on stdout and
// appends it to the step summary, and exits with the library's exit code: 2
// for a usage or input error, 3 for a run refused before the CLI ran. Neither
// secret is ever printed.
//
// It is the only legitimate writer of the step's five file commands -
// GITHUB_ENV, GITHUB_PATH, GITHUB_OUTPUT, GITHUB_STATE and the step summary
// when set - and it writes only its summary line, after the steps. So before
// the steps it records each one (audit.RecordFileCommands: its identity and
// the sha256 of its content), and after them it empties every one the steps
// changed (audit.FileCommands.Repair, issues #615, #614): the digest only
// detects a change, while the runner applies GITHUB_ENV and GITHUB_PATH after
// a failed step too and publishes the summary by its path. A file command is
// changed when its path no longer names the entry recorded, or when it no
// longer holds its recorded content - followed, for the summary, by exactly
// the bytes the run wrote to it. It is left as an empty regular file:
// truncated in place, or replaced by an empty file renamed over its path, so
// no link is followed. Any repair names the emptied file commands on stderr; a
// run that had passed then fails with the audit's exit, 1, and its result
// file is removed; a run that had already failed keeps its exit code. The
// repair tries every changed file command, whatever failed before it, so one
// it cannot empty (a non-empty directory over its path) never leaves another's
// bytes for the runner; each one it could not empty gets its own line naming
// it and saying the posted summary or the job's environment may be hostile,
// and fails the run.
//
// A file command that exists before the run but is no regular file - a
// directory, a link, a FIFO - is refused with exit 2 before any step
// (audit.CheckFileCommands, Lstat alone, before the summary is opened; the
// auditor's round-4 L1): the run could not record its content. The runner
// lays only regular files down, so a real step loses nothing; a path absent
// before the run is allowed and recorded as absent. The summary is opened
// without following a link (audit.OpenAppend); when it was absent, auditrun
// creates it, so a refusal after that (audit.RecordFileCommands) leaves an
// empty summary file behind, which is harmless. Every open of an existing file
// command, the summary's included, is O_RDWR and fails closed (issues #615,
// #614): on Linux an O_RDWR open never blocks on a FIFO swapped in between the
// Lstat and the open, so a file command the run may read but not write is
// refused with exit 2 before any step too, and the audit's digest of the
// directory fails on such an entry. The API key's refusal, which
// comes first, writes its line to the summary only when OpenAppend takes it,
// and to stdout always.
//
// It is no part of the product: a main package, which Go refuses to import,
// held by the strict depguard rule audit-main to the standard packages it
// imports and the audit's library.
package main
