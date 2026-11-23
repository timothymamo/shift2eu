// Package github is the GitHub side of the scheduled security audit's publish
// program (PRD #175 milestone 2, ADR 0040, "The publish program's GitHub
// side"): everything ci/auditpublish does that reaches outside its process.
//
// It runs gh and gitleaks, each by the absolute path its caller gives and with
// an environment it builds itself, nothing inherited. Every GitHub call goes
// through one function, (*Client).Call, which checks the method, the endpoint
// and the request body against a fixed table (Endpoints, Allowed) before gh
// runs: the reads the run needs, the Git data endpoints of the one report
// commit and its security-audit/* ref, the report's pull request, the audit
// App's issues, and the repository's security advisories, whose state is only
// ever "draft". It never publishes an advisory, requests a CVE, reviews or
// merges a pull request, or touches a tag, a release or another branch.
//
// It checks the run's artifacts before a job downloads them (CheckArtifacts),
// and scans every text with gitleaks (Gitleaks) beside the core's own scans.
// It decides nothing a model wrote: ci/auditpublish/internal/publish, the
// core, validates, places, renders and scans; this package reads GitHub, files
// what the core built and returns errors of the core's classes, which never
// carry an API response, a finding's text or a count of private findings.
//
// It reads no environment variable: what it needs of the step's environment,
// gh's token included, reaches it in its Config from ci/auditpublish.
package github
