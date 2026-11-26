# 0043. Check the live site against the release's manifest, and hold the edge to a size budget

- Status: Accepted
- Date: 2026-11-23

## Context

PRD #297's third trust layer lets users check what they run (*Layer 3*): the site that
`www.shift2eu.com` serves must be byte for byte the bundle a release built and attested. The release's
check (milestone 3, a later pull request of this milestone) and milestone 4's scheduled workflow both need
a program that compares the live site with a release's `manifest.json` (ADR 0036) and judges what only the
edge decides: redirects, certificates, caching, HSTS, compression. The hosting cannot set response headers
(Hosting decision 2), the apex is served by a redirect of another provider (Hosting decisions 1 and 3), and
a leaked deploy key could upload pages of its own (Hosting decision 5), so the check reads the edge's
answers as untrusted input.

Issue #346 item 2 adds the size question: ADR 0035's budget test gzips the module script with Go's default
level, while the edge (Scaleway Edge Services) serves whatever bytes we upload, pre-compressed or not
(Hosting decision 4). The PRD asks for the size the edge actually transfers to be checked against the 3 MiB
budget, and for a budget on the uncompressed module script in case the edge cannot serve it gzip-encoded.

## Decision

**`ci/sitecheck`, a standard-library CI program, checks the live site against a release's manifest; its
library `ci/sitecheck/internal/livecheck` does every fetch and judgement, and the module script is held to
3 MiB transferred when gzip-encoded and to 11 MiB decoded however it is served.**

- **Shape (ADR 0033's pattern).** A main package that nothing can import, held by the depguard rule
  `site-check-main` (named standard packages, `os` to read the files its flags name, `syscall` only so
  that a runner's SIGTERM cancels the run like an interrupt and the partial report is still printed, no
  `os/exec`, no `net`), and a library held by `site-check-library` (named standard packages, no `os`, `os/exec` or
  `syscall`): it opens no file, reads no environment variable (forbidigo `site-check-env`, both packages,
  tests included) and starts no process. ~~No release builds it; the release's check and the scheduled
  workflow run it with `go run`.~~ *Amended (PRD #297 milestone 3, PR C2):* the release builds it in its
  `site-programs` job (`make site-programs`, static, `-trimpath`), and its `site-check` job runs that
  binary, checked against the job's SHA-256 output, so the check needs no Go toolchain or module fetch;
  the scheduled workflow (milestone 4) may still `go run` it. This pull request wires no workflow.
- **Inputs, all explicit.** The canonical host and the apex (defaults `www.shift2eu.com` and
  `shift2eu.com`); the release's `manifest.json`, read strictly (exactly its fields, a clean relative URL
  path per file in printable ASCII with no `..`, `.`, empty element, leading `/`, `\`, `?`, `#`, `%` or
  space, 64 lowercase hex digits per digest, no path twice, at most 1 MiB); the records
  `site/records/hsts.json` and `site/records/preload.json` of the commit it runs from; and the repository's
  tag list (a file or stdin). Manifest paths are only ever URL paths on the canonical host, never opened as
  files. The program reads exactly the files its flags name, each capped.
- **What a round checks**, every URL built from the configured hosts and the manifest's paths, every
  request without a query string, with `Accept-Encoding: gzip`, through no proxy, following no redirect,
  over HTTP/1.1 only (the transport dials through the injected dialer with its own TLS configuration,
  which turns HTTP/2 off: the dialer is what lets the tests reach local servers under the production
  names, and the edge serves the same objects over every protocol, so headers, compression and size are
  judged as the edge serves them over HTTP/1.1, not necessarily byte for byte as a browser sees them over
  h2 or h3):
  1. every manifest file at `https://<host>/<path>` answers 200 and its SHA-256 after content decoding
     (gzip; any other or a listed `Content-Encoding` is a mismatch) is the manifest's; a 3xx, with or
     without `Location` (an `x-amz-website-redirect-location` included), is a mismatch;
  2. `https://<host>/` serves the bytes of the manifest's `index.html`;
  3. `/sw.js`, `/service-worker.js`, `/serviceworker.js` and `/worker.js` each answer with a status
     outside 2xx (a browser registers a script served with any 2xx); a path with no response at all fails
     too, since it says nothing about what the path serves (a manifest listing one is a configuration
     error);
  4. `/` and every HTML page answer with a `Cache-Control` whose `max-age` is at most 300 and whose
     `s-maxage`, `stale-while-revalidate` and `stale-if-error`, if any, are too (the `stale-*` directives
     let a cache serve the page past its `max-age`, which the retry bound does not wait out; missing,
     unparseable or conflicting directives fail); assets' headers are recorded only;
  5. the redirect matrix on `/` and `/privacy.html`: `http://<apex>/<p>`, `https://<apex>/<p>` and
     `http://<host>/<p>` each answer 301 or 308 to exactly `https://<host>/<p>`; when preload is pursued,
     `http://<apex>/<p>` goes first to `https://<apex>/<p>` (the two hops the preload list requires); in
     refuse-or-close mode (the flag `-plain-http-www`, below) `http://<host>/<p>` may instead refuse the
     connection or close (or reset) it before any byte of a response arrives, redirect to the canonical URL, or answer 4xx/5xx with a body
     that is no manifest file's (any other 3xx and any 2xx fail). A dial that times out with no
     connection made (a filtered port) counts as refused: a browser gets no page from it either. So
     does a TCP connect that would complete only after the request timeout: no connection made within
     the timeout counts as refused, though a browser that waits longer could get a page from such an
     edge. This is accepted: it takes a deliberately slow edge, and the release's and the scheduled
     check's other HTTPS checks on the same host still run. A deadline on the whole run's context is
     not expected (the program passes none); if one expired during a dial, the request would count as
     refused too. A connection that times out once made, or any byte read from the connection, even bytes Go cannot
     parse as a response (raw HTML without a status line, HTTP/0.9, a malformed header block) or
     discards because they came before the request ("unsolicited response"), fail: a browser may still
     wait for or render them. The check counts the bytes on the connection itself: its dialer wraps each
     connection in a reader that marks the request's probe on any read of a byte, and records whether a
     dial returned a connection at all; keep-alive is off, so every request has its own connection and
     probe. `httptrace`'s `GotFirstResponseByte` is not used: it never fires for discarded bytes. A
     response whose bytes race a reset passes only if none of them was read, which matches what a
     browser can show; neither host ever answers 200 over plain HTTP;
  6. the certificates of the host and the apex verify for their names against the configured roots at the
     injected time, and the leaf and every intermediate of the verified chain (not the root) stay valid
     for the margin, at least **14 days**, so a failed renewal, or an intermediate that expires first, is
     found two weeks before visitors see it; the dial and the handshake are bounded by the request
     timeout (30 s by default), so a host that accepts the connection and never answers fails the check
     instead of hanging it; `CheckCertificate` keeps the deadline its caller's context has, longer or
     shorter, and sets the 30 s default only on a context with none;
  7. HSTS against the records: when `met`, the host's `Strict-Transport-Security` has `max-age` at least
     31536000, `includeSubDomains`, and `preload` when preload is pursued (`hsts.json`'s `preload: true`);
     the apex's header is held to the same minimum (with `preload`) only when preload is pursued, and is
     otherwise only recorded. When `waived`, `privacy.html` and `verify.html` must carry the `hsts-waived`
     statement. Unless the site counts as on the preload list (below), `privacy.html` must carry
     `downgrade-risk`. A statement is
     carried when the deployed page, the one whose digest matched the manifest, has exactly one element with
     that `id`, not `hidden`, with non-empty text, outside comments, raw-text elements and templates (ADR
     0036's markers). The header parser is strict: one header value, exactly one `max-age` of digits
     (quoted or not), no value on `includeSubDomains` or `preload`, a closed quote, so `max-age=31536000x`,
     a second `max-age` or a quoted trick fails rather than reading as stronger or weaker than it is;
  8. the live version (below);
  9. the module script's size and `Content-Type: text/javascript` (below).
- **Fail-safe records.** A record that is missing, unreadable or refused fails a check of its own and
  never counts in the site's favour: an unreadable `hsts.json` fails the `hsts.json` check and the rest is
  judged as `met` without preload; an unreadable `preload.json` fails the `preload.json` check and counts
  as not on the preload list, so the `downgrade-risk` statement is required.
- **The records' format is ADR 0042's.** [ADR 0042](0042-host-the-site-behind-edge-services-and-deploy-it-from-the-release-with-a-bucket-scoped-key.md)
  ("The HSTS and preload records") owns the format of `site/records/hsts.json` (`format`, `outcome`,
  `preload`, `date`, `reason`) and `site/records/preload.json` (`format`, `listed`, `submitted`,
  `checked`, `subdomains`); `livecheck`'s parsers follow it, never the other way round. They read both
  strictly: exactly those fields, each key exactly in lower case and present once (`encoding/json` alone
  matches keys in any case and keeps the last duplicate), no null, `format` 1, no trailing data, at most
  4 KiB, every value checked against that ADR's rules (`preload` true only with `met`; dates real
  calendar days written `YYYY-MM-DD`; `reason` at most 500 printable characters, empty when `met` and
  required when `waived`; `listed` true only with a `submitted` date; `subdomains` lower-case host names
  under `shift2eu.com`, sorted, each once, empty while nothing was submitted). A record that breaks any
  rule is refused and fails safe as above.
- **The records meet in one rule.** `preload.json`'s `listed: true` counts only when `hsts.json` was read
  and pursues preload (`outcome` `met`, `preload` true), as ADR 0042 requires. Otherwise, a missing,
  unreadable, met-without-preload or waived `hsts.json`, the claim fails a `hsts` check of its own at
  `preload.json` and the site counts as not on the preload list, so `downgrade-risk` is still required:
  the claim never removes a statement the other record does not back. Like an unreadable record, this
  failure ends the rounds early; no later round can change it.
- **Plain HTTP on the canonical host is a flag, `-plain-http-www`.** `redirect`, the default, requires
  `http://<host>/<p>` to redirect like the apex; `refuse-or-close` also accepts the refused or closed
  connection of item 5, which ADR 0042's Hosting decision 3 allows only if Edge Services cannot
  redirect. Any other value is a configuration error (exit 2). It is a flag, not a record field, because
  ADR 0042's records carry no such field: the answer belongs to the edge's configuration, which the
  workflow that runs the check knows, not to the HSTS outcome. Milestone 4 passes `refuse-or-close` in
  the workflows only if it accepts that outcome (Hosting decision 3), and records it there.
- **The retry bound and rounds.** A deploy reaches visitors only when the HTML cached before it expires,
  so a failing round is repeated, the whole round, every `RetryInterval` (30 s in the program) until the
  bound, at least the HTML's `max-age` of **300 s** (default 600 s; less is a configuration error). A round
  that passes ends the run, and so does a round whose only failing checks are records that could not be
  read or a preload claim `hsts.json` does not back: the records are read once, before the first round, so no later round can change them, and
  waiting out the bound would only delay the report; the report holds the last round's checks and responses and the number of
  rounds. Every response records the URL, the remote address the connection reached and its `Age` header
  (absent and unparseable recorded as such, never failing by themselves). The clock, the sleep and the
  dialer are injected, so the tests run against local `httptest` servers, TLS included, and never wait.
- **The live version is untrusted text.** It is the text of the one element `id="site-version"` (the
  footer's version span of every page, read from the root, and only when the root's bytes are the
  manifest's `index.html`: a root that is not fails the version check without being scanned, so the scan
  only ever reads a page the release built; the scan is one linear pass, so even a hostile page could
  not make it super-linear), untrimmed, at most 64 bytes, and must match
  `^v[0-9]+\.[0-9]+\.[0-9]+$` exactly, equal an entry of the tag list and equal the manifest's version.
  The match returns a `Tag` that holds the tag list's own entry, never the page's text, and nothing else
  can construct a non-zero `Tag`; the commands of milestone 4 (`gh release download <tag> ...` and
  `gh attestation verify <bundle> --repo timothymamo/shift2eu --signer-workflow
  timothymamo/shift2eu/.github/workflows/release.yml --source-ref refs/tags/<tag>
  --deny-self-hosted-runners`) are built only from a `Tag`, as argv slices, never a shell string, and
  refuse a caller path that is empty or starts with `-`. The library runs none of them.
- **The size budget (issue #346 item 2, decision 4).** The module script is fetched like every manifest
  file, with `Accept-Encoding: gzip`, and measured twice: **transferred**, the raw body's bytes as the
  edge sends them, before content decoding; and **decoded**. Served with `Content-Encoding: gzip`, the
  transferred size must be at most **3 MiB (3,145,728 bytes)**, ADR 0035's budget, now measured at the
  edge rather than by our own gzip. The decoded size must always be at most **11 MiB (11,534,336 bytes)**,
  the uncompressed budget the PRD leaves to this ADR (maintainer's decision 4, 2026-11-23): the module
  script is 8.5 MiB today, so 11 MiB leaves about 2.5 MiB, some 29%, for growth before TinyGo is
  reconsidered. Served as identity, only the decoded budget applies (Hosting decision 4 accepts an edge
  that cannot serve it compressed), and the report says that the edge does not compress it. Both sizes
  and the encoding are in the report. Every body is read to at most 16 MiB, transferred and decoded alike,
  so a hostile endless body or a gzip bomb costs one byte over the cap, not the runner's memory.
- **The report.** A plain-text report: one `PASS`/`FAIL` line per check with its reason, the hosts, the
  rounds, one line per response, the module script's sizes, the version and a last line `result: PASS` or
  `result: FAIL`. Every line starts with a fixed word, and every piece of text taken from a response
  (headers, bodies, `Location`, server-supplied error text) is printed only through `livecheck.Quote`
  (Go's `%q` of its first 200 bytes), so a hostile header cannot add a line or start one with a workflow
  command (`::`); nothing from a response reaches a command, a file path or the environment. Exit codes:
  0 passed, 1 a check failed, 2 a usage or configuration error (one line on stderr, no connection made).

### The module script at this commit

Measured with Go 1.27.4 at this commit (`make site`, then the module script file of `bin/site`; and ADR
0035's test, `TestWebModuleStaysWithinTheSizeBudget`, which gzips with Go's default level):

| | Bytes |
|---|---|
| Raw module (ADR 0035's test) | 6,696,477 |
| Base64 module script (ADR 0035's test) | 8,928,668 |
| Module script file in `bin/site` (`module.<h>.js`) | 8,928,444 (8.51 MiB, 77% of the 11 MiB decoded budget) |
| Gzip-compressed, Go's default level (ADR 0035's test) | 2,511,273 (2.39 MiB, 80% of the 3 MiB budget) |
| Gzip-compressed, `gzip -6` of the file | 2,436,344 (2.32 MiB, 77%) |
| Gzip-compressed, `gzip -9` of the file | 2,408,638 (2.30 MiB, 77%) |

The edge serves the bytes the deploy uploads; the live check measures them as served.

## Alternatives considered

- **Trust ADR 0035's build-time gzip measurement alone.** Simple, but it measures our compressor, not the
  edge: an edge that serves the module script uncompressed, recompresses it or adds an encoding would go
  unnoticed (issue #346 item 2).
- **Follow redirects and compare the final page.** It would accept an edge that redirects a manifest file
  to another object or host, which is exactly what a misconfigured or tampered bucket can do
  (`x-amz-website-redirect-location`). Not following and treating any 3xx as a mismatch is stricter.
- **Judge each request once, without retries.** A release's check runs right after the deploy, while
  pages cached up to 300 s earlier are still served; it would fail on every release. Retrying single
  checks instead of whole rounds would mix two deploy states in one report.
- **Read the live version with a regular expression over the page, or trim it.** Lenient readings would
  let a page's text decide what reaches a command; the strict element read, the exact pattern and the
  `Tag` that only the tag list fills make that structural.
- **No uncompressed budget, or one equal to the 3 MiB transferred budget.** Without one, an
  identity-served module script could grow unchecked; at 3 MiB the current module script (8.5 MiB) would
  already fail. 11 MiB leaves room for growth and stays under the 16 MiB read cap.
- **A third-party HTML parser for the statements.** `golang.org/x/net/html` is the right tool for general
  HTML, but the check reads a few ids in pages we build; a small tokenizer in the standard library keeps
  the CI tool free of dependencies, and it errs towards not finding a statement (a failed check).

## Consequences

- The release's check (PR C of milestone 3) runs the `site-programs` job's `bin/sitecheck -manifest
  <manifest.json> -tags <file>` (amended in PR C2, see Shape), and milestone 4's scheduled workflow runs
  `go run ./ci/sitecheck live-version -tags <file> -anchor <time> -window 180m`, then
  `go run ./ci/sitecheck -manifest <manifest.json> -tags <file>`; both get a report and an exit code; the
  scheduled workflow's `gh` commands take their tag from the tag list's entry that `live-version` prints,
  never from the page (*Amendment: the scheduled check*).
- The pages carry `id="site-version"` on the footer's version span; `test/site` pins it in both builds.
- The records' format follows ADR 0042: a change to it there changes `livecheck`'s parsers and their tests
  with it. The plain-HTTP www mode stays the program's flag, so a change of the edge's answer is a
  change to the workflows' command line, not to a record.
- A change to either budget is a decision: `livecheck`'s tests pin 3,145,728 and 11,534,336 bytes. Above
  either, TinyGo is reconsidered, as the PRD decides.
- The apex's HSTS header is not judged unless preload is pursued; milestone 4 decides whether to pursue it.
- The check is only as good as its trust in the edge's TLS: it verifies against the system roots in
  production, and its own transport, not `http.DefaultTransport`, so no proxy or environment setting
  changes where it connects.

## Amendment: the container in front of Edge Services (PRD #297 milestone 4)

2026-11-24. The decision above is unchanged; the hosting it checks changed
([ADR 0045](0045-front-the-site-with-a-serverless-container-running-a-pinned-caddy.md), ADR 0042's
amendment of that day). Read the Context's "the hosting cannot set response headers" and "the apex is
served by a redirect of another provider" as Edge Services alone: a Scaleway Serverless Container
running Caddy now serves `www` and the apex, writes the redirects and sends HSTS and the four other
headers, and proxies the pipeline.

- **The platform's headers.** Serverless Containers adds `server: envoy` and
  `x-envoy-upstream-service-time` to every response (the maintainer's test C3). The check has no
  response-header allow list today (`ci/sitecheck/internal/livecheck` judges only the headers it names),
  so a later unit confirms that it tolerates exactly those two and pins that with a test; if an allow
  list is ever added, it allows exactly those two, not an `x-envoy-*` prefix. This amendment changes no
  code.
- **`-plain-http-www`** stays at `redirect`: the server redirects `http://www.shift2eu.com` itself, so
  the `refuse-or-close` mode is not needed by this hosting. Whether to keep or remove it is that later
  unit's.
- **The size budget** still measures what the deploy uploaded: the server proxies the pipeline's bytes
  and `Content-Encoding` unchanged and compresses nothing (ADR 0045).

## Amendment: the apex's HSTS stays recorded, not judged (issue #601)

2026-11-25. The maintainer decided that the apex's HTTPS 308 sends `Strict-Transport-Security:
max-age=31536000`, without `includeSubDomains` and without `preload` (ADR 0045, *The headers*; ADR
0042, *Decided 2026-11-25*). The live check's handling of the apex does not change: while preload is not
pursued, `ci/sitecheck` still only records the apex's header, and its code is unchanged.

- **Why not judge it.** The Caddyfile reaches the live site only when the maintainer applies
  `infra/site` (agents never plan or apply), independently of any release. A check that required the
  apex's value would fail the `site-check` job of a release that runs before that apply, though the
  release changed nothing about the apex.
- **What holds the value instead.** `test/sitefront` pins it: the static test of `infra/site/Caddyfile`
  under `make test` (exactly one `max-age=31536000` on the apex's HTTPS answers, none on a plain-HTTP
  one) and the behaviour test against the pinned Caddy image under `make test-sitefront`. After the
  apply, `docs/site-runbook.md` R8 reads it live.
- **When preload is pursued,** the apex's header is judged as the Decision says (`max-age` at least
  31536000, `includeSubDomains`, `preload`), and the apex's header and the preload record change in one change.

## Amendment: the scheduled check (PRD #297 milestone 4)

2026-11-26. The release's check runs once, right after a deploy. PRD #297's *Layer 3* also asks for the
same check every day, so that a page changed between releases (a leaked key, a changed bucket or
front) is noticed within a day. `.github/workflows/site-check.yml` runs it. The Decision above is
unchanged. This amendment adds a subcommand, a rule and a workflow.

- **Which release to check: `sitecheck live-version`.** The scheduled check has no release of its own,
  so it first asks the live site which release it serves:
  `sitecheck live-version -tags <file|-> -anchor <RFC 3339> -window <duration> [-host <host>]` makes one
  GET of `https://<host>/` with the check's own client (`livecheck.LiveVersion`). It reads the
  `id="site-version"` text with the same scanner and rules as `KindVersion` (`VersionText`,
  `MatchVersion`), judges it with the window rule below, and prints the tag list's own entry. It exits 1
  with one line on stderr for a refused version, a root that does not answer 200, or a version that is
  missing, hidden or malformed. It exits 2 on a usage error, before any connection. Response text
  reaches stderr only through `Quote`. Unlike `Run`, it compares no manifest, because which manifest to
  fetch is exactly what it answers. The full check that follows compares `/` with that release's
  `index.html`. The default command (`-manifest`/`-tags`) is unchanged.
- **The window rule (`livecheck.AcceptLive`).** The highest release tag in the list is always accepted.
  Tags are compared as versions, never as strings, so v0.10.0 is above v0.9.0, and entries that are not
  `vX.Y.Z` are ignored. The tag immediately below the highest is accepted only while the clock is before
  the anchor plus the bound. Everything else is refused: a release two behind, a version above the
  highest, one outside the list, the zero `Tag`, an empty list and a bound that is not positive.
- **The anchor** is the committer date of the commit the highest tag points to
  (`git log -1 --format=%cI <tag>`). The job can read it with `contents: read`, and it needs no
  `actions: read` to look at release runs.
- **The bound, 180 minutes** (`check.SiteCheckWindow`). It must cover the time from that commit to the
  end of the release's deploy. `check.MinSiteCheckWindow` computes that floor from the workflows
  themselves: release-please.yml's `release-please` and `tag` jobs (15 + 15 minutes), plus the longest
  `needs:` chain of release.yml's jobs ending at `site-check`, each job at its `timeout-minutes`
  (130 minutes today: test 20, site 15, goreleaser 15, image 20, sbom 10, sign 10, verify 10,
  site-deploy 15, site-check 15), plus the
  HTML's `max-age` (5 minutes). That is 165 minutes today. 180 leaves a 15-minute margin, and
  `TestSiteCheckWindowCoversTheRelease` fails once a timeout grows past it. No timeout bounds the time a
  run spends queued. A release queued past the bound fails the scheduled check once, which the alert
  reports like any other failure.
- **Two jobs, no default permission.** The workflow has one daily `schedule`, no other trigger, and
  `permissions: {}`. The `check` job holds `contents: read` and `attestations: read` alone. Its checkout
  keeps no credential (`persist-credentials: false`, the whole history for the tags), and it pins every
  action by commit. Its tag list is main's release tags, fetched and listed exactly as release.yml's
  `site-check` job reads them (main and the `v*` tags fetched, `git tag -l 'v*.*.*' --merged
  refs/remotes/origin/main`, `vX.Y.Z` alone, version-sorted), so a tag that is not on main is never
  the expected release. It reads that list and the anchor, runs `live-version` with the anchor passed
  through the step's `env:` (`ANCHOR`), never pasted into the script, and re-checks the printed
  tag against the tag list with `grep -qxF`. It downloads that release's site bundle and verifies its
  attestation with `GITHUB_TOKEN` alone, using the deploy's and the guide's flags (`--signer-workflow`
  release.yml, `--source-ref refs/tags/<tag>`, `--deny-self-hosted-runners`). It then runs the full
  check against the bundle's `manifest.json` and the tag list, at the default `-plain-http-www`. The
  `alert` job runs `if: failure()`, `needs: check`, holds `issues: write` alone and checks nothing out.
- **No environment.** Neither job names one. The workflow holds no secret but `GITHUB_TOKEN`, so the
  environments the release-tag skill's step 2 checks (`site`, `release-please`, `release-tag`,
  `ai-review`, `security-audit`) play no part in it. The site's Scaleway key stays in `site`, which only
  release.yml's `site-deploy` uses.
- **The alert issue, fixed text only.** On a failure, the alert job comments on the one open issue
  labelled `live site check`, or opens one assigned to the maintainer, `timothymamo`. GitHub notifies the
  assignee and each comment, but not an edit, and it notifies a failed scheduled run only to whoever last
  changed its cron line. The title and body are fixed text plus the failed run's URL. The job uses no
  output of the `check` job, no site content and no expression but the token, the repository, the
  server's URL and the run's id. Before it lists or opens the issue, the job creates the label with
  `gh label create --force`, which `issues: write` allows and which succeeds whether or not the label
  exists (it resets its colour and description), so the first failure is not lost to a missing label
  and no one creates it by hand. GitHub also disables a schedule
  after 60 days without repository activity, and R15 says how to re-enable it.
- **The workflow test.** `check.SiteCheckWorkflow` reads the workflow as a closed schema, through the
  release workflow's reader. `test/architecture` holds the repository's file to it, and the check's own
  test holds a fixture that is the file byte for byte. Every `run:` is compared exactly, and none may
  hold a `${{ }}` expression: the runner pastes its value into the script as code, so every value a
  step needs, the anchor included, reaches it through `env:`.
