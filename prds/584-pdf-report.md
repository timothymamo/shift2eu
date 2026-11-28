# PRD #584: The site's PDF report download

**Status:** In progress: milestone 1 done (the PDF writer `pdfreport` and `--format pdf`), milestone 1b
next (the PDF's visual styling and the contact banner, added by the maintainer on 2026-11-28), then
milestone 2. The maintainer decided all twelve questions under *Decisions* as
recommended (2026-11-25), and milestone 1b's five on 2026-11-28; the ADR is
[0049](../docs/adr/0049-write-the-pdf-report-with-a-standard-library-writer.md), amended for 1b.
**Issue:** #584

## Problem

The site (PRD #297, [prds/done/297-web-assessment-site.md](done/297-web-assessment-site.md)) shows the assessment in
the page and offers two downloads: the Markdown report and the versioned JSON report, byte for byte what
`shift2eu assess --format markdown` and `--format json` write. Neither works well for the person the site
is for: someone evaluating a move from AWS, Azure or GCP to OVHcloud, Scaleway or UpCloud. They want a
document they can keep, attach to a ticket or hand to a manager or an auditor.

- A Markdown file is source text. Opened without a renderer, it shows backticks, pipes and table rules;
  opened with one, it looks different in every tool.
- The JSON report is for scripts and CI jobs (ADR 0018). Someone who uses the site has no script to read it.
- Both carry only what the page shows, plus some estimate detail. Neither carries the evidence a reader
  needs to check the verdict: every candidate offering with the capabilities it lacks, the estimate's
  breakdown, how fits are worked out, and which release of shift2eu and which catalog snapshots produced
  the report.

The browser's print dialog is no answer. Its output depends on the browser, the platform and the user's
settings. It carries a date and a URL in its margins, and it is not something the CLI can write too. So
the site would offer a document that nobody can reproduce, and PRD #297's promise that users can check
what they run would not cover it.

## Outcome

`shift2eu assess --format pdf` writes the assessment, or the comparison of several targets, as a PDF
built by a new reporter, `internal/adapter/report/pdfreport`. The reporter sits beside the Markdown, JSON
and HTML reporters and uses the standard library only. The PDF holds what the Markdown report holds,
plus three more sections:

- **Resource details:** every candidate offering with its fit and missing capabilities, and the
  resource's blockers, required changes and estimate breakdown.
- **How this report is worked out:** how fits are decided.
- **About this report:** the shift2eu version line and every catalog snapshot's source URL and retrieval
  date.

The file is reproducible: the same inventory, options and build give the same bytes on every platform
and release target. It has no timestamp and no varying metadata, and no link, script, form, embedded
file or embedded font. Every value from the inventory or a catalog is escaped, so it shows as text and
can never become a PDF operator or object. A character the PDF core fonts cannot show is written as a
visible escape.

The site then offers exactly one download, **Download PDF**, which replaces the Markdown and JSON
buttons. It is byte-identical to the CLI's `--format pdf` output for the same inventory and options. The
site's JSON download goes away. The CLI keeps every format. Nothing leaves the browser: the PDF is made in
the WebAssembly module and offered through the page script's one existing download path.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | AWS, Azure and GCP, through the inventory documents the CLI and the site already read (schema versions 1 and 2, ADR 0006, ADR 0025). No source changes. | Live source adapters. Any source change. |
| Target providers | OVHcloud, Scaleway and UpCloud from the embedded catalog snapshots, one target, several, or all of them (`--target all`). Each can be scoped to a region (`--region`). No target changes. | Catalog changes. New targets. |
| Taxonomy categories | Every taxonomy category the release knows. The PDF renders them; it adds none. | New taxonomy categories. Any mapping, fit, estimate or assessment rule change. |
| Output | The PDF report: the single-target report and the comparison report, with estimates on (the default) and off (`--no-estimate`), with and without `--region`. `shift2eu assess --format pdf`. The site's one PDF download, which replaces its Markdown and JSON downloads. | Changes to the Markdown, JSON or HTML reports' content. A readiness score (see *Decided, as recommended*, 6). Embedded fonts. Tagged (accessible) PDF, PDF/A or PDF/UA in milestones 1 and 2 (*Decided, as recommended*, 9). Any server side: the HTTP API stays retired (ADR 0037). The browser's print dialog. |
| Site | The page script's download (one button, one blob), the WebAssembly wiring, the parity and browser smoke tests, the size budget check, and the privacy, verification and inventory guide pages' wording. | Any other page change. New browser APIs. Any CSP relaxation. |

## The PDF

The PDF is one document per run, A4 portrait (595 × 842 pt). Text uses three of the PDF's 14 standard
(core) fonts, which every PDF reader has and the file therefore never embeds: Helvetica for the
report's own words, Helvetica-Bold for headings and table header cells (from milestone 1b also badge
words, tile figures and the banner's eyebrow, heading and address), and Courier for every value
from the inventory or a catalog. Courier plays the part of the Markdown report's code spans: a reader
can tell a value from the report's own words.

### Characters: WinAnsiEncoding, everything else as an escape

Each font is used with the standard `/WinAnsiEncoding` (ISO 32000-1, Annex D.2). The core fonts can show
exactly the characters that encoding names:

- the printable ASCII characters, U+0020 to U+007E;
- the 27 characters WinAnsiEncoding assigns in bytes 0x80 to 0x9F: € ‚ ƒ „ … † ‡ ˆ ‰ Š ‹ Œ Ž ‘ ’ “ ” • – —
  ˜ ™ š › œ ž Ÿ (U+20AC, U+201A, U+0192, U+201E, U+2026, U+2020, U+2021, U+02C6, U+2030, U+0160, U+2039,
  U+0152, U+017D, U+2018, U+2019, U+201C, U+201D, U+2022, U+2013, U+2014, U+02DC, U+2122, U+0161,
  U+203A, U+0153, U+017E, U+0178). Bytes 0x81, 0x8D, 0x8F, 0x90 and 0x9D are unassigned;
- the Latin-1 Supplement, U+00A1 to U+00FF (bytes 0xA1 to 0xFF), except U+00AD SOFT HYPHEN.

U+00A0 NO-BREAK SPACE and U+00AD SOFT HYPHEN are left out on purpose. One shows as a blank and the other
as nothing or a hyphen, so two values that differ only by one would look the same. The rule is about
invisible and blank-looking characters only. Visible characters of the set that resemble ASCII ones
(‘ ’ and `'`, “ ” and `"`, – — and `-`, ‚ and `,`, ‹ › and `< >`, ˜ and `~`, ˆ and `^`, × and `x`) are
shown as they are, as in the Markdown report, and `text.Validate` does not refuse them.

The report's own words use only these characters. A value from the inventory or a catalog goes through
`pdfreport`'s own escaper. It keeps every rune of the set above as it is, and writes every other rune as
a visible escape. It never drops a rune and never replaces one with `?` or anything else, so a rune
outside the set is always visible as an escape. Two values that differ only inside the set can still
look alike, through the visible look-alikes above. It uses the escape forms of Go's `strconv` package,
with lowercase hex as `strconv` writes it:

- `\u` and four lowercase hex digits for a code point up to U+FFFF: U+0416 CYRILLIC CAPITAL LETTER
  ZHE becomes the six ASCII characters backslash, `u`, `0`, `4`, `1`, `6` (`\u0416`), and `é` stays `é`;
- `\U` and eight for one above it (`😀` is `\U0001f600`);
- `\x` and two hex digits for each byte of invalid UTF-8 (`\xff`);
- a backslash in the value is `\\`, so an escape can never be confused with a value that spells one.

The escaper differs from `text.Quote` and `strconv.Quote` in three ways. `text.Quote` is the style
reference, not the function to call:

- it escapes every rune outside the set above, printable or not, so a printable non-Latin letter such as
  `Ж` is escaped, where `strconv.Quote` keeps it;
- it adds no surrounding quotes, and `"` is printable ASCII, so it stays `"`; it uses no short escape
  such as `\n` or `\t`: a line feed is `\u000a`;
- it never truncates: `text.Quote` cuts a value after 128 runes, while the PDF shows the whole value,
  which the domain already caps (*Tables and page breaks*).

U+00A0 and U+00AD are outside the set, so they are always escaped, as the six ASCII characters
`\u00a0` and `\u00ad` (backslash, `u`, then the four hex digits), never shown as themselves.

This covers control characters, the line and paragraph separators, bidi overrides and other invisible
formatting characters, as well as every non-Latin script. `text.Validate` already refuses controls, bidi
and other format characters, the line and paragraph separators and invalid UTF-8 when the inventory is
read, so those reach the escaper only from a test: its handling of them is defence in depth.

No font is embedded to show the escaped characters. This keeps the WebAssembly module inside its size
budget (ADR 0043: 3 MiB transferred gzip-compressed, 11 MiB decoded, about 2.5 MiB of decoded headroom
today). The *How this report is worked out* section tells the reader
what the escapes mean.

### Page header and footer

- **Header**, every page: "shift2eu migration assessment" and, after it, the target's display name, or
  "comparison of N targets".
- **Footer**, every page: "Page N of M" and the shift2eu version line (below).

### Look (milestone 1b)

Milestone 1 writes black text and black hairline rules. Milestone 1b gives the PDF the site's look
(*Decided by the maintainer (2026-11-28)*), drawn with a few PDF path and colour operators (ADR 0049,
amended). It is not a copy of the site: every section, value and detail of milestone 1 stays, in the
same order and words, and only the contact banner (below) is added.

**Palette.** The colours are the site's light-theme tokens in `site/style.css`, nothing else. Each
component is written as a fixed decimal with three places, `byte × 1000 / 255` rounded to the nearest
thousandth in integer arithmetic (`(byte × 1000 + 127) / 255`, ADR 0049); the column shows the result.

| Name (`site/style.css`) | Hex | Written as | Where used |
|---|---|---|---|
| `--ink` | `#14213d` | `0.078 0.129 0.239` | Body text and values, table cells, the total tile's rule and figure |
| `--muted` | `#4a5568` | `0.290 0.333 0.408` | Page header and footer, the title block's header facts, tile captions, "none" and "not estimated" notes |
| `--mast` | `#0b2a66` | `0.043 0.165 0.400` | The title band and the contact banner's fill |
| `--mast-ink` | `#ffffff` | `1.000 1.000 1.000` | The title and the banner heading on navy |
| `--mast-link` | `#c9d8ff` | `0.788 0.847 1.000` | The banner's eyebrow on navy |
| `--mast-text` | `#e6ebf5` | `0.902 0.922 0.961` | The banner's text on navy |
| `--accent` | `#0b3d91` | `0.043 0.239 0.569` | Section headings, the estimate tile's figure |
| `--gold` | `#ffcc00` | `1.000 0.800 0.000` | The title band's and per-target headings' left rule, the banner's top rule and its email box |
| `--gold-ink` | `#0b2a66` | `0.043 0.165 0.400` | The email address in the gold box |
| `--direct-ink` / `--direct-bg` | `#0f6b3a` / `#e3f4ea` | `0.059 0.420 0.227` / `0.890 0.957 0.918` | Direct badge (word and icon / fill), the Direct tile's rule and figure |
| `--partial-ink` / `--partial-bg` | `#874600` / `#fdf0d5` | `0.529 0.275 0.000` / `0.992 0.941 0.835` | Partial badge, the Partial tile's rule and figure |
| `--none-ink` / `--none-bg` | `#a3201a` / `#fde4e1` | `0.639 0.125 0.102` / `0.992 0.894 0.882` | None badge, the None tile's rule and figure |
| `--info-bg` / `--info-rule` | `#e8eefb` / `#0b3d91` | `0.910 0.933 0.984` / `0.043 0.239 0.569` | The required-changes box (fill / left rule) |
| `--warn-bg` / `--warn-rule` | `#fdf0d5` / `#a86400` | `0.992 0.941 0.835` / `0.659 0.392 0.000` | The blockers box (fill / left rule) and its warning icon |
| `--warn-ink` | `#5c3a00` | `0.361 0.227 0.000` | The blockers box's heading |
| `--ring` | `#dde2ea` | `0.867 0.886 0.918` | Table row rules, tile and card outlines, the rule above a section heading, header and footer rules |
| `--surface-2` | `#eef1f6` | `0.933 0.945 0.965` | Table header rows, the band heading each block of *Resource details* |
| `--bg` | `#f6f7f9` | `0.965 0.969 0.976` | Workload cards' fill, even table rows |

The page itself stays unpainted (white). `--warn-bg` and `--partial-bg` are the same colour on the
site, so one written value serves both. No dark-theme token is used: a PDF has one look.

**Shapes.** Every shape is a rectangle or a straight-line path; there are no curves and no rounded
corners.

- **Title block:** a `--mast` band across the text width on page 1, with a 4 pt `--gold` rule at its
  left edge, holding "Migration assessment: <target>" (or the comparison's title) in Helvetica-Bold,
  `--mast-ink`. The header facts follow below the band, `--muted`, values in Courier as today.
- **Page header and footer:** their words in `--muted`, each separated from the body by a 0.5 pt
  `--ring` rule.
- **Section headings:** Helvetica-Bold in `--accent`, with a 0.5 pt `--ring` rule across the text width
  above them, as the site's `#report .part`. A per-target section of a comparison is headed by the
  target's display name in `--ink` with a 4 pt `--gold` left rule, as the site's `.part-target`.
- **Fit badges:** a filled rectangle in the fit's `-bg` colour, holding the fit's icon and its word,
  "Direct", "Partial" or "None", in Helvetica-Bold, both in the fit's `-ink` colour. A badge can carry a
  count before the word ("3 Direct"), as a workload card's fit counts do. Badges stand wherever a fit is
  shown: the resources table's Fit cell, the comparison's stacked cells, the candidate table of
  *Resource details*, the summary tiles and the workload cards. The word is always there.
- **Summary tiles:** the summary table becomes one row of tiles: one per fit, then the total, then, with
  estimates on, the monthly estimate. Each tile has a 0.5 pt `--ring` outline and a 3 pt rule along its
  top in its fit's ink (`--ink` for the total, `--accent` for the estimate); it holds the fit's badge
  (the total and estimate tiles: their label), the count as a big Helvetica-Bold figure in the same
  colour as the rule, and its caption ("of N resources", "resources in total", "per month, estimated") in
  `--muted`. The comparison's summary table stays a table (one column per target), with badges in its
  row labels.
- **Required-changes box:** the section on an `--info-bg` fill with a 4 pt `--info-rule` rule at its
  left edge, as the site's `.notice`.
- **Blockers box:** the section on a `--warn-bg` fill with a 4 pt `--warn-rule` rule at its left edge,
  as the site's `.notice.warn`, its heading in `--warn-ink` after the warning icon. The "affected by a
  blocker on" notes stay inside it. The comparison's blockers on every target use the same box.
- **Workload cards:** one card per workload, the text width wide, stacked: a `--bg` fill with a 0.5 pt
  `--ring` outline, the workload's name in Courier, then its fit counts as badges, its estimate total and
  source cost, and its blockers, as today's lines.
- **Tables:** the header row on a `--surface-2` fill, every even body row on `--bg`, and a 0.5 pt
  `--ring` rule under every row in place of today's black hairlines. Cell text in `--ink`.
- ***Resource details*:** each block's ID on a `--surface-2` band across the text width; the block's
  candidate table, blockers and required changes in the same shapes as above.
- **The contact banner:** see *The contact banner* below.

**Icons.** Vector paths of the operators above, no font glyph and no image, each drawn in a square the
height of its badge's text:

- **Direct: a check mark** (the site's `✓`): a stroked polyline of three points.
- **Partial: a half-filled disc** (the site's `◐`): a circle drawn as a regular 16-sided polygon,
  stroked, with its left half filled. Its vertices are a constant table of integers in thousandths of a
  point for the one icon size, never computed with floating point.
- **None: a cross** (the site's `✕`): two stroked diagonal lines.
- **Warning, in the blockers box's heading: a triangle** filled in `--warn-rule`, with an exclamation
  mark as two small `--mast-ink` (white) rectangles inside it.

The three fit icons are the shapes `htmlreport` and the site use (`fragment.go`'s glyphs and the
stylesheet's `::before` content), so a reader who knows the page recognises them. The triangle is the
PDF's own: the site's blockers carry the cross, but in the PDF the cross means fit "none", and a
blocker is not a fit.

**Colour is never the only signal.** Every badge keeps its word beside its colour and icon, and the
three icons differ in shape, so a fit reads the same printed in greyscale or by a reader who cannot tell
the colours apart. Every coloured text is a dark ink on a light fill, or `--mast-ink`, `--mast-link` or
`--mast-text` on `--mast`, or `--gold-ink` on `--gold`, the site's own pairs, so it stays legible in
greyscale. A box's meaning is in its heading's words, not only its fill.

**The contact banner.** The site's "Planning a migration?" band (`site/index.html`,
`section.contact-band`) in the PDF, once per document:

- a `--mast` fill the text width wide with a 3 pt `--gold` rule along its top (the site's inset gold
  shadow);
- the eyebrow "Planning a migration?" in Helvetica-Bold, `--mast-link`;
- the heading "Questions about your results, or how to move forward?" in Helvetica-Bold, `--mast-ink`;
- the text "We're happy to help you read an assessment, weigh the EU options, or plan the move itself.
  Email info@shift2eu.com." in Helvetica, `--mast-text`. The apostrophe is the ASCII `'` (U+0027),
  the character `site/index.html` uses;
- a `--gold` box holding "info@shift2eu.com" in Helvetica-Bold, `--gold-ink`, in place of the site's
  gold button. The site's button reads "Email info@shift2eu.com"; the PDF's box holds the address alone,
  since the text just before it already says "Email".

It is **plain text on a filled box**: no link annotation, URI, action or anything else ADR 0049 forbids.
A viewer may make the address clickable on its own, as with any URL-shaped text (*ADR 0036* below).

Where it sits:

- **Single-target report:** right after the last of the sections from Summary to Cost estimate that the
  report shows (Cost estimate with estimates on; otherwise Workloads, Blockers, Required changes or
  Resources, whichever is last), and before *Resource details*.
- **Comparison:** right after the *Comparison* section (its summary, resources and blockers tables) and
  before the first per-target section. The per-target sections hold no banner.

The reason: that is where the site places it, right after the results it shows. At the very end it would
sit behind pages of resource details, how-it-works and about text for a large inventory, where few
readers would reach it.

**Boxes across pages.** A box is the blockers box, the required-changes box, a workload card, a summary
tile row, a *Resource details* block's ID band and the banner.

- A box's header (its heading, or a table's header row) never ends a page: when the header and its first
  row or line do not both fit in the space left, the box starts on the next page.
- A box that continues on a new page is drawn again there: its fill, its rules and its outline are drawn
  on each page for the part of the box on that page, so no text of the box stands outside its fill. Its
  heading is not repeated; a table's header row repeats as today.
- The banner, a summary tile row and a workload card are never split: when one does not fit in the space
  left, it moves whole to the next page. A workload card taller than a page's body, which only a
  hostile inventory makes, continues as a box does.
- A badge stays on its line, beside its word.

### Single-target report

The sections follow the Markdown report's order and words (`internal/adapter/report/markdown/doc.go`),
then add the detail sections. With the layout of today's reports:

| PDF section | Markdown report | HTML fragment (the page) | What the PDF adds |
|---|---|---|---|
| Title block: "Migration assessment: <target>", then the header facts (inventory file, source, catalog snapshot URL and retrieval date, region line) | Title and header lines | h2 title and header line | Nothing |
| Summary: a table of the resources per fit and in total; with estimates on, the monthly total | `## Summary` | Summary boxes | Nothing |
| Resources: one table row per resource, in the assessment's order: ID, category, fit, best offering with its tie note, then, with estimates on, the plan and the monthly estimate cell as the Markdown report words it | `## Resources` (also raw type, relations and reasons) | Resources table | Raw type, relations and reasons move to *Resource details*, so the table fits A4 portrait |
| Required changes | `## Required changes` | In one box with Blockers | Nothing |
| Blockers, the "affected by a blocker on" notes included | `## Blockers` | In one box | Nothing |
| Workloads, only when the assessment shows workloads: per workload its fit counts, with estimates on its total and the source's cost per currency, and its blockers | `## Workloads` | Workload boxes (no source cost) | Nothing beyond the Markdown report |
| Cost estimate, only with estimates on: total, basis, region, hours basis, billing caps, source cost | `## Cost estimate` | Total and one line | Nothing beyond the Markdown report |
| **Contact banner** (milestone 1b), right after the last of the sections above that the report shows | Not shown | The site's "Planning a migration?" band, after the report | The banner's plain text on a filled box (*Look*) |
| **Resource details** (new): one block per resource, in the assessment's order | Not shown | Not shown | See below |
| **How this report is worked out** (new) | Not shown | Not shown | See below |
| **About this report** (new) | Header lines only | Header line only | See below |

**Resource details.** One block per resource, headed by its ID. Each block holds:

- the raw type, category and region the inventory gives;
- the fit and the best offering, with the tie note;
- the relations and the "affected by a blocker on" notes;
- a table of **every candidate offering**, best first as the mapping orders them (`mapping.Candidate`):
  the offering, its fit, and the capabilities it lacks (`Missing`), or "none";
- the reasons (`mapping.Reason.Parts`, as the other reporters render them);
- the required changes, and the resource's blockers;
- with estimates on, **the estimate's breakdown**, from the same data the JSON report's breakdown takes:
  - the plan (name, vCPUs, memory, included storage, plan price), or "no plan large enough" with the
    largest plan;
  - the plan, storage and fixed-fee parts and the monthly total;
  - the count, marked "assumed" when the resource gives none, and the hours, marked `"assumed" (730)` or
    `as the inventory gives them`; as in the Markdown report, the PDF does not print the hours an
    inventory gives, because `HoursPerMonth` is a float and pdfreport has no floating point (ADR 0049);
  - the plan's billing cap, if any (ADR 0029), and "storage not included" when it applies (ADR 0032);
  - otherwise "not estimated" and the reason in plain words, as in the Markdown report.

A resource with no candidate shows "no offering in the catalog serves this category" in place of the
table.

**How this report is worked out.** Fixed text that explains the report:

- Fits: **direct** means an offering serves the category and offers every capability the resource uses,
  **partial** means it lacks some, and **none** means no offering serves the category.
- How the best offering is chosen, and what a tie means.
- What a required change and a blocker are, and what "affected by a blocker on" means (ADR 0028).
- How workloads are grouped.
- How an estimate is worked out: plan matching, `price × count × hours / 730`, billing caps, included
  storage, fixed fees, rounding to the cent half away from zero, list prices on the retrieval date
  excluding VAT and discounts, and that it is not a quote. With `--no-estimate`, one sentence says
  estimates were off.
- How values are shown: Courier, and the escapes above.

shift2eu has no readiness score today (PRD #2 left it open; PRDs #89, #190 and #297 keep it out of scope). This
section says so in one sentence, and explains the fits instead (*Decided, as recommended*, 6).

**About this report.** What a reader needs to check what produced the report:

- the shift2eu version line, exactly what `shift2eu version` prints for the build that wrote the PDF
  (`internal/version.String()`: "shift2eu <version> (commit <commit>, built <date>)"). In a release,
  `<version>` is GoReleaser's `{{ .Version }}`, the tag without its leading `v` (`0.15.0`). The site's
  pages show the tag itself (`v0.15.0`), which `ci/sitebuild` writes from its `-version` flag. Both come
  from the same tag; the PDF shows the version line, and the site's page footer shows the tag;
- every catalog snapshot used: the target's display name, the snapshot's source URL (`sourceURL`), its
  retrieval date as a UTC calendar date, and its default region for estimates;
- the options the report reflects: the targets, the region per target (chosen with `--region`, or the
  default for estimates), and whether estimates were on;
- the inventory file name and the source, as in the title block (*Decided, as recommended*, 12).

### Comparison report (several targets)

The comparison follows the Markdown comparison report's order:

1. The title block naming every target, with one catalog line and one region line per target.
2. **Comparison:**
   - a summary table with one column per target and one estimate row per target with estimates;
   - a resources table, one row per resource in the inventory's order, with the ID and category, then
     one cell per target that stacks the fit, the best offering and, with estimates on, the monthly
     estimate. Stacking keeps three targets readable on A4 portrait, where the Markdown table's column
     per value would not fit;
   - the blockers on every target.
3. **The contact banner** (milestone 1b), once, right after the *Comparison* section and before the
   first per-target section (*Look*).
4. One section per target, headed by its display name, holding that target's single-target sections
   from Summary to Resource details, one heading level lower, without the banner.
5. **How this report is worked out** and **About this report**, once, at the end, covering every target.

The comparison ranks no target and recommends none, as today.

### Variants

- `--no-estimate`: no plan or estimate column, no Cost estimate section, no breakdown in Resource
  details, and the sentence that estimates were off. Nothing else changes.
- `--region <target>=<region>`: the region line says "chosen with --region", as the Markdown report
  does. An offering not offered in that region is "not estimated: not offered in <region>" in the
  resources table and in its breakdown.

### Tables and page breaks

- A table that continues on a new page repeats its header row.
- A row stays on one page unless it is taller than a page. Only a hostile, very long value makes it
  that tall; such a row is then split at a line.
- A value wraps at the character boundaries of its escaped form. It is never cut short: the domain
  already caps a value's length (`text.Validate`).
- A wrap point never falls inside an escape (`\uXXXX`, `\UXXXXXXXX`, `\xXX` or `\\`): each escape is
  one unit that moves whole to the next line, so a reader never sees `\u04` at a line's end and takes it
  for a complete escape. Only an escape wider than its whole column is split.
- The worst case is real, not theoretical. A value `text.Validate` accepts, of 2048 bytes
  (`text.MaxLen`) made only of characters the escaper rewrites, grows to between 4,096 and 6,144 shown
  characters:
  - 1,024 two-byte runes such as U+0416 give 6,144 (its escape `\u0416` is 6 characters);
  - 512 four-byte runes give 5,120 (`\U0001f600` is 10);
  - 682 three-byte runes with one two-byte rune in the 2 bytes left give 4,098 (683 escapes of 6);
  - 2048 backslashes give 4,096: `text.Validate` accepts a backslash, the one ASCII character the
    escaper rewrites, as `\\`.

  In a narrow Courier table column that is more lines than a page holds, so the split-at-a-line path
  runs. The hostile golden holds a 2048-byte value of two-byte runes.
- The escaper's own table test can go further, since its in-test values skip `text.Validate`: 2048
  bytes of invalid UTF-8 (`0xff`) give 8,192 characters (`\xff` is 4 each). The unit test of the
  split-at-a-line path uses that 8,192-character case.
- A section heading never ends a page.
- From milestone 1b, boxes follow *Look*'s *Boxes across pages*: a box's header never ends a page, a box
  that spans pages is drawn again on each, and the banner, a tile row and a workload card move whole.

## How it fits the architecture

### Layers and dependency rules

- `pdfreport` is a reporter adapter. It implements `analysis.Reporter` and `analysis.ComparisonReporter`,
  as `markdown`, `jsonreport` and `htmlreport` do. It is held by the existing `reporters-stay-offline` rule,
  whose files glob already covers every package under `internal/adapter/report`. Its strict allow list
  grows only by the standard packages the writer needs, each with a reason in the rule's comment. The list
  gains no I/O package, no `math` and no third-party package.
- **The version line reaches the reporter by its constructor:** `pdfreport.New(versionLine string)`. An
  adapter may not import `internal/version`, and the reporter ports and `analysis.ReportMeta` do not
  change. The composition roots `cmd/shift2eu` and `cmd/shift2eu-web/internal/wire` pass
  `version.String()`. Both already import `internal/version`, and their strict allow lists
  (`composition-root`, `web-composition-root-wire`) gain `pdfreport$`. Both construct `pdfreport`; unlike
  `htmlreport`, the CLI must construct it too. So it is not added to `web-imported-only-by-the-web-root`.
- That rule's deny entries for `htmlreport` carry the desc "only cmd/shift2eu-web/internal/wire
  constructs the HTML reporter; the CLI writes Markdown and JSON (PRD #297)" (`.golangci.yml`). Milestone
  1 makes it untrue, so milestone 1's depguard changes reword it to "the CLI writes Markdown, JSON and
  PDF (PRD #297, PRD #584)". The depguard pin (`TestDepguardRulesArePinned`) pins deny entries by their
  `pkg` only and drops the desc, so the pin does not change for it.
- `internal/cli` gets the `pdf` format through `cli.Deps`'s format table. `cmd/shift2eu` adds it.
  `analysis.SelectSchemaVersion` already refuses `--schema-version` for any format but `json`.
- New depguard rules or entries are added to the depguard pin (`TestDepguardRulesArePinned`).
  `test/architecture` checks the new package for every release target and `js/wasm`, like its
  siblings.
- Milestone 1 updates CLAUDE.md: `pdfreport` in the adapters row, and `--format pdf` in the status
  paragraph. It also describes the reporter in `docs/architecture.md`. Milestone 2 updates CLAUDE.md's
  status paragraph for the site's PDF download.

### ADR 0018: PDF is not a versioned format

ADR 0018 versions the JSON report because scripts parse it, and a change can break them. A PDF is for
people, like the Markdown report. Nobody parses it as a contract. So the PDF has no `schemaVersion`.
A layout change is not a breaking change, and the golden files pin the layout byte for byte.
`--schema-version` with `--format pdf` is a usage error (exit 2), as it is with Markdown. What the PDF
does carry is the producing version: the version line in every page footer and in *About this report*.
The `%PDF-` header's version (*Decided, as recommended*, 4) is the file format's version, not the report's.

### ADR 0035: the WebAssembly module

- `internal/web`'s `Deps` drops `Markdown` and `JSON` and gains `PDF`. Its `Result` holds the PDF's
  bytes and the HTML fragment. Its `Options` drops `SchemaVersion`, which only the JSON download used.
- With `SchemaVersion` gone, no web format takes a schema version, so `web.Format`'s constructors drop
  their `schemaVersion int` parameter: `NewReporter func() analysis.Reporter` and
  `NewComparisonReporter func() analysis.ComparisonReporter`. `wire` wraps `pdfreport.New(version.String())`
  and `htmlreport` in them. `cli.Format` keeps its parameter, since the CLI's JSON format uses it.
- `wire` constructs `pdfreport` and `htmlreport` and no longer constructs `markdown` or `jsonreport`.
  A test pins that the `js/wasm` dependency tree of `cmd/shift2eu-web` holds neither, so the module stops
  carrying them.
- **The glue hands the PDF over as bytes, not as a string.** Today the glue converts the Markdown and
  JSON reports to JavaScript strings. That is safe for UTF-8 text, but not a byte-exact path for a file
  format. So the glue copies the PDF into a `Uint8Array` (`js.CopyBytesToJS`). `test/site`'s exact glue
  allow list of constant names (`TestGlueAllowListIsExact`) gains `Uint8Array`, which none of its banned
  names matches, and loses `schemaVersion`, since the glue no longer reads that option; nothing else
  changes in it. `assess`'s doc comment in `cmd/shift2eu-web/main.go`, which names the `markdown` and
  `json` fields, is updated to the `pdf` (bytes) and `html` fields. The PDF is
  also 7-bit ASCII (*Trust*), so the bytes would survive either path. The `Uint8Array` path keeps
  byte-exactness from depending on that, and keeps a later compression (*Decided, as recommended*, 5) from
  needing a glue change.
- **The module must carry the release's version.** Today `ci/sitebuild` builds the module with
  `-trimpath -buildvcs=false` and no `-X` flags, so inside the module `version.String()` is
  `shift2eu dev (commit none, built unknown)`. The pages show the release's version because the builder
  writes it into the HTML. For the PDF's version line, and its byte-identity with the released CLI's,
  milestone 2 stamps the module with the same three values `.goreleaser.yaml` gives the CLI:
  - `version`: GoReleaser's `{{ .Version }}`, the tag without its leading `v` (`0.15.0` for `v0.15.0`);
  - `commit`: the commit hash, as GoReleaser's binaries and the container image carry it (below);
  - `date`: `{{ .CommitDate }}`, the commit's committer date in UTC, RFC 3339. A depth-1 checkout gives
    exactly that string with the recipe the `image` job already uses:
    `TZ=UTC git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ`.

  The values are taken from the tag and its commit, never from the clock, so the bundle stays
  reproducible (ADR 0036). An untagged build keeps the development values `shift2eu version` shows.
- **The commit hash must be the same string in all three release binaries:** GoReleaser's archives, the
  container image and the module. Today GoReleaser stamps `{{ .ShortCommit }}`, git's abbreviated hash,
  whose length depends on which objects the clone holds. The `image` job copies it on purpose: it checks
  out the full history and runs `git rev-parse --short HEAD`, "as GoReleaser stamps them" (`release.yml`,
  ADR 0015's "Same stamp as the release binaries", pinned by `test/architecture`'s release workflow check,
  `releaseworkflow.go` and `imageStepTemplates` in its test). The `site` job checks out shallowly
  (`release.yml`: "ci/sitebuild reads no history, so a shallow checkout is enough"), so a short hash
  computed there can differ. And it runs before `goreleaser`, which consumes its bundle (it lists it in
  `checksums.txt` and uploads it), so it cannot take GoReleaser's outputs. How milestone 2 gets one
  string on all three is *Decided, as recommended*, 7: the maintainer chose (a), a fixed cut of the full
  hash, its first 12 hex digits, on all three (options (b) and (c), which keep `shift2eu version`'s
  format, are kept there as the alternatives weighed). The milestone 2 and release criteria below are written for (a).

### ADR 0036: the bundle, the CSP and the banned-API scan

The download keeps the one path ADR 0036 and PRD #297 allow (*Decided: one allowed download path*):

- the page script's one `download` function makes a `blob:` URL with `URL.createObjectURL`, sets it on
  an `<a download>`, clicks it, and revokes the previous URL;
- a `blob:` URL is same-document and sends nothing; the CSP (`connect-src 'none'`, `default-src 'none'`)
  is unchanged and needs nothing new.

What changes in the page script:

- one button, `download-pdf`, labelled "Download PDF", replaces `download-markdown` and `download-json`;
- one blob, `new Blob([result.pdf], { type: "application/pdf" })`;
- the constant file name `shift2eu-report.pdf` (*Decided, as recommended*, 1).

What it may not use, held by `test/site`'s banned-API scan, which does not change:

- no new browser API;
- no `window.open`, no navigation to the blob, no `<iframe>`, `<object>` or `<embed>` to preview the PDF;
- no `click()` or `dispatchEvent` outside `download`;
- no computed property name, no storage.

The scan's expectations that name the two buttons change to the one. The bundle's reproducibility test
and SRI values are unaffected beyond the changed files.

A saved PDF opened later in a viewer contacts nothing and runs nothing by itself. It holds no link
annotation, no URI or launch action, no JavaScript, no form, no embedded file and no reference to an
outside resource. A test pins this. Some viewers (Chromium's and Firefox's, Acrobat, macOS Preview) make
URL-shaped text clickable on their own: the catalog snapshots' source URLs, which *About this report*
prints, and any inventory value shaped like a URL, which the escaper keeps as ASCII. Only a click on such
text follows it. The privacy page says exactly this: the PDF holds no link, action, script or form, and
some viewers make URL-shaped text clickable, which only a click follows.

### ADR 0043: the size budget

- **Expected cost.** The writer adds a small amount of code: an object and cross-reference writer,
  string escaping, the three fonts' character width tables (integers, about 700 entries, used to wrap
  text), and the layout. Removing `markdown` and `jsonreport` from the module offsets part of it.
  `encoding/json` stays: the inventory reader needs it. Expected net growth: below 200 KiB decoded and
  below 64 KiB gzip-compressed. Today's measurements are 8.51 MiB of 11 MiB decoded (77%) and 2.39 MiB of
  3 MiB gzip-compressed (80%). No font is embedded, and no compression package is linked unless
  milestone 3 decides otherwise (*Decided, as recommended*, 5).
- **How milestone 2 measures it.** The existing budget tests (`TestWebModuleStaysWithinTheSizeBudget`, and
  the live check's 3 MiB and 11 MiB limits) must pass, unchanged. Milestone 2's PR records, before and
  after the switch:
  - the raw module size;
  - the module script file in `bin/site`;
  - its gzip size at Go's default level.

  It appends them to ADR 0043's table of measurements as an amendment. Gzip-compressed growth above
  128 KiB is raised with the maintainer before the PR is reported ready.

### Trust and privacy

- **Hostile text cannot become PDF syntax.**
  - Every value is written only inside a PDF literal string, in a text-showing operator of a content
    stream. It is never written as a name, a number, a dictionary key or value, a comment, a stream
    length or an object.
  - Inside the string, `(`, `)` and `\` are escaped as `\(`, `\)` and `\\`.
  - Every byte outside 0x20 to 0x7E is written as a three-digit octal escape (`\ddd`): the WinAnsi bytes
    of the characters the fonts show, and nothing else, since every other character has already become
    a visible escape. So a carriage return, a line feed or a NUL never appears raw.
  - A value never ends its string, starts an operator, or adds an object.
  - `/Title`, the one entry of `/Info` (*Decided, as recommended*, 8), is a constant, never a value.
- **The file is printable ASCII plus line feeds.** Every byte of the PDF is 0x0A or in 0x20 to 0x7E.
  There is no binary comment line and no compressed stream (*Decided, as recommended*, 5). So:
  - writing it to a terminal cannot send an escape sequence, and `--format pdf` writes to standard
    output or `--output` exactly as the other formats do;
  - the golden files diff as text;
  - each cross-reference entry ends with a space and a line feed (`SP LF`), the one two-byte end of line
    ISO 32000-1 (7.5.4) allows within this byte set, so every entry is exactly 20 bytes. A bare line
    feed would give 19-byte entries, which some viewers reject.
- **No active content.** No `/Annots`, `/URI`, `/Launch`, `/JavaScript`, `/JS`, `/OpenAction`, `/AA`,
  `/AcroForm`, `/EmbeddedFile`, `/RichMedia` or `/XFA` name appears in the file's objects. Every name the
  writer emits is a constant of its own and holds no `#`, so no name can spell another with `#xx`
  (`/J#53` is `/JS`). The catalog snapshot's source URL is shown as text, never as a link; a viewer may
  still make such text clickable, and only a click follows it (*ADR 0036* above).
- **Nothing leaves the browser.** The PDF is built in the module from the inventory already in memory,
  and offered through the one download path. The privacy page's statement that no inventory, report or
  choice is sent anywhere holds unchanged. Milestone 2 updates its wording from "report" downloads in
  general to the PDF, says the PDF holds no link, action, script or form and that some viewers make
  URL-shaped text clickable, which only a click follows, and says the PDF names the inventory's file name
  (*Decided, as recommended*, 12), since it is a document meant to be passed on.
- **Reproducible.** The bytes depend only on the assessment, the report metadata and the version line:
  - never the clock, the environment or map iteration;
  - no creation or modification date, no `/ID`, no `/Producer`;
  - integer layout arithmetic only (positions and widths in thousandths of a point), so no
    floating-point result differs between amd64, arm64 and `js/wasm` (arm64 may fuse a multiply and an
    add, so the same float expression can round differently there).

  The same inventory, options and build therefore give the same bytes on every release target.

## Acceptance criteria

Test levels:

- *unit*: `make test`, the package's own tests;
- *CLI*: `internal/cli`'s golden tests under `make test`;
- *native parity*: `cmd/shift2eu-web/internal/wire`'s tests under `make test`;
- *static*: `test/site` or `test/architecture` under `make test`;
- *lint*: `make lint`;
- *e2e*: `make test-e2e`;
- *browser*: `make test-browser` and its CI job;
- *release*: `release.yml`;
- *review*.

### The PDF reporter (milestone 1)

- [x] *Unit:* given an assessment and report metadata, `pdfreport` writes a PDF that matches its golden
  file byte for byte. Its goldens include:
  - a single-target report with estimates and one without;
  - one with a region chosen;
  - one with workloads;
  - a comparison of two targets and one of three;
  - an empty inventory;
  - a hostile report and a hostile comparison;
  - a report long enough to break pages inside a table.

  They are updated with `-update`.
- [x] *Unit:* the PDF starts with `%PDF-` and the chosen version, and ends with `%%EOF` and a line feed.
  Every object listed in the cross-reference table starts at the byte offset the table gives, and every
  cross-reference entry is exactly 20 bytes, ending in a space and a line feed (`SP LF`). The
  trailer's `/Size` and `/Root` are right. Every stream's `/Length` is its exact byte count. A test-only
  reader in `pdfreport`'s test files parses the writer's subset and checks all of this for every golden.
- [x] *Unit:* the text the test-only reader extracts from each page holds, in order:
  - the sections of *The PDF* for that variant;
  - every resource's ID, fit and best offering;
  - for each resource, every candidate offering with its fit and missing capabilities, its blockers, its
    required changes and, with estimates on, its breakdown;
  - *How this report is worked out*;
  - *About this report*, with the version line passed to `New` and every snapshot's source URL and
    retrieval date.
- [x] *Unit:* with estimates off, the extracted text holds no plan, no amount, no Cost estimate section and
  no breakdown. With a region chosen, it says "chosen with --region".
- [x] *Unit:* every page carries the header and "Page N of M" with the right N and M, and the version
  line. A table continued on a new page repeats its header row.
- [x] *Unit:* every byte of every golden is 0x0A or in 0x20 to 0x7E.
- [x] *Unit:* the test-only reader's parsed names, after `#xx` decoding and outside strings, hold none of
  `/Annots`, `/URI`, `/Launch`, `/JavaScript`, `/JS`, `/OpenAction`, `/AA`, `/AcroForm`, `/EmbeddedFile`,
  `/RichMedia`, `/XFA`, `/FontFile`, `/FontFile2`, `/FontFile3`, `/CreationDate`, `/ModDate`, `/ID` or
  `/Producer`, in any golden. No name the writer emits contains `#`. A value that merely spells one of
  these names inside a string is not a finding. Every font object is a standard Type 1 font named
  `Helvetica`, `Helvetica-Bold` or `Courier`, with `/Encoding /WinAnsiEncoding`.
- [x] *Unit:* the escaper's table test covers each character class, with the exact escaped form of each:
  - printable ASCII, `"` and `\` included (`"` stays, `\` is `\\`);
  - the 27 WinAnsi characters in 0x80 to 0x9F and the Latin-1 Supplement, kept as they are;
  - U+00A0 and U+00AD, escaped as the ASCII text `\u00a0` and `\u00ad`;
  - C0 and C1 controls, U+2028, U+2029 and bidi overrides, escaped (defence in depth: `text.Validate`
    already refuses them at read);
  - a printable non-Latin letter, U+0416, escaped as the ASCII text `\u0416` and never replaced or
    dropped;
  - a character above U+FFFF (`\U0001f600`) and invalid UTF-8 (`\xff`);
  - a value of `text.MaxLen` bytes, escaped in full, never truncated, and 2048 bytes of `0xff`
    (8,192 characters, *Tables and page breaks*).

  The test's expected values are written as ASCII Go string literals, so no editor or tool can turn an
  expected escape into the character it names.
- [x] *Unit:* a value whose escape straddles the wrap width (for example `Ж` starting a few
  characters before a line's end) wraps before the escape, and every wrapped line of every golden starts
  and ends on an escape boundary. Only an escape wider than its column, built in-test, is split.
- [x] *Unit:* the hostile report and hostile comparison goldens take in-test hostile values, as the other
  reporters' hostile goldens do, in every field the PDF shows. The values carry PDF metacharacters and
  syntax: unbalanced `(` and `)`, `\`, `) Tj ET`, `endstream`, `%%EOF`, `/JavaScript`, `<< >>`, `obj`,
  plus a 2048-byte value of two-byte runes (*Tables and page breaks*) and the escaper's character classes.
  For each, the structural invariants below hold, and `(`, `)` and `\` inside a value are escaped in the
  content stream.
- [x] *Unit:* the structural invariants, checked for every golden and in the fuzz test:
  - the test-only reader parses the output;
  - the text extracted for a value, joined across its wrapped lines, equals the value's escaped form.
    The writer draws each wrapped line of a value as its own `Tj` at the x of the value's column or
    line start, so the test finds a value's lines by that x on consecutive lines, across a page break;
    the hostile and fuzz tests check this in reports where only the field under test holds the value;
  - every token of every content stream is a literal string, or a pair of an operator and operand shape
    from the writer's fixed table, which the test pins (`BT`, `ET`, `Tf`, `Td` or `Tm`, `Tj`, `re`, `f`,
    `S`, `w`, `q`, `Q` and whatever else the writer uses, each with the operands it takes). Operands are
    numbers, except `Tf`'s first, one of the writer's constant font resource names (`/F1` to `/F3`).
    `gs`, `Do` and inline images are not in the table: the writer uses no graphics state dictionary and
    no XObject;
  - no object has a type outside the writer's set (catalog, pages, page, font, content stream, info);
  - every byte is 0x0A or in 0x20 to 0x7E.
- [x] *Unit:* a native Go fuzz test (standard `testing`, seeds run under `make test`) writes reports in
  which one field at a time holds the fuzzed string, and fails when a structural invariant does not hold.
- [x] *Unit:* the same input written twice gives the same bytes. A test fails if a non-test file of
  `pdfreport` imports `math`, declares a floating-point type, holds a floating-point literal, calls
  `strconv.FormatFloat` or `strconv.ParseFloat`, or uses a `%f`, `%F`, `%g`, `%G`, `%e` or `%E` verb.
- [x] *Unit:* a comparison with no target, or whose targets assess different resources, is an error, and
  nothing is written, as with the other comparison reporters.
- [x] *CLI:* `shift2eu assess --inventory <file> --target <t> --format pdf` exits 0 and writes the PDF to
  standard output, matching a CLI golden file. `--output <path>` writes the same bytes to the file.
  `--target all` writes the comparison.
- [x] *CLI:* `--format pdf --schema-version 3` is a usage error, exit 2, naming `--schema-version` and
  `--format pdf`, and writes nothing. An unknown `--format` lists `pdf` among the supported formats.
- [x] *CLI:* the PDF's version line is the line `shift2eu version` prints, in the same build.
- [x] *e2e:* the built binary writes a PDF for each example inventory (`examples/aws`, `examples/azure`,
  `examples/gcp`) against each target and `--target all`, with and without `--no-estimate`, and exits 0.
  Each file starts with `%PDF-`, and two runs give the same bytes.
- [x] *CLI and e2e:* `--format pdf` runs on every fixture of
  `.github/security-audit/testdata/hostile-inventories/`. The test derives the accepted set from the
  Markdown run, so a fixture added later is covered without a change: every fixture the Markdown run
  accepts gives a PDF that exits 0 and passes the structural invariants, and every fixture it refuses is
  refused with `--format pdf` exactly as with Markdown (the same message and exit code, and nothing on
  standard output). *For information, at the time of writing:* of the eighteen fixtures, the reader
  accepts one, `markup-injection.json`, and refuses the rest (the `schema-*` files, `number-overflow`,
  `numbers`, and `path-like-names`, `terminal-escapes`, `unicode-tricks`, `relations-graph` and
  `unknown-values`, for their controls, bidi characters, duplicate ID or unknown category). That split
  is not the criterion; the derived set is.
- [x] *Lint and static:* `pdfreport` imports only the allow list of `reporters-stay-offline`.
  `cmd/shift2eu` and `wire` may import it, and the depguard pin holds the changed rules.
  `test/architecture` checks it for every release target and `js/wasm`.
- [x] *Review:* CLAUDE.md's adapters row names `pdfreport`, its status paragraph names `--format pdf`, and
  `docs/architecture.md` and the CLI guide (`docs/guide/assess.md`) describe the format. The Markdown,
  JSON and HTML reporters' goldens do not change.

### The PDF's styling (milestone 1b)

Every milestone 1 criterion above keeps holding on the restyled goldens. Their tests change only where
they name the operator table (below) or must expect the banner's text where it sits.

- [x] *Unit:* every golden of milestone 1 is regenerated with `-update` and matches byte for byte. No
  golden is added or dropped: each carries the styling, and each carries the banner once.
- [x] *Unit:* the test-only reader's operator table is exactly ADR 0049's amended set, each with its
  operand shape, and nothing else: `BT`, `ET`, `q`, `Q`, `f`, `S` and `h` with no operand; `Tf` with a
  font resource name and an integer; `Tm` with six integers; `Tj` with a literal string; `re` with four
  integers; `w` with one decimal; `rg` and `RG` with three decimals; `m` and `l` with two decimals. `G`,
  milestone 1's grey operator, is gone. A test pins the table's keys and shapes.
- [x] *Unit:* a content stream built in-test with an operator outside the table fails the reader, for
  each of `G`, `g`, `k`, `K`, `B`, `b`, `c`, `v`, `y`, `F`, `f*`, `W`, `n`, `J`, `j`, `d`, `cs`, `scn`, `sh`,
  `gs`, `Do`, `BI`, `TJ`, `'` and `"`; so does a listed operator with an operand of the wrong shape (an
  integer for `rg`, a decimal for `re`, a missing or an extra operand).
- [x] *Unit:* every number in every content stream has its written form: an integer is ASCII digits
  only; a decimal is ASCII digits, `.`, and exactly three digits; neither has a sign or an exponent.
  Every `rg` and `RG` component is between `0.000` and `1.000`, every `m` and `l` coordinate within the
  page (0 to 595 and 0 to 842), and every `rg`/`RG` triple of every golden is a row of *Look*'s palette,
  written exactly as the table gives it.
- [x] *Unit:* the colour conversion, from a 0 to 255 byte, gives `(byte × 1000 + 127) / 255` thousandths
  written with three places: `0` is `0.000`, `11` is `0.043`, `255` is `1.000`, and every palette entry's
  hex gives the table's value. The no-floating-point test of milestone 1 passes unchanged.
- [x] *Unit:* badge words: in every golden, every rectangle filled in a fit's `-bg` colour is a badge,
  holding one icon path and one text run, both in that fit's `-ink` colour, the text being the fit's word
  ("Direct", "Partial", "None"), alone or after a count. Every fit the page shows as a word in a Fit cell,
  a stacked comparison cell, a candidate table, a tile or a workload card is in such a badge. No `-bg`
  fill holds another fit's word.
- [x] *Unit:* icons: each badge's icon is its fit's shape: three points stroked for direct; a 16-sided
  closed polygon stroked plus a filled half for partial; two separate stroked lines for none. The
  blockers box's heading carries the filled triangle. No icon is a font glyph.
- [x] *Unit:* the banner: every golden holds, once, the four runs "Planning a migration?", "Questions
  about your results, or how to move forward?", the text paragraph (its wrapped lines joined, the ASCII
  apostrophe included) and "info@shift2eu.com", in that order, all on one page and inside one `--mast`
  rectangle, the address inside a `--gold` one. In a single-target golden they follow the last section
  from Summary to Cost estimate that the golden shows and precede *Resource details*; in a comparison
  golden they follow the *Comparison* section and precede the first per-target section.
- [x] *Unit:* boxes across pages, in the long golden and the hostile ones and in a layout test that
  builds a box near a page's end: a box's heading or header row is never the last text on a page; every
  text run of a box that spans pages lies inside a fill or outline of that box drawn on its own page; the
  banner, a tile row and a workload card that fits a page's body never span two pages.
- [x] *Unit:* the structural invariants of milestone 1 hold for every golden and in the fuzz test, with
  the amended operator table, and the banned names gain `/Link`, `/XObject`, `/ExtGState`, `/Pattern`,
  `/Shading` and `/ColorSpace`: each page's `/Resources` holds only `/Font`. No `/Annots`, `/URI` or
  other milestone 1 banned name appears, the banner's address included. The object types stay
  milestone 1's set.
- [x] *Review:* the text extracted from each golden still holds every section, value and detail milestone
  1's text tests check, in the same order. The only changes to the text are: the banner's text; the
  summary table's "Fit" and "Resources" header replaced by the tiles, which show the same counts, with
  their labels "Total" and "Estimate" and their captions "of N resources", "resources in total" and "per
  month, estimated"; the fit words capitalised in their badges (Direct, Partial, None); and the workload
  line's badges ("Fit: " then "1 Direct" and so on). Milestone 1's text tests, unchanged, and the
  banner's test hold the first sentence; they check that text is shown, not that no other text is.
  The reviewer held the second once: the text the test-only reader extracts from each of the 10
  goldens, `origin/main` against 29ec99a1, holds every milestone 1 line in order, and differs only by
  the changes above (the estimate tile's figure equal to the "Monthly estimate:" total) and by one wrap
  point in `report-estimates` and `report-region`, where the workload card's narrower text column
  breaks before "VAT".
- [ ] *Review:* the PR states every golden PDF's size before and after, and the WebAssembly module's raw
  size and gzip size before and after. The module does not construct `pdfreport` until milestone 2, so
  both are expected unchanged; the budget tests are unchanged and pass.
- [ ] *Review:* the PR records a look at the bundled examples' PDFs, single and comparison, in the four
  viewers of *Decided, as recommended*, 10, and one printed or previewed in greyscale, in which every fit
  still reads by its word and icon.
- [x] *Review:* ADR 0049 is amended (*Amended 2026-11-28 (PRD #584 milestone 1b)*), and `pdfreport`'s
  `doc.go`, `docs/architecture.md` and the CLI guide (`docs/guide/assess.md`) describe the styling and the
  banner. The scope rows hold: no embedded font, no third-party dependency, no tagged PDF, no
  compression. The Markdown, JSON and HTML reporters' goldens do not change.
  The reviewer held it in both review rounds: the ADR amendment, `doc.go`, `docs/architecture.md` and
  `docs/guide/assess.md` match the code, and nothing changed under `markdown/`, `jsonreport/`,
  `htmlreport/` or in `go.mod`.

### The site's PDF download (milestone 2)

- [ ] *Native parity:* given the same inventory and options, the web adapter's PDF bytes equal what
  `internal/cli` writes for `--format pdf`. This is checked over every example inventory and every
  hostile fixture the reader accepts, for one target, several and `all`, with and without a region and
  estimates. Any difference fails the test. This replaces the Markdown and JSON parity tests. Both sides
  run in the one `go test` binary (`wire` runs `cli.Run` in process), so they read the same
  `internal/version` values, unstamped, and the version lines match with no stamping.
- [ ] *Native parity:* the option-parity table no longer has a schema-version row for the web adapter.
  Every other row is unchanged.
- [ ] *Static:* `go list -deps` of `./cmd/shift2eu-web` with `GOOS=js GOARCH=wasm` holds `pdfreport` and
  `htmlreport`, and neither `markdown` nor `jsonreport`.
- [ ] *Static:* the assessment page has exactly one download button, `download-pdf`, with the visible
  label "Download PDF", disabled until an assessment has run. It has no `download-markdown` or
  `download-json` element.
- [ ] *Static:* `test/site`'s banned-API scan passes unchanged in its banned names. The download function
  stays the one place `URL.createObjectURL`, `click()` and the `href` assignment appear. The page script
  creates exactly one blob type, `application/pdf`, and the file name `shift2eu-report.pdf`. The glue's
  allow list of constant names adds `Uint8Array`, removes `schemaVersion`, and changes nothing else.
- [ ] *Static:* the CSP of every page is unchanged.
- [ ] *Browser:* the smoke test runs an example with one target and estimates on and clicks
  "Download PDF". It reads the blob's bytes and checks that they are byte-identical to what the CLI binary
  built from the same commit writes for the same inventory and options. Its file name is
  `shift2eu-report.pdf` and its type `application/pdf`. It does the same for a comparison of all three
  targets. Both sides carry the same version line because neither is stamped: the test builds its
  reference CLI as it does today (`buildCLI` in `test/browser`, a plain `go build ./cmd/shift2eu` with no
  `-ldflags`), and the bundle `make test-browser` builds is untagged, so `ci/sitebuild` passes no `-X`
  either. Both print `shift2eu dev (commit none, built unknown)`. The test does not stamp its CLI with
  the Makefile's `VERSION`, `COMMIT` and `DATE` instead, and the module with the same values, because
  `make build`'s `DATE` is the clock, which would make the bundle unreproducible (ADR 0036), and
  because an untagged build keeps the development values (*ADR 0035* above); this needs no new stamping
  path outside a release. When the bundle is a release's (`TEST_BROWSER_BUNDLE`, its `manifest.json`
  naming a tag), the module is stamped and the test's own unstamped CLI is no reference: that run
  compares against the released CLI, as the *Release* criterion below says. The bytes are read as today's download test reads them, with no page API: the test's own
  injected script (`recordDownloads` in `test/browser`) keeps the blob the page's `download` function
  passes to `URL.createObjectURL`, and the test reads it with `blob.arrayBuffer()` and hands the bytes
  back as an array of byte values, in place of today's `blob.text()`. The page script does not change for
  the test, and the browser still refuses to write the file.
- [ ] *Browser:* with the network disabled, the PDF download still works. The page makes no request
  while building or offering it, and the existing network test passes.
- [ ] *Browser:* the download works with the keyboard alone, and the accessibility checks of the main flow
  pass with the one button.
- [ ] *Release:* the release builds the module, GoReleaser's binaries and the container image's binary
  with the same three values: the release's version (the tag without `v`), one commit string (in the
  form *Decided, as recommended*, 7's option (a) gives: the first 12 hex digits of the full hash) and its commit date. The smoke test on the attested
  bundle compares its PDF download with the output of the released `linux/amd64` CLI for one example:
  the same bytes, version line included.
- [ ] *Release:* on every release, `release.yml`'s `verify` job, which already runs the signed image's
  `shift2eu version`, also runs the `linux/amd64` archive's binary from the `goreleaser` job's `archives`
  artifact, and fails unless the two lines are identical, version, commit and date included. Downloading
  that artifact needs no new permission: `actions/download-artifact` reads an artifact of the same run
  with the runner's runtime token, not `GITHUB_TOKEN`, as the `sign` job already downloads the same-run
  `sbom` artifact with neither `contents` nor `actions` permission. `verify`'s permissions stay exactly
  `packages: read` and `attestations: read`, and `test/architecture`'s exact pin of them
  (`releaseworkflow.go`) stays unchanged. Only if the release run shows the download refused does
  milestone 2's PR grant the one permission the failure names, with a comment saying why, and amend that
  pin to match. The archive's binary runs in a step of its own
  with no token in its `env`, after the archive is checked against the artifact's `checksums.txt`
  (`sha256sum -c --ignore-missing`), as the `goreleaser` job checks the site bundle. This would hold for every
  option of *Decided, as recommended*, 7, and its step is pinned in `test/architecture` with the others.
- [ ] *Static:* `test/architecture`'s pins of every step and template that stamps the version, in
  whichever jobs the chosen option uses (the `site` and `image` jobs' steps, `.goreleaser.yaml`'s commit
  template, and a new job's under option (b)), match the chosen option.
- [ ] *Review:* the scheduled audit's row 13 (ADR 0040's table,
  `.github/workflows/scheduled-security-audit.yml`, the CLI on hostile inventory files) also runs
  `--format pdf` on every file of its corpus and the files it generates. Its step text and corpus pins
  in `test/architecture` (`auditworkflow.go` and its tests, `auditcorpus_test.go`) and ADR 0040's row 13
  are amended to match. The `security-audit-run` skill reads those runs as it reads the Markdown ones.
  Row 13 runs the latest release, which may not hold milestone 1 yet. The skill reads a refusal of
  `--format pdf` as "PDF not covered by this release" only when both hold:
  - the run is a usage error (exit 2) with the CLI's message `unknown format "pdf"`;
  - the release under audit predates the first release that ships `--format pdf`. The skill tells this
    from the release's version, as the image's `shift2eu version` prints it, against that first
    release's version, which the skill names once that release exists; until then every release
    predates it. If milestone 1 is already released when milestone 2's PR is written, that PR names the
    version in the skill; if not, the `release-tag` review of the first release that ships `--format pdf`
    files an issue to add it.

  Any other refusal of `--format pdf` is a finding.
- [ ] *Review:* milestone 2's PR states row 13's step time before and after the PDF runs, from a manual
  dispatch of the scheduled audit or its last scheduled run. A third format adds half again as many
  container runs to a loop that starts no run after 720 seconds and only logs the skipped ones to
  `limits.txt`. If that limit no longer leaves headroom, the PR raises it, with its `test/architecture`
  pin and ADR 0040's row 13, or runs the PDF on a subset of the corpus the PR names.
- [ ] *Static:* the module size budget test passes. Milestone 2's PR states the raw module size, the
  module script file and its gzip size before and after the change, and ADR 0043 records them.
- [ ] *Static:* the privacy page says the report download is a PDF made in the browser, that nothing is
  sent, that the PDF holds no link, action, script or form, that some viewers make URL-shaped text
  clickable, which only a click follows, and that the PDF names the inventory's file name.
  The verification page and the inventory guide page mention no Markdown or JSON download. The README
  and the guides that describe the site say the same.
- [ ] *Review:* PRD #297's amended criteria and decisions (*Amendments to PRD #297*) are edited in place
  with a dated amendment note. ADRs 0018, 0035, 0036 and 0043, and the doc.go files of `jsonreport`,
  `htmlreport` and `internal/web`, are amended where they name the site's Markdown or JSON downloads.
  ADR 0035 records the module's version stamp, ADR 0040 row 13's PDF runs, and, under option (a) or (b)
  of *Decided, as recommended*, 7 (the maintainer chose (a)), ADR 0015 the image's new commit stamp.

## Amendments to PRD #297

This PRD **lists** the amendments; milestone 2's PR **edits PRD #297's text in place**, each change marked
"*Amended <date of the milestone 2 PR> (PRD #584, decided 2026-11-25):*" as PRD #297 already marks its hosting amendments. The reason: until
milestone 2 ships, the site does offer the Markdown and JSON downloads, and PRD #297 must keep describing
what runs. Item 1 of PRD #297's milestone list (milestone 1, done) keeps its text as history. The
criteria that milestone ticked and this list amends (3 to 5) are edited in place like the rest, each with
the dated note, and stay ticked.

1. **Outcome**, first paragraph: "They can download the report as Markdown and as the versioned JSON
   report, byte for byte what `shift2eu assess` prints for the same inventory and options" becomes "They
   can download the report as a PDF, byte for byte what `shift2eu assess --format pdf` writes for the same
   inventory and options (PRD #584)".
2. **Scope**, the *Output* row:
   - in scope, "Downloads of the Markdown report and the JSON report (schema version 2, ADR 0018,
     ADR 0030)" becomes "A download of the PDF report (PRD #584)";
   - out of scope, "PDF." is removed. "A readiness score." and "Changes to the Markdown or JSON report."
     stay.
3. **Acceptance criteria, *Assessment in the browser*, the fourth criterion** (the download parity): "the
   Markdown and JSON downloads are byte-identical to the output of `shift2eu assess --format markdown`
   and `--format json`" becomes "the PDF download is byte-identical to the output of `shift2eu assess
   --format pdf`". The parity coverage (every example and the hostile fixtures; one, several and `all`
   targets; with and without a region and estimates; one example in the browser) stays.
4. **Acceptance criteria, *Architecture*, the second criterion** (`internal/web`): "returns the report
   bytes for each download and the HTML fragment" becomes "returns the PDF report's bytes and the HTML
   fragment".
5. **Acceptance criteria, *Architecture*, the third criterion** (`wire`):
   - "the Markdown, JSON and HTML reporters" becomes "the PDF and HTML reporters";
   - the download parity test compares the PDF.
6. **Acceptance criteria, *Architecture*, the *Review* criterion**: "the Markdown and JSON reporters are
   reused unchanged; the CLI's and the HTTP API's output do not change" becomes "the Markdown and JSON
   reporters are reused unchanged, and the CLI's Markdown and JSON output do not change. The CLI gains
   `--format pdf` (PRD #584), and the HTTP API is retired (ADR 0037)".
7. **Decisions, *Decided by the maintainer (2026-11-19)*, "the first version's scope"**: "Markdown and
   JSON downloads identical to the CLI's" becomes "a PDF download identical to the CLI's (decided by the
   maintainer, 2026-11-25, PRD #584)".
8. **Decisions, *Decided in this PRD*, the `htmlreport` decision**: "the downloads are the existing
   reporters' bytes" becomes "the download is the PDF reporter's bytes, which the CLI writes too
   (PRD #584)".

These hold unchanged and are not amended:

- *Trust, Layer 1*, the download function: one function, from the report bytes;
- *Decided by the orchestrator in review*, "one allowed download path";
- *Accessibility*, the keyboard flow, download included;
- *Page content*, "as the Markdown report words it", a wording reference;
- *Decided: Go's standard toolchain*, "keeps the downloads identical".

## Milestones

Each milestone ships as one PR, in this order, and each gets the security audit (`security-audit` skill).
Milestones 1 and 2 touch the handling of hostile text and the site's trust model; milestone 1b changes
the content streams' operator set.

- [x] **1. The PDF writer, `pdfreport`, `--format pdf` and the ADR.**
   - The reporter, standard library only, with:
     - the deterministic writer: objects, cross-reference table (20-byte entries ending in `SP LF`),
       trailer, content streams, literal string escaping;
     - the three core fonts with WinAnsiEncoding and their width tables;
     - text wrapping, tables, repeated header rows and page breaks;
     - the sections of *The PDF* for the single-target and the comparison report, and the
       `--no-estimate` and `--region` variants.
   - Its golden files, hostile ones included, the escaper and its table test, the test-only reader, the
     structural invariants, and the fuzz test.
   - `pdfreport.New(versionLine)`, wired in `cmd/shift2eu`, and `--format pdf` in `internal/cli`, with
     CLI goldens and e2e tests.
   - The depguard allow list changes and their pin, the reworded `htmlreport` deny desc of
     `web-imported-only-by-the-web-root` ("the CLI writes Markdown, JSON and PDF"), and
     `test/architecture`. The permission for `cmd/shift2eu-web/internal/wire` to import `pdfreport`
     (`pdfreport$` in `web-composition-root-wire`) is granted early, in milestone 1, so the milestone-1
     criterion holds as written, and milestone 2 constructs `pdfreport` in `wire`.
   - CLAUDE.md's adapters row and status paragraph, `docs/architecture.md`, and the CLI guide.
   - An ADR that records:
     - PDF as the report's people-facing format, not versioned (ADR 0018);
     - the standard-library writer and why there is no third-party library;
     - A4, the three core fonts, WinAnsiEncoding and the escape rule for every other character;
     - why no font is embedded (the size budget);
     - no timestamp, no varying metadata, no `/ID`;
     - integer layout arithmetic;
     - no links, JavaScript, forms or embedded files;
     - the string escaping, and the printable-ASCII file;
     - the version line passed by the constructor;
     - the answers to *Decided, as recommended*, 2 to 4, 6, 8 and 10 to 12, and the interim answers to 5
       (compression) and 9 (tagged PDF): not in milestones 1 and 2. Milestone 3 amends the ADR with the
       final decisions on both.
   - The site does not change in this milestone.

- [ ] **1b. The PDF's visual styling and the contact banner.** Depends on 1. Added by the maintainer on
  2026-11-28, between 1 and 2, so the site never offers the plain PDF (*Decided by the maintainer
  (2026-11-28)*). The milestones keep their numbers.
  - The drawing operators of ADR 0049's amended set and the colours of *Look*'s palette, written as
    three-place decimals from integer arithmetic.
  - The vector icons: check mark, half-filled disc, cross, and the blockers' warning triangle.
  - Fit badges that always carry their word beside colour and icon.
  - The summary tiles with the big fit counts, the total and the estimate.
  - The required-changes box, the blockers box with the warning rule, and the workload cards.
  - The title band, section headings, table header shading and row rules, and the *Resource details*
    ID bands.
  - The contact banner and its placement (*Look*).
  - Page breaks that never split a box's header from its first row or line, redraw a box that spans
    pages on each page, and move the banner, a tile row and a workload card whole.
  - The test-only reader's operator allow list and the structural invariants, with a test that an
    unlisted operator fails; the badge-word test; the banner and box tests.
  - The regenerated goldens. The PR states every golden PDF's size before and after, and the
    WebAssembly module's raw and gzip size before and after; the budget tests are unchanged.
  - ADR 0049 amended; `pdfreport`'s `doc.go`, `docs/architecture.md` and the CLI guide.
  - The site does not change in this milestone.

- [ ] **2. The site's switch to the PDF download.** Depends on 1b.
   - `internal/web`'s `Deps`, `Options` and `Result`, and `wire` constructing `pdfreport` and
     `htmlreport` only.
   - The glue's `Uint8Array` hand-over.
   - The module stamped with the release's version, commit and date by `ci/sitebuild`, from
     `release.yml`'s `site` job, as *Decided, as recommended*, 7's option (a) says, with the same commit
     string in GoReleaser's binaries and the container image. Under (a), as decided: `.goreleaser.yaml`, the
     `image` job's stamp step and its `test/architecture` pin, and ADR 0015's "Same stamp" bullet.
   - The scheduled audit's row 13 runs `--format pdf` over its hostile corpus too, with its
     `test/architecture` pins, ADR 0040's row 13, the `security-audit-run` skill's reading of a
     release without the format, and row 13's step time before and after.
   - The `verify` job's comparison of the image's and the archive's `shift2eu version` lines.
   - The page script's single download path, and the one button.
   - The native parity, option-parity, dependency and static tests; the browser smoke test's PDF
     download and its release run.
   - The size budget measurements, before and after.
   - The privacy, verification and inventory guide pages, the README and the guides.
   - PRD #297 amended in place (*Amendments to PRD #297*).
   - ADRs 0015 (option (a) of *Decided, as recommended*, 7), 0018, 0035, 0036, 0040 and 0043, and the
     doc.go files amended. ADR 0036's amendment records the download's file name (*Decided, as
     recommended*, 1).
   - Ships on PRD #297's site track. It does not depend on PRD #297 milestone 4, and the first deploy does
     not wait for it.
- [ ] **3. Polish round on layout and the detail sections.** Planned on PRD #297's site track **after the
   first deploy** (PRD #297 milestone 4), so it does not delay it. Depends on 2 and on PRD #297
   milestone 4.
   - The maintainer reviews the PDFs of the bundled examples and of a large inventory, and their
     feedback on layout, typography, the detail sections' order and wording, and the comparison's
     stacked cells is applied.
   - The goldens are updated, and the parity holds.
   - The questions this PRD defers are decided and done or filed: compression (5), tagged PDF (9).
     Milestone 3's PR amends milestone 1's ADR with both final decisions, in place of their interim
     answers.
   - This PRD moves to `prds/done/` (`Closes #584`).

## Decisions

### Decided by the maintainer (2026-11-25)

1. **A real PDF built in the page, not the browser's print dialog.**
   - A new reporter, `internal/adapter/report/pdfreport`, sits beside the Markdown, JSON and HTML ones,
     and the CLI gets `shift2eu assess --format pdf`.
   - So the site keeps offering what the CLI writes. The download parity test and PRD #297's "check what
     you run" story hold: the PDF download is byte-identical to the CLI's for the same inventory and
     options.
2. **Standard library only.**
   - A small, deterministic PDF writer (text, tables, page breaks, the PDF core fonts), tested with
     golden files.
   - No timestamp and no varying metadata in the file, so it is reproducible.
   - No links, JavaScript, forms or embedded files.
   - Hostile inventory text escaped, and the audit's hostile-inventory fixtures cover it: the ones the
     reader accepts in the CLI and e2e tests, PDF metacharacters in the unit goldens, and the whole
     corpus in the scheduled audit's row 13 from milestone 2.
   - One ADR (milestone 1) records the format, the writer and these choices.
3. **Non-Latin characters are shown as escapes, not with an embedded font,** so the module's size budget
   (ADR 0043) stays safe. *Characters* names the set the core fonts show (WinAnsiEncoding) and how every
   other character is written.
4. **The site offers only the PDF.**
   - One "Download PDF" button replaces the Markdown and JSON buttons.
   - The site's JSON download goes away; the CLI keeps every format.
   - Every PRD #297 decision and criterion that promises Markdown or JSON downloads is amended
     (*Amendments to PRD #297*).
5. **The PDF has more detail than the page.**
   - Per resource: every candidate offering with its fit and missing capabilities, its blockers and
     required changes, and its estimate's breakdown.
   - For the whole report: the catalog snapshots' retrieval dates and sources, "how the readiness score
     is worked out" (the maintainer's words), and the shift2eu version with its build and commit.
   - *Note:* shift2eu has no readiness score today (PRD #2 left it open; PRDs #89, #190 and #297 keep it
     out of scope). This PRD reads the decision as how the report is worked out: the PDF explains the
     fits, blockers, required changes and estimates, and says in one sentence that there is no readiness
     score (*Decided, as recommended*, 6).
6. **Milestones:**
   - (1) the writer, `pdfreport`, `--format pdf`, the ADR;
   - (2) the site's switch;
   - (3) a polish round after PRD #297's first deploy.
   - *Amended 2026-11-28:* the maintainer added milestone 1b, the PDF's styling and the contact banner,
     between (1) and (2) (*Decided by the maintainer (2026-11-28)*).

### Decided by the maintainer (2026-11-28)

The maintainer checked milestone 1's PDFs in the four viewers (PASS) and found them very plain. They
decided:

1. **A new milestone, 1b, between 1 and 2,** so the site never offers the plain PDF. The milestones keep
   their numbers; milestone 2 depends on 1b, and 1b on 1.
2. **The contact banner in both the site's and the CLI's PDF:** the site's "Planning a migration?" band
   (`site/index.html`, `section.contact-band`), its eyebrow, heading and text in the site's exact words,
   and the site's gold button as a gold box holding `info@shift2eu.com`. Plain text only: no link
   annotation, URI or action; ADR 0049's ban on them stays.
3. **The site's look:** its colours (`site/style.css`), fit badges with icons, summary tiles with the big
   fit counts, the blockers box with a warning rule, and workload cards (*Look*).
4. **Colour is never the only signal:** every badge keeps its word (Direct, Partial, None) beside its
   colour and icon, and the PDF reads the same printed in greyscale.
5. **Not a copy of the site:** the PDF keeps every section and detail it has today (the detail sections,
   *How this report is worked out*, the escapes, the header and footer, the comparison tables), in the
   new styling. Nothing is dropped.

**The banner's placement** (decided by the orchestrator, within decision 2): in a single-target report,
right after the last of the sections from Summary to Cost estimate that the report shows and before
*Resource details*; in a comparison, right after the *Comparison* section and before the per-target
sections; once per document, never split across pages. The reason: that is where the site places it,
right after the results it shows. At the very end it would sit behind pages of resource details,
how-it-works and about text for a large inventory, where few readers would reach it.

### Decided in this PRD (the maintainer can overrule them)

- **PRD #297 is amended in milestone 2, in place, not now.** Until milestone 2 ships, PRD #297 describes
  the live site. This PRD lists the amendments, and milestone 2's PR applies them with a dated note.
- **The version line reaches `pdfreport` through its constructor.** An adapter may not import
  `internal/version`. Adding a field to `analysis.ReportMeta` would touch every reporter and the
  application layer for one reporter's need.
- **Courier for values, Helvetica for the report's own words.** Values stand out as the Markdown report's
  code spans do, and a fixed-width font makes escapes and look-alike values easier to compare.
- **The resources overview drops raw type, relations and reasons to *Resource details*, and the
  comparison stacks each target's fit, best offering and estimate in one cell.** Otherwise the tables do
  not fit A4 portrait with three targets.
- **The file is printable ASCII plus line feeds,** so writing it to a terminal is harmless and
  `--format pdf` behaves like the other formats on standard output, with no terminal check.
- **The glue hands the PDF over as a `Uint8Array`,** so byte-exactness does not rest on string
  conversion.
- **The module carries the release's version** (milestone 2), so the PDF's version line matches the
  released CLI's, and *About this report* names the build that made it.

### Decided by the maintainer (2026-11-25), as recommended

These twelve questions were open when this PRD was written. The maintainer decided all of them as
recommended on 2026-11-25 (recorded on PR #587): questions 1 to 7 and 9 to 11 first, then 8 and 12,
which were confirmed after asking. Question 7 is option (a), for milestone 2. Questions 5 and 9 are
**interim** answers for milestones 1 and 2; milestone 3 decides them finally and amends ADR 0049. Each
question keeps its reasoning and the alternatives weighed. Other sections refer to them as *Decided, as
recommended*, followed by the number. ADR 0049 records the answers that shape milestone 1.

1. **The download's file name.** *Decided (2026-11-25), as recommended:* `shift2eu-report.pdf`, a constant like today's
   `shift2eu-report.md`. A name built from the target or a date would carry catalog text into a file
   name and vary between runs. The CLI writes wherever `--output` says.
2. **Page size.** *Decided (2026-11-25), as recommended:* A4 portrait only. The audience is EU-based, and the layout is tested
   for one size. US Letter could be a later option.
3. **A title page or not.** *Decided (2026-11-25), as recommended:* no title page. Page 1 starts with the title block and the
   summary, so a one-page assessment stays one page or close to it. *About this report* closes the
   document.
4. **The PDF version.** *Decided (2026-11-25), as recommended:* `%PDF-1.7` (ISO 32000-1, the freely available ISO standard the
   ADR can cite by section). The writer uses only features every reader has supported since PDF 1.3, so
   the header is a label, not a dependency. PDF 2.0 deprecates the non-embedded core fonts this design
   relies on, so not 2.0. 1.4 would be equally readable; 1.7 is chosen for its ISO reference.
5. **Compressing the content streams.** `compress/zlib` (FlateDecode) is standard library and would make
   a large report several times smaller. The costs:
   - the file is no longer ASCII, so the goldens no longer diff as text;
   - the output depends on the Go release's deflate implementation, so a toolchain bump could churn
     every golden. CLI and module stay in parity, since one toolchain builds both;
   - it links `compress/flate` into the module, a cost milestone 2 would have to measure.

   *Decided (2026-11-25), as recommended, an interim answer for milestones 1 and 2, final in
   milestone 3:* no compression in milestones 1 and 2. Milestone 3 measures the PDF of a large
   inventory (several hundred resources) and decides. If it compresses, the printable-ASCII criterion is
   replaced by "every byte outside a compressed stream is printable ASCII".
6. **The readiness score.** The maintainer's decision 5 asks for "how the readiness score is worked out",
   but shift2eu has no readiness score: PRD #2 left it open, and PRDs #89, #190 and #297 keep it out of
   scope. *Decided (2026-11-25), as recommended:* the PDF explains how fits, blockers, required changes and estimates are
   worked out, and says in one sentence that there is no readiness score. A score is a domain decision
   for its own PRD. When it lands, the PDF's *How this report is worked out* section gains it.
   *Follow-up, decided with it:* CLAUDE.md's glossary defines **Assessment** with a "readiness score", and
   `docs/architecture.md` mentions one. *Decided (2026-11-27):* the maintainer decided not to implement
   the readiness score, and no docs issue is filed.
   This PRD does not change either.
7. **Where the module's version stamp comes from** (milestone 2). GoReleaser's `{{ .ShortCommit }}` is
   git's abbreviated hash, whose length depends on which objects the clone holds. The `image` job
   matches it from a full clone (ADR 0015). The release's `site` job checks out shallowly and runs before
   `goreleaser`, which consumes its bundle (it downloads the `site` artifact, checks its digest, lists it
   in `checksums.txt` and uploads it), so it can neither compute the same abbreviation reliably nor take
   GoReleaser's outputs.
   In every option the `site` job takes the version from the tag without `v` and the date with the
   `image` job's recipe (`TZ=UTC git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ`, which
   works in a depth-1 checkout), and `ci/sitebuild` stamps the three values with `-X`. The options for
   the commit:
   - (a) **a fixed cut of the full hash, the first 12 hex digits, on all three release binaries:**
     - `.goreleaser.yaml` stamps `{{ slice .FullCommit 0 12 }}` (`slice` is a `text/template` builtin);
     - the `image` job stamps `commit=$(git rev-parse HEAD | cut -c1-12)`, and the `site` job passes
       `ci/sitebuild` the same expression, run in its own checkout (it works in a depth-1 clone), so
       every binary takes the commit from what it built, never from an event field such as
       `github.sha`. Never `git rev-parse --short=12`: `--short=N` is a minimum, and git lengthens it
       when the prefix is ambiguous;
     - `test/architecture`'s pin of the image step (`releaseworkflow.go`, `imageStepTemplates`) follows,
       and ADR 0015's "Same stamp" bullet is amended;
     - `test/internal/releasetarget/testdata/real.goreleaser.yaml` is a snapshot of the file's shape, not
       a copy (its test says so), so it need not change; milestone 2 changes its commit line too, to keep
       the shape faithful;
     - the Makefile's `COMMIT` and `ci.yml`'s image stamp build no release binary and keep
       `git rev-parse --short HEAD`; a later change can align them, and this PRD does not ask for it.

     Cost: `shift2eu version` shows a 12-digit commit in every release from then on, a visible change
     the release notes mention.
   - (b) **compute the stamp once, in a new job that `site`, `goreleaser` and `image` all need.** The
     job checks out the full history, computes the version, `git rev-parse --short HEAD` and the date,
     and gives them as job outputs; `.goreleaser.yaml` reads the commit from an environment variable in
     place of `{{ .ShortCommit }}`, and the `image` and `site` jobs take the outputs. It keeps
     `shift2eu version`'s output format. Cost: a new job on the release's critical path, a
     `.goreleaser.yaml` change, three jobs' `needs` and their `test/architecture` pins, and ADR 0015's
     "Same stamp" bullet amended. The abbreviation's length still depends on that one clone, but all three
     binaries carry the same string.

     Simply reordering the jobs, so that `site` needs `goreleaser` and takes its outputs, is not an
     option: `goreleaser` consumes the `site` bundle, so that order is a cycle. Breaking it would take
     the bundle out of GoReleaser's `checksums.txt` and upload, and give it a checksum and attestation
     path of its own, which changes what the release attests and publishes (ADR 0036, PRD #297
     milestone 3). That is out of proportion to a version line.
   - (c) give the `site` job `fetch-depth: 0` and `git rev-parse --short HEAD`, exactly as the `image`
     job does (ADR 0015). It keeps `shift2eu version`'s output unchanged. Cost: the `site` job checks out
     the whole history, and the release keeps today's latent risk, now across three full clones, that
     clones taken at different moments abbreviate the same commit differently. The `verify` job's
     comparison (*Release* criteria) catches a mismatch between the image and the archives, and the
     smoke test's byte comparison one between the module and the archives, so it would fail the
     release, not ship silently, but a release could fail for it.

   *Decided (2026-11-25), as recommended: (a), for milestone 2.* It keeps today's job order, needs no new job or job outputs, does not depend on
   any clone's depth, and removes the latent risk for all three binaries. For an untagged build, the
   development values stay. Milestone 2's ADR amendments (ADR 0035, and ADR 0015 under (a) or (b))
   record the choice.
8. **Document metadata.** *Decided (2026-11-25), as recommended, confirmed after asking:* an `/Info`
   dictionary with one constant `/Title` ("shift2eu migration assessment"), so viewers show a title and
   not a blob name. No `/Producer`, `/Author`, `/Creator` or dates, and no trailer `/ID`, which ISO
   32000-1 requires only for encrypted files. The alternative was no `/Info` at all.
9. **Accessibility of the PDF.** A tagged PDF (a structure tree, as PDF/UA asks) is substantial work for a
   standard-library writer. *Decided (2026-11-25), as recommended, an interim answer for milestones 1
   and 2, final in milestone 3:* not in milestones 1 and 2. The page's HTML report stays the
   accessible view, which PRD #297's accessibility criteria cover. Milestone 3 weighs tagging, or files it.
10. **Independent validation.** The test-only reader parses only what the writer writes. *Decided
    (2026-11-25), as recommended:* milestone 1's PR also records a manual check of the bundled examples' PDFs, single and comparison, in
    four viewers: Chromium's and Firefox's built-in viewers, macOS Preview and Adobe Acrobat Reader. A
    pinned `qpdf --check` in a container (like the browser smoke test's Chromium) is a follow-up, filed
    only if the maintainer wants it in CI.
11. **Where *About this report* sits.** *Decided (2026-11-25), as recommended:* at the end, with the version line in every page
    footer too, so a printed page always names its build. The alternative is right after the title
    block, which puts the evidence first but pushes the summary down.
12. **The inventory's file name in the PDF.** The title block and *About this report* name the base name
    of the user's inventory file (`analysis.ReportMeta.InventoryName`), as the Markdown report does. A
    name such as `acme-prod-aws.json` can name a customer, and the PDF is made to be passed on.
    *Decided (2026-11-25), as recommended, confirmed after asking:* keep it. It ties the report to its input, which a reader checking the report needs,
    and the user chose the name. The privacy page says the PDF names the file, so the user can rename it
    before running. The alternative was a constant ("the inventory") in the PDF only, which breaks its
    agreement with the other reports' header.

## Out of scope

- Anything beyond the milestones above (1, 1b, 2 and 3).
- Embedding fonts, or any glyph beyond WinAnsiEncoding.
- Any server side: no rendering service, no upload, no HTTP API (ADR 0037).
- The browser's print dialog or a print stylesheet as the download.
- Changes to the content of the Markdown, JSON or HTML reports, or to the JSON report's versions.
- A readiness score, ranking or recommended target.
- Any change to sources, targets, catalogs or taxonomy categories.
