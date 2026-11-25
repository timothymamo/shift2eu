package taxonomy_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// The string values of the constants are the only names sources, targets and the
// inventory file format share, so they are pinned literally.
func TestCategoryValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  taxonomy.Category
		want string
	}{
		{name: "compute vm", got: taxonomy.ComputeVM, want: "compute.vm"},
		{name: "compute bare metal", got: taxonomy.ComputeBareMetal, want: "compute.baremetal"},
		{name: "kubernetes managed", got: taxonomy.KubernetesManaged, want: "kubernetes.managed"},
		{name: "object storage", got: taxonomy.StorageObject, want: "storage.object"},
		{name: "block storage", got: taxonomy.StorageBlock, want: "storage.block"},
		{name: "postgresql", got: taxonomy.DatabasePostgreSQL, want: "database.postgresql"},
		{name: "mysql", got: taxonomy.DatabaseMySQL, want: "database.mysql"},
		{name: "serverless function", got: taxonomy.ServerlessFunction, want: "serverless.function"},
		{name: "messaging queue", got: taxonomy.MessagingQueue, want: "messaging.queue"},
		// The seven categories PRD #552 adds.
		{name: "cache redis", got: taxonomy.CacheRedis, want: "cache.redis"},
		{name: "mongodb", got: taxonomy.DatabaseMongoDB, want: "database.mongodb"},
		{name: "network load balancer", got: taxonomy.NetworkLoadBalancer, want: "network.loadbalancer"},
		{name: "messaging topic", got: taxonomy.MessagingTopic, want: "messaging.topic"},
		{name: "serverless container", got: taxonomy.ServerlessContainer, want: "serverless.container"},
		{name: "registry container", got: taxonomy.RegistryContainer, want: "registry.container"},
		{name: "storage file", got: taxonomy.StorageFile, want: "storage.file"},
		{name: "unclassified", got: taxonomy.Unclassified, want: "unclassified"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, string(tc.got)); diff != "" {
				t.Errorf("category value mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want taxonomy.Category
	}{
		{name: "compute vm", in: "compute.vm", want: taxonomy.ComputeVM},
		{name: "compute bare metal", in: "compute.baremetal", want: taxonomy.ComputeBareMetal},
		{name: "kubernetes managed", in: "kubernetes.managed", want: taxonomy.KubernetesManaged},
		{name: "object storage", in: "storage.object", want: taxonomy.StorageObject},
		{name: "block storage", in: "storage.block", want: taxonomy.StorageBlock},
		{name: "postgresql", in: "database.postgresql", want: taxonomy.DatabasePostgreSQL},
		{name: "mysql", in: "database.mysql", want: taxonomy.DatabaseMySQL},
		{name: "serverless function", in: "serverless.function", want: taxonomy.ServerlessFunction},
		{name: "messaging queue", in: "messaging.queue", want: taxonomy.MessagingQueue},
		{name: "cache redis", in: "cache.redis", want: taxonomy.CacheRedis},
		{name: "mongodb", in: "database.mongodb", want: taxonomy.DatabaseMongoDB},
		{name: "network load balancer", in: "network.loadbalancer", want: taxonomy.NetworkLoadBalancer},
		{name: "messaging topic", in: "messaging.topic", want: taxonomy.MessagingTopic},
		{name: "serverless container", in: "serverless.container", want: taxonomy.ServerlessContainer},
		{name: "registry container", in: "registry.container", want: taxonomy.RegistryContainer},
		{name: "storage file", in: "storage.file", want: taxonomy.StorageFile},
		{name: "search opensearch", in: "search.opensearch", want: taxonomy.SearchOpenSearch},
		{name: "messaging kafka", in: "messaging.kafka", want: taxonomy.MessagingKafka},
		{name: "unclassified", in: "unclassified", want: taxonomy.Unclassified},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := taxonomy.ParseCategory(tc.in)
			if err != nil {
				t.Fatalf("ParseCategory(%q) returned unexpected error: %v", tc.in, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseCategory(%q) mismatch (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}

// Parsing is strict: no trimming, no case folding, no prefixes. Anything else
// must be rejected rather than silently becoming unclassified.
func TestParseCategoryRejectsUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{name: "typo", in: "compute.mv"},
		{name: "empty", in: ""},
		{name: "upper case", in: "COMPUTE.VM"},
		{name: "leading space", in: " compute.vm"},
		{name: "trailing space", in: "compute.vm "},
		{name: "prefix only", in: "compute"},
		// Memcached is not the Redis protocol and no target serves it, so it stays
		// unclassified: a category for it would map it direct to a cache that
		// cannot serve it (PRD #552, decided question 3).
		{name: "memcached", in: "cache.memcached"},
		{name: "cache without a kind", in: "cache"},
		{name: "load balancer under the compute family", in: "compute.loadbalancer"},
		// Elasticsearch is not OpenSearch: none of the three targets runs it, and
		// from 7.14 its own clients refuse a server that is not Elasticsearch
		// (PRD #571, Names). Kinesis speaks its own protocol and no target
		// equivalent exists (PRD #552, Left out).
		{name: "elasticsearch", in: "search.elasticsearch"},
		{name: "kinesis", in: "messaging.kinesis"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := taxonomy.ParseCategory(tc.in)
			if !errors.Is(err, taxonomy.ErrUnknownCategory) {
				t.Fatalf("ParseCategory(%q) error = %v, want an error wrapping ErrUnknownCategory", tc.in, err)
			}

			if tc.in == "" {
				// strings.Contains(msg, "") is always true, so there is nothing to pin here.
				return
			}

			if msg := err.Error(); !strings.Contains(msg, tc.in) {
				t.Errorf("ParseCategory(%q) error = %q, want it to mention %q", tc.in, msg, tc.in)
			}
		})
	}
}

// An unknown value comes from a file and can be megabytes long, so the error
// message must stay bounded while still wrapping the sentinel callers check.
func TestParseErrorsAreBounded(t *testing.T) {
	t.Parallel()

	// Shared read-only input: one allocation for both subtests.
	huge := strings.Repeat("x", 8<<20)

	tests := []struct {
		name    string
		parse   func(string) error
		wantErr error
	}{
		{
			name: "category",
			parse: func(s string) error {
				_, err := taxonomy.ParseCategory(s)

				return err
			},
			wantErr: taxonomy.ErrUnknownCategory,
		},
		{
			name: "capability",
			parse: func(s string) error {
				_, err := taxonomy.ParseCapability(s)

				return err
			},
			wantErr: taxonomy.ErrUnknownCapability,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.parse(huge)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("parse(8 MiB unknown value) error = %v, want an error wrapping %v", err, tc.wantErr)
			}

			if n := len(err.Error()); n >= 1024 {
				t.Errorf("len(error) = %d, want less than 1024", n)
			}
		})
	}
}

func TestCategories(t *testing.T) {
	t.Parallel()

	// The whole vocabulary, sorted by value: the ten categories of ADR 0011 and 0023,
	// the seven PRD #552 adds and the two PRD #571 adds. Unclassified is one of
	// them.
	want := []taxonomy.Category{
		taxonomy.CacheRedis,
		taxonomy.ComputeBareMetal,
		taxonomy.ComputeVM,
		taxonomy.DatabaseMongoDB,
		taxonomy.DatabaseMySQL,
		taxonomy.DatabasePostgreSQL,
		taxonomy.KubernetesManaged,
		taxonomy.MessagingKafka,
		taxonomy.MessagingQueue,
		taxonomy.MessagingTopic,
		taxonomy.NetworkLoadBalancer,
		taxonomy.RegistryContainer,
		taxonomy.SearchOpenSearch,
		taxonomy.ServerlessContainer,
		taxonomy.ServerlessFunction,
		taxonomy.StorageBlock,
		taxonomy.StorageFile,
		taxonomy.StorageObject,
		taxonomy.Unclassified,
	}

	if diff := cmp.Diff(want, taxonomy.Categories()); diff != "" {
		t.Fatalf("Categories() mismatch (-want +got):\n%s", diff)
	}

	// Every call returns a fresh slice: mutating one result must not leak into the next.
	got := taxonomy.Categories()
	got[0] = taxonomy.Category("tampered")
	got[1] = taxonomy.Category("tampered")

	if diff := cmp.Diff(want, taxonomy.Categories()); diff != "" {
		t.Errorf("Categories() after mutating a previous result (-want +got):\n%s", diff)
	}
}

func TestCapabilityValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  taxonomy.Capability
		want string
	}{
		{name: "ipv6", got: taxonomy.IPv6, want: "ipv6"},
		{name: "point in time restore", got: taxonomy.PointInTimeRestore, want: "point-in-time-restore"},
		{name: "object versioning", got: taxonomy.ObjectVersioning, want: "object-versioning"},
		{name: "node high availability", got: taxonomy.NodeHighAvailability, want: "node-high-availability"},
		{name: "zone high availability", got: taxonomy.ZoneHighAvailability, want: "zone-high-availability"},
		{name: "read replicas", got: taxonomy.ReadReplicas, want: "read-replicas"},
		{name: "autoscaling", got: taxonomy.Autoscaling, want: "autoscaling"},
		{name: "encryption at rest", got: taxonomy.EncryptionAtRest, want: "encryption-at-rest"},
		{name: "snapshots", got: taxonomy.Snapshots, want: "snapshots"},
		{name: "dead letter queue", got: taxonomy.DeadLetterQueue, want: "dead-letter-queue"},
		// The two capabilities PRD #552 adds (ADR 0046).
		{name: "sharding", got: taxonomy.Sharding, want: "sharding"},
		{name: "udp", got: taxonomy.UDP, want: "udp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, string(tc.got)); diff != "" {
				t.Errorf("capability value mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want taxonomy.Capability
	}{
		{name: "ipv6", in: "ipv6", want: taxonomy.IPv6},
		{name: "point in time restore", in: "point-in-time-restore", want: taxonomy.PointInTimeRestore},
		{name: "object versioning", in: "object-versioning", want: taxonomy.ObjectVersioning},
		{name: "node high availability", in: "node-high-availability", want: taxonomy.NodeHighAvailability},
		{name: "zone high availability", in: "zone-high-availability", want: taxonomy.ZoneHighAvailability},
		{name: "read replicas", in: "read-replicas", want: taxonomy.ReadReplicas},
		{name: "autoscaling", in: "autoscaling", want: taxonomy.Autoscaling},
		{name: "encryption at rest", in: "encryption-at-rest", want: taxonomy.EncryptionAtRest},
		{name: "snapshots", in: "snapshots", want: taxonomy.Snapshots},
		{name: "dead letter queue", in: "dead-letter-queue", want: taxonomy.DeadLetterQueue},
		{name: "sharding", in: "sharding", want: taxonomy.Sharding},
		{name: "udp", in: "udp", want: taxonomy.UDP},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := taxonomy.ParseCapability(tc.in)
			if err != nil {
				t.Fatalf("ParseCapability(%q) returned unexpected error: %v", tc.in, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseCapability(%q) mismatch (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}

func TestParseCapabilityRejectsUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{name: "upper case", in: "IPV6"},
		{name: "empty", in: ""},
		{name: "abbreviation", in: "pitr"},
		{name: "leading space", in: " ipv6"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := taxonomy.ParseCapability(tc.in)
			if !errors.Is(err, taxonomy.ErrUnknownCapability) {
				t.Fatalf("ParseCapability(%q) error = %v, want an error wrapping ErrUnknownCapability", tc.in, err)
			}

			if tc.in == "" {
				// strings.Contains(msg, "") is always true, so there is nothing to pin here.
				return
			}

			if msg := err.Error(); !strings.Contains(msg, tc.in) {
				t.Errorf("ParseCapability(%q) error = %q, want it to mention %q", tc.in, msg, tc.in)
			}
		})
	}
}

// ADR 0011 retires high-availability: it is refused by name, never read as one of
// the two levels that replace it, and the error names both replacements so a user
// whose file was written before the split knows what to change. Every value in an
// error is quoted with text.Quote, so each name is pinned in its quoted form.
func TestParseCapabilityRefusesRetiredHighAvailability(t *testing.T) {
	t.Parallel()

	const retired = "high-availability"

	got, err := taxonomy.ParseCapability(retired)
	if !errors.Is(err, taxonomy.ErrUnknownCapability) {
		t.Fatalf("ParseCapability(%q) = (%q, %v), want no capability and an error wrapping ErrUnknownCapability",
			retired, got, err)
	}

	if got != "" {
		t.Errorf("ParseCapability(%q) = %q, want no capability", retired, got)
	}

	// The replacements, each quoted: the retired value is a substring of
	// "node-high-availability", so the quotes are what keep the three assertions
	// apart.
	for _, want := range []string{retired, "node-high-availability", "zone-high-availability"} {
		if quoted := text.Quote(want); !strings.Contains(err.Error(), quoted) {
			t.Errorf("ParseCapability(%q) error = %q, want it to name %s", retired, err.Error(), quoted)
		}
	}

	for _, c := range taxonomy.Capabilities() {
		if string(c) == retired {
			t.Errorf("Capabilities() holds %q, want it retired from the vocabulary", retired)
		}
	}
}

// High availability has two scopes, and the only implication between
// capabilities is that zone level implies node level: a target that survives the
// loss of a whole zone survives the loss of one node too. The implication is
// data in the taxonomy, and catalog.New applies it to offerings only. Nothing
// implies a stronger level, and nothing else implies anything.
func TestCapabilityImplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   taxonomy.Capability
		want []taxonomy.Capability
	}{
		{
			name: "zone level implies node level",
			in:   taxonomy.ZoneHighAvailability,
			want: []taxonomy.Capability{taxonomy.NodeHighAvailability},
		},
		{name: "node level implies nothing", in: taxonomy.NodeHighAvailability, want: nil},
		{name: "unrelated capability implies nothing", in: taxonomy.IPv6, want: nil},
		{name: "capability outside the vocabulary implies nothing", in: taxonomy.Capability("x"), want: nil},
		{
			// A retired value is no longer a capability, so it implies no scoped
			// level either. It is written out as a string because the constant is gone.
			name: "retired high availability implies nothing",
			in:   taxonomy.Capability("high-availability"),
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, tc.in.Implies()); diff != "" {
				t.Errorf("Implies() of %q mismatch (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}

// The implication table is data a future entry can break silently, so its
// invariants are pinned over the whole vocabulary: every implied value is a
// capability of the vocabulary (it can be parsed back), the result is sorted by
// value as Implies promises, and no capability implies itself, which would make
// an offering satisfy a need out of nothing.
func TestCapabilityImplicationInvariants(t *testing.T) {
	t.Parallel()

	for _, c := range taxonomy.Capabilities() {
		t.Run(string(c), func(t *testing.T) {
			t.Parallel()

			implied := c.Implies()

			if !slices.IsSorted(implied) {
				t.Errorf("Implies() of %q = %v, want it sorted by value", c, implied)
			}

			for _, i := range implied {
				if _, err := taxonomy.ParseCapability(string(i)); err != nil {
					t.Errorf("Implies() of %q contains %q, which is not in the vocabulary: %v", c, i, err)
				}

				if i == c {
					t.Errorf("Implies() of %q contains itself", c)
				}
			}
		})
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	// The whole vocabulary, sorted by value: the ten capabilities of ADR 0011 and
	// the two PRD #552 adds.
	want := []taxonomy.Capability{
		taxonomy.Autoscaling,
		taxonomy.DeadLetterQueue,
		taxonomy.EncryptionAtRest,
		taxonomy.IPv6,
		taxonomy.NodeHighAvailability,
		taxonomy.ObjectVersioning,
		taxonomy.PointInTimeRestore,
		taxonomy.ReadReplicas,
		taxonomy.Sharding,
		taxonomy.Snapshots,
		taxonomy.UDP,
		taxonomy.ZoneHighAvailability,
	}

	if diff := cmp.Diff(want, taxonomy.Capabilities()); diff != "" {
		t.Fatalf("Capabilities() mismatch (-want +got):\n%s", diff)
	}

	got := taxonomy.Capabilities()
	got[0] = taxonomy.Capability("tampered")
	got[1] = taxonomy.Capability("tampered")

	if diff := cmp.Diff(want, taxonomy.Capabilities()); diff != "" {
		t.Errorf("Capabilities() after mutating a previous result (-want +got):\n%s", diff)
	}
}
