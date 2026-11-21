package docs_test

import (
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/internal/mdlink"
)

// repoRoot is the repository's root, from this package's directory.
const repoRoot = "../.."

// markdownFiles lists the repository's root pages, README.md and SECURITY.md,
// and every Markdown file under each of dirs, slash-separated and relative to
// the repository's root, in lexical order. SECURITY.md is a root page too and
// links into docs/, so a moved page must fail here as well.
func markdownFiles(t *testing.T, fsys fs.FS, dirs ...string) []string {
	t.Helper()

	files := []string{"README.md", "SECURITY.md"}
	for _, dir := range dirs {
		err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && path.Ext(p) == ".md" {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("listing %s: %v", dir, err)
		}
	}
	return files
}

// TestRelativeLinksResolve fails on every relative link or #anchor in
// README.md, SECURITY.md, docs/ and the skills that names a file or a heading
// that does not exist, leaves the repository, is absolute, or names this
// repository by URL where a relative link belongs.
func TestRelativeLinksResolve(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)
	files := markdownFiles(t, fsys, "docs", ".claude/skills")

	problems, err := mdlink.Check(fsys, files)
	if err != nil {
		t.Fatalf("checking links: %v", err)
	}
	for _, p := range problems {
		t.Errorf("%s:%d: link %q: %s", p.File, p.Line, p.Link, p.Reason)
	}
}

// TestPagesHaveOneTitle fails on a page in README.md, SECURITY.md or docs/ whose
// first line is not a # heading, or that has a second # heading: a site
// generator takes the page's title from it.
func TestPagesHaveOneTitle(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)
	for _, file := range markdownFiles(t, fsys, "docs") {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		if !strings.HasPrefix(string(data), "# ") {
			t.Errorf("%s: the first line is not the page's # title", file)
		}
		for _, h := range mdlink.Headings(data) {
			if h.Level == 1 && h.Line != 1 {
				t.Errorf("%s:%d: a second # heading, %q; a page has one title", file, h.Line, h.Text)
			}
		}
	}
}
