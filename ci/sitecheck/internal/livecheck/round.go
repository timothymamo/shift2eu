package livecheck

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"sync/atomic"
)

// The pages a site's manifest must list and the checks read.
const (
	indexPage   = "index.html"
	privacyPage = "privacy.html"
	verifyPage  = "verify.html"
)

// The targets of the KindHSTS checks of a record that could not be read.
const (
	recordHSTS    = "hsts.json"
	recordPreload = "preload.json"
)

// kindOrder is the order of the kinds in a Report.
var kindOrder = []string{
	KindManifestFile,
	KindRoot,
	KindServiceWorker,
	KindHTMLCache,
	KindRedirect,
	KindCertificate,
	KindHSTS,
	KindStatement,
	KindVersion,
	KindModuleScriptSize,
}

// checker runs the rounds of one Run: its configuration and the HTTP client
// every request of every round goes through.
type checker struct {
	cfg       Config
	transport *http.Transport
	client    *http.Client
}

// newChecker returns the checker of cfg, which has its defaults set. Its
// client dials through cfg.Dial, verifies certificates against cfg.Roots at
// cfg.Now, asks for no compression of its own (so a body arrives as the edge
// transfers it), uses no proxy and follows no redirect. Every request gets a
// connection of its own (no keep-alive), so what its probe (connProbe) sees
// on the connection belongs to that request alone.
func newChecker(cfg Config) *checker {
	transport := &http.Transport{
		Proxy:             nil,
		DialContext:       probingDial(cfg.Dial),
		DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{
			RootCAs:    cfg.Roots,
			Time:       cfg.Now,
			MinVersion: tls.VersionTLS12,
		},
		TLSHandshakeTimeout:   cfg.RequestTimeout,
		ResponseHeaderTimeout: cfg.RequestTimeout,
		DisableCompression:    true,
	}

	return &checker{
		cfg:       cfg,
		transport: transport,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// close closes the client's idle connections.
func (c *checker) close() {
	c.transport.CloseIdleConnections()
}

// connProbe is what one request's connections showed: whether a dial returned
// a connection, and whether any byte was read from one. It is read from the
// connection itself, not from httptrace: Go's transport reads, and discards
// as "unsolicited", bytes a server sends before the request is registered,
// and never reports a first response byte for them.
type connProbe struct {
	connected atomic.Bool
	read      atomic.Bool
}

// connProbeKey is the context key of a request's *connProbe.
type connProbeKey struct{}

// probingDial returns dial with every connection it makes reporting to the
// *connProbe of the dial's context, if any. The transport dials with a
// context that keeps the request's values.
func probingDial(dial Dialer) Dialer {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)

		probe, _ := ctx.Value(connProbeKey{}).(*connProbe)
		if err != nil || probe == nil {
			return conn, err
		}

		probe.connected.Store(true)

		return &probedConn{Conn: conn, probe: probe}, nil
	}
}

// probedConn is a connection that marks its probe once a Read returns a byte.
type probedConn struct {
	net.Conn

	probe *connProbe
}

// Read reads from the connection and marks the probe when n > 0.
func (c *probedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.probe.read.Store(true)
	}

	return n, err
}

// fetched is one request of a round and what came back.
type fetched struct {
	// err is set when no response came (refused, closed, timed out,
	// unreadable).
	err error
	// refusedOrClosed is set with err when no byte was read from any of the
	// request's connections and either no connection was made (refused, or a
	// dial that timed out) or the one made was closed or reset: not a timeout
	// after connecting, not bytes Go could not parse or discarded.
	refusedOrClosed bool
	status          int
	header          http.Header
	// body is the body as read; bodyErr is set when it could not be.
	body    Body
	bodyErr error
}

// location returns the response's one Location, or an error.
func (f fetched) location() (string, error) {
	values := f.header.Values("Location")
	if len(values) != 1 {
		return "", fmt.Errorf("answered %d with %d Location headers, want one", f.status, len(values))
	}

	return values[0], nil
}

// round is one round of every check, with its report.
type round struct {
	*checker

	checks    []Check
	responses []Response
}

// add records one check.
func (r *round) add(kind, target string, err error) {
	check := Check{Kind: kind, Target: target, Passed: err == nil}
	if err != nil {
		check.Reason = err.Error()
	}

	r.checks = append(r.checks, check)
}

// fetch requests url (built by the checker, never taken from a response) with
// Accept-Encoding: gzip, following no redirect, reads its body under the cap
// and records the response.
func (r *round) fetch(ctx context.Context, url string) fetched {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.RequestTimeout)
	defer cancel()

	var remote string

	probe := &connProbe{}
	ctx = context.WithValue(ctx, connProbeKey{}, probe)
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			remote = info.Conn.RemoteAddr().String()
		},
	})

	recorded := Response{URL: url}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		r.responses = append(r.responses, recorded)

		return fetched{err: err}
	}

	req.Header.Set("Accept-Encoding", EncodingGzip)

	resp, err := r.client.Do(req)
	recorded.RemoteAddr = remote

	if err != nil {
		r.responses = append(r.responses, recorded)

		return fetched{
			err:             fmt.Errorf("no response: %s", Quote(err.Error())),
			refusedOrClosed: refusedOrClosed(probe, err),
		}
	}

	defer func() { _ = resp.Body.Close() }()

	f := fetched{status: resp.StatusCode, header: resp.Header}
	f.body, f.bodyErr = ReadBody(strings.Join(resp.Header.Values("Content-Encoding"), ", "), resp.Body, r.cfg.MaxBodyBytes)

	recorded.Status = resp.StatusCode
	recorded.Age = ParseAge(resp.Header.Values("Age"))
	recorded.CacheControl = strings.Join(resp.Header.Values("Cache-Control"), ", ")
	recorded.HSTS = strings.Join(resp.Header.Values("Strict-Transport-Security"), ", ")
	r.responses = append(r.responses, recorded)

	return f
}

// refusedOrClosed reports whether a request that ended with err got no page
// a browser could show: no byte was read from any of its connections, the run
// was not cancelled, and either no connection was made (refused, or a
// filtered port whose dial timed out) or err is no timeout (the connection
// was closed or reset). A timeout after a connection was made is not: a
// browser would still be waiting. A response whose bytes race a reset passes
// only if none of them was read, which is what a browser can show too.
func refusedOrClosed(probe *connProbe, err error) bool {
	switch {
	case probe.read.Load(), errors.Is(err, context.Canceled):
		return false
	case !probe.connected.Load():
		return true
	default:
		return !isTimeout(err)
	}
}

// isTimeout reports whether err is a timeout: the request's deadline, or a
// dial, handshake or read that timed out.
func isTimeout(err error) bool {
	var netErr net.Error

	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

// round runs every check once and returns its report.
func (c *checker) round(ctx context.Context) Report {
	r := &round{checker: c}
	host := "https://" + c.cfg.Host

	files := make(map[string]fetched, len(c.cfg.Manifest.Files))
	matched := make(map[string]bool, len(c.cfg.Manifest.Files))

	for _, file := range c.cfg.Manifest.Files {
		f := r.fetch(ctx, host+"/"+file.Path)
		files[file.Path] = f

		err := judgeFile(f, file.SHA256)
		matched[file.Path] = err == nil
		r.add(KindManifestFile, file.Path, err)
	}

	root := r.fetch(ctx, host+"/")
	indexDigest, _ := c.cfg.Manifest.Digest(indexPage)
	rootErr := judgeFile(root, indexDigest)
	r.add(KindRoot, "/", rootErr)

	for _, path := range ServiceWorkerPaths() {
		r.add(KindServiceWorker, path, judgeServiceWorker(r.fetch(ctx, host+path)))
	}

	r.add(KindHTMLCache, "/", judgeCache(root))

	for _, page := range c.cfg.Manifest.HTMLPages() {
		r.add(KindHTMLCache, page, judgeCache(files[page]))
	}

	record, recordErr := c.cfg.Records.HSTS, c.cfg.Records.HSTSErr
	if recordErr != nil {
		// Fail safe: the record's own check fails, and the rest is judged as
		// if HSTS were met, without preload and with plain HTTP redirecting.
		record = HSTSRecord{Outcome: HSTSMet, PlainHTTPWWW: PlainHTTPRedirect}
	}

	apexHTTPS := r.redirectMatrix(ctx, record)

	for _, name := range []string{c.cfg.Host, c.cfg.Apex} {
		err := c.checkCertificate(ctx, name)
		if err != nil {
			err = errors.New(Quote(err.Error()))
		}

		r.add(KindCertificate, name, err)
	}

	r.judgeHSTS(root, apexHTTPS, record, recordErr)
	r.judgeStatements(files, matched, record, recordErr)

	report := Report{Host: c.cfg.Host, Apex: c.cfg.Apex}

	tag, err := c.judgeVersion(root, rootErr)
	r.add(KindVersion, "/", err)

	if err == nil {
		report.Version = tag
	}

	script, _ := c.cfg.Manifest.ModuleScript()
	report.ModuleScript, err = judgeModuleScript(files[script])
	r.add(KindModuleScriptSize, script, err)

	slices.SortStableFunc(r.checks, func(a, b Check) int {
		if byKind := slices.Index(kindOrder, a.Kind) - slices.Index(kindOrder, b.Kind); byKind != 0 {
			return byKind
		}

		return strings.Compare(a.Target, b.Target)
	})

	report.Checks = r.checks
	report.Responses = r.responses

	return report
}

// checkCertificate runs CheckCertificate for name, its dial and handshake
// bounded by the request timeout.
func (c *checker) checkCertificate(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()

	_, err := CheckCertificate(ctx, c.cfg.Dial, c.cfg.Roots, name, c.cfg.Now(), c.cfg.CertMargin)

	return err
}

// judgeFile returns nil when f is a 200 whose body, after content decoding,
// has the SHA-256 digest.
func judgeFile(f fetched, digest string) error {
	switch {
	case f.err != nil:
		return f.err
	case f.status >= 300 && f.status < 400:
		return fmt.Errorf("answered %d, a redirect the check does not follow (Location %s)", f.status, Quote(f.header.Get("Location")))
	case f.status != http.StatusOK:
		return fmt.Errorf("answered %d, want 200", f.status)
	case f.bodyErr != nil:
		return fmt.Errorf("the body could not be read: %w", f.bodyErr)
	}

	if got := sha256Hex(f.body.Data); got != digest {
		return fmt.Errorf("the body's SHA-256 after decoding is %s, the manifest's %s", got, digest)
	}

	return nil
}

// sha256Hex returns the lowercase hexadecimal SHA-256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

// judgeServiceWorker returns nil when f, a service worker path, answered with
// a status outside 2xx. A browser registers a service worker script served
// with any 2xx, so each fails; so does no response at all, which says nothing
// about what the path serves.
func judgeServiceWorker(f fetched) error {
	switch {
	case f.err != nil:
		return f.err
	case f.status >= 200 && f.status < 300:
		return fmt.Errorf("answered %d: a service worker script may be served", f.status)
	}

	return nil
}

// judgeCache returns nil when f, an HTML page, is cached for at most
// MaxHTMLAge.
func judgeCache(f fetched) error {
	if f.err != nil {
		return f.err
	}

	return JudgeHTMLCache(f.header.Values("Cache-Control"))
}

// judgeRedirect returns nil when f is a 301 or 308 to exactly want.
func judgeRedirect(f fetched, want string) error {
	if f.err != nil {
		return f.err
	}

	if f.status != http.StatusMovedPermanently && f.status != http.StatusPermanentRedirect {
		return fmt.Errorf("answered %d, want 301 or 308 to %s", f.status, want)
	}

	location, err := f.location()
	if err != nil {
		return err
	}

	if location != want {
		return fmt.Errorf("redirects to %s, want %s", Quote(location), want)
	}

	return nil
}

// redirectMatrix runs behaviour 6's checks under record and returns the
// responses of https://<apex>/, whose Strict-Transport-Security judgeHSTS
// reads.
func (r *round) redirectMatrix(ctx context.Context, record HSTSRecord) fetched {
	var apexRoot fetched

	for _, path := range RedirectPaths() {
		canonical := "https://" + r.cfg.Host + path
		apexHTTPSURL := "https://" + r.cfg.Apex + path
		apexHTTPURL := "http://" + r.cfg.Apex + path
		hostHTTPURL := "http://" + r.cfg.Host + path

		apexHTTP := r.fetch(ctx, apexHTTPURL)
		apexHTTPS := r.fetch(ctx, apexHTTPSURL)
		hostHTTP := r.fetch(ctx, hostHTTPURL)

		if path == "/" {
			apexRoot = apexHTTPS
		}

		httpsErr := judgeRedirect(apexHTTPS, canonical)

		var apexErr error

		if record.PreloadPursued {
			apexErr = judgeRedirect(apexHTTP, apexHTTPSURL)
			if apexErr == nil && httpsErr != nil {
				apexErr = fmt.Errorf("its second hop %s: %w", apexHTTPSURL, httpsErr)
			}
		} else {
			apexErr = judgeRedirect(apexHTTP, canonical)
		}

		r.add(KindRedirect, apexHTTPURL, apexErr)
		r.add(KindRedirect, apexHTTPSURL, httpsErr)

		if record.PlainHTTPWWW == PlainHTTPRefuseOrClose {
			r.add(KindRedirect, hostHTTPURL, r.judgeRefuseOrClose(hostHTTP, canonical))
		} else {
			r.add(KindRedirect, hostHTTPURL, judgeRedirect(hostHTTP, canonical))
		}
	}

	return apexRoot
}

// judgeRefuseOrClose returns nil when f, plain HTTP on the canonical host in
// refuse-or-close mode, was refused (a dial that timed out with no connection
// included), or closed or reset before any byte was read from the connection,
// or got a 301 or 308 to exactly canonical, or a 4xx or 5xx whose body is no
// manifest file's. A timeout after connecting, or any byte read that Go could
// not parse as a response or discarded (HTTP/0.9, a malformed header block,
// bytes sent before the request), which a browser may still render, fail.
func (r *round) judgeRefuseOrClose(f fetched, canonical string) error {
	switch {
	case f.err != nil && f.refusedOrClosed:
		return nil
	case f.err != nil:
		return fmt.Errorf("%w; want a refused connection, or one closed before any response", f.err)
	case f.status >= 300 && f.status < 400:
		return judgeRedirect(f, canonical)
	case f.status < 400:
		return fmt.Errorf("answered %d, want a refused or closed connection, a redirect to %s or a refusal", f.status, canonical)
	case f.bodyErr != nil:
		return fmt.Errorf("answered %d with a body that could not be read: %w", f.status, f.bodyErr)
	}

	digest := sha256Hex(f.body.Data)

	for _, file := range r.cfg.Manifest.Files {
		if file.SHA256 == digest {
			return fmt.Errorf("answered %d with the bytes of %s over plain HTTP", f.status, file.Path)
		}
	}

	return nil
}

// judgeHSTS runs behaviour 8's header checks: each record that could not be
// read fails a check of its own; when HSTS is met, the canonical host's header
// (from root) is held to the minimum, and the apex's (from apexRoot) too when
// preload is pursued.
func (r *round) judgeHSTS(root, apexRoot fetched, record HSTSRecord, recordErr error) {
	if recordErr != nil {
		r.add(KindHSTS, recordHSTS, fmt.Errorf("the HSTS record could not be read (fail safe): %w", recordErr))
	}

	if err := r.cfg.Records.PreloadErr; err != nil {
		r.add(KindHSTS, recordPreload, fmt.Errorf("the preload record could not be read (fail safe, counted as not on the preload list): %w", err))
	}

	if record.Outcome != HSTSMet {
		return
	}

	r.add(KindHSTS, r.cfg.Host, judgeHSTSHeader(root, record.PreloadPursued))

	if record.PreloadPursued {
		r.add(KindHSTS, r.cfg.Apex, judgeHSTSHeader(apexRoot, true))
	}
}

// judgeHSTSHeader returns nil when f's Strict-Transport-Security meets the
// minimum.
func judgeHSTSHeader(f fetched, preload bool) error {
	if f.err != nil {
		return f.err
	}

	hsts, err := ParseHSTS(f.header.Values("Strict-Transport-Security"))
	if err != nil {
		return err
	}

	return hsts.Meets(preload)
}

// judgeStatements runs behaviour 8's statement checks on the pages whose
// digest matched the manifest.
func (r *round) judgeStatements(files map[string]fetched, matched map[string]bool, record HSTSRecord, recordErr error) {
	carried := func(page, marker string) {
		var err error

		switch {
		case !matched[page]:
			err = fmt.Errorf("the deployed %s is not the manifest's", page)
		case !Carries(files[page].body.Data, marker):
			err = fmt.Errorf("%s carries no visible, non-empty element with id=%q", page, marker)
		}

		r.add(KindStatement, page+"#"+marker, err)
	}

	if recordErr == nil && record.Outcome == HSTSWaived {
		carried(privacyPage, MarkerHSTSWaived)
		carried(verifyPage, MarkerHSTSWaived)
	}

	if r.cfg.Records.PreloadErr != nil || !r.cfg.Records.Preload.OnPreloadList {
		carried(privacyPage, MarkerDowngradeRisk)
	}
}

// judgeVersion reads the live version from root and returns its Tag when it
// is a tag of the list and the manifest's version. rootErr is the root's own
// check: only a root whose bytes are the manifest's index.html is read, so
// the scan never runs over a page the release did not build.
func (c *checker) judgeVersion(root fetched, rootErr error) (Tag, error) {
	if rootErr != nil {
		return Tag{}, fmt.Errorf("the root is not the manifest's %s, so its version is not read: %w", indexPage, rootErr)
	}

	text, err := VersionText(root.body.Data)
	if err != nil {
		return Tag{}, err
	}

	tag, err := MatchVersion(text, c.cfg.Tags)
	if err != nil {
		return Tag{}, err
	}

	if tag.String() != c.cfg.Manifest.Version {
		return Tag{}, fmt.Errorf("the live version %s is a tag of the repository, but not the release's %s", tag.String(), Quote(c.cfg.Manifest.Version))
	}

	return tag, nil
}

// judgeModuleScript measures the module script's response.
func judgeModuleScript(f fetched) (Size, error) {
	switch {
	case f.err != nil:
		return Size{}, f.err
	case f.status != http.StatusOK:
		return Size{}, fmt.Errorf("answered %d, want 200", f.status)
	case f.bodyErr != nil:
		return Size{}, fmt.Errorf("the body could not be read: %w", f.bodyErr)
	}

	return MeasureModuleScript(strings.Join(f.header.Values("Content-Type"), ", "), f.body)
}
