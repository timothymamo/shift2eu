# 0018. Version the JSON report format, shared by the CLI and the HTTP API

- Status: Accepted; amended 2026-11-18 by [issue #247](https://github.com/timothymamo/shift2eu/issues/247):
  the quantity rule (Encoding) records how the report already writes numbers, changing no document;
  amended 2026-11-19 by [issue #307](https://github.com/timothymamo/shift2eu/issues/307):
  `schemaVersion` 3 leaves the amount out of a total that estimated nothing, the CLI writes it by
  default, and the version 1 and 2 schema files are frozen (Versioning)
- Date: 2026-11-11

## Context

PRD #103 (issue #103) serves assessments over an HTTP API, and its first milestone adds
`shift2eu assess --format json`. Until now the only output was the Markdown report ([ADR 0016](0016-compare-targets-through-a-second-reporter-port-and-use-case.md) for the
comparison), written for people. Scripts, CI jobs and the website need a document they can parse, and
once one is published, changing what it means breaks them. [ADR 0006](0006-json-inventory-file-format-v1.md)
gave the inventory file, our input, a recorded shape and a version policy; the report, our output,
needs the same before anyone depends on it.

The version policy deliberately differs from ADR 0006's. There, any change, even a new optional key, is
a new version, because `shift2eu` is the reader and decodes strictly: a version 1 reader refuses a key it
does not know, so an added key would break it anyway. Here `shift2eu` is the writer, and the readers are
scripts, CI jobs and the website, which ignore keys they do not know. Forcing a new version for every
added key would make each of them upgrade for a change that cannot break them, and would contradict the
API's rule that additive fields are not breaking (PRD #103). So only a change that can break such a
reader is a new version.

Forces:

- Every string in a report comes from the inventory or a catalog and is untrusted: quotes,
  backslashes, control characters, `</script>`, U+2028 and invalid UTF-8 must not break the document
  or a page that embeds it.
- Reports must be deterministic (CLAUDE.md, Testing): golden files pin them byte for byte.
- The JSON report must say what the Markdown report says, ties and required changes included, so the
  two never disagree.
- The CLI and the API must give the same bytes for the same request (PRD #103, Decisions).
- CLAUDE.md rule 6 prefers the standard library; there is no JSON Schema validator in it.

## Decision

The JSON report format **v1** is two documents, rendered by `internal/adapter/report/jsonreport`,
which implements both `analysis.Reporter` and `analysis.ComparisonReporter`:

- `"kind": "assessment"`: one inventory against one target. Keys: `schemaVersion`, `kind`,
  `inventoryName`, `source`, `target` (`name`, `snapshot` with `source` and `retrieved`), `summary`
  (`direct`, `partial`, `none`, `total`), `resources` and `blockers`.
- `"kind": "comparison"`: one inventory against several targets. Keys: `schemaVersion`, `kind`,
  `inventoryName`, `source`, `targets` (each `key` plus the single document's `target`, `summary`,
  `resources`, `blockers`) and `sharedBlockers` (`resourceId`, `targets` with `key` and `reasons`).

A resource holds `id`, `rawType`, `category`, `region`, `fit` (`direct`, `partial`, `none`),
`bestOffering`, `ties`, `reasons` and `requiredChanges`. A reason holds `code` and `text` (the plain
sentence of `mapping.Reason.String`, no Markdown), and `target`, `category`, `offering` and `capability`
only when the reason sets them; the same reason object is used in resources, blockers and shared
blockers. Ties and required changes come from `assessment.Verdict.Ties` and `ChangesToList`, which the
Markdown renderer uses too. The JSON also carries each resource's `region`, which the Markdown report
does not show.

Every document carries `"schemaVersion": 1` and its `kind`, so a reader can dispatch on both before
reading anything else.

**Schemas.** `internal/adapter/report/jsonreport/schema/assessment-v1.schema.json` and
`comparison-v1.schema.json`, JSON Schema draft 2020-12, restricted to the keywords `$schema`, `$id`,
`title`, `description`, `$defs`, local `$ref`, `type`, `properties`, `required`,
`additionalProperties` (false only), `items`, `enum`, `const`, `minimum` and `pattern`. Every object
has `"additionalProperties": false` and lists its always-present keys in `required`. Each file is
self-contained: the comparison schema repeats the shared definitions instead of a cross-file `$ref`.
A test validates every golden document against its schema with a validator written on the standard
library alone, which refuses any keyword outside the subset, so the schemas never use a construct
nobody checks. No new dependency.

**Encoding.** Documents are built from private Go structs with `json` tags and encoded with
`encoding/json`, never by concatenating strings: two-space indent and exactly one trailing newline.
`encoding/json` escapes `<`, `>`, `&`, U+2028 and U+2029 and writes each invalid UTF-8 byte as U+FFFD,
so every document is valid JSON and valid UTF-8. Keys are camelCase. Required keys are always present:
an empty string is `""` and an empty array is `[]`, never `null`. Order is the domain's (resources
and blockers in the assessment's order, targets by key, shared blockers by resource ID), never map
order. `retrieved` is the snapshot's UTC calendar date, `YYYY-MM-DD`, as the Markdown report prints it.

*Amended (2026-11-18, [issue #247](https://github.com/timothymamo/shift2eu/issues/247)):* the
**quantity rule**, which records the shape `schemaVersion` 2 already has and changes no field's type,
name or value. A whole-number quantity is a JSON number, typed `integer` in the schema: counts, a plan's
`vcpus` and `includedStorageGiB`, `billingCapHours` and `hoursBasis`. An exact decimal value is a JSON
string, typed `string` with a `pattern`: every amount of money (a plan's `price`, an estimate's
`amount`, the `breakdown` parts, a `total`'s `amount`, a source `currencyCost`'s `amount`, each rounded
to the cent, `"587.58"`) and every quantity that can be fractional (a plan's `memoryGiB`, `"0.5"`,
`"1.875"`). No field is a fractional JSON number (`number`), so a consumer never reads a float that
`encoding/json` or its own parser has rounded; it parses the strings as decimals. The current schemas
state the rule in the description of every amount of money and every measured quantity, a plan's
`vcpus`, `memoryGiB` and `includedStorageGiB`, `billingCapHours` and `hoursBasis` ("whole number" or
"exact decimal"); a count of resources (the `summary` counts, a `total`'s `estimated`, `resources`
and `storageNotIncluded`, a `sourceCost`'s `covered`, every other `resources`) is whole by nature and
says only what it counts. `schemaVersion` is left out of the rule too, but not as a count: it
identifies the document's format, says which schema a reader must use, and counts nothing, so no
representation rule applies to it ([issue #284](https://github.com/timothymamo/shift2eu/issues/284);
the test lists it in `countAndIdentifierFields`). `TestSchemaQuantitiesStateTheQuantityRule` and
`TestSchemaQuantitiesAreNeverNumbers` pin it; `TestSchemaQuantitiesClassifyEveryField` makes every new
field be classified as a quantity or a count. Version 1 has only counts, no money and no fractional
quantity, so the rule holds for it as written; its schema files keep the prose they shipped with
([ADR 0030](0030-serve-the-version-2-report-on-v1.md) keeps them unchanged). A new money or fractional
field follows the rule; turning a string into a number or the reverse is a retyping, so a new
`schemaVersion`.

**Versioning.**

- Adding an optional key, or a new value of an open set such as a reason `code`, keeps
  `schemaVersion` 1. The schema files are updated in the same change. A consumer must ignore keys it
  does not know and accept reason codes it does not know (`text` always explains them). The schemas'
  `additionalProperties: false` pins what this release writes; it is not a promise that no key is
  ever added, so a consumer that validates documents uses the schema of the release that wrote them.
- Removing, renaming or retyping a key, making a required key optional, adding a `fit` or `kind`
  value, or changing what a value means is a new `schemaVersion`, with new schema files beside the old
  ones and an ADR. This matches the API's rule that additive fields are not breaking (PRD #103).
- Moving the schema files' `$id` URLs keeps `schemaVersion` 1: no document a reader receives
  changes. The project's rename to `shift2eu` ([ADR 0020](0020-rename-the-project-to-shift2eu.md),
  issue #117) moved them from the old repository to `https://github.com/timothymamo/shift2eu` this
  way. The OpenAPI document's references to the schemas are relative and did not change.

*Amended (2026-11-19, [issue #307](https://github.com/timothymamo/shift2eu/issues/307)):* **version 3**.
A `schemaVersion` 2 total (`estimate.total`, each workload's `estimate`, the "No workload" group's
included, and each comparison target's total) always carries `amount`, and when nothing was estimated
(`estimated: 0`) it reads `"0.00"`, which the version 2 schemas call meaningless; a consumer that reads
the amount without the count shows a free bill. In `schemaVersion` 3 such a total has **no `amount` key
at all**; its counts stay, and a total that estimated something is written exactly as in version 2.
This makes a required key optional, which the rule above makes a new version, with the new schema files
`assessment-v3.schema.json` and `comparison-v3.schema.json` beside the old ones: copies of version 2
whose `$defs/total` no longer requires `amount` and says it is absent exactly when `estimated` is 0.

- **Absent, not `null`.** [ADR 0030](0030-serve-the-version-2-report-on-v1.md) already chose an absent
  key over a flag or an empty value for estimates off, because an absent key cannot be misread as a
  zero; the schemas' keyword subset has no `null` type; and a consumer tests for the key in either
  case.
- **Which version is written.** `jsonreport.New` takes the version, `jsonreport.Version2` or
  `Version3`, and refuses any other when it reports. `shift2eu assess --format json` writes version 3
  by default; `--schema-version 2` writes the previous document, byte for byte, and the flag with any
  other format is a usage error. The HTTP API keeps serving version 2 on `/v1/assessments` unless the
  request asks for 3 (its optional `schemaVersion`, ADR 0030's amendment), so no existing client sees
  a change.
- **Frozen files.** The version 1 and version 2 schema files never change again: a reader of an old
  document must find the schema it was published with. `TestFrozenSchemaFilesAreUnchanged` pins their
  SHA-256 digests ([issue #293](https://github.com/timothymamo/shift2eu/issues/293)); version 2 is
  pinned now because version 3 supersedes it. A deliberate change records the new digest with an ADR.
- **Wording in version 3 only.** The reworded `breakdown.storage` description
  ([issue #285](https://github.com/timothymamo/shift2eu/issues/285), "The storage's price per GiB, never
  scaled by hours, as an exact decimal string rounded to the cent.") is in the version 3 files only, so
  the version 2 files stay byte for byte what shipped.

**One format for the CLI and the API.** `shift2eu assess --format json` writes these documents (the
default stays `markdown`, byte-identical to before), and the HTTP API of PRD #103 answers with the
same reporter and therefore the same bytes. The schema files are where milestone 2's API can serve
or reference them from; embedding them is left to that milestone, so the reporter today imports only
`encoding/json` beyond what the Markdown reporter does (the `reporters-stay-offline` depguard rule).

## Alternatives considered

- **Marshal the domain types directly.** No tags to write, but the document would follow every
  internal rename, expose fields such as `Candidates` that are not part of the contract, and write
  `null` for nil slices. Private structs keep the format a deliberate contract.
- **Concatenate strings, as the Markdown renderer does.** Escaping JSON by hand is the bug the hostile
  goldens exist to catch; `encoding/json` already gets it right.
- **One document with an optional comparison part.** Every consumer would have to branch on which
  keys are present. Two kinds with their own schemas say it up front.
- **A third-party JSON Schema validator for the tests.** Full draft 2020-12 support, but a dependency
  for tests only, and it would let the schemas use keywords that a reader of the schema has to know.
  The small standard-library validator keeps the schemas simple and the module unchanged.
- **Every change is a new `schemaVersion`.** Simple, but each added field would force every consumer
  to upgrade, and it would disagree with the API's rule that additive fields are not breaking.

## Consequences

- Scripts and the website get a stable, documented document; the CLI and the API cannot drift apart.
- The Markdown and JSON reports agree on ties and required changes because both read them from the
  domain.
- `cli.Deps` now takes `Formats`, a map of `--format` value to `cli.Format` (the two reporter
  constructors); adding a format is one entry in `cmd/shift2eu`.
- A change to the report's structs must update the schemas, the goldens and, for anything but an
  additive change, the version and an ADR. The schema tests fail on a key the schema does not allow, so
  an unplanned addition is caught.
- The schemas stay within the keyword subset; a constraint that needs another keyword means extending
  the test validator first.
