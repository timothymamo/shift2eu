package sitefront_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This file holds the front's two inputs from the site beside the Caddyfile,
// and the checks they feed: the HSTS record site/records/hsts.json (docs/adr/0042),
// which decides whether www sends Strict-Transport-Security and with which
// value, and the Content-Security-Policy every page carries in its <meta>
// element (docs/adr/0036), which the header must repeat plus frame-ancestors
// 'none' (docs/adr/0045). Both are read from the repository, never copied: a
// change of the record or of a page's policy fails the front's test until the
// configuration follows.

// hstsRecordPath is the HSTS record from the repository root. Milestone 4
// creates it; until then the missing record reads as not waived, as
// ci/sitebuild reads it (docs/adr/0042, *The HSTS and preload records*).
const hstsRecordPath = "site/records/hsts.json"

// hstsRecordFormat is the one format of site/records/hsts.json (docs/adr/0042).
const hstsRecordFormat = 1

// hstsRecord is site/records/hsts.json as the front's test reads it
// (docs/adr/0042):
//
//	{"format": 1, "outcome": "met"|"waived", "preload": bool, "date": "YYYY-MM-DD", "reason": "..."}
type hstsRecord struct {
	Format  int    `json:"format"`
	Outcome string `json:"outcome"`
	Preload bool   `json:"preload"`
	Date    string `json:"date"`
	Reason  string `json:"reason"`
}

// hstsHeader returns the Strict-Transport-Security value www sends, and whether
// it sends one at all, for a record and whether the record exists: waived sends
// none, met sends max-age=31536000; includeSubDomains, plus preload when preload
// is pursued, and a missing record reads as not waived (docs/adr/0042,
// docs/adr/0045).
func hstsHeader(rec hstsRecord, exists bool) (string, bool, error) {
	if !exists {
		return "max-age=31536000; includeSubDomains", true, nil
	}

	switch rec.Outcome {
	case "waived":
		return "", false, nil
	case "met":
		value := "max-age=31536000; includeSubDomains"
		if rec.Preload {
			value += "; preload"
		}

		return value, true, nil
	default:
		return "", false, fmt.Errorf("site/records/hsts.json's outcome is %q, want \"met\" or \"waived\" (docs/adr/0042)", rec.Outcome)
	}
}

// readHSTSRecord reads site/records/hsts.json, and reports whether it exists.
// It refuses a record docs/adr/0042 does not allow, since a record the live
// checks cannot read decides nothing.
func readHSTSRecord(t *testing.T) (hstsRecord, bool) {
	t.Helper()

	path := filepath.Join(theRepositoryRoot(t), filepath.FromSlash(hstsRecordPath))

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return hstsRecord{}, false
	}
	if err != nil {
		t.Fatalf("read %s: %v", hstsRecordPath, err)
	}

	var rec hstsRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("%s does not parse as its one JSON object: %v (docs/adr/0042)", hstsRecordPath, err)
	}

	switch {
	case rec.Format != hstsRecordFormat:
		t.Errorf("%s's format is %d, want %d (docs/adr/0042)", hstsRecordPath, rec.Format, hstsRecordFormat)
	case rec.Outcome != "met" && rec.Outcome != "waived":
		t.Errorf("%s's outcome is %q, want \"met\" or \"waived\" (docs/adr/0042)", hstsRecordPath, rec.Outcome)
	case rec.Outcome == "met" && rec.Reason != "":
		t.Errorf("%s's outcome is met but it carries a reason %q: the reason is required only with waived (docs/adr/0042)", hstsRecordPath, rec.Reason)
	case rec.Outcome == "waived" && strings.TrimSpace(rec.Reason) == "":
		t.Errorf("%s's outcome is waived but it carries no reason: a waiver says why (docs/adr/0042)", hstsRecordPath)
	case rec.Date == "":
		t.Errorf("%s carries no date: the day the outcome was read (docs/adr/0042)", hstsRecordPath)
	}

	return rec, true
}

// hstsHeaderFor returns what the repository's record implies for www, failing
// the test when the record itself is not one docs/adr/0042 allows.
func hstsHeaderFor(t *testing.T) (string, bool) {
	t.Helper()

	value, sent, err := hstsHeader(readHSTSRecord(t))
	if err != nil {
		t.Fatal(err)
	}

	return value, sent
}

// pageCSP returns the Content-Security-Policy every page of site/ carries in
// its <meta http-equiv> element, and fails the test when a page carries none or
// when the pages differ from each other: the header is one policy for all of
// them, so a page of its own could not be repeated exactly.
func pageCSP(t *testing.T) string {
	t.Helper()

	root := theRepositoryRoot(t)

	pages, err := filepath.Glob(filepath.Join(root, "site", "*.html"))
	if err != nil {
		t.Fatalf("list the pages of site/: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("site/ holds no page, so there is no <meta> policy to hold the header to (docs/adr/0036)")
	}
	slices.Sort(pages)

	var policy string

	for i, page := range pages {
		src, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("read %s: %v", page, err)
		}

		got, ok := metaCSP(string(src))
		if !ok {
			t.Fatalf("%s carries no <meta http-equiv=\"Content-Security-Policy\"> element, so the header cannot repeat its policy (docs/adr/0036, docs/adr/0045)", filepath.Base(page))
		}

		if i == 0 {
			policy = got

			continue
		}
		if got != policy {
			t.Fatalf("%s's policy differs from %s's:\n %q\n %q\nthe header repeats one policy for every page, so they must agree (docs/adr/0036, docs/adr/0045)", filepath.Base(page), filepath.Base(pages[0]), got, policy)
		}
	}

	return policy
}

// metaCSP returns the content of the page's <meta http-equiv="Content-Security-Policy">
// element, matched whatever order its attributes are in and whatever case the
// http-equiv value is written in, as a browser reads it.
func metaCSP(src string) (string, bool) {
	for i := 0; ; {
		start := strings.Index(src[i:], "<meta")
		if start < 0 {
			return "", false
		}
		start += i

		end := strings.IndexByte(src[start:], '>')
		if end < 0 {
			return "", false
		}
		tag := src[start : start+end+1]
		i = start + end + 1

		attrs := tagAttributes(tag)

		if !strings.EqualFold(attrs["http-equiv"], "content-security-policy") {
			continue
		}

		content, ok := attrs["content"]
		if !ok {
			return "", false
		}

		return content, true
	}
}

// tagAttributes returns the attributes of one tag, by their lower-case name,
// with a double-quoted value as the pages write it.
func tagAttributes(tag string) map[string]string {
	attrs := map[string]string{}

	for i := 0; i < len(tag); i++ {
		if tag[i] != '=' {
			continue
		}

		// Read the name back to the white space before it.
		nameEnd := i
		nameStart := nameEnd
		for nameStart > 0 && !strings.ContainsRune(" \t\n\r<", rune(tag[nameStart-1])) {
			nameStart--
		}

		value := ""
		if i+1 < len(tag) && tag[i+1] == '"' {
			if stop := strings.IndexByte(tag[i+2:], '"'); stop >= 0 {
				value = tag[i+2 : i+2+stop]
			}
		}

		if name := strings.ToLower(tag[nameStart:nameEnd]); name != "" {
			attrs[name] = value
		}
	}

	return attrs
}

// cspDirectives splits a Content-Security-Policy into its directives, sorted,
// with the surrounding white space of each run removed, so two policies are
// compared as sets of directives whatever order and spacing they are written
// with. A directive written twice is kept twice: it is not the same policy.
func cspDirectives(policy string) []string {
	parts := strings.Split(policy, ";")

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.Join(strings.Fields(part), " "); part != "" {
			out = append(out, part)
		}
	}
	slices.Sort(out)

	return out
}

// withFrameAncestors returns a page's policy as the set of directives the
// header must carry: the page's own, plus frame-ancestors 'none', which only a
// response header can enforce (docs/adr/0036, docs/adr/0045).
func withFrameAncestors(directives []string) []string {
	out := append(slices.Clone(directives), "frame-ancestors 'none'")
	slices.Sort(out)

	return out
}

// permissionsPolicyFeatures are the features the front's Permissions-Policy
// must name, each turned off: what a page of the site would otherwise be able
// to ask a visitor's browser for (docs/adr/0045). browsing-topics is the
// Topics API, and interest-cohort is its predecessor, which a policy may leave
// out (browsers no longer ship it) but must not allow.
var permissionsPolicyFeatures = []string{
	"browsing-topics",
	"camera",
	"geolocation",
	"microphone",
	"payment",
	"usb",
}

// checkPermissionsPolicy refuses a Permissions-Policy that allows any feature,
// omits one of permissionsPolicyFeatures, or allows interest-cohort when it
// names it (docs/adr/0045).
func checkPermissionsPolicy(value string) error {
	features, err := parsePermissionsPolicy(value)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(features))
	for name, allowlist := range features {
		names = append(names, name)
		if allowlist != "" {
			return fmt.Errorf("allows %s (%s): the site uses no feature, so every one is turned off (docs/adr/0045)", name, allowlist)
		}
	}
	slices.Sort(names)

	for _, want := range permissionsPolicyFeatures {
		if _, ok := features[want]; !ok {
			return fmt.Errorf("does not name %s, so the browser's default for it decides: every feature the site does not use is turned off by name (docs/adr/0045)", want)
		}
	}

	return nil
}

// parsePermissionsPolicy splits a Permissions-Policy into its features, by
// lower-case name, and their allow lists without the parentheses: camera=()
// gives "camera": "" and geolocation=(self) gives "geolocation": "self".
func parsePermissionsPolicy(value string) (map[string]string, error) {
	features := map[string]string{}

	if strings.TrimSpace(value) == "" {
		return nil, errors.New("is empty, so no feature is turned off (docs/adr/0045)")
	}

	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		name, allowlist, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("%q is not feature=allowlist (docs/adr/0045)", entry)
		}

		name = strings.TrimSpace(name)
		allowlist = strings.TrimSpace(allowlist)
		if !strings.HasPrefix(allowlist, "(") || !strings.HasSuffix(allowlist, ")") {
			return nil, fmt.Errorf("%q's allow list is not in parentheses (docs/adr/0045)", entry)
		}

		features[strings.ToLower(name)] = strings.TrimSpace(allowlist[1 : len(allowlist)-1])
	}

	return features, nil
}

// TestHSTSHeader pins the header a record implies (docs/adr/0042): the minimum
// with met, plus preload when it is pursued, none with waived, and the minimum
// for a missing record, which reads as not waived.
func TestHSTSHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		record   hstsRecord
		exists   bool
		want     string
		wantSent bool
		wantErr  bool
	}{
		{
			name:     "met",
			record:   hstsRecord{Format: 1, Outcome: "met"},
			exists:   true,
			want:     "max-age=31536000; includeSubDomains",
			wantSent: true,
		},
		{
			name:     "met, pursuing preload",
			record:   hstsRecord{Format: 1, Outcome: "met", Preload: true},
			exists:   true,
			want:     "max-age=31536000; includeSubDomains; preload",
			wantSent: true,
		},
		{
			name:     "waived",
			record:   hstsRecord{Format: 1, Outcome: "waived", Reason: "the apex is not ours yet"},
			exists:   true,
			wantSent: false,
		},
		{
			name:     "missing",
			want:     "max-age=31536000; includeSubDomains",
			wantSent: true,
		},
		{
			name:    "an outcome this front does not know",
			record:  hstsRecord{Format: 1, Outcome: "later"},
			exists:  true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, sent, err := hstsHeader(tc.record, tc.exists)
			if (err != nil) != tc.wantErr {
				t.Fatalf("hstsHeader(%+v, %t) error = %v, want error: %t", tc.record, tc.exists, err, tc.wantErr)
			}
			if got != tc.want || sent != tc.wantSent {
				t.Errorf("hstsHeader(%+v, %t) = %q, %t, want %q, %t", tc.record, tc.exists, got, sent, tc.want, tc.wantSent)
			}
		})
	}
}

// TestCSPDirectives pins how two policies are compared: as sorted sets of
// directives, with runs of white space collapsed, so the header passes whatever
// order and spacing the configuration writes it with and fails when a directive
// is missing, extra or doubled.
func TestCSPDirectives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy string
		want   []string
	}{
		{
			name:   "one directive",
			policy: "default-src 'none'",
			want:   []string{"default-src 'none'"},
		},
		{
			name:   "white space around the runs and inside them",
			policy: " default-src  'none' ;; frame-ancestors 'none' ;",
			want:   []string{"default-src 'none'", "frame-ancestors 'none'"},
		},
		{
			name:   "order does not count",
			policy: "frame-ancestors 'none'; default-src 'none'",
			want:   []string{"default-src 'none'", "frame-ancestors 'none'"},
		},
		{
			name:   "a directive written twice stays twice",
			policy: "frame-ancestors 'none'; frame-ancestors 'none'",
			want:   []string{"frame-ancestors 'none'", "frame-ancestors 'none'"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := cspDirectives(tc.policy)
			if !slices.Equal(got, tc.want) {
				t.Errorf("cspDirectives(%q) = %q, want %q", tc.policy, got, tc.want)
			}
		})
	}
}

// TestMetaCSP pins the <meta> reader on the forms a page may write the element
// in.
func TestMetaCSP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
		ok   bool
	}{
		{
			name: "as the pages write it",
			src:  `<head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'"></head>`,
			want: "default-src 'none'",
			ok:   true,
		},
		{
			name: "another attribute order",
			src:  `<meta content="default-src 'none'" http-equiv="content-security-policy">`,
			want: "default-src 'none'",
			ok:   true,
		},
		{
			name: "no such element",
			src:  `<head><meta charset="utf-8"></head>`,
		},
		{
			name: "the element without a content",
			src:  `<meta http-equiv="Content-Security-Policy">`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := metaCSP(tc.src)
			if got != tc.want || ok != tc.ok {
				t.Errorf("metaCSP(%q) = %q, %t, want %q, %t", tc.src, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestParsePermissionsPolicy pins the Permissions-Policy reader: every feature
// is read with its allow list, and a feature turned off reads as the empty
// allow list.
func TestParsePermissionsPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "all off",
			value: "camera=(), microphone=()",
			want:  map[string]string{"camera": "", "microphone": ""},
		},
		{
			name:  "white space around the entries",
			value: " camera=() ,  usb=() ",
			want:  map[string]string{"camera": "", "usb": ""},
		},
		{
			name:  "an allow list",
			value: "geolocation=(self)",
			want:  map[string]string{"geolocation": "self"},
		},
		{
			name:  "another case of the feature",
			value: "Camera=()",
			want:  map[string]string{"camera": ""},
		},
		{
			name:    "an entry without an allow list",
			value:   "camera",
			wantErr: true,
		},
		{
			name:    "an allow list without its parentheses",
			value:   "camera=none",
			wantErr: true,
		},
		{
			name:    "empty",
			value:   " ",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parsePermissionsPolicy(tc.value)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parsePermissionsPolicy(%q) error = %v, want error: %t", tc.value, err, tc.wantErr)
			}
			if err != nil {
				return
			}

			if len(got) != len(tc.want) {
				t.Fatalf("parsePermissionsPolicy(%q) = %v, want %v", tc.value, got, tc.want)
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Errorf("parsePermissionsPolicy(%q)[%s] = %q, want %q", tc.value, name, got[name], want)
				}
			}
		})
	}
}

// TestCheckPermissionsPolicy pins what the front's policy must say: every named
// feature off, the six the site does not use named, and interest-cohort either
// absent or off.
func TestCheckPermissionsPolicy(t *testing.T) {
	t.Parallel()

	allOff := "browsing-topics=(), camera=(), geolocation=(), microphone=(), payment=(), usb=()"

	tests := []struct {
		name      string
		value     string
		wantErr   bool
		wantErrIn string // a substring the error must hold, when set
	}{
		{
			name:  "all off, interest-cohort left out",
			value: allOff,
		},
		{
			name:  "interest-cohort turned off too",
			value: allOff + ", interest-cohort=()",
		},
		{
			name:      "a feature allowed",
			value:     strings.Replace(allOff, "camera=()", "camera=(self)", 1),
			wantErr:   true,
			wantErrIn: "camera",
		},
		{
			name:      "a feature left out",
			value:     strings.Replace(allOff, ", usb=()", "", 1),
			wantErr:   true,
			wantErrIn: "usb",
		},
		// interest-cohort may be left out but must not be allowed to anyone: the well-formed allow lists
		// below are refused for the feature they allow, not for their spelling.
		{
			name:      "interest-cohort allowed to every origin",
			value:     allOff + ", interest-cohort=(*)",
			wantErr:   true,
			wantErrIn: "interest-cohort",
		},
		{
			name:      "interest-cohort allowed to self",
			value:     allOff + ", interest-cohort=(self)",
			wantErr:   true,
			wantErrIn: "interest-cohort",
		},
		{
			name:    "an empty policy",
			value:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := checkPermissionsPolicy(tc.value)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkPermissionsPolicy(%q) error = %v, want error: %t", tc.value, err, tc.wantErr)
			}
			if err != nil && tc.wantErrIn != "" && !strings.Contains(err.Error(), tc.wantErrIn) {
				t.Errorf("checkPermissionsPolicy(%q) reports %q, want an error naming %q: the refusal is the feature the policy allows, not its spelling", tc.value, err, tc.wantErrIn)
			}
		})
	}
}
