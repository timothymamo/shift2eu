package lint_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gitleaks honours its inline allow comment in the text it scans, so the
// scheduled audit's publish program refuses a whole run whose model's report
// holds it, in any letter case (PRD #175 milestone 2, ADR 0040, "The publish
// program's GitHub side"). The model reads this repository: a tracked file that
// spells the marker out is a line the model may quote, and the run then refuses
// its own source. The marker is built from its two parts, here and in the tests
// that feed a scanner with it (ci/auditpublish/internal/github's and
// ci/internal/audit's), so that no line of any of them spells it out.

// TestNoTrackedFileSpellsTheInlineAllowComment walks every path git tracks and
// fails on each file holding gitleaks' inline allow comment - its name, a colon
// and allow - in any letter case.
func TestNoTrackedFileSpellsTheInlineAllowComment(t *testing.T) {
	t.Parallel()

	// The marker, built in code: its name, a colon and "allow".
	marker := "gitleaks" + ":allow"

	root := moduleRoot(t)

	var scanned []string

	for _, rel := range trackedFiles(t) {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // tracked but not in this checkout, as a sparse one has it
			}

			t.Fatalf("read %s: %v", rel, err)
		}

		scanned = append(scanned, rel)

		lower := strings.ToLower(string(src))

		i := strings.Index(lower, marker)
		if i < 0 {
			continue
		}

		t.Errorf("%s:%d holds gitleaks' inline allow comment; build the marker in code (\"gitleaks\" + \":allow\") instead, so that a model reading this repository cannot quote the line and refuse the audit's own run (PRD #175, ADR 0040)",
			rel, strings.Count(lower[:i], "\n")+1)
	}

	// Guard the walk itself: a scan of nothing, or one that stopped reading the
	// repository's files, would pass whatever they hold.
	for _, rel := range []string{
		"ci/auditpublish/internal/github/gitleaks.go",
		"test/lint/doc.go",
		"test/lint/inlineallow_test.go",
	} {
		if !slices.Contains(scanned, rel) {
			t.Errorf("the scan skipped %s, a tracked file", rel)
		}
	}

	t.Logf("scanned %d tracked files", len(scanned))
}
