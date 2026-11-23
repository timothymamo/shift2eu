package deploy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// ManifestName is the manifest's name in a bundle and its object key.
const ManifestName = "manifest.json"

// maxManifestBytes bounds a manifest: ParseManifest refuses more.
const maxManifestBytes = 1 << 20

// The names a bundle may hold besides manifest.json (doc.go, "The bundle it
// consumes"): the four pages, the four content-addressed assets and the two
// legal texts. Nothing else can become an object key.
var (
	pageName  = regexp.MustCompile(`^(?:index|inventory|privacy|verify)\.html$`)
	assetName = regexp.MustCompile(`^(?:style\.([0-9a-f]{16})\.css|(?:page|wasm_exec|module)\.([0-9a-f]{16})\.js)$`)
)

// The legal texts at the bundle's root: the licence and the NOTICE
// attribution, plain text under fixed names (issue #495).
const (
	licenseName = "LICENSE"
	noticeName  = "NOTICE"
)

// isLegalText reports whether name is exactly one of the two legal texts.
func isLegalText(name string) bool {
	return name == licenseName || name == noticeName
}

// tagPattern is a release tag, vMAJOR.MINOR.PATCH, each part at most nine
// digits with no leading zero, so it always fits an int.
var tagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)

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

// lookup returns the digest the manifest names for path, and whether it
// names path at all.
func (m Manifest) lookup(path string) (string, bool) {
	for _, f := range m.Files {
		if f.Path == path {
			return f.SHA256, true
		}
	}

	return "", false
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
	if len(data) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("manifest: over %d bytes", maxManifestBytes)
	}

	if !utf8.Valid(data) {
		return Manifest{}, errors.New("manifest: not valid UTF-8")
	}

	dec := json.NewDecoder(bytes.NewReader(data))

	m, err := decodeManifest(dec)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("manifest: data after the object")
	}

	return m, nil
}

// decodeManifest reads the manifest's one object, token by token, so that a
// duplicate or unknown field is refused, which encoding/json's decoding into
// a struct would accept or drop.
func decodeManifest(dec *json.Decoder) (Manifest, error) {
	if err := expectDelim(dec, '{', "the manifest"); err != nil {
		return Manifest{}, err
	}

	var (
		m                     Manifest
		seenVersion, seenFile bool
	)

	for dec.More() {
		name, err := fieldName(dec)
		if err != nil {
			return Manifest{}, err
		}

		switch name {
		case "version":
			if seenVersion {
				return Manifest{}, errors.New(`duplicate field "version"`)
			}

			seenVersion = true

			if m.Version, err = stringValue(dec, "version"); err != nil {
				return Manifest{}, err
			}

			if err := checkVersionText(m.Version); err != nil {
				return Manifest{}, err
			}
		case "files":
			if seenFile {
				return Manifest{}, errors.New(`duplicate field "files"`)
			}

			seenFile = true

			if m.Files, err = decodeFiles(dec); err != nil {
				return Manifest{}, err
			}
		default:
			return Manifest{}, fmt.Errorf("unknown field %s", quote(name))
		}
	}

	if err := expectDelim(dec, '}', "the manifest"); err != nil {
		return Manifest{}, err
	}

	if !seenVersion {
		return Manifest{}, errors.New(`missing field "version"`)
	}

	if !seenFile {
		return Manifest{}, errors.New(`missing field "files"`)
	}

	return m, nil
}

// decodeFiles reads the "files" array: objects of exactly "path" and
// "sha256", each path a bundle name, sorted and each once.
func decodeFiles(dec *json.Decoder) ([]ManifestFile, error) {
	if err := expectDelim(dec, '[', `field "files"`); err != nil {
		return nil, err
	}

	var files []ManifestFile

	for dec.More() {
		f, err := decodeFile(dec)
		if err != nil {
			return nil, err
		}

		if n := len(files); n > 0 && files[n-1].Path >= f.Path {
			return nil, fmt.Errorf("files not sorted by path, or listed twice, at %s", quote(f.Path))
		}

		files = append(files, f)
	}

	if err := expectDelim(dec, ']', `field "files"`); err != nil {
		return nil, err
	}

	return files, nil
}

// decodeFile reads one object of "files".
func decodeFile(dec *json.Decoder) (ManifestFile, error) {
	if err := expectDelim(dec, '{', "a file"); err != nil {
		return ManifestFile{}, err
	}

	var (
		f                 ManifestFile
		seenPath, seenSum bool
	)

	for dec.More() {
		name, err := fieldName(dec)
		if err != nil {
			return ManifestFile{}, err
		}

		switch name {
		case "path":
			if seenPath {
				return ManifestFile{}, errors.New(`duplicate field "path"`)
			}

			seenPath = true

			if f.Path, err = stringValue(dec, "path"); err != nil {
				return ManifestFile{}, err
			}

			if !isBundleFile(f.Path) {
				return ManifestFile{}, fmt.Errorf(`field "path" %s is not a file of a site bundle`, quote(f.Path))
			}
		case "sha256":
			if seenSum {
				return ManifestFile{}, errors.New(`duplicate field "sha256"`)
			}

			seenSum = true

			if f.SHA256, err = stringValue(dec, "sha256"); err != nil {
				return ManifestFile{}, err
			}

			if !hexDigest.MatchString(f.SHA256) {
				return ManifestFile{}, errors.New(`field "sha256" is not 64 lowercase hexadecimal characters`)
			}
		default:
			return ManifestFile{}, fmt.Errorf("unknown field %s in a file", quote(name))
		}
	}

	if err := expectDelim(dec, '}', "a file"); err != nil {
		return ManifestFile{}, err
	}

	if !seenPath || !seenSum {
		return ManifestFile{}, errors.New(`a file without field "path" or "sha256"`)
	}

	return f, nil
}

// expectDelim reads one token and refuses anything but delim.
func expectDelim(dec *json.Decoder, delim json.Delim, what string) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	if d, ok := tok.(json.Delim); !ok || d != delim {
		return fmt.Errorf("%s: want %q", what, string(delim))
	}

	return nil
}

// fieldName reads an object's next field name.
func fieldName(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}

	name, ok := tok.(string)
	if !ok {
		return "", errors.New("want a field name")
	}

	return name, nil
}

// stringValue reads a field's value, which must be a string.
func stringValue(dec *json.Decoder, field string) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", fmt.Errorf("field %q: %w", field, err)
	}

	s, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("field %q is not a string", field)
	}

	return s, nil
}

// checkVersionText refuses an empty version and one holding a control
// character. The bundle's own version is then compared with Config.Version.
func checkVersionText(v string) error {
	if v == "" {
		return errors.New(`field "version" is empty`)
	}

	if len(v) > 64 {
		return errors.New(`field "version" is over 64 bytes`)
	}

	for _, r := range v {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return errors.New(`field "version" holds a control character`)
		}
	}

	return nil
}

// isBundleFile reports whether name is one of the names a bundle may hold
// besides manifest.json.
func isBundleFile(name string) bool {
	return pageName.MatchString(name) || assetName.MatchString(name) || isLegalText(name)
}

// assetDigestPrefix returns the <h> of a content-addressed asset's name, and
// false for a page or a legal text.
func assetDigestPrefix(name string) (string, bool) {
	m := assetName.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}

	if m[1] != "" {
		return m[1], true
	}

	return m[2], true
}

// quote quotes untrusted text for an error: escaped, and cut to 64 bytes.
func quote(s string) string {
	const maxQuoted = 64

	if len(s) > maxQuoted {
		cut := maxQuoted
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}

		return strconv.Quote(s[:cut]) + "..."
	}

	return strconv.Quote(s)
}

// The footer's markup LiveVersion reads.
const (
	footerOpen = `<p class="footer-links">`
	footerEnd  = `</p>`
	versionTag = `<span class="mono" id="site-version">`
)

// versionAtEnd is the version span that ends the footer's paragraph.
var versionAtEnd = regexp.MustCompile(`<span class="mono" id="site-version">([^<]*)</span>$`)

// LiveVersion reads the shift2eu version a live page shows: the text of the
// one `<span class="mono" id="site-version">...</span></p>` that ends the
// page's footer (the `<p class="footer-links">` line of every page of
// site/). The page is
// untrusted: LiveVersion returns ErrNoLiveVersion when there is no such span
// or more than one, and when the text does not match
// ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$. A
// version it returns is only a candidate: Deploy trusts it only when it is
// also a tag of Config.Releases.
func LiveVersion(page []byte) (string, error) {
	if bytes.Count(page, []byte(footerOpen)) != 1 {
		return "", fmt.Errorf("%w: no single footer paragraph", ErrNoLiveVersion)
	}

	rest := page[bytes.Index(page, []byte(footerOpen))+len(footerOpen):]

	end := bytes.Index(rest, []byte(footerEnd))
	if end < 0 {
		return "", fmt.Errorf("%w: the footer paragraph does not end", ErrNoLiveVersion)
	}

	paragraph := rest[:end]

	if bytes.Count(paragraph, []byte(versionTag)) != 1 {
		return "", fmt.Errorf("%w: no single version span", ErrNoLiveVersion)
	}

	m := versionAtEnd.FindSubmatch(paragraph)
	if m == nil {
		return "", fmt.Errorf("%w: the version span does not end the footer", ErrNoLiveVersion)
	}

	version := string(m[1])
	if !tagPattern.MatchString(version) {
		return "", fmt.Errorf("%w: the footer shows no release version", ErrNoLiveVersion)
	}

	return version, nil
}

// compareTags compares two tags of tagPattern: -1, 0 or 1.
func compareTags(a, b string) int {
	pa, pb := tagPattern.FindStringSubmatch(a), tagPattern.FindStringSubmatch(b)

	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])

		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}

	return 0
}

// isTag reports whether s is a release tag, vMAJOR.MINOR.PATCH.
func isTag(s string) bool {
	return tagPattern.MatchString(s)
}
