package review

import (
	"context"
	"net/http"
)

// PostedReview is a review already on the pull request, as the reviews listing
// gives it: its ID, its author's login, its state (APPROVED, CHANGES_REQUESTED,
// COMMENTED, DISMISSED, PENDING) and the commit it reviewed.
type PostedReview struct {
	ID       int64
	Login    string
	State    string
	CommitID string
}

// GitHub is the client of the GitHub REST API for one repository. It sends
// Authorization: Bearer <token>, Accept: application/vnd.github+json (the
// diff: application/vnd.github.diff) and X-GitHub-Api-Version: 2022-11-28 on
// every request, and never puts the token in an error.
type GitHub struct {
	baseURL    string
	repository string
	token      string
	client     *http.Client
}

// NewGitHub returns a client of the API at baseURL (https://api.github.com in
// production, an httptest server's URL in tests) for repository, owner/name,
// with token. client carries the timeout.
func NewGitHub(baseURL, repository, token string, client *http.Client) *GitHub {
	return &GitHub{baseURL: baseURL, repository: repository, token: token, client: client}
}

// PullRequest reads pull request number: GET /repos/{repository}/pulls/{number}.
func (g *GitHub) PullRequest(ctx context.Context, number int) (PullRequest, error) {
	_, _ = ctx, number // the skeleton's stub (step 1)

	return PullRequest{}, errNotImplemented
}

// Diff reads the unified diff of pr, pinned to its SHAs:
// GET /repos/{repository}/compare/{BaseSHA}...{HeadSHA} with Accept
// application/vnd.github.diff. It reads at most MaxDiffBytes+1 bytes of the
// body and returns an error wrapping ErrDiffTooLarge when the diff is longer
// than MaxDiffBytes.
func (g *GitHub) Diff(ctx context.Context, pr PullRequest) (string, error) {
	_, _ = ctx, pr // the skeleton's stub (step 1)

	return "", errNotImplemented
}

// ChangedFiles reads the pull request's file list, every page:
// GET /repos/{repository}/pulls/{number}/files?per_page=100&page=N.
func (g *GitHub) ChangedFiles(ctx context.Context, number int) ([]ChangedFile, error) {
	_, _ = ctx, number // the skeleton's stub (step 1)

	return nil, errNotImplemented
}

// Reviews reads the pull request's reviews, every page:
// GET /repos/{repository}/pulls/{number}/reviews?per_page=100&page=N.
func (g *GitHub) Reviews(ctx context.Context, number int) ([]PostedReview, error) {
	_, _ = ctx, number // the skeleton's stub (step 1)

	return nil, errNotImplemented
}

// PostReview posts r on pull request number:
// POST /repos/{repository}/pulls/{number}/reviews with commit_id, body, event
// and comments (path, line, side RIGHT, body). It returns an error wrapping
// ErrEventRefused, and makes no HTTP call, unless r.Event is exactly
// EventComment or EventRequestChanges.
func (g *GitHub) PostReview(ctx context.Context, number int, r Review) error {
	_, _, _ = ctx, number, r // the skeleton's stub (step 1)

	return errNotImplemented
}

// DismissReview dismisses review id of pull request number with message:
// PUT /repos/{repository}/pulls/{number}/reviews/{id}/dismissals with message
// and event DISMISS.
func (g *GitHub) DismissReview(ctx context.Context, number int, id int64, message string) error {
	_, _, _, _ = ctx, number, id, message // the skeleton's stub (step 1)

	return errNotImplemented
}
