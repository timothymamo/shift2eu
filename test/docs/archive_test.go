package docs_test

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

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

// TestRootPagesLinksStayInTheArchive fails on a relative link in a root page of
// the repository, README.md or SECURITY.md, to a file or directory a release
// archive does not ship: every archive ships both pages, so their links must
// work in an unpacked archive too, whichever binary's archive it is. Deep links
// into the source belong in the pages under docs/.
func TestRootPagesLinksStayInTheArchive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// page is the root page whose links the case checks, slash-separated
		// and relative to the repository's root.
		page string
	}{
		{name: "README.md", page: "README.md"},
		{name: "SECURITY.md", page: "SECURITY.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := os.DirFS(repoRoot)
			archives := archiveFiles(t, fsys)
			for i, files := range archives {
				if !shipped(tc.page, files) {
					t.Fatalf(".goreleaser.yaml: archive %d does not ship %s; files: %q", i+1, tc.page, files)
				}
			}

			data, err := fs.ReadFile(fsys, tc.page)
			if err != nil {
				t.Fatalf("reading %s: %v", tc.page, err)
			}
			for _, l := range mdlink.Links(data) {
				target, _, _ := strings.Cut(l.Dest, "#")
				if target == "" || strings.Contains(target, ":") {
					continue
				}
				name := path.Clean(target)
				for i, files := range archives {
					if !shipped(name, files) {
						t.Errorf("%s:%d: link %q: archive %d of .goreleaser.yaml does not ship %s (archives: files: %q)", tc.page, l.Line, l.Dest, i+1, name, files)
					}
				}
			}
		})
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
//     sub-index links is a page, not an index. Only a link to the README.md
//     file itself makes a sub-index; a link to its directory does not.
//
// A link to a directory lists every Markdown page directly inside it, so a
// dated report needs no index edit of its own; a page in a subdirectory of a
// linked directory is not covered by that link, and a README.md the link
// lists is a page, not an index.
func indexProblems(fsys fs.FS) ([]string, error) {
	const index = "docs/README.md"
	listed := map[string]bool{}

	// listLinks marks what the index or sub-index file links as listed: a
	// page, or every Markdown page directly inside a linked directory. It
	// returns the links that name a sub-index of docs/README.md's, which only
	// the index's own links can.
	listLinks := func(file string) ([]string, error) {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, err
		}
		var subIndexes []string
		for _, l := range mdlink.Links(data) {
			target, _, _ := strings.Cut(l.Dest, "#")
			if target == "" || strings.Contains(target, ":") {
				continue
			}
			name := path.Join(path.Dir(file), target)
			info, err := fs.Stat(fsys, name)
			if err != nil {
				continue // a missing target is TestRelativeLinksResolve's to report
			}
			if !info.IsDir() {
				listed[name] = true
				if path.Base(name) == "README.md" && path.Dir(path.Dir(name)) == "docs" {
					subIndexes = append(subIndexes, name)
				}
				continue
			}
			entries, err := fs.ReadDir(fsys, name)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && path.Ext(e.Name()) == ".md" {
					listed[path.Join(name, e.Name())] = true
				}
			}
		}
		return subIndexes, nil
	}

	subIndexes, err := listLinks(index)
	if err != nil {
		return nil, err
	}
	for _, sub := range subIndexes {
		if _, err := listLinks(sub); err != nil {
			return nil, err
		}
	}

	unlisted := []string{}
	err = fs.WalkDir(fsys, "docs", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path.Ext(p) == ".md" && p != index && !listed[p] {
			unlisted = append(unlisted, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return unlisted, nil
}

// writeDocsTree writes files, a map from a slash-separated path to its
// contents, under a temporary directory and returns that directory as an fs.FS,
// so a test reads a docs tree of its own.
func writeDocsTree(t *testing.T, files map[string]string) fs.FS {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("creating the directory of %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
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
		{
			name: "a page of any name in a directory a linked sub-index links is indexed",
			files: map[string]string{
				"docs/README.md":                         "# Documentation\n\n- [Security](security/README.md)\n",
				"docs/security/README.md":                "# Security\n\nEvery report in [audits/](audits/), newest first.\n",
				"docs/security/audits/TEMPLATE.md":       "# Audit report template\n",
				"docs/security/audits/2026-12-01-123.md": "# Audit report, 2026-12-01\n",
			},
		},
		{
			name: "a ./ prefix and an anchor do not stop a link listing a page or a directory",
			files: map[string]string{
				"docs/README.md":                         "# Documentation\n\n- [Security](./security/README.md#the-reports)\n",
				"docs/security/README.md":                "# Security\n\n## The reports\n\nEvery report in [audits/](./audits/), newest first.\n",
				"docs/security/audits/2026-12-01-123.md": "# Audit report, 2026-12-01\n",
			},
		},
		{
			name: "a page a directory-linked README.md links by file is not indexed",
			files: map[string]string{
				"docs/README.md":                         "# Documentation\n\n- [Security](security/)\n",
				"docs/security/README.md":                "# Security\n\n- [Report](audits/2026-12-01-123.md)\n",
				"docs/security/audits/2026-12-01-123.md": "# Audit report, 2026-12-01\n",
			},
			// A link to a directory lists the Markdown pages directly inside
			// it, security/README.md among them, but it makes no sub-index of
			// the directory's README.md: only a link to that file does.
			unlisted: []string{"docs/security/audits/2026-12-01-123.md"},
		},
		{
			name: "a README.md a directory link reaches is no index of its own",
			files: map[string]string{
				"docs/README.md":     "# Documentation\n\n- [A](a/)\n",
				"docs/a/b/README.md": "# B\n\n- [Page](c.md)\n",
				"docs/a/b/c.md":      "# Page\n",
			},
			// A directory link lists only the pages directly inside the
			// directory; it does not make the README.md below it an index, so
			// no chain of indexes arises through directory links.
			unlisted: []string{
				"docs/a/b/README.md",
				"docs/a/b/c.md",
			},
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
			if diff := cmp.Diff(tc.unlisted, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("pages under docs/ that no index lists (-want +got):\n%s", diff)
			}
		})
	}
}
