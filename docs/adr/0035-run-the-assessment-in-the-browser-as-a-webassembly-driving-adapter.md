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
  agreement through the real CLI over valid and invalid choices: each target key, `all`, several targets,
  unknown, empty and duplicate targets, regions per target (unknown, empty, repeated or for a target not
  chosen), the estimate switch and the JSON report's schema versions.
- **`cmd/shift2eu-web`, the composition root, in two parts.** `cmd/shift2eu-web` itself (build constraint
  `js && wasm`) is the thin `syscall/js` glue. It registers one function, `shift2euAssess`, converts the
  page's values to `web.Options` and converts the result back. It imports only `wire`, `internal/web`,
  `syscall/js`, `context` and `errors` (depguard `web-composition-root`). Its untagged package
  `cmd/shift2eu-web/internal/wire` constructs what `cmd/shift2eu`'s offline path constructs into `web.Deps`:
  the inventory reader in its in-memory mode, the three embedded catalogs, and the Markdown, JSON and HTML
  reporters (`web-composition-root-wire`). Wire's tests run `internal/cli` and `internal/web` over the same
  adapters: one option-parity table, and download parity over every example inventory and the hostile
  fixtures, across one target and the comparison, chosen regions, the estimate on and off, and the JSON
  report's schema versions. Only those test files may import `internal/cli`
  (`web-composition-root-wire-tests`); every other test file under `cmd/shift2eu-web` is refused it
  (`web-root-tests-never-run-the-cli`), and `cli-and-cmd-imported-only-by-cmd` leaves out only the glue's
  own files, which import `wire`.
- **The glue is hardened against the page's values.** It reads, sets and calls JavaScript names only through
  three helpers that take a `jsName`, and every name is a constant of that type: the constants are the
  glue's named allow list, it never computes a name, and none of them reaches another window (PRD #297,
  *Layer 1*; milestone 2's static test pins the rule). `targets` and `regions` must be real arrays
  (`Array.isArray`) of at most 64 entries each (`maxEntries`), of strings and of `{target, region}` objects
  of strings; anything else, or a mistyped `estimate`, `schemaVersion` or `inventoryName`, is a `usage`
  failure with a constant message that echoes no value. The bound is fixed and generous on purpose: it
  refuses a hostile `length` before any entry is read, yet any realistic list (a target named twice, more
  regions than targets) reaches the option rules, so the page shows the CLI's usage error, as the PRD
  requires, rather than the glue's generic one. A bound derived from the number of targets would have
  hidden those messages. A panic during an assessment is recovered and
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
  | Raw module | 6,688,360 |
  | Base64 module script | 8,917,848 |
  | Gzip-compressed module script | 2,513,376 (2.40 MiB, 80% of the budget) |

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

**Note of 2026-11-20, issue #347: the exact go line is the pin, and every build runs that release.** The
decision is unchanged; this states how the pin holds.

- **The pin is `go.mod`'s go line, one exact patch release (`go 1.27.4`), and nothing beside it.** It ties
  the module and the bundled `wasm_exec.js` to one toolchain. A `toolchain` line equal to the go line does
  not last: `go mod tidy` drops it, and CI (`go mod tidy -diff`), `make tidy` and Renovate's `gomodTidy` all
  run tidy. A `toolchain` line that differs would split the pin, the module's minimum Go from the Go it is
  built with. `tools/go.mod` has the same go line (test/architecture, `TestToolsModuleFollowsTheRootModulesGo`).
- **A line in `go.mod` never stops a newer local Go.** Go only switches up: a go or toolchain line older
  than the local Go is satisfied by it, so the newer Go builds the module and its `GOROOT` provides
  `wasm_exec.js`. The pin is therefore enforced where Go runs:
  - The Makefile exports `GOTOOLCHAIN=go<go line>`, read from `go.mod` in one place. Every go command it
    runs (sub-makes, `go -C tools`, `dev/` scripts, `ci/sitebuild`'s `go env GOROOT` and builds) runs
    exactly that release, the local one when it matches. Otherwise the go command fetches the pin as the
    module `golang.org/toolchain` through `GOPROXY`, verified against the checksum database
    (`sum.golang.org`), so `make` can then touch the network, and the fetch is only as trustworthy as the
    user's `GOSUMDB` settings: `GOSUMDB=off`, or a `GONOSUMDB` or `GOPRIVATE` pattern matching
    `golang.org`, turns the check off. Normally nothing is fetched: the VM installs the pinned Go from its
    checksum-verified tarball (`dev/lima.yaml`), and CI and the release (below) never download. The
    Makefile keeps only the value `local`, which never downloads and lets the test below fail on a
    mismatch; every other value is replaced.
  - CI and the release run with `GOTOOLCHAIN=local`. `actions/setup-go` installs the go line's release
    (`go-version-file: go.mod`) and exports `GOTOOLCHAIN=local` for the job's later steps, so no job
    downloads another Go. The workflows set nothing of their own: the checks on `ci.yml` and `release.yml`
    allow no workflow-level `env:`, and the release check refuses any go-command variable in
    `release.yml`. The Dockerfile's builder image is tagged with the go line (`test/container`); the
    official golang image sets `GOTOOLCHAIN=local` in its own `ENV`, and the Dockerfile sets nothing of
    its own. Renovate's "Go toolchain" group bumps the go lines, `dev/lima.yaml`'s `goVersion` and that
    image in one PR.
- **Two tests catch a mismatch.** `TestGoModPinsTheBuildingToolchain` (`test/site`) fails unless the go line
  is an exact patch release, `go.mod` has no `toolchain` line and the Go that compiled the test is the go
  line's (`runtime.Version()`). `TestWasmExecIsTheToolchains` fails unless the bundled `wasm_exec.js` is the
  building toolchain's, byte for byte.

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

## Amendment: constant classes in the fragment (issue #373, 2026-11-20)

The site's redesign (PRD #297, issue #373) draws a fit as a coloured badge, colours the Summary rows, shows
each report section as a card and the Cost estimate's total as a large figure. A stylesheet cannot select an
element by its text, so it cannot tell `<td>direct</td>` from `<td>none</td>`, or the Blockers list from the
Required changes list. The page may not run a script over the report to add classes either: it would have
to read values to decide them. So the fragment now carries `class` attributes, written by `htmlreport`.

Every class is a constant chosen from a closed set, never derived from a value:

| Element | Classes | Chosen from |
|---|---|---|
| A Fit cell of a Resources table, and each per-target fit cell of the comparison's (the comparison's cells removed by the amendment of 2026-11-22) | `fit fit-direct`, `fit fit-partial`, `fit fit-none`; `fit` alone for a fit the report does not know | the `mapping.Fit` enum, by an exhaustive switch |
| A Summary table's rows, the comparison's included (superseded: the summary is boxes since the amendment of 2026-11-21, and the comparison's table went with the amendment of 2026-11-22) | `fit-direct`, `fit-partial`, `fit-none`, `fit-total` (its estimate rows carry none) | the row's role |
| A `section` around each report section | `part part-summary`, `part-resources`, `part-changes`, `part-blockers`, `part-workloads`, `part-cost`, `part-comparison` (removed by the amendment of 2026-11-22), `part-target` | the section's kind |
| The Cost estimate's total item | `total` | the item's role |

Both sets are fixed tables in `htmlreport` (`classNames`, `partNames`), keyed by unexported enums; the
renderer refuses a key outside them. So the rule above still holds: a value is only ever element content,
escaped as text, never a tag, an attribute or a class. The wrappers are `section` elements, never `div`,
as the browser test's `TestReportIsSemanticHTML` asks. The goldens were regenerated; they differ from the
earlier ones by the classes and the wrappers alone. `classes_test.go` refuses any class outside the closed
set, over the hostile fixtures too.

A class tied to a value's content, such as one for an estimate that could not be made, or a number in an
attribute (`<meter value>`), stays ruled out.

The fragment also carries one constant attribute beside the classes: `role="list"` on the `ul` of the
title list (the list after the `h2` that names the inventory file, the source, the catalogs and the
regions, in a report and a comparison alike) and of each Blockers and each Cost estimate section, at h3
and h4 level alike. These are exactly the lists the stylesheet strips of their markers
(`list-style: none`), and Safari with VoiceOver then drops the list role and the item count, so the list
is read as plain text; stating the role keeps it announced as a list (PR #379 review). Every other list
keeps its markers and needs no role; `test/site` sweeps the goldens against the stylesheet so a list it
strips without the role fails. The start tag comes from a fixed table in `htmlreport` (`listStartTags`,
keyed by an unexported enum), like the classes, so it is never derived from a value; no other list
carries a role, and the only other role is the table box's constant `region` (below). None of these lists can
be empty (the title list always names the source and a catalog, the Blockers list is written only when
there is a blocker, the Cost estimate list has fixed items), and an empty list would be left out as any
other is, so the goldens differ from the earlier ones by that attribute alone.

The stylesheet's fit and heading glyphs are decoration: the word in the cell or heading carries the fit,
so a browser that drops the glyph's `content: "…" / ""` declaration loses nothing.

## Amendment: the fragment's boxes and its one link (issue #373, 2026-11-21)

The maintainer's review of the redesign (PR #379) asked for the report to look like the mock-up: one
header line, the summary and the workloads as boxes, required changes and blockers side by side, a
resources table that scrolls in its own box, and a one-line cost estimate. The HTML fragment, and only
the fragment, changes; the Markdown and JSON reports keep their layout and their words.

- **The header** is one line, a `role="list"` list after the `h2` as before: `source <source> ·
  <inventory file> · catalog retrieved <date> · region <region>`. The region carries no note about how it
  was chosen. The dots are `<span aria-hidden="true">`, so a screen reader reads the items of a list. The
  `h2` holds the words "Migration assessment" in a `span` the stylesheet sets above the target's name.
- **"catalog retrieved <date>" links the catalog's `SourceURL`**, with `target="_blank"` and
  `rel="noopener noreferrer"`. This is the one value the fragment writes into an attribute. It is safe
  there because the domain refuses any catalog URL that is no absolute https URL, the reporter writes the
  link only for a URL that starts with `https://` (otherwise the words alone), and the URL is escaped by
  the same escaper as text, which turns both quotes, `&`, `<`, `>` and `+` into references, so it never
  ends the attribute. A link loads nothing into the page and is no request the Content Security Policy
  governs. `TestReportCatalogLinkIsEscaped` pins the escaping with a hostile URL.
- **Boxes are sections without a class**: the summary's tiles are `p` elements (badge, count, caption),
  each workload is a `section` holding its `h4`, its badges and its estimate, and one `section` holds the
  Required changes and Blockers parts. The stylesheet lays them out by their place (`.part-summary > p`,
  `.part-workloads > section`, `section:not([class]):has(> .part-changes)`), so no new class is needed for
  them. The summary is no table any more; a comparison's summary is still one (superseded by the
  amendment of 2026-11-22: a comparison has no combined summary, each target's is boxes, and the
  "Summary table" label leaves `tableLabels`).
- **A badge outside a table** is `<span class="fit fit-direct"><span aria-hidden="true">✓</span>
  direct</span>`: the glyph is markup, a constant of the package chosen by the badge's class, and hidden
  from screen readers. In a table the Fit cell keeps its classes and holds its word in a `span` the
  stylesheet draws as a bubble, with the glyph as CSS content, as before.
- **Every table sits in `<section class="table-scroll">`**, which scrolls sideways on its own, so a wide
  table never widens the page on a phone. `table-scroll` joins the closed set of classes (`classNames`).
  A box that scrolls must be reachable by keyboard, so it carries the constant attributes `tabindex="0"`,
  `role="region"` and an `aria-label` from a fixed table keyed by an unexported enum (`tableLabels`:
  "Resources table", "Summary table"), never from a value (PR #379 review). `region` is the second role
  value the fragment states, besides `list`.
- **Figures** are `strong` elements; the Cost estimate's total is the one with class `total`, holding the
  amount alone, with "per month, x of y resources" beside it, and one closing line: the region and date of
  the list prices, what they exclude, the storage the estimates leave out and the source's cost. The
  hours basis and the fixed-fee note stay in the Markdown report.

Every element and attribute is still a constant of the package, and every value but the link's URL is
element content. The goldens were regenerated with `-update`.

## Amendment: no combined comparison and no source cost in the fragment (issue #373, 2026-11-22)

The maintainer's third review of the redesign (PR #379) settled two more differences between the HTML
fragment and the Markdown report. Only the fragment changes; the Markdown and JSON reports, and so the
site's downloads, keep both.

- **A comparison shows each target's own sections and nothing else.** The combined block above them (the
  `part-comparison` section: its "Comparison" heading, the resources table with a fit, best offering and
  estimate column per target, and the "Blockers on every target" list) is gone, and `part-comparison`
  leaves the closed set of part classes (`partNames`). The title and the header line still name every
  target, and each target's section holds its Summary boxes, Resources table, Required changes and
  Blockers, Workloads and Cost estimate, one heading level lower. So no table has a column per target
  any more, and the class table above no longer has a per-target fit cell of the comparison's.
- **The fragment names the source's cost nowhere**: a workload box ends with "x of y priced", and the
  Cost estimate's closing line ends with the list prices' region and date, what they exclude and the
  storage the estimates leave out. Every amount the fragment shows is a target's list price, so it is
  always in euros. This replaces the earlier amendment's "and the source's cost" in both places.
