// Package httpapi is the HTTP driving adapter, beside internal/cli: it serves
// the analysis use cases over a small, versioned JSON API (PRD #103, ADR 0019).
// It contains no business rules and constructs no adapter: the composition
// root (cmd/shift2eu-server) passes everything in through Deps, as cmd/shift2eu does
// for the CLI through cli.Deps. New returns an http.Handler; listening, flags,
// the environment, timeouts, graceful shutdown and the access log belong to
// the composition root, not to this package.
//
// The package imports the application, the domain and a named list of standard
// packages only (and may import internal/version), never an adapter, internal/cli or
// cmd/ (depguard rule httpapi-gets-adapters-from-cmd). Strict JSON decoding
// lives in the adapters, because jsondoc (ADR 0009) is internal to
// internal/adapter: Deps.DecodeRequest is assessmentrequest.Decode and
// Deps.NewInventorySource is inventoryfile.FromBytes. The OpenAPI 3.1 document
// api/openapi.json describes every route, request, response, header and error,
// and a test holds the handler to it.
//
// # Routes
//
//	POST /v1/assessments   assess one inventory against one target, or compare several
//	GET  /v1/targets       list the targets, their names and snapshot dates, sorted by key
//	GET  /healthz          200 {"status":"ok"} while the process runs
//	GET  /readyz           200 {"status":"ready"}, or 503 not_ready when the readiness check fails
//
// POST /v1/assessments takes Content-Type application/json (a charset
// parameter other than utf-8, compared ignoring case, is 415) and a body {"inventory": <inventory document>,
// "targets": [<key>, ...]}. One target runs analysis.Analysis; several, or
// ["all"], run analysis.Comparison in sorted key order. The target list
// follows the CLI's rules: an unknown, repeated or empty key, or all with
// another key, is refused. The report names the inventory inventory.json
// (analysis.ReportMeta), because the body carries no file name. The report is
// rendered into memory in full before anything is written, so a failed request
// never sends part of one.
//
// Content negotiation on Accept: none, */*, application/* or application/json
// gives the JSON report (ADR 0018), byte for byte what shift2eu assess --format
// json --schema-version N prints, N being the request's schemaVersion: 2
// unless the body asks for 3 (ADR 0030, issue #307), passed to the
// representation's reporter constructor; text/markdown or text/* gives the Markdown report, byte for
// byte the CLI's, as text/markdown; charset=utf-8. A media range with q=0 is
// excluded, and an entry whose q is not a number in [0, 1] (NaN included) is
// invalid and skipped. Nothing acceptable is 406 not_acceptable.
//
// GET /v1/targets loads every target's catalog once, on the first request, and
// keeps the result; a failed load is not kept and the next request retries it.
//
// An unknown path is 404 not_found; a known path with another method is 405
// method_not_allowed with an Allow header.
//
// # Limits and hardening
//
//   - At most WithMaxConcurrent requests to POST /v1/assessments are served at
//     once (default runtime.NumCPU(), at least 1). A request over the cap is
//     refused at once with 503 busy and Retry-After: 1, never queued. The slot
//     is taken before the body is read, so at most that many bodies are in
//     memory at once; WithBodyReadTimeout (default 30 seconds) bounds how long
//     reading one body may hold its slot. A body that cannot be read in full
//     in time, or at all, is 400 invalid_request with Connection: close, and
//     the server closes the connection after the answer. The handler sets the
//     connection's read deadline for the body and clears it once the body is
//     read, so for POST /v1/assessments the body's bound is
//     WithBodyReadTimeout, measured from when the slot is taken, and replaces
//     the server's ReadTimeout. The composition root must still set the
//     server's ReadHeaderTimeout and ReadTimeout, and pass a
//     WithBodyReadTimeout no greater than its ReadTimeout.
//   - The body is read through http.MaxBytesReader, capped at the limit
//     WithMaxBodyBytes sets (the composition root passes
//     assessmentrequest.MaxBodySize, the inventory reader's cap plus 64 KiB);
//     over it is 413 payload_too_large. A request holds its body, the decoded
//     inventory (Deps.NewInventorySource takes the body's bytes without a copy)
//     and the rendered report in memory, so the concurrency cap is what bounds
//     the server's memory.
//   - WithBearerToken, when set, requires Authorization: Bearer <token> on
//     every /v1/ request, checked before the body is read; without it the
//     answer is 401 unauthorized with WWW-Authenticate: Bearer. The tokens are
//     compared as SHA-256 digests with subtle.ConstantTimeCompare, so timing
//     leaks neither the token nor its length. An empty or malformed token is a
//     configuration error from New. /healthz and /readyz stay open.
//   - WithAllowedHosts, when set, names the Host values served, compared
//     ignoring case (an entry without a port matches any port, one with a
//     port that port only); any other Host is 421 misdirected_request, before
//     any other check. The Host is read from the request, never from a
//     forwarded header. /healthz and /readyz are exempt. It defends a server on
//     loopback against DNS rebinding. New refuses the option with no entry, an
//     empty entry, an unbracketed IPv6 address, or an empty or non-numeric
//     port.
//   - CORS is off unless WithAllowedOrigins names origins. New refuses the
//     option with no entry, and an entry that is null or *, has a path, query,
//     fragment, user info or an empty port, or a scheme other than http or
//     https. An Origin matches an entry by scheme and host ignoring case and
//     by port exactly, once the scheme's default port (443 for https, 80 for
//     http) is dropped on both sides. A matching origin gets
//     Access-Control-Allow-Origin with that origin and
//     Access-Control-Expose-Headers: Retry-After, WWW-Authenticate, and its
//     preflight OPTIONS on a /v1/ route gets 204 with the allowed methods, the
//     headers Authorization, Content-Type and Accept, and a max age of 600
//     seconds. Never *, never credentials. While CORS is configured, every
//     response carries Vary: Origin, whatever the origin.
//   - Every response, errors included, sets Cache-Control: no-store and
//     X-Content-Type-Options: nosniff; a JSON response sets Content-Type:
//     application/json.
//   - Nothing from a request is logged or written anywhere: the package takes no
//     logger and keeps a request only in memory while it is served.
//   - A panic in a handler is recovered and answered as 500 internal, with no
//     stack in the body.
//
// # Errors
//
// Every error has one shape, {"error":{"code":"<code>","message":"<message>"}}:
//
//	400 invalid_request         the body is not a valid request (assessmentrequest.Decode)
//	                            or cannot be read in full within WithBodyReadTimeout
//	400 invalid_inventory       the inventory reader refuses the inventory
//	400 invalid_targets         the target list breaks the CLI's rules; the message lists the targets
//	401 unauthorized            a /v1/ request without the configured bearer token
//	404 not_found               an unknown path
//	405 method_not_allowed      a known path with another method
//	406 not_acceptable          no representation the Accept header allows
//	413 payload_too_large       a body over the cap
//	415 unsupported_media_type  a POST body that is not application/json in utf-8
//	421 misdirected_request     a Host outside WithAllowedHosts
//	500 internal                anything else, with the fixed message "internal error"
//	503 busy                    the concurrency cap is reached
//	503 not_ready               the readiness check fails
//
// No value from a request is ever echoed: a message names what is wrong by
// position and field or by a quoted, escaped key name, never a target value,
// an inventory value, a header value, the Host or the path the client sent. An
// invalid_targets message names a bad target by its index in the list and ends
// with the supported keys, sorted. An invalid_request message is the request
// decoder's, which names a wrong value by its JSON type only. An
// invalid_inventory message comes from the inventory reader's in-memory mode
// (inventoryfile.FromBytes), which names the position and field
// (resources[3].category) and never the value, unlike the CLI's messages from
// inventoryfile.New. Both are escaped as the CLI's are: a control or format
// character, a line or paragraph separator or an invalid byte is written as its
// escape (a line feed as \n, an escape as \x1b), and a message over 1 KiB is
// cut. A 500 carries nothing internal.
//
// A route that exists answers 401 without the token where an unknown one
// answers 404; that is accepted, as the routes are public in api/openapi.json.
// The Markdown report escapes inventory text for Markdown, not for HTML: a
// website that shows it as HTML must render it with a sanitising renderer.
//
// The order of the checks: a Host outside WithAllowedHosts is 421 (except on
// /healthz and /readyz); an unknown path is 404; an allowed origin's
// preflight is answered next, before the token, as a browser sends no
// Authorization on it; then a wrong method is 405 (an OPTIONS from any other
// origin included); then the bearer token on /v1/; then, for POST
// /v1/assessments, the Content-Type, the Accept header, the concurrency cap,
// the body cap and read timeout, and the decoding.
package httpapi
