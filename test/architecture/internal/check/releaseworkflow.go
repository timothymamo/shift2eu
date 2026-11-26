package check

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// The release workflow is read as a closed schema: every key, job and step it
// may hold is listed below, with the value it must have. A pin a Renovate
// update changes (an action's commit, goreleaser's, cosign's and syft's
// versions, the BuildKit image) is matched by form, everything else exactly.
var (
	// actionPin matches the commit an action is pinned to.
	actionPin = `@[0-9a-f]{40}$`

	checkoutAction         = regexp.MustCompile(`^actions/checkout` + actionPin)
	setupGoAction          = regexp.MustCompile(`^actions/setup-go` + actionPin)
	setupNodeAction        = regexp.MustCompile(`^actions/setup-node` + actionPin)
	goreleaserAction       = regexp.MustCompile(`^goreleaser/goreleaser-action` + actionPin)
	setupBuildxAction      = regexp.MustCompile(`^docker/setup-buildx-action` + actionPin)
	loginAction            = regexp.MustCompile(`^docker/login-action` + actionPin)
	metadataAction         = regexp.MustCompile(`^docker/metadata-action` + actionPin)
	buildPushAction        = regexp.MustCompile(`^docker/build-push-action` + actionPin)
	sbomAction             = regexp.MustCompile(`^anchore/sbom-action` + actionPin)
	uploadArtifactAction   = regexp.MustCompile(`^actions/upload-artifact` + actionPin)
	cosignInstallerAction  = regexp.MustCompile(`^sigstore/cosign-installer` + actionPin)
	downloadArtifactAction = regexp.MustCompile(`^actions/download-artifact` + actionPin)
	attestAction           = regexp.MustCompile(`^actions/attest` + actionPin)
	provenanceAction       = regexp.MustCompile(`^actions/attest-build-provenance` + actionPin)

	// exactRelease matches one exact release of a tool: goreleaser, cosign,
	// syft.
	exactRelease = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

	// buildkitImage matches the BuildKit image setup-buildx-action runs,
	// pinned by an exact release and the digest of its multi-arch index.
	buildkitImage = regexp.MustCompile(`^image=moby/buildkit:v[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}$`)
)

// releaseStep is one step a job of the release workflow must run. The keys
// the step holds are exactly those whose field is set.
type releaseStep struct {
	// what names the step in a message.
	what string
	// name and id are the step's name: and id:, "" for none.
	name, id string
	// uses matches the action the step runs, nil for a run: step.
	uses *regexp.Regexp
	// with is the step's whole with: mapping, each key with what its value
	// must match; nil for none. A literal block value (|) is matched as its
	// lines joined by newlines.
	with map[string]*regexp.Regexp
	// env is the step's whole env: mapping, line by line; nil for none.
	env []string
	// run is the step's command, a literal block's lines joined by newlines;
	// "" for none.
	run string
	// noToken refuses every line of the step that names the job's token,
	// GITHUB_TOKEN or GH_TOKEN, in any case: the step that holds the site's
	// key holds nothing that could write to the repository.
	noToken bool
	// runExpressions lets run hold the ${{ }} expressions it lists, compared
	// exactly like the rest of it; false refuses every expression in run.
	// Only the scheduled live check sets it (SiteCheckWorkflow), never the
	// release workflow.
	runExpressions bool
}

// exactly matches value and nothing else.
func exactly(value string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(value) + `$`)
}

// releaseImage is one container image the release publishes, by three jobs of
// its own: one builds and pushes it, one reads its SBOM, one signs and attests
// it. Each image is held to the same steps; only these values differ.
type releaseImage struct {
	// name is the image's repository in the registry.
	name string
	// target is the Dockerfile stage it is built from, "" for the last one.
	target string
	// imageJob, sbomJob and signJob are the keys of its three jobs, and
	// suffix what their names add to the CLI image's job names.
	imageJob, sbomJob, signJob, suffix string
	// sbomFile is the SPDX document of the image, and sbomArtifact the name
	// of the workflow artifact the SBOM job hands it to the sign job under.
	sbomFile, sbomArtifact string
	// ciVar is the variable ci.yml's image job names the image it built for
	// this one by, so the dry run's container runs mirror each image's own.
	ciVar string
}

// digest is the expression through which the image's SBOM and sign jobs read
// the digest its image job built, to read, sign and attest the image by.
func (img releaseImage) digest() string {
	return "${{ needs." + img.imageJob + ".outputs.digest }}"
}

// The image the release publishes: the CLI's, from the Dockerfile's last stage
// (ADR 0013, ADR 0015). It is the only one: the HTTP API's image, sbom and
// sign jobs are gone with the server (issue #393), and a release document that
// still names them is refused (retiredNames).
var (
	cliImage = releaseImage{
		name: "ghcr.io/timothymamo/shift2eu", imageJob: "image", sbomJob: "sbom", signJob: "sign",
		sbomFile: "shift2eu.spdx.json", sbomArtifact: "sbom", ciVar: "IMAGE",
	}
	releaseImages = []releaseImage{cliImage}
)

// The expression of the digest an image job builds and gives as its output,
// and the registry every job logs in to.
const (
	builtDigest   = "${{ steps.build.outputs.digest }}"
	registryLogin = "ghcr.io"
)

var (
	// setupGoStep is setup-go with its Actions cache off: the cache would
	// restore GOMODCACHE and GOCACHE, which the go command trusts without
	// verifying them again, from an earlier job.
	setupGoStep = releaseStep{
		what: "actions/setup-go", uses: setupGoAction,
		with: map[string]*regexp.Regexp{"go-version-file": exactly("go.mod"), "cache": exactly("false")},
	}

	// fullCheckoutStep checks out the whole history without keeping the token.
	fullCheckoutStep = releaseStep{
		what: "actions/checkout", uses: checkoutAction,
		with: map[string]*regexp.Regexp{"fetch-depth": exactly("0"), "persist-credentials": exactly("false")},
	}

	// loginStep logs in to GHCR with the job's own token.
	loginStep = releaseStep{
		what: "docker/login-action", uses: loginAction,
		with: map[string]*regexp.Regexp{
			"registry": exactly(registryLogin),
			"username": exactly("${{ github.actor }}"),
			"password": exactly("${{ secrets.GITHUB_TOKEN }}"),
		},
	}

	// testSteps are the test job's steps: the whole history, the guard that
	// the tagged commit is on main (ADR 0015) before anything runs repository
	// code, Go, the Node release dev/lima.yaml pins for
	// TestPiEnvDenyExtensionLoads (issue #548), set up and checked by the same
	// steps as ci.yml's Test job (nodeSteps), and the tests, on a runner of
	// their own, with
	// SHIFT2EU_REQUIRE_NODE=1 so that check fails rather than skips.
	testSteps = append([]releaseStep{
		fullCheckoutStep,
		{
			what: "the guard that the tagged commit is on main", name: "The tagged commit is on main",
			run: strings.Join([]string{
				`git rev-parse --verify --quiet refs/remotes/origin/main > /dev/null || {`,
				`  echo "origin/main is missing from the checkout" >&2`,
				`  exit 1`,
				`}`,
				`git merge-base --is-ancestor "$GITHUB_SHA" refs/remotes/origin/main || {`,
				`  echo "$GITHUB_SHA is not on main: nothing is released" >&2`,
				`  exit 1`,
				`}`,
			}, "\n"),
		},
		setupGoStep,
		// The four Node steps, declared once for this job and ci.yml's
		// (nodeSteps, citestnode.go, issue #554).
	}, nodeSteps(".github/workflows/release.yml", "Test", "make test")...)

	// goreleaserSteps are the goreleaser job's steps: no repository code runs
	// before goreleaser, which is asked for exactly release --clean; the site
	// job's bundle is downloaded into bin/ (git-ignored) and checked against
	// that job's digest before goreleaser lists it in checksums.txt and
	// uploads it to the draft (.goreleaser.yaml's extra_files, PRD #297
	// milestone 3); and the archives and checksums.txt leave the job as a
	// workflow artifact for the attest-archives and publish jobs, kept a week
	// so a failed attestation or publication is recovered by re-running the
	// failed jobs (issue #163). Nothing is copied into dist/ beside them, and
	// the bundle is not handed on in that artifact: the attest-archives job
	// downloads the site job's own.
	goreleaserSteps = []releaseStep{
		fullCheckoutStep,
		setupGoStep,
		siteDownloadStep(siteBundleDir),
		{
			what: "the check of the site bundle against the site job's digest",
			name: "Check the site bundle against the site job's digest",
			env:  siteDigestEnv,
			run:  `printf '%s  %s\n' "$SITE_SHA256" "bin/shift2eu-site_$TAG.tar.gz" | sha256sum -c -`,
		},
		{
			what: "goreleaser/goreleaser-action", uses: goreleaserAction,
			with: map[string]*regexp.Regexp{
				"distribution": exactly("goreleaser"),
				"version":      exactRelease,
				"args":         exactly("release --clean"),
			},
			env: []string{"GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}"},
		},
		{
			what: "actions/upload-artifact (the archives)", uses: uploadArtifactAction,
			with: map[string]*regexp.Regexp{
				"name":              exactly(archivesArtifact),
				"path":              exactly("dist/" + strings.Join(handedOnArchives, "\ndist/")),
				"if-no-files-found": exactly("error"),
				"retention-days":    exactly("7"),
			},
		},
	}
)

// The release's archives and checksums.txt: the workflow artifact that hands
// them from the goreleaser job to the attest-archives and publish jobs, and the
// patterns of the files that job attests, which verify-archives verifies on the
// published release (issues #139, #163).
const archivesArtifact = "archives"

// handedOnArchives are the files the goreleaser job hands on in the archives
// artifact, each archive named by its extension: a bare shift2eu_* in dist/
// also matches GoReleaser's build directories (dist/shift2eu_linux_amd64_v1/
// ...), and would hand on and attest the bare binaries beside the archives.
// The HTTP API's archive and its OpenAPI document are gone (issue #393), and
// the site's bundle is not among them: it travels in the site job's own
// artifact.
var handedOnArchives = []string{
	"shift2eu_*.tar.gz", "shift2eu_*.zip",
	"checksums.txt",
}

// attestedSubjects are the files the attest-archives job attests: the CLI's
// archives, the site's bundle (PRD #297 milestone 3) and checksums.txt, which
// lists them all.
var attestedSubjects = []string{
	"shift2eu_*.tar.gz", "shift2eu_*.zip",
	siteBundlePattern,
	"checksums.txt",
}

// The site's bundle as the release handles it (PRD #297 milestone 3): the
// workflow artifact the site job hands it to the jobs of its run under, the
// pattern of its archive's name, the directory the jobs that build or check it
// keep it in, and the directory the smoke test unpacks it into.
const (
	siteArtifact      = "site"
	siteBundlePattern = "shift2eu-site_*.tar.gz"
	siteBundleDir     = "bin"
	siteUnpackedDir   = "bin/release-bundle"
)

// siteDigestEnv is the env: of a step that checks the site's bundle: the tag,
// which names the archive, and the digest the site job gave it, never one from
// another job.
var siteDigestEnv = []string{tagEnv, "SITE_SHA256: ${{ needs.site.outputs.sha256 }}"}

// siteDownloadStep is GitHub's own download of the site job's bundle, into
// path, or into the workspace for "".
func siteDownloadStep(path string) releaseStep {
	with := map[string]*regexp.Regexp{"name": exactly(siteArtifact)}
	if path != "" {
		with["path"] = exactly(path)
	}

	return releaseStep{what: "actions/download-artifact (the site bundle)", uses: downloadArtifactAction, with: with}
}

// shallowCheckoutStep checks out the tag's own commit alone, without keeping
// the token: what a job that reads no history needs.
var shallowCheckoutStep = releaseStep{
	what: "actions/checkout", uses: checkoutAction,
	with: map[string]*regexp.Regexp{"persist-credentials": exactly("false")},
}

// siteSteps are the site job's steps (PRD #297 milestone 3): the tag's commit,
// Go without the Actions cache, make site with the tag as the bundle's version
// through env:, the archive's sha256 as the job's output, and the archive as a
// workflow artifact for the jobs of this run alone, kept a week: the deploy
// and the live check read the published release, never this artifact.
var siteSteps = []releaseStep{
	shallowCheckoutStep,
	setupGoStep,
	{
		what: "make site", name: "Build the site's bundle",
		env: []string{"SITE_VERSION: ${{ github.ref_name }}"}, run: "make site",
	},
	{
		what: "the bundle's digest", name: "The bundle's digest", id: "digest", env: []string{tagEnv},
		run: strings.Join([]string{
			strictShell,
			`sha256="$(sha256sum "bin/shift2eu-site_$TAG.tar.gz" | cut -d' ' -f1)"`,
			`echo "sha256=$sha256" >> "$GITHUB_OUTPUT"`,
		}, "\n"),
	},
	{
		what: "actions/upload-artifact (the site bundle)", uses: uploadArtifactAction,
		with: map[string]*regexp.Regexp{
			"name":              exactly(siteArtifact),
			"path":              exactly(siteBundleDir + "/" + siteBundlePattern),
			"if-no-files-found": exactly("error"),
			"retention-days":    exactly("7"),
		},
	},
}

// siteBrowserSteps are the site-browser job's steps (PRD #297 milestone 3):
// the repository and Go for make, the site job's artifact, its check against
// that job's digest before it is unpacked into bin/release-bundle, and make
// test-browser on exactly that directory, so the bundle the release attests
// and publishes is the one smoke-tested, never one the recipe builds itself.
var siteBrowserSteps = []releaseStep{
	shallowCheckoutStep,
	setupGoStep,
	siteDownloadStep(siteBundleDir),
	{
		what: "the check and unpacking of the site bundle", name: "Check and unpack the bundle", env: siteDigestEnv,
		run: strings.Join([]string{
			strictShell,
			`printf '%s  %s\n' "$SITE_SHA256" "bin/shift2eu-site_$TAG.tar.gz" | sha256sum -c -`,
			"mkdir " + siteUnpackedDir,
			`tar -xzf "bin/shift2eu-site_$TAG.tar.gz" -C ` + siteUnpackedDir,
		}, "\n"),
	},
	{
		what: "make test-browser of the release's bundle", name: "Smoke test the release's bundle in a headless Chromium",
		run: siteBrowserRun,
	},
}

// siteBrowserRun is the release's smoke test of its own bundle, which the dry
// run holds ci.yml's browser job to mirror.
const siteBrowserRun = "make test-browser TEST_BROWSER_BUNDLE=" + siteUnpackedDir

// attestArchivesSteps are the attest-archives job's steps: GitHub's own
// download of the archives' workflow artifact and of the site job's bundle
// beside it, the check of every file against checksums.txt, with checksums.txt
// held to exactly one site bundle carrying the site job's digest (so an
// extra_files glob that matched nothing fails too), and one build provenance
// attestation of every archive, of the bundle and of checksums.txt itself, by
// path, so each file is a subject.
var attestArchivesSteps = []releaseStep{
	{
		what: "actions/download-artifact (the archives)", uses: downloadArtifactAction,
		with: map[string]*regexp.Regexp{"name": exactly(archivesArtifact)},
	},
	siteDownloadStep(""),
	{
		what: "the check of the archives and the site bundle against checksums.txt",
		name: "Check the archives and the site bundle against checksums.txt", env: siteDigestEnv,
		run: strings.Join([]string{
			strictShell,
			"sha256sum -c checksums.txt",
			`grep -qxF "$SITE_SHA256  shift2eu-site_$TAG.tar.gz" checksums.txt`,
			`test "$(grep -cF '  shift2eu-site_' checksums.txt)" = 1`,
		}, "\n"),
	},
	{
		what: "actions/attest-build-provenance (the archives)", name: "Attest the archives", uses: provenanceAction,
		with: map[string]*regexp.Regexp{"subject-path": exactly(strings.Join(attestedSubjects, "\n"))},
	},
}

// ghEnv is the env: of a gh step: the tag, and the job's own token for gh.
var ghEnv = []string{tagEnv, "GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}"}

// latestEnv is the latest job's decision, as the publish job reads it.
const latestEnv = "LATEST: ${{ needs.latest.outputs.latest }}"

// publishSteps are the publish job's steps (issues #156, #163, #173): no
// checkout, so no repository code runs beside contents: write; GitHub's own
// download of the archives' workflow artifact, for the checksums.txt the
// attest-archives job attested; and one step that refuses a latest decision
// other than exactly true or false, refuses, while that decision is true, to
// take GitHub's "Latest" release from a release higher than the tag (a
// partial re-run, gh run rerun --failed, does not re-run the latest job and
// can hand it a stale decision; only the API's 404 is no latest release, any
// other failed read fails the step), reads the API's list of the releases,
// since a draft is not readable by its tag, refuses a tag that already holds
// a published release, checks that the tag's one draft holds exactly that
// file and the files it lists, each with the digest it records, and
// publishes that draft by the id the list gave it, with make_latest from the
// latest job's decision alone (LATEST through env:), a string field (-f),
// since the API takes "true" or "false" and not a JSON boolean. Publishing
// by tag in a step of its own would let gh resolve a published release, or a
// draft swapped in after the check, in the place of the one checked. gh is
// given the repository in its own arguments, so none reads it from a git
// remote.
var publishSteps = []releaseStep{
	{
		what: "actions/download-artifact (the archives)", uses: downloadArtifactAction,
		with: map[string]*regexp.Regexp{"name": exactly(archivesArtifact)},
	},
	{
		what: "the check and publication of the draft", name: "Check the draft's assets and publish it",
		env: slices.Insert(slices.Clone(ghEnv), 1, latestEnv),
		run: strings.Join([]string{
			strictShell,
			`# Whether this tag is the highest release tag on main is the latest`,
			`# job's decision, made once for this release: the`,
			`# step refuses anything but the two values it can hold, rather than`,
			`# publish a release with a make_latest nobody decided (issue #173).`,
			`case "$LATEST" in`,
			`  true|false) ;;`,
			`  *)`,
			`    echo "latest is '$LATEST', want true or false" >&2`,
			`    exit 1`,
			`    ;;`,
			`esac`,
			`# A partial re-run of this release run (gh run rerun --failed) does`,
			`# not re-run the successful latest job, so a decision made by that`,
			`# job's first attempt can reach this step after GitHub's "Latest"`,
			`# release has moved on: taking it over from a higher release is`,
			`# refused, before anything is read for the check below or published,`,
			`# and the release stays a draft (issue #173).`,
			`if [ "$LATEST" = true ]; then`,
			`  err="$(mktemp)"`,
			`  if current="$(gh api "repos/` + releaseRepo + `/releases/latest" --jq '.tag_name' 2> "$err")"; then`,
			`    rm -f "$err"`,
			`  elif grep -qF -e "HTTP 404" "$err"; then`,
			`    # The repository publishes no latest release yet: the first`,
			`    # release has nothing to take "Latest" over from.`,
			`    current=""`,
			`    rm -f "$err"`,
			`  else`,
			`    echo "GitHub's latest release could not be read:" >&2`,
			`    cat "$err" >&2`,
			`    rm -f "$err"`,
			`    exit 1`,
			`  fi`,
			`  if [ -n "$current" ] && [ "$current" != "$TAG" ] && [ "$(printf '%s\n%s\n' "$TAG" "$current" | sort -V | tail -n 1)" = "$current" ]; then`,
			`    echo "GitHub's latest release is $current, higher than $TAG: the latest decision is stale, re-run the whole release run rather than its failed jobs" >&2`,
			`    exit 1`,
			`  fi`,
			`fi`,
			`tmp="$(mktemp -d)"`,
			`# A draft release is not readable by its tag. The API lists it among`,
			`# the releases, with draft: true, and gives every asset the digest of`,
			`# its bytes. --paginate --slurp hands the pages over as one array of`,
			`# pages, which add joins into one list of releases.`,
			`gh api --paginate --slurp "repos/` + releaseRepo + `/releases" > "$tmp/releases.json"`,
			`# A tag that already has a published release is refused rather than`,
			`# checked: publishing it again needs a human.`,
			`if [ "$(jq --arg tag "$TAG" 'add | map(select(.draft == false and .tag_name == $tag)) | length' "$tmp/releases.json")" != 0 ]; then`,
			`  echo "repos/` + releaseRepo + ` already publishes a release for $TAG" >&2`,
			`  exit 1`,
			`fi`,
			`if [ "$(jq --arg tag "$TAG" 'add | map(select(.draft and .tag_name == $tag)) | length' "$tmp/releases.json")" != 1 ]; then`,
			`  echo "repos/` + releaseRepo + ` holds no single draft release for $TAG" >&2`,
			`  exit 1`,
			`fi`,
			`# The draft is published by the id this response gives it, and never`,
			`# by the tag again: gh resolves a tag to a published release before a`,
			`# draft, so a release published, or a draft swapped, after the checks`,
			`# below would be published in the place of the one that was checked.`,
			`id="$(jq --arg tag "$TAG" -r 'add | map(select(.draft and .tag_name == $tag)) | .[0].id' "$tmp/releases.json")"`,
			`# The names checksums.txt lists, and checksums.txt itself: what that`,
			`# draft must hold exactly, so nothing is published that the`,
			`# attestation never covered.`,
			`{`,
			`  sed -e 's/^[0-9a-f]\{64\}  //' checksums.txt`,
			`  echo checksums.txt`,
			`} | sort > "$tmp/attested.txt"`,
			`jq --argjson id "$id" -r 'add | map(select(.id == $id)) | .[0].assets[] | .name' "$tmp/releases.json" | sort > "$tmp/published.txt"`,
			`if ! cmp -s "$tmp/attested.txt" "$tmp/published.txt"; then`,
			`  echo "the draft release for $TAG does not hold what checksums.txt lists:" >&2`,
			`  diff -u "$tmp/attested.txt" "$tmp/published.txt" >&2 || true`,
			`  exit 1`,
			`fi`,
			`# Every asset's digest is the one checksums.txt lists: the bytes the`,
			`# attestation covers are the bytes that are published.`,
			`while read -r hash name; do printf 'sha256:%s %s\n' "$hash" "$name"; done < checksums.txt | sort > "$tmp/attested-digests.txt"`,
			`jq --argjson id "$id" -r 'add | map(select(.id == $id)) | .[0].assets[] | select(.name != "checksums.txt") | "\(.digest) \(.name)"' "$tmp/releases.json" | sort > "$tmp/published-digests.txt"`,
			`if ! cmp -s "$tmp/attested-digests.txt" "$tmp/published-digests.txt"; then`,
			`  echo "the draft release for $TAG holds an asset whose digest checksums.txt does not list:" >&2`,
			`  diff -u "$tmp/attested-digests.txt" "$tmp/published-digests.txt" >&2 || true`,
			`  exit 1`,
			`fi`,
			`# checksums.txt is attested too: the draft's copy must be the very`,
			`# file this run attested, not one written after the attestation.`,
			`if [ "$(jq --argjson id "$id" -r 'add | map(select(.id == $id)) | .[0].assets[] | select(.name == "checksums.txt") | .digest' "$tmp/releases.json")" != "sha256:$(sha256sum checksums.txt | cut -d' ' -f1)" ]; then`,
			`  echo "the draft release's checksums.txt is not the one this run attested" >&2`,
			`  exit 1`,
			`fi`,
			`# The draft this job read and checked, and no other, published with`,
			`# GitHub's "Latest" release moved by the latest job's one decision:`,
			`# true only for the highest release tag on main, so a patch on an`,
			`# older line is published without taking it over (issue #173).`,
			`gh api -X PATCH "repos/` + releaseRepo + `/releases/$id" -F draft=false -f make_latest="$LATEST"`,
		}, "\n"),
	},
}

// verifyArchivesSteps are the verify-archives job's steps: every asset of the
// published release, the check that it is published and holds exactly
// checksums.txt and the files it lists, their digests, and the build
// provenance of each, made by the release workflow for this tag and this
// commit on GitHub's runners (docs/guide/verify-releases.md, issue #163).
// Every downloaded file is a subject: the check before the loop holds the
// directory to exactly the release's assets, so no pattern of the workflow's
// own decides what is verified. The last step checks GitHub's "Latest"
// release (issue #173): it must be the tag when the tag is the highest release
// tag on main, read by ancestry as the verify job reads it, independently of
// the latest job's decision, and must not be when the highest release tag's
// release is published; while that release is not published yet (only the
// API's 404), a ::notice:: says so and the check is skipped, as the verify job
// skips the image's latest check. A pre-release must never be it, checked
// before and whatever the highest is, so that skip cannot let one through.
var verifyArchivesSteps = []releaseStep{
	{
		what: "the download of the published archives", name: "Download the published archives",
		env: ghEnv,
		run: `gh release download "$TAG" --repo ` + releaseRepo,
	},
	{
		what: "the archives' verification", name: "Verify the published archives",
		env: ghEnv,
		run: strings.Join([]string{
			strictShell,
			`if [ "$(gh release view "$TAG" --repo ` + releaseRepo + ` --json isDraft --jq '.isDraft')" != false ]; then`,
			`  echo "the release of $TAG is still a draft, so it was never published" >&2`,
			`  exit 1`,
			`fi`,
			`tmp="$(mktemp -d)"`,
			`# The files checksums.txt lists, and checksums.txt itself: what the`,
			`# release must hold exactly, so nothing is verified that the`,
			`# release does not carry, and nothing is left unverified.`,
			`{`,
			`  sed -e 's/^[0-9a-f]\{64\}  //' checksums.txt`,
			`  echo checksums.txt`,
			`} | sort > "$tmp/attested.txt"`,
			`# Every file the release holds, dotfiles included: ls -1 and the`,
			`# shell's * both skip a name that starts with ".", which would let a`,
			`# file hide from this check and from the loop after it. find, not`,
			`# ls -A, so shellcheck has no SC2012 to raise (issue #177).`,
			`find . -mindepth 1 -maxdepth 1 -printf '%P\n' | sort > "$tmp/downloaded.txt"`,
			`if ! cmp -s "$tmp/attested.txt" "$tmp/downloaded.txt"; then`,
			`  echo "the release of $TAG does not hold what checksums.txt lists:" >&2`,
			`  diff -u "$tmp/attested.txt" "$tmp/downloaded.txt" >&2 || true`,
			`  exit 1`,
			`fi`,
			`sha256sum -c checksums.txt`,
			`# dotglob makes * list the dotfiles too, so every downloaded file is`,
			`# a subject and none is left unverified.`,
			`shopt -s dotglob`,
			`for subject in *; do`,
			`  gh attestation verify "$subject" --repo ` + releaseRepo + ` \`,
			`    --signer-workflow ` + signerWorkflow + ` \`,
			`    --source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" --deny-self-hosted-runners`,
			`done`,
		}, "\n"),
	},
	{
		what: "the check of GitHub's latest release", name: "Check GitHub's latest release",
		env: ghEnv,
		run: strings.Join([]string{
			strictShell,
			mainTagsInit,
			mainTagsFetch,
			highestReleaseTag("git -C tags"),
			`if [ -z "$highest" ]; then`,
			`  echo "no release tag is merged into main, so GitHub's latest release cannot be checked" >&2`,
			`  exit 1`,
			`fi`,
			`# published reports whether the release of a tag is published: only`,
			`# the API's 404, its answer for a tag it holds no release for (a draft`,
			`# is not published and has none), is an unpublished release; any other`,
			`# answer, a 5xx, a rate limit, a failed login or a missing gh, is`,
			`# printed and fails the step.`,
			`published() {`,
			`  err="$(mktemp)"`,
			`  if gh api "repos/` + releaseRepo + `/releases/tags/$1" > /dev/null 2> "$err"; then`,
			`    rm -f "$err"`,
			`    return 0`,
			`  fi`,
			`  if grep -qF -e "HTTP 404" "$err"; then`,
			`    rm -f "$err"`,
			`    return 1`,
			`  fi`,
			`  echo "the release of $1 could not be read:" >&2`,
			`  cat "$err" >&2`,
			`  rm -f "$err"`,
			`  return 2`,
			`}`,
			`# latest_tag prints the tag GitHub calls its latest release, or`,
			`# nothing when the repository publishes none.`,
			`latest_tag() {`,
			`  err="$(mktemp)"`,
			`  if out="$(gh api "repos/` + releaseRepo + `/releases/latest" --jq '.tag_name' 2> "$err")"; then`,
			`    printf '%s\n' "$out"`,
			`    rm -f "$err"`,
			`    return 0`,
			`  fi`,
			`  if grep -qF -e "HTTP 404" "$err"; then`,
			`    rm -f "$err"`,
			`    return 0`,
			`  fi`,
			`  echo "GitHub's latest release could not be read:" >&2`,
			`  cat "$err" >&2`,
			`  rm -f "$err"`,
			`  return 1`,
			`}`,
			`latest="$(latest_tag)"`,
			`# A pre-release is never GitHub's "Latest" release, whatever the`,
			`# highest release tag is, and without a condition: while the highest`,
			`# release tag's own release is still unpublished the check below is`,
			`# skipped with a notice, which must not let a pre-release through.`,
			`case "$TAG" in`,
			`  *-*)`,
			`    if [ "$latest" = "$TAG" ]; then`,
			`      echo "GitHub's latest release is $TAG, a pre-release, and a pre-release is never the latest release" >&2`,
			`      exit 1`,
			`    fi`,
			`    ;;`,
			`esac`,
			`if [ "$highest" = "$TAG" ]; then`,
			`  if [ "$latest" != "$TAG" ]; then`,
			`    echo "GitHub's latest release is '$latest', not $TAG, the highest release tag" >&2`,
			`    exit 1`,
			`  fi`,
			`else`,
			`  rc=0`,
			`  published "$highest" || rc=$?`,
			`  case "$rc" in`,
			`    0)`,
			`      if [ "$latest" = "$TAG" ]; then`,
			`        echo "GitHub's latest release is $TAG, which is not the highest release tag, $highest" >&2`,
			`        exit 1`,
			`      fi`,
			`      ;;`,
			`    1)`,
			`      echo "::notice::the release of $highest is not published yet, so this run does not check GitHub's latest release"`,
			`      ;;`,
			`    *)`,
			`      exit 1`,
			`      ;;`,
			`  esac`,
			`fi`,
		}, "\n"),
	},
}

// imageSteps are the image job's steps: the build arguments GoReleaser
// stamps, read from git with the tag passed through env:; a build of two
// platforms from an empty BuildKit, pushed, without BuildKit's own provenance
// or SBOM, tagged latest by the latest job's decision alone, so the image and
// GitHub's "Latest" release cannot disagree. Nothing signs or attests here.
func imageSteps(img releaseImage) []releaseStep {
	return []releaseStep{
		fullCheckoutStep,
		{
			what: "the stamp of the build arguments", name: "Build arguments, as GoReleaser stamps them", id: "stamp",
			env: []string{"TAG: ${{ github.ref_name }}"},
			run: strings.Join([]string{
				`{`,
				`  echo "version=${TAG#v}"`,
				`  echo "commit=$(git rev-parse --short HEAD)"`,
				`  echo "date=$(TZ=UTC git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ)"`,
				`} >> "$GITHUB_OUTPUT"`,
			}, "\n"),
		},
		{
			what: "docker/setup-buildx-action", uses: setupBuildxAction,
			with: map[string]*regexp.Regexp{"driver-opts": buildkitImage},
		},
		loginStep,
		{
			what: "docker/metadata-action", name: "Image tags", id: "tags", uses: metadataAction,
			with: map[string]*regexp.Regexp{
				"images": exactly(img.name),
				"flavor": exactly(latestFlavorOutput),
				"tags": exactly(strings.Join([]string{
					"type=semver,pattern={{raw}}",
					"type=semver,pattern={{major}}.{{minor}},enable=${{ !contains(github.ref_name, '-') }}",
				}, "\n")),
			},
		},
		{
			what: "docker/build-push-action", name: "Build and push linux/amd64 and linux/arm64", id: "build", uses: buildPushAction,
			with: buildWith(img),
		},
	}
}

// buildWith is the with: of an image's build: the release's build arguments
// and settings, and, for an image of a stage other than the last, the stage
// as its target.
func buildWith(img releaseImage) map[string]*regexp.Regexp {
	with := map[string]*regexp.Regexp{
		"context":     exactly("."),
		"platforms":   exactly("linux/amd64,linux/arm64"),
		"push":        exactly("true"),
		"tags":        exactly("${{ steps.tags.outputs.tags }}"),
		"annotations": exactly("index:org.opencontainers.image.source=https://github.com/timothymamo/shift2eu"),
		"provenance":  exactly("false"),
		"sbom":        exactly("false"),
		"build-args": exactly(strings.Join([]string{
			"VERSION=${{ steps.stamp.outputs.version }}",
			"COMMIT=${{ steps.stamp.outputs.commit }}",
			"DATE=${{ steps.stamp.outputs.date }}",
		}, "\n")),
	}

	if img.target != "" {
		with["target"] = exactly(img.target)
	}

	return with
}

// sbomSteps are the sbom job's steps: a read-only login, syft on the
// pushed index by digest (an exact syft, uploading nothing itself), and
// the SPDX document handed to the sign job as a short-lived workflow
// artifact.
func sbomSteps(img releaseImage) []releaseStep {
	return []releaseStep{
		loginStep,
		{
			what: "anchore/sbom-action", uses: sbomAction,
			with: map[string]*regexp.Regexp{
				"image":                 exactly("registry:" + img.name + "@" + img.digest()),
				"format":                exactly("spdx-json"),
				"output-file":           exactly(img.sbomFile),
				"upload-artifact":       exactly("false"),
				"upload-release-assets": exactly("false"),
				"syft-version":          exactRelease,
			},
		},
		{
			what: "actions/upload-artifact", uses: uploadArtifactAction,
			with: map[string]*regexp.Regexp{
				"name":              exactly(img.sbomArtifact),
				"path":              exactly(img.sbomFile),
				"if-no-files-found": exactly("error"),
				"retention-days":    exactly("1"),
			},
		},
	}
}

// signSteps are the sign job's steps, sigstore's and GitHub's code alone:
// cosign by exact release; a cosign login fed the actor and the token
// through env:; a keyless, recursive signature by digest; the provenance
// attestation; the SBOM artifact; and its attestation, both by digest,
// pushed to the registry and stored in no organisation API.
func signSteps(img releaseImage) []releaseStep {
	return []releaseStep{
		cosignInstallerStep,
		cosignLoginStep,
		{
			what: "the signature", name: "Sign the image",
			env: []string{"DIGEST: " + img.digest()},
			run: `cosign sign --yes --recursive "` + img.name + `@${DIGEST}"`,
		},
		{
			what: "actions/attest (the build provenance)", name: "Attest the build provenance", uses: attestAction,
			with: map[string]*regexp.Regexp{
				"subject-name":          exactly(img.name),
				"subject-digest":        exactly(img.digest()),
				"push-to-registry":      exactly("true"),
				"create-storage-record": exactly("false"),
			},
		},
		{
			what: "actions/download-artifact", uses: downloadArtifactAction,
			with: map[string]*regexp.Regexp{"name": exactly(img.sbomArtifact)},
		},
		{
			what: "actions/attest (the SBOM)", name: "Attest the SBOM", uses: attestAction,
			with: map[string]*regexp.Regexp{
				"subject-name":          exactly(img.name),
				"subject-digest":        exactly(img.digest()),
				"sbom-path":             exactly(img.sbomFile),
				"push-to-registry":      exactly("true"),
				"create-storage-record": exactly("false"),
			},
		},
	}
}

var (
	// cosignInstallerStep installs cosign by exact release.
	cosignInstallerStep = releaseStep{
		what: "sigstore/cosign-installer", uses: cosignInstallerAction,
		with: map[string]*regexp.Regexp{"cosign-release": exactRelease},
	}

	// cosignLoginStep logs cosign, and through its credential docker and
	// buildx, in to GHCR with the job's own token, fed through env:.
	cosignLoginStep = releaseStep{
		what: "the cosign login", name: "Log in to ghcr.io",
		env: []string{"ACTOR: ${{ github.actor }}", "GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}"},
		run: `printf '%s' "$GITHUB_TOKEN" | cosign login ` + registryLogin + ` --username "$ACTOR" --password-stdin`,
	}
)

// The release identity every published image is verified against: the
// release workflow, run on a push of exactly the tag being verified.
const (
	releaseRepo     = "timothymamo/shift2eu"
	signerWorkflow  = releaseRepo + "/.github/workflows/release.yml"
	spdxPredicate   = "https://spdx.dev/Document/v2.3"
	tagEnv          = "TAG: ${{ github.ref_name }}"
	verifyJob       = "verify"
	verifyJobName   = "Verify the published image"
	tagRuleStepName = "The tags follow the release rule"
)

// verifyImageStep is the verify job's step for img, the checks of
// docs/guide/verify-releases.md on the digest its image job built: the tag
// must resolve to that digest, and that image must carry the signature this
// workflow made for exactly this tag and this commit, the build provenance and
// the SBOM, attested from this workflow for this tag and commit on GitHub's
// runners, and run with --rm --read-only --network none and then smoke.
func verifyImageStep(img releaseImage, what, name string, smoke ...string) releaseStep {
	attest := []string{
		`gh attestation verify "oci://$IMAGE" --repo ` + releaseRepo + ` \`,
		`  --signer-workflow ` + signerWorkflow + ` \`,
		`  --source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" --deny-self-hosted-runners`,
	}

	sbom := slices.Clone(attest)
	sbom[len(sbom)-1] += ` \`
	sbom = append(sbom, `  --predicate-type `+spdxPredicate)

	return releaseStep{
		what: what, name: name,
		env: []string{tagEnv, "GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}", "BUILT: " + img.digest()},
		run: strings.Join(slices.Concat(
			[]string{
				strictShell,
				`DIGEST="$(docker buildx imagetools inspect "` + img.name + `:$TAG" --format '{{.Manifest.Digest}}')"`,
				`if [ "$DIGEST" != "$BUILT" ]; then`,
				`  echo "` + img.name + `:$TAG resolves to $DIGEST, not the digest this run built, $BUILT" >&2`,
				`  exit 1`,
				`fi`,
				`IMAGE="` + img.name + `@$DIGEST"`,
				`cosign verify "$IMAGE" \`,
				`  --certificate-identity "https://github.com/` + signerWorkflow + `@refs/tags/$TAG" \`,
				`  --certificate-oidc-issuer https://token.actions.githubusercontent.com \`,
				`  --certificate-github-workflow-trigger push \`,
				`  --certificate-github-workflow-sha "$GITHUB_SHA"`,
			},
			attest,
			sbom,
			smoke,
		), "\n"),
	}
}

// highestReleaseTag is the line that sets highest to the highest release tag
// merged into main, read by git, the command given: git in a checkout, git -C
// tags in the repository mainTagsInit makes. Release tags only (vX.Y.Z, no
// pre-release), by version order. The latest job decides by it, once per
// run, whether the tag is the latest release, and the image's latest tag and
// GitHub's "Latest" release follow that one decision (ADR 0015, issue #173);
// the verify and verify-archives jobs read it again on their own to check
// them.
func highestReleaseTag(git string) string {
	return `highest="$(` + releaseTagsPipeline(git) + ` | tail -n 1)"`
}

// releaseTagsPipeline is the pipeline that lists the release tags merged into
// main, read by git, the command given: release tags only (vX.Y.Z, no
// pre-release), in version order. The latest job, the verify jobs and the
// site's deploy and live check all read the release tags through it, so they
// cannot disagree on which tags are releases or on their order.
func releaseTagsPipeline(git string) string {
	return git + ` tag -l 'v*.*.*' --merged refs/remotes/origin/main | sed -nE '/^v[0-9]+\.[0-9]+\.[0-9]+$/p' | sort -V`
}

// mainTagsInit and mainTagsFetch read main's ancestry and the v* tags of the
// public repository, with no trees, into a repository of the job's own, tags,
// so a job that checks nothing out reads the highest release tag as the
// image jobs read it.
const (
	mainTagsInit  = `git init -q tags`
	mainTagsFetch = `git -C tags fetch -q --filter=tree:0 --no-tags https://github.com/` + releaseRepo + `.git '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*'`
)

// latestFlavorOutput is the flavor of the image's metadata-action: latest
// from the latest job's one decision (issue #173).
const latestFlavorOutput = "latest=${{ needs.latest.outputs.latest }}"

// latestSteps are the latest job's steps: the whole history, for origin/main,
// and the one step that sets latest to true only when the tag is the highest
// release tag on main and writes it to the job's output. It refuses a
// checkout without origin/main: its run: has no set -o pipefail, and the run
// shell, bash -e, would otherwise let the pipeline leave highest empty and
// latest false without failing.
var latestSteps = []releaseStep{
	fullCheckoutStep,
	{
		what: "the decision of the latest release", name: "Decide whether the tag is the highest release tag on main", id: "decide",
		env: []string{tagEnv},
		run: strings.Join([]string{
			`git rev-parse --verify --quiet refs/remotes/origin/main > /dev/null || {`,
			`  echo "origin/main is missing from the checkout" >&2`,
			`  exit 1`,
			`}`,
			highestReleaseTag("git"),
			`latest=false`,
			`if [ "$TAG" = "$highest" ]; then latest=true; fi`,
			`echo "latest=$latest" >> "$GITHUB_OUTPUT"`,
		}, "\n"),
	},
}

// strictShell is the first line of every verify script: the run shell is
// bash -e without pipefail or nounset, so an error inside a pipeline or an
// unset variable would otherwise pass unseen.
const strictShell = "set -euo pipefail"

// verifySteps are the verify job's steps: cosign by exact release, a cosign
// login, the check of the image this run built, by digest, and the tag rule,
// whose highest release tag is read by ancestry from main in a bare repository
// of the job's own. No checkout: the job runs no repository code.
var verifySteps = []releaseStep{
	cosignInstallerStep,
	cosignLoginStep,
	verifyImageStep(cliImage, "the CLI image's verification", "Verify the CLI image",
		`docker run --rm --read-only --network none "$IMAGE" version | tee version.txt`,
		`grep -qF "shift2eu ${TAG#v} (" version.txt`,
	),
	{
		what: "the tag rule", name: tagRuleStepName,
		env: []string{tagEnv, "CLI_BUILT: " + cliImage.digest()},
		run: strings.Join([]string{
			strictShell,
			`version="${TAG#v}"`,
			`minor="${version%%-*}"`,
			`minor="${minor%.*}"`,
			mainTagsInit,
			mainTagsFetch,
			highestReleaseTag("git -C tags"),
			`if [ -z "$highest" ]; then`,
			`  echo "no release tag is merged into main, so X.Y and latest cannot be checked" >&2`,
			`  exit 1`,
			`fi`,
			`# digest_or_absent prints the digest its tag resolves to, or nothing`,
			`# when the registry says the tag does not exist. The digest is what`,
			`# the inspect writes on stdout, in stderr kept apart, so a docker`,
			`# warning is never read as one, and what succeeds must be exactly one`,
			`# sha256 line. Absence is exit status 1 with the fixed, case-sensitive`,
			`# "$1: not found" in stderr, which names this very reference, and`,
			`# nothing else is absence: any other answer, a 5xx, a rate limit, a`,
			`# failed login, a missing docker or a broken credential helper, is`,
			`# printed and fails the step, whose caller assigns the digest in a`,
			`# command of its own so set -e sees the failure.`,
			`digest_or_absent() {`,
			`  err="$(mktemp)"`,
			`  if out="$(docker buildx imagetools inspect "$1" --format '{{.Manifest.Digest}}' 2> "$err")"; then`,
			`    if [[ "$out" =~ ^sha256:[0-9a-f]{64}$ ]]; then`,
			`      printf '%s\n' "$out"`,
			`    else`,
			`      echo "$1 answered '$out' rather than one sha256 digest" >&2`,
			`      rm -f "$err"`,
			`      return 1`,
			`    fi`,
			`  else`,
			`    rc=$?`,
			`    if [ "$rc" -eq 1 ] && grep -qF -e "$1: not found" "$err"; then`,
			`      :`,
			`    else`,
			`      echo "$1 could not be read, docker exited $rc:" >&2`,
			`      cat "$err" >&2`,
			`      rm -f "$err"`,
			`      return 1`,
			`    fi`,
			`  fi`,
			`  rm -f "$err"`,
			`}`,
			`check() {`,
			`  image="$1"`,
			`  digest="$2"`,
			`  minor_digest="$(digest_or_absent "$image:$minor")"`,
			`  case "$TAG" in`,
			`    *-*)`,
			`      if [ "$minor_digest" = "$digest" ]; then`,
			`        echo "$image:$minor is $TAG, a pre-release, and a pre-release moves neither X.Y nor latest" >&2`,
			`        exit 1`,
			`      fi`,
			`      latest_digest="$(digest_or_absent "$image:latest")"`,
			`      if [ "$latest_digest" = "$digest" ]; then`,
			`        echo "$image:latest is $TAG, a pre-release, and a pre-release moves neither X.Y nor latest" >&2`,
			`        exit 1`,
			`      fi`,
			`      ;;`,
			`    *)`,
			`      if [ "$minor_digest" != "$digest" ]; then`,
			`        echo "$image:$minor is not $TAG" >&2`,
			`        exit 1`,
			`      fi`,
			`      if [ "$highest" = "$TAG" ]; then`,
			`        latest_digest="$(digest_or_absent "$image:latest")"`,
			`        if [ "$latest_digest" != "$digest" ]; then`,
			`          echo "$image:latest is not $TAG, the highest release tag" >&2`,
			`          exit 1`,
			`        fi`,
			`      else`,
			`        highest_digest="$(digest_or_absent "$image:$highest")"`,
			`        if [ -z "$highest_digest" ]; then`,
			`          echo "::notice::$image:$highest is not published yet, so this run does not check $image:latest"`,
			`        else`,
			`          latest_digest="$(digest_or_absent "$image:latest")"`,
			`          if [ "$latest_digest" = "$digest" ]; then`,
			`            echo "$image:latest is $TAG, which is not the highest release tag, $highest" >&2`,
			`            exit 1`,
			`          fi`,
			`        fi`,
			`      fi`,
			`      ;;`,
			`  esac`,
			`}`,
			`check ` + cliImage.name + ` "$CLI_BUILT"`,
		}, "\n"),
	},
}

// The site's two programs as the release builds and hands them on (PRD #297
// milestone 3): each the name of its workflow artifact, of its file in bin/
// and of its digest's output of the site-programs job.
const (
	siteDeployProgram = "sitedeploy"
	siteCheckProgram  = "sitecheck"
)

// siteProgramsSteps are the site-programs job's steps (PRD #297 milestone 3):
// the tag's commit, Go without the Actions cache, make site-programs (whose
// static build's environment lives in the Makefile, so no GO or CGO variable is
// named here), the two programs' sha256 as the job's outputs (id: digests),
// and each program as a workflow artifact of its own, kept thirty days,
// GitHub's re-run window, so the deploy downloads the deploy program alone and
// a deploy is re-run by re-running its own job, never the site job, while
// GitHub still re-runs the run's jobs.
var siteProgramsSteps = []releaseStep{
	shallowCheckoutStep,
	setupGoStep,
	{what: "make site-programs", name: "Build the site's programs", run: "make site-programs"},
	{
		what: "the programs' digests", name: "The programs' digests", id: "digests",
		run: strings.Join([]string{
			strictShell,
			`echo "` + siteDeployProgram + `=$(sha256sum bin/` + siteDeployProgram + ` | cut -d' ' -f1)" >> "$GITHUB_OUTPUT"`,
			`echo "` + siteCheckProgram + `=$(sha256sum bin/` + siteCheckProgram + ` | cut -d' ' -f1)" >> "$GITHUB_OUTPUT"`,
		}, "\n"),
	},
	siteProgramUploadStep(siteDeployProgram),
	siteProgramUploadStep(siteCheckProgram),
}

// siteProgramUploadStep is the upload of one of the site's programs from bin/
// as a workflow artifact of its own name, kept thirty days: GitHub re-runs a
// run's jobs only within 30 days of the initial run, so a longer retention
// would serve no re-run.
func siteProgramUploadStep(program string) releaseStep {
	return releaseStep{
		what: "actions/upload-artifact (" + program + ")", uses: uploadArtifactAction,
		with: map[string]*regexp.Regexp{
			"name":              exactly(program),
			"path":              exactly("bin/" + program),
			"if-no-files-found": exactly("error"),
			"retention-days":    exactly("30"),
		},
	}
}

// siteDraftCheck refuses a release of the tag that is still a draft: the deploy
// and the live check read the published release's bundle, and a draft's assets
// are not what the release published.
var siteDraftCheck = []string{
	`if [ "$(gh release view "$TAG" --repo ` + releaseRepo + ` --json isDraft --jq '.isDraft')" != false ]; then`,
	`  echo "the release of $TAG is still a draft, so its bundle was never published" >&2`,
	`  exit 1`,
	`fi`,
}

// siteReleaseTagsStep writes the release tags on main, in releaseTagsPipeline's
// filter and sort, from a treeless fetch into a repository of the job's own,
// to file, and fails unless the tag is one of them.
func siteReleaseTagsStep(file string) releaseStep {
	return releaseStep{
		what: "the release tags on main", name: "The release tags on main", env: []string{tagEnv},
		run: strings.Join([]string{
			strictShell,
			mainTagsInit,
			mainTagsFetch,
			releaseTagsPipeline("git -C tags") + " > " + file,
			`grep -qxF "$TAG" ` + file + ` || { echo "$TAG is not a release tag on main" >&2; exit 1; }`,
		}, "\n"),
	}
}

// siteDeployStepName is the name of the one step of the workflow that holds the
// site's key.
const siteDeployStepName = "Deploy the site"

// siteDeploySteps are the site-deploy job's steps (PRD #297 milestone 3, ADR
// 0042), with no checkout: the deploy program's artifact; the published
// release's bundle, after the check that the release is not a draft, checked
// with the program against the site job's and the site-programs job's digests
// in one sha256sum -c, its attestation verified for this workflow, this tag and
// this commit on GitHub's runners, and unpacked; the release tags on main; the
// manifest of every higher release, read from the bundle that release attested
// (a release not published yet, a draft and a release without the bundle each
// a ::notice:: and skipped, any other failed read a failure); and last the
// deploy, the one step that holds the key, with no token.
var siteDeploySteps = []releaseStep{
	{
		what: "actions/download-artifact (" + siteDeployProgram + ")", uses: downloadArtifactAction,
		with: map[string]*regexp.Regexp{"name": exactly(siteDeployProgram)},
	},
	{
		what: "the check of the site bundle and the deploy program", name: "Check the site bundle and the deploy program",
		env: []string{
			tagEnv, "SITE_SHA256: ${{ needs.site.outputs.sha256 }}",
			"SITEDEPLOY_SHA256: ${{ needs.site-programs.outputs." + siteDeployProgram + " }}",
			"GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
		},
		run: strings.Join(slices.Concat(
			[]string{strictShell},
			siteDraftCheck,
			[]string{
				`gh release download "$TAG" --repo ` + releaseRepo + ` --pattern "shift2eu-site_$TAG.tar.gz"`,
				`printf '%s  %s\n%s  %s\n' "$SITE_SHA256" "shift2eu-site_$TAG.tar.gz" "$SITEDEPLOY_SHA256" sitedeploy | sha256sum -c -`,
				`gh attestation verify "shift2eu-site_$TAG.tar.gz" --repo ` + releaseRepo + ` \`,
				`  --signer-workflow ` + signerWorkflow + ` \`,
				`  --source-ref "refs/tags/$TAG" --source-digest "$GITHUB_SHA" --deny-self-hosted-runners`,
				`mkdir bundle`,
				`tar -xzf "shift2eu-site_$TAG.tar.gz" -C bundle`,
				`chmod u+x sitedeploy`,
			},
		), "\n"),
	},
	siteReleaseTagsStep("releases.txt"),
	{
		what: "the higher releases' manifests", name: "The higher releases' manifests", env: ghEnv,
		run: strings.Join([]string{
			strictShell,
			`mkdir manifests higher`,
			`mapfile -t tags < releases.txt`,
			`for t in "${tags[@]}"; do`,
			`  if [ "$t" = "$TAG" ] || [ "$(printf '%s\n%s\n' "$TAG" "$t" | sort -V | tail -n 1)" != "$t" ]; then`,
			`    continue`,
			`  fi`,
			`  err="$(mktemp)"`,
			`  if ! gh api "repos/` + releaseRepo + `/releases/tags/$t" > "higher/release.json" 2> "$err"; then`,
			`    if grep -qF -e "HTTP 404" "$err"; then`,
			`      echo "::notice::the release of $t is not published yet, so its manifest is not read"`,
			`      rm -f "$err"`,
			`      continue`,
			`    fi`,
			`    echo "the release of $t could not be read:" >&2`,
			`    cat "$err" >&2`,
			`    rm -f "$err"`,
			`    exit 1`,
			`  fi`,
			`  rm -f "$err"`,
			`  if [ "$(jq -r '.draft' "higher/release.json")" != false ]; then`,
			`    echo "::notice::the release of $t is still a draft, so its manifest is not read"`,
			`    continue`,
			`  fi`,
			`  if [ "$(jq --arg name "shift2eu-site_$t.tar.gz" -r '[.assets[].name] | map(select(. == $name)) | length' "higher/release.json")" != 1 ]; then`,
			`    echo "::notice::the release of $t publishes no shift2eu-site_$t.tar.gz, so its manifest is not read"`,
			`    continue`,
			`  fi`,
			`  gh release download "$t" --repo ` + releaseRepo + ` --pattern "shift2eu-site_$t.tar.gz" --dir higher`,
			`  gh attestation verify "higher/shift2eu-site_$t.tar.gz" --repo ` + releaseRepo + ` \`,
			`    --signer-workflow ` + signerWorkflow + ` \`,
			`    --source-ref "refs/tags/$t" --deny-self-hosted-runners`,
			`  tar -xzOf "higher/shift2eu-site_$t.tar.gz" manifest.json > "manifests/$t.json"`,
			`done`,
		}, "\n"),
	},
	{
		what: "the deploy", name: siteDeployStepName, noToken: true,
		env: []string{
			siteKeyEnv[0], siteKeyEnv[1], tagEnv,
			"SITE_BUCKET: ${{ vars.SITE_BUCKET }}", "SITE_REGION: ${{ vars.SITE_REGION }}",
		},
		run: `./sitedeploy -bundle bundle -version "$TAG" -bucket "$SITE_BUCKET" -region "$SITE_REGION" -releases releases.txt -manifests manifests`,
	},
}

// siteKeyEnv are the deploy step's two env: lines that name the site's key,
// its access key's and its secret key's, the only lines of the whole document
// either secret may appear on. They name the secrets; they hold no value.
var siteKeyEnv = [2]string{
	"SCW_ACCESS_KEY: ${{ secrets.SCW_ACCESS_KEY }}",
	"SCW_SECRET_KEY: ${{ secrets.SCW_SECRET_KEY }}",
}

// siteCheckSteps are the site-check job's steps (PRD #297 milestone 3): the
// tag's commit without the token, for site/records' HSTS and preload lists at
// the live check's default paths; the live check's artifact into bin; the
// published release's bundle, after the draft check, checked with the program
// against the site job's and the site-programs job's digests in one sha256sum
// -c, and its manifest extracted; the release tags on main; and the live check
// at its defaults (no -plain-http-www, -host, -apex or -retry-bound).
var siteCheckSteps = []releaseStep{
	shallowCheckoutStep,
	{
		what: "actions/download-artifact (" + siteCheckProgram + ")", uses: downloadArtifactAction,
		with: map[string]*regexp.Regexp{"name": exactly(siteCheckProgram), "path": exactly("bin")},
	},
	{
		what: "the check of the site bundle and the live check program", name: "Check the site bundle and the live check program",
		env: []string{
			tagEnv, "SITE_SHA256: ${{ needs.site.outputs.sha256 }}",
			"SITECHECK_SHA256: ${{ needs.site-programs.outputs." + siteCheckProgram + " }}",
			"GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
		},
		run: strings.Join(slices.Concat(
			[]string{strictShell},
			siteDraftCheck,
			[]string{
				`gh release download "$TAG" --repo ` + releaseRepo + ` --pattern "shift2eu-site_$TAG.tar.gz" --dir bin`,
				`printf '%s  %s\n%s  %s\n' "$SITE_SHA256" "bin/shift2eu-site_$TAG.tar.gz" "$SITECHECK_SHA256" bin/sitecheck | sha256sum -c -`,
				`tar -xzOf "bin/shift2eu-site_$TAG.tar.gz" manifest.json > bin/manifest.json`,
				`chmod u+x bin/sitecheck`,
			},
		), "\n"),
	},
	siteReleaseTagsStep("bin/releases.txt"),
	{what: "the live check", name: "Check the live site", run: "bin/sitecheck -manifest bin/manifest.json -tags bin/releases.txt"},
}

// siteSkippedSteps are the site-skipped job's one step (PRD #297 milestone 3):
// the reason the deploy did not run, as a ::notice:: and as the summary's
// note, from LATEST and ENABLED through env:, the variable's value never
// printed, and a failure when the deploy's conditions do hold.
var siteSkippedSteps = []releaseStep{
	{
		what: "the skip notice", name: "Say that the site deploy is skipped",
		env: []string{latestEnv, siteSkippedEnabledEnv},
		run: strings.Join([]string{
			strictShell,
			`if [ "$LATEST" != true ]; then`,
			`  reason="the tag is not the highest release tag on main"`,
			`elif [ "$ENABLED" != true ]; then`,
			`  reason="SITE_DEPLOY_ENABLED is not true"`,
			`else`,
			`  echo "the deploy's conditions hold, so this job should not have run" >&2`,
			`  exit 1`,
			`fi`,
			`echo "::notice title=Site deploy skipped::The site deploy and its live check are skipped: $reason"`,
			`printf '> [!NOTE]\n> The site deploy and its live check are skipped: %s.\n' "$reason" >> "$GITHUB_STEP_SUMMARY"`,
		}, "\n"),
	},
}

// The site deploy's switch (PRD #297 milestone 3): the conditions of the
// deploy, of the live check and of the skip notice, and the skip notice's
// env: line, the only lines that may name the variable.
const (
	siteDeployIf          = "needs.latest.outputs.latest == 'true' && vars.SITE_DEPLOY_ENABLED == 'true'"
	siteSkippedIf         = "needs.latest.outputs.latest != 'true' || vars.SITE_DEPLOY_ENABLED != 'true'"
	siteSkippedEnabledEnv = "ENABLED: ${{ vars.SITE_DEPLOY_ENABLED }}"
)

// releaseJob is one job of the release workflow.
type releaseJob struct {
	key, name string
	// needs are the jobs it needs, in the order written; nil for none. One
	// job may be written as a single name or a list of one.
	needs []string
	// permissions is the job's whole permissions: mapping; emptyPermissions
	// holds it to exactly permissions: {} instead.
	permissions      map[string]string
	emptyPermissions bool
	// outputs is the job's whole outputs: mapping; nil for none.
	outputs map[string]string
	// timeout is the job's timeout-minutes, which every job holds: GitHub's
	// default of six hours would keep the one release concurrency group
	// waiting that long on a hung job (ADR 0042).
	timeout string
	// ifCond is the job's if:, exactly, "" for none; environment its
	// environment:, a scalar, "" for none; and concurrency its whole
	// concurrency: mapping, nil for none.
	ifCond, environment string
	concurrency         map[string]string
	// secrets are the secrets the job's lines may name, nil for any the
	// steps' exact env: lines allow (the jobs before the site's deploy, held
	// by their steps alone).
	secrets []string
	steps   []releaseStep
}

// releaseJobs are the release workflow's jobs, in the order a message lists
// them: test, latest, site and site-browser, goreleaser, attest-archives and
// publish, then the image's three jobs, then verify and verify-archives, then
// the site's deploy: site-programs, site-deploy, site-check and site-skipped.
// The write permissions stay split: the site jobs, which build and smoke-test
// the site's bundle and build its programs, only read the repository, with no
// OIDC token and no secret; site-deploy alone holds the site's key, with
// contents: read and attestations: read, and site-check and site-skipped
// read the repository or nothing at all;
// the goreleaser job writes the repository's contents to upload to a draft
// release, and publish alone publishes it, holding nothing else, and only
// once the bundle passed its smoke test; an image job pushes and nothing
// more; an sbom job, which runs syft, can only read the registry; a sign job
// and attest-archives alone hold an OIDC token and write attestations,
// attest-archives with read-only contents; and the verify jobs read and write
// nothing.
var releaseJobs = func() []releaseJob {
	jobs := []releaseJob{
		{key: "test", name: "Test", permissions: map[string]string{"contents": "read"}, timeout: "20", steps: testSteps},
		{
			key: "latest", name: "Latest", needs: []string{"test"},
			permissions: map[string]string{"contents": "read"},
			outputs:     map[string]string{"latest": "${{ steps.decide.outputs.latest }}"},
			timeout:     "5", steps: latestSteps,
		},
		{
			key: "site", name: "Site bundle", needs: []string{"test"},
			permissions: map[string]string{"contents": "read"},
			outputs:     map[string]string{"sha256": "${{ steps.digest.outputs.sha256 }}"},
			timeout:     "15", steps: siteSteps,
		},
		{
			key: "site-browser", name: "Site browser smoke test", needs: []string{"site"},
			permissions: map[string]string{"contents": "read"}, timeout: "30", steps: siteBrowserSteps,
		},
		{
			key: "goreleaser", name: "GoReleaser", needs: []string{"test", "site"},
			permissions: map[string]string{"contents": "write"}, timeout: "15", steps: goreleaserSteps,
		},
		{
			key: "attest-archives", name: "Attest the archives", needs: []string{"goreleaser", "site"},
			permissions: map[string]string{"id-token": "write", "attestations": "write", "contents": "read"},
			timeout:     "10", steps: attestArchivesSteps,
		},
		{
			key: "publish", name: "Publish the release",
			needs:       []string{"test", "goreleaser", "attest-archives", "latest", "site-browser"},
			permissions: map[string]string{"contents": "write"}, timeout: "10", steps: publishSteps,
		},
	}

	for _, img := range releaseImages {
		jobs = append(jobs,
			releaseJob{
				key: img.imageJob, name: "Container image" + img.suffix, needs: []string{"test", "goreleaser", "latest"},
				permissions: map[string]string{"contents": "read", "packages": "write"},
				outputs:     map[string]string{"digest": builtDigest},
				timeout:     "20", steps: imageSteps(img),
			},
			releaseJob{
				key: img.sbomJob, name: "SBOM" + img.suffix, needs: []string{img.imageJob},
				permissions: map[string]string{"packages": "read"}, timeout: "10", steps: sbomSteps(img),
			},
			releaseJob{
				key: img.signJob, name: "Sign and attest" + img.suffix, needs: []string{img.imageJob, img.sbomJob},
				permissions: map[string]string{"packages": "write", "id-token": "write", "attestations": "write"},
				timeout:     "10", steps: signSteps(img),
			},
		)
	}

	return append(jobs,
		releaseJob{
			key: verifyJob, name: verifyJobName,
			needs:       []string{cliImage.imageJob, cliImage.signJob},
			permissions: map[string]string{"packages": "read", "attestations": "read"},
			timeout:     "10", steps: verifySteps,
		},
		releaseJob{
			key: "verify-archives", name: "Verify the published archives", needs: []string{"publish"},
			permissions: map[string]string{"contents": "read", "attestations": "read"},
			timeout:     "10", steps: verifyArchivesSteps,
		},
		releaseJob{
			key: "site-programs", name: "Site programs", needs: []string{"test"},
			permissions: map[string]string{"contents": "read"},
			outputs: map[string]string{
				siteDeployProgram: "${{ steps.digests.outputs." + siteDeployProgram + " }}",
				siteCheckProgram:  "${{ steps.digests.outputs." + siteCheckProgram + " }}",
			},
			timeout: "10", secrets: []string{}, steps: siteProgramsSteps,
		},
		releaseJob{
			key: siteDeployJob, name: "Deploy the site",
			needs:   []string{"latest", verifyJob, "verify-archives", "attest-archives", "site", "site-browser", "site-programs"},
			ifCond:  siteDeployIf,
			timeout: "15", environment: "site",
			concurrency: map[string]string{"group": "site-deploy", "cancel-in-progress": "false"},
			permissions: map[string]string{"contents": "read", "attestations": "read"},
			secrets:     []string{"GITHUB_TOKEN", "SCW_ACCESS_KEY", "SCW_SECRET_KEY"},
			steps:       siteDeploySteps,
		},
		releaseJob{
			key: "site-check", name: "Check the live site", needs: []string{"latest", "site", "site-programs", siteDeployJob},
			ifCond:      siteDeployIf,
			permissions: map[string]string{"contents": "read"},
			timeout:     "15", secrets: []string{"GITHUB_TOKEN"}, steps: siteCheckSteps,
		},
		releaseJob{
			key: "site-skipped", name: "Site deploy skipped", needs: []string{"latest"},
			ifCond: siteSkippedIf, emptyPermissions: true,
			timeout: "5", secrets: []string{}, steps: siteSkippedSteps,
		},
	)
}()

// siteDeployJob is the key of the one job that holds the site's key.
const siteDeployJob = "site-deploy"

// ReleaseWorkflow returns a message for everything in the release workflow
// document src that could make goreleaser build, or the publishing jobs
// publish, something other than what the check lists. It reads the document
// as a closed schema:
//
//   - the top level holds exactly name, on (a push of v*.*.* tags),
//     permissions ({}), concurrency (exactly group: release and
//     cancel-in-progress: false, so one release runs at a time across every
//     tag and a run in progress is never cancelled half published; a waiting
//     run may be replaced by a newer tag's and must then be re-run by hand,
//     ADR 0015) and jobs, so no workflow-level env: or defaults:;
//   - jobs holds exactly test, latest, site and site-browser (the site's
//     bundle, PRD #297 milestone 3), goreleaser, attest-archives, publish, the
//     image, sbom and sign jobs of the one image the release publishes (the
//     CLI's, ghcr.io/timothymamo/shift2eu), verify, the independent check of
//     that image (issue #99), verify-archives, that of its assets (issue
//     #163), and the site's deploy (PRD #297 milestone 3, ADR 0042):
//     site-programs, site-deploy, site-check and site-skipped. Each holds
//     exactly name, runs-on
//     (ubuntu-latest), permissions, steps and timeout-minutes (exactly test
//     20, latest 5, site 15, site-browser 30, goreleaser 15, image 20,
//     site-deploy 15, site-check 15, site-skipped 5, and 10 for every other
//     job, ADR 0042), and needs for all but test
//     (latest needs test; site test; site-browser site; goreleaser [test,
//     site]; attest-archives [goreleaser, site]; publish [test, goreleaser,
//     attest-archives, latest, site-browser]; an image job
//     [test, goreleaser, latest]; an sbom job its image job; a sign job [its
//     image job, its sbom job], in that order; verify [image, sign];
//     verify-archives publish; site-programs test; site-deploy [latest,
//     verify, verify-archives, attest-archives, site, site-browser,
//     site-programs]; site-check [latest, site, site-programs, site-deploy];
//     site-skipped latest), latest an
//     outputs: of exactly its decide step's latest, site one of exactly its
//     digest step's sha256, site-programs one of exactly its digests step's
//     sitedeploy and sitecheck, and an image job an
//     outputs: of exactly its build's digest. site-deploy and site-check alone
//     hold an if:, exactly needs.latest.outputs.latest == 'true' &&
//     vars.SITE_DEPLOY_ENABLED == 'true', and site-skipped its exact negation
//     (!= and ||); site-deploy alone holds environment: site, a scalar, and a
//     concurrency: of exactly group: site-deploy and cancel-in-progress:
//     false. So no job-level env:, container:, or other if:, environment: or
//     concurrency: key;
//   - the site job's steps are exactly actions/checkout of the tag's commit
//     without the token, actions/setup-go, make site with SITE_VERSION the tag
//     through env:, the archive's sha256 written to GITHUB_OUTPUT (id: digest)
//     and actions/upload-artifact of bin/shift2eu-site_*.tar.gz as site, kept
//     seven days; the site-browser job's exactly that checkout, setup-go,
//     actions/download-artifact of site into bin, the check of the archive
//     against needs.site.outputs.sha256 before it is unpacked into
//     bin/release-bundle, and make test-browser
//     TEST_BROWSER_BUNDLE=bin/release-bundle, so the bundle smoke-tested is
//     the one the release attests and publishes;
//   - no line, comments included, names shift2eu-server or openapi: the release
//     ships the CLI alone, so the HTTP API's jobs, its archive and its OpenAPI
//     asset are gone rather than merely unreferenced (issue #393);
//   - the permissions are exactly contents: read for test, latest, site and
//     site-browser;
//     contents: write for goreleaser and for publish; id-token: write,
//     attestations: write and contents: read for attest-archives; contents:
//     read and packages: write for an image job; packages: read for an sbom
//     job; packages: write, id-token: write and attestations: write for a
//     sign job; packages: read and attestations: read for verify, and
//     contents: read and attestations: read for verify-archives, so no OIDC
//     token and nothing either can write; contents: read for site-programs
//     and site-check, contents: read and attestations: read for site-deploy,
//     and exactly permissions: {} for site-skipped;
//   - the site-programs job's steps are exactly actions/checkout of the tag's
//     commit without the token, actions/setup-go, make site-programs (whose
//     static build's environment is the Makefile's), the two programs' sha256
//     written to GITHUB_OUTPUT (id: digests), and actions/upload-artifact of
//     bin/sitedeploy as sitedeploy and of bin/sitecheck as sitecheck, each
//     kept thirty days, GitHub's re-run window;
//   - the site-deploy job's steps are exactly, with no checkout and the
//     deploy last: actions/download-artifact of sitedeploy; one step under set
//     -euo pipefail that refuses a release still a draft (isDraft not false)
//     before it downloads the published release's shift2eu-site_$TAG.tar.gz,
//     checks it and the program in one sha256sum -c against
//     needs.site.outputs.sha256 and needs.site-programs.outputs.sitedeploy,
//     runs gh attestation verify of the bundle with the release identity,
//     --source-ref "refs/tags/$TAG", --source-digest "$GITHUB_SHA" and
//     --deny-self-hosted-runners, and unpacks it into bundle; the release tags
//     on main, by the latest job's pipeline (releaseTagsPipeline) on a
//     treeless fetch, into releases.txt, which must hold the tag; the higher
//     releases' manifests, each tag read from releases.txt alone, a release
//     not published yet (the API's 404), a draft or one without the bundle a
//     ::notice:: and skipped, any other failed read a failure, every other
//     bundle verified with the release identity, its own --source-ref and
//     --deny-self-hosted-runners before its manifest.json is read; and the
//     deploy, ./sitedeploy -bundle bundle -version "$TAG" -bucket
//     "$SITE_BUCKET" -region "$SITE_REGION" -releases releases.txt
//     -manifests manifests (no -list-bucket), with an env: of exactly the two
//     keys from secrets.SCW_ACCESS_KEY and secrets.SCW_SECRET_KEY, the tag,
//     and SITE_BUCKET and SITE_REGION from the site environment's variables,
//     and no line naming GITHUB_TOKEN or GH_TOKEN;
//   - the site-check job's steps are exactly actions/checkout of the tag's
//     commit without the token (site/records at the tag),
//     actions/download-artifact of sitecheck into bin, the same draft check
//     before the published bundle is downloaded into bin, checked with the
//     program in one sha256sum -c against needs.site.outputs.sha256 and
//     needs.site-programs.outputs.sitecheck, and its manifest.json extracted;
//     the release tags on main into bin/releases.txt; and bin/sitecheck
//     -manifest bin/manifest.json -tags bin/releases.txt, at its defaults;
//   - the site-skipped job's one step prints why the deploy was skipped as a
//     ::notice:: and in the step summary, LATEST and ENABLED through env:,
//     never the variable's value, and fails when the deploy's conditions hold;
//   - the four site jobs name no secret but those they need (in any case and
//     either expression form): none in site-programs and site-skipped,
//     GITHUB_TOKEN alone in site-check, and GITHUB_TOKEN, SCW_ACCESS_KEY and
//     SCW_SECRET_KEY in site-deploy;
//   - the test job's steps are exactly actions/checkout of the whole history,
//     the guard that the tagged commit is on main (an exact run: script),
//     actions/setup-go and make test; the goreleaser job's exactly
//     actions/checkout, actions/setup-go, actions/download-artifact of site
//     into bin, the check of the bundle against needs.site.outputs.sha256,
//     goreleaser/goreleaser-action and
//     actions/upload-artifact of the archives, each named by its extension,
//     and checksums.txt (archives, kept seven days so a failed
//     attestation or publication can be re-run); setup-go with cache: false in both jobs,
//     goreleaser asked for exactly release --clean at an exact version, with
//     an env: of exactly GITHUB_TOKEN from secrets.GITHUB_TOKEN;
//   - the latest job's steps are exactly actions/checkout of the whole
//     history, without the token, and one step, id: decide, with the tag
//     through env:, that refuses a checkout without origin/main (its run:
//     has no pipefail), reads the release tags merged into origin/main,
//     pre-releases left out, and writes latest=true to GITHUB_OUTPUT only
//     when the tag is the highest of them, latest=false otherwise: the one
//     decision, made once per run, that the image's latest tag and GitHub's
//     "Latest" release follow, so they cannot disagree (issue #173);
//   - the attest-archives job's steps are exactly actions/download-artifact
//     of that artifact and of site beside it, one step under set -euo
//     pipefail that runs sha256sum -c checksums.txt and holds checksums.txt
//     to exactly one shift2eu-site_ line, the one with the site job's digest,
//     and actions/attest-build-provenance with a subject-path of every
//     archive, by its extension, the site's bundle and checksums.txt;
//   - the publish job's steps are exactly actions/download-artifact of that
//     artifact, with no checkout, and one step that refuses a LATEST other
//     than exactly true or false and checks the draft release (an exact run:
//     script under set -euo pipefail, reading the API's list of the
//     releases: no published release for the tag, exactly one draft for it,
//     holding exactly checksums.txt and the files it lists, each asset's
//     digest the one it records, and its checksums.txt the attested one) and
//     then publishes that draft, the one GoReleaser uploaded to (.goreleaser.yaml's
//     release.draft, check.GitHubRelease), by the id the list gave it with gh
//     api -X PATCH repos/timothymamo/shift2eu/releases/$id -F draft=false -f
//     make_latest="$LATEST", never by its tag, the tag, the latest job's
//     decision and the token through env:; the one step of the workflow that
//     publishes it (issues #156, #163, #173);
//   - an image job's steps are exactly actions/checkout, the stamp of the
//     build arguments (version, commit and date, a run: script with the tag
//     through env:),
//     docker/setup-buildx-action (BuildKit by exact release and index
//     digest), docker/login-action, docker/metadata-action (the CLI's image
//     name, latest from the latest job's output alone) and
//     docker/build-push-action (linux/amd64 and linux/arm64, pushed, no cache,
//     no BuildKit provenance or SBOM, and no target:, so the Dockerfile's last
//     stage);
//   - an sbom job's steps are exactly docker/login-action, anchore/sbom-action
//     (its pushed index by its image job's digest, an exact syft, uploading
//     nothing) and actions/upload-artifact of its SPDX document
//     (shift2eu.spdx.json as sbom);
//   - a sign job's steps are exactly sigstore/cosign-installer (an exact
//     release), a cosign login with the actor and token through env:, cosign
//     sign --recursive of its image by its image job's digest,
//     actions/attest of the build provenance, actions/download-artifact of
//     its SPDX document and actions/attest of the SBOM, both by that digest,
//     pushed to the registry;
//   - the verify job's steps are exactly sigstore/cosign-installer, the cosign
//     login, one step that, under set -euo pipefail, resolves its
//     tag to a digest, fails unless it is the digest its image job built
//     (BUILT through env:), and, by that digest, runs cosign verify with the
//     release identity and this commit (--certificate-github-workflow-sha),
//     gh attestation verify of the provenance and of the SBOM, both bound to
//     this commit (--source-digest), and the image with --rm --read-only
//     --network none (version, compared to the tag), and the tag rule, whose
//     highest release tag is the
//     release tags merged into main, read by ancestry from a treeless fetch into a
//     repository of the job's own, with no checkout;
//   - the verify-archives job's steps are exactly gh release download, with
//     --repo, of every asset of the published release, and, under set -euo
//     pipefail, the check that the release is not a draft and holds exactly
//     checksums.txt and the files it lists, dotfiles included (find -printf %P),
//     sha256sum -c checksums.txt, and gh attestation verify of every
//     downloaded file, dotfiles included (shopt -s dotglob), with the
//     release identity, bound to this tag and commit, on GitHub's runners
//     only; and the check of GitHub's latest release (releases/latest,
//     tag_name) against the highest release tag, read as the verify job's tag
//     rule reads it, independently of the latest job, refusing an empty one:
//     never the tag if it is a pre-release, whatever else holds; the tag if
//     it is the highest, not the tag if the highest's release is published,
//     a ::notice:: and no check while the API answers 404 for it, and any
//     other answer a failure (issue #173);
//   - every step holds exactly the keys listed (name, id, uses, with, env,
//     run), in that order of steps, each action pinned by commit, each with:
//     key and value, env: line and run: command as listed. A run: command is
//     one line or a literal block (|), compared line by line, and holds no
//     ${{ }} expression: what a step takes from the workflow comes in through
//     env:.
//
// The goreleaser job therefore runs no repository code before goreleaser, and
// make test, which does, runs on another runner: neither the go env file nor
// GITHUB_ENV or GITHUB_PATH carries over from it. The one other channel
// between jobs, setup-go's Actions cache of GOMODCACHE and GOCACHE (shared by
// every job with the same runner OS, Go version and go.sum, CI on main
// included), is closed by cache: false: the go command trusts what GOCACHE
// holds and does not verify extracted modules again. The variables of the go
// command can come only from the runner image or from inside a pinned action.
//
// As a backstop, no line, comments included, may name github.env or
// github.path in any case, nor GITHUB_ENV, GITHUB_PATH or a variable of the
// go command or of cgo (a word starting GO or CGO in upper case: GOFLAGS,
// GOEXPERIMENT, GOAMD64, CGO_ENABLED, ...); no line may name
// shift2eu-server or openapi, in any case, since the release publishes the CLI
// alone (issue #393); and no line may name HTTPS_PROXY, HTTP_PROXY, NO_PROXY,
// SSL_CERT_FILE or SSL_CERT_DIR, in any case, which would change where the
// site deploy's client connects or what it trusts. Read line by line, with
// '...' and "..." string literals skipped as GitHub's expression syntax reads
// them (so a }} inside one does not end the expression, and the word secrets
// inside one is not the context), no ${{ }} expression, in any job or
// comment, may use the secrets context in a form other than an exact
// secrets.NAME or secrets['NAME'] (toJSON(secrets), a name built with
// format(...)), whatever secrets the line's job may name; no line may open a
// ${{ expression it does not close, nor hold a \x, \u or \U escape inside
// one, the two forms a line-by-line reading cannot see through. This rule
// reads raw lines, not YAML: a form it cannot see (a scalar folded or quoted
// so that YAML builds an expression no one line holds) is refused by the
// closed schema above, which holds every env: and with: value and every run:
// line to the exact text listed, and the rule is the backstop beneath it. The site's names are
// confined, comments included, in any case and in both expression forms
// (ctx.NAME, ctx['NAME']): secrets.SCW_ACCESS_KEY and secrets.SCW_SECRET_KEY
// each on exactly one line, its env: line in site-deploy's last step;
// vars.SITE_DEPLOY_ENABLED on exactly the three if: lines and site-skipped's
// env: line; vars.SITE_BUCKET and vars.SITE_REGION on their one env: line in
// that last step each (PRD #297 milestone 3).
func ReleaseWorkflow(src []byte) []string {
	var messages []string

	raw := strings.Split(string(src), "\n")
	for i := range raw {
		raw[i] = strings.TrimSuffix(raw[i], "\r")
		messages = append(messages, forbiddenNames(strings.TrimSpace(raw[i]))...)
		messages = append(messages, retiredNameMessages(strings.TrimSpace(raw[i]))...)
		messages = append(messages, proxyNames(strings.TrimSpace(raw[i]))...)
		messages = append(messages, secretsContextForms(i+1, raw[i])...)
	}

	messages = append(messages, confinedNames(raw)...)

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		return append(messages, fmt.Sprintf("read the release workflow: %v", err))
	}

	return append(messages, releaseSchema(raw, lines)...)
}

// releaseSchema returns a message for every way lines, the content lines of
// the release workflow whose every line is raw, departs from the schema
// ReleaseWorkflow describes.
func releaseSchema(raw []string, lines []yamlread.Line) []string {
	r := &schemaReader{raw: raw}

	if len(lines) == 0 {
		return []string{"the release workflow is empty, so this check cannot tell what goreleaser is asked to do"}
	}

	top := r.exactKeys(lines, "the release workflow", "name", "on", "permissions", "concurrency", "jobs")
	if top == nil {
		return r.messages
	}

	r.scalar(top["name"], "the release workflow's name", exactly("Release"))
	r.emptyMapping(top["permissions"], "the release workflow's permissions")

	if c := r.exactKeys(r.block(top["concurrency"], "concurrency"), "concurrency", "group", "cancel-in-progress"); c != nil {
		r.scalar(c["group"], "concurrency: group", exactly("release"))
		r.scalar(c["cancel-in-progress"], "concurrency: cancel-in-progress", exactly("false"))
	}

	if on := r.exactKeys(r.block(top["on"], "on"), "on", "push"); on != nil {
		if push := r.exactKeys(r.block(on["push"], "on: push"), "on: push", "tags"); push != nil {
			r.list(push["tags"], "on: push: tags", []string{"v*.*.*"})
		}
	}

	keys := make([]string, 0, len(releaseJobs))
	for _, j := range releaseJobs {
		keys = append(keys, j.key)
	}

	jobs := r.exactKeys(r.block(top["jobs"], "jobs"), "jobs", keys...)
	if jobs == nil {
		return r.messages
	}

	for _, j := range releaseJobs {
		r.job(jobs[j.key], "jobs: "+j.key, j)
		r.jobSecrets(jobs[j.key], "jobs: "+j.key, j.secrets)
	}

	return r.messages
}

// secretRef matches a reference to a secret in an expression, in either form
// GitHub reads: secrets.NAME and secrets['NAME'] (or "NAME"), any case.
var secretRef = regexp.MustCompile(`(?i)secrets\s*(?:\.\s*([A-Za-z_][A-Za-z0-9_-]*)|\[\s*['"]([^'"]*)['"]\s*\])`)

// secretsWord matches the secrets context's name, any case; secretsContextForms
// skips a match that is part of a longer name.
var secretsWord = regexp.MustCompile(`(?i)secrets`)

// expressionEscape matches a YAML escape that spells a character by its code
// (\x, \u, \U): inside a double-quoted scalar it becomes a character of the
// expression GitHub reads, one this line-by-line check never sees.
var expressionEscape = regexp.MustCompile(`\\[xuU]`)

// secretsContextForms returns a message for every ${{ }} expression of line,
// the num-th, that uses the secrets context in any form but an exact
// secrets.NAME or secrets['NAME'] reference (any case): toJSON(secrets) hands
// out the whole context and secrets[format(...)] builds a name at run time, so
// no closed list of names could tell which secret such a line reaches. It also
// refuses the two forms it cannot read: an expression the line opens and does
// not close (one split over lines, as a folded scalar can), and one that holds
// a \x, \u or \U escape, which YAML turns into characters of the expression.
// The word inside a string literal is not the context and is not reported.
func secretsContextForms(num int, line string) []string {
	var messages []string

	for _, e := range lineExpressions(line) {
		if !e.closed {
			messages = append(messages, fmt.Sprintf(
				"line %d: %s opens a ${{ expression it does not close on that line, so this check cannot read which secrets it names",
				num, strings.TrimSpace(line)))

			continue
		}

		if expressionEscape.MatchString(e.text) {
			messages = append(messages, fmt.Sprintf(
				"line %d: %s holds an escape inside a ${{ }} expression (\\x, \\u or \\U), which YAML turns into characters of the expression this check cannot read",
				num, strings.TrimSpace(e.text)))

			continue
		}

		for _, m := range secretsWord.FindAllStringIndex(e.code, -1) {
			if partOfName(e.code, m[0]-1, ".") || partOfName(e.code, m[1], "") {
				continue
			}

			if ref := secretRef.FindStringIndex(e.text[m[0]:]); ref != nil && ref[0] == 0 {
				continue
			}

			messages = append(messages, fmt.Sprintf(
				"line %d: %s names the secrets context in a form other than an exact secrets.NAME or secrets['NAME'], so no list of names can tell which secret it reaches",
				num, strings.TrimSpace(e.text)))

			break
		}
	}

	return messages
}

// lineExpression is one ${{ }} expression of a line.
type lineExpression struct {
	// text is the expression between ${{ and }}, or to the line's end when it
	// is not closed.
	text string
	// code is text with the content of every string literal blanked to
	// spaces, byte for byte, so an offset into code is one into text.
	code string
	// closed reports whether the line closes the expression.
	closed bool
}

// lineExpressions returns the ${{ }} expressions of line, read as GitHub's
// expression syntax reads them: a '...' string literal, in which a doubled
// single quote is one quote, runs to its closing quote, and so does a "..."
// one, so a }} inside a literal does not end the expression.
func lineExpressions(line string) []lineExpression {
	var expressions []lineExpression

	for {
		start := strings.Index(line, "${{")
		if start < 0 {
			return expressions
		}

		body := line[start+len("${{"):]
		code := []byte(body)
		end := -1

		for i := 0; i < len(body) && end < 0; i++ {
			switch c := body[i]; {
			case c == '\'' || c == '"':
				i = literalEnd(body, i, code)
			case c == '}' && i+1 < len(body) && body[i+1] == '}':
				end = i
			}
		}

		if end < 0 {
			return append(expressions, lineExpression{text: body, code: string(code)})
		}

		expressions = append(expressions, lineExpression{text: body[:end], code: string(code[:end]), closed: true})
		line = body[end+len("}}"):]
	}
}

// literalEnd returns the index of the quote that closes the string literal
// opening at body[open], or the last index of body when none does, and blanks
// the literal's content in code. A doubled quote inside the literal is one
// quote of its content.
func literalEnd(body string, open int, code []byte) int {
	quote := body[open]

	for i := open + 1; i < len(body); i++ {
		if body[i] != quote {
			code[i] = ' '

			continue
		}

		if i+1 < len(body) && body[i+1] == quote {
			code[i], code[i+1] = ' ', ' '
			i++

			continue
		}

		return i
	}

	return len(body) - 1
}

// partOfName reports whether text's byte at i, if there is one, is a character
// of an expression name or one of also, so that a match beside it is part of a
// longer name or a property of another object.
func partOfName(text string, i int, also string) bool {
	if i < 0 || i >= len(text) {
		return false
	}

	c := text[i]

	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.IndexByte(also, c) >= 0
}

// jobSecrets refuses every line of the job e, comments included, that names a
// secret other than allowed (any case); nil allowed checks nothing.
func (r *schemaReader) jobSecrets(e yamlread.Entry, path string, allowed []string) {
	if allowed == nil || e.Num < 1 {
		return
	}

	last := e.Num
	if len(e.Block) != 0 {
		last = e.Block[len(e.Block)-1].Num
	}

	for num := e.Num; num <= last && num <= len(r.raw); num++ {
		line := r.raw[num-1]

		for _, m := range secretRef.FindAllStringSubmatch(line, -1) {
			name := m[1] + m[2]
			if !slices.ContainsFunc(allowed, func(a string) bool { return strings.EqualFold(a, name) }) {
				r.fail("line %d: %s names the secret %s (%q), want only %s, so no secret but the ones the job needs reaches it",
					num, path, name, strings.TrimSpace(line), namesOrNone(allowed))
			}
		}
	}
}

// namesOrNone lists names, or says that there are none.
func namesOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}

	return strings.Join(names, ", ")
}

// schemaReader collects the messages of a schema check.
type schemaReader struct {
	// raw are the document's lines, line ends removed, from which a literal
	// block is read.
	raw      []string
	messages []string
	// chompedRun reads a step's run: written as a literal block with a
	// chomping indicator (|- or |+) as the literal block (|) it compares
	// equal to, the newline after the last line dropped either way: ci.yml's
	// Test job may write its run: steps so (CITestJobNode), release.yml's
	// closed schema may not.
	chompedRun bool
}

func (r *schemaReader) fail(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

// job checks one job against want: its name, needs, runner, permissions,
// outputs and steps.
func (r *schemaReader) job(e yamlread.Entry, path string, want releaseJob) {
	keys := []string{"name", "runs-on", "permissions", "steps"}
	if want.needs != nil {
		keys = append(keys, "needs")
	}

	if want.outputs != nil {
		keys = append(keys, "outputs")
	}

	keys = append(keys, "timeout-minutes")

	for _, k := range []struct {
		key string
		set bool
	}{
		{"if", want.ifCond != ""},
		{"environment", want.environment != ""},
		{"concurrency", want.concurrency != nil},
	} {
		if k.set {
			keys = append(keys, k.key)
		}
	}

	job := r.exactKeys(r.block(e, path), path, keys...)
	if job == nil {
		return
	}

	r.scalar(job["name"], path+": name", exactly(want.name))
	r.scalar(job["runs-on"], path+": runs-on", exactly("ubuntu-latest"))
	r.scalar(job["timeout-minutes"], path+": timeout-minutes", exactly(want.timeout))

	if want.needs != nil {
		r.needs(job["needs"], path+": needs", want.needs)
	}

	if want.ifCond != "" {
		r.scalar(job["if"], path+": if", exactly(want.ifCond))
	}

	if want.environment != "" {
		r.scalar(job["environment"], path+": environment", exactly(want.environment))
	}

	if want.concurrency != nil {
		r.exactMapping(job["concurrency"], path+": concurrency", want.concurrency)
	}

	if want.emptyPermissions {
		r.emptyMapping(job["permissions"], path+": permissions")
	} else {
		r.exactMapping(job["permissions"], path+": permissions", want.permissions)
	}

	if want.outputs != nil {
		r.exactMapping(job["outputs"], path+": outputs", want.outputs)
	}

	stepsPath := path + ": steps"

	block := r.block(job["steps"], stepsPath)
	if block == nil {
		return
	}

	items, err := yamlread.Sequence(block)
	if err != nil {
		r.fail("%s: %v", stepsPath, err)

		return
	}

	if len(items) != len(want.steps) {
		whats := make([]string, 0, len(want.steps))
		for _, s := range want.steps {
			whats = append(whats, s.what)
		}

		r.fail("%s holds %d steps, want exactly %d: %s, so that nothing the check never lists runs in this job",
			stepsPath, len(items), len(want.steps), strings.Join(whats, ", "))

		return
	}

	for i, it := range items {
		r.step(it, fmt.Sprintf("%s: step %d", stepsPath, i+1), want.steps[i])
	}
}

// needs checks that e's value is exactly the jobs want, in that order: a flow
// or block list, or one job name on the key's own line.
func (r *schemaReader) needs(e yamlread.Entry, path string, want []string) {
	var (
		got []string
		err error
	)

	if e.Value == "" || strings.HasPrefix(e.Value, "[") {
		got, err = yamlread.List(e)
	} else {
		var one string

		one, err = yamlread.Single(e)
		got = []string{one}
	}

	if err != nil {
		r.fail("%s: %v", path, err)

		return
	}

	if !slices.Equal(got, want) {
		r.fail("line %d: %s is %q, want exactly %q", e.Num, path, got, want)
	}
}

// exactMapping checks that e's value is a block mapping holding exactly the
// keys of want, each with exactly its value.
func (r *schemaReader) exactMapping(e yamlread.Entry, path string, want map[string]string) {
	keys := sortedKeys(want)

	if e.Value == "" && len(e.Block) == 0 {
		pairs := make([]string, 0, len(keys))
		for _, key := range keys {
			pairs = append(pairs, key+": "+want[key])
		}

		r.fail("line %d: %s is empty, want exactly %s", e.Num, path, strings.Join(pairs, ", "))

		return
	}

	if got := r.exactKeys(r.block(e, path), path, keys...); got != nil {
		for _, key := range keys {
			r.scalar(got[key], path+": "+key, exactly(want[key]))
		}
	}
}

// step checks one step against want.
func (r *schemaReader) step(it yamlread.Item, path string, want releaseStep) {
	path = fmt.Sprintf("%s (line %d), want %s", path, it.Num, want.what)

	keys := want.keys()

	if want.noToken {
		for _, l := range it.Lines {
			lower := strings.ToLower(l.Text)
			if strings.Contains(lower, "github_token") || strings.Contains(lower, "gh_token") {
				r.fail("line %d: %s names the job's token (%q): the step that holds the site's key holds no GITHUB_TOKEN or GH_TOKEN", l.Num, path, l.Text)
			}
		}
	}

	step := r.exactKeys(it.Lines, path, keys...)
	if step == nil {
		return
	}

	if want.name != "" {
		r.scalar(step["name"], path+": name", exactly(want.name))
	}

	if want.id != "" {
		r.scalar(step["id"], path+": id", exactly(want.id))
	}

	if want.uses != nil {
		r.scalar(step["uses"], path+": uses", want.uses)
	}

	if want.with != nil {
		withKeys := sortedKeys(want.with)

		// An empty with: is every key missing, said by name: with: alone
		// would leave persist-credentials to the action's default, true.
		if e := step["with"]; e.Value == "" && len(e.Block) == 0 {
			for _, key := range withKeys {
				r.fail("line %d: %s: with holds no %q, want exactly %s", e.Num, path, key, strings.Join(withKeys, ", "))
			}
		} else if with := r.exactKeys(r.block(e, path+": with"), path+": with", withKeys...); with != nil {
			for _, key := range withKeys {
				r.scalar(with[key], path+": with: "+key, want.with[key])
			}
		}
	}

	if want.env != nil {
		r.env(step["env"], path+": env", want.env)
	}

	if want.run != "" {
		if want.runExpressions {
			r.exactRun(step["run"], path+": run", want.run)
		} else {
			r.run(step["run"], path+": run", want.run)
		}
	}
}

// exactRun checks that e's value is exactly the command want, which may hold
// the ${{ }} expressions want itself holds and no other: the scheduled live
// check's one step that pastes its own job's output (SiteCheckWorkflow).
func (r *schemaReader) exactRun(e yamlread.Entry, path, want string) {
	value, err := r.value(e)
	if err != nil {
		r.fail("%s: %v", path, err)

		return
	}

	if value != want {
		r.fail("line %d: %s is %q, want exactly %q", e.Num, path, value, want)
	}
}

// env checks that e's value is exactly the env: mapping want, line by line:
// its keys are upper case, which the mapping reader refuses.
func (r *schemaReader) env(e yamlread.Entry, path string, want []string) {
	env := r.block(e, path)
	if env == nil {
		return
	}

	got := make([]string, 0, len(env))
	for _, l := range env {
		got = append(got, l.Text)
	}

	if !slices.Equal(got, want) {
		r.fail("%s holds %q, want exactly %q, so the step could run with an environment the check never lists", path, got, want)
	}
}

// run checks that e's value is exactly the command want, and refuses a ${{ }}
// expression in it: the runner pastes the expression's value into the script
// before the shell reads it, so a tag or a branch name could run as a command.
func (r *schemaReader) run(e yamlread.Entry, path, want string) {
	if r.chompedRun && (e.Value == "|-" || e.Value == "|+") {
		e.Value = "|"
	}

	value, err := r.value(e)
	if err != nil {
		r.fail("%s: %v", path, err)

		return
	}

	if strings.Contains(value, "${{") {
		r.fail("line %d: %s holds a ${{ }} expression, whose value the runner pastes into the script as code; pass it through env: instead", e.Num, path)
	}

	if value != want {
		r.fail("line %d: %s is %q, want exactly %q", e.Num, path, value, want)
	}
}

// sortedKeys returns the keys of m in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

// exactKeys reads lines as a block mapping holding exactly want, and returns
// its entries by key; nil when it cannot, with the reasons in the messages.
func (r *schemaReader) exactKeys(lines []yamlread.Line, path string, want ...string) map[string]yamlread.Entry {
	// A nil block was reported by block already.
	if lines == nil {
		return nil
	}

	entries, err := yamlread.Mapping(lines)
	if err != nil {
		r.fail("%s: %v", path, err)

		return nil
	}

	got := make(map[string]yamlread.Entry, len(entries))
	ok := true

	for _, e := range entries {
		got[e.Key] = e

		if !slices.Contains(want, e.Key) {
			r.fail("%s holds the key %q (line %d), want only %s", path, e.Key, e.Num, strings.Join(want, ", "))

			ok = false
		}
	}

	for _, key := range want {
		if _, found := got[key]; !found {
			r.fail("%s holds no %q, want exactly %s", path, key, strings.Join(want, ", "))

			ok = false
		}
	}

	if !ok {
		return nil
	}

	return got
}

// block returns the lines of e's value, a block below its key; nil when it is
// not one, with the reason in the messages.
func (r *schemaReader) block(e yamlread.Entry, path string) []yamlread.Line {
	lines, err := yamlread.BlockValue(e, path)
	if err != nil {
		r.fail("%v", err)

		return nil
	}

	return lines
}

// scalar checks that e's value, one scalar or a literal block, is one that
// want matches.
func (r *schemaReader) scalar(e yamlread.Entry, path string, want *regexp.Regexp) {
	value, err := r.value(e)
	if err != nil {
		r.fail("%s: %v", path, err)

		return
	}

	if !want.MatchString(value) {
		r.fail("line %d: %s is %q, want one matching %s", e.Num, path, value, want)
	}
}

// value reads e's value: a literal block (|) as its lines joined by newlines,
// anything else as one scalar on the key's own line.
func (r *schemaReader) value(e yamlread.Entry) (string, error) {
	if e.Value == "|" {
		return yamlread.LiteralBlock(r.raw, e)
	}

	return yamlread.Single(e)
}

// emptyMapping checks that e's value is exactly {}, as written on its line:
// the one raw value the workflows' schemas hold a key to (permissions: {}).
func (r *schemaReader) emptyMapping(e yamlread.Entry, path string) {
	const want = "{}"

	if e.Value != want || len(e.Block) != 0 {
		r.fail("line %d: %s is %q, want exactly %q", e.Num, path, e.Value, want)
	}
}

// list checks that e's value is exactly the list want.
func (r *schemaReader) list(e yamlread.Entry, path string, want []string) {
	got, err := yamlread.List(e)
	if err != nil {
		r.fail("%s: %v", path, err)

		return
	}

	if !slices.Equal(got, want) {
		r.fail("line %d: %s is %q, want exactly %q", e.Num, path, got, want)
	}
}

// forbiddenNames returns a message for every word of line that names a
// variable of the go command or of cgo, or GITHUB_ENV or GITHUB_PATH, and for
// github.env or github.path in any case.
func forbiddenNames(line string) []string {
	var messages []string

	lower := strings.ToLower(line)

	for _, token := range []string{"github.env", "github.path"} {
		if strings.Contains(lower, token) {
			messages = append(messages, fmt.Sprintf("the release workflow's line %q names %s, through which a step sets the environment or the PATH of the steps after it", line, token))
		}
	}

	words := strings.FieldsFunc(line, func(r rune) bool {
		return r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})

	for _, word := range words {
		switch {
		case word == "GITHUB_ENV" || word == "GITHUB_PATH":
			messages = append(messages, fmt.Sprintf("the release workflow's line %q names %s, through which a step sets the environment of the steps after it, goreleaser's included", line, word))
		case (strings.HasPrefix(word, "GO") || strings.HasPrefix(word, "CGO")) && strings.ToUpper(word) == word:
			messages = append(messages, fmt.Sprintf("the release workflow's line %q names %s, a variable of the go command or of cgo, which would change what goreleaser builds and which the check never lists with", line, word))
		}
	}

	return messages
}

// proxyVariables are the variables that change where an HTTP client connects or
// which certificates it trusts: a step that set one could point the deploy
// program's upload, with the site's key, at a proxy of its own.
var proxyVariables = []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR"}

// proxyNames returns a message for every proxy or certificate variable line
// names, in any case.
func proxyNames(line string) []string {
	var messages []string

	lower := strings.ToLower(line)

	for _, name := range proxyVariables {
		if strings.Contains(lower, strings.ToLower(name)) {
			messages = append(messages, fmt.Sprintf("the release workflow's line %q names %s, which would change where the site deploy's client connects or which certificates it trusts", line, name))
		}
	}

	return messages
}

// confined are the names the release workflow may name only on the lines
// listed, each line as often as it is listed: the site's two secrets on the
// deploy step's env: alone, the deploy's switch in the three conditions and
// the skip notice's env:, and the bucket and the region on the deploy step's
// env: alone (PRD #297 milestone 3). A name counts in any case and in both
// forms an expression reads it by, ctx.NAME and ctx['NAME'].
var confined = []struct {
	ctx, name string
	lines     []string
	where     string
}{
	{"secrets", "SCW_ACCESS_KEY", []string{siteKeyEnv[0]}, "the env: of site-deploy's last step, " + siteDeployStepName},
	{"secrets", "SCW_SECRET_KEY", []string{siteKeyEnv[1]}, "the env: of site-deploy's last step, " + siteDeployStepName},
	{
		"vars", "SITE_DEPLOY_ENABLED",
		[]string{"if: " + siteDeployIf, "if: " + siteDeployIf, "if: " + siteSkippedIf, siteSkippedEnabledEnv},
		"the if: of site-deploy, site-check and site-skipped, and site-skipped's env:",
	},
	{"vars", "SITE_BUCKET", []string{"SITE_BUCKET: ${{ vars.SITE_BUCKET }}"}, "the env: of site-deploy's last step, " + siteDeployStepName},
	{"vars", "SITE_REGION", []string{"SITE_REGION: ${{ vars.SITE_REGION }}"}, "the env: of site-deploy's last step, " + siteDeployStepName},
}

// confinedNames returns a message for every confined name that raw, the
// document's lines, names on other lines, or more or fewer times, than listed.
// The schema holds each listed line to its place.
func confinedNames(raw []string) []string {
	var messages []string

	for _, c := range confined {
		var got []string

		for _, line := range raw {
			if namesContext(line, c.ctx, c.name) {
				got = append(got, strings.TrimSpace(line))
			}
		}

		want := slices.Sorted(slices.Values(c.lines))
		if !slices.Equal(slices.Sorted(slices.Values(got)), want) {
			messages = append(messages, fmt.Sprintf("the release workflow names %s.%s on the lines %q, want exactly %q: %s, and nowhere else",
				c.ctx, c.name, got, c.lines, c.where))
		}
	}

	return messages
}

// namesContext reports whether line names ctx's name in an expression, as
// ctx.name or ctx['name'] (or "name"), in any case and with any spaces.
func namesContext(line, ctx, name string) bool {
	squeezed := strings.ToLower(strings.Join(strings.Fields(line), ""))
	ctx, name = strings.ToLower(ctx), strings.ToLower(name)

	for _, form := range []string{ctx + "." + name, ctx + "['" + name + "']", ctx + `["` + name + `"]`} {
		if strings.Contains(squeezed, form) {
			return true
		}
	}

	return false
}

// strayReleaseFiles are the names at the repository root, slash-separated,
// that change what goreleaser builds without the check reading them, each
// with the reason.
var strayReleaseFiles = []struct {
	name   string
	reason string
}{
	{name: ".config/goreleaser.yml", reason: "it is one of the other file names goreleaser looks for, so the release could build from a document the check never reads"},
	{name: ".config/goreleaser.yaml", reason: "it is one of the other file names goreleaser looks for, so the release could build from a document the check never reads"},
	{name: ".goreleaser.yml", reason: "it is one of the other file names goreleaser looks for, so the release could build from a document the check never reads"},
	{name: "goreleaser.yml", reason: "it is one of the other file names goreleaser looks for, so the release could build from a document the check never reads"},
	{name: "goreleaser.yaml", reason: "it is one of the other file names goreleaser looks for, so the release could build from a document the check never reads"},
	{name: "go.work", reason: "the go command goreleaser runs reads it and the check lists with GOWORK=off, so the release could build modules the check never lists"},
	{name: "go.work.sum", reason: "it comes with a go.work, which the go command goreleaser runs reads and the check lists with GOWORK=off"},
	{name: "vendor", reason: "the go command goreleaser runs builds from it and the check lists with -mod=readonly, so the release could build sources the check never lists"},
}

// StrayReleaseFiles returns a message for every file under root that git
// could commit and that changes what goreleaser builds without the check
// reading it: a goreleaser document other than .goreleaser.yaml, go.work,
// go.work.sum, or anything in vendor. It decides by what `git ls-files
// --cached --others --exclude-standard` lists (gitFiles): a tracked file, even
// one gone from the working tree or added with --force over .gitignore, and an
// untracked file git does not ignore. A developer's ignored go.work never
// reaches the release, which builds from a fresh checkout, so it is not
// refused. Symlinks are listed by git as files and count too.
//
// It fails closed: when git cannot run or cannot list root, it returns an
// error.
func StrayReleaseFiles(ctx context.Context, root string) ([]string, error) {
	names, err := gitFiles(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("find stray release files under %s: %w", root, err)
	}

	var messages []string

	for _, stray := range strayReleaseFiles {
		if slices.ContainsFunc(names, func(name string) bool {
			return name == stray.name || strings.HasPrefix(name, stray.name+"/")
		}) {
			messages = append(messages, stray.name+" is one git could commit: "+stray.reason)
		}
	}

	return messages, nil
}
