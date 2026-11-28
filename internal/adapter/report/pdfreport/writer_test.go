package pdfreport_test

// The writer's number form (writer.go, decimal): the thousandths of a point its
// paths, its rules and its text matrices are written with — exactly three
// decimal places, and never a sign.

import (
	"strconv"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
)

// TestDecimalWritesThreePlacesAndNoSign pins the one form the writer puts a
// coordinate in: three decimal places and no sign, as ISO 32000-1's re, m l and
// Tm operands take it. The layout never draws below zero, so a negative input is
// a fault of the writer's caller: decimal must fail closed, never write a signed
// or malformed number that a reader would take for another coordinate.
func TestDecimalWritesThreePlacesAndNoSign(t *testing.T) {
	t.Parallel()

	t.Run("a coordinate", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			n    int
			want string
		}{
			{name: "zero", n: 0, want: "0.000"},
			{name: "one thousandth of a point", n: 1, want: "0.001"},
			{name: "the largest fraction of a point", n: 999, want: "0.999"},
			{name: "one whole point", n: 1000, want: "1.000"},
			{name: "a page's width in thousandths", n: 123456, want: "123.456"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				if got := pdfreport.Decimal(tc.n); got != tc.want {
					t.Errorf("decimal(%d) = %q, want %q: a coordinate is written with exactly three decimal places and no sign",
						tc.n, got, tc.want)
				}
			})
		}
	})

	t.Run("a coordinate below zero", func(t *testing.T) {
		t.Parallel()

		for _, n := range []int{-1, -999, -1000, -123456} {
			t.Run(strconv.Itoa(n), func(t *testing.T) {
				t.Parallel()

				if got, panicked := decimalPanics(n); !panicked {
					t.Errorf("decimal(%d) = %q and does not panic, want a panic: the layout never draws below zero, so a negative coordinate must fail closed rather than be written with a sign or as a malformed number",
						n, got)
				}
			})
		}
	})
}

// decimalPanics returns decimal's result and whether it panicked: a panic is the
// way the writer fails closed on a coordinate below zero.
func decimalPanics(n int) (got string, panicked bool) {
	defer func() { panicked = recover() != nil }()

	return pdfreport.Decimal(n), false
}
