# Hostile inventories

The scheduled security audit's corpus for row 13, the CLI on hostile inventory files (PRD #175,
[ADR 0040](../../../../docs/adr/0040-audit-the-repository-and-the-latest-release-on-a-schedule.md)). The
checks job copies every `*.json` file here, with the files it generates at run time (an oversized file,
too many resources, deep nesting, malformed UTF-8, a byte order mark, a NUL byte, a huge string, an
empty file, a directory and links to `/proc/self/environ`, `/dev/zero` and `/dev/urandom`), into a
container of the latest release's image that never starts, commits it, and runs `shift2eu assess` on
each file in a read-only container without network. The raw output goes only to the encrypted results.

Every value is sanitised (CLAUDE.md rule 2): `example.com`, `192.0.2.0/24` and `123456789012` only.
Each file holds one kind of hostility; a file the decoder refuses at its first problem holds that one
problem alone, so the refusal is the one under test.

## The two halves

The corpus holds two kinds of file, and `TestHostileCorpusDecisions` in
`internal/adapter/report/htmlreport/inventorycorpus_test.go` pins which is which and the sentinel each
refusal wraps:

- **Refused at load**, one defect each: the strict JSON decoder's rules, a version this decoder does not
  read, a value of the wrong type, a category or capability outside the taxonomy, and free text
  `text.Validate` refuses. These exercise the error path, which must stay one line and a controlled
  error.
- **Load, and are hostile all the same**: hostile text in every free-text field the format has, which
  no rule of the decoder or of the domain refuses. These reach the assessment and the renderers, which
  is what the first audit found missing: 31 of the 32 inputs it ran were refused at load, so the
  Markdown and JSON reports were exercised by one input only.

`TestHostileCorpusReports` renders each file that loads, against the Scaleway target with estimates on,
as Markdown, as the version 3 JSON document and as the site's HTML fragment, and holds the three to the
goldens under `internal/adapter/report/htmlreport/testdata/hostile-corpus/` (update them with `-update`).
`FuzzInventoryDecoding`, in the same package, seeds its corpus from these files and the inventory-file
adapter's version 1 and version 2 fixtures, and holds the same path for arbitrary bytes: no panic, a
refusal that is controlled, and an accepted inventory that assesses and renders without an error.

Two kinds of hostility are refused wherever they appear, so no file can carry them and load: any
character `text.Validate` refuses (control characters, the bidi overrides and other format characters,
U+2028 and U+2029, variation selectors, noncharacters, invalid UTF-8) in a free-text field, and a
document that is not strict JSON. The format's free-text fields are the source, a resource's `id`,
`rawType`, `region` and `workload`, a relation's `target` and a cost's three letters; it has no tags.

### The files that load

| File | Hostility |
|---|---|
| `html-metacharacters.json` | `<`, `>`, `&`, `"` and `'` in the source and in every free-text field: script, style and attribute-injection payloads |
| `length-limits.json` | every free-text value exactly 2048 (`text.MaxLen`) bytes, of markup characters: `<` in a raw type, `\|` in a region, a backtick in a workload |
| `markdown-metacharacters.json` | `\|`, `*`, `_`, backtick, `#`, `[`, `]`, `(`, `)`, `~`, `\`, `!` in the source and in every free-text field: table rows, code fences, links, images, headings, blockquotes, lists, footnotes and reference definitions |
| `markup-injection.json` | Markdown and HTML syntax in IDs, raw types, workloads and a relation's target |
| `number-limits.json` | version 2 numbers at the ends of every range the domain accepts, from 0 and 1e-9 to the maxima of a size, a usage and a monthly cost |
| `unicode-lookalikes.json` | Unicode that only looks hostile and that `text.Validate` accepts: Cyrillic homoglyphs, combining marks, Hebrew and Cyrillic letters, a no-break space inside a value, fullwidth markup, a private-use code point and an astral emoji |
