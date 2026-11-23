package github

import (
	"context"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// ReportPlan is what PublishReport files: the rendered report and the run's
// templates (the commit message, the pull request's title and body).
type ReportPlan struct {
	Report    publish.Rendered
	Templates publish.Templates
}

// PublishReport files the run's report as one commit on its own branch and
// opens its pull request, through the Git data API, in this order and with
// no call repeated:
//
//  1. GET R/git/ref/heads/<branch> (row 5): 404 goes on; 200 is
//     ErrBranchExists and nothing follows; any other status
//     publish.ErrGitHub.
//  2. GET R/git/commits/<audited commit> (row 4): the answer's tree.sha is the
//     base tree.
//  3. POST R/git/blobs (row 6) {content: the report's body, encoding:
//     "utf-8"}: the answer's sha is the blob.
//  4. POST R/git/trees (row 7) {base_tree: step 2's tree, tree: [{path: the
//     report's path, mode: "100644", type: "blob", sha: step 3's sha}]}.
//  5. POST R/git/commits (row 8) {message: the commit message, tree: step 4's
//     sha, parents: [the audited commit]}.
//  6. GET R/compare/<audited commit>...<step 5's sha> (row 10): the answer's
//     files must be exactly one, status "added", filename the report's path;
//     anything else is ErrReportDiff, and no ref and no pull request follow.
//  7. POST R/git/refs (row 9) {ref: refs/heads/<branch>, sha: step 5's sha}.
//  8. POST R/pulls (row 11) {title, body, head: the branch, base: "main",
//     maintainer_can_modify: false, draft: false}.
//
// Every write goes through one helper (write), which turns a non-2xx status
// into publish.ErrGitHub, so no write follows a refused one; a write is never
// retried, a timed-out one included. An answer without its 40-hex sha (or
// tree.sha) is publish.ErrGitHub. A plan whose path, branch or texts the
// endpoint table refuses wraps ErrEndpointRefused, before gh runs.
func PublishReport(_ context.Context, _ *Client, _ ReportPlan) error {
	return errNotImplemented
}
