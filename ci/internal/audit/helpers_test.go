// Shared fixtures of the audit library's tests: the invented secrets a run
// must never print, the inputs one run is given, and a file writer. ADR 0040's
// model-job amendment (issue #277) is what the tests beside the audit library
// pin; nothing here runs a model, a sandbox or a command.
package audit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

const (
	// testToken and testCanary are invented values made at test time in place of
	// the step's two secrets: no line of a run, and no error, may hold either.
	testToken  = "oauth-sentinel-token-never-printed" //nolint:gosec // G101: an invented value made at test time, which grants nothing
	testCanary = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// testIntegrity is a well-formed npm integrity: the shape -claude-integrity
	// takes, base64 of the sha512's 64 bytes.
	testIntegrity = "sha512-" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
)

// testInputs returns the inputs of one run under root: the two secrets above, a
// GITHUB_ENV path and a step summary, and the tarball's path and integrity the
// install step hands on.
func testInputs(root string) audit.Inputs {
	return audit.Inputs{
		Token:       testToken,
		Canary:      testCanary,
		GitHubEnv:   filepath.Join(root, "github-env"),
		StepSummary: filepath.Join(root, "step-summary.md"),
		Tarball:     filepath.Join(root, "claude-code.tgz"),
		Integrity:   testIntegrity,
	}
}

// writeFile writes body to path, making the directories above it.
func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("make the directory of %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// secretText is a text holding both invented secrets, the way a failed step's
// error can: a run must print it nowhere.
func secretText() string {
	return strings.Join([]string{testToken, testCanary, "the-clis-own-text"}, " ")
}
