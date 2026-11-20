# PRD #103: Serve assessments over an HTTP API

> **Retired** ([ADR 0037](../../docs/adr/0037-retire-the-http-api-and-release-the-cli-alone.md), issue
> #393): the HTTP API this PRD built, `shift2eu-server`, is no longer released, and its code is removed.
> The CLI and the site run the same assessments. The JSON report format (ADR 0018) stays. This page is
> kept as history.

**Status:** Complete (milestones 1 to 4 done). The decisions the user accepted with milestones 1 and 2
are marked *Decided* (issue #112).
**Issue:** #103

## Problem

The user wants to run `shift2eu` as the backend of a website at a later stage: a visitor uploads an
inventory, the site calls `shift2eu`, and shows the assessment. Today that is not possible:

- `shift2eu` is a CLI. Its only commands are `assess` and `version`, and nothing in the module serves
  HTTP.
- The only report format is Markdown. `internal/adapter/report/jsonreport` is an empty package, and every
  PRD so far deferred JSON to "its own PRD". A website needs data it can lay out, not Markdown.
- The CLI binary is guaranteed offline. `TestAssessPathHasNoNetworkOrCloudSDK` (`test/e2e`) fails if
  `cmd/shift2eu` depends on `net` or `net/http` at all. That is a promise to CLI users, that the tool never
  opens a connection, and it should not be given up to add a server.

A web backend also brings risks the CLI does not have. An inventory describes someone's infrastructure,
so it is sensitive (CLAUDE.md rule 2). Untrusted clients can send hostile or huge bodies, and many
requests at once.

## Outcome

A second binary, `shift2eu-server`, serves the same assessments over a small, versioned JSON API:
`POST /v1/assessments` takes an inventory document and one or more targets, and returns the assessment
(or the comparison, PRD #89) as JSON. `GET /v1/targets` lists the targets and their snapshot dates.
Health endpoints let a platform run it. It is stateless: it stores nothing, and logs no request content.
It has the same strict input rules as the CLI, plus HTTP limits (body size, timeouts, concurrency). It is
meant to run behind the website's own backend or a reverse proxy, which handles TLS and users. The CLI
`shift2eu` stays offline, and its output does not change. Both share one JSON report format, which the CLI
also offers as `shift2eu assess --format json`.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | None live. Inventories arrive as inventory documents (ADR 0006), in the request body. | Live discovery through the API. It would mean the server accepting cloud credentials, which needs its own PRD and threat model (see *Decisions*). |
| Target providers | OVHcloud, Scaleway and UpCloud, from their embedded snapshots as they are | Catalog changes |
| Output | A versioned JSON report (schema v1) for one assessment and for a comparison, shared by the CLI (`--format json`) and the API. The Markdown report, available from the API too. | HTML. PDF. Changes to the Markdown report. |
| API | `POST /v1/assessments`, `GET /v1/targets`, `GET /healthz`, `GET /readyz`, an OpenAPI 3.1 document in the repository | Users, accounts, sessions, storage of inventories or reports, async jobs, webhooks, a web UI |
| Deployment | A separate `shift2eu-server` binary and container image, built and signed by the release workflow like `shift2eu`. Docs for running it behind a reverse proxy. | A hosted service. TLS termination or user authentication inside the server (the proxy's job, see *Decisions*). |

## Acceptance criteria

All criteria are pinned by tests that run offline under `make test` or `make test-e2e` (the API's tests
use `net/http/httptest` and a server on `127.0.0.1`), unless marked *review*.

**JSON report**

- [x] `shift2eu assess --format json` prints the assessment as JSON. Its document has a `schemaVersion` of
  `1`, and holds the target, the snapshot source and date, the inventory name, the summary counts, and
  every verdict with its fit, best offering, ties, reasons and required changes, as well as blockers.
  This is the same content as the Markdown report, in the same resource order. `--format markdown` is
  the default, and its output is byte-identical to today's.
- [x] With several targets (PRD #89), `--format json` prints a comparison document. It holds the
  per-target assessments and the blockers every target shares.
- [x] Golden files pin both documents. The output is byte-identical across runs. A JSON Schema for each
  document is in the repository, and a test validates the goldens against it.
- [x] Every string comes from the inventory or a catalog, and is JSON-encoded, never concatenated.
  Hostile values (quotes, backslashes, control characters, invalid UTF-8) produce valid JSON. Pinned by
  a hostile golden.

**API**

- [x] `POST /v1/assessments` with `Content-Type: application/json`, and a body holding `inventory` (an
  inventory document) and `targets` (one or more target keys, or `["all"]`), answers `200` with the JSON
  report (one target) or comparison (several). The body is the same document `shift2eu assess --format json`
  prints for the same inventory and targets. Pinned by comparing the two.
- [x] With `Accept: text/markdown`, the same request answers with the Markdown report, byte-identical to
  the CLI's.
- [x] `GET /v1/targets` lists every target with its key, display name, snapshot source URL and retrieval
  date, sorted by key.
- [x] `GET /healthz` answers `200` while the process runs. `GET /readyz` answers `200` once every
  target's catalog has loaded, and `503` before that.
- [x] Errors use one JSON shape (`{"error": {"code": "...", "message": "..."}}`) and the right status:
  - `400` for a malformed body, an inventory the reader refuses, or a bad target list (the same rules
    as the CLI's usage errors);
  - `404` or `405` for an unknown path or method;
  - `413` for a body over the limit;
  - `415` for the wrong content type;
  - `503` with `Retry-After: 1` when the concurrency limit is reached (*Decided*, not `429`: the limit
    is the server's capacity, not the client's rate);
  - `500` for an internal error, with no internal detail in the message;
  - and, beside this list (*Decided*): `406` when nothing in `Accept` is available, and `421` for a
    `Host` outside the allow-list.

  An error message never echoes request content beyond a quoted, escaped key or field name, as the
  CLI's one-line errors do.
- [x] The OpenAPI 3.1 document describes every endpoint, request, response and error. A test checks that
  the handler's routes, status codes and JSON shapes match it.

**Limits and hardening**

- [x] The request body is capped (*Decided*: the inventory reader's size cap plus 64 KiB for the
  envelope), and read through the same strict decoder (`jsondoc`, ADR 0009). An unknown field, a
  duplicate key or trailing data is a `400`. Reading one body is bounded by a 30 s body read timeout
  (*Decided*), from the moment its concurrency slot is taken.
- [x] The server sets read-header, read, write and idle timeouts, and a maximum header size. A slow or
  oversized client gets a timeout or a `431`/`413`, and never blocks a worker for longer than the
  timeouts. Pinned with a slow-client test.
- [x] Concurrent assessments are capped (*Decided*: a flag, `--concurrency`, whose default is sized
  against memory as well as the CPUs, since one maximal `["all"]` request allocates about 1 GB: the
  number of CPUs, but no more maximal requests than fit in `GOMEMLIMIT`, or in 2 GiB without one;
  ADR 0019). A request over the cap is refused at once, not queued without bound.
- [x] Nothing from a request (body, inventory, report, resource IDs) is written to disk or to the logs.
  The access log has the method, path, status, duration and size only. Pinned by a test that sends a
  marked inventory and checks that no log line holds the marker.
- [x] Responses set `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` and a JSON content type.
  CORS is off unless an allow-list of origins is configured, and then it applies only to those origins.
- [x] *Decided:* when a bearer token is configured (from an environment variable or a file, never a
  flag), every `/v1/` request without it gets a `401`. The health endpoints stay open. The server
  refuses to listen on a non-loopback address without a token, unless the operator passes an explicit
  `--insecure-no-auth`.
- [x] On `SIGTERM` or `SIGINT`, the server stops accepting connections and finishes in-flight requests
  within a deadline, then exits `0`.

**Architecture**

- [x] The HTTP API is a driving adapter, `internal/httpapi`, beside `internal/cli`. It may import the
  application and domain layers, and the report adapters through the composition root's wiring, but never
  `internal/cli`. `cmd/shift2eu-server` is its composition root. depguard rules and `test/architecture` hold
  this for every release target, and the depguard pin (#84) includes the new rules.
- [x] `cmd/shift2eu` still depends on no `net` package: `TestAssessPathHasNoNetworkOrCloudSDK` is unchanged
  and passes. A matching test pins that `cmd/shift2eu-server` depends on no cloud SDK and no `os/exec`.
- [x] *Review:* the use cases (`analysis.Analysis`, `analysis.Comparison`), the target adapters and the
  Markdown renderer's output are reused unchanged. The domain changes only by the accepted refactor of
  milestone 1 (issue #112): the tie list and the required-changes list moved into it (`Verdict.Ties`,
  `ChangesToList`), shared by both renderers, with the Markdown output unchanged. An ADR records the
  separate binary, the API's versioning, and the security model.
- [x] `make check` passes. The security-audit skill is run on milestones 2 to 4. *Done:* `make check`
  passes on every milestone; the audit ran on milestones 2, 3 and 4. On milestone 4 it found nothing
  critical, high or medium, and five low or informational findings, all resolved in the same PR.

**Packaging**

- [x] The release workflow builds `shift2eu-server` for the release targets, and publishes its container
  image `ghcr.io/timothymamo/shift2eu-server`. The image is non-root and distroless, signed and attested like
  `shift2eu`'s (ADR 0015), with `ENTRYPOINT ["/shift2eu-server"]`. `check.ReleaseWorkflow` and the CI dry run
  cover it.
- [x] The README documents running the server behind a reverse proxy (TLS, authentication, rate limits),
  the environment it reads, and an example request.

## Milestones

Each milestone ships as one PR, in this order.

1. [x] **JSON report v1.** `internal/adapter/report/jsonreport` renders an assessment and a comparison as
   versioned JSON, with JSON Schemas, goldens and hostile goldens. `shift2eu assess --format json|markdown`.
   An ADR records the schema and its versioning, the counterpart of ADR 0006 for inventories. No HTTP yet.

   *Done:* `jsonreport` renders both documents (`schemaVersion` 1), with JSON Schemas under
   `internal/adapter/report/jsonreport/schema/`, goldens and hostile goldens validated against them,
   and `--format json` e2e goldens. `shift2eu assess --format json|markdown` selects the renderer;
   Markdown stays the default and unchanged. A comparison is validated once, in the domain, for every
   reporter. The format and its version policy are recorded in
   [ADR 0018](../../docs/adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md).

2. [x] **HTTP API adapter.** `internal/httpapi`: the routes, request decoding and limits, error shapes,
   content negotiation, headers, and the OpenAPI document, tested with `httptest` and fakes for the use
   cases. An ADR records the API's design, versioning and security model. depguard and
   `test/architecture` rules for the new package. Depends on 1.

   *Done:* `internal/httpapi` is a driving adapter that returns an `http.Handler` over `httpapi.Deps`,
   which the composition root fills. It serves `POST /v1/assessments`, `GET /v1/targets` (each catalog
   loaded once, a failed load retried), `GET /healthz` and `GET /readyz` (over a check passed with
   `WithReadiness`), with 404 for an unknown path and 405 with `Allow` for another method. Every error
   has one shape and a code, including two statuses the criteria did not list: 406 `not_acceptable`
   when nothing in `Accept` is available, and 421 `misdirected_request` for a `Host` outside the allow
   list. No message echoes a request value. `Accept` chooses the JSON or the Markdown report. The
   handler checks the content type and charset (415), caps concurrent assessments with a non-blocking
   semaphore (503 `busy` with `Retry-After`, the slot taken before the body is read), bounds the body
   read (`WithBodyReadTimeout`) and its size (413), and recovers a panic into a 500. It also handles an
   optional bearer token (401, compared as SHA-256 digests in constant time), a validated CORS
   allow-list with `Vary: Origin`, a validated `Host` allow-list against DNS rebinding, and sets
   `Cache-Control: no-store` and `X-Content-Type-Options: nosniff` on every response. The body is
   decoded by a new adapter, `internal/adapter/request/assessmentrequest`, through `jsondoc`. The
   inventory is read by `inventoryfile.FromBytes`, which passes the `sourcetest` contract suite, and
   whose errors on this path name a position and field, never a value; `New(path)` keeps the CLI's
   messages. The OpenAPI 3.1 document `api/openapi.json` refers to the report's JSON Schemas by `$ref`,
   and a conformance test holds the handler's routes, statuses and shapes to it. `test/apiparity` pins
   that the API and `shift2eu assess` give the same bytes, in JSON and Markdown, and that no inventory
   value is echoed. The depguard rules `httpapi-gets-adapters-from-cmd`,
   `httpapi-imported-only-by-the-server` and `request-decoders-parse-bytes` are in the depguard pin, and
   `test/architecture` holds them for every release target. The design is recorded in
   [ADR 0019](../../docs/adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md). The handler's halves of four criteria are done, and `cmd/shift2eu-server` adds the rest in
   milestone 3: the readiness check over every catalog, the concurrency flag and its default, the token
   from an environment variable or a file with the loopback rule, and the composition root with its
   dependency test. ADR 0019 also binds milestone 3 to: set the server's read-header, read, write and
   idle timeouts; pass a `WithBodyReadTimeout` no greater than `ReadTimeout`; size the concurrency
   default against memory and `GOMEMLIMIT` as well as the CPUs, using `BenchmarkMaximalAllComparison`
   (about 1.05 GB allocated per maximal `["all"]` request); treat an empty token source or an empty
   resolved host list as a startup error; trim the white space around a token read from a file; set
   the `Host` allow-list whenever it listens on loopback without a token; and keep request content out
   of the access log, which holds the method, path, status, duration and size only.
3. [x] **`shift2eu-server` binary.** `cmd/shift2eu-server` wires the adapters. It adds flags and environment
   (listen address, token, concurrency, CORS origins), timeouts, graceful shutdown, the access log, the
   readiness check, and the loopback-without-token rule. e2e tests start the binary on `127.0.0.1` and call
   it. The offline test for `cmd/shift2eu` stays as it is, and a new one pins `cmd/shift2eu-server`'s
   dependencies. Depends on 2.

   *Done:* `cmd/shift2eu-server` is the API's composition root, held by the strict depguard rule
   `server-composition-root` (in the depguard pin, and held by `test/architecture` for every release
   target). It wires the adapters `cmd/shift2eu` wires into `httpapi.New` and adds `--listen`,
   `--token-file` or `SHIFT2EU_SERVER_TOKEN` (never a flag; a configured but empty source, or both at
   once, is a startup error, the token is trimmed and must be at least 32 characters), `--concurrency`,
   `--max-connections` (default 256; a connection over the cap is closed at once), `--cors-origin`,
   `--allowed-host` and `--insecure-no-auth`. It refuses a non-loopback address without a token unless
   `--insecure-no-auth` is given (and then warns that it serves without authentication), treats a
   `localhost` listener as loopback only when the address it bound is loopback, warns about a token
   file other users can access, and on loopback without a token answers only to `localhost` and its
   bound address with the port. It prints the effective concurrency after the listening line. It sets `ReadHeaderTimeout` 10 s, `ReadTimeout` 30 s, a 30 s body read
   timeout, `WriteTimeout` 60 s, `IdleTimeout` 60 s and `MaxHeaderBytes` 16 KiB. The concurrency
   default is `max(1, min(CPUs, budget / 1 GiB))`, the budget being `GOMEMLIMIT` or, without it, 2 GiB,
   from `BenchmarkMaximalAllComparison`'s 1.05 GB per maximal request. `/readyz` answers 200 once every
   catalog has loaded, and 503 at once while another check is loading. The access log writes one line per request to stderr with the method, the
   quoted path (cut to 256 bytes), the status, the duration and the size, and nothing else. `SIGTERM` and `SIGINT` shut it
   down gracefully within 25 s and exit 0. `make build` builds `bin/shift2eu-server` too. White-box tests
   of `run` pin the token, the loopback and `Host` rules, the concurrency, the access log, stalled
   header and body clients and the graceful shutdown; e2e tests run the built binary, compare its bytes
   with `shift2eu assess` in JSON and Markdown, and pin that its dependencies hold no cloud SDK and no
   `os/exec`. The decisions are recorded in [ADR 0019](../../docs/adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md).
4. [x] **Packaging and docs.** GoReleaser builds, the `shift2eu-server` image in the `Dockerfile`, the
   release workflow (publish, sign, attest) and its checks, and the README's deployment section. Moves this
   PRD to `prds/done/` and completes #103. Depends on 3. Security-sensitive: the release workflow changes.

   *Done:* `.goreleaser.yaml` has a second builds entry, `shift2eu-server`, with the CLI's env, flags,
   ldflags and platforms, and each release archive holds both binaries. `releasetarget.Read` accepts a
   build of `./cmd/shift2eu-server` beside `./cmd/shift2eu`, each once, with the same platforms, and
   `test/architecture` holds the server binary's sources as it does the CLI's. The `Dockerfile`'s
   builder compiles the server too, and a stage named `server` ships it on the CLI's distroless
   `nonroot` base with `ENTRYPOINT ["/shift2eu-server"]` and `EXPOSE 8080`; the CLI stays the last
   stage, and `make server-image` builds `--target server`. `test/container` holds the stage and the
   build to the release's. `release.yml` publishes `ghcr.io/timothymamo/shift2eu-server` from jobs of
   its own, `server-image`, `server-sbom` and `server-sign`, with the CLI image's tags, SBOM, cosign
   signature and attestations and the same split permissions; not a matrix, as a matrix job's outputs
   keep one leg's digest only. `check.ReleaseWorkflow` builds both images' jobs from the same step
   lists. CI's `image` job also builds the server image for both platforms, reads its SBOM and
   smoke-tests it (refused off loopback without a token, the health checks, 401 without the token, an
   assessment with it), and `check.DryRunMirrorsRelease` pairs each release image with its CI build.
   The README's "Run the HTTP API" section documents the image and binary, the reverse proxy (with an
   nginx example), every flag and variable, and example requests. ADR 0013 and 0015 record the second
   image as amendments. Left open: ADR 0019 also names a published OpenAPI document that bundles the
   report's JSON Schemas, which `api/openapi.json` still refers to by relative path; it is not part of
   this milestone's criteria and is deferred to [issue #139](https://github.com/timothymamo/shift2eu/issues/139).

## Decisions and open questions

- **Decided: a separate binary, not `shift2eu serve`** ([ADR 0019](../../docs/adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md)
  records it with the API's design, versioning and security model). The CLI's guarantee, that nothing it reaches can
  open a connection, is pinned by a test, and is worth keeping for users who run it on sensitive
  inventories. A `serve` subcommand would put `net/http` in the CLI's dependencies and end that guarantee.
  A second binary in the same module reuses every package. It costs a second build target and image.
- **Decided: the API takes inventory documents only, never cloud credentials.** Recorded in [ADR 0019](../../docs/adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md). Live discovery from the
  API would mean a web server holding customers' cloud credentials, the most sensitive thing this project
  could handle (CLAUDE.md rules 1 and 3). If the website needs it later, it gets its own PRD and threat
  model. Until then the website, or the user, runs discovery and uploads the inventory.
- **Decided: TLS and users belong to the proxy or the website's backend.** Recorded in [ADR 0019](../../docs/adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md). The server is a stateless
  compute service: no accounts, no sessions, no storage. A shared bearer token protects it from anyone
  who reaches it directly. Users, rate limits per user, and TLS are better handled by the site's backend
  or a reverse proxy, which the docs describe. Built-in TLS can follow if a deployment needs it.
- **Decided: synchronous requests.** An assessment over an embedded snapshot takes milliseconds, so a
  request answers directly. Async jobs, queues and webhooks are left out until something makes requests
  slow, such as live discovery.
- **Decided: one JSON report format for the CLI and the API** ([ADR 0018](../../docs/adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md)).
  Users can script the CLI and the website can call the API, and both read the same document. A test
  pins that both give the same bytes. The format carries a `schemaVersion`: adding an optional key or a
  new value of an open set (such as a reason code) keeps it, and readers ignore what they do not know;
  removing, renaming or retyping a key, or any other change that can break a reader, is a new
  `schemaVersion` with an ADR. Unlike the inventory format (ADR 0006), where `shift2eu` is the strict
  reader, here `shift2eu` is the writer.
- **Decided: the API is versioned in the path (`/v1/`).** Recorded in [ADR 0019](../../docs/adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md). A breaking change is a `/v2/` route beside
  `/v1/` for a deprecation period. Additive fields are not breaking.
- **Decided: no request content is ever stored or logged.** Inventories and reports are customer data
  (CLAUDE.md rule 2). The server keeps them only in memory for the length of the request.
- **Open: a readiness score.** A website will want one number per target, but PRD #2's open question
  still applies: no score until sizing and workload grouping exist. The API returns the same facts as the
  CLI and nothing more.
- **Open: hosting.** Where the website and server run (and so which proxy, TLS and auth setup) is not
  decided. The PRD keeps the server deployable anywhere a container runs.
