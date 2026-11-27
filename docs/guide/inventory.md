# Build an inventory from your cloud

Turn your AWS, GCP or Azure estate into an inventory file that `shift2eu assess` reads, using only
the provider's own command-line tool, read-only credentials and `jq`. You run a handful of list and
describe calls, save their output in one directory, and run one `jq` program from this repository
over it. This is an interim path: `shift2eu discover`, which will do the same from inside the tool,
does not exist yet.

## Before you start

You need:

- the provider's CLI (`aws` version 2, `gcloud`, or `az` with the `resource-graph` extension),
  signed in the way you normally sign in. Every command below uses the CLI's default credential
  chain; nothing here asks you to print, paste or store a credential;
- `jq` (the programs are tested with jq 1.8);
- `shift2eu` ([Install](install.md)), or a clone with `make build`;
- the programs from this repository, `docs/guide/inventory/aws.jq`, `gcp.jq` and `azure.jq`. A
  release archive ships them under `docs/guide/inventory/` too.

Every command on this page reads: it lists, describes, gets, searches or queries, and never creates,
updates, deletes or tags anything. Where a provider's inventory service must be switched on first
(AWS Config, the GCP Cloud Asset API), this page says so and leaves that to your cloud administrator.

## The inventory format in brief

An inventory is a JSON object with a version, the source and a list of resources.
[ADR 0006](../adr/0006-json-inventory-file-format-v1.md) records the format in full; the reader is
strict, so a misspelt key or an unknown value is an error, never ignored. This guide builds version 2,
recorded in [ADR 0025](../adr/0025-json-inventory-file-format-v2.md): version 1 plus a resource's size,
usage, workload, relations and monthly cost, all optional. `shift2eu assess` reads both versions.

```json
{
  "schemaVersion": 2,
  "source": "aws",
  "resources": [
    {
      "id": "shop-db",
      "rawType": "AWS::RDS::DBInstance",
      "category": "database.postgresql",
      "region": "eu-central-1",
      "capabilities": ["point-in-time-restore", "zone-high-availability"],
      "size": { "vcpus": 2, "memoryGiB": 8, "storageGiB": 50 },
      "workload": "shop",
      "monthlyCost": { "amount": 383.25, "currency": "USD" }
    },
    {
      "id": "shop-db-replica",
      "rawType": "AWS::RDS::DBInstance",
      "category": "database.postgresql",
      "region": "eu-central-1",
      "size": { "vcpus": 2, "memoryGiB": 8, "storageGiB": 50 },
      "workload": "shop",
      "relations": [{ "type": "replica-of", "target": "shop-db" }]
    },
    { "id": "shop-cdn", "rawType": "AWS::CloudFront::Distribution", "category": "unclassified" }
  ]
}
```

| Field | Required | Meaning |
|---|---|---|
| `schemaVersion` | yes | `2` (or `1`, without the fields below `capabilities`) |
| `source` | yes | `aws`, `gcp` or `azure` here |
| `resources[].id` | yes, unique | A short name for the resource |
| `resources[].category` | yes | A taxonomy category, below |
| `resources[].rawType` | no | The source's own type name |
| `resources[].region` | no | The source region; absent for a global resource |
| `resources[].capabilities` | no | Capabilities the resource depends on, below |
| `resources[].size` | no | `vcpus`, `memoryGiB`, `storageGiB` and `count` (instances, nodes or volumes), each optional; see [Sizes](#sizes) |
| `resources[].usage` | no | `hoursPerMonth`, the hours a month the resource runs: 730 when absent, at most 744. Only for compute, Kubernetes, database, `cache.redis`, `search.opensearch` and `messaging.kafka` categories; on any other category it is an error |
| `resources[].workload` | no | A free-text name of the application the resource belongs to, such as `shop` |
| `resources[].relations` | no | A list of `{"type", "target"}`: `type` is `replica-of`, `dead-letter-queue-of`, `attached-to` or `depends-on`, and `target` is another resource's `id` |
| `resources[].monthlyCost` | no | `{"amount", "currency"}`: what the source charges for the resource a month, from your own bill; `currency` is an ISO 4217 code such as `USD` or `EUR` |

The **taxonomy categories** are the only bridge between a source and a target: `compute.vm` (a
virtual machine), `compute.baremetal` (a dedicated physical server, with no hypervisor; see
[ADR 0023](../adr/0023-separate-bare-metal-servers-from-virtual-machines-in-the-taxonomy.md)),
`kubernetes.managed`, `storage.object`, `storage.block`, `storage.file` (an NFS file share),
`database.postgresql`, `database.mysql`, `database.mongodb` (a database that speaks the MongoDB wire
protocol), `cache.redis` (a Redis or Valkey cache), `network.loadbalancer`, `serverless.function`,
`serverless.container` (a container image run and scaled with requests), `registry.container` (a
private OCI image registry), `messaging.queue` and `messaging.topic` (a publish-subscribe topic, every
subscription getting its own copy of each message;
[ADR 0046](../adr/0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md)),
`search.opensearch` (a search engine that speaks the OpenSearch REST API: OpenSearch, or Elasticsearch
up to 7.10) and `messaging.kafka` (a cluster of brokers that speaks the Apache Kafka protocol;
[ADR 0047](../adr/0047-add-search-opensearch-and-messaging-kafka-and-price-their-storage-per-node.md)),
and `network.dns` (a public DNS zone the provider's authoritative name servers serve on the internet:
the records of one domain or subdomain, delegated to the provider by NS records at the parent; private
zones, resolvers, health checks and domain registrations are not one;
[ADR 0048](../adr/0048-record-global-offerings-in-target-snapshot-format-v2-and-add-network-dns.md)). A
DNS zone is a global resource: write it with no `region`. A
resource that fits none of these categories is `unclassified` and keeps its `rawType`: it then shows
up in the report as a blocker on every target, instead of disappearing from the assessment. The
programs below classify into the seven categories ADR 0046 adds, on every source that has such a
service: AWS and Azure into all seven, GCP into all but `database.mongodb` (its program classifies no
GCP service as one; Firestore stays `unclassified`, PRD #552); each source's *classification* table
names the types. Of the two categories ADR 0047 adds (PRD #571), the AWS program classifies into
both, the GCP and Azure programs into `messaging.kafka` (neither source runs a managed OpenSearch),
and no program into `network.dns`: a search service on Azure, an Event Hubs namespace whose clients
all use its Kafka endpoint, or a DNS zone on any source, is listed as `unclassified`, or not at all,
so set its category (`search.opensearch`, `messaging.kafka` or `network.dns`) by hand.

The **capabilities** are features a resource depends on:

| Capability | Meaning |
|---|---|
| `ipv6` | Reachable over IPv6 and can reach IPv6 destinations |
| `point-in-time-restore` | Data can be restored to any moment in a retention window |
| `object-versioning` | Earlier versions of overwritten or deleted objects are kept |
| `node-high-availability` | Keeps serving through the loss of one node, without manual failover |
| `zone-high-availability` | Keeps serving through the loss of a zone; implies `node-high-availability` |
| `read-replicas` | Reads are served from replicas of a primary database |
| `autoscaling` | Capacity is added and removed with load |
| `encryption-at-rest` | Stored data is encrypted |
| `snapshots` | Point-in-time copies of a volume, machine, file share or data set (such as a cache's) are taken |
| `dead-letter-queue` | Messages that keep failing are set aside |
| `sharding` | Data is partitioned across several primary nodes that clients address (a Redis Cluster) |
| `udp` | UDP datagrams are forwarded (a layer 4 load balancer) |
| `dnssec` | The provider signs the DNS zone with DNSSEC and publishes the keys the parent's DS record points at |
| `dns-routing` | The DNS zone's answers depend on the client's location, on weights or on the health of the endpoints |

A capability changes the verdict: an offering that serves the category but lacks a capability the
resource lists is a partial fit, not a direct one, and the report names the missing capability. A
capability left out can therefore make a move look easier than it is, which is why the programs
derive them from the resources' settings rather than leaving them out.

### Sizes

The size is what `assess` matches a target's plans against and prices: the cheapest plan whose vCPU and
memory cover the resource's (memory alone for `cache.redis`), times the `count`, plus storage per GiB
([Assess an inventory](assess.md#read-the-estimate)). Which fields a category takes:

| Category | `vcpus`, `memoryGiB` | `storageGiB` | `count` | Priced by |
|---|---|---|---|---|
| `compute.vm`, `compute.baremetal` | per instance or server | yes | instances | plan |
| `kubernetes.managed` | per node | per node | nodes | plan per node, plus the control-plane fee |
| `database.postgresql`, `database.mysql`, `database.mongodb` | yes | yes | nodes | plan, plus storage above what it includes |
| `cache.redis` | `memoryGiB` only, per node | no | nodes | plan per node, matched on memory alone |
| `search.opensearch`, `messaging.kafka` | per data node or broker | per node | data nodes or brokers | plan per node, plus storage above what it includes, per node |
| `storage.block` | no | yes | volumes | GiB |
| `storage.object` | no | stored GiB | no | GiB |
| `storage.file` | no | provisioned GiB | no | GiB |
| `serverless.function` | `memoryGiB` only | no | no | not estimated (priced by usage) |
| `messaging.queue`, `messaging.topic`, `serverless.container` | no | no | no | not estimated (priced by usage) |
| `network.loadbalancer`, `registry.container`, `network.dns` | no | no | no | not estimated (not priced) |
| `unclassified` | no | no | no | not estimated |

A field a category does not take is an error that names it. A resource without the fields its category
is priced by shows "not estimated: size unknown", never €0. The programs fill in the sizes the
read-only calls give (each source's *Sizes* table below) and leave out any they do not, never guessing.

A high-availability database (an RDS Multi-AZ instance, a `REGIONAL` Cloud SQL instance, an Azure
flexible server with `ZoneRedundant` or `SameZone` high availability) gets no `count`, and its estimate
says "(count assumed 1)" until you set one. Before you count its standby as a second node, check whether
the target's plan already includes the standby (for example a "Multi-node" plan): if it does, a `count`
of 2 doubles the estimate.

### What you add by hand

No read-only call says what a resource is for, so the programs never write these; add them to the
inventory yourself, with an editor or `jq`:

- **`workload`**: the application a resource belongs to. The report groups resources by it, with fits,
  blockers and an estimate per workload; resources without one are under "No workload".
- **`relations`**: a replica (`replica-of`), a dead-letter queue (`dead-letter-queue-of`), a volume on
  its server (`attached-to`), or a dependency (`depends-on`). A blocker on a resource is then also
  listed under each resource that `depends-on` it or is a `replica-of` it.
- **`monthlyCost`**: what the resource costs on the source today, from your own bill or cost export.
  `shift2eu` never reads billing APIs and knows no hyperscaler price; the report shows this cost next
  to the estimate, per currency, without converting it.
- **`usage.hoursPerMonth`**: for a resource that does not run all month, such as a server stopped at
  night. Only compute (`compute.vm`, `compute.baremetal`), Kubernetes (`kubernetes.managed`),
  database (`database.postgresql`, `database.mysql`, `database.mongodb`), cache (`cache.redis`),
  search (`search.opensearch`) and Kafka (`messaging.kafka`) resources take it; on any other category
  the inventory is refused.
- any size the calls do not give, such as an RDS instance's vCPU and memory (its instance class), an
  ElastiCache cache's memory per node (its node type), a DocumentDB cluster's vCPU and memory per
  instance (its instance class), a Cloud SQL instance's unless its tier is `db-custom-*`, the `count` of a high-availability database
  (Multi-AZ, `REGIONAL`, `ZoneRedundant` or `SameZone`; see [Sizes](#sizes)), or an Auto Scaling
  group's instance count.

For example, with `jq`:

```bash
jq '(.resources[] | select(.id == "shop-db")) += {workload: "shop",
      monthlyCost: {amount: 383.25, currency: "USD"}}' \
  inventories/aws.inventory.json > inventories/aws.edited.inventory.json
```

The [samples](../../examples/) show every field in use.

## How the programs work

There is one program per source under `docs/guide/inventory/`: `aws.jq`, `gcp.jq` and `azure.jq`.
For each source you:

1. run the source's commands below, which write their JSON output into one directory
   (`inventories/aws/<region>/`, `inventories/gcp/` or `inventories/azure/`);
2. run the source's program once, with `jq -n`, over every file in that directory, and redirect the
   result to `inventories/<source>.inventory.json`;
3. review the inventory, and delete the collected directory.

Run the commands from the directory you want the files in, such as a clone of this repository:
`inventories/` and `*.inventory.json` are in its `.gitignore`, so neither the raw output nor an
inventory can be committed by accident.

The collected directory is the providers' raw output. The commands keep only the fields the programs
read (`--query` for `aws` and `az`, a `--format` projection and a `jq` filter for `gcloud`), so it
holds no environment variables, startup scripts, admin user names or SSH keys, and no tag or label
values beyond the few the programs read, except inside an AKS cluster's node pools, which the Azure
query keeps whole. It still holds full ARNs and resource IDs, which carry the account, project or
subscription, and can hold IP addresses, tag keys, and an AKS node pool's labels, tags (keys and
values), taints, subnet ID and node image version. Share only the reviewed inventory file, never the
directory, and delete the directory once the inventory is built (see [Privacy](#privacy)).

The loops below go on when one call fails, so read what they print on stderr. A failed call
(`AccessDenied`, `AuthorizationFailed`, `PERMISSION_DENIED`) means the setting it reads is missing
from the inventory: the resource is still listed, but without the capability that setting would have
added, which makes a move look easier than it is. Fix the permission and collect again, or add the
capability by hand.

A program recognises each input by its shape, not by its file name, so the file names below are only
a convention, and a file may hold several JSON documents one after the other (the loops below write
them that way). The program header lists the shapes it reads.

The programs classify by the tables in each source's section: a `rawType` becomes a taxonomy
category, and a setting becomes a capability. They state how a source's resources map to the
taxonomy, nothing else; which target offers what is the catalog's business, decided by `assess`.

The programs write short IDs (an instance ID, a bucket, database or queue name), never an ARN, a full
GCP resource name or an Azure resource ID, which carry the account, project or subscription. Two
resources with the same short name, such as a function and a queue both called `orders`, get their
type and region appended (`orders (AWS::SQS::Queue, eu-central-1)`) so every ID stays unique.

The commands are written for a POSIX shell (sh, dash, bash, zsh).

## AWS

### AWS permissions

The AWS managed policy `ReadOnlyAccess` covers every call. It also lets its holder read your data
(objects in buckets, for example), so prefer a policy, created by your administrator, that allows
only the calls this page makes:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceTypes",
        "ec2:DescribeVolumes",
        "ec2:DescribeSnapshots",
        "autoscaling:DescribeAutoScalingGroups",
        "s3:ListAllMyBuckets",
        "s3:GetBucketVersioning",
        "rds:DescribeDBInstances",
        "eks:ListClusters",
        "eks:DescribeCluster",
        "eks:ListNodegroups",
        "eks:DescribeNodegroup",
        "lambda:ListFunctions",
        "sqs:ListQueues",
        "sqs:GetQueueAttributes",
        "sns:ListTopics",
        "sns:GetTopicAttributes",
        "elasticache:DescribeReplicationGroups",
        "elasticache:DescribeCacheClusters",
        "elasticache:DescribeServerlessCaches",
        "elasticloadbalancing:DescribeLoadBalancers",
        "elasticloadbalancing:DescribeListeners",
        "elasticloadbalancing:DescribeTargetGroups",
        "elasticloadbalancing:DescribeTags",
        "elasticfilesystem:DescribeFileSystems",
        "es:ListDomainNames",
        "es:DescribeDomains",
        "aoss:ListCollections",
        "aoss:BatchGetCollection",
        "kafka:ListClustersV2",
        "config:SelectResourceConfig"
      ],
      "Resource": "*"
    }
  ]
}
```

The search and Kafka actions read only how a domain, collection or cluster is set up. The policy
grants no data-plane action (no `es:ESHttp*`, `aoss:APIAccessAll` or `kafka-cluster:*`, which would
read or write the documents and topics) and no connection or credentials call (no
`kafka:GetBootstrapBrokers` or `kafka:ListScramSecrets`).

`ViewOnlyAccess` is narrower than `ReadOnlyAccess`: it lists and describes resources and grants no
reads of your data. It may lack some of the actions above, though, so check it against this list with
the IAM policy simulator before you rely on it. The custom policy above is the minimal option.

### Collect from AWS

Run these once per region you use, with the region in `AWS_REGION`. Each region's output goes to a
directory of its own:

```bash
export AWS_REGION=eu-central-1
out="inventories/aws/$AWS_REGION"
mkdir -p "$out"

aws ec2 describe-instances --output json --query '{Reservations: Reservations[].{Instances: Instances[].{
  InstanceId: InstanceId, InstanceType: InstanceType, State: {Name: State.Name},
  Placement: {AvailabilityZone: Placement.AvailabilityZone},
  BlockDeviceMappings: BlockDeviceMappings[].{Ebs: {VolumeId: Ebs.VolumeId}},
  NetworkInterfaces: NetworkInterfaces[].{Ipv6AddressCount: length(Ipv6Addresses || `[]`)},
  Tags: Tags[].{Key: Key}}}}' > "$out/ec2-instances.json"

aws opensearch list-domain-names --output json \
  --query '{DomainNames: DomainNames[].{DomainName: DomainName}}' > "$out/opensearch-domain-names.json"
jq -r '.DomainNames[].DomainName' "$out/opensearch-domain-names.json" |
  xargs -r -n 5 aws opensearch describe-domains --output json --query '{DomainStatusList: DomainStatusList[].{
    DomainName: DomainName, ARN: ARN, EngineVersion: EngineVersion, Deleted: Deleted,
    ClusterConfig: {InstanceType: ClusterConfig.InstanceType, InstanceCount: ClusterConfig.InstanceCount,
    ZoneAwarenessEnabled: ClusterConfig.ZoneAwarenessEnabled,
    MultiAZWithStandbyEnabled: ClusterConfig.MultiAZWithStandbyEnabled},
    EBSOptions: {EBSEnabled: EBSOptions.EBSEnabled, VolumeSize: EBSOptions.VolumeSize},
    EncryptionAtRestOptions: {Enabled: EncryptionAtRestOptions.Enabled}}}' --domain-names \
  > "$out/opensearch-domains.json"

f="$out/opensearchserverless-collections.json"
: > "$f"
token=
while :; do
  if ! page=$(aws opensearchserverless list-collections --output json --max-results 100 \
      ${token:+--next-token} ${token:+"$token"} \
      --query '{collectionSummaries: collectionSummaries[].{id: id, name: name}, nextToken: nextToken}'); then
    if [ -n "$token" ]; then
      echo "list-collections failed: $f is incomplete; collect $out again" >&2
    else
      echo "list-collections failed on its first page: $f is empty;" \
        "if the region runs OpenSearch Serverless, collect $out again" >&2
    fi
    break
  fi
  printf '%s\n' "$page" >> "$f"
  last=$token
  token=$(printf '%s' "$page" | jq -r '.nextToken // empty')
  if [ -z "$token" ]; then
    break
  fi
  if [ "$token" = "$last" ]; then
    echo "list-collections repeated its nextToken: $f may be incomplete; collect $out again" >&2
    break
  fi
done
jq -r '.collectionSummaries[]?.id' "$f" |
  xargs -r -n 100 aws opensearchserverless batch-get-collection --output json --query '{collectionDetails:
    collectionDetails[].{name: name, arn: arn, type: type, standbyReplicas: standbyReplicas,
    status: status}}' --ids \
  > "$out/opensearchserverless-batch-collections.json"

aws kafka list-clusters-v2 --output json --query '{ClusterInfoList: ClusterInfoList[].{
  ClusterName: ClusterName, ClusterType: ClusterType, ClusterArn: ClusterArn, State: State,
  Provisioned: Provisioned.{NumberOfBrokerNodes: NumberOfBrokerNodes,
  BrokerNodeGroupInfo: BrokerNodeGroupInfo.{InstanceType: InstanceType, ClientSubnets: ClientSubnets,
  StorageInfo: {EbsStorageInfo: {VolumeSize: StorageInfo.EbsStorageInfo.VolumeSize}}}}}}' \
  > "$out/kafka-clusters-v2.json"

# The vCPU and memory of the instance types in use.
# A search domain's and a Kafka broker's instance type is built on an EC2 type
# (r6g.large.search and kafka.m5.large are r6g.large and m5.large), asked for one
# at a time: a type with no EC2 twin fails alone and its resource stays unsized.
{ jq -r '[.Reservations[].Instances[].InstanceType] | unique[]' "$out/ec2-instances.json" |
    xargs -r -n 100 aws ec2 describe-instance-types --output json --query '{InstanceTypes: InstanceTypes[].{
      InstanceType: InstanceType, VCpuInfo: {DefaultVCpus: VCpuInfo.DefaultVCpus},
      MemoryInfo: {SizeInMiB: MemoryInfo.SizeInMiB}}}' --instance-types
  jq -rn '[inputs | (.DomainStatusList[]?.ClusterConfig.InstanceType // empty | select(endswith(".search"))
      | sub("\\.search$"; "")), (.ClusterInfoList[]?.Provisioned.BrokerNodeGroupInfo.InstanceType // empty
      | select(startswith("kafka.")) | sub("^kafka\\."; ""))] | unique[] | select(test("^o[a-z][0-9]") | not)' \
    "$out/opensearch-domains.json" "$out/kafka-clusters-v2.json" |
    xargs -r -n 1 aws ec2 describe-instance-types --output json --query '{InstanceTypes: InstanceTypes[].{
      InstanceType: InstanceType, VCpuInfo: {DefaultVCpus: VCpuInfo.DefaultVCpus},
      MemoryInfo: {SizeInMiB: MemoryInfo.SizeInMiB}}}' --instance-types
} > "$out/ec2-instance-types.json"

aws ec2 describe-volumes --output json --query '{Volumes: Volumes[].{
  VolumeId: VolumeId, AvailabilityZone: AvailabilityZone, Encrypted: Encrypted, Size: Size,
  Attachments: Attachments[].{InstanceId: InstanceId, DeleteOnTermination: DeleteOnTermination}}}' \
  > "$out/ec2-volumes.json"
aws ec2 describe-snapshots --owner-ids self --output json \
  --query '{Snapshots: Snapshots[].{VolumeId: VolumeId}}' > "$out/ec2-snapshots.json"
aws autoscaling describe-auto-scaling-groups --output json --query '{AutoScalingGroups: AutoScalingGroups[].{
  AutoScalingGroupName: AutoScalingGroupName, AutoScalingGroupARN: AutoScalingGroupARN,
  MinSize: MinSize, MaxSize: MaxSize, AvailabilityZones: AvailabilityZones,
  Tags: Tags[].{Key: Key}}}' > "$out/autoscaling-groups.json"

aws s3api list-buckets --output json \
  --query '{Buckets: Buckets[].{Name: Name, BucketRegion: BucketRegion}}' > "$out/s3-buckets.json"
jq -r '.Buckets[].Name' "$out/s3-buckets.json" | while read -r bucket; do
  aws s3api get-bucket-versioning --bucket "$bucket" --output json --query '{Status: Status}' |
    jq --arg bucket "$bucket" '{Bucket: $bucket, Versioning: .}'
done > "$out/s3-versioning.json"

aws rds describe-db-instances --output json --query '{DBInstances: DBInstances[].{
  DBInstanceIdentifier: DBInstanceIdentifier, DBInstanceArn: DBInstanceArn,
  DBClusterIdentifier: DBClusterIdentifier, Engine: Engine, AllocatedStorage: AllocatedStorage,
  AvailabilityZone: AvailabilityZone, BackupRetentionPeriod: BackupRetentionPeriod, MultiAZ: MultiAZ,
  StorageEncrypted: StorageEncrypted,
  ReadReplicaDBInstanceIdentifiers: ReadReplicaDBInstanceIdentifiers}}' > "$out/rds-instances.json"

aws elasticache describe-replication-groups --output json --query '{ReplicationGroups: ReplicationGroups[].{
  ReplicationGroupId: ReplicationGroupId, ARN: ARN, Engine: Engine, AutomaticFailover: AutomaticFailover,
  MultiAZ: MultiAZ, ClusterEnabled: ClusterEnabled, SnapshotRetentionLimit: SnapshotRetentionLimit,
  AtRestEncryptionEnabled: AtRestEncryptionEnabled, StorageEncryptionType: StorageEncryptionType,
  MemberClusters: MemberClusters, NodeGroups: NodeGroups[].{
  NodeGroupMembers: NodeGroupMembers[].{CacheClusterId: CacheClusterId}},
  CacheNodeType: CacheNodeType}}' > "$out/elasticache-replication-groups.json"
aws elasticache describe-cache-clusters --output json --query '{CacheClusters: CacheClusters[].{
  CacheClusterId: CacheClusterId, ARN: ARN, ReplicationGroupId: ReplicationGroupId, Engine: Engine,
  NumCacheNodes: NumCacheNodes, SnapshotRetentionLimit: SnapshotRetentionLimit,
  AtRestEncryptionEnabled: AtRestEncryptionEnabled,
  CacheNodeType: CacheNodeType}}' > "$out/elasticache-cache-clusters.json"
aws elasticache describe-serverless-caches --output json --query '{ServerlessCaches: ServerlessCaches[].{
  ServerlessCacheName: ServerlessCacheName, ARN: ARN, Engine: Engine,
  SnapshotRetentionLimit: SnapshotRetentionLimit}}' \
  > "$out/elasticache-serverless-caches.json"

aws elbv2 describe-load-balancers --output json --query '{LoadBalancers: LoadBalancers[].{
  LoadBalancerName: LoadBalancerName, LoadBalancerArn: LoadBalancerArn, Type: Type,
  IpAddressType: IpAddressType, AvailabilityZones: AvailabilityZones[].{ZoneName: ZoneName}}}' \
  > "$out/elbv2-load-balancers.json"
jq -r '.LoadBalancers[].LoadBalancerArn' "$out/elbv2-load-balancers.json" | while read -r lb; do
  aws elbv2 describe-listeners --load-balancer-arn "$lb" --output json --query '{Listeners: Listeners[].{
    ListenerArn: ListenerArn, LoadBalancerArn: LoadBalancerArn, Protocol: Protocol, Port: Port}}'
done > "$out/elbv2-listeners.json"
aws elbv2 describe-target-groups --output json --query '{TargetGroups: TargetGroups[].{
  TargetGroupArn: TargetGroupArn, IpAddressType: IpAddressType, LoadBalancerArns: LoadBalancerArns}}' \
  > "$out/elbv2-target-groups.json"
# The tag keys of the load balancers, to leave out those a Kubernetes cluster created.
jq -r '.LoadBalancers[].LoadBalancerArn' "$out/elbv2-load-balancers.json" |
  xargs -r -n 20 aws elbv2 describe-tags --output json --query '{TagDescriptions: TagDescriptions[].{
    ResourceArn: ResourceArn, Tags: Tags[].{Key: Key}}}' --resource-arns > "$out/elbv2-tags.json"
aws elb describe-load-balancers --output json --query '{LoadBalancerDescriptions: LoadBalancerDescriptions[].{
  LoadBalancerName: LoadBalancerName, AvailabilityZones: AvailabilityZones}}' \
  > "$out/elb-load-balancers.json"

aws efs describe-file-systems --output json --query '{FileSystems: FileSystems[].{
  FileSystemId: FileSystemId, FileSystemArn: FileSystemArn, Encrypted: Encrypted,
  AvailabilityZoneName: AvailabilityZoneName, SizeInBytes: {Value: SizeInBytes.Value}}}' \
  > "$out/efs-file-systems.json"

aws eks list-clusters --output json | jq -r '.clusters[]' | while read -r cluster; do
  aws eks describe-cluster --name "$cluster" --output json \
    --query '{cluster: {name: cluster.name, arn: cluster.arn}}'
  aws eks list-nodegroups --cluster-name "$cluster" --output json | jq -r '.nodegroups[]' |
    while read -r nodegroup; do
      aws eks describe-nodegroup --cluster-name "$cluster" --nodegroup-name "$nodegroup" \
        --output json --query '{nodegroup: {clusterName: nodegroup.clusterName,
          nodegroupArn: nodegroup.nodegroupArn, scalingConfig: nodegroup.scalingConfig,
          resources: {autoScalingGroups: nodegroup.resources.autoScalingGroups}}}'
    done
done > "$out/eks.json"

aws lambda list-functions --output json \
  --query '{Functions: Functions[].{FunctionName: FunctionName, FunctionArn: FunctionArn}}' \
  > "$out/lambda-functions.json"

aws sqs list-queues --output json | jq -r '.QueueUrls[]?' | while read -r queue; do
  aws sqs get-queue-attributes --queue-url "$queue" --output json \
    --attribute-names QueueArn RedrivePolicy SqsManagedSseEnabled KmsMasterKeyId
done > "$out/sqs-queues.json"

aws sns list-topics --output json | jq -r '.Topics[]?.TopicArn' | while read -r topic; do
  aws sns get-topic-attributes --topic-arn "$topic" --output json --query '{Attributes: {
    TopicArn: Attributes.TopicArn, KmsMasterKeyId: Attributes.KmsMasterKeyId}}'
done > "$out/sns-topics.json"

# Optional: everything else, from AWS Config (see "Everything else on AWS").
aws configservice select-resource-config \
  --expression "SELECT resourceId, resourceName, resourceType, awsRegion" \
  --output json > "$out/config.json"
```

Every list and describe call above, `select-resource-config` included, is paginated by AWS, and the
AWS CLI version 2 fetches every page and merges them before it applies `--query`, so each file holds
the whole region. Do not pass `--no-paginate` or `--max-items`. The one exception is
`opensearchserverless list-collections`: the AWS CLI has no paginator for it and answers with one page
(20 collections by default), so the guide pages it by hand. Its loop asks for 100 collections a page,
the documented maximum, passes each page's `nextToken` back as `--next-token` until a page returns
none, and appends every page to `opensearchserverless-collections.json` as a JSON value of its own;
the `batch-get-collection` call reads the IDs of every page. A page that fails leaves the file
incomplete: the loop stops, says so on stderr, and the region must be collected again. It also stops,
with a message, if AWS hands back the token it was just given, rather than ask for the same page
forever.

`describe-instance-types` takes at most 100 instance types per call; `xargs -n 100` splits a longer list
into several calls, and the program reads each. `elbv2 describe-tags` takes at most 20 load balancers per
call, so `xargs -n 20` splits the same way. `opensearch describe-domains` is asked for 5 domains at a
time and `opensearchserverless batch-get-collection` for 100 collections, its documented maximum.

The search and Kafka calls keep only how each domain, collection or cluster is set up: never a domain's
access policy, security or Cognito options or endpoints, a collection's endpoints, or a cluster's broker
addresses or ZooKeeper connection strings. A region where OpenSearch Serverless or MSK does not run
answers with an error on stderr and an empty file, which the program reads as no resources.

`sns get-topic-attributes` keeps only the topic's ARN and its KMS key: the attributes it leaves out
include the owner's account ID and the topic's access policy.

S3 is global: `list-buckets` returns every bucket from any region, and the program lists each bucket
once. A bucket's region comes from the `BucketRegion` field, which recent versions of the AWS CLI
version 2 print; with an older CLI, buckets have no region.

### Build the AWS inventory

```bash
jq -n -f docs/guide/inventory/aws.jq inventories/aws/*/*.json > inventories/aws.inventory.json
```

Review `inventories/aws.inventory.json`, then delete the raw output: `rm -r inventories/aws`.

### AWS sizes

| Resource | Size | From |
|---|---|---|
| EC2 instance | `vcpus`, `memoryGiB` | its `InstanceType`, looked up in `ec2 describe-instance-types` (`VCpuInfo.DefaultVCpus`, `MemoryInfo.SizeInMiB` / 1024) |
| EBS volume | `storageGiB` | `ec2 describe-volumes`, `Volumes[].Size` |
| RDS DB instance (not in a DB cluster) | `storageGiB` | `rds describe-db-instances`, `DBInstances[].AllocatedStorage` |
| RDS DB instance (not in a DB cluster), single-node | `count`: 1 | `rds describe-db-instances`, `DBInstances[].MultiAZ` not true |
| EKS cluster | `count` | the sum of its managed node groups' `scalingConfig.desiredSize` (`eks describe-nodegroup`) |
| ElastiCache replication group | `count` | its nodes, primary and replicas of every shard: `elasticache describe-replication-groups`, the number of `MemberClusters` |
| ElastiCache cluster outside a group | `count` | `elasticache describe-cache-clusters`, `NumCacheNodes` |
| DocumentDB cluster | `count` | its instances: the `rds describe-db-instances` entries of engine `docdb` with its `DBClusterIdentifier` |
| EFS file system | `storageGiB` | `efs describe-file-systems`, `SizeInBytes.Value` / 1073741824 |
| OpenSearch Service domain (OpenSearch or Elasticsearch 7.x) | `vcpus`, `memoryGiB` per data node | its `ClusterConfig.InstanceType` without `.search` (`r6g.large.search` is `r6g.large`), looked up in `ec2 describe-instance-types` |
| OpenSearch Service domain | `count` | `opensearch describe-domains`, `ClusterConfig.InstanceCount` (the data nodes; dedicated master and warm nodes are not counted) |
| OpenSearch Service domain on EBS | `storageGiB` per data node | `opensearch describe-domains`, `EBSOptions.VolumeSize` when `EBSOptions.EBSEnabled` is true |
| MSK provisioned cluster | `vcpus`, `memoryGiB` per broker | its `BrokerNodeGroupInfo.InstanceType` without `kafka.` (`kafka.m5.large` is `m5.large`), looked up in `ec2 describe-instance-types` |
| MSK provisioned cluster | `count`, `storageGiB` per broker | `kafka list-clusters-v2`, `Provisioned.NumberOfBrokerNodes` and `BrokerNodeGroupInfo.StorageInfo.EbsStorageInfo.VolumeSize` |

A cache's and a DocumentDB cluster's memory is not filled in: ElastiCache names a node type
(`CacheNodeType`, such as `cache.m6g.large`) and DocumentDB an instance class (`DBInstanceClass`, such as
`db.r6g.large`), not a memory size. The ElastiCache queries project `CacheNodeType` for this alone:
`aws.jq` does not read it. Add `memoryGiB` per node by hand from the node type, and for
DocumentDB `vcpus` too from the instance class, as for an RDS instance. A DocumentDB cluster gets no
`storageGiB`: its storage is the cluster volume, which `AllocatedStorage` does not describe, as for
Aurora. An ElastiCache Serverless cache gets no size at all: it names no node type, and its data storage
setting is a limit, not a size. EFS has no provisioned size: `SizeInBytes` is the metered size of the
data stored (AWS says it is exact only when the file system has not changed for a couple of hours), and
the targets bill provisioned space, so set `storageGiB` to the size you will provision if it differs.

AWS names a search data node's or a Kafka broker's instance type, not its size. The program takes the
vCPU and memory of the EC2 type of the same name without the `.search` suffix or the `kafka.` prefix,
which `ec2 describe-instance-types` gives; AWS's API references call a broker type "the type of Amazon
EC2 instances to use for brokers", and its MSK broker-size page points to the EC2 instance types for
their sizes. A type with no EC2 twin is not looked up and its resource gets no `vcpus` or `memoryGiB`:
the OpenSearch-optimized families (`or1`, `or2`, `om2`, `oi2`) and MSK Express brokers
(`express.m7g.large`, which also have no volume to size, since MSK manages their storage). Add
`vcpus` and `memoryGiB` by hand for them from AWS's pricing pages, as for an RDS instance class. A
domain on instance storage (such as `i3` or `r6gd`, `EBSEnabled` false) gets no `storageGiB`: add the
instance's storage by hand.

An OpenSearch Serverless collection and an MSK Serverless cluster get no size: they name no node or
broker, and are billed by usage (OCUs, partitions and throughput), so the estimate says "not
estimated: size unknown".

Some target tiers have a minimum billed size or a fixed node count, which the estimate does not apply
yet: a share smaller than a target's smallest file share, a 1-node cache on a 2-node tier, or a
1-instance DocumentDB cluster on a 3-node MongoDB set is priced at its own size and `count`, below what
the target bills, and a 3-node cache on a tier of exactly 2 nodes is priced at 3, above it. Once a target
serves the category, its `SOURCES.md` names each minimum and fixed node count (PRD #552, milestones 5 to
7); check the tier the report names before you rely on such an estimate.

Not filled in: an RDS instance's vCPU and memory (from its `DBInstanceClass`, such as `db.m5.large`), the
storage of an instance in a DB cluster (Aurora, or an RDS Multi-AZ DB cluster), whose storage belongs to
the cluster (`AllocatedStorage` does not describe an instance with a `DBClusterIdentifier`), the count of
a Multi-AZ instance, an EKS cluster's per-node vCPU and memory (from its node groups' `instanceTypes`),
an Auto Scaling group's instance size and count, and an S3 bucket's stored GiB (the `BucketSizeBytes`
metric in CloudWatch). Add them by hand where you want an estimate. A Multi-AZ instance has a standby,
and how many nodes a standby counts as depends on how a target prices high availability: the program
does not guess, so the estimate says "(count assumed 1)" until you set the `count` (see
[Sizes](#sizes) for the value).

### AWS classification

The `rawType` is the resource's AWS CloudFormation type.

| `rawType` | Category | Read from |
|---|---|---|
| `AWS::EC2::Instance`, a `*.metal` or `*.metal-*` instance type (such as `m5.metal`, `c7i.metal-24xl`) | `compute.baremetal` | `ec2 describe-instances` |
| `AWS::EC2::Instance`, any other instance type | `compute.vm` | `ec2 describe-instances` |
| `AWS::AutoScaling::AutoScalingGroup` | `compute.vm` | `autoscaling describe-auto-scaling-groups` |
| `AWS::EC2::Volume` | `storage.block` | `ec2 describe-volumes` |
| `AWS::S3::Bucket` | `storage.object` | `s3api list-buckets` |
| `AWS::RDS::DBInstance`, engine `postgres` or `aurora-postgresql` | `database.postgresql` | `rds describe-db-instances` |
| `AWS::RDS::DBInstance`, engine `mysql` or `aurora-mysql` | `database.mysql` | `rds describe-db-instances` |
| `AWS::RDS::DBInstance`, any other engine but `docdb` (MariaDB, Oracle, SQL Server, Neptune) | `unclassified` | `rds describe-db-instances` |
| `AWS::DocDB::DBCluster`: one per DocumentDB cluster, from the instances of engine `docdb`, grouped by `DBClusterIdentifier` | `database.mongodb` | `rds describe-db-instances` |
| `AWS::RDS::DBCluster` (Aurora or DocumentDB) | none: its instances stand for it | `configservice select-resource-config` |
| `AWS::ElastiCache::ReplicationGroup`, engine `redis` or `valkey` | `cache.redis` | `elasticache describe-replication-groups` |
| `AWS::ElastiCache::CacheCluster` not in a replication group, engine `redis` or `valkey` | `cache.redis` | `elasticache describe-cache-clusters` |
| `AWS::ElastiCache::ServerlessCache`, engine `redis` or `valkey` | `cache.redis` | `elasticache describe-serverless-caches` |
| `AWS::ElastiCache::*` of engine `memcached` | `unclassified` | the same calls |
| `AWS::ElasticLoadBalancingV2::LoadBalancer`, type `application` or `network` | `network.loadbalancer` | `elbv2 describe-load-balancers` |
| `AWS::ElasticLoadBalancingV2::LoadBalancer`, type `gateway` | `unclassified` | `elbv2 describe-load-balancers` |
| `AWS::ElasticLoadBalancing::LoadBalancer` (Classic) | `network.loadbalancer` | `elb describe-load-balancers` |
| `AWS::EFS::FileSystem` | `storage.file` | `efs describe-file-systems` |
| `AWS::EKS::Cluster` | `kubernetes.managed` | `eks describe-cluster` |
| `AWS::Lambda::Function` | `serverless.function` | `lambda list-functions` |
| `AWS::SQS::Queue` | `messaging.queue` | `sqs get-queue-attributes` |
| `AWS::SNS::Topic` | `messaging.topic` | `sns get-topic-attributes` |
| `AWS::OpenSearchService::Domain`, engine `OpenSearch_*` or `Elasticsearch_7.*` | `search.opensearch` | `opensearch describe-domains` |
| `AWS::OpenSearchService::Domain`, engine `Elasticsearch_1.5` to `Elasticsearch_6.8` | `unclassified` | `opensearch describe-domains` |
| `AWS::OpenSearchServerless::Collection` | `search.opensearch` | `opensearchserverless batch-get-collection` |
| `AWS::MSK::Cluster` (provisioned) | `messaging.kafka` | `kafka list-clusters-v2` |
| `AWS::MSK::ServerlessCluster` | `messaging.kafka` | `kafka list-clusters-v2` |
| `AWS::OpenSearch::Domain`, `AWS::Elasticsearch::Domain` | none: AWS Config's types for a domain, which `describe-domains` lists | `configservice select-resource-config` |
| `AWS::MSK::Cluster`, `AWS::MSK::ServerlessCluster` or `AWS::OpenSearchServerless::Collection` that the service call lists by the same name or ID in the same region | none: the call lists it | `configservice select-resource-config` |
| `AWS::MSK::Configuration`, `::ClusterPolicy`, `::BatchScramSecret`, `::VpcConnection`, `AWS::OpenSearchServerless::AccessPolicy`, `::SecurityPolicy`, `::SecurityConfig`, `::LifecyclePolicy`, `::VpcEndpoint` | none: settings and parts of a cluster or collection, which stands for them | `configservice select-resource-config` |
| `AWS::KafkaConnect::Connector`, `AWS::KafkaConnect::CustomPlugin`, `AWS::OSIS::Pipeline` | `unclassified`: MSK Connect and OpenSearch Ingestion are integration runtimes, not clusters | `configservice select-resource-config` |
| `AWS::AppRunner::Service` | `serverless.container` | `configservice select-resource-config` |
| `AWS::ECR::Repository` | `registry.container` | `configservice select-resource-config` |
| `AWS::ElasticLoadBalancingV2::Listener`, `::ListenerRule`, `::TargetGroup`, `AWS::EFS::AccessPoint`, `AWS::ECR::RegistryPolicy`, `::ReplicationConfiguration`, `::PullThroughCacheRule`, `::RepositoryCreationTemplate` | none: parts or settings of a load balancer, file system or registry, which stands for them | `configservice select-resource-config` |
| any other type AWS Config records (such as `AWS::DynamoDB::Table` or `AWS::ECR::PublicRepository`) | `unclassified` | `configservice select-resource-config` |

App Runner services and ECR repositories come from AWS Config alone: the type is enough, since
`serverless.container` carries no capability and every ECR repository is encrypted at rest. Without
`config.json` they are not in the inventory.

MemoryDB is not collected and stays out of the inventory, or `unclassified` if AWS Config records it: it
is a durable primary database with a Multi-AZ transaction log, not a cache, and no capability expresses
that durability. Likewise a node-based Valkey 9.0 or later cache with durability turned on
(`Durability` in `describe-replication-groups`) keeps a Multi-AZ transaction log and may serve as a
primary database: the program still classifies it as `cache.redis`, and a target cache that snapshots
from time to time would lose that durability, so check such a cache by hand. Memcached is not the Redis
protocol and stays `unclassified`.

A domain on `Elasticsearch_7.*` is `search.opensearch` because OpenSearch is a fork of Elasticsearch
7.10.2 and keeps its REST API: a client of the typeless 7.x API speaks to OpenSearch unchanged. Check
two things by hand before you rely on its `direct` fit:

- a 7.x client that still uses typed endpoints or `include_type_name` (deprecated in 7.x, removed in
  OpenSearch 2.x and 3.x) needs a change for OpenSearch 2.x and 3.x;
- the official Elastic clients from 7.14 on refuse to talk to a server that is not Elasticsearch, so an
  application on such a client changes its client library (to an OpenSearch client, or an earlier
  Elastic one) to move.

A domain before 7.0 stays `unclassified`: 7.0 removed mapping types, so a client written against the
6.x API needs a change before it runs on OpenSearch. Beyond that boundary the engine version is not
assessed. OpenSearch Serverless speaks a subset of the OpenSearch API, and its clients must be
compatible with OpenSearch 3.x, so they work against a provisioned OpenSearch cluster too.

MSK Serverless accepts IAM authentication only, which a move replaces with the target's own; check
each client's authentication before you rely on the fit.

A search domain, a collection and a Kafka cluster get the region of their ARN, as every other AWS
resource; the ARN itself, which carries the account ID, stays out of the inventory. Two of the same
name in two regions are two entries, each with its region.

Left out:

- instances that are terminated or shutting down, and instances an Auto Scaling group launched (tag
  `aws:autoscaling:groupName`): the group stands for them. Their volumes that are deleted with them
  (`DeleteOnTermination`), such as their root volumes, go with them;
- the nodes of an EKS cluster, which the cluster stands for: the Auto Scaling group of a managed node
  group (`describe-nodegroup` names it), instances and Auto Scaling groups tagged `eks:cluster-name`
  or `kubernetes.io/cluster/<name>`, and a node's volumes that are deleted with it
  (`DeleteOnTermination`). A volume that outlives its node, such as a persistent volume, stays;
- a load balancer a Kubernetes cluster created, tagged `elbv2.k8s.aws/cluster` (the AWS Load Balancer
  Controller's tag, read with `elbv2 describe-tags`): it goes with its cluster, as the nodes do. The
  in-tree Kubernetes cloud provider's `kubernetes.io/cluster/<name>` tag is not used on load balancers,
  because AWS documents it on subnets only: a load balancer that provider created is listed, and you
  can delete it from the inventory by hand;
- the member clusters of an ElastiCache replication group (those with a `ReplicationGroupId`): the
  group stands for them, and counts them as its nodes;
- the instances of a DocumentDB cluster: the cluster is one `database.mongodb` resource, since every
  target sells MongoDB as one replica-set service, and one resource per instance would count the
  cluster several times. A cluster with no instances has no entry in `describe-db-instances` and is not
  listed;
- an Aurora cluster (`AWS::RDS::DBCluster`, which AWS Config lists): Aurora is assessed per instance,
  and each instance of the cluster is a `database.postgresql` or `database.mysql` resource. Its
  capabilities come from the instance's settings, and Aurora keeps replicas and zone redundancy at the
  cluster level, as more instances in other zones, which the instance does not report: add
  `read-replicas` or `zone-high-availability` by hand when the cluster has them;
- the AWS Config rows of what the search and Kafka calls list: a domain's `AWS::OpenSearch::Domain` and
  `AWS::Elasticsearch::Domain` rows, which name it under AWS Config's own types rather than its
  CloudFormation type, and the rows of an MSK cluster or an OpenSearch Serverless collection the call
  lists by the same name or ID (AWS Config may record a collection by its ID alone, and a cluster by
  its ARN, whose last segment is the cluster's ID);
- search domains, collections and Kafka clusters being deleted or that failed, as terminated
  instances are: a domain whose `Deleted` is true, a collection whose `status` is `DELETING` or
  `FAILED`, and a cluster whose `State` is `DELETING` or `FAILED`;
- the parts and settings of a Kafka cluster or a collection, which AWS Config records as types of
  their own: an MSK cluster's configurations, cluster policy, SCRAM secrets and VPC connections, and a
  collection's access, security (encryption and network) and lifecycle policies, security
  configurations and VPC endpoints. The cluster or collection stands for them.

An Auto Scaling group is always `compute.vm`: the guide does not read its launch template, so a
group of `*.metal` instances is classified as a group of virtual machines. Change its category to
`compute.baremetal` by hand if it runs bare-metal instances.

An EKS cluster's node groups are matched to it by name and region, so a cluster of the same name in
another region gets none of their capabilities.

| Category | Capability | When |
|---|---|---|
| `compute.vm` or `compute.baremetal` (instance) | `ipv6` | a network interface has an IPv6 address |
| `compute.vm` or `compute.baremetal` (instance) | `snapshots` | one of its volumes has a snapshot you own |
| `compute.vm` (Auto Scaling group) | `autoscaling` | `MaxSize` is greater than `MinSize` |
| `compute.vm` (Auto Scaling group) | `zone-high-availability` | it spans two or more availability zones |
| `storage.block` | `encryption-at-rest` | `Encrypted` is true |
| `storage.block` | `snapshots` | the volume has a snapshot you own |
| `storage.object` | `encryption-at-rest` | always: S3 encrypts every bucket at rest |
| `storage.object` | `object-versioning` | versioning status is `Enabled` |
| `database.postgresql`, `database.mysql` | `point-in-time-restore` | `BackupRetentionPeriod` is above 0 |
| `database.postgresql`, `database.mysql` | `read-replicas` | it has read replicas |
| `database.postgresql`, `database.mysql` | `zone-high-availability` | `MultiAZ` is true |
| `database.postgresql`, `database.mysql` | `encryption-at-rest` | `StorageEncrypted` is true |
| `database.mongodb` (DocumentDB) | `point-in-time-restore` | every instance's `BackupRetentionPeriod` is above 0 |
| `database.mongodb` (DocumentDB) | `encryption-at-rest` | every instance's `StorageEncrypted` is true |
| `database.mongodb` (DocumentDB) | `read-replicas`, `node-high-availability` | it has two or more instances |
| `database.mongodb` (DocumentDB) | `zone-high-availability` | its instances are in two or more `AvailabilityZone`s |
| `cache.redis` (replication group) | `node-high-availability` | `AutomaticFailover` is `enabled` and every one of its `NodeGroups` (shards) has two or more `NodeGroupMembers` (a replica to fail over to in every shard: a cluster-mode group must turn failover on even with none, and a shard with a primary only loses its data with its node) |
| `cache.redis` (replication group) | `zone-high-availability` | `MultiAZ` is `enabled` |
| `cache.redis` (replication group) | `sharding` | `ClusterEnabled` is true (cluster mode) |
| `cache.redis` (replication group or cluster) | `snapshots` | `SnapshotRetentionLimit` is above 0 |
| `cache.redis` (replication group) | `encryption-at-rest` | `StorageEncryptionType` is `sse-elasticache` or `sse-kms` (the effective state; `AtRestEncryptionEnabled` can be false while it is on) |
| `cache.redis` (cluster outside a group) | `encryption-at-rest` | `AtRestEncryptionEnabled` is true |
| `cache.redis` (serverless) | `node-high-availability`, `zone-high-availability`, `sharding`, `encryption-at-rest` | always: ElastiCache Serverless stores data across three zones, always encrypts it and runs cluster mode only |
| `cache.redis` (serverless) | `snapshots` | `SnapshotRetentionLimit` is above 0 |
| `network.loadbalancer` (ALB, NLB or Classic) | `zone-high-availability` | it is in two or more availability zones |
| `network.loadbalancer` (NLB) | `udp` | a listener of protocol `UDP`, `TCP_UDP`, `QUIC` or `TCP_QUIC` (QUIC runs over UDP) |
| `network.loadbalancer` (ALB or NLB) | `ipv6` | `IpAddressType` is `dualstack` or `dualstack-without-public-ipv4` **and** one of its target groups has `IpAddressType` `ipv6`: clients and backends both over IPv6. An IPv6 frontend over IPv4 target groups gets none |
| `storage.file` | `encryption-at-rest` | `Encrypted` is true |
| `storage.file` | `zone-high-availability` | a Regional file system (no `AvailabilityZoneName`), not One Zone |
| `kubernetes.managed` | `autoscaling` | a managed node group's `maxSize` is above its `minSize` |
| `kubernetes.managed` | `node-high-availability` | its managed node groups want two or more nodes in all |
| `messaging.queue` | `dead-letter-queue` | it has a `RedrivePolicy` |
| `messaging.queue` | `encryption-at-rest` | SQS-managed or KMS server-side encryption is on |
| `messaging.topic` | `encryption-at-rest` | it has a `KmsMasterKeyId` |
| `messaging.topic` | `dead-letter-queue` | never: SNS sets a dead-letter queue per subscription, which no topic attribute says; add it by hand if every subscription has one |
| `registry.container` | `encryption-at-rest` | always: ECR encrypts every repository at rest |
| `search.opensearch` (domain) | `node-high-availability` | `ClusterConfig.InstanceCount` is 2 or more |
| `search.opensearch` (domain) | `zone-high-availability` | `ClusterConfig.ZoneAwarenessEnabled` or `MultiAZWithStandbyEnabled` is true |
| `search.opensearch` (domain) | `encryption-at-rest` | `EncryptionAtRestOptions.Enabled` is true |
| `search.opensearch` (domain) | `snapshots` | always: OpenSearch Service takes automated snapshots of every domain (hourly from Elasticsearch 5.3 on) |
| `search.opensearch` (serverless collection) | `node-high-availability`, `zone-high-availability` | `standbyReplicas` is `ENABLED` |
| `search.opensearch` (serverless collection) | `encryption-at-rest` | always: OpenSearch Serverless requires encryption at rest for every collection |
| `search.opensearch` (serverless collection) | `snapshots` | always: OpenSearch Serverless takes an automated snapshot of every collection every hour, and a snapshot can be restored |
| `messaging.kafka` (provisioned) | `node-high-availability` | `NumberOfBrokerNodes` is 2 or more |
| `messaging.kafka` (provisioned) | `zone-high-availability` | two or more `ClientSubnets`: MSK spreads the brokers evenly over the subnets' zones, one subnet per zone |
| `messaging.kafka` (provisioned or serverless) | `encryption-at-rest` | always: MSK always encrypts data at rest |
| `messaging.kafka` (serverless) | `node-high-availability`, `zone-high-availability` | always: MSK manages a serverless cluster's partitions itself, and asks for at least two subnets per VPC "to help client applications switch over to another Availability Zone in the event of an outage" |

A search domain's or a Kafka cluster's high availability holds only for data that is replicated, and
no read-only call says how indexes or topics are: change it by hand when it is not.

- **Search domain:** two or more data nodes keep serving through the loss of one only for indexes with
  at least one replica, and zone awareness keeps a copy in another zone only for those too (AWS: "Without
  replicas, OpenSearch Service can't distribute copies of your data to other Availability Zones"). The
  default is one replica per index; remove `node-high-availability` and `zone-high-availability` when
  your indexes have none.
- **Kafka cluster:** a topic survives the loss of a broker only with a replication factor of 2 or more,
  and of a zone only with its replicas in several zones. MSK's default replication factor for topics
  created automatically is 3 in three zones and 2 in two; AWS recommends at least 3. Remove
  `node-high-availability` and `zone-high-availability` when your topics have a replication factor of
  1.
- **Kafka zones:** MSK takes two or three client subnets in distinct zones (exactly two in US West
  (N. California)), so a provisioned cluster always spans two or more zones and always gets
  `zone-high-availability`. It then maps `partial` to a target's single-zone Kafka plan, missing
  `zone-high-availability`, which is the intended reading: the source keeps serving through the loss of
  a zone and that plan does not.
- **Serverless collection:** AWS offers standby replicas "for high availability" and does not say in
  which zone they run; the program reads them as both capabilities, and a collection without them gets
  neither. Remove `zone-high-availability` by hand if you know your collection's replicas share a zone.

Not capabilities, and so never derived: shards and partitions (what both categories are), UltraWarm and
cold storage, dedicated master nodes, fine-grained access control, Kafka tiered storage, MSK Connect and
the schema registry. No source or target offers `point-in-time-restore` for either category. A Kafka
cluster gets no `snapshots`: MSK takes no backup of a topic's data.

A Classic Load Balancer never gets `udp` or `ipv6`: its listeners are HTTP, HTTPS, TCP or SSL, and it
has no IP address type. DocumentDB keeps backups and encryption per cluster, and AWS does not say that
an instance reports its cluster's values, so a cluster gets either capability only when every instance
reports it.

### Everything else on AWS

The service calls above, with AWS Config's App Runner and ECR types, cover what the taxonomy
classifies. Everything else (CloudFront distributions, Route 53 zones, DynamoDB tables, and also
networking and IAM resources) should show up
as `unclassified`, so the report shows what has no place to go. Two AWS services list everything:

- **AWS Config** (`configservice select-resource-config`, an SQL-like read-only query). Its
  `resourceType` is the CloudFormation type, the same convention as the table above, so the program
  reads it: App Runner services and ECR repositories are classified from it, the types the service
  calls cover and the parts of a resource listed in the table are dropped, as the calls already list
  them, and every other type becomes `unclassified`. Config only knows what its recorder records,
  per region, and the recorder must be switched on by your administrator. With a Config aggregator,
  `select-aggregate-resource-config` queries every account and region at once; its output has the
  same shape.
- **Resource Explorer** (`resource-explorer-2 search` or `list-resources`) lists the resources of
  every region from one aggregator index, but names types its own way (`cloudfront:distribution`),
  not as CloudFormation types, so the program does not read it. It is still a quick way to see what
  an account holds; its index must be created by your administrator.

Without `config.json`, the inventory holds only the resources the service calls list.

## GCP

### GCP permissions

Prefer these roles on the project, each of which reads configuration only:

- `roles/cloudasset.viewer` (Cloud Asset Inventory, which also lists Bare Metal Solution servers:
  no Bare Metal Solution role is needed, as the program reads nothing more about them);
- `roles/compute.viewer` (instances, managed instance groups, snapshots, and the URL maps, backend
  services, forwarding rules, target pools and target HTTP and HTTPS proxies of load balancers);
- `roles/cloudsql.viewer` (Cloud SQL instances);
- `roles/container.clusterViewer` (GKE clusters);
- `roles/pubsub.viewer` (Pub/Sub subscriptions);
- `roles/storage.bucketViewer` (bucket metadata, with no access to the objects in them);
- `roles/redis.viewer` (Memorystore for Redis instances and clusters);
- `roles/memorystore.viewer` (Memorystore for Valkey instances);
- `roles/file.viewer` (Filestore instances);
- `roles/cloudfunctions.viewer` (Cloud Functions, for the Cloud Run service each one runs on);
- `roles/managedkafka.viewer` (Managed Service for Apache Kafka clusters). It is read-only and lacks
  `managedkafka.clusters.connect`, so it reaches no broker and no message, but it can also read the
  topics, the consumer groups and their offsets, the ACLs, the schemas and the connector
  configurations, which can hold credentials in plain text; the listing needs none of them. If that
  is too much, grant a custom role with only `managedkafka.clusters.list` instead;
- `roles/artifactregistry.reader` (Artifact Registry repositories). It is read-only, but it can also
  download the artifacts in the repositories (`artifactregistry.repositories.downloadArtifacts`), which
  the listing does not need; no predefined Artifact Registry role lists repositories without it. If
  that is too much, grant a custom role with only `artifactregistry.repositories.list` and
  `artifactregistry.repositories.get` instead.

The simpler fallback is `roles/cloudasset.viewer` and the basic role `roles/viewer`, which covers
every call too but is broad: it reads the configuration of every service in the project, far more
than this page needs. The Cloud Asset API (`cloudasset.googleapis.com`) must be enabled on the project;
if it is not, ask your administrator to enable it.

### Collect from GCP

```bash
project=example-project   # your project ID
out=inventories/gcp
mkdir -p "$out"

# Keep only the one label gcp.jq reads, not the values of any other label.
gcloud asset search-all-resources --scope="projects/$project" --quiet \
  --format='json(name,assetType,location,labels)' |
  jq '[.[] | if .labels then .labels |= with_entries(select(.key == "goog-gke-node")) else . end]' \
  > "$out/assets.json"

# Whether the asset listing holds an asset type, in a region when one is given:
# the Memorystore, Filestore and Artifact Registry lists below run only then.
has() { jq -e --arg t "$1" --arg r "${2:-}" \
  'any(.[]; .assetType == $t and ($r == "" or (.name | contains("/locations/" + $r + "/"))))' \
  "$out/assets.json" > /dev/null; }

gcloud compute instances list --project="$project" --quiet \
  --format='json(kind,selfLink,machineType,metadata.items[].key,disks[].source,disks[].boot,disks[].autoDelete,networkInterfaces[].stackType,networkInterfaces[].ipv6AccessConfigs[].type)' \
  > "$out/compute-instances.json"
# The vCPU and memory of the machine types in use, and every disk's size.
types="$(jq -r '[.[].machineType // empty | split("/") | last] | unique | join(" ")' "$out/compute-instances.json")"
# With no instances there are no types to ask for: write an empty list instead.
if [ -n "$types" ]; then
  gcloud compute machine-types list --project="$project" --filter="name:($types)" --quiet \
    --format='json(kind,selfLink,name,zone,guestCpus,memoryMb)' > "$out/compute-machine-types.json"
else
  echo '[]' > "$out/compute-machine-types.json"
fi

gcloud compute disks list --project="$project" --quiet \
  --format='json(kind,name,zone,region,sizeGb)' > "$out/compute-disks.json"
gcloud compute instance-groups managed list --project="$project" --quiet \
  --format='json(kind,selfLink,status.autoscaler)' > "$out/compute-instance-groups.json"
gcloud compute snapshots list --project="$project" --quiet \
  --format='json(kind,sourceDisk)' > "$out/compute-snapshots.json"
gcloud sql instances list --project="$project" --quiet \
  --format='json(kind,selfLink,region,databaseVersion,replicaNames,settings.availabilityType,settings.tier,settings.dataDiskSizeGb,settings.backupConfiguration.pointInTimeRecoveryEnabled,settings.backupConfiguration.binaryLogEnabled)' \
  > "$out/sql-instances.json"
gcloud storage buckets list --project="$project" --raw --quiet \
  --format='json(kind,name,versioning)' > "$out/storage-buckets.json"
gcloud container clusters list --project="$project" --quiet \
  --format='json(selfLink,location,currentNodeCount,autopilot.enabled,nodePools[].autoscaling.enabled,nodePools[].instanceGroupUrls)' \
  > "$out/container-clusters.json"
gcloud pubsub subscriptions list --project="$project" --quiet \
  --format='json(name,deadLetterPolicy.deadLetterTopic)' > "$out/pubsub-subscriptions.json"

# Load balancers: global and every region's, each list by default.
gcloud compute url-maps list --project="$project" --quiet \
  --format='json(selfLink,region,defaultService,defaultRouteAction.weightedBackendServices[].backendService,defaultRouteAction.requestMirrorPolicy.backendService,defaultCustomErrorResponsePolicy.errorService,pathMatchers[].defaultService,pathMatchers[].defaultRouteAction.weightedBackendServices[].backendService,pathMatchers[].defaultRouteAction.requestMirrorPolicy.backendService,pathMatchers[].defaultCustomErrorResponsePolicy.errorService,pathMatchers[].pathRules[].service,pathMatchers[].pathRules[].routeAction.weightedBackendServices[].backendService,pathMatchers[].pathRules[].routeAction.requestMirrorPolicy.backendService,pathMatchers[].pathRules[].customErrorResponsePolicy.errorService,pathMatchers[].routeRules[].service,pathMatchers[].routeRules[].routeAction.weightedBackendServices[].backendService,pathMatchers[].routeRules[].routeAction.requestMirrorPolicy.backendService,pathMatchers[].routeRules[].customErrorResponsePolicy.errorService)' \
  > "$out/compute-url-maps.json"
gcloud compute backend-services list --project="$project" --quiet \
  --format='json(kind,selfLink,region,protocol,loadBalancingScheme,ipAddressSelectionPolicy)' \
  > "$out/compute-backend-services.json"
gcloud compute forwarding-rules list --project="$project" --quiet \
  --format='json(kind,selfLink,region,IPProtocol,ipVersion,target,backendService)' \
  > "$out/compute-forwarding-rules.json"
gcloud compute target-pools list --project="$project" --quiet \
  --format='json(kind,selfLink,region)' > "$out/compute-target-pools.json"
gcloud compute target-https-proxies list --project="$project" --quiet \
  --format='json(kind,selfLink,urlMap)' > "$out/compute-target-https-proxies.json"
gcloud compute target-http-proxies list --project="$project" --quiet \
  --format='json(kind,selfLink,urlMap)' > "$out/compute-target-http-proxies.json"

# Memorystore, per region: the regions come from the asset listing, and each
# list runs only in a region that holds its asset type.
for region in $(jq -r '[.[] | select(.assetType | test("^(redis|memorystore)\\.googleapis\\.com/")) | .name | capture("/locations/(?<r>[^/]+)/").r] | unique | .[]' "$out/assets.json"); do
  if has redis.googleapis.com/Instance "$region"; then
    gcloud redis instances list --project="$project" --region="$region" --quiet \
      --format='json(name,tier,memorySizeGb,replicaCount,locationId,alternativeLocationId,nodes[].zone,persistenceConfig.persistenceMode)'
  fi
  if has redis.googleapis.com/Cluster "$region"; then
    gcloud redis clusters list --project="$project" --region="$region" --quiet \
      --format='json(name,shardCount,replicaCount,nodeType,sizeGb,zoneDistributionConfig.mode,persistenceConfig.mode)'
  fi
  if has memorystore.googleapis.com/Instance "$region"; then
    gcloud memorystore instances list --project="$project" --location="$region" --quiet \
      --format='json(name,mode,shardCount,replicaCount,nodeType,nodeConfig.sizeGb,zoneDistributionConfig.mode,persistenceConfig.mode)'
  fi
done > "$out/memorystore.json"

# Filestore and Artifact Registry, when the asset listing holds one of theirs.
if has file.googleapis.com/Instance; then
  gcloud filestore instances list --project="$project" --quiet \
    --format='json(name,tier,fileShares[].capacityGb,protocol)' > "$out/filestore-instances.json"
else
  echo '[]' > "$out/filestore-instances.json"
fi
if has artifactregistry.googleapis.com/Repository; then
  gcloud artifacts repositories list --project="$project" --location=all --quiet \
    --format='json(name,format,mode)' > "$out/artifacts-repositories.json"
else
  echo '[]' > "$out/artifacts-repositories.json"
fi

# Managed Service for Apache Kafka, per region the asset listing names a cluster in,
# merged into one list (an empty one when there is none): only each cluster's name
# and capacity, never its bootstrap address or network access.
for region in $(jq -r '[.[] | select(.assetType == "managedkafka.googleapis.com/Cluster") | .name | capture("/locations/(?<r>[^/]+)/").r] | unique | .[]' "$out/assets.json"); do
  gcloud managed-kafka clusters list --project="$project" --location="$region" --quiet \
    --format='json(name,capacityConfig)'
done | jq -s 'add // []' > "$out/managed-kafka-clusters.json"

# The Cloud Run service each 2nd gen function runs on, per region with functions.
for region in $(jq -r '[.[] | select(.assetType == "cloudfunctions.googleapis.com/Function") | .name | capture("/locations/(?<r>[^/]+)/").r] | unique | .[]' "$out/assets.json"); do
  gcloud functions list --project="$project" --v2 --regions="$region" --quiet \
    --format='json(name,serviceConfig.service)'
done > "$out/functions.json"
```

The Memorystore, Filestore, Artifact Registry, Managed Kafka and Cloud Functions lists run only
when the asset listing holds a resource of theirs, so a project that does not use a service never
calls its API, which may not be enabled. Every `gcloud` call above carries `--quiet`, which makes
it take the default answer, No, when it offers to enable an API, as it does on a project with that
API disabled; if any call still asks to enable one, answer N: enabling an API writes to the project.

Check that a list is not empty when the asset listing holds its resources: a failed list (a
missing role or an interrupted run) stops nothing, and its error on stderr is the only sign. With
Cloud Functions, check `functions.json`: without it each function's Cloud Run service is counted a
second time, as a `serverless.container` (`roles/cloudfunctions.viewer`). With load balancers, check
the compute lists (`roles/compute.viewer`). A failed `compute-target-pools.json` leaves every target
pool out, with nothing in its place. A failed `compute-url-maps.json` puts, in place of each URL
map, one load balancer per backend service it names. A failed `compute-backend-services.json`
leaves out every load balancer that is a backend service alone, gives the URL maps no `udp` or
`ipv6` from their backend services, and keeps a service mesh's URL map as a load balancer. A failed
`compute-forwarding-rules.json`, `compute-target-https-proxies.json` or
`compute-target-http-proxies.json` leaves the load balancers in, but without the `ipv6` or `udp`
their forwarding rules prove. A failed Valkey list in `memorystore.json`
leaves every Valkey instance without `sharding` and without its size (`roles/memorystore.viewer`).
A failed Managed Kafka list in a region leaves that region's clusters without their size
(`roles/managedkafka.viewer`); check that `managed-kafka-clusters.json` holds every cluster the
asset listing names.

The `--format` projections keep only what the program reads: an instance's metadata keys, for
example, but not their values, which hold startup scripts and SSH keys, and of its disks only the
source, whether it boots and whether it is deleted with the instance (`autoDelete`). Of the asset
labels, `jq` keeps only `goog-gke-node`, the one the program reads; the Cloud Run service behind
each function comes from `functions list`, not from a label. Of a URL map, only the links to its
backend services are kept (its default, path and route services, the weighted and mirrored backends
of its route actions, and the error services of its custom error response policies), not its route
actions' header values. Of a Managed Kafka cluster, only its name and its capacity
(`capacityConfig`) are kept, never its bootstrap address, TLS settings or the networks allowed to
reach it.

The load balancer lists read only the HTTP and HTTPS target proxies, the frontends of an Application
Load Balancer; a TCP, SSL or gRPC proxy is not listed, so a load balancer behind one gets no `ipv6`.
The Memorystore and Managed Kafka lists run once per region the asset listing names: the gcloud
pages document no "all regions" value for them. Every call above lists or describes; none changes
anything.

The Cloud Asset Inventory listing is the list of resources, but for load balancers, which come from
the compute lists; the other calls add the settings the capabilities and sizes need. A resource the
other calls do not cover still appears, as `unclassified` or without capabilities. For several
projects, collect each into a directory of its own and build one inventory per project.

### Build the GCP inventory

```bash
jq -n -f docs/guide/inventory/gcp.jq inventories/gcp/*.json > inventories/gcp.inventory.json
```

Review `inventories/gcp.inventory.json`, then delete the raw output: `rm -r inventories/gcp`.

### GCP sizes

| Resource | Size | From |
|---|---|---|
| Compute Engine instance | `vcpus`, `memoryGiB` | its `machineType`, looked up in `compute machine-types list` for its zone (`guestCpus`, `memoryMb` / 1024) |
| Persistent disk | `storageGiB` | `compute disks list`, `sizeGb` (a string in the API's JSON) |
| Cloud SQL instance (PostgreSQL or MySQL) | `storageGiB` | `sql instances list`, `settings.dataDiskSizeGb` (a string in the API's JSON) |
| Cloud SQL instance, custom tier | `vcpus`, `memoryGiB` | `sql instances list`, `settings.tier` `db-custom-<vCPU>-<memory MiB>` (`db-custom-2-7680`: 2 vCPU, 7.5 GiB) |
| Cloud SQL instance, single-node | `count`: 1 | `sql instances list`, `settings.availabilityType` `ZONAL` |
| Memorystore for Redis instance | `memoryGiB` (per node), `count` | `redis instances list`, `memorySizeGb` (each node holds the whole data set) and the number of `nodes` (for an instance that lists none, 1 for `BASIC` and `replicaCount` + 1 for `STANDARD_HA`) |
| Memorystore for Redis Cluster | `count` | `redis clusters list`, `shardCount` × (`replicaCount` + 1) |
| Memorystore for Valkey instance | `memoryGiB` (per node), `count` | `memorystore instances list`, `nodeConfig.sizeGb` and `shardCount` × (`replicaCount` + 1) |
| Filestore instance | `storageGiB` | `filestore instances list`, the sum of `fileShares[].capacityGb` (a string in the API's JSON) |
| Managed Service for Apache Kafka cluster | `count`, `vcpus` and `memoryGiB` per broker | `managed-kafka clusters list`, `capacityConfig`: `count` is max(3, ceiling(`vcpuCount` / 15)), and `vcpus` and `memoryGiB` are `vcpuCount` and `memoryBytes` / 1024³ divided by `count` (both strings in the API's JSON) |

The Memorystore and Filestore APIs write GB; Filestore defines a GB as 1024³ bytes, and the program
takes every one of these GB as GiB. A Redis cluster names only the memory of the whole cluster
(`sizeGb`), and no page says whether it counts the replicas, so the program writes no `memoryGiB` for
it: add the memory of one node by hand (its `nodeType`, from Google's node specification) for an
estimate.

A Managed Kafka cluster names its total vCPU and memory, not its brokers. Google's
[broker provisioning](https://cloud.google.com/managed-service-for-apache-kafka/docs/brokers) page
says "number of brokers = max(3, ceiling(vCPUs / 15))" and that "all brokers have the same number of
vCPUs, which might be a fractional value", so the program writes that count and each broker's equal
share of the vCPU, which may be fractional. The memory split is inferred, not stated: memory is
provisioned per cluster at a fixed vCPU:GiB ratio between 1:1 and 1:8
([`CapacityConfig`](https://cloud.google.com/managed-service-for-apache-kafka/docs/reference/rest/v1/CapacityConfig)),
and all brokers have the same vCPUs, so the program gives each broker an equal share of the memory
too. The brokers do have a disk, which Google provisions by itself, but the list call gives no disk
size (`capacityConfig` holds only `vcpuCount` and `memoryBytes`), so the program writes no
`storageGiB`: add each broker's disk by hand for a storage estimate. A cluster the list does not
hold gets no size.

Not filled in: the vCPU and memory of a Cloud SQL instance whose tier is not a custom one (such as
`db-n1-standard-2` or `db-f1-micro`), the count of a `REGIONAL` Cloud SQL instance, a GKE cluster's
node count and machine type, a managed instance group's size and instance template, and a bucket's
stored GiB. Add them by hand where you want an estimate. A `REGIONAL` instance has a standby, and how
many nodes a standby counts as depends on how a target prices high availability: the program does not
guess, so the estimate says "(count assumed 1)" until you set the `count` (see [Sizes](#sizes) for the
value). A custom machine type (`custom-4-16384`) is not in `machine-types list`; its vCPU and memory are
in its name.

### GCP classification

The `rawType` is the resource's Cloud Asset Inventory asset type.

| `rawType` | Category |
|---|---|
| `compute.googleapis.com/Instance` | `compute.vm` |
| `compute.googleapis.com/InstanceGroupManager` | `compute.vm` |
| `baremetalsolution.googleapis.com/Instance` (Bare Metal Solution) | `compute.baremetal` |
| `compute.googleapis.com/Disk` | `storage.block` |
| `storage.googleapis.com/Bucket` | `storage.object` |
| `sqladmin.googleapis.com/Instance`, `databaseVersion` `POSTGRES_*` | `database.postgresql` |
| `sqladmin.googleapis.com/Instance`, `databaseVersion` `MYSQL_*` | `database.mysql` |
| `sqladmin.googleapis.com/Instance`, any other engine, or not in `sql instances list` | `unclassified` |
| `container.googleapis.com/Cluster` | `kubernetes.managed` |
| `cloudfunctions.googleapis.com/Function`, `cloudfunctions.googleapis.com/CloudFunction` | `serverless.function` |
| `pubsub.googleapis.com/Topic` | `messaging.topic` |
| `pubsub.googleapis.com/Subscription` | `messaging.queue` |
| `run.googleapis.com/Service` that no 2nd gen Cloud Function runs on | `serverless.container` |
| `redis.googleapis.com/Instance`, `redis.googleapis.com/Cluster` (Memorystore for Redis) | `cache.redis` |
| `memorystore.googleapis.com/Instance` (Memorystore for Valkey) | `cache.redis` |
| `compute.googleapis.com/UrlMap`, from `compute url-maps list` | `network.loadbalancer` |
| `compute.googleapis.com/BackendService` that no URL map names, from `compute backend-services list` | `network.loadbalancer` |
| `compute.googleapis.com/TargetPool`, from `compute target-pools list` | `network.loadbalancer` |
| `artifactregistry.googleapis.com/Repository`, `format` `DOCKER` | `registry.container` |
| `artifactregistry.googleapis.com/Repository`, any other format, or not in `artifacts repositories list` | `unclassified` |
| `file.googleapis.com/Instance` (Filestore) | `storage.file` |
| `managedkafka.googleapis.com/Cluster` (Managed Service for Apache Kafka) | `messaging.kafka` |
| `managedkafka.googleapis.com/ConnectCluster` (Kafka Connect) | `unclassified` |
| `memcache.googleapis.com/Instance` (Memorystore for Memcached) | `unclassified` |
| any other asset type | `unclassified` |

A Pub/Sub topic (where publishers write, and every subscription gets its own copy of each message) and
a subscription (where consumers read) are both listed, as both have to move. Memcached speaks another
protocol than Redis, so it stays `unclassified`. A Kafka Connect cluster is an integration runtime
around a Kafka cluster, not a cluster to move, so it stays `unclassified` too.

A load balancer is one URL map (an Application Load Balancer), one backend service no URL map names (a
passthrough Network Load Balancer, or a proxy Network Load Balancer) or one target pool. These come
from the compute lists, not from the asset listing, which may not hold a regional URL map: the asset
rows of URL maps, backend services and target pools are left out, so none is listed twice. A backend
service of scheme `INTERNAL_SELF_MANAGED` belongs to a service mesh (no load balancer runs in front of
its backends) and is left out, as is a URL map that names only such services. A load balancer a GKE
cluster created is kept: URL maps and backend services carry no labels, and no documented rule tells
them apart, so delete it from the inventory by hand if the cluster's move recreates it.

Left out: instances a managed instance group created (metadata `created-by`; the group stands for
them) and the disks they delete with them (`disks[].autoDelete`, boot disk or not), the nodes of a GKE
cluster (the managed instance groups its node pools name in `instanceGroupUrls`, and instances and
disks labelled `goog-gke-node`; the cluster stands for them, while a persistent volume's disk stays),
the Cloud Run service behind a 2nd gen Cloud Function (the one its `serviceConfig.service` names in
`functions list`; the function stands for it), a load balancer's forwarding rules and target proxies
(its frontends, read for its protocol and IP version; the URL map, backend service or target pool
stands for the load balancer), the backend services a URL map names, target instances (the asset
rows of `TargetInstance`, a forwarding rule's target that is no load balancer), Artifact Registry
repositories of mode `REMOTE_REPOSITORY` or `VIRTUAL_REPOSITORY` (they pull through from an upstream
or aggregate other repositories, and store no image of their own), compute and Filestore snapshots
and autoscalers, which become capabilities of their disk, file share or group, and Filestore backups,
which belong to their instance.

| Category | Capability | When |
|---|---|---|
| `compute.vm` (instance) | `ipv6` | a network interface has stack type `IPV4_IPV6` or an IPv6 access config |
| `compute.vm` (managed instance group) | `autoscaling` | an autoscaler is attached (`status.autoscaler`) |
| `compute.vm` (managed instance group) | `zone-high-availability` | the group is regional |
| `storage.block` | `encryption-at-rest` | always: Google Cloud encrypts all data at rest |
| `storage.block` | `snapshots` | a snapshot's `sourceDisk` is the disk |
| `storage.object` | `encryption-at-rest` | always |
| `storage.object` | `object-versioning` | `versioning.enabled` is true |
| `database.*` | `encryption-at-rest` | always |
| `database.*` | `point-in-time-restore` | `pointInTimeRecoveryEnabled` is true, or for MySQL `binaryLogEnabled` |
| `database.*` | `zone-high-availability` | `availabilityType` is `REGIONAL` |
| `database.*` | `read-replicas` | `replicaNames` is not empty |
| `kubernetes.managed` | `zone-high-availability` | the cluster is regional |
| `kubernetes.managed` | `node-high-availability` | it runs two or more nodes |
| `kubernetes.managed` | `autoscaling` | Autopilot, or a node pool with autoscaling |
| `messaging.queue` (subscription) | `dead-letter-queue` | it has a dead-letter topic |
| `messaging.topic` | `encryption-at-rest` | always: Pub/Sub encrypts messages at rest. A dead-letter topic is set on a subscription, never on a topic, so a topic gets no `dead-letter-queue` |
| `cache.redis` | `encryption-at-rest` | always |
| `cache.redis` (Redis instance) | `node-high-availability` | tier `STANDARD_HA` |
| `cache.redis` (Redis instance) | `zone-high-availability` | tier `STANDARD_HA`, and either its `nodes` are in two or more zones or its `alternativeLocationId` is another zone than its `locationId`: either one is enough |
| `cache.redis` (Redis instance) | `snapshots` | `persistenceConfig.persistenceMode` is `RDB` |
| `cache.redis` (Redis cluster, Valkey) | `node-high-availability` | `replicaCount` is 1 or more |
| `cache.redis` (Redis cluster, Valkey) | `zone-high-availability` | `replicaCount` is 1 or more and `zoneDistributionConfig.mode` is not `SINGLE_ZONE` (absent means `MULTI_ZONE`) |
| `cache.redis` (Redis cluster, Valkey) | `sharding` | always for a Redis cluster; a listed Valkey instance unless its `mode` is `CLUSTER_DISABLED` or its deprecated spelling `STANDALONE` (absent or `MODE_UNSPECIFIED` means `CLUSTER`) |
| `cache.redis` (Redis cluster, Valkey) | `snapshots` | `persistenceConfig.mode` is `RDB` (an AOF log is not a snapshot) |
| `network.loadbalancer` | `zone-high-availability` | always: every GCP load balancer is regional or global, and a global one counts only for its zone redundancy within a region |
| `network.loadbalancer` | `udp` | one of its backend services has `protocol` `UDP`, or a forwarding rule of `IPProtocol` `UDP` reaches it |
| `network.loadbalancer` | `ipv6` | a passthrough load balancer when a forwarding rule of `ipVersion` `IPV6` names its backend service directly in `backendService` (not through a target proxy) and that backend service has `loadBalancingScheme` `EXTERNAL` or `INTERNAL`: it delivers IPv6 to dual-stack or IPv6-only backends and has no IP selection policy. A scheme alone is not enough, as a classic Application Load Balancer and a proxy Network Load Balancer are `EXTERNAL` too. A proxy-based load balancer (a URL map, a target proxy, or a backend service of scheme `EXTERNAL_MANAGED` or `INTERNAL_MANAGED`) when a forwarding rule of `ipVersion` `IPV6` reaches it **and** at least one backend service it names has `ipAddressSelectionPolicy` `IPV6_ONLY` or `PREFER_IPV6`: one IPv6 backend service is enough (say, one path of a URL map), the others may stay IPv4, so a load balancer whose main paths are IPv6 is not under-reported. An IPv6 frontend over IPv4 backends only gets none. A target pool takes IPv4 only and gets none |
| `registry.container` | `encryption-at-rest` | always |
| `storage.file` | `encryption-at-rest` | always |
| `storage.file` | `zone-high-availability` | tier `REGIONAL` or `ENTERPRISE` |
| `storage.file` | `snapshots` | a Filestore snapshot of the instance is in the asset listing |
| `messaging.kafka` | `encryption-at-rest` | always: Google Cloud encrypts all data at rest |
| `messaging.kafka` | `node-high-availability`, `zone-high-availability` | always: Google runs every cluster's brokers in three zones of its region, with the replicas spread across them |

A Kafka cluster's high availability holds only for topics with a replication factor of 2 or more, which
no read-only call gives: remove `node-high-availability` and `zone-high-availability` by hand when your
topics have a replication factor of 1. It then maps `partial` to a target's single-zone Kafka plan,
missing `zone-high-availability`, which is the intended reading.

A forwarding rule of `IPProtocol` `L3_DEFAULT` forwards every protocol, but no page read says so in
as many words, so it does not count as UDP: add `udp` by hand for such a load balancer that carries
UDP.

## Azure

### Azure permissions

The built-in `Reader` role on the subscription. Its one action, `*/read`, covers Resource Graph and
the management-plane reads below (Service Bus queues, topics and topic subscriptions, storage queues,
blob service properties, file shares and Redis Enterprise databases); none of the commands reads a
storage account key, an access key or any data. The three reads added for file shares, topic
subscriptions and Azure Managed Redis are the actions
`Microsoft.Storage/storageAccounts/fileServices/shares/read`,
`Microsoft.ServiceBus/namespaces/topics/subscriptions/read` and
`Microsoft.Cache/redisEnterprise/databases/read`, all within `*/read`. An HDInsight cluster is read
only through what the Resource Graph query below projects, which `*/read` covers: no HDInsight
command, and no action that reads a cluster's configuration or credentials, is needed or granted.

### Collect from Azure

Resource Graph returns at most 1,000 rows per call, so the loop below fetches page after page until
no skip token is left. It reads the subscription the CLI is set to (`az account show`):

```bash
subscription="$(az account show --query id --output tsv)"
out=inventories/azure
mkdir -p "$out"

# Keep only the properties azure.jq reads, and only for the types it reads them from.
# An HDInsight cluster has a branch of its own: its roles array is expanded and
# only its kind, its state and the worker role's count, VM size and disks are
# packed back, as each role
# also carries its admin user, SSH key, script actions and network.
query="Resources
| where type =~ 'microsoft.hdinsight/clusters'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| extend properties = bag_pack(
    'clusterDefinition', bag_pack('kind', properties.clusterDefinition.kind),
    'clusterState', properties.clusterState,
    'computeProfile', bag_pack('roles', pack_array(bag_pack(
      'name', role.name,
      'targetInstanceCount', role.targetInstanceCount,
      'hardwareProfile', bag_pack('vmSize', role.hardwareProfile.vmSize),
      'dataDisksGroups', pack_array(bag_pack(
        'disksPerNode', role.dataDisksGroups[0].disksPerNode,
        'diskSizeGB', role.dataDisksGroups[0].diskSizeGB))))))
| union (Resources
  | where type !~ 'microsoft.hdinsight/clusters'
  | extend properties = case(
    type =~ 'microsoft.compute/virtualmachines', bag_pack(
      'virtualMachineScaleSet', bag_pack('id', properties.virtualMachineScaleSet.id),
      'storageProfile', bag_pack(
        'osDisk', bag_pack('managedDisk', bag_pack('id', properties.storageProfile.osDisk.managedDisk.id)),
        'dataDisks', properties.storageProfile.dataDisks),
      'networkProfile', bag_pack('networkInterfaces', properties.networkProfile.networkInterfaces),
      'hardwareProfile', bag_pack('vmSize', properties.hardwareProfile.vmSize)),
    type =~ 'microsoft.compute/disks',
      bag_pack('diskSizeGB', properties.diskSizeGB),
    type =~ 'microsoft.network/networkinterfaces',
      bag_pack('ipConfigurations', properties.ipConfigurations),
    type =~ 'microsoft.compute/snapshots',
      bag_pack('creationData', bag_pack('sourceResourceId', properties.creationData.sourceResourceId)),
    type =~ 'microsoft.insights/autoscalesettings',
      bag_pack('enabled', properties.enabled, 'targetResourceUri', properties.targetResourceUri),
    type in~ ('microsoft.dbforpostgresql/flexibleservers', 'microsoft.dbformysql/flexibleservers'),
      bag_pack('backup', bag_pack('backupRetentionDays', properties.backup.backupRetentionDays),
        'highAvailability', bag_pack('mode', properties.highAvailability.mode),
        'sourceServerResourceId', properties.sourceServerResourceId,
        'storage', bag_pack('storageSizeGB', properties.storage.storageSizeGB)),
    type =~ 'microsoft.containerservice/managedclusters',
      bag_pack('nodeResourceGroup', properties.nodeResourceGroup,
        'agentPoolProfiles', properties.agentPoolProfiles),
    type =~ 'microsoft.compute/virtualmachinescalesets',
      bag_pack('virtualMachineProfile', bag_pack('networkProfile',
        bag_pack('networkInterfaceConfigurations',
          properties.virtualMachineProfile.networkProfile.networkInterfaceConfigurations))),
    type =~ 'microsoft.cache/redis',
      bag_pack('sku', properties.sku,
        'shardCount', properties.shardCount,
        'replicasPerMaster', properties.replicasPerMaster,
        'replicasPerPrimary', properties.replicasPerPrimary,
        'zonalAllocationPolicy', properties.zonalAllocationPolicy,
        'redisConfiguration', bag_pack('rdb-backup-enabled', properties.redisConfiguration['rdb-backup-enabled'])),
    type =~ 'microsoft.cache/redisenterprise',
      bag_pack('highAvailability', properties.highAvailability,
        'redundancyMode', properties.redundancyMode),
    type =~ 'microsoft.documentdb/databaseaccounts',
      bag_pack('backupPolicy', bag_pack('type', properties.backupPolicy.type),
        'locations', properties.locations),
    type =~ 'microsoft.documentdb/mongoclusters',
      bag_pack('highAvailability', bag_pack('targetMode', properties.highAvailability.targetMode),
        'sharding', bag_pack('shardCount', properties.sharding.shardCount),
        'storage', bag_pack('sizeGb', properties.storage.sizeGb),
        'compute', bag_pack('tier', properties.compute.tier),
        'replicaParameters', bag_pack('sourceResourceId', properties.replicaParameters.sourceResourceId)),
    type =~ 'microsoft.network/loadbalancers',
      bag_pack('frontendIPConfigurations', properties.frontendIPConfigurations,
        'backendAddressPools', properties.backendAddressPools,
        'loadBalancingRules', properties.loadBalancingRules),
    type =~ 'microsoft.network/publicipaddresses',
      bag_pack('publicIPAddressVersion', properties.publicIPAddressVersion),
    type =~ 'microsoft.fileshares/fileshares',
      bag_pack('protocol', properties.protocol,
        'provisionedStorageGiB', properties.provisionedStorageGiB,
        'redundancy', properties.redundancy),
    dynamic(null)))
| project id, name, type, kind, location, zones, sku, properties"
page=1
token=""
while :; do
  if [ -z "$token" ]; then
    az graph query --graph-query "$query" --subscriptions "$subscription" --first 1000 \
      --output json > "$out/graph-$page.json"
  else
    az graph query --graph-query "$query" --subscriptions "$subscription" --first 1000 \
      --skip-token "$token" --output json > "$out/graph-$page.json"
  fi
  token="$(jq -r '.skip_token // empty' "$out/graph-$page.json")"
  [ -n "$token" ] || break
  page=$((page + 1))
done

az servicebus namespace list --query "[].[name, resourceGroup]" --output tsv |
  while read -r name group; do
    az servicebus queue list --namespace-name "$name" --resource-group "$group" \
      --query "[].{id: id, name: name, type: type}" --output json
    az servicebus topic list --namespace-name "$name" --resource-group "$group" \
      --query "[].{id: id, name: name, type: type}" --output json
    az servicebus topic list --namespace-name "$name" --resource-group "$group" \
      --query "[].name" --output tsv |
      while read -r topic; do
        az servicebus topic subscription list --namespace-name "$name" --resource-group "$group" \
          --topic-name "$topic" --query "[].{id: id, name: name, type: type}" --output json
      done
  done > "$out/servicebus.json"

az storage account list --query "[?kind=='StorageV2' || kind=='Storage'].id" --output tsv |
  while read -r id; do
    az rest --method get \
      --url "https://management.azure.com${id}/queueServices/default/queues?api-version=2023-05-01" \
      --query "value[].{id: id, name: name, type: type}" --output json
  done > "$out/storage-queues.json"

az storage account list --query "[].[name, resourceGroup]" --output tsv |
  while read -r name group; do
    az storage account blob-service-properties show --account-name "$name" \
      --resource-group "$group" --query "{id: id, type: type, isVersioningEnabled: isVersioningEnabled}" \
      --output json
  done > "$out/blob-services.json"

# The file shares of FileStorage accounts, the only ones with NFS shares, and their snapshots.
az storage account list --query "[?kind=='FileStorage'].id" --output tsv |
  while read -r id; do
    az rest --method get \
      --url "https://management.azure.com${id}/fileServices/default/shares?api-version=2023-05-01&\$expand=snapshots" \
      --query "value[].{id: id, name: name, type: type, properties: {enabledProtocols: properties.enabledProtocols, shareQuota: properties.shareQuota, snapshotTime: properties.snapshotTime}}" \
      --output json
  done > "$out/file-shares.json"

# The databases of every Azure Managed Redis (Redis Enterprise) cluster: clustering and persistence.
jq -r '.data[] | select(.type == "microsoft.cache/redisenterprise") | .id' "$out"/graph-*.json |
  while read -r id; do
    az rest --method get --url "https://management.azure.com${id}/databases?api-version=2026-09-01" \
      --query "value[].{id: id, name: name, type: type, properties: {clusteringPolicy: properties.clusteringPolicy, persistence: properties.persistence}}" \
      --output json
  done > "$out/redis-databases.json"

# The vCPU and memory of every VM size, in each region with virtual machines, scale sets
# or HDInsight clusters.
jq -r '[.data[] | select(.type == "microsoft.compute/virtualmachines"
    or .type == "microsoft.compute/virtualmachinescalesets"
    or .type == "microsoft.hdinsight/clusters") | .location] | unique[]' "$out"/graph-*.json |
  while read -r location; do
    az vm list-sizes --location "$location" --output json
  done > "$out/vm-sizes.json"
```

The query keeps no admin user names, SSH keys, server logins or host names. Of a cache's
`redisConfiguration` it keeps only `rdb-backup-enabled`: the whole object can hold a storage
connection string (`rdb-storage-connection-string`), a secret. No query or call keeps an access key, a
connection string or a cache's host name. It keeps whole arrays where the program reads one field of
each element (a virtual machine's data disks and network interfaces, a network interface's IP
configurations with their private IP addresses, a scale set's network interface configurations, a
load balancer's frontend IP configurations, backend pools with their members' IP addresses and
load-balancing rules, a Cosmos DB account's locations, an AKS cluster's node pools with their labels,
tags (keys and values), taints, subnet IDs and node image versions), as Resource Graph cannot trim
inside an array. So a Cosmos DB account's locations keep each region's endpoint host name
(`documentEndpoint`), and a scale set's network interface configurations keep a public IP
address's DNS label (`dnsSettings.domainNameLabel`), where one is set. An HDInsight cluster's roles
are the exception: each role carries the cluster's admin user and SSH key (`osProfile`), its script
actions and its virtual network, so the query expands the roles (`mv-expand`), keeps the worker role
and packs back only its name, count, VM size and first data disk group, with the cluster's kind. Its
gateway host names (`connectivityEndpoints`) are not kept either. A cluster without a role named
`workernode` drops out of the listing; add it by hand.

Resource Graph holds most settings; Service Bus queues, topics and topic subscriptions, storage
queues, a storage account's blob versioning, file shares, the databases of an Azure Managed Redis
cluster and the VM sizes are not in it, hence the loops. Storage queues, file shares and Redis
Enterprise databases are listed through the Resource Manager API (`az rest --method get`, a read), not
the data plane, so `Reader` is enough and no account key is needed. A Basic tier Service Bus
namespace has no topics, and its `topic list` fails: that error is expected. An account with more
queues or shares, or a Redis Enterprise cluster with more databases, than one response returns adds a
`nextLink` to the response; run that call without `--query` and fetch the `nextLink` URL with
`az rest --method get --url` as well. The shares call
lists each share snapshot as one more element with its share's ID and a `snapshotTime`; the program
counts it as a snapshot of the share, not as a share.

### Build the Azure inventory

```bash
jq -n -f docs/guide/inventory/azure.jq inventories/azure/*.json > inventories/azure.inventory.json
```

Review `inventories/azure.inventory.json`, then delete the raw output: `rm -r inventories/azure`.

### Azure sizes

| Resource | Size | From |
|---|---|---|
| Virtual machine | `vcpus`, `memoryGiB` | `properties.hardwareProfile.vmSize`, looked up in `az vm list-sizes` (`numberOfCores`, `memoryInMb` / 1024) |
| Scale set | `vcpus`, `memoryGiB`, `count` | its `sku.name`, looked up the same way, and `sku.capacity` |
| Managed disk | `storageGiB` | `properties.diskSizeGB` |
| PostgreSQL or MySQL flexible server | `storageGiB` | `properties.storage.storageSizeGB` |
| PostgreSQL or MySQL flexible server, single-node | `count`: 1 | `properties.highAvailability.mode` `Disabled` or not set |
| AKS cluster | `count` | the sum of its agent pools' `count` |
| Azure Cache for Redis | `count` | Basic 1; Standard 2; Premium (1 + `properties.replicasPerPrimary`, or `replicasPerMaster`, 1 when neither is set) × `properties.shardCount` (at least 1) |
| Azure Managed Redis (Redis Enterprise) | `count` | 2 (a primary and a replica) with high availability (a legacy Enterprise or Enterprise Flash cluster always has it), else 1 |
| Azure DocumentDB (`mongoClusters`) | `count` | `properties.sharding.shardCount` (at least 1), × 2 with high availability (a standby per shard) |
| Azure DocumentDB (`mongoClusters`) | `storageGiB` | `properties.storage.sizeGb` (per shard) × the shard count |
| NFS file share of a FileStorage account | `storageGiB` | `properties.shareQuota` |
| `Microsoft.FileShares` file share | `storageGiB` | `properties.provisionedStorageGiB` |
| HDInsight Kafka cluster | `count` | the worker role's `targetInstanceCount` |
| HDInsight Kafka cluster | `vcpus`, `memoryGiB` | the worker role's `hardwareProfile.vmSize`, looked up in `az vm list-sizes` |
| HDInsight Kafka cluster | `storageGiB` | the worker role's `dataDisksGroups[0]`: `disksPerNode` × `diskSizeGB` (per worker) |

Not filled in: a flexible server's vCPU and memory (its `sku.name`, such as `Standard_D2ds_v4`), the
count of a flexible server with high availability (`ZoneRedundant` or `SameZone`), an AKS cluster's
per-node vCPU and memory (its pools' `vmSize`), a storage account's stored GiB, a cache's memory, an
Azure DocumentDB cluster's vCPU and memory, and a Cosmos DB account's size (it is billed in request
units, not nodes). Add them by hand where you want an estimate. An Azure Cache for Redis node's memory
follows its `properties.sku` family and capacity (per shard for a clustered Premium cache):

| SKU | C0 | C1 | C2 | C3 | C4 | C5 | C6 | P1 | P2 | P3 | P4 | P5 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Memory | 250 MB | 1 GB | 2.5 GB | 6 GB | 13 GB | 26 GB | 53 GB | 6 GB | 13 GB | 26 GB | 53 GB | 120 GB |

An Azure DocumentDB cluster's vCPU and memory per shard follow its `properties.compute.tier`:

| Tier | M10 | M20 | M25 | M30 | M40 | M50 | M60 | M80 | M200 |
|---|---|---|---|---|---|---|---|---|---|
| vCores | 1 (burstable) | 2 (burstable) | 2 (burstable) | 2 | 4 | 8 | 16 | 32 | 64 |
| Memory (GiB) | 2 | 4 | 8 | 8 | 16 | 32 | 64 | 128 | 256 |

An Azure Managed Redis cluster's memory is its `sku.name`'s size, which the program does not look up.
A legacy Enterprise or Enterprise Flash cluster (`sku.name` such as `Enterprise_E10`) runs
`sku.capacity` nodes (its scale), which the program does not read either: its count
of 2 may understate it, so set the `count` by hand from `sku.capacity`.

A flexible server with high availability has a standby, and how many nodes a standby counts as depends
on how a target prices high availability: the program does not guess, so the estimate says "(count
assumed 1)" until you set the `count` (see [Sizes](#sizes) for the value). A DocumentDB cluster's
standbys are counted as nodes all the same: the targets sell managed MongoDB per node, and the source
lists and runs each standby as a node of its own.

### Azure classification

The `rawType` is the resource's Azure Resource Manager type, taken from its resource ID (Resource
Graph writes types in lower case).

| `rawType` | Category |
|---|---|
| `Microsoft.Compute/virtualMachines` | `compute.vm` |
| `Microsoft.Compute/virtualMachineScaleSets` | `compute.vm` |
| `Microsoft.BareMetalInfrastructure/bareMetalInstances` (BareMetal Infrastructure) | `compute.baremetal` |
| `Microsoft.AzureLargeInstance/azureLargeInstances` (SAP HANA Large Instances) | `compute.baremetal` |
| `Microsoft.Compute/disks` | `storage.block` |
| `Microsoft.Storage/storageAccounts` (not kind `FileStorage`) | `storage.object` |
| `Microsoft.Storage/storageAccounts`, kind `FileStorage`, with no share listed | `unclassified` |
| `Microsoft.Storage/storageAccounts/fileServices/shares`, protocol `NFS` | `storage.file` |
| `Microsoft.Storage/storageAccounts/fileServices/shares`, protocol `SMB` (or none given) | `unclassified` |
| `Microsoft.FileShares/fileShares`, protocol `NFS` | `storage.file` |
| `Microsoft.DBforPostgreSQL/flexibleServers` | `database.postgresql` |
| `Microsoft.DBforMySQL/flexibleServers` | `database.mysql` |
| `Microsoft.DocumentDB/databaseAccounts`, kind `MongoDB` (Azure Cosmos DB for MongoDB, RU) | `database.mongodb` |
| `Microsoft.DocumentDB/databaseAccounts`, any other kind | `unclassified` |
| `Microsoft.DocumentDB/mongoClusters` (Azure DocumentDB, vCore) | `database.mongodb` |
| `Microsoft.DBforMariaDB/servers` | `unclassified` |
| `Microsoft.Cache/redis` (Azure Cache for Redis) | `cache.redis` |
| `Microsoft.Cache/redisEnterprise` (Azure Managed Redis) | `cache.redis` |
| `Microsoft.ContainerService/managedClusters` | `kubernetes.managed` |
| `Microsoft.Web/sites`, kind `functionapp` | `serverless.function` |
| `Microsoft.Web/sites`, any other kind (web app, Logic App) | `unclassified` |
| `Microsoft.App/containerApps`, kind `functionapp` | `serverless.function` |
| `Microsoft.App/containerApps`, kind `workflowapp` (Logic App) | `unclassified` |
| `Microsoft.App/containerApps`, any other kind | `serverless.container` |
| `Microsoft.ContainerRegistry/registries` | `registry.container` |
| `Microsoft.Network/loadBalancers`, SKU `Standard` or `Basic` | `network.loadbalancer` |
| `Microsoft.Network/loadBalancers`, SKU `Gateway` | `unclassified` |
| `Microsoft.Network/applicationGateways` | `network.loadbalancer` |
| `Microsoft.ServiceBus/namespaces/queues` | `messaging.queue` |
| `Microsoft.ServiceBus/namespaces/topics` | `messaging.topic` |
| `Microsoft.ServiceBus/namespaces/topics/subscriptions` | `messaging.queue` |
| `Microsoft.Storage/storageAccounts/queueServices/queues` | `messaging.queue` |
| `Microsoft.HDInsight/clusters`, `clusterDefinition.kind` `KAFKA` (any casing) | `messaging.kafka` |
| `Microsoft.HDInsight/clusters`, any other kind (Spark, Hadoop, HBase, ...) | `unclassified` |
| `Microsoft.EventHub/namespaces` (Event Hubs) | `unclassified` |
| `Microsoft.Search/searchServices` (Azure AI Search) | `unclassified` |
| any other type | `unclassified` |

An Event Hubs namespace stays `unclassified` even when its Kafka endpoint is on: most Event Hubs
clients use AMQP or the Event Hubs SDKs, and no read-only call says which protocol they use, so
listing it as Kafka could hide a client rewrite. Only a Standard, Premium or Dedicated namespace has
a Kafka endpoint; a Basic namespace has none and stays `unclassified`. Change a Standard, Premium or
Dedicated namespace's category to `messaging.kafka` by hand only when every client of the namespace
uses its Kafka endpoint. Azure AI Search is no OpenSearch
service, so it stays `unclassified`.

An HDInsight Kafka cluster's virtual machines and disks are not separate resources in the listing:
the cluster stands for them, and its size is its worker nodes'. No cluster setting gives a topic's
replication factor, which decides whether a topic survives the loss of a broker: three or more
workers are counted as `node-high-availability`, so check the topics' replication factor (2 or
more) and remove the capability by hand where it is lower. An HDInsight cluster being deleted or
that failed (`clusterState` `Deleting`, `Error` or `DeletionFailed`, in any casing) is left out, as
it is nothing to move.

A Service Bus topic (where publishers write) and each of its subscriptions (where consumers read, a
queue of their own with a dead-letter subqueue) are both listed, as both have to move. A subscription
is named `<topic>/<subscription>`, as the same subscription name often recurs across topics. An SMB
share, a Cosmos DB account of another kind than `MongoDB`, a Logic App and a MariaDB server stay
`unclassified`, so the report shows them as blockers. Azure Database for MariaDB was retired on 19
September 2025, and Basic Load Balancer on 30 September 2025; a Basic load balancer still running is
classified, with no capability. Azure Cache for Redis Basic, Standard and Premium retire on 30
September 2028, its Enterprise and Enterprise Flash tiers on 31 March 2027 (Microsoft Learn,
*Frequently asked questions on the retirement of Azure Cache for Redis*). A cluster in those legacy
tiers (an `Enterprise_*` or `EnterpriseFlash_*` SKU) is always `node-high-availability`, with or
without `zones`: it runs on a Redis Enterprise cluster of three nodes by default (always an odd
number) that never puts a partition's primary and replica on the same node (Microsoft Learn, *High
availability for Azure Cache for Redis*, Enterprise and Enterprise Flash tiers); two or more `zones`
decide only its `zone-high-availability`. The `zones` rule holds only for those legacy tiers: an Azure
Managed Redis cluster reads its `highAvailability` and `redundancyMode`, whatever its `zones`. Redis
modules of an Azure Managed Redis cluster are no capability.

Left out: virtual machines of a scale set, their OS disks (`storageProfile.osDisk`) and their data
disks created with `deleteOption: Delete` (the scale set stands for them), snapshots and autoscale
settings, which become capabilities of their disk or scale set, a Service Bus namespace whose queues,
topics or subscriptions are listed (they stand for it; a namespace with none stays `unclassified`), a
FileStorage account whose shares are listed (they stand for it), share snapshots, which become a
capability of their share, the databases of an Azure Managed Redis cluster (the cluster stands for
them), and everything in an AKS cluster's node resource group (`properties.nodeResourceGroup`, usually
`MC_<group>_<cluster>_<region>`): the node pools' scale sets, load balancers, public IP addresses
and network interfaces, which AKS manages and the cluster stands for, and the FileStorage accounts
the Azure Files CSI driver creates there. A managed disk in that group is a persistent volume that
holds data, and stays; so does a share of such a FileStorage account, a persistent volume too.

| Category | Capability | When |
|---|---|---|
| `compute.vm` (virtual machine) | `ipv6` | a network interface has an IPv6 IP configuration |
| `compute.vm` (virtual machine) | `snapshots` | one of its managed disks has a snapshot |
| `compute.vm` (scale set) | `autoscaling` | an enabled autoscale setting targets it |
| `compute.vm` (scale set) | `zone-high-availability` | it spans two or more zones |
| `storage.block` | `encryption-at-rest` | always: Azure encrypts every managed disk |
| `storage.block` | `snapshots` | a snapshot was taken from the disk |
| `storage.object` | `encryption-at-rest` | always: Azure encrypts every storage account |
| `storage.object` | `object-versioning` | blob versioning is enabled |
| `database.*` | `encryption-at-rest` | always |
| `database.*` | `point-in-time-restore` | `backupRetentionDays` is above 0 |
| `database.*` | `zone-high-availability` | high availability mode `ZoneRedundant` |
| `database.*` | `node-high-availability` | high availability mode `SameZone` |
| `database.*` | `read-replicas` | another server names it as its source |
| `kubernetes.managed` | `autoscaling` | a node pool has autoscaling on |
| `kubernetes.managed` | `zone-high-availability` | a node pool spans two or more zones |
| `kubernetes.managed` | `node-high-availability` | its node pools run two or more nodes in all |
| `messaging.queue` (Service Bus) | `dead-letter-queue` | always: every Service Bus queue has a dead-letter subqueue |
| `messaging.queue` (topic subscription) | `dead-letter-queue` | always: every subscription has a dead-letter subqueue |
| `messaging.queue` (storage queue) | `encryption-at-rest` | always: Azure encrypts every storage account |
| `messaging.topic` | `encryption-at-rest` | always: topics exist only in the Standard and Premium tiers, which encrypt all data at rest |
| `cache.redis` (Azure Cache for Redis) | `node-high-availability` | tier Standard or Premium (a primary and a replica) |
| `cache.redis` (Azure Cache for Redis) | `zone-high-availability` | tier Standard or Premium, and two or more `zones` or `zonalAllocationPolicy` `Automatic` |
| `cache.redis` (Azure Cache for Redis) | `sharding` | tier Premium with `shardCount` 1 or more (OSS clustering) |
| `cache.redis` (Azure Cache for Redis) | `snapshots` | tier Premium with `rdb-backup-enabled` `true` (RDB persistence; AOF alone is no snapshot) |
| `cache.redis` (Azure Cache for Redis) | `encryption-at-rest` | any SKU but C0 and C1, whose disks Azure does not encrypt |
| `cache.redis` (Azure Managed Redis) | `encryption-at-rest` | always |
| `cache.redis` (Azure Managed Redis) | `node-high-availability` | `highAvailability` `Enabled`, `redundancyMode` `LR` or `ZR`, or a legacy Enterprise or Enterprise Flash cluster (always, `zones` or not) |
| `cache.redis` (Azure Managed Redis) | `zone-high-availability` | `redundancyMode` `ZR`, or two or more `zones` (a legacy Enterprise or Enterprise Flash cluster only) |
| `cache.redis` (Azure Managed Redis) | `sharding` | a database's `clusteringPolicy` is `OSSCluster` |
| `cache.redis` (Azure Managed Redis) | `snapshots` | a database has RDB persistence (`persistence.rdbEnabled`) |
| `database.mongodb` (Cosmos DB) | `encryption-at-rest` | always |
| `database.mongodb` (Cosmos DB) | `node-high-availability` | always: each region keeps a quorum of replicas |
| `database.mongodb` (Cosmos DB) | `point-in-time-restore` | `backupPolicy.type` `Continuous` |
| `database.mongodb` (Cosmos DB) | `zone-high-availability` | a location has `isZoneRedundant` true |
| `database.mongodb` (Cosmos DB) | `read-replicas` | two or more locations |
| `database.mongodb` (DocumentDB) | `encryption-at-rest`, `point-in-time-restore` | always |
| `database.mongodb` (DocumentDB) | `node-high-availability` | `highAvailability.targetMode` is not `Disabled` (`SameZone` or `ZoneRedundantPreferred`) |
| `database.mongodb` (DocumentDB) | `zone-high-availability` | `highAvailability.targetMode` is `ZoneRedundantPreferred` (a `SameZone` standby shares its primary's zone) |
| `database.mongodb` (DocumentDB) | `read-replicas` | another cluster names it in `replicaParameters.sourceResourceId` |
| `network.loadbalancer` (load balancer) | `zone-high-availability` | SKU `Standard`, and a frontend IP configuration, or its public IP address, in two or more zones |
| `network.loadbalancer` (load balancer) | `udp` | a load-balancing rule's protocol is `Udp`, `All` (HA ports) or `Quic` (carried over UDP) |
| `network.loadbalancer` (load balancer) | `ipv6` | a load-balancing rule leads from an IPv6 frontend to a backend pool with an IPv6 member |
| `network.loadbalancer` (Application Gateway) | `zone-high-availability` | it spans two or more zones |
| `registry.container` | `encryption-at-rest` | always: Azure encrypts every image in a registry |
| `storage.file` | `encryption-at-rest` | always: Azure Files encrypts every share |
| `storage.file` (share of a FileStorage account) | `zone-high-availability` | the account's SKU is zone redundant (`_ZRS`) |
| `storage.file` (share of a FileStorage account) | `snapshots` | the share has a snapshot |
| `storage.file` (`Microsoft.FileShares`) | `zone-high-availability` | `redundancy` `Zone` |
| `messaging.kafka` (HDInsight) | `encryption-at-rest` | always: Azure encrypts the cluster's managed disks |
| `messaging.kafka` (HDInsight) | `node-high-availability` | the worker role's `targetInstanceCount` is 3 or more |
| `messaging.kafka` (HDInsight) | `zone-high-availability` | the cluster has `zones` set |

A frontend is IPv6 when its `privateIPAddressVersion` or its public IP address's
`publicIPAddressVersion` is `IPv6`. A backend pool member is IPv6 when the IP configuration it names
(of a network interface, or of a scale set's network profile) is `IPv6`, or its address is an IPv6
address. An IPv6 frontend over IPv4 backends is no `ipv6`: the backends would have to move to IPv6
too. A frontend's public IP address without `zones` is not counted as zone redundant, although Azure
now makes a Standard one zone redundant in a region with zones: the program cannot see whether a
region has zones, so it understates rather than overstates. A DocumentDB cluster with `SameZone` high
availability gets `node-high-availability` but no `zone-high-availability`: its standby protects
against a node failure, not a zone outage (Microsoft Learn, *High availability in Azure DocumentDB*).
A `ZoneRedundantPreferred` cluster in a region without zones falls back to the same zone, and is still
credited `zone-high-availability` it does not have, as the program cannot see the region's zones:
remove it by hand there. An Application Gateway is never `ipv6` (it supports no IPv6 backends) and
never `udp` (it balances HTTP). A `Microsoft.FileShares` share's snapshots are not listed.

## Trim what you do not want assessed

The full listings include supporting resources, such as networks, network interfaces, security
groups and IAM roles, and each becomes an `unclassified` blocker. To drop the unclassified resources
of a type, filter the inventory with `jq`; for example, every Azure networking resource:

```bash
jq 'del(.resources[] | select(.category == "unclassified" and (.rawType | startswith("Microsoft.Network/"))))' \
  inventories/azure.inventory.json > inventories/azure-trimmed.inventory.json
```

Edit the file by hand as well: it is plain JSON. Leave out only what really has nothing to move.

## Privacy

An inventory holds the names and IDs of resources in a real account. The programs keep it as small as
they can (short IDs, no account, project or subscription field), but names can still say a lot about
a business.

The collected directory under `inventories/<source>/` says far more. It is the providers' raw output:
even trimmed to the fields the programs read, it holds full ARNs and resource IDs with the account,
project or subscription, and it can hold IP addresses, tag keys, labels and settings, and, if you
drop a `--query` or `--format` projection, secrets such as function environment variables or startup
scripts.

- Share only the reviewed inventory file, never the collected directory.
- Delete the collected directory right after the build (`rm -r inventories/<source>`).
- Keep short IDs. Do not replace them with ARNs, full GCP resource names or Azure resource IDs, which
  carry the account, project or subscription ID.
- Review the file before you share it, and rename anything that should not leave your organisation.
- `shift2eu assess` runs offline: it reads the file and writes a report, and sends nothing anywhere.
- Never commit an inventory or the raw CLI output from a real account to a repository. In a clone of
  this one, `inventories/` and `*.inventory.json` are git-ignored; its fixtures are sanitised by rule.

## Validate the result

Assess the inventory against every target:

```bash
shift2eu assess --inventory inventories/aws.inventory.json --target all
```

`assess` refuses a file it cannot read exactly, with exit code 1 and one line that names the file
and the field by its path, for example:

```text
shift2eu: load inventory: inventory file "inventories/aws.inventory.json": not a valid inventory document: unknown key "regoin" at "resources[0].regoin", line 1, column 82
shift2eu: load inventory: inventory file "inventories/aws.inventory.json": resource "i-0123456789abcdef0": unknown taxonomy category "compute.mv"
shift2eu: load inventory: inventory file "inventories/aws.inventory.json": resources[1]: duplicate resource ID "a"
shift2eu: load inventory: inventory file "inventories/aws.inventory.json": "resources[3].relations[0].target": relation target is no resource of the inventory: "shop-web-9"
```

It also refuses a file over 10 MiB, more than 50,000 resources and more than 32 capabilities on one
resource, and more than 32 relations on one resource; split a larger estate into several inventories,
by region or project. After a hand edit, check the category and capability names against the lists
above, and that every relation's `target` is the `id` of another resource in the file. [Assess an inventory](assess.md)
describes the report and the exit codes, and [Try it](try-it.md#read-the-report) how to read it.
