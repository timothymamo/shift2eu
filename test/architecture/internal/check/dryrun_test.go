package check_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// The dry-run fixtures, built from the repository's workflows: the cases below
// change one part of them. ciDryRun takes ci.yml's pins and releaseDryRun
// release.yml's, so a Renovate update that moves a pin in one workflow alone
// fails the cases that compare the two on purpose: the workflows have drifted
// apart, and TestDryRunMirrorsTheRepositoryWorkflows names the drift.
var (
	ciDryRun      = ciDryRunFrom(repoCI)
	releaseDryRun = releaseDryRunFrom(repoRelease)
)

// ciDryRunFrom returns ciDryRunTemplate with the pins of ci, a ci.yml.
func ciDryRunFrom(ci []byte) string {
	return pinsOf(ci).expand(ciDryRunTemplate)
}

// releaseDryRunFrom returns releaseDryRunTemplate with the pins of release, a
// release.yml.
func releaseDryRunFrom(release []byte) string {
	return pinsOf(release).expand(releaseDryRunTemplate)
}

// ciDryRunTemplate is the image job of .github/workflows/ci.yml as far as the
// dry run checks it against release.yml: the same BuildKit, the same action
// pins, the release's latest and tag rules run on sample tags in place of the
// pushed one, the release's build settings written to an OCI archive, and the
// SBOM of the built image. Its pins are placeholders (workflowPins).
const ciDryRunTemplate = `name: CI

on:
  pull_request:

jobs:
  image:
    name: Container image
    runs-on: ubuntu-latest
    env:
      BUILDKIT_VERSION: {{image moby/buildkit}}
    steps:
      - uses: {{pin docker/setup-buildx-action}}
        with:
          driver-opts: image=moby/buildkit:${{ env.BUILDKIT_VERSION }}
      - name: Latest for sample tags, as release.yml decides it
        id: latest
        run: |
          # $1 is the pushed tag, the rest every tag merged into main.
          decide() {
            TAG="$1"
            shift
            highest="$(printf '%s\n' "$@" | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V | tail -n 1)"
            latest=false
            if [ "$TAG" = "$highest" ]; then latest=true; fi
            echo "$latest"
          }
          {
            echo "highest=$(decide v1.2.3 v1.1.0 v1.2.2 v1.2.3 v1.3.0-rc.1)"
          } >> "$GITHUB_OUTPUT"
      - name: Image tags for a sample release tag, the highest
        uses: {{pin docker/metadata-action}}
        with:
          images: ghcr.io/timothymamo/shift2eu
          flavor: latest=${{ steps.latest.outputs.highest }}
          tags: |
            type=semver,pattern={{raw}},value=v1.2.3
            type=semver,pattern={{major}}.{{minor}},value=v1.2.3,enable=${{ !contains('v1.2.3', '-') }}
      - name: Build linux/amd64 and linux/arm64
        uses: {{pin docker/build-push-action}}
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: false
          outputs: type=oci,dest=${{ runner.temp }}/shift2eu.oci.tar
          annotations: index:org.opencontainers.image.source=https://github.com/timothymamo/shift2eu
          provenance: false
          sbom: false
      - name: Load linux/amd64 for the smoke test
        uses: {{pin docker/build-push-action}}
        with:
          context: .
          platforms: linux/amd64
          load: true
      - name: SBOM of the built image
        uses: {{pin anchore/sbom-action}}
        with:
          image: oci-archive:${{ runner.temp }}/shift2eu.oci.tar
          format: spdx-json
          output-file: ${{ runner.temp }}/shift2eu.spdx.json
          syft-version: {{release syft}}
      # release.yml's verify job runs the published image the same way, so the
      # dry run runs the image it built the same way, on the runner alone.
      - name: Smoke test the CLI image, read-only root filesystem, no network
        run: |
          docker run --rm --read-only --network none "$IMAGE" version | tee version.txt
`

// releaseDryRunTemplate is the latest, image, sbom and verify jobs of
// .github/workflows/release.yml as far as the dry run checks them: the latest
// decision on the release tags on main, made once in a job of its own (issue
// #173) that the image jobs read, the BuildKit image by exact release and index
// digest, the pins, the tag rules on github.ref_name, the build settings, the
// SPDX SBOM with an exact syft, and the container run of each published image.
// Its pins are placeholders (workflowPins).
const releaseDryRunTemplate = `name: Release

on:
  push:
    tags: ["v*.*.*"]

jobs:
  latest:
    name: Latest
    needs: test
    runs-on: ubuntu-latest
    permissions:
      contents: read # check out the repository
    outputs:
      latest: ${{ steps.decide.outputs.latest }}
    steps:
      - uses: {{pin actions/checkout}}
        with:
          fetch-depth: 0
          persist-credentials: false
      - name: Decide whether the tag is the highest release tag on main
        id: decide
        env:
          TAG: ${{ github.ref_name }}
        run: |
          git rev-parse --verify --quiet refs/remotes/origin/main > /dev/null || {
            echo "origin/main is missing from the checkout" >&2
            exit 1
          }
          highest="$(git tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V | tail -n 1)"
          latest=false
          if [ "$TAG" = "$highest" ]; then latest=true; fi
          echo "latest=$latest" >> "$GITHUB_OUTPUT"
  image:
    name: Container image
    needs: [test, goreleaser, latest]
    runs-on: ubuntu-latest
    steps:
      - name: Build arguments, as GoReleaser stamps them
        id: stamp
        env:
          TAG: ${{ github.ref_name }}
        run: |
          {
            echo "version=${TAG#v}"
            echo "commit=$(git rev-parse --short HEAD)"
            echo "date=$(TZ=UTC git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ)"
          } >> "$GITHUB_OUTPUT"
      - uses: {{pin docker/setup-buildx-action}}
        with:
          driver-opts: image=moby/buildkit:{{image moby/buildkit}}
      - name: Image tags
        uses: {{pin docker/metadata-action}}
        with:
          images: ghcr.io/timothymamo/shift2eu
          flavor: latest=${{ needs.latest.outputs.latest }}
          tags: |
            type=semver,pattern={{raw}}
            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}
      - name: Build and push linux/amd64 and linux/arm64
        uses: {{pin docker/build-push-action}}
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          annotations: index:org.opencontainers.image.source=https://github.com/timothymamo/shift2eu
          provenance: false
          sbom: false
  sbom:
    name: SBOM
    runs-on: ubuntu-latest
    steps:
      - uses: {{pin anchore/sbom-action}}
        with:
          image: registry:ghcr.io/timothymamo/shift2eu@${{ needs.image.outputs.digest }}
          format: spdx-json
          output-file: shift2eu.spdx.json
          syft-version: {{release syft}}
  verify:
    name: Verify the published image
    runs-on: ubuntu-latest
    steps:
      - name: Verify the CLI image
        run: |
          DIGEST="$(docker buildx imagetools inspect "ghcr.io/timothymamo/shift2eu:$TAG" --format '{{.Manifest.Digest}}')"
          IMAGE="ghcr.io/timothymamo/shift2eu@$DIGEST"
          docker run --rm --read-only --network none "$IMAGE" version | tee version.txt
`

// mutate returns doc with old replaced by new, failing unless doc holds old
// exactly once, so that a case that mistypes the fixture cannot pass for the
// wrong reason.
func mutate(t *testing.T, doc, old, new string) string {
	t.Helper()

	if got := strings.Count(doc, old); got != 1 {
		t.Fatalf("the fixture holds %q %d times, want exactly once", old, got)
	}

	return strings.Replace(doc, old, new, 1)
}

// TestDryRunMirrorsRelease pins the review's finding of issue #72: ci.yml's
// image job is the dry run of release.yml's image and sbom jobs, the CLI
// image's, so it must
// fail the pull request when the release's build settings drift from it. What
// the two documents publish differ by design (push: true against an OCI
// archive, a real tag against a sample one, a registry image against an
// archive), so the check compares the fields the release's build and SBOM rest
// on: the latest decision too, whose step and whose highest-tag pipeline the
// dry run must have, on sample tags in place of the release's git tag -l. The
// release publishes the CLI's image alone, so a dry run that still builds,
// tags, reads or runs the HTTP API's image is refused (issue #393).
func TestDryRunMirrorsRelease(t *testing.T) {
	t.Parallel()

	// The pins the cases below drift, as the fixtures hold them, and the
	// values they drift to: each derived from the pin it replaces, so that a
	// Renovate update moves both and no case stops matching.
	syft := ciPins.expand("{{release syft}}")
	driftedSyft := nextRelease(t, syft)
	buildkit := ciPins.expand("{{image moby/buildkit}}")
	buildkitTag, buildkitDigest, _ := strings.Cut(buildkit, "@sha256:")
	driftedBuildkit := nextRelease(t, buildkitTag) + "@sha256:" + nextCommit(t, buildkitDigest)
	commit := func(action string) string { return ciPins.expand("{{commit " + action + "}}") }
	driftedCommit := func(action string) string { return action + "@" + nextCommit(t, ciPins.commitOf(action)) }

	tests := []struct {
		name    string
		ci      string
		release string
		// wantIn is a part of the message the check must return; "" means
		// none at all.
		wantIn string
	}{
		{name: "the accepted pair", ci: ciDryRun, release: releaseDryRun},
		{
			// The same pair with CRLF line ends, which the reader tolerates.
			name:    "the accepted pair with CRLF line ends",
			ci:      strings.ReplaceAll(ciDryRun, "\n", "\r\n"),
			release: strings.ReplaceAll(releaseDryRun, "\n", "\r\n"),
		},

		// The build: the release's platforms, annotations and BuildKit's own
		// provenance and SBOM settings, compared on the step that builds what
		// the release publishes (not the ci's second, single-platform load for
		// the smoke test).
		{name: "a drifted platform", ci: mutate(t, ciDryRun, "          platforms: linux/amd64,linux/arm64\n", "          platforms: linux/amd64\n"), release: releaseDryRun, wantIn: `with: platforms is "linux/amd64", release.yml's`},
		{name: "a drifted annotation", ci: mutate(t, ciDryRun, "https://github.com/timothymamo/shift2eu", "https://example.com"), release: releaseDryRun, wantIn: "with: annotations is"},
		{name: "a drifted provenance", ci: mutate(t, ciDryRun, "          provenance: false\n", "          provenance: true\n"), release: releaseDryRun, wantIn: `with: provenance is "true"`},
		{name: "a drifted sbom setting", ci: mutate(t, ciDryRun, "          sbom: false\n", "          sbom: true\n"), release: releaseDryRun, wantIn: `with: sbom is "true"`},

		// The tags: the release's rules with a sample tag in place of
		// github.ref_name, and the same image name.
		{
			name:    "a drifted tag rule",
			ci:      mutate(t, ciDryRun, "type=semver,pattern={{major}}.{{minor}},value=v1.2.3", "type=semver,pattern={{major}},value=v1.2.3"),
			release: releaseDryRun,
			wantIn:  "with: tags are",
		},
		{
			name:    "a tag rule the release does not have",
			ci:      mutate(t, ciDryRun, "            type=semver,pattern={{raw}},value=v1.2.3\n", "            type=semver,pattern={{raw}},value=v1.2.3\n            type=ref,event=branch\n"),
			release: releaseDryRun,
			wantIn:  "with: tags are",
		},
		{name: "a drifted image name", ci: mutate(t, ciDryRun, "          images: ghcr.io/timothymamo/shift2eu\n", "          images: ghcr.io/example/shift2eu\n"), release: releaseDryRun, wantIn: `with: images is "ghcr.io/example/shift2eu"`},

		// The SBOM: the release's format and exact syft.
		{name: "a drifted syft version", ci: mutate(t, ciDryRun, "syft-version: "+syft, "syft-version: "+driftedSyft), release: releaseDryRun, wantIn: `with: syft-version is "` + driftedSyft + `"`},
		{name: "a drifted SBOM format", ci: mutate(t, ciDryRun, "          format: spdx-json\n", "          format: cyclonedx-json\n"), release: releaseDryRun, wantIn: `with: format is "cyclonedx-json"`},

		// The pins: the same commit for every action the two share, and the
		// same BuildKit image by tag and digest.
		{name: "a drifted metadata-action pin", ci: mutate(t, ciDryRun, commit("docker/metadata-action"), driftedCommit("docker/metadata-action")), release: releaseDryRun, wantIn: "docker/metadata-action runs at"},
		{
			name:    "a drifted build-push-action pin",
			ci:      ciDryRun,
			release: mutate(t, releaseDryRun, commit("docker/build-push-action"), driftedCommit("docker/build-push-action")),
			wantIn:  "docker/build-push-action runs at " + ciPins.commitOf("docker/build-push-action") + " (line 39) and at " + nextCommit(t, ciPins.commitOf("docker/build-push-action")),
		},
		{name: "a drifted sbom-action pin", ci: mutate(t, ciDryRun, commit("anchore/sbom-action"), driftedCommit("anchore/sbom-action")), release: releaseDryRun, wantIn: "anchore/sbom-action runs at"},
		{name: "a drifted setup-buildx-action pin", ci: ciDryRun, release: mutate(t, releaseDryRun, commit("docker/setup-buildx-action"), driftedCommit("docker/setup-buildx-action")), wantIn: "docker/setup-buildx-action runs at"},
		{name: "a drifted BuildKit image", ci: mutate(t, ciDryRun, buildkit, driftedBuildkit), release: releaseDryRun, wantIn: "the dry run must build with the release's moby/buildkit tag and digest"},

		// The one build the dry run compares is the one that does not load the
		// image: none left, or a second one, is refused rather than guessed at.
		{
			name:    "a multi-platform build that loads the image too",
			ci:      mutate(t, ciDryRun, "          outputs: type=oci,dest=${{ runner.temp }}/shift2eu.oci.tar\n", "          load: true\n          outputs: type=oci,dest=${{ runner.temp }}/shift2eu.oci.tar\n"),
			release: releaseDryRun,
			wantIn:  "load: true left out, for target: none, holds 0 docker/build-push-action steps, want exactly 1",
		},
		{
			name:    "a second build that does not load the image",
			ci:      mutate(t, ciDryRun, "      - name: Load linux/amd64 for the smoke test\n", "      - name: Build once more\n        uses: "+ciPins.expand("{{pin docker/build-push-action}}")+"\n        with:\n          context: .\n          platforms: linux/amd64,linux/arm64\n          push: false\n          outputs: type=oci,dest=${{ runner.temp }}/shift2eu-2.oci.tar\n      - name: Load linux/amd64 for the smoke test\n"),
			release: releaseDryRun,
			wantIn:  "load: true left out, for target: none, holds 2 docker/build-push-action steps, want exactly 1",
		},
		{
			// The load: true step is the dry run's own smoke test, not part of
			// what the release publishes: leaving it out leaves the one build.
			name:    "the dry run without its smoke-test load",
			ci:      mutate(t, ciDryRun, "      - name: Load linux/amd64 for the smoke test\n        uses: "+ciPins.expand("{{pin docker/build-push-action}}")+"\n        with:\n          context: .\n          platforms: linux/amd64\n          load: true\n", ""),
			release: releaseDryRun,
		},

		// The sample tag the dry run's tag rules must carry for the release's
		// github.ref_name rules to be compared at all.
		{
			name:    "a metadata step without a sample tag",
			ci:      mutate(t, ciDryRun, "            type=semver,pattern={{raw}},value=v1.2.3\n            type=semver,pattern={{major}}.{{minor}},value=v1.2.3,enable=${{ !contains('v1.2.3', '-') }}\n", "            type=semver,pattern={{raw}}\n            type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}\n"),
			release: releaseDryRun,
			wantIn:  "holds no ,value=<sample>",
		},
		{
			name:    "a metadata step with an empty sample tag",
			ci:      mutate(t, ciDryRun, "            type=semver,pattern={{raw}},value=v1.2.3\n", "            type=semver,pattern={{raw}},value=\n"),
			release: releaseDryRun,
			wantIn:  "holds an empty ,value=",
		},
		{
			name:    "a dry run without the metadata-action step",
			ci:      mutate(t, ciDryRun, "      - name: Image tags for a sample release tag, the highest\n        uses: "+ciPins.expand("{{pin docker/metadata-action}}")+"\n        with:\n          images: ghcr.io/timothymamo/shift2eu\n          flavor: latest=${{ steps.latest.outputs.highest }}\n          tags: |\n            type=semver,pattern={{raw}},value=v1.2.3\n            type=semver,pattern={{major}}.{{minor}},value=v1.2.3,enable=${{ !contains('v1.2.3', '-') }}\n", ""),
			release: releaseDryRun,
			wantIn:  "so the dry run checks none of the release's tags",
		},

		// The latest decision: the release makes it once, in the latest job, and
		// its image jobs read that job's output (issue #173); the dry run makes it
		// in a step of its own, id: latest, whose outputs its metadata-action
		// steps read. A flavor: that reads anything else, a literal or another
		// step, is refused, and so is one that is not the release's shape.
		{name: "a flavor of latest=auto", ci: mutate(t, ciDryRun, "          flavor: latest=${{ steps.latest.outputs.highest }}\n", "          flavor: latest=auto\n"), release: releaseDryRun, wantIn: "the dry run must tag latest as the release does"},
		{name: "a flavor that is always latest", ci: mutate(t, ciDryRun, "          flavor: latest=${{ steps.latest.outputs.highest }}\n", "          flavor: latest=true\n"), release: releaseDryRun, wantIn: "the dry run must tag latest as the release does"},
		{name: "a flavor read from a step that is not the latest step", ci: mutate(t, ciDryRun, "          flavor: latest=${{ steps.latest.outputs.highest }}\n", "          flavor: latest=${{ steps.stamp.outputs.latest }}\n"), release: releaseDryRun, wantIn: "the dry run must tag latest as the release does"},
		{
			// The decision is made in the latest job, so the image job's flavor:
			// reads that job's output and never a step of its own.
			name:    "a release flavor read from a step of its image job",
			ci:      ciDryRun,
			release: mutate(t, releaseDryRun, "          flavor: latest=${{ needs.latest.outputs.latest }}\n", "          flavor: latest=${{ steps.stamp.outputs.latest }}\n"),
			wantIn:  `want exactly "latest=${{ needs.latest.outputs.latest }}"`,
		},

		// The pipeline that picks latest: the release's latest job takes the
		// highest release tag merged into main, in a decide step of its own
		// (issue #173), with its git tag -l 'v*.*.*' --merged
		// refs/remotes/origin/main in place of the dry run's printf '%s\n'
		// "$@". Everything after the source, the sed, the sort and the tail,
		// must be the release's.
		{
			// The decision is no step of the image job's any more: a release
			// without the latest job is refused rather than read from there.
			name:    "a release without a latest job",
			ci:      ciDryRun,
			release: mutate(t, releaseDryRun, "  latest:\n", "  decision:\n"),
			wantIn:  "release.yml: no jobs: latest",
		},
		{
			name:    "a release whose latest job has no decide step",
			ci:      ciDryRun,
			release: mutate(t, releaseDryRun, "        id: decide\n", "        id: decision\n"),
			wantIn:  `release.yml's latest job holds 0 run: steps with id: decide, want exactly 1`,
		},
		{name: "a latest rule without the release regex", ci: mutate(t, ciDryRun, `'/^v[0-9]+\.[0-9]+\.[0-9]+$/p'`, `'/^v[0-9]+\.[0-9]+\.[0-9]+$/p;/^latest$/p'`), release: releaseDryRun, wantIn: "the dry run must pick the highest tag the release picks"},
		{name: "a latest rule that sorts like the shell", ci: mutate(t, ciDryRun, "| sort -V |", "| sort |"), release: releaseDryRun, wantIn: "the dry run must pick the highest tag the release picks"},
		{name: "a latest rule that takes another line", ci: mutate(t, ciDryRun, "| tail -n 1)", "| tail -n 2)"), release: releaseDryRun, wantIn: "the dry run must pick the highest tag the release picks"},
		{name: "a release that counts every tag", ci: ciDryRun, release: mutate(t, releaseDryRun, "git tag -l 'v*.*.*' --merged refs/remotes/origin/main", "git tag -l 'v*.*.*'"), wantIn: "the dry run must pick the highest tag the release picks"},
		{name: "a release that takes another line", ci: ciDryRun, release: mutate(t, releaseDryRun, "| tail -n 1)", "| tail -n 2)"), wantIn: "the dry run must pick the highest tag the release picks"},

		// The latest script opens with a comment line. The reader of the dry
		// run's script takes such a line at exactly the block's indentation as
		// the block's first line, and keeps it; the release's reader, which
		// reads the stamp's script, still refuses one. A comment line at any
		// other indentation, a rule left out or repeated behind the comment, is
		// caught as before: the comment must not hide the rule it describes.
		{
			name:    "a second comment line in the latest script",
			ci:      mutate(t, ciDryRun, "          # $1 is the pushed tag, the rest every tag merged into main.\n", "          # $1 is the pushed tag, the rest every tag merged into main.\n          # The highest release tag on main is the latest one.\n"),
			release: releaseDryRun,
		},
		{
			name:    "a comment line in the latest script indented deeper than its first line",
			ci:      mutate(t, ciDryRun, "          # $1 is the pushed tag, the rest every tag merged into main.\n", "            # $1 is the pushed tag, the rest every tag merged into main.\n"),
			release: releaseDryRun,
			wantIn:  "run has a line before its first content line",
		},
		{
			name:    "a comment line in the latest script indented less than its first line",
			ci:      mutate(t, ciDryRun, "          # $1 is the pushed tag, the rest every tag merged into main.\n", "        # $1 is the pushed tag, the rest every tag merged into main.\n"),
			release: releaseDryRun,
			wantIn:  "run has a line before its first content line",
		},
		{
			name:    "a second highest rule in the latest script after its comment",
			ci:      mutate(t, ciDryRun, "          # $1 is the pushed tag, the rest every tag merged into main.\n", "          # $1 is the pushed tag, the rest every tag merged into main.\n          highest=\"$(printf '%s\\n' \"$@\" | sort | tail -n 1)\"\n"),
			release: releaseDryRun,
			wantIn:  `holds 2 lines starting highest="$(, want exactly 1`,
		},
		{
			name:    "a highest rule commented out",
			ci:      mutate(t, ciDryRun, "            highest=\"$(printf", "            # highest=\"$(printf"),
			release: releaseDryRun,
			wantIn:  `holds 0 lines starting highest="$(, want exactly 1`,
		},
		{
			// A commented-out rule is text inside the block, not the rule the
			// check compares: the live one beside it is still the release's.
			name:    "a commented-out highest rule in the latest script",
			ci:      mutate(t, ciDryRun, "          # $1 is the pushed tag, the rest every tag merged into main.\n", "          # $1 is the pushed tag, the rest every tag merged into main.\n          # highest=\"$(printf '%s\\n' \"$@\" | sort | tail -n 1)\"\n"),
			release: releaseDryRun,
		},

		// The with: keys the build and the SBOM are compared on, and the steps
		// themselves: a key or a step the dry run does not have is named.
		{name: "a build step without platforms", ci: mutate(t, ciDryRun, "          platforms: linux/amd64,linux/arm64\n          push: false\n", "          push: false\n"), release: releaseDryRun, wantIn: "holds no with: platforms"},
		{name: "a metadata step without images", ci: mutate(t, ciDryRun, "          images: ghcr.io/timothymamo/shift2eu\n", ""), release: releaseDryRun, wantIn: "holds no with: images"},

		// The BuildKit image comes from the job's env:, which the check resolves
		// and refuses when it cannot.
		{
			name:    "a BuildKit expression that is not an env variable",
			ci:      mutate(t, ciDryRun, "          driver-opts: image=moby/buildkit:${{ env.BUILDKIT_VERSION }}\n", "          driver-opts: image=moby/buildkit:${{ github.sha }}\n"),
			release: releaseDryRun,
			wantIn:  `the expression "github.sha" is not an env. variable`,
		},
		{
			name:    "a BuildKit expression the job env does not hold",
			ci:      mutate(t, ciDryRun, "${{ env.BUILDKIT_VERSION }}", "${{ env.BUILDKIT }}"),
			release: releaseDryRun,
			wantIn:  "the job's env: holds no BUILDKIT",
		},
		{
			name:    "a job without the env the BuildKit image is in",
			ci:      mutate(t, ciDryRun, "    env:\n      BUILDKIT_VERSION: "+buildkit+"\n", ""),
			release: releaseDryRun,
			wantIn:  "the job's env: holds no BUILDKIT_VERSION",
		},
		{name: "an env variable whose name the reader does not follow", ci: mutate(t, ciDryRun, "      BUILDKIT_VERSION:", "      buildkit_version:"), release: releaseDryRun, wantIn: "is not one variable this reader understands"},

		// The HTTP API's image is gone with the server (issue #393): the dry run
		// builds, tags and reads the CLI's image alone. A dry run that still
		// tags the server's image is refused, since the release publishes no
		// such image to dry-run.
		{
			name:    "a dry run that still tags the HTTP API's image",
			ci:      mutate(t, ciDryRun, "      - name: Build linux/amd64 and linux/arm64\n", "      - name: Image tags for the server's image\n        uses: "+ciPins.expand("{{pin docker/metadata-action}}")+"\n        with:\n          images: ghcr.io/timothymamo/shift2eu-server\n          flavor: latest=${{ steps.latest.outputs.highest }}\n          tags: |\n            type=semver,pattern={{raw}},value=v1.2.3\n      - name: Build linux/amd64 and linux/arm64\n"),
			release: releaseDryRun,
			wantIn:  `with: images is "ghcr.io/timothymamo/shift2eu-server"`,
		},
		// The dry run is the release's, so no line of its image job, a comment
		// included, may name a retired name either (issue #393), in any case.
		{
			name:    "a dry run whose image job comments on the HTTP API's binary",
			ci:      mutate(t, ciDryRun, "jobs:\n  image:\n", "jobs:\n  image:\n    # The Shift2eu-Server image is gone (issue #393).\n"),
			release: releaseDryRun,
			wantIn:  "names shift2eu-server",
		},
		{
			name:    "a dry run whose image job comments on the bundled OpenAPI document",
			ci:      mutate(t, ciDryRun, "jobs:\n  image:\n", "jobs:\n  image:\n    # The OpenAPI document is gone (issue #393).\n"),
			release: releaseDryRun,
			wantIn:  "names openapi",
		},

		// The release's verify job (issue #99) is the independent check of what a
		// release published. A pull request has no published image to verify, so
		// the dry run mirrors the part that needs none of it: the container run of
		// the image, with the release's flags and the release's command, on the
		// image it built itself.
		{
			name:    "a dry run that runs the CLI image without --network none",
			ci:      mutate(t, ciDryRun, `docker run --rm --read-only --network none "$IMAGE" version`, `docker run --rm --read-only "$IMAGE" version`),
			release: releaseDryRun,
			wantIn:  "the dry run must run the image as release.yml's verify job runs it",
		},
		{
			name:    "a dry run that runs the CLI image without --read-only",
			ci:      mutate(t, ciDryRun, `docker run --rm --read-only --network none "$IMAGE" version`, `docker run --rm --network none "$IMAGE" version`),
			release: releaseDryRun,
			wantIn:  "the dry run must run the image as release.yml's verify job runs it",
		},
		{
			name:    "a dry run that runs the CLI image's assess in place of version",
			ci:      mutate(t, ciDryRun, `--network none "$IMAGE" version`, `--network none "$IMAGE" assess`),
			release: releaseDryRun,
			wantIn:  "the dry run must run the image as release.yml's verify job runs it",
		},
		{
			name:    "a release whose verify job runs the CLI image without --network none",
			ci:      ciDryRun,
			release: mutate(t, releaseDryRun, `docker run --rm --read-only --network none "$IMAGE" version`, `docker run --rm --read-only "$IMAGE" version`),
			wantIn:  "the dry run must run the image as release.yml's verify job runs it",
		},
		{
			name:    "a release whose verify job runs no image",
			ci:      ciDryRun,
			release: mutate(t, releaseDryRun, `docker run --rm --read-only --network none "$IMAGE" version`, `true`),
			wantIn:  "runs no container run",
		},
		{
			// A docker run line the release continues onto a second line is
			// refused rather than compared: this check reads one line, so it
			// would compare the flags before the continuation alone, and in
			// ci.yml's image job a line ending so does exist, so the first line
			// alone can match a run that drops everything after it.
			name:    "a release whose verify job continues its docker run line",
			ci:      ciDryRun,
			release: mutate(t, releaseDryRun, `docker run --rm --read-only --network none "$IMAGE" version | tee version.txt`, "docker run --rm --read-only --network none \\\n            \"$IMAGE\" version | tee version.txt"),
			wantIn:  "continues its docker run line onto another line",
		},
		{
			name:    "a release whose verify job names the image twice",
			ci:      ciDryRun,
			release: mutate(t, releaseDryRun, "      - name: Verify the CLI image\n", "      - name: Verify the CLI image again\n        run: |\n          DIGEST=\"$(docker buildx imagetools inspect \"ghcr.io/timothymamo/shift2eu:$TAG\" --format '{{.Manifest.Digest}}')\"\n      - name: Verify the CLI image\n"),
			wantIn:  "whose run: names ghcr.io/timothymamo/shift2eu",
		},
		{name: "a release.yml without a verify job", ci: ciDryRun, release: mutate(t, releaseDryRun, "  verify:\n", "  publishing:\n"), wantIn: "release.yml: no jobs: verify"},

		// The jobs the two documents must hold to be compared at all.
		{name: "a ci.yml without an image job", ci: mutate(t, ciDryRun, "jobs:\n  image:\n", "jobs:\n  images:\n"), release: releaseDryRun, wantIn: "ci.yml: no jobs: image"},
		{name: "a release.yml without an sbom job", ci: ciDryRun, release: mutate(t, releaseDryRun, "  sbom:\n", "  provenance:\n"), wantIn: "release.yml: no jobs: sbom"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := check.DryRunMirrorsRelease([]byte(tc.ci), []byte(tc.release))

			if tc.wantIn == "" {
				if len(got) != 0 {
					t.Errorf("DryRunMirrorsRelease() = %q, want no message", got)
				}

				return
			}

			if !strings.Contains(strings.Join(got, "\n"), tc.wantIn) {
				t.Errorf("DryRunMirrorsRelease() = %q, want a message containing %q", got, tc.wantIn)
			}
		})
	}
}

// TestDryRunMirrorsTheRepositoryWorkflows pins the same pair the repository
// runs: the real ci.yml and release.yml must not have drifted apart.
func TestDryRunMirrorsTheRepositoryWorkflows(t *testing.T) {
	t.Parallel()

	for _, message := range check.DryRunMirrorsRelease(repoCI, repoRelease) {
		t.Error(message)
	}
}
