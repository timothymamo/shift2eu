package pincheck_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/claudecodepin/internal/pincheck"
)

const (
	// testVersion is the Claude Code CLI version the fixtures pin: a version
	// shape the check accepts.
	testVersion = "2.1.330"

	// packageName is the npm package the pin is for: the linux-x64 platform
	// package, whose dist.integrity the pin file names (ADR 0040's milestone-2
	// amendment, issue #277).
	packageName = "@anthropic-ai/claude-code-linux-x64"

	// publishedIntegrity is the integrity the 2.1.330 registry fixture
	// publishes. It is the value .github/claude-code.yaml pins: public data, no
	// credential.
	publishedIntegrity = "sha512-9Gn+V7DtJ8M1nztP9CeDVl9g3RtrepI7rOFTyPRRnZe8D6zebIHKmC7OvBauucuAP0X0k85SDN/LPVZyjsGezQ=="

	// staleIntegrity is the integrity of an older release: a well-formed
	// sha512- value, the base64 of a 64-byte digest, that the 2.1.330 fixture
	// does not publish. It is the issue #428 case, where a Renovate bump left
	// the previous version's integrity in place, so it must pass the shape
	// check FromPinFile makes and fail only on the comparison.
	staleIntegrity = "sha512-b2xkLWludGVncml0eS1mcm9tLTIuMS4zMjcuLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLg=="

	// renovateComment is the comment the pin's version line sits under: the one
	// Renovate's generic manager reads, and the reason the version lives in a
	// file of its own rather than in a workflow (ADR 0040, CLAUDE.md rule 7).
	renovateComment = "# renovate: datasource=npm depName=" + packageName

	// realPinPath is the pin file the check reads in CI, relative to this
	// package's directory.
	realPinPath = "../../../../.github/claude-code.yaml"
)

// pinFile returns the text of the one committed pin file, .github/claude-code.yaml,
// with the lines a case gives it: the comment Renovate reads, the version under
// it and the integrity beside it. It is the input FromPinFile reads, so a case
// may leave a line out, write it twice or change its value.
func pinFile(lines ...string) string {
	var b strings.Builder
	b.WriteString("# The Claude Code CLI the AI reviewer and the audit run (ADR 0040).\n")

	for _, line := range lines {
		b.WriteString(line + "\n")
	}

	return b.String()
}

// versionLine and integrityLine write one pin line as the file does.
func versionLine(version string) string {
	return "version: " + version
}

func integrityLine(integrity string) string {
	return "integrity: " + integrity
}

func TestFromPinFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
		want     pincheck.Pin
		wantErr  bool
	}{
		{
			name: "one version and one integrity line under the renovate comment",
			contents: pinFile(
				renovateComment,
				versionLine(testVersion),
				integrityLine(publishedIntegrity),
			),
			want: pincheck.Pin{Version: testVersion, Integrity: publishedIntegrity},
		},
		{
			name:     "missing version line",
			contents: pinFile(renovateComment, integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "missing integrity line",
			contents: pinFile(renovateComment, versionLine(testVersion)),
			wantErr:  true,
		},
		{
			name: "version line twice",
			contents: pinFile(
				renovateComment,
				versionLine(testVersion),
				versionLine(testVersion),
				integrityLine(publishedIntegrity),
			),
			wantErr: true,
		},
		{
			name: "integrity line twice",
			contents: pinFile(
				renovateComment,
				versionLine(testVersion),
				integrityLine(publishedIntegrity),
				integrityLine(publishedIntegrity),
			),
			wantErr: true,
		},
		{
			name:     "version of two numbers",
			contents: pinFile(renovateComment, versionLine("2.1"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "version with a prerelease suffix",
			contents: pinFile(renovateComment, versionLine("2.1.330-beta"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "version that leaves the registry path",
			contents: pinFile(renovateComment, versionLine("2.1.330/../x"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "version that is a dist tag",
			contents: pinFile(renovateComment, versionLine("latest"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "version with a trailing comment",
			contents: pinFile(renovateComment, versionLine(testVersion+" # the reviewer's"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "integrity of another algorithm",
			contents: pinFile(renovateComment, versionLine(testVersion), integrityLine("sha1-abc")),
			wantErr:  true,
		},
		{
			name:     "integrity without a digest",
			contents: pinFile(renovateComment, versionLine(testVersion), integrityLine("sha512-")),
			wantErr:  true,
		},
		{
			name:     "integrity that is not base64",
			contents: pinFile(renovateComment, versionLine(testVersion), integrityLine("sha512-a b")),
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := pincheck.FromPinFile(strings.NewReader(tc.contents))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("FromPinFile() = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("FromPinFile() error = %v, want none", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("FromPinFile() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestFromPinFileRealPinFile reads the pin file CI reads, so the parser is held
// to the file's real shape and to where the version line sits: directly under
// the # renovate: comment Renovate's generic manager reads, which is what makes
// a bump move this file and no other. It asserts the version and the integrity
// only as present: Renovate changes them, the test does not.
func TestFromPinFileRealPinFile(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.FromSlash(realPinPath))
	if err != nil {
		t.Fatalf("read %s: %v; ADR 0040 moves the Claude Code pin into that one file", realPinPath, err)
	}

	pin, err := pincheck.FromPinFile(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("FromPinFile(%s) error = %v, want none", realPinPath, err)
	}
	if pin.Version == "" {
		t.Error("FromPinFile() Version is empty, want the pinned version")
	}
	if pin.Integrity == "" {
		t.Error("FromPinFile() Integrity is empty, want the pinned integrity")
	}

	lines := strings.Split(string(src), "\n")

	found := false
	for i, line := range lines {
		if strings.TrimSpace(line) != versionLine(pin.Version) {
			continue
		}

		found = true

		if i == 0 || !strings.Contains(lines[i-1], "# renovate:") {
			t.Errorf("%s: line %d holds the version pin and the line above it is not a # renovate: comment: Renovate's generic manager reads the comment and the line under it, so a pin without one is a version nobody bumps (CLAUDE.md rule 7)", realPinPath, i+1)
		}

		break
	}

	if !found {
		t.Errorf("%s: no line holds %q, want the pin's version line", realPinPath, versionLine(pin.Version))
	}
}

func TestPinRegistryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "the pinned version",
			version: "2.1.330",
			want:    "https://registry.npmjs.org/@anthropic-ai/claude-code-linux-x64/2.1.330",
		},
		{
			name:    "a first release",
			version: "1.0.0",
			want:    "https://registry.npmjs.org/@anthropic-ai/claude-code-linux-x64/1.0.0",
		},
		{
			name:    "a two-digit minor version",
			version: "10.20.30",
			want:    "https://registry.npmjs.org/@anthropic-ai/claude-code-linux-x64/10.20.30",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pin := pincheck.Pin{Version: tc.version, Integrity: publishedIntegrity}
			if diff := cmp.Diff(tc.want, pin.RegistryURL()); diff != "" {
				t.Errorf("RegistryURL() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pin  pincheck.Pin
		doc  string
		// wantErrContains, when set, must each be a substring of the error:
		// the operator needs both values and the command that prints the
		// published one.
		wantErrContains []string
		wantErr         bool
	}{
		{
			name: "published integrity matches the pin",
			pin:  pincheck.Pin{Version: testVersion, Integrity: publishedIntegrity},
			doc:  "registry-2.1.330.json",
		},
		{
			name: "stale integrity from the previous release",
			pin:  pincheck.Pin{Version: testVersion, Integrity: staleIntegrity},
			doc:  "registry-2.1.330.json",
			wantErrContains: []string{
				staleIntegrity,
				publishedIntegrity,
				"npm view @anthropic-ai/claude-code-linux-x64@2.1.330 dist.integrity",
			},
			wantErr: true,
		},
		{
			name:    "document of another version",
			pin:     pincheck.Pin{Version: testVersion, Integrity: staleIntegrity},
			doc:     "registry-2.1.327.json",
			wantErr: true,
		},
		{
			name:    "document of another package",
			pin:     pincheck.Pin{Version: testVersion, Integrity: publishedIntegrity},
			doc:     "registry-other-package.json",
			wantErr: true,
		},
		{
			name:    "document without dist.integrity",
			pin:     pincheck.Pin{Version: testVersion, Integrity: publishedIntegrity},
			doc:     "registry-no-integrity.json",
			wantErr: true,
		},
		{
			name:    "document with an empty dist.integrity and an empty pin",
			pin:     pincheck.Pin{Version: testVersion, Integrity: ""},
			doc:     "registry-empty-integrity.json",
			wantErr: true,
		},
		{
			name:    "document that is not JSON",
			pin:     pincheck.Pin{Version: testVersion, Integrity: publishedIntegrity},
			doc:     "registry-not-json.json",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, err := os.Open(filepath.Join("testdata", tc.doc))
			if err != nil {
				t.Fatalf("open testdata/%s: %v", tc.doc, err)
			}
			t.Cleanup(func() {
				if err := f.Close(); err != nil {
					t.Errorf("close testdata/%s: %v", tc.doc, err)
				}
			})

			err = pincheck.Verify(tc.pin, f)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Verify(%+v, testdata/%s) error = %v, want none", tc.pin, tc.doc, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Verify(%+v, testdata/%s) = nil, want an error", tc.pin, tc.doc)
			}
			for _, want := range tc.wantErrContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Verify(%+v, testdata/%s) error = %q, want it to contain %q", tc.pin, tc.doc, err, want)
				}
			}
		})
	}
}
