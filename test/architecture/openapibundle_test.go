package architecture_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
	"github.com/timothymamo/shift2eu/test/internal/openapibundle"
)

// TestTheReleasePublishesNoBundledDocument pins what issue #393 leaves of
// issue #139: the bundled OpenAPI document, api/openapi.bundled.json, is still
// built and pinned (test/openapibundle does that), but the release no longer
// publishes it. The release ships the CLI's archives and their checksums.txt
// alone, so .goreleaser.yaml names neither the bundle's path nor the asset name
// it used to be published as, and both release checks refuse a document that
// carries the extra_files entry again.
func TestTheReleasePublishesNoBundledDocument(t *testing.T) {
	t.Parallel()

	goreleaser := string(readRepositoryFile(t, ".goreleaser.yaml"))

	for _, retired := range []string{openapibundle.Path, "openapi"} {
		if bytes.Contains([]byte(goreleaser), []byte(retired)) {
			t.Errorf(".goreleaser.yaml names %q, want no mention of the bundled OpenAPI document: the release publishes the CLI's archives and checksums.txt alone (issue #393)", retired)
		}
	}

	if err := check.GitHubRelease([]byte(goreleaser)); err != nil {
		t.Errorf("GitHubRelease(.goreleaser.yaml): %v", err)
	}

	if messages := check.GoReleaserArchives([]byte(goreleaser)); len(messages) != 0 {
		t.Errorf("GoReleaserArchives(.goreleaser.yaml) = %q, want no message", messages)
	}

	// The entry issue #139 added is refused wherever a case adds it back: the
	// release: section and the checksum section both carried it.
	bundleEntry := "  extra_files:\n    - glob: ./" + openapibundle.Path + "\n      name_template: shift2eu-server_openapi.json\n"

	releaseSection := "  replace_existing_draft: true\n"
	if !strings.Contains(goreleaser, releaseSection) {
		t.Fatalf(".goreleaser.yaml holds no release: section ending in %q, so this test cannot add the bundle's extra file back", strings.TrimSpace(releaseSection))
	}

	withBundle := strings.Replace(goreleaser, releaseSection, releaseSection+bundleEntry, 1)

	if err := check.GitHubRelease([]byte(withBundle)); err == nil {
		t.Errorf("GitHubRelease() of .goreleaser.yaml with the bundle's extra_files = nil, want an error: the release publishes no file beside the CLI's archives (issue #393)")
	}

	checksumSection := "checksum:\n  name_template: checksums.txt\n"
	if !strings.Contains(goreleaser, checksumSection) {
		t.Fatalf(".goreleaser.yaml holds no %q section, so this test cannot add the bundle's extra file back", "checksum")
	}

	withChecksumBundle := strings.Replace(goreleaser, checksumSection, checksumSection+bundleEntry, 1)

	if messages := check.GoReleaserArchives([]byte(withChecksumBundle)); len(messages) == 0 {
		t.Errorf("GoReleaserArchives() of .goreleaser.yaml with the bundle's extra_files = no messages, want one: checksums.txt covers the CLI's archives alone (issue #393)")
	}
}
