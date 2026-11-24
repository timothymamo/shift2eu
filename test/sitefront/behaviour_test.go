//go:build sitefront && !js

package sitefront_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// This file holds the front's behaviour test: the pinned image running
// infra/site/Caddyfile with the command line infra/site/container.tf gives it,
// against a local HTTPS upstream, driven over raw HTTP/1.1 with every case of
// docs/adr/0045's list. `make test-sitefront` runs it, outside make test and
// make check, as `make test-browser` runs the browser smoke test
// (docs/adr/0036): it starts no container of its own until a test asks it to, it
// builds no image, and it contacts no cloud.
//
// The front is a container on the host's network namespace, as the browser
// smoke test runs Chromium, so the Caddyfile's :8080 is dialled on 127.0.0.1 and
// the test's upstream on 127.0.0.1 is what the container reaches. The upstream
// is a Go TLS server the test starts, with a CA this process signs and the
// container trusts through SSL_CERT_FILE, so Caddy verifies the upstream's
// certificate as it does in production and the Caddyfile stays a static file
// (docs/adr/0045).

// The harness the test needs. `make test-sitefront` sets the first; the rest are
// this test's own.
const (
	// runtimeEnv names the container runtime to start the front with, docker or
	// nerdctl, as the Makefile's DOCKER picks it.
	runtimeEnv = "SHIFT2EU_SITEFRONT_RUNTIME"
	// frontAddress is the address :8080 of the Caddyfile answers on, in the
	// host's network namespace.
	frontAddress = "127.0.0.1:8080"
	// containerCAPath is where the test's CA is mounted in the container and
	// read from: Go's SSL_CERT_FILE, so Caddy's proxy trusts it.
	containerCAPath = "/etc/ssl/certs/shift2eu-sitefront-ca.pem"
	// startDeadline is how long the container has to answer the first request:
	// a cold start pulls the image once and runs the Caddyfile.
	startDeadline = 120 * time.Second
	// rmDeadline caps the container's removal, as make test-browser caps its own
	// (docs/adr/0036, issue #449).
	rmDeadline = 60 * time.Second
)

// The upstream's paths: one answer each, for the cases that need the upstream
// to answer something of its own.
const (
	// homeBody is what the upstream answers at /, so a proxied page is
	// recognisable.
	homeBody = "the site"
	// missingPath is the upstream's 404.
	missingPath = "/missing"
	// ownHeadersPath answers with a Content-Security-Policy, a Strict-Transport-Security, a
	// Permissions-Policy, an X-Content-Type-Options and a Referrer-Policy of its own, whose copies must not
	// reach the client.
	ownHeadersPath = "/own-headers"
	// gzipPath answers a gzip object with the cache headers the deploy sets.
	gzipPath = "/page.wasm"
	// gzipBody is the object's uncompressed content.
	gzipBody = "the module's bytes, however long they are"
	// gzipType and gzipCacheControl are the object's Content-Type and
	// Cache-Control, which must pass through unchanged.
	gzipType         = "application/wasm"
	gzipCacheControl = "public, max-age=31536000, immutable"
	// gzipAge is the object's Age, as Edge Services would set it.
	gzipAge = "42"
)

// The markers one request carries, whose value would make the container's log a record of the visitor who
// sent them: the path it asked for, its user agent and its address, in the two headers the platform and a
// client forward. testLogHoldsNoMarker reads the container's whole log for each of them (docs/adr/0045,
// *The log*).
const (
	markerPath         = "/marker-path-1234"
	markerUserAgent    = "shift2eu-marker-ua"
	markerForwardedFor = "198.51.100.7"
	markerRealIP       = "198.51.100.8"
)

// logMarkers are the values above as strings.Contains finds them in the container's log.
var logMarkers = []string{markerPath, markerUserAgent, markerForwardedFor, markerRealIP}

// TestSiteFront drives the front's every case, in the order they share one
// container and one upstream (docs/adr/0045): the four rows of the table, the
// joined X-Forwarded-Proto, the Location of every redirect, the headers against
// an upstream that sends its own, a 404, a gzip object, the request headers the
// upstream never sees, the markers no log may hold, OPTIONS * and, last, the 502
// of no upstream at all, after which the container's whole log is read. Nothing
// here is parallel: the cases share the container the first line starts.
func TestSiteFront(t *testing.T) {
	front := startFront(t)

	t.Run("the four rows of the table", func(t *testing.T) { testTableRows(t, front) })
	t.Run("the joined X-Forwarded-Proto", func(t *testing.T) { testJoinedForwardedProto(t, front) })
	t.Run("the Location of every redirect", func(t *testing.T) { testLocations(t, front) })
	t.Run("the headers against the upstream's own", func(t *testing.T) { testHeaders(t, front) })
	t.Run("a 404 from the upstream", func(t *testing.T) { testUpstreamNotFound(t, front) })
	t.Run("a gzip object passes byte for byte", func(t *testing.T) { testGzip(t, front) })
	t.Run("the request headers the upstream never sees", func(t *testing.T) { testRequestHeaders(t, front) })
	t.Run("the markers a visitor's request carries", func(t *testing.T) { testLogMarkers(t, front) })
	t.Run("OPTIONS *", func(t *testing.T) { testOptionsAsterisk(t, front) })
	t.Run("no upstream", func(t *testing.T) { testNoUpstream(t, front) })
	t.Run("the log holds no marker", func(t *testing.T) { testLogHoldsNoMarker(t, front) })
}

// testTableRows drives docs/adr/0045's four rows: a request whose joined
// X-Forwarded-Proto is not exactly https, the apex, any host but www, and www
// over HTTPS, which is the one row that reaches the site.
func testTableRows(t *testing.T, front *harness) {
	t.Helper()

	proxies := front.upstream.count()

	tests := []struct {
		name   string
		raw    string
		status int
		loc    string // the Location of a redirect; "" when the row is proxied
	}{
		{
			name:   "plain HTTP, www, no X-Forwarded-Proto",
			raw:    request("GET / HTTP/1.1", "Host: www.shift2eu.com"),
			status: http.StatusPermanentRedirect,
			loc:    "https://www.shift2eu.com/",
		},
		{
			name:   "plain HTTP, www, X-Forwarded-Proto http",
			raw:    request("GET / HTTP/1.1", "Host: www.shift2eu.com", "X-Forwarded-Proto: http"),
			status: http.StatusPermanentRedirect,
			loc:    "https://www.shift2eu.com/",
		},
		{
			name:   "plain HTTP, the apex",
			raw:    request("GET / HTTP/1.1", "Host: shift2eu.com"),
			status: http.StatusPermanentRedirect,
			loc:    "https://www.shift2eu.com/",
		},
		{
			name:   "HTTPS, the apex",
			raw:    request("GET / HTTP/1.1", "Host: shift2eu.com", "X-Forwarded-Proto: https"),
			status: http.StatusPermanentRedirect,
			loc:    "https://www.shift2eu.com/",
		},
		{
			name:   "HTTPS, the container's own endpoint",
			raw:    request("GET / HTTP/1.1", "Host: twelve-twentyone.functions.fnc.fr-par.scw.cloud", "X-Forwarded-Proto: https"),
			status: http.StatusPermanentRedirect,
			loc:    "https://www.shift2eu.com/",
		},
		{
			name:   "HTTPS, www",
			raw:    request("GET / HTTP/1.1", "Host: www.shift2eu.com", "X-Forwarded-Proto: https"),
			status: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := front.do(t, tc.raw)

			if resp.status != tc.status {
				t.Fatalf("%q answers %d, want %d", firstLine(tc.raw), resp.status, tc.status)
			}

			if tc.status == http.StatusOK {
				if got := string(resp.body); got != homeBody {
					t.Errorf("%q answers the body %q, want the upstream's %q: www over HTTPS is proxied", firstLine(tc.raw), got, homeBody)
				}

				return
			}

			if got := resp.header.Get("Location"); got != tc.loc {
				t.Errorf("%q redirects to %q, want %q", firstLine(tc.raw), got, tc.loc)
			}

			// The preceding row carries no HSTS: the header is the HTTPS answer's,
			// and nothing holds a value sent on a redirect (docs/adr/0043,
			// docs/adr/0045).
			if got := resp.header.Values("Strict-Transport-Security"); len(got) > 0 {
				t.Errorf("%q answers Strict-Transport-Security %q, want none: the header belongs to the site's HTTPS answers (docs/adr/0045)", firstLine(tc.raw), got)
			}
		})
	}

	// The rows that must not reach the site: five redirects, one request
	// proxied.
	if got := front.upstream.count(); got != proxies+1 {
		t.Errorf("the upstream received %d requests for the six rows, want one: a request that is not exactly https or not exactly www gets a 308 and never the site (docs/adr/0045)", got-proxies)
	}
}

// testJoinedForwardedProto pins how Caddy joins a repeated X-Forwarded-Proto
// field (docs/adr/0045): two fields in either order, two https fields and the
// joined value https, https are all not exactly https, so every one of them is
// a 308, never the site. Only a client that sends https as one value over plain
// HTTP would be answered as the site, which is recorded in docs/adr/0045.
func testJoinedForwardedProto(t *testing.T, front *harness) {
	t.Helper()

	proxies := front.upstream.count()

	tests := []struct {
		name    string
		headers []string
	}{
		{name: "https then http", headers: []string{"X-Forwarded-Proto: https", "X-Forwarded-Proto: http"}},
		{name: "http then https", headers: []string{"X-Forwarded-Proto: http", "X-Forwarded-Proto: https"}},
		{name: "two https fields", headers: []string{"X-Forwarded-Proto: https", "X-Forwarded-Proto: https"}},
		{name: "https, https in one field", headers: []string{"X-Forwarded-Proto: https, https"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := request("GET /p?q=1&r=2 HTTP/1.1", append([]string{"Host: www.shift2eu.com"}, tc.headers...)...)
			resp := front.do(t, raw)
			if resp.status != http.StatusPermanentRedirect {
				t.Fatalf("%q with %q answers %d, want 308: the joined header is not exactly https (docs/adr/0045)", firstLine(raw), tc.headers, resp.status)
			}
			if got, want := resp.header.Get("Location"), "https://www.shift2eu.com/p?q=1&r=2"; got != want {
				t.Errorf("%q with %q redirects to %q, want %q", firstLine(raw), tc.headers, got, want)
			}
		})
	}

	if got := front.upstream.count(); got != proxies {
		t.Errorf("the upstream received %d requests for the four spoofed headers, want none: each of them is a 308 (docs/adr/0045)", got-proxies)
	}
}

// testLocations drives the redirects docs/adr/0045 lists as the escaped path's
// own cases: a Location is a URL of the site with the request's escaped path and
// its query, never a decoded path, never another host and never the request
// target glued to the host. The absolute-form, the opaque target and the hosts
// that are not exactly www.shift2eu.com go to https://www.shift2eu.com/.
func testLocations(t *testing.T, front *harness) {
	t.Helper()

	tests := []struct {
		name    string
		raw     string
		path    string // the Location's decoded path
		query   string // the Location's raw query
		escaped string // the Location's escaped path, when the escaping must survive
	}{
		{
			name: "the root",
			raw:  request("GET / HTTP/1.1", "Host: www.shift2eu.com"),
			path: "/",
		},
		{
			name:    "a path and a query",
			raw:     request("GET /p?q=1&r=2 HTTP/1.1", "Host: www.shift2eu.com"),
			path:    "/p",
			query:   "q=1&r=2",
			escaped: "/p",
		},
		{
			name:    "a target that begins with two slashes",
			raw:     request("GET //evil.example/x HTTP/1.1", "Host: www.shift2eu.com"),
			path:    "//evil.example/x",
			escaped: "//evil.example/x",
		},
		{
			name:    "an escaped two slashes",
			raw:     request("GET /%2F%2Fevil.example HTTP/1.1", "Host: www.shift2eu.com"),
			path:    "///evil.example",
			escaped: "/%2F%2Fevil.example",
		},
		{
			name:    "an escaped question mark",
			raw:     request("GET /a%3Fb HTTP/1.1", "Host: www.shift2eu.com"),
			path:    "/a?b",
			escaped: "/a%3Fb",
		},
		{
			name:    "an escaped fragment",
			raw:     request("GET /a%23b HTTP/1.1", "Host: www.shift2eu.com"),
			path:    "/a#b",
			escaped: "/a%23b",
		},
		{
			name:    "an escaped line break",
			raw:     request("GET /a%0Ab HTTP/1.1", "Host: www.shift2eu.com"),
			path:    "/a\nb",
			escaped: "/a%0Ab",
		},
		{
			name: "an absolute-form target",
			raw:  request("GET http://evil.example/ HTTP/1.1", "Host: www.shift2eu.com"),
			path: "/",
		},
		{
			name: "an opaque target",
			raw:  request("GET http:foo HTTP/1.1", "Host: www.shift2eu.com"),
			path: "/",
		},
		{
			name: "a host with a port",
			raw:  request("GET /p?q=1&r=2 HTTP/1.1", "Host: www.shift2eu.com:443", "X-Forwarded-Proto: https"),
			path: "/p", query: "q=1&r=2", escaped: "/p",
		},
		{
			name: "a host in another case",
			raw:  request("GET /p HTTP/1.1", "Host: WWW.shift2eu.com", "X-Forwarded-Proto: https"),
			path: "/p", escaped: "/p",
		},
		{
			name: "a host with a trailing dot",
			raw:  request("GET /p HTTP/1.1", "Host: www.shift2eu.com.", "X-Forwarded-Proto: https"),
			path: "/p", escaped: "/p",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := front.do(t, tc.raw)

			if resp.status != http.StatusPermanentRedirect {
				t.Fatalf("%q answers %d, want 308", firstLine(tc.raw), resp.status)
			}

			location := resp.header.Get("Location")
			loc, err := url.Parse(location)
			if err != nil {
				t.Fatalf("%q redirects to %q, which is not a URL: %v", firstLine(tc.raw), location, err)
			}

			if loc.Scheme != "https" {
				t.Errorf("%q redirects to %q, whose scheme is %q, want https", firstLine(tc.raw), location, loc.Scheme)
			}
			if loc.Host != "www.shift2eu.com" {
				t.Errorf("%q redirects to %q, whose host is %q, want exactly www.shift2eu.com: the Location is never the request target glued to a prefix (docs/adr/0045)", firstLine(tc.raw), location, loc.Host)
			}
			if loc.Path != tc.path {
				t.Errorf("%q redirects to %q, whose decoded path is %q, want %q", firstLine(tc.raw), location, loc.Path, tc.path)
			}
			if loc.RawQuery != tc.query {
				t.Errorf("%q redirects to %q, whose query is %q, want %q: the query passes through", firstLine(tc.raw), location, loc.RawQuery, tc.query)
			}
			if tc.escaped != "" && loc.EscapedPath() != tc.escaped {
				t.Errorf("%q redirects to %q, whose escaped path is %q, want %q: the escaping survives, so no ? or # or line break enters the header (docs/adr/0045)", firstLine(tc.raw), location, loc.EscapedPath(), tc.escaped)
			}
			if strings.ContainsAny(location, "\r\n") {
				t.Errorf("%q redirects to %q, which holds a line break", firstLine(tc.raw), location)
			}
		})
	}
}

// testHeaders holds the headers of a proxied answer (docs/adr/0045, *The
// headers*): Caddy's single copy of each of the five, when the upstream sends
// its own copies of the three it may and the client sees only Caddy's.
func testHeaders(t *testing.T, front *harness) {
	t.Helper()

	t.Run("the site's answer", func(t *testing.T) {
		resp := front.do(t, request("GET / HTTP/1.1", "Host: www.shift2eu.com", "X-Forwarded-Proto: https"))

		if resp.status != http.StatusOK {
			t.Fatalf("GET / over HTTPS to www answers %d, want 200", resp.status)
		}

		assertSiteHeaders(t, resp)
	})

	t.Run("an upstream that sends its own copies", func(t *testing.T) {
		resp := front.do(t, request("GET "+ownHeadersPath+" HTTP/1.1", "Host: www.shift2eu.com", "X-Forwarded-Proto: https"))

		if resp.status != http.StatusOK {
			t.Fatalf("GET %s answers %d, want the upstream's 200", ownHeadersPath, resp.status)
		}

		// Only Caddy's copies: the upstream's own values must not reach the
		// client, and neither must a second copy of Caddy's.
		assertSiteHeaders(t, resp)

		if got := resp.header.Values("Content-Security-Policy"); len(got) != 1 || strings.Contains(got[0], "upstream.example") {
			t.Errorf("the client sees Content-Security-Policy %q, want Caddy's one copy, not the upstream's (docs/adr/0045)", got)
		}
	})
}

// testUpstreamNotFound holds that an upstream's 404 passes as a 404 and still
// carries every header: the headers are set before the proxy's answer, not only
// on a 200 (docs/adr/0045).
func testUpstreamNotFound(t *testing.T, front *harness) {
	t.Helper()

	resp := front.do(t, request("GET "+missingPath+" HTTP/1.1", "Host: www.shift2eu.com", "X-Forwarded-Proto: https"))

	if resp.status != http.StatusNotFound {
		t.Fatalf("GET %s answers %d, want the upstream's 404", missingPath, resp.status)
	}

	assertSiteHeaders(t, resp)
}

// testGzip holds a gzip object's bytes and the headers that describe them
// (docs/adr/0045, docs/adr/0043): no encode and no cache in the front, so the
// object the deploy uploaded passes through unchanged.
func testGzip(t *testing.T, front *harness) {
	t.Helper()

	var compressed bytes.Buffer

	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write([]byte(gzipBody)); err != nil {
		t.Fatalf("compress the fixture: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("compress the fixture: %v", err)
	}

	resp := front.do(t, request("GET "+gzipPath+" HTTP/1.1", "Host: www.shift2eu.com", "X-Forwarded-Proto: https", "Accept-Encoding: gzip"))

	if resp.status != http.StatusOK {
		t.Fatalf("GET %s answers %d, want 200", gzipPath, resp.status)
	}

	if !bytes.Equal(resp.body, compressed.Bytes()) {
		t.Errorf("GET %s answers %d bytes, want the %d the upstream sent: the object is not re-encoded (docs/adr/0043)", gzipPath, len(resp.body), compressed.Len())
	}

	for _, want := range []struct{ name, value string }{
		{"Content-Encoding", "gzip"},
		{"Content-Type", gzipType},
		{"Cache-Control", gzipCacheControl},
		{"Age", gzipAge},
	} {
		if got := resp.header.Get(want.name); got != want.value {
			t.Errorf("GET %s answers %s %q, want %q: the deploy's headers pass through unchanged (docs/adr/0045)", gzipPath, want.name, got, want.value)
		}
	}

	assertSiteHeaders(t, resp)
}

// testRequestHeaders holds docs/adr/0045's *The upstream*: the platform's
// forwarding headers, the visitor's address, a cookie, an authorization and a
// client's own copies reach Edge Services never, its Host is the endpoint's own,
// and no X-Forwarded-* of Caddy's own is added.
func testRequestHeaders(t *testing.T, front *harness) {
	t.Helper()

	const path = "/the-upstream-sees-this"

	sent := []string{
		"X-Forwarded-For: 203.0.113.7",
		"X-Real-IP: 203.0.113.7",
		"Forwarded: for=203.0.113.7",
		"X-Forwarded-Host: www.shift2eu.com",
		"X-Forwarded-Port: 443",
		"Via: 1.1 client.example",
		"X-Envoy-Upstream-Service-Time: 12",
		"X-Envoy-External-Address: 203.0.113.7",
		"X-Request-Id: 1234-5678",
		"Cookie: session=secret",
		"Authorization: Bearer secret",
	}

	headers := append([]string{"Host: www.shift2eu.com", "X-Forwarded-Proto: https"}, sent...)
	resp := front.do(t, request("GET "+path+" HTTP/1.1", headers...))

	if resp.status != http.StatusOK {
		t.Fatalf("GET %s answers %d, want the upstream's 200", path, resp.status)
	}

	seen, ok := front.upstream.request(path)
	if !ok {
		t.Fatalf("the upstream recorded no request for %q: the request was not proxied", path)
	}

	for _, header := range sent {
		name, _, _ := strings.Cut(header, ":")
		if got := seen.header.Values(name); len(got) > 0 {
			t.Errorf("the upstream receives %s: %q, want none: it carries the visitor's address, a credential or a client's forwarding header, and Edge Services needs none of them (docs/adr/0045)", name, got)
		}
	}

	for name := range seen.header {
		if strings.HasPrefix(strings.ToLower(name), "x-forwarded-") {
			t.Errorf("the upstream receives %s: %q, want no X-Forwarded-* at all, Caddy's own included: the platform's are removed and Caddy's must not replace them (docs/adr/0045)", name, seen.header.Values(name))
		}
	}

	if got := seen.host; got != front.upstream.addr {
		t.Errorf("the upstream receives Host %q, want %q: the proxy sends the endpoint's own host and port (C3's --change-host-header, docs/adr/0045)", got, front.upstream.addr)
	}
}

// testLogMarkers sends the two requests whose answers are ordinary - one the site
// proxies, one the front redirects - and each of which carries the markers a log
// line would expose: the path the visitor asked for, its user agent and its
// address (docs/adr/0045, *The log*). The container's log is read once, after
// the cases that make Caddy fail (testLogHoldsNoMarker): a log line of a failed
// request is what Caddy's default logger writes.
func testLogMarkers(t *testing.T, front *harness) {
	t.Helper()

	tests := []struct {
		name   string
		raw    string
		status int
	}{
		{
			name:   "a request the site answers",
			raw:    markerRequest("GET "+markerPath+" HTTP/1.1", "X-Forwarded-Proto: https"),
			status: http.StatusOK,
		},
		{
			name:   "a request the front redirects",
			raw:    markerRequest("GET " + markerPath + " HTTP/1.1"),
			status: http.StatusPermanentRedirect,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if resp := front.do(t, tc.raw); resp.status != tc.status {
				t.Fatalf("%q answers %d, want %d", firstLine(tc.raw), resp.status, tc.status)
			}
		})
	}
}

// testLogHoldsNoMarker holds that no line of the container's log names the visitor
// (docs/adr/0045, *The log*): the front's output on stdout and stderr is what the
// platform ships to Cockpit, so a line that holds the visitor's address, its user
// agent or the path it asked for is a third party's record of that visitor. Caddy
// writes such a line for every request it fails - the reverse proxy's own error,
// http.handlers.reverse_proxy, and the server's http.log.error - unless the
// global log silences both, so the two failures are driven here: no upstream at
// all (the front's own 502), and an upstream whose certificate the container does
// not trust. It runs last: it leaves an untrusted TLS server on the address Edge
// Services had.
func testLogHoldsNoMarker(t *testing.T, front *harness) {
	t.Helper()

	// The front's own 502: testNoUpstream stopped the upstream for good.
	if resp := front.do(t, markerRequest("GET "+markerPath+" HTTP/1.1", "X-Forwarded-Proto: https")); resp.status != http.StatusBadGateway {
		t.Fatalf("with no upstream the front answers %d, want its own 502", resp.status)
	}

	// The proxy's TLS failure: something answers on the upstream's address, and its certificate is one a
	// CA of its own signs, which the container does not trust.
	startUntrustedUpstream(t, front.upstream.addr)

	if resp := front.do(t, markerRequest("GET "+markerPath+" HTTP/1.1", "X-Forwarded-Proto: https")); resp.status != http.StatusBadGateway {
		t.Fatalf("with an upstream the container does not trust the front answers %d, want its own 502", resp.status)
	}

	log, err := front.wholeLog()
	if err != nil {
		t.Fatal(err)
	}

	for _, marker := range logMarkers {
		if strings.Contains(log, marker) {
			t.Errorf("the container's log holds %q: the platform ships the container's output to Cockpit, so a line that names the visitor's address, its user agent or the path it asked for is a third party's record of that visitor, which the front keeps none of (docs/adr/0045, *The log*). The whole log:\n%s", marker, log)
		}
	}
}

// markerRequest builds a www request that carries the markers: the field lines every request has, plus the
// visitor's user agent and the two headers its address is forwarded in.
func markerRequest(requestLine string, fieldLines ...string) string {
	head := []string{
		"Host: www.shift2eu.com",
		"User-Agent: " + markerUserAgent,
		"X-Forwarded-For: " + markerForwardedFor,
		"X-Real-IP: " + markerRealIP,
	}

	return request(requestLine, append(head, fieldLines...)...)
}

// testOptionsAsterisk pins what OPTIONS * is answered with (docs/adr/0045): Go's
// general options handler answers it before Caddy's routes see the request, as
// an empty 200 with no body and none of the site's headers. It is the ADR's
// accepted exception and the only answer the front may give it: a redirect or a
// proxied answer would mean the request reached the Caddyfile's routes.
func testOptionsAsterisk(t *testing.T, front *harness) {
	t.Helper()

	resp := front.do(t, request("OPTIONS * HTTP/1.1", "Host: www.shift2eu.com"))

	if resp.status != http.StatusOK {
		t.Fatalf("OPTIONS * answers %d, want Go's general options handler's empty 200, which no route of the Caddyfile reaches (docs/adr/0045)", resp.status)
	}

	if got := resp.header.Get("Content-Length"); got != "0" {
		t.Errorf("OPTIONS * answers 200 with Content-Length %q, want 0", got)
	}
	if len(resp.body) != 0 {
		t.Errorf("OPTIONS * answers 200 with %d bytes of body, want none: the general options handler sends no content (docs/adr/0045)", len(resp.body))
	}
	for _, name := range []string{"Content-Security-Policy", "Strict-Transport-Security", "Permissions-Policy", "X-Content-Type-Options", "Referrer-Policy"} {
		if got := resp.header.Values(name); len(got) > 0 {
			t.Errorf("OPTIONS * answers 200 with %s %q, want none: the accepted exception carries no header of the site (docs/adr/0045)", name, got)
		}
	}
}

// testNoUpstream holds the front's own 502 (docs/adr/0045): when Edge Services
// answers nothing, the client still gets every header, which a deferred header
// would leave off. It stops the upstream for good; testLogHoldsNoMarker, which
// runs after it, uses the address it had.
func testNoUpstream(t *testing.T, front *harness) {
	t.Helper()

	front.upstream.stop()

	resp := front.do(t, request("GET / HTTP/1.1", "Host: www.shift2eu.com", "X-Forwarded-Proto: https"))

	if resp.status != http.StatusBadGateway {
		t.Fatalf("with no upstream the front answers %d, want its own 502", resp.status)
	}

	assertSiteHeaders(t, resp)
}

// assertSiteHeaders holds a response of the site to the five headers of
// docs/adr/0045, each exactly once: HSTS as site/records/hsts.json implies it,
// the pages' Content-Security-Policy plus frame-ancestors 'none', nosniff, no
// referrer, and a Permissions-Policy that turns every feature off. Every one of
// them is compared against a one-element list, so a second copy - Caddy's and the
// upstream's - fails here.
func assertSiteHeaders(t *testing.T, resp *response) {
	t.Helper()

	wantHSTS, sendHSTS := hstsHeaderFor(t)

	gotHSTS := resp.header.Values("Strict-Transport-Security")
	switch {
	case !sendHSTS && len(gotHSTS) > 0:
		t.Errorf("the answer carries Strict-Transport-Security %q, but site/records/hsts.json waives it (docs/adr/0042)", gotHSTS)
	case sendHSTS && len(gotHSTS) != 1:
		t.Errorf("the answer carries %d Strict-Transport-Security headers, want exactly one %q (docs/adr/0045)", len(gotHSTS), wantHSTS)
	case sendHSTS && gotHSTS[0] != wantHSTS:
		t.Errorf("the answer carries Strict-Transport-Security %q, want %q, the header site/records/hsts.json implies (docs/adr/0042, docs/adr/0045)", gotHSTS[0], wantHSTS)
	}

	gotCSP := resp.header.Values("Content-Security-Policy")
	if len(gotCSP) != 1 {
		t.Errorf("the answer carries %d Content-Security-Policy headers, want exactly one, which replaces the upstream's: %q (docs/adr/0045)", len(gotCSP), gotCSP)
	} else if want := withFrameAncestors(cspDirectives(pageCSP(t))); !slices.Equal(cspDirectives(gotCSP[0]), want) {
		t.Errorf("the answer's Content-Security-Policy is %q, want the pages' policy plus frame-ancestors 'none' as the directive set %q (docs/adr/0036, docs/adr/0045)", gotCSP[0], want)
	}

	for _, want := range []struct{ name, value string }{
		{"X-Content-Type-Options", "nosniff"},
		{"Referrer-Policy", "no-referrer"},
	} {
		if got := resp.header.Values(want.name); !slices.Equal(got, []string{want.value}) {
			t.Errorf("the answer carries %s %q, want exactly %q (docs/adr/0045)", want.name, got, want.value)
		}
	}

	if got := resp.header.Values("Permissions-Policy"); len(got) != 1 {
		t.Errorf("the answer carries %d Permissions-Policy headers, want exactly one (docs/adr/0045)", len(got))
	} else if err := checkPermissionsPolicy(got[0]); err != nil {
		t.Errorf("the answer's Permissions-Policy %q: %v", got[0], err)
	}
}

// harness is the front and the upstream one run of the test shares.
type harness struct {
	runtime string
	name    string
	dir     string

	upstream *upstream
}

// startFront starts the front from what infra/site/container.tf runs.
func startFront(t *testing.T) *harness {
	t.Helper()

	container, err := readContainerRun()
	if err != nil {
		t.Fatal(err)
	}

	return startFrontWith(t, container)
}

// startFrontWith starts the upstream, then the given container, and waits for
// the first proxied answer. Its cleanup removes the container and the upstream,
// whatever the test's outcome.
func startFrontWith(t *testing.T, container containerRun) *harness {
	t.Helper()

	runtime := os.Getenv(runtimeEnv)
	if runtime != "docker" && runtime != "nerdctl" {
		t.Fatalf("%s is %q: this test starts the front's container itself, so make test-sitefront names the container runtime (docker or nerdctl), as make test-browser starts Chromium; it never skips, since a smoke test that skips reports the same green as one that passed", runtimeEnv, runtime)
	}

	h := &harness{
		runtime: runtime,
		name:    fmt.Sprintf("shift2eu-test-sitefront-%d", os.Getpid()),
	}

	h.dir = t.TempDir()

	caPEM, leaf, err := testCertificates()
	if err != nil {
		t.Fatalf("create the test CA: %v", err)
	}

	caPath := filepath.Join(h.dir, "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write the test CA: %v", err)
	}

	h.upstream, err = startUpstream(leaf)
	if err != nil {
		t.Fatalf("start the upstream: %v", err)
	}

	t.Cleanup(func() {
		h.removeContainer(t)
		h.upstream.stop()
	})

	h.startContainer(t, container, caPath)
	h.waitReady(t)

	return h
}

// startContainer starts the container as the platform does: the pinned image,
// then the command line container.tf gives it, with UPSTREAM as the local
// upstream and SSL_CERT_FILE the test's CA. The front is on the host's network
// namespace, as the browser smoke test's Chromium is, so 127.0.0.1:8080 is the
// Caddyfile's :8080 and 127.0.0.1:<port> is the upstream the container reaches.
func (h *harness) startContainer(t *testing.T, container containerRun, caPath string) {
	t.Helper()

	args := []string{
		"run", "-d",
		"--name", h.name,
		"--network", "host",
		"-e", "UPSTREAM=" + h.upstream.addr,
		"-e", "SSL_CERT_FILE=" + containerCAPath,
		"-v", caPath + ":" + containerCAPath + ":ro",
		container.image,
	}
	args = append(args, container.argv...)

	ctx, cancel := context.WithTimeout(t.Context(), startDeadline)
	defer cancel()

	out, err := exec.CommandContext(ctx, h.runtime, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", h.runtime, strings.Join(args[:6], " "), err, out)
	}
}

// waitReady waits until the container answers a proxied request, and reports its
// log when it exits first: a Caddyfile it refuses, or a :8080 another run still
// holds, fails here with the reason rather than in every case below.
func (h *harness) waitReady(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(startDeadline)
	for time.Now().Before(deadline) {
		if running, err := h.running(); err == nil && !running {
			t.Fatalf("the container exited before it answered a request; its log:\n%s", h.logs())
		}

		before := h.upstream.count()
		resp, err := doRequest(request("GET / HTTP/1.1", "Host: www.shift2eu.com", "X-Forwarded-Proto: https"))
		if err == nil && resp.status == http.StatusOK && h.upstream.count() > before {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("the container answered no proxied request within %s; its log:\n%s", startDeadline, h.logs())
}

// running reports whether the container still runs.
func (h *harness) running() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, h.runtime, "inspect", "-f", "{{.State.Running}}", h.name).Output()
	if err != nil {
		return false, fmt.Errorf("%s inspect %s: %w", h.runtime, h.name, err)
	}

	return strings.TrimSpace(string(out)) == "true", nil
}

// logs returns the tail of the container's log, for a failure's message.
func (h *harness) logs() string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, h.runtime, "logs", "--tail", "30", h.name).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("(%s logs %s: %v) %s", h.runtime, h.name, err, out)
	}

	return string(out)
}

// wholeLog returns the container's whole log, stdout and stderr together, as the platform ships it: what the
// front wrote about a request is what a visitor's data would be kept in (docs/adr/0045, *The log*).
func (h *harness) wholeLog() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, h.runtime, "logs", h.name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s logs %s: %w", h.runtime, h.name, err)
	}

	return string(out), nil
}

// removeContainer removes the container, waiting no longer than rmDeadline, as
// make test-browser does (docs/adr/0036, issue #449). A removal that times out
// says which command removes it by hand: the test never signals a process it did
// not start.
func (h *harness) removeContainer(t *testing.T) {
	t.Helper()

	if h.runtime == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), rmDeadline)
	defer cancel()

	if out, err := exec.CommandContext(ctx, h.runtime, "rm", "-f", h.name).CombinedOutput(); err != nil {
		t.Logf("%s rm -f %s did not finish within %s: %v\n%s\nremove the container by hand with: %s rm -f %s", h.runtime, h.name, rmDeadline, err, out, h.runtime, h.name)
	}
}

// do performs one raw request against the front and reads its answer whole.
func (h *harness) do(t *testing.T, raw string) *response {
	t.Helper()

	resp, err := doRequest(raw)
	if err != nil {
		t.Fatalf("%q: %v", firstLine(raw), err)
	}

	return resp
}

// response is one answer of the front, whole.
type response struct {
	status int
	header http.Header
	body   []byte
}

// doRequest writes raw, a whole HTTP/1.1 request, over a connection of its own
// and reads the answer: a raw connection is what carries an absolute-form or an
// opaque request target, OPTIONS *, repeated field lines and any Host a client
// likes, which net/http's client would refuse or normalise.
func doRequest(raw string) (*response, error) {
	conn, err := net.DialTimeout("tcp", frontAddress, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", frontAddress, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, fmt.Errorf("set the deadline: %w", err)
	}

	if _, err := io.WriteString(conn, raw); err != nil {
		return nil, fmt.Errorf("write the request: %w", err)
	}

	req := &http.Request{Method: "GET"}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return nil, fmt.Errorf("read the answer: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read the answer's body: %w", err)
	}

	return &response{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

// request builds a whole HTTP/1.1 request: the request line, the field lines in
// the order given, and no body.
func request(requestLine string, fieldLines ...string) string {
	var b strings.Builder

	b.WriteString(requestLine)
	b.WriteString("\r\n")
	for _, line := range fieldLines {
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")

	return b.String()
}

// upstream is the HTTPS server the front proxies to, and the requests it
// received.
type upstream struct {
	addr   string
	server *httptest.Server

	mu       sync.Mutex
	received []recorded
}

// recorded is one request the upstream received.
type recorded struct {
	method string
	host   string
	path   string
	header http.Header
}

// startUpstream starts the TLS upstream on 127.0.0.1, with the leaf
// certificate the container trusts, on a port the kernel picks.
func startUpstream(leaf tls.Certificate) (*upstream, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	return serveUpstream(listener, leaf)
}

// serveUpstream answers on listener with the leaf certificate, recording every request it receives: the
// upstream the front proxies to.
func serveUpstream(listener net.Listener, leaf tls.Certificate) (*upstream, error) {
	u := &upstream{addr: listener.Addr().String()}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(u.handle))
	srv.Listener = listener
	srv.EnableHTTP2 = true
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()

	u.server = srv

	return u, nil
}

// startUntrustedUpstream answers on addr with a certificate a CA of its own signs, which the container does
// not trust: the front reaches it and refuses it in the TLS handshake, so its proxy answers its own 502 and
// Caddy logs the failure. The handshake itself is the answer here, so every connection is closed once it
// returns; the listener belongs to the test, which closes it when the test ends.
func startUntrustedUpstream(t *testing.T, addr string) {
	t.Helper()

	_, leaf, err := testCertificates()
	if err != nil {
		t.Fatalf("create the untrusted CA: %v", err)
	}

	listener, err := tls.Listen("tcp", addr, &tls.Config{Certificates: []tls.Certificate{leaf}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("listen on %s, the address Edge Services had: %v", addr, err)
	}

	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Logf("close the untrusted upstream on %s: %v", addr, err)
		}
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // the listener is closed: the test is over
			}

			go func(conn net.Conn) {
				defer conn.Close()

				// The client refuses the certificate during the handshake, which is the case here: its error is
				// the front's 502, and the connection ends with it.
				if tlsConn, ok := conn.(*tls.Conn); ok {
					_ = tlsConn.Handshake()
				}
			}(conn)
		}
	}()
}

// stop stops the upstream: from here on the front reaches nothing, which is the
// case of Edge Services answering nothing.
func (u *upstream) stop() {
	u.mu.Lock()
	server := u.server
	u.server = nil
	u.mu.Unlock()

	if server == nil {
		return
	}

	server.CloseClientConnections()
	server.Close()
}

// count returns how many requests the upstream received.
func (u *upstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()

	return len(u.received)
}

// request returns the first request the upstream received for path.
func (u *upstream) request(path string) (recorded, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	for _, r := range u.received {
		if r.path == path {
			return r, true
		}
	}

	return recorded{}, false
}

// handle records the request and answers by path: the site's page, a 404, its
// own copies of the three headers, or a gzip object.
func (u *upstream) handle(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.received = append(u.received, recorded{method: r.Method, host: r.Host, path: r.URL.Path, header: r.Header.Clone()})
	u.mu.Unlock()

	switch r.URL.Path {
	case ownHeadersPath:
		// Copies of the five headers Caddy also sets, with other values: the client must see Caddy's alone.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors https://upstream.example")
		w.Header().Set("Strict-Transport-Security", "max-age=1")
		w.Header().Set("X-Content-Type-Options", "nosniff-from-the-upstream")
		w.Header().Set("Referrer-Policy", "unsafe-url")
		w.Header().Set("Permissions-Policy", "camera=*")
		fmt.Fprint(w, "upstream's own headers")
	case missingPath:
		http.Error(w, "not found upstream", http.StatusNotFound)
	case gzipPath:
		w.Header().Set("Content-Type", gzipType)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Cache-Control", gzipCacheControl)
		w.Header().Set("Age", gzipAge)

		zw := gzip.NewWriter(w)
		if _, err := zw.Write([]byte(gzipBody)); err != nil {
			panic(http.ErrAbortHandler)
		}
		if err := zw.Close(); err != nil {
			panic(http.ErrAbortHandler)
		}
	default:
		fmt.Fprint(w, homeBody)
	}
}

// testCertificates returns a CA in PEM and a leaf certificate for 127.0.0.1
// signed by it: the container trusts the CA through SSL_CERT_FILE, so Caddy
// verifies the upstream's certificate as it does in production, and the
// Caddyfile stays the file the platform runs (docs/adr/0045).
func testCertificates() ([]byte, tls.Certificate, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tls.Certificate{}, err
	}

	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "shift2eu sitefront test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, tls.Certificate{}, err
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tls.Certificate{}, err
	}

	leaf := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, tls.Certificate{}, err
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	return caPEM, tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}, nil
}

// firstLine returns text up to its first line break, for a message.
func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}

	return text
}
