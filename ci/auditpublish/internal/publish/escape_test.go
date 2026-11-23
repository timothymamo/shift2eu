package publish_test

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// TestCodeSpan pins ADR 0040's inline escaping: CR LF and a lone CR become LF,
// every LF and TAB becomes one space, the delimiter is one backtick longer than
// the longest run of backticks in the text, and the result is the delimiter,
// one space, the text, one space and the delimiter. An empty text is the plain
// word none.
func TestCodeSpan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty text is the word none", "", "none"},
		{"plain text", "a repository path", "` a repository path `"},
		{"one backtick in the text needs two", "a`b", "`` a`b ``"},
		{"a backtick run in the text needs one more", "a``b", "``` a``b ```"},
		{"a text starting with a backtick stays intact", "`x", "`` `x ``"},
		{"a text ending with a backtick stays intact", "x`", "`` x` ``"},
		{"a text of backticks only", "``", "``` `` ```"},
		{"LF becomes one space", "a\nb", "` a b `"},
		{"TAB becomes one space", "a\tb", "` a b `"},
		{"CR LF becomes LF, then one space", "a\r\nb", "` a b `"},
		{"a lone CR becomes LF, then one space", "a\rb", "` a b `"},
		{"two LFs become two spaces", "a\n\nb", "` a  b `"},
		{"LF and TAB mix", "a\n\tb", "` a  b `"},
		{
			name: "HTML is inert inside the span",
			in:   `<img src=x onerror="alert(1)">`,
			want: "` <img src=x onerror=\"alert(1)\"> `",
		},
		{
			name: "an image is inert inside the span",
			in:   "![x](https://example.com/pixel.png)",
			want: "` ![x](https://example.com/pixel.png) `",
		},
		{
			name: "a link is inert inside the span",
			in:   "[click](https://example.com/target)",
			want: "` [click](https://example.com/target) `",
		},
		{"a mention is inert inside the span", "@octocat see this", "` @octocat see this `"},
		{"a reference is inert inside the span", "fixed in #123", "` fixed in #123 `"},
		{
			name: "a bare URL is inert inside the span",
			in:   "https://example.com/path?q=1&r=2",
			want: "` https://example.com/path?q=1&r=2 `",
		},
		{"emphasis is inert inside the span", "*not bold* _not italic_", "` *not bold* _not italic_ `"},
		{"an entity is inert inside the span", "&amp; &lt;", "` &amp; &lt; `"},
		{"an escaped pipe is untouched", `a\|b`, "` a\\|b `"},
		{"non-ASCII text is kept", "café ☕", "` café ☕ `"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := publish.CodeSpan(tc.in); got != tc.want {
				t.Errorf("CodeSpan(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTableCell pins ADR 0040's table escaping: a code span with every | written
// \| and no other byte changed.
func TestTableCell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty text is the word none", "", "none"},
		{"a pipe is escaped", "a|b", "` a\\|b `"},
		{"every pipe is escaped", "|a||b|", "` \\|a\\|\\|b\\| `"},
		{"a pipe inside a backtick run", "a`|`b", "`` a`\\|`b ``"},
		{"no pipe, no change", "a\\b", "` a\\b `"},
		{"a CR LF is a space, and a pipe escapes", "a\r\nb|c", "` a b\\|c `"},
		{"an HTML table is inert and its pipes escaped", "<table><tr><td>x", "` <table><tr><td>x `"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := publish.TableCell(tc.in); got != tc.want {
				t.Errorf("TableCell(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestCodeBlock pins ADR 0040's block escaping: CR LF and a lone CR become LF,
// the fence is at least three backticks and one longer than the longest run of
// backticks in the text, with the info string text, and the content ends with
// one LF before the closing fence.
func TestCodeBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty text", "", "```text\n\n```"},
		{"one line", "a", "```text\na\n```"},
		{"a text already ending in LF gets no second one", "a\n", "```text\na\n```"},
		{"two lines", "a\nb", "```text\na\nb\n```"},
		{"CR LF becomes LF", "a\r\nb", "```text\na\nb\n```"},
		{"a lone CR becomes LF", "a\rb", "```text\na\nb\n```"},
		{"a TAB is kept", "a\tb", "```text\na\tb\n```"},
		{"a fence line in the text opens a longer fence", "a\n```\nb", "````text\na\n```\nb\n````"},
		{"four backticks in the text need five", "````", "`````text\n````\n`````"},
		{"two backticks need three", "``", "```text\n``\n```"},
		{
			name: "HTML is inert in the block",
			in:   "<img src=x onerror=alert(1)>",
			want: "```text\n<img src=x onerror=alert(1)>\n```",
		},
		{
			name: "a link and a mention are inert in the block",
			in:   "[x](https://example.com) @octocat #1",
			want: "```text\n[x](https://example.com) @octocat #1\n```",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := publish.CodeBlock(tc.in); got != tc.want {
				t.Errorf("CodeBlock(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestCodeBlockCannotBeClosedByItsText pins the property the fence length is
// for: no line of a model-written text closes the block, however many backticks
// it holds, and the block is one a Markdown renderer reads back as that text.
func TestCodeBlockCannotBeClosedByItsText(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"```",
		"````",
		"```sh\nrm -rf /\n```",
		"a\n```text\nb\n```\nc",
	} {
		block := publish.CodeBlock(text)
		lines := strings.Split(block, "\n")

		fence := lines[0]
		if !strings.HasSuffix(fence, "text") || len(fence) < len("```text") {
			t.Fatalf("CodeBlock(%q) opens with %q, want a fence of at least three backticks with the info string text", text, fence)
		}

		closing := fence[:len(fence)-len("text")]
		if lines[len(lines)-1] != closing {
			t.Errorf("CodeBlock(%q) closes with %q, want the opening fence %q", text, lines[len(lines)-1], closing)
		}

		for _, line := range lines[1 : len(lines)-1] {
			if strings.HasPrefix(strings.TrimSpace(line), closing) && strings.Trim(strings.TrimSpace(line), "`") == "" {
				t.Errorf("CodeBlock(%q) holds the line %q, which closes the block", text, line)
			}
		}

		if !strings.HasPrefix(fence, strings.Repeat("`", 3)) || len(fence)-len("text") <= longestBacktickRun(text) {
			t.Errorf("CodeBlock(%q) opens with %q, want a fence longer than the text's longest run of %d backticks",
				text, fence, longestBacktickRun(text))
		}
	}
}

// longestBacktickRun returns the length of the longest run of backticks in s.
func longestBacktickRun(s string) int {
	longest, run := 0, 0

	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			run++

			if run > longest {
				longest = run
			}

			continue
		}

		run = 0
	}

	return longest
}
