package check_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// The fixture below is .goreleaser.yaml in parts, as the release leaves it
// after issue #393: the top level with the one build the release makes, then
// the one archive entry, then the checksum section.

// goreleaserArchivesTop is .goreleaser.yaml up to its archives:, with the one
// build the release makes: the CLI's (shift2eu). The HTTP API's build is gone
// with the server (issue #393).
const goreleaserArchivesTop = `version: 2

project_name: shift2eu

builds:
  - id: shift2eu
    main: ./cmd/shift2eu
    binary: shift2eu

archives:
`

// cliArchive is the archive entry of the CLI's build: its own id, the build
// it holds (shift2eu alone), the name the CLI's archives have always had,
// goreleaser's default template written out, tar.gz on every platform, zip on
// Windows, and the files the archive ships beside the binary (LICENSE and
// README.md, with the pages under docs/ that README.md links to, issue #149,
// and the sample inventories under examples/ that docs/guide/try-it.md runs
// the CLI on, issue #188).
const cliArchive = `  - id: shift2eu
    ids: [shift2eu]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    files:
      - LICENSE
      - README.md
      - docs/*.md
      - docs/**/*
      - examples/*.md
      - examples/**/*

`

// serverArchive is the archive entry the release used to publish for the HTTP
// API's build (PRD #103): a second entry, holding shift2eu-server alone under
// its own name. Every case that adds it to a document is refused: the release
// ships the CLI alone (issue #393).
const serverArchive = `  - id: shift2eu-server
    ids: [shift2eu-server]
    name_template: "{{ .ProjectName }}-server_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    files:
      - LICENSE
      - README.md
      - docs/*.md
      - docs/**/*
      - examples/*.md
      - examples/**/*

`

// goreleaserChecksum ends the document: one checksums.txt covering the
// release's archive, and nothing beside it: the bundled OpenAPI document the
// release used to publish is gone (issue #393).
const goreleaserChecksum = `checksum:
  name_template: checksums.txt
`

// retiredChecksum is the checksum section with the bundled OpenAPI document
// the release used to publish beside the archives (issue #139) added back, so
// checksums.txt covered it too. The release refuses it now (issue #393).
const retiredChecksum = `checksum:
  name_template: checksums.txt
  extra_files:
    - glob: ./api/openapi.bundled.json
      name_template: shift2eu-server_openapi.json
`

// goreleaserArchives renders .goreleaser.yaml from its parts: the top level,
// the CLI's archive, the second archive entry a case gives ("" for none) and
// the checksum section.
func goreleaserArchives(cli, second string) string {
	return goreleaserArchivesTop + cli + second + goreleaserChecksum
}

// TestGoReleaserArchives pins issue #147, as issue #393 leaves it: the release
// ships the CLI's binary, shift2eu, in one archive of its own, and nothing
// else. check.GoReleaserArchives reads .goreleaser.yaml and returns one
// message per way the document publishes more than that, or moves the binary
// out of the archive its downloads expect:
//
//   - no line, comments included, names shift2eu-server or openapi, in any
//     case: the HTTP API's build, its archive and the bundled OpenAPI document
//     are gone (issue #393), worded with the name the line holds;
//   - archives: holds exactly one entry, and it names exactly one build
//     through ids: an entry without ids takes every build, and every build of
//     builds: sits in that archive: the CLI's, the build whose binary is
//     shift2eu, with the ids of its own build alone. The message names the
//     build that is in no archive, or the id that names no build;
//   - the entry's id: is the one build its ids: names: goreleaser defaults an
//     entry's id to "default", so an entry without id: is the archive
//     "default" to it. An entry with no id:, and an id: that is not the build
//     its ids: names, are refused, worded with id;
//   - its name_template is the one that build's downloads expect:
//     {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }} for the CLI,
//     whose archive names stay exactly as they were. A name_template that is
//     missing or differs is refused, worded with name_template;
//   - the entry is tar.gz on every platform and zip on Windows (formats: and
//     format_overrides:, worded with tar.gz and zip);
//   - it ships exactly LICENSE, README.md, docs/*.md, docs/**/*,
//     examples/*.md and examples/**/* (files:), worded with the entry it does
//     not ship: README.md's links stay inside the archive (issue #149), and
//     the walkthrough in docs/guide/try-it.md finds the sample inventories a
//     release unpacks in examples/ rather than only in a clone (issue #188);
//   - the checksum section holds exactly name_template: checksums.txt, so one
//     checksums.txt covers the archive: a missing section, another name and
//     any other key in it (split:, and extra_files:, which the release no
//     longer publishes) are refused, worded with what it holds instead;
//   - an archive entry holds no key beyond id, ids, name_template, formats,
//     format_overrides and files: the section is a closed schema, so a key
//     the check never lists cannot change what an archive is.
//
// An accepted document returns no message at all.
func TestGoReleaserArchives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		goreleaser string
		// wantIn is a part of the message the check must return; "" means
		// none at all.
		wantIn string
	}{
		// The accepted shape.
		{name: "the CLI's archive alone", goreleaser: goreleaserArchives(cliArchive, "")},

		// The document's line ends do not matter, as in every reader here.
		{
			name:       "the accepted document with CRLF line ends",
			goreleaser: strings.ReplaceAll(goreleaserArchives(cliArchive, ""), "\n", "\r\n"),
		},

		// The names the release stopped publishing (issue #393): the HTTP
		// API's binary, its archive and the bundled OpenAPI document are gone,
		// and no line of the document may name them, a comment included.
		{
			name:       "the HTTP API's archive beside the CLI's",
			goreleaser: goreleaserArchives(cliArchive, serverArchive),
			wantIn:     "shift2eu-server",
		},
		{
			name:       "the HTTP API's build beside the CLI's",
			goreleaser: strings.Replace(goreleaserArchives(cliArchive, ""), "  - id: shift2eu\n    main: ./cmd/shift2eu\n    binary: shift2eu\n", "  - id: shift2eu\n    main: ./cmd/shift2eu\n    binary: shift2eu\n  - id: shift2eu-server\n    main: ./cmd/shift2eu-server\n    binary: shift2eu-server\n", 1),
			wantIn:     "shift2eu-server",
		},
		{
			name:       "a comment naming the HTTP API's binary",
			goreleaser: goreleaserArchives(cliArchive, "") + "# the shift2eu-server archive is gone\n",
			wantIn:     "shift2eu-server",
		},
		{
			name:       "a comment naming the bundled OpenAPI document",
			goreleaser: goreleaserArchives(cliArchive, "") + "# release.extra_files: api/openapi.bundled.json\n",
			wantIn:     "openapi",
		},
		// The name is retired in any case it is written in: a comment that
		// capitalises it names the same build or document, and is refused too.
		{
			name:       "a comment naming the HTTP API's binary in another case",
			goreleaser: goreleaserArchives(cliArchive, "") + "# Shift2eu-Server is gone with the release's second image\n",
			wantIn:     "shift2eu-server",
		},
		{
			name:       "a comment naming the bundled OpenAPI document in another case",
			goreleaser: goreleaserArchives(cliArchive, "") + "# The OpenAPI document is gone\n",
			wantIn:     "openapi",
		},

		// The archives section itself.
		{
			name:       "no archives section",
			goreleaser: strings.TrimSuffix(goreleaserArchivesTop, "archives:\n"),
			wantIn:     "want one entry, the CLI's build (shift2eu)",
		},
		{
			name: "two archive entries",
			goreleaser: goreleaserArchives(
				cliArchive,
				mutate(t, cliArchive, "  - id: shift2eu\n", "  - id: shift2eu-2\n"),
			),
			wantIn: "archives holds 2 entries",
		},
		{
			// An entry without ids takes every build, so the download would
			// hold more than the CLI's binary.
			name: "an archive entry with no ids",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "    ids: [shift2eu]\n    name_template:", "    name_template:"),
				"",
			),
			wantIn: "ids",
		},
		{
			name: "an archive whose ids names two builds",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "ids: [shift2eu]", "ids: [shift2eu, shift2eu-http]"),
				"",
			),
			wantIn: "ids",
		},
		{
			name: "an archive whose ids names no build",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "ids: [shift2eu]", "ids: [shift2eu-http]"),
				"",
			),
			wantIn: "shift2eu-http",
		},

		// The archive id: goreleaser defaults an entry's id to "default", so
		// an entry that leaves it out is another archive to it. The rule is
		// one id per entry, equal to the one build its ids names, so each case
		// below pins that the message names that id: the one the entry wants
		// (shift2eu) or the one it wrongly carries (shift2eu-api). Wording the
		// message with the id is what tells the reader which archive to fix.
		{
			name: "an archive entry with no id",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "  - id: shift2eu\n    ids: [shift2eu]\n", "  - ids: [shift2eu]\n"),
				"",
			),
			wantIn: "shift2eu",
		},
		{
			// The id of an entry is the one build its ids names: here the
			// entry holds shift2eu but is called shift2eu-api, an id no build
			// has.
			name: "an archive whose id differs from its single ids value",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "  - id: shift2eu\n", "  - id: shift2eu-api\n"),
				"",
			),
			wantIn: "shift2eu-api",
		},

		// The archive name: the CLI's must not change.
		{
			name: "a CLI name_template that drifts",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, `"{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"`, `"{{ .ProjectName }}_{{ .Version }}_{{ .Os }}"`),
				"",
			),
			wantIn: "name_template",
		},
		{
			name: "a CLI name_template that is no template",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, `"{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"`, `"shift2eu"`),
				"",
			),
			wantIn: "name_template",
		},
		{
			name: "an archive with no name_template",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "    name_template: \"{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}\"\n", ""),
				"",
			),
			wantIn: "name_template",
		},

		// The formats: tar.gz everywhere, zip on Windows.
		{
			name: "an archive that is not tar.gz",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "    formats: [tar.gz]", "    formats: [zip]"),
				"",
			),
			wantIn: "tar.gz",
		},
		{
			name: "a Windows archive that is not zip",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "    format_overrides:\n      - goos: windows\n        formats: [zip]\n", ""),
				"",
			),
			wantIn: "zip",
		},

		// The files the archive ships beside its binary (issue #149).
		{
			name: "an archive that ships no LICENSE",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "      - LICENSE\n", ""),
				"",
			),
			wantIn: "LICENSE",
		},
		{
			name: "an archive that ships no README.md",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "      - README.md\n", ""),
				"",
			),
			wantIn: "README.md",
		},
		{
			name: "an archive without the pages directly in docs/",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "      - docs/*.md\n", ""),
				"",
			),
			wantIn: "docs/*.md",
		},
		{
			name: "an archive without the pages in the docs/ subdirectories",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "      - docs/**/*\n", ""),
				"",
			),
			wantIn: "docs/**/*",
		},

		// The sample inventories docs/guide/try-it.md runs the CLI on (issue
		// #188): a release someone unpacks holds them too, not only a clone.
		{
			name: "an archive without the sample inventories' README",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "      - examples/*.md\n", ""),
				"",
			),
			wantIn: "examples/*.md",
		},
		{
			name: "an archive without the inventories under examples/",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "      - examples/**/*\n", ""),
				"",
			),
			wantIn: "examples/**/*",
		},

		// The checksum: one file covering the archive.
		{
			name:       "no checksum section",
			goreleaser: goreleaserArchivesTop + cliArchive,
			wantIn:     "checksum",
		},
		{
			name: "a checksum name that is not checksums.txt",
			goreleaser: goreleaserArchivesTop + cliArchive +
				"checksum:\n  name_template: SHA256SUMS.txt\n",
			wantIn: "checksums.txt",
		},
		{
			// split: true publishes one checksums file per archive, so no
			// single checksums.txt would cover the release.
			name: "a checksum section that splits the file",
			goreleaser: goreleaserArchivesTop + cliArchive +
				"checksum:\n  name_template: checksums.txt\n  split: true\n",
			wantIn: "split",
		},

		// The bundle the release published beside the archives (issue #139) is
		// gone (issue #393): a checksum section that still carries it is
		// refused rather than read as a covered file.
		{
			name:       "a checksum section with the bundled OpenAPI document",
			goreleaser: goreleaserArchivesTop + cliArchive + retiredChecksum,
			wantIn:     "extra_files",
		},

		// The closed schema.
		{
			name: "an archive entry with a key the check never lists",
			goreleaser: goreleaserArchives(
				mutate(t, cliArchive, "    formats: [tar.gz]\n", "    formats: [tar.gz]\n    wrap_in_directory: true\n"),
				"",
			),
			wantIn: "wrap_in_directory",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := check.GoReleaserArchives([]byte(tc.goreleaser))
			joined := strings.Join(got, "\n")

			if tc.wantIn == "" {
				if len(got) != 0 {
					t.Errorf("GoReleaserArchives() = %q, want no message", got)
				}

				return
			}

			if !strings.Contains(joined, tc.wantIn) {
				t.Errorf("GoReleaserArchives() = %q, want a message containing %q", got, tc.wantIn)
			}
		})
	}
}

// ciGoreleaserInstall is the step of .github/workflows/ci.yml's test job that
// installs the GoReleaser the release runs: the same action commit, the same
// distribution and the same version as release.yml's goreleaser step, installed
// only, so the snapshot that builds the archives in the dry run is the
// GoReleaser that publishes them (issue #147). Its pins are those of the
// repository's ci.yml (workflowPins), as the release fixture's are those of
// its release.yml, so that a Renovate update of both needs no edit here.
var ciGoreleaserInstall = ciPins.expand(`      # The same goreleaser version release.yml runs, installed only: the script below runs a snapshot of
      # the release at this commit, which builds the archives and publishes nothing.
      - uses: {{pin goreleaser/goreleaser-action}}
        with:
          distribution: goreleaser
          version: "{{release goreleaser}}" # exact pin, as release.yml's; Renovate updates it (github-actions manager)
          install-only: true
`)

// ciArchiveStep is the step of .github/workflows/ci.yml's test job that runs
// the archive check's script.
const ciArchiveStep = `      # The release's archives, built by a snapshot run of its GoReleaser
      # configuration: each holds exactly its own binary (issue #147).
      - name: Each archive holds its own binary
        run: .github/scripts/verify-archives.sh
`

// ciArchiveCheck is the part of .github/workflows/ci.yml that issue #147 adds:
// the dry run of the release's archives. The test job, which runs on every pull
// request and every push to main, installs the release's GoReleaser and runs
// .github/scripts/verify-archives.sh, which builds the release's archives and
// fails unless each holds exactly its own binary: the CLI's archive shift2eu
// and nothing else (issue #393).
var ciArchiveCheck = ciPins.expand(`name: CI

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

jobs:
  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: {{pin actions/checkout}}
        with:
          persist-credentials: false
      - uses: {{pin actions/setup-go}}
        with:
          go-version-file: go.mod
`) + ciGoreleaserInstall + ciArchiveStep

// TestDryRunVerifiesArchives pins the other half of issue #147: the CI dry run
// verifies what the release publishes, as ci.yml's image job does for the
// image. check.DryRunVerifiesArchives reads ci.yml and release.yml and returns
// one message unless the test job, jobs: test, holds exactly one step named
//
//	Each archive holds its own binary
//
// whose run: is exactly the one line
//
//	.github/scripts/verify-archives.sh
//
// and nothing else: no literal block, no argument and no ${{ }} expression, so
// that the script, which builds the release's archives with a goreleaser
// snapshot and fails unless each archive holds exactly its own binary, is what
// runs on every pull request and every push to main. A message names the job
// (jobs: test) or the script path it wants, and the step name.
//
// The dry run also installs the GoReleaser the release runs, so that a Renovate
// pull request bumping one workflow cannot let CI check the archives with a
// different GoReleaser than the one that publishes them (issue #147): the test
// job holds exactly one goreleaser/goreleaser-action step, before the archive
// check's step, whose uses: (the full commit), with: distribution: and with:
// version: are release.yml's goreleaser step's, and whose with: install-only:
// is true (the release publishes, the dry run only builds). A message names the
// field that differs (uses, distribution, version, install-only), or the step
// that is missing, repeated or misplaced; a step after the archive check is
// worded with before.
func TestDryRunVerifiesArchives(t *testing.T) {
	t.Parallel()

	release := releaseWorkflowFixture()
	releaseWorkflow := release.render()

	// The GoReleaser pins the fixtures hold, and the drifts the cases below
	// make of them, each derived from the pin it replaces.
	goreleaserAction := ciPins.commitOf("goreleaser/goreleaser-action")
	goreleaser := `version: "` + ciPins.expand("{{release goreleaser}}") + `"`
	releaseGoreleaser := `version: "` + releasePins.expand("{{release goreleaser}}") + `"`
	drifted := func(version string) string {
		return `version: "` + nextRelease(t, strings.TrimSuffix(strings.TrimPrefix(version, `version: "`), `"`)) + `"`
	}

	tests := []struct {
		name string
		ci   string
		// release is release.yml the case compares ci against; "" means
		// releaseWorkflow, unchanged.
		release string
		// wantIn is a part of the message the check must return; "" means
		// none at all.
		wantIn string
	}{
		// The accepted shape.
		{name: "the test job runs the archive check", ci: ciArchiveCheck},
		{
			name: "the accepted workflow with CRLF line ends",
			ci:   strings.ReplaceAll(ciArchiveCheck, "\n", "\r\n"),
		},

		// The step must exist, under its own name.
		{
			name:   "no step verifying the archives",
			ci:     mutate(t, ciArchiveCheck, "      - name: Each archive holds its own binary\n        run: .github/scripts/verify-archives.sh\n", ""),
			wantIn: "Each archive holds its own binary",
		},
		{
			name:   "the step renamed",
			ci:     mutate(t, ciArchiveCheck, "      - name: Each archive holds its own binary\n", "      - name: Check the archives\n"),
			wantIn: "Each archive holds its own binary",
		},

		// The step must run the archive check's script, and run it whole.
		{
			name:   "the step running another script",
			ci:     mutate(t, ciArchiveCheck, "        run: .github/scripts/verify-archives.sh\n", "        run: .github/scripts/other.sh\n"),
			wantIn: ".github/scripts/verify-archives.sh",
		},
		{
			name:   "the step running the script with an argument",
			ci:     mutate(t, ciArchiveCheck, "        run: .github/scripts/verify-archives.sh\n", "        run: .github/scripts/verify-archives.sh --all\n"),
			wantIn: ".github/scripts/verify-archives.sh",
		},
		{
			// A literal block is compared line by line, so a script the
			// workflow writes itself would be the check, not the script's.
			name:   "the step running a script of its own",
			ci:     mutate(t, ciArchiveCheck, "        run: .github/scripts/verify-archives.sh\n", "        run: |\n          echo ok\n"),
			wantIn: ".github/scripts/verify-archives.sh",
		},

		// The step must sit in the test job, which runs on every pull request
		// and every push to main.
		{
			name:   "the step in another job",
			ci:     mutate(t, ciArchiveCheck, "  test:\n", "  lint:\n"),
			wantIn: "jobs: test",
		},

		// The dry run installs the GoReleaser the release runs: the same
		// action commit, distribution and version, installed only.
		{
			name:   "no goreleaser-action step in the test job",
			ci:     mutate(t, ciArchiveCheck, ciGoreleaserInstall, ""),
			wantIn: "goreleaser/goreleaser-action",
		},
		{
			// The snapshot would not find the action installed at all.
			name:   "the goreleaser-action step after the archive check",
			ci:     mutate(t, ciArchiveCheck, ciGoreleaserInstall+ciArchiveStep, ciArchiveStep+ciGoreleaserInstall),
			wantIn: "before",
		},
		{
			// A Renovate pull request bumping ci.yml alone.
			name:   "a goreleaser-action commit that is not the release's",
			ci:     mutate(t, ciArchiveCheck, "@"+goreleaserAction, "@"+nextCommit(t, goreleaserAction)),
			wantIn: "uses",
		},
		{
			name:   "a goreleaser version that is not the release's",
			ci:     mutate(t, ciArchiveCheck, goreleaser, drifted(goreleaser)),
			wantIn: "version",
		},
		{
			name:   "a goreleaser distribution that is not the release's",
			ci:     mutate(t, ciArchiveCheck, "distribution: goreleaser", "distribution: goreleaser-pro"),
			wantIn: "distribution",
		},
		{
			// Renovate bumping release.yml's version instead: the drift is
			// the same, whichever workflow moved.
			name:    "a release version that is not the dry run's",
			ci:      ciArchiveCheck,
			release: release.inJob(t, "goreleaser", releaseGoreleaser, drifted(releaseGoreleaser)),
			wantIn:  "version",
		},
		{
			// Without install-only the step runs the action's own
			// `goreleaser release`, publishing a snapshot to nowhere.
			name:   "a goreleaser-action step that is not install-only",
			ci:     mutate(t, ciArchiveCheck, "          install-only: true\n", ""),
			wantIn: "install-only",
		},
		{
			name:   "an install-only that is false",
			ci:     mutate(t, ciArchiveCheck, "          install-only: true\n", "          install-only: false\n"),
			wantIn: "install-only",
		},
		{
			// Two installs of GoReleaser: one of them may be the wrong one.
			name:   "two goreleaser-action steps",
			ci:     mutate(t, ciArchiveCheck, ciGoreleaserInstall, ciGoreleaserInstall+ciGoreleaserInstall),
			wantIn: "goreleaser/goreleaser-action",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			releaseDoc := tc.release
			if releaseDoc == "" {
				releaseDoc = releaseWorkflow
			}

			got := check.DryRunVerifiesArchives([]byte(tc.ci), []byte(releaseDoc))
			joined := strings.Join(got, "\n")

			if tc.wantIn == "" {
				if len(got) != 0 {
					t.Errorf("DryRunVerifiesArchives() = %q, want no message", got)
				}

				return
			}

			if !strings.Contains(joined, tc.wantIn) {
				t.Errorf("DryRunVerifiesArchives() = %q, want a message containing %q", got, tc.wantIn)
			}
		})
	}
}
