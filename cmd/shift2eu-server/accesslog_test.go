// White-box tests of the access log the composition root writes to stderr: the
// method, the path, the status, the duration and the size, and nothing of the
// request. An inventory describes someone's infrastructure (CLAUDE.md rule 2),
// so a log line that quotes a value, a query or a header would leak it to
// whatever collects the process's stderr (ADR 0019).
package main

import (
	"bytes"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// accessMarker is the value the test plants wherever a request carries content
// a log must not hold: an inventory ID, a resource type and a region, the query
// string, the Authorization header and another header.
const accessMarker = "m3accessmarker9f2"

// durationToken matches a duration as a log line writes it: a number with a unit
// time.Duration.String() produces, such as 12.3ms, 1.000123s or 250µs.
var durationToken = regexp.MustCompile(`\d+(?:\.\d+)?(?:ns|µs|μs|us|ms|s)\b`)

// quotedPathField matches the quoted path an access log line carries.
var quotedPathField = regexp.MustCompile(`path=("(?:[^"\\]|\\.)*")`)

// accessLogBound is how long a test waits for the access log line of a request the
// server has already answered: the handler writes the line in a defer, so the
// line exists only a moment after the client holds the whole body.
const accessLogBound = 5 * time.Second

// TestServerAccessLog pins the access log of an assessment: one line per
// request holding the method, the path, the status, a duration and the size of
// the answer, and no value of the request at all, neither from the body (an
// inventory ID, a resource type, a region) nor from the query string nor from a
// header.
//
// The assertion on the size is the answer's body length, which is what a reader
// of the log needs to tell a rendered report from an error document; the log
// line must carry it as a value of its own.
func TestServerAccessLog(t *testing.T) {
	t.Parallel()

	// The shared fixture is the base, with the marker in place of an ID, a
	// resource type and a region, so the report the request renders holds it and
	// a log that wrote any of the report's content would show it.
	document := []byte(strings.NewReplacer(
		"partial-example", accessMarker+"-id",
		"AWS::EC2::Instance", accessMarker+"-type",
		"eu-west-1", accessMarker+"-region",
	).Replace(string(fixtureInventory(t))))

	s := startServer(t, loopbackListen, nil)

	req := newRequest(t, http.MethodPost, "/v1/assessments?probe="+accessMarker, assessRequest(t, document, "ovhcloud"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessMarker+"-authorization")
	req.Header.Set("X-Probe", accessMarker+"-header")

	got := s.do(t, req)

	if got.status != http.StatusOK {
		t.Fatalf("POST /v1/assessments = %d, want %d; body: %q; stderr = %q",
			got.status, http.StatusOK, got.body, s.stderr.String())
	}

	// The marker reached the report, so the log had something to leak and this
	// test cannot pass on an assessment that echoed nothing.
	if !bytes.Contains(got.body, []byte(accessMarker)) {
		t.Fatalf("the report does not hold the marker %q, so the log had nothing to leak:\n%s", accessMarker, got.body)
	}

	// Wait for the request's line before reading the log, and read it before the
	// server is stopped: the shutdown may write a line of its own, which is no
	// access log line.
	lines := accessLines(t, s, 1)

	logged := s.stderr.String()

	if strings.Contains(logged, accessMarker) {
		t.Errorf("the log holds the marker %q, so it quotes the request:\n%s", accessMarker, logged)
	}

	for _, line := range lines {
		if !holdsValue(line, http.MethodPost) {
			t.Errorf("the access log line %q does not hold the method %s", line, http.MethodPost)
		}

		if !strings.Contains(line, "/v1/assessments") {
			t.Errorf("the access log line %q does not hold the path", line)
		}

		if !holdsValue(line, strconv.Itoa(got.status)) {
			t.Errorf("the access log line %q does not hold the status %d", line, got.status)
		}

		if !durationToken.MatchString(line) {
			t.Errorf("the access log line %q does not hold a duration", line)
		}

		if !holdsValue(line, strconv.Itoa(len(got.body))) {
			t.Errorf("the access log line %q does not hold the size %d of the answer", line, len(got.body))
		}

		// The path alone: the query string carries request content.
		if strings.Contains(line, "?") {
			t.Errorf("the access log line %q holds a query string, want the path only", line)
		}
	}

	// A second request adds a second line, so the log is per request rather than
	// one line for the run.
	if status, _ := s.get(t, "/healthz"); status != http.StatusOK {
		t.Errorf("GET /healthz = %d, want %d", status, http.StatusOK)
	}

	if got, want := len(accessLines(t, s, len(lines)+1)), len(lines)+1; got != want {
		t.Errorf("the log holds %d access log lines after two requests, want %d:\n%s",
			got, want, s.stderr.String())
	}

	if code := s.stop(t); code != 0 {
		t.Errorf("run returned %d after a cancelled context, want 0; stderr = %q", code, s.stderr.String())
	}
}

// TestServerAccessLogTruncatesLongPath pins the access log's handling of a long
// request path (review S6): the line quotes at most a fixed number of bytes of
// the path and marks it as cut, so a client cannot write an unbounded run of its
// own choosing into whatever collects stderr, and a reader still sees that the
// path was shortened. A path within the cap is logged whole and unmarked.
func TestServerAccessLogTruncatesLongPath(t *testing.T) {
	t.Parallel()

	const (
		// maxLoggedPathBytes is the path a line may quote: 256 bytes.
		maxLoggedPathBytes = 256
		// truncationMarker is what the line holds where the path was cut.
		truncationMarker = "..."
	)

	longPath := "/" + strings.Repeat("a", 4096)

	s := startServer(t, loopbackListen, nil)

	s.get(t, longPath)

	line := accessLineHolding(t, s, strings.Repeat("a", 8))

	match := quotedPathField.FindStringSubmatch(line)
	if match == nil {
		t.Fatalf("the access log line %q carries no quoted path", shorten(line, 120))
	}

	quoted, err := strconv.Unquote(match[1])
	if err != nil {
		t.Fatalf("the access log line %q carries a path that does not unquote: %v", shorten(line, 120), err)
	}

	if len(quoted) > maxLoggedPathBytes+len(truncationMarker) {
		t.Errorf("the access log quotes %d bytes of a request path of %d bytes, want at most %d: the log truncates a long path",
			len(quoted), len(longPath), maxLoggedPathBytes)
	}

	if !strings.Contains(line, truncationMarker) {
		t.Errorf("the access log line %q holds no %q marker, want a reader to see that the path was cut",
			shorten(line, 120), truncationMarker)
	}

	// A short path is logged whole, and only a cut path carries the marker.
	if status, body := s.get(t, "/healthz"); status != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want %d; body: %q", status, http.StatusOK, body)
	}

	// The handler writes the line after the client holds the body, so wait for it
	// rather than reading stderr the moment the request returns.
	shortLine := accessLineHolding(t, s, "/healthz")

	if !strings.Contains(shortLine, "/healthz") {
		t.Errorf("the access log line %q does not hold the whole path /healthz", shortLine)
	}

	if strings.Contains(shortLine, truncationMarker) {
		t.Errorf("the access log line %q holds the %q marker for a path within the cap, want it only on a cut path",
			shortLine, truncationMarker)
	}

	if code := s.stop(t); code != 0 {
		t.Errorf("run returned %d after a cancelled context, want 0; stderr = %q", code, s.stderr.String())
	}
}

// accessLines returns the access log lines s has written, polling until it holds
// at least want of them and failing the test when they do not appear within
// accessLogBound. A client holds its whole answer before the handler runs the
// deferred write of its line, so a test that read stderr the moment a request
// returned would race the handler: this is that wait, and no fixed sleep stands
// in for it, so a line that is never written still fails the test.
func accessLines(t *testing.T, s *server, want int) []string {
	t.Helper()

	deadline := time.Now().Add(accessLogBound)

	for {
		lines := startLines(s.stderr.String())

		if len(lines) >= want {
			return lines
		}

		if time.Now().After(deadline) {
			t.Fatalf("the log holds %d access log lines within %s, want at least %d; stderr = %q",
				len(lines), accessLogBound, want, s.stderr.String())
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// accessLineHolding returns the access log line s wrote that holds want, polling
// until the request's handler has written it, and fails the test when none
// appears.
func accessLineHolding(t *testing.T, s *server, want string) string {
	t.Helper()

	deadline := time.Now().Add(accessLogBound)

	for {
		logged := s.stderr.String()

		for _, line := range strings.Split(logged, "\n") {
			if strings.Contains(line, "method=") && strings.Contains(line, "path=") && strings.Contains(line, want) {
				return line
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("the log holds no access log line holding %q within %s; stderr = %q", want, accessLogBound, logged)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// shorten returns s cut to at most n bytes, with a marker when it was cut: for a
// failure message that would otherwise print a request path of kilobytes. The
// marker is not the access log's own, so the message cannot be mistaken for a
// line that marks its path.
func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + " [cut]"
}

// startLines returns the lines of stderr that are no startup line: the access
// log lines. The startup lines are the ones that announce the address, which a
// test needs before it can send a request, the effective concurrency, and any
// warning (audit L1, L4): none of them describes a request.
func startLines(stderr string) []string {
	startupPrefixes := []string{
		"shift2eu-server: concurrency ",
		"shift2eu-server: warning: ",
	}

	var lines []string

	for _, line := range strings.Split(stderr, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		if _, announced, ok := strings.Cut(line, "listening on "); ok && announced != "" {
			continue
		}

		var startup bool

		for _, prefix := range startupPrefixes {
			if strings.HasPrefix(line, prefix) {
				startup = true

				break
			}
		}

		if startup {
			continue
		}

		lines = append(lines, line)
	}

	return lines
}

// holdsValue reports whether line holds want as a value of its own: a field
// alone, or one behind a key and a separator, so "status=200", `"status":200`,
// "size 12" and "12" all hold their value.
func holdsValue(line, want string) bool {
	for _, field := range strings.Fields(line) {
		trimmed := strings.Trim(field, `"',[]{}() `)
		if trimmed == want {
			return true
		}

		for _, separator := range []string{"=", ":"} {
			if _, value, ok := strings.Cut(trimmed, separator); ok && strings.Trim(value, `"',`) == want {
				return true
			}
		}
	}

	return false
}
