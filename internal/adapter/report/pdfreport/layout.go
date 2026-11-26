package pdfreport

import (
	"slices"
	"strings"
)

// The page geometry, in points. Text runs from the left margin over the content
// width; the header sits at the top of every page and the footer at its foot.
const (
	marginLeft   = 50
	contentWidth = 495
	headerTop    = 818
	footerBase   = 28
	ruleGap      = 6
	bodyGap      = 14
)

// The type sizes and line heights, in points.
const (
	titleSize        = 12
	sectionSize      = 13
	subsectionSize   = 11
	blockSize        = 10
	bodySize         = 9
	tableSize        = 8
	furnitureSize    = 8
	titleLeading     = 16
	sectionLeading   = 17
	subLeading       = 14
	blockLeading     = 13
	bodyLeading      = 12
	tableLeading     = 10
	furnitureLeading = 10
	cellPad          = 3
	rowGap           = 3
	bulletIndent     = 10
	keepWithHeading  = 3 * bodyLeading
)

// milli is the number of layout units in a point: widths are measured in
// thousandths of a point, so the arithmetic stays integer (ADR 0049).
const milli = 1000

// seg is one piece of a paragraph: the report's own words (prose), which wrap
// at spaces, or a value, already escaped, drawn in Courier and wrapped at the
// boundaries of its escaped form.
type seg struct {
	font  font
	size  int
	text  string
	value bool
}

// words returns the report's own words in f at size.
func words(f font, size int, text string) seg { return seg{font: f, size: size, text: text} }

// value returns a value from the inventory or a catalog, escaped, in Courier
// at size.
func value(size int, raw string) seg {
	return seg{font: courier, size: size, text: escapeValue(raw), value: true}
}

// piece is one Tj of a laid-out line: its offset from the line's start, in
// whole points, and its text.
type piece struct {
	font  font
	size  int
	x     int
	text  string
	value bool
}

// textLine is one laid-out line.
type textLine struct {
	pieces []piece
}

// lineBuilder lays out segments into lines no wider than limit thousandths of
// a point.
type lineBuilder struct {
	limit int
	lines []textLine
	width int
}

// newLineBuilder returns a builder for lines width points wide.
func newLineBuilder(width int) *lineBuilder {
	return &lineBuilder{limit: width * milli, lines: []textLine{{}}}
}

// current returns the line being filled.
func (lb *lineBuilder) current() *textLine { return &lb.lines[len(lb.lines)-1] }

// newline starts a new line.
func (lb *lineBuilder) newline() {
	lb.lines = append(lb.lines, textLine{})
	lb.width = 0
}

// start returns where a new piece starts on the current line: the width so
// far, rounded up to a whole point.
func (lb *lineBuilder) start() int {
	return (lb.width + milli - 1) / milli * milli
}

// addWords adds one word of prose, merging it into the line's last piece when
// that is prose in the same font and size.
func (lb *lineBuilder) addWords(s seg, text string) {
	w := textWidth(s.font, s.size, text)
	line := lb.current()

	if n := len(line.pieces); n > 0 {
		last := &line.pieces[n-1]
		if !last.value && last.font == s.font && last.size == s.size {
			last.text += text
			lb.width += w

			return
		}
	}

	x := lb.start()
	line.pieces = append(line.pieces, piece{font: s.font, size: s.size, x: x / milli, text: text})
	lb.width = x + w
}

// addProse lays out prose, wrapping at spaces. A word wider than a whole line
// is broken between characters.
func (lb *lineBuilder) addProse(s seg) {
	for _, word := range splitWords(s.text) {
		core := strings.TrimRight(word, " ")
		if core == "" && lb.width == 0 {
			continue
		}

		coreWidth := textWidth(s.font, s.size, core)
		if lb.width > 0 && lb.start()+coreWidth > lb.limit {
			lb.newline()

			if core == "" {
				continue
			}
		}

		if coreWidth <= lb.limit {
			lb.addWords(s, word)

			continue
		}

		for _, r := range word {
			char := string(r)
			if lb.width > 0 && lb.width+textWidth(s.font, s.size, char) > lb.limit {
				lb.newline()
			}

			lb.addWords(s, char)
		}
	}
}

// splitWords splits text into words, each holding the spaces that follow it; a
// leading run of spaces is a word of its own.
func splitWords(text string) []string {
	var out []string

	start := 0

	for i := 1; i < len(text); i++ {
		if text[i] != ' ' && text[i-1] == ' ' {
			out = append(out, text[start:i])
			start = i
		}
	}

	if start < len(text) {
		out = append(out, text[start:])
	}

	return out
}

// addValue lays out a value. A value that does not fit in what is left of the
// line starts a new one; a value wider than a line wraps between the units of
// its escaped form, never inside an escape, after its last space on the line
// when it has one, each line one piece of its own. No character is dropped or
// added at a wrap: a space stays at the end of its line. Only a unit wider
// than the whole line is broken between characters.
func (lb *lineBuilder) addValue(s seg) {
	if s.text == "" {
		return
	}

	if lb.width > 0 && lb.start()+textWidth(s.font, s.size, s.text) > lb.limit {
		lb.newline()
	}

	x := lb.start()

	var (
		units []string
		width int
	)

	flush := func(upTo int) {
		if upTo > 0 {
			line := lb.current()
			line.pieces = append(line.pieces, piece{
				font: s.font, size: s.size, x: x / milli, text: strings.Join(units[:upTo], ""), value: true,
			})
		}
	}

	place := func(unit string) {
		w := textWidth(s.font, s.size, unit)
		// The units carried after a space may still leave no room for this
		// one: they then go on a line of their own, so the loop runs at most
		// twice before the unit starts an empty line.
		for x+width+w > lb.limit && (width > 0 || x > 0) {
			upTo := len(units)
			if space := lastSpace(units); space >= 0 {
				upTo = space + 1
			}

			flush(upTo)
			lb.newline()

			units = units[upTo:]
			x, width = 0, textWidth(s.font, s.size, strings.Join(units, ""))
		}

		units = append(units, unit)
		width += w
	}

	for _, unit := range atoms(s.text) {
		if textWidth(s.font, s.size, unit) <= lb.limit {
			place(unit)

			continue
		}

		for _, r := range unit {
			place(string(r))
		}
	}

	flush(len(units))

	lb.width = x + width
}

// lastSpace returns the index of the last space among units, or -1.
func lastSpace(units []string) int {
	for i := len(units) - 1; i >= 0; i-- {
		if units[i] == " " {
			return i
		}
	}

	return -1
}

// layoutSegs lays out segments into lines width points wide.
func layoutSegs(width int, segs ...seg) []textLine {
	lb := newLineBuilder(width)

	for _, s := range segs {
		if s.value {
			lb.addValue(s)
		} else {
			lb.addProse(s)
		}
	}

	return lb.lines
}

// document lays the report out on pages. Text flows from the top of the body
// to its bottom; a line that does not fit starts a new page.
type document struct {
	pages  []*page
	y      int
	top    int
	bottom int
	// atTop is true while nothing has been drawn on the current page.
	atTop bool
	// onNewPage, when set, draws something at the top of every new page, such
	// as the header row of a table that continues there.
	onNewPage func()
	// kept holds the headings drawn since the last body line: they are kept
	// with the block that follows them, so a page break before that block
	// moves them to the new page too.
	kept keptHeadings
	// inHeading is true while a heading's own lines are drawn.
	inHeading bool
}

// keptHeadings marks the headings at the foot of the current page that no
// body line has followed yet: they start at run index run of the page, at y.
type keptHeadings struct {
	active bool
	run    int
	y      int
}

// newDocument returns a document whose body runs from top down to bottom.
func newDocument(top, bottom int) *document {
	d := &document{top: top, bottom: bottom}
	d.newPage()

	return d
}

// page returns the page being drawn.
func (d *document) page() *page { return d.pages[len(d.pages)-1] }

// newPage starts a new page.
func (d *document) newPage() {
	d.pages = append(d.pages, &page{})
	d.y = d.top
	d.atTop = true

	if d.onNewPage != nil {
		draw := d.onNewPage
		d.onNewPage = nil
		draw()
		d.onNewPage = draw
	}
}

// ensure starts a new page unless height points fit above the bottom. Headings
// that no body line has followed yet move to the new page with what follows
// them, so a heading never ends a page. heading makes sure the moved chain and
// the heading that asks leave room under them on the new page.
//
// The move assumes onNewPage is nil whenever headings are pending: a table
// sets it only after drawing its header row, a body line that ends the chain,
// and no heading is drawn inside a table, so nothing newPage draws lands
// above the moved chain.
func (d *document) ensure(height int) {
	if d.atTop || d.y-height >= d.bottom {
		return
	}

	if !d.kept.active || d.inHeading || d.kept.y >= d.top {
		d.newPage()

		return
	}

	old := d.page()
	moved := slices.Clone(old.runs[d.kept.run:])
	old.runs = old.runs[:d.kept.run]
	shift := d.top - d.kept.y
	y := d.y + shift

	d.newPage()

	for _, run := range moved {
		run.y += shift
		d.page().runs = append(d.page().runs, run)
	}

	d.kept = keptHeadings{active: true, run: 0, y: d.top}
	d.y = y
	d.atTop = false
}

// space moves down by points, unless the page is still empty.
func (d *document) space(points int) {
	if !d.atTop {
		d.y -= points
	}
}

// drawLine draws one line with its left edge at x and moves down by leading,
// on a new page when it does not fit.
func (d *document) drawLine(line textLine, x, size, leading int) {
	d.ensure(leading)
	d.drawLineHere(line, x, size)
	d.y -= leading
}

// drawLineHere draws one line at the current position without moving down.
func (d *document) drawLineHere(line textLine, x, size int) {
	baseline := d.y - size
	p := d.page()

	for _, pc := range line.pieces {
		p.runs = append(p.runs, textRun{font: pc.font, size: pc.size, x: x + pc.x, y: baseline, text: pc.text})
	}

	d.atTop = false

	if !d.inHeading {
		d.kept = keptHeadings{}
	}
}

// paragraph lays out segs at the left edge plus indent and draws them.
func (d *document) paragraph(indent, size, leading int, segs ...seg) {
	for _, line := range layoutSegs(contentWidth-indent, segs...) {
		d.drawLine(line, marginLeft+indent, size, leading)
	}
}

// body draws a paragraph in the body size.
func (d *document) body(segs ...seg) {
	d.paragraph(0, bodySize, bodyLeading, segs...)
}

// item draws a list item: a bullet, and the item's text indented after it.
func (d *document) item(indent int, segs ...seg) {
	lines := layoutSegs(contentWidth-indent-bulletIndent, segs...)

	for i, line := range lines {
		d.ensure(bodyLeading)

		if i == 0 {
			d.drawLineHere(textLine{pieces: []piece{{font: helvetica, size: bodySize, text: "•"}}},
				marginLeft+indent, bodySize)
		}

		d.drawLine(line, marginLeft+indent+bulletIndent, bodySize, bodyLeading)
	}
}

// heading draws a heading of the given size and leading, kept on one page with
// the block that follows it: a heading never ends a page. It asks for room for
// itself and keepWithHeading more; when the block that follows needs more than
// that, the page break that block's ensure makes moves the heading along
// (document.ensure).
func (d *document) heading(size, leading, before int, segs ...seg) {
	lines := layoutSegs(contentWidth, segs...)

	d.space(before)

	need := len(lines)*leading + keepWithHeading

	chain := 0
	if d.kept.active {
		chain = d.kept.y - d.y
	}

	if chain+need > d.top-d.bottom {
		// A heading too tall to fit a page with the room kept under it and
		// the headings kept above it, which only a very long value makes it,
		// would end a page whatever is done: moving the chain to a new page
		// leaves no room for it either. It is drawn at the body size as a
		// paragraph instead, where it stands, so no page ends with heading
		// lines; its first line follows the chain, and moves it along once
		// when the page has no room for that line.
		d.body(resized(bodySize, segs)...)
		d.y -= 2

		return
	}

	d.ensure(need)

	if !d.kept.active {
		d.kept = keptHeadings{active: true, run: len(d.page().runs), y: d.y}
	}

	// The ensure above left room for every line, so none of them breaks the
	// page.
	d.inHeading = true

	for _, line := range lines {
		d.drawLine(line, marginLeft, size, leading)
	}

	d.inHeading = false
	d.y -= 2
}

// resized returns segs drawn at size, in their own fonts.
func resized(size int, segs []seg) []seg {
	out := make([]seg, len(segs))
	for i, s := range segs {
		s.size = size
		out[i] = s
	}

	return out
}

// headingSize returns the type size of a heading of level 1, 2 or 3.
func headingSize(level int) int {
	switch level {
	case 1:
		return sectionSize
	case 2:
		return subsectionSize
	default:
		return blockSize
	}
}

// section draws a heading of level 1, 2 or 3 made of segs, which are sized
// with headingSize.
func (d *document) section(level int, segs ...seg) {
	switch level {
	case 1:
		d.heading(sectionSize, sectionLeading, 10, segs...)
	case 2:
		d.heading(subsectionSize, subLeading, 8, segs...)
	default:
		d.heading(blockSize, blockLeading, 8, segs...)
	}
}

// title draws a heading of the given level holding only the report's own
// words.
func (d *document) title(level int, text string) {
	d.section(level, words(helveticaBold, headingSize(level), text))
}

// cell is one table cell: its paragraphs, stacked.
type cell [][]seg

// table is a table: its column widths, its header cells and its rows.
type table struct {
	widths []int
	header []cell
	rows   [][]cell
}

// headerRow returns header cells holding the report's own words, in bold.
func headerRow(names ...string) []cell {
	cells := make([]cell, 0, len(names))
	for _, name := range names {
		cells = append(cells, cell{{words(helveticaBold, tableSize, name)}})
	}

	return cells
}

// layoutCell lays out a cell's paragraphs inside a column width points wide.
func layoutCell(c cell, width int) []textLine {
	var lines []textLine

	for _, para := range c {
		lines = append(lines, layoutSegs(width-2*cellPad, para...)...)
	}

	return lines
}

// drawTable draws t. Its header row is drawn first and again at the top of
// every page the table continues on. A row stays on one page unless it is
// taller than a page, which only a very long value makes it; such a row is
// split between two lines.
func (d *document) drawTable(t table) {
	header := make([][]textLine, len(t.header))
	for i, h := range t.header {
		header[i] = layoutCell(h, t.widths[i])
	}

	rows := make([][][]textLine, len(t.rows))
	for r, row := range t.rows {
		rows[r] = make([][]textLine, len(row))
		for i, c := range row {
			rows[r][i] = layoutCell(c, t.widths[i])
		}
	}

	headerHeight := tallest(header)*tableLeading + rowGap

	tableWidth := 0
	for _, w := range t.widths {
		tableWidth += w
	}

	drawHeader := func() {
		d.drawRowLines(t.widths, header, 0, tallest(header))
		d.page().rules = append(d.page().rules, rule{x: marginLeft, y: d.y - 1, w: tableWidth})
		d.y -= rowGap
	}

	first := headerHeight
	if len(rows) > 0 {
		first += min(tallest(rows[0]), 3) * tableLeading
	}

	d.ensure(first)
	drawHeader()

	d.onNewPage = drawHeader

	for _, row := range rows {
		n := tallest(row)
		height := n * tableLeading

		if d.y-height < d.bottom && height <= d.top-d.bottom-headerHeight {
			d.newPage()
		}

		for i := 0; i < n; {
			if d.y-tableLeading < d.bottom {
				d.newPage()
			}

			fit := max(1, (d.y-d.bottom)/tableLeading)
			end := min(n, i+fit)
			d.drawRowLines(t.widths, row, i, end)
			i = end
		}

		d.y -= rowGap
	}

	d.onNewPage = nil
}

// tallest returns the number of lines of a row's tallest cell.
func tallest(row [][]textLine) int {
	n := 0
	for _, c := range row {
		n = max(n, len(c))
	}

	return n
}

// drawRowLines draws the lines from to end of every cell of a row, line by
// line across the cells, so that each line's runs are drawn together.
func (d *document) drawRowLines(widths []int, row [][]textLine, from, end int) {
	for i := from; i < end; i++ {
		x := marginLeft

		for c, lines := range row {
			if i < len(lines) {
				d.drawLineHere(lines[i], x+cellPad, tableSize)
			}

			x += widths[c]
		}

		d.y -= tableLeading
	}
}
