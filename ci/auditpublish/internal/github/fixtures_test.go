package github_test

// The fixtures the three behaviour test files share (PRD #175 milestone 2,
// PR 4a): one sanitised repository, run, date and commit, and the clients the
// tests drive the fake gh with. Every value is invented: the repository is
// example/shift2eu, the run is 1234567890, the date 2026-12-03 and the SHAs
// are hex that does not decode to printable ASCII. A token is invented at test
// time, never real.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

const (
	// fixtureRepository, fixtureRunID and fixtureDate are GITHUB_REPOSITORY,
	// GITHUB_RUN_ID and the run's UTC date; fixtureCommit is the audited main
	// commit.
	fixtureRepository = "example/shift2eu"
	fixtureRunID      = "1234567890"
	fixtureDate       = "2026-12-03"
	fixtureCommit     = "0123456789abcdef0123456789abcdef01234567"

	// fixtureSHA is the report commit's blob or tree: another 40-hex value,
	// which is no commit of the fixture run.
	fixtureSHA = "89abcdef0123456789abcdef0123456789abcdef"

	// fixtureDigest is the released image's digest: sha256: and 64 hex.
	fixtureDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// fixtureTag is the release a run names.
	fixtureTag = "v0.12.0"

	// fixtureGHSA is one identifier of the advisory rows.
	fixtureGHSA = "GHSA-2222-3333-4444"

	// fixtureRepoPrefix is the endpoint table's R: repos/<owner>/<name>.
	fixtureRepoPrefix = "repos/" + fixtureRepository

	// fixtureBranch is the report's branch, its ref and its file.
	fixtureBranch      = "security-audit/" + fixtureDate + "-" + fixtureRunID
	fixtureBranchRef   = "refs/heads/" + fixtureBranch
	fixtureReportPath  = "docs/security/audits/" + fixtureDate + "-" + fixtureRunID + ".md"
	fixtureIssuesQuery = fixtureRepoPrefix + "/issues?creator=shift2eu-audit%5Bbot%5D&state=all&per_page=100&"

	// fixtureAdvisoriesEndpoint is the advisory listing's first page.
	fixtureAdvisoriesEndpoint = fixtureRepoPrefix + "/security-advisories?per_page=100"

	// fixtureReleaseEndpoint is the release lookup's endpoint.
	fixtureReleaseEndpoint = fixtureRepoPrefix + "/releases/tags/" + fixtureTag

	// fixtureLatestReleaseEndpoint is the endpoint the release check reads
	// second: the repository's latest release, whose tag must be the result's.
	fixtureLatestReleaseEndpoint = fixtureRepoPrefix + "/releases/latest"

	// fixtureArtifactsEndpoint is the artifact check's one call.
	fixtureArtifactsEndpoint = fixtureRepoPrefix + "/actions/runs/" + fixtureRunID + "/artifacts?per_page=100"

	// fixtureServerURL is GITHUB_SERVER_URL: the one server the publish run
	// publishes to, whose URL the report, the commit and the pull request name.
	fixtureServerURL = "https://github.com"

	// The Git data API's endpoints of a report publication, and the
	// attestation listing of an image digest.
	fixtureBranchEndpoint       = fixtureRepoPrefix + "/git/ref/heads/" + fixtureBranch
	fixtureCommitEndpoint       = fixtureRepoPrefix + "/git/commits/" + fixtureCommit
	fixtureBlobsEndpoint        = fixtureRepoPrefix + "/git/blobs"
	fixtureTreesEndpoint        = fixtureRepoPrefix + "/git/trees"
	fixtureCommitsEndpoint      = fixtureRepoPrefix + "/git/commits"
	fixtureRefsEndpoint         = fixtureRepoPrefix + "/git/refs"
	fixturePullsEndpoint        = fixtureRepoPrefix + "/pulls"
	fixtureIssuesEndpoint       = fixtureRepoPrefix + "/issues"
	fixtureAttestationsEndpoint = fixtureRepoPrefix + "/attestations/" + fixtureDigest + "?per_page=100"

	// fixtureReportBody is the report the report-side tests file: an escaped
	// text whose path is the run's own.
	fixtureReportBody = "# Security audit report, " + fixtureDate + "\n"
)

// The Git data API's answers of a report publication: each sha distinct, so a
// binding of one answer's value into the next call is observable, and none of
// them the audited commit or the report's own path. None decodes to printable
// ASCII, as the fixtures' do not.
const (
	fixtureBaseTreeSHA     = "a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0"
	fixtureBlobSHA         = "b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0"
	fixtureAnswerTreeSHA   = "c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0"
	fixtureReportCommitSHA = "d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0"

	// fixtureOtherCommitSHA is a 40-hex run no answer of a report publication
	// carries: the commit-run check's one lookup.
	fixtureOtherCommitSHA = "e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0"
)

// fixtureFinding returns one finding of the model's result document: a
// confirmed finding of the tree review, public for the low and informational
// severities of a public category.
func fixtureFinding(rule, severity, category string) map[string]any {
	return map[string]any{
		"check":     "tree-review",
		"rule":      rule,
		"file":      "internal/cli/run.go",
		"line":      12,
		"sink":      "os.Open",
		"severity":  severity,
		"confirmed": true,
		"category":  category,
		"private":   false,
		"scenario":  "the scenario of " + rule,
		"evidence":  "the evidence of " + rule,
		"fix":       "the fix of " + rule,
		"dismissal": "",
	}
}

// unconfirmed returns f as an unconfirmed finding: one the report lists under
// needs investigation, never an issue.
func unconfirmed(f map[string]any) map[string]any {
	f["confirmed"] = false

	return f
}

// fixtureCheckIDs are the fifteen check ids of the result schema, in the
// coverage table's order.
var fixtureCheckIDs = []string{
	"dependencies",
	"history-secrets",
	"workflows",
	"scorecard",
	"scorecard-admin",
	"release-integrity",
	"image-pull",
	"binary-vulnerabilities",
	"image-scan",
	"image-runtime",
	"hostile-inventories",
	"fuzzing",
	"supply-chain",
	"tree-review",
	"agent-surface",
}

// fixtureResultDocument returns the model's result document as the schema
// takes it: the fixture run's commit, tag and digest, every check ran, and
// findings.
func fixtureResultDocument(findings ...map[string]any) map[string]any {
	checks := make([]any, 0, len(fixtureCheckIDs))

	for _, id := range fixtureCheckIDs {
		checks = append(checks, map[string]any{"id": id, "ran": true, "not_run": ""})
	}

	list := make([]any, 0, len(findings))
	for _, f := range findings {
		list = append(list, f)
	}

	return map[string]any{
		"schema":       1,
		"commit":       fixtureCommit,
		"release_tag":  fixtureTag,
		"image_digest": fixtureDigest,
		"tools":        []any{},
		"checks":       checks,
		"findings":     list,
		"changes":      map[string]any{"dependencies": []any{}, "narrative": "No dependency changed."},
		"limits":       []any{},
	}
}

// fixtureResultJSON returns the document's text.
func fixtureResultJSON(t *testing.T, findings ...map[string]any) []byte {
	t.Helper()

	data, err := json.Marshal(fixtureResultDocument(findings...))
	if err != nil {
		t.Fatalf("marshal the result document: %v", err)
	}

	return data
}

// fixtureResult parses the document into the core's Result.
func fixtureResult(t *testing.T, findings ...map[string]any) publish.Result {
	t.Helper()

	result, err := publish.ParseResult(fixtureResultJSON(t, findings...), fixtureCommit)
	if err != nil {
		t.Fatalf("ParseResult on the fixture document: %v", err)
	}

	return result
}

// fixtureResultFile writes the fixture document to a fresh temporary
// directory and returns the file's absolute path: the publish subcommand's
// -result.
func fixtureResultFile(t *testing.T, findings ...map[string]any) string {
	t.Helper()

	return fixtureResultFileFrom(t, fixtureResultDocument(findings...))
}

// fixtureResultFileFrom writes the result document to a fresh temporary
// directory and returns the file's absolute path.
func fixtureResultFileFrom(t *testing.T, document map[string]any) string {
	t.Helper()

	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal the result document: %v", err)
	}

	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write the result file %s: %v", path, err)
	}

	return path
}

// fixtureRun returns the run a report-side test publishes for.
func fixtureRun(t *testing.T) publish.Run {
	t.Helper()

	run, err := publish.NewRun(fixtureServerURL, fixtureRepository, fixtureRunID, fixtureDate)
	if err != nil {
		t.Fatalf("NewRun: %v", err)
	}

	return run
}

// fixtureKey returns an invented AUDIT_FINGERPRINT_KEY of the shortest length
// the core takes.
func fixtureKey(t *testing.T) publish.FingerprintKey {
	t.Helper()

	key, err := publish.NewFingerprintKey([]byte("invented-fingerprint-key-32-bytes"))
	if err != nil {
		t.Fatalf("NewFingerprintKey: %v", err)
	}

	return key
}

// fixtureTemplates renders the run's templates from the result.
func fixtureTemplates(t *testing.T, result publish.Result) publish.Templates {
	t.Helper()

	templates, err := publish.RenderTemplates(publish.ReportInput{Result: result, Run: fixtureRun(t)})
	if err != nil {
		t.Fatalf("RenderTemplates: %v", err)
	}

	return templates
}

// renderFixtureReport renders one run's report.
func renderFixtureReport(t *testing.T, result publish.Result, private publish.PrivateState, previous *publish.Previous, date, id string) publish.Rendered {
	t.Helper()

	run, err := publish.NewRun(fixtureServerURL, fixtureRepository, id, date)
	if err != nil {
		t.Fatalf("NewRun(%q, %q): %v", date, id, err)
	}

	rendered, err := publish.RenderReport(publish.ReportInput{Result: result, Run: run, Private: private, Previous: previous})
	if err != nil {
		t.Fatalf("RenderReport(%q, %q): %v", date, id, err)
	}

	return rendered
}

// fixturePrevious renders one run's report and reads it back as the previous
// report a later run derives from.
func fixturePrevious(t *testing.T, result publish.Result, private publish.PrivateState, previous *publish.Previous, date, id string) *publish.Previous {
	t.Helper()

	rendered := renderFixtureReport(t, result, private, previous, date, id)

	parsed, err := publish.ParsePreviousReport(filepath.Base(rendered.Path), []byte(rendered.Body))
	if err != nil {
		t.Fatalf("ParsePreviousReport(%q): %v", rendered.Path, err)
	}

	return &parsed
}

// writeFixtureReport writes the rendered report into dir, which is the
// checkout's docs/security/audits itself, under the report's own file name, as
// the checkout holds it, and returns its path. The rendered path is relative to
// the checkout, so only its base name places it under dir.
func writeFixtureReport(t *testing.T, dir string, rendered publish.Rendered) string {
	t.Helper()

	path := filepath.Join(dir, filepath.Base(filepath.FromSlash(rendered.Path)))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("make the reports directory: %v", err)
	}

	if err := os.WriteFile(path, []byte(rendered.Body), 0o600); err != nil {
		t.Fatalf("write the report %s: %v", path, err)
	}

	return path
}

// fixtureReportsDir makes a fresh directory a checkout's
// docs/security/audits would be, empty unless written to.
func fixtureReportsDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "docs", "security", "audits")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make the reports directory: %v", err)
	}

	return dir
}

// fixtureStatement returns the in-toto statement of an image attestation, as
// the attestation listing's payload holds it: the digest's hex, the workflow
// ref, the workflow repository and the predicate type.
func fixtureStatement(digestHex, ref, repository, predicateType string) map[string]any {
	return map[string]any{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": []any{map[string]any{
			"name":   "ghcr.io/" + fixtureRepository,
			"digest": map[string]any{"sha256": digestHex},
		}},
		"predicateType": predicateType,
		"predicate": map[string]any{"buildDefinition": map[string]any{"externalParameters": map[string]any{
			"workflow": map[string]any{
				"ref":        ref,
				"repository": repository,
				"path":       ".github/workflows/release.yml",
			},
		}}},
	}
}

// attestation returns one attestation of the listing, holding the payload the
// statement marshals to, in standard base64.
func attestation(t *testing.T, statement map[string]any) map[string]any {
	t.Helper()

	payload, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal the attestation's statement: %v", err)
	}

	return map[string]any{
		"repository_id": 1,
		"bundle": map[string]any{
			"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
			"dsseEnvelope": map[string]any{
				"payloadType": "application/vnd.in-toto+json",
				"payload":     base64.StdEncoding.EncodeToString(payload),
				"signatures":  []any{map[string]any{"sig": "AAAA"}},
			},
		},
	}
}

// attestationListing returns the attestations endpoint's answer.
func attestationListing(attestations ...map[string]any) map[string]any {
	list := make([]any, 0, len(attestations))
	for _, a := range attestations {
		list = append(list, a)
	}

	return map[string]any{"attestations": list}
}

// fixtureDigestHex is the released digest's 64 hex, without its algorithm.
func fixtureDigestHex() string { return strings.TrimPrefix(fixtureDigest, "sha256:") }

// fixtureWorkflowRepository is the repository an attestation's workflow names.
func fixtureWorkflowRepository() string { return fixtureServerURL + "/" + fixtureRepository }

// testScope is the scope of a publish run.
func testScope() github.Scope {
	return github.Scope{Repository: fixtureRepository, RunID: fixtureRunID, Date: fixtureDate, Commit: fixtureCommit}
}

// artifactScope is the scope of the artifact check, which knows no commit.
func artifactScope() github.Scope {
	return github.Scope{Repository: fixtureRepository, RunID: fixtureRunID, Date: fixtureDate}
}

// fixtureToken returns a token invented for one test.
func fixtureToken(t *testing.T) string {
	t.Helper()

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}

	return "test-token-" + hex.EncodeToString(b)
}

// newTestClient returns a client for the fake gh or program at cfg.GHPath, and
// closes it at the end of the test.
func newTestClient(t *testing.T, cfg github.ClientConfig) *github.Client {
	t.Helper()

	c, err := github.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient(%+v): %v", cfg, err)
	}

	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	})

	return c
}

// newFixtureClient returns a client for ghPath with the publish scope, a fresh
// token and a fresh temporary directory.
func newFixtureClient(t *testing.T, ghPath string) *github.Client {
	t.Helper()

	return newTestClient(t, github.ClientConfig{
		GHPath:  ghPath,
		Token:   github.NewToken(fixtureToken(t)),
		TempDir: t.TempDir(),
		Scope:   testScope(),
	})
}

// newReadOnlyClient returns a client for ghPath whose scope is scope, built
// with ReadOnly set: the artifacts subcommand's client, which reads the run's
// artifacts and may write nothing.
func newReadOnlyClient(t *testing.T, ghPath string, scope github.Scope) *github.Client {
	t.Helper()

	return newTestClient(t, github.ClientConfig{
		GHPath:   ghPath,
		Token:    github.NewToken(fixtureToken(t)),
		TempDir:  t.TempDir(),
		Scope:    scope,
		ReadOnly: true,
	})
}

// newTestArtifactsClient returns the client cfg describes, built through
// NewArtifactsClient, and closes it at the end of the test.
func newTestArtifactsClient(t *testing.T, cfg github.ClientConfig) *github.Client {
	t.Helper()

	c, err := github.NewArtifactsClient(cfg)
	if err != nil {
		t.Fatalf("NewArtifactsClient(%+v): %v", cfg, err)
	}

	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	})

	return c
}

// newArtifactClient returns a client for ghPath with the artifact check's
// scope, a fresh token and a fresh temporary directory.
func newArtifactClient(t *testing.T, ghPath string) *github.Client {
	t.Helper()

	return newTestClient(t, github.ClientConfig{
		GHPath:  ghPath,
		Token:   github.NewToken(fixtureToken(t)),
		TempDir: t.TempDir(),
		Scope:   artifactScope(),
	})
}
