package check_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The workflow fixtures copy .github/workflows/release.yml and ci.yml, and
// Renovate updates those two documents without touching a Go file. So no
// fixture holds a pin Renovate moves: it holds a placeholder in its place, and
// takes the value from the workflow it is built from (issue #421):
//
//   - {{pin OWNER/REPO}}: the action's commit and version comment, as a uses:
//     line writes them (OWNER/REPO@<commit> # <version>);
//   - {{commit OWNER/REPO}}: the action's commit alone (OWNER/REPO@<commit>);
//   - {{release syft}}, {{release goreleaser}}, {{release cosign}}: the exact
//     release a step's with: pins in syft-version:, the goreleaser
//     distribution's version: and cosign-release:;
//   - {{image moby/buildkit}}: the BuildKit image's release and index digest
//     (<tag>@sha256:<digest>), in the release's driver-opts or the dry run's
//     BUILDKIT_VERSION.
//
// A placeholder never collides with the workflows' own expressions: those
// open ${{ with a space, and the metadata-action patterns ({{raw}}) and the Go
// templates ({{.Manifest.Digest}}) hold no space.
//
// The fixtures are package variables, built before any test runs, so a
// workflow they cannot be built from (a pin Renovate left at two values, or
// one a fixture needs that the workflow no longer holds) panics the package.
// The panic names the workflow, the pin and the lines at fault; the checks of
// the real workflows in ./test/architecture (repository_test.go, such as
// TestCIImageJobIsTheReleaseDryRun and TestReleaseWorkflowIsClosed), which
// build no fixture, say what the workflow does wrong.

// workflowPins is what Renovate updates in one workflow, keyed by the
// placeholder body that stands for it ("pin actions/checkout", "release syft").
type workflowPins struct {
	// file names the workflow the pins are read from, for the panics.
	file string
	// values holds each pin, and lines the workflow's line it was read from.
	values, lines map[string]string
}

var (
	// pinPlaceholder matches a placeholder in a fixture.
	pinPlaceholder = regexp.MustCompile(`\{\{((?:pin|commit|release|image) [^{}\s]+)\}\}`)

	// pinnedAction matches a uses: line's action, commit and version comment.
	pinnedAction = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*([^\s@]+)@([0-9a-f]{40}) # (\S+)$`)

	// pinnedReleases are the exact releases a with: pins, and the BuildKit
	// image, each by the placeholder body that stands for it. GoReleaser's is
	// the version: right below the goreleaser distribution, so that no other
	// tool's quoted version: is taken for it.
	pinnedReleases = []struct {
		key string
		re  *regexp.Regexp
	}{
		{key: "release syft", re: regexp.MustCompile(`(?m)^\s*syft-version:\s*(\S+)$`)},
		{key: "release goreleaser", re: regexp.MustCompile(`(?m)^\s*distribution:\s*goreleaser\n\s*version:\s*"([^"]+)"`)},
		{key: "release cosign", re: regexp.MustCompile(`(?m)^\s*cosign-release:\s*(\S+)$`)},
		{key: "image moby/buildkit", re: regexp.MustCompile(`(?:moby/buildkit:|BUILDKIT_VERSION:\s*)(\S+@sha256:[0-9a-f]{64})`)},
	}

	// workflowName matches a workflow's top-level name:.
	workflowName = regexp.MustCompile(`(?m)^name:\s*(.+?)\s*$`)
)

// The repository's workflows, by path from this package and by top-level
// name, so that a panic names the file a document is.
const (
	repoCIPath      = "../../../../.github/workflows/ci.yml"
	repoReleasePath = "../../../../.github/workflows/release.yml"
)

var workflowFiles = map[string]string{
	"CI":      ".github/workflows/ci.yml",
	"Release": ".github/workflows/release.yml",
}

// The repository's workflows and their pins, read once: the fixtures every
// case changes one part of are built from them.
var (
	repoCI      = readRepoWorkflow(repoCIPath)
	repoRelease = readRepoWorkflow(repoReleasePath)

	ciPins      = pinsOf(repoCI)
	releasePins = pinsOf(repoRelease)
)

// readRepoWorkflow returns the repository's workflow at path. It is called
// while the package's variables are set, before any test runs, so it panics:
// no fixture can be built without the workflow it copies.
func readRepoWorkflow(path string) []byte {
	doc, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		panic(fmt.Sprintf("the workflow fixtures cannot be built: read %s: %v", path, err))
	}

	return doc
}

// fileOf names the workflow doc is: its file, by its top-level name:.
func fileOf(doc []byte) string {
	m := workflowName.FindSubmatch(doc)
	if m == nil {
		return "a workflow without a name:"
	}

	if file, ok := workflowFiles[string(m[1])]; ok {
		return file
	}

	return fmt.Sprintf("the workflow named %q", m[1])
}

// pinsOf returns the pins doc holds. A value doc pins twice differently panics:
// a fixture could not say which of the two it copies.
func pinsOf(doc []byte) workflowPins {
	pins := workflowPins{file: fileOf(doc), values: map[string]string{}, lines: map[string]string{}}

	set := func(key, value, line string) {
		line = strings.TrimSpace(line)
		if was, ok := pins.values[key]; ok && was != value {
			panic(fmt.Sprintf("the workflow fixtures cannot be built: %s pins %s twice, as %q in %q and as %q in %q "+
				"(a Renovate update applied to one of the two?); the checks of the real workflows in ./test/architecture "+
				"(repository_test.go) name the cause", pins.file, key, was, pins.lines[key], value, line))
		}

		pins.values[key], pins.lines[key] = value, line
	}

	for _, m := range pinnedAction.FindAllStringSubmatch(string(doc), -1) {
		line, action, commit, version := m[0], m[1], m[2], m[3]

		set("commit "+action, action+"@"+commit, line)
		set("pin "+action, action+"@"+commit+" # "+version, line)
	}

	for _, r := range pinnedReleases {
		for _, m := range r.re.FindAllStringSubmatch(string(doc), -1) {
			set(r.key, m[1], m[0])
		}
	}

	return pins
}

// expand returns template with every placeholder replaced by the pin it stands
// for. A placeholder whose pin the workflow does not hold panics, so that a
// fixture never carries a placeholder into the document a check reads.
func (p workflowPins) expand(template string) string {
	return pinPlaceholder.ReplaceAllStringFunc(template, func(placeholder string) string {
		key := pinPlaceholder.FindStringSubmatch(placeholder)[1]

		value, ok := p.values[key]
		if !ok {
			panic(fmt.Sprintf("the workflow fixtures cannot be built: %s holds no %s, which the fixture's line %q needs "+
				"(the pin removed, or moved to a tag or range a fixture placeholder cannot copy?); the checks of the real "+
				"workflows in ./test/architecture (repository_test.go) name the cause",
				p.file, key, strings.TrimSpace(lineOf(template, placeholder))))
		}

		return value
	})
}

// lineOf returns the line of doc that holds part, or part when none does.
func lineOf(doc, part string) string {
	for line := range strings.SplitSeq(doc, "\n") {
		if strings.Contains(line, part) {
			return line
		}
	}

	return part
}

// commitOf returns the commit alone that the workflow pins action at.
func (p workflowPins) commitOf(action string) string {
	return strings.TrimPrefix(p.expand("{{commit "+action+"}}"), action+"@")
}
