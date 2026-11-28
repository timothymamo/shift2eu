package pdfreport_test

// This file pins the layout's own rule that no page ends with a heading at the
// level of the document itself. It draws into a document through the hooks in
// export_test.go, so it can put a kept heading chain and a block that outgrows
// the page together: no report of today's data does (a pending chain stays under
// 30 pt), and only the layout can show what its page break does then.
//
// document.ensure moves the headings a body line has not followed yet to the new
// page with what follows them. When the moved chain and the block that asked for
// the page break still do not fit under it, the block's own lines are what has to
// give: they must not end a page with a heading (PRD, Tables and page breaks),
// and no text may be lost or cut at the break.

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// The sweep of TestLayoutKeepsAHeadingOffTheFootOfAPage: a chain of kept
// headings, then one block heading, drawn after a number of body lines that
// walks the chain down the page.
const (
	// chainHeadings is how many headings the test draws with no body line
	// between them: four block-size headings of two lines each hold 136 pt of
	// the page, more than keepWithHeading (36 pt), so the chain is what the
	// block after it has to fit under.
	chainHeadings = 4
	// chainLines is how many lines each heading of the chain holds.
	chainLines = 2
	// headingLineChars is how many characters of a value one line of a
	// block-size heading holds: Courier's fixed advance of 6 pt at 10 pt over
	// the 495 pt content width, 82 characters.
	headingLineChars = 82
	// fillerLines is how many body lines the sweep draws before the chain: it
	// walks the chain from just under the top of the page down to its foot. The
	// sweep counts from one line, because a chain that starts at the very top of
	// a page is the other side of the page break: document.ensure moves it
	// nowhere (kept.y >= d.top), and the sweep of the test's second subtest, with
	// no filler line at all, is the one that walks it.
	fillerLines = 56
	// blockLines is how many heading lines the block after the chain holds. The
	// sweep covers a block that fits under the chain, one that does not fit on a
	// page with it, and one taller than a page, which the layout draws as a
	// paragraph instead.
	blockLines = 56
)

// The body lines the sweep draws before and after the headings, so every
// heading of the document has a body line under it. Neither holds a "c", the
// chain's character, and neither is empty.
const (
	fillerText = "Filler line."
	afterText  = "After the block."
)

// chainValue is the value every heading of the chain holds: exactly chainLines
// lines of a block-size heading.
var chainValue = strings.Repeat("c", headingLineChars*chainLines)

// TestLayoutKeepsAHeadingOffTheFootOfAPage pins the layout's answer to a kept
// heading chain that leaves no room for the block it is kept with: every page of
// the document must end with a body line, never with a heading, and every
// character drawn must be there exactly once.
//
// Two sweeps reach that break from both sides of document.ensure: a chain the
// break moves to a new page, walked down the page by the filler, and a chain
// that starts at the very top of a page already, so the break moves it nowhere.
func TestLayoutKeepsAHeadingOffTheFootOfAPage(t *testing.T) {
	t.Parallel()

	t.Run("chain walked down the page", func(t *testing.T) {
		t.Parallel()

		fillers := make([]int, 0, fillerLines)
		for filler := 1; filler <= fillerLines; filler++ {
			fillers = append(fillers, filler)
		}

		moved, _ := sweepLayout(t, fillers)

		if moved == 0 {
			t.Fatalf("no case of the sweep of %d filler line counts and %d block line counts moved a kept heading chain to a new page, so the sweep never reached the page break this test is about",
				fillerLines, (blockLines+1)/2)
		}

		t.Logf("%d cases of the sweep moved a kept heading chain to a new page", moved)
	})

	t.Run("chain at the top of a page", func(t *testing.T) {
		t.Parallel()

		// No filler line at all: the chain is the first thing drawn, so it sits
		// at the top of the first page and the block after it reaches the page
		// break with kept.y >= d.top, where document.ensure moves it nowhere.
		_, breaks := sweepLayout(t, []int{0})

		if breaks == 0 {
			t.Fatalf("no case of the sweep of a chain at the top of a page and %d block line counts drew a second page, so the sweep never reached a page break",
				(blockLines+1)/2)
		}

		t.Logf("%d cases of the sweep drew a second page under a chain at the top of a page", breaks)
	})
}

// sweepLayout runs the sweep for every filler line count in fillers and every
// second heading line of the block, which still walks a page break that spans
// several lines. It fails the test for the layout's invariants of its own and
// returns how many cases moved a kept heading chain to a new page and how many
// drew more than one page.
func sweepLayout(t *testing.T, fillers []int) (moved, breaks int) {
	t.Helper()

	for _, filler := range fillers {
		for lines := 1; lines <= blockLines; lines += 2 {
			pages := drawLayout(filler, lines)

			if len(pages) > 1 {
				breaks++
			}

			if len(pages) > 1 && !pageDrawsTheChain(pages[0]) && pageDrawsTheChain(pages[1]) {
				moved++
			}

			checkNoLayoutPageEndsWithAHeading(t, filler, lines, pages)
			checkLayoutDrawsItsTextOnce(t, filler, lines, pages)
		}
	}

	return moved, breaks
}

// drawLayout draws the test's document: filler body lines, a chain of kept
// headings with no body line after them, a block heading of lines lines, and the
// body line that follows the block.
func drawLayout(filler, lines int) [][]pdfreport.TestRun {
	d := pdfreport.NewTestDocument()

	for range filler {
		d.Body(fillerText)
	}

	for range chainHeadings {
		d.ValueHeading(3, chainValue)
	}

	d.ValueHeading(3, strings.Repeat("a", headingLineChars*lines))
	d.Body(afterText)

	return d.Pages()
}

// pageDrawsTheChain reports whether a page draws any line of the chain's
// headings: every character of a chain heading is a "c", and no other line of
// the document holds one.
func pageDrawsTheChain(page []pdfreport.TestRun) bool {
	for _, run := range page {
		if run.Text != "" && strings.Trim(run.Text, "c") == "" {
			return true
		}
	}

	return false
}

// checkNoLayoutPageEndsWithAHeading fails the test when the lowest line of a page
// of the drawn document is a heading: a heading never ends a page.
func checkNoLayoutPageEndsWithAHeading(t *testing.T, filler, lines int, pages [][]pdfreport.TestRun) {
	t.Helper()

	for index, page := range pages {
		last := lowestLine(page)
		if len(last) == 0 {
			continue
		}

		heading := true

		for _, run := range last {
			if !headingSizes[run.Size] {
				heading = false
			}
		}

		if !heading {
			continue
		}

		t.Fatalf("layout: %d filler line(s) and a block heading of %d line(s) draw %d page(s), and page %d ends its body with the heading %s at y %d pt in the %d pt size, so the heading alone sits at the foot of the page: a heading never ends a page (PRD, Tables and page breaks)",
			filler, lines, len(pages), index+1, shortened(last[0].Text), last[0].Y, last[0].Size)
	}
}

// lowestLine returns the runs of a page's lowest line, the one drawn last.
func lowestLine(page []pdfreport.TestRun) []pdfreport.TestRun {
	if len(page) == 0 {
		return nil
	}

	lowest := page[0].Y
	for _, run := range page {
		lowest = min(lowest, run.Y)
	}

	var last []pdfreport.TestRun

	for _, run := range page {
		if run.Y == lowest {
			last = append(last, run)
		}
	}

	return last
}

// checkLayoutDrawsItsTextOnce fails the test when the document's text, joined
// page by page and run by run, is not what was drawn into it: a page break that
// moved a heading chain must neither lose a character nor draw one twice.
func checkLayoutDrawsItsTextOnce(t *testing.T, filler, lines int, pages [][]pdfreport.TestRun) {
	t.Helper()

	want := strings.Repeat(fillerText, filler) + strings.Repeat(chainValue, chainHeadings) +
		strings.Repeat("a", headingLineChars*lines) + afterText

	var drawn strings.Builder

	for _, page := range pages {
		for _, run := range page {
			drawn.WriteString(run.Text)
		}
	}

	if got := drawn.String(); got != want {
		t.Fatalf("layout: %d filler line(s) and a block heading of %d line(s) draw %d characters over %d page(s), want %d: the document lost, cut or repeated text at its page breaks",
			filler, lines, len(got), len(pages), len(want))
	}
}

// The sweep of TestLayoutKeepsABoxHeaderOffTheFootOfAPage: a box of boxLines
// item lines, drawn after a number of body lines that walks the box's header
// down the page.
const (
	// boxFillerLines is how many body lines the sweep draws before the box: one
	// more moves the box down by one body line (12 pt), from the top of the page
	// to its foot, past the point where the heading and its first item no longer
	// both fit and the box moves whole to the next page.
	boxFillerLines = 56
	// boxLines is how many item lines the box holds: more than a page's body, so
	// a box that starts high on a page continues on the next one.
	boxLines = 70
)

// boxHeadingText heads the box of the sweep.
const boxHeadingText = "A boxed section near the foot of a page"

// boxItemText returns the text of the box's item at index, counted from 0.
func boxItemText(index int) string {
	return fmt.Sprintf("Box line %d.", index+1)
}

// The box's fill is the report's own content area (layout.go's marginLeft and
// contentWidth), which the sweep's document draws the box across.
const (
	boxBodyLeft  = 50
	boxBodyWidth = 495
)

// TestLayoutKeepsABoxHeaderOffTheFootOfAPage pins the layout's box rule at the
// level that can put a box's header at the foot of a page: a box's heading is
// never the last text on a page, and a box that continues on the next page has
// its frame drawn on every page its text reaches, so no text of it stands
// outside its fill.
//
// Criterion (PRD #584 milestone 1b, Unit): boxes across pages — in a layout test
// that builds a box near a page's end, a box's heading is never the last text on
// a page, and every text run of a box that spans pages lies inside a fill of that
// box drawn on its own page.
func TestLayoutKeepsABoxHeaderOffTheFootOfAPage(t *testing.T) {
	t.Parallel()

	moved, spanning := 0, 0

	for filler := 0; filler <= boxFillerLines; filler++ {
		pages, shapes := drawBoxLayout(filler)

		checkBoxLinesAreDrawnOnce(t, filler, pages)

		headingPage := -1

		for index, page := range pages {
			if last := lowestLine(page); len(last) > 0 && last[0].Text == boxHeadingText {
				t.Errorf("layout: %d filler line(s) draw %d page(s), and page %d ends its text with the box's heading, so the heading alone sits at the foot of the page: a box's heading never ends a page (PRD, Look, Boxes across pages)",
					filler, len(pages), index+1)
			}

			for _, run := range page {
				if !isBoxRun(run) {
					continue
				}

				if run.Text == boxHeadingText {
					headingPage = index
				}

				if !insideTheBoxFrame(shapes[index], run) {
					t.Errorf("layout: %d filler line(s) draw the box's line %s on page %d outside the box's own fill, so the box is not drawn again on that page (PRD, Look, Boxes across pages)",
						filler, shortened(run.Text), index+1)
				}
			}
		}

		if headingPage < 0 {
			t.Fatalf("layout: %d filler line(s) draw no box heading", filler)
		}

		if headingPage > 0 {
			moved++
		}

		if boxLinePages(pages) > 1 {
			spanning++
		}
	}

	if moved == 0 {
		t.Fatalf("no case of the sweep of %d filler line counts moved the box to a page after its filler, so the sweep never reached the page break this test is about",
			boxFillerLines)
	}

	if spanning == 0 {
		t.Fatalf("no case of the sweep of %d filler line counts drew the box on more than one page, so no box of the sweep continued on a new page",
			boxFillerLines)
	}

	t.Logf("%d cases of the sweep moved the box to a new page; %d drew it on more than one page", moved, spanning)
}

// drawBoxLayout draws the test's document: filler body lines, then the box.
func drawBoxLayout(filler int) ([][]pdfreport.TestRun, [][]pdfreport.TestShape) {
	d := pdfreport.NewTestDocument()

	for range filler {
		d.Body(fillerText)
	}

	d.Box(boxHeadingText, boxLines)

	return d.Pages(), d.Shapes()
}

// isBoxRun reports whether the run belongs to the box of the sweep: its heading,
// one of its item lines or an item's bullet.
func isBoxRun(run pdfreport.TestRun) bool {
	return run.Text == boxHeadingText || strings.HasPrefix(run.Text, "Box line ") || run.Text == "•"
}

// insideTheBoxFrame reports whether the run starts inside a fill of the box's
// own width on the run's page.
func insideTheBoxFrame(shapes []pdfreport.TestShape, run pdfreport.TestRun) bool {
	for _, shape := range shapes {
		if shape.Op != "f" {
			continue
		}

		for _, rect := range shape.Rects {
			if rect.X != boxBodyLeft || rect.W != boxBodyWidth {
				continue
			}

			if run.X >= rect.X && run.X <= rect.X+rect.W && run.Y >= rect.Y && run.Y <= rect.Y+rect.H {
				return true
			}
		}
	}

	return false
}

// boxLinePages returns how many pages draw a line of the box.
func boxLinePages(pages [][]pdfreport.TestRun) int {
	count := 0

	for _, page := range pages {
		for _, run := range page {
			if isBoxRun(run) {
				count++

				break
			}
		}
	}

	return count
}

// checkBoxLinesAreDrawnOnce fails the test when a page break inside the box lost
// or repeated one of its lines.
func checkBoxLinesAreDrawnOnce(t *testing.T, filler int, pages [][]pdfreport.TestRun) {
	t.Helper()

	counts := make(map[string]int)

	for _, page := range pages {
		for _, run := range page {
			counts[run.Text]++
		}
	}

	if got := counts[boxHeadingText]; got != 1 {
		t.Errorf("layout: %d filler line(s) draw the box's heading %d time(s), want once", filler, got)
	}

	for index := range boxLines {
		if got := counts[boxItemText(index)]; got != 1 {
			t.Errorf("layout: %d filler line(s) draw the box's item %s %d time(s), want once: a page break inside a box must neither lose nor repeat a line",
				filler, boxItemText(index), got)

			break
		}
	}
}

// The sweep of TestLayoutKeepsATileRowAndAWorkloadCardWhole: a block that moves
// whole, drawn after a number of body lines that walks it down the page.
const (
	// blockFillerLines is how many body lines the sweep draws before the block:
	// the same sweep helps the box of TestLayoutKeepsABoxHeaderOffTheFootOfAPage,
	// one more line moving the block down by one body line (12 pt).
	blockFillerLines = 56
	// cardHeadingText heads the workload card of the sweep.
	cardHeadingText = "A workload card near the foot of a page"
	// cardLines is how many body lines the card holds: few enough that the card
	// fits a page's body, so it moves whole rather than continuing.
	cardLines = 3
)

// tileTexts are the words and figures the tile row of the sweep draws: the three
// fits' badges, the counts, the two captions.
var tileTexts = map[string]bool{
	"Direct": true, "Partial": true, "None": true,
	"1": true, "2": true, "3": true, "6": true,
	"of 6 resources": true, "resources in total": true,
}

// TestLayoutKeepsATileRowAndAWorkloadCardWhole pins the layout's rule that a tile
// row and a workload card that fits a page's body move whole to the next page:
// none of their text reaches two pages. A report's Workloads section can put a
// card at the foot of a page, but no report of today's data puts the tile row
// there, so only the layout walks it through a page break.
//
// Criterion (PRD #584 milestone 1b, Unit): boxes across pages — the banner, a tile
// row and a workload card that fits a page's body never span two pages.
func TestLayoutKeepsATileRowAndAWorkloadCardWhole(t *testing.T) {
	t.Parallel()

	t.Run("a tile row", func(t *testing.T) {
		t.Parallel()

		moved := 0

		for filler := 0; filler <= blockFillerLines; filler++ {
			pages := drawTilesLayout(filler)

			if page := blockPage(t, filler, "tile row", pages, isTileRun); page > 0 {
				moved++
			}
		}

		if moved == 0 {
			t.Fatalf("no case of the sweep of %d filler line counts moved the tile row to a page after its filler, so the sweep never reached the page break this test is about",
				blockFillerLines)
		}

		t.Logf("%d cases of the sweep moved the tile row to a new page", moved)
	})

	t.Run("a workload card", func(t *testing.T) {
		t.Parallel()

		moved := 0

		for filler := 0; filler <= blockFillerLines; filler++ {
			pages, shapes := drawCardLayout(filler)

			page := blockPage(t, filler, "workload card", pages, isCardRun)
			if page > 0 {
				moved++
			}

			for _, run := range pages[page] {
				if !isCardRun(run) {
					continue
				}

				if !insideTheBoxFrame(shapes[page], run) {
					t.Errorf("layout: %d filler line(s) draw the card's line %s on page %d outside the card's own fill",
						filler, shortened(run.Text), page+1)
				}
			}
		}

		if moved == 0 {
			t.Fatalf("no case of the sweep of %d filler line counts moved the workload card to a page after its filler, so the sweep never reached the page break this test is about",
				blockFillerLines)
		}

		t.Logf("%d cases of the sweep moved the workload card to a new page", moved)
	})
}

// drawTilesLayout draws the test's document: filler body lines, then the tile
// row.
func drawTilesLayout(filler int) [][]pdfreport.TestRun {
	d := pdfreport.NewTestDocument()

	for range filler {
		d.Body(fillerText)
	}

	d.Tiles()

	return d.Pages()
}

// drawCardLayout draws the test's document: filler body lines, then the card.
func drawCardLayout(filler int) ([][]pdfreport.TestRun, [][]pdfreport.TestShape) {
	d := pdfreport.NewTestDocument()

	for range filler {
		d.Body(fillerText)
	}

	d.Card(cardHeadingText, cardLines)

	return d.Pages(), d.Shapes()
}

// isTileRun reports whether the run belongs to the tile row of the sweep.
func isTileRun(run pdfreport.TestRun) bool {
	return tileTexts[run.Text]
}

// isCardRun reports whether the run belongs to the card of the sweep: its
// heading, one of its lines or a line's bullet.
func isCardRun(run pdfreport.TestRun) bool {
	return run.Text == cardHeadingText || strings.HasPrefix(run.Text, "Card line ") || run.Text == "•"
}

// blockPage returns the page every run of a block that moves whole is drawn on,
// and fails the test for a block whose text reaches a second page.
func blockPage(t *testing.T, filler int, name string, pages [][]pdfreport.TestRun, isBlock func(pdfreport.TestRun) bool) int {
	t.Helper()

	found := -1

	for index, page := range pages {
		for _, run := range page {
			if !isBlock(run) {
				continue
			}

			if found < 0 {
				found = index

				continue
			}

			if index != found {
				t.Fatalf("layout: %d filler line(s) draw the %s on page %d and page %d: a block that fits a page's body never spans two pages (PRD, Look, Boxes across pages)",
					filler, name, found+1, index+1)
			}
		}
	}

	if found < 0 {
		t.Fatalf("layout: %d filler line(s) draw no %s", filler, name)
	}

	return found
}

// The sweep of TestLayoutOpensABoxRightAfterAHeading: a heading, then a box
// opened straight after it with no measure of the two first, drawn after a
// number of body lines that walks the heading down the page.
const (
	// boxAfterHeadingFillerLines is how many body lines the sweep draws before
	// the heading: one more moves the heading down by one body line (12 pt),
	// from the top of the page to its foot, past the point where the box's first
	// item no longer fits under the heading.
	boxAfterHeadingFillerLines = 56
	// boxAfterHeadingLines is how many item lines the box holds: more than the
	// room a heading near the foot of a page leaves, so the box continues on the
	// next page.
	boxAfterHeadingLines = 12
)

// boxAfterHeadingSectionText heads the section of the sweep: the heading the box
// is opened under.
const boxAfterHeadingSectionText = "A section near the foot of a page"

// boxAfterHeadingText heads the box of the sweep, drawn inside it.
const boxAfterHeadingText = "A box opened right after the section heading"

// boxAfterHeadingItemText returns the text of the box's item at index, counted
// from 0.
func boxAfterHeadingItemText(index int) string {
	return fmt.Sprintf("Box line %d.", index+1)
}

// TestLayoutOpensABoxRightAfterAHeading pins the layout at the one place a box's
// opening meets a kept heading chain: a section heading, then a box opened
// straight after it, its own heading drawn inside the box and its items after
// that, with no measure of the heading, the box and its first item first. No
// report of today does (report.noticeBox measures its heading and the box's first
// item together, and draw.go's drawBox leaves the measuring to its caller), but
// the page break that the box's own heading makes then reaches document.ensure
// with a box open and a chain pending, where cutting the old page back to the
// chain's shape index leaves the box's fill placeholder behind and draw.go's
// closePart indexes a shape its page no longer holds. The section heading's own
// keepWithHeading only covers a box opening shorter than keepWithHeading points,
// which is why the sweep draws a heading inside the box: a box with no block of
// its own under the section heading keeps room for its first line and never
// reaches the cut.
//
// Criterion (PRD #584 milestone 1b, Unit): boxes across pages — a box opened
// under a heading never panics the layout, never leaves a heading alone at the
// foot of a page, and has its frame drawn on every page the box reaches; a page
// break inside it neither loses nor repeats a line.
func TestLayoutOpensABoxRightAfterAHeading(t *testing.T) {
	t.Parallel()

	breaks, spanning := 0, 0

	for filler := 0; filler <= boxAfterHeadingFillerLines; filler++ {
		pages, shapes, panicked := drawBoxAfterHeading(filler)

		if panicked != nil {
			t.Errorf("layout: %d filler line(s) draw a section heading, open a box straight after it and draw the box's own heading in it, and the layout panics: %v: a box opened under a heading must not panic the layout, whatever the page break under it does (PRD, Look, Boxes across pages)",
				filler, panicked)

			continue
		}

		if len(pages) > 1 {
			breaks++
		}

		checkBoxAfterHeadingLinesAreDrawnOnce(t, filler, pages)

		reached := 0

		for index, page := range pages {
			if last := lowestLine(page); len(last) > 0 &&
				(last[0].Text == boxAfterHeadingSectionText || last[0].Text == boxAfterHeadingText) {
				t.Errorf("layout: %d filler line(s) draw %d page(s), and page %d ends its text with the heading %s, so the heading alone sits at the foot of the page: a heading never ends a page (PRD, Tables and page breaks)",
					filler, len(pages), index+1, shortened(last[0].Text))
			}

			drawn := 0

			for _, run := range page {
				if !isBoxAfterHeadingRun(run) {
					continue
				}

				drawn++

				if !insideTheBoxFrame(shapes[index], run) {
					t.Errorf("layout: %d filler line(s) draw the box's line %s on page %d outside the box's own fill, so the box is not drawn again on that page (PRD, Look, Boxes across pages)",
						filler, shortened(run.Text), index+1)
				}
			}

			if drawn > 0 {
				reached++
			}
		}

		if reached > 1 {
			spanning++
		}
	}

	if breaks == 0 {
		t.Fatalf("no case of the sweep of %d filler line counts drew a second page, so the sweep never reached the page break this test is about",
			boxAfterHeadingFillerLines)
	}

	if spanning == 0 {
		t.Fatalf("no case of the sweep of %d filler line counts drew the box on more than one page, so no box of the sweep continued on a new page",
			boxAfterHeadingFillerLines)
	}

	t.Logf("%d cases of the sweep drew a second page; %d drew the box on more than one page", breaks, spanning)
}

// drawBoxAfterHeading draws the test's document — filler body lines, the
// heading, then the box — and recovers a panic of the layout, which it returns:
// a panic in one case of the sweep must not hide the others.
func drawBoxAfterHeading(filler int) (pages [][]pdfreport.TestRun, shapes [][]pdfreport.TestShape, panicked any) {
	d := pdfreport.NewTestDocument()

	for range filler {
		d.Body(fillerText)
	}

	defer func() { panicked = recover() }()

	d.BoxAfterHeading(boxAfterHeadingSectionText, boxAfterHeadingText, boxAfterHeadingLines)

	return d.Pages(), d.Shapes(), nil
}

// isBoxAfterHeadingRun reports whether the run belongs to the box of the sweep:
// its heading, one of its item lines or an item's bullet.
func isBoxAfterHeadingRun(run pdfreport.TestRun) bool {
	return run.Text == boxAfterHeadingText || strings.HasPrefix(run.Text, "Box line ") || run.Text == "•"
}

// checkBoxAfterHeadingLinesAreDrawnOnce fails the test when a page break under
// the heading lost or repeated one of the box's lines or the heading itself.
func checkBoxAfterHeadingLinesAreDrawnOnce(t *testing.T, filler int, pages [][]pdfreport.TestRun) {
	t.Helper()

	counts := make(map[string]int)

	for _, page := range pages {
		for _, run := range page {
			counts[run.Text]++
		}
	}

	if got := counts[boxAfterHeadingSectionText]; got != 1 {
		t.Errorf("layout: %d filler line(s) draw the heading the box is opened under %d time(s), want once", filler, got)
	}

	if got := counts[boxAfterHeadingText]; got != 1 {
		t.Errorf("layout: %d filler line(s) draw the box's own heading %d time(s), want once", filler, got)
	}

	for index := range boxAfterHeadingLines {
		if got := counts[boxAfterHeadingItemText(index)]; got != 1 {
			t.Errorf("layout: %d filler line(s) draw the box's item %s %d time(s), want once: a page break under a heading must neither lose nor repeat a line",
				filler, boxAfterHeadingItemText(index), got)

			break
		}
	}
}

// tileRects returns the outlines of the tiles of a page's tile row, in x order:
// the row's rectangles are the only stroked paths its page draws.
func tileRects(shapes []pdfreport.TestShape) []pdfreport.TestRect {
	rects := make([]pdfreport.TestRect, 0, len(shapes))

	for _, shape := range shapes {
		if shape.Op != "S" || len(shape.Rects) != 1 {
			continue
		}

		rects = append(rects, shape.Rects[0])
	}

	slices.SortFunc(rects, func(a, b pdfreport.TestRect) int { return a.X - b.X })

	return rects
}

// tileOf returns the tile whose outline holds the run's start, and whether there
// is one.
func tileOf(tiles []pdfreport.TestRect, run pdfreport.TestRun) (pdfreport.TestRect, bool) {
	for _, tile := range tiles {
		if run.X >= tile.X && run.X < tile.X+tile.W {
			return tile, true
		}
	}

	return pdfreport.TestRect{}, false
}

// isFigureRun reports whether the run's text holds only what a figure holds: the
// euro sign, digits, the thousands' commas and the decimal point. A tile's label
// and its caption hold letters.
func isFigureRun(run pdfreport.TestRun) bool {
	if run.Text == "" {
		return false
	}

	for _, r := range run.Text {
		if !strings.ContainsRune("0123456789,.\u20ac", r) {
			return false
		}
	}

	return true
}

// TestLayoutKeepsATilesFigureInsideItsTile pins the summary's tile row against
// the figure no tile of today's reports holds: a monthly total longer than the
// tile's inner width at the smallest size draw.go's tileRow shrinks a figure to,
// which runs past its tile's outline into the tile beside it (draw.go, tileRow:
// the figure is one line, and it shrinks no further than tileFigureMin).
//
// Criterion (PRD #584 milestone 1b, Unit): boxes across pages — the summary's
// tiles: no run of a tile reaches outside that tile's outline, and the whole
// figure is still drawn, however many lines it takes.
func TestLayoutKeepsATilesFigureInsideItsTile(t *testing.T) {
	t.Parallel()

	// The largest amount the domain's Money allows, 9223372036854775807/1 EUR,
	// written with the thousands' commas euros gives it: 28 characters, wider
	// than a tile of a five-tile row at tileFigureMin.
	figure := euros(estimate.Ratio(math.MaxInt64, 1))

	if got := utf8.RuneCountInString(figure); got < 25 {
		t.Fatalf("the sweep's figure %q holds %d characters, want 25 or more: a shorter figure fits a tile at the smallest size, so the sweep would never reach the overflow",
			figure, got)
	}

	d := pdfreport.NewTestDocument()
	d.TilesWithEstimate(figure)

	pages, shapes := d.Pages(), d.Shapes()

	if len(pages) != 1 {
		t.Fatalf("the tile row of a five-tile summary draws %d page(s), want one: the sweep draws the row alone", len(pages))
	}

	tiles := tileRects(shapes[0])
	if len(tiles) != 5 {
		t.Fatalf("the tile row draws %d tile outlines, want the summary's five (three fits, the total and the estimate)", len(tiles))
	}

	for _, run := range pages[0] {
		tile, ok := tileOf(tiles, run)
		if !ok {
			t.Fatalf("the tile row draws the run %s at x %d pt, outside every tile's outline", shortened(run.Text), run.X)
		}

		left := run.X * milli
		right := left + pdfreport.TextWidth(run.Font, run.Size, run.Text)

		if left < tile.X*milli || right > (tile.X+tile.W)*milli {
			t.Errorf("the tile whose outline runs from x %d pt to x %d pt draws the run %s from x %s pt to x %s pt in the %d pt size, so the run reaches past its tile (PRD, Look: a tile's figure stays inside its tile)",
				tile.X, tile.X+tile.W, shortened(run.Text), points(left), points(right), run.Size)
		}
	}

	var drawn strings.Builder

	estimate := tiles[len(tiles)-1]

	for _, run := range pages[0] {
		if run.X < estimate.X || run.X >= estimate.X+estimate.W || !isFigureRun(run) {
			continue
		}

		drawn.WriteString(run.Text)
	}

	if got := drawn.String(); got != figure {
		t.Errorf("the estimate's tile draws the figure %q over its runs, want %q: a figure too wide for its tile must be wrapped onto more lines inside it, never cut short or run past it",
			got, figure)
	}
}

// tileRunDescent is the clearance the test keeps between a tile's lowest
// baseline and the tile's bottom edge, in points: the descent of Helvetica at the
// caption's 8 pt, rounded up (the font's standard metrics give 0.212 × 8 pt =
// 1.7 pt), so a line whose baseline sits on the tile's edge is not taken for a
// line inside it.
const tileRunDescent = 2

// TestLayoutKeepsATilesCaptionInsideItsTile pins the summary's tile row for a
// caption that wraps onto more lines than a tile holds by default: a tile lays
// its label, its figure and then its caption downwards from its top edge (draw.go,
// tileRow), so a caption continues below its figure's last line, and a row grown
// for the figures alone leaves its last line on the tile's outline. A five-tile
// row is narrow enough for "of N resources" to take three lines once N is long.
//
// Criterion (PRD #584 milestone 1b, Unit): boxes across pages — the summary's
// tiles: every run of a tile stays inside that tile's outline in both
// directions, every tile of the row keeps one height, and the whole caption is
// drawn, however many lines it takes.
func TestLayoutKeepsATilesCaptionInsideItsTile(t *testing.T) {
	t.Parallel()

	// eighteenDigits is a resource count of eighteen digits, which the total's
	// figure still holds on one line at the smallest size tileRow shrinks a
	// figure to (18 × 4.448 pt = 80.1 pt inside a five-tile row's 82 pt), so the
	// row does not grow for the figure, and its caption "of 999999999999999999
	// resources" takes three lines of 82 pt: "of", the count, and "resources".
	const eighteenDigits = 999_999_999_999_999_999

	// sixteenDigits is the count whose caption takes two lines: "of 9999999999999999
	// resources" wraps once and grows the row by one caption leading. Its second
	// line clears the tile's bottom edge even in a row grown for the figures alone,
	// so it pins the near side of that boundary, and the eighteen-digit case the
	// far side.
	const sixteenDigits = 9_999_999_999_999_999

	tests := []struct {
		name     string
		draw     func(*pdfreport.TestDocument)
		captions []string
	}{
		{
			name: "an estimate beside four counts",
			draw: func(d *pdfreport.TestDocument) {
				d.TilesWithEstimate(euros(estimate.Ratio(math.MaxInt64, 1)))
			},
			captions: []string{
				"of 6 resources", "of 6 resources", "of 6 resources", "resources in total", "per month, estimated",
			},
		},
		{
			name: "counts of sixteen digits",
			draw: func(d *pdfreport.TestDocument) {
				d.TilesWithEstimateAndCounts(1, 2, 3, sixteenDigits, "€1,234.00")
			},
			captions: []string{
				"of 9999999999999999 resources",
				"of 9999999999999999 resources",
				"of 9999999999999999 resources",
				"resources in total",
				"per month, estimated",
			},
		},
		{
			name: "counts of eighteen digits",
			draw: func(d *pdfreport.TestDocument) {
				d.TilesWithEstimateAndCounts(1, 2, 3, eighteenDigits, "€1,234.00")
			},
			captions: []string{
				"of 999999999999999999 resources",
				"of 999999999999999999 resources",
				"of 999999999999999999 resources",
				"resources in total",
				"per month, estimated",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			d := pdfreport.NewTestDocument()
			test.draw(d)

			pages, shapes := d.Pages(), d.Shapes()

			if len(pages) != 1 {
				t.Fatalf("the tile row draws %d page(s), want one: the sweep draws the row alone", len(pages))
			}

			tiles := tileRects(shapes[0])
			if len(tiles) != len(test.captions) {
				t.Fatalf("the tile row draws %d tile outlines, want %d", len(tiles), len(test.captions))
			}

			for _, tile := range tiles[1:] {
				if tile.Y != tiles[0].Y || tile.H != tiles[0].H {
					t.Errorf("the tile at x %d pt runs from y %d pt to y %d pt, and the row's first tile runs from y %d pt to y %d pt: every tile of the row keeps one height",
						tile.X, tile.Y, tile.Y+tile.H, tiles[0].Y, tiles[0].Y+tiles[0].H)
				}
			}

			for _, run := range pages[0] {
				tile, ok := tileOf(tiles, run)
				if !ok {
					t.Fatalf("the tile row draws the run %s at x %d pt, outside every tile's outline", shortened(run.Text), run.X)
				}

				left := run.X * milli
				right := left + pdfreport.TextWidth(run.Font, run.Size, run.Text)

				if left < tile.X*milli || right > (tile.X+tile.W)*milli {
					t.Errorf("the tile whose outline runs from x %d pt to x %d pt draws the run %s from x %s pt to x %s pt in the %d pt size, so the run reaches past its tile",
						tile.X, tile.X+tile.W, shortened(run.Text), points(left), points(right), run.Size)
				}

				if run.Y <= tile.Y+tileRunDescent {
					t.Errorf("the tile whose outline runs from y %d pt to y %d pt draws the run %s on the baseline %d pt, at or under the tile's bottom edge plus the %d pt descent margin, so the caption lands on the tile's outline",
						tile.Y, tile.Y+tile.H, shortened(run.Text), run.Y, tileRunDescent)
				}

				if run.Y >= tile.Y+tile.H {
					t.Errorf("the tile whose outline runs from y %d pt to y %d pt draws the run %s on the baseline %d pt, at or above the tile's top edge",
						tile.Y, tile.Y+tile.H, shortened(run.Text), run.Y)
				}
			}

			for i, tile := range tiles {
				var drawn strings.Builder

				for _, run := range pages[0] {
					if isCaptionRun(run) && run.X >= tile.X && run.X < tile.X+tile.W {
						drawn.WriteString(run.Text)
					}
				}

				if got := strings.TrimRight(drawn.String(), " "); got != test.captions[i] {
					t.Errorf("the tile at x %d pt draws the caption %q over its runs, want %q: a caption too wide for its tile must be wrapped onto more lines inside it, never cut short",
						tile.X, got, test.captions[i])
				}
			}
		})
	}
}

// isCaptionRun reports whether the run is one line of a tile's caption: the
// report's own words in the furniture size, which a tile's label and figure, in
// Helvetica-Bold, are not.
func isCaptionRun(run pdfreport.TestRun) bool { return run.Font == "F1" && run.Size == 8 }
