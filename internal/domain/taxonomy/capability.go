package taxonomy

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// Capability is a feature a source resource depends on or a target offering
// provides, such as ipv6. Its string value is shared by sources, targets and the
// inventory file format.
type Capability string

// The capability vocabulary.
const (
	// IPv6 is IPv6 connectivity in both directions: the resource is reachable
	// over IPv6 (inbound) and can reach IPv6 destinations (outbound).
	IPv6 Capability = "ipv6"
	// PointInTimeRestore is restoring data to any moment within a retention window.
	PointInTimeRestore Capability = "point-in-time-restore"
	// ObjectVersioning is keeping earlier versions of overwritten or deleted objects.
	ObjectVersioning Capability = "object-versioning"
	// NodeHighAvailability is the resource keeping serving through the loss of
	// one node, server or host, without manual failover.
	NodeHighAvailability Capability = "node-high-availability"
	// ZoneHighAvailability is the resource keeping serving through the loss of a
	// whole availability zone or data centre, without manual failover. It
	// implies NodeHighAvailability.
	ZoneHighAvailability Capability = "zone-high-availability"
	// ReadReplicas is serving reads from replicas of a primary database.
	ReadReplicas Capability = "read-replicas"
	// Autoscaling is adding and removing capacity automatically with load.
	Autoscaling Capability = "autoscaling"
	// EncryptionAtRest is encrypting stored data.
	EncryptionAtRest Capability = "encryption-at-rest"
	// Snapshots is taking point-in-time copies of a volume, instance, file
	// share or data set (such as a cache's RDB snapshot).
	Snapshots Capability = "snapshots"
	// DeadLetterQueue is setting aside messages that repeatedly fail processing.
	DeadLetterQueue Capability = "dead-letter-queue"
	// Sharding is data partitioned across several primary nodes that clients
	// address, such as a Redis Cluster.
	Sharding Capability = "sharding"
	// UDP is forwarding UDP datagrams, as a layer 4 load balancer may.
	UDP Capability = "udp"
)

// ErrUnknownCapability is returned for a value that is not in the capability
// vocabulary.
var ErrUnknownCapability = errors.New("unknown capability")

// retired maps a value that ADR 0011 removed from the vocabulary to the
// capabilities that replace it, so the error for it can say what to write
// instead. It is read-only.
var retired = map[Capability][]Capability{
	"high-availability": {NodeHighAvailability, ZoneHighAvailability},
}

// capabilities lists every capability, sorted by value. It is read-only.
var capabilities = []Capability{
	Autoscaling,
	DeadLetterQueue,
	EncryptionAtRest,
	IPv6,
	NodeHighAvailability,
	ObjectVersioning,
	PointInTimeRestore,
	ReadReplicas,
	Sharding,
	Snapshots,
	UDP,
	ZoneHighAvailability,
}

// implications maps a capability to the weaker capabilities it implies: an
// offering that provides the key also provides every value. It is read-only.
var implications = map[Capability][]Capability{
	ZoneHighAvailability: {NodeHighAvailability},
}

// ParseCapability returns the capability whose value is exactly s. It does not
// trim or fold case: anything else is an error wrapping ErrUnknownCapability.
// The error quotes s with text.Quote, so it stays short however long s is. A
// retired value is refused too, and its error names the capabilities that
// replace it.
func ParseCapability(s string) (Capability, error) {
	if replacements, ok := retired[Capability(s)]; ok {
		quoted := make([]string, len(replacements))
		for i, r := range replacements {
			quoted[i] = text.Quote(string(r))
		}

		return "", fmt.Errorf("%w %s: retired, use %s instead",
			ErrUnknownCapability, text.Quote(s), strings.Join(quoted, " or "))
	}

	c := Capability(s)
	if !slices.Contains(capabilities, c) {
		return "", fmt.Errorf("%w %s", ErrUnknownCapability, text.Quote(s))
	}

	return c, nil
}

// Capabilities returns the whole capability vocabulary, sorted by value. The
// slice is a fresh copy on each call.
func Capabilities() []Capability {
	return slices.Clone(capabilities)
}

// Implies returns the capabilities that c directly implies, sorted by value, or
// nil when it implies none (including for a value outside the vocabulary). The
// slice is a fresh copy on each call.
func (c Capability) Implies() []Capability {
	return slices.Clone(implications[c])
}
