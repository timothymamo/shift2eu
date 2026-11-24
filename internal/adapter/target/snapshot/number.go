package snapshot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/timothymamo/shift2eu/internal/adapter/internal/jsondoc"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// The fractional digits an exact decimal of the format may have (ADR 0024):
// memoryGiB is held in thousandths of a GiB, amount in ten-thousandths of a
// currency unit (catalog.AmountScale).
const (
	memoryDigits = 3
	amountDigits = 4
)

// integer reads the next value, which must be a JSON number written as an
// integer, -?(0|[1-9][0-9]*), that fits an int64, and returns it. It reads the
// number from its JSON text, never through float64, and refuses with ErrDecode
// a fraction or an exponent even when the value is whole (2.0, 2e0), -0, a
// value outside int64 and any value that is not a number. A negative value is
// returned: whether it is allowed is catalog.New's rule.
func integer(w *jsondoc.Walker) (int64, error) {
	path, at, raw, err := number(w, "an integer")
	if err != nil {
		return 0, err
	}

	s := string(raw)
	if strings.ContainsAny(s, ".eE") {
		return 0, numberError(path, at, raw, "an integer, written without a fraction or an exponent")
	}

	if s == "-0" {
		return 0, numberError(path, at, raw, "an integer, written without a minus sign on zero")
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, numberError(path, at, raw, "an integer that fits 64 bits")
	}

	return n, nil
}

// givenVCPUs reads the next value, a plan's vcpus, as integer does, and also
// refuses 0 with ErrDecode: a plan whose target gives no vCPU count leaves the
// key out, so "not given" has exactly one spelling (ADR 0045). Whether a plan
// may lack the count, and a negative count, are catalog.New's rules.
func givenVCPUs(w *jsondoc.Walker) (int64, error) {
	path, at := w.Path(), w.Position()

	n, err := integer(w)
	if err != nil {
		return 0, err
	}

	if n == 0 {
		return 0, fmt.Errorf("%w: %s is 0, want at least 1, or leave the key out when the target gives no count, at %s",
			ErrDecode, text.Quote(path), at)
	}

	return n, nil
}

// exactDecimal reads the next value, which must be a JSON number written as an
// exact decimal, -?(0|[1-9][0-9]*)(\.[0-9]+)?, with at most digits fractional
// digits, trailing zeros included, and returns it as an integer count of
// 10^-digits units: with 4 digits, 12.5 is 125000. It reads the number from its
// JSON text, never through float64, and refuses with ErrDecode an exponent,
// more fractional digits than digits, a zero with a minus sign in any spelling
// (-0, -0.0), a count that does not fit an int64, and any value that is not a
// number. A negative value is returned: whether it is allowed is catalog.New's
// rule.
func exactDecimal(w *jsondoc.Walker, digits int) (int64, error) {
	path, at, raw, err := number(w, "a number")
	if err != nil {
		return 0, err
	}

	s := string(raw)
	if strings.ContainsAny(s, "eE") {
		return 0, numberError(path, at, raw, "an exact decimal, written without an exponent")
	}

	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > digits {
		return 0, numberError(path, at, raw,
			fmt.Sprintf("an exact decimal with at most %d fractional digits", digits))
	}

	if strings.HasPrefix(whole, "-") && strings.Trim(whole[1:]+frac, "0") == "" {
		return 0, numberError(path, at, raw, "an exact decimal, written without a minus sign on zero")
	}

	n, err := strconv.ParseInt(whole+frac+strings.Repeat("0", digits-len(frac)), 10, 64)
	if err != nil {
		return 0, numberError(path, at, raw, "an exact decimal that fits 64 bits at its precision")
	}

	return n, nil
}

// number reads the next value whole and returns its path, its position, and its
// JSON text, which is a number: any other value is refused with ErrDecode as a
// value of the wrong type, want naming the type the format wants.
func number(w *jsondoc.Walker, want string) (path, at string, raw []byte, err error) {
	path, at = w.Path(), w.Position()

	raw, err = w.Raw()
	if err != nil {
		return "", "", nil, err
	}

	if c := raw[0]; c != '-' && (c < '0' || c > '9') {
		return "", "", nil, fmt.Errorf("%w: %s is %s, want %s, at %s",
			ErrDecode, text.Quote(path), jsondoc.Describe(raw), want, at)
	}

	return path, at, raw, nil
}

// numberError reports that the number raw, the value at path that starts at
// at, is not written as the format wants.
func numberError(path, at string, raw []byte, want string) error {
	return fmt.Errorf("%w: %s is %s, want %s, at %s", ErrDecode, text.Quote(path), jsondoc.Describe(raw), want, at)
}
