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
//     release a step's with: pins in syft-version:, version: and
//     cosign-release:;
//   - {{image moby/buildkit}}: the BuildKit image's release and index digest
//     (<tag>@sha256:<digest>), in the release's driver-opts or the dry run's
//     BUILDKIT_VERSION.
//
// A placeholder never collides with the workflows' own expressions: those
// open ${{ with a space, and the metadata-action patterns ({{raw}}) and the Go
// templates ({{.Manifest.Digest}}) hold no space.

// workflowPins is what Renovate updates in one workflow, keyed by the
// placeholder body that stands for it ("pin actions/checkout", "release syft").
type workflowPins map[string]string

var (
	// pinPlaceholder matches a placeholder in a fixture.
	pinPlaceholder = regexp.MustCompile(`\{\{((?:pin|commit|release|image) [^{}\s]+)\}\}`)

	// pinnedAction matches a uses: line's action, commit and version comment.
	pinnedAction = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*([^\s@]+)@([0-9a-f]{40}) # (\S+)$`)

	// pinnedReleases are the exact releases a with: pins, and the BuildKit
	// image, each by the placeholder body that stands for it.
	pinnedReleases = []struct {
		key string
		re  *regexp.Regexp
	}{
		{key: "release syft", re: regexp.MustCompile(`(?m)^\s*syft-version:\s*(\S+)$`)},
		{key: "release goreleaser", re: regexp.MustCompile(`(?m)^\s*version:\s*"([^"]+)"`)},
		{key: "release cosign", re: regexp.MustCompile(`(?m)^\s*cosign-release:\s*(\S+)$`)},
		{key: "image moby/buildkit", re: regexp.MustCompile(`(?:moby/buildkit:|BUILDKIT_VERSION:\s*)(\S+@sha256:[0-9a-f]{64})`)},
	}
)

// The repository's workflows and their pins, read once: the fixtures every
// case changes one part of are built from them.
var (
	repoCI      = readRepoWorkflow("ci.yml")
	repoRelease = readRepoWorkflow("release.yml")

	ciPins      = pinsOf(repoCI)
	releasePins = pinsOf(repoRelease)
)

// readRepoWorkflow returns the repository's .github/workflows/name. It is
// called while the package's variables are set, before any test runs, so it
// panics: no fixture can be built without the workflow it copies.
func readRepoWorkflow(name string) []byte {
	doc, err := os.ReadFile(filepath.FromSlash("../../../../.github/workflows/" + name))
	if err != nil {
		panic(fmt.Sprintf("read %s: %v", name, err))
	}

	return doc
}

// pinsOf returns the pins doc holds. A value doc pins twice differently panics:
// a fixture could not say which of the two it copies.
func pinsOf(doc []byte) workflowPins {
	pins := workflowPins{}

	set := func(key, value string) {
		if was, ok := pins[key]; ok && was != value {
			panic(fmt.Sprintf("the workflow pins %s twice, as %q and as %q", key, was, value))
		}

		pins[key] = value
	}

	for _, m := range pinnedAction.FindAllStringSubmatch(string(doc), -1) {
		action, commit, version := m[1], m[2], m[3]

		set("commit "+action, action+"@"+commit)
		set("pin "+action, action+"@"+commit+" # "+version)
	}

	for _, r := range pinnedReleases {
		for _, m := range r.re.FindAllStringSubmatch(string(doc), -1) {
			set(r.key, m[1])
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

		value, ok := p[key]
		if !ok {
			panic(fmt.Sprintf("the workflow holds no %s for the fixture's %s", key, placeholder))
		}

		return value
	})
}

// commitOf returns the commit alone that the workflow pins action at.
func (p workflowPins) commitOf(action string) string {
	return strings.TrimPrefix(p.expand("{{commit "+action+"}}"), action+"@")
}
