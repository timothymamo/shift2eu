package pdfreport

import (
	"strconv"
	"strings"
)

// The file's fixed frame: the header, which names PDF 1.7 (ISO 32000-1), and
// the end-of-file marker.
const (
	fileHeader = "%PDF-1.7\n"
	fileEnd    = "%%EOF\n"
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
// at size points and in colour with its baseline starting at x, y (points).
type textRun struct {
	font   font
	size   int
	x, y   int
	text   string
	colour colour
}

// rect is a rectangle in whole points: its left x, bottom y, width and height.
// A rule is a rect of height 0, stroked.
type rect struct {
	x, y, w, h int
}

// point is one point of a path, in thousandths of a point.
type point struct {
	x, y int
}

// subpath is one subpath of a path: its points, joined by straight lines, and
// whether it is closed back to its first point.
type subpath struct {
	points []point
	closed bool
}

// shape is one path the page fills or strokes: its rectangles and subpaths, in
// one colour, and for a stroke its line width in thousandths of a point. A
// shape with neither rectangles nor subpaths draws nothing: a box keeps such a
// placeholder where its fill goes until it knows the fill's height.
type shape struct {
	stroke   bool
	colour   colour
	width    int
	rects    []rect
	subpaths []subpath
}

// shift moves the shape up by dy points (down for a negative dy).
func (s *shape) shift(dy int) {
	for i := range s.rects {
		s.rects[i].y += dy
	}

	for i := range s.subpaths {
		for j := range s.subpaths[i].points {
			s.subpaths[i].points[j].y += dy * milli
		}
	}
}

// fillRect returns a rectangle filled in c.
func fillRect(x, y, w, h int, c colour) shape {
	return shape{colour: c, rects: []rect{{x: x, y: y, w: w, h: h}}}
}

// ruleLine returns a horizontal 0.5 pt --ring rule from x, y, w points long.
func ruleLine(x, y, w int) shape {
	return shape{stroke: true, colour: ringColour, width: hairline, rects: []rect{{x: x, y: y, w: w}}}
}

// hairline is the width of every rule and outline: 0.5 pt, in thousandths.
const hairline = 500

// page is what one page draws: its shapes, then its text runs, each in drawing
// order. Every shape is drawn under every run.
type page struct {
	shapes []shape
	runs   []textRun
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

// contentStream returns the content stream of p: its shapes inside q and Q,
// each pathed with re, m, l and h and completed with f or S, then one text
// object holding every run, each placed by an absolute text matrix and drawn
// with Tj. A colour (rg, RG), a line width (w) and a font (Tf) are set only
// when they change. These sixteen operators are the whole set (ADR 0049,
// Drawing: the operator set): every operand is an integer, a decimal of three
// places, a literal string or a font resource name.
func contentStream(p *page) string {
	var b strings.Builder

	if len(p.shapes) > 0 {
		b.WriteString("q\n")
		writeShapes(&b, p.shapes)
		b.WriteString("Q\n")
	}

	b.WriteString("BT")

	var (
		current font
		size    int
		ink     colour
	)

	for _, run := range p.runs {
		if run.colour != ink {
			ink = run.colour
			b.WriteString("\n" + ink.operands() + " rg")
		}

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

// writeShapes writes the shapes in order. The colour and line width start
// unset after q, so the first shape of each kind sets its own.
func writeShapes(b *strings.Builder, shapes []shape) {
	var fill, stroke colour

	width := -1

	for _, s := range shapes {
		if len(s.rects) == 0 && len(s.subpaths) == 0 {
			continue
		}

		switch {
		case s.stroke:
			if s.colour != stroke {
				stroke = s.colour
				b.WriteString(stroke.operands() + " RG\n")
			}

			if s.width != width {
				width = s.width
				b.WriteString(decimal(width) + " w\n")
			}
		case s.colour != fill:
			fill = s.colour
			b.WriteString(fill.operands() + " rg\n")
		}

		for _, r := range s.rects {
			b.WriteString(strconv.Itoa(r.x) + " " + strconv.Itoa(r.y) + " " + strconv.Itoa(r.w) + " " +
				strconv.Itoa(r.h) + " re\n")
		}

		for _, sp := range s.subpaths {
			for i, pt := range sp.points {
				op := " l\n"
				if i == 0 {
					op = " m\n"
				}

				b.WriteString(decimal(pt.x) + " " + decimal(pt.y) + op)
			}

			if sp.closed {
				b.WriteString("h\n")
			}
		}

		if s.stroke {
			b.WriteString("S\n")
		} else {
			b.WriteString("f\n")
		}
	}
}

// decimal writes n thousandths as a decimal with exactly three places and no
// sign: 500 is "0.500", 43 is "0.043". The layout never draws below zero, so n
// is never negative; a negative n is a bug of the layout, and decimal panics
// rather than write a signed or malformed number into the PDF.
func decimal(n int) string {
	if n < 0 {
		panic("pdfreport: decimal of a negative number " + strconv.Itoa(n) + ": the layout drew below zero")
	}

	fraction := strconv.Itoa(n % milli)

	return strconv.Itoa(n/milli) + "." + strings.Repeat("0", 3-len(fraction)) + fraction
}
