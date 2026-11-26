# PRD #297: shift2eu in the browser at www.shift2eu.com

**Status:** Complete (milestones 1 to 4 done; the first deploy was v0.16.1, 2026-11-25). Every
acceptance criterion is met; the runbook's key-limit step (R7) is recorded in ADR 0042, with one probe
skipped (a bucket in a third project) and named there as a gap for its next run.
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

`https://www.shift2eu.com`, the canonical host, serves a static site from this repository, released with
shift2eu; the apex `shift2eu.com` redirects to it permanently over HTTPS. A user pastes
or uploads an inventory document (schema version 1 or 2), or starts from a bundled example, chooses one
or more targets (OVHcloud, Scaleway, UpCloud) and optionally a region, and sees the assessment or the
comparison in the page, with estimates and their caveats. They can download the report as Markdown and as
the versioned JSON report, byte for byte what `shift2eu assess` prints for the same inventory and options.

The assessment runs client-side, compiled to WebAssembly from the same Go packages as the CLI. There is no
backend. The trust model has three layers:

1. **The code.** The page's code is reviewed Go plus three scripts, each held a stated way: the page
   script, small and reviewed, is scanned by a static rule that bans every browser API that can send data
   (fetch, navigation, WebSocket, WebRTC, beacons, workers, cross-origin messaging) and every computed
   property name that could hide one, as the Go glue is; Go's `wasm_exec.js` is pinned byte for byte to the building
   toolchain's; and the module script is pinned to one fixed template that carries only the encoded
   module.
2. **The browser.** A strict Content Security Policy refuses network connections, third-party resources,
   inline script and forms, so a bug or an injection that slips past the first layer is still refused for
   every channel the CSP governs. Its known gaps (navigation, WebRTC, DNS prefetch, and messaging to
   another window: `postMessage`, `opener`, `parent`, `top`, `BroadcastChannel`, `MessageChannel`,
   `window.name`) are documented and closed by the first layer.
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
| Site | Static pages (HTML, CSS, one small script, the WebAssembly module embedded in a content-addressed script and Go's `wasm_exec.js`), a privacy page, built by Go's toolchain only and deployed by the release. | Accounts, saved or shared reports, any server-side code, analytics, a JavaScript framework, npm or any JavaScript build tool. The HTTP API (`shift2eu-server`): the site never calls it. |
| Hosting | Scaleway Object Storage behind Scaleway Edge Services (Starter plan, one pipeline) serving the canonical host `www.shift2eu.com`, the domain `shift2eu.com` registered and DNS-hosted at Namecheap with the apex redirecting to `www`, deployed from this repository's GitHub Actions only. *Amended 2026-11-24 (Hosting decision 1):* a Scaleway Serverless Container running Caddy serves both `www` and the apex in front of the pipeline, writing the redirects and the headers. | Any third-party builder or hosting (Netlify, Vercel, Cloudflare Pages, GitHub Pages). |

## Acceptance criteria

All criteria are pinned by tests that run offline under `make test`, except those marked *browser* (the
headless-browser smoke test, `make test-browser` and its CI job), *release* (checked by `release.yml` on a
release), *live* (checked against the live site) or *review*.

### Assessment in the browser

- [x] *Browser:* given a bundled example inventory (`examples/aws`, `examples/azure`, `examples/gcp`), when
  the user picks it and runs the assessment with one target, then the page shows the assessment: the
  summary, every resource with its fit, best offering, blockers and required changes, the workloads, and
  the estimates.
- [x] *Browser:* given an inventory pasted into the text field or chosen with the file input (schema
  version 1 or 2), when the user selects several targets or all of them, then the page shows the
  comparison, as `shift2eu assess --target all` reports it.
- [x] *Browser:* given a region for a target, the report is scoped to that region as `--region
  <target>=<region>` scopes it; given the estimate switch turned off, the report has no estimates, as
  with `--no-estimate`.
- [x] Given the same inventory and options, the Markdown and JSON downloads are byte-identical to the
  output of `shift2eu assess --format markdown` and `--format json` of the same commit. Pinned natively by
  a parity test over every example inventory and the hostile fixtures, for one target, several and `all`,
  with and without a region and estimates; and in the browser by the smoke test for one example
  (*browser*). Any difference fails the test.
- [x] An inventory the reader refuses (malformed JSON, an unknown schema version, an unknown field, a
  document over the reader's size cap) shows one error message in the page, by the rule the HTTP API
  applies: it names a position and field and never echoes a value from the inventory (the in-memory mode
  of `inventoryfile.FromBytes`, PRD #103). A bad target or region choice shows the CLI's usage error. The
  page stays usable after an error.
- [x] The option rules (target keys, `all`, duplicates, regions per target, the estimate switch) are the
  CLI's: a test runs one table of option sets through the CLI and the web adapter and expects the same
  accepted options and the same errors.
- [x] Every string the page shows from an inventory or a catalog is escaped: a hostile inventory (quotes,
  angle brackets, `<script>`, control characters, invalid UTF-8) renders as text, pinned by a hostile
  golden of the HTML reporter and, in the browser, by the smoke test finding no element, attribute or
  script the inventory tried to inject.

### Trust: nothing leaves the browser

**Layer 1, the code.**

- [x] A static test fails when the page's script or the `syscall/js` glue names a browser API that can
  send data or load a resource: `fetch` (with no exception: the module is not fetched, *Layer 2*),
  `XMLHttpRequest`, `WebSocket`, `EventSource`, `sendBeacon`, `RTCPeerConnection`, `Worker`, `SharedWorker`,
  `importScripts`, `open`, `location` (assignment or navigation methods), assignment to `href`, `src`,
  `action`, `search`, `pathname`, `hash`, `host`, `hostname`, `port`, `protocol`, `username` or
  `password` (on any object: a `location` or an `<a>` navigates or changes its target through each),
  `setAttribute` or `setAttributeNS` with `href`, `src` or `action`, `click()` and `dispatchEvent` (each
  outside the one download function below), `document.cookie`, `localStorage`, `sessionStorage`,
  `indexedDB` and `navigator.serviceWorker` (outside the one unregister function below), and the
  messaging APIs that reach another window or browsing context, which the CSP does not govern
  (*Decided by the orchestrator in review*): `postMessage` (on any object: a window, a frame, a port, a
  worker), `opener`, `parent` and `top` (each outside the framing guard's two uses below),
  `BroadcastChannel`, `MessageChannel` and assignment to `name` (on any object: `window.name` survives
  a navigation to another origin), and any `message` listener (`addEventListener("message", ...)` or
  `onmessage`, on any object), so a message another window posts in is never read. Since a name
  scan cannot see a computed name, the test also fails on any computed-key member access in the page
  script, read or assignment (`x[expr]`, `x[expr] = ...`), and on `Object.assign`, `Reflect.set`,
  `Reflect.get`, `Object.defineProperty` and `Object.defineProperties`, with no exception: the script
  decodes the module with `atob` and `Uint8Array.from`, which needs none. Three uses are allowed, each in
  one named function of the page script that the test names and confines them to: the **framing
  guard** compares `window.top !== window.self` once (its only use of `top`, and no use of `parent`)
  and sets `window.opener = null` on load (its only use of `opener`; defence in depth behind the
  `opener` and `postMessage` ban: it drops only this page's own handle to its opener, and since the
  site cannot send `Cross-Origin-Opener-Policy` the opener keeps its handle to this page, with which
  it can navigate the tab or post messages in but read nothing); the **download
  function** creates a `blob:` URL with `URL.createObjectURL` from the report bytes and sets it as the
  `href` of an `<a download>` it clicks (a `blob:` URL is same-document and sends nothing), then revokes
  it; and the **unregister function** calls `navigator.serviceWorker.getRegistrations()` and
  `unregister()` on each, on load (*Decided by the orchestrator in review*; it registers nothing). The
  test fails when any of these APIs appears outside its function, when the framing guard's `top` or
  `opener` use differs from the two forms above, or when the functions do anything else that the scan
  bans. The glue's allow list of constant names holds none of the messaging names above (`postMessage`,
  `opener`, `parent`, `top`, `BroadcastChannel`, `MessageChannel`, `name`, `onmessage`, and
  `addEventListener` with `message`), and the test fails when one is added. The glue calls `js.Global().Get`, `Value.Get`, `Value.Set`, `Value.Delete` and
  `Value.Call` only with constant names from a named allow list, and `Value.New` and `Value.Invoke`,
  which take no name, only on a value it obtained through such a constant name; a forbidigo or test
  rule refuses a computed name and a `New` or `Invoke` on any other value.
- [x] The glue is the only Go code that can reach the browser: the dependency test (`go list -deps` with
  `GOOS=js GOARCH=wasm` on `./cmd/shift2eu-web`, *Architecture*) also fails when any package in the
  module's dependency tree other than `cmd/shift2eu-web` imports `syscall/js`, except the standard
  library's own runtime and syscall internals, which the test names in an explicit list (a new standard
  package that imports it fails the test until it is reviewed and added). So no domain or adapter package,
  and no third-party dependency, can call `fetch` or any other sending API through `js.Global()`, and
  `net/http`'s fetch transport stays out of the tree (the same test refuses `net/http`).
- [x] Go's `wasm_exec.js` is trusted as toolchain code, not scanned: the banned-API scan covers the page's
  script and the glue only, and `wasm_exec.js` is pinned instead by the toolchain test (*`wasm_exec.js`*
  below: byte-identical to the building toolchain's, which `go.mod` pins). The module script is not
  scanned either: it is pinned to its fixed template (*Layer 2*), so it carries no code of its own.
  Milestone 1's ADR and the privacy page's technical note say so.
- [x] Every page has `<meta http-equiv="x-dns-prefetch-control" content="off">` and no `<link rel>` of
  `dns-prefetch`, `preconnect`, `prefetch` or `prerender` (static test).
- [x] The page script, which every page loads (*Layer 2*, the scripts per page), stops, and shows why,
  when the page is framed (`window.top !== window.self`), since a `<meta>` CSP cannot carry
  `frame-ancestors`, and sets `window.opener = null` on load as defence in depth behind the `opener`
  and `postMessage` ban (static test of the script and *browser*: opened by another page with
  `window.open`, the page's own `window.opener` reference is `null` after load). This drops only the
  page's own handle: the site cannot send `Cross-Origin-Opener-Policy`, so the opener keeps its handle
  to the page, with which it can navigate the tab or post messages in, but read nothing (and a posted
  message is never read, since the page adds no `message` listener, *Layer 1*). No criterion or
  statement claims the assignment closes that handle.

**Layer 2, the browser.**

- [x] Every page carries a Content Security Policy in a `<meta http-equiv>` element, the first child of
  `<head>` (before any `<link>` or `<script>`), with at least: `default-src 'none'`; `script-src 'self'
  'wasm-unsafe-eval'` and nothing else (no `'unsafe-inline'`, no `'unsafe-eval'`, no other host);
  `style-src 'self'`; `img-src 'none'`; `connect-src 'none'` from the first byte; `form-action 'none'`;
  `base-uri 'none'`; `object-src 'none'`; `worker-src 'none'`; `child-src 'none'`; `frame-src 'none'`;
  `manifest-src 'none'`; `require-trusted-types-for 'script'` and `trusted-types` naming
  the one policy that inserts the Go-rendered report; and no `report-uri` or `report-to` (violation
  reports carry blocked URLs and samples). A static test parses each built page's policy and fails on a
  missing, widened or added directive, and on a `connect-src`, `img-src`, `worker-src`, `child-src`,
  `frame-src` or `manifest-src` other than exactly `'none'`. `worker-src` must be set (*Decided by the
  orchestrator in review*): without it, CSP Level 3 falls back from `worker-src` to `child-src` to
  `script-src`, so `script-src 'self'` would allow a same-origin dedicated or shared worker and a
  service-worker registration, and a worker loaded from a same-origin URL takes its CSP from its own
  response headers, which this site cannot send (Hosting decision 2), so its `fetch` would escape
  `connect-src 'none'`. `child-src`, `frame-src` and `manifest-src` already fall back to `default-src
  'none'`; they are pinned explicitly so no later directive can widen them by fallback. `worker-src 'none'`
  does not hinder the unregister function: `getRegistrations()` and `unregister()` load no script, and
  the CSP governs only the fetch of a worker's script.
  *Amended 2026-11-24 (Hosting decision 2 as amended, ADR 0045):* once the container's front is live,
  `www` sends the policy as a header on every response, so a same-origin worker's script there gets the
  header CSP; on other origins it still gets none. `worker-src 'none'` stays right, and the reasoning
  above now holds for those other origins.
  Where the host can also send the policy as a header (Hosting decision 2), the header adds
  `frame-ancestors 'none'`.
  - [x] Milestone 2: the `<meta>` policy of every built page and its static test
    (`test/site`'s `TestContentSecurityPolicyIsExact` and `TestContentSecurityPolicyCheckRejects`).
  - [x] Milestone 3: the policy as a header with `frame-ancestors 'none'`, where the host can send it.
    *Amended 2026-11-24:* the host can (Hosting decision 2 as amended); the header comes from the
    container's server configuration, in milestone 4 (ADR 0045). *Done in milestone 4:*
    `infra/site/Caddyfile` sends it, `test/sitefront` pins it, and `www` served it exactly once per
    response after v0.16.1's deploy (2026-11-25).
- [x] The site loads no image (*Decided by the orchestrator in review*). `img-src 'none'`, not `'self'`:
  an own-origin image GET carries data in its path or query to the host's logs, the same channel for
  which `connect-src 'self'` is rejected, and a CSP host-source that pins exact paths needs a host,
  which breaks the run-anywhere bundle. Any icon or logo is inline SVG markup in the HTML, which loads
  nothing; no `<img>`, `<picture>`, `<input type="image">`, `<link rel="icon">` or other image link, and
  no `data:` image either (`img-src 'none'` refuses it). A static test of every built page and of the
  HTML reporter's goldens (the hostile golden included) fails on an `<img>`, a `<picture>`, an
  `<input type="image">`, a `<link rel="icon">`, any SVG element with an external `href` or
  `xlink:href` (`<image>`, `<use>`, `<feImage>`), and any `data:` URL. Fonts and media fall to `default-src 'none'`
  (no `font-src` or `media-src` directive is set, and the static test fails if one is added), so the
  site uses the browser's system fonts. CSS `url()` loads are governed by the directive of what they
  load (`img-src` for images and `background-image`, `font-src` for `@font-face`), so the stylesheet,
  loaded under `style-src 'self'`, references no `url()` and no `@import`; a static test of the built
  stylesheet fails on either. The site ships no favicon. A browser may request `/favicon.ico` on its own,
  outside the page's CSP; that request is a fixed URL sent by the browser, not the page, and carries no
  data from the page, and since the bundle has no such file the host answers it with an error.
- [x] The module is never fetched (*Decided by the orchestrator in review*). `make site` emits the
  module's bytes, base64-encoded, in a content-addressed script (`module.<sha>.js`), which the page loads
  with `<script src integrity="sha384-...">` under `script-src 'self'`; the page script decodes the bytes
  and calls `WebAssembly.instantiate` (allowed by `'wasm-unsafe-eval'`), never `instantiateStreaming` or
  `compileStreaming`. Every reference in the built files is a same-origin relative path, so the bundle
  runs unchanged on any origin, a local static file server included (*Layer 3*); the static test fails
  on an absolute URL to a site file.
- [x] The module script's content is constrained, not only its digest. A static test fails unless
  `module.<sha>.js` is exactly one fixed template, `globalThis.__shift2euModule="<base64>";` followed by
  one newline, with nothing but the standard base64 alphabet (`A-Z`, `a-z`, `0-9`, `+`, `/`, trailing
  `=`) between the quotes, and unless that base64 decodes to the built module byte for byte. So the
  module script can carry no code, only the module.
- [x] The assessment page loads exactly three scripts, all classic scripts with `defer`, never `async`
  or `type="module"`, in this order: `wasm_exec.js`, the module script, the page script. `defer` runs
  them in document order after parsing, so the global is set before the page script reads it. The
  privacy page and the verification page load exactly one script, the page script (classic, `defer`),
  and never `wasm_exec.js` or the module script: there the page script runs only the framing guard and
  the unregister function, and reads no module, since it reads the module only on the page whose
  `<body>` carries the fixed attribute `data-page="assessment"`. A static test of every built page pins,
  per page, the script elements, their attributes and their order: the three on the assessment page,
  the page script alone on the privacy and verification pages. The `<noscript>` message, and the static
  loading notice the HTML shows, are in the HTML itself, so no script delays them; the no-WebAssembly
  message is shown by the page script before it reads the module.
- [x] The page script reads `globalThis.__shift2euModule` once, before any report fragment is inserted,
  checks that `typeof` it is `"string"` (and otherwise stops with an error), and deletes the global; a
  static test of the script and the *browser* smoke test (the global is gone after load) pin it. The HTML
  reporter never emits an `id` or `name` attribute derived from an inventory or catalog value
  (any anchor it needs is a fixed or counter-based name with a fixed prefix, such as `r-1`), so no report
  element can shadow that global or any name the glue reads through `js.Global().Get` (DOM named
  access). The hostile golden carries an inventory whose names equal those globals and the glue's
  allow-listed names, and shows no `id` or `name` with them.
- [x] `require-trusted-types-for` is not enforced in every browser (browsers without Trusted Types ignore
  it). Milestone 2's ADR and the privacy page's technical note say so, and say that in such a browser the
  one insertion of the report rests on the HTML reporter's escaping (a closed set of constant elements,
  values only as element content, escaped with `html/template`'s text-context replacements, pinned by a
  differential test against `html/template` and by the reporter's hostile golden; ADR 0035, pending the
  maintainer's confirmation, *Decided by the orchestrator in review*) and on the CSP's ban on inline
  script, not on Trusted Types. No statement claims Trusted Types protect every user.
- [x] *Browser:* after loading, the smoke test attempts, from the page, a `fetch` and an
  `XMLHttpRequest` to another origin, to an own-origin path, and to the module script's own URL, and
  records every request made while the page is open, on the `httptest` server that serves the bundle
  and in the page's DevTools Network events. Every attempt must be blocked by `connect-src 'none'`, and
  the recorded requests must be only the site's own files, loaded as the page, its stylesheet and its
  scripts. The one request tolerated beyond those files is a browser-initiated `GET /favicon.ico` with
  no query string, which the server answers 404 (the site ships no favicon); any other request, a
  `/favicon.ico` with a query or of another method included, fails the test.
  It also attempts, from the page, an image load (an `Image` with its `src` set and an inserted `<img>`)
  and a CSS `url()` load (a `background-image` in a style rule constructed by the test through CSSOM),
  each to an own-origin path and to another origin; every one must be blocked by `img-src 'none'`, and
  none may appear in the recorded requests.
  It also attempts a dedicated `Worker`, a `SharedWorker` and a
  `navigator.serviceWorker.register()`, each with a same-origin script URL (served by the test's
  `httptest` server, which the bundle does not contain) and the `Worker` also from a `blob:` URL. `new Worker(url)`,
  `new SharedWorker(url)` and `register(url)` are Trusted Types `TrustedScriptURL` sinks, so on the built page a
  string URL throws a Trusted Types `TypeError` before any `worker-src` check, and a test there would
  pass with `worker-src` deleted. The four attempts therefore run on a test-only copy of the page whose
  policy is the built one with only `trusted-types` widened by one test policy name, every other
  directive byte-identical (the test compares them), and the test mints each `TrustedScriptURL` through
  that policy. Each of the four must be observed blocked as a `worker-src` violation: a
  `securitypolicyviolation` event or a DevTools log entry whose `effectiveDirective` is `worker-src`,
  with no Trusted Types error for that attempt. The bundle's origin is a secure context (`127.0.0.1` or
  `localhost`), and the test asserts `navigator.serviceWorker` is defined before the `register()`
  attempt, so the attempt cannot pass vacuously. No worker may run, and no worker script may appear in
  the recorded requests.
  The smoke test serves the bundle from an `httptest` origin and passes there unchanged, which shows the
  bundle runs on an origin other than `www.shift2eu.com`; the release's smoke test does the same with the
  attested bundle.
- [x] *Browser:* the smoke test records that a navigation or an `RTCPeerConnection` started from the page
  is **not** blocked by the CSP, and the ADR documents both as gaps that layer 1 closes, so no criterion
  or statement claims the CSP covers them. The same holds for messaging to another window or browsing
  context (`postMessage`, `opener`, `parent`, `top`, `BroadcastChannel`, `MessageChannel`, `window.name`):
  the CSP governs none of them, milestone 2's ADR and the privacy page's technical note list them as
  gaps that layer 1 closes (its banned-API scan, `message` listener ban included, with the framing
  guard's `window.opener = null` as defence in depth), and list as a remaining gap, not a closed one,
  that the opener keeps a handle to the page (no COOP): it can navigate the tab or post messages in,
  read nothing. The smoke test records that the page's own `window.opener` reference is `null` after
  load when another page opened it.
- [x] Every script, the module script included, and the stylesheet are loaded with Subresource
  Integrity, SHA-384: the `<script>` and `<link rel="stylesheet">` elements carry `integrity`. A static
  test computes the SHA-384 of every built file and fails when an `integrity` value is missing or does
  not match.
- [x] The page has no inline script, no inline `style` attribute or `<style>` element (with `style-src
  'self'` the CSP refuses every one), and no inline event handler: a static test of every built page,
  and the HTML reporter's goldens, hostile golden included, contain none.
- [x] No page, stylesheet or script references another origin as a loaded resource (no fonts, images,
  scripts or styles from a third party); links to other sites are plain `<a href>` navigations. A static
  test lists every URL in the built files and fails on a loaded resource that is not the site's own.
- [x] *Live:* no response from the site carries `Set-Cookie`. *Checked live by the maintainer* before
  the first deploy (runbook R8) and after v0.16.1's (2026-11-25): no `Set-Cookie` on `www`, the apex
  or a redirect. The automated live checks do not read `Set-Cookie`. The Caddyfile sets none, and
  `test/sitefront` holds what it sends.

**Privacy statement.**

- [x] A privacy page, linked from every page, says what the site is built to do and what checks it: the
  assessment runs in the browser and the page's code sends nothing (layer 1, with its static test); the
  browser refuses connections and third-party resources (layer 2, with its CSP test and smoke test); the
  deployed files are the attested ones (layer 3); no cookies, storage, analytics or third-party resources.
  It also says what the site cannot promise: the hosting provider (Scaleway) sees the usual request
  metadata of any web server (IP address, user agent, the files requested) and nothing of the inventory,
  and the maintainer reads its aggregate request counts (no IP address, no user agent, no cookie,
  kept 31 days; milestone 4, ADR 0039's *Amendment: log retention*, pinned by `test/site`'s
  `TestPrivacyPageNamesTheAggregateRequestCounts`); browser extensions can read any page; a released page served from a poisoned cache can stay in a
  browser's own cache for up to its `max-age` (300 seconds) after the site is repaired, but a page
  uploaded with a leaked deploy key sets its own caching (up to a year) and can register a same-origin
  service worker, and both can persist in a visitor's browser after the repair, since the site cannot
  send `Clear-Site-Data` (Hosting decision 5; the page script unregisters any service worker when a
  genuine page loads, which helps only visitors who load one); the live checks compare only the files
  in the attested manifests, `/` and a few well-known service-worker paths, so a look-alike page
  uploaded at a new path with a leaked key is not detected by them (*Layer 3*); a user who must not
  trust the host can run the attested bundle locally (layer 3), which runs unchanged on any origin.
  *Review:* each statement names the tested criterion that backs it, and none rests on review alone.
- [x] The privacy page states the first-visit downgrade risk next to its "nothing leaves the browser"
  statement, not only on the verification page, until `shift2eu.com` is on the HSTS preload list in
  shipping browsers: HSTS on `www.shift2eu.com` protects only a browser that has already visited it over
  HTTPS, so on a first visit, or a visit that starts at `http://shift2eu.com` (answered by the apex
  redirect's provider, which may send no HSTS), a network attacker can strip HTTPS and serve a look-alike
  page that sends the inventory away; the way to avoid it is to type `https://www.shift2eu.com` or run the
  attested bundle locally. The statement carries a stable marker (the element id `downgrade-risk`) and is
  shown by default: a static test of the built privacy page fails when the marker is missing, hidden or
  empty. *Hidden* means any of: a `hidden` or `aria-hidden` attribute on the element or an ancestor; a
  closed `<details>` ancestor; a stylesheet rule matching the element or an ancestor that sets
  `display: none`, `visibility: hidden`, a zero width or height, or a position off-screen. *Browser:* the
  smoke test also asserts on the rendered privacy page that `#downgrade-risk` is visible:
  `checkVisibility()` returns `true` and its bounding box has a non-zero width and height. Only a change
  made after preload is live in shipping browsers may remove it, and that change retires the static test
  in the same commit; the live check (*Layer 3* and milestone 4) refuses the removal before then.
  - [x] Milestone 2: the statement, its static test (`test/site`'s `TestDowngradeRiskIsShown`) and the
    browser test (`test/browser`'s `TestPrivacyAndVerificationPages`).
  - [x] Milestone 4: the live check that refuses the statement's removal before preload is live
    (`KindStatement`, in the release's check since milestone 3, passed after v0.16.1's deploy, and in the
    scheduled check, `.github/workflows/site-check.yml`).
- [x] If milestone 4's HSTS outcome is recorded as `waived` (*Layer 3*, the HSTS record
  `site/records/hsts.json`), the privacy page and the site's verification page also carry a statement
  with the marker `id="hsts-waived"`, shown by default, saying that without HSTS the downgrade risk also
  covers returning visitors, not only a first visit. A static test reads the HSTS record and fails when
  the outcome is `waived` and either page's marker is missing, hidden (as above) or empty; *browser:*
  when the outcome is `waived`, the smoke test asserts `#hsts-waived` visible on both pages as it does
  `#downgrade-risk`; the live check enforces it on the deployed pages. The record does not exist before
  milestone 4: the static and browser tests treat a missing record as "not waived" (and require
  `downgrade-risk` regardless, as above), while the release's live check and the scheduled one fail
  safe on a missing record (*Layer 3*).
  - [x] Milestone 2: the static test (`test/site`'s `TestHSTSStatementFollowsTheRecord`) and the browser
    test (`test/browser`'s `TestPrivacyAndVerificationPages`), a missing record read as not waived.
  - [x] Milestone 3: the release's live check (`ci/sitecheck/internal/livecheck`, `KindStatement`)
    enforces `hsts-waived` on both deployed pages when the record says waived and `downgrade-risk` on the
    privacy page unless the site counts as preloaded, and fails safe on a missing or unreadable record.
  - [x] Milestone 4: the scheduled live check doing the same (`.github/workflows/site-check.yml` runs
    the same `ci/sitecheck` against the live release's manifest and the records at the live release's
    tag, ADR 0043's *Amendment: the scheduled check*).
  - [x] Milestone 4: the record itself: `site/records/hsts.json` (`met`, preload not pursued) and
    `site/records/preload.json` (not listed, not submitted), 2026-11-25 (ADR 0042, *The HSTS and preload
    records*).
- [x] The privacy page names the apex redirect's provider (chosen in milestone 4), says what it sees of a
  visitor who enters through `shift2eu.com` (IP address, user agent, the requested path and the
  `Referer`), and that it only serves the redirect and never the site or the inventory. *Review* at
  milestone 4, when the provider is known. *Amended 2026-11-24:* the provider is Scaleway, whose
  Serverless Container serves `www` too (Hosting decision 3 as amended), so the page names no new
  party; it says that Scaleway also answers the apex, and that for `www` the same container passes the
  site's files through from Edge Services.

**`wasm_exec.js`.**

- [x] Go's `wasm_exec.js` is taken at build time from the Go toolchain that builds the module
  (`$(go env GOROOT)/lib/wasm/wasm_exec.js`), never committed and never fetched. The page shows the Go
  version the module reports (`runtime.Version()`), and a test fails when the bundled `wasm_exec.js`
  differs from the building toolchain's. The toolchain is pinned by `go.mod`, which Renovate updates, and
  every CI and release job that builds the site sets up Go from `go.mod` (`go-version-file`).

### Layer 3: users can check what they run

- [x] *Release:* every release publishes the site bundle as the release asset
  `shift2eu-site_<version>.tar.gz`, listed in the release's checksums file and carrying build provenance
  attested by `release.yml` like the archives (ADR 0015), with a manifest (`manifest.json` in the bundle)
  of every file's path and SHA-256.
- [x] The bundle is reproducible: building the site twice from the same commit with the pinned toolchain
  gives byte-identical files and archive (a test builds it twice and compares). The build uses
  `-trimpath`, a fixed or no VCS stamp, sorted archive entries with fixed times, owners and modes, and
  compressed files with zero mtime and a fixed level.
- [x] *Release:* after the deploy, `release.yml` fetches every file of the manifest from the canonical
  host (`https://www.shift2eu.com`), with no query string, and fails when one does not match its SHA-256
  after content decoding. The live check runs only when the deploy ran: whenever the deploy is skipped
  (`SITE_DEPLOY_ENABLED` unset or not `true`, or the tag not `latest`), the live check is skipped too,
  with the same visible notice, and `check.ReleaseWorkflow` pins that it has the deploy's conditions. A
  3xx where a manifest file is expected (for instance an object carrying
  `x-amz-website-redirect-location`) counts as a mismatch; the check follows no redirect for a manifest
  file. It retries for a stated bound at least as long as the HTML's cache lifetime, and records which
  host it checked and the `Age` of each response. The same check also asserts, and fails on a mismatch:
  - the root: `https://www.shift2eu.com/` answers with the bytes of the manifest's `index.html`;
  - no service worker: a few well-known worker paths (at least `/sw.js`, `/service-worker.js`,
    `/serviceworker.js` and `/worker.js`, none of them a site file) do not answer 200 (Hosting
    decision 5);
  - the HTML's caching: every HTML page answers with `Cache-Control` whose `max-age` is 300 or less;
  - the redirects: `http://shift2eu.com`, `https://shift2eu.com` and `http://www.shift2eu.com` each
    answer with a 301 or 308 to `https://www.shift2eu.com` with the same path and no other hop, except the
    preload chain of Hosting decision 2; where milestone 4 accepted a refused or closed plain-HTTP `www`
    instead (Hosting decision 3), `http://www.shift2eu.com` must refuse or close. *Refuse or close*
    means the TCP connection is refused, or closed without an HTTP response; a non-2xx response with no
    site content (such as 403 or 426) is accepted only if its body carries no site file, which the check
    asserts by comparing the body with every file of the manifest. Neither `http://shift2eu.com` nor
    `http://www.shift2eu.com` ever answers 200;
  - the certificates of the apex and of `www.shift2eu.com`: each valid for its host, and not expiring
    within a stated margin (at least 14 days);
  - HSTS against the recorded HSTS state. Milestone 4 records the HSTS outcome (`met` or `waived`) and
    whether preload is pursued in the HSTS record, `site/records/hsts.json`, and whether the domain is
    on the preload list in shipping browsers in the preload record, `site/records/preload.json` (both
    in the repository, never in the bundle; milestone 3's ADR refers to them and fixes their format),
    and the check reads them at the commit it runs from, failing safe when either is missing or
    unreadable. If `met`, the check fails whenever `https://www.shift2eu.com/` sends no
    `Strict-Transport-Security` or one weaker than the minimum (`max-age` at least 31536000 and
    `includeSubDomains`, plus `preload` when preload is pursued), whatever the deployed pages say; the
    apex's header is held to the same minimum, with `preload`, only when preload is pursued, and is
    otherwise only recorded. If `waived`, it fails when the deployed privacy page or verification page
    lacks the `hsts-waived` statement (*Privacy statement*). In either state it fails when the deployed
    privacy page lacks the `downgrade-risk` statement while the domain is not on the preload list in
    shipping browsers; without a preload record saying it is, the check assumes it is not, so it fails
    safe.
- [x] *Live:* a scheduled workflow repeats that check (the manifest including the HTML, `/` against
  `index.html`, the service-worker paths, the HTML's `Cache-Control`, the redirects, both certificates'
  validity and expiry, and HSTS against the recorded state) at least daily. Its expected version is the
  highest release tag on `main` (the deploy's `latest` rule, *Deploy*). The live version, read from the
  live page, is untrusted text: it must match `^v[0-9]+\.[0-9]+\.[0-9]+$` and equal a tag in the
  repository's own tag list, and a missing, unparseable or unknown version fails the check. Every
  command after that (`gh attestation verify --source-ref refs/tags/<tag>`, the download of the
  release's bundle) takes the tag name from the repository's tag list, never the page's text. A live
  version higher than the highest release tag on `main` always fails. A live version lower than the
  expected one fails, except within a window after the expected release: then only the expected release
  or the release immediately before it is accepted, and anything else fails. The window is anchored at
  the committer date of the commit the expected tag points to (readable with `contents: read`; the job
  gets no `actions: read`), and has a fixed bound covering the delay from that commit to the end of the
  release run: at least release-please's delay from the merge to the tag (the `timeout-minutes` of
  `release-please.yml`'s `release-please` and `tag` jobs, 15 + 15 today) plus the release workflow's
  timeout (the `timeout-minutes` along `release.yml`'s longest chain of jobs up to the live check;
  `release.yml` sets none today, so milestone 3 sets one on every job and `check.ReleaseWorkflow` pins
  them) plus the HTML's `max-age`. Time a run spends queued is not bounded by any timeout, so a release
  queued past the bound fails the check once, which the alert reports like any failure (milestone 4
  sets the number with a margin; the workflow test pins it). Whichever version is live, the check
  verifies that release's bundle attestation and compares the live files with that release's own
  manifest; it never skips the file check. It verifies each attestation with `gh attestation verify`
  and the workflow's `GITHUB_TOKEN` only, with the same flags as the deploy and the guide:
  `--signer-workflow` naming `release.yml`, `--source-ref refs/tags/<tag>` and
  `--deny-self-hosted-runners`. A page changed between releases (a leaked key, a changed bucket or Edge
  Services setting) is noticed within a day, not at the next release. The workflow has two jobs: the
  checking job, with `contents: read` (and `attestations: read`) only and no secret, whose checkout sets
  `persist-credentials: false`, which runs no action that is not pinned by commit SHA and fails on any
  mismatch; and an alert job, `if: failure()` (*Amended (milestone 4):* `if: ${{ always() &&
  needs.check.result != 'success' }}`, since a check past its timeout concludes `cancelled`, where
  `failure()` is false; ADR 0043, *Amendment: the scheduled check*) and `needs` the checking job, with `issues: write` only,
  which opens one issue with a fixed label, assigned to the maintainer, or, when one is open, adds a
  comment to it on each new failure (editing an issue notifies no one). The issue's title, body and
  comments are fixed text plus the failed run's URL only: no `${{ needs.*.outputs.* }}`, no site content
  and no other value from the checking job in the alert job's `run:` steps or the issue text. The issue
  is the alert because GitHub notifies a failed scheduled run only to whoever last changed its cron line
  (here the agents App), and disables a schedule after 60 days without repository activity; the runbook
  says how the maintainer re-enables a disabled schedule. A workflow test, like `check.ReleaseWorkflow`,
  pins the schedule, both jobs' permissions (no `actions: read`), the alert job's `if` and its fixed
  text, the checkout's `persist-credentials: false`, the attestation flags, the window's anchor and
  bound, and the live version's handling (the pattern, the tag-list match, the commands built from the
  listed tag, and a failure on a missing, unparseable or unknown version). The check's program,
  shared by the release's check and the scheduled one, is CI tooling under `ci/`, standard library only,
  with depguard rules of its own, like `ci/aireview` (ADR 0033).
  *Done in milestone 4:* `.github/workflows/site-check.yml` runs daily, the window's bound is 180
  minutes, pinned by `check.SiteCheckWindow` against `check.MinSiteCheckWindow` (165 minutes today),
  and `check.SiteCheckWorkflow` holds the workflow (ADR 0043, *Amendment: the scheduled check*).
- [x] The checks compare only what the manifests name, `/`, and the service-worker paths: a path added
  outside every release's manifest (such as a look-alike page at a new path, uploaded with a leaked key)
  is not detected by them. Milestone 3's hosting ADR
  ([ADR 0042](../../docs/adr/0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md))
  and the privacy page say so. Milestone 3 checks
  whether the deploy job's key can hold `ListBucket` on the site's bucket (*To be checked by the
  maintainer*); if it can, the deploy program lists the bucket before it uploads and fails on any key
  that is in no release's manifest, so such a path is found at the next release (the scheduled check
  holds no secret and does not list).
  *Decided 2026-11-25/26 (ADR 0042):* the maintainer keeps `-list-bucket` off, so the deploy does not
  list the bucket. ADR 0042 and the privacy page state the residual.
- [x] The release verification guide (`docs/guide/verify-releases.md`) documents the same check as
  commands a user can run (`gh attestation verify` on the bundle with `--signer-workflow`,
  `--source-ref refs/tags/<tag>` and `--deny-self-hosted-runners`, then fetching each file of its manifest
  from the live site and comparing digests); that the page shows its version so the user knows which
  release to check; that the check is one point in time from one vantage point, since the HTML is the root
  of trust and nothing pins it; that a released page from a poisoned cache can stay in a browser's cache
  for up to its `max-age` after the site is repaired, while a page uploaded with a leaked key sets its
  own caching (up to a year) and can register a service worker, both of which can outlast the repair in
  a visitor's browser (Hosting decision 5), and that a path outside the manifests is not checked; and
  how to run the attested bundle locally with any static file server, which it supports unchanged. It
  also carries the first-visit downgrade advice with the exact URL `https://www.shift2eu.com`, to type
  rather than follow from an `http://` link; served by GitHub over HTTPS, it reaches users outside the
  page an attacker could replace. A docs test pins the advice and the URL in the guide. The site's
  verification page, linked from every page, summarises the guide and links to the guide's path
  (`docs/guide/verify-releases.md` on GitHub at the release's tag, or `main` when untagged), which
  milestone 2's static test pins; milestone 3's docs test pins that the page's summary and the guide
  agree (the same steps, flags and URL).

### Deploy

- [x] *Release:* `release.yml` gains a site deploy job that runs after the release's `verify` and
  `verify-archives` jobs have passed, and only when the `latest` job says this tag is the highest release
  tag on `main` (as the images' `latest` tag), so a re-run or an older line's patch release never rolls
  the site back. `check.ReleaseWorkflow` pins both conditions.
- [x] The deploy job runs only when the repository variable `SITE_DEPLOY_ENABLED` is `true`; otherwise it
  is skipped with a visible notice in the run's summary. The maintainer sets it at the end of the runbook
  (milestone 4), so releases between milestones 3 and 4 do not fail, and only once both the HSTS record
  and the preload record are merged to `main` and included in a release tag (the release's live check
  reads them at the tag it runs from, and fails safe without them). `check.ReleaseWorkflow` pins the
  condition, and the `release-tag` skill's post-release check reports the skip as expected while the
  variable is unset.
- [x] The deploy job does not build. A job without secrets builds the deploy program; the deploy job takes
  it and the bundle of this run as artifacts, checks both digests against those this run produced, and
  verifies the bundle's provenance with `gh attestation verify` naming `release.yml` as the signer
  workflow, the run's tag as the source ref and `--deny-self-hosted-runners`. It runs no third-party
  action after the key is in scope; it checks out nothing, or with `persist-credentials: false`; its
  permissions are `contents: read` and `attestations: read`; and the key's two secrets are the `env:` of
  the one step that uploads, never of the job.
- [x] The deploy program's rollback guard trusts the live page's version only when it is a real release:
  before the upload it reads the live page once and refuses to deploy only when the embedded version is
  a release tag on `main` (from the repository's own tag list, as the `latest` job reads it) higher
  than the bundle's and the live HTML matches that tag's manifest. Otherwise it overwrites, with a
  visible warning in the run's summary: when the live version is not a known tag, cannot be parsed, or
  names a tag whose manifest the live HTML does not match (a page a leaked key could have written). So
  a forged higher version cannot block the redeploy that repairs the site (Hosting decision 5). The
  fake-object-store test (below) pins both cases: the refusal for a genuine higher release, and the
  overwrite with its warning for each untrusted case.
- [x] The `site` environment holds the Scaleway API key as the environment secrets `SCW_ACCESS_KEY` and
  `SCW_SECRET_KEY`, never at repository level or in another environment. Its deployment policy is exactly
  one custom policy of type `tag`, named `v*.*.*`; the `v*` tag rulesets that the `release-tag` skill
  already checks are what let only the release-tag App create such a tag (ADR 0021).
- [x] The key belongs to a Scaleway IAM application that can write objects **only to the site's bucket**:
  no other bucket, project or product, and no bucket-configuration, bucket-policy or ACL permission
  (runbook and Hosting decision 5). The bucket policy allows that application only the object writes and
  reads the deploy needs. The key has an expiry date, and the runbook has a rotation step. *Review:* the
  runbook's policies are checked against Scaleway's permission sets.
  *Met (R7, 2026-11-24, ADR 0042's R7 table):* every bucket-configuration, policy, ACL and object probe
  but `PutObject` and `ListObjectsV2` answered 403. The probe to a bucket
  in a third project was skipped (no bucket given); the state bucket's `put-object`, a write to the
  admin project's bucket, was denied, so a write outside the site's project was shown refused for that
  project. A re-run with a third project's bucket closes that gap.
- [x] The `release-tag` skill's step 2 checks the `site` environment: `custom_branch_policies` `true` and
  `protected_branches` `false`; `deployment-branch-policies` lists exactly `tag v*.*.*` (type `tag`, so a
  branch named like a tag is not allowed); its secrets are `SCW_ACCESS_KEY` and `SCW_SECRET_KEY`; and
  neither name is listed at repository level. A docs test pins that the skill names the environment, the
  policy and both secrets.
- [x] A failed deploy fails the release run visibly and leaves the previous release's site serving:
  content-addressed assets are uploaded first, the HTML pages last, and no earlier release's file is
  deleted. A test of the deploy program against a fake object store (`httptest`) pins the order, that a
  failure before the HTML upload changes no page, and that the program never logs the key.
- [x] The deploy program is CI tooling under `ci/` (beside the AI reviewer, ADR 0033), standard library
  only, held by strict depguard rules of its own added to the pin. CLAUDE.md's CI tooling row and
  `docs/architecture.md` name it and say that the release runs it. Every new tool or action is pinned,
  with a `# renovate:` comment where Renovate does not find it (CLAUDE.md rule 7).

### Page content

- [x] Every estimate shown carries the catalog snapshot's date and the caveat that prices are list prices
  from that date, not a quote, as the Markdown report words it.
- [x] Every page shows the shift2eu version that built it, and links to the repository and to the
  inventory guide (`docs/guide/inventory.md` on GitHub at that release's tag). Built from an untagged
  commit (pull request CI, `make site`), the page shows `internal/version`'s development value, as
  `shift2eu version` does, and the guide link points to `main`.
- [x] Without JavaScript, a `<noscript>` message says the assessment needs JavaScript and WebAssembly and
  points to the CLI (static test); without WebAssembly support, the script shows the same message
  (*browser*, with WebAssembly disabled). The privacy page, the verification page and the links work
  without either: those two pages load neither `wasm_exec.js` nor the module script (*Layer 2*).

### Size and speed

- [x] The module is built with Go's standard toolchain (`GOOS=js GOARCH=wasm`), not TinyGo.
- [x] A test fails when the gzip-compressed module script (the transferred file: base64 is about 4/3 of
  the raw module, and gzip recovers part of that) exceeds the budget: **3 MiB**, recorded with the
  measured sizes of the raw module and of the compressed module script in milestone 1's ADR. Above it,
  TinyGo is reconsidered (Decisions). The module is compiled from bytes, not streamed (*Layer 2*), so
  the HTML shows a static loading notice until the module is instantiated. The notice carries a stable
  marker (the element id `loading-notice`); a static test of the built assessment page fails when it is
  missing, empty or hidden (*Hidden* as the *Privacy statement* defines it). *Browser:* the notice is
  replaced after instantiation.
- [x] The module script, the page script, `wasm_exec.js` and the stylesheet are served under
  content-addressed names with `Cache-Control: public, max-age=31536000, immutable`; the HTML pages with
  `max-age=300` or less, so a release shows within minutes without a cache purge. Scaleway's
  documentation says an object's caching directive takes precedence over the pipeline's cache lifetime
  ([cache](https://www.scaleway.com/en/docs/edge-services/how-to/configure-cache/)); *live:* the release's
  site check and the scheduled one read these headers and the `Age` of the HTML, and fail on an HTML
  `max-age` above 300 (*Layer 3*).
- [x] *Live:* the module script is served with `Content-Type: text/javascript` and, if Scaleway serves
  it so (Hosting decision 4), `Content-Encoding: gzip`. If it cannot, a second test also holds the
  uncompressed module script to a budget set in milestone 3's ADR.
- [x] *Browser:* once loaded, the page runs an assessment with the network disabled.

### Accessibility

- [x] Every control has a label; the whole flow (choose an input, choose targets and region, run, read,
  download) works with the keyboard alone; focus is visible; text and controls meet WCAG 2.2 AA contrast
  (a test checks the stylesheet's colour pairs).
- [x] The report is semantic HTML (headings, tables with header cells, lists) rendered in Go by the HTML
  reporter, not tables assembled in JavaScript: a closed set of constant elements, values only as element
  content, escaped with `html/template`'s text-context replacements, pinned by a differential test
  against `html/template` (ADR 0035; pending the maintainer's confirmation, *Decided by the orchestrator
  in review*). Errors are announced to
  assistive technology (`role="alert"`). *Browser:* the smoke test checks the labels and the tab order of
  the main flow.

### Architecture

- [x] The report in the page is rendered by a new reporter adapter, `internal/adapter/report/htmlreport`,
  which writes an HTML fragment for an assessment and a comparison through the same reporter ports as
  `markdown` and `jsonreport`: a closed set of constant elements, values only as element content,
  escaped with `html/template`'s text-context replacements, pinned by a differential test against
  `html/template` (ADR 0035; pending the maintainer's confirmation, *Decided by the orchestrator in
  review*). It is held by
  `reporters-stay-offline` and named in CLAUDE.md's adapters row; its goldens, hostile golden included,
  are in its own `testdata/`.
- [x] The logic the browser calls is a new driving adapter, `internal/web`, beside `internal/cli` and
  `internal/httpapi`. It takes the inventory bytes and the options, runs the use cases and returns the
  report bytes for each download and the HTML fragment, or an error. It imports only the application, the
  domain, `internal/version` and a named list of standard packages (no `os`, no `net`, no `syscall/js`),
  and gets every adapter through its `Deps`.
- [x] The adapters are wired in `cmd/shift2eu-web/internal/wire`, an untagged package that builds and
  tests natively: it constructs what `cmd/shift2eu`'s offline path constructs (the inventory file reader,
  the three target catalogs, the Markdown, JSON and HTML reporters) into `internal/web`'s `Deps`. Its
  black-box tests, run by `make test`, hold the option-parity table and the download parity test: they run
  `internal/cli` and `internal/web` over the same deps and compare. `cmd/shift2eu-web` itself (build
  constraint `js && wasm`) holds only the `syscall/js` glue and imports `wire`, `internal/web`,
  `syscall/js` and a named list of standard packages; never `internal/cli`, `internal/httpapi`, a request
  decoder or a cloud SDK.
- [x] New depguard rules, written like their siblings and added to the depguard pin
  (`TestDepguardRulesArePinned`): `web-gets-adapters-from-cmd` (the strict allow list of `internal/web`),
  `web-imported-only-by-the-web-root` (only `cmd/shift2eu-web` and its `wire` package import it),
  `web-composition-root` (the strict allow list of the glue, `cmd/shift2eu-web`),
  `web-composition-root-wire` (the strict allow list of `wire`), `web-composition-root-wire-tests` (the
  strict allow list of `wire`'s test files, with the parity tests' extra imports, `internal/cli` among
  them), `web-root-tests-never-run-the-cli` (every other test file under `cmd/shift2eu-web` is refused
  `internal/cli`, so only `wire`'s test files import it) and `syscall-js-only-in-the-web-root` (no package
  outside `cmd/shift2eu-web` imports `syscall/js`, tests included). The existing rules are extended so that neither `internal/cli`, `internal/httpapi`, `cmd/shift2eu` nor `cmd/shift2eu-server`
  imports `internal/web`, `htmlreport` aside from the composition roots that wire it.
- [x] `make lint` and CI's lint matrix also run golangci-lint with `GOOS=js GOARCH=wasm`, and
  `test/architecture` checks `js/wasm` as an explicit extra target beside those it reads from
  `.goreleaser.yaml` (ADR 0012); a test fails when `js/wasm` drops out of either list. Without this, the
  `js && wasm` files would never be linted or checked.
- [x] A test under `make test` runs `go list -deps` with `GOOS=js GOARCH=wasm` on `./cmd/shift2eu-web` and
  fails on `net`, `net/http`, `os/exec` or a cloud SDK in its dependency tree.
- [x] CLAUDE.md's layer table names `internal/web` among the driving adapters, `cmd/shift2eu-web` among
  the composition roots and `htmlreport` among the adapters, and `docs/architecture.md` describes them.
- [x] *Review:* the use cases, the target adapters, the inventory reader and the Markdown and JSON
  reporters are reused unchanged; the CLI's and the HTTP API's output do not change. Option parsing that
  the CLI and the web share moves out of `internal/cli` only into a layer both may import (the
  application layer), with the CLI's output unchanged. *Reviewed:* the shared option rules are in
  `internal/app/analysis` (`options.go`, `choices.go`), and `internal/web` and `internal/cli` import
  only it, the domain and `internal/version`. Wire's parity tests pin the CLI's output, and the HTTP API
  is retired (ADR 0037).

### Testing and tooling

- [x] `make test` runs every native test of this PRD with no network and no browser.
- [x] `make site` builds the bundle into a git-ignored directory. `make test-browser` runs the smoke test,
  written in Go with `chromedp` (no npm, no Playwright or Puppeteer) against the headless Chromium of a
  container image pinned by digest with a `# renovate:` comment, whose DevTools port is published on
  `127.0.0.1` only; it serves the built bundle from a local `httptest` server.
- [x] `chromedp` and its dependencies are confined by depguard to the smoke test's package, and a test
  pins that no product binary's dependency tree (`cmd/shift2eu`, `cmd/shift2eu-server`,
  `cmd/shift2eu-web`) reaches `chromedp` or any package only it brings in.
- [x] CI runs the smoke test as its own job on every pull request and on `main`, with `contents: read`
  only. The release runs it on the bundle it attests, before the deploy, in a job that holds neither the
  attestation's `id-token` nor the Scaleway key.
  - [x] Milestone 2: CI's `browser` job (`Browser smoke test` in `ci.yml`), on every pull request and push
    to `main`, with `contents: read` only.
  - [x] Milestone 3: the release's run on the bundle it attests, before the deploy.

### Runbook

- [x] A one-time runbook for the maintainer, written in milestone 3 and followed in milestone 4
  ([docs/site-runbook.md](../../docs/site-runbook.md), [ADR 0039](../../docs/adr/0039-create-the-sites-scaleway-hosting-with-opentofu.md), issue #422).
  **OpenTofu creates** the Scaleway side from `infra/site/`: the project that holds only the site's
  bucket, the bucket with versioning, its website setting and its bucket policy, the IAM application and
  its policy, the Edge Services Starter plan, its one pipeline, its cache and `www.shift2eu.com` with a
  managed Let's Encrypt certificate. The maintainer runs `init`, `plan` and `apply` on mac-mini-home (the
  admin machine, `docs/dev-environment.md`) with their own Scaleway credentials, through `dev/tofu.sh`
  from a checkout of the repository there; agents never do. **By hand**, because no secret may reach the
  state or because the provider cannot: the state bucket (R1); the deploy API key with an expiry, its
  rotation (R6, R11) and the canary check of its limits (R7); the origin and plain-HTTP checks (R5); the
  Namecheap records (the `www` CNAME to the Edge Services endpoint, the apex's permanent redirect to
  `https://www.shift2eu.com` by the means chosen in milestone 4, CAA allowing the certificate
  authorities of Edge Services and, if it issues its own certificate, the apex redirect's provider,
  DNSSEC on, R8; *amended 2026-11-24, Hosting decisions 1 and 10:* the `www` CNAME and an apex ALIAS
  both to the Serverless Container's endpoint, CAA unchanged, and OpenTofu also creates the container
  and its two custom domains, ADR 0045); the `site` GitHub environment, its deployment policy, secrets and variables
  `SITE_BUCKET` and `SITE_REGION` (R9); the HSTS record
  (`site/records/hsts.json`) and the preload record (`site/records/preload.json`) committed, merged to
  `main` and included in a release tag (the outcome is read from `www.shift2eu.com`'s headers before the
  first deploy; the records PR is a `docs` change, which cuts no release by itself, so its squash commit
  on `main` carries a `Release-As: <next version>` footer, the version picked by the maintainer when
  approving and written by them into the squash commit's message when merging, and release-please then
  opens the release PR whose tag contains the records) and, last, only after that tag exists,
  `SITE_DEPLOY_ENABLED` (R10). The teardown removes the DNS record before OpenTofu deletes the pipeline or
  the bucket, so no dangling CNAME can be taken over (R13). Every step names what the `release-tag`
  checks or the live check verify afterwards. The recovery steps (R12): a redeploy of the last good
  release (re-running the deploy job of the `release.yml` run of the highest release tag on `main`, which
  overwrites a live page whose version is not a genuine higher release, *Deploy*; the job takes its
  programs from that run's artifacts, kept 30 days, GitHub's re-run window, so the re-run works only
  within 30 days of the run; after that the last good release is restored by the version rollback, or a
  new release is cut with a `Release-As:` footer) or a version rollback with the maintainer's own key of
  each of the four pages, `LICENSE`, `NOTICE` and `manifest.json`, either followed by a manual Edge
  Services cache purge by the maintainer (Hosting decision 5); and how to re-enable the scheduled live
  check's workflow if GitHub has disabled its schedule (*Layer 3*).

  The runbook is in [docs/site-runbook.md](../../docs/site-runbook.md): steps R0 to R15, each a command
  block with its `PASS` and `FAIL` lines (R15, the scheduled check's label and re-enabling, added in
  milestone 4).
  - [x] Milestone 3: written ([docs/site-runbook.md](../../docs/site-runbook.md)).
  - [x] Milestone 4: followed, for the first deploy (2026-11-24 to 26), R7 included (below). R15 runs
    once this workflow is on `main`, as the step itself says.
- [x] The runbook has a step (R7, [docs/site-runbook.md](../../docs/site-runbook.md)), run once the key exists, that verifies the key's limits rather than only
  reviewing them. With the deploy key, against a canary object under a prefix the site never serves,
  every write action in Scaleway's bucket-policy action list
  ([bucket policies](https://www.scaleway.com/en/docs/object-storage/api-cli/bucket-policy/)) and every
  bucket call that IAM alone governs, except `PutObject`, must answer `403 AccessDenied`: at least
  `DeleteObject`, `DeleteObjectVersion`, `DeleteObjectTagging`, `DeleteObjectVersionTagging`,
  `PutObjectTagging`, `PutObjectVersionTagging`, `PutObjectAcl`, `PutObjectRetention`,
  `PutObjectLegalHold`, `AbortMultipartUpload`, `RestoreObject`, `PutBucketAcl`, `PutBucketCORS`,
  `PutBucketTagging`, `PutBucketWebsite`, `DeleteBucketWebsite`, `PutBucketVersioning`,
  `PutLifecycleConfiguration`, `PutBucketObjectLockConfiguration`, `PutEncryptionConfiguration`,
  `PutBucketPolicy`, `DeleteBucketPolicy` and `DeleteBucket`. `PutBucketPolicy`, `DeleteBucketPolicy`
  and `DeleteBucket` are not in the bucket-policy action list, so IAM alone governs them, and the
  project-scoped object permission sets must not grant them. Only `403 AccessDenied` counts as a pass;
  any other answer (another 4xx, a 5xx, a 2xx, a timeout) is recorded in the hosting ADR as
  inconclusive, with the probe, the answer and the reason, and fails the step until it is explained.
  The probes are built to do no harm if one unexpectedly succeeds:
  - Before the probes, the runbook saves the site bucket's current website configuration, versioning
    state, ACL and bucket policy with the maintainer's own credentials, and gives the commands that
    restore each.
  - Only a probe that re-submits one of the site bucket's existing configurations unchanged runs against
    the site's bucket: `PutBucketWebsite`, `PutBucketVersioning` (only if versioning is on, re-submitting
    `Enabled`), `PutBucketAcl` and `PutBucketPolicy` (each re-submitting the saved configuration). Every
    other bucket-level probe runs against a temporary canary bucket in the same project, created by the
    maintainer with versioning on, a website configuration and a bucket policy of the same shape as the
    site bucket's (the deploy application allowed `s3:PutObject` and `s3:GetObject`, plus
    `s3:ListBucket` if granted, and the same read grant), and deleted by the maintainer afterwards: the
    destructive `DeleteBucketWebsite`, `DeleteBucketPolicy` and `DeleteBucket`; suspending versioning;
    and every probe that sets a configuration the site bucket does not have, which no re-submission can
    make harmless there (`PutBucketObjectLockConfiguration`, since enabling object lock cannot be
    undone; `PutBucketCORS`, since a CORS rule applies to the whole bucket and cannot be scoped to a
    prefix; `PutLifecycleConfiguration`, `PutBucketTagging` and `PutEncryptionConfiguration`; and
    `PutBucketVersioning` if the site bucket has no versioning). On the canary bucket each submits a
    minimal valid configuration: an object-lock configuration with no default retention, one CORS rule,
    one lifecycle rule, one tag, the default encryption. Should one unexpectedly succeed, only the canary
    bucket is changed; with no default retention and no locked object, it can still be deleted.
  - Object probes target only the canary object in the site's bucket: `PutObjectAcl` sets it private,
    tagging and retention probes use an empty tag set or no retention, `AbortMultipartUpload` names an
    upload ID that does not exist, and `RestoreObject` and the object delete probes name only the
    canary object.

  A negative probe outside the project must also fail: a `PutObject` to a bucket in another project of
  the maintainer, and `ListBuckets`. If versioning is on, the key must not be able to delete an object
  version (on the canary object) or suspend versioning (on the canary bucket). The maintainer removes
  the canary object and the canary bucket with their own credentials afterwards. Milestone 3 writes the
  step; the maintainer runs it in milestone 4 and again after every key rotation and every change to the IAM or bucket policy, and its result is
  recorded in milestone 3's hosting ADR (amended in milestone 4).
  - [x] Milestone 3: the step written (R7).
  - [x] Milestone 4: run (2026-11-24), its result in ADR 0042's R7 table: `PutObject` and
    `ListObjectsV2` allowed (`-list-bucket` kept off), every other probe 403, the probe to a third
    project's bucket skipped (the state bucket's denied `put-object` is the outside-the-project write
    that ran), and the state lock the closing plan left (issue #537), force-unlocked.

## Milestones

Each milestone ships as one PR (milestone 4 as two: its HSTS and preload records first, released
through a `Release-As` footer, then the rest after the first deploy), in this order, and each gets the
security audit (`security-audit` skill), since every one of them touches the trust model or the release.

1. [x] **The WebAssembly adapter and its ADR.** `internal/web`, the HTML reporter `htmlreport`,
   `cmd/shift2eu-web` (the thin `syscall/js` glue) and its `wire` package, with native tests: the
   option-parity table, the download parity with the CLI over every example and the hostile fixtures, the
   HTML reporter's goldens, the module size budget, the offline dependency test for `js/wasm`. The
   depguard rules and their pin, `js/wasm` in `make lint`, CI's lint matrix and `test/architecture`,
   CLAUDE.md's table and `docs/architecture.md`. An ADR records the WebAssembly driving adapter, its
   composition root and wiring package, the HTML reporter, the standard toolchain over TinyGo, the size
   budget and the measured size, how `wasm_exec.js` is tied to the toolchain and trusted as toolchain code
   rather than scanned, and that only `cmd/shift2eu-web` may import `syscall/js`.
2. [x] **The pages, their tests and the CSP.** The static pages under a new directory, `site/` (HTML,
   CSS, one small page script; `site/records/`, which milestone 4 fills, is never bundled), the privacy
   page and the verification page, `make site` (a Go program, standard library
   only, that assembles the bundle: content-addressed names, the module embedded in its script, SRI
   values, the version, the snapshot dates; as built, it also imports the three embedded-snapshot
   targets, `internal/domain/catalog` and `internal/version`, to take the snapshot dates, regions and
   version from where the CLI does rather than a copy that could drift, ADR 0036), the static tests (CSP, no image in any built page or HTML
   reporter golden, the hostile golden included (no `<img>`, `<picture>`, `<input type="image">`,
   `<link rel="icon">`, SVG `<image>`, `<use>` or `<feImage>` with an external `href` or `xlink:href`, or `data:` URL),
   no stylesheet `url()` or `@import`, SRI, banned APIs (cross-origin messaging included) and computed
   property names with the framing guard, download and unregister functions as their only exceptions, the glue's
   constant names, the module script's template, the scripts per page with their attributes and order,
   the module global read once and deleted, prefetch, the `downgrade-risk`, `hsts-waived` (a missing
   HSTS record read as not waived) and `loading-notice` markers, links resolve, the verification
   page's link to the guide's path, no third-party resource, contrast), the reproducibility test, and
   the `chromedp` smoke test with `make test-browser` and its CI job. `chromedp` is a new dependency with its reason in the commit
   message (CLAUDE.md rule 6). An ADR ([0036](../../docs/adr/0036-hold-the-site-to-three-trust-layers-and-test-them-in-a-headless-browser.md)) records the site's trust model (its three layers and how each of
   the three scripts is held, the CSP with `connect-src 'none'` and `img-src 'none'` (no image, no CSS
   `url()`, icons as inline SVG, no favicon, and the browser's own `/favicon.ico` request as a fixed URL
   that carries no page data), `worker-src 'none'` and why the fallback matters (without it CSP Level 3
   falls back to `child-src`, then to `script-src 'self'`, and a same-origin worker takes its CSP from its
   own response headers, which the site cannot send, so its `fetch` would escape `connect-src 'none'`),
   `child-src`, `frame-src` and `manifest-src` pinned to `'none'`, and its known gaps (navigation,
   WebRTC, DNS prefetch, and messaging to another window or browsing context: `postMessage`, `opener`,
   `parent`, `top`, `BroadcastChannel`, `MessageChannel`, `window.name`, closed by layer 1's scan, which
   also bans any `message` listener, with the framing guard's `window.opener = null` as defence in depth;
   and, as a remaining gap, the opener keeps a handle to the page (no COOP): it can navigate the tab or
   post messages in, read nothing), the module embedded
   in an SRI-pinned script of a fixed template rather than fetched, the script order, SRI, Trusted Types
   and the browsers that do not enforce `require-trusted-types-for`, no storage, the one download path,
   the service-worker unregister, how the report is inserted and why it carries no inventory-derived
   `id` or `name`) and the headless-browser test. Depends on 1.
3. [x] **Release, attestation and deploy.** The bundle built, tested in the browser, attested and
   published by `release.yml`; the deploy program and the `site` deploy job behind `SITE_DEPLOY_ENABLED`
   and `latest`; the live site check and its program under `ci/`; `check.ReleaseWorkflow` and the CI dry
   run; the `release-tag` skill's step 2 for the `site` environment and its post-release check;
   `docs/guide/verify-releases.md`; `infra/site/`, the OpenTofu configuration of the site's Scaleway
   hosting, with its offline checks (`make infra-check`; ADR 0039, issue #422); the runbook. An ADR
   ([ADR 0042](../../docs/adr/0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md))
   records the hosting, the deploy (environment, key
   scope, upload order, cache headers, the rollback guard that trusts only a genuine higher release,
   recovery by a redeploy of the last good release or a version rollback plus a manual cache purge,
   `timeout-minutes` on every `release.yml` job) and the bundle's attestation. It refers to the HSTS
   record `site/records/hsts.json` and the preload record `site/records/preload.json` and fixes their
   format (*Layer 3*). It also lists the origins that can serve the site, the bucket website endpoint
   `<bucket>.s3-website.<region>.scw.cloud`, the pipeline's own endpoint `<id>.svc.edge.scw.cloud` and
   `www.shift2eu.com`, says which of them actually serve it under the bucket's chosen read rule (Hosting
   decision 11) and whether each answers over plain HTTP (checked and recorded in this milestone), and
   says that only `www.shift2eu.com` is attested and checked (*Layer 3*); the leaked-key residuals (a
   page uploaded with a leaked key sets its own caching and can register a service worker, both of which
   can outlast the repair in visitors' browsers, and a path outside the manifests is not detected by the
   live checks; the mitigations: the unregister function, the service-worker probes and, if the key can
   hold `ListBucket`, the deploy's listing of the bucket); and the result of the runbook's key-limit
   step. Releases cut after this merge skip the deploy, and its live check, visibly until the variable
   is set. Depends on 2.
4. [x] **First deploy and the privacy statement live.** After the maintainer has followed the runbook and
   settled the items left to check for this milestone (*To be checked by the maintainer*: how the apex
   redirect is served over valid HTTPS, Edge Services' response headers and redirects, HSTS and preload;
   the first two answered on 2026-11-24 by Hosting decisions 1 to 3 as amended),
   and the go/no-go below is met: the HSTS record and the preload record merged to `main` and included
   in a release tag before `SITE_DEPLOY_ENABLED` is set (so this milestone's records land in a first
   PR, a `docs` change that cuts no release by itself, whose squash commit on `main` carries a
   `Release-As: <next version>` footer that the maintainer picks when approving and writes when merging,
   so release-please opens the release PR whose tag contains the records; the variable is set only
   after that tag exists; the PR that closes the milestone follows the first deploy); the first release
   that deploys, the live checks green (files match the manifest, `/`, no service worker, headers, no
   cookie, HTTPS, the redirects, both certificates and HSTS against the recorded state), the scheduled live check workflow (*Layer 3*) added and running at
   least daily, with the version window's bound set and pinned, any fix the
   live site needs, the runbook updated with the apex redirect means chosen, milestone 3's hosting ADR
   amended with that means, the apex redirect's provider (what it sees, its certificate authority if it
   issues its own certificate) and the HSTS and preload outcome or the waiver, the HSTS record (`met` or
   `waived`, and whether preload is pursued) committed in `site/records/hsts.json`, the privacy page
   naming the apex redirect's provider (*Privacy statement*), the README's link to the site with the
   first-visit downgrade advice and the exact URL `https://www.shift2eu.com` (pinned by the docs test,
   like the guide's), and this PRD moved to `prds/done/` (`Closes #297`).

   **Go/no-go, mandatory (never waived):** `http://shift2eu.com`, `https://shift2eu.com` and
   `http://www.shift2eu.com` each answer with a 301 or 308 (no other status) to exactly
   `https://www.shift2eu.com` with the same path, and no other hop, except the preload chain
   `http://shift2eu.com` -> `https://shift2eu.com` -> `https://www.shift2eu.com` when preload is pursued
   (Hosting decision 2); and the apex's HTTPS answer carries a valid certificate for `shift2eu.com`.
   *Amended 2026-11-24 (Hosting decisions 1 to 3 as amended):* the earlier allowance for a refused or
   closed plain-HTTP `www`, if Edge Services could not redirect, is dropped: the container's server
   redirects `http://www.shift2eu.com` with a 308 like the apex, and a refusal is no longer the
   expected answer. Neither `shift2eu.com` nor `www.shift2eu.com` ever serves the site over
   plain HTTP with a 200; for the other origins, milestone 3's ADR records whether they answer over
   plain HTTP.

   **Go/no-go, HSTS (met, or explicitly waived):** `https://www.shift2eu.com` sends
   `Strict-Transport-Security` with `max-age` of at least 31536000 and `includeSubDomains` (plus
   `preload`, and the apex the same, when preload is pursued; the apex's header is required only then);
   or the maintainer explicitly waives it, recorded in the ADR and in the HSTS record. Either way the
   privacy page states the first-visit risk until preload is live (*Privacy statement*); a waiver also
   means that risk covers returning visitors, and the verification page and the privacy page carry the
   `hsts-waived` statement. Pursuing preload is effectively irreversible: removal from the list takes
   months to reach shipping browsers, and `includeSubDomains` binds every future subdomain of
   `shift2eu.com` to HTTPS. The go/no-go names that, and the ADR lists the subdomains that exist when
   preload is submitted. *Amended 2026-11-24:* the header comes from the container's server
   configuration (Hosting decision 2 as amended), so `met` is reachable; a waiver stays possible but is
   no longer forced by the host. The container, its configuration and its tests (ADR 0045) are part of
   this milestone, in `infra/site`, before the first deploy. Depends on 3.

## Decisions

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
- **Decided: canonical host `www.shift2eu.com`;** the apex `shift2eu.com` redirects to
  `https://www.shift2eu.com` permanently (301 or 308) over HTTPS with a valid certificate. Edge Services
  serves only a subdomain: "You cannot customize your endpoint with a primary domain directly (e.g.
  `beautiful-domain.com`), only with a subdomain of it", and "It is **not** possible to use only a root
  domain [...] CNAME records [...] cannot by definition be created for root domains"
  ([How to configure a custom domain](https://www.scaleway.com/en/docs/edge-services/how-to/configure-custom-domain/),
  docs validated 2026-06-08, read 2026-11-19). This replaces the earlier decision of the apex as canonical.
  How the apex redirect is served over valid HTTPS (Namecheap's URL redirect or another means) is left to
  check for milestone 4 (*To be checked by the maintainer*); it does not block milestones 1 to 3, where the
  canonical host is one value of the build and the deploy. *Amended 2026-11-24:* a Serverless Container
  serves the apex redirect and `www` (Hosting decisions 1 to 3 as amended).
- **Decided: the domain `shift2eu.com`, registered and DNS-hosted at Namecheap.** The maintainer sets the
  records the runbook lists: the `www` CNAME to the Edge Services endpoint, the apex redirect, CAA
  restricting certificate issuance, DNSSEC on. *Amended 2026-11-24:* the `www` CNAME and an apex ALIAS
  point at the Serverless Container's endpoint (Hosting decision 10 as amended).
- **Decided: Go's standard toolchain, not TinyGo.** One compiler keeps the downloads identical to the
  CLI's, `encoding/json` and reflection are fully supported, and no second toolchain trails Go's
  releases. The module is served compressed and cached per release; a test enforces a compressed-size
  budget, and TinyGo is revisited only if the module exceeds it.
- **Decided: testing.** The logic lives in plain Go functions tested natively by `make test` (no network,
  no browser); the `syscall/js` glue stays thin. Static checks in Go tests cover the CSP, the SRI values
  and the links. One small headless-browser smoke test in Go with `chromedp` against a pinned Chromium
  runs outside `make test`: its own make target, a CI job, and before each release.

### Decided in this PRD (the maintainer can overrule them)

- **The report in the page is HTML rendered in Go by a new reporter, `htmlreport`;** the downloads are
  the existing reporters' bytes. The reporter writes a closed set of constant elements, values only as
  element content, escaped with `html/template`'s text-context replacements, pinned by a differential
  test against `html/template` (ADR 0035; amended from "with `html/template`", pending the maintainer's
  confirmation, *Decided by the orchestrator in review*). That escaping covers hostile inventory values,
  Trusted Types confine the one insertion, and the CSP's ban on inline script is
  a further layer. Rendering Markdown in JavaScript would need a parser the project does not want to ship,
  and building tables in JavaScript would move logic out of tested Go. Rendering is a reporter's job, so
  it is an adapter, not part of `internal/web`.
- **The CSP is a layer, not the guarantee.** It cannot govern navigation, WebRTC, DNS prefetch or
  messaging to another window or browsing context (`postMessage`, `opener`, `parent`, `top`,
  `BroadcastChannel`, `MessageChannel`, `window.name`).
  "Nothing leaves the browser" rests on the code (layer 1, with a static rule against every sending
  API, every navigation, every messaging API and every computed property name that could hide one), with
  the CSP refusing what it can and the attested bundle making the code checkable; the privacy page says
  so. What the CSP does govern it closes to the host as well as to third parties: `connect-src 'none'`,
  `img-src 'none'` and `worker-src 'none'`, since an own-origin request carries data in its path or query
  to the host's logs and a same-origin worker would run under its own response headers' CSP, not the
  page's; `child-src`, `frame-src` and `manifest-src` are pinned to `'none'` as well. The one
  own-origin request it leaves is the browser's own `/favicon.ico` fetch, a fixed URL with no page data.
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

### Decided by the orchestrator in review (2026-11-19), for the maintainer to confirm

- **Decided: the module is embedded, never fetched; `connect-src 'none'` from the first byte.** A CSP
  source must name a host (CSP Level 3's `host-source`), so a `connect-src` that lets the page fetch its
  module names an origin, and the attested bundle could then not load its module locally, in
  `make test-browser`'s `httptest` origin, in the release's smoke test or on the other origins (Hosting
  decision 11). Instead `make site` emits the module's bytes, base64-encoded, in a content-addressed
  script (`module.<sha>.js`) loaded by `<script src integrity="sha384-...">` under the existing
  `script-src 'self'`, and the page script decodes the bytes and calls `WebAssembly.instantiate`
  (allowed by `'wasm-unsafe-eval'`). `fetch` is banned outright by layer 1, the bundle runs unchanged on
  any origin, a local one included, and no post-load policy or query-string residual is needed. The cost:
  the transferred file is larger (base64 is about 4/3 of the raw bytes; gzip recovers part of that, and
  the size budget is held on the compressed module script, measured in milestone 1), the module is not
  compiled while it streams, and `Content-Type: application/wasm` no longer applies to it (Hosting
  decision 4). Rejected: `connect-src 'self'` plus a second `connect-src 'none'` policy added after load,
  because `'self'` allows every own-origin path until then, browsers differ on enforcing a policy added
  after load, and an own-origin `fetch` remains a channel to the host; and keeping the module's exact URL
  on `www.shift2eu.com` in `connect-src` while dropping the local run, because the local run is the
  mitigation for a user who must not trust the host and for the first-visit downgrade, and the smoke
  tests could not run the attested bundle unchanged.
- **Decided: the page script unregisters any service worker for its origin on load.** A page uploaded
  with a leaked deploy key can register a same-origin service worker that outlives the repair and serves
  a look-alike page from then on; the site cannot send `Clear-Site-Data`. When a genuine page loads, one
  named function of the page script calls `navigator.serviceWorker.getRegistrations()` and
  `unregister()` on each registration; it registers nothing. It is the one allowed use of
  `navigator.serviceWorker` in the banned-API scan (*Layer 1*), and the live checks also probe a few
  well-known worker paths (*Layer 3*). It helps only visitors whose browser loads a genuine page;
  a worker that intercepts every navigation keeps them from loading one, which the privacy page says.
- **Decided: only re-submission probes touch the site bucket; every other bucket-level probe runs
  against a temporary canary bucket.** `DeleteBucketWebsite`, `DeleteBucketPolicy` and `DeleteBucket`
  would take the site down or open the bucket if they unexpectedly succeeded; enabling object lock cannot
  be undone; a CORS rule applies to the whole bucket and cannot be scoped to a prefix; and no probe that
  sets a configuration the site bucket lacks (lifecycle, tagging, encryption, versioning if off) or
  suspends versioning can be made harmless there. The runbook first saves the site bucket's website
  configuration, versioning state, ACL and bucket policy with the maintainer's credentials and gives
  their restore commands; only the probes that re-submit one of those unchanged (website, versioning if
  on, ACL, policy) run on the site bucket, and all others run on a temporary bucket in the same project
  with versioning on and a website configuration and policy of the same shape, which the maintainer
  deletes afterwards. Object probes name only the canary object, and only `403 AccessDenied` counts as a
  pass (*Runbook*). *Amended 2026-11-19 (fourth review):* this first moved only the two destructive
  probes and gave stated no-ops for the rest, one of them a CORS rule scoped to a prefix, which S3 CORS
  cannot express.
- **Decided: one allowed download path.** The page script's one download function makes a `blob:` URL
  with `URL.createObjectURL` and sets it on an `<a download>`; a `blob:` URL is same-document and sends
  nothing. `setAttribute` and `setAttributeNS` with `href`, `src` or `action` are banned elsewhere
  (*Layer 1*).
- **Decided: the scheduled check's version window** accepts only the expected release or the one before
  it, for a fixed bound, and always checks the live version against its own attested manifest; a
  version above the highest tag always fails (*Layer 3*). Milestone 4 sets the bound.
- **Decided: the window is anchored at the tagged commit's committer date** (fourth review), not at the
  start of the tag's `release.yml` run: reading a run's start needs `actions: read`, which the checking
  job does not get, while the commit's date needs only `contents: read`. The bound therefore covers the
  whole delay from that commit to the end of the release run: release-please's merge-to-tag delay
  (its `release-please` and `tag` jobs' timeouts), the release workflow's timeout (milestone 3 sets
  `timeout-minutes` on every `release.yml` job, which has none today) and the HTML's `max-age`. A run
  queued past the bound fails the check once, a false alarm the maintainer reads in the alert issue.
- **Decided: the scheduled check treats the live version as untrusted** (fourth review): it must match
  `^v[0-9]+\.[0-9]+\.[0-9]+$` and equal a tag in the repository's own tag list, every command uses the
  listed tag's name, never the page's text, and a missing or unparseable version fails (*Layer 3*).
- **Decided: the deploy's rollback guard trusts only a genuine release** (fourth review): it refuses
  only when the live page's version is a release tag on `main`, higher than the bundle's, and the live
  HTML matches that tag's manifest; it overwrites with a visible warning when the version is unknown,
  unparseable or does not match its manifest. Trusting the page's text would let a leaked key block
  every repair by writing a page that claims a high version. Recovery is a redeploy of the last good
  release, or a version rollback, plus a manual cache purge (*Deploy*, *Runbook*, Hosting decision 5).
- **Decided: the records PR carries a `Release-As` footer** (fourth review). release-please cuts a
  release only for `feat`, `fix`, `perf`, `revert` or a breaking change (`release-please-config.json`'s
  visible changelog sections; the others are hidden), so milestone 4's records-only `docs` PR would
  otherwise never reach a tag, and the live checks, which read the records at the tag, would fail safe
  on every deploy. Its squash commit on `main` carries `Release-As: <next version>`, which release-please
  honours (ADR 0021 and the `release-tag` skill already use it); the maintainer picks the version when
  approving and writes the footer into the squash commit's message when merging, so no one pushes to
  the agent's branch after approval. The `release-tag` skill's review of that release PR accepts the
  version the footer sets.
- **Decided: `img-src 'none'`, not `'self'`** (AI reviewer on 031835d). An own-origin image GET, from
  an `<img>`, an `Image` or a CSS `url()`, carries data in its path or query to the host's logs, the
  same channel for which `connect-src 'self'` is rejected above. Pinning `img-src` to the exact paths of
  content-addressed images would need a host-source, which breaks the run-anywhere bundle, and a
  `data:` source is not allowed either. So the site loads no image: any icon or logo is inline SVG in
  the HTML, the stylesheet has no `url()` or `@import`, fonts and media fall to `default-src 'none'`
  (system fonts only), and the site ships no favicon. The residual: a browser may request
  `/favicon.ico` on its own, a fixed URL that carries no page data (*Layer 2*). The static test pins
  `img-src 'none'`, and the smoke test shows an image and a CSS `url()` load blocked, own-origin and
  cross-origin.
- **Decided: `worker-src 'none'`, with `child-src`, `frame-src` and `manifest-src` pinned to `'none'`**
  (AI reviewer on a5d7f72). With `script-src 'self'` and no `worker-src`, CSP Level 3 falls back from
  `worker-src` to `child-src` to `script-src`, so same-origin dedicated and shared workers and a
  service-worker registration would be allowed. A worker loaded from a same-origin URL takes its CSP
  from its own response headers, not from the page, and this site sends no headers (Hosting decision 2),
  so the worker's `fetch` would escape `connect-src 'none'`. (*Amended 2026-11-24:* once the container's
  front is live, `www` sends the policy as a header, so a same-origin worker's script there gets it;
  the reasoning holds on the other origins, and `worker-src 'none'` stays, ADR 0045.) `child-src`,
  `frame-src` and `manifest-src` already fall back to `default-src 'none'`, but are pinned so a later
  directive cannot widen them by fallback; the static test fails when any of the four is missing or
  other than exactly `'none'` (*Layer 2*). It does not conflict with the unregister function: `getRegistrations()` and
  `unregister()` fetch no script, and `worker-src` governs only the fetch of a worker's script. The
  smoke test shows a dedicated worker (same-origin and `blob:`), a shared worker and a service-worker
  registration each blocked as a `worker-src` violation. Since `require-trusted-types-for 'script'`
  makes each of them a `TrustedScriptURL` sink, a string URL would fail on Trusted Types first and the
  test would pass with `worker-src` deleted; the attempts run on a test-only copy of the page whose
  policy differs only by one test policy name in `trusted-types`, through which the test mints the URLs
  (*Layer 2*).
- **Decided: layer 1 bans messaging to another window or browsing context** (AI reviewer on a5d7f72).
  The CSP governs none of `postMessage`, `opener`, `parent`, `top`, `BroadcastChannel`, `MessageChannel`
  or `window.name`, and each can hand the inventory to another page: a window that opened this one, a
  frame's parent, another same-origin context (for instance a page uploaded with a leaked key, open in
  another tab), or the next page loaded in the same tab, which reads `window.name`. The page script's
  banned-API scan and the glue's allow-list rule therefore refuse `postMessage` (on any object),
  `opener`, `parent`, `top`, `BroadcastChannel`, `MessageChannel`, assignment to `name` and any
  `message` listener (`addEventListener("message", ...)`, `onmessage`), so posted messages are never
  read, with two exceptions, both in the framing guard: its one comparison `window.top !== window.self`,
  and `window.opener = null` on load (*Layer 1*). That assignment is defence in depth behind the ban: it
  drops only this page's own handle to its opener. Since the site cannot send
  `Cross-Origin-Opener-Policy`, the opener keeps its handle to this page, with which it can navigate
  the tab or post messages in, but read nothing. Milestone 2's ADR and the privacy page's technical note
  list the messaging APIs as gaps the CSP does not cover and layer 1 closes, and list the opener's
  remaining handle (no COOP: it can navigate the tab or post messages in, read nothing) as a remaining
  gap, not a closed one; the smoke test checks the page's own `window.opener` reference is `null` after
  load in a page opened by another.
- **Decided (2026-11-19, milestone 1 review): the HTML reporter does not use `html/template`.** Rendering
  with `html/template` grew the gzip-compressed module script to 3,638,609 bytes, 16% over the 3 MiB
  budget, since `text/template`'s reflection keeps every exported method of every reachable type. The
  reporter instead writes a closed set of constant elements, values only as element content, escaped with
  `html/template`'s text-context replacements, pinned by a differential test against `html/template`
  (ADR 0035). The passages of *Trust*, *Accessibility*, *Architecture* and *Decided in this PRD* that said
  "with `html/template`" are amended to match, and the *Architecture* criterion names the depguard rules
  as milestone 1 split them. Pending the maintainer's confirmation; to go back, the budget would rise to
  about 3.5 MiB or the module would move to TinyGo.

### Hosting decisions (decided by the maintainer, 2026-11-19)

These were the PRD's open questions 1 to 11, numbered as the open questions of the PRD's first draft
(PR #298). The maintainer accepted every recommendation on 2026-11-19; each is worded below as decided.
What is still unknown is listed under *To be checked by the maintainer*, with the milestone that needs
it. Decision 4 is amended by the orchestrator's decision above.

1. **Decided: `www.shift2eu.com` on Edge Services, the apex redirected to it.** Edge Services serves only
   a subdomain ([How to configure a custom domain](https://www.scaleway.com/en/docs/edge-services/how-to/configure-custom-domain/)),
   so the one pipeline serves `www.shift2eu.com` with a managed certificate, and the apex `shift2eu.com`
   answers with a permanent redirect to `https://www.shift2eu.com` over HTTPS with a valid certificate
   (*Decided by the maintainer*). Asking Scaleway support about an apex on Edge Services is no longer
   needed. Milestones 1 to 3 do not depend on how the apex redirect is served; milestone 4 does.
   *Amended 2026-11-24 by the maintainer (Option 1a,
   [ADR 0045](../../docs/adr/0045-front-the-site-with-a-serverless-container-running-a-pinned-caddy.md)):*
   Scaleway support (ticket #1639626) answered that Edge Services always answers plain HTTP with a 302,
   cannot turn plain HTTP off and cannot set response headers, so it cannot meet decision 3 or the HSTS
   go/no-go. **Both `www.shift2eu.com` and `shift2eu.com` are now served by one public Scaleway Serverless
   Container in front of the Edge Services pipeline:** a small server (Caddy) on both names as
   custom domains (`www` by a Namecheap CNAME, the apex by a Namecheap ALIAS record), with the
   container's HTTP option `enabled` (not `redirected`), reverse-proxying `www` to the pipeline endpoint
   (`<id>.svc.edge.scw.cloud`). The private bucket, the pipeline and its cache, `ci/sitedeploy`, the
   key's scope (decision 5) and the runbook's R7 stay.
2. **Decided: the CSP in a `<meta>` element, the framing guard in the script, and HSTS as milestone 4's
   go/no-go.** The documentation lists caching, WAF and a custom domain, and no way to set response
   headers ([Edge Services FAQ](https://www.scaleway.com/en/docs/edge-services/faq/)); Object Storage
   lets an object carry `Cache-Control` and `x-amz-meta-*` headers only
   ([Object Storage FAQ](https://www.scaleway.com/en/docs/object-storage/faq/)). The CSP is in a
   `<meta>` element in any case (it travels with the pages, to the other origins too, decision 11), and
   the script refuses to run framed. If Edge Services can add headers, the site also sends
   `Content-Security-Policy` (with `frame-ancestors 'none'`), `X-Content-Type-Options`, `Referrer-Policy`
   and `Permissions-Policy`. HSTS has no substitute against a stripped returning visit, so milestone 4's
   HSTS go/no-go is `Strict-Transport-Security` on `www.shift2eu.com` with `max-age` of at least
   31536000 and `includeSubDomains`, or the maintainer's explicit, recorded waiver. HSTS on `www` never protects a
   first visit, nor a visit that starts at `http://shift2eu.com` (served by the apex redirect's provider,
   which may send no HSTS): only the HSTS preload list closes that. So the privacy page states that risk
   and its mitigation always, until the domain is on the preload list in shipping browsers (*Privacy
   statement*), and the live check holds the page to it. The domain is submitted to the preload list only
   if the apex redirect can meet the list's requirements (checked in milestone 4): `https://shift2eu.com`
   sends `Strict-Transport-Security: max-age=31536000; includeSubDomains; preload` (`max-age` at least
   31536000), and `http://shift2eu.com` redirects first to `https://shift2eu.com` (same host) and only
   then to `https://www.shift2eu.com`. `includeSubDomains` binds every present and future subdomain of
   `shift2eu.com` to HTTPS, and pursuing preload is effectively irreversible (removal takes months to
   reach shipping browsers); milestone 3's hosting ADR
   ([ADR 0042](../../docs/adr/0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md),
   as amended in milestone 4) records that and the
   subdomains that exist when preload is submitted. Milestone 4 records the HSTS outcome (`met` or
   `waived`) and whether preload is pursued in `site/records/hsts.json`, which the live checks read
   (*Layer 3*).
   *Amended 2026-11-24 by the maintainer (decision 1 as amended, ADR 0045):* the host can now send
   headers, so the conditional wording above no longer applies. The container's server sends, on every
   HTTPS response of `www.shift2eu.com`, `Strict-Transport-Security: max-age=31536000;
   includeSubDomains` and the four headers `Content-Security-Policy` (the `<meta>` policy plus
   `frame-ancestors 'none'`), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and
   `Permissions-Policy`. The `<meta>` CSP and the script's framing guard stay, for the other origins
   (decision 11). The apex's HTTPS redirect, served by the same container, sent no HSTS; decided
   2026-11-25 (issue #601), it sends `Strict-Transport-Security: max-age=31536000`, without
   `includeSubDomains` or `preload`, once `infra/site` is applied, and the plain-HTTP redirect sends none (ADR 0045,
   *The headers*). The preload decision is unchanged: it is still milestone 4's, and pursued only by
   recording it in `site/records/hsts.json`; the container can then meet the list's requirements (the
   apex's HTTPS answer with `preload` and `http://shift2eu.com` first to `https://shift2eu.com`, and
   `www`'s header with `preload` too), by a change to its configuration made with the record. Where
   this PRD says elsewhere that the site "cannot send" a response header (`Clear-Site-Data`,
   `Cross-Origin-Opener-Policy`, a worker's own CSP), read "does not send": they are now possible and
   not decided here.
3. **Decided: permanent redirects over HTTPS only.** The apex, and `http://www.shift2eu.com`, answer with
   a 301 or 308 to `https://www.shift2eu.com` with the same path. For `http://www`, a redirect from Edge
   Services if it can redirect; if it cannot, plain HTTP on `www` is refused or the connection closed, if
   Edge Services allows that, and milestone 4's go/no-go accepts it; if it allows neither, that is a
   no-go. Neither `shift2eu.com` nor `www.shift2eu.com` ever serves the site over plain HTTP with a 200
   (*refuse or close* is defined in *Layer 3*). For the apex, which Edge Services cannot
   serve, Namecheap's URL redirect record is used only if it serves HTTPS with a valid certificate for
   `shift2eu.com`; otherwise another means that does (to be checked for milestone 4). The apex redirect's
   provider is a third party: it sees the IP address, user agent, path and `Referer` of every visitor
   who enters through `shift2eu.com`, and serves only the redirect. The privacy page and milestone 3's
   hosting ADR (amended in milestone 4) name it, and if it issues its own certificate, its certificate
   authority goes into the CAA record (decision 10) and the ADR.
   *Amended 2026-11-24 by the maintainer (decision 1 as amended, ADR 0045):* the container's server
   writes the redirects itself, deciding the scheme by `X-Forwarded-Proto`: `http://www.shift2eu.com`,
   `https://shift2eu.com` and `http://shift2eu.com` all answer **308** to `https://www.shift2eu.com` with
   the same path, in one hop (the preload chain only if preload is pursued, decision 2). So `http://www`
   is redirected, and the *refuse or close* alternative above is no longer used. Namecheap's URL
   redirect is not used: by Namecheap's articles 384, 385 and 9604 (not tested), it serves no
   certificate for `shift2eu.com`. **The apex redirect's provider is Scaleway** (Serverless
   Containers), already named on the privacy page, so no third party is added; its certificate
   authority is Let's Encrypt, which the CAA record already names (decision 10).
4. **Decided: the module script is uploaded pre-compressed with `Content-Encoding: gzip`** if Edge
   Services serves it with that header and `Content-Type: text/javascript`; otherwise it is served
   uncompressed, the uncompressed size is held to a budget too (milestone 3's ADR,
   [ADR 0042](../../docs/adr/0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md)), and TinyGo is
   reconsidered only if that budget fails. *Amended 2026-11-19 by the orchestrator's decision above:*
   this decision first named the module itself, served with `Content-Type: application/wasm`; the module
   is now embedded in a script, so no file is served as `application/wasm` and nothing compiles while
   streaming.
5. **Decided: the deploy key is scoped by project and bucket policy.** Object Storage is not among the
   products that support IAM resource-level conditions
   ([Products supporting resource-level conditions](https://www.scaleway.com/en/docs/iam/policies-permissions/supported-products-resource-level/)),
   so an IAM policy cannot name one bucket. The site's bucket lives in a Scaleway project that holds only
   it; an IAM application's policy grants `ObjectStorageObjectsWrite` and `ObjectStorageObjectsRead`
   ([permission sets](https://www.scaleway.com/en/docs/iam/policies-permissions/permission-sets/)) in that
   project only; and a bucket policy, version `2023-04-17`, which allows only what it lists ("Deny
   statements are therefore useless",
   [bucket policies](https://www.scaleway.com/en/docs/object-storage/api-cli/bucket-policy/)), allows the
   application `s3:PutObject` and `s3:GetObject` (and `s3:ListBucket` only if the deploy needs it) on that
   bucket and nothing else, plus the read grant for the site that decision 11 settles. Bucket versioning
   is turned on if it works with a website bucket. The runbook verifies the key's limits with a canary
   object (*Runbook*). A leaked key can still overwrite the pages until it expires or is revoked. The
   scheduled live check notices a changed page within a day and the release's live check at each
   release; until then, users are served the changed page. Recovery is a redeploy of the last good
   release (re-running the deploy job of the highest release tag's `release.yml` run, only within
   GitHub's 30-day re-run window, for which the run's programs' artifacts are kept; the rollback
   guard refuses only a live page that is a genuine higher release, so a page a leaked key wrote,
   whatever version it claims, is overwritten with a warning, *Deploy*) or, with versioning, a version
   rollback of the four pages, `LICENSE`, `NOTICE` and `manifest.json` with the maintainer's own key,
   which after the window is the way back to the last good release unless a new release is cut with a
   `Release-As:` footer ([docs/site-runbook.md](../../docs/site-runbook.md), R12), either followed by a manual Edge Services cache purge by the maintainer, whose own
   credentials hold the Edge Services permission the deploy key lacks. Browsers may keep a
   released page served from a poisoned cache for up to its `max-age` (300 seconds) after that. A page
   uploaded with the leaked key is not bound by that: it sets its own caching (up to a year) and can
   register a same-origin service worker, and both can persist in visitors' browsers after the repair,
   since the site cannot send `Clear-Site-Data` (it sets no response headers, decision 2). The
   mitigations: the page script unregisters any service worker for the origin when a genuine page loads
   (which helps only visitors who load one), and the live checks fail when a well-known worker path
   answers 200 (*Layer 3*). A page uploaded at a path outside every manifest is not detected by the live
   checks; if the key can hold `ListBucket`, the deploy fails on such a key at the next release. The
   privacy page, the verification guide and milestone 3's hosting ADR
   ([ADR 0042](../../docs/adr/0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md))
   say so.
6. **Decided: Chromium for the smoke test is the `chromedp/headless-shell` container image, pinned by
   digest** with a `# renovate: datasource=docker` comment, run by the CI job and in the Lima VM's
   container runtime (ADR 0017); `chromedp` connects to it on loopback. A Chromium download pinned by
   version and checksum was rejected: it needs its own update path.
7. **Decided: `chromedp` lives in the main module, in `test/browser` behind a build tag (`browser`),**
   with a depguard rule that confines it there and a test that the product binaries do not reach it or its
   dependencies. It is the first test-only third-party dependency that drives a browser. A nested module
   was rejected: ADR 0012 names it as a way past the architecture checks.
8. **Decided: the deploy program is a standard-library Go program under `ci/`** (like the AI reviewer,
   ADR 0033) that signs S3 requests itself, tested against a fake object store; not the AWS CLI or
   `s3cmd`, which are unpinned on GitHub's runners or need a Python install, and would hold the key.
9. **Decided: the Starter plan, with one pipeline.** Scaleway's network pricing page, *Edge Services*
   (saved by the maintainer on 2026-11-19, `scaleway-pricing-network-2026-11-19`): **Starter €0.99/month**,
   cache 100 GB, custom domain included, SSL certificate "Let's Encrypt: free" (a custom, uploaded
   certificate is billed by Secret Manager), egress fees free, **1 pipeline**; an additional pipeline
   €4/pipeline per month, additional cache €0.0135/GB. Edge Services' documentation counts the cache limit
   as the data egressed from all pipelines' caches in a month; transfers from the backend into the cache
   are not counted
   ([Understanding Edge Services pricing](https://www.scaleway.com/en/docs/edge-services/reference-content/understanding-pricing/)).
   Object Storage, Scaleway's storage pricing page for Amsterdam (saved 2026-11-19,
   `scaleway-pricing-storage-2026-11-19`): Standard One Zone €0.00803/GB-month, Standard Multi-AZ
   €0.01606/GB-month, requests and ingress included, egress 75 GB free every month, then €0.01/GB. A
   bundle of a few MiB per release, kept for earlier releases too, and cache fills of a few MiB per
   release (within the 75 GB of free egress) cost cents a month. **One pipeline is enough:** the site is
   one backend (the bucket) served on one host (`www.shift2eu.com`). The apex redirect cannot use a
   pipeline at all, because Edge Services cannot serve an apex domain (decision 1), so a second pipeline
   (€4/month) would not serve it either; the apex redirect is served outside Edge Services. Only if the
   maintainer later wants a second Edge-served host (none is planned) would it cost a second pipeline,
   €4/month, or the Professional plan (€12.99/month, 10 pipelines).
   *Amended 2026-11-24 (decision 1 as amended):* the Starter plan and its one pipeline stay, as the
   container's upstream. The container adds its own cost, held by three pins the infra unit enforces:
   `max_scale` 1, 140 mvCPU and 0.25 GB of memory. Serverless Containers' prices (verified by the
   maintainer on Scaleway's pricing page, 2026-11-24, Amsterdam): €0.00001 per vCPU-second after 200,000
   free and €0.000002 per GB-second after 400,000 free, per account and month. The worst case, one
   instance running a whole 30-day month, is about **€2.12 a month**; an idle container scaled to zero
   stays in the free tier. The apex no longer needs a front outside Scaleway.
10. **Decided: CAA `0 issue "letsencrypt.org"` and `0 issuewild ";"`** (and an `iodef` address if the
    maintainer wants reports), since Edge Services generates managed certificates with Let's Encrypt
    ([SSL/TLS certificates](https://www.scaleway.com/en/docs/edge-services/reference-content/ssl-tls-certificate/)),
    and DNSSEC on at Namecheap. DNSSEC covers Namecheap's records only; the CNAME's target zone is
    Scaleway's. If the apex redirect's provider issues its own certificate, the CAA record also names
    that provider's certificate authority (milestone 4).
    *Amended 2026-11-24 (decision 1 as amended):* both names' certificates are now issued by Scaleway for
    the container's custom domains. The apex's certificate was issued by Let's Encrypt under the existing
    CAA record (the maintainer's test C3); `www`'s is expected from the same issuer (Scaleway's Serverless
    Containers FAQ) and is checked at the first deploy (R8). So the CAA record stays as decided and names
    no other authority. The `www` CNAME points at the container's endpoint,
    and the apex is a Namecheap ALIAS record to the same endpoint; DNSSEC stays on, and C3 showed the
    ALIAS's synthesised records signed and validated.
11. **Decided: other origins serving the site are accepted, and only `www` is attested and checked.**
    The site's files are public through `www` in any case; the bucket website endpoint
    `<bucket>.s3-website.<region>.scw.cloud` serves them only under the public-read fallback below, and
    the pipeline's own endpoint `<id>.svc.edge.scw.cloud` serves them as `www` does, each on its own
    origin. The `<meta>` CSP travels with the pages, and the bundle references its files by same-origin
    relative paths and fetches nothing, so it runs the same on each. Milestone 3's hosting ADR lists the
    origins, says which actually serve the site under the chosen read rule and whether each answers over
    plain HTTP, and says that only `www.shift2eu.com` is attested and checked. **The bucket's read rule,
    decided for milestone 3:** the objects stay private, with a bucket-policy grant to Edge Services'
    bucket-website backend, if Edge Services supports it; otherwise the bucket policy grants public
    `s3:GetObject` on the site's objects. A bucket policy's principal can only be a `user_id`, an
    `application_id` or `*` ([bucket policies](https://www.scaleway.com/en/docs/object-storage/api-cli/bucket-policy/)),
    so milestone 3 checks which principal, if any, stands for Edge Services; if none does, the rule is
    public read. Edge Services' quickstart says that with bucket website "objects can remain private"
    ([Edge Services quickstart](https://www.scaleway.com/en/docs/edge-services/quickstart/)), while
    enabling bucket website applies a default policy that makes every object public
    ([bucket website](https://www.scaleway.com/en/docs/object-storage/how-to/use-bucket-website/)); what
    the backend needs is to be checked for milestone 3, and the ADR records which rule applies.
    *Outcome (2026-11-21, [ADR 0039](../../docs/adr/0039-create-the-sites-scaleway-hosting-with-opentofu.md)):*
    no principal stands for Edge Services, so no grant can name it; the backend reads the bucket in
    website mode, where the documentation says the objects can stay private. The private rule is tried
    first: `infra/site`'s `public_read` defaults to `false`, so the bucket policy grants `*` nothing, and
    Runbook R5 asks the pipeline for a probe object. If it answers 200, the objects stay private and only
    the pipeline serves them; otherwise `public_read = true` adds the fallback, a `*` `s3:GetObject`
    grant on the site's objects only, and the bucket website endpoint and the bucket's S3 endpoints
    serve the site's files too. R5 checks each origin over HTTPS and plain HTTP, and its result is
    recorded in milestone 3's hosting ADR
    ([ADR 0042](../../docs/adr/0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md);
    ADR 0039, open point 8).
    *Amended 2026-11-24 (decision 1 as amended):* the container's own endpoint
    `<namespace>-<name>.functions.fnc.<region>.scw.cloud` is a further origin, but it serves no site: its
    server answers every request there with a 308 to `https://www.shift2eu.com`, over plain HTTP too,
    never a 200. The pipeline's endpoint is now the container's upstream and still serves the site on
    its own origin, without the container's headers. The read rule is unchanged. ADR 0042, as amended
    that day, lists the origins.

### To be checked by the maintainer

Facts still unknown, each with the milestone that needs it. None blocks milestones 1 and 2.

- **Milestone 3** (the runbook, the deploy program and its ADR):
  - Edge Services' compression and content types: whether it compresses responses itself, and whether an
    object uploaded with `Content-Encoding: gzip` and `Content-Type: text/javascript` is served with
    those headers (decision 4, as amended; it sets whether milestone 3 adds the uncompressed budget).
  - The IAM permission-set details (decision 5): that the bucket policy refuses the application every
    write action in its action list but `PutObject` (the canary step's list) even if
    `ObjectStorageObjectsWrite` would allow them, and that the project-scoped object permission sets do
    not grant `PutBucketPolicy`, `DeleteBucketPolicy` or `DeleteBucket`, which IAM alone governs; the
    runbook's canary step verifies it in milestone 4.
  - The bucket's read rule (decision 11): which principal, if any, stands for Edge Services in a bucket
    policy (the policy allows only `user_id`, `application_id` or `*`), whether Edge Services'
    bucket-website backend can then read private objects, and which actions the grant names; if no
    principal does, the objects are public read, and the bucket website endpoint serves the site too.
    *Answered by ADR 0039:* no principal does; whether the backend reads private objects without one is
    Runbook R5's test, public read only if it does not (`public_read`).
  - The other origins (decision 11): which of them serve the site under the chosen read rule, and
    whether each answers over plain HTTP. *ADR 0039 lists them; Runbook R5 checks plain HTTP.*
  - Versioning (decision 5): whether bucket versioning works with a website bucket. *OpenTofu enables
    both; R4's apply and R7's saved versioning state confirm it.*
  - Listing the bucket (decision 5, *Layer 3*): whether the deploy key can hold `s3:ListBucket` on the
    site's bucket only; if it can, the deploy program lists the bucket and fails on any key that is in no
    release's manifest. *The bucket policy grants it on the site bucket only; R7 records whether IAM's
    two object permission sets let it through.*
  - The canary probes (*Runbook*): that each probe's request (the re-submissions on the site bucket; on
    the canary bucket an object-lock configuration with no default retention, one CORS rule, one
    lifecycle rule, one tag, the default encryption; on the canary object an empty tag set, no
    retention, an `AbortMultipartUpload` of an unknown upload ID) is evaluated by the policy before the
    bucket's or object's state, so a 403 is meaningful (for instance, that a retention probe on a bucket
    without object lock answers 403 to the deploy key, not an object-lock error); that a temporary
    canary bucket can carry versioning, a website configuration and a bucket policy of the site
    bucket's shape; and that it can be deleted after an object-lock probe that unexpectedly succeeded.
  - The manual cache purge (decision 5): how the maintainer purges the pipeline's cache, for the
    runbook's recovery step. *R12: `scw edge-services purge-request create`, or the console.*
- **Milestone 4** (the first deploy and its go/no-go):
  - How the apex redirect is served over valid HTTPS (decisions 1 and 3): whether Namecheap's URL
    redirect serves `https://shift2eu.com` with a valid certificate and a 301 or 308 to the same path, or
    which other means does; who provides it and what it logs; whether it issues its own certificate (and
    from which authority, for CAA); and whether it can send HSTS with `includeSubDomains` and `preload`
    and redirect `http://shift2eu.com` to `https://shift2eu.com` first (the preload requirements,
    decision 2).
    *Answered 2026-11-24:* not Namecheap's URL redirect (no certificate for `shift2eu.com`, by Namecheap's
    articles 384, 385 and 9604, not tested); the
    Serverless Container of decision 1 as amended, run by Scaleway, with a Let's Encrypt certificate
    (CAA unchanged). It can send HSTS with `preload` and redirect to `https://shift2eu.com` first, by
    its configuration. Checked by the maintainer's test C3 (below).
  - Edge Services' response headers and redirects (decisions 2 and 3): whether it can add
    `Strict-Transport-Security`, `Content-Security-Policy`, `X-Content-Type-Options`, `Referrer-Policy`
    and `Permissions-Policy`, and redirect `http://www.shift2eu.com` to HTTPS, or else refuse or close
    plain HTTP on `www`. *Answered 2026-11-24 by Scaleway support (ticket #1639626):* it cannot; it
    always answers plain HTTP with a 302, cannot turn plain HTTP off and cannot set response headers.
    Hence decisions 1 to 3 as amended.
  - [x] *Checked 2026-11-24 by the maintainer (test C3, on mac-mini-home, throwaway containers in
    `nl-ams`, all pass; ADR 0045 records the settings):* Serverless Containers accept the command and
    arguments, 140 mvCPU, 0.25 GB, plain HTTP enabled (`https-connections-only=false`) and `max-scale`
    1, with the endpoint `<namespace>-<name>.functions.fnc.nl-ams.scw.cloud`; the container's own 308
    reaches the client unchanged over plain HTTP and HTTPS, and `Strict-Transport-Security`,
    `X-Content-Type-Options` and `Content-Security-Policy` unchanged (the platform adds `server: envoy`
    and `x-envoy-upstream-service-time`); a container reverse-proxying the pipeline endpoint answers
    with the same status and the same probe object; and the apex behind a Namecheap ALIAS resolves to
    the container, stays DNSSEC-signed and validated, reaches *ready* as a custom domain with a Let's
    Encrypt certificate under the existing CAA record, and answers 308 over HTTPS and plain HTTP.
  - For the infra unit (ADR 0045): whether the platform overwrites a client's own `X-Forwarded-Proto`
    (a plain-HTTP request carrying `X-Forwarded-Proto: https`, the header as two fields in both orders,
    two `https` fields, or `https, https` in one field must still get the 308: the rule compares the
    header's joined value exactly with `https`, never Caddy's `header` matcher, which accepts any one
    field); that it runs the Caddy image by its digest (a go/no-go; else a private Container
    Registry mirror, recorded in ADR 0045 first); how the configuration reaches the container;
    `min_scale`, and a cold start within the live check's request timeout.
  - The HSTS go/no-go (decision 2): HSTS on `www.shift2eu.com` with `max-age` of at least 31536000 and
    `includeSubDomains`, or the recorded waiver; whether preload is pursued, knowing it is effectively
    irreversible and binds every future subdomain; and the HSTS record (`met` or `waived`, preload or
    not) committed in `site/records/hsts.json` and the preload record in `site/records/preload.json`,
    merged to `main` and included in a release tag before `SITE_DEPLOY_ENABLED` is set.
  - The scheduled check's version window (*Layer 3*): its bound, from the tagged commit's committer
    date, at least release-please's merge-to-tag delay (its `release-please` and `tag` jobs' timeouts)
    plus `release.yml`'s timeout (set in milestone 3) plus the HTML's `max-age`, with a margin for
    queueing. *Set 2026-11-26:* 180 minutes, against a floor of 165 today (30 + 130 + 5; ADR 0043,
    *Amendment: the scheduled check*).
  - The records' release (*Runbook*): the version the records PR's `Release-As` footer names, picked
    when approving it and written into its squash commit's message when merging.
  - CAA and DNSSEC (decision 10): that Scaleway's managed issuance passes with the CAA record, and that
    Namecheap supports CAA and DNSSEC for the domain. *Checked 2026-11-24 for the apex (C3):* the
    container's Let's Encrypt certificate was issued under the CAA record, and DNSSEC validated with the
    ALIAS; `www`'s certificate on the container is checked at the first deploy (R8).
  - The deploy key's limits (decision 5): the runbook's canary step answers `403 AccessDenied` for every
    write action in the bucket-policy action list but `PutObject` and for the bucket calls IAM alone
    governs (any other answer recorded as inconclusive until explained), the site bucket's website
    configuration, versioning state, ACL and policy saved first, only the re-submission probes run on
    the site bucket and every other bucket-level probe on the temporary canary bucket, the negative
    probes outside the project fail, and the maintainer removes the canary object and bucket; rerun
    after every key rotation and policy change.

### Proposed follow-ups (not filed)

- Pruning earlier releases' assets from the bucket, by hand or with a separate key.
- Sending `Cross-Origin-Opener-Policy` and, for the leaked-key repair, `Clear-Site-Data` from the
  container's server, now that the host can send headers (Hosting decision 2 as amended, ADR 0045).

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

Scaleway's prices were read from its pricing pages as saved by the maintainer from a browser on
2026-11-19 (scaleway.com refuses automated requests; the saved copies are not in the repository):
[network, Edge Services](https://www.scaleway.com/en/pricing/network/#edge-services) and
[storage, Object Storage](https://www.scaleway.com/en/pricing/storage/) (region Amsterdam). Prices
change; the runbook rechecks them before the subscription is taken out.
