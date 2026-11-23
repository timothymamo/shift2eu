package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This file holds the checks of the guide extractor (issue #373, item 15; the
// review's findings 2 and 3 and the auditor's 2 and 3): sourcesData reads each
// source's commands out of docs/guide/inventory.md, and the page ships without
// them when the extraction goes wrong, so every way the guide can break the
// extraction must fail the build with an error that names the heading, and no
// fence or comment outside a source's section may be read as one of its
// headings.
//
// The checks call sourcesData with a root of their own, holding a copy of the
// guide: the extractor is unexported and build() writes a whole bundle, so a
// fixture root is the lowest level at which a broken guide is observed, and the
// error the checks read is the error build() returns unchanged. The fixtures are
// edits of the real guide, never a copy of it, so a change to the guide cannot
// leave a fixture behind.

// TestSourcesDataFailsOnABrokenGuide pins the ways the guide can break the
// extraction (issue #373, item 15): a source heading that is gone, a section with
// no bash block, a bash block left open, a section that holds only comments, a
// fenced block under a source heading that is not bash, and the same source
// heading twice. Each must fail, and each failure must name the heading, so a
// visitor is never shown a page that silently lost a source's commands or the
// commands after one.
func TestSourcesDataFailsOnABrokenGuide(t *testing.T) {
	t.Parallel()

	aws := guideSources[0]
	gcp := guideSources[1]

	tests := []struct {
		name string
		// heading is the heading the error must name.
		heading string
		// edit applies the break to a guide of its own.
		edit func(t *testing.T, lines []string) []string
	}{
		{
			name:    "a source heading is removed",
			heading: gcp.collect,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				return withoutHeading(t, lines, gcp.collect)
			},
		},
		{
			name:    "a heading holds no bash block",
			heading: gcp.collect,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				return withoutBashBlock(t, lines, gcp.collect)
			},
		},
		{
			name:    "a bash block is left open",
			heading: aws.build,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				_, closeAt := bashFence(t, lines, aws.build)

				return splice(lines, closeAt, closeAt+1, nil)
			},
		},
		{
			name:    "a section holds only comments",
			heading: gcp.build,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				open, closeAt := bashFence(t, lines, gcp.build)

				return splice(lines, open+1, closeAt, []string{"# Nothing to collect here."})
			},
		},
		{
			name:    "a section holds a sh block",
			heading: aws.collect,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				return withExtraFence(t, lines, aws.collect, "sh")
			},
		},
		{
			name:    "a section holds a text block",
			heading: aws.collect,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				return withExtraFence(t, lines, aws.collect, "text")
			},
		},
		{
			name:    "a section holds a block with no info string",
			heading: aws.collect,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				return withExtraFence(t, lines, aws.collect, "")
			},
		},
		{
			name:    "a section holds a ~~~ text block",
			heading: aws.collect,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				return withExtraTildeFence(t, lines, aws.collect, "text")
			},
		},
		{
			name:    "a section holds a bare ~~~ block",
			heading: aws.collect,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				return withExtraTildeFence(t, lines, aws.collect, "")
			},
		},
		{
			name:    "the same source heading twice",
			heading: aws.collect,
			edit: func(t *testing.T, lines []string) []string {
				t.Helper()

				section := []string{
					"### " + aws.collect,
					"",
					"```bash",
					"aws s3api list-buckets --output json",
					"```",
					"",
				}

				return splice(lines, firstSectionLine(t, lines), firstSectionLine(t, lines), section)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := extract(t, tc.edit(t, theGuide(t)))
			if err == nil {
				t.Fatalf("sourcesData returned no error for a guide whose %q section is broken, want an error naming the heading: the page would ship without those commands (issue #373, item 15)", tc.heading)
			}

			if !strings.Contains(err.Error(), tc.heading) {
				t.Errorf("sourcesData failed with %q, want an error that names the heading %q: a failure that does not say which section broke the build leaves the next reader to find it (issue #373, item 15)",
					err.Error(), tc.heading)
			}
		})
	}
}

// TestSourcesDataIgnoresHeadingsInsideAFence pins the other half of the fence
// rule (issue #373, item 15, review finding 2): a line that starts with # inside
// a fenced block is text, not a heading, so a ```text block - and a ~~~text
// block, the other fence Markdown writes - that sits before the six source
// sections and shows `# Collect from GCP` is read neither as that section's start
// nor as a second heading of it. A fence opened by N backticks closes only on a
// line of at least N of them (CommonMark), so a shorter run of backticks inside
// a longer fence is text too and ends nothing. The commands stay the guide's own.
func TestSourcesDataIgnoresHeadingsInsideAFence(t *testing.T) {
	t.Parallel()

	guide := theGuide(t)

	want, err := extract(t, guide)
	if err != nil {
		t.Fatalf("sourcesData failed on docs/guide/inventory.md: %v", err)
	}

	tests := []struct {
		name  string
		fence []string
	}{
		{
			name:  "a ```text block",
			fence: []string{"```text", "# " + guideSources[1].collect, "```", ""},
		},
		{
			name:  "a ~~~text block",
			fence: []string{"~~~text", "# " + guideSources[1].collect, "~~~", ""},
		},
		{
			name:  "a ````text block holding a ``` line",
			fence: []string{"````text", "```", "# " + guideSources[1].collect, "````", ""},
		},
	}

	at := firstSectionLine(t, guide)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := extract(t, splice(guide, at, at, tc.fence))
			if err != nil {
				t.Fatalf("sourcesData failed on a guide whose %s before the source sections holds %q, want it read as text: a line starting with # inside that fence is no heading (issue #373, item 15): %v",
					tc.name, "# "+guideSources[1].collect, err)
			}

			for _, source := range guideSources {
				if want[source.key].Name != got[source.key].Name {
					t.Errorf("the %s source's name is %q after %s, want %q (issue #373, item 15)",
						source.key, got[source.key].Name, tc.name, want[source.key].Name)
				}

				for _, step := range []struct {
					name string
					want []string
					got  []string
				}{
					{name: "collect", want: want[source.key].CollectCommands, got: got[source.key].CollectCommands},
					{name: "build", want: want[source.key].BuildCommands, got: got[source.key].BuildCommands},
				} {
					if !slices.Equal(step.want, step.got) {
						t.Errorf("the %s source's %s commands are %q after %s, want the guide's own %q: the fence's # line ends neither the section nor starts it early (issue #373, item 15)",
							source.key, step.name, step.got, tc.name, step.want)
					}
				}
			}
		})
	}
}

// TestSourcesDataKeepsALongerFenceOpen pins the longer-fence half of the fence
// rule in sectionCommands (issue #373, item 15, review nit 2): a ```bash block
// opened by four backticks closes only on a line of at least four, so a ``` or
// ```bash line inside it is command text of that block, neither a close nor a
// second block. Closing at the shorter run instead would drop both lines from
// the page's commands without a word.
func TestSourcesDataKeepsALongerFenceOpen(t *testing.T) {
	t.Parallel()

	aws := guideSources[0]

	guide := theGuide(t)
	open, closeAt := bashFence(t, guide, aws.collect)

	want := "aws s3api list-buckets --output json\n" +
		"```\n" +
		"```bash\n" +
		"aws s3api list-objects-v2 --bucket 123456789012-example"

	block := []string{
		"````bash",
		"aws s3api list-buckets --output json",
		"```",
		"```bash",
		"aws s3api list-objects-v2 --bucket 123456789012-example",
		"````",
	}

	got, err := extract(t, splice(guide, open, closeAt+1, block))
	if err != nil {
		t.Fatalf("sourcesData failed on a guide whose %q section holds a ````bash block with ``` lines in it, want it read as one block (issue #373, item 15): %v",
			aws.collect, err)
	}

	if !slices.Contains(got[aws.key].CollectCommands, want) {
		t.Errorf("the %s collect commands are %q, want one of them %q: a ``` or ```bash line inside a ````bash block is command text, and only a line of four backticks closes the block (issue #373, item 15)",
			aws.key, got[aws.key].CollectCommands, want)
	}
}

// theGuide reads docs/guide/inventory.md as the extractor reads it.
func theGuide(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("..", "..", filepath.FromSlash(guideFile))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}

// extract writes guide into a root of its own, under the guide's own path, and
// returns what the builder reads back from it.
func extract(t *testing.T, guide []string) (map[string]sourceData, error) {
	t.Helper()

	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(guideFile))

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create the fixture guide's directory: %v", err)
	}

	if err := os.WriteFile(path, []byte(strings.Join(guide, "\n")), 0o600); err != nil {
		t.Fatalf("write the fixture guide: %v", err)
	}

	return sourcesData(root)
}

// splice returns lines with at most remove lines dropped from at and with insert
// put in their place.
func splice(lines []string, at, remove int, insert []string) []string {
	out := make([]string, 0, len(lines)+len(insert))

	out = append(out, lines[:at]...)
	out = append(out, insert...)

	return append(out, lines[min(remove, len(lines)):]...)
}

// indexOfHeading returns the index of the line of the heading under name,
// failing the test when the guide holds none.
func indexOfHeading(t *testing.T, lines []string, name string) int {
	t.Helper()

	for i, line := range lines {
		if headingText(line) == name {
			return i
		}
	}

	t.Fatalf("docs/guide/inventory.md holds no heading %q, so this fixture cannot break it (issue #373, item 15)", name)

	return -1
}

// withoutHeading returns lines without the heading's line.
func withoutHeading(t *testing.T, lines []string, name string) []string {
	t.Helper()

	at := indexOfHeading(t, lines, name)

	return splice(lines, at, at+1, nil)
}

// bashFence returns the indices of the ```bash line under the heading and of the
// line that closes it, failing the test when the section holds no bash block.
func bashFence(t *testing.T, lines []string, name string) (int, int) {
	t.Helper()

	heading := indexOfHeading(t, lines, name)

	for i := heading + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```bash" {
			continue
		}

		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				return i, j
			}
		}

		t.Fatalf("the ```bash block under %q is never closed, so this fixture cannot break it (issue #373, item 15)", name)
	}

	t.Fatalf("the guide holds no ```bash block under %q, so this fixture cannot break it (issue #373, item 15)", name)

	return -1, -1
}

// withoutBashBlock returns lines without the bash block under the heading.
func withoutBashBlock(t *testing.T, lines []string, name string) []string {
	t.Helper()

	open, closeAt := bashFence(t, lines, name)

	return splice(lines, open, closeAt+1, nil)
}

// withExtraFence returns lines with a fenced block of the info string inserted
// under the heading, before its bash block: a block the extraction must never
// drop without a word.
func withExtraFence(t *testing.T, lines []string, name, info string) []string {
	t.Helper()

	at := indexOfHeading(t, lines, name)

	fence := []string{
		"```" + info,
		"This note is no command: the page shows the commands below it.",
		"```",
		"",
	}

	return splice(lines, at+1, at+1, fence)
}

// withExtraTildeFence returns lines with a ~~~ fenced block of the info string
// inserted under the heading, before its bash block, holding a line that starts
// with #: a ~~~ block is a block as ``` is, so the extraction must never read
// its lines as prose, a heading or a command of the section.
func withExtraTildeFence(t *testing.T, lines []string, name, info string) []string {
	t.Helper()

	at := indexOfHeading(t, lines, name)

	fence := []string{
		"~~~" + info,
		"# A note in a ~~~ block, not a command and not a heading.",
		"~~~",
		"",
	}

	return splice(lines, at+1, at+1, fence)
}

// firstSectionLine returns the index of the guide's first source section heading,
// where the fixtures that must not be read as a heading are inserted.
func firstSectionLine(t *testing.T, lines []string) int {
	t.Helper()

	var headings []string

	for _, source := range guideSources {
		headings = append(headings, source.collect, source.build)
	}

	for i, line := range lines {
		if slices.Contains(headings, headingText(line)) {
			return i
		}
	}

	t.Fatalf("the guide holds no source section, so this fixture read nothing (issue #373, item 15)")

	return -1
}
