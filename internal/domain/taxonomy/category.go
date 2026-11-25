package taxonomy

import (
	"errors"
	"fmt"
	"slices"

	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// Category is a provider-neutral service type, such as compute.vm. Its string
// value is the name sources, targets and the inventory file format share.
type Category string

// The taxonomy categories. Sources classify resources into one of these, and
// targets declare which of them their offerings serve.
const (
	// ComputeVM is a virtual machine running a general purpose operating
	// system, on a host the provider runs and may share. A dedicated physical
	// server is ComputeBareMetal instead.
	ComputeVM Category = "compute.vm"
	// ComputeBareMetal is a dedicated physical (bare-metal) server running a
	// general purpose operating system, with no hypervisor between the
	// operating system and the hardware.
	ComputeBareMetal Category = "compute.baremetal"
	// KubernetesManaged is a Kubernetes cluster whose control plane the provider runs.
	KubernetesManaged Category = "kubernetes.managed"
	// StorageObject is object storage: buckets of objects addressed by key.
	StorageObject Category = "storage.object"
	// StorageBlock is a block volume attached to a machine.
	StorageBlock Category = "storage.block"
	// DatabasePostgreSQL is a PostgreSQL database run by the provider.
	DatabasePostgreSQL Category = "database.postgresql"
	// DatabaseMySQL is a MySQL database run by the provider.
	DatabaseMySQL Category = "database.mysql"
	// ServerlessFunction is code the provider runs on demand, per invocation.
	ServerlessFunction Category = "serverless.function"
	// MessagingQueue is a message queue the provider runs.
	MessagingQueue Category = "messaging.queue"
	// CacheRedis is an in-memory key-value store the provider runs that speaks
	// the Redis protocol: Redis OSS or its fork Valkey.
	CacheRedis Category = "cache.redis"
	// DatabaseMongoDB is a document database the provider runs that speaks the
	// MongoDB wire protocol.
	DatabaseMongoDB Category = "database.mongodb"
	// NetworkLoadBalancer is a load balancer the provider runs in front of a
	// set of backends, at layer 4 (TCP, and UDP where the capability udp says
	// so) or layer 7 (HTTP/HTTPS).
	NetworkLoadBalancer Category = "network.loadbalancer"
	// MessagingTopic is a publish-subscribe topic the provider runs: every
	// subscription gets its own copy of each message.
	MessagingTopic Category = "messaging.topic"
	// ServerlessContainer is a container image the provider runs and scales
	// with requests; it may scale to zero when idle.
	ServerlessContainer Category = "serverless.container"
	// RegistryContainer is a private registry of OCI container images the
	// provider runs.
	RegistryContainer Category = "registry.container"
	// StorageFile is a shared file system the provider runs, mounted by
	// several machines over NFS.
	StorageFile Category = "storage.file"
	// SearchOpenSearch is a search and analytics engine the provider runs that
	// speaks the OpenSearch REST API: OpenSearch, or Elasticsearch up to 7.10,
	// from which OpenSearch was forked.
	SearchOpenSearch Category = "search.opensearch"
	// MessagingKafka is a cluster of brokers the provider runs that speaks the
	// Apache Kafka protocol: a partitioned, replicated log whose consumers track
	// their own offsets.
	MessagingKafka Category = "messaging.kafka"
	// Unclassified marks a source resource that fits no other category. It is a
	// fact about the resource, not a fallback for unknown values, and no target
	// offering ever serves it.
	Unclassified Category = "unclassified"
)

// ErrUnknownCategory is returned for a value that is not a taxonomy category.
var ErrUnknownCategory = errors.New("unknown taxonomy category")

// categories lists every category, sorted by value. It is read-only.
var categories = []Category{
	CacheRedis,
	ComputeBareMetal,
	ComputeVM,
	DatabaseMongoDB,
	DatabaseMySQL,
	DatabasePostgreSQL,
	KubernetesManaged,
	MessagingKafka,
	MessagingQueue,
	MessagingTopic,
	NetworkLoadBalancer,
	RegistryContainer,
	SearchOpenSearch,
	ServerlessContainer,
	ServerlessFunction,
	StorageBlock,
	StorageFile,
	StorageObject,
	Unclassified,
}

// ParseCategory returns the category whose value is exactly s. It does not trim
// or fold case, and it never maps an unknown value to Unclassified: anything
// else is an error wrapping ErrUnknownCategory. The error quotes s with
// text.Quote, so it stays short however long s is.
func ParseCategory(s string) (Category, error) {
	c := Category(s)
	if !slices.Contains(categories, c) {
		return "", fmt.Errorf("%w %s", ErrUnknownCategory, text.Quote(s))
	}

	return c, nil
}

// Categories returns every category, sorted by value. The slice is a fresh copy
// on each call.
func Categories() []Category {
	return slices.Clone(categories)
}
