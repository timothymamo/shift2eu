// White-box tests of the live site check's entry point: ci/sitecheck is a main
// package, so its tests are package main (Go refuses an import of a main
// package). run hands its arguments, its standard streams and the parts of
// Config a test replaces in as parameters, so no test touches os.Args or a
// process's own streams, and every test of a run reaches only a local httptest
// server, a fake clock and a fake sleep.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// These tests pin the entry point (behaviour 11 and the flags): the exit codes
// 0, 1 and 2, the flags it requires and refuses, that a record file it cannot
// read is a check failure and not a crash, that a manifest path which could be a
// local one is refused by the manifest parser, and that the report it prints
// escapes response text.

const (
	// mainVersion is the version the fixture site shows, its manifest names and
	// its tag list holds.
	mainVersion = "v1.2.3"

	// mainModuleScript is the fixture's module script, in ci/sitebuild's name
	// pattern.
	mainModuleScript = "module.0123456789abcdef.js"

	// mainHSTS is the Strict-Transport-Security header of a site whose HSTS is
	// met.
	mainHSTS = "max-age=31536000; includeSubDomains; preload"

	// mainHSTSRecord is site/records/hsts.json of a site whose HSTS is met and
	// whose preload is not pursued, in PR A's format (docs/adr/0042).
	mainHSTSRecord = `{"format":1,"outcome":"met","preload":false,"date":"2026-11-23","reason":""}`

	// mainPreloadRecord is site/records/preload.json of a domain not on the
	// browsers' preload list, in the same format.
	mainPreloadRecord = `{"format":1,"listed":false,"submitted":"","checked":"2026-11-23","subdomains":[]}`
)

// mainTestNow is the clock the fixture reads: a fixed instant, never the wall
// clock.
var mainTestNow = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

// mainPages returns the pages and assets a passing fixture serves.
func mainPages(t *testing.T) map[string][]byte {
	t.Helper()

	return map[string][]byte{
		"/":                    mainIndex(mainVersion),
		"/index.html":          mainIndex(mainVersion),
		"/about.html":          []byte(`<!DOCTYPE html><html lang="en"><body><p>about</p></body></html>`),
		"/privacy.html":        []byte(`<!DOCTYPE html><html lang="en"><body><p id="downgrade-risk">The first visit is exposed to a downgrade.</p></body></html>`),
		"/verify.html":         []byte(`<!DOCTYPE html><html lang="en"><body><p>verify</p></body></html>`),
		"/style.css":           []byte("body { color: #111111; }\n"),
		"/" + mainModuleScript: mainGzip(t, mainScript()),
	}
}

// mainIndex returns the root page, whose footer carries the live version.
func mainIndex(version string) []byte {
	return []byte(`<!DOCTYPE html><html lang="en"><body><footer><p class="footer-links"><span class="mono" id="site-version">` + version + `</span></p></footer></body></html>`)
}

// mainScript returns the module script's decoded content.
func mainScript() []byte {
	return []byte(`export function assess(inventory) { return inventory; }`)
}

// mainGzip returns b as a gzip stream.
func mainGzip(t *testing.T, b []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)

	if _, err := zw.Write(b); err != nil {
		t.Fatalf("gzip the fixture body: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close the fixture's gzip stream: %v", err)
	}

	return buf.Bytes()
}

// mainResponse is one response a fixture server gives for one path.
type mainResponse struct {
	status int
	header http.Header
	body   []byte
}

// mainServer is one fixture server: the pages it answers and the requests it
// saw.
type mainServer struct {
	mu       sync.Mutex
	pages    map[string]mainResponse
	hsts     string
	requests []string
}

// mainNewServer returns a server that answers each of pages' paths with its
// response, any other path with 404 (without HSTS), and Strict-Transport-Security
// set to hsts on every page answer whose own header does not set it (nothing
// when hsts is empty).
func mainNewServer(pages map[string]mainResponse, hsts string) *mainServer {
	return &mainServer{pages: pages, hsts: hsts}
}

// ServeHTTP answers a request, recording its path and query, with 404 for a path
// the server does not serve.
func (s *mainServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.RequestURI())
	response, ok := s.pages[r.URL.Path]
	hsts := s.hsts
	s.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	for name, values := range response.header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	if hsts != "" && w.Header().Get("Strict-Transport-Security") == "" {
		w.Header().Set("Strict-Transport-Security", hsts)
	}

	status := response.status
	if status == 0 {
		status = http.StatusOK
	}

	w.WriteHeader(status)
	_, _ = w.Write(response.body)
}

// mainSet replaces one response of the server.
func (s *mainServer) mainSet(path string, response mainResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pages[path] = response
}

// mainCA is the certificate authority the fixture site's certificates are
// signed by, and the pool that trusts it.
type mainCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

// mainNewCA makes a certificate authority valid around mainTestNow.
func mainNewCA(t *testing.T) *mainCA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate the test CA's key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "shift2eu sitecheck test CA"},
		NotBefore:             mainTestNow.Add(-24 * time.Hour),
		NotAfter:              mainTestNow.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create the test CA's certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse the test CA's certificate: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return &mainCA{cert: cert, key: key, pool: pool}
}

// mainLeaf signs a server certificate for hosts, valid around mainTestNow.
func (ca *mainCA) mainLeaf(t *testing.T, hosts ...string) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate the leaf's key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: hosts[0]},
		DNSNames:              hosts,
		NotBefore:             mainTestNow.Add(-time.Hour),
		NotAfter:              mainTestNow.Add(90 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create the leaf's certificate: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// mainStartTLS starts an HTTPS httptest server with cert, closed when the test
// ends.
func mainStartTLS(t *testing.T, handler http.Handler, cert tls.Certificate) *httptest.Server {
	t.Helper()

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()

	t.Cleanup(srv.Close)

	return srv
}

// mainDialer is the dialer the entry point's tests pass: every address of the
// fixture site is dialled to the local server that stands in for it, and an
// address it does not hold is a refused connection.
type mainDialer struct {
	mu     sync.Mutex
	routes map[string]string
	dials  int
}

// mainNewDialer returns a dialer with no route.
func mainNewDialer() *mainDialer {
	return &mainDialer{routes: make(map[string]string)}
}

// set routes addr to target.
func (d *mainDialer) set(addr, target string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.routes[addr] = target
}

// dial opens the connection for addr.
func (d *mainDialer) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.dials++
	target, ok := d.routes[addr]
	d.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("no route to %s", addr)
	}

	var dialer net.Dialer

	return dialer.DialContext(ctx, network, target)
}

// remove drops the route of addr, so a dial to it is refused.
func (d *mainDialer) remove(addr string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.routes, addr)
}

// dialCount returns how many connections the dialer was asked for.
func (d *mainDialer) dialCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.dials
}

// mainClock is the fixture's fake clock and sleep: it never waits.
type mainClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps int
}

// Now returns the fake clock.
func (c *mainClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

// Sleep advances the fake clock, without waiting.
func (c *mainClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
	c.sleeps++

	return nil
}

// mainFixture is a whole passing site on disk: the servers the check talks to,
// the manifest and the tag list in a temporary directory, and the base Config a
// test passes to run.
type mainFixture struct {
	base     livecheck.Config
	manifest string
	tags     string
	host     *mainServer
	// dialer is the run's dialer: a test takes away a route to make a
	// connection refused.
	dialer *mainDialer
}

// mainNewFixture builds a site that passes every check, writes its manifest and
// tag list into dir, and returns them.
func mainNewFixture(t *testing.T, dir string) *mainFixture {
	t.Helper()

	ca := mainNewCA(t)
	dialer := mainNewDialer()
	clock := &mainClock{now: mainTestNow}

	pages := mainPages(t)

	host := mainNewServer(map[string]mainResponse{}, mainHSTS)

	for path, body := range pages {
		response := mainResponse{body: body}

		if path == "/" || strings.HasSuffix(path, ".html") {
			response.header = http.Header{"Cache-Control": {"public, max-age=300"}}
		}

		if path == "/"+mainModuleScript {
			response.header = http.Header{
				"Cache-Control":    {"public, max-age=31536000"},
				"Content-Encoding": {"gzip"},
				"Content-Type":     {"text/javascript; charset=utf-8"},
			}
		}

		host.mainSet(path, response)
	}

	apex := mainNewServer(map[string]mainResponse{
		"/":             {status: http.StatusMovedPermanently, header: http.Header{"Location": {"https://" + livecheck.DefaultHost + "/"}}},
		"/privacy.html": {status: http.StatusMovedPermanently, header: http.Header{"Location": {"https://" + livecheck.DefaultHost + "/privacy.html"}}},
	}, mainHSTS)

	plainHost := mainNewServer(map[string]mainResponse{
		"/":             {status: http.StatusMovedPermanently, header: http.Header{"Location": {"https://" + livecheck.DefaultHost + "/"}}},
		"/privacy.html": {status: http.StatusMovedPermanently, header: http.Header{"Location": {"https://" + livecheck.DefaultHost + "/privacy.html"}}},
	}, "")
	plainApex := mainNewServer(map[string]mainResponse{
		"/":             {status: http.StatusMovedPermanently, header: http.Header{"Location": {"https://" + livecheck.DefaultHost + "/"}}},
		"/privacy.html": {status: http.StatusMovedPermanently, header: http.Header{"Location": {"https://" + livecheck.DefaultHost + "/privacy.html"}}},
	}, "")

	hostTLS := mainStartTLS(t, host, ca.mainLeaf(t, livecheck.DefaultHost))
	apexTLS := mainStartTLS(t, apex, ca.mainLeaf(t, livecheck.DefaultApex))
	hostHTTP := httptest.NewServer(plainHost)
	apexHTTP := httptest.NewServer(plainApex)

	t.Cleanup(hostHTTP.Close)
	t.Cleanup(apexHTTP.Close)

	dialer.set(livecheck.DefaultHost+":443", hostTLS.Listener.Addr().String())
	dialer.set(livecheck.DefaultApex+":443", apexTLS.Listener.Addr().String())
	dialer.set(livecheck.DefaultHost+":80", hostHTTP.Listener.Addr().String())
	dialer.set(livecheck.DefaultApex+":80", apexHTTP.Listener.Addr().String())

	manifest := mainManifest(t, pages)
	manifestPath := mainWrite(t, dir, "manifest.json", []byte(manifest))
	tagsPath := mainWrite(t, dir, "tags.txt", []byte(mainVersion+"\nv1.2.2\n"))

	return &mainFixture{
		base: livecheck.Config{
			Dial:  dialer.dial,
			Roots: ca.pool,
			Now:   clock.Now,
			Sleep: clock.Sleep,
		},
		manifest: manifestPath,
		tags:     tagsPath,
		host:     host,
		dialer:   dialer,
	}
}

// mainManifest returns the manifest.json of pages, with the digest of every file
// after decoding.
func mainManifest(t *testing.T, pages map[string][]byte) string {
	t.Helper()

	files := make([]string, 0, len(pages))

	for _, path := range []string{"/index.html", "/about.html", "/privacy.html", "/verify.html", "/style.css", "/" + mainModuleScript} {
		body, ok := pages[path]
		if !ok {
			t.Fatalf("the fixture serves no %s", path)
		}

		decoded := body
		if path == "/"+mainModuleScript {
			decoded = mainScript()
		}

		sum := sha256.Sum256(decoded)
		files = append(files, fmt.Sprintf(`{"path":%q,"sha256":%q}`, strings.TrimPrefix(path, "/"), hex.EncodeToString(sum[:])))
	}

	return fmt.Sprintf(`{"version":%q,"files":[%s]}`, mainVersion, strings.Join(files, ","))
}

// mainWrite writes content to name in dir and returns the path.
func mainWrite(t *testing.T, dir, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

// mainRefusingBase returns a base Config whose dialer refuses every connection,
// with no route at all, and the dialer, so a test can count the connections a
// run asked for.
func mainRefusingBase() (livecheck.Config, *mainDialer) {
	dialer := mainNewDialer()
	clock := &mainClock{now: mainTestNow}

	return livecheck.Config{
		Dial:  dialer.dial,
		Now:   clock.Now,
		Sleep: clock.Sleep,
	}, dialer
}

// mainRun calls run with args and the base, and returns its exit code and what it
// wrote to stdout and stderr.
func mainRun(t *testing.T, args []string, stdin string, base livecheck.Config) (code int, stdout, stderr string) {
	t.Helper()

	var out, errOut bytes.Buffer

	code = run(t.Context(), args, strings.NewReader(stdin), &out, &errOut, base)

	return code, out.String(), errOut.String()
}

// TestRunRefusesACommandLineOrAFileItCannotUse pins exit code 2 and one line on
// stderr for a usage or configuration error: no arguments, a missing required
// flag, an unknown flag, a positional argument, a retry bound below 300 s, and a
// manifest or tag list that cannot be read. None of them dials.
func TestRunRefusesACommandLineOrAFileItCannotUse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantInErr string
		args      func(t *testing.T, dir string) []string
	}{
		{
			name:      "no arguments",
			wantInErr: "-manifest",
			args:      func(*testing.T, string) []string { return nil },
		},
		{
			name:      "no tag list",
			wantInErr: "-tags",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", mainWrite(t, dir, "manifest.json", []byte(`{"version":"v1.2.3","files":[]}`))}
			},
		},
		{
			name:      "no manifest",
			wantInErr: "-manifest",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-tags", mainWrite(t, dir, "tags.txt", []byte(mainVersion+"\n"))}
			},
		},
		{
			name:      "an unknown flag",
			wantInErr: "-unknown",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", mainWrite(t, dir, "manifest.json", []byte(`{"version":"v1.2.3","files":[]}`)), "-tags", "-", "-unknown"}
			},
		},
		{
			name:      "a positional argument",
			wantInErr: "extra",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", mainWrite(t, dir, "manifest.json", []byte(`{"version":"v1.2.3","files":[]}`)), "-tags", "-", "extra"}
			},
		},
		{
			name:      "a retry bound below the HTML's max-age",
			wantInErr: "retry",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", mainWrite(t, dir, "manifest.json", []byte(`{"version":"v1.2.3","files":[]}`)), "-tags", "-", "-retry-bound", "200s"}
			},
		},
		{
			name:      "a retry bound that is not a duration",
			wantInErr: "soon",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", mainWrite(t, dir, "manifest.json", []byte(`{"version":"v1.2.3","files":[]}`)), "-tags", "-", "-retry-bound", "soon"}
			},
		},
		{
			name:      "a plain-HTTP www mode outside the set",
			wantInErr: "-plain-http-www",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", mainWrite(t, dir, "passing.json", []byte(mainManifest(t, mainPages(t)))), "-tags", "-", "-plain-http-www", "refuse"}
			},
		},
		{
			name:      "a manifest that is not there",
			wantInErr: "absent.json",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", filepath.Join(dir, "absent.json"), "-tags", "-"}
			},
		},
		{
			name:      "a manifest that is not JSON",
			wantInErr: "broken.json",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", mainWrite(t, dir, "broken.json", []byte("not json")), "-tags", "-"}
			},
		},
		{
			name:      "a tag list that is not there",
			wantInErr: "absent.txt",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-manifest", mainWrite(t, dir, "manifest.json", []byte(`{"version":"v1.2.3","files":[]}`)), "-tags", filepath.Join(dir, "absent.txt")}
			},
		},
		{
			name:      "a manifest path that could be a local one",
			wantInErr: "passwd",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				manifest := `{"version":"v1.2.3","files":[{"path":"../../etc/passwd","sha256":"` + strings.Repeat("ab", 32) + `"}]}`

				return []string{"-manifest", mainWrite(t, dir, "escaping.json", []byte(manifest)), "-tags", "-"}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base, dialer := mainRefusingBase()

			code, stdout, stderr := mainRun(t, tc.args(t, t.TempDir()), "", base)

			if code != exitConfig {
				t.Errorf("exit code %d, want %d for %s", code, exitConfig, tc.name)
			}

			if stdout != "" {
				t.Errorf("stdout is %q, want nothing: a configuration error prints no report", stdout)
			}

			if stderr == "" {
				t.Fatalf("stderr is empty, want one line naming the usage or configuration error: %s", tc.name)
			}

			if !strings.HasSuffix(stderr, "\n") || strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr is %q, want one line ending in a line break", stderr)
			}

			if !strings.Contains(stderr, tc.wantInErr) {
				t.Errorf("stderr is %q, want it to name %q: the line says what is wrong", stderr, tc.wantInErr)
			}

			if dialCount := dialer.dialCount(); dialCount != 0 {
				t.Errorf("the run dialled %d times, want no connection for a configuration error: %s", dialCount, tc.name)
			}
		})
	}
}

// TestRunPassesAPassingSiteFromItsFiles pins exit code 0: run reads the manifest
// and the tag list its flags name (the tag list from stdin when -tags is "-"),
// checks the site, writes the report to stdout and exits 0.
func TestRunPassesAPassingSiteFromItsFiles(t *testing.T) {
	t.Parallel()

	fixture := mainNewFixture(t, t.TempDir())

	code, stdout, stderr := mainRun(t, []string{
		"-manifest", fixture.manifest,
		"-tags", "-",
		"-hsts-record", mainWrite(t, t.TempDir(), "hsts.json", []byte(mainHSTSRecord)),
		"-preload-record", mainWrite(t, t.TempDir(), "preload.json", []byte(mainPreloadRecord)),
	}, mainVersion+"\n", fixture.base)

	if code != exitPassed {
		t.Fatalf("exit code %d, want %d for a passing site: stderr %q\n%s", code, exitPassed, stderr, stdout)
	}

	if stderr != "" {
		t.Errorf("stderr is %q, want nothing for a passing run", stderr)
	}

	if lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n"); lines[len(lines)-1] != "result: PASS" {
		t.Errorf("the report's last line is %q, want %q:\n%s", lines[len(lines)-1], "result: PASS", stdout)
	}
}

// TestRunPlainHTTPWWWMode pins -plain-http-www, the program's mode for plain
// HTTP on the canonical host: the default is redirect, which requires it to
// redirect to https://<host>/<p> like the apex; refuse-or-close also accepts an
// edge that refuses the connection or closes it before any response
// (behaviour 6's exception). The mode is the program's, not a record's: PR A's
// hsts.json has no field for it.
func TestRunPlainHTTPWWWMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		// wantCode is the exit code the run ends with.
		wantCode int
		// wantInStdout, when set, must appear in the report.
		wantInStdout string
	}{
		{
			name:         "the default mode requires the redirect",
			wantCode:     exitFailed,
			wantInStdout: "FAIL " + livecheck.KindRedirect + " http://" + livecheck.DefaultHost + "/",
		},
		{
			name:         "redirect named explicitly requires it too",
			args:         []string{"-plain-http-www", "redirect"},
			wantCode:     exitFailed,
			wantInStdout: "FAIL " + livecheck.KindRedirect + " http://" + livecheck.DefaultHost + "/",
		},
		{
			name:     "refuse-or-close accepts the refused connection",
			args:     []string{"-plain-http-www", "refuse-or-close"},
			wantCode: exitPassed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fixture := mainNewFixture(t, dir)
			// Plain HTTP on the canonical host cannot connect: refused, as an
			// edge that refuses the port does.
			fixture.dialer.remove(livecheck.DefaultHost + ":80")

			args := append([]string{
				"-manifest", fixture.manifest,
				"-tags", "-",
				"-hsts-record", mainWrite(t, t.TempDir(), "hsts.json", []byte(mainHSTSRecord)),
				"-preload-record", mainWrite(t, t.TempDir(), "preload.json", []byte(mainPreloadRecord)),
			}, tc.args...)

			code, stdout, stderr := mainRun(t, args, mainVersion+"\n", fixture.base)

			if code != tc.wantCode {
				t.Fatalf("exit code %d, want %d: stderr %q\n%s", code, tc.wantCode, stderr, stdout)
			}

			if stderr != "" {
				t.Errorf("stderr is %q, want nothing: the mode is no configuration error here", stderr)
			}

			if tc.wantInStdout != "" && !strings.Contains(stdout, tc.wantInStdout) {
				t.Errorf("the report does not hold %q, want the plain HTTP check that failed:\n%s", tc.wantInStdout, stdout)
			}
		})
	}
}

// TestRunFailsSafeOnARecordItCannotRead pins behaviour 8's fail-safe on the two
// records: a record file that is missing or that its parser refuses is no
// configuration error and no crash. Every case but the control runs against the
// same site as TestRunPassesAPassingSiteFromItsFiles and changes only the
// records, so the record is the only reason a case fails: the run then exits 1
// with a report that names the offending record and still passes the site's own
// checks. The control row pins that the very same setup exits 0 with valid
// records, so this test cannot pass for an unrelated reason.
func TestRunFailsSafeOnARecordItCannotRead(t *testing.T) {
	t.Parallel()

	recordValid := func(name, content string) func(t *testing.T, dir string) string {
		return func(t *testing.T, dir string) string {
			t.Helper()

			return mainWrite(t, dir, name, []byte(content))
		}
	}

	recordAbsent := func(name string) func(t *testing.T, dir string) string {
		return func(t *testing.T, dir string) string {
			t.Helper()

			return filepath.Join(dir, "absent-"+name)
		}
	}

	validHSTS := recordValid("hsts.json", mainHSTSRecord)
	validPreload := recordValid("preload.json", mainPreloadRecord)

	// A record its parser refuses: no "outcome", so no value to judge the
	// site's header against.
	malformedHSTS := recordValid("hsts.json", `{"format":1}`)
	malformedPreload := recordValid("preload.json", `{"format":1}`)

	failing := func(want ...string) []string {
		lines := make([]string, len(want))
		for i, record := range want {
			lines[i] = "FAIL " + livecheck.KindHSTS + " " + record
		}

		return lines
	}

	tests := []struct {
		name string
		// hsts and preload return the path the flag names.
		hsts    func(t *testing.T, dir string) string
		preload func(t *testing.T, dir string) string
		// wantCode is the exit code the run ends with.
		wantCode int
		// wantInStdout are lines the report must hold: the record checks that
		// must fail, and result: FAIL; empty means a passing run.
		wantInStdout []string
		// wantNoRecord is the record the report must not fail on.
		wantNoRecord string
	}{
		{
			name:     "a passing site whose records are both valid",
			hsts:     validHSTS,
			preload:  validPreload,
			wantCode: exitPassed,
		},
		{
			name:         "the HSTS record missing",
			hsts:         recordAbsent("hsts.json"),
			preload:      validPreload,
			wantCode:     exitFailed,
			wantInStdout: failing("hsts.json"),
			wantNoRecord: "preload.json",
		},
		{
			name:         "the preload record missing",
			hsts:         validHSTS,
			preload:      recordAbsent("preload.json"),
			wantCode:     exitFailed,
			wantInStdout: failing("preload.json"),
			wantNoRecord: "hsts.json",
		},
		{
			name:         "both records missing",
			hsts:         recordAbsent("hsts.json"),
			preload:      recordAbsent("preload.json"),
			wantCode:     exitFailed,
			wantInStdout: failing("hsts.json", "preload.json"),
		},
		{
			name:         "the HSTS record malformed",
			hsts:         malformedHSTS,
			preload:      validPreload,
			wantCode:     exitFailed,
			wantInStdout: failing("hsts.json"),
			wantNoRecord: "preload.json",
		},
		{
			name:         "the preload record malformed",
			hsts:         validHSTS,
			preload:      malformedPreload,
			wantCode:     exitFailed,
			wantInStdout: failing("preload.json"),
			wantNoRecord: "hsts.json",
		},
		{
			name:         "both records malformed",
			hsts:         malformedHSTS,
			preload:      malformedPreload,
			wantCode:     exitFailed,
			wantInStdout: failing("hsts.json", "preload.json"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fixture := mainNewFixture(t, dir)

			code, stdout, stderr := mainRun(t, []string{
				"-manifest", fixture.manifest,
				"-tags", "-",
				"-hsts-record", tc.hsts(t, dir),
				"-preload-record", tc.preload(t, dir),
			}, mainVersion+"\n", fixture.base)

			if code != tc.wantCode {
				t.Fatalf("exit code %d, want %d: stderr %q\n%s", code, tc.wantCode, stderr, stdout)
			}

			if stderr != "" {
				t.Errorf("stderr is %q, want nothing: a record that cannot be read is a check failure, not a usage error", stderr)
			}

			// The site itself passed whatever the records said: the run reached
			// it and no check of it failed, so only the records can explain the
			// exit code.
			for _, want := range []string{"PASS root /", "PASS manifest-file index.html"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("the report does not hold %q, want the site's own checks to pass whatever the records say:\n%s", want, stdout)
				}
			}

			if dialCount := fixture.dialer.dialCount(); dialCount == 0 {
				t.Errorf("the run dialled no host, want it to have checked the passing site")
			}

			if len(tc.wantInStdout) == 0 {
				if !strings.Contains(stdout, "result: PASS") {
					t.Errorf("the report does not end in %q, want the control run to pass:\n%s", "result: PASS", stdout)
				}

				return
			}

			for _, want := range append(tc.wantInStdout, "result: FAIL") {
				if !strings.Contains(stdout, want) {
					t.Errorf("the report does not hold %q, want a failing check naming the record:\n%s", want, stdout)
				}
			}

			if tc.wantNoRecord != "" {
				if unwanted := "FAIL " + livecheck.KindHSTS + " " + tc.wantNoRecord; strings.Contains(stdout, unwanted) {
					t.Errorf("the report holds %q, want only the record that could not be read to fail:\n%s", unwanted, stdout)
				}
			}
		})
	}
}

// TestRunEscapesResponseTextInTheReport pins behaviour 11 at the entry point: a
// header value holding a workflow command is printed quoted, so it can neither
// start a line of the report nor be read as a workflow command.
func TestRunEscapesResponseTextInTheReport(t *testing.T) {
	t.Parallel()

	fixture := mainNewFixture(t, t.TempDir())

	const hostile = "max-age=300\t::error::pwned"

	fixture.host.mainSet("/", mainResponse{
		header: http.Header{"Cache-Control": {hostile}},
		body:   mainIndex(mainVersion),
	})

	code, stdout, stderr := mainRun(t, []string{
		"-manifest", fixture.manifest,
		"-tags", fixture.tags,
		"-hsts-record", mainWrite(t, t.TempDir(), "hsts.json", []byte(mainHSTSRecord)),
		"-preload-record", mainWrite(t, t.TempDir(), "preload.json", []byte(mainPreloadRecord)),
	}, "", fixture.base)

	if code != exitFailed {
		t.Fatalf("exit code %d, want %d for a page whose Cache-Control does not parse: stderr %q\n%s", code, exitFailed, stderr, stdout)
	}

	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "::error::") {
			t.Errorf("a line of the report starts with a workflow command: %q", line)
		}
	}

	if strings.Contains(stdout, "::error::pwned\n") {
		t.Errorf("the report prints the hostile header raw at the end of a line:\n%s", stdout)
	}

	if !strings.Contains(stdout, strconv.Quote(hostile)) {
		t.Errorf("the report does not print the hostile header as %q: response text reaches the report only through Quote:\n%s", strconv.Quote(hostile), stdout)
	}
}
