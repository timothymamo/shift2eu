package docs_test

import (
	"os"
	"strings"
	"testing"
)

// countingRule is the counting rule CLAUDE.md's `### Dispatcher` subsection
// states in the paragraph right below its values table, as the fragments that
// carry its meaning, in the order the paragraph says them (issue #348): which
// limit counts, what it is counted against, what happens to the units still
// running after 09:30, what that means for new units, and that a finished PR is
// never held back. They are read as one sequence, so a rewrite that keeps the
// words but loses the rule fails.
var countingRule = []string{
	"the one in force when a unit is dispatched",
	"the waiting PRs plus the running units that will open a PR plus the new unit",
	"after 09:30, units still running finish and their PRs count",
	"nothing new is dispatched until that count, the new unit included, fits the day limit",
	"a finished PR is never held back or closed to get under it",
}

// TestCLAUDEDispatcherStatesTheCountingRule fails when CLAUDE.md's
// `### Dispatcher` subsection stops stating the counting rule of the review
// limit in the paragraph right below its values table, or states its parts in
// another order (issue #348). The table's row only says the limit is "counted
// as the sentence below the table says", so this test pins that sentence where
// the row sends the reader: removing it, rewording it, moving it elsewhere, or
// splitting it into two paragraphs fails.
func TestCLAUDEDispatcherStatesTheCountingRule(t *testing.T) {
	t.Parallel()

	const heading = "### Dispatcher"
	fsys := os.DirFS(repoRoot)
	data := readDoc(t, fsys, claudeMD)
	body, start, ok := subsection(data, "Dispatcher")
	if !ok {
		t.Fatalf("%s: no %q subsection, which holds the counting rule of the review limit (issue #348)", claudeMD, heading)
	}
	// The table's helper reads CLAUDE.md itself: the file is read a second time
	// here rather than reshaping the shared helper for one caller.
	table := claudeDispatcherValues(t, fsys)
	if len(table.rows) == 0 {
		t.Fatalf("%s:%d: the %q table lists no values, so no row refers to the counting rule (issue #348)", claudeMD, table.line, heading)
	}
	lastRow := table.rows[len(table.rows)-1].line

	// The rule is the paragraph the table's review-limit row points below the
	// table: the subsection's lines after the table's last row (body line i is
	// file line start+i+1), leading blank lines skipped, up to the next blank
	// line.
	var lines []string
	for _, line := range body[lastRow-start:] {
		if strings.TrimSpace(line) == "" {
			if len(lines) == 0 {
				continue // the blank line right below the table
			}
			break // the paragraph ends at the next blank line
		}
		lines = append(lines, line)
	}
	rule := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	if rule == "" {
		t.Fatalf("%s:%d: no paragraph below the %q table, but its review-limit row says the limit is counted as the sentence below the table says (issue #348)",
			claudeMD, lastRow, heading)
	}

	// Each fragment is searched for after the end of the one before it, so the
	// paragraph must state them in this order.
	at := 0
	for _, fragment := range countingRule {
		i := strings.Index(rule[at:], fragment)
		if i < 0 {
			if strings.Contains(rule, fragment) {
				t.Fatalf("%s:%d: the counting rule of the review limit states %q out of order, after the fragment before it: the rule reads %s (issue #348)",
					claudeMD, lastRow, fragment, strings.Join(countingRule, " -> "))
			}
			t.Fatalf("%s:%d: the counting rule of the review limit is missing %q, which the paragraph right below the %q table must state (issue #348)",
				claudeMD, lastRow, fragment, heading)
		}
		at += i + len(fragment)
	}
}
