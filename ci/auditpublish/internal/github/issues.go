package github

import (
	"context"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// The audit App's identity as the issue listing names it.
const (
	// AuditBotLogin is the audit App's login: the listing's creator and the
	// user.login of every issue PlanIssues matches.
	AuditBotLogin = "shift2eu-audit[bot]"
	// AuditBotType is the user.type of the audit App's issues.
	AuditBotType = "Bot"
)

// MaxIssuePages is the most pages ListAuditIssues reads: a listing that
// still has a next page after it fails closed (publish.ErrGitHub).
const MaxIssuePages = 10

// Issue is one entry of the audit App's issue listing, the fields PlanIssues
// reads and nothing else.
type Issue struct {
	Title     string
	UserLogin string
	UserType  string
	// PullRequest is set for an entry that is a pull request (the listing's
	// pull_request key is present).
	PullRequest bool
}

// ListAuditIssues lists the audit App's issues, open and closed: GET
// R/issues?creator=shift2eu-audit%5Bbot%5D&state=all&per_page=100&page=N
// (row 12), from page 1 on while the answer's Link header has rel="next", at
// most MaxIssuePages pages. A next page after the tenth, a non-2xx status, an
// answer that is not a JSON array of issues (title a string, user an object
// with login and type strings) and a failed call wrap publish.ErrGitHub. No
// error names an issue.
func ListAuditIssues(_ context.Context, _ *Client) ([]Issue, error) {
	return nil, errNotImplemented
}

// PlanIssues returns the issues a run files: one, rendered by
// publish.RenderIssue, for each finding of prev's findings table whose status
// is new or open and whose fingerprint no matching issue's title holds, in the
// table's order. A matching issue is one of existing that is no pull request
// and whose UserLogin is AuditBotLogin and UserType AuditBotType, open or
// closed; an outsider's issue carrying the fingerprint matches nothing. A
// title holds a fingerprint when it contains it neither preceded by a letter,
// digit or - nor followed by a hex digit. A nil prev files nothing. A finding
// RenderIssue refuses is its error.
func PlanIssues(_ *publish.Previous, _ []Issue, _ publish.Run) ([]publish.IssueText, error) {
	return nil, errNotImplemented
}

// CreateIssue files one issue: POST R/issues (row 13) with exactly {title,
// body, labels: ["security"]}, through the one write helper: a non-2xx status
// wraps publish.ErrGitHub, and the call is never retried.
func CreateIssue(_ context.Context, _ *Client, _ publish.IssueText) error {
	return errNotImplemented
}
