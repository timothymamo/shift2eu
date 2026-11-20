// This is an internal test file, not the black-box package testutil_test the
// other tests here use: the const forms are read by an unexported function,
// estimateReasonsInSource, which EstimateReasons itself cannot exercise. The
// domain declares one form only, so a guard on the real domain alone would
// never show that a form it cannot read fails rather than passes unnoticed.
// The test pins every form: the typed literal, the conversion, the untyped
// constant below a typed one, and the spec that is a Reason whose value the
// test cannot read, which must fail and not be skipped.
package testutil

import (
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// sourceForms are the const declarations estimateReasonsInSource reads, one
// form each, as a file of the estimate domain holds them. A source string is a
// whole file, so a case can state the declaration exactly as the domain writes
// it, comments included.
var sourceForms = []struct {
	name string
	src  string
	// want are the reasons the source declares, in declaration order.
	want []estimate.Reason
	// wantErr is the constant the source declares as a Reason whose value the
	// test cannot read, so reading it fails and names it; empty when the source
	// reads.
	wantErr string
	// wantParseErr is set for a source the Go parser refuses, which reading it
	// fails on too.
	wantParseErr bool
}{
	{
		name: "a typed string literal",
		src: `package estimate

// Reason says why a resource is not estimated.
type Reason string

// The reasons a resource is not estimated.
const (
	// NoFit is the reason for a resource with no fit.
	NoFit Reason = "no-fit"
	// NoSize is the reason for a resource that gives no size.
	NoSize Reason = "no-size"
)
`,
		want: []estimate.Reason{"no-fit", "no-size"},
	},
	{
		name: "a const declaration that is not a group",
		src: `package estimate

type Reason string

const NotOffered Reason = "not-offered"
`,
		want: []estimate.Reason{"not-offered"},
	},
	{
		name: "several names in one spec, each with its value",
		src: `package estimate

type Reason string

const (
	NoFit, UsagePriced Reason = "no-fit", "usage-priced"
)
`,
		want: []estimate.Reason{"no-fit", "usage-priced"},
	},
	{
		name: "a conversion to Reason, with and without the type",
		src: `package estimate

type Reason string

const Converted = Reason("converted")

const TypedConverted Reason = Reason("typed-converted")
`,
		want: []estimate.Reason{"converted", "typed-converted"},
	},
	{
		name: "an untyped constant below a typed one is not a reason",
		src: `package estimate

type Reason string

const HoursPerMonth = 730

const (
	NoFit Reason = "no-fit"
	// Untyped keeps no type: Go gives a spec with values and no type the
	// default type of its expression, so it is a string, not a Reason.
	Untyped = "untyped"
	Hours   = 24
)
`,
		want: []estimate.Reason{"no-fit"},
	},
	{
		name: "a spec with another type resets what the specs below inherit",
		src: `package estimate

type Reason string

type Method string

// ByPlan is priced by plan.
const (
	ByPlan Method = "plan"
)

const (
	NoFit Reason = "no-fit"
)

const (
	NotPriced Method = "not-priced"
)
`,
		want: []estimate.Reason{"no-fit"},
	},
	{
		name: "a typed literal below an untyped spec",
		src: `package estimate

type Reason string

const (
	Hours   = 24
	NoFit   Reason = "no-fit"
)
`,
		want: []estimate.Reason{"no-fit"},
	},
	{
		name: "an implicit repetition below an untyped spec is not a reason",
		src: `package estimate

type Reason string

const (
	Label  = "label"
	Repeat
)
`,
	},
	{
		name: "an implicit repetition below a typed spec cannot be read",
		src: `package estimate

type Reason string

const (
	NoFit Reason = "no-fit"
	Repeated
)
`,
		wantErr: "Repeated",
	},
	{
		name: "iota cannot be read",
		src: `package estimate

type Reason string

const (
	First Reason = iota
)
`,
		wantErr: "First",
	},
	{
		name: "a concatenation cannot be read",
		src: `package estimate

type Reason string

const Joined Reason = "no-" + "fit"
`,
		wantErr: "Joined",
	},
	{
		name: "another constant cannot be read",
		src: `package estimate

type Reason string

const (
	NoFit Reason = "no-fit"
	Alias Reason = NoFit
)
`,
		wantErr: "Alias",
	},
	{
		name: "a file that declares no Reason",
		src: `package estimate

// Method is how a taxonomy category is priced.
type Method string

const (
	// ByPlan is priced by plan.
	ByPlan Method = "plan"
)
`,
	},
	{
		name:         "a source that does not parse",
		src:          "package estimate\n\nconst NoFit Reason =\n",
		wantParseErr: true,
	},
}

// TestEstimateReasonsInSource pins every const form the reader must read, and
// the form it must refuse: a Reason constant whose value it cannot read fails
// and names the constant, so a guard that turns one into a skipped reason (and
// so into no guard at all) fails here instead.
func TestEstimateReasonsInSource(t *testing.T) {
	t.Parallel()

	for _, tc := range sourceForms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := estimateReasonsInSource(tc.name+".go", []byte(tc.src))

			switch {
			case tc.wantParseErr:
				if err == nil {
					t.Fatalf("estimateReasonsInSource returned no error for a source that does not parse; got %v", got)
				}

				return
			case tc.wantErr != "":
				if err == nil {
					t.Fatalf("estimateReasonsInSource returned no error, want one naming %s; got %v", tc.wantErr, got)
				}

				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("estimateReasonsInSource returned the error %q, want it to name the constant %s",
						err, tc.wantErr)
				}

				return
			case err != nil:
				t.Fatalf("estimateReasonsInSource returned unexpected error: %v", err)
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("estimateReasonsInSource() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEstimateReasonsReadsTheDomain pins the exported reader on the real domain:
// it finds internal/domain/estimate from its own source file, reads it, and
// returns the reasons the package declares, the first and the last among them.
// A reader that looked in the wrong directory would find no reason at all.
func TestEstimateReasonsReadsTheDomain(t *testing.T) {
	t.Parallel()

	got := EstimateReasons(t)

	for _, want := range []estimate.Reason{estimate.NoFit, estimate.NoStoragePrice} {
		if !slices.Contains(got, want) {
			t.Errorf("EstimateReasons() = %v, want it to hold %q, which the domain declares", got, want)
		}
	}
}
