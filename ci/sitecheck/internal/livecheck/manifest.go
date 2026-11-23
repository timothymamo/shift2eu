package livecheck

import "io"

// MaxManifestBytes caps how much of manifest.json ParseManifest reads: a
// longer input is an error, not a truncated manifest.
const MaxManifestBytes = 1 << 20

// Manifest is a release's manifest.json, as ci/sitebuild writes it: the
// version the pages show and every other file of the bundle with its SHA-256.
type Manifest struct {
	// Version is the manifest's "version", as written; Run does not judge it.
	Version string
	// Files are the manifest's files in the order of the document.
	Files []ManifestFile
}

// ManifestFile is one file of the manifest.
type ManifestFile struct {
	// Path is the file's path relative to the site's root, a URL path without
	// its leading slash ("index.html", "style.0123456789abcdef.css").
	Path string
	// SHA256 is the SHA-256 of the file's bytes, 64 lowercase hexadecimal
	// characters.
	SHA256 string
}

// ParseManifest reads manifest.json strictly from r, at most
// MaxManifestBytes (more is an error): exactly one JSON object with exactly
// the fields "version" (a string) and "files" (an array of objects with
// exactly the fields "path" and "sha256"), nothing after the object but
// white space, no unknown or missing field. It refuses a path that is empty,
// starts with "/", holds a "." or ".." element, an empty element ("a//b"), a
// backslash, "?", "#", "%", a control character or a space, and a sha256
// that is not 64 lowercase hexadecimal characters, and a path listed twice.
// The error names the offending path quoted (%q). The paths are only ever
// used as URL paths on the canonical host, never opened as files.
func ParseManifest(r io.Reader) (Manifest, error) {
	return Manifest{}, stub(r)
}

// Digest returns the SHA-256 the manifest lists for path (as in
// ManifestFile.Path, without a leading slash) and whether it lists the path.
func (m Manifest) Digest(path string) (string, bool) {
	_ = stub(m, path)

	return "", false
}

// HTMLPages returns the manifest's paths that end in ".html", sorted.
func (m Manifest) HTMLPages() []string {
	_ = stub(m)

	return nil
}

// ModuleScript returns the path of the manifest's module script, the one
// file whose path matches ci/sitebuild's name pattern module.<h>.js (<h>: 16
// lowercase hexadecimal characters). No such file, or more than one, is an
// error.
func (m Manifest) ModuleScript() (string, error) {
	return "", stub(m)
}

// ServiceWorkerPaths returns the URL paths that must not answer 200 on the
// canonical host (behaviour 4): /sw.js, /service-worker.js,
// /serviceworker.js and /worker.js, in that order. A manifest that lists one
// of them (without its leading slash) is a configuration error of Run.
func ServiceWorkerPaths() []string {
	return []string{"/sw.js", "/service-worker.js", "/serviceworker.js", "/worker.js"}
}

// RedirectPaths returns the URL paths the redirect matrix probes on the apex
// and over plain HTTP (behaviour 6): "/" and one deeper path,
// "/privacy.html", in that order.
func RedirectPaths() []string {
	return []string{"/", "/privacy.html"}
}
