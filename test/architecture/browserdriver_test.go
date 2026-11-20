package architecture_test

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

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

// goListProbeEnv returns the environment the probe's go list runs in: this
// test's own but for the variables that would decide what it lists, which
// check.ListPackages drops too, plus the settings the check's listings fix, so
// the probe reads the same files the product listings read.
func goListProbeEnv() []string {
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

	return append(env, "CGO_ENABLED=0", "GOENV=off", "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOWORK=off")
}
