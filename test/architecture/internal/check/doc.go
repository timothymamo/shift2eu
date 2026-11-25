// Package check holds the package-level architecture checks that
// test/architecture runs against this repository (issue #40, ADR 0012).
//
// The checks work on packages, not file names. ListPackages lists packages and
// every package they depend on with go list, for one release platform, with
// cgo off or on and with a set of build tags, in an environment built from an
// allow list so that nothing outside its arguments changes the listing.
// Packages holds each package of the module to the allow list of every
// depguard rule whose directory covers it and does not exclude it, and, for a
// rule that holds test files, the imports of its _test.go files too (issue
// #79). A rule whose files: list names the test files of a directory alone
// (the pair **/<dir>/**/*_test.go and **/<dir>/*_test.go, RuleFiles.TestsOnly)
// holds only those imports (issue #144); a package held by such rules alone is
// reported, since then no rule holds its own imports. AllowList follows a deny
// list written as an alias of another rule's anchor. An import is matched as depguard v2.2.1 matches it: the entries are
// sorted as written and only the one nearest before the import decides, exact
// for an entry ending in $, by string prefix for one ending in /. AllowList
// refuses a prefix entry that does not end in /, and a rule whose deny list
// names a path its allow list allows, since depguard then decides by entry
// length. A third-party package that in-module packages import only where a
// strict rule holds and allows the import, and everything it imports that no
// in-module package imports directly, is exempt: depguard lints no file of a
// dependency. Any other package outside the module that the layers reach is
// reported. The exemption reads the strict allow lists only: the lax deny
// rules (go-cmp-only-in-tests, cloud-sdks-only-in-provider-adapters) are held
// by depguard in make lint alone. A third-party import in internal/cli is
// refused by depguard alone, whose strict rule allows only $gostd and named
// module packages. One in cmd/shift2eu is refused by the strict rule
// composition-root (issue #101), which the architecture tests hold the
// composition root to with Packages, apart from the shared layer listing.
// The exemption trusts the import path, which is sound because the root
// go.mod, like every other tracked one outside testdata, holds no replace
// directive, read with the go command's own parser
// (TestModulesHaveNoReplace), and the repository holds no go.work,
// go.work.sum or vendor directory (TestRepositoryHasNoWorkspaceOrVendor), any
// of which could otherwise substitute code for an allowed import path. A
// package only a test imports is not listed by go list -deps, so it is not
// placed itself. Sources holds every package of the module, layer or not, to
// the rules that do not depend on a rule: it must belong to the main module,
// sit in the directory its import path names with symlinks resolved, and hold
// no cgo, C, assembly or other non-Go source.
// FindLineDirectives reports every line directive in a Go file git tracks or
// would add under a root, and every symlink among those files.
// StrayReleaseFiles and ReleaseWorkflow pin that the release builds what the
// check lists: no other goreleaser document, go.work or vendor at the root that
// git could commit, and a release workflow read as a closed schema of one
// release at a time and sixteen jobs, each with its own timeout-minutes: the
// tests, behind the guard that the tagged commit is on main; a latest job,
// read-only, that decides once per run whether the tag is the highest release
// tag on main, the one decision the image's latest tag and GitHub's "Latest"
// release both follow (issue #173); a site job and a site-browser job (PRD #297
// milestone 3), read-only, with no secret and no OIDC token, that build the
// site's bundle with make site, give its SHA-256 as an output, and smoke-test
// that very archive with make test-browser TEST_BROWSER_BUNDLE; a goreleaser
// job that runs no repository code, sets no environment for the go command,
// checks the site's bundle against the site job's digest, uploads to a draft
// release and hands the CLI's archives and checksums.txt on as a workflow
// artifact; an attest-archives job that holds checksums.txt to the one bundle
// of that digest and attests every archive, the bundle and checksums.txt with
// GitHub build provenance and cannot write the repository; a publish job,
// contents: write alone, that publishes the draft last (issues #156, #163),
// only once the bundle passed its smoke test, with no checkout, GitHub's
// "Latest" release only when the latest job decided so; for the CLI's image
// (the HTTP API's is gone, issue #393), an image job that builds and pushes
// the image and can do nothing more, an sbom job that can only read the
// registry, and a sign job that signs and attests the image by digest with
// sigstore's and GitHub's actions alone; and a verify job on a fresh runner,
// read-only to the registry and the attestations, with no OIDC token and no
// checkout, that verifies the published image by digest and the tag rule
// (issue #99), and a verify-archives job beside it, as read-only, that
// verifies the published assets' digests and attestations (issue #163) and,
// reading the highest release tag again on its own, that GitHub's "Latest"
// release follows it and is never a pre-release (issue #173); and the site's
// deploy (PRD #297 milestone 3, ADR 0042): a site-programs job, read-only and
// secretless, that builds ci/sitedeploy and ci/sitecheck with make
// site-programs and gives their SHA-256 as outputs; a site-deploy job, the one
// job that holds the site's key, run only for the highest release tag on main
// while vars.SITE_DEPLOY_ENABLED is 'true', behind the site environment and a
// concurrency group of its own, contents: read and attestations: read alone,
// that deploys the published, non-draft release's bundle once it matches the
// site job's digest and its attestation for this tag and commit, with the
// higher releases' manifests beside it; a site-check job on the same
// condition, contents: read alone, that checks the live site against that
// bundle's manifest; and a site-skipped job, with no permission, that says why
// the deploy did not run. The two Scaleway secrets are named on one line each,
// the deploy step's env:, the switch only in the three conditions and the skip
// notice's env:, the bucket and region only in the deploy step's env:, and no
// line names a proxy or certificate variable.
// WorkflowPrivileges keeps every other
// workflow from publishing: no packages, id-token or attestations write, no
// write-all, no trigger a tag runs (push of tags, create, release,
// workflow_run), and a top-level permissions: always. DryRunMirrorsRelease
// holds ci.yml's image job, the release's dry run, to the release's build
// settings for each image, tag rules, latest decision, SBOM settings, action
// pins and BuildKit image, and to the verify job's container run of each
// image; ci.yml's test job to running make site and make site-programs before
// the archive check, release.yml's site-programs job to running make
// site-programs, and
// its browser job to the release's own path to a real Chromium, in order: make
// site, the archive unpacked into bin/release-bundle as release.yml's
// site-browser job unpacks it (without its digest check, under the untagged
// version dev), and make test-browser TEST_BROWSER_BUNDLE=bin/release-bundle,
// the step that job runs on the release's own bundle, at the same checkout and
// setup-go commits (PRD #297 milestone 3).
//
// The four Node steps through which the Test jobs of ci.yml and release.yml
// run TestPiEnvDenyExtensionLoads on the Node dev/lima.yaml pins (issue #548,
// ADR 0044) are declared once, by nodeSteps (issue #554): the read of
// dev/lima.yaml's nodeVersion with its strict X.Y.Z check, actions/setup-node
// pinned by commit installing that version with its package-manager cache off,
// the check that the node on the PATH is that one, and the tests with
// SHIFT2EU_REQUIRE_NODE: "1". The two workflows differ only in the file the
// check's error names and in the name and command of the step the tests run in.
// ReleaseWorkflow's closed schema holds release.yml's Test job to them;
// CITestJobNode holds ci.yml's Test job, read with yamlread, to exactly one of
// each, compared with the declaration by the step comparison release.yml's
// steps go through, so with exactly the declared keys and values, in that
// order. The job's keys are closed to name, runs-on and steps and the
// workflow's top level to name, on, permissions, concurrency and jobs, so no
// if:, needs:, continue-on-error:, defaults: or env: can skip the job or make
// its run: steps no-ops; each part an edit could break is also named on its
// own. PinnedNodeVersion reads dev/lima.yaml's nodeVersion from the same
// pattern as the workflows' sed line, for TestPiEnvDenyExtensionLoads to name
// the Node it needs, and refuses a document with no such line or more than
// one, as the workflows' read step does.
//
// AuditWorkflow holds the scheduled security audit's workflow,
// .github/workflows/scheduled-security-audit.yml, to PRD #175 milestone 2 and
// ADR 0040: workflow_dispatch with no inputs as its only trigger, the one guard
// condition verbatim in every job's if: and in the concurrency group (which
// falls through to a group of the run's own and never cancels), permissions:
// {} at the top and exactly each job's read scopes, the security-audit
// environment, ubuntu-24.04 and a timeout below 360 on every job and a timeout
// on every step. Each job's steps are a closed list, recognised by name and held
// in order, with the commands each must run, none missing. Only
// checkout, upload-artifact and the App token action run, each pinned by commit
// with a version comment; no Actions cache, job output, set -x, ${{ }} in a
// script, path under /tmp or /run, secrets[, toJSON(secrets) or secrets:
// inherit. Every secret, variable, github.token and App token output is read on
// the one line its step's spec allows, every Go command after the module
// download runs with GOFLAGS=-mod=readonly GOPROXY=off, every tool version has
// a Renovate comment and every downloaded one a SHA-256. Two ciphertexts
// cross, by exact name with a one-day retention. The raw lines also hold the
// model job free of every check and container, so a document the YAML reader
// refuses still draws the message that names them.
//
// ReleasePleaseWorkflow and ReleasePleaseConfig hold the workflow that
// proposes the next release, .github/workflows/release-please.yml, and the
// document it reads, release-please-config.json, to issues #142, #157 and
// #159 (ADR 0021). The workflow runs on a push to main alone, with
// permissions: {} at the top level, job permissions that only read, and a
// concurrency group of its own that never cancels a run in progress; it runs
// only the App token action and release-please, pinned by commit, on
// ubuntu-latest alone, in three jobs. The release-please job, in the
// release-please environment, mints the release-please App's token with
// exactly Contents and Pull requests write and runs release-please with it.
// The tag job, in the release-tag environment, mints the release-tag App's
// token with exactly Contents write and Pull requests read, waits with
// GITHUB_TOKEN for no release.yml run to be requested, waiting, pending,
// queued or in progress, and creates the tag through the API in a step whose
// env: is exactly that token, the repository and the release-please App's
// slug as a constant, never an output of the release-please job, which
// declares none, and which calls only the release PR's, the manifest's, the
// merge commit's and the tag's API endpoints: no Release, no label, no gh
// release, no curl. The guard's env: is as exact: GITHUB_TOKEN, the
// repository and the same slug. The relabel job, in the release-please
// environment, relabels the PR the tag job tagged with the release-please
// App's token. Each token step passes client-id, never app-id, from its own
// App's variable and secret; no other variable or secret is read, and each
// token reaches its job's working step alone. The only needs expressions are
// the tag job's two outputs, which the relabel job reads. The workflow
// publishes nothing and pastes no expression but github.repository into a
// script.
// ReleaseAppIdentifiers holds the two documents that record the release Apps'
// identifiers, the release-tag skill and ADR 0021, to each other: the same
// release-please App ID and slug, and for the release-tag App either the
// maintainer's placeholders or a numeric ID and a slug, neither of them the
// release-please App's.
// ReleasePleaseDryRun holds ci.yml to running the dry run of the guard, tag
// and relabel steps, .github/scripts/release-please-dry-run.sh, on every pull
// request and every push to main, in a job with no write permission and no
// secret.
// The configuration holds the release-tag
// skill's version rules, vX.Y.Z tags with no component,
// skip-github-release, no key that could move the version or the tag
// unreviewed, and one package at the repository root.
//
// GitHubRelease holds the release section of .goreleaser.yaml to exactly
// draft: true, prerelease: auto and replace_existing_draft: true, so every
// asset is uploaded to a draft that release.yml publishes last (issue #156),
// and a pre-release tag is a GitHub pre-release and never the latest release
// (issue #92), and extra_files of exactly the site's bundle,
// ./bin/shift2eu-site_*.tar.gz (PRD #297 milestone 3), and nothing else: the
// bundled OpenAPI document the release used to publish beside the archives is
// gone (issue #393), so no line of the document may name it, or the HTTP API's
// binary, any more.
//
// GoReleaserArchives and DryRunVerifiesArchives hold the release to shipping
// the CLI in an archive of its own (issue #147, issue #393).
// GoReleaserArchives reads the archives: section of .goreleaser.yaml as a
// closed schema: one entry, with an id: equal to the one build its
// ids: names, keeping the archive's name shift2eu_<version>_<os>_<arch>,
// tar.gz (zip on Windows) with LICENSE, NOTICE, README.md, SECURITY.md and
// docs/, and
// one checksums.txt covering that archive and the site's bundle, its one extra
// file (PRD #297 milestone 3).
// DryRunVerifiesArchives holds ci.yml's test job to one step running
// .github/scripts/verify-archives.sh, which builds a goreleaser snapshot and
// fails unless the archive holds exactly its own binary, after one
// goreleaser-action step that installs (install-only) the GoReleaser
// release.yml's goreleaser job runs: the same commit, distribution and
// version.
//
// Nothing is copied: the release platforms are read from .goreleaser.yaml
// (releasetarget.Read, in test/internal/releasetarget, which test/lint reads
// them with too; ListPackages takes a releasetarget.Target), and the strict
// rules and their allow lists from .golangci.yml (StrictRules, RuleDirs,
// AllowList). These files and the workflows are read with the standard
// library, by the strict line reader of test/internal/yamlread, keyed on
// indentation, that refuses what it does not expect, and the characters YAML
// readers do not agree on, rather than guess at them.
//
// DepguardRules reads every depguard rule of .golangci.yml whole, and
// PinnedRules compares those rules with a pinned copy, naming the rule of every
// difference (issue #80).
//
// HidingSettings reads .golangci.yml for the settings that hide issues from
// make lint without touching a rule, and the ways of turning depguard or
// forbidigo off, and returns one message per setting found, naming it by its
// dotted key (issue #81). The document's shape is an allow list that fails
// closed: version must be "2", and a top-level, run, linters,
// linters.exclusions or issues key, a linters.default or a
// linters.exclusions.presets entry it does not know is refused. It wants
// linters.exclusions.generated: disable, which golangci-lint reads as lax when
// it is absent, and refuses a run.issues-exit-code outside 1..125, Go's
// portable exit range: os.Exit truncates the code to 8 bits, so 256 exits 0
// while lint prints issues. StrayLintConfigs refuses
// a .golangci.json, .golangci.toml or .golangci.yaml at the root that git
// could commit, any of which golangci-lint would read in place of
// .golangci.yml.
//
// NonExecutableScripts lists every file in the git index under a directory
// whose staged content starts with #! and whose mode is 100644, reading
// both from the index with git ls-files -s and git cat-file --batch (issue #125).
//
// HiddenPathPackages returns the module packages in a listing whose path
// below the module path holds an element the go command skips when it walks
// ./...: testdata, or one starting with _ or a dot; HiddenPathMessages turns
// each into a finding, "<root> reaches <pkg>", naming the composition root
// that reaches it directly or transitively (issue #206). A symlinked
// directory, the other spelling ./... skips, is left to FindLineDirectives,
// which reports every symlink, and Sources, which requires a package's
// directory with symlinks resolved to be the one its import path names.
//
// CLIImporterProblems holds the packages whose test files import internal/cli,
// one CLIImportersBuild per build, to the exemption of the depguard rule
// cli-only-in-named-tests (issues #345, #382): a package outside it that does
// so is reported per build, and a package of it that does so in no build, over
// the union of the builds, as a stale exemption.
//
// The package is internal to test/architecture: it is not an API of the module.
package check
