package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// retiredBundle is the extra file the release used to publish beside the
// archives (issue #139): the bundled OpenAPI document under the name its
// release asset had. The release publishes the CLI's archives alone now, so a
// release: section that carries it is refused (issue #393).
const retiredBundle = `  extra_files:
    - glob: ./api/openapi.bundled.json
      name_template: shift2eu-server_openapi.json
`

// releaseSection returns the release: section of .goreleaser.yaml for the
// given prerelease value, with draft: true, replace_existing_draft: true and the
// site's bundle as its one extra file (PRD #297 milestone 3).
func releaseSection(prerelease string) string {
	return "release:\n  draft: true\n  prerelease: " + prerelease + "\n  replace_existing_draft: true\n  extra_files:\n    - glob: ./bin/shift2eu-site_*.tar.gz\n"
}

// TestGitHubRelease pins how the GitHub release is published: release holds
// exactly draft: true, prerelease: auto and replace_existing_draft: true, in
// that order, then extra_files: the one glob of the site's bundle (PRD #297
// milestone 3). Every asset GoReleaser uploads goes to a draft release, which
// locks its assets when it is published
// (issue #156, ADR 0015), so a job of release.yml publishes it last;
// replace_existing_draft: true makes a re-run of a failed goreleaser job
// replace the draft that job left, rather than add a second one for the tag
// (GoReleaser finds a draft release by neither GetReleaseByTag nor a tag
// lookup); and a tag with a pre-release suffix (v0.1.0-rc.2) is a GitHub
// pre-release and never the latest release (issue #92). Every other document
// is refused.
func TestGitHubRelease(t *testing.T) {
	t.Parallel()

	buildFlow := goreleaserFixture(t, "flow")
	buildReal := goreleaserFixture(t, "real")

	tests := []struct {
		name       string
		goreleaser string
		wantErr    bool
	}{
		{
			name:       "draft, prerelease auto and replace_existing_draft are accepted",
			goreleaser: buildFlow + "\n" + releaseSection("auto"),
		},
		{
			name:       "a quoted auto is accepted",
			goreleaser: buildFlow + "\n" + releaseSection(`"auto"`),
		},
		{
			name:       "the real document with its release section is accepted",
			goreleaser: buildReal + "\n" + releaseSection("auto"),
		},
		{
			name:       "CRLF line ends are accepted",
			goreleaser: strings.ReplaceAll("version: 2\n\n"+releaseSection("auto"), "\n", "\r\n"),
		},
		{
			name:       "a comment after the value is accepted",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto # rc tags\n  replace_existing_draft: true # a failed run's draft is replaced\n",
		},
		{
			// A re-run of a failed goreleaser job leaves the draft the failed
			// run created. GoReleaser finds an existing release by its tag with
			// GetReleaseByTag, which skips drafts, so without this key it
			// creates a second draft for the tag instead of replacing the first.
			name:       "a release section without replace_existing_draft is refused: a re-run would leave two drafts for the tag",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n",
			wantErr:    true,
		},
		{
			name:       "replace_existing_draft false is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: false\n",
			wantErr:    true,
		},
		{
			name:       "a quoted replace_existing_draft is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: \"true\"\n",
			wantErr:    true,
		},
		{
			name:       "replace_existing_draft before prerelease is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  replace_existing_draft: true\n  prerelease: auto\n",
			wantErr:    true,
		},
		{
			// A published release's assets cannot be replaced, so goreleaser
			// must upload them to a draft and publish it last (issue #156). Its
			// default publishes the release, assets and all, at once.
			name:       "a release section without draft is refused: goreleaser's default publishes the assets it uploads",
			goreleaser: buildFlow + "\nrelease:\n  prerelease: auto\n  replace_existing_draft: true\n",
			wantErr:    true,
		},
		{
			name:       "draft false is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: false\n  prerelease: auto\n",
			wantErr:    true,
		},
		{
			name:       "an upper-case True draft is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: True\n  prerelease: auto\n",
			wantErr:    true,
		},
		{
			name:       "an empty draft is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft:\n  prerelease: auto\n",
			wantErr:    true,
		},
		{
			name:       "a draft from an expression is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: '{{ .Env.DRAFT }}'\n  prerelease: auto\n",
			wantErr:    true,
		},
		{
			name:       "no release section is refused: goreleaser's default publishes every tag as a normal release",
			goreleaser: buildFlow,
			wantErr:    true,
		},
		{
			name:       "an empty release section is refused",
			goreleaser: buildFlow + "\nrelease:\n",
			wantErr:    true,
		},
		{
			name:       "a release section without prerelease is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n",
			wantErr:    true,
		},
		{
			name:       "prerelease false is refused",
			goreleaser: buildFlow + "\nrelease:\n  prerelease: false\n",
			wantErr:    true,
		},
		{
			name:       "prerelease true is refused",
			goreleaser: buildFlow + "\nrelease:\n  prerelease: true\n",
			wantErr:    true,
		},
		{
			name:       "an upper-case Auto is refused",
			goreleaser: buildFlow + "\nrelease:\n  prerelease: Auto\n",
			wantErr:    true,
		},
		{
			name:       "a templated prerelease is refused",
			goreleaser: buildFlow + "\nrelease:\n  prerelease: '{{ .Env.PRERELEASE }}'\n",
			wantErr:    true,
		},
		{
			name:       "an empty prerelease is refused",
			goreleaser: buildFlow + "\nrelease:\n  prerelease:\n",
			wantErr:    true,
		},
		{
			name:       "an anchored auto is refused",
			goreleaser: buildFlow + "\nrelease:\n  prerelease: &a auto\n",
			wantErr:    true,
		},
		{
			name:       "make_latest beside prerelease is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: true\n  make_latest: true\n",
			wantErr:    true,
		},
		{
			name:       "any other release key beside draft, prerelease and replace_existing_draft is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: true\n  mode: replace\n",
			wantErr:    true,
		},
		{
			name:       "a flow mapping release is refused",
			goreleaser: buildFlow + "\nrelease: {prerelease: auto}\n",
			wantErr:    true,
		},
		{
			name:       "a repeated release key is refused",
			goreleaser: buildFlow + "\nrelease:\n  prerelease: auto\nrelease:\n  prerelease: false\n",
			wantErr:    true,
		},
		{
			name:       "a prerelease key outside release is not read as the setting",
			goreleaser: buildFlow + "\nchangelog:\n  prerelease: auto\n",
			wantErr:    true,
		},
		{
			name:       "an empty document is refused",
			goreleaser: "",
			wantErr:    true,
		},

		// The extra file the release published beside the archives (issue
		// #139) is gone (issue #393): the release ships the CLI's archives,
		// their checksums.txt and the site's bundle alone, so release: may hold
		// no extra_files but the bundle's one glob, and no line, a comment
		// included, may name the retired bundle or shift2eu-server.
		{
			name:       "a release section without extra_files is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: true\n",
			wantErr:    true,
		},
		{
			name:       "a release extra file with a second glob is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: true\n  extra_files:\n    - glob: ./bin/shift2eu-site_*.tar.gz\n    - glob: ./bin/other.tar.gz\n",
			wantErr:    true,
		},
		{
			name:       "a release extra file of another glob is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: true\n  extra_files:\n    - glob: ./bin/*\n",
			wantErr:    true,
		},
		{
			name:       "a release extra file with a name_template is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: true\n  extra_files:\n    - glob: ./bin/shift2eu-site_*.tar.gz\n      name_template: site.tar.gz\n",
			wantErr:    true,
		},
		{
			name:       "a release section with the bundled OpenAPI document is refused",
			goreleaser: buildFlow + "\n" + releaseSection("auto") + retiredBundle,
			wantErr:    true,
		},
		{
			name:       "a release section with an empty extra_files is refused",
			goreleaser: buildFlow + "\nrelease:\n  draft: true\n  prerelease: auto\n  replace_existing_draft: true\n  extra_files:\n",
			wantErr:    true,
		},
		{
			name:       "a comment naming the bundled OpenAPI document is refused",
			goreleaser: buildFlow + "\n# extra_files: ./api/openapi.bundled.json\n" + releaseSection("auto"),
			wantErr:    true,
		},
		{
			name:       "a comment naming the HTTP API's binary is refused",
			goreleaser: buildFlow + "\n# the shift2eu-server archive is gone\n" + releaseSection("auto"),
			wantErr:    true,
		},
		// The name is retired in any case it is written in: a comment that
		// capitalises it names the same bundle or binary, and is refused too.
		{
			name:       "a comment naming the bundled API document in another case is refused",
			goreleaser: buildFlow + "\n# The OpenAPI document is gone\n" + releaseSection("auto"),
			wantErr:    true,
		},
		{
			name:       "a comment naming the HTTP API's binary in another case is refused",
			goreleaser: buildFlow + "\n# Shift2eu-Server is gone\n" + releaseSection("auto"),
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := check.GitHubRelease([]byte(tc.goreleaser))
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("GitHubRelease() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// goreleaserFixture returns the .goreleaser.yaml document name of
// test/internal/releasetarget/testdata, a snapshot of the shape of this
// repository's .goreleaser.yaml (before hooks, archives, checksum and
// changelog around one build) that releasetarget.Read accepts, so a case here
// only adds a release section to a build the release-target reader reads.
// test/architecture/repository_test.go checks the real .goreleaser.yaml itself.
func goreleaserFixture(t *testing.T, name string) string {
	t.Helper()

	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "internal", "releasetarget", "testdata", name+".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	return string(doc)
}
