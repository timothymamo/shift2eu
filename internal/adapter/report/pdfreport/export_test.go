package pdfreport

import "fmt"

// The escaper and the literal string form are unexported: only the writer uses
// them, and no reporter port opens them. The black-box tests reach them through
// these aliases, which exist in test builds alone.

// EscapeValue is escapeValue, for the tests: the character-level escape of a
// value into the displayed ASCII/WinAnsi text, as the PRD's Characters section
// describes it.
var EscapeValue = escapeValue

// Decimal is decimal, for the tests: the writer's form of a coordinate in
// thousandths of a point, three decimal places and never a sign.
var Decimal = decimal

// TextWidth is textWidth, for the tests: the width of text in the font resource
// fontName (F1, F2 or F3) at size points, in thousandths of a point, so a test
// can tell where a run the writer draws ends.
func TextWidth(fontName string, size int, text string) int {
	switch fontName {
	case helvetica.resourceName():
		return textWidth(helvetica, size, text)
	case helveticaBold.resourceName():
		return textWidth(helveticaBold, size, text)
	case courier.resourceName():
		return textWidth(courier, size, text)
	default:
		panic("pdfreport: TextWidth of the unknown font resource " + fontName)
	}
}

// PDFString is pdfString, for the tests: the value's PDF literal string form,
// including the outer parentheses.
var PDFString = pdfString

// The layout is unexported too: a black-box test reaches a document only
// through these hooks, which build one the way report.render does, draw into it
// and hand its pages back as plain values. A layout test can then put a kept
// heading chain and a block that outgrows the page together, which no report of
// today's data does, or a boxed section's header near the foot of a page.

// TestRun is one text run of a hand-built document.
type TestRun struct {
	// Font is the run's font resource name, without its slash: F1, F2 or F3.
	Font string
	// Size is the run's type size, in points.
	Size int
	// X and Y are the baseline's start, in points from the page's left and
	// bottom edges.
	X, Y int
	// Text is the text the run draws.
	Text string
}

// TestDocument is a document a layout test draws into directly.
type TestDocument struct{ doc *document }

// TestRect is one rectangle a page's path draws, in points from the page's left
// and bottom edges: a rule, a box's fill or left rule, a badge's fill.
type TestRect struct{ X, Y, W, H int }

// TestShape is one path a page fills or strokes, as a layout test sees it.
type TestShape struct {
	// Op is "f" for a fill or "S" for a stroke.
	Op string
	// Rects are the path's rectangles.
	Rects []TestRect
}

// NewTestDocument returns an empty document whose body has the report's own
// edges, as report.render gives it for a one-line header and a one-line version
// line: the page furniture is left out, so a page holds body lines only.
func NewTestDocument() *TestDocument {
	return &TestDocument{doc: newDocument(headerTop-furnitureLeading-ruleGap-bodyGap,
		footerBase+2*furnitureLeading+ruleGap+bodyGap)}
}

// Body draws a paragraph of the report's own words in the body size.
func (d *TestDocument) Body(text string) {
	d.doc.body(words(helvetica, bodySize, text))
}

// ValueHeading draws a heading of level 1, 2 or 3 holding one value, so the
// number of lines it takes follows from the width of the value's escaped form
// alone, and it is kept with what follows it as a report's heading is.
func (d *TestDocument) ValueHeading(level int, raw string) {
	d.doc.section(level, value(headingSize(level), raw))
}

// Box draws a boxed section the way report.noticeBox draws the required-changes
// box: its heading inside the box, kept on the page with its first item, then
// lines item lines of body text. A layout test can put the box's header near a
// page's end with it, and see the box continue on the next page.
func (d *TestDocument) Box(heading string, lines int) {
	items := make([]func(*document), 0, lines)

	for i := range lines {
		items = append(items, func(d *document) {
			d.item(0, words(helvetica, bodySize, fmt.Sprintf("Box line %d.", i+1)))
		})
	}

	d.doc.noticeBox(infoBox, 3, accentColour, false, heading, items)
}

// Tiles draws the summary's tile row: one tile per fit, then the total, as the
// report's Summary draws it. A layout test can put the row near a page's end.
func (d *TestDocument) Tiles() {
	d.doc.tileRow(fitTiles(1, 2, 3, 6))
}

// TilesWithEstimateAndCounts draws the summary's tile row of an assessment with
// estimates and the counts given: one tile per fit, the total, then the
// estimate's tile holding figure, as report.writeSummary draws it. A layout test
// can then give the row a caption longer than any real inventory holds, "of N
// resources" with an N of as many digits as it likes, beside an estimate.
func (d *TestDocument) TilesWithEstimateAndCounts(direct, partial, none, total int, figure string) {
	d.doc.tileRow(append(fitTiles(direct, partial, none, total), tile{
		label: tileLabel("Estimate"), figure: figure, ink: accentColour, caption: "per month, estimated",
	}))
}

// TilesWithEstimate draws the summary's tile row of an assessment with
// estimates: one tile per fit, the total, then the estimate's tile holding
// figure, as report.writeSummary draws it. A layout test can then give the row a
// figure longer than any real one, which no other hook reaches: the report takes
// it from the assessment's monthly total.
func (d *TestDocument) TilesWithEstimate(figure string) {
	d.doc.tileRow(append(fitTiles(1, 2, 3, 6), tile{
		label: tileLabel("Estimate"), figure: figure, ink: accentColour, caption: "per month, estimated",
	}))
}

// BoxAfterHeading draws a section heading, a box straight after it, the box's
// own heading inside the box, and lines item lines, with no measure of the
// heading, the box and its first item first, as no report of today does:
// report.noticeBox measures its heading and the box's first item together, and
// draw.go's drawBox leaves the measuring to its caller. A layout test can then
// put a box's opening right under a heading at the foot of a page, which is
// where the layout's own page break has to keep both.
func (d *TestDocument) BoxAfterHeading(section, heading string, lines int) {
	d.doc.section(2, words(helveticaBold, headingSize(2), section))
	d.doc.openBox(infoBox)
	d.doc.section(3, words(helveticaBold, headingSize(3), heading))

	for i := range lines {
		d.doc.item(0, words(helvetica, bodySize, fmt.Sprintf("Box line %d.", i+1)))
	}

	d.doc.closeBox()
}

// Card draws one workload card the way report.writeWorkloads draws it: its
// heading inside a filled card, then lines item lines, moved whole to the next
// page when the card does not fit.
func (d *TestDocument) Card(heading string, lines int) {
	items := make([]func(*document), 0, lines)

	for i := range lines {
		items = append(items, func(d *document) {
			d.body(words(helvetica, bodySize, fmt.Sprintf("Card line %d.", i+1)))
		})
	}

	d.doc.space(8)
	d.doc.keepWhole(func(m *document) {
		m.drawBox(workloadBox, []seg{words(helveticaBold, headingSize(3), heading)}, 3, inkColour, false, items)
	})
}

// Shapes returns the shapes each page draws, in the order the writer drew them.
func (d *TestDocument) Shapes() [][]TestShape {
	shapes := make([][]TestShape, 0, len(d.doc.pages))

	for _, p := range d.doc.pages {
		page := make([]TestShape, 0, len(p.shapes))

		for _, s := range p.shapes {
			op := "f"
			if s.stroke {
				op = "S"
			}

			rects := make([]TestRect, 0, len(s.rects))
			for _, r := range s.rects {
				rects = append(rects, TestRect{X: r.x, Y: r.y, W: r.w, H: r.h})
			}

			page = append(page, TestShape{Op: op, Rects: rects})
		}

		shapes = append(shapes, page)
	}

	return shapes
}

// Pages returns the pages drawn so far, each with its text runs in the order the
// writer drew them.
func (d *TestDocument) Pages() [][]TestRun {
	pages := make([][]TestRun, 0, len(d.doc.pages))

	for _, p := range d.doc.pages {
		runs := make([]TestRun, 0, len(p.runs))

		for _, run := range p.runs {
			runs = append(runs, TestRun{
				Font: run.font.resourceName(), Size: run.size, X: run.x, Y: run.y, Text: run.text,
			})
		}

		pages = append(pages, runs)
	}

	return pages
}
