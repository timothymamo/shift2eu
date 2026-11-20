package releasetarget

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// Main is the main package of the CLI, the binary every release builds: Read
// refuses a .goreleaser.yaml without a build of it, and the repository test
// lists the packages of the release binary from it.
const Main = "./cmd/shift2eu"

// retiredServerMain was the main package of the HTTP API, shift2eu-server (PRD
// #103, ADR 0019), retired with its code (issue #393, ADR 0037). Read refuses a
// build of it with an error that names the retirement.
const retiredServerMain = "./cmd/shift2eu-server"

// Env returns the environment every build of the release must set, exactly
// and in this order (issue #102): CGO_ENABLED=0, so the binary links no C
// library of the build host; GOWORK=off, so a go.work file cannot put the
// build in workspace mode and swap in other module versions; and
// GOFLAGS=-mod=readonly, so the build fails rather than changes go.mod or
// go.sum, and uses no vendor directory. Each call returns a fresh slice.
func Env() []string {
	return []string{"CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=-mod=readonly"}
}

// Target is one platform the release builds.
type Target struct {
	GOOS   string
	GOARCH string
}

// Read returns the platforms a .goreleaser.yaml document builds: the
// goos x goarch product of its builds, sorted by GOOS then GOARCH. Every build
// must give the same product, so it is one list.
//
// It fails closed, and holds the document to the keys the check knows cannot
// change what the release compiles:
//
//   - The top level may hold only version, project_name, before, builds,
//     archives, checksum, changelog and release. Any other key is refused: a
//     top-level env applies to every build, gomod picks the Go binary that
//     builds, and a key the check does not know may do either.
//   - before may hold only hooks, and they must be exactly
//     [go mod tidy -diff]: a hook runs before the build, outside its env, and
//     could rewrite what it compiles; -diff fails when the module is not tidy
//     and writes nothing, where plain go mod tidy would rewrite go.mod and
//     go.sum for the -mod=readonly build to accept.
//   - builds must hold a build of Main and nothing else: each main once, no
//     other package, so the one binary the release ships is the one the
//     repository test lists the packages of. The retired HTTP API's main
//     package, ./cmd/shift2eu-server, is refused by name (issue #393).
//     Each entry
//     may hold only id, main, binary,
//     env, flags, ldflags, goos and goarch: ignore and targets (which change
//     the platform list), tags, overrides, goamd64 and goarm64 (which change
//     the build tags a file is selected by) and every other key are refused.
//     env must be exactly Env(), in its order: cgo off,
//     GOWORK=off so a go.work file cannot swap in other module versions, and
//     GOFLAGS=-mod=readonly so the build fails rather than changes go.mod or
//     go.sum, and uses no vendor directory. No other entry is allowed:
//     goreleaser renders each entry as a template, its name included, so no
//     rule on names alone tells which variable an entry sets. flags may hold
//     only -trimpath. goos and goarch must be non-empty lists of GOOS and
//     GOARCH names, in flow ([a, b]) or block (- a) form. A template ({{) in
//     goos, goarch or flags is refused.
func Read(goreleaser []byte) ([]Target, error) {
	lines, err := yamlread.SplitLines(goreleaser)
	if err != nil {
		return nil, fmt.Errorf("read .goreleaser.yaml: %w", err)
	}

	block, err := topLevel(lines)
	if err != nil {
		return nil, fmt.Errorf("read .goreleaser.yaml: %w", err)
	}

	builds, err := yamlread.Sequence(block)
	if err != nil {
		return nil, fmt.Errorf("read .goreleaser.yaml: builds: %w", err)
	}

	var (
		targets []Target
		mains   []string
	)

	for i, build := range builds {
		mainPkg, buildTargets, err := readBuild(build)
		if err != nil {
			return nil, fmt.Errorf("read .goreleaser.yaml: the build at line %d: %w", build.Num, err)
		}

		if slices.Contains(mains, mainPkg) {
			return nil, fmt.Errorf("read .goreleaser.yaml: the build at line %d builds %s a second time", build.Num, mainPkg)
		}

		mains = append(mains, mainPkg)

		if i > 0 && !slices.Equal(buildTargets, targets) {
			return nil, fmt.Errorf("read .goreleaser.yaml: the build at line %d builds %v, want %v, the platforms of the build at line %d: every binary of the release is built for the same platforms, so they are the one list the check reads",
				build.Num, buildTargets, targets, builds[0].Num)
		}

		targets = buildTargets
	}

	if !slices.Contains(mains, Main) {
		return nil, fmt.Errorf("read .goreleaser.yaml: no build of %s, the CLI every release ships", Main)
	}

	return targets, nil
}

// topLevel checks every top-level key of the document and returns the lines
// of builds.
func topLevel(lines []yamlread.Line) ([]yamlread.Line, error) {
	entries, err := yamlread.Mapping(lines)
	if err != nil {
		return nil, err
	}

	var builds []yamlread.Line

	for _, e := range entries {
		switch e.Key {
		case "version", "project_name", "archives", "checksum", "changelog", "release":
			// The format version, the project's name, and how the built
			// binaries are packed, summed, announced and published: none
			// changes what is compiled. GitHubRelease holds release to its
			// one setting.
		case "before":
			err = checkBefore(e)
		case "builds":
			builds, err = yamlread.BlockValue(e, "builds")
		default:
			err = fmt.Errorf("line %d: top-level key %s may change what the release compiles, and the check does not read it", e.Num, e.Key)
		}

		if err != nil {
			return nil, err
		}
	}

	if builds == nil {
		return nil, errors.New("no builds")
	}

	return builds, nil
}

// checkBefore refuses a before section that holds anything but the hook
// go mod tidy -diff, which fails instead of rewriting go.mod or go.sum.
func checkBefore(before yamlread.Entry) error {
	block, err := yamlread.BlockValue(before, "before")
	if err != nil {
		return err
	}

	entries, err := yamlread.Mapping(block)
	if err != nil {
		return fmt.Errorf("before: %w", err)
	}

	for _, e := range entries {
		if e.Key != "hooks" {
			return fmt.Errorf("line %d: before: key %s may change what the release compiles, and the check does not read it", e.Num, e.Key)
		}

		hooks, err := yamlread.List(e)
		if err != nil {
			return fmt.Errorf("before: %w", err)
		}

		if !slices.Equal(hooks, []string{"go mod tidy -diff"}) {
			return fmt.Errorf("line %d: before: hooks are %q, want exactly [go mod tidy -diff]: a hook runs before the build, outside its env, and -diff fails instead of rewriting go.mod or go.sum", e.Num, hooks)
		}
	}

	return nil
}

// readBuild returns the main package of one build and the goos x goarch
// product it builds, sorted, after checking every key the build holds.
func readBuild(build yamlread.Item) (string, []Target, error) {
	entries, err := yamlread.Mapping(build.Lines)
	if err != nil {
		return "", nil, err
	}

	var (
		mainPkg                  string
		env, flags, goos, goarch []string
	)

	for _, e := range entries {
		switch e.Key {
		case "id", "binary", "ldflags":
			// They name the build and its binary, and set link-time values:
			// none selects the files a package compiles.
		case "main":
			mainPkg, err = yamlread.Single(e)
		case "env":
			env, err = yamlread.List(e)
		case "flags":
			flags, err = yamlread.List(e)
		case "goos":
			goos, err = yamlread.List(e)
		case "goarch":
			goarch, err = yamlread.List(e)
		default:
			return "", nil, fmt.Errorf("line %d: key %s may change what the release compiles, and the check does not read it", e.Num, e.Key)
		}

		if err != nil {
			return "", nil, err
		}
	}

	if mainPkg != Main {
		if mainPkg == retiredServerMain {
			return "", nil, fmt.Errorf("main is %q, the retired HTTP API's main package: the release ships the CLI alone, so it builds %s only (issue #393)", mainPkg, Main)
		}

		return "", nil, fmt.Errorf("main is %q, want %s, the only package the release builds", mainPkg, Main)
	}

	if want := Env(); !slices.Equal(env, want) {
		return "", nil, fmt.Errorf("env is %q, want exactly %q, in that order: goreleaser renders each entry, its name included, as a template, so any other entry may set a variable the go command reads", env, want)
	}

	for _, kv := range []struct {
		key    string
		values []string
	}{
		{key: "flags", values: flags},
		{key: "goos", values: goos},
		{key: "goarch", values: goarch},
	} {
		if err := checkNoTemplate(kv.key, kv.values); err != nil {
			return "", nil, err
		}
	}

	for _, flag := range flags {
		if flag != "-trimpath" {
			return "", nil, fmt.Errorf("flag %s may change what the release compiles, and the check does not apply it", flag)
		}
	}

	if err := checkPlatformNames("goos", goos); err != nil {
		return "", nil, err
	}

	if err := checkPlatformNames("goarch", goarch); err != nil {
		return "", nil, err
	}

	targets := make([]Target, 0, len(goos)*len(goarch))

	for _, system := range goos {
		for _, arch := range goarch {
			targets = append(targets, Target{GOOS: system, GOARCH: arch})
		}
	}

	slices.SortFunc(targets, func(a, b Target) int {
		if c := strings.Compare(a.GOOS, b.GOOS); c != 0 {
			return c
		}

		return strings.Compare(a.GOARCH, b.GOARCH)
	})

	return mainPkg, targets, nil
}

// checkNoTemplate refuses an entry that holds a template, which goreleaser
// renders and the check does not.
func checkNoTemplate(key string, values []string) error {
	for _, value := range values {
		if strings.Contains(value, "{{") {
			return fmt.Errorf("%s entry %q is a template, which goreleaser renders and the check does not", key, value)
		}
	}

	return nil
}

// checkPlatformNames refuses an empty list, a name that is not lower-case
// ASCII letters and digits, and a name listed twice.
func checkPlatformNames(key string, names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("%s is missing or empty", key)
	}

	for i, name := range names {
		if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789") != "" {
			return fmt.Errorf("%s entry %q is not a GOOS or GOARCH name", key, name)
		}

		if slices.Contains(names[:i], name) {
			return fmt.Errorf("%s lists %s twice", key, name)
		}
	}

	return nil
}
