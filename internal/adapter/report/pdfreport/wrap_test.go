package pdfreport_test

// This file finds one value's drawing in the document and checks the two rules
// the PRD's Tables and page breaks gives for it: the text extracted for the
// value, joined across its wrapped lines (across page breaks too), is exactly
// the value's escaped form, and no wrap point falls inside one of the escaper's
// escape forms, so a reader never sees half an escape at a line's end.
//
// The writer draws each wrapped line of a value as its own Tj, at the x of the
// value's column, or at a line's start when the value starts one, so a value's
// lines are found by that x on later lines.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// TestReportWrapsOnEscapeBoundaries checks the worst case of the PRD's Tables
// and page breaks on the narrowest column a report has, the resources table's
// ID cell with estimates on: a value of text.MaxLen bytes made only of
// characters the escaper rewrites grows to thousands of characters, wraps over
// many lines, and every line still ends on an escape boundary. Only an escape
// wider than the whole column could be split, and none of these is.
func TestReportWrapsOnEscapeBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "2048 backslashes become 4096 characters", value: strings.Repeat(`\`, textMaxLen)},
		{name: "1024 two-byte runes become 6144 characters", value: strings.Repeat("\u0416", textMaxLen/2)},
		{name: "512 four-byte runes become 5120 characters", value: strings.Repeat("\U0001f600", textMaxLen/4)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			inv := newInventory(t, sourceName, []inventory.Resource{{
				ID:           test.value,
				RawType:      "AWS::EC2::Instance",
				Category:     taxonomy.ComputeVM,
				Region:       "eu-west-1",
				Capabilities: []taxonomy.Capability{taxonomy.IPv6},
			}})

			a := assessment.AssessWith(inv, testutil.ReportCatalog(t), assessment.Options{Estimate: true})
			file := checkPDFStructure(t, renderReport(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

			wrapped := checkWrappedValue(t, file, test.value)
			if len(wrapped.chunks) < 2 {
				t.Errorf("the value is drawn on %d line, so it never reached the wrap width; its escaped form is %d characters",
					len(wrapped.chunks), len(pdfreport.EscapeValue(test.value)))
			}
		})
	}
}

// TestReportDrawsAnOverTallHeadingValueAsAParagraph pins the layout's fallback
// for a heading taller than a page: a resource's ID of text.MaxLen bytes makes
// one, and it is drawn at the body size in Courier, whole and escaped, as a
// paragraph rather than at a heading's size, so the lines a heading would need
// never run over a page's foot. The resources table's cell and the resource's
// own heading both show the value, and neither is cut, so its drawn pieces join
// to its escaped form twice over.
func TestReportDrawsAnOverTallHeadingValueAsAParagraph(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("\u0416", textMaxLen/2)

	inv := newInventory(t, sourceName, []inventory.Resource{{
		ID:           value,
		RawType:      "AWS::EC2::Instance",
		Category:     taxonomy.ComputeVM,
		Region:       "eu-west-1",
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
	}})

	a := assessment.AssessWith(inv, testutil.ReportCatalog(t), assessment.Options{Estimate: true})
	file := checkPDFStructure(t, renderReport(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	escaped := pdfreport.EscapeValue(value)

	// A piece of the value starts at an atom of its escaped form, so each starts
	// with its first escape, \u0416: the report's own words never do.
	const atom = `\u0416`

	var drawn []string

	for _, run := range file.allRuns() {
		if !strings.HasPrefix(run.Text, atom) || !strings.Contains(escaped, run.Text) {
			continue
		}

		if run.Font != "F3" {
			t.Errorf("page %d draws the value %s in the font resource /%s, want /F3, Courier, which every value from the inventory is drawn in",
				run.Page+1, shortened(run.Text), run.Font)
		}

		if headingSizes[run.Size] {
			t.Errorf("page %d draws the value %s at %d pt, one of the heading sizes, so a heading too tall for a page was drawn as a heading: the heading runs over the page's foot whatever is done, and the value is drawn at the body size as a paragraph instead",
				run.Page+1, shortened(run.Text), run.Size)
		}

		drawn = append(drawn, run.Text)
	}

	if got := strings.Join(drawn, ""); got != escaped+escaped {
		t.Fatalf("the document draws %d characters of the value in full, so its drawings do not join to its %d-character escaped form twice over: the resources table's cell and the resource's own heading each show the value, and neither is cut",
			len(got), len(escaped))
	}
}

// wrappedValue is one value as the writer drew it.
type wrappedValue struct {
	// chunks is one line of the value per Tj, in the order drawn.
	chunks []string
	// joined is the chunks joined: what a reader sees of the value, across its
	// wrapped lines.
	joined string
}

// allRuns returns every text run of the document, in the order the writer drew
// them.
func (f *testPDF) allRuns() []pdfTextRun {
	var runs []pdfTextRun

	for _, page := range f.pages {
		runs = append(runs, page.runs...)
	}

	return runs
}

// collectWrappedValue finds the drawing of escaped in the document and returns
// its lines, or fails the test when the document does not draw it.
func collectWrappedValue(tb testing.TB, file *testPDF, escaped string) wrappedValue {
	tb.Helper()

	runs := file.allRuns()
	if len(runs) == 0 {
		tb.Fatalf("the PDF draws no text, so it cannot show %q", escaped)
	}

	lineOf := make([]int, len(runs))
	firstOfLine := make([]bool, len(runs))

	line := -1
	previousPage, previousY := -1, 0

	for i, run := range runs {
		if i == 0 || run.Page != previousPage || run.Y != previousY {
			line++
			firstOfLine[i] = true
			previousPage, previousY = run.Page, run.Y
		}

		lineOf[i] = line
	}

	// A drawing of the value starts at a run whose text starts its escaped form
	// and ends where the escaped form ends. Several runs start the same way: a
	// wrapped chunk that repeats the escaped form's first atoms starts it too,
	// and a value drawn twice (in the title block and in About this report, say)
	// starts twice. Only a whole drawing follows the escaped form to its end, so
	// every such run is tried, the longest first because a value's first line is
	// usually its longest, and the first drawing that reaches the end is the
	// value.
	var starts []int

	for i, run := range runs {
		if run.Text != "" && strings.HasPrefix(escaped, run.Text) {
			starts = append(starts, i)
		}
	}

	if len(starts) == 0 {
		tb.Fatalf("the PDF draws no run that starts the escaped value %q; its runs are:\n%s",
			escaped, renderRuns(runs))
	}

	slices.SortStableFunc(starts, func(a, b int) int {
		if longer := len(runs[b].Text) - len(runs[a].Text); longer != 0 {
			return longer
		}

		return a - b
	})

	firstFailure := ""

	for _, start := range starts {
		chunks, followed, why := followValue(runs, lineOf, firstOfLine, escaped, start)
		if followed {
			return wrappedValue{chunks: chunks, joined: strings.Join(chunks, "")}
		}

		if firstFailure == "" {
			firstFailure = why
		}
	}

	tb.Fatalf("%s", firstFailure)

	return wrappedValue{}
}

// followValue follows the drawing of escaped from the run at start, and reports
// its lines and true, or false and why the drawing does not reach the value's
// end.
func followValue(runs []pdfTextRun, lineOf []int, firstOfLine []bool, escaped string, start int) ([]string, bool, string) {
	chunks := []string{runs[start].Text}
	position := len(runs[start].Text)
	lastLine := lineOf[start]
	startX := runs[start].X

	for position < len(escaped) {
		found := -1

		for j := start + 1; j < len(runs); j++ {
			if lineOf[j] <= lastLine {
				continue
			}

			run := runs[j]
			if run.Text == "" {
				continue
			}

			// A wrapped line starts at the value's column, or at the start of a
			// line when the value starts one.
			if run.X != startX && !firstOfLine[j] {
				continue
			}

			if strings.HasPrefix(escaped[position:], run.Text) {
				found = j

				break
			}
		}

		if found < 0 {
			return nil, false, fmt.Sprintf("the value %q is drawn as %q on line %d and stops there: no later line at its column or at a line's start continues it; its runs are:\n%s",
				escaped, strings.Join(chunks, ""), lastLine+1, renderRuns(runs))
		}

		chunks = append(chunks, runs[found].Text)
		position += len(runs[found].Text)
		lastLine = lineOf[found]
	}

	return chunks, true, ""
}

// renderRuns renders the text runs for a failure, one per line.
func renderRuns(runs []pdfTextRun) string {
	rendered := make([]string, 0, len(runs))

	for _, run := range runs {
		rendered = append(rendered, fmt.Sprintf("page %d x %d y %d font %s text %s",
			run.Page+1, run.X, run.Y, run.Font, strconv.Quote(run.Text)))
	}

	return strings.Join(rendered, "\n")
}

// escapeAtoms splits an escaped value into the units a wrap may not split: one
// escape form per atom, and one character otherwise. It fails the test on an
// escape form the PRD does not name or one that is not lowercase hex.
func escapeAtoms(tb testing.TB, escaped string) []string {
	tb.Helper()

	var atoms []string

	for i := 0; i < len(escaped); {
		if escaped[i] == '\\' {
			size := 0

			switch {
			case i+1 >= len(escaped):
				tb.Fatalf("the escaped value %q ends in a lone backslash", escaped)
			case escaped[i+1] == 'u':
				size = 6
			case escaped[i+1] == 'U':
				size = 10
			case escaped[i+1] == 'x':
				size = 4
			case escaped[i+1] == '\\':
				size = 2
			default:
				tb.Fatalf("the escaped value %q holds the escape %q, which is not one of \\uXXXX, \\UXXXXXXXX, \\xXX and \\\\",
					escaped, escaped[i:])
			}

			if i+size > len(escaped) {
				tb.Fatalf("the escaped value %q ends inside the escape %q", escaped, escaped[i:])
			}

			atom := escaped[i : i+size]

			if size > 2 {
				for _, c := range atom[2:] {
					if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
						tb.Fatalf("the escape %q is not written with lowercase hex digits", atom)
					}
				}
			}

			atoms = append(atoms, atom)
			i += size

			continue
		}

		_, size := utf8.DecodeRuneInString(escaped[i:])
		atoms = append(atoms, escaped[i:i+size])
		i += size
	}

	return atoms
}

// checkWrappedValue checks, for one value the document shows, that the text
// extracted for it, joined across its wrapped lines and page breaks, is exactly
// its escaped form, and that every line ends on an escape boundary. It returns
// the value's lines, so a test can go on to check the literal string form of
// each.
func checkWrappedValue(tb testing.TB, file *testPDF, value string) wrappedValue {
	tb.Helper()

	escaped := pdfreport.EscapeValue(value)

	wrapped := collectWrappedValue(tb, file, escaped)
	if wrapped.joined != escaped {
		tb.Errorf("the text extracted for the value %q is %q across its %d line(s), want its escaped form %q; the lines are:\n%s",
			value, wrapped.joined, len(wrapped.chunks), escaped, strings.Join(wrapped.chunks, "\n"))
	}

	// Where the atoms end: a wrap may end a line there and nowhere else. atomEnd
	// walks the atoms in step with the lines, so a long value stays linear.
	atoms := escapeAtoms(tb, escaped)

	atom, atomEnd := 0, 0
	offset := 0

	for _, chunk := range wrapped.chunks {
		offset += len(chunk)

		for atom < len(atoms) && atomEnd < offset {
			atomEnd += len(atoms[atom])
			atom++
		}

		if atomEnd == offset {
			continue
		}

		broken := atoms[atom-1]
		if len(broken) > 16 {
			broken = broken[:16] + "..."
		}

		tb.Errorf("a wrapped line ends inside the escape %q of the value %q: the line stops after %q (%d of %d characters); the value's lines are:\n%s",
			broken, value, chunk, offset, len(escaped), strings.Join(wrapped.chunks, "\n"))
	}

	return wrapped
}
