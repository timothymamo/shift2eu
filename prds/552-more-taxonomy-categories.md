# PRD #552: More taxonomy categories

**Status:** Draft. The open questions under *Decisions and open questions* wait for the maintainer.
**Issue:** #552

## Problem

The taxonomy has nine categories (`internal/domain/taxonomy/category.go`): `compute.vm`,
`compute.baremetal`, `kubernetes.managed`, `storage.object`, `storage.block`, `database.postgresql`,
`database.mysql`, `serverless.function` and `messaging.queue`. Every other source resource is
`unclassified`, and an `unclassified` resource has fit `none` on every target, by definition: no
offering ever serves it.

The inventory guide (`docs/guide/inventory.md`, *AWS classification*, *GCP classification*, *Azure
classification*, and `docs/guide/inventory/{aws,gcp,azure}.jq`) therefore marks many common services
`unclassified`: in-memory caches (ElastiCache, Memorystore, Azure Cache for Redis), load balancers
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

Some services are missing from the inventory altogether. AWS Config records neither ElastiCache,
MemoryDB clusters nor DocumentDB (the
[AWS Config resource type list](https://docs.aws.amazon.com/config/latest/developerguide/resource-config-reference.html)
has no `AWS::ElastiCache::*` and no DocumentDB type), so the guide's AWS inventory does not list them
at all, not even as `unclassified`.

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
| Capabilities | One new capability, `sharding` (open question 4). The existing `node-high-availability`, `zone-high-availability`, `encryption-at-rest`, `snapshots`, `point-in-time-restore`, `read-replicas`, `ipv6` and `dead-letter-queue`, reused; `snapshots` widened to file shares and cache data | Any other new capability (protocol, durability, modules, WAF) |
| Inventory | Schema version 2, with the size fields the new categories take (*Estimates*) | A schema version 3 |
| Assessment and output | The current fit rules; estimates for `cache.redis`, `database.mongodb` and `storage.file`; a new not-estimated reason for categories that are never priced; Markdown, JSON and the site's HTML report | A JSON report `schemaVersion` bump. Minimum billed sizes. Request or traffic pricing |

### The new categories

Each definition is what the taxonomy's doc comment will say. Commonness is a judgement from typical
production estates, not a measured figure.

| Category | Definition | Why | OVHcloud | Scaleway | UpCloud |
|---|---|---|---|---|---|
| `cache.redis` | An in-memory key-value store the provider runs that speaks the Redis protocol: Redis OSS or its fork Valkey | Very common on all three sources | Public Cloud Databases for Valkey | Managed Database for Redis™ | Managed Valkey |
| `database.mongodb` | A document database the provider runs that speaks the MongoDB wire protocol | Common on AWS and Azure; two targets run real MongoDB | Public Cloud Databases for MongoDB | Managed MongoDB® | none |
| `network.loadbalancer` | A load balancer the provider runs in front of a set of backends, at layer 4 (TCP/UDP) or layer 7 (HTTP/HTTPS) | Very common; every target has one, with L4, L7 and TLS termination | Public Cloud Load Balancer | Load Balancer | Managed Load Balancer |
| `messaging.topic` | A publish-subscribe topic the provider runs: every subscription gets its own copy of each message | Common (SNS, Service Bus topics, Pub/Sub); stops a topic mapping `direct` to a queue | none | Topics and Events | none |
| `serverless.container` | A container image the provider runs and scales with requests, down to zero when idle | Common on GCP (Cloud Run) and Azure (Container Apps) | none | Serverless Containers | none |
| `registry.container` | A private registry of OCI container images the provider runs | Very common; two targets have a GA registry | Managed Private Registry | Container Registry | none (open beta) |
| `storage.file` | A shared file system the provider runs, mounted by several machines over NFS | Moderate (EFS, Filestore, Azure Files NFS); every target has one | File Storage Service | File Storage | File Storage |

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
  - Scaleway: Load Balancer, zonal, TCP and HTTP/HTTPS, SSL offloading, sizes LB-S to LB-XL; on failure
    Scaleway spawns a replica and reroutes the flexible IP; IPv6 frontends.
    <https://www.scaleway.com/en/docs/load-balancer/faq/>,
    <https://www.scaleway.com/en/docs/load-balancer/reference-content/ssl-bridging-offloading-passthrough/>,
    <https://www.scaleway.com/en/docs/load-balancer/reference-content/load-balancers-limitations/>.
  - UpCloud: Managed Load Balancer, `tcp` (layer 4) and `http` (layer 7) frontends, TLS termination,
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
  - Scaleway: Container Registry, in Paris, Amsterdam and Warsaw; €0.027 per GB and month stored for
    private images. <https://www.scaleway.com/en/docs/container-registry/faq/>.
  - UpCloud: Managed Container Registry is in open beta ("standard UpCloud SLAs do not apply"), so
    none until it is generally available. <https://upcloud.com/docs/products/container-registry/>.
- **`storage.file`**
  - OVHcloud: File Storage Service (OpenStack Manila), "managed NFS shares on private networks", NFSv4,
    optional encryption at rest, snapshots.
    <https://docs.ovhcloud.com/en/guides/storage-and-backup/file-storage/file-storage-service/getting-started>.
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
| `cache.redis` | `AWS::ElastiCache::ReplicationGroup`, and `AWS::ElastiCache::CacheCluster` not in a group, engine `redis` or `valkey` (*call*: `elasticache describe-replication-groups`, `describe-cache-clusters`); `AWS::ElastiCache::ServerlessCache`, engine `redis` or `valkey` (*call*: `describe-serverless-caches`) | `redis.googleapis.com/Instance`, `redis.googleapis.com/Cluster`, `memorystore.googleapis.com/Instance` | `Microsoft.Cache/redis`, `Microsoft.Cache/redisEnterprise` |
| `database.mongodb` | `AWS::DocDB::DBInstance` (the CloudFormation type of a DocumentDB instance), read from engine `docdb` in `rds describe-db-instances` (*call*, already made), per instance as for Aurora | none (GCP runs no managed MongoDB) | `Microsoft.DocumentDB/databaseAccounts`, kind `MongoDB`; `Microsoft.DocumentDB/mongoClusters` |
| `network.loadbalancer` | `AWS::ElasticLoadBalancingV2::LoadBalancer`, type `application` or `network` (*call*: `elbv2 describe-load-balancers`); `AWS::ElasticLoadBalancing::LoadBalancer` | `compute.googleapis.com/UrlMap`; `compute.googleapis.com/BackendService` that no URL map names; `compute.googleapis.com/TargetPool` (*call*: `compute url-maps list`, `backend-services list`) | `Microsoft.Network/loadBalancers`, SKU `Standard` or `Basic`; `Microsoft.Network/applicationGateways` |
| `messaging.topic` | `AWS::SNS::Topic` | `pubsub.googleapis.com/Topic` (today `messaging.queue`) | `Microsoft.ServiceBus/namespaces/topics` (today `unclassified`) |
| `serverless.container` | `AWS::AppRunner::Service` | `run.googleapis.com/Service` not backing a 2nd gen Cloud Function (today `unclassified`) | `Microsoft.App/containerApps` |
| `registry.container` | `AWS::ECR::Repository` | `artifactregistry.googleapis.com/Repository`, format `DOCKER` (*call*: `artifacts repositories list`) | `Microsoft.ContainerRegistry/registries` |
| `storage.file` | `AWS::EFS::FileSystem` (*call*: `efs describe-file-systems` for size and settings) | `file.googleapis.com/Instance` (Filestore) | `Microsoft.Storage/storageAccounts/fileServices/shares` with protocol NFS (*call*: `az storage share-rm list`); a `FileStorage` account whose shares are listed is left out, as a Service Bus namespace is |

Left out on the source side, as the guide leaves out an EKS cluster's nodes today: load balancers that
a Kubernetes cluster created (AWS tags `elbv2.k8s.aws/cluster` or `kubernetes.io/cluster/<name>`, GCP
`goog-gke-*` labels, everything in an AKS node resource group, which the guide already drops), a GCP
forwarding rule and target proxy (the frontends of a load balancer the URL map or backend service stands
for), the member clusters of an ElastiCache replication group (the group stands for them), and the Cloud
Run service behind a 2nd gen Cloud Function (already dropped).

Capabilities the programs derive, from settings the calls above return:

| Category | Capability | When (examples; each milestone pins the exact rule) |
|---|---|---|
| `cache.redis` | `node-high-availability` | ElastiCache automatic failover on; Memorystore tier `STANDARD_HA` or a cluster with replicas; Azure Standard or Premium tier |
| `cache.redis` | `zone-high-availability` | ElastiCache Multi-AZ on; Azure zones set on a Premium cache; Memorystore per its placement (to be checked) |
| `cache.redis` | `sharding` | ElastiCache cluster mode on; `redis.googleapis.com/Cluster` or a sharded Valkey instance; Azure Premium with `shardCount`, or Azure Managed Redis in cluster policy `OSSCluster` |
| `cache.redis` | `snapshots` | ElastiCache snapshot retention above 0; Azure Premium RDB persistence; Memorystore RDB persistence |
| `cache.redis` | `encryption-at-rest` | ElastiCache at-rest encryption on; always on Memorystore and Azure |
| `database.mongodb` | as `database.*` today | the RDS fields the guide reads for an Aurora instance; Cosmos DB continuous backup for `point-in-time-restore`, more than one region for `zone-high-availability` (to be checked) |
| `network.loadbalancer` | `zone-high-availability` | an ALB or NLB in two or more zones; a GCP global or regional load balancer; an Azure Standard load balancer with a zone-redundant frontend, or an Application Gateway in two or more zones |
| `network.loadbalancer` | `ipv6` | ELBv2 `IpAddressType` `dualstack`; a GCP IPv6 forwarding rule; an Azure IPv6 frontend |
| `messaging.topic` | `encryption-at-rest` | SNS with a KMS key; always on Pub/Sub and Service Bus |
| `messaging.topic` | `dead-letter-queue` | per source, to be checked (SNS and Pub/Sub set it per subscription, Service Bus subscriptions always have one) |
| `registry.container` | `encryption-at-rest` | always: ECR, Artifact Registry and Azure Container Registry encrypt images at rest |
| `storage.file` | `encryption-at-rest` | EFS `Encrypted`; always on Filestore and Azure Files |
| `storage.file` | `zone-high-availability` | EFS Regional (not One Zone); Filestore tier `REGIONAL` or `ENTERPRISE`; Azure Files on ZRS |
| `storage.file` | `snapshots` | Filestore or Azure share snapshots exist |

`serverless.container` resources carry no capability: scaling with requests is what the category is.

### Estimates

| Category | Size fields (inventory version 2) | Priced by | Why |
|---|---|---|---|
| `cache.redis` | `memoryGiB` (per node), `count` (nodes), `usage.hoursPerMonth` | plan, on memory alone: the cheapest plan whose memory covers the resource's, × `count` | Every source sizes a cache by memory (Memorystore `memorySizeGb`, Azure SKU capacity, an ElastiCache node type), and every target sells cache plans by memory; a vCPU count is rarely known and would make most caches "size unknown" |
| `database.mongodb` | as `database.postgresql`: `vcpus`, `memoryGiB`, `storageGiB`, `count`, `usage.hoursPerMonth` | plan, plus storage above what it includes | The targets price MongoDB per node flavour plus storage, like their other databases |
| `storage.file` | `storageGiB` (provisioned) | GiB | OVHcloud, Scaleway and UpCloud bill provisioned GiB (Scaleway: "billed based on the provisioned space") |
| `messaging.topic` | none | not estimated: usage-priced | Billed by bytes or messages delivered (Scaleway Topics and Events: "topic volume") |
| `serverless.container` | none | not estimated: usage-priced | Billed per GB-s and vCPU-s (Scaleway Serverless Containers) |
| `network.loadbalancer` | none | not estimated: not priced | A flat fee per size, and the size is chosen by bandwidth or connections, which no inventory field records |
| `registry.container` | none | not estimated: not priced | OVHcloud sells plans with included storage, Scaleway prices stored GB; one method would misprice one of them (open question 6) |

Sizes the guide's programs fill in: a Memorystore instance's `memorySizeGb`, a node count where the
call gives it, an EFS file system's `SizeInBytes`, a Filestore share's `capacityGb`, an Azure share's
`shareQuota`, and DocumentDB the way the guide sizes RDS today. ElastiCache and Azure Cache SKUs name a
node type, not a memory size; the guide says to add `memoryGiB` by hand, as it does for an RDS instance
class.

"Not priced" is a new not-estimated reason, `not-priced`: today a category that has a fit but is never
priced gets `usage-priced`, which would be wrong for a load balancer. A new value of the reason set
keeps the JSON report's `schemaVersion` (*Decisions*).

### Left out

| Candidate | Why left out |
|---|---|
| DNS zones (Route 53, Cloud DNS, Azure DNS) | A zone maps cleanly (zone files are standard, OVHcloud and Scaleway host external zones by API), but it is a global service: an offering has no region, and ADR 0007 needs at least one declared EU region per offering, which for an anycast service would claim a residency it does not have. UpCloud offers none ("We do not currently offer a user-configurable authoritative DNS service", <https://upcloud.com/docs/products/networking/dns/>). Needs region semantics for global services first (open question 2) |
| CDNs (CloudFront, Cloud CDN, Front Door, Azure CDN) | Global, as DNS, and too vague to assess: Scaleway Edge Services fronts only Scaleway origins (<https://www.scaleway.com/en/docs/edge-services/faq/>), UpCloud CDN is a closed beta with no API (<https://upcloud.com/docs/products/cdn/>), OVHcloud CDN Infrastructure is a Network product sold by traffic quota (<https://docs.ovhcloud.com/en/guides/network/cdn/quota>); on GCP Cloud CDN is a flag on a backend, not a resource; CloudFront features (edge functions, WAF) have no vocabulary |
| Managed search, `search.opensearch` (OpenSearch Service) | Well bounded and served by OVHcloud (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/opensearch-capabilities>) and UpCloud (<https://upcloud.com/docs/products/managed-opensearch/>), Scaleway's status to be checked; but only AWS has the source, and it is the least common family. First candidate for a follow-up PRD (open question 1) |
| Kafka, `messaging.kafka` (MSK, GCP Managed Kafka; Event Hubs' Kafka API) | OVHcloud only (<https://docs.ovhcloud.com/en/guides/public-cloud/databases/kafka-capabilities>); Scaleway Clusters for Kafka's status to be checked; few source resources run Kafka itself. Follow-up candidate (open question 1) |
| Other streaming and event buses (Kinesis, EventBridge, Event Grid, Event Hubs) | No target equivalent; each is its own model. Stay `unclassified` |
| Key-value and wide-column databases (DynamoDB, Bigtable, Firestore, Keyspaces, Cosmos DB NoSQL, Cassandra, Table and Gremlin APIs) | No target runs one (OVHcloud's index lists none; UpCloud and Scaleway none). Stay `unclassified`, which is the truthful verdict |
| MemoryDB | A durable primary database with a multi-AZ transaction log, not a cache: a target cache that snapshots every 12 hours would map `direct` and hide the loss of durability, and no capability expresses it. Stays `unclassified` (open question 3) |
| Memcached (ElastiCache, Memorystore) | No target runs Memcached; it is not the Redis protocol. Stays `unclassified` |
| MariaDB, SQL Server, Oracle (RDS, Cloud SQL, Azure SQL, SQL Managed Instance) | No target runs them as a public cloud managed database. OVHcloud's MariaDB is a Web Cloud Databases (web hosting) product. Stay `unclassified` |
| ECS services and clusters, Fargate | An orchestrator of long-running tasks, not request-scaled containers; the move is a re-platforming (to `kubernetes.managed` or `serverless.container`) the user decides, not a like-for-like mapping. Stays `unclassified` (open question 8) |
| App Service web apps (`Microsoft.Web/sites`, kinds other than `functionapp`) | A runtime-stack PaaS with no target equivalent; too vague as a category. Stays `unclassified` |
| Cloud Run jobs, Container Instances, Batch | Run-to-completion jobs; no category. Scaleway Serverless Jobs could serve a later `serverless.job` |
| FSx (Windows, Lustre, ONTAP, OpenZFS) and SMB shares (Azure Files SMB) | AWS Config does not record FSx; SMB and Lustre have no target equivalent in the public clouds. Stay `unclassified` or out of the inventory, as today |
| AWS Gateway Load Balancer, Azure Gateway load balancer | Appliance insertion, not load balancing to backends; no target equivalent |
| A `containers` category covering all of the above | Too vague: it would map an App Service plan, an ECS cluster and a Cloud Run service to the same offerings |

## Acceptance criteria

All criteria are pinned by tests that run offline under `make test` or `make test-e2e`, unless marked
*review*.

**Taxonomy and inventory**

- [ ] An inventory (schema version 1 or 2) with a resource of category `cache.redis`,
  `database.mongodb`, `network.loadbalancer`, `messaging.topic`, `serverless.container`,
  `registry.container` or `storage.file` is read, and `shift2eu assess` exits 0. An unknown value such
  as `cache.memcached` is still refused with exit 1 and "unknown taxonomy category".
- [ ] A version 2 resource of a new category takes exactly the size fields under *Estimates*. Any other
  size or usage field (`vcpus` on `cache.redis`, `storageGiB` on `network.loadbalancer`, any size on
  `messaging.topic`) is refused with exit 1 and a message naming the field by its path.
- [ ] Given a resource that lists `sharding`, the inventory is read; an offering that does not list it
  gives fit `partial` naming `sharding` as missing.
- [ ] A version 1 or 2 inventory with only the nine existing categories gives byte-identical reports
  before and after this PRD, except where a target snapshot changed (none of the existing offerings
  changes).
- [ ] The sample inventories under `examples/` together use every taxonomy category, the new ones
  included (the existing check in `test/examples`).

**Mapping and report**

- [ ] Given a resource of a new category and a target with no offering for it, its fit is `none`, the
  reason is "no offering in the <target> catalog for `<category>`", and it is listed as a blocker. This
  holds for `database.mongodb`, `serverless.container` and `registry.container` on UpCloud, and for
  `messaging.topic` and `serverless.container` on OVHcloud and UpCloud.
- [ ] Given a resource of each new category and a target that serves it, the report names the best
  offering, with fit `direct` when the offering lists every capability the resource lists and
  `partial` naming each missing capability otherwise. A test per target pins the best offering for a
  resource with no capability and for one with each capability a tier variant exists for (for example
  `zone-high-availability` on a `cache.redis` resource points at an OVHcloud 3-AZ Valkey variant).
- [ ] The JSON report carries the new category values in `category` and keeps `schemaVersion` 1, 2
  and 3 as today; every golden document validates against its schema.

**Estimates**

- [ ] A `cache.redis` resource with `memoryGiB` and `count` is priced at the cheapest plan of the best
  offering whose memory covers it, × `count`, scaled by `hoursPerMonth` as other plan-priced
  categories are; without `memoryGiB` it is "not estimated: size unknown".
- [ ] A `database.mongodb` resource is priced as a `database.postgresql` resource is, plan plus
  storage above what the plan includes.
- [ ] A `storage.file` resource with `storageGiB` is priced per GiB from the offering's storage price.
- [ ] A `messaging.topic` or `serverless.container` resource with a fit says "not estimated:
  usage-priced"; a `network.loadbalancer` or `registry.container` resource with a fit says "not
  estimated: not-priced", in the Markdown, JSON and HTML reports. None is counted as 0 in a total.
- [ ] The per-category estimate table in `test/examples` has a representative resource for every new
  priced category each snapshot offers, and pins its plan and amount.

**Inventory guide**

- [ ] Run as the guide runs them, against recorded, sanitised fixtures, each source's jq program
  classifies every `rawType` in *How the sources classify* into its category, with the capabilities
  and sizes the tables give, and writes a schema version 2 inventory the binary accepts (the existing
  e2e test, with its category list per source: GCP has no `database.mongodb`).
- [ ] A GCP Pub/Sub topic is `messaging.topic` and its subscription stays `messaging.queue`; an Azure
  Service Bus topic is `messaging.topic`; a Cloud Run service that is not behind a Cloud Function is
  `serverless.container`.
- [ ] An ElastiCache Memcached cluster, a MemoryDB cluster, a DynamoDB table and an RDS MariaDB
  instance in the fixtures stay `unclassified` (MemoryDB and Memcached only when collected); a load
  balancer a Kubernetes cluster created, a GCP forwarding rule and an ElastiCache group's member
  cluster are not in the inventory.
- [ ] Every new command in the guide only lists, describes or gets, and the AWS policy, GCP roles and
  Azure role in the guide cover it (*review*, and the existing `cloud-read-only` checks where they apply).
- [ ] The site's inventory page shows the guide's new commands (`test/site`, which compares the page
  with the guide).

**Catalogs**

- [ ] Each target's catalog serves the new categories its column under *The new categories* names, each
  with at least one offering (`targettest` *category* check), only in declared EU regions (*regions*
  check), and no other new category.
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
can run in parallel. Until a target's catalog milestone lands, its report shows the new categories as
fit `none` with "no offering in the <target> catalog", which is already more precise than
`unclassified`.

1. [ ] **Taxonomy, inventory and estimates.** The seven categories in `internal/domain/taxonomy`, with
   their definitions; the `sharding` capability and the widened `snapshots` definition (subject to open
   question 4); the size-field table for the new categories (`internal/domain/inventory`); memory-only
   plan matching for `cache.redis`, plan pricing for `database.mongodb`, GiB pricing for
   `storage.file`, and the `not-priced` reason (`internal/domain/estimate`), rendered in the Markdown,
   JSON and HTML reports; the JSON schemas' descriptions of the reason set. One ADR records the
   categories, the capability, the field table (amending ADR 0025), the memory-only matching (amending
   ADR 0027) and why no format version changes. The samples under `examples/` gain one resource per
   new category, and their goldens change. The guide's lists of categories and capabilities and its
   *Sizes* table name the new ones.
2. [ ] **AWS classification in the guide.** New read-only calls for ElastiCache (replication groups,
   cache clusters, serverless caches), ELBv2, EFS and, where needed, ECR, with their IAM actions in the
   guide's policy; `aws.jq` classifies the AWS column of *How the sources classify* and drops what is
   left out; DocumentDB instances from the existing `rds describe-db-instances` become
   `database.mongodb`; tables, recorded fixtures and the e2e golden updated, and the e2e category list
   made per source. Depends on 1.
3. [ ] **GCP classification in the guide.** The asset types of the GCP column, Pub/Sub topics moved to
   `messaging.topic`, Cloud Run services to `serverless.container`, the calls for Memorystore sizes, URL
   maps and backend services, Artifact Registry formats and Filestore capacity, with the roles they need;
   fixtures and golden. Depends on 1.
4. [ ] **Azure classification in the guide.** The types of the Azure column, Service Bus topics to
   `messaging.topic`, the Resource Graph projection extended to the properties read (cache SKU and
   zones, Cosmos DB `kind` and capabilities, load balancer SKU and frontends), the file share loop; fixtures
   and golden. Depends on 1.
5. [ ] **OVHcloud catalog.** Offerings for `cache.redis` (Valkey Discovery, Production and their 3-AZ
   variants), `database.mongodb` (Production, Advanced, 3-AZ variants), `network.loadbalancer` (1-AZ
   and 3-AZ), `registry.container` (Managed Private Registry, 3-AZ variant for Medium and Large) and
   `storage.file` (File Storage Service), with plans and prices for the priced ones; `SOURCES.md`,
   golden, `targettest` categories. Pages needed: the documentation above (fetchable), the prices page
   <https://www.ovhcloud.com/en-ie/public-cloud/prices/> (Valkey, MongoDB, File Storage, Load Balancer
   and Managed Private Registry sections) and the regions page
   <https://www.ovhcloud.com/en/public-cloud/regions-availability/>, fetched as before or, if they
   refuse an automated client, saved by the maintainer. Depends on 1.
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
   are needed only if an open question makes them priced. Depends on 1.
7. [ ] **UpCloud catalog.** Offerings for `cache.redis` (Managed Valkey: 1 node, and a multi-node
   variant with `node-high-availability` and `read-replicas`), `network.loadbalancer` (Managed Load
   Balancer, and a Production variant if its failover is confirmed) and `storage.file` (File Storage),
   with plans and prices for Valkey and File Storage. **Gated on the maintainer saving
   <https://upcloud.com/pricing/>, EUR edition, logged out, as before:** the Managed Databases tab's
   Valkey table and the File Storage table (and the Managed Load Balancer table only if load balancers
   become priced). Depends on 1.
8. [ ] **Comparison, docs and close.** An e2e fixture inventory with a resource of every new category,
   assessed against all three targets, golden for Markdown and JSON, so each target's `direct`,
   `partial` and `none` for the new categories show side by side; the guide's *Everything else on AWS*
   text, the README and the `CLAUDE.md` status line updated. Moves this PRD to `prds/done/` and closes
   #552. Depends on 2 to 7.

## Decisions and open questions

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
  consumer reads; both still have to move.
- **`serverless.container` is request-scaled containers only.** "Containers" is too vague: an App Service
  plan, an ECS cluster and a Cloud Run service have no common target. The category is the Cloud Run
  shape (a container scaled by requests, to zero), which Scaleway Serverless Containers serves.
- **`storage.file` is NFS.** EFS, Filestore and the three targets' file storage are NFS; SMB has no
  target equivalent, so SMB shares stay `unclassified` instead of mapping `direct` to an NFS share.
- **No new capability but `sharding` (open question 4).** Every other capability the new categories
  need is in the vocabulary: `node-high-availability`, `zone-high-availability`, `encryption-at-rest`,
  `read-replicas`, `point-in-time-restore`, `ipv6`, `dead-letter-queue` and `snapshots`. `snapshots`
  is widened from "a volume or instance" to "a volume, instance, file share or data set", so a file
  share snapshot and a cache's RDB snapshot count; this changes nothing for the existing categories.
  Layer 4 versus layer 7 needs no capability: every target load balancer does both.
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
- **No catalog snapshot format change.** Offerings name categories the domain validates (ADR 0007);
  cache plans carry vCPU and memory like database plans; unpriced offerings have no plans, as Scaleway
  Queues has today.
- **GA only, and the target rule.** As in PRDs #11 and #77: a beta product (UpCloud Container Registry
  and CDN, Scaleway Redis 2-node High Availability while it is in public beta) is not an offering or
  not a tier, and a capability is listed only where the documentation states it (ADR 0011).
- **The guide can ship before the catalogs.** A classified resource with no offering yet is fit `none`
  with a reason naming its category, never worse than `unclassified`. So milestones 2 to 4 do not wait
  for the hand-saved pages, and the catalogs do not wait for the guide.
- **One saving session per target.** The catalog milestones are per target, not per category, so the
  maintainer saves each target's price pages once.

**Open questions for the maintainer** (each with a recommendation)

1. **Are these seven the right first set?** OpenSearch (`search.opensearch`: OVHcloud and UpCloud,
   AWS only as a source) and Kafka (`messaging.kafka`: OVHcloud, Scaleway to be checked) are well
   bounded but narrower. *Recommendation:* yes, these seven; OpenSearch and Kafka in a follow-up PRD
   once Scaleway's Cloud Essentials for OpenSearch and Clusters for Kafka are confirmed GA.
2. **DNS and CDN.** *Recommendation:* a follow-up PRD on global services that first decides how a
   snapshot records an offering with no region (and what `--region` does with it), then adds public DNS
   zones (`network.dns`, OVHcloud and Scaleway). CDNs stay out even then.
3. **MemoryDB.** *Recommendation:* stays `unclassified`, as decided above; revisit only with a
   durability capability.
4. **Add `sharding`?** A Redis Cluster client cannot use a non-clustered cache: OVHcloud Valkey says
   "Cluster mode (sharding) is not supported", Scaleway has Cluster mode. Without the capability a
   clustered ElastiCache group maps `direct` to OVHcloud. *Recommendation:* add it, defined as "data is
   partitioned across several primary nodes that clients address", used by `cache.redis` now and
   available to `database.mongodb` later.
5. **Memory-only plan matching for `cache.redis`.** It is a second plan-matching rule in the domain.
   *Recommendation:* accept it; the alternative (vCPU and memory, both required) leaves almost every
   cache "size unknown".
6. **Load balancers and registries not priced.** *Recommendation:* `not-priced` for both in this PRD.
   Pricing a load balancer needs a size field the sources do not give; pricing a registry by stored GiB
   fits Scaleway but not OVHcloud's plans. A follow-up issue can add a registry storage estimate.
7. **Reclassify GCP Pub/Sub topics.** It changes what the guide writes for an existing GCP estate.
   *Recommendation:* yes; today's `messaging.queue` lets a topic map `direct` to a queue.
8. **ECS, Fargate and App Service web apps.** *Recommendation:* keep them `unclassified`; mapping them
   would choose a re-platforming for the user.
9. **Names.** `cache.redis` (rather than `cache.valkey` or `cache.keyvalue`), `network.loadbalancer`
   (rather than `loadbalancer.managed`), `registry.container` (rather than `container.registry`).
   *Recommendation:* as named.
10. **Minimum billed sizes.** UpCloud File Storage starts at 250 GiB, so a 50 GiB EFS file system
    priced per GiB is underestimated. *Recommendation:* record the minimum in `SOURCES.md`, say so in the
    guide, and open a follow-up issue for minimum billed sizes in snapshot format v2; do not block this
    PRD on it.
11. **Scaleway Redis High Availability.** Only Cluster mode is GA and highly available, and it needs
    a cluster-aware client, so a non-sharded cache that needs `node-high-availability` would be
    pointed at a tier its client may not use. *Recommendation:* list only Cluster mode as the highly
    available tier until the 2-node High Availability topology leaves public beta, and have the tier's
    `SOURCES.md` section and the report's tie or required change say that Cluster mode needs a
    cluster-aware client; if that cannot be said without a capability, Scaleway Redis lists no
    `node-high-availability` until then.

**To be checked** (each in the milestone named; a fact not confirmed stays out of the snapshot)

- Milestone 2: that `rds describe-db-instances` returns DocumentDB instances with engine `docdb`, and
  whether AWS Config lists them under another type the program must drop; which
  ElastiCache settings give node and zone high availability for a serverless cache; whether App Runner
  and ECR need a call beyond AWS Config.
- Milestone 3: the rule that makes one GCP load balancer (a URL map, or a backend service no URL map
  names; `GlobalForwardingRule` and `RegionBackendService` are not in the search APIs, `ForwardingRule`
  and `BackendService` return both scopes); Memorystore zone placement for `zone-high-availability`;
  the `gcloud redis` and `memorystore` list calls and their fields.
- Milestone 4: whether Resource Graph lists file shares and NFS protocol, and whether `Reader` covers
  `az storage share-rm list`; the `Microsoft.Web/sites` kind values; Cosmos DB settings for
  point-in-time restore and zone redundancy; the retirement of Basic load balancers and Azure Database
  for MariaDB.
- Milestone 5: OVHcloud Load Balancer node-level failover; File Storage Service regions and price;
  whether Managed Private Registry Small ("active-passive across 2 AZs") meets the zone rule without
  manual failover; whether `www.ovhcloud.com` answers an automated client.
- Milestone 6: the regions of every new Scaleway product (the documentation names none but Redis's
  zones); encryption at rest for Redis™ and MongoDB®; whether Topics and Events has a dead-letter
  setting; the page that prices File Storage.
- Milestone 7: that Managed Load Balancer Production fails over without manual action; File Storage
  node-level redundancy; whether UpCloud's multi-node Valkey plans are priced per plan or per node.
