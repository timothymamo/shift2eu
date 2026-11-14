package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
)

// Request is a decoded body of POST /v1/assessments.
type Request struct {
	// Inventory is the inventory document, as the raw bytes the body holds.
	Inventory []byte
	// Targets are the target keys in the order sent.
	Targets []string
	// Regions maps a target key to the one region to assess that target in;
	// nil when the body names none. The API checks every key against the
	// selected targets, and every region against its target's catalog.
	Regions map[string]string
	// Estimate turns plans and estimates on: true unless the body says false.
	Estimate bool
}

// Deps are the adapters the API runs with. The composition root
// (cmd/shift2eu-server) builds them; the API never constructs an adapter itself.
// New refuses a Deps that lacks any of them.
type Deps struct {
	// DecodeRequest strictly decodes the body of POST /v1/assessments into a
	// Request (assessmentrequest.Decode, converted by the composition root).
	DecodeRequest func(body []byte) (Request, error)
	// NewInventorySource returns the inventory source over one inventory
	// document (inventoryfile.FromBytes).
	NewInventorySource func(doc []byte) analysis.InventorySource
	// Targets maps every supported target key to the constructor of its catalog
	// source, as cli.Deps.Targets does.
	Targets map[string]func() analysis.CatalogSource
	// JSON is the representation application/json selects: the JSON report
	// (ADR 0018).
	JSON Format
	// Markdown is the representation text/markdown selects: the Markdown
	// report.
	Markdown Format
}

// Format is one representation of a report: the constructors of its
// reporters. A request builds only the one it needs.
type Format struct {
	// NewReporter returns the reporter of an assessment against one target.
	NewReporter func() analysis.Reporter
	// NewComparisonReporter returns the reporter of a comparison of several
	// targets.
	NewComparisonReporter func() analysis.ComparisonReporter
}

// Option configures the handler New returns.
type Option func(*config)

// defaultMaxBodyBytes is the body cap without WithMaxBodyBytes: the inventory
// reader's 10 MiB cap plus 64 KiB for the request envelope, the value of
// assessmentrequest.MaxBodySize, which this package may not import.
const defaultMaxBodyBytes = 10<<20 + 64<<10

// defaultBodyReadTimeout is how long reading one body may hold its slot
// without WithBodyReadTimeout.
const defaultBodyReadTimeout = 30 * time.Second

// config is what the options set.
type config struct {
	maxBodyBytes    int64
	maxConcurrent   int
	bodyReadTimeout time.Duration
	// tokenSet is true once WithBearerToken was given a valid token, whose
	// SHA-256 digest is tokenDigest.
	tokenSet    bool
	tokenDigest [sha256.Size]byte
	// hosts holds the allowed hosts, lower case; nil serves every Host.
	hosts map[string]bool
	// origins holds the allowed origins, normalised by originKey; empty is
	// CORS off.
	origins   map[string]bool
	readiness func(context.Context) error
	// err is the first configuration error an option found; New returns it.
	err error
}

// fail records err as the configuration error unless one is recorded already.
func (c *config) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// errInvalidOption is wrapped by every configuration error an option reports
// through New.
var errInvalidOption = errors.New("httpapi: invalid option")

// WithMaxBodyBytes caps the body of POST /v1/assessments at n bytes; a larger
// body is 413 payload_too_large. A value below 1 keeps the default, the
// inventory reader's cap plus 64 KiB.
func WithMaxBodyBytes(n int64) Option {
	return func(c *config) {
		if n > 0 {
			c.maxBodyBytes = n
		}
	}
}

// WithMaxConcurrent sets how many assessments run at once; one more is refused
// with 503 busy. The default is runtime.NumCPU(); a value below 1 means 1.
func WithMaxConcurrent(n int) Option {
	return func(c *config) {
		c.maxConcurrent = max(n, 1)
	}
}

// WithBodyReadTimeout bounds how long reading the body of one POST
// /v1/assessments may hold its slot, from the moment the slot is taken; past
// it the read fails and the slot is freed. It replaces the connection's read
// deadline, and so the server's ReadTimeout, for that body: a caller passes a
// value no greater than its ReadTimeout. A value below or equal to 0 keeps
// the default, 30 seconds.
func WithBodyReadTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.bodyReadTimeout = d
		}
	}
}

// WithBearerToken requires Authorization: Bearer <token> on every /v1/
// request. Without it authentication is off. The token must be a valid bearer
// token, an RFC 6750 token68: an empty token, or one with white space, a
// control character or any other character token68 does not allow, is a
// configuration error from New, never authentication turned off.
func WithBearerToken(token string) Option {
	return func(c *config) {
		if !isToken68(token) {
			c.fail(errors.New(errInvalidOption.Error() + ": the bearer token is empty or not an RFC 6750 token68"))

			return
		}

		c.tokenSet = true
		c.tokenDigest = sha256.Sum256([]byte(token))
	}
}

// isToken68 reports whether s is an RFC 6750 token68: one or more of ALPHA,
// DIGIT, "-", ".", "_", "~", "+" and "/", followed by any number of "=".
func isToken68(s string) bool {
	body := strings.TrimRight(s, "=")
	if body == "" {
		return false
	}

	for i := range len(body) {
		b := body[i]

		switch {
		case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
		case strings.IndexByte("-._~+/", b) >= 0:
		default:
			return false
		}
	}

	return true
}

// WithAllowedHosts serves only requests whose Host is one of hosts, compared
// ignoring case; any other Host is 421 misdirected_request, before any other
// check. An entry without a port matches its host on any port; an entry with
// one ("localhost:8080") matches that port only. /healthz and /readyz are
// exempt. Without it every Host is served. Each entry is a name, an IPv4
// address or a bracketed IPv6 address ("[2001:db8::1]"), with an optional
// port that is a number: no entry at all, an empty entry, an unbracketed IPv6
// address, an empty port or one that is not a number is a configuration error
// from New, never an allow list that serves another host.
func WithAllowedHosts(hosts ...string) Option {
	return func(c *config) {
		if len(hosts) == 0 {
			c.fail(errors.New(errInvalidOption.Error() + ": the allowed host list is empty"))

			return
		}

		for _, host := range hosts {
			key, ok := allowedHostKey(host)
			if !ok {
				c.fail(errors.New(errInvalidOption.Error() + ": an allowed host is empty or not a host"))

				return
			}

			if c.hosts == nil {
				c.hosts = make(map[string]bool)
			}

			c.hosts[key] = true
		}
	}
}

// allowedHostKey returns host, an entry of WithAllowedHosts, in lower case as
// the allow list holds it, and false when it is not a host with an optional
// numeric port: a name or IPv4 address, or an IPv6 address in brackets.
func allowedHostKey(host string) (string, bool) {
	host = strings.ToLower(host)

	port, hasPort := "", false

	if strings.HasPrefix(host, "[") {
		end := strings.IndexByte(host, ']')
		if end < 0 {
			return "", false
		}

		if !validIPv6Text(host[1:end]) {
			return "", false
		}

		rest := host[end+1:]

		if rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return "", false
			}

			port, hasPort = rest[1:], true
		}
	} else {
		name := host

		switch strings.Count(host, ":") {
		case 0:
		case 1:
			i := strings.IndexByte(host, ':')
			name, port, hasPort = host[:i], host[i+1:], true
		default:
			return "", false // An IPv6 address without brackets.
		}

		if !validHostName(name) {
			return "", false
		}
	}

	if hasPort && !validPort(port) {
		return "", false
	}

	return host, true
}

// validIPv6Text reports whether addr, the text between the brackets of a
// host, has the shape of an IPv6 address: hexadecimal digits and at least two
// colons, with dots for an embedded IPv4 address. A malformed address of that
// shape matches no request's Host, so it allows nothing it should not.
func validIPv6Text(addr string) bool {
	if strings.Count(addr, ":") < 2 {
		return false
	}

	for _, r := range addr {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r == ':', r == '.':
		default:
			return false
		}
	}

	return true
}

// validHostName reports whether name, in lower case, is a DNS name or an IPv4
// address: letters, digits, dots, hyphens and underscores, and not empty.
func validHostName(name string) bool {
	if name == "" {
		return false
	}

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}

	return true
}

// validPort reports whether port is a decimal TCP port, 0 to 65535.
func validPort(port string) bool {
	if port == "" || len(port) > 5 {
		return false
	}

	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
	}

	n, err := strconv.Atoi(port)

	return err == nil && n <= 65535
}

// WithAllowedOrigins turns CORS on for exactly these origins. Without it CORS
// is off. Each origin is a scheme, http or https, and a host with an optional
// port, nothing else: no origin at all, an empty entry, null, *, an empty
// port, or one with a path (a trailing slash included), a query, a fragment
// or user info is a configuration error from New. An Origin matches an entry
// by scheme and host ignoring case and by port exactly, once the scheme's
// default port (443 for https, 80 for http) is dropped on both sides.
func WithAllowedOrigins(origins ...string) Option {
	return func(c *config) {
		if len(origins) == 0 {
			c.fail(errors.New(errInvalidOption.Error() + ": the allowed origin list is empty"))

			return
		}

		for _, origin := range origins {
			key, ok := originKey(origin)
			if !ok {
				c.fail(errors.New(errInvalidOption.Error() + ": an allowed origin is not a scheme and host"))

				return
			}

			c.origins[key] = true
		}
	}
}

// originKey returns origin as the allowed origins hold it, the scheme and the
// host in lower case and the port as given unless it is the scheme's default,
// which is dropped, and false when origin is not an http or https origin with
// nothing but a scheme, a host and a port that is not empty.
func originKey(origin string) (string, bool) {
	u, err := url.Parse(origin)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" || u.Hostname() == "" ||
		u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || strings.ContainsAny(origin, "?#") || strings.HasSuffix(u.Host, ":") {
		return "", false
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}

	host := strings.ToLower(u.Host)
	if port := u.Port(); (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		host = strings.TrimSuffix(host, ":"+port)
	}

	return scheme + "://" + host, true
}

// WithReadiness sets the check GET /readyz runs: nil from it is ready, an
// error is 503 not_ready. Without it the API is always ready.
func WithReadiness(check func(context.Context) error) Option {
	return func(c *config) {
		c.readiness = check
	}
}

// errIncompleteDeps is returned by New for a Deps that lacks an adapter: a
// wiring mistake in the composition root.
var errIncompleteDeps = errors.New("httpapi: incomplete deps")

// route is one path the API serves: its one method and its handler.
type route struct {
	method  string
	handler func(w http.ResponseWriter, r *http.Request)
	// versioned is true for a /v1/ route: the bearer token and CORS apply.
	versioned bool
}

// api is the handler New returns.
type api struct {
	deps   Deps
	cfg    config
	keys   []string
	slots  chan struct{}
	routes map[string]route

	// targetsMu guards targetsBody, the body of GET /v1/targets once every
	// catalog has loaded; nil until then.
	targetsMu   sync.Mutex
	targetsBody []byte
}

// New returns the handler of the API over deps, configured by opts. It returns
// an error, and no handler, when deps lacks any field or has no target, or
// when an option is given an invalid value.
func New(deps Deps, opts ...Option) (http.Handler, error) {
	if err := checkDeps(deps); err != nil {
		return nil, err
	}

	cfg := config{
		maxBodyBytes:    defaultMaxBodyBytes,
		maxConcurrent:   max(runtime.NumCPU(), 1),
		bodyReadTimeout: defaultBodyReadTimeout,
		origins:         make(map[string]bool),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.err != nil {
		return nil, cfg.err
	}

	a := &api{
		deps:  deps,
		cfg:   cfg,
		keys:  slices.Sorted(maps.Keys(deps.Targets)),
		slots: make(chan struct{}, cfg.maxConcurrent),
	}

	a.routes = map[string]route{
		"/v1/assessments": {method: http.MethodPost, handler: a.assess, versioned: true},
		"/v1/targets":     {method: http.MethodGet, handler: a.targets, versioned: true},
		"/healthz":        {method: http.MethodGet, handler: a.healthz},
		"/readyz":         {method: http.MethodGet, handler: a.readyz},
	}

	return a, nil
}

// checkDeps reports the first field deps lacks.
func checkDeps(deps Deps) error {
	missing := ""

	switch {
	case deps.DecodeRequest == nil:
		missing = "DecodeRequest"
	case deps.NewInventorySource == nil:
		missing = "NewInventorySource"
	case len(deps.Targets) == 0:
		missing = "Targets"
	case deps.JSON.NewReporter == nil:
		missing = "JSON.NewReporter"
	case deps.JSON.NewComparisonReporter == nil:
		missing = "JSON.NewComparisonReporter"
	case deps.Markdown.NewReporter == nil:
		missing = "Markdown.NewReporter"
	case deps.Markdown.NewComparisonReporter == nil:
		missing = "Markdown.NewComparisonReporter"
	}

	if missing != "" {
		return errors.New(errIncompleteDeps.Error() + ": no " + missing)
	}

	for key, newCatalog := range deps.Targets {
		if key == "" || key == allTargets || newCatalog == nil {
			return errors.New(errIncompleteDeps.Error() + ": a target with an empty or reserved key or no constructor")
		}
	}

	return nil
}

// ServeHTTP answers one request: the hardening headers, the Host, routing,
// CORS, the method, the bearer token, then the route's handler, with any panic
// answered as 500.
func (a *api) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w := &recordingWriter{ResponseWriter: rw}

	header := w.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")

	if len(a.cfg.origins) > 0 {
		header.Add("Vary", "Origin")
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			// http.ErrAbortHandler asks the server to abort the response: let it.
			if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(recovered)
			}

			if !w.wroteHeader {
				writeInternal(w)
			}
		}
	}()

	if !a.hostAllowed(r) {
		writeError(w, http.StatusMisdirectedRequest, "misdirected_request", "this server does not serve the requested host")

		return
	}

	rt, known := a.routes[r.URL.Path]
	if !known {
		writeError(w, http.StatusNotFound, "not_found", "no such resource")

		return
	}

	allowedOrigin := rt.versioned && a.originAllowed(r)
	if allowedOrigin {
		header.Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		header.Set("Access-Control-Expose-Headers", "Retry-After, WWW-Authenticate")

		if r.Method == http.MethodOptions {
			header.Set("Access-Control-Allow-Methods", rt.method)
			header.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
			header.Set("Access-Control-Max-Age", strconv.Itoa(preflightMaxAge))
			w.WriteHeader(http.StatusNoContent)

			return
		}
	}

	if r.Method != rt.method {
		header.Set("Allow", rt.method)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "the method is not allowed on this resource")

		return
	}

	if rt.versioned && !a.authorized(r) {
		header.Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")

		return
	}

	rt.handler(w, r)
}

// preflightMaxAge is how many seconds a browser may cache a preflight answer.
const preflightMaxAge = 600

// originAllowed reports whether r carries an Origin that matches the
// configured list: scheme and host ignoring case, port exactly.
func (a *api) originAllowed(r *http.Request) bool {
	if len(a.cfg.origins) == 0 {
		return false
	}

	key, ok := originKey(r.Header.Get("Origin"))

	return ok && a.cfg.origins[key]
}

// hostAllowed reports whether the API serves r's Host: always without an
// allow list and on /healthz and /readyz, otherwise only a Host the list
// names, with its port or, for an entry without one, on any port. It reads
// r.Host, never a forwarded header a client controls.
func (a *api) hostAllowed(r *http.Request) bool {
	if a.cfg.hosts == nil || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
		return true
	}

	host := strings.ToLower(r.Host)

	return a.cfg.hosts[host] || a.cfg.hosts[hostWithoutPort(host)]
}

// hostWithoutPort returns host, a Host header value, without its port; an IPv6
// address keeps its brackets.
func hostWithoutPort(host string) string {
	if strings.HasPrefix(host, "[") {
		if end := strings.IndexByte(host, ']'); end >= 0 {
			return host[:end+1]
		}

		return host
	}

	if i := strings.LastIndexByte(host, ':'); i >= 0 && strings.Count(host, ":") == 1 {
		return host[:i]
	}

	return host
}

// bearerPrefix is the scheme of the Authorization header, with its separator.
const bearerPrefix = "Bearer "

// authorized reports whether r may use a /v1/ route: always without a token,
// otherwise only with Authorization: Bearer <token>. The SHA-256 digests of the
// token sent and the token configured are compared in constant time, so timing
// leaks neither the token nor its length.
func (a *api) authorized(r *http.Request) bool {
	if !a.cfg.tokenSet {
		return true
	}

	value := r.Header.Get("Authorization")
	if len(value) < len(bearerPrefix) || !strings.EqualFold(value[:len(bearerPrefix)], bearerPrefix) {
		return false
	}

	sent := sha256.Sum256([]byte(value[len(bearerPrefix):]))

	return subtle.ConstantTimeCompare(sent[:], a.cfg.tokenDigest[:]) == 1
}

// recordingWriter remembers whether the header was written, so a recovered
// panic answers 500 only when nothing was sent yet.
type recordingWriter struct {
	http.ResponseWriter

	wroteHeader bool
}

func (w *recordingWriter) WriteHeader(status int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.wroteHeader = true

	return w.ResponseWriter.Write(p)
}

// Unwrap gives http.ResponseController and http.MaxBytesReader the writer of
// the server.
func (w *recordingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
