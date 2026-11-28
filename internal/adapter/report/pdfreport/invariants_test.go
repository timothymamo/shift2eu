package pdfreport_test

// This file pins the structural invariants every PDF the writer emits has to
// hold, and the writer's fixed content-stream operator table. The golden,
// hostile, determinism and fuzz tests all check them through checkPDFStructure.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// The page size the PRD fixes: A4 portrait, 595 × 842 pt (Decided, as
// recommended, 2).
const (
	pageWidth  = 595
	pageHeight = 842
)

// pdfContentOperators is the writer's fixed content-stream operator table: each
// operator with the operands it takes, exactly ADR 0049's amended set of sixteen
// (q Q h f S BT ET w rg RG re m l Tf Tm Tj). The tests pin it so that a writer
// cannot slip in an operator of its own: no graphics state dictionary (gs), no
// XObject (Do), no inline image (BI), no clipping, curve, dash or other colour
// operator, and none of the other text operators. Milestone 1's grey G is gone:
// RG strokes the palette's colours.
//
// Operands are integers (re, Tf's size and Tm), decimals (w, rg, RG, m and l),
// literal strings (Tj) and font resource names (Tf), and the reader checks each
// operator's operands against its row here, that a decimal is digits, a dot and
// exactly three digits, that an integer is digits alone, and that the font
// resource a Tf selects is one of the writer's constant names /F1, /F2, /F3.
var pdfContentOperators = map[string][]pdfValueKind{
	"q":  {},
	"Q":  {},
	"h":  {},
	"f":  {},
	"S":  {},
	"BT": {},
	"ET": {},
	"w":  {pdfValueDecimal},
	"rg": {pdfValueDecimal, pdfValueDecimal, pdfValueDecimal},
	"RG": {pdfValueDecimal, pdfValueDecimal, pdfValueDecimal},
	"re": {pdfValueInteger, pdfValueInteger, pdfValueInteger, pdfValueInteger},
	"m":  {pdfValueDecimal, pdfValueDecimal},
	"l":  {pdfValueDecimal, pdfValueDecimal},
	"Tf": {pdfValueName, pdfValueInteger},
	"Tm": {pdfValueInteger, pdfValueInteger, pdfValueInteger, pdfValueInteger, pdfValueInteger, pdfValueInteger},
	"Tj": {pdfValueString},
}

// pdfBannedNames are the names a file must never hold, outside a string: each
// one would make a reader act on the file, name an image, a graphic state, a
// pattern, a shading or a colour space of its own, or name metadata that varies
// between runs (URLs, actions, scripts, forms, embedded files and fonts, dates,
// the document ID and the producer). ADR 0049's amendment adds the six names of
// its own: no /Link beside the banner's address, no /XObject, /ExtGState,
// /Pattern, /Shading or /ColorSpace anywhere, since each page's /Resources holds
// only /Font.
var pdfBannedNames = []string{
	"Annots",
	"URI",
	"Launch",
	"JavaScript",
	"JS",
	"OpenAction",
	"AA",
	"AcroForm",
	"EmbeddedFile",
	"RichMedia",
	"XFA",
	"FontFile",
	"FontFile2",
	"FontFile3",
	"CreationDate",
	"ModDate",
	"ID",
	"Producer",
	"Link",
	"XObject",
	"ExtGState",
	"Pattern",
	"Shading",
	"ColorSpace",
}

// pdfTitle is the one constant entry of the /Info dictionary (Decided, as
// recommended, 8).
const pdfTitle = "shift2eu migration assessment"

// checkPDFStructure checks every structural invariant of the PRD for one PDF and
// returns it parsed, so a test can go on to read its text. It fails the test on
// the first invariant that does not hold.
func checkPDFStructure(tb testing.TB, data []byte) *testPDF {
	tb.Helper()

	checkPrintableASCII(tb, data)

	file := openTestPDF(tb, data)
	checkObjectTypes(tb, file)
	checkFonts(tb, file)
	checkInfo(tb, file)
	checkBannedNames(tb, file)
	checkColoursComeFromThePalette(tb, file)
	checkRunsStayInsideTheirBoxes(tb, file)
	checkNoHeadingEndsAPage(tb, file)
	checkNoBoxHeaderEndsAPage(tb, file)

	return file
}

// checkPrintableASCII checks that every byte of the file is a line feed or in
// 0x20 to 0x7E: the file is printable ASCII plus line feeds, so writing it to a
// terminal cannot send an escape sequence and the goldens diff as text.
func checkPrintableASCII(tb testing.TB, data []byte) {
	tb.Helper()

	for i, b := range data {
		if b == '\n' || (b >= 0x20 && b <= 0x7E) {
			continue
		}

		start := max(0, i-12)
		end := min(len(data), i+12)

		tb.Fatalf("pdf: byte %d is %#02x, outside the file's printable ASCII plus line feeds: %q",
			i, b, data[start:end])
	}
}

// checkObjectTypes checks that every object is one of the writer's types: the
// catalog, the page tree, a page, a font, a content stream or the info
// dictionary. The reader has already checked the trees and the stream framing.
func checkObjectTypes(tb testing.TB, file *testPDF) {
	tb.Helper()

	root := file.trailer.mustRef(tb, "the trailer", "Root")
	pageTree := file.object(tb, root).dict.mustRef(tb, "the catalog", "Pages")

	info := -1
	if value, ok := file.trailer.get("Info"); ok {
		if value.kind != pdfValueRef {
			tb.Fatalf("pdf: the trailer's /Info is %s, want an indirect reference", kindName(value.kind))
		}

		info = value.ref
	}

	pages := make(map[int]bool, len(file.pages))
	contents := make(map[int]bool, len(file.pages))

	for _, page := range file.pages {
		pages[page.num] = true
		contents[page.content] = true
	}

	for _, obj := range file.order {
		switch {
		case obj.num == root:
			mustType(tb, obj, "Catalog")
		case obj.num == pageTree:
			mustType(tb, obj, "Pages")
		case pages[obj.num]:
			mustType(tb, obj, "Page")
		case contents[obj.num]:
			if name, ok := obj.typeName(); ok {
				tb.Fatalf("pdf: the content stream object %d has /Type /%s, want no type", obj.num, name)
			}

			if !obj.hasStream {
				tb.Fatalf("pdf: the content stream object %d is not a stream", obj.num)
			}
		case obj.num == info:
			if name, ok := obj.typeName(); ok {
				tb.Fatalf("pdf: the info object %d has /Type /%s, want no type", obj.num, name)
			}
		default:
			name, ok := obj.typeName()
			if !ok || name != "Font" {
				tb.Fatalf("pdf: object %d has /Type /%s, outside the writer's catalog, pages, page, font, content stream and info objects",
					obj.num, name)
			}
		}
	}

	for _, page := range file.pages {
		obj := file.object(tb, page.num)
		checkMediaBox(tb, page, obj)
		checkPageFonts(tb, file, page, obj)
	}
}

// mustType fails the test unless obj's /Type is want.
func mustType(tb testing.TB, obj *pdfObject, want string) {
	tb.Helper()

	name, ok := obj.typeName()
	if !ok || name != want {
		tb.Fatalf("pdf: object %d has /Type /%s, want /%s", obj.num, name, want)
	}
}

// checkMediaBox checks that the page is A4 portrait: [0 0 595 842].
func checkMediaBox(tb testing.TB, page *pdfPage, obj *pdfObject) {
	tb.Helper()

	box := obj.dict.mustArray(tb, fmt.Sprintf("page %d", page.num), "MediaBox")
	want := []int{0, 0, pageWidth, pageHeight}

	if len(box) != len(want) {
		tb.Fatalf("pdf: page %d's /MediaBox holds %d numbers, want [0 0 595 842]", page.num, len(box))
	}

	for i, number := range box {
		if number.kind != pdfValueInteger || number.num != want[i] {
			tb.Fatalf("pdf: page %d's /MediaBox is not A4 portrait [0 0 595 842]", page.num)
		}
	}
}

// checkPageFonts checks that a page's /Resources holds only a /Font dictionary,
// and that the dictionary names every font its text runs use, as an indirect
// reference to a font object. ADR 0049's amendment allows no other resource:
// no /XObject, /ExtGState, /Pattern, /Shading or /ColorSpace.
func checkPageFonts(tb testing.TB, file *testPDF, page *pdfPage, obj *pdfObject) {
	tb.Helper()

	resources, ok := obj.dict.get("Resources")
	if !ok || resources.kind != pdfValueDict {
		tb.Fatalf("pdf: page %d has no /Resources dictionary", page.num)
	}

	if names := resources.dict.names(); len(names) != 1 || names[0] != "Font" {
		tb.Fatalf("pdf: page %d's /Resources holds %q, want only /Font: no XObject, ExtGState, Pattern, Shading or ColorSpace resource is in the file",
			page.num, names)
	}

	fonts, ok := resources.dict.get("Font")
	if !ok || fonts.kind != pdfValueDict {
		tb.Fatalf("pdf: page %d has no /Resources /Font dictionary", page.num)
	}

	for _, run := range page.runs {
		font, ok := fonts.dict.get(run.Font)
		if !ok || font.kind != pdfValueRef {
			tb.Fatalf("pdf: page %d draws text in the font resource /%s, which its /Resources /Font dictionary does not name",
				page.num, run.Font)
		}

		mustType(tb, file.object(tb, font.ref), "Font")
	}
}

// checkFonts checks every font object: one of the writer's three core fonts, a
// standard Type 1 font with WinAnsiEncoding, and never an embedded font file.
func checkFonts(tb testing.TB, file *testPDF) {
	tb.Helper()

	fonts := 0

	for _, obj := range file.order {
		if name, ok := obj.typeName(); !ok || name != "Font" {
			continue
		}

		fonts++

		if subtype := obj.dict.mustName(tb, "a font object", "Subtype"); subtype != "Type1" {
			tb.Fatalf("pdf: a font's /Subtype is /%s, want the standard Type 1 font /Type1", subtype)
		}

		switch base := obj.dict.mustName(tb, "a font object", "BaseFont"); base {
		case "Helvetica", "Helvetica-Bold", "Courier":
		default:
			tb.Fatalf("pdf: a font's /BaseFont is /%s, want one of the writer's core fonts Helvetica, Helvetica-Bold or Courier", base)
		}

		if encoding := obj.dict.mustName(tb, "a font object", "Encoding"); encoding != "WinAnsiEncoding" {
			tb.Fatalf("pdf: a font's /Encoding is /%s, want /WinAnsiEncoding", encoding)
		}
	}

	if fonts == 0 {
		tb.Fatalf("pdf: the file holds no font object")
	}
}

// checkInfo checks the /Info dictionary: one constant /Title, and no producer,
// author, creator, date or trailer /ID (Decided, as recommended, 8, and the
// reproducibility rule of the PRD).
func checkInfo(tb testing.TB, file *testPDF) {
	tb.Helper()

	for _, name := range file.trailer.names() {
		switch name {
		case "Size", "Root", "Info":
		default:
			tb.Fatalf("pdf: the trailer holds /%s, want only /Size, /Root and /Info: the file has no /ID, /Prev, /Encrypt or /XRefStm",
				name)
		}
	}

	info := file.trailer.mustRef(tb, "the trailer", "Info")
	obj := file.object(tb, info)

	if obj.hasStream {
		tb.Fatalf("pdf: the info object %d is a stream", info)
	}

	if names := obj.dict.names(); len(names) != 1 || names[0] != "Title" {
		tb.Fatalf("pdf: the info dictionary holds %q, want the one entry /Title", names)
	}

	title, ok := obj.dict.get("Title")
	if !ok || title.kind != pdfValueString {
		tb.Fatalf("pdf: the info dictionary's /Title is %s, want a literal string", kindName(title.kind))
	}

	if title.str != pdfTitle {
		tb.Fatalf("pdf: the info dictionary's /Title is %q, want the constant %q", title.str, pdfTitle)
	}
}

// checkBannedNames checks that no name of any dictionary, outside a string, is
// one of the names that would make a reader act on the file or name varying
// metadata. A value that merely spells one of them inside a string is no
// finding.
func checkBannedNames(tb testing.TB, file *testPDF) {
	tb.Helper()

	names := make(map[string]bool)
	collectNames(file.trailer, names)

	for _, obj := range file.order {
		collectNames(obj.dict, names)
	}

	for _, banned := range pdfBannedNames {
		if names[banned] {
			tb.Fatalf("pdf: the file holds the name /%s, which would make a reader act on the file or name varying metadata", banned)
		}
	}
}

// collectNames adds every name of dict, its arrays and its nested dictionaries,
// to names.
func collectNames(dict pdfDict, names map[string]bool) {
	for _, entry := range dict.entries {
		names[entry.name] = true
		collectValueNames(entry.value, names)
	}
}

// collectValueNames adds every name of value to names, descending into arrays
// and dictionaries.
func collectValueNames(value pdfValue, names map[string]bool) {
	switch value.kind {
	case pdfValueName:
		names[value.name] = true
	case pdfValueArray:
		for _, item := range value.array {
			collectValueNames(item, names)
		}
	case pdfValueDict:
		collectNames(value.dict, names)
	case pdfValueInteger, pdfValueDecimal, pdfValueString, pdfValueRef:
		// A number, a string and a reference hold no name; a value that spells
		// one inside a string is text, never a name.
	}
}

// courierAdvance is Courier's advance width of every character, in thousandths
// of the type size (ISO 32000-1, 9.6.2.2: the standard 14 fonts' metrics). Every
// value the report shows is drawn in Courier at one size, so the tests measure a
// value's drawing without the writer's own metrics table.
const courierAdvance = 600

// headerRuleGap bounds how far above its rule a table's header row may sit. The
// writer draws the rule just under the header row's baseline and the table's
// first row further below it, so a rule with no line within this gap above it is
// no table's.
const headerRuleGap = 9

// tableHeaderFont and tableHeaderSize are what a table's header cells are drawn
// in: Helvetica-Bold at the table size (PRD, Look: table header cells in
// Helvetica-Bold). A styled page draws more zero-height rules than a table has —
// the 0.5 pt rule above a section heading, the gold rules of the title block and
// the banner — so a header row is recognised by its font and size too, not by
// its rule alone.
const (
	tableHeaderFont = "F2"
	tableHeaderSize = 8
)

// checkRunsStayInsideTheirBoxes checks that no value the report draws ends past
// the right edge of its box: a value drawn in a table cell stays inside its
// column, and a value drawn anywhere else inside the content area. A value whose
// wrapping lets a line run past its column overlaps the next column's text.
func checkRunsStayInsideTheirBoxes(tb testing.TB, file *testPDF) {
	tb.Helper()

	for index, page := range file.pages {
		checkPageRunsStayInsideTheirBoxes(tb, index, page)
	}
}

// pdfRunLine is one baseline of one page: the runs drawn on it, in the order
// drawn.
type pdfRunLine struct {
	Y    int
	runs []pdfTextRun
}

// pdfPageTable is one table a page draws, recovered from the page's rules and
// lines: the hairline under its header row, the type size of its cells, the
// inner left edge of each of its columns and its header row's baseline.
type pdfPageTable struct {
	rule    pdfRule
	size    int
	columns []int
	headerY int
}

// pageRunLines returns the page's runs grouped by baseline, in drawing order. The
// writer draws one line's runs together, so its runs are consecutive in the
// content stream.
func pageRunLines(page *pdfPage) []pdfRunLine {
	var lines []pdfRunLine

	for _, run := range page.runs {
		if n := len(lines); n > 0 && lines[n-1].Y == run.Y {
			lines[n-1].runs = append(lines[n-1].runs, run)

			continue
		}

		lines = append(lines, pdfRunLine{Y: run.Y, runs: []pdfTextRun{run}})
	}

	return lines
}

// pageBand returns the baselines of the page's own two rules, the header's at the
// top and the footer's at the foot, and true: the page's furniture is above the
// first and below the second, and every table's rule lies between them. It
// reports false for a page that draws no rule, such as the tests' own hand-built
// minimal PDF, which has no box to measure text against.
func pageBand(page *pdfPage) (lowest, highest int, ok bool) {
	if len(page.rules) == 0 {
		return 0, 0, false
	}

	lowest, highest = page.rules[0].Y, page.rules[0].Y

	for _, rule := range page.rules {
		lowest, highest = min(lowest, rule.Y), max(highest, rule.Y)
	}

	return lowest, highest, true
}

// pageTables recovers the tables a page draws. Each table has a hairline rule
// under its header row; the header row is the nearest line above that rule, and
// its runs are its cells, one run each at the inner left edge of the column, so
// their x positions are the columns' inner left edges. The rule spans the whole
// table, from its left edge to its right, and the padding a cell leaves before
// its column's edge is the distance from the rule's left edge to the first
// column's inner left edge. checkHeaderSpansTheColumns fails the test when that
// recovery could have missed a column.
func pageTables(tb testing.TB, page *pdfPage) []pdfPageTable {
	tb.Helper()

	lowest, highest, ok := pageBand(page)
	if !ok {
		return nil
	}

	lines := pageRunLines(page)

	var tables []pdfPageTable

	for _, rule := range page.rules {
		if rule.Y <= lowest || rule.Y >= highest || rule.H != 0 {
			continue
		}

		header, found := headerLineAbove(lines, rule.Y)
		if !found || len(header.runs) == 0 || header.runs[0].Font != tableHeaderFont || header.runs[0].Size != tableHeaderSize {
			continue
		}

		columns := make([]int, 0, len(header.runs))
		for _, run := range header.runs {
			columns = append(columns, run.X)
		}

		slices.Sort(columns)

		tables = append(tables, pdfPageTable{
			rule:    rule,
			size:    header.runs[0].Size,
			columns: slices.Compact(columns),
			headerY: header.Y,
		})
	}

	checkHeaderSpansTheColumns(tb, lines, tables, lowest)

	return tables
}

// checkHeaderSpansTheColumns checks that no table's header cell wrapped onto a
// second line. The columns are recovered from the nearest line above the rule,
// which is a table's only header line today; a wrapped header would put its
// second line there, naming only the wrapped columns' inner left edges, and
// columnBound would silently loosen over the columns that line does not name.
// Every row of the table draws a cell at every column's inner left edge, so a
// table whose widest row names more columns than its header line does fails
// here, loudly, instead.
func checkHeaderSpansTheColumns(tb testing.TB, lines []pdfRunLine, tables []pdfPageTable, lowest int) {
	tb.Helper()

	for i, table := range tables {
		// The table's rows lie below its rule and above the next table's header
		// row, which is drawn above that table's rule, and above the page's own
		// footer below the lowest rule.
		floor := lowest
		if i+1 < len(tables) {
			floor = max(floor, tables[i+1].headerY)
		}

		widest := 0

		for _, line := range lines {
			if line.Y >= table.rule.Y || line.Y <= floor {
				continue
			}

			widest = max(widest, lineColumns(line, table.size))
		}

		if widest > len(table.columns) {
			tb.Fatalf("pdf: the table whose rule is at y %d pt draws %d columns, but its header line names %d: a header cell wrapped onto a second line, so that line alone does not name every column's inner left edge",
				table.rule.Y, widest, len(table.columns))
		}
	}
}

// cellGap is how far past the end of the run before it, in thousandths of a
// point, a run may start and still continue the same cell. A cell's pieces are
// drawn one after another, each rounded up to a whole point, so a piece of the
// same cell starts less than a point past the end of the one before it; the
// report's own words are measured in Courier's advance here, which the metrics
// of Helvetica, their font, may miss by more than that, so the gap allows for
// two points. A cell's text ends at least one cell padding before the next
// column's inner left edge, where the next column's cell begins, which is two
// paddings, six points, away.
const cellGap = 2 * milli

// lineColumns returns the number of table columns a line draws a cell in: its
// runs of the table's size, every run that does not continue the run before it
// on the line starting a cell of its own.
func lineColumns(line pdfRunLine, size int) int {
	columns := 0

	for i, run := range line.runs {
		if run.Size != size {
			continue
		}

		if i == 0 || run.X*milli-valueRight(line.runs[i-1]) >= cellGap {
			columns++
		}
	}

	return columns
}

// headerLineAbove returns the header row of the table whose rule is at ruleY: the
// nearest line above the rule, when its baseline is close enough above it.
func headerLineAbove(lines []pdfRunLine, ruleY int) (pdfRunLine, bool) {
	best, found := pdfRunLine{}, false

	for _, line := range lines {
		if line.Y <= ruleY || line.Y-ruleY > headerRuleGap {
			continue
		}

		if !found || line.Y < best.Y {
			best, found = line, true
		}
	}

	return best, found
}

// tableAt returns the table a line of the page at y belongs to: the table whose
// header row it is, or else the table whose rule sits above it, the nearest one
// when several do.
func tableAt(tables []pdfPageTable, y int) (pdfPageTable, bool) {
	for _, table := range tables {
		if table.headerY == y {
			return table, true
		}
	}

	best, found := pdfPageTable{}, false

	for _, table := range tables {
		if table.rule.Y <= y {
			continue
		}

		if !found || table.rule.Y < best.rule.Y {
			best, found = table, true
		}
	}

	return best, found
}

// columnBound returns the right edge of the table column that holds x, in points:
// the next column's inner left edge less the cell padding, which is where the
// layout stops a cell's text, or the table's right edge less the padding for the
// last column. A cell is laid out width-2*cellPad wide from its column's inner
// left edge (layoutCell), so its last point sits one padding inside the next
// column's inner left edge, which is itself one padding inside the column's
// outer edge.
func (t pdfPageTable) columnBound(x int) int {
	right := t.rule.X + t.rule.W

	if len(t.columns) == 0 {
		return right
	}

	pad := t.columns[0] - t.rule.X

	for i := len(t.columns) - 1; i >= 0; i-- {
		if x < t.columns[i] {
			continue
		}

		if i+1 < len(t.columns) {
			return t.columns[i+1] - 2*pad
		}

		return right - pad
	}

	return right - pad
}

// valueRight returns where the run's text ends, in thousandths of a point.
func valueRight(run pdfTextRun) int {
	return run.X*milli + courierAdvance*run.Size*utf8.RuneCountInString(run.Text)
}

// milli is the number of thousandths of a point in a point, the unit the widths
// are measured in (ADR 0049).
const milli = 1000

// points renders thousandths of a point for a failure message.
func points(milli int) string { return fmt.Sprintf("%d.%03d", milli/1000, milli%1000) }

// shortened quotes a run's text for a failure message, cut short when it is long.
func shortened(text string) string {
	const limit = 40

	if utf8.RuneCountInString(text) <= limit {
		return fmt.Sprintf("%q", text)
	}

	cut := 0
	for i := range text {
		if cut == limit {
			return fmt.Sprintf("%q...", text[:i])
		}

		cut++
	}

	return fmt.Sprintf("%q", text)
}

// checkPageRunsStayInsideTheirBoxes checks one page's values: every value drawn
// in a table cell ends inside its column, and every value ends inside the
// content area. Only values are measured: the report draws every value in
// Courier, whose characters all have the same advance, while the reader carries
// no metrics for the report's own words, and the wrap that can run past a column
// is the value's.
func checkPageRunsStayInsideTheirBoxes(tb testing.TB, pageIndex int, page *pdfPage) {
	tb.Helper()

	lowest, highest, ok := pageBand(page)
	if !ok {
		return
	}

	tables := pageTables(tb, page)

	contentRight := 0
	for _, rule := range page.rules {
		contentRight = max(contentRight, rule.X+rule.W)
	}

	for _, line := range pageRunLines(page) {
		if line.Y <= lowest || line.Y >= highest {
			// The page's furniture: its header above the topmost rule and its
			// footer below the bottommost one.
			continue
		}

		table, inTable := tableAt(tables, line.Y)

		for _, run := range line.runs {
			if run.Font != "F3" {
				continue
			}

			bound, box := contentRight, "the content area"
			if inTable && run.Size == table.size {
				bound, box = table.columnBound(run.X), fmt.Sprintf("the column from x %d pt of the table whose rule is at y %d pt",
					table.columns[0], table.rule.Y)
			}

			if right := valueRight(run); right > bound*milli {
				tb.Errorf("pdf: page %d draws the value %s at x %d pt in the %d pt font, which ends at %s pt, past the right edge of %s at %s pt: a wrapped value ran past its box and overlaps what is next to it",
					pageIndex+1, shortened(run.Text), run.X, run.Size, points(right), box, points(bound*milli))
			}
		}
	}
}

// headingSizes are the type sizes the writer draws a heading at: a level-1
// heading at 13 pt, a level-2 heading at 11 and a level-3 heading at 10
// (layout.go's sectionSize, subsectionSize and blockSize). Every other line of
// a report is smaller: body text at 9 pt, a table cell and the page furniture at
// 8. The size alone therefore tells a heading line from a line of text, whatever
// font it is drawn in, which matters because a heading made of a value is drawn
// in Courier and a section's own title in Helvetica-Bold.
var headingSizes = map[int]bool{13: true, 11: true, 10: true}

// checkNoHeadingEndsAPage checks the PRD's rule that a heading never ends a page
// (Tables and page breaks): the last line of a page's body is text, never a
// heading. A page's furniture, its header above the top rule and its footer below
// the bottom rule, is no body, so it is left out.
func checkNoHeadingEndsAPage(tb testing.TB, file *testPDF) {
	tb.Helper()

	for index, page := range file.pages {
		checkPageDoesNotEndWithAHeading(tb, index, page)
	}
}

// checkPageDoesNotEndWithAHeading fails the test when the lowest body line of one
// page is a heading.
func checkPageDoesNotEndWithAHeading(tb testing.TB, pageIndex int, page *pdfPage) {
	tb.Helper()

	last, found := lowestBodyLine(page)
	if !found || !isHeadingLine(last) {
		return
	}

	tb.Errorf("pdf: page %d ends its body with the heading %s at y %d pt, so the heading alone sits at the foot of the page: a heading never ends a page (PRD, Tables and page breaks)",
		pageIndex+1, shortened(lineText(last)), last.Y)
}

// lowestBodyLine returns the page's lowest line inside its band, the last line of
// its body, and false for a page with no body line.
func lowestBodyLine(page *pdfPage) (pdfRunLine, bool) {
	lowest, highest, ok := pageBand(page)
	if !ok {
		return pdfRunLine{}, false
	}

	var (
		last  pdfRunLine
		found bool
	)

	for _, line := range pageRunLines(page) {
		if line.Y <= lowest || line.Y >= highest {
			// The page's furniture.
			continue
		}

		if !found || line.Y < last.Y {
			last, found = line, true
		}
	}

	return last, found
}

// boxMinHeight is how tall a filled shape must be to be a box rather than a
// badge, a table row's fill, a block's ID band or a rule, in thousandths of a
// point. A badge is one line of text tall (PRD, Look, Fit badges).
const boxMinHeight = 24 * milli

// boxSlack is how far a point may lie outside a box and still belong to it, in
// thousandths of a point: a fill's edge and a line's baseline are computed
// separately, so a line sits a point or so inside or outside an edge.
const boxSlack = milli

// boxFills returns the page's boxes: the filled shapes too tall to be a badge, a
// table row's fill, a block's ID band or a rule (PRD, Look, Boxes across pages).
func boxFills(page *pdfPage) []pdfShape {
	boxes := make([]pdfShape, 0, len(page.shapes))

	for _, shape := range page.shapes {
		if shape.Op == "f" && shape.Box.H > boxMinHeight {
			boxes = append(boxes, shape)
		}
	}

	return boxes
}

// lineStartsInside reports whether the line's leftmost run starts inside the box:
// a line of a box begins at the box's padding.
func lineStartsInside(line pdfRunLine, box pdfShape) bool {
	if len(line.runs) == 0 {
		return false
	}

	left := line.runs[0]
	for _, run := range line.runs {
		if run.X < left.X {
			left = run
		}
	}

	return box.Box.contains(pdfPoint{X: left.X * milli, Y: left.Y * milli}, boxSlack)
}

// checkNoBoxHeaderEndsAPage checks the PRD's rule that a box's header never ends
// a page (Look, Boxes across pages): a box whose heading is the last line of a
// page's body would be a heading alone at the foot, with its first row on the
// next page, and a table's header row is a box's header too.
func checkNoBoxHeaderEndsAPage(tb testing.TB, file *testPDF) {
	tb.Helper()

	for index, page := range file.pages {
		checkPageDoesNotEndWithABoxHeader(tb, index, page)
	}
}

// checkPageDoesNotEndWithABoxHeader fails the test when the lowest body line of
// one page is a box's first line or a table's header row.
func checkPageDoesNotEndWithABoxHeader(tb testing.TB, pageIndex int, page *pdfPage) {
	tb.Helper()

	last, found := lowestBodyLine(page)
	if !found {
		return
	}

	tables := pageTables(tb, page)
	if table, inTable := tableAt(tables, last.Y); inTable && table.headerY == last.Y {
		tb.Errorf("pdf: page %d ends its body with the header row %s of the table whose rule is at y %d pt, so the header row alone sits at the foot of the page: a box's header never ends a page (PRD, Look, Boxes across pages)",
			pageIndex+1, shortened(lineText(last)), table.rule.Y)

		return
	}

	for _, box := range boxFills(page) {
		first, ok := highestLineInside(page, box)
		if !ok || first.Y != last.Y {
			continue
		}

		tb.Errorf("pdf: page %d ends its body with the line %s at y %d pt, which is the first line of the %s box (x %s pt, y %s pt, %s × %s pt): the box's header alone sits at the foot of the page, and a box's header never ends a page (PRD, Look, Boxes across pages)",
			pageIndex+1, shortened(lineText(first)), first.Y, colourName(box.Fill),
			points(box.Box.X), points(box.Box.Y), points(box.Box.W), points(box.Box.H))

		return
	}
}

// highestLineInside returns the highest line of the page that starts inside the
// box: its first line.
func highestLineInside(page *pdfPage, box pdfShape) (pdfRunLine, bool) {
	var (
		first pdfRunLine
		found bool
	)

	for _, line := range pageRunLines(page) {
		if !lineStartsInside(line, box) {
			continue
		}

		if !found || line.Y > first.Y {
			first, found = line, true
		}
	}

	return first, found
}

// checkColoursComeFromThePalette checks that every colour a content stream sets
// with rg or RG is one of the palette's rows, written exactly as the palette
// writes it (PRD, Look: the colours are the site's light-theme tokens, nothing
// else).
func checkColoursComeFromThePalette(tb testing.TB, file *testPDF) {
	tb.Helper()

	for index, page := range file.pages {
		for _, colour := range page.colors {
			if _, ok := paletteName(colour); ok {
				continue
			}

			tb.Errorf("pdf: page %d sets the colour %s %s %s, which is no row of the palette: the report's colours are the site's light-theme tokens, nothing else (PRD, Look)",
				index+1, points(colour.R), points(colour.G), points(colour.B))
		}
	}
}

// isHeadingLine reports whether a line is a heading: every run of it is drawn at
// one of the heading sizes.
func isHeadingLine(line pdfRunLine) bool {
	if len(line.runs) == 0 {
		return false
	}

	for _, run := range line.runs {
		if !headingSizes[run.Size] {
			return false
		}
	}

	return true
}

// lineText returns the text of a line, its runs in x order.
func lineText(line pdfRunLine) string {
	runs := slices.Clone(line.runs)
	slices.SortStableFunc(runs, func(a, b pdfTextRun) int { return a.X - b.X })

	texts := make([]string, 0, len(runs))
	for _, run := range runs {
		texts = append(texts, run.Text)
	}

	return strings.Join(texts, "")
}

// The sweeps of the heading-near-the-page-bottom fixtures. One more padded
// resource moves the sections under the resources table down one table line (13
// pt), and one more line of the inventory file name moves the sections under the
// facts block down one body line (12 pt). A sweep of either walks a heading
// through every position in a page's last sixty points several times over.
const (
	resourceSweepCount = 60
	nameSweepCount     = 20
)

// paddedNameStart and paddedNameStep are the inventory file name lengths the table
// sweep tries: a step of 91 characters is one more line of the name in Courier's
// advance, and the sweep starts where that line count first pushes the Resources
// heading down towards the foot of the first page.
const (
	paddedNameStart = 3400
	paddedNameStep  = 91
)

// TestNoHeadingEndsAPage pins the PRD's rule that a heading never ends a page
// (Tables and page breaks) on reports built to leave one there. The writer
// reserves keepWithHeading (36 pt) under a heading, and what follows may need
// more:
//
//   - a level-2 heading asks for 8 pt of space and, with its own 36 pt, 50 pt of
//     the page, which is what a comparison's level-1 target heading is followed
//     by;
//   - a table asks for its header row and the lines of its first row, up to three
//     of them (43 pt), as the resources table does when the first resource's ID
//     wraps onto three lines.
//
// A heading that lands in that gap is drawn at the foot of the page and its block
// starts the next one. Each fixture sweeps a step that moves the heading down the
// page a little, so at least one step puts a heading in the gap, and the invariant
// has to catch it.
func TestNoHeadingEndsAPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		sweep  int
		render func(tb testing.TB, step int) []byte
	}{
		{
			name:  "a Workloads heading followed by a workload heading",
			sweep: resourceSweepCount,
			render: func(tb testing.TB, step int) []byte {
				tb.Helper()

				a := assessment.Assess(newInventory(tb, sourceName, paddedResources(step)), testutil.ReportCatalog(tb))

				return renderReport(tb, a, analysis.ReportMeta{InventoryName: inventoryFileName})
			},
		},
		{
			name:  "a comparison's target heading followed by the level-2 Summary",
			sweep: resourceSweepCount,
			render: func(tb testing.TB, step int) []byte {
				tb.Helper()

				inv := newInventory(tb, sourceName, paddedResources(step))
				c := assessment.Compare(assessTargets(tb, inv, []string{ovhcloudKey, scalewayKey}))

				return renderComparison(tb, c, analysis.ReportMeta{InventoryName: inventoryFileName})
			},
		},
		{
			name:  "a Resources heading followed by a table whose first row wraps onto three lines",
			sweep: nameSweepCount,
			render: func(tb testing.TB, step int) []byte {
				tb.Helper()

				a := assessment.Assess(newInventory(tb, sourceName, deepRowResources()), testutil.ReportCatalog(tb))
				name := paddedInventoryName(paddedNameStart + paddedNameStep*step)

				return renderReport(tb, a, analysis.ReportMeta{InventoryName: name})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			atBoundary := 0

			for step := 1; step <= test.sweep; step++ {
				t.Logf("sweep step %d", step)

				atBoundary += countHeadingsAtAPageBoundary(checkPDFStructure(t, test.render(t, step)))
			}

			if atBoundary == 0 {
				t.Fatalf("no heading of the %d reports of the sweep sits within %d pt of a page's own rule, so the sweep never reached the gap the invariant is about",
					test.sweep, headingBand)
			}
		})
	}
}

// headingBand is how far from a page's own rule a heading may sit and still be
// the one the sweep is looking for. A page's body ends bodyGap + ruleGap above
// its bottom rule and starts bodyGap + ruleGap below its top rule, so a heading
// that ends a page, or that a kept heading moves to the next page's top, sits
// within fifty points of one of them.
const headingBand = 50

// countHeadingsAtAPageBoundary returns how many heading lines of the document
// sit within headingBand of one of a page's own two rules, above the bottom one
// or below the top one: the positions from which a heading can end a page before
// it is kept, and to which a kept heading is moved after.
func countHeadingsAtAPageBoundary(file *testPDF) int {
	found := 0

	for _, page := range file.pages {
		lowest, highest, ok := pageBand(page)
		if !ok {
			continue
		}

		for _, line := range pageRunLines(page) {
			if line.Y <= lowest || line.Y >= highest || !isHeadingLine(line) {
				continue
			}

			if line.Y-headingBand <= lowest || line.Y+headingBand >= highest {
				found++
			}
		}
	}

	return found
}

// TestReportValueWrapsInsideItsColumn pins a value's wrap against the narrowest
// column a report has, the resources table's ID cell with estimates on: a value
// of a short word, a space and escapes six characters wide wraps between its
// escapes, and every line still ends inside the column, so no line of it runs
// into the next column.
func TestReportValueWrapsInsideItsColumn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "a space after a short word, then 60 escapes of six characters", value: "a " + strings.Repeat("\u0416", 60)},
		{name: "a space before a full line's worth of characters, then an escape", value: "x " + strings.Repeat("a", 14) + strings.Repeat("\u0416", 20)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			inv := newInventory(t, sourceName, []inventory.Resource{{
				ID:           test.value,
				RawType:      "AWS::EC2::Instance",
				Category:     taxonomy.ComputeVM,
				Region:       "eu-west-1",
				Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			}})

			a := assessment.AssessWith(inv, testutil.ReportCatalog(t), assessment.Options{Estimate: true})
			file := openTestPDF(t, renderReport(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

			wrapped := checkWrappedValue(t, file, test.value)
			if len(wrapped.chunks) < 2 {
				t.Fatalf("the value is drawn on %d line, so it never reached the wrap width of its column", len(wrapped.chunks))
			}

			checkValueInsideItsColumn(t, file, test.value)
		})
	}
}

// checkValueInsideItsColumn checks that every line of the drawing of value that
// the report draws in a table cell ends inside that cell's column.
func checkValueInsideItsColumn(tb testing.TB, file *testPDF, value string) {
	tb.Helper()

	escaped := pdfreport.EscapeValue(value)
	checked := 0

	for index, page := range file.pages {
		tables := pageTables(tb, page)

		for _, line := range pageRunLines(page) {
			table, inTable := tableAt(tables, line.Y)

			for _, run := range line.runs {
				if run.Font != "F3" || !inTable || run.Size != table.size {
					continue
				}

				if run.Text == "" || !strings.Contains(escaped, run.Text) {
					continue
				}

				checked++

				if right, bound := valueRight(run), table.columnBound(run.X); right > bound*milli {
					tb.Errorf("pdf: page %d draws the value's line %s at x %d pt, which ends at %s pt, past its column's right edge at %s pt",
						index+1, shortened(run.Text), run.X, points(right), points(bound*milli))
				}
			}
		}
	}

	if checked == 0 {
		tb.Fatalf("the report draws no table cell holding the value %q", value)
	}
}

// operatorWords names the operand kinds as ADR 0049's table words them.
var operatorWords = map[pdfValueKind]string{
	pdfValueInteger: "integer",
	pdfValueDecimal: "decimal",
	pdfValueName:    "name",
	pdfValueString:  "string",
}

// TestContentStreamOperatorTableIsTheADRs pins the operator table's keys and
// operand shapes against ADR 0049's amended set, which is exactly what the writer
// may emit: sixteen operators, no more and no fewer, and the two number kinds
// kept apart.
//
// Criterion (PRD #584 milestone 1b, Unit): the test-only reader's operator table
// is exactly ADR 0049's amended set, each with its operand shape, and nothing
// else: BT, ET, q, Q, f, S and h with no operand; Tf with a font resource name
// and an integer; Tm with six integers; Tj with a literal string; re with four
// integers; w with one decimal; rg and RG with three decimals; m and l with two
// decimals. G, milestone 1's grey operator, is gone.
func TestContentStreamOperatorTableIsTheADRs(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"q":  "",
		"Q":  "",
		"h":  "",
		"f":  "",
		"S":  "",
		"BT": "",
		"ET": "",
		"w":  "decimal",
		"rg": "decimal decimal decimal",
		"RG": "decimal decimal decimal",
		"re": "integer integer integer integer",
		"m":  "decimal decimal",
		"l":  "decimal decimal",
		"Tf": "name integer",
		"Tm": "integer integer integer integer integer integer",
		"Tj": "string",
	}

	if len(pdfContentOperators) != len(want) {
		keys := make([]string, 0, len(pdfContentOperators))
		for operator := range pdfContentOperators {
			keys = append(keys, operator)
		}

		slices.Sort(keys)

		t.Fatalf("the operator table holds %d operators (%s), want ADR 0049's sixteen",
			len(pdfContentOperators), strings.Join(keys, " "))
	}

	for operator, shape := range pdfContentOperators {
		wantShape, ok := want[operator]
		if !ok {
			t.Errorf("the operator table holds %q, which ADR 0049's amended set does not", operator)

			continue
		}

		words := make([]string, 0, len(shape))
		for _, kind := range shape {
			words = append(words, operatorWords[kind])
		}

		if got := strings.Join(words, " "); got != wantShape {
			t.Errorf("the operator %s takes %q, want %q", operator, got, wantShape)
		}
	}
}

// TestReaderRejectsWhatTheOperatorTableDoesNotHold feeds hand-written content
// streams through the reader the invariants use and requires every one of them to
// fail: an operator outside the table, an operand of the wrong number or shape, a
// number without its written form, and a colour outside 0.000 to 1.000.
//
// Criterion (PRD #584 milestone 1b, Unit): a content stream built in-test with an
// operator outside the table fails the reader, for each of G, c, sh, gs, Do, BI
// and TJ; so does a listed operator with an operand of the wrong shape (an integer
// for rg, a decimal for re, a missing or an extra operand), a signed or exponent
// number, and a colour component above 1.000.
func TestReaderRejectsWhatTheOperatorTableDoesNotHold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stream string
	}{
		{name: "the unlisted operator Do", stream: "q Do Q"},
		{name: "the unlisted operator sh", stream: "q sh Q"},
		{name: "the unlisted operator gs", stream: "q gs Q"},
		{name: "milestone 1's grey operator G", stream: "q 0.500 G Q"},
		{name: "the curve operator c", stream: "q 0.000 0.000 m 1.000 1.000 2.000 2.000 3.000 3.000 c S Q"},
		{name: "the unlisted text operator TJ", stream: "BT /F1 8 Tf 1 0 0 1 50 700 Tm (a) TJ ET"},
		{name: "the inline image operator BI", stream: "q BI Q"},
		{name: "the clipping operator W", stream: "q 10 10 100 100 re W n Q"},
		{name: "a missing operand of rg", stream: "q 0.500 0.500 rg Q"},
		{name: "an extra operand of rg", stream: "q 0.500 0.500 0.500 0.500 rg Q"},
		{name: "a missing operand of m", stream: "q 1.000 m 2.000 3.000 l S Q"},
		{name: "an integer for rg's decimal", stream: "q 1 1 1 rg Q"},
		{name: "a decimal for re's integer", stream: "q 10.000 20.000 30.000 40.000 re f Q"},
		{name: "a signed number", stream: "q -1.000 0.500 0.500 rg Q"},
		{name: "a number with an exponent", stream: "q 1e3 0.500 0.500 rg Q"},
		{name: "a decimal of two places", stream: "q 0.50 0.500 0.500 rg Q"},
		{name: "a colour component above 1.000", stream: "q 1.001 0.500 0.500 rg Q"},
		{name: "a stroking colour component above 1.000", stream: "q 0.500 0.500 1.001 RG Q"},
		{name: "a path coordinate beyond the page", stream: "q 596.000 20.000 m 30.000 40.000 l S Q"},
		{name: "a font outside F1 to F3", stream: "BT /F4 8 Tf ET"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := checkContentStream(test.stream); err == nil {
				t.Errorf("the reader accepted the content stream %q, want it refused", test.stream)
			} else {
				t.Logf("refused: %v", err)
			}
		})
	}
}

// TestReaderAcceptsEveryOperatorOfTheTable is the positive case: a content stream
// holding all sixteen operators, each with the operands its row gives, passes, and
// the reader reads what it draws — the colour of a fill and of a stroke, a path's
// subpaths, a rectangle's box and the colour a run is drawn in.
//
// Criterion (PRD #584 milestone 1b, Unit): one positive case with all sixteen
// operators passes.
func TestReaderAcceptsEveryOperatorOfTheTable(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		"q",
		"0.500 w",
		"0.078 0.129 0.239 RG",
		"0.078 0.129 0.239 rg",
		"50 100 495 0 re",
		"S",
		"50 100 495 20 re",
		"f",
		"50.000 100.000 m",
		"60.000 110.000 l",
		"70.000 100.000 l",
		"h",
		"S",
		"BT",
		"/F1 9 Tf",
		"1 0 0 1 50 700 Tm",
		"(hello) Tj",
		"ET",
		"Q",
	}, "\n")

	if err := checkContentStream(stream); err != nil {
		t.Fatalf("the reader refused a content stream holding every operator of the table: %v\n%s", err, stream)
	}

	// The reader parses a stream it accepts, and fails the test on a departure, so
	// the drawing it reads is the one the checks of this package measure.
	file := openTestPDF(t, buildPagePDF(t, stream))

	if len(file.pages) != 1 {
		t.Fatalf("the file has %d pages, want the one its content stream draws", len(file.pages))
	}

	page := file.pages[0]

	if len(page.colors) != 2 {
		t.Fatalf("the page sets %d colours, want the two the stream gives", len(page.colors))
	}

	ink := pdfRGB{78, 129, 239}
	if page.colors[0] != ink || page.colors[1] != ink {
		t.Errorf("the page's colours are %v, want the ink %v twice", page.colors, ink)
	}

	if len(page.shapes) != 3 {
		t.Fatalf("the page draws %d shapes, want the rule, the fill and the closed path", len(page.shapes))
	}

	if rule := page.shapes[0]; rule.Op != "S" || rule.Width != 500 || rule.Stroke != ink || rule.Box != (pdfBox{X: 50000, Y: 100000, W: 495000, H: 0}) {
		t.Errorf("the first shape is %+v, want the 0.5 pt ink rule from (50, 100) pt, 495 pt long", rule)
	}

	if fill := page.shapes[1]; fill.Op != "f" || fill.Fill != ink || fill.Box.H != 20000 {
		t.Errorf("the second shape is %+v, want the 20 pt tall ink fill", fill)
	}

	if icon := page.shapes[2]; icon.Op != "S" || len(icon.Subpaths) != 1 || !icon.Subpaths[0].Closed || len(icon.Subpaths[0].Points) != 3 {
		t.Errorf("the third shape is %+v, want the closed three-point path", icon)
	}

	if len(page.runs) != 1 {
		t.Fatalf("the page draws %d runs, want the one Tj", len(page.runs))
	}

	if run := page.runs[0]; run.Text != "hello" || run.Colour != ink || run.Font != "F1" || run.Size != 9 {
		t.Errorf("the run is %+v, want %q in the ink colour, /F1 at 9 pt", run, "hello")
	}
}
