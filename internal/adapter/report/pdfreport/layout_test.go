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
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
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
