package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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

// issueEntry is one entry of the listing as the answer holds it. A pointer
// field the answer lacks is an answer that is not the listing.
type issueEntry struct {
	Title *string `json:"title"`
	User  *struct {
		Login *string `json:"login"`
		Type  *string `json:"type"`
	} `json:"user"`
	PullRequest json.RawMessage `json:"pull_request"`
}

// ListAuditIssues lists the audit App's issues, open and closed: GET
// R/issues?creator=shift2eu-audit%5Bbot%5D&state=all&per_page=100&page=N
// (row 12), from page 1 on while the answer's Link header has rel="next", at
// most MaxIssuePages pages. A next page after the tenth, a non-2xx status, an
// answer that is not a JSON array of issues (title a string, user an object
// with login and type strings) and a failed call wrap publish.ErrGitHub. No
// error names an issue.
func ListAuditIssues(ctx context.Context, c *Client) ([]Issue, error) {
	if c == nil {
		return nil, refuse(publish.CheckNoClient, "issue listing: no client", publish.ErrConfig)
	}

	var out []Issue

	for page := 1; ; page++ {
		if page > MaxIssuePages {
			return nil, refuse(publish.CheckListIssues, "issue listing: more than "+strconv.Itoa(MaxIssuePages)+" pages", publish.ErrGitHub)
		}

		resp, err := read(ctx, c, "repos/"+c.scope.Repository+
			"/issues?creator=shift2eu-audit%5Bbot%5D&state=all&per_page=100&page="+strconv.Itoa(page))
		if err != nil {
			return nil, publish.Refuse(publish.CheckListIssues, fmt.Errorf("issue listing: %w", err))
		}

		if resp.Status/100 != 2 {
			return nil, refuse(publish.CheckListIssues, "issue listing: the status is not 2xx", publish.ErrGitHub)
		}

		var entries []issueEntry
		if json.Unmarshal(resp.Body, &entries) != nil || entries == nil {
			return nil, refuse(publish.CheckListIssues, "issue listing: the answer is not the listing", publish.ErrGitHub)
		}

		for _, e := range entries {
			if e.Title == nil || e.User == nil || e.User.Login == nil || e.User.Type == nil {
				return nil, refuse(publish.CheckListIssues, "issue listing: an entry that is no issue", publish.ErrGitHub)
			}

			out = append(out, Issue{
				Title:       *e.Title,
				UserLogin:   *e.User.Login,
				UserType:    *e.User.Type,
				PullRequest: len(e.PullRequest) > 0 && string(e.PullRequest) != "null",
			})
		}

		if !strings.Contains(resp.Link, `rel="next"`) {
			return out, nil
		}
	}
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
func PlanIssues(prev *publish.Previous, existing []Issue, run publish.Run) ([]publish.IssueText, error) {
	var out []publish.IssueText

	for _, f := range prev.PublicFindings() {
		if f.Status != "new" && f.Status != "open" {
			continue
		}

		if filed(existing, string(f.Fingerprint)) {
			continue
		}

		issue, err := publish.RenderIssue(f, run)
		if err != nil {
			return nil, err
		}

		out = append(out, issue)
	}

	return out, nil
}

// filed reports whether one of the audit App's issues holds the fingerprint
// in its title.
func filed(existing []Issue, fp string) bool {
	for _, issue := range existing {
		if issue.PullRequest || issue.UserLogin != AuditBotLogin || issue.UserType != AuditBotType {
			continue
		}

		if titleHolds(issue.Title, fp) {
			return true
		}
	}

	return false
}

// titleHolds reports whether title contains fp neither preceded by a letter,
// a digit or - nor followed by a hex digit.
func titleHolds(title, fp string) bool {
	for i := 0; ; {
		j := strings.Index(title[i:], fp)
		if j < 0 {
			return false
		}

		start, end := i+j, i+j+len(fp)

		before := start == 0 || !isWordByte(title[start-1])
		after := end == len(title) || !isHex(title[end])

		if before && after {
			return true
		}

		i = start + 1
	}
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-'
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f'
}

// CreateIssue files one issue: POST R/issues (row 13) with exactly {title,
// body, labels: ["security"]}, through the one write helper: a non-2xx status
// wraps publish.ErrGitHub, and the call is never retried.
func CreateIssue(ctx context.Context, c *Client, issue publish.IssueText) error {
	if c == nil {
		return refuse(publish.CheckNoClient, "issue: no client", publish.ErrConfig)
	}

	if _, err := write(ctx, c, "repos/"+c.scope.Repository+"/issues", map[string]any{
		"title":  issue.Title,
		"body":   issue.Body,
		"labels": []string{"security"},
	}); err != nil {
		return publish.Refuse(publish.CheckCreateIssue, fmt.Errorf("issue: %w", err))
	}

	return nil
}
