package github_test

// The fixtures the three behaviour test files share (PRD #175 milestone 2,
// PR 4a): one sanitised repository, run, date and commit, and the clients the
// tests drive the fake gh with. Every value is invented: the repository is
// example/shift2eu, the run is 1234567890, the date 2026-12-03 and the SHAs
// are hex that does not decode to printable ASCII. A token is invented at test
// time, never real.

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
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

	// fixtureArtifactsEndpoint is the artifact check's one call.
	fixtureArtifactsEndpoint = fixtureRepoPrefix + "/actions/runs/" + fixtureRunID + "/artifacts?per_page=100"
)

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
