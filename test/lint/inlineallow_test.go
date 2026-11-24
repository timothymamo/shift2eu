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
// and allow - in any letter case. It reports every occurrence, through
// inlineAllowLines, so a file holding the comment twice is reported twice
// (issue #516).
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

// TestInlineAllowLines pins the scan TestNoTrackedFileSpellsTheInlineAllowComment
// reports an occurrence with: inlineAllowLines returns the line number of every
// occurrence of the marker, in any letter case, in order and one entry per
// occurrence, so that two occurrences on one line report that line twice and a
// file holding the comment twice is reported twice rather than once (issue
// #516). No line of this table spells the marker out, in any letter case: each
// one is built from its two parts, as the walk builds it.
func TestInlineAllowLines(t *testing.T) {
	t.Parallel()

	// The marker the walk searches for, and two other letter cases of it, each
	// built from its two parts so that this file holds no line a model could
	// quote to refuse the audit's own run.
	marker := "gitleaks" + ":allow"
	camel := "GitLeaks" + ":Allow"
	mixed := "gItLeAkS" + ":aLlOw"

	tests := []struct {
		name string
		src  string
		want []int
	}{
		{
			name: "no occurrence",
			src:  "// a comment\nvar nothing int\n",
			want: nil,
		},
		{
			name: "one occurrence",
			src:  "var a int\n// " + marker + "\nvar b int\n",
			want: []int{2},
		},
		{
			name: "two on different lines",
			src:  "var a int\n// " + marker + "\nvar b int\n// " + marker + "\n",
			want: []int{2, 4},
		},
		{
			name: "two on one line",
			src:  "// " + camel + " and " + marker + "\nvar a int\n",
			want: []int{1, 1},
		},
		{
			name: "mixed letter case",
			src:  "x := 1 // " + mixed + "\n",
			want: []int{1},
		},
		{
			name: "first and last line, no trailing newline",
			src:  "// " + marker + "\nvar a int\n// " + camel,
			want: []int{1, 3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := inlineAllowLines(tc.src, marker)
			if !slices.Equal(got, tc.want) {
				t.Errorf("inlineAllowLines(%q, marker) = %v, want %v; report every occurrence, by the line it starts on, in order, one entry per occurrence", tc.src, got, tc.want)
			}
		})
	}
}

// inlineAllowLines returns the 1-based line number of every occurrence of marker
// in src, in order and one entry per occurrence, ignoring letter case: src is
// lower-cased before the search and marker is given lower case.
//
// STUB, for the coder to replace (issue #516): the walk of
// TestNoTrackedFileSpellsTheInlineAllowComment reports through this function, so
// it lives here, beside the table that pins it, and returns nothing yet so that
// the rest of test/lint compiles and TestInlineAllowLines fails for the right
// reason.
func inlineAllowLines(src, marker string) []int {
	_, _ = src, marker

	return nil
}
