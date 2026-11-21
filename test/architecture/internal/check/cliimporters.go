package check

import "slices"

// CLIImportersBuild is one build TestOnlyNamedPackagesTestTheCLI lists the
// module in (issue #345): its name and the module packages whose test files
// import internal/cli, or a package below it, in that build.
type CLIImportersBuild struct {
	// Build names the build, as "linux/amd64/tags=none".
	Build string
	// Importers are the module packages, sorted, whose test files import
	// internal/cli or a package below it in the build.
	Importers []string
}

// CLIImporterProblemKind is the kind of problem CLIImporterProblems reports.
type CLIImporterProblemKind int

const (
	// CLIImporterNotNamed is a package outside the named set whose test files
	// import internal/cli in the problem's build. It is reported per build.
	CLIImporterNotNamed CLIImporterProblemKind = iota
	// CLIImporterStaleExemption is a named package whose test files import
	// internal/cli in no build at all. Its Build is empty.
	CLIImporterStaleExemption
)

// CLIImporterProblem is one problem CLIImporterProblems reports.
type CLIImporterProblem struct {
	Kind CLIImporterProblemKind
	// Build is the build a CLIImporterNotNamed problem is found in, and empty
	// for a CLIImporterStaleExemption.
	Build string
	// Pkg is the package the problem is about.
	Pkg string
}

// CLIImporterProblems holds the packages whose test files import internal/cli
// to named, the exemption of the depguard rule cli-only-in-named-tests, over
// builds (issues #345, #382). A package outside named that imports the CLI in a
// build is reported in that build, the one whose files depguard reads. The
// other direction is checked once, over the union of the builds: a named
// package is reported as a stale exemption only when it imports the CLI in no
// build, so a parity test behind a build constraint, as //go:build !js, is not
// stale in the builds that leave its file out.
//
// The problems come in this order: for each build in turn, each of its
// importers not in named, as CLIImporterNotNamed with the build's name; then
// each package of named that imports the CLI in no build, in named's order, as
// CLIImporterStaleExemption with an empty Build.
func CLIImporterProblems(builds []CLIImportersBuild, named []string) []CLIImporterProblem {
	var problems []CLIImporterProblem

	seen := make(map[string]bool)

	for _, build := range builds {
		for _, pkg := range build.Importers {
			seen[pkg] = true

			if !slices.Contains(named, pkg) {
				problems = append(problems, CLIImporterProblem{Kind: CLIImporterNotNamed, Build: build.Build, Pkg: pkg})
			}
		}
	}

	for _, pkg := range named {
		if !seen[pkg] {
			problems = append(problems, CLIImporterProblem{Kind: CLIImporterStaleExemption, Pkg: pkg})
		}
	}

	return problems
}
