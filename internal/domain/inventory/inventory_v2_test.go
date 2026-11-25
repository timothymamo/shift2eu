// This file pins the inventory format version 2 fields the domain keeps on a
// resource - its size, usage profile, workload, relations and the source's own
// monthly cost - and every way inventory.New refuses them (PRD #190, milestone
// 1). It is part of the black-box package inventory_test and reuses that
// package's helpers.
//
// The shapes pinned here keep "not given" apart from zero: every optional number
// is a pointer, nil when the file did not give it, so a resource with
// storageGiB 0 is not a resource without a size.

package inventory_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// ptr returns a pointer to v, for a version 2 field that is given: the domain
// keeps nil, a field the file did not give, apart from the zero value.
func ptr[T any](v T) *T {
	return &v
}

// The two categories PRD #571 adds, as the values the taxonomy, the inventory
// file and the report share. They are written as values, not as the package's
// constants, so this file compiles while those constants do not exist yet.
const (
	searchCategory = taxonomy.Category("search.opensearch")
	kafkaCategory  = taxonomy.Category("messaging.kafka")
)

// vm returns a compute.vm resource with a valid ID and raw type: the base of
// the version 2 field tests.
func vm(id string) inventory.Resource {
	return inventory.Resource{
		ID:       id,
		RawType:  "AWS::EC2::Instance",
		Category: taxonomy.ComputeVM,
		Region:   "eu-west-1",
	}
}

// database returns a database.postgresql resource with a valid ID.
func database(id string) inventory.Resource {
	return inventory.Resource{
		ID:       id,
		RawType:  "AWS::RDS::DBInstance",
		Category: taxonomy.DatabasePostgreSQL,
		Region:   "eu-west-1",
	}
}

// withSize returns r with its size set.
func withSize(r inventory.Resource, s inventory.Size) inventory.Resource {
	r.Size = &s

	return r
}

// withUsage returns r with its usage profile set.
func withUsage(r inventory.Resource, u inventory.Usage) inventory.Resource {
	r.Usage = &u

	return r
}

// withWorkload returns r with its workload set.
func withWorkload(r inventory.Resource, workload string) inventory.Resource {
	r.Workload = workload

	return r
}

// withRelations returns r with its relations set.
func withRelations(r inventory.Resource, relations ...inventory.Relation) inventory.Resource {
	r.Relations = relations

	return r
}

// withCost returns r with the source's monthly cost set.
func withCost(r inventory.Resource, c inventory.Cost) inventory.Resource {
	r.MonthlyCost = &c

	return r
}

// withCategory returns r classified into c.
func withCategory(r inventory.Resource, c taxonomy.Category) inventory.Resource {
	r.Category = c

	return r
}

// TestNewKeepsVersion2Fields pins the round trip of the version 2 fields through
// New and Resources(): every field that is given comes back as it was given, and
// every optional number stays a pointer, so "not given" and zero stay apart.
func TestNewKeepsVersion2Fields(t *testing.T) {
	t.Parallel()

	// plain is a version 1 resource: no version 2 field at all.
	plain := vm("i-0123456789abcdef0")

	// everyField is a compute.vm with every size field, and every other version 2
	// field beside it, so no field of the resource is dropped on the way through.
	everyField := vm("i-0123456789abcdef0")
	everyField.Size = &inventory.Size{
		VCPUs:      ptr(2.0),
		MemoryGiB:  ptr(8.0),
		StorageGiB: ptr(100.0),
		Count:      ptr(1),
	}
	everyField.Usage = &inventory.Usage{HoursPerMonth: ptr(730.0)}
	everyField.Workload = "shop"
	everyField.MonthlyCost = &inventory.Cost{Amount: ptr(123.45), Currency: "USD"}

	// fractionalVCPUs is a shared core: vcpus is a number, not a count.
	fractionalVCPUs := vm("i-0123456789abcdef0")
	fractionalVCPUs.Size = &inventory.Size{VCPUs: ptr(0.5)}

	// zeroStorage is a size whose storage is explicitly 0 GiB, which is a value
	// and not an absent field.
	zeroStorage := vm("i-0123456789abcdef0")
	zeroStorage.Size = &inventory.Size{StorageGiB: ptr(0.0)}

	// zeroCost is a source cost of 0, in its own currency: a value too. It has
	// its own ID so it can share an inventory with zeroStorage.
	zeroCost := vm("i-0123456789abcdef1")
	zeroCost.MonthlyCost = &inventory.Cost{Amount: ptr(0.0), Currency: "EUR"}

	// usage is an hoursPerMonth of its own, and hoursPerMonth 744 is the whole of
	// a 31-day month.
	usage := vm("i-0123456789abcdef0")
	usage.Usage = &inventory.Usage{HoursPerMonth: ptr(744.0)}

	// workload is a name on its own; the free text is kept as given.
	workload := vm("i-0123456789abcdef0")
	workload.Workload = "shop eu-west"

	// replica is a database with a relation to another resource of the same
	// inventory.
	primary := database("db-primary")
	replica := database("db-replica")
	replica.Relations = []inventory.Relation{{Type: inventory.ReplicaOf, Target: "db-primary"}}

	tests := []struct {
		name      string
		resources []inventory.Resource
		// wantID is the resource whose round trip the case pins.
		wantID string
		// want is the resource Resources() must hold for wantID, field by field.
		want inventory.Resource
	}{
		{
			name:      "no version 2 field",
			resources: []inventory.Resource{plain},
			wantID:    plain.ID,
			want:      plain,
		},
		{
			name:      "every version 2 field",
			resources: []inventory.Resource{everyField},
			wantID:    everyField.ID,
			want:      everyField,
		},
		{
			name:      "fractional vcpus",
			resources: []inventory.Resource{fractionalVCPUs},
			wantID:    fractionalVCPUs.ID,
			want:      fractionalVCPUs,
		},
		{
			name:      "storage of zero GiB",
			resources: []inventory.Resource{zeroStorage},
			wantID:    zeroStorage.ID,
			want:      zeroStorage,
		},
		{
			name:      "usage of 744 hours a month",
			resources: []inventory.Resource{usage},
			wantID:    usage.ID,
			want:      usage,
		},
		{
			name:      "workload",
			resources: []inventory.Resource{workload},
			wantID:    workload.ID,
			want:      workload,
		},
		{
			name:      "a relation",
			resources: []inventory.Resource{primary, replica},
			wantID:    replica.ID,
			want:      replica,
		},
		{
			name:      "a cost of zero",
			resources: []inventory.Resource{zeroCost},
			wantID:    zeroCost.ID,
			want:      zeroCost,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := resourceOf(t, newInventory(t, "aws", tc.resources), tc.wantID)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Resources() of %s differs (-want +got):\n%s", tc.wantID, diff)
			}
		})
	}

	// A given field is a pointer, never zero: the two cases above carry a value of
	// zero, so a domain that stored a plain float would make them look absent.
	t.Run("a given zero stays given", func(t *testing.T) {
		t.Parallel()

		got := resourceOf(t, newInventory(t, "aws", []inventory.Resource{zeroStorage, zeroCost}), zeroStorage.ID)

		if got.Size == nil || got.Size.StorageGiB == nil {
			t.Fatalf("Resources() of %s holds Size.StorageGiB = %v, want the given 0", zeroStorage.ID, got.Size)
		}

		if *got.Size.StorageGiB != 0 {
			t.Errorf("Size.StorageGiB = %v, want 0", *got.Size.StorageGiB)
		}

		got = resourceOf(t, newInventory(t, "aws", []inventory.Resource{zeroCost}), zeroCost.ID)

		if got.MonthlyCost == nil || got.MonthlyCost.Amount == nil {
			t.Fatalf("Resources() of %s holds MonthlyCost = %v, want the given amount 0", zeroCost.ID, got.MonthlyCost)
		}

		if *got.MonthlyCost.Amount != 0 {
			t.Errorf("MonthlyCost.Amount = %v, want 0", *got.MonthlyCost.Amount)
		}
	})
}

// TestNewStoresNegativeZeroAsZero pins that a given -0 is kept as +0: a negative
// zero passes the "at least 0" bound, and a later renderer that prints a stored
// number must never print "-0.00". Everything else about the value stays: it is
// still a given field, not an absent one.
func TestNewStoresNegativeZeroAsZero(t *testing.T) {
	t.Parallel()

	minusZero := math.Copysign(0, -1)
	if !math.Signbit(minusZero) {
		t.Fatalf("math.Copysign(0, -1) = %v, want a negative zero", minusZero)
	}

	volume := withSize(vm("volume-example"), inventory.Size{StorageGiB: ptr(minusZero)})
	cost := withCost(vm("i-0123456789abcdef0"),
		inventory.Cost{Amount: ptr(minusZero), Currency: "EUR"})

	inv := newInventory(t, "aws", []inventory.Resource{volume, cost})

	sized := resourceOf(t, inv, volume.ID)
	if sized.Size == nil || sized.Size.StorageGiB == nil {
		t.Fatalf("Resources() of %s holds Size = %v, want the given -0 kept as a given 0",
			volume.ID, sized.Size)
	}

	if storage := *sized.Size.StorageGiB; storage != 0 || math.Signbit(storage) {
		t.Errorf("Size.StorageGiB = %v, want +0: a negative zero is stored as zero", storage)
	}

	priced := resourceOf(t, inv, cost.ID)
	if priced.MonthlyCost == nil || priced.MonthlyCost.Amount == nil {
		t.Fatalf("Resources() of %s holds MonthlyCost = %v, want the given -0 kept as a given 0",
			cost.ID, priced.MonthlyCost)
	}

	if amount := *priced.MonthlyCost.Amount; amount != 0 || math.Signbit(amount) {
		t.Errorf("MonthlyCost.Amount = %v, want +0: a negative zero is stored as zero", amount)
	}
}

// TestNewCanonicalisesAbsentVersion2Fields pins the other side of the pointer
// rule: an empty size object, an empty usage object and an empty relations list
// describe the same resource as leaving the key out, so callers never have to
// tell the two spellings apart. The reader relies on it: a file may write
// "size": {} or null and get the same inventory.
func TestNewCanonicalisesAbsentVersion2Fields(t *testing.T) {
	t.Parallel()

	want := vm("i-0123456789abcdef0")

	tests := []struct {
		name     string
		resource inventory.Resource
	}{
		{name: "no version 2 key", resource: vm("i-0123456789abcdef0")},
		{name: "empty size", resource: withSize(vm("i-0123456789abcdef0"), inventory.Size{})},
		{name: "empty usage", resource: withUsage(vm("i-0123456789abcdef0"), inventory.Usage{})},
		{name: "empty relations", resource: withRelations(vm("i-0123456789abcdef0"))},
		{name: "empty workload", resource: withWorkload(vm("i-0123456789abcdef0"), "")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newInventory(t, "aws", []inventory.Resource{tc.resource}).Resources()
			if len(got) != 1 {
				t.Fatalf("Resources() returned %d resources, want 1", len(got))
			}

			if diff := cmp.Diff(want, got[0]); diff != "" {
				t.Errorf("Resources()[0] differs (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNewDeduplicatesAndSortsRelations pins that relations are stored like
// capabilities: free of duplicates and sorted by type, then target, so
// everything derived from them is deterministic.
func TestNewDeduplicatesAndSortsRelations(t *testing.T) {
	t.Parallel()

	replica := withRelations(database("db-replica"),
		inventory.Relation{Type: inventory.ReplicaOf, Target: "db-primary"},
		inventory.Relation{Type: inventory.DependsOn, Target: "db-primary"},
		inventory.Relation{Type: inventory.AttachedTo, Target: "queue-main"},
		inventory.Relation{Type: inventory.DependsOn, Target: "db-primary"},
		inventory.Relation{Type: inventory.DependsOn, Target: "queue-main"},
	)

	want := []inventory.Relation{
		{Type: inventory.AttachedTo, Target: "queue-main"},
		{Type: inventory.DependsOn, Target: "db-primary"},
		{Type: inventory.DependsOn, Target: "queue-main"},
		{Type: inventory.ReplicaOf, Target: "db-primary"},
	}

	inv := newInventory(t, "aws", []inventory.Resource{
		database("db-primary"),
		withCategory(vm("queue-main"), taxonomy.MessagingQueue),
		replica,
	})

	if diff := cmp.Diff(want, resourceOf(t, inv, "db-replica").Relations); diff != "" {
		t.Errorf("Relations() are not de-duplicated and sorted by type then target (-want +got):\n%s", diff)
	}
}

// TestNewResolvesRelationTargets pins that a target is matched with the same
// comparison as a duplicate ID - runs of white space are one space - and stored
// as the target resource's own ID. Deduplication happens after resolving, so two
// spellings of one target are one relation.
func TestNewResolvesRelationTargets(t *testing.T) {
	t.Parallel()

	// The resource's own ID holds a run of two spaces.
	primary := database("db  primary")

	replica := withRelations(database("db-replica"),
		inventory.Relation{Type: inventory.ReplicaOf, Target: "db primary"},
		inventory.Relation{Type: inventory.ReplicaOf, Target: "db  primary"},
	)

	inv := newInventory(t, "aws", []inventory.Resource{primary, replica})

	want := []inventory.Relation{{Type: inventory.ReplicaOf, Target: "db  primary"}}
	if diff := cmp.Diff(want, resourceOf(t, inv, "db-replica").Relations); diff != "" {
		t.Errorf("Relations() hold the target as written, not as the target resource names it (-want +got):\n%s", diff)
	}
}

// TestNewKeepsCyclicRelations pins that relations may point at each other:
// milestone 1 checks the targets exist and are not the resource itself, not that
// the graph is acyclic.
func TestNewKeepsCyclicRelations(t *testing.T) {
	t.Parallel()

	first := withRelations(database("db-primary"),
		inventory.Relation{Type: inventory.DependsOn, Target: "db-replica"})

	second := withRelations(database("db-replica"),
		inventory.Relation{Type: inventory.DependsOn, Target: "db-primary"})

	newInventory(t, "aws", []inventory.Resource{first, second})
}

// TestNewAcceptsVersion2FieldsPerCategory pins the "yes" cells of the category
// table: every field the table allows on a category is accepted there. The "no"
// cells are pinned by TestNewRejectsVersion2Fields.
func TestNewAcceptsVersion2FieldsPerCategory(t *testing.T) {
	t.Parallel()

	// sized builds a resource of category c whose size uses exactly the fields
	// the table allows, with an hoursPerMonth where it allows one.
	sized := func(id string, c taxonomy.Category, size inventory.Size, hours *float64) inventory.Resource {
		r := withCategory(vm(id), c)
		r.Size = &size

		if hours != nil {
			r.Usage = &inventory.Usage{HoursPerMonth: hours}
		}

		return r
	}

	computeSize := inventory.Size{VCPUs: ptr(2.0), MemoryGiB: ptr(8.0), StorageGiB: ptr(100.0), Count: ptr(2)}

	tests := []struct {
		name     string
		resource inventory.Resource
	}{
		{
			name:     "compute.vm",
			resource: sized("i-0123456789abcdef0", taxonomy.ComputeVM, computeSize, ptr(730.0)),
		},
		{
			name:     "compute.baremetal",
			resource: sized("i-0123456789abcdef0", taxonomy.ComputeBareMetal, computeSize, ptr(730.0)),
		},
		{
			name:     "kubernetes.managed",
			resource: sized("cluster-example", taxonomy.KubernetesManaged, computeSize, ptr(730.0)),
		},
		{
			name:     "database.postgresql",
			resource: sized("db-example", taxonomy.DatabasePostgreSQL, computeSize, ptr(730.0)),
		},
		{
			name:     "database.mysql",
			resource: sized("db-example", taxonomy.DatabaseMySQL, computeSize, ptr(730.0)),
		},
		{
			name: "storage.block",
			resource: sized("volume-example", taxonomy.StorageBlock,
				inventory.Size{StorageGiB: ptr(500.0), Count: ptr(1)}, nil),
		},
		{
			name: "storage.object",
			resource: sized("bucket-example", taxonomy.StorageObject,
				inventory.Size{StorageGiB: ptr(2048.0)}, nil),
		},
		{
			name: "serverless.function",
			resource: sized("fn-example", taxonomy.ServerlessFunction,
				inventory.Size{MemoryGiB: ptr(0.5)}, nil),
		},
		{
			name:     "messaging.queue without a size",
			resource: withCategory(vm("queue-example"), taxonomy.MessagingQueue),
		},
		// The seven categories PRD #552 adds take the fields of ADR 0046: a cache
		// by memory and nodes, a MongoDB database exactly what PostgreSQL takes, a
		// file share by provisioned GiB, and the rest none at all.
		{
			name: "cache.redis",
			resource: sized("cache-example", taxonomy.CacheRedis,
				inventory.Size{MemoryGiB: ptr(8.0), Count: ptr(2)}, ptr(730.0)),
		},
		{
			name:     "database.mongodb",
			resource: sized("db-example", taxonomy.DatabaseMongoDB, computeSize, ptr(730.0)),
		},
		// The two categories PRD #571 adds take every size field the table knows,
		// as a database does: vCPUs, memory and storage of one node, the node count
		// and the hours a month (PRD #571, Estimates). Taking every field, they have
		// no "no" cell in TestNewRejectsVersion2FieldsPerCategory.
		{
			name:     "search.opensearch",
			resource: sized("search-example", searchCategory, computeSize, ptr(730.0)),
		},
		{
			name:     "messaging.kafka",
			resource: sized("kafka-example", kafkaCategory, computeSize, ptr(730.0)),
		},
		{
			name: "storage.file",
			resource: sized("share-example", taxonomy.StorageFile,
				inventory.Size{StorageGiB: ptr(500.0)}, nil),
		},
		{
			name:     "network.loadbalancer without a size",
			resource: withCategory(vm("lb-example"), taxonomy.NetworkLoadBalancer),
		},
		{
			name:     "messaging.topic without a size",
			resource: withCategory(vm("topic-example"), taxonomy.MessagingTopic),
		},
		{
			name:     "serverless.container without a size",
			resource: withCategory(vm("container-example"), taxonomy.ServerlessContainer),
		},
		{
			name:     "registry.container without a size",
			resource: withCategory(vm("registry-example"), taxonomy.RegistryContainer),
		},
		{
			name:     "unclassified without a size",
			resource: withCategory(vm("mystery-example"), taxonomy.Unclassified),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newInventory(t, "aws", []inventory.Resource{tc.resource})
		})
	}

	// A workload, relations and a source cost are allowed on every category,
	// unclassified included.
	t.Run("unclassified keeps workload, relations and cost", func(t *testing.T) {
		t.Parallel()

		mystery := withCost(withWorkload(withRelations(
			withCategory(vm("mystery-example"), taxonomy.Unclassified),
			inventory.Relation{Type: inventory.DependsOn, Target: "i-0123456789abcdef0"},
		), "legacy"), inventory.Cost{Amount: ptr(10.0), Currency: "EUR"})

		inv := newInventory(t, "aws", []inventory.Resource{vm("i-0123456789abcdef0"), mystery})

		got := resourceOf(t, inv, "mystery-example")
		if got.Workload != "legacy" {
			t.Errorf("Workload = %q, want %q", got.Workload, "legacy")
		}

		if diff := cmp.Diff([]inventory.Relation{{Type: inventory.DependsOn, Target: "i-0123456789abcdef0"}}, got.Relations); diff != "" {
			t.Errorf("Relations mismatch (-want +got):\n%s", diff)
		}

		if got.MonthlyCost == nil || got.MonthlyCost.Currency != "EUR" {
			t.Errorf("MonthlyCost = %v, want an EUR cost", got.MonthlyCost)
		}
	})
}

// TestNewRejectsVersion2Fields pins every way the version 2 fields are refused:
// the sentinel the error wraps and the path the error starts with, in file order,
// before the resources are sorted. Values are quoted with text.Quote, and the
// message stays on one line, so the CLI can print it as it is.
func TestNewRejectsVersion2Fields(t *testing.T) {
	t.Parallel()

	// second puts r at index 1 of the inventory, after a resource whose ID sorts
	// later: an error must name resources[1], the file's index, not
	// resources[0], the sorted one. The first resource is a valid relation
	// target.
	second := func(r inventory.Resource) []inventory.Resource {
		return []inventory.Resource{vm("zz-first"), r}
	}

	tests := []struct {
		name      string
		resources []inventory.Resource
		wantErr   error
		// wantPath is the exact prefix of the error: the resource's index in file
		// order, then the field.
		wantPath string
		// wantValue, when set, is a value the error must quote with text.Quote.
		wantValue string
	}{
		{
			name: "relation to a resource that is not in the inventory",
			resources: second(withRelations(database("aa-replica"),
				inventory.Relation{Type: inventory.ReplicaOf, Target: "db-missing"})),
			wantErr:   inventory.ErrUnknownRelationTarget,
			wantPath:  "resources[1].relations[0].target",
			wantValue: "db-missing",
		},
		{
			name: "relation to the resource itself",
			resources: second(withRelations(database("aa-replica"),
				inventory.Relation{Type: inventory.ReplicaOf, Target: "aa-replica"})),
			wantErr:   inventory.ErrSelfRelation,
			wantPath:  "resources[1].relations[0].target",
			wantValue: "aa-replica",
		},
		{
			name: "unknown relation type",
			resources: second(withRelations(database("aa-replica"),
				inventory.Relation{Type: inventory.RelationType("replica"), Target: "zz-first"})),
			wantErr:   inventory.ErrUnknownRelationType,
			wantPath:  "resources[1].relations[0].type",
			wantValue: "replica",
		},
		{
			// The index in the path is the relation's position as the file lists it.
			name: "unknown relation type on the second relation",
			resources: second(withRelations(database("aa-replica"),
				inventory.Relation{Type: inventory.ReplicaOf, Target: "zz-first"},
				inventory.Relation{Type: inventory.RelationType("replica"), Target: "zz-first"})),
			wantErr:   inventory.ErrUnknownRelationType,
			wantPath:  "resources[1].relations[1].type",
			wantValue: "replica",
		},
		{
			name: "empty relation type",
			resources: second(withRelations(database("aa-replica"),
				inventory.Relation{Target: "zz-first"})),
			wantErr:  inventory.ErrUnknownRelationType,
			wantPath: "resources[1].relations[0].type",
		},
		{
			name: "empty relation target",
			resources: second(withRelations(database("aa-replica"),
				inventory.Relation{Type: inventory.AttachedTo})),
			wantErr:  inventory.ErrUnknownRelationTarget,
			wantPath: "resources[1].relations[0].target",
		},
		{
			name:      "vcpus of zero",
			resources: second(withSize(vm("aa-second"), inventory.Size{VCPUs: ptr(0.0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.vcpus",
		},
		{
			name:      "negative vcpus",
			resources: second(withSize(vm("aa-second"), inventory.Size{VCPUs: ptr(-1.0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.vcpus",
		},
		{
			name:      "vcpus above the maximum",
			resources: second(withSize(vm("aa-second"), inventory.Size{VCPUs: ptr(1000001.0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.vcpus",
		},
		{
			name:      "memoryGiB of zero",
			resources: second(withSize(vm("aa-second"), inventory.Size{MemoryGiB: ptr(0.0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.memoryGiB",
		},
		{
			name:      "negative memoryGiB",
			resources: second(withSize(vm("aa-second"), inventory.Size{MemoryGiB: ptr(-8.0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.memoryGiB",
		},
		{
			name:      "memoryGiB above the maximum",
			resources: second(withSize(vm("aa-second"), inventory.Size{MemoryGiB: ptr(1000001.0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.memoryGiB",
		},
		{
			name:      "negative storageGiB",
			resources: second(withSize(vm("aa-second"), inventory.Size{StorageGiB: ptr(-1.0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.storageGiB",
		},
		{
			name:      "storageGiB above the maximum",
			resources: second(withSize(vm("aa-second"), inventory.Size{StorageGiB: ptr(1000000001.0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.storageGiB",
		},
		{
			name:      "count of zero",
			resources: second(withSize(vm("aa-second"), inventory.Size{Count: ptr(0)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.count",
		},
		{
			name:      "negative count",
			resources: second(withSize(vm("aa-second"), inventory.Size{Count: ptr(-1)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.count",
		},
		{
			name:      "count above the maximum",
			resources: second(withSize(vm("aa-second"), inventory.Size{Count: ptr(100001)})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.count",
		},
		{
			name:      "hoursPerMonth of zero",
			resources: second(withUsage(vm("aa-second"), inventory.Usage{HoursPerMonth: ptr(0.0)})),
			wantErr:   inventory.ErrInvalidUsage,
			wantPath:  "resources[1].usage.hoursPerMonth",
		},
		{
			name:      "hoursPerMonth above the longest month",
			resources: second(withUsage(vm("aa-second"), inventory.Usage{HoursPerMonth: ptr(745.0)})),
			wantErr:   inventory.ErrInvalidUsage,
			wantPath:  "resources[1].usage.hoursPerMonth",
		},
		{
			name: "negative cost",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(-0.01), Currency: "EUR"})),
			wantErr:  inventory.ErrInvalidCost,
			wantPath: "resources[1].monthlyCost.amount",
		},
		{
			name: "cost above the maximum",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(1000000000001.0), Currency: "EUR"})),
			wantErr:  inventory.ErrInvalidCost,
			wantPath: "resources[1].monthlyCost.amount",
		},
		{
			name: "cost without an amount",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Currency: "EUR"})),
			wantErr:  inventory.ErrInvalidCost,
			wantPath: "resources[1].monthlyCost.amount",
		},
		{
			name: "currency in lower case",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(1.0), Currency: "usd"})),
			wantErr:   inventory.ErrInvalidCost,
			wantPath:  "resources[1].monthlyCost.currency",
			wantValue: "usd",
		},
		{
			name: "currency in upper case words",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(1.0), Currency: "EURO"})),
			wantErr:   inventory.ErrInvalidCost,
			wantPath:  "resources[1].monthlyCost.currency",
			wantValue: "EURO",
		},
		{
			name: "currency with a digit",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(1.0), Currency: "E1R"})),
			wantErr:   inventory.ErrInvalidCost,
			wantPath:  "resources[1].monthlyCost.currency",
			wantValue: "E1R",
		},
		{
			name: "empty currency",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(1.0)})),
			wantErr:  inventory.ErrInvalidCost,
			wantPath: "resources[1].monthlyCost.currency",
		},
		{
			name:      "workload with a control character",
			resources: second(withWorkload(vm("aa-second"), "\x1b[2J")),
			wantErr:   text.ErrInvalid,
			wantPath:  "resources[1].workload",
		},
		{
			name:      "workload with leading white space",
			resources: second(withWorkload(vm("aa-second"), " shop")),
			wantErr:   text.ErrInvalid,
			wantPath:  "resources[1].workload",
		},
		{
			name:      "workload with trailing white space",
			resources: second(withWorkload(vm("aa-second"), "shop ")),
			wantErr:   text.ErrInvalid,
			wantPath:  "resources[1].workload",
		},
		// The JSON reader cannot produce NaN or an infinity - 1e999 is refused
		// while the document is walked - but a source adapter calls New with the
		// numbers its SDK reports. The range checks are written so that NaN, which
		// compares false with everything, and both infinities are refused, and
		// these rows keep them that way: a rewrite as the natural-looking
		// "n <= 0 || n > limit" would let NaN through silently.
		{
			name:      "vcpus of NaN",
			resources: second(withSize(vm("aa-second"), inventory.Size{VCPUs: ptr(math.NaN())})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.vcpus",
		},
		{
			name:      "vcpus of positive infinity",
			resources: second(withSize(vm("aa-second"), inventory.Size{VCPUs: ptr(math.Inf(1))})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.vcpus",
		},
		{
			name:      "vcpus of negative infinity",
			resources: second(withSize(vm("aa-second"), inventory.Size{VCPUs: ptr(math.Inf(-1))})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.vcpus",
		},
		{
			name:      "memoryGiB of NaN",
			resources: second(withSize(vm("aa-second"), inventory.Size{MemoryGiB: ptr(math.NaN())})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.memoryGiB",
		},
		{
			name:      "memoryGiB of positive infinity",
			resources: second(withSize(vm("aa-second"), inventory.Size{MemoryGiB: ptr(math.Inf(1))})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.memoryGiB",
		},
		{
			name:      "memoryGiB of negative infinity",
			resources: second(withSize(vm("aa-second"), inventory.Size{MemoryGiB: ptr(math.Inf(-1))})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.memoryGiB",
		},
		{
			name:      "storageGiB of NaN",
			resources: second(withSize(vm("aa-second"), inventory.Size{StorageGiB: ptr(math.NaN())})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.storageGiB",
		},
		{
			name:      "storageGiB of positive infinity",
			resources: second(withSize(vm("aa-second"), inventory.Size{StorageGiB: ptr(math.Inf(1))})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.storageGiB",
		},
		{
			name:      "storageGiB of negative infinity",
			resources: second(withSize(vm("aa-second"), inventory.Size{StorageGiB: ptr(math.Inf(-1))})),
			wantErr:   inventory.ErrInvalidSize,
			wantPath:  "resources[1].size.storageGiB",
		},
		{
			name:      "hoursPerMonth of NaN",
			resources: second(withUsage(vm("aa-second"), inventory.Usage{HoursPerMonth: ptr(math.NaN())})),
			wantErr:   inventory.ErrInvalidUsage,
			wantPath:  "resources[1].usage.hoursPerMonth",
		},
		{
			name:      "hoursPerMonth of positive infinity",
			resources: second(withUsage(vm("aa-second"), inventory.Usage{HoursPerMonth: ptr(math.Inf(1))})),
			wantErr:   inventory.ErrInvalidUsage,
			wantPath:  "resources[1].usage.hoursPerMonth",
		},
		{
			name:      "hoursPerMonth of negative infinity",
			resources: second(withUsage(vm("aa-second"), inventory.Usage{HoursPerMonth: ptr(math.Inf(-1))})),
			wantErr:   inventory.ErrInvalidUsage,
			wantPath:  "resources[1].usage.hoursPerMonth",
		},
		{
			name: "cost amount of NaN",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(math.NaN()), Currency: "EUR"})),
			wantErr:  inventory.ErrInvalidCost,
			wantPath: "resources[1].monthlyCost.amount",
		},
		{
			name: "cost amount of positive infinity",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(math.Inf(1)), Currency: "EUR"})),
			wantErr:  inventory.ErrInvalidCost,
			wantPath: "resources[1].monthlyCost.amount",
		},
		{
			name: "cost amount of negative infinity",
			resources: second(withCost(vm("aa-second"),
				inventory.Cost{Amount: ptr(math.Inf(-1)), Currency: "EUR"})),
			wantErr:  inventory.ErrInvalidCost,
			wantPath: "resources[1].monthlyCost.amount",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := inventory.New("aws", tc.resources)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New() error = %v, want an error wrapping %v", err, tc.wantErr)
			}

			msg := err.Error()
			if !strings.HasPrefix(msg, tc.wantPath) {
				t.Errorf("New() error = %q, want it to start with the path %q", msg, tc.wantPath)
			}

			if strings.ContainsAny(msg, "\r\n") {
				t.Errorf("New() error = %q, want a single line", msg)
			}

			if tc.wantValue != "" {
				if quoted := text.Quote(tc.wantValue); !strings.Contains(msg, quoted) {
					t.Errorf("New() error = %q, want it to quote the value as %s", msg, quoted)
				}
			}
		})
	}
}

// TestNewRejectsVersion2FieldsPerCategory pins the "no" cells of the category
// table: a size or usage field the category does not take is an error, whatever
// its value.
func TestNewRejectsVersion2FieldsPerCategory(t *testing.T) {
	t.Parallel()

	// second puts r at index 1, after a resource whose ID sorts later.
	second := func(r inventory.Resource) []inventory.Resource {
		return []inventory.Resource{vm("zz-first"), r}
	}

	// sized builds a resource of category c with size and usage as given.
	sized := func(id string, c taxonomy.Category, size inventory.Size, usage *inventory.Usage) inventory.Resource {
		r := withCategory(vm(id), c)
		r.Size = &size
		r.Usage = usage

		return r
	}

	tests := []struct {
		name      string
		resources []inventory.Resource
		wantPath  string
	}{
		{
			name: "vcpus on storage.object",
			resources: second(sized("aa-bucket", taxonomy.StorageObject,
				inventory.Size{VCPUs: ptr(2.0)}, nil)),
			wantPath: "resources[1].size.vcpus",
		},
		{
			name: "memoryGiB on storage.object",
			resources: second(sized("aa-bucket", taxonomy.StorageObject,
				inventory.Size{MemoryGiB: ptr(8.0)}, nil)),
			wantPath: "resources[1].size.memoryGiB",
		},
		{
			name: "count on storage.object",
			resources: second(sized("aa-bucket", taxonomy.StorageObject,
				inventory.Size{Count: ptr(1)}, nil)),
			wantPath: "resources[1].size.count",
		},
		{
			name: "hoursPerMonth on storage.object",
			resources: second(sized("aa-bucket", taxonomy.StorageObject,
				inventory.Size{StorageGiB: ptr(2048.0)},
				&inventory.Usage{HoursPerMonth: ptr(730.0)})),
			wantPath: "resources[1].usage.hoursPerMonth",
		},
		{
			name: "vcpus on storage.block",
			resources: second(sized("aa-volume", taxonomy.StorageBlock,
				inventory.Size{VCPUs: ptr(2.0)}, nil)),
			wantPath: "resources[1].size.vcpus",
		},
		{
			name: "memoryGiB on storage.block",
			resources: second(sized("aa-volume", taxonomy.StorageBlock,
				inventory.Size{MemoryGiB: ptr(8.0)}, nil)),
			wantPath: "resources[1].size.memoryGiB",
		},
		{
			name: "hoursPerMonth on storage.block",
			resources: second(sized("aa-volume", taxonomy.StorageBlock,
				inventory.Size{StorageGiB: ptr(500.0)},
				&inventory.Usage{HoursPerMonth: ptr(730.0)})),
			wantPath: "resources[1].usage.hoursPerMonth",
		},
		{
			name: "vcpus on messaging.queue",
			resources: second(sized("aa-queue", taxonomy.MessagingQueue,
				inventory.Size{VCPUs: ptr(2.0)}, nil)),
			wantPath: "resources[1].size.vcpus",
		},
		{
			name: "storageGiB on messaging.queue",
			resources: second(sized("aa-queue", taxonomy.MessagingQueue,
				inventory.Size{StorageGiB: ptr(1.0)}, nil)),
			wantPath: "resources[1].size.storageGiB",
		},
		{
			name: "hoursPerMonth on messaging.queue",
			resources: second(sized("aa-queue", taxonomy.MessagingQueue,
				inventory.Size{}, &inventory.Usage{HoursPerMonth: ptr(730.0)})),
			wantPath: "resources[1].usage.hoursPerMonth",
		},
		{
			name: "vcpus on unclassified",
			resources: second(sized("aa-mystery", taxonomy.Unclassified,
				inventory.Size{VCPUs: ptr(2.0)}, nil)),
			wantPath: "resources[1].size.vcpus",
		},
		// A cache is sized by memory and nodes only: its vCPUs are rarely known
		// and are never matched on (ADR 0046), and its storage is the provider's.
		{
			name: "vcpus on cache.redis",
			resources: second(sized("aa-cache", taxonomy.CacheRedis,
				inventory.Size{VCPUs: ptr(2.0)}, nil)),
			wantPath: "resources[1].size.vcpus",
		},
		{
			name: "storageGiB on cache.redis",
			resources: second(sized("aa-cache", taxonomy.CacheRedis,
				inventory.Size{StorageGiB: ptr(100.0)}, nil)),
			wantPath: "resources[1].size.storageGiB",
		},
		// A file share is billed on its provisioned GiB, once: no count and no
		// memory.
		{
			name: "count on storage.file",
			resources: second(sized("aa-share", taxonomy.StorageFile,
				inventory.Size{StorageGiB: ptr(500.0), Count: ptr(1)}, nil)),
			wantPath: "resources[1].size.count",
		},
		{
			name: "memoryGiB on storage.file",
			resources: second(sized("aa-share", taxonomy.StorageFile,
				inventory.Size{MemoryGiB: ptr(8.0)}, nil)),
			wantPath: "resources[1].size.memoryGiB",
		},
		{
			name: "hoursPerMonth on storage.file",
			resources: second(sized("aa-share", taxonomy.StorageFile,
				inventory.Size{StorageGiB: ptr(500.0)}, &inventory.Usage{HoursPerMonth: ptr(730.0)})),
			wantPath: "resources[1].usage.hoursPerMonth",
		},
		{
			name: "storageGiB on network.loadbalancer",
			resources: second(sized("aa-lb", taxonomy.NetworkLoadBalancer,
				inventory.Size{StorageGiB: ptr(1.0)}, nil)),
			wantPath: "resources[1].size.storageGiB",
		},
		{
			name: "memoryGiB on network.loadbalancer",
			resources: second(sized("aa-lb", taxonomy.NetworkLoadBalancer,
				inventory.Size{MemoryGiB: ptr(1.0)}, nil)),
			wantPath: "resources[1].size.memoryGiB",
		},
		{
			name: "vcpus on messaging.topic",
			resources: second(sized("aa-topic", taxonomy.MessagingTopic,
				inventory.Size{VCPUs: ptr(2.0)}, nil)),
			wantPath: "resources[1].size.vcpus",
		},
		{
			name: "storageGiB on messaging.topic",
			resources: second(sized("aa-topic", taxonomy.MessagingTopic,
				inventory.Size{StorageGiB: ptr(1.0)}, nil)),
			wantPath: "resources[1].size.storageGiB",
		},
		{
			name: "hoursPerMonth on messaging.topic",
			resources: second(sized("aa-topic", taxonomy.MessagingTopic,
				inventory.Size{}, &inventory.Usage{HoursPerMonth: ptr(730.0)})),
			wantPath: "resources[1].usage.hoursPerMonth",
		},
		{
			name: "memoryGiB on serverless.container",
			resources: second(sized("aa-container", taxonomy.ServerlessContainer,
				inventory.Size{MemoryGiB: ptr(2.0)}, nil)),
			wantPath: "resources[1].size.memoryGiB",
		},
		{
			name: "count on registry.container",
			resources: second(sized("aa-registry", taxonomy.RegistryContainer,
				inventory.Size{Count: ptr(1)}, nil)),
			wantPath: "resources[1].size.count",
		},
		{
			name: "count on unclassified",
			resources: second(sized("aa-mystery", taxonomy.Unclassified,
				inventory.Size{Count: ptr(1)}, nil)),
			wantPath: "resources[1].size.count",
		},
		{
			name: "hoursPerMonth on unclassified",
			resources: second(sized("aa-mystery", taxonomy.Unclassified,
				inventory.Size{}, &inventory.Usage{HoursPerMonth: ptr(730.0)})),
			wantPath: "resources[1].usage.hoursPerMonth",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := inventory.New("aws", tc.resources)
			if !errors.Is(err, inventory.ErrFieldNotForCategory) {
				t.Fatalf("New() error = %v, want an error wrapping %v", err, inventory.ErrFieldNotForCategory)
			}

			if msg := err.Error(); !strings.HasPrefix(msg, tc.wantPath) {
				t.Errorf("New() error = %q, want it to start with the path %q", msg, tc.wantPath)
			}
		})
	}
}

// TestParseRelationType pins the closed relation vocabulary: exactly four
// values, matched character for character, with a parse function and a sentinel
// of its own, like the taxonomy vocabularies.
func TestParseRelationType(t *testing.T) {
	t.Parallel()

	accepted := []struct {
		name string
		in   string
		want inventory.RelationType
	}{
		{name: "replica-of", in: "replica-of", want: inventory.ReplicaOf},
		{name: "dead-letter-queue-of", in: "dead-letter-queue-of", want: inventory.DeadLetterQueueOf},
		{name: "attached-to", in: "attached-to", want: inventory.AttachedTo},
		{name: "depends-on", in: "depends-on", want: inventory.DependsOn},
	}

	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := inventory.ParseRelationType(tc.in)
			if err != nil {
				t.Fatalf("ParseRelationType(%q) returned unexpected error: %v", tc.in, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseRelationType(%q) mismatch (-want +got):\n%s", tc.in, diff)
			}
		})
	}

	refused := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "unknown", in: "replica"},
		{name: "capitalised", in: "Replica-Of"},
		{name: "upper case", in: "REPLICA-OF"},
		{name: "padded", in: " replica-of"},
		{name: "a synonym", in: "replicated-from"},
	}

	for _, tc := range refused {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := inventory.ParseRelationType(tc.in)
			if !errors.Is(err, inventory.ErrUnknownRelationType) {
				t.Fatalf("ParseRelationType(%q) error = %v, want an error wrapping ErrUnknownRelationType", tc.in, err)
			}

			if got != "" {
				t.Errorf("ParseRelationType(%q) = %q, want no relation type", tc.in, got)
			}

			if tc.in != "" {
				if quoted := text.Quote(tc.in); !strings.Contains(err.Error(), quoted) {
					t.Errorf("ParseRelationType(%q) error = %q, want it to quote the value as %s", tc.in, err, quoted)
				}
			}
		})
	}
}

// TestResourcesDoesNotAliasVersion2Fields pins that Resources() hands out a deep
// copy of the version 2 fields too: changing a returned size, usage, cost,
// workload or relation never changes the inventory, and neither does changing
// the input New was given.
func TestResourcesDoesNotAliasVersion2Fields(t *testing.T) {
	t.Parallel()

	// wanted returns a fresh inventory holding one resource with every version 2
	// field, so a comparison after a mutation has something untampered to show.
	wanted := func() []inventory.Resource {
		r := database("db-replica")
		r.Size = &inventory.Size{VCPUs: ptr(2.0), MemoryGiB: ptr(8.0), StorageGiB: ptr(100.0), Count: ptr(1)}
		r.Usage = &inventory.Usage{HoursPerMonth: ptr(730.0)}
		r.Workload = "shop"
		r.Relations = []inventory.Relation{{Type: inventory.ReplicaOf, Target: "db-primary"}}
		r.MonthlyCost = &inventory.Cost{Amount: ptr(123.45), Currency: "USD"}

		return []inventory.Resource{database("db-primary"), r}
	}

	input := wanted()
	inv := newInventory(t, "aws", input)

	got := inv.Resources()
	replica := resourceOf(t, inv, "db-replica")

	// Mutate everything the returned copies hold, including the values the
	// pointers point at.
	for i := range got {
		if got[i].ID != "db-replica" {
			continue
		}

		got[i].Workload = "tampered"
		*got[i].Size.VCPUs = 999
		*got[i].Usage.HoursPerMonth = 1
		*got[i].MonthlyCost.Amount = 999
		got[i].MonthlyCost.Currency = "XXX"
		got[i].Relations[0] = inventory.Relation{Type: inventory.AttachedTo, Target: "tampered"}
	}

	if diff := cmp.Diff(wanted(), inv.Resources()); diff != "" {
		t.Errorf("Resources() changed after mutating a previous result (-want +got):\n%s", diff)
	}

	if *replica.Size.VCPUs != 2 {
		t.Errorf("Size.VCPUs = %v after mutating a returned size, want 2", *replica.Size.VCPUs)
	}

	// Mutating the input New was given must not reach the inventory either.
	for i := range input {
		if input[i].ID != "db-replica" {
			continue
		}

		input[i].Workload = "tampered"
		*input[i].Size.VCPUs = 999
		*input[i].Usage.HoursPerMonth = 1
		*input[i].MonthlyCost.Amount = 999
		input[i].Relations[0].Target = "tampered"
	}

	if diff := cmp.Diff(wanted(), inv.Resources()); diff != "" {
		t.Errorf("Resources() changed after mutating the input slice (-want +got):\n%s", diff)
	}
}

// resourceOf returns the resource with id from inv, failing the test if the
// inventory does not hold it.
func resourceOf(t *testing.T, inv inventory.Inventory, id string) inventory.Resource {
	t.Helper()

	for _, r := range inv.Resources() {
		if r.ID == id {
			return r
		}
	}

	t.Fatalf("Resources() does not hold %q; it holds %v", id, resourceIDs(inv.Resources()))

	return inventory.Resource{}
}
