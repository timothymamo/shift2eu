# PRD #552: More taxonomy categories

**Status:** In progress, milestones 1, 2, 3, 4 and 5 done. The maintainer decided all eleven questions under *Decisions and
decided questions* on 2026-11-24, as recommended. Milestone 1's ADR is
[0046](../docs/adr/0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md).
**Issue:** #552

## Problem

The taxonomy has nine categories (`internal/domain/taxonomy/category.go`): `compute.vm`,
`compute.baremetal`, `kubernetes.managed`, `storage.object`, `storage.block`, `database.postgresql`,
`database.mysql`, `serverless.function` and `messaging.queue`. Every other source resource is
`unclassified`, and an `unclassified` resource has fit `none` on every target, by definition: no
offering ever serves it.

The inventory guide (`docs/guide/inventory.md`, *AWS classification*, *GCP classification*, *Azure
classification*, and `docs/guide/inventory/{aws,gcp,azure}.jq`) therefore marks many common services
`unclassified`, or does not list them at all (below): in-memory caches (ElastiCache, Memorystore, Azure Cache for Redis), load balancers
(ELB, Cloud Load Balancing, Azure Load Balancer and Application Gateway), MongoDB-compatible databases
(DocumentDB, Cosmos DB for MongoDB), pub/sub topics (SNS, Service Bus topics), serverless containers
(Cloud Run, App Runner, Container Apps), container registries (ECR, Artifact Registry, Azure Container
Registry) and file shares (EFS, Filestore, Azure Files). A platform engineer who assesses a real
workload sees a row of `none` blockers for services that OVHcloud, Scaleway and UpCloud often do offer.
The report then overstates how hard the move is, and it cannot point at the offering to use.

Some of today's classification is also wrong in the other direction. The guide classifies a GCP
Pub/Sub topic as `messaging.queue`, so a topic that fans every message out to several subscriptions
maps `direct` to Scaleway Queues, which delivers each message to one consumer. A topic needs a
category of its own.

Some services are missing from the inventory altogether. AWS Config records neither ElastiCache nor
MemoryDB clusters (the
[AWS Config resource type list](https://docs.aws.amazon.com/config/latest/developerguide/resource-config-reference.html)
has no `AWS::ElastiCache::*` and no MemoryDB cluster type), so the guide's AWS inventory does not list them at
all, not even as `unclassified`. DocumentDB is listed, but not as what it is: `rds describe-db-instances`
returns DocumentDB (and Neptune) instances, and `aws.jq` writes each one as an `unclassified`
`AWS::RDS::DBInstance`.

Now is the time because the taxonomy is the only bridge between sources and targets: every live source
adapter that follows (`shift2eu discover`, not written yet) classifies into it. A category added before
those adapters exist costs one guide and three catalogs; added after, it also costs every adapter and
a new inventory format version (ADR 0025, *Consequences*).

## Outcome

A user who builds an inventory with the guide gets seven more taxonomy categories: `cache.redis`,
`database.mongodb`, `network.loadbalancer`, `messaging.topic`, `serverless.container`,
`registry.container` and `storage.file`. Their caches, MongoDB-compatible databases, load balancers,
topics, serverless containers, container registries and NFS file shares are classified instead of
`unclassified`, and `shift2eu assess` maps them to the OVHcloud, Scaleway and UpCloud offerings that
serve them, with fit `direct` or `partial` and the missing capabilities named, or fit `none` with the
reason "no offering in the <target> catalog for `<category>`" where a target has none. Caches and
MongoDB databases are priced by plan, file shares by GiB, from each snapshot's dated prices; topics and
serverless containers are "not estimated" as usage-priced, and load balancers and registries as not
priced, each with its reason. Every target offering comes from public documentation cited in its
`SOURCES.md`. The inventory format stays at version 2 and the JSON report keeps its `schemaVersion`.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | None live. The inventory file (schema versions 1 and 2) and the inventory guide's commands, jq programs and tables for **AWS**, **GCP** and **Azure** | Live discovery (`shift2eu discover`) |
| Target providers | **OVHcloud**, **Scaleway** and **UpCloud**: new offerings in their embedded snapshots, with sources, plans and prices | New targets. Live catalog or pricing APIs |
| Taxonomy categories | New: `cache.redis`, `database.mongodb`, `network.loadbalancer`, `messaging.topic`, `serverless.container`, `registry.container`, `storage.file`. The nine existing categories keep their meaning; only GCP Pub/Sub topics move from `messaging.queue` to `messaging.topic` in the guide | The candidates under *Left out*, among them DNS zones, CDNs, OpenSearch, Kafka, key-value and wide-column databases, MariaDB, SQL Server and Oracle |
| Capabilities | Two new capabilities, `sharding` and `udp` (decided question 4). The existing `node-high-availability`, `zone-high-availability`, `encryption-at-rest`, `snapshots`, `point-in-time-restore`, `read-replicas`, `ipv6` and `dead-letter-queue`, reused; `snapshots` widened to file shares and cache data | Any other new capability (other protocols, durability, modules, WAF) |
| Inventory | Schema version 2, with the size fields the new categories take (*Estimates*) | A schema version 3 |
| Assessment and output | The current fit rules; estimates for `cache.redis`, `database.mongodb` and `storage.file`; a new not-estimated reason for categories that are never priced; Markdown, JSON and the site's HTML report | A JSON report `schemaVersion` bump. Minimum billed sizes. Request or traffic pricing |

### The new categories

Each definition is what the taxonomy's doc comment will say. Commonness is a judgement from typical
production estates, not a measured figure.

| Category | Definition | Why | OVHcloud | Scaleway | UpCloud |
|---|---|---|---|---|---|
| `cache.redis` | An in-memory key-value store the provider runs that speaks the Redis protocol: Redis OSS or its fork Valkey | Very common on all three sources | Public Cloud Databases for Valkey | Managed Database for Redis™ | Managed Valkey |
| `database.mongodb` | A document database the provider runs that speaks the MongoDB wire protocol | Common on AWS and Azure; two targets run real MongoDB | Public Cloud Databases for MongoDB | Managed MongoDB® | none |
| `network.loadbalancer` | A load balancer the provider runs in front of a set of backends, at layer 4 (TCP, and UDP where the capability `udp` says so) or layer 7 (HTTP/HTTPS) | Very common; every target has one, with TCP, HTTP and TLS termination (UDP on OVHcloud only, so far) | Public Cloud Load Balancer | Load Balancer | Managed Load Balancer |
| `messaging.topic` | A publish-subscribe topic the provider runs: every subscription gets its own copy of each message | Common (SNS, Service Bus topics, Pub/Sub); stops a topic mapping `direct` to a queue | none | Topics and Events | none |
| `serverless.container` | A container image the provider runs and scales with requests; it may scale to zero when idle (Cloud Run, Container Apps and Scaleway can, App Runner keeps at least one provisioned instance) | Common on GCP (Cloud Run) and Azure (Container Apps) | none | Serverless Containers | none |
| `registry.container` | A private registry of OCI container images the provider runs | Very common; two targets have a GA registry | Managed Private Registry (no 3-AZ variant: zone continuity not stated, milestone 5) | Container Registry | none (open beta) |
| `storage.file` | A shared file system the provider runs, mounted by several machines over NFS | Moderate (EFS, Filestore, Azure Files NFS); every target has one | none (File Storage Service: general availability not confirmed, milestone 5) | File Storage | File Storage |

The target offerings, with the documentation they were found in on 2026-11-24 (milestones 5 to 7
re-read each one and cite it in `SOURCES.md`):

- **`cache.redis`**
  - OVHcloud: Valkey 8.1 and 9.1, Discovery (1 node) and Production (2 nodes, "a primary node and a
    replica node"), in 1-AZ and 3-AZ regions; "Cluster mode (sharding) is not supported"; RDB
    persistence, no AOF; no read-only nodes.
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/valkey-capabilities>,
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/capabilities>,
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/faq> (automatic failover),
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/public-cloud-databases-regions-comparison>
    (3-AZ: "service continuity even if one zone experiences an outage"),
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/concepts-security-overview> (encryption
    at rest).
  - Scaleway: Redis 8.4; node types RED1-micro to RED1-XL; Standalone, High Availability (2 nodes,
    public beta since 2024-12-03, no GA entry found) and Cluster mode (3 to 6 nodes); zonal, in PAR1,
    PAR2, AMS1, AMS2, WAW1 and WAW2, not Milan.
    <https://www.scaleway.com/en/docs/managed-databases-for-redis/faq/>,
    <https://www.scaleway.com/en/docs/managed-databases-for-redis/concepts/>,
    <https://www.scaleway.com/en/docs/managed-databases-for-redis/how-to/create-a-database-for-redis/>,
    <https://www.scaleway.com/en/docs/managed-databases-for-redis/reference-content/ensuring-data-persistence/>.
  - UpCloud: Valkey 8.1 and 9.1; plans of 1, 2 or 3 nodes, 1 to 112 GiB; automatic failover on 2 and 3
    nodes, standby nodes on "physically separated backend hosts"; replica reads through a separate DNS
    name; encryption at rest always on; all eight EU zones.
    <https://upcloud.com/docs/products/managed-valkey/> and its `configurations/`,
    `high-availability/`, `encryption/` and `availability/` pages.
- **`database.mongodb`**
  - OVHcloud: MongoDB 7.0 and 8.0, Production (3 nodes) and Advanced (3 to 7 nodes, "PITR capability
    for the last 24 hours"), read-only nodes, 1-AZ and 3-AZ regions.
    <https://docs.ovhcloud.com/en/guides/public-cloud/databases/mongodb-concept-capabilities>.
  - Scaleway: Managed MongoDB®, GA since 2025-06-10, MongoDB 7.0 and 8.0, a 3-node replica set ("one of
    the remaining standby nodes … can take over"), snapshots.
    <https://www.scaleway.com/en/docs/managed-mongodb-databases/concepts/>,
    <https://www.scaleway.com/en/docs/managed-mongodb-databases/faq/>. Scaleway's older Document Database
    (FerretDB on PostgreSQL) has no product pages left and is not used.
  - UpCloud: none; its managed databases are PostgreSQL, MySQL, Valkey and OpenSearch.
- **`network.loadbalancer`**
  - OVHcloud: Public Cloud Load Balancer (OpenStack Octavia), HTTP, HTTPS, TCP, UDP and SCTP listeners,
    TLS termination, sizes S, M, L and XL; in 3-AZ regions "spread across three availability zones for
    high availability".
    <https://docs.ovhcloud.com/en/guides/public-cloud/network-services/load-balancer-concepts>,
    <https://docs.ovhcloud.com/en/guides/public-cloud/network-services/loadbalancer-update-size>.
  - Scaleway: Load Balancer, zonal, TCP and HTTP/HTTPS (no UDP), SSL offloading, sizes LB-S to LB-XL; on failure
    Scaleway spawns a replica and reroutes the flexible IP; IPv6 frontends.
    <https://www.scaleway.com/en/docs/load-balancer/faq/>,
    <https://www.scaleway.com/en/docs/load-balancer/reference-content/ssl-bridging-offloading-passthrough/>,
    <https://www.scaleway.com/en/docs/load-balancer/reference-content/load-balancers-limitations/>.
  - UpCloud: Managed Load Balancer, `tcp` (layer 4) and `http` (layer 7) frontends (no UDP mode found
    in the documentation; to be checked, and `udp` stays off the offering until a page states it), TLS termination,
    plans Essentials and Development (1 node) and Production S and M (2 and 5 nodes, "Forming a high
    availability cluster"); all eight EU zones.
    <https://upcloud.com/docs/products/managed-load-balancer/> and its `plans/` and `availability/`
    pages.
- **`messaging.topic`**
  - Scaleway: Topics and Events, "an in-house implementation of AWS SNS protocol", GA on 2024-06-25.
    <https://www.scaleway.com/en/docs/topics-and-events/faq/>.
  - OVHcloud and UpCloud: none (OVHcloud's only messaging product is Kafka, UpCloud has none).
- **`serverless.container`**
  - Scaleway: Serverless Containers, scaling on concurrent requests between a minimum and a maximum
    scale, "scale down to zero" at a minimum of 0.
    <https://www.scaleway.com/en/docs/serverless-containers/concepts/>.
  - OVHcloud: none. AI Deploy runs a customer's image but keeps at least one replica and does not scale
    per request (<https://docs.ovhcloud.com/en/guides/public-cloud/ai-machine-learning/ai-deploy-capabilities>).
  - UpCloud: none.
- **`registry.container`**
  - OVHcloud: Managed Private Registry (Harbor), plans Small, Medium and Large; in 3-AZ regions "Plans
    Medium and Large run in active-active mode across at least 2 AZs".
    <https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-private-registry/managed-private-registry-faq>.
    Milestone 5 (2026-11-25) records one offering with no 3-AZ variant: no quoted sentence says the
    registry keeps serving through the loss of a zone, so `zone-high-availability` is not listed.
  - Scaleway: Container Registry, in Paris, Amsterdam and Warsaw; €0.027 per GB and month stored for
    private images. <https://www.scaleway.com/en/docs/container-registry/faq/>.
  - UpCloud: Managed Container Registry is in open beta ("standard UpCloud SLAs do not apply"), so
    none until it is generally available. <https://upcloud.com/docs/products/container-registry/>.
- **`storage.file`**
  - OVHcloud: File Storage Service (OpenStack Manila), "managed NFS shares on private networks", NFSv4,
    optional encryption at rest, snapshots.
    <https://docs.ovhcloud.com/en/guides/storage-and-backup/file-storage/file-storage-service/getting-started>.
    Milestone 5 (2026-11-25) records no offering: general availability is not confirmed (roadmap issue
    #54 open, the product page's "other consumption modes coming at GA"), so OVHcloud shows fit `none`
    for `storage.file` until a refresh confirms it.
  - Scaleway: File Storage, GA in Paris (2026-07-16) and Amsterdam (2026-10-21), 25 GB to 50 TB,
    "triple replication" within one zone, encrypted at rest, snapshots, billed on provisioned space;
    mounted by Instances and Kapsule. <https://www.scaleway.com/en/docs/file-storage/faq/>,
    <https://www.scaleway.com/en/docs/file-storage/quickstart/>.
  - UpCloud: File Storage, NFS 4.1, 250 to 25,000 GiB, encryption at rest chosen at creation; in
    `de-fra1`, `dk-cph1`, `fi-hel1`, `fi-hel2`, `nl-ams1` and `se-sto1`.
    <https://upcloud.com/docs/products/file-storage/> and its `encryption-at-rest/` and `availability/`
    pages.

### How the sources classify

The `rawType` keeps the guide's conventions: the CloudFormation type on AWS, the Cloud Asset Inventory
asset type on GCP, the Resource Manager type on Azure. Rows marked *call* need a read-only service call
the guide does not make today (AWS Config does not record the type, or the category depends on a
setting).

| Category | AWS | GCP | Azure |
|---|---|---|---|
| `cache.redis` | `AWS::ElastiCache::ReplicationGroup`, and `AWS::ElastiCache::CacheCluster` not in a group, engine `redis` or `valkey` (*call*: `elasticache describe-replication-groups`, `describe-cache-clusters`); `AWS::ElastiCache::ServerlessCache`, engine `redis` or `valkey` (*call*: `describe-serverless-caches`) | `redis.googleapis.com/Instance`, `redis.googleapis.com/Cluster`, `memorystore.googleapis.com/Instance` | `Microsoft.Cache/redis`, `Microsoft.Cache/redisEnterprise` (*call*: its databases, `az rest --method get .../databases`, for clustering and persistence) |
| `database.mongodb` | `AWS::DocDB::DBCluster`, one resource per DocumentDB cluster: the `rds describe-db-instances` entries with engine `docdb` (*call*, already made, no new one), grouped by `DBClusterIdentifier`; the member instances are dropped, as an ElastiCache group's member clusters are | none (GCP runs no managed MongoDB) | `Microsoft.DocumentDB/databaseAccounts`, kind `MongoDB`; `Microsoft.DocumentDB/mongoClusters` |
| `network.loadbalancer` | `AWS::ElasticLoadBalancingV2::LoadBalancer`, type `application` or `network` (*call*: `elbv2 describe-load-balancers`, and for `udp` and `ipv6` `describe-listeners` and `describe-target-groups`, and `describe-tags` for the Kubernetes tag); `AWS::ElasticLoadBalancing::LoadBalancer` (*call*: `elb describe-load-balancers`, for its zones); AWS Config's `AWS::ElasticLoadBalancingV2::Listener`, `::ListenerRule` and `::TargetGroup` are parts of the load balancer and dropped (milestone 2) | `compute.googleapis.com/UrlMap`; `compute.googleapis.com/BackendService` that no URL map names; `compute.googleapis.com/TargetPool` (*call*: `compute url-maps list`, `backend-services list`) | `Microsoft.Network/loadBalancers`, SKU `Standard` or `Basic`; `Microsoft.Network/applicationGateways` |
| `messaging.topic` | `AWS::SNS::Topic` (*call*: `sns list-topics` and `sns get-topic-attributes` for `KmsMasterKeyId`; the guide's `select-resource-config` selects no configuration, and Config lists topics only where the optional recorder runs) | `pubsub.googleapis.com/Topic` (today `messaging.queue`) | `Microsoft.ServiceBus/namespaces/topics` (today `unclassified`) |
| `messaging.queue` (moved or new rows only) | | `pubsub.googleapis.com/Subscription` (unchanged) | `Microsoft.ServiceBus/namespaces/topics/subscriptions` (*call*: `az servicebus topic subscription list`, per topic, as the guide's `topic list` loop already runs; Resource Graph lists neither topics nor subscriptions, which is why the guide calls `az servicebus` today) |
| `serverless.container` | `AWS::AppRunner::Service` (AWS Config alone: the category carries no capability, so the type is enough) | `run.googleapis.com/Service` not backing a 2nd gen Cloud Function (today `unclassified`) | `Microsoft.App/containerApps` (kind `functionapp`: `serverless.function`; kind `workflowapp`: `unclassified`) |
| `registry.container` | `AWS::ECR::Repository` (AWS Config alone: its one capability, `encryption-at-rest`, holds for every repository) | `artifactregistry.googleapis.com/Repository`, format `DOCKER` (*call*: `artifacts repositories list`) | `Microsoft.ContainerRegistry/registries` |
| `storage.file` | `AWS::EFS::FileSystem` (*call*: `efs describe-file-systems` for size and settings) | `file.googleapis.com/Instance` (Filestore) | `Microsoft.Storage/storageAccounts/fileServices/shares` with protocol NFS (*call*: the shares list, `az rest --method get .../fileServices/default/shares`, the REST call `az storage share-rm list` wraps); a `FileStorage` account whose shares are listed is left out, as a Service Bus namespace is; `Microsoft.FileShares/fileShares` (NFS only, in Resource Graph) |

Left out on the source side, as the guide leaves out an EKS cluster's nodes today: load balancers that
a Kubernetes cluster created (AWS tag `elbv2.k8s.aws/cluster`, everything in an AKS node resource
group, which the guide already drops; milestone 2 found `kubernetes.io/cluster/<name>` documented by
AWS on subnets only, so it is not used on load balancers; GCP keeps them, as URL maps and backend
services carry no labels, see *To be checked*), a GCP forwarding rule and target proxy (the frontends
of a load balancer the URL map or backend service stands for), a GCP backend service of scheme
`INTERNAL_SELF_MANAGED` (a service mesh) and a URL map naming only such services, Artifact Registry
repositories of mode `REMOTE_REPOSITORY` or `VIRTUAL_REPOSITORY` (they store no image of their own),
Filestore backups (they belong to their instance), the parts and settings of a classified AWS resource
that AWS Config records as types of their own (an ELBv2 listener, listener rule and target group, an
EFS access point, an ECR registry policy, replication configuration, pull-through cache rule and
repository creation template; `AWS::ECR::PublicRepository` stays `unclassified`), the member clusters of
an ElastiCache replication group (the group stands for them), the member instances of a DocumentDB
cluster (the cluster stands for them), and the Cloud Run service behind a 2nd gen Cloud Function
(already dropped).

A DocumentDB cluster moves away from today's `AWS::RDS::DBInstance` on purpose. Every target sells
MongoDB as a replica set of three nodes or more, one service; classified per instance, a three-instance
cluster would be three resources, each matched to a three-node service, and the estimate and the
blockers would count the cluster three times. The cluster's `rawType` is its CloudFormation type,
`AWS::DocDB::DBCluster`, its `id` the `DBClusterIdentifier` and its region that of its instances. Aurora
stays per instance, as today: this PRD does not change it.

Capabilities the programs derive, from settings the calls above return:

| Category | Capability | When (examples; each milestone pins the exact rule) |
|---|---|---|
| `cache.redis` | `node-high-availability` | ElastiCache automatic failover on with a replica in every shard (two or more members in each node group; failover alone is not enough, a cluster-mode group must enable it with no replica), always on ElastiCache Serverless; Memorystore tier `STANDARD_HA` or a cluster with replicas; Azure Standard or Premium tier |
| `cache.redis` | `zone-high-availability` | ElastiCache Multi-AZ on, always on ElastiCache Serverless (three zones); Azure zones set on a Premium cache; Memorystore per its placement (to be checked) |
| `cache.redis` | `sharding` | ElastiCache cluster mode on (`ClusterEnabled`), always on ElastiCache Serverless (cluster mode only); `redis.googleapis.com/Cluster` or a sharded Valkey instance; Azure Premium with `shardCount`, or Azure Managed Redis in cluster policy `OSSCluster` |
| `cache.redis` | `snapshots` | ElastiCache snapshot retention above 0; Azure Premium RDB persistence; Memorystore RDB persistence |
| `cache.redis` | `encryption-at-rest` | ElastiCache at-rest encryption on (a group's `StorageEncryptionType`, the effective state, not `AtRestEncryptionEnabled`), always on ElastiCache Serverless; always on Memorystore and Azure, except Azure Cache for Redis Basic and Standard C0 and C1 |
| `database.mongodb` | as `database.*` today | DocumentDB, from the cluster's member instances: `point-in-time-restore` when every instance's `BackupRetentionPeriod` is above 0, `encryption-at-rest` when every instance's `StorageEncrypted` is true (no page says an instance reports its cluster's values), `read-replicas` and `node-high-availability` from two or more instances, `zone-high-availability` from instances in two or more `AvailabilityZone`s; Cosmos DB continuous backup for `point-in-time-restore`, `locations[].isZoneRedundant` true for `zone-high-availability` (not "more than one region", which is a regional replica, not zone redundancy; to be checked) |
| `network.loadbalancer` | `zone-high-availability` | an ALB or NLB in two or more zones; a GCP regional load balancer, or a global one (anycast across regions; it maps onto a regional target load balancer, as a DNS or CDN service would, so only its zone redundancy within a region is claimed, never its reach across regions); an Azure Standard load balancer with a zone-redundant frontend, or an Application Gateway in two or more zones |
| `network.loadbalancer` | `ipv6` | only where clients and backends are both reached over IPv6 (`ipv6` is inbound and outbound): an ALB or NLB with `IpAddressType` `dualstack` or `dualstack-without-public-ipv4` **and** a target group with `IpAddressType` `ipv6` (`describe-target-groups`); a GCP IPv6 forwarding rule that names a passthrough backend service (scheme `EXTERNAL` or `INTERNAL`) in `backendService`, or that reaches a proxy-based load balancer whose backend service sends to IPv6 backends (`ipAddressSelectionPolicy` `IPV6_ONLY` or `PREFER_IPV6`; checked in milestone 3); an Azure IPv6 frontend with an IPv6 backend pool (to be checked). A load balancer with an IPv6 frontend only gets no `ipv6` |
| `network.loadbalancer` | `udp` | an NLB with a listener of protocol `UDP`, `TCP_UDP`, `QUIC` or `TCP_QUIC` (`elbv2 describe-listeners`; QUIC runs over UDP); an Azure load balancer rule of protocol `Udp` or `All`; a GCP backend service of protocol `UDP`, or a forwarding rule of `IPProtocol` `UDP` (to a target pool or backend service; the forwarding rule is read, then dropped) |
| `messaging.topic` | `encryption-at-rest` | SNS with a `KmsMasterKeyId` (`sns get-topic-attributes`); always on Pub/Sub and Service Bus |
| `messaging.topic` | `dead-letter-queue` | never on SNS (milestone 2: a dead-letter queue is attached to a subscription, which no topic attribute says); per source, to be checked in milestones 3 (Pub/Sub) and 4 (Service Bus) (SNS and Pub/Sub set it per subscription, Service Bus subscriptions always have one) |
| `messaging.queue` | `dead-letter-queue` | a Service Bus topic subscription: always, as a Service Bus queue today |
| `registry.container` | `encryption-at-rest` | always: ECR, Artifact Registry and Azure Container Registry encrypt images at rest |
| `storage.file` | `encryption-at-rest` | EFS `Encrypted`; always on Filestore and Azure Files |
| `storage.file` | `zone-high-availability` | EFS Regional (not One Zone); Filestore tier `REGIONAL` or `ENTERPRISE`; Azure Files on ZRS |
| `storage.file` | `snapshots` | Filestore or Azure share snapshots exist |

`serverless.container` resources carry no capability: scaling with requests is what the category is.

### Estimates

| Category | Size fields (inventory version 2) | Priced by | Why |
|---|---|---|---|
| `cache.redis` | `memoryGiB` (per node), `count` (nodes), `usage.hoursPerMonth` | plan, on memory alone: the cheapest plan whose memory covers the resource's, its per-node price × `count` | Every source sizes a cache by memory (Memorystore `memorySizeGb`, Azure SKU capacity, an ElastiCache node type), and every target sells cache plans by memory; a vCPU count is rarely known and would make most caches "size unknown" |
| `database.mongodb` | as `database.postgresql`: `vcpus`, `memoryGiB`, `storageGiB`, `count` (nodes: a DocumentDB cluster's instances), `usage.hoursPerMonth` | plan, its per-node price × `count`, plus storage above what it includes | The targets price MongoDB per node flavour plus storage, like their other databases |
| `storage.file` | `storageGiB` (provisioned) | GiB | OVHcloud, Scaleway and UpCloud bill provisioned GiB (Scaleway: "billed based on the provisioned space") |
| `messaging.topic` | none | not estimated: usage-priced | Billed by bytes or messages delivered (Scaleway Topics and Events: "topic volume") |
| `serverless.container` | none | not estimated: usage-priced | Billed per GB-s and vCPU-s (Scaleway Serverless Containers) |
| `network.loadbalancer` | none | not estimated: not priced | A flat fee per size, and the size is chosen by bandwidth or connections, which no inventory field records |
| `registry.container` | none | not estimated: not priced | OVHcloud sells plans with included storage, Scaleway prices stored GB; one method would misprice one of them (decided question 6) |

Sizes the guide's programs fill in: a Memorystore instance's `memorySizeGb`, a node count where the
call gives it, an EFS file system's `SizeInBytes` (its metered size: EFS has no provisioned size, and
the guide says so), a Filestore share's `capacityGb`, an Azure share's
`shareQuota`, and a DocumentDB cluster's `count` from its instances (no `storageGiB`: its storage is the
cluster volume, which `AllocatedStorage` does not describe, as for Aurora today). ElastiCache, Azure Cache
and DocumentDB name a node type or instance class, not a memory size; the guide says to add `memoryGiB`
(and `vcpus` for DocumentDB) by hand, as it does for an RDS instance class.

**What `count` means for a cache or a MongoDB service.** A plan in the snapshot is one node, and its
price is per node: where a target's page prices a whole service (a 2-node Production Valkey, a 3-node
MongoDB set), the snapshot converts it to the per-node price, as ADR 0027 and
`internal/adapter/target/ovhcloud/SOURCES.md` (*Managed databases*) already do for databases, and the
`SOURCES.md` section says which. An estimate is that per-node price × the resource's `count`, its nodes,
whatever the tier's node count:

- `count` absent: priced as one node and shown with "(count assumed 1)", as for every category that takes a
  count today (ADR 0025).
- `count` below the tier's node count (a 1-node ElastiCache cluster pointed at OVHcloud's 2-node
  Production because it needs `node-high-availability`, a 1-instance DocumentDB cluster on a 3-node
  MongoDB set; UpCloud sells 1-, 2- and 3-node Valkey plans): priced at `count` nodes, so the estimate is
  below what the target bills. The minimum node count is a minimum billed size, which the snapshot format
  cannot record yet (decided question 10); the tier's `SOURCES.md` section names its node count.
- `count` above it: priced at `count` nodes, as today for databases, which overstates a tier of a fixed
  node count (OVHcloud Production); decided question 10 covers both directions.

The guide writes `count` for a replicated cache group and a DocumentDB cluster, unlike an RDS instance
with a standby (`aws.jq` leaves its count out): a cache replica and a DocumentDB instance are nodes the
source lists and runs, one each, while how many nodes an RDS standby becomes is the target's to say. The
count is the group's nodes, primary and replicas of every shard (ElastiCache `MemberClusters`, Memorystore
`replicaCount` + 1 per shard, Azure's shards × (1 + replicas)), or the cluster's instances.

"Not priced" is a new not-estimated reason, code `not-priced` in the JSON report and "not priced" in the
Markdown and HTML reports, as the other reasons are worded in plain text there
(`internal/adapter/report/markdown/estimate.go`: "size unknown", "no fit", ...): today a category that
has a fit but is never priced gets `usage-priced`, which would be wrong for a load balancer. A new value of the reason set
keeps the JSON report's `schemaVersion` (*Decisions*). `estimate.NotPriced` already names the `Method`
of value `"not-priced"` (`internal/domain/estimate/estimate.go`), so the new `Reason` needs another Go
identifier, which milestone 1 chooses; its JSON code stays `not-priced` and its Markdown text "not
priced".

### Left out

| Candidate | Why left out |
|---|---|
| DNS zones (Route 53, Cloud DNS, Azure DNS) | A zone maps cleanly (zone files are standard, OVHcloud and Scaleway host external zones by API), but it is a global service: an offering has no region, and ADR 0007 needs at least one declared EU region per offering, which for an anycast service would claim a residency it does not have. UpCloud offers none ("We do not currently offer a user-configurable authoritative DNS service", <https://upcloud.com/docs/products/networking/dns/>). Needs region semantics for global services first (decided question 2) |
| CDNs (CloudFront, Cloud CDN, Front Door, Azure CDN) | Global, as DNS, and too vague to assess: Scaleway Edge Services fronts only Scaleway origins (<https://www.scaleway.com/en/docs/edge-services/faq/>), UpCloud CDN is a closed beta with no API (<https://upcloud.com/docs/products/cdn/>), OVHcloud CDN Infrastructure is a Network product sold by traffic quota (<https://docs.ovhcloud.com/en/guides/network/cdn/quota>); on GCP Cloud CDN is a flag on a backend, not a resource; CloudFront features (edge functions, WAF) have no vocabulary |
| Managed search, `search.opensearch` (OpenSearch Service) | Well bounded and served by OVHcloud (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/opensearch-capabilities>) and UpCloud (<https://upcloud.com/docs/products/managed-opensearch/>), Scaleway's status to be checked; but only AWS has the source, and it is the least common family. First candidate for a follow-up PRD (decided question 1) |
| Kafka, `messaging.kafka` (MSK, GCP Managed Kafka; Event Hubs' Kafka API) | OVHcloud only (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/kafka-capabilities>); Scaleway Clusters for Kafka's status to be checked; few source resources run Kafka itself. Follow-up candidate (decided question 1) |
| Other streaming and event buses (Kinesis, EventBridge, Event Grid, Event Hubs) | No target equivalent; each is its own model. Stay `unclassified` |
| Key-value and wide-column databases (DynamoDB, Bigtable, Firestore, Keyspaces, Cosmos DB NoSQL, Cassandra, Table and Gremlin APIs) | No target runs one (OVHcloud's index lists none; UpCloud and Scaleway none). Stay `unclassified`, which is the truthful verdict |
| MemoryDB | A durable primary database with a multi-AZ transaction log, not a cache: a target cache that snapshots every 12 hours would map `direct` and hide the loss of durability, and no capability expresses it. Stays `unclassified` (decided question 3) |
| Memcached (ElastiCache, Memorystore) | No target runs Memcached; it is not the Redis protocol. Stays `unclassified` |
| MariaDB, SQL Server, Oracle (RDS, Cloud SQL, Azure SQL, SQL Managed Instance) | No target runs them as a public cloud managed database. OVHcloud's MariaDB is a Web Cloud Databases (web hosting) product. Stay `unclassified` |
| ECS services and clusters, Fargate | An orchestrator of long-running tasks, not request-scaled containers; the move is a re-platforming (to `kubernetes.managed` or `serverless.container`) the user decides, not a like-for-like mapping. Stays `unclassified` (decided question 8) |
| App Service web apps (`Microsoft.Web/sites`, kinds other than `functionapp`) | A runtime-stack PaaS with no target equivalent; too vague as a category. Stays `unclassified` |
| Cloud Run jobs, Container Instances, Batch | Run-to-completion jobs; no category. Scaleway Serverless Jobs could serve a later `serverless.job` |
| FSx (Windows, Lustre, ONTAP, OpenZFS) and SMB shares (Azure Files SMB) | AWS Config does not record FSx; SMB and Lustre have no target equivalent in the public clouds. Stay `unclassified` or out of the inventory, as today |
| AWS Gateway Load Balancer, Azure Gateway load balancer | Appliance insertion, not load balancing to backends; no target equivalent |
| A `containers` category covering all of the above | Too vague: it would map an App Service plan, an ECS cluster and a Cloud Run service to the same offerings |

## Acceptance criteria

All criteria are pinned by tests that run offline under `make test` or `make test-e2e`, unless marked
*review*.

**Taxonomy and inventory**

- [x] An inventory (schema version 1 or 2) with a resource of category `cache.redis`,
  `database.mongodb`, `network.loadbalancer`, `messaging.topic`, `serverless.container`,
  `registry.container` or `storage.file` is read, and `shift2eu assess` exits 0. An unknown value such
  as `cache.memcached` is still refused with exit 1 and "unknown taxonomy category".
- [x] A version 2 resource of a new category takes exactly the size fields under *Estimates*. Any other
  size or usage field (`vcpus` on `cache.redis`, `storageGiB` on `network.loadbalancer`, any size on
  `messaging.topic`) is refused with exit 1 and a message naming the field by its path.
- [ ] Given a resource that lists `sharding` or `udp`, the inventory is read; an offering that does not
  list it gives fit `partial` naming it as missing (a UDP load balancer on Scaleway is `partial`, missing
  `udp`).
- [ ] Inventories with only the nine existing categories keep their reports: in milestones 1 to 4 no
  existing golden file of such an inventory changes (the e2e `assess-*` goldens, the report adapters'
  goldens; the `examples/` goldens change in milestone 1 only because the samples gain resources of the
  new categories); in milestones 5 to 7 such a golden may change only in its catalog line's or `snapshot`
  object's source and retrieved date, and in exactly the amounts of an existing offering whose price or
  plan the re-read page shows changed. Each snapshot has one source and one retrieved date (ADR 0007),
  so a milestone that re-reads a target's price page on a new date updates every existing price or plan
  the page now shows differently to its current value, rather than keep a dated price the page no longer
  shows, and says so in its PR and in the target's `SOURCES.md`; no other existing offering, plan, price
  or default region changes.
- [x] The sample inventories under `examples/` together use every taxonomy category, the new ones
  included (the existing check in `test/examples`).

**Mapping and report**

- [ ] Given a resource of a new category and a target with no offering for it, its fit is `none`, the
  reason is "no offering in the <target> catalog for `<category>`", and it is listed as a blocker. This
  holds for `database.mongodb`, `serverless.container` and `registry.container` on UpCloud, and for
  `messaging.topic` and `serverless.container` on OVHcloud and UpCloud, and for `storage.file` on
  OVHcloud (milestone 5: general availability not confirmed).
- [ ] Given a resource of each new category and a target that serves it, the report names the best
  offering, with fit `direct` when the offering lists every capability the resource lists and
  `partial` naming each missing capability otherwise. A test per target pins the best offering for a
  resource with no capability and for one with each capability a tier variant exists for (for example
  `zone-high-availability` on a `cache.redis` resource points at an OVHcloud 3-AZ Valkey variant).
- [x] The JSON report carries the new category values in `category` and keeps `schemaVersion` 1, 2
  and 3 as today; every golden document validates against its schema.

**Estimates**

- [x] A `cache.redis` resource with `memoryGiB` and `count` is priced at the per-node price of the
  cheapest plan of the best offering whose memory covers it, × `count`, scaled by `hoursPerMonth` as
  other plan-priced categories are; without `count` it is priced as one node with "(count assumed 1)";
  without `memoryGiB` it is "not estimated: size unknown".
- [x] A `database.mongodb` resource is priced as a `database.postgresql` resource is, plan plus
  storage above what the plan includes.
- [x] A `storage.file` resource with `storageGiB` is priced per GiB from the offering's storage price.
- [x] A `messaging.topic` or `serverless.container` resource with a fit says "not estimated:
  usage-priced"; a `network.loadbalancer` or `registry.container` resource with a fit is not estimated
  with reason "not priced" in the Markdown and HTML reports and `not-priced` in the JSON report. None is
  counted as 0 in a total.
- [ ] The per-category estimate table in `test/examples` has a representative resource for every new
  priced category each snapshot offers, and pins its plan and amount.

**Inventory guide**

- [ ] Run as the guide runs them, against recorded, sanitised fixtures, each source's jq program
  classifies every `rawType` in *How the sources classify* into its category, with the capabilities
  and sizes the tables give, and writes a schema version 2 inventory the binary accepts (the existing
  e2e test, with its category list per source from milestone 1: GCP has no `database.mongodb`).
- [ ] A GCP Pub/Sub topic is `messaging.topic` and its subscription stays `messaging.queue`; an Azure
  Service Bus topic is `messaging.topic` and its subscription is `messaging.queue` with
  `dead-letter-queue`; a Cloud Run service that is not behind a Cloud Function is
  `serverless.container`.
- [x] A DocumentDB cluster of three instances is one `database.mongodb` resource, `rawType`
  `AWS::DocDB::DBCluster`, `count` 3; its instances are not in the inventory.
- [ ] A load balancer with an IPv6 frontend and IPv4 backends only has no `ipv6`; one with a UDP
  listener or rule has `udp`.
- [ ] An ElastiCache Memcached cluster, a MemoryDB cluster, a DynamoDB table, an RDS MariaDB instance and
  an Azure Files SMB share in the fixtures stay `unclassified` (MemoryDB and Memcached only when
  collected); a load balancer a Kubernetes cluster created (AWS, Azure; GCP keeps them, see *To be
  checked*), a GCP forwarding rule, an ElastiCache group's
  member cluster and a DocumentDB cluster's member instance are not in the inventory.
- [ ] Every new command in the guide only lists, describes or gets, and the AWS policy, GCP roles and
  Azure role in the guide cover it (*review*, and the existing `cloud-read-only` checks where they apply).
- [ ] The site's inventory page shows the guide's new commands (`test/site`, which compares the page
  with the guide).

**Catalogs**

- [ ] Each target's catalog serves the new categories its column under *The new categories* names, each
  with at least one offering (`targettest` *category* check), only in declared EU regions (*regions*
  check), and no other new category: `targettest` only asserts that the listed categories are served, so
  the parsed-catalog golden below is what pins the absence of the others.
- [ ] Every new offering has its own section in its target's `SOURCES.md`, naming the pages it comes
  from, the fact behind each capability, region and price, and each source's licence (the existing
  per-offering section test). Every capability that holds on some plans only is on a tier variant
  (ADR 0011), and `zone-high-availability` is listed only where a quoted source says the service keeps
  serving through the loss of a zone.
- [ ] A golden file per target pins the parsed catalog with the new offerings.
- [ ] Each target's `SOURCES.md` *Left out on purpose* names every new category it does not serve, with
  the pages checked (for example UpCloud's Container Registry, open beta).
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
milestone 1. Milestones 2 to 4 (the guide) and 5 to 7 (the catalogs) are independent of one another and
can run in parallel: milestone 1 makes the e2e category list per source, so the guide milestones touch
no shared list. Until a target's catalog milestone lands, its report shows the new categories as
fit `none` with "no offering in the <target> catalog", which is already more precise than
`unclassified`.

1. [x] **Taxonomy, inventory and estimates.** The seven categories in `internal/domain/taxonomy`, with
   their definitions; the `sharding` and `udp` capabilities and the widened `snapshots` definition
   (decided question 4); the size-field table for the new categories (`internal/domain/inventory`); memory-only
   plan matching for `cache.redis`, plan pricing for `database.mongodb`, GiB pricing for
   `storage.file`, and the `not-priced` reason (`internal/domain/estimate`; its Go identifier is
   milestone 1's choice, distinct from the existing `NotPriced` method, and a never-priced category with
   a fit gets this reason instead of `UsagePriced`), rendered in the Markdown,
   JSON and HTML reports; the JSON schemas' descriptions of the reason set. One ADR records the
   categories, the capabilities `sharding` and `udp` and the widened `snapshots`, the field table (amending ADR 0025), the memory-only matching (amending
   ADR 0027) and why no format version changes. The samples under `examples/` gain one resource per
   new category, and their goldens change. The guide's lists of categories and capabilities and its
   *Sizes* table name the new ones. A refactor with no new category: `test/e2e/inventory_guide_test.go`'s
   one shared `taxonomyCategories()` list, compared with exact equality, becomes one list per source
   (each holding today's categories that source's fixtures cover), so milestones 2 to 4 each add only
   their own source's entries and do not conflict. Before the ADR is written, M1 reads the three
   targets' cache documentation for a vCPU count per plan; if a target gives none (a memory-only plan),
   the ADR decides the plan format for `cache.redis` (for example a plan whose vCPU count is optional for
   a memory-matched category), since `catalog.Plan` requires at least 1 vCPU today; no milestone invents
   a vCPU count. Checked on 2026-11-24 (*To be checked*): Scaleway's pages were not reachable, so
   [ADR 0046](../docs/adr/0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md) makes a
   plan's `vcpus` optional on a `cache.redis`-only offering and names the new reason
   `estimate.NeverPriced`.
2. [x] **AWS classification in the guide.** New read-only calls for ElastiCache (replication groups,
   cache clusters, serverless caches), ELBv2 (`describe-load-balancers`, `describe-listeners`,
   `describe-target-groups`, and `describe-tags` for the Kubernetes tag, with
   `elasticloadbalancing:DescribeTags`), Classic ELB (`elb describe-load-balancers`), EFS and SNS (`list-topics`, `get-topic-attributes`), with their IAM actions
   (`elasticloadbalancing:DescribeLoadBalancers`, `elasticloadbalancing:DescribeListeners`,
   `elasticloadbalancing:DescribeTargetGroups`, `sns:ListTopics`, `sns:GetTopicAttributes` and the
   ElastiCache and EFS describe actions) in the guide's policy; App Runner and ECR need none, AWS Config's
   type is enough; `aws.jq` classifies the AWS column of *How the sources classify* and drops what is
   left out; DocumentDB instances from the existing `rds describe-db-instances` become one
   `database.mongodb` cluster each; tables, recorded fixtures, the e2e golden and the AWS entries of the
   e2e category list updated. Depends on 1. Done on 2026-11-25 with the rules *To be checked*,
   Milestone 2 records.
3. [x] **GCP classification in the guide.** The asset types of the GCP column, Pub/Sub topics moved to
   `messaging.topic`, Cloud Run services to `serverless.container`, the calls for Memorystore sizes, URL
   maps, backend services and forwarding rules (read for protocol and IPv6, then dropped), Artifact
   Registry formats and Filestore capacity, with the roles they need; fixtures, golden and the GCP entries
   of the e2e category list. Depends on 1.
4. [x] **Azure classification in the guide.** The types of the Azure column, Service Bus topics to
   `messaging.topic` and their subscriptions to `messaging.queue` (`az servicebus topic subscription
   list` in the existing loop), the Resource Graph projection extended to the properties read (cache SKU
   and zones, Cosmos DB `kind`, capabilities and `locations[].isZoneRedundant`, load balancer SKU,
   frontends, backend pools and rule protocols), the file share loop; fixtures, golden and the Azure
   entries of the e2e category list. Depends on 1. Checked on 2026-11-25 (*To be checked*): Resource
   Graph lists neither classic file shares nor Redis Enterprise databases, so the guide lists shares
   with `az rest` (snapshots included) and adds one `az rest` read of each Azure Managed Redis
   cluster's databases; `Microsoft.FileShares/fileShares` (NFS only, in Resource Graph) is
   `storage.file` too; a container app's `kind` splits it three ways; Azure Cache for Redis C0 and C1
   have no encryption at rest.
5. [x] **OVHcloud catalog.** Offerings for `cache.redis` (Valkey Discovery, Production and their 3-AZ
   variants), `database.mongodb` (Production, Advanced, 3-AZ variants), `network.loadbalancer` (1-AZ
   and 3-AZ, with `udp`), `registry.container` (Managed Private Registry, 3-AZ variant for Medium and Large) and
   `storage.file` (File Storage Service), with plans and prices for the priced ones; `SOURCES.md`,
   golden, `targettest` categories. Pages needed: the documentation above (fetchable), the prices page
   <https://www.ovhcloud.com/en-ie/public-cloud/prices/> (Valkey, MongoDB, File Storage, Load Balancer
   and Managed Private Registry sections) and the regions page
   <https://www.ovhcloud.com/en/public-cloud/regions-availability/>, fetched as before or, if they
   refuse an automated client, saved by the maintainer. An existing price or plan the re-read page
   shows changed is updated to its current value (*Acceptance criteria*, goldens). Depends on 1.
   Done on 2026-11-25 with two departures, each a fact not confirmed (*To be checked*): no
   `storage.file` offering, since File Storage Service's general availability is not confirmed, and one
   Managed Private Registry offering with no 3-AZ variant, since no quoted page says it keeps serving
   through the loss of a zone. Both are under *Left out on purpose* in OVHcloud's `SOURCES.md`, with what
   reverses them. The re-read prices page changed no existing price or plan.
6. [ ] **Scaleway catalog.** Offerings for `cache.redis` (Managed Database for Redis™; a cluster-mode
   variant with `node-high-availability` and `sharding`), `database.mongodb` (Managed MongoDB®),
   `network.loadbalancer`, `messaging.topic` (Topics and Events), `serverless.container`,
   `registry.container` and `storage.file`, with plans and prices for the priced ones. **Gated on the
   maintainer saving, from a logged-out browser, into a scratch directory outside the repository (as
   `internal/adapter/target/upcloud/SOURCES.md` describes):**
   <https://www.scaleway.com/en/pricing/managed-databases/> (Redis™ and MongoDB® node types and
   storage), <https://www.scaleway.com/en/pricing/storage/> (File Storage; to be checked that it is
   there) and <https://www.scaleway.com/en/product-availability-by-region/> (the regions of every new
   product). The load balancer, Topics and Events, Serverless Containers and Container Registry pages
   are not needed: decided question 6 leaves load balancers and registries unpriced, and topics and
   serverless containers are usage-priced. An existing price or plan the re-read pages
   show changed is updated to its current value (*Acceptance criteria*, goldens). Depends on 1.
7. [ ] **UpCloud catalog.** Offerings for `cache.redis` (Managed Valkey: 1 node, and a multi-node
   variant with `node-high-availability`), `network.loadbalancer` (Managed Load Balancer, and a
   Production variant if its failover is confirmed; `udp` only if a page states it) and `storage.file` (File Storage),
   with plans and prices for Valkey and File Storage. **Gated on the maintainer saving
   <https://upcloud.com/pricing/>, EUR edition, logged out, as before:** the Managed Databases tab's
   Valkey table and the File Storage table (and the Managed Load Balancer table only if load balancers
   become priced). An existing price or plan the re-read page shows changed is updated to its current
   value (*Acceptance criteria*, goldens). Depends on 1.
8. [ ] **Comparison, docs and close.** An e2e fixture inventory with a resource of every new category,
   assessed against all three targets, golden for Markdown and JSON, so each target's `direct`,
   `partial` and `none` for the new categories show side by side; the guide's *Everything else on AWS*
   text, the README and the `CLAUDE.md` status line updated. Moves this PRD to `prds/done/` and closes
   #552. Depends on 2 to 7.

## Decisions and decided questions

**Decisions**

- **Seven categories, by protocol or by kind of product.** A category is a thing a user can move like
  for like and a target sells as one product. `cache.redis` and `database.mongodb` are named after the
  protocol, as `database.postgresql` and `database.mysql` are: an application's client library decides
  what it can move to. `network.loadbalancer`, `messaging.topic`, `serverless.container`,
  `registry.container` and `storage.file` are named after the kind of product, as `messaging.queue` is:
  their APIs differ between every source and target, and the move rewrites configuration, not
  application code. The new families (`cache`, `network`, `registry`) follow `<family>.<kind>`.
- **`cache.redis` holds Redis and Valkey, one category.** Valkey is a fork of Redis OSS 7.2 and speaks
  the same protocol; OVHcloud and UpCloud run Valkey, Scaleway runs Redis 8.4. Splitting them would
  give a Redis cache fit `none` on two targets that serve it. Redis 8 features beyond the shared protocol,
  and Azure Managed Redis modules, have no capability; the guide says so. Not `database.redis`: on every
  source and target the product is sold and run as a cache. Not `cache.inmemory`: Memcached clients
  cannot talk to Valkey, so one category for both would map Memcached `direct` to a product that cannot
  serve it.
- **A topic is not a queue.** `messaging.topic` is distinct from `messaging.queue`: a queue gives each
  message to one consumer, a topic copies it to every subscription. GCP Pub/Sub's topic becomes
  `messaging.topic` and its subscription stays `messaging.queue`, as a subscription is the queue a
  consumer reads; both still have to move. An Azure Service Bus topic subscription is the same thing (it
  holds its own copy of each message for its receivers, with a dead-letter subqueue), so it becomes
  `messaging.queue` too. Resource Graph does not list it; the guide's existing per-namespace
  `az servicebus` loop gains a per-topic `topic subscription list`, a read, expected under `Reader` (to
  be checked in milestone 4). SNS
  subscriptions are not resources that hold messages (they forward to an SQS queue, a Lambda or an HTTP
  endpoint, which are listed on their own), so they stay out.
- **`serverless.container` is request-scaled containers only.** "Containers" is too vague: an App Service
  plan, an ECS cluster and a Cloud Run service have no common target. The category is the Cloud Run
  shape (a container scaled by requests), which Scaleway Serverless Containers serves. Scaling to zero
  is not part of the definition, so App Runner, which keeps at least one provisioned instance, belongs.
- **`storage.file` is NFS.** EFS, Filestore and the three targets' file storage are NFS; SMB has no
  target equivalent, so SMB shares stay `unclassified` instead of mapping `direct` to an NFS share.
- **No new capability but `sharding` and `udp` (decided question 4).** Every other capability the new
  categories need is in the vocabulary: `node-high-availability`, `zone-high-availability`,
  `encryption-at-rest`, `read-replicas`, `point-in-time-restore`, `ipv6`, `dead-letter-queue` and
  `snapshots`. `snapshots` is widened from "a volume or instance" to "a volume, instance, file share or
  data set", so a file share snapshot and a cache's RDB snapshot count; this changes nothing for the
  existing categories. `udp`, "forwards UDP datagrams", exists because the targets differ: OVHcloud's load
  balancer has UDP listeners, Scaleway's does TCP and HTTP/HTTPS only, UpCloud's documents `tcp` and
  `http` modes; without it a UDP Network Load Balancer would map `direct` to Scaleway. TCP versus HTTP
  needs no capability: every target load balancer does both.
- **`read-replicas` stays a database capability.** It is defined as "serving reads from replicas of a
  primary database", and no source rule derives it for `cache.redis`, so no cache offering lists it (not
  UpCloud's multi-node Valkey, though it has replica reads): a capability no resource can list would only
  blur the definition. A cache's replicas are counted by `node-high-availability` and by `count`.
- **`ipv6` keeps its strict meaning.** It is inbound and outbound (`internal/domain/taxonomy`, ADR 0011),
  so a load balancer gets it only when its frontend and its backends both speak IPv6; an IPv6 frontend
  over IPv4 backends is a common setup a target's IPv4-only backend network serves, and crediting it
  would misreport a move as needing IPv6.
- **No inventory format version.** ADR 0006 and ADR 0007 leave the valid category values to the
  domain, and `taxonomy.ParseCategory` accepts a new value in a version 1 or 2 file; ADR 0023 added
  `compute.baremetal` the same way. The size-field table gains rows for the new categories only, so no
  existing file changes meaning. ADR 0025 asks for a new ADR when the table changes, and a new format
  version only "once `shift2eu discover` writes files", which it does not yet. As in ADR 0023, an older
  binary refuses an inventory that uses a new category with "unknown taxonomy category".
- **No JSON report `schemaVersion` bump.** The schemas type `category` as a string with no `enum`, and
  ADR 0023's new category kept `schemaVersion`. The estimate `reason` is an open set ("A consumer must
  accept a reason it does not know"), and ADR 0018 keeps the version for a new value of an open set,
  with the schema files' descriptions updated in the same change. No key is added, removed or retyped.
- **No catalog snapshot format change, if every cache page gives vCPUs.** Offerings name categories the
  domain validates (ADR 0007); cache plans carry vCPU and memory like database plans, and a plan's price
  is per node (*Estimates*); unpriced offerings have no plans, as Scaleway Queues has today.
  `catalog.Plan` requires at least 1 vCPU; if a target's cache page gives memory only, milestone 1's ADR
  decides the format for such a plan before any catalog milestone (milestone 1). Decided in ADR 0046:
  Scaleway's cache pages could not be read for vCPUs, so a plan's `vcpus` is optional on an offering
  whose every category is `cache.redis` (absent means not given; a present value is at least 1), and the
  snapshot format stays at version 2.
- **DocumentDB per cluster.** A DocumentDB cluster is one `database.mongodb` resource
  (`AWS::DocDB::DBCluster`, `count` its instances), not one per instance as today's `unclassified`
  `AWS::RDS::DBInstance` rows are: the targets sell MongoDB as one replica-set service, and per-instance
  rows would multiply the estimate and the blockers (*How the sources classify*).
- **GA only, and the target rule.** As in PRDs #11 and #77: a beta product (UpCloud Container Registry
  and CDN, Scaleway Redis 2-node High Availability while it is in public beta) is not an offering or
  not a tier, and a capability is listed only where the documentation states it (ADR 0011).
- **The guide can ship before the catalogs.** A classified resource with no offering yet is fit `none`
  with a reason naming its category, never worse than `unclassified`. So milestones 2 to 4 do not wait
  for the hand-saved pages, and the catalogs do not wait for the guide.
- **One saving session per target.** The catalog milestones are per target, not per category, so the
  maintainer saves each target's price pages once.

**Decided by the maintainer** (2026-11-24, as recommended: each question keeps its recommendation's
text as the decision)

1. **Are these seven the right first set?** OpenSearch (`search.opensearch`: OVHcloud and UpCloud,
   AWS only as a source) and Kafka (`messaging.kafka`: OVHcloud, Scaleway to be checked) are well
   bounded but narrower. *Decision (2026-11-24, decided by the maintainer as recommended):* yes, these seven; OpenSearch and Kafka in a follow-up PRD
   once Scaleway's Cloud Essentials for OpenSearch and Clusters for Kafka are confirmed GA.
2. **DNS and CDN.** *Decision (2026-11-24, decided by the maintainer as recommended):* a follow-up PRD on global services that first decides how a
   snapshot records an offering with no region (and what `--region` does with it), then adds public DNS
   zones (`network.dns`, OVHcloud and Scaleway). CDNs stay out even then.
3. **MemoryDB.** *Decision (2026-11-24, decided by the maintainer as recommended):* stays `unclassified`, as decided above; revisit only with a
   durability capability.
4. **Add `sharding` and `udp`?** A Redis Cluster client cannot use a non-clustered cache: OVHcloud
   Valkey says "Cluster mode (sharding) is not supported", Scaleway has Cluster mode. Without the
   capability a clustered ElastiCache group maps `direct` to OVHcloud. Likewise a UDP service (DNS, game
   servers, syslog, QUIC) behind an NLB or Azure load balancer would map `direct` to Scaleway's TCP and
   HTTP load balancer. *Decision (2026-11-24, decided by the maintainer as recommended):* add both: `sharding`, "data is partitioned across several
   primary nodes that clients address", used by `cache.redis` now and available to `database.mongodb`
   later; `udp`, "forwards UDP datagrams", used by `network.loadbalancer`, listed on OVHcloud and on
   UpCloud only if its documentation states it.
5. **Memory-only plan matching for `cache.redis`.** It is a second plan-matching rule in the domain.
   *Decision (2026-11-24, decided by the maintainer as recommended):* accept it; the alternative (vCPU and memory, both required) leaves almost every
   cache "size unknown".
6. **Load balancers and registries not priced.** *Decision (2026-11-24, decided by the maintainer as recommended):* `not-priced` for both in this PRD.
   Pricing a load balancer needs a size field the sources do not give; pricing a registry by stored GiB
   fits Scaleway but not OVHcloud's plans. A follow-up issue can add a registry storage estimate.
7. **Reclassify GCP Pub/Sub topics.** It changes what the guide writes for an existing GCP estate.
   *Decision (2026-11-24, decided by the maintainer as recommended):* yes; today's `messaging.queue` lets a topic map `direct` to a queue.
8. **ECS, Fargate and App Service web apps.** *Decision (2026-11-24, decided by the maintainer as recommended):* keep them `unclassified`; mapping them
   would choose a re-platforming for the user.
9. **Names.** `cache.redis` (rather than `cache.valkey` or `cache.keyvalue`), `network.loadbalancer`
   (rather than `loadbalancer.managed`), `registry.container` (rather than `container.registry`).
   *Decision (2026-11-24, decided by the maintainer as recommended):* as named.
10. **Minimum billed sizes and fixed node counts, in both directions.** UpCloud File Storage starts at
    250 GiB, so a 50 GiB EFS file system priced per GiB is underestimated; likewise a 1-node cache on
    OVHcloud's 2-node Production tier, or a 1-instance DocumentDB cluster on a 3-node MongoDB set, is
    priced at its own `count`, below what the target bills. The other way, a 3-node cache on OVHcloud's
    Production tier, which is exactly 2 nodes, is priced at 3 nodes, above what the target bills
    (*Estimates*). *Decision (2026-11-24, decided by the maintainer as recommended):* record each minimum and each fixed node count (GiB, nodes) in
    `SOURCES.md`, say so in the guide, and open one follow-up issue for minimum billed sizes and fixed or
    bounded node counts in snapshot format v2; do not block this PRD on it. The guide (milestone 2)
    already says a target's `SOURCES.md` names each minimum and fixed node count once the target serves
    the category: the catalog milestones 5 to 7 must make that true for every new offering.
11. **Scaleway Redis High Availability.** Only Cluster mode is GA and highly available, and it needs
    a cluster-aware client, so a non-sharded cache that needs `node-high-availability` would be
    pointed at a tier its client may not use. *Decision (2026-11-24, decided by the maintainer as recommended):* list only Cluster mode as the highly
    available tier until the 2-node High Availability topology leaves public beta, and have the tier's
    `SOURCES.md` section and the report's tie or required change say that Cluster mode needs a
    cluster-aware client; if that cannot be said without a capability, Scaleway Redis lists no
    `node-high-availability` until then.

**To be checked** (each in the milestone named; a fact not confirmed stays out of the snapshot)

- Milestone 1, checked on 2026-11-24 (public pages, read-only): whether each target's cache
  documentation or price page gives a vCPU count per plan.
  - OVHcloud Valkey: given on the price page, a `vCore` column per node (Discovery B3-4: 4 GB, 1 vCore;
    Production B3-8: 8 GB, 2 vCores), <https://www.ovhcloud.com/en-ie/public-cloud/prices/> (`#valkey`);
    not on <https://docs.ovhcloud.com/en/guides/public-cloud/databases/valkey-capabilities>, which
    refers to the price page.
  - Scaleway Redis™ (RED1-micro to RED1-XL): page not reachable (HTTP 403 to an automated client):
    <https://www.scaleway.com/en/docs/managed-databases-for-redis/concepts/>, `faq/` and
    `how-to/create-a-database-for-redis/`.
  - UpCloud Managed Valkey: given, a "CPU cores" column per plan (`1x1xCPU-1GB`: 1 GiB, 1 core;
    `2x4xCPU-28GB`: 28 GiB, 4 cores), <https://upcloud.com/docs/products/managed-valkey/configurations/>.
  - So the plan format of
    [ADR 0046](../docs/adr/0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md) applies:
    `vcpus` optional on a `cache.redis`-only offering; milestone 6 checks the Scaleway node types again.
- Milestone 2: that every DocumentDB instance in `rds describe-db-instances` (engine `docdb`) carries a
  `DBClusterIdentifier` and the `BackupRetentionPeriod` and `StorageEncrypted` of its cluster, and
  whether AWS Config lists the cluster (as `AWS::RDS::DBCluster` or a DocumentDB type) so that the program
  must drop it; which ElastiCache settings give node and zone high availability for a serverless cache;
  that `TCP_UDP` and `UDP` are the NLB listener protocols that carry UDP, and whether NLB `QUIC`/`TCP_QUIC`
  listeners count as UDP; that a target group's `IpAddressType` `ipv6` is what sends to IPv6 targets;
  the SNS rule for a topic's `dead-letter-queue` (a redrive policy is set per subscription).
  Checked on 2026-11-25 (public AWS documentation, read-only, no AWS call):
  - DocumentDB: `rds describe-db-instances` returns DocumentDB instances, engine `docdb`
    (<https://docs.aws.amazon.com/documentdb/latest/developerguide/db-instance-view-details.html>,
    <https://docs.aws.amazon.com/cli/latest/reference/docdb/describe-db-instances.html>); an instance's
    `DBClusterIdentifier` names "the cluster that the instance is a member of", and no page describes an
    instance outside a cluster
    (<https://docs.aws.amazon.com/documentdb/latest/developerguide/API_DBInstance.html>,
    <https://docs.aws.amazon.com/documentdb/latest/developerguide/how-it-works.html>), so `aws.jq` groups
    by `DBClusterIdentifier`, falling back to the instance's own identifier. That the instance carries
    its cluster's `BackupRetentionPeriod` and `StorageEncrypted` is **not confirmed** (both are cluster
    settings, <https://docs.aws.amazon.com/documentdb/latest/developerguide/API_DBCluster.html>), so with
    no new call a cluster gets `point-in-time-restore` or `encryption-at-rest` only when every instance
    reports it. AWS Config records the cluster as `AWS::RDS::DBCluster` and has no DocumentDB type
    (<https://docs.aws.amazon.com/config/latest/developerguide/docdb-cluster-encrypted.html>,
    <https://docs.aws.amazon.com/config/latest/developerguide/resource-config-reference.html>): already
    dropped, nothing to add. A cluster of 0 instances has no instance row and is not listed.
  - ElastiCache Serverless: stores data across three zones, always encrypted at rest, cluster mode only
    (<https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/WhatIs.deployment.html>,
    <https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/WhatIs.corecomponents.html>,
    <https://docs.aws.amazon.com/AmazonElastiCache/latest/APIReference/API_ServerlessCache.html>): always
    `node-high-availability`, `zone-high-availability`, `encryption-at-rest` and `sharding`, `snapshots`
    from `SnapshotRetentionLimit` above 0. Node-based groups: `AutomaticFailover` `enabled` alone is not
    a replica (<https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/AutoFailover.html>), so
    `node-high-availability` also needs a replica in every shard (two or more `NodeGroupMembers` in each
    node group); `StorageEncryptionType` is the
    effective encryption state, not `AtRestEncryptionEnabled`
    (<https://docs.aws.amazon.com/AmazonElastiCache/latest/APIReference/API_ReplicationGroup.html>).
    Node-based Valkey 9.0+ `Durability` (a Multi-AZ transaction log) gets no rule; the guide notes it.
  - NLB: `UDP` and `TCP_UDP` carry UDP, and so do `QUIC` and `TCP_QUIC` (QUIC per RFC 9000, over UDP),
    so all four give `udp`
    (<https://docs.aws.amazon.com/elasticloadbalancing/latest/APIReference/API_Listener.html>,
    <https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-listeners.html>).
  - IPv6: a target group of `IpAddressType` `ipv6` communicates with its targets over IPv6
    (<https://docs.aws.amazon.com/elasticloadbalancing/latest/APIReference/API_TargetGroup.html>,
    <https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-target-groups.html>,
    <https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-target-groups.html>):
    confirmed, joined to its load balancer through `LoadBalancerArns`.
  - SNS: a dead-letter queue is attached to a subscription, not a topic
    (<https://docs.aws.amazon.com/sns/latest/dg/sns-dead-letter-queues.html>), so a topic never gets
    `dead-letter-queue`; `KmsMasterKeyId` gives `encryption-at-rest`
    (<https://docs.aws.amazon.com/sns/latest/api/API_GetTopicAttributes.html>), and the guide projects
    away the `Owner` and `Policy` attributes.
  - Kubernetes load balancers: `elbv2.k8s.aws/cluster` is confirmed
    (<https://kubernetes-sigs.github.io/aws-load-balancer-controller/latest/guide/service/annotations/>);
    `kubernetes.io/cluster/<name>` is documented on subnets only
    (<https://docs.aws.amazon.com/eks/latest/userguide/network-load-balancing.html>), so it is not used.
  - AWS Config also records an ELBv2 listener, listener rule and target group, an EFS access point and
    ECR registry settings (resource-config reference above): dropped as parts of a classified resource.
    EFS `SizeInBytes` is the metered size
    (<https://docs.aws.amazon.com/efs/latest/ug/API_FileSystemSize.html>), and `AvailabilityZoneName` is
    set only on One Zone file systems
    (<https://docs.aws.amazon.com/efs/latest/ug/API_FileSystemDescription.html>). Every new IAM action is
    of access level List or Read in the Service Authorization Reference
    (<https://docs.aws.amazon.com/service-authorization/latest/reference/list_amazonelasticache.html>,
    `list_awselasticloadbalancingv2.html`, `list_awselasticloadbalancing.html`,
    `list_amazonelasticfilesystem.html`, `list_amazonsns.html`).
- Milestone 3: the rule that makes one GCP load balancer (a URL map, or a backend service no URL map
  names; `GlobalForwardingRule` and `RegionBackendService` are not in the search APIs, `ForwardingRule`
  and `BackendService` return both scopes); Memorystore zone placement for `zone-high-availability`;
  the `gcloud redis` and `memorystore` list calls and their fields; the backend service field that says
  it sends to IPv6 backends (`ipAddressSelectionPolicy`) and the protocol fields that say UDP; the
  Pub/Sub rule for a topic's `dead-letter-queue` (a dead-letter topic is set per subscription).
  Checked on 2026-11-25 (public documentation, read-only; the `cloud.google.com` URLs now redirect to
  `docs.cloud.google.com`):
  - One load balancer: confirmed. `ForwardingRule` and `BackendService` return both scopes in search,
    `GlobalForwardingRule` and `RegionBackendService` are not in it
    (<https://docs.cloud.google.com/asset-inventory/docs/supported-asset-types>); the `url-maps`,
    `backend-services`, `forwarding-rules`, `target-https-proxies` and `target-http-proxies` lists give
    global and every region's by default, `target-pools` every region's
    (<https://docs.cloud.google.com/sdk/gcloud/reference/compute/url-maps/list> and the sibling
    pages); a target proxy's `urlMap`
    (<https://docs.cloud.google.com/compute/docs/reference/rest/v1/targetHttpsProxies>,
    <https://docs.cloud.google.com/compute/docs/reference/rest/v1/targetHttpProxies>). Not confirmed,
    and so the rule does not rest on it: that search returns regional URL maps (no `RegionUrlMap` row),
    so the load balancers come from the compute lists and their asset rows are dropped. Decided: a
    backend service of scheme `INTERNAL_SELF_MANAGED` (a service mesh) is left out, and so is a URL
    map naming only such services. Not confirmed, and left out: a rule for the load balancers a GKE
    cluster created (URL maps and backend services carry no labels, the `k8s-` prefixes appear only in
    examples), so they are kept and the guide says so; the default scope of the TCP, SSL and gRPC proxy
    lists, which the guide does not call; the zone redundancy of each load balancer type, where the
    PRD's own rule stands.
  - Memorystore zone placement: confirmed. A Redis instance's `nodes[].zone` and
    `alternativeLocationId`
    (<https://docs.cloud.google.com/memorystore/docs/redis/reference/rest/v1/projects.locations.instances>,
    <https://docs.cloud.google.com/memorystore/docs/redis/redis-tiers>), and for a Redis cluster and
    Valkey `zoneDistributionConfig.mode`, "Defaults to MULTI_ZONE", with one replica per shard needed
    for high availability
    (<https://docs.cloud.google.com/memorystore/docs/cluster/ha-and-replicas>,
    <https://docs.cloud.google.com/memorystore/docs/valkey/ha-and-replicas>). Not confirmed, and left
    out: that a `STANDARD_HA` instance always has its replica in another zone, so its placement
    decides: listed nodes in two or more zones, or an `alternativeLocationId` other than its
    `locationId`, either one is enough.
  - The `gcloud redis` and `memorystore` list calls and fields: confirmed
    (<https://docs.cloud.google.com/sdk/gcloud/reference/redis/instances/list>,
    <https://docs.cloud.google.com/sdk/gcloud/reference/redis/clusters/list>,
    <https://docs.cloud.google.com/sdk/gcloud/reference/memorystore/instances/list>, and the REST
    pages above: `memorySizeGb`, `shardCount`, `replicaCount`, `nodeConfig.sizeGb`, `mode`,
    `persistenceConfig`). Not confirmed, and left out: an "all regions" value for the gcloud calls (the
    guide loops over the regions the asset listing names); the API spelling of `nodeType`, and whether
    a Redis cluster's `sizeGb` counts replicas, so a Redis cluster gets a `count` and no `memoryGiB`.
  - Memorystore for Valkey's default mode: confirmed, Cluster Mode Enabled
    (<https://docs.cloud.google.com/memorystore/docs/valkey/create-instances>,
    <https://docs.cloud.google.com/memorystore/docs/valkey/configure-instances>), and `STANDALONE` is
    the deprecated spelling of `CLUSTER_DISABLED`; so a listed instance gets `sharding` unless its
    `mode` is `CLUSTER_DISABLED` or `STANDALONE`, and one with no list data gets none. Not confirmed:
    whether a list response omits `mode` (or gives `MODE_UNSPECIFIED`) for an instance created without
    it.
  - IPv6 backends and UDP: confirmed. `ipAddressSelectionPolicy` `IPV6_ONLY` and `PREFER_IPV6`
    (`IPV4_ONLY` the default), a backend service's `protocol` `UDP`, a forwarding rule's `IPProtocol`
    and `ipVersion`, and a target pool has no protocol
    (<https://docs.cloud.google.com/compute/docs/reference/rest/v1/backendServices>,
    <https://docs.cloud.google.com/compute/docs/reference/rest/v1/forwardingRules>,
    <https://docs.cloud.google.com/compute/docs/reference/rest/v1/targetPools>). Not confirmed, and left
    out: that `L3_DEFAULT` forwards UDP, so it does not count as `udp` and the guide says so.
    Corrected on 2026-11-25 (PR #591's review): `ipAddressSelectionPolicy` is documented only for
    proxy-based load balancers, "a preference for traffic sent from the proxy to the backend", for
    schemes `EXTERNAL_MANAGED`, `INTERNAL_MANAGED` and `INTERNAL_SELF_MANAGED`
    (<https://docs.cloud.google.com/compute/docs/reference/rest/v1/backendServices>); an external
    passthrough Network Load Balancer serves IPv6 through an IPv6 forwarding rule to backends with
    `stack-type` `IPV4_IPV6` or `IPV6_ONLY`
    (<https://docs.cloud.google.com/load-balancing/docs/network/networklb-backend-service>), and so
    does an internal one (<https://docs.cloud.google.com/load-balancing/docs/internal>), neither naming
    a selection policy. So a backend service of scheme `EXTERNAL` or `INTERNAL` that an IPv6 forwarding
    rule names in `backendService` gets `ipv6`; proxy-based load balancers keep the policy rule. A
    target pool "support[s] only IPv4 traffic"
    (<https://docs.cloud.google.com/load-balancing/docs/network/networklb-target-pools>), so it gets
    none. Not confirmed, and left out: that the backends are in fact dual-stack (the forwarding rule
    can point only at dual-stack or IPv6-only backends, so the program does not read the instances'
    `stackType`).
  - Pub/Sub `dead-letter-queue`: confirmed per subscription, "You configure a dead-letter topic on a
    subscription, not on the topic it pulls from"
    (<https://docs.cloud.google.com/pubsub/docs/handling-failures>), so a topic gets none and its
    subscription keeps it; a topic gets `encryption-at-rest`
    (<https://docs.cloud.google.com/pubsub/docs/encryption>).
  - Also checked: the Cloud Run service behind a 2nd gen function is the one its
    `serviceConfig.service` names
    (<https://docs.cloud.google.com/functions/docs/reference/rest/v2/projects.locations.functions>),
    confirmed; the label `goog-managed-by: cloudfunctions` the guide relied on is documented nowhere,
    so the program no longer reads it. `roles/artifactregistry.reader` can also download artifacts
    (<https://docs.cloud.google.com/iam/docs/roles-permissions/artifactregistry>), which the guide
    says next to the role.
- Milestone 4: whether Resource Graph lists file shares and NFS protocol, and whether `Reader` covers
  `az storage share-rm list` and `az servicebus topic subscription list`; the `Microsoft.Web/sites` kind
  values; Cosmos DB settings for point-in-time restore and `locations[].isZoneRedundant`; what makes an
  Azure load balancer's or Application Gateway's backend pool IPv6; the retirement of Basic load
  balancers and Azure Database for MariaDB; the Service Bus rule for a topic's `dead-letter-queue`
  (every subscription has a dead-letter subqueue).
  Checked on 2026-11-25 (public Microsoft documentation only, no Azure call):
  - File shares: Resource Graph lists no `fileServices/shares` (nor Service Bus topics or
    subscriptions, nor Redis Enterprise databases), but does list `microsoft.fileshares/fileshares`,
    <https://learn.microsoft.com/en-us/azure/governance/resource-graph/reference/supported-tables-resources>.
    The guide lists shares with `az rest --method get .../fileServices/default/shares` and
    `$expand=snapshots`, whose `enabledProtocols` gives NFS,
    <https://learn.microsoft.com/en-us/rest/api/storagerp/file-shares/list?view=rest-storagerp-2023-05-01>
    (`az storage share-rm list` documents no output shape). `Microsoft.FileShares/fileShares` is GA and
    NFS only, <https://learn.microsoft.com/en-us/azure/storage/files/files-management-concepts>; it is
    `storage.file` as well (its snapshots are not listed).
  - `Reader` is `*/read`,
    <https://learn.microsoft.com/en-us/azure/role-based-access-control/built-in-roles/general#reader>,
    and covers `Microsoft.Storage/storageAccounts/fileServices/shares/read`,
    `Microsoft.ServiceBus/namespaces/topics/subscriptions/read` and
    `Microsoft.Cache/redisEnterprise/databases/read`,
    <https://learn.microsoft.com/en-us/azure/role-based-access-control/permissions/storage>,
    <https://learn.microsoft.com/en-us/azure/role-based-access-control/permissions/integration>,
    <https://learn.microsoft.com/en-us/azure/role-based-access-control/permissions/databases>.
  - `Microsoft.Web/sites` kinds: every function app kind contains `functionapp`,
    <https://github.com/Azure/app-service-linux-docs/blob/master/Things_You_Should_Know/kind_property.md>;
    today's rule stays. A container app's `kind` is `functionapp` or `workflowapp`,
    <https://learn.microsoft.com/en-us/rest/api/resource-manager/containerapps/container-apps/get>:
    `functionapp` is `serverless.function`, `workflowapp` (a Logic App) `unclassified`, any other
    `serverless.container`.
  - Cosmos DB: `backupPolicy.type` `Continuous` is point-in-time restore,
    <https://learn.microsoft.com/en-us/azure/cosmos-db/continuous-backup-restore-introduction>, and
    `locations[].isZoneRedundant` is per region,
    <https://learn.microsoft.com/en-us/rest/api/cosmos-db-resource-provider/database-accounts/get>;
    node high availability always, read replicas with two or more locations. Azure DocumentDB
    (`mongoClusters`): `highAvailability.targetMode` not `Disabled` gives a standby per shard (node
    high availability); only `ZoneRedundantPreferred` is zone redundant, where the region has zones,
    while `SameZone` "protects against node failures but doesn't protect against an availability zone
    outage", <https://learn.microsoft.com/en-us/azure/documentdb/high-availability>.
  - Load balancer IPv6: a backend pool member is IPv6 when the network interface IP configuration it
    names is `IPv6`,
    <https://learn.microsoft.com/en-us/azure/load-balancer/virtual-network-ipv4-ipv6-dual-stack-standard-load-balancer-cli>;
    an Application Gateway supports no IPv6 backends, so never `ipv6`,
    <https://learn.microsoft.com/en-us/azure/application-gateway/ipv6-application-gateway-portal>. Rule
    protocols are `Udp`, `Tcp`, `All` and `Quic`; `Udp`, `All` and `Quic` count as `udp`,
    <https://learn.microsoft.com/en-us/rest/api/load-balancer/load-balancers/get>.
  - Retirements: Basic Load Balancer on 30 September 2025 (still running, unsupported; classified),
    <https://learn.microsoft.com/en-us/azure/load-balancer/load-balancer-basic-upgrade-guidance>;
    Azure Database for MariaDB on 19 September 2025,
    <https://learn.microsoft.com/en-us/lifecycle/products/azure-database-for-mariadb>; Azure Cache
    for Redis Basic, Standard and Premium on 30 September 2028, Enterprise and Enterprise Flash on
    31 March 2027 (both still classified),
    <https://learn.microsoft.com/en-us/azure/azure-cache-for-redis/retirement-faq>.
  - Service Bus: every queue and subscription has a dead-letter subqueue, a topic none ("messages
    don't sit at the topic level"),
    <https://learn.microsoft.com/en-us/azure/service-bus-messaging/service-bus-dead-letter-queues>;
    a topic is `encryption-at-rest`, as topics exist only in Standard and Premium, which encrypt at
    rest, <https://learn.microsoft.com/en-us/azure/service-bus-messaging/service-bus-premium-messaging>.
  - Also found: Azure Cache for Redis C0 and C1 have no disk encryption,
    <https://learn.microsoft.com/en-us/azure/azure-cache-for-redis/cache-how-to-encryption>; an Azure
    Managed Redis cluster's clustering and persistence are its databases' settings,
    <https://learn.microsoft.com/en-us/rest/api/redis/redisenterprisecache/databases/get>, read with
    one more `az rest` call. Not confirmed, so out of the guide: an Azure Managed Redis SKU's memory.
- Milestone 5: OVHcloud Load Balancer node-level failover; File Storage Service regions and price;
  whether Managed Private Registry Small ("active-passive across 2 AZs") meets the zone rule without
  manual failover; whether the Valkey and MongoDB prices are per node or per service (the snapshot stores
  per node, *Estimates*) and that the Valkey page gives a vCPU count per plan; whether
  `www.ovhcloud.com` answers an automated client. Checked on 2026-11-25 (public pages, anonymous client;
  sources in OVHcloud's `SOURCES.md`):
  - Load Balancer node-level failover: confirmed in the 3-AZ regions only (the reference architecture
    page's Octavia row: an active and a passive load balancer "each deployed in a separate AZ", "The
    service will remain available without interruption",
    <https://docs.ovhcloud.com/en/guides/public-cloud/cross-functional/deployments-modes-reference-architecture>),
    so *Load Balancer 3-AZ* lists `zone-high-availability` (node level implied). Not confirmed in a
    1-AZ region: the product page's "distributed architecture" and 99.99% SLA name no failure the load
    balancer survives, so *Load Balancer* lists neither level.
  - File Storage Service regions and price: all seven EU regions and €0.000194 per GiB and hour (0.1416
    a month) on the prices page and the product page, but general availability is not confirmed
    (roadmap issue <https://github.com/ovh/public-cloud-roadmap/issues/54> open, the product page's
    "other consumption modes coming at GA"), so no offering; *Left out on purpose* records the offering a
    confirmed GA would add.
  - Managed Private Registry Small: not confirmed ("a secondary zone can take over in case of an issue"
    says neither automatic nor uninterrupted; 3-AZ SLA 99.9%), and Medium and Large not either (their
    "active-active mode across at least 2 AZs" and 99.99% SLA state no continuity through a zone loss,
    <https://docs.ovhcloud.com/en/guides/public-cloud/containers-orchestration/managed-private-registry/managed-private-registry-faq>):
    no 3-AZ variant.
  - Valkey and MongoDB prices: per node (the prices page's "Price (excl. VAT / hour / node)", its data's
    `per-node` price beside the `total`, which is the per-node price × the tier's nodes in every row);
    the snapshot stores the per-node price × 730. The Valkey tables give a "vCore" count per plan, which
    the snapshot records as `vcpus` (<https://www.ovhcloud.com/en-ie/public-cloud/prices/>).
  - `www.ovhcloud.com` answers an automated client: yes, with the full pages and the prices page's price
    data; no page had to be saved by hand.
- Milestone 6: the regions of every new Scaleway product (the documentation names none but Redis's
  zones); encryption at rest for Redis™ and MongoDB®; whether Topics and Events has a dead-letter
  setting; the page that prices File Storage; whether the Redis™ and MongoDB® node-type prices are per
  node or per cluster, and that each Redis™ node type gives a vCPU count.
- Milestone 7: that Managed Load Balancer Production fails over without manual action, and whether any
  frontend mode forwards UDP; File Storage node-level redundancy; whether UpCloud's multi-node Valkey
  plans are priced per plan or per node, and that each plan gives a vCPU count.
