package architecture_test

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
)

// This file holds the check that confines the browser driver to the one package
// that drives a browser (PRD #297 milestone 2). The site's smoke test, test/browser,
// starts a headless Chromium over the DevTools protocol through
// github.com/chromedp/chromedp, a package that needs a browser, a network and a
// testing-only process, none of which a product binary may carry: cmd/shift2eu,
// cmd/shift2eu-server and cmd/shift2eu-web are built for a user, and ci/sitebuild
// assembles the site's bundle for a release. depguard refuses the import of
// chromedp outside test/browser; this test holds the other half of that promise
// on what a binary is actually built from, its dependency tree, where a
// transitive import no rule of the importing file names would otherwise carry
// it in.

// chromedpDriver is the driver package itself, the one the smoke test imports
// and the one the check below must find to prove it can fire.
const chromedpDriver = "github.com/chromedp/chromedp"

// driverOnlyJSON is a package no product binary may reach although chromedp's
// own prefix does not name it: cdproto, the protocol library chromedp drives the
// browser through, encodes and decodes with go-json-experiment/json. On this
// toolchain it reaches the package only under the cdproto_jsoncompat build tag
// (cdproto's cdp/jsonv2/json_compat.go), and the derived set below reads the
// browser tag alone, so the package is refused explicitly: a product built with
// the compat tag and a driver import would otherwise carry it unseen.
const (
	driverOnlyJSON       = "github.com/go-json-experiment/json"
	driverOnlyJSONPrefix = "github.com/go-json-experiment/"
)

// siteBuilderMain is the site's builder, ci/sitebuild (PRD #297 milestone 2):
// no product binary and no release build, but a program the release and make run
// to assemble the bundle, so nothing it is built from may reach a browser
// either.
const siteBuilderMain = "./ci/sitebuild"

// browserDriverProbe is the package whose dependency tree holds the driver: the
// site's smoke test, the one importer of chromedp in the repository.
const browserDriverProbe = "./test/browser"

// TestNoProductBinaryReachesChromedp lists the dependencies of every program
// the repository builds for a user or a release, and fails when one reaches a
// package the browser driver brings into the module: the release platforms
// .goreleaser.yaml builds for each of cmd/shift2eu, cmd/shift2eu-server and
// ci/sitebuild, and js/wasm for cmd/shift2eu-web, the site's module.
//
// The refused set is derived, not a list of prefixes: go list -deps -test with
// the browser tag over test/browser, the one importer of the driver, names every
// package the driver needs, chromedp's own and the third-party packages only its
// tree uses, and the check takes from that set what is neither the module's own
// code nor the standard library. So a package the driver adds, or one of its
// dependencies moves it to, is refused in every product tree without an edit
// here, while a dependency a product shares with the rest of the module stays
// allowed. github.com/go-json-experiment/ is refused beside the set: cdproto
// reaches it only under a build tag the listing does not compile.
//
// The listing carries the browser build tag, so a file of a product package
// behind that tag, which no release compiles, is read as well: a product file
// that imported the driver would be caught before the tag ever came off. The
// check fails when go list fails too, so a dependency tree it could not read is
// no pass.
//
// The last subtest proves the check can fire: the refused set must hold
// github.com/chromedp/chromedp, the package the smoke test imports, and
// github.com/go-json-experiment/json, which cdproto reaches under its compat
// build tag. Without it a set that matched nothing, or a listing that came back
// empty, would pass every subtest above.
func TestNoProductBinaryReachesChromedp(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	goreleaser := readRepositoryFile(t, ".goreleaser.yaml")

	targets, err := releasetarget.Read(goreleaser)
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so this check would run on nothing")
	}

	// The refused set, read once: the packages the driver's tree brings into the
	// module.
	driver := driverOnlyPackages(t, root)

	// program is one main package built for one platform: what the check below
	// lists the dependencies of.
	type program struct {
		main   string
		target releasetarget.Target
	}

	var programs []program

	for _, target := range targets {
		for _, main := range []string{releasetarget.Main, releasetarget.ServerMain, siteBuilderMain} {
			programs = append(programs, program{main: main, target: target})
		}
	}

	// js/wasm is no release platform, so .goreleaser.yaml does not list it: the
	// site's module is the one program built for it (webtarget_test.go).
	programs = append(programs, program{main: webMain, target: jsWasm})

	for _, p := range programs {
		t.Run(fmt.Sprintf("%s/%s/%s", p.target.GOOS, p.target.GOARCH, strings.TrimPrefix(p.main, "./")), func(t *testing.T) {
			t.Parallel()

			pkgs, err := check.ListPackages(t.Context(), root, p.target, false, []string{"browser"}, p.main)
			if err != nil {
				t.Fatalf("ListPackages(%s, %v, tags=browser): %v", p.main, p.target, err)
			}

			if len(pkgs) == 0 {
				t.Fatalf("go list -deps %s with GOOS=%s GOARCH=%s and the browser tag listed no package, so this check would hold nothing",
					p.main, p.target.GOOS, p.target.GOARCH)
			}

			listed := make([]string, 0, len(pkgs))
			for _, pkg := range pkgs {
				listed = append(listed, pkg.ImportPath)
			}

			reached := driverReaches(listed, driver)
			if len(reached) != 0 {
				// One message per program, naming the reached packages that no
				// reached package holds: the driver's tree is hundreds of
				// packages, and the roots are what a reader has to look at.
				t.Errorf("%s built for %s/%s reaches %s, a package of the browser driver: only test/browser may drive a browser, and a product binary that carried the driver or one of its dependencies could start one",
					p.main, p.target.GOOS, p.target.GOARCH, strings.Join(driverRoots(reached), ", "))
			}
		})
	}

	t.Run("the-check-can-fire", func(t *testing.T) {
		t.Parallel()

		for _, want := range []string{chromedpDriver, driverOnlyJSON} {
			if !driverBrought(want, driver) {
				t.Errorf("the refused set holds no %s, so the check above cannot refuse a product tree that reached it: the set is go list -deps -test -tags=browser %s less the module's own packages and the standard library, and the packages cdproto reaches under its compat tag",
					want, browserDriverProbe)
			}
		}
	})
}

// driverOnlyPackages returns the import paths the browser driver brings into the
// module, sorted: the dependencies of test/browser, the one importer of the
// driver, less the module's own packages and the standard library's.
//
// The listing carries -test and the browser tag for the reason the subtest above
// does: the only imports of chromedp in the repository are in test/browser's
// _test.go files, so go list -deps lists them only with -test, and the browser
// tag compiles the files that carry them.
func driverOnlyPackages(t *testing.T, root string) []string {
	t.Helper()

	probe := goListImportPaths(t, root, "list", "-deps", "-test", "-tags=browser", "--", browserDriverProbe)
	if len(probe) == 0 {
		t.Fatalf("go list -deps -test -tags=browser %s listed no package, so this check would hold nothing", browserDriverProbe)
	}

	module := modulePath

	var driver []string

	for _, path := range probe {
		if path == module || strings.HasPrefix(path, module+"/") || isStandardLibrary(path) {
			continue
		}

		driver = append(driver, path)
	}

	if len(driver) == 0 {
		t.Fatalf("%s reaches no package outside the module and the standard library, so the driver is not in the listing this check reads", browserDriverProbe)
	}

	slices.Sort(driver)

	return driver
}

// isStandardLibrary reports whether the import path is a standard package's: a
// standard package's first element holds no dot, so internal/abi is the
// toolchain's and github.com/... is not.
func isStandardLibrary(path string) bool {
	first, _, _ := strings.Cut(path, "/")

	return !strings.Contains(first, ".")
}

// driverReaches returns those of the import paths that the refused set holds,
// sorted: the entry to report.
func driverReaches(paths, driver []string) []string {
	var reached []string

	for _, path := range paths {
		if driverBrought(path, driver) {
			reached = append(reached, path)
		}
	}

	slices.Sort(reached)

	return reached
}

// driverBrought reports whether the import path is one a product tree may not
// reach: one the derived set names, or one under the prefix cdproto's compat
// build tag reaches.
func driverBrought(path string, driver []string) bool {
	return slices.Contains(driver, path) || strings.HasPrefix(path, driverOnlyJSONPrefix)
}

// driverRoots returns the roots of the reached subtrees, sorted: the reached
// paths whose parent is not reached itself. One import of the driver reaches
// hundreds of packages, and the roots are the ones a reader has to look at; every
// other reached path sits under one of them.
func driverRoots(reached []string) []string {
	var roots []string

	for _, path := range reached {
		slash := strings.LastIndex(path, "/")
		if slash < 0 || !slices.Contains(reached, path[:slash]) {
			roots = append(roots, path)
		}
	}

	return roots
}

// goListImportPaths runs go list with args in dir and returns the import paths it
// prints.
func goListImportPaths(t *testing.T, dir string, args ...string) []string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = dir
	cmd.Env = goListProbeEnv()

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}

	return strings.Fields(string(out))
}

// probeInheritedEnv are the only variables the probe's go list takes from the
// environment this test runs in (issue #395). Every other one is left out, so
// none the probe does not know can change the listing or how it fetches: not
// GOOS or GOARCH, which would list for another platform, not GOEXPERIMENT, and
// not GOSUMDB, GONOSUMDB, GOPRIVATE, GONOPROXY or GOINSECURE, which would let a
// fetch go around GOPROXY or the checksum checks. go.sum pins every module the
// listing may fetch, so it needs no checksum database either.
//
// The first group is check's inheritedEnv (internal/check/packages.go) but for
// GOTOOLCHAIN, which the probe fixes to local instead: what the go command
// needs to find its toolchain, its caches and a temporary directory. It is a
// copy rather than a reference to check's list on purpose: TestGoListProbeEnv
// pins this list exactly, and a name added to check's for its own offline
// listing should not widen what a networked one inherits without that test
// saying so. A name added there belongs here only after a look at both.
var probeInheritedEnv = []string{
	// The toolchain: go test puts the one it runs under first on PATH, and sets
	// GOROOT to it.
	"PATH",
	"GOROOT",
	// The defaults of GOPATH, the module cache and the build cache, under the
	// home directory or a platform's cache directory.
	"HOME",
	"USERPROFILE",
	"XDG_CACHE_HOME",
	"LOCALAPPDATA",
	// Where a developer or CI put the caches instead of the defaults.
	"GOPATH",
	"GOMODCACHE",
	"GOCACHE",
	// The temporary directory on Linux and macOS, and on Windows.
	"TMPDIR",
	"TEMP",
	"TMP",

	// The proxy the listing fetches through on an empty module cache (issue
	// #391), as the go test build fetches its own modules: passed through, never
	// fixed, so the listing fetches from where the build does.
	"GOPROXY",

	// What a fetch through that proxy reads: an HTTP proxy in front of it, in
	// both spellings Go's net/http honours, and the certificates that verify
	// it, where a machine keeps them outside the system's defaults.
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"NO_PROXY",
	"http_proxy",
	"https_proxy",
	"no_proxy",
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
}

// goListProbeEnv returns the environment the probe's go list runs in: the
// variables of probeInheritedEnv that are set in this test's environment, and
// the settings the check's listings fix, so the probe reads the files the
// product listings read. Everything else is left out (issue #395), as
// check.ListPackages leaves it out of its own listings. PWD is not set: os/exec
// sets it only when Env is nil, and the go command then finds its directory
// itself.
//
// Unlike check.ListPackages it leaves GOPROXY as it is (issue #391). Only the
// browser-tagged test/browser imports chromedp, and go test ./... never builds
// it, so on an empty module cache, as in a release run, chromedp and cdproto
// are in no cache until this listing loads them: with GOPROXY=off it would
// fail. The listing fetches them through GOPROXY as set in the environment
// (GOENV=off ignores go env -w), as the go test build fetches its own modules,
// and -mod=readonly keeps go.mod as it is while go.sum pins and verifies every
// module it fetches. GOTOOLCHAIN=local keeps this networked listing from
// switching to, and downloading, another toolchain: go test puts the running,
// pinned one first on PATH. CGO_ENABLED=0 lists as the release builds, and
// GOWORK=off reads no workspace.
func goListProbeEnv() []string {
	env := make([]string, 0, len(probeInheritedEnv)+5)

	// os.LookupEnv ignores case on Windows, where PATH is often spelt Path.
	for _, name := range probeInheritedEnv {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}

	return append(env, "CGO_ENABLED=0", "GOENV=off", "GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local", "GOWORK=off")
}

// probeEnvSetting is the effective value of one variable as the go command reads
// it: unset when no entry of the environment names the variable at all, which
// differs from an entry that sets it to the empty string.
type probeEnvSetting struct {
	value string
	set   bool
}

// allowProbeEnvSetting lets cmp compare probeEnvSetting, whose fields are
// unexported because nothing outside this file reads them.
var allowProbeEnvSetting = cmp.AllowUnexported(probeEnvSetting{})

// probeEnvWant is one variable and the effective setting the probe must give it.
type probeEnvWant struct {
	name    string
	setting probeEnvSetting
}

// effectiveProbeEnvSetting returns the setting the go command reads from env, the
// environment slice of a child process, for the variable name: of the entries
// that name it the last one wins, as an environment later in the slice overrides
// an earlier one, and an entry wins over no entry at all.
func effectiveProbeEnvSetting(env []string, name string) probeEnvSetting {
	var setting probeEnvSetting

	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if found && key == name {
			setting = probeEnvSetting{value: value, set: true}
		}
	}

	return setting
}

// inheritProbeEnv sets name in this test's own environment for the length of t,
// or unsets it when the setting names no value, so the environment the probe
// inherits is the one a case names. t.Setenv records the inherited value and
// restores it, unset included, so the unset below is undone as well.
func inheritProbeEnv(t *testing.T, name string, setting probeEnvSetting) {
	t.Helper()

	t.Setenv(name, "")
	if setting.set {
		t.Setenv(name, setting.value)

		return
	}

	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("os.Unsetenv(%s): %v", name, err)
	}
}

// checkAllowedProbeEnv fails when env, the environment slice of the probe's go
// list, names a variable that is not in allowed, the only names the probe may
// hand the listing. It prints names alone, sorted: a value may be this
// machine's.
func checkAllowedProbeEnv(t *testing.T, env, allowed []string) {
	t.Helper()

	var unlisted []string

	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(allowed, name) {
			unlisted = append(unlisted, name)
		}
	}

	if len(unlisted) == 0 {
		return
	}

	slices.Sort(unlisted)

	t.Errorf("goListProbeEnv() hands the probe's go list %d names that are not allow-listed: %s",
		len(unlisted), strings.Join(unlisted, ", "))
}

// TestGoListProbeEnv pins the environment the browser-driver probe's go list runs
// in (issue #391, issue #395): the settings goListProbeEnv fixes whatever it
// inherits, the variables it leaves out, and every allow-listed variable it
// passes through, GOPROXY among them.
//
// The assertions are on the effective value of each variable, the one the go
// command reads from the environment slice: an entry appended by the probe wins,
// so a probe that stopped fixing a setting would show the hostile inherited one,
// and a probe that added a setting of its own — GOPROXY=off, which a listing on a
// cold module cache cannot recover from — would show it here rather than in a
// release run with an empty module cache, the only place it used to show.
//
// GOPROXY is the case the two kinds of regression meet in: with GOPROXY=off
// inherited, a probe that also appended GOPROXY=off would still agree, so a proxy
// URL and no GOPROXY at all are the cases that catch an appended one, and a
// dropped pass-through is what the effective value of the inherited URL checks.
//
// The cases also hold the allow list itself, in both directions: every hostile
// setting that is not one of the names the probe may hand the listing must be
// gone from its environment, no entry of that environment may name anything
// else, and every name the probe may inherit arrives with the value this test
// gave it. A probe that inherited the whole environment would pass every hostile
// setting below straight through; a probe that stopped inheriting one
// allow-listed name, SSL_CERT_FILE or TMP, say, would fail the value check for
// it rather than go unnoticed.
//
// probeInheritedEnv is pinned exactly, against the names the cases set: a name
// added to it alone would otherwise pass while this machine had that variable
// unset, and be handed to the probe wherever it is set.
//
// No case runs a child process or touches the network: goListProbeEnv only builds
// a slice of strings, and t.Setenv forbids t.Parallel, the whole test's as well.
func TestGoListProbeEnv(t *testing.T) {
	// hostile is the environment a developer's shell or a CI step may hand the
	// probe: a setting that would change what it lists (-mod=mod, a go.work, cgo
	// on) or which platform and toolchain run it (GOOS, GOARCH, GOTOOLCHAIN), a
	// GOENV file that would do both, and the settings that would let the listing
	// fetch around GOPROXY. Every one of them is one the probe must replace or
	// leave out.
	hostile := map[string]string{
		"CGO_ENABLED":  "1",
		"GOARCH":       "arm64",
		"GOENV":        "/some/file",
		"GOEXPERIMENT": "none",
		"GOFLAGS":      "-mod=mod",
		"GOINSECURE":   "example.com/*",
		"GONOPROXY":    "example.com/*",
		"GONOSUMDB":    "example.com/*",
		"GOOS":         "windows",
		"GOPRIVATE":    "example.com/*",
		"GOSUMDB":      "sum.example.com",
		"GOTOOLCHAIN":  "auto",
		"GOWORK":       "/some/go.work",

		"SHIFT2EU_PROBE_UNLISTED": "set",
	}

	// fixed are the settings the probe must carry whatever it inherited: a
	// listing for this machine, built without cgo as the release's are, that
	// keeps go.mod as it is (go.sum pins and verifies every module it fetches),
	// runs the toolchain go test runs, and reads no go env -w file (GOENV=off) and
	// no workspace, so it reads the files the product listings read. GOPROXY is
	// not among them: the probe passes it through.
	fixed := []probeEnvWant{
		{name: "CGO_ENABLED", setting: probeEnvSetting{value: "0", set: true}},
		{name: "GOFLAGS", setting: probeEnvSetting{value: "-mod=readonly", set: true}},
		{name: "GOENV", setting: probeEnvSetting{value: "off", set: true}},
		{name: "GOTOOLCHAIN", setting: probeEnvSetting{value: "local", set: true}},
		{name: "GOWORK", setting: probeEnvSetting{value: "off", set: true}},
	}

	// dropped are the names the probe must leave out of the environment it builds
	// altogether, hostile or not: GOOS and GOARCH would list for another platform,
	// the fetch settings (GOPRIVATE and the rest) would make the listing go around
	// GOPROXY and its checks, GOEXPERIMENT would change what the toolchain builds,
	// and SHIFT2EU_PROBE_UNLISTED stands for any name a later Go, a shell or a CI
	// step adds, which the probe cannot know.
	dropped := []string{
		"GOOS",
		"GOARCH",
		"GOEXPERIMENT",
		"GOINSECURE",
		"GONOPROXY",
		"GONOSUMDB",
		"GOPRIVATE",
		"GOSUMDB",
		"SHIFT2EU_PROBE_UNLISTED",
	}

	// allowed are the only names the probe's environment may hold. It starts
	// from this test's own environment, as check.ListPackages' listEnv does, for
	// the variables the go command needs to find its toolchain, its caches and a
	// temporary directory: PATH and GOROOT for the toolchain, HOME, USERPROFILE,
	// XDG_CACHE_HOME and LOCALAPPDATA for the defaults of the caches, GOPATH,
	// GOMODCACHE and GOCACHE for where a developer or CI put them, TMPDIR, TEMP
	// and TMP for the temporary directory.
	allowed := []string{
		"PATH",
		"GOROOT",
		"HOME",
		"USERPROFILE",
		"XDG_CACHE_HOME",
		"LOCALAPPDATA",
		"GOPATH",
		"GOMODCACHE",
		"GOCACHE",
		"TMPDIR",
		"TEMP",
		"TMP",
		// GOPROXY, and the proxy and certificate settings a fetch through it
		// reads. Unlike the check's listing, the probe's may fetch: on an empty
		// module cache it loads chromedp and cdproto, which the go test build
		// loads as well, so the fetch it makes needs the settings that build uses.
		"GOPROXY",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
		"SSL_CERT_FILE",
		"SSL_CERT_DIR",
	}

	// The settings the probe fixes, and PWD, which os/exec or the probe may set to
	// the listing's directory: this test neither requires nor forbids it.
	for _, want := range fixed {
		allowed = append(allowed, want.name)
	}

	allowed = append(allowed, "PWD")

	// passedThrough are the allow-listed variables a case hands the probe with a
	// sentinel value of its own, so the case checks each one arrives unchanged:
	// every name the probe may inherit but PWD, the settings it fixes (checked
	// above) and GOPROXY (its own cases below). A slice, not a map, so a failure
	// reports in this order every run. The proxy and certificate settings come in
	// two spellings, and os.LookupEnv ignores case on Windows, where HTTP_PROXY and
	// http_proxy name one variable: a pair carries one sentinel value between its
	// spellings, so whichever one Windows reads or writes, the effective value the
	// case asserts is the same. Overriding PATH is safe: no case runs a child
	// process, and t.Setenv restores every one of them.
	passedThrough := []probeEnvWant{
		{name: "PATH", setting: probeEnvSetting{value: "/some/PATH", set: true}},
		{name: "GOROOT", setting: probeEnvSetting{value: "/some/GOROOT", set: true}},
		{name: "HOME", setting: probeEnvSetting{value: "/some/HOME", set: true}},
		{name: "USERPROFILE", setting: probeEnvSetting{value: "/some/USERPROFILE", set: true}},
		{name: "XDG_CACHE_HOME", setting: probeEnvSetting{value: "/some/XDG_CACHE_HOME", set: true}},
		{name: "LOCALAPPDATA", setting: probeEnvSetting{value: "/some/LOCALAPPDATA", set: true}},
		{name: "GOPATH", setting: probeEnvSetting{value: "/some/GOPATH", set: true}},
		{name: "GOMODCACHE", setting: probeEnvSetting{value: "/some/GOMODCACHE", set: true}},
		{name: "GOCACHE", setting: probeEnvSetting{value: "/some/GOCACHE", set: true}},
		{name: "TMPDIR", setting: probeEnvSetting{value: "/some/TMPDIR", set: true}},
		{name: "TEMP", setting: probeEnvSetting{value: "/some/TEMP", set: true}},
		{name: "TMP", setting: probeEnvSetting{value: "/some/TMP", set: true}},
		{name: "HTTP_PROXY", setting: probeEnvSetting{value: "/some/http_proxy", set: true}},
		{name: "http_proxy", setting: probeEnvSetting{value: "/some/http_proxy", set: true}},
		{name: "HTTPS_PROXY", setting: probeEnvSetting{value: "/some/https_proxy", set: true}},
		{name: "https_proxy", setting: probeEnvSetting{value: "/some/https_proxy", set: true}},
		{name: "NO_PROXY", setting: probeEnvSetting{value: "/some/no_proxy", set: true}},
		{name: "no_proxy", setting: probeEnvSetting{value: "/some/no_proxy", set: true}},
		{name: "SSL_CERT_FILE", setting: probeEnvSetting{value: "/some/SSL_CERT_FILE", set: true}},
		{name: "SSL_CERT_DIR", setting: probeEnvSetting{value: "/some/SSL_CERT_DIR", set: true}},
	}

	// probeInheritedEnv must hold exactly those names and no other, whatever this
	// machine happens to have set: the settings a case hands it with a sentinel
	// (passedThrough) and GOPROXY, whose cases give it its own value. A name added
	// to the list but not to this test would otherwise pass unnoticed while the
	// variable was unset here, and reach the probe on a machine that set it, so a
	// widening of what the probe inherits could not land without a case for it.
	// The order of the list carries no meaning, so both sides are sorted before
	// they are compared, and a name listed twice is reported as well.
	wantInherited := make([]string, 0, len(passedThrough)+1)
	for _, want := range passedThrough {
		wantInherited = append(wantInherited, want.name)
	}

	wantInherited = append(wantInherited, "GOPROXY")

	inherited := slices.Clone(probeInheritedEnv)

	slices.Sort(wantInherited)
	slices.Sort(inherited)

	if diff := cmp.Diff(wantInherited, inherited); diff != "" {
		t.Errorf("probeInheritedEnv does not hold exactly the variables the cases assert on (-want +got):\n%s", diff)
	}

	for _, tc := range []struct {
		name        string
		goproxy     probeEnvSetting
		wantGOPROXY probeEnvSetting
	}{
		{
			name:        "an inherited GOPROXY=off is passed through",
			goproxy:     probeEnvSetting{value: "off", set: true},
			wantGOPROXY: probeEnvSetting{value: "off", set: true},
		},
		{
			name:        "an inherited proxy URL is passed through",
			goproxy:     probeEnvSetting{value: "https://proxy.example.com", set: true},
			wantGOPROXY: probeEnvSetting{value: "https://proxy.example.com", set: true},
		},
		{
			name:        "an unset GOPROXY stays unset",
			goproxy:     probeEnvSetting{},
			wantGOPROXY: probeEnvSetting{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, value := range hostile {
				t.Setenv(name, value)
			}

			for _, want := range passedThrough {
				inheritProbeEnv(t, want.name, want.setting)
			}

			inheritProbeEnv(t, "GOPROXY", tc.goproxy)

			env := goListProbeEnv()

			wants := append([]probeEnvWant{{name: "GOPROXY", setting: tc.wantGOPROXY}}, fixed...)
			wants = append(wants, passedThrough...)

			for _, want := range wants {
				got := effectiveProbeEnvSetting(env, want.name)
				if diff := cmp.Diff(want.setting, got, allowProbeEnvSetting); diff != "" {
					t.Errorf("goListProbeEnv() passes %s to the probe's go list as (-want +got):\n%s", want.name, diff)
				}
			}

			for _, name := range dropped {
				got := effectiveProbeEnvSetting(env, name)
				if diff := cmp.Diff(probeEnvSetting{}, got, allowProbeEnvSetting); diff != "" {
					t.Errorf("goListProbeEnv() lets %s reach the probe's go list (-want +got):\n%s", name, diff)
				}
			}

			checkAllowedProbeEnv(t, env, allowed)
		})
	}
}
