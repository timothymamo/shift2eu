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
#
# Resource IDs are short (the last part of the resource name), never the full name,
# which carries the project. Anything it cannot classify is "unclassified" with its
# rawType (a Cloud Asset Inventory asset type), so it shows up as a blocker.
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
# a standby is the target's to say). A number no call gives is left out,
# never guessed. The workload, relations and monthlyCost fields are the user's to
# add by hand; this program never writes them.

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
        # A 2nd gen Cloud Function runs on a Cloud Run service of its own: the
        # function stands for it. Any other Cloud Run service is unclassified.
        select(.labels["goog-managed-by"]? != "cloudfunctions")
        | $base + {category: "unclassified"}
      elif .assetType == "pubsub.googleapis.com/Topic" then
        $base + {category: "messaging.queue"}
      elif .assetType == "pubsub.googleapis.com/Subscription" then
        $base + {category: "messaging.queue", capabilities: [
          cap("dead-letter-queue"; $subscriptions[$key].deadLetterPolicy.deadLetterTopic? != null)
        ]}
      elif .assetType == "compute.googleapis.com/Snapshot"
        or .assetType == "compute.googleapis.com/Autoscaler" then
        # Part of a disk's or a group's capabilities, not a resource to move.
        empty
      else
        $base + {category: "unclassified"}
      end
  ]
| inventory("gcp")
