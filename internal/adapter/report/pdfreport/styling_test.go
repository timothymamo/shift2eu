package pdfreport_test

// The restyled report of PRD #584 milestone 1b: the site's palette, the fit
// badges with their icons, the boxes and the contact banner (PRD, The PDF, Look;
// ADR 0049, Drawing: the operator set).
//
// Every check here reads what a PDF reader reads — the content stream's colours,
// paths and text runs — and never the writer's own code. The goldens are the ten
// milestone-1 fixtures, rendered fresh: after the coder regenerates the golden
// files with -update, these are exactly their bytes.

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// paletteEntry is one row of the palette (PRD, Look): the token's name in
// site/style.css, its hex value there, the three components written as fixed
// decimals with three places, and the written form the PRD's column gives.
type paletteEntry struct {
	Name  string
	Hex   int
	RGB   pdfRGB
	Shown string
}

// pdfPalette is the palette of the site's light-theme tokens, exactly as the
// PRD's Look table lists it: the only colours a report's content stream holds.
var pdfPalette = []paletteEntry{
	{Name: "--ink", Hex: 0x14213d, RGB: pdfRGB{78, 129, 239}, Shown: "0.078 0.129 0.239"},
	{Name: "--muted", Hex: 0x4a5568, RGB: pdfRGB{290, 333, 408}, Shown: "0.290 0.333 0.408"},
	{Name: "--mast", Hex: 0x0b2a66, RGB: pdfRGB{43, 165, 400}, Shown: "0.043 0.165 0.400"},
	{Name: "--mast-ink", Hex: 0xffffff, RGB: pdfRGB{1000, 1000, 1000}, Shown: "1.000 1.000 1.000"},
	{Name: "--mast-link", Hex: 0xc9d8ff, RGB: pdfRGB{788, 847, 1000}, Shown: "0.788 0.847 1.000"},
	{Name: "--mast-text", Hex: 0xe6ebf5, RGB: pdfRGB{902, 922, 961}, Shown: "0.902 0.922 0.961"},
	{Name: "--accent", Hex: 0x0b3d91, RGB: pdfRGB{43, 239, 569}, Shown: "0.043 0.239 0.569"},
	{Name: "--gold", Hex: 0xffcc00, RGB: pdfRGB{1000, 800, 0}, Shown: "1.000 0.800 0.000"},
	{Name: "--gold-ink", Hex: 0x0b2a66, RGB: pdfRGB{43, 165, 400}, Shown: "0.043 0.165 0.400"},
	{Name: "--direct-ink", Hex: 0x0f6b3a, RGB: pdfRGB{59, 420, 227}, Shown: "0.059 0.420 0.227"},
	{Name: "--direct-bg", Hex: 0xe3f4ea, RGB: pdfRGB{890, 957, 918}, Shown: "0.890 0.957 0.918"},
	{Name: "--partial-ink", Hex: 0x874600, RGB: pdfRGB{529, 275, 0}, Shown: "0.529 0.275 0.000"},
	{Name: "--partial-bg", Hex: 0xfdf0d5, RGB: pdfRGB{992, 941, 835}, Shown: "0.992 0.941 0.835"},
	{Name: "--none-ink", Hex: 0xa3201a, RGB: pdfRGB{639, 125, 102}, Shown: "0.639 0.125 0.102"},
	{Name: "--none-bg", Hex: 0xfde4e1, RGB: pdfRGB{992, 894, 882}, Shown: "0.992 0.894 0.882"},
	{Name: "--info-bg", Hex: 0xe8eefb, RGB: pdfRGB{910, 933, 984}, Shown: "0.910 0.933 0.984"},
	{Name: "--info-rule", Hex: 0x0b3d91, RGB: pdfRGB{43, 239, 569}, Shown: "0.043 0.239 0.569"},
	{Name: "--warn-bg", Hex: 0xfdf0d5, RGB: pdfRGB{992, 941, 835}, Shown: "0.992 0.941 0.835"},
	{Name: "--warn-rule", Hex: 0xa86400, RGB: pdfRGB{659, 392, 0}, Shown: "0.659 0.392 0.000"},
	{Name: "--warn-ink", Hex: 0x5c3a00, RGB: pdfRGB{361, 227, 0}, Shown: "0.361 0.227 0.000"},
	{Name: "--ring", Hex: 0xdde2ea, RGB: pdfRGB{867, 886, 918}, Shown: "0.867 0.886 0.918"},
	{Name: "--surface-2", Hex: 0xeef1f6, RGB: pdfRGB{933, 945, 965}, Shown: "0.933 0.945 0.965"},
	{Name: "--bg", Hex: 0xf6f7f9, RGB: pdfRGB{965, 969, 976}, Shown: "0.965 0.969 0.976"},
}

// writtenColour renders a colour as the palette's column writes it.
func writtenColour(colour pdfRGB) string {
	return points(colour.R) + " " + points(colour.G) + " " + points(colour.B)
}

// paletteColourOf returns the palette's colour of a token, and the zero colour
// for a token the palette does not hold. The badge and box checks use it where
// they have no testing.TB to fail with; TestPaletteIsTheIntegerConversion pins the
// table itself.
func paletteColourOf(name string) pdfRGB {
	for _, entry := range pdfPalette {
		if entry.Name == name {
			return entry.RGB
		}
	}

	return pdfRGB{}
}

// paletteName returns the palette's name of a colour, and false for a colour that
// is no row of it. Two tokens can share a colour (--warn-bg and --partial-bg,
// --gold-ink and --mast, --info-rule and --accent), so the name names the first
// row that matches.
func paletteName(colour pdfRGB) (string, bool) {
	for _, entry := range pdfPalette {
		if entry.RGB == colour {
			return entry.Name, true
		}
	}

	return "", false
}

// colourName names a colour for a failure message.
func colourName(colour pdfRGB) string {
	if name, ok := paletteName(colour); ok {
		return name + " (" + writtenColour(colour) + ")"
	}

	return writtenColour(colour)
}

// thousandthsOf is the PRD's integer conversion of one colour component, a 0 to
// 255 byte, into thousandths: byte × 1000 / 255 rounded to the nearest
// thousandth, (byte × 1000 + 127) / 255 (ADR 0049, Colours).
func thousandthsOf(component int) int { return (component*1000 + 127) / 255 }

// TestPaletteIsTheIntegerConversion pins the palette's written values against the
// integer conversion of ADR 0049, and the PRD's three examples of it.
//
// Criterion (PRD #584 milestone 1b, Unit): the colour conversion, from a 0 to 255
// byte, gives (byte × 1000 + 127) / 255 thousandths written with three places: 0
// is 0.000, 11 is 0.043, 255 is 1.000, and every palette entry's hex gives the
// table's value.
func TestPaletteIsTheIntegerConversion(t *testing.T) {
	t.Parallel()

	examples := []struct {
		component int
		want      string
	}{
		{component: 0, want: "0.000"},
		{component: 11, want: "0.043"},
		{component: 255, want: "1.000"},
	}

	for _, example := range examples {
		if got := points(thousandthsOf(example.component)); got != example.want {
			t.Errorf("the component %d writes %s, want %s", example.component, got, example.want)
		}
	}

	for _, entry := range pdfPalette {
		want := pdfRGB{
			R: thousandthsOf(entry.Hex >> 16 & 0xff),
			G: thousandthsOf(entry.Hex >> 8 & 0xff),
			B: thousandthsOf(entry.Hex & 0xff),
		}

		if entry.RGB != want {
			t.Errorf("the palette entry %s (#%06x) writes %s, but the integer conversion gives %s",
				entry.Name, entry.Hex, writtenColour(entry.RGB), writtenColour(want))
		}

		if got := writtenColour(entry.RGB); got != entry.Shown {
			t.Errorf("the palette entry %s (#%06x) is written %q, but the PRD's table gives %q",
				entry.Name, entry.Hex, got, entry.Shown)
		}
	}
}

// stylingVariant is one report the styling invariants are checked on: its name
// and the renderer of the fixture its golden file pins.
type stylingVariant struct {
	name   string
	render func(tb testing.TB) []byte
}

// stylingVariants returns the ten milestone-1 fixtures, rendered the way the
// golden files pin them.
func stylingVariants() []stylingVariant {
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	return []stylingVariant{
		{
			name: "report with estimates on",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				return renderReport(tb, estimateAssessment(tb, "", true), meta)
			},
		},
		{
			name: "report with estimates off",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				return renderReport(tb, estimateAssessment(tb, "", false), meta)
			},
		},
		{
			name: "report with a region chosen",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				return renderReport(tb, estimateAssessment(tb, testutil.ReportSecondRegion, true), meta)
			},
		},
		{
			name: "report with workloads",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				a := assessment.Assess(newInventory(tb, sourceName, workloadResources()), testutil.ReportCatalog(tb))

				return renderReport(tb, a, meta)
			},
		},
		{
			name: "empty report",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				a := assessment.AssessWith(newInventory(tb, sourceName, nil), testutil.ReportCatalog(tb),
					assessment.Options{Estimate: true})

				return renderReport(tb, a, meta)
			},
		},
		{
			name: "long report",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				a := assessment.Assess(newInventory(tb, sourceName, longResources()), testutil.ReportCatalog(tb))

				return renderReport(tb, a, meta)
			},
		},
		{
			name: "hostile report",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				a := assessment.Assess(newInventory(tb, hostileSource, hostileResources()), hostileCatalog(tb, hostileTarget))

				return renderReport(tb, a, analysis.ReportMeta{InventoryName: hostileInventoryName})
			},
		},
		{
			name: "comparison of three targets",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				c := assessment.Compare(assessTargets(tb, newInventory(tb, sourceName, comparisonResources()), threeTargetKeys()))

				return renderComparison(tb, c, meta)
			},
		},
		{
			name: "comparison of two targets",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				c := assessment.Compare(assessTargets(tb, newInventory(tb, sourceName, twoTargetResources()),
					[]string{ovhcloudKey, scalewayKey}))

				return renderComparison(tb, c, meta)
			},
		},
		{
			name: "hostile comparison",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				return renderComparison(tb, assessment.Compare(hostileComparisonTargets(tb)),
					analysis.ReportMeta{InventoryName: hostileInventoryName})
			},
		},
	}
}

// drawnColour returns the colour a shape is drawn in: its fill for an f, its
// stroke for an S.
func drawnColour(shape pdfShape) pdfRGB {
	if shape.Op == "S" {
		return shape.Stroke
	}

	return shape.Fill
}

// TestReportColoursComeFromThePalette pins the palette on every golden's content
// streams: every colour a shape is drawn in and every colour a text run is drawn
// in is a row of the palette, so the report's colours are the site's light-theme
// tokens, nothing else.
//
// Criterion (PRD #584 milestone 1b, Unit): every number in every content stream
// has its written form, and every rg/RG triple of every golden is a row of Look's
// palette, written exactly as the table gives it.
func TestReportColoursComeFromThePalette(t *testing.T) {
	t.Parallel()

	for _, variant := range stylingVariants() {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()

			file := checkPDFStructure(t, variant.render(t))

			for index, page := range file.pages {
				for _, shape := range page.shapes {
					if _, ok := paletteName(drawnColour(shape)); !ok {
						t.Errorf("page %d draws a %s in %s, which is no row of the palette",
							index+1, shape.Op, colourName(drawnColour(shape)))
					}
				}

				for _, run := range page.runs {
					if _, ok := paletteName(run.Colour); !ok {
						t.Errorf("page %d draws the text %s in %s, which is no row of the palette",
							index+1, shortened(run.Text), colourName(run.Colour))
					}
				}
			}
		})
	}
}

// TestTextKeepsTheSitesContrastPairs pins the PRD's greyscale rule: every coloured
// text is one of the site's own pairs, a dark ink on a light fill, or --mast-ink,
// --mast-link or --mast-text on --mast, or --gold-ink on --gold, so a fit or a box
// stays readable printed in greyscale.
//
// Criterion (PRD #584 milestone 1b, Unit): colour is never the only signal — every
// coloured text is a dark ink on a light fill, or --mast-ink, --mast-link or
// --mast-text on --mast, or --gold-ink on --gold, the site's own pairs.
func TestTextKeepsTheSitesContrastPairs(t *testing.T) {
	t.Parallel()

	onNavy := []string{"--mast-ink", "--mast-link", "--mast-text"}
	darkInks := []string{"--ink", "--muted", "--accent", "--warn-ink", "--direct-ink", "--partial-ink", "--none-ink"}

	for _, variant := range stylingVariants() {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()

			file := checkPDFStructure(t, variant.render(t))

			for index, page := range file.pages {
				for _, run := range page.runs {
					switch {
					case colourIn(run.Colour, onNavy):
						if !runIsOnFill(page, run, paletteColourOf("--mast")) {
							t.Errorf("page %d draws %s in %s, which the site pairs with --mast alone, outside a --mast fill",
								index+1, shortened(run.Text), colourName(run.Colour))
						}
					case run.Colour == paletteColourOf("--gold-ink"):
						if !runIsOnFill(page, run, paletteColourOf("--gold")) {
							t.Errorf("page %d draws %s in --gold-ink outside a --gold fill", index+1, shortened(run.Text))
						}
					case colourIn(run.Colour, darkInks):
						if runIsOnFill(page, run, paletteColourOf("--mast")) || runIsOnFill(page, run, paletteColourOf("--gold")) {
							t.Errorf("page %d draws %s in %s on a dark fill, which the site pairs only with its light-on-dark inks",
								index+1, shortened(run.Text), colourName(run.Colour))
						}
					}
				}
			}
		})
	}
}

// colourIn reports whether colour is one of the palette tokens named.
func colourIn(colour pdfRGB, tokens []string) bool {
	for _, token := range tokens {
		if colour == paletteColourOf(token) {
			return true
		}
	}

	return false
}

// runIsOnFill reports whether the run sits inside a fill of the colour given.
func runIsOnFill(page *pdfPage, run pdfTextRun, colour pdfRGB) bool {
	point := pdfPoint{X: run.X * milli, Y: run.Y * milli}

	for _, shape := range page.shapes {
		if shape.Op == "f" && shape.Fill == colour && len(shape.Rects) == 1 && shape.Box.contains(point, boxSlack) {
			return true
		}
	}

	return false
}

// fitBadge is one fit badge a report draws: the filled rectangle in the fit's
// background colour, its index among its page's shapes, the fit it is filled for,
// the fit word its text holds, and its runs.
type fitBadge struct {
	Page  int
	Index int
	Fill  pdfShape
	Fit   string
	Word  string
	Runs  []pdfTextRun
}

// fitWords are the three words a badge carries, in the PRD's capitalisation
// (PRD, Look, Fit badges).
var fitWords = []string{"Direct", "Partial", "None"}

// fitWordOf returns the fit word a badge's text ends with, alone or after a
// count ("3 Direct"), and false for a text that is no badge's.
func fitWordOf(text string) (string, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || len(fields) > 2 {
		return "", false
	}

	word := fields[len(fields)-1]
	if !slices.Contains(fitWords, word) {
		return "", false
	}

	if len(fields) == 2 {
		if _, err := strconv.Atoi(fields[0]); err != nil {
			return "", false
		}
	}

	return word, true
}

// runsInside returns the runs whose start lies inside the shape's box, in x
// order.
func runsInside(page *pdfPage, shape pdfShape) []pdfTextRun {
	var runs []pdfTextRun

	for _, run := range page.runs {
		if shape.Box.contains(pdfPoint{X: run.X * milli, Y: run.Y * milli}, boxSlack) {
			runs = append(runs, run)
		}
	}

	slices.SortStableFunc(runs, func(a, b pdfTextRun) int { return a.X - b.X })

	return runs
}

// badgeFills returns the page's fit badges: the filled shapes in a fit's
// background colour that are one line tall. The blockers box is filled in
// --warn-bg, which the site gives the same value as --partial-bg, so a fill's
// height tells a badge from the box.
func badgeFills(page *pdfPage) []fitBadge {
	if len(page.runs) == 0 {
		return nil
	}

	var badges []fitBadge

	for index, shape := range page.shapes {
		if shape.Op != "f" || shape.Box.H > boxMinHeight || len(shape.Rects) != 1 {
			continue
		}

		var fit string

		switch shape.Fill {
		case paletteColourOf("--direct-bg"):
			fit = "Direct"
		case paletteColourOf("--partial-bg"):
			fit = "Partial"
		case paletteColourOf("--none-bg"):
			fit = "None"
		default:
			continue
		}

		runs := runsInside(page, shape)

		texts := make([]string, 0, len(runs))
		for _, run := range runs {
			texts = append(texts, run.Text)
		}

		word, _ := fitWordOf(squash(strings.Join(texts, " ")))

		badges = append(badges, fitBadge{Page: page.runs[0].Page, Index: index, Fill: shape, Fit: fit, Word: word, Runs: runs})
	}

	return badges
}

// joinedText returns the runs' text, joined with a space.
func joinedText(runs []pdfTextRun) string {
	texts := make([]string, 0, len(runs))
	for _, run := range runs {
		texts = append(texts, run.Text)
	}

	return strings.Join(texts, " ")
}

// TestFitBadgesHoldTheirWord pins the PRD's rule that colour is never the only
// signal: every fit badge drawn holds its word, so a fit reads the same in
// greyscale or by a reader who cannot tell the colours apart.
//
// Criterion (PRD #584 milestone 1b, Unit): badge words — every rectangle filled
// in a fit's -bg colour is a badge holding one text run in that fit's -ink
// colour, the fit's word alone or after a count; no -bg fill holds another fit's
// word; and every fit the page shows as a word is in such a badge.
func TestFitBadgesHoldTheirWord(t *testing.T) {
	t.Parallel()

	for _, variant := range stylingVariants() {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()

			file := checkPDFStructure(t, variant.render(t))

			badges := 0

			for _, page := range file.pages {
				for _, badge := range badgeFills(page) {
					badges++

					if badge.Word == "" {
						t.Errorf("page %d draws a badge filled in %s whose text is %s, which is no fit's word",
							badge.Page+1, colourName(badge.Fill.Fill), shortened(joinedText(badge.Runs)))

						continue
					}

					if badge.Word != badge.Fit {
						t.Errorf("page %d draws the %s badge filled in %s whose word is %q: no -bg fill holds another fit's word",
							badge.Page+1, badge.Fit, colourName(badge.Fill.Fill), badge.Word)
					}

					ink := paletteColourOf("--" + strings.ToLower(badge.Fit) + "-ink")

					for _, run := range badge.Runs {
						if run.Font != tableHeaderFont {
							t.Errorf("page %d draws the %s badge's word %s in the font /%s, want Helvetica-Bold (/%s): the word is in the badge's -ink colour and Helvetica-Bold",
								badge.Page+1, badge.Fit, shortened(run.Text), run.Font, tableHeaderFont)
						}

						if run.Colour != ink {
							t.Errorf("page %d draws the %s badge's word %s in %s, want the fit's ink %s",
								badge.Page+1, badge.Fit, shortened(run.Text), colourName(run.Colour), colourName(ink))
						}
					}
				}
			}

			if badges == 0 {
				t.Fatalf("the report draws no fit badge, but a fit is shown as the word Direct, Partial or None beside its colour and icon")
			}

			assertNoFitWordOutsideABadge(t, file)
		})
	}
}

// assertNoFitWordOutsideABadge checks the other direction: no run holding a fit's
// word stands outside a filled badge.
func assertNoFitWordOutsideABadge(tb testing.TB, file *testPDF) {
	tb.Helper()

	for index, page := range file.pages {
		badges := badgeFills(page)

		for _, run := range page.runs {
			if _, ok := fitWordOf(squash(run.Text)); !ok {
				continue
			}

			inBadge := false

			for _, badge := range badges {
				if badge.Fill.Box.contains(pdfPoint{X: run.X * milli, Y: run.Y * milli}, boxSlack) {
					inBadge = true

					break
				}
			}

			if !inBadge {
				tb.Errorf("page %d draws the fit word %s at (%d, %d) outside every filled badge, so the word does not stand beside its colour and icon",
					index+1, shortened(run.Text), run.X, run.Y)
			}
		}
	}
}

// TestFitBadgeIcons pins the PRD's icons: each badge's icon is its fit's shape,
// drawn in the fit's ink colour, so the three fits differ in shape and not only
// in colour.
//
// Criterion (PRD #584 milestone 1b, Unit): icons — each badge's icon is its fit's
// shape: three points stroked for direct; a 16-sided closed polygon stroked plus
// a filled half for partial; two separate stroked lines for none. No icon is a
// font glyph.
func TestFitBadgeIcons(t *testing.T) {
	t.Parallel()

	// The fits the fixtures draw between them: the report with estimates on and the
	// comparison of three targets draw all three, and the badge check is the same
	// for every one of them.
	seen := make(map[string]bool, len(fitWords))

	for _, variant := range stylingVariants() {
		file := checkPDFStructure(t, variant.render(t))

		for _, page := range file.pages {
			for _, badge := range badgeFills(page) {
				if badge.Word == "" {
					continue
				}

				seen[badge.Word] = true

				checkBadgeIcon(t, variant.name, page, badge)
			}
		}
	}

	for _, fit := range fitWords {
		if !seen[fit] {
			t.Errorf("no report of the fixtures draws a %s badge, so the fit's icon is nowhere tested", fit)
		}
	}
}

// checkBadgeIcon checks one badge's icon against its fit's shape.
func checkBadgeIcon(t *testing.T, variant string, page *pdfPage, badge fitBadge) {
	t.Helper()

	where := fmt.Sprintf("the %s's page %d", variant, badge.Page+1)
	ink := paletteColourOf("--" + strings.ToLower(badge.Fit) + "-ink")

	var strokes, fills []pdfShape

	for index, shape := range page.shapes {
		if index == badge.Index || !badge.Fill.Box.containsBox(shape.Box, boxSlack) {
			continue
		}

		if drawnColour(shape) != ink {
			t.Errorf("%s draws the %s badge's icon in %s, want the fit's ink %s",
				where, badge.Fit, colourName(drawnColour(shape)), colourName(ink))

			continue
		}

		switch shape.Op {
		case "S":
			strokes = append(strokes, shape)
		case "f":
			fills = append(fills, shape)
		}
	}

	stroked := subpathsOf(strokes)

	switch badge.Fit {
	case "Direct":
		if len(stroked) != 1 || len(stroked[0].Points) != 3 {
			t.Errorf("%s draws the Direct badge's icon as %d stroked subpath(s) of %s points, want one polyline of three points (a check mark)",
				where, len(stroked), pointCounts(stroked))
		}

		if len(fills) != 0 {
			t.Errorf("%s fills %d shape(s) inside the Direct badge, but the check mark is a stroked polyline alone",
				where, len(fills))
		}
	case "None":
		if len(stroked) != 2 || len(stroked[0].Points) != 2 || len(stroked[1].Points) != 2 {
			t.Errorf("%s draws the None badge's icon as %d stroked subpath(s) of %s points, want two separate lines of two points each (a cross)",
				where, len(stroked), pointCounts(stroked))
		}

		if len(fills) != 0 {
			t.Errorf("%s fills %d shape(s) inside the None badge, but the cross is two stroked lines alone",
				where, len(fills))
		}
	case "Partial":
		if len(stroked) != 1 || len(stroked[0].Points) != 16 || !stroked[0].Closed {
			t.Errorf("%s draws the Partial badge's icon as %d stroked subpath(s) of %s points, want one closed 16-sided polygon (a circle)",
				where, len(stroked), pointCounts(stroked))
		}

		if len(fills) != 1 {
			t.Errorf("%s fills %d shape(s) inside the Partial badge, want the disc's left half alone",
				where, len(fills))
		}
	}
}

// pointCounts renders the point counts of a path's subpaths for a failure
// message.
func pointCounts(subpaths []pdfSubpath) string {
	counts := make([]string, 0, len(subpaths))
	for _, subpath := range subpaths {
		counts = append(counts, strconv.Itoa(len(subpath.Points)))
	}

	if len(counts) == 0 {
		return "no"
	}

	return strings.Join(counts, ", ")
}

// TestBlockersBoxCarriesItsWarningTriangle pins the PRD's own icon: the blockers
// box's heading carries a filled warning triangle with an exclamation mark of two
// small rectangles inside it.
//
// Criterion (PRD #584 milestone 1b, Unit): icons — the blockers box's heading
// carries the filled triangle; no icon is a font glyph.
func TestBlockersBoxCarriesItsWarningTriangle(t *testing.T) {
	t.Parallel()

	// The report with estimates on holds a blocked resource (queue-jobs), so it
	// has a Blockers box.
	file := checkPDFStructure(t, renderReport(t, estimateAssessment(t, "", true),
		analysis.ReportMeta{InventoryName: inventoryFileName}))

	warnRule := paletteColourOf("--warn-rule")
	white := paletteColourOf("--mast-ink")

	triangles := 0

	for index, page := range file.pages {
		for _, triangle := range page.shapes {
			if triangle.Op != "f" || triangle.Fill != warnRule {
				continue
			}

			if len(triangle.Subpaths) != 1 || len(triangle.Subpaths[0].Points) != 3 {
				continue
			}

			triangles++

			marks := 0

			for _, shape := range page.shapes {
				if shape.Op != "f" || shape.Fill != white || len(shape.Rects) == 0 {
					continue
				}

				if triangle.Box.containsBox(shape.Box, boxSlack) {
					marks += len(shape.Rects)
				}
			}

			if marks < 2 {
				t.Errorf("page %d draws the warning triangle with %d white rectangle(s) inside it, want the exclamation mark's two",
					index+1, marks)
			}
		}
	}

	if triangles == 0 {
		t.Fatalf("the report draws no filled triangle in %s, but the blockers box's heading carries one (PRD, Look, Icons)",
			colourName(warnRule))
	}
}

// pdfBanner is the document's contact banner: the page it sits on, the --mast
// fill that stands behind it, its text runs in reading order and that text.
type pdfBanner struct {
	Page int
	Fill pdfShape
	Runs []pdfTextRun
	Text string
}

// The banner's four runs, exactly as the PRD words them (PRD, Look, The contact
// banner). The apostrophe of the paragraph is the ASCII one, U+0027.
const (
	bannerEyebrow = "Planning a migration?"
	bannerHeading = "Questions about your results, or how to move forward?"
	bannerText    = "We're happy to help you read an assessment, weigh the EU options, or plan the move itself. " +
		"Email info@shift2eu.com."
	bannerAddress = "info@shift2eu.com"
)

// bannerBold and bannerBody are the fonts the banner's runs are drawn in:
// Helvetica-Bold for the eyebrow, the heading and the address, Helvetica for the
// paragraph.
const (
	bannerBold = tableHeaderFont
	bannerBody = "F1"
)

// findBanner returns the document's contact banner: the --mast fill that holds
// the eyebrow.
func findBanner(tb testing.TB, file *testPDF) (pdfBanner, bool) {
	tb.Helper()

	mast := paletteColourOf("--mast")

	for pageIndex, page := range file.pages {
		for _, shape := range page.shapes {
			if shape.Op != "f" || shape.Fill != mast || len(shape.Rects) != 1 {
				continue
			}

			ordered := runsInside(page, shape)
			slices.SortStableFunc(ordered, func(a, b pdfTextRun) int {
				if a.Y != b.Y {
					return b.Y - a.Y
				}

				return a.X - b.X
			})

			for _, run := range ordered {
				if run.Text != bannerEyebrow {
					continue
				}

				return pdfBanner{
					Page: pageIndex, Fill: shape, Runs: ordered, Text: squash(joinedText(ordered)),
				}, true
			}
		}
	}

	return pdfBanner{}, false
}

// bannerPhases are the banner's four runs in the PRD's order, each with the
// colour and font it is drawn in.
var bannerPhases = []struct {
	Text   string
	Colour string
	Font   string
}{
	{Text: bannerEyebrow, Colour: "--mast-link", Font: bannerBold},
	{Text: bannerHeading, Colour: "--mast-ink", Font: bannerBold},
	{Text: bannerText, Colour: "--mast-text", Font: bannerBody},
	{Text: bannerAddress, Colour: "--gold-ink", Font: bannerBold},
}

// assertBanner checks the banner's four runs in the PRD's order, each in its
// colour and font, the 3 pt gold rule along its top and the gold box holding the
// address.
func assertBanner(tb testing.TB, file *testPDF, banner pdfBanner) {
	tb.Helper()

	want := make([]string, 0, len(bannerPhases))
	for _, phase := range bannerPhases {
		want = append(want, phase.Text)
	}

	if expected := squash(strings.Join(want, " ")); banner.Text != expected {
		tb.Errorf("the banner's text is:\n%s\nwant its four runs in the PRD's order:\n%s", banner.Text, expected)
	}

	// Each run belongs to the phase its first word falls in: the four phrases
	// never share a line, so no run straddles two.
	bounds := make([]int, 0, len(bannerPhases))
	words := 0

	for _, phase := range bannerPhases {
		words += len(strings.Fields(phase.Text))
		bounds = append(bounds, words)
	}

	at := 0

	for _, run := range banner.Runs {
		phase := -1

		for index, bound := range bounds {
			if at < bound {
				phase = index

				break
			}
		}

		if phase < 0 {
			tb.Errorf("the banner holds the run %s beyond its four runs", shortened(run.Text))

			break
		}

		want := bannerPhases[phase]

		if run.Colour != paletteColourOf(want.Colour) {
			tb.Errorf("the banner draws %s in %s, want %s (%s)", shortened(run.Text), colourName(run.Colour),
				want.Colour, colourName(paletteColourOf(want.Colour)))
		}

		if run.Font != want.Font {
			tb.Errorf("the banner draws %s in the font /%s, want /%s", shortened(run.Text), run.Font, want.Font)
		}

		at += len(strings.Fields(run.Text))
	}

	assertBannerAddressBox(tb, file, banner)
	assertBannerGoldRule(tb, file, banner)
	assertBannerSpansTheTextWidth(tb, file, banner)
}

// assertBannerAddressBox checks that the address sits inside a --gold fill.
func assertBannerAddressBox(tb testing.TB, file *testPDF, banner pdfBanner) {
	tb.Helper()

	gold := paletteColourOf("--gold")

	addresses := 0

	for _, run := range banner.Runs {
		if run.Text != bannerAddress {
			continue
		}

		addresses++

		inGold := false

		for _, shape := range file.pages[banner.Page].shapes {
			if shape.Op != "f" || shape.Fill != gold || len(shape.Rects) != 1 {
				continue
			}

			if shape.Box.contains(pdfPoint{X: run.X * milli, Y: run.Y * milli}, boxSlack) {
				inGold = true

				break
			}
		}

		if !inGold {
			tb.Errorf("the banner draws the address at (%d, %d) outside its --gold box, but the address sits in a gold box (PRD, Look)",
				run.X, run.Y)
		}
	}

	if addresses != 1 {
		tb.Errorf("the banner holds the address %d time(s), want once in its gold box", addresses)
	}
}

// assertBannerGoldRule checks the 3 pt --gold rule along the banner's top.
func assertBannerGoldRule(tb testing.TB, file *testPDF, banner pdfBanner) {
	tb.Helper()

	fill := banner.Fill.Rects[0]
	gold := paletteColourOf("--gold")
	top := fill.Y + fill.H

	for _, shape := range file.pages[banner.Page].shapes {
		if drawnColour(shape) != gold {
			continue
		}

		for _, rect := range shape.Rects {
			if rect.X != fill.X || rect.W != fill.W {
				continue
			}

			if edge := rect.Y + rect.H; edge < top-4 || edge > top+4 {
				continue
			}

			if shape.Op == "f" && rect.H == 3 || shape.Op == "S" && shape.Width == 3*milli {
				return
			}
		}
	}

	tb.Errorf("the banner carries no 3 pt %s rule along its top (its fill is at y %d pt, %d pt tall)",
		colourName(gold), fill.Y, fill.H)
}

// assertBannerSpansTheTextWidth checks that the banner's fill is as wide as the
// page's own furniture rule, which spans the text width.
func assertBannerSpansTheTextWidth(tb testing.TB, file *testPDF, banner pdfBanner) {
	tb.Helper()

	var (
		header pdfRule
		found  bool
	)

	for _, rule := range file.pages[banner.Page].rules {
		if !found || rule.Y > header.Y {
			header, found = rule, true
		}
	}

	if !found {
		tb.Fatalf("page %d draws no rule to measure the text width by", banner.Page+1)
	}

	fill := banner.Fill.Rects[0]
	if fill.X != header.X || fill.W != header.W {
		tb.Errorf("the banner's fill is x %d pt, %d pt wide, but the page's header rule spans x %d pt, %d pt: the banner is the text width wide (PRD, Look)",
			fill.X, fill.W, header.X, header.W)
	}
}

// TestContactBanner pins the contact banner of every variant: once per document,
// its four runs in the PRD's order, the gold rule along its top, the address in
// its gold box, and that it is on one page.
//
// Criterion (PRD #584 milestone 1b, Unit): the banner — every golden holds, once,
// the four runs "Planning a migration?", "Questions about your results, or how to
// move forward?", the text paragraph (its wrapped lines joined, the ASCII
// apostrophe included) and "info@shift2eu.com", in that order, all on one page and
// inside one --mast rectangle, the address inside a --gold one.
func TestContactBanner(t *testing.T) {
	t.Parallel()

	for _, variant := range stylingVariants() {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()

			file := checkPDFStructure(t, variant.render(t))

			banner, ok := findBanner(t, file)
			if !ok {
				t.Fatalf("the report holds no contact banner: no --mast fill holds the eyebrow %q (PRD, Look, The contact banner)",
					bannerEyebrow)
			}

			assertBanner(t, file, banner)
			assertBannerOnce(t, file)
			assertBannerOnOnePage(t, banner)
		})
	}
}

// assertBannerOnce checks that the eyebrow and the address are drawn once each in
// the whole document.
func assertBannerOnce(tb testing.TB, file *testPDF) {
	tb.Helper()

	eyebrows, addresses := 0, 0

	for _, page := range file.pages {
		for _, run := range page.runs {
			switch run.Text {
			case bannerEyebrow:
				eyebrows++
			case bannerAddress:
				addresses++
			}
		}
	}

	if eyebrows != 1 {
		tb.Errorf("the document draws the banner's eyebrow %d times, want once per document", eyebrows)
	}

	if addresses != 1 {
		tb.Errorf("the document draws the address %d times, want once, in the banner's gold box", addresses)
	}
}

// assertBannerOnOnePage checks that no line of the banner is on another page than
// its fill: the banner is never split.
func assertBannerOnOnePage(tb testing.TB, banner pdfBanner) {
	tb.Helper()

	for _, run := range banner.Runs {
		if run.Page != banner.Page {
			tb.Errorf("the banner draws %s on page %d while its fill is on page %d: the banner is never split across pages (PRD, Look, Boxes across pages)",
				shortened(run.Text), run.Page+1, banner.Page+1)
		}
	}
}

// resultsHeadings are the sections from Summary to Cost estimate (PRD,
// Single-target report).
var resultsHeadings = []string{"Summary", "Resources", "Required changes", "Blockers", "Workloads", "Cost estimate"}

// lastResultsSection returns the index among the document's lines of the last of
// the results headings it shows.
func lastResultsSection(tb testing.TB, file *testPDF) int {
	tb.Helper()

	lines := file.lines()
	last := -1

	for _, heading := range resultsHeadings {
		for index, line := range lines {
			if squash(line.Text) == heading && index > last {
				last = index
			}
		}
	}

	if last < 0 {
		tb.Fatalf("the report holds none of the sections from Summary to Cost estimate")
	}

	return last
}

// bannerLine returns the index among the document's lines of the banner's first
// line.
func bannerLine(tb testing.TB, file *testPDF, banner pdfBanner) int {
	tb.Helper()

	lines := file.lines()
	first := squash(banner.Runs[0].Text)

	for index, line := range lines {
		if line.Page == banner.Page && squash(line.Text) == first {
			return index
		}
	}

	tb.Fatalf("the document holds no line for the banner's first run %q", banner.Runs[0].Text)

	return -1
}

// TestContactBannerPlacement pins where the banner sits: in a single-target
// report after the last of the sections from Summary to Cost estimate and before
// Resource details, and in a comparison after the Comparison section and before
// the first per-target section.
//
// Criterion (PRD #584 milestone 1b, Unit): the banner's placement, with and
// without estimates, with --region, and for a comparison of two and of three
// targets (PRD, Look, The contact banner, Where it sits).
func TestContactBannerPlacement(t *testing.T) {
	t.Parallel()

	t.Run("a single-target report", func(t *testing.T) {
		t.Parallel()

		variants := stylingVariants()

		for _, index := range []int{0, 1, 2, 3} { // estimates on, off, a region, workloads
			variant := variants[index]

			t.Run(variant.name, func(t *testing.T) {
				t.Parallel()

				file := checkPDFStructure(t, variant.render(t))

				banner, ok := findBanner(t, file)
				if !ok {
					t.Fatalf("the report holds no contact banner")
				}

				at := bannerLine(t, file, banner)
				lines := file.lines()

				last := lastResultsSection(t, file)
				details := assertLine(t, lines, "Resource details")

				if at <= last {
					t.Errorf("the banner is on line %d of the document, at or before the last of the sections from Summary to Cost estimate (line %d): it follows them",
						at, last)
				}

				if at >= details {
					t.Errorf("the banner is on line %d of the document, at or after the Resource details heading (line %d): it precedes Resource details",
						at, details)
				}
			})
		}
	})

	t.Run("a comparison", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			keys      []string
			inventory []inventory.Resource
		}{
			{name: "three targets", keys: threeTargetKeys(), inventory: comparisonResources()},
			{name: "two targets", keys: []string{ovhcloudKey, scalewayKey}, inventory: twoTargetResources()},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				c := assessment.Compare(assessTargets(t, newInventory(t, sourceName, test.inventory), test.keys))

				file := checkPDFStructure(t, renderComparison(t, c, analysis.ReportMeta{InventoryName: inventoryFileName}))

				banner, ok := findBanner(t, file)
				if !ok {
					t.Fatalf("the comparison holds no contact banner")
				}

				lines := file.lines()
				at := bannerLine(t, file, banner)

				comparison := assertLine(t, lines, "Comparison")
				firstTarget := assertLine(t, lines, pdfreport.EscapeValue(c.Targets[0].Assessment.Target))

				if at <= comparison {
					t.Errorf("the banner is on line %d of the comparison, at or before the Comparison heading (line %d)",
						at, comparison)
				}

				if at >= firstTarget {
					t.Errorf("the banner is on line %d of the comparison, at or after the first per-target section (line %d): it precedes every per-target section",
						at, firstTarget)
				}

				// Nothing of the Comparison section's own tables stands after the
				// banner: the banner follows the section whole.
				for index := at + 1; index < firstTarget; index++ {
					if isPageFurniture(file, lines[index]) {
						continue
					}

					if !bannerLineHolds(banner, lines[index]) {
						t.Errorf("the line %q stands after the banner but before the first per-target section (line %d), so the banner is not after the Comparison section",
							squash(lines[index].Text), index)

						break
					}
				}
			})
		}
	})
}

// bannerLineHolds reports whether one of the banner's runs is drawn on the line.
func bannerLineHolds(banner pdfBanner, line pdfLine) bool {
	for _, run := range banner.Runs {
		if run.Page != line.Page || strings.TrimSpace(run.Text) == "" {
			continue
		}

		if strings.Contains(squash(line.Text), squash(run.Text)) {
			return true
		}
	}

	return false
}

// isPageFurniture reports whether the line is one of its page's furniture lines:
// its header above the page's top rule or its footer below its bottom rule. The
// writer draws a page's body first and its header and footer after it, so in the
// document's own order they stand between the body lines of the page they belong
// to, and a check on what follows a body line has to pass over them.
func isPageFurniture(file *testPDF, line pdfLine) bool {
	if line.Page < 0 || line.Page >= len(file.pages) {
		return false
	}

	lowest, highest, ok := pageBand(file.pages[line.Page])
	if !ok {
		return false
	}

	return line.Y <= lowest || line.Y >= highest
}

// TestContactBannerMovesWholeToTheNextPage pins the PRD's rule that the banner is
// never split: a report whose content is filled so the banner would straddle a
// page break shows it whole on the next page.
//
// Criterion (PRD #584 milestone 1b, Unit): boxes across pages — the banner is
// never split: when it does not fit in the space left, it moves whole to the next
// page.
func TestContactBannerMovesWholeToTheNextPage(t *testing.T) {
	t.Parallel()

	moved := 0

	for step := 1; step <= bannerSweepCount; step++ {
		inv := newInventory(t, sourceName, paddedResources(step))
		a := assessment.Assess(inv, testutil.ReportCatalog(t))

		file := checkPDFStructure(t, renderReport(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

		banner, ok := findBanner(t, file)
		if !ok {
			t.Fatalf("the report of %d resources holds no contact banner", step)
		}

		// The banner is whole on one page in every case of the sweep.
		assertBannerOnOnePage(t, banner)

		// The banner follows the last section the report shows, Workloads here.
		lines := file.lines()
		workloads := assertLineWithin(t, lines, "Workloads", banner.Page)

		if at := bannerLine(t, file, banner); at <= workloads {
			t.Errorf("the report of %d resources draws the banner on line %d, at or before the Workloads heading (line %d)", step, at, workloads)
		}

		if linePage(t, lines, workloads) < banner.Page {
			moved++
		}
	}

	if moved == 0 {
		t.Fatalf("no report of the sweep of %d resource counts moved the banner to a page after the last results section, so the sweep never reached the page break this test is about",
			bannerSweepCount)
	}
}

// bannerSweepCount is how many resource counts the banner's page-break sweep
// tries: one more padded resource moves the sections under the resources table
// down by one table line (13 pt), which walks the banner through every position
// above a page's bottom margin.
const bannerSweepCount = 40

// assertLineWithin returns the index among the lines of the last line holding want
// up to the banner's page.
func assertLineWithin(tb testing.TB, lines []pdfLine, want string, page int) int {
	tb.Helper()

	found := -1

	for index, line := range lines {
		if line.Page > page {
			break
		}

		if squash(line.Text) == squash(want) {
			found = index
		}
	}

	if found < 0 {
		rendered := make([]string, 0, len(lines))
		for _, line := range lines {
			rendered = append(rendered, squash(line.Text))
		}

		tb.Fatalf("the PDF holds no line %q on or before page %d; its lines are:\n%s",
			want, page+1, strings.Join(rendered, "\n"))
	}

	return found
}

// linePage returns the page of the line at index among the document's lines.
func linePage(tb testing.TB, lines []pdfLine, index int) int {
	tb.Helper()

	if index < 0 || index >= len(lines) {
		tb.Fatalf("the document holds no line %d", index)
	}

	return lines[index].Page
}

// blockerResources is an inventory whose every resource is blocked: a messaging
// queue that no offering serves, so the Blockers section is longer than a page
// and its box has to continue on the next one.
func blockerResources() []inventory.Resource {
	resources := make([]inventory.Resource, 0, blockerCount)

	for i := range blockerCount {
		resources = append(resources, inventory.Resource{
			ID:       fmt.Sprintf("queue-%04d", i),
			RawType:  "AWS::SQS::Queue",
			Category: taxonomy.MessagingQueue,
			Region:   "eu-west-1",
		})
	}

	return resources
}

// blockerCount is how many blocked resources the blockers box's fixture holds:
// more than one page of blocker lines.
const blockerCount = 70

// TestBlockerBoxIsDrawnOnEachPageItTouches pins the PRD's rule that a box that
// continues on a new page is drawn again there: its fill is drawn on each page for
// the part of the box on that page, so no text of the box stands outside its fill.
//
// Criterion (PRD #584 milestone 1b, Unit): boxes across pages — a box that spans
// pages has its frame drawn on each page it touches, and its heading or header row
// is never the last text on a page.
func TestBlockerBoxIsDrawnOnEachPageItTouches(t *testing.T) {
	t.Parallel()

	a := assessment.Assess(newInventory(t, sourceName, blockerResources()), testutil.ReportCatalog(t))

	file := checkPDFStructure(t, renderReport(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	if len(a.Blockers) < blockerCount {
		t.Fatalf("the fixture holds %d blockers, want the %d blocked resources", len(a.Blockers), blockerCount)
	}

	lines := file.lines()
	from := assertLine(t, lines, "Blockers")
	to := assertLine(t, lines, "Resource details")

	if to <= from {
		t.Fatalf("the Blockers heading is not before the Resource details heading")
	}

	warnBG := paletteColourOf("--warn-bg")

	pages := make(map[int]bool)

	for _, blocker := range a.Blockers {
		line, ok := blockerLine(t, lines, from, to, blocker.ResourceID)
		if !ok {
			continue
		}

		pages[line.Page] = true

		if !lineIsInsideAFill(file, line, warnBG) {
			t.Errorf("page %d draws the blocker %s outside the --warn-bg fill of its box: a box that spans pages is drawn again on each page it touches (PRD, Look, Boxes across pages)",
				line.Page+1, shortened(squash(line.Text)))
		}
	}

	if len(pages) < 2 {
		t.Fatalf("the blockers box reaches %d page(s), so the report does not break inside it: the fixture has to make the box span pages", len(pages))
	}
}

// blockerLine returns the line of the Blockers section holding the resource's ID.
func blockerLine(tb testing.TB, lines []pdfLine, from, to int, resourceID string) (pdfLine, bool) {
	tb.Helper()

	for _, line := range lines[from+1 : to] {
		if strings.Contains(squash(line.Text), squash(resourceID)) {
			return line, true
		}
	}

	tb.Errorf("the Blockers section draws no line for the blocked resource %q", resourceID)

	return pdfLine{}, false
}

// lineIsInsideAFill reports whether the whole line lies inside a fill of the
// colour given: a box that spans pages is drawn again on each page, so the fill
// of the box holds the line on the page it is drawn on, from the line's first
// character to its last. The fill has to be the box's own — the text width wide,
// which no fit badge reaches — and to hold every run of the line, not only the
// point the leftmost one starts at: --warn-bg shares its value with --partial-bg,
// so a Partial badge's fill carries the same colour, and it would satisfy a check
// that asked for one point inside a fill of that colour.
func lineIsInsideAFill(file *testPDF, line pdfLine, colour pdfRGB) bool {
	if line.Page < 0 || line.Page >= len(file.pages) {
		return false
	}

	page := file.pages[line.Page]

	left, right := math.MaxInt, math.MinInt

	for _, run := range page.runs {
		if run.Y != line.Y || strings.TrimSpace(run.Text) == "" {
			continue
		}

		start := run.X * milli
		end := start + pdfreport.TextWidth(run.Font, run.Size, run.Text)

		left, right = min(left, start), max(right, end)
	}

	if left > right {
		return false
	}

	for _, shape := range page.shapes {
		if shape.Op != "f" || shape.Fill != colour || len(shape.Rects) != 1 {
			continue
		}

		rect := shape.Rects[0]
		if rect.W != boxBodyWidth {
			// A badge's fill, a box's left rule or a top rule: narrower than the
			// box.
			continue
		}

		if left >= rect.X*milli && right <= (rect.X+rect.W)*milli {
			return true
		}
	}

	return false
}
