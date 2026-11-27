package pdfreport

// The escaper and the literal string form are unexported: only the writer uses
// them, and no reporter port opens them. The black-box tests reach them through
// these aliases, which exist in test builds alone.

// EscapeValue is escapeValue, for the tests: the character-level escape of a
// value into the displayed ASCII/WinAnsi text, as the PRD's Characters section
// describes it.
var EscapeValue = escapeValue

// PDFString is pdfString, for the tests: the value's PDF literal string form,
// including the outer parentheses.
var PDFString = pdfString

// The layout is unexported too: a black-box test reaches a document only
// through these hooks, which build one the way report.render does, draw into it
// and hand its pages back as plain values. A layout test can then put a kept
// heading chain and a block that outgrows the page together, which no report of
// today's data does.

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
