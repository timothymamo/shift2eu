package deploy_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// The invented keys and the bucket every test uses. Neither key is a
// credential: the access key is letters only, so a fake store can echo it into
// an S3 error code and a test can still look for the whole value, and the secret
// key is a made-up string (CLAUDE.md rule 2). Nothing here reads the environment
// (rule 3).
const (
	testAccessKey = "SCWEXAMPLEACCESSKEY"
	testSecretKey = "exampleSecretKey0123456789abcdefghijklmn"
	testBucket    = "shift2eu-site"
	testRegion    = "fr-par"
)

// The release tags the tests deploy: the bundle's own, and the release the live
// page usually serves.
const (
	testedVersion  = "v0.13.0"
	earlierVersion = "v0.12.0"
)

// The bundle's asset bodies. The module script is the one file Deploy uploads
// gzip-compressed (doc.go's table), so its body is long, compressible text.
const (
	styleBody    = "/* shift2eu: the site's stylesheet */\n"
	pageBody     = "// shift2eu: the site's page script\n"
	wasmExecBody = "// shift2eu: the Go toolchain's wasm_exec.js\n"

	// secondStyleBody is another stylesheet's body: a bundle holds exactly one
	// stylesheet, so a bundle with two is refused.
	secondStyleBody = "/* shift2eu: a second stylesheet, which no bundle holds */\n"

	moduleBodyPrefix = "globalThis.__shift2euModule=\"AAAA//8=\";\n"
)

// The bundle's two fixed names beside the pages, which the module root's files
// give byte for byte (ci/sitebuild, issue #495): Apache-2.0 section 4 asks every
// redistribution, the site bundle included, to carry the licence and the NOTICE
// attribution. Neither is content-addressed, so neither name holds a digest.
const (
	licenseName = "LICENSE"
	noticeName  = "NOTICE"

	licenseBody = "Apache License\nVersion 2.0, January 2004\n"
	noticeBody  = "shift2eu\nCopyright 2026 the shift2eu authors\n"
)

// testCredentials is the key every request is signed with.
var testCredentials = deploy.Credentials{
	AccessKey: deploy.NewSecret(testAccessKey),
	SecretKey: deploy.NewSecret(testSecretKey),
}

// moduleBody is the module script ci/sitebuild writes: one fixed template around
// the module's base64, long enough that gzip has something to compress.
var moduleBody = []byte(moduleBodyPrefix + strings.Repeat("the module's base64 ", 256))

// bundle is a site bundle in memory, following the format of doc.go's "The
// bundle it consumes": the four pages, the four content-addressed assets, the
// licence and the NOTICE attribution, and manifest.json.
type bundle struct {
	// version is the release tag the manifest names.
	version string
	// files maps every file of the bundle but manifest.json to its bytes.
	files map[string][]byte
	// manifest is manifest.json itself, for the cases that hand Deploy a
	// manifest the bundle's files do not produce; nil means build it from
	// files.
	manifest []byte
}

// newBundle returns the bundle Deploy should accept: every file of the format,
// each asset named after its own digest, the licence and the NOTICE attribution,
// at version.
func newBundle(t *testing.T, version string) *bundle {
	t.Helper()

	return &bundle{version: version, files: map[string][]byte{
		"index.html":     page(version, "Assessment"),
		"inventory.html": page(version, "Inventory guide"),
		"privacy.html":   page(version, "Privacy"),
		"verify.html":    page(version, "Verify this site"),

		assetName("style", ".css", []byte(styleBody)):       []byte(styleBody),
		assetName("page", ".js", []byte(pageBody)):          []byte(pageBody),
		assetName("wasm_exec", ".js", []byte(wasmExecBody)): []byte(wasmExecBody),
		assetName("module", ".js", moduleBody):              moduleBody,

		licenseName: []byte(licenseBody),
		noticeName:  []byte(noticeBody),
	}}
}

// fsys returns the bundle, manifest.json included, as the fs.FS Deploy reads:
// the manifest it was given, or one naming every file of files.
func (b *bundle) fsys(t *testing.T) fstest.MapFS {
	t.Helper()

	files := make(fstest.MapFS, len(b.files)+1)

	for name, data := range b.files {
		files[name] = &fstest.MapFile{Data: data}
	}

	files[deploy.ManifestName] = &fstest.MapFile{Data: b.manifestBytes(t)}

	return files
}

// manifestBytes returns manifest.json: the one the test set, or one naming every
// file of files, sorted by path, with each file's digest.
func (b *bundle) manifestBytes(t *testing.T) []byte {
	t.Helper()

	if b.manifest != nil {
		return b.manifest
	}

	return manifestOf(t, b.version, b.files)
}

// keys returns the names of the bundle's files but manifest.json, sorted.
func (b *bundle) keys() []string {
	return slices.Sorted(maps.Keys(b.files))
}

// manifestDoc is manifest.json as ci/sitebuild writes it: the version and the
// files, sorted by path.
type manifestDoc struct {
	Version string            `json:"version"`
	Files   []manifestFileDoc `json:"files"`
}

// manifestFileDoc is one file of a manifest.
type manifestFileDoc struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// manifestOf returns the manifest.json of a release: every file of files, sorted
// by path, with its digest.
func manifestOf(t *testing.T, version string, files map[string][]byte) []byte {
	t.Helper()

	names := slices.Sorted(maps.Keys(files))
	filesDoc := make([]manifestFileDoc, 0, len(names))

	for _, name := range names {
		filesDoc = append(filesDoc, manifestFileDoc{Path: name, SHA256: digest(files[name])})
	}

	return manifestNaming(t, version, filesDoc)
}

// manifestNaming returns a manifest.json of exactly these entries, in this
// order, so a test can name a path the bundle does not hold, a path of the
// wrong shape or a digest the file does not have.
func manifestNaming(t *testing.T, version string, entries []manifestFileDoc) []byte {
	t.Helper()

	data, err := json.MarshalIndent(manifestDoc{Version: version, Files: entries}, "", "  ")
	if err != nil {
		t.Fatalf("marshal the manifest of %s: %v", version, err)
	}

	return append(data, '\n')
}

// manifestsFS returns the manifests tree the rollback guard reads: one
// <tag>.json per tag, naming the page that tag served as index.html and one
// content-addressed asset of its own.
func manifestsFS(t *testing.T, indexByTag map[string][]byte) fstest.MapFS {
	t.Helper()

	files := make(fstest.MapFS, len(indexByTag))

	for tag, index := range indexByTag {
		files[tag+".json"] = &fstest.MapFile{Data: manifestOf(t, tag, map[string][]byte{
			"index.html":                 index,
			"style.0123456789abcdef.css": []byte("the stylesheet of " + tag),
		})}
	}

	return files
}

// assetName returns the content-addressed name of a file: prefix, a dot, the
// first 16 lowercase hexadecimal characters of the body's SHA-256, and suffix,
// as ci/sitebuild names it.
func assetName(prefix, suffix string, body []byte) string {
	return prefix + "." + digest(body)[:16] + suffix
}

// digest returns the 64 lowercase hexadecimal characters of data's SHA-256.
func digest(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// page returns a page of the site showing version: the footer carries the one
// <span class="mono" id="site-version"> of site/privacy.html, which LiveVersion
// reads and the rollback guard compares with a release's manifest.
func page(version, title string) []byte {
	return []byte(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>` + title + `: shift2eu</title></head>
<body>
<main><h1>` + title + `</h1></main>
<footer>
<div class="wrap footer-row">
<p>No cookies, no tracking, no requests: the page cannot connect anywhere.</p>
<p class="footer-links"><a href="privacy.html">Privacy</a> <a href="verify.html">Verify this site</a> <span class="mono" id="site-version">` + version + `</span></p>
</div>
</footer>
</body>
</html>
`)
}
