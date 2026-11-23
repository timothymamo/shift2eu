package mdlink_test

import (
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/mdlink"
)

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files fstest.MapFS
		check []string
		want  []mdlink.Problem
	}{
		{
			name: "links to files, directories and headings that exist",
			files: fstest.MapFS{
				"README.md": file("# Title\n\nSee [install](docs/guide/install.md#get-the-binaries), " +
					"[the schemas](internal/schema/), [license](LICENSE) and [below](#a-b-c).\n\n" +
					"## A `b`: c!\n\n![logo](docs/assets/logo.png)\n"),
				"docs/guide/install.md":  file("# Install\n\n## Get the binaries\n\nBack to [the start](../../README.md#title).\n"),
				"internal/schema/a.json": file("{}"),
				"LICENSE":                file("Apache License 2.0"),
				"docs/assets/logo.png":   file("png"),
			},
			check: []string{"README.md", "docs/guide/install.md"},
		},
		{
			name: "external URLs, mail and links inside code are not checked",
			files: fstest.MapFS{
				"a.md": file("# A\n\n[web](https://example.com/missing#nothing) [mail](mailto:a@example.com)\n\n" +
					"`[not](a-link.md)` and ``[nor](this.md)``\n\n```markdown\n[fenced](missing.md)\n```\n\n" +
					"  ~~~\n  [tilde](missing.md)\n  ~~~\n"),
			},
			check: []string{"a.md"},
		},
		{
			name: "a missing file, a missing heading and a missing local heading",
			files: fstest.MapFS{
				"docs/a.md": file("# A\n\n[gone](b.md)\n[wrong](c.md#nowhere)\n[here](#absent)\n"),
				"docs/c.md": file("# C\n"),
			},
			check: []string{"docs/a.md"},
			want: []mdlink.Problem{
				{File: "docs/a.md", Line: 3, Link: "b.md", Reason: "docs/b.md does not exist"},
				{File: "docs/a.md", Line: 4, Link: "c.md#nowhere", Reason: "docs/c.md has no heading with the anchor #nowhere"},
				{File: "docs/a.md", Line: 5, Link: "#absent", Reason: "docs/a.md has no heading with the anchor #absent"},
			},
		},
		{
			name: "repeated headings get numbered anchors, as on GitHub",
			files: fstest.MapFS{
				"a.md": file("# A\n\n## Example\n\n## Example\n\n[first](#example) [second](#example-1) [third](#example-2)\n"),
			},
			check: []string{"a.md"},
			want: []mdlink.Problem{
				{File: "a.md", Line: 7, Link: "#example-2", Reason: "a.md has no heading with the anchor #example-2"},
			},
		},
		{
			name: "headings inside fenced code are not headings",
			files: fstest.MapFS{
				"a.md": file("# A\n\n```bash\n# a comment\n```\n\n[comment](#a-comment)\n"),
			},
			check: []string{"a.md"},
			want: []mdlink.Problem{
				{File: "a.md", Line: 7, Link: "#a-comment", Reason: "a.md has no heading with the anchor #a-comment"},
			},
		},
		{
			name: "heading text loses its markup and punctuation",
			files: fstest.MapFS{
				"a.md": file("# A\n\n### Releases before the rename: v0.1.0 and v0.1.0-rc.1\n\n" +
					"## Run the [HTTP API](b.md) **now** ##\n\n## snake_case\n\n" +
					"[x](#releases-before-the-rename-v010-and-v010-rc1) [y](#run-the-http-api-now) [z](#snake_case)\n"),
				"b.md": file("# B\n"),
			},
			check: []string{"a.md"},
		},
		{
			name: "links that leave the tree, are absolute, or point at the repository by URL",
			files: fstest.MapFS{
				"docs/a.md": file("# A\n\n[up](../../outside.md)\n[root](/docs/a.md)\n" +
					"[blob](https://github.com/timothymamo/shift2eu/blob/main/README.md)\n" +
					"[tree](https://github.com/timothymamo/shift2eu/tree/main/docs)\n" +
					"[issue](https://github.com/timothymamo/shift2eu/issues/149)\n"),
			},
			check: []string{"docs/a.md"},
			want: []mdlink.Problem{
				{File: "docs/a.md", Line: 3, Link: "../../outside.md", Reason: "the link leaves the repository"},
				{File: "docs/a.md", Line: 4, Link: "/docs/a.md", Reason: "the link is absolute; use a path relative to docs/a.md"},
				{File: "docs/a.md", Line: 5, Link: "https://github.com/timothymamo/shift2eu/blob/main/README.md", Reason: "the link names the repository by URL; use a relative path"},
				{File: "docs/a.md", Line: 6, Link: "https://github.com/timothymamo/shift2eu/tree/main/docs", Reason: "the link names the repository by URL; use a relative path"},
			},
		},
		{
			name: "an anchor into a file that is not Markdown",
			files: fstest.MapFS{
				"a.md":    file("# A\n\n[line](main.go#L10)\n"),
				"main.go": file("package main\n"),
			},
			check: []string{"a.md"},
			want: []mdlink.Problem{
				{File: "a.md", Line: 3, Link: "main.go#L10", Reason: "main.go is not Markdown, so #L10 names no heading"},
			},
		},
		{
			name: "angle brackets, titles and escaped spaces in the destination",
			files: fstest.MapFS{
				"a.md":       file("# A\n\n[x](<my file.md>) [y](my%20file.md \"title\") [z](b.md 'title')\n"),
				"my file.md": file("# M\n"),
				"b.md":       file("# B\n"),
			},
			check: []string{"a.md"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := mdlink.Check(tc.files, tc.check)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Check problems (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCheckRefusesAFileItCannotRead(t *testing.T) {
	t.Parallel()

	if _, err := mdlink.Check(fstest.MapFS{}, []string{"missing.md"}); err == nil {
		t.Fatal("Check of a missing file: got no error")
	}
}

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func TestHeadings(t *testing.T) {
	t.Parallel()

	got := mdlink.Headings([]byte("# Title\n\nText\n\n```\n# not a heading\n```\n\n## Section ##\n####### seven is text\n#nospace\n   ### Indented\n"))
	want := []mdlink.Heading{
		{Line: 1, Level: 1, Text: "Title"},
		{Line: 9, Level: 2, Text: "Section"},
		{Line: 12, Level: 3, Text: "Indented"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Headings (-want +got):\n%s", diff)
	}
}

func TestLinks(t *testing.T) {
	t.Parallel()

	got := mdlink.Links([]byte("# T\n\n[a](a.md) and ![b](<b c.png> \"t\")\n`[no](code.md)`\n```\n[no](fence.md)\n```\n[d](https://example.com)\n"))
	want := []mdlink.Link{
		{Line: 3, Dest: "a.md"},
		{Line: 3, Dest: "b c.png"},
		{Line: 8, Dest: "https://example.com"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Links (-want +got):\n%s", diff)
	}
}
