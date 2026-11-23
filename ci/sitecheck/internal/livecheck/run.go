package livecheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// The production hosts (Hosting decisions of PRD #297).
const (
	// DefaultHost is the canonical host the site is served from.
	DefaultHost = "www.shift2eu.com"
	// DefaultApex is the apex, which only redirects to the canonical host.
	DefaultApex = "shift2eu.com"
)

// MinRetryBound is the least Config.RetryBound: the HTML's max-age, so a
// page cached before the deploy has expired before the check gives up.
const MinRetryBound = MaxHTMLAge * time.Second

// MinCertMargin is the least Config.CertMargin: 14 days.
const MinCertMargin = 14 * 24 * time.Hour

// DefaultRequestTimeout bounds one request, or one TLS handshake, when
// Config.RequestTimeout is zero.
const DefaultRequestTimeout = 30 * time.Second

// Dialer opens the TCP connection for network ("tcp") and addr ("host:port",
// the URL's host and port 443 or 80). Run and CheckCertificate dial every
// connection through it, plain HTTP and TLS alike, and do TLS themselves on top
// with the URL's host as the server name, so a test maps
// "www.shift2eu.com:443" to an httptest TLS server's listener address and
// the certificate is still verified for www.shift2eu.com. An error, or a
// connection closed before any response, is what a refused connection looks
// like (behaviour 6's refuse-or-close).
type Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

// Config is everything Run depends on. Every field without a stated default
// is required; Validate says which is wrong.
type Config struct {
	// Host is the canonical host (DefaultHost in production) and Apex the
	// apex (DefaultApex); both plain DNS names, no port, no scheme, and not
	// equal.
	Host string
	Apex string

	// Manifest is the release's manifest (ParseManifest).
	Manifest Manifest
	// Records are the HSTS and preload records, or the errors reading them.
	Records Records
	// Tags is the repository's tag list (ReadTagList). Empty fails the
	// KindVersion check, never the configuration.
	Tags []string

	// Dial opens every connection. Nil dials with a net.Dialer bounded by
	// RequestTimeout (production).
	Dial Dialer
	// Roots are the root certificates the served certificates must verify
	// against. Nil means the system roots (production); a test passes the
	// pool of its test CA.
	Roots *x509.CertPool
	// Now is the clock: the certificates' validity and expiry and the retry
	// bound are judged against it, never against time.Now. Required.
	Now func() time.Time
	// Sleep waits d between rounds, or returns ctx's error when ctx ends
	// first. Required; a test passes one that advances its fake clock.
	Sleep func(ctx context.Context, d time.Duration) error

	// RetryBound is how long, on Now's clock from the first round's start,
	// rounds are repeated while a check fails; at least MinRetryBound.
	RetryBound time.Duration
	// RetryInterval is the wait between rounds; positive and at most
	// RetryBound.
	RetryInterval time.Duration
	// CertMargin is how long each certificate must stay valid after Now; at
	// least MinCertMargin.
	CertMargin time.Duration
	// MaxBodyBytes caps every response body, transferred and decoded
	// (ReadBody). Zero means DefaultMaxBodyBytes; it must not be below
	// DecodedBudget+1.
	MaxBodyBytes int64
	// RequestTimeout bounds each request and each handshake. Zero means
	// DefaultRequestTimeout.
	RequestTimeout time.Duration
}

// Validate returns nil when c is a usable configuration, or an error naming
// the first field that is not: Host or Apex empty, not a DNS name, or equal;
// RetryBound below MinRetryBound; RetryInterval not positive or above
// RetryBound; CertMargin below MinCertMargin; MaxBodyBytes negative or
// non-zero below DecodedBudget+1; RequestTimeout negative; Now or Sleep nil;
// a Manifest with no files, without index.html, privacy.html or verify.html,
// or listing a path of ServiceWorkerPaths (misconfigured), or whose
// ModuleScript fails. Records and Tags are never a configuration error: they
// fail their checks instead.
func (c Config) Validate() error {
	for _, check := range []func() error{c.validateHosts, c.validateTiming, c.validateLimits, c.validateManifest} {
		if err := check(); err != nil {
			return err
		}
	}

	return nil
}

// validateHosts checks Host and Apex.
func (c Config) validateHosts() error {
	if !isDNSName(c.Host) {
		return fmt.Errorf("the host %q is not a DNS name", c.Host)
	}

	if !isDNSName(c.Apex) {
		return fmt.Errorf("the apex %q is not a DNS name", c.Apex)
	}

	if strings.EqualFold(c.Host, c.Apex) {
		return fmt.Errorf("the host and the apex are both %q", c.Host)
	}

	return nil
}

// validateTiming checks the clock, the sleep, the retry bound and interval
// and the certificate margin.
func (c Config) validateTiming() error {
	switch {
	case c.RetryBound < MinRetryBound:
		return fmt.Errorf("the retry bound %v is below the HTML's max-age, %v", c.RetryBound, MinRetryBound)
	case c.RetryInterval <= 0 || c.RetryInterval > c.RetryBound:
		return fmt.Errorf("the retry interval %v is not positive or over the retry bound %v", c.RetryInterval, c.RetryBound)
	case c.CertMargin < MinCertMargin:
		return fmt.Errorf("the certificate margin %v is below %v", c.CertMargin, MinCertMargin)
	case c.Now == nil:
		return errors.New("no clock (Now)")
	case c.Sleep == nil:
		return errors.New("no sleep (Sleep)")
	}

	return nil
}

// validateLimits checks the body cap and the request timeout.
func (c Config) validateLimits() error {
	switch {
	case c.MaxBodyBytes < 0 || (c.MaxBodyBytes != 0 && c.MaxBodyBytes <= DecodedBudget):
		return fmt.Errorf("the body cap %d is below the decoded budget plus one byte, %d", c.MaxBodyBytes, DecodedBudget+1)
	case c.RequestTimeout < 0:
		return fmt.Errorf("the request timeout %v is negative", c.RequestTimeout)
	}

	return nil
}

// validateManifest checks that the manifest is a site's: clean paths and
// digests, the pages the checks read, no service worker and one module
// script.
func (c Config) validateManifest() error {
	if len(c.Manifest.Files) == 0 {
		return errors.New("the manifest lists no file")
	}

	for _, file := range c.Manifest.Files {
		if err := checkManifestPath(file.Path); err != nil {
			return fmt.Errorf("the manifest's path %q: %w", file.Path, err)
		}

		if !digestPattern.MatchString(file.SHA256) {
			return fmt.Errorf("the manifest's path %q has the sha256 %q, not 64 lowercase hexadecimal characters", file.Path, file.SHA256)
		}
	}

	for _, page := range []string{indexPage, privacyPage, verifyPage} {
		if _, ok := c.Manifest.Digest(page); !ok {
			return fmt.Errorf("the manifest lists no %s", page)
		}
	}

	for _, path := range ServiceWorkerPaths() {
		if _, ok := c.Manifest.Digest(strings.TrimPrefix(path, "/")); ok {
			return fmt.Errorf("the manifest lists the service worker path %s: misconfigured", path)
		}
	}

	if _, err := c.Manifest.ModuleScript(); err != nil {
		return err
	}

	return nil
}

// isDNSName reports whether s is a plain DNS name: dot-separated labels of
// 1 to 63 lowercase letters, digits and inner hyphens, at most 253 bytes.
func isDNSName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}

	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}

		for i := range len(label) {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}

	return true
}

// withDefaults returns c with its zero optional fields set.
func (c Config) withDefaults() Config {
	if c.MaxBodyBytes == 0 {
		c.MaxBodyBytes = DefaultMaxBodyBytes
	}

	if c.RequestTimeout == 0 {
		c.RequestTimeout = DefaultRequestTimeout
	}

	if c.Dial == nil {
		d := &net.Dialer{Timeout: c.RequestTimeout}
		c.Dial = d.DialContext
	}

	return c
}

// Run checks the live site: it validates cfg (an invalid one is returned as
// the error, with the zero Report, before any connection), then runs rounds
// of every check of the package doc, all URLs built from cfg.Host, cfg.Apex
// and the manifest's paths, no request carrying a query string, every request
// sending Accept-Encoding: gzip and following no redirect. A round's start is
// read from cfg.Now. While a round has a failing check and the time from the
// first round's start to Now plus RetryInterval does not exceed RetryBound,
// Run sleeps RetryInterval through cfg.Sleep and runs another round; it never
// sleeps after a round that passed. A Sleep error (ctx ended) ends the run:
// the report holds the last finished round, and the error is ctx's.
//
// The Report holds the last round's checks, the number of rounds, every
// response of that round (URL, remote address, status, Age, Cache-Control),
// the module script's Size and the matched Tag. A check failure is no error:
// Run returns a nil error and a Report whose Passed is false.
func Run(ctx context.Context, cfg Config) (Report, error) {
	if err := cfg.Validate(); err != nil {
		return Report{}, err
	}

	cfg = cfg.withDefaults()
	c := newChecker(cfg)

	defer c.close()

	start := cfg.Now()

	for rounds := 1; ; rounds++ {
		report := c.round(ctx)
		report.Rounds = rounds

		if report.Passed() || cfg.Now().Sub(start)+cfg.RetryInterval > cfg.RetryBound {
			return report, nil
		}

		if err := cfg.Sleep(ctx, cfg.RetryInterval); err != nil {
			return report, err
		}
	}
}

// CheckCertificate dials host:443 through dial, does a TLS handshake with
// host as the server name, verifies the served chain for host against roots
// (nil: the system roots) at the time now, not the wall clock, and returns
// the leaf's NotAfter. A chain that does not verify, a leaf not valid for
// host or not yet valid at now, or a NotAfter before now+margin is an error
// (behaviour 7). margin below MinCertMargin is an error before any dial.
func CheckCertificate(ctx context.Context, dial Dialer, roots *x509.CertPool, host string, now time.Time, margin time.Duration) (time.Time, error) {
	if margin < MinCertMargin {
		return time.Time{}, fmt.Errorf("the certificate margin %v is below %v", margin, MinCertMargin)
	}

	if dial == nil {
		d := &net.Dialer{Timeout: DefaultRequestTimeout}
		dial = d.DialContext
	}

	conn, err := dial(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return time.Time{}, fmt.Errorf("connect to %s: %w", host, err)
	}

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: host,
		RootCAs:    roots,
		Time:       func() time.Time { return now },
		MinVersion: tls.VersionTLS12,
	})

	defer func() { _ = tlsConn.Close() }()

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return time.Time{}, fmt.Errorf("the certificate of %s does not verify: %w", host, err)
	}

	peers := tlsConn.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		return time.Time{}, fmt.Errorf("%s served no certificate", host)
	}

	notAfter := peers[0].NotAfter
	if notAfter.Before(now.Add(margin)) {
		return notAfter, fmt.Errorf("the certificate of %s expires at %s, within the margin of %v", host, notAfter.UTC().Format(time.RFC3339), margin)
	}

	return notAfter, nil
}
