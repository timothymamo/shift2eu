# 0049. Write the PDF report with a standard-library writer, the core fonts and visible escapes

- Status: Accepted. Records PRD #584 milestone 1. The answers to compression and tagged PDF (below) are
  interim, for milestones 1 and 2; milestone 3 amends this ADR with the final decisions.
- Date: 2026-11-26

## Context

PRD #584 ([prds/584-pdf-report.md](../../prds/584-pdf-report.md)) gives the assessment a document people
can keep, attach to a ticket or hand to a manager or an auditor. Today there are three reporters:
Markdown, for people but shown as source text in most tools; the versioned JSON report
([ADR 0018](0018-version-the-json-report-format-for-the-cli-and-the-api.md)), for scripts; and the
site's HTML fragment ([ADR 0035](0035-run-the-assessment-in-the-browser-as-a-webassembly-driving-adapter.md)),
which lives only in the page. The browser's print dialog is no answer: its output depends on the browser,
the platform and the user's settings, carries a date and a URL in its margins, and cannot be written by
the CLI, so nobody could reproduce it.

Milestone 1 adds `internal/adapter/report/pdfreport` and `shift2eu assess --format pdf`. Milestone 2
makes the PDF the site's one download, byte-identical to the CLI's. The forces:

- **Hostile text.** Every value in a report comes from the inventory or a catalog and is untrusted. In a
  PDF, a value that ends its string could add operators or objects, actions, scripts or links.
- **Reproducibility.** Golden files pin reports byte for byte, the site's bundle is reproducible
  ([ADR 0036](0036-hold-the-site-to-three-trust-layers-and-test-them-in-a-headless-browser.md)), and the
  site's download must equal the CLI's on every release target, `js/wasm` included.
- **The size budget.** The WebAssembly module must stay within 3 MiB gzip-compressed and 11 MiB decoded
  ([ADR 0043](0043-check-the-live-site-against-the-release-and-hold-the-edge-to-a-size-budget.md)); it
  uses 2.39 MiB and 8.51 MiB today.
- **Layering.** An adapter may not import `internal/version`, yet the PDF must name the build that
  wrote it. The reporter ports and `analysis.ReportMeta` serve four reporters.
- **CLAUDE.md rule 6** prefers the standard library, and the composition roots' allow lists admit no
  third-party package.

The maintainer decided the PRD's twelve questions as recommended on 2026-11-25.

## Decision

`pdfreport` writes the assessment, or the comparison of several targets, as a PDF with a small
deterministic writer of its own, built on the standard library alone. The PDF is for people, like the
Markdown report, and is not a versioned format. It uses the three core fonts with WinAnsiEncoding,
embeds no font, writes every character those fonts cannot show as a visible escape, holds no varying
metadata and no active content, and is printable ASCII plus line feeds.

### The format: for people, not versioned

ADR 0018 versions the JSON report because scripts parse it. Nobody parses a PDF as a contract, so it
carries no `schemaVersion`: a layout change is not a breaking change, and the golden files pin the
layout byte for byte. `--schema-version` with `--format pdf` is a usage error (exit 2), as with
Markdown. What the PDF carries is the producing build's version line (below). The `%PDF-1.7` header
names the file format's version, not the report's.

The PDF holds what the Markdown report holds, in its order and words, plus three sections: *Resource
details* (every candidate offering with its fit and missing capabilities, and each resource's blockers,
required changes and estimate breakdown), *How this report is worked out*, and *About this report*. The
content is the PRD's (*The PDF*); this ADR records the format and the writer.

### The writer: standard library only

The writer emits the objects, the cross-reference table, the trailer and the content streams itself.
It is held by the existing `reporters-stay-offline` depguard rule, whose allow list grows only by the
standard packages it needs, each with a reason; no I/O package, no `math` and no third-party package.

The file it writes:

- **`%PDF-1.7`** (ISO 32000-1, the freely available ISO standard), using only features every reader has
  supported since PDF 1.3, so the header is a label, not a dependency. Not PDF 2.0, which deprecates the
  non-embedded core fonts this design relies on.
- **A4 portrait** (595 × 842 pt) only. The audience is EU-based, and the layout is tested for one size.
- **No title page.** Page 1 starts with the title block and the summary. *About this report* closes the
  document.
- **A header and a footer on every page**: "shift2eu migration assessment" and the target or "comparison
  of N targets"; "Page N of M" and the version line.
- **Tables** that repeat their header row on a new page, keep a row on one page unless it is taller than
  a page (then split at a line), and never end a page with a section heading.
- **Cross-reference entries of exactly 20 bytes**, each ending in a space and a line feed, the one
  two-byte end of line ISO 32000-1 (7.5.4) allows within the file's byte set.
- **Uncompressed content streams** (interim, below).

The exact operator set and the layout's margins and spacing are the writer's; its tests pin the operator
table it uses, and no graphics state dictionary, XObject or inline image is in it.

### Fonts and characters: core fonts, WinAnsiEncoding, visible escapes

Text uses three of the 14 standard (core) fonts, which every PDF reader has, so the file embeds none:
**Helvetica** for the report's own words, **Helvetica-Bold** for headings and table header cells, and
**Courier** for every value from the inventory or a catalog, as the Markdown report's code spans set
values apart. Each font is a standard Type 1 font with `/Encoding /WinAnsiEncoding` (ISO 32000-1,
Annex D.2). The writer carries the three fonts' character widths as integer tables to wrap text.

The fonts show exactly WinAnsiEncoding's characters: printable ASCII, the 27 characters it assigns in
0x80 to 0x9F, and the Latin-1 Supplement from U+00A1 to U+00FF except U+00AD. U+00A0 and U+00AD are left
out on purpose, since one looks blank and the other invisible. The report's own words use only this
set. Every value goes through `pdfreport`'s own escaper, which keeps every rune of the set and writes
every other rune as a visible escape in the style of Go's `strconv`, lowercase hex: `\u` and four digits
up to U+FFFF, `\U` and eight above it, `\x` and two digits per byte of invalid UTF-8, and `\\` for a
backslash, so an escape is never confused with a value that spells one. It never drops a rune, never
replaces one, adds no quotes, uses no short escape such as `\n`, and never truncates (the domain caps a
value's length, `text.Validate`). A wrap point never falls inside an escape, unless the escape is wider
than its whole column. *How this report is worked out* tells the reader what the escapes mean.

**No embedded font**, so no non-Latin glyph. A font that covers the scripts an inventory may hold would
cost far more than the module's decoded headroom of about 2.5 MiB (ADR 0043), and a subsetting font
writer is far more code than the rest of the writer. An escape keeps every character visible and
distinct, which is what a reader checking a report needs.

### Hostile text never becomes PDF syntax

- Every value is written only inside a PDF literal string, in a text-showing operator of a content
  stream: never as a name, a number, a dictionary key or value, a comment, a stream length or an object.
- Inside the string, `(`, `)` and `\` are escaped as `\(`, `\)` and `\\`, and every byte outside 0x20 to
  0x7E as a three-digit octal escape (`\ddd`): only the WinAnsi bytes of the characters the fonts show
  remain, since every other character is already an escape.
- Every name the writer emits is a constant of its own and holds no `#`, so no name can spell another
  with `#xx`.

**The file is printable ASCII plus line feeds:** every byte is 0x0A or in 0x20 to 0x7E. There is no
binary comment line and no compressed stream. Writing the PDF to a terminal cannot send an escape
sequence, so `--format pdf` writes to standard output or `--output` exactly as the other formats do,
with no terminal check, and the golden files diff as text.

### No active content

No link annotation, URI or launch action, JavaScript, open action, additional action, form, embedded
file, rich media or XFA: none of `/Annots`, `/URI`, `/Launch`, `/JavaScript`, `/JS`, `/OpenAction`,
`/AA`, `/AcroForm`, `/EmbeddedFile`, `/RichMedia` or `/XFA` appears, and no reference to an outside
resource. Object types are limited to catalog, pages, page, font, content stream and info. The catalog
snapshots' source URLs are shown as text. Some viewers make URL-shaped text clickable on their own; only
a click follows it.

### Reproducible: no timestamp, no varying metadata, integer arithmetic

The bytes depend only on the assessment, the report metadata and the version line: never the clock, the
environment or map iteration.

- **Metadata:** an `/Info` dictionary with one constant `/Title`, "shift2eu migration assessment", so
  viewers show a title and not a blob name. No `/Producer`, `/Author`, `/Creator`, `/CreationDate` or
  `/ModDate`, and no trailer `/ID`, which ISO 32000-1 requires only for encrypted files.
- **Integer layout arithmetic:** positions and widths are integers in thousandths of a point. No
  floating-point result can then differ between amd64, arm64 (which may fuse a multiply and an add) and
  `js/wasm`. A test fails if a non-test file of `pdfreport` imports `math`, uses a floating-point type
  or literal, `strconv.FormatFloat` or `ParseFloat`, or a floating-point format verb.

### The version line, by the constructor

`pdfreport.New(versionLine string)` takes the line `shift2eu version` prints
(`internal/version.String()`), which the PDF shows in every page footer and in *About this report*.
The composition roots, `cmd/shift2eu` now and `cmd/shift2eu-web/internal/wire` in milestone 2, pass
`version.String()`; both already import `internal/version`. The reporter ports and
`analysis.ReportMeta` do not change.

### The contents' answers

- **No readiness score.** shift2eu has none (PRD #2 left it open; PRDs #89, #190 and #297 keep it out of
  scope). *How this report is worked out* explains fits, blockers, required changes and estimates, and
  says in one sentence that there is no readiness score. A score is a domain decision for its own PRD.
- ***About this report* at the end**, with the version line also in every page footer, so a printed page
  always names its build. It lists the version line, every catalog snapshot's source URL, retrieval
  date and default region, the options the report reflects, and the inventory file name and source.
- **The inventory's file name stays**, in the title block and *About this report*, as in the Markdown
  report: it ties the report to its input. The user chose the name, and milestone 2's privacy page says
  the PDF names it.
- **Independent validation:** the tests' own reader parses only the writer's subset, so milestone 1's PR
  records a manual check of the bundled examples' PDFs, single and comparison, in Chromium's and
  Firefox's built-in viewers, macOS Preview and Adobe Acrobat Reader. A pinned `qpdf --check` in CI is
  a follow-up, filed only if the maintainer wants it.

### Interim answers (milestone 3 decides)

- **Compression: none in milestones 1 and 2.** `compress/zlib` would make a large report several times
  smaller, but the file would no longer be ASCII, the goldens would depend on the Go release's deflate
  output, and the module would link `compress/flate`. Milestone 3 measures the PDF of a large inventory
  and decides; if it compresses, the printable-ASCII rule becomes "every byte outside a compressed
  stream is printable ASCII".
- **Tagged PDF: none in milestones 1 and 2.** A structure tree (PDF/UA) is substantial work for a
  standard-library writer. The page's HTML report stays the accessible view, under PRD #297's
  accessibility criteria. Milestone 3 weighs tagging, or files it.

## Alternatives considered

- **The browser's print dialog or a print stylesheet.** Output varies by browser, platform and settings,
  carries a date and a URL, and the CLI cannot write it: nobody can reproduce it, and the site would
  offer a document the "check what you run" promise does not cover.
- **A third-party PDF library** (such as a Go PDF generator). It would bring its own fonts, metadata
  defaults (producer, dates, IDs), floating-point layout and features (links, images, compression) that
  this design must keep out, and each would have to be proved absent, release after release. The
  composition roots' allow lists admit no third-party package, and rule 6 prefers the standard library.
  The subset needed here (text, rules, tables, page breaks) is small.
- **An embedded font with wide script coverage.** Shows non-Latin text as itself, but costs megabytes
  against a 2.5 MiB decoded headroom, or a font subsetter. Escapes keep every character visible.
- **Replacing unshowable characters with `?`.** Two different values would look the same; an escape
  keeps them distinct.
- **A versioned PDF (`schemaVersion`).** Nobody parses it; a version would turn every layout change into
  a breaking change with no reader to protect.
- **The version line in `analysis.ReportMeta`.** Touches every reporter and the application layer for
  one reporter's need.
- **US Letter, a title page, PDF 1.4 or 2.0, no `/Info`.** One tested size suits an EU audience; a title
  page makes a short assessment longer; 1.4 would read equally well but 1.7 has the ISO reference, and
  2.0 deprecates the core fonts; with no `/Info`, viewers show a blob name.
- **Floating-point layout.** The same expression can round differently on arm64 than on amd64 or
  `js/wasm`, which would break byte-identity across release targets.

## Consequences

- The CLI gains `--format pdf`, and milestone 2 can offer the site's one PDF download, byte-identical to
  the CLI's, without a font, a compression package or a third-party package in the module.
- The writer, its width tables, its escaper and its layout are ours to maintain. Its tests carry the
  weight: golden files (hostile ones included), a test-only reader that checks the cross-reference
  offsets, stream lengths and trailer, the structural invariants (only literal strings and the writer's
  fixed operator table in content streams, only its object types, only printable ASCII), a fuzz test
  that holds them, and the no-floating-point test.
- Non-Latin text shows as escapes such as `Ж`, which a reader must decode; *How this report is
  worked out* explains them.
- The PDF names the inventory's file name, which a user who passes the PDF on may want to rename first.
- The PDF is not accessible to screen readers until milestone 3 decides on tagging; the page's HTML
  report is the accessible view meanwhile.
- Milestone 1 updates CLAUDE.md, `docs/architecture.md`, the CLI guide, depguard and its pin, and
  `test/architecture`. Milestone 2 amends ADRs 0018, 0035, 0036, 0040 and 0043, and ADR 0015 for the
  commit stamp. Milestone 3 amends this ADR's interim answers.
