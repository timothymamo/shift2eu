package site_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This file holds the check of the Go toolchain that builds the module (issue
// #347): go.mod's toolchain line is the pin, and the Go that compiled this test
// must be the Go that line names. wasm_exec.js is trusted as toolchain code and
// never scanned (ADR 0035), so the bundle carries the copy of exactly one
// toolchain - the one the module was built with - and the release must be
// reproducible from the tag. TestWasmExecIsTheToolchains in builder_test.go
// compares the bundled file with the building toolchain's, byte for byte; this
// file holds the toolchain half of that promise.
//
// The pin alone is not enough: Go's own rule is that a toolchain line makes an
// OLDER local Go switch up to it, while a NEWER local Go still builds the
// module. So the check reads the Go that compiled it, runtime.Version(), rather
// than trusting the line to decide.

// toolchainPin is what a go.mod says about the Go version the module builds
// with: its go directive's version and, when it has one, its toolchain
// directive's version, each as the line writes it.
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

// TestParseToolchainPin pins the reader the check below trusts over go.mod
// files written here: a module with no toolchain line at all, a pin that holds
// the go line's version, a pin that holds another one, and the lines that are
// no directive - a commented-out one, an indented block entry, and a word that
// merely begins with "toolchain" - as well as a trailing comment after the
// version and a CRLF file's lines. A reader that missed a pin would let a
// module without one through; one that invented a pin would fail an innocent
// go.mod.
func TestParseToolchainPin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		goMod string
		want  toolchainPin
	}{
		{
			name:  "a go.mod holding no toolchain line pins none",
			goMod: "module example.com/m\n\ngo 1.27.4\n\nrequire (\n\texample.com/lib v1.0.0\n)\n",
			want:  toolchainPin{Go: "1.27.4"},
		},
		{
			name:  "a toolchain line holding the go line's version pins it",
			goMod: "module example.com/m\n\ngo 1.27.4\n\ntoolchain go1.27.4\n",
			want:  toolchainPin{Go: "1.27.4", Toolchain: "go1.27.4"},
		},
		{
			name:  "a toolchain line holding another version is read as it is written",
			goMod: "module example.com/m\n\ngo 1.27.4\n\ntoolchain go1.28.0\n",
			want:  toolchainPin{Go: "1.27.4", Toolchain: "go1.28.0"},
		},
		{
			name:  "a commented-out toolchain line pins none",
			goMod: "module example.com/m\n\ngo 1.27.4\n\n// toolchain go1.27.4\n",
			want:  toolchainPin{Go: "1.27.4"},
		},
		{
			name:  "a trailing comment after the version is dropped",
			goMod: "module example.com/m\n\ngo 1.27.4\n\ntoolchain go1.27.4 // Renovate bumps this\n",
			want:  toolchainPin{Go: "1.27.4", Toolchain: "go1.27.4"},
		},
		{
			name:  "a CRLF go.mod's lines are read",
			goMod: "module example.com/m\r\n\r\ngo 1.27.4\r\ntoolchain go1.27.4\r\n",
			want:  toolchainPin{Go: "1.27.4", Toolchain: "go1.27.4"},
		},
		{
			name:  "a word that merely begins with toolchain is no directive",
			goMod: "module example.com/m\n\ngo 1.27.4\n\ntoolchainx go1.99.0\n",
			want:  toolchainPin{Go: "1.27.4"},
		},
		{
			name:  "an indented block entry named like the directive is none",
			goMod: "module example.com/m\n\ngo 1.27.4\n\nrequire (\n\ttoolchain go1.99.0\n)\n",
			want:  toolchainPin{Go: "1.27.4"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, parseToolchainPin(tc.goMod)); diff != "" {
				t.Errorf("parseToolchainPin(-want +got):\n%s", diff)
			}
		})
	}
}

// TestGoModPinsTheBuildingToolchain pins go.mod's toolchain line against the Go
// that built this test (issue #347, ADR 0036: "the bundled file is byte for
// byte the building toolchain's, which test/site checks, and go.mod pins that
// toolchain"). The line is the go line made exact, so it names the same version
// the module's language level does, and it names the toolchain that compiled
// this test. Go switches an older local Go up to the pin but lets a newer one
// build, so only reading runtime.Version() closes the gap: without that half, a
// developer's newer Go builds the module while the pin still says otherwise,
// and the module's wasm_exec.js and the module itself come from two toolchains
// between which the pin cannot choose.
func TestGoModPinsTheBuildingToolchain(t *testing.T) {
	t.Parallel()

	path := filepath.Join(theRepositoryRoot(t), "go.mod")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the module's go.mod at %s: %v", path, err)
	}

	pin := parseToolchainPin(string(content))

	if pin.Go == "" {
		t.Fatalf("%s holds no go directive: the module states no minimum Go, so no toolchain can be pinned to it", path)
	}

	if pin.Toolchain == "" {
		t.Fatalf("%s holds no toolchain directive, only `go %s`: a newer local Go builds the module, and the WebAssembly module and the wasm_exec.js the bundle carries beside it must come from the same toolchain. "+
			"Add `toolchain go%s` under the go line, or run the tests with GOTOOLCHAIN=go%s",
			path, pin.Go, pin.Go, pin.Go)
	}

	if want := "go" + pin.Go; pin.Toolchain != want {
		t.Errorf("%s pins toolchain %s while its go line says `go %s`: the pin is the go line made exact, so that one version names both the language level the module is built at and the toolchain that builds it, and Renovate bumps the two together. "+
			"Write `toolchain %s`, the same version in both lines", path, pin.Toolchain, pin.Go, want)
	}

	if got := runtime.Version(); got != pin.Toolchain {
		t.Errorf("this test was built by %s, but %s pins toolchain %s: wasm_exec.js is trusted as the building toolchain's own copy (TestWasmExecIsTheToolchains) and must come from the toolchain the module is built with. "+
			"Install %s and run the tests with it, or run them with GOTOOLCHAIN=%s",
			got, path, pin.Toolchain, pin.Toolchain, pin.Toolchain)
	}
}
