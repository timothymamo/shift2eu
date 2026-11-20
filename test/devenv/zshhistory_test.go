// Pins issue #335: the maintainer pastes the shell blocks of docs/dev-environment.md into an interactive
// zsh on the Mac, and interactive zsh expands a history event at a `!` that is followed by anything but a
// blank, `=`, `(`, a backslash-quoted one or the end of the line. The stop blocks' `case "$pid" in` line
// with the `[!0-9]` pattern broke there with "event not found" before a single command ran, so no fenced shell
// block of the page may hold a `!` zsh expands: the `if` test writes `[ ! -e "$l" ]` with a blank, which zsh
// leaves alone.
package devenv_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// devEnvironmentPage is the page whose fenced shell blocks the maintainer pastes into a terminal.
const devEnvironmentPage = "docs/dev-environment.md"

// zshExpansion is a line of a fenced shell block that interactive zsh would expand history in, so that the
// block breaks before it runs anything.
type zshExpansion struct {
	line int
	text string
}

// zshHistoryExpansionsIn returns a finding for every line of data that sits inside a fenced code block
// tagged bash, sh, shell or console, or not tagged at all, and holds a `!` interactive zsh would expand.
// zsh expands a `!` followed by anything but a blank, `=`, `(` or the end of the line (zshmisc(1), "BANG
// HISTORY"), inside double quotes as much as outside them, while a `!` inside single quotes, one a backslash
// quotes, and one on a comment line is no event. A block tagged with anything else, such as a markdown one,
// is prose.
func zshHistoryExpansionsIn(data string) []zshExpansion {
	var found []zshExpansion
	inFence, shell := false, false
	var marker byte
	var width int
	for i, line := range strings.Split(data, "\n") {
		if !inFence {
			if m, w, info, ok := zshFenceOpen(line); ok {
				inFence, marker, width, shell = true, m, w, zshShellBlock(info)
			}
			continue
		}
		if zshFenceClose(line, marker, width) {
			inFence = false
			continue
		}
		if shell && zshExpandsHistory(line) {
			found = append(found, zshExpansion{line: i + 1, text: strings.TrimSpace(line)})
		}
	}
	return found
}

// zshExpandsHistory reports whether interactive zsh would expand a history event in line.
func zshExpandsHistory(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return false
	}
	inSingle := false
	for i := 0; i < len(trimmed); i++ {
		switch trimmed[i] {
		case '\\':
			i++ // a backslash quotes the byte after it, so `\!` is no event
		case '\'':
			inSingle = !inSingle
		case '!':
			if !inSingle && zshExpandsBang(trimmed[i+1:]) {
				return true
			}
		}
	}
	return false
}

// zshExpandsBang reports whether what follows a `!` starts a history event: zsh leaves a `!` alone when a
// blank, `=` or `(` follows it or it ends the line.
//
// It is stricter than zsh on purpose (issue #396, item 6): a `!` right before the `"` that closes a double-quoted
// string ("hi!") is no event in zsh, and this reports it anyway. Telling that case apart would mean tracking
// double quotes the way zsh's lexer does; reporting it only ever refuses a line that was safe, which the page
// rewrites at no cost (`!` before a blank, or in single quotes), while a mistake the other way breaks a paste.
func zshExpandsBang(rest string) bool {
	if rest == "" {
		return false
	}
	switch rest[0] {
	case ' ', '\t', '\r', '=', '(':
		return false
	}
	return true
}

// zshFenceOpen reports the marker and its width when line opens a fenced code block, with the block's info
// string, trimmed.
func zshFenceOpen(line string) (marker byte, width int, info string, ok bool) {
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

// zshShellBlock reports whether a fence with this info string holds commands the maintainer pastes: bash,
// sh, shell, console, or no tag at all.
func zshShellBlock(info string) bool {
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

// zshFenceClose reports whether line closes a fence opened with marker repeated width times: the same
// marker, at least as long, and nothing else.
func zshFenceClose(line string, marker byte, width int) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < width || strings.Trim(trimmed, string(marker)) != "" {
		return false
	}
	return trimmed[0] == marker
}

// TestTheDevEnvironmentBlocksHoldNoZshHistoryExpansion pins what interactive zsh expands (issue #335): a
// `!` followed by anything but a blank, `=`, `(`, a quoting backslash or the end of the line, unless single
// quotes hold it, and the page's fenced shell blocks, which the maintainer pastes, hold none of them.
func TestTheDevEnvironmentBlocksHoldNoZshHistoryExpansion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		doc  string
		want []zshExpansion
	}{
		{
			name: "a character class in a case pattern expands",
			doc:  "```bash\nelif case \"$pid\" in ''|0|*[!0-9]*) true ;; *) false ;; esac\n```\n",
			want: []zshExpansion{{line: 2, text: `elif case "$pid" in ''|0|*[!0-9]*) true ;; *) false ;; esac`}},
		},
		{
			name: "double quotes expand as much as no quote at all",
			doc:  "```sh\necho \"[!0-9]\"\n```\n",
			want: []zshExpansion{{line: 2, text: `echo "[!0-9]"`}},
		},
		{
			name: "an untagged fence holds commands too",
			doc:  "```\necho hi!there\n```\n",
			want: []zshExpansion{{line: 2, text: "echo hi!there"}},
		},
		{
			name: "a bang a blank follows is no event",
			doc:  "```bash\nif [ ! -e \"$l\" ]; then echo no lock\nfi\n```\n",
		},
		{
			name: "a bang = follows is no event",
			doc:  "```bash\necho !=x\n```\n",
		},
		{
			name: "a bang ( follows is no event",
			doc:  "```bash\necho !(x)\n```\n",
		},
		{
			name: "a bang that ends the line is no event",
			doc:  "```bash\necho hi !\n```\n",
		},
		{
			name: "a bang single quotes hold is no event",
			doc:  "```bash\necho '[!0-9]'\n```\n",
		},
		{
			name: "a bang a backslash quotes is no event",
			doc:  "```bash\necho \\!here\n```\n",
		},
		{
			name: "a comment line is prose",
			doc:  "```bash\n# PASS: nothing runs, so !here is no event either\n```\n",
		},
		{
			name: "a bang outside a fence is prose",
			doc:  "Paste echo hi!there at the prompt.\n",
		},
		{
			name: "a markdown fence is prose",
			doc:  "```markdown\necho hi!there\n```\n",
		},
		{
			name: "a markdown fence does not leak into the next block",
			doc:  "```markdown\necho hi!there\n```\n\n```bash\nif [ ! -e \"$l\" ]; then echo no lock\nfi\n```\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, zshHistoryExpansionsIn(tc.doc), cmp.AllowUnexported(zshExpansion{})); diff != "" {
				t.Errorf("zshHistoryExpansionsIn(%q) mismatch (-want +got):\n%s", tc.doc, diff)
			}
		})
	}

	t.Run(devEnvironmentPage, func(t *testing.T) {
		t.Parallel()

		for _, f := range zshHistoryExpansionsIn(readRepoFile(t, devEnvironmentPage)) {
			t.Errorf("%s:%d: interactive zsh expands history at this `!` and stops the block with \"event not found\" before it runs anything: write the `!` so a blank, `=` or `(` follows it, or quote it in single quotes (issue #335): %s",
				devEnvironmentPage, f.line, f.text)
		}
	})
}
