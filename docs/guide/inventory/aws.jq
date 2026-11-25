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
#   {"Attributes": {"TopicArn"}}   sns get-topic-attributes
#   {"ReplicationGroups": ...}     elasticache describe-replication-groups
#   {"CacheClusters": ...}         elasticache describe-cache-clusters
#   {"ServerlessCaches": ...}      elasticache describe-serverless-caches
#   {"LoadBalancers": ...}         elbv2 describe-load-balancers
#   {"Listeners": ...}             elbv2 describe-listeners
#   {"TargetGroups": ...}          elbv2 describe-target-groups
#   {"TagDescriptions": ...}       elbv2 describe-tags
#   {"LoadBalancerDescriptions"}   elb describe-load-balancers (Classic)
#   {"FileSystems": ...}           efs describe-file-systems
#   {"Results": [...]}             configservice select-resource-config (optional;
#                                  one value per page if you page by hand)
#
# Resource IDs are short (an instance ID, a bucket or queue name), never an ARN,
# which would carry the account ID. Anything it cannot classify is "unclassified"
# with its rawType (a CloudFormation type), so it shows up as a blocker.
#
# The nodes of an EKS cluster are left out, the cluster stands for them: the Auto
# Scaling groups of its managed node groups, and instances and Auto Scaling groups
# tagged eks:cluster-name or kubernetes.io/cluster/<name>. A load balancer tagged
# elbv2.k8s.aws/cluster (the AWS Load Balancer Controller made it) is left out
# with its cluster too. An instance an Auto Scaling group launched is left out,
# the group stands for it. The volumes deleted with an instance left out go with
# it (a persistent volume, which outlives its instance, stays). The member
# clusters of an ElastiCache replication group are left out, the group stands
# for them, and so are the instances of a DocumentDB cluster, which is one
# resource.
#
# The size comes from the calls too: an instance's vCPU and memory from its
# instance type (ec2 describe-instance-types), a volume's GiB (Size), a DB
# instance's allocated storage (AllocatedStorage; none for an instance in a DB
# cluster, Aurora or an RDS Multi-AZ DB cluster, whose storage belongs to the
# cluster), a single-node DB instance's count of 1 (MultiAZ not true; an
# instance with a standby gets no count, how many nodes a target bills for a
# standby is the target's to say), an EKS cluster's node count (the desired
# sizes of its managed node groups), a cache's node count (a replication
# group's member clusters, a standalone cluster's NumCacheNodes), a DocumentDB
# cluster's instance count and an EFS file system's metered size. A number no
# call gives is left out, never guessed. The workload, relations and monthlyCost
# fields are the user's to add by hand; this program never writes them.

# The CloudFormation types the service calls above cover. AWS Config lists them
# too; those entries are dropped, so a resource is not listed twice. An Aurora
# cluster (AWS::RDS::DBCluster) is covered by its instances, which
# rds describe-db-instances lists; AWS Config records a DocumentDB cluster as
# AWS::RDS::DBCluster too, and its instances stand for it the same way.
def covered_types:
  [
    "AWS::EC2::Instance", "AWS::EC2::Volume", "AWS::AutoScaling::AutoScalingGroup",
    "AWS::S3::Bucket", "AWS::RDS::DBInstance", "AWS::RDS::DBCluster", "AWS::EKS::Cluster",
    "AWS::Lambda::Function", "AWS::SQS::Queue", "AWS::SNS::Topic", "AWS::EFS::FileSystem",
    "AWS::ElasticLoadBalancingV2::LoadBalancer", "AWS::ElasticLoadBalancing::LoadBalancer"
  ];

# The parts and settings of a classified resource that AWS Config records as
# types of their own: a load balancer's listeners, rules and target groups, an
# EFS file system's access points, and an ECR registry's settings and repository
# creation templates. The resource stands for them, so they are dropped, not
# listed as unclassified.
def part_types:
  [
    "AWS::ElasticLoadBalancingV2::Listener", "AWS::ElasticLoadBalancingV2::ListenerRule",
    "AWS::ElasticLoadBalancingV2::TargetGroup", "AWS::EFS::AccessPoint",
    "AWS::ECR::RegistryPolicy", "AWS::ECR::ReplicationConfiguration", "AWS::ECR::PullThroughCacheRule",
    "AWS::ECR::RepositoryCreationTemplate"
  ];

# The category of an AWS Config type that needs no service call: the type is
# enough. Any other type is unclassified.
def config_category:
  if . == "AWS::AppRunner::Service" then "serverless.container"
  elif . == "AWS::ECR::Repository" then "registry.container"
  else "unclassified" end;

# True for an ElastiCache engine that speaks the Redis protocol.
def is_redis: . == "redis" or . == "valkey";

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
| ([$docs[] | .TagDescriptions? // empty | .[]
    | select(any(.Tags[]?.Key // empty; . == "elbv2.k8s.aws/cluster"))
    | .ResourceArn] | unique) as $k8slbs
| [$docs[] | .Listeners? // empty | .[]] as $listeners
| [$docs[] | .TargetGroups? // empty | .[]] as $targetgroups
| [$docs[] | .DBInstances? // empty | .[] | select(.Engine == "docdb")
   | . + {cluster: (.DBClusterIdentifier // .DBInstanceIdentifier), region: (.DBInstanceArn | arn_region)}]
  as $docdb
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
    # AllocatedStorage does not describe it. A single-node instance (MultiAZ
    # not true) is one instance; one with a standby gets no count, since how
    # many nodes a standby is depends on how a target prices it. A DocumentDB
    # instance is left out here: its cluster is listed below.
    ($docs[] | .DBInstances? // empty | .[]
      | select(.Engine != "docdb")
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
                 else size_of({storageGiB: .AllocatedStorage,
                               count: (if .MultiAZ == true then null else 1 end)}) end)
        }),

    # DocumentDB clusters, one resource each from the instances
    # rds describe-db-instances lists with engine docdb, grouped by cluster and
    # region. Backups and encryption are cluster settings; no page says an
    # instance reports its cluster's, so a capability holds only when every
    # instance reports it. The storage is the cluster volume, which
    # AllocatedStorage does not describe: no storageGiB.
    ($docdb | group_by([.region, .cluster])[]
      | {
          id: .[0].cluster,
          rawType: "AWS::DocDB::DBCluster",
          category: "database.mongodb",
          region: .[0].region,
          capabilities: [
            cap("point-in-time-restore"; all(.[]; (.BackupRetentionPeriod // 0) > 0)),
            cap("encryption-at-rest"; all(.[]; .StorageEncrypted == true)),
            cap("read-replicas"; length >= 2),
            cap("node-high-availability"; length >= 2),
            cap("zone-high-availability"; [.[].AvailabilityZone // empty] | unique | length >= 2)
          ],
          size: {count: length}
        }),

    # ElastiCache replication groups: one cache each, its member clusters its
    # nodes. Failover is node high availability only with a replica to promote
    # (more member clusters than shards): a cluster-mode group must turn
    # failover on even with none. StorageEncryptionType is the effective
    # encryption state; AtRestEncryptionEnabled can be false while it is on.
    ($docs[] | .ReplicationGroups? // empty | .[]
      | ((.Engine // "") | is_redis) as $redis
      | ((.MemberClusters // []) | length) as $nodes
      | {
          id: .ReplicationGroupId,
          rawType: "AWS::ElastiCache::ReplicationGroup",
          category: (if $redis then "cache.redis" else "unclassified" end),
          region: (.ARN | arn_region),
          capabilities: (if $redis then [
            cap("node-high-availability"; .AutomaticFailover == "enabled" and $nodes > ((.NodeGroups // []) | length)),
            cap("zone-high-availability"; .MultiAZ == "enabled"),
            cap("sharding"; .ClusterEnabled == true),
            cap("snapshots"; (.SnapshotRetentionLimit // 0) > 0),
            cap("encryption-at-rest"; if .StorageEncryptionType != null
              then .StorageEncryptionType == "sse-elasticache" or .StorageEncryptionType == "sse-kms"
              else .AtRestEncryptionEnabled == true end)
          ] else [] end),
          size: (if $redis and $nodes >= 1 then {count: $nodes} else null end)
        }),

    # ElastiCache clusters outside a replication group (a member cluster is
    # left out: its group stands for it). Memcached is not the Redis protocol.
    ($docs[] | .CacheClusters? // empty | .[]
      | select((.ReplicationGroupId // "") == "")
      | ((.Engine // "") | is_redis) as $redis
      | {
          id: .CacheClusterId,
          rawType: "AWS::ElastiCache::CacheCluster",
          category: (if $redis then "cache.redis" else "unclassified" end),
          region: (.ARN | arn_region),
          capabilities: (if $redis then [
            cap("snapshots"; (.SnapshotRetentionLimit // 0) > 0),
            cap("encryption-at-rest"; .AtRestEncryptionEnabled == true)
          ] else [] end),
          size: (if $redis then size_of({count: .NumCacheNodes}) else null end)
        }),

    # ElastiCache Serverless caches: always replicated across three zones,
    # always encrypted at rest and always in cluster mode. No size: no node
    # type, and the data storage limit is a limit, not a size.
    ($docs[] | .ServerlessCaches? // empty | .[]
      | ((.Engine // "") | is_redis) as $redis
      | {
          id: .ServerlessCacheName,
          rawType: "AWS::ElastiCache::ServerlessCache",
          category: (if $redis then "cache.redis" else "unclassified" end),
          region: (.ARN | arn_region),
          capabilities: (if $redis then [
            "node-high-availability", "zone-high-availability", "sharding", "encryption-at-rest",
            cap("snapshots"; (.SnapshotRetentionLimit // 0) > 0)
          ] else [] end)
        }),

    # Application and Network Load Balancers; a Gateway Load Balancer is
    # unclassified. One a Kubernetes cluster made is left out. ipv6 needs IPv6
    # at both ends: a dualstack load balancer and an IPv6 target group of its
    # own. udp: a listener of a protocol that carries UDP (QUIC runs over UDP).
    ($docs[] | .LoadBalancers? // empty | .[]
      | select(.LoadBalancerArn | IN($k8slbs[]) | not)
      | .LoadBalancerArn as $arn
      | (.Type == "application" or .Type == "network") as $lb
      | {
          id: .LoadBalancerName,
          rawType: "AWS::ElasticLoadBalancingV2::LoadBalancer",
          category: (if $lb then "network.loadbalancer" else "unclassified" end),
          region: ($arn | arn_region),
          capabilities: (if $lb then [
            cap("zone-high-availability"; [.AvailabilityZones[]?.ZoneName] | unique | length >= 2),
            cap("udp"; any($listeners[]; .LoadBalancerArn == $arn
              and (.Protocol | IN("UDP", "TCP_UDP", "QUIC", "TCP_QUIC")))),
            cap("ipv6"; (.IpAddressType | IN("dualstack", "dualstack-without-public-ipv4"))
              and any($targetgroups[]; .IpAddressType == "ipv6" and any(.LoadBalancerArns[]?; . == $arn)))
          ] else [] end)
        }),

    # Classic Load Balancers: no ARN, so the region comes from a zone; their
    # listeners are HTTP, HTTPS, TCP or SSL, and they have no IPv6 type.
    ($docs[] | .LoadBalancerDescriptions? // empty | .[]
      | {
          id: .LoadBalancerName,
          rawType: "AWS::ElasticLoadBalancing::LoadBalancer",
          category: "network.loadbalancer",
          region: (.AvailabilityZones[0]? | zone_region),
          capabilities: [
            cap("zone-high-availability"; (.AvailabilityZones // []) | unique | length >= 2)
          ]
        }),

    # EFS file systems. A Regional one (no AvailabilityZoneName) stores data
    # across zones; a One Zone one does not. EFS has no provisioned size: the
    # size is the metered size of the data stored.
    ($docs[] | .FileSystems? // empty | .[]
      | {
          id: .FileSystemId,
          rawType: "AWS::EFS::FileSystem",
          category: "storage.file",
          region: (.FileSystemArn | arn_region),
          capabilities: [
            cap("encryption-at-rest"; .Encrypted == true),
            cap("zone-high-availability"; (.AvailabilityZoneName // "") == "")
          ],
          size: size_of({storageGiB: (if .SizeInBytes.Value? == null then null
                                      else .SizeInBytes.Value / 1073741824 end)})
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

    # SNS topics. A dead-letter queue is set per subscription, which no topic
    # attribute says, so a topic never gets dead-letter-queue.
    ($docs[] | .Attributes? // empty | select(.TopicArn?)
      | {
          id: (.TopicArn | short_id),
          rawType: "AWS::SNS::Topic",
          category: "messaging.topic",
          region: (.TopicArn | arn_region),
          capabilities: [
            cap("encryption-at-rest"; (.KmsMasterKeyId // "") != "")
          ]
        }),

    # Everything else AWS Config recorded. App Runner services and ECR
    # repositories need no call: the type is enough, and ECR encrypts every
    # repository at rest. Any other type is unclassified.
    ($docs[] | .Results? // empty | .[]
      | if type == "string" then fromjson else . end
      | select(.resourceType | IN(covered_types[], part_types[]) | not)
      | (.resourceType | config_category) as $category
      | {
          id: ((.resourceName // .resourceId) | short_id),
          rawType: .resourceType,
          category: $category,
          region: (if .awsRegion == "global" then null else .awsRegion end),
          capabilities: [cap("encryption-at-rest"; $category == "registry.container")]
        })
  ]
| inventory("aws")
