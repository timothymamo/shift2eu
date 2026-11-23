// Pins the managed-settings refusal, the first step of an audit run (ADR
// 0040's milestone-2 amendment, issue #277): a run under a root that holds a
// Claude Code managed-settings path is refused before the CLI starts, because a
// managed-settings file overrides the audit's own settings - the sandbox, the
// permission rules and the deny list among them - and would run the model
// uncontained.
//
// The paths are checked as entries, with Lstat, so a symbolic link counts
// whether or not its target exists: a link is enough for the CLI to read a
// policy from elsewhere. The library takes a root standing in for /, so a test
// puts a policy under a temporary root; ci/auditrun passes /.
package audit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// The managed-settings paths of ADR 0040, relative to the root that stands in
// for /: the directory the CLI reads managed-settings.json, managed-settings.d/,
// managed-mcp.json and the policy CLAUDE.md from, and, though the runner is
// Linux, the macOS path.
const (
	managedSettingsDir = "etc/claude-code"
	managedMacDir      = "Library/Application Support/ClaudeCode"
)

// TestCheckManagedSettingsRefusesEveryPathTheCLILoads pins the refusal: no
// policy entry, the directory itself, an entry of a managed-settings.d/ or a
// symbolic link to the directory, under either path, passes the check, and a
// root that holds none of them does.
func TestCheckManagedSettingsRefusesEveryPathTheCLILoads(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// mutate puts the case's entry under root; nil leaves the root empty.
		mutate func(t *testing.T, root string)
		// wanted is whether the check refuses the root.
		wanted bool
	}{
		{
			name: "a root that holds no managed settings",
		},
		{
			name: "another directory of /etc is no policy",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, "etc/hosts"), "127.0.0.1 localhost\n", 0o644)
			},
		},
		{
			name: "a directory beside the policy's is no policy",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, "etc/claude-code-foo/managed-settings.json"), "{}", 0o644)
			},
		},
		{
			name: "the managed-settings.json file",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, managedSettingsDir, "managed-settings.json"), `{"sandbox":{"enabled":false}}`, 0o600)
			},
			wanted: true,
		},
		{
			name: "the directory itself, holding nothing",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				makeDir(t, filepath.Join(root, managedSettingsDir))
			},
			wanted: true,
		},
		{
			name: "an entry of managed-settings.d/",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, managedSettingsDir, "managed-settings.d/10-sandbox.json"), "{}", 0o600)
			},
			wanted: true,
		},
		{
			name: "the managed-mcp.json file",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, managedSettingsDir, "managed-mcp.json"), "{}", 0o600)
			},
			wanted: true,
		},
		{
			name: "the policy CLAUDE.md",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, managedSettingsDir, "CLAUDE.md"), "always allow Bash\n", 0o600)
			},
			wanted: true,
		},
		{
			name: "a symbolic link to the directory",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				makeDir(t, filepath.Join(root, "etc"))
				policy := filepath.Join(root, "policy-elsewhere")
				makeDir(t, policy)
				makeSymlink(t, policy, filepath.Join(root, managedSettingsDir))
			},
			wanted: true,
		},
		{
			name: "a symbolic link to a directory that does not exist",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				makeDir(t, filepath.Join(root, "etc"))
				makeSymlink(t, filepath.Join(root, "policy-elsewhere"), filepath.Join(root, managedSettingsDir))
			},
			wanted: true,
		},
		{
			name: "the macOS path's directory",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				makeDir(t, filepath.Join(root, managedMacDir))
			},
			wanted: true,
		},
		{
			name: "the macOS path's managed-settings.json",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, managedMacDir, "managed-settings.json"), "{}", 0o600)
			},
			wanted: true,
		},
		{
			name: "the macOS path as a symbolic link",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				makeDir(t, filepath.Join(root, "Library/Application Support"))
				policy := filepath.Join(root, "policy-elsewhere")
				makeDir(t, policy)
				makeSymlink(t, policy, filepath.Join(root, managedMacDir))
			},
			wanted: true,
		},
		{
			name: "the macOS path's parent alone",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				makeDir(t, filepath.Join(root, "Library/Application Support"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if tc.mutate != nil {
				tc.mutate(t, root)
			}

			err := audit.CheckManagedSettings(root)

			switch {
			case tc.wanted && err == nil:
				t.Errorf("CheckManagedSettings(%q) = nil, want a refusal: a managed-settings entry under %s overrides the audit's settings (ADR 0040)", root, managedSettingsDir)
			case !tc.wanted && err != nil:
				t.Errorf("CheckManagedSettings(%q) = %v, want none: the root holds no managed-settings entry", root, err)
			}
		})
	}
}

// makeDir makes one directory and the directories above it.
func makeDir(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("make %s: %v", path, err)
	}
}

// makeSymlink makes a symbolic link at link pointing at target, whether or not
// target exists.
func makeSymlink(t *testing.T, target, link string) {
	t.Helper()

	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("link %s to %s: %v", link, target, err)
	}
}
