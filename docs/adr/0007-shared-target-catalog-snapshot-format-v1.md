# 0007. Read every embedded target catalog snapshot through one strictly decoded, versioned format

- Status: Accepted; the separate token walker ("One reader, not one decoder yet") is superseded,
  and the declared regions and the lint rules are amended, by
  [0009](0009-decode-adapter-json-documents-through-one-strict-decoder.md). 0009 adds the declared
  region code shape within schema version 1, a one-time, recorded exception to this ADR's schema
  version policy that ends once a snapshot can come from outside this repository; the schema version
  policy is amended by [0011](0011-scope-capabilities-and-one-offering-per-tier.md): snapshots are
  embedded and rewritten in the same change as the vocabulary, so retiring a vocabulary value is not
  a version change; the format moves to schema version 2, with plans, monthly EUR prices and a default
  region, by [0024](0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md), and
  the reader no longer reads version 1; one optional offering key, `fixedFees`, is added within
  version 2 by [0026](0026-add-an-optional-fixed-monthly-fee-per-offering-to-snapshot-format-v2.md), a
  recorded exception to the schema version policy and to "every key required"; a second,
  `planBillingCapHours`, by
  [0029](0029-cap-the-billed-hours-of-an-offerings-plans-in-target-snapshot-format-v2.md); a third,
  `global`, by [0048](0048-record-global-offerings-in-target-snapshot-format-v2-and-add-network-dns.md),
  whose global offering's `regions` is empty, an exception to "at least one" region per offering
- Date: 2026-11-10

## Context

A target adapter serves its catalog from an embedded, dated snapshot: a JSON document compiled by hand
from the provider's public documentation, with a `SOURCES.md` beside it saying where each fact comes
from. PRD 2 (issue #2) milestone 5 added the first, for Scaleway, with a parser of its own in the
Scaleway package. PRD 11 (issue #11) adds OVHcloud, and UpCloud is planned as the third target.

Forces:

- **Copies drift.** Two strict parsers of the same shape have drifted already. The inventory-file
  reader (ADR 0006) gained exact keys, refused repeated keys and refused lone surrogates. The Scaleway
  parser did not: it still matched keys ignoring case and kept the last of a repeated key (issue #13). A
  copy per target would make every rule a review question per target.
- **A snapshot error changes a verdict without a word.** The snapshot is the only evidence the report
  has of what a target offers. With `encoding/json` alone, a repeated `regions` key drops the first
  list, a `null` or missing `capabilities` reads as no capabilities, and an escaped lone surrogate
  becomes U+FFFD. Each one changes a fit or a region in the report and nothing says so. PRD 11 asks that
  such a value is "never dropped or defaulted".
- **The snapshot is reviewed, not trusted.** It is embedded at build time, so tests are the gate that
  stops a bad edit. Issue #13 also anticipates catalogs that are fetched or supplied by a user, and
  those would be untrusted input.
- **Errors must be the same for every target.** A shared reader cannot name the target it reads for.
  The adapter knows the target and the embedded file, so it adds them.
- **The domain decides which values are valid.** `catalog.New`, `taxonomy.ParseCategory`,
  `taxonomy.ParseCapability` and `text.Validate` already hold those rules. Repeating them in a reader
  would let the two drift.
- **Layering.** JSON is an adapter concern, and the domain holds no encoding (ADR 0001). PRD 11 also
  forbids changes under `internal/domain`, `internal/app` and `internal/adapter/report` for a new target.
- CLAUDE.md rule 6 prefers the standard library.

## Decision

Every target adapter with an embedded snapshot reads it through one package,
`internal/adapter/target/snapshot` (`snapshot.Parse(data) (snapshot.Document, error)`). The package
defines the format below. A target package holds only its own `catalog.json`, its `SOURCES.md`, a thin
`Source` that calls `Parse` and wraps errors with its name, and its tests. This ADR fixes **schema
version 1**.

A snapshot is a UTF-8 JSON document (RFC 8259), without a byte order mark, that holds one object:

```json
{
  "schemaVersion": 1,
  "target": "Example Cloud",
  "sourceURL": "https://www.example.com/docs/",
  "retrievedOn": "2026-11-10",
  "regions": ["fr-par", "nl-ams"],
  "offerings": [
    {
      "name": "Example Compute",
      "categories": ["compute.vm"],
      "capabilities": ["ipv6", "snapshots"],
      "regions": ["fr-par", "nl-ams"]
    }
  ]
}
```

**Fields.** Every field is required, at the top level and in each offering, and there are no optional
fields. Keys are spelt exactly as below, character for character once JSON escapes are decoded, and
appear at most once per object. `null` is never a value: it is refused like any other value of the wrong
type, and is never read as absent or empty. An explicit empty list is a value and is accepted where the
domain accepts it.

| Field | JSON type | Meaning |
|---|---|---|
| `schemaVersion` | integer | Format version; `1` for this ADR |
| `target` | string | The target's display name, as reports show it (for example `Scaleway`) |
| `sourceURL` | string | The `https` page the data was compiled from, as reports cite it |
| `retrievedOn` | string, `YYYY-MM-DD` | The date the data was taken, read as UTC midnight |
| `regions` | array of strings; may be empty | The EU regions the snapshot declares, in the order reports list them |
| `offerings` | array of offering objects; may be empty | The offerings, in any order |

Each offering:

| Field | JSON type | Meaning |
|---|---|---|
| `name` | string | The offering's name, unique in the snapshot |
| `categories` | array of strings | Taxonomy categories it serves, at least one (`catalog.New`) |
| `capabilities` | array of strings; may be empty | Capabilities from the taxonomy vocabulary |
| `regions` | array of strings | Declared regions it is available in, at least one (`catalog.New`) |

**Strict decoding.** `Parse` checks the following, in this order, and refuses a failure with
`ErrDecode` unless another sentinel is named:

1. The document is at most `MaxDocumentBytes` (1 MiB), checked before anything else. Exactly 1 MiB
   is accepted.
2. It does not start with a byte order mark, UTF-8 (`EF BB BF`) or UTF-16 (`FF FE`, `FE FF`), which is
   refused by name as the inventory-file reader does (ADR 0006), and it is valid UTF-8.
3. It is one JSON value, an object, with nothing after it but white space. Data after the first value
   is reported as "unexpected data after the first JSON value", the inventory-file reader's words.
4. `schemaVersion` is exactly the integer `1` (`ErrSchemaVersion` otherwise, also when it is missing).
   It is found by scanning the top-level keys for the exact key, before any other key or value is
   checked. So a document of another version is refused for its version alone, even if it also has
   unknown, repeated or misspelt keys. `1.0` and `"1"` are not version 1. A key that differs only in
   case is not the version, and the error names it.
5. Token by token (`json.Decoder.Token`), the reader refuses each of these:
   - a key the format does not define, a key spelt inexactly or a repeated key;
   - a value of the wrong JSON type, `null` included;
   - an object without every key it requires;
   - a string, key or value, that escapes a surrogate (U+D800 to U+DFFF) other than as a high surrogate
     followed at once by a low one.

   The walk builds the document as it reads, so nothing is decoded twice. `encoding/json` on its own
   matches keys ignoring case, keeps the last of a repeated key, reads `null` and a missing field as the
   zero value, and decodes a lone surrogate to U+FFFD.
6. `retrievedOn` is a `YYYY-MM-DD` date.
7. Every declared region is non-blank, valid text (`text.Validate`) and declared once.
8. Every offering region is a declared region (`ErrUndeclaredRegion`). The declared regions are held in
   a set, so this check stays linear however many regions are declared.
9. `catalog.New` accepts the target, the source URL and every offering, and its error is returned as
   it is, so `catalog.ErrInvalidCatalog` and the taxonomy sentinels stay reachable. The domain sorts
   each offering's lists and drops duplicates in them: order and repetition in those lists carry no
   meaning.

**Declared regions.** The top-level `regions` list promises that the snapshot was checked for these
regions and no others. `Document.Regions` holds it in file order, in a new slice for every `Parse`. Each
target adapter pins its list in a test and passes it to `targettest`. Whether a region is in an EU
member state (PRD 11) is checked in review against the provider's published regions. The reader only
checks that the snapshot is consistent with its own declaration.

**Errors.** Every error is one line and wraps one of the sentinels above or the error of `catalog.New`.
A problem the token walk finds, other than a missing top-level key, is placed by line and column (both
from 1, the column in characters), and so are a byte that is not UTF-8, malformed JSON and data after
the first JSON value. The other errors carry no position: they name the value by its path or its name,
or speak of the whole document. These are the size cap, a byte order mark, a document that is empty,
cut short or not an object, the schema version, a missing top-level key, and the checks after the walk
(steps 6 to 9). A path writes an array element with its index in brackets, as the domain does
(`offerings[1].regions[0]`). Values from the document are shown through `text.Quote`, and a
`schemaVersion` number not written as a plain integer (`-0`, `1.0`, `1e0`) is shown as written. The text
of `encoding/json` and `time.Parse` errors is never passed on. The reader's errors name neither the input
nor any target. The adapter wraps them: `embedded Scaleway catalog "catalog.json": ...`.

**Schema version policy.** As for inventories (ADR 0006): any change to the format, even a new
optional field, is a new version recorded in a new ADR. The reader is shared, so a new version reaches
every target at once. A transition either rewrites every embedded snapshot in the same change or has the
reader accept both versions for a while.

**Size cap.** An embedded snapshot is a few kilobytes, and 1 MiB leaves room to grow. The cap also
bounds the work: the worst case under it is about 60,000 declared regions with the last one mentioned
about 80,000 times. `BenchmarkParseRepeatedRegionMention` builds that document and measured about
22 ms per parse on the development VM, against about 7 s for a variant that scans the declared list
for each mention. `Parse` takes the whole document in memory, so the cap bounds decoding, not reading.
A caller that ever reads a snapshot from a stream, such as a fetched or user-supplied catalog (issue
#13), must read at most `MaxDocumentBytes`+1 bytes (`io.LimitReader`) before calling `Parse`, so a
larger one is still refused without being read whole.

**`SOURCES.md`.** Every `catalog.json` has a `SOURCES.md` beside it with:

- a `## Offerings` section with exactly one `### <offering name>` subsection per offering, and no
  subsection for an offering the snapshot does not have;
- in each subsection, every category, capability and region of the offering written as a Markdown code
  span, beside the page and the fact it comes from;
- a non-blank `## Licences of the sources` section that states the licence of each source.

`TestEveryOfferingHasASourcesSection` in the snapshot package globs `../*/catalog.json`, so it checks
every target adapter's snapshot and picks up a new target without a change to the test.

**One reader, not one decoder yet.** The reader follows ADR 0006's rules and uses the same technique as
the inventory-file reader, but it is a separate implementation. Extracting one strict JSON decoder that
both use is issue #13.

## Alternatives considered

- **A parser per target, copied from Scaleway's.** Simple at first, but the copies drift (they already
  have, see *Context*), and every strictness rule would need its tests and review once per target.
- **Parse the snapshot in the domain (`catalog`).** JSON is an encoding, which the domain does not hold
  (ADR 0001), and PRD 11 rules out domain changes for a new target. `catalog.New` stays the validator
  of values and knows nothing of JSON.
- **One generic strict JSON decoder for the inventory file and the snapshot now.** This is the right
  end state, and issue #13 tracks it. It would also change the inventory-file reader and the
  behaviour ADR 0006 pins, in a milestone scoped to targets. Two concrete walkers with the same rules
  are easier to unify than one walker designed ahead of its second user.
- **Optional fields with defaults, or `null` read as absent** (ADR 0006 allows that for its optional
  fields). A snapshot has no field whose absence means something, so a missing or `null` field is
  always a mistake. Read as empty, a forgotten `capabilities` key would describe an offering with no
  capabilities, and the report would show worse fits than the provider offers.
- **Lenient decoding (ignore unknown or misspelt keys).** It turns a typo into lost data, as ADR 0006
  records for inventories.
- **Declared regions in `catalog.Catalog`, checked by `catalog.New`.** This is cleaner, and it is one of
  the items in issue #13. It changes `internal/domain`, which PRD 11 asks to avoid, so the declared list
  stays in `snapshot.Document` and in each adapter's `Regions()` for now.
- **A format per target**, for OVHcloud's per-product region names or its 3-AZ variants. PRD 11
  expresses both in this format: one region code per location, and a separate offering for a variant
  whose capabilities differ by region. A target-specific format would also bring back the per-target
  parser.
- **YAML, CSV or JSON Lines, and `encoding/json/v2`.** These were rejected for the reasons ADR 0006
  gives.

## Consequences

- A second or third target adds a `catalog.json`, a `SOURCES.md`, a thin `Source` and its own tests:
  the golden catalog, the declared regions and `targettest`. The format, its strictness and its errors
  are pinned once, in the snapshot package's tests.
- Snapshot errors are worded the same for every target, and a report error for one target never
  names another.
- Changing the format changes every target's snapshot. That is deliberate friction, recorded in a new
  ADR and a new version.
- Until issue #13 lands, two token walkers implement the same rules, one per format. A fix to one is
  checked against the other. The snapshot reader already puts the column of a malformed multi-byte
  character on its first byte; the inventory-file reader does not yet.
- `.golangci.yml` holds five rules for this decision:
  - `target-snapshot-reader-parses-bytes` limits the snapshot package to a list of parsing packages
    from the standard library and the three domain packages it uses (`catalog`, `taxonomy`, `text`),
    so a target that reads its snapshot through it stays offline. An import rule cannot see a call, so
    review keeps `fmt.Print*` and `time.LoadLocation` out of it.
  - `target-adapters-read-snapshots-through-the-reader` denies `encoding/json`, and with it
    `encoding/json/v2` and `encoding/json/jsontext`, to every file under `internal/adapter/target` that
    is not a `_test.go` file, except the reader's own. That covers every target adapter, a target added
    later included, and the contract suite `targettest`. A target cannot decode its snapshot on its own
    and skip the rules above, and a JSON decode in `targettest` would be the second reader this ADR
    rules out. Only `_test.go` files may decode JSON.
  - `adapters-are-built-in-cmd` stops one adapter importing another. It allows each shared adapter
    helper under `internal/adapter/internal/` by name, ending in `$` (issues #49, #50), and, by an
    entry of its own, this reader
    (`internal/adapter/target/snapshot$`): a target adapter that imports it imports a shared
    adapter-layer reader, not another adapter. The reader stays where it is rather than under
    `internal/adapter/internal/`, beside the targets whose `catalog.json` its tests check. An allow
    entry holds for every file the rule covers, so on its own it would let any package import the
    reader.
  - `snapshot-reader-only-in-targets` keeps the reader to the packages under `internal/adapter/target`:
    the target adapters, `targettest` and the reader itself. It denies the reader to every other file
    that is not a `_test.go` file, `cmd/shift2eu` included. A source adapter, a reporter, `internal/config`
    and the CLI have no snapshot to read, and the composition root gets each catalog through the target
    adapter it constructs.
  - `embedded-snapshot-targets-stay-offline` limits every file of a target that serves an embedded
    snapshot (OVHcloud and Scaleway) that is not a `_test.go` file to a strict allow list: `context`,
    `embed`, `fmt`, the reader and the domain packages `catalog` and `text`, each ending in `$`. Such a
    target reads only the bytes it embeds, and every project package it imports is held offline by a
    strict rule of its own, so the whole path from embedded bytes to catalog stays offline. A shared
    adapter helper under `internal/adapter/internal/` is allowed only once the rule names it: an import
    rule sees direct imports only, and a helper under a `testdata` or `_` directory is never linted
    (issue #42). It still denies `io/fs`, `io/ioutil`, `net`, `os`, `plugin`, `syscall` and `unsafe` by
    name, so the reason is given. The targets are named one by one, because a target that calls its
    provider's API needs the network: a new embedded-snapshot target is added to the rule. An import
    rule cannot see a call through an allowed package, so review holds that line, as for the reader.
- The embedded snapshot is parsed on every `Catalog` and `Regions` call. That is cheap at a few
  kilobytes, and it keeps the adapter free of package-level state.
