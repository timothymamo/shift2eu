package estimate_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// TestSum pins the totals: only the estimated resources are summed, every
// resource is counted, a resource that is not estimated is never added as
// 0 EUR, and the estimates that leave out storage their plan does not include
// are counted (ADR 0032).
func TestSum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		estimates []estimate.Estimate
		want      estimate.Total
	}{
		{
			name:      "no estimates at all",
			estimates: nil,
			want:      estimate.Total{},
		},
		{
			name:      "an empty list",
			estimates: []estimate.Estimate{},
			want:      estimate.Total{},
		},
		{
			name: "two estimated and one not estimated",
			estimates: []estimate.Estimate{
				{Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(120000)},
				{Region: "xx-par", Reason: estimate.NoSize},
				{Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(250000)},
			},
			want: estimate.Total{Monthly: estimate.FromAmount(370000), Estimated: 2, Resources: 3},
		},
		{
			// A total of nothing but unpriced resources is not 0 EUR: it says
			// nothing was estimated.
			name: "only resources that are not estimated",
			estimates: []estimate.Estimate{
				{Region: "xx-par", Reason: estimate.NoPrice},
				{Region: "xx-par", Reason: estimate.NoPlan},
			},
			want: estimate.Total{Estimated: 0, Resources: 2},
		},
		{
			// A category that is never priced (ADR 0045) is not counted as 0: it
			// joins the estimates that are not estimated, whatever its reason.
			name: "a resource that is never priced is not counted as 0",
			estimates: []estimate.Estimate{
				{Region: "xx-par", Reason: estimate.NeverPriced},
				{Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(10000)},
			},
			want: estimate.Total{Monthly: estimate.FromAmount(10000), Estimated: 1, Resources: 2},
		},
		{
			name: "a free estimate is estimated",
			estimates: []estimate.Estimate{
				{Region: "xx-par", Estimated: true},
			},
			want: estimate.Total{Estimated: 1, Resources: 1},
		},
		{
			// An estimate that is not estimated contributes nothing, whatever
			// its Monthly holds.
			name: "the monthly of a resource that is not estimated is ignored",
			estimates: []estimate.Estimate{
				{Region: "xx-par", Estimated: false, Monthly: estimate.FromAmount(990000)},
				{Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(10000)},
			},
			want: estimate.Total{Monthly: estimate.FromAmount(10000), Estimated: 1, Resources: 2},
		},
		{
			// The sum is exact: it rounds nothing before it is displayed.
			name: "the sum is exact",
			estimates: []estimate.Estimate{
				{Region: "xx-par", Estimated: true, Monthly: estimate.Ratio(1, 3)},
				{Region: "xx-par", Estimated: true, Monthly: estimate.Ratio(2, 3)},
			},
			want: estimate.Total{Monthly: estimate.Ratio(1, 1), Estimated: 2, Resources: 2},
		},
		{
			name: "no estimate leaves out storage the plan does not include",
			estimates: []estimate.Estimate{
				{Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(10000)},
				{Region: "xx-par", Reason: estimate.NoSize},
			},
			want: estimate.Total{Monthly: estimate.FromAmount(10000), Estimated: 1, Resources: 2},
		},
		{
			// The note is a fact about an estimate, so the total counts the
			// estimates that carry one (ADR 0032).
			name: "the estimates that leave out storage are counted",
			estimates: []estimate.Estimate{
				{
					Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(20000),
					StorageNotIncluded: estimate.BootVolume,
				},
				{Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(10000)},
				{
					Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(30000),
					StorageNotIncluded: estimate.DataStorage,
				},
				{Region: "xx-par", Reason: estimate.NoPrice},
			},
			want: estimate.Total{
				Monthly: estimate.FromAmount(60000), Estimated: 3, Resources: 4, StorageNotIncluded: 2,
			},
		},
		{
			// A resource that is not estimated is never priced and so never
			// leaves storage out: only an estimated resource counts.
			name: "a note on a resource that is not estimated is not counted",
			estimates: []estimate.Estimate{
				{
					Region: "xx-par", Reason: estimate.NoStoragePrice,
					StorageNotIncluded: estimate.BootVolume,
				},
				{
					Region: "xx-par", Estimated: true, Monthly: estimate.FromAmount(10000),
					StorageNotIncluded: estimate.DataStorage,
				},
			},
			want: estimate.Total{Monthly: estimate.FromAmount(10000), Estimated: 1, Resources: 2, StorageNotIncluded: 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before := slices.Clone(tc.estimates)
			got := estimate.Sum(tc.estimates)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Sum() mismatch (-want +got):\n%s", diff)
			}

			// The result is a plain value: Sum changed neither an estimate nor
			// the slice it was given.
			if diff := cmp.Diff(before, tc.estimates); diff != "" {
				t.Errorf("Sum() changed its input (-before +after):\n%s", diff)
			}
		})
	}
}

// TestSumDistinguishesUnpricedFromFree pins that a total over resources that are
// not estimated is distinct from a total of a zero-euro estimate, though both
// sum to 0 EUR: only Estimated says how many resources the total covers.
func TestSumDistinguishesUnpricedFromFree(t *testing.T) {
	t.Parallel()

	unpriced := estimate.Sum([]estimate.Estimate{
		{Region: "xx-par", Reason: estimate.NoPrice},
		{Region: "xx-par", Reason: estimate.NoSize},
	})

	free := estimate.Sum([]estimate.Estimate{
		{Region: "xx-par", Estimated: true},
	})

	if !unpriced.Monthly.IsZero() || !free.Monthly.IsZero() {
		t.Fatalf("precondition failed: both totals should be 0 EUR, got %s and %s",
			unpriced.Monthly.Exact(), free.Monthly.Exact())
	}

	if diff := cmp.Diff(unpriced, free); diff == "" {
		t.Error("a total of unpriced resources equals a total of a free estimate, want them distinct")
	}

	if got, want := free.Estimated, 1; got != want {
		t.Errorf("the free total estimated %d resources, want %d", got, want)
	}

	if got, want := unpriced.Estimated, 0; got != want {
		t.Errorf("the unpriced total estimated %d resources, want %d", got, want)
	}
}
