package architecture_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// This file holds the check that pins which packages' test files import the
// CLI (issue #345). The depguard rule cli-only-in-named-tests refuses
// internal/cli and the packages below it in every _test.go file but those of
// internal/cli, cmd/shift2eu-web/internal/wire and test/apiparity. depguard
// matches its files: globs against absolute paths and has no anchor at the
// module root, and ** crosses /, so each exemption also matches a directory
// whose path merely ends in the same segments: a test file in
// internal/web/test/apiparity would be exempt. This check holds the exact set
// on what go list reports, by import path, which no such directory can spell.

// cliPackage is the CLI's import path: the import the check looks for, and the
// prefix of the packages below it, which the rule refuses too.
const cliPackage = modulePath + "/internal/cli"

// cliTestImporters are the packages whose test files may import the CLI, and
// must: internal/cli's black-box cli_test, the CLI/web parity tests in
// cmd/shift2eu-web/internal/wire (ADR 0035) and the CLI/HTTP API parity test in
// test/apiparity (PRD #103). The list is sorted, as the check's result is.
var cliTestImporters = []string{
	modulePath + "/cmd/shift2eu-web/internal/wire",
	modulePath + "/internal/cli",
	modulePath + "/test/apiparity",
}

// cliTestImportersBuild is one build the check lists the module in: a
// platform and a tag set, named as tagSets name theirs.
type cliTestImportersBuild struct {
	target releasetarget.Target
	tags   tagSet
}

// cliTestImportersBuilds returns the builds the check lists the module in: the
// platform the tests run on with no tag, and with every custom tag; windows,
// for the !unix files; and js/wasm, for the site's glue and any js file, with
// and without the tags. The custom tags are run.build-tags in .golangci.yml,
// read here rather than copied: test/lint's TestLintLoadsEveryGoFile fails on
// a file behind a tag that list leaves out, so a test file behind a new tag
// cannot hide from this check either. cgo is off, as the release builds; no
// test file of the module uses cgo.
func cliTestImportersBuilds(t *testing.T) []cliTestImportersBuild {
	t.Helper()

	tags := golangciBuildTags(t)
	none := tagSet{name: "tags=none"}
	all := tagSet{name: "tags=" + strings.Join(tags, "+"), tags: tags}
	linux := releasetarget.Target{GOOS: "linux", GOARCH: "amd64"}

	return []cliTestImportersBuild{
		{target: linux, tags: none},
		{target: linux, tags: all},
		{target: releasetarget.Target{GOOS: "windows", GOARCH: "amd64"}, tags: all},
		{target: jsWasm, tags: none},
		{target: jsWasm, tags: all},
	}
}

// golangciBuildTags returns, sorted, the entries of run.build-tags in
// .golangci.yml, the custom build tags every lint run sets. It fails the test
// on a file it cannot read or on an empty list, which would leave every tagged
// file out of the check.
func golangciBuildTags(t *testing.T) []string {
	t.Helper()

	lines, err := yamlread.SplitLines(readRepositoryFile(t, ".golangci.yml"))
	if err != nil {
		t.Fatalf(".golangci.yml: %v", err)
	}

	run, err := yamlread.Walk(lines, "run")
	if err != nil {
		t.Fatalf(".golangci.yml: %v", err)
	}

	entries, err := yamlread.Mapping(run)
	if err != nil {
		t.Fatalf(".golangci.yml: run: %v", err)
	}

	i := slices.IndexFunc(entries, func(e yamlread.Entry) bool { return e.Key == "build-tags" })
	if i < 0 {
		t.Fatal(".golangci.yml: no run.build-tags, so the check would list no tagged file")
	}

	tags, err := yamlread.List(entries[i])
	if err != nil {
		t.Fatalf(".golangci.yml: run: %v", err)
	}

	if len(tags) == 0 {
		t.Fatal(".golangci.yml: run.build-tags is empty, so the check would list no tagged file")
	}

	slices.Sort(tags)

	return tags
}

// TestOnlyNamedPackagesTestTheCLI lists every package of the module, with the
// imports of its test files (TestImports and XTestImports), in each build of
// cliTestImportersBuilds, and fails unless the packages whose test files import
// internal/cli, or a package below it, are exactly cliTestImporters. A package
// outside the set is named with the rule it gets past; a named one missing
// means the listing read nothing, or the set is stale and the rule's exemption
// with it.
func TestOnlyNamedPackagesTestTheCLI(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	for _, build := range cliTestImportersBuilds(t) {
		t.Run(build.target.GOOS+"/"+build.target.GOARCH+"/"+build.tags.name, func(t *testing.T) {
			t.Parallel()

			pkgs, err := check.ListPackages(t.Context(), root, build.target, false, build.tags.tags, "./...")
			if err != nil {
				t.Fatalf("ListPackages(./..., %v, %s): %v", build.target, build.tags.name, err)
			}

			got := cliTestImportersOf(pkgs)

			for _, pkg := range got {
				if !slices.Contains(cliTestImporters, pkg) {
					t.Errorf("a test file of %s imports %s: only the test files of %s may (depguard rule cli-only-in-named-tests, issue #345). Test the driving adapter through its own package, or name the package in the rule and in cliTestImporters on purpose",
						pkg, cliPackage, strings.Join(cliTestImporters, ", "))
				}
			}

			for _, pkg := range cliTestImporters {
				if !slices.Contains(got, pkg) {
					t.Errorf("no test file of %s imports %s, so the listing read nothing or the exemption of cli-only-in-named-tests is stale (issue #345); drop the package from the rule and from cliTestImporters",
						pkg, cliPackage)
				}
			}
		})
	}
}

// cliTestImportersOf returns, sorted, the module packages among pkgs whose test
// files import internal/cli or a package below it.
func cliTestImportersOf(pkgs []check.Package) []string {
	var importers []string

	for _, pkg := range pkgs {
		if pkg.Standard || !strings.HasPrefix(pkg.ImportPath, modulePath+"/") {
			continue
		}

		if slices.ContainsFunc(slices.Concat(pkg.TestImports, pkg.XTestImports), isCLIPackage) {
			importers = append(importers, pkg.ImportPath)
		}
	}

	slices.Sort(importers)

	return slices.Compact(importers)
}

// isCLIPackage reports whether path is internal/cli or a package below it.
func isCLIPackage(path string) bool {
	return path == cliPackage || strings.HasPrefix(path, cliPackage+"/")
}

// TestCLITestImportersOf pins the helper: a test import of the CLI or of a
// package below it counts, in either test package; a production import, a
// package whose name only starts with cli, and a package outside the module do
// not.
func TestCLITestImportersOf(t *testing.T) {
	t.Parallel()

	pkg := func(path string, imports, testImports, xTestImports []string) check.Package {
		return check.Package{
			ImportPath:   path,
			Imports:      imports,
			TestImports:  testImports,
			XTestImports: xTestImports,
		}
	}

	pkgs := []check.Package{
		pkg(modulePath+"/internal/web", nil, []string{cliPackage}, nil),
		pkg(modulePath+"/internal/httpapi", nil, nil, []string{cliPackage + "/sub"}),
		pkg(modulePath+"/cmd/shift2eu", []string{cliPackage}, nil, nil),
		pkg(modulePath+"/internal/version", nil, []string{cliPackage + "x"}, nil),
		pkg("example.com/other", nil, []string{cliPackage}, nil),
		pkg(modulePath+"/internal/web", nil, nil, []string{cliPackage}),
	}

	want := []string{
		modulePath + "/internal/httpapi",
		modulePath + "/internal/web",
	}

	if diff := cmp.Diff(want, cliTestImportersOf(pkgs)); diff != "" {
		t.Errorf("cliTestImportersOf() mismatch (-want +got):\n%s", diff)
	}
}
