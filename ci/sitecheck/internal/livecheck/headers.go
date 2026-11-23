package livecheck

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
	return HSTS{}, stub(values)
}

// Meets returns nil when h meets the minimum for the canonical host when HSTS
// is met: MaxAge at least MinHSTSMaxAge, IncludeSubDomains, and Preload when
// preload is true (preload pursued). Otherwise its error says what is missing.
func (h HSTS) Meets(preload bool) error {
	return stub(h, preload)
}

// CacheControl is the part of a parsed Cache-Control header Run judges.
type CacheControl struct {
	// MaxAge is max-age in seconds; HasMaxAge says whether the header had it.
	MaxAge    int64
	HasMaxAge bool
	// SMaxAge is s-maxage in seconds; HasSMaxAge says whether the header had it.
	SMaxAge    int64
	HasSMaxAge bool
}

// ParseCacheControl parses the Cache-Control header from its values (several
// values are joined as one comma-separated list, as RFC 9110 allows).
// Directive names are compared without regard to case. A max-age or s-maxage
// whose value is not decimal digits (optionally inside one pair of double
// quotes) or overflows an int64 is an error; the same directive twice with
// different values is an error, twice with the same value is not. Other
// directives are ignored.
func ParseCacheControl(values []string) (CacheControl, error) {
	return CacheControl{}, stub(values)
}

// JudgeHTMLCache returns nil when the Cache-Control values of an HTML page
// meet behaviour 5: the header present, ParseCacheControl accepts it, max-age
// present and at most MaxHTMLAge, and s-maxage, when present, at most
// MaxHTMLAge. Otherwise its error says why (no header, no max-age,
// unparseable, conflicting, too long).
func JudgeHTMLCache(values []string) error {
	return stub(values)
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
	_ = stub(values)

	return Age{}
}
