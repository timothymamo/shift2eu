package github

import (
	"context"
	"encoding/json"
	"fmt"

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
//  6. GET R/compare/<audited commit>...<step 5's sha> (row 10): the answer
//     must be status "ahead", ahead_by 1, behind_by 0, merge_base_commit.sha
//     the audited commit, and its files exactly one, status "added", filename
//     the report's path; anything else is ErrReportDiff, and no ref and no
//     pull request follow.
//  7. POST R/git/refs (row 9) {ref: refs/heads/<branch>, sha: step 5's sha}.
//  8. POST R/pulls (row 11) {title, body, head: the branch, base: "main",
//     maintainer_can_modify: false, draft: false}.
//
// The compare runs before the ref: the new commit exists as an object before
// any ref names it, so no push-triggered workflow ever runs a commit that is
// more than the report.
//
// Every write goes through one helper (write), which turns a non-2xx status
// into publish.ErrGitHub, so no write follows a refused one; a write is never
// retried, a timed-out one included. An answer without its 40-hex sha (or
// tree.sha) is publish.ErrGitHub. A plan whose path, branch or texts the
// endpoint table refuses wraps ErrEndpointRefused, before gh runs.
func PublishReport(ctx context.Context, c *Client, plan ReportPlan) error {
	if c == nil || c.scope.Commit == "" {
		return refuse(publish.CheckNoClient, "report: no publish client", publish.ErrConfig)
	}

	repo := "repos/" + c.scope.Repository
	branch := branchOf(c.scope)

	if plan.Report.Path != reportPathOf(c.scope) || plan.Templates.Branch != branch {
		return fmt.Errorf("report: the plan's path or branch is not the run's: %w", ErrEndpointRefused)
	}

	if err := checkBranchAbsent(ctx, c, repo+"/git/ref/heads/"+branch); err != nil {
		return publish.Refuse(publish.CheckReadReportBranch, err)
	}

	baseTree, err := auditedTree(ctx, c, repo+"/git/commits/"+c.scope.Commit)
	if err != nil {
		return publish.Refuse(publish.CheckReadAuditedCommit, err)
	}

	blob, err := writeSHA(ctx, c, repo+"/git/blobs", map[string]any{"content": plan.Report.Body, "encoding": "utf-8"})
	if err != nil {
		return publish.Refuse(publish.CheckWriteBlob, fmt.Errorf("report: the blob: %w", err))
	}

	tree, err := writeSHA(ctx, c, repo+"/git/trees", map[string]any{
		"base_tree": baseTree,
		"tree":      []any{map[string]any{"path": plan.Report.Path, "mode": "100644", "type": "blob", "sha": blob}},
	})
	if err != nil {
		return publish.Refuse(publish.CheckWriteTree, fmt.Errorf("report: the tree: %w", err))
	}

	commit, err := writeSHA(ctx, c, repo+"/git/commits", map[string]any{
		"message": plan.Templates.CommitMessage,
		"tree":    tree,
		"parents": []string{c.scope.Commit},
	})
	if err != nil {
		return publish.Refuse(publish.CheckWriteCommit, fmt.Errorf("report: the commit: %w", err))
	}

	if err := checkReportDiff(ctx, c, repo+"/compare/"+c.scope.Commit+"..."+commit, c.scope.Commit, plan.Report.Path); err != nil {
		return publish.Refuse(publish.CheckCompare, err)
	}

	if _, err := write(ctx, c, repo+"/git/refs", map[string]any{"ref": "refs/heads/" + branch, "sha": commit}); err != nil {
		return publish.Refuse(publish.CheckWriteBranch, fmt.Errorf("report: the ref: %w", err))
	}

	if _, err := write(ctx, c, repo+"/pulls", map[string]any{
		"title":                 plan.Templates.PRTitle,
		"body":                  plan.Templates.PRBody,
		"head":                  branch,
		"base":                  "main",
		"maintainer_can_modify": false,
		"draft":                 false,
	}); err != nil {
		return publish.Refuse(publish.CheckOpenPull, fmt.Errorf("report: the pull request: %w", err))
	}

	return nil
}

// send is the one helper every write goes through: one call of the method,
// never repeated, whose non-2xx status, a timed-out call included, wraps
// publish.ErrGitHub. It returns the answer, so a caller can tell one refusal
// from another by its status. No error carries the answer.
func send(ctx context.Context, c *Client, method, endpoint string, body any) (Response, error) {
	resp, err := c.Call(ctx, Request{Method: method, Endpoint: endpoint, Body: body})
	if err != nil {
		return Response{}, fmt.Errorf("write: %w", err)
	}

	if resp.Status/100 != 2 {
		return resp, fmt.Errorf("write: the status is not 2xx: %w", publish.ErrGitHub)
	}

	return resp, nil
}

// write is send with POST: it returns the answer's body.
func write(ctx context.Context, c *Client, endpoint string, body any) ([]byte, error) {
	resp, err := send(ctx, c, "POST", endpoint, body)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// writeSHA is write for a Git data API object: the answer's sha, 40 lowercase
// hex, or publish.ErrGitHub.
func writeSHA(ctx context.Context, c *Client, endpoint string, body any) (string, error) {
	answer, err := write(ctx, c, endpoint, body)
	if err != nil {
		return "", err
	}

	var object struct {
		SHA string `json:"sha"`
	}

	if json.Unmarshal(answer, &object) != nil || !sha40.MatchString(object.SHA) {
		return "", fmt.Errorf("write: the answer has no 40-hex sha: %w", publish.ErrGitHub)
	}

	return object.SHA, nil
}

// read is one GET whose status its caller decides on.
func read(ctx context.Context, c *Client, endpoint string) (Response, error) {
	resp, err := c.Call(ctx, Request{Method: "GET", Endpoint: endpoint})
	if err != nil {
		return Response{}, fmt.Errorf("read: %w", err)
	}

	return resp, nil
}

// checkBranchAbsent looks the report's branch up: 404 is no branch, 200 an
// existing one (ErrBranchExists), any other status publish.ErrGitHub.
func checkBranchAbsent(ctx context.Context, c *Client, endpoint string) error {
	resp, err := read(ctx, c, endpoint)
	if err != nil {
		return fmt.Errorf("report: the branch lookup: %w", err)
	}

	switch resp.Status {
	case 404:
		return nil
	case 200:
		return fmt.Errorf("report: the branch lookup: %w", ErrBranchExists)
	default:
		return fmt.Errorf("report: the branch lookup's status is neither 200 nor 404: %w", publish.ErrGitHub)
	}
}

// auditedTree reads the audited commit and returns its tree's sha: an answer
// that is not the commit, or without a 40-hex tree.sha, is publish.ErrGitHub.
func auditedTree(ctx context.Context, c *Client, endpoint string) (string, error) {
	resp, err := read(ctx, c, endpoint)
	if err != nil {
		return "", fmt.Errorf("report: the audited commit: %w", err)
	}

	var commit struct {
		SHA  string `json:"sha"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}

	if resp.Status != 200 || json.Unmarshal(resp.Body, &commit) != nil ||
		commit.SHA != c.scope.Commit || !sha40.MatchString(commit.Tree.SHA) {
		return "", fmt.Errorf("report: the audited commit's answer has no 40-hex tree: %w", publish.ErrGitHub)
	}

	return commit.Tree.SHA, nil
}

// checkReportDiff compares the audited commit with the report commit: the
// report commit must be one commit ahead of the audited one and none behind
// (status ahead, ahead_by 1, behind_by 0, the merge base the audited commit),
// and its files exactly one, the report, added. Any other answer of 200 is
// ErrReportDiff; another status or a body that is no comparison
// publish.ErrGitHub.
func checkReportDiff(ctx context.Context, c *Client, endpoint, audited, path string) error {
	resp, err := read(ctx, c, endpoint)
	if err != nil {
		return fmt.Errorf("report: the compare: %w", err)
	}

	if resp.Status != 200 {
		return fmt.Errorf("report: the compare's status is not 200: %w", publish.ErrGitHub)
	}

	var comparison struct {
		Status          string `json:"status"`
		AheadBy         *int   `json:"ahead_by"`
		BehindBy        *int   `json:"behind_by"`
		MergeBaseCommit struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
		Files *[]struct {
			Filename string `json:"filename"`
			Status   string `json:"status"`
		} `json:"files"`
	}

	if json.Unmarshal(resp.Body, &comparison) != nil {
		return fmt.Errorf("report: the compare's answer is no comparison: %w", publish.ErrGitHub)
	}

	if comparison.Status != "ahead" || comparison.AheadBy == nil || *comparison.AheadBy != 1 ||
		comparison.BehindBy == nil || *comparison.BehindBy != 0 || comparison.MergeBaseCommit.SHA != audited {
		return fmt.Errorf("report: the commit is not one commit on the audited one: %w", ErrReportDiff)
	}

	if comparison.Files == nil || len(*comparison.Files) != 1 {
		return fmt.Errorf("report: the commit is not exactly one file: %w", ErrReportDiff)
	}

	if f := (*comparison.Files)[0]; f.Filename != path || f.Status != "added" {
		return fmt.Errorf("report: the commit is not the report added: %w", ErrReportDiff)
	}

	return nil
}
