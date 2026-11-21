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
// from the assessment or the metadata, shown as code, and the few inline
// elements the redesign's boxes need (issue #373): a fit badge, an emphasised
// figure, a plain span, a separator hidden from screen readers and the
// catalog's link. Every element and attribute a kind writes is a constant;
// Text is always element content, and only a link writes a value, the
// catalog's URL, into an attribute (writeRich).
type segment struct {
	// Text is the run's text, cleaned (clean).
	Text string
	// Code shows Text as code: a value rather than the report's own words.
	// It takes precedence over Kind.
	Code bool
	// Kind is how any other run is written; the zero kind is the report's
	// words.
	Kind segmentKind
	// Class is the class of a badge or a figure: a constant of classNames.
	Class class
	// URL is a link's address, cleaned (clean).
	URL string
}

// segmentKind is how a segment is written: one of a closed set of constants.
type segmentKind int

// The kinds of segment.
const (
	// segWords is the report's own words, as text.
	segWords segmentKind = iota
	// segBadge is a fit badge: a span of Class holding the fit's glyph,
	// hidden from screen readers, and Text.
	segBadge
	// segFigure is a figure the report emphasises: a strong element, of
	// Class when it has one.
	segFigure
	// segSpan is Text inside a span without attributes, which the
	// stylesheet sets apart by its position.
	segSpan
	// segSeparator is Text inside a span hidden from screen readers: a
	// separator a reader sees between list items and a screen reader does
	// not read out.
	segSeparator
	// segLink is Text linking URL, opened in a new tab with no handle on
	// the report's page: the catalog's own page (issue #373, item 10-1).
	segLink
)

// glyphs are the glyphs of the badge classes, which a badge shows before its
// text and hides from screen readers: the fit's word or count is what a
// screen reader reads. The badge of a fit the report does not know shows none.
var glyphs = map[class]string{
	classFitDirect:  "✓",
	classFitPartial: "◐",
	classFitNone:    "✕",
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

	return r.then(rich{{Text: clean(s), Kind: segWords}})
}

// code returns r followed by the value s, shown as code. An empty s adds
// nothing, as an empty Markdown code span renders as nothing.
func (r rich) code(s string) rich {
	if s == "" {
		return r.then(nil)
	}

	return r.then(rich{{Text: clean(s), Code: true}})
}

// badge returns r followed by the badge of class c reading s.
func (r rich) badge(c class, s string) rich {
	return r.then(rich{{Text: clean(s), Kind: segBadge, Class: c}})
}

// figure returns r followed by the figure s, of class c (classNone for
// none).
func (r rich) figure(c class, s string) rich {
	return r.then(rich{{Text: clean(s), Kind: segFigure, Class: c}})
}

// span returns r followed by the words s inside a span.
func (r rich) span(s string) rich {
	return r.then(rich{{Text: clean(s), Kind: segSpan}})
}

// separator returns r followed by the separator s, hidden from screen
// readers.
func (r rich) separator(s string) rich {
	return r.then(rich{{Text: clean(s), Kind: segSeparator}})
}

// link returns r followed by the words s linking url, when url is an
// absolute https URL, and by the words alone otherwise: the domain refuses
// any other catalog URL already, and the reporter is the last line of
// defence, so no other scheme (javascript:, data:) ever reaches an href.
func (r rich) link(s, url string) rich {
	url = clean(url)
	if !strings.HasPrefix(url, "https://") {
		return r.words(s)
	}

	return r.then(rich{{Text: clean(s), Kind: segLink, URL: url}})
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
	// classTotal is the Cost estimate's total.
	classTotal
	// classTableScroll is the box a table sits in, which scrolls sideways
	// on its own when the table is wider than the page (issue #373, item
	// 11).
	classTableScroll
)

// classNames are the attribute values of the classes, a fixed table.
var classNames = map[class]string{
	classFitDirect:   "fit fit-direct",
	classFitPartial:  "fit fit-partial",
	classFitNone:     "fit fit-none",
	classFit:         "fit",
	classRowDirect:   "fit-direct",
	classRowPartial:  "fit-partial",
	classRowNone:     "fit-none",
	classRowTotal:    "fit-total",
	classTotal:       "total",
	classTableScroll: "table-scroll",
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
	// (the header line and the Blockers lists): it states role="list",
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
	// Box starts a section without a class: a box the stylesheet lays out by
	// its place, such as a workload's box or the box that holds the required
	// changes and the blockers side by side (issue #373).
	Box bool
	// Close ends the part section or the box opened last.
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

// box writes the blocks body adds inside a box: a section without a class.
func (f *fragment) box(body func()) {
	f.blocks = append(f.blocks, block{Box: true})
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
// empty list: a header line always holds the source and catalog items
// (writeHeader, writeComparisonHeader), and the Blockers list is written only
// when there is a blocker, one item each (writeBlockers, "None." otherwise).
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
		case blk.Box:
			b.WriteString("<section>\n")

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

// writeTable writes t: its header row of column headings, then its body rows,
// inside a box of class table-scroll, which scrolls sideways on its own when
// the table is wider than the page, so the page itself never does (issue
// #373, item 11).
func writeTable(b *strings.Builder, t *table) error {
	b.WriteString("<section class=\"" + classNames[classTableScroll] + "\">\n<table>\n<thead>\n<tr>")

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

	b.WriteString("</tbody>\n</table>\n</section>\n")

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

// writeRich writes the segments of r, each escaped as text: a value inside a
// code element, and each other kind inside its constant element. A badge's
// or a figure's class outside classNames is a mistake in this package and
// writes no class. A link's URL is the one value written into an attribute,
// escaped by textEscaper, which escapes both quotes, so it never ends the
// attribute; link has already refused any URL that is no https URL.
func writeRich(b *strings.Builder, r rich) {
	for _, s := range r {
		escaped := textEscaper.Replace(s.Text)

		if s.Code {
			b.WriteString("<code>" + escaped + "</code>")

			continue
		}

		switch s.Kind {
		case segBadge:
			b.WriteString("<span" + classAttribute(s.Class) + ">")

			if glyph, ok := glyphs[s.Class]; ok {
				b.WriteString("<span aria-hidden=\"true\">" + glyph + "</span> ")
			}

			b.WriteString(escaped + "</span>")
		case segFigure:
			b.WriteString("<strong" + classAttribute(s.Class) + ">" + escaped + "</strong>")
		case segSpan:
			b.WriteString("<span>" + escaped + "</span>")
		case segSeparator:
			b.WriteString("<span aria-hidden=\"true\">" + escaped + "</span>")
		case segLink:
			b.WriteString("<a href=\"" + textEscaper.Replace(s.URL) + "\" target=\"_blank\" rel=\"noopener noreferrer\">" +
				escaped + "</a>")
		default:
			b.WriteString(escaped)
		}
	}
}

// classAttribute returns the class attribute of c, a constant of classNames,
// with its leading space, or nothing for classNone or a class outside the
// table.
func classAttribute(c class) string {
	name, ok := classNames[c]
	if c == classNone || !ok {
		return ""
	}

	return " class=\"" + name + "\""
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
