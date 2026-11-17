# aws.jq turns the output of the read-only aws CLI calls in docs/guide/inventory.md
# into a shift2eu inventory file, schema version 2
# (docs/adr/0025-json-inventory-file-format-v2.md). Run it with -n on every file
# the guide's commands wrote, from one region or several:
#
#   jq -n -f docs/guide/inventory/aws.jq inventories/aws/*/*.json > inventories/aws.inventory.json
#
# It recognises each input by its shape, not by its file name, and a file may hold
# several JSON values one after the other. The guide's --query projections keep
# only the fields read here; the full, unprojected output is read the same way:
#
#   {"Reservations": ...}          ec2 describe-instances
#   {"InstanceTypes": ...}         ec2 describe-instance-types
#   {"Volumes": ...}               ec2 describe-volumes
#   {"Snapshots": ...}             ec2 describe-snapshots --owner-ids self
#   {"AutoScalingGroups": ...}     autoscaling describe-auto-scaling-groups
#   {"Buckets": ...}               s3api list-buckets
#   {"Bucket": ..., "Versioning"}  s3api get-bucket-versioning, wrapped by the guide
#   {"DBInstances": ...}           rds describe-db-instances
#   {"cluster": ...}               eks describe-cluster
#   {"nodegroup": ...}             eks describe-nodegroup
#   {"Functions": ...}             lambda list-functions
#   {"Attributes": {"QueueArn"}}   sqs get-queue-attributes
#   {"Results": [...]}             configservice select-resource-config (optional;
#                                  one value per page if you page by hand)
#
# Resource IDs are short (an instance ID, a bucket or queue name), never an ARN,
# which would carry the account ID. Anything it cannot classify is "unclassified"
# with its rawType (a CloudFormation type), so it shows up as a blocker.
#
# The nodes of an EKS cluster are left out, the cluster stands for them: the Auto
# Scaling groups of its managed node groups, and instances and Auto Scaling groups
# tagged eks:cluster-name or kubernetes.io/cluster/<name>. An instance an Auto
# Scaling group launched is left out too, the group stands for it. The volumes
# deleted with an instance left out go with it (a persistent volume, which
# outlives its instance, stays).
#
# The size comes from the calls too: an instance's vCPU and memory from its
# instance type (ec2 describe-instance-types), a volume's GiB (Size), a DB
# instance's allocated storage (AllocatedStorage; none for an instance in a DB
# cluster, Aurora or an RDS Multi-AZ DB cluster, whose storage belongs to the
# cluster) and an EKS cluster's node count (the desired sizes of its managed
# node groups). A number no call gives is left out, never guessed. The workload, relations and monthlyCost fields are the
# user's to add by hand; this program never writes them.

# The CloudFormation types the service calls above cover. AWS Config lists them
# too; those entries are dropped, so a resource is not listed twice. An Aurora
# cluster (AWS::RDS::DBCluster) is covered by its instances, which
# rds describe-db-instances lists.
def covered_types:
  [
    "AWS::EC2::Instance", "AWS::EC2::Volume", "AWS::AutoScaling::AutoScalingGroup",
    "AWS::S3::Bucket", "AWS::RDS::DBInstance", "AWS::RDS::DBCluster", "AWS::EKS::Cluster",
    "AWS::Lambda::Function", "AWS::SQS::Queue"
  ];

# True for an instance or an Auto Scaling group that is a Kubernetes node, by its
# tag keys.
def is_node:
  any(.Tags[]?.Key // empty; . == "eks:cluster-name" or startswith("kubernetes.io/cluster/"));

# The region of an availability zone: eu-central-1a -> eu-central-1.
def zone_region: if . == null or . == "" then null else sub("[a-z]$"; "") end;

# The region field of an ARN: arn:aws:sqs:eu-central-1:123456789012:q -> eu-central-1.
def arn_region: if . == null then null else split(":")[3] end;

# A short ID: an ARN loses its partition, service, region and account and keeps
# the last element of its resource part.
def short_id:
  if startswith("arn:") then split(":")[5:] | join(":") | split("/") | last else . end;

# The capability $name when the condition is true.
def cap($name; cond): if cond then $name else empty end;

# Gives every resource a distinct ID: two resources with the same short ID get
# their rawType and region appended, and any still equal a counter.
def unique_ids:
  def suffix: " (" + (.rawType // "unknown") + (if .region then ", " + .region else "" end) + ")";
  group_by(.id)
  | map(if length == 1 then . else map(.id += suffix) end)
  | add // []
  | group_by(.id)
  | map(if length == 1 then . else to_entries | map(.value.id += " #" + (.key + 1 | tostring) | .value) end)
  | add // [];

# A size object without the fields no call gave; null when none is left.
def size_of(fields):
  fields | with_entries(select(.value != null)) | if . == {} then null else . end;

# Writes the inventory document: no empty region or capabilities, capabilities
# sorted, identical resources (from two regions' global calls) listed once.
def inventory($source):
  map(
    {id, rawType, category, region, capabilities, size}
    | if .region == null or .region == "" then del(.region) else . end
    | if .size == null then del(.size) else . end
    | .capabilities = ((.capabilities // []) | unique)
    | if .capabilities == [] then del(.capabilities) else . end
  )
  | unique
  | unique_ids
  | sort_by(.id)
  | {schemaVersion: 2, source: $source, resources: .};

[inputs | objects] as $docs
| ([$docs[] | .Snapshots? // empty | .[] | .VolumeId] | unique) as $snapshotted
| ([$docs[] | select(.Bucket? and has("Versioning")) | select(.Versioning.Status? == "Enabled") | .Bucket] | unique) as $versioned
| ([$docs[] | .InstanceTypes? // empty | .[]
    | {key: .InstanceType, value: {vcpus: .VCpuInfo.DefaultVCpus, memoryGiB: (.MemoryInfo.SizeInMiB / 1024)}}]
   | from_entries) as $types
| [$docs[] | .nodegroup? // empty] as $nodegroups
| [$docs[] | .Reservations? // empty | .[].Instances[]?
   | select(is_node or ([.Tags[]?.Key] | index("aws:autoscaling:groupName")))
   | .InstanceId] as $leftout
| ([$nodegroups[] | .resources.autoScalingGroups[]?.name]
   + [$docs[] | .AutoScalingGroups? // empty | .[] | select(is_node) | .AutoScalingGroupName]
   | unique) as $nodegroupasgs
| [
    # EC2 instances. One launched by an Auto Scaling group is left out: the group
    # stands for it. A Kubernetes node is left out: its cluster stands for it.
    # An instance of a bare-metal instance type (*.metal, or a sized
    # *.metal-* such as c7i.metal-24xl) is a dedicated physical server,
    # compute.baremetal (ADR 0023); any other is compute.vm.
    ($docs[] | .Reservations? // empty | .[].Instances[]?
      | select(.State.Name != "terminated" and .State.Name != "shutting-down")
      | select([.Tags[]?.Key] | index("aws:autoscaling:groupName") | not)
      | select(is_node | not)
      | {
          id: .InstanceId,
          rawType: "AWS::EC2::Instance",
          category: (if (.InstanceType // "") | test("\\.metal(-|$)") then "compute.baremetal" else "compute.vm" end),
          region: (.Placement.AvailabilityZone | zone_region),
          capabilities: [
            cap("ipv6"; any(.NetworkInterfaces[]?;
              ((.Ipv6Addresses // []) | length > 0) or (.Ipv6AddressCount // 0) > 0)),
            cap("snapshots"; any(.BlockDeviceMappings[]?; .Ebs.VolumeId | IN($snapshotted[])))
          ],
          size: size_of($types[.InstanceType // ""] // {})
        }),

    # EBS volumes. A volume deleted with an instance left out (a Kubernetes
    # node or an Auto Scaling group member), such as its root volume, is left
    # out with it.
    ($docs[] | .Volumes? // empty | .[]
      | select(any(.Attachments[]?; .DeleteOnTermination == true and (.InstanceId | IN($leftout[]))) | not)
      | {
          id: .VolumeId,
          rawType: "AWS::EC2::Volume",
          category: "storage.block",
          region: (.AvailabilityZone | zone_region),
          capabilities: [
            cap("encryption-at-rest"; .Encrypted == true),
            cap("snapshots"; .VolumeId | IN($snapshotted[]))
          ],
          size: size_of({storageGiB: .Size})
        }),

    # Auto Scaling groups. The group of a managed node group, or one tagged as a
    # Kubernetes node group, is left out: its cluster stands for it.
    ($docs[] | .AutoScalingGroups? // empty | .[]
      | select(.AutoScalingGroupName | IN($nodegroupasgs[]) | not)
      | {
          id: .AutoScalingGroupName,
          rawType: "AWS::AutoScaling::AutoScalingGroup",
          category: "compute.vm",
          region: (.AutoScalingGroupARN | arn_region),
          capabilities: [
            cap("autoscaling"; .MaxSize > .MinSize),
            cap("zone-high-availability"; (.AvailabilityZones // []) | length >= 2)
          ]
        }),

    # S3 buckets. S3 encrypts every object at rest (SSE-S3 by default since 2023).
    ($docs[] | .Buckets? // empty | .[]
      | {
          id: .Name,
          rawType: "AWS::S3::Bucket",
          category: "storage.object",
          region: .BucketRegion,
          capabilities: [
            "encryption-at-rest",
            cap("object-versioning"; .Name | IN($versioned[]))
          ]
        }),

    # RDS DB instances: the engine decides the category; any other engine is
    # unclassified. An Aurora cluster is assessed per instance. An instance in
    # a DB cluster (DBClusterIdentifier set: Aurora, or an RDS Multi-AZ DB
    # cluster) gets no storage size: its storage belongs to the cluster, and
    # AllocatedStorage does not describe it.
    ($docs[] | .DBInstances? // empty | .[]
      | (if .Engine == "postgres" or .Engine == "aurora-postgresql" then "database.postgresql"
         elif .Engine == "mysql" or .Engine == "aurora-mysql" then "database.mysql"
         else "unclassified" end) as $category
      | {
          id: .DBInstanceIdentifier,
          rawType: "AWS::RDS::DBInstance",
          category: $category,
          region: (.DBInstanceArn | arn_region),
          capabilities: (if $category == "unclassified" then [] else [
            cap("point-in-time-restore"; (.BackupRetentionPeriod // 0) > 0),
            cap("read-replicas"; (.ReadReplicaDBInstanceIdentifiers // []) | length > 0),
            cap("zone-high-availability"; .MultiAZ == true),
            cap("encryption-at-rest"; .StorageEncrypted == true)
          ] end),
          size: (if $category == "unclassified" or .DBClusterIdentifier != null then null
                 else size_of({storageGiB: .AllocatedStorage}) end)
        }),

    # EKS clusters, with their managed node groups. Cluster names are unique per
    # region only, so a node group matches by name and region.
    ($docs[] | .cluster? // empty
      | .name as $name
      | (.arn | arn_region) as $region
      | [$nodegroups[] | select(.clusterName == $name and (.nodegroupArn | arn_region) == $region)] as $groups
      | {
          id: .name,
          rawType: "AWS::EKS::Cluster",
          category: "kubernetes.managed",
          region: (.arn | arn_region),
          capabilities: [
            cap("autoscaling"; any($groups[]; .scalingConfig.maxSize > .scalingConfig.minSize)),
            cap("node-high-availability"; ([$groups[].scalingConfig.desiredSize // 0] | add // 0) >= 2)
          ],
          size: size_of({count: ([$groups[].scalingConfig.desiredSize // 0] | add // 0
                                 | if . >= 1 then . else null end)})
        }),

    # Lambda functions.
    ($docs[] | .Functions? // empty | .[]
      | {
          id: .FunctionName,
          rawType: "AWS::Lambda::Function",
          category: "serverless.function",
          region: (.FunctionArn | arn_region)
        }),

    # SQS queues.
    ($docs[] | .Attributes? // empty | select(.QueueArn?)
      | {
          id: (.QueueArn | short_id),
          rawType: "AWS::SQS::Queue",
          category: "messaging.queue",
          region: (.QueueArn | arn_region),
          capabilities: [
            cap("dead-letter-queue"; has("RedrivePolicy")),
            cap("encryption-at-rest"; .SqsManagedSseEnabled == "true" or has("KmsMasterKeyId"))
          ]
        }),

    # Everything else AWS Config recorded: unclassified, with its type.
    ($docs[] | .Results? // empty | .[]
      | if type == "string" then fromjson else . end
      | select(.resourceType | IN(covered_types[]) | not)
      | {
          id: ((.resourceName // .resourceId) | short_id),
          rawType: .resourceType,
          category: "unclassified",
          region: (if .awsRegion == "global" then null else .awsRegion end)
        })
  ]
| inventory("aws")
