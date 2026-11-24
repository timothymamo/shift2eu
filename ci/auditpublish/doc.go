// Command auditpublish is the scheduled security audit's publish program
// (PRD #175 milestone 2, ADR 0040, "The publish program's GitHub side"), run
// by the audit workflow's model and publish jobs. It has three subcommands:
//
//   - artifacts -job model|publish -gh <path> -tmp <dir>: lists the run's
//     artifacts and fails unless they are exactly the ones the job expects.
//   - image -result <path> -gh <path> -tmp <dir>: on a read-only client with
//     the job's token, checks that the result's tag is a release and that its
//     image digest has the release workflow's provenance attestation.
//   - publish -result <path> -reports <dir> -gh <path> -gitleaks <path> -tmp
//     <dir>: files each private finding's draft advisory, the report's pull
//     request and the issues of the previous report's open public findings,
//     every text scanned before the first write. A creation GitHub refuses
//     publishes the report as not filed and ends with outcome 5.
//
// It reads the step's environment through one os.LookupEnv, passed to run:
// GH_TOKEN, which it hands to gh unread, GITHUB_SERVER_URL,
// GITHUB_REPOSITORY, GITHUB_RUN_ID and GITHUB_STEP_SUMMARY, for image and
// publish also GITHUB_SHA (40 lowercase hex), and for publish
// AUDIT_FINGERPRINT_KEY (32 bytes or more), and nothing else. image and
// publish refuse a GITHUB_SERVER_URL other than exactly https://github.com.
// Before anything else it refuses to run when the App's private key, either
// age private key or the Claude Code token is set in its environment at all.
//
// It prints exactly one line, to standard output and appended to the step
// summary, and exits with that line's code; a usage error also writes one
// constant usage text to standard error. A step summary that is unset, not
// absolute or cannot be opened, written or closed fails the run closed: a run
// that would have passed prints the configuration line and exits 9 instead,
// and a failed run keeps its own line and code. No
// argument or environment value, API response, error text, finding text or
// count of private findings reaches any output. The logic is in
// ci/auditpublish/internal/github and the core, ci/auditpublish/internal/publish.
package main
