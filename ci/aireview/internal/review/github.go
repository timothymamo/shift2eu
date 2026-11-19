package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// gitHubAPIVersion is the X-GitHub-Api-Version every request sends.
const gitHubAPIVersion = "2022-11-28"

// The Accept headers: JSON for every call but the diff's.
const (
	acceptJSON = "application/vnd.github+json"
	acceptDiff = "application/vnd.github.diff"
)

// maxJSONBytes caps a JSON answer the client reads from GitHub. A page of 100
// files or reviews is far below it.
const maxJSONBytes = 16 << 20

// perPage is the page size of the listings.
const perPage = 100

// maxPages caps a listing, so a misbehaving server cannot keep the run paging:
// GitHub lists at most 3,000 files of a pull request.
const maxPages = 50

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
	var raw struct {
		Number int     `json:"number"`
		Title  string  `json:"title"`
		Body   *string `json:"body"`
		Draft  bool    `json:"draft"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
		} `json:"base"`
	}

	if err := g.getJSON(ctx, g.repoPath("pulls", strconv.Itoa(number)), nil, &raw); err != nil {
		return PullRequest{}, fmt.Errorf("read pull request %d: %w", number, err)
	}

	if raw.Number != number || !isCommitSHA(raw.Head.SHA) || !isCommitSHA(raw.Base.SHA) {
		return PullRequest{}, fmt.Errorf("read pull request %d: %w: no number, head or base commit in the answer", number, ErrGitHub)
	}

	pr := PullRequest{
		Number:  raw.Number,
		Title:   raw.Title,
		Author:  raw.User.Login,
		Draft:   raw.Draft,
		HeadSHA: raw.Head.SHA,
		BaseSHA: raw.Base.SHA,
	}
	if raw.Body != nil {
		pr.Body = *raw.Body
	}

	return pr, nil
}

// Diff reads the unified diff of pr, pinned to its SHAs:
// GET /repos/{repository}/compare/{BaseSHA}...{HeadSHA} with Accept
// application/vnd.github.diff. It reads at most MaxRawDiffBytes+1 bytes of the
// body and returns an error wrapping ErrDiffTooLarge when the diff is longer
// than MaxRawDiffBytes. It measures nothing against MaxDiffBytes: Run does,
// once FilterDiff has left the generated and golden files out (issue #324).
func (g *GitHub) Diff(ctx context.Context, pr PullRequest) (string, error) {
	if !isCommitSHA(pr.BaseSHA) || !isCommitSHA(pr.HeadSHA) {
		return "", fmt.Errorf("read the diff of pull request %d: %w: no base or head commit", pr.Number, ErrGitHub)
	}

	resp, err := g.do(ctx, http.MethodGet, g.repoPath("compare", pr.BaseSHA+"..."+pr.HeadSHA), nil, acceptDiff, nil)
	if err != nil {
		return "", fmt.Errorf("read the diff of pull request %d: %w", pr.Number, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read the diff of pull request %d: %w: status %d", pr.Number, ErrGitHub, resp.StatusCode)
	}

	diff, err := io.ReadAll(io.LimitReader(resp.Body, MaxRawDiffBytes+1))
	if err != nil {
		return "", fmt.Errorf("read the diff of pull request %d: %w: %w", pr.Number, ErrGitHub, err)
	}

	if len(diff) > MaxRawDiffBytes {
		return "", fmt.Errorf("pull request %d: %w: more than %d bytes before generated and golden files are left out",
			pr.Number, ErrDiffTooLarge, MaxRawDiffBytes)
	}

	return string(diff), nil
}

// ChangedFiles reads the pull request's file list, every page:
// GET /repos/{repository}/pulls/{number}/files?per_page=100&page=N.
func (g *GitHub) ChangedFiles(ctx context.Context, number int) ([]ChangedFile, error) {
	type rawFile struct {
		Filename  string `json:"filename"`
		Status    string `json:"status"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	}

	raws, err := listAll[rawFile](ctx, g, g.repoPath("pulls", strconv.Itoa(number), "files"))
	if err != nil {
		return nil, fmt.Errorf("read the files of pull request %d: %w", number, err)
	}

	files := make([]ChangedFile, 0, len(raws))
	for _, f := range raws {
		files = append(files, ChangedFile{Path: f.Filename, Status: f.Status, Additions: f.Additions, Deletions: f.Deletions})
	}

	return files, nil
}

// Reviews reads the pull request's reviews, every page:
// GET /repos/{repository}/pulls/{number}/reviews?per_page=100&page=N.
func (g *GitHub) Reviews(ctx context.Context, number int) ([]PostedReview, error) {
	type rawReview struct {
		ID   int64 `json:"id"`
		User *struct {
			Login string `json:"login"`
		} `json:"user"`
		State    string `json:"state"`
		CommitID string `json:"commit_id"`
	}

	raws, err := listAll[rawReview](ctx, g, g.repoPath("pulls", strconv.Itoa(number), "reviews"))
	if err != nil {
		return nil, fmt.Errorf("read the reviews of pull request %d: %w", number, err)
	}

	reviews := make([]PostedReview, 0, len(raws))
	for _, r := range raws {
		posted := PostedReview{ID: r.ID, State: r.State, CommitID: r.CommitID}
		if r.User != nil {
			posted.Login = r.User.Login
		}

		reviews = append(reviews, posted)
	}

	return reviews, nil
}

// PostReview posts r on pull request number:
// POST /repos/{repository}/pulls/{number}/reviews with commit_id, body, event
// and comments (path, line, side RIGHT, body). It returns an error wrapping
// ErrEventRefused, and makes no HTTP call, unless r.Event is exactly
// EventComment or EventRequestChanges.
func (g *GitHub) PostReview(ctx context.Context, number int, r Review) error {
	// The one place the reviewer writes a review: it never approves (ADR 0033).
	if r.Event != EventComment && r.Event != EventRequestChanges {
		return fmt.Errorf("post the review of pull request %d: %w (got %q)", number, ErrEventRefused, string(r.Event))
	}

	type comment struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Side string `json:"side"`
		Body string `json:"body"`
	}

	payload := struct {
		CommitID string    `json:"commit_id"`
		Body     string    `json:"body"`
		Event    string    `json:"event"`
		Comments []comment `json:"comments"`
	}{
		CommitID: r.CommitID,
		Body:     r.Body,
		Event:    string(r.Event),
		Comments: make([]comment, 0, len(r.Comments)),
	}

	for _, c := range r.Comments {
		payload.Comments = append(payload.Comments, comment{Path: c.Path, Line: c.Line, Side: "RIGHT", Body: c.Body})
	}

	if err := g.sendJSON(ctx, http.MethodPost, g.repoPath("pulls", strconv.Itoa(number), "reviews"), payload); err != nil {
		return fmt.Errorf("post the review of pull request %d: %w", number, err)
	}

	return nil
}

// DismissReview dismisses review id of pull request number with message:
// PUT /repos/{repository}/pulls/{number}/reviews/{id}/dismissals with message
// and event DISMISS.
func (g *GitHub) DismissReview(ctx context.Context, number int, id int64, message string) error {
	payload := struct {
		Message string `json:"message"`
		Event   string `json:"event"`
	}{Message: message, Event: "DISMISS"}

	path := g.repoPath("pulls", strconv.Itoa(number), "reviews", strconv.FormatInt(id, 10), "dismissals")
	if err := g.sendJSON(ctx, http.MethodPut, path, payload); err != nil {
		return fmt.Errorf("dismiss review %d of pull request %d: %w", id, number, err)
	}

	return nil
}

// listAll reads every page of a listing at path, perPage items a page, until a
// page holds fewer.
func listAll[T any](ctx context.Context, g *GitHub, path string) ([]T, error) {
	var all []T

	for page := 1; page <= maxPages; page++ {
		query := url.Values{"per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(page)}}

		var items []T
		if err := g.getJSON(ctx, path, query, &items); err != nil {
			return nil, err
		}

		all = append(all, items...)

		if len(items) < perPage {
			return all, nil
		}
	}

	return nil, fmt.Errorf("%w: more than %d pages", ErrGitHub, maxPages)
}

// getJSON GETs path with query and decodes the JSON answer into v.
func (g *GitHub) getJSON(ctx context.Context, path string, query url.Values, v any) error {
	resp, err := g.do(ctx, http.MethodGet, path, query, acceptJSON, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: GET %s: status %d", ErrGitHub, path, resp.StatusCode)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(v); err != nil {
		return fmt.Errorf("%w: GET %s: decode the answer: %w", ErrGitHub, path, err)
	}

	return nil
}

// sendJSON sends payload as JSON to path with method and wants a 200 answer.
func (g *GitHub) sendJSON(ctx context.Context, method, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode the request: %w", err)
	}

	resp, err := g.do(ctx, method, path, nil, acceptJSON, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxJSONBytes))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s %s: status %d", ErrGitHub, method, path, resp.StatusCode)
	}

	return nil
}

// do sends one request with the client's headers. body, when not nil, is sent
// as JSON. A transport error wraps ErrGitHub and never names the token.
func (g *GitHub) do(ctx context.Context, method, path string, query url.Values, accept string, body []byte) (*http.Response, error) {
	target := g.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, fmt.Errorf("%w: build %s %s: %w", ErrGitHub, method, path, err)
	}

	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", gitHubAPIVersion)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", ErrGitHub, method, path, err)
	}

	return resp, nil
}

// repoPath returns /repos/{repository}/ followed by parts, each escaped.
func (g *GitHub) repoPath(parts ...string) string {
	path := "/repos/" + g.repository

	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}

	return path
}

// isCommitSHA reports whether s is 40 lower-case hex digits, as GitHub names a
// commit.
func isCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}

	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}

	return true
}
