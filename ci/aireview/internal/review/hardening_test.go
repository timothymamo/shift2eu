package review_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// TestNewHTTPClientRefusesRedirects pins the client both APIs are built with: it
// follows no redirect at all, so a 3xx answer that points elsewhere fails the
// call with the API's own error and the other host never sees the request, nor
// the x-api-key header and the pull request's text it carries (ADR 0033). A
// followed redirect would hand the model key and the diff to whatever host a
// response names.
func TestNewHTTPClientRefusesRedirects(t *testing.T) {
	t.Parallel()

	redirects := []struct {
		name string
		code int
	}{
		{name: "307", code: http.StatusTemporaryRedirect},
		{name: "308", code: http.StatusPermanentRedirect},
		{name: "302", code: http.StatusFound},
	}

	for _, redirect := range redirects {
		t.Run("the model API answers "+redirect.name, func(t *testing.T) {
			t.Parallel()

			var reached int

			elsewhere := server(t, func(w http.ResponseWriter, _ *http.Request) {
				reached++

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn"}`))
			})

			srv := server(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, elsewhere.URL, redirect.code)
			})

			model := review.NewAnthropic(srv.URL, testKey, review.NewHTTPClient(10*time.Second))

			_, err := model.Ask(t.Context(), review.Prompt{System: "the instructions", User: "the pull request"})
			if !errors.Is(err, review.ErrModel) {
				t.Fatalf("Ask of a %d answer = %v, want an error wrapping ErrModel: the client refuses the redirect", redirect.code, err)
			}

			if reached != 0 {
				t.Errorf("the server the %d answer pointed at got %d requests, want 0: a followed redirect sends the x-api-key and the diff to another host", redirect.code, reached)
			}
		})

		t.Run("GitHub answers "+redirect.name, func(t *testing.T) {
			t.Parallel()

			var reached int

			elsewhere := server(t, func(w http.ResponseWriter, _ *http.Request) {
				reached++

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"number":7,"title":"Add x","head":{"sha":"` + headSHA + `"},"base":{"sha":"` + baseSHA + `"}}`))
			})

			srv := server(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, elsewhere.URL, redirect.code)
			})

			gh := review.NewGitHub(srv.URL, "owner/name", testToken, review.NewHTTPClient(10*time.Second))

			if _, err := gh.PullRequest(t.Context(), 7); !errors.Is(err, review.ErrGitHub) {
				t.Fatalf("PullRequest of a %d answer = %v, want an error wrapping ErrGitHub: the client refuses the redirect", redirect.code, err)
			}

			if reached != 0 {
				t.Errorf("the server the %d answer pointed at got %d requests, want 0: a followed redirect sends the token to another host", redirect.code, reached)
			}
		})
	}

	t.Run("the timeout is the client's", func(t *testing.T) {
		t.Parallel()

		if got := review.NewHTTPClient(30 * time.Second).Timeout; got != 30*time.Second {
			t.Errorf("NewHTTPClient(30s).Timeout = %v, want 30s: the client carries the timeout both APIs are given", got)
		}
	})
}

// TestAnnotationDataEscapesWorkflowCommandData pins the escaping of every text
// the program prints as a workflow command's data (GitHub's rule: % as %25, CR
// as %0D, LF as %0A): an error or an answer that holds a line break or a
// percent sign stays on the annotation's one line and cannot start a command of
// its own, whoever wrote it.
func TestAnnotationDataEscapesWorkflowCommandData(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "a text without CR, LF or % is unchanged",
			in:   "the model API is down",
			want: "the model API is down",
		},
		{
			name: "a percent sign is escaped",
			in:   "50% of the files",
			want: "50%25 of the files",
		},
		{
			name: "a line feed is escaped",
			in:   "line1\nline2",
			want: "line1%0Aline2",
		},
		{
			name: "a carriage return is escaped",
			in:   "line1\rline2",
			want: "line1%0Dline2",
		},
		{
			name: "a CRLF is escaped as its two parts",
			in:   "50%\r\nx",
			want: "50%25%0D%0Ax",
		},
		{
			name: "an escape the text already holds is escaped itself",
			in:   "100%0A%25",
			want: "100%250A%2525",
		},
		{
			name: "a workflow command in the text stays one line of data",
			in:   "line1\n::error::fake",
			want: "line1%0A::error::fake",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.AnnotationData(tc.in)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("AnnotationData(%q) (-want +got):\n%s", tc.in, diff)
			}

			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("AnnotationData(%q) = %q holds a raw CR or LF, which ends the annotation's line", tc.in, got)
			}
		})
	}
}
