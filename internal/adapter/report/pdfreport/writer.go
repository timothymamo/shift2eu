package pdfreport

import (
	"strconv"
	"strings"
)

// The file's fixed frame: the header, which names PDF 1.7 (ISO 32000-1), and
// the end-of-file marker. The marker is split so that no string literal of the
// package holds a percent sign before a letter a float verb would use.
const (
	fileHeader = "%PDF-1.7\n"
	fileEnd    = "%%" + "EOF\n"
)

// infoTitle is the one entry of the /Info dictionary: a constant, never a value.
const infoTitle = "shift2eu migration assessment"

// The page size: A4 portrait, in points.
const (
	pageWidth  = 595
	pageHeight = 842
)

// The fixed object numbers. Each page then takes two objects, the page and its
// content stream, from firstPageObject on.
const (
	catalogObject   = 1
	pagesObject     = 2
	firstFontObject = 3 // one object per font, in the order of fonts
	infoObject      = 6
	firstPageObject = 7
)

// textRun is one Tj: text, already escaped where it is a value, drawn in font
// at size points with its baseline starting at x, y (points).
type textRun struct {
	font font
	size int
	x, y int
	text string
}

// rule is a horizontal hairline from x, y, w points long: a rectangle of
// height 0, stroked with the thinnest line a device draws (line width 0).
type rule struct {
	x, y, w int
}

// page is what one page draws: its rules and its text runs, in drawing order.
type page struct {
	rules []rule
	runs  []textRun
}

// pdfFile assembles the objects of the file and its cross-reference table.
type pdfFile struct {
	b       strings.Builder
	offsets []int
}

// object writes object num with the dictionary body, recording its offset.
// Objects are written in number order, so num is always the next number.
func (f *pdfFile) object(num int, body string) {
	f.offsets = append(f.offsets, f.b.Len())
	f.b.WriteString(strconv.Itoa(num) + " 0 obj\n" + body + "\nendobj\n")
}

// stream writes object num as a stream holding data, with its exact /Length.
func (f *pdfFile) stream(num int, data string) {
	f.offsets = append(f.offsets, f.b.Len())
	f.b.WriteString(strconv.Itoa(num) + " 0 obj\n<< /Length " + strconv.Itoa(len(data)) + " >>\nstream\n")
	f.b.WriteString(data)
	f.b.WriteString("\nendstream\nendobj\n")
}

// writeFile returns the whole PDF of pages: the header, the catalog, the page
// tree, the three fonts, the info dictionary, each page and its content stream,
// the cross-reference table with its 20-byte entries, the trailer and the
// end-of-file marker. Every byte is a line feed or printable ASCII.
func writeFile(pages []*page) string {
	var f pdfFile

	f.b.WriteString(fileHeader)

	f.object(catalogObject, "<< /Type /Catalog /Pages "+ref(pagesObject)+" >>")

	kids := make([]string, 0, len(pages))
	for i := range pages {
		kids = append(kids, ref(firstPageObject+2*i))
	}

	f.object(pagesObject, "<< /Type /Pages /Kids ["+strings.Join(kids, " ")+"] /Count "+strconv.Itoa(len(pages))+" >>")

	for i, entry := range fonts {
		f.object(firstFontObject+i, "<< /Type /Font /Subtype /Type1 /BaseFont /"+entry.name+
			" /Encoding /WinAnsiEncoding >>")
	}

	f.object(infoObject, "<< /Title "+pdfString(infoTitle)+" >>")

	fontResources := make([]string, 0, len(fonts))
	for i, entry := range fonts {
		fontResources = append(fontResources, "/"+entry.font.resourceName()+" "+ref(firstFontObject+i))
	}

	resources := "/Resources << /Font << " + strings.Join(fontResources, " ") + " >> >>"
	mediaBox := "/MediaBox [0 0 " + strconv.Itoa(pageWidth) + " " + strconv.Itoa(pageHeight) + "]"

	for i, p := range pages {
		num := firstPageObject + 2*i
		f.object(num, "<< /Type /Page /Parent "+ref(pagesObject)+" "+mediaBox+" "+resources+
			" /Contents "+ref(num+1)+" >>")
		f.stream(num+1, contentStream(p))
	}

	xref := f.b.Len()
	count := len(f.offsets) + 1

	f.b.WriteString("xref\n0 " + strconv.Itoa(count) + "\n")
	f.b.WriteString("0000000000 65535 f \n")

	for _, offset := range f.offsets {
		digits := strconv.Itoa(offset)
		f.b.WriteString(strings.Repeat("0", 10-len(digits)) + digits + " 00000 n \n")
	}

	f.b.WriteString("trailer\n<< /Size " + strconv.Itoa(count) + " /Root " + ref(catalogObject) +
		" /Info " + ref(infoObject) + " >>\nstartxref\n" + strconv.Itoa(xref) + "\n" + fileEnd)

	return f.b.String()
}

// ref returns an indirect reference to object num.
func ref(num int) string { return strconv.Itoa(num) + " 0 R" }

// contentStream returns the content stream of p: its rules as black hairlines,
// then one text object holding every run, each placed by an absolute text
// matrix and drawn with Tj, the font selected only when it changes. Every
// operand is an integer, a literal string or a font resource name.
func contentStream(p *page) string {
	var b strings.Builder

	if len(p.rules) > 0 {
		b.WriteString("q\n0 G\n0 w\n")

		for _, r := range p.rules {
			b.WriteString(strconv.Itoa(r.x) + " " + strconv.Itoa(r.y) + " " + strconv.Itoa(r.w) + " 0 re\nS\n")
		}

		b.WriteString("Q\n")
	}

	b.WriteString("BT")

	var (
		current font
		size    int
	)

	for _, run := range p.runs {
		if run.font != current || run.size != size {
			current, size = run.font, run.size
			b.WriteString("\n/" + current.resourceName() + " " + strconv.Itoa(size) + " Tf")
		}

		b.WriteString("\n1 0 0 1 " + strconv.Itoa(run.x) + " " + strconv.Itoa(run.y) + " Tm\n")
		b.WriteString(pdfString(run.text) + " Tj")
	}

	b.WriteString("\nET")

	return b.String()
}
