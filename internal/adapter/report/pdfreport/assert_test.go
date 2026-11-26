package pdfreport_test

// This file reads the text the writer draws back out of the parsed pages: the
// lines, the order of the sections' headings, the page header and footer of
// every page, and the header row a table repeats when it continues on a new
// page.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
)

// pdfLine is one line of one page: the text runs on one baseline, in x order,
// joined into the text a reader sees on that line.
type pdfLine struct {
	// Page is the page, counted from 0 in the page tree's order.
	Page int
	// Text is the joined text of the runs.
	Text string
}

// lines returns every line of the document, in the order the writer drew them:
// page by page, then in the content stream's order.
func (f *testPDF) lines() []pdfLine {
	var lines []pdfLine

	for pageIndex, page := range f.pages {
		var current []pdfTextRun

		flush := func() {
			if len(current) == 0 {
				return
			}

			slices.SortStableFunc(current, func(a, b pdfTextRun) int { return a.X - b.X })

			parts := make([]string, 0, len(current))
			for _, run := range current {
				parts = append(parts, run.Text)
			}

			lines = append(lines, pdfLine{Page: pageIndex, Text: strings.Join(parts, "")})
			current = nil
		}

		for _, run := range page.runs {
			if len(current) > 0 && run.Y != current[0].Y {
				flush()
			}

			current = append(current, run)
		}

		flush()
	}

	return lines
}

// text returns the document's text: every line, in order, one per line.
func (f *testPDF) text() string {
	lines := f.lines()

	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}

	return strings.Join(texts, "\n")
}

// squashed returns the document's text with every run of white space collapsed
// to one space, so a check on a phrase does not depend on where a line wrap
// fell.
func (f *testPDF) squashed() string { return squash(f.text()) }

// pageText returns one page's text, one line per line.
func (f *testPDF) pageText(page int) string {
	var texts []string

	for _, line := range f.lines() {
		if line.Page == page {
			texts = append(texts, line.Text)
		}
	}

	return strings.Join(texts, "\n")
}

// squash collapses every run of white space to one ASCII space.
func squash(text string) string { return strings.Join(strings.Fields(text), " ") }

// assertShown checks that the document shows every want, compared with white
// space squashed, so a phrase may wrap anywhere.
func assertShown(tb testing.TB, file *testPDF, wants ...string) {
	tb.Helper()

	text := file.squashed()

	for _, want := range wants {
		if !strings.Contains(text, squash(want)) {
			tb.Errorf("the PDF does not show %q; its text is:\n%s", want, text)
		}
	}
}

// assertNotShown checks that the document shows none of the wants.
func assertNotShown(tb testing.TB, file *testPDF, unwanted ...string) {
	tb.Helper()

	text := file.squashed()

	for _, want := range unwanted {
		if strings.Contains(text, squash(want)) {
			tb.Errorf("the PDF shows %q, which this variant must not show; its text is:\n%s", want, text)
		}
	}
}

// assertLine checks that the document holds a line whose squashed text is want,
// and returns the line's index among lines.
func assertLine(tb testing.TB, lines []pdfLine, want string) int {
	tb.Helper()

	for i, line := range lines {
		if squash(line.Text) == squash(want) {
			return i
		}
	}

	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, squash(line.Text))
	}

	tb.Fatalf("the PDF holds no line %q; its lines are:\n%s", want, strings.Join(rendered, "\n"))

	return -1
}

// assertOrder checks that the document holds each line, in the order given, so
// the sections of a report are in the order the PRD fixes.
func assertOrder(tb testing.TB, file *testPDF, wants ...string) {
	tb.Helper()

	lines := file.lines()
	searchFrom := 0

	for _, want := range wants {
		found := -1

		for i := searchFrom; i < len(lines); i++ {
			if squash(lines[i].Text) == squash(want) {
				found = i

				break
			}
		}

		if found < 0 {
			rendered := make([]string, 0, len(lines))
			for _, line := range lines {
				rendered = append(rendered, squash(line.Text))
			}

			tb.Fatalf("the PDF holds no line %q after the earlier headings; its lines are:\n%s",
				want, strings.Join(rendered, "\n"))
		}

		searchFrom = found + 1
	}
}

// assertTitleBlock checks that page 1 starts with the title block: the title
// naming the subject, which is a value and therefore shown escaped.
func assertTitleBlock(tb testing.TB, file *testPDF, subject string) {
	tb.Helper()

	lines := file.lines()
	if len(lines) == 0 {
		tb.Fatalf("the PDF has no lines")
	}

	shown := squash(pdfreport.EscapeValue(subject))

	first := squash(lines[0].Text)
	if !strings.Contains(first, "Migration assessment") || !strings.Contains(first, shown) {
		tb.Fatalf("the PDF's first line is %q, want the title block naming %q", first, shown)
	}
}

// assertPageFurniture checks every page's header and footer: the header names
// the report and its subject, and the footer gives this page's number of the
// total and the version line the constructor took (Decided, as recommended, 11).
func assertPageFurniture(tb testing.TB, file *testPDF, subject string) {
	tb.Helper()

	if len(file.pages) == 0 {
		tb.Fatalf("the PDF has no pages")
	}

	shown := squash(pdfreport.EscapeValue(subject))

	for page := range file.pages {
		text := squash(file.pageText(page))

		if !strings.Contains(text, "shift2eu migration assessment") {
			tb.Errorf("page %d does not carry the header %q; its text is:\n%s",
				page+1, "shift2eu migration assessment", text)
		}

		if !strings.Contains(text, shown) {
			tb.Errorf("page %d's header does not name %q; its text is:\n%s", page+1, shown, text)
		}

		want := fmt.Sprintf("Page %d of %d", page+1, len(file.pages))
		if !strings.Contains(text, want) {
			tb.Errorf("page %d does not carry the footer %q; its text is:\n%s", page+1, want, text)
		}

		if !strings.Contains(text, squash(versionLine)) {
			tb.Errorf("page %d's footer does not carry the version line %q; its text is:\n%s",
				page+1, versionLine, text)
		}
	}
}

// sectionText returns the text between two headings: the lines after from,
// squashed and joined, up to the line to or the end of the document when to is
// empty.
func sectionText(tb testing.TB, file *testPDF, from, to string) string {
	tb.Helper()

	lines := file.lines()
	start := assertLine(tb, lines, from)

	stop := len(lines)
	if to != "" {
		stop = assertLine(tb, lines, to)
		if stop <= start {
			tb.Fatalf("the %q heading is not after the %q heading", to, from)
		}
	}

	texts := make([]string, 0, stop-start-1)
	for _, line := range lines[start+1 : stop] {
		texts = append(texts, squash(line.Text))
	}

	return strings.Join(texts, " ")
}

// assertTableHeaderRepeats checks that the table between the from and to
// headings continues on at least one further page, and that every page it
// continues on repeats its header row: each of cells is shown on every page the
// table's lines reach (PRD, Tables and page breaks).
func assertTableHeaderRepeats(tb testing.TB, file *testPDF, from, to string, cells []string) {
	tb.Helper()

	lines := file.lines()
	start := assertLine(tb, lines, from)
	stop := assertLine(tb, lines, to)

	if stop <= start {
		tb.Fatalf("the %q heading is not after the %q heading", to, from)
	}

	pages := []int{}

	for _, line := range lines[start+1 : stop] {
		if !slices.Contains(pages, line.Page) {
			pages = append(pages, line.Page)
		}
	}

	if len(pages) < 2 {
		tb.Fatalf("the table under %q reaches %d page, so the report does not break inside it", from, len(pages))
	}

	for _, page := range pages {
		text := squash(file.pageText(page))

		for _, cell := range cells {
			if !strings.Contains(text, squash(cell)) {
				tb.Errorf("page %d continues the table under %q without repeating its header row: the cell %q is missing; its text is:\n%s",
					page+1, from, cell, text)
			}
		}
	}
}
