package livecheck_test

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the end-to-end checks of Run against local httptest servers,
// TLS included, reached through the injected dialer with an injected clock and
// sleep (behaviours 1 to 10): what the edge serves, the redirect matrix, the
// certificates, the records and the statements, the live version against the tag
// list, the module script's budget, and the retry bound. The unit tests of the
// parsers and of the headless rules are in the files beside this one.

// TestRunPassesAPassingSite pins the whole check on a correct site (behaviours 1
// to 10 together): the run returns a passing report of one round, every kind of
// check is in it, the responses carry the host, the remote address and the Age
// header, the module script is measured, the version is the matched tag, and
// every request was for a URL of the check's own building, without a query
// string, asking for gzip.
func TestRunPassesAPassingSite(t *testing.T) {
	t.Parallel()

	site := newSite(t)

	report, err := livecheck.Run(t.Context(), site.config(t))
	if err != nil {
		t.Fatalf("Run of a correct site failed: %v", err)
	}

	if !report.Passed() {
		t.Fatalf("the check of a correct site did not pass:\n%s", reportText(report))
	}

	if report.Rounds != 1 {
		t.Errorf("the run of a correct site took %d rounds, want 1: a passing round is never retried", report.Rounds)
	}

	if site.sleepCount() != 0 {
		t.Errorf("the run of a correct site slept %d times, want never", site.sleepCount())
	}

	if report.Host != livecheck.DefaultHost || report.Apex != livecheck.DefaultApex {
		t.Errorf("the report names the hosts (%q, %q), want (%q, %q)", report.Host, report.Apex, livecheck.DefaultHost, livecheck.DefaultApex)
	}

	if got := report.Version.String(); got != testVersion {
		t.Errorf("the report's version is %q, want %q: the matched tag, taken from the tag list", got, testVersion)
	}

	if !report.ModuleScript.Measured {
		t.Error("the report holds no module script measurement, want the module script's two sizes")
	}

	if got := report.ModuleScript.Encoding; got != livecheck.EncodingGzip {
		t.Errorf("the module script's recorded encoding is %q, want %q: the fixture serves it gzip compressed", got, livecheck.EncodingGzip)
	}

	if got, want := report.ModuleScript.Decoded, int64(len(moduleJS)); got != want {
		t.Errorf("the module script's decoded size is %d, want %d", got, want)
	}

	for _, kind := range []string{
		livecheck.KindManifestFile,
		livecheck.KindRoot,
		livecheck.KindServiceWorker,
		livecheck.KindHTMLCache,
		livecheck.KindRedirect,
		livecheck.KindCertificate,
		livecheck.KindHSTS,
		livecheck.KindStatement,
		livecheck.KindVersion,
		livecheck.KindModuleScriptSize,
	} {
		if !hasKind(report, kind) {
			t.Errorf("the report holds no %s check, want every check of a round:\n%s", kind, reportText(report))
		}
	}

	if len(report.Responses) == 0 {
		t.Fatalf("the report records no response, want one per request:\n%s", reportText(report))
	}

	aged := false

	for _, response := range report.Responses {
		if response.RemoteAddr == "" {
			t.Errorf("the response of %s records no remote address, want the address of the connection it came from", response.URL)
		}

		if !strings.HasPrefix(response.URL, "http://") && !strings.HasPrefix(response.URL, "https://") {
			t.Errorf("the report records the URL %q, want one the check built from the hosts", response.URL)
		}

		if response.Age.State == livecheck.AgeParsed && response.Age.Seconds == 42 {
			aged = true
		}
	}

	if !aged {
		t.Errorf("no response records the Age header 42 the fixture sends:\n%s", reportText(report))
	}

	for _, content := range site.allContent() {
		requests := content.requestLog()
		if len(requests) == 0 {
			t.Fatal("a fixture server saw no request, want the check to use all four (TLS and plain HTTP, both hosts)")
		}

		for _, request := range requests {
			if request.rawQuery != "" {
				t.Errorf("the check asked for %s?%s, want no request carrying a query string", request.path, request.rawQuery)
			}

			if !strings.Contains(strings.ToLower(request.acceptEncoding), "gzip") {
				t.Errorf("the request for %s asked for Accept-Encoding %q, want gzip", request.path, request.acceptEncoding)
			}
		}
	}
}

// TestRunFailsABrokenSite pins the hostile and broken cases one at a time on the
// fixture that otherwise passes: every one of them fails the check it belongs
// to, with a reason that names what is wrong, and none of them is an error or a
// crash (behaviours 1, 3, 4, 5, 6, 7, 9 and 10).
func TestRunFailsABrokenSite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       []siteOption
		mutate     func(t *testing.T, site *site)
		wantKind   string
		wantTarget string
		// wantReason, when set, is a part of the failing check's reason.
		wantReason string
	}{
		{
			name: "a root that is not index.html",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setBody("/", []byte("<!DOCTYPE html><p>not the release's index.html</p>"))
			},
			wantKind:   livecheck.KindRoot,
			wantTarget: "/",
		},
		{
			name: "a root that is not index.html is not scanned for the version",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setBody("/", []byte(`<html><body><p>a page that is not the release's index.html</p></body></html>`))
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
			wantReason: "index.html",
		},
		{
			name: "a manifest file whose bytes changed",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.digests["style.css"] = strings.Repeat("0", 64)
			},
			wantKind:   livecheck.KindManifestFile,
			wantTarget: "style.css",
		},
		{
			name: "a manifest file answering a redirect without a Location",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/style.css", served{status: http.StatusFound})
			},
			wantKind:   livecheck.KindManifestFile,
			wantTarget: "style.css",
		},
		{
			name: "a manifest file answering the edge's own redirect",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/style.css", served{
					status: http.StatusMovedPermanently,
					header: http.Header{"X-Amz-Website-Redirect-Location": {"https://" + livecheck.DefaultHost + "/style.css"}},
				})
			},
			wantKind:   livecheck.KindManifestFile,
			wantTarget: "style.css",
		},
		{
			name: "a manifest file that is not there",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.digests["style.css"] = sha256Hex(styleCSS)
				site.hostTLS.remove("/style.css")
			},
			wantKind:   livecheck.KindManifestFile,
			wantTarget: "style.css",
		},
		{
			name: "a manifest file in an encoding the check does not know",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setHeader("/style.css", "Content-Encoding", "br")
			},
			wantKind:   livecheck.KindManifestFile,
			wantTarget: "style.css",
		},
		{
			name: "a manifest file whose body never ends",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.maxBodyBytes = livecheck.DecodedBudget + 1
				site.hostTLS.set("/style.css", served{endless: 12 << 20})
			},
			wantKind:   livecheck.KindManifestFile,
			wantTarget: "style.css",
		},
		{
			name: "a service worker script that answers 200",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/sw.js", served{body: []byte("self.addEventListener('install', () => {})")})
			},
			wantKind:   livecheck.KindServiceWorker,
			wantTarget: "/sw.js",
		},
		{
			name: "another service worker script that answers 200",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/worker.js", served{body: []byte("postMessage('hello')")})
			},
			wantKind:   livecheck.KindServiceWorker,
			wantTarget: "/worker.js",
		},
		{
			name: "a service worker script that answers 203",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/sw.js", served{status: http.StatusNonAuthoritativeInfo, body: []byte("self.addEventListener('install', () => {})")})
			},
			wantKind:   livecheck.KindServiceWorker,
			wantTarget: "/sw.js",
		},
		{
			name: "a service worker script that answers 206",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/sw.js", served{status: http.StatusPartialContent, body: []byte("postMessage('hello')")})
			},
			wantKind:   livecheck.KindServiceWorker,
			wantTarget: "/sw.js",
		},
		{
			name: "a service worker script that answers 204",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/worker.js", served{status: http.StatusNoContent})
			},
			wantKind:   livecheck.KindServiceWorker,
			wantTarget: "/worker.js",
		},
		{
			name: "a service worker path with no response at all",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.routes.remove(livecheck.DefaultHost + ":443")
			},
			wantKind:   livecheck.KindServiceWorker,
			wantTarget: "/sw.js",
		},
		{
			name: "an HTML page cached for longer than the HTML's max-age",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setHeader("/privacy.html", "Cache-Control", "public, max-age=301")
			},
			wantKind:   livecheck.KindHTMLCache,
			wantTarget: "privacy.html",
		},
		{
			name: "a root without a Cache-Control",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.removeHeader("/", "Cache-Control")
			},
			wantKind:   livecheck.KindHTMLCache,
			wantTarget: "/",
		},
		{
			name: "an HTML page with two conflicting max-age directives",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setHeader("/privacy.html", "Cache-Control", "max-age=60, max-age=301")
			},
			wantKind:   livecheck.KindHTMLCache,
			wantTarget: "privacy.html",
		},
		{
			name: "an HTML page whose s-maxage is over the HTML's max-age",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setHeader("/privacy.html", "Cache-Control", "max-age=60, s-maxage=301")
			},
			wantKind:   livecheck.KindHTMLCache,
			wantTarget: "privacy.html",
		},
		{
			name: "the apex answering plain HTTP with a 302",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.apexHTTP.set("/", served{status: http.StatusFound, header: redirect("https://" + livecheck.DefaultHost + "/")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultApex + "/",
		},
		{
			name: "the apex adding a query string",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.apexHTTP.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/?from=apex")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultApex + "/",
		},
		{
			name: "the apex redirecting to another host",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.apexHTTP.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + ".evil.example/")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultApex + "/",
		},
		{
			name: "the apex over plain HTTP adding a hop the records do not allow",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.apexHTTP.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultApex + "/")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultApex + "/",
		},
		{
			name: "the apex over HTTPS answering 200",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.apexTLS.set("/", served{body: []byte("welcome")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "https://" + livecheck.DefaultApex + "/",
		},
		{
			name: "plain HTTP on the canonical host answering 200",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostHTTP.set("/", served{body: indexPage(testVersion)})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			name: "refuse-or-close: a 403 whose body is a manifest file's bytes",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.hostHTTP.set("/", served{status: http.StatusForbidden, body: indexPage(testVersion)})
				site.hostHTTP.set("/privacy.html", served{status: http.StatusForbidden, body: indexPage(testVersion)})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			name: "refuse-or-close: plain HTTP answering raw HTML without a status line",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.routes.set(livecheck.DefaultHost+":80", rawListener(t, "<html><body><p>welcome</p></body></html>", rawReadTheRequest))
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			name: "refuse-or-close: plain HTTP answering a header block Go refuses",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.routes.set(livecheck.DefaultHost+":80", rawListener(t, "HTTP/1.1 200 OK\r\nBad Header\r\n\r\n<html><body><p>welcome</p></body></html>", rawReadTheRequest))
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			// The order an edge that writes as soon as it accepts the connection
			// reaches: Go's transport reads those bytes before the request is
			// registered and discards them as unsolicited, so the check used to pass
			// a page a browser renders. verdictFirst forces that order.
			name: "refuse-or-close: plain HTTP answering raw HTML before it reads the request",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.routes.set(livecheck.DefaultHost+":80", rawListener(t, "<html><body><p>welcome</p></body></html>", rawWriteOnAccept))
				forceVerdictFirst(site, livecheck.DefaultHost+":80")
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			name: "refuse-or-close: plain HTTP answering a header block Go refuses before it reads the request",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.routes.set(livecheck.DefaultHost+":80", rawListener(t, "HTTP/1.1 200 OK\r\nBad Header\r\n\r\n<html><body><p>welcome</p></body></html>", rawWriteOnAccept))
				forceVerdictFirst(site, livecheck.DefaultHost+":80")
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			// The short request timeout this case needs to end the stalled
			// request applies to the whole run, so an HTTPS fetch of the same
			// run may time out under load too; the case wants a failing report,
			// and the check it asserts fails for a reason load cannot flip (a
			// connection was made), so that is no flake. The timeout cannot be
			// scoped to this host through the dialer: a dial cannot make a
			// served request end.
			name: "refuse-or-close: plain HTTP accepting the connection and never answering",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.requestTimeout = 100 * time.Millisecond
				site.routes.set(livecheck.DefaultHost+":80", stalledListener(t))
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			name: "refuse-or-close: a 302 to https://<host>/",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.hostHTTP.set("/", served{status: http.StatusFound, header: redirect("https://" + livecheck.DefaultHost + "/")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			name: "refuse-or-close: a 301 to another URL",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.hostHTTP.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/elsewhere")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultHost + "/",
		},
		{
			name: "the preload chain whose second hop is another host",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.pursuePreload(t)
				site.apexTLS.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://elsewhere.example/")})
				site.apexTLS.set("/privacy.html", served{status: http.StatusMovedPermanently, header: redirect("https://elsewhere.example/privacy.html")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultApex + "/",
		},
		{
			name: "the preload chain whose second hop is a 302",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.pursuePreload(t)
				site.apexTLS.set("/", served{status: http.StatusFound, header: redirect("https://" + livecheck.DefaultHost + "/")})
				site.apexTLS.set("/privacy.html", served{status: http.StatusFound, header: redirect("https://" + livecheck.DefaultHost + "/privacy.html")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultApex + "/",
		},
		{
			name: "the preload chain whose second hop is a third hop",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.pursuePreload(t)
				site.apexTLS.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultApex + "/home")})
				site.apexTLS.set("/privacy.html", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultApex + "/home")})
			},
			wantKind:   livecheck.KindRedirect,
			wantTarget: "http://" + livecheck.DefaultApex + "/",
		},
		{
			name: "a gzip body whose digest is the manifest's before decoding",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				compressed := gzipBytes(t, styleCSS)

				site.hostTLS.set("/style.css", served{
					header:  http.Header{"Content-Encoding": {"gzip"}},
					body:    compressed,
					decoded: styleCSS,
				})
				site.digests["style.css"] = sha256Hex(compressed)
			},
			wantKind:   livecheck.KindManifestFile,
			wantTarget: "style.css",
		},
		{
			name:       "a host certificate expiring inside the margin",
			opts:       []siteOption{withHostLeaf(leafExpiringWithin(7*24*time.Hour, livecheck.DefaultHost))},
			wantKind:   livecheck.KindCertificate,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name:       "an apex certificate expiring inside the margin",
			opts:       []siteOption{withApexLeaf(leafExpiringWithin(7*24*time.Hour, livecheck.DefaultApex))},
			wantKind:   livecheck.KindCertificate,
			wantTarget: livecheck.DefaultApex,
		},
		{
			name:       "a host certificate for another name",
			opts:       []siteOption{withHostLeaf(leafValidFor("example.com"))},
			wantKind:   livecheck.KindCertificate,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name:       "a host certificate from an authority the check does not trust",
			opts:       []siteOption{withRoots(x509.NewCertPool())},
			wantKind:   livecheck.KindCertificate,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name: "a version the tag list does not hold",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setVersion("v1.2.4")
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
		},
		{
			name: "a version that is a tag but not the manifest's version",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setVersion("v1.2.4")
				site.tags = []string{testVersion, "v1.2.4"}
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
		},
		{
			name: "a version with a trailing newline",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setVersion(testVersion + "\n")
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
		},
		{
			name: "a version with a leading space",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setVersion(" " + testVersion)
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
		},
		{
			name: "a release candidate version",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setVersion("v1.2.3-rc1")
				site.tags = []string{"v1.2.3-rc1"}
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
		},
		{
			name: "an upper case version",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setVersion("V1.2.3")
				site.tags = []string{"V1.2.3"}
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
		},
		{
			name: "a 10 KiB version",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setVersion(strings.Repeat(testVersion, 1748))
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
		},
		{
			name: "a root page with no version element",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setVersion("")
			},
			wantKind:   livecheck.KindVersion,
			wantTarget: "/",
		},
		{
			name: "a module script with the wrong Content-Type",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setHeader("/"+testModuleScript, "Content-Type", "application/javascript")
			},
			wantKind:   livecheck.KindModuleScriptSize,
			wantTarget: testModuleScript,
		},
		{
			name: "a module script transferred over the transfer budget",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				script := make([]byte, livecheck.TransferBudget+1)
				if _, err := rand.Read(script); err != nil {
					t.Fatalf("make the incompressible module script: %v", err)
				}

				site.hostTLS.set("/"+testModuleScript, served{
					header:  http.Header{"Content-Encoding": {"gzip"}, "Content-Type": {"text/javascript"}},
					body:    gzipBytes(t, script),
					decoded: script,
				})
			},
			wantKind:   livecheck.KindModuleScriptSize,
			wantTarget: testModuleScript,
		},
		{
			name: "a module script decoded over the decoded budget",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				script := make([]byte, livecheck.DecodedBudget+1)

				site.hostTLS.set("/"+testModuleScript, served{
					header:  http.Header{"Content-Encoding": {"gzip"}, "Content-Type": {"text/javascript"}},
					body:    gzipBytes(t, script),
					decoded: script,
				})
			},
			wantKind:   livecheck.KindModuleScriptSize,
			wantTarget: testModuleScript,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			site := newSite(t, tc.opts...)
			if tc.mutate != nil {
				tc.mutate(t, site)
			}

			report, err := runWithin(t, 60*time.Second, site.config(t))
			if err != nil {
				t.Fatalf("Run failed: %v, want a report of a failed check: a check failure is no error", err)
			}

			if site.afterRun != nil {
				site.afterRun(t)
			}

			if report.Passed() {
				t.Fatalf("the check of the site passed, want the %s check of %q to fail: %s", tc.wantKind, tc.wantTarget, tc.name)
			}

			check, ok := findCheck(report, tc.wantKind, tc.wantTarget)
			if !ok {
				t.Fatalf("the report holds no %s check of %q, want a failing one: %s\n%s", tc.wantKind, tc.wantTarget, tc.name, reportText(report))
			}

			if check.Passed {
				t.Errorf("the %s check of %q passed, want it to fail: %s\n%s", tc.wantKind, tc.wantTarget, tc.name, reportText(report))
			}

			if check.Reason == "" {
				t.Errorf("the failing %s check of %q says nothing about why, want a reason: %s", tc.wantKind, tc.wantTarget, tc.name)
			}

			if tc.wantReason != "" && !strings.Contains(check.Reason, tc.wantReason) {
				t.Errorf("the failing %s check of %q fails with %q, want the reason to say %q: %s", tc.wantKind, tc.wantTarget, check.Reason, tc.wantReason, tc.name)
			}
		})
	}
}

// TestRunPassesTheAcceptedVariants pins the exceptions the spec allows: a body
// whose digest is over its decoded bytes, a service worker path that redirects
// rather than answers, the refuse-or-close mode of plain HTTP on the canonical
// host, the preload chain, the waived statement and the identity-served module
// script (behaviours 1, 4, 6, 8 and 10).
func TestRunPassesTheAcceptedVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(t *testing.T, site *site)
	}{
		{
			name: "a gzip body whose digest is over the decoded bytes",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/style.css", served{
					header:  http.Header{"Content-Encoding": {"gzip"}},
					body:    gzipBytes(t, styleCSS),
					decoded: styleCSS,
				})
			},
		},
		{
			name: "a service worker path that redirects",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/sw.js", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/")})
			},
		},
		{
			name: "refuse-or-close: plain HTTP on the canonical host refuses the connection",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.routes.remove(livecheck.DefaultHost + ":80")
			},
		},
		{
			name: "refuse-or-close: plain HTTP on the canonical host closes without a response",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.routes.set(livecheck.DefaultHost+":80", closingListener(t))
			},
		},
		{
			// A port a firewall drops: nothing refuses, nothing answers, and the
			// dial ends at once with a timeout and no connection made, as a dial
			// whose deadline passed does. A browser gets no page from it any
			// more than from a refusal, so refuse-or-close accepts it; a timeout
			// with a connection made behind it still fails (the stalled case
			// above). The dial ends by itself, so this run keeps the default
			// request timeout: a short one applied to every request flaked the
			// HTTPS fetches, each with a TLS handshake of its own.
			name: "refuse-or-close: plain HTTP on the canonical host filtered, the dial timing out with no connection",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.dial = filteredDial(site.routes.dial, livecheck.DefaultHost+":80")
			},
		},
		{
			name: "refuse-or-close: a refusal whose body matches no manifest file",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.hostHTTP.set("/", served{status: http.StatusForbidden, body: []byte("refused")})
				site.hostHTTP.set("/privacy.html", served{status: http.StatusForbidden, body: []byte("refused")})
			},
		},
		{
			name: "refuse-or-close: a redirect is accepted too",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
			},
		},
		{
			name: "refuse-or-close: a 308 to exactly https://<host>/<p> is accepted too",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.plainHTTPWWW = livecheck.PlainHTTPRefuseOrClose
				site.hostHTTP.set("/", served{status: http.StatusPermanentRedirect, header: redirect("https://" + livecheck.DefaultHost + "/")})
				site.hostHTTP.set("/privacy.html", served{status: http.StatusPermanentRedirect, header: redirect("https://" + livecheck.DefaultHost + "/privacy.html")})
			},
		},
		{
			name: "the preload chain when preload is pursued",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.pursuePreload(t)
			},
		},
		{
			name: "a waived HSTS record whose pages carry the statement, with no HSTS header at all",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTS = parsedHSTSRecord(t, hstsRecordWaived)
				site.setHSTS("")
				site.setApexHSTS("")
				site.setPrivacy(true, true)
				site.setVerify(true)
			},
		},
		{
			name: "a domain on the preload list, preload pursued, needs no downgrade-risk statement",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.pursuePreload(t)
				site.records.Preload = parsedPreloadRecord(t, preloadRecordListed)
				site.setPrivacy(false, false)
			},
		},
		{
			name: "an s-maxage of exactly the HTML's max-age",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setHeader("/privacy.html", "Cache-Control", "max-age=300, s-maxage=300")
			},
		},
		{
			name: "a module script served without compression",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.set("/"+testModuleScript, served{
					header: http.Header{"Content-Type": {"text/javascript; charset=utf-8"}},
					body:   moduleJS,
				})
			},
		},
		{
			name: "an Age header that does not parse never fails a check",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setHeader("/", "Age", "soon")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			site := newSite(t)
			tc.mutate(t, site)

			report, err := livecheck.Run(t.Context(), site.config(t))
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if !report.Passed() {
				t.Fatalf("the check failed, want it to pass: %s\n%s", tc.name, reportText(report))
			}
		})
	}
}

// TestRunHSTSAndStatements pins behaviour 8: the Strict-Transport-Security
// minimum the record's two states hold the site to, the statements the pages must
// carry, and the fail-safe of a record that is missing or cannot be read.
func TestRunHSTSAndStatements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(t *testing.T, site *site)
		wantKind   string
		wantTarget string
	}{
		{
			name: "a max-age shorter than a year",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setHSTS("max-age=86400; includeSubDomains; preload")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name: "a max-age with a trailing character",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setHSTS("max-age=31536000x; includeSubDomains; preload")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name: "a max-age that is quoted and then continued",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setHSTS(`max-age="31536000"x; includeSubDomains; preload`)
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name: "two max-age directives",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setHSTS("max-age=31536000; max-age=0; includeSubDomains; preload")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name: "no includeSubDomains",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setHSTS("max-age=31536000; preload")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name: "no HSTS header at all",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.setHSTS("")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name: "preload pursued and not sent",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.pursuePreload(t)
				site.setHSTS("max-age=31536000; includeSubDomains")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: livecheck.DefaultHost,
		},
		{
			name: "the apex's HSTS weaker than the minimum while preload is pursued",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.pursuePreload(t)
				site.setApexHSTS("max-age=60; includeSubDomains; preload")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: livecheck.DefaultApex,
		},
		{
			name: "a waived HSTS record whose pages carry no statement",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTS = parsedHSTSRecord(t, hstsRecordWaived)
			},
			wantKind:   livecheck.KindStatement,
			wantTarget: "privacy.html#" + livecheck.MarkerHSTSWaived,
		},
		{
			name: "a waived HSTS record whose statement is hidden",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTS = parsedHSTSRecord(t, hstsRecordWaived)
				site.hostTLS.setBody("/privacy.html", []byte(`<p id="hsts-waived" hidden>HSTS is waived.</p><p id="downgrade-risk">The first visit is exposed.</p>`))
				site.hostTLS.setBody("/verify.html", []byte(`<p id="hsts-waived">HSTS is waived.</p>`))
			},
			wantKind:   livecheck.KindStatement,
			wantTarget: "privacy.html#" + livecheck.MarkerHSTSWaived,
		},
		{
			name: "an empty downgrade-risk statement",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.hostTLS.setBody("/privacy.html", []byte(`<p id="downgrade-risk"> </p>`))
			},
			wantKind:   livecheck.KindStatement,
			wantTarget: "privacy.html#" + livecheck.MarkerDowngradeRisk,
		},
		{
			name: "a missing HSTS record",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTSErr = errors.New("open site/records/hsts.json: no such file or directory")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: "hsts.json",
		},
		{
			name: "an unreadable HSTS record",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTSErr = errors.New("hsts.json: format 2, want 1")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: "hsts.json",
		},
		{
			name: "a missing preload record",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.PreloadErr = errors.New("open site/records/preload.json: no such file or directory")
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: "preload.json",
		},
		{
			name: "a missing preload record still requires the downgrade-risk statement",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.PreloadErr = errors.New("open site/records/preload.json: no such file or directory")
				site.setPrivacy(false, false)
			},
			wantKind:   livecheck.KindStatement,
			wantTarget: "privacy.html#" + livecheck.MarkerDowngradeRisk,
		},
		// The two records meet here: preload.json's listed true is only usable
		// with hsts.json's preload true, so a site that claims the list without
		// it fails safe, and a record that cannot be read can never back the
		// claim.
		{
			name: "the preload list claimed while the HSTS record does not pursue preload",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.Preload = parsedPreloadRecord(t, preloadRecordListed)
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: "preload.json",
		},
		{
			name: "the preload list claimed while the HSTS record is missing",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTSErr = errors.New("open site/records/hsts.json: no such file or directory")
				site.records.Preload = parsedPreloadRecord(t, preloadRecordListed)
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: "preload.json",
		},
		{
			name: "the preload list claimed while the HSTS record is waived",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTS = parsedHSTSRecord(t, hstsRecordWaived)
				site.records.Preload = parsedPreloadRecord(t, preloadRecordListed)
				site.setPrivacy(true, true)
				site.setVerify(true)
			},
			wantKind:   livecheck.KindHSTS,
			wantTarget: "preload.json",
		},
		{
			name: "the preload list claimed without preload pursued still requires the downgrade-risk statement",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.Preload = parsedPreloadRecord(t, preloadRecordListed)
				site.setPrivacy(false, false)
			},
			wantKind:   livecheck.KindStatement,
			wantTarget: "privacy.html#" + livecheck.MarkerDowngradeRisk,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			site := newSite(t)
			tc.mutate(t, site)

			report, err := livecheck.Run(t.Context(), site.config(t))
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if report.Passed() {
				t.Fatalf("the check passed, want the %s check of %q to fail: %s", tc.wantKind, tc.wantTarget, tc.name)
			}

			check, ok := findCheck(report, tc.wantKind, tc.wantTarget)
			if !ok {
				t.Fatalf("the report holds no %s check of %q, want a failing one: %s\n%s", tc.wantKind, tc.wantTarget, tc.name, reportText(report))
			}

			if check.Passed {
				t.Errorf("the %s check of %q passed, want it to fail: %s\n%s", tc.wantKind, tc.wantTarget, tc.name, reportText(report))
			}
		})
	}
}

// TestCheckCertificate pins behaviour 7 in isolation: a leaf that verifies for
// the host and stays valid past the margin passes and reports its expiry; one
// expiring inside the margin, one for another name and one from an untrusted
// authority fail; and a margin below 14 days is refused before any connection.
func TestCheckCertificate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		leaf    leafBuilder
		roots   func(ca *testCA) *x509.CertPool
		margin  time.Duration
		wantErr bool
	}{
		{
			name: "a certificate valid for the host past the margin",
			leaf: leafValidFor(livecheck.DefaultHost),
		},
		{
			name:    "a certificate expiring inside the margin",
			leaf:    leafExpiringWithin(7*24*time.Hour, livecheck.DefaultHost),
			wantErr: true,
		},
		{
			name:    "a leaf whose intermediate expires inside the margin",
			leaf:    leafThroughAnIntermediateExpiringWithin(7*24*time.Hour, livecheck.DefaultHost),
			wantErr: true,
		},
		{
			name: "a leaf whose intermediate stays valid past the margin",
			leaf: leafThroughAValidIntermediate(livecheck.DefaultHost),
		},
		{
			name:    "a certificate for another name",
			leaf:    leafValidFor("example.com"),
			wantErr: true,
		},
		{
			name:    "a certificate from an untrusted authority",
			leaf:    leafValidFor(livecheck.DefaultHost),
			roots:   func(*testCA) *x509.CertPool { return x509.NewCertPool() },
			wantErr: true,
		},
		{
			name:    "a margin below 14 days",
			leaf:    leafValidFor(livecheck.DefaultHost),
			margin:  livecheck.MinCertMargin - time.Hour,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ca := newTestCA(t)

			roots := ca.pool
			if tc.roots != nil {
				roots = tc.roots(ca)
			}

			margin := tc.margin
			if margin == 0 {
				margin = livecheck.MinCertMargin
			}

			routes := newRouteTable()

			cert := tc.leaf(t, ca)
			srv := startTLSServer(t, http.NotFoundHandler(), cert)
			routes.set(livecheck.DefaultHost+":443", srv.Listener.Addr().String())

			notAfter, err := livecheck.CheckCertificate(t.Context(), routes.dial, roots, livecheck.DefaultHost, testNow, margin)

			if tc.margin != 0 && tc.margin < livecheck.MinCertMargin {
				if err == nil {
					t.Fatalf("CheckCertificate with a margin of %v returned %v and no error, want a configuration error", tc.margin, notAfter)
				}

				if dialed := routes.dialLog(); len(dialed) != 0 {
					t.Errorf("CheckCertificate dialled %v before refusing a margin below 14 days, want no connection", dialed)
				}

				return
			}

			if tc.wantErr {
				if err == nil {
					t.Fatalf("CheckCertificate returned the expiry %v and no error, want an error: %s", notAfter, tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("CheckCertificate failed: %v", err)
			}

			leaf, parseErr := x509.ParseCertificate(cert.Certificate[0])
			if parseErr != nil {
				t.Fatalf("parse the fixture's certificate: %v", parseErr)
			}

			if !notAfter.Equal(leaf.NotAfter) {
				t.Errorf("CheckCertificate reports the expiry %v, want the leaf's %v", notAfter, leaf.NotAfter)
			}
		})
	}
}

// TestCheckCertificateKeepsTheCallersDeadline pins what CheckCertificate grants
// the dial of a ctx that already has a deadline: that deadline itself, longer
// than DefaultRequestTimeout included, and DefaultRequestTimeout only for a ctx
// with no deadline. The dialer records the deadline it is called with and fails
// at once, so a deadline longer than the default costs no wall clock.
func TestCheckCertificateKeepsTheCallersDeadline(t *testing.T) {
	t.Parallel()

	longer := time.Now().Add(2 * livecheck.DefaultRequestTimeout)
	shorter := time.Now().Add(time.Second)

	tests := []struct {
		name     string
		deadline time.Time
	}{
		{name: "a deadline longer than the default request timeout", deadline: longer},
		{name: "a deadline shorter than it", deadline: shorter},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithDeadline(t.Context(), tc.deadline)
			defer cancel()

			dial := &deadlineDial{}

			_, err := livecheck.CheckCertificate(ctx, dial.dial, nil, livecheck.DefaultHost, testNow, livecheck.MinCertMargin)
			if err == nil {
				t.Fatalf("CheckCertificate returned no error though its dial failed, want the dial's error")
			}

			if !dial.recorded {
				t.Fatalf("CheckCertificate dialled with no deadline, want the caller's %v", tc.deadline)
			}

			if !dial.deadline.Equal(tc.deadline) {
				t.Errorf("CheckCertificate dialled with the deadline %v, want the caller's %v: a caller's deadline, shorter or longer than the default %v, is kept as it is", dial.deadline, tc.deadline, livecheck.DefaultRequestTimeout)
			}
		})
	}
}

// deadlineDial records the deadline of the context its dial is called with and
// makes no connection.
type deadlineDial struct {
	deadline time.Time
	recorded bool
}

// dial records the context's deadline and returns the error of a dial that
// reached no host.
func (d *deadlineDial) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	d.deadline, d.recorded = ctx.Deadline()

	return nil, errors.New("the test's dial makes no connection")
}

// TestRunBoundsTheTLSHandshake pins that a host that accepts the TCP connection
// and never sends a ServerHello makes the certificate check fail within
// Config.RequestTimeout, not hang the run: the transport's dial and read
// timeouts bound the plain HTTP requests, and the handshake CheckCertificate
// does must be bounded by the same timeout (auditor finding 1, reviewer 2).
// The check runs in a goroutine under a guard, so an unbounded handshake fails
// this test instead of hanging the package's run; the guard is loose (the check
// measures about 2 s) because it catches a hang, never a duration. The short
// request timeout the case needs applies to the whole run, so a fetch of the
// same run may time out under load too; the case wants a failing certificate
// check, which a stalled handshake cannot turn into a passing one, so that is
// no flake. It cannot be scoped to this host through the dialer: what must end
// is a handshake CheckCertificate does, not a dial.
func TestRunBoundsTheTLSHandshake(t *testing.T) {
	t.Parallel()

	site := newSite(t)
	site.requestTimeout = 100 * time.Millisecond
	site.routes.set(livecheck.DefaultHost+":443", stalledListener(t))

	report, err := runWithin(t, 60*time.Second, site.config(t))
	if err != nil {
		t.Fatalf("Run failed: %v, want a report of a failed certificate check", err)
	}

	check, ok := findCheck(report, livecheck.KindCertificate, livecheck.DefaultHost)
	if !ok {
		t.Fatalf("the report holds no %s check of %q, want a failing one:\n%s", livecheck.KindCertificate, livecheck.DefaultHost, reportText(report))
	}

	if check.Passed {
		t.Errorf("the %s check of %q passed against a host that never sends a ServerHello, want it to fail:\n%s", livecheck.KindCertificate, livecheck.DefaultHost, reportText(report))
	}

	if check.Reason == "" {
		t.Error("the failing certificate check says nothing about why, want a reason")
	}
}

// TestRunRetriesUntilTheSiteIsCorrect pins the retry of behaviour 2 and D4: a
// site that is wrong on the first round and right on the second passes, after
// one sleep of the injected clock and no real waiting.
func TestRunRetriesUntilTheSiteIsCorrect(t *testing.T) {
	t.Parallel()

	site := newSite(t)
	site.retryInterval = 30 * time.Second
	site.hostTLS.setBody("/", indexPage("v0.0.0-gone"))

	corrected := false

	site.onSleep = func(_ context.Context, _ time.Duration) error {
		if corrected {
			return nil
		}

		corrected = true
		site.hostTLS.setBody("/", indexPage(testVersion))

		return nil
	}

	report, err := livecheck.Run(t.Context(), site.config(t))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !report.Passed() {
		t.Fatalf("the check of a site that became correct on the second round failed:\n%s", reportText(report))
	}

	if report.Rounds != 2 {
		t.Errorf("the check took %d rounds, want 2: the first round failed and the second passed", report.Rounds)
	}

	if got := site.sleepCount(); got != 1 {
		t.Errorf("the check slept %d times, want once between the two rounds", got)
	}
}

// TestRunGivesUpAfterTheRetryBound pins behaviour 2 and D4's bound: a site that
// is never correct fails after the rounds the bound of 300 s and an interval of
// 30 s allow, eleven rounds and ten sleeps, with the last round's report.
func TestRunGivesUpAfterTheRetryBound(t *testing.T) {
	t.Parallel()

	site := newSite(t)
	site.retryInterval = 30 * time.Second
	site.hostTLS.setBody("/", indexPage("v0.0.0-gone"))

	report, err := livecheck.Run(t.Context(), site.config(t))
	if err != nil {
		t.Fatalf("Run failed: %v, want a report of a failed check after the retry bound", err)
	}

	if report.Passed() {
		t.Fatalf("the check of a site that never became correct passed:\n%s", reportText(report))
	}

	wantRounds := int(livecheck.MinRetryBound/site.retryInterval) + 1
	if report.Rounds != wantRounds {
		t.Errorf("the check took %d rounds, want %d: the bound of %v and the interval of %v allow no more",
			report.Rounds, wantRounds, livecheck.MinRetryBound, site.retryInterval)
	}

	if got, want := site.sleepCount(), wantRounds-1; got != want {
		t.Errorf("the check slept %d times, want %d: one between each two rounds and none after the last", got, want)
	}

	check, ok := findCheck(report, livecheck.KindRoot, "/")
	if !ok || check.Passed {
		t.Errorf("the last round's report holds no failing root check, want the last round's checks:\n%s", reportText(report))
	}
}

// TestRunDoesNotWaitOutTheRetryBoundForARecord pins that a check no round can
// change — a record that is missing or unreadable — is reported after one round
// instead of after the whole retry bound (reviewer 11), while a site check that
// a later round could fix still retries as TestRunGivesUpAfterTheRetryBound
// pins.
func TestRunDoesNotWaitOutTheRetryBoundForARecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(t *testing.T, site *site)
		wantRounds int
	}{
		{
			name: "only the record checks fail",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTSErr = errors.New("open site/records/hsts.json: no such file or directory")
				site.records.PreloadErr = errors.New("open site/records/preload.json: no such file or directory")
			},
			wantRounds: 1,
		},
		{
			name: "a site check that a later round could fix fails too",
			mutate: func(t *testing.T, site *site) {
				t.Helper()

				site.records.HSTSErr = errors.New("open site/records/hsts.json: no such file or directory")
				site.hostTLS.setBody("/", indexPage("v0.0.0-gone"))
			},
			wantRounds: int(livecheck.MinRetryBound/(30*time.Second)) + 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			site := newSite(t)
			site.retryInterval = 30 * time.Second
			tc.mutate(t, site)

			report, err := livecheck.Run(t.Context(), site.config(t))
			if err != nil {
				t.Fatalf("Run failed: %v, want a report of the failed record checks", err)
			}

			if report.Passed() {
				t.Fatalf("the check of a site whose records cannot be read passed:\n%s", reportText(report))
			}

			if report.Rounds != tc.wantRounds {
				t.Errorf("the check took %d rounds, want %d: a record no round can change is reported after one round, a site check retries", report.Rounds, tc.wantRounds)
			}

			if got, want := site.sleepCount(), tc.wantRounds-1; got != want {
				t.Errorf("the check slept %d times, want %d", got, want)
			}
		})
	}
}

// TestConfigValidate pins the configuration errors of behaviour 2 and of the
// margins (7): a retry bound below 300 s, a certificate margin below 14 days, a
// retry interval outside its bounds, a missing clock or sleep, a bad host, a
// manifest that is not a site's, and a body cap below the decoded budget are all
// refused before any connection; the records and the tag list never are.
func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(cfg *livecheck.Config)
		wantErr bool
	}{
		{name: "a valid configuration"},
		{name: "a retry bound below the HTML's max-age", mutate: func(cfg *livecheck.Config) { cfg.RetryBound = livecheck.MinRetryBound - time.Second }, wantErr: true},
		{name: "no retry bound", mutate: func(cfg *livecheck.Config) { cfg.RetryBound = 0 }, wantErr: true},
		{name: "no retry interval", mutate: func(cfg *livecheck.Config) { cfg.RetryInterval = 0 }, wantErr: true},
		{name: "a retry interval over the bound", mutate: func(cfg *livecheck.Config) { cfg.RetryInterval = cfg.RetryBound + time.Second }, wantErr: true},
		{name: "a certificate margin below 14 days", mutate: func(cfg *livecheck.Config) { cfg.CertMargin = 13 * 24 * time.Hour }, wantErr: true},
		{name: "no clock", mutate: func(cfg *livecheck.Config) { cfg.Now = nil }, wantErr: true},
		{name: "no sleep", mutate: func(cfg *livecheck.Config) { cfg.Sleep = nil }, wantErr: true},
		{name: "no host", mutate: func(cfg *livecheck.Config) { cfg.Host = "" }, wantErr: true},
		{name: "no apex", mutate: func(cfg *livecheck.Config) { cfg.Apex = "" }, wantErr: true},
		{name: "the host is the apex", mutate: func(cfg *livecheck.Config) { cfg.Apex = cfg.Host }, wantErr: true},
		{name: "a host with a port", mutate: func(cfg *livecheck.Config) { cfg.Host = "www.shift2eu.com:443" }, wantErr: true},
		{name: "a host with a space", mutate: func(cfg *livecheck.Config) { cfg.Host = "www shift2eu com" }, wantErr: true},
		{name: "a host with a scheme", mutate: func(cfg *livecheck.Config) { cfg.Apex = "https://shift2eu.com" }, wantErr: true},
		{
			name: "a manifest without index.html",
			mutate: func(cfg *livecheck.Config) {
				cfg.Manifest.Files = without(cfg.Manifest.Files, "index.html")
			},
			wantErr: true,
		},
		{
			name: "a manifest without privacy.html",
			mutate: func(cfg *livecheck.Config) {
				cfg.Manifest.Files = without(cfg.Manifest.Files, "privacy.html")
			},
			wantErr: true,
		},
		{
			name: "a manifest without verify.html",
			mutate: func(cfg *livecheck.Config) {
				cfg.Manifest.Files = without(cfg.Manifest.Files, "verify.html")
			},
			wantErr: true,
		},
		{name: "a manifest with no files", mutate: func(cfg *livecheck.Config) { cfg.Manifest.Files = nil }, wantErr: true},
		{
			name: "a manifest listing a service worker path",
			mutate: func(cfg *livecheck.Config) {
				cfg.Manifest.Files = append(cfg.Manifest.Files, livecheck.ManifestFile{Path: "sw.js", SHA256: manifestDigest})
			},
			wantErr: true,
		},
		{
			name: "a manifest with no module script",
			mutate: func(cfg *livecheck.Config) {
				cfg.Manifest.Files = without(cfg.Manifest.Files, testModuleScript)
			},
			wantErr: true,
		},
		{
			name: "a manifest with two module scripts",
			mutate: func(cfg *livecheck.Config) {
				cfg.Manifest.Files = append(cfg.Manifest.Files, livecheck.ManifestFile{Path: "module.fedcba9876543210.js", SHA256: manifestDigest})
			},
			wantErr: true,
		},
		{
			name:    "a body cap below the decoded budget",
			mutate:  func(cfg *livecheck.Config) { cfg.MaxBodyBytes = livecheck.DecodedBudget },
			wantErr: true,
		},
		{name: "a body cap of one byte over the decoded budget", mutate: func(cfg *livecheck.Config) { cfg.MaxBodyBytes = livecheck.DecodedBudget + 1 }},
		{name: "a request timeout below zero", mutate: func(cfg *livecheck.Config) { cfg.RequestTimeout = -time.Second }, wantErr: true},
		{
			name:    "a plain-HTTP www mode outside the set",
			mutate:  func(cfg *livecheck.Config) { cfg.PlainHTTPWWW = "refuse" },
			wantErr: true,
		},
		{
			name:   "the refuse-or-close plain-HTTP www mode",
			mutate: func(cfg *livecheck.Config) { cfg.PlainHTTPWWW = livecheck.PlainHTTPRefuseOrClose },
		},
		{
			name: "a record that could not be read is no configuration error",
			mutate: func(cfg *livecheck.Config) {
				cfg.Records.HSTSErr = errors.New("hsts.json: no such file")
				cfg.Records.PreloadErr = errors.New("preload.json: no such file")
			},
		},
		{name: "an empty tag list is no configuration error", mutate: func(cfg *livecheck.Config) { cfg.Tags = nil }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}

			err := cfg.Validate()

			if tc.wantErr && err == nil {
				t.Fatalf("Validate returned no error, want one: %s", tc.name)
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("Validate failed: %v, want the configuration accepted", err)
			}
		})
	}
}

// validConfig returns a configuration Validate accepts, which a test then breaks
// in one way. Nothing in it dials: Validate must refuse before any connection.
func validConfig() livecheck.Config {
	manifest := livecheck.Manifest{Version: testVersion}

	for _, path := range []string{"index.html", "privacy.html", "verify.html", testModuleScript} {
		manifest.Files = append(manifest.Files, livecheck.ManifestFile{Path: path, SHA256: manifestDigest})
	}

	return livecheck.Config{
		Host:          livecheck.DefaultHost,
		Apex:          livecheck.DefaultApex,
		Manifest:      manifest,
		PlainHTTPWWW:  livecheck.PlainHTTPRedirect,
		Tags:          []string{testVersion},
		Now:           func() time.Time { return testNow },
		Sleep:         func(context.Context, time.Duration) error { return nil },
		RetryBound:    livecheck.MinRetryBound,
		RetryInterval: 30 * time.Second,
		CertMargin:    livecheck.MinCertMargin,
	}
}

// without returns files without the file of path.
func without(files []livecheck.ManifestFile, path string) []livecheck.ManifestFile {
	kept := make([]livecheck.ManifestFile, 0, len(files))

	for _, file := range files {
		if file.Path != path {
			kept = append(kept, file)
		}
	}

	return kept
}

// TestRunRefusesAnInvalidConfigBeforeAnyConnection pins that an invalid
// configuration is the one error Run returns: no report, and no connection.
func TestRunRefusesAnInvalidConfigBeforeAnyConnection(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.RetryBound = livecheck.MinRetryBound - time.Second

	dialed := 0
	cfg.Dial = func(context.Context, string, string) (net.Conn, error) {
		dialed++

		return nil, fmt.Errorf("unexpected connection")
	}

	report, err := livecheck.Run(t.Context(), cfg)
	if err == nil {
		t.Fatalf("Run of an invalid configuration returned %+v and no error, want the configuration error", report)
	}

	if !strings.Contains(strings.ToLower(err.Error()), "retry") {
		t.Errorf("Run of a retry bound below the HTML's max-age fails with %q, want the error to name the retry bound it refused", err)
	}

	if dialed != 0 {
		t.Errorf("Run dialled %d times for an invalid configuration, want no connection", dialed)
	}

	if len(report.Checks) != 0 || report.Rounds != 0 {
		t.Errorf("Run returned the report %+v, want the zero Report", report)
	}
}
