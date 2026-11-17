package docs_test

import (
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// shellFileWrites are the ways a command writes or removes a file through the
// shell that CLAUDE.md rule 9 forbids agents (issue #251): unattended panes
// stop at a permission prompt nobody answers for them. Agents change files
// with their file-editing tool instead. The list covers the forms seen in
// #251 and is not complete; see the package doc for the gaps.
var shellFileWrites = []struct {
	name string
	re   *regexp.Regexp
}{
	{"a heredoc", regexp.MustCompile(`(^|[^<])<<(?:[^<]|$)`)},
	{"cat >", regexp.MustCompile(`\bcat\s+>>?`)},
	{"tee", regexp.MustCompile(`\btee\b`)},
	{"sed -i", regexp.MustCompile(`\bsed\b[^|;&]*\s(?:-[a-zA-Z]*i\b|--in-place)`)},
	{"perl -i", regexp.MustCompile(`\bperl\b[^|;&]*\s-[a-zA-Z]*i\b`)},
	{"a python - script", regexp.MustCompile(`\bpython3?\s+-(\s|$)`)},
	{"rm of a variable", regexp.MustCompile(`(^|[\s;&|(])rm\s[^|;&]*\$`)},
	{"a redirection to a file", regexp.MustCompile(`\d?&?>>?\s*[^\s&>]`)},
}

// devNullRedirect is a redirection whose target is exactly /dev/null, the one
// file a command may write: its bytes are discarded, not stored. The target
// must end there, so /dev/null.txt is still a file.
var devNullRedirect = regexp.MustCompile(`\d?&?>>?\s*/dev/null\b(?:\s|$)`)

// placeholderEnd is the closing > of a <placeholder> such as <n> or
// <repository root>: prose in a command, not a redirection to a file. Only the
// closing bracket is dropped, so a real redirection before a placeholder is
// still seen.
var placeholderEnd = regexp.MustCompile(`(<[^<>]*)>`)

// shellWriteIn returns the names of the shellFileWrites patterns that make
// line write or remove a file through the shell, in the order the patterns
// are declared. A line starting with # is a comment and names these forms as
// prose, so it is never flagged.
func shellWriteIn(line string) []string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return nil
	}
	checked := placeholderEnd.ReplaceAllString(devNullRedirect.ReplaceAllString(line, " "), "$1")
	var names []string
	for _, w := range shellFileWrites {
		if w.re.MatchString(checked) {
			names = append(names, w.name)
		}
	}
	return names
}

// shellFinding is a line inside a shell fenced block that writes or removes a
// file through the shell.
type shellFinding struct {
	line     int
	text     string
	patterns []string
}

// shellWritesIn returns a finding for every line of data that sits inside a
// fenced code block tagged bash, sh, shell or console, or not tagged at all,
// and writes or removes a file through the shell. A block tagged with
// anything else, such as the ADR template's markdown, is prose.
func shellWritesIn(data string) []shellFinding {
	var findings []shellFinding
	inFence, shell := false, false
	var marker byte
	var width int
	for i, line := range strings.Split(data, "\n") {
		n := i + 1
		if !inFence {
			if m, w, info, ok := openingFence(line); ok {
				inFence, marker, width, shell = true, m, w, shellFence(info)
			}
			continue
		}
		if closesFence(line, marker, width) {
			inFence = false
			continue
		}
		if !shell {
			continue
		}
		if patterns := shellWriteIn(line); len(patterns) > 0 {
			findings = append(findings, shellFinding{line: n, text: line, patterns: patterns})
		}
	}
	return findings
}

// openingFence reports the marker and its width when line opens a fenced code
// block, with the block's info string, trimmed.
func openingFence(line string) (marker byte, width int, info string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, "", false
	}
	marker = trimmed[0]
	width = len(trimmed) - len(strings.TrimLeft(trimmed, string(marker)))
	if width < 3 {
		return 0, 0, "", false
	}
	return marker, width, strings.TrimSpace(trimmed[width:]), true
}

// shellFence reports whether a fence with this info string holds commands an
// agent runs: bash, sh, shell, console, or no tag at all.
func shellFence(info string) bool {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return true
	}
	switch strings.ToLower(fields[0]) {
	case "bash", "sh", "shell", "console":
		return true
	}
	return false
}

// closesFence reports whether line closes a fence opened with marker repeated
// width times: the same marker, at least as long, and nothing else.
func closesFence(line string, marker byte, width int) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < width || strings.Trim(trimmed, string(marker)) != "" {
		return false
	}
	return trimmed[0] == marker
}

// sorted returns a sorted copy of names.
func sorted(names []string) []string {
	out := slices.Clone(names)
	slices.Sort(out)
	return out
}

// TestShellFileWritePatterns pins the forms shellWriteIn refuses: a command
// that rewrites or removes a file through the shell (issue #251). The list is
// not complete; see the package doc for what it cannot see.
func TestShellFileWritePatterns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		line string
		want []string
	}{
		{
			name: "rm of a quoted variable",
			line: `rm -f "$tmp"`,
			want: []string{"rm of a variable"},
		},
		{
			name: "rm of a braced variable",
			line: `rm -rf "${S}"`,
			want: []string{"rm of a variable"},
		},
		{
			name: "cat into a heredoc",
			line: `cat > $S/x.sh <<'EOF'`,
			want: []string{"cat >", "a heredoc", "a redirection to a file"},
		},
		{
			name: "python reading a heredoc",
			line: `python3 - <<'EOF'`,
			want: []string{"a python - script", "a heredoc"},
		},
		{
			name: "sed in place",
			line: `sed -i s/a/b/ f`,
			want: []string{"sed -i"},
		},
		{
			name: "sed in place, long form",
			line: `sed --in-place s/a/b/ f`,
			want: []string{"sed -i"},
		},
		{
			name: "perl in place",
			line: `perl -pi -e s/a/b/ f`,
			want: []string{"perl -i"},
		},
		{
			name: "tee after a pipe",
			line: `cmd | tee f`,
			want: []string{"tee"},
		},
		{
			name: "redirection to a quoted file",
			line: `awk '{print}' f > "$ARCHIVE.sha256"`,
			want: []string{"a redirection to a file"},
		},
		{
			name: "redirection without a space",
			line: `cmd>out.txt`,
			want: []string{"a redirection to a file"},
		},
		{
			name: "redirection of both streams",
			line: `cmd &>out.txt`,
			want: []string{"a redirection to a file"},
		},
		{
			name: "redirection to a file named like /dev/null",
			line: `cmd >/dev/null.txt`,
			want: []string{"a redirection to a file"},
		},
		{
			name: "a file descriptor fed to a pipe",
			line: `cmd 2>&1 | tail`,
		},
		{
			name: "a redirection to /dev/null",
			line: `cmd >/dev/null`,
		},
		{
			name: "a redirection to a spaced /dev/null",
			line: `cmd > /dev/null 2>&1`,
		},
		{
			name: "a variable holding an image, not a path to remove",
			line: `nerdctl run --rm "$IMAGE"`,
		},
		{
			name: "sed printing a line",
			line: `sed -n 1p f`,
		},
		{
			name: "sed printing a line into grep -i",
			line: `sed -n 1p f | grep -i foo`,
		},
		{
			name: "a here-string is not a heredoc",
			line: `tr a b <<< "$x"`,
		},
		{
			name: "a placeholder is not a redirection",
			line: `gh pr view <n> --json headRefOid --jq '.reviews'`,
		},
		{
			name: "a comment naming the forms",
			line: `# a > b`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shellWriteIn(tc.line)
			if diff := cmp.Diff(sorted(tc.want), sorted(got)); diff != "" {
				t.Errorf("shellWriteIn(%q) mismatch (-want +got):\n%s", tc.line, diff)
			}
		})
	}
}

// TestShellFenceScan pins which fenced blocks hold commands: a bash, sh,
// shell or console block and an untagged one, opened with ``` or ~~~, and not
// a block tagged with anything else, such as the ADR template's markdown.
func TestShellFenceScan(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		doc  string
		want []shellFinding
	}{
		{
			name: "an untagged fence holds commands",
			doc:  "intro\n\n```\ncat > out.md\n```\n",
			want: []shellFinding{{line: 4, text: "cat > out.md", patterns: []string{"cat >", "a redirection to a file"}}},
		},
		{
			name: "an indented bash fence holds commands",
			doc:  "   ```bash\n   cat > out.md\n   ```\n",
			want: []shellFinding{{line: 2, text: "   cat > out.md", patterns: []string{"cat >", "a redirection to a file"}}},
		},
		{
			name: "a console fence holds commands",
			doc:  "~~~console\ncat > out.md\n~~~\n",
			want: []shellFinding{{line: 2, text: "cat > out.md", patterns: []string{"cat >", "a redirection to a file"}}},
		},
		{
			name: "a tilde fence opens and closes on a tilde",
			doc:  "~~~sh\ncat > out.md\n~~~\n\n```bash\nrm -f \"$tmp\"\n```\n",
			want: []shellFinding{
				{line: 2, text: "cat > out.md", patterns: []string{"cat >", "a redirection to a file"}},
				{line: 6, text: `rm -f "$tmp"`, patterns: []string{"rm of a variable"}},
			},
		},
		{
			name: "a markdown fence is prose, not commands",
			doc:  "```markdown\ncat > out.md\n```\n",
		},
		{
			name: "a markdown fence does not leak into the next block",
			doc:  "```markdown\ncat > out.md\n```\n\n```bash\nrm -f \"$tmp\"\n```\n",
			want: []shellFinding{{line: 6, text: `rm -f "$tmp"`, patterns: []string{"rm of a variable"}}},
		},
		{
			name: "a line is one finding, whatever it matches",
			doc:  "```bash\ncat > $S/x.sh <<'EOF'\nEOF\n```\n",
			want: []shellFinding{{line: 2, text: "cat > $S/x.sh <<'EOF'", patterns: []string{"cat >", "a heredoc", "a redirection to a file"}}},
		},
		{
			name: "a comment inside a fence is prose",
			doc:  "```bash\n# a > b\n```\n",
		},
		{
			name: "text outside a fence is prose",
			doc:  "cat > out.md\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shellWritesIn(tc.doc)
			for i := range got {
				got[i].patterns = sorted(got[i].patterns)
			}
			for i := range tc.want {
				tc.want[i].patterns = sorted(tc.want[i].patterns)
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(shellFinding{})); diff != "" {
				t.Errorf("shellWritesIn(%q) mismatch (-want +got):\n%s", tc.doc, diff)
			}
		})
	}
}

// TestDocumentedCommandsWriteNoFileThroughTheShell fails on a line of a shell
// fenced block in CLAUDE.md or in a skill (.claude/skills/**/*.md) that writes
// or removes a file through the shell. Fenced blocks hold the commands an
// agent copies and runs; inline code and untagged prose may still name these
// forms as the ones to avoid.
func TestDocumentedCommandsWriteNoFileThroughTheShell(t *testing.T) {
	t.Parallel()

	const skillsDir = ".claude/skills/"
	fsys := os.DirFS(repoRoot)
	files := []string{"CLAUDE.md"}
	// markdownFiles lists README.md first; only CLAUDE.md and the skills are
	// commands an agent runs.
	for _, file := range markdownFiles(t, fsys, ".claude/skills") {
		if strings.HasPrefix(file, skillsDir) {
			files = append(files, file)
		}
	}
	for _, file := range files {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for _, f := range shellWritesIn(string(data)) {
			t.Errorf("%s:%d: writes a file through the shell (%s): change files with the file-editing tool and never rm a path held in a variable (CLAUDE.md, rule 9; issue #251): %s",
				file, f.line, strings.Join(f.patterns, ", "), strings.TrimSpace(f.text))
		}
	}
}
