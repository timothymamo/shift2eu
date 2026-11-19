package htmlreport

import (
	"fmt"
	"strings"
)

// A report is built as a list of blocks (headings, paragraphs, lists and
// tables) whose text is a list of segments, each plain text or a value shown
// as code. The report's code decides what the report says; render decides how
// a block is written, from a closed set of elements whose tags and attributes
// are constants, and escapes every segment as text (textEscaper), so no value
// can become markup.

// segment is one run of text in a block: the report's own words, or a value
// from the assessment or the metadata, shown as code.
type segment struct {
	// Text is the run's text, cleaned (clean).
	Text string
	// Code shows Text as code: a value rather than the report's own words.
	Code bool
}

// rich is the text of a heading, a paragraph, a list item or a table cell.
// Its methods return a new rich and never change the one they are called on,
// so one rich, such as a target's name, can start several others.
type rich []segment

// then returns r followed by o.
func (r rich) then(o rich) rich {
	out := make(rich, 0, len(r)+len(o))

	return append(append(out, r...), o...)
}

// words returns r followed by the report's own words s.
func (r rich) words(s string) rich {
	if s == "" {
		return r.then(nil)
	}

	return r.then(rich{{Text: clean(s)}})
}

// code returns r followed by the value s, shown as code. An empty s adds
// nothing, as an empty Markdown code span renders as nothing.
func (r rich) code(s string) rich {
	if s == "" {
		return r.then(nil)
	}

	return r.then(rich{{Text: clean(s), Code: true}})
}

// join returns r followed by parts, separated by the words sep.
func (r rich) join(parts []rich, sep string) rich {
	out := r.then(nil)

	for i, p := range parts {
		if i > 0 {
			out = out.words(sep)
		}

		out = out.then(p)
	}

	return out
}

// text returns rich with the words s.
func text(s string) rich { return rich{}.words(s) }

// code returns rich with the value s.
func code(s string) rich { return rich{}.code(s) }

// item is one list item, with the items of a nested list.
type item struct {
	Text     rich
	Children []rich
}

// table is a table: one header row of column headings, and its body rows.
type table struct {
	Header []rich
	Rows   [][]rich
}

// block is one block of a report: exactly one of a heading (Heading set to
// its level, 2 to 5), a paragraph, a list or a table.
type block struct {
	// Heading is the level of a heading, h2 to h5, or 0.
	Heading int
	// Text is the text of a heading or a paragraph.
	Text rich
	// List is a list's items.
	List []item
	// Table is a table.
	Table *table
}

// fragment collects the blocks of one report.
type fragment struct {
	blocks []block
}

// heading adds a heading of level.
func (f *fragment) heading(level int, r rich) {
	f.blocks = append(f.blocks, block{Heading: level, Text: r})
}

// paragraph adds a paragraph.
func (f *fragment) paragraph(r rich) {
	f.blocks = append(f.blocks, block{Text: r})
}

// list adds a list of items; an empty list adds nothing.
func (f *fragment) list(items []item) {
	if len(items) == 0 {
		return
	}

	f.blocks = append(f.blocks, block{List: items})
}

// table adds a table.
func (f *fragment) table(header []rich, rows [][]rich) {
	f.blocks = append(f.blocks, block{Table: &table{Header: header, Rows: rows}})
}

// headingTags are the opening and closing tags of each heading level a block
// may have, h2 to h5.
var headingTags = map[int][2]string{
	2: {"<h2>", "</h2>\n"},
	3: {"<h3>", "</h3>\n"},
	4: {"<h4>", "</h4>\n"},
	5: {"<h5>", "</h5>\n"},
}

// render writes the fragment of the blocks to b: one section of semantic HTML,
// headings, paragraphs, lists and tables with header cells, every element
// and attribute a constant of this function, and every segment escaped as
// text (writeRich). A heading level outside h2 to h5 is a mistake in this
// package and an error.
func (f *fragment) render(b *strings.Builder) error {
	b.WriteString("<section class=\"shift2eu-report\">\n")

	for _, blk := range f.blocks {
		switch {
		case blk.Heading != 0:
			tags, ok := headingTags[blk.Heading]
			if !ok {
				return fmt.Errorf("heading level %d is not one of h2 to h5", blk.Heading)
			}

			b.WriteString(tags[0])
			writeRich(b, blk.Text)
			b.WriteString(tags[1])
		case blk.Table != nil:
			writeTable(b, blk.Table)
		case len(blk.List) > 0:
			writeList(b, blk.List)
		default:
			b.WriteString("<p>")
			writeRich(b, blk.Text)
			b.WriteString("</p>\n")
		}
	}

	b.WriteString("</section>\n")

	return nil
}

// writeTable writes t: its header row of column headings, then its body rows.
func writeTable(b *strings.Builder, t *table) {
	b.WriteString("<table>\n<thead>\n<tr>")

	for _, cell := range t.Header {
		b.WriteString("<th scope=\"col\">")
		writeRich(b, cell)
		b.WriteString("</th>")
	}

	b.WriteString("</tr>\n</thead>\n<tbody>\n")

	for _, row := range t.Rows {
		b.WriteString("<tr>")

		for _, cell := range row {
			b.WriteString("<td>")
			writeRich(b, cell)
			b.WriteString("</td>")
		}

		b.WriteString("</tr>\n")
	}

	b.WriteString("</tbody>\n</table>\n")
}

// writeList writes items as a list, each item's children as a nested list.
func writeList(b *strings.Builder, items []item) {
	b.WriteString("<ul>\n")

	for _, it := range items {
		b.WriteString("<li>")
		writeRich(b, it.Text)

		if len(it.Children) > 0 {
			b.WriteString("\n<ul>\n")

			for _, child := range it.Children {
				b.WriteString("<li>")
				writeRich(b, child)
				b.WriteString("</li>\n")
			}

			b.WriteString("</ul>\n")
		}

		b.WriteString("</li>\n")
	}

	b.WriteString("</ul>\n")
}

// writeRich writes the segments of r, each escaped as text, a value inside a
// code element.
func writeRich(b *strings.Builder, r rich) {
	for _, s := range r {
		if s.Code {
			b.WriteString("<code>")
			b.WriteString(textEscaper.Replace(s.Text))
			b.WriteString("</code>")

			continue
		}

		b.WriteString(textEscaper.Replace(s.Text))
	}
}

// textEscaper escapes text for HTML element content, the one context a value
// reaches in a fragment: no value is ever written into a tag, an attribute, a
// URL, a script or a style. Its replacements are html/template's for that
// context, character for character: the markup characters and both quotes
// become character references, and + too, as html/template writes it, so a
// value never closes, opens or extends an element. NUL becomes U+FFFD, as
// html/template writes it, though clean has replaced it already.
var textEscaper = strings.NewReplacer(
	"\x00", replacement,
	`"`, "&#34;",
	"&", "&amp;",
	"'", "&#39;",
	"+", "&#43;",
	"<", "&lt;",
	">", "&gt;",
)

// replacement stands in for what a fragment must not hold: invalid UTF-8 and
// control characters.
const replacement = "�"

// clean returns s as valid UTF-8 without control characters: each invalid
// byte sequence, and each C0 or C1 control character and DEL, is replaced by
// U+FFFD, except a tab or a line ending, which becomes a space, as a Markdown
// code span shows it. The domain refuses such values already; the reporter is
// the last line of defence, so the page never receives them. writeRich
// escapes the result.
func clean(s string) string {
	// strings.ToValidUTF8 returns s itself when s is valid UTF-8.
	s = strings.ToValidUTF8(s, replacement)
	if strings.IndexFunc(s, isControl) < 0 {
		return s
	}

	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case isControl(r):
			return '�'
		default:
			return r
		}
	}, s)
}

// isControl reports whether r is a C0 or C1 control character or DEL.
func isControl(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r < 0xa0)
}
