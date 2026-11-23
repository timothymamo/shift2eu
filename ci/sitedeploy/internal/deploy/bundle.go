package deploy

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// The bounds on what Deploy reads from the bundle into memory: one file and
// the whole bundle. They bound memory, not the site's size budgets (ADR
// 0035, ADR 0042), which the size test and the live check hold.
const (
	maxFileBytes   = 32 << 20
	maxBundleBytes = 64 << 20
)

// readBundle reads and checks the bundle against its manifest (doc.go, "The
// bundle it consumes"): the manifest parses and names version, every listed
// file is present, a regular file, of its digest and, for an asset, named
// after it, the manifest lists exactly the four pages and one asset of each
// kind (checkComplete), and the bundle holds no file the manifest does not
// list. The
// files it returns are every listed file and manifest.json, by name.
func readBundle(bundle fs.FS, version string) (Manifest, map[string][]byte, error) {
	data, err := readBounded(bundle, ManifestName, maxManifestBytes)
	if err != nil {
		return Manifest{}, nil, err
	}

	m, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, nil, err
	}

	if m.Version != version {
		return Manifest{}, nil, fmt.Errorf("the manifest's version %s is not %s", quote(m.Version), quote(version))
	}

	if err := checkComplete(m); err != nil {
		return Manifest{}, nil, err
	}

	entries, err := fs.ReadDir(bundle, ".")
	if err != nil {
		return Manifest{}, nil, errors.New("the bundle's directory cannot be read")
	}

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return Manifest{}, nil, fmt.Errorf("the bundle holds %s, which is not a regular file", quote(entry.Name()))
		}

		if entry.Name() == ManifestName {
			continue
		}

		if _, ok := m.lookup(entry.Name()); !ok {
			return Manifest{}, nil, fmt.Errorf("the bundle holds %s, which the manifest does not list", quote(entry.Name()))
		}
	}

	// The manifest is uploaded from the bytes checked here, as every other
	// file is, so a bundle changed on disk after the check is not uploaded.
	files := make(map[string][]byte, len(m.Files)+1)
	files[ManifestName] = data
	total := len(data)

	for _, f := range m.Files {
		body, err := readBounded(bundle, f.Path, maxFileBytes)
		if err != nil {
			return Manifest{}, nil, err
		}

		total += len(body)
		if total > maxBundleBytes {
			return Manifest{}, nil, fmt.Errorf("the bundle is over %d bytes", maxBundleBytes)
		}

		sum := HashPayload(body)
		if sum != f.SHA256 {
			return Manifest{}, nil, fmt.Errorf("the SHA-256 of %s is not the manifest's", quote(f.Path))
		}

		if prefix, ok := assetDigestPrefix(f.Path); ok && prefix != sum[:len(prefix)] {
			return Manifest{}, nil, fmt.Errorf("the asset %s is not named after its SHA-256", quote(f.Path))
		}

		files[f.Path] = body
	}

	return m, files, nil
}

// bundleKinds is what a complete bundle's manifest lists besides
// manifest.json: each of the four pages, one asset of each kind and each of
// the two legal texts.
var bundleKinds = []string{
	"index.html", "inventory.html", "privacy.html", "verify.html",
	"style", "page", "wasm_exec", "module",
	licenseName, noticeName,
}

// checkComplete holds the manifest of the bundle to deploy to exactly the
// four pages, one asset of each kind (style, page script, wasm_exec, module),
// LICENSE and NOTICE, so a subset of a bundle, or one with two of a kind, is
// never deployed. The release manifests the guard and the listing read are
// not held to it.
func checkComplete(m Manifest) error {
	counts := make(map[string]int, len(bundleKinds))

	for _, f := range m.Files {
		kind := f.Path
		if _, ok := assetDigestPrefix(f.Path); ok {
			kind, _, _ = strings.Cut(f.Path, ".")
		}

		counts[kind]++
	}

	for _, kind := range bundleKinds {
		if counts[kind] != 1 {
			return fmt.Errorf("the manifest lists %d of %s, want exactly one", counts[kind], kind)
		}
	}

	// ParseManifest admits no other name (isBundleFile), so the counts of one
	// for every kind of bundleKinds are the whole manifest.
	return nil
}

// readBounded reads the regular file name of fsys, refusing more than limit
// bytes. Its errors name the file, never the file system's own text.
func readBounded(fsys fs.FS, name string, limit int64) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", quote(name), fs.ErrNotExist)
		}

		return nil, fmt.Errorf("%s cannot be opened", quote(name))
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", quote(name))
	}

	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read", quote(name))
	}

	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is over %d bytes", quote(name), limit)
	}

	return data, nil
}
