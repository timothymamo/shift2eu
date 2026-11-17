package docs_test

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// shellFileWrites are the ways a command writes or removes a file through the
// shell that CLAUDE.md rule 9 forbids agents (issue #251): unattended panes
// stop at a permission prompt nobody answers for them. Agents change files
// with their file-editing tool instead.
var shellFileWrites = []struct {
	name string
	re   *regexp.Regexp
}{
	{"a heredoc", regexp.MustCompile(`<<`)},
	{"cat >", regexp.MustCompile(`\bcat\s*>`)},
	{"tee", regexp.MustCompile(`\btee\b`)},
	{"sed -i", regexp.MustCompile(`\bsed\b.*\s-[a-zA-Z]*i`)},
	{"a python - script", regexp.MustCompile(`\bpython3?\s+-(\s|$)`)},
	{"rm of a variable", regexp.MustCompile(`(^|[\s;&|(])rm\s[^|;&]*\$`)},
	{"a redirection to a file", regexp.MustCompile(`(^|\s)\d?>>?\s*[^\s&]`)},
}

// TestSkillCommandsWriteNoFileThroughTheShell fails on a line of a fenced
// block in a skill (.claude/skills/**/*.md) that writes a file through the
// shell or removes a path held in a variable. Fenced blocks hold the commands
// an agent copies and runs; inline code may still name these forms as the
// ones to avoid. A redirection to /dev/null is allowed.
func TestSkillCommandsWriteNoFileThroughTheShell(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)
	// markdownFiles lists README.md first; only the skills are checked.
	for _, file := range markdownFiles(t, fsys, ".claude/skills")[1:] {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		inFence := false
		sc := bufio.NewScanner(bytes.NewReader(data))
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if !inFence {
				continue
			}
			checked := strings.ReplaceAll(line, ">/dev/null", "")
			checked = strings.ReplaceAll(checked, "> /dev/null", "")
			for _, w := range shellFileWrites {
				if w.re.MatchString(checked) {
					t.Errorf("%s:%d: %s in a command block: change files with the file-editing tool and never rm a path held in a variable (CLAUDE.md, rule 9; issue #251): %s",
						file, n, w.name, strings.TrimSpace(line))
				}
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("scanning %s: %v", file, err)
		}
	}
}
