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
	// region is the region the inventory has to give the resource; "" does not
	// check it. A recorded call names a resource's region in more than one way
	// (an ARN, an availability zone, or the instances a cluster is made of), so
	// the sources whose calls disagree pin it here rather than in the golden
	// file, which a careless -update could accept away.
	region string
	// capabilities is the exact capability set the inventory has to give the
	// resource, sorted or not. A nil slice does not check it; an empty, non-nil
	// slice says the resource has none, so a capability the program over-claims
	// fails as well as one it loses.
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
	// noVCPUs, noMemoryGiB and noStorageGiB say the resource gives no vcpus, no
	// memoryGiB and no storageGiB: a Cloud SQL tier other than
	// db-custom-<vcpu>-<memory MiB> names no size the other calls report, so a
	// number taken from it would be a guess, and a DocumentDB cluster's
	// AllocatedStorage describes the volume of nothing.
	noVCPUs      bool
	noMemoryGiB  bool
	noStorageGiB bool
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
				// No target runs MariaDB: an instance of it carries no
				// capability, whatever its settings say.
				{
					id: "shop-reports-mariadb", rawType: "AWS::RDS::DBInstance", category: "unclassified",
					capabilities: []string{},
				},
				// A replication group is one cache, its member clusters are
				// not: the group stands for them. Failover with a replica in
				// every shard is node high availability and Multi-AZ is zone
				// high availability, while failover alone is not (a cluster-mode
				// group must enable it with no replica at all); cluster mode is
				// sharding; the effective encryption state is
				// StorageEncryptionType, not the at-rest flag; the count is the
				// group's member clusters, its nodes. Both shards list a primary
				// and a replica in their NodeGroupMembers, so the loss of either
				// shard's primary is survived.
				{
					id: "shop-orders-redis", rawType: "AWS::ElastiCache::ReplicationGroup", category: "cache.redis",
					region:       "eu-central-1",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "snapshots", "zone-high-availability"},
				},
				// One shard of this cluster-mode group lists a primary only: losing
				// that shard's node breaks the cache, so failover on and more
				// member clusters than shards is not node high availability. Only
				// cluster mode is left: MultiAZ is off, there is no snapshot and
				// StorageEncryptionType is none.
				{
					id: "shop-search-redis", rawType: "AWS::ElastiCache::ReplicationGroup", category: "cache.redis",
					region:       "eu-central-1",
					capabilities: []string{"sharding"},
				},
				// A non-cluster-mode group with a replica in its one shard has node
				// high availability, but no sharding, no snapshot and no zone high
				// availability: MultiAZ is off. StorageEncryptionType is null here,
				// so the at-rest flag is what says the cache encrypts.
				{
					id: "shop-cart-redis", rawType: "AWS::ElastiCache::ReplicationGroup", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability"},
				},
				// Failover on with no replica to promote keeps serving through
				// no node loss, so only cluster mode is left.
				{
					id: "shop-sessions-valkey", rawType: "AWS::ElastiCache::ReplicationGroup", category: "cache.redis",
					capabilities: []string{"sharding"},
				},
				// A cache cluster outside a group is a resource of its own, one
				// node, with neither failover nor sharding nor a snapshot. It
				// encrypts at rest: a standalone cluster has no
				// StorageEncryptionType, so its AtRestEncryptionEnabled is the
				// effective state.
				{
					id: "shop-analytics-redis", rawType: "AWS::ElastiCache::CacheCluster", category: "cache.redis",
					capabilities: []string{"encryption-at-rest"},
				},
				// The same standalone cluster unencrypted, and with no snapshot: the
				// other branch of the at-rest rule, where the flag is false and the
				// cluster carries no capability at all.
				{
					id: "shop-preview-redis", rawType: "AWS::ElastiCache::CacheCluster", category: "cache.redis",
					capabilities: []string{},
				},
				// Memcached is not the Redis protocol, so no target runs it. Its
				// NumCacheNodes is the nodes of a cache no category takes, not a
				// size: the program writes none.
				{
					id: "shop-reports-memcached", rawType: "AWS::ElastiCache::CacheCluster", category: "unclassified",
					capabilities: []string{},
				},
				// ElastiCache Serverless always replicates over three zones,
				// always encrypts and runs cluster mode only, so those four hold
				// always; snapshots follow the retention.
				{
					id: "shop-edge-valkey", rawType: "AWS::ElastiCache::ServerlessCache", category: "cache.redis",
					region:       "eu-central-1",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "snapshots", "zone-high-availability"},
				},
				{
					id: "shop-edge-memcached", rawType: "AWS::ElastiCache::ServerlessCache", category: "unclassified",
					capabilities: []string{},
				},
				// A DocumentDB cluster is one document database, not one
				// resource per instance: every target sells MongoDB as a replica
				// set, so per instance the cluster would be counted three times.
				{
					id: "shop-docs", rawType: "AWS::DocDB::DBCluster", category: "database.mongodb",
					region:       "eu-central-1",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "point-in-time-restore", "read-replicas", "zone-high-availability"},
				},
				// The cluster settings are per instance in the only call the guide
				// makes, so a capability holds only when every instance reports it:
				// one instance without backups or encryption loses both for the
				// cluster, and two instances in one zone are no zone high
				// availability. Two instances still replicate and survive a node.
				{
					id: "shop-docs-dev", rawType: "AWS::DocDB::DBCluster", category: "database.mongodb",
					capabilities: []string{"node-high-availability", "read-replicas"},
				},
				// An ALB whose target group is IPv6 reaches clients and backends
				// over IPv6; two zones are zone high availability.
				{
					id: "shop-alb", rawType: "AWS::ElasticLoadBalancingV2::LoadBalancer", category: "network.loadbalancer",
					region:       "eu-central-1",
					capabilities: []string{"ipv6", "zone-high-availability"},
				},
				// A Gateway Load Balancer inspects traffic for appliances through
				// a gateway endpoint: no target sells one, so it carries no
				// capability, whatever its zones and listeners say.
				{
					id: "shop-inspection-gwlb", rawType: "AWS::ElasticLoadBalancingV2::LoadBalancer", category: "unclassified",
					capabilities: []string{},
				},
				// A dualstack NLB whose only target group is IPv4 is not reached
				// over IPv6 at both ends; its UDP listener is the udp capability.
				{
					id: "shop-nlb", rawType: "AWS::ElasticLoadBalancingV2::LoadBalancer", category: "network.loadbalancer",
					capabilities: []string{"udp", "zone-high-availability"},
				},
				// QUIC runs over UDP, so a TCP_QUIC listener carries UDP too: the
				// NLB is only one zone and IPv4, so udp is all it has.
				{
					id: "shop-quic-nlb", rawType: "AWS::ElasticLoadBalancingV2::LoadBalancer", category: "network.loadbalancer",
					capabilities: []string{"udp"},
				},
				// The same NLB with a TCP listener instead: TCP is not UDP, so a
				// TCP-only listener gives no udp, whatever else the load balancer is.
				{
					id: "shop-tcp-nlb", rawType: "AWS::ElasticLoadBalancingV2::LoadBalancer", category: "network.loadbalancer",
					capabilities: []string{},
				},
				// A Classic Load Balancer is a load balancer as well, with no
				// IPv6 address type and no UDP listener to read. It has no ARN, so
				// its region comes from its zones.
				{
					id: "shop-classic", rawType: "AWS::ElasticLoadBalancing::LoadBalancer", category: "network.loadbalancer",
					region:       "eu-central-1",
					capabilities: []string{"zone-high-availability"},
				},
				// An SNS topic is a topic, never a queue: its dead-letter queue
				// is per subscription, which no topic attribute says.
				{
					id: "shop-alerts", rawType: "AWS::SNS::Topic", category: "messaging.topic",
					region:       "eu-central-1",
					capabilities: []string{"encryption-at-rest"},
				},
				{
					id: "shop-events", rawType: "AWS::SNS::Topic", category: "messaging.topic",
					capabilities: []string{},
				},
				// A Regional EFS file system stores data across zones; a One
				// Zone one stays in a single zone.
				{
					id: "fs-shop-regional", rawType: "AWS::EFS::FileSystem", category: "storage.file",
					region:       "eu-central-1",
					capabilities: []string{"encryption-at-rest", "zone-high-availability"},
				},
				{
					id: "fs-shop-onezone", rawType: "AWS::EFS::FileSystem", category: "storage.file",
					capabilities: []string{},
				},
				// App Runner and ECR are in AWS Config alone: App Runner's
				// category carries no capability, and ECR always encrypts.
				{
					id: "shop-api", rawType: "AWS::AppRunner::Service", category: "serverless.container",
					capabilities: []string{},
				},
				{
					id: "shop-images", rawType: "AWS::ECR::Repository", category: "registry.container",
					capabilities: []string{"encryption-at-rest"},
				},
				// A public gallery is not a private registry a workload pulls
				// from, and no category covers it: it keeps its type and no
				// capability, unlike the private repository beside it.
				{
					id: "shop-public-gallery", rawType: "AWS::ECR::PublicRepository", category: "unclassified",
					capabilities: []string{},
				},
				// No target runs a key-value table.
				{
					id: "shop-carts", rawType: "AWS::DynamoDB::Table", category: "unclassified",
					capabilities: []string{},
				},
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
				// ElastiCache Serverless names no node type and no memory: its
				// DataStorage limit is not a size.
				"shop-edge-valkey",
				"shop-edge-memcached",
				// The NumCacheNodes of a Memcached cluster is the nodes of a cache
				// the program cannot classify, not a size.
				"shop-reports-memcached",
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
				// The nodes of a cache: a replication group's member clusters,
				// both shards of shop-orders-redis included (1 primary and 1
				// replica each), and a standalone cluster's NumCacheNodes. No
				// recorded call gives a node's memory, so the guide has the user
				// add memoryGiB by hand.
				{id: "shop-orders-redis", count: 4, noMemoryGiB: true},
				{id: "shop-search-redis", count: 3, noMemoryGiB: true},
				{id: "shop-sessions-valkey", count: 1, noMemoryGiB: true},
				{id: "shop-cart-redis", count: 2, noMemoryGiB: true},
				{id: "shop-analytics-redis", count: 1, noMemoryGiB: true},
				{id: "shop-preview-redis", count: 1, noMemoryGiB: true},
				// A DocumentDB cluster's instances are its nodes. Its
				// AllocatedStorage describes nothing: the storage is the
				// cluster's volume, so the cluster gets no storageGiB.
				{id: "shop-docs", count: 3, noStorageGiB: true},
				{id: "shop-docs-dev", count: 2, noStorageGiB: true},
				// The metered size of an EFS file system, in GiB: 5 GiB for the
				// Regional one and 1 GiB for the One Zone one.
				{id: "fs-shop-regional", storageGiB: 5},
				{id: "fs-shop-onezone", storageGiB: 1},
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
				// A replication group's member clusters: the group stands for
				// them, as it does for the nodes they run.
				"shop-orders-redis-001",
				"shop-orders-redis-002",
				"shop-orders-redis-003",
				"shop-orders-redis-004",
				"shop-search-redis-001",
				"shop-search-redis-002",
				"shop-search-redis-003",
				"shop-sessions-valkey-001",
				"shop-cart-redis-001",
				"shop-cart-redis-002",
				// A DocumentDB cluster's member instances: the cluster stands
				// for them.
				"shop-docs-1",
				"shop-docs-2",
				"shop-docs-3",
				"shop-docs-dev-1",
				"shop-docs-dev-2",
				// A load balancer a Kubernetes cluster created: it goes with
				// the cluster, as its nodes do.
				"k8s-shop-ingress",
			},
			absentRawTypes: []string{
				"AWS::RDS::DBCluster",
				// The parts of a load balancer AWS Config records: the load
				// balancer stands for them, so they are not resources of their
				// own.
				"AWS::ElasticLoadBalancingV2::Listener",
				"AWS::ElasticLoadBalancingV2::ListenerRule",
				"AWS::ElasticLoadBalancingV2::TargetGroup",
				// So are a file system's access points and a registry's settings:
				// parts and settings of the resource that stands for them.
				"AWS::EFS::AccessPoint",
				"AWS::ECR::RegistryPolicy",
				"AWS::ECR::ReplicationConfiguration",
				"AWS::ECR::PullThroughCacheRule",
				"AWS::ECR::RepositoryCreationTemplate",
			},
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
				// A Valkey instance is the same kind of cache: sharded unless its mode
				// is CLUSTER_DISABLED (or STANDALONE).
				{
					id: "valkey-1", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "snapshots"},
				},
				{
					id: "valkey-sessions", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "snapshots", "zone-high-availability"},
				},
				// A Valkey instance created without a mode runs with Cluster Mode
				// Enabled: only CLUSTER_DISABLED turns sharding off, so an absent
				// mode is sharding. It is valkey-sessions without its mode, so it
				// carries the same capabilities.
				{
					id: "valkey-default", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "snapshots", "zone-high-availability"},
				},
				// STANDALONE is the deprecated spelling of CLUSTER_DISABLED, so this
				// instance is valkey-1's: no sharding, and the same capabilities.
				{
					id: "valkey-legacy", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "snapshots"},
				},
				// An instance the assets listing holds and the instances list does
				// not is no instance the program has read: no call gives its mode,
				// so it is not sharded, and it has no size either (unsized).
				{
					id: "valkey-archive", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest"},
				},
				// Every node of a single-zone Valkey instance is in one zone, so the
				// replica in each shard survives a node's loss but not a zone's.
				{
					id: "valkey-single", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability"},
				},
				// MODE_UNSPECIFIED is a mode the instance carries without naming one:
				// the API documents the default as Cluster Mode Enabled, so only
				// CLUSTER_DISABLED and STANDALONE turn sharding off. It is
				// valkey-default with the mode spelled out, so it carries the same
				// capabilities.
				{
					id: "valkey-unspecified", rawType: "memorystore.googleapis.com/Instance", category: "cache.redis",
					capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "snapshots", "zone-high-availability"},
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
				// A frontend is not only an HTTPS proxy: an IPv6 forwarding rule
				// reaches this URL map through a target HTTP proxy, and its
				// backends select IPv6 addresses, so the load balancer is ipv6.
				{
					id: "shop-edge-map", rawType: "compute.googleapis.com/UrlMap", category: "network.loadbalancer",
					capabilities: []string{"ipv6", "zone-high-availability"},
				},
				// A backend service no URL map names is a load balancer of its own,
				// and a UDP forwarding rule reaching it gives it udp. A target pool
				// names no protocol, so its UDP comes from its forwarding rule. The
				// forwarding rule in front of this passthrough service is IPv4, so
				// the load balancer serves no IPv6.
				{
					id: "shop-dns-be", rawType: "compute.googleapis.com/BackendService", category: "network.loadbalancer",
					capabilities: []string{"udp", "zone-high-availability"},
				},
				// A passthrough Network Load Balancer serves IPv6 through its
				// IPv6 forwarding rule alone: ipAddressSelectionPolicy is a proxy
				// feature, and Google documents it only for proxy-based load
				// balancers, so a passthrough backend service carries none and is
				// still ipv6. The rule reaches the service directly, with no URL
				// map or proxy between them.
				{
					id: "shop-ntp-be", rawType: "compute.googleapis.com/BackendService", category: "network.loadbalancer",
					region:       "europe-west1",
					capabilities: []string{"ipv6", "udp", "zone-high-availability"},
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
				// A Valkey node's sizeGb is its own node's memory, and its shards
				// times one plus their replicas are its nodes.
				{id: "valkey-1", memoryGiB: 6.5, count: 1},
				{id: "valkey-sessions", memoryGiB: 6.5, count: 4},
				{id: "valkey-default", memoryGiB: 6.5, count: 4},
				{id: "valkey-legacy", memoryGiB: 6.5, count: 1},
				{id: "valkey-single", memoryGiB: 6.5, count: 2},
				{id: "valkey-unspecified", memoryGiB: 6.5, count: 4},
				// A Filestore instance's capacityGb is a string in the API's JSON.
				{id: "shared", storageGiB: 1024},
				{id: "scratch", storageGiB: 1024},
			},
			// An engine the program cannot classify gets no size at all, even
			// though the sql instances list records the instance's disk and
			// tier. So does a Valkey instance the instances list does not hold: no
			// call gives its shards, its replicas or its node's memory.
			unsized: []string{"shop-reports-sqlserver", "valkey-archive"},
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
				"shop-edge-be",
				"shop-mesh-be",
				// The error service a URL map links as its custom error page, at the
				// URL map and at one of its path matchers: the URL map stands for it,
				// as it does for the backends in its routes.
				"shop-errors-be",
				// A URL map that names only that mesh's services fronts no load
				// balancer either: the mesh's backends are reached directly.
				"shop-mesh-map",
				// A load balancer's frontends, the forwarding rules and the target
				// proxies they reach: the URL map, backend service or target pool
				// behind them stands for the load balancer.
				"shop-web-fr6",
				"shop-legacy-fr6",
				// A frontend of its own kind: the IPv6 forwarding rule and the
				// target HTTP proxy it reaches.
				"shop-edge-fr6",
				"shop-dns-fr",
				"shop-ntp-fr6",
				"shop-syslog-fr",
				"shop-web-proxy",
				"shop-legacy-proxy",
				"shop-edge-proxy",
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
				// A storage queue is a queue, and so is a topic subscription, which
				// has a dead-letter subqueue of its own. A topic publishes and
				// subscribes, and Azure encrypts every Service Bus namespace at rest.
				{id: "shop-thumbnail-jobs", rawType: "Microsoft.Storage/storageAccounts/queueServices/queues", category: "messaging.queue"},
				{id: "sb-shop-order-events", rawType: "Microsoft.ServiceBus/namespaces/topics", category: "messaging.topic", capabilities: []string{"encryption-at-rest"}},
				{id: "sb-shop-order-events/billing", rawType: "Microsoft.ServiceBus/namespaces/topics/subscriptions", category: "messaging.queue", capabilities: []string{"dead-letter-queue"}},
				{id: "sb-shop-order-events/shipping", rawType: "Microsoft.ServiceBus/namespaces/topics/subscriptions", category: "messaging.queue", capabilities: []string{"dead-letter-queue"}},
				// A Premium cache is a cluster of shards with a replica each, spread
				// over zones, snapshotting its data with RDB persistence, and encrypted
				// at rest. A Basic C0 cache is one node, in no zone, and one of the two
				// SKUs Azure does not encrypt its disks for.
				{id: "redis-shop-cart", rawType: "Microsoft.Cache/redis", category: "cache.redis", capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "snapshots", "zone-high-availability"}},
				{id: "redis-shop-dev", rawType: "Microsoft.Cache/redis", category: "cache.redis", capabilities: []string{}},
				// A Standard cache is a primary and a replica, in no zone, and one of the
				// SKUs Azure encrypts the disks of. So is a Premium cache that names
				// neither shards nor replicas: two nodes, not four.
				{id: "redis-shop-sessions", rawType: "Microsoft.Cache/redis", category: "cache.redis", capabilities: []string{"encryption-at-rest", "node-high-availability"}},
				{id: "redis-shop-catalog", rawType: "Microsoft.Cache/redis", category: "cache.redis", capabilities: []string{"encryption-at-rest", "node-high-availability"}},
				// A legacy Redis Enterprise cluster spread over three zones keeps serving
				// through the loss of a zone, and the nodes that needs: one per zone is a
				// primary and its failover replicas. It names no redundancy mode, so only
				// its zones make it zone redundant. Without a database row it claims
				// neither clustering nor persistence.
				{id: "redis-shop-enterprise", rawType: "Microsoft.Cache/redisEnterprise", category: "cache.redis", capabilities: []string{"encryption-at-rest", "node-high-availability", "zone-high-availability"}},
				// Azure Managed Redis: a primary and a replica, zone redundant, and
				// clustered and persisted as its database says.
				{id: "amr-shop-session", rawType: "Microsoft.Cache/redisEnterprise", category: "cache.redis", capabilities: []string{"encryption-at-rest", "node-high-availability", "sharding", "snapshots", "zone-high-availability"}},
				// An Azure Managed Redis cluster in three zones with its high availability
				// disabled is one node: its own highAvailability setting is what makes it
				// redundant, and the zones it is deployed in do not stand in for it.
				{id: "amr-shop-scratch", rawType: "Microsoft.Cache/redisEnterprise", category: "cache.redis", capabilities: []string{"encryption-at-rest"}},
				// A Cosmos DB account for MongoDB: replicas within a region, zone
				// redundancy and continuous backup; one region serves no read replica.
				// An account of any other kind stays unclassified.
				{id: "cosmos-shop-catalog", rawType: "Microsoft.DocumentDB/databaseAccounts", category: "database.mongodb", capabilities: []string{"encryption-at-rest", "node-high-availability", "point-in-time-restore", "zone-high-availability"}},
				{id: "cosmos-shop-events", rawType: "Microsoft.DocumentDB/databaseAccounts", category: "unclassified"},
				// An account served from two regions serves reads from both.
				{id: "cosmos-shop-history", rawType: "Microsoft.DocumentDB/databaseAccounts", category: "database.mongodb", capabilities: []string{"encryption-at-rest", "node-high-availability", "read-replicas"}},
				// Azure DocumentDB: two shards, each with a standby, 128 GiB each.
				{id: "docdb-shop-orders", rawType: "Microsoft.DocumentDB/mongoClusters", category: "database.mongodb", capabilities: []string{"encryption-at-rest", "node-high-availability", "point-in-time-restore", "zone-high-availability"}},
				// A cluster in the same zone keeps a standby per shard against a node
				// failure, not against the loss of an availability zone: Microsoft,
				// "Same-zone HA protects against node failures but doesn't protect against
				// an availability zone outage"
				// (https://learn.microsoft.com/en-us/azure/documentdb/high-availability), so
				// it is not zone redundant.
				{id: "docdb-shop-samezone", rawType: "Microsoft.DocumentDB/mongoClusters", category: "database.mongodb", capabilities: []string{"encryption-at-rest", "node-high-availability", "point-in-time-restore"}},
				// A cluster another cluster replicates from serves read replicas.
				{id: "docdb-shop-events", rawType: "Microsoft.DocumentDB/mongoClusters", category: "database.mongodb", capabilities: []string{"encryption-at-rest", "node-high-availability", "point-in-time-restore", "read-replicas", "zone-high-availability"}},
				// A Standard load balancer with a UDP rule from an IPv6 frontend to an
				// IPv6 backend pool is IPv6 end to end. One with an IPv6 frontend over
				// IPv4-only backends is not IPv6 at all, and one in no zone is not zone
				// redundant. A Basic load balancer claims neither.
				{id: "lb-shop-dns", rawType: "Microsoft.Network/loadBalancers", category: "network.loadbalancer", capabilities: []string{"ipv6", "udp", "zone-high-availability"}},
				{id: "lb-shop-web-v6front", rawType: "Microsoft.Network/loadBalancers", category: "network.loadbalancer", capabilities: []string{}},
				{id: "lb-shop-legacy", rawType: "Microsoft.Network/loadBalancers", category: "network.loadbalancer", capabilities: []string{}},
				// An IPv6 frontend is IPv6 end to end with an IPv6 member of its pool,
				// whether the member is a scale set's IPv6 IP configuration or an IPv6
				// address the pool names itself. A rule of protocol All or Quic carries UDP.
				// A Gateway load balancer inserts a firewall appliance into a flow, and is
				// no load balancer to move.
				{id: "lb-shop-api", rawType: "Microsoft.Network/loadBalancers", category: "network.loadbalancer", capabilities: []string{"ipv6"}},
				{id: "lb-shop-mesh", rawType: "Microsoft.Network/loadBalancers", category: "network.loadbalancer", capabilities: []string{"ipv6"}},
				{id: "lb-shop-quic", rawType: "Microsoft.Network/loadBalancers", category: "network.loadbalancer", capabilities: []string{"udp"}},
				{id: "lb-shop-all", rawType: "Microsoft.Network/loadBalancers", category: "network.loadbalancer", capabilities: []string{"udp"}},
				{id: "lb-shop-gateway", rawType: "Microsoft.Network/loadBalancers", category: "unclassified"},
				// An Application Gateway in three zones is zone redundant, and is
				// neither an IPv6 nor a UDP load balancer.
				{id: "agw-shop", rawType: "Microsoft.Network/applicationGateways", category: "network.loadbalancer", capabilities: []string{"zone-high-availability"}},
				// A container app scales with requests. Of kind functionapp it is a
				// function, of kind workflowapp a Logic App, which stays unclassified.
				{id: "ca-shop-api", rawType: "Microsoft.App/containerApps", category: "serverless.container"},
				{id: "ca-shop-fn", rawType: "Microsoft.App/containerApps", category: "serverless.function"},
				{id: "ca-shop-flows", rawType: "Microsoft.App/containerApps", category: "unclassified"},
				// A registry encrypts every image it holds at rest.
				{id: "acrshop", rawType: "Microsoft.ContainerRegistry/registries", category: "registry.container", capabilities: []string{"encryption-at-rest"}},
				// An NFS share of a zone-redundant FileStorage account has a snapshot
				// and keeps serving through the loss of a zone; its SMB sibling stays
				// unclassified, as an SMB share has no target equivalent.
				{id: "shop-media", rawType: "Microsoft.Storage/storageAccounts/fileServices/shares", category: "storage.file", capabilities: []string{"encryption-at-rest", "snapshots", "zone-high-availability"}},
				{id: "shop-reports", rawType: "Microsoft.Storage/storageAccounts/fileServices/shares", category: "unclassified"},
				// A Microsoft.FileShares share is NFS only; one deployed across zones
				// keeps serving through the loss of a zone, a local one cannot.
				{id: "fs-shop-build", rawType: "Microsoft.FileShares/fileShares", category: "storage.file", capabilities: []string{"encryption-at-rest", "zone-high-availability"}},
				{id: "fs-shop-archive", rawType: "Microsoft.FileShares/fileShares", category: "storage.file", capabilities: []string{"encryption-at-rest"}},
				// No target runs MariaDB as a public cloud managed database.
				{id: "mariadb-shop-legacy", rawType: "Microsoft.DBforMariaDB/servers", category: "unclassified"},
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
				// A cache's nodes: one per shard, a replica on each for a Premium
				// cache (two shards are four nodes), one for a Basic cache, and the
				// primary and replica of an Azure Managed Redis cluster.
				{id: "redis-shop-cart", count: 4},
				{id: "redis-shop-dev", count: 1},
				// A Standard or Premium cache that names no shard and no replica.
				{id: "redis-shop-sessions", count: 2},
				{id: "redis-shop-catalog", count: 2},
				{id: "amr-shop-session", count: 2},
				// The nodes of the legacy Enterprise cluster spread over three zones, one
				// per zone, and the one node of an Azure Managed Redis cluster whose high
				// availability is disabled however many zones it names.
				{id: "redis-shop-enterprise", count: 2},
				{id: "amr-shop-scratch", count: 1},
				// Azure DocumentDB: a primary and a standby per shard, 128 GiB each.
				{id: "docdb-shop-orders", count: 4, storageGiB: 256},
				// A cluster in the same zone runs the same standby per shard.
				{id: "docdb-shop-samezone", count: 2},
				// A cluster and the replica another cluster replicates from it.
				{id: "docdb-shop-events", count: 4},
				{id: "docdb-shop-events-replica", count: 4},
				// The quota of an NFS share, from the shares list, and the
				// provisioned GiB of a Microsoft.FileShares share.
				{id: "shop-media", storageGiB: 1024},
				{id: "fs-shop-build", storageGiB: 256},
				{id: "fs-shop-archive", storageGiB: 512},
			},
			// The cluster's node scale set and its load balancer, the OS disk
			// of a scale set member, and a member's data disk its member deletes
			// with it: the member's scale set stands for all of them. So does a
			// FileStorage account for its shares, and a Redis Enterprise cluster for
			// its database.
			absent: []string{
				"aks-system-12345678-vmss",
				"kubernetes",
				"vmss-shop-web_0abc_OsDisk_1",
				"vmss-shop-web_0abc_DataDisk_0",
				"stshopfiles",
				"default",
				"amr-shop-session/default",
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
			// PRD #552's AWS classification: the caches, the MongoDB-compatible
			// cluster, the load balancers, the topic, the serverless container,
			// the registry and the file system its fixtures record.
			"cache.redis",
			"database.mongodb",
			"network.loadbalancer",
			"messaging.topic",
			"serverless.container",
			"registry.container",
			"storage.file",
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
			"storage.file",
			"database.postgresql",
			"database.mysql",
			"database.mongodb",
			"cache.redis",
			"kubernetes.managed",
			"serverless.function",
			"serverless.container",
			"messaging.queue",
			"messaging.topic",
			"network.loadbalancer",
			"registry.container",
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
	ID           string   `json:"id"`
	RawType      string   `json:"rawType"`
	Category     string   `json:"category"`
	Region       string   `json:"region"`
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
// with its category, raw type and (when the fact names one) region.
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

		if want.region != "" && resource.Region != want.region {
			t.Errorf("%s: resource %q has region %q, want %q",
				source, want.id, resource.Region, want.region)
		}

		if want.capabilities == nil {
			continue
		}

		got := slices.Clone(resource.Capabilities)
		slices.Sort(got)

		wantCaps := slices.Clone(want.capabilities)
		slices.Sort(wantCaps)

		// slices.Equal holds a nil capability list and an empty one equal: a
		// resource with no capability writes no key at all.
		if !slices.Equal(wantCaps, got) {
			t.Errorf("%s: resource %q has capabilities %v, want exactly %v",
				source, want.id, got, wantCaps)
		}
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
		assertNoSizeNumber(t, source, want.id, "storageGiB", want.noStorageGiB, resource.Size.StorageGiB)
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

	if f.noStorageGiB {
		fields = append(fields, "no storageGiB")
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
