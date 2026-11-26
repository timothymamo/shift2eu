# 0042. Host the site behind Edge Services and deploy it from the release with a bucket-scoped key

- Status: Proposed
- Date: 2026-11-23

Part of [PRD #297](../../prds/done/297-web-assessment-site.md) milestone 3. It records the hosting, the deploy,
the bundle's attestation, the HSTS and preload records' format, the origins and the leaked-key residuals.
[ADR 0039](0039-create-the-sites-scaleway-hosting-with-opentofu.md) records how the hosting is created;
this one records how the site is served and deployed on it. Milestone 4 amends it with what only the live
hosting can tell (marked *to be checked in milestone 4* below): the origins' plain-HTTP answers, the
key-limit step's result, the apex redirect's means and provider, and Edge Services' headers and
compression. The HSTS and preload outcome is settled (2026-11-25: HSTS met, preload not pursued; *The
HSTS and preload records*). Passages marked *proposed, for the maintainer to confirm* are values
the maintainer has not decided yet; the PR that lands each one (named there) checks it against this ADR.

## Context

Milestone 2 ([ADR 0036](0036-hold-the-site-to-three-trust-layers-and-test-them-in-a-headless-browser.md))
built a reproducible bundle of static files: four pages, four content-addressed assets (the stylesheet,
the page script, `wasm_exec.js` and the module script that embeds the WebAssembly module,
[ADR 0035](0035-run-the-assessment-in-the-browser-as-a-webassembly-driving-adapter.md)), the licence
and its attribution as plain text (`LICENSE` and `NOTICE`, issue #495) and a `manifest.json` of every
other file's path and SHA-256. Milestone 3 puts that bundle on `www.shift2eu.com` from the release, so
that what users run is what the release attested.

The forces:

- **The HTML is the root of trust and nothing pins it.** Whoever can write `index.html` can serve a
  look-alike page that sends inventories away. So the write path (the deploy key) is the asset to
  protect, and a changed page must be noticed (the live checks, PR B) and repaired (the redeploy).
- **Scaleway's IAM cannot name a bucket** (Hosting decision 5): Object Storage does not support
  resource-level conditions, so the key is scoped by a project that holds only the site's bucket, plus
  the bucket policy ADR 0039 writes.
- **The site sends no response headers of its own** (Hosting decision 2): Object Storage lets an object
  carry `Cache-Control`, `Content-Type`, `Content-Encoding` and `x-amz-meta-*`, and Edge Services
  documents no header rule. The CSP is a `<meta>` element; HSTS, if any, comes from Edge Services
  (milestone 4).
- **A release must never roll the site back, and a leaked key must never block the repair.** A guard
  that trusts the version the live page claims would let a leaked key write a page claiming
  `v999.0.0` and refuse every redeploy.
- **The deploy key must be in scope as briefly as possible**, on code this run built and attested.
- The release's live check and the scheduled one (PR B) read two records the maintainer writes in
  milestone 4, `site/records/hsts.json` and `site/records/preload.json`; their format is fixed here so
  that `ci/sitebuild` (which already reads `hsts.json`'s `outcome`), the live check and milestone 4
  agree.

## Decision

**The site is the release's attested bundle, served from a private Scaleway Object Storage bucket
through one Edge Services pipeline on `www.shift2eu.com`, and deployed only by `release.yml`'s `site`
deploy job, with a key that can write objects of that bucket and nothing else, by `ci/sitedeploy`, a
standard-library program that uploads the assets first and the pages last, never deletes, and refuses
only a rollback over a genuine higher release.**

### Hosting

As ADR 0039 creates it: the bucket (versioning on, website mode, `index.html` its index document) in a
Scaleway project that holds only the site, the bucket policy (the maintainer's `s3:*`, the deploy
application's `s3:PutObject` and `s3:GetObject` on `<bucket>/*` and `s3:ListBucket` on `<bucket>`, and
`*`'s `s3:GetObject` only with `public_read`), and the Starter plan's one pipeline: the bucket in website
mode as backend, a cache stage (`fallback_ttl = 300`, only for a response without `Cache-Control`; an
object's own `Cache-Control` takes precedence), a managed Let's Encrypt certificate, and
`www.shift2eu.com` as its one host. The apex `shift2eu.com` is not served by Edge Services (it cannot
serve an apex, Hosting decision 1); it answers with a permanent redirect to `https://www.shift2eu.com`
by a means milestone 4 chooses, and this ADR is amended with that means, its provider (what it sees:
the IP address, user agent, path and `Referer` of every visitor entering through the apex) and, if it
issues its own certificate, its certificate authority (for the CAA record).

*Amended 2026-11-24 (Edge Services answers plain HTTP with a 302 and sets no headers, ticket
#1639626):* a Serverless Container now serves both names in front of the pipeline; see the amendment
*A Serverless Container in front of Edge Services* below and
[ADR 0045](0045-front-the-site-with-a-serverless-container-running-a-pinned-caddy.md).

### The origins

Only `https://www.shift2eu.com` is attested and checked (Hosting decision 11). The bundle runs the same
on every origin (same-origin relative paths, the `<meta>` CSP, no fetch), so another origin serving it
is accepted; what each origin answers over plain HTTP is recorded here.

| Origin | Serves the site | Plain HTTP |
|---|---|---|
| `https://www.shift2eu.com` | Yes: the pipeline. The only origin attested and checked (PR B) | `http://www.shift2eu.com` must answer 301 or 308 to `https://www.shift2eu.com` with the same path, or refuse or close (*Layer 3*'s definition) if Edge Services cannot redirect; a 200 is a no-go (Hosting decision 3). *To be checked in milestone 4* (R8) |
| `https://shift2eu.com` | No: the apex redirect only, 301 or 308 to `https://www.shift2eu.com`, same path (the preload chain excepted, Hosting decision 2) | `http://shift2eu.com` must redirect the same way, never 200. *To be checked in milestone 4* (the means is milestone 4's) |
| The pipeline's endpoint `https://<pipeline id>.svc.edge.scw.cloud` | Yes, as `www` does, on its own origin, under either read rule | *To be checked in milestone 4* (R5) |
| The bucket website endpoint `https://<bucket>.s3-website.<region>.scw.cloud` | Only with `public_read` (the private read failed in R5); otherwise it must refuse the probe (403) | *To be checked in milestone 4* (R5) |
| The bucket's S3 endpoints `https://<bucket>.s3.<region>.scw.cloud/<key>` and `https://s3.<region>.scw.cloud/<bucket>/<key>` | Object by object, without the index document, only with `public_read`; otherwise 403 | *To be checked in milestone 4* (R5) |

Milestone 4 replaces each *to be checked* cell with R5's and R8's recorded answer (status and redirect
target, per URL) and `public_read`'s value. A plain-HTTP 200 on an origin other than `shift2eu.com` and
`www.shift2eu.com` is recorded, not a no-go: no link leads there, and a user who types such a URL is
outside what the site attests.

*Amended 2026-11-24:* the amendment *A Serverless Container in front of Edge Services* below replaces
the first two rows and adds one for the container's own endpoint.

### The bundle's attestation

The release builds the bundle with `make site` at the tag (reproducible, ADR 0036), runs the
headless-browser smoke test on it, and publishes `shift2eu-site_<version>.tar.gz` as a release asset,
listed in `checksums.txt` and attested with GitHub build provenance in the same job and with the same
pinned action that attests the archives (`attest-archives`). The archive holds the bundle and its
`manifest.json`; the manifest's digest of the module script is of the decoded file, the bytes the
bundle holds.

As built (PR C1): the `site` job ("Site bundle") builds the archive and gives its SHA-256 as a job
output; `site-browser` ("Site browser smoke test") unpacks that very archive, after checking it against
the digest, and runs `make test-browser TEST_BROWSER_BUNDLE=<dir>` on it; `goreleaser` checks it against
the digest before GoReleaser lists it in `checksums.txt` and uploads it to the draft (`checksum` and
`release` `extra_files`); `attest-archives` holds `checksums.txt` to exactly one bundle line with that
digest and attests it with the archives; and `publish` needs `site-browser`, so a bundle that failed
its smoke test leaves the release a draft. None of the site jobs holds a key, a secret or an OIDC token.

`attest-archives` attests the bundle before, and independently of, the smoke test, so a draft whose
bundle failed `site-browser` can still hold a build-provenance attestation for it: an attestation says
where the bundle was built, never that it passed its smoke test. Publication is the gate (`publish` needs
`site-browser`), so the deploy (PR C2) takes the bundle only from a published, non-draft release, never
from a draft or on the strength of an attestation alone. `attest-archives` deliberately does not need
`site-browser`, which would lengthen the chain of job timeouts before the draft is attested. The smoke
test gates the release's publication, not the container image: the image is pushed even when
`site-browser` fails, as [ADR 0015](0015-publish-the-signed-image-to-ghcr-with-docker-buildx.md)
already accepts for a failed `attest-archives` or `publish`.

The deploy job verifies the bundle before the key is in scope: it takes the bundle archive from the
published release asset, which never expires, and the deploy program's binary as an artifact of this
run, checks both SHA-256 digests against the outputs of the jobs that produced them (the bundle's against
the `site` job's), and runs `gh attestation verify <archive> --repo <owner>/shift2eu
--signer-workflow <owner>/shift2eu/.github/workflows/release.yml --source-ref refs/tags/<tag>
--source-digest <commit> --deny-self-hosted-runners` with the job's `GITHUB_TOKEN`. Only then does the one step that holds the
key run. The guide (`docs/guide/verify-releases.md`, PR D) and the live checks (PR B) use the same flags.
The release's live check (PR C2) takes the bundle from the published release asset too, checked against
the `site` job's digest, whose attestation the deploy job verified in the same run.

As built (PR C2): both jobs first refuse a release that is still a draft (`gh release view <tag> --json
isDraft` must be `false`), then download `shift2eu-site_<tag>.tar.gz` from it, and check the bundle and
their program in one `sha256sum -c` against `needs.site.outputs.sha256` and the `site-programs` job's
output for that program. The deploy's `gh attestation verify` adds `--source-digest "$GITHUB_SHA"` to the
flags above, binding the bundle to this run's commit as `verify-archives` binds the archives; it is
stricter than the list this ADR first gave. The manifests of the higher releases (below) are verified
with the same flags but their own `--source-ref`, and without `--source-digest`, since that release's
commit is not known there.

### The deploy job (PR C)

- **Where:** a job of `release.yml`, `environment: site`, after `verify`, `verify-archives`, the bundle's
  attestation and the deploy program's build, and only when the `latest` job says the tag is the highest
  release tag on `main` and the repository variable `SITE_DEPLOY_ENABLED` is `true`. Otherwise it is
  skipped with a visible notice in the run's summary, and the live check after it is skipped with it.
  `check.ReleaseWorkflow` pins both conditions. The maintainer sets the variable last in milestone 4
  (R10), after the records are in a release tag.
- **The `site` environment** holds the key as the environment secrets `SCW_ACCESS_KEY` and
  `SCW_SECRET_KEY`, never at repository level or in another environment. Its deployment policy is
  exactly one custom policy of type `tag`, named `v*.*.*` (`custom_branch_policies` `true`,
  `protected_branches` `false`; type `tag`, so a branch named like a tag is not allowed). The `v*` tag
  rulesets ([ADR 0021](0021-propose-releases-with-release-please-and-tag-with-a-github-app.md)) let only
  the release-tag App and the maintainer create such a tag. The `release-tag` skill's step 2 checks all
  of it (PR C). The environment admits any `v*.*.*` tag, and the workflow that runs is the tagged
  commit's, so those tag rulesets are the one control on the deploy key; the key is bucket-scoped, so
  what a misused tag could reach is the site's content. A required reviewer or a wait timer on the
  `site` environment would add defence in depth; it stays the maintainer's option, not taken here,
  because it would add a second human gate to every release (PRD #161).
- **The job does not build.** A job without secrets builds `ci/sitedeploy` with the pinned toolchain;
  the deploy job downloads it, and the bundle from the published release, checks the digests and the
  attestation (above), and runs
  no third-party action after that. It checks out nothing, or with `persist-credentials: false`; its
  permissions are `contents: read` and `attestations: read`; the two secrets are the `env:` of the one
  step that runs `sitedeploy`, never of the job.
- **One deploy at a time** (confirmed by the maintainer, 2026-11-23; PR C2 writes it): the job carries
  `concurrency: {group: site-deploy, cancel-in-progress: false}`, so a re-run and a new release's
  deploy cannot interleave their uploads.
- **`release.yml` lands in PR C**, with every job given a `timeout-minutes` (below).
- **As built (PR C2):** four jobs, which `check.ReleaseWorkflow` pins as a closed schema.
  - `site-programs` ("Site programs", needs `test`, `contents: read`, no secret) builds `ci/sitedeploy`
    and `ci/sitecheck` with `make site-programs` (static, `-trimpath`), gives their SHA-256 as outputs and
    hands each on as an artifact of its own, kept 30 days, GitHub's re-run window.
  - `site-deploy` ("Deploy the site") needs `latest`, `verify`, `verify-archives`, `attest-archives`,
    `site`, `site-browser` and `site-programs`, runs on exactly `needs.latest.outputs.latest == 'true' &&
    vars.SITE_DEPLOY_ENABLED == 'true'`, and has five steps, the deploy last: the program's artifact; the
    bundle's draft, digest and attestation checks (above); the release tags on `main`, by the `latest`
    job's filter and sort, read at deploy time from a treeless fetch, so a higher release published since
    `latest` decided is in the list; the higher releases' manifests; and `sitedeploy -bundle bundle
    -version "$TAG" -bucket "$SITE_BUCKET" -region "$SITE_REGION" -releases releases.txt -manifests
    manifests`.
  - **The bucket and region** are the variables `SITE_BUCKET` and `SITE_REGION` of the `site`
    environment, not secrets, set by the maintainer from `tofu output` in milestone 4 and checked by the
    `release-tag` skill's step 2; no bucket name is written into `release.yml`. The deploy step's `env:`
    is exactly the two keys, the tag and those two variables: no `GH_TOKEN`. `check.ReleaseWorkflow`
    holds each key's secret to that one line of the whole document, the variables to it, the switch to
    the three `if:` lines and `site-skipped`'s `env:`, and refuses any line that names `HTTPS_PROXY`,
    `HTTP_PROXY`, `NO_PROXY`, `SSL_CERT_FILE` or `SSL_CERT_DIR`.
  - **The higher releases' manifests.** Without them the guard could never refuse a rollback. For each
    tag of the list above `TAG` (by `sort -V`), the step reads that release: an HTTP 404 (not published
    yet: its run is queued behind this one and deploys after it), a release still a draft, and a release
    without `shift2eu-site_<tag>.tar.gz` each give a `::notice::` and are skipped; any other failed read
    fails the step. Otherwise it downloads that bundle, verifies its attestation and extracts its
    `manifest.json` into `manifests/<tag>.json`. The tags come from the list alone, never from a page or
    an API answer. Normally the tag is the highest and the directory stays empty.
  - `-list-bucket` stays off: R7 has not recorded "ListBucket allowed" (milestone 4).
  - `site-check` ("Check the live site") needs `latest`, `site`, `site-programs` and `site-deploy`, on
    the deploy's exact condition, with `contents: read` alone and no environment; it checks out the tag
    for `site/records/`, checks the published bundle and its program against their digests, and runs
    `sitecheck -manifest bin/manifest.json -tags bin/releases.txt` at its defaults (`-plain-http-www`
    stays at `redirect` until milestone 4).
  - `site-skipped` ("Site deploy skipped", needs `latest`, `permissions: {}`) runs on the exact negation
    of that condition and says why in a `::notice::` and the run's summary, never printing the
    variable's value. It is a job of its own, because a step-level condition would still start the `site`
    environment's job, with a deployment, on every release.

### The key's scope

The key belongs to the IAM application ADR 0039 creates. Its IAM rule grants
`ObjectStorageObjectsWrite` and `ObjectStorageObjectsRead` in the site's project only, and the bucket
policy allows that application `s3:PutObject` and `s3:GetObject` on the site's objects and
`s3:ListBucket` on the site's bucket, nothing else: no delete, no tagging, no ACL, no bucket
configuration, no bucket policy, no other bucket, project or product, no Edge Services permission (so no
cache purge). The key is created by hand with an expiry (90 days, R6), its default project the site's
(the S3 calls resolve the bucket in the key's default project), rotated before it expires (R11), and its
limits are probed after each creation and every change to the IAM rule or the bucket policy (R7, below).

### The deploy program

`ci/sitedeploy` (CI tooling, standard library only, depguard `site-deploy-main` and
`site-deploy-library`, like `ci/aireview`,
[ADR 0033](0033-review-pull-requests-with-an-ai-reviewer-that-only-comments-or-requests-changes.md)) speaks
the S3 API path-style to `https://s3.<region>.scw.cloud`, a fixed template (no flag can send the key to
another host; the region is held to `^[a-z]{2}-[a-z]{3}$`), signing each request with AWS Signature
Version 4 written by hand (Hosting decision 8: the AWS CLI and `s3cmd` are unpinned on the runners or
need Python, and would hold the key). The signer is pinned by the AWS documentation's three published S3
example signatures (the GET object, the ListObjects GET and the PutObject of a path holding `$`); it
canonicalises the path itself, each segment URI-encoded once from the decoded path, and sends the
listing's query exactly as it canonicalises it (a space as `%20`, a plus as `%2B`). It follows no
redirect, sends only `GET` (the live page, the listing) and `PUT` (a file of the bundle), and never
`DELETE`. It refuses, before any request, a configuration it cannot trust (an endpoint with a path,
query or user info, a bucket that is no S3 bucket name, a version or a release that is no `vX.Y.Z` tag,
a missing key, client, writer or clock) and a bundle whose manifest does not parse strictly, names a
path that is not one of the bundle's fixed flat names (so no `/`, `..`, `\`, `%`, control character,
absolute or empty key), that does not list exactly the four pages, one asset of each kind (style, page
script, `wasm_exec`, module), `LICENSE` and `NOTICE`, whose asset names do not match their digests,
whose files differ from their digests, or that holds a file the manifest does not list or one that is
not a regular file. It uploads the bytes it checked, held in memory, never a second read of the bundle.

Everything it reads from outside is bounded: a manifest at 1 MiB, a bundle file at 32 MiB and the bundle
at 64 MiB (memory bounds, not the site's size budgets), the live page at 1 MiB, an error answer at
64 KiB (read only for its code), a listing page at 8 MiB and the listing at 1,000 pages.

**The upload order:** every content-addressed asset first, then `manifest.json`, then the four pages
with `LICENSE` and `NOTICE` (issue #495; names without a digest, like the pages'), each group in sorted
order. The first failed upload stops the run and fails the job: a failure before the pages leaves
every page as it was, and the earlier release's assets are never deleted, so the pages still served
keep working (no file of an earlier release is ever deleted; pruning them is a proposed follow-up, by
hand or with another key). `manifest.json` is uploaded before the pages, so between it and the last
page the live `manifest.json` already names the new release while some pages are still the old one,
and a failure in that window leaves it so (the live `manifest.json` naming the new release over the
old pages) until the redeploy (Recovery), which `verify.html` reports in the meantime; the live checks
compare against the attested manifest, never the live one, so the window only delays a green check.

**The cache headers and content types**, per file:

| File | Content-Type | Cache-Control | Content-Encoding |
|---|---|---|---|
| `*.html` (the four pages) | `text/html; charset=utf-8` | `public, max-age=300` | none |
| `manifest.json` | `application/json` | `public, max-age=300` | none |
| `LICENSE`, `NOTICE` (issue #495) | `text/plain; charset=utf-8` | `public, max-age=300` | none |
| `style.<h>.css` | `text/css; charset=utf-8` | `public, max-age=31536000, immutable` | none |
| `page.<h>.js`, `wasm_exec.<h>.js` | `text/javascript; charset=utf-8` | `public, max-age=31536000, immutable` | none |
| `module.<h>.js` | `text/javascript; charset=utf-8` | `public, max-age=31536000, immutable` | `gzip` |

`<h>` is the first 16 hexadecimal characters of the file's SHA-256, so a changed asset gets a new name
and an immutable one is never stale; the pages' `max-age` of 300 seconds lets a release show within five
minutes without a cache purge, and is the bound the live checks hold the pages to. Every `PUT` carries
`X-Amz-Content-Sha256`, the SHA-256 of the bytes sent, which the store checks.

**The module script is uploaded gzip-compressed** (Hosting decision 4): `sitedeploy` compresses it with
`compress/gzip` at `gzip.BestCompression`, with no name and a zero modification time, so one bundle and
one toolchain give the same bytes (the release builds `sitedeploy` with go.mod's pinned toolchain; a
later toolchain may give other gzip bytes, which changes nothing a check compares, since the manifest's
digest and the SRI hash are of the decoded file). The level is the highest because the transferred size
is the budget's (3 MiB, ADR 0035) and the size test's default-level measurement is then an upper bound
of what is served. Browsers decode it before SRI and before the page script reads it. Whether Edge
Services serves it with `Content-Encoding: gzip` and `Content-Type: text/javascript` unchanged, and
whether it compresses the other files itself, was left *to be checked in milestone 4*. **Checked by
v0.16.1's release live check** (run 45692962040, `result: PASS`, 2026-11-25): it measured the module
script's transferred and decoded size and its `Content-Type` as `www` serves them (ADR 0043's size
check) and held every manifest file's decoded digest, so both budgets and `Content-Type:
text/javascript` held as served, and no file came with an encoding the check refuses. The sizes and
the encoding it saw are in that run's report, not recorded here (issue #346, item 2).

**The uncompressed budget: the decoded module script at most 10 MiB (10,485,760 bytes)** (*proposed, for
the maintainer to confirm*; PR B holds it). Decision 4 as amended has the live check hold both the
transferred size (3 MiB, ADR 0035) and the decoded size. Measured at 076e8e1 with Go 1.27.4: the raw
module 6,696,477 bytes, the decoded module script 8,928,668 bytes (85% of 10 MiB), its gzip at the
default level 2,511,276 bytes (80% of 3 MiB). The script compresses 3.56 to 1, so the 3 MiB transferred
budget corresponds to about 10.7 MiB decoded; 10 MiB is the round number just under that, so an edge
that serves the script uncompressed is held no looser than one that serves it gzipped, and the decoded
size, which the browser holds as a string and decodes before compiling, has a bound of its own whatever
the edge does. It leaves about 1.5 MiB of decoded growth, about 1.1 MiB of raw module, against the
transferred budget's about 2.2 MiB decoded: the decoded budget fails first, by design, while the module
grows. Above it, TinyGo is reconsidered, as above the 3 MiB budget. PR B's live check fails above either
budget; the same 10 MiB is held offline by a second assertion beside
`TestWebModuleStaysWithinTheSizeBudget`'s (proposed for PR B).

**The rollback guard.** Before any upload, `sitedeploy` reads `https://www.shift2eu.com/` (a constant)
once, unsigned, following no redirect, and reads the version its footer shows (the one
`<span class="mono" id="site-version">` that ends the `footer-links` paragraph). That text is untrusted. It **refuses**
(`ErrRollback`, nothing uploaded) only when all three hold: the version matches
`^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$` and equals a tag of the repository's
own release tags on `main` (as the `latest` job lists them, passed in with `-releases`); that tag is
higher than the bundle's; and the SHA-256 of the live body equals the `index.html` entry of that tag's
manifest (passed in with `-manifests`, a file `<tag>.json` per release with a site bundle, opened by a
name built from the tag list, never from the page, and parsed as strictly as the bundle's own).
`-releases` must hold only `vX.Y.Z` tags, one per line (blank lines and CRLF ends are ignored): any other
line refuses the whole deploy before a request. The strictness is deliberate, so PR C's release workflow
must write exactly the `latest` job's tags there and nothing else. In every other case it overwrites,
and says so in the run's summary:

- with a plain **notice** (`> [!NOTE]`) when the live page is a genuine release not higher than the
  bundle's (the normal upgrade, and the redeploy of the same release): its version is a known tag and
  its body matches that tag's manifest;
- with a **visible warning** (`> [!WARNING]`) naming the case otherwise: no answer or a non-200 answer
  (a redirect included), a page over 1 MiB, no version or an unparseable one, a version that is no known
  tag, a tag with no manifest or one that does not parse or names another version, or a body that does
  not match its tag's manifest (a page a leaked key could have written, whatever version it claims).

Both say that the deploy overwrites the live page and name its version where there is one. So a forged
higher version cannot block the redeploy that repairs the site, and a genuine higher release is never
overwritten by a lower one (the `latest` condition already keeps a lower tag's run from deploying; the
guard holds when a run is re-run after a higher release went live). The guard reads through the edge
cache, so it may see a page up to 300 seconds old; the `latest` condition covers that window. The first
deploy finds no site (an error page or 404) and overwrites with the warning.

**The listing, behind `-list-bucket`, off by default.** With the flag, `sitedeploy` lists the bucket
(`ListObjectsV2`, every page, following `NextContinuationToken`) before any upload and fails
(`ErrUnknownObject`, nothing uploaded) naming each key that is in no release's manifest (every file of
every `<tag>.json` of `-manifests` for a tag of `-releases`), not in this bundle and not `manifest.json`:
a page a leaked key planted at a new path is then found at the next release. A listed key is only
compared, never sent, so a hostile key (`../x`, `/etc/passwd`, `a%2Fb`) cannot become a write; it is
scrubbed of the key, then named in the error escaped and cut to 64 bytes, at most 20 of them (the
smallest; only those and a count are kept, whatever the listing's length). A listing that fails (a
transport error, a status other than 200, an answer that does not parse, a listing over its bounds)
fails with `ErrList`, never `ErrUpload`: nothing was uploaded, and the repair is the listing's grant,
not the upload's. Once `-list-bucket` is on, PR C must pass in `-manifests` the manifest of every
release ever deployed, not only the recent ones: a release whose manifest is missing has its
still-present assets counted as unknown keys, which fails the deploy. It is off by default because it
depends on runbook step R7 showing that IAM lets the application list with only its two object
permission sets (ADR 0039, open point 3): the bucket policy grants `s3:ListBucket`, but whether IAM does
is known only from R7's probe. PR C passes the flag only once R7 recorded "ListBucket allowed" here
(milestone 4); a key that fails the listing would otherwise fail every deploy. With the flag on, a key
the runbook left behind (an aborted R5 or R7 probe) also fails the deploy until the maintainer deletes it
with their own key, which is intended. The scheduled live check holds no secret and does not list.

**The key in the program:** read from `SCW_ACCESS_KEY` and `SCW_SECRET_KEY` only (the one
`os.LookupEnv`, forbidigo `site-deploy-env`), held in a type whose every printed form is `[redacted]`
(and the entry point's settings print themselves redacted, since `fmt` does not reach a field's own
`Format` through an unexported field), never in an error, a log line or the summary. Errors name the
request (method, key, status) and at most an S3 error code of `[A-Za-z]{1,64}` that holds neither value,
never a response body or header. A transport error is written as a fixed classification (`timed out`,
`cancelled` or `transport error`), never the HTTP client's message, which can quote what a server sent
(a malformed status line); this holds for the uploads, the listing and the guard's read of the live
page alike. Errors wrap only the program's sentinel errors (and the context's), never an error whose
text a server wrote; and every text written is also scrubbed of both values, so a server that echoes
the key gets nothing printed.

**The transport, for PR C.** `sitedeploy` sends through Go's default transport, which honours
`HTTPS_PROXY` (and `HTTP_PROXY`, `NO_PROXY`) and `SSL_CERT_FILE` / `SSL_CERT_DIR` from the environment.
A step that could set them could route the signed requests through a proxy of its choosing or make it
trust another certificate authority. So the deploy job's steps before the deploy run no third-party
code and write nothing to `GITHUB_ENV` (or `GITHUB_PATH`), and the key is set only in the `env:` of the
deploy step itself. As built (PR C2), the deploy's client has a transport of its own whose `Proxy` is
nil (`newHTTPClient` in `ci/sitedeploy`), so the proxy variables are never consulted, and
`check.ReleaseWorkflow` refuses any line of `release.yml` that names them or the certificate variables.

### Recovery

- **Redeploy the last good release:** re-run the deploy job of the `release.yml` run of the highest
  release tag on `main` ([the site runbook](../site-runbook.md#r12-recovery), R12), within GitHub's
  30-day re-run window (below); after the window, either
  the maintainer restores the last good version of each page from the bucket's object versions with
  their own key (R12), or cuts a new release with a `Release-As:` footer.
  The guard overwrites any live page that is not a genuine higher release,
  with a warning, so a page a leaked key wrote is replaced whatever version it claims. Redeploying the
  same release is the same re-run: the guard sees an equal version and overwrites, with a notice.
- **A deploy that failed part-way:** `manifest.json` is uploaded before the pages, so a failure
  between the two leaves the live `manifest.json` naming the new release over the old pages until the
  redeploy (the re-run above), and `verify.html` reports the mismatch until then. A failure before
  `manifest.json` leaves only unreferenced new assets.
- **Roll back to an older release:** the deploy refuses it by design while a genuine higher release is
  live. The maintainer rolls back with their own key instead: with versioning, each of the four pages,
  `LICENSE`, `NOTICE` and `manifest.json` copied from its earlier version (R12); the earlier release's
  assets are still in the bucket, since nothing is ever deleted. The live checks then fail until the
  expected version is live again, which the alert reports; a rollback is followed by a fixing release.
- **Either is followed by a manual Edge Services cache purge** by the maintainer, whose credentials hold
  the Edge Services permission the deploy key lacks (R12). Browsers may still keep a released page
  served from a poisoned cache for up to its `max-age` (300 seconds).
- **The artifacts' retention and the recovery after they expire** (confirmed by the maintainer,
  2026-11-23; amended in PR C1): the bundle's workflow artifact (`site`) is kept 7 days, as the
  archives artifact is, and only the jobs of its own run read it: `site-browser`, `goreleaser` and
  `attest-archives`. The deploy and the live check (PR C2) take the bundle from the published release
  asset instead, which never expires, checked against the `site` job's digest and its attestation. The
  re-run of the deploy job therefore never needs the bundle's artifact, and **the bundle job is never
  re-run after publication**: a GitHub re-run of a job also re-runs every job that needs it, so a re-run
  of `site` would re-run `goreleaser` (which would upload to a published, immutable release),
  `attest-archives` and `publish` (which refuses a tag that already holds a published release). This
  replaces the earlier plan of re-running the bundle job once its artifact had expired. As built (PR
  C2): GitHub re-runs a run's jobs only within 30 days of the initial run, so the programs' artifacts
  are kept exactly 30 days; within that window, re-run `Deploy the site` (which re-runs `Check the live
  site`); after it nothing of the run can be re-run, and the recovery is either the maintainer's
  restore of the last good version of each page from the bucket's object versions with their own key
  (R12), or a new release cut with a `Release-As:` footer. `Site bundle` is
  never re-run after publication. The programs' artifacts are kept 30 days, the bundle's 7 (*proposed*,
  measured on the first release).
- *Amended 2026-11-24:* the front (the container, its image and its configuration) has a recovery of
  its own; see the amendment *A Serverless Container in front of Edge Services* below.

### `timeout-minutes` (values for PR C)

*Proposed, for the maintainer to confirm.* `release.yml` sets none today. Every job gets one,
`check.ReleaseWorkflow` pins them, and the scheduled check's version window (PR B, milestone 4) is
bounded by their longest chain. The last release run (v0.12.0, 2026-11-23) took 8 minutes in all, its
longest job (Test) 3 minutes.

| Job | Minutes | Job | Minutes |
|---|---|---|---|
| `test` | 20 | `image` | 20 |
| `latest` | 5 | `sbom` | 10 |
| `goreleaser` | 15 | `sign` | 10 |
| `attest-archives` | 10 | `verify` | 10 |
| `publish` | 10 | `verify-archives` | 10 |
| `site` (the bundle's build) | 15 | `site-programs` (the programs' build) | 10 |
| `site-browser` (the bundle's smoke test) | 30 | `site-skipped` (the skip notice) | 5 |
| `site-deploy` (the deploy) | 15 (`sitedeploy`'s own deadline is 10, each request 2) | `site-check` (the live check) | 15 (its retry bound at least the pages' 300 seconds) |

PR C1 sets the first twelve (`release.yml` from `test` to `verify-archives`, with `site` and
`site-browser`), PR C2 the last four (`site-programs`, `site-deploy`, `site-check` and `site-skipped`),
and `check.ReleaseWorkflow` pins all sixteen. The bundle's build and smoke test, first proposed
as one job of 20 minutes, are two jobs: `site` 15, and `site-browser` 30, as ci.yml's `browser` job
(the headless Chromium's image pull and the smoke test's own 20-minute `go test` timeout).

`goreleaser` now needs `site`, and `publish` needs `site-browser`, so the longest chain to the end of the
live check (PR C2) is `test`, `site`, `site-browser`, `publish`, `verify-archives`, the deploy, the live
check: 115 minutes with PR C2's jobs (the earlier chain through the image and `verify` was 100). With release-please's
`release-please` and `tag` jobs (15 + 15) and the pages' `max-age` (5), the version window's floor is
150 minutes (it was 135). The real durations are measured on the first release that runs the site jobs;
milestone 4 adds the margin for queueing and pins the number.

### The HSTS and preload records (milestone 4 creates them)

**The formats are confirmed by the maintainer (2026-11-23)** (PR B reads them, milestone 4 writes them).
The outcome, whether preload is pursued and the subdomains were decided on 2026-11-25 (*Decided
2026-11-25*, below).
Both live under `site/records/`, which `ci/sitebuild` never bundles. Both are one JSON object of exactly
the fields below, read strictly: no unknown, missing or duplicate field, field names matched exactly
(case-sensitive), no trailing data, at most 4 KiB. A missing or unreadable record makes the live checks
fail safe (*Layer 3*); for `ci/sitebuild`, a missing `hsts.json` reads as not waived, as today.

`site/records/hsts.json`:

```json
{
  "format": 1,
  "outcome": "met",
  "preload": false,
  "date": "2026-12-01",
  "reason": ""
}
```

| Field | Values | Meaning |
|---|---|---|
| `format` | `1` | The record's format; a reader refuses another. |
| `outcome` | `"met"` or `"waived"` | `met`: `https://www.shift2eu.com/` sends `Strict-Transport-Security` with `max-age` at least 31536000 and `includeSubDomains` (plus `preload` when `preload` is true), which the live checks then require whatever the pages say. `waived`: the maintainer waived HSTS; the privacy and verification pages carry the `hsts-waived` statement (`ci/sitebuild` shows it), which the live checks then require. |
| `preload` | `true` or `false` | Whether preload is pursued. `true` only with `outcome` `met`; then the apex's header is held to the same minimum with `preload`, and `http://shift2eu.com` must redirect to `https://shift2eu.com` first (Hosting decision 2). |
| `date` | `YYYY-MM-DD` | The day the outcome was read from the live headers, before the first deploy from the release (`SITE_DEPLOY_ENABLED`). |
| `reason` | a string of at most 500 printable characters | Empty when `met`; required when `waived`: why. |

`site/records/preload.json`:

```json
{
  "format": 1,
  "listed": false,
  "submitted": "",
  "checked": "2026-12-01",
  "subdomains": []
}
```

| Field | Values | Meaning |
|---|---|---|
| `format` | `1` | As above. |
| `listed` | `true` or `false` | Whether `shift2eu.com` is on the HSTS preload list of shipping browsers: `true` only once hstspreload.org reports it preloaded and the current stable Chrome and Firefox ship it. While `false` (and when the record is missing) the live checks require the `downgrade-risk` statement on the privacy page. `true` requires `hsts.json`'s `preload` `true` and a `submitted` date. |
| `submitted` | `YYYY-MM-DD` or `""` | The day the domain was submitted to the preload list; empty when it was not. |
| `checked` | `YYYY-MM-DD` | The day `listed` was last checked. |
| `subdomains` | an array of host names under `shift2eu.com`, sorted, each once | The subdomains that existed when the domain was submitted (such as `"www.shift2eu.com"`); empty when not submitted. `includeSubDomains` binds each of them, and every future one, to HTTPS. |

Pursuing preload is effectively irreversible: removal takes months to reach shipping browsers, and
`includeSubDomains` binds every present and future subdomain of `shift2eu.com`. Milestone 4 decided
whether it is pursued, the subdomains at submission and the HSTS outcome or waiver (below), amends this
ADR with them and commits both records in a first PR whose squash commit carries
`Release-As: <next version>`, so a release tag contains them before `SITE_DEPLOY_ENABLED` is set.

**Decided 2026-11-25** (the maintainer, PRD #297 milestone 4):

- **HSTS: met.** `strict-transport-security: max-age=31536000; includeSubDomains` was read live on
  `https://www.shift2eu.com/` during the first deploy's checks (R8, ADR 0045) on 2026-11-25, before the
  release deploy is enabled (`SITE_DEPLOY_ENABLED`): the header the container front sends (the amendment
  *a Serverless Container in front of Edge Services*, below). `site/records/hsts.json` records
  `"outcome": "met"`, `"preload": false`, `"date": "2026-11-25"` and an empty `"reason"`; the pages
  carry no `hsts-waived` statement.
- **Preload: not pursued.** Pursuing preload is effectively irreversible: removal from the browsers'
  preload lists takes months to reach shipping browsers, and `includeSubDomains` under preload binds
  every present and future subdomain of `shift2eu.com` to HTTPS. HSTS on `www` alone meets the site's
  go/no-go without that commitment. As read live on 2026-11-25, the apex's 308 sent no HSTS, as ADR 0045
  then configured (until issue #601: the next item), and `http://shift2eu.com` needs no first hop to
  `https://shift2eu.com` while preload is not pursued.
- **The apex: `max-age=31536000` on its HTTPS 308** (decided 2026-11-25, issue #601). The alternative
  ADR 0045 named is taken: the apex's HTTPS 308 sends exactly one `Strict-Transport-Security:
  max-age=31536000`, without `includeSubDomains` and without `preload`, so a returning visitor who types
  `shift2eu.com` is held to HTTPS; the plain-HTTP 308 sends none; `www` is unchanged. It changes neither
  record: `preload` stays `false`, and the live check still only records the apex's header (ADR 0043,
  amendment of 2026-11-25; ADR 0045, *The headers*).
- **No subdomains submitted.** `site/records/preload.json` records `"listed": false`, `"submitted": ""`,
  `"checked": "2026-11-25"` and an empty `"subdomains"` array (`[]`); while `listed` is false the live checks require the
  `downgrade-risk` statement on the privacy page, as the table above says.

Pursuing preload later is a new decision: it changes both records (`preload` true, then a `submitted`
date and the subdomains at submission) and amends this ADR again.

### The leaked-key residuals

A leaked deploy key, until it expires (at most 90 days) or is revoked (R11), can still:

- **overwrite any page or asset** of the site's bucket, and write a page that sets its own caching (up
  to a year) and registers a same-origin service worker, both of which can outlast the repair in a
  visitor's browser (the site cannot send `Clear-Site-Data`);
- **plant objects at paths outside every manifest**, such as a look-alike page at a new path, or an
  object carrying `x-amz-website-redirect-location` that the website backend answers with a redirect
  to another host;
- **read and list** the bucket's objects (`GetObject`, `ListBucket` if IAM grants it): the same files
  `www` serves, so nothing secret;
- **fill the bucket** with objects the site's project is billed for.

It cannot delete an object or a version (no delete permission, verified by R7, and versioning keeps
every overwritten version for the rollback), change the bucket's website, versioning, ACL, policy,
CORS, lifecycle or object lock, reach another bucket, project or product, or purge or change the Edge
Services pipeline. What bounds the rest:

- **The rollback guard** trusts only a genuine higher release, so a forged page never blocks the
  repair.
- **The manifests:** the release's live check and the scheduled one (daily) compare every file of the
  live release's attested manifest, `/` against its `index.html`, and a few well-known service-worker
  paths, and fail on a 3xx where a manifest file is expected. A changed page is noticed within a day.
  A tampered asset under its content-addressed name is also refused by the browser: every script and the
  stylesheet are loaded with an SRI hash the page carries.
- **The service worker:** the genuine page script unregisters every service worker for the origin when
  it loads (ADR 0036), which helps only visitors whose browser loads a genuine page.
- **The privacy statement:** the privacy page, under "What the site cannot promise", says "The site
  is only the files named in a release's manifest: any other path on the host, even one that answers,
  is not part of the site.", and that the live checks compare only the manifests' files, the root and
  the service-worker paths, so a look-alike page at a new path uploaded with a leaked key is not
  detected by them (pinned by `test/site`'s `TestPrivacyPageNamesThePathsOutsideTheManifests`). Once
  the listing is on, milestone 4 may add that the next release's deploy fails on any object outside
  every release's manifest.
- **ListBucket**, if R7 shows IAM grants it and the deploy then passes `-list-bucket`: a path outside
  the manifests is found at the next release, not by the daily check. *Amended 2026-11-26:* the
  maintainer decided to keep `-list-bucket` off (the amendment *The first deploy*, below), so this
  bound does not hold: a path outside every manifest stays undetected by the deploy as well as by the
  live checks.

### The key-limit step (R7): result

*Recorded in milestone 4: R7 ran on 2026-11-24, its probe to a third project's bucket on 2026-11-26*
(issue #637; and R7 runs again after every key rotation and every
change to the IAM rule or the bucket policy). Per probe, the answer and, for anything but
`403 AccessDenied` (or `NoSuchBucket` for a bucket outside the key's project), the reason it was
accepted; and whether `ListBucket` was allowed, which decides `-list-bucket` above:

| Probe | Answer |
|---|---|
| `PutObject` on `canary/probe.txt` | Allowed (the deploy key's job) |
| `ListObjectsV2` on the site's bucket | Allowed. The maintainer kept `-list-bucket` off all the same (2026-11-26; the amendment *The first deploy*, below): the deploy works without it |
| the four re-submissions on the site's bucket (`put-bucket-website`, `put-bucket-versioning`, `put-bucket-acl`, `put-bucket-policy`) | `PASS 403`, each |
| the eleven object probes on `canary/probe.txt` (`delete-object` with and without a version, `delete-object-tagging` and `put-object-tagging` with and without a version, `put-object-acl`, `put-object-retention`, `put-object-legal-hold`, `abort-multipart-upload`, `restore-object`) | `PASS 403`, each |
| the nine canary-bucket probes (`delete-bucket-website`, `delete-bucket-policy`, `delete-bucket`, `put-bucket-versioning` to `Suspended`, `put-object-lock-configuration`, `put-bucket-cors`, `put-bucket-lifecycle`, `put-bucket-tagging`, `put-bucket-encryption`) | `PASS 403`, each |
| the two probes outside the project: `list-buckets` | `PASS 403` |
| the two probes outside the project: `put-object` to a bucket in another project | `PASS 403` (2026-11-26, issue #637): `canary/probe.txt` to the one empty bucket of a throwaway third project, made for the probe with the maintainer's own key; the bucket held nothing afterwards, and the bucket and the project were deleted. Skipped on 2026-11-24 (no bucket in another project was given); with the state bucket's denied `put-object` below, a write to the admin project, the key is shown unable to write outside the site's project for two other projects |
| the three on the state bucket (`get-object` of the state, `list-objects-v2`, `put-object`) | `PASS 403`, each |

After the probes, the step's closing `tofu plan` (the maintainer's own key) failed to release its state
lock (`DeleteObject` 403), right after the script switched the key's default project back to the admin
project: most likely the switch had not taken effect yet (issue #537). The maintainer force-unlocked the
state, and `plan -detailed-exitcode` then exited 0. Nothing in the probes' answers depends on it.

The third-project probe (2026-11-26) showed two things about the maintainer's own key, now in R7's step
7: it reaches a bucket in another project only with its default project switched to that project (its
S3 calls go to its default project), and `scw object bucket delete` answered 403 on the throwaway
bucket, which `aws s3api delete-bucket` with the same key then deleted (confirmed by the maintainer on
2026-11-26): it is the command R7 uses.

## Alternatives considered

- **The AWS CLI or `s3cmd` for the upload** (Hosting decision 8). Unpinned on GitHub's runners or a
  Python install, and a large third-party program holding the key.
- **A rollback guard that trusts the live page's version.** A leaked key could then block every repair
  by writing a page claiming a high version.
- **No guard, `latest` alone.** `latest` stops a lower tag's run, but not a re-run of an older run's
  deploy job after a higher release went live; the guard costs one GET.
- **A warning on every overwrite.** The normal upgrade would then warn on every release, and a warning
  that always shows is one nobody reads; a notice for the genuine case keeps the warning for the cases
  that need a look.
- **Deleting the earlier release's files after the upload.** It would make a failed upload break the
  pages still served, and need a delete permission a leaked key would then hold too.
- **Listing the bucket by default.** It fails every deploy if IAM does not let the application list
  (ADR 0039, open point 3); behind a flag, it waits for R7's answer.
- **Serving the module script uncompressed and letting Edge Services compress it.** Edge Services
  documents no compression setting, and what it would do is unknown until milestone 4; a pre-compressed
  object makes the transferred bytes the deploy's own, and the decoded budget holds the other case.
- **gzip at the default level.** A few percent larger on the wire, against a budget the transferred size
  is held to; `BestCompression` costs a second or so once per deploy.
- **The key at repository level, or in the job's `env:`.** Every job, or every step of the deploy job,
  could read it; in the `site` environment, deployable only from a `v*.*.*` tag, and in one step's
  `env:`, it is in scope for one process.
- **An uncompressed budget equal to the transferred one's equivalent (about 11 MiB).** Close to the
  proposal; 10 MiB keeps the uncompressed case no looser than the compressed one. The maintainer may
  prefer 11 MiB (11,534,336 bytes), which fails at about the same module size as 3 MiB gzipped.

## Consequences

- A release from PR C's merge on builds, attests and publishes the bundle, and deploys it only once the
  maintainer sets `SITE_DEPLOY_ENABLED` (milestone 4); until then the deploy and its live check are
  skipped visibly, by the `site-skipped` job's notice.
- The maintainer sets the `site` environment's variables `SITE_BUCKET` (`tofu output`'s
  `bucket_name`) and `SITE_REGION` (R0's region: `infra/site`'s `terraform.tfvars`, or
  `variables.tf`'s default; `tofu output` has no region) before the switch
  ([R9](../site-runbook.md#r9-the-github-site-environment)); the `release-tag` skill reports them missing as "not set up yet" while the
  switch is unset, and stops the release once it is `true`.
- The deploy key is in scope for one step, on a program and a bundle this run built and attested; a
  failed deploy fails the release run and leaves the previous release serving.
- The bucket grows by one bundle per release (a few MiB), never pruned by the deploy.
- `ci/sitedeploy` holds the whole bundle in memory (a few tens of MiB at most, bounded at 64 MiB).
- PR B's live check holds the transferred (3 MiB) and decoded (10 MiB) module script sizes, the cache
  headers, the redirects and HSTS against the records; PR C writes `release.yml`, the timeouts, the
  `site` environment's checks in `release-tag` and the artifacts' retention; PR D writes the runbook's
  R12 rollback for all four pages, `LICENSE`, `NOTICE` and `manifest.json`, and the guide.
- Milestone 4 amends this ADR: the origins' plain-HTTP answers, R7's result and the `-list-bucket`
  decision, the apex redirect's means, provider and certificate authority, Edge Services' headers and
  compression, the HSTS outcome or waiver, whether preload is pursued and the subdomains at submission
  (these last three settled on 2026-11-25: met, not pursued, none; *The HSTS and preload records*).

## Amendment: a fifth page, about.html (issue #528)

2026-11-24. Issue #528 adds an About page (ADR 0036, amendment for issue #528). The decision above is
unchanged; where it says "four pages", read the five pages from that issue on.

- **The bundle has five pages:** `about.html`, `index.html`, `inventory.html`, `privacy.html` and
  `verify.html`. `ci/sitedeploy`'s bundle check requires exactly those five, with one asset of each kind
  and the two legal texts, and uploads them last as before, with the `*.html` headers of the table above.
  `ci/sitecheck` refuses a release manifest that lists no `about.html`, as it refuses one without
  `index.html`, `privacy.html` or `verify.html`.
- **Rolling back across issue #528:** a rollback stays the maintainer's R12 restore with their own key,
  as Recovery (above) says; the deploy refuses it by design. To a release before issue #528, the restore
  copies the four pages that existed then, with `LICENSE`, `NOTICE` and `manifest.json`, from their
  earlier versions; `about.html` has no earlier version to copy. R12 takes the pages to restore and
  check from the good release's manifest, and its headers check knows `about.html`, so the same block
  serves a rollback to either side of issue #528. The `ci/sitecheck` of a release at or
  after issue #528 refuses the restored manifest, which lists no `about.html`, so the live check fails
  until a fixing release ships, as Recovery expects.
- **The newer About page outlives such a rollback:** the deploy never deletes, and an earlier release has
  no `about.html` to restore, so the newer release's `about.html` stays in the bucket and is still served,
  with that release's footer version and links, beside the older pages. The maintainer removes it by hand
  (their own key, then an Edge Services cache purge, R12) when it should not stay; the fixing release that
  follows a rollback uploads it again.

## Amendment: a Serverless Container in front of Edge Services (PRD #297 milestone 4)

2026-11-24. Scaleway support (ticket #1639626) answered that Edge Services always answers plain HTTP
with a 302, cannot turn plain HTTP off and cannot set response headers, so `http://www.shift2eu.com`
could meet neither Hosting decision 3 nor the HSTS go/no-go. The maintainer chose a Scaleway Serverless
Container running Caddy in front of the pipeline (Hosting decisions 1 to 3 as amended that day, after
the maintainer's test C3), recorded in
[ADR 0045](0045-front-the-site-with-a-serverless-container-running-a-pinned-caddy.md). The bucket, its
read rule, the deploy, the key's scope, R7, the attestation, the records and the residuals above are
unchanged; only *Hosting*, *The origins* and *Recovery* change, as follows.

- **Hosting.** One public Serverless Container serves both `www.shift2eu.com` and `shift2eu.com` as
  custom domains (`www` by a Namecheap CNAME, the apex by a Namecheap ALIAS, DNSSEC kept, CAA
  unchanged), with plain HTTP `enabled`. The apex's certificate was issued by Let's Encrypt under the
  existing CAA record (C3); `www`'s is expected from the same issuer (Scaleway's Serverless Containers
  FAQ) and is checked at the first deploy (R8). Its server, Caddy (the
  official image pinned by digest, ADR 0045), writes the redirects and the headers from its
  configuration in `infra/site`: `http://www.shift2eu.com`, `http://shift2eu.com` and
  `https://shift2eu.com` answer 308 to `https://www.shift2eu.com` with the same path in one hop, by
  `X-Forwarded-Proto` and `Host`; `https://www.shift2eu.com` is reverse-proxied to the pipeline endpoint
  and carries `Strict-Transport-Security` (`max-age=31536000; includeSubDomains`, `preload` only once
  pursued), the `<meta>` policy as a `Content-Security-Policy` header with `frame-ancestors 'none'`,
  `X-Content-Type-Options`, `Referrer-Policy` and `Permissions-Policy`. The apex sent no HSTS; decided
  2026-11-25 (issue #601), its HTTPS 308 sends `max-age=31536000`, without `includeSubDomains` or
  `preload`, once `infra/site` is applied, and its plain-HTTP 308 none (ADR 0045, *The headers*). The pipeline stays the bucket's only reader, its cache and its backend
  unchanged; its own `www.shift2eu.com` custom domain goes; the infra unit removes it (and its TLS
  stage) in `infra/site`. The apex redirect's provider is therefore
  Scaleway, already named on the privacy page, with Let's Encrypt as its certificate authority. Context's
  force "the site sends no response headers of its own" holds for the bucket and the pipeline only: the
  headers now come from the container.
- **The origins.** The table above reads, from this amendment on:

  | Origin | Serves the site | Plain HTTP |
  |---|---|---|
  | `https://www.shift2eu.com` | Yes: the container, proxying the pipeline. The only origin attested and checked | `http://www.shift2eu.com` answers the server's own 308 to `https://www.shift2eu.com` + same path, never a 200 (ADR 0045's rule and its tests; R8, rewritten by the infra unit, records the live answer) |
  | `https://shift2eu.com` | No: the server's 308 to `https://www.shift2eu.com` + same path (the preload chain, if pursued, first to `https://shift2eu.com`) | `http://shift2eu.com` answers 308 the same way, never a 200 (ADR 0045's rule and its tests) |
  | The container's endpoint `https://<namespace>-<name>.functions.fnc.<region>.scw.cloud` | No: every request gets the server's 308 to `https://www.shift2eu.com` + same path | The server's own 308, never a 200 (ADR 0045's rule and its tests) |
  | The pipeline's endpoint, the bucket website endpoint and the bucket's S3 endpoints | As in the table above | As in the table above (R5) |

  "Never a 200" over plain HTTP is a property of ADR 0045's rule (the joined value of the raw
  `X-Forwarded-Proto` header compared exactly with `https`, so two fields or a comma list fail closed,
  else the 308; not Caddy's `header` matcher, which accepts any one field) and of its tests, not a
  result of C3: C3's redirect container answered 308 unconditionally, and showed only that the
  platform passes the container's 308 and headers unchanged over plain HTTP and HTTPS.

  The pipeline's endpoint is now the container's upstream as well as an origin of its own; it serves the
  site without the container's headers, which the `<meta>` policy and the framing guard cover as on any
  other origin (Hosting decision 11).
- **Recovery.** The front is part of what is recovered. The bucket's recovery above (the redeploy, the
  version rollback, the Edge Services cache purge) is unchanged; Caddy caches nothing, so no second
  purge. A broken or changed container, or a configuration or image that is not the committed one, is
  repaired by the maintainer re-applying `infra/site` at `main` with `dev/tofu.sh` (the pinned digest and
  the committed configuration), then the runbook's redirect and header checks. A bad image (a Renovate
  digest bump that fails) is rolled back by reverting that PR and applying again. The deploy key cannot
  change the container: it holds no Serverless Containers or Container Registry permission.
- **What the live check now meets.** The platform adds `server: envoy` and
  `x-envoy-upstream-service-time` to every response (C3); a later unit confirms that `ci/sitecheck`
  tolerates exactly those two and pins it with a test (ADR 0043's note: no response-header allow list
  exists today; if one is ever added, exactly those two, not an `x-envoy-*` prefix). Its
  `-plain-http-www` stays `redirect`.
- **Consequences and the residuals.** Milestone 4 still amends this ADR with the live answers (R5, R8)
  and R7's result; the HSTS outcome (met) and preload (not pursued) are settled (2026-11-25, *The HSTS
  and preload records*). Once the header CSP is live, it also binds
  a page or script a leaked key uploads at any path (`connect-src 'none'`, `worker-src 'none'`), which
  narrows the residuals above; that amendment says so with the live result.

## Amendment: the first deploy (PRD #297 milestone 4)

2026-11-26. The maintainer's live answers at the first deploy (2026-11-24 to 2026-11-26), read through
the first-deploy runbook; ADR 0045's amendment *Settled live* holds the front's own answers. The
decision above stands; this records what milestone 4 was to record.

- **The origins (R5, `public_read = true`), with the container's endpoint.** Answers per URL, the same
  as on 2026-11-24:

  | Origin | HTTPS | Plain HTTP |
  |---|---|---|
  | The pipeline's endpoint `https://<pipeline id>.svc.edge.scw.cloud` | 200 | 302 to HTTPS |
  | The bucket website endpoint `https://<bucket>.s3-website.<region>.scw.cloud` | 200 | 200 |
  | The bucket's S3 endpoints | 200 | 200 |
  | The container's endpoint `https://<namespace>-<name>.functions.fnc.<region>.scw.cloud` | `308 https://www.shift2eu.com/<key>` | `308 https://www.shift2eu.com/<key>` |
  | `https://www.shift2eu.com` and `https://shift2eu.com` | ADR 0045, *Settled live* | 308 to `https://www.shift2eu.com/`, the path and query kept |

  The plain-HTTP 200s of the bucket's endpoints are recorded, not a no-go (*The origins*, above): only
  `https://www.shift2eu.com` is attested and checked. This answers every *to be checked* cell of both
  origins tables.
- **R7's ListBucket: `-list-bucket` stays off.** The maintainer decided (2026-11-25/26) to keep the
  deploy's `-list-bucket` off; the deploy works without it (v0.16.1, below). So a path outside every
  manifest, such as a look-alike page a leaked key plants, is detected neither by the deploy nor by the
  live checks (*The leaked-key residuals*, amended above); the privacy page's statement that such a path
  is not part of the site stays the bound, and the "next release's deploy fails on any object outside
  every release's manifest" sentence is not added.
- **HSTS: met; preload: not pursued.** The records shipped in v0.16.0: `site/records/hsts.json` with
  `"outcome": "met"`, `"preload": false`, `"date": "2026-11-25"`; `site/records/preload.json` with
  `"listed": false`. v0.16.0's site jobs ran with the deploy still off (Site programs succeeded, the
  deploy and the live check skipped). The apex's HTTPS 308 sends `max-age=31536000` without
  `includeSubDomains` since PR #618 was applied (2026-11-26; ADR 0045, *Settled live*).
- **The first deploying release: v0.16.1.** With `SITE_DEPLOY_ENABLED=true` (set 2026-11-25) and the
  `site` environment complete (its deployment policy `tag v*.*.*`), v0.16.1's run deployed the site
  (*Deploy the site* succeeded) and its live check passed (*Check the live site*, `result: PASS`),
  with notices only in the run's summary. `www` then served v0.16.1's `manifest.json` with the
  container's headers.
- **The header CSP is live.** Every answer of `www` carries the header CSP with `frame-ancestors 'none'`
  (ADR 0045, *Settled live*), so a page or script a leaked key uploads at any path is held to
  `connect-src 'none'` and `worker-src 'none'` on `www`. The pipeline's default endpoint stays the
  residual ADR 0045 records: it serves every object without the header policy.
- **The read rule (R5, 2026-11-24): `public_read = true`.** With `public_read` false, Edge Services
  answered 403 with the bucket's own access-denied page in the website endpoint's HTML form
  (`Code: AccessDenied`, an S3 request ID), on the first try: it cannot read private objects through the
  website endpoint. So `public_read = true`, Hosting decision 11's fallback; R4 applied the bucket-policy
  change and the next plan showed no changes. With it, Edge Services answered 200 (`x-cache: miss`, the
  bucket's `x-amz-request-id`, the body as written). The runbook's R5 recognised only the S3 XML form of
  `AccessDenied`; it now recognises the HTML form too (issue #535).
- **R8 on Edge Services alone (2026-11-24): the no-go.** The `www` CNAME, the CAA records (exactly
  `0 issue "letsencrypt.org"` and `0 issuewild ";"`) and the DS record passed, but
  `http://www.shift2eu.com/` answered 302, which the go/no-go refuses, and Scaleway support's answer
  (ticket #1639626, the amendment *A Serverless Container in front of Edge Services*, above) closed every
  way around it. That no-go led to ADR 0045's container front; R8's answers on the front are ADR 0045's
  *Settled live*.
- **R9 (2026-11-24): the `site` environment, PASS.** Every check passed: a custom branch policy only, the
  deployment policy exactly tag `v*.*.*`, the environment's secrets exactly `SCW_ACCESS_KEY` and
  `SCW_SECRET_KEY` (none at repository level), its variables `SITE_BUCKET` and `SITE_REGION`
  (`nl-ams`), the `v*` tag rulesets with no bypass for the release-please App, and the skipped site jobs
  of a release run before the switch.
