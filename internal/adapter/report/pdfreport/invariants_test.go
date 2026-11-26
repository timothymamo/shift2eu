package pdfreport_test

// This file pins the structural invariants every PDF the writer emits has to
// hold, and the writer's fixed content-stream operator table. The golden,
// hostile, determinism and fuzz tests all check them through checkPDFStructure.

import (
	"fmt"
	"testing"
)

// The page size the PRD fixes: A4 portrait, 595 × 842 pt (Decided, as
// recommended, 2).
const (
	pageWidth  = 595
	pageHeight = 842
)

// pdfContentOperators is the writer's fixed content-stream operator table: each
// operator with the operands it takes. The tests pin it so that a writer cannot
// slip in an operator of its own: no graphics state dictionary (gs), no XObject
// (Do) and no inline image is in it.
//
// Operands are integers, literal strings and font resource names; the reader
// checks each operator's operands against its row here, and that the font
// resource a Tf selects is one of the writer's constant names /F1, /F2, /F3.
var pdfContentOperators = map[string][]pdfValueKind{
	"BT": {},
	"ET": {},
	"Tf": {pdfValueName, pdfValueNumber},
	"Td": {pdfValueNumber, pdfValueNumber},
	"Tm": {pdfValueNumber, pdfValueNumber, pdfValueNumber, pdfValueNumber, pdfValueNumber, pdfValueNumber},
	"Tj": {pdfValueString},
	"re": {pdfValueNumber, pdfValueNumber, pdfValueNumber, pdfValueNumber},
	"f":  {},
	"S":  {},
	"w":  {pdfValueNumber},
	"q":  {},
	"Q":  {},
	"g":  {pdfValueNumber},
	"G":  {pdfValueNumber},
	"rg": {pdfValueNumber, pdfValueNumber, pdfValueNumber},
	"RG": {pdfValueNumber, pdfValueNumber, pdfValueNumber},
}

// pdfBannedNames are the names a file must never hold, outside a string: each
// one would make a reader act on the file, or name metadata that varies between
// runs (URLs, actions, scripts, forms, embedded files and fonts, dates, the
// document ID and the producer).
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
		if number.kind != pdfValueNumber || number.num != want[i] {
			tb.Fatalf("pdf: page %d's /MediaBox is not A4 portrait [0 0 595 842]", page.num)
		}
	}
}

// checkPageFonts checks that the page's /Resources names every font its text
// runs use, as an indirect reference to a font object.
func checkPageFonts(tb testing.TB, file *testPDF, page *pdfPage, obj *pdfObject) {
	tb.Helper()

	resources, ok := obj.dict.get("Resources")
	if !ok || resources.kind != pdfValueDict {
		tb.Fatalf("pdf: page %d has no /Resources dictionary", page.num)
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
	case pdfValueNumber, pdfValueString, pdfValueRef:
		// A number, a string and a reference hold no name; a value that spells
		// one inside a string is text, never a name.
	}
}
