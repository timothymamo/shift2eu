package site_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This file holds the check of the Go toolchain that builds the module (issue
// #347): go.mod's exact go line is the pin, and the Go that compiled this test
// must be the Go that line names. wasm_exec.js is trusted as toolchain code and
// never scanned (ADR 0035), so the bundle carries the copy of exactly one
// toolchain - the one the module was built with - and the release must be
// reproducible from the tag. TestWasmExecIsTheToolchains in builder_test.go
// compares the bundled file with the building toolchain's, byte for byte; this
// file holds the toolchain half of that promise.
//
// The pin is the go line alone. A toolchain line equal to the go line does not
// survive: with Go 1.27.4, `go mod tidy` rewrites `go 1.27.4` plus `toolchain
// go1.27.4` as the go line only, and CI (`go mod tidy -diff`), `make tidy` and
// Renovate's gomodTidy all run tidy, so that line would come back removed in
// every passing build. A toolchain line newer than the go line would survive
// tidy, and would split what the module states as its minimum Go from the
// toolchain it is built with. So the go line IS the pin (ADR 0035), it must
// name one exact patch release, and no toolchain line belongs beside it.
//
// The pin alone is not enough: Go's own rule is that a go line admits any
// toolchain of that release or newer, so a newer local Go still builds the
// module. So the check reads the Go that compiled it, runtime.Version(), rather
// than trusting the line to decide. make exports GOTOOLCHAIN, and in CI
// actions/setup-go installs the go line's release from go-version-file and sets
// GOTOOLCHAIN=local itself, so every build uses exactly the pinned release.
//
// tools/go.mod needs no check here: test/architecture's
// TestToolsModuleFollowsTheRootModulesGo requires its go line - and its
// toolchain line, when either module has one - to be the root module's, so an
// exactness or a toolchain-line rule proven against the root's go.mod holds for
// it too.

// toolchainPin is what a go.mod says about the Go version the module builds
// with: its go directive's version as the line writes it, and its toolchain
// directive's version when it has one. Any toolchain line at all is one too
// many, so the check below fails on a non-empty Toolchain rather than on a
// differing one.
type toolchainPin struct {
	Go        string
	Toolchain string
}

// parseToolchainPin reads the go and toolchain directives out of the content of
// a go.mod. A directive starts its own line, the way the go command writes
// one: an indented line is a block entry, a line starting with // is a comment,
// and a word that merely begins with the directive's name is no directive. A
// trailing comment after the version goes with the rest of the line's fields.
func parseToolchainPin(content string) toolchainPin {
	var pin toolchainPin

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSuffix(line, "\r")

		if line == "" || strings.HasPrefix(line, "//") ||
			strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "go":
			pin.Go = fields[1]
		case "toolchain":
			pin.Toolchain = fields[1]
		}
	}

	return pin
}

// isExactPatchVersion reports whether a go directive's version names one exact
// patch release - 1.27.4 - rather than a whole minor release (1.27) or a
// prerelease (1.28rc1). Only the first names one toolchain: `go 1.27` admits
// every 1.27.x, so the Go that builds the module would still be the machine's
// choice, and a prerelease is no version a release reproduces from.
func isExactPatchVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}

	for _, part := range parts {
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return false
		}
	}

	return true
}

// toolchainPinProblems reports every way the content of the go.mod at path
// fails to pin the toolchain that the Go version goVersion (runtime.Version())
// names, one line per problem, in the order the checks run. An empty result is
// a pinned toolchain: go.mod's go line names one exact patch release, no
// toolchain line stands beside it, and that Go built whatever is being checked.
func toolchainPinProblems(path, content, goVersion string) []string {
	pin := parseToolchainPin(content)

	if pin.Go == "" {
		return []string{fmt.Sprintf("%s holds no go directive: the module states no Go at all, so no toolchain can be pinned to it", path)}
	}

	var problems []string

	if !isExactPatchVersion(pin.Go) {
		problems = append(problems, fmt.Sprintf(
			"%s says `go %s`, which names no one exact patch release: the go line IS the toolchain pin (ADR 0035), so it must be an exact 1.X.Y release that names the single Go the module and the wasm_exec.js beside it are built with. "+
				"Write `go 1.<minor>.<patch>`, though not a prerelease, and let Renovate bump it",
			path, pin.Go))
	}

	if pin.Toolchain != "" {
		problems = append(problems, fmt.Sprintf(
			"%s holds the toolchain line `toolchain %s`: the go line alone is the pin (ADR 0035). "+
				"`go mod tidy` removes a toolchain line equal to the go line, so it cannot be kept through CI's `go mod tidy -diff`, `make tidy` or Renovate's tidy, and one that differs would survive tidy and split what the module states as its minimum Go from the toolchain it is built with. "+
				"Delete the line; `make` targets set GOTOOLCHAIN, and in CI actions/setup-go installs the go line's release and sets GOTOOLCHAIN=local",
			path, pin.Toolchain))
	}

	// Comparing against a go line that names no one release would only repeat
	// the problem reported above.
	if isExactPatchVersion(pin.Go) {
		if want := "go" + pin.Go; goVersion != want {
			problems = append(problems, fmt.Sprintf(
				"this test was built by %s, but %s pins the toolchain with `go %s`, which is %s: wasm_exec.js is trusted as the building toolchain's own copy (TestWasmExecIsTheToolchains) and must come from the toolchain the module is built with. "+
					"`make` targets set GOTOOLCHAIN to the go line, so build and test through make; otherwise install %s and run the tests with it, or run them with GOTOOLCHAIN=%s",
				goVersion, path, pin.Go, want, want, want))
		}
	}

	return problems
}

// TestParseToolchainPin pins the reader the check below trusts over go.mod
// files written here: a module with no toolchain line at all, one that holds a
// toolchain line, and the lines that are no directive - a commented-out one, an
// indented block entry, and a word that merely begins with "toolchain" - as
// well as a trailing comment after the version and a CRLF file's lines. Each
// case also pins isExactPatchVersion, over the go lines a pin must and must not
// accept: an exact patch release, a minor release that would leave the
// toolchain to the machine, and a prerelease no release reproduces. A reader
// that missed a directive would let a module without one through; one that
// invented a pin would fail an innocent go.mod; and a pin check that accepted
// `go 1.27` would leave the toolchain the machine's choice.
func TestParseToolchainPin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		goMod   string
		want    toolchainPin
		wantPin bool
	}{
		{
			name:    "a go.mod holding no toolchain line pins none",
			goMod:   "module example.com/m\n\ngo 1.27.4\n\nrequire (\n\texample.com/lib v1.0.0\n)\n",
			want:    toolchainPin{Go: "1.27.4"},
			wantPin: true,
		},
		{
			name:    "a go.mod holding a toolchain line is read as it is written",
			goMod:   "module example.com/m\n\ngo 1.27.4\n\ntoolchain go1.27.4\n",
			want:    toolchainPin{Go: "1.27.4", Toolchain: "go1.27.4"},
			wantPin: true,
		},
		{
			// A newer toolchain line survives tidy and would split the pin.
			name:    "a toolchain line newer than the go line is read as it is written",
			goMod:   "module example.com/m\n\ngo 1.27.4\n\ntoolchain go1.28.0\n",
			want:    toolchainPin{Go: "1.27.4", Toolchain: "go1.28.0"},
			wantPin: true,
		},
		{
			name:  "a go line naming only a minor release is no exact pin",
			goMod: "module example.com/m\n\ngo 1.27\n",
			want:  toolchainPin{Go: "1.27"},
		},
		{
			name:  "a prerelease go line is no exact pin",
			goMod: "module example.com/m\n\ngo 1.28rc1\n",
			want:  toolchainPin{Go: "1.28rc1"},
		},
		{
			name:    "a commented-out toolchain line pins none",
			goMod:   "module example.com/m\n\ngo 1.27.4\n\n// toolchain go1.27.4\n",
			want:    toolchainPin{Go: "1.27.4"},
			wantPin: true,
		},
		{
			name:    "a trailing comment after the version is dropped",
			goMod:   "module example.com/m\n\ngo 1.27.4\n\ntoolchain go1.27.4 // Renovate bumps this\n",
			want:    toolchainPin{Go: "1.27.4", Toolchain: "go1.27.4"},
			wantPin: true,
		},
		{
			name:    "a CRLF go.mod's lines are read",
			goMod:   "module example.com/m\r\n\r\ngo 1.27.4\r\ntoolchain go1.27.4\r\n",
			want:    toolchainPin{Go: "1.27.4", Toolchain: "go1.27.4"},
			wantPin: true,
		},
		{
			name:    "a word that merely begins with toolchain is no directive",
			goMod:   "module example.com/m\n\ngo 1.27.4\n\ntoolchainx go1.99.0\n",
			want:    toolchainPin{Go: "1.27.4"},
			wantPin: true,
		},
		{
			name:    "an indented block entry named like the directive is none",
			goMod:   "module example.com/m\n\ngo 1.27.4\n\nrequire (\n\ttoolchain go1.99.0\n)\n",
			want:    toolchainPin{Go: "1.27.4"},
			wantPin: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := parseToolchainPin(tc.goMod)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("parseToolchainPin(-want +got):\n%s", diff)
			}

			if pin := isExactPatchVersion(got.Go); pin != tc.wantPin {
				t.Errorf("isExactPatchVersion(%q) = %t, want %t", got.Go, pin, tc.wantPin)
			}
		})
	}
}

// TestToolchainPinProblems pins the verdict the check below reports, over go.mod
// files written here: an exact go line and the Go it names hold no problem,
// while a minor-only go line, a prerelease go line, a toolchain line beside it,
// a go.mod without a go line and a Go other than the pinned one are each one
// problem, the last naming both versions. A verdict that passed a broken
// go.mod would let the bundle carry a wasm_exec.js from a toolchain the pin
// cannot choose; one that invented a problem would fail an innocent module.
func TestToolchainPinProblems(t *testing.T) {
	t.Parallel()

	const path = "go.mod"

	tests := []struct {
		name         string
		goMod        string
		goVersion    string
		wantProblems int
		wantMentions []string
	}{
		{
			name:      "an exact go line and the Go it pins hold no problem",
			goMod:     "module example.com/m\n\ngo 1.27.4\n",
			goVersion: "go1.27.4",
		},
		{
			name:         "a go line naming only a minor release is a problem",
			goMod:        "module example.com/m\n\ngo 1.27\n",
			goVersion:    "go1.27.4",
			wantProblems: 1,
			wantMentions: []string{"`go 1.27`", "1.X.Y"},
		},
		{
			name:         "a prerelease go line is a problem",
			goMod:        "module example.com/m\n\ngo 1.28rc1\n",
			goVersion:    "go1.28rc1",
			wantProblems: 1,
			wantMentions: []string{"`go 1.28rc1`", "prerelease"},
		},
		{
			name:         "a toolchain line beside the go line is a problem",
			goMod:        "module example.com/m\n\ngo 1.27.4\n\ntoolchain go1.28.0\n",
			goVersion:    "go1.27.4",
			wantProblems: 1,
			wantMentions: []string{"toolchain go1.28.0", "go mod tidy"},
		},
		{
			name:         "a go.mod without a go line is a problem",
			goMod:        "module example.com/m\n",
			goVersion:    "go1.27.4",
			wantProblems: 1,
			wantMentions: []string{"holds no go directive"},
		},
		{
			name:         "a Go other than the pinned one is a problem naming both versions",
			goMod:        "module example.com/m\n\ngo 1.27.4\n",
			goVersion:    "go1.28.0",
			wantProblems: 1,
			wantMentions: []string{"built by go1.28.0", "go1.27.4", "GOTOOLCHAIN=go1.27.4"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := toolchainPinProblems(path, tc.goMod, tc.goVersion)

			if len(problems) != tc.wantProblems {
				t.Fatalf("toolchainPinProblems(%q) reported %d problems, want %d:\n%s",
					tc.goMod, len(problems), tc.wantProblems, strings.Join(problems, "\n"))
			}

			for _, want := range tc.wantMentions {
				if !slices.ContainsFunc(problems, func(problem string) bool {
					return strings.Contains(problem, want)
				}) {
					t.Errorf("toolchainPinProblems(%q) mentions no %q in:\n%s",
						tc.goMod, want, strings.Join(problems, "\n"))
				}
			}
		})
	}
}

// TestGoModPinsTheBuildingToolchain pins go.mod's go line against the Go that
// built this test (issue #347, ADR 0035: the go line is the toolchain pin, and
// the wasm_exec.js the bundle carries is the building toolchain's own copy,
// which test/site checks). The line names one exact patch release, so that one
// version names the Go the module is built with, and no toolchain line stands
// beside it: tidy drops one equal to the go line, and one that differs would
// split the pin. Go builds a module with any toolchain of its go line's release
// or newer, so only reading runtime.Version() closes the gap: without that
// half, a developer's newer Go builds the module while the pin still says
// otherwise, and the module's wasm_exec.js and the module itself come from two
// toolchains between which the pin cannot choose.
func TestGoModPinsTheBuildingToolchain(t *testing.T) {
	t.Parallel()

	path := filepath.Join(theRepositoryRoot(t), "go.mod")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the module's go.mod at %s: %v", path, err)
	}

	for _, problem := range toolchainPinProblems(path, string(content), runtime.Version()) {
		t.Error(problem)
	}
}
