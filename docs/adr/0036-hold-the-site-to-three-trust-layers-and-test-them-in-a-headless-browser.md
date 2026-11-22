# 0036. Hold the site to three trust layers, and test them in a headless browser

- Status: Accepted
- Date: 2026-11-20

## Context

PRD #297 promises that an inventory pasted into `www.shift2eu.com` never leaves the browser. Milestone 1
(ADR 0035) built the WebAssembly module that runs the assessment. Milestone 2 adds the pages that load it:
`site/` (the assessment page, the privacy page and the verification page, one stylesheet and one page
script), `make site`, which assembles the bundle (`ci/sitebuild`), and the tests that hold the promise.

A promise like this cannot rest on one mechanism. The page's own code could send data. A browser could
load a resource the code never asked for. And the files a visitor loads could be other than the ones that
were reviewed. Each needs its own defence, and each defence has gaps the others must cover or the site
must state. The static host (Hosting decision 2) cannot send response headers of the site's choosing, so
everything the browser enforces has to be in the pages themselves. Some of what the PRD requires can only
be seen in a real browser: what the policy refuses on the wire, what Trusted Types do, how the page
behaves when the module runs.

## Decision

The site is held by three layers, each pinned by tests. Static tests run under `make test`, and a
headless-browser smoke test runs under `make test-browser` and in CI.

### Layer 1: the code sends nothing

- **The page script** (`site/page.js`) is scanned by `test/site/internal/jsscan`. The scan refuses every
  browser API that can send data, load a resource or navigate: `fetch`, `XMLHttpRequest`, `WebSocket`,
  `EventSource`, `sendBeacon`, `RTCPeerConnection`, workers, `importScripts`, `open`, `location`, URL-part
  assignments, `setAttribute` with `href`, `src` or `action`, storage and cookies. It refuses every way to
  reach another window or browsing context: `postMessage`, `opener`, `parent`, `top`, `BroadcastChannel`,
  `MessageChannel`, assignment to `name`, and any `message` listener. It also refuses every computed member
  access and `Object.assign`, `Reflect` and `defineProperty`, since a name scan cannot see a computed
  name. Three named functions hold the only exceptions, each in the one form the scan allows:
  - the **framing guard**, `guardFraming`: `window.top !== window.self` once and `window.opener = null` on
    load;
  - the **download function**, `download`: one `blob:` URL on an `<a download>` it clicks, then revoked;
  - the **unregister function**, `unregisterServiceWorkers`: it unregisters every service worker of the
    origin and registers none.
- **The glue** (`cmd/shift2eu-web`) names JavaScript only through its allow list of constant names (ADR
  0035). `test/site` fails when a sending or messaging name joins that list. `test/architecture` holds
  `syscall/js` to the glue alone.
- **The glue's trust boundary** (issue #346, part 1). `shift2euAssess` returns data and inserts nothing.
  The page script treats each field of its answer in one way only, and the scan pins each:
  - `error` and `kind` are plain text. The page shows `error` only as `element.textContent = result.error;`
    and reads either field otherwise only under `typeof`.
  - `markdown` and `json` are the downloads' bytes. They go only into `new Blob([result.markdown], ...)`
    and `new Blob([result.json], ...)`, never into the page.
  - `html`, the HTML reporter's fragment, is the only markup. It goes only to
    `insertReport(target, result.html)`.

  The smoke test's `TestGlueReturnsTextOnlyErrors` and `TestHostileInventoryRendersAsText` call the
  module with hostile values. They check that nothing a value carries reaches the page as markup, and that
  nothing runs.
- **A hostile getter makes `shift2euAssess` throw.** The glue reads the options object with
  `js.Value.Get`. If a property of that object is a getter that throws, the JavaScript exception unwinds
  through the call, and `syscall/js` cannot catch it: it is not a Go panic, so the glue's `recover` never
  sees it, and the call throws to its caller rather than returning an `internal` failure. The page builds
  the options object itself, from its own controls, so only code already running in the page can pass
  such a getter. Even so, the page wraps the call in `try`/`catch` and shows a constant message, never the
  exception. The smoke test observed that the module answers the next call normally after such a throw.
- **`wasm_exec.js` is trusted as toolchain code** and is not scanned (ADR 0035). The bundled file is byte
  for byte the building toolchain's, which `test/site` checks, and `go.mod`'s exact go line pins that
  toolchain (ADR 0035, issue #347). The module script is not scanned either: `test/site` holds it to its
  fixed template, so it carries only the encoded module.
- **The report carries no inventory-derived `id` or `name`.** The HTML reporter emits no `id` or `name`
  from a value (ADR 0035), so no report element can shadow, through DOM named access, the module global or
  a name the glue reads. The page reads `globalThis.__shift2euModule` once, before any report is inserted,
  checks that it is a string and deletes it.

### Layer 2: the browser refuses

- **The Content Security Policy**, a `<meta http-equiv>` element that is the first child of every page's
  `<head>`, is exactly:

  ```text
  default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'none';
  connect-src 'none'; form-action 'none'; base-uri 'none'; object-src 'none'; worker-src 'none';
  child-src 'none'; frame-src 'none'; manifest-src 'none'; require-trusted-types-for 'script';
  trusted-types shift2eu-report
  ```

  It sets no `report-uri` or `report-to`, since violation reports carry blocked URLs and samples.
  `test/site` parses each built page's policy and fails on a missing, widened or added directive.
- **`connect-src 'none'` and `img-src 'none'`.** The site loads no image. Icons are inline SVG, the site
  has no favicon, and the stylesheet has no `url()` or `@import`. An own-origin image request would carry
  data in its path to the host's logs, and a CSP host-source that pins exact paths needs a host, which
  would break the run-anywhere bundle. A browser may still request `/favicon.ico` by itself, outside the
  page's policy. That is a fixed URL that carries no page data, and the host answers it with an error.
- **`worker-src 'none'`, and why the fallback matters.** Without it, CSP Level 3 falls back from
  `worker-src` to `child-src`, then to `script-src 'self'`, which would allow a same-origin worker. A
  worker takes its policy from its own response headers, which this site cannot send, so the worker's
  `fetch` would escape `connect-src 'none'`. `child-src`, `frame-src` and `manifest-src` would fall back to
  `default-src 'none'` anyway. They are pinned to `'none'` so that no later directive can widen them by
  fallback.
- **The module is embedded, never fetched.** `make site` writes the module, base64-encoded, into
  `module.<h>.js`, one fixed template. The page loads it under `script-src 'self'`, decodes it and calls
  `WebAssembly.instantiate`, which `'wasm-unsafe-eval'` allows.
- **Three scripts, in order.** The assessment page loads `wasm_exec.js`, the module script and the page
  script, each a classic `defer` script, in that order. The privacy and verification pages load only the
  page script. Every script and the stylesheet carry Subresource Integrity (SHA-384), which `test/site`
  computes and compares.
- **Trusted Types: one policy and one insertion point.** The page creates exactly one policy,
  `shift2eu-report`, which the CSP names, with no `'allow-duplicates'`. The report enters the page in
  exactly one place, `insertReport`, through that policy. The scan refuses a second policy, a second
  markup sink and any other value at the insertion point.
- **Not every browser enforces `require-trusted-types-for`.** A browser without Trusted Types ignores the
  directive. In such a browser, the one insertion of the report rests on two things, not on Trusted Types:
  - the HTML reporter's escaping: a closed set of constant elements, values only as element content,
    escaped with `html/template`'s text-context replacements, pinned by a differential test against
    `html/template` and by the reporter's hostile golden (ADR 0035);
  - the CSP's ban on inline script.

  No statement of the site claims that Trusted Types protect every user.
- **No storage.** The scan refuses cookies, `localStorage`, `sessionStorage` and `indexedDB`. The
  `downgrade-risk` statement and, if HSTS is waived, the `hsts-waived` statement are shown by default.

### Gaps the CSP does not close

The CSP governs none of these, and the site says so in the privacy page's technical note:

- **Navigation** of the page, and **WebRTC** (`RTCPeerConnection`). The smoke test records that both are
  *not* blocked by the policy. Layer 1's scan closes them in the page's own code.
- **DNS prefetch.** Every page sets `x-dns-prefetch-control` to `off` and has no prefetch, preconnect or
  prerender link.
- **Messaging to another window or browsing context**: `postMessage`, `opener`, `parent`, `top`,
  `BroadcastChannel`, `MessageChannel` and `window.name`. Layer 1's scan closes them, its ban on any
  `message` listener included. The framing guard's `window.opener = null` is defence in depth.
- **A remaining gap, not a closed one: the opener's handle.** The site cannot send
  `Cross-Origin-Opener-Policy`. A page that opened this one keeps its handle to it. With that handle it can
  navigate the tab or post messages in, but read nothing. The page adds no `message` listener, so a posted
  message is never read. `window.opener = null` drops only the page's own handle to its opener; it does
  not close this one.
- **Framing.** A meta policy cannot carry `frame-ancestors`, so the framing guard stops the page, and says
  why, when it is framed.

### Layer 3: users can check what they run

`make site` is reproducible: `test/site` builds the bundle twice and compares the two builds. The module
is built with `-trimpath` and `-buildvcs=false`, so it depends on the sources alone. The bundle's
`manifest.json` lists every file's SHA-256. The verification page says how to check the live files against
an attested bundle's manifest, and how to run that bundle locally. Its attestation, the deploy and the live checks are milestone 3's.

### The builder reads the catalogs and the version

`ci/sitebuild` is CI tooling, but not standard library only, as the PRD first said. The assessment page
names each target's snapshot date and regions, and every page the version. The builder takes them from
where the CLI does: it imports `internal/adapter/target/{ovhcloud,scaleway,upcloud}`, constructs the three
targets and reads their embedded catalogs (`internal/domain/catalog`), and takes the development value an
untagged build shows from `internal/version`. A copy of those values in the builder would drift from the
snapshots. This makes it the one place outside `cmd/` that constructs adapters: `adapters-are-built-in-cmd`
leaves `ci/sitebuild` out, and its strict rule `site-builder` names exactly those five packages, each
ending in `$`, beside its standard packages. It is a main package that nothing imports, the three targets
read only embedded snapshots and never the network, and the release runs it from milestone 3.

### The headless-browser smoke test

- **`test/browser`**, behind the build tag `browser && !js`, drives the built bundle with `chromedp`.
  The PRD allows no npm, Playwright or Puppeteer. The smoke test pins every criterion the PRD marks
  *browser*.
- **`make test-browser`** builds the bundle with `make site` (amended below, issue #423) and starts
  `chromedp/headless-shell` in a container pinned by tag and digest in the Makefile, with a `# renovate:`
  comment and a Renovate manager for the Makefile.
- **The networking model.** The container uses the host network, and its entrypoint is replaced by the
  headless shell itself with `--remote-debugging-address=127.0.0.1`, so DevTools listens on 127.0.0.1
  only. The image's own `run.sh` would open it on every interface through `socat`. DevTools listens on a
  port the kernel picks for each run, not a fixed one (amended below, issue #368). The recipe waits until
  Chromium logs its DevTools address on 127.0.0.1 and passes that address to the test.
  It removes the container on every exit, including a failure or an interrupt. The recipe works with
  docker (CI) and with nerdctl (the VM, whose rootless nerdctl has no bridge network), through the
  Makefile's `DOCKER`.
- **The bundle is served from 127.0.0.1**, by an `httptest` server of the test. 127.0.0.1 is a secure
  context, which the smoke test asserts (`window.isSecureContext`, and `navigator.serviceWorker` defined),
  so the service-worker attempts cannot pass vacuously. It is also an origin other than the site's, which
  shows that the bundle runs unchanged on any origin. "Another origin" in the tests is a second local
  `httptest` server, so the test never touches the internet.
- **Workers are tested on a test-only copy of the page.** On the built page, a string worker URL fails as
  a Trusted Types error before any `worker-src` check, so a test there would pass with `worker-src`
  deleted. The test copy widens only `trusted-types`, by one test policy, and compares every other
  directive with the built page's.
- **`chromedp` is confined to `test/browser`.** The depguard rule `chromedp-only-in-the-browser-test`
  refuses it in every other file. `browser-smoke-test` holds the test files to exactly what they import,
  and `browser-smoke-test-doc` holds the untagged `doc.go`. `test/architecture`'s
  `TestNoProductBinaryReachesChromedp` holds the dependency trees of `cmd/shift2eu`, `cmd/shift2eu-server`
  and `cmd/shift2eu-web` to the same. Every lint run sets the `browser` tag. The test files leave out
  `js/wasm`, where chromedp does not build (it needs `syscall.SysProcAttr.Setsid`).
- **CI runs it as its own job**, `Browser smoke test` in `ci.yml`, with `contents: read` only.

### Size

The module script measured at this commit, Go 1.27.4 on linux/arm64: 8,917,668 bytes, 2,407,832 bytes with
`gzip -9` (2.30 MiB, 77% of the 3 MiB budget of ADR 0035).

## Alternatives considered

- **Send the policy as a response header.** The static host cannot send headers of the site's choosing
  (Hosting decision 2). The meta policy is in every page, so it also holds when the bundle runs locally.
  Where the host can add a header later, that header adds `frame-ancestors 'none'`.
- **`connect-src 'self'` or `img-src 'self'`.** An own-origin request carries data in its path or query to
  the host's logs. `'none'` leaves no such channel.
- **Fetch the module** with `instantiateStreaming`. That needs `connect-src` to allow the module's URL,
  which is the same channel. Embedding it in an SRI-pinned script of one fixed template needs no
  connection.
- **Rely on Trusted Types alone for the report.** Not every browser enforces them. The reporter's escaping
  and the ban on inline script hold in every browser, and Trusted Types are a second line where they exist.
- **Playwright or Puppeteer for the smoke test.** Both need npm, which the PRD rules out. `chromedp` is Go
  and talks to Chromium over DevTools directly.
- **The image's own entrypoint.** Its `run.sh` exposes DevTools on `0.0.0.0` through `socat`. With the host
  network, that would open the browser to every interface of the VM or the runner.
- **A bridge network with a published port.** The VM's rootless nerdctl has no bridge CNI plugin, and the
  browser has to reach the test's `httptest` origin on the host's 127.0.0.1 anyway.

## Consequences

- A change to the page script that adds a sending, storage or messaging API fails `make test`. So does a
  change to the CSP, the scripts per page or the SRI values. A change in what the browser does with the
  bundle fails the smoke test.
- The opener's handle and a browser without Trusted Types are known limits. The privacy page's technical
  note states them, so no statement overclaims.
- `make test-browser` needs a container runtime and about a gigabyte of free memory. The Makefile's pin is
  the one copy of the image, which Renovate updates with its digest.
- Every lint run loads `test/browser`. A new import there must join `browser-smoke-test`'s allow list.
- Milestone 3 runs the same smoke test on the bundle the release attests, before the deploy.

## Amendment: the chromedp check may fetch, in an allow-listed environment (issues #391 and #395)

2026-11-20. `TestNoProductBinaryReachesChromedp` lists `test/browser`'s dependencies with `go list`. Only
that browser-tagged package imports `chromedp`, and `go test ./...` never builds it, so on an empty module
cache (a release run) `chromedp` and `cdproto` are in no cache until that listing loads them. With
`GOPROXY=off`, as the other architecture listings run, it failed (#391). The listing therefore fetches the
modules go.sum pins through `GOPROXY`, as `go test` itself fetches its own: it is the one listing in
`test/architecture` that may touch the network, and only on a cold cache.

It does so in an environment built from an allow list, not this test's own minus a few names (#395):
the variables `check`'s `inheritedEnv` takes for the toolchain, the caches and a temporary directory
(all except `GOTOOLCHAIN`), `GOPROXY` passed through, and the HTTP proxy and certificate variables a fetch
through it reads, plus the fixed `CGO_ENABLED=0`, `GOENV=off`, `GOFLAGS=-mod=readonly`,
`GOTOOLCHAIN=local` and `GOWORK=off`. `GOSUMDB`, `GONOSUMDB`, `GOPRIVATE`, `GONOPROXY`, `GOINSECURE`,
`GOEXPERIMENT`, `GOOS`, `GOARCH` and every name it does not know stay out, so nothing can make the fetch
go around `GOPROXY` or go.sum, switch the toolchain, or change what is listed. `TestGoListProbeEnv` pins
that environment.

## Amendment: DevTools on a random port, read back from Chromium's log (issue #368)

2026-11-20. The smoke test's Chromium listened on the fixed port 9222. Any process in the VM or on the CI
runner could attach to it while the test ran, without authentication, and two `make test-browser` runs at
once collided: the second one's wait failed (the audit of PR #359, finding 2).

`make test-browser` now starts Chromium with `--remote-debugging-port=0`, still on 127.0.0.1 with the
host network. The kernel picks a free port for each run. The recipe reads the browser's address from the
log line `DevTools listening on ws://127.0.0.1:<port>/devtools/browser/<id>`, accepting it only on
127.0.0.1, and passes it as it is in `SHIFT2EU_DEVTOOLS_URL`. Because the address carries the browser's
id, the test attaches to the browser this run started and to no other. That is a correctness guarantee,
not access control: any local process that finds the port can read the id from `/json/version` on it.
The 60-second wait on the log line stays, with its diagnostics, and so does the removal of the container
on every exit. The container's name carries the recipe shell's PID, so the DevTools ports and containers
of parallel runs never collide. The Makefile's `DEVTOOLS_PORT` is gone.

Issue #423 settles the two follow-ups. Each run serves a bundle of its own: `ci/sitebuild` builds it
straight into a fresh directory from `mktemp -d`, which the run passes in `SHIFT2EU_SITE_BUNDLE` and
removes on every exit, a hang-up, an interrupt and a termination included. `make test-browser` no
longer depends on `make site` and neither creates, reads nor removes `bin/site`: `make site` removes and
rebuilds that directory, so a second run's `make site` in the same worktree would delete the bundle from
under the first run's browser or its builder. A copy of `bin/site` could still race with that rebuild,
and a lock would make a second run wait or refuse, so neither was chosen. `make site` alone writes
`bin/site` and its archive, for the release and the deploy. During the 60-second wait, each poll also
asks `docker inspect` (or `nerdctl inspect`) whether the container still runs. One that inspect reports
as not running fails the run at once, naming its exit status and showing the tail of its log. A failed
inspect alone is no verdict; three in a row mean the container is gone, and the run fails with
inspect's last error instead of waiting out the 60 seconds.

Issue #449 bounds the container's removal. A wedged container runtime never answered the EXIT trap's
`docker rm -f`, so the run hung there after its smoke test had passed. The trap now runs that removal in
the background and waits no longer than `TEST_BROWSER_RM_DEADLINE` seconds (default 30). Past that, it
prints the container's name and the command that removes it by hand, and ends that one `rm`. The run's
own exit status stays.

The deadline is a whole number from 1 to 99999 with no leading zero; anything else is refused with exit
status 2 before anything runs. Make exports it to the recipe with `$(value ...)`, and the recipe reads it
as a shell variable, never pasted into its shell text. The default and a value from the environment reach
the recipe raw. A value given on make's command line is expanded by make first, as make does every
command-line variable, and it too reaches the recipe only as data the guard checks. A value holding a quote
so cannot break out of the recipe's quoting and run as a command. `override export` was considered and not
chosen. The bound does not poll: beside the `rm`, the trap starts a watchdog that sleeps out the deadline,
then sends the `rm` SIGKILL (the `rm` ignores the signals the trap ignores) and exits 3. The watchdog sets
its USR1 trap before it starts its sleep. A flag catches a USR1 that arrives before the sleep's pid is
known, so the sleep ends either way. Once the sleep has run out, the watchdog ignores USR1, so a late one
can neither turn its exit 3 into 0 nor make it kill its own reaped sleep. The trap waits on the `rm`, its
own child, and then always sends the watchdog USR1, whatever status the `rm` gave back. It prints its one
line only when the watchdog exited 3. A healthy removal so costs no extra second, and every background
process writes to `/dev/null`, so none holds make's output open.

A window an instant wide is left for each of the three pids the trap signals. An `rm` that returns at
the very instant the deadline passes can be reaped by the trap's `wait`, and its pid reused, before the
watchdog's SIGKILL lands. A USR1 that lands between the sleep's exit (bash reaps it at once) and the
watchdog's `trap "" USR1` sends SIGKILL to the sleep's reaped pid. A watchdog that exits (after its
SIGKILL of the `rm`, or otherwise) in the instant before the trap's `kill -s USR1` may already be
reaped, since dash and bash reap an exited background child before any `wait`, and its pid reused, so
that USR1 lands elsewhere. A shell offers no call that kills a child only while it is unreaped, so
POSIX sh cannot close these windows; they are accepted as negligible. When `DOCKER` is a wrapper
(`sudo docker`, a script), the kill ends only the wrapper, and a child of its own may outlive it.

A port the kernel picks is still a listening port, which a local process can find and attach to. It is
open only while a run lasts and is no longer a fixed, well-known one. That residual risk is accepted.

**Not chosen: `--remote-debugging-pipe`.** This option opens no port at all. But chromedp (v0.24.0)
drives a pipe only through its `ExecAllocator`, which starts the browser itself and gives it file
descriptors 3 and 4 (`allocate_pipe.go`). `docker run` and `nerdctl run` pass only standard input,
output and error into a container, never further descriptors. A pipe would therefore need the test to
start the container itself and bridge the protocol over the container's standard streams, with a
wrapper inside the image. That would give up "the test starts no browser" and move the image pin and the
container runtime into the test. It is more machinery than a port the kernel picks, for a residual risk
that is already low.
