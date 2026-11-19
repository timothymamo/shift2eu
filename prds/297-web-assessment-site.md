# PRD #297: shift2eu in the browser at shift2eu.com

**Status:** Draft
**Issue:** #297

## Problem

Today the only ways to run an assessment are the `shift2eu` CLI and the `shift2eu-server` HTTP API
(PRD #103). Both ask a user to download a binary or an image, verify it and run it, before they learn
whether shift2eu is worth their time. Someone evaluating a move from AWS, Azure or GCP to an EU cloud
provider wants to paste an inventory into a page and see the answer.

A hosted assessment usually means uploading the inventory to someone's server. An inventory describes a
company's infrastructure (CLAUDE.md rule 2), and many users cannot, or should not, send it anywhere. The
HTTP API was built to sit behind a website's backend (PRD #103, *Open: hosting*), but a backend that
receives inventories is exactly what such users refuse.

Go compiles to WebAssembly, and the assessment needs no network: the CLI's offline path reads an
inventory, assesses it against embedded catalog snapshots and renders a report. The same code can run in
the user's browser, so a site can offer the assessment while **no inventory ever leaves the browser**.
That promise is the product: it has to hold by design, be enforced in layers, be visible to the user and
be checkable.

## Outcome

`https://shift2eu.com` serves a static site from this repository, released with shift2eu. A user pastes
or uploads an inventory document (schema version 1 or 2), or starts from a bundled example, chooses one
or more targets (OVHcloud, Scaleway, UpCloud) and optionally a region, and sees the assessment or the
comparison in the page, with estimates and their caveats. They can download the report as Markdown and as
the versioned JSON report, byte for byte what `shift2eu assess` prints for the same inventory and options.

The assessment runs client-side, compiled to WebAssembly from the same Go packages as the CLI. There is no
backend. The trust model has three layers:

1. **The code.** The page's code is reviewed Go plus one small reviewed script; a static rule bans every
   browser API that can send data (fetch, navigation, WebSocket, WebRTC, beacons, workers) from both.
2. **The browser.** A strict Content Security Policy refuses network connections, third-party resources,
   inline script and forms, so a bug or an injection that slips past the first layer is still refused for
   every channel the CSP governs. Its known gaps (navigation, WebRTC, DNS prefetch) are documented and
   closed by the first layer.
3. **Verification.** The site bundle is reproducible and attested by the release, and the release checks,
   and documents how a user can check, that the live site serves exactly the attested files.

The site sets no cookies, has no analytics, and loads nothing from a third party. Each release deploys it
to Scaleway Object Storage behind Scaleway Edge Services from `release.yml`.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | AWS, Azure and GCP, through inventory documents (ADR 0006, ADR 0025) the user pastes or uploads, and the bundled examples in `examples/`. A link to the inventory guide, which explains how to make an inventory from read-only CLI output. | Live source adapters. Any cloud credential in the browser. |
| Target providers | OVHcloud, Scaleway and UpCloud, from the embedded catalog snapshots of the release that built the site, any or all of them (as `--target`), each optionally scoped to one region (as `--region`). | Catalog changes. |
| Taxonomy categories | Every taxonomy category the release's catalogs and inventory reader know; the site adds none. | New taxonomy categories. |
| Output | The assessment and the comparison shown in the page, per resource and per workload, with estimates (on by default, with an equivalent of `--no-estimate`). Downloads of the Markdown report and the JSON report (schema version 2, ADR 0018, ADR 0030). | PDF. A readiness score. Changes to the Markdown or JSON report. |
| Site | Static pages (HTML, CSS, one small script, the WebAssembly module and Go's `wasm_exec.js`), a privacy page, built by Go's toolchain only and deployed by the release. | Accounts, saved or shared reports, any server-side code, analytics, a JavaScript framework, npm or any JavaScript build tool. The HTTP API (`shift2eu-server`): the site never calls it. |
| Hosting | Scaleway Object Storage behind Scaleway Edge Services (Starter plan), the domain `shift2eu.com` at Namecheap, deployed from this repository's GitHub Actions only. | Any third-party builder or hosting (Netlify, Vercel, Cloudflare Pages, GitHub Pages). |

## Acceptance criteria

All criteria are pinned by tests that run offline under `make test`, except those marked *browser* (the
headless-browser smoke test, `make test-browser` and its CI job), *release* (checked by `release.yml` on a
release), *live* (checked against the live site) or *review*.

### Assessment in the browser

- [ ] *Browser:* given a bundled example inventory (`examples/aws`, `examples/azure`, `examples/gcp`), when
  the user picks it and runs the assessment with one target, then the page shows the assessment: the
  summary, every resource with its fit, best offering, blockers and required changes, the workloads, and
  the estimates.
- [ ] *Browser:* given an inventory pasted into the text field or chosen with the file input (schema
  version 1 or 2), when the user selects several targets or all of them, then the page shows the
  comparison, as `shift2eu assess --target all` reports it.
- [ ] *Browser:* given a region for a target, the report is scoped to that region as `--region
  <target>=<region>` scopes it; given the estimate switch turned off, the report has no estimates, as
  with `--no-estimate`.
- [ ] Given the same inventory and options, the Markdown and JSON downloads are byte-identical to the
  output of `shift2eu assess --format markdown` and `--format json` of the same commit. Pinned natively by
  a parity test over every example inventory and the hostile fixtures, for one target, several and `all`,
  with and without a region and estimates; and in the browser by the smoke test for one example
  (*browser*). Any difference fails the test.
- [ ] An inventory the reader refuses (malformed JSON, an unknown schema version, an unknown field, a
  document over the reader's size cap) shows one error message in the page, by the rule the HTTP API
  applies: it names a position and field and never echoes a value from the inventory (the in-memory mode
  of `inventoryfile.FromBytes`, PRD #103). A bad target or region choice shows the CLI's usage error. The
  page stays usable after an error.
- [ ] The option rules (target keys, `all`, duplicates, regions per target, the estimate switch) are the
  CLI's: a test runs one table of option sets through the CLI and the web adapter and expects the same
  accepted options and the same errors.
- [ ] Every string the page shows from an inventory or a catalog is escaped: a hostile inventory (quotes,
  angle brackets, `<script>`, control characters, invalid UTF-8) renders as text, pinned by a hostile
  golden of the HTML reporter and, in the browser, by the smoke test finding no element, attribute or
  script the inventory tried to inject.

### Trust: nothing leaves the browser

**Layer 1, the code.**

- [ ] A static test fails when the page's script or the `syscall/js` glue names a browser API that can
  send data or load a resource: `fetch` (except the one call that loads the module), `XMLHttpRequest`,
  `WebSocket`, `EventSource`, `sendBeacon`, `RTCPeerConnection`, `Worker`, `SharedWorker`,
  `importScripts`, `open`, `location` (assignment or navigation methods), `href`, `src` or `action`
  assignment, `document.cookie`, `localStorage`, `sessionStorage` and `indexedDB`. The glue calls
  `js.Global().Get`, `Value.Get` and `Value.Call` only with constant names from a named allow list; a
  forbidigo or test rule refuses a computed name.
- [ ] Every page has `<meta http-equiv="x-dns-prefetch-control" content="off">` and no `<link rel>` of
  `dns-prefetch`, `preconnect`, `prefetch` or `prerender` (static test).
- [ ] The script stops, and shows why, when the page is framed (`window.top !== window.self`), since a
  `<meta>` CSP cannot carry `frame-ancestors` (static test of the script and *browser*).

**Layer 2, the browser.**

- [ ] Every page carries a Content Security Policy in a `<meta http-equiv>` element, the first child of
  `<head>` (before any `<link>` or `<script>`), with at least: `default-src 'none'`; `script-src 'self'
  'wasm-unsafe-eval'` and nothing else (no `'unsafe-inline'`, no `'unsafe-eval'`, no other host);
  `style-src 'self'`; `img-src 'self'`; `connect-src` naming only the module's exact content-addressed
  URL, never `'self'`, a directory or another host; `form-action 'none'`; `base-uri 'none'`;
  `object-src 'none'`; `require-trusted-types-for 'script'` and `trusted-types` naming the one policy that
  inserts the Go-rendered report; and no `report-uri` or `report-to` (violation reports carry blocked
  URLs and samples). A static test parses each built page's policy and fails on a missing, widened or
  added directive. Where the host can also send the policy as a header (Open question 2), the header adds
  `frame-ancestors 'none'`.
- [ ] Once the module is loaded, the page adds a second policy with `connect-src 'none'`, if the browser
  enforces a policy added after load; milestone 2's ADR records whether it does. *Browser:* after loading,
  the smoke test attempts, from the page, a `fetch` to another origin, to an own-origin path that is not
  the module, and to the module's URL with an appended query string, and records every request the page
  made. The first two must be blocked; the third must be blocked if the second policy holds, and is
  otherwise documented as a residual in the ADR and the privacy page's technical note (CSP path matching
  ignores the query). The recorded requests must be only the site's own files.
- [ ] *Browser:* the smoke test records that a navigation or an `RTCPeerConnection` started from the page
  is **not** blocked by the CSP, and the ADR documents both as gaps that layer 1 closes, so no criterion
  or statement claims the CSP covers them.
- [ ] Every script, stylesheet and the module are loaded with Subresource Integrity, SHA-384: the
  `<script>` and `<link rel="stylesheet">` elements carry `integrity`, and the script loads the module
  with `fetch(url, {integrity})`, its digest held in the script (itself SRI-pinned) or the page. A static
  test computes the SHA-384 of every built file and fails when an `integrity` value, including the
  module's digest in the script, is missing or does not match.
- [ ] The page has no inline script, no inline style attribute that the CSP would refuse, and no inline
  event handler (static test).
- [ ] No page, stylesheet or script references another origin as a loaded resource (no fonts, images,
  scripts or styles from a third party); links to other sites are plain `<a href>` navigations. A static
  test lists every URL in the built files and fails on a loaded resource that is not the site's own.
- [ ] *Live:* no response from the site carries `Set-Cookie`.

**Privacy statement.**

- [ ] A privacy page, linked from every page, says what the site is built to do and what checks it: the
  assessment runs in the browser and the page's code sends nothing (layer 1, with its static test); the
  browser refuses connections and third-party resources (layer 2, with its CSP test and smoke test); the
  deployed files are the attested ones (layer 3); no cookies, storage, analytics or third-party resources.
  It also says what the site cannot promise: the hosting provider (Scaleway) sees the usual request
  metadata of any web server (IP address, user agent, the files requested) and nothing of the inventory;
  browser extensions can read any page; a user who must not trust the host can run the attested bundle
  locally (layer 3). *Review:* each statement names the tested criterion that backs it, and none rests on
  review alone.

**`wasm_exec.js`.**

- [ ] Go's `wasm_exec.js` is taken at build time from the Go toolchain that builds the module
  (`$(go env GOROOT)/lib/wasm/wasm_exec.js`), never committed and never fetched. The page shows the Go
  version the module reports (`runtime.Version()`), and a test fails when the bundled `wasm_exec.js`
  differs from the building toolchain's. The toolchain is pinned by `go.mod`, which Renovate updates, and
  every CI and release job that builds the site sets up Go from `go.mod` (`go-version-file`).

### Layer 3: users can check what they run

- [ ] *Release:* every release publishes the site bundle as the release asset
  `shift2eu-site_<version>.tar.gz`, listed in the release's checksums file and carrying build provenance
  attested by `release.yml` like the archives (ADR 0015), with a manifest (`manifest.json` in the bundle)
  of every file's path and SHA-256.
- [ ] The bundle is reproducible: building the site twice from the same commit with the pinned toolchain
  gives byte-identical files and archive (a test builds it twice and compares). The build uses
  `-trimpath`, a fixed or no VCS stamp, sorted archive entries with fixed times, owners and modes, and
  compressed files with zero mtime and a fixed level.
- [ ] *Release:* after the deploy, `release.yml` fetches every file of the manifest from the canonical
  host, with no query string, and fails when one does not match its SHA-256 after content decoding. It
  retries for a stated bound at least as long as the HTML's cache lifetime, and records which host it
  checked and the `Age` of each response.
- [ ] The release verification guide (`docs/guide/verify-releases.md`) documents the same check as
  commands a user can run (`gh attestation verify` on the bundle with `--signer-workflow`,
  `--source-ref refs/tags/<tag>` and `--deny-self-hosted-runners`, then fetching each file of its manifest
  from the live site and comparing digests); that the page shows its version so the user knows which
  release to check; that the check is one point in time from one vantage point, since the HTML is the root
  of trust and nothing pins it; and how to run the attested bundle locally with any static file server.

### Deploy

- [ ] *Release:* `release.yml` gains a site deploy job that runs after the release's `verify` and
  `verify-archives` jobs have passed, and only when the `latest` job says this tag is the highest release
  tag on `main` (as the images' `latest` tag), so a re-run or an older line's patch release never rolls
  the site back. `check.ReleaseWorkflow` pins both conditions.
- [ ] The deploy job runs only when the repository variable `SITE_DEPLOY_ENABLED` is `true`; otherwise it
  is skipped with a visible notice in the run's summary. The maintainer sets it at the end of the runbook
  (milestone 4), so releases between milestones 3 and 4 do not fail. `check.ReleaseWorkflow` pins the
  condition, and the `release-tag` skill's post-release check reports the skip as expected while the
  variable is unset.
- [ ] The deploy job does not build. A job without secrets builds the deploy program; the deploy job takes
  it and the bundle of this run as artifacts, checks both digests against those this run produced, and
  verifies the bundle's provenance with `gh attestation verify` naming `release.yml` as the signer
  workflow, the run's tag as the source ref and `--deny-self-hosted-runners`. It runs no third-party
  action after the key is in scope; it checks out nothing, or with `persist-credentials: false`; its
  permissions are `contents: read` and `attestations: read`; and the key's two secrets are the `env:` of
  the one step that uploads, never of the job.
- [ ] The deploy program refuses to replace live HTML whose embedded version is higher than the bundle's
  (one read of the live page before the upload).
- [ ] The `site` environment holds the Scaleway API key as the environment secrets `SCW_ACCESS_KEY` and
  `SCW_SECRET_KEY`, never at repository level or in another environment. Its deployment policy is exactly
  one custom policy of type `tag`, named `v*.*.*`; the `v*` tag rulesets that the `release-tag` skill
  already checks are what let only the release-tag App create such a tag (ADR 0021).
- [ ] The key belongs to a Scaleway IAM application that can write objects **only to the site's bucket**:
  no other bucket, project or product, and no bucket-configuration, bucket-policy or ACL permission
  (runbook and Open question 5). The bucket policy allows that application only the object writes and
  reads the deploy needs. The key has an expiry date, and the runbook has a rotation step. *Review:* the
  runbook's policies are checked against Scaleway's permission sets.
- [ ] The `release-tag` skill's step 2 checks the `site` environment: `custom_branch_policies` `true` and
  `protected_branches` `false`; `deployment-branch-policies` lists exactly `tag v*.*.*` (type `tag`, so a
  branch named like a tag is not allowed); its secrets are `SCW_ACCESS_KEY` and `SCW_SECRET_KEY`; and
  neither name is listed at repository level. A docs test pins that the skill names the environment, the
  policy and both secrets.
- [ ] A failed deploy fails the release run visibly and leaves the previous release's site serving:
  content-addressed assets are uploaded first, the HTML pages last, and no earlier release's file is
  deleted. A test of the deploy program against a fake object store (`httptest`) pins the order, that a
  failure before the HTML upload changes no page, and that the program never logs the key.
- [ ] The deploy program is CI tooling under `ci/` (beside the AI reviewer, ADR 0033), standard library
  only, held by strict depguard rules of its own added to the pin. CLAUDE.md's CI tooling row and
  `docs/architecture.md` name it and say that the release runs it. Every new tool or action is pinned,
  with a `# renovate:` comment where Renovate does not find it (CLAUDE.md rule 7).

### Page content

- [ ] Every estimate shown carries the catalog snapshot's date and the caveat that prices are list prices
  from that date, not a quote, as the Markdown report words it.
- [ ] Every page shows the shift2eu version that built it, and links to the repository and to the
  inventory guide (`docs/guide/inventory.md` on GitHub at that release's tag). Built from an untagged
  commit (pull request CI, `make site`), the page shows `internal/version`'s development value, as
  `shift2eu version` does, and the guide link points to `main`.
- [ ] Without JavaScript, a `<noscript>` message says the assessment needs JavaScript and WebAssembly and
  points to the CLI (static test); without WebAssembly support, the script shows the same message
  (*browser*, with WebAssembly disabled). The privacy page and the links work without either.

### Size and speed

- [ ] The module is built with Go's standard toolchain (`GOOS=js GOARCH=wasm`), not TinyGo.
- [ ] A test fails when the gzip-compressed module exceeds the budget: **3 MiB**, recorded with the
  measured size in milestone 1's ADR. Above it, TinyGo is reconsidered (Decisions).
- [ ] The module, the script, `wasm_exec.js` and the stylesheet are served under content-addressed names
  with `Cache-Control: public, max-age=31536000, immutable`; the HTML pages with `max-age=300` or less, so
  a release shows within minutes without a cache purge. Scaleway's documentation says an object's caching
  directive takes precedence over the pipeline's cache lifetime
  ([cache](https://www.scaleway.com/en/docs/edge-services/how-to/configure-cache/)); *live:* the release's
  site check reads these headers and the `Age` of the HTML.
- [ ] *Live:* the module is served with `Content-Type: application/wasm` and, if Scaleway serves it so
  (Open question 4), `Content-Encoding: gzip`. If it cannot, a second test also holds the uncompressed
  module to a budget set in milestone 3's ADR.
- [ ] *Browser:* once loaded, the page runs an assessment with the network disabled.

### Accessibility

- [ ] Every control has a label; the whole flow (choose an input, choose targets and region, run, read,
  download) works with the keyboard alone; focus is visible; text and controls meet WCAG 2.2 AA contrast
  (a test checks the stylesheet's colour pairs).
- [ ] The report is semantic HTML (headings, tables with header cells, lists) rendered in Go with
  `html/template` by the HTML reporter, not tables assembled in JavaScript. Errors are announced to
  assistive technology (`role="alert"`). *Browser:* the smoke test checks the labels and the tab order of
  the main flow.

### Architecture

- [ ] The report in the page is rendered by a new reporter adapter, `internal/adapter/report/htmlreport`,
  which writes an HTML fragment for an assessment and a comparison through the same reporter ports as
  `markdown` and `jsonreport`, with `html/template`'s contextual escaping. It is held by
  `reporters-stay-offline` and named in CLAUDE.md's adapters row; its goldens, hostile golden included,
  are in its own `testdata/`.
- [ ] The logic the browser calls is a new driving adapter, `internal/web`, beside `internal/cli` and
  `internal/httpapi`. It takes the inventory bytes and the options, runs the use cases and returns the
  report bytes for each download and the HTML fragment, or an error. It imports only the application, the
  domain, `internal/version` and a named list of standard packages (no `os`, no `net`, no `syscall/js`),
  and gets every adapter through its `Deps`.
- [ ] The adapters are wired in `cmd/shift2eu-web/internal/wire`, an untagged package that builds and
  tests natively: it constructs what `cmd/shift2eu`'s offline path constructs (the inventory file reader,
  the three target catalogs, the Markdown, JSON and HTML reporters) into `internal/web`'s `Deps`. Its
  black-box tests, run by `make test`, hold the option-parity table and the download parity test: they run
  `internal/cli` and `internal/web` over the same deps and compare. `cmd/shift2eu-web` itself (build
  constraint `js && wasm`) holds only the `syscall/js` glue and imports `wire`, `internal/web`,
  `syscall/js` and a named list of standard packages; never `internal/cli`, `internal/httpapi`, a request
  decoder or a cloud SDK.
- [ ] New depguard rules, written like their siblings and added to the depguard pin
  (`TestDepguardRulesArePinned`): `web-gets-adapters-from-cmd` (the strict allow list of `internal/web`),
  `web-imported-only-by-the-web-root` (only `cmd/shift2eu-web` and its `wire` package import it),
  `web-composition-root` (the strict allow lists of `cmd/shift2eu-web` and of `wire`, with the parity
  tests' extra imports, `internal/cli` among them, allowed in `wire`'s test files only). The existing rules
  are extended so that neither `internal/cli`, `internal/httpapi`, `cmd/shift2eu` nor `cmd/shift2eu-server`
  imports `internal/web`, `htmlreport` aside from the composition roots that wire it.
- [ ] `make lint` and CI's lint matrix also run golangci-lint with `GOOS=js GOARCH=wasm`, and
  `test/architecture` checks `js/wasm` as an explicit extra target beside those it reads from
  `.goreleaser.yaml` (ADR 0012); a test fails when `js/wasm` drops out of either list. Without this, the
  `js && wasm` files would never be linted or checked.
- [ ] A test under `make test` runs `go list -deps` with `GOOS=js GOARCH=wasm` on `./cmd/shift2eu-web` and
  fails on `net`, `net/http`, `os/exec` or a cloud SDK in its dependency tree.
- [ ] CLAUDE.md's layer table names `internal/web` among the driving adapters, `cmd/shift2eu-web` among
  the composition roots and `htmlreport` among the adapters, and `docs/architecture.md` describes them.
- [ ] *Review:* the use cases, the target adapters, the inventory reader and the Markdown and JSON
  reporters are reused unchanged; the CLI's and the HTTP API's output do not change. Option parsing that
  the CLI and the web share moves out of `internal/cli` only into a layer both may import (the
  application layer), with the CLI's output unchanged.

### Testing and tooling

- [ ] `make test` runs every native test of this PRD with no network and no browser.
- [ ] `make site` builds the bundle into a git-ignored directory. `make test-browser` runs the smoke test,
  written in Go with `chromedp` (no npm, no Playwright or Puppeteer) against the headless Chromium of a
  container image pinned by digest with a `# renovate:` comment, whose DevTools port is published on
  `127.0.0.1` only; it serves the built bundle from a local `httptest` server.
- [ ] `chromedp` and its dependencies are confined by depguard to the smoke test's package, and a test
  pins that no product binary's dependency tree (`cmd/shift2eu`, `cmd/shift2eu-server`,
  `cmd/shift2eu-web`) reaches `chromedp` or any package only it brings in.
- [ ] CI runs the smoke test as its own job on every pull request and on `main`, with `contents: read`
  only. The release runs it on the bundle it attests, before the deploy, in a job that holds neither the
  attestation's `id-token` nor the Scaleway key.

### Runbook

- [ ] A one-time runbook for the maintainer, written in milestone 3 and followed in milestone 4: the
  Scaleway project that holds only the site's bucket; the bucket, its website setting, versioning (if
  Scaleway offers it for a website bucket, so an overwrite can be rolled back) and its bucket policy; the
  Edge Services subscription (Starter) and pipeline, its cache and custom domain; the IAM application, its
  policy, and its API key with an expiry and a rotation step; the `site` GitHub environment, its
  deployment policy and secrets; the Namecheap records (the Edge Services CNAME, the redirect of the
  second host, CAA allowing only the certificate authority Edge Services uses, DNSSEC on); and, last,
  `SITE_DEPLOY_ENABLED`. It also says to remove the DNS record before deleting the pipeline or the bucket,
  so no dangling CNAME can be taken over. Every step names what the `release-tag` checks or the live check
  verify afterwards.

## Milestones

Each milestone ships as one PR, in this order, and each gets the security audit (`security-audit`
skill), since every one of them touches the trust model or the release.

1. [ ] **The WebAssembly adapter and its ADR.** `internal/web`, the HTML reporter `htmlreport`,
   `cmd/shift2eu-web` (the thin `syscall/js` glue) and its `wire` package, with native tests: the
   option-parity table, the download parity with the CLI over every example and the hostile fixtures, the
   HTML reporter's goldens, the module size budget, the offline dependency test for `js/wasm`. The
   depguard rules and their pin, `js/wasm` in `make lint`, CI's lint matrix and `test/architecture`,
   CLAUDE.md's table and `docs/architecture.md`. An ADR records the WebAssembly driving adapter, its
   composition root and wiring package, the HTML reporter, the standard toolchain over TinyGo, the size
   budget and the measured size, and how `wasm_exec.js` is tied to the toolchain.
2. [ ] **The pages, their tests and the CSP.** The static pages under a new directory (HTML, CSS, one
   small script), the privacy page, `make site` (a Go program, standard library only, that assembles the
   bundle: content-addressed names, SRI values, the version, the snapshot dates), the static tests (CSP,
   SRI, banned APIs, prefetch, links resolve, no third-party resource, contrast), the reproducibility test,
   and the `chromedp` smoke test with `make test-browser` and its CI job. `chromedp` is a new dependency
   with its reason in the commit message (CLAUDE.md rule 6). An ADR records the site's trust model (its
   three layers, the CSP and its known gaps, SRI, Trusted Types, no storage, how the report is inserted)
   and the headless-browser test. Depends on 1.
3. [ ] **Release, attestation and deploy.** The bundle built, tested in the browser, attested and
   published by `release.yml`; the deploy program and the `site` deploy job behind `SITE_DEPLOY_ENABLED`
   and `latest`; the live site check; `check.ReleaseWorkflow` and the CI dry run; the `release-tag`
   skill's step 2 for the `site` environment and its post-release check; `docs/guide/verify-releases.md`;
   the runbook. An ADR records the hosting, the deploy (environment, key scope, upload order, cache
   headers, rollback guard) and the bundle's attestation. Releases cut after this merge skip the deploy
   visibly until the variable is set. Depends on 2.
4. [ ] **First deploy and the privacy statement live.** After the maintainer has followed the runbook and
   answered the hosting questions (Open questions 1 to 5, 9 and 10), and the go/no-go below is met: the
   first release that deploys, the live checks green (files match the manifest, headers, no cookie, HTTPS
   and the redirects), any fix the live site needs, the README's link to the site, and this PRD moved to
   `prds/done/` (`Closes #297`). **Go/no-go:** both hosts answer `http://` with a 301 or 308 to the
   canonical `https://` URL and nothing else, and the canonical host sends `Strict-Transport-Security`;
   or the maintainer explicitly accepts the risk of a stripped first visit without HSTS, recorded in the
   ADR and stated on the verification page (Open question 2). Depends on 3.

## Decisions and open questions

### Decided by the maintainer (2026-11-19)

- **Decided: the assessment runs in the browser, compiled to WebAssembly from the existing Go code.** No
  inventory ever leaves the user's browser: no backend, no upload, no API call with inventory data. The
  site does not use `shift2eu-server`.
- **Decided: plain HTML and CSS, with as little JavaScript as possible.** No framework, no npm, no
  JavaScript build tool or bundler; the only build is Go's. Go's `wasm_exec.js` is the expected exception,
  taken from the building toolchain (Acceptance criteria, *Trust*).
- **Decided: the same repository.** The site's code, pages, tests and deploy live in
  `timothymamo/shift2eu` and go through the same CI, architecture rules and release flow.
- **Decided: the first version's scope** is as in *Scope*: paste, upload or an example; targets and
  region; the report in the page with estimates; Markdown and JSON downloads identical to the CLI's; a
  link to the inventory guide. Accounts, saved or shared reports, live source adapters, any server side
  and analytics are out of scope.
- **Decided: hosting on Scaleway Object Storage behind Scaleway Edge Services, Starter plan,** deployed
  from this repository's GitHub Actions as part of the release (`release.yml`, which runs on the tag the
  release PR's merge creates), never from a third-party builder.
- **Decided: canonical host `shift2eu.com`;** `www.shift2eu.com` redirects to it permanently. *See Open
  question 1:* Scaleway's documentation says Edge Services cannot serve an apex domain.
- **Decided: the domain `shift2eu.com`, registered and DNS-hosted at Namecheap.** The maintainer sets the
  records the runbook lists: the Edge Services CNAME, CAA restricting certificate issuance, DNSSEC on.
- **Decided: Go's standard toolchain, not TinyGo.** One compiler keeps the downloads identical to the
  CLI's, `encoding/json` and reflection are fully supported, and no second toolchain trails Go's
  releases. The module is served compressed and cached per release; a test enforces a compressed-size
  budget, and TinyGo is revisited only if the module exceeds it.
- **Decided: testing.** The logic lives in plain Go functions tested natively by `make test` (no network,
  no browser); the `syscall/js` glue stays thin. Static checks in Go tests cover the CSP, the SRI values
  and the links. One small headless-browser smoke test in Go with `chromedp` against a pinned Chromium
  runs outside `make test`: its own make target, a CI job, and before each release.

### Decided in this PRD (the maintainer can overrule them)

- **The report in the page is HTML rendered in Go by a new reporter, `htmlreport`,** with
  `html/template`; the downloads are the existing reporters' bytes. Contextual auto-escaping covers
  hostile inventory values, Trusted Types confine the one insertion, and the CSP's ban on inline script is
  a further layer. Rendering Markdown in JavaScript would need a parser the project does not want to ship,
  and building tables in JavaScript would move logic out of tested Go. Rendering is a reporter's job, so
  it is an adapter, not part of `internal/web`.
- **The CSP is a layer, not the guarantee.** It cannot govern navigation, WebRTC or DNS prefetch, and
  its path matching ignores the query. "Nothing leaves the browser" rests on the code (layer 1, with a
  static rule against every sending API), with the CSP refusing what it can and the attested bundle
  making the code checkable; the privacy page says so.
- **The deploy is gated on `SITE_DEPLOY_ENABLED` and on `latest`.** Releases between milestones 3 and 4
  skip it visibly instead of failing, and no re-run or older line rolls the site back.
- **No cache purge in the deploy.** Content-addressed assets never change, and the HTML pages expire within
  minutes, so the deploy key needs no Edge Services permission, which Scaleway's IAM grants only for the
  whole product (`EdgeServicesFullAccess`,
  [permission sets](https://www.scaleway.com/en/docs/iam/policies-permissions/permission-sets/)).
- **The deploy keeps earlier releases' assets.** A page of the previous release still finds its module
  while caches turn over, and the key needs no delete permission. Pruning them is a follow-up (proposed
  in the report, not filed).
- **The deploy program is built in a job without secrets** and runs in the deploy job from a digest-checked
  artifact, so no toolchain download or third-party action runs beside the key.

### Open questions (numbered as in the report to the maintainer)

1. **The apex domain on Edge Services.** Scaleway's documentation says an Edge Services endpoint can be
   customized only with a subdomain, never an apex domain ("It is **not** possible to use only a root
   domain", [How to configure a custom domain](https://www.scaleway.com/en/docs/edge-services/how-to/configure-custom-domain/)),
   and its pricing example gives the Starter plan one pipeline
   ([Understanding Edge Services pricing](https://www.scaleway.com/en/docs/edge-services/reference-content/understanding-pricing/)).
   So the decided canonical host, `shift2eu.com`, cannot be served by Edge Services as documented, and the
   documentation says nothing of serving two hosts on one pipeline or of redirects. *Recommendation:*
   ask Scaleway support whether the Starter plan can serve the apex (for example with an ALIAS or
   flattened record at Namecheap) and redirect `www`. If not, make `www.shift2eu.com` canonical on Edge
   Services and redirect the apex to it permanently, but only with a redirect that serves HTTPS with a
   valid certificate and can send HSTS (see 2 and 3 and milestone 4's go/no-go). Milestones 1 to 3 do not
   depend on the answer: the canonical host is one value of the build and the deploy; milestone 4 does.
2. **Security headers at Edge Services.** The documentation lists caching, WAF and a custom domain, and no
   way to set response headers ([Edge Services FAQ](https://www.scaleway.com/en/docs/edge-services/faq/));
   Object Storage lets an object carry `Cache-Control` and `x-amz-meta-*` headers only
   ([Object Storage FAQ](https://www.scaleway.com/en/docs/object-storage/faq/)). To be checked by the
   maintainer: whether Edge Services can add `Strict-Transport-Security`, `Content-Security-Policy` (for
   `frame-ancestors`), `X-Content-Type-Options`, `Referrer-Policy` and `Permissions-Policy`.
   *Recommendation:* the CSP is in a `<meta>` element in any case (it travels with the pages, to the
   bucket's direct URL too), and the script refuses to run framed. HSTS has no substitute: without it, a
   network attacker can strip HTTPS on a user's first visit and serve a look-alike page that sends the
   inventory away. Treat it as milestone 4's go/no-go: HSTS on the canonical host (then submit the domain
   to the HSTS preload list), or the maintainer's recorded acceptance of the risk.
3. **The redirect between the two hosts** depends on 1. To be checked by the maintainer: whether Edge
   Services or Namecheap can answer the second host with a permanent (301 or 308) redirect over HTTPS.
   *Recommendation:* a 301 from Edge Services if it can redirect; otherwise Namecheap's URL redirect
   record only if it serves HTTPS with a valid certificate for the host; decide together with 1.
4. **Compression and content types.** To be checked by the maintainer: whether Edge Services compresses
   responses itself, and whether an object uploaded with `Content-Encoding: gzip` and `Content-Type:
   application/wasm` is served with those headers (the Object Storage FAQ names only `Cache-Control` and
   `x-amz-meta-*` as headers that can be changed). *Recommendation:* upload the module pre-compressed with
   `Content-Encoding: gzip` if it is served so; otherwise serve it uncompressed, hold the uncompressed size
   to a budget too, and reconsider TinyGo only if that budget fails.
5. **Scoping the deploy key to one bucket.** Object Storage is not among the products that support IAM
   resource-level conditions ([Products supporting resource-level conditions](https://www.scaleway.com/en/docs/iam/policies-permissions/supported-products-resource-level/)),
   so an IAM policy cannot name one bucket. *Recommendation:* a Scaleway project that holds only the
   site's bucket; an IAM application whose policy grants `ObjectStorageObjectsWrite` and
   `ObjectStorageObjectsRead` ([permission sets](https://www.scaleway.com/en/docs/iam/policies-permissions/permission-sets/))
   in that project only; and a bucket policy, version `2023-04-17`, which allows only what it lists ("Deny
   statements are therefore useless",
   [bucket policies](https://www.scaleway.com/en/docs/object-storage/api-cli/bucket-policy/)), that allows
   the application `s3:PutObject` and `s3:GetObject` (and `s3:ListBucket` only if the deploy needs it) on
   that bucket and nothing else, with public `s3:GetObject` for the site. To be checked by the maintainer:
   that this policy refuses the application `DeleteObject`, ACL and bucket configuration calls even if
   `ObjectStorageObjectsWrite` would allow them, the actions Edge Services' bucket-website backend needs,
   and whether bucket versioning works with a website bucket. A leaked key can still overwrite the pages;
   versioning, the key's expiry and the release's live check limit that.
6. **Pinning Chromium for the smoke test.** *Recommendation:* the `chromedp/headless-shell` container
   image by digest, with a `# renovate: datasource=docker` comment, run by the CI job and in the Lima VM's
   container runtime (ADR 0017); `chromedp` connects to it on loopback. The alternative, a Chromium
   download pinned by version and checksum, needs its own update path.
7. **Where `chromedp` lives.** It is the first test-only third-party dependency that drives a browser.
   *Recommendation:* in the main module, in `test/browser` behind a build tag (`browser`), with a depguard
   rule that confines it there and a test that the product binaries do not reach it or its dependencies;
   not a nested module, which ADR 0012 names as a way past the architecture checks.
8. **The deploy program.** *Recommendation:* a standard-library Go program under `ci/` (like the AI
   reviewer, ADR 0033) that signs S3 requests itself, tested against a fake object store; not the AWS CLI
   or `s3cmd`, which are unpinned on GitHub's runners or need a Python install, and would hold the key.
9. **Edge Services Starter plan's price and limits** are not in the saved pages (no pricing page was
   saved; the docs' figures are examples "subject to change"). To be checked by the maintainer on
   [Scaleway's pricing page](https://www.scaleway.com/en/pricing/network/#edge-services) before
   milestone 4: the plan's monthly price, pipelines and cache egress. *Recommendation:* go ahead if the
   Starter plan covers one pipeline and the site's expected egress (a module of a few MiB, cached per
   release); if 1 needs a second pipeline, weigh the extra pipeline's fee against the Professional plan.
10. **CAA and the certificate authority.** The documentation says Edge Services generates managed
    certificates with Let's Encrypt ([SSL/TLS certificates](https://www.scaleway.com/en/docs/edge-services/reference-content/ssl-tls-certificate/)).
    *Recommendation:* CAA `0 issue "letsencrypt.org"` and `0 issuewild ";"` (and an `iodef` address if the
    maintainer wants reports); the maintainer checks that Scaleway's managed issuance passes with it, and
    that Namecheap supports CAA and DNSSEC for the domain. DNSSEC covers Namecheap's records only; the
    CNAME's target zone is Scaleway's.
11. **The bucket's direct URL.** The bucket website endpoint also serves the files, outside Edge
    Services, on its own origin. *Recommendation:* accept it (the content is public and the `<meta>` CSP
    travels with the pages), and keep the objects private behind Edge Services if bucket website allows
    it, as its quickstart suggests ([Edge Services quickstart](https://www.scaleway.com/en/docs/edge-services/quickstart/)).

### Proposed follow-ups (not filed)

- A scheduled workflow that repeats the live manifest check daily, so a change to the live site between
  releases is noticed.
- Pruning earlier releases' assets from the bucket, by hand or with a separate key.

### Sources

Scaleway's documentation was read from the public `scaleway/docs-content` repository at commit
`e10b284732eceefcaf55a5bdebdb20641f370fb5` (2026-11-19): Edge Services
([quickstart](https://www.scaleway.com/en/docs/edge-services/quickstart/),
[custom domain](https://www.scaleway.com/en/docs/edge-services/how-to/configure-custom-domain/),
[cache](https://www.scaleway.com/en/docs/edge-services/how-to/configure-cache/),
[certificates](https://www.scaleway.com/en/docs/edge-services/reference-content/ssl-tls-certificate/),
[pricing](https://www.scaleway.com/en/docs/edge-services/reference-content/understanding-pricing/),
[FAQ](https://www.scaleway.com/en/docs/edge-services/faq/)), IAM
([permission sets](https://www.scaleway.com/en/docs/iam/policies-permissions/permission-sets/),
[resource-level conditions](https://www.scaleway.com/en/docs/iam/policies-permissions/understanding-resource-level-conditions/),
[supported products](https://www.scaleway.com/en/docs/iam/policies-permissions/supported-products-resource-level/))
and Object Storage
([bucket policies](https://www.scaleway.com/en/docs/object-storage/api-cli/bucket-policy/),
[bucket website](https://www.scaleway.com/en/docs/object-storage/how-to/use-bucket-website/),
[FAQ](https://www.scaleway.com/en/docs/object-storage/faq/)).
