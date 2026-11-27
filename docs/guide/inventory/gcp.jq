# gcp.jq turns the output of the read-only gcloud calls in docs/guide/inventory.md
# into a shift2eu inventory file, schema version 2
# (docs/adr/0025-json-inventory-file-format-v2.md). Run it with -n on every file
# the guide's commands wrote:
#
#   jq -n -f docs/guide/inventory/gcp.jq inventories/gcp/*.json > inventories/gcp.inventory.json
#
# The Cloud Asset Inventory listing (gcloud asset search-all-resources) is the list
# of resources; the other calls only add the details the capabilities need. Each
# element of each input is recognised by its shape, not by its file name. The
# guide's --format projections keep only the fields read here; the full output is
# read the same way:
#
#   {"assetType": ..., "name": "//..."}       asset search-all-resources
#   {"kind": "compute#instance"}              compute instances list
#   {"kind": "compute#instanceGroupManager"}  compute instance-groups managed list
#   {"kind": "compute#machineType"}         compute machine-types list
#   {"kind": "compute#disk"}                compute disks list
#   {"kind": "compute#snapshot"}              compute snapshots list
#   {"kind": "sql#instance"}                  sql instances list
#   {"kind": "storage#bucket"}                storage buckets list --raw
#   {"selfLink": "https://container..."}      container clusters list
#   {"name": "projects/.../subscriptions/.."} pubsub subscriptions list
#   {"selfLink": ".../urlMaps/..."}           compute url-maps list
#   {"selfLink": ".../backendServices/..."}   compute backend-services list
#   {"selfLink": ".../forwardingRules/..."}   compute forwarding-rules list
#   {"selfLink": ".../targetPools/..."}       compute target-pools list
#   {"selfLink": ".../targetHttpsProxies/.."} compute target-https-proxies list
#   {"selfLink": ".../targetHttpProxies/.."}  compute target-http-proxies list
#   {"name": ".../instances/..", "memorySizeGb"} redis instances list
#   {"name": ".../clusters/..", "shardCount"} redis clusters list
#   {"name": ".../instances/..", "nodeConfig"} memorystore instances list
#   {"name": ".../instances/..", "fileShares"} filestore instances list
#   {"name": ".../repositories/..", "format"} artifacts repositories list
#   {"name": ".../functions/..", "serviceConfig"} functions list
#   {"name": ".../clusters/..", "capacityConfig"} managed-kafka clusters list
#   {"kind": "dns#managedZone", "project"}    dns managed-zones list, with the
#                                             project the guide's jq filter adds
#   {"project", "name", "routedRecordSets"}   dns record-sets list, reduced to a
#                                             count per zone by the guide
#
# A Cloud DNS managed zone is its asset row, joined to the managed-zones list on
# the project and the zone's name: the list writes no project, so the guide adds
# it, and an entry without one joins no row. A public zone is network.dns with
# no region (Google serves it from name servers it picks); a private zone
# (forwarding and peering zones included) and a zone the list does not hold
# stay unclassified. A public zone is dnssec when dnssecConfig.state is on or
# transfer, and dns-routing when the record-set count, the number of its record
# sets with a routingPolicy, is above 0. No record set is ever read here.
#
# Resource IDs are short (the last part of the resource name), never the full name,
# which carries the project. Anything it cannot classify is "unclassified" with its
# rawType (a Cloud Asset Inventory asset type), so it shows up as a blocker.
#
# A load balancer is one URL map, one backend service no URL map names, or one
# target pool, and those three come from the compute lists, not from the asset
# listing, which may not hold a regional URL map: the asset rows of URL maps,
# backend services, target pools, forwarding rules and target proxies are left
# out, so nothing is listed twice. The forwarding rules and target proxies are
# the load balancer's frontends: they are read for its protocol and IP version,
# then left out. A backend service of scheme INTERNAL_SELF_MANAGED (a service
# mesh: no load balancer runs in front of its backends) is no load balancer, nor
# is a URL map that names only such services. A load balancer Kubernetes created
# is kept: URL maps and backend services carry no labels, and no documented rule
# tells them apart.
#
# The nodes of a GKE cluster are left out, the cluster stands for them: the
# managed instance groups its node pools name (instanceGroupUrls), and instances
# and disks labelled goog-gke-node. An instance a managed instance group created
# is left out with its boot disk, the group stands for it.
#
# The size comes from the calls too: an instance's vCPU and memory from its
# machine type (compute machine-types list, the instance's machineType URL
# matched to the type's selfLink, zone included), a disk's GiB (compute
# disks list, sizeGb, which the Compute API writes as a string; a regional disk
# is matched by its region), and a Cloud SQL instance's storage
# (settings.dataDiskSizeGb, a string too), its vCPU and memory when its tier is
# a custom one (db-custom-<vCPU>-<memory MiB>; any other tier gives neither) and
# a count of 1 when it is a single-node instance (availabilityType ZONAL; a
# REGIONAL one with a standby gets no count, how many nodes a target bills for
# a standby is the target's to say). A Memorystore cache's memory is per node
# (a Redis instance's memorySizeGb, a Valkey instance's nodeConfig.sizeGb) and its
# count its nodes (a Redis instance's nodes, or for a STANDARD_HA one that lists
# none its replicaCount plus one; a cluster's or Valkey instance's shards times
# one plus their replicas); a Redis cluster names only its whole memory
# (sizeGb), so it gets a count and no memory. A Filestore instance's storage is
# the sum of its shares' capacityGb (a string). A Managed Service for Apache
# Kafka cluster's capacityConfig is its total vCPU and memory over the brokers
# Google provisions, max(3, ceiling(vCPU / 15)) of them, all with the same vCPU
# (https://cloud.google.com/managed-service-for-apache-kafka/docs/brokers): the
# count is that number and the vCPU one broker's share. The memory share is
# inferred: memory is provisioned per cluster at a fixed vCPU:GiB ratio
# (https://cloud.google.com/managed-service-for-apache-kafka/docs/reference/rest/v1/CapacityConfig),
# so each broker gets an equal share of it. The brokers have a disk, but the list
# gives no disk size (capacityConfig holds only vcpuCount and memoryBytes), so
# no storageGiB is written. The GB of these APIs are taken
# as GiB. A number no call gives is left out, never guessed. The workload,
# relations and monthlyCost fields are the user's to add by hand; this program
# never writes them.

# A key that names the same resource in an asset name
# (//compute.googleapis.com/projects/p/zones/z/instances/n), a selfLink
# (https://www.googleapis.com/compute/v1/projects/p/zones/z/instances/n) and a
# Pub/Sub name (projects/p/subscriptions/s): the path from "projects/" on, with
# zones and regions written as locations. A bucket's key is its name.
def key:
  if . == null then null else
  (if test("projects/") then capture("(?<k>projects/.*)$").k else sub("^//[^/]+/"; "") end)
  | gsub("/(zones|regions)/"; "/locations/") end;

# A zone: europe-west1-b.
def is_zone: test("^[a-z]+-[a-z]+[0-9]+-[a-z]$");

# The region of a location: a zone loses its suffix, global is none.
def location_region:
  if . == null or . == "" or . == "global" then null
  elif is_zone then sub("-[a-z]$"; "")
  else ascii_downcase end;

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
# sorted, identical resources listed once.
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

# The details of one kind, by key.
def details(f; k): map(select(f)) | INDEX(k);

# A compute object of a collection, recognised by its selfLink path: the
# url-maps list writes no kind.
def compute_of($collection): (.selfLink? // "") | test("/" + $collection + "/[^/]+$");

# A resource name of the API calls (projects/p/locations/l/<collection>/n).
def named_in($collection): (.name? // "") | test("^projects/[^/]+/locations/[^/]+/" + $collection + "/[^/]+$");

# The region of a compute selfLink: none for a global resource.
def link_region: (capture("/regions/(?<r>[^/]+)/") // {r: null}).r;

# A number the API writes as a string (int64) or a number.
def number: if . == null then null else tonumber end;

# The keys of the backend services a URL map names, in any of its routes, its
# route actions' weighted services and mirror policies included. A backend
# bucket is not a backend service.
def named_services: [.. | strings | select(test("/backendServices/[^/]+$")) | key] | unique;

# The key of the load balancer a forwarding rule reaches: its backend service,
# its target pool, or the URL map or backend service of its target proxy.
def reaches($proxies):
  if .backendService then .backendService | key
  elif (.target // "") | test("/targetPools/") then .target | key
  elif .target then ($proxies[.target | key] // {}) | (.urlMap // .service // empty) | key
  else empty end;

[inputs | if type == "array" then .[] else . end | objects] as $all
| ($all | details(.kind? == "compute#instance"; .selfLink | key)) as $instances
| ($all | details(.kind? == "compute#instanceGroupManager"; .selfLink | key)) as $groups
| ($all | details(.kind? == "sql#instance"; .selfLink | key)) as $sql
| ($all | details(.kind? == "compute#machineType"; .selfLink | key)) as $machinetypes
| ($all | details(.kind? == "compute#disk"; (.zone // .region) + "/disks/" + .name | key)) as $disks
| ($all | details(.kind? == "storage#bucket"; .name)) as $buckets
| ($all | details((.selfLink? // "") | startswith("https://container.googleapis.com/"); .selfLink | key)) as $clusters
| ($all | details((.name? // "") | test("^projects/[^/]+/subscriptions/"); .name | key)) as $subscriptions
| ([$all[] | select(.kind? == "compute#snapshot") | .sourceDisk? // empty | key] | unique) as $snapshotted
| ([$clusters[] | .nodePools[]?.instanceGroupUrls[]? | key] | unique) as $nodegroups
# The disks a managed instance group's instances delete with them (autoDelete),
# boot disks or not: the group stands for them.
| ([$instances[] | select([.metadata.items[]?.key] | index("created-by"))
    | .disks[]? | select(.autoDelete == true) | .source // empty | key] | unique) as $groupdisks
| ($all | details(named_in("instances") and (has("memorySizeGb") or (.tier? | IN("BASIC", "STANDARD_HA"))); .name | key)) as $redis
| ($all | details(named_in("clusters") and (has("shardCount") or has("nodeType")); .name | key)) as $redisclusters
| ($all | details(named_in("instances") and (has("nodeConfig") or has("shardCount"))
    and (has("memorySizeGb") or has("fileShares") | not); .name | key)) as $valkey
| ($all | details(named_in("instances") and has("fileShares"); .name | key)) as $filestore
| ($all | details(named_in("repositories") and has("format"); .name | key)) as $repositories
| ($all | details(named_in("clusters") and has("capacityConfig"); .name | key)) as $kafka
# Cloud DNS: the managed zones and the routed record-set counts, keyed as the
# zone's asset name is (projects/p/managedZones/z), from the project the guide
# adds and the zone's name.
| (def zone_key: "projects/" + .project + "/managedZones/" + .name | key;
   def joinable: (.project? | type) == "string" and (.name? | type) == "string";
   ($all | details(.kind? == "dns#managedZone" and joinable; zone_key)) as $dnszones
   | ($all | details(has("routedRecordSets") and joinable; zone_key)) as $dnsrouted
   | {zones: $dnszones, routed: $dnsrouted}) as $dns
# A Filestore instance has snapshots when a snapshot asset is named under it.
| ([$all[] | select(.assetType? == "file.googleapis.com/Snapshot") | .name | key
    | sub("/snapshots/[^/]+$"; "")] | unique) as $filesnapshotted
# The Cloud Run services 2nd gen Cloud Functions run on.
| ([$all[] | select(named_in("functions")) | .serviceConfig.service? // empty | key] | unique) as $functionservices
| ($all | details(compute_of("urlMaps"); .selfLink | key)) as $urlmaps
| ($all | details(compute_of("backendServices"); .selfLink | key)) as $backends
| ($all | details(compute_of("targetPools"); .selfLink | key)) as $pools
| ($all | details(compute_of("target(Https?|Grpc|Tcp|Ssl)Proxies"); .selfLink | key)) as $proxies
| [$all[] | select(compute_of("forwardingRules"))] as $frontends
| ([$urlmaps[] | named_services[]] | unique) as $named
| (def mesh: $backends[.].loadBalancingScheme? == "INTERNAL_SELF_MANAGED";
   [$urlmaps | to_entries[]
     | {key, link: .value.selfLink, rawType: "compute.googleapis.com/UrlMap", services: (.value | named_services)}
     # A URL map of a service mesh names only mesh services.
     | select((.services | length) == 0 or any(.services[]; mesh | not))]
   + [$backends | to_entries[]
     | select((.key | IN($named[]) | not) and (.key | mesh | not))
     | {key, link: .value.selfLink, rawType: "compute.googleapis.com/BackendService", services: [.key]}]
   + [$pools | to_entries[]
     | {key, link: .value.selfLink, rawType: "compute.googleapis.com/TargetPool", services: []}]
  ) as $loadbalancers
| [
    $all[] | select(.assetType? and ((.name? // "") | startswith("//")))
    # A GKE node or its disk: the cluster stands for it.
    | select(.labels["goog-gke-node"]? == null)
    | (.name | key) as $key
    | {
        id: (.name | split("/") | last),
        rawType: .assetType,
        region: (.location | location_region)
      } as $base
    | if .assetType == "compute.googleapis.com/Instance" then
        $instances[$key] as $d
        # An instance a managed instance group created is left out: the group
        # stands for it.
        | select([$d.metadata.items[]?.key] | index("created-by") | not)
        | $base + {category: "compute.vm", capabilities: [
            cap("ipv6"; any($d.networkInterfaces[]?;
              (.ipv6AccessConfigs // []) | length > 0 or .stackType == "IPV4_IPV6"))
          ],
          size: ($machinetypes[$d.machineType | key] as $m
            | if $m == null then null
              else size_of({vcpus: $m.guestCpus, memoryGiB: (if $m.memoryMb == null then null else $m.memoryMb / 1024 end)}) end)}
      elif .assetType == "compute.googleapis.com/InstanceGroupManager" then
        # The group of a GKE node pool is left out: the cluster stands for it.
        select($key | IN($nodegroups[]) | not)
        | $base + {category: "compute.vm", capabilities: [
          cap("autoscaling"; $groups[$key].status.autoscaler? != null),
          cap("zone-high-availability"; .name | test("/regions/"))
        ]}
      elif .assetType == "baremetalsolution.googleapis.com/Instance" then
        # A Bare Metal Solution server: a dedicated physical server
        # (ADR 0023).
        $base + {category: "compute.baremetal"}
      elif .assetType == "compute.googleapis.com/Disk" then
        # A disk a managed instance group's instance deletes with it goes with
        # the instance.
        # Google Cloud encrypts all data at rest.
        select($key | IN($groupdisks[]) | not)
        | $base + {category: "storage.block", capabilities: [
          "encryption-at-rest",
          cap("snapshots"; $key | IN($snapshotted[]))
        ],
        size: size_of({storageGiB: ($disks[$key].sizeGb? // null | if . == null then null else tonumber end)})}
      elif .assetType == "storage.googleapis.com/Bucket" then
        $buckets[$key] as $d
        | $base + {category: "storage.object", capabilities: [
            "encryption-at-rest",
            cap("object-versioning"; ($d.versioning.enabled? // $d.versioning_enabled? // false) == true)
          ]}
      elif .assetType == "sqladmin.googleapis.com/Instance" then
        # The engine decides the category; without the sql instances list it is
        # unknown, so unclassified.
        $sql[$key] as $d
        | (($d.databaseVersion? // "") as $v
           | if $v | startswith("POSTGRES_") then "database.postgresql"
             elif $v | startswith("MYSQL_") then "database.mysql"
             else "unclassified" end) as $category
        | $base + {category: $category, region: ($d.region // $base.region),
            capabilities: (if $category == "unclassified" then [] else [
              "encryption-at-rest",
              cap("point-in-time-restore"; $d.settings.backupConfiguration.pointInTimeRecoveryEnabled == true
                or ($category == "database.mysql" and $d.settings.backupConfiguration.binaryLogEnabled == true)),
              cap("zone-high-availability"; $d.settings.availabilityType == "REGIONAL"),
              cap("read-replicas"; ($d.replicaNames // []) | length > 0)
            ] end),
            # Only a custom tier names its vCPU and memory; a single-node
            # (ZONAL) instance is one instance, one with a standby gets no count.
            size: (if $category == "unclassified" then null else
              (($d.settings.tier? // "") | capture("^db-custom-(?<c>[0-9]+)-(?<m>[0-9]+)$")? // null) as $t
              | size_of({
                  vcpus: (if $t == null then null else $t.c | tonumber end),
                  memoryGiB: (if $t == null then null else ($t.m | tonumber) / 1024 end),
                  storageGiB: ($d.settings.dataDiskSizeGb? // null | if . == null then null else tonumber end),
                  count: (if $d.settings.availabilityType? == "ZONAL" then 1 else null end)
                }) end)}
      elif .assetType == "container.googleapis.com/Cluster" then
        $clusters[$key] as $d
        | (.location // "") as $location
        | $base + {category: "kubernetes.managed", capabilities: [
            cap("zone-high-availability"; ($location | is_zone | not) and $location != ""),
            cap("node-high-availability"; ($d.currentNodeCount // 0) >= 2),
            cap("autoscaling"; $d.autopilot.enabled? == true or any($d.nodePools[]?; .autoscaling.enabled == true))
          ]}
      elif .assetType == "cloudfunctions.googleapis.com/Function"
        or .assetType == "cloudfunctions.googleapis.com/CloudFunction" then
        $base + {category: "serverless.function"}
      elif .assetType == "run.googleapis.com/Service" then
        # A 2nd gen Cloud Function runs on a Cloud Run service of its own, the
        # one its serviceConfig.service names: the function stands for it. Any
        # other Cloud Run service is a container the provider runs and scales.
        select($key | IN($functionservices[]) | not)
        | $base + {category: "serverless.container"}
      elif .assetType == "pubsub.googleapis.com/Topic" then
        # A topic copies each message to every subscription; Google Cloud
        # encrypts it at rest. A dead-letter topic is set on a subscription,
        # never on a topic, so the subscription carries dead-letter-queue.
        $base + {category: "messaging.topic", capabilities: ["encryption-at-rest"]}
      elif .assetType == "pubsub.googleapis.com/Subscription" then
        $base + {category: "messaging.queue", capabilities: [
          cap("dead-letter-queue"; $subscriptions[$key].deadLetterPolicy.deadLetterTopic? != null)
        ]}
      elif .assetType == "redis.googleapis.com/Instance" then
        # Memorystore for Redis: STANDARD_HA has a replica that takes over, and
        # its nodes in two or more zones, or an alternativeLocationId other than
        # its locationId, either one, make it zone redundant. Each node holds
        # the whole data set.
        $redis[$key] as $d
        | ($d.tier? == "STANDARD_HA") as $ha
        | $base + {category: "cache.redis", capabilities: [
            "encryption-at-rest",
            cap("node-high-availability"; $ha),
            cap("zone-high-availability"; $ha and (([$d.nodes[]?.zone] | unique | length) >= 2
              or ($d.alternativeLocationId? != null and $d.alternativeLocationId != $d.locationId))),
            cap("snapshots"; $d.persistenceConfig.persistenceMode? == "RDB")
          ],
          size: (if $d == null then null else size_of({
            memoryGiB: $d.memorySizeGb,
            count: ((($d.nodes // []) | length) as $n
              | if $n > 0 then $n
                elif $d.tier == "BASIC" then 1
                # A STANDARD_HA instance that lists no nodes: the primary and
                # its replicas.
                elif $d.tier == "STANDARD_HA" and $d.replicaCount != null then $d.replicaCount + 1
                else null end)
          }) end)}
      elif .assetType == "redis.googleapis.com/Cluster"
        or .assetType == "memorystore.googleapis.com/Instance" then
        # Memorystore for Redis Cluster and for Valkey: a replica per shard
        # takes over, and with one the cluster survives a zone unless all of it
        # is in one zone (MULTI_ZONE is the default). A Redis cluster is
        # sharded; a listed Valkey instance is unless its mode is
        # CLUSTER_DISABLED or its deprecated spelling STANDALONE; an unset or
        # MODE_UNSPECIFIED mode means CLUSTER.
        (.assetType == "redis.googleapis.com/Cluster") as $cluster
        | (if $cluster then $redisclusters[$key] else $valkey[$key] end) as $d
        | (($d.replicaCount? // 0) >= 1) as $replicas
        | $base + {category: "cache.redis", capabilities: [
            "encryption-at-rest",
            cap("node-high-availability"; $replicas),
            cap("zone-high-availability"; $replicas
              and ($d.zoneDistributionConfig.mode? // "MULTI_ZONE") != "SINGLE_ZONE"),
            cap("sharding"; $cluster
              or ($d != null and ($d.mode? | IN("CLUSTER_DISABLED", "STANDALONE") | not))),
            cap("snapshots"; $d.persistenceConfig.mode? == "RDB")
          ],
          size: (if $d == null then null else size_of({
            memoryGiB: (if $cluster then null else $d.nodeConfig.sizeGb? end),
            count: (if $d.shardCount == null then null else $d.shardCount * (($d.replicaCount // 0) + 1) end)
          }) end)}
      elif .assetType == "artifactregistry.googleapis.com/Repository" then
        # A Docker repository is a container registry. A remote repository only
        # pulls through from an upstream and a virtual one only aggregates
        # others: neither stores an image of its own, so both are left out.
        # Without the repositories list the format is unknown: unclassified.
        $repositories[$key] as $d
        | select($d.mode? | IN("REMOTE_REPOSITORY", "VIRTUAL_REPOSITORY") | not)
        | if $d.format? == "DOCKER" then
            $base + {category: "registry.container", capabilities: ["encryption-at-rest"]}
          else $base + {category: "unclassified"} end
      elif .assetType == "file.googleapis.com/Instance" then
        # Filestore is NFS. The REGIONAL and ENTERPRISE tiers keep serving
        # through the loss of a zone.
        $filestore[$key] as $d
        | $base + {category: "storage.file", capabilities: [
            "encryption-at-rest",
            cap("zone-high-availability"; $d.tier? | IN("REGIONAL", "ENTERPRISE")),
            cap("snapshots"; $key | IN($filesnapshotted[]))
          ],
          size: (if $d == null then null else
            size_of({storageGiB: ([$d.fileShares[]?.capacityGb | number] | if . == [] then null else add end)}) end)}
      elif .assetType == "managedkafka.googleapis.com/Cluster" then
        # Managed Service for Apache Kafka: Google runs every cluster in three
        # zones, its replicas spread across them, and encrypts it at rest. Its
        # capacity is the cluster's total over its brokers, so each broker gets
        # an equal share of it. A cluster the list does not hold gets no size.
        # A Kafka Connect cluster (ConnectCluster) is an integration runtime,
        # not a cluster to move: it stays unclassified, below.
        $kafka[$key].capacityConfig as $c
        | $base + {category: "messaging.kafka", capabilities: [
            "encryption-at-rest", "node-high-availability", "zone-high-availability"
          ],
          size: (if $c.vcpuCount? == null then null else
            ($c.vcpuCount | number) as $vcpus
            | ([3, ($vcpus / 15 | ceil)] | max) as $brokers
            | size_of({
                vcpus: ($vcpus / $brokers),
                memoryGiB: ($c.memoryBytes | number | if . == null then null else . / 1073741824 / $brokers end),
                count: $brokers
              }) end)}
      elif .assetType == "dns.googleapis.com/ManagedZone" then
        # A public zone is network.dns, with no region; a private one, or one the
        # managed-zones list does not hold, stays unclassified. The capabilities
        # are read for a public zone only.
        $dns.zones[$key] as $d
        | if $d.visibility? == "public" then
            ($dns.routed[$key].routedRecordSets? // 0) as $n
            | $base + {category: "network.dns", region: null, capabilities: [
                cap("dnssec"; $d.dnssecConfig.state? | IN("on", "transfer")),
                cap("dns-routing"; ($n | type) == "number" and $n > 0)
              ]}
          else $base + {category: "unclassified"} end
      elif .assetType | test("^compute\\.googleapis\\.com/(Region)?(UrlMap|BackendService|TargetPool|TargetInstance|Target[A-Za-z]*Proxy|(Global)?ForwardingRule)$") then
        # A load balancer comes from the compute lists, below; its frontends are
        # left out.
        empty
      elif .assetType == "compute.googleapis.com/Snapshot"
        or .assetType == "compute.googleapis.com/Autoscaler"
        or .assetType == "file.googleapis.com/Snapshot"
        or .assetType == "file.googleapis.com/Backup" then
        # Part of a disk's, a group's or a file share's capabilities, not a
        # resource to move. A Filestore backup adds no capability.
        empty
      else
        $base + {category: "unclassified"}
      end
  ]
  + [
    # One network.loadbalancer per URL map, backend service no URL map names
    # and target pool. A regional or global load balancer is zone redundant
    # within a region. It forwards UDP when one of its backend services or a
    # forwarding rule that reaches it is UDP. It is ipv6 when an IPv6 forwarding
    # rule names a passthrough backend service (scheme EXTERNAL or INTERNAL)
    # directly in backendService: a passthrough load balancer delivers IPv6 to
    # dual-stack or IPv6-only backends and has no IP selection policy. A proxy
    # (an IPv6 rule through a target proxy, or scheme EXTERNAL_MANAGED or
    # INTERNAL_MANAGED) is ipv6 only when one of its backend services also
    # sends to IPv6 backends (IPV6_ONLY or PREFER_IPV6; absent is IPV4_ONLY).
    # A target pool has no protocol and takes IPv4 only.
    $loadbalancers[] as $lb
    | [$frontends[] | select(any(reaches($proxies); . == $lb.key))] as $fronts
    | {
        id: ($lb.link | split("/") | last),
        rawType: $lb.rawType,
        category: "network.loadbalancer",
        region: ($lb.link | link_region),
        capabilities: [
          "zone-high-availability",
          cap("udp"; any($lb.services[]; $backends[.].protocol? == "UDP") or any($fronts[]; .IPProtocol? == "UDP")),
          cap("ipv6"; any($fronts[]; .ipVersion? == "IPV6" and .backendService != null
              and ($backends[.backendService | key].loadBalancingScheme? | IN("EXTERNAL", "INTERNAL")))
            or (any($fronts[]; .ipVersion? == "IPV6")
              and any($lb.services[]; $backends[.].ipAddressSelectionPolicy? | IN("IPV6_ONLY", "PREFER_IPV6"))))
        ]
      }
  ]
| inventory("gcp")
