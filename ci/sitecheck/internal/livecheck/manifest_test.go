package livecheck_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the checks of manifest.json and of the two lists the check
// derives from it: the digests, the HTML pages, the module script and the paths
// the service worker and redirect checks probe (behaviours 1, 3, 4, 6 and 10).
// A manifest is untrusted input of a release, so every hostile path and digest
// is a test case.

// manifestDigest is a digest a manifest accepts: 64 lowercase hexadecimal
// characters.
var manifestDigest = strings.Repeat("ab", 32)

// manifestJSON returns a manifest.json holding files, each with
// manifestDigest.
func manifestJSON(version string, paths ...string) string {
	files := make([]string, len(paths))
	for i, path := range paths {
		files[i] = `{"path":"` + path + `","sha256":"` + manifestDigest + `"}`
	}

	return `{"version":"` + version + `","files":[` + strings.Join(files, ",") + `]}`
}

// TestParseManifestReadsAReleaseManifest pins the manifest of a release
// (behaviour 1's input): its version, its files in the document's order, and
// white space after the object.
func TestParseManifestReadsAReleaseManifest(t *testing.T) {
	t.Parallel()

	manifest := `{"version":"v1.2.3","files":[{"path":"index.html","sha256":"` + manifestDigest + `"},` +
		`{"path":"module.0123456789abcdef.js","sha256":"` + manifestDigest + `"}]}`

	got, err := livecheck.ParseManifest(strings.NewReader(manifest + "\n"))
	if err != nil {
		t.Fatalf("ParseManifest of a release's manifest failed: %v", err)
	}

	want := livecheck.Manifest{
		Version: "v1.2.3",
		Files: []livecheck.ManifestFile{
			{Path: "index.html", SHA256: manifestDigest},
			{Path: "module.0123456789abcdef.js", SHA256: manifestDigest},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParseManifest differs from the manifest (-want +got):\n%s", diff)
	}
}

// TestParseManifestRefusesWhatItCannotUse pins the strictness of
// ParseManifest: one object, exactly its fields with their keys taken exactly
// (case included) and each of them once, a path that is a clean relative URL
// path (no "..", no leading slash, no query, no backslash, no space or control
// character), a digest of 64 lowercase hexadecimal characters, no duplicate
// path, and nothing over the cap. The paths are only ever URL paths, so a path
// that could be a local one is refused before anything opens it.
func TestParseManifestRefusesWhatItCannotUse(t *testing.T) {
	t.Parallel()

	valid := manifestJSON("v1.2.3", "index.html")

	tests := []struct {
		name      string
		manifest  string
		wantInErr string
		wantNoErr bool
	}{
		{name: "the smallest manifest", manifest: valid, wantNoErr: true},
		{name: "a path with a subdirectory", manifest: manifestJSON("v1.2.3", "assets/style.css"), wantNoErr: true},
		{name: "white space after the object", manifest: valid + " \n\t", wantNoErr: true},
		{name: "no files at all", manifest: `{"version":"v1.2.3","files":[]}`, wantNoErr: true},
		{name: "empty input", manifest: ""},
		{name: "an array, not an object", manifest: `[]`},
		{name: "an unknown field", manifest: `{"version":"v1.2.3","files":[],"extra":1}`},
		{name: "an unknown field of a file", manifest: `{"version":"v1.2.3","files":[{"path":"index.html","sha256":"` + manifestDigest + `","size":1}]}`},
		{name: "a missing version", manifest: `{"files":[]}`},
		{name: "a missing files", manifest: `{"version":"v1.2.3"}`},
		{name: "a version that is not a string", manifest: `{"version":1,"files":[]}`},
		{name: "a null files", manifest: `{"version":"v1.2.3","files":null}`},
		{name: "trailing data after the object", manifest: valid + `{}`},
		{name: "an empty path", manifest: manifestJSON("v1.2.3", "")},
		{name: "a path with a leading slash", manifest: manifestJSON("v1.2.3", "/index.html"), wantInErr: `"/index.html"`},
		{name: "a path climbing out of the site", manifest: manifestJSON("v1.2.3", "../../etc/passwd"), wantInErr: `"../../etc/passwd"`},
		{name: "a path that is a parent directory", manifest: manifestJSON("v1.2.3", ".."), wantInErr: `".."`},
		{name: "a path that is the current directory", manifest: manifestJSON("v1.2.3", "."), wantInErr: `"."`},
		{name: "a path with a parent element inside", manifest: manifestJSON("v1.2.3", "assets/../../etc/passwd"), wantInErr: `"assets/../../etc/passwd"`},
		{name: "a path with an empty element", manifest: manifestJSON("v1.2.3", "a//b"), wantInErr: `"a//b"`},
		{name: "a path with a backslash", manifest: manifestJSON("v1.2.3", `a\\b`), wantInErr: `"a\\b"`},
		{name: "a path with a query", manifest: manifestJSON("v1.2.3", "index.html?v=2"), wantInErr: `"index.html?v=2"`},
		{name: "a path with a fragment", manifest: manifestJSON("v1.2.3", "index.html#top"), wantInErr: `"index.html#top"`},
		{name: "a path with a percent escape", manifest: manifestJSON("v1.2.3", "index%2Ehtml"), wantInErr: `"index%2Ehtml"`},
		{name: "a path with a space", manifest: manifestJSON("v1.2.3", "index .html"), wantInErr: `"index .html"`},
		{
			// The path holds a U+0000 the JSON decoder produced from its escape,
			// so this pins the path check, not the decoder's refusal of a raw
			// control byte in the document.
			name:      "a path with a control character",
			manifest:  manifestJSON("v1.2.3", `index\u0000.html`),
			wantInErr: `"index\x00.html"`,
		},
		{name: "a path twice", manifest: manifestJSON("v1.2.3", "index.html", "index.html"), wantInErr: `"index.html"`},

		// A key is taken exactly as the manifest writes it, case included, and
		// only once: encoding/json alone matches a key without regard to case and
		// keeps the last of a duplicated one, so every other part of these cases is
		// valid and the key is the only reason for the refusal.
		{name: "a version key in another case", manifest: `{"Version":"v1.2.3","files":[]}`, wantInErr: `"Version"`},
		{name: "a files key in another case", manifest: `{"version":"v1.2.3","FILES":[]}`, wantInErr: `"FILES"`},
		{name: "a path key in another case", manifest: `{"version":"v1.2.3","files":[{"Path":"index.html","sha256":"` + manifestDigest + `"}]}`, wantInErr: `"Path"`},
		{name: "a digest key in another case", manifest: `{"version":"v1.2.3","files":[{"path":"index.html","SHA256":"` + manifestDigest + `"}]}`, wantInErr: `"SHA256"`},
		{name: "a version key twice", manifest: `{"version":"v1.2.3","version":"v1.2.3","files":[]}`, wantInErr: `"version" appears twice`},
		{name: "a files key twice", manifest: `{"version":"v1.2.3","files":[],"files":[]}`, wantInErr: `"files" appears twice`},
		{name: "a path key twice", manifest: `{"version":"v1.2.3","files":[{"path":"index.html","path":"index.html","sha256":"` + manifestDigest + `"}]}`, wantInErr: `"path" appears twice`},
		{name: "a digest key twice", manifest: `{"version":"v1.2.3","files":[{"path":"index.html","sha256":"` + manifestDigest + `","sha256":"` + manifestDigest + `"}]}`, wantInErr: `"sha256" appears twice`},

		// A file is an object with the keys above, so a null or any other kind of
		// value in its place is refused before any of its keys is read.
		{name: "a file that is null", manifest: `{"version":"v1.2.3","files":[null]}`, wantInErr: "not a JSON object"},
		{name: "a file that is a number", manifest: `{"version":"v1.2.3","files":[1]}`, wantInErr: "not a JSON object"},
		{name: "a file that is a string", manifest: `{"version":"v1.2.3","files":["index.html"]}`, wantInErr: "not a JSON object"},
		{name: "a digest in upper case", manifest: `{"version":"v1.2.3","files":[{"path":"index.html","sha256":"` + strings.ToUpper(manifestDigest) + `"}]}`},
		{name: "a digest a character short", manifest: `{"version":"v1.2.3","files":[{"path":"index.html","sha256":"` + manifestDigest[:63] + `"}]}`},
		{name: "a digest with a character that is not hexadecimal", manifest: `{"version":"v1.2.3","files":[{"path":"index.html","sha256":"` + manifestDigest[:63] + `z"}]}`},
		{name: "a missing digest", manifest: `{"version":"v1.2.3","files":[{"path":"index.html"}]}`},
		{
			name:     "more white space than the cap allows",
			manifest: valid + strings.Repeat(" ", livecheck.MaxManifestBytes),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manifest, err := livecheck.ParseManifest(strings.NewReader(tc.manifest))

			if tc.wantNoErr {
				if err != nil {
					t.Fatalf("ParseManifest(%q) failed: %v, want it accepted", tc.manifest, err)
				}

				return
			}

			if err == nil {
				t.Fatalf("ParseManifest(%q) returned %+v and no error, want an error: %s", tc.manifest, manifest, tc.name)
			}

			if tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("ParseManifest(%q) fails with %q, want the error to name the offending path %s: a refusal names what it read", tc.manifest, err, tc.wantInErr)
			}
		})
	}
}

// TestManifestDerivesTheChecksInputs pins what a parsed manifest answers: a
// file's digest, the HTML pages sorted, and the one module script of
// ci/sitebuild's name pattern (behaviours 3, 5 and 10).
func TestManifestDerivesTheChecksInputs(t *testing.T) {
	t.Parallel()

	manifest, err := livecheck.ParseManifest(strings.NewReader(manifestJSON(
		"v1.2.3",
		"verify.html",
		"style.0123456789abcdef.css",
		"index.html",
		"privacy.html",
		"module.fedcba9876543210.js",
	)))
	if err != nil {
		t.Fatalf("ParseManifest of the fixture manifest failed: %v", err)
	}

	if digest, ok := manifest.Digest("index.html"); !ok || digest != manifestDigest {
		t.Errorf("Digest(\"index.html\") is (%q, %t), want (%q, true)", digest, ok, manifestDigest)
	}

	if digest, ok := manifest.Digest("missing.html"); ok {
		t.Errorf("Digest(\"missing.html\") is (%q, true), want no digest: the manifest lists no such file", digest)
	}

	wantPages := []string{"index.html", "privacy.html", "verify.html"}
	if diff := cmp.Diff(wantPages, manifest.HTMLPages()); diff != "" {
		t.Errorf("HTMLPages differs from the .html files sorted (-want +got):\n%s", diff)
	}

	if got, err := manifest.ModuleScript(); err != nil || got != "module.fedcba9876543210.js" {
		t.Errorf("ModuleScript() is (%q, %v), want (%q, nil)", got, err, "module.fedcba9876543210.js")
	}
}

// TestManifestModuleScriptNeedsExactlyOne pins that a manifest without exactly
// one module script of ci/sitebuild's name pattern (module.<16 hex>.js) is an
// error (behaviour 10).
func TestManifestModuleScriptNeedsExactlyOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest livecheck.Manifest
		wantErr  bool
	}{
		{
			name:     "one module script",
			manifest: livecheck.Manifest{Files: []livecheck.ManifestFile{{Path: "module.0123456789abcdef.js", SHA256: manifestDigest}}},
		},
		{
			name: "two module scripts",
			manifest: livecheck.Manifest{Files: []livecheck.ManifestFile{
				{Path: "module.0123456789abcdef.js", SHA256: manifestDigest},
				{Path: "module.fedcba9876543210.js", SHA256: manifestDigest},
			}},
			wantErr: true,
		},
		{
			name:     "no module script",
			manifest: livecheck.Manifest{Files: []livecheck.ManifestFile{{Path: "page.0123456789abcdef.js", SHA256: manifestDigest}}},
			wantErr:  true,
		},
		{
			name:     "a 15 character hash",
			manifest: livecheck.Manifest{Files: []livecheck.ManifestFile{{Path: "module.0123456789abcde.js", SHA256: manifestDigest}}},
			wantErr:  true,
		},
		{
			name:     "an upper case hash",
			manifest: livecheck.Manifest{Files: []livecheck.ManifestFile{{Path: "module.0123456789ABCDEF.js", SHA256: manifestDigest}}},
			wantErr:  true,
		},
		{
			name:     "an empty manifest",
			manifest: livecheck.Manifest{},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.manifest.ModuleScript()

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ModuleScript() returned %q and no error, want an error: %s", got, tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("ModuleScript() failed: %v, want the one module script", err)
			}
		})
	}
}

// TestServiceWorkerPaths pins the paths behaviour 4 holds to anything but a 200,
// in the order the report lists them.
func TestServiceWorkerPaths(t *testing.T) {
	t.Parallel()

	want := []string{"/sw.js", "/service-worker.js", "/serviceworker.js", "/worker.js"}

	if diff := cmp.Diff(want, livecheck.ServiceWorkerPaths()); diff != "" {
		t.Errorf("ServiceWorkerPaths differs (-want +got):\n%s", diff)
	}
}

// TestRedirectPaths pins the matrix's paths (behaviour 6): the root and one
// deeper page, /privacy.html, which the manifest must list too.
func TestRedirectPaths(t *testing.T) {
	t.Parallel()

	want := []string{"/", "/privacy.html"}

	if diff := cmp.Diff(want, livecheck.RedirectPaths()); diff != "" {
		t.Errorf("RedirectPaths differs (-want +got):\n%s", diff)
	}
}
