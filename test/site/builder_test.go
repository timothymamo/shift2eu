package site_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/upcloud"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/version"
)

// This file holds the checks of the build itself (PRD #297 milestone 2, and the
// reproducibility criterion of Layer 3): the files the bundle holds, their
// content-addressed names, the module script's one fixed template, wasm_exec.js
// as the toolchain ships it, the manifest, the version and the catalog snapshot
// dates the pages show, the archive, and that two builds of the same commit are
// byte-identical.

// pageNames are the bundle's HTML pages. inventory.html is the page that shows
// how to build an inventory for each source (issue #373, item 15), and
// about.html is the maintainer's About page (issue #528), so each carries every
// per-page rule the other pages carry.
var pageNames = []string{"about.html", "index.html", "inventory.html", "privacy.html", "verify.html"}

// licenseFiles are the files the project redistributes beside its work: the
// Apache License 2.0 and the NOTICE attribution Apache-2.0 section 4 asks every
// redistribution to carry (issue #495). They sit at the bundle's root, as at
// the repository's.
var licenseFiles = []string{"LICENSE", "NOTICE"}

// assetKinds are the bundle's content-addressed assets: a prefix, the suffix of
// its file name, and the cache lifetime reason it is addressed by content.
var assetKinds = []struct{ prefix, suffix string }{
	{"style", ".css"},
	{"page", ".js"},
	{"wasm_exec", ".js"},
	{"module", ".js"},
}

// wantedFileNames returns the bundle's contract set: the pages, the manifest,
// the one content-addressed file of each kind, and the licence and the NOTICE
// attribution Apache-2.0 section 4 asks a redistribution to carry (issue #495).
func wantedFileNames(t *testing.T, bundle *bundle) []string {
	t.Helper()

	want := slices.Clone(pageNames)
	want = append(want, "manifest.json")
	want = append(want, licenseFiles...)

	for _, kind := range assetKinds {
		name, _ := bundle.asset(t, kind.prefix, kind.suffix)
		want = append(want, name)
	}

	slices.Sort(want)

	return want
}

// TestBundleHoldsTheContractFiles pins the bundle's file set: the three pages,
// the manifest, the licence and the NOTICE attribution, and one
// content-addressed file of each kind, and nothing else. The site ships no
// favicon, site/records/ is never bundled (milestone 4 fills it), and a stray
// file would be deployed without a manifest entry.
func TestBundleHoldsTheContractFiles(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	if diff := cmp.Diff(wantedFileNames(t, bundle), bundle.files); diff != "" {
		t.Errorf("the bundle's files differ from the contract set (-want +got):\n%s", diff)
	}
}

// TestAssetNamesAreContentAddressed pins that every asset's name carries the
// first 16 hexadecimal characters of the SHA-256 of its own bytes, so a release
// caches it forever and a changed file gets a new name (PRD #297, Size and
// speed).
func TestAssetNamesAreContentAddressed(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, kind := range assetKinds {
		t.Run(kind.prefix, func(t *testing.T) {
			t.Parallel()

			name, data := bundle.asset(t, kind.prefix, kind.suffix)

			hash := strings.TrimSuffix(strings.TrimPrefix(name, kind.prefix+"."), kind.suffix)
			if want := sha256Hex(data)[:16]; hash != want {
				t.Errorf("%s is named for the hash %q, want the first 16 hexadecimal characters of the SHA-256 of its bytes, %q", name, hash, want)
			}
		})
	}
}

// moduleScriptPrefix and moduleScriptSuffix bracket the module script's one
// assignment: PRD #297 (Layer 2) fixes the whole file as
// globalThis.__shift2euModule="<base64>"; followed by one newline.
const (
	moduleScriptPrefix = `globalThis.__shift2euModule="`
	moduleScriptSuffix = "\";\n"
	moduleScriptBase64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
)

// TestModuleScriptIsTheFixedTemplate pins that the module script carries no
// code, only the module: the one template, the standard base64 alphabet and
// nothing else between the quotes, and that the base64 decodes byte for byte to
// the module built with -trimpath, no VCS stamp, GOOS=js GOARCH=wasm and
// CGO_ENABLED=0.
func TestModuleScriptIsTheFixedTemplate(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "module", ".js")

	script := string(data)

	if !strings.HasPrefix(script, moduleScriptPrefix) || !strings.HasSuffix(script, moduleScriptSuffix) {
		t.Fatalf("%s is not the fixed template `globalThis.__shift2euModule=\"<base64>\";` followed by one newline; it starts %q and ends %q",
			name, firstBytes(script, 40), lastBytes(script, 40))
	}

	body := script[len(moduleScriptPrefix) : len(script)-len(moduleScriptSuffix)]
	if strings.Trim(body, moduleScriptBase64) != "" {
		t.Errorf("%s holds %q between the quotes, which is not the standard base64 alphabet A-Z, a-z, 0-9, +, / and trailing =: the module script may carry no code of its own",
			name, strings.TrimLeftFunc(body, func(r rune) bool { return strings.ContainsRune(moduleScriptBase64, r) }))
	}

	decoded, err := base64.StdEncoding.Strict().DecodeString(body)
	if err != nil {
		t.Fatalf("%s does not decode as base64: %v", name, err)
	}

	built := buildModule(t, t.TempDir())

	if !bytes.Equal(decoded, built) {
		t.Errorf("%s decodes to %d bytes, but the module built with -trimpath, -buildvcs=false, GOOS=js GOARCH=wasm and CGO_ENABLED=0 from ./cmd/shift2eu-web is %d bytes: the page must run the module this commit builds",
			name, len(decoded), len(built))
	}
}

// TestWasmExecIsTheToolchains pins Go's wasm_exec.js as toolchain code: the
// bundled file is byte-identical to the one the toolchain that builds the module
// ships, at $(go env GOROOT)/lib/wasm/wasm_exec.js, and is never fetched or
// committed (PRD #297, wasm_exec.js).
func TestWasmExecIsTheToolchains(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "wasm_exec", ".js")

	toolchainPath := filepath.Join(goEnv(t, "GOROOT"), "lib", "wasm", "wasm_exec.js")

	toolchain, err := os.ReadFile(toolchainPath)
	if err != nil {
		t.Fatalf("read the toolchain's wasm_exec.js at %s: %v", toolchainPath, err)
	}

	if !bytes.Equal(data, toolchain) {
		t.Errorf("the bundle's %s (%d bytes) is not byte-identical to the toolchain's %s (%d bytes): wasm_exec.js is trusted as toolchain code and pinned to it, not scanned",
			name, len(data), toolchainPath, len(toolchain))
	}
}

// TestManifestListsEveryFile pins manifest.json: the version the bundle shows,
// and every other file of the bundle with its SHA-256, sorted by path, with no
// file under records/ (the HSTS and preload records stay in the repository,
// PRD #297, Layer 3).
func TestManifestListsEveryFile(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)
	manifest := bundle.manifest(t)

	if want := devVersion(t); manifest.Version != want {
		t.Errorf("manifest.json says the version is %q, want %q, the value internal/version holds when the build is untagged", manifest.Version, want)
	}

	want := make([]manifestFile, 0, len(bundle.files))

	for _, name := range bundle.files {
		if strings.HasPrefix(name, "records/") {
			t.Errorf("the bundle holds %s: site/records/ is never bundled", name)
		}

		if name == "manifest.json" {
			continue
		}

		want = append(want, manifestFile{Path: name, SHA256: sha256Hex(bundle.read(t, name))})
	}

	if diff := cmp.Diff(want, manifest.Files); diff != "" {
		t.Errorf("manifest.json's files differ from the bundle's (-want +got):\n%s", diff)
	}
}

// TestBundleShipsLicenseAndNotice pins issue #495: the site bundle is a
// redistribution of the project, so Apache-2.0 section 4 asks it to carry the
// licence and the NOTICE attribution beside the pages it publishes. The bundle
// holds both at its root, byte for byte as the repository root holds them, and
// manifest.json lists each with its SHA-256 like every other file.
//
// The archive and the reproducibility criterion cover them too, without a
// check of their own: TestArchiveHoldsTheBundleWithFixedTimes holds the
// archive's entries to bundle.files and each entry to the bundle's bytes, and
// TestTheBundleIsReproducible compares every file and both archives of two
// builds. A file missing from either half makes those checks fail.
func TestBundleShipsLicenseAndNotice(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)
	root := theRepositoryRoot(t)
	manifest := bundle.manifest(t)

	for _, name := range licenseFiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if !slices.Contains(bundle.files, name) {
				t.Fatalf("the bundle holds no %s: Apache-2.0 section 4 asks every redistribution to carry the licence and the NOTICE attribution, and the site bundle is one (issue #495); the bundle's files are %v",
					name, bundle.files)
			}

			want, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatalf("read the repository's %s: %v", name, err)
			}

			if got := bundle.read(t, name); !bytes.Equal(got, want) {
				t.Errorf("the bundle's %s holds %d bytes, the repository's %d: the bundle ships the root %s byte for byte, so the copy the site deploys cannot drift from the one the archive and the image carry",
					name, len(got), len(want), name)
			}

			entry, listed := manifestEntry(manifest, name)

			switch {
			case !listed:
				t.Errorf("manifest.json lists no %s: every file of the bundle has an entry with its SHA-256, so the host serves and caches the file the build wrote (issue #495); manifest.json's files are %v",
					name, manifest.Files)
			case entry.SHA256 != sha256Hex(want):
				t.Errorf("manifest.json lists %s with the SHA-256 %q, want %q, the hash of the file the repository root holds: the manifest is the bundle's own index",
					name, entry.SHA256, sha256Hex(want))
			}
		})
	}
}

// manifestEntry returns the manifest's entry for the bundle file name, and
// whether it lists one.
func manifestEntry(m manifest, name string) (manifestFile, bool) {
	for _, entry := range m.Files {
		if entry.Path == name {
			return entry, true
		}
	}

	return manifestFile{}, false
}

// TestPagesShowTheVersionAndTheSnapshotDates pins what every page must say about
// the build the user runs (PRD #297, Page content): the shift2eu version that
// built it, which is internal/version's development value when the build is
// untagged, exactly as `shift2eu version` prints it; and, on the assessment
// page, the date of each embedded catalog snapshot, so the prices a user is
// shown are dated before any assessment runs.
func TestPagesShowTheVersionAndTheSnapshotDates(t *testing.T) {
	t.Parallel()

	untagged := theBundle(t)

	for _, page := range pageNames {
		t.Run("untagged/"+page, func(t *testing.T) {
			t.Parallel()

			text := untagged.html(t, page).body(t).textContent()

			if want := devVersion(t); !strings.Contains(text, want) {
				t.Errorf("%s shows no version: the page shows the version that built it, and an untagged build's is %q, the value `shift2eu version` prints", page, want)
			}
		})
	}

	tagged := theTaggedBundle(t)

	for _, page := range pageNames {
		t.Run("tagged/"+page, func(t *testing.T) {
			t.Parallel()

			text := tagged.html(t, page).body(t).textContent()

			if !strings.Contains(text, taggedVersion) {
				t.Errorf("%s built with -version %s does not show it: the page shows the version that built it", page, taggedVersion)
			}
		})
	}

	assessment := untagged.html(t, "index.html").body(t).textContent()

	for _, snapshot := range snapshotDates(t) {
		t.Run("snapshot/"+snapshot.target, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(assessment, snapshot.date) {
				t.Errorf("the assessment page does not say that the %s catalog snapshot is from %s: the page dates every catalog its estimates come from",
					snapshot.target, snapshot.date)
			}
		})
	}
}

// snapshot is one embedded catalog's target and its snapshot date, formatted as
// the reports format it.
type snapshot struct {
	target string
	date   string
}

// snapshotDates returns the three embedded catalogs' snapshot dates, read from
// the catalogs themselves, so the check reads the date the site was built from
// rather than a copy of it.
func snapshotDates(t *testing.T) []snapshot {
	t.Helper()

	catalogs := []struct {
		target  string
		catalog func(context.Context) (catalog.Catalog, error)
	}{
		{"OVHcloud", ovhcloud.New().Catalog},
		{"Scaleway", scaleway.New().Catalog},
		{"UpCloud", upcloud.New().Catalog},
	}

	out := make([]snapshot, 0, len(catalogs))

	for _, entry := range catalogs {
		doc, err := entry.catalog(t.Context())
		if err != nil {
			t.Fatalf("read the %s catalog: %v", entry.target, err)
		}

		out = append(out, snapshot{target: entry.target, date: doc.Snapshot().RetrievedOn.Format("2006-01-02")})
	}

	return out
}

// TestTargetCardsShowTheCountry pins the one line each target card carries about
// where its provider is based (issue #373, EU targets, item 05-1): the provider's
// name followed by its country in brackets, and no head-office city, which the
// maintainer removed as noise. ci/sitebuild writes the three strings by hand, so
// the check pins them here: nothing else would catch an empty line, a typo, a
// swapped country or a city that came back.
func TestTargetCardsShowTheCountry(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"OVHcloud": "(France)",
		"Scaleway": "(France)",
		"UpCloud":  "(Finland)",
	}

	// The cities the cards showed before item 05-1: a card that names one still
	// shows the head office rather than only the country.
	cities := []string{"Roubaix", "Paris", "Helsinki"}

	doc := theBundle(t).html(t, "index.html")

	cards := 0
	got := make(map[string]string, len(want))

	for _, card := range doc.all() {
		if !hasClass(card, "target") {
			continue
		}

		cards++

		text := collapseSpace(card.textContent())

		label := ""

		for name := range want {
			if strings.HasPrefix(text, name) {
				label = name
			}
		}

		if label == "" {
			t.Fatalf("a target card reads %q, want it to begin with one of the providers' names: each card names its provider (issue #373, EU targets)", text)
		}

		if country := want[label]; !strings.Contains(text, country) {
			t.Errorf("the %s target card reads %q, want it to name the country of the provider's head office as %q (issue #373, item 05-1)",
				label, text, country)
		}

		for _, city := range cities {
			if strings.Contains(text, city) {
				t.Errorf("the %s target card names the city %q in %q, want only the country in brackets: the head office's city was removed (issue #373, item 05-1)",
					label, city, text)
			}
		}

		got[label] = text
	}

	if cards != len(want) {
		t.Fatalf("index.html holds %d target cards, want %d: one per embedded catalog (issue #373, EU targets)", cards, len(want))
	}

	if len(got) != len(want) {
		t.Errorf("the target cards name %d of the %d providers: %v", len(got), len(want), got)
	}
}

// TestTargetCardsDateTheirCatalog pins the line each target card carries about
// its catalog (issue #373, item 05-3): "Catalog retrieved <date>", the date the
// catalog's prices were retrieved, and never the "Catalog snapshot of" wording
// the maintainer asked to replace.
func TestTargetCardsDateTheirCatalog(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, "index.html")
	dates := make(map[string]string, 3)

	for _, snapshot := range snapshotDates(t) {
		dates[snapshot.target] = snapshot.date
	}

	cards := 0

	for _, card := range doc.all() {
		if !hasClass(card, "target") {
			continue
		}

		cards++

		text := collapseSpace(card.textContent())

		dated := false

		for target, date := range dates {
			if strings.Contains(text, target) && strings.Contains(text, "Catalog retrieved "+date) {
				dated = true
			}
		}

		if !dated {
			t.Errorf("the target card reads %q, want it to say \"Catalog retrieved <date>\" with its catalog's date %v (issue #373, item 05-3)",
				text, dates)
		}

		if strings.Contains(text, "Catalog snapshot of") {
			t.Errorf("the target card reads %q, want \"Catalog retrieved <date>\" instead of \"Catalog snapshot of\" (issue #373, item 05-3)", text)
		}
	}

	if cards == 0 {
		t.Fatal("index.html holds no target card, so the check read nothing: one card per embedded catalog")
	}
}

// TestEstimatesNoteIsTheListPriceNote pins the one line under the EU targets
// (issue #373, item 05-4): the maintainer replaced the page's long paragraph with
// the artifact's sentence, which says what the estimates are and on what basis.
func TestEstimatesNoteIsTheListPriceNote(t *testing.T) {
	t.Parallel()

	const note = "Estimates are list prices on each catalog's retrieval date, excluding VAT and discounts."

	text := collapseSpace(theBundle(t).html(t, "index.html").body(t).textContent())

	if !strings.Contains(text, note) {
		t.Errorf("index.html's text does not carry %q, want the one-line note under the EU targets (issue #373, item 05-4); the page's text is:\n%s",
			note, excerptText(text, 2000))
	}
}

// excerptText returns the first limit bytes of text, for a failure message that
// would otherwise print a whole page.
func excerptText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}

	return text[:limit] + "…"
}

// TestArchiveHoldsTheBundleWithFixedTimes pins the archive a release publishes
// beside the bundle (PRD #297, Layer 3): one entry per bundle file in sorted
// order, each byte-identical to the file, with fixed owners, one fixed
// modification time and no execute bit, and a gzip stream of zero modification
// time. The reproducibility test proves the rest.
func TestArchiveHoldsTheBundleWithFixedTimes(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	file, err := os.Open(bundle.archive)
	if err != nil {
		t.Fatalf("open the archive %s: %v", bundle.archive, err)
	}
	defer file.Close()

	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("read %s as gzip: %v", bundle.archive, err)
	}

	if !compressed.ModTime.IsZero() {
		t.Errorf("the archive's gzip header carries the modification time %v, want zero: a reproducible archive compresses with zero mtime (PRD #297, Layer 3)",
			compressed.ModTime)
	}

	entries := readTar(t, compressed)
	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		names = append(names, entry.name)
	}

	if diff := cmp.Diff(bundle.files, names); diff != "" {
		t.Errorf("the archive's entries differ from the bundle's files, in sorted order (-want +got):\n%s", diff)
	}

	for _, entry := range entries {
		if entry.mode&0o111 != 0 {
			t.Errorf("the archive entry %s is mode %04o, want no execute bit: the archive's modes are fixed", entry.name, entry.mode)
		}

		if entry.uid != entries[0].uid || entry.gid != entries[0].gid {
			t.Errorf("the archive entry %s is owned by %d:%d, want the same fixed owner as %s, %d:%d",
				entry.name, entry.uid, entry.gid, entries[0].name, entries[0].uid, entries[0].gid)
		}

		if entry.mode != entries[0].mode {
			t.Errorf("the archive entry %s is mode %04o, want the same fixed mode as %s, %04o", entry.name, entry.mode, entries[0].name, entries[0].mode)
		}

		if !entry.modTime.Equal(entries[0].modTime) {
			t.Errorf("the archive entry %s is dated %v, want the same fixed time as %s, %v: a reproducible archive fixes every entry's time",
				entry.name, entry.modTime, entries[0].name, entries[0].modTime)
		}

		if !slices.Contains(bundle.files, entry.name) {
			t.Errorf("the archive holds %s, which the bundle does not", entry.name)

			continue
		}

		if want := bundle.read(t, entry.name); !bytes.Equal(entry.content, want) {
			t.Errorf("the archive entry %s holds %d bytes, the bundle's file %d: the archive is the bundle's files",
				entry.name, len(entry.content), len(want))
		}
	}

	// A time after the archive could have been built is a time the builder took
	// from the clock, not a fixed one.
	if fixedBefore := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC); !entries[0].modTime.Before(fixedBefore) {
		t.Errorf("the archive's entries are dated %v, want a fixed time: a time near the build's own makes the archive depend on the clock",
			entries[0].modTime)
	}
}

// tarEntry is one regular file of the archive.
type tarEntry struct {
	name    string
	mode    int64
	uid     int
	gid     int
	modTime time.Time
	content []byte
}

// readTar reads every entry of the tar stream, failing the test on one that is
// no regular file.
func readTar(t *testing.T, reader io.Reader) []tarEntry {
	t.Helper()

	var entries []tarEntry

	archive := tar.NewReader(reader)

	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}

		if header.Typeflag != tar.TypeReg {
			t.Fatalf("the archive's entry %s is of type %q, want a regular file", header.Name, header.Typeflag)
		}

		content, err := io.ReadAll(archive)
		if err != nil {
			t.Fatalf("read the archive's entry %s: %v", header.Name, err)
		}

		entries = append(entries, tarEntry{
			name:    header.Name,
			mode:    header.Mode,
			uid:     header.Uid,
			gid:     header.Gid,
			modTime: header.ModTime,
			content: content,
		})
	}

	if len(entries) == 0 {
		t.Fatal("the archive holds no entry: it must hold the bundle's files")
	}

	return entries
}

// TestTheBundleIsReproducible pins the reproducibility criterion of PRD #297's
// Layer 3: two builds of the same commit with the pinned toolchain, each into a
// temporary directory of its own, give byte-identical files and a
// byte-identical archive. The two builds sit at different paths, so a path that
// leaked into the bundle would fail this test too.
func TestTheBundleIsReproducible(t *testing.T) {
	t.Parallel()

	first := theBundle(t)

	second, err := buildSite(t.Context(), t.TempDir(), "", "")
	if err != nil {
		t.Fatalf("the second build of the site: %v", err)
	}

	if diff := cmp.Diff(first.files, second.files); diff != "" {
		t.Errorf("the two builds of the same commit hold different files (-first +second):\n%s", diff)
	}

	for _, name := range first.files {
		if !bytes.Equal(first.read(t, name), second.read(t, name)) {
			t.Errorf("the file %s differs between two builds of the same commit: the bundle is not reproducible (PRD #297, Layer 3)", name)
		}
	}

	firstArchive, err := os.ReadFile(first.archive)
	if err != nil {
		t.Fatalf("read the first archive %s: %v", first.archive, err)
	}

	secondArchive, err := os.ReadFile(second.archive)
	if err != nil {
		t.Fatalf("read the second archive %s: %v", second.archive, err)
	}

	if !bytes.Equal(firstArchive, secondArchive) {
		t.Errorf("the two archives of the same commit differ in %d of %d bytes: the archive is not reproducible, so a release cannot attest it (PRD #297, Layer 3)",
			countDifferingBytes(firstArchive, secondArchive), len(firstArchive))
	}
}

// countDifferingBytes counts the byte positions at which the two archives
// differ, which tells a digest mismatch from a wholly different archive.
func countDifferingBytes(first, second []byte) int {
	different := 0

	for i := range min(len(first), len(second)) {
		if first[i] != second[i] {
			different++
		}
	}

	return different + abs(len(first)-len(second))
}

// abs returns the absolute value of n.
func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

// devVersion returns the version string internal/version holds when the build is
// untagged: the value `shift2eu version` prints after the program's name, which
// the site's pages show too (PRD #297, Page content).
func devVersion(t *testing.T) string {
	t.Helper()

	fields := strings.Fields(version.String())
	if len(fields) < 2 {
		t.Fatalf("version.String() is %q, want the program's name and then the version, so this check can read the untagged version from it", version.String())
	}

	return fields[1]
}

// firstBytes returns at most n leading bytes of s, for a failure message.
func firstBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "..."
}

// lastBytes returns at most n trailing bytes of s, for a failure message.
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return "..." + s[len(s)-n:]
}
