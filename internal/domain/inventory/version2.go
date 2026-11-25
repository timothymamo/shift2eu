package inventory

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// This file holds the fields inventory format version 2 adds to a resource
// (PRD #190, ADR 0025): its size, usage profile, workload, relations and the
// source's own monthly cost, and how New checks them.

// Size is how big a resource is. Every field is optional: nil is "not given",
// which is not 0. Which fields a resource may give depends on its category (see
// New). Later milestones read a count that is not given as 1.
type Size struct {
	// VCPUs is the number of virtual CPUs, per instance or node. Fractions are
	// allowed, for shared cores: more than 0, at most 1,000,000.
	VCPUs *float64
	// MemoryGiB is the memory in GiB, per instance, node or function: more than
	// 0, at most 1,000,000.
	MemoryGiB *float64
	// StorageGiB is the storage in GiB, per instance, node or volume, or the GiB
	// stored in an object store: at least 0, at most 1,000,000,000.
	StorageGiB *float64
	// Count is the number of instances, nodes or volumes: at least 1, at most
	// 100,000.
	Count *int
}

// Usage is how much of a month a resource runs.
type Usage struct {
	// HoursPerMonth is the hours a month the resource runs: more than 0, at most
	// 744, the hours of a 31-day month. nil is "not given"; later milestones read
	// it as 730, an average month.
	HoursPerMonth *float64
}

// Cost is what the source charges for a resource in a month, as the source
// reports it, in the source's own currency. Both fields are required.
type Cost struct {
	// Amount is at least 0 and at most 1,000,000,000,000. nil is refused.
	Amount *float64
	// Currency is an ISO 4217 code by its shape: exactly three upper-case ASCII
	// letters. No list of codes is checked.
	Currency string
}

// RelationType is the kind of a relation from one resource to another, from a
// closed vocabulary.
type RelationType string

// The relation vocabulary, spelt exactly as the inventory file writes it.
const (
	// ReplicaOf says the resource is a replica of the target.
	ReplicaOf RelationType = "replica-of"
	// DeadLetterQueueOf says the resource is the dead-letter queue of the target.
	DeadLetterQueueOf RelationType = "dead-letter-queue-of"
	// AttachedTo says the resource is attached to the target, as a volume to a
	// server.
	AttachedTo RelationType = "attached-to"
	// DependsOn says the resource needs the target to work.
	DependsOn RelationType = "depends-on"
)

// Relation points from a resource to another resource of the same inventory.
type Relation struct {
	// Type is what the relation means.
	Type RelationType
	// Target is the ID of the other resource. In an Inventory it is that
	// resource's ID as the resource itself writes it.
	Target string
}

var (
	// ErrUnknownRelationType is returned for a relation type outside the
	// vocabulary.
	ErrUnknownRelationType = errors.New("unknown relation type")
	// ErrUnknownRelationTarget is returned for a relation whose target is no
	// resource of the inventory.
	ErrUnknownRelationTarget = errors.New("relation target is no resource of the inventory")
	// ErrSelfRelation is returned for a relation whose target is the resource
	// itself.
	ErrSelfRelation = errors.New("relation targets the resource itself")
	// ErrInvalidSize is returned for a size value out of its range.
	ErrInvalidSize = errors.New("size out of range")
	// ErrInvalidUsage is returned for a usage value out of its range.
	ErrInvalidUsage = errors.New("usage out of range")
	// ErrFieldNotForCategory is returned for a size or usage field that makes no
	// sense for the resource's category, such as vcpus on storage.object.
	ErrFieldNotForCategory = errors.New("field makes no sense for the category")
	// ErrInvalidCost is returned for a monthly cost without an amount, with an
	// amount out of range, or with a currency that is not three upper-case
	// letters.
	ErrInvalidCost = errors.New("invalid monthly cost")
)

// FieldError is a refusal of a field New places by its path, in the order the
// resources were given: resources[1].size.vcpus. Its message is the path, then
// Err's. A reader that must not repeat a value names the field by Path alone.
type FieldError struct {
	// Path names the field, as the inventory file spells it.
	Path string
	// Err says what is wrong, and wraps the sentinel.
	Err error
}

// Error returns the path, then what is wrong.
func (e *FieldError) Error() string { return e.Path + ": " + e.Err.Error() }

// Unwrap returns Err, so errors.Is finds the sentinel.
func (e *FieldError) Unwrap() error { return e.Err }

// ParseRelationType returns the relation type s names, exactly as the
// vocabulary spells it. Anything else wraps ErrUnknownRelationType.
func ParseRelationType(s string) (RelationType, error) {
	switch t := RelationType(s); t {
	case ReplicaOf, DeadLetterQueueOf, AttachedTo, DependsOn:
		return t, nil
	}

	return "", fmt.Errorf("%w %s", ErrUnknownRelationType, text.Quote(s))
}

// The size and usage fields, as the file spells them, and the bit each has in a
// category's set of allowed fields.
const (
	fieldVCPUs = 1 << iota
	fieldMemory
	fieldStorage
	fieldCount
	fieldHours
)

// sized is the set of fields a category of compute, Kubernetes nodes or
// database nodes takes: every field.
const sized = fieldVCPUs | fieldMemory | fieldStorage | fieldCount | fieldHours

// allowedFields says which size and usage fields each category takes (ADR
// 0025, amended by ADR 0046). A category that is not listed takes none, as
// network.loadbalancer, messaging.topic, serverless.container and
// registry.container do.
var allowedFields = map[taxonomy.Category]int{
	taxonomy.ComputeVM:          sized,
	taxonomy.ComputeBareMetal:   sized,
	taxonomy.KubernetesManaged:  sized,
	taxonomy.DatabasePostgreSQL: sized,
	taxonomy.DatabaseMySQL:      sized,
	taxonomy.DatabaseMongoDB:    sized,
	taxonomy.StorageBlock:       fieldStorage | fieldCount,
	taxonomy.StorageObject:      fieldStorage,
	taxonomy.StorageFile:        fieldStorage,
	taxonomy.ServerlessFunction: fieldMemory,
	taxonomy.CacheRedis:         fieldMemory | fieldCount | fieldHours,
}

// The bounds of the version 2 numbers.
const (
	maxVCPUs   = 1_000_000
	maxMemory  = 1_000_000
	maxStorage = 1_000_000_000
	maxCount   = 100_000
	maxHours   = 744
	maxAmount  = 1_000_000_000_000
)

// checkVersion2 checks the size, usage, workload and cost of r, the resource at
// index i, whose category is known, and returns a copy of r with an empty size
// or usage made nil. Relations are checked by resolveRelations, once every ID
// is known.
func checkVersion2(i int, r Resource) (Resource, error) {
	at := "resources[" + strconv.Itoa(i) + "]"
	allowed := allowedFields[r.Category]

	if r.Size != nil {
		s := r.Size

		if err := checkNumber(at+".size.vcpus", s.VCPUs, allowed&fieldVCPUs != 0, ErrInvalidSize, false, maxVCPUs); err != nil {
			return Resource{}, err
		}

		if err := checkNumber(at+".size.memoryGiB", s.MemoryGiB, allowed&fieldMemory != 0, ErrInvalidSize, false, maxMemory); err != nil {
			return Resource{}, err
		}

		if err := checkNumber(at+".size.storageGiB", s.StorageGiB, allowed&fieldStorage != 0, ErrInvalidSize, true, maxStorage); err != nil {
			return Resource{}, err
		}

		if err := checkCount(at+".size.count", s.Count, allowed&fieldCount != 0); err != nil {
			return Resource{}, err
		}
	}

	if r.Usage != nil {
		if err := checkNumber(at+".usage.hoursPerMonth", r.Usage.HoursPerMonth, allowed&fieldHours != 0, ErrInvalidUsage, false, maxHours); err != nil {
			return Resource{}, err
		}
	}

	if err := text.Validate(r.Workload); err != nil {
		return Resource{}, &FieldError{Path: at + ".workload", Err: err}
	}

	if err := checkCost(at+".monthlyCost", r.MonthlyCost); err != nil {
		return Resource{}, err
	}

	// The fields that take 0 store a given -0 as +0, so nothing derived from
	// the inventory ever shows "-0". r is a clone, so its numbers may be set.
	if r.Size != nil {
		unsignZero(r.Size.StorageGiB)
	}

	if r.MonthlyCost != nil {
		unsignZero(r.MonthlyCost.Amount)
	}

	return r, nil
}

// unsignZero sets *v to +0 when it is a zero of either sign, and leaves any
// other number, and nil, as it is.
func unsignZero(v *float64) {
	if v != nil && *v == 0 {
		*v = 0
	}
}

// checkNumber checks v, the number at path: not given, or, when the category
// allows it, more than 0 (at least 0 when zeroOK) and at most limit. A refusal
// wraps sentinel, or ErrFieldNotForCategory.
func checkNumber(path string, v *float64, allowed bool, sentinel error, zeroOK bool, limit float64) error {
	if v == nil {
		return nil
	}

	if !allowed {
		return &FieldError{Path: path, Err: ErrFieldNotForCategory}
	}

	// Written so that NaN, which compares false with everything, is refused.
	n := *v
	inRange := n > 0 && n <= limit
	want := "more than 0"

	if zeroOK {
		inRange = n >= 0 && n <= limit
		want = "at least 0"
	}

	if !inRange {
		return &FieldError{Path: path, Err: fmt.Errorf("%w: %s, want %s and at most %s",
			sentinel, formatNumber(n), want, formatNumber(limit))}
	}

	return nil
}

// checkCount checks c, the count at path: not given, or, when the category
// allows it, at least 1 and at most maxCount.
func checkCount(path string, c *int, allowed bool) error {
	if c == nil {
		return nil
	}

	if !allowed {
		return &FieldError{Path: path, Err: ErrFieldNotForCategory}
	}

	if *c < 1 || *c > maxCount {
		return &FieldError{Path: path, Err: fmt.Errorf("%w: %d, want at least 1 and at most %d",
			ErrInvalidSize, *c, maxCount)}
	}

	return nil
}

// checkCost checks c, the cost at path, when it is given: an amount from 0 to
// maxAmount and a currency of three upper-case ASCII letters.
func checkCost(path string, c *Cost) error {
	if c == nil {
		return nil
	}

	if c.Amount == nil {
		return &FieldError{Path: path + ".amount", Err: fmt.Errorf("%w: no amount", ErrInvalidCost)}
	}

	if a := *c.Amount; !(a >= 0 && a <= maxAmount) {
		return &FieldError{Path: path + ".amount", Err: fmt.Errorf("%w: %s, want at least 0 and at most %s",
			ErrInvalidCost, formatNumber(a), formatNumber(maxAmount))}
	}

	if !isCurrency(c.Currency) {
		return &FieldError{Path: path + ".currency", Err: fmt.Errorf("%w: currency %s, want three upper-case letters",
			ErrInvalidCost, text.Quote(c.Currency))}
	}

	return nil
}

// isCurrency reports whether s has the shape of an ISO 4217 code: exactly
// three upper-case ASCII letters.
func isCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}

	for i := range len(s) {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}

	return true
}

// formatNumber writes n as short as it reads back, never in a locale.
func formatNumber(n float64) string {
	return strconv.FormatFloat(n, 'g', -1, 64)
}

// resolveRelations checks the relations of r, the resource at index i, against
// ids, which holds the index of each resource by its collapsed ID, and returns
// them with each target written as its resource writes its own ID,
// de-duplicated, sorted by type then target, and nil when there are none.
func resolveRelations(i int, r Resource, resources []Resource, ids map[string]int) ([]Relation, error) {
	if len(r.Relations) == 0 {
		return nil, nil
	}

	at := "resources[" + strconv.Itoa(i) + "].relations["
	out := make([]Relation, 0, len(r.Relations))

	for j, rel := range r.Relations {
		where := at + strconv.Itoa(j) + "]"

		t, err := ParseRelationType(string(rel.Type))
		if err != nil {
			return nil, &FieldError{Path: where + ".type", Err: err}
		}

		k, ok := ids[collapseSpace(rel.Target)]
		if !ok {
			return nil, &FieldError{Path: where + ".target", Err: fmt.Errorf("%w: %s",
				ErrUnknownRelationTarget, text.Quote(rel.Target))}
		}

		if k == i {
			return nil, &FieldError{Path: where + ".target", Err: fmt.Errorf("%w: %s",
				ErrSelfRelation, text.Quote(rel.Target))}
		}

		out = append(out, Relation{Type: t, Target: resources[k].ID})
	}

	slices.SortFunc(out, func(a, b Relation) int {
		if c := strings.Compare(string(a.Type), string(b.Type)); c != 0 {
			return c
		}

		return strings.Compare(a.Target, b.Target)
	})

	return slices.Compact(out), nil
}

// canonical returns r with an empty size or usage and an empty relations list
// made nil, so that "not given" has one spelling.
func canonical(r Resource) Resource {
	if r.Size != nil && *r.Size == (Size{}) {
		r.Size = nil
	}

	if r.Usage != nil && *r.Usage == (Usage{}) {
		r.Usage = nil
	}

	if len(r.Relations) == 0 {
		r.Relations = nil
	}

	return r
}

// clonePointer returns a copy of the value p points at, or nil.
func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}

	v := *p

	return &v
}

// cloneSize returns a copy of s that shares no memory with it.
func cloneSize(s *Size) *Size {
	if s == nil {
		return nil
	}

	return &Size{
		VCPUs:      clonePointer(s.VCPUs),
		MemoryGiB:  clonePointer(s.MemoryGiB),
		StorageGiB: clonePointer(s.StorageGiB),
		Count:      clonePointer(s.Count),
	}
}

// cloneUsage returns a copy of u that shares no memory with it.
func cloneUsage(u *Usage) *Usage {
	if u == nil {
		return nil
	}

	return &Usage{HoursPerMonth: clonePointer(u.HoursPerMonth)}
}

// cloneCost returns a copy of c that shares no memory with it.
func cloneCost(c *Cost) *Cost {
	if c == nil {
		return nil
	}

	return &Cost{Amount: clonePointer(c.Amount), Currency: c.Currency}
}
