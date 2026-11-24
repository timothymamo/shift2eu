# 0045. Add seven taxonomy categories, match cache plans on memory alone, and keep every format version

- Status: Accepted; amends [0025](0025-json-inventory-file-format-v2.md) (the size-field table) and
  [0027](0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md) (memory-only plan matching
  for `cache.redis`, the reason `not-priced`)
- Date: 2026-11-24

## Context

PRD #552 ([prds/552-more-taxonomy-categories.md](../../prds/552-more-taxonomy-categories.md)) adds the
taxonomy categories that common source services need. Today the taxonomy has nine categories, and every
other resource is `unclassified`, fit `none` on every target: caches, MongoDB-compatible databases, load
balancers, topics, serverless containers, container registries and file shares show up as blockers
although OVHcloud, Scaleway and UpCloud often serve them, and a GCP Pub/Sub topic is classified
`messaging.queue` and maps `direct` to a queue that gives each message to one consumer.

The taxonomy is the only bridge between sources and targets, and no live source adapter exists yet: a
category added now costs one guide and three catalogs, added later also every adapter and an inventory
format version ([ADR 0025](0025-json-inventory-file-format-v2.md), *Consequences*). The maintainer decided
the PRD's eleven open questions on 2026-11-24, as recommended.

Four things need deciding beyond the category names: which capabilities the new categories need, which
size fields each takes (ADR 0025 asks for a new ADR when that table changes), how a cache is matched to a
plan (ADR 0027 matches every plan on vCPUs and memory), and what a category that is never priced but has
a fit is "not estimated" for. And whether any of it changes a format version.

Before this ADR, milestone 1 read the three targets' cache documentation for a vCPU count per plan
(2026-11-24, public pages, read-only):

| Target | vCPUs per plan | Page |
|---|---|---|
| OVHcloud (Valkey) | Given on the price page: a `vCore` column per node, e.g. Discovery B3-4: 4 GB, 1 vCore; Production B3-8: 8 GB, 2 vCores. Not on the capabilities page, which refers to the price page | <https://www.ovhcloud.com/en-ie/public-cloud/prices/> (`#valkey`), <https://docs.ovhcloud.com/en/guides/public-cloud/databases/valkey-capabilities> |
| Scaleway (Redis™, RED1-micro to RED1-XL) | Page not reachable: HTTP 403 to an automated client | <https://www.scaleway.com/en/docs/managed-databases-for-redis/concepts/>, `faq/`, `how-to/create-a-database-for-redis/` |
| UpCloud (Managed Valkey) | Given: a "CPU cores" column per plan, e.g. `1x1xCPU-1GB`: 1 GiB, 1 core; `2x4xCPU-28GB`: 28 GiB, 4 cores | <https://upcloud.com/docs/products/managed-valkey/configurations/> |

Scaleway's vCPUs cannot be confirmed, so a cache plan whose vCPU count is not given must be possible
without a milestone inventing one; `catalog.Plan` today requires at least 1 vCPU
(`internal/domain/catalog/plan.go`).

## Decision

Add seven categories, two capabilities and a widened third; give the new categories their size fields;
match a `cache.redis` resource to a plan on memory alone, with a plan's vCPU count optional for it; add
the not-estimated reason `not-priced`, Go identifier `estimate.NeverPriced`; and change no format
version.

### The categories

`internal/domain/taxonomy` gains these values, each with this definition as its doc comment:

| Category | Definition |
|---|---|
| `cache.redis` | An in-memory key-value store the provider runs that speaks the Redis protocol: Redis OSS or its fork Valkey |
| `database.mongodb` | A document database the provider runs that speaks the MongoDB wire protocol |
| `network.loadbalancer` | A load balancer the provider runs in front of a set of backends, at layer 4 (TCP, and UDP where the capability `udp` says so) or layer 7 (HTTP/HTTPS) |
| `messaging.topic` | A publish-subscribe topic the provider runs: every subscription gets its own copy of each message |
| `serverless.container` | A container image the provider runs and scales with requests; it may scale to zero when idle |
| `registry.container` | A private registry of OCI container images the provider runs |
| `storage.file` | A shared file system the provider runs, mounted by several machines over NFS |

Categories are named after the protocol where an application's client library decides what it can move
to (`cache.redis`, `database.mongodb`, as `database.postgresql`), and after the kind of product where the
move rewrites configuration, not code (the other five, as `messaging.queue`). The new families `cache`,
`network` and `registry` follow `<family>.<kind>`. The nine existing categories keep their meaning;
`taxonomy.ParseCategory` still refuses any other value with "unknown taxonomy category".

### The capabilities

- **`sharding`**, new: "data is partitioned across several primary nodes that clients address". Used by
  `cache.redis` (a Redis Cluster client cannot use a non-clustered cache); available to
  `database.mongodb` later.
- **`udp`**, new: "forwards UDP datagrams". Used by `network.loadbalancer`: OVHcloud's load balancer has
  UDP listeners, Scaleway's does TCP and HTTP/HTTPS only, UpCloud documents `tcp` and `http` modes. TCP
  versus HTTP needs no capability: every target load balancer does both.
- **`snapshots`**, widened from "taking point-in-time copies of a volume or instance" to "taking
  point-in-time copies of a volume, instance, file share or data set", so a file share snapshot and a
  cache's RDB snapshot count. Nothing changes for the existing categories.

No other capability is added; the new categories reuse `node-high-availability`,
`zone-high-availability`, `encryption-at-rest`, `read-replicas`, `point-in-time-restore`, `ipv6` and
`dead-letter-queue` with their current definitions (ADR 0011). `read-replicas` stays a database
capability and `ipv6` keeps its inbound-and-outbound meaning.

### The size fields (amends ADR 0025)

ADR 0025's table *Which fields each category takes* gains these rows; every other row is unchanged. A
field a row says "no" to is refused with `inventory.ErrFieldNotForCategory`, named by its path, as today.

| Category | `vcpus` | `memoryGiB` | `storageGiB` | `count` | `usage.hoursPerMonth` |
|---|---|---|---|---|---|
| `cache.redis` | no | yes (per node) | no | yes (nodes) | yes |
| `database.mongodb` | yes | yes | yes | yes (nodes) | yes |
| `storage.file` | no | no | yes (provisioned GiB) | no | no |
| `network.loadbalancer`, `messaging.topic`, `serverless.container`, `registry.container` | no | no | no | no | no |

`count` on a cache or a MongoDB service is its nodes: a replicated cache group's primaries and replicas
of every shard, a DocumentDB cluster's instances.

### How each new category is priced (amends ADR 0027)

`estimate.PricingOf` and ADR 0027's table *How each category is priced* gain:

| Category | Method | Needs | Estimate |
|---|---|---|---|
| `cache.redis` | `ByPlan`, matched on memory alone | `memoryGiB` | plan × count × hours / 730 (capped as ADR 0029 says) + fee × hours / 730; no storage |
| `database.mongodb` | `ByPlan` | `vcpus` and `memoryGiB` | as `database.postgresql`: plan × count × hours / 730 + max(0, storageGiB − included) × per-GiB + fee × hours / 730 |
| `storage.file` | `ByStorage` | `storageGiB` | storageGiB × per-GiB, as `storage.object` |
| `messaging.topic`, `serverless.container` | `ByUsage` | — | not estimated: `usage-priced` |
| `network.loadbalancer`, `registry.container` | `NotPriced` | — | not estimated: `not-priced` |

A plan's price in a snapshot is per node; where a target's page prices a whole multi-node service, the
snapshot converts it to the per-node price and its `SOURCES.md` section says so (as for databases today).
The storage of `database.mongodb` is counted once, not × `count`, as for the other databases
(`storedOnce`).

**Memory-only matching for `cache.redis`.** Every source sizes a cache by memory, every target sells
cache plans by memory, and a cache's vCPU count is rarely known; matching on vCPUs too would leave
almost every cache "size unknown". For a `cache.redis` resource, and only for it:

- the resource needs `memoryGiB`; without it, or with it NaN, infinite or negative, it is "not
  estimated: no size" (`no-size`). `vcpus` cannot be given (the field table).
- a plan **covers** the resource when `plan.MemoryMilliGiB ≥ memoryGiB × 1000`, exactly as a rational.
  The plan's vCPUs are ignored, whether given or not.
- among the covering plans with a price in the estimate region, the chosen one is the cheapest, ties
  broken by less memory, then plan name, then offering name (byte order); vCPUs take no part.
- with covering plans but none priced in the region, `no-price` names the first covering plan in the
  order less memory, plan name, offering name.
- with no covering plan, `no-plan-large-enough` names as `Largest` the plan with most memory, then the
  smaller plan name, then the smaller offering name.
- `count` absent is 1 with "(count assumed 1)", and `hoursPerMonth` absent is 730, as for every
  plan-priced category. No storage is added, and `StorageNotIncluded` is always "" for `cache.redis`.

Every other plan-priced category keeps ADR 0027's rule unchanged: vCPUs and memory both cover, ties and
order with vCPUs first.

### The cache plan format: a vCPU count is optional on a memory-matched offering

A plan whose target page gives no vCPU count is written without one; no milestone invents a count:

- **Domain.** `catalog.Plan.VCPUs` of 0 means "not given by the target". `catalog.New` accepts a plan
  with `VCPUs` 0 only on an offering whose every category is a memory-matched category; the only one is
  `cache.redis`. On any other offering, including one that serves `cache.redis` and another category,
  `VCPUs` 0 is refused with `catalog.ErrInvalidPlan`, as today ("vcpus 0, want at least 1"). A negative
  count is refused on every offering, and `VCPUs` above `MaxPlanVCPUs` too. A cache plan whose page does
  give vCPUs records them (1 to `MaxPlanVCPUs`), and they are shown but not matched on. `MemoryMilliGiB`
  stays required on every plan (at least 1). A cache plan's `IncludedStorageGiB` is 0.
- **Snapshot (format v2).** A plan's `vcpus` key becomes optional. Absent, the plan's `VCPUs` is 0 and
  the domain rule above decides. Present, it must be an integer of at least 1: a present `0` (or `-0`)
  is refused by the snapshot reader, naming `offerings[i].plans[j].vcpus`, so "not given" has exactly
  one spelling. Every other key and rule of the format stays as it is.
- **Reports.** The Markdown report writes a plan without vCPUs by its memory alone, "8 GiB" (with
  vCPUs, "2 vCPU, 8 GiB" as today). The HTML report shows a plan by its name alone since
  [issue #373](https://github.com/timothymamo/shift2eu/issues/373) (`htmlreport` `renderPlan`), with
  no size for any plan, so it needs no text for a plan without vCPUs. The JSON report writes
  `"vcpus": 0` for it, which every schema that has a plan (versions 2 and 3) already allows
  (`minimum` 0, required); the version 3 schema files' descriptions of `vcpus` say that 0 means the
  target gives no vCPU count, for a plan matched on memory alone (the version 1 and 2 files are frozen,
  see *No format version changes*).

This is how OVHcloud and UpCloud cache plans can carry their documented vCPUs, and Scaleway's can carry
none until a page that states them is read (milestone 6).

### The reason `not-priced`

A category with a fit that is never priced (`estimate.NotPriced`: `network.loadbalancer`,
`registry.container`, and any category the package does not know) gets the new reason, not
`usage-priced`, which would be wrong for a load balancer:

- Go: `estimate.NeverPriced Reason = "not-priced"`. The name differs from the existing Method
  `estimate.NotPriced` (also of value `"not-priced"`, a different type), which keeps its name and value.
- JSON report: code `not-priced`. Markdown and HTML: "not priced" ("not estimated: not priced").
- It sits where `usage-priced` sits in ADR 0027's order: after `no-fit`, before `no-size`. `ByUsage`
  categories keep `usage-priced`. `unclassified` still always gets `no-fit`, its fit being `none`.
- The version 3 JSON schemas' descriptions of the reason set list `not-priced` (the version 1 and 2
  files are frozen, see *No format version changes*); none is counted as 0 in a total.

### No format version changes

- **Inventory stays at schema version 2.** ADR 0006 and ADR 0007 leave the valid category and capability
  values to the domain, and `taxonomy.ParseCategory` accepts a new value in a version 1 or 2 file, as
  `compute.baremetal` was added ([ADR 0023](0023-separate-bare-metal-servers-from-virtual-machines-in-the-taxonomy.md)).
  The field table gains rows for new categories only, so no existing file changes meaning; ADR 0025 asks
  for a new version only once `shift2eu discover` writes files, which it does not yet. An older binary
  refuses an inventory that uses a new category with "unknown taxonomy category".
- **Catalog snapshots stay at format version 2.** Offerings name categories the domain validates. The
  one change, an optional `vcpus` on a plan, makes no existing snapshot invalid or change meaning (every
  plan in one gives `vcpus`), and snapshots are embedded in the binary that reads them, so no older
  reader meets a new one.
- **The JSON report keeps `schemaVersion` 1, 2 and 3.** `category` is a string with no `enum`, and ADR
  0023's new category kept the version. The estimate reason is an open set ("A consumer must accept a
  reason it does not know"), and [ADR 0018](0018-version-the-json-report-format-for-the-cli-and-the-api.md)
  keeps the version for a new value of an open set, with the schema files' descriptions updated in the
  same change. `"vcpus": 0` is within the schemas' range. No key is added, removed or retyped.
- **Only the version 3 schema files change, and only in two descriptions.** `assessment-v3.schema.json`
  and `comparison-v3.schema.json` list `not-priced` in `$defs/resourceEstimate.reason` and say in
  `$defs/plan.vcpus` that 0 means the target gives no vCPU count. The version 1 and 2 files are frozen
  by digest (`TestFrozenSchemaFilesAreUnchanged`, [issue #293](https://github.com/timothymamo/shift2eu/issues/293)),
  and stay byte for byte as they shipped. That is consistent with ADR 0018: its rule to update the
  schema files with a new open-set value predates its 2026-11-19 amendment, which froze versions 1 and
  2 so a reader of an old document finds the schema it was published with, and put new wording in
  version 3 only, as [issue #285](https://github.com/timothymamo/shift2eu/issues/285) did for
  `breakdown.storage`. A version 2 document (`--schema-version 2`) can now carry reason `not-priced` and
  `"vcpus": 0`; both validate against the frozen version 2 files (`reason` is a string without `enum`,
  `vcpus` an integer of `minimum` 0), and their consumers get the open-set rule those files already
  state, "A consumer must accept a reason it does not know". Version 1 documents carry no estimate, so
  neither value reaches them.

## Alternatives considered

- **Match caches on vCPUs and memory, as every other plan.** One rule, but sources give a cache's memory
  and almost never its vCPUs: nearly every cache would be "not estimated: no size".
- **Require a vCPU count on every plan, and leave Scaleway's cache unpriced until a page gives one.**
  Keeps `catalog.Plan` as it is, but blocks a priced offering on a figure the matching never uses.
- **Write `"vcpus": 0` in the snapshot for "not given".** No reader change, but a `0` reads as a fact
  and a typo of it passes; an absent key is how the inventory says "unknown" too.
- **Make vCPUs optional on every plan.** A compute or database plan without vCPUs could never cover a
  resource, and a missing figure there is a snapshot mistake the reader should catch.
- **Name the reason `estimate.NotPricedReason`, or rename the Method.** The first reads as a suffix
  workaround; renaming the Method touches every caller for no gain. `NeverPriced` says what it is.
- **Reuse `usage-priced` for load balancers and registries.** Wrong: they are billed per size or plan,
  not per use.
- **One category per protocol fork (`cache.valkey`, `cache.redis`), or `cache.inmemory` with
  Memcached.** The first gives a Redis cache fit `none` on two targets that serve it; the second maps
  Memcached `direct` to a product that cannot serve it.
- **Bump the inventory, snapshot or JSON report version.** Nothing an existing reader or file relies on
  changes meaning; a version would cost every consumer for no protection.

## Consequences

- `internal/domain/taxonomy` lists seventeen categories (the sixteen an offering can serve, the nine
  existing and the seven new, and `unclassified`; `taxonomy.Categories()` returns all seventeen) and
  twelve capabilities; `internal/domain/inventory`
  holds the new field rows; `internal/domain/estimate` gains memory-only matching, `storage.file` per GiB,
  `database.mongodb` by plan and `NeverPriced`; `internal/domain/catalog` accepts a plan without vCPUs on
  a `cache.redis`-only offering (the memory-matched categories are one list behind
  `catalog.MatchesOnMemory`, which the estimate reads too); the snapshot reader accepts an absent
  `vcpus`; the three reports render `not-priced`, and the Markdown and JSON reports a plan without
  vCPUs.
- Plan matching now has two rules in the domain; a later memory-matched category joins `cache.redis`'s
  rule by being added to one list in the domain.
- Until a target's catalog milestone lands, its report shows the new categories as fit `none` with "no
  offering in the <target> catalog for `<category>`".
- Minimum billed sizes and fixed node counts (UpCloud File Storage from 250 GiB, OVHcloud's 2-node
  Production Valkey) are not in snapshot format v2: a resource below or above them is priced at its own
  size and count. Each is recorded in `SOURCES.md` and the guide, and one follow-up issue covers them.
- The milestone-6 Scaleway catalog may write Redis™ plans without `vcpus`; once a reachable page states
  them, they are added.
