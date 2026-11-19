package review_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// diffFile returns one file section of a unified diff as git writes it: the
// diff --git header, the two file headers and one hunk that adds added to the
// file at path.
func diffFile(path, added string) string {
	return "diff --git a/" + path + " b/" + path + "\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/" + path + "\n" +
		"+++ b/" + path + "\n" +
		"@@ -1,2 +1,3 @@\n" +
		" package y\n" +
		" \n" +
		"+" + added + "\n"
}

// renamedFile returns the section git writes for a rename from oldPath to
// newPath: the header names both paths, and so do the rename lines and the two
// file headers.
func renamedFile(oldPath, newPath, added string) string {
	return "diff --git a/" + oldPath + " b/" + newPath + "\n" +
		"similarity index 90%\n" +
		"rename from " + oldPath + "\n" +
		"rename to " + newPath + "\n" +
		"--- a/" + oldPath + "\n" +
		"+++ b/" + newPath + "\n" +
		"@@ -1,2 +1,3 @@\n" +
		" package y\n" +
		" \n" +
		"+" + added + "\n"
}

// addedFile returns the section git writes for a file the pull request adds:
// the old side of its file header is /dev/null.
func addedFile(path, added string) string {
	return "diff --git a/" + path + " b/" + path + "\n" +
		"new file mode 100644\n" +
		"--- /dev/null\n" +
		"+++ b/" + path + "\n" +
		"@@ -0,0 +1,1 @@\n" +
		"+" + added + "\n"
}

// deletedFile returns the section git writes for a file the pull request
// deletes: the new side of its file header is /dev/null.
func deletedFile(path, removed string) string {
	return "diff --git a/" + path + " b/" + path + "\n" +
		"deleted file mode 100644\n" +
		"--- a/" + path + "\n" +
		"+++ /dev/null\n" +
		"@@ -1,1 +0,0 @@\n" +
		"-" + removed + "\n"
}

// binaryFile returns the section git writes for a binary file it does not show
// as text.
func binaryFile(path string) string {
	return "diff --git a/" + path + " b/" + path + "\n" +
		"index 1111111..2222222 100644\n" +
		"Binary files a/" + path + " and b/" + path + " differ\n"
}

// quotedFile returns the section git writes for a path it quotes: one holding a
// character it cannot write bare, a space here.
func quotedFile(path, added string) string {
	return "diff --git \"a/" + path + "\" \"b/" + path + "\"\n" +
		"--- \"a/" + path + "\"\n" +
		"+++ \"b/" + path + "\"\n" +
		"@@ -1,2 +1,3 @@\n" +
		" package y\n" +
		" \n" +
		"+" + added + "\n"
}

// TestFilterDiffLeavesGeneratedAndGoldenFilesOut pins what the reviewer does not
// read (issue #324, ADR 0033): the diff it sends the model and measures against
// MaxDiffBytes is the pull request's diff without the generated OpenAPI bundle
// (api/openapi.bundled.json) and without the golden files of a testdata
// directory, and every file it left out is listed once, sorted, so the review
// that names them reads the same for the same diff.
//
// The list is the reviewer's own: it is fixed in this package and no pull
// request can widen it. A path counts only exactly as git writes it, so a path
// holding a ".." element, a path in another case and a ".golden" file outside a
// testdata directory are reviewed; and a section only goes when every path it
// names does, so a pull request cannot hide a code file by renaming it into a
// left-out name or a left-out file by renaming it out of one. A section is cut
// at its "diff --git " header alone: a line of a hunk is a content line, even
// when it reads like a header.
func TestFilterDiffLeavesGeneratedAndGoldenFilesOut(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		diff         string
		wantKept     string
		wantExcluded []string
	}{
		{
			name:     "a diff of code is kept whole",
			diff:     diffFile("internal/x/y.go", "CODE-ONE") + diffFile("internal/x/z.go", "CODE-TWO"),
			wantKept: diffFile("internal/x/y.go", "CODE-ONE") + diffFile("internal/x/z.go", "CODE-TWO"),
		},
		{
			name: "the generated OpenAPI bundle is left out",
			diff: diffFile("internal/x/y.go", "CODE-ONE") +
				diffFile("api/openapi.bundled.json", "GENERATED") +
				diffFile("internal/x/z.go", "CODE-TWO"),
			wantKept:     diffFile("internal/x/y.go", "CODE-ONE") + diffFile("internal/x/z.go", "CODE-TWO"),
			wantExcluded: []string{"api/openapi.bundled.json"},
		},
		{
			name:         "a golden file of a testdata directory is left out",
			diff:         diffFile("testdata/comparison-v3.golden", "GOLDEN"),
			wantExcluded: []string{"testdata/comparison-v3.golden"},
		},
		{
			name:         "a golden file of a nested testdata directory is left out",
			diff:         diffFile("internal/x/testdata/a/b/c.golden", "GOLDEN"),
			wantExcluded: []string{"internal/x/testdata/a/b/c.golden"},
		},
		{
			name:         "a golden file of testdata at the repository root is left out",
			diff:         diffFile("testdata/x.golden", "GOLDEN"),
			wantExcluded: []string{"testdata/x.golden"},
		},
		{
			name: "a golden file of testdata is left out wherever it sits in the diff",
			diff: diffFile("testdata/z.golden", "GOLDEN") +
				diffFile("api/openapi.bundled.json", "GENERATED") +
				diffFile("internal/x/testdata/a.golden", "GOLDEN"),
			wantKept:     "",
			wantExcluded: []string{"api/openapi.bundled.json", "internal/x/testdata/a.golden", "testdata/z.golden"},
		},
		{
			name:     "a golden file outside a testdata directory is kept",
			diff:     diffFile("internal/x/code.golden", "GOLDEN") + diffFile("mytestdata/a.golden", "GOLDEN") + diffFile("testdata.golden", "GOLDEN"),
			wantKept: diffFile("internal/x/code.golden", "GOLDEN") + diffFile("mytestdata/a.golden", "GOLDEN") + diffFile("testdata.golden", "GOLDEN"),
		},
		{
			// The path is compared as git writes it: cleaning it first would
			// leave a generated file behind a name no review ever sees.
			name:     "the generated path with a .. element is kept",
			diff:     diffFile("api/../api/openapi.bundled.json", "GENERATED"),
			wantKept: diffFile("api/../api/openapi.bundled.json", "GENERATED"),
		},
		{
			name:     "the generated path in another case is kept",
			diff:     diffFile("API/openapi.bundled.json", "GENERATED"),
			wantKept: diffFile("API/openapi.bundled.json", "GENERATED"),
		},
		{
			// A rename names two paths: the section goes only when both are
			// left out, so a pull request cannot keep a code file out of the
			// review by renaming it to a left-out name or hide a left-out file
			// by renaming it to a reviewed one.
			name:     "a golden file renamed out of testdata is kept",
			diff:     renamedFile("testdata/a.golden", "internal/x/a.go", "CODE"),
			wantKept: renamedFile("testdata/a.golden", "internal/x/a.go", "CODE"),
		},
		{
			name:     "a code file renamed into testdata as a golden file is kept",
			diff:     renamedFile("internal/x/a.go", "testdata/a.golden", "GOLDEN"),
			wantKept: renamedFile("internal/x/a.go", "testdata/a.golden", "GOLDEN"),
		},
		{
			name:         "a golden file renamed within testdata is left out",
			diff:         renamedFile("testdata/a.golden", "testdata/b.golden", "GOLDEN"),
			wantKept:     "",
			wantExcluded: []string{"testdata/a.golden", "testdata/b.golden"},
		},
		{
			name:         "a deleted golden file is left out",
			diff:         deletedFile("testdata/old.golden", "GOLDEN"),
			wantExcluded: []string{"testdata/old.golden"},
		},
		{
			name:         "an added generated file is left out",
			diff:         addedFile("api/openapi.bundled.json", "GENERATED"),
			wantExcluded: []string{"api/openapi.bundled.json"},
		},
		{
			name:         "a binary golden file is left out",
			diff:         binaryFile("testdata/image.golden"),
			wantExcluded: []string{"testdata/image.golden"},
		},
		{
			name:     "a header line spoofed inside a hunk does not split the section",
			diff:     diffFile("internal/x/y.go", "diff --git a/api/openapi.bundled.json b/api/openapi.bundled.json"),
			wantKept: diffFile("internal/x/y.go", "diff --git a/api/openapi.bundled.json b/api/openapi.bundled.json"),
		},
		{
			name:     "a quoted path is never left out",
			diff:     quotedFile("x y.go", "CODE-QUOTED"),
			wantKept: quotedFile("x y.go", "CODE-QUOTED"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kept, excluded := review.FilterDiff(tc.diff)

			if diff := cmp.Diff(tc.wantKept, kept); diff != "" {
				t.Errorf("FilterDiff kept the diff (-want +got):\n%s", diff)
			}

			if !slices.Equal(tc.wantExcluded, excluded) {
				t.Errorf("FilterDiff left out %v, want %v", excluded, tc.wantExcluded)
			}

			if !slices.IsSorted(excluded) {
				t.Errorf("FilterDiff left out %v in no order; the list is sorted, so the review names the files in the same order for the same diff", excluded)
			}

			if len(slices.Compact(slices.Clone(excluded))) != len(excluded) {
				t.Errorf("FilterDiff left out %v, which names a file twice; every left-out path is listed once", excluded)
			}

			// The same diff gives the same answer: the filter is a function of
			// the diff alone, so a rerun of the review leaves out the same
			// files.
			againKept, againExcluded := review.FilterDiff(tc.diff)

			if diff := cmp.Diff(kept, againKept); diff != "" {
				t.Errorf("FilterDiff kept another diff the second time (-first +second):\n%s", diff)
			}

			if !slices.Equal(excluded, againExcluded) {
				t.Errorf("FilterDiff left out %v the second time, want %v", againExcluded, excluded)
			}
		})
	}
}
