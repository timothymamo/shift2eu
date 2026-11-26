# 0045. Front the site with a Serverless Container running a pinned Caddy that writes the redirects and headers

- Status: Accepted
- Date: 2026-11-25

Part of [PRD #297](../../prds/done/297-web-assessment-site.md) milestone 4 (Hosting decisions 1 to 3, as
amended on 2026-11-24). It amends the front of
[ADR 0042](0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md)
(its amendment *A Serverless Container in front of Edge Services* says what changes there) and leaves the
bucket, the deploy, the key and the records as that ADR decides them. How the new resources are created
is [ADR 0039](0039-create-the-sites-scaleway-hosting-with-opentofu.md)'s, amended by the unit that
writes them in `infra/site`. Passages marked *for the infra unit* are left to that unit, which amends
this ADR with what it chose.

## Context

- **Edge Services cannot meet the mandatory go/no-go.** Scaleway support (ticket #1639626, 2026-11-24)
  answered that Edge Services always answers plain HTTP with a **302**, cannot turn plain HTTP off and
  cannot set response headers. `http://www.shift2eu.com` would then get neither the 301 or 308 nor the
  refusal that Hosting decision 3 allows, and `Strict-Transport-Security` could not be sent at all, so
  milestone 4's HSTS go/no-go could only be waived.
- **The apex needs an HTTPS front of its own.** Edge Services serves no apex (Hosting decision 1), and
  Namecheap's URL redirect serves no certificate of its own on the source name (the dispatcher's reading
  of Namecheap's articles 384, 385 and 9604, 2026-11-24), so it can serve neither
  `https://shift2eu.com` nor HSTS.
- **What must not change.** The private bucket behind Edge Services' cache, `ci/sitedeploy`, the deploy
  key's scope and its canary step (R7), the attested bundle and the live checks were built and reviewed
  in milestone 3. A front that keeps them keeps that work.
- **The maintainer tested the front before deciding** (C3, mac-mini-home, 2026-11-24, all pass; the
  runbook's settings: `scw` 2.67, the official `docker.io/library/caddy:2.8.4` image by tag (C3's
  release; the infra unit pins the release current when it writes the container, by digest) with the
  container's command and arguments set, `port=8080`, `privacy=public`, `https-connections-only=false`,
  `min-scale=0`, `max-scale=1`, `mvcpu-limit=140`, `memory-limit-bytes=0.25GB`, region `nl-ams`):
  - the container's endpoint is `<namespace>-<name>.functions.fnc.nl-ams.scw.cloud`;
  - the container's own **308** reaches the client unchanged over plain HTTP and over HTTPS, so the
    platform does not redirect when plain HTTP is enabled; `Strict-Transport-Security`,
    `X-Content-Type-Options` and `Content-Security-Policy` reach it unchanged; the platform adds
    `server: envoy` and `x-envoy-upstream-service-time`;
  - a container reverse-proxying the Edge Services pipeline endpoint (`caddy reverse-proxy --to
    https://<pipeline endpoint> --change-host-header`) answers with the same status, and serves a probe
    object identically (404 bodies differ only in their per-request `RequestId` and `HostId`);
  - the apex `shift2eu.com` behind a Namecheap **ALIAS** record resolves to the container's addresses,
    stays **DNSSEC-signed and validated** (an RRSIG, and the `ad` flag from 1.1.1.1), reaches *ready* as
    a custom domain, gets a **Let's Encrypt** certificate (the existing CAA record allowing only
    `letsencrypt.org` unchanged), and answers 308 over HTTPS and plain HTTP. The zone's negative TTL is
    3601 s: a lookup made before the ALIAS existed can stay cached for up to an hour.

Scaleway's documentation agrees where it speaks: plain HTTP option `enabled` is "Serve both HTTP and
HTTPS traffic", `redirected` "Responds to HTTP request with a 301 redirect" on the same host
([scaleway-sdk-go, container v1beta1](https://github.com/scaleway/scaleway-sdk-go/blob/master/api/container/v1beta1/container_sdk.go));
`X-Forwarded-Proto` is "the protocol (HTTP or HTTPS) used by the client to connect to the container"
([request headers](https://www.scaleway.com/en/docs/serverless-containers/reference-content/request-headers/));
a root domain needs CNAME flattening or "an ALIAS on your root domain", and "during that step [the
certificate's issuance], the custom domain might be available in plain HTTP"
([custom domain](https://www.scaleway.com/en/docs/serverless-containers/how-to/add-a-custom-domain-to-a-container/)).
The certificate authority is Let's Encrypt: "Scaleway uses Let's Encrypt to generate and automatically
renew certificates on your Custom Domains"
([Serverless Containers FAQ](https://www.scaleway.com/en/docs/serverless-containers/faq/), source
`pages/serverless-containers/faq.mdx` in `scaleway/docs-content`, read 2026-11-24), and C3 saw it for
the apex under the existing CAA record. `www`'s certificate on the container is expected from the same
issuer and is checked at the first deploy (R8).

## Decision

**One public Scaleway Serverless Container, running the official Caddy image pinned by digest with a
configuration from the repository, serves both `www.shift2eu.com` and `shift2eu.com`, with plain HTTP
`enabled`; Caddy writes every redirect and every security header itself and reverse-proxies the site
from the Edge Services pipeline endpoint, which stays the only reader of the private bucket.**

### The container

- **One container, two custom domains:** `www.shift2eu.com` (a Namecheap CNAME to the container's
  endpoint) and `shift2eu.com` (a Namecheap ALIAS at `@` to the same endpoint), each with the
  certificate Scaleway issues for a custom domain (Let's Encrypt: the FAQ, and C3 for the apex).
  DNSSEC stays on, and the CAA record stays `0 issue "letsencrypt.org"`, `0 issuewild ";"` (Hosting
  decision 10).
- **Plain HTTP `enabled`, not `redirected`.** `redirected` answers 301 on the same host, so
  `http://shift2eu.com` would take two hops to `https://www.shift2eu.com`, and the redirect would be the
  platform's rather than the configuration's. With `enabled`, plain HTTP reaches Caddy, which redirects.
- **The cost pins** (enforced by the infra unit in `infra/site`, as the configuration's values):
  `max_scale` **1**, **140 mvCPU**, **0.25 GB** of memory. Prices verified by the maintainer on
  2026-11-24 (Scaleway's pricing page, Amsterdam): EUR 0.00001 per vCPU-second after 200,000 free, and
  EUR 0.000002 per GB-second after 400,000 free, per account and month. The worst case, one instance
  running a whole 30-day month (2,592,000 s), is about **EUR 2.12 a month**; with no instance warm most
  of the time it stays in the free tier. `min_scale` (0, with a cold start on the first visit after an
  idle period, or 1, always warm) is *for the infra unit*: both stay within the worst case. A cold start
  also meets the live check's request timeout (`DefaultRequestTimeout`, 30 s, ADR 0043) and the first
  deploy's probes: with 0, the infra unit checks that the first request after an idle period fits that
  timeout, or R8 warms the container (one request, answer discarded) before the checks run. One instance
  also bounds capacity: a burst beyond it waits or fails, which this small static site accepts.
- **Who can change it:** only the maintainer, through `infra/site` and `dev/tofu.sh` on the admin
  machine (ADR 0039). The deploy key holds no Serverless Containers or Container Registry permission
  (its IAM rule grants the two Object Storage permission sets only, ADR 0042), and no release changes the
  container: a release changes the bucket's objects, never the front.

### What the server answers

Every rule is keyed on the request's `Host` and `X-Forwarded-Proto` (the TLS connection ends at the
platform; Caddy listens on plain `:8080`). The global options set `auto_https off` and `admin off` (no
admin endpoint in the container), both pinned by the configuration test.

- **The scheme is the joined value of the raw `X-Forwarded-Proto` request header, compared exactly
  with `https`:** `@https expression {http.request.header.X-Forwarded-Proto} == "https"`. Caddy's
  placeholder joins repeated fields with `,` (by Caddy's source and documentation, to be pinned by the
  infra unit's behaviour test), so the rule fails closed for every value but one field of exactly
  `https`: a missing header, two fields in either order (`https` + `http`, `http` + `https`), two
  `https` fields and `https, https` in one field all get the 308. Not Caddy's `header
  X-Forwarded-Proto https` matcher: it succeeds when *any* field equals `https`, so `https` + `http`
  would be proxied with a 200 over plain HTTP. Not the `{scheme}` placeholder or the `protocol`
  matcher either, which on plain `:8080` always see `http` and would answer every request with a 308,
  a loop for `www`; no `trusted_proxies`.
- **`www` is an exact comparison on the raw host:** `expression {http.request.hostport} ==
  "www.shift2eu.com"`, the one expression the rule and the static test name. Not Caddy's `host`
  matcher: it drops the port and ignores case, so `host www.shift2eu.com` would proxy
  `www.shift2eu.com:443` and `WWW.shift2eu.com`, contrary to the table. Not `header Host` either: its
  value is a pattern (`*` a wildcard), not an exact comparison.
- **The redirect target is built from the path and the query, never from `{uri}`:**
  `https://www.shift2eu.com` + the escaped path, plus `?{http.request.uri.query}` only when the query
  is not empty. Caddy's `{uri}` is the raw request target, so an absolute-form target (`GET
  http://evil.example/ HTTP/1.1`) would give `https://www.shift2eu.comhttp://evil.example/` and
  `OPTIONS *` would give `https://www.shift2eu.com*`. A path that does not begin with `/` is
  redirected to `https://www.shift2eu.com/`. "The URI" below means that path and query.
- **The escaped path is the preferred choice, not the decoded one.** The decoded
  `{http.request.uri.path}` turns `%3F` and `%23` into a literal `?` and `#` in `Location`, which a
  browser then reads as a query or a fragment, and `%20` or `%0A` into a raw space or line break in a
  header value. The escaped form (Go's `URL.EscapedPath()`) keeps them encoded, so the target's path,
  decoded, is the request's path. The infra unit confirms the pinned release's placeholder for it and
  records its name here; only if the release has none is the decoded path taken, recorded here with
  the behaviour test's answers for those cases.
- **`OPTIONS *` may never reach the routes.** Go's `net/http` server, which Caddy runs on, answers
  `OPTIONS *` itself (200, `Content-Length: 0`, no body) unless the server sets
  `DisableGeneralOptionsHandler`, which Caddy is not known to do. Its outcome is *for the infra unit*,
  recorded here: either the 308 to `https://www.shift2eu.com/` above or Go's empty-bodied 200. If the
  200, it is the one accepted exception to "never a 200 over plain HTTP": it carries no content, no
  page and no header of the site, and no browser sends it. The live probe records whether the
  platform's Envoy forwards `OPTIONS *` to the container at all.

| Request | Answer |
|---|---|
| Joined `X-Forwarded-Proto` anything but exactly `https`, any host (`http://www.shift2eu.com`, `http://shift2eu.com`, the container's endpoint over plain HTTP) | **308** to `https://www.shift2eu.com` + the URI, one hop |
| HTTPS, `Host: shift2eu.com` | **308** to `https://www.shift2eu.com` + the URI |
| HTTPS, `Host` exactly `www.shift2eu.com` | the site: reverse proxy to the Edge Services pipeline endpoint, with the headers below |
| HTTPS, any other host (the container's own endpoint `<namespace>-<name>.functions.fnc.<region>.scw.cloud`, and a `Host` with a port suffix, in another case or with a trailing dot, such as `www.shift2eu.com:443`, `WWW.shift2eu.com` or `www.shift2eu.com.`) | **308** to `https://www.shift2eu.com` + the URI; the exact host comparison sends those three variants to this row, and the configuration and behaviour tests pin them here, never the proxy |

So neither name, nor the container's endpoint, ever answers 200 over plain HTTP, the transient plain
HTTP during a certificate's issuance included (it reaches the same rule), and only
`https://www.shift2eu.com` serves the site through the container. That is a property of this rule and of
its tests below, not a result of C3: C3's redirect container answered 308 unconditionally, and showed
only that the platform passes the container's 308 and headers unchanged.

**The preload chain** (Hosting decision 2) is not configured while preload is not pursued. If milestone
4 records `preload: true` in `site/records/hsts.json`, the configuration changes in the same change:
`http://shift2eu.com` goes first to `https://shift2eu.com` (308, same host), whose HTTPS answer carries
`Strict-Transport-Security: max-age=31536000; includeSubDomains; preload` with its 308 to
`https://www.shift2eu.com`, and `www`'s header gains `preload`.

**Whether the platform overwrites a client's own `X-Forwarded-Proto`** is undocumented (the
documentation says only that `X-Real-IP` is overwritten). If it did not, a client sending
`X-Forwarded-Proto: https` over plain HTTP would get the site with a 200 on plain HTTP. Only that client
could: a network attacker cannot make a victim's browser send the header, and could serve its own page
anyway. If the platform appends its value to the client's rather than overwriting it (two fields in
either order, or `https, http` in one), the rule above fails closed: the joined value is no longer
exactly `https`, so that request gets the 308, and only the spoofing client, on `www` over HTTPS, would
loop. That is intended. The infra unit's live probe settles what the platform does: it records the
header's value as the container receives it on `www`, on the apex and on the container's endpoint, over
plain HTTP and over HTTPS, and sends over plain HTTP a request carrying `X-Forwarded-Proto: https`, the
header as two fields in both orders (`https` + `http`, `http` + `https`), two `https` fields, and
`https, https` in one field, each of which must still get the 308. The infra unit records the answers
here; a 200 is the maintainer's to accept or not, on the record, since it changes no browser's visit.

**The host as the container receives it** is undocumented too. Before the rule is taken, the same live
probe records `{http.request.hostport}` as the container receives it on `www`, on the apex and on the
container's endpoint, over plain HTTP and over HTTPS. If the platform adds a port, changes the case or
moves the original host to `X-Forwarded-Host`, the exact comparison with `www.shift2eu.com` never
matches, and `www` answers with a 308 to itself, a loop: it fails closed but takes the site down. The
infra unit records the values here, and the rule follows what the probe shows before it goes live.

### The headers

Caddy sets these on every HTTPS response of `www.shift2eu.com` (replacing any copy from upstream),
the site's files and every other path alike:

| Header | Value |
|---|---|
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` (plus `; preload` only once preload is pursued, above) |
| `Content-Security-Policy` | exactly the `<meta>` policy every page carries (ADR 0036), plus `frame-ancestors 'none'` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `no-referrer`, as the pages' `<meta name="referrer">` |
| `Permissions-Policy` | every feature the site does not use turned off (`()`); the exact list is *for the infra unit*, pinned by the configuration test |

The `<meta>` policy and the framing guard in the page script stay: they travel with the bundle to every
other origin (Hosting decision 11).

**The apex's HTTPS 308 carries `Strict-Transport-Security: max-age=31536000`** (decided by the maintainer
on 2026-11-25, PRD #297 milestone 4, issue #601), exactly once, on every HTTPS answer to a host other than
`www.shift2eu.com` (the apex, the container's own endpoint, `www` with a port, a trailing dot or
capitals), none of which is proxied:

- **Why:** it protects a returning visitor who types `shift2eu.com`: a browser that has once been to
  `https://shift2eu.com` goes there over HTTPS from then on, and its 308 takes it to `www` over HTTPS.
- **No `includeSubDomains`:** the apex's HSTS with `includeSubDomains` would bind every present and
  future subdomain of `shift2eu.com` to HTTPS from one redirect, which is preload's commitment without
  preload; `www` sends its own `includeSubDomains` for itself. **No `preload`:** preload is not pursued
  (`site/records/hsts.json`, `preload: false`, ADR 0042).
- **Never on plain HTTP:** the plain-HTTP 308, for any name, carries no HSTS. A browser ignores the
  header over plain HTTP (RFC 6797, section 8.1), and one sent there is one a visitor's network could
  have added.

The live check still only records the apex's header while preload is not pursued (ADR 0043, its
amendment of 2026-11-25): the value is pinned by `test/sitefront` (the static test under `make test`,
the behaviour test under `make test-sitefront`) and read live by `docs/site-runbook.md` R8 after the
maintainer applies `infra/site`. The release ships the privacy page that states this header but not the
Caddyfile, so the maintainer applies `infra/site` and passes R8's apex line before merging the first
release that carries that page (`docs/site-runbook.md` R10). Pursuing preload changes the apex's header (to the same minimum with
`includeSubDomains` and `preload`) together with the preload record, in one change (above). This
replaces the earlier wording that left the apex's header to the maintainer at milestone 4; it changes
neither the PRD's HSTS decision nor the record. No other header is decided
here; `Clear-Site-Data` and `Cross-Origin-Opener-Policy` are now possible and are proposed follow-ups.

Guidance for the infra unit (the behaviour tests below decide): Caddy's response `header` directive
applies immediately, and `reverse_proxy` then adds the upstream's own `Content-Security-Policy`,
`Strict-Transport-Security` and `Permissions-Policy` as duplicates; with `defer` instead, the headers
can be missing from Caddy's own 502. The likely configuration is therefore the immediate `header` plus
`header_down -Content-Security-Policy`, `-Strict-Transport-Security` and `-Permissions-Policy` in
`reverse_proxy`.

### The upstream

Caddy reverse-proxies `www`'s HTTPS requests to `https://<pipeline id>.svc.edge.scw.cloud`, verifying
its certificate, with the endpoint as SNI and `Host` (C3's `--change-host-header`). Edge Services' cache
and the private bucket stay as ADR 0042 has them; `ci/sitedeploy`, the key's scope and R7 do not change.
The pipeline's own `www.shift2eu.com` custom domain goes; the infra unit removes it (and its TLS stage)
in `infra/site`. The response passes through unchanged but for the headers above: status (a 3xx where a
manifest file is expected still fails the live check), body, `Content-Type`, `Content-Encoding`,
`Cache-Control` and `Age`. The configuration has no `encode` directive and no cache, so the module
script is still served with the bytes and the `Content-Encoding: gzip` the deploy uploaded (Hosting
decision 4, ADR 0043's budgets).

The configuration removes from the request sent upstream every header that carries the visitor's
address or a client's forwarding headers, `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host`,
`Forwarded`, `X-Real-IP` (set by the platform) and `X-Envoy-*`, and also `Cookie` and `Authorization`,
since Edge Services needs nothing of the visitor; Caddy's own `X-Forwarded-*` are removed too
(`header_up -<name>`). `Forwarded` is removed as a client's header: Caddy does not set it. Caddy
applies the `header_up` deletions after it adds its own forwarding headers (by Caddy's source and
documentation), but the behaviour test, not that reading, is what proves they are gone. The exact
list, and any further platform header such as `x-request-id`, is the infra unit's, and the behaviour
test's local upstream asserts that it receives none of them. The configuration turns no access log on
(no `log` directive in a site block), and the front writes nothing about a visitor to its output: see
*Amendment: the front writes nothing about a visitor to its output* below. What the platform itself logs
is Scaleway's, as for Edge Services; the privacy page already names Scaleway, and says that the
maintainer reads the hosting provider's aggregate request counts (no IP address, no user agent, no
cookie, kept 31 days; ADR 0039, *Amendment: log retention*).

### The image and its configuration

- **The image is the official `caddy` image, pinned by digest**
  (`docker.io/library/caddy:<version>@sha256:<digest>`), with a
  `# renovate: datasource=docker depName=caddy` comment on the line above it (CLAUDE.md rule 7), so
  that Renovate, once its manager covers the file, moves it. Today `.github/renovate.json5`'s custom
  manager matches no file in `infra/site`, and its `matchString` expects `key: "value"`, not HCL's
  `image = "..."`: the infra unit extends it (below).
- **Digest pull is a go/no-go for the infra unit.** C3 ran the official image straight from Docker Hub,
  by tag. The infra unit confirms that the platform runs the image by its digest: the image reference
  the platform records keeps `@sha256:`, and a container given a wrong digest fails to start. If not,
  the pinned digest is copied into a private Container Registry namespace of the site's project and
  pulled from there by digest, recorded in this ADR before it is taken. The same mirror is the answer if
  Docker Hub's pull limits or an outage stop a cold start. The mirror has costs, recorded with it:
  Renovate moves the Docker Hub digest while the reference points into the private namespace, so each
  Renovate PR needs a copy step before the apply; the copy must preserve the digest (`skopeo copy
  --preserve-digests`, or `crane copy` of the whole index); and the push needs a Container Registry
  write key, which this ADR otherwise avoids.
- **The configuration is a file in the repository**, beside the container in `infra/site`, changed only
  by a reviewed pull request and applied by the maintainer. How it reaches the container is *for the
  infra unit*, preferring the upstream image byte for byte with the configuration given at start (C3
  set the command and arguments; an environment variable or a start command that writes the file are
  the candidates), so that no image is built and no registry key exists. Only if that cannot hold the
  configuration does the maintainer build an image `FROM` the pinned digest with the file copied in and
  push it to a Container Registry namespace in the site's project; that path is recorded here before it
  is taken.
- **The configuration is tested, offline and live.** A static test under `make check` pins what the
  configuration says: the four rules of the table, with the scheme read by the joined-value
  expression on `X-Forwarded-Proto` and `www` by the exact `{http.request.hostport}` comparison (no
  `header X-Forwarded-Proto` or `host` matcher), the `Location` built from the path and the query
  (no `{uri}`), and the `Host` variants pinned to the "any other host" 308; `auto_https off` and
  `admin off`; the HSTS header against `site/records/hsts.json` (with `met`, the header with the
  record's value; with `waived`, no header; `preload` exactly when it is pursued); and the
  `Content-Security-Policy` header against the built pages' `<meta>` policy plus
  `frame-ancestors 'none'`, so a change of the policy in `site/` fails until the configuration
  follows. A behaviour test runs the pinned image on the configuration in a
  local container, outside `make test` like the browser smoke test (ADR 0036), against a local
  upstream, and drives:
  - every row of the table with `Host` and `X-Forwarded-Proto`, the headers, and a gzip-encoded object
    passing unchanged;
  - `X-Forwarded-Proto` missing, as two fields in both orders (`https` + `http`, `http` + `https`), as
    two `https` fields, and as `https, https` in one field: each gets the 308, never the proxy (this
    pins Caddy's joining of repeated fields);
  - an upstream that sends its own `Content-Security-Policy`, `Strict-Transport-Security` and
    `Permissions-Policy` with other values (the client sees only Caddy's), an upstream 404, and no
    upstream at all (Caddy's own 502 still carries every header);
  - redirects of `//evil.example/x`, `/%2F%2Fevil.example`, `/a%3Fb`, `/a%23b`, `/a%0Ab`, a path
    with a query string, an absolute-form request target (`GET http://evil.example/ HTTP/1.1`),
    `OPTIONS *` (its answer recorded, above), and a `Host` with a port, a trailing dot or in upper
    case: every `Location` parses as a URL, with scheme `https` and host exactly `www.shift2eu.com`
    (not a prefix test), its path, decoded, equal to the request's decoded path, and an unknown host
    gets the 308, never the proxy;
  - the upstream request: the local upstream asserts it receives none of the removed headers (*The
    upstream*).

  The live checks (ADR 0043) hold the result in production.
  Where each test lives and how it is run is *for the infra unit*.

### Why Caddy, not nginx

Both can do it. Caddy: C3 tested it on the platform; its `redir` takes a 308 directly and `header`
sets the response headers in a few lines; its `reverse_proxy` to an `https://` upstream verifies the
certificate and sends the upstream's name as SNI without further settings; it is one static Go binary
in a memory-safe language, fitting 140 mvCPU and 0.25 GB. nginx would need a `map` on
`$http_x_forwarded_proto`, `proxy_ssl_server_name on` and `proxy_ssl_verify on` with a trust store for
the same upstream (easy to leave out, and nginx does not verify an upstream's certificate by default),
and `add_header ... always` so that the headers reach error answers too. No strong reason for nginx was
found.

## Alternatives considered

- **Edge Services as is, with the 302 accepted and HSTS waived.** Every visit starting at plain HTTP,
  returning visits included, would cross the network in clear text before a temporary redirect;
  preload would become impossible; the apex would still need a front. It breaks a mandatory go/no-go
  that is "never waived".
- **The platform's `redirected` HTTP option.** A 301 on the same host: two hops from
  `http://shift2eu.com`, and the redirect outside the tested configuration.
- **Namecheap's URL redirect for the apex.** No certificate on `https://shift2eu.com` (by Namecheap's
  articles 384, 385 and 9604, not tested), so no HSTS either, and nothing for `http://www`.
- **Proxying the bucket website endpoint instead of the pipeline** (`public_read = true`). Every bucket
  endpoint would then serve the site, and Edge Services' cache would be dropped; ADR 0042's residuals
  would widen for nothing.
- **Serving the files from the container's image.** It would replace the attested bundle's deploy, the
  bucket-scoped key and R7 with an image build per release.
- **Bunny.net, an UpCloud or OVHcloud load balancer, a Scaleway Load Balancer or an Instance with
  Caddy** (the research note of 2026-11-24, ranked). Bunny and UpCloud add a third party to the privacy
  page and a second provider to `infra/`; Scaleway's Load Balancer sets no response headers and costs
  about EUR 17 a month; an Instance makes the project patch and run a server.
- **A section of ADR 0042 instead of this ADR.** The front brings a supply chain of its own (an image
  pinned by digest and moved by Renovate), a configuration with its own tests, and a lifecycle apart
  from the releases; ADR 0042 records the bucket, the deploy and the key, and is already long. It is
  amended only where the front changes what it says.

## Consequences

- The mandatory go/no-go and the HSTS go/no-go can both be met: `http://www.shift2eu.com`,
  `http://shift2eu.com` and `https://shift2eu.com` answer 308 to `https://www.shift2eu.com` in one hop,
  and `www` sends HSTS and the four other headers. `frame-ancestors 'none'` works as a header at last.
- The header CSP binds every response the front serves for `www`, so a page or a script a leaked deploy
  key uploads at any path is held there to `connect-src 'none'` and `worker-src 'none'` as well, whatever
  it carries itself (two policies both apply). It binds `www` alone: the pipeline's default endpoint
  serves the same objects without it (the residual in the amendment below). Navigation stays outside the
  CSP. ADR 0042's residuals say so once the
  header is live (milestone 4). Follow-ups for the unit that changes `site/`, on the privacy page
  (`site/privacy.html`):
  - the sentences that the site "cannot send" `Clear-Site-Data` (line 55) or
    `Cross-Origin-Opener-Policy` (line 66) become "does not send";
  - line 38, the downgrade risk: the apex redirect's provider is now Scaleway's container, which sends no
    HSTS while preload is not pursued (since 2026-11-25, issue #601: its plain-HTTP 308 sends none, its
    HTTPS 308 sends `max-age=31536000`, *The headers*);
  - line 67, the framing guard: `www` now sends `frame-ancestors 'none'` as a header; the script's guard
    stays for the other origins.
- Scaleway now also serves the apex: the apex redirect's provider (Hosting decision 3) is Scaleway, which
  the privacy page already names; no third party is added.
- **For the infra unit:** a Serverless Containers namespace, the container (the pins above), two custom
  domains, the configuration and its tests in `infra/site` and `make check`; the pipeline's `www` custom
  domain and its TLS stage removed; the digest pull confirmed (or the registry mirror recorded, above);
  `.github/renovate.json5` extended so Renovate moves the Caddy pin (a file pattern for the file that
  holds it and a `matchString` for the pin's own syntax), proven by a configuration test or a Renovate
  dry run; the provider stays `scaleway/scaleway` alone, so `dev/infra-rules.sh` keeps its one
  provider; ADR 0039 amended; the runbook's R4, R5 and R8 rewritten (the `www` CNAME and the apex ALIAS to
  the container's endpoint, the origin and redirect checks).
- **For the live-check unit:** it confirms that `ci/sitecheck` tolerates exactly the platform's
  `server: envoy` and `x-envoy-upstream-service-time` response headers, and pins that with a test.
  `ci/sitecheck/internal/livecheck` has no response-header allow list today (it judges only the headers
  it names); if one is ever added, it allows exactly those two, not an `x-envoy-*` prefix.
  `-plain-http-www` stays at `redirect`, so the
  refuse-or-close mode (ADR 0043) is no longer needed by this hosting.
- New upkeep: Renovate's digest PRs for Caddy, each reviewed and applied by the maintainer; a Caddy CVE
  means such a PR and an apply. A container's cold start can delay the first visit after an idle period
  if `min_scale` is 0.
- Recovery gains a step: if the container or its configuration is broken or changed, the maintainer
  re-applies `infra/site` at `main` (the pinned digest and the committed configuration), then runs the
  redirect and header checks (ADR 0042, *Recovery*, as amended).

## Amendment: what the infra unit chose (PRD #297 milestone 4)

2026-11-24. The passages *for the infra unit* above, settled from Caddy v2.11.9's source,
terraform-provider-scaleway v2.93.0 (the lock file's version) and a local run of the pinned image on
`infra/site/Caddyfile`. How the resources are created is ADR 0039's amendment *The container front, and no
TLS stage*.

- **The image** is `docker.io/library/caddy:2.11.9-alpine@sha256:003a97808e781aa9f7e75cc2f43d34d461099031faf5e20faefb9f61126836f0`,
  the digest of the `-alpine` tag's multi-arch index: `caddy:2.11.9` holds the same linux/amd64 image,
  but its index also lists Windows images, which this Linux platform never runs. The image has busybox's
  `/bin/sh`. The pin is one `locals` value in `infra/site/container.tf`, under
  `# renovate: datasource=docker depName=caddy`; a custom manager of `.github/renovate.json5` reads that
  HCL line (Docker versioning, so `2.11.9-alpine` moves to the next `-alpine` tag, its digest with it),
  pinned by `test/lint`'s `TestCaddyImagePinIsFoundOnce`.
- **The configuration** reaches the upstream image at start, with no image build and no registry key:
  `command = ["/bin/sh", "-c"]`, and `args` whose first writes its `$1`, the Caddyfile's bytes
  (`file("${path.module}/Caddyfile")`), to `/tmp/Caddyfile` with `printf '%s'` and execs
  `caddy run --adapter caddyfile --config /tmp/Caddyfile` (the provider caps an environment variable at
  1000 characters, too few for the file, about 5.2 KiB, which travels as `args[2]`). The upstream's name is the environment variable `UPSTREAM`,
  the DNS stage's `default_fqdn`, read as `{$UPSTREAM}` when Caddy parses the file, so the file names no
  ID. The registry mirror is not taken unless the digest pull fails (below).
- **The upstream's precondition** (issue #566). Scaleway sets the DNS stage's `default_fqdn` a while
  after creating the stage, so on a first setup it may still be empty when the container is created:
  `UPSTREAM` would be `""` and Caddy refuses `reverse_proxy https://` at start. The container's
  `lifecycle` holds a `precondition` that `default_fqdn` is neither null nor `""`; it fails that apply on
  `scaleway_container.site` alone (the rest stands), and R4 creates the container in a second pass once
  `edge_endpoint` is set. `[container-lifecycle]` leaves preconditions be, like the postconditions.
- **`min_scale = 0`**: the site stays in the free tier. The first request after an idle period starts an
  instance; R8 warms the container (one request, its answer discarded) before the live checks, and
  records how long that first request took against the live check's 30 s.
- **Postconditions.** The provider does not send `false` or `0` on create (`GetOk`), so the container's
  `lifecycle` asserts `self.https_connections_only == false` and
  `self.min_scale == local.container_min_scale` after the apply. `min_scale` is written once, as the
  local `container_min_scale` (0), which the attribute and the postcondition both read, so the check
  follows the pin: `[container-min-scale]` accepts the local only when it is the literal 0 or 1,
  `[container-min-scale-postcondition]` requires exactly one postcondition comparing `self.min_scale` with
  the expression the attribute sets, and refuses one missing or any other condition on `self` and
  `min_scale` (another value, `self["min_scale"]`), and `[container-https-only-postcondition-missing]`
  requires the `self.https_connections_only == false` postcondition, of which
  `[container-https-only-postcondition]` refuses a second, even an identical one. `[container-image-pin]`
  refuses an `image` other than exactly `local.caddy_image`, the local the Renovate comment pins.
  `[container-environment]` holds `environment_variables` to an object literal whose one key is `UPSTREAM`
  and refuses `secret_environment_variables`, since another variable (`HTTPS_PROXY`, which Caddy's
  `reverse_proxy` honours, `GODEBUG`, `SSL_CERT_FILE`) would change what the front does while the
  behaviour test runs only its own environment; it also refuses any `environment_variables` or
  `secret_environment_variables` on the namespace, attribute or block, since the platform injects a
  namespace's variables into every container in it. `[container-lifecycle]` refuses a non-empty
  `ignore_changes` and any `replace_triggered_by` on the container or its namespace, either of which lets
  a plan pass over a tested change or a console change to a cost pin, and any `enabled` there (OpenTofu
  1.11+), since `enabled = false` acts like `count = 0` and destroys the front. The rules read a condition as its
  tokens: either side of `==`, and one outer pair of parentheses, are the same comparison; any other
  spelling of these two checks is refused, not read.
- **The escaped path.** Caddy v2.11.9 has no placeholder for Go's `URL.EscapedPath()` alone:
  `{http.request.uri.path_escaped}` is `url.QueryEscape` of the path, which escapes every `/`.
  `{http.request.uri}` is Go's `URL.RequestURI()`, the escaped path plus `?` and the raw query when there
  is one, and for an absolute-form target it drops the scheme and the host (`GET http://evil.example/x?q=1`
  gives `/x?q=1`). This amends the ban on `{uri}` above: the `Location` is
  `https://www.shift2eu.com{http.request.uri}` **only behind the guard**
  `@rooted expression {http.request.uri.path}.startsWith("/")`; a target whose path is empty or does not
  start with `/` (an opaque `GET http:foo`, `*`) is redirected to `https://www.shift2eu.com/`. Caddy's
  own `{uri}` stays unused. The local run gave `/a%3Fb?x=1`, `/a%0Ab` and `//evil.example/x` kept
  escaped behind `https://www.shift2eu.com`, and `/` for `GET http:foo`.
- **Repeated `X-Forwarded-Proto`.** Caddy's `{http.request.header.*}` placeholder joins a field's values
  with `,` (its replacer), so `https` + `http` is `https,http`, which the expression misses: the local run
  answered that request with the 308. The behaviour test pins all four spoofing cases on the pinned
  image: `https` then `http`, `http` then `https`, two `https` fields, and the one field `https, https`
  each arrive as a joined value that is not exactly `https`, and each gets the 308 to
  `https://www.shift2eu.com` + the URI, with no request reaching the upstream.
  `{http.request.hostport}` is the request's raw `Host`, port and case as received.
- **`OPTIONS *`: Go's empty 200.** Caddy v2.11.9 never sets `DisableGeneralOptionsHandler`, so Go's server
  answers `OPTIONS *` itself (200, `Content-Length: 0`, no body, no header of the site's) before any
  route; the local run confirmed it, and so does the behaviour test on the pinned image, which pins
  that answer (no Caddy header, none of the site's five). It is the accepted exception above. Whether the
  platform's Envoy forwards it at all is settled live below: it does not.
- **The behaviour test** (`test/sitefront`, `sitefront && !js`) runs the image and the command line
  `infra/site/container.tf` gives, with the Caddyfile's bytes, on the host's network (the fixed `:8080`),
  against a local Go HTTPS upstream named by `UPSTREAM`. Caddy trusts that upstream's test CA through
  `SSL_CERT_FILE`, the CA file mounted read-only: a test-only setting, so the Caddyfile and the
  production container stay as they are and Caddy verifies the upstream's certificate as it does live.
  `make test-sitefront` runs it (docker or nerdctl, through `SHIFT2EU_SITEFRONT_RUNTIME`), outside
  `make test` and `make check`, one run at a time on a machine because of the fixed port; the first run
  pulls the pinned image from Docker Hub. The static Caddyfile test in the same package runs under
  `make test`.
- **Routes.** Matchers in their block form, so each expression is a line of its own: `@https` (the scheme
  expression), `@www` (the hostport expression) and `@rooted` (the guard). `handle @https { handle @www
  { headers; reverse_proxy } handle @rooted { 308 with the URI } handle { 308 to / } }`, then the same two
  redirects for every other request. Nested `handle` blocks are mutually exclusive, so a request matches
  exactly one row of the table.
- **Headers.** The immediate `header` directive (one line per header), and `header_down` deleting the
  upstream's `Strict-Transport-Security`, `Content-Security-Policy`, `Permissions-Policy`,
  `X-Content-Type-Options` and `Referrer-Policy`, and its `Set-Cookie` and `X-Amz-*` (the amendment
  below). **The front sends no `Via`** (issue #626): `header -Via` in `www`'s block removes the
  `Via: 2.0 Caddy` that `reverse_proxy` writes on the answer (a `header_down` alone cannot, since the
  proxy adds its own after the upstream's is gone), and `header_down -Via` drops the upstream's own
  copy; the 308s carry none. It named the front's software, which no other header does. The local run
  with no upstream answered Caddy's 502
  with every header. `Permissions-Policy` turns off by name `accelerometer`, `ambient-light-sensor`,
  `autoplay`, `bluetooth`, `browsing-topics`, `camera`, `compute-pressure`, `display-capture`,
  `encrypted-media`, `fullscreen`, `gamepad`, `geolocation`, `gyroscope`, `hid`,
  `identity-credentials-get`, `idle-detection`, `local-fonts`, `magnetometer`, `microphone`, `midi`,
  `otp-credentials`, `payment`, `picture-in-picture`, `publickey-credentials-create`,
  `publickey-credentials-get`, `screen-wake-lock`, `serial`, `storage-access`, `usb`, `web-share`,
  `window-management` and `xr-spatial-tracking`. **`clipboard-write` is left out**, at the browser's
  default (`self`): the pages' copy buttons write a command to the clipboard on a click
  (`site/page.js`, `copyCommand`), so turning it off would break them.
- **Request headers removed upstream:** `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host`,
  `X-Forwarded-Port`, `Forwarded`, `X-Real-IP`, `X-Envoy-*` (Caddy's header deletion takes a trailing
  `*` as a prefix), `X-Request-Id`, `Via`, `Cookie` and `Authorization`; `Host` is set to
  `{upstream_hostport}` (Caddy's default for an `https://` upstream, written out so the file says it).
- **The staged apply.** A custom domain needs its record first, so `var.front_domains` (default
  `false`) gates both: R4's apply creates the namespace and the container (and, with `pipeline_www` at
  its default `false`, removes the pipeline's `www` and TLS stage on a pipeline created before this
  change; ADR 0039's amendment), R8 points `www` (CNAME) and the apex
  (ALIAS) at the container's endpoint, then an apply with `front_domains = true` creates the domains, and
  R8 waits until both are *ready* before its live checks.
- **Settled live, at the first deploy (R4 to R8), 2026-11-24 to 2026-11-26.** This bullet listed what
  only the live platform could answer: the headers as the container receives them (`X-Forwarded-Proto`
  and `Host` over plain HTTP and HTTPS), the spoofed and repeated `X-Forwarded-Proto` requests,
  `OPTIONS *`, the digest pull, HTTPS on the pipeline's default endpoint with no TLS stage, that the
  Containers API accepts the Caddyfile (about 5.2 KiB) as `args[2]`, the first request's time, the
  domains, their certificates and the headers live. The maintainer's live reading answered each; every
  item below is settled, and nothing of this ADR is left to be settled live:
  - **The baseline (R4, before the apply): settled.** With the TLS stage still in place, the
    pipeline's default endpoint over HTTPS answered `404 verify=0` (its certificate verified).
  - **The front's two checks (R4, after the apply): settled, PASS.** Check 1: HTTPS on the pipeline's
    default endpoint, with no TLS stage left, answered. Check 2: `front ready`, the container running
    the pinned image by its digest, with the Caddyfile taken as `args[2]`.
  - **The digest pull: settled.** A probe container given a zero digest fails to start:
    `error image was not found in container registry`. The platform enforces the digest, so the
    registry mirror above is neither needed nor recorded.
  - **The Caddyfile as `args[2]`: settled.** The Containers API accepted it: the front started with it
    (R4's check 2).
  - **The `scw` flags: settled.** The `scw` container commands take `image=` and `mvcpu-limit=`
    (not `registry-image=` or `cpu-limit=`), `memory-limit-bytes` with a unit (`0.25GB`, shown as
    250 MB; a bare byte count is refused, "size must be defined using the G or GB unit"), and the
    namespace's bare UUID, not the `<region>/<uuid>` that tofu's output gives.
  - **The origins: settled.** The pipeline's default endpoint (Edge Services): HTTPS 200, plain HTTP 302 to
    HTTPS; so HTTPS on it works with no TLS stage. The bucket website endpoint: HTTPS and plain HTTP 200.
    The bucket's S3 endpoints: HTTPS and plain HTTP 200. The container's own endpoint: HTTPS and plain
    HTTP `308 https://www.shift2eu.com/<key>`.
  - **The first request: settled.** The warming request answered 404 in 0.34 s before the deploy (the
    bucket still empty), and 200 in 0.0985 s after the first deploying release (v0.16.1), both far
    inside the live check's 30 s.
  - **The redirects: settled.** Every listed origin, `http://www.shift2eu.com/`,
    `http://shift2eu.com/`, `https://shift2eu.com/` and the container's endpoint over plain HTTP and
    HTTPS, answers 308 to `https://www.shift2eu.com/`, and `https://shift2eu.com/a?b=1` keeps the
    query; the same after the deploy.
  - **The headers: settled.** On `www` each of the five Caddyfile headers comes once with its exact
    value (HSTS `max-age=31536000; includeSubDomains`), before and after the deploy, beside the
    platform's `server: envoy` and `x-envoy-upstream-service-time`, with no `set-cookie` and no
    `x-amz-*` header. The `via: 2.0 Caddy` header seen there is removed by issue #626 (*Headers*, above), live
    with the maintainer's next `infra/site` apply.
  - **The apex's HSTS: settled.** Before issue #601's change was applied the apex sent no HSTS: 0
    headers on every answer. After PR #618 and its apply (2026-11-26, a plan of "No changes" once
    applied), `https://shift2eu.com/` answers HTTP/2 308 to `https://www.shift2eu.com/` with exactly one
    `Strict-Transport-Security: max-age=31536000`, `http://shift2eu.com/` answers 308 with none, and
    `www` is unchanged (*The headers*, above).
  - **Spoofed and repeated `X-Forwarded-Proto` over plain HTTP: settled.** One `https`; `https` + `http`;
    `http` + `https`; `https` + `https`; the one field `https, https`. All five answer the 308. The rule
    fails closed, and the platform does not pass a client's own header through to Caddy as `https`.
  - **`OPTIONS *` over TLS: settled.** It gets the platform's own answer, `HTTP/1.1 404 Not Found`, `server: envoy`,
    `content-length: 0`, none of the site's headers: it never reaches Caddy, so Go's empty 200 above
    never reaches a visitor.
  - **The headers as the container receives them: settled**, read by a temporary container
    `header-probe` on its default endpoint (deleted after; only `front` remains):
    - `X-Forwarded-Proto: http` over plain HTTP, `X-Forwarded-Proto: https` over HTTPS: the platform sets
      the header itself.
    - `Host` is the endpoint, `<namespace>-header-probe.functions.fnc.nl-ams.scw.cloud`, in lower case,
      with no port. `www`'s exact host comparison cannot loop.
    - The platform sends no `X-Forwarded-Host`.
    - The platform adds `Forwarded: for=192.0.2.1;proto=http|https, for=100.96.x.x`, where `192.0.2.1`
      is a documentation address standing in for the visitor's. Caddy never logs it (no access log; the
      amendment *the front writes nothing about a visitor*, below) and strips it upstream
      (`header_up -Forwarded`).
    - A client's `X-Forwarded-Proto: https` plus `X-Forwarded-Proto: http` over plain HTTP reaches the
      container as `X-Forwarded-Proto: http`: the platform replaces a client's value, neither passing it
      through nor joining it.
    - A client's `X-Forwarded-Proto: https` plus `X-Forwarded-Host: evil.example` over plain HTTP on the
      probe's default endpoint got an empty body, its status not captured. This one answer is recorded
      as it came and stays **unexplained** (settled as unexplained: the front's answer below is what
      matters):
      likely the platform routing that request by `X-Forwarded-Host` and answering itself, but
      unverified. The front's own answer to the header: against the front, `X-Forwarded-Host:
      evil.example` and `X-Forwarded-Host: www.shift2eu.com` over plain HTTP both answer
      `308 https://www.shift2eu.com/` (the redirect target never follows the header), and over HTTPS
      `evil.example` gets `HTTP/2 404` with the site's headers (CSP, HSTS) and `server: envoy`, the same
      answer as without it. The front ignores a client's `X-Forwarded-Host`.
  - **The domains and certificates: settled.** `www` is a CNAME to the container's endpoint; the apex's ALIAS
    answers the same A records as the container; CAA unchanged; the DS record present, and DNSSEC
    validates with the ALIAS. Both custom domains were *ready* at the first check. Both certificates are
    Let's Encrypt's (YR2): `www` until 2027-02-23 13:45:22 GMT, the apex until 2027-02-23 13:44:47 GMT,
    which answers Context's "checked at the first deploy (R8)".
- **Residual: the pipeline's default endpoint is a second public origin.**
  `https://<pipeline id>.svc.edge.scw.cloud`, the upstream, stays reachable by anyone and serves every
  bucket object without the front's header policy: no HSTS, no header CSP, no `frame-ancestors`, so it
  can be framed. A page a leaked deploy key uploads is held by the header CSP on `www` only; on the
  default endpoint only its own meta CSP, if any, applies. This is the residual milestone 3 already had
  (the pipeline served `www` and its default endpoint alike, with no header at all), not a regression
  of this front. A shared-secret request header that the front adds and the edge checks would close it,
  if Edge Services could check one: ticket #1639626 answered that Edge Services cannot set headers;
  whether it can match a request header and refuse without it is **unverified** (check Edge Services'
  request-header rules before relying on it). Until then the residual stands, and milestone 4's
  amendment of ADR 0042's residuals carries it with the header CSP.

## Amendment: the memory the platform keeps (issue #596)

2026-11-25. The memory pin is `memory_limit_bytes = 268000000`, no longer `268435456`.

- **Why.** The first apply asked for 268435456 (256 MiB); Scaleway's Serverless Containers kept
  268000000, so every later plan showed `memory_limit_bytes = 268000000 -> 268435456` and never reached
  "No changes": a standing drift the maintainer could not tell from a real one (observed 2026-11-25).
  Why the platform kept 268000000 is not known: neither Scaleway's documentation nor the provider
  states a rounding rule:
  terraform-provider-scaleway v2.93.0 sends the bytes unchanged
  (`internal/services/container/helpers_container.go`, `setCreateContainerRequest`) and reads back the
  platform's value (`container.go`, `ResourceContainerRead`), and its `docs/resources/container.md`
  example is `2048000000`, whole decimal megabytes (scaleway-sdk-go's `scw.MB` is 1000 x 1000 bytes).
  The pin is the value observed on the platform.
- **The cost.** The pin is 268000000 bytes, 268 MB (about 0.268 GB), not 0.25 GB (250000000 bytes),
  and is not to be "corrected" to that; the scaleway/scaleway memory table lists 256 MB for 140
  mvCPU. The worst case above, about EUR 2.12 a month, prices 0.25 GB of memory; billed at 0.268 GB
  per second, which 268435456 was as well, it would be about EUR 2.22. The choice
  between the two values moves it by less than one cent, so no figure here or in PRD #297 changes.
- **What holds it.** `dev/infra-rules.sh`'s `[container-memory]` accepts only the literal 268000000 and
  refuses 268435456 like any other value; `infra/site/tests/container.tftest.hcl` asserts it.

## Amendment: the front writes nothing about a visitor to its output (PRD #297 milestone 4)

2026-11-24. "No `log` directive" turns no access log on, but it does not keep Caddy quiet about visitors.
Caddy's default logger still writes request loggers to the container's output: `http.log.error`, for
every request a handler fails (the 502 with no upstream, or an upstream whose certificate does not
verify), `http.handlers.reverse_proxy`, for a failed round trip, and `http.stdlib`, Go's standard
library server log, whose `http: panic serving <remote addr>` names the client's address. The first two
lines hold the request's `remote_ip` and `client_ip`, its URI and its headers, `User-Agent`,
`X-Forwarded-For` and `X-Real-IP` included, and the platform ships the container's output to Cockpit.
The review's finding; the `http.log.error` leak is confirmed by the behaviour test on the pinned image.

- **The decision.** The Caddyfile's global options hold exactly one `log` block,
  `log { exclude http.log.error http.handlers.reverse_proxy http.stdlib }`, which removes the three
  loggers from the default log. No `log` stands anywhere else (a site block's `log` turns an access log
  on).
- **Why `http.stdlib` too.** Behind the platform the remote address Go names is the platform's, not the
  visitor's, so the line holds no visitor today; it is excluded anyway, since nothing decides that the
  platform always stands in front, and a panic line is no startup or configuration error that R4 reads.
- **Why the exclusion and not `log { output discard }`.** The exclusion keeps the default logger's
  other lines, Caddy's startup and its configuration errors, which hold nothing of a visitor and are
  what a failed start in R4 is read from. `discard` would hide those too.
- **What the behaviour test proves, and what it does not.** `TestSiteFront/the_log_holds_no_marker`
  sends a proxied 200 and a 308 carrying a marker path, a marker `User-Agent` and marker
  `X-Forwarded-For` and `X-Real-IP` addresses, then a 502 with no upstream and a 502 against an upstream
  whose certificate an untrusted CA signs, and reads the container's whole output. It proves the
  `http.log.error` leak and its fix: without that exclusion the markers appear, with it none does. Its
  positive control is Caddy's startup line `serving initial configuration`, which must be in the log
  read, so an empty or partial log cannot pass. The `http.handlers.reverse_proxy` and `http.stdlib`
  exclusions are defence in depth that the test cannot prove (for these two failures neither writes a
  marker, so a Caddyfile excluding `http.log.error` alone still passes it); they are pinned statically,
  name by name. Should a later Caddy release log a request under another logger name, the behaviour
  test fails, and the exclusion grows or the form becomes `discard`.
- **What holds it.** `test/sitefront`'s static test refuses a `log` outside the global options and wants
  exactly one global `log` of one of two shapes, the three-name exclusion in any order or `output
  discard` (`TestCaddyfileRefusedDirectives`, `TestCheckGlobalLogForms`); the behaviour test proves the
  `http.log.error` fix on the pinned image.
- **The upstream's cookie and S3 headers.** The same review pass adds `header_down -Set-Cookie` and
  `header_down -X-Amz-*` to `reverse_proxy`: a cookie Edge Services or the bucket sets, and the store's
  `X-Amz-Request-Id`, `X-Amz-Id-2` and `X-Amz-Meta-*`, are the platform's and the bucket's, not the
  site's. The front sets no cookie, so no cookie reaches the visitor. The static proxy check
  (`TestCaddyfileReverseProxy`) and the behaviour test
  (`TestSiteFront/the_headers_against_the_upstream's_own/an_upstream_whose_headers_are_not_the_site's`)
  hold both.
