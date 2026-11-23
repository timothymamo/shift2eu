// Black-box tests of the deploy program's library: the bundle it consumes, the
// S3 requests it signs, the rollback guard, the bucket listing and the key it
// must never print (PRD #297 milestone 3).
//
// Every test is offline. The object store and the live page are httptest servers
// built here, the bundle and the release manifests are testing/fstest.MapFS
// trees, and the clock is Config.Now, so no test opens a connection of its own,
// needs credentials or depends on the wall clock. The keys are invented
// (CLAUDE.md rule 2) and nothing here reads the environment (rule 3).
package deploy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// seenRequest is what a fake server saw of one request: everything a test
// asserts on, and the whole body it was sent.
type seenRequest struct {
	// Method is the request's method.
	Method string
	// Path is the request's path, without the query.
	Path string
	// Query is the request's query.
	Query url.Values
	// Header is the request's headers.
	Header http.Header
	// Body is the bytes of the request's body.
	Body []byte
}

// seenResponse is what a fake server answers one request with.
type seenResponse struct {
	// Status is the status code; 0 means 200.
	Status int
	// Header holds the headers to answer with.
	Header http.Header
	// Body is the bytes to answer with.
	Body []byte
}

// fakeServer is a fake HTTP server Deploy talks to: it records every request it
// receives and answers with respond, or with 200 and no body when respond is
// nil. A test points Config.Endpoint at an object store's and Config.LivePage at
// a live page's.
type fakeServer struct {
	// Server is the httptest server, closed when the test ends.
	Server *httptest.Server

	mu       sync.Mutex
	requests []seenRequest
	respond  func(seenRequest) seenResponse
}

// newFakeServer returns a fake server that answers with respond, and records
// every request. It is closed when the test ends.
func newFakeServer(t *testing.T, respond func(seenRequest) seenResponse) *fakeServer {
	t.Helper()

	server := &fakeServer{respond: respond}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serve))
	t.Cleanup(server.Server.Close)

	return server
}

// serve records one request and answers it with the server's responder.
func (s *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)

	received := seenRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
		Header: r.Header.Clone(),
		Body:   body,
	}

	s.mu.Lock()
	s.requests = append(s.requests, received)
	respond := s.respond
	s.mu.Unlock()

	if err != nil {
		http.Error(w, "the fake server could not read the request body", http.StatusInternalServerError)

		return
	}

	answer := seenResponse{Status: http.StatusOK}
	if respond != nil {
		answer = respond(received)
	}

	for name, values := range answer.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	status := answer.Status
	if status == 0 {
		status = http.StatusOK
	}

	w.WriteHeader(status)
	_, _ = w.Write(answer.Body)
}

// setRespond replaces the server's responder, so a subtest can make a store
// fail one key or answer a live page with a page of its own.
func (s *fakeServer) setRespond(respond func(seenRequest) seenResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.respond = respond
}

// seen returns the requests the server received, in order.
func (s *fakeServer) seen() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.requests)
}

// paths returns the path of every request the server received, in order.
func (s *fakeServer) paths() []string {
	requests := s.seen()
	paths := make([]string, 0, len(requests))

	for _, request := range requests {
		paths = append(paths, request.Path)
	}

	return paths
}

// puts returns the requests that were PUTs, in order.
func (s *fakeServer) puts() []seenRequest {
	var puts []seenRequest

	for _, request := range s.seen() {
		if request.Method == http.MethodPut {
			puts = append(puts, request)
		}
	}

	return puts
}

// putKeys returns the object name of every PUT, in order, taken from its
// path-style path /<bucket>/<name>.
func (s *fakeServer) putKeys() []string {
	puts := s.puts()
	keys := make([]string, 0, len(puts))

	for _, put := range puts {
		keys = append(keys, objectKey(put.Path))
	}

	return keys
}

// objectKey returns the object name of a path-style request path
// /<bucket>/<name>, and the path itself when it is not under the bucket.
func objectKey(path string) string {
	return strings.TrimPrefix(path, "/"+testBucket+"/")
}

// putPath returns the path-style path of an object of the site's bucket.
func putPath(name string) string {
	return "/" + testBucket + "/" + name
}

// listResponder answers the listing requests with pages in turn: the first
// listing gets the first page, the second the second, and the last page answers
// every further listing.
func listResponder(t *testing.T, pages ...[]byte) func(seenRequest) seenResponse {
	t.Helper()

	if len(pages) == 0 {
		t.Fatal("listResponder was given no page")
	}

	var (
		mu   sync.Mutex
		next int
	)

	return func(seenRequest) seenResponse {
		mu.Lock()
		defer mu.Unlock()

		page := pages[next]
		if next < len(pages)-1 {
			next++
		}

		return seenResponse{Status: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: page}
	}
}

// listRequests returns the requests a store received that are bucket listings.
func listRequests(requests []seenRequest) []seenRequest {
	var listings []seenRequest

	for _, request := range requests {
		if request.Query.Get("list-type") == "2" {
			listings = append(listings, request)
		}
	}

	return listings
}
