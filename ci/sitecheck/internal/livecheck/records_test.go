package livecheck_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the checks of the two records the live site check reads from
// the commit it runs from, site/records/hsts.json and
// site/records/preload.json (behaviour 8's inputs), in the format PR A's
// hosting ADR fixes (docs/adr/0042, "The HSTS and preload records"). Both are
// read as strictly as the manifest: one object of exactly their fields, the
// format this program knows, each field a value of its set, and nothing over
// the cap. Which accepted record means what is pinned where the check reads
// it: the outcome, preload and listed cases of run_test.go.
//
// hsts.json is exactly the fields format, outcome, preload, date and reason;
// preload.json is exactly format, listed, submitted, checked and subdomains.
// The interim names the check read before PR A's format (schema,
// preload_pursued, on_preload_list, plain_http_www) are unknown fields now,
// every rule of them having moved into a record's own value or the
// program's -plain-http-www flag.

// TestParseHSTSRecordAcceptsTheFormat pins the records hsts.json may hold: the
// two outcomes, preload only with "met", a real calendar date, and a reason
// that is empty when met and printable when it explains a waiver.
func TestParseHSTSRecordAcceptsTheFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record string
	}{
		{
			name:   "HSTS met, preload not pursued, no reason",
			record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":""}`,
		},
		{
			name:   "HSTS met, preload pursued, no reason",
			record: `{"format":1,"outcome":"met","preload":true,"date":"2026-12-01","reason":""}`,
		},
		{
			name:   "HSTS waived, with the reason it was waived",
			record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":"the apex is served by the old provider until the migration"}`,
		},
		{
			name:   "a reason of exactly 500 printable characters",
			record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":"` + strings.Repeat("a", 500) + `"}`,
		},
		{
			name:   "a leap day in a leap year",
			record: `{"format":1,"outcome":"met","preload":false,"date":"2024-02-29","reason":""}`,
		},
		{
			name: "a pretty-printed record",
			record: `{
  "format": 1,
  "outcome": "met",
  "preload": false,
  "date": "2026-12-01",
  "reason": ""
}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := livecheck.ParseHSTSRecord(strings.NewReader(tc.record)); err != nil {
				t.Fatalf("ParseHSTSRecord(%q) failed: %v, want it accepted", tc.record, err)
			}
		})
	}
}

// TestParseHSTSRecordRefusesWhatItCannotUse pins the strictness of hsts.json: a
// record it cannot fully believe is an error, which Run turns into a failing
// check rather than a guess (fail safe).
func TestParseHSTSRecordRefusesWhatItCannotUse(t *testing.T) {
	t.Parallel()

	const valid = `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":""}`

	tests := []struct {
		name   string
		record string
	}{
		{name: "empty input", record: ""},
		{name: "not an object", record: `[]`},
		{name: "an unknown field", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":"","extra":1}`},

		// The interim names of the format PR A replaced are unknown fields.
		{name: "the old schema field", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":"","schema":1}`},
		{name: "the old preload_pursued field", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":"","preload_pursued":false}`},
		{name: "the old plain_http_www field", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":"","plain_http_www":"redirect"}`},
		{name: "only the old fields", record: `{"schema":1,"outcome":"met","preload_pursued":false,"plain_http_www":"redirect"}`},

		// Every field is required, "reason" included when it is empty.
		{name: "a missing format", record: `{"outcome":"met","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "a missing outcome", record: `{"format":1,"preload":false,"date":"2026-12-01","reason":""}`},
		{name: "a missing preload", record: `{"format":1,"outcome":"met","date":"2026-12-01","reason":""}`},
		{name: "a missing date", record: `{"format":1,"outcome":"met","preload":false,"reason":""}`},
		{name: "a missing reason", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01"}`},

		{name: "format 2", record: `{"format":2,"outcome":"met","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "format 0", record: `{"format":0,"outcome":"met","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "a format that is a string", record: `{"format":"1","outcome":"met","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "a null format", record: `{"format":null,"outcome":"met","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "a format key in upper case", record: `{"FORMAT":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":""}`},

		{name: "an outcome outside the set", record: `{"format":1,"outcome":"unknown","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "an outcome in upper case", record: `{"format":1,"outcome":"MET","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "an outcome with trailing space", record: `{"format":1,"outcome":"met ","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "a null outcome", record: `{"format":1,"outcome":null,"preload":false,"date":"2026-12-01","reason":""}`},
		{name: "an outcome key in upper case", record: `{"format":1,"OUTCOME":"met","preload":false,"date":"2026-12-01","reason":""}`},

		{name: "a preload that is not a boolean", record: `{"format":1,"outcome":"met","preload":"yes","date":"2026-12-01","reason":""}`},
		{name: "a preload that is a number", record: `{"format":1,"outcome":"met","preload":1,"date":"2026-12-01","reason":""}`},
		{name: "a null preload", record: `{"format":1,"outcome":"met","preload":null,"date":"2026-12-01","reason":""}`},
		{name: "preload pursued while HSTS is waived", record: `{"format":1,"outcome":"waived","preload":true,"date":"2026-12-01","reason":"the apex is served by the old provider"}`},

		{name: "a month 13", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-13-01","reason":""}`},
		{name: "a day the month does not have", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-02-30","reason":""}`},
		{name: "a leap day in a year that is not a leap year", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-02-29","reason":""}`},
		{name: "a two-digit year", record: `{"format":1,"outcome":"met","preload":false,"date":"26-12-01","reason":""}`},
		{name: "a month without its leading zero", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-1-01","reason":""}`},
		{name: "a date without separators", record: `{"format":1,"outcome":"met","preload":false,"date":"20261201","reason":""}`},
		{name: "a date with a time", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01T00:00:00Z","reason":""}`},
		{name: "a date with a zone", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01+01:00","reason":""}`},
		{name: "an empty date", record: `{"format":1,"outcome":"met","preload":false,"date":"","reason":""}`},
		{name: "a null date", record: `{"format":1,"outcome":"met","preload":false,"date":null,"reason":""}`},

		{name: "an empty reason while HSTS is waived", record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "a reason while HSTS is met", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":"because"}`},
		{name: "a reason of 501 printable characters", record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":"` + strings.Repeat("a", 501) + `"}`},
		{name: "a reason with a line break", record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":"the old\nprovider"}`},
		{name: "a reason with a tab", record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":"the old\tprovider"}`},
		{name: "a reason with a null byte", record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":"the old\u0000provider"}`},
		{name: "a reason with an escape", record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":"the old\u001bprovider"}`},
		{name: "a reason that is a number", record: `{"format":1,"outcome":"waived","preload":false,"date":"2026-12-01","reason":1}`},
		{name: "a null reason", record: `{"format":1,"outcome":"met","preload":false,"date":"2026-12-01","reason":null}`},

		{name: "a duplicated outcome", record: `{"format":1,"outcome":"waived","outcome":"met","preload":false,"date":"2026-12-01","reason":"because"}`},
		{name: "a duplicated outcome in another case", record: `{"format":1,"outcome":"met","OUTCOME":"met","preload":false,"date":"2026-12-01","reason":""}`},
		{name: "trailing data", record: valid + `{}`},
		{name: "more than the cap", record: valid + strings.Repeat(" ", livecheck.MaxRecordBytes)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record, err := livecheck.ParseHSTSRecord(strings.NewReader(tc.record))
			if err == nil {
				t.Fatalf("ParseHSTSRecord(%q) returned %+v and no error, want an error: %s", tc.record, record, tc.name)
			}
		})
	}
}

// TestParsePreloadRecord pins the strict parser of preload.json: the fields of
// PR A's format, a submitted date and the subdomains only once submitted, host
// names under shift2eu.com sorted and each once.
func TestParsePreloadRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		record  string
		wantErr bool
	}{
		{name: "not listed, never submitted", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[]}`},
		{name: "listed with the day it was submitted", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`},
		{name: "listed with the subdomains at submission, sorted", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["a.shift2eu.com","b.shift2eu.com","www.shift2eu.com"]}`},

		{name: "empty input", record: "", wantErr: true},
		{name: "not an object", record: `"on"`, wantErr: true},
		{name: "an unknown field", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[],"extra":1}`, wantErr: true},

		// The interim names of the format PR A replaced are unknown fields.
		{name: "the old schema field", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[],"schema":1}`, wantErr: true},
		{name: "the old on_preload_list field", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[],"on_preload_list":false}`, wantErr: true},
		{name: "only the old fields", record: `{"schema":1,"on_preload_list":false}`, wantErr: true},

		// Every field is required, "submitted" and "subdomains" included when
		// they are empty.
		{name: "a missing format", record: `{"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[]}`, wantErr: true},
		{name: "a missing listed", record: `{"format":1,"submitted":"","checked":"2026-12-01","subdomains":[]}`, wantErr: true},
		{name: "a missing submitted", record: `{"format":1,"listed":false,"checked":"2026-12-01","subdomains":[]}`, wantErr: true},
		{name: "a missing checked", record: `{"format":1,"listed":false,"submitted":"","subdomains":[]}`, wantErr: true},
		{name: "a missing subdomains", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01"}`, wantErr: true},

		{name: "format 2", record: `{"format":2,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[]}`, wantErr: true},
		{name: "a format key in upper case", record: `{"FORMAT":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[]}`, wantErr: true},

		{name: "a listed that is not a boolean", record: `{"format":1,"listed":"no","submitted":"","checked":"2026-12-01","subdomains":[]}`, wantErr: true},
		{name: "a null listed", record: `{"format":1,"listed":null,"submitted":"","checked":"2026-12-01","subdomains":[]}`, wantErr: true},

		{name: "listed with no submitted date", record: `{"format":1,"listed":true,"submitted":"","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`, wantErr: true},
		{name: "listed on the preload list with a submitted month 13", record: `{"format":1,"listed":true,"submitted":"2026-13-01","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`, wantErr: true},
		{name: "a submitted day the month does not have", record: `{"format":1,"listed":true,"submitted":"2026-02-30","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`, wantErr: true},
		{name: "a two-digit submitted year", record: `{"format":1,"listed":true,"submitted":"26-12-01","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`, wantErr: true},
		{name: "a submitted date with a time", record: `{"format":1,"listed":true,"submitted":"2026-12-01T00:00:00Z","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`, wantErr: true},
		{name: "a null submitted", record: `{"format":1,"listed":false,"submitted":null,"checked":"2026-12-01","subdomains":[]}`, wantErr: true},

		{name: "an empty checked date", record: `{"format":1,"listed":false,"submitted":"","checked":"","subdomains":[]}`, wantErr: true},
		{name: "a checked month 13", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-13-01","subdomains":[]}`, wantErr: true},
		{name: "a checked date with a time", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01T00:00:00Z","subdomains":[]}`, wantErr: true},
		{name: "a null checked", record: `{"format":1,"listed":false,"submitted":"","checked":null,"subdomains":[]}`, wantErr: true},

		{name: "subdomains out of order", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["www.shift2eu.com","a.shift2eu.com"]}`, wantErr: true},
		{name: "a subdomain twice", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["www.shift2eu.com","www.shift2eu.com"]}`, wantErr: true},
		{name: "a subdomain of another domain", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["evil.com"]}`, wantErr: true},
		{name: "another domain ending in shift2eu.com", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["shift2eu.com.evil.com"]}`, wantErr: true},
		{name: "a domain only ending in shift2eu.com", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["xshift2eu.com"]}`, wantErr: true},
		{name: "the apex without a subdomain", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["shift2eu.com"]}`, wantErr: true},
		{name: "a subdomain in upper case", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["WWW.shift2eu.com"]}`, wantErr: true},
		{name: "an empty subdomain", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":[""]}`, wantErr: true},
		{name: "subdomains while nothing was submitted", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`, wantErr: true},
		{name: "subdomains that are a string", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":"www.shift2eu.com"}`, wantErr: true},
		{name: "a null subdomains", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":null}`, wantErr: true},
		{name: "a subdomain that is a number", record: `{"format":1,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":[1]}`, wantErr: true},

		{name: "a listed key in upper case", record: `{"format":1,"LISTED":false,"submitted":"","checked":"2026-12-01","subdomains":[]}`, wantErr: true},
		{name: "a duplicated listed", record: `{"format":1,"listed":false,"listed":true,"submitted":"2026-12-01","checked":"2026-12-01","subdomains":["www.shift2eu.com"]}`, wantErr: true},
		{name: "trailing data", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[]}{}`, wantErr: true},
		{name: "more than the cap", record: `{"format":1,"listed":false,"submitted":"","checked":"2026-12-01","subdomains":[]}` + strings.Repeat(" ", livecheck.MaxRecordBytes), wantErr: true},
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
				t.Fatalf("ParsePreloadRecord(%q) failed: %v, want it accepted", tc.record, err)
			}
		})
	}
}
