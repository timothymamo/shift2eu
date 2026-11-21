package check

import (
	"errors"
	"fmt"
	"strings"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// GitHubRelease checks how a .goreleaser.yaml document publishes the GitHub
// release: its top-level release section must hold exactly draft: true,
// prerelease: auto and replace_existing_draft: true, in that order, and
// nothing else, so no extra file is published beside the CLI's archives (issue
// #393).
//
//   - draft: true (issue #156): goreleaser uploads every asset to a draft and
//     leaves it a draft, and release.yml's publish job publishes it last,
//     after the archives are attested. A published release whose repository
//     has release immutability on can then never gain or lose an asset (ADR
//     0015). goreleaser's default publishes the release itself, as soon as
//     its own uploads are done.
//   - prerelease: auto (issue #92): goreleaser marks a tag with a pre-release
//     suffix (v0.1.0-rc.2) as a GitHub pre-release, which never becomes the
//     latest release, and a plain vX.Y.Z as a normal release, as the image
//     gets no X.Y and no latest tag for a pre-release (ADR 0015).
//   - replace_existing_draft: true: a re-run of a failed goreleaser job
//     deletes the draft the failed run left for the tag and starts a new one,
//     rather than leave two drafts for one tag. GitHub's release by tag
//     (GetReleaseByTag) skips drafts, so without it goreleaser creates a
//     second one, and the publish job may publish the incomplete first.
//   - no extra_files (issue #393): the release ships the CLI's archives and
//     their checksums.txt alone. The HTTP API's OpenAPI document is not
//     published any more, and a line naming it, or shift2eu-server, is
//     refused wherever it sits, comments included.
//
// It fails closed: a missing release section is refused, because goreleaser's
// default publishes every tag as a normal release, and so is any other key or
// value in it: make_latest, for one, would override what prerelease: auto
// decides, and draft: false would publish assets that could no longer change
// before they are attested.
func GitHubRelease(goreleaser []byte) error {
	lines, err := yamlread.SplitLines(goreleaser)
	if err != nil {
		return fmt.Errorf("read .goreleaser.yaml: %w", err)
	}

	entries, err := yamlread.Mapping(lines)
	if err != nil {
		return fmt.Errorf("read .goreleaser.yaml: %w", err)
	}

	if messages := retiredNameMessagesIn(goreleaser); len(messages) != 0 {
		return fmt.Errorf("read .goreleaser.yaml: %s", messages[0])
	}

	for _, e := range entries {
		if e.Key == "release" {
			if err := checkRelease(e); err != nil {
				return fmt.Errorf("read .goreleaser.yaml: %w", err)
			}

			return nil
		}
	}

	return errors.New("read .goreleaser.yaml: no release section, want release: draft: true, prerelease: auto and replace_existing_draft: true: goreleaser's default publishes a pre-release tag as a normal release, the latest one, and publishes the assets before they are attested")
}

// releaseKeys are the keys of the release section, in order, each with the
// one plain value it must have.
var releaseKeys = []struct{ key, value string }{
	{"draft", "true"},
	{"prerelease", "auto"},
	{"replace_existing_draft", "true"},
}

// checkRelease refuses a release section that holds anything but draft: true,
// prerelease: auto and replace_existing_draft: true, in that order.
func checkRelease(release yamlread.Entry) error {
	block, err := yamlread.BlockValue(release, "release")
	if err != nil {
		return err
	}

	entries, err := yamlread.Mapping(block)
	if err != nil {
		return fmt.Errorf("release: %w", err)
	}

	if len(entries) != len(releaseKeys) {
		return fmt.Errorf("line %d: release holds %d keys, want exactly draft: true, prerelease: auto and replace_existing_draft: true: a missing replace_existing_draft leaves a re-run two drafts for one tag, and any other key may change which release GitHub shows as the latest, publish it before its archives are attested, or publish a file beside the CLI's archives (issue #393)", release.Num, len(entries))
	}

	for i, want := range releaseKeys {
		e := entries[i]
		if e.Key != want.key {
			return fmt.Errorf("line %d: release holds %s where it wants %s, in the order draft, prerelease, replace_existing_draft, extra_files", e.Num, e.Key, want.key)
		}

		value, err := yamlread.Single(e)
		if err != nil {
			return fmt.Errorf("release: %w", err)
		}

		// draft and replace_existing_draft are booleans: a quoted "true" is a
		// string goreleaser refuses, so only the plain word is taken.
		if want.key != "prerelease" && strings.ContainsAny(e.Value[:min(1, len(e.Value))], `"'`) {
			value = e.Value
		}

		if value != want.value {
			return fmt.Errorf("line %d: release.%s is %q, want %s", e.Num, want.key, value, want.value)
		}
	}

	return nil
}
