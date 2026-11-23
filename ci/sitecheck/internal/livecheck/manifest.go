package livecheck

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

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

// manifestJSON is manifest.json as decoded: pointers, so a missing field or a
// JSON null is told from an empty value.
type manifestJSON struct {
	Version *string             `json:"version"`
	Files   *[]manifestFileJSON `json:"files"`
}

// manifestFileJSON is one file of manifestJSON.
type manifestFileJSON struct {
	Path   *string `json:"path"`
	SHA256 *string `json:"sha256"`
}

// manifestFileFields is manifestFileJSON without its UnmarshalJSON method, so
// decoding into it does not call that method again.
type manifestFileFields manifestFileJSON

// UnmarshalJSON decodes one file of the manifest as strictly as decodeStrict
// decodes the manifest itself: an object with only the keys "path" and
// "sha256", exactly, each at most once, no unknown field.
func (f *manifestFileJSON) UnmarshalJSON(data []byte) error {
	return decodeStrict(bytes.NewReader(data), int64(len(data)), (*manifestFileFields)(f), "path", "sha256")
}

// digestPattern is a SHA-256 as the manifest holds it.
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// moduleScriptPattern is ci/sitebuild's name of the module script.
var moduleScriptPattern = regexp.MustCompile(`^module\.[0-9a-f]{16}\.js$`)

// ParseManifest reads manifest.json strictly from r, at most
// MaxManifestBytes (more is an error): exactly one JSON object with exactly
// the fields "version" (a string) and "files" (an array of objects with
// exactly the fields "path" and "sha256"), nothing after the object but
// white space, no unknown or missing field (a null counts as missing), every
// key exactly as written here, case included, and none twice in its object.
// It refuses a file entry that is not a JSON object, null included, a path
// that is empty, starts with "/", holds a "." or ".." element, an empty
// element ("a//b"), a backslash, "?", "#", "%", a control character, a space
// or any other character outside printable ASCII, a sha256 that is not 64
// lowercase hexadecimal characters, and a path listed twice. The error names
// the offending path or key, when there is one, quoted (%q). The paths are
// only ever used as URL paths on the canonical host, never opened as files.
func ParseManifest(r io.Reader) (Manifest, error) {
	var doc manifestJSON

	if err := decodeStrict(r, MaxManifestBytes, &doc, "version", "files"); err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}

	if doc.Version == nil {
		return Manifest{}, errors.New("manifest: no \"version\"")
	}

	if doc.Files == nil {
		return Manifest{}, errors.New("manifest: no \"files\"")
	}

	manifest := Manifest{Version: *doc.Version, Files: make([]ManifestFile, 0, len(*doc.Files))}
	seen := make(map[string]bool, len(*doc.Files))

	for i, file := range *doc.Files {
		if file.Path == nil {
			return Manifest{}, fmt.Errorf("manifest: file %d has no \"path\"", i)
		}

		path := *file.Path

		if err := checkManifestPath(path); err != nil {
			return Manifest{}, fmt.Errorf("manifest: path %q: %w", path, err)
		}

		if seen[path] {
			return Manifest{}, fmt.Errorf("manifest: path %q is listed twice", path)
		}

		seen[path] = true

		if file.SHA256 == nil {
			return Manifest{}, fmt.Errorf("manifest: path %q has no \"sha256\"", path)
		}

		if !digestPattern.MatchString(*file.SHA256) {
			return Manifest{}, fmt.Errorf("manifest: path %q: sha256 %q is not 64 lowercase hexadecimal characters", path, *file.SHA256)
		}

		manifest.Files = append(manifest.Files, ManifestFile{Path: path, SHA256: *file.SHA256})
	}

	return manifest, nil
}

// checkManifestPath returns nil when path is a clean relative URL path.
func checkManifestPath(path string) error {
	if path == "" {
		return errors.New("empty")
	}

	if strings.HasPrefix(path, "/") {
		return errors.New("starts with a slash")
	}

	for i := range len(path) {
		c := path[i]

		switch {
		case c <= ' ' || c >= 0x7f:
			return fmt.Errorf("holds the byte %#x, not a printable ASCII character other than a space", c)
		case strings.IndexByte(`\?#%`, c) >= 0:
			return fmt.Errorf("holds %q", string(c))
		}
	}

	for element := range strings.SplitSeq(path, "/") {
		switch element {
		case "":
			return errors.New("holds an empty element")
		case ".", "..":
			return fmt.Errorf("holds the element %q", element)
		}
	}

	return nil
}

// Digest returns the SHA-256 the manifest lists for path (as in
// ManifestFile.Path, without a leading slash) and whether it lists the path.
func (m Manifest) Digest(path string) (string, bool) {
	for _, file := range m.Files {
		if file.Path == path {
			return file.SHA256, true
		}
	}

	return "", false
}

// HTMLPages returns the manifest's paths that end in ".html", sorted.
func (m Manifest) HTMLPages() []string {
	var pages []string

	for _, file := range m.Files {
		if strings.HasSuffix(file.Path, ".html") {
			pages = append(pages, file.Path)
		}
	}

	slices.Sort(pages)

	return pages
}

// ModuleScript returns the path of the manifest's module script, the one
// file whose path matches ci/sitebuild's name pattern module.<h>.js (<h>: 16
// lowercase hexadecimal characters). No such file, or more than one, is an
// error.
func (m Manifest) ModuleScript() (string, error) {
	var found []string

	for _, file := range m.Files {
		if moduleScriptPattern.MatchString(file.Path) {
			found = append(found, file.Path)
		}
	}

	switch len(found) {
	case 0:
		return "", errors.New("the manifest lists no module script module.<16 hex>.js")
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("the manifest lists %d module scripts %q, want one", len(found), found)
	}
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
