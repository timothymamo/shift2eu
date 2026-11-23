package github

import "fmt"

// BodyRule names the check a request body must pass for its endpoint.
type BodyRule int

// The body rules of the endpoint table (ADR 0040, "The publish program's
// GitHub side").
const (
	// BodyNone is a call that sends no body.
	BodyNone BodyRule = iota
	// BodyBlob is exactly {content, encoding: "utf-8"}.
	BodyBlob
	// BodyTree is {base_tree: <40 hex>, tree: [one entry]}: the entry's path is
	// this run's report, its mode "100644", its type "blob", its sha 40 hex.
	BodyTree
	// BodyCommit is {message, tree: <40 hex>, parents: [the audited commit]}.
	BodyCommit
	// BodyRef is {ref: "refs/heads/<this run's branch>", sha: <40 hex>}.
	BodyRef
	// BodyPull is {title, body, head: this run's branch, base: "main",
	// maintainer_can_modify: false, draft: false}.
	BodyPull
	// BodyIssue is exactly {title, body, labels: ["security"]}.
	BodyIssue
	// BodyAdvisoryCreate holds only summary, description, severity and
	// vulnerabilities: no state, cve_id, credits or collaborators.
	BodyAdvisoryCreate
	// BodyAdvisoryUpdate holds only summary, description, severity and state,
	// and state is present and exactly "draft".
	BodyAdvisoryUpdate
)

// Endpoint is one row of the fixed table of calls the publish program may
// make. Pattern is anchored at both ends when Allowed compiles it, and its
// placeholders are filled from the Scope, each value quoted:
//
//   - {repo}: repos/<owner>/<name> of the scope's repository;
//   - {run}: the scope's run ID;
//   - {branch}: security-audit/<date>-<run ID> of the scope;
//   - {commit}: the scope's audited commit;
//   - {sha}: any 40 lowercase hex;
//   - {tag}: a release tag v<major>.<minor>.<patch>;
//   - {digest}: sha256: and 64 lowercase hex;
//   - {ghsa}: a GHSA identifier.
type Endpoint struct {
	Method  string
	Pattern string
	Body    BodyRule
}

// Endpoints is every call the publish program and the artifact check may
// make, and nothing else: no PUT, no DELETE, no merge or review of a pull
// request, no CVE request, no tag, release or other branch.
var Endpoints = []Endpoint{
	{Method: "GET", Pattern: `{repo}/actions/runs/{run}/artifacts\?per_page=100`, Body: BodyNone},
	{Method: "GET", Pattern: `{repo}/releases/tags/{tag}`, Body: BodyNone},
	{Method: "GET", Pattern: `{repo}/attestations/{digest}\?per_page=100`, Body: BodyNone},
	{Method: "GET", Pattern: `{repo}/git/commits/{sha}`, Body: BodyNone},
	{Method: "GET", Pattern: `{repo}/git/ref/heads/{branch}`, Body: BodyNone},
	{Method: "POST", Pattern: `{repo}/git/blobs`, Body: BodyBlob},
	{Method: "POST", Pattern: `{repo}/git/trees`, Body: BodyTree},
	{Method: "POST", Pattern: `{repo}/git/commits`, Body: BodyCommit},
	{Method: "POST", Pattern: `{repo}/git/refs`, Body: BodyRef},
	{Method: "GET", Pattern: `{repo}/compare/{commit}\.\.\.{sha}`, Body: BodyNone},
	{Method: "POST", Pattern: `{repo}/pulls`, Body: BodyPull},
	{Method: "GET", Pattern: `{repo}/issues\?creator=shift2eu-audit%5Bbot%5D&state=all&per_page=100&page=[1-9][0-9]?`, Body: BodyNone},
	{Method: "POST", Pattern: `{repo}/issues`, Body: BodyIssue},
	{Method: "GET", Pattern: `{repo}/security-advisories\?per_page=100(&after=[A-Za-z0-9+/=_%-]{1,200})?`, Body: BodyNone},
	{Method: "POST", Pattern: `{repo}/security-advisories`, Body: BodyAdvisoryCreate},
	{Method: "PATCH", Pattern: `{repo}/security-advisories/{ghsa}`, Body: BodyAdvisoryUpdate},
}

// Allowed reports whether a call is in the endpoint table for the scope: its
// method and endpoint match one row, and its JSON body, nil for none, passes
// that row's body rule. A refusal wraps ErrEndpointRefused and names the rule,
// never the endpoint or the body.
func Allowed(_ Scope, _, _ string, _ []byte) error {
	return fmt.Errorf("endpoint check: %w", errNotImplemented)
}
