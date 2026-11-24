package deploy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// TestParseManifest pins ParseManifest on what it accepts: one JSON object of a
// version and the files ci/sitebuild names, sorted by path, each with its
// digest — the bundle's own manifest included, which lists the licence and the
// NOTICE attribution beside the pages and the assets.
func TestParseManifest(t *testing.T) {
	t.Parallel()

	bundle := newBundle(t, testedVersion)

	files := make([]deploy.ManifestFile, 0, len(bundle.files))

	for _, name := range bundle.keys() {
		files = append(files, deploy.ManifestFile{Path: name, SHA256: digest(bundle.files[name])})
	}

	tests := []struct {
		name string
		data []byte
		want deploy.Manifest
	}{
		{
			name: "the bundle's own manifest",
			data: bundle.manifestBytes(t),
			want: deploy.Manifest{Version: testedVersion, Files: files},
		},
		{
			name: "a manifest of one file",
			data: manifestNaming(t, earlierVersion, []manifestFileDoc{
				{Path: "index.html", SHA256: emptySHA256},
			}),
			want: deploy.Manifest{Version: earlierVersion, Files: []deploy.ManifestFile{{Path: "index.html", SHA256: emptySHA256}}},
		},
		{
			name: "a manifest naming the about page",
			data: manifestNaming(t, earlierVersion, []manifestFileDoc{
				{Path: "about.html", SHA256: emptySHA256},
			}),
			want: deploy.Manifest{Version: earlierVersion, Files: []deploy.ManifestFile{{Path: "about.html", SHA256: emptySHA256}}},
		},
		{
			name: "a manifest that names no file",
			data: []byte(`{"version": "v1.0.0", "files": []}`),
			want: deploy.Manifest{Version: "v1.0.0"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := deploy.ParseManifest(tc.data)
			if err != nil {
				t.Fatalf("ParseManifest = %v, want nil", err)
			}

			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("ParseManifest (-want +got):\n%s", diff)
			}
		})
	}
}

// TestParseManifestRefusesHostileData pins ParseManifest's strictness: a
// manifest of an earlier release is hostile input, so it refuses anything but
// one object of exactly the fields the format has, a path of the bundle's names,
// files sorted and each digest 64 lowercase hexadecimal characters. Where the
// refusal has a field to name, the error names it.
func TestParseManifestRefusesHostileData(t *testing.T) {
	t.Parallel()

	valid := func(files string) string {
		return `{"version":"v0.13.0","files":[` + files + `]}`
	}

	file := func(path, sha string) string {
		return `{"path":"` + path + `","sha256":"` + sha + `"}`
	}

	tests := []struct {
		name string
		data []byte
		// wantField is the field the error must name, "" when the refusal has
		// none.
		wantField string
	}{
		{
			name: "data that is not JSON",
			data: []byte("not a manifest\n"),
		},
		{
			name: "a JSON array",
			data: []byte(`[{"version":"v0.13.0","files":[]}]`),
		},
		{
			name: "data over 1 MiB",
			data: []byte(valid(file("index.html", emptySHA256)) + strings.Repeat(" ", 1<<20)),
		},
		{
			name:      "an unknown field",
			data:      []byte(`{"version":"v0.13.0","files":[],"signature":"abc"}`),
			wantField: "signature",
		},
		{
			name:      "a duplicate field",
			data:      []byte(`{"version":"v0.13.0","version":"v0.13.0","files":[]}`),
			wantField: "version",
		},
		{
			name: "trailing data",
			data: []byte(valid(file("index.html", emptySHA256)) + `{"version":"v0.13.0","files":[]}`),
		},
		{
			name: "invalid UTF-8",
			data: []byte("{\"version\":\"v0.13.0\xff\",\"files\":[]}"),
		},
		{
			name:      "a path with a slash",
			data:      []byte(valid(file("sub/index.html", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "a path with two dots",
			data:      []byte(valid(file("../index.html", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "a path with a backslash",
			data:      []byte(valid(file(`index\html`, emptySHA256))),
			wantField: "path",
		},
		{
			name:      "a path with a percent sign",
			data:      []byte(valid(file("index%2Ehtml", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "an empty path",
			data:      []byte(valid(file("", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "a path that is no file of a bundle",
			data:      []byte(valid(file("index.css", emptySHA256))),
			wantField: "path",
		},
		{
			// Issue #528: the bundle's pages are its known set, so a page that
			// the site does not hold is no bundle file either.
			name:      "a page the bundle does not hold",
			data:      []byte(valid(file("elsewhere.html", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "the licence with a suffix",
			data:      []byte(valid(file("LICENSE.txt", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "the licence in lower case",
			data:      []byte(valid(file("license", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "the NOTICE as Markdown",
			data:      []byte(valid(file("NOTICE.md", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "the licence in a directory",
			data:      []byte(valid(file("docs/LICENSE", emptySHA256))),
			wantField: "path",
		},
		{
			name:      "a directory named after the licence",
			data:      []byte(valid(file("LICENSE/x", emptySHA256))),
			wantField: "path",
		},
		{
			name: "a path listed twice",
			data: []byte(valid(file("index.html", emptySHA256) + "," + file("index.html", emptySHA256))),
		},
		{
			name: "files out of order",
			data: []byte(valid(file("verify.html", emptySHA256) + "," + file("index.html", emptySHA256))),
		},
		{
			name:      "a sha256 that is not hexadecimal",
			data:      []byte(valid(file("index.html", strings.Repeat("z", 64)))),
			wantField: "sha256",
		},
		{
			name:      "a sha256 in upper case",
			data:      []byte(valid(file("index.html", strings.ToUpper(emptySHA256)))),
			wantField: "sha256",
		},
		{
			name:      "a sha256 of 63 characters",
			data:      []byte(valid(file("index.html", emptySHA256[:63]))),
			wantField: "sha256",
		},
		{
			name:      "an empty version",
			data:      []byte(`{"version":"","files":[]}`),
			wantField: "version",
		},
		{
			name:      "a version with a control character",
			data:      []byte("{\"version\":\"v0.13.0\\u0000\",\"files\":[]}"),
			wantField: "version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := deploy.ParseManifest(tc.data)
			if err == nil {
				t.Fatalf("ParseManifest = %+v, want an error for %s", got, tc.name)
			}

			if tc.wantField != "" && !strings.Contains(err.Error(), tc.wantField) {
				t.Errorf("ParseManifest refused %s with %q, want the error to name the field %q", tc.name, err, tc.wantField)
			}
		})
	}
}

// TestLiveVersion pins LiveVersion on the footer span every page of site/
// carries: the one <span class="mono" id="site-version"> that ends the
// footer's <p class="footer-links">, holding vMAJOR.MINOR.PATCH. The page is
// untrusted text, so everything else is ErrNoLiveVersion.
func TestLiveVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		page []byte
		want string
	}{
		{name: "the site's footer", page: page(testedVersion, "Privacy"), want: testedVersion},
		{name: "a version of zeros", page: page("v0.0.0", "Privacy"), want: "v0.0.0"},
		{name: "a version of long numbers", page: page("v999999999.888888888.777777777", "Privacy"), want: "v999999999.888888888.777777777"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := deploy.LiveVersion(tc.page)
			if err != nil {
				t.Fatalf("LiveVersion = %v, want %q", err, tc.want)
			}

			if got != tc.want {
				t.Errorf("LiveVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLiveVersionRefusesAnUntrustedPage pins LiveVersion's refusals: no footer
// span, more than one, a span that does not end the footer's paragraph, an
// id="site-version" on an element that is no mono span, the bare
// <span class="mono"> every page carried before #503, and any text that is not a
// version. Each is ErrNoLiveVersion, so the guard treats the page as untrusted
// rather than reading a version out of it. The bare span is a case of its own:
// versionAtEnd (or versionTag too) shortened back to it would read a version
// out of every page of an old bundle while each positive test still passed.
func TestLiveVersionRefusesAnUntrustedPage(t *testing.T) {
	t.Parallel()

	// footer returns a page whose footer's paragraph holds text.
	footer := func(text string) []byte {
		return []byte(`<!DOCTYPE html><html><body><footer><div class="wrap footer-row">` +
			`<p class="footer-links"><a href="privacy.html">Privacy</a> ` + text + `</p>` +
			`</div></footer></body></html>`)
	}

	tests := []struct {
		name string
		page []byte
	}{
		{name: "no page at all", page: nil},
		{name: "a page without a footer", page: []byte(`<!DOCTYPE html><html><body><h1>Privacy</h1></body></html>`)},
		{name: "a footer whose paragraph holds no span", page: footer(`<strong>v0.13.0</strong>`)},
		{name: "a paragraph of another class", page: []byte(`<p class="footer"><span class="mono" id="site-version">v0.13.0</span></p>`)},
		{name: "a span of another class", page: footer(`<span class="version" id="site-version">v0.13.0</span>`)},
		{name: "the version id on an element that is no mono span", page: footer(`<span id="site-version">v0.13.0</span>`)},
		{name: "the footer span before the version id", page: footer(`<span class="mono">v0.13.0</span>`)},
		{name: "two version spans", page: footer(`<span class="mono" id="site-version">v0.13.0</span> <span class="mono" id="site-version">v0.14.0</span>`)},
		{name: "a span that does not end the paragraph", page: footer(`<span class="mono" id="site-version">v0.13.0</span> <a href="verify.html">Verify</a>`)},
		{name: "no version at all", page: footer(`<span class="mono" id="site-version"></span>`)},
		{name: "a word instead of a version", page: footer(`<span class="mono" id="site-version">latest</span>`)},
		{name: "no leading v", page: footer(`<span class="mono" id="site-version">1.2.3</span>`)},
		{name: "two parts", page: footer(`<span class="mono" id="site-version">v1.2</span>`)},
		{name: "four parts", page: footer(`<span class="mono" id="site-version">v1.2.3.4</span>`)},
		{name: "a leading zero", page: footer(`<span class="mono" id="site-version">v01.2.3</span>`)},
		{name: "a pre-release suffix", page: footer(`<span class="mono" id="site-version">v1.2.3-rc1</span>`)},
		{name: "a part of ten digits", page: footer(`<span class="mono" id="site-version">v1234567890.0.0</span>`)},
		{name: "text around the version", page: footer(`<span class="mono" id="site-version">release v0.13.0</span>`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := deploy.LiveVersion(tc.page)
			if !errors.Is(err, deploy.ErrNoLiveVersion) {
				t.Fatalf("LiveVersion = %q, %v, want an error wrapping ErrNoLiveVersion", got, err)
			}

			if got != "" {
				t.Errorf("LiveVersion = %q with its error, want no version at all", got)
			}
		})
	}
}
