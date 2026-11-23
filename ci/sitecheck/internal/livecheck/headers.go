package livecheck

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxHTMLAge is the largest max-age (and s-maxage), in seconds, an HTML page
// may be cached for (behaviour 5), and the least retry bound (behaviour 2).
const MaxHTMLAge = 300

// MinHSTSMaxAge is the least Strict-Transport-Security max-age, in seconds,
// the canonical host must send when HSTS is met: one year (behaviour 8).
const MinHSTSMaxAge = 31536000

// HSTS is a parsed Strict-Transport-Security header.
type HSTS struct {
	MaxAge            int64
	IncludeSubDomains bool
	Preload           bool
}

// ParseHSTS parses the Strict-Transport-Security header from its values (as
// http.Header.Values returns them, possibly several). It is strict: exactly
// one header value (none, or two or more, is an error); directives separated
// by ";" with optional white space around each; directive names compared
// without regard to case; exactly one max-age, whose value is decimal digits
// only, optionally inside one pair of double quotes holding digits only, and
// fits an int64 ("max-age=31536000x", "max-age=", "max-age=-1",
// "max-age=\"31536000\"x" and a second max-age are errors);
// includeSubDomains and preload without a value ("preload=1" is an error) and
// at most once each. An unknown directive is ignored, as RFC 6797 asks, unless
// its name is malformed or it holds a quote that is not closed.
func ParseHSTS(values []string) (HSTS, error) {
	if len(values) != 1 {
		return HSTS{}, fmt.Errorf("%d Strict-Transport-Security header values, want exactly one", len(values))
	}

	directives, err := parseDirectives(values[0], ';')
	if err != nil {
		return HSTS{}, fmt.Errorf("Strict-Transport-Security %s: %w", Quote(values[0]), err)
	}

	var (
		hsts      HSTS
		hasMaxAge bool
	)

	for _, d := range directives {
		switch d.name {
		case "max-age":
			if hasMaxAge {
				return HSTS{}, errors.New("Strict-Transport-Security holds max-age twice")
			}

			seconds, err := parseSeconds(d)
			if err != nil {
				return HSTS{}, fmt.Errorf("Strict-Transport-Security max-age: %w", err)
			}

			hsts.MaxAge, hasMaxAge = seconds, true
		case "includesubdomains":
			if d.hasValue || hsts.IncludeSubDomains {
				return HSTS{}, errors.New("Strict-Transport-Security includeSubDomains has a value or is there twice")
			}

			hsts.IncludeSubDomains = true
		case "preload":
			if d.hasValue || hsts.Preload {
				return HSTS{}, errors.New("Strict-Transport-Security preload has a value or is there twice")
			}

			hsts.Preload = true
		}
	}

	if !hasMaxAge {
		return HSTS{}, errors.New("Strict-Transport-Security has no max-age")
	}

	return hsts, nil
}

// Meets returns nil when h meets the minimum for the canonical host when HSTS
// is met: MaxAge at least MinHSTSMaxAge, IncludeSubDomains, and Preload when
// preload is true (preload pursued). Otherwise its error says what is missing.
func (h HSTS) Meets(preload bool) error {
	var missing []string

	if h.MaxAge < MinHSTSMaxAge {
		missing = append(missing, fmt.Sprintf("max-age %d is below %d", h.MaxAge, MinHSTSMaxAge))
	}

	if !h.IncludeSubDomains {
		missing = append(missing, "no includeSubDomains")
	}

	if preload && !h.Preload {
		missing = append(missing, "no preload while preload is pursued")
	}

	if len(missing) > 0 {
		return errors.New(strings.Join(missing, "; "))
	}

	return nil
}

// CacheControl is the part of a parsed Cache-Control header Run judges.
type CacheControl struct {
	// MaxAge is max-age in seconds; HasMaxAge says whether the header had it.
	MaxAge    int64
	HasMaxAge bool
	// SMaxAge is s-maxage in seconds; HasSMaxAge says whether the header had it.
	SMaxAge    int64
	HasSMaxAge bool
	// StaleWhileRevalidate and StaleIfError are those directives in seconds
	// (RFC 5861); each Has* says whether the header had it. Either lets a
	// cache serve the page past its max-age.
	StaleWhileRevalidate    int64
	HasStaleWhileRevalidate bool
	StaleIfError            int64
	HasStaleIfError         bool
}

// ParseCacheControl parses the Cache-Control header from its values (several
// values are joined as one comma-separated list, as RFC 9110 allows).
// Directive names are compared without regard to case. A max-age, s-maxage,
// stale-while-revalidate or stale-if-error whose value is not decimal digits
// (optionally inside one pair of double quotes) or overflows an int64 is an
// error; the same directive twice with different values is an error, twice
// with the same value is not. Other directives are ignored.
func ParseCacheControl(values []string) (CacheControl, error) {
	joined := strings.Join(values, ",")

	directives, err := parseDirectives(joined, ',')
	if err != nil {
		return CacheControl{}, fmt.Errorf("Cache-Control %s: %w", Quote(joined), err)
	}

	var cc CacheControl

	for _, d := range directives {
		var (
			value *int64
			has   *bool
		)

		switch d.name {
		case "max-age":
			value, has = &cc.MaxAge, &cc.HasMaxAge
		case "s-maxage":
			value, has = &cc.SMaxAge, &cc.HasSMaxAge
		case "stale-while-revalidate":
			value, has = &cc.StaleWhileRevalidate, &cc.HasStaleWhileRevalidate
		case "stale-if-error":
			value, has = &cc.StaleIfError, &cc.HasStaleIfError
		default:
			continue
		}

		seconds, err := parseSeconds(d)
		if err != nil {
			return CacheControl{}, fmt.Errorf("Cache-Control %s: %w", d.name, err)
		}

		if *has && *value != seconds {
			return CacheControl{}, fmt.Errorf("Cache-Control holds %s twice with different values (%d, %d)", d.name, *value, seconds)
		}

		*value, *has = seconds, true
	}

	return cc, nil
}

// JudgeHTMLCache returns nil when the Cache-Control values of an HTML page
// meet behaviour 5: the header present, ParseCacheControl accepts it, max-age
// present and at most MaxHTMLAge, and s-maxage, stale-while-revalidate and
// stale-if-error, when present, each at most MaxHTMLAge (the stale-*
// directives let a cache serve the page past its max-age, which the retry
// bound does not wait out beyond MaxHTMLAge). Otherwise its error says why
// (no header, no max-age, unparseable, conflicting, too long).
func JudgeHTMLCache(values []string) error {
	if len(values) == 0 {
		return errors.New("no Cache-Control header")
	}

	cc, err := ParseCacheControl(values)
	if err != nil {
		return err
	}

	switch {
	case !cc.HasMaxAge:
		return errors.New("Cache-Control has no max-age")
	case cc.MaxAge > MaxHTMLAge:
		return fmt.Errorf("Cache-Control max-age %d is over %d", cc.MaxAge, MaxHTMLAge)
	case cc.HasSMaxAge && cc.SMaxAge > MaxHTMLAge:
		return fmt.Errorf("Cache-Control s-maxage %d is over %d", cc.SMaxAge, MaxHTMLAge)
	case cc.HasStaleWhileRevalidate && cc.StaleWhileRevalidate > MaxHTMLAge:
		return fmt.Errorf("Cache-Control stale-while-revalidate %d is over %d", cc.StaleWhileRevalidate, MaxHTMLAge)
	case cc.HasStaleIfError && cc.StaleIfError > MaxHTMLAge:
		return fmt.Errorf("Cache-Control stale-if-error %d is over %d", cc.StaleIfError, MaxHTMLAge)
	}

	return nil
}

// AgeState says what a response's Age header was.
type AgeState int

// The states of an Age header.
const (
	// AgeAbsent: the response had no Age header.
	AgeAbsent AgeState = iota
	// AgeParsed: exactly one Age value of decimal digits that fits an int64.
	AgeParsed
	// AgeUnparseable: anything else (two values, a sign, a letter, empty).
	AgeUnparseable
)

// Age is a response's Age header as recorded (behaviour 2). It never fails a
// check by itself.
type Age struct {
	State AgeState
	// Seconds is the parsed value when State is AgeParsed, else 0.
	Seconds int64
	// Raw is the header's values joined by ", " when State is
	// AgeUnparseable, else ""; the report prints it only through Quote.
	Raw string
}

// ParseAge records the Age header from its values.
func ParseAge(values []string) Age {
	if len(values) == 0 {
		return Age{State: AgeAbsent}
	}

	if len(values) == 1 && isDigits(values[0]) {
		if seconds, err := strconv.ParseInt(values[0], 10, 64); err == nil {
			return Age{State: AgeParsed, Seconds: seconds}
		}
	}

	return Age{State: AgeUnparseable, Raw: strings.Join(values, ", ")}
}

// directive is one directive of a Strict-Transport-Security or Cache-Control
// header: its name in lower case and its value, unquoted.
type directive struct {
	name     string
	value    string
	hasValue bool
}

// parseDirectives splits a header into directives separated by sep (";" for
// Strict-Transport-Security, "," for Cache-Control): each a token name, with
// optional white space around it and around "=", and an optional value, a
// token or a quoted string. Empty elements are skipped. A directive without a
// name, a quoted string that is not closed, or anything but white space or sep
// after a directive is an error.
func parseDirectives(s string, sep byte) ([]directive, error) {
	var directives []directive

	i := 0

	for {
		i = skipOWS(s, i)
		if i == len(s) {
			return directives, nil
		}

		if s[i] == sep {
			i++

			continue
		}

		start := i
		for i < len(s) && isTChar(s[i]) {
			i++
		}

		if i == start {
			return nil, fmt.Errorf("a directive without a name at %s", Quote(s[start:]))
		}

		d := directive{name: strings.ToLower(s[start:i])}

		i = skipOWS(s, i)

		if i < len(s) && s[i] == '=' {
			i = skipOWS(s, i+1)
			d.hasValue = true

			if i < len(s) && s[i] == '"' {
				value, n, err := unquote(s[i:])
				if err != nil {
					return nil, err
				}

				d.value = value
				i += n
			} else {
				start = i
				for i < len(s) && isTChar(s[i]) {
					i++
				}

				d.value = s[start:i]
			}

			i = skipOWS(s, i)
		}

		if i < len(s) && s[i] != sep {
			return nil, fmt.Errorf("unexpected %s after the directive %s", Quote(s[i:i+1]), Quote(d.name))
		}

		directives = append(directives, d)
	}
}

// skipOWS returns the index of the first byte of s at or after i that is no
// space or tab.
func skipOWS(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}

	return i
}

// unquote reads the quoted string at the start of s (RFC 9110, a backslash
// escaping the next byte) and returns its content and the bytes it took.
func unquote(s string) (string, int, error) {
	var b strings.Builder

	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '"':
			return b.String(), i + 1, nil
		case '\\':
			i++
			if i == len(s) {
				return "", 0, errors.New("a quoted string that is not closed")
			}
		}

		b.WriteByte(s[i])
	}

	return "", 0, errors.New("a quoted string that is not closed")
}

// isTChar reports whether c is a token character of RFC 9110.
func isTChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	default:
		return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
	}
}

// isDigits reports whether s is one or more ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}

	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

// parseSeconds returns a delta-seconds directive's value: decimal digits,
// quoted or not, that fit an int64.
func parseSeconds(d directive) (int64, error) {
	if !d.hasValue || !isDigits(d.value) {
		return 0, fmt.Errorf("value %s is not decimal digits", Quote(d.value))
	}

	seconds, err := strconv.ParseInt(d.value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("value %s does not fit: %w", Quote(d.value), errors.Unwrap(err))
	}

	return seconds, nil
}
