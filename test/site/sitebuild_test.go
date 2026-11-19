// Package site_test holds the static checks of the site's bundle: the build
// ci/sitebuild assembles from site/, the pages it emits, and the rules PRD
// #297's three trust layers put on them.
//
// Every check reads one bundle, built once per package run by TestMain with
// `go run ./ci/sitebuild` into a temporary directory: the tests never run make,
// never start a browser and never touch the network. Three more builds happen
// in the tests that need them, each into its own temporary directory: a second
// build proves the bundle reproducible (Layer 3), and a build of a fixture copy
// of site/ whose HSTS record says waived, with -version set, proves the waived
// statement and the tagged version's links.
package site_test

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const (
	// taggedVersion is the version the tagged fixture build is given, so the
	// checks read the release branch of every version-dependent page and link
	// as well as the untagged one.
	taggedVersion = "v1.2.3"

	// buildTimeout bounds one site build, so a builder that hangs fails the
	// check instead of the package.
	buildTimeout = 10 * time.Minute
)

// bundle is one build of the site: the directory the builder wrote and the
// archive beside it.
type bundle struct {
	// dir is the bundle directory.
	dir string
	// archive is the archive the builder wrote.
	archive string
	// version is the -version the builder was given, "" for its default.
	version string
	// files are the bundle's files, relative to dir with forward slashes,
	// sorted.
	files []string
}

// manifest is manifest.json: the version the bundle shows and the path and
// SHA-256 of every other file in it.
type manifest struct {
	Version string         `json:"version"`
	Files   []manifestFile `json:"files"`
}

// manifestFile is one file of the manifest.
type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

var (
	// defaultBundle is the bundle every check reads unless it built one of its
	// own: site/ as the builder's default version, built once per package run.
	defaultBundle *bundle
	// buildFailure is why defaultBundle was not built. Every test reports it, so
	// a missing builder or a missing page names itself in the failures rather
	// than in one package-level message.
	buildFailure error
	// taggedBundle is the build of a fixture copy of site/ whose HSTS record says
	// waived, with -version taggedVersion: the two branches of the built pages
	// the default bundle does not show.
	taggedBundle *bundle
	// taggedFailure is why taggedBundle was not built.
	taggedFailure error
)

// TestMain builds the bundles the checks read, once each, and removes them
// afterwards.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "shift2eu-site-bundle-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create a temporary directory for the site bundle: %v\n", err)

		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)

	defaultBundle, buildFailure = buildSite(ctx, filepath.Join(dir, "default"), "", "")
	taggedBundle, taggedFailure = buildTaggedFixture(ctx, filepath.Join(dir, "tagged"))

	cancel()

	code := m.Run()

	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(os.Stderr, "remove the temporary site build in %s: %v\n", dir, err)
	}

	if (buildFailure != nil || taggedFailure != nil) && code == 0 {
		code = 1
	}

	os.Exit(code)
}

// buildTaggedFixture builds a fixture copy of site/ whose HSTS record says
// waived, with -version taggedVersion, into dir.
func buildTaggedFixture(ctx context.Context, dir string) (*bundle, error) {
	root, err := repositoryRoot()
	if err != nil {
		return nil, err
	}

	site, err := waivedSiteFixture(filepath.Join(dir, "site"), filepath.Join(root, "site"))
	if err != nil {
		return nil, err
	}

	return buildSite(ctx, filepath.Join(dir, "build"), taggedVersion, site)
}

// waivedSiteFixture copies the site sources at source to dir and gives the copy
// an HSTS record whose outcome is waived, the state PRD #297's Privacy statement
// describes for a maintainer who waives HSTS: the built privacy and verification
// pages must then carry the hsts-waived statement. It returns dir.
func waivedSiteFixture(dir, source string) (string, error) {
	if err := copyTree(source, dir); err != nil {
		return "", fmt.Errorf("copy the site sources from %s: %w", source, err)
	}

	records := filepath.Join(dir, "records")
	if err := os.MkdirAll(records, 0o750); err != nil {
		return "", fmt.Errorf("create %s: %w", records, err)
	}

	if err := os.WriteFile(filepath.Join(records, "hsts.json"), []byte("{\n  \"outcome\": \"waived\"\n}\n"), 0o600); err != nil {
		return "", fmt.Errorf("write the waived HSTS record: %w", err)
	}

	return dir, nil
}

// copyTree writes a copy of the files below source into dir: fs.WalkDir walks the
// source as a file system, so no path the walk reports is opened as a file of
// the host's.
func copyTree(source, dir string) error {
	files := os.DirFS(source)

	return fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		target := filepath.Join(dir, filepath.FromSlash(name))

		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}

		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0o600)
	})
}

// buildSite runs the builder into a temporary directory of its own and returns
// what it wrote. version and site are passed as -version and -site when they are
// not empty, so the tagged and the waived builds go through the same program as
// the default one.
func buildSite(ctx context.Context, dir, version, site string) (*bundle, error) {
	root, err := repositoryRoot()
	if err != nil {
		return nil, err
	}

	out := filepath.Join(dir, "bundle")
	archive := filepath.Join(dir, "shift2eu-site.tar.gz")

	args := []string{"run", "./ci/sitebuild", "-out", out, "-archive", archive}
	if version != "" {
		args = append(args, "-version", version)
	}

	if site != "" {
		args = append(args, "-site", site)
	}

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	cmd.Env = buildEnv()

	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	files, err := filesUnder(out)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("go %s wrote no file into %s", strings.Join(args, " "), out)
	}

	return &bundle{dir: out, archive: archive, version: version, files: files}, nil
}

// repositoryRoot returns the module root, which the site tests reach as two
// directories above the package directory go test runs them in. It returns an
// error rather than failing a test, so TestMain can report one.
func repositoryRoot() (string, error) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return "", fmt.Errorf("resolve the module root: %w", err)
	}

	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("the module root %s holds no go.mod: %w", root, err)
	}

	return root, nil
}

// theRepositoryRoot is repositoryRoot for a test.
func theRepositoryRoot(t *testing.T) string {
	t.Helper()

	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}

	return root
}

// buildEnv is the environment the builder and the module build run in: the
// test's own, with the settings make fixes, so a developer's GOFLAGS, GOWORK or
// GOOS cannot decide what is built or how.
func buildEnv() []string {
	drop := map[string]bool{
		"GOOS":        true,
		"GOARCH":      true,
		"CGO_ENABLED": true,
		"GOFLAGS":     true,
		"GOWORK":      true,
	}

	env := make([]string, 0, len(os.Environ())+2)

	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if drop[name] {
			continue
		}

		env = append(env, entry)
	}

	return append(env, "GOFLAGS=-mod=readonly", "GOWORK=off")
}

// filesUnder returns the files below dir, relative to it with forward slashes,
// sorted.
func filesUnder(dir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		files = append(files, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the bundle %s: %w", dir, err)
	}

	slices.Sort(files)

	return files, nil
}

// theBundle returns the bundle TestMain built, failing the test with the
// build's own failure: every check names what it could not read.
func theBundle(t *testing.T) *bundle {
	t.Helper()

	if buildFailure != nil {
		t.Fatalf("the site bundle was not built, so this check read nothing: %v", buildFailure)
	}

	if defaultBundle == nil {
		t.Fatal("the site bundle was not built, so this check read nothing")
	}

	return defaultBundle
}

// theTaggedBundle returns the build of the waived fixture at taggedVersion,
// failing the test with that build's own failure.
func theTaggedBundle(t *testing.T) *bundle {
	t.Helper()

	if taggedFailure != nil {
		t.Fatalf("the tagged, waived build of the site was not built, so this check read nothing: %v", taggedFailure)
	}

	if taggedBundle == nil {
		t.Fatal("the tagged, waived build of the site was not built, so this check read nothing")
	}

	return taggedBundle
}

// read returns one file of the bundle.
func (b *bundle) read(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(b.dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read the bundle file %s: %v", name, err)
	}

	return data
}

// html reads one HTML page of the bundle.
func (b *bundle) html(t *testing.T, name string) *document {
	t.Helper()

	return parseHTML(t, name, b.read(t, name))
}

// manifest reads the bundle's manifest.json.
func (b *bundle) manifest(t *testing.T) manifest {
	t.Helper()

	data := b.read(t, "manifest.json")

	var m manifest

	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&m); err != nil {
		t.Fatalf("decode manifest.json: %v", err)
	}

	if decoder.More() {
		t.Fatalf("manifest.json holds more than one JSON value")
	}

	return m
}

// asset returns the name and the bytes of the bundle's one asset of a kind:
// asset(t, "style", ".css") finds style.<h>.css and fails the test when the
// bundle holds none or more than one.
func (b *bundle) asset(t *testing.T, prefix, suffix string) (string, []byte) {
	t.Helper()

	var found []string

	for _, name := range b.files {
		if !strings.HasPrefix(name, prefix+".") || !strings.HasSuffix(name, suffix) {
			continue
		}

		hash := strings.TrimSuffix(strings.TrimPrefix(name, prefix+"."), suffix)
		if len(hash) != 16 || strings.Trim(hash, "0123456789abcdef") != "" {
			continue
		}

		found = append(found, name)
	}

	if len(found) != 1 {
		t.Fatalf("the bundle holds %d %s.<hash>%s assets (%v), want exactly one", len(found), prefix, suffix, found)
	}

	return found[0], b.read(t, found[0])
}

// goEnv returns one go env value, such as GOROOT, from the toolchain that builds
// the site.
func goEnv(t *testing.T, name string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "env", name)
	cmd.Dir = theRepositoryRoot(t)
	cmd.Env = buildEnv()

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}

	return strings.TrimSpace(string(output))
}

// buildModule builds the site's WebAssembly module as the site ships it:
// -trimpath with no VCS stamp, GOOS=js, GOARCH=wasm and CGO_ENABLED=0 from
// ./cmd/shift2eu-web. No VCS stamp keeps the bundle independent of the state of
// the checkout that builds it, as the reproducibility criterion requires (PRD
// #297, Layer 3: "a fixed or no VCS stamp").
func buildModule(t *testing.T, dir string) []byte {
	t.Helper()

	module := filepath.Join(dir, "shift2eu-web.wasm")

	cmd := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-buildvcs=false", "-o", module, "./cmd/shift2eu-web")
	cmd.Dir = theRepositoryRoot(t)
	cmd.Env = append(buildEnv(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")

	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/shift2eu-web with GOOS=js GOARCH=wasm: %v\n%s", err, strings.TrimSpace(string(output)))
	}

	data, err := os.ReadFile(module)
	if err != nil {
		t.Fatalf("read the built module: %v", err)
	}

	return data
}

// sha256Hex returns the lower-case hexadecimal SHA-256 of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// sha384Base64 returns the base64 SHA-384 of data, as a Subresource Integrity
// value spells it after its "sha384-" prefix.
func sha384Base64(data []byte) string {
	sum := sha512.Sum384(data)

	return base64.StdEncoding.EncodeToString(sum[:])
}

// diffNames reports a sorted list comparison.
func diffNames(want, got []string) string {
	return cmp.Diff(want, got)
}
