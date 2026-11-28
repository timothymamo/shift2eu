package pdfreport

import (
	"strconv"

	"github.com/timothymamo/shift2eu/internal/domain/mapping"
)

// fitStyle is how one fit is drawn: its word, its ink and background colours
// and its icon (PRD #584, The PDF, Look, Fit badges and Icons).
type fitStyle struct {
	word string
	ink  colour
	bg   colour
	icon func(x, y int) []shape
}

// The three fits' styles. The icons are the site's: a check mark, a
// half-filled disc and a cross.
var (
	directStyle  = &fitStyle{word: "Direct", ink: directInkColour, bg: directBgColour, icon: checkIcon}
	partialStyle = &fitStyle{word: "Partial", ink: partialInkColour, bg: partialBgColour, icon: discIcon}
	noneStyle    = &fitStyle{word: "None", ink: noneInkColour, bg: noneBgColour, icon: crossIcon}
)

// styleOf returns the style of a fit.
func styleOf(fit mapping.Fit) *fitStyle {
	switch fit {
	case mapping.Direct:
		return directStyle
	case mapping.Partial:
		return partialStyle
	default:
		return noneStyle
	}
}

// badge returns a fit's badge: its icon and its word, after count when count
// is not empty ("3 Direct").
func badge(fit mapping.Fit, count string) seg {
	style := styleOf(fit)

	text := style.word
	if count != "" {
		text = count + " " + style.word
	}

	return seg{font: helveticaBold, size: badgeSize, text: text, badge: style}
}

// A badge's geometry, in points: its word's size, the padding at its left, the
// icon's side and the gap after it, the padding at its right, how far its fill
// reaches below the baseline and how tall the fill is, and the gap between two
// badges on a line.
const (
	badgeSize     = 7
	badgePad      = 2
	iconSide      = 6
	iconGap       = 2
	badgeBelow    = 2
	badgeHeight   = 9
	badgeGap      = 4
	badgeTextLeft = badgePad + iconSide + iconGap
)

// badgeWidth returns the width of a badge holding text, in whole points.
func badgeWidth(text string) int {
	return badgeTextLeft + (textWidth(helveticaBold, badgeSize, text)+milli-1)/milli + badgePad
}

// drawBadge draws a badge at x on the baseline: its fill in the fit's
// background colour, then its icon and its word in the fit's ink.
func (d *document) drawBadge(x, baseline int, style *fitStyle, text string) {
	d.addShape(fillRect(x, baseline-badgeBelow, badgeWidth(text), badgeHeight, style.bg))

	for _, s := range style.icon(x+badgePad, baseline) {
		s.colour = style.ink
		d.addShape(s)
	}

	d.run(textRun{font: helveticaBold, size: badgeSize, x: x + badgeTextLeft, y: baseline, text: text, colour: style.ink})
}

// iconBottom is how far below the baseline an icon's square starts, in
// thousandths of a point.
const iconBottom = 500

// iconPoint returns a point of an icon whose square starts at x points and half
// a point below the baseline, dx and dy thousandths into it.
func iconPoint(x, baseline, dx, dy int) point {
	return point{x: x*milli + dx, y: baseline*milli - iconBottom + dy}
}

// checkIcon is direct's check mark: a stroked polyline of three points.
func checkIcon(x, baseline int) []shape {
	return []shape{{stroke: true, width: milli, subpaths: []subpath{{points: []point{
		iconPoint(x, baseline, 700, 2900),
		iconPoint(x, baseline, 2400, 1100),
		iconPoint(x, baseline, 5300, 4900),
	}}}}}
}

// crossIcon is none's cross: two stroked diagonal lines.
func crossIcon(x, baseline int) []shape {
	return []shape{{stroke: true, width: milli, subpaths: []subpath{
		{points: []point{iconPoint(x, baseline, 1000, 1000), iconPoint(x, baseline, 5000, 5000)}},
		{points: []point{iconPoint(x, baseline, 1000, 5000), iconPoint(x, baseline, 5000, 1000)}},
	}}}
}

// discVertices are a regular 16-sided polygon of radius 2.6 points around its
// centre, from the right going anticlockwise, in thousandths of a point: the
// circle of partial's half-filled disc, never computed with floating point.
var discVertices = [16][2]int{
	{2600, 0},
	{2402, 995},
	{1838, 1838},
	{995, 2402},
	{0, 2600},
	{-995, 2402},
	{-1838, 1838},
	{-2402, 995},
	{-2600, 0},
	{-2402, -995},
	{-1838, -1838},
	{-995, -2402},
	{0, -2600},
	{995, -2402},
	{1838, -1838},
	{2402, -995},
}

// discCentre is the disc's centre inside the icon's square, in thousandths.
const discCentre = 3000

// discIcon is partial's half-filled disc: the polygon stroked, and its left
// half, from its top vertex to its bottom one, filled.
func discIcon(x, baseline int) []shape {
	vertex := func(i int) point {
		return iconPoint(x, baseline, discCentre+discVertices[i][0], discCentre+discVertices[i][1])
	}

	circle := make([]point, 0, len(discVertices))
	for i := range discVertices {
		circle = append(circle, vertex(i))
	}

	half := make([]point, 0, len(discVertices)/2+1)
	for i := len(discVertices) / 4; i <= 3*len(discVertices)/4; i++ {
		half = append(half, vertex(i))
	}

	return []shape{
		{subpaths: []subpath{{points: half, closed: true}}},
		{stroke: true, width: 800, subpaths: []subpath{{points: circle, closed: true}}},
	}
}

// warningTriangle draws the blockers box's warning icon with its base's left
// end at x, y points: a triangle filled in --warn-rule with an exclamation mark
// of two white rectangles inside it.
func (d *document) warningTriangle(x, y int) {
	d.addShape(shape{colour: warnRuleColour, subpaths: []subpath{{points: []point{
		{x: x * milli, y: y * milli},
		{x: (x + triangleSide) * milli, y: y * milli},
		{x: (x + triangleSide/2) * milli, y: (y + triangleHeight) * milli},
	}, closed: true}}})
	d.addShape(shape{colour: mastInkColour, rects: []rect{
		{x: x + triangleSide/2 - 1, y: y + 4, w: 2, h: 4},
		{x: x + triangleSide/2 - 1, y: y + 1, w: 2, h: 2},
	}})
}

// The warning triangle's base and height, and how far the heading's words
// stand after its left end, in points.
const (
	triangleSide   = 12
	triangleHeight = 11
	triangleOffset = 16
)

// boxStyle is how a box is drawn: its fill, the 4 pt rule at its left edge (0
// for none) and its 0.5 pt outline (0 for none), and the padding of its text.
type boxStyle struct {
	fill    colour
	rule    colour
	outline colour
	pad     int
}

// The boxes (PRD #584, The PDF, Look): the title band, the required-changes
// box, the blockers box and a workload card.
var (
	titleBox    = boxStyle{fill: mastColour, rule: goldColour, pad: 8}
	infoBox     = boxStyle{fill: infoBgColour, rule: infoRuleColour, pad: 8}
	warnBox     = boxStyle{fill: warnBgColour, rule: warnRuleColour, pad: 8}
	workloadBox = boxStyle{fill: bgColour, outline: ringColour, pad: 8}
)

// The boxes' shapes, in points: the left rule's width, the padding under a
// box's last line, how far the part of a box on a new page reaches above the
// body's top, how far under its last line a part ends at a page break, and the
// least height of a part, which keeps every box taller than a badge.
const (
	boxRuleWidth   = 4
	boxBottomPad   = 6
	boxContinueTop = 4
	boxBreakGap    = 2
	boxMinPart     = 26
)

// openBox is the box being drawn: its style and outer edges, and the part on
// the current page: the index of its fill's placeholder among the page's
// shapes and its top.
type openBox struct {
	style       boxStyle
	left, width int
	page        *page
	slot        int
	top         int
}

// openPart starts the box's part on the current page at top: a placeholder
// for its fill, so the fill is drawn under everything drawn inside it.
func (b *openBox) openPart(d *document, top int) {
	b.page = d.page()
	b.slot = d.addShape(shape{})
	b.top = top
}

// closePart ends the box's part on its page at bottom: its fill in the
// placeholder, then its left rule and outline.
func (b *openBox) closePart(bottom int) {
	height := b.top - bottom

	b.page.shapes[b.slot] = fillRect(b.left, bottom, b.width, height, b.style.fill)

	if b.style.rule != 0 {
		b.page.shapes = append(b.page.shapes, fillRect(b.left, bottom, boxRuleWidth, height, b.style.rule))
	}

	if b.style.outline != 0 {
		b.page.shapes = append(b.page.shapes, shape{
			stroke: true, colour: b.style.outline, width: hairline,
			rects: []rect{{x: b.left, y: bottom, w: b.width, h: height}},
		})
	}
}

// openBox starts a box at the current position: its text column moves inside
// its padding and left rule, and a page break inside it draws it again on the
// new page.
func (d *document) openBox(style boxStyle) {
	b := &openBox{style: style, left: d.left, width: d.width}
	b.openPart(d, d.y)
	d.box = b

	inset := style.pad
	if style.rule != 0 {
		inset += boxRuleWidth
	}

	d.left += inset
	d.width -= inset + style.pad
	d.y -= style.pad
}

// closeBox ends the box under its last line. A part shorter than boxMinPart,
// which only a part on a new page holding one line can be, is made taller, so
// no box is mistaken for a badge.
func (d *document) closeBox() {
	b := d.box

	bottom := min(d.y-boxBottomPad, b.top-boxMinPart)
	b.closePart(bottom)

	d.box = nil
	d.left, d.width = b.left, b.width
	d.y = bottom
	d.atTop = false
}

// measureTop is the top of the page a measuring document draws on: far enough
// from anything a block draws that it never breaks a page.
const measureTop = 1 << 29

// measure returns how many points draw takes when drawn at the current
// position on a page tall enough to hold it: the height a block asks for when
// it moves whole to the next page. Nothing it draws reaches the document.
func (d *document) measure(draw func(m *document)) int {
	m := &document{top: measureTop, bottom: -measureTop, left: d.left, width: d.width, ink: d.ink}
	m.pages = []*page{{}}
	m.y = measureTop

	draw(m)

	return measureTop - m.y
}

// keepWhole draws a block that moves whole to the next page when it does not
// fit in the space left: the banner, a tile row and a workload card. A block
// taller than a page's body starts on a new page and continues as a box does.
func (d *document) keepWhole(draw func(m *document)) {
	d.ensure(d.measure(draw))
	draw(d)
}

// drawBox draws a box from the current position: its heading inside it, then
// its items, each drawing its own lines.
func (d *document) drawBox(style boxStyle, heading []seg, level int, ink colour, warn bool, items []func(*document)) {
	d.openBox(style)
	d.boxHeading(level, ink, warn, heading...)

	for _, item := range items {
		item(d)
	}

	d.closeBox()
}

// boxHeading draws a box's heading made of segs at the size of level, in ink,
// after the warning triangle when warn is set. The caller has made room for it
// and for the box's first line, so it never ends a page.
func (d *document) boxHeading(level int, ink colour, warn bool, segs ...seg) {
	size := headingSize(level)
	leading := headingLeading(level)

	indent := 0
	if warn {
		indent = triangleOffset
	}

	saved := d.ink
	d.ink = ink

	for i, line := range layoutSegs(d.width-indent, segs...) {
		if i == 0 && warn {
			d.warningTriangle(d.left, d.y-size-1)
		}

		d.drawLine(line, d.left+indent, size, leading)
	}

	d.ink = saved
	d.y -= 2
}

// headingLeading returns the leading of a heading of level 1, 2 or 3.
func headingLeading(level int) int {
	switch level {
	case 1:
		return sectionLeading
	case 2:
		return subLeading
	default:
		return blockLeading
	}
}

// tile is one tile of the summary's row: its label (a fit's badge, or the
// total's or the estimate's words), its figure, the colour of its figure and
// top rule, and its caption.
type tile struct {
	label   seg
	figure  string
	ink     colour
	caption string
}

// The tile row's geometry, in points: the gap between two tiles, the top
// rule's height, the padding, the figure's largest and smallest size, and the
// row's height.
const (
	tileGap        = 6
	tileRule       = 3
	tilePad        = 6
	tileFigureSize = 18
	tileFigureMin  = 8
	tileHeight     = 62
)

// tileRow draws the summary's tiles side by side across the text width, moved
// whole to the next page when they do not fit.
func (d *document) tileRow(tiles []tile) {
	d.space(6)
	d.ensure(tileHeight)

	width := (d.width - tileGap*(len(tiles)-1)) / len(tiles)
	top := d.y

	for i, t := range tiles {
		x := d.left + i*(width+tileGap)

		d.addShape(shape{
			stroke: true, colour: ringColour, width: hairline,
			rects: []rect{{x: x, y: top - tileHeight, w: width, h: tileHeight}},
		})
		d.addShape(fillRect(x, top-tileRule, width, tileRule, t.ink))

		inner := x + tilePad
		label := top - tileRule - tilePad - badgeHeight + badgeBelow
		d.drawLineAt(layoutSegs(width-2*tilePad, t.label)[0], inner, label)

		size := tileFigureSize
		for size > tileFigureMin && textWidth(helveticaBold, size, t.figure) > (width-2*tilePad)*milli {
			size--
		}

		figure := label - badgeBelow - tileFigureGap
		d.run(textRun{font: helveticaBold, size: size, x: inner, y: figure, text: t.figure, colour: t.ink})

		caption := figure - furnitureLeading - 2
		for _, line := range layoutSegs(width-2*tilePad, words(helvetica, furnitureSize, t.caption)) {
			d.drawLineAt(colourLine(line, mutedColour), inner, caption)
			caption -= furnitureLeading
		}
	}

	d.y = top - tileHeight
}

// tileFigureGap is how far a tile's figure's baseline sits under its label's
// badge, in points: the figure's cap height and a gap.
const tileFigureGap = 17

// colourLine returns line with every piece drawn in c.
func colourLine(line textLine, c colour) textLine {
	pieces := make([]piece, len(line.pieces))
	for i, pc := range line.pieces {
		pc.colour = c
		pieces[i] = pc
	}

	return textLine{pieces: pieces}
}

// fitTiles returns the summary's tiles: one per fit with its badge and count
// "of N resources", then the total.
func fitTiles(direct, partial, none, total int) []tile {
	of := "of " + strconv.Itoa(total) + " resources"

	return []tile{
		{label: badge(mapping.Direct, ""), figure: strconv.Itoa(direct), ink: directInkColour, caption: of},
		{label: badge(mapping.Partial, ""), figure: strconv.Itoa(partial), ink: partialInkColour, caption: of},
		{label: badge(mapping.None, ""), figure: strconv.Itoa(none), ink: noneInkColour, caption: of},
		{label: tileLabel("Total"), figure: strconv.Itoa(total), ink: inkColour, caption: "resources in total"},
	}
}

// The contact banner's words, the site's "Planning a migration?" band
// (site/index.html, section.contact-band) in its exact words; the apostrophe
// is the ASCII one, as the site writes it.
const (
	bannerEyebrow = "Planning a migration?"
	bannerHeading = "Questions about your results, or how to move forward?"
	bannerText    = "We're happy to help you read an assessment, weigh the EU options, or plan the move itself. " +
		"Email info@shift2eu.com."
	bannerAddress = "info@shift2eu.com"
)

// The banner's geometry, in points: its padding, the gold rule along its top,
// the sizes and leadings of its eyebrow, heading and text, the gaps between
// them, and the gold box around the address.
const (
	bannerPad          = 14
	bannerRule         = 3
	bannerEyebrowSize  = 9
	bannerEyebrowLead  = 13
	bannerHeadingSize  = 12
	bannerHeadingLead  = 16
	bannerTextSize     = 9
	bannerTextLead     = 12
	bannerGap          = 4
	bannerAddressSize  = 9
	bannerAddressPad   = 6
	bannerAddressBox   = 18
	bannerAddressBelow = 5
)

// banner draws the contact banner across the text width: a --mast fill with a
// 3 pt --gold rule along its top, the eyebrow, the heading and the text, and
// the address in a --gold box. It is plain text on filled boxes, with no link,
// and moves whole to the next page when it does not fit in the space left.
func (d *document) banner() {
	inner := d.width - 2*bannerPad
	heading := layoutSegs(inner, words(helveticaBold, bannerHeadingSize, bannerHeading))
	text := layoutSegs(inner, words(helvetica, bannerTextSize, bannerText))

	height := 2*bannerPad + bannerEyebrowLead + bannerGap + len(heading)*bannerHeadingLead + bannerGap +
		len(text)*bannerTextLead + bannerGap + bannerAddressBox

	d.space(12)
	d.ensure(height)

	top := d.y
	left := d.left + bannerPad

	d.addShape(fillRect(d.left, top-height, d.width, height, mastColour))
	d.addShape(fillRect(d.left, top-bannerRule, d.width, bannerRule, goldColour))

	y := top - bannerPad
	d.run(textRun{
		font: helveticaBold, size: bannerEyebrowSize, x: left, y: y - bannerEyebrowSize,
		text: bannerEyebrow, colour: mastLinkColour,
	})
	y -= bannerEyebrowLead + bannerGap

	for _, line := range heading {
		d.drawLineAt(colourLine(line, mastInkColour), left, y-bannerHeadingSize)
		y -= bannerHeadingLead
	}

	y -= bannerGap

	for _, line := range text {
		d.drawLineAt(colourLine(line, mastTextColour), left, y-bannerTextSize)
		y -= bannerTextLead
	}

	y -= bannerGap

	box := (textWidth(helveticaBold, bannerAddressSize, bannerAddress)+milli-1)/milli + 2*bannerAddressPad
	d.addShape(fillRect(left, y-bannerAddressBox, box, bannerAddressBox, goldColour))
	d.run(textRun{
		font: helveticaBold, size: bannerAddressSize, x: left + bannerAddressPad, y: y - bannerAddressBox + bannerAddressBelow,
		text: bannerAddress, colour: goldInkColour,
	})

	d.y = top - height
}

// tileLabel returns the label of the total's or the estimate's tile.
func tileLabel(text string) seg {
	return seg{font: helveticaBold, size: badgeSize, text: text, colour: inkColour}
}
