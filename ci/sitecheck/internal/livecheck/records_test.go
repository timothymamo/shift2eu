package livecheck_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the checks of the two records the live site check reads from
// the commit it runs from, site/records/hsts.json and
// site/records/preload.json (behaviour 8's inputs). Both are read as strictly
// as the manifest: one object, exactly its fields, the schema this program
// knows, a value of the set, and nothing over the cap.

// TestParseHSTSRecordReadsTheOutcomes pins the two states of hsts.json and the
// two answers of plain HTTP on the canonical host.
func TestParseHSTSRecordReadsTheOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record string
		want   livecheck.HSTSRecord
	}{
		{
			name:   "HSTS met, preload pursued and plain HTTP redirecting",
			record: `{"schema":1,"outcome":"met","preload_pursued":true,"plain_http_www":"redirect"}`,
			want:   livecheck.HSTSRecord{Outcome: livecheck.HSTSMet, PreloadPursued: true, PlainHTTPWWW: livecheck.PlainHTTPRedirect},
		},
		{
			name:   "HSTS waived and plain HTTP refusing or closing",
			record: `{"schema":1,"outcome":"waived","preload_pursued":false,"plain_http_www":"refuse-or-close"}`,
			want:   livecheck.HSTSRecord{Outcome: livecheck.HSTSWaived, PlainHTTPWWW: livecheck.PlainHTTPRefuseOrClose},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := livecheck.ParseHSTSRecord(strings.NewReader(tc.record + "\n"))
			if err != nil {
				t.Fatalf("ParseHSTSRecord(%q) failed: %v", tc.record, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseHSTSRecord(%q) differs (-want +got):\n%s", tc.record, diff)
			}
		})
	}
}

// TestParseHSTSRecordRefusesWhatItCannotUse pins the strictness of hsts.json: a
// record it cannot fully believe is an error, which Run turns into a failing
// check rather than a guess (fail safe).
func TestParseHSTSRecordRefusesWhatItCannotUse(t *testing.T) {
	t.Parallel()

	const valid = `{"schema":1,"outcome":"met","preload_pursued":true,"plain_http_www":"redirect"}`

	tests := []struct {
		name      string
		record    string
		wantNoErr bool
	}{
		{name: "the smallest record", record: valid, wantNoErr: true},
		{name: "empty input", record: ""},
		{name: "not an object", record: `[]`},
		{name: "an unknown field", record: `{"schema":1,"outcome":"met","preload_pursued":true,"plain_http_www":"redirect","extra":1}`},
		{name: "a missing outcome", record: `{"schema":1,"preload_pursued":true,"plain_http_www":"redirect"}`},
		{name: "a missing preload_pursued", record: `{"schema":1,"outcome":"met","plain_http_www":"redirect"}`},
		{name: "a missing plain_http_www", record: `{"schema":1,"outcome":"met","preload_pursued":true}`},
		{name: "a null outcome", record: `{"schema":1,"outcome":null,"preload_pursued":true,"plain_http_www":"redirect"}`},
		{name: "a preload_pursued that is not a boolean", record: `{"schema":1,"outcome":"met","preload_pursued":"yes","plain_http_www":"redirect"}`},
		{name: "schema 2", record: `{"schema":2,"outcome":"met","preload_pursued":true,"plain_http_www":"redirect"}`},
		{name: "a schema that is a string", record: `{"schema":"1","outcome":"met","preload_pursued":true,"plain_http_www":"redirect"}`},
		{name: "a missing schema", record: `{"outcome":"met","preload_pursued":true,"plain_http_www":"redirect"}`},
		{name: "an outcome outside the set", record: `{"schema":1,"outcome":"unknown","preload_pursued":true,"plain_http_www":"redirect"}`},
		{name: "a plain_http_www outside the set", record: `{"schema":1,"outcome":"met","preload_pursued":true,"plain_http_www":"refused"}`},
		{name: "trailing data", record: valid + `{}`},
		{name: "more than the cap", record: valid + strings.Repeat(" ", livecheck.MaxRecordBytes)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record, err := livecheck.ParseHSTSRecord(strings.NewReader(tc.record))

			if tc.wantNoErr {
				if err != nil {
					t.Fatalf("ParseHSTSRecord(%q) failed: %v, want it accepted", tc.record, err)
				}

				return
			}

			if err == nil {
				t.Fatalf("ParseHSTSRecord(%q) returned %+v and no error, want an error: %s", tc.record, record, tc.name)
			}
		})
	}
}

// TestParsePreloadRecord pins the strict parser of preload.json.
func TestParsePreloadRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		record  string
		want    livecheck.PreloadRecord
		wantErr bool
	}{
		{name: "on the list", record: `{"schema":1,"on_preload_list":true}`, want: livecheck.PreloadRecord{OnPreloadList: true}},
		{name: "not on the list", record: `{"schema":1,"on_preload_list":false}`, want: livecheck.PreloadRecord{}},
		{name: "empty input", record: "", wantErr: true},
		{name: "not an object", record: `"on"`, wantErr: true},
		{name: "an unknown field", record: `{"schema":1,"on_preload_list":true,"extra":1}`, wantErr: true},
		{name: "a missing on_preload_list", record: `{"schema":1}`, wantErr: true},
		{name: "a null on_preload_list", record: `{"schema":1,"on_preload_list":null}`, wantErr: true},
		{name: "a value that is not a boolean", record: `{"schema":1,"on_preload_list":1}`, wantErr: true},
		{name: "schema 2", record: `{"schema":2,"on_preload_list":true}`, wantErr: true},
		{name: "a missing schema", record: `{"on_preload_list":true}`, wantErr: true},
		{name: "trailing data", record: `{"schema":1,"on_preload_list":true}{}`, wantErr: true},
		{name: "more than the cap", record: `{"schema":1,"on_preload_list":true}` + strings.Repeat(" ", livecheck.MaxRecordBytes), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := livecheck.ParsePreloadRecord(strings.NewReader(tc.record))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParsePreloadRecord(%q) returned %+v and no error, want an error: %s", tc.record, got, tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParsePreloadRecord(%q) failed: %v", tc.record, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParsePreloadRecord(%q) differs (-want +got):\n%s", tc.record, diff)
			}
		})
	}
}
