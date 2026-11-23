package livecheck

import (
	"context"
	"crypto/x509"
	"net"
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
	return stub(c)
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
	return Report{}, stub(ctx, cfg)
}

// CheckCertificate dials host:443 through dial, does a TLS handshake with
// host as the server name, verifies the served chain for host against roots
// (nil: the system roots) at the time now, not the wall clock, and returns
// the leaf's NotAfter. A chain that does not verify, a leaf not valid for
// host or not yet valid at now, or a NotAfter before now+margin is an error
// (behaviour 7). margin below MinCertMargin is an error before any dial.
func CheckCertificate(ctx context.Context, dial Dialer, roots *x509.CertPool, host string, now time.Time, margin time.Duration) (time.Time, error) {
	return time.Time{}, stub(ctx, dial, roots, host, now, margin)
}
