package livecheck_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the checks of the header parsing the live site check judges
// responses by: Strict-Transport-Security against the HSTS record (behaviour
// 8), Cache-Control of every HTML page (behaviour 5), and the Age header the
// report records (behaviour 2). Every parser is pure, so no server is needed.

// TestParseHSTS pins the strict parser of the Strict-Transport-Security header:
// exactly one header value, exactly one max-age whose value is digits (quoted
// or bare) and fits an int64, includeSubDomains and preload without a value and
// at most once each, and an unknown directive ignored. The tricks an edge could
// play on the check's minimum are refused, not read as a weaker or stronger
// header than they are.
func TestParseHSTS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		values  []string
		want    livecheck.HSTS
		wantErr bool
	}{
		{
			name:   "the full minimum with preload",
			values: []string{"max-age=31536000; includeSubDomains; preload"},
			want:   livecheck.HSTS{MaxAge: 31536000, IncludeSubDomains: true, Preload: true},
		},
		{
			name:   "white space around the directives and their names in any case",
			values: []string{"  MAX-AGE = 31536000 ;IncludeSubDomains;PRELOAD  "},
			want:   livecheck.HSTS{MaxAge: 31536000, IncludeSubDomains: true, Preload: true},
		},
		{
			name:   "a quoted max-age of digits",
			values: []string{`max-age="31536000"; includeSubDomains`},
			want:   livecheck.HSTS{MaxAge: 31536000, IncludeSubDomains: true},
		},
		{
			name:   "a max-age of zero",
			values: []string{"max-age=0"},
			want:   livecheck.HSTS{MaxAge: 0},
		},
		{
			name:   "an unknown directive is ignored",
			values: []string{"max-age=31536000; includeSubDomains; unknown=1"},
			want:   livecheck.HSTS{MaxAge: 31536000, IncludeSubDomains: true},
		},
		{name: "no header value", values: nil, wantErr: true},
		{name: "an empty header value", values: []string{""}, wantErr: true},
		{name: "two header values", values: []string{"max-age=31536000", "includeSubDomains"}, wantErr: true},
		{name: "no max-age", values: []string{"includeSubDomains"}, wantErr: true},
		{name: "a trailing character after the digits", values: []string{"max-age=31536000x"}, wantErr: true},
		{name: "two max-age directives", values: []string{"max-age=31536000; max-age=0"}, wantErr: true},
		{name: "an empty max-age", values: []string{"max-age="}, wantErr: true},
		{name: "a negative max-age", values: []string{"max-age=-1"}, wantErr: true},
		{name: "a quoted max-age with a trailing character", values: []string{`max-age="31536000"x`}, wantErr: true},
		{name: "an unclosed quote", values: []string{`max-age=31536000; unknown="x`}, wantErr: true},
		{name: "a max-age over an int64", values: []string{"max-age=99999999999999999999"}, wantErr: true},
		{name: "preload with a value", values: []string{"max-age=31536000; preload=1"}, wantErr: true},
		{name: "includeSubDomains twice", values: []string{"max-age=31536000; includeSubDomains; includeSubDomains"}, wantErr: true},
		{name: "a directive without a name", values: []string{"max-age=31536000; =1"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := livecheck.ParseHSTS(tc.values)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseHSTS(%q) returned %v and no error, want an error: the parser is strict about %s", tc.values, got, tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseHSTS(%q) failed: %v, want %v", tc.values, err, tc.want)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseHSTS(%q) differs from the parsed header (-want +got):\n%s", tc.values, diff)
			}
		})
	}
}

// TestHSTSMeets pins the minimum a met HSTS record holds the canonical host to
// (behaviour 8): one year, includeSubDomains, and preload only when preload is
// pursued.
func TestHSTSMeets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hsts    livecheck.HSTS
		preload bool
		wantErr bool
	}{
		{
			name:    "one year, includeSubDomains and preload when pursued",
			hsts:    livecheck.HSTS{MaxAge: livecheck.MinHSTSMaxAge, IncludeSubDomains: true, Preload: true},
			preload: true,
		},
		{
			name: "preload not required when not pursued",
			hsts: livecheck.HSTS{MaxAge: livecheck.MinHSTSMaxAge, IncludeSubDomains: true},
		},
		{
			name:    "a second short of a year",
			hsts:    livecheck.HSTS{MaxAge: livecheck.MinHSTSMaxAge - 1, IncludeSubDomains: true, Preload: true},
			preload: true,
			wantErr: true,
		},
		{
			name:    "no includeSubDomains",
			hsts:    livecheck.HSTS{MaxAge: livecheck.MinHSTSMaxAge, Preload: true},
			preload: true,
			wantErr: true,
		},
		{
			name:    "preload pursued and not sent",
			hsts:    livecheck.HSTS{MaxAge: livecheck.MinHSTSMaxAge, IncludeSubDomains: true},
			preload: true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.hsts.Meets(tc.preload)

			if tc.wantErr && err == nil {
				t.Errorf("Meets(%t) of %+v returned no error, want one: %s", tc.preload, tc.hsts, tc.name)
			}

			if !tc.wantErr && err != nil {
				t.Errorf("Meets(%t) of %+v failed: %v, want no error", tc.preload, tc.hsts, err)
			}
		})
	}
}

// TestParseCacheControl pins the Cache-Control parser behind behaviour 5: several
// header values are one comma-separated list, names are compared without regard
// to case, a max-age or s-maxage is digits (quoted or bare) or an error, and the
// same directive twice is fine only with the same value.
func TestParseCacheControl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		values  []string
		want    livecheck.CacheControl
		wantErr bool
	}{
		{
			name:   "a public page with a max-age",
			values: []string{"public, max-age=60"},
			want:   livecheck.CacheControl{MaxAge: 60, HasMaxAge: true},
		},
		{
			name:   "the max-age in the second header value",
			values: []string{"public", "max-age=60"},
			want:   livecheck.CacheControl{MaxAge: 60, HasMaxAge: true},
		},
		{
			name:   "names in any case",
			values: []string{"PUBLIC, Max-Age=60"},
			want:   livecheck.CacheControl{MaxAge: 60, HasMaxAge: true},
		},
		{
			name:   "a quoted max-age",
			values: []string{`public, max-age="60"`},
			want:   livecheck.CacheControl{MaxAge: 60, HasMaxAge: true},
		},
		{
			name:   "an s-maxage",
			values: []string{"max-age=60, s-maxage=301"},
			want:   livecheck.CacheControl{MaxAge: 60, HasMaxAge: true, SMaxAge: 301, HasSMaxAge: true},
		},
		{
			name:   "the same max-age twice with the same value",
			values: []string{"max-age=60, max-age=60"},
			want:   livecheck.CacheControl{MaxAge: 60, HasMaxAge: true},
		},
		{
			name:   "no directive at all",
			values: []string{"public, no-store"},
			want:   livecheck.CacheControl{},
		},
		{name: "the same max-age twice with different values", values: []string{"max-age=60, max-age=301"}, wantErr: true},
		{name: "a max-age that is not digits", values: []string{"max-age=x"}, wantErr: true},
		{name: "a negative max-age", values: []string{"max-age=-60"}, wantErr: true},
		{name: "a max-age over an int64", values: []string{"max-age=99999999999999999999"}, wantErr: true},
		{name: "an unclosed quote", values: []string{`max-age="60`}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := livecheck.ParseCacheControl(tc.values)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseCacheControl(%q) returned %+v and no error, want an error: %s", tc.values, got, tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseCacheControl(%q) failed: %v, want %+v", tc.values, err, tc.want)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseCacheControl(%q) differs from the parsed header (-want +got):\n%s", tc.values, diff)
			}
		})
	}
}

// TestJudgeHTMLCache pins behaviour 5 as a pure decision: an HTML page's
// Cache-Control must be present, parse, carry a max-age of at most 300, and
// carry no s-maxage above 300 either, even when its max-age is short enough.
func TestJudgeHTMLCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		values  []string
		wantErr bool
	}{
		{name: "a max-age of exactly the HTML's max-age", values: []string{"public, max-age=300"}},
		{name: "a max-age below the HTML's max-age", values: []string{"public, max-age=60"}},
		{name: "an s-maxage of exactly the HTML's max-age", values: []string{"max-age=300, s-maxage=300"}},
		{name: "no header", values: nil, wantErr: true},
		{name: "no max-age", values: []string{"public, no-store"}, wantErr: true},
		{name: "a max-age one second over", values: []string{"public, max-age=301"}, wantErr: true},
		{name: "an s-maxage over, whatever the max-age", values: []string{"max-age=60, s-maxage=301"}, wantErr: true},
		{name: "a stale-while-revalidate of exactly the HTML's max-age", values: []string{"max-age=300, stale-while-revalidate=300"}},
		{name: "a stale-if-error below the HTML's max-age", values: []string{"max-age=60, stale-if-error=60"}},
		{name: "a stale-while-revalidate over the HTML's max-age", values: []string{"max-age=60, stale-while-revalidate=301"}, wantErr: true},
		{name: "a stale-if-error over the HTML's max-age", values: []string{"max-age=60, stale-if-error=301"}, wantErr: true},
		{name: "a stale-while-revalidate over a max-age at the HTML's max-age", values: []string{"max-age=300, stale-while-revalidate=3600"}, wantErr: true},
		{name: "two conflicting max-age directives", values: []string{"max-age=60, max-age=301"}, wantErr: true},
		{name: "an unparseable max-age", values: []string{"max-age=abc"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := livecheck.JudgeHTMLCache(tc.values)

			if tc.wantErr && err == nil {
				t.Errorf("JudgeHTMLCache(%q) returned no error, want one: %s", tc.values, tc.name)
			}

			if !tc.wantErr && err != nil {
				t.Errorf("JudgeHTMLCache(%q) failed: %v, want no error", tc.values, err)
			}
		})
	}
}

// TestParseAge pins how a response's Age header is recorded (behaviour 2): one
// value of digits is parsed, anything else is recorded as unparseable with its
// raw values, and an absent header is recorded as absent. None of it fails a
// check by itself.
func TestParseAge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		want   livecheck.Age
	}{
		{name: "no header", values: nil, want: livecheck.Age{State: livecheck.AgeAbsent}},
		{name: "a value of digits", values: []string{"42"}, want: livecheck.Age{State: livecheck.AgeParsed, Seconds: 42}},
		{name: "zero", values: []string{"0"}, want: livecheck.Age{State: livecheck.AgeParsed}},
		{name: "an empty value", values: []string{""}, want: livecheck.Age{State: livecheck.AgeUnparseable, Raw: ""}},
		{name: "a letter", values: []string{"soon"}, want: livecheck.Age{State: livecheck.AgeUnparseable, Raw: "soon"}},
		{name: "a sign", values: []string{"-42"}, want: livecheck.Age{State: livecheck.AgeUnparseable, Raw: "-42"}},
		{
			name:   "two values are joined as the raw text",
			values: []string{"42", "43"},
			want:   livecheck.Age{State: livecheck.AgeUnparseable, Raw: "42, 43"},
		},
		{
			name:   "a value over an int64",
			values: []string{"99999999999999999999"},
			want:   livecheck.Age{State: livecheck.AgeUnparseable, Raw: "99999999999999999999"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, livecheck.ParseAge(tc.values)); diff != "" {
				t.Errorf("ParseAge(%q) differs from the recorded header (-want +got):\n%s", tc.values, diff)
			}
		})
	}
}
