# azure.jq turns the output of the read-only az calls in docs/guide/inventory.md
# into a shift2eu inventory file, schema version 2
# (docs/adr/0025-json-inventory-file-format-v2.md). Run it with -n on every file
# the guide's commands wrote:
#
#   jq -n -f docs/guide/inventory/azure.jq inventories/azure/*.json > inventories/azure.inventory.json
#
# The Resource Graph listing (az graph query, one file per page) is the list of
# resources and holds most details; eight calls add what it lacks. Each element of
# each input is recognised by its shape, not by its file name. The guide's queries
# keep only the fields read here; the full output is read the same way:
#
#   {"data": [...]}                              az graph query, one page
#   {"type": "Microsoft.ServiceBus/Namespaces/Queues"}  az servicebus queue list
#   {"type": "Microsoft.ServiceBus/Namespaces/Topics"}  az servicebus topic list
#   {"type": "Microsoft.ServiceBus/Namespaces/Topics/Subscriptions"}
#                                                az servicebus topic
#                                                subscription list
#   {"type": "Microsoft.Storage/storageAccounts/queueServices/queues"}
#                                                az rest --method get .../queues
#   {"type": "Microsoft.Storage/storageAccounts/blobServices"}
#                                                az storage account
#                                                blob-service-properties show
#   {"type": "Microsoft.Storage/storageAccounts/fileServices/shares"}
#                                                az rest --method get .../shares
#                                                (with their snapshots)
#   {"type": "Microsoft.Cache/redisEnterprise/databases"}
#                                                az rest --method get .../databases
#   [{"name": ..., "numberOfCores": ...}]        az vm list-sizes
#
# Resource IDs are short (the resource's name, a topic subscription's
# "<topic>/<subscription>"), never the full Azure resource ID, which carries the
# subscription. Anything it cannot classify is "unclassified" with its rawType
# (an Azure Resource Manager type), so it shows up as a blocker.
#
# Everything in an AKS cluster's node resource group (its scale sets, load
# balancers, public IP addresses, network interfaces, the Azure Files CSI
# driver's FileStorage accounts) is left out, the cluster stands for it; its
# managed disks and those accounts' shares, persistent volumes that hold data,
# stay. So
# are a FileStorage account whose shares are listed (they stand for it), share
# snapshots (a capability of their share) and Redis Enterprise databases (their
# cluster stands for them).
#
# The size comes from the calls too: a virtual machine's or a scale set's vCPU
# and memory from its VM size (properties.hardwareProfile.vmSize or sku.name,
# looked up in az vm list-sizes), a scale set's instance count (sku.capacity), a
# managed disk's GiB (properties.diskSizeGB), a flexible server's storage
# (properties.storage.storageSizeGB), a single-node flexible server's count of 1
# (properties.highAvailability.mode Disabled or unset; a server with a standby
# gets no count, how many nodes a target bills for a standby is the target's to
# say), an AKS cluster's node count (the counts of its agent pools), a cache's
# node count (from its tier, replicas and shards, or an Azure Managed Redis
# cluster's high availability or zones), an Azure DocumentDB cluster's node count and
# storage (its shards, a standby each with high availability, and sizeGb per
# shard) and an NFS share's GiB (shareQuota, or provisionedStorageGiB). A number
# no call gives is left out, never guessed. The workload, relations and
# monthlyCost fields are the user's to add by hand; this program never writes
# them.

# The Resource Manager type of a resource ID, in the casing of the ID (Resource
# Graph writes types in lower case):
# /subscriptions/s/resourceGroups/g/providers/Microsoft.ServiceBus/namespaces/n/queues/q
# -> Microsoft.ServiceBus/namespaces/queues.
def id_type:
  split("/providers/") | last | split("/") as $p
  | [$p[0]] + [range(1; $p | length; 2) as $i | $p[$i]] | join("/");

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

# The vCPU and memory of a VM size name, from az vm list-sizes ($vmsizes), or
# null when the call did not list it.
def vm_size($vmsizes):
  if . == null then null else $vmsizes[ascii_downcase] as $s
  | if $s == null then null
    else {vcpus: $s.numberOfCores, memoryGiB: (if $s.memoryInMb == null then null else $s.memoryInMb / 1024 end)} end
  end;

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

def lower_type: (.type // "") | ascii_downcase;
def lid: ascii_downcase;

# The resource group of a resource ID, in lower case.
def id_group: lid | split("/")[4] // "";

# The types the calls next to Resource Graph return.
def child_types:
  "microsoft.servicebus/namespaces/queues", "microsoft.servicebus/namespaces/topics",
  "microsoft.servicebus/namespaces/topics/subscriptions",
  "microsoft.storage/storageaccounts/queueservices/queues",
  "microsoft.storage/storageaccounts/blobservices",
  "microsoft.storage/storageaccounts/fileservices/shares",
  "microsoft.cache/redisenterprise/databases";

# Whether the IP configuration ID names an IPv6 one, looked up in Resource Graph
# ($byid): a network interface's (.../networkInterfaces/<nic>/ipConfigurations/<name>)
# in its ipConfigurations, a scale set member's
# (.../virtualMachineScaleSets/<set>/virtualMachines/<n>/networkInterfaces/<config>/ipConfigurations/<name>)
# in its scale set's network profile.
def ipconfig_v6($byid):
  lid as $c
  | ([$c | capture("^(?<set>.*/virtualmachinescalesets/[^/]+)/virtualmachines/[^/]+/networkinterfaces/(?<nic>[^/]+)/ipconfigurations/(?<ip>[^/]+)$")] | first) as $member
  | ([$c | capture("^(?<nic>.*/networkinterfaces/[^/]+)/ipconfigurations/(?<ip>[^/]+)$")] | first) as $nic
  | if $member != null then
      any($byid[$member.set].properties.virtualMachineProfile.networkProfile.networkInterfaceConfigurations[]?
          | select((.name // "" | ascii_downcase) == $member.nic) | .properties.ipConfigurations[]?;
        (.name // "" | ascii_downcase) == $member.ip and .properties.privateIPAddressVersion == "IPv6")
    elif $nic != null then
      any($byid[$nic.nic].properties.ipConfigurations[]?;
        (.name // "" | ascii_downcase) == $nic.ip and .properties.privateIPAddressVersion == "IPv6")
    else false end;

# The number of zones a resource (or a frontend IP configuration) names.
def zone_count: (.zones // []) | length;

[inputs] as $inputs
# The VM sizes az vm list-sizes lists: an array of sizes, no id or type.
| ([$inputs[] | arrays | .[] | objects | select(has("numberOfCores") and (.name | type) == "string")]
   | INDEX(.name | ascii_downcase)) as $vmsizes
| [$inputs[] | if type == "object" and (.data | type) == "array" then .data[]
          elif type == "object" and (.value | type) == "array" then .value[]
          elif type == "array" then .[] else . end
        | objects | select((.id? | type) == "string" and (.type? | type) == "string")] as $all
| [$all[] | select(lower_type == "microsoft.servicebus/namespaces/queues")] as $queues
| [$all[] | select(lower_type == "microsoft.servicebus/namespaces/topics")] as $topics
| [$all[] | select(lower_type == "microsoft.servicebus/namespaces/topics/subscriptions")] as $subscriptions
| [$all[] | select(lower_type == "microsoft.storage/storageaccounts/queueservices/queues")] as $storagequeues
| [$all[] | select(lower_type == "microsoft.storage/storageaccounts/blobservices")] as $blobs
# A share snapshot is listed with its share's ID and a snapshotTime.
| [$all[] | select(lower_type == "microsoft.storage/storageaccounts/fileservices/shares")] as $shareelements
| [$shareelements[] | select(.properties.snapshotTime? == null)] as $shares
| ([$shareelements[] | select(.properties.snapshotTime? != null) | .id | lid] | unique) as $snapshottedshares
| ([$shares[] | .id | lid | sub("/fileservices/.*$"; "")] | unique) as $shareaccounts
| [$all[] | select(lower_type == "microsoft.cache/redisenterprise/databases")] as $redisdatabases
| [$all[] | select(lower_type | IN(child_types) | not)] as $rows
| ($rows | INDEX(.id | lid)) as $byid
| ([$rows[] | select(lower_type == "microsoft.compute/snapshots")
    | .properties.creationData.sourceResourceId // empty | lid] | unique) as $snapshotted
| ([$rows[] | select(lower_type == "microsoft.insights/autoscalesettings")
    | select(.properties.enabled != false) | .properties.targetResourceUri // empty | lid] | unique) as $autoscaled
| ([$rows[] | .properties.sourceServerResourceId? // empty | lid] | unique) as $replicated
| ([$rows[] | select(lower_type == "microsoft.documentdb/mongoclusters")
    | .properties.replicaParameters.sourceResourceId? // empty | lid] | unique) as $mongoreplicated
| ([$blobs[] | select(.isVersioningEnabled == true) | .id | lid | sub("/blobservices/.*$"; "")]) as $versioned
| ([$queues[], $topics[], $subscriptions[] | .id | lid | sub("/(queues|topics)/.*$"; "")] | unique) as $queuenamespaces
| ([$rows[] | select(lower_type == "microsoft.containerservice/managedclusters")
    | .properties.nodeResourceGroup // empty | ascii_downcase] | unique) as $nodegroups
# The OS disks of a scale set's virtual machines, and their data disks deleted with them: the
# scale set stands for them.
| ([$rows[] | select(lower_type == "microsoft.compute/virtualmachines")
    | select(.properties.virtualMachineScaleSet.id? != null)
    | .properties.storageProfile
    | (.osDisk.managedDisk.id?,
       (.dataDisks[]? | select((.deleteOption // "" | ascii_downcase) == "delete")
         | .managedDisk.id?))
    | values | lid] | unique) as $scalesetdisks
| [
    # Service Bus queues. Every queue has a dead-letter subqueue.
    ($queues[]
      | (.id | lid | sub("/queues/.*$"; "")) as $ns
      | {
          id: .name,
          rawType: (.id | id_type),
          category: "messaging.queue",
          region: $byid[$ns].location,
          capabilities: ["dead-letter-queue"]
        }),

    # Service Bus topics: publish and subscribe. Topics exist only in the
    # Standard and Premium tiers, which encrypt all data at rest.
    ($topics[]
      | (.id | lid | sub("/topics/.*$"; "")) as $ns
      | {
          id: .name,
          rawType: (.id | id_type),
          category: "messaging.topic",
          region: $byid[$ns].location,
          capabilities: ["encryption-at-rest"]
        }),

    # Service Bus topic subscriptions: each is a queue of its topic's messages
    # with a dead-letter subqueue of its own. Named "<topic>/<subscription>", as
    # the same subscription name recurs across topics.
    ($subscriptions[]
      | (.id | lid | sub("/topics/.*$"; "")) as $ns
      | {
          id: ((.id | capture("/topics/(?<t>[^/]+)/subscriptions/"; "i").t) + "/" + .name),
          rawType: (.id | id_type),
          category: "messaging.queue",
          region: $byid[$ns].location,
          capabilities: ["dead-letter-queue"]
        }),

    # File shares of a FileStorage account (snapshots are left out above). An
    # NFS share is a file share a target can serve; an SMB share stays
    # unclassified. Azure Files encrypts every share at rest.
    ($shares[]
      | (.id | lid) as $id
      | ($id | sub("/fileservices/.*$"; "")) as $account
      | {
          id: .name,
          rawType: (.id | id_type),
          region: $byid[$account].location
        } as $base
      | if (.properties.enabledProtocols // "" | ascii_downcase) == "nfs" then
          $base + {category: "storage.file", capabilities: [
            "encryption-at-rest",
            cap("snapshots"; $id | IN($snapshottedshares[])),
            cap("zone-high-availability"; $byid[$account].sku.name? // "" | ascii_downcase | endswith("_zrs"))
          ],
          size: size_of({storageGiB: .properties.shareQuota?})}
        else $base + {category: "unclassified"} end),

    # Storage queues. Azure encrypts every storage account at rest.
    ($storagequeues[]
      | (.id | lid | sub("/queueservices/.*$"; "")) as $account
      | {
          id: .name,
          rawType: (.id | id_type),
          category: "messaging.queue",
          region: $byid[$account].location,
          capabilities: ["encryption-at-rest"]
        }),

    ($rows[]
      | (.id | lid) as $id
      | lower_type as $t
      # Managed by AKS in a cluster's node resource group: the cluster stands
      # for it. A managed disk there is a persistent volume and stays.
      | select(($id | id_group | IN($nodegroups[])) and $t != "microsoft.compute/disks" | not)
      | {
          id: .name,
          rawType: (.id | id_type | if ascii_downcase == $t then . else $t end),
          region: (if .location == "global" then null else .location end)
        } as $base
      | if $t == "microsoft.compute/virtualmachines" then
          # A virtual machine of a scale set is left out: the scale set stands
          # for it.
          select(.properties.virtualMachineScaleSet.id? == null)
          | ([.properties.storageProfile.osDisk.managedDisk.id?,
              .properties.storageProfile.dataDisks[]?.managedDisk.id?] | map(select(.) | lid)) as $disks
          | $base + {category: "compute.vm", capabilities: [
              cap("ipv6"; any(.properties.networkProfile.networkInterfaces[]?.id | lid;
                any($byid[.].properties.ipConfigurations[]?; .properties.privateIPAddressVersion == "IPv6"))),
              cap("snapshots"; any($disks[]; IN($snapshotted[])))
            ],
            size: size_of(.properties.hardwareProfile.vmSize? | vm_size($vmsizes) // {})}
        elif $t == "microsoft.compute/virtualmachinescalesets" then
          $base + {category: "compute.vm", capabilities: [
            cap("autoscaling"; $id | IN($autoscaled[])),
            cap("zone-high-availability"; (.zones // []) | length >= 2)
          ],
          size: size_of((.sku.name? | vm_size($vmsizes) // {})
            + {count: (.sku.capacity? | if type == "number" and . >= 1 then . else null end)})}
        elif $t == "microsoft.baremetalinfrastructure/baremetalinstances"
          or $t == "microsoft.azurelargeinstance/azurelargeinstances" then
          # A BareMetal Infrastructure instance or a SAP HANA Large Instance: a
          # dedicated physical server (ADR 0023).
          $base + {category: "compute.baremetal"}
        elif $t == "microsoft.compute/disks" then
          # The OS disk of a scale set's virtual machine, and a data disk it
          # deletes with it, go with it. Azure encrypts every managed disk at
          # rest.
          select($id | IN($scalesetdisks[]) | not)
          | $base + {category: "storage.block", capabilities: [
            "encryption-at-rest",
            cap("snapshots"; $id | IN($snapshotted[]))
          ],
          size: size_of({storageGiB: .properties.diskSizeGB?})}
        elif $t == "microsoft.storage/storageaccounts" then
          # A FileStorage account holds file shares only: its listed shares
          # stand for it, and one with none listed is unclassified. Azure
          # encrypts every storage account at rest.
          if .kind == "FileStorage" then
            if $id | IN($shareaccounts[]) then empty else $base + {category: "unclassified"} end
          else $base + {category: "storage.object", capabilities: [
            "encryption-at-rest",
            cap("object-versioning"; $id | IN($versioned[]))
          ]} end
        elif $t == "microsoft.dbforpostgresql/flexibleservers" or $t == "microsoft.dbformysql/flexibleservers" then
          $base + {
            category: (if $t == "microsoft.dbforpostgresql/flexibleservers" then "database.postgresql" else "database.mysql" end),
            capabilities: [
              "encryption-at-rest",
              cap("point-in-time-restore"; (.properties.backup.backupRetentionDays // 0) > 0),
              cap("zone-high-availability"; .properties.highAvailability.mode? == "ZoneRedundant"),
              cap("node-high-availability"; .properties.highAvailability.mode? == "SameZone"),
              cap("read-replicas"; $id | IN($replicated[]))
            ],
            # A single-node server is one instance; one with a standby gets no
            # count, how many nodes a standby is depends on how a target prices it.
            size: size_of({storageGiB: .properties.storage.storageSizeGB?,
              count: ((.properties.highAvailability.mode? // "Disabled") as $ha
                | if $ha == "Disabled" then 1 else null end)})}
        elif $t == "microsoft.containerservice/managedclusters" then
          [.properties.agentPoolProfiles[]?] as $pools
          | $base + {category: "kubernetes.managed", capabilities: [
              cap("autoscaling"; any($pools[]; .enableAutoScaling == true)),
              cap("zone-high-availability"; any($pools[]; (.availabilityZones // []) | length >= 2)),
              cap("node-high-availability"; ([$pools[].count // 0] | add // 0) >= 2)
            ],
            size: size_of({count: ([$pools[].count // 0] | add // 0 | if . >= 1 then . else null end)})}
        elif $t == "microsoft.web/sites" then
          # A function app; a web app or a Logic App is unclassified.
          (.kind // "" | ascii_downcase | split(",")) as $kind
          | $base + {category: (if ($kind | index("functionapp")) and ($kind | index("workflowapp") | not)
              then "serverless.function" else "unclassified" end)}
        elif $t == "microsoft.app/containerapps" then
          # A container app of kind functionapp runs Azure Functions, one of
          # kind workflowapp a Logic App, which is unclassified.
          (.kind // "" | ascii_downcase | split(",")) as $kind
          | $base + {category: (if $kind | index("workflowapp") then "unclassified"
              elif $kind | index("functionapp") then "serverless.function"
              else "serverless.container" end)}
        elif $t == "microsoft.containerregistry/registries" then
          # Every registry encrypts its images at rest.
          $base + {category: "registry.container", capabilities: ["encryption-at-rest"]}
        elif $t == "microsoft.cache/redis" then
          # Azure Cache for Redis. Basic is one node; Standard a primary and a
          # replica; Premium adds shards (OSS clustering), more replicas and RDB
          # persistence. The C0 and C1 SKUs have no disk encryption.
          .properties as $p
          | ($p.sku.name? // "") as $tier
          | ([$p.shardCount? // 1, 1] | max) as $shards
          | $base + {category: "cache.redis", capabilities: [
              cap("node-high-availability"; $tier | IN("Standard", "Premium")),
              cap("zone-high-availability"; ($tier | IN("Standard", "Premium"))
                and (zone_count >= 2 or $p.zonalAllocationPolicy? == "Automatic")),
              cap("sharding"; $tier == "Premium" and ($p.shardCount? // 0) >= 1),
              cap("snapshots"; $tier == "Premium"
                and ($p.redisConfiguration["rdb-backup-enabled"]? // "" | tostring | ascii_downcase) == "true"),
              cap("encryption-at-rest"; ($p.sku.family? == "C" and ($p.sku.capacity? | IN(0, 1))) | not)
            ],
            size: size_of({count: (if $tier == "Basic" then 1 elif $tier == "Standard" then 2
              elif $tier == "Premium" then (1 + ($p.replicasPerPrimary? // $p.replicasPerMaster? // 1)) * $shards
              else null end)})}
        elif $t == "microsoft.cache/redisenterprise" then
          # Azure Managed Redis: a primary and a replica with high availability;
          # its zones never override highAvailability and redundancyMode.
          # Only a legacy Enterprise or Enterprise Flash cluster (an Enterprise*
          # SKU) names its zones in zones instead. Such a cluster always runs
          # a primary and a replica of every partition on different nodes, so
          # it is always highly available; its zones (two or more) decide only
          # zone high availability. Clustering and persistence are its
          # databases' settings, from the databases call; without it neither
          # is claimed. Disks are always encrypted.
          [$redisdatabases[] | select(.id | lid | startswith($id + "/databases/"))] as $dbs
          | ((.sku.name? // "") | ascii_downcase | startswith("enterprise")) as $legacy
          | ($legacy and zone_count >= 2) as $zonal
          | (.properties.highAvailability? == "Enabled"
              or (.properties.redundancyMode? | IN("LR", "ZR")) or $legacy) as $ha
          | $base + {category: "cache.redis", capabilities: [
              "encryption-at-rest",
              cap("node-high-availability"; $ha),
              cap("zone-high-availability"; .properties.redundancyMode? == "ZR" or $zonal),
              cap("sharding"; any($dbs[]; .properties.clusteringPolicy? == "OSSCluster")),
              cap("snapshots"; any($dbs[]; .properties.persistence.rdbEnabled? == true))
            ],
            size: size_of({count: (if $ha then 2 else 1 end)})}
        elif $t == "microsoft.documentdb/databaseaccounts" and .kind == "MongoDB" then
          # Azure Cosmos DB for MongoDB (RU): replicas in every region, other
          # regions serve reads, continuous backup restores to a point in time.
          [.properties.locations[]?] as $locations
          | $base + {category: "database.mongodb", capabilities: [
              "encryption-at-rest",
              "node-high-availability",
              cap("point-in-time-restore"; .properties.backupPolicy.type? == "Continuous"),
              cap("zone-high-availability"; any($locations[]; .isZoneRedundant == true)),
              cap("read-replicas"; ($locations | length) >= 2)
            ]}
        elif $t == "microsoft.documentdb/mongoclusters" then
          # Azure DocumentDB (vCore): with high availability, a standby per
          # shard; zone redundant only with ZoneRedundantPreferred (SameZone
          # keeps the standby in the primary's zone). Backups restore to a
          # point in time, and data is always encrypted.
          (.properties.highAvailability.targetMode? // "Disabled") as $hamode
          | ([.properties.sharding.shardCount? // 1, 1] | max) as $shards
          | $base + {category: "database.mongodb", capabilities: [
              "encryption-at-rest",
              "point-in-time-restore",
              cap("node-high-availability"; $hamode != "Disabled"),
              cap("zone-high-availability"; $hamode == "ZoneRedundantPreferred"),
              cap("read-replicas"; $id | IN($mongoreplicated[]))
            ],
            size: size_of({count: ($shards * (if $hamode == "Disabled" then 1 else 2 end)),
              storageGiB: (.properties.storage.sizeGb? | if type == "number" then . * $shards else null end)})}
        elif $t == "microsoft.network/loadbalancers" and (.sku.name? // "" | IN("Standard", "Basic")) then
          # A Standard or Basic load balancer (a Gateway one is unclassified).
          # IPv6 only end to end: a rule from an IPv6 frontend to a pool with an
          # IPv6 member. Zone redundant: a Standard one with a frontend, or its
          # public IP, in two or more zones.
          [.properties.frontendIPConfigurations[]?] as $fe
          | [.properties.backendAddressPools[]?] as $pools
          | [.properties.loadBalancingRules[]?] as $rules
          | ([$fe[] | select(.properties.privateIPAddressVersion? == "IPv6"
              or $byid[.properties.publicIPAddress.id? // "" | lid].properties.publicIPAddressVersion? == "IPv6")
              | .id | lid]) as $v6fe
          | ([$pools[] | select(any(.properties.backendIPConfigurations[]?.id | values; ipconfig_v6($byid))
              or any(.properties.loadBalancerBackendAddresses[]?.properties.ipAddress | strings; contains(":")))
              | .id | lid]) as $v6pools
          | $base + {category: "network.loadbalancer", capabilities: [
              cap("zone-high-availability"; .sku.name == "Standard"
                and any($fe[]; zone_count >= 2 or ($byid[.properties.publicIPAddress.id? // "" | lid] | zone_count) >= 2)),
              cap("udp"; any($rules[]; .properties.protocol? | IN("Udp", "All", "Quic"))),
              cap("ipv6"; any($rules[];
                (.properties.frontendIPConfiguration.id? // "" | lid | IN($v6fe[]))
                and any(.properties.backendAddressPool.id?, .properties.backendAddressPools[]?.id | values | lid; IN($v6pools[]))))
            ]}
        elif $t == "microsoft.network/applicationgateways" then
          # Layer 7, and no IPv6 backends: never ipv6 or udp.
          $base + {category: "network.loadbalancer", capabilities: [
            cap("zone-high-availability"; zone_count >= 2)
          ]}
        elif $t == "microsoft.fileshares/fileshares" then
          # A Microsoft.FileShares share, NFS only. Azure Files encrypts every
          # share at rest.
          if (.properties.protocol? // "" | ascii_downcase) == "nfs" then
            $base + {category: "storage.file", capabilities: [
              "encryption-at-rest",
              cap("zone-high-availability"; .properties.redundancy? == "Zone")
            ],
            size: size_of({storageGiB: .properties.provisionedStorageGiB?})}
          else $base + {category: "unclassified"} end
        elif $t == "microsoft.compute/snapshots" or $t == "microsoft.insights/autoscalesettings" then
          # Part of a disk's or a scale set's capabilities, not a resource to move.
          empty
        elif $t == "microsoft.servicebus/namespaces" and ($id | IN($queuenamespaces[])) then
          # Its queues and topics stand for it.
          empty
        else
          $base + {category: "unclassified"}
        end)
  ]
| inventory("azure")
