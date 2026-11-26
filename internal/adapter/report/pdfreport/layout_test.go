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
	// sweep starts at one line, because a chain that starts at the very top of a
	// page is the page break ensure makes when moving it could not help, not the
	// one this test is about.
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
func TestLayoutKeepsAHeadingOffTheFootOfAPage(t *testing.T) {
	t.Parallel()

	moved := 0

	// The sweep walks every body line of the filler and every second heading line
	// of the block: the page break the block asks for spans several lines, so a
	// step of two still walks it.
	for filler := 1; filler <= fillerLines; filler++ {
		for lines := 1; lines <= blockLines; lines += 2 {
			pages := drawLayout(filler, lines)

			if len(pages) > 1 && !pageDrawsTheChain(pages[0]) && pageDrawsTheChain(pages[1]) {
				moved++
			}

			checkNoLayoutPageEndsWithAHeading(t, filler, lines, pages)
			checkLayoutDrawsItsTextOnce(t, filler, lines, pages)
		}
	}

	if moved == 0 {
		t.Fatalf("no case of the sweep of %d filler line counts and %d block line counts moved a kept heading chain to a new page, so the sweep never reached the page break this test is about",
			fillerLines, (blockLines+1)/2)
	}

	t.Logf("%d cases of the sweep moved a kept heading chain to a new page", moved)
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
