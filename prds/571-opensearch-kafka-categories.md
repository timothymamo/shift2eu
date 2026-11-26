# PRD #571: OpenSearch and Kafka as taxonomy categories

**Status:** In progress, milestones 1 and 2 done. The maintainer decided all six questions under *Decisions and
decided questions* on 2026-11-25, as recommended. Milestone 1's ADR is
[0047](../docs/adr/0047-add-search-opensearch-and-messaging-kafka-and-price-their-storage-per-node.md).
**Issue:** #571

## Problem

PRD #552 ([prds/552-more-taxonomy-categories.md](552-more-taxonomy-categories.md)) added seven taxonomy
categories ([ADR 0046](../docs/adr/0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md)),
and its decided question 1 left two well-bounded candidates for a follow-up: managed OpenSearch and
managed Kafka. Today both are `unclassified`, so fit `none` on every target, by definition:

- the inventory guide (`docs/guide/inventory.md`, *AWS classification*, *GCP classification*, *Azure
  classification*, and `docs/guide/inventory/{aws,gcp,azure}.jq`) makes no call for Amazon OpenSearch
  Service, Amazon MSK, Google Cloud Managed Service for Apache Kafka or HDInsight Kafka. They reach the
  inventory only through AWS Config, Cloud Asset Inventory or Resource Graph, as `unclassified` rows with
  no size and no capability;
- OVHcloud runs both (Public Cloud Databases for OpenSearch, Public Cloud Analytics for Kafka), UpCloud
  runs OpenSearch, and Scaleway documents both. A platform engineer whose workload uses a search cluster
  or a Kafka cluster sees a blocker where a target has a like-for-like product, and the report cannot
  name it or price it;
- OVHcloud's `SOURCES.md` already lists OpenSearch and Kafka under *Products not in the snapshot* ("no
  matching taxonomy category"), and UpCloud's lists Managed OpenSearch the same way.

The maintainer asked for this PRD now (2026-11-25). As PRD #552 said, now is cheaper than later: no
live source adapter exists, so a category costs one guide and three catalogs today, and every adapter
and an inventory format version once `shift2eu discover` exists (ADR 0025, *Consequences*).

## Outcome

A user who builds an inventory with the guide gets two more taxonomy categories, `search.opensearch`
and `messaging.kafka`. Their Amazon OpenSearch Service domains and OpenSearch Serverless collections are
`search.opensearch`; their MSK clusters, Google Cloud Managed Service for Apache Kafka clusters and
HDInsight Kafka clusters are `messaging.kafka`. `shift2eu assess` maps them to the OVHcloud, UpCloud and
(once its general availability is confirmed) Scaleway offerings that serve them, with fit `direct` or
`partial` and the missing capabilities named, or fit `none` with "no offering in the <target> catalog
for `<category>`". Both categories are priced by plan, per node, with storage per node above what a
plan includes, from each snapshot's dated prices. Every target offering comes from public documentation
cited in its `SOURCES.md`. The inventory format stays at version 2, the catalog snapshot format at
version 2, and the JSON report keeps its `schemaVersion`s.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | None live. The inventory file (schema versions 1 and 2) and the inventory guide's commands, jq programs and tables for **AWS**, **GCP** and **Azure** | Live discovery (`shift2eu discover`) |
| Target providers | **OVHcloud**, **Scaleway** and **UpCloud**: new offerings in their embedded snapshots, with sources, plans and prices | New targets. Live catalog or pricing APIs |
| Taxonomy categories | New: `search.opensearch`, `messaging.kafka`. The sixteen existing categories keep their meaning | Everything under *Left out*, among them Kinesis, Pub/Sub, Event Hubs without its Kafka endpoint, Azure AI Search, Elastic Cloud, Kafka Connect and Schema Registry as resources. DNS and other global services (a separate PRD) |
| Capabilities | The existing `node-high-availability`, `zone-high-availability`, `encryption-at-rest` and `snapshots`, reused with their current definitions | Any new capability (decided question 4: none) |
| Inventory | Schema version 2, with the size fields the new categories take (*Estimates*) | A schema version 3 |
| Assessment and output | The current fit rules; plan estimates for both categories; Markdown, JSON and the site's HTML report | A JSON report `schemaVersion` bump. Minimum billed sizes and fixed node counts (PRD #552's follow-up issue covers them). Usage pricing (MSK Serverless, OpenSearch Serverless OCUs) |

### The new categories

Each definition is what the taxonomy's doc comment will say (`internal/domain/taxonomy/category.go`).

| Category | Definition | OVHcloud | Scaleway | UpCloud |
|---|---|---|---|---|
| `search.opensearch` | A search and analytics engine the provider runs that speaks the OpenSearch REST API: OpenSearch, or Elasticsearch up to 7.10, from which OpenSearch was forked | Public Cloud Databases for OpenSearch (GA) | Cloud Essentials for OpenSearch (general availability not confirmed: decided question 2) | Managed OpenSearch (GA) |
| `messaging.kafka` | A cluster of brokers the provider runs that speaks the Apache Kafka protocol: a partitioned, replicated log whose consumers track their own offsets | Public Cloud Analytics for Kafka (GA) | Clusters for Apache Kafka® (general availability not confirmed: decided question 2) | none |

**Names.** Both are named after the protocol, as `cache.redis`, `database.postgresql` and
`database.mongodb` are (ADR 0046, *The categories*): an application's client library decides what it
can move to, and an OpenSearch or Kafka client moves to any engine that speaks the protocol.

- `search.opensearch`, confirmed. The family is new, `search`, not `database`: on the sources the product
  is sold as search and analytics (AWS lists OpenSearch Service under Analytics), and it is not a
  system of record a `database.*` move would suggest; PRD #552 kept `cache.redis` out of `database` for
  the same reason. OVHcloud, Scaleway and UpCloud file it under their databases, but the category names
  what the workload is, not a target's menu. Not `search.elasticsearch`: none of the three targets runs
  Elasticsearch, and Elasticsearch after 7.10 (Elastic License, new APIs) is not served by OpenSearch.
- `messaging.kafka`, confirmed. It joins the existing `messaging` family (`messaging.queue`,
  `messaging.topic`): users and every source look for Kafka under messaging or streaming, and OVHcloud's
  menu calls it a "Queuing solution for event-driven architectures". It stays distinct from both: a
  Kafka topic is a retained, partitioned log replayed by offset, not a queue (one consumer per message,
  acknowledged, dead-lettered: OVHcloud `SOURCES.md`, `messaging.queue` entry) and not a push topic (a
  copy per subscription). Not `streaming.kafka`: a new family for one category, and Kinesis, the other
  streaming product, stays out.

**What counts, and what does not.**

| Source service | Category | Why |
|---|---|---|
| Amazon OpenSearch Service domain, engine `OpenSearch_*` | `search.opensearch` | It is OpenSearch |
| Amazon OpenSearch Service domain, engine `Elasticsearch_7.*` | `search.opensearch` | OpenSearch is a fork of Elasticsearch 7.10.2 and keeps its REST API: a client using the typeless 7.x API speaks it unchanged. A 7.x client that still uses typed endpoints or `include_type_name` (deprecated in 7.x, removed in OpenSearch 2.x and 3.x) needs a change; the guide's warning (milestones 2 to 4) states it |
| Amazon OpenSearch Service domain, engine `Elasticsearch_1.5` to `Elasticsearch_6.8` | `unclassified` | The client and REST API break: 7.0 removed mapping types, so a client written against the 6.x API needs a change before it runs on OpenSearch, and `direct` would hide a rewrite |
| OpenSearch Serverless collection (`AWS::OpenSearchServerless::Collection`) | `search.opensearch` | It speaks the OpenSearch API (a subset of it), so every OpenSearch client that works against it works against a provisioned target cluster; no size (*Estimates*) |
| Amazon MSK provisioned cluster | `messaging.kafka` | It is Apache Kafka |
| Amazon MSK Serverless cluster (`AWS::MSK::ServerlessCluster`) | `messaging.kafka` | It speaks the Kafka protocol (IAM authentication only, which the move replaces); no size |
| Google Cloud Managed Service for Apache Kafka cluster (`managedkafka.googleapis.com/Cluster`) | `messaging.kafka` | It is Apache Kafka |
| Azure HDInsight cluster, kind `KAFKA` | `messaging.kafka` | It is Apache Kafka on VMs the provider runs |
| Azure Event Hubs namespace with its Kafka endpoint | `unclassified`, with a hand rule in the guide | Decided question 1 (2026-11-25): the namespace exposes the Kafka protocol, but most Event Hubs clients use AMQP or the Event Hubs SDKs, and no read-only call says which protocol the clients use. Classified `messaging.kafka` by the program, an AMQP client would map `direct` to Kafka and hide a client rewrite. The guide says to change the category by hand when every client of the namespace uses the Kafka endpoint |
| Confluent Cloud, Elastic Cloud, Aiven or Instaclustr through a marketplace (Elastic on Azure `Microsoft.Elastic/monitors`, Confluent on Azure `Microsoft.Confluent/organizations`, GCP Marketplace) | `unclassified`, or not in the inventory | Another vendor runs the service, under its own account and bill: it is not a resource of the source, and the vendor may run in the EU already. Where the source lists it (Azure's resource types), it stays `unclassified`; GCP Marketplace SaaS is not an asset at all |
| Self-managed OpenSearch, Elasticsearch or Kafka on VMs or Kubernetes | the VM's or cluster's category | The source sees VMs and clusters, not the software; they map as `compute.vm` or `kubernetes.managed` today |
| Azure AI Search (`Microsoft.Search/searchServices`) | `unclassified` | Its own REST API and index model, not OpenSearch's |
| Kinesis Data Streams, Pub/Sub, Event Hubs without the Kafka protocol (Basic tier), EventBridge, Event Grid | unchanged (`unclassified`, or `messaging.topic` / `messaging.queue` for Pub/Sub as PRD #552 decided) | Other protocols; no target equivalent (PRD #552, *Left out*) |
| MSK Connect connectors, Kafka Connect clusters (`AWS::KafkaConnect::Connector`, `managedkafka.googleapis.com/ConnectCluster`), Schema Registry, OpenSearch Ingestion pipelines | `unclassified` | Integration runtimes, not clusters; OVHcloud's Kafka Connect could serve a later category |

The 6.x/7.x boundary rests on the client and REST API break alone, not on upgrades or index
compatibility: a move to a target is a new cluster and a reindex or snapshot restore whatever the
version (OpenSearch 3.x cannot read 7.x indices either, and AWS upgrades a 6.8 domain to OpenSearch
1.x). Beyond that boundary the engine version is not assessed, as for the databases: a domain on
`Elasticsearch_7.1` and one on `OpenSearch_2.19` map alike. The guide warns that the official Elastic
clients from 7.14 on refuse to talk to a server that is not Elasticsearch, so an application on such a
client changes its client library (to an OpenSearch client or an earlier Elastic one) to move.

### The target offerings

Read on 2026-11-25 from public documentation, anonymously, no credentials: OVHcloud from
`ovh/ovhcloud-docs` (`develop`, commit `35618714a7c5cc7555f86349d5224bda7e0b70aa`, the source of
docs.ovhcloud.com), Scaleway from `scaleway/docs-content` (`main`, commit
`9cc4361c1ddcecee8cbf3a920e175f127ecb8bb9`) and `scaleway/scaleway-sdk-go` (default branch, commit
`161beef41adb308b9988fc1b289e3e169ab58cca`) because `www.scaleway.com` refuses automated clients, and
UpCloud from `upcloud.com/docs/`. The catalog milestones read each page again and cite it in
`SOURCES.md`.

- **`search.opensearch`**
  - **OVHcloud, GA.** OpenSearch 2.19 and 3.3; plans Essential/Discovery (1 node), Business/Production
    (3 nodes) and Enterprise/Advanced (6 nodes); "available in all Public Cloud 1-AZ and 3-AZ regions",
    and in 3-AZ regions only Discovery, Production and Advanced; hourly and daily backups on every plan
    (retention 3, 14 and 30 days), "Point In Time Recovery (PITR) is not available for OpenSearch";
    node types and prices on the prices page.
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/opensearch-capabilities>.
    High availability on Production and Advanced, none on Discovery (plan table):
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>. 3-AZ: "Multi-node database
    service is deployed across three independent availability zones, ensuring service continuity even
    if one zone experiences an outage":
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>.
    Encryption at rest: "Are the nodes and the backups encrypted at rest? Yes, on every plan":
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview>.
  - **Scaleway, general availability not confirmed.** Cloud Essentials for OpenSearch exists in the
    documentation (published 2025-08-05, validated 2025-12-16): deployment configurations High
    Availability ("fault tolerance in case of node failure") and Standalone ("without redundancy"),
    shared and dedicated vCPU node types, storage per node; changelog entries on 2026-07-01 (scale up
    "without downtime") and 2026-07-22 (node count and disk size). No changelog entry announces a
    public beta or general availability, and no page says either. The SDK's API is `searchdb/v1alpha1`,
    in `fr-par` only (`api/searchdb/v1alpha1/searchdb_sdk.go`, `Regions()`).
    <https://www.scaleway.com/en/docs/opensearch/faq/>,
    <https://www.scaleway.com/en/docs/opensearch/how-to/create-opensearch-deployment/>,
    <https://www.scaleway.com/en/docs/opensearch/reference-content/shared-vs-dedicated-resources/>.
    No page states encryption at rest, backups or zone redundancy.
  - **UpCloud, GA** (no page marks it beta, whereas Container Registry's says "open beta"). Plans of 1,
    3, 6, 9, 12 or 15 nodes, 2 cores and 4, 8 or 14 GiB per node, storage bundled per plan (for example
    `3x2xCPU-8GB-240GB-3D`: 3 nodes, 8 GiB, 240 GiB), "priced according to the number of nodes and the
    resources allocated to each node"
    (<https://upcloud.com/docs/products/managed-opensearch/configurations/>); multi-node plans keep
    serving through the loss of a host, "nodes are created on physically separated backend hosts", "the
    remaining operational nodes continue to handle client requests without interruption", managed
    "automatically … in all plans", no zone is mentioned
    (<https://upcloud.com/docs/products/managed-opensearch/high-availability/>); encryption at rest with
    LUKS for instances and backups (<https://upcloud.com/docs/products/managed-opensearch/encryption/>);
    hourly and daily backups, no point-in-time restore
    (<https://upcloud.com/docs/products/managed-opensearch/backups/>); all eight declared EU zones
    (`de-fra1`, `dk-cph1`, `es-mad1`, `fi-hel1`, `fi-hel2`, `nl-ams1`, `pl-waw1`, `se-sto1`) among its
    fifteen (<https://upcloud.com/docs/products/managed-opensearch/availability/>).
- **`messaging.kafka`**
  - **OVHcloud, GA.** Kafka 3.9, 4.2 and 4.3; plans Production (Gen 3) / Business (Gen 2), 3 nodes, and
    Advanced / Enterprise, 6 nodes; "available in all Public Cloud 1-AZ and 3-AZ regions"; flexible
    storage; "We don't backup Kafka data"; Kafka Connect, MirrorMaker 2, Schema Registry and a REST API
    as options. <https://docs.ovhcloud.com/en/guides/public-cloud/databases/kafka-capabilities>. Whether
    the 3-AZ and encryption pages above, written for Public Cloud Databases, also cover Kafka, now under
    "Public Cloud Analytics", is to be checked (milestone 5).
  - **Scaleway, general availability not confirmed.** Clusters for Apache Kafka® exists in the
    documentation (posted 2025-09-15, validated 2026-06-19): 1 to 30 nodes, in multiples of 3 except
    one; Single-AZ and Multi-AZ ("replicate three nodes over two Availability Zones"; changelog
    2026-07-01: "Multi-AZ is now available … with automatic failover and zero event loss"); dedicated or
    shared nodes; storage per node; Private Networks only; one user per cluster; storage resize
    (changelog 2026-10-28). No changelog entry or page announces a public beta or general availability.
    The SDK's API is `kafka/v1alpha1`, in `fr-par` only. No page states encryption at rest.
    <https://www.scaleway.com/en/docs/clusters-for-kafka/faq/>,
    <https://www.scaleway.com/en/docs/clusters-for-kafka/concepts/>,
    <https://www.scaleway.com/en/docs/clusters-for-kafka/how-to/create-cluster/>.
  - **UpCloud: none.** Its product list has no Kafka, streaming or broker product; its managed databases
    are MySQL, PostgreSQL, Valkey and OpenSearch (<https://upcloud.com/docs/products/>).

A `v1alpha1` API alone does not mean beta: Scaleway File Storage is GA in Paris and Amsterdam (changelog
2026-10-21) on `file/v1alpha1`. But Scaleway announces every general availability in its changelog
(Managed MongoDB® on 2025-06-10, File Storage, Data Warehouse for ClickHouse® on 2026-03-26) and none
for these two. Under PRD #552's rule (a beta offering is not an offering, and a fact not confirmed stays
out of the snapshot) neither is an offering until a page states its general availability.

### How the sources classify

The `rawType` keeps the guide's conventions: the CloudFormation type on AWS, the Cloud Asset Inventory
asset type on GCP, the Resource Manager type on Azure. Rows marked *call* need a read-only service call
the guide does not make today.

| Category | AWS | GCP | Azure |
|---|---|---|---|
| `search.opensearch` | `AWS::OpenSearchService::Domain`, engine `OpenSearch_*` or `Elasticsearch_7.*` (*call*: `opensearch list-domain-names`, `opensearch describe-domains`, IAM `es:ListDomainNames`, `es:DescribeDomains`); `AWS::OpenSearchServerless::Collection` (*call*: `opensearchserverless list-collections`, `batch-get-collection`, IAM `aoss:ListCollections`, `aoss:BatchGetCollection`) | none (GCP runs no managed OpenSearch) | none (Azure runs no managed OpenSearch) |
| `messaging.kafka` | `AWS::MSK::Cluster` and `AWS::MSK::ServerlessCluster` (*call*: `kafka list-clusters-v2`, IAM `kafka:ListClustersV2`, which returns both types with their broker, storage and encryption settings) | `managedkafka.googleapis.com/Cluster` (in Cloud Asset Inventory's search API, which the guide already runs; *call* for sizes: `gcloud managed-kafka clusters list`, role `roles/managedkafka.viewer`) | `Microsoft.HDInsight/clusters`, `properties.clusterDefinition.kind` `KAFKA` (Resource Graph, already queried; the projection gains the properties read) |

AWS Config lists OpenSearch domains as `AWS::OpenSearch::Domain` and `AWS::Elasticsearch::Domain`
([resource type list](https://docs.aws.amazon.com/config/latest/developerguide/resource-config-reference.html)),
not as the CloudFormation type `AWS::OpenSearchService::Domain`. The service call is what classifies the
domain and gives it its CloudFormation `rawType`; `aws.jq` drops both Config types, as it drops the
types the service calls already list. Milestone 2 checked the strings on 2026-11-26 (*To be checked*,
milestone 2).

**Only the fields the program reads.** Each new call keeps only the fields its jq program reads, with an
explicit `--query` (AWS), `--format` (gcloud) or Resource Graph `project` (Azure), so nothing else
reaches the raw output the guide keeps in its scratch directory (*Privacy*, `docs/guide/inventory.md`):

- OpenSearch Service: `DomainName`, `ARN`, `EngineVersion`, `ClusterConfig`, `EBSOptions`,
  `EncryptionAtRestOptions`;
- OpenSearch Serverless: `name`, `arn`, `type`, `standbyReplicas`;
- MSK: `ClusterName`, `ClusterType`, `ClusterArn`, the broker instance type, the broker count, the
  per-broker storage and the client subnets (MSK always encrypts at rest, so no encryption field is
  kept);
- the three ARNs (decided in milestone 2) for the region alone, as every other AWS resource's: the
  program keeps the ARN's region and never the ARN, whose account ID stays in the raw output;
- HDInsight: `clusterDefinition.kind`, the worker role's `targetInstanceCount` and
  `hardwareProfile.vmSize`, the zones and, if milestone 4 confirms they give `storageGiB`, the worker
  role's `dataDisksGroups` (disks per node, disk size);
- GCP Managed Kafka: likewise only the fields read (name, capacity, zones and encryption, as milestone 3
  confirms).

Never `AccessPolicies`, `AdvancedSecurityOptions`, `CognitoOptions`, `Endpoint`, `Endpoints`,
`DomainEndpointOptions`, a `ZookeeperConnectString*` or `BootstrapBroker*` key (MSK),
`collectionEndpoint` or `dashboardEndpoint` (OpenSearch Serverless), `bootstrapAddress` (GCP Managed
Kafka), `osProfile` or `connectivityEndpoints` (HDInsight).

**No data-plane or configuration access.** No data-plane permission and no configuration or credentials
call is used or granted: not `es:ESHttp*`, `aoss:APIAccessAll`, `kafka-cluster:*`,
`roles/managedkafka.client`, `kafka:GetBootstrapBrokers`, `kafka:ListScramSecrets` or
`Microsoft.HDInsight/clusters/configurations/action`. HDInsight is read only through what Resource Graph
projects, never through an `az hdinsight` configuration call.

Left out on the source side, as the guide leaves out an EKS cluster's nodes: the parts a cluster or
collection stands for, `AWS::MSK::Configuration`, `AWS::MSK::ClusterPolicy`, `AWS::MSK::BatchScramSecret`,
`AWS::MSK::VpcConnection` and the OpenSearch Serverless `AccessPolicy`, `SecurityPolicy`,
`SecurityConfig`, `LifecyclePolicy` and `VpcEndpoint`; and the virtual machines and disks of an HDInsight
cluster, which do not appear as separate resources. An Elasticsearch domain before 7.0 stays
`unclassified`, under its own name.

Capabilities the programs derive (each milestone pins the exact rule):

| Category | Capability | When |
|---|---|---|
| `search.opensearch` | `node-high-availability` | AWS: `ClusterConfig.InstanceCount` 2 or more (to be checked: whether replicas per index, which no domain setting gives, must be stated by hand); a Serverless collection with standby replicas `ENABLED` |
| `search.opensearch` | `zone-high-availability` | AWS: `ClusterConfig.ZoneAwarenessEnabled` true, or `MultiAZWithStandbyEnabled` true; a Serverless collection with standby replicas `ENABLED` (to be checked: AWS says it places them in another zone) |
| `search.opensearch` | `encryption-at-rest` | AWS: `EncryptionAtRestOptions.Enabled` true; always for a Serverless collection |
| `search.opensearch` | `snapshots` | AWS: always for a domain (OpenSearch Service takes automated snapshots of every domain; to be checked); always for a Serverless collection (hourly automated snapshots, restorable; decided in milestone 2) |
| `messaging.kafka` | `node-high-availability` | MSK: `NumberOfBrokerNodes` 2 or more (to be checked: whether a topic's replication factor, 2 or more for a topic to survive the loss of a broker, which no cluster setting gives, must be stated by hand); MSK Serverless: always; GCP Managed Kafka: always (to be checked); HDInsight: 3 or more worker nodes (to be checked: its minimum, and the replication factor, as for MSK) |
| `messaging.kafka` | `zone-high-availability` | MSK: brokers in two or more zones (`BrokerNodeGroupInfo.ClientSubnets`, one subnet per zone; to be checked); MSK Serverless: always (to be checked); GCP: to be checked; HDInsight: zones set on the cluster |
| `messaging.kafka` | `encryption-at-rest` | MSK and MSK Serverless: always (`EncryptionInfo.EncryptionAtRest`); GCP: always (Google default encryption); HDInsight: always (Azure managed disks, encrypted by default; to be checked) |

Not capabilities: shards and partitions (what both categories are, as scaling with requests is what
`serverless.container` is; `sharding` stays a `cache.redis` capability), UltraWarm and cold storage,
dedicated master nodes, fine-grained access control, Kafka tiered storage, Kafka Connect and Schema
Registry. The guide says so. `point-in-time-restore` is not derived: no source or target offers it for
either.

`zone-high-availability` is likely true of nearly every Kafka resource: a provisioned MSK cluster puts
its client subnets in 2 or 3 zones, and a GCP Managed Kafka cluster is regional. Such a resource then
maps `partial`, missing `zone-high-availability`, to every OVHcloud 1-AZ Kafka plan and to Scaleway
Single-AZ, and `direct` only to a 3-AZ or Multi-AZ variant where one is an offering. That is the
intended reading (the source keeps serving through the loss of a zone, the 1-AZ target does not), and
milestones 2 and 3 check whether a single-zone cluster can exist at all.

### Estimates

| Category | Size fields (inventory version 2) | Priced by | Why |
|---|---|---|---|
| `search.opensearch` | `vcpus`, `memoryGiB` and `storageGiB` per data node, `count` (data nodes), `usage.hoursPerMonth` | plan, its per-node price × `count`, plus storage above what the plan includes, per node × `count` | Every target sells search clusters as nodes of a flavour with storage per node (UpCloud bundles storage into its plans; OVHcloud and Scaleway price node types and storage) |
| `messaging.kafka` | as `search.opensearch`: per broker, `count` (brokers) | as `search.opensearch` | Same shape: OVHcloud's and Scaleway's brokers are node types with storage per node |

- **Matching is on vCPUs and memory**, as for every plan-priced category but `cache.redis` (ADR 0027):
  both categories join `estimate.ByPlan` and the existing rule. Neither joins `catalog.MatchesOnMemory`.
- **Storage is per node, not stored once.** `storedOnce` (`internal/domain/estimate/estimate.go`) stays
  the managed databases'. A search or Kafka cluster shards its data across its nodes, and each source
  sizes storage per node (OpenSearch Service `EBSOptions.VolumeSize`, MSK
  `BrokerNodeGroupInfo.StorageInfo.EbsStorageInfo.VolumeSize`), as Scaleway's create pages do ("storage
  capacity for each node"). So `storageGiB` is per node and the extra storage is × `count`, as for
  `compute.vm` and `kubernetes.managed`. Milestone 1's ADR records it.
- **Per-node prices.** A plan in the snapshot is one node, its price per node; where a page prices a
  whole cluster (UpCloud's `3x2xCPU-8GB-240GB-3D`, OVHcloud's per-service totals), the snapshot converts
  it and `SOURCES.md` says so, as ADR 0046 does for caches. An UpCloud plan's included storage is the
  plan's storage ÷ its nodes (240 GiB ÷ 3 = 80 GiB per node).
- **Count.** Absent: one node, "(count assumed 1)". Below or above a tier's fixed node count (OVHcloud's
  3-node Production, 6-node Advanced): priced at `count`, as PRD #552's decided question 10 says;
  `SOURCES.md` names each tier's node count, and PRD #552's follow-up issue on fixed and minimum node
  counts covers these tiers too.
- **What the guide fills in.** AWS names instance types, not sizes (`r6g.large.search`,
  `kafka.m5.large`); milestone 2 checks whether stripping `.search` and `kafka.` gives an EC2 type the
  guide's existing `ec2 describe-instance-types` call sizes, and otherwise the guide says to add `vcpus`
  and `memoryGiB` by hand, as for an RDS instance class. `count` is `InstanceCount` (data nodes, not the
  dedicated masters) or `NumberOfBrokerNodes`; `storageGiB` the per-node volume. HDInsight: `count` from
  the worker role's `targetInstanceCount`, `vcpus` and `memoryGiB` from its VM size, which the guide's
  existing VM size lookup gives; `storageGiB` from the worker role's managed disks (`dataDisksGroups`:
  disks per node × disk size) if Resource Graph projects them and the disk size can be read
  (milestone 4 checks), and otherwise no `storageGiB`, so storage is not priced. GCP Managed Kafka
  sizes a cluster in total vCPUs and memory, not brokers; milestone 3 checks whether a broker count can
  be derived, and otherwise writes no size ("not estimated: size unknown"), never a cluster total as
  one node.
- **Serverless resources** (MSK Serverless, OpenSearch Serverless collections) have no node size: no
  size fields, so "not estimated: size unknown". They are billed by usage (partitions and throughput,
  OCUs), and a plan-priced category has no usage path; decided question 3.

No new not-estimated reason: every case above is an existing one.

### Left out

| Candidate | Why left out |
|---|---|
| Event Hubs namespaces as `messaging.kafka` by default | Decided question 1: they stay `unclassified`, with the guide's hand rule |
| Elasticsearch after 7.10 (Elastic Cloud, Elastic on Azure) | Another vendor runs it, and its APIs have moved past OpenSearch's |
| Elasticsearch domains before 7.0 | Not served by OpenSearch without a reindex and a client change |
| Kinesis Data Streams, Kinesis Firehose | Its own protocol; no target equivalent (PRD #552, *Left out*) |
| Kafka Connect, MirrorMaker, Schema Registry, OpenSearch Ingestion, Dashboards | Integration and tooling around a cluster; OVHcloud's Kafka Connect could serve a later category |
| A `streaming.*` or `search.*` family with several members | One category each is enough until a second product needs one |

## Acceptance criteria

All criteria are pinned by tests that run offline under `make test` or `make test-e2e`, unless marked
*review*.

**Taxonomy and inventory**

- [x] An inventory (schema version 1 or 2) with a resource of category `search.opensearch` or
  `messaging.kafka` is read, and `shift2eu assess` exits 0. An unknown value such as
  `search.elasticsearch` or `messaging.kinesis` is still refused with exit 1 and "unknown taxonomy
  category".
- [x] A version 2 resource of either new category takes `vcpus`, `memoryGiB`, `storageGiB`, `count` and
  `usage.hoursPerMonth`, and nothing else; a resource of another category keeps exactly the fields it
  takes today.
- [ ] Inventories without the new categories keep their reports: in milestones 1 to 4 no existing
  golden file of such an inventory changes (the e2e `assess-*` goldens, the report adapters' goldens;
  the `examples/` goldens change in milestone 1 only because the samples gain resources of the new
  categories); in milestones 5 to 7 such a golden may change only in its catalog line's or `snapshot`
  object's source and retrieved date, and in exactly the amounts of an existing offering whose price or
  plan the re-read page shows changed, as PRD #552's same criterion says.
- [x] The sample inventories under `examples/` together use every taxonomy category, the new ones
  included (the existing check in `test/examples`).

**Mapping and report**

- [ ] Given a `messaging.kafka` resource and UpCloud, its fit is `none`, the reason is "no offering in
  the UpCloud catalog for `messaging.kafka`", and it is listed as a blocker; the same for both
  categories on Scaleway while its products' general availability is not confirmed (open
  question 2), and on any target before its catalog milestone lands.
- [ ] Given a resource of each new category and a target that serves it, the report names the best
  offering, with fit `direct` when the offering lists every capability the resource lists and `partial`
  naming each missing capability otherwise. A test per target pins the best offering for a resource
  with no capability and for one with each capability a tier variant exists for (for example
  `zone-high-availability` on a `search.opensearch` resource gives `partial` missing
  `zone-high-availability` on UpCloud, and points at an OVHcloud 3-AZ OpenSearch variant if milestone 5
  confirms one exists).
- [x] The JSON report carries the new category values in `category` and keeps `schemaVersion` 1, 2 and
  3 as today; every golden document validates against its schema.

**Estimates**

- [x] A `search.opensearch` or `messaging.kafka` resource with `vcpus`, `memoryGiB` and `count` is priced
  at the per-node price of the cheapest covering plan of the best offering × `count`, scaled by
  `hoursPerMonth`, plus `max(0, storageGiB − included) × per-GiB × count`; without `count` as one node
  with "(count assumed 1)"; without `vcpus` or `memoryGiB` "not estimated: size unknown"; never counted
  as 0 in a total.
- [x] The storage of a `search.opensearch` or `messaging.kafka` resource is multiplied by `count`, and
  that of a `database.*` resource still is not (a test with the same sizes in both).
- [ ] The per-category estimate table in `test/examples` has a representative resource for every new
  category each snapshot offers, and pins its plan and amount.

**Inventory guide**

- [ ] Run as the guide runs them, against synthetic fixtures in each call's documented response shape,
  sanitised as CLAUDE.md rule 2 says (`123456789012`, `192.0.2.0/24`, `example.com`, all-zero GUIDs and
  UUIDs) and never captured from a real account, each source's jq program
  classifies every `rawType` in *How the sources classify* into its category, with the capabilities and
  sizes the tables give, and writes a schema version 2 inventory the binary accepts (the existing e2e
  test, its category list per source: AWS gains both categories, GCP and Azure `messaging.kafka` only).
- [ ] An OpenSearch Service domain on `Elasticsearch_6.8`, an Event Hubs namespace, an Azure AI Search
  service and an `AWS::KafkaConnect::Connector` in the fixtures stay `unclassified`; an
  `AWS::MSK::Configuration` and an OpenSearch Serverless security policy are not in the inventory; the
  AWS Config rows of a domain the service call lists (`AWS::OpenSearch::Domain`,
  `AWS::Elasticsearch::Domain`) are not in the inventory twice.
- [ ] The guide says, beside the Azure table, when to change an Event Hubs namespace to
  `messaging.kafka` by hand.
- [ ] Every new command in the guide only lists, describes or gets, and the AWS policy, GCP roles and
  Azure role in the guide cover it (*review*, and the existing `cloud-read-only` checks where they
  apply).
- [ ] Every new call keeps only the fields its program reads, with an explicit `--query`, `--format`
  or Resource Graph `project` (*Only the fields the program reads*; the guide's *Privacy* section,
  `docs/guide/inventory.md`). Two offline tests pin it:
  - *the fixtures:* every key in a new call's raw-output fixture is one that call's jq program reads (an
    allow list), so none of `AccessPolicies`, `AdvancedSecurityOptions`, `CognitoOptions`, `Endpoint`,
    `Endpoints`, `DomainEndpointOptions`, `ZookeeperConnectString*`, `BootstrapBroker*`,
    `collectionEndpoint`, `dashboardEndpoint`, `bootstrapAddress` (GCP Managed Kafka), `osProfile` or
    `connectivityEndpoints` (HDInsight) appears;
  - *the guide's command text*, so that a fixture without those keys cannot hide a command that dropped
    its projection: an extension of the guide-command comparison (`TestInventoryCommandsAreTheGuides`,
    `test/site/inventory_test.go`) checks that each new `aws opensearch`, `aws opensearchserverless` and
    `aws kafka` command has a `--query`, the `gcloud managed-kafka` command a `--format`, and the
    Resource Graph query a `project`, and, where the projection parses, that it names only the fields
    under *Only the fields the program reads*. A half of this that cannot be made a test is *review*.
- [ ] *Review:* no data-plane permission and no configuration or credentials call is used or granted
  (*No data-plane or configuration access*): the guide's AWS policy, GCP roles and Azure role name
  none of the permissions listed there.
- [ ] The site's inventory page shows the guide's new commands (`test/site`, which compares the page
  with the guide).

**Catalogs**

- [ ] OVHcloud's catalog serves `search.opensearch` and `messaging.kafka`, UpCloud's
  `search.opensearch` only, and Scaleway's neither until decided question 2's gate is met; each served
  category with at least one offering (`targettest` *category* check), only in declared EU regions
  (*regions* check). A golden file per target pins the parsed catalog, which is what pins the absence of
  the others.
- [ ] Every new offering has its own section in its target's `SOURCES.md`, naming the pages it comes
  from, the fact behind each capability, region and price, and each source's licence. Every capability
  that holds on some plans only is on a tier variant (ADR 0011), and `zone-high-availability` is listed
  only where a quoted source says the service keeps serving through the loss of a zone.
- [ ] Each target's `SOURCES.md` *Left out on purpose* names every new category it does not serve, with
  the pages checked (UpCloud: no Kafka product; Scaleway: general availability not confirmed), and the
  *Products not in the snapshot* lines of OVHcloud and UpCloud no longer list OpenSearch or Kafka as
  having no category.
- [ ] *Review:* no price in a snapshot comes from a page that was neither fetched from public
  documentation nor saved by the maintainer; no saved page is committed (`TestNoTrackedSavedPages`).

**Architecture and formats**

- [ ] `make check` passes, including depguard and `test/architecture` for every release target.
- [ ] *Review:* the inventory file format stays at schema version 2, the catalog snapshot format at
  version 2 and the JSON report at its current versions; the ADR of milestone 1 records why.
- [ ] *Review:* no direct source-service-to-target-service table exists anywhere: the guide maps
  `rawType` to a category, the catalogs map offerings to categories, and only the taxonomy joins them.

## Milestones

Each milestone ships as one PR. All depend on this PRD being merged, and every other milestone on
milestone 1. Milestones 2 to 4 (the guide) and 5 to 7 (the catalogs) are independent of one another.
The guide milestones also touch the files PRD #552's milestones 2 to 4 change (`docs/guide/inventory.md`,
the `.jq` programs, the fixtures and e2e goldens): each starts after the PRD #552 milestone of the same
source has merged, so the two never conflict (decided question 5). The catalog milestones likewise touch the
files PRD #552's catalog milestones change (`catalog.json`, the parsed-catalog golden, the `targettest`
category list, and `SOURCES.md`'s *Left out on purpose* and *Products not in the snapshot*): milestone 6
(UpCloud) starts after PRD #552 milestone 7 (UpCloud) has merged and milestone 7 (Scaleway) after PRD
#552 milestone 6 (Scaleway); milestone 5 (OVHcloud) waits for nothing, as PRD #552 milestone 5
(OVHcloud) has merged. Until a target's catalog milestone
lands, its report shows the new categories as fit `none` with "no offering in the <target> catalog".

1. [x] **Taxonomy, inventory and estimates.** `search.opensearch` and `messaging.kafka` in
   `internal/domain/taxonomy`, with their definitions; their rows in the size-field table
   (`internal/domain/inventory`, all five fields); both in `estimate.ByPlan`, matched on vCPUs and
   memory, storage per node × `count` (not `storedOnce`). One ADR records the categories and their
   boundaries (*What counts*), the field rows (amending ADR 0025), the storage-per-node rule (amending
   ADR 0027) and why no format version changes. The samples under `examples/` gain one resource per new
   category, and their goldens change. The guide's lists of categories and its *Sizes* table name the
   new ones. No JSON schema file changes: `category` has no `enum`, no reason is added, and no
   description lists categories (only `$defs/plan.vcpus` names `cache.redis`, unchanged).
2. [x] **AWS classification in the guide.** `opensearch list-domain-names` and `describe-domains`,
   `opensearchserverless list-collections` and `batch-get-collection`, `kafka list-clusters-v2`, with
   `es:ListDomainNames`, `es:DescribeDomains`, `aoss:ListCollections`, `aoss:BatchGetCollection` and
   `kafka:ListClustersV2` in the guide's policy; `aws.jq` classifies the AWS column, derives the
   capabilities and sizes, drops the Config duplicates and the parts left out; each call with an
   explicit `--query` that keeps only the fields listed under *Only the fields the program reads*, and
   no data-plane, configuration or credentials permission in the policy (*No data-plane or
   configuration access*: not `es:ESHttp*`, `aoss:APIAccessAll`, `kafka-cluster:*`,
   `kafka:GetBootstrapBrokers` or `kafka:ListScramSecrets`); tables, synthetic sanitised fixtures in
   the documented response shape (never captured from a real account), the test that they carry no
   excluded key, the e2e golden and the AWS entries of the e2e category list. Depends on 1 and PRD #552
   milestone 2.
3. [x] **GCP classification in the guide.** `managedkafka.googleapis.com/Cluster` from the existing asset
   search, the Managed Kafka list call for its settings and sizes, with an explicit `--format` that
   keeps only the fields read, and `roles/managedkafka.viewer` added to the guide's list of narrow GCP
   roles, saying what it covers (cluster configuration only, no topic data or messages; milestone 3
   confirms it grants no message access); `roles/viewer` stays the fallback; never
   `roles/managedkafka.client`. `ConnectCluster` stays `unclassified`; synthetic sanitised fixtures,
   the test that they carry no excluded key, golden and the GCP entries of the e2e category list.
   Depends on 1 and PRD #552 milestone 3.
4. [ ] **Azure classification in the guide.** HDInsight clusters of kind `KAFKA` from Resource Graph, the
   `project` extended to `clusterDefinition.kind`, the worker role's count and VM size, its disks if
   they give `storageGiB`, and the zones, and nothing else (never `osProfile` or
   `connectivityEndpoints`); HDInsight is read only through what Resource Graph projects, never an
   `az hdinsight` configuration call or `Microsoft.HDInsight/clusters/configurations/action`;
   Event Hubs namespaces stay `unclassified`, with the hand rule; synthetic sanitised fixtures (all-zero
   GUIDs), the test that they carry no excluded key, golden and the Azure entries of the e2e category
   list. Depends on 1 and PRD #552 milestone 4.
5. [ ] **OVHcloud catalog.** Offerings for `search.opensearch` (Discovery, Production and Advanced, with
   3-AZ variants of Production and Advanced if milestone 5's check confirms them) and `messaging.kafka`
   (Production and Advanced, Gen 3; Gen 2 Business and Enterprise only if the prices page still sells
   them; 3-AZ variants if the zone continuity sentence covers Kafka), with plans and per-node prices,
   storage prices and included storage; `SOURCES.md`, golden, `targettest` categories. Pages: the
   documentation above (fetchable), the prices page <https://www.ovhcloud.com/en-ie/public-cloud/prices/>
   (*OpenSearch* and *Kafka* sections) and the regions page
   <https://www.ovhcloud.com/en/public-cloud/regions-availability/>, fetched as in PRD #552 milestone 5
   (`www.ovhcloud.com` answered an automated client on 2026-11-25) or, if they refuse one, saved by the
   maintainer into a scratch directory outside the repository, never committed (as milestones 6 and 7
   say). An existing price or plan the re-read page shows changed is updated to its current value.
   Depends on 1.
6. [ ] **UpCloud catalog.** Offerings for `search.opensearch`: Managed OpenSearch (1 node) and a
   multi-node variant with `node-high-availability`, `encryption-at-rest` and `snapshots` (the backups
   page; to be checked that a restorable backup meets the `snapshots` definition, as for OVHcloud), in
   the eight EU zones, with plans and per-node prices. **Gated on the maintainer saving
   <https://upcloud.com/pricing/>, EUR edition, logged out, as before, into a scratch directory outside
   the repository, never committed (as milestone 7 says):** the Managed Databases tab's OpenSearch
   table. One saved page serves this milestone and PRD #552 milestone 7 (its Valkey table), so the
   maintainer saves it once, for whichever runs first. Depends on 1 and PRD #552 milestone 7.
7. [ ] **Scaleway catalog, or its record of absence.** **Gated on decided question 2.** If the maintainer
   confirms general availability: offerings for `search.opensearch` (Standalone, and High Availability
   with `node-high-availability`) and `messaging.kafka` (Single-AZ, and Multi-AZ with
   `zone-high-availability` only if a page says it keeps serving through the loss of a zone; the
   changelog's "automatic failover and zero event loss" is quoted), with plans and prices, in the
   regions the availability page and the SDK both give (`fr-par` today). The maintainer saves, from a
   logged-out browser, into a scratch directory outside the repository: the product pages
   <https://www.scaleway.com/en/opensearch/> and <https://www.scaleway.com/en/clusters-for-apache-kafka/>
   (or wherever Scaleway's menu leads; the badge or text that states the status), the pricing page or
   pages that list OpenSearch and Kafka node types and storage (from <https://www.scaleway.com/en/pricing/>;
   likely its *Managed Databases* and *Data* sections), and
   <https://www.scaleway.com/en/product-availability-by-region/> (which PRD #552 milestone 6 also needs,
   as it may the *Managed Databases* pricing page: a copy saved for one serves both if it is still
   current). If general availability is not
   confirmed: Scaleway's `SOURCES.md` *Left out on purpose* records both products, the pages checked and
   what reverses it, and no offering is added. Depends on 1 and PRD #552 milestone 6.
8. [ ] **Comparison, docs and close.** An e2e fixture inventory with a resource of each new category,
   assessed against all three targets, golden for Markdown and JSON; the README and the `CLAUDE.md`
   status line updated. Moves this PRD to `prds/done/` and closes #571. Depends on 2 to 7.

## Decisions and decided questions

**Decisions** (approved by the maintainer with the decided questions below, 2026-11-25)

- **Two categories, named after the protocol** (*Names*): `search.opensearch` in a new `search` family,
  `messaging.kafka` in the existing `messaging` family.
- **Elasticsearch 7.x is in, before 7.0 is out.** The boundary is the client and REST API break alone:
  the OpenSearch API is the Elasticsearch 7.10 API, so a client using the typeless 7.x API speaks it
  unchanged, and 7.0 removed mapping types, so a 6.x client needs a change. Upgrades and index
  compatibility do not decide it (a move is a new cluster either way). As for the databases, the engine
  version is not assessed beyond that boundary. The guide's warning (milestones 2 to 4) says that a 7.x
  client still using typed endpoints or `include_type_name` needs a change for OpenSearch 2.x and 3.x,
  and that Elastic clients from 7.14 on refuse a server that is not Elasticsearch.
- **Serverless variants are in, unsized.** An OpenSearch Serverless collection and an MSK Serverless
  cluster speak their protocol, so their clients move to a target cluster; leaving them `unclassified`
  would show a blocker where a target serves them. They carry no size, and the report says "size
  unknown" (decided question 3).
- **Vendor-run services are out.** Elastic Cloud, Confluent Cloud and Aiven through a marketplace are the
  vendor's service, not the source's.
- **No new capability.** `node-high-availability`, `zone-high-availability`, `encryption-at-rest` and
  `snapshots` cover the differences the targets document; partitions and shards are what the categories
  are.
- **Plan pricing with storage per node.** Both categories are ByPlan on vCPUs and memory, and storage is
  per node × `count`, unlike the managed databases (*Estimates*). Recorded in
  [ADR 0047](../docs/adr/0047-add-search-opensearch-and-messaging-kafka-and-price-their-storage-per-node.md),
  with the boundaries, the field rows and why no format version changes.
- **No inventory format version.** As ADR 0023 and ADR 0046: `taxonomy.ParseCategory` accepts a new
  value in a version 1 or 2 file; the field table gains rows for new categories only, so no existing
  file changes meaning; ADR 0025 asks for a new version only once `shift2eu discover` writes files. An
  older binary refuses an inventory that uses a new category with "unknown taxonomy category".
- **No catalog snapshot format change.** Offerings name categories the domain validates (ADR 0007), and
  both categories' plans carry vCPUs, memory and included storage as database plans do (ADR 0024).
- **No JSON report `schemaVersion` bump, and no schema file change.** `category` is a string with no
  `enum` (ADR 0018, ADR 0023); no estimate reason or key is added, removed or retyped; the frozen
  version 1 and 2 files are untouched, and no version 3 description names the categories.
- **GA only, and the target rule.** As in PRDs #11, #77 and #552: a beta product, or one whose general
  availability no page states, is not an offering, and a capability is listed only where the
  documentation states it (ADR 0011).
- **The guide can ship before the catalogs.** A classified resource with no offering yet is fit `none`
  with a reason naming its category, never worse than `unclassified`.

**Decided by the maintainer** (2026-11-25, as recommended: each question keeps its recommendation's
text as the decision)

1. **Event Hubs and its Kafka endpoint.** The Standard, Premium and Dedicated tiers expose the Kafka
   protocol, but most clients use AMQP, and no read-only call tells which. *Decision (2026-11-25,
   decided by the maintainer as recommended):* keep Event
   Hubs namespaces `unclassified`, and have the guide say: change the category to `messaging.kafka` by
   hand when every client uses the Kafka endpoint. The alternative, `messaging.kafka` for every Standard
   or higher namespace, maps AMQP clients `direct` to Kafka and hides a client rewrite.
2. **Scaleway's two products are not confirmed GA.** No changelog entry or documentation page states a
   beta or general availability for Cloud Essentials for OpenSearch or Clusters for Apache Kafka®, both
   APIs are `v1alpha1` in `fr-par` only, and the product pages that would show a status are on
   `www.scaleway.com`, which refuses automated clients. Options: (a) add both categories now with
   OVHcloud and UpCloud (OpenSearch) and OVHcloud alone (Kafka), and gate Scaleway's milestone on the
   maintainer's saved product pages; (b) wait for Scaleway. *Decision (2026-11-25, decided by the
   maintainer as recommended):* (a), both categories now, only the Scaleway milestone gated. A category with fewer
   targets is still more precise than `unclassified`: OVHcloud's and UpCloud's offerings are GA, and
   Scaleway shows "no offering in the Scaleway catalog" until its pages confirm GA, as PRD #552 did for
   OVHcloud File Storage. Milestone 7 then adds the offerings or records their absence.
3. **Serverless variants unsized.** *Decision (2026-11-25, decided by the maintainer as recommended):*
   classify them and leave them unsized ("size unknown"), as under *Decisions* (*Serverless variants
   are in, unsized*). The
   alternative, a usage path for a ByPlan category, would need an estimate method per resource rather
   than per category, which ADR 0027 does not have.
4. **No new capability.** Candidates were a Kafka `tiered-storage` and an OpenSearch `k-nn` (vector
   search). *Decision (2026-11-25, decided by the maintainer as recommended):* none now. Every target's OpenSearch enables k-NN by default (OVHcloud's
   plugin list) or does not say, and no target documents Kafka tiered storage; a capability that every
   offering lists, or none does, only blurs the vocabulary.
5. **Order against PRD #552's milestones.** Both PRDs change the same guide sections, jq programs and
   fixtures, and the same catalog files (`catalog.json`, the parsed-catalog golden, the `targettest`
   category list, `SOURCES.md`'s *Left out on purpose* and *Products not in the snapshot*).
   *Decision (2026-11-25, decided by the maintainer as recommended):* each guide milestone here starts after PRD #552's milestone of the same source
   merges, and each catalog milestone after PRD #552's milestone of the same target: UpCloud
   (milestone 6) after PRD #552 milestone 7, Scaleway (milestone 7) after PRD #552 milestone 6;
   OVHcloud (milestone 5) waits for nothing, PRD #552 milestone 5 having merged. One saved
   `upcloud.com/pricing/` page serves both PRDs' UpCloud milestones, so the maintainer saves it once.
6. **Names.** `search.opensearch` (rather than `database.opensearch` or `search.managed`) and
   `messaging.kafka` (rather than `streaming.kafka`). *Decision (2026-11-25, decided by the maintainer
   as recommended):* as named, for the reasons under
   *Names*.

**To be checked** (each in the milestone named; a fact not confirmed stays out of the snapshot)

- Milestone 2: the exact AWS Config strings for OpenSearch domains and whether Config lists OpenSearch
  Serverless collections and MSK Serverless clusters (so the program drops the duplicates); that
  `describe-domains` gives `EngineVersion`, `ClusterConfig` (count, zone awareness, Multi-AZ with
  standby), `EBSOptions.VolumeSize` and `EncryptionAtRestOptions`; whether automated snapshots are on
  for every domain; whether a domain's node high availability needs replicas the guide cannot read;
  that OpenSearch Serverless standby replicas sit in another zone; that `list-clusters-v2` returns
  provisioned and serverless clusters with broker type, count, per-broker storage, client subnets and
  encryption; whether `.search` and `kafka.` instance types map onto EC2 types the guide already sizes;
  whether a single-zone provisioned MSK cluster can exist (client subnets in one zone), which decides
  whether `zone-high-availability` is ever false on MSK; that `opensearch describe-domains` takes at
  most 5 domain names per call and `opensearchserverless batch-get-collection` up to 100 ids, so the
  guide batches the names and ids it lists; the `--query` expressions that keep only the fields read.

  *Checked in milestone 2 (2026-11-26), on docs.aws.amazon.com, anonymously, no AWS API called:*
  - **Config types.** AWS Config records `AWS::OpenSearch::Domain` and `AWS::Elasticsearch::Domain`
    (listed under "Amazon OpenSearch Service (legacy Elasticsearch)"); `AWS::OpenSearchServerless::Collection`,
    `::VpcEndpoint`, `::SecurityConfig`, `::SecurityPolicy`, `::AccessPolicy`, `::LifecyclePolicy`;
    `AWS::MSK::Cluster`, `::Configuration`, `::BatchScramSecret`, `::ClusterPolicy`,
    `::ServerlessCluster`, `::VpcConnection`; `AWS::KafkaConnect::Connector` and `::CustomPlugin`; and
    `AWS::OSIS::Pipeline` (OpenSearch Ingestion). So Config lists collections and MSK Serverless clusters
    too, and `aws.jq` drops their rows, and the provisioned clusters', when the service call lists one of
    the same type and name. <https://docs.aws.amazon.com/config/latest/developerguide/resource-config-reference.html>
  - **`describe-domains` fields.** `DomainStatus` has `EngineVersion` ("in the format
    `Elasticsearch_X.Y` or `OpenSearch_X.Y`"), `ClusterConfig.InstanceType`, `InstanceCount` ("Number of
    data nodes"), `ZoneAwarenessEnabled`, `MultiAZWithStandbyEnabled`, `EBSOptions.EBSEnabled` and
    `VolumeSize` ("the size (in GiB) of EBS volumes attached to data nodes") and
    `EncryptionAtRestOptions.Enabled`. Neither the API nor the CLI reference states a maximum number of
    domain names per call; the guide asks for 5 at a time, which is safe either way.
    <https://docs.aws.amazon.com/opensearch-service/latest/APIReference/API_DescribeDomains.html>,
    <https://docs.aws.amazon.com/cli/latest/reference/opensearch/describe-domains.html>
  - **Automated snapshots, every domain.** "All OpenSearch Service domains take automated snapshots";
    hourly, kept 14 days, from Elasticsearch 5.3 on, daily before; they restore the domain. So
    `snapshots` always holds for a domain.
    <https://docs.aws.amazon.com/opensearch-service/latest/developerguide/managedomains-snapshots.html>
  - **Node and zone high availability need replicas.** "If you enable multi-AZ without Standby, you
    should create at least one replica for each index in your cluster. Without replicas, OpenSearch
    Service can't distribute copies of your data to other Availability Zones. Fortunately, the default
    configuration for any index is a replica count of 1." Multi-AZ with Standby spreads the domain over
    three zones, "each zone containing a complete copy of data". No domain setting gives the replica
    count, so the guide derives both capabilities from the settings and says to remove them by hand for
    indexes with no replica.
    <https://docs.aws.amazon.com/opensearch-service/latest/developerguide/managedomains-multiaz.html>
  - **OpenSearch Serverless standby replicas.** Not confirmed as another zone: the console's
    *Standby replicas* option is "for high availability"
    (<https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-create.html>), the
    API says only "Indicates whether standby replicas should be used for a collection"
    (<https://docs.aws.amazon.com/opensearch-service/latest/ServerlessAPIReference/API_CreateCollection.html>),
    and the capacity page now says "OpenSearch Serverless offers high availability by default without
    requiring standby nodes or redundancy configuration"
    (<https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-scaling.html>). The
    program keeps the planned rule (both capabilities when `standbyReplicas` is `ENABLED`), and the guide
    says AWS does not name the zone and to remove `zone-high-availability` by hand where it does not
    hold.
  - **OpenSearch Serverless encryption and snapshots.** "Encryption at rest is *required* for all
    serverless collections" (<https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-encryption.html>).
    Found, and not in the capability table above: "By default, when you create a new collection,
    OpenSearch Serverless automatically creates snapshots every hour", restorable to the original
    collection (<https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-snapshots.html>).
    Decided in milestone 2: every collection gets `snapshots`, since an hourly, restorable snapshot
    meets the taxonomy's definition; the capability table above says so. Serverless clients "must be compatible with OpenSearch 3.x"
    (<https://docs.aws.amazon.com/opensearch-service/latest/developerguide/serverless-overview.html>).
  - **`batch-get-collection` batch.** `ids` and `names` take "Maximum number of 100 items" each; the
    guide passes ids, 100 at a time.
    <https://docs.aws.amazon.com/opensearch-service/latest/ServerlessAPIReference/API_BatchGetCollection.html>
  - **`list-collections` paging.** The AWS CLI has no paginator for the call: it answers with one page
    (20 by default, `maxResults` at most 100) and a `nextToken`. Decided in milestone 2 (review
    finding B1): the guide pages it by hand, in one loop that asks for 100 a page, passes `nextToken`
    back as `--next-token` until a page has none, and appends each page to the collections file as a
    JSON value of its own; `batch-get-collection` reads the IDs of every page. No new IAM action.
    Decided in milestone 2, review round 3 and the audit: the loop holds no shell array, so it runs in
    POSIX sh, dash, bash 3.2 (also under `set -u`) and zsh; it passes the token as
    `${token:+--next-token} ${token:+"$token"}`, two words or none in every one of them (finding N-a). A
    failed page after the first stops the loop with a message on stderr that names the collections
    file, says it is incomplete and that the region must be collected again; a failed first page says
    the file is empty and that the region must be collected again if it runs OpenSearch Serverless,
    since the two cannot be told apart from the exit status alone (finding S-B). A page that hands back
    the token it was given stops the loop with a message, rather than loop forever (audit nit).
    Decided in milestone 2, review round 4 (finding F1): the block names the collections file once,
    as `f`, and is laid out conventionally (`if ...; then`, `fi` on lines of their own), with every
    line short enough to read on the page.
    <https://docs.aws.amazon.com/opensearch-service/latest/ServerlessAPIReference/API_ListCollections.html>
  - **`ec2:DescribeInstanceTypes` in the policy.** Decided in milestone 2 (audit, low): the guide's
    minimal policy lacked the action `ec2 describe-instance-types` needs, a gap older than this PRD that
    milestone 2 widened, as the call now also sizes OpenSearch domains and MSK brokers. The policy now
    grants it, next to `ec2:DescribeInstances`, and a test checks every collect call against the policy.
  - **Config rows by name or ID.** Decided in milestone 2 (review finding S1): AWS Config may record a
    collection by its ID alone and an MSK cluster by its ARN alone, so the program drops the Config
    row of a collection or cluster the call lists when its `resourceName` or its `resourceId` (an
    ARN's last segment) matches the name, the ID or the ARN's last segment of a listed one.
  - **Deleted and failed resources.** Decided in milestone 2 (review finding N2): a domain whose
    `Deleted` is true, a collection whose `status` is `DELETING` or `FAILED`, and an MSK cluster whose
    `State` is `DELETING` or `FAILED` are left out, as a terminated EC2 instance is; the guide's
    queries keep those three fields.
  - **`list-clusters-v2`.** Paginated (the CLI fetches every page); `ClusterType` is `PROVISIONED` or
    `SERVERLESS`; `Provisioned` gives `NumberOfBrokerNodes`, `BrokerNodeGroupInfo.InstanceType` ("The
    type of Amazon EC2 instances to use for Apache Kafka brokers"), `ClientSubnets`,
    `StorageInfo.EbsStorageInfo.VolumeSize` ("The size in GiB of the EBS volume for the data drive on
    each broker node") and `EncryptionInfo.EncryptionAtRest.DataVolumeKMSKeyId`.
    <https://docs.aws.amazon.com/cli/latest/reference/kafka/list-clusters-v2.html>
  - **MSK encryption at rest.** "Amazon MSK always encrypts your data at rest." So the guide's query
    does not keep `DataVolumeKMSKeyId`, a key ARN with the account ID in it, which the rule does not
    need. <https://docs.aws.amazon.com/msk/latest/developerguide/msk-encryption.html>
  - **One subnet per zone; no single-zone MSK cluster.** "If you use the US West (N. California)
    Region, specify exactly two subnets. For other Regions where Amazon MSK is available, you can
    specify either two or three subnets. The subnets that you specify must be in distinct Availability
    Zones. When you create a cluster, Amazon MSK distributes the broker nodes evenly across the subnets
    that you specify." So a provisioned cluster always spans 2 or 3 zones and always gets
    `zone-high-availability`. <https://docs.aws.amazon.com/msk/1.0/apireference/clusters.html>
  - **MSK node high availability needs a replication factor.** "Ensure that the replication factor
    (RF) is at least 3. Note that a RF of 1 can lead to offline partitions during a rolling update; and
    a RF of 2 may lead to data loss" (<https://docs.aws.amazon.com/msk/latest/developerguide/bestpractices.html>);
    `default.replication.factor` is "3 for clusters in 3 Availability Zones, and 2 for clusters in 2
    Availability Zones" (<https://docs.aws.amazon.com/msk/latest/developerguide/msk-default-configuration.html>).
    No cluster setting gives a topic's factor, so the guide says to remove both capabilities by hand
    for topics with a factor of 1.
  - **MSK Serverless zones.** Not stated as replication: the pages say only "To help client
    applications switch over to another Availability Zone in the event of an outage, you must specify
    at least two subnets in each VPC"
    (<https://docs.aws.amazon.com/msk/latest/developerguide/create-serverless-cluster.html>) and that
    Amazon MSK sets the broker configuration, which cannot be changed
    (<https://docs.aws.amazon.com/msk/latest/developerguide/serverless-config.html>). The program keeps
    the planned "always" for both capabilities, and the guide quotes the subnet sentence.
  - **Instance types and EC2.** No AWS documentation page gives a `.search` or `kafka.` type's vCPU and
    memory, or says they equal the EC2 type's; the MSK API calls a broker type "the type of Amazon EC2
    instances to use for brokers", and the broker-size page sends readers to the EC2 instance types
    for M5, M7g and T3 sizes (<https://docs.aws.amazon.com/msk/latest/developerguide/broker-instance-sizes.html>).
    OpenSearch Service also runs families with no EC2 twin, `or1`, `or2`, `om2` and `oi2`
    (<https://docs.aws.amazon.com/opensearch-service/latest/developerguide/supported-instance-types.html>),
    and MSK Express brokers (`express.m7g.large`) have none either. The guide strips `.search` and
    `kafka.`, sizes the result with `ec2 describe-instance-types` one type per call (so an unknown type
    fails alone), never asks for the families without a twin, and says to add `vcpus` and `memoryGiB`
    by hand for those.
  - **Region.** Decided in milestone 2, not foreseen: each call keeps the resource's ARN (`ARN`,
    `arn`, `ClusterArn`) and the program takes the region from it, as for every other AWS resource;
    the ARN, which carries the account ID, never reaches the inventory. Two domains of the same name in
    two regions are two entries, and an MSK cluster's or a collection's AWS Config row is dropped only
    when the call lists one of the same type, name and region.
- Milestone 3: the `gcloud managed-kafka clusters list` call and its fields (`capacityConfig`, zones,
  encryption), the `--format` that keeps only those, whether a broker count can be derived, GCP Managed
  Kafka's zone layout (whether every cluster spans several zones of its region), and
  `roles/managedkafka.viewer` as the narrowest role, granting cluster configuration only and no topic
  data or message access.
- Milestone 4: the Resource Graph properties of an HDInsight Kafka cluster (`clusterDefinition.kind`,
  the worker role's `targetInstanceCount` and `hardwareProfile.vmSize`, zones), whether the worker
  role's managed disks (`dataDisksGroups`: disks per node and disk size) are projected and give
  `storageGiB` (otherwise no `storageGiB`, storage not priced), that HDInsight managed disks are
  encrypted at rest by default, HDInsight's minimum worker count, and the Event Hubs tiers that expose
  the Kafka endpoint (for the guide's hand rule).
- Milestone 5: whether the 3-AZ continuity and encryption-at-rest pages cover Kafka under "Public Cloud
  Analytics"; whether the prices page still sells Gen 2 Kafka plans; that the OpenSearch and Kafka
  prices are per node and give vCPUs, memory and storage per node.
- Milestone 6: UpCloud's OpenSearch prices per plan, converted per node, and whether a backup restorable
  to a new service meets `snapshots`.
- Milestone 7: Scaleway's general availability of both products; their regions; their node types'
  vCPUs, memory and prices; whether any page states encryption at rest, backups or zone continuity.
