package publish

import "strings"

// Model text reaches Markdown only through these three functions, inside a
// code span or a fenced code block, where GitHub renders no HTML, image, link,
// autolink, mention, reference, entity or emphasis (ADR 0040, "Escaping").

// CodeSpan returns s as an inline code span: CR LF and a lone CR become LF,
// every LF and TAB one space, and the delimiter is a run of backticks one
// longer than the longest run in the text, with one space inside each end.
// An empty text is the word none, not a code span.
func CodeSpan(s string) string {
	if s == "" {
		return "none"
	}

	s = normalizeNewlines(s)
	s = strings.NewReplacer("\n", " ", "\t", " ").Replace(s)

	delimiter := strings.Repeat("`", longestBackticks(s)+1)

	return delimiter + " " + s + " " + delimiter
}

// TableCell returns CodeSpan(s) with every | written \|, which GFM's table
// parser turns back into | inside a cell, code spans included.
func TableCell(s string) string {
	return strings.ReplaceAll(CodeSpan(s), "|", `\|`)
}

// CodeBlock returns s as a fenced code block with the info string text: CR LF
// and a lone CR become LF, the fence is at least three backticks and one
// longer than the longest run in the text, and the text ends with one LF
// before the closing fence, so no line of it can close the block.
func CodeBlock(s string) string {
	s = normalizeNewlines(s)

	fence := strings.Repeat("`", max(3, longestBackticks(s)+1))

	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}

	return fence + "text\n" + s + fence
}

// normalizeNewlines turns CR LF and a lone CR into LF.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// longestBackticks returns the length of the longest run of backticks in s.
func longestBackticks(s string) int {
	longest, run := 0, 0

	for i := 0; i < len(s); i++ {
		if s[i] != '`' {
			run = 0

			continue
		}

		run++
		longest = max(longest, run)
	}

	return longest
}
