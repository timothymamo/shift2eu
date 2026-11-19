package architecture_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
)

// This file holds the checks for js/wasm, the build target of the site's
// WebAssembly module (PRD #297). No release builds it, so .goreleaser.yaml does
// not list it and the platform loop of test/architecture would never read it:
// it is an explicit extra target, required beside the release platforms in make
// lint and in CI's lint matrix, and held here to the architecture rules and the
// offline promise.
//
// The page's promise is that no inventory leaves the browser. The module is
// compiled from the same Go packages as the CLI, so the promise holds only if
// nothing in its dependency tree can open a connection: a driver rooted at
// cmd/shift2eu-web may not reach net, net/http, os/exec or a cloud SDK, and only
// the module's own glue may reach syscall/js.

// jsWasm is the platform the site's module is built for. It is no release
// platform: the release ships native binaries, and the site ships this module.
var jsWasm = releasetarget.Target{GOOS: "js", GOARCH: "wasm"}

// jsWasmPair is jsWasm as make lint and CI's lint matrix name a target,
// goos/goarch.
const jsWasmPair = "js/wasm"

// webMain is the site's entry point: its composition root, the thin syscall/js
// glue that hands the page to internal/web.
const webMain = "./cmd/shift2eu-web"

// webModulePath is the module's path: the import path of every package the
// check reads under it.
const webModulePath = modulePath

// wantLintTargets returns every goos/goarch make lint and CI's lint matrix must
// cover, sorted: the release platforms .goreleaser.yaml builds, plus js/wasm. A
// target missing from either list ships with its files unlinted, and without
// js/wasm the js && wasm files of the site's glue are never type-checked at all
// (PRD #297, ADR 0012).
func wantLintTargets(targets []releasetarget.Target) []string {
	pairs := make([]string, 0, len(targets)+1)

	for _, target := range targets {
		pairs = append(pairs, target.GOOS+"/"+target.GOARCH)
	}

	pairs = append(pairs, jsWasmPair)

	slices.Sort(pairs)

	return pairs
}

// forbiddenWebDeps are the packages the site's module may not reach, each with
// why: a page that reaches one can send the inventory it was given, or start a
// process, which no browser page can honestly promise. The net packages are
// refused by the rule below instead, which allows the pure parsing ones.
var forbiddenWebDeps = []struct {
	// path is refused as the import path itself.
	path string
	// prefix refuses every import path under it.
	prefix bool
	// why is what reaching it would allow.
	why string
}{
	{path: "os/exec", why: "a process that could carry the inventory out of the browser"},
	{path: "github.com/aws/", prefix: true, why: "a cloud SDK, which speaks to a hyperscaler"},
	{path: "github.com/Azure/", prefix: true, why: "a cloud SDK, which speaks to a hyperscaler"},
	{path: "cloud.google.com/", prefix: true, why: "a cloud SDK, which speaks to a hyperscaler"},
	{path: "google.golang.org/api", prefix: true, why: "a cloud SDK, which speaks to a hyperscaler"},
}

// pureNetPackages are the net packages the site's module may reach: the ones that
// parse a value and open nothing. Every other net and net/... package is refused
// below, so a new one is reviewed before the page may reach it. A package that
// resolves a name or dials (net, net/http, net/smtp, net/rpc, ...) can carry the
// inventory out of the browser.
var pureNetPackages = []string{"net/netip", "net/url"}

// syscallJSImporters are the standard packages whose js/wasm implementations may
// import syscall/js beside the site's own glue, named on purpose: a new one fails
// this test until it is reviewed and added. syscall and time reach the browser
// through their js ports; the site's module is the one package that may use the
// glue's own API.
var syscallJSImporters = []string{"syscall", "time"}

// TestWebBinaryReachesNoNetworkAndNoCloud is the offline dependency test PRD
// #297 promises: go list -deps with GOOS=js GOARCH=wasm on the site's entry
// point must not hold net, net/http, os/exec or a cloud SDK, and only the site's
// own package may import syscall/js. It fails when go list fails too, so a
// dependency tree the check could not read is no pass.
//
// Every net and net/... package is refused except pureNetPackages' few, so a
// package added to that tree later is reviewed here rather than shipped.
func TestWebBinaryReachesNoNetworkAndNoCloud(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	pkgs, err := check.ListPackages(t.Context(), root, jsWasm, false, nil, webMain)
	if err != nil {
		t.Fatalf("go list -deps %s with GOOS=js GOARCH=wasm: %v", webMain, err)
	}

	if len(pkgs) == 0 {
		t.Fatalf("go list -deps %s listed no package, so this test would hold nothing", webMain)
	}

	webPkg := webModulePath + "/cmd/shift2eu-web"
	listed := make([]string, 0, len(pkgs))

	for _, pkg := range pkgs {
		listed = append(listed, pkg.ImportPath)

		if isNetPackage(pkg.ImportPath) && !slices.Contains(pureNetPackages, pkg.ImportPath) {
			t.Errorf("%s reaches %s, a net package that is not one of the pure parsing ones (%s): the page promises that no inventory leaves the browser, so review what it does, then add it here and to the record",
				webMain, pkg.ImportPath, strings.Join(pureNetPackages, ", "))

			continue
		}

		for _, forbidden := range forbiddenWebDeps {
			if pkg.ImportPath != forbidden.path &&
				(!forbidden.prefix || !strings.HasPrefix(pkg.ImportPath, forbidden.path)) {
				continue
			}

			t.Errorf("%s reaches %s, which allows %s: the page promises that no inventory leaves the browser",
				webMain, pkg.ImportPath, forbidden.why)
		}
	}

	for _, pkg := range pkgs {
		if !slices.Contains(pkg.Imports, "syscall/js") {
			continue
		}

		if pkg.ImportPath == webPkg || slices.Contains(syscallJSImporters, pkg.ImportPath) {
			continue
		}

		t.Errorf("%s reaches %s, which imports syscall/js: only the module's own glue may reach the browser, and the standard packages allowed to are %s. Review it, then add it here and to the record",
			webMain, pkg.ImportPath, strings.Join(syscallJSImporters, ", "))
	}

	slices.Sort(listed)

	for _, want := range []string{
		webPkg,
		webModulePath + "/internal/web",
		webModulePath + "/internal/adapter/report/htmlreport",
	} {
		if !slices.Contains(listed, want) {
			t.Errorf("go list did not list %s in %s, so the check above read nothing of it", want, webMain)
		}
	}
}

// isNetPackage reports whether path is the net package or one under it, the
// packages the offline check allows only from pureNetPackages.
func isNetPackage(path string) bool {
	return path == "net" || strings.HasPrefix(path, "net/")
}

// TestWebReleaseTargetDependencies holds the site's binary to the architecture
// rules on js/wasm as TestReleaseTargetDependencies holds the release binaries to
// them on every release platform: every package of the module it is built from
// belongs to the main module, sits in the directory its import path names and
// holds no source but Go files, and the binary carries the packages the page
// needs. It fails too when the binary reaches internal/cli or internal/httpapi,
// which the page never runs (PRD #297).
func TestWebReleaseTargetDependencies(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	required := []string{
		webModulePath + "/internal/adapter/report/htmlreport",
		webModulePath + "/internal/adapter/target/ovhcloud",
		webModulePath + "/internal/adapter/target/scaleway",
		webModulePath + "/internal/adapter/target/upcloud",
		webModulePath + "/internal/web",
	}

	for _, cgo := range cgoModes {
		for _, tags := range tagSets {
			t.Run(fmt.Sprintf("%s/%s/%s/%s", jsWasm.GOOS, jsWasm.GOARCH, cgo.name, tags.name), func(t *testing.T) {
				t.Parallel()

				checkReleaseBinary(t, root, jsWasm, cgo.enabled, tags.tags, webMain, required)
			})
		}
	}
}

// TestWebBinaryRunsNeitherTheCLINorTheHTTPAPI pins that the site's binary is a
// driving adapter of its own: it reaches neither internal/cli nor
// internal/httpapi, which the page never runs (PRD #297).
func TestWebBinaryRunsNeitherTheCLINorTheHTTPAPI(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	pkgs, err := check.ListPackages(t.Context(), root, jsWasm, false, nil, webMain)
	if err != nil {
		t.Fatalf("ListPackages(%s, %v): %v", webMain, jsWasm, err)
	}

	refused := []string{
		webModulePath + "/internal/cli",
		webModulePath + "/internal/httpapi",
	}

	for _, pkg := range pkgs {
		if slices.Contains(refused, pkg.ImportPath) {
			t.Errorf("%s reaches %s, which the page never runs: the site is a driving adapter of its own, beside the CLI and the HTTP API",
				webMain, pkg.ImportPath)
		}
	}
}

// webModuleSizeBudget is the largest gzip-compressed module script the site may
// transfer: 3 MiB (PRD #297, Size and speed). Above it, TinyGo is reconsidered;
// until then the budget fails the test rather than the release.
const webModuleSizeBudget = 3 << 20

// webModuleScriptTemplate is the module script the site ships, in the one form
// PRD #297 (Layer 2) fixes it: the module base64-encoded in a single assignment
// to globalThis.__shift2euModule, then one newline. Nothing else may be in the
// file, so the script carries no code but the module. The size below is measured
// on this exact template, the file the edge serves.
const webModuleScriptTemplate = "globalThis.__shift2euModule=\"%s\";\n"

// TestWebModuleStaysWithinTheSizeBudget builds the site's module as the site
// ships it, base64-encodes it as the module script carries it and gzips it, and
// fails when the transferred script is over webModuleSizeBudget. The raw and the
// compressed size are logged, so the record is in the test's output.
//
// The script is compressed at gzip's default level. Which level and encoding the
// edge actually serves it with is milestone 3's decision (PRD #297, Hosting
// decision 4), so this measures the transferred file at the default level and
// leaves the edge's own settings to that milestone.
func TestWebModuleStaysWithinTheSizeBudget(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	module := filepath.Join(t.TempDir(), "shift2eu-web.wasm")

	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", module, webMain)
	build.Dir = root
	build.Env = webBuildEnv()

	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build %s with GOOS=js GOARCH=wasm: %v\n%s", webMain, err, out)
	}

	raw, err := os.ReadFile(module)
	if err != nil {
		t.Fatalf("read the built module: %v", err)
	}

	// The module script carries the module base64-encoded in a string, as the
	// fixed template writes it: no byte of it is anything but the module, so the
	// budget is measured on the whole script.
	var script bytes.Buffer

	fmt.Fprintf(&script, webModuleScriptTemplate, base64.StdEncoding.EncodeToString(raw))

	var compressed bytes.Buffer

	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := gzipWriter.Write(script.Bytes()); err != nil {
		t.Fatalf("gzip the module script: %v", err)
	}

	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close the gzip writer: %v", err)
	}

	t.Logf("the module is %d bytes, its base64-encoded script %d bytes, and the gzip-compressed script %d bytes",
		len(raw), script.Len(), compressed.Len())

	if compressed.Len() > webModuleSizeBudget {
		t.Errorf("the gzip-compressed module script is %d bytes, over the budget of %d bytes (3 MiB); "+
			"the record of the measured size is PRD #297 milestone 1's ADR",
			compressed.Len(), webModuleSizeBudget)
	}
}

// webBuildEnv returns the environment the site's module is built in: the test's
// own, with the settings the release and make lint fix, so a developer's
// GOFLAGS or GOOS cannot decide what is measured.
func webBuildEnv() []string {
	drop := map[string]bool{
		"GOOS":        true,
		"GOARCH":      true,
		"CGO_ENABLED": true,
		"GOFLAGS":     true,
		"GOWORK":      true,
	}

	env := make([]string, 0, len(os.Environ())+5)

	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if drop[name] {
			continue
		}

		env = append(env, entry)
	}

	return append(env, "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOFLAGS=-mod=readonly", "GOWORK=off")
}
