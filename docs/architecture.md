# Architecture

`shift2eu` follows a hexagonal (ports and adapters) architecture with a small, explicit domain model.
The decision and its alternatives are in [ADR 0001](adr/0001-hexagonal-architecture-with-provider-neutral-taxonomy.md).

## Layers and the dependency rule

```
            ┌──── cmd/shift2eu, cmd/shift2eu-server, cmd/shift2eu-web (composition roots) ───────────────┐
            │                                                                                      │
            │   internal/cli ──────▶ internal/app/analysis ◀────── internal/adapter/...           │
            │   internal/httpapi ──┤                                                               │
            │   internal/web ──────┘                                                               │
            │   (driving adapters)     (use case + ports)          (driven adapters: implement     │
            │                                 │                     the ports)                     │
            │                                 ▼                           │                        │
            │                         internal/domain/... ◀───────────────┘                        │
            │                         (stdlib only, no I/O)                                         │
            └──────────────────────────────────────────────────────────────────────────────────────┘
```

Arrows are imports. They point inwards only:

- **Domain** (`internal/domain/...`) imports only other domain packages and an explicit allow list of
  pure standard packages. No I/O, no `context`, no clock, no logging. This is where the value of the
  project lives, so it is the most heavily unit-tested code.
- **Application** (`internal/app/analysis`) orchestrates a use case and declares the interfaces (ports)
  it needs, next to where they are consumed. It never imports an adapter. Its tests use fakes, not
  adapters: `app-tests-use-fakes-not-adapters` holds its `_test.go` files to a minimal strict allow list
  (issue #144) of the application package, the domain, `internal/testutil`, `go-cmp` and the standard
  packages the tests import (`bytes`, `context`, `errors`, `io`, `slices`, `strings`, `sync`,
  `testing`), so a standard package on no list, one added to Go later included, is refused as well.
  `internal/testutil`'s own imports are a strict allow list too (`testutil-imports`, its tests
  included), and the forbidigo group `testutil-no-env` keeps its helpers from reading the environment,
  locating the home, config, cache or temp directory, or starting a process. These rules are a
  guardrail on imports and named calls, not a sandbox: the I/O they leave an application test is what
  `internal/testutil` can do through `os` (file access, the golden files), `fmt`, `time` and `flag`,
  and what `testing` itself offers (`t.TempDir` and test output). The forbidigo group
  `tests-no-process-state` (issue #153) refuses `t.Setenv` and `t.Chdir` there, and in the tests of
  the driving adapters and the composition roots (Composition-root process and environment rule below).
  No lint rule can refuse a hard-coded path, so any new file path or `os`, `fmt`, `time` or `flag`
  call in `internal/testutil` is a widening a reviewer must read. `test/architecture`
  holds it package by package for every release target, as it holds the other strict rules
  ([ADR 0012](adr/0012-check-layer-dependencies-by-package-for-every-release-target.md)).
- **Adapters** (`internal/adapter/...`) implement ports. Each cloud SDK is confined to one adapter
  package, and its types are translated to domain types at the boundary. They never import the CLI
  or `cmd/`. Helpers that adapters share live in `internal/adapter/internal/`: every adapter that
  reads a JSON document (the inventory file, the target catalog snapshots) decodes it through the
  strict decoder `adapter/internal/jsondoc`
  ([ADR 0009](adr/0009-decode-adapter-json-documents-through-one-strict-decoder.md)). A helper does
  no I/O unless that is decided: lint holds every package there to an allow list of packages that
  do no I/O, and a helper that needs I/O needs a lint rule of its own. Lint names each helper an
  adapter may import (`adapters-are-built-in-cmd`), never by prefix, so lint flags any adapter that
  imports a helper it does not name, such as one under a `testdata` or `_` directory, which lint
  never visits. The same rule stops any package but the two composition roots from importing an
  adapter: its deny list names each adapter subtree (`report/`, `request/`, `source/`, `target/`)
  beside `internal/`, and the package at each subtree's root by an exact entry (`report$`, ...),
  which the `/` entry does not cover, because depguard compares an import only with the entry that sorts nearest
  before it and a broad `internal/adapter/` entry was shadowed by `internal/adapter/internal/`
  (issue #119). `test/architecture` fails on a list entry that is a strict prefix of another and on
  a directory under `internal/adapter` the rule does not name; `test/lint` probes the rule with
  depguard. The adapters that must stay offline (the inventory-file reader, the snapshot reader, the
  embedded-snapshot targets and the reporters) each name what they import in a strict allow list. A reporter writes only to the
  `io.Writer` it is given: it reads no file, no environment and no clock, and makes no network
  call. The live sources share one strict allow list, `cloud-adapters-reach-the-cloud-through-the-sdk`
  (Cloud read-only rule below).
- **CLI** (`internal/cli`) turns user input into use-case calls and prints results. No business rules.
  Only `cmd/shift2eu` imports it. Its imports are a strict allow list (`cli-gets-adapters-from-cmd`,
  issue #152): the application, the domain, `internal/version` and exactly the standard packages it
  imports today, `os` among them for the files the user names; the forbidigo group
  `cli-no-env-or-process` keeps it from reaching the environment through `os` (Composition-root
  process and environment rule below).
- **HTTP API** (`internal/httpapi`) is the second driving adapter, beside the CLI
  ([ADR 0019](adr/0019-serve-assessments-over-a-versioned-http-api-from-a-separate-binary.md)): it serves
  the same use cases over `/v1/` and returns an `http.Handler`. Like the CLI it gets every adapter from
  its composition root through `httpapi.Deps` and imports only the application, the domain,
  `internal/version` and a named list of standard packages (`httpapi-gets-adapters-from-cmd`), never an
  adapter, the CLI, `cmd/` or `os`. Strict JSON decoding stays in the adapters, because `jsondoc` is
  internal to `internal/adapter`: the request body is decoded by the adapter
  `adapter/request/assessmentrequest` (`request-decoders-parse-bytes`: `errors`, `fmt` and `jsondoc`
  only, no `net/http`), and the inventory in it by `inventoryfile.FromBytes`, both passed in as function
  values. `FromBytes` reads in an in-memory-only mode whose errors name a position and field, never a
  value, so no value from a request is ever echoed in an API error. Only `cmd/shift2eu-server` imports it,
  and of the tests only the parity test in `test/apiparity` (`httpapi-imported-only-by-the-server`);
  `cmd/shift2eu` never does, so the CLI stays offline. `api/openapi.json` (OpenAPI 3.1) describes the API
  and refers to the JSON report's schemas by relative path rather than copying them; a document that
  bundles them, for readers outside the repository, is not published yet
  ([issue #139](https://github.com/timothymamo/shift2eu/issues/139)). The release ships the server
  as its own binary and its own image, `ghcr.io/timothymamo/shift2eu-server`, built from the
  `Dockerfile`'s `server` stage and signed and attested as the CLI's is (ADR 0013, ADR 0015);
  [Behind a reverse proxy](guide/reverse-proxy.md) documents running it behind a reverse proxy.
- **Web adapter** (`internal/web`) is the third driving adapter, the logic the site's page calls
  ([ADR 0035](adr/0035-run-the-assessment-in-the-browser-as-a-webassembly-driving-adapter.md), PRD #297):
  it takes an inventory document and the options the user chose and returns the Markdown report and the
  JSON report the page offers as downloads, byte for byte what `shift2eu assess` writes, and the HTML
  fragment the page shows, rendered by the reporter `adapter/report/htmlreport` (held by
  `reporters-stay-offline`). It runs in the browser, compiled to WebAssembly, so no inventory leaves it.
  It checks its options by the same application rules as the CLI (`analysis.SelectTargets` and its
  siblings, which moved out of `internal/cli`; each driving adapter keeps its own parsing), runs the use
  case once with one reporter that renders every format, and gets every adapter through `web.Deps`. Its
  imports are a strict allow list (`web-gets-adapters-from-cmd`): the application, two domain packages,
  `internal/version` and a named list of pure standard packages, never `os`, `net`, `syscall/js`, an
  adapter, the CLI or the HTTP API. Only the site's composition root and its wiring import it or the HTML
  reporter (`web-imported-only-by-the-web-root`).
- **`cmd/shift2eu-web`** is the site's composition root, in two parts. The package itself (`js && wasm`)
  is the thin `syscall/js` glue: it registers one JavaScript function that calls `internal/web`, and it is
  the only package that imports `syscall/js` (`web-composition-root`, `syscall-js-only-in-the-web-root`).
  Its untagged package `cmd/shift2eu-web/internal/wire` builds the adapters `cmd/shift2eu`'s offline path
  builds, the inventory reader in its in-memory mode and the HTML reporter, into `web.Deps`
  (`web-composition-root-wire`). It builds and is tested natively. Its tests, alone allowed to import
  `internal/cli` (`web-composition-root-wire-tests`), hold the option parity and the download parity with
  the CLI. `make lint` and CI's lint matrix lint `js/wasm` as an extra target beside the release platforms,
  and `test/architecture` holds the module's dependency tree to no `net`, `net/http`, `os/exec` or cloud
  SDK and the gzip-compressed module script to 3 MiB.
- **The site** (PRD #297 milestone 2, ADR 0036) is outside the Go layers. `site/` holds its pages, its
  stylesheet and its one page script, which loads the module and is the only JavaScript of the site's own.
  `ci/sitebuild`, the program `make site` runs, is CI tooling held to the standard library
  (`site-builder`). It assembles the reproducible bundle: content-addressed files, the module embedded in a
  script of one fixed template, the Subresource Integrity values and the version. Three layers hold the
  promise that nothing leaves the browser. Layer 1 is `test/site`'s banned-API scan of the page script and
  of the glue's allow list. Layer 2 is each page's Content Security Policy (`connect-src 'none'`,
  `img-src 'none'`, `worker-src 'none'`, Trusted Types with the one policy `shift2eu-report`). Layer 3 is
  the reproducible, attested bundle. `test/browser`, behind `browser && !js`, drives the built bundle in a
  headless Chromium with `chromedp`, which depguard confines to it (`chromedp-only-in-the-browser-test`,
  `browser-smoke-test`); `make test-browser` runs it against a container pinned by digest, with DevTools on
  127.0.0.1 only, and CI runs it as its own job.
- **`cmd/shift2eu`** and **`cmd/shift2eu-server`** are the composition roots, the only places that construct
  adapters and inject them: `cmd/shift2eu` into the CLI, `cmd/shift2eu-server` into the HTTP API. `cmd/shift2eu`'s
  imports are a strict allow list (`composition-root`, issue #101): the module's own packages and the
  standard packages it needs today (`context`, `os`, `os/signal`, `syscall`), and no third-party package.
  The rule holds `cmd/shift2eu` only. `cmd/shift2eu-server` (PRD #103 milestone 3) is held by its sibling,
  `server-composition-root`, written the same way: the module's own packages and the standard packages
  the server needs (`context`, `errors`, `flag`, `fmt`, `io`, `log`, `math`, `net`,
  `net/http`, `os`, `os/signal`, `runtime`, `runtime/debug`, `slices`, `strconv`, `strings`, `sync`,
  `syscall`, `time`), no third-party package and no cloud SDK. `test/architecture` holds it to that
  list on every release target, and `TestServerPathHasNoCloudSDKOrProcess` (`test/e2e`) refuses a cloud
  SDK or `os/exec` anywhere in its dependency tree. Around the handler, `cmd/shift2eu-server` owns what a
  running server needs: the flags and the token (from `SHIFT2EU_SERVER_TOKEN` or `--token-file`, never a
  flag), the loopback rule, the `http.Server` timeouts, the `Host` allow-list, the concurrency default,
  the readiness check, the access log and the graceful shutdown (ADR 0019).

The rule is enforced by `depguard` in `.golangci.yml`, so `make lint`, which lints the host natively and
then every GOOS/GOARCH `.goreleaser.yaml` builds with cgo off, fails on a violating import. These are all
the ways code could be hidden from it, and what refuses each:

- A comment. Every file is linted, also one that looks generated, and `test/lint` fails on any nolint
  directive that would hide a depguard issue (one naming `depguard`, `all` or no linter) and on a build
  constraint that negates the `e2e` or `integration` tag (lint sets both, so it would never read that
  file).
- A line directive. depguard matches its file globs against the position a `//line` directive names,
  so `test/architecture` (`TestRepositoryHasNoLineDirectives`) fails on any line directive in a Go file
  git tracks or would add.
- A symbolic link. golangci-lint lints a linked Go file with the nolint directives of its target.
  `TestRepositoryHasNoLineDirectives` fails on any symlink git tracks or would add, and `test/lint`
  fails on any Go file the `./...` pattern reaches that is a symlink, git-ignored ones included, as
  deliberate defence in depth.
- A file name. `test/architecture` checks every directory a strict depguard rule names (the domain, the
  application layer and its tests, the snapshot reader, the adapter helpers, the inventory file reader, the
  request decoders, the HTTP API and the targets that embed a snapshot) package by package, for every platform `.goreleaser.yaml` builds. It
  reads those directories from `.golangci.yml`, lists them with `go list` and holds each package to the
  allow list of every rule that covers it, and it holds every package of the module in the release
  binaries, `shift2eu` and `shift2eu-server`, to Go sources in their own directory, so assembly, cgo, line directives and symlinked
  directories cannot get past the file-name-based rules
  ([ADR 0012](adr/0012-check-layer-dependencies-by-package-for-every-release-target.md)).
- An edit to the rules themselves. A merge conflict resolution once dropped the whole
  `reporters-stay-offline` rule and the exact `jsondoc$` allow entry, and no check noticed (issue #80).
  `test/architecture` (`TestDepguardRulesArePinned`) now pins every depguard rule: its list-mode, its
  files and allow lists and the `pkg` of each deny entry, against a copy in
  `test/architecture/depguardpin_test.go`. Adding, removing or changing a rule means updating
  `pinnedDepguardRules` in the same commit, on purpose, so review sees it as a change to the
  architecture rules. `TestDepguardPinCatchesMutations` proves the pin notices, with 633 mutations of
  the real file: for every rule it deletes the rule and each of its files, allow and deny entries
  (every deny entry, a shared list's too), drops the trailing `$` of each exact allow entry, adds an
  allow entry `os` and a negated files glob, and rewrites, deletes or adds the list-mode. A self-check
  fails if the walk misses an entry, and a document the reader refuses counts as caught only for the
  one mutation expected to break it: deleting `app-uses-ports-not-adapters`, which removes the
  `&app-deny` anchor a later alias reads.
- A setting that hides issues or turns depguard off. The pin covers the rules, not whether they run
  or what lint reports. `test/architecture` (`TestGolangciSettingsHideNoIssue`, issue #81) reads
  `.golangci.yml` with the same strict reader and fails, naming the setting, on a non-empty
  `linters.exclusions.paths` or `paths-except`; a `linters.exclusions.generated` that is missing or
  not `disable` (absent, golangci-lint v2 reads it as `lax`, which drops every issue, depguard's and
  forbidigo's included, in a file with a "code generated" comment); any `issues` setting but a value
  that reports every issue (`max-issues-per-linter` and `max-same-issues` absent or 0, where
  golangci-lint's defaults are 50 and 3 and 0 means unlimited; `new`, `whole-files` and `fix` false;
  `uniq-by-line` either value; no `new-from-*`); `run.issues-exit-code` outside 1..125, Go's
  portable exit range (`os.Exit` truncates the code to 8 bits, so 0, 256 or -256 lets lint exit 0
  while it prints issues); `run.tests` other than true; `linters.default` other than `standard` or
  `all`; depguard or forbidigo missing from `linters.enable` or listed in `linters.disable`; and a
  `linters.exclusions.rules` entry that names depguard, names forbidigo without a `text` or with an
  empty one, or names no linter at all. The document's shape is an allow list that fails closed:
  `version` must be `"2"`, and a top-level, `run`, `linters`, `linters.exclusions` or `issues` key, or
  a `linters.exclusions.presets` entry, the check does not know is refused, since a later
  golangci-lint may act on it. forbidigo is held the same way as depguard because the
  `cloud-read-only` and `domain-is-pure` groups depend on it; its two text-scoped `domain-is-pure`
  exclusion rules pass, and `test/lint` (`TestForbidigoExclusionsDropOneGroup`) pins their texts.
  `TestRepositoryReadsOnlyGolangciYML` refuses a `.golangci.json`, `.golangci.toml` or
  `.golangci.yaml` at the root that git could commit: golangci-lint finds those before
  `.golangci.yml` and would run with them instead. The command lines cannot point lint elsewhere
  either: `test/architecture` pins `make lint`'s arguments to `run --allow-serial-runners` and the
  CI golangci-lint step's `with:` block to `version` and `skip-cache`. `test/lint` and this check
  each read `linters.exclusions.rules`; the two readers could later share one (issue #82).

A cloud SDK is imported only in its provider's adapter package (`source/aws`, `source/azure`,
`source/gcp`, `target/ovhcloud`, `target/scaleway`, `target/upcloud`): the depguard rule
`cloud-sdks-only-in-provider-adapters` refuses it everywhere else, a sibling helper package and
`cmd/shift2eu` included.

## Cloud read-only rule

A source adapter only reads from a cloud (CLAUDE.md rule 1) and never handles a credential itself
(rule 3). An import rule alone cannot hold that, because a cloud SDK puts reads and writes on the same
client, and the operator's credential chain may hold more rights than the read-only role. Three lint
guardrails hold it instead; [ADR 0014](adr/0014-lint-guardrails-for-read-only-cloud-access.md) records
why.

1. **forbidigo, group `cloud-read-only`, an allowlist of reads.** In every file lint checks, tests and
   integration tests included, an SDK client allows only read verbs: AWS `Describe*`, `Get*` and
   `List*`; Azure `Get*`, `List*`, `NewGet*` and `NewList*` (the pagers); GCP `Get*`, `List*`,
   `AggregatedList*` and `Close`. Only a read operation's input, output, options, request or response
   type may be named. Further entries refuse whole packages or single names: clients no allowlist
   covers (`google.golang.org/api`, the hand-written GCP clients, the AWS SDK v1, Azure track 1), the
   hooks through which a read is rewritten into a write or a credential is handed out (signers,
   credentials providers, middleware, `aws.Config.Credentials`, `config.With*`, Azure transports and
   policies, `option.With*` transports and credentials, gRPC connections), allowed verbs that return
   a secret, and, on a source adapter's own types, a method named for a write.
2. **depguard, rule `cloud-adapters-reach-the-cloud-through-the-sdk`, a strict import allow list.**
   Every package under `internal/adapter/source` but `inventoryfile` and `sourcetest`, tests
   included, imports only the standard packages an adapter needs, the cloud SDK roots and the project
   packages it uses. So nothing that calls a method by a name forbidigo cannot see (`reflect`,
   `text/template`, `html/template`), runs a program (`os`, `os/exec`, `syscall`, `golang.org/x/sys`,
   cgo) or opens a connection of its own (`net`, `database/sql`) gets in.
3. **`test/lint`, on the AST.** An interface that a cloud source adapter declares, named, literal or a
   type-parameter constraint, may declare only read verbs (`Describe`, `Get`, `List`, `NewGet`,
   `NewList`, `AggregatedList`, `Close`, `HasMorePages`, `NextPage`, `More`) or unexported methods:
   an SDK client would satisfy any other with its operation types left to inference, and a call
   through a type parameter has no type name forbidigo can match. Elsewhere in the module an interface
   may not declare a method named for a write (a denylist of verbs; a source adapter cannot import
   such a package anyway). No struct embeds an SDK type, no alias names one, no file dot-imports an
   SDK.

As with depguard, every file is linted, also one that looks generated, and `test/lint` fails on any
nolint directive that would hide a depguard or forbidigo issue.

### How the rule is pinned

forbidigo's type-aware patterns fail silently when they are wrong. `TestCloudReadOnlyProbe` runs the
golangci-lint version CI pins, forbidigo and depguard, on probe adapters over stub SDKs
(`test/lint/testdata/cloudreadonly`), offline, with lines left unmerged (`--uniq-by-line=false`). A
line marked `// want "<message prefix>"` must be refused with that message, no other line may be,
and every entry of the group must refuse a line of its own, so an entry that stops matching fails.
The stubs have the real SDKs' import paths, package names and shapes; azcore's `ClientOptions` is laid
out as in the real SDK. `TestCloudReadOnlyPatterns` checks the client and operation patterns against
long lists of write and read names, so a typo in a first-letter class fails without golangci-lint. It
covers the denylist entries too (the secret, credential, hook and gRPC entries and the refused
packages): `cloudDenied` gives names each entry must refuse, and the test drops each alternative and
optional part (`x?`, `x*`, `x{0,n}`) of an entry's pattern and pkg in turn and fails unless one of
those names then gets through. So narrowing an entry, say dropping `Secrets?` from the Azure secret
entry or the `\w*` in `List\w*AuthTokens`, fails at once. Where no real name needs a part, a stand-in
of its shape does, marked `// stand-in`. A character class is not mutated, and `regexp/syntax` turns
an alternation of single letters (`URI|URL`) into one (`UR[IL]`).
`cloudDenylistReads` holds reads the entries were narrowed to allow (`armkeyvault.KeysClient`).
`TestInterfaceFindingsProbe` runs the AST rule on the same probe. `make lint` and every entry of CI's
lint-target matrix, which reports as the Lint check, run them; CI fails on a golangci-lint version
that differs from the pinned one, and elsewhere the probe runs anyway and logs a warning that points
at the dev VM ([dev-environment.md](dev-environment.md)).

### How forbidigo renders a name

Probed on golangci-lint v2.15.0 (forbidigo v2.3.1). Probe again when either moves.

- A method is `<package name>.<type>.<method>`, with a pointer receiver dropped:
  `ec2.Client.RunInstances` for `(*ec2.Client).RunInstances`. A package-level name is
  `<package name>.<name>`: `ec2.RunInstancesInput`. It is the package name, not its path or import
  alias: `armcompute` for `.../armcompute/v5`, and `compute` for both GCP compute SDKs.
- A field read or set through a selector is `<package>.<type>.<field>`, as a method is:
  `policy.ClientOptions.Transport`. A key in a composite literal is `<package>.<field>`, without the
  type: `policy.Transport` in `policy.ClientOptions{Transport: t}`.
- The type is the one the selector is used on, not the one that declares the method or field: a
  method promoted from an embedded client is matched against the embedding type, and a field of an
  embedded struct against the outer one. A value of an alias type is matched against the type the
  alias names (a `*policy.Request` is an `exported.Request`), a generic type by its name alone.
- `pkg` is matched against the import path of the package that declares that type, or of the package a
  qualified name names, which is what tells the SDKs apart.
- A call through an interface is matched against the interface, not the type behind it. A selector on
  a type parameter, an interface literal or an unnamed struct has no type name, so no entry can match
  it; `test/lint` refuses the declarations that lead there.
- `pattern` and `pkg` are unanchored, so every entry spells out `^` and `$`.
- golangci-lint turns forbidigo's `//permit:` comments off. `exclude-godoc-examples` is false, so a
  godoc example is checked like other code.
- depguard sees the imports of the code cgo generates, not `C`: cgo is refused through `unsafe`.

### Allowing a new read verb

Go regexps have no lookahead, so "a name that does not start with an allowed verb" is written as a
prefix trie, one branch for each way a name can leave the verbs. For AWS:

```
[A-CEFH-KM-Z]      a first letter no verb starts with (not D, G, L)
D[^e]|De[^s]|...   the first letters of a verb, then another letter
Describe[^A-Z0-9]  the whole verb run on in lower case, so Describes, Getaway and Listen are refused
```

Every other name is allowed: `Get`, `GetObject`, `DescribeInstances`. A name that stops part-way
through a verb (`Ge`, `Lis`, `Aggregate`) is not refused; no SDK client has one. Azure's `New` branch
repeats the `Get` and `List` branches after `New` (`NewDeletePager` is refused); GCP's `Close` is a
whole name, so `Close\w` refuses anything longer.

To allow a read verb, say `Search` on AWS, drop `S` from the first-letter class and add
`S[^e]|Se[^a]|Sea[^r]|Sear[^c]|Searc[^h]|Search[^A-Z0-9]`. Where two verbs share a prefix, the class
at the fork leaves out both next letters (`L[^io]` for `List` and `Lookup`). Change the SDK's method
and operation entries alike, add only a verb that cannot write, add the verb to the reads in
`TestCloudReadOnlyPatterns` and to `readName` in `test/lint/sourcewrites_test.go`, add a read call of
it to the probe, and run `make lint`. Before using an SDK type no entry covers, such as a new
hand-written GCP package, add an entry for it, a probe line only it refuses and names for it in
`cloudDenied`.

### Gaps that remain

- **Packages only a test imports are not placed.** `test/architecture` (ADR 0012) holds
  `cloud-adapters-reach-the-cloud-through-the-sdk` package by package on every release target,
  as depguard does in `make lint` and CI (issue #79): its SDK-root prefix entries, which must
  end in `/`, matched as depguard matches them (only the entry sorted nearest before an import
  decides), the two directories it excludes and its test files, integration tests included. The
  SDK packages an allowed import reaches, and their own dependencies, are exempt, as depguard
  lints no file of a dependency, unless a package no strict rule holds imports one directly. The
  exemption trusts the import path, which is sound only because the root `go.mod` holds no
  `replace` directive, read with the go command's own parser (`go mod edit -json`), and the
  repository holds no `go.work`, `go.work.sum` or `vendor/` directory, any of which could otherwise
  substitute code for an allowed import path (`test/architecture/gomod_test.go`). It reads the
  strict allow lists only: the lax deny rules (`go-cmp-only-in-tests`,
  `cloud-sdks-only-in-provider-adapters`, `composition-root-tests`) are held by depguard in `make lint` alone. A third-party
  import in `internal/cli` is refused by depguard alone, whose strict rule there allows only the
  standard packages it names and named module packages (issue #152); one in `cmd/shift2eu` is refused by the strict rule `composition-root`
  (issue #101), which `test/architecture` holds the composition root to on every release target,
  cgo mode and tag set, apart from the shared layer listing, and whose listing must hold no package
  under the module path from a module other than the main one, since the rule's prefix entry would
  allow a nested module.
  Nor may a composition root reach a package of the module whose path below the module path holds
  `testdata` or an element starting with `_` or `.` (issue #206): `./...` never walks such a
  directory, so neither depguard nor `test/lint` sees it, yet the go command builds it when a package
  imports it by explicit path. `test/architecture`
  (`TestCompositionRootRulesAreHeldToTheirRuleForEveryReleaseTarget`, on the same listing as its
  main-module check) refuses one in `go list -deps` of `cmd/shift2eu` and `cmd/shift2eu-server`, imported
  directly or transitively, on every release target, cgo mode and tag set, with the finding
  `<root> reaches <pkg>, which ./... never walks but go builds when a package imports it by explicit
  path: no lint or architecture rule sees it`. A probe module (`testdata/hiddenpathprobe`, whose
  `cmd/shift2eu` imports `testdata/x`, `_x` and `.x`) proves it fires. A symlinked directory is another
  spelling `./...` skips; this check does not close it, the existing ones do:
  `TestRepositoryHasNoLineDirectives` refuses every symlink git tracks or would add, and
  `TestReleaseTargetDependencies` (`check.Sources`) requires every module package a release binary is
  built from to sit in the directory its import path names, symlinks resolved. A change to either
  would reopen issue #206.
  `go list -deps` lists the imports of a held package's tests but not those imported packages
  themselves, so a package only a test imports is checked against the test's allow list, not for
  its own module, directory or non-Go sources.
- **Files lint never loads**: a file behind a build tag other than `integration` and `e2e`, a
  `//go:build ignore` helper, and the `testdata`, `_` and `.` directories `./...` skips. `test/lint`
  fails on such a file, and on an imported package in such a directory. A main package there, run
  with `go run`, is not checked. `make lint` and CI lint the host and every release target, and
  `test/lint` reads the same targets from `.goreleaser.yaml`, so a file one release target builds
  passes it.
- **A write or a credential that uses no SDK type**: a request signed, or given a bearer token, by hand
  with only the standard library. The strict import list leaves no `net` package to send it with,
  but an SDK's own HTTP client could carry it. Lint is a guardrail; the read-only role is the
  backstop.
- **Endpoint overrides are a reviewer check, an accepted gap.** `aws.Config.BaseEndpoint`,
  `option.WithEndpoint` and an Azure `cloud.Configuration` send a signed request or a bearer token to
  another host. A sovereign cloud region needs one, so lint does not refuse them. A reviewer checks
  that every endpoint an adapter sets comes from configuration naming a known cloud endpoint, never
  from a constant or input that could point elsewhere.
- **An interface from another module** that declares a write and that an adapter could name: the SDK
  interfaces with a write method are refused by name (`manager.UploadAPIClient`), so check a new SDK,
  a new package of one, or a new dependency for such interfaces.
- **Allowed verbs that do more than read.** The secret entries refuse the known ones that mint
  credentials, register an identity, return a secret or a presigned URL; others remain, such as AWS
  Cost Explorer `Get*` calls, billed per request, and the secrets in the fields of an ordinary read
  (EC2 user data, Lambda and ECS environment variables), which the adapter drops. A secret read
  through a generic interface with its types inferred (a `Get*` method) is not refused by name. The
  secret entries also refuse a few reads that return none, such as Azure
  `GetAuthSettingsV2WithoutSecrets` and the key-vault `SecretsClient` listings.
- **Write verbs outside the source adapters** are a denylist (`writeVerbs` in `test/lint` and the
  last forbidigo entry), so an interface elsewhere named with a verb not on it gets through. A source
  adapter can import only the project packages its depguard rule lists, which keeps that interface
  out of its reach unless one of those packages declares it.

## Composition-root process and environment rule

The strict allow lists `composition-root` and `server-composition-root` must allow `os` and
`syscall`, and both can start a process (shelling out to a cloud CLI) or read the environment, where
credentials live (CLAUDE.md rule 3). An import rule cannot see a call, so two forbidigo groups hold
`cmd/shift2eu` and `cmd/shift2eu-server` (issue #111, the security audit's LOW-2 on issue #101):

- **`composition-root-no-exec`** refuses `os.StartProcess`, `syscall.Exec`, `syscall.ForkExec`,
  `syscall.StartProcess`, every use of `os/exec` (pattern `^\w+\.\w+` with `pkg: '^os/exec$'`), and
  dumping the whole environment, `os.Environ` and `syscall.Environ`. On windows it refuses the same
  through `syscall.CreateProcess`, `syscall.CreateProcessAsUser` and `syscall.GetEnvironmentStrings`,
  and loading a library whose code would run: `syscall.LoadDLL`, `MustLoadDLL`, `NewLazyDLL`,
  `LoadLibrary` and `GetProcAddress`. It refuses the raw system calls a process can
  be started with just as well (`syscall.Syscall`, `Syscall6`, `Syscall9`, `Syscall12`, `Syscall15`,
  `Syscall18`, `SyscallN`, `RawSyscall`, `RawSyscall6`) and the `syscall.SYS_*` numbers they take,
  and every use of package `unsafe`, which hands a raw system call an address. Every pattern is anchored with `^...$`, so a longer name (`syscall.CreateProcessX`,
  `syscall.Syscalls`, `syscall.SYSCTL`) passes. It has no exception. These named entries are
  defence in depth, not the guard: a deny list of names leaked (`syscall.AllThreadsSyscall`, a
  `syscall.LazyDLL` literal with `NewProc().Call`, `NewCallback`, `Ptrace*`), so the syscall allow
  list below is what holds. The `unsafe` ban is defence in depth too: `fmt` and `strconv` can print
  and parse an address without it.
- **`composition-root-env`** refuses reading a named variable: `os.Getenv`, `os.LookupEnv`,
  `os.ExpandEnv`, `syscall.Getenv` and, on windows, `syscall.GetEnvironmentVariable`. Since issue
  #152 it also refuses `os.UserHomeDir`, `os.UserConfigDir`, `os.UserCacheDir` and `os.TempDir`, in
  an entry of their own: each reads `$HOME`, `$XDG_CONFIG_HOME`, `$XDG_CACHE_HOME` or `$TMPDIR`
  without naming it, the directories a credentials file sits under, so the roots are held to the
  same nine `os` reads as every other package allowed `os` (below). Its one
  exception is a single `os.LookupEnv` call in `cmd/shift2eu-server/main.go`, with the argument
  `tokenEnvVar` (`SHIFT2EU_SERVER_TOKEN`), in the token source
  `func() (string, bool) { return os.LookupEnv(tokenEnvVar) }` that `main` hands to `run` (ADR 0019).
  `run`, `setUp` and `readToken` see only the token and whether it is set, never a lookup by name, so
  no code in `cmd/shift2eu-server` can read another variable through them.
  `os.LookupEnv` has an entry of its own, and the exception's exclusion rule has
  `path: ^cmd/shift2eu-server/main\.go$` and a text that is the group's form instantiated with that
  entry's msg, regex-escaped, so it drops no other entry's issue in that file and nothing elsewhere.

Each group has one exclusion rule, `path-except: ^cmd/shift2eu(?:-server)?/` (two per-root rules would
each drop the other root's issues). Both hold for `_test.go` files under the roots too: a test there
runs in the same package and environment, and the server's tests hand `run` a fake token source. The
patterns are names, so they hold on every release target whether a name exists there or not
(`syscall.Exec` has no windows form, `syscall.CreateProcess` no unix one).
`TestCompositionRootForbidigoProbe` runs forbidigo on a probe module
(`test/lint/testdata/compositionroot`) with every refused call in both roots and in `internal/cli`
(where the CLI's own group refuses them, below),
natively and with `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`, where the windows names type-check.
`TestCompositionRootForbidigoPatterns` pins the patterns against lists of refused and allowed names,
and `TestCompositionRootExclusions` pins the scope and the one exception.

Four source checks in `test/lint` read the real roots, tests included, as the groups cannot state
them; the `//go:linkname` and `/proc` checks read both driving adapters, `internal/cli` and
`internal/httpapi`, and the inventory-file reader, `internal/adapter/source/inventoryfile`, as well
(issue #152):

- `TestCompositionRootsUseSyscallOnlyForSignals` is the comprehensive guard on `syscall`: an AST
  check under which a root uses package `syscall` only for a `^SIG[A-Z0-9]+$` constant and the type
  `syscall.Signal`, read through the import name the file gives it; a dot or blank import of
  `syscall` is refused. Any other name, whether forbidigo lists it or not, fails.
- `TestRootsAndCLILinkNoForeignSymbol` refuses a `//go:linkname` directive in the roots and the
  driving adapters, which
  would reach an unexported runtime or `syscall` symbol past every name rule.
- `TestCompositionRootPassesLookupEnvToRun` holds `cmd/shift2eu-server/main.go` to exactly one
  reference to `os.LookupEnv`: a call with the one argument `tokenEnvVar`, the sole return expression
  of a `func() (string, bool)` literal passed directly to the package-level `run`, which `main.go`
  neither declares nor assigns (the forbidigo exception is a path and a msg, so a second use in that
  file, or handing `os.LookupEnv` itself to `run`, would lint clean).
- `TestRootsAndCLINameNoEnvironmentPath` refuses a string literal holding the path segment `proc`
  or `dev/fd`, at its start or after a `/` and followed by a `/` or the end of the literal, in any Go
  file of the roots, the driving adapters and the inventory-file reader, as
  `os.ReadFile("/proc/self/environ")` dumps every variable. The leading slash is optional since the
  audit of #152: `fs.ReadFile(os.DirFS("/"), "proc/self/environ")` reads the same file, and a
  relative `proc/...` resolves to it from `/`. The trailing slash is optional since the next audit:
  `os.DirFS("/proc")`, `os.OpenRoot("/proc")` and `os.Open("/dev/fd")` name the directory itself, so
  `/proc`, `proc` and `/dev/fd` are refused too. A name that only ends or starts in `proc`
  (`myproc/...`, `/procfs/...`) passes.

The roots' `_test.go` files are compiled into the same package and run in the same process, but the
strict depguard rules `composition-root` and `server-composition-root` skip them. The lax depguard
rule `composition-root-tests` covers them: it denies `go/build`, `net/http/cgi`, `os/exec`, `plugin`,
`os/user` (a lookup returns a home directory), `runtime/cgo`, `syscall/js` and `unsafe`. Since issue
#152 it holds the `_test.go` files of the two driving adapters, `internal/cli` and `internal/httpapi`,
as well: a root hands them the work, and their tests run in the same process as the code under test.
This import rule is only half of the roots' limits; the call half is in "The driving adapters" below.
`TestCompositionRootTestsReachNoForeignCodeProbe` runs it on a probe module
(`test/lint/testdata/compositionroottests`) for both roots and both driving adapters, and
`test/architecture` pins it with the other depguard rules.

### The driving adapters

A root could pass a call it may not make itself to the driving adapter it hands the work to, where
it would happen one call deeper with lint green. Issue #152 closes that for the class, not for named
members. An earlier version of this section said the adapters' tests already had the roots' limits
through `composition-root-tests`; they had the import half only, and the review of #152 showed a test
of either adapter could still call `os.Getenv` or `syscall.Exec` with lint green. The call half is
now held as well:

- **`cli-gets-adapters-from-cmd`** is a strict allow list. It used to allow `$gostd` and deny a list
  of packages beside it, so any standard package nobody thought to deny, `unsafe` among them, was
  allowed. It now names the module's packages the CLI may import and exactly the standard packages it
  imports today (`bytes`, `context`, `errors`, `flag`, `fmt`, `io`, `io/fs`, `maps`, `os`,
  `path/filepath`, `slices`, `strconv`, `strings`, `unicode`, `unicode/utf8`), each ending in `$`.
  `syscall`, `unsafe`, `os/exec`, `os/signal`, `plugin`, the network and everything else are refused
  in its production files because nothing names them. `os` is named because the CLI reads the
  inventory a user names (`--inventory`) and creates the report the user asked for (`--output`).
  `TestCLIImportsProbe` lints the rule on a probe module, and `test/architecture`'s
  `TestCLIImportRuleAllowsNoProcessOrPointer` holds the property beside the exact pin. The rule is no
  longer skipped by `test/architecture`'s per-release-target `go list` check
  (`TestReleaseTargetDependencies`), so every package the CLI reaches on every release target is
  held by a strict rule.
- **`version-is-build-metadata`** (depguard, strict) holds `internal/version`, which the CLI imports,
  to `fmt`, the one package it imports today. Without it the build metadata was a delegation path of
  its own: a root hands the CLI the work, the CLI calls `internal/version`, and `internal/version`
  could import `os` or `os/exec` with lint green. `TestVersionImportsProbe` lints it on a probe
  module, and `test/architecture` pins it (`TestVersionRuleIsBuildMetadata`).
- **`cli-no-env-or-process`** (forbidigo, `analyze-types`) refuses what `os` reaches beyond files:
  `os.Getenv`, `os.LookupEnv`, `os.ExpandEnv`, `os.Environ`, `os.StartProcess`, `os.UserHomeDir`,
  `os.UserConfigDir`, `os.UserCacheDir` and `os.TempDir`, the roots' `os` names and
  `testutil-no-env`'s. The CLI's tests import `syscall` for `syscall.Mkfifo` (`output_unix_test.go`),
  so a second entry refuses the roots' `syscall` names: `Getenv`, `GetEnvironmentVariable`,
  `Environ`, `Exec`, `ForkExec`, `StartProcess`, the windows `CreateProcess`, `CreateProcessAsUser`,
  `GetEnvironmentStrings` and DLL loaders, the raw `(Raw)Syscall*` calls and the `SYS_*` numbers.
  It is one entry with one msg rather than the roots' three: the windows names exist on windows
  only, the probe's windows run lints `cmd/` alone, so `TestCLINoEnvOrProcessForbidigoPatterns` pins
  every name offline instead. The writes (`os.Setenv`, `os.Unsetenv`, `os.Clearenv`, `os.Chdir`)
  left this group in issue #153: `tests-no-process-state` owns them everywhere, so no line is
  reported by two groups. The group has no exception: the CLI reads no named variable. Its one
  exclusion rule, `path-except: ^internal/cli/`, makes it hold for every file there, `_test.go` files
  included. `TestCompositionRootForbidigoProbe` runs it on the probe's `internal/cli`, an aliased
  `os` import included.
- **`httpapi-tests-no-env-or-process`** (forbidigo, `analyze-types`) holds the `_test.go` files of
  `internal/httpapi` to the same `os` and `syscall` names, for the same reasons. Its production files
  need no group: `httpapi-gets-adapters-from-cmd` is a strict allow list that leaves `os` and
  `syscall` out. Its exclusion rule is `path-except: ^internal/httpapi/.*_test\.go$`.
  `TestCompositionRootForbidigoProbe` runs it on the probe's `internal/httpapi`, and
  `TestHTTPAPITestsNoEnvOrProcessForbidigoPatterns` pins the patterns.

`os.Getwd` stays allowed in the CLI: it reads `$PWD` (when it names the working directory) and
returns a path only, the one the user ran the CLI in.

### Every package a root reaches is held

Both roots allow every module package through the prefix `github.com/timothymamo/shift2eu/`, so a
root can hand a call it may not make to any package of the module, not only to the driving adapters.
The audit of #152 found three such packages that no strict rule held (`internal/config`, the contract
suites) and one allowed `os` with no call rule (`inventoryfile`). Issue #152 closes the class with three
properties that `test/architecture` checks over every package `go list` reports for every release
target, with the `e2e` and `integration` tags (`strictcoverage_test.go`):

- **Every non-test module package a root can import is held by a strict depguard rule**
  (`TestEveryModulePackageIsHeldByAStrictRule`), over `go list ./...`: `internal/`, `cmd/`, the
  doc-only packages under `test/`, and a new top-level package such as `pkg/` the day it is added. A
  new package without one fails the test, so whatever a root reaches has a strict allow list. The
  only packages left out are those Go's internal rule hides from `cmd/` and `internal/`, whatever
  the roots' allow lists say: a path with an `internal` element whose parent is not the module root
  (`test/internal/...`, `test/architecture/internal/...`). The round that widened the property from
  `internal/` and `cmd/` (the next audit of #152 found that `test/devenv/leak.go` or a new `pkg/leak`
  stayed lint-green) added `test-docs-are-pure` for the doc-only `test/*` packages.
- **Every package whose strict rule allows `os` or `syscall` is held by a forbidigo group refusing
  their reads** (`TestEveryOsAllowingRuleHasAnEnvironmentGroup`): for `os` the nine `os.Getenv`,
  `os.LookupEnv`, `os.ExpandEnv`, `os.Environ`, `os.StartProcess`, `os.UserHomeDir`,
  `os.UserConfigDir`, `os.UserCacheDir` and `os.TempDir`; for `syscall` (issue #167)
  `syscall.Getenv`, `GetEnvironmentVariable`, `GetEnvironmentStrings`, `Environ`, `Exec`,
  `ForkExec`, `StartProcess`, `CreateProcess`, `CreateProcessAsUser`, the library loads (`LoadDLL`,
  `MustLoadDLL`, `NewLazyDLL`, `LoadLibrary`, `GetProcAddress`) and the raw system calls (`Syscall`,
  `Syscall6`, `9`, `12`, `15`, `18` and `N`, `RawSyscall`, `RawSyscall6`, `AllThreadsSyscall`,
  `AllThreadsSyscall6`); each in an entry whose exclusion rules leave the package in scope. The names
  are matched as strings, so one that exists on a single release target costs nothing on the others,
  and `make lint` lints every target. `$gostd` counts as allowing both. Today those are the roots
  (`composition-root-env`, `composition-root-no-exec`; theirs are the only rules that name
  `syscall$`), `internal/cli` (`cli-no-env-or-process`), `internal/testutil` (`testutil-no-env`),
  the inventory-file reader (`inventoryfile-no-env`) and `test/e2e` (`e2e-no-env`, whose
  `e2e-is-black-box` allows `syscall` through `$gostd`). A rule that gains `os$`, `syscall$` or `$gostd` fails the
  test until its package has a group.
- **No package a root can import is held only through a `$gostd` rule**
  (`TestNoPackageIsHeldOnlyByAGostdRule`): a strict rule that allows `$gostd` allows `os/exec`,
  `syscall` and `unsafe` with the rest, so every non-test module package a strict rule holds is held
  by at least one strict rule that does not allow `$gostd`, unless it is named, with its reason, in
  `gostdOnlyExempt`, which is empty. The final review and audit of #152 found `test/e2e` held by
  `e2e-is-black-box` alone: its `doc.go` carries no build tag, so it is a production package a root
  can import, and an untagged file there could read `os.Getenv` and start `exec.Command` with lint
  green. `test-docs-are-pure` holds it now.

`TestStrictRuleCoverageHolds` pins the helpers the tests share. The rules the properties called for:

- **`config-is-pure`** (depguard, strict) holds `internal/config`, which has only a `doc.go` today,
  to an empty allow list, `allow: []`: it imports nothing, so the rule allows nothing.
  `check.AllowList` reads an explicit `allow: []` as no entries and still refuses a missing or
  null `allow:`, so a rule parsed as empty by mistake does not pass for one (issue #166).
  depguard refuses a rule with no list at all, so the rule carries one deny entry, `os`; the strict
  empty allow list is what refuses every import. Its `doc.go` announces flags, the environment and a
  file; a future `os` import comes with a forbidigo group for its reads (the second property fails
  until it does), and any read of a named variable needs an exception decided in an ADR, as
  `composition-root-env`'s for `SHIFT2EU_SERVER_TOKEN` (ADR 0019). The same commit drops the `os`
  deny entry, as `check.AllowList` refuses a rule whose allow list allows a package its deny list
  names.
- **`target-docs-are-pure`** (depguard, strict) holds `internal/adapter/target`, a `doc.go` with no
  code, to `allow: []` and the `os` deny entry, as `config-is-pure`; negated globs take out the
  providers, the snapshot reader and `targettest`, which have rules of their own. A provider added
  under it is named there and in `embedded-snapshot-targets-stay-offline` in the same commit.
- **`contract-suites-are-pure`** (depguard, strict) holds `internal/adapter/source/sourcetest` and
  `internal/adapter/target/targettest`. They are production packages that any `_test.go` file may
  import, so a scoped test could delegate a call through them. The allow list is what they import
  today (`context`, `errors`, `fmt`, `slices`, `testing`, `time`, `internal/app/analysis` and the
  domain's `catalog`, `inventory`, `taxonomy` and `text`); `!$test` leaves their own tests, which run
  in the suites' own test binaries, to the module-wide rules. `TestContractSuitesArePureProbe` lints
  it on a probe module (`test/lint/testdata/contractsuites`).
- **`inventoryfile-no-env`** (forbidigo, `analyze-types`) refuses the nine `os` reads under
  `internal/adapter/source/inventoryfile/`, `_test.go` files included (one `path-except` exclusion
  rule). `inventory-file-reader-reads-one-file` must allow `os` for `os.Open`, and `cmd/shift2eu`
  imports the reader. The writes are `tests-no-process-state`'s. `TestInventoryFileNoEnvProbe` and
  `TestInventoryFileNoEnvForbidigoPatterns` pin it.
- **`e2e-no-env`** (forbidigo, `analyze-types`, issue #167) refuses the nine `os` reads and the
  `syscall` reads above under `test/e2e/`, `_test.go` files included (one `path-except` exclusion
  rule). `e2e-is-black-box` allows `$gostd`, which the first property reads as allowing `os` and
  `syscall`; `test-docs-are-pure` holds the untagged `doc.go` to `errors$` as well, so for it the
  group is defence in depth. It refuses `os.TempDir`, the direct temp-directory lookup, not every
  use of `$TMPDIR`: `os.MkdirTemp("", ...)` still reads it. It refuses `(*exec.Cmd).Environ` too,
  which hands a child the whole environment of the test process (issue #185). A method promoted
  through an embedded `*exec.Cmd` (`type w struct{ *exec.Cmd }; w{cmd}.Environ()`) is rendered
  under the embedding type and is not refused; the entry is defence in depth for `$test`, since the
  non-test files cannot import `os/exec`. The e2e `_test.go` files keep `$gostd` to build and run
  the binary with `os/exec`. The runs of the binary (`run` and the server helpers) set `cmd.Env`;
  the `go build` calls in `TestMain` and the `go list -deps` calls leave `Env` nil and inherit the
  environment, which the go toolchain needs. A nil `Env` in a `_test.go` file is not refused: issue
  #185 leaves `$test` as it is, and issue #195 follows it up. In the non-test files
  `test-docs-are-pure` refuses `os/exec`, `unsafe` and `os/user` at the import
  (`TestE2EImportsProbe` and `test/architecture`'s depguard pin hold it), so an `exec.Cmd` with a
  nil `Env`, which inherits the whole environment, cannot be written there through an import. An
  assembly file needs no import, so `test/lint` refuses one there (issue #194). `TestE2ENoEnvProbe` and
  `TestE2ENoEnvForbidigoPatterns` pin the group.
- **`test-docs-are-pure`** (depguard, strict) holds the doc-only packages `test/apiparity`,
  `test/architecture`, `test/container`, `test/devenv`, `test/docs`, `test/e2e` and `test/lint` to
  `allow: []` and the `os` deny entry, as `config-is-pure`. Each is a production package a root
  could import through the module prefix. `!$test` leaves their tests out, and a negated glob takes
  out `test/architecture/internal/...`, which Go's internal rule hides from the roots and which
  reads `os` and `os/exec`. `test/e2e`'s untagged `doc.go` is held here as well (the third property):
  `!$test` takes the e2e test files out (depguard applies negated globs whatever their position), so
  they stay on `e2e-is-black-box` alone, which allows `$gostd` and holds the package too. A package
  that starts importing something names it there and in the pin in the same commit. An import
  list says nothing of a file that needs no import, so `TestDocPackagesHoldNoNonGoSource`
  (`test/lint`, issue #194) refuses a non-Go source the Go toolchain builds beside a package
  (assembly, C, C++, Objective-C, Fortran, SWIG or a `.syso` object) in every tree the rule's
  `files:` list holds, outside `testdata`, `_` and `.` directories and other modules (a directory
  holding a `go.mod` regular file, as for the go command); it reads those trees from `.golangci.yml`.
  The go command leaves `testdata`, `_` and `.` directories out of `./...`, and that exemption
  covers non-Go files only: a non-Go file there is no source of the held package. The check refuses
  every symlink in, or above, a held tree, whatever its name and wherever it sits, inside
  `testdata`, `_` and `.` directories included: the walk reads the link, not what it points at, yet
  an import of its path follows it and the toolchain builds the source it finds there. A package
  under `testdata`, `_x` or `.x` is still built when it is imported by path, which this check does
  not cover: a composition root that reaches one is refused by `test/architecture` instead (issue
  #206, the follow-up to #194).

### Tests change no process state

`t.Setenv` and `os.Setenv` change the environment of the whole test binary, so every test in the
process sees the variable, a credential's name among them, and `t.Chdir` and `os.Chdir` change the
directory a `t.Parallel` test shares. The forbidigo group **`tests-no-process-state`** (issue #153,
`analyze-types`) refuses them with two entries:

- `pattern: '^(?:[\w*()\[\]]+\.)+(?:Setenv|Chdir)$'` with no `pkg`, which matches any function or
  method named `Setenv` or `Chdir`, whatever its receiver. forbidigo renders a method on the type it
  is found on: `testing.T.Setenv`, `testing.TB.Setenv`, a method value, `os.Setenv`, `os.Chdir`, but
  also `pkg.wrapper.Setenv` for a struct that embeds `*testing.T` and `pkg.iface.Setenv` for a named
  interface `t` is assigned to (probed on the golangci-lint version CI pins). The first version of the
  group named `testing.(T|B|F|TB)` only and missed those two.
- `pattern: '^(?:os|syscall)\.(?:Unsetenv|Clearenv|Fchdir)$'` with `pkg: '^(?:os|syscall)$'`, which
  refuses `os.Unsetenv`, `os.Clearenv`, `syscall.Unsetenv`, `syscall.Clearenv` and `syscall.Fchdir`
  (the review of #153: `syscall` writes the same environment and working directory `os` does; `os`
  has no `Fchdir`, and the pattern refuses one all the same).

forbidigo renders nothing for a call through an interface literal
(`var s interface{ Setenv(k, v string) } = t`) or through a type parameter constrained by one, so
`test/lint`'s `TestTestsChangeNoProcessStateByNoSelector` refuses every selector named `Setenv`,
`Chdir`, `Unsetenv`, `Clearenv` or `Fchdir` on the AST in the group's scope, and
`TestProcessStateSelectorFindings` pins that check.

The group owns the writes: `testutil-no-env` and `cli-no-env-or-process` leave `os.Setenv`,
`os.Unsetenv`, `os.Clearenv` and the `syscall` writes out, so no line is reported by two groups.
Its first exclusion rule, `path: ^test/(?:lint|architecture)/`, drops it in those two harnesses, which call `t.Setenv` on
purpose, in test binaries of their own, to run the go command in a known environment (`GOCACHE`,
`GOFLAGS`, `HOME`). Its second, `path: ^ci/aireview/internal/review/claudecli_test\.go$`, drops it in
that one file of the AI reviewer's library (PRD #161 milestone 4, ADR 0033): one test there sets
`ANTHROPIC_API_KEY` and a variable of its own, with no parallel test beside it, to prove that the
Claude Code CLI the reviewer runs inherits nothing of the process's environment. Every other file of
the library, its other tests included, is held to the group. Everywhere else it holds: the application layer, the test helpers, the driving
adapters, the composition roots, and since the review of #153 the adapters', the domain's and the e2e
tests too, so a future source adapter's test cannot set `AWS_PROFILE`. `TestTestsNoProcessStateProbe`
runs the group on a probe module (`test/lint/testdata/testsprocessstate`) with a line on each
receiver and dodge in each area, and `TestTestsNoProcessStateForbidigoPatterns` pins the patterns.

Known gaps, which these rules do not close:

- **A `/proc` path built at run time.** The source check sees a literal only; a path built at run
  time (`"/pr" + "oc"`, `filepath.Join`) gets through.
- **Implicit reads.** `net/http`'s `DefaultTransport` reads `HTTP_PROXY`, `HTTPS_PROXY` and
  `NO_PROXY` (and their lower-case forms) itself. It reads named variables and exposes none, so this
  is accepted. Likewise `os.MkdirTemp("", ...)`, `os.CreateTemp("", ...)` and `t.TempDir()` read
  `$TMPDIR` through `os.TempDir` (issue #153): they return only a fresh temporary path, never a home,
  config or credential path, and `t.TempDir` goes through the same read, so refusing them would leave
  a test no way to get a temporary directory. They are accepted; `os.TempDir` named directly stays
  refused where `testutil-no-env`, `cli-no-env-or-process`, `httpapi-tests-no-env-or-process`,
  `inventoryfile-no-env` and, since issue #152, `composition-root-env` refuse it. That refusal is no confidentiality control: `filepath.Dir` of any temporary path
  recovers `$TMPDIR`. It is acceptable because `$TMPDIR` holds no secret; the refusal keeps a helper
  from building paths off the temporary directory by habit, nothing more.
- **`os.Getwd` reads `$PWD`.** It returns the working directory only, a path the user chose, and
  stays allowed in the CLI and in tests.
- **A method called through `reflect`.** `reflect.ValueOf(t).MethodByName("Setenv").Call(...)`
  changes the environment past every name rule and the AST selector check; no lint rule here can
  see a method named by a string.

## Domain-is-pure rule

The domain does no I/O and reads no clock. The depguard rule `domain-is-pure` holds it to an allow list
of pure standard packages, but it allows `fmt` for `fmt.Errorf` and `time` for `time.Time`, and both
hold functions that reach standard I/O, the clock, zoneinfo files or the environment. An import rule
cannot see a call, so the forbidigo group `domain-is-pure` refuses those functions. It holds for
production files under `internal/domain/` only: two exclusion rules at the end of
`linters.exclusions.rules` drop its issues in every other path and in `_test.go` files, godoc examples
included, so the CLI and tests may print and read the clock.

### Why each entry is listed

Each claim was checked against the Go 1.27 source.

| Entry | Refuses | Why |
|---|---|---|
| `fmt.Print*` | `fmt.Print`, `Printf`, `Println` | They write to `os.Stdout`. |
| `fmt.Scan*` | `fmt.Scan`, `Scanf`, `Scanln` | They read `os.Stdin`. |
| `print` and `println` | the builtins | They write to standard error. `pkg: '^$'` matches a name with no package, which is the builtin, so a call of a domain function named `println` is not refused. A `print :=` that shadows the builtin is refused at the `:=`, because forbidigo finds no package for a name being declared. |
| the clock | `time.Now`, `Since`, `Until`, `Sleep`, `After`, `Tick`, `NewTimer`, `NewTicker`, `AfterFunc` | They read or wait on the clock. |
| `time.Timer` and `time.Ticker` | the types, their methods (`Stop`, `Reset`) and their field `C` | A timer or ticker handed in still waits on the clock. forbidigo renders `(*time.Timer).Stop` as `time.Timer.Stop`, so one pattern, `^time\.(?:Timer\|Ticker)(?:\.\w+)?$`, covers the type, each method and `t.C`. |
| `time.LoadLocation` | `time.LoadLocation` | It reads the `ZONEINFO` variable and zoneinfo files. |
| `time.Local` | the variable `time.Local` and the method `Time.Local` | A time in `Local` reads the `TZ` variable and `/etc/localtime` when its fields, zone or text are first read (`initLocal`). |
| `time.Unix*` | `time.Unix`, `UnixMilli`, `UnixMicro` | They return a time in `Local`, as `Time.Local` does. |
| a zone offset matched against `Local` | `time.Parse` and the `Time` methods `UnmarshalJSON`, `UnmarshalText`, `UnmarshalBinary`, `GobDecode` | They match a zone offset or abbreviation in the value against `Local`. |

What stays allowed takes or returns the data, or names its location: `fmt.Errorf`, `Sprint*`,
`Sscan*`, and `Fprint*` and `Fscan*` on a writer or reader passed in (the domain cannot import `os` to
reach a standard stream); `time.ParseInLocation` with `time.UTC` or a `*time.Location` passed in,
`time.FixedZone`, `time.Date`, and the `Time` methods that read no zone, such as `Unix` (an `int64`),
`UTC`, `In` and `Format`. Each entry's msg says what to use instead.

### How the domain rule is pinned

`TestDomainIsPureProbe` runs forbidigo, with the same harness as `TestCloudReadOnlyProbe`, on a probe
domain package (`test/lint/testdata/domainispure`). It has a line for every name an entry's pattern
lists, each marked `// want "<msg prefix>"` with the start of its own entry's msg, so narrowing a
pattern fails the probe, and lines for a method value (`_ = t.Local`), a func value
(`f := time.Now`), a method expression (`time.Time.Local`) and an aliased import. The pure calls, a
domain function named `println`, and the refused calls in a domain test and a CLI file must pass.
`TestDomainIsPurePatterns` checks the patterns, offline, against lists of refused and allowed names.
`TestForbidigoExclusionsDropOneGroup` pins the exclusion rules of every group: every rule that drops
forbidigo issues names forbidigo alone with a text of `because "<tag>: [^"]*"$`, or, for a named
exception, that form with one entry's exact msg in place of `[^"]*` (composition-root rule above), the
cloud-read-only group has none, and the domain-is-pure rules are exactly `path-except: ^internal/domain/`
and `path: _test\.go$`. A new group adds its tag and states its rules, even none. A line in the
cloud-read-only probe passes the string `"domain-is-pure: x"` to a refused call, so a text of just the
tag fails the probe.
For a new entry, add its names to both, each on a line of its own in the probe.

### Gaps in the domain rule

forbidigo matches a selector against its static type, so these get through:

- **A call through an interface or a type parameter.** `Local` or an `Unmarshal*` method called on a
  domain interface that a `time.Time` satisfies is matched against the interface, and one called
  through a type parameter has no type name.
- **A method promoted from an embedded `time.Time`**, which is matched against the embedding type.
- **A `time.Time` handed to the domain already in `Local`, or a `*time.Location` handed in that is
  `time.Local`.** Formatting such a time reads `TZ`, and so does building one with `time.Date` or
  `time.ParseInLocation` in that location, or in `t.Location()` of a `Local` time. No call rule can see
  where the value came from. The adapter converts a time with `.UTC()` before handing it in, and passes
  `time.UTC` or a location it loaded, never `time.Local`.
- **A dot import of `time`.** After `import . "time"`, `Local.String()` is not reported, yet it reads
  `TZ` and `/etc/localtime` through `initLocal`. staticcheck's ST1001 refuses dot imports, so this gets
  through only behind a nolint directive that silences it.
- **A receive from a channel handed in.** `<-t.C` on a `time.Timer` or `time.Ticker` is refused through
  the field `C`, but a `<-chan time.Time` passed as a parameter is a plain channel.

## Domain model

| Package | Holds |
|---|---|
| `taxonomy` | Provider-neutral categories (`compute.vm`, `compute.baremetal` (ADR 0023), `kubernetes.managed`, `storage.object`, `database.postgresql`, `serverless.function`, `messaging.queue`, ...) and the capability vocabulary |
| `inventory` | Resources discovered in a source: category, region, size, capabilities used, dependencies, and the raw provider type for traceability |
| `catalog` | Offerings of a target: categories, capabilities, plans and sizes, EU regions |
| `mapping` | Rules that match a resource to candidate offerings and assign a fit (direct, partial, none) with reasons |
| `assessment` | Per-resource and per-workload verdicts, blockers, required changes, readiness score |
| `text` | Validation and bounded quoting of untrusted free text (IDs, raw types, regions, names); no output-format escaping |

Why a taxonomy: with S sources and T targets, direct service-to-service tables need S×T mappings that
drift independently. With a taxonomy, each provider maps once into a shared vocabulary (S+T), and the
matching rules live in one place.

## Ports (declared in `internal/app/analysis`)

The shapes and their alternatives are in [ADR 0005](adr/0005-context-first-ports-and-fstest-style-contract-suites.md).

| Port | Method | Implemented by | Pinned by |
|---|---|---|---|
| `InventorySource` | `Inventory(ctx) (inventory.Inventory, error)` | `adapter/source/inventoryfile` (the JSON inventory file, versions 1 and 2, [ADR 0006](adr/0006-json-inventory-file-format-v1.md), [ADR 0025](adr/0025-json-inventory-file-format-v2.md)), `adapter/source/{aws,azure,gcp}` | Contract suite `adapter/source/sourcetest` |
| `CatalogSource` | `Catalog(ctx) (catalog.Catalog, error)` | `adapter/target/{ovhcloud,scaleway,upcloud}`. OVHcloud, Scaleway and UpCloud each serve an embedded, dated snapshot read through the shared reader `adapter/target/snapshot` ([ADR 0007](adr/0007-shared-target-catalog-snapshot-format-v1.md), schema version 2 since [ADR 0024](adr/0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md)) | Contract suite `adapter/target/targettest`; the snapshot format once, in `adapter/target/snapshot` |
| `Reporter` | `Report(ctx, w io.Writer, assessment.Assessment, ReportMeta) error` | `adapter/report/markdown` (the default) and `adapter/report/jsonreport` (the versioned JSON report, [ADR 0018](adr/0018-version-the-json-report-format-for-the-cli-and-the-api.md)), selected by `--format` | Golden files; the JSON goldens also against their JSON Schemas |
| `ComparisonReporter` | `ReportComparison(ctx, w io.Writer, assessment.Comparison, ReportMeta) error` | `adapter/report/markdown` (PRD #89, milestone 2) and `adapter/report/jsonreport` (PRD #103); a second port so a reporter opts in to comparisons ([ADR 0016](adr/0016-compare-targets-through-a-second-reporter-port-and-use-case.md)) | Golden files; the JSON goldens also against their JSON Schemas |

The use case is `analysis.New(inventorySource, catalogSource, reporter)` followed by
`Run(ctx, w, meta)`. Run checks `ReportMeta` (the inventory file's base name must pass `text.Validate`)
before any port is called, loads the inventory and the catalog, assesses, checks the context itself and
renders to `w`. The driving adapter chooses `w` (standard output or a file) and owns it. Blockers are a
result, not an error.

A comparison of several targets is `analysis.NewComparison(inventorySource, []analysis.Target{{Key, Catalog}}, comparisonReporter)`
followed by `Run(ctx, w, meta)`: the inventory is read once, each target's catalog is loaded and
assessed in key order, `assessment.Compare` derives the blockers every target shares, and the
comparison is rendered. Any target's failure fails the run with one line naming the target
([ADR 0016](adr/0016-compare-targets-through-a-second-reporter-port-and-use-case.md)).

A contract suite exports `Check(ctx, src, want) error`, which returns every violation joined and named
after its check, and `Run(t, src, want)`, which fails the test on a violation. A target adapter declares
its expectations itself: its display name, its EU regions and the categories its catalog must serve.

## Testing strategy

| Level | Scope | Location | Runs in CI |
|---|---|---|---|
| Unit | Domain rules, use cases with fakes | next to the code, `package foo_test` | yes |
| Contract | Behaviour every source or target adapter must share | `adapter/source/sourcetest`, `adapter/target/targettest` | yes |
| Golden | Report and CLI output | `testdata/*.golden` | yes |
| Integration | Real provider APIs, read-only | `//go:build integration` | no (manual, credentials) |
| E2E | The built binary against fixtures | `test/e2e`, `//go:build e2e` | yes |
| Source rules | Rules on the Go source and its packages that golangci-lint cannot express, and the probes that prove the cloud-read-only and domain-is-pure lint rules fire | `test/lint`, `test/architecture` | yes |

Most tests sit in the domain, where they are fast and need no fakes. Adapters are tested against
recorded, sanitised API responses through the contract suites, so adding a provider means passing an
existing suite rather than inventing new expectations.
