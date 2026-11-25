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
| `resources[].usage` | no | `hoursPerMonth`, the hours a month the resource runs: 730 when absent, at most 744. Only for compute, Kubernetes, database and `cache.redis` categories; on any other category it is an error |
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
[ADR 0046](../adr/0046-add-seven-taxonomy-categories-and-match-cache-plans-on-memory.md)). A
resource that fits none of these categories is `unclassified` and keeps its `rawType`: it then shows
up in the report as a blocker on every target, instead of disappearing from the assessment. The
programs below do not classify into the seven categories ADR 0046 adds yet (they list those resources
as `unclassified`, or not at all): until they do, set such a resource's category by hand.

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
| `storage.block` | no | yes | volumes | GiB |
| `storage.object` | no | stored GiB | no | GiB |
| `storage.file` | no | provisioned GiB | no | GiB |
| `serverless.function` | `memoryGiB` only | no | no | not estimated (priced by usage) |
| `messaging.queue`, `messaging.topic`, `serverless.container` | no | no | no | not estimated (priced by usage) |
| `network.loadbalancer`, `registry.container` | no | no | no | not estimated (not priced) |
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
  database (`database.postgresql`, `database.mysql`, `database.mongodb`) and cache (`cache.redis`)
  resources take it; on any other category the inventory is refused.
- any size the calls do not give, such as an RDS instance's vCPU and memory (its instance class), a
  Cloud SQL instance's unless its tier is `db-custom-*`, the `count` of a high-availability database
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

The commands are written for a POSIX shell (bash, zsh).

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
        "config:SelectResourceConfig"
      ],
      "Resource": "*"
    }
  ]
}
```

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

# The vCPU and memory of the instance types in use.
jq -r '[.Reservations[].Instances[].InstanceType] | unique[]' "$out/ec2-instances.json" |
  xargs -r -n 100 aws ec2 describe-instance-types --output json --query '{InstanceTypes: InstanceTypes[].{
    InstanceType: InstanceType, VCpuInfo: {DefaultVCpus: VCpuInfo.DefaultVCpus},
    MemoryInfo: {SizeInMiB: MemoryInfo.SizeInMiB}}}' --instance-types > "$out/ec2-instance-types.json"

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
  BackupRetentionPeriod: BackupRetentionPeriod, MultiAZ: MultiAZ, StorageEncrypted: StorageEncrypted,
  ReadReplicaDBInstanceIdentifiers: ReadReplicaDBInstanceIdentifiers}}' > "$out/rds-instances.json"

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

# Optional: everything else, from AWS Config (see "Everything else on AWS").
aws configservice select-resource-config \
  --expression "SELECT resourceId, resourceName, resourceType, awsRegion" \
  --output json > "$out/config.json"
```

Every list and describe call above, `select-resource-config` included, is paginated by AWS, and the
AWS CLI version 2 fetches every page and merges them before it applies `--query`, so each file holds
the whole region. Do not pass `--no-paginate` or `--max-items`.

`describe-instance-types` takes at most 100 instance types per call; `xargs -n 100` splits a longer list
into several calls, and the program reads each.

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
| `AWS::RDS::DBInstance`, any other engine (MariaDB, Oracle, SQL Server) | `unclassified` | `rds describe-db-instances` |
| `AWS::RDS::DBCluster` (Aurora) | none: its instances stand for it | `configservice select-resource-config` |
| `AWS::EKS::Cluster` | `kubernetes.managed` | `eks describe-cluster` |
| `AWS::Lambda::Function` | `serverless.function` | `lambda list-functions` |
| `AWS::SQS::Queue` | `messaging.queue` | `sqs get-queue-attributes` |
| any other type AWS Config records | `unclassified` | `configservice select-resource-config` |

Left out:

- instances that are terminated or shutting down, and instances an Auto Scaling group launched (tag
  `aws:autoscaling:groupName`): the group stands for them. Their volumes that are deleted with them
  (`DeleteOnTermination`), such as their root volumes, go with them;
- the nodes of an EKS cluster, which the cluster stands for: the Auto Scaling group of a managed node
  group (`describe-nodegroup` names it), instances and Auto Scaling groups tagged `eks:cluster-name`
  or `kubernetes.io/cluster/<name>`, and a node's volumes that are deleted with it
  (`DeleteOnTermination`). A volume that outlives its node, such as a persistent volume, stays;
- an Aurora cluster (`AWS::RDS::DBCluster`, which AWS Config lists): Aurora is assessed per instance,
  and each instance of the cluster is a `database.postgresql` or `database.mysql` resource. Its
  capabilities come from the instance's settings, and Aurora keeps replicas and zone redundancy at the
  cluster level, as more instances in other zones, which the instance does not report: add
  `read-replicas` or `zone-high-availability` by hand when the cluster has them.

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
| `database.*` | `point-in-time-restore` | `BackupRetentionPeriod` is above 0 |
| `database.*` | `read-replicas` | it has read replicas |
| `database.*` | `zone-high-availability` | `MultiAZ` is true |
| `database.*` | `encryption-at-rest` | `StorageEncrypted` is true |
| `kubernetes.managed` | `autoscaling` | a managed node group's `maxSize` is above its `minSize` |
| `kubernetes.managed` | `node-high-availability` | its managed node groups want two or more nodes in all |
| `messaging.queue` | `dead-letter-queue` | it has a `RedrivePolicy` |
| `messaging.queue` | `encryption-at-rest` | SQS-managed or KMS server-side encryption is on |

### Everything else on AWS

The service calls above cover what the taxonomy classifies. Everything else (CloudFront
distributions, Route 53 zones, load balancers, and also networking and IAM resources) should show up
as `unclassified`, so the report shows what has no place to go. Two AWS services list everything:

- **AWS Config** (`configservice select-resource-config`, an SQL-like read-only query). Its
  `resourceType` is the CloudFormation type, the same convention as the table above, so the program
  reads it: every type the table does not cover becomes `unclassified`, and the types it does cover
  are dropped, as the service calls already list them. Config only knows what its recorder records,
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
gcloud asset search-all-resources --scope="projects/$project" \
  --format='json(name,assetType,location,labels)' |
  jq '[.[] | if .labels then .labels |= with_entries(select(.key == "goog-gke-node")) else . end]' \
  > "$out/assets.json"

# Whether the asset listing holds an asset type, in a region when one is given:
# the Memorystore, Filestore and Artifact Registry lists below run only then.
has() { jq -e --arg t "$1" --arg r "${2:-}" \
  'any(.[]; .assetType == $t and ($r == "" or (.name | contains("/locations/" + $r + "/"))))' \
  "$out/assets.json" > /dev/null; }

gcloud compute instances list --project="$project" \
  --format='json(kind,selfLink,machineType,metadata.items[].key,disks[].source,disks[].boot,disks[].autoDelete,networkInterfaces[].stackType,networkInterfaces[].ipv6AccessConfigs[].type)' \
  > "$out/compute-instances.json"
# The vCPU and memory of the machine types in use, and every disk's size.
types="$(jq -r '[.[].machineType // empty | split("/") | last] | unique | join(" ")' "$out/compute-instances.json")"
# With no instances there are no types to ask for: write an empty list instead.
if [ -n "$types" ]; then
  gcloud compute machine-types list --project="$project" --filter="name:($types)" \
    --format='json(kind,selfLink,name,zone,guestCpus,memoryMb)' > "$out/compute-machine-types.json"
else
  echo '[]' > "$out/compute-machine-types.json"
fi

gcloud compute disks list --project="$project" \
  --format='json(kind,name,zone,region,sizeGb)' > "$out/compute-disks.json"
gcloud compute instance-groups managed list --project="$project" \
  --format='json(kind,selfLink,status.autoscaler)' > "$out/compute-instance-groups.json"
gcloud compute snapshots list --project="$project" \
  --format='json(kind,sourceDisk)' > "$out/compute-snapshots.json"
gcloud sql instances list --project="$project" \
  --format='json(kind,selfLink,region,databaseVersion,replicaNames,settings.availabilityType,settings.tier,settings.dataDiskSizeGb,settings.backupConfiguration.pointInTimeRecoveryEnabled,settings.backupConfiguration.binaryLogEnabled)' \
  > "$out/sql-instances.json"
gcloud storage buckets list --project="$project" --raw \
  --format='json(kind,name,versioning)' > "$out/storage-buckets.json"
gcloud container clusters list --project="$project" \
  --format='json(selfLink,location,currentNodeCount,autopilot.enabled,nodePools[].autoscaling.enabled,nodePools[].instanceGroupUrls)' \
  > "$out/container-clusters.json"
gcloud pubsub subscriptions list --project="$project" \
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
fi
if has artifactregistry.googleapis.com/Repository; then
  gcloud artifacts repositories list --project="$project" --location=all --quiet \
    --format='json(name,format,mode)' > "$out/artifacts-repositories.json"
fi

# The Cloud Run service each 2nd gen function runs on, per region with functions.
for region in $(jq -r '[.[] | select(.assetType == "cloudfunctions.googleapis.com/Function") | .name | capture("/locations/(?<r>[^/]+)/").r] | unique | .[]' "$out/assets.json"); do
  gcloud functions list --project="$project" --v2 --regions="$region" --quiet \
    --format='json(name,serviceConfig.service)'
done > "$out/functions.json"
```

The Memorystore, Filestore, Artifact Registry and Cloud Functions lists run only when the asset
listing holds a resource of theirs, so a project that does not use a service never calls its API,
which may not be enabled. `--quiet` makes `gcloud` take the default answer, No, when it offers to
enable an API; if a call still asks to enable one, answer N: enabling an API writes to the project.

The `--format` projections keep only what the program reads: an instance's metadata keys, for
example, but not their values, which hold startup scripts and SSH keys, and of its disks only the
source, whether it boots and whether it is deleted with the instance (`autoDelete`). Of the asset
labels, `jq` keeps only `goog-gke-node`, the one the program reads; the Cloud Run service behind
each function comes from `functions list`, not from a label. Of a URL map, only the links to its
backend services are kept (its default, path and route services, the weighted and mirrored backends
of its route actions, and the error services of its custom error response policies), not its route
actions' header values.

The load balancer lists read only the HTTP and HTTPS target proxies, the frontends of an Application
Load Balancer; a TCP, SSL or gRPC proxy is not listed, so a load balancer behind one gets no `ipv6`.
The Memorystore lists run once per region the asset listing names: the gcloud pages document no
"all regions" value for them. Every call above lists or describes; none changes anything.

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

The Memorystore and Filestore APIs write GB; Filestore defines a GB as 1024³ bytes, and the program
takes every one of these GB as GiB. A Redis cluster names only the memory of the whole cluster
(`sizeGb`), and no page says whether it counts the replicas, so the program writes no `memoryGiB` for
it: add the memory of one node by hand (its `nodeType`, from Google's node specification) for an
estimate.

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
| `memcache.googleapis.com/Instance` (Memorystore for Memcached) | `unclassified` |
| any other asset type | `unclassified` |

A Pub/Sub topic (where publishers write, and every subscription gets its own copy of each message) and
a subscription (where consumers read) are both listed, as both have to move. Memcached speaks another
protocol than Redis, so it stays `unclassified`.

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
| `cache.redis` (Redis instance) | `zone-high-availability` | tier `STANDARD_HA`, and its `nodes` are in two or more zones (or `alternativeLocationId` is another zone than `locationId`) |
| `cache.redis` (Redis instance) | `snapshots` | `persistenceConfig.persistenceMode` is `RDB` |
| `cache.redis` (Redis cluster, Valkey) | `node-high-availability` | `replicaCount` is 1 or more |
| `cache.redis` (Redis cluster, Valkey) | `zone-high-availability` | `replicaCount` is 1 or more and `zoneDistributionConfig.mode` is not `SINGLE_ZONE` (absent means `MULTI_ZONE`) |
| `cache.redis` (Redis cluster, Valkey) | `sharding` | always for a Redis cluster; a Valkey instance of `mode` `CLUSTER` |
| `cache.redis` (Redis cluster, Valkey) | `snapshots` | `persistenceConfig.mode` is `RDB` (an AOF log is not a snapshot) |
| `network.loadbalancer` | `zone-high-availability` | always: every GCP load balancer is regional or global, and a global one counts only for its zone redundancy within a region |
| `network.loadbalancer` | `udp` | one of its backend services has `protocol` `UDP`, or a forwarding rule of `IPProtocol` `UDP` reaches it |
| `network.loadbalancer` | `ipv6` | a forwarding rule of `ipVersion` `IPV6` reaches it (directly, or through a target HTTP or HTTPS proxy) **and** one of its backend services has `ipAddressSelectionPolicy` `IPV6_ONLY` or `PREFER_IPV6`; an IPv6 frontend over IPv4 backends gets none, nor does a target pool |
| `registry.container` | `encryption-at-rest` | always |
| `storage.file` | `encryption-at-rest` | always |
| `storage.file` | `zone-high-availability` | tier `REGIONAL` or `ENTERPRISE` |
| `storage.file` | `snapshots` | a Filestore snapshot of the instance is in the asset listing |

A forwarding rule of `IPProtocol` `L3_DEFAULT` forwards every protocol, but no page read says so in
as many words, so it does not count as UDP: add `udp` by hand for such a load balancer that carries
UDP.

## Azure

### Azure permissions

The built-in `Reader` role on the subscription. It covers Resource Graph and the management-plane
reads below (Service Bus queues and topics, storage queues, blob service properties); none of the
commands reads a storage account key or any data.

### Collect from Azure

Resource Graph returns at most 1,000 rows per call, so the loop below fetches page after page until
no skip token is left. It reads the subscription the CLI is set to (`az account show`):

```bash
subscription="$(az account show --query id --output tsv)"
out=inventories/azure
mkdir -p "$out"

# Keep only the properties azure.jq reads, and only for the types it reads them from.
query="Resources
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
    dynamic(null))
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

# The vCPU and memory of every VM size, in each region with virtual machines or scale sets.
jq -r '[.data[] | select(.type == "microsoft.compute/virtualmachines"
    or .type == "microsoft.compute/virtualmachinescalesets") | .location] | unique[]' "$out"/graph-*.json |
  while read -r location; do
    az vm list-sizes --location "$location" --output json
  done > "$out/vm-sizes.json"
```

The query keeps no admin user names, SSH keys, server logins or host names. It keeps whole arrays
where the program reads one field of each element (a virtual machine's data disks and network
interfaces, a network interface's IP configurations with their private IP addresses, an AKS
cluster's node pools with their labels, tags (keys and values), taints, subnet IDs and node image
versions), as Resource Graph cannot trim inside an array.

Resource Graph holds most settings; Service Bus queues and topics, storage queues, a storage
account's blob versioning and the VM sizes are not in it, hence the four loops. Storage queues are listed through the
Resource Manager API (`az rest --method get`, a read), not the data plane, so `Reader` is enough and
no account key is needed. A Basic tier Service Bus namespace has no topics, and its `topic list`
fails: that error is expected. An account with more queues than one response returns adds a
`nextLink` to the response; run that call without `--query` and fetch the `nextLink` URL with
`az rest --method get --url` as well.

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

Not filled in: a flexible server's vCPU and memory (its `sku.name`, such as `Standard_D2ds_v4`), the
count of a flexible server with high availability (`ZoneRedundant` or `SameZone`), an AKS cluster's
per-node vCPU and memory (its pools' `vmSize`), and a storage account's stored GiB. Add them by hand
where you want an estimate. A server with high availability has a standby, and how many nodes a standby
counts as depends on how a target prices high availability: the program does not guess, so the estimate
says "(count assumed 1)" until you set the `count` (see [Sizes](#sizes) for the value).

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
| `Microsoft.Storage/storageAccounts`, kind `FileStorage` | `unclassified` |
| `Microsoft.DBforPostgreSQL/flexibleServers` | `database.postgresql` |
| `Microsoft.DBforMySQL/flexibleServers` | `database.mysql` |
| `Microsoft.ContainerService/managedClusters` | `kubernetes.managed` |
| `Microsoft.Web/sites`, kind `functionapp` | `serverless.function` |
| `Microsoft.Web/sites`, any other kind (web app, Logic App) | `unclassified` |
| `Microsoft.ServiceBus/namespaces/queues` | `messaging.queue` |
| `Microsoft.ServiceBus/namespaces/topics` | `unclassified` |
| `Microsoft.Storage/storageAccounts/queueServices/queues` | `messaging.queue` |
| any other type | `unclassified` |

A Service Bus topic, where every subscription gets its own copy of a message, is publish and
subscribe, which no taxonomy category covers: each topic is listed as `unclassified` under its own
name, so the report shows it as a blocker.

Left out: virtual machines of a scale set, their OS disks (`storageProfile.osDisk`) and their data
disks created with `deleteOption: Delete` (the scale set stands for them), snapshots and autoscale
settings, which become capabilities of their disk or scale set, a Service Bus namespace whose queues
or topics are listed (they stand for it; a namespace with neither stays `unclassified`), and
everything in an AKS cluster's node resource group (`properties.nodeResourceGroup`, usually
`MC_<group>_<cluster>_<region>`): the node pools' scale sets, load balancers, public IP addresses
and network interfaces, which AKS manages and the cluster stands for. A managed disk in that group
is a persistent volume that holds data, and stays.

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
| `messaging.queue` (storage queue) | `encryption-at-rest` | always: Azure encrypts every storage account |

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
