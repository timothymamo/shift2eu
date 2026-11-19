# 0035. Run the assessment in the browser as a WebAssembly driving adapter

- Status: Accepted
- Date: 2026-11-19

## Context

PRD #297 puts shift2eu on a static site, `www.shift2eu.com`, whose promise is that no inventory ever leaves
the user's browser. The assessment is offline already: the CLI reads an inventory, assesses it against
catalog snapshots embedded in the binary and renders a report. Go compiles to WebAssembly, so the same code
can run in the page. Milestone 1 builds that module and the rules around it. The pages, the CSP and the deploy
come in later milestones.

The module has to reuse the use cases, the target adapters, the inventory reader and the Markdown and JSON
reporters unchanged. Its downloads must be byte for byte what `shift2eu assess` prints for the same inventory
and options. It must accept and refuse the same options with the same messages. It has to render the report
as semantic HTML that a hostile inventory cannot turn into markup, and it must stay small enough to load: the
PRD sets a budget of 3 MiB on the gzip-compressed module script, the file the browser transfers, which holds
the module base64-encoded. It may not reach the network, a process or a cloud SDK, and only its own glue may
call JavaScript.

The maintainer settled the toolchain (Go's own, not TinyGo) and how option checking is shared before this
milestone (PRD #297, *Decisions*).

## Decision

The site's module is a third driving adapter, beside `internal/cli` and `internal/httpapi`, with its own
composition root, built with Go's standard toolchain for `GOOS=js GOARCH=wasm`:

- **`internal/web`, the driving adapter.** `web.Assessor.Assess` takes the inventory bytes and the options as
  the page supplies them. It returns the Markdown report, the JSON report and the HTML fragment, or a
  `*web.Error` whose kind is usage, inventory or internal. It gets every adapter through `web.Deps` and builds
  none. It imports only the application, two domain packages (assessment and inventory, the types of the ports
  it implements), `internal/version` and a named list of pure standard packages (depguard
  `web-gets-adapters-from-cmd`). It does not import `os`, `net` or `syscall/js`, so it builds and is tested
  natively. One run reads the inventory once: the use case gets one reporter that renders every format in turn,
  each into its own buffer, with the reporters the CLI uses. The source from `Deps` is wrapped, so a document
  the reader refuses is `KindInventory` however the use case wraps the error. Its message is the reader's,
  which names a position and a field and never a value (`inventoryfile.FromBytes`, PRD #103).
- **Option rules shared through the application layer only.** As the maintainer decided, the validated
  option rules moved from `internal/cli` into `internal/app/analysis` (`SelectTargets`, `SelectRegions`,
  `SelectSchemaVersion`, `RegionChoiceError` and their siblings, PRD #297 milestone 1). Each driving adapter
  keeps its own parsing: the CLI splits `--target` on commas and `--region` into pairs, and the page passes
  lists. The CLI's output, messages and exit codes are unchanged byte for byte: no golden changed. The web
  adapter shows the CLI's message without its `shift2eu: ` prefix. Wire's option-parity table pins the
  agreement over 22 option sets through the real CLI.
- **`cmd/shift2eu-web`, the composition root, in two parts.** `cmd/shift2eu-web` itself (build constraint
  `js && wasm`) is the thin `syscall/js` glue. It registers one function, `shift2euAssess`, converts the
  page's values to `web.Options` and converts the result back. It imports only `wire`, `internal/web`,
  `syscall/js`, `context` and `errors` (depguard `web-composition-root`). Its untagged package
  `cmd/shift2eu-web/internal/wire` constructs what `cmd/shift2eu`'s offline path constructs into `web.Deps`:
  the inventory reader in its in-memory mode, the three embedded catalogs, and the Markdown, JSON and HTML
  reporters (`web-composition-root-wire`). Wire's tests run `internal/cli` and `internal/web` over the same
  adapters: one option-parity table, and download parity over every example inventory and the hostile
  fixtures with seven option sets. Only those test files may import `internal/cli`
  (`web-composition-root-wire-tests`); every other test file under `cmd/shift2eu-web` is refused it
  (`web-root-tests-never-run-the-cli`), and `cli-and-cmd-imported-only-by-cmd` leaves out only the glue's
  own files, which import `wire`.
- **The glue is hardened against the page's values.** It reads, sets and calls JavaScript names only through
  three helpers that take a `jsName`, and every name is a constant of that type: the constants are the
  glue's named allow list, it never computes a name, and none of them reaches another window (PRD #297,
  *Layer 1*; milestone 2's static test pins the rule). `targets` and `regions` must be real arrays
  (`Array.isArray`) of at most one entry per target (plus `all` for `targets`), of strings and of
  `{target, region}` objects of strings; anything else, or a mistyped `estimate`, `schemaVersion` or
  `inventoryName`, is a `usage` failure with a constant message that echoes no value. The glue never
  converts more entries than the option rules could accept. A panic during an assessment is recovered and
  returned as an `internal` failure with a constant message, so the module stays usable for the next one.
- **The trust boundary of the module's answer.** The object `shift2euAssess` returns has `error` and `kind`,
  plain text that the page shows with `textContent` and never as markup; `markdown` and `json`, the
  downloads' bytes, which the page offers as blobs and never inserts as HTML; and `html`, the only markup,
  the HTML reporter's fragment, inserted once. The glue's doc comment and `doc.go` state it. None of this
  can be tested natively, since the glue builds only for `js && wasm`: milestone 2's browser smoke test
  pins it, with the page script's static checks.
- **Only `cmd/shift2eu-web` imports `syscall/js`.** The lax rule `syscall-js-only-in-the-web-root` refuses it
  everywhere else, tests included. `web-imported-only-by-the-web-root` keeps `internal/web` and `htmlreport`
  to the site's root and its wiring, so neither the CLI, the HTTP API nor their roots import them. The
  existing rules name the site's root beside the other two. `test/architecture` runs `go list -deps` with
  `GOOS=js GOARCH=wasm` and fails on `net`, `net/http`, `os/exec`, a cloud SDK, `internal/cli`,
  `internal/httpapi`, or a package other than the glue (and `syscall` and `time`, whose js ports need it) that
  imports `syscall/js`.
- **`js/wasm` is linted as an explicit extra target.** No release builds it, so `.goreleaser.yaml` does not
  list it (ADR 0012). `make lint` runs golangci-lint for `LINT_EXTRA_TARGETS := js/wasm` with
  `CGO_ENABLED=0` beside the release targets. CI's lint matrix has the `js/wasm` row, and `test/architecture`
  holds the site's binary to the package rules on `js/wasm` and fails when either list drops the target.
  Without it, the `js && wasm` files would never be type-checked.
- **`internal/adapter/report/htmlreport`, the HTML reporter.** It implements the same two reporter ports as
  `markdown` and `jsonreport` and writes an HTML fragment with the Markdown report's content (summary,
  resources with fit, best offering, plan and estimate, required changes, blockers, workloads, cost estimate
  and its caveats; the comparison as `--target all` reports it). It adds a Region column that the hostile
  golden pins. The fragment is semantic HTML: headings h2 to h5, paragraphs, lists, and tables with
  `<th scope="col">`. It is built from a closed block model whose elements and attributes are constants. A
  value from the assessment or the metadata is only ever element content, inside `<code>`. It is escaped with
  `html/template`'s own replacements for text (`&`, `<`, `>`, `"`, `'`, `+` and NUL), after invalid UTF-8 and
  control characters become U+FFFD. **It does not use `html/template`** (below). It is held by
  `reporters-stay-offline`, which gains no package for it.
- **The size budget: 3 MiB for the gzip-compressed module script.** `TestWebModuleStaysWithinTheSizeBudget`
  builds the module as the site ships it (`-trimpath`, `GOOS=js GOARCH=wasm`, `CGO_ENABLED=0`), base64-encodes
  it as the module script carries it, gzips it and fails above 3,145,728 bytes. Measured with Go 1.27.4 at this
  commit:

  | | Bytes |
  |---|---|
  | Raw module | 6,688,906 |
  | Base64 module script | 8,918,576 |
  | Gzip-compressed module script | 2,512,821 (2.40 MiB, 80% of the budget) |

  Above the budget, TinyGo is reconsidered, as the PRD decides.
- **The standard toolchain, not TinyGo** (the maintainer's decision). One compiler builds the CLI and the
  module, so the downloads are identical by construction. `encoding/json` and reflection work fully, and no
  second toolchain trails Go's releases.
- **`wasm_exec.js` is tied to the toolchain and trusted as toolchain code.** The site's glue needs Go's
  `wasm_exec.js` for its version of the runtime ABI. It is taken at build time from the toolchain that
  builds the module, `$(go env GOROOT)/lib/wasm/wasm_exec.js`, and is never committed and never fetched. The
  toolchain is pinned by `go.mod`'s `go` line (`go 1.27.4`; there is no `toolchain` line), which Renovate
  updates, with every CI and release job that installs Go setting it up from `go.mod`
  (`actions/setup-go` with `go-version-file: go.mod` in `ci.yml`, `release.yml`, `security.yml` and
  `ai-review.yml`; `security.yml` also fails when the installed version differs from the declared one).
  Milestone 2's job that builds the site does the same. It is Go's own code, shipped with and reviewed as the toolchain, so
  it is trusted like the compiler rather than scanned by the site's banned-API checks, which cover the
  site's own script and the glue. Milestone 2 bundles it and adds the test that fails when the bundled file
  differs from the building toolchain's.

## Alternatives considered

- **Render the fragment with `html/template`**, as the PRD first said. It was built that way first and its
  output is the reporter's goldens. But `text/template` calls methods through reflection, so the linker keeps
  every exported method of every reachable type. The module grew from 6.68 MB to 10.03 MB, and the compressed
  module script to 3,638,609 bytes, 16% over the budget, for one template. The fragment needs only one escaping
  context, element content, because no value reaches a tag, an attribute, a URL, a script or a style. So the
  reporter writes its constant elements itself and escapes values with `html/template`'s replacements for that
  context. Its output reproduces the `html/template` goldens byte for byte, the hostile one included. To go
  back to `html/template`, the budget would have to rise to about 3.5 MiB, or the module would move to TinyGo.
- **TinyGo.** It produces a much smaller module, but its `encoding/json` and reflection support is partial,
  it trails Go's releases, and a second compiler could make the downloads differ from the CLI's. The
  maintainer decided against it unless the module goes over the budget.
- **Run the use case once per format.** It is simpler, but it reads and decodes the inventory three times and
  builds each catalog three times. One reporter that renders every format reads once, and still uses the
  CLI's reporters unchanged.
- **Classify inventory errors by `inventoryfile.ErrTooLarge` and its siblings.** `internal/web` may not import
  an adapter. Wrapping the source at the boundary marks every refusal of the document, the size cap included,
  without naming the adapter's errors.
- **One composition-root rule for `cmd/shift2eu-web` and `wire`.** A single allow list would let the glue
  import adapters and the wiring import `syscall/js`. Two strict rules, plus one for wire's tests, keep each
  package to what it needs.
- **Let the page call `shift2eu-server`.** That would break the product's promise: the inventory would leave
  the browser.

## Consequences

- The site's module carries the same use cases, catalogs and reporters as the CLI. A parity test fails on
  any difference in options, messages or downloads, over every example and the hostile fixtures.
- `make lint` and CI lint one more target. Every `js && wasm` file is type-checked and held to depguard.
- The module has headroom of about 600 KB compressed. A dependency that adds reflection-heavy code (another
  template engine, a large standard package) can use it up. The budget test fails then, and the choice is
  between dropping the dependency, raising the budget, or TinyGo.
- `htmlreport` owns its escaping. A change that writes a value into an attribute or a new context must escape
  it for that context. The hostile golden and the structural tests (`checkNoInjectedMarkup`) catch a value
  that reaches the fragment as markup.
- PRD #297 milestone 2 builds on this: the pages, `make site` with `wasm_exec.js` taken from `GOROOT`, the
  CSP, and the browser smoke test.
