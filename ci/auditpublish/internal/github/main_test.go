package github_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain makes the test binary stand in for gh and gitleaks: the fakes are
// symbolic links named gh and gitleaks to this binary (newFakeGH,
// newFakeGitleaks), so the name it was started under says which one to be. No
// environment variable carries that, because the library starts both with an
// environment of its own, which a marker would not survive.
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case fakeGHName:
		os.Exit(fakeGHMain())
	case fakeGitleaksName:
		os.Exit(fakeGitleaksMain())
	}

	os.Exit(m.Run())
}
