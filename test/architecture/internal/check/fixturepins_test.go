package check_test

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// Renovate updates .github/workflows/release.yml and .github/workflows/ci.yml;
// it never edits a Go file. The fixtures that copy the two documents must
// therefore take what they copy from the documents at test time: the commit
// and the trailing version comment of every uses: line they repeat, and the
// exact release the with: of a step pins, GoReleaser's, cosign's, syft's and
// the BuildKit image's (issue #421, Renovate's pull request #419).
//
// The checks of the fixtures themselves are below: a Renovate update is
// applied to the two documents in memory, and the fixtures built from the
// updated bytes must say what those bytes say.
var (
	// usesPin matches a uses: line's value: the action, its commit, and the
	// version comment Renovate moves with it. The comment is matched by shape
	// alone, so that a pin written as a range or a pre-release is followed as
	// the pin it is: the helpers' pinnedAction (workflowpins_test.go) takes any
	// comment too.
	usesPin = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*(\S+?@[0-9a-f]{40})(?: # (\S+))?$`)

	// usesCommit matches a uses: line of any shape, whatever follows its
	// commit: what bumpPins holds every pin to.
	usesCommit = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*\S+@[0-9a-f]{40}`)

	// exactRelease matches a version comment or a with: value written as an
	// exact release, the only kind of pin this test bumps: Renovate moves a
	// range (# v4) or a pre-release (# v1.2.3-rc.1) itself, untouched by the
	// update applied here.
	exactRelease = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

	// syftVersion matches the exact syft release a syft-version: line pins.
	syftVersion = regexp.MustCompile(`(?m)^\s*syft-version:\s*(v[0-9]+\.[0-9]+\.[0-9]+)$`)

	// goreleaserVersion matches the exact GoReleaser release the goreleaser
	// step's with: pins: the version: right below the goreleaser distribution,
	// as the helpers' twin holds it (workflowpins_test.go), so that no other
	// tool's quoted version: (the golangci-lint action's, say) is taken for
	// GoReleaser's and moved.
	goreleaserVersion = regexp.MustCompile(`(?m)^\s*distribution:\s*goreleaser\n\s*version:\s*"(v[0-9]+\.[0-9]+\.[0-9]+)"`)

	// cosignRelease matches the exact cosign release a cosign-release: line
	// pins.
	cosignRelease = regexp.MustCompile(`(?m)^\s*cosign-release:\s*(v[0-9]+\.[0-9]+\.[0-9]+)$`)

	// digestImage matches an image pinned by release and index digest: the
	// BuildKit image in the release's driver-opts and in the dry run's env:.
	digestImage = regexp.MustCompile(`v[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}`)
)

// TestWorkflowFixturesFollowThePins pins that the workflow fixtures are built
// from the workflows, so that a Renovate update needs no hand edit of a Go
// file: rendering the release fixture rebuilds the workflow it was built from
// byte for byte (as TestReleaseWorkflowFixtureIsTheWorkflow does for the
// repository's own release.yml), the dry-run fixtures carry the pins of the
// documents they were built from, and they still pass the checks
// TestDryRunMirrorsRelease runs. A drifted syft or action pin is still
// refused, as TestReleaseWorkflow's and TestDryRunMirrorsRelease's own cases
// refuse one.
func TestWorkflowFixturesFollowThePins(t *testing.T) {
	t.Parallel()

	ci, release := readWorkflows(t)
	bumpedCI, bumpedRelease := renovateBump(t, ci, release)

	// The fixtures of the two documents, built from the updated bytes.
	releaseFixture := releaseWorkflowFixtureFrom(bumpedRelease)
	ciDryRun, releaseDryRun := ciDryRunFrom(bumpedCI), releaseDryRunFrom(bumpedRelease)

	t.Run("the release fixture is the updated release workflow", func(t *testing.T) {
		t.Parallel()

		if diff := cmp.Diff(string(bumpedRelease), releaseFixture.render()); diff != "" {
			t.Errorf("the fixture built from an updated release.yml is not that workflow (-want +got):\n%s", diff)
		}
	})

	t.Run("the updated workflow is one the release schema accepts", func(t *testing.T) {
		t.Parallel()

		// Every pin the schema lists is matched by form, so what the case above
		// reports is the fixture's own copy and never the update itself.
		if got := check.ReleaseWorkflow(bumpedRelease); len(got) != 0 {
			t.Errorf("ReleaseWorkflow(updated release.yml) = %q, want no message", got)
		}
	})

	t.Run("the dry-run fixtures carry the updated pins", func(t *testing.T) {
		t.Parallel()

		assertPinsFollow(t, "the dry run of ci.yml", ciDryRun, string(bumpedCI))
		assertPinsFollow(t, "the dry run of release.yml", releaseDryRun, string(bumpedRelease))
	})

	t.Run("the updated pair passes the dry-run checks", func(t *testing.T) {
		t.Parallel()

		if got := check.DryRunMirrorsRelease([]byte(ciDryRun), []byte(releaseDryRun)); len(got) != 0 {
			t.Errorf("DryRunMirrorsRelease() = %q, want no message", got)
		}
	})

	// The accepted pair is still compared pin by pin: a Renovate update after
	// the one both fixtures were built from is a drift like any other.
	t.Run("a syft release the dry run does not follow", func(t *testing.T) {
		t.Parallel()

		want := nextRelease(t, syftOf(t, bumpedCI))

		drifted := replaceOnce(t, ciDryRun, "syft-version: "+syftOf(t, bumpedCI), "syft-version: "+want)

		got := check.DryRunMirrorsRelease([]byte(drifted), []byte(releaseDryRun))
		if !strings.Contains(strings.Join(got, "\n"), `with: syft-version is "`+want+`"`) {
			t.Errorf("DryRunMirrorsRelease() = %q, want a message containing %q", got, `with: syft-version is "`+want+`"`)
		}
	})

	t.Run("an action pin the release dry run does not follow", func(t *testing.T) {
		t.Parallel()

		// The dry run shares docker/metadata-action with the release, which the
		// check holds to one commit in both.
		drifted := driftedPin(t, releaseDryRun, "docker/metadata-action")

		got := check.DryRunMirrorsRelease([]byte(ciDryRun), []byte(drifted))
		if !strings.Contains(strings.Join(got, "\n"), "docker/metadata-action runs at") {
			t.Errorf("DryRunMirrorsRelease() = %q, want a message naming the drifted docker/metadata-action pin", got)
		}
	})

	t.Run("a syft release range", func(t *testing.T) {
		t.Parallel()

		ranged := replaceOnce(t, releaseFixture.render(), "syft-version: "+syftOf(t, bumpedRelease), "syft-version: ~> v1")

		got := check.ReleaseWorkflow([]byte(ranged))
		if !strings.Contains(strings.Join(got, "\n"), "syft-version") {
			t.Errorf("ReleaseWorkflow(a range in the updated release.yml's syft-version) = %q, want a message naming syft-version", got)
		}
	})

	// The bump is what holds the cases above to a Renovate update, so it is held
	// to the pins itself: it moves GoReleaser's version: and no other tool's,
	// and it leaves no action at the commit it found.
	t.Run("the bump moves GoReleaser's version and no other version:", func(t *testing.T) {
		t.Parallel()

		// The golangci-lint action's quoted version:, which only a goreleaser
		// distribution's version: can be mistaken for.
		doc := "      - uses: goreleaser/goreleaser-action@" + strings.Repeat("a", 40) + " # v7.3.0\n" +
			"        with:\n" +
			"          distribution: goreleaser\n" +
			"          version: \"v2.20.1\"\n" +
			"      - uses: golangci/golangci-lint-action@" + strings.Repeat("b", 40) + " # v9.3.0\n" +
			"        with:\n" +
			"          version: \"v2.15.0\"\n"

		bumped := string(bumpPins(t, []byte(doc)))

		if want := `version: "` + nextRelease(t, "v2.20.1") + `"`; !strings.Contains(bumped, want) {
			t.Errorf("bumpPins() holds no %s: the goreleaser distribution's version: is the GoReleaser pin:\n%s", want, bumped)
		}

		if unwanted := `version: "v2.15.0"`; !strings.Contains(bumped, unwanted) {
			t.Errorf("bumpPins() moved %s, which is not a goreleaser distribution's version::\n%s", unwanted, bumped)
		}
	})

	t.Run("the bump moves an action pinned at a range's comment", func(t *testing.T) {
		t.Parallel()

		// A comment that is no exact release: Renovate moves the commit and
		// leaves the comment, and no check of the fixtures may skip the pin
		// because of the comment's shape.
		commit := strings.Repeat("a", 40)
		doc := "      - uses: example/action@" + commit + " # v4\n"

		if got, want := string(bumpPins(t, []byte(doc))), "      - uses: example/action@"+nextCommit(t, commit)+" # v4\n"; got != want {
			t.Errorf("bumpPins() = %q, want %q: the commit moves, the range's comment stays", got, want)
		}
	})
}

// readWorkflows returns the repository's ci.yml and release.yml: the two
// documents the fixtures are built from, read once by this package
// (repoCI and repoRelease, workflowpins_test.go). The clones are the caller's,
// so that no case can change what the package's own fixtures were built from;
// the bump (bumpPins) returns new bytes and leaves neither document changed.
func readWorkflows(t *testing.T) (ci, release []byte) {
	t.Helper()

	return slices.Clone(repoCI), slices.Clone(repoRelease)
}

// renovateBump returns ci.yml and release.yml with a Renovate update applied to
// them in memory: every action at the next commit, the next release in the
// version comment of every pin that names one, and GoReleaser, cosign, syft and
// BuildKit at the next release. Every new value is derived from the one it
// replaces, so the two documents move together, as a grouped Renovate pull
// request moves them.
func renovateBump(t *testing.T, ci, release []byte) (bumpedCI, bumpedRelease []byte) {
	t.Helper()

	bumpedCI, bumpedRelease = bumpPins(t, ci), bumpPins(t, release)

	for _, doc := range []struct {
		name       string
		was, isNow []byte
	}{{"ci.yml", ci, bumpedCI}, {"release.yml", release, bumpedRelease}} {
		if string(doc.was) == string(doc.isNow) {
			t.Fatalf("the update changed nothing in %s, so the fixtures would be held to the pins they already copy", doc.name)
		}
	}

	return bumpedCI, bumpedRelease
}

// bumpPins returns doc with every pin this test follows moved on one update:
// every action's commit, the version comment of an action's pin that names an
// exact release, and the exact releases of GoReleaser, cosign, syft and
// BuildKit. A comment that is a range or a pre-release stays as Renovate leaves
// it, while the commit under it moves. Every uses: line doc holds must come out
// at another commit: a pattern that skipped one would let the checks below pass
// on a pin no update ever moved, and would let a fixture hard-code it.
func bumpPins(t *testing.T, doc []byte) []byte {
	t.Helper()

	bumped := usesPin.ReplaceAllStringFunc(string(doc), func(line string) string {
		m := usesPin.FindStringSubmatch(line)
		pin := m[1]

		at := strings.LastIndex(pin, "@")

		out := strings.Replace(line, pin, pin[:at+1]+nextCommit(t, pin[at+1:]), 1)
		if m[2] != "" && exactRelease.MatchString(m[2]) {
			out = strings.Replace(out, "# "+m[2], "# "+nextRelease(t, m[2]), 1)
		}

		return out
	})

	for _, line := range usesCommit.FindAllString(string(doc), -1) {
		commit := line[strings.LastIndex(line, "@")+1:]

		if strings.Contains(bumped, "@"+commit) {
			t.Fatalf("the update left %q at its commit: no pin of the document was bumped there, so the fixtures would be "+
				"held to a pin no update moves", strings.TrimSpace(line))
		}
	}

	for _, re := range []*regexp.Regexp{syftVersion, goreleaserVersion, cosignRelease} {
		bumped = re.ReplaceAllStringFunc(bumped, func(match string) string {
			old := re.FindStringSubmatch(match)[1]

			return strings.Replace(match, old, nextRelease(t, old), 1)
		})
	}

	return []byte(digestImage.ReplaceAllStringFunc(bumped, func(match string) string {
		tag, digest, _ := strings.Cut(match, "@sha256:")

		return nextRelease(t, tag) + "@sha256:" + nextCommit(t, digest)
	}))
}

// nextRelease returns another exact release than v: its next major, so that no
// updated value can be mistaken for the pin it replaces.
func nextRelease(t *testing.T, v string) string {
	t.Helper()

	major, _, found := strings.Cut(strings.TrimPrefix(v, "v"), ".")
	if !found {
		t.Fatalf("%q is not an exact release", v)
	}

	n, err := strconv.Atoi(major)
	if err != nil {
		t.Fatalf("%q is not an exact release: %v", v, err)
	}

	return "v" + strconv.Itoa(n+1) + ".0.0"
}

// nextCommit returns another commit or digest than commit, hex: the value one
// later, so that its shape, and that of the pin it sits in, stays the same.
func nextCommit(t *testing.T, commit string) string {
	t.Helper()

	digits := []byte(commit)

	for i := len(digits) - 1; i >= 0; i-- {
		switch digits[i] {
		case '9':
			digits[i] = 'a'

			return string(digits)
		case 'f':
			digits[i] = '0'
		default:
			digits[i]++

			return string(digits)
		}
	}

	t.Fatalf("%q is all f: no later commit of its length", commit)

	return ""
}

// syftOf returns the exact syft release doc pins.
func syftOf(t *testing.T, doc []byte) string {
	t.Helper()

	m := syftVersion.FindSubmatch(doc)
	if m == nil {
		t.Fatalf("the workflow holds no syft-version: %s", doc)
	}

	return string(m[1])
}

// replaceOnce returns doc with old replaced by new, failing unless doc holds
// old exactly once, so that a case cannot pass for the wrong reason.
func replaceOnce(t *testing.T, doc, old, new string) string {
	t.Helper()

	if got := strings.Count(doc, old); got != 1 {
		t.Fatalf("the fixture holds %q %d times, want exactly once", old, got)
	}

	return strings.Replace(doc, old, new, 1)
}

// driftedPin returns doc with every commit of action's pins moved on one
// commit: an action the fixtures share, updated in one document alone.
func driftedPin(t *testing.T, doc, action string) string {
	t.Helper()

	re := regexp.MustCompile(regexp.QuoteMeta(action) + `@([0-9a-f]{40})`)

	if !re.MatchString(doc) {
		t.Fatalf("the fixture holds no %s pin", action)
	}

	return re.ReplaceAllStringFunc(doc, func(pin string) string {
		return action + "@" + nextCommit(t, strings.TrimPrefix(pin, action+"@"))
	})
}

// assertPinsFollow fails unless every pin doc carries is one of source's: the
// action commits and their version comments, the exact releases of syft,
// GoReleaser and cosign, and the BuildKit image by release and digest. what
// names doc in a message, and source is the workflow doc was built from.
func assertPinsFollow(t *testing.T, what, doc, source string) {
	t.Helper()

	pinValues := func(text string) []string {
		var values []string

		for _, m := range usesPin.FindAllStringSubmatch(text, -1) {
			value := m[1]
			if m[2] != "" {
				value += " # " + m[2]
			}

			values = append(values, value)
		}

		return values
	}

	known := pinValues(source)

	for _, value := range pinValues(doc) {
		if !slices.Contains(known, value) {
			t.Errorf("%s pins the action %q, which the workflow it copies holds nowhere: the fixture must take the pin from that workflow", what, value)
		}
	}

	for _, re := range []*regexp.Regexp{syftVersion, goreleaserVersion, cosignRelease} {
		releases := func(text string) []string {
			var values []string

			for _, m := range re.FindAllStringSubmatch(text, -1) {
				values = append(values, m[1])
			}

			return values
		}

		gots := releases(doc)
		if len(gots) == 0 {
			continue
		}

		wants := releases(source)

		for _, got := range gots {
			if !slices.Contains(wants, got) {
				t.Errorf("%s pins %s, which the workflow it copies does not: the fixture must take the pin from that workflow", what, got)
			}
		}
	}

	wants := digestImage.FindAllString(source, -1)

	for _, got := range digestImage.FindAllString(doc, -1) {
		if !slices.Contains(wants, got) {
			t.Errorf("%s pins the image %s, which the workflow it copies does not: the fixture must take the pin from that workflow", what, got)
		}
	}
}
