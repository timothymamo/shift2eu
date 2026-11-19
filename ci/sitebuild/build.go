package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/upcloud"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/version"
)

// options are the builder's flags.
type options struct {
	out     string
	archive string
	version string
	site    string
}

// pages are the site's HTML pages: each is a template under -site and a file of
// the bundle of the same name.
var pages = []string{"index.html", "privacy.html", "verify.html"}

// The site's other sources, copied byte for byte under content-addressed names.
const (
	styleSource = "style.css"
	pageSource  = "page.js"
)

// hstsRecord is the HSTS record milestone 4 commits, relative to -site.
const hstsRecord = "records/hsts.json"

// moduleScriptPrefix and moduleScriptSuffix are the module script's one fixed
// template around the module's base64 (PRD #297, Layer 2).
const (
	moduleScriptPrefix = `globalThis.__shift2euModule="`
	moduleScriptSuffix = "\";\n"
)

// tagPattern is the form of a release tag -version accepts.
var tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

// target is one target catalog the pages name: its key, as --target takes it,
// the name the reports give it, and its catalog.
type target struct {
	key     string
	name    string
	catalog func(context.Context) (catalog.Catalog, error)
}

// targets are the three embedded catalogs, in the order the page lists them.
var targets = []target{
	{key: "ovhcloud", name: "OVHcloud", catalog: ovhcloud.New().Catalog},
	{key: "scaleway", name: "Scaleway", catalog: scaleway.New().Catalog},
	{key: "upcloud", name: "UpCloud", catalog: upcloud.New().Catalog},
}

// examples are the bundled example inventories, by their directory under
// examples/, in the order the page lists them.
var examples = []struct{ key, name string }{
	{key: "aws", name: "AWS"},
	{key: "azure", name: "Azure"},
	{key: "gcp", name: "GCP"},
}

// pageData is what the page templates are rendered with.
type pageData struct {
	// Version is the shift2eu version the pages show.
	Version string
	// Ref is the git ref the links to the repository's guides name: the tag, or
	// main when the build is untagged.
	Ref string
	// GoVersion is the version of the Go toolchain that built the module.
	GoVersion string
	// StyleAttrs, PageAttrs, WasmExecAttrs and ModuleAttrs are the href or src
	// and integrity attributes of the stylesheet and the three scripts: a
	// content-addressed name and a SHA-384 base64 value, which the builder
	// writes and html/template must not re-escape (a "+" would become &#43;).
	StyleAttrs    template.HTMLAttr
	PageAttrs     template.HTMLAttr
	WasmExecAttrs template.HTMLAttr
	ModuleAttrs   template.HTMLAttr
	// Targets are the target catalogs, with their snapshot dates and regions.
	Targets []targetData
	// Examples are the bundled example inventories.
	Examples []exampleData
	// HSTSWaived reports whether the HSTS record says waived.
	HSTSWaived bool
}

// targetData is one target as the assessment page shows it.
type targetData struct {
	Key      string
	Name     string
	Snapshot string
	Regions  []string
}

// exampleData is one bundled example inventory.
type exampleData struct {
	Key  string
	Name string
	Text string
}

// toolchain is what the go command reports of itself and the module.
type toolchain struct {
	GOROOT    string
	GOMOD     string
	GOVERSION string
}

// bundleFile is one file the bundle holds.
type bundleFile struct {
	name string
	data []byte
}

// build assembles the bundle into opts.out and its archive into opts.archive.
func build(ctx context.Context, opts options) error {
	tc, err := goEnv(ctx)
	if err != nil {
		return err
	}

	root := filepath.Dir(tc.GOMOD)

	siteDir := opts.site
	if siteDir == "" {
		siteDir = filepath.Join(root, "site")
	}

	versionShown, ref, err := versionAndRef(opts.version)
	if err != nil {
		return err
	}

	if err := prepareOut(opts.out); err != nil {
		return err
	}

	module, err := buildModule(ctx, root, filepath.Dir(opts.out))
	if err != nil {
		return err
	}

	wasmExec, err := readIn(filepath.Join(tc.GOROOT, "lib", "wasm"), "wasm_exec.js")
	if err != nil {
		return fmt.Errorf("read the toolchain's wasm_exec.js: %w", err)
	}

	style, err := readIn(siteDir, styleSource)
	if err != nil {
		return fmt.Errorf("read the stylesheet: %w", err)
	}

	script, err := readIn(siteDir, pageSource)
	if err != nil {
		return fmt.Errorf("read the page script: %w", err)
	}

	moduleScript := []byte(moduleScriptPrefix + base64.StdEncoding.EncodeToString(module) + moduleScriptSuffix)

	styleFile := asset("style", ".css", style)
	pageFile := asset("page", ".js", script)
	wasmExecFile := asset("wasm_exec", ".js", wasmExec)
	moduleFile := asset("module", ".js", moduleScript)

	waived, err := readHSTSWaived(siteDir)
	if err != nil {
		return err
	}

	data := pageData{
		Version:       versionShown,
		Ref:           ref,
		GoVersion:     tc.GOVERSION,
		StyleAttrs:    loadAttrs("href", styleFile),
		PageAttrs:     loadAttrs("src", pageFile),
		WasmExecAttrs: loadAttrs("src", wasmExecFile),
		ModuleAttrs:   loadAttrs("src", moduleFile),
		HSTSWaived:    waived,
	}

	if data.Targets, err = targetsData(ctx); err != nil {
		return err
	}

	if data.Examples, err = examplesData(root); err != nil {
		return err
	}

	files := []bundleFile{styleFile, pageFile, wasmExecFile, moduleFile}

	for _, page := range pages {
		rendered, err := renderPage(siteDir, page, data)
		if err != nil {
			return err
		}

		files = append(files, bundleFile{name: page, data: rendered})
	}

	manifest, err := manifestOf(versionShown, files)
	if err != nil {
		return err
	}

	files = append(files, bundleFile{name: "manifest.json", data: manifest})

	slices.SortFunc(files, func(a, b bundleFile) int { return strings.Compare(a.name, b.name) })

	for _, file := range files {
		if err := os.WriteFile(filepath.Join(opts.out, file.name), file.data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", file.name, err)
		}
	}

	return writeArchive(opts.archive, files)
}

// goEnv asks the go command for GOROOT, GOMOD and GOVERSION.
func goEnv(ctx context.Context) (toolchain, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "-json", "GOROOT", "GOMOD", "GOVERSION")
	cmd.Env = goCommandEnv(cmd)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		return toolchain{}, fmt.Errorf("go env: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var tc toolchain
	if err := json.Unmarshal(output, &tc); err != nil {
		return toolchain{}, fmt.Errorf("read the output of go env: %w", err)
	}

	if tc.GOROOT == "" || tc.GOVERSION == "" {
		return toolchain{}, errors.New("go env reports no GOROOT or GOVERSION")
	}

	if tc.GOMOD == "" || tc.GOMOD == os.DevNull {
		return toolchain{}, errors.New("run the builder inside the shift2eu module: go env reports no go.mod")
	}

	return tc, nil
}

// goCommandEnv returns the environment the go command runs in: the one the
// builder was given, with the settings that decide what is built fixed, so a
// developer's GOFLAGS or GOWORK cannot change the module.
func goCommandEnv(cmd *exec.Cmd) []string {
	return append(cmd.Environ(), "GOFLAGS=-mod=readonly", "GOWORK=off")
}

// versionAndRef returns the version the pages show and the git ref their links
// name: the tag and the tag, or internal/version's development value and main.
func versionAndRef(tag string) (string, string, error) {
	if tag != "" {
		if !tagPattern.MatchString(tag) {
			return "", "", fmt.Errorf("-version %q is no release tag of the form vX.Y.Z", tag)
		}

		return tag, tag, nil
	}

	// version.String is "shift2eu <version> (commit ..., built ...)", the line
	// `shift2eu version` prints: the pages show the same value.
	fields := strings.Fields(version.String())
	if len(fields) < 2 {
		return "", "", fmt.Errorf("internal/version reports %q, which names no version", version.String())
	}

	return fields[1], "main", nil
}

// prepareOut creates the bundle directory, refusing one that holds anything: a
// stray file would be deployed without a manifest entry.
func prepareOut(out string) error {
	entries, err := os.ReadDir(out)

	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read the bundle directory %s: %w", out, err)
	case len(entries) != 0:
		return fmt.Errorf("the bundle directory %s is not empty", out)
	}

	if err := os.MkdirAll(out, 0o750); err != nil {
		return fmt.Errorf("create the bundle directory: %w", err)
	}

	return nil
}

// buildModule builds cmd/shift2eu-web for js/wasm into a temporary directory
// under scratch and returns its bytes.
func buildModule(ctx context.Context, root, scratch string) ([]byte, error) {
	dir, err := os.MkdirTemp(scratch, ".shift2eu-sitebuild-")
	if err != nil {
		return nil, fmt.Errorf("create a directory for the module: %w", err)
	}
	defer os.RemoveAll(dir)

	output := filepath.Join(dir, "shift2eu-web.wasm")

	// Every argument is a constant but the output path, a file of the
	// directory the builder just made.
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", output, "./cmd/shift2eu-web") //nolint:gosec // G204: constant arguments and an output path in the builder's own temporary directory
	cmd.Dir = root
	cmd.Env = append(goCommandEnv(cmd), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")

	if combined, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("go build ./cmd/shift2eu-web for js/wasm: %w\n%s", err, strings.TrimSpace(string(combined)))
	}

	module, err := readIn(dir, "shift2eu-web.wasm")
	if err != nil {
		return nil, fmt.Errorf("read the module: %w", err)
	}

	return module, nil
}

// readIn reads the file name inside dir through an os.Root, so neither a ".."
// in name nor a symbolic link under dir reads a file outside it.
func readIn(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	return root.ReadFile(name)
}

// asset names a file by the first 16 hexadecimal characters of its SHA-256.
func asset(prefix, suffix string, data []byte) bundleFile {
	sum := sha256.Sum256(data)

	return bundleFile{name: prefix + "." + hex.EncodeToString(sum[:])[:16] + suffix, data: data}
}

// loadAttrs returns the attributes that load file with Subresource Integrity:
// its name, which is ASCII letters, digits, '.' and '_', under attr and its
// SHA-384 in the integrity attribute, whose base64 html/template would escape.
func loadAttrs(attr string, file bundleFile) template.HTMLAttr {
	sum := sha512.Sum384(file.data)

	// The values are the builder's own: a content-addressed name and a base64
	// digest, neither of which holds a quote, an ampersand or an angle bracket.
	return template.HTMLAttr(fmt.Sprintf(`%s="%s" integrity="sha384-%s"`, attr, file.name, base64.StdEncoding.EncodeToString(sum[:]))) //nolint:gosec // G203: a content-addressed file name and a base64 digest the builder computed
}

// readHSTSWaived reads the HSTS record: a missing record is not waived, and a
// record that is neither met nor waived is an error.
func readHSTSWaived(siteDir string) (bool, error) {
	data, err := readIn(siteDir, filepath.FromSlash(hstsRecord))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("read the HSTS record: %w", err)
	}

	var record struct {
		Outcome string `json:"outcome"`
	}

	if err := json.Unmarshal(data, &record); err != nil {
		return false, fmt.Errorf("read the HSTS record %s: %w", hstsRecord, err)
	}

	switch record.Outcome {
	case "met":
		return false, nil
	case "waived":
		return true, nil
	}

	return false, fmt.Errorf("the HSTS record %s says %q, want met or waived", hstsRecord, record.Outcome)
}

// targetsData reads each embedded catalog's snapshot date and regions.
func targetsData(ctx context.Context) ([]targetData, error) {
	out := make([]targetData, 0, len(targets))

	for _, t := range targets {
		c, err := t.catalog(ctx)
		if err != nil {
			return nil, fmt.Errorf("read the %s catalog: %w", t.name, err)
		}

		out = append(out, targetData{
			Key:      t.key,
			Name:     t.name,
			Snapshot: c.Snapshot().RetrievedOn.Format("2006-01-02"),
			Regions:  c.Regions(),
		})
	}

	return out, nil
}

// examplesData reads the bundled example inventories from examples/.
func examplesData(root string) ([]exampleData, error) {
	out := make([]exampleData, 0, len(examples))

	for _, e := range examples {
		data, err := readIn(filepath.Join(root, "examples"), filepath.Join(e.key, "inventory.json"))
		if err != nil {
			return nil, fmt.Errorf("read the %s example inventory: %w", e.name, err)
		}

		out = append(out, exampleData{Key: e.key, Name: e.name, Text: string(data)})
	}

	return out, nil
}

// renderPage renders one page template of siteDir.
func renderPage(siteDir, page string, data pageData) ([]byte, error) {
	source, err := readIn(siteDir, page)
	if err != nil {
		return nil, fmt.Errorf("read the page %s: %w", page, err)
	}

	tmpl, err := template.New(page).Option("missingkey=error").Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("parse the page %s: %w", page, err)
	}

	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, fmt.Errorf("render the page %s: %w", page, err)
	}

	return b.Bytes(), nil
}

// manifest is manifest.json: the version and every other file's path and
// SHA-256, sorted by path.
type manifest struct {
	Version string         `json:"version"`
	Files   []manifestFile `json:"files"`
}

// manifestFile is one file of the manifest.
type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// manifestOf returns manifest.json for the files.
func manifestOf(versionShown string, files []bundleFile) ([]byte, error) {
	m := manifest{Version: versionShown, Files: make([]manifestFile, 0, len(files))}

	for _, file := range files {
		sum := sha256.Sum256(file.data)
		m.Files = append(m.Files, manifestFile{Path: file.name, SHA256: hex.EncodeToString(sum[:])})
	}

	slices.SortFunc(m.Files, func(a, b manifestFile) int { return strings.Compare(a.Path, b.Path) })

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("write the manifest: %w", err)
	}

	return append(data, '\n'), nil
}
