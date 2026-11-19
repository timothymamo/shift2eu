package review_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// seenRequest is what the test server saw of the one request a call made.
type seenRequest struct {
	method        string
	path          string
	authorization string
	contentType   string
	accept        string
	apiVersion    string
	body          []byte
}

// seenReview is the JSON of a posted review, as the GitHub API reads it.
type seenReview struct {
	CommitID string        `json:"commit_id"`
	Body     string        `json:"body"`
	Event    string        `json:"event"`
	Comments []seenComment `json:"comments"`
}

// seenComment is one inline comment of a posted review: a path, a line of the
// new side of the diff, and a body.
type seenComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

// TestPostReviewNeverApproves pins ADR 0033's rule that the poster supports
// exactly COMMENT and REQUEST_CHANGES: any other event is refused with
// ErrEventRefused before the poster makes an HTTP call. The GitHub API would
// accept APPROVE; the reviewer must never send it, whatever the model answers
// and whatever the program asks for.
func TestPostReviewNeverApproves(t *testing.T) {
	t.Parallel()

	refused := []struct {
		name  string
		event review.Event
	}{
		{name: "APPROVE", event: review.Event("APPROVE")},
		{name: "an empty event", event: review.Event("")},
		{name: "COMMENT in lower case", event: review.Event("comment")},
		{name: "REQUEST_CHANGES in lower case", event: review.Event("request_changes")},
		{name: "DISMISS", event: review.Event("DISMISS")},
		{name: "an unknown event", event: review.Event("LGTM")},
	}

	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var requests int

			srv := server(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++

				w.WriteHeader(http.StatusOK)
			})

			gh := review.NewGitHub(srv.URL, "owner/name", testToken, srv.Client())

			err := gh.PostReview(t.Context(), 7, review.Review{
				CommitID: headSHA,
				Event:    tc.event,
				Body:     "a summary",
			})
			if !errors.Is(err, review.ErrEventRefused) {
				t.Fatalf("PostReview with the event %q = %v, want an error wrapping ErrEventRefused", tc.event, err)
			}

			if requests != 0 {
				t.Errorf("PostReview with the event %q made %d HTTP requests, want 0: the refusal comes before any call", tc.event, requests)
			}
		})
	}
}

// TestPostReviewPostsOneReviewOnTheHeadCommit pins the request of a review the
// poster does send: POST /repos/{owner}/{name}/pulls/{n}/reviews with the head
// commit it reviewed, its body, its event and its inline comments on the right
// side of the diff, authorized with the token and pinned to one API version.
func TestPostReviewPostsOneReviewOnTheHeadCommit(t *testing.T) {
	t.Parallel()

	for _, event := range []review.Event{review.EventComment, review.EventRequestChanges} {
		t.Run(string(event), func(t *testing.T) {
			t.Parallel()

			seen := make(chan seenRequest, 1)

			srv := server(t, func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read the request body: %v", err)
				}

				seen <- seenRequest{
					method:        r.Method,
					path:          r.URL.Path,
					authorization: r.Header.Get("Authorization"),
					contentType:   r.Header.Get("Content-Type"),
					accept:        r.Header.Get("Accept"),
					apiVersion:    r.Header.Get("X-GitHub-Api-Version"),
					body:          body,
				}

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": 1}`))
			})

			gh := review.NewGitHub(srv.URL, "owner/name", testToken, srv.Client())

			want := review.Review{
				CommitID: headSHA,
				Event:    event,
				Body:     "**Summary**: the change does what the issue asks.",
				Comments: []review.InlineComment{
					{Path: "internal/x/y.go", Line: 2, Body: "**blocker** (correctness): the loop stops one short"},
					{Path: "internal/x/z.go", Line: 11, Body: "**nit** (readability): name it"},
				},
			}
			if err := gh.PostReview(t.Context(), 7, want); err != nil {
				t.Fatalf("PostReview: %v", err)
			}

			var got seenRequest

			select {
			case got = <-seen:
			default:
				t.Fatal("PostReview made no HTTP request")
			}

			if got.method != http.MethodPost {
				t.Errorf("the request's method is %s, want POST", got.method)
			}

			if wantPath := "/repos/owner/name/pulls/7/reviews"; got.path != wantPath {
				t.Errorf("the request's path is %s, want %s", got.path, wantPath)
			}

			if !strings.Contains(got.authorization, testToken) {
				t.Errorf("the Authorization header is %q, want it to carry the token %q", got.authorization, testToken)
			}

			if !strings.Contains(got.contentType, "application/json") {
				t.Errorf("the Content-Type header is %q, want application/json", got.contentType)
			}

			if !strings.Contains(got.accept, "application/vnd.github+json") {
				t.Errorf("the Accept header is %q, want application/vnd.github+json", got.accept)
			}

			if got.apiVersion != "2022-11-28" {
				t.Errorf("the X-GitHub-Api-Version header is %q, want 2022-11-28", got.apiVersion)
			}

			var payload seenReview
			if err := json.Unmarshal(got.body, &payload); err != nil {
				t.Fatalf("decode the posted review %q: %v", got.body, err)
			}

			wantPayload := seenReview{
				CommitID: headSHA,
				Body:     want.Body,
				Event:    string(event),
				Comments: []seenComment{
					{Path: "internal/x/y.go", Line: 2, Side: "RIGHT", Body: want.Comments[0].Body},
					{Path: "internal/x/z.go", Line: 11, Side: "RIGHT", Body: want.Comments[1].Body},
				},
			}
			if diff := cmp.Diff(wantPayload, payload); diff != "" {
				t.Errorf("the posted review (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDiffReadsUpToTheRawLimit pins the client's read of the diff (ADR 0033,
// issue #324): it reads a whole diff up to MaxRawDiffBytes, so the run can leave
// the generated and golden files out and measure what remains, and a raw diff it
// cannot read whole fails the run with ErrDiffTooLarge instead of being filtered
// in part, since a section cut in half is no diff. MaxRawDiffBytes leaves room
// above MaxDiffBytes: a diff over MaxDiffBytes is read whole, and the run's size
// check on what it kept is the one that refuses it. The client reads the diff of
// the compare of the pull request's two SHAs, so the review is of the head it
// read.
func TestDiffReadsUpToTheRawLimit(t *testing.T) {
	t.Parallel()

	if review.MaxRawDiffBytes <= review.MaxDiffBytes {
		t.Fatalf("MaxRawDiffBytes = %d, want more than MaxDiffBytes = %d: the run reads a whole diff and leaves the generated files out before it measures what remains",
			review.MaxRawDiffBytes, review.MaxDiffBytes)
	}

	cases := []struct {
		name    string
		size    int
		wantErr error
	}{
		{name: "a diff exactly at the raw limit is read", size: review.MaxRawDiffBytes},
		{name: "a diff one byte over the raw limit fails the run", size: review.MaxRawDiffBytes + 1, wantErr: review.ErrDiffTooLarge},
		{name: "a diff over MaxDiffBytes is read whole, for the run to filter and measure", size: review.MaxDiffBytes + 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			seen := make(chan string, 1)
			body := strings.Repeat("x", tc.size)

			srv := server(t, func(w http.ResponseWriter, r *http.Request) {
				seen <- r.URL.Path

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
			})

			gh := review.NewGitHub(srv.URL, "owner/name", testToken, srv.Client())

			pr := review.PullRequest{Number: 7, BaseSHA: baseSHA, HeadSHA: headSHA}

			diff, err := gh.Diff(t.Context(), pr)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Diff of %d bytes = %v, want an error wrapping %v", tc.size, err, tc.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("Diff of %d bytes, all of it read whole: %v, want it read: the raw limit refuses a longer diff only", tc.size, err)
				}

				if len(diff) != tc.size {
					t.Errorf("Diff returned %d bytes, want %d", len(diff), tc.size)
				}
			}

			select {
			case path := <-seen:
				if want := "/repos/owner/name/compare/" + baseSHA + "..." + headSHA; path != want {
					t.Errorf("Diff read %s, want %s: the diff is pinned to the head it reviews", path, want)
				}
			default:
				t.Fatal("Diff made no HTTP request")
			}
		})
	}
}

// listingPage returns one page of a listing of 201 items: full pages of 100
// while fewer than 201 items are handed out, then the one item that ends the
// listing, as GitHub answers a listing's last page.
func listingPage(page int, item func(i int) string) []string {
	if page > 2 {
		return []string{item(200)}
	}

	items := make([]string, 0, 100)
	for i := range 100 {
		items = append(items, item((page-1)*100+i))
	}

	return items
}

// TestGitHubReadsEveryPageOfAListing pins the pagination of the file and review
// listings: per_page=100 and page 1, 2, 3 upwards, stopping on the first page
// that holds fewer than 100 items, so a pull request with more than one page of
// files or reviews is read whole and no run keeps paging a server that always
// answers a full page.
func TestGitHubReadsEveryPageOfAListing(t *testing.T) {
	t.Parallel()

	const (
		filesPath   = "/repos/owner/name/pulls/7/files"
		reviewsPath = "/repos/owner/name/pulls/7/reviews"
	)

	cases := []struct {
		name string
		path string
		page func(page int) []string
		read func(ctx context.Context, gh *review.GitHub) (int, error)
		want int
	}{
		{
			name: "the file list",
			path: filesPath,
			page: func(page int) []string {
				return listingPage(page, func(i int) string {
					return fmt.Sprintf(`{"filename":"internal/x/file-%03d.go","status":"modified","additions":1,"deletions":0}`, i)
				})
			},
			read: func(ctx context.Context, gh *review.GitHub) (int, error) {
				files, err := gh.ChangedFiles(ctx, 7)

				return len(files), err
			},
			want: 201,
		},
		{
			name: "the review list",
			path: reviewsPath,
			page: func(page int) []string {
				return listingPage(page, func(i int) string {
					return fmt.Sprintf(`{"id":%d,"user":{"login":"reviewer-%03d"},"state":"COMMENTED","commit_id":"%s"}`, i+1, i, headSHA)
				})
			},
			read: func(ctx context.Context, gh *review.GitHub) (int, error) {
				reviews, err := gh.Reviews(ctx, 7)

				return len(reviews), err
			},
			want: 201,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu      sync.Mutex
				queries []string
			)

			srv := server(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("the listing read %s, want %s", r.URL.Path, tc.path)
				}

				query := r.URL.Query()

				mu.Lock()
				queries = append(queries, query.Get("per_page")+" "+query.Get("page"))
				mu.Unlock()

				page, err := strconv.Atoi(query.Get("page"))
				if err != nil {
					page = 0
				}

				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "["+strings.Join(tc.page(page), ",")+"]")
			})

			gh := review.NewGitHub(srv.URL, "owner/name", testToken, srv.Client())

			got, err := tc.read(t.Context(), gh)
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}

			if got != tc.want {
				t.Errorf("the listing gave %d items, want %d: every page of it is read", got, tc.want)
			}

			wantQueries := []string{"100 1", "100 2", "100 3"}

			mu.Lock()
			defer mu.Unlock()

			if diff := cmp.Diff(wantQueries, queries); diff != "" {
				t.Errorf("the listing's page requests, as per_page and page (-want +got):\n%s", diff)
			}
		})
	}
}
