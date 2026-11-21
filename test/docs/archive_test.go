package docs_test

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/mdlink"
	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// archiveFiles reads the files each archive in .goreleaser.yaml ships beside
// its binary: one list per archives: entry, in the order the document writes
// them, since issue #147 gives each binary an archive of its own.
func archiveFiles(t *testing.T, fsys fs.FS) [][]string {
	t.Helper()

	data, err := fs.ReadFile(fsys, ".goreleaser.yaml")
	if err != nil {
		t.Fatalf("reading .goreleaser.yaml: %v", err)
	}
	lines, err := yamlread.SplitLines(data)
	if err != nil {
		t.Fatalf(".goreleaser.yaml: %v", err)
	}
	block, err := yamlread.Walk(lines, "archives")
	if err != nil {
		t.Fatalf(".goreleaser.yaml: %v", err)
	}
	archives, err := yamlread.Sequence(block)
	if err != nil {
		t.Fatalf(".goreleaser.yaml: archives: %v", err)
	}
	if len(archives) == 0 {
		t.Fatal(".goreleaser.yaml: archives: holds no entry, so the release ships nothing this test could read")
	}
	lists := make([][]string, 0, len(archives))
	for i, archive := range archives {
		entries, err := yamlread.Mapping(archive.Lines)
		if err != nil {
			t.Fatalf(".goreleaser.yaml: archive %d: %v", i+1, err)
		}
		files := []string(nil)
		for _, e := range entries {
			if e.Key == "files" {
				files, err = yamlread.List(e)
				if err != nil {
					t.Fatalf(".goreleaser.yaml: archive %d: files: %v", i+1, err)
				}
			}
		}
		if files == nil {
			t.Fatalf(".goreleaser.yaml: archive %d lists no files", i+1)
		}
		lists = append(lists, files)
	}
	return lists
}

// shipped reports whether name is in an archive whose files are files, read
// as GoReleaser v2 reads them: D/**/* matches every file in a directory below
// D, but not a file directly in D, and any other entry is a path.Match
// pattern. A directory is shipped when a file in it is.
func shipped(name string, files []string) bool {
	for _, f := range files {
		if dir, ok := strings.CutSuffix(f, "/**/*"); ok {
			if rest, ok := strings.CutPrefix(name, dir+"/"); ok && strings.Contains(rest, "/") {
				return true
			}
			continue
		}
		if ok, err := path.Match(f, name); err == nil && ok {
			return true
		}
	}
	return false
}

// TestReadmeLinksStayInTheArchive fails on a relative link in README.md to a
// file or directory a release archive does not ship: every archive ships
// README.md, so its links must work in an unpacked archive too, whichever
// binary's archive it is. Deep links into the source belong in the pages under
// docs/.
func TestReadmeLinksStayInTheArchive(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)
	archives := archiveFiles(t, fsys)
	for i, files := range archives {
		if !shipped("README.md", files) {
			t.Fatalf(".goreleaser.yaml: archive %d does not ship README.md; files: %q", i+1, files)
		}
	}

	data, err := fs.ReadFile(fsys, "README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	for _, l := range mdlink.Links(data) {
		target, _, _ := strings.Cut(l.Dest, "#")
		if target == "" || strings.Contains(target, ":") {
			continue
		}
		name := path.Clean(target)
		for i, files := range archives {
			if !shipped(name, files) {
				t.Errorf("README.md:%d: link %q: archive %d of .goreleaser.yaml does not ship %s (archives: files: %q)", l.Line, l.Dest, i+1, name, files)
			}
		}
	}
}

// indexProblems returns every Markdown page under docs/ in fsys that no index
// lists, slash-separated and relative to fsys's root, in lexical order, or an
// error when it cannot read one of them. Only docs/ is read: a root README.md
// is none of its business. A page is listed when an index links it, directly or
// through a directory. There are two kinds of index (PRD #175 milestone 1,
// issue #280):
//
//   - docs/README.md, the documentation's index;
//   - a sub-index: a README.md in a directory directly below docs/ that
//     docs/README.md links. One level only, no chains: a README.md that only a
//     sub-index links is a page, not an index.
//
// A link to a directory lists every Markdown page directly inside it, so a
// dated report needs no index edit of its own; a page in a subdirectory of a
// linked directory is not covered by that link.
func indexProblems(fsys fs.FS) ([]string, error)

// writeDocsTree writes files, a map from a slash-separated path to its
// contents, under a temporary directory and returns that directory as an fs.FS,
// so a test reads a docs tree of its own.
func writeDocsTree(t *testing.T, files map[string]string) fs.FS {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("creating the directory of %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return os.DirFS(root)
}

// TestIndexListsEveryPage fails on a Markdown page under docs/ that no index
// lists: neither docs/README.md, the documentation's index, nor a sub-index it
// links, nor a directory one of them links (indexProblems). The first case is
// the repository's own tree; the others are small docs trees written for the
// test, so the rule is pinned without a page of the real tree changing.
func TestIndexListsEveryPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// files is the docs tree to write to a temporary directory, from a
		// slash-separated path to its contents. Empty reads the repository's
		// own tree.
		files map[string]string
		// unlisted are the pages no index lists, in lexical order.
		unlisted []string
	}{
		{
			name: "the repository's own docs tree",
		},
		{
			name: "a page the index links is indexed",
			files: map[string]string{
				"docs/README.md":        "# Documentation\n\n- [Install](guide/install.md)\n",
				"docs/guide/install.md": "# Install\n",
			},
		},
		{
			name: "a page no index links is not indexed",
			files: map[string]string{
				"docs/README.md":        "# Documentation\n",
				"docs/guide/install.md": "# Install\n",
			},
			unlisted: []string{"docs/guide/install.md"},
		},
		{
			name: "a page a linked sub-index links is indexed",
			files: map[string]string{
				"docs/README.md":                       "# Documentation\n\n- [Security](security/README.md)\n",
				"docs/security/README.md":              "# Security\n\n- [Report](audits/2026-12-01-1.md)\n",
				"docs/security/audits/2026-12-01-1.md": "# Audit report, 2026-12-01\n",
			},
		},
		{
			name: "a page only an unlinked sub-index links is not indexed",
			files: map[string]string{
				"docs/README.md":                       "# Documentation\n\n- [Install](guide/install.md)\n",
				"docs/guide/install.md":                "# Install\n",
				"docs/security/README.md":              "# Security\n\n- [Report](audits/2026-12-01-1.md)\n",
				"docs/security/audits/2026-12-01-1.md": "# Audit report, 2026-12-01\n",
			},
			unlisted: []string{
				"docs/security/README.md",
				"docs/security/audits/2026-12-01-1.md",
			},
		},
		{
			name: "a page only a sub-index of a sub-index links is not indexed",
			files: map[string]string{
				"docs/README.md":     "# Documentation\n\n- [A](a/README.md)\n",
				"docs/a/README.md":   "# A\n\n- [B](b/README.md)\n",
				"docs/a/b/README.md": "# B\n\n- [Page](page.md)\n",
				"docs/a/b/page.md":   "# Page\n",
			},
			unlisted: []string{"docs/a/b/page.md"},
		},
		{
			name: "a page in a directory a linked sub-index links is indexed",
			files: map[string]string{
				"docs/README.md":                         "# Documentation\n\n- [Security](security/README.md)\n",
				"docs/security/README.md":                "# Security\n\nEvery report in [audits/](audits/), newest first.\n",
				"docs/security/audits/2026-11-01-1.md":   "# Audit report, 2026-11-01\n",
				"docs/security/audits/2026-12-01-123.md": "# Audit report, 2026-12-01\n",
			},
		},
		{
			name: "a page in a directory a linked sub-index links without a trailing slash is indexed",
			files: map[string]string{
				"docs/README.md":                         "# Documentation\n\n- [Security](security/README.md)\n",
				"docs/security/README.md":                "# Security\n\nEvery report in [audits](audits), newest first.\n",
				"docs/security/audits/2026-12-01-123.md": "# Audit report, 2026-12-01\n",
			},
		},
		{
			name: "a page in a directory the index links is indexed",
			files: map[string]string{
				"docs/README.md":                         "# Documentation\n\n- [Security](security/README.md)\n- [Audit reports](security/audits/)\n",
				"docs/security/README.md":                "# Security\n",
				"docs/security/audits/2026-12-01-123.md": "# Audit report, 2026-12-01\n",
			},
		},
		{
			name: "a page deeper than a linked directory is not indexed",
			files: map[string]string{
				"docs/README.md":                         "# Documentation\n\n- [Security](security/README.md)\n",
				"docs/security/README.md":                "# Security\n\nEvery report in [audits/](audits/), newest first.\n",
				"docs/security/audits/2026-12-01-123.md": "# Audit report, 2026-12-01\n",
				"docs/security/audits/old/x.md":          "# An older report, moved here\n",
			},
			unlisted: []string{"docs/security/audits/old/x.md"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := os.DirFS(repoRoot)
			if tc.files != nil {
				fsys = writeDocsTree(t, tc.files)
			}

			got, err := indexProblems(fsys)
			if err != nil {
				t.Fatalf("checking which pages the indexes list: %v", err)
			}
			want := tc.unlisted
			if want == nil {
				want = []string{}
			}
			if got == nil {
				got = []string{}
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("pages under docs/ that no index lists (-want +got):\n%s", diff)
			}
		})
	}
}
