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

// class is the class attribute of a cell, a row or a list item: one of a
// closed set of constants (classNames), chosen from the fit enum or the
// element's role, never from a value. The zero class writes no attribute.
type class int

// The classes a cell, a row or a list item may carry.
const (
	classNone class = iota
	// classFitDirect, classFitPartial and classFitNone are a Fit cell's
	// badge; classFit is the badge of a fit the report does not know, which
	// names no fit.
	classFitDirect
	classFitPartial
	classFitNone
	classFit
	// classRowDirect, classRowPartial, classRowNone and classRowTotal are the
	// rows of a Summary table.
	classRowDirect
	classRowPartial
	classRowNone
	classRowTotal
	// classTotal is the Cost estimate's total item.
	classTotal
)

// classNames are the attribute values of the classes, a fixed table.
var classNames = map[class]string{
	classFitDirect:  "fit fit-direct",
	classFitPartial: "fit fit-partial",
	classFitNone:    "fit fit-none",
	classFit:        "fit",
	classRowDirect:  "fit-direct",
	classRowPartial: "fit-partial",
	classRowNone:    "fit-none",
	classRowTotal:   "fit-total",
	classTotal:      "total",
}

// part is the kind of a report section a part section wraps: one of a closed
// set of constants (partNames).
type part int

// The kinds of part.
const (
	partSummary part = iota + 1
	partResources
	partChanges
	partBlockers
	partWorkloads
	partCost
	partComparison
	partTarget
)

// partNames are the class attribute values of the parts, a fixed table.
var partNames = map[part]string{
	partSummary:    "part part-summary",
	partResources:  "part part-resources",
	partChanges:    "part part-changes",
	partBlockers:   "part part-blockers",
	partWorkloads:  "part part-workloads",
	partCost:       "part part-cost",
	partComparison: "part part-comparison",
	partTarget:     "part part-target",
}

// listKind is the kind of a list: one of a closed set of constants
// (listStartTags), chosen from the section the list belongs to, never from a
// value. The zero kind is a list that keeps its markers.
type listKind int

// The kinds of list.
const (
	listPlain listKind = iota
	// listMarkerless is a list the site's stylesheet strips of its markers
	// (the title list, the Blockers and the Cost estimate lists): it states
	// role="list",
	// because Safari and VoiceOver drop the list role and the item count of a
	// list drawn without markers (issue #373).
	listMarkerless
)

// listStartTags are the start tags of the kinds of list, a fixed table.
var listStartTags = map[listKind]string{
	listPlain:      "<ul>\n",
	listMarkerless: "<ul role=\"list\">\n",
}

// item is one list item, with the items of a nested list.
type item struct {
	Text     rich
	Children []rich
	// Class is the item's class.
	Class class
}

// cell is one body cell of a table.
type cell struct {
	Text  rich
	Class class
}

// row is one body row of a table.
type row struct {
	Cells []cell
	Class class
}

// cells returns a cell without a class for each of rs.
func cells(rs ...rich) []cell {
	out := make([]cell, 0, len(rs))
	for _, r := range rs {
		out = append(out, cell{Text: r})
	}

	return out
}

// table is a table: one header row of column headings, and its body rows.
type table struct {
	Header []rich
	Rows   []row
}

// block is one block of a report: exactly one of a heading (Heading set to
// its level, 2 to 5), a paragraph, a list, a table, the start of a part
// section (Open set to its kind) or the end of one (Close).
type block struct {
	// Heading is the level of a heading, h2 to h5, or 0.
	Heading int
	// Text is the text of a heading or a paragraph.
	Text rich
	// List is a list's items.
	List []item
	// ListKind is the kind of the list.
	ListKind listKind
	// Table is a table.
	Table *table
	// Open starts a part section of this kind.
	Open part
	// Close ends the part section opened last.
	Close bool
}

// fragment collects the blocks of one report.
type fragment struct {
	blocks []block
}

// part writes the blocks body adds inside a part section of kind.
func (f *fragment) part(kind part, body func()) {
	f.blocks = append(f.blocks, block{Open: kind})
	body()
	f.blocks = append(f.blocks, block{Close: true})
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

// markerlessList adds a list the site's stylesheet strips of its markers,
// which states role="list" (listMarkerless). An empty list adds nothing, as
// list does, so the two differ only by the attribute. No caller passes an
// empty list: a title list always holds the Source and Catalog items
// (writeHeader, writeComparisonHeader), the Blockers list is written only
// when there is a blocker, one item each (writeBlockers, "None." otherwise),
// and the Cost estimate list always holds its four fixed items
// (writeCostEstimate).
func (f *fragment) markerlessList(items []item) {
	if len(items) == 0 {
		return
	}

	f.blocks = append(f.blocks, block{List: items, ListKind: listMarkerless})
}

// table adds a table.
func (f *fragment) table(header []rich, rows []row) {
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
// part sections holding headings, paragraphs, lists and tables with header
// cells, every element and attribute a constant of this package (a class
// from classNames or partNames), and every segment escaped as text
// (writeRich). A heading level outside h2 to h5, a part or a class outside
// those tables, or part sections that do not nest are mistakes in this
// package and an error.
func (f *fragment) render(b *strings.Builder) error {
	b.WriteString("<section class=\"shift2eu-report\">\n")

	open := 0

	for _, blk := range f.blocks {
		switch {
		case blk.Open != 0:
			name, ok := partNames[blk.Open]
			if !ok {
				return fmt.Errorf("part %d is not one of the report's parts", blk.Open)
			}

			b.WriteString("<section class=\"" + name + "\">\n")

			open++
		case blk.Close:
			if open == 0 {
				return fmt.Errorf("a part section is closed that was never opened")
			}

			b.WriteString("</section>\n")

			open--
		case blk.Heading != 0:
			tags, ok := headingTags[blk.Heading]
			if !ok {
				return fmt.Errorf("heading level %d is not one of h2 to h5", blk.Heading)
			}

			b.WriteString(tags[0])
			writeRich(b, blk.Text)
			b.WriteString(tags[1])
		case blk.Table != nil:
			if err := writeTable(b, blk.Table); err != nil {
				return err
			}
		case len(blk.List) > 0:
			if err := writeList(b, blk.ListKind, blk.List); err != nil {
				return err
			}
		default:
			b.WriteString("<p>")
			writeRich(b, blk.Text)
			b.WriteString("</p>\n")
		}
	}

	if open != 0 {
		return fmt.Errorf("%d part sections are left open", open)
	}

	b.WriteString("</section>\n")

	return nil
}

// writeStart writes the start tag of the element tag with the class c, which
// is a constant of classNames, or none for classNone.
func writeStart(b *strings.Builder, tag string, c class) error {
	if c == classNone {
		b.WriteString("<" + tag + ">")

		return nil
	}

	name, ok := classNames[c]
	if !ok {
		return fmt.Errorf("class %d is not one of the report's classes", c)
	}

	b.WriteString("<" + tag + " class=\"" + name + "\">")

	return nil
}

// writeTable writes t: its header row of column headings, then its body rows.
func writeTable(b *strings.Builder, t *table) error {
	b.WriteString("<table>\n<thead>\n<tr>")

	for _, cell := range t.Header {
		b.WriteString("<th scope=\"col\">")
		writeRich(b, cell)
		b.WriteString("</th>")
	}

	b.WriteString("</tr>\n</thead>\n<tbody>\n")

	for _, r := range t.Rows {
		if err := writeStart(b, "tr", r.Class); err != nil {
			return err
		}

		for _, c := range r.Cells {
			if err := writeStart(b, "td", c.Class); err != nil {
				return err
			}

			writeRich(b, c.Text)
			b.WriteString("</td>")
		}

		b.WriteString("</tr>\n")
	}

	b.WriteString("</tbody>\n</table>\n")

	return nil
}

// writeList writes items as a list of kind, whose start tag is a constant of
// listStartTags, each item's children as a nested list that keeps its markers.
func writeList(b *strings.Builder, kind listKind, items []item) error {
	start, ok := listStartTags[kind]
	if !ok {
		return fmt.Errorf("list kind %d is not one of the report's kinds of list", kind)
	}

	b.WriteString(start)

	for _, it := range items {
		if err := writeStart(b, "li", it.Class); err != nil {
			return err
		}

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

	return nil
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
