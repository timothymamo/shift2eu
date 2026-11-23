package deploy

// ManifestName is the manifest's name in a bundle and its object key.
const ManifestName = "manifest.json"

// Manifest is a bundle's manifest.json (ci/sitebuild, ADR 0036): the
// version the pages show and every other file of the bundle.
type Manifest struct {
	// Version is the shift2eu version the bundle's pages show, such as
	// "v0.13.0".
	Version string
	// Files is every file of the bundle but manifest.json, sorted by path.
	Files []ManifestFile
}

// ManifestFile is one file of a manifest.
type ManifestFile struct {
	// Path is the file's name in the bundle and its object key.
	Path string
	// SHA256 is the 64 lowercase hexadecimal characters of the SHA-256 of
	// the file's bytes (decoded, for the gzip-uploaded module script).
	SHA256 string
}

// ParseManifest decodes a manifest.json. It is strict, since a manifest of a
// release can be hostile input: it refuses data over 1 MiB, data that is not
// one JSON object of exactly the fields "version" (a string) and "files" (an
// array of objects of exactly "path" and "sha256"), an unknown or duplicate
// field, trailing data, invalid UTF-8, a path that is not one of the bundle's
// names (doc.go, "The bundle it consumes"), a path listed twice, files not
// sorted by path, a sha256 that is not 64 lowercase hexadecimal characters,
// and a version that is empty or holds a control character. Its error names
// the field and never quotes more than the offending path.
func ParseManifest(data []byte) (Manifest, error) {
	_ = data

	return Manifest{}, ErrNotImplemented
}

// LiveVersion reads the shift2eu version a live page shows: the text of the
// one `<span class="mono">...</span></p>` that ends the page's footer (the
// `<p class="footer-links">` line of every page of site/). The page is
// untrusted: LiveVersion returns ErrNoLiveVersion when there is no such span
// or more than one, and when the text does not match
// ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$. A
// version it returns is only a candidate: Deploy trusts it only when it is
// also a tag of Config.Releases.
func LiveVersion(page []byte) (string, error) {
	_ = page

	return "", ErrNotImplemented
}
