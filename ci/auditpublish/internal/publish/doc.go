// Package publish is the model-free core of the scheduled security audit's
// publish job (PRD #175 milestone 2, ADR 0040, "The publish program's core"):
// it decides what the audit publishes and renders it, and does no I/O.
//
// It validates the model's structured result fail-closed (ParseResult: the
// schema, its caps, printable ASCII as the only text a field holds, unknown
// and duplicate fields, the finding limit), places
// each finding public or private from its check, severity, confirmation and
// category alone (Place), computes its fingerprint, plain for a public finding
// and an HMAC with the fingerprint key for a private one, derives each public
// finding's status and drops a recorded false positive from the previous
// merged report, keeps the public row of a finding that turns private,
// derives the per-check coverage from a fixed vocabulary,
// escapes every model-written text into a code span or a fenced code block,
// renders the report and the fixed branch, pull request and commit templates,
// flags a gap in the schedule against GapThresholdDays, scans every text it
// would publish for tokens, keys and encoded runs, and maps each outcome to an
// exit code and one summary line that holds no finding's text.
//
// The package opens no file and no connection, reads no environment variable
// and no clock, and starts no process: ci/auditpublish, its entry point, reads
// the result, the merged reports, the run's variables and the fingerprint key,
// runs gitleaks and calls GitHub, and passes the core what it read. Its errors
// name a field and a rule, never a value.
package publish
