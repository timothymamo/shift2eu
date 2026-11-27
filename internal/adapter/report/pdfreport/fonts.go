package pdfreport

// font is one of the three core fonts the PDF uses. Its value is the number of
// its font resource: /F1, /F2 or /F3.
type font int

// The three core fonts (ISO 32000-1, 9.6.2.2), never embedded: Helvetica for
// the report's own words, Helvetica-Bold for headings and table header cells,
// Courier for every value from the inventory or a catalog.
const (
	helvetica     font = 1
	helveticaBold font = 2
	courier       font = 3
)

// fonts lists the fonts in resource order, with their base font names.
var fonts = []struct {
	font font
	name string
}{
	{font: helvetica, name: "Helvetica"},
	{font: helveticaBold, name: "Helvetica-Bold"},
	{font: courier, name: "Courier"},
}

// resourceName is the font's resource name in a content stream, without its
// slash.
func (f font) resourceName() string {
	switch f {
	case helveticaBold:
		return "F2"
	case courier:
		return "F3"
	default:
		return "F1"
	}
}

// courierWidth is the advance width of every Courier character, in thousandths
// of the font size.
const courierWidth = 600

// fallbackWidth is the width used for a byte a table does not cover: wide
// enough never to let text run past its column.
const fallbackWidth = 1000

// helveticaWidths are Helvetica's advance widths of the WinAnsiEncoding bytes
// 0x20 to 0xFF, in thousandths of the font size, from the font's standard AFM
// metrics.
var helveticaWidths = [224]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584, 350,
	556, 350, 222, 556, 333, 1000, 556, 556, 333, 1000, 667, 333, 1000, 350, 611, 350,
	350, 222, 222, 333, 333, 350, 556, 1000, 333, 1000, 500, 333, 944, 350, 500, 667,
	278, 333, 556, 556, 556, 556, 260, 556, 333, 737, 370, 556, 584, 333, 737, 333,
	400, 584, 333, 333, 333, 556, 537, 278, 333, 333, 365, 556, 834, 834, 834, 611,
	667, 667, 667, 667, 667, 667, 1000, 722, 667, 667, 667, 667, 278, 278, 278, 278,
	722, 722, 778, 778, 778, 778, 778, 584, 778, 722, 722, 722, 722, 667, 667, 611,
	556, 556, 556, 556, 556, 556, 889, 500, 556, 556, 556, 556, 278, 278, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 584, 611, 556, 556, 556, 556, 500, 556, 500,
}

// helveticaBoldWidths are Helvetica-Bold's advance widths of the WinAnsiEncoding
// bytes 0x20 to 0xFF, in thousandths of the font size, from the font's standard
// AFM metrics.
var helveticaBoldWidths = [224]int{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584, 350,
	556, 350, 278, 556, 500, 1000, 556, 556, 333, 1000, 667, 333, 1000, 350, 611, 350,
	350, 278, 278, 500, 500, 350, 556, 1000, 333, 1000, 556, 333, 944, 350, 500, 667,
	278, 333, 556, 556, 556, 556, 280, 556, 333, 737, 370, 556, 584, 333, 737, 333,
	400, 584, 333, 333, 333, 611, 556, 278, 333, 333, 365, 556, 834, 834, 834, 611,
	722, 722, 722, 722, 722, 722, 1000, 722, 667, 667, 667, 667, 278, 278, 278, 278,
	722, 722, 778, 778, 778, 778, 778, 584, 778, 722, 722, 722, 722, 667, 667, 611,
	556, 556, 556, 556, 556, 556, 889, 556, 556, 556, 556, 556, 278, 278, 278, 278,
	611, 611, 611, 611, 611, 611, 611, 584, 611, 611, 611, 611, 611, 556, 611, 556,
}

// charWidth returns the advance width of r in f, in thousandths of the font
// size.
func charWidth(f font, r rune) int {
	if f == courier {
		return courierWidth
	}

	c, ok := shownByte(r)
	if !ok || c < 0x20 {
		return fallbackWidth
	}

	if f == helveticaBold {
		return helveticaBoldWidths[c-0x20]
	}

	return helveticaWidths[c-0x20]
}

// textWidth returns the width of text in f at size points, in thousandths of a
// point.
func textWidth(f font, size int, text string) int {
	width := 0
	for _, r := range text {
		width += charWidth(f, r)
	}

	return width * size
}
