# 0047. Add search.opensearch and messaging.kafka, price their storage per node, and keep every format version

- Status: Accepted; amends [0025](0025-json-inventory-file-format-v2.md) (the size-field table) and
  [0027](0027-match-plans-and-estimate-monthly-prices-in-a-domain-package.md) (storage per node × count
  for the two new plan-priced categories)
- Date: 2026-11-25

## Context

PRD #571 ([prds/571-opensearch-kafka-categories.md](../../prds/571-opensearch-kafka-categories.md))
takes up the two candidates [ADR 0046](0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md)
left for a follow-up: managed OpenSearch and managed Kafka. Today both are `unclassified`, fit `none` on
every target, although OVHcloud runs both, UpCloud runs OpenSearch, and Scaleway documents both (their
general availability is not confirmed yet). The maintainer decided the PRD's six open questions on
2026-11-25, as recommended.

As for ADR 0046, now is the cheap time: no live source adapter exists, so a category costs one guide and
three catalogs, and later also every adapter and an inventory format version
([ADR 0025](0025-json-inventory-file-format-v2.md), *Consequences*).

Beyond the names, three things need deciding: where each category's boundary lies (which source
services count), which size fields each takes (ADR 0025 asks for a new ADR when that table changes), and
how its storage is priced: ADR 0027 counts a managed database's storage once, as the service's data,
and a compute or Kubernetes resource's per node. And whether any of it changes a format version.

## Decision

Add two taxonomy categories, `search.opensearch` and `messaging.kafka`, both priced by plan on vCPUs and
memory, with their storage per node × `count`. No new capability, and no format version changes.

### The categories

Both are named after the protocol an application's client speaks, as `cache.redis` and
`database.mongodb` are: the client library decides what an application can move to.

| Category | Definition (the taxonomy's doc comment) |
|---|---|
| `search.opensearch` | A search and analytics engine the provider runs that speaks the OpenSearch REST API: OpenSearch, or Elasticsearch up to 7.10, from which OpenSearch was forked |
| `messaging.kafka` | A cluster of brokers the provider runs that speaks the Apache Kafka protocol: a partitioned, replicated log whose consumers track their own offsets |

`search.opensearch` opens a new family, `search`: on the sources the product is sold as search and
analytics, not as a system of record a `database.*` move would suggest. `messaging.kafka` joins the
`messaging` family and stays distinct from `messaging.queue` (one consumer per message, acknowledged)
and `messaging.topic` (a copy per subscription): a Kafka topic is a retained, partitioned log replayed
by offset.

### The boundaries

- **Elasticsearch 7.x is in, before 7.0 is out.** OpenSearch is a fork of Elasticsearch 7.10.2 and keeps
  its REST API, so a client using the typeless 7.x API speaks it unchanged. 7.0 removed mapping types: a
  client written against the 6.x API needs a change, and `direct` would hide a rewrite, so a domain on
  `Elasticsearch_1.5` to `Elasticsearch_6.8` stays `unclassified`. The boundary rests on the client and
  REST API break alone, not on upgrades or index compatibility: a move is a new cluster and a reindex or
  snapshot restore whatever the version. Beyond the boundary the engine version is not assessed, as for
  the databases.
  - *Caveat:* a 7.x client that still uses typed endpoints or `include_type_name` (deprecated in 7.x,
    removed in OpenSearch 2.x and 3.x) needs a change before it runs on a current OpenSearch target, and
    the official Elastic clients from 7.14 on refuse a server that is not Elasticsearch. The
    classification cannot see either; the guide's warning (PRD #571 milestones 2 to 4) states both.
- **Serverless variants are in, unsized.** An OpenSearch Serverless collection and an MSK Serverless
  cluster speak their protocol, so their clients move to a target cluster. They carry no node size, so
  their estimate is "not estimated: size unknown"; a usage path for a plan-priced category would need an
  estimate method per resource, which ADR 0027 does not have.
- **Vendor-run services are out.** Elastic Cloud, Confluent Cloud, Aiven or Instaclustr through a
  marketplace are another vendor's service, under its own account and bill, not a resource of the
  source: they stay `unclassified` where the source lists them, and Elasticsearch after 7.10 is not
  served by OpenSearch anyway.
- **Event Hubs stays `unclassified`, with a hand rule.** An Event Hubs namespace can expose the Kafka
  protocol, but most of its clients use AMQP or the Event Hubs SDKs, and no read-only call says which
  protocol they use. Classified `messaging.kafka` by the program, an AMQP client would map `direct` to
  Kafka and hide a client rewrite. The guide says to set the category to `messaging.kafka` by hand when
  every client of the namespace uses the Kafka endpoint.
- Self-managed OpenSearch, Elasticsearch or Kafka on VMs or Kubernetes keeps the VM's or cluster's
  category; Azure AI Search, Kinesis, Pub/Sub, Kafka Connect, Schema Registry and OpenSearch Ingestion
  stay out (PRD #571, *What counts* and *Left out*).

**No new capability.** `node-high-availability`, `zone-high-availability`, `encryption-at-rest` and
`snapshots` cover the differences the targets document, with their current definitions; shards and
partitions are what the categories are. Where the guide derives `node-high-availability` from a node
count (an OpenSearch domain's data nodes, an MSK cluster's brokers, an HDInsight cluster's worker
nodes), the count alone does not prove it: an OpenSearch index needs a replica per shard and a Kafka
topic a replication factor of 2 or more to keep serving through the loss of a node. Whether the guide
can read those, or must ask for them by hand, is to be checked in milestones 2 to 4, the same caveat for
both categories.

### The size fields (amends ADR 0025)

ADR 0025's table *Which fields each category takes* gains these rows; every other row is unchanged.

| Category | `vcpus` | `memoryGiB` | `storageGiB` | `count` | `usage.hoursPerMonth` |
|---|---|---|---|---|---|
| `search.opensearch` | yes (per data node) | yes (per data node) | yes (per data node) | yes (data nodes) | yes |
| `messaging.kafka` | yes (per broker) | yes (per broker) | yes (per broker) | yes (brokers) | yes |

`count` on a search cluster is its data nodes, not its dedicated master nodes; on a Kafka cluster its
brokers.

### Storage per node (amends ADR 0027)

Both categories join `estimate.ByPlan` and are matched on vCPUs and memory, the rule of every
plan-priced category but `cache.redis`; neither joins `catalog.MatchesOnMemory`. The estimate is the
cheapest covering plan's per-node price × `count`, scaled by `hoursPerMonth`, plus
`max(0, storageGiB − included) × per-GiB × count`.

The storage is per node, × `count`, as for `compute.vm` and `kubernetes.managed`, not stored once as
for the managed databases (`storedOnce` stays theirs): a search or Kafka cluster shards its data across
its nodes, and each source sizes storage per node (OpenSearch Service `EBSOptions.VolumeSize`, MSK's
per-broker EBS volume), as Scaleway's create pages do. A plan in a snapshot is one node, at its per-node
price; where a page prices a whole cluster, the snapshot converts it and `SOURCES.md` says so, as for
caches. A count below or above a tier's fixed node count is priced at `count` (PRD #552's follow-up issue
on fixed and minimum node counts covers these tiers too).

No not-estimated reason is added: every case is an existing one.

### No format version changes

- **Inventory stays at schema version 2.** `taxonomy.ParseCategory` accepts a new value in a version 1 or
  2 file, as for ADR 0023 and ADR 0046. The field table gains rows for new categories only, so no
  existing file changes meaning; ADR 0025 asks for a new version only once `shift2eu discover` writes
  files. An older binary refuses an inventory that uses a new category with "unknown taxonomy
  category".
- **Catalog snapshots stay at format version 2.** Offerings name categories the domain validates
  ([ADR 0007](0007-shared-target-catalog-snapshot-format-v1.md)), and both categories' plans carry vCPUs,
  memory and included storage as database plans do
  ([ADR 0024](0024-add-plans-prices-and-a-default-region-in-target-snapshot-format-v2.md)).
- **The JSON report keeps `schemaVersion` 1, 2 and 3, and no schema file changes.** `category` is a
  string with no `enum` ([ADR 0018](0018-version-the-json-report-format-for-the-cli-and-the-api.md),
  ADR 0023); no estimate reason or key is added, removed or retyped; the frozen version 1 and 2 files
  are untouched, and no version 3 description names the categories (only `$defs/plan.vcpus` names
  `cache.redis`, unchanged).

## Alternatives considered

- **`search.elasticsearch`, or `database.opensearch`.** None of the three targets runs Elasticsearch,
  and Elasticsearch after 7.10 is not served by OpenSearch; `database` suggests a system of record and
  follows the targets' menus rather than what the workload is.
- **`streaming.kafka`.** A new family for one category, with Kinesis, the other streaming product, out.
- **Elasticsearch 6.x in as well.** It maps `direct` while a client needs a change for mapping types.
- **Event Hubs namespaces as `messaging.kafka` by default.** Maps AMQP clients `direct` to Kafka.
- **Leave serverless variants `unclassified`.** Shows a blocker where a target serves the client.
- **Storage stored once, as for the databases.** Understates a sharded cluster's storage by its node
  count; every source and target sizes it per node.
- **Capabilities `tiered-storage` (Kafka) and `k-nn` (OpenSearch).** Every target's OpenSearch enables
  k-NN or does not say, and no target documents Kafka tiered storage: a capability every offering lists,
  or none does, only blurs the vocabulary.
- **Bump the inventory, snapshot or JSON report version.** Nothing an existing reader or file relies on
  changes meaning.

## Consequences

- `internal/domain/taxonomy` lists nineteen categories (eighteen an offering can serve and
  `unclassified`); `internal/domain/inventory` holds the two new field rows; `internal/domain/estimate`
  prices both by plan with storage per node.
- Until a target's catalog milestone lands (PRD #571 milestones 5 to 7), its report shows the new
  categories as fit `none` with "no offering in the <target> catalog for `<category>`". UpCloud never
  serves `messaging.kafka` today, and Scaleway serves neither until its general availability is
  confirmed.
- Until the guide's milestones land (2 to 4), the programs list these resources as `unclassified`, and a
  user sets the category by hand.
- A `zone-high-availability` Kafka resource, the common case, maps `partial` to a 1-AZ target plan and
  `direct` only to a 3-AZ or Multi-AZ variant where one is an offering.
