package livecheck_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the checks of the body reader and of the module script's size
// budget (behaviour 10, issue #346 item 2, ADR 0043): what the edge transfers
// and what the body decodes to, the 3 MiB transferred budget for a gzip-served
// module script, the 11 MiB decoded budget however it is served, and the
// Content-Type it must have. The budgets are fixed constants, so their values
// are pinned here too.

// TestSizeBudgetsAreFixed pins the budgets' values: 3 MiB of transfer and 11 MiB
// decoded, the numbers ADR 0043 states and the site's pages hold their assets
// to. A change to one of them is a decision, not a tweak.
func TestSizeBudgetsAreFixed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		got  int64
		want int64
	}{
		{name: "the transfer budget", got: livecheck.TransferBudget, want: 3145728},
		{name: "the decoded budget", got: livecheck.DecodedBudget, want: 11534336},
		{name: "the default body cap", got: livecheck.DefaultMaxBodyBytes, want: 16777216},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.got != tc.want {
				t.Errorf("%s is %d bytes, want %d (ADR 0043): a budget is a fixed decision", tc.name, tc.got, tc.want)
			}
		})
	}
}

// endlessReader is a body that never ends, counting the bytes it hands out.
type endlessReader struct {
	handedOut int64
}

// Read always returns a full buffer of zeros.
func (r *endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}

	r.handedOut += int64(len(p))

	return len(p), nil
}

// TestReadBody pins the reader behind behaviours 1 and 10: it counts the bytes
// as transferred, decodes gzip and nothing else, and stops at the cap, so a
// hostile endless body cannot exhaust memory.
func TestReadBody(t *testing.T) {
	t.Parallel()

	gzipped := gzipBytes(t, []byte("a module script"))

	tests := []struct {
		name            string
		contentEncoding string
		body            []byte
		maxBytes        int64
		want            livecheck.Body
		wantErr         bool
	}{
		{
			name: "an identity body",
			body: []byte("abc"),
			want: livecheck.Body{Encoding: livecheck.EncodingIdentity, Transferred: 3, Data: []byte("abc")},
		},
		{
			name:            "an explicit identity encoding",
			contentEncoding: "identity",
			body:            []byte("abc"),
			want:            livecheck.Body{Encoding: livecheck.EncodingIdentity, Transferred: 3, Data: []byte("abc")},
		},
		{
			name:            "a gzip body is counted as transferred and decoded",
			contentEncoding: "gzip",
			body:            gzipped,
			want:            livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: int64(len(gzipped)), Data: []byte("a module script")},
		},
		{
			name:            "an encoding the reader does not know",
			contentEncoding: "br",
			body:            []byte("abc"),
			wantErr:         true,
		},
		{
			name:            "a list of encodings",
			contentEncoding: "gzip, br",
			body:            gzipped,
			wantErr:         true,
		},
		{
			name:            "a gzip body that does not decode",
			contentEncoding: "gzip",
			body:            []byte("not gzip at all"),
			wantErr:         true,
		},
		{
			name:     "a body one byte over the cap",
			body:     []byte("abcd"),
			maxBytes: 3,
			wantErr:  true,
		},
		{
			name:            "a gzip body over the cap once decoded",
			contentEncoding: "gzip",
			body:            gzipBytes(t, []byte("abcd")),
			maxBytes:        3,
			wantErr:         true,
		},
		{
			name:     "a cap that is not positive",
			body:     []byte("abc"),
			maxBytes: -1,
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			maxBytes := tc.maxBytes
			if maxBytes == 0 {
				maxBytes = livecheck.DefaultMaxBodyBytes
			}

			got, err := livecheck.ReadBody(tc.contentEncoding, strings.NewReader(string(tc.body)), maxBytes)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ReadBody(%q) returned %+v and no error, want an error: %s", tc.contentEncoding, got, tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("ReadBody(%q) failed: %v", tc.contentEncoding, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ReadBody(%q) differs (-want +got):\n%s", tc.contentEncoding, diff)
			}
		})
	}
}

// TestReadBodyStopsAtTheCap pins the cap's other half: from a body that never
// ends, ReadBody reads at most maxBytes+1 bytes, so an endless body costs the
// check one byte over its cap and not the machine's memory.
func TestReadBodyStopsAtTheCap(t *testing.T) {
	t.Parallel()

	const maxBytes = 32

	source := &endlessReader{}

	body, err := livecheck.ReadBody("", source, maxBytes)
	if err == nil {
		t.Fatalf("ReadBody of an endless body returned %+v and no error, want an error after the cap", body)
	}

	if source.handedOut == 0 {
		t.Fatalf("ReadBody read no byte of the endless body, want it to read up to the cap: %v", err)
	}

	if source.handedOut > maxBytes+1 {
		t.Errorf("ReadBody read %d bytes of an endless body capped at %d, want at most %d: the cap is what keeps a hostile body from exhausting memory",
			source.handedOut, maxBytes, maxBytes+1)
	}
}

// TestMeasureModuleScript pins behaviour 10's decision: a gzip-served module
// script is held to both budgets, the bytes as the edge transfers them (3 MiB)
// and the bytes after decoding (11 MiB); one served as identity is held only to
// the decoded budget, and the report says the edge does not compress it. The
// Content-Type is text/javascript, parameters allowed.
func TestMeasureModuleScript(t *testing.T) {
	t.Parallel()

	oneMiB := make([]byte, 1<<20)
	decodedAtTheBudget := make([]byte, livecheck.DecodedBudget)
	decodedOverTheBudget := make([]byte, livecheck.DecodedBudget+1)

	tests := []struct {
		name        string
		contentType string
		body        livecheck.Body
		wantErr     bool
	}{
		{
			name:        "a gzip body exactly at the transfer budget",
			contentType: "text/javascript",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: livecheck.TransferBudget, Data: oneMiB},
		},
		{
			name:        "a gzip body one byte over the transfer budget",
			contentType: "text/javascript",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: livecheck.TransferBudget + 1, Data: oneMiB},
			wantErr:     true,
		},
		{
			name:        "a gzip body exactly at the decoded budget",
			contentType: "text/javascript",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: 3 << 10, Data: decodedAtTheBudget},
		},
		{
			name:        "a gzip body one byte over the decoded budget",
			contentType: "text/javascript",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: 3 << 10, Data: decodedOverTheBudget},
			wantErr:     true,
		},
		{
			name:        "an identity body is held only to the decoded budget",
			contentType: "text/javascript",
			body:        livecheck.Body{Encoding: livecheck.EncodingIdentity, Transferred: livecheck.TransferBudget + 1, Data: oneMiB},
		},
		{
			name:        "an identity body over the decoded budget",
			contentType: "text/javascript",
			body:        livecheck.Body{Encoding: livecheck.EncodingIdentity, Transferred: int64(len(decodedOverTheBudget)), Data: decodedOverTheBudget},
			wantErr:     true,
		},
		{
			name:        "a type with a charset parameter",
			contentType: "text/javascript; charset=utf-8",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: 3 << 10, Data: oneMiB},
		},
		{
			name:        "a type in upper case",
			contentType: "TEXT/JAVASCRIPT",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: 3 << 10, Data: oneMiB},
		},
		{
			name:        "the wrong media type",
			contentType: "application/javascript",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: 3 << 10, Data: oneMiB},
			wantErr:     true,
		},
		{
			name:        "a type that only starts with the right one",
			contentType: "text/javascriptx",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: 3 << 10, Data: oneMiB},
			wantErr:     true,
		},
		{
			name:        "no type at all",
			contentType: "",
			body:        livecheck.Body{Encoding: livecheck.EncodingGzip, Transferred: 3 << 10, Data: oneMiB},
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			size, err := livecheck.MeasureModuleScript(tc.contentType, tc.body)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("MeasureModuleScript(%q, %+v) returned %+v and no error, want an error: %s", tc.contentType, tc.body, size, tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("MeasureModuleScript(%q, %+v) failed: %v", tc.contentType, tc.body, err)
			}

			want := livecheck.Size{
				Measured:    true,
				Encoding:    tc.body.Encoding,
				Transferred: tc.body.Transferred,
				Decoded:     int64(len(tc.body.Data)),
				ContentType: tc.contentType,
			}

			if diff := cmp.Diff(want, size); diff != "" {
				t.Errorf("MeasureModuleScript(%q) differs from the measurement (-want +got):\n%s", tc.contentType, diff)
			}
		})
	}
}
