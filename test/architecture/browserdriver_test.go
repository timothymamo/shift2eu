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

// chromedpPrefix is the import path prefix of the browser driver: chromedp and
// everything under it, cdproto, sysutil, its json and easierjson packages. A
// product binary that reached one of them could start a browser.
const chromedpPrefix = "github.com/chromedp/"

// chromedpDriver is the driver package itself, the one the smoke test imports
// and the one the check below must find to prove it can fire.
const chromedpDriver = "github.com/chromedp/chromedp"

// siteBuilderMain is the site's builder, ci/sitebuild (PRD #297 milestone 2):
// no product binary and no release build, but a program the release and make run
// to assemble the bundle, so nothing it is built from may reach a browser
// either.
const siteBuilderMain = "./ci/sitebuild"

// browserDriverProbe is the package whose dependency tree holds the driver: the
// site's smoke test, the one importer of chromedp in the repository.
const browserDriverProbe = "./test/browser"

// TestNoProductBinaryReachesChromedp lists the dependencies of every program
// the repository builds for a user or a release, and fails when one reaches
// github.com/chromedp/: the release platforms .goreleaser.yaml builds for each
// of cmd/shift2eu, cmd/shift2eu-server and ci/sitebuild, and js/wasm for
// cmd/shift2eu-web, the site's module.
//
// The listing carries the browser build tag, so a file of a product package
// behind that tag, which no release compiles, is read as well: a product file
// that imported the driver would be caught before the tag ever came off. The
// check fails when go list fails too, so a dependency tree it could not read is
// no pass.
//
// The last subtest proves the check can fire: go list -deps with the browser tag
// on test/browser, the one package that imports the driver, must list
// github.com/chromedp/chromedp. Without it a prefix that matched nothing, or a
// listing that came back empty, would pass every subtest above.
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

			reached := chromedpReaches(listed)
			if len(reached) != 0 {
				// One message per program, naming the reached packages that no
				// reached package holds: the driver's tree is hundreds of
				// packages, and the roots are what a reader has to look at.
				t.Errorf("%s built for %s/%s reaches %s, a package of the browser driver: only test/browser may drive a browser, and a product binary that carried the driver could start one",
					p.main, p.target.GOOS, p.target.GOARCH, strings.Join(chromedpRoots(reached), ", "))
			}
		})
	}

	t.Run("the-check-can-fire", func(t *testing.T) {
		t.Parallel()

		// -test is what makes this listing show the driver: the only imports of
		// chromedp in the repository are in test/browser's _test.go files, and
		// go list -deps lists the imports of a package's own files alone. The
		// browser tag compiles the files that carry them.
		paths := goListImportPaths(t, root, "list", "-deps", "-test", "-tags=browser", "--", browserDriverProbe)

		if !slices.Contains(chromedpReaches(paths), chromedpDriver) {
			t.Errorf("go list -deps -test -tags=browser %s lists no %s, so the check above cannot fire: a prefix that matches nothing, or a listing that comes back empty, would pass every subtest",
				browserDriverProbe, chromedpDriver)
		}
	})
}

// chromedpReaches returns those of the import paths that belong to the browser
// driver, sorted: the entry to report, and what makes the check above and its
// proof read one prefix.
func chromedpReaches(paths []string) []string {
	var reached []string

	for _, path := range paths {
		if strings.HasPrefix(path, chromedpPrefix) {
			reached = append(reached, path)
		}
	}

	slices.Sort(reached)

	return reached
}

// chromedpRoots returns the roots of the reached subtrees, sorted: the reached
// paths whose parent is not reached itself. One import of the driver reaches
// hundreds of chromedp packages, and the roots are the ones a reader has to
// look at; every other reached path sits under one of them.
func chromedpRoots(reached []string) []string {
	isReached := make(map[string]bool, len(reached))
	for _, path := range reached {
		isReached[path] = true
	}

	var roots []string

	for _, path := range reached {
		if !isReached[path[:strings.LastIndex(path, "/")]] {
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
