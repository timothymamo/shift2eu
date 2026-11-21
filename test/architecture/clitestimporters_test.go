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
// internal/cli and cmd/shift2eu-web/internal/wire. depguard
// matches its files: globs against absolute paths and has no anchor at the
// module root, and ** crosses /, so each exemption also matches a directory
// whose path merely ends in the same segments: a test file in
// internal/web/internal/cli would be exempt. This check holds the exact set
// on what go list reports, by import path, which no such directory can spell.

// cliPackage is the CLI's import path: the import the check looks for, and the
// prefix of the packages below it, which the rule refuses too.
const cliPackage = modulePath + "/internal/cli"

// cliTestImporters are the packages whose test files may import the CLI, and
// must: internal/cli's black-box cli_test and the CLI/web parity tests in
// cmd/shift2eu-web/internal/wire (ADR 0035). The list is sorted, as the
// check's result is.
var cliTestImporters = []string{
	modulePath + "/cmd/shift2eu-web/internal/wire",
	modulePath + "/internal/cli",
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
// cliTestImportersBuilds, and fails on the two problems check.CLIImporterProblems
// returns for the sets it collects. A package outside cliTestImporters whose
// test files import internal/cli is named in the build it does so in: that is
// the build whose files depguard reads, so the package gets past
// cli-only-in-named-tests there. A package of cliTestImporters that imports
// internal/cli in no build at all has a stale exemption and is named once,
// over the union of the builds: a parity test that gains a build constraint,
// as //go:build !js, is still exempt where its file is built, so no build but
// the ones it is left out of may report its exemption as stale (issue #382).
func TestOnlyNamedPackagesTestTheCLI(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	// The builds are listed one after the other, not in parallel subtests: a
	// parallel subtest resumes only after its parent returns, and the union
	// check below needs every build's set.
	var sets []check.CLIImportersBuild

	for _, build := range cliTestImportersBuilds(t) {
		name := build.target.GOOS + "/" + build.target.GOARCH + "/" + build.tags.name

		t.Run(name, func(t *testing.T) {
			pkgs, err := check.ListPackages(t.Context(), root, build.target, false, build.tags.tags, "./...")
			if err != nil {
				t.Fatalf("ListPackages(./..., %v, %s): %v", build.target, build.tags.name, err)
			}

			sets = append(sets, check.CLIImportersBuild{Build: name, Importers: cliTestImportersOf(pkgs)})
		})
	}

	for _, problem := range check.CLIImporterProblems(sets, cliTestImporters) {
		switch problem.Kind {
		case check.CLIImporterNotNamed:
			t.Errorf("%s: a test file of %s imports %s: only the test files of %s may (depguard rule cli-only-in-named-tests, issue #345). Test the driving adapter through its own package, or name the package in the rule and in cliTestImporters on purpose",
				problem.Build, problem.Pkg, cliPackage, strings.Join(cliTestImporters, ", "))
		case check.CLIImporterStaleExemption:
			t.Errorf("no test file of %s imports %s in any build the check lists, so the listing read nothing or the exemption of cli-only-in-named-tests is stale (issue #345); drop the package from the rule and from cliTestImporters, or check the build tags of its test files",
				problem.Pkg, cliPackage)
		default:
			t.Errorf("check.CLIImporterProblems returned problem kind %d for %s, which this check does not know",
				problem.Kind, problem.Pkg)
		}
	}
}

// TestCLIImporterProblems pins the decision TestOnlyNamedPackagesTestTheCLI
// makes, on synthetic importer sets, so the union of builds is pinned without
// a build constraint in a real parity test. check.CLIImporterProblems takes the
// named packages, the exemption of cli-only-in-named-tests, and one
// CLIImportersBuild per build: the build's name and the module packages, as
// cliTestImportersOf returns them, whose test files import internal/cli in it.
// It returns the problems in this order: for each build in turn, each of its
// importers that is not named, as CLIImporterNotNamed with the build's name;
// then each named package that imports internal/cli in no build, in the order
// named lists them, as CLIImporterStaleExemption with an empty Build.
func TestCLIImporterProblems(t *testing.T) {
	t.Parallel()

	const (
		wirePkg = modulePath + "/cmd/shift2eu-web/internal/wire"
		cliPkg  = modulePath + "/internal/cli"
		webPkg  = modulePath + "/internal/web"

		nativeBuild = "linux/amd64/tags=none"
		wasmBuild   = "js/wasm/tags=none"
	)

	named := []string{wirePkg, cliPkg}

	// build is one build of the check: its name and the packages whose test
	// files import internal/cli in it.
	build := func(name string, importers ...string) check.CLIImportersBuild {
		return check.CLIImportersBuild{Build: name, Importers: importers}
	}

	// notNamed and stale are the problems check.CLIImporterProblems returns.
	notNamed := func(name, pkg string) check.CLIImporterProblem {
		return check.CLIImporterProblem{Kind: check.CLIImporterNotNamed, Build: name, Pkg: pkg}
	}

	stale := func(pkg string) check.CLIImporterProblem {
		return check.CLIImporterProblem{Kind: check.CLIImporterStaleExemption, Pkg: pkg}
	}

	tests := []struct {
		name string
		sets []check.CLIImportersBuild
		want []check.CLIImporterProblem
	}{
		{
			name: "every named package imports the CLI in every build",
			sets: []check.CLIImportersBuild{
				build(nativeBuild, cliPkg, wirePkg),
				build(wasmBuild, cliPkg, wirePkg),
			},
			want: nil,
		},
		{
			name: "a named package imports the CLI in some builds only",
			sets: []check.CLIImportersBuild{
				build(nativeBuild, cliPkg, wirePkg),
				build(wasmBuild, cliPkg),
			},
			want: nil,
		},
		{
			name: "a named package imports the CLI in no build",
			sets: []check.CLIImportersBuild{
				build(nativeBuild, cliPkg),
				build(wasmBuild, cliPkg),
			},
			want: []check.CLIImporterProblem{stale(wirePkg)},
		},
		{
			name: "no build lists a named package at all",
			sets: nil,
			want: []check.CLIImporterProblem{stale(wirePkg), stale(cliPkg)},
		},
		{
			name: "a package outside the set imports the CLI in one build",
			sets: []check.CLIImportersBuild{
				build(nativeBuild, cliPkg, webPkg, wirePkg),
				build(wasmBuild, cliPkg, wirePkg),
			},
			want: []check.CLIImporterProblem{notNamed(nativeBuild, webPkg)},
		},
		{
			name: "a package outside the set imports the CLI in the last build only",
			sets: []check.CLIImportersBuild{
				build(nativeBuild, cliPkg, wirePkg),
				build(wasmBuild, cliPkg, webPkg, wirePkg),
			},
			want: []check.CLIImporterProblem{notNamed(wasmBuild, webPkg)},
		},
		{
			name: "a stale exemption beside a package outside the set",
			sets: []check.CLIImportersBuild{
				build(nativeBuild, cliPkg, webPkg),
				build(wasmBuild, cliPkg),
			},
			want: []check.CLIImporterProblem{
				notNamed(nativeBuild, webPkg),
				stale(wirePkg),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, check.CLIImporterProblems(tc.sets, named)); diff != "" {
				t.Errorf("CLIImporterProblems() mismatch (-want +got):\n%s", diff)
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
		pkg(modulePath+"/internal/app/analysis", nil, nil, []string{cliPackage + "/sub"}),
		pkg(modulePath+"/cmd/shift2eu", []string{cliPackage}, nil, nil),
		pkg(modulePath+"/internal/version", nil, []string{cliPackage + "x"}, nil),
		pkg("example.com/other", nil, []string{cliPackage}, nil),
		pkg(modulePath+"/internal/web", nil, nil, []string{cliPackage}),
	}

	want := []string{
		modulePath + "/internal/app/analysis",
		modulePath + "/internal/web",
	}

	if diff := cmp.Diff(want, cliTestImportersOf(pkgs)); diff != "" {
		t.Errorf("cliTestImportersOf() mismatch (-want +got):\n%s", diff)
	}
}
