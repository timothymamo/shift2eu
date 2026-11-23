// Command auditpublish is the scheduled security audit's publish program
// (PRD #175 milestone 2, ADR 0040, "The publish program's GitHub side"), run
// by the audit workflow's model and publish jobs. It has two subcommands:
//
//   - artifacts -job model|publish -gh <path> -tmp <dir>: lists the run's
//     artifacts and fails unless they are exactly the ones the job expects;
//   - publish -result <path> -reports <dir> -gh <path> -gitleaks <path> -tmp
//     <dir>: reads the decrypted result and the merged reports, and files the
//     report's pull request, the draft advisories and the issues.
//
// It reads the step's environment through one os.LookupEnv, passed to run:
// GH_TOKEN, which it hands to gh unread, GITHUB_SERVER_URL,
// GITHUB_REPOSITORY, GITHUB_RUN_ID, GITHUB_SHA, AUDIT_FINGERPRINT_KEY and
// GITHUB_STEP_SUMMARY, and nothing else. It refuses to run when the App's
// private key, either age private key or the Claude Code token is set in its
// environment at all. It prints exactly one line, to standard output and to
// the step summary, and exits with that line's code; no API response, error
// text, finding text or count of private findings reaches any output. The
// logic is in ci/auditpublish/internal/github and the core,
// ci/auditpublish/internal/publish.
package main
