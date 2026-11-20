package docs_test

import (
	"os"
	"strings"
	"testing"
)

// countingRule is the counting rule CLAUDE.md's `### Dispatcher` subsection
// states under its values table, as the fragments that carry its meaning, in
// the order the paragraph says them (issue #348): which limit counts, what it
// is counted against, what happens to the units still running after 09:30,
// what that means for new units, and that a finished PR is never held back.
// They are read as one sequence, so a rewrite that keeps the words but loses
// the rule fails.
var countingRule = []string{
	"the one in force when a unit is dispatched",
	"the waiting PRs plus the running units that will open a PR plus the new unit",
	"after 09:30, units still running finish and their PRs count",
	"nothing new is dispatched until that count, the new unit included, fits the day limit",
	"a finished PR is never held back or closed to get under it",
}

// collapsedText is a block of Markdown read as one line: text is its non-blank
// lines with their whitespace collapsed and joined by single spaces, and lines
// holds, for each of those lines, the 0-based index in the block of the line
// and the offset of its first word in text. A sentence wraps over several
// lines, so it is found in text and mapped back to a line through lines.
type collapsedText struct {
	text  string
	lines []collapsedLine
}

// collapsedLine is one non-blank line of a collapsedText: where it starts in
// the block and where its first word starts in the text.
type collapsedLine struct {
	line   int
	offset int
}

// lineOf returns the 0-based index in the block of the line offset falls on:
// the last line to start at or before it.
func (c collapsedText) lineOf(offset int) int {
	line := 0
	for _, l := range c.lines {
		if l.offset > offset {
			break
		}
		line = l.line
	}
	return line
}

// collapseLines reads lines as one line.
func collapseLines(lines []string) collapsedText {
	var (
		text  strings.Builder
		spans []collapsedLine
	)
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte(' ')
		}
		spans = append(spans, collapsedLine{line: i, offset: text.Len()})
		text.WriteString(strings.Join(fields, " "))
	}
	return collapsedText{text: text.String(), lines: spans}
}

// TestCLAUDEDispatcherStatesTheCountingRule fails when CLAUDE.md's
// `### Dispatcher` subsection stops stating the counting rule of the review
// limit, or states its parts in another order (issue #348). The values table's
// row only says the limit is "counted as the sentence below the table says";
// this test pins that sentence, inside the subsection and below the table, so
// removing it, rewording it or moving it elsewhere fails.
func TestCLAUDEDispatcherStatesTheCountingRule(t *testing.T) {
	t.Parallel()

	const heading = "### Dispatcher"
	fsys := os.DirFS(repoRoot)
	body, start, ok := subsection(readDoc(t, fsys, claudeMD), "Dispatcher")
	if !ok {
		t.Fatalf("%s: no %q subsection, which holds the counting rule of the review limit (issue #348)", claudeMD, heading)
	}
	collapsed := collapseLines(body)

	// Each fragment is searched for after the end of the one before it, so the
	// paragraph must state them in this order.
	at := 0
	for _, fragment := range countingRule {
		i := strings.Index(collapsed.text[at:], fragment)
		if i < 0 {
			if strings.Contains(collapsed.text, fragment) {
				t.Fatalf("%s: the counting rule of the review limit states %q out of order, after the fragment before it: the rule reads %s (issue #348)",
					claudeMD, fragment, strings.Join(countingRule, " -> "))
			}
			t.Fatalf("%s: the counting rule of the review limit is missing %q, which the %q subsection must state (issue #348)",
				claudeMD, fragment, heading)
		}
		at += i + len(fragment)
	}

	// The rule belongs to the table that refers to it: the review limit's row
	// says the limit is counted as the sentence below the table says.
	table := claudeDispatcherValues(t, fsys)
	if len(table.rows) == 0 {
		t.Fatalf("%s:%d: the %q table lists no values, so no row refers to the counting rule (issue #348)", claudeMD, table.line, heading)
	}
	lastRow := table.rows[len(table.rows)-1].line
	// at is the end of the last fragment, which sits in the rule's paragraph;
	// body line i is file line start+i+1.
	ruleLine := start + collapsed.lineOf(at-1) + 1
	if ruleLine < lastRow {
		t.Errorf("%s:%d: the counting rule sits above the table's last row on line %d, but the table says the limit is counted as the sentence below the table (issue #348)",
			claudeMD, ruleLine, lastRow)
	}
}
