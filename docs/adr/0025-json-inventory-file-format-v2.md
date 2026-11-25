# 0025. Add inventory file format version 2 for sizing, usage, workloads, relations and cost

- Status: Accepted; amends [0006](0006-json-inventory-file-format-v1.md), whose rules version 2 keeps;
  amended by [0046](0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md): the size-field
  rows of seven new categories; amended by
  [0047](0047-add-search-opensearch-and-messaging-kafka-and-price-their-storage-per-node.md): the size-field
  rows of `search.opensearch` and `messaging.kafka`
- Date: 2026-11-13

## Context

PRD #190 (issue #190) adds plan matching, monthly estimates, workload grouping and relations to the
assessment. Each needs facts about a resource that the inventory file of
[ADR 0006](0006-json-inventory-file-format-v1.md) cannot hold: how big a resource is, how much of a month
it runs, which application it belongs to, which other resources it depends on, and what the source
charges for it today. ADR 0006 versions the format so that a change in what a file means is a new
version, and refuses any key it does not know, so these fields need a new version.

Forces:

- Version 1 files exist (the samples under `examples/`, users' own files), and must keep their meaning
  and their verdicts.
- Every format version costs a reader, an ADR and a migration of the samples. Four features that each
  need new resource fields would cost four versions if they arrived one by one.
- The file stays untrusted input (ADR 0006): every rule of version 1 (exact keys, no repeats, right JSON
  types, caps counted while the document is read, values checked by the domain) must hold for the new
  fields too.
- An estimate must never be lower than the truth because a missing number was read as 0 (PRD #190,
  "never zero for unknown"). The domain must keep "not given" apart from 0.
- The HTTP API never repeats a value from a document in an error (ADR 0019), so a refused new field
  must be nameable by its path alone.

## Decision

The inventory file has a version 2, `"schemaVersion": 2`, which is version 1 plus five optional resource
fields. The reader accepts versions 1 and 2. A writer (the future `shift2eu discover`) writes version 2.
Version 1 is unchanged: a version 2 key in a version 1 file is an unknown key, refused by its path.

### The fields

```json
{
  "id": "db-shop-replica",
  "category": "database.postgresql",
  "size": { "vcpus": 2, "memoryGiB": 8, "storageGiB": 100, "count": 1 },
  "usage": { "hoursPerMonth": 730 },
  "workload": "shop",
  "relations": [{ "type": "replica-of", "target": "db-shop" }],
  "monthlyCost": { "amount": 123.45, "currency": "USD" }
}
```

| Field | JSON type | Rule |
|---|---|---|
| `size.vcpus` | number | more than 0, at most 1,000,000; fractions allowed |
| `size.memoryGiB` | number | more than 0, at most 1,000,000 |
| `size.storageGiB` | number | at least 0, at most 1,000,000,000 |
| `size.count` | integer, written with no fraction or exponent (like `schemaVersion`) | at least 1, at most 100,000 |
| `usage.hoursPerMonth` | number | more than 0, at most 744 |
| `workload` | string | free text (`text.Validate`); `""` is the same as absent |
| `relations[i].type` | string | `replica-of`, `dead-letter-queue-of`, `attached-to` or `depends-on` |
| `relations[i].target` | string | the `id` of another resource of the same file |
| `monthlyCost.amount` | number | at least 0, at most 1,000,000,000,000 |
| `monthlyCost.currency` | string | three upper-case ASCII letters (the shape of ISO 4217) |

The ranges and why:

- `vcpus` and `memoryGiB` are more than 0 because nothing runs on none. `vcpus` takes fractions for
  shared cores (0.5). The upper bounds sit far above any real machine or node, and only refuse a typo or
  a unit mistake (bytes written as GiB).
- `storageGiB` may be 0: an empty bucket or a server with no local disk is a real size, and 0 given is
  not "not given". Its bound is a billion GiB, about an exabyte, above any single store.
- `count` is an integer, because half an instance is meaningless, and at least 1, because zero instances
  is a resource that does not exist. 100,000 bounds a multiplication the estimate makes.
- `hoursPerMonth` is at most 744, the hours of a 31-day month. More is a mistake, not a busy month.
- `amount` may be 0 (free tier). A trillion bounds it far above any bill.
- A number that overflows a float64 (`1e999`) is refused by the reader. JSON's other types (`"4"`,
  `true`) are refused for their type, never converted.

Inside `size`, `usage` and `monthlyCost`, keys follow version 1's rules: exact, at most once, of the
right type. `null` is the same as absent at every level, and `size: {}` and `usage: {}` are the same as
absent too. Both keys of a relation and of a cost are required. An element of `relations` may not be
`null`, as an element of `resources` or `capabilities` may not.

### Absent versus 0, and defaults

The domain keeps every number as a pointer: nil is "not given", a pointer to 0 is a given 0. It stores
what the file says and no defaults, except that a given -0 (`storageGiB`, `amount`) is stored as 0, so no
report ever prints "-0". Later milestones read a `count` that is not given as 1 and an
`hoursPerMonth` that is not given as 730 (an average month: 365 × 24 / 12), and say so in the report.
A missing `vcpus`, `memoryGiB` or `storageGiB` stays unknown, and a resource that cannot be sized is
never priced as if it cost 0.

### Which fields each category takes

A size or usage field outside this table is refused as making no sense for the category
(`inventory.ErrFieldNotForCategory`), for example `vcpus` on `storage.object`.

| Category | `vcpus` | `memoryGiB` | `storageGiB` | `count` | `usage.hoursPerMonth` |
|---|---|---|---|---|---|
| `compute.vm`, `compute.baremetal` | yes | yes | yes | yes (instances) | yes |
| `kubernetes.managed` | yes (per node) | yes (per node) | yes (per node) | yes (nodes) | yes |
| `database.postgresql`, `database.mysql` | yes | yes | yes | yes (nodes) | yes |
| `storage.block` | no | no | yes | yes (volumes) | no |
| `storage.object` | no | no | yes (stored GiB) | no | no |
| `serverless.function` | no | yes (per function) | no | no | no |
| `messaging.queue`, `unclassified` | no | no | no | no | no |

`workload`, `relations` and `monthlyCost` are allowed on every category, `unclassified` included.

### Relations

- The vocabulary is closed and kept in the domain (`inventory.ParseRelationType`), like the taxonomy's
  vocabularies. A type is matched character for character.
- A target names another resource of the same file, compared as duplicate IDs are compared (runs of
  white space are one space). A relation to itself is refused. The stored target is the target
  resource's ID as that resource writes it.
- Duplicate relations (same type and target, after resolving) are dropped, and relations are kept sorted
  by type, then target, as capabilities are, so everything derived from them is deterministic.
- Cycles are allowed: `depends-on` both ways is a fact about some systems. Nothing in milestone 1 walks
  the graph.
- The reader refuses more than `MaxRelations` = 32 relations on one resource, counted while it walks the
  document, before the elements are decoded, as it counts `MaxCapabilities`. Duplicates count toward
  the cap. 32 is far above the relations one resource has, and bounds the work per resource.

### Errors

A refused field is named by its path, with the resource's index in file order:
`resources[3].relations[0].target`, `resources[1].size.vcpus`. The domain returns an
`*inventory.FieldError` with that path, wrapping its sentinel (`ErrUnknownRelationType`,
`ErrUnknownRelationTarget`, `ErrSelfRelation`, `ErrInvalidSize`, `ErrInvalidUsage`,
`ErrFieldNotForCategory`, `ErrInvalidCost`, or `text.ErrInvalid` for a workload). The file reader quotes
the path and keeps the domain's quoted value. The in-memory reader of the HTTP API names the path and
the sentinel only, never the value.

### Why one version for four features

PRD #190's decision: sizing, cost, workloads and relations each need new resource fields, and each format
version is a reader, an ADR and a migration of the samples. One version for all of them costs that once.

## Alternatives considered

- **Supersede ADR 0006.** Version 2 keeps every rule of version 1 (strict keys, null policy, caps,
  domain validation, the reader's place in the architecture) and only adds fields. Superseding would
  restate all of that and leave two ADRs saying the same thing; amending keeps one source per rule.
- **A version per feature.** Four versions in one PRD, four reader changes and four sample migrations,
  and intermediate versions no writer ever produces. Refused for cost, as above.
- **An open relation vocabulary (any string).** Every later use of a relation (notes in the report,
  workload grouping) would have to decide what an unknown type means, and a typo (`replica_of`) would
  pass silently, which ADR 0006 refuses on principle. Adding a type to a closed vocabulary is a small,
  reviewed change.
- **A list of currency codes.** ISO 4217 changes, and the tool converts no currency (PRD #190 keeps
  currency conversion out of scope). The shape check catches a typo such as `usd` or `EURO`; a list
  would need updating for no gain.
- **Plain numbers with 0 for "not given".** Simpler types, but an estimate could not tell a free
  resource from an unknown one, which "never zero for unknown" forbids.
- **Defaults stored by the reader (count 1, hoursPerMonth 730).** The report must say when it assumed a
  value, which it cannot once the default is stored. The domain keeps "not given", and the milestone that
  uses the value applies the default.

## Consequences

- Version 1 files read exactly as before and build the same inventories as their version 2 twins without
  new fields. No report changes in milestone 1: the new fields are read and validated, not shown yet.
- The HTTP API accepts version 2 inventories with no change to its own code, because it hands the
  inventory's bytes to the same reader (`inventoryfile.FromBytes`).
- The reader imports `strconv` to read numbers (`inventory-file-reader-reads-one-file` allow list).
- Changing a range, the category table or the relation vocabulary changes what a file means: it needs
  a new ADR, and a new format version once `shift2eu discover` writes files (ADR 0011's policy).
- The samples under `examples/` stay version 1 until PRD #190 milestone 9 migrates them.
