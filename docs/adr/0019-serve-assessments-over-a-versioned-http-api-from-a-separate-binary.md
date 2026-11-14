# 0019. Serve assessments over a versioned HTTP API from a separate binary

- Status: Accepted; amended 2026-11-12: the bundled OpenAPI document moves from milestone 4 to [issue #139](https://github.com/timothymamo/shift2eu/issues/139),
  which publishes it as the release asset `shift2eu-server_openapi.json` (Consequences);
  amended by issue #147: `shift2eu-server` ships in release archives of its own (Consequences);
  amended by [ADR 0030](0030-serve-the-version-2-report-on-v1.md) (PRD #190): `/v1` serves the JSON report
  `schemaVersion` 2 (Versioning), and an `invalid_region` message may name a region-shaped region the
  client sent (Errors)
- Date: 2026-11-11 (accepted 2026-11-12, issue #112; milestone 3 decisions added 2026-11-12)

## Context

PRD #103 (issue #103) makes `shift2eu` usable as the backend of a website: a client sends an inventory and
gets the assessment back. Milestone 1 gave the CLI and the API one versioned JSON report format
([ADR 0018](0018-version-the-json-report-format-for-the-cli-and-the-api.md)). Milestone 2 adds the HTTP
driving adapter, and this ADR records its design, versioning and security model.

Forces:

- The CLI is guaranteed offline. `TestAssessPathHasNoNetworkOrCloudSDK` (`test/e2e`) fails if
  `cmd/shift2eu` depends on `net` or `net/http` at all. That promise to users who run it on sensitive
  inventories must survive.
- An inventory describes someone's infrastructure (CLAUDE.md rule 2). A server receives it from
  untrusted clients, who may send hostile or huge bodies, and many requests at once.
- Input rules must not drift between the CLI and the API: the strict decoder `jsondoc`
  ([ADR 0009](0009-decode-adapter-json-documents-through-one-strict-decoder.md)) and the inventory
  reader's rules ([ADR 0006](0006-json-inventory-file-format-v1.md)) are the only ones.
- `jsondoc` lives in `internal/adapter/internal/jsondoc`, so Go's internal rule lets only packages under
  `internal/adapter/` import it, and the inventory reader, `inventoryfile`, only reads a file path.
- The dependency rule (ADR 0001) is enforced by depguard and, package by package for every release
  target, by `test/architecture` ([ADR 0012](0012-check-layer-dependencies-by-package-for-every-release-target.md)).
  The driving adapter `internal/cli` already shows the shape: it gets every adapter from the
  composition root through `cli.Deps`.
- The use cases `analysis.Analysis` and `analysis.Comparison`
  ([ADR 0016](0016-compare-targets-through-a-second-reporter-port-and-use-case.md)), the target adapters
  and the reporters are reused unchanged.

## Decision

A second binary, `shift2eu-server`, serves a small JSON API under `/v1/`, through a new driving adapter
`internal/httpapi`. The API takes inventory documents only, never cloud credentials, stores and logs
nothing from a request, and leaves TLS and users to a reverse proxy or the website's backend.

**A separate binary.** `cmd/shift2eu-server` is the API's composition root (milestone 3). `cmd/shift2eu`
never imports `internal/httpapi` (depguard rule `httpapi-imported-only-by-the-server`, which exempts only
`cmd/shift2eu-server`, the package itself and the parity test in `test/apiparity`, test files elsewhere
included), so the CLI's offline test stays unchanged and passes. Both binaries are built from the same module and reuse every
package.

**Three pieces, so strict decoding stays in the adapters.**

1. `inventoryfile.FromBytes(data)` returns the same `*Source` as `New(path)`, over an in-memory
   inventory document: the same `MaxFileSize` cap, the same strict decoding and sentinels, and the
   `sourcetest` contract suite. It takes ownership of `data` (the caller must not change it afterwards)
   rather than copying it, so a request holds one copy of its inventory's bytes. Its errors are in an
   in-memory-only mode that never quotes a value from the document: they name what is wrong by its
   position and field (`resources[3].category`) and why (missing, the wrong JSON type, unknown,
   repeated, not in the taxonomy), never the value. `New(path)`, the CLI's reader, keeps its messages
   exactly as they are, values included: the CLI's user wrote the file and is the only one who reads
   them.
2. A new adapter, `internal/adapter/request/assessmentrequest`, decodes the body of
   `POST /v1/assessments` through `jsondoc`: one object with exactly the keys `inventory` (an object,
   returned as its raw bytes) and `targets` (a non-empty array of strings, in order). Anything else wraps
   `assessmentrequest.ErrDecode`. Its messages never quote a value from the body, by construction rather
   than because of `jsondoc`: `jsondoc`'s own errors place a problem by line and column and name a key,
   but its `Describe`, which a format may use for its own errors, does quote a scalar (`the string "1"`,
   `the number "1.0"`), so `assessmentrequest` never calls it (nor `Document.Version`, which does) and
   names a wrong value by its JSON type only (`an array`, `a string`).
   `MaxBodySize` is the inventory cap plus 64 KiB for the envelope; an adapter imports no other adapter,
   so the constant is written out and a test pins it to `inventoryfile.MaxFileSize + 64<<10`. Its imports
   are a strict allow list (`request-decoders-parse-bytes`): `errors`, `fmt` and `jsondoc`, no `net/http`.
3. `internal/httpapi` is a driving adapter beside `internal/cli`. `httpapi.Deps` takes
   `DecodeRequest` (`assessmentrequest.Decode`), `NewInventorySource` (`inventoryfile.FromBytes`),
   `Targets` (target key to catalog source constructor, as `cli.Deps.Targets`) and the `JSON` and
   `Markdown` reporter pairs (as `cli.Format`). `New(deps, opts...)` returns an `http.Handler`, with
   options for the body cap, the concurrency cap, the bearer token, the CORS origins and the readiness
   check. Its imports are a strict allow list of named standard packages, the application, the domain
   and `internal/version` (`httpapi-gets-adapters-from-cmd`); it never imports an adapter,
   `internal/cli`, `cmd/` or `os`. Because each standard package is named rather than allowed through
   `$gostd`, `test/architecture` holds it package by package for every release target, as it holds the
   offline adapters.

**Routes.**

- `POST /v1/assessments`: `Content-Type: application/json` and `{"inventory": {...}, "targets": [...]}`.
  One target runs `analysis.Analysis`; several, or `["all"]`, run `analysis.Comparison` in sorted key
  order. The target list follows the CLI's rules (unknown, repeated or empty keys, and `all` with another
  key, are refused). The 200 body is what `shift2eu assess --format json` prints for the same inventory and
  targets, byte for byte, and a parity test in `test/apiparity` compares the two.
- `GET /v1/targets`: every target's key, display name and snapshot (`source`, `retrieved`), sorted by
  key, in the shape of the JSON report's `target` object.
- `GET /healthz` (always 200) and `GET /readyz` (200 when the readiness check passes, else 503).
- An unknown path is 404; a known path with another method is 405 with an `Allow` header.

**Report name.** The request carries no file name, so `ReportMeta.InventoryName` is the constant
`inventory.json`. The parity test names its CLI input `inventory.json` so the bytes can match.

**Content negotiation.** With no `Accept`, or `*/*`, `application/*` or `application/json`, the answer is
the JSON report as `application/json`. With `text/markdown` or `text/*` it is the Markdown report,
byte-identical to the CLI's, as `text/markdown; charset=utf-8`. A `q=0` range is excluded, and an entry
whose `q` is not a number in [0, 1] (`NaN` included) is invalid and skipped, as if it were absent. When nothing
the client accepts is available the answer is **406 `not_acceptable`**, a status the PRD's list did not
name: answering JSON to a client that refused it would hide the mismatch.

**One error shape.** Every error, 404 and 405 included, is
`{"error": {"code": "<code>", "message": "<message>"}}` as `application/json`, with the codes
`invalid_request`, `invalid_inventory`, `invalid_targets` (400), `unauthorized` (401), `not_found` (404),
`method_not_allowed` (405), `not_acceptable` (406), `payload_too_large` (413),
`misdirected_request` (421), `unsupported_media_type` (415), `internal` (500), `busy` and `not_ready`
(503). **No value from a request is ever echoed**: a message names what is wrong by position and field
(`targets[2]`, `resources[3].category`) or by a quoted, escaped key name, never a target value, an
inventory value, a header value, the `Host` or the path the client sent. A website may show an error
message to a user, or log it, so a message must be safe to show to anyone and to store; the client
already holds the value it sent. A 500 says `internal error` and nothing more; a panic is recovered into
one.

**Versioning.** The API is versioned in the path. Within `/v1/`, adding a route, an optional request
key that changes nothing for a client that leaves it out, a response key, a response header or an
error code is not breaking: clients ignore what they do not know. Removing or renaming a route or a key,
changing a key's type or meaning, making an optional request key required, or changing a status code
for a request that used to succeed is breaking, and goes in a `/v2/` route beside `/v1/` for a
deprecation period, with an ADR. The report documents inside a 200 follow ADR 0018 on their own: a new
report `schemaVersion` is a breaking change of the report, and so of the route that serves it. The
OpenAPI 3.1 document `api/openapi.json` describes `/v1/`, and a test in `internal/httpapi` holds the
handler's routes, status codes and JSON shapes to it. It refers to the report's JSON Schemas in
`internal/adapter/report/jsonreport/schema/` by relative `$ref` rather than copying them, so they stay
the one source of truth for the report.

**Security model.**

- No credentials. The API reads inventory documents and embedded catalog snapshots only. Live discovery
  would mean a web server holding customers' cloud credentials (CLAUDE.md rules 1 and 3) and needs its
  own PRD and threat model.
- Stateless. A request's body, inventory and report exist only in memory while it is served; nothing
  is written to disk, and `internal/httpapi` takes no logger. The access log (milestone 3) holds the
  method, path, status, duration and size only.
- Media type. A POST body must be `application/json`; a `charset` parameter other than `utf-8`
  (compared ignoring case) is 415, as the body is decoded as UTF-8 only.
- Limits. At most N requests to `POST /v1/assessments` are served at once (default the number of
  CPUs, at least 1) through a non-blocking semaphore; one over the cap gets **503 `busy`** with
  `Retry-After: 1` at once, never queued. 503 rather than 429: the limit is the server's capacity, not
  the client's rate. **The slot is taken before the body is read**, so at most N bodies are in memory at
  once, whatever the clients send. The trade-off: a slow upload holds a slot while it trickles in, and N
  slow clients can keep everyone else at 503. `WithBodyReadTimeout` (default 30 s) bounds how long
  reading one body may hold its slot; past it the request is 400 `invalid_request` with
  `Connection: close`, the server closes the connection and the slot is freed. The handler sets the
  connection's read deadline for the body through `http.ResponseController` and clears it once the
  body is read, which replaces the server's `ReadTimeout`: for `POST /v1/assessments` the body's bound
  is `WithBodyReadTimeout`, measured from when the slot is taken, not `ReadTimeout`. Taking the slot
  after the body would free slots from slow clients but put no bound on memory, which the cap exists
  for. The body is read through `http.MaxBytesReader` up to `MaxBodySize` (413 over it) and decoded
  strictly (400 for an unknown key, a duplicate key or trailing data).
- Memory. One request holds its body (up to `MaxBodySize`), the decoded inventory and the rendered
  report, which is built in memory in full before it is written; a comparison of every target over a
  maximal inventory is the largest case. `FromBytes` takes the inventory's bytes without copying them.
  The peak is therefore about N times one request's, and N's default must be sized against it
  (milestone 3).
- Optional bearer token. When configured, every `/v1/` request without `Authorization: Bearer <token>`
  gets 401 with `WWW-Authenticate: Bearer`, checked before the body is read. The token sent and the
  token configured are compared as SHA-256 digests with `subtle.ConstantTimeCompare`, so neither the
  content nor the length of the token leaks through timing. An explicitly empty token, or one that is
  not a valid bearer token (RFC 6750 `token68`), is a configuration error from `New`, never a server
  that accepts an empty `Authorization`. `/healthz` and `/readyz` stay open for the platform's probes.
- Allowed hosts. `WithAllowedHosts` names the `Host` values the server answers to, compared ignoring case; any other `Host`
  gets **421 `misdirected_request`**, before anything else is checked. `/healthz` and `/readyz` are
  exempt, as probes address the server by its pod or container address. Without it every `Host` is
  served. Each entry is a name, an IPv4 address or a bracketed IPv6 address, with an optional numeric
  port; the option with no entry, an empty entry, an unbracketed IPv6 address (`::1`) and an empty or
  non-numeric port are configuration errors from `New`, never an allow list that serves another host
  or every host. It stops DNS rebinding: a page on an attacker's domain that resolves to `127.0.0.1` reaches a
  server on loopback as same-origin, past CORS, but sends the attacker's `Host`.
- CORS is off unless an allow-list of origins is configured. `WithAllowedOrigins` validates each entry
  and refuses, as a configuration error from `New`, the option with no entry, `null`, `*`, an entry
  with a path, query, fragment, user info or an empty port, and any scheme but `http` and `https`. An
  `Origin` is compared with the list by scheme and host ignoring case, and port exactly, once the
  scheme's default port (443 for `https`, 80 for `http`) is dropped from both the entry and the
  `Origin`: `https://app.example.com:443` and `https://app.example.com` are the same origin. Only an origin in the list gets
  `Access-Control-Allow-Origin` with that origin (never `*`, never credentials),
  `Access-Control-Expose-Headers: Retry-After, WWW-Authenticate`, so a browser client can read the
  back-off and the auth challenge, and a 204 preflight on `/v1/` routes. When CORS is configured, every
  response, to any origin or none, carries `Vary: Origin`, so a shared cache never serves one origin's
  answer to another.
- Every response sets `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
- `GET /v1/targets` loads every target's catalog once, on the first request, and keeps the result; a
  load that fails is not kept, and the next request tries again.
- Route enumeration. A request without the token learns whether a path exists (404 against 401 or
  405), because the route check runs before the token. This is accepted: the routes are public, in the
  OpenAPI document.
- The Markdown report contains text from the inventory (names, tags), escaped for Markdown but not for
  HTML. A website that renders it as HTML must use a sanitising renderer (or render the JSON report
  itself); the API does not make Markdown safe to inject as HTML.
- TLS, users, accounts and per-user rate limits belong to the reverse proxy or the website's backend,
  which the README will describe (milestone 4).

**What milestone 3 adds, in `cmd/shift2eu-server`.** Flags and environment (listen address, token from an
environment variable or a file, never a flag; concurrency; CORS origins; allowed hosts), the server's
read-header, read, write and idle timeouts and maximum header size, graceful shutdown on `SIGTERM` and
`SIGINT`, the access log, the readiness check over every target's catalog, and the refusal to listen on
a non-loopback address without a token unless `--insecure-no-auth` is given. `internal/httpapi` stays a
handler with no listener of its own. Milestone 3 **must** also:

- set `http.Server.ReadHeaderTimeout` and `ReadTimeout`: `WithBodyReadTimeout` bounds a body only once
  the handler runs, and a client that is slow to send its headers holds a connection, not a slot;
- pass a `WithBodyReadTimeout` no greater than its `ReadTimeout`: the body deadline replaces the
  server's for `POST /v1/assessments`, so a greater one would let an assessment body take longer than
  the server allows any other request;
- treat a token source that is configured but empty (an empty variable, an empty file) as a startup
  error, and trim the white space around a token read from a file, which ends with a line feed as
  editors write it;
- set `WithAllowedHosts` (to `localhost` and the loopback addresses with the port) whenever it listens
  on loopback without a token, the case DNS rebinding attacks, and treat an empty resolved host list in
  that case as a startup error, never as "any host";
- size the concurrency default against memory and `GOMEMLIMIT`, not only the number of CPUs, using the
  benchmark `BenchmarkMaximalAllComparison` in `test/apiparity` for one request's peak;
- check end to end that the built `shift2eu` and `shift2eu-server` binaries give the same bytes for the same
  inventory and targets, beside the in-process parity test.

**What milestone 3 decided, in `cmd/shift2eu-server`.** Its imports are a strict allow list,
`server-composition-root`, the sibling of `composition-root`: the module's own packages and the standard
packages it needs, no third-party package, no cloud SDK, no `os/exec` and never `internal/cli`.

- *Flags and environment.* `--listen` (default `127.0.0.1:8080`), `--token-file <path>`,
  `--concurrency <n>` (0, the default, is the computed default below), `--max-connections <n>`
  (default 256; 0 or less is a configuration error), `--cors-origin <origin>` and
  `--allowed-host <host>` (both repeatable) and `--insecure-no-auth`. The token comes from
  `SHIFT2EU_SERVER_TOKEN` or `--token-file`, never a flag; both at once is a configuration error. A
  variable that is set but trims to nothing, an empty or blank file, and a file that cannot be read are
  configuration errors, never "no token"; an unset variable is no token source. White space around a
  token is trimmed, from the file and from the variable. A token shorter than 32 characters once
  trimmed is a configuration error naming its source and the minimum: a shorter one is within reach of
  a client that guesses, and 32 characters is what `openssl rand -hex 16` or a password manager give.
  Every configuration error is one line, `shift2eu-server: ...`, exit code 2, before anything listens; no
  error quotes the token. A failure to open the listener (an address in use) is a runtime failure,
  one line, exit code 1.
- *The loopback rule.* A `--listen` host that is `localhost` or a loopback IP address is loopback;
  anything else (an empty host, `0.0.0.0`, `::`, any other name) is not, and without a token is refused
  unless `--insecure-no-auth` is given. The name `localhost` is resolved by the system, so it is
  trusted only as far as the address it binds: once the listener is open, a listener named as loopback
  whose bound address is not a loopback IP is non-loopback, and without a token or
  `--insecure-no-auth` the server closes it and exits 2. On loopback without a token and without `--allowed-host`, the
  `Host` allow-list is `localhost:<port>` and `<bound address>:<port>`, from the bound listener, so it
  holds the real port when `--listen` asks for port 0. A listener whose bound address is not loopback,
  or that has no port, resolves to no host, which is a startup error, never "any host". `--allowed-host`
  replaces the resolved list, it is not appended to it: the operator who names the hosts chooses
  them, including whether `localhost` is among them, and is validated by `WithAllowedHosts`. The handler is built once before
  the listener opens, so an invalid option never opens a port, and again with the resolved list.
- *Timeouts and limits.* `ReadHeaderTimeout` 10 s, `ReadTimeout` 30 s, `WithBodyReadTimeout` 30 s
  (equal to `ReadTimeout`, as required above), `WriteTimeout` 60 s (from the end of the headers: the
  body read, the assessment, about 0.4 s for the largest on one core, and the write),
  `IdleTimeout` 60 s and `MaxHeaderBytes` 16 KiB (431 above it; the API reads five headers, which
  need far less, and a smaller cap bounds what each open connection may make the server buffer). A
  client that stalls in its headers is closed after 10 s and holds no slot; one that stalls in its body
  frees its slot after 30 s.
- *Connection cap.* The listener holds at most `--max-connections` connections open, 256 by default;
  a connection over the cap is closed at once, never queued, and its slot is free again when one
  closes. The concurrency cap bounds assessments, not connections: without this cap a client that
  opens connections and stalls in their headers holds a file descriptor and a header buffer for 10 s
  each, without limit. 256 stays well below the 1024 open files a default process may hold and is far
  more than the handful of assessments the server runs at once; an operator behind a proxy that pools
  connections needs fewer, one with many direct clients sets more. The cap is global, not per
  client: one client that holds `--max-connections` connections locks every other client out,
  including the `/healthz` and `/readyz` probes, which share the cap, until `ReadHeaderTimeout`
  (10 s) or `IdleTimeout` (60 s) reclaims them. It bounds file descriptors and memory at the cost of
  availability under a flood; per-client limits and probe isolation belong to the proxy or the
  platform.
- *Concurrency default.* `max(1, min(cpus, budget / peak))`, where `cpus` is `runtime.GOMAXPROCS(0)`
  (which follows a container's CPU limit), `peak` is 1 GiB and `budget` is `GOMEMLIMIT` when it is set
  (`debug.SetMemoryLimit(-1)` below `math.MaxInt64`), else 2 GiB. `BenchmarkMaximalAllComparison`
  (`test/apiparity`) measures 1,053,936,082 bytes allocated per maximal `["all"]` request (the largest
  inventory the reader accepts, every target) on linux/arm64, in 0.42 s; the bytes allocated bound
  the request's live memory from above, so 1 GiB, that number rounded up, is a safe peak. It is the
  bytes allocated with no headroom added: the garbage collector reclaims during the request, so live
  memory stays below it, and the rounding is the only margin. With a
  `GOMEMLIMIT` the server never admits more maximal requests than fit in it, but always one. Without
  one it knows nothing of its memory, so it assumes a small container's 2 GiB, room for two maximal
  requests, rather than one maximal request per CPU, which would exceed the memory of most hosts with
  many CPUs; an operator with more memory sets `GOMEMLIMIT` (the image in milestone 4 can) or
  `--concurrency`. The fallback is applied by the caller, not inside `defaultConcurrency`, which reads
  `math.MaxInt64` as no bound: so the function stays non-decreasing in its limit, as its test pins.
  Most requests are far smaller than the maximal one, so the default is conservative by design: a busy
  server answers 503 `busy` with `Retry-After: 1` rather than running out of memory.
- *Readiness.* `/readyz` loads every target's catalog, in key order, and answers 200 once all have
  loaded, 503 until then; a failed load is retried on the next check, and once every catalog has
  loaded it does not load them again, as the embedded snapshots cannot change while the process runs.
  A check that comes while another check is loading answers 503 at once rather than waiting for that
  load: a probe's timeout is short, and 503 is the answer it understands.
- *Access log.* One line per request on stderr, once it is answered:
  `shift2eu-server: access method=POST path="/v1/assessments" status=200 duration=12.3ms size=1234`. The
  path is quoted, so a path holding a line feed cannot forge a line, cut to its first 256 bytes and
  marked with `...` after the quotes when it is longer, so a client cannot write an unbounded run of
  its own into the log, and carries no query string; the
  size is the answer's body in bytes. Nothing else of the request is logged: no header value, no query,
  no body, no inventory value, no report. The `http.Server`'s own error log goes to stderr with the
  same prefix; it reports connection-level problems and no request content.
- *Startup and shutdown.* Once listening, the server prints `shift2eu-server: listening on <host:port>`,
  then `shift2eu-server: concurrency <n>`, the effective concurrency (the flag's value or the computed
  default), so the operator sees what it picked. Two startup warnings, each one line
  `shift2eu-server: warning: ...`, come before them, and the server starts anyway: `serving without
  authentication on <host:port>` (the bound address) for `--insecure-no-auth` on a non-loopback
  listener without a token, and, on systems with permission bits (not Windows), a `--token-file` that
  its group or other users can access (`mode & 0o077`), naming the path and never the token. Refusing
  to start over the file's mode would turn a permission mistake into an outage.
  `SIGTERM` or `SIGINT` starts the graceful shutdown: `http.Server.Shutdown` stops accepting
  connections and waits up to 25 s, below the 30 s a container platform usually allows before
  `SIGKILL`, for the requests in flight, then the process exits 0. Requests that outlast the deadline
  are closed and the process exits 1. A failure to listen (an address in use) or to serve is exit
  code 1.
- *Build.* `make build` builds `bin/shift2eu-server` beside `bin/shift2eu`, with the same link flags
  (`make server` builds it alone). An e2e test builds both binaries and checks that they give the same
  bytes for the same inventory and targets, in JSON and Markdown.

**What milestone 4 adds (packaging).** The `shift2eu-server` image and the README's deployment notes.
*Amended (2026-11-12):* a published OpenAPI document that bundles the report's JSON Schemas is deferred to
[issue #139](https://github.com/timothymamo/shift2eu/issues/139): `api/openapi.json` refers to them by repo-relative path, which a reader outside the repository
cannot follow, until then.

*Amended (2026-11-12, issue #139, done):* `api/openapi.bundled.json` is the self-contained document.
`test/internal/openapibundle` generates it from `api/openapi.json`: every `$ref` to a file becomes
`#/components/schemas/<Name>` (`assessment-v1.schema.json` is `AssessmentReportV1`), the schema is
added there once without its `$id` and `$schema`, and its own pointers (`#`, `#/$defs/X`) are rewritten
to absolute ones into the bundle, so two schemas' `$defs` cannot collide. `test/openapibundle`
regenerates it on every test run and fails unless it equals the committed file (`make openapi` writes
it), so the bundle cannot drift from the document or the schemas, and `api/openapi.json` stays the one
source. Each release publishes it as `shift2eu-server_openapi.json` (GoReleaser `release.extra_files`)
and lists it in `checksums.txt` (`checksum.extra_files`); `check.GitHubRelease` and
`check.GoReleaserArchives` pin both entries, and ci.yml's snapshot dry run
(`.github/scripts/verify-archives.sh`) fails unless `checksums.txt` lists it with the sha256 of the
committed file. It is attested like the archives: GoReleaser copies neither extra file into `dist/`,
so `release.yml`'s `goreleaser` job copies `api/openapi.bundled.json` there as
`shift2eu-server_openapi.json` and hands it on with the archives, and `attest-archives` checks it with
`sha256sum -c checksums.txt` and gives it GitHub build provenance (`actions/attest-build-provenance`,
one subject beside the archives, ADR 0015). `publish` then refuses a draft whose copy differs from
the digest `checksums.txt` records, and `verify-archives` verifies its digest and attestation on the
published release, as for every asset. `check.ReleaseWorkflow` pins the copy, the upload and the
subject, and the dry run makes the same copy and checks the same hand-over. The asset name carries no version, so
`releases/latest/download/shift2eu-server_openapi.json` is a stable URL: the release already scopes
the version (`releases/download/vX.Y.Z/...`). GitHub's "Latest" release is the highest release tag
on `main`, by the one decision the release workflow's `latest` job makes per run and the images'
`latest` tag also follows (ADR 0015, issue #173), so that URL serves the highest release's document, and a patch to an older line does not take it over. The `shift2eu-server_` prefix matches the server's
archives, as the document describes that binary's API.

## Alternatives considered

- **`shift2eu serve`, a subcommand of the CLI.** One binary and one image, but `net/http` in the CLI's
  dependencies ends the offline guarantee its test pins. A second binary from the same module costs
  only a build target and an image.
- **Decode the request in `internal/httpapi` with `encoding/json`.** No new adapter, but a second set of
  JSON rules beside `jsondoc`: case-insensitive keys, the last of a repeated key kept, trailing data
  accepted by a `Decoder`. ADR 0009 exists so there is one.
- **Move `jsondoc` out of `internal/adapter/internal/`** so `internal/httpapi` could import it. That
  would let a driving adapter, and anything else, decode adapter documents, and widen a helper that
  adapter-helpers-do-no-io and the strict rules hold to the adapters on purpose. Passing function values
  from the composition root is how `internal/cli` already gets its adapters.
- **A multipart upload, or the inventory as the whole body with targets in the query.** A file upload
  is what a browser form sends, but it needs a multipart parser and its limits; targets in the query
  string end up in access logs and proxy logs. One JSON object is simplest to decode strictly and to
  describe in OpenAPI.
- **Version by header or media type** (`Accept: application/vnd.shift2eu.v1+json`). Harder to route at a
  proxy, to see in a log and to try with `curl`; the PRD asks for path versioning.
- **429 when busy.** 429 says the client sent too many requests; the cap is shared by all clients and
  is about the server's capacity, so 503 with `Retry-After` is the honest status. The PRD allows either.
- **Fall back to JSON on an unacceptable `Accept`.** Lenient, but a client that asked only for, say,
  `text/html` would get a body it said it cannot read, and an error would hide nothing.
- **`api/openapi.yaml`.** The usual format, but a test would need a YAML parser in the module. JSON is
  read with `encoding/json` and is valid OpenAPI.
- **Copy the report schemas into the OpenAPI document.** Self-contained, but two copies of the report's
  contract that can drift; the relative `$ref` keeps one, and [issue #139](https://github.com/timothymamo/shift2eu/issues/139) bundles them, generated
  and compared by a test, when it publishes the document.
- **Echo the offending value, escaped, in `invalid_inventory`** (the CLI's wording). Most helpful to a
  developer, but the message reaches a website, its users and its logs, and an inventory value can be a
  secret someone pasted into a tag. Naming the position and field is enough to find it in the document
  the client still holds.
- **Take the concurrency slot after the body is read.** A slow upload would hold no slot, but N slots
  would no longer bound memory: every connection could hold a body of up to `MaxBodySize` at once. A
  body read timeout bounds the slow-upload cost instead.
- **Compare the token bytes directly with `subtle.ConstantTimeCompare`.** It returns at once when the
  lengths differ, which leaks the token's length; comparing digests of fixed size does not.

## Consequences

- The CLI stays offline, by test and by depguard; the API is a second composition root with its own
  binary and image (milestone 4).
- The API and the CLI share every rule that matters: the strict decoder, the inventory reader, the
  target-list rules, the use cases and the reporters, and a parity test pins their bytes.
- `inventoryfile` gains `FromBytes`, and a new adapter family, `internal/adapter/request`, holds request
  decoders; each new decoder falls under `request-decoders-parse-bytes`.
- Two depguard rules hold `internal/httpapi` (`httpapi-gets-adapters-from-cmd`,
  `httpapi-imported-only-by-the-server`), and one the request decoders; all three are in the depguard pin
  (`test/architecture/depguardpin_test.go`). A standard package `internal/httpapi` needs is added to its
  allow list and the pin on purpose. A new test that needs the handler goes in `internal/httpapi` or
  `test/apiparity`, or widens `httpapi-imported-only-by-the-server` on purpose. `cmd/shift2eu-server` is
  held by `server-composition-root`, which is pinned too. It constructs adapters, so
  `adapters-are-built-in-cmd` excludes it as it does `cmd/shift2eu`, and the depguard pin holds both
  exclusions. The rule first reported nothing: depguard (v2.2.1) compares an import only with the one
  list entry that sorts nearest before it, as a prefix unless the entry ends in `$`, and the deny entry
  `internal/adapter/internal/` sorted after `internal/adapter/` and before every real adapter
  subpackage, so an import of an adapter was compared with that entry alone. Issue #119 replaced
  `internal/adapter/` with one deny entry per subtree (`report/`, `request/`, `source/`, `target/`)
  beside `internal/adapter/internal/`, and an exact entry (`report$`, `request$`, `source$`,
  `target$`) for the package at each subtree's root, which the `/` entry does not cover. `test/lint`
  runs depguard on a probe that imports an adapter from each subtree in `internal/httpapi`, and both
  an adapter and the root package of each subtree in `internal/foo`, which no strict rule holds
  (`TestAdaptersBuiltInCmdProbe`), and `test/architecture` fails on a deny or allow entry that is a strict prefix of another in the same list
  (`TestDepguardEntriesShadowNothing`) and on a directory under `internal/adapter` the rule does not
  name (`TestAdaptersBuiltInCmdNamesEveryAdapterSubtree`).
- A change to a route, a status code or an error code updates `api/openapi.json` in the same change,
  and a breaking one is a `/v2/` route and an ADR.
- Operators must put the server behind a proxy for TLS and users; a token alone protects it from direct
  access, not from a client that holds the token.
- API errors are less precise than the CLI's: they say where in the inventory the problem is, not what
  value is there. A client that wants the value reads it from its own document.
- A slow client can hold a slot for up to the body read timeout; with N slots, N such clients make the
  API answer 503 `busy` for that long. Per-client limits belong to the proxy.
- Without `GOMEMLIMIT` the server serves at most two assessments at once, however many CPUs it has; an
  operator who wants more sets `GOMEMLIMIT` or `--concurrency`. The per-request peak is a measurement:
  a change that makes a maximal request larger reruns `BenchmarkMaximalAllComparison` and revisits the
  1 GiB in `cmd/shift2eu-server`.
- *Amended (issue #147):* the release ships `shift2eu-server` in archives of its own,
  `shift2eu-server_<version>_<os>_<arch>`, with the same tag and version as `shift2eu`, whose archives
  have never held it in a release ([ADR 0015](0015-publish-the-signed-image-to-ghcr-with-docker-buildx.md),
  Consequences). An operator who runs the binary rather than the image fetches that archive and checks
  it against the release's one `checksums.txt`, exactly as the CLI's (`docs/guide/verify-releases.md`,
  "Verify an archive"). The archive is not signed or attested; the image is, so an operator who needs
  the publisher verified runs the image.
