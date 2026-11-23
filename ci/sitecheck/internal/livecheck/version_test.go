package livecheck_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// This file holds the checks of the live version and of the commands it builds
// (behaviour 9): the tag list, the match of the page's untrusted text against
// it, and the two gh argv slices, whose tag can only come from the match. The
// page's text is never an input of a command: the Tag a match returns holds the
// tag list's own entry, which is the only way to build a non-zero Tag.

// TestReadTagList pins the tag list's format: one tag per line as `git tag
// --list` prints it, a trailing carriage return removed, empty lines skipped,
// and nothing else trimmed.
func TestReadTagList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		list    string
		want    []string
		wantErr bool
	}{
		{name: "one tag a line", list: "v1.2.3\nv1.2.4\n", want: []string{"v1.2.3", "v1.2.4"}},
		{name: "no trailing newline", list: "v1.2.3", want: []string{"v1.2.3"}},
		{name: "carriage returns", list: "v1.2.3\r\nv1.2.4\r\n", want: []string{"v1.2.3", "v1.2.4"}},
		{name: "empty lines are skipped", list: "v1.2.3\n\nv1.2.4\n\n", want: []string{"v1.2.3", "v1.2.4"}},
		{name: "an empty list", list: "", want: []string{}},
		{name: "a space is not trimmed", list: " v1.2.3\n", want: []string{" v1.2.3"}},
		{name: "more than the cap", list: "v1.2.3\n" + strings.Repeat("v1.2.3\n", livecheck.MaxTagListBytes/7+1), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := livecheck.ReadTagList(strings.NewReader(tc.list))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ReadTagList returned %d tags and no error, want an error: %s", len(got), tc.name)
				}

				return
			}

			if err != nil {
				t.Fatalf("ReadTagList failed: %v", err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ReadTagList differs (-want +got):\n%s", diff)
			}
		})
	}
}

// TestMatchVersion pins behaviour 9's match: the page's text must be exactly
// ^v<digits>.<digits>.<digits>$, at most MaxVersionBytes long, and equal an
// entry of the tag list byte for byte. A trailing newline, a leading space, a
// release candidate, an upper-case tag, a hostile long string and a well-formed
// tag the repository does not have are all failures.
func TestMatchVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		tags    []string
		want    string
		wantErr bool
	}{
		{name: "a tag of the list", text: "v1.2.3", tags: []string{"v1.2.2", "v1.2.3"}, want: "v1.2.3"},
		{name: "the only tag of the list", text: "v1.2.3", tags: []string{"v1.2.3"}, want: "v1.2.3"},
		{name: "a multi-digit version", text: "v10.200.3000", tags: []string{"v10.200.3000"}, want: "v10.200.3000"},
		{name: "a well-formed tag the repository does not have", text: "v9.9.9", tags: []string{"v1.2.3"}, wantErr: true},
		{name: "an empty tag list", text: "v1.2.3", tags: nil, wantErr: true},
		{name: "a trailing newline", text: "v1.2.3\n", tags: []string{"v1.2.3"}, wantErr: true},
		{name: "a leading space", text: " v1.2.3", tags: []string{"v1.2.3"}, wantErr: true},
		{name: "a trailing space", text: "v1.2.3 ", tags: []string{"v1.2.3"}, wantErr: true},
		{name: "a release candidate", text: "v1.2.3-rc1", tags: []string{"v1.2.3-rc1"}, wantErr: true},
		{name: "an upper-case v", text: "V1.2.3", tags: []string{"V1.2.3"}, wantErr: true},
		{name: "no v", text: "1.2.3", tags: []string{"1.2.3"}, wantErr: true},
		{name: "two digits", text: "v1.2", tags: []string{"v1.2"}, wantErr: true},
		{name: "a build suffix", text: "v1.2.3+build", tags: []string{"v1.2.3+build"}, wantErr: true},
		{name: "an empty text", text: "", tags: []string{"v1.2.3"}, wantErr: true},
		{name: "a 10 KiB string", text: strings.Repeat("v1.2.3", 1748), tags: []string{"v1.2.3"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tag, err := livecheck.MatchVersion(tc.text, tc.tags)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("MatchVersion(%q, %q) returned %q and no error, want an error: %s", tc.text, tc.tags, tag.String(), tc.name)
				}

				if !tag.IsZero() {
					t.Errorf("MatchVersion(%q, %q) failed but returned the tag %q, want the zero Tag", tc.text, tc.tags, tag.String())
				}

				return
			}

			if err != nil {
				t.Fatalf("MatchVersion(%q, %q) failed: %v", tc.text, tc.tags, err)
			}

			if got := tag.String(); got != tc.want {
				t.Errorf("MatchVersion(%q, %q) is %q, want %q: the Tag holds the tag list's entry", tc.text, tc.tags, got, tc.want)
			}

			if tag.IsZero() {
				t.Errorf("MatchVersion(%q, %q) returned the zero Tag, want %q", tc.text, tc.tags, tc.want)
			}
		})
	}
}

// TestMatchVersionTakesTheTagListsEntry pins the structural part of behaviour 9:
// the matched Tag carries the tag list's own string, and a test outside the
// package cannot build any other. A tag list entry the page could not have said
// (here with trailing text that the pattern refuses) never matches, so nothing a
// page holds can reach a command.
func TestMatchVersionTakesTheTagListsEntry(t *testing.T) {
	t.Parallel()

	// A page cannot produce this entry: the pattern refuses it.
	if tag, err := livecheck.MatchVersion("v1.2.3\nv1.2.4", []string{"v1.2.3\nv1.2.4"}); err == nil {
		t.Fatalf("MatchVersion matched the tag list entry %q, want a refusal: the page's text must be one version", tag.String())
	}

	tag, err := livecheck.MatchVersion("v1.2.3", []string{"v1.2.3", "v1.2.4"})
	if err != nil {
		t.Fatalf("MatchVersion failed: %v", err)
	}

	var zero livecheck.Tag

	if !zero.IsZero() || zero.String() != "" {
		t.Error("the zero Tag is not empty: only MatchVersion may build a Tag")
	}

	if tag.String() != "v1.2.3" {
		t.Errorf("the matched tag is %q, want %q", tag.String(), "v1.2.3")
	}
}

// TestBundleNameAndArgv pins the argv the check builds for milestone 4
// (behaviour 9): the bundle's name, the download's argv and the attestation
// check's argv, one argument per element, with the tag taken from the matched
// Tag and the signer workflow and the source ref spelled out.
func TestBundleNameAndArgv(t *testing.T) {
	t.Parallel()

	tag, err := livecheck.MatchVersion("v1.2.3", []string{"v1.2.3"})
	if err != nil {
		t.Fatalf("MatchVersion failed: %v", err)
	}

	name, err := livecheck.BundleName(tag)
	if err != nil {
		t.Fatalf("BundleName failed: %v", err)
	}

	if want := "shift2eu-site_v1.2.3.tar.gz"; name != want {
		t.Errorf("BundleName is %q, want %q", name, want)
	}

	download, err := livecheck.DownloadArgv(tag, "/tmp/bundles")
	if err != nil {
		t.Fatalf("DownloadArgv failed: %v", err)
	}

	wantDownload := []string{
		"gh", "release", "download", "v1.2.3",
		"--repo", "timothymamo/shift2eu",
		"--pattern", "shift2eu-site_v1.2.3.tar.gz",
		"--dir", "/tmp/bundles",
	}

	if diff := cmp.Diff(wantDownload, download); diff != "" {
		t.Errorf("DownloadArgv differs (-want +got):\n%s", diff)
	}

	verify, err := livecheck.AttestationVerifyArgv(tag, "/tmp/bundles/shift2eu-site_v1.2.3.tar.gz")
	if err != nil {
		t.Fatalf("AttestationVerifyArgv failed: %v", err)
	}

	wantVerify := []string{
		"gh", "attestation", "verify", "/tmp/bundles/shift2eu-site_v1.2.3.tar.gz",
		"--repo", "timothymamo/shift2eu",
		"--signer-workflow", "timothymamo/shift2eu/.github/workflows/release.yml",
		"--source-ref", "refs/tags/v1.2.3",
		"--deny-self-hosted-runners",
	}

	if diff := cmp.Diff(wantVerify, verify); diff != "" {
		t.Errorf("AttestationVerifyArgv differs (-want +got):\n%s", diff)
	}
}

// TestArgvRefusesTheZeroTagAndAHostilePath pins that a builder refuses what it
// cannot trust: the zero Tag (which no match returns), an empty path and a path
// starting with "-", which gh would read as another option.
func TestArgvRefusesTheZeroTagAndAHostilePath(t *testing.T) {
	t.Parallel()

	tag, err := livecheck.MatchVersion("v1.2.3", []string{"v1.2.3"})
	if err != nil {
		t.Fatalf("MatchVersion failed: %v", err)
	}

	var zero livecheck.Tag

	tests := []struct {
		name string
		call func() ([]string, error)
	}{
		{name: "the download of the zero Tag", call: func() ([]string, error) { return livecheck.DownloadArgv(zero, "/tmp") }},
		{name: "the download into an empty directory", call: func() ([]string, error) { return livecheck.DownloadArgv(tag, "") }},
		{name: "the download into an option", call: func() ([]string, error) { return livecheck.DownloadArgv(tag, "--output=/tmp/x") }},
		{name: "the check of the zero Tag", call: func() ([]string, error) { return livecheck.AttestationVerifyArgv(zero, "/tmp/x.tar.gz") }},
		{name: "the check of an empty bundle path", call: func() ([]string, error) { return livecheck.AttestationVerifyArgv(tag, "") }},
		{name: "the check of an option", call: func() ([]string, error) { return livecheck.AttestationVerifyArgv(tag, "--repo=attacker/repo") }},
		{name: "the bundle name of the zero Tag", call: func() ([]string, error) {
			name, err := livecheck.BundleName(zero)

			return []string{name}, err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			argv, err := tc.call()
			if err == nil {
				t.Fatalf("%s returned %q and no error, want an error", tc.name, argv)
			}
		})
	}
}
