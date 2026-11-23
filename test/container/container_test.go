package container_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
)

const (
	dockerfileName     = "Dockerfile"
	dockerignoreName   = ".dockerignore"
	goModName          = "go.mod"
	goreleaserName     = ".goreleaser.yaml"
	versionPackagePath = "github.com/timothymamo/shift2eu/internal/version"
	mainPackage        = "./cmd/shift2eu"

	// The second binary the release used to ship, the HTTP API (PRD #103, ADR
	// 0019): its builds entry, the package it was built from, the path the image
	// kept it at and the stage of the Dockerfile that shipped it. The release
	// builds neither any more (issue #393), so the tests below hold the
	// repository to their absence.
	serverBuildID     = "shift2eu-server"
	serverMainPackage = "./cmd/shift2eu-server"
	serverBinaryPath  = "/shift2eu-server"
	serverStageName   = "server"
	cliBinaryPath     = "/shift2eu"

	// The variable the build stage sets to the name of the toolchain go must use,
	// and the value that keeps it to the Go of the base image: `local` makes go
	// fail, with its own error, instead of fetching another toolchain or building
	// with a different one (issue #383).
	toolchainEnv   = "GOTOOLCHAIN"
	toolchainLocal = "local"
)

// TestBuilderGoVersionMatchesGoMod pins that the builder stage is the golang
// image of the Go version go.mod declares, so the image and a local build
// compile with the same toolchain. A tag that only starts with the same major
// and minor version, such as golang:1.27, is not enough: it moves to another
// patch release without a change here.
func TestBuilderGoVersionMatchesGoMod(t *testing.T) {
	t.Parallel()

	want := goDirectiveVersion(t, readRepoFile(t, goModName))
	dockerfile := readRepoFile(t, dockerfileName)

	var builders []baseImage
	for _, image := range fromImages(dockerfileInstructions(t, dockerfile)) {
		if strings.HasPrefix(image.image, "golang:") {
			builders = append(builders, image)
		}
	}

	if len(builders) == 0 {
		t.Fatalf("%s: no stage starts FROM golang:<version>, so there is no builder stage to compare with %s", dockerfileName, goModName)
	}

	for _, builder := range builders {
		tag := imageTag(builder.image)

		if tag != want && !strings.HasPrefix(tag, want+"-") {
			t.Errorf("%s:%d: the builder stage uses the image %q, whose tag %q is not the Go version %s declares (go %s): want golang:%s or golang:%s-<variant>, so the image is built with the toolchain the module requires",
				dockerfileName, builder.line, builder.image, tag, goModName, want, want, want)
		}
	}
}

// TestBuildStagePinsLocalToolchain pins that the stage that runs `go build` sets
// ENV GOTOOLCHAIN=local before it runs a single go command, so a base image whose
// Go differs from go.mod's go line fails the build with Go's own error instead of
// fetching another toolchain or building with one the repository was never
// reviewed against (issue #383). The RUN instruction that downloads the modules
// is the first go command of that stage, so an ENV after it would leave that step
// unprotected. Docker applies an ENV to the instructions that follow it, so the
// pin must also be the last value the stage gives the variable: an ENV
// GOTOOLCHAIN=auto after a correct pin puts another toolchain back in force for
// `go build`, and is reported as well.
//
// The check runs over the repository's Dockerfile and over copies of a fixture
// that carry one fault each: the ENV removed, moved to the image stage, given
// another value, left after the module download, or overridden by a later ENV
// before the build. A check that cannot tell those apart would pass on a
// Dockerfile that pins nothing, or on one whose last word on the toolchain is
// not local.
func TestBuildStagePinsLocalToolchain(t *testing.T) {
	t.Parallel()

	const fixture = `FROM golang:1.27.4-alpine AS build
ENV GOTOOLCHAIN=local
RUN go mod download
RUN go build -o /out/shift2eu ./cmd/shift2eu

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/shift2eu /shift2eu
`

	const (
		pin         = "ENV GOTOOLCHAIN=local\n"
		modDownload = "RUN go mod download\n"
		imageFrom   = "FROM gcr.io/distroless/static-debian13:nonroot\n"
	)

	tests := []struct {
		name       string
		dockerfile string
		// want holds parts of the findings the check must return; none means
		// the Dockerfile pins the toolchain.
		want []string
	}{
		{
			name:       "the repository's Dockerfile",
			dockerfile: readRepoFile(t, dockerfileName),
		},
		{
			name:       "a fixture that pins the toolchain",
			dockerfile: fixture,
		},
		{
			name:       "a fixture that pins it in the legacy ENV form",
			dockerfile: strings.Replace(fixture, "ENV GOTOOLCHAIN=local", "ENV GOTOOLCHAIN local", 1),
		},
		{
			name:       "a fixture that quotes the value",
			dockerfile: strings.Replace(fixture, "ENV GOTOOLCHAIN=local", `ENV GOTOOLCHAIN="local"`, 1),
		},
		{
			name:       "no ENV GOTOOLCHAIN at all",
			dockerfile: strings.Replace(fixture, pin, "", 1),
			want:       []string{"no ENV GOTOOLCHAIN=local"},
		},
		{
			name:       "the ENV in the image stage alone",
			dockerfile: strings.Replace(strings.Replace(fixture, pin, "", 1), imageFrom, imageFrom+pin, 1),
			want:       []string{"no ENV GOTOOLCHAIN=local"},
		},
		{
			name:       "another value",
			dockerfile: strings.Replace(fixture, "GOTOOLCHAIN=local", "GOTOOLCHAIN=auto", 1),
			want:       []string{"GOTOOLCHAIN=auto"},
		},
		{
			name:       "after the first RUN that runs go",
			dockerfile: strings.Replace(fixture, pin+modDownload, modDownload+pin, 1),
			want:       []string{"after the RUN instruction"},
		},
		{
			name:       "after a go command named by its path",
			dockerfile: strings.Replace(fixture, pin+modDownload, "RUN /usr/local/go/bin/go mod download\n"+pin, 1),
			want:       []string{"after the RUN instruction"},
		},
		{
			name:       "after a go command inside sh -c",
			dockerfile: strings.Replace(fixture, pin+modDownload, "RUN sh -c \"go mod download\"\n"+pin, 1),
			want:       []string{"after the RUN instruction"},
		},
		{
			name:       "after a bare go command",
			dockerfile: strings.Replace(fixture, pin+modDownload, "RUN go\n"+pin+modDownload, 1),
			want:       []string{"after the RUN instruction"},
		},
		{
			// A word that merely holds go, such as gofmt, golang or cargo, runs
			// no go command: only `go` itself, however it is written, counts.
			name: "a RUN that only mentions words containing go",
			dockerfile: strings.Replace(fixture, "FROM golang:1.27.4-alpine AS build\n",
				"FROM golang:1.27.4-alpine AS build\nRUN apk add --no-cache ca-certificates && ls /usr/local/go/bin/gofmt /usr/local/go/lib/golang /opt/cargo\n", 1),
		},
		{
			name:       "another value after the first RUN that runs go",
			dockerfile: strings.Replace(fixture, pin+modDownload, modDownload+"ENV GOTOOLCHAIN=auto\n", 1),
			want:       []string{"after the RUN instruction", "GOTOOLCHAIN=auto"},
		},
		{
			// A pin before the first go command is not enough: Docker applies
			// an ENV to the instructions that follow it, so a later ENV puts
			// another toolchain back in force for `go build` and the build runs
			// with one the repository was never reviewed against.
			name:       "a non-local value after the module download",
			dockerfile: strings.Replace(fixture, modDownload, modDownload+"ENV GOTOOLCHAIN=auto\n", 1),
			// The later ENV is on line 4 of the fixture, the go build on line 5.
			want: []string{"a non-local value after the module download:4: ", "GOTOOLCHAIN=auto"},
		},
		{
			name:       "a second local value after the module download",
			dockerfile: strings.Replace(fixture, modDownload, modDownload+pin, 1),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings := toolchainFindings(t, tc.name, tc.dockerfile)

			for _, finding := range findings {
				if !slices.ContainsFunc(tc.want, func(want string) bool { return strings.Contains(finding, want) }) {
					t.Errorf("unexpected finding: %s", finding)
				}
			}

			for _, want := range tc.want {
				if !slices.ContainsFunc(findings, func(finding string) bool { return strings.Contains(finding, want) }) {
					t.Errorf("no finding mentions %q, got %q", want, findings)
				}
			}
		})
	}
}

// toolchainFindings returns what name, a Dockerfile, gets wrong about the Go
// toolchain its build stage uses, and no finding when the stage pins it: the
// stage that runs `go build` must set GOTOOLCHAIN=local before its first RUN
// instruction that runs a go command, so a base image whose Go differs from
// go.mod's go line fails the build instead of fetching a toolchain of its own
// (issue #383). The value that takes effect at that first command is the one the
// last ENV before it sets, since Docker applies an ENV to the instructions that
// follow it, and every ENV of the stage that sets another value is reported
// wherever it stands: one after a correct pin leaves `go build`, and anything
// else after it, on a toolchain the repository was never reviewed against.
func toolchainFindings(t *testing.T, name, dockerfile string) []string {
	t.Helper()

	builder, stage := buildStage(t, dockerfileInstructions(t, dockerfile))

	firstGo, ok := firstGoRun(stage)
	if !ok {
		return []string{fmt.Sprintf("%s:%d: the build stage runs no go command, so there is no step for the toolchain pin to protect; want a stage that runs `go mod download` and `go build`",
			name, builder.line)}
	}

	// envValue is one ENV of the stage that sets the toolchain variable, with
	// the value Docker applies from it on.
	type envValue struct {
		at    instruction
		value string
	}

	var (
		applied      instruction
		appliedValue string
		pinned       bool

		// The first ENV of the stage that sets the variable, wherever it
		// stands: when none comes before the first go command, it is the one
		// the finding about a pin in the wrong place names.
		firstSet   instruction
		firstValue string
		firstFound bool

		// Every ENV of the stage that sets a value other than local, in file
		// order.
		notLocal []envValue
	)

	for _, in := range stage {
		if in.keyword() != "ENV" {
			continue
		}

		value, setsIt := envAssignments(in)[toolchainEnv]
		if !setsIt {
			continue
		}

		if !firstFound {
			firstSet, firstValue, firstFound = in, value, true
		}

		if in.line < firstGo.line {
			applied, appliedValue, pinned = in, value, true
		}

		if value != toolchainLocal {
			notLocal = append(notLocal, envValue{at: in, value: value})
		}
	}

	var findings []string

	// reported holds the lines the checks below name, so an ENV that is both the
	// value in force at the first go command (or the only one the stage sets) and
	// a non-local value is reported once, with the finding that says what it
	// should have been.
	reported := make(map[int]struct{}, len(notLocal))

	switch {
	case !pinned && firstFound:
		findings = append(findings, fmt.Sprintf("%s:%d: the build stage sets `ENV %s=%s` only at line %d, after the RUN instruction at line %d that runs go (%q), so that step still runs with whatever toolchain the base image holds: want the ENV before the first RUN instruction that runs go, the module download included (issue #383)",
			name, firstSet.line, toolchainEnv, firstValue, firstSet.line, firstGo.line, firstGo.arguments()))
		reported[firstSet.line] = struct{}{}
	case pinned && appliedValue != toolchainLocal:
		findings = append(findings, fmt.Sprintf("%s:%d: the build stage sets %s=%s, want `ENV %s=%s`: with another value go fetches or uses another toolchain instead of failing when the base image's Go differs from the go line of %s (issue #383)",
			name, applied.line, toolchainEnv, appliedValue, toolchainEnv, toolchainLocal, goModName))
		reported[applied.line] = struct{}{}
	case !pinned:
		findings = append(findings, fmt.Sprintf("%s:%d: the build stage, the one that runs `go build`, has no ENV %s=%s: want `ENV %s=%s` in that stage, before the first RUN instruction that runs go, so a base image whose Go differs from the go line of %s fails the build instead of fetching another toolchain or building with a different one (issue #383)",
			name, builder.line, toolchainEnv, toolchainLocal, toolchainEnv, toolchainLocal, goModName))
	}

	// Every other ENV of the stage that leaves the pin: Docker applies it to the
	// instructions that follow, so the go command after it runs on another
	// toolchain than the one checked above.
	for _, set := range notLocal {
		if _, once := reported[set.at.line]; once {
			continue
		}

		findings = append(findings, fmt.Sprintf("%s:%d: the build stage sets `ENV %s=%s` at line %d, which is not `%s`: go fetches or uses another toolchain from there on instead of failing when the base image's Go differs from the go line of %s, so every go command after it, `go build` included, runs on a toolchain the repository was never reviewed against (issue #383)",
			name, set.at.line, toolchainEnv, set.value, set.at.line, toolchainLocal, goModName))
	}

	return findings
}

// TestEveryBaseImageIsDigestPinned pins that every FROM instruction names its
// image by digest, so a rebuild uses the same base image and Renovate updates
// the pin (CLAUDE.md rule 7). A reference to a stage declared earlier, and
// scratch, name no registry image and are left out.
func TestEveryBaseImageIsDigestPinned(t *testing.T) {
	t.Parallel()

	dockerfile := readRepoFile(t, dockerfileName)
	images := fromImages(dockerfileInstructions(t, dockerfile))

	if len(images) == 0 {
		t.Fatalf("%s: no FROM instruction, so there is no base image to pin", dockerfileName)
	}

	digest := regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
	declared := make(map[string]struct{}, len(images))

	for _, image := range images {
		_, earlierStage := declared[image.image]

		switch {
		case earlierStage, strings.EqualFold(image.image, "scratch"):
		case digest.MatchString(image.image):
		default:
			t.Errorf("%s:%d: the base image %q is not pinned by digest: want <image>@sha256:<64 hex characters>, so a rebuild uses the same base image",
				dockerfileName, image.line, image.image)
		}

		if image.stage != "" {
			declared[image.stage] = struct{}{}
		}
	}
}

// TestBuildMatchesGoReleaser pins that the image's build carries the settings
// .goreleaser.yaml gives the release binary: the build environment the release
// requires (cgo off, no Go workspace, modules read-only), -trimpath, the -s -w
// linker flags and the internal/version -X flags, the same main package, and a
// cross build from TARGETOS and TARGETARCH. The test reads the settings from
// .goreleaser.yaml rather than pinning a copy of them here.
//
// Every setting is read from the one RUN instruction that runs `go build`, not
// from the file as a whole, and the env settings from that command's
// environment prefix: an assignment moved to another instruction, or mentioned
// only in a comment or a flag value, does not build anything and must not
// satisfy the check.
//
// The command line is read as a whole, not searched for strings: parseBuildCommand
// holds it to the release env followed directly by `go build`, and the go build
// to the three flags .goreleaser.yaml gives it. A stray flag such as -mod=mod,
// a command between the prefix and the build, or a variable assigned twice would
// build with settings the release does not use, so each of them is refused
// rather than left to a mention somewhere in the text.
//
// It runs once per image the release ships: the CLI, which the last stage of
// the Dockerfile builds. The HTTP API's binary and its server stage are gone
// (issue #393), which TestTheDockerfileHasNoServerStage pins.
func TestBuildMatchesGoReleaser(t *testing.T) {
	t.Parallel()

	instructions := dockerfileInstructions(t, readRepoFile(t, dockerfileName))
	builds := parseGoreleaserBuilds(t, readRepoFile(t, goreleaserName))

	tests := []struct {
		name      string
		id        string
		pkg       string
		stage     string
		stageDesc string
		imageBin  string
	}{
		{
			name:      "cli",
			id:        "shift2eu",
			pkg:       mainPackage,
			stage:     "",
			stageDesc: "the final stage, the CLI image",
			imageBin:  cliBinaryPath,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertBuildMatchesGoReleaser(t, instructions, goreleaserBuildSettings(t, builds, tc.id), tc.pkg, tc.stage, tc.stageDesc, tc.imageBin)
		})
	}
}

// assertBuildMatchesGoReleaser holds one image's build to the .goreleaser.yaml
// build entry that ships the same binary: settings is that entry, pkg the main
// package the image builds, stage the name of the stage that ships it, described
// by stageDesc, and imageBin the path its ENTRYPOINT runs. The stage is looked
// up only once the build itself is matched, so a missing stage is reported by the
// test that pins the stage, not as a gap in this one.
func assertBuildMatchesGoReleaser(t *testing.T, instructions []instruction, settings goreleaserBuild, pkg, stageName, stageDesc, imageBin string) {
	t.Helper()

	build := goBuildRunFor(t, instructions, pkg)
	command := parseBuildCommand(t, build, pkg)

	// The env entries are releasetarget.Env(), the list releasetarget.Read
	// holds every build's env to (issue #102): cgo off, so nothing links a C
	// library; GOWORK=off, so no go.work in a parent directory puts the build in
	// workspace mode; and GOFLAGS=-mod=readonly, so the build never rewrites
	// go.mod, go.sum or a vendor tree. They are read from Env rather than
	// listed here, so the reader and this test stay in step by construction: a
	// setting added to the release env is checked against the image's build
	// without a second edit that could be forgotten.
	settingsToMatch := make([]buildSetting, 0, len(releasetarget.Env())+2)
	for _, setting := range releasetarget.Env() {
		settingsToMatch = append(settingsToMatch, buildSetting{setting: setting, list: "env", in: settings.env})
	}

	settingsToMatch = append(settingsToMatch,
		buildSetting{setting: "-trimpath", list: "flags", in: settings.flags},
		buildSetting{setting: "-s -w", list: "ldflags", in: settings.ldflags},
	)

	for _, tc := range settingsToMatch {
		if !slices.Contains(tc.in, tc.setting) {
			t.Errorf("%s: the builds entry no longer sets %q in its %s list, so this test has nothing of the release build to match",
				goreleaserName, tc.setting, tc.list)

			continue
		}

		switch tc.list {
		case "env":
			if !slices.Contains(command.env, tc.setting) {
				t.Errorf("%s:%d: the RUN instruction that runs `go build` sets the environment prefix %q, which does not assign %q, so the image and the release are built differently; %s sets it for the release binary, and an assignment on another instruction, in a comment or in a flag value does not build anything",
					dockerfileName, build.line, strings.Join(command.env, " "), tc.setting, goreleaserName)
			}
		case "flags":
			if !slices.Contains(command.flags, tc.setting) {
				t.Errorf("%s:%d: the RUN instruction that runs `go build` does not pass %q, which %s sets for the release binary, so the image and the release are built differently",
					dockerfileName, build.line, tc.setting, goreleaserName)
			}
		default:
			if !strings.Contains(command.ldflags, tc.setting) {
				t.Errorf("%s:%d: the RUN instruction that runs `go build` passes the linker flags %q, which do not hold %q, so the image and the release are built differently",
					dockerfileName, build.line, command.ldflags, tc.setting)
			}
		}
	}

	prefixes := xFlagPrefixes(settings.ldflags)
	for _, name := range []string{"version", "commit", "date"} {
		want := versionPackagePath + "." + name + "="
		if !slices.Contains(prefixes, want) {
			t.Errorf("%s: ldflags no longer carry -X %s, so this test cannot check that the image stamps %s",
				goreleaserName, want, name)
		}
	}

	for _, prefix := range prefixes {
		flag := "-X " + prefix
		if !strings.Contains(command.ldflags, flag) {
			t.Errorf("%s:%d: the RUN instruction that runs `go build` does not pass the linker flag %q in its -ldflags value %q, which %s sets for the release binary; the image must take the value from a build arg, so only the part up to the = is compared",
				dockerfileName, build.line, flag, command.ldflags, goreleaserName)
		}
	}

	if command.pkg != pkg {
		t.Errorf("%s:%d: the RUN instruction that runs `go build` builds %q, want %s, the main package %s builds the release binary from",
			dockerfileName, build.line, command.pkg, pkg, goreleaserName)
	}

	// The -o value is where this build writes the binary, and the one path the
	// stage that ships it copies it from is the binary the image runs. It is
	// pinned to that path rather than read as an opaque value: a shell reads
	// `/out/shift2eu;GOFLAGS=-mod=mod${IFS}go${IFS}build ...` as the end of this go
	// build and a second command, so an -o value that is not the copied path
	// leaves the image shipping a binary built outside the release env.
	copied, destination := copiedBinary(t, instructions, stageFor(t, instructions, stageName), stageDesc)
	if command.output != copied {
		t.Errorf("%s:%d: the RUN instruction that runs `go build` writes the binary to %q, want %q, the path %s copies it from: any other value, such as a path followed by a `;` and a second go build, overwrites the binary outside this env",
			dockerfileName, build.line, command.output, copied, stageDesc)
	}

	if destination != imageBin {
		t.Errorf("%s: %s copies the binary to %q, want %q, the path its ENTRYPOINT runs: a COPY to any other path leaves the image with no binary to start",
			dockerfileName, stageDesc, destination, imageBin)
	}

	// The prefix may name no other variable, and must assign each name exactly
	// once. The go command takes the last value a variable is assigned, so
	// `GOFLAGS=-mod=readonly GOFLAGS=-mod=mod` would build with modules on the
	// write side while the check above still finds the readonly entry, and a
	// name the test does not know, such as GOEXPERIMENT or GOTOOLCHAIN, would
	// change the build in a way the release env was never reviewed for.
	wantNames := make([]string, 0, len(releasetarget.Env())+2)
	for _, setting := range releasetarget.Env() {
		name, _, _ := strings.Cut(setting, "=")
		wantNames = append(wantNames, name)
	}

	wantNames = append(wantNames, "GOOS", "GOARCH")

	gotNames := make([]string, 0, len(command.env))
	for _, setting := range command.env {
		name, _, _ := strings.Cut(setting, "=")
		gotNames = append(gotNames, name)
	}

	slices.Sort(wantNames)
	slices.Sort(gotNames)

	if diff := cmp.Diff(wantNames, gotNames); diff != "" {
		t.Errorf("%s:%d: the environment prefix %q assigns the names %q, want those of releasetarget.Env() plus GOOS and GOARCH (-want +got):\n%s\nwant exactly one assignment per name and no other variable: a name assigned twice keeps the last value, so the build would use a value the release env does not give",
			dockerfileName, build.line, strings.Join(command.env, " "), gotNames, diff)
	}

	for _, tc := range []struct{ env, buildArg string }{
		{env: "GOOS", buildArg: "TARGETOS"},
		{env: "GOARCH", buildArg: "TARGETARCH"},
	} {
		if !usesBuildArg(command.env, tc.env, tc.buildArg) {
			t.Errorf("%s:%d: the RUN instruction that runs `go build` does not set %s from %s (as $%s or ${%s}), so docker buildx --platform builds every platform for the builder's own",
				dockerfileName, build.line, tc.env, tc.buildArg, tc.buildArg, tc.buildArg)
		}
	}
}

// TestGoReleaserBuildsTheCLIOnly pins that the release builds one binary, the
// CLI's (issue #393). .goreleaser.yaml holds one entry of the builds list, id
// shift2eu, main ./cmd/shift2eu and binary shift2eu, and no entry for the HTTP
// API: a second one would publish a binary the release no longer ships, for
// platforms and with settings of its own.
func TestGoReleaserBuildsTheCLIOnly(t *testing.T) {
	t.Parallel()

	builds := parseGoreleaserBuilds(t, readRepoFile(t, goreleaserName))

	ids := make([]string, 0, len(builds))
	for _, build := range builds {
		ids = append(ids, build.id)
	}

	if diff := cmp.Diff([]string{"shift2eu"}, ids); diff != "" {
		t.Errorf("%s: the builds: section holds the entries %q (-want +got):\n%s\nwant exactly one, the CLI's: the release ships shift2eu alone, so the HTTP API's entry is gone (issue #393)",
			goreleaserName, ids, diff)
	}

	cli := goreleaserBuildSettings(t, builds, "shift2eu")

	for _, tc := range []struct{ field, want, got string }{
		{field: "main", want: mainPackage, got: cli.main},
		{field: "binary", want: "shift2eu", got: cli.binary},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: the builds entry %q at line %d sets %s to %q, want %q",
				goreleaserName, "shift2eu", cli.line, tc.field, tc.got, tc.want)
		}
	}

	goreleaser := readRepoFile(t, goreleaserName)

	for _, retired := range []string{serverBuildID, serverMainPackage} {
		if strings.Contains(goreleaser, retired) {
			t.Errorf("%s names %q, want no mention of the HTTP API's binary: the release ships the CLI alone (issue #393)",
				goreleaserName, retired)
		}
	}
}

// TestBuildCommandFields pins how the command of the RUN instruction that runs
// `go build` is read: fields are split the way a shell splits them, keep their
// quotation marks, and a field that carries a shell operator outside any
// quotation marks is refused. The -o value and an unquoted -ldflags= value are
// one field each however long they are, so without the refusal a payload such as
// `/out/shift2eu;GOFLAGS=-mod=mod${IFS}go${IFS}build${IFS}-o${IFS}/out/shift2eu ...`
// reads as a path here while the shell runs a second build with it.
func TestBuildCommandFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    []string
		wantErr string
	}{
		{
			name:    "a quoted value reads as one field and keeps its quotes",
			command: `go build -trimpath -ldflags "-s -w -X pkg.version=${VERSION}" -o /out/shift2eu ./cmd/shift2eu`,
			want:    []string{"go", "build", "-trimpath", "-ldflags", `"-s -w -X pkg.version=${VERSION}"`, "-o", "/out/shift2eu", "./cmd/shift2eu"},
		},
		{
			name:    "an operator inside the quotation marks is part of the value",
			command: `go build -trimpath -ldflags "-s -w" -o "/out/shift2eu;true" ./cmd/shift2eu`,
			want:    []string{"go", "build", "-trimpath", "-ldflags", `"-s -w"`, "-o", `"/out/shift2eu;true"`, "./cmd/shift2eu"},
		},
		{
			name:    "an unquoted semicolon in the -o value is refused",
			command: `go build -trimpath -ldflags "-s -w" -o /out/shift2eu;GOFLAGS=-mod=mod${IFS}go${IFS}build${IFS}-o${IFS}/out/shift2eu${IFS}./cmd/shift2eu;: ./cmd/shift2eu`,
			wantErr: ";",
		},
		{
			name:    "an unquoted semicolon in an unquoted -ldflags value is refused",
			command: `go build -trimpath -ldflags=-s;true -o /out/shift2eu ./cmd/shift2eu`,
			wantErr: ";",
		},
		{
			name:    "an unquoted ampersand is refused",
			command: `go build -trimpath -ldflags "-s -w" -o /out/shift2eu&&true ./cmd/shift2eu`,
			wantErr: "&",
		},
		{
			name:    "an unquoted pipe is refused",
			command: `go build -trimpath -ldflags "-s -w" -o /out/shift2eu|true ./cmd/shift2eu`,
			wantErr: "|",
		},
		{
			name:    "an unquoted backtick is refused",
			command: "go build -trimpath -ldflags \"-s -w\" -o /out/`true` ./cmd/shift2eu",
			wantErr: "`",
		},
		{
			name:    "an unquoted command substitution is refused",
			command: `go build -trimpath -ldflags "-s -w" -o /out/$(true) ./cmd/shift2eu`,
			wantErr: "$(",
		},
		{
			name:    "a quote the command never closes is refused",
			command: `go build -trimpath -ldflags "-s -w -o /out/shift2eu ./cmd/shift2eu`,
			wantErr: "unclosed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := splitCommandFields(tc.command)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("splitCommandFields(%q) = %q, want an error naming %q", tc.command, got, tc.wantErr)
				}

				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("splitCommandFields(%q) error = %v, want it to name %q", tc.command, err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("splitCommandFields(%q) error = %v, want no error", tc.command, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("splitCommandFields(%q) fields mismatch (-want +got):\n%s", tc.command, diff)
			}
		})
	}
}

// TestBuilderStageCrossCompilesOnBuildPlatform pins that the stage running
// `go build` starts FROM --platform=$BUILDPLATFORM, so `docker buildx build
// --platform linux/arm64` compiles the binary on the build host and
// cross-compiles it for the target, instead of running the whole builder stage
// under QEMU emulation (docs/adr/0013-ship-shift2eu-as-a-distroless-container-image.md).
// The builder is found by the go build it runs rather than by its name, so a
// renamed or reordered stage is still the one checked.
func TestBuilderStageCrossCompilesOnBuildPlatform(t *testing.T) {
	t.Parallel()

	assertBuilderCrossCompiles(t, readRepoFile(t, dockerfileName))
}

// TestFinalStageIsMinimalAndNonRoot pins the last stage of the Dockerfile, the
// CLI image: a base image with nothing to install, patch or run but the binary
// and the CA certificates; a non-root user; the binary as the exec-form
// entrypoint; and the OCI labels that say where the image comes from and under
// which license. It stays the last stage, so `docker build .` and make image
// keep building the CLI with the entrypoint it always had (ADR 0013).
func TestFinalStageIsMinimalAndNonRoot(t *testing.T) {
	t.Parallel()

	final := finalStage(t, dockerfileInstructions(t, readRepoFile(t, dockerfileName)))

	// The CLI image takes its command from the command line alone, so a CMD
	// here would run a default command, or prepend arguments to its own, in
	// place of what `docker run shift2eu assess ...` asks for.
	for _, cmd := range stageCommands(final) {
		t.Errorf("%s:%d: the final stage, the CLI image, has CMD %s: want none, so the arguments given to `docker run` reach the binary as they always have and no default command is run in their place",
			dockerfileName, cmd.line, cmd.arguments())
	}

	assertMinimalNonRootStage(t, final, "the final stage", cliBinaryPath)
}

// imageDocDir is where the image keeps the licence and the NOTICE attribution
// Apache-2.0 section 4 asks a redistribution to carry (issue #495).
const imageDocDir = "/usr/share/doc/shift2eu"

// licenseFileMode is the mode the final stage gives both files: the same for
// every builder, whatever its checkout umask sets, and read-only for the reader
// of the image.
const licenseFileMode = "0444"

// imageDocDirMode is the mode the final stage must create imageDocDir with,
// before it copies the files into it. BuildKit gives a COPY's --chmod to the
// directories that COPY creates as well, so a directory the 0444 COPY makes has
// no search bit and the image's nonroot user cannot enter it to read either
// file (issue #495).
const imageDocDirMode = "0755"

// shippedLicenseFiles are the files the image ships from the repository root,
// each at the path in the image, read-only.
var shippedLicenseFiles = []struct{ name, path string }{
	{name: "LICENSE", path: imageDocDir + "/LICENSE"},
	{name: "NOTICE", path: imageDocDir + "/NOTICE"},
}

// TestTheImageShipsLicenseAndNotice pins issue #495: the image is a
// redistribution of the project, so Apache-2.0 section 4 asks it to carry the
// licence and the NOTICE attribution beside the binary it publishes. The final
// stage copies both from the build context into imageDocDir with --chmod=0444,
// so neither the builder's checkout umask nor a reader of the image decides the
// mode, .dockerignore leaves them in the context, so the COPY finds them, and a
// GoReleaser docker section, were the release to build the image, puts them into
// the context it sends to the daemon.
//
// The check runs over the repository's build files and over fixtures that
// carry one fault each: a file the stage does not copy, a file it copies
// somewhere else, a file it copies with no --chmod=0444, a context that leaves
// it out, and a GoReleaser docker entry that names one file alone. A check that
// cannot tell those apart would pass on an image that ships neither.
//
// It also holds the directory the files land in: BuildKit gives a COPY's
// --chmod to the directories that COPY creates as well, so a stage that lets the
// 0444 COPY create imageDocDir leaves a directory without a search bit, which
// the image's nonroot user cannot enter to read either file. Fixtures that drop
// the directory COPY, move it after the licence COPY, or make the directory with
// another mode must each be reported.
func TestTheImageShipsLicenseAndNotice(t *testing.T) {
	t.Parallel()

	// A build that carries both files, which the faults below are made from.
	const fixture = `FROM golang:1.27.4-alpine AS build
RUN go build -o /out/shift2eu ./cmd/shift2eu

FROM build AS doc
RUN mkdir -m 0755 /doc

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/shift2eu /shift2eu
COPY --from=doc /doc/ /usr/share/doc/shift2eu/
COPY --chmod=0444 LICENSE NOTICE /usr/share/doc/shift2eu/
`

	const (
		bothFiles = "COPY --chmod=0444 LICENSE NOTICE /usr/share/doc/shift2eu/\n"
		// The stage that makes imageDocDir 0755, and the COPY that carries it
		// into the image before the licence COPY. BuildKit gives a COPY's
		// --chmod to the directories it creates too, so the 0444 COPY must find
		// the directory already there (issue #495).
		docStage = "FROM build AS doc\nRUN mkdir -m 0755 /doc\n\n"
		docCopy  = "COPY --from=doc /doc/ /usr/share/doc/shift2eu/\n"
	)

	tests := []struct {
		name string
		// dockerfile is the Dockerfile the check reads.
		dockerfile string
		// dockerignore is the .dockerignore the check reads, and "" skips
		// that half of the check.
		dockerignore string
		// goreleaser is the .goreleaser.yaml the check reads for a docker
		// section, and "" skips that half of the check.
		goreleaser string
		// want holds parts of the findings the check must return; none means
		// the image ships both files.
		want []string
	}{
		{
			name:         "the repository's build files",
			dockerfile:   readRepoFile(t, dockerfileName),
			dockerignore: readRepoFile(t, dockerignoreName),
			goreleaser:   readRepoFile(t, goreleaserName),
		},
		{
			name:         "a fixture that ships both files",
			dockerfile:   fixture,
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			goreleaser:   "builds:\n  - id: shift2eu\n",
		},
		{
			name:         "a Dockerfile that copies neither file",
			dockerfile:   strings.Replace(fixture, bothFiles, "", 1),
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			want:         []string{"copies no LICENSE", "copies no NOTICE"},
		},
		{
			name:         "a Dockerfile that copies NOTICE alone",
			dockerfile:   strings.Replace(fixture, bothFiles, "COPY --chmod=0444 NOTICE /usr/share/doc/shift2eu/\n", 1),
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			want:         []string{"copies no LICENSE"},
		},
		{
			name:         "a Dockerfile that copies both files elsewhere",
			dockerfile:   strings.Replace(fixture, bothFiles, "COPY --chmod=0444 LICENSE NOTICE /licenses/\n", 1),
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			want:         []string{`LICENSE to "/licenses/LICENSE"`, `NOTICE to "/licenses/NOTICE"`},
		},
		{
			name:         "a Dockerfile that copies both files with no mode",
			dockerfile:   strings.Replace(fixture, "--chmod="+licenseFileMode+" ", "", 1),
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			want:         []string{"copies LICENSE without --chmod=0444", "copies NOTICE without --chmod=0444"},
		},
		{
			name:         "a Dockerfile that copies both files with another mode",
			dockerfile:   strings.Replace(fixture, "--chmod="+licenseFileMode, "--chmod=0644", 1),
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			want:         []string{"copies LICENSE without --chmod=0444", "copies NOTICE without --chmod=0444"},
		},
		{
			name:         "a Dockerfile whose licence COPY creates the doc directory",
			dockerfile:   strings.Replace(strings.Replace(fixture, docStage, "", 1), docCopy, "", 1),
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			want:         []string{"create " + imageDocDir + " with mode " + licenseFileMode},
		},
		{
			name:         "a Dockerfile that creates the doc directory after the licence COPY",
			dockerfile:   strings.Replace(fixture, docCopy+bothFiles, bothFiles+docCopy, 1),
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			want:         []string{"create " + imageDocDir + " with mode " + licenseFileMode},
		},
		{
			name:         "a Dockerfile whose doc stage makes the directory with another mode",
			dockerfile:   strings.Replace(fixture, "RUN mkdir -m 0755 /doc", "RUN mkdir /doc", 1),
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			want:         []string{"does not make /doc with mode 0755"},
		},
		{
			name:         "a build context that leaves both files out",
			dockerfile:   fixture,
			dockerignore: "*\n!go.mod\n!cmd/\n!internal/\n",
			want:         []string{"leaves LICENSE out", "leaves NOTICE out"},
		},
		{
			name:         "a GoReleaser docker entry that names LICENSE alone",
			dockerfile:   fixture,
			dockerignore: "*\n!LICENSE\n!NOTICE\n",
			goreleaser:   "dockers_v2:\n  - id: shift2eu\n    dockerfile: Dockerfile\n    extra_files:\n      - LICENSE\n",
			want:         []string{"names no NOTICE"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings := licenseFileFindings(t, tc.name, tc.dockerfile, tc.dockerignore, tc.goreleaser)

			for _, finding := range findings {
				if !slices.ContainsFunc(tc.want, func(want string) bool { return strings.Contains(finding, want) }) {
					t.Errorf("unexpected finding: %s", finding)
				}
			}

			for _, want := range tc.want {
				if !slices.ContainsFunc(findings, func(finding string) bool { return strings.Contains(finding, want) }) {
					t.Errorf("no finding mentions %q, got %q", want, findings)
				}
			}
		})
	}
}

// licenseFileFindings returns what name, one build's Dockerfile, .dockerignore
// and .goreleaser.yaml, leaves of issue #495's promise that the image ships
// LICENSE and NOTICE: a file the final stage does not copy from the build
// context into imageDocDir, a file it copies with a mode other than 0444, a file
// .dockerignore keeps out of that context, and a file a GoReleaser docker
// section does not put into the context it sends to the daemon, or a directory
// its --chmod=0444 COPY is left to create. An empty dockerignore or goreleaser
// skips that half of the check.
func licenseFileFindings(t *testing.T, name, dockerfile, dockerignore, goreleaser string) []string {
	t.Helper()

	var findings []string

	instructions := dockerfileInstructions(t, dockerfile)
	destinations := contextCopyDestinations(finalStage(t, instructions))

	for _, file := range shippedLicenseFiles {
		landing, copied := destinations[file.name]

		switch {
		case !copied:
			findings = append(findings, fmt.Sprintf("%s: the final stage copies no %s from the build context: want `COPY %s %s`, so the image carries the licence Apache-2.0 section 4 asks every redistribution to keep (issue #495)",
				name, file.name, file.name, file.path))
		case landing.destination != file.path:
			findings = append(findings, fmt.Sprintf("%s: the final stage copies %s to %q, want %q: the image keeps the licence and the NOTICE attribution under %s, where a reader of the image finds them (issue #495)",
				name, file.name, landing.destination, file.path, imageDocDir))
		case landing.chmod != licenseFileMode:
			findings = append(findings, fmt.Sprintf("%s: the final stage copies %s without --chmod=%s: want `COPY --chmod=%s %s %s`, so the file's mode is the same for every builder, whatever its checkout umask sets, and an image that is read-only for its user carries it read-only (issue #495)",
				name, file.name, licenseFileMode, licenseFileMode, file.name, file.path))
		}
	}

	findings = append(findings, licenseDirFindings(t, name, instructions)...)

	if dockerignore != "" {
		patterns := dockerignorePatterns(t, dockerignore)

		for _, file := range shippedLicenseFiles {
			if excluded, rule := dockerignoreExcludes(patterns, file.name); excluded {
				findings = append(findings, fmt.Sprintf("%s:%d: the pattern %q leaves %s out of the build context, so the final stage's COPY fails with \"not found\": %s must reach the daemon for the image to ship it (issue #495)",
					dockerignoreName, rule.line, rule.raw, file.name, file.name))
			}
		}
	}

	for _, section := range imageBuildSections(goreleaser) {
		for _, file := range shippedLicenseFiles {
			if !strings.Contains(section, file.name) {
				findings = append(findings, fmt.Sprintf("%s: the docker section of the release names no %s: GoReleaser sends the daemon only what the section's extra_files names, so the image it builds ships no %s whatever the Dockerfile copies; add %s beside the Dockerfile in extra_files (issue #495)",
					goreleaserName, file.name, file.name, file.name))
			}
		}
	}

	return findings
}

// licenseDirFindings returns what name, one Dockerfile, leaves of issue #495's
// other half: imageDocDir must be created with imageDocDirMode before the final
// stage copies LICENSE and NOTICE into it with --chmod=0444. BuildKit gives that
// COPY's mode to the directories the COPY creates as well, and a directory
// without a search bit cannot be entered, so an image whose licence directory
// the 0444 COPY creates carries two files its nonroot user cannot open.
//
// The directory counts as created when the stage copies it from another stage
// that runs `RUN mkdir -m 0755 <dir>`: the mode is explicit, so the directory
// does not depend on the umask of whoever builds the image, and it is the mode
// the COPY of the directory keeps. A Dockerfile with no such COPY before the
// licence COPY, or whose stage makes the directory another way, is reported.
func licenseDirFindings(t *testing.T, name string, instructions []instruction) []string {
	t.Helper()

	stageCopies := copies(finalStage(t, instructions))

	licence := -1

	for i, c := range stageCopies {
		if c.from == "" && c.chmod == licenseFileMode && c.copiesFilesInto(imageDocDir) {
			licence = i

			break
		}
	}

	if licence < 0 {
		return nil
	}

	for _, c := range stageCopies[:licence] {
		source, ok := c.directoryAt(imageDocDir)
		if !ok {
			continue
		}

		if from, declared := stageNamed(instructions, c.from); declared && makesDirectory(from, source, imageDocDirMode) {
			return nil
		}

		return []string{fmt.Sprintf("%s:%d: the final stage copies %s from the stage %q, which does not make %s with mode %s: want `RUN mkdir -m %s %s` in that stage, so the directory the final stage copies keeps the search bit the image's nonroot user needs to enter it (issue #495)",
			name, c.line, imageDocDir, c.from, source, imageDocDirMode, imageDocDirMode, source)}
	}

	return []string{fmt.Sprintf("%s:%d: the final stage lets its `COPY --chmod=%s ... %s/` create %s with mode %s: BuildKit gives a COPY's --chmod to the directories that COPY creates as well, and a directory with no search bit cannot be entered, so the image's nonroot user reads neither LICENSE nor NOTICE; copy the directory in first, from a stage that runs `RUN mkdir -m %s <dir>` (issue #495)",
		name, stageCopies[licence].line, licenseFileMode, imageDocDir, imageDocDir, licenseFileMode, imageDocDirMode)}
}

// copy is one COPY instruction of a stage, parsed as Docker reads it.
type copy struct {
	// line is the line of the Dockerfile the instruction starts at.
	line int
	// from is the stage a --from names, "" for a copy of the build context.
	from string
	// chmod is the --chmod value, "" when the instruction carries none.
	chmod string
	// paths are the sources and the destination, in order.
	paths []string
}

// copies returns the COPY instructions of a stage, in file order. A --chmod
// written as its own argument is read too, since Docker accepts that form.
func copies(stage []instruction) []copy {
	var parsed []copy

	for _, in := range stage {
		if in.keyword() != "COPY" {
			continue
		}

		parsedCopy := copy{line: in.line}

		fields := strings.Fields(in.arguments())

		for i := 0; i < len(fields); i++ {
			field := fields[i]

			switch {
			case strings.HasPrefix(field, "--from="):
				parsedCopy.from = strings.TrimPrefix(field, "--from=")
			case strings.HasPrefix(field, "--chmod="):
				parsedCopy.chmod = strings.TrimPrefix(field, "--chmod=")
			case field == "--chmod" && i+1 < len(fields):
				i++
				parsedCopy.chmod = fields[i]
			case strings.HasPrefix(field, "--"):
			default:
				parsedCopy.paths = append(parsedCopy.paths, field)
			}
		}

		parsed = append(parsed, parsedCopy)
	}

	return parsed
}

// destination returns where the copy puts source: a destination written as a
// directory, which Docker joins to the source's base name, or the destination
// itself for a copy of one file, and "" when the paths do not say.
func (c copy) destination(source string) string {
	if len(c.paths) < 2 {
		return ""
	}

	destination := c.paths[len(c.paths)-1]

	switch {
	case strings.HasSuffix(destination, "/"):
		return path.Join(destination, path.Base(source))
	case len(c.paths) == 2:
		return destination
	default:
		return ""
	}
}

// copiesFilesInto reports whether the copy puts a file of the build context
// into dir: a destination written as dir itself or as a path inside it.
func (c copy) copiesFilesInto(dir string) bool {
	if len(c.paths) < 2 {
		return false
	}

	for _, source := range c.paths[:len(c.paths)-1] {
		if destination := c.destination(source); destination != "" && path.Dir(destination) == path.Clean(dir) {
			return true
		}
	}

	return false
}

// directoryAt reports the directory a copy of another stage puts at dir, and
// whether this copy is one: one source and one destination that name the same
// directory, written with or without a trailing slash.
func (c copy) directoryAt(dir string) (string, bool) {
	if c.from == "" || len(c.paths) != 2 {
		return "", false
	}

	if path.Clean(c.paths[1]) != path.Clean(dir) {
		return "", false
	}

	return path.Clean(c.paths[0]), true
}

// stageNamed returns the whole of the stage a COPY's --from names, its FROM
// included, and false when the Dockerfile declares no stage of that name, as for
// a copy from another image. It reports nothing it cannot find, unlike stageFor:
// a copy the check cannot resolve is not a directory the check can vouch for.
func stageNamed(instructions []instruction, name string) ([]instruction, bool) {
	for i, in := range instructions {
		if in.keyword() != "FROM" {
			continue
		}

		fields := fromFields(in)
		if len(fields) >= 3 && strings.EqualFold(fields[1], "AS") && strings.EqualFold(fields[2], name) {
			return stageLines(instructions, i), true
		}
	}

	return nil, false
}

// makesDirectory reports whether a stage runs the mkdir that makes dir with
// mode, such as `RUN mkdir -m 0755 /doc`. A mkdir that leaves the mode to the
// builder's umask does not count: the directory's mode must not depend on who
// builds the image.
func makesDirectory(stage []instruction, dir, mode string) bool {
	for _, in := range stage {
		if in.keyword() == "RUN" && mkdirWithMode(in.arguments(), dir, mode) {
			return true
		}
	}

	return false
}

// mkdirWithMode reports whether a RUN command runs mkdir to make dir with mode.
// The mode is read from -m or --mode, and mkdir's other flags, such as -p and
// --parents, are skipped, so a command written in any of those forms counts.
func mkdirWithMode(command, dir, mode string) bool {
	fields := strings.Fields(command)

	for i, field := range fields {
		if path.Base(unquote(field)) != "mkdir" {
			continue
		}

		var (
			gotMode string
			targets []string
		)

		for j := i + 1; j < len(fields); j++ {
			field := unquote(fields[j])

			switch {
			case field == "-m" || field == "--mode":
				if j+1 < len(fields) {
					j++
					gotMode = unquote(fields[j])
				}
			case strings.HasPrefix(field, "--mode="):
				gotMode = strings.TrimPrefix(field, "--mode=")
			case strings.HasPrefix(field, "-"):
			default:
				targets = append(targets, field)
			}
		}

		for _, target := range targets {
			if gotMode == mode && path.Clean(target) == path.Clean(dir) {
				return true
			}
		}
	}

	return false
}

// contextCopy is where one stage copies a file of the build context, and the
// mode it gives it.
type contextCopy struct {
	// destination is the path the file lands at in the image.
	destination string
	// chmod is the COPY's --chmod value, "" when the instruction carries none.
	chmod string
}

// contextCopyDestinations returns where one stage copies each file of the build
// context and with which --chmod: the base name of every source, and the path it
// lands at, a destination written as a directory joined to the source's base
// name. A COPY from another stage is not a copy of the context and is skipped,
// and a stage that copies one file twice keeps the last destination, as Docker
// does.
func contextCopyDestinations(stage []instruction) map[string]contextCopy {
	destinations := make(map[string]contextCopy)

	for _, c := range copies(stage) {
		if c.from != "" || len(c.paths) < 2 {
			continue
		}

		for _, source := range c.paths[:len(c.paths)-1] {
			if destination := c.destination(source); destination != "" {
				destinations[path.Base(source)] = contextCopy{destination: destination, chmod: c.chmod}
			}
		}
	}

	return destinations
}

// imageBuildSections returns the body of every top-level docker, dockers or
// dockers_v2 section of .goreleaser.yaml, the sections that build the image with
// GoReleaser, comment and blank lines dropped. A release that builds the image
// with docker build or make image has none, and returns nothing. The
// dockers_v2 half stays although GoReleaser builds no image today — the release
// builds it with docker/build-push-action and `context: .` — because it guards a
// future GoReleaser docker section, which would otherwise send the daemon a
// context holding neither file.
func imageBuildSections(goreleaser string) []string {
	imageKeys := map[string]bool{"docker": true, "dockers": true, "dockers_v2": true}

	var (
		sections []string
		current  []string
		inImage  bool
	)

	closeSection := func() {
		if inImage {
			sections = append(sections, strings.Join(current, "\n"))
		}

		current, inImage = nil, false
	}

	for _, raw := range strings.Split(goreleaser, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// A top level key is one that starts in the first column.
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			closeSection()

			key, _, ok := strings.Cut(trimmed, ":")
			inImage = ok && imageKeys[key]

			continue
		}

		if inImage {
			current = append(current, trimmed)
		}
	}

	closeSection()

	return sections
}

// TestTheDockerfileHasNoServerStage pins what issue #393 leaves of PRD #103's
// milestone 4: the Dockerfile builds the CLI's image alone. A stage named
// server, the HTTP API's binary, its --listen CMD and its published port are
// gone, so `docker buildx build --target server` and `make server-image` build
// nothing rather than an image nothing publishes, and no image can start
// serving the API the release no longer ships.
//
// The check runs over the repository's Dockerfile, which keeps nothing of the
// HTTP API, and over fixtures that do: Docker reads a stage name in any case,
// so a FROM that declares the retired stage as Server declares the same stage.
func TestTheDockerfileHasNoServerStage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		// want holds parts of the findings the check must return; none means
		// the Dockerfile keeps nothing of the HTTP API.
		want []string
	}{
		{
			name:       "the repository's Dockerfile",
			dockerfile: readRepoFile(t, dockerfileName),
		},
		{
			name:       "a stage declared as Server",
			dockerfile: "FROM scratch AS Server\n",
			want:       []string{"the stage server is declared again"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings := retiredServerFindings(t, tc.name, tc.dockerfile)

			for _, finding := range findings {
				if !slices.ContainsFunc(tc.want, func(want string) bool { return strings.Contains(finding, want) }) {
					t.Errorf("unexpected finding: %s", finding)
				}
			}

			for _, want := range tc.want {
				if !slices.ContainsFunc(findings, func(finding string) bool { return strings.Contains(finding, want) }) {
					t.Errorf("no finding mentions %q, got %q", want, findings)
				}
			}
		})
	}
}

// retiredServerFindings returns what name, a Dockerfile, still keeps of the
// HTTP API that issue #393 retired: its binary and main package, a stage named
// server written in any case (Docker reads stage names case-insensitively),
// and an EXPOSE of the port its image served.
func retiredServerFindings(t *testing.T, name, dockerfile string) []string {
	t.Helper()

	var findings []string

	for _, retired := range []string{serverBinaryPath, serverMainPackage, serverBuildID} {
		if strings.Contains(dockerfile, retired) {
			findings = append(findings, fmt.Sprintf("%s names %q, want no mention of the HTTP API's binary: the Dockerfile builds the CLI's image alone (issue #393)",
				name, retired))
		}
	}

	instructions := dockerfileInstructions(t, dockerfile)

	// A stage is named by the `AS <name>` of its FROM, which is the last field.
	for _, in := range instructions {
		if in.keyword() != "FROM" {
			continue
		}

		fields := fromFields(in)
		if len(fields) == 0 || !strings.EqualFold(fields[len(fields)-1], serverStageName) {
			continue
		}

		findings = append(findings, fmt.Sprintf("%s:%d: the stage %s is declared again: want no stage named %s, so no --target server builds an image of the HTTP API (issue #393)",
			name, in.line, serverStageName, serverStageName))
	}

	for _, in := range instructions {
		if in.keyword() != "EXPOSE" {
			continue
		}

		findings = append(findings, fmt.Sprintf("%s:%d: the Dockerfile EXPOSEs %s: want no EXPOSE instruction, because the CLI image's binary serves nothing over the network and the port belonged to the HTTP API's image (issue #393)",
			name, in.line, in.arguments()))
	}

	return findings
}

// assertMinimalNonRootStage holds one stage that ships an image to what both
// images promise: a base image with nothing to install, patch or run but the
// binary and the CA certificates; a non-root user; the binary as the exec-form
// entrypoint; and the OCI labels that say where the image comes from and under
// which license. name names the stage in failure messages, and entrypointPath is
// the binary its ENTRYPOINT must run.
func assertMinimalNonRootStage(t *testing.T, stage []instruction, name, entrypointPath string) {
	t.Helper()

	base := fromImages(stage)[0]
	if !strings.EqualFold(base.image, "scratch") && !strings.HasPrefix(base.image, "gcr.io/distroless/static") {
		t.Errorf("%s:%d: %s starts FROM %q: want scratch or gcr.io/distroless/static, which hold no shell, package manager or Go toolchain",
			dockerfileName, base.line, name, base.image)
	}

	if isDebugBaseImage(base.image) {
		t.Errorf("%s:%d: %s starts FROM %q, the debug variant of a distroless image: :debug and :debug-nonroot ship a busybox shell and a package manager's leftovers, contradicting the image's promise of no shell; want the same image under its non-debug tag, such as :nonroot",
			dockerfileName, base.line, name, base.image)
	}

	// The builder cross-compiles for $BUILDPLATFORM, so CI needs no QEMU
	// emulation. A RUN in either stage that ships an image would need it, and a
	// shell distroless does not have.
	for _, in := range stage {
		if in.keyword() == "RUN" {
			t.Errorf("%s:%d: %s has a RUN instruction, %q: want none, because the builder cross-compiles on $BUILDPLATFORM, so CI needs no QEMU emulation, and a RUN in this stage would need it as well as a shell, which distroless does not have; do the work in the builder and COPY its result",
				dockerfileName, in.line, name, in.text)
		}
	}

	user, ok := lastInstruction(stage, "USER")
	switch {
	case !ok:
		t.Errorf("%s: %s has no USER instruction: want a non-root user such as USER 65532:65532, so the process does not run as root",
			dockerfileName, name)
	default:
		// Docker reads the user as <name>[:<group>]; a name that is root, or
		// the root uid, runs the process as root whatever the group is.
		userName, _, _ := strings.Cut(user.arguments(), ":")
		if userName == "" || userName == "root" || userName == "0" {
			t.Errorf("%s:%d: %s runs as USER %q: want a non-root user such as nonroot or 65532:65532",
				dockerfileName, user.line, name, user.arguments())
		}
	}

	entrypoint, ok := lastInstruction(stage, "ENTRYPOINT")
	switch {
	case !ok:
		t.Errorf("%s: %s has no ENTRYPOINT: want ENTRYPOINT [\"%s\"] in exec form, so the binary runs without a shell",
			dockerfileName, name, entrypointPath)
	default:
		var argv []string
		if err := json.Unmarshal([]byte(entrypoint.arguments()), &argv); err != nil {
			t.Errorf("%s:%d: ENTRYPOINT %s is not in exec (JSON array) form, so Docker wraps it in /bin/sh, which the image does not hold: %v",
				dockerfileName, entrypoint.line, entrypoint.arguments(), err)
		} else if diff := cmp.Diff([]string{entrypointPath}, argv); diff != "" {
			t.Errorf("%s:%d: ENTRYPOINT mismatch (-want +got):\n%s",
				dockerfileName, entrypoint.line, diff)
		}
	}

	labels := finalStageLabels(stage)
	for _, key := range []string{
		"org.opencontainers.image.source",
		"org.opencontainers.image.version",
		"org.opencontainers.image.revision",
		"org.opencontainers.image.licenses",
	} {
		value, ok := labels[key]
		switch {
		case !ok:
			t.Errorf("%s: %s has no LABEL %s: want the four OCI labels, source, version, revision and licenses",
				dockerfileName, name, key)
		case value == "":
			t.Errorf("%s: LABEL %s is empty: want a value, set from a build arg where it changes per release",
				dockerfileName, key)
		}
	}
}

// TestDebugBaseImageClassification exercises the check that keeps the final
// stage off the debug variant of a distroless image. Distroless publishes
// :debug and :debug-nonroot builds of every image, which hold a busybox shell
// (and a package manager) for troubleshooting: they look like the right image
// and contradict the reason the final stage is distroless at all. The table is
// black-box: it goes through example image references only.
func TestDebugBaseImageClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image string
		want  bool
	}{
		{
			name:  "distroless nonroot tag",
			image: "gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329",
		},
		{
			name:  "distroless untagged with digest",
			image: "gcr.io/distroless/static-debian13@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329",
		},
		{
			name:  "distroless latest tag",
			image: "gcr.io/distroless/static-debian13:latest",
		},
		{
			name:  "scratch",
			image: "scratch",
		},
		{
			name:  "registry with a port and no tag",
			image: "localhost:5000/distroless/static-debian13",
		},
		{
			name:  "distroless debug tag",
			image: "gcr.io/distroless/static-debian13:debug@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329",
			want:  true,
		},
		{
			name:  "distroless debug-nonroot tag",
			image: "gcr.io/distroless/static-debian13:debug-nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329",
			want:  true,
		},
		{
			name:  "registry with a port and a debug tag",
			image: "localhost:5000/distroless/static-debian13:debug",
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isDebugBaseImage(tc.image); got != tc.want {
				t.Errorf("isDebugBaseImage(%q) = %t, want %t", tc.image, got, tc.want)
			}
		})
	}
}

// TestDockerignoreKeepsEmbeddedFiles pins that .dockerignore keeps every file a
// //go:embed directive embeds in the build context. The image builds from cmd/
// and internal/ only, and go build fails outright when an embed pattern matches
// no file, so an over-loose ignore pattern breaks the image build rather than
// shrinking it. Today the embedded files are the target catalog snapshots,
// internal/adapter/target/{ovhcloud,scaleway}/catalog.json, which are not test
// data and must stay in.
func TestDockerignoreKeepsEmbeddedFiles(t *testing.T) {
	t.Parallel()

	patterns := dockerignorePatterns(t, readRepoFile(t, dockerignoreName))
	embedded := embeddedFiles(t)

	if len(embedded) == 0 {
		t.Fatalf("no //go:embed directive in a non-test .go file under cmd/ or internal/ names a file, so this test would pass whatever %s holds: the target catalog snapshots are embedded and must be found",
			dockerignoreName)
	}

	for _, file := range embedded {
		excluded, rule := dockerignoreExcludes(patterns, file.path)
		if !excluded {
			continue
		}

		t.Errorf("%s:%d: the pattern %q excludes the file %q, which %s:%d embeds with //go:embed: the build context leaves it out, so `go build` fails in the image with \"pattern ...: no matching files found\"; the embedded catalog snapshots are not test data and must stay in",
			dockerignoreName, rule.line, rule.raw, file.path, file.source, file.line)
	}
}

// TestDockerignoreKeepsDataOutOfContext pins that .dockerignore keeps the git
// data, the worktrees, the build output, the credentials and the local
// inventories and reports out of the build context, so no layer can hold real
// infrastructure data (CLAUDE.md rule 2). The sample paths are evaluated with
// the same matcher TestDockerignoreKeepsEmbeddedFiles uses, last matching
// pattern winning, so the test pins where a path ends up rather than which lines
// the file happens to hold: the context is an allow list (`*`, then
// `!internal/`), and a pattern anchored to the context root, such as `.env`,
// never matches the .env a package keeps under internal/. The literal entries
// are pinned as well, because they name the data in the file itself.
func TestDockerignoreKeepsDataOutOfContext(t *testing.T) {
	t.Parallel()

	dockerignore := readRepoFile(t, dockerignoreName)
	entries := dockerignoreEntries(t, dockerignore)
	patterns := dockerignorePatterns(t, dockerignore)

	for _, want := range []string{".git", ".worktrees", "bin", "coverage.out"} {
		if !slices.Contains(entries, want) {
			t.Errorf("%s: no entry %q: the build context carries it, and no layer of the image may hold git data, a worktree, a local build or a coverage report",
				dockerignoreName, want)
		}
	}

	for _, want := range []string{"inventory", "report"} {
		mention := func(entry string) bool {
			return strings.Contains(strings.ToLower(entry), want)
		}
		if !slices.ContainsFunc(entries, mention) {
			t.Errorf("%s: no entry mentions %q: a local inventory or report would be sent to the daemon and could land in a layer, which CLAUDE.md rule 2 forbids",
				dockerignoreName, want)
		}
	}

	tests := []struct {
		name string
		path string
		why  string
	}{
		{name: "env file at the context root", path: ".env", why: "holds credentials"},
		{name: "env file in a package", path: "internal/x/.env", why: "holds credentials"},
		{name: "env variant in a package", path: "internal/x/.env.local", why: "holds credentials"},
		{name: "envrc in a package", path: "cmd/shift2eu/.envrc", why: "holds credentials"},
		{name: "private key in a package", path: "internal/x/key.pem", why: "is a private key"},
		{name: "key file in a package", path: "internal/x/id.key", why: "is a private key"},
		{name: "aws credentials", path: "internal/x/.aws/credentials", why: "holds cloud credentials"},
		{name: "azure configuration", path: "internal/x/.azure/config", why: "holds cloud credentials"},
		{name: "gcloud credentials", path: "internal/x/.config/gcloud/creds.json", why: "holds cloud credentials"},
		{name: "inventory in a package", path: "internal/x/inventory.json", why: "describes real infrastructure"},
		{name: "report in a package", path: "internal/x/report.md", why: "describes real infrastructure"},
		{name: "git data", path: ".git/config", why: "holds the repository's history and remotes"},
		{name: "local build", path: "bin/shift2eu", why: "is a locally built binary"},
		{name: "coverage report", path: "coverage.out", why: "is a local build artefact"},
		{name: "worktree module", path: ".worktrees/x/go.mod", why: "belongs to a worktree, not the build"},
		{name: "test fixture", path: "internal/x/testdata/inventory.json", why: "is test data the binary does not build"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			excluded, rule := dockerignoreExcludes(patterns, tc.path)
			if excluded {
				return
			}

			matched := "no pattern matches it at all"
			if rule.pattern != "" {
				matched = fmt.Sprintf("the last pattern matching it is %q, on line %d", rule.raw, rule.line)
			}

			t.Errorf("%s: the path %q would be sent to the daemon (%s) because it is in the build context: %s. Patterns are matched against the whole path relative to the context root, so an entry without a **/ prefix only covers the root, and `!internal/` re-includes the directory after it; want a pattern such as `**/%s` that excludes it at any depth",
				dockerignoreName, tc.path, tc.why, matched, tc.path)
		})
	}
}

// TestEmbedPatternTokens pins how the patterns of a //go:embed directive are
// split: whitespace separates them, and a double-quoted or back-quoted literal is
// one pattern however many spaces it holds, unquoted the way the go command
// unquotes it. A pattern that cannot be read fails rather than being skipped, so
// the test never silently stops looking at an embedded file.
func TestEmbedPatternTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		patterns string
		want     []string
		wantErr  bool
	}{
		{
			name:     "bare patterns",
			patterns: "catalog.json other.json",
			want:     []string{"catalog.json", "other.json"},
		},
		{
			name:     "tabs separate patterns too",
			patterns: "catalog.json\tother.json",
			want:     []string{"catalog.json", "other.json"},
		},
		{
			name:     "leading and trailing whitespace",
			patterns: "  catalog.json  ",
			want:     []string{"catalog.json"},
		},
		{
			name:     "double-quoted pattern with a space",
			patterns: `catalog.json "catalog 2.json"`,
			want:     []string{"catalog.json", "catalog 2.json"},
		},
		{
			name:     "back-quoted pattern with a space",
			patterns: "`catalog 3.json` other.json",
			want:     []string{"catalog 3.json", "other.json"},
		},
		{
			name:     "escape in a double-quoted pattern",
			patterns: `"say \"hi\".json"`,
			want:     []string{`say "hi".json`},
		},
		{
			name:     "escaped tab in a double-quoted pattern",
			patterns: `"catalog\t2.json"`,
			want:     []string{"catalog\t2.json"},
		},
		{
			name:     "glob pattern",
			patterns: "snapshots/*.json",
			want:     []string{"snapshots/*.json"},
		},
		{
			name:     "all prefix is kept on the token",
			patterns: "all:web",
			want:     []string{"all:web"},
		},
		{name: "no pattern", patterns: "", want: nil},
		{name: "unterminated quote", patterns: `"catalog.json`, wantErr: true},
		{name: "invalid escape", patterns: `"catalog\q.json"`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := embedPatternTokens(tc.patterns)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("embedPatternTokens(%q) = %q, want an error", tc.patterns, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("embedPatternTokens(%q): %v", tc.patterns, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("embedPatternTokens(%q) mismatch (-want +got):\n%s", tc.patterns, diff)
			}
		})
	}
}

// baseImage is one FROM instruction: the image it starts the stage from and the
// stage name it declares, if any.
type baseImage struct {
	line  int
	image string
	stage string
}

// instruction is one Dockerfile instruction, its line continuations joined and
// its whitespace collapsed, with the line it starts on for failure messages.
type instruction struct {
	line int
	text string
}

// keyword returns the instruction's upper-case keyword, such as FROM or LABEL.
func (in instruction) keyword() string {
	keyword, _, _ := strings.Cut(in.text, " ")
	return strings.ToUpper(keyword)
}

// arguments returns everything after the keyword.
func (in instruction) arguments() string {
	_, args, _ := strings.Cut(in.text, " ")
	return args
}

// goreleaserBuild is one entry of .goreleaser.yaml's builds list: the id it is
// named by, the main package and binary it builds, and the lists the image's
// build must match. line is the line the entry starts on, for failure messages.
type goreleaserBuild struct {
	line    int
	id      string
	main    string
	binary  string
	env     []string
	flags   []string
	ldflags []string
	goos    []string
	goarch  []string
}

// list returns the list a key of a builds entry names, and nil for a key that
// names no list the test reads.
func (b *goreleaserBuild) list(key string) *[]string {
	switch key {
	case "env":
		return &b.env
	case "flags":
		return &b.flags
	case "ldflags":
		return &b.ldflags
	case "goos":
		return &b.goos
	case "goarch":
		return &b.goarch
	default:
		return nil
	}
}

// setScalar sets the string field a key of a builds entry names, and reports
// whether it names one.
func (b *goreleaserBuild) setScalar(key, value string) bool {
	switch key {
	case "id":
		b.id = value
	case "main":
		b.main = value
	case "binary":
		b.binary = value
	default:
		return false
	}

	return true
}

// buildSetting is one setting the image's build must carry: the value, the
// .goreleaser.yaml list it belongs to, and the items of that list the test read
// from the document, so a setting the document itself dropped is reported as
// the document's gap rather than the image's.
type buildSetting struct {
	setting string
	list    string
	in      []string
}

// repoRoot returns the module root: the first directory above the directory the
// test runs in that holds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get the working directory: %v", err)
	}

	for start := dir; ; {
		_, err := os.Stat(filepath.Join(dir, goModName))
		switch {
		case err == nil:
			return dir
		case !errors.Is(err, fs.ErrNotExist):
			t.Fatalf("stat %s: %v", filepath.Join(dir, goModName), err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no %s above %s, so the repository root is not found", goModName, start)
		}

		dir = parent
	}
}

// readRepoFile reads the file name under the repository root, and fails the
// test naming it when it is missing or unreadable: every test here pins one of
// the build files.
func readRepoFile(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("%s: read from the repository root: %v; the container build needs it at the root", name, err)
	}

	return string(data)
}

// goDirectiveVersion returns the version of go.mod's go directive, "1.27.4"
// for "go 1.27.4".
func goDirectiveVersion(t *testing.T, goMod string) string {
	t.Helper()

	for _, line := range strings.Split(goMod, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "go" {
			return fields[1]
		}
	}

	t.Fatalf("%s: no go directive: want a line such as \"go 1.27.4\", which the builder image's tag must match", goModName)

	return ""
}

// dockerfileInstructions parses the Dockerfile into its instructions. Blank
// lines and comments are dropped, and a line ending in a backslash is joined to
// the next, so a LABEL continued over several lines reads as one instruction.
func dockerfileInstructions(t *testing.T, dockerfile string) []instruction {
	t.Helper()

	var (
		instructions []instruction
		current      strings.Builder
		start        int
	)

	for i, raw := range strings.Split(dockerfile, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))

		// Docker removes comment lines before it joins continuations, so a
		// comment inside a continued instruction is dropped too.
		if strings.HasPrefix(line, "#") {
			continue
		}

		if line == "" && current.Len() == 0 {
			continue
		}

		if current.Len() == 0 {
			start = i + 1
		}

		continued := strings.HasSuffix(line, `\`)
		current.WriteString(" ")
		current.WriteString(strings.TrimSuffix(line, `\`))

		if continued {
			continue
		}

		instructions = append(instructions, instruction{
			line: start,
			text: strings.Join(strings.Fields(current.String()), " "),
		})
		current.Reset()
	}

	if current.Len() != 0 {
		t.Errorf("%s:%d: the instruction ends in a line continuation with no next line", dockerfileName, start)
	}

	if len(instructions) == 0 {
		t.Fatalf("%s: no instruction, so there is nothing to build", dockerfileName)
	}

	return instructions
}

// fromImages returns the FROM instruction of every stage, in file order. The
// --platform flag a FROM may carry is skipped.
func fromImages(instructions []instruction) []baseImage {
	var images []baseImage

	for _, in := range instructions {
		if in.keyword() != "FROM" {
			continue
		}

		fields := fromFields(in)
		if len(fields) == 0 {
			continue
		}

		image := baseImage{line: in.line, image: fields[0]}
		if len(fields) >= 3 && strings.EqualFold(fields[1], "AS") {
			image.stage = strings.ToLower(fields[2])
		}

		images = append(images, image)
	}

	return images
}

// finalStage returns the instructions of the last stage, the ones after the
// last FROM, and fails when the Dockerfile has no FROM at all.
func finalStage(t *testing.T, instructions []instruction) []instruction {
	t.Helper()

	last := -1
	for i, in := range instructions {
		if in.keyword() == "FROM" {
			last = i
		}
	}

	if last < 0 {
		t.Fatalf("%s: no FROM instruction, so there is no final stage", dockerfileName)
	}

	return instructions[last:]
}

// stageCommands returns the CMD instructions of a stage, in file order. Docker
// applies the last one, so more than one is reported rather than picked from.
func stageCommands(stage []instruction) []instruction {
	var cmds []instruction

	for _, in := range stage {
		if in.keyword() == "CMD" {
			cmds = append(cmds, in)
		}
	}

	return cmds
}

// lastInstruction returns the last instruction of the given keyword, which is
// the one Docker applies when the same instruction appears twice.
func lastInstruction(instructions []instruction, keyword string) (instruction, bool) {
	found := false
	var last instruction

	for _, in := range instructions {
		if in.keyword() == keyword {
			last, found = in, true
		}
	}

	return last, found
}

// runsGoBuild reports whether a RUN instruction's command runs `go build`. The
// fields are read in order, so `go mod download` and a mention of the string in
// a flag value or a file name are not the build itself.
func runsGoBuild(command string) bool {
	fields := strings.Fields(command)

	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "go" && fields[i+1] == "build" {
			return true
		}
	}

	return false
}

// runsGo reports whether a RUN instruction's command runs a go command, such as
// `go mod download` or `go build`, however the command spells it: bare, by its
// path (`/usr/local/go/bin/go`) or inside a shell (`sh -c "go mod download"`).
// Each field is unquoted and reduced to its last path element, so only the
// command `go` counts: a word that merely contains go (`gofmt`, `golang`,
// `cargo`) and a mention in a flag value or a path, such as the cache mount's
// target=/go/pkg/mod, is not the command itself.
func runsGo(command string) bool {
	for _, field := range strings.Fields(command) {
		if path.Base(strings.Trim(field, `"'`)) == "go" {
			return true
		}
	}

	return false
}

// firstGoRun returns the first RUN instruction of a stage whose command runs a go
// command, the step the toolchain pin must come before, and reports whether the
// stage holds one.
func firstGoRun(stage []instruction) (instruction, bool) {
	for _, in := range stage {
		if in.keyword() == "RUN" && runsGo(in.arguments()) {
			return in, true
		}
	}

	return instruction{}, false
}

// envAssignments returns the variables an ENV instruction sets, read in both
// forms Docker accepts: `ENV NAME=value`, one or more variables space separated,
// and the legacy `ENV NAME value`, whose value is the rest of the line. A value
// is unquoted, because Docker strips a matching pair of quotes, so
// `ENV GOTOOLCHAIN="local"` sets the same value as `ENV GOTOOLCHAIN=local`.
func envAssignments(in instruction) map[string]string {
	fields := strings.Fields(in.arguments())
	if len(fields) == 0 {
		return nil
	}

	if !strings.Contains(fields[0], "=") {
		return map[string]string{fields[0]: unquote(strings.Join(fields[1:], " "))}
	}

	names := make(map[string]string, len(fields))

	for _, field := range fields {
		if name, value, found := strings.Cut(field, "="); found {
			names[name] = unquote(value)
		}
	}

	return names
}

// goBuildRunFor returns the one RUN instruction that runs `go build` for pkg:
// the instruction that builds the binary one image ships, and the only place a
// build setting can take effect. It fails the test when there is no such
// instruction, so the build settings cannot be checked, or more than one, so it
// is not clear which build the image ships.
func goBuildRunFor(t *testing.T, instructions []instruction, pkg string) instruction {
	t.Helper()

	var builds []instruction
	for _, in := range instructions {
		if in.keyword() == "RUN" && runsGoBuild(in.arguments()) && goBuildPackage(t, in) == pkg {
			builds = append(builds, in)
		}
	}

	switch len(builds) {
	case 0:
		t.Fatalf("%s: no RUN instruction runs `go build ... %s`, so nothing builds the binary the image ships from that package and its settings cannot be matched with %s; want one RUN such as `CGO_ENABLED=0 GOWORK=off GOFLAGS=-mod=readonly GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags ... -o /out/%s %s`",
			dockerfileName, pkg, goreleaserName, path.Base(pkg), pkg)
	case 1:
		return builds[0]
	}

	lines := make([]string, 0, len(builds))
	for _, build := range builds {
		lines = append(lines, strconv.Itoa(build.line))
	}

	t.Fatalf("%s: %d RUN instructions run `go build` of %s (lines %s): want exactly one, the instruction that builds the binary the image ships, so this test checks one build and not whichever came first",
		dockerfileName, len(builds), pkg, strings.Join(lines, ", "))

	return instruction{}
}

// goBuildPackage returns the package the `go build` of a RUN instruction
// builds: the last field after it, once the environment prefix and any --mount
// flag are read past.
func goBuildPackage(t *testing.T, in instruction) string {
	t.Helper()

	_, args := commandEnv(t, in.arguments())

	for i := 0; i+1 < len(args); i++ {
		if args[i] != "go" || args[i+1] != "build" {
			continue
		}

		if rest := args[i+2:]; len(rest) > 0 {
			return rest[len(rest)-1]
		}

		return ""
	}

	return ""
}

// buildCommand is the command of the one RUN instruction that builds the
// image's binary, read the way a shell reads it: the environment assignments
// in front of it, the flags the go build itself carries, the value of its
// -ldflags flag, the path its -o flag writes the binary to and the package it
// builds.
type buildCommand struct {
	env     []string
	flags   []string
	ldflags string
	output  string
	pkg     string
}

// parseBuildCommand reads the command of the RUN instruction that runs
// `go build` and holds it to the release build of pkg: the environment prefix
// must be followed directly by `go build`, and the go build may carry only
// -trimpath, -ldflags with the internal/version stamps and -o with the binary's
// path, then pkg.
//
// It fails the test rather than reporting a mismatch: a command line that is
// not shaped like the release build is not the thing the caller set out to
// compare with .goreleaser.yaml, so the caller cannot say anything useful about
// the settings it would read from it.
func parseBuildCommand(t *testing.T, build instruction, pkg string) buildCommand {
	t.Helper()

	env, args := commandEnv(t, build.arguments())

	// The assignments apply to the one command they prefix. In
	// `CGO_ENABLED=0 GOWORK=off GOFLAGS=-mod=readonly true && go build ...`,
	// or after a ; or a |, they apply to true and the build runs without
	// them, which is what this test must not accept however the rest of the
	// line reads.
	if len(args) < 2 || args[0] != "go" || args[1] != "build" {
		t.Fatalf("%s:%d: the environment prefix %q is followed by %q, not by `go build`: an assignment applies to the one command it prefixes, so a shell operator or another command between them leaves the build without the release env; want `%s go build ...`",
			dockerfileName, build.line, strings.Join(env, " "), strings.Join(args, " "), strings.Join(env, " "))
	}

	rest := args[2:]
	if len(rest) == 0 {
		t.Fatalf("%s:%d: the command runs `go build` with no flag and no package: want -trimpath, -ldflags <value> and -o <path>, then %s",
			dockerfileName, build.line, pkg)
	}

	command := buildCommand{env: env, pkg: rest[len(rest)-1]}
	seen := make(map[string]struct{}, 3)

	// Only the three flags .goreleaser.yaml gives the build, so a flag the
	// release does not pass cannot override the release env: -mod=mod and
	// -mod=vendor override GOFLAGS, -tags and -gcflags change what is
	// compiled, and -buildvcs writes information the release binary does not
	// carry. releasetarget.Read refuses every one of them in the release's
	// flags list.
	for i := 0; i < len(rest)-1; i++ {
		field := rest[i]

		name, value, takesValue := field, "", false

		switch {
		case field == "-trimpath":
		case field == "-ldflags", field == "-o":
			takesValue = true
		case strings.HasPrefix(field, "-ldflags="):
			name, value = "-ldflags", strings.TrimPrefix(field, "-ldflags=")
		default:
			t.Fatalf("%s:%d: the command passes %q to `go build`: want only -trimpath, -ldflags <value> and -o <path>, then %s. Any other flag, such as -mod=mod or -tags, overrides the release env or changes what is compiled",
				dockerfileName, build.line, field, pkg)
		}

		if _, twice := seen[name]; twice {
			t.Fatalf("%s:%d: the command passes %s to `go build` twice: want it at most once, so a later value cannot quietly override the one checked here",
				dockerfileName, build.line, name)
		}

		seen[name] = struct{}{}

		if takesValue {
			if i+1 >= len(rest)-1 {
				t.Fatalf("%s:%d: the %s flag has no value: want `%s <value>`, and %s as the last field, the package the build compiles",
					dockerfileName, build.line, name, name, pkg)
			}

			i++
			value = rest[i]
		}

		switch name {
		case "-ldflags":
			command.ldflags = value
		case "-o":
			command.output = value
		}

		command.flags = append(command.flags, name)
	}

	return command
}

// commandEnv returns the environment assignments at the front of a RUN
// instruction's command and the fields after them: the NAME=value fields after
// the leading --mount (or other --) flags and an optional env, up to the
// command itself. Only these assignments apply to the command; one written on
// another instruction, in a comment or inside a flag value does not.
//
// A value must be written unquoted, because commandFields keeps the quotation
// marks in the field: GOFLAGS="-mod=readonly" reads as the value
// `"-mod=readonly"`, which equals no setting of the release env, so the caller
// refuses it. The check fails closed rather than unquoting a value it does not
// know into one it accepts.
func commandEnv(t *testing.T, command string) (env, args []string) {
	t.Helper()

	fields := commandFields(t, command)

	for len(fields) > 0 && strings.HasPrefix(fields[0], "--") {
		fields = fields[1:]
	}

	if len(fields) > 0 && fields[0] == "env" {
		fields = fields[1:]
	}

	for len(fields) > 0 {
		name, _, isAssignment := strings.Cut(fields[0], "=")
		if !isAssignment || !isEnvName(name) {
			break
		}

		env = append(env, fields[0])
		fields = fields[1:]
	}

	return env, fields
}

// commandFields splits a RUN instruction's command into fields the way a shell
// would, except that it keeps the quote characters in the field: a quoted value
// reads as the quoted text, so a caller that compares it with an unquoted
// setting refuses it rather than unquoting a value it has not read. A quote the
// command never closes fails the test, since no shell could run the command, and
// so does a field that carries a shell operator outside its quotation marks.
func commandFields(t *testing.T, command string) []string {
	t.Helper()

	fields, err := splitCommandFields(command)
	if err != nil {
		t.Fatalf("%s: %v", dockerfileName, err)
	}

	return fields
}

// splitCommandFields is commandFields without a *testing.T: it returns the
// error commandFields fails the test with, so a table can pin which command
// lines are refused and which are read into which fields.
//
// A field that carries a shell operator outside any quotes is refused rather
// than kept as an opaque value: the -o value and an unquoted -ldflags= value are
// one field each however much of a command line they hold, so
// `/out/shift2eu;GOFLAGS=-mod=mod${IFS}go${IFS}build${IFS}-o${IFS}/out/shift2eu ...`
// would read as a path while the shell reads it as a second build that
// overwrites the binary outside the release env.
func splitCommandFields(command string) ([]string, error) {
	var (
		fields     []string
		field      strings.Builder
		quote      rune
		insideMark bool
	)

	for _, r := range command {
		switch {
		case insideMark:
			field.WriteRune(r)
			insideMark = r != quote
		case r == '"' || r == '\'':
			quote, insideMark = r, true
			field.WriteRune(r)
		case unicode.IsSpace(r):
			if field.Len() > 0 {
				fields = append(fields, field.String())
				field.Reset()
			}
		default:
			field.WriteRune(r)
		}
	}

	if insideMark {
		return nil, fmt.Errorf("the command %q has an unclosed %c, so no shell could run it and this test cannot read its fields", command, quote)
	}

	if field.Len() > 0 {
		fields = append(fields, field.String())
	}

	for _, field := range fields {
		if operator := unquotedShellOperator(field); operator != "" {
			return nil, fmt.Errorf("the command field %q carries the shell operator %q outside any quotes, so the RUN instruction runs more than `go build`: an unquoted ;, &, |, ` or $( ends this command or starts another, such as a second go build with other settings that overwrites the binary; want the operator inside quotes, for a value that is not a command, or no operator at all",
				field, operator)
		}
	}

	return fields, nil
}

// unquotedShellOperator returns the first shell operator in field that a shell
// reads outside the quotation marks the field carries, and "" when the field
// holds none: the operators that end a command or start another, ;, &, |, the
// backtick substitution and $(. A field holding one is not a flag value but
// part of a command line.
func unquotedShellOperator(field string) string {
	var quote rune

	for i, r := range field {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ';' || r == '&' || r == '|' || r == '`':
			return string(r)
		case strings.HasPrefix(field[i:], "$("):
			return "$("
		}
	}

	return ""
}

// isEnvName reports whether name is a shell variable name: a letter or an
// underscore, then letters, digits and underscores. The = after such a name is
// what tells an environment assignment at the front of a command from a flag
// such as --mount=type=cache or -ldflags "...".
func isEnvName(name string) bool {
	if name == "" {
		return false
	}

	for i, r := range name {
		letter := r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'

		switch {
		case letter:
		case i > 0 && digit:
		default:
			return false
		}
	}

	return true
}

// imageTag returns the tag of a golang image reference, "1.27.4-bookworm" for
// "golang:1.27.4-bookworm@sha256:...".
func imageTag(image string) string {
	tag := strings.TrimPrefix(image, "golang:")
	tag, _, _ = strings.Cut(tag, "@")

	return tag
}

// baseImageTag returns the tag of any image reference: the part between the
// last colon and the @ digest, and "" for an untagged reference. A colon in a
// registry port, as in localhost:5000/distroless/static, is not a tag.
func baseImageTag(image string) string {
	reference, _, _ := strings.Cut(image, "@")

	colon := strings.LastIndex(reference, ":")
	if colon <= strings.LastIndex(reference, "/") {
		return ""
	}

	return reference[colon+1:]
}

// isDebugBaseImage reports whether an image reference asks for the debug
// variant of a distroless image. Distroless publishes :debug and
// :debug-nonroot builds that hold a busybox shell and a package manager for
// troubleshooting, which is exactly what the minimal final stage rules out.
func isDebugBaseImage(image string) bool {
	return strings.Contains(strings.ToLower(baseImageTag(image)), "debug")
}

// usesBuildArg reports whether the environment prefix sets env to the buildx
// platform argument, as GOOS=$TARGETOS or GOOS=${TARGETOS}. The whole
// assignment is compared, so a GOOS that reads anything else is not accepted.
func usesBuildArg(env []string, name, buildArg string) bool {
	return slices.Contains(env, name+"=$"+buildArg) ||
		slices.Contains(env, name+"=${"+buildArg+"}")
}

// parseGoreleaserBuilds reads the builds list of .goreleaser.yaml: each "- "
// item at the list's own indentation starts an entry, the keys indented under it
// set the entry's strings and lists, and a list is either a "key:" line
// followed by "- value" items or a flow list [a, b] on the key's own line. The
// indentation of an entry is read from the first item, so the "- value" items of
// a list are told apart from the entries of the builds list, and a key at the
// top level ends it. A YAML library would be a dependency for one file
// (CLAUDE.md rule 6).
func parseGoreleaserBuilds(t *testing.T, goreleaser string) []goreleaserBuild {
	t.Helper()

	var (
		builds      []goreleaserBuild
		current     *goreleaserBuild
		list        *[]string
		entryIndent = -1
		inBuilds    bool
		flush       = func() {
			if current != nil {
				builds = append(builds, *current)
				current = nil
			}
		}
	)

	for i, raw := range strings.Split(goreleaser, "\n") {
		line := strings.TrimRight(strings.TrimSuffix(raw, "\r"), " ")
		text := strings.TrimSpace(line)

		if text == "" || strings.HasPrefix(text, "#") || text == "---" {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))

		if !inBuilds {
			if indent == 0 && text == "builds:" {
				inBuilds = true
			}

			continue
		}

		item, isItem := strings.CutPrefix(text, "- ")

		// A key at the top level, such as archives: or release:, ends the
		// builds list.
		if indent == 0 && !isItem {
			break
		}

		switch {
		case isItem && (entryIndent < 0 || indent == entryIndent):
			entryIndent = indent
			flush()
			current = &goreleaserBuild{line: i + 1}

			key, value, found := strings.Cut(item, ":")
			if !found {
				t.Fatalf("%s:%d: the builds entry %q names no key: want an entry such as `- id: shift2eu`",
					goreleaserName, i+1, text)
			}

			list = setBuildKey(t, current, i+1, strings.TrimSpace(key), strings.TrimSpace(value))
		case isItem:
			if list == nil {
				t.Fatalf("%s:%d: the list item %q under the builds entry at line %d belongs to no list: want it under env, flags, ldflags, goos or goarch",
					goreleaserName, i+1, text, current.line)
			}

			*list = append(*list, unquote(item))
		default:
			if current == nil {
				t.Fatalf("%s:%d: the key %q comes before the first builds entry: want every key indented under an entry such as `- id: shift2eu`",
					goreleaserName, i+1, text)
			}

			key, value, found := strings.Cut(text, ":")
			if !found {
				t.Fatalf("%s:%d: the line %q in the builds entry at line %d names no value: want key: value, or key: and then the list items",
					goreleaserName, i+1, text, current.line)
			}

			list = setBuildKey(t, current, i+1, strings.TrimSpace(key), strings.TrimSpace(value))
		}
	}

	flush()

	if !inBuilds {
		t.Fatalf("%s: no builds: section, so the release builds no binary", goreleaserName)
	}

	if len(builds) == 0 {
		t.Fatalf("%s: the builds: section holds no entry: want one per binary the release ships", goreleaserName)
	}

	return builds
}

// setBuildKey sets the field key of one builds entry from the value written on
// the same line, and returns the list a following "- value" line belongs to: a
// key with no value opens its list, and a flow list [a, b] is filled at once. A
// key the test does not read is ignored, so the document may carry settings
// this test has nothing to say about.
func setBuildKey(t *testing.T, build *goreleaserBuild, line int, key, value string) *[]string {
	t.Helper()

	list := build.list(key)
	if list == nil {
		build.setScalar(key, unquote(value))

		return nil
	}

	switch {
	case value == "":
		return list
	case strings.HasPrefix(value, "["):
		if !strings.HasSuffix(value, "]") {
			t.Fatalf("%s:%d: the %s list %q has no closing bracket", goreleaserName, line, key, value)
		}

		for _, item := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"), ",") {
			if item = strings.TrimSpace(item); item != "" {
				*list = append(*list, unquote(item))
			}
		}
	default:
		*list = append(*list, unquote(value))
	}

	return nil
}

// goreleaserBuildSettings returns the builds entry named id, and fails the test
// when the file holds none, naming the ids it does hold: an entry that was
// renamed or never added is then reported as that, rather than leaving this test
// with nothing to compare.
func goreleaserBuildSettings(t *testing.T, builds []goreleaserBuild, id string) goreleaserBuild {
	t.Helper()

	ids := make([]string, 0, len(builds))

	for _, build := range builds {
		if build.id != id {
			ids = append(ids, build.id)

			continue
		}

		if len(build.env) == 0 || len(build.flags) == 0 || len(build.ldflags) == 0 {
			t.Fatalf("%s: the builds entry %q at line %d has env=%v, flags=%v and ldflags=%v: the test reads the image's build settings from all three lists, so each must hold an item",
				goreleaserName, id, build.line, build.env, build.flags, build.ldflags)
		}

		return build
	}

	t.Fatalf("%s: no builds entry with id %q: the builds: section holds %q; want an entry that builds this image's binary, with the release's env, flags and ldflags",
		goreleaserName, id, ids)

	return goreleaserBuild{}
}

// unquote removes the matching quotes YAML allows around a scalar, and Docker
// around an ENV value, which sets GOTOOLCHAIN="local" to local.
func unquote(value string) string {
	if len(value) >= 2 {
		if first, last := value[0], value[len(value)-1]; (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}

	return value
}

// stageLines returns the FROM instruction at index i and the body after it, up
// to the next FROM: the whole of the stage that FROM starts.
func stageLines(instructions []instruction, i int) []instruction {
	for j := i + 1; j < len(instructions); j++ {
		if instructions[j].keyword() == "FROM" {
			return instructions[i:j]
		}
	}

	return instructions[i:]
}

// stageInstructions returns the instructions after the FROM at index i, up to
// the next FROM: the body of the stage that FROM starts.
func stageInstructions(instructions []instruction, i int) []instruction {
	return stageLines(instructions, i)[1:]
}

// stageFor returns the whole of the stage named name, its FROM instruction
// included, and the last stage when name is empty. It fails the test when no
// stage carries the name, naming the stages the Dockerfile does declare, so a
// missing or renamed stage is reported as that rather than as a check on
// another stage's instructions.
func stageFor(t *testing.T, instructions []instruction, name string) []instruction {
	t.Helper()

	if name == "" {
		return finalStage(t, instructions)
	}

	var declared []string

	for i, in := range instructions {
		if in.keyword() != "FROM" {
			continue
		}

		fields := fromFields(in)
		if len(fields) < 3 || !strings.EqualFold(fields[1], "AS") {
			continue
		}

		if strings.EqualFold(fields[2], name) {
			return stageLines(instructions, i)
		}

		declared = append(declared, fields[2])
	}

	t.Fatalf("%s: no stage is named %q: the Dockerfile declares the stages %q; want a stage `FROM <base> AS %s`, built with `docker buildx build --target %s`",
		dockerfileName, name, declared, name, name)

	return nil
}

// builderStage returns the FROM instruction of the stage that runs `go build`,
// and fails the test when no stage builds anything: the builder is found by what
// it does, so it is checked whatever the stage is called.
func builderStage(t *testing.T, instructions []instruction) instruction {
	t.Helper()

	for i, in := range instructions {
		if in.keyword() != "FROM" {
			continue
		}

		for _, stageIn := range stageInstructions(instructions, i) {
			if stageIn.keyword() == "RUN" && runsGoBuild(stageIn.arguments()) {
				return in
			}
		}
	}

	t.Fatalf("%s: no stage runs `go build`, so there is no builder stage whose platform can be checked",
		dockerfileName)

	return instruction{}
}

// buildStage returns the FROM instruction of the stage that runs `go build` and
// the instructions after it, up to the next FROM: the whole of the stage whose
// build the toolchain pin protects.
func buildStage(t *testing.T, instructions []instruction) (instruction, []instruction) {
	t.Helper()

	builder := builderStage(t, instructions)

	for i, in := range instructions {
		if in.keyword() == "FROM" && in.line == builder.line {
			return builder, stageInstructions(instructions, i)
		}
	}

	t.Fatalf("%s:%d: the FROM instruction that starts the stage that runs `go build` is not among the instructions read: want stageInstructions to find it",
		dockerfileName, builder.line)

	return instruction{}, nil
}

// copiedBinary returns the path in the build stage that one image stage copies
// to become its binary, and the path it copies it to: the source and destination
// of its one COPY --from=<builder> instruction, /out/shift2eu and /shift2eu for
// the CLI image. It fails the test when the stage copies nothing from the stage
// that runs `go build`, or copies more than one thing from it, so the path the
// build writes and the path the image ships cannot drift apart. stageDesc names
// the stage in failure messages.
func copiedBinary(t *testing.T, instructions, stage []instruction, stageDesc string) (source, destination string) {
	t.Helper()

	builder := builderStage(t, instructions)

	builderFields := fromFields(builder)
	if len(builderFields) < 3 || !strings.EqualFold(builderFields[1], "AS") {
		t.Fatalf("%s: the stage that runs `go build` starts FROM %q and names no stage: want a named stage such as `FROM ... AS build`, so %s can copy the binary from it and this test can check the path the build writes it to",
			dockerfileName, fromImage(builder), stageDesc)
	}

	builderName := builderFields[2]
	var copies [][2]string

	for _, in := range stage {
		if in.keyword() != "COPY" {
			continue
		}

		from := ""
		var paths []string

		for _, field := range strings.Fields(in.arguments()) {
			switch {
			case strings.HasPrefix(field, "--from="):
				from = strings.TrimPrefix(field, "--from=")
			case strings.HasPrefix(field, "--"):
			default:
				paths = append(paths, field)
			}
		}

		if !strings.EqualFold(from, builderName) {
			continue
		}

		if len(paths) < 2 {
			t.Fatalf("%s:%d: the COPY instruction copies %q from the stage %q that runs `go build`: want `COPY --from=%s <source> <destination>`, so this test can read the path the binary is built to",
				dockerfileName, in.line, in.arguments(), builderName, builderName)
		}

		copies = append(copies, [2]string{paths[len(paths)-2], paths[len(paths)-1]})
	}

	switch len(copies) {
	case 1:
		return copies[0][0], copies[0][1]
	case 0:
		t.Fatalf("%s: %s has no COPY instruction with --from=%s, the stage that runs `go build`: want one, such as `COPY --from=%s /out/shift2eu /shift2eu`, so the binary the image ships is the one that build writes",
			dockerfileName, stageDesc, builderName, builderName)
	}

	t.Fatalf("%s: %s has %d COPY instructions with --from=%s, the stage that runs `go build`: want exactly one, so it is clear which of the paths the build writes the image ships",
		dockerfileName, stageDesc, len(copies), builderName)

	return "", ""
}

// assertBuilderCrossCompiles fails unless the stage that runs `go build` starts
// FROM --platform=$BUILDPLATFORM. It reads the Dockerfile text it is given, so a
// mutated copy can be run through it to show the check discriminates.
func assertBuilderCrossCompiles(t *testing.T, dockerfile string) {
	t.Helper()

	builder := builderStage(t, dockerfileInstructions(t, dockerfile))

	switch platform := fromPlatform(builder); platform {
	case "$BUILDPLATFORM", "${BUILDPLATFORM}":
		return
	case "":
		t.Errorf("%s:%d: the stage that runs `go build` starts FROM %q and names no platform: want FROM --platform=$BUILDPLATFORM, so the builder runs on the build host and cross-compiles for TARGETOS and TARGETARCH; without it a --platform linux/arm64 build emulates the whole builder stage under QEMU",
			dockerfileName, builder.line, fromImage(builder))
	default:
		t.Errorf("%s:%d: the stage that runs `go build` starts FROM --platform=%s %q: want --platform=$BUILDPLATFORM, so the builder runs on the build host and cross-compiles for TARGETOS and TARGETARCH instead of being emulated under QEMU",
			dockerfileName, builder.line, platform, fromImage(builder))
	}
}

// fromFields returns the fields of a FROM instruction after its flags: the
// image reference, then AS and the stage name when the stage has one.
func fromFields(in instruction) []string {
	fields := strings.Fields(in.arguments())
	for len(fields) > 0 && strings.HasPrefix(fields[0], "--") {
		fields = fields[1:]
	}

	return fields
}

// fromImage returns the image reference of a FROM instruction, and "" when the
// instruction names no image.
func fromImage(in instruction) string {
	if fields := fromFields(in); len(fields) > 0 {
		return fields[0]
	}

	return ""
}

// fromPlatform returns the value of the --platform flag of a FROM instruction,
// "$BUILDPLATFORM" for "--platform=$BUILDPLATFORM", and "" when it carries
// none. Only the flags before the image reference are read.
func fromPlatform(in instruction) string {
	for _, field := range strings.Fields(in.arguments()) {
		if !strings.HasPrefix(field, "--") {
			break
		}

		if value, found := strings.CutPrefix(field, "--platform="); found {
			return value
		}
	}

	return ""
}

// xFlagPrefixes returns the path prefix of every linker -X flag in ldflags, the
// part up to and including the =: GoReleaser fills the value from a template,
// the Dockerfile from a build arg, so only the prefix can match.
func xFlagPrefixes(ldflags []string) []string {
	var prefixes []string

	for _, flag := range ldflags {
		fields := strings.Fields(flag)

		for i := 0; i+1 < len(fields); i++ {
			if fields[i] != "-X" {
				continue
			}

			if path, _, found := strings.Cut(fields[i+1], "="); found {
				prefixes = append(prefixes, path+"=")
			}
		}
	}

	return prefixes
}

// labelPair matches one key=value pair of a LABEL instruction, the value quoted
// with either quote character or bare.
var labelPair = regexp.MustCompile(`([A-Za-z0-9][A-Za-z0-9._-]*)=(?:"([^"]*)"|'([^']*)'|(\S+))`)

// finalStageLabels returns the labels the final stage sets, its last value
// winning when a key appears twice.
func finalStageLabels(instructions []instruction) map[string]string {
	labels := make(map[string]string)

	for _, in := range instructions {
		if in.keyword() != "LABEL" {
			continue
		}

		for _, pair := range labelPair.FindAllStringSubmatch(in.arguments(), -1) {
			for _, value := range pair[2:] {
				if value != "" {
					labels[pair[1]] = value

					break
				}
			}

			if _, set := labels[pair[1]]; !set {
				labels[pair[1]] = ""
			}
		}
	}

	return labels
}

// dockerignoreEntries returns the normalised lines of .dockerignore: comments and
// blank lines dropped, a leading or trailing slash removed, so "/bin/" reads as
// the entry "bin".
func dockerignoreEntries(t *testing.T, dockerignore string) []string {
	t.Helper()

	var entries []string

	for _, raw := range strings.Split(dockerignore, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		entries = append(entries, strings.Trim(strings.TrimPrefix(line, "/"), "/"))
	}

	if len(entries) == 0 {
		t.Fatalf("%s: no entry, so the build context holds the git data, the worktrees, the build output and any local inventory or report",
			dockerignoreName)
	}

	return entries
}

// dockerignorePattern is one .dockerignore line: the pattern with its `!` and
// surrounding slashes removed, whether it re-includes what an earlier line
// excluded, and the line number and text for failure messages.
type dockerignorePattern struct {
	line    int
	raw     string
	pattern string
	negate  bool
}

// embeddedFile is one file a //go:embed directive embeds, named by its slash
// path relative to the repository root, with the directive's place for failure
// messages.
type embeddedFile struct {
	path   string
	source string
	line   int
}

// embedDirective matches a //go:embed line and captures its patterns.
var embedDirective = regexp.MustCompile(`^//go:embed\s+(.+)$`)

// dockerignorePatterns parses .dockerignore into its patterns: comments and
// blank lines are dropped, a leading `!` marks a re-include, and the slashes
// that only anchor a pattern to the context root are removed. A line that
// cannot be evaluated fails the test instead of being skipped.
func dockerignorePatterns(t *testing.T, dockerignore string) []dockerignorePattern {
	t.Helper()

	var patterns []dockerignorePattern

	for i, raw := range strings.Split(dockerignore, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		pattern := strings.Trim(strings.TrimPrefix(line, "!"), "/")
		if pattern == "" {
			t.Errorf("%s:%d: the entry %q names no path, so this test cannot tell what the build context holds",
				dockerignoreName, i+1, line)

			continue
		}

		for _, segment := range strings.Split(pattern, "/") {
			if _, err := path.Match(segment, "x"); err != nil {
				t.Errorf("%s:%d: the pattern %q is not a valid glob: %v",
					dockerignoreName, i+1, line, err)
			}
		}

		patterns = append(patterns, dockerignorePattern{
			line:    i + 1,
			raw:     line,
			pattern: pattern,
			negate:  strings.HasPrefix(line, "!"),
		})
	}

	if len(patterns) == 0 {
		t.Fatalf("%s: no pattern, so the build context holds every file in the repository", dockerignoreName)
	}

	return patterns
}

// dockerignoreExcludes reports whether the patterns leave the slash path,
// relative to the repository root, out of the build context, and returns the
// pattern that decided it: the last pattern that matches wins, and a `!`
// pattern puts the path back in.
func dockerignoreExcludes(patterns []dockerignorePattern, filePath string) (bool, dockerignorePattern) {
	excluded := false
	var last dockerignorePattern

	for _, pattern := range patterns {
		if pattern.matches(filePath) {
			excluded, last = !pattern.negate, pattern
		}
	}

	return excluded, last
}

// matches reports whether the pattern matches the slash path relative to the
// repository root. Docker evaluates a pattern against the path and against each
// of its parent directories, so the directory pattern `cmd` covers what is under
// cmd, and a `**` segment matches zero or more directories.
func (p dockerignorePattern) matches(filePath string) bool {
	if matchDockerignorePath(p.pattern, filePath) {
		return true
	}

	for dir := path.Dir(filePath); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if matchDockerignorePath(p.pattern, dir) {
			return true
		}
	}

	return false
}

// matchDockerignorePath reports whether one pattern matches the slash path
// segment by segment, with `**` standing for zero or more segments and every
// other segment matched by path.Match.
func matchDockerignorePath(pattern, filePath string) bool {
	return matchDockerignoreSegments(strings.Split(pattern, "/"), strings.Split(filePath, "/"))
}

// matchDockerignoreSegments reports whether the pattern segments match the path
// segments: `**` consumes any number of them, and each other segment is one
// path.Match, which does not cross a slash.
func matchDockerignoreSegments(pattern, segments []string) bool {
	if len(pattern) == 0 {
		return len(segments) == 0
	}

	if pattern[0] == "**" {
		for skip := 0; skip <= len(segments); skip++ {
			if matchDockerignoreSegments(pattern[1:], segments[skip:]) {
				return true
			}
		}

		return false
	}

	if len(segments) == 0 {
		return false
	}

	matched, err := path.Match(pattern[0], segments[0])
	if err != nil || !matched {
		return false
	}

	return matchDockerignoreSegments(pattern[1:], segments[1:])
}

// embeddedFiles walks the non-test .go files under cmd/ and internal/, the two
// directories the Dockerfile copies into the builder, and returns every file
// their embed directives name. The patterns are tokenised and unquoted as the go
// command reads them, a glob is expanded, and a directory pattern is walked
// recursively, leaving out the names starting with . or _ that it leaves out
// unless the pattern carries the all: prefix.
func embeddedFiles(t *testing.T) []embeddedFile {
	t.Helper()

	repository, err := os.OpenRoot(repoRoot(t))
	if err != nil {
		t.Fatalf("open the repository root: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Errorf("close the repository root: %v", err)
		}
	})

	tree := repository.FS()
	var files []embeddedFile

	for _, top := range []string{"cmd", "internal"} {
		err := fs.WalkDir(tree, top, func(filePath string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case entry.IsDir(), !strings.HasSuffix(filePath, ".go"), strings.HasSuffix(filePath, "_test.go"):
				return nil
			}

			data, err := fs.ReadFile(tree, filePath)
			if err != nil {
				return err
			}

			for i, line := range strings.Split(string(data), "\n") {
				match := embedDirective.FindStringSubmatch(strings.TrimSpace(line))
				if match == nil {
					continue
				}

				patterns, parseErr := embedPatternTokens(match[1])
				if parseErr != nil {
					t.Fatalf("%s:%d: //go:embed %s: %v", filePath, i+1, match[1], parseErr)
				}

				for _, pattern := range patterns {
					files = append(files, embedTargets(t, tree, filePath, i+1, pattern)...)
				}
			}

			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", top, err)
		}
	}

	slices.SortFunc(files, func(a, b embeddedFile) int { return strings.Compare(a.path, b.path) })

	return files
}

// embedAllPrefix is the prefix an embed pattern carries to include the files a
// directory pattern otherwise leaves out, the ones whose name starts with . or
// _. The go command strips it before it resolves the pattern.
const embedAllPrefix = "all:"

// embedTargets resolves one embed pattern, declared on the given line of the
// .go file source, to the files it embeds, relative to its package directory.
// The pattern is one token as embedPatternTokens returns it: already unquoted,
// with the all: prefix still on it. A pattern matching nothing fails the test: go
// build fails the same way, so the repository is broken rather than this test.
func embedTargets(t *testing.T, tree fs.FS, source string, line int, pattern string) []embeddedFile {
	t.Helper()

	name := pattern
	all := false
	if stripped, found := strings.CutPrefix(name, embedAllPrefix); found {
		name, all = stripped, true
	}

	target := path.Join(path.Dir(source), name)

	matches := []string{target}
	if strings.ContainsAny(name, `*?[`) {
		found, err := fs.Glob(tree, target)
		if err != nil {
			t.Fatalf("%s:%d: the embed glob %q cannot be expanded: %v", source, line, pattern, err)
		}

		if len(found) == 0 {
			t.Fatalf("%s:%d: the embed glob %q matches no file, so go build fails on the package",
				source, line, pattern)
		}

		matches = found
	}

	var files []embeddedFile

	for _, match := range matches {
		info, err := fs.Stat(tree, match)
		if err != nil {
			t.Fatalf("%s:%d: the embed pattern %q names %s, which cannot be read: %v; go build fails on the package",
				source, line, pattern, match, err)
		}

		if !info.IsDir() {
			files = append(files, embeddedFile{path: match, source: source, line: line})

			continue
		}

		err = fs.WalkDir(tree, match, func(embeddedPath string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case embeddedPath == match:
				return nil
			case !all && excludedByEmbed(entry.Name()):
				if entry.IsDir() {
					return fs.SkipDir
				}

				return nil
			case entry.IsDir():
				return nil
			}

			files = append(files, embeddedFile{path: embeddedPath, source: source, line: line})

			return nil
		})
		if err != nil {
			t.Fatalf("%s:%d: walk the embed directory %s: %v", source, line, match, err)
		}
	}

	return files
}

// excludedByEmbed reports whether go:embed leaves the name out of a directory
// pattern: it skips the files and directories whose name starts with . or _,
// unless the pattern carries the all: prefix.
func excludedByEmbed(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// embedPatternTokens splits the patterns of a //go:embed directive the way the
// go command does: spaces and tabs separate them, and a pattern may be written
// as a double-quoted or back-quoted Go string literal, which may hold spaces and
// escapes. Every quoted token is unquoted, so the caller works with the file
// names as the go command reads them.
func embedPatternTokens(patterns string) ([]string, error) {
	var tokens []string

	for rest := strings.TrimLeft(patterns, " \t"); rest != ""; rest = strings.TrimLeft(rest, " \t") {
		if rest[0] == '"' || rest[0] == '`' {
			token, after, err := cutQuotedEmbedPattern(rest)
			if err != nil {
				return nil, err
			}

			tokens = append(tokens, token)
			rest = after

			continue
		}

		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			end = len(rest)
		}

		tokens = append(tokens, rest[:end])
		rest = rest[end:]
	}

	return tokens, nil
}

// cutQuotedEmbedPattern reads the Go string literal at the start of rest, which
// begins with a double or back quote, and returns its value and the text after
// it.
func cutQuotedEmbedPattern(rest string) (string, string, error) {
	quote := rest[0]

	for i := 1; i < len(rest); i++ {
		switch {
		case quote == '"' && rest[i] == '\\':
			i++
		case rest[i] == quote:
			value, err := strconv.Unquote(rest[:i+1])
			if err != nil {
				return "", "", fmt.Errorf("the embed pattern %s is not a valid Go string literal: %w", rest[:i+1], err)
			}

			return value, rest[i+1:], nil
		}
	}

	return "", "", fmt.Errorf("the embed pattern %s has no closing %c", rest, quote)
}
