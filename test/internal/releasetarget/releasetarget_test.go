package releasetarget_test

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
)

// The .goreleaser.yaml documents below are hand-written copies of this
// repository's build, cut down to the keys Read reads. The build has one
// build, the environment Env names (CGO_ENABLED=0, GOWORK=off and
// GOFLAGS=-mod=readonly, in that order; issue #102), and three goos by two
// goarch: six platforms, which is the list every case below compares against
// or breaks. A document with a before section carries the one hook Read
// accepts, `go mod tidy -diff`: it fails and writes nothing when the module is
// not tidy, where plain `go mod tidy` would rewrite go.mod and go.sum before
// the -mod=readonly build.
//
// buildFlow and buildReal live in testdata, as files: the tests of
// test/architecture/internal/check add a release section to the same two
// documents, so there is one copy of each to keep in step with the release.
var (
	// buildFlow writes the goos and goarch lists in flow form, as
	// .goreleaser.yaml does.
	//
	//go:embed testdata/flow.goreleaser.yaml
	buildFlow string

	// buildReal is a snapshot of the shape of this repository's
	// .goreleaser.yaml: the before hook `go mod tidy -diff`, archives, checksum
	// and changelog around one build. It is not a copy of that file. The check
	// may refuse a top-level key that could change what the release builds, so
	// a document of this shape must stay accepted;
	// test/architecture/repository_test.go checks the real .goreleaser.yaml
	// itself.
	//
	//go:embed testdata/real.goreleaser.yaml
	buildReal string
)

const (
	// buildBlock writes the same lists in block form.
	buildBlock = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    binary: shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
`

	// buildEnvFlow keeps the flow lists and writes env in flow form too, so a
	// reader that only understands one form of either list fails here.
	buildEnvFlow = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env: [CGO_ENABLED=0, GOWORK=off, GOFLAGS=-mod=readonly]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildCgoOn turns cgo on: a release build that would link the host's C
	// library and so depend on what the host has installed. It is Env with its
	// first entry turned on, so a case using it fails for cgo alone.
	buildCgoOn = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=1
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildNoEnv leaves the environment out, so cgo falls back to the default
	// of the toolchain that reads it.
	buildNoEnv = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildEmptyGoos asks for no platform at all.
	buildEmptyGoos = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: []
    goarch: [amd64, arm64]
`

	// buildNoGoarch leaves arch selection to whatever the toolchain defaults
	// to, which is not the list the release was reviewed for.
	buildNoGoarch = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
`

	// buildTwo makes two binaries, each with its own target list, so the
	// platforms of one document are no longer one list, and the second is a
	// main package the release does not ship.
	buildTwo = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
  - id: shift2eu-helper
    main: ./cmd/shift2eu-helper
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux]
    goarch: [amd64]
`

	// buildIgnore drops platforms from the list with a matrix of its own, so
	// the goos x goarch product is not what the release builds.
	buildIgnore = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ignore:
      - goos: darwin
        goarch: arm64
`

	// buildTargets names platforms one by one instead of crossing goos with
	// goarch, in the form goreleaser v1 took.
	buildTargets = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    targets:
      - linux_amd64
      - darwin_arm64
`

	// buildTopGomod sets the Go binary goreleaser builds with, so the release
	// would compile with a toolchain the check never read. It is written in
	// block form, as the file would hold it.
	buildTopGomod = `version: 2

gomod:
  gobinary: go1.22.0

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildBeforeGenerate runs the go mod tidy hook and then a second one,
	// which could generate, rewrite or delete the files the release compiles.
	buildBeforeGenerate = `version: 2

before:
  hooks:
    - go mod tidy -diff
    - go generate ./...

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildBeforeOtherKey puts something other than hooks under before, which
	// the check does not read.
	buildBeforeOtherKey = `version: 2

before:
  env:
    - FOO=bar

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildRepeatedGoos lists the goos key twice: the reader must refuse it
	// rather than keep one of the two lists.
	buildRepeatedGoos = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildTemplatedGoos asks for a platform goreleaser computes from a
	// template, so the list the check reads is not the list the release
	// builds.
	buildTemplatedGoos = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, '{{ .GOOS }}']
    goarch: [amd64, arm64]
`

	// buildMergeKey merges another mapping into the document, whose keys the
	// check would never see.
	buildMergeKey = `version: 2

<<: *base

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildAnchoredEnv anchors the env list, so the list the check reads is
	// not necessarily the one goreleaser expands.
	buildAnchoredEnv = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env: &envs
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
`

	// buildAliasedGoos takes its platform list from an alias: the reader does
	// not resolve aliases, so it cannot read the list.
	buildAliasedGoos = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: *gos
    goarch: [amd64, arm64]
`

	// buildCRInComment is the auditor's probe: a CR inside the comment on the
	// goarch key. A reader that takes CR for a line end sees the `- riscv64`
	// entry outside the comment and lists nine platforms instead of failing.
	buildCRInComment = `version: 2

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    env:
      - CGO_ENABLED=0
      - GOWORK=off
      - GOFLAGS=-mod=readonly
    goos: [linux, darwin, windows]
    goarch: # amd64 and arm64 only` + "\r" + `      - riscv64
      - amd64
      - arm64
`
)

// serverBuild returns a build entry of the HTTP API, ./cmd/shift2eu-server
// (PRD #103), with the main and the goos and goarch lines given, each already
// indented under the build's dash, and the environment Read requires. Every
// case that adds it to a document is refused: the release stopped building the
// server (issue #393).
func serverBuild(main, goos, goarch string) string {
	return "  - id: shift2eu-server\n    main: " + main + "\n" + envDoc + goos + "\n" + goarch + "\n"
}

// withBuilds returns buildFlow with the build entries given added to its
// builds list, after the CLI's build and before the sections that follow it.
func withBuilds(entries ...string) string {
	return strings.Replace(buildFlow, "\narchives:", strings.Join(entries, "")+"\narchives:", 1)
}

// envDoc is the build environment Read requires, in block form: the entries of
// Env, in its order, each line already indented under the build's dash.
const envDoc = "    env:\n      - CGO_ENABLED=0\n      - GOWORK=off\n      - GOFLAGS=-mod=readonly\n"

// buildDoc returns a .goreleaser.yaml with one build: the lines given, each
// already indented under the build's dash, then the six-platform goos and
// goarch lists.
func buildDoc(lines string) string {
	return "version: 2\n\nbuilds:\n  - id: shift2eu\n    main: ./cmd/shift2eu\n" + lines +
		"    goos: [linux, darwin, windows]\n    goarch: [amd64, arm64]\n"
}

// buildWithBefore returns buildFlow with a before section whose hooks are the
// given lines, each already written as a list item: the accepted build with one
// thing about it changed.
func buildWithBefore(hooks string) string {
	return buildFlow + "\nbefore:\n  hooks:\n" + hooks
}

// buildLists returns a .goreleaser.yaml with one build whose goos and goarch
// lists are exactly the two lines given, each already indented under the
// build's dash: for a case about a list itself rather than about the keys
// around it.
func buildLists(goos, goarch string) string {
	return "version: 2\n\nbuilds:\n  - id: shift2eu\n    main: ./cmd/shift2eu\n" + envDoc +
		goos + "\n" + goarch + "\n"
}

// buildWithComment returns buildReal with comment added as a comment line
// above project_name. The reader skips comments, so a character the check must
// refuse is the only reason a case using it fails.
func buildWithComment(comment string) string {
	return strings.Replace(buildReal, "project_name: shift2eu", comment+"\nproject_name: shift2eu", 1)
}

// TestRead pins the platform list the release builds: every goos x
// goarch pair of this repository's one build, sorted by GOOS then GOARCH, and
// an error for every document the check cannot read the whole list from. The
// release builds the CLI alone, so a second build of any other main package,
// the HTTP API's included, is refused (issue #393).
func TestRead(t *testing.T) {
	t.Parallel()

	// allSix is the goos x goarch product of buildFlow and buildBlock, sorted
	// by GOOS then GOARCH.
	allSix := []releasetarget.Target{
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "windows", GOARCH: "amd64"},
		{GOOS: "windows", GOARCH: "arm64"},
	}

	tests := []struct {
		name       string
		goreleaser string
		want       []releasetarget.Target
		wantErr    bool
	}{
		{
			name:       "flow lists give every pair",
			goreleaser: buildFlow,
			want:       allSix,
		},
		{
			name:       "block lists give every pair",
			goreleaser: buildBlock,
			want:       allSix,
		},
		{
			name:       "an env list in flow form still turns cgo off",
			goreleaser: buildEnvFlow,
			want:       allSix,
		},
		{
			name:       "the real document, with its before hooks and trailing keys, gives every pair",
			goreleaser: buildReal,
			want:       allSix,
		},
		{
			name:       "CRLF line ends throughout still give every pair",
			goreleaser: strings.ReplaceAll(buildReal, "\n", "\r\n"),
			want:       allSix,
		},
		{
			name:       "cgo enabled is refused",
			goreleaser: buildCgoOn,
			wantErr:    true,
		},
		{
			name:       "no env is refused",
			goreleaser: buildNoEnv,
			wantErr:    true,
		},
		{
			name:       "an empty goos list is refused",
			goreleaser: buildEmptyGoos,
			wantErr:    true,
		},
		{
			name:       "a missing goarch key is refused",
			goreleaser: buildNoGoarch,
			wantErr:    true,
		},
		{
			name:       "two builds are refused",
			goreleaser: buildTwo,
			wantErr:    true,
		},
		{
			name:       "the CLI and the server built for the same platforms are refused: the release ships the CLI alone",
			goreleaser: withBuilds(serverBuild("./cmd/shift2eu-server", "    goos: [linux, darwin, windows]", "    goarch: [amd64, arm64]")),
			wantErr:    true,
		},
		{
			name:       "the server built for the same platforms in another order is refused too",
			goreleaser: withBuilds(serverBuild("./cmd/shift2eu-server", "    goos: [windows, linux, darwin]", "    goarch: [arm64, amd64]")),
			wantErr:    true,
		},
		{
			name:       "the server built for more platforms than the CLI is refused",
			goreleaser: withBuilds(serverBuild("./cmd/shift2eu-server", "    goos: [linux, darwin, windows]", "    goarch: [amd64, arm64, riscv64]")),
			wantErr:    true,
		},
		{
			name:       "the CLI built twice is refused",
			goreleaser: withBuilds(serverBuild("./cmd/shift2eu", "    goos: [linux, darwin, windows]", "    goarch: [amd64, arm64]")),
			wantErr:    true,
		},
		{
			name: "the server built twice is refused",
			goreleaser: withBuilds(
				serverBuild("./cmd/shift2eu-server", "    goos: [linux, darwin, windows]", "    goarch: [amd64, arm64]"),
				serverBuild("./cmd/shift2eu-server", "    goos: [linux, darwin, windows]", "    goarch: [amd64, arm64]"),
			),
			wantErr: true,
		},
		{
			name: "the server alone, without the CLI, is refused",
			goreleaser: "version: 2\n\nbuilds:\n" +
				serverBuild("./cmd/shift2eu-server", "    goos: [linux, darwin, windows]", "    goarch: [amd64, arm64]"),
			wantErr: true,
		},
		{
			name:       "a build of another main package beside the CLI is refused",
			goreleaser: withBuilds(serverBuild("./cmd/shift2eu-helper", "    goos: [linux, darwin, windows]", "    goarch: [amd64, arm64]")),
			wantErr:    true,
		},
		{
			name:       "a server build with a tags key is refused",
			goreleaser: withBuilds(serverBuild("./cmd/shift2eu-server", "    tags: [sneaky]\n    goos: [linux, darwin, windows]", "    goarch: [amd64, arm64]")),
			wantErr:    true,
		},
		{
			name:       "an ignore list is refused",
			goreleaser: buildIgnore,
			wantErr:    true,
		},
		{
			name:       "a targets list is refused",
			goreleaser: buildTargets,
			wantErr:    true,
		},
		{
			name:       "an empty document is refused",
			goreleaser: "",
			wantErr:    true,
		},

		// A top-level key the checker does not read could change what the
		// release builds, so only the keys the real document holds pass.
		{
			name:       "a top-level env is refused",
			goreleaser: buildFlow + "\nenv:\n  - FOO=bar\n",
			wantErr:    true,
		},
		{
			name:       "a top-level gomod is refused",
			goreleaser: buildTopGomod,
			wantErr:    true,
		},
		{
			name:       "an unknown top-level key is refused",
			goreleaser: buildFlow + "\nupx:\n  - enabled: true\n",
			wantErr:    true,
		},
		{
			// release says how the GitHub release is published, not what is
			// compiled: GitHubRelease holds it to its one setting.
			name:       "a release section still gives every pair",
			goreleaser: buildFlow + "\nrelease:\n  prerelease: auto\n",
			want:       allSix,
		},
		{
			name:       "a document marker is refused",
			goreleaser: "---\n" + buildFlow,
			wantErr:    true,
		},
		{
			name:       "a merge key is refused",
			goreleaser: buildMergeKey,
			wantErr:    true,
		},

		// before: hooks run before the build, so a hook the check does not
		// know could generate or rewrite what the release compiles. The only
		// accepted hook is go mod tidy -diff, which fails and writes nothing
		// when the module is not tidy (issue #102): plain go mod tidy would
		// rewrite go.mod and go.sum before the -mod=readonly build, and the
		// release would compile a module the commit does not hold.
		{
			name:       "a before hook of go mod tidy -diff and go generate is refused",
			goreleaser: buildBeforeGenerate,
			wantErr:    true,
		},
		{
			name:       "a before hook of plain go mod tidy is refused, since it writes where -diff fails",
			goreleaser: buildWithBefore("    - go mod tidy\n"),
			wantErr:    true,
		},
		{
			name:       "a before hook of go mod tidy -e is refused, since -e reports no error either",
			goreleaser: buildWithBefore("    - go mod tidy -e\n"),
			wantErr:    true,
		},
		{
			name:       "a before hook of go mod tidy -diff -e is refused, since -e hides the difference -diff reports",
			goreleaser: buildWithBefore("    - go mod tidy -diff -e\n"),
			wantErr:    true,
		},
		{
			name:       "a before hook of go mod tidy -diff with another argument is refused",
			goreleaser: buildWithBefore("    - go mod tidy -diff -v\n"),
			wantErr:    true,
		},
		{
			name:       "a before hook of go mod tidy -diff and go mod vendor is refused",
			goreleaser: buildWithBefore("    - go mod tidy -diff\n    - go mod vendor\n"),
			wantErr:    true,
		},
		{
			name:       "a key under before other than hooks is refused",
			goreleaser: buildBeforeOtherKey,
			wantErr:    true,
		},

		// The build environment the release was reviewed for is exactly the
		// three entries Env names, in its order: cgo off, so the binary links no
		// C library; GOWORK=off, so no go.work in a parent directory puts the
		// build in workspace mode; and GOFLAGS=-mod=readonly, so the build never
		// rewrites go.mod, go.sum or a vendor tree (issue #102).
		{
			name:       "an env of only CGO_ENABLED=0 is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n"),
			wantErr:    true,
		},
		{
			name:       "an env without GOWORK=off is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - GOFLAGS=-mod=readonly\n"),
			wantErr:    true,
		},
		{
			name:       "an env without GOFLAGS=-mod=readonly is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - GOWORK=off\n"),
			wantErr:    true,
		},
		{
			name:       "the entries in another order are refused",
			goreleaser: buildDoc("    env:\n      - GOWORK=off\n      - CGO_ENABLED=0\n      - GOFLAGS=-mod=readonly\n"),
			wantErr:    true,
		},
		{
			name:       "GOWORK=on is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - GOWORK=on\n      - GOFLAGS=-mod=readonly\n"),
			wantErr:    true,
		},
		{
			name:       "GOWORK naming a go.work file is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - GOWORK=/some/go.work\n      - GOFLAGS=-mod=readonly\n"),
			wantErr:    true,
		},
		{
			name:       "GOFLAGS=-mod=mod is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - GOWORK=off\n      - GOFLAGS=-mod=mod\n"),
			wantErr:    true,
		},
		{
			name:       "GOFLAGS=-mod=vendor is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - GOWORK=off\n      - GOFLAGS=-mod=vendor\n"),
			wantErr:    true,
		},
		{
			name:       "GOFLAGS=-mod=readonly with another flag is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - GOWORK=off\n      - GOFLAGS=-mod=readonly -tags=x\n"),
			wantErr:    true,
		},
		{
			name:       "a fourth env entry is refused",
			goreleaser: buildDoc(envDoc + "      - FOO=bar\n"),
			wantErr:    true,
		},
		{
			name:       "a templated env entry is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - GOWORK=off\n      - GOFLAGS=-mod=readonly\n      - '{{ \"GOFLAGS\" }}=-tags=sneaky'\n"),
			wantErr:    true,
		},
		{
			name:       "CGO_ENABLED=0 listed twice is refused",
			goreleaser: buildDoc("    env:\n      - CGO_ENABLED=0\n      - CGO_ENABLED=0\n      - GOWORK=off\n      - GOFLAGS=-mod=readonly\n"),
			wantErr:    true,
		},

		// The build keys the check does not read are exactly the ones that
		// cannot change which files the release compiles.
		{
			name:       "a tags key is refused",
			goreleaser: buildDoc(envDoc + "    tags:\n      - sneaky\n"),
			wantErr:    true,
		},
		{
			name:       "an overrides key is refused",
			goreleaser: buildDoc(envDoc + "    overrides:\n      - goos: linux\n        goarch: arm64\n"),
			wantErr:    true,
		},
		{
			name:       "a goamd64 key is refused",
			goreleaser: buildDoc(envDoc + "    goamd64:\n      - v3\n"),
			wantErr:    true,
		},
		{
			name:       "a flag other than -trimpath is refused",
			goreleaser: buildDoc(envDoc + "    flags:\n      - -trimpath\n      - -tags=x\n"),
			wantErr:    true,
		},
		{
			name:       "a templated goos entry is refused",
			goreleaser: buildTemplatedGoos,
			wantErr:    true,
		},
		{
			name:       "a repeated key is refused",
			goreleaser: buildRepeatedGoos,
			wantErr:    true,
		},

		// A ' #' starts a comment, and a comment runs to the end of the line:
		// a closing bracket inside one does not close the list, so the document
		// never says which platforms the release builds.
		{
			name:       "a goos flow list whose closing bracket sits in a comment is refused",
			goreleaser: buildLists("    goos: [linux # ]", "    goarch: [amd64, arm64]"),
			wantErr:    true,
		},
		{
			name:       "a goarch flow list whose closing bracket sits in a comment is refused",
			goreleaser: buildLists("    goos: [linux, darwin, windows]", "    goarch: [amd64 # ]"),
			wantErr:    true,
		},
		{
			name:       "an anchored env list is refused",
			goreleaser: buildAnchoredEnv,
			wantErr:    true,
		},
		{
			name:       "an aliased goos list is refused",
			goreleaser: buildAliasedGoos,
			wantErr:    true,
		},

		// The characters the document may hold: a comment is no place for a
		// control character either, since no two YAML readers agree on what
		// the ones that look like line breaks do.
		{
			name:       "a lone CR is refused",
			goreleaser: buildWithComment("# a lone\rCR"),
			wantErr:    true,
		},
		{
			name:       "U+0085 is refused",
			goreleaser: buildWithComment("# a NEL:\u0085x"),
			wantErr:    true,
		},
		{
			name:       "U+2028 is refused",
			goreleaser: buildWithComment("# a line separator:\u2028x"),
			wantErr:    true,
		},
		{
			name:       "U+2029 is refused",
			goreleaser: buildWithComment("# a paragraph separator:\u2029x"),
			wantErr:    true,
		},
		{
			name:       "a byte-order mark is refused",
			goreleaser: buildWithComment("#\uFEFF a byte-order mark"),
			wantErr:    true,
		},
		{
			name:       "a tab is refused",
			goreleaser: buildWithComment("#\ta tab"),
			wantErr:    true,
		},
		{
			name:       "another C0 control is refused",
			goreleaser: buildWithComment("#\x01a control"),
			wantErr:    true,
		},
		{
			name:       "DEL is refused",
			goreleaser: buildWithComment("#\x7fDEL"),
			wantErr:    true,
		},
		{
			name:       "invalid UTF-8 is refused",
			goreleaser: buildWithComment("#\xff not UTF-8"),
			wantErr:    true,
		},
		{
			name:       "the auditor's CR inside the goarch comment is an error, not six platforms",
			goreleaser: buildCRInComment,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := releasetarget.Read([]byte(tc.goreleaser))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("Read() = %v, nil; want an error", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("Read(): %v", err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Read() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEnv pins the build environment Read requires, the one the release and
// the image must both build with: cgo off, no Go workspace and modules
// read-only, in that order (issue #102). Env returns a fresh copy each time,
// so a caller that reorders, truncates or edits the slice it got cannot change
// what the next call, and so the next build, is held to.
func TestEnv(t *testing.T) {
	t.Parallel()

	want := []string{"CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=-mod=readonly"}

	got := releasetarget.Env()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Env() mismatch (-want +got):\n%s", diff)
	}

	// The caller that mutates the slice it was given must not change what a
	// later call returns: Read holds every build entry to Env, and a caller
	// that edited the shared slice would loosen that rule for everyone.
	for i := range got {
		got[i] = "MUTATED"
	}

	if diff := cmp.Diff(want, releasetarget.Env()); diff != "" {
		t.Errorf("Env() changed after a caller mutated the slice it returned (-want +got):\n%s\nwant a fresh copy on every call", diff)
	}
}
