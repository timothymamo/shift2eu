package livecheck_test

import (
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the checks of what the deployed pages say: the statements the
// records ask for (behaviour 8) and the live version the root page shows
// (behaviour 9). A page is untrusted text, so an id in a comment, a hidden
// element, a data-id attribute and two elements with the same id are all test
// cases: none of them says what the check reads.

// TestCarries pins the reading of a statement marker (behaviour 8): the page has
// an element whose id is exactly the marker, which is not hidden and whose text
// is not empty, outside comments, scripts, styles and templates, and only one
// such element.
func TestCarries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		page   string
		marker string
		want   bool
	}{
		{
			name:   "an element with the marker and text",
			page:   `<p id="downgrade-risk">a network could downgrade the first visit</p>`,
			marker: livecheck.MarkerDowngradeRisk,
			want:   true,
		},
		{
			name:   "the attribute in single quotes",
			page:   `<p id='downgrade-risk'>text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
			want:   true,
		},
		{
			name:   "the attribute without quotes",
			page:   `<p id=downgrade-risk>text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
			want:   true,
		},
		{
			name:   "the attribute name in upper case",
			page:   `<p ID="downgrade-risk">text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
			want:   true,
		},
		{
			name:   "an entity in the marker",
			page:   `<p id="downgrade&#45;risk">text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
			want:   true,
		},
		{
			name:   "text built from child elements",
			page:   `<p id="downgrade-risk"><strong>a</strong> risk</p>`,
			marker: livecheck.MarkerDowngradeRisk,
			want:   true,
		},
		{
			name:   "an entity for the text",
			page:   `<p id="downgrade-risk">a &amp; b</p>`,
			marker: livecheck.MarkerDowngradeRisk,
			want:   true,
		},
		{
			name:   "hidden without a value",
			page:   `<p id="downgrade-risk" hidden>text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "hidden in upper case",
			page:   `<p id="downgrade-risk" HIDDEN>text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "hidden with a value",
			page:   `<p id="downgrade-risk" hidden="hidden">text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "an empty element",
			page:   `<p id="downgrade-risk"></p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "white space only",
			page:   `<p id="downgrade-risk"> <span> </span> </p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "no such element",
			page:   `<p id="other">text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "a data-id attribute",
			page:   `<p data-id="downgrade-risk">text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "an id that only starts with the marker",
			page:   `<p id="downgrade-risk-note">text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "the id in a comment",
			page:   `<!-- <p id="downgrade-risk">text</p> -->`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "the id in a script",
			page:   `<script>var s = '<p id="downgrade-risk">text</p>';</script>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "the id in a style",
			page:   `<style>/* <p id="downgrade-risk">text</p> */</style>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "the id in a template",
			page:   `<template><p id="downgrade-risk">text</p></template>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "two elements with the marker",
			page:   `<p id="downgrade-risk">one</p><p id="downgrade-risk">two</p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
		{
			name:   "the other marker",
			page:   `<p id="hsts-waived">text</p>`,
			marker: livecheck.MarkerDowngradeRisk,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := livecheck.Carries([]byte(tc.page), tc.marker)
			if got != tc.want {
				t.Errorf("Carries(%q, %q) is %t, want %t: %s", tc.page, tc.marker, got, tc.want, tc.name)
			}
		})
	}
}

// TestVersionText pins the reading of the live version (behaviour 9): the text
// of the element id="site-version" of index.html, untrimmed and after entity
// decoding, so that MatchVersion refuses a trailing newline or a leading space
// rather than the reader silently removing it.
func TestVersionText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		page    string
		want    string
		wantErr bool
	}{
		{
			name: "the footer's version span",
			page: `<footer><p class="footer-links"><span class="mono" id="site-version">v1.2.3</span></p></footer>`,
			want: "v1.2.3",
		},
		{
			name: "a trailing newline is kept",
			page: "<footer><span id=\"site-version\">v1.2.3\n</span></footer>",
			want: "v1.2.3\n",
		},
		{
			name: "a leading space is kept",
			page: `<footer><span id="site-version"> v1.2.3</span></footer>`,
			want: " v1.2.3",
		},
		{
			name: "an entity in the version",
			page: `<footer><span id="site-version">v1&#46;2&#46;3</span></footer>`,
			want: "v1.2.3",
		},
		{name: "no such element", page: `<footer><span class="mono">v1.2.3</span></footer>`, wantErr: true},
		{name: "two such elements", page: `<span id="site-version">v1.2.3</span><span id="site-version">v1.2.3</span>`, wantErr: true},
		{name: "a hidden element", page: `<span id="site-version" hidden>v1.2.3</span>`, wantErr: true},
		{name: "an empty element", page: `<span id="site-version"></span>`, wantErr: true},
		{
			name:    "a text over the cap",
			page:    `<span id="site-version">` + strings.Repeat("v", livecheck.MaxVersionBytes+1) + `</span>`,
			wantErr: true,
		},
		{
			name: "a text at the cap",
			page: `<span id="site-version">` + strings.Repeat("v", livecheck.MaxVersionBytes) + `</span>`,
			want: strings.Repeat("v", livecheck.MaxVersionBytes),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := livecheck.VersionText([]byte(tc.page))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("VersionText(%q) returned %q and no error, want an error: %s", tc.page, got, tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("VersionText(%q) failed: %v", tc.page, err)
			}

			if got != tc.want {
				t.Errorf("VersionText(%q) is %q, want %q", tc.page, got, tc.want)
			}
		})
	}
}

// TestVersionTextScansARawTextHeavyBodyInLinearTime pins that reading the live
// version out of an untrusted body stays linear in the body's length: a body of
// many raw-text elements followed by one upper-case byte is read within a
// generous guard, where lowering the whole remainder once per element made the
// scan super-linear (auditor finding 2).
//
// The guard catches a hang, not a slow run, and is deliberately far too wide to
// measure the scan: the regression it pins is no near miss. The former
// rawTextLength copied and lowered the whole remainder for every raw-text
// element, so its work grows with the square of the body's length: about 10s at
// 512 KiB, hence about 16 times that, well over two minutes, at the 2 MiB here,
// and more the larger the body. The linear scan reads the same 2 MiB body in
// well under a second off -race and -cover, and a few seconds at worst on a
// loaded shared runner, which is two orders of magnitude below the guard. A 2s
// budget was neither: it timed the scan against whatever else the runner was
// doing (CI on a saturated runner took 2.03s). The scan therefore runs in a
// goroutine that this test stops waiting for after 60s, so a regressed scan
// fails this test instead of being held up by the package's timeout, while a
// linear one never comes near it.
func TestVersionTextScansARawTextHeavyBodyInLinearTime(t *testing.T) {
	t.Parallel()

	const (
		guard = 60 * time.Second
		size  = 2 << 20
	)

	pairs := strings.Repeat(`<style></style><script></script><title></title>`, size/47)
	body := pairs + "A"

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = livecheck.VersionText([]byte(body))
	}()

	select {
	case <-done:
	case <-time.After(guard):
		t.Fatalf("VersionText did not read a %d-byte body of %d raw-text elements within %v: the scan is super-linear in the body's length", len(body), strings.Count(body, "<style></style>")*3, guard)
	}
}

// TestVersionMarkerAndStatementMarkers pins the ids the pages and the check
// agree on (ADR 0036): the footer's version span and the two statements the
// records can require.
func TestVersionMarkerAndStatementMarkers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, got, want string }{
		{name: "the version marker", got: livecheck.VersionMarker, want: "site-version"},
		{name: "the waived statement", got: livecheck.MarkerHSTSWaived, want: "hsts-waived"},
		{name: "the downgrade-risk statement", got: livecheck.MarkerDowngradeRisk, want: "downgrade-risk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.got != tc.want {
				t.Errorf("the marker is %q, want %q: the built pages and the live check read the same id", tc.got, tc.want)
			}
		})
	}
}
