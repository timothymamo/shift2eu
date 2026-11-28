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
// at spaces, a value, already escaped, drawn in Courier and wrapped at the
// boundaries of its escaped form, or a fit badge, which never wraps. colour,
// when set, overrides the document's ink for the segment.
type seg struct {
	font   font
	size   int
	text   string
	value  bool
	colour colour
	badge  *fitStyle
}

// words returns the report's own words in f at size.
func words(f font, size int, text string) seg { return seg{font: f, size: size, text: text} }

// value returns a value from the inventory or a catalog, escaped, in Courier
// at size.
func value(size int, raw string) seg {
	return seg{font: courier, size: size, text: escapeValue(raw), value: true}
}

// piece is one Tj of a laid-out line: its offset from the line's start, in
// whole points, and its text; or, for a badge, the badge drawn there.
type piece struct {
	font   font
	size   int
	x      int
	text   string
	value  bool
	colour colour
	badge  *fitStyle
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
		if !last.value && last.badge == nil && last.font == s.font && last.size == s.size && last.colour == s.colour {
			last.text += text
			lb.width += w

			return
		}
	}

	x := lb.start()
	line.pieces = append(line.pieces, piece{font: s.font, size: s.size, x: x / milli, text: text, colour: s.colour})
	lb.width = x + w
}

// addBadge adds a fit badge: a unit that never wraps, set a gap apart from a
// badge or words before it on its line, and starting a new line when it does
// not fit in what is left of this one.
func (lb *lineBuilder) addBadge(s seg) {
	w := badgeWidth(s.text) * milli

	if lb.width > 0 {
		if lb.start()+badgeGap*milli+w > lb.limit {
			lb.newline()
		} else {
			lb.width = lb.start() + badgeGap*milli
		}
	}

	x := lb.start()
	line := lb.current()
	line.pieces = append(line.pieces, piece{
		font: helveticaBold, size: badgeSize, x: x / milli, text: s.text, badge: s.badge,
	})
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
				colour: s.colour,
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
		switch {
		case s.badge != nil:
			lb.addBadge(s)
		case s.value:
			lb.addValue(s)
		default:
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
	// left and width are the text column: the content area, or the inside of
	// the box being drawn.
	left, width int
	// ink is the colour text is drawn in unless its segment sets its own.
	ink colour
	// box is the box being drawn, if any: a page break inside it closes its
	// part on the old page and opens a part on the new one.
	box *openBox
}

// keptHeadings marks the headings at the foot of the current page that no
// body line has followed yet: they start at run index run and shape index
// shape of the page, at y.
type keptHeadings struct {
	active bool
	run    int
	shape  int
	y      int
}

// newDocument returns a document whose body runs from top down to bottom, its
// text column the content area, drawn in --ink.
func newDocument(top, bottom int) *document {
	d := &document{top: top, bottom: bottom, left: marginLeft, width: contentWidth, ink: inkColour}
	d.newPage()

	return d
}

// page returns the page being drawn.
func (d *document) page() *page { return d.pages[len(d.pages)-1] }

// addShape adds a shape to the page being drawn and returns its index.
func (d *document) addShape(s shape) int {
	p := d.page()
	p.shapes = append(p.shapes, s)

	return len(p.shapes) - 1
}

// newPage starts a new page. A box being drawn closes its part on the old page
// and opens its part on the new one first.
func (d *document) newPage() {
	if d.box != nil && len(d.pages) > 0 {
		d.box.closePart(max(d.y-boxBreakGap, d.bottom-boxBreakGap))
	}

	d.pages = append(d.pages, &page{})
	d.y = d.top
	d.atTop = true

	if d.box != nil {
		d.box.openPart(d, d.top+boxContinueTop)
	}

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
// sets it only after drawing its header row (a body line, which ends the
// chain) and clears it when the table ends; no heading is drawn inside a
// table, so nothing newPage draws lands above the moved chain.
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
	movedShapes := slices.Clone(old.shapes[d.kept.shape:])
	old.runs = old.runs[:d.kept.run]
	old.shapes = old.shapes[:d.kept.shape]
	shift := d.top - d.kept.y
	y := d.y + shift

	// A box opened under the chain has its fill's placeholder among the
	// shapes that move: the box opens on the new page with the chain, so it
	// has no part on the old page to close and newPage must not open a second
	// one. The box is set aside while newPage runs and its part follows its
	// placeholder. A box opened before the chain keeps its placeholder on the
	// old page, and newPage closes and opens its parts as for any break.
	box := d.box
	movesBox := box != nil && box.page == old && box.slot >= d.kept.shape

	if movesBox {
		d.box = nil
	}

	d.newPage()

	first := len(d.page().shapes)

	for _, s := range movedShapes {
		s.shift(shift)
		d.page().shapes = append(d.page().shapes, s)
	}

	for _, run := range moved {
		run.y += shift
		d.page().runs = append(d.page().runs, run)
	}

	if movesBox {
		box.page = d.page()
		box.slot = first + box.slot - d.kept.shape
		box.top += shift
		d.box = box
	}

	d.kept = keptHeadings{active: true, run: len(d.page().runs) - len(moved), shape: first, y: d.top}
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
	d.drawLineAt(line, x, d.y-size)
}

// drawLineAt draws one line with its baseline at the y given, its badges
// included.
func (d *document) drawLineAt(line textLine, x, baseline int) {
	for _, pc := range line.pieces {
		if pc.badge != nil {
			d.drawBadge(x+pc.x, baseline, pc.badge, pc.text)

			continue
		}

		ink := pc.colour
		if ink == 0 {
			ink = d.ink
		}

		d.run(textRun{font: pc.font, size: pc.size, x: x + pc.x, y: baseline, text: pc.text, colour: ink})
	}

	d.atTop = false

	if !d.inHeading {
		d.kept = keptHeadings{}
	}
}

// run draws one text run as it is given and marks the page as drawn on: a
// body run ends a chain of kept headings.
func (d *document) run(r textRun) {
	p := d.page()
	p.runs = append(p.runs, r)

	d.atTop = false

	if !d.inHeading {
		d.kept = keptHeadings{}
	}
}

// paragraph lays out segs at the text column's left edge plus indent and draws
// them.
func (d *document) paragraph(indent, size, leading int, segs ...seg) {
	for _, line := range layoutSegs(d.width-indent, segs...) {
		d.drawLine(line, d.left+indent, size, leading)
	}
}

// body draws a paragraph in the body size.
func (d *document) body(segs ...seg) {
	d.paragraph(0, bodySize, bodyLeading, segs...)
}

// item draws a list item: a bullet, and the item's text indented after it.
func (d *document) item(indent int, segs ...seg) {
	lines := layoutSegs(d.width-indent-bulletIndent, segs...)

	for i, line := range lines {
		d.ensure(bodyLeading)

		if i == 0 {
			d.drawLineHere(textLine{pieces: []piece{{font: helvetica, size: bodySize, text: "•"}}},
				d.left+indent, bodySize)
		}

		d.drawLine(line, d.left+indent+bulletIndent, bodySize, bodyLeading)
	}
}

// headingStyle is how a heading is drawn: its ink, and whether a --ring rule
// runs above it (a section heading), a 4 pt --gold rule stands at its left (a
// comparison's per-target heading) or a --surface-2 band lies behind it (a
// Resource details block's ID).
type headingStyle struct {
	ink      colour
	rule     bool
	goldLeft bool
	band     bool
}

// The headings' shapes, in points: the gap under a section heading's rule, the
// gold rule's width and the text's offset after it, and the band's padding.
const (
	headingRuleGap = 4
	goldRuleWidth  = 4
	goldRuleOffset = 10
	bandPad        = 4
	bandBelow      = 3
)

// indentOf returns how far a heading's text stands in from the column's left
// edge, and the space its shapes add under its lines.
func (s headingStyle) indentOf() (indent, extra int) {
	switch {
	case s.goldLeft:
		return goldRuleOffset, 0
	case s.band:
		return bandPad, bandBelow
	default:
		return 0, 0
	}
}

// heading draws a heading of the given size and leading, kept on one page with
// the block that follows it: a heading never ends a page. It asks for room for
// itself and keepWithHeading more; when the block that follows needs more than
// that, the page break that block's ensure makes moves the heading along
// (document.ensure).
func (d *document) heading(style headingStyle, size, leading, before int, segs ...seg) {
	indent, extra := style.indentOf()
	lines := layoutSegs(d.width-indent, segs...)

	d.space(before)

	if style.rule {
		extra += headingRuleGap
	}

	need := len(lines)*leading + extra + keepWithHeading

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
		// when the page has no room for that line (a chain at the top of a
		// page always leaves room for it).
		ink := d.ink
		d.ink = style.ink
		d.body(resized(bodySize, segs)...)
		d.ink = ink
		d.y -= 2

		return
	}

	d.ensure(need)

	if !d.kept.active {
		d.kept = keptHeadings{active: true, run: len(d.page().runs), shape: len(d.page().shapes), y: d.y}
	}

	d.headingShapes(style, len(lines)*leading)

	// The ensure above left room for every line, so none of them breaks the
	// page.
	d.inHeading = true

	ink := d.ink
	d.ink = style.ink

	for _, line := range lines {
		d.drawLine(line, d.left+indent, size, leading)
	}

	d.ink = ink
	d.inHeading = false

	if style.band {
		d.y -= bandBelow
	}

	d.y -= 2
}

// headingShapes draws a heading's shapes for its lines, height points tall,
// starting at the current position: the rule above it, which moves the text
// down, the gold rule at its left, or the band behind it.
func (d *document) headingShapes(style headingStyle, height int) {
	switch {
	case style.rule:
		d.addShape(ruleLine(d.left, d.y, d.width))
		d.y -= headingRuleGap
	case style.goldLeft:
		d.addShape(fillRect(d.left, d.y-height, goldRuleWidth, height, goldColour))
	case style.band:
		d.addShape(fillRect(d.left, d.y-height-bandBelow, d.width, height+bandBelow, surface2Colour))
	}
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
// with headingSize, in --accent.
func (d *document) section(level int, segs ...seg) {
	d.styledSection(headingStyle{ink: accentColour}, level, segs...)
}

// styledSection draws a heading of level 1, 2 or 3 made of segs in style.
func (d *document) styledSection(style headingStyle, level int, segs ...seg) {
	switch level {
	case 1:
		d.heading(style, sectionSize, sectionLeading, 10, segs...)
	case 2:
		d.heading(style, subsectionSize, subLeading, 8, segs...)
	default:
		d.heading(style, blockSize, blockLeading, 8, segs...)
	}
}

// title draws a section heading of the given level holding only the report's
// own words: Helvetica-Bold in --accent under a --ring rule, as the site's
// report parts.
func (d *document) title(level int, text string) {
	d.styledSection(headingStyle{ink: accentColour, rule: true}, level, words(helveticaBold, headingSize(level), text))
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

	left := d.left

	// drawHeader draws the header row on its --surface-2 fill, with the
	// --ring rule under it.
	drawHeader := func() {
		d.addShape(fillRect(left, d.y-headerHeight, tableWidth, headerHeight, surface2Colour))
		d.drawRowLines(left, t.widths, header, 0, tallest(header))
		d.y -= rowGap
		d.addShape(ruleLine(left, d.y, tableWidth))
	}

	// The header row is a box's header: it never ends a page. It asks for room
	// for itself and its first row, or the first row's first line when the
	// row is taller than a page and is split anyway.
	first := headerHeight
	if len(rows) > 0 {
		height := tallest(rows[0]) * tableLeading
		if height > d.top-d.bottom-headerHeight {
			height = tableLeading
		}

		first += height
	}

	d.ensure(first)
	drawHeader()

	d.onNewPage = drawHeader

	for r, row := range rows {
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

			// An even row (the second, the fourth, ...) lies on --bg, its
			// part on each page filled where it is drawn.
			part := (end - i) * tableLeading
			if end == n {
				part += rowGap
			}

			if r%2 == 1 {
				d.addShape(fillRect(left, d.y-part, tableWidth, part, bgColour))
			}

			d.drawRowLines(left, t.widths, row, i, end)
			i = end
		}

		d.y -= rowGap
		d.addShape(ruleLine(left, d.y, tableWidth))
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
func (d *document) drawRowLines(left int, widths []int, row [][]textLine, from, end int) {
	for i := from; i < end; i++ {
		x := left

		for c, lines := range row {
			if i < len(lines) {
				d.drawLineHere(lines[i], x+cellPad, tableSize)
			}

			x += widths[c]
		}

		d.y -= tableLeading
	}
}
