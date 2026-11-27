package pdfreport_test

// This file is the tests' own reader for the PDF subset the reporter writes: the
// header, the cross-reference table, the trailer, the objects, the page tree and
// the content streams. It is written against ISO 32000-1 only, never against the
// writer's code, so a change in the writer's output has to be read the way a PDF
// reader reads it.
//
// It parses the writer's fixed set and fails the test on anything outside it: an
// object that is not a catalog, page tree, page, font, content stream or info
// dictionary; a name with a '#'; a keyword, boolean, null, hex string or array
// where the writer writes an integer, a literal string or a name; a content
// stream operator outside the writer's table; a literal string escape the writer
// never writes; a raw byte outside 0x20 to 0x7E in a literal string; a
// floating-point number.
//
// Text is decoded from WinAnsiEncoding (ISO 32000-1, Annex D.2) to UTF-8, so the
// tests compare what a PDF reader shows with the escaper's ASCII form.

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	// pdfMagic is the header every file the writer emits starts with, and the
	// version the PRD fixes (Decided, as recommended, 4).
	pdfMagic = "%PDF-1.7\n"
	// pdfEOFMarker is how every file the writer emits ends.
	pdfEOFMarker = "%%EOF\n"
)

// pdfValueKind is the kind of one PDF value of the writer's subset.
type pdfValueKind int

const (
	pdfValueNumber pdfValueKind = iota
	pdfValueName
	pdfValueString
	pdfValueRef
	pdfValueArray
	pdfValueDict
)

// kindName is the kind as an error message names it.
func kindName(kind pdfValueKind) string {
	switch kind {
	case pdfValueNumber:
		return "an integer"
	case pdfValueName:
		return "a name"
	case pdfValueString:
		return "a literal string"
	case pdfValueRef:
		return "an indirect reference"
	case pdfValueArray:
		return "an array"
	case pdfValueDict:
		return "a dictionary"
	default:
		return "an unknown kind"
	}
}

// pdfValue is one value: a number, a name, a literal string, an indirect
// reference, an array or a dictionary.
type pdfValue struct {
	kind  pdfValueKind
	num   int
	name  string
	str   string
	ref   int
	array []pdfValue
	dict  pdfDict
}

// pdfDictEntry is one entry of a dictionary.
type pdfDictEntry struct {
	name  string
	value pdfValue
}

// pdfDict is a dictionary, in the order the writer wrote it.
type pdfDict struct {
	entries []pdfDictEntry
}

// get returns the value of the dictionary's entry name.
func (d pdfDict) get(name string) (pdfValue, bool) {
	for _, entry := range d.entries {
		if entry.name == name {
			return entry.value, true
		}
	}

	return pdfValue{}, false
}

// names returns the names of the dictionary's entries, sorted.
func (d pdfDict) names() []string {
	names := make([]string, 0, len(d.entries))
	for _, entry := range d.entries {
		names = append(names, entry.name)
	}

	slices.Sort(names)

	return names
}

// mustNumber returns the dictionary's entry name as an integer.
func (d pdfDict) mustNumber(tb testing.TB, owner, name string) int {
	tb.Helper()

	value, ok := d.get(name)
	if !ok {
		tb.Fatalf("pdf: %s has no /%s", owner, name)
	}

	if value.kind != pdfValueNumber {
		tb.Fatalf("pdf: %s's /%s is %s, want an integer", owner, name, kindName(value.kind))
	}

	return value.num
}

// mustName returns the dictionary's entry name as a name.
func (d pdfDict) mustName(tb testing.TB, owner, name string) string {
	tb.Helper()

	value, ok := d.get(name)
	if !ok {
		tb.Fatalf("pdf: %s has no /%s", owner, name)
	}

	if value.kind != pdfValueName {
		tb.Fatalf("pdf: %s's /%s is %s, want a name", owner, name, kindName(value.kind))
	}

	return value.name
}

// mustRef returns the dictionary's entry name as the number of an indirect
// reference.
func (d pdfDict) mustRef(tb testing.TB, owner, name string) int {
	tb.Helper()

	value, ok := d.get(name)
	if !ok {
		tb.Fatalf("pdf: %s has no /%s", owner, name)
	}

	if value.kind != pdfValueRef {
		tb.Fatalf("pdf: %s's /%s is %s, want an indirect reference", owner, name, kindName(value.kind))
	}

	return value.ref
}

// mustArray returns the dictionary's entry name as an array.
func (d pdfDict) mustArray(tb testing.TB, owner, name string) []pdfValue {
	tb.Helper()

	value, ok := d.get(name)
	if !ok {
		tb.Fatalf("pdf: %s has no /%s", owner, name)
	}

	if value.kind != pdfValueArray {
		tb.Fatalf("pdf: %s's /%s is %s, want an array", owner, name, kindName(value.kind))
	}

	return value.array
}

// pdfObject is one indirect object of the file.
type pdfObject struct {
	num       int
	dict      pdfDict
	stream    []byte
	hasStream bool
}

// typeName returns the object's /Type, if it has one.
func (o *pdfObject) typeName() (string, bool) {
	value, ok := o.dict.get("Type")
	if !ok || value.kind != pdfValueName {
		return "", false
	}

	return value.name, true
}

// pdfXrefEntry is one entry of the cross-reference table.
type pdfXrefEntry struct {
	offset int
}

// pdfTextRun is one Tj: the text it shows, at the x and y the text matrix had.
type pdfTextRun struct {
	// Page is the run's page, counted from 0 in the page tree's order.
	Page int
	// X and Y are the run's position, in points, as the writer drew it.
	X int
	Y int
	// Font is the font resource name the run was drawn in: F1, F2 or F3.
	Font string
	// Size is the type size the run was drawn at, in points.
	Size int
	// Text is the run's text, decoded from WinAnsiEncoding.
	Text string
}

// pdfRule is one rectangle the content stream fills or strokes: a horizontal
// hairline, from x, y, w points long and h points high.
type pdfRule struct {
	X, Y, W, H int
}

// pdfPage is one page of the page tree, the text it draws and its rules.
type pdfPage struct {
	num     int
	content int
	runs    []pdfTextRun
	rules   []pdfRule
}

// testPDF is the parsed file.
type testPDF struct {
	version    string
	xrefOffset int
	xref       []pdfXrefEntry
	trailer    pdfDict
	objects    map[int]*pdfObject
	order      []*pdfObject
	pages      []*pdfPage
}

// object returns the object with the number given.
func (f *testPDF) object(tb testing.TB, num int) *pdfObject {
	tb.Helper()

	obj, ok := f.objects[num]
	if !ok {
		tb.Fatalf("pdf: the file holds no object %d", num)
	}

	return obj
}

// pdfLexer reads the writer's subset out of bytes.
type pdfLexer struct {
	tb   testing.TB
	data []byte
	pos  int
}

// fail fails the test, naming the byte the lexer stopped at.
func (l *pdfLexer) fail(format string, args ...any) {
	l.tb.Helper()
	l.tb.Fatalf("pdf: at byte %d: %s", l.pos, fmt.Sprintf(format, args...))
}

// eof reports whether the lexer has read every byte.
func (l *pdfLexer) eof() bool { return l.pos >= len(l.data) }

// peek returns the next byte, or 0 at the end.
func (l *pdfLexer) peek() byte {
	if l.eof() {
		return 0
	}

	return l.data[l.pos]
}

// has reports whether the next bytes are prefix.
func (l *pdfLexer) has(prefix string) bool { return bytes.HasPrefix(l.data[l.pos:], []byte(prefix)) }

// expect consumes literal, or fails the test naming what was wanted.
func (l *pdfLexer) expect(what, literal string) {
	if !l.has(literal) {
		l.fail("want %s %q, found %q", what, literal, preview(l.data[l.pos:]))
	}

	l.pos += len(literal)
}

// skipSpace skips white space.
func (l *pdfLexer) skipSpace() {
	for !l.eof() && isPDFSpace(l.data[l.pos]) {
		l.pos++
	}
}

// isPDFSpace reports whether b is PDF white space.
func isPDFSpace(b byte) bool {
	switch b {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	default:
		return false
	}
}

// isPDFDelimiter reports whether b is a PDF delimiter.
func isPDFDelimiter(b byte) bool {
	switch b {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	default:
		return false
	}
}

// preview returns the start of data as a short quoted form for a failure.
func preview(data []byte) string {
	const n = 32

	if len(data) > n {
		return string(data[:n]) + "..."
	}

	return string(data)
}

// parseNumber reads an integer. The writer's layout arithmetic is integer only
// (ADR 0049), so a number with a fraction is a failure, not a value.
func (l *pdfLexer) parseNumber() int {
	start := l.pos

	if l.peek() == '-' || l.peek() == '+' {
		l.pos++
	}

	digits := l.pos
	for !l.eof() && l.data[l.pos] >= '0' && l.data[l.pos] <= '9' {
		l.pos++
	}

	if l.pos == digits {
		l.fail("want an integer, found %q", preview(l.data[start:]))
	}

	if !l.eof() && (l.data[l.pos] == '.' || l.data[l.pos] == 'e' || l.data[l.pos] == 'E') {
		l.fail("want an integer, but the number %q holds a fraction or an exponent; the writer's layout arithmetic is integer only",
			preview(l.data[start:]))
	}

	number, err := strconv.Atoi(string(l.data[start:l.pos]))
	if err != nil {
		l.fail("want an integer, found %q: %v", string(l.data[start:l.pos]), err)
	}

	return number
}

// parseName reads a name and fails the test on a '#', which the writer never
// emits: a name with an escape could spell another name (/J#53 is /JS).
func (l *pdfLexer) parseName() string {
	l.expect("a name", "/")

	start := l.pos
	for !l.eof() && !isPDFDelimiter(l.data[l.pos]) && !isPDFSpace(l.data[l.pos]) {
		l.pos++
	}

	if l.pos == start {
		l.fail("want a name, found an empty one")
	}

	raw := string(l.data[start:l.pos])
	if strings.Contains(raw, "#") {
		l.fail("the name /%s holds a '#', but every name the writer emits is a constant of its own with no '#'", raw)
	}

	return raw
}

// parseLiteralString reads a literal string, undoing the escapes the writer
// writes: \(, \), \\ and a three-digit octal escape for every byte outside 0x20
// to 0x7E. Any other escape is a failure: it would mean the writer wrote a value
// that spells PDF syntax as syntax.
func (l *pdfLexer) parseLiteralString() string {
	l.expect("a literal string", "(")

	var text strings.Builder

	depth := 1

	for {
		if l.eof() {
			l.fail("want the end of a literal string, found the end of the file")
		}

		c := l.data[l.pos]
		l.pos++

		switch c {
		case '(':
			depth++
			text.WriteByte(c)
		case ')':
			depth--
			if depth == 0 {
				return text.String()
			}

			text.WriteByte(c)
		case '\\':
			l.parseEscape(&text)
		default:
			if c < 0x20 || c > 0x7E {
				l.fail("a literal string holds the raw byte %#02x; every byte outside 0x20 to 0x7E is written as a three-digit octal escape", c)
			}

			text.WriteByte(c)
		}
	}
}

// parseEscape reads the escape after a backslash inside a literal string.
func (l *pdfLexer) parseEscape(text *strings.Builder) {
	if l.eof() {
		l.fail("want an escape, found the end of the file after a backslash")
	}

	c := l.data[l.pos]
	l.pos++

	switch c {
	case 'n':
		text.WriteByte('\n')
	case 'r':
		text.WriteByte('\r')
	case 't':
		text.WriteByte('\t')
	case 'b':
		text.WriteByte('\b')
	case 'f':
		text.WriteByte('\f')
	case '(', ')', '\\':
		text.WriteByte(c)
	case '\n':
		// A line continuation, which the writer never needs.
	case '0', '1', '2', '3', '4', '5', '6', '7':
		value := int(c - '0')

		for range 2 {
			if l.eof() || l.data[l.pos] < '0' || l.data[l.pos] > '7' {
				break
			}

			value = value*8 + int(l.data[l.pos]-'0')
			l.pos++
		}

		if value > 0xFF {
			l.fail("a literal string holds the octal escape \\%o, which is not a byte", value)
		}

		text.WriteByte(byte(value)) //nolint:gosec // the range check above keeps value within a byte
	default:
		l.fail("a literal string holds the escape \\%c, which the writer never writes: a value's own escape such as \\u0416 is written as \\\\u0416", c)
	}
}

// parseValue reads the writer's next value.
func (l *pdfLexer) parseValue() pdfValue {
	l.skipSpace()

	if l.eof() {
		l.fail("want a value, found the end of the file")
	}

	c := l.data[l.pos]

	switch {
	case c == '/':
		return pdfValue{kind: pdfValueName, name: l.parseName()}
	case c == '(':
		return pdfValue{kind: pdfValueString, str: l.parseLiteralString()}
	case c == '[':
		return pdfValue{kind: pdfValueArray, array: l.parseArray()}
	case c == '<':
		return pdfValue{kind: pdfValueDict, dict: l.parseDict()}
	case c == '-' || c == '+' || (c >= '0' && c <= '9'):
		number := l.parseNumber()

		save := l.pos
		l.skipSpace()

		if !l.eof() && l.data[l.pos] >= '0' && l.data[l.pos] <= '9' {
			l.parseNumber()
			l.skipSpace()

			if !l.eof() && l.data[l.pos] == 'R' {
				l.pos++

				return pdfValue{kind: pdfValueRef, ref: number}
			}
		}

		l.pos = save

		return pdfValue{kind: pdfValueNumber, num: number}
	default:
		l.fail("want a value the writer emits, found %q; it writes no keyword, boolean, null or hex string", preview(l.data[l.pos:]))
	}

	return pdfValue{}
}

// parseArray reads an array.
func (l *pdfLexer) parseArray() []pdfValue {
	l.expect("an array", "[")

	var array []pdfValue

	for {
		l.skipSpace()

		if l.has("]") {
			l.pos++

			return array
		}

		array = append(array, l.parseValue())
	}
}

// parseDict reads a dictionary, keeping its entries in the order written.
func (l *pdfLexer) parseDict() pdfDict {
	l.expect("a dictionary", "<<")

	var dict pdfDict

	for {
		l.skipSpace()

		if l.has(">>") {
			l.pos += 2

			return dict
		}

		name := l.parseName()
		dict.entries = append(dict.entries, pdfDictEntry{name: name, value: l.parseValue()})
	}
}

// openTestPDF parses the whole file and returns it. It fails the test on any
// departure from the writer's subset or from the structural rules of the PRD.
func openTestPDF(tb testing.TB, data []byte) *testPDF {
	tb.Helper()

	if !bytes.HasPrefix(data, []byte(pdfMagic)) {
		tb.Fatalf("pdf: the file starts with %q, want the header %q", preview(data), pdfMagic)
	}

	if !bytes.HasSuffix(data, []byte(pdfEOFMarker)) {
		last := data
		if len(last) > len(pdfEOFMarker)+16 {
			last = last[len(last)-(len(pdfEOFMarker)+16):]
		}

		tb.Fatalf("pdf: the file ends with %q, want %q", last, pdfEOFMarker)
	}

	file := &testPDF{version: "1.7", objects: make(map[int]*pdfObject)}

	at := bytes.LastIndex(data, []byte("startxref\n"))
	if at < 0 {
		tb.Fatalf("pdf: the file has no startxref")
	}

	lexer := &pdfLexer{tb: tb, data: data, pos: at + len("startxref\n")}
	file.xrefOffset = lexer.parseNumber()
	lexer.expect("the start of the end of the file", "\n%%EOF\n")

	if lexer.pos != len(data) {
		lexer.fail("the file holds %d bytes after its end-of-file marker", len(data)-lexer.pos)
	}

	parseXref(tb, file, data)
	parseObjects(tb, file, data)
	parsePageTree(tb, file)

	return file
}

// parseXref parses the cross-reference table and the trailer, and checks the
// entries' shape and the trailer's /Size and /Root.
func parseXref(tb testing.TB, file *testPDF, data []byte) {
	tb.Helper()

	if file.xrefOffset <= 0 || file.xrefOffset >= len(data) {
		tb.Fatalf("pdf: startxref names byte %d, which is outside the file", file.xrefOffset)
	}

	lexer := &pdfLexer{tb: tb, data: data, pos: file.xrefOffset}
	lexer.expect("the cross-reference table", "xref\n")

	if first := lexer.parseNumber(); first != 0 {
		lexer.fail("the cross-reference table starts at object %d, want 0", first)
	}

	lexer.skipSpace()

	count := lexer.parseNumber()
	lexer.expect("the cross-reference table's object count", "\n")

	for i := range count {
		if lexer.pos+20 > len(data) {
			lexer.fail("the cross-reference entry %d is cut off", i)
		}

		raw := data[lexer.pos : lexer.pos+20]
		file.xref = append(file.xref, parseXrefEntry(tb, i, raw))
		lexer.pos += 20
	}

	lexer.expect("the trailer", "trailer\n")

	trailer := lexer.parseValue()
	if trailer.kind != pdfValueDict {
		lexer.fail("the trailer is %s, want a dictionary", kindName(trailer.kind))
	}

	file.trailer = trailer.dict

	lexer.expect("the final cross-reference offset", "\nstartxref\n")

	if got := lexer.parseNumber(); got != file.xrefOffset {
		lexer.fail("startxref names byte %d, want the cross-reference table's offset %d", got, file.xrefOffset)
	}

	lexer.expect("the end of the file", "\n%%EOF\n")

	if size := file.trailer.mustNumber(tb, "the trailer", "Size"); size != len(file.xref) {
		tb.Fatalf("pdf: the trailer's /Size is %d, want the cross-reference table's count %d", size, len(file.xref))
	}
}

// parseXrefEntry parses one cross-reference entry, which the PRD pins to
// exactly 20 bytes: ten offset digits, a space, five generation digits, a space,
// 'n' or 'f', and the two-byte end of line SP LF.
func parseXrefEntry(tb testing.TB, index int, raw []byte) pdfXrefEntry {
	tb.Helper()

	digits := func(b []byte) bool {
		for _, c := range b {
			if c < '0' || c > '9' {
				return false
			}
		}

		return true
	}

	if len(raw) != 20 || !digits(raw[0:10]) || raw[10] != ' ' || !digits(raw[11:16]) || raw[16] != ' ' ||
		(raw[17] != 'n' && raw[17] != 'f') || raw[18] != ' ' || raw[19] != '\n' {
		tb.Fatalf("pdf: the cross-reference entry %d is %q, want ten offset digits, a space, five generation digits, a space, 'n' or 'f' and the bytes SP LF; every entry is exactly 20 bytes",
			index, raw)
	}

	offset, err := strconv.Atoi(string(raw[0:10]))
	if err != nil {
		tb.Fatalf("pdf: the cross-reference entry %d's offset is not a number: %v", index, err)
	}

	generation, err := strconv.Atoi(string(raw[11:16]))
	if err != nil {
		tb.Fatalf("pdf: the cross-reference entry %d's generation is not a number: %v", index, err)
	}

	switch {
	case index == 0:
		if string(raw) != "0000000000 65535 f \n" {
			tb.Fatalf("pdf: the free cross-reference entry 0 is %q, want %q", raw, "0000000000 65535 f \n")
		}
	case raw[17] != 'n':
		tb.Fatalf("pdf: the cross-reference entry %d is free, but object %d is in the file", index, index)
	case generation != 0:
		tb.Fatalf("pdf: the cross-reference entry %d's generation is %d, want the writer's constant 0", index, generation)
	}

	return pdfXrefEntry{offset: offset}
}

// parseObjects parses every object the cross-reference table lists, at the
// offset the table gives, and checks the stream framing and the /Length.
func parseObjects(tb testing.TB, file *testPDF, data []byte) {
	tb.Helper()

	for num := 1; num < len(file.xref); num++ {
		obj := parseObject(tb, data, num, file.xref[num].offset)
		file.objects[num] = obj
		file.order = append(file.order, obj)
	}

	root := file.trailer.mustRef(tb, "the trailer", "Root")

	catalog := file.object(tb, root)
	if name, ok := catalog.typeName(); !ok || name != "Catalog" {
		tb.Fatalf("pdf: the trailer's /Root names object %d, whose /Type is %q, want the catalog", root, name)
	}
}

// parseObject parses the object whose number and offset the cross-reference
// table gives.
func parseObject(tb testing.TB, data []byte, num, offset int) *pdfObject {
	tb.Helper()

	header := fmt.Sprintf("%d 0 obj\n", num)
	if offset <= 0 || offset+len(header) > len(data) || !bytes.HasPrefix(data[offset:], []byte(header)) {
		tb.Fatalf("pdf: the cross-reference entry for object %d names byte %d, which does not hold %q: %q",
			num, offset, header, previewAt(data, offset))
	}

	lexer := &pdfLexer{tb: tb, data: data, pos: offset + len(header)}

	value := lexer.parseValue()
	if value.kind != pdfValueDict {
		lexer.fail("object %d is %s, want a dictionary", num, kindName(value.kind))
	}

	obj := &pdfObject{num: num, dict: value.dict}

	lexer.skipSpace()
	if lexer.has("stream") {
		lexer.expect("a stream", "stream\n")

		length := value.dict.mustNumber(tb, fmt.Sprintf("object %d", num), "Length")
		end := lexer.pos + length
		if end > len(data) {
			lexer.fail("object %d's /Length %d runs past the end of the file", num, length)
		}

		obj.stream = data[lexer.pos:end]
		obj.hasStream = true
		lexer.pos = end
		lexer.expect("the end of the stream", "\nendstream\nendobj")

		return obj
	}

	lexer.expect("the end of the object", "endobj")

	return obj
}

// previewAt returns a short quoted form of the bytes at offset.
func previewAt(data []byte, offset int) string {
	if offset < 0 || offset > len(data) {
		return "outside the file"
	}

	return preview(data[offset:])
}

// parsePageTree walks the page tree's flat /Kids list and parses each page's
// content stream into text runs.
func parsePageTree(tb testing.TB, file *testPDF) {
	tb.Helper()

	root := file.trailer.mustRef(tb, "the trailer", "Root")
	catalog := file.object(tb, root)
	pagesNum := catalog.dict.mustRef(tb, "the catalog", "Pages")

	pages := file.object(tb, pagesNum)
	if name, ok := pages.typeName(); !ok || name != "Pages" {
		tb.Fatalf("pdf: the catalog's /Pages names object %d, whose /Type is %q, want /Pages", pagesNum, name)
	}

	kids := pages.dict.mustArray(tb, "the page tree", "Kids")
	if count := pages.dict.mustNumber(tb, "the page tree", "Count"); count != len(kids) {
		tb.Fatalf("pdf: the page tree's /Count is %d, want its %d kids", count, len(kids))
	}

	for index, kid := range kids {
		if kid.kind != pdfValueRef {
			tb.Fatalf("pdf: the page tree's kid %d is %s, want an indirect reference", index, kindName(kid.kind))
		}

		page := file.object(tb, kid.ref)
		if name, ok := page.typeName(); !ok || name != "Page" {
			tb.Fatalf("pdf: the page tree's kid %d (object %d) is %q, want /Page", index, kid.ref, name)
		}

		contents := page.dict.mustRef(tb, fmt.Sprintf("page %d", index+1), "Contents")
		content := file.object(tb, contents)
		if !content.hasStream {
			tb.Fatalf("pdf: page %d's /Contents (object %d) is not a stream", index+1, contents)
		}

		runs, rules := parseContentStream(tb, index, content.stream)

		file.pages = append(file.pages, &pdfPage{
			num:     kid.ref,
			content: contents,
			runs:    runs,
			rules:   rules,
		})
	}
}

// parseContentStream parses one content stream into its rules and its text
// runs. It checks every token against the writer's fixed operator table:
// operands are integers, literal strings and the font resource names F1 to F3,
// and there is no graphics state dictionary, XObject, inline image, array,
// dictionary or comment.
func parseContentStream(tb testing.TB, page int, stream []byte) ([]pdfTextRun, []pdfRule) {
	tb.Helper()

	lexer := &pdfLexer{tb: tb, data: stream}

	var (
		operands []pdfValue
		runs     []pdfTextRun
		rules    []pdfRule
		inText   bool
		x, y     int
		font     string
		size     int
	)

	for {
		lexer.skipSpace()

		if lexer.eof() {
			break
		}

		c := lexer.data[lexer.pos]

		switch {
		case c == '(':
			operands = append(operands, pdfValue{kind: pdfValueString, str: lexer.parseLiteralString()})
		case c == '/':
			operands = append(operands, pdfValue{kind: pdfValueName, name: lexer.parseName()})
		case c == '-' || c == '+' || (c >= '0' && c <= '9'):
			operands = append(operands, pdfValue{kind: pdfValueNumber, num: lexer.parseNumber()})
		case c == '[' || c == '<' || c == '%':
			lexer.fail("the content stream holds %q, but the writer's content streams hold only literal strings, integers, font resource names and its fixed operators",
				preview(stream[lexer.pos:]))
		default:
			operator := lexer.parseOperator()

			shape, ok := pdfContentOperators[operator]
			if !ok {
				lexer.fail("the content stream uses the operator %q, which is not in the writer's fixed operator table", operator)
			}

			checkOperands(tb, lexer, operator, shape, operands)

			switch operator {
			case "BT":
				inText, x, y = true, 0, 0
			case "ET":
				if !inText {
					lexer.fail("the content stream has an ET with no BT")
				}

				inText = false
			case "Tf":
				if !inText {
					lexer.fail("the content stream selects a font outside BT and ET")
				}

				font, size = operands[0].name, operands[1].num
			case "Tm":
				if !inText {
					lexer.fail("the content stream sets the text matrix outside BT and ET")
				}

				x, y = operands[4].num, operands[5].num
			case "Tj":
				if !inText {
					lexer.fail("the content stream shows text outside BT and ET")
				}

				if font == "" {
					lexer.fail("the content stream shows text before it selects a font")
				}

				text, err := decodeWinAnsi(operands[0].str)
				if err != nil {
					lexer.fail("%v", err)
				}

				runs = append(runs, pdfTextRun{Page: page, X: x, Y: y, Font: font, Size: size, Text: text})
			case "re":
				rules = append(rules, pdfRule{X: operands[0].num, Y: operands[1].num, W: operands[2].num, H: operands[3].num})
			}

			operands = nil
		}
	}

	if len(operands) > 0 {
		lexer.fail("the content stream ends with operands and no operator")
	}

	if inText {
		lexer.fail("the content stream has a BT with no ET")
	}

	return runs, rules
}

// parseOperator reads one content stream operator.
func (l *pdfLexer) parseOperator() string {
	start := l.pos

	for !l.eof() {
		c := l.data[l.pos]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '*' || c == '\'' || c == '"' {
			l.pos++

			continue
		}

		break
	}

	if l.pos == start {
		l.fail("the content stream holds the byte %q where an operator was expected", l.data[l.pos])
	}

	return string(l.data[start:l.pos])
}

// checkOperands checks one operator's operands against the writer's fixed table.
func checkOperands(tb testing.TB, lexer *pdfLexer, operator string, shape []pdfValueKind, operands []pdfValue) {
	tb.Helper()

	if len(operands) != len(shape) {
		lexer.fail("the operator %s takes %d operands, found %d", operator, len(shape), len(operands))
	}

	for i, kind := range shape {
		if operands[i].kind != kind {
			lexer.fail("operand %d of the operator %s is %s, want %s", i+1, operator, kindName(operands[i].kind), kindName(kind))
		}
	}

	if operator != "Tf" {
		return
	}

	switch operands[0].name {
	case "F1", "F2", "F3":
	default:
		lexer.fail("the content stream selects the font /%s, want one of the writer's constant font resources /F1, /F2, /F3",
			operands[0].name)
	}
}

// TestReaderParsesAMinimalPDF checks the tests' reader itself against a small
// file written by hand here, not by the reporter: a bug in the reader must not
// hide a bug in the writer, so the reader is pinned on its own.
func TestReaderParsesAMinimalPDF(t *testing.T) {
	t.Parallel()

	data := buildMinimalPDF(t)
	file := checkPDFStructure(t, data)

	if file.version != "1.7" {
		t.Errorf("the parsed version is %q, want 1.7", file.version)
	}

	if len(file.pages) != 1 {
		t.Fatalf("the file has %d pages, want 1", len(file.pages))
	}

	runs := file.pages[0].runs
	if len(runs) != 1 {
		t.Fatalf("page 1 draws %d text runs, want 1: %v", len(runs), runs)
	}

	run := runs[0]
	if run.X != 50 || run.Y != 700 || run.Font != "F1" || run.Page != 0 {
		t.Errorf("the run is at (%d, %d) in /%s on page %d, want (50, 700) in /F1 on page 0",
			run.X, run.Y, run.Font, run.Page)
	}

	if want := "Hello (x) \u00e9"; run.Text != want {
		t.Errorf("the run's text is %q, want %q", run.Text, want)
	}
}

// buildMinimalPDF builds a one-page file of the writer's subset, with its
// cross-reference offsets computed as it is assembled.
func buildMinimalPDF(tb testing.TB) []byte {
	tb.Helper()

	content := "BT /F1 10 Tf 1 0 0 1 50 700 Tm (Hello \\(x\\) \\351) Tj ET\n"

	// Object 6 is the info dictionary; the trailer names it too, so the file
	// covers every object type of the writer's set.
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Title (shift2eu migration assessment) >>",
	}

	var buf bytes.Buffer

	buf.WriteString(pdfMagic)

	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xrefOffset := buf.Len()

	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}

	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n",
		len(objects)+1, xrefOffset)
	buf.WriteString(pdfEOFMarker)

	return buf.Bytes()
}

// decodeWinAnsi decodes one literal string's bytes as WinAnsiEncoding (ISO
// 32000-1, Annex D.2).
func decodeWinAnsi(text string) (string, error) {
	var shown strings.Builder

	for i := 0; i < len(text); i++ {
		c := text[i]

		switch {
		case c >= 0x20 && c <= 0x7E:
			shown.WriteByte(c)
		case c >= 0x80:
			r, ok := winAnsiHigh[c]
			if !ok {
				return "", fmt.Errorf("the literal string holds byte %#02x, which WinAnsiEncoding assigns no character", c)
			}

			shown.WriteRune(r)
		default:
			return "", fmt.Errorf("the literal string holds the raw byte %#02x; every control character of a value is already a visible escape", c)
		}
	}

	return shown.String(), nil
}

// winAnsiHigh is WinAnsiEncoding's bytes 0x80 to 0xFF (ISO 32000-1, Annex D.2).
// Bytes 0x81, 0x8D, 0x8F, 0x90 and 0x9D are unassigned, so a literal string may
// not hold them.
var winAnsiHigh = map[byte]rune{
	0x80: '\u20ac',
	0x82: '\u201a',
	0x83: '\u0192',
	0x84: '\u201e',
	0x85: '\u2026',
	0x86: '\u2020',
	0x87: '\u2021',
	0x88: '\u02c6',
	0x89: '\u2030',
	0x8A: '\u0160',
	0x8B: '\u2039',
	0x8C: '\u0152',
	0x8E: '\u017d',
	0x91: '\u2018',
	0x92: '\u2019',
	0x93: '\u201c',
	0x94: '\u201d',
	0x95: '\u2022',
	0x96: '\u2013',
	0x97: '\u2014',
	0x98: '\u02dc',
	0x99: '\u2122',
	0x9A: '\u0161',
	0x9B: '\u203a',
	0x9C: '\u0153',
	0x9E: '\u017e',
	0x9F: '\u0178',
	0xA0: '\u00a0',
	0xA1: '\u00a1',
	0xA2: '\u00a2',
	0xA3: '\u00a3',
	0xA4: '\u00a4',
	0xA5: '\u00a5',
	0xA6: '\u00a6',
	0xA7: '\u00a7',
	0xA8: '\u00a8',
	0xA9: '\u00a9',
	0xAA: '\u00aa',
	0xAB: '\u00ab',
	0xAC: '\u00ac',
	0xAE: '\u00ae',
	0xAF: '\u00af',
	0xB0: '\u00b0',
	0xB1: '\u00b1',
	0xB2: '\u00b2',
	0xB3: '\u00b3',
	0xB4: '\u00b4',
	0xB5: '\u00b5',
	0xB6: '\u00b6',
	0xB7: '\u00b7',
	0xB8: '\u00b8',
	0xB9: '\u00b9',
	0xBA: '\u00ba',
	0xBB: '\u00bb',
	0xBC: '\u00bc',
	0xBD: '\u00bd',
	0xBE: '\u00be',
	0xBF: '\u00bf',
	0xC0: '\u00c0',
	0xC1: '\u00c1',
	0xC2: '\u00c2',
	0xC3: '\u00c3',
	0xC4: '\u00c4',
	0xC5: '\u00c5',
	0xC6: '\u00c6',
	0xC7: '\u00c7',
	0xC8: '\u00c8',
	0xC9: '\u00c9',
	0xCA: '\u00ca',
	0xCB: '\u00cb',
	0xCC: '\u00cc',
	0xCD: '\u00cd',
	0xCE: '\u00ce',
	0xCF: '\u00cf',
	0xD0: '\u00d0',
	0xD1: '\u00d1',
	0xD2: '\u00d2',
	0xD3: '\u00d3',
	0xD4: '\u00d4',
	0xD5: '\u00d5',
	0xD6: '\u00d6',
	0xD7: '\u00d7',
	0xD8: '\u00d8',
	0xD9: '\u00d9',
	0xDA: '\u00da',
	0xDB: '\u00db',
	0xDC: '\u00dc',
	0xDD: '\u00dd',
	0xDE: '\u00de',
	0xDF: '\u00df',
	0xE0: '\u00e0',
	0xE1: '\u00e1',
	0xE2: '\u00e2',
	0xE3: '\u00e3',
	0xE4: '\u00e4',
	0xE5: '\u00e5',
	0xE6: '\u00e6',
	0xE7: '\u00e7',
	0xE8: '\u00e8',
	0xE9: '\u00e9',
	0xEA: '\u00ea',
	0xEB: '\u00eb',
	0xEC: '\u00ec',
	0xED: '\u00ed',
	0xEE: '\u00ee',
	0xEF: '\u00ef',
	0xF0: '\u00f0',
	0xF1: '\u00f1',
	0xF2: '\u00f2',
	0xF3: '\u00f3',
	0xF4: '\u00f4',
	0xF5: '\u00f5',
	0xF6: '\u00f6',
	0xF7: '\u00f7',
	0xF8: '\u00f8',
	0xF9: '\u00f9',
	0xFA: '\u00fa',
	0xFB: '\u00fb',
	0xFC: '\u00fc',
	0xFD: '\u00fd',
	0xFE: '\u00fe',
	0xFF: '\u00ff',
}
