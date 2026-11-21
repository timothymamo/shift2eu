package pincheck_test

import (
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
	// package, whose dist.integrity the workflow pins (ADR 0033).
	packageName = "@anthropic-ai/claude-code-linux-x64"

	// publishedIntegrity is the integrity the 2.1.330 registry fixture
	// publishes. It is the value .github/workflows/ai-review.yml pins: public
	// data, no credential.
	publishedIntegrity = "sha512-9Gn+V7DtJ8M1nztP9CeDVl9g3RtrepI7rOFTyPRRnZe8D6zebIHKmC7OvBauucuAP0X0k85SDN/LPVZyjsGezQ=="

	// staleIntegrity is the integrity of an older release, a valid sha512
	// value the 2.1.330 fixture does not publish: the issue #428 case, where a
	// Renovate bump left the previous version's integrity in place.
	staleIntegrity = "sha512-b2xkLWludGVncml0eS1mcm9tLTIuMS4zMjc="

	// realWorkflowPath is the workflow the check reads in CI, relative to this
	// package's directory.
	realWorkflowPath = "../../../../.github/workflows/ai-review.yml"
)

// workflowEnv returns an excerpt of the AI review workflow whose env: block
// holds lines, indented as .github/workflows/ai-review.yml indents its pin. It
// is the input FromWorkflow reads: a YAML file, not a whole valid workflow, so
// a case may leave a line out or write it twice.
func workflowEnv(lines ...string) string {
	var b strings.Builder
	b.WriteString("jobs:\n")
	b.WriteString("  review:\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - name: Install the Claude Code CLI\n")
	b.WriteString("        env:\n")
	for _, line := range lines {
		b.WriteString("          " + line + "\n")
	}
	b.WriteString("        run: .github/scripts/install-claude-code.sh x\n")
	return b.String()
}

// versionLine and integrityLine write one pin line as the workflow does.
func versionLine(version string) string {
	return "CLAUDE_CODE_VERSION: " + version
}

func integrityLine(integrity string) string {
	return "CLAUDE_CODE_INTEGRITY: " + integrity
}

func TestFromWorkflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		workflow string
		want     pincheck.Pin
		wantErr  bool
	}{
		{
			name: "one version and one integrity line",
			workflow: workflowEnv(
				"# renovate: datasource=npm depName="+packageName,
				versionLine(testVersion),
				integrityLine(publishedIntegrity),
			),
			want: pincheck.Pin{Version: testVersion, Integrity: publishedIntegrity},
		},
		{
			name:     "missing version line",
			workflow: workflowEnv(integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "missing integrity line",
			workflow: workflowEnv(versionLine(testVersion)),
			wantErr:  true,
		},
		{
			name: "version line twice",
			workflow: workflowEnv(
				versionLine(testVersion),
				versionLine(testVersion),
				integrityLine(publishedIntegrity),
			),
			wantErr: true,
		},
		{
			name: "integrity line twice",
			workflow: workflowEnv(
				versionLine(testVersion),
				integrityLine(publishedIntegrity),
				integrityLine(publishedIntegrity),
			),
			wantErr: true,
		},
		{
			name:     "version of two numbers",
			workflow: workflowEnv(versionLine("2.1"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "version with a prerelease suffix",
			workflow: workflowEnv(versionLine("2.1.330-beta"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "version that leaves the registry path",
			workflow: workflowEnv(versionLine("2.1.330/../x"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "version that is a dist tag",
			workflow: workflowEnv(versionLine("latest"), integrityLine(publishedIntegrity)),
			wantErr:  true,
		},
		{
			name:     "integrity of another algorithm",
			workflow: workflowEnv(versionLine(testVersion), integrityLine("sha1-abc")),
			wantErr:  true,
		},
		{
			name:     "integrity without a digest",
			workflow: workflowEnv(versionLine(testVersion), integrityLine("sha512-")),
			wantErr:  true,
		},
		{
			name:     "integrity that is not base64",
			workflow: workflowEnv(versionLine(testVersion), integrityLine("sha512-a b")),
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := pincheck.FromWorkflow(strings.NewReader(tc.workflow))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("FromWorkflow() = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("FromWorkflow() error = %v, want none", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("FromWorkflow() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestFromWorkflowRealWorkflowFile reads the workflow CI runs, so the parser
// is held to the file's real shape. It asserts only that a pin came out:
// Renovate changes the version and the integrity, the test does not.
func TestFromWorkflowRealWorkflowFile(t *testing.T) {
	t.Parallel()

	f, err := os.Open(filepath.FromSlash(realWorkflowPath))
	if err != nil {
		t.Fatalf("open %s: %v", realWorkflowPath, err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close %s: %v", realWorkflowPath, err)
		}
	})

	pin, err := pincheck.FromWorkflow(f)
	if err != nil {
		t.Fatalf("FromWorkflow(%s) error = %v, want none", realWorkflowPath, err)
	}
	if pin.Version == "" {
		t.Error("FromWorkflow() Version is empty, want the pinned version")
	}
	if pin.Integrity == "" {
		t.Error("FromWorkflow() Integrity is empty, want the pinned integrity")
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
