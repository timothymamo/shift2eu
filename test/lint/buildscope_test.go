package lint_test

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
)

// TestLintLoadsEveryGoFile fails on every Go file of the module that
// golangci-lint never loads, so that no lint rule checks it, the
// cloud-read-only and dependency rules included.
//
// make lint and every entry of CI's lint-target matrix lint the host, every
// release target of .goreleaser.yaml and js/wasm, with the tags of run.build-tags
// (lintTags), and the release builds every one of those targets with
// CGO_ENABLED=0. This test reads that list of targets from .goreleaser.yaml
// (releasetarget.Read, the reader test/architecture holds the release to), so
// a target the release gains is a target this test holds files to, and it
// counts a file as loaded when the go command loads it for any one of them: a
// file behind another tag or platform, //go:build ignore among them, can still
// run with real credentials, so it is what this test reports.
//
// The loaded files, and the module's own files (walkGoFiles, the walk of
// TestSourcesHideNothingFromDepguard), come from unloadedGoFiles, whose report
// TestUnloadedGoFilesProbe pins on a probe module. The files in directories
// ./... skips are left to TestLintVisitsEveryImportedPackage.
func TestLintLoadsEveryGoFile(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	// .goreleaser.yaml is the one source of the release platforms: the test
	// reads the list from it rather than pinning a copy here.
	goreleaser, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}

	targets, err := releasetarget.Read(goreleaser)
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so this test would pin nothing")
	}

	// make lint and CI's lint matrix also lint js/wasm, the site's WebAssembly
	// module (PRD #297, ADR 0035), which no release build lists: its js && wasm
	// files are loaded by that run alone. test/architecture requires the run in
	// both lists.
	targets = append(targets, releasetarget.Target{GOOS: "js", GOARCH: "wasm"})

	platforms := make([]string, 0, len(targets))
	for _, target := range targets {
		platforms = append(platforms, target.GOOS+"/"+target.GOARCH)
	}

	for _, rel := range unloadedGoFiles(t, root, targets) {
		base := path.Base(rel)
		if strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
			t.Errorf("%s: the go command ignores a file whose name starts with _ or ., so golangci-lint never "+
				"lints it; rename or delete it", rel)
			continue
		}
		t.Errorf("%s: golangci-lint never loads this file, so no lint rule checks it: its build constraints or "+
			"its name leave it out of every release target of .goreleaser.yaml and of js/wasm (%s), each with CGO_ENABLED=0 and "+
			"the tags %s. Use no build tag but integration or e2e (never //go:build ignore), and let a file the "+
			"release builds build for one of those platforms: a platform the release gains in .goreleaser.yaml "+
			"is a platform this test holds files to", rel, strings.Join(platforms, ", "), lintTags)
	}
}

// TestUnloadedGoFilesProbe pins unloadedGoFiles, the helper
// TestLintLoadsEveryGoFile reports from, on a probe module
// (testdata/platformprobe) of five files: one built on every platform, one
// built for windows/arm64 alone, one built for plan9 alone, one built only with
// cgo, and one built for amd64 only at GOAMD64=v3. Only the first is loaded by
// any release target, which builds every target with CGO_ENABLED=0 at the
// default GOAMD64 level (v1): the other four must be reported by every target
// list, and the windows/arm64 file only by a list that leaves windows/arm64
// out. A target list that loads a file must not report it, and one that leaves
// it out must; a helper that reported nothing, that looked at one target only,
// or that reported in the order the go command answered fails here. The probe
// module has a go.mod of its own, so ./... of the main module skips it.
func TestUnloadedGoFilesProbe(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("testdata", "platformprobe"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("probe module: %v", err)
	}

	linux := releasetarget.Target{GOOS: "linux", GOARCH: "amd64"}
	windowsARM := releasetarget.Target{GOOS: "windows", GOARCH: "arm64"}
	windowsAMD := releasetarget.Target{GOOS: "windows", GOARCH: "amd64"}

	tests := []struct {
		name    string
		targets []releasetarget.Target
		want    []string
	}{
		{
			// The release builds with CGO_ENABLED=0 and at GOAMD64=v1, so no
			// target loads the cgo file or the amd64.v3 file.
			name:    "a target list that holds windows/arm64 reports the cgo file, the amd64.v3 file and the file no target builds",
			targets: []releasetarget.Target{linux, windowsARM},
			want:    []string{"amd64v3.go", "cgoonly.go", "plan9only.go"},
		},
		{
			// Issue #82: the test this helper replaces held files to linux/amd64
			// alone, so a file only another release target builds failed it,
			// though lint lints that file in that target's run.
			name:    "linux/amd64 alone reports the windows/arm64 file, the complaint of issue #82",
			targets: []releasetarget.Target{linux},
			want:    []string{"amd64v3.go", "cgoonly.go", "plan9only.go", "windowsarm.go"},
		},
		{
			name:    "another windows target is not windows/arm64",
			targets: []releasetarget.Target{linux, windowsAMD},
			want:    []string{"amd64v3.go", "cgoonly.go", "plan9only.go", "windowsarm.go"},
		},
		{
			name:    "the order of the targets does not change the report",
			targets: []releasetarget.Target{windowsARM, linux},
			want:    []string{"amd64v3.go", "cgoonly.go", "plan9only.go"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := unloadedGoFiles(t, root, tc.targets)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("unloadedGoFiles(%v) mismatch (-want +got):\n%s", tc.targets, diff)
			}
		})
	}
}

// TestLintVisitsEveryImportedPackage fails on every package of the module's
// directory tree that the module's code imports, tests included, but that
// golangci-lint never lints: ./... skips testdata directories, directories
// whose name starts with _ or ., and the root of another module, while an
// import of a package there still builds and runs.
func TestLintVisitsEveryImportedPackage(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	var problems []string
	seen := map[string]bool{}
	for _, p := range goList(t, root, "ImportPath,Dir,Module", "-deps", "-test", "./...") {
		if p.Dir == "" {
			continue
		}
		dir, ok := relToRoot(t, root, p.Dir)
		if !ok || seen[dir] {
			continue
		}
		seen[dir] = true

		if skipped := slices.ContainsFunc(strings.Split(dir, "/"), func(elem string) bool {
			return elem == "testdata" || strings.HasPrefix(elem, "_") || (strings.HasPrefix(elem, ".") && elem != ".")
		}); skipped {
			problems = append(problems, dir+": a package here is imported, but ./... skips testdata, _ and . "+
				"directories, so golangci-lint never lints it and no lint rule checks it; move the package "+
				"out of that directory")
			continue
		}
		if p.Module == nil || !p.Module.Main {
			problems = append(problems, dir+": a package here is imported from a module nested in this one, "+
				"which golangci-lint does not lint from here; move the package into this module")
		}
	}
	slices.Sort(problems)
	for _, problem := range problems {
		t.Error(problem)
	}
}
