package github_test

// Group A of PR 4a: the endpoint and body allow list. Every row of the design's
// chapter 3.1 table is accepted for the fixture run, a near miss of each is
// refused with ErrEndpointRefused, and a refused request never reaches gh
// (ADR 0040, "The publish program's GitHub side": there is no call beyond the
// fixed table, no PUT, no DELETE, no merge or review, no CVE request, no tag,
// release or other branch).

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
)

// endpointCase is one call the allow list judges. body is the marshalled
// request body, empty when the call sends none.
type endpointCase struct {
	name     string
	method   string
	endpoint string
	body     string
}

// fixtureBodies are the valid bodies of the write rows, filled for the fixture
// run. They are built here so that a row's accepted call and the near miss
// that changes one value start from the same document.
func fixtureBodies() map[string]string {
	return map[string]string{
		"blob":           `{"content":"aGVsbG8=","encoding":"utf-8"}`,
		"tree":           `{"base_tree":"` + fixtureCommit + `","tree":[{"path":"` + fixtureReportPath + `","mode":"100644","type":"blob","sha":"` + fixtureSHA + `"}]}`,
		"commit":         `{"message":"the audit report","tree":"` + fixtureSHA + `","parents":["` + fixtureCommit + `"]}`,
		"ref":            `{"ref":"` + fixtureBranchRef + `","sha":"` + fixtureSHA + `"}`,
		"pull":           `{"title":"t","body":"b","head":"` + fixtureBranch + `","base":"main","maintainer_can_modify":false,"draft":false}`,
		"issue":          `{"title":"t","body":"b","labels":["security"]}`,
		"advisoryCreate": `{"summary":"s","description":"d","severity":"high","vulnerabilities":[]}`,
		"advisoryCreateVulnerability": `{"summary":"s","description":"d","severity":"high",` +
			`"vulnerabilities":[` + fixtureVulnerability() + `]}`,
		"advisoryUpdate": `{"summary":"s","description":"d","severity":"high","state":"draft"}`,
	}
}

// fixtureVulnerability is a vulnerabilities element of the row-15 body with
// every key GitHub's advisory create takes, filled for one invented module.
func fixtureVulnerability() string {
	return `{"package":{"ecosystem":"go","name":"example.com/module"},` +
		`"vulnerable_version_range":"< 1.2.0","patched_versions":"1.2.0",` +
		`"vulnerable_functions":["parse"]}`
}

// acceptedEndpoints is every row of the design's table 3.1 with a call the
// table allows: the reads, the report's one commit with its blob, tree and ref,
// the pull request, the issues, and the advisories, whose state is only ever
// draft.
func acceptedEndpoints() []endpointCase {
	b := fixtureBodies()

	return []endpointCase{
		{name: "row 1: the run's artifacts", method: "GET", endpoint: fixtureArtifactsEndpoint},
		{name: "row 2: a release tag", method: "GET", endpoint: fixtureReleaseEndpoint},
		{name: "row 3: an image digest's attestations", method: "GET", endpoint: fixtureRepoPrefix + "/attestations/" + fixtureDigest + "?per_page=100"},
		{name: "row 4: a commit", method: "GET", endpoint: fixtureRepoPrefix + "/git/commits/" + fixtureCommit},
		{name: "row 5: the report branch", method: "GET", endpoint: fixtureRepoPrefix + "/git/ref/heads/" + fixtureBranch},
		{name: "row 6: a blob", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs", body: b["blob"]},
		{name: "row 7: a tree", method: "POST", endpoint: fixtureRepoPrefix + "/git/trees", body: b["tree"]},
		{name: "row 8: a commit", method: "POST", endpoint: fixtureRepoPrefix + "/git/commits", body: b["commit"]},
		{name: "row 9: a ref", method: "POST", endpoint: fixtureRepoPrefix + "/git/refs", body: b["ref"]},
		{name: "row 10: the compare of the report commit", method: "GET", endpoint: fixtureRepoPrefix + "/compare/" + fixtureCommit + "..." + fixtureSHA},
		{name: "row 11: the report's pull request", method: "POST", endpoint: fixtureRepoPrefix + "/pulls", body: b["pull"]},
		{name: "row 12: the issues' first page", method: "GET", endpoint: fixtureIssuesQuery + "page=1"},
		{name: "row 12: the issues' tenth page", method: "GET", endpoint: fixtureIssuesQuery + "page=10"},
		{name: "row 13: an issue", method: "POST", endpoint: fixtureRepoPrefix + "/issues", body: b["issue"]},
		{name: "row 14: the advisories", method: "GET", endpoint: fixtureAdvisoriesEndpoint},
		{name: "row 14: the advisories' next page", method: "GET", endpoint: fixtureAdvisoriesEndpoint + "&after=Y3Vyc29yOnYyOpK5MjAyNi0xMi0wMw=="},
		{name: "row 15: a draft advisory", method: "POST", endpoint: fixtureRepoPrefix + "/security-advisories", body: b["advisoryCreate"]},
		{name: "row 15: a draft advisory naming a vulnerability", method: "POST", endpoint: fixtureRepoPrefix + "/security-advisories", body: b["advisoryCreateVulnerability"]},
		{name: "row 16: a draft advisory updated", method: "PATCH", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA, body: b["advisoryUpdate"]},
	}
}

// writeEndpoints is every write row of the table with a call the table
// accepts, taken from acceptedEndpoints so that the two cannot drift: the rows
// that create the report's blob, tree, commit and ref, the pull request, the
// issues and the advisories, and the one that updates an advisory.
func writeEndpoints() []endpointCase {
	out := make([]endpointCase, 0, len(acceptedEndpoints()))

	for _, tc := range acceptedEndpoints() {
		if tc.method != "GET" {
			out = append(out, tc)
		}
	}

	return out
}

// refusedEndpoints is a near miss of every row: another repository, another run
// ID, another branch or date or ref, a tree of two entries or another path, a
// commit whose parent is not the audited SHA, a pull request a maintainer may
// modify or that targets another base, an issue with another label or an extra
// key, an advisory created with state, cve_id, credits or collaborators and one
// updated to anything but draft, an unknown body key, a body on a read, PUT and
// DELETE, the merge, review, CVE, tag and release endpoints, and an endpoint
// that gh would read as a flag, a path or a URL.
func refusedEndpoints() []endpointCase {
	b := fixtureBodies()

	return []endpointCase{
		{name: "another repository", method: "GET", endpoint: "repos/example/other/actions/runs/" + fixtureRunID + "/artifacts?per_page=100"},
		{name: "a repository that extends ours", method: "GET", endpoint: "repos/example/shift2eu2/releases/tags/" + fixtureTag},
		{name: "another run's artifacts", method: "GET", endpoint: fixtureRepoPrefix + "/actions/runs/1234567891/artifacts?per_page=100"},
		{name: "an extra query parameter", method: "GET", endpoint: fixtureArtifactsEndpoint + "&page=2"},
		{name: "another release tag", method: "GET", endpoint: fixtureRepoPrefix + "/releases/tags/latest"},
		{name: "a tag that is no semver", method: "GET", endpoint: fixtureRepoPrefix + "/releases/tags/v0.12"},
		{name: "a 64-hex commit", method: "GET", endpoint: fixtureRepoPrefix + "/git/commits/" + strings.Repeat("a", 64)},
		{name: "an uppercase commit", method: "GET", endpoint: fixtureRepoPrefix + "/git/commits/" + strings.ToUpper(fixtureCommit)},
		{name: "another date's branch", method: "GET", endpoint: fixtureRepoPrefix + "/git/ref/heads/security-audit/2026-12-04-" + fixtureRunID},
		{name: "another run's branch", method: "GET", endpoint: fixtureRepoPrefix + "/git/ref/heads/security-audit/" + fixtureDate + "-1234567891"},
		{name: "a branch outside security-audit", method: "GET", endpoint: fixtureRepoPrefix + "/git/ref/heads/main"},
		{name: "a compare against another base", method: "GET", endpoint: fixtureRepoPrefix + "/compare/" + fixtureSHA + "..." + fixtureSHA},
		{
			name: "a ref to another date's branch", method: "POST", endpoint: fixtureRepoPrefix + "/git/refs",
			body: `{"ref":"refs/heads/security-audit/2026-12-04-` + fixtureRunID + `","sha":"` + fixtureSHA + `"}`,
		},
		{
			name: "a ref to another run's branch", method: "POST", endpoint: fixtureRepoPrefix + "/git/refs",
			body: `{"ref":"refs/heads/security-audit/` + fixtureDate + `-1234567891","sha":"` + fixtureSHA + `"}`,
		},
		{
			name: "a ref to a tag", method: "POST", endpoint: fixtureRepoPrefix + "/git/refs",
			body: `{"ref":"refs/tags/` + fixtureTag + `","sha":"` + fixtureSHA + `"}`,
		},
		{
			name: "a ref to main", method: "POST", endpoint: fixtureRepoPrefix + "/git/refs",
			body: `{"ref":"refs/heads/main","sha":"` + fixtureSHA + `"}`,
		},
		{
			name: "a tree with two entries", method: "POST", endpoint: fixtureRepoPrefix + "/git/trees",
			body: `{"base_tree":"` + fixtureCommit + `","tree":[` +
				`{"path":"` + fixtureReportPath + `","mode":"100644","type":"blob","sha":"` + fixtureSHA + `"},` +
				`{"path":"docs/security/audits/other.md","mode":"100644","type":"blob","sha":"` + fixtureSHA + `"}]}`,
		},
		{
			name: "a tree with another run's report", method: "POST", endpoint: fixtureRepoPrefix + "/git/trees",
			body: `{"base_tree":"` + fixtureCommit + `","tree":[{"path":"docs/security/audits/` + fixtureDate + `-1234567891.md","mode":"100644","type":"blob","sha":"` + fixtureSHA + `"}]}`,
		},
		{
			name: "a tree with another mode", method: "POST", endpoint: fixtureRepoPrefix + "/git/trees",
			body: `{"base_tree":"` + fixtureCommit + `","tree":[{"path":"` + fixtureReportPath + `","mode":"100755","type":"blob","sha":"` + fixtureSHA + `"}]}`,
		},
		{
			name: "a tree with another type", method: "POST", endpoint: fixtureRepoPrefix + "/git/trees",
			body: `{"base_tree":"` + fixtureCommit + `","tree":[{"path":"` + fixtureReportPath + `","mode":"100644","type":"tree","sha":"` + fixtureSHA + `"}]}`,
		},
		{
			name: "a commit with two parents", method: "POST", endpoint: fixtureRepoPrefix + "/git/commits",
			body: `{"message":"m","tree":"` + fixtureSHA + `","parents":["` + fixtureCommit + `","` + fixtureSHA + `"]}`,
		},
		{
			name: "a commit with another parent", method: "POST", endpoint: fixtureRepoPrefix + "/git/commits",
			body: `{"message":"m","tree":"` + fixtureSHA + `","parents":["` + fixtureSHA + `"]}`,
		},
		{
			name: "a commit with no parent", method: "POST", endpoint: fixtureRepoPrefix + "/git/commits",
			body: `{"message":"m","tree":"` + fixtureSHA + `","parents":[]}`,
		},
		{
			name: "a pull request maintainers may modify", method: "POST", endpoint: fixtureRepoPrefix + "/pulls",
			body: `{"title":"t","body":"b","head":"` + fixtureBranch + `","base":"main","maintainer_can_modify":true,"draft":false}`,
		},
		{
			name: "a pull request into another branch", method: "POST", endpoint: fixtureRepoPrefix + "/pulls",
			body: `{"title":"t","body":"b","head":"` + fixtureBranch + `","base":"release","maintainer_can_modify":false,"draft":false}`,
		},
		{
			name: "a pull request from another branch", method: "POST", endpoint: fixtureRepoPrefix + "/pulls",
			body: `{"title":"t","body":"b","head":"security-audit/other","base":"main","maintainer_can_modify":false,"draft":false}`,
		},
		{
			name: "a draft pull request", method: "POST", endpoint: fixtureRepoPrefix + "/pulls",
			body: `{"title":"t","body":"b","head":"` + fixtureBranch + `","base":"main","maintainer_can_modify":false,"draft":true}`,
		},
		{
			name: "an issue with another label", method: "POST", endpoint: fixtureRepoPrefix + "/issues",
			body: `{"title":"t","body":"b","labels":["bug"]}`,
		},
		{
			name: "an issue with two labels", method: "POST", endpoint: fixtureRepoPrefix + "/issues",
			body: `{"title":"t","body":"b","labels":["security","bug"]}`,
		},
		{
			name: "an issue with an extra key", method: "POST", endpoint: fixtureRepoPrefix + "/issues",
			body: `{"title":"t","body":"b","labels":["security"],"assignees":["someone"]}`,
		},
		{
			name: "an advisory created with a state", method: "POST", endpoint: fixtureRepoPrefix + "/security-advisories",
			body: `{"summary":"s","description":"d","severity":"high","vulnerabilities":[],"state":"draft"}`,
		},
		{
			name: "an advisory created with a cve_id", method: "POST", endpoint: fixtureRepoPrefix + "/security-advisories",
			body: `{"summary":"s","description":"d","severity":"high","vulnerabilities":[],"cve_id":"CVE-2026-0001"}`,
		},
		{
			name: "an advisory created with credits", method: "POST", endpoint: fixtureRepoPrefix + "/security-advisories",
			body: `{"summary":"s","description":"d","severity":"high","vulnerabilities":[],"credits":[]}`,
		},
		{
			name: "an advisory created with collaborating users", method: "POST", endpoint: fixtureRepoPrefix + "/security-advisories",
			body: `{"summary":"s","description":"d","severity":"high","vulnerabilities":[],"collaborating_users":[]}`,
		},
		{
			name: "an advisory created with collaborating teams", method: "POST", endpoint: fixtureRepoPrefix + "/security-advisories",
			body: `{"summary":"s","description":"d","severity":"high","vulnerabilities":[],"collaborating_teams":[]}`,
		},
		{
			name: "an advisory updated to published", method: "PATCH", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA,
			body: `{"summary":"s","description":"d","severity":"high","state":"published"}`,
		},
		{
			name: "an advisory updated to triage", method: "PATCH", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA,
			body: `{"summary":"s","description":"d","severity":"high","state":"triage"}`,
		},
		{
			name: "an advisory update without a state", method: "PATCH", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA,
			body: `{"summary":"s","description":"d","severity":"high"}`,
		},
		{
			name: "an advisory update with a cve_id", method: "PATCH", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA,
			body: `{"summary":"s","description":"d","severity":"high","state":"draft","cve_id":"CVE-2026-0001"}`,
		},
		{
			name: "an unknown body key in a blob", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: `{"content":"aGVsbG8=","encoding":"utf-8","sha":"` + fixtureSHA + `"}`,
		},
		{
			name: "a blob encoded otherwise than utf-8", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: `{"content":"aGVsbG8=","encoding":"base64"}`,
		},
		{
			name: "an unknown body key in a commit", method: "POST", endpoint: fixtureRepoPrefix + "/git/commits",
			body: `{"message":"m","tree":"` + fixtureSHA + `","parents":["` + fixtureCommit + `"],"author":{}}`,
		},
		{
			name: "a PATCH body with a duplicate state", method: "PATCH", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA,
			body: `{"summary":"s","state":"published","state":"draft"}`,
		},
		{
			name: "a blob with a duplicate encoding", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: `{"content":"aGVsbG8=","encoding":"utf-8","encoding":"utf-8"}`,
		},
		{
			name: "a tree whose entry holds a duplicate key", method: "POST", endpoint: fixtureRepoPrefix + "/git/trees",
			body: `{"base_tree":"` + fixtureCommit + `","tree":[{"path":"` + fixtureReportPath + `","mode":"100644","type":"blob",` +
				`"sha":"` + fixtureSHA + `","sha":"` + fixtureSHA + `"}]}`,
		},
		{name: "a lowercase method", method: "get", endpoint: fixtureReleaseEndpoint},
		{name: "an encoded branch separator", method: "GET", endpoint: fixtureRepoPrefix + "/git/ref/heads/security-audit%2F" + fixtureDate + "-" + fixtureRunID},
		{name: "an encoded parent directory", method: "GET", endpoint: fixtureRepoPrefix + "/%2e%2e/other/pulls"},
		{name: "a zero-padded page", method: "GET", endpoint: fixtureIssuesQuery + "page=01"},
		{name: "an endpoint with a fragment", method: "GET", endpoint: fixtureAdvisoriesEndpoint + "#frag"},
		{name: "an endpoint with a space", method: "GET", endpoint: "repos/example/shift2eu /git/ref/heads/main"},
		{name: "an endpoint with gh's owner placeholder", method: "GET", endpoint: "repos/{owner}/{repo}/git/ref/heads/main"},
		{name: "an endpoint with gh's repo placeholder", method: "GET", endpoint: "{repo}/git/ref/heads/main"},
		{name: "a body on a GET", method: "GET", endpoint: fixtureArtifactsEndpoint, body: b["issue"]},
		{name: "a PUT of a ref", method: "PUT", endpoint: fixtureRepoPrefix + "/git/refs", body: b["ref"]},
		{name: "a DELETE of the report branch", method: "DELETE", endpoint: fixtureRepoPrefix + "/git/refs/heads/" + fixtureBranch},
		{name: "a merge", method: "POST", endpoint: fixtureRepoPrefix + "/pulls/1/merge"},
		{name: "a review", method: "POST", endpoint: fixtureRepoPrefix + "/pulls/1/reviews"},
		{name: "a CVE request", method: "POST", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA + "/cve"},
		{name: "a tag ref", method: "POST", endpoint: fixtureRepoPrefix + "/git/refs/tags/" + fixtureTag},
		{name: "a release created", method: "POST", endpoint: fixtureRepoPrefix + "/releases"},
		{name: "an issue comment", method: "POST", endpoint: fixtureRepoPrefix + "/issues/1/comments"},
		{name: "an endpoint gh reads as a flag", method: "GET", endpoint: "-" + fixtureReleaseEndpoint},
		{name: "a flag instead of an endpoint", method: "GET", endpoint: "--hostname=api.example.com"},
		{name: "an endpoint with a leading slash", method: "GET", endpoint: "/" + fixtureRepoPrefix + "/pulls"},
		{name: "an endpoint with ..", method: "GET", endpoint: fixtureRepoPrefix + "/../other/pulls"},
		{name: "a full URL", method: "GET", endpoint: "https://api.github.com/" + fixtureRepoPrefix + "/pulls"},
		{name: "an empty endpoint", method: "GET", endpoint: ""},
		{name: "the eleventh page of a listing", method: "GET", endpoint: fixtureIssuesQuery + "page=11"},
		{name: "the zeroth page of a listing", method: "GET", endpoint: fixtureIssuesQuery + "page=0"},
	}
}

// requestBody is a case's body as Call marshals it, nil when it sends none.
func requestBody(t *testing.T, body string) any {
	t.Helper()

	if body == "" {
		return nil
	}

	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("a fixture body that is no JSON: %v", err)
	}

	return v
}

// bodyBytes is a case's body as Allowed reads it, nil when it sends none.
func bodyBytes(body string) []byte {
	if body == "" {
		return nil
	}

	return []byte(body)
}

// TestAllowedAcceptsEveryRowOfTheTable pins the allow list's one purpose: the
// calls the run needs, and only those, for this run's scope.
func TestAllowedAcceptsEveryRowOfTheTable(t *testing.T) {
	t.Parallel()

	for _, tc := range acceptedEndpoints() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := github.Allowed(testScope(), tc.method, tc.endpoint, bodyBytes(tc.body)); err != nil {
				t.Errorf("Allowed(%s %s): %v, want no error", tc.method, tc.endpoint, err)
			}
		})
	}
}

// TestAllowedRefusesANearMissOfEveryRow pins the other side: one changed value
// of a row, one endpoint of a forbidden family, one method the table has no row
// for, or one body key outside the row's rule is refused with
// ErrEndpointRefused, and the refusal names neither the endpoint nor a body's
// value.
func TestAllowedRefusesANearMissOfEveryRow(t *testing.T) {
	t.Parallel()

	for _, tc := range refusedEndpoints() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := github.Allowed(testScope(), tc.method, tc.endpoint, bodyBytes(tc.body))
			if !errors.Is(err, github.ErrEndpointRefused) {
				t.Fatalf("Allowed(%s %s) = %v, want an error wrapping ErrEndpointRefused", tc.method, tc.endpoint, err)
			}

			if tc.endpoint != "" && strings.Contains(err.Error(), tc.endpoint) {
				t.Errorf("the refusal names the endpoint: %v", err)
			}

			if tc.body != "" && (strings.Contains(err.Error(), fixtureSHA) || strings.Contains(err.Error(), "aGVsbG8=")) {
				t.Errorf("the refusal names a value of the body: %v", err)
			}
		})
	}
}

// TestCallRefusesBeforeGHRuns pins that the check sits in the function that runs
// gh: a refused request runs nothing at all, so the fake records no call.
func TestCallRefusesBeforeGHRuns(t *testing.T) {
	t.Parallel()

	refused := refusedEndpoints()

	for _, tc := range takeCases(refused, "another repository", "a DELETE of the report branch", "a merge", "a CVE request", "an endpoint gh reads as a flag", "an endpoint with ..", "a full URL", "an advisory updated to published") {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t)
			client := newFixtureClient(t, fake.Path)

			_, err := client.Call(t.Context(), github.Request{Method: tc.method, Endpoint: tc.endpoint, Body: requestBody(t, tc.body)})
			if !errors.Is(err, github.ErrEndpointRefused) {
				t.Fatalf("Call(%s %s) = %v, want an error wrapping ErrEndpointRefused", tc.method, tc.endpoint, err)
			}

			if calls := fake.Calls(t); len(calls) != 0 {
				t.Errorf("the refused call ran gh %d time(s), want 0: %+v", len(calls), calls)
			}
		})
	}
}

// TestAllowedChecksTheAdvisoryCreateBody pins row 15's body rule (audit finding
// 3, review finding 9): summary and description are both required and both
// non-empty, and every element of vulnerabilities is an object of GitHub's
// four keys, its package an object of its two. A body outside the rule is
// refused with ErrEndpointRefused, so a model's finding cannot widen the
// advisory's shape (ADR 0040).
func TestAllowedChecksTheAdvisoryCreateBody(t *testing.T) {
	t.Parallel()

	const endpoint = fixtureRepoPrefix + "/security-advisories"

	const (
		bothFields = `"summary":"s","description":"d"`
		vulnerable = `{"package":{"ecosystem":"go","name":"example.com/module"}}`
	)

	for _, tc := range []struct {
		name     string
		body     string
		accepted bool
	}{
		{name: "the row-15 body", body: `{` + bothFields + `,"severity":"high","vulnerabilities":[]}`, accepted: true},
		{name: "a vulnerability with every key", body: `{` + bothFields + `,"severity":"high","vulnerabilities":[` + fixtureVulnerability() + `]}`, accepted: true},
		{
			name: "a vulnerability whose range and versions are null", accepted: true,
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/module"},` +
				`"vulnerable_version_range":null,"patched_versions":null,"vulnerable_functions":null}]}`,
		},
		{name: "a body with neither severity nor vulnerabilities", body: `{` + bothFields + `}`, accepted: true},

		{name: "no summary", body: `{"description":"d"}`},
		{name: "no description", body: `{"summary":"s"}`},
		{name: "an empty summary", body: `{"summary":"","description":"d"}`},
		{name: "an empty description", body: `{"summary":"s","description":""}`},
		{name: "a summary that is no string", body: `{"summary":[1],"description":"d"}`},
		{name: "a description that is null", body: `{"summary":"s","description":null}`},
		{name: "vulnerabilities null", body: `{` + bothFields + `,"vulnerabilities":null}`},
		{name: "vulnerabilities that are no array", body: `{` + bothFields + `,"vulnerabilities":` + vulnerable + `}`},
		{name: "a vulnerability that is no object", body: `{` + bothFields + `,"vulnerabilities":["< 1.2.0"]}`},
		{
			name: "an unknown key in a vulnerability",
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/module"},"notes":"n"}]}`,
		},
		{
			name: "a duplicate key in a vulnerability",
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/module"},` +
				`"vulnerable_version_range":"< 1.2.0","vulnerable_version_range":"< 1.3.0"}]}`,
		},
		{name: "a vulnerability without a package", body: `{` + bothFields + `,"vulnerabilities":[{"patched_versions":"1.2.0"}]}`},
		{
			name: "an unknown key in a vulnerability's package",
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/module","purl":"pkg:go/module"}}]}`,
		},
		{
			name: "a package without an ecosystem",
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"name":"example.com/module"}}]}`,
		},
		{name: "a package that is no object", body: `{` + bothFields + `,"vulnerabilities":[{"package":"example.com/module"}]}`},
		{name: "a package name that is no string", body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":1}}]}`},
		{
			name: "a version range that is no string",
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/module"},"vulnerable_version_range":12}]}`,
		},
		{
			name: "patched versions that are no string",
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/module"},"patched_versions":["1.2.0"]}]}`,
		},
		{
			name: "vulnerable functions that are no array",
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/module"},"vulnerable_functions":"parse"}]}`,
		},
		{
			name: "vulnerable functions holding a value that is no string",
			body: `{` + bothFields + `,"vulnerabilities":[{"package":{"ecosystem":"go","name":"example.com/module"},"vulnerable_functions":[1]}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := github.Allowed(testScope(), "POST", endpoint, bodyBytes(tc.body))

			switch {
			case tc.accepted && err != nil:
				t.Fatalf("Allowed(POST %s) = %v, want no error: the body is inside row 15's rule", endpoint, err)
			case !tc.accepted && !errors.Is(err, github.ErrEndpointRefused):
				t.Fatalf("Allowed(POST %s) = %v, want an error wrapping ErrEndpointRefused: the body is outside row 15's rule", endpoint, err)
			}
		})
	}
}

// TestAllowedRefusesDataAfterTheBody pins decodeObject's stated guarantee on
// raw bytes: a body is exactly one JSON object, so anything after its closing
// brace is refused with ErrEndpointRefused even when the object itself is
// inside the row's rule (raw bytes, because the other tables marshal a
// document and so cannot carry a trailing token). A body that is the object
// alone, or the object followed by nothing but JSON whitespace, stays
// accepted.
func TestAllowedRefusesDataAfterTheBody(t *testing.T) {
	t.Parallel()

	b := fixtureBodies()

	for _, tc := range []struct {
		name     string
		method   string
		endpoint string
		body     []byte
		accepted bool
	}{
		{
			name: "the blob's body alone", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: []byte(b["blob"]), accepted: true,
		},
		{
			name: "the blob's body and a newline", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: []byte(b["blob"] + "\n"), accepted: true,
		},
		{
			name: "the blob's body and every kind of JSON whitespace", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: []byte(b["blob"] + " \t\r\n"), accepted: true,
		},
		{
			name: "a word after the blob's body", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: []byte(b["blob"] + " junk"),
		},
		{
			name: "a second closing brace after the blob's body", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: []byte(b["blob"] + "}"),
		},
		{
			name: "a second value after the blob's body", method: "POST", endpoint: fixtureRepoPrefix + "/git/blobs",
			body: []byte(b["blob"] + `{}`),
		},
		{
			name: "the advisory update's body alone", method: "PATCH", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA,
			body: []byte(b["advisoryUpdate"]), accepted: true,
		},
		{
			name: "a word after the advisory update's body", method: "PATCH", endpoint: fixtureRepoPrefix + "/security-advisories/" + fixtureGHSA,
			body: []byte(b["advisoryUpdate"] + " junk"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := github.Allowed(testScope(), tc.method, tc.endpoint, tc.body)

			switch {
			case tc.accepted && err != nil:
				t.Fatalf("Allowed(%s %s) = %v, want no error: the body is the object, with nothing after it but JSON whitespace", tc.method, tc.endpoint, err)
			case !tc.accepted && !errors.Is(err, github.ErrEndpointRefused):
				t.Fatalf("Allowed(%s %s) = %v, want an error wrapping ErrEndpointRefused: the body holds more than one JSON object", tc.method, tc.endpoint, err)
			}
		})
	}
}

// takeCases returns the named cases of cases, so that a table shared with
// another test can lend it a few of its rows.
func takeCases(cases []endpointCase, names ...string) []endpointCase {
	out := make([]endpointCase, 0, len(names))

	for _, name := range names {
		for _, tc := range cases {
			if tc.name == name {
				out = append(out, tc)
			}
		}
	}

	return out
}
