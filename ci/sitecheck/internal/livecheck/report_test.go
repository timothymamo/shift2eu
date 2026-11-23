package livecheck_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the checks of the report (behaviours 2 and 11): whether it
// passed, its text form, and Quote, the one way text taken from a response
// reaches the report. The text form is what a workflow log shows, so a hostile
// header must be shown quoted and capped, never as a line of its own that could
// be read as a workflow command.

// TestReportPassed pins Passed: a report with no check at all has not passed, a
// report with one failing check has not, and only a report whose every check
// passed has.
func TestReportPassed(t *testing.T) {
	t.Parallel()

	passed := livecheck.Check{Kind: livecheck.KindRoot, Target: "/", Passed: true}

	tests := []struct {
		name   string
		report livecheck.Report
		want   bool
	}{
		{name: "no check at all", report: livecheck.Report{}},
		{
			name:   "one failing check",
			report: livecheck.Report{Checks: []livecheck.Check{passed, {Kind: livecheck.KindVersion, Target: "/"}}},
		},
		{
			name:   "every check passed",
			report: livecheck.Report{Checks: []livecheck.Check{passed, {Kind: livecheck.KindHTMLCache, Target: "index.html", Passed: true}}},
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.report.Passed(); got != tc.want {
				t.Errorf("Passed() is %t, want %t: %s", got, tc.want, tc.name)
			}
		})
	}
}

// TestQuote pins the escaping of response text (behaviour 11): the text is
// quoted as %q does, so no line break or control character survives, the first
// MaxQuotedBytes bytes are kept (cut at a UTF-8 boundary) and the rest is named,
// not shown.
func TestQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "a plain value", text: "max-age=300", want: `"max-age=300"`},
		{name: "an empty value", text: "", want: `""`},
		{name: "a line break and a workflow command", text: "\n::error::pwned", want: `"\n::error::pwned"`},
		{name: "a carriage return", text: "a\rb", want: `"a\rb"`},
		{
			name: "a value one byte over the cap",
			text: strings.Repeat("a", livecheck.MaxQuotedBytes+1),
			want: strconv.Quote(strings.Repeat("a", livecheck.MaxQuotedBytes)) + "...(truncated)",
		},
		{
			name: "a value at the cap",
			text: strings.Repeat("a", livecheck.MaxQuotedBytes),
			want: strconv.Quote(strings.Repeat("a", livecheck.MaxQuotedBytes)),
		},
		{
			name: "a cut inside a three-byte character",
			text: strings.Repeat("\u20ac", 100),
			want: strconv.Quote(strings.Repeat("\u20ac", 66)) + "...(truncated)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := livecheck.Quote(tc.text); got != tc.want {
				t.Errorf("Quote(%q) is %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// TestReportWriteText pins the report's text (behaviour 11): one line per check
// starting with PASS or FAIL, the hosts, the rounds, one line per response, the
// module script's two sizes and its encoding, the version, and a last line
// "result: PASS" or "result: FAIL". Every piece of response text is quoted, so a
// hostile header cannot start a line, and no line starts with "::", the prefix
// of a GitHub workflow command.
func TestReportWriteText(t *testing.T) {
	t.Parallel()

	tag, err := livecheck.MatchVersion(testVersion, []string{testVersion})
	if err != nil {
		t.Fatalf("MatchVersion(%q) failed: %v", testVersion, err)
	}

	const (
		hostileCacheControl = "max-age=300\n::error::pwned"
		hostileAge          = "42\n::error::pwned"
	)

	tests := []struct {
		name   string
		report livecheck.Report
		want   string
	}{
		{
			name: "a failing report of a site served without gzip",
			report: livecheck.Report{
				Host:   livecheck.DefaultHost,
				Apex:   livecheck.DefaultApex,
				Rounds: 3,
				Checks: []livecheck.Check{
					{Kind: livecheck.KindManifestFile, Target: "index.html", Passed: true},
					{Kind: livecheck.KindRoot, Target: "/", Passed: true},
					{Kind: livecheck.KindHSTS, Target: livecheck.DefaultHost, Passed: false, Reason: "max-age is weaker than one year"},
				},
				Responses: []livecheck.Response{
					{
						URL:          "https://" + livecheck.DefaultHost + "/",
						RemoteAddr:   "127.0.0.1:40404",
						Status:       200,
						Age:          livecheck.Age{State: livecheck.AgeAbsent},
						CacheControl: "public, max-age=60",
					},
					{
						URL:          "https://" + livecheck.DefaultHost + "/index.html",
						RemoteAddr:   "127.0.0.1:40405",
						Status:       200,
						Age:          livecheck.Age{State: livecheck.AgeUnparseable, Raw: hostileAge},
						CacheControl: hostileCacheControl,
					},
				},
				ModuleScript: livecheck.Size{
					Measured:    true,
					Encoding:    livecheck.EncodingIdentity,
					Transferred: 3145728,
					Decoded:     11534336,
					ContentType: "text/javascript",
				},
				Version: tag,
			},
			want: "result: FAIL",
		},
		{
			name: "a passing report of a site served gzip compressed",
			report: livecheck.Report{
				Host:   livecheck.DefaultHost,
				Apex:   livecheck.DefaultApex,
				Rounds: 1,
				Checks: []livecheck.Check{
					{Kind: livecheck.KindRoot, Target: "/", Passed: true},
					{Kind: livecheck.KindModuleScriptSize, Target: testModuleScript, Passed: true},
				},
				Responses: []livecheck.Response{
					{
						URL:          "https://" + livecheck.DefaultHost + "/",
						RemoteAddr:   "127.0.0.1:40404",
						Status:       200,
						Age:          livecheck.Age{State: livecheck.AgeParsed, Seconds: 42},
						CacheControl: "public, max-age=60",
					},
				},
				ModuleScript: livecheck.Size{
					Measured:    true,
					Encoding:    livecheck.EncodingGzip,
					Transferred: 1024,
					Decoded:     1048576,
					ContentType: "text/javascript",
				},
				Version: tag,
			},
			want: "result: PASS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			if err := tc.report.WriteText(&buf); err != nil {
				t.Fatalf("WriteText failed: %v", err)
			}

			text := buf.String()
			lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

			if last := lines[len(lines)-1]; last != tc.want {
				t.Errorf("the report's last line is %q, want %q", last, tc.want)
			}

			checkLines := 0

			for _, line := range lines {
				if strings.HasPrefix(line, "PASS") || strings.HasPrefix(line, "FAIL") {
					checkLines++
				}

				if strings.HasPrefix(line, "::") {
					t.Errorf("a line of the report starts with %q: response text was not quoted: %q", "::", line)
				}
			}

			if checkLines != len(tc.report.Checks) {
				t.Errorf("the report has %d lines starting with PASS or FAIL, want one per check (%d):\n%s",
					checkLines, len(tc.report.Checks), text)
			}

			for _, check := range tc.report.Checks {
				word := "FAIL"
				if check.Passed {
					word = "PASS"
				}

				if !hasLineWith(lines, word, check.Kind, check.Target) {
					t.Errorf("the report holds no %s line naming the check %s %q:\n%s", word, check.Kind, check.Target, text)
				}
			}

			for _, want := range []string{
				livecheck.DefaultHost,
				livecheck.DefaultApex,
				strconv.Itoa(tc.report.Rounds),
				tc.report.Version.String(),
			} {
				if !strings.Contains(text, want) {
					t.Errorf("the report does not hold %q:\n%s", want, text)
				}
			}

			if tc.report.Rounds == 3 {
				if !hasLineWith(lines, "FAIL", "max-age is weaker than one year") {
					t.Errorf("the report does not hold the failing check's reason:\n%s", text)
				}

				if !hasLineWith(lines, "absent") {
					t.Errorf("the report does not record the response without an Age header as absent:\n%s", text)
				}
			}

			if strings.Contains(text, "\n::error::") {
				t.Errorf("the report holds a raw hostile value on a line of its own:\n%s", text)
			}

			// The escaping and the response lines are pinned per case, from the
			// responses that case's report holds: the passing report is not asked
			// for another case's hostile text or another case's response.
			for _, response := range tc.report.Responses {
				if !hasLineWith(lines, response.URL, response.RemoteAddr) {
					t.Errorf("the report holds no line with the response's URL %q and remote address %q:\n%s",
						response.URL, response.RemoteAddr, text)
				}

				if !strings.Contains(text, strconv.Quote(response.CacheControl)) {
					t.Errorf("the report does not hold the cache-control %q quoted as %q: response text reaches the report only through Quote:\n%s",
						response.CacheControl, strconv.Quote(response.CacheControl), text)
				}

				if response.Age.State == livecheck.AgeUnparseable && !strings.Contains(text, strconv.Quote(response.Age.Raw)) {
					t.Errorf("the report does not hold the Age %q quoted as %q: response text reaches the report only through Quote:\n%s",
						response.Age.Raw, strconv.Quote(response.Age.Raw), text)
				}
			}

			if tc.report.ModuleScript.Encoding == livecheck.EncodingIdentity {
				if !strings.Contains(strings.ToLower(text), "identity") {
					t.Errorf("the report does not say the module script was served as identity:\n%s", text)
				}

				if !strings.Contains(strings.ToLower(text), "compress") {
					t.Errorf("the report does not say that the edge does not compress the module script (Hosting decision 4):\n%s", text)
				}
			} else if !strings.Contains(strings.ToLower(text), livecheck.EncodingGzip) {
				t.Errorf("the report does not say the module script was served gzip compressed:\n%s", text)
			}

			for _, size := range []string{
				strconv.FormatInt(tc.report.ModuleScript.Transferred, 10),
				strconv.FormatInt(tc.report.ModuleScript.Decoded, 10),
			} {
				if !strings.Contains(text, size) {
					t.Errorf("the report does not hold the module script's size %s:\n%s", size, text)
				}
			}
		})
	}
}

// hasLineWith reports whether one line holds every fragment.
func hasLineWith(lines []string, fragments ...string) bool {
	for _, line := range lines {
		holds := true

		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false

				break
			}
		}

		if holds {
			return true
		}
	}

	return false
}
