# 0045. Front the site with a Serverless Container running a pinned Caddy that writes the redirects and headers

- Status: Proposed
- Date: 2026-11-24

Part of [PRD #297](../../prds/297-web-assessment-site.md) milestone 4 (Hosting decisions 1 to 3, as
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
  runbook's settings: `scw` 2.67, the official `docker.io/library/caddy:2.8.4` image with the container's
  command and arguments set, `port=8080`, `privacy=public`, `https-connections-only=false`,
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
The documentation does not name the certificate authority: that it is Let's Encrypt is C3's result.

## Decision

**One public Scaleway Serverless Container, running the official Caddy image pinned by digest with a
configuration from the repository, serves both `www.shift2eu.com` and `shift2eu.com`, with plain HTTP
`enabled`; Caddy writes every redirect and every security header itself and reverse-proxies the site
from the Edge Services pipeline endpoint, which stays the only reader of the private bucket.**

### The container

- **One container, two custom domains:** `www.shift2eu.com` (a Namecheap CNAME to the container's
  endpoint) and `shift2eu.com` (a Namecheap ALIAS at `@` to the same endpoint), each with the
  certificate Scaleway issues for a custom domain (Let's Encrypt, C3). DNSSEC stays on, and the CAA
  record stays `0 issue "letsencrypt.org"`, `0 issuewild ";"` (Hosting decision 10).
- **Plain HTTP `enabled`, not `redirected`.** `redirected` answers 301 on the same host, so
  `http://shift2eu.com` would take two hops to `https://www.shift2eu.com`, and the redirect would be the
  platform's rather than the configuration's. With `enabled`, plain HTTP reaches Caddy, which redirects.
- **The cost pins** (enforced by the infra unit in `infra/site`, as the configuration's values):
  `max_scale` **1**, **140 mvCPU**, **0.25 GB** of memory. Prices verified by the maintainer on
  2026-11-24 (Scaleway's pricing page, Amsterdam): EUR 0.00001 per vCPU-second after 200,000 free, and
  EUR 0.000002 per GB-second after 400,000 free, per account and month. The worst case, one instance
  running a whole 30-day month (2,592,000 s), is about **EUR 2.13 a month**; with no instance warm most
  of the time it stays in the free tier. `min_scale` (0, with a cold start on the first visit after an
  idle period, or 1, always warm) is *for the infra unit*: both stay within the worst case. One instance
  also bounds capacity: a burst beyond it waits or fails, which this small static site accepts.
- **Who can change it:** only the maintainer, through `infra/site` and `dev/tofu.sh` on the admin
  machine (ADR 0039). The deploy key holds no Serverless Containers or Container Registry permission
  (its IAM rule grants the two Object Storage permission sets only, ADR 0042), and no release changes the
  container: a release changes the bucket's objects, never the front.

### What the server answers

Every rule is keyed on the request's `Host` and `X-Forwarded-Proto` (the TLS connection ends at the
platform; Caddy listens on plain `:8080`, with its automatic HTTPS off). "The URI" is the path and the
query, unchanged.

| Request | Answer |
|---|---|
| `X-Forwarded-Proto` anything but exactly `https`, any host (`http://www.shift2eu.com`, `http://shift2eu.com`, the container's endpoint over plain HTTP) | **308** to `https://www.shift2eu.com` + the URI, one hop |
| HTTPS, `Host: shift2eu.com` | **308** to `https://www.shift2eu.com` + the URI |
| HTTPS, `Host: www.shift2eu.com` | the site: reverse proxy to the Edge Services pipeline endpoint, with the headers below |
| HTTPS, any other host (the container's own endpoint `<namespace>-<name>.functions.fnc.<region>.scw.cloud`) | **308** to `https://www.shift2eu.com` + the URI |

So neither name, nor the container's endpoint, ever answers 200 over plain HTTP, the transient plain
HTTP during a certificate's issuance included (it reaches the same rule), and only
`https://www.shift2eu.com` serves the site through the container.

**The preload chain** (Hosting decision 2) is not configured while preload is not pursued. If milestone
4 records `preload: true` in `site/records/hsts.json`, the configuration changes in the same change:
`http://shift2eu.com` goes first to `https://shift2eu.com` (308, same host), whose HTTPS answer carries
`Strict-Transport-Security: max-age=31536000; includeSubDomains; preload` with its 308 to
`https://www.shift2eu.com`, and `www`'s header gains `preload`.

**Whether the platform overwrites a client's own `X-Forwarded-Proto`** is undocumented (the
documentation says only that `X-Real-IP` is overwritten). If it did not, a client sending
`X-Forwarded-Proto: https` over plain HTTP would get the site with a 200 on plain HTTP. Only that client
could: a network attacker cannot make a victim's browser send the header, and could serve its own page
anyway. The infra unit's test (below) and a live probe settle it: a plain-HTTP request carrying
`X-Forwarded-Proto: https` must still get the 308. The infra unit records the answer here; a 200 is the
maintainer's to accept or not, on the record, since it changes no browser's visit.

### The headers

Caddy sets these on every HTTPS response of `www.shift2eu.com` (replacing any copy from upstream), the
site's files and every other path alike:

| Header | Value |
|---|---|
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` (plus `; preload` only once preload is pursued, above) |
| `Content-Security-Policy` | exactly the `<meta>` policy every page carries (ADR 0036), plus `frame-ancestors 'none'` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `no-referrer`, as the pages' `<meta name="referrer">` |
| `Permissions-Policy` | every feature the site does not use turned off (`()`); the exact list is *for the infra unit*, pinned by the configuration test |

The `<meta>` policy and the framing guard in the page script stay: they travel with the bundle to every
other origin (Hosting decision 11). The apex's 308 carries no HSTS while preload is not pursued (the live
check only records the apex's header then, ADR 0043). No other header is decided here;
`Clear-Site-Data` and `Cross-Origin-Opener-Policy` are now possible and are proposed follow-ups.

### The upstream

Caddy reverse-proxies `www`'s HTTPS requests to `https://<pipeline id>.svc.edge.scw.cloud`, verifying
its certificate, with the endpoint as SNI and `Host` (C3's `--change-host-header`). Edge Services' cache
and the private bucket stay as ADR 0042 has them; `ci/sitedeploy`, the key's scope and R7 do not change.
The pipeline's own `www.shift2eu.com` custom domain and its certificate are no longer needed (*for the
infra unit*). The response passes through unchanged but for the headers above: status (a 3xx where a
manifest file is expected still fails the live check), body, `Content-Type`, `Content-Encoding`,
`Cache-Control` and `Age`. The configuration has no `encode` directive and no cache, so the module
script is still served with the bytes and the `Content-Encoding: gzip` the deploy uploaded (Hosting
decision 4, ADR 0043's budgets).

Proposed, *for the infra unit to confirm*: the configuration removes `X-Forwarded-For` from the request
upstream (`header_up -X-Forwarded-For`), since Edge Services needs nothing of the visitor, and turns no
access log on (no `log` directive). What the platform itself logs is Scaleway's, as for Edge Services;
the privacy page already names Scaleway.

### The image and its configuration

- **The image is the official `caddy` image, pinned by digest**
  (`docker.io/library/caddy:<version>@sha256:<digest>`), with a
  `# renovate: datasource=docker depName=caddy` comment on the line above it (CLAUDE.md rule 7), so
  only a Renovate PR moves it. C3 ran the official image straight from Docker Hub (by tag). Whether the
  platform pulls by digest, and how it behaves when Docker Hub is slow or limits pulls at a cold start,
  is *for the infra unit*.
- **The configuration is a file in the repository**, beside the container in `infra/site`, changed only
  by a reviewed pull request and applied by the maintainer. How it reaches the container is *for the
  infra unit*, preferring the upstream image byte for byte with the configuration given at start (C3
  set the command and arguments; an environment variable or a start command that writes the file are
  the candidates), so that no image is built and no registry key exists. Only if that cannot hold the
  configuration does the maintainer build an image `FROM` the pinned digest with the file copied in and
  push it to a Container Registry namespace in the site's project; that path is recorded here before it
  is taken.
- **The configuration is tested, offline and live.** A static test under `make check` pins what the
  configuration says: the four rules of the table, the HSTS value against `site/records/hsts.json`
  (with `preload` exactly when it is pursued), and the `Content-Security-Policy` header against the
  built pages' `<meta>` policy plus `frame-ancestors 'none'`, so a change of the policy in `site/` fails
  until the configuration follows. A behaviour test runs the pinned image on the configuration in a
  local container, outside `make test` like the browser smoke test (ADR 0036), against a local
  upstream, and drives every row of the table with `Host` and `X-Forwarded-Proto`, the headers, and a
  gzip-encoded object passing unchanged. The live checks (ADR 0043) hold the result in production.
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
  returning visits included, would cross the network in clear text before a temporary redirect; preload
  would become impossible; the apex would still need a front. It breaks a mandatory go/no-go that is
  "never waived".
- **The platform's `redirected` HTTP option.** A 301 on the same host: two hops from
  `http://shift2eu.com`, and the redirect outside the tested configuration.
- **Namecheap's URL redirect for the apex.** No certificate on `https://shift2eu.com`, no HSTS, and
  nothing for `http://www`.
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
- The header CSP binds every response of `www`, so a page or a script a leaked deploy key uploads at
  any path is held to `connect-src 'none'` and `worker-src 'none'` as well, whatever it carries itself
  (two policies both apply). Navigation stays outside the CSP. ADR 0042's residuals say so once the
  header is live (milestone 4); the privacy page's sentences that the
  site "cannot send" `Clear-Site-Data` or `Cross-Origin-Opener-Policy` become "does not send", in the
  unit that changes `site/`.
- Scaleway now also serves the apex: the apex redirect's provider (Hosting decision 3) is Scaleway, which
  the privacy page already names; no third party is added.
- **For the infra unit:** a Serverless Containers namespace, the container (the pins above), two custom
  domains, the configuration and its tests in `infra/site` and `make check`; the pipeline's `www` custom
  domain dropped; the provider stays `scaleway/scaleway` alone, so `dev/infra-rules.sh` keeps its one
  provider; ADR 0039 amended; the runbook's R4, R5 and R8 rewritten (the `www` CNAME and the apex ALIAS to
  the container's endpoint, the origin and redirect checks).
- **For the live-check unit:** `ci/sitecheck` allows the platform's `server: envoy` and
  `x-envoy-upstream-service-time` response headers; `-plain-http-www` stays at `redirect`, so the
  refuse-or-close mode (ADR 0043) is no longer needed by this hosting.
- New upkeep: Renovate's digest PRs for Caddy, each reviewed and applied by the maintainer; a Caddy CVE
  means such a PR and an apply. A container's cold start can delay the first visit after an idle period
  if `min_scale` is 0.
- Recovery gains a step: if the container or its configuration is broken or changed, the maintainer
  re-applies `infra/site` at `main` (the pinned digest and the committed configuration), then runs the
  redirect and header checks (ADR 0042, *Recovery*, as amended).
