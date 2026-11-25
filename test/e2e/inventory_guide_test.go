//go:build e2e

package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file pins the jq programs docs/guide/inventory.md documents (issue #178):
// each program turns the recorded output of the guide's read-only CLI calls into
// a schema version 2 inventory the binary accepts, with the `size` of every
// resource a recorded call gives it (PRD #190, docs/adr/0025). The programs are
// part of what the guide promises, so they are run as the guide runs them,
// against the recorded fixtures, not just read.

const (
	// inventoryGuideDir holds the recorded, sanitised CLI output of the guide,
	// one directory per source.
	inventoryGuideDir = "testdata/inventory-guide"
	// jqProgramDir holds the programs the guide runs.
	jqProgramDir = "../../docs/guide/inventory"
	// inventoryGuideGoldenDir holds the inventories the programs write. It is a
	// directory of its own because the gcp and azure programs read every
	// <source>/*.json file: a golden beside them would be an input too.
	inventoryGuideGoldenDir = "inventory-guide/golden"
	// schemaVersion is the version the programs write: version 2 adds the size
	// (docs/adr/0025), which the guide's programs fill from the recorded calls.
	schemaVersion = 2
)

// inventoryGuideCase is one source the guide covers: the recorded CLI output its
// program reads, and the inventory the program writes for it.
type inventoryGuideCase struct {
	// source is the inventory's source and the program's name without .jq.
	source string
	// pattern is the glob, relative to this package, of the recorded CLI output.
	// The AWS calls are per region, so its pattern has one more level.
	pattern string
	// golden is the inventory the program writes for the recorded output. It is
	// named .golden rather than .json because .gitignore excludes
	// *.inventory.json: a real inventory describes someone's infrastructure,
	// while this one is a sanitised fixture and has to be committed.
	golden string
	// bareMetal are the recorded bare-metal servers of this source: the
	// resources the program has to classify compute.baremetal, never compute.vm
	// (ADR 0023). One source spells it in more than one way, so a source may
	// record several.
	bareMetal []bareMetalServer
	// present are resources the inventory has to hold, by ID, with this category
	// and rawType.
	present []resourceFact
	// sized are the sizes the inventory has to give, by ID: the numbers the
	// recorded read-only calls give to a resource, which the program fills its
	// `size` from (PRD #190).
	sized []sizeFact
	// unsized are resources the inventory has to give no size at all, by ID: a
	// recorded call gives the resource a number that is not its size, and the
	// program has to write none rather than that number.
	unsized []string
	// absent are resource IDs the program has to leave out altogether: another
	// recorded resource stands for them.
	absent []string
	// absentRawTypes are raw types no resource of this source may carry.
	absentRawTypes []string
}

// bareMetalServer is one bare-metal server a source's recorded output holds.
// Every provider spells it differently, so the case names the ID and the raw
// type the guide's program writes for it.
type bareMetalServer struct {
	// id is the short ID the program writes for the server.
	id string
	// rawType is the source's own type for it.
	rawType string
}

// resourceFact is what the inventory has to say about one resource, by ID. The
// assertions on a resourceFact are separate from the golden file on purpose: a
// careless -update rewrites the golden alone, so it must not be able to accept
// a mapping away.
type resourceFact struct {
	id       string
	rawType  string
	category string
	// capabilities is the set of capabilities the inventory has to give the
	// resource, sorted. It is nil when the fact checks none, and an empty,
	// non-nil slice when the resource carries none: a capability the program
	// added or dropped must not be able to hide in the golden file alone.
	capabilities []string
}

// sizeFact is the size the inventory has to give one resource, by ID. A zero
// field is not checked, so a source pins only the numbers its recorded calls
// give it; no* pins a field the recorded calls do not give, so a size written
// from a number that is not the resource's fails too.
type sizeFact struct {
	// id is the resource's ID in the inventory, short IDs only.
	id string
	// vcpus and memoryGiB are per instance, node or server: the numbers an
	// instance type, a machine type or a VM size records.
	vcpus     float64
	memoryGiB float64
	// storageGiB is the GiB of a volume, a disk or a database.
	storageGiB float64
	// count is the number of instances, nodes or volumes.
	count int
	// noVCPUs and noMemoryGiB say the resource gives no vcpus and no memoryGiB:
	// a Cloud SQL tier other than db-custom-<vcpu>-<memory MiB> names no size
	// the other calls report, so a number taken from it would be a guess.
	noVCPUs     bool
	noMemoryGiB bool
	// noCount says the resource gives no count: a database instance of high
	// availability gets none, because what its standby adds to the count
	// depends on how the target prices high availability, and the guide fills
	// count for a single-node instance only.
	noCount bool
}

// inventoryGuides returns the sources the guide covers. A source added to the
// guide gets a row here, its fixtures and a golden file.
func inventoryGuides() []inventoryGuideCase {
	return []inventoryGuideCase{
		{
			source:  "aws",
			pattern: inventoryGuideDir + "/aws/*/*.json",
			golden:  "aws.inventory.golden",
			bareMetal: []bareMetalServer{
				// An instance type ending in .metal is a dedicated physical
				// server, and so is a metal type with a size suffix.
				{id: "i-0123456789abcdef6", rawType: "AWS::EC2::Instance"},
				{id: "i-0123456789abcdef7", rawType: "AWS::EC2::Instance"},
			},
			present: []resourceFact{
				// An Aurora cluster is covered by its instances, which
				// rds describe-db-instances lists.
				{id: "shop-aurora-1", rawType: "AWS::RDS::DBInstance", category: "database.postgresql"},
				// So is an RDS Multi-AZ DB cluster: its instances are resources
				// of their own, classified by their engine.
				{id: "shop-db-cluster-writer", rawType: "AWS::RDS::DBInstance", category: "database.postgresql"},
				// A volume that outlives its node is a persistent volume and
				// stays: only the node's own root volume is left out.
				{id: "vol-0123456789abcdef6", rawType: "AWS::EC2::Volume", category: "storage.block"},
			},
			// The storage of an Aurora instance belongs to its cluster, so
			// rds describe-db-instances gives every Aurora instance
			// AllocatedStorage 1 (and, in one fixture, a stale 100): neither is
			// the instance's size. An RDS Multi-AZ DB cluster's instances get
			// none either, for the same reason, and so does an engine the
			// program cannot classify.
			unsized: []string{
				"shop-aurora-1",
				"shop-aurora-2",
				"shop-db-cluster-writer",
				"shop-reports-mariadb",
			},
			sized: []sizeFact{
				// An m5.large is 2 vCPU and 8 GiB, from ec2 describe-instance-types.
				{id: "i-0123456789abcdef0", vcpus: 2, memoryGiB: 8},
				// An m5.metal is 96 vCPU and 384 GiB, from the same call.
				{id: "i-0123456789abcdef6", vcpus: 96, memoryGiB: 384},
				// The GiB of an EBS volume, from ec2 describe-volumes.
				{id: "vol-0123456789abcdef6", storageGiB: 100},
				// The AllocatedStorage of an RDS instance, with the one instance a
				// single-node instance is. A Multi-AZ instance gets no count: what
				// its standby adds depends on how the target prices high
				// availability, so the program writes none.
				{id: "shop-db", storageGiB: 50, noCount: true},
				{id: "shop-db-replica", storageGiB: 50, count: 1},
				{id: "shop-legacy-mysql", storageGiB: 20, count: 1},
				// The nodes a managed node group wants, from eks describe-nodegroup.
				{id: "shop-eks (AWS::EKS::Cluster, eu-central-1)", count: 3},
			},
			// The EKS nodes, the Auto Scaling groups that run them and their
			// root volumes, and the root volume of an Auto Scaling group's
			// instance: the group stands for its instances.
			absent: []string{
				"i-0123456789abcdef4",
				"i-0123456789abcdef5",
				"vol-0123456789abcdef2",
				"vol-0123456789abcdef4",
				"vol-0123456789abcdef5",
				"eks-shop-eks-default-00000000-0000-0000-0000-000000000000",
				"shop-eks-self-managed",
			},
			absentRawTypes: []string{"AWS::RDS::DBCluster"},
		},
		{
			source:    "gcp",
			pattern:   inventoryGuideDir + "/gcp/*.json",
			golden:    "gcp.inventory.golden",
			bareMetal: []bareMetalServer{{id: "shop-baremetal-1", rawType: "baremetalsolution.googleapis.com/Instance"}},
			present: []resourceFact{
				// A persistent volume is a disk no Kubernetes node label covers:
				// it holds data and stays, only a node's boot disk goes.
				{id: "pvc-00000000-0000-0000-0000-000000000001", rawType: "compute.googleapis.com/Disk", category: "storage.block"},
				// So does a disk a standalone instance keeps after it goes: only a
				// managed instance group's members and their disks are left out.
				{id: "shop-web-1-data", rawType: "compute.googleapis.com/Disk", category: "storage.block"},
				// A Pub/Sub topic is where publishers write, which a queue is not,
				// and Google Cloud encrypts it at rest. A dead-letter policy is set
				// on a subscription, not on a topic, so no topic carries
				// dead-letter-queue: the subscription below carries it.
				{
					id: "shop-orders", rawType: "pubsub.googleapis.com/Topic", category: "messaging.topic",
					capabilities: []string{"encryption-at-rest"},
				},
				{
					id: "shop-orders-dlq", rawType: "pubsub.googleapis.com/Topic", category: "messaging.topic",
					capabilities: []string{"encryption-at-rest"},
				},
				{
					id: "shop-orders-worker", rawType: "pubsub.googleapis.com/Subscription", category: "messaging.queue",
					capabilities: []string{"dead-letter-queue"},
				},
				// A Cloud Run service no Cloud Function runs is a container the
				// provider runs and scales, and the category carries no capability.
				{
					id: "shop-storefront", rawType: "run.googleapis.com/Service", category: "serverless.container",
					capabilities: []string{},
				},
				// The function stands for the Cloud Run service it deployed to, so
				// the service is not in the inventory and the function keeps its
				// own rawType.
				{id: "shop-resize-images", rawType: "cloudfunctions.googleapis.com/Function", category: "serverless.function"},
				// A Memorystore instance: an at-rest-encrypted cache, with snapshots
				// when it writes RDB files, and high availability per node (a
				// replica, STANDARD_HA) and per zone (its nodes in two zones).
				{
					id: "cache-ha", rawType: "redis.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "snapshots", "zone-high-availability"},
				},
				{
					id: "cache-basic", rawType: "redis.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest"},
				},
				// An instance that lists no nodes of its own is zone redundant when
				// its alternative location is another zone: the replica it takes over
				// with lives there.
				{
					id: "cache-alt", rawType: "redis.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "zone-high-availability"},
				},
				// A cluster is sharded by definition, and a shard's replica is what
				// makes it highly available per node and per zone.
				{
					id: "sessions", rawType: "redis.googleapis.com/Cluster", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "zone-high-availability"},
				},
				// A sharded Valkey instance is the same kind of cache.
				{
					id: "valkey-1", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "snapshots"},
				},
				{
					id: "valkey-sessions", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "snapshots", "zone-high-availability"},
				},
				// Every node of a single-zone Valkey instance is in one zone, so the
				// replica in each shard survives a node's loss but not a zone's.
				{
					id: "valkey-single", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability"},
				},
				// A Memcached instance speaks another protocol, so no cache
				// category serves it and it stays unclassified with its rawType.
				{
					id: "shop-cache", rawType: "memcache.googleapis.com/Instance", category: "unclassified",
					capabilities: []string{},
				},
				// One load balancer per URL map, per backend service no URL map
				// names and per target pool. A global URL map is zone redundant
				// within its regions, and gets ipv6 only when an IPv6 forwarding
				// rule reaches it and a backend service it names sends to IPv6
				// backends.
				{
					id: "shop-web-map", rawType: "compute.googleapis.com/UrlMap", category: "network.loadbalancer",
					capabilities: []string{"ipv6", "zone-high-availability"},
				},
				{
					id: "shop-int-map", rawType: "compute.googleapis.com/UrlMap", category: "network.loadbalancer",
					capabilities: []string{"zone-high-availability"},
				},
				// An IPv6 frontend over IPv4-only backends is not ipv6: the
				// capability is the whole path, not the frontend alone.
				{
					id: "shop-legacy-map", rawType: "compute.googleapis.com/UrlMap", category: "network.loadbalancer",
					capabilities: []string{"zone-high-availability"},
				},
				// A backend service no URL map names proves its own front end via
				// a UDP forwarding rule; a target pool never names its protocol.
				{
					id: "shop-dns-be", rawType: "compute.googleapis.com/BackendService", category: "network.loadbalancer",
					capabilities: []string{"udp", "zone-high-availability"},
				},
				{
					id: "shop-syslog-pool", rawType: "compute.googleapis.com/TargetPool", category: "network.loadbalancer",
					capabilities: []string{"udp", "zone-high-availability"},
				},
				// A backend service no URL map names and no forwarding rule reaches
				// is a load balancer too, and its own UDP protocol is what proves it
				// forwards UDP: there is no frontend to read it from.
				{
					id: "shop-metrics-be", rawType: "compute.googleapis.com/BackendService", category: "network.loadbalancer",
					capabilities: []string{"udp", "zone-high-availability"},
				},
				// A Docker repository is a registry, and Artifact Registry
				// encrypts it at rest; another format holds another kind of image.
				{
					id: "images", rawType: "artifactregistry.googleapis.com/Repository", category: "registry.container",
					capabilities: []string{"encryption-at-rest"},
				},
				{
					id: "jars", rawType: "artifactregistry.googleapis.com/Repository", category: "unclassified",
					capabilities: []string{},
				},
				// Filestore is NFS file storage, regional when its tier says so,
				// and snapshotted when a snapshot of it exists.
				{
					id: "shared", rawType: "file.googleapis.com/Instance", category: "storage.file",
					capabilities: []string{"encryption-at-rest", "snapshots", "zone-high-availability"},
				},
				{
					id: "scratch", rawType: "file.googleapis.com/Instance", category: "storage.file",
					capabilities: []string{"encryption-at-rest"},
				},
			},
			sized: []sizeFact{
				// An e2-standard-2 is 2 vCPU and 8192 MB, from compute machine-types list.
				{id: "shop-web-1 (compute.googleapis.com/Instance, europe-west1)", vcpus: 2, memoryGiB: 8},
				// The sizeGb of a disk, from compute disks list.
				{id: "shop-web-1-data", storageGiB: 100},
				{id: "pvc-00000000-0000-0000-0000-000000000001", storageGiB: 50},
				// The dataDiskSizeGb of a Cloud SQL instance, which gcloud writes
				// as a string. Its tier is db-n1-standard-2, which names no vCPU
				// and memory the calls report, and its availabilityType is
				// REGIONAL, so it gets no count: what its standby adds depends on
				// how the target prices high availability.
				{id: "shop-db", storageGiB: 100, noVCPUs: true, noMemoryGiB: true, noCount: true},
				// A db-custom-<vCPU>-<memory MiB> tier gives both numbers:
				// db-custom-2-7680 is 2 vCPU and 7680 MiB. A zonal instance is one
				// instance.
				{id: "shop-catalog-mysql", vcpus: 2, memoryGiB: 7.5, storageGiB: 20, count: 1},
				// A Memorystore instance's memorySizeGb is the memory of each node,
				// which holds the whole data set, and nodes are the instance's.
				{id: "cache-ha", memoryGiB: 5, count: 2},
				{id: "cache-basic", memoryGiB: 1, count: 1},
				// An instance that lists no nodes names them through its replica
				// count: one replica next to the primary is two nodes.
				{id: "cache-alt", memoryGiB: 5, count: 2},
				// A cluster's sizeGb is the whole cluster, and no page read says
				// whether it counts replicas, so the program writes no memoryGiB
				// rather than a number divided on a guess. Its nodes are its shards
				// and their replicas.
				{id: "sessions", count: 6, noMemoryGiB: true},
				// A Valkey node's sizeGb is its own node's memory.
				{id: "valkey-1", memoryGiB: 6.5, count: 1},
				{id: "valkey-sessions", memoryGiB: 6.5, count: 4},
				{id: "valkey-single", memoryGiB: 6.5, count: 2},
				// A Filestore instance's capacityGb is a string in the API's JSON.
				{id: "shared", storageGiB: 1024},
				{id: "scratch", storageGiB: 1024},
			},
			// An engine the program cannot classify gets no size at all, even
			// though the sql instances list records the instance's disk and
			// tier.
			unsized: []string{"shop-reports-sqlserver"},
			// A node pool's managed instance group, a node with its disk,
			// labelled goog-gke-node, and the boot and data disks of the group's
			// member: the group stands for its instances.
			absent: []string{
				"gke-shop-gke-default-pool-0a1b2c3d-grp",
				"gke-shop-gke-default-pool-0a1b2c3d-x1y2",
				"shop-web-mig-0abc",
				"shop-web-mig-0abc-data",
				// The backend services its URL maps name, and the service mesh's
				// own: a mesh has no load balancer in front of its backends, so it
				// is not one.
				"shop-web-be",
				"shop-api-be",
				"shop-canary-be",
				"shop-int-be",
				"shop-legacy-be",
				"shop-mesh-be",
				// A URL map that names only that mesh's services fronts no load
				// balancer either: the mesh's backends are reached directly.
				"shop-mesh-map",
				// A load balancer's frontends, the forwarding rules and the target
				// proxies they reach: the URL map, backend service or target pool
				// behind them stands for the load balancer.
				"shop-web-fr6",
				"shop-legacy-fr6",
				"shop-dns-fr",
				"shop-syslog-fr",
				"shop-web-proxy",
				"shop-legacy-proxy",
				// A repository that only pulls through from an upstream, and one
				// that only aggregates others: neither stores an image of its own.
				"upstream",
				"mirror",
				// A Filestore snapshot is a capability of its instance, as a
				// compute snapshot is of its disk.
				"daily",
				// So is a Filestore backup: it belongs to its instance, which is
				// the resource to move.
				"shared-backup",
			},
		},
		{
			source:  "azure",
			pattern: inventoryGuideDir + "/azure/*.json",
			golden:  "azure.inventory.golden",
			bareMetal: []bareMetalServer{
				{id: "shop-baremetal-1", rawType: "Microsoft.BareMetalInfrastructure/bareMetalInstances"},
				// A SAP HANA Large Instance is a dedicated physical server too.
				{id: "shop-hana-1", rawType: "Microsoft.AzureLargeInstance/azureLargeInstances"},
			},
			present: []resourceFact{
				// A storage queue is a queue; a Service Bus topic publishes and
				// subscribes, which no taxonomy category covers.
				{id: "shop-thumbnail-jobs", rawType: "Microsoft.Storage/storageAccounts/queueServices/queues", category: "messaging.queue"},
				{id: "sb-shop-order-events", rawType: "Microsoft.ServiceBus/namespaces/topics", category: "unclassified"},
				// A persistent volume in the node resource group holds data and
				// stays: only what AKS manages for the cluster is left out.
				{id: "pvc-00000000-0000-0000-0000-000000000000", rawType: "Microsoft.Compute/disks", category: "storage.block"},
				// The OS disk of a standalone virtual machine goes with it, and is
				// still a disk to move: only a scale set member's does not.
				{id: "vm-shop-web-1_OsDisk_1", rawType: "Microsoft.Compute/disks", category: "storage.block"},
				// A scale set member's data disk that outlives the member holds
				// data and stays: only one its member deletes with it goes.
				{id: "vmss-shop-web_0abc_DataDisk_1", rawType: "Microsoft.Compute/disks", category: "storage.block"},
			},
			sized: []sizeFact{
				// A Standard_D2s_v3 is 2 cores and 8192 MB, from az vm list-sizes.
				{id: "vm-shop-web-1", vcpus: 2, memoryGiB: 8},
				// The diskSizeGB of a managed disk, from the Resource Graph query.
				{id: "disk-shop-web-1-data", storageGiB: 64},
				{id: "vm-shop-web-1_OsDisk_1", storageGiB: 128},
				// The storageSizeGB of a flexible server. ZoneRedundant and SameZone
				// get no count: what a standby adds depends on how the target prices
				// high availability, so the program writes none; a server with high
				// availability disabled or unset is one server.
				{id: "psql-shop", storageGiB: 128, noCount: true},
				{id: "mysql-shop-catalog", storageGiB: 64, noCount: true},
				{id: "psql-shop-replica", storageGiB: 128, count: 1},
				{id: "mysql-shop-reports", storageGiB: 32, count: 1},
			},
			// The cluster's node scale set and its load balancer, the OS disk
			// of a scale set member, and a member's data disk its member deletes
			// with it: the member's scale set stands for all of them.
			absent: []string{
				"aks-system-12345678-vmss",
				"kubernetes",
				"vmss-shop-web_0abc_OsDisk_1",
				"vmss-shop-web_0abc_DataDisk_0",
			},
		},
	}
}

// taxonomyCategories returns the taxonomy categories the recorded fixtures of
// source cover, unclassified aside, and nil for a source it does not know. Each
// source has a list of its own, so a change to one source's guide adds only
// that source's entries (PRD #552: GCP, for one, has no database.mongodb). They
// are written out because this package is black box and imports no domain
// package (the e2e-is-black-box depguard rule), and because a -update that
// dropped a mapping must not be able to hide itself: the inventory is checked
// against this list, not against the golden file it was written from.
func taxonomyCategories(source string) []string {
	switch source {
	case "aws":
		return []string{
			"compute.baremetal",
			"compute.vm",
			"storage.block",
			"storage.object",
			"database.postgresql",
			"database.mysql",
			"kubernetes.managed",
			"serverless.function",
			"messaging.queue",
		}
	case "gcp":
		return []string{
			"cache.redis",
			"compute.baremetal",
			"compute.vm",
			"database.mysql",
			"database.postgresql",
			"kubernetes.managed",
			"messaging.queue",
			"messaging.topic",
			"network.loadbalancer",
			"registry.container",
			"serverless.container",
			"serverless.function",
			"storage.block",
			"storage.file",
			"storage.object",
		}
	case "azure":
		return []string{
			"compute.baremetal",
			"compute.vm",
			"storage.block",
			"storage.object",
			"database.postgresql",
			"database.mysql",
			"kubernetes.managed",
			"serverless.function",
			"messaging.queue",
		}
	default:
		return nil
	}
}

// inventoryDoc is the part of a schema version 2 inventory this file checks.
type inventoryDoc struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Source        string              `json:"source"`
	Resources     []inventoryResource `json:"resources"`
}

// inventoryResource is the part of one resource this file checks: the fields the
// programs write, and the version 2 fields the guide tells the user to add by
// hand, which a program must not write.
type inventoryResource struct {
	ID       string `json:"id"`
	RawType  string `json:"rawType"`
	Category string `json:"category"`
	Region   string `json:"region"`
	// Capabilities is the set of capabilities the guide's program derives for
	// the resource, sorted.
	Capabilities []string `json:"capabilities"`
	// Size is the resource's size, filled from the recorded read-only calls.
	Size *sizeSpec `json:"size"`
	// Workload, Relations and MonthlyCost are added by hand: no recorded call
	// gives them.
	Workload    string            `json:"workload"`
	Relations   []json.RawMessage `json:"relations"`
	MonthlyCost *json.RawMessage  `json:"monthlyCost"`
}

// sizeSpec is a resource's `size` object as this file checks it. Every field is
// optional and nil when the program does not write it.
type sizeSpec struct {
	VCPUs      *float64 `json:"vcpus"`
	MemoryGiB  *float64 `json:"memoryGiB"`
	StorageGiB *float64 `json:"storageGiB"`
	Count      *int     `json:"count"`
}

// findResource returns the resource with id, and whether the inventory holds it.
func findResource(doc inventoryDoc, id string) (inventoryResource, bool) {
	for _, resource := range doc.Resources {
		if resource.ID == id {
			return resource, true
		}
	}

	return inventoryResource{}, false
}

// jqPath returns the path of the jq every release target has installed. A test
// that cannot run the programs fails instead of skipping: the guide's promise
// would otherwise go unchecked wherever jq is missing.
func jqPath(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("jq")
	if err != nil {
		t.Fatalf("jq is not on PATH; the inventory guide's programs need it (install jq): %v", err)
	}

	return path
}

// TestInventoryGuidePrograms pins docs/guide/inventory/<source>.jq end to end:
// the program, run as the guide runs it on the recorded CLI output, writes the
// golden inventory, and the built binary assesses that inventory against every
// target without an error. The assertions beyond the golden file are the
// guide's promises that a careless -update must not be able to accept away: the
// document's schema version and source, every taxonomy category the fixtures
// cover, the bare-metal servers of each source (ADR 0023), the resources the
// program has to leave out or classify a particular way, an unclassified
// resource that keeps its rawType, and short IDs only.
func TestInventoryGuidePrograms(t *testing.T) {
	t.Parallel()

	jq := jqPath(t)

	for _, tc := range inventoryGuides() {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()

			inputs, err := filepath.Glob(tc.pattern)
			if err != nil {
				t.Fatalf("glob %s: %v", tc.pattern, err)
			}

			if len(inputs) == 0 {
				t.Fatalf("%s matched no recorded CLI output; the fixtures are missing", tc.pattern)
			}

			slices.Sort(inputs)

			stdout := runJQ(t, jq, tc.source, inputs)

			testutil.Golden(t, filepath.Join("testdata", inventoryGuideGoldenDir, tc.golden), stdout)

			doc := decodeInventory(t, stdout)

			if doc.SchemaVersion != schemaVersion {
				t.Errorf("schemaVersion = %d, want %d", doc.SchemaVersion, schemaVersion)
			}

			if doc.Source != tc.source {
				t.Errorf("source = %q, want %q", doc.Source, tc.source)
			}

			if len(doc.Resources) == 0 {
				t.Fatalf("the inventory holds no resource; the program classified nothing\n%s", stdout)
			}

			assertCategories(t, tc.source, doc)
			assertBareMetal(t, tc.source, doc, tc.bareMetal)
			assertPresent(t, tc.source, doc, tc.present)
			assertSized(t, tc.source, doc, tc.sized)
			assertUnsized(t, tc.source, doc, tc.unsized)
			assertNoUserFields(t, tc.source, doc)
			assertAbsent(t, tc.source, doc, tc.absent)
			assertNoRawTypes(t, tc.source, doc, tc.absentRawTypes)
			assertUnclassifiedKeepsRawType(t, tc.source, doc)
			assertShortIDs(t, tc.source, doc)

			assertAssessAccepts(t, tc, stdout)
		})
	}
}

// runJQ runs jq -n -f the source's program on the recorded CLI output, as the
// guide does, and returns what it wrote to stdout. The program must exit 0 with
// nothing on stderr, so a warning a reader would not notice fails the test.
func runJQ(t *testing.T, jq, source string, inputs []string) []byte {
	t.Helper()

	args := append([]string{"-n", "-f", filepath.Join(jqProgramDir, source+".jq")}, inputs...)

	cmd := exec.Command(jq, args...)

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// The program needs no environment: it reads the files the arguments name.
	cmd.Env = []string{}

	err := cmd.Run()

	var exitErr *exec.ExitError

	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		t.Fatalf("jq -n -f %s %v exited %d, want 0; stderr = %q",
			source+".jq", inputs, exitErr.ExitCode(), stderr.String())
	default:
		t.Fatalf("running jq on %s's recorded CLI output: %v", source, err)
	}

	if stderr.Len() != 0 {
		t.Errorf("jq wrote to stderr, want nothing: %q", stderr.String())
	}

	return []byte(stdout.String())
}

// decodeInventory decodes the document the program wrote. The binary has to
// decode it too, so a document that does not decode fails here first.
func decodeInventory(t *testing.T, document []byte) inventoryDoc {
	t.Helper()

	var doc inventoryDoc
	if err := json.Unmarshal(document, &doc); err != nil {
		t.Fatalf("the program's output is not a JSON inventory: %v\n%s", err, document)
	}

	return doc
}

// assertCategories fails unless the inventory's categories are exactly the ones
// the source's fixtures cover (taxonomyCategories) and unclassified. A category
// that no longer appears is a mapping the program lost, or a fixture it no
// longer reads.
func assertCategories(t *testing.T, source string, doc inventoryDoc) {
	t.Helper()

	categories := taxonomyCategories(source)
	if categories == nil {
		t.Fatalf("%s: no category list for this source in taxonomyCategories", source)
	}

	got := make([]string, 0, len(doc.Resources))
	for _, resource := range doc.Resources {
		got = append(got, resource.Category)
	}

	slices.Sort(got)
	got = slices.Compact(got)

	want := append(slices.Clone(categories), "unclassified")
	slices.Sort(want)
	want = slices.Compact(want)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s: the inventory's categories do not cover the fixture (-want +got):\n%s", source, diff)
	}
}

// assertBareMetal fails unless the inventory holds each recorded bare-metal
// server, by ID, as compute.baremetal with the source's raw type. A bare-metal
// server is not a virtual machine (ADR 0023): moving to it changes how the
// resource is provisioned, sized, snapshotted and billed, so classifying it
// compute.vm assesses it against hosts that cannot replace it.
func assertBareMetal(t *testing.T, source string, doc inventoryDoc, wants []bareMetalServer) {
	t.Helper()

	for _, want := range wants {
		resource, ok := findResource(doc, want.id)
		if !ok {
			t.Errorf("%s: the inventory holds no resource %q; the recorded bare-metal server is missing",
				source, want.id)

			continue
		}

		if resource.Category != "compute.baremetal" {
			t.Errorf("%s: bare-metal server %q has category %q, want %q",
				source, want.id, resource.Category, "compute.baremetal")
		}

		if resource.RawType != want.rawType {
			t.Errorf("%s: bare-metal server %q has rawType %q, want %q",
				source, want.id, resource.RawType, want.rawType)
		}
	}
}

// assertPresent fails unless the inventory holds each fact's resource, by ID,
// with its category and raw type.
func assertPresent(t *testing.T, source string, doc inventoryDoc, wants []resourceFact) {
	t.Helper()

	for _, want := range wants {
		resource, ok := findResource(doc, want.id)
		if !ok {
			t.Errorf("%s: the inventory holds no resource %q", source, want.id)

			continue
		}

		if resource.Category != want.category {
			t.Errorf("%s: resource %q has category %q, want %q",
				source, want.id, resource.Category, want.category)
		}

		if resource.RawType != want.rawType {
			t.Errorf("%s: resource %q has rawType %q, want %q",
				source, want.id, resource.RawType, want.rawType)
		}

		if want.capabilities != nil {
			assertCapabilities(t, source, want, resource.Capabilities)
		}
	}
}

// assertCapabilities fails unless the inventory gives the resource exactly the
// capabilities the fact lists. The golden file alone cannot catch a capability
// a careless -update drops, and a capability is what the assessment matches a
// plan on. A fact that lists none means the resource carries none, and the
// program writes that by omitting the key, so the decoded slice is nil there:
// an empty want matches nil or an empty got, which is the same inventory.
func assertCapabilities(t *testing.T, source string, want resourceFact, got []string) {
	t.Helper()

	if diff := cmp.Diff(want.capabilities, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("%s: resource %q has capabilities that differ from the fact (-want +got):\n%s",
			source, want.id, diff)
	}
}

// assertSized fails unless the inventory gives each fact's resource the size its
// recorded calls give it: the vCPU and memory of an instance type, a machine
// type or a VM size, the GiB of a volume, a disk or a database, and the nodes a
// node group wants. A size the program dropped, or wrote for the wrong resource,
// shows up here; the golden file alone cannot catch it after an -update.
func assertSized(t *testing.T, source string, doc inventoryDoc, wants []sizeFact) {
	t.Helper()

	for _, want := range wants {
		resource, ok := findResource(doc, want.id)
		if !ok {
			t.Errorf("%s: the inventory holds no resource %q, so its size cannot be checked", source, want.id)

			continue
		}

		if resource.Size == nil {
			t.Errorf("%s: resource %q has no size, want the one its recorded calls give (%s)",
				source, want.id, want.describe())

			continue
		}

		assertSizeNumber(t, source, want.id, "vcpus", want.vcpus, resource.Size.VCPUs)
		assertSizeNumber(t, source, want.id, "memoryGiB", want.memoryGiB, resource.Size.MemoryGiB)
		assertSizeNumber(t, source, want.id, "storageGiB", want.storageGiB, resource.Size.StorageGiB)
		assertNoSizeNumber(t, source, want.id, "vcpus", want.noVCPUs, resource.Size.VCPUs)
		assertNoSizeNumber(t, source, want.id, "memoryGiB", want.noMemoryGiB, resource.Size.MemoryGiB)
		assertNoSizeCount(t, source, want.id, want.noCount, resource.Size.Count)

		if want.count != 0 && (resource.Size.Count == nil || *resource.Size.Count != want.count) {
			t.Errorf("%s: resource %q has size.count %s, want %d", source, want.id,
				derefCount(resource.Size.Count), want.count)
		}
	}
}

// describe says which numbers the fact checks, for an error message.
func (f sizeFact) describe() string {
	var fields []string

	if f.vcpus != 0 {
		fields = append(fields, "vcpus")
	}

	if f.memoryGiB != 0 {
		fields = append(fields, "memoryGiB")
	}

	if f.storageGiB != 0 {
		fields = append(fields, "storageGiB")
	}

	if f.count != 0 {
		fields = append(fields, "count")
	}

	if f.noVCPUs {
		fields = append(fields, "no vcpus")
	}

	if f.noMemoryGiB {
		fields = append(fields, "no memoryGiB")
	}

	if f.noCount {
		fields = append(fields, "no count")
	}

	if len(fields) == 0 {
		return "no field"
	}

	return strings.Join(fields, ", ")
}

// assertSizeNumber fails unless got is want, which is 0 when the fact does not
// check the field.
func assertSizeNumber(t *testing.T, source, id, field string, want float64, got *float64) {
	t.Helper()

	if want == 0 {
		return
	}

	if got == nil {
		t.Errorf("%s: resource %q gives no size.%s, want %v", source, id, field, want)

		return
	}

	if *got != want {
		t.Errorf("%s: resource %q has size.%s = %v, want %v", source, id, field, *got, want)
	}
}

// assertNoSizeNumber fails when got is set, which the fact says no recorded
// call gives: a number the program guessed is worse than none, because the
// assessment prices it instead of saying the size is unknown.
func assertNoSizeNumber(t *testing.T, source, id, field string, wantNone bool, got *float64) {
	t.Helper()

	if wantNone && got != nil {
		t.Errorf("%s: resource %q gives size.%s = %v, want none: no recorded call gives it",
			source, id, field, *got)
	}
}

// assertNoSizeCount fails when the resource gives a count, which the fact says
// it has none of.
func assertNoSizeCount(t *testing.T, source, id string, wantNone bool, got *int) {
	t.Helper()

	if wantNone && got != nil {
		t.Errorf("%s: resource %q gives size.count = %d, want none", source, id, *got)
	}
}

// derefCount returns the count as text, and "none" when the resource gives none.
func derefCount(count *int) string {
	if count == nil {
		return "none"
	}

	return fmt.Sprint(*count)
}

// assertUnsized fails when the inventory gives a resource a size. The recorded
// call exists for the resource but the number it gives is not the resource's
// size, so the program has to write none: a size written from it is worse than
// no size, because the assessment prices a wrong number instead of saying the
// size is unknown.
func assertUnsized(t *testing.T, source string, doc inventoryDoc, ids []string) {
	t.Helper()

	for _, id := range ids {
		resource, ok := findResource(doc, id)
		if !ok {
			t.Errorf("%s: the inventory holds no resource %q, so its size cannot be checked", source, id)

			continue
		}

		if resource.Size != nil {
			t.Errorf("%s: resource %q has a size (%s), want none: no recorded call gives its size",
				source, id, resource.Size.describe())
		}
	}
}

// describe says which numbers a size holds, for an error message.
func (s sizeSpec) describe() string {
	var fields []string

	if s.VCPUs != nil {
		fields = append(fields, fmt.Sprintf("vcpus: %v", *s.VCPUs))
	}

	if s.MemoryGiB != nil {
		fields = append(fields, fmt.Sprintf("memoryGiB: %v", *s.MemoryGiB))
	}

	if s.StorageGiB != nil {
		fields = append(fields, fmt.Sprintf("storageGiB: %v", *s.StorageGiB))
	}

	if s.Count != nil {
		fields = append(fields, fmt.Sprintf("count: %d", *s.Count))
	}

	if len(fields) == 0 {
		return "no field"
	}

	return strings.Join(fields, ", ")
}

// assertNoUserFields fails when the inventory gives a resource a workload, a
// relation or a monthly cost: the guide tells the user to add those by hand, and
// no recorded read-only call gives them.
func assertNoUserFields(t *testing.T, source string, doc inventoryDoc) {
	t.Helper()

	for _, resource := range doc.Resources {
		if resource.Workload != "" {
			t.Errorf("%s: resource %q has workload %q; the programs write no workload",
				source, resource.ID, resource.Workload)
		}

		if len(resource.Relations) != 0 {
			t.Errorf("%s: resource %q has %d relation(s); the programs write no relations",
				source, resource.ID, len(resource.Relations))
		}

		if resource.MonthlyCost != nil {
			t.Errorf("%s: resource %q has a monthlyCost; the programs write none", source, resource.ID)
		}
	}
}

// assertAbsent fails when the inventory holds any of the IDs. The nodes of a
// managed Kubernetes cluster are left out, the cluster stands for them, and so
// is what the cluster or a group manages for them: an inventory that lists them
// assesses every node as a machine of its own.
func assertAbsent(t *testing.T, source string, doc inventoryDoc, ids []string) {
	t.Helper()

	for _, id := range ids {
		resource, ok := findResource(doc, id)
		if !ok {
			continue
		}

		t.Errorf("%s: the inventory holds %q (%s, %s), want it left out: another recorded resource stands for it",
			source, id, resource.RawType, resource.Category)
	}
}

// assertNoRawTypes fails when a resource carries one of the raw types. The
// entries name a resource that another, listed resource covers: a cluster is
// assessed per instance, so listing it too would count it twice.
func assertNoRawTypes(t *testing.T, source string, doc inventoryDoc, rawTypes []string) {
	t.Helper()

	for _, resource := range doc.Resources {
		if slices.Contains(rawTypes, resource.RawType) {
			t.Errorf("%s: resource %q has rawType %q, want none: another resource covers it",
				source, resource.ID, resource.RawType)
		}
	}
}

// assertUnclassifiedKeepsRawType fails unless the inventory holds an
// unclassified resource with a rawType. An unclassified resource without one
// tells a reader what blocks the move even less than the category already does.
func assertUnclassifiedKeepsRawType(t *testing.T, source string, doc inventoryDoc) {
	t.Helper()

	var unclassified []string

	for _, resource := range doc.Resources {
		if resource.Category != "unclassified" {
			continue
		}

		if resource.RawType == "" {
			t.Errorf("%s: unclassified resource %q has no rawType", source, resource.ID)

			continue
		}

		unclassified = append(unclassified, resource.ID)
	}

	if len(unclassified) == 0 {
		t.Errorf("%s: the inventory holds no unclassified resource with a rawType, want at least one", source)
	}
}

// assertShortIDs fails when a resource ID still carries an ARN, an Azure
// resource ID or a GCP resource name: those hold the account, subscription or
// project, which must not reach an inventory or a report.
func assertShortIDs(t *testing.T, source string, doc inventoryDoc) {
	t.Helper()

	long := []string{"arn:", "/subscriptions/", "projects/"}

	for _, resource := range doc.Resources {
		for _, marker := range long {
			if strings.Contains(resource.ID, marker) {
				t.Errorf("%s: resource id %q holds %q; the guide's programs write short ids only",
					source, resource.ID, marker)
			}
		}
	}
}

// assertAssessAccepts runs the built binary on the inventory the program wrote,
// against every target, and fails unless it exits 0 with nothing on stderr. The
// output goes to a temporary directory, so no fixture is written by a test.
func assertAssessAccepts(t *testing.T, tc inventoryGuideCase, document []byte) {
	t.Helper()

	inventory := filepath.Join(t.TempDir(), tc.golden)
	if err := os.WriteFile(inventory, document, 0o600); err != nil {
		t.Fatalf("write %s: %v", inventory, err)
	}

	got := assess(t, allKey, inventory)

	if got.code != 0 {
		t.Fatalf("shift2eu assess --target %s on the %s inventory exited %d, want 0; stderr = %q",
			allKey, tc.source, got.code, got.stderr)
	}

	if got.stderr != "" {
		t.Errorf("assess wrote to stderr, want nothing: %q", got.stderr)
	}

	// The report names the source the program wrote, so a document that
	// decodes but says nothing usable still fails here.
	want := "- Source: `" + tc.source + "`"

	if !strings.Contains(got.stdout, want) {
		t.Errorf("the report does not hold %q:\n%s", want, got.stdout)
	}
}
