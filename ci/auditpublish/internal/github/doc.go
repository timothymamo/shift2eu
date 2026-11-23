// Package github is the GitHub side of the scheduled security audit's publish
// program (PRD #175 milestone 2, ADR 0040, "The publish program's GitHub
// side"): everything ci/auditpublish does that reaches outside its process.
//
// It runs gh and gitleaks, each by the absolute path its caller gives and with
// an environment it builds itself, nothing inherited. Every GitHub call goes
// through one function, (*Client).Call, which checks the method, the endpoint
// and the request body against a fixed table (Allowed; Endpoints returns a
// copy of it) before gh
// runs: the reads the run needs, the Git data endpoints of the one report
// commit and its security-audit/* ref, the report's pull request, the audit
// App's issues, and the repository's security advisories, whose state is only
// ever "draft". It never publishes an advisory, requests a CVE, reviews or
// merges a pull request, or touches a tag, a release or another branch. A
// listing reads at most ten pages: the issue listing's row accepts pages 1 to
// 10 only.
//
// Call returns every answer gh prints a status line for, a 404 or any other
// non-2xx included, as a Response with no error: its caller decides what a
// status means. Only a gh that fails without a status line, or whose output is
// over the 8 MiB cap, is publish.ErrGitHub.
//
// It checks the run's artifacts before a job downloads them (CheckArtifacts),
// refusing an answer with anything after the one listing. The artifacts
// subcommand's client is read-only by construction (NewArtifactsClient, which
// sets ClientConfig.ReadOnly whatever its caller passes): Call refuses every
// method but GET with ErrEndpointRefused before the table and before gh runs,
// so the check that holds the job's token can reach no wider than the one read
// it needs, not the publish job's whole table.
//
// It scans every text with gitleaks (Gitleaks) beside the core's own scans.
// It decides nothing a model wrote: ci/auditpublish/internal/publish, the
// core, validates, places, renders and scans; this package reads GitHub, files
// what the core built and returns errors of the core's classes, which never
// carry an API response, a finding's text or a count of private findings.
//
// It reads no environment variable: what it needs of the step's environment,
// gh's token included, reaches it in its Config from ci/auditpublish.
package github
