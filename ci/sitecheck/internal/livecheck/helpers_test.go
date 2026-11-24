package livecheck_test

import (
	"bufio"
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
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds what the live site check's tests share: a test certificate
// authority and the site's certificates, local httptest servers reached through
// an injected dialer, a fake clock and sleep, and a fixture site that passes
// every check until a test breaks one thing about it. Nothing here opens a
// connection to a host of its own or sleeps in real time, and nothing in it
// reads the wall clock apart from the hang guards below.
//
// Those guards are this package's exception to the rule that tests never depend
// on time (CLAUDE.md, Testing). Such a guard - runWithin's bound,
// verdictGuard, rawListenerGuard, and the guard inside
// TestVersionTextScansARawTextHeavyBodyInLinearTime in page_test.go - bounds a
// wait, so that a hang (a fixture that never answers, a transport that stops
// closing a connection, a scan gone super-linear in the body's length) fails
// that one test with its own message instead of holding up the package until go
// test's timeout kills the whole run. No assertion depends on the time a case
// takes: every guard sits orders of magnitude above the work it bounds, and a
// case that passes returns long before one is reached.
//
// The package's other wall-clock read is
// TestCheckCertificateKeepsTheCallersDeadline in run_test.go, which builds its
// deadlines from the wall clock too: it only compares the deadline it recorded
// with the one it set, and never waits for either, so it is no guard and no
// dependence on time.

// testNow is the clock every fixture reads: a fixed instant, so no test depends
// on when it runs.
var testNow = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

const (
	// testVersion is the version the fixture pages show, the manifest names and
	// the tag list holds.
	testVersion = "v1.2.3"

	// testModuleScript is the fixture's module script: ci/sitebuild's name
	// pattern with the 16 hexadecimal characters it gives it.
	testModuleScript = "module.0123456789abcdef.js"

	// hstsFull is the Strict-Transport-Security header of a site whose HSTS is
	// met with preload pursued: the strongest value the check accepts, so the
	// fixture passes in every state of the record.
	hstsFull = "max-age=31536000; includeSubDomains; preload"
)

// The fixture's records, written as site/records/hsts.json and
// site/records/preload.json carry them after PR A's hosting ADR (docs/adr/0042,
// "The HSTS and preload records"). The fixture builds its Records from this
// JSON with the parsers, as configure builds them from the release's files, so
// a test changes a record the way a record file changes and no test repeats
// the format: records_test.go pins it.
const (
	// hstsRecordMet is hsts.json with HSTS in force, preload not pursued.
	hstsRecordMet = `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":""}`
	// hstsRecordPreload is hsts.json with HSTS in force and preload pursued.
	hstsRecordPreload = `{"format":1,"outcome":"met","preload":true,"date":"2026-12-01","reason":""}`
	// hstsRecordWaived is hsts.json with HSTS waived, and the reason for it.
	hstsRecordWaived = `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":"the apex is served by the old provider until the migration, so preload is not pursued yet"}`
	// preloadRecordNotListed is preload.json for a domain never submitted.
	preloadRecordNotListed = `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[]}`
	// preloadRecordListed is preload.json for a domain on the browsers' list,
	// submitted with the one subdomain the site had then. Listed is only
	// usable with hsts.json's preload true (behaviour 8).
	preloadRecordListed = `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`
)

// moduleJS is the module script's decoded content. The fixture serves it gzip
// compressed, well inside both budgets.
var moduleJS = []byte(`export function assess(inventory) { return inventory; }`)

// styleCSS is a manifest file that is neither HTML nor the module script.
var styleCSS = []byte("body { color: #111111; background-color: #ffffff; }\n")

// indexPage is the root page: its footer carries the live version in the
// element VersionMarker names, as site/index.html does after this PR.
func indexPage(version string) []byte {
	return []byte(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>shift2eu</title></head>
<body>
<main><h1>shift2eu</h1></main>
<footer><p class="footer-links"><span class="mono" id="site-version">` + version + `</span></p></footer>
</body>
</html>
`)
}

// privacyPage is privacy.html: the page that carries the downgrade-risk
// statement whenever the preload record does not say the domain is on the list,
// and the hsts-waived statement when the HSTS record says waived.
func privacyPage(withDowngradeRisk, withHSTSWaived bool) []byte {
	body := `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Privacy</title></head>
<body>
<main><h1>Privacy</h1>`
	if withDowngradeRisk {
		body += `
<p id="downgrade-risk">On the first visit, before HSTS is in force, a network could downgrade the connection.</p>`
	}
	if withHSTSWaived {
		body += `
<p id="hsts-waived">HSTS is waived for this site; the owner accepts the risk.</p>`
	}
	body += `
</main>
</body>
</html>
`

	return []byte(body)
}

// verifyPage is verify.html: the page that carries the hsts-waived statement
// when the HSTS record says waived.
func verifyPage(withHSTSWaived bool) []byte {
	body := `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Verify</title></head>
<body>
<main><h1>Verify this site</h1>`
	if withHSTSWaived {
		body += `
<p id="hsts-waived">HSTS is waived for this site; the owner accepts the risk.</p>`
	}
	body += `
</main>
</body>
</html>
`

	return []byte(body)
}

// aboutPage is about.html (issue #528): the About page, the site's fifth page,
// which a release's manifest lists and the host serves with the others. It
// carries no statement the checks read, so its bytes only have to be the
// manifest's.
func aboutPage() []byte {
	return []byte(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>About</title></head>
<body>
<main><h1>About the maintainer</h1></main>
</body>
</html>
`)
}

// sha256Hex returns the lowercase hexadecimal SHA-256 of b, as a manifest holds
// it.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

// gzipBytes returns b as a gzip stream.
func gzipBytes(t *testing.T, b []byte) []byte {
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

// testCA is a certificate authority the tests sign the site's certificates
// with, and the pool that trusts it.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

// newTestCA makes a certificate authority valid around testNow.
func newTestCA(t *testing.T) *testCA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate the test CA's key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "shift2eu test CA"},
		NotBefore:             testNow.Add(-24 * time.Hour),
		NotAfter:              testNow.Add(365 * 24 * time.Hour),
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

	return &testCA{cert: cert, key: key, pool: pool}
}

// leaf signs a server certificate for hosts, valid from notBefore to notAfter.
func (ca *testCA) leaf(t *testing.T, hosts []string, notBefore, notAfter time.Time) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate the leaf's key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: hosts[0]},
		DNSNames:              hosts,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
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

// chained signs a certificate for hosts through a fresh intermediate
// certificate of ca, so the server sends the chain leaf <- intermediate <- ca:
// the intermediate expires at intermediateNotAfter and the leaf at
// leafNotAfter, both after notBefore.
func (ca *testCA) chained(t *testing.T, hosts []string, notBefore, intermediateNotAfter, leafNotAfter time.Time) tls.Certificate {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate the intermediate's key: %v", err)
	}

	intermediateTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(3),
		Subject:               pkix.Name{CommonName: "shift2eu test intermediate"},
		NotBefore:             notBefore,
		NotAfter:              intermediateNotAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	intermediateDER, err := x509.CreateCertificate(rand.Reader, intermediateTemplate, ca.cert, &caKey.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create the intermediate's certificate: %v", err)
	}

	intermediate, err := x509.ParseCertificate(intermediateDER)
	if err != nil {
		t.Fatalf("parse the intermediate's certificate: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate the leaf's key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(4),
		Subject:               pkix.Name{CommonName: hosts[0]},
		DNSNames:              hosts,
		NotBefore:             notBefore,
		NotAfter:              leafNotAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, intermediate, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create the leaf's certificate: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der, intermediateDER}, PrivateKey: key}
}

// startTLSServer starts an HTTPS httptest server with cert as its certificate,
// closed when the test ends.
func startTLSServer(t *testing.T, handler http.Handler, cert tls.Certificate) *httptest.Server {
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

// startPlainServer starts a plain HTTP httptest server, closed when the test
// ends.
func startPlainServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv
}

// served is one response a fixture server gives for one path.
type served struct {
	// status is the response's status; 0 means 200.
	status int
	// header holds the response's headers.
	header http.Header
	// body is the bytes written to the response.
	body []byte
	// decoded holds the file's bytes when body is an encoding of them, as
	// the manifest lists them; nil means body is the file.
	decoded []byte
	// endless, when positive, makes the handler write that many zero bytes
	// after the status, as a body that never ends.
	endless int64
}

// file returns the bytes the manifest lists for the response.
func (s served) file() []byte {
	if s.decoded != nil {
		return s.decoded
	}

	return s.body
}

// recordedRequest is one request a fixture server answered.
type recordedRequest struct {
	method         string
	path           string
	rawQuery       string
	acceptEncoding string
}

// content is what one fixture server serves and the requests it saw.
type content struct {
	mu        sync.Mutex
	responses map[string]served
	hsts      string
	requests  []recordedRequest
}

// newContent returns a server's content with nothing served.
func newContent() *content {
	return &content{responses: make(map[string]served)}
}

// ServeHTTP answers the request from c, recording it, with 404 for a path c
// does not serve.
func (c *content) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.requests = append(c.requests, recordedRequest{
		method:         r.Method,
		path:           r.URL.Path,
		rawQuery:       r.URL.RawQuery,
		acceptEncoding: r.Header.Get("Accept-Encoding"),
	})
	s, ok := c.responses[r.URL.Path]
	hsts := c.hsts
	c.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	for name, values := range s.header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	if hsts != "" && w.Header().Get("Strict-Transport-Security") == "" {
		w.Header().Set("Strict-Transport-Security", hsts)
	}

	status := s.status
	if status == 0 {
		status = http.StatusOK
	}

	w.WriteHeader(status)

	if s.endless > 0 {
		writeZeros(w, s.endless)

		return
	}

	_, _ = w.Write(s.body)
}

// writeZeros writes n zero bytes to w, stopping when w refuses them.
func writeZeros(w http.ResponseWriter, n int64) {
	buf := make([]byte, 32<<10)

	for left := n; left > 0; {
		chunk := int64(len(buf))
		if left < chunk {
			chunk = left
		}

		if _, err := w.Write(buf[:chunk]); err != nil {
			return
		}

		left -= chunk
	}
}

// set serves one response for path.
func (c *content) set(path string, s served) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.responses[path] = s
}

// setBody replaces the body of the response for path, keeping its status and
// headers.
func (c *content) setBody(path string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.responses[path]
	if !ok {
		s = served{status: http.StatusOK}
	}

	s.body = body
	s.decoded = nil
	c.responses[path] = s
}

// setHeader sets one header of the response for path.
func (c *content) setHeader(path, name, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.responses[path]
	if !ok {
		s = served{status: http.StatusOK}
	}

	if s.header == nil {
		s.header = make(http.Header, 1)
	}

	s.header.Set(name, value)
	c.responses[path] = s
}

// removeHeader removes one header of the response for path.
func (c *content) removeHeader(path, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.responses[path]
	if !ok {
		return
	}

	s.header.Del(name)
	c.responses[path] = s
}

// remove stops serving path, so the server answers 404 for it.
func (c *content) remove(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.responses, path)
}

// file returns the bytes served for path as the manifest lists them.
func (c *content) file(path string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.responses[path]
	if !ok {
		return nil, false
	}

	return s.file(), true
}

// requestLog returns the requests c answered.
func (c *content) requestLog() []recordedRequest {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]recordedRequest(nil), c.requests...)
}

// routeTable is the dialer the fixture passes to livecheck.Config: every
// connection to one of the site's addresses is dialled to the httptest server's
// listener instead.
type routeTable struct {
	mu      sync.Mutex
	targets map[string]string
	dials   []string
}

// newRouteTable returns an empty route table.
func newRouteTable() *routeTable {
	return &routeTable{targets: make(map[string]string)}
}

// set routes addr ("www.shift2eu.com:443") to target ("127.0.0.1:1234").
func (r *routeTable) set(addr, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.targets[addr] = target
}

// remove stops routing addr, so a connection to it is refused.
func (r *routeTable) remove(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.targets, addr)
}

// dial opens the connection for addr, or returns an error (a refused
// connection) when the table holds no route to it.
func (r *routeTable) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	r.mu.Lock()
	r.dials = append(r.dials, addr)
	target, ok := r.targets[addr]
	r.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("no route to %s", addr)
	}

	var d net.Dialer

	return d.DialContext(ctx, network, target)
}

// dialLog returns the addresses dialled.
func (r *routeTable) dialLog() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.dials...)
}

// verdictGuard bounds the wait in verdictConn.RemoteAddr: a hang guard, never a
// value any behaviour depends on.
const verdictGuard = 10 * time.Second

// verdictFirst returns inner's dialer with every connection to addr wrapped so
// that its RemoteAddr waits until the transport has closed it (verdictConn),
// and a func reporting whether the transport asked such a connection for its
// remote address.
//
// The transport reads the bytes already on a fresh connection in its read loop,
// before the request is registered, and a byte it reads there makes it discard
// the response as unsolicited and close the connection
// (net/http/transport.go, readLoop's Peek and readLoopPeekFailLocked). That is
// the order an edge that writes as soon as it accepts the connection reaches
// only by chance, which made the refuse-or-close check pass the bytes it must
// fail; the wait in RemoteAddr, on the trace's GotConn, which runs before the
// request is registered, makes that order the only one. The wait is
// RemoteAddr's, so a case that uses this dialer asserts the call happened
// (forceVerdictFirst).
func verdictFirst(inner livecheck.Dialer, addr string) (livecheck.Dialer, func() bool) {
	var asked atomic.Bool

	dial := func(ctx context.Context, network, target string) (net.Conn, error) {
		conn, err := inner(ctx, network, target)
		if err != nil {
			return nil, err
		}

		if target != addr {
			return conn, nil
		}

		return &verdictConn{Conn: conn, closed: make(chan struct{}), asked: &asked}, nil
	}

	return dial, asked.Load
}

// forceVerdictFirst makes site force the write-on-accept order on addr
// (verdictFirst) and, after the run, fails the case unless the transport asked
// the forced connection for its remote address: that call is where the wait
// runs, so without it the case would silently stop forcing the order and pass
// or fail by chance.
func forceVerdictFirst(site *site, addr string) {
	dial, asked := verdictFirst(site.dial, addr)
	site.dial = dial
	site.afterRun = func(t *testing.T) {
		t.Helper()

		if !asked() {
			t.Error("the transport never asked the forced connection for its remote address, want the call whose wait forces the write-on-accept order")
		}
	}
}

// verdictConn is a connection whose RemoteAddr waits for the transport's verdict
// on the bytes already on it: it returns once the transport has closed the
// connection, which it does when its read loop discarded those bytes instead of
// answering the request with them (verdictFirst). The wait never depends on the
// wall clock: it ends with the connection or, should the transport stop closing
// it, with verdictGuard, so a hang fails this test instead of hanging the
// package's run.
type verdictConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
	// asked records that the transport called RemoteAddr, the call whose wait
	// forces the order the case needs.
	asked *atomic.Bool
}

// Close marks the connection closed, releasing RemoteAddr, and closes it.
func (c *verdictConn) Close() error {
	c.once.Do(func() { close(c.closed) })

	return c.Conn.Close()
}

// RemoteAddr returns the connection's remote address once the transport has
// closed the connection, and records that it was called.
func (c *verdictConn) RemoteAddr() net.Addr {
	c.asked.Store(true)

	select {
	case <-c.closed:
	case <-time.After(verdictGuard):
	}

	return c.Conn.RemoteAddr()
}

// filteredDial returns inner's dialer with addr filtered: a connection to addr
// is never made, and the dial ends at once with the error of a dial that timed
// out, as a port a firewall drops does (no refusal, no answer, until the client
// gives up). It ends the dial itself rather than waiting for the request's
// context to end, so the case needs no request timeout below the run's own:
// every other request of the run keeps the default, where a short one left the
// HTTPS fetches, each with a TLS handshake of its own, to fail under load.
func filteredDial(inner livecheck.Dialer, addr string) livecheck.Dialer {
	return func(ctx context.Context, network, target string) (net.Conn, error) {
		if target != addr {
			return inner(ctx, network, target)
		}

		return nil, &net.OpError{Op: "dial", Net: network, Err: context.DeadlineExceeded}
	}
}

// leafBuilder signs one host's certificate with the fixture's CA.
type leafBuilder func(t *testing.T, ca *testCA) tls.Certificate

// leafValidFor builds a certificate for hosts that stays valid well past the
// margin: an hour back and ninety days forward of testNow.
func leafValidFor(hosts ...string) leafBuilder {
	return func(t *testing.T, ca *testCA) tls.Certificate {
		t.Helper()

		return ca.leaf(t, hosts, testNow.Add(-time.Hour), testNow.Add(90*24*time.Hour))
	}
}

// leafExpiringWithin builds a certificate for hosts that expires within d of
// testNow, inside the margin for a d below 14 days.
func leafExpiringWithin(d time.Duration, hosts ...string) leafBuilder {
	return func(t *testing.T, ca *testCA) tls.Certificate {
		t.Helper()

		return ca.leaf(t, hosts, testNow.Add(-time.Hour), testNow.Add(d))
	}
}

// leafThroughAnIntermediateExpiringWithin builds the chain leaf <-
// intermediate <- ca for hosts: the leaf stays valid well past the margin and
// the intermediate expires within d of testNow, inside the margin for a d
// below 14 days.
func leafThroughAnIntermediateExpiringWithin(d time.Duration, hosts ...string) leafBuilder {
	return func(t *testing.T, ca *testCA) tls.Certificate {
		t.Helper()

		return ca.chained(t, hosts, testNow.Add(-time.Hour), testNow.Add(d), testNow.Add(90*24*time.Hour))
	}
}

// leafThroughAValidIntermediate builds the chain leaf <- intermediate <- ca for
// hosts with both the leaf and the intermediate valid well past the margin.
func leafThroughAValidIntermediate(hosts ...string) leafBuilder {
	return func(t *testing.T, ca *testCA) tls.Certificate {
		t.Helper()

		return ca.chained(t, hosts, testNow.Add(-time.Hour), testNow.Add(90*24*time.Hour), testNow.Add(60*24*time.Hour))
	}
}

// siteOptions are the parts of a fixture site a test changes before it starts
// its servers.
type siteOptions struct {
	hostLeaf leafBuilder
	apexLeaf leafBuilder
	roots    *x509.CertPool
}

// siteOption changes one of a fixture site's options.
type siteOption func(*siteOptions)

// withHostLeaf serves the canonical host's certificate built by b.
func withHostLeaf(b leafBuilder) siteOption {
	return func(o *siteOptions) { o.hostLeaf = b }
}

// withApexLeaf serves the apex's certificate built by b.
func withApexLeaf(b leafBuilder) siteOption {
	return func(o *siteOptions) { o.apexLeaf = b }
}

// withRoots passes roots as Config.Roots instead of the pool that trusts the
// fixture's CA (an empty pool is an untrusted root).
func withRoots(roots *x509.CertPool) siteOption {
	return func(o *siteOptions) { o.roots = roots }
}

// site is a whole fixture site: the four servers the check talks to (TLS and
// plain HTTP, the canonical host and the apex), the CA it trusts, the fake
// clock and sleep, and what Config carries.
type site struct {
	ca *testCA

	mu     sync.Mutex
	now    time.Time
	sleeps int
	// onSleep, when set, runs after each sleep, after the clock advanced.
	onSleep func(ctx context.Context, d time.Duration) error

	hostTLS  *content
	apexTLS  *content
	hostHTTP *content
	apexHTTP *content

	routes *routeTable
	// dial is what Config.Dial carries: routes.dial, unless a test wraps it.
	dial livecheck.Dialer

	manifestVersion string
	manifestPaths   []string
	digests         map[string]string
	records         livecheck.Records
	// plainHTTPWWW is what Config.PlainHTTPWWW carries: PlainHTTPRedirect
	// (the production default) unless a test asks for the refuse-or-close
	// acceptance.
	plainHTTPWWW   livecheck.PlainHTTPWWW
	tags           []string
	retryBound     time.Duration
	retryInterval  time.Duration
	maxBodyBytes   int64
	requestTimeout time.Duration
	roots          *x509.CertPool
	// afterRun, when set, runs after the run: it asserts what the run had to
	// do first (a forced order, a call the case depends on).
	afterRun func(t *testing.T)
}

// parsedHSTSRecord returns hsts.json's parsed record for record, the JSON of a
// record file, and fails the test when the parser refuses it: a fixture holds
// only records the check can read.
func parsedHSTSRecord(t *testing.T, record string) livecheck.HSTSRecord {
	t.Helper()

	parsed, err := livecheck.ParseHSTSRecord(strings.NewReader(record))
	if err != nil {
		t.Fatalf("the fixture's hsts.json %q: %v", record, err)
	}

	return parsed
}

// parsedPreloadRecord returns preload.json's parsed record for record, under
// the same rule as parsedHSTSRecord.
func parsedPreloadRecord(t *testing.T, record string) livecheck.PreloadRecord {
	t.Helper()

	parsed, err := livecheck.ParsePreloadRecord(strings.NewReader(record))
	if err != nil {
		t.Fatalf("the fixture's preload.json %q: %v", record, err)
	}

	return parsed
}

// newSite builds a fixture site that passes every check, and breaks the parts a
// test names through opts or by changing the site afterwards.
func newSite(t *testing.T, opts ...siteOption) *site {
	t.Helper()

	var options siteOptions

	for _, opt := range opts {
		opt(&options)
	}

	ca := newTestCA(t)

	s := &site{
		ca:              ca,
		now:             testNow,
		hostTLS:         newContent(),
		apexTLS:         newContent(),
		hostHTTP:        newContent(),
		apexHTTP:        newContent(),
		routes:          newRouteTable(),
		manifestVersion: testVersion,
		manifestPaths:   []string{"about.html", "index.html", "privacy.html", "verify.html", "style.css", testModuleScript},
		digests:         make(map[string]string),
		records: livecheck.Records{
			HSTS:    parsedHSTSRecord(t, hstsRecordMet),
			Preload: parsedPreloadRecord(t, preloadRecordNotListed),
		},
		plainHTTPWWW:  livecheck.PlainHTTPRedirect,
		tags:          []string{testVersion},
		retryBound:    livecheck.MinRetryBound,
		retryInterval: livecheck.MinRetryBound,
		roots:         ca.pool,
	}

	s.dial = s.routes.dial

	if options.roots != nil {
		s.roots = options.roots
	}

	hostLeaf := leafValidFor(livecheck.DefaultHost)
	if options.hostLeaf != nil {
		hostLeaf = options.hostLeaf
	}

	apexLeaf := leafValidFor(livecheck.DefaultApex)
	if options.apexLeaf != nil {
		apexLeaf = options.apexLeaf
	}

	hostTLS := startTLSServer(t, s.hostTLS, hostLeaf(t, ca))
	apexTLS := startTLSServer(t, s.apexTLS, apexLeaf(t, ca))
	hostHTTP := startPlainServer(t, s.hostHTTP)
	apexHTTP := startPlainServer(t, s.apexHTTP)

	s.routes.set(livecheck.DefaultHost+":443", hostTLS.Listener.Addr().String())
	s.routes.set(livecheck.DefaultApex+":443", apexTLS.Listener.Addr().String())
	s.routes.set(livecheck.DefaultHost+":80", hostHTTP.Listener.Addr().String())
	s.routes.set(livecheck.DefaultApex+":80", apexHTTP.Listener.Addr().String())

	s.serveDefaultContent(t)

	return s
}

// serveDefaultContent serves the pages, assets and redirects of a correct site.
func (s *site) serveDefaultContent(t *testing.T) {
	t.Helper()

	gzipped := gzipBytes(t, moduleJS)

	s.hostTLS.hsts = hstsFull
	s.hostTLS.set("/", served{
		status: http.StatusOK,
		header: http.Header{
			"Cache-Control": {"public, max-age=60"},
			"Age":           {"42"},
		},
		body: indexPage(testVersion),
	})
	s.hostTLS.set("/index.html", served{
		status: http.StatusOK,
		header: http.Header{"Cache-Control": {"public, max-age=300"}},
		body:   indexPage(testVersion),
	})
	s.hostTLS.set("/privacy.html", served{
		status: http.StatusOK,
		header: http.Header{"Cache-Control": {"public, max-age=300"}},
		body:   privacyPage(true, false),
	})
	s.hostTLS.set("/about.html", served{
		status: http.StatusOK,
		header: http.Header{"Cache-Control": {"public, max-age=300"}},
		body:   aboutPage(),
	})
	s.hostTLS.set("/verify.html", served{
		status: http.StatusOK,
		header: http.Header{"Cache-Control": {"public, max-age=300"}},
		body:   verifyPage(false),
	})
	s.hostTLS.set("/style.css", served{
		status: http.StatusOK,
		header: http.Header{"Cache-Control": {"public, max-age=31536000"}},
		body:   styleCSS,
	})
	s.hostTLS.set("/"+testModuleScript, served{
		status: http.StatusOK,
		header: http.Header{
			"Cache-Control":    {"public, max-age=31536000"},
			"Content-Encoding": {"gzip"},
			"Content-Type":     {"text/javascript; charset=utf-8"},
		},
		body:    gzipped,
		decoded: moduleJS,
	})

	s.apexTLS.hsts = hstsFull
	s.apexTLS.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/")})
	s.apexTLS.set("/privacy.html", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/privacy.html")})

	s.apexHTTP.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/")})
	s.apexHTTP.set("/privacy.html", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/privacy.html")})

	s.hostHTTP.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/")})
	s.hostHTTP.set("/privacy.html", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultHost + "/privacy.html")})
}

// redirect returns the headers of a redirect to location.
func redirect(location string) http.Header {
	return http.Header{"Location": {location}}
}

// config returns the Config that checks the fixture site.
func (s *site) config(t *testing.T) livecheck.Config {
	t.Helper()

	manifest := livecheck.Manifest{Version: s.manifestVersion}

	for _, path := range s.manifestPaths {
		digest := s.digests[path]
		if digest == "" {
			file, ok := s.hostTLS.file("/" + path)
			if !ok {
				t.Fatalf("the fixture lists %q in its manifest but serves no bytes for it and holds no digest for it", path)
			}

			digest = sha256Hex(file)
		}

		manifest.Files = append(manifest.Files, livecheck.ManifestFile{Path: path, SHA256: digest})
	}

	return livecheck.Config{
		Host:           livecheck.DefaultHost,
		Apex:           livecheck.DefaultApex,
		Manifest:       manifest,
		Records:        s.records,
		PlainHTTPWWW:   s.plainHTTPWWW,
		Tags:           s.tags,
		Dial:           s.dial,
		Roots:          s.roots,
		Now:            s.clock,
		Sleep:          s.sleep,
		RetryBound:     s.retryBound,
		RetryInterval:  s.retryInterval,
		CertMargin:     livecheck.MinCertMargin,
		MaxBodyBytes:   s.maxBodyBytes,
		RequestTimeout: s.requestTimeout,
	}
}

// clock returns the fixture's fake clock.
func (s *site) clock() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.now
}

// sleep advances the fake clock by d and records the sleep, then runs the
// test's hook, if any: no test waits in real time.
func (s *site) sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	s.now = s.now.Add(d)
	s.sleeps++
	hook := s.onSleep
	s.mu.Unlock()

	if hook != nil {
		return hook(ctx, d)
	}

	return nil
}

// sleepCount returns how often the fixture's fake sleep ran.
func (s *site) sleepCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sleeps
}

// setHSTS makes the canonical host send value as every response's
// Strict-Transport-Security header; "" sends none.
func (s *site) setHSTS(value string) {
	s.hostTLS.mu.Lock()
	defer s.hostTLS.mu.Unlock()

	s.hostTLS.hsts = value
}

// setApexHSTS makes the apex send value as every HTTPS response's
// Strict-Transport-Security header; "" sends none.
func (s *site) setApexHSTS(value string) {
	s.apexTLS.mu.Lock()
	defer s.apexTLS.mu.Unlock()

	s.apexTLS.hsts = value
}

// pursuePreload makes the fixture pursue preload as hsts.json's preload true
// requires (behaviour 8): the HSTS record says so, and http://<apex>/<p>
// redirects to https://<apex>/<p> before the canonical host does.
func (s *site) pursuePreload(t *testing.T) {
	t.Helper()

	s.records.HSTS = parsedHSTSRecord(t, hstsRecordPreload)

	s.apexHTTP.set("/", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultApex + "/")})
	s.apexHTTP.set("/privacy.html", served{status: http.StatusMovedPermanently, header: redirect("https://" + livecheck.DefaultApex + "/privacy.html")})
}

// setVersion makes the live root page show version, as the deployed index.html
// would: both "/" and "/index.html" carry it, so the manifest and the root
// agree on the bytes and only the version differs.
func (s *site) setVersion(version string) {
	s.hostTLS.setBody("/", indexPage(version))
	s.hostTLS.setBody("/index.html", indexPage(version))
}

// setPrivacy makes privacy.html carry the statements a test asks for.
func (s *site) setPrivacy(withDowngradeRisk, withHSTSWaived bool) {
	s.hostTLS.setBody("/privacy.html", privacyPage(withDowngradeRisk, withHSTSWaived))
}

// setVerify makes verify.html carry the statements a test asks for.
func (s *site) setVerify(withHSTSWaived bool) {
	s.hostTLS.setBody("/verify.html", verifyPage(withHSTSWaived))
}

// allContent returns the four servers' content, for the request assertions.
func (s *site) allContent() []*content {
	return []*content{s.hostTLS, s.apexTLS, s.hostHTTP, s.apexHTTP}
}

// reportText returns the report's text form, or the error that kept it from
// being written, for a failure message.
func reportText(report livecheck.Report) string {
	var buf bytes.Buffer

	if err := report.WriteText(&buf); err != nil {
		return fmt.Sprintf("(the report's text could not be written: %v)", err)
	}

	return buf.String()
}

// findCheck returns the check of kind and target, and whether the report holds
// one.
func findCheck(report livecheck.Report, kind, target string) (livecheck.Check, bool) {
	for _, check := range report.Checks {
		if check.Kind == kind && check.Target == target {
			return check, true
		}
	}

	return livecheck.Check{}, false
}

// hasKind reports whether the report holds a check of kind.
func hasKind(report livecheck.Report, kind string) bool {
	for _, check := range report.Checks {
		if check.Kind == kind {
			return true
		}
	}

	return false
}

// runResult is what Run returned, for the goroutine that runs it under a
// guard.
type runResult struct {
	report livecheck.Report
	err    error
}

// runWithin returns Run's result, or fails the test when Run has not returned
// within bound. The guard is the one place a check is allowed a wall-clock
// bound: a server that never answers must make a check fail, not hang the
// package's run, so a hang fails this test instead of the go test timeout
// killing the whole package.
func runWithin(t *testing.T, bound time.Duration, cfg livecheck.Config) (livecheck.Report, error) {
	t.Helper()

	ctx := t.Context()
	done := make(chan runResult, 1)

	go func() {
		report, err := livecheck.Run(ctx, cfg)
		done <- runResult{report: report, err: err}
	}()

	select {
	case got := <-done:
		return got.report, got.err
	case <-time.After(bound):
		t.Fatalf("the check did not return within %v: it hangs on a server that never answers", bound)

		return livecheck.Report{}, nil
	}
}

// stalledListener starts a listener that accepts every connection and never
// writes or closes it, as a host that accepts the TCP connection and never
// sends a ServerHello (or an HTTP response) does. It returns its address; the
// connections it accepted are closed when the test ends.
func stalledListener(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for the stalled listener: %v", err)
	}

	var (
		mu    sync.Mutex
		conns []net.Conn
	)

	done := make(chan struct{})

	go func() {
		defer close(done)

		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		<-done

		mu.Lock()
		defer mu.Unlock()

		for _, conn := range conns {
			_ = conn.Close()
		}
	})

	return ln.Addr().String()
}

// rawAnswerOrder is the order a raw edge answers a connection in.
type rawAnswerOrder int

const (
	// rawWriteOnAccept writes the response as soon as the connection is accepted
	// and reads nothing of the request: the edge whose bytes Go's transport can
	// discard as unsolicited, and a browser renders.
	rawWriteOnAccept rawAnswerOrder = iota
	// rawReadTheRequest reads the request's header up to its empty line, then
	// writes the response, then half-closes, so the client reads the bytes with
	// its request registered, as an ordinary edge's answer arrives.
	rawReadTheRequest
)

// rawListenerGuard bounds a raw listener's read of the request and its drain, so a
// connection that goes silent fails the test instead of holding the listener.
const rawListenerGuard = 10 * time.Second

// rawListener starts a listener that answers every connection with exactly
// response, in the order order names, as an edge answering plain HTTP with
// something that is no HTTP/1.x response does (raw HTML without a status line, a
// header block Go's parser refuses). It returns its address.
func rawListener(t *testing.T, response string, order rawAnswerOrder) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for the raw listener: %v", err)
	}

	var (
		mu    sync.Mutex
		conns []net.Conn
	)

	done := make(chan struct{})

	go func() {
		defer close(done)

		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()

			go answerRaw(conn, []byte(response), order)
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		<-done

		mu.Lock()
		defer mu.Unlock()

		for _, conn := range conns {
			_ = conn.Close()
		}
	})

	return ln.Addr().String()
}

// answerRaw answers one connection in the order order names.
func answerRaw(conn net.Conn, response []byte, order rawAnswerOrder) {
	if order == rawWriteOnAccept {
		// The edge writes before it reads anything and then leaves the connection
		// open (the test's cleanup closes it): a close here would let the kernel
		// answer the request with a reset that can throw the bytes away before the
		// client reads them, which is not the order this case is about. The bytes
		// wait in the client's socket for the transport's read loop to discard
		// them.
		_, _ = conn.Write(response)

		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(rawListenerGuard))
	readRequestHeader(conn)
	_ = conn.SetReadDeadline(time.Time{})

	_, _ = conn.Write(response)

	// A half close, not a close: the client sees a graceful end of the bytes
	// instead of a reset. Then drain what the client still sends, so closing with
	// it unread does not reset the connection either.
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}

	_ = conn.SetReadDeadline(time.Now().Add(rawListenerGuard))
	_, _ = io.Copy(io.Discard, conn)
	_ = conn.Close()
}

// readRequestHeader reads conn up to and including the empty line that ends the
// request's header.
func readRequestHeader(conn net.Conn) {
	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			return
		}
	}
}

// closingListener starts a listener that accepts every connection and closes it
// without writing a response, and returns its address. It is what
// refuse-or-close looks like when the edge does not refuse the connection.
func closingListener(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for the closing listener: %v", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			_ = conn.Close()
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	return ln.Addr().String()
}
