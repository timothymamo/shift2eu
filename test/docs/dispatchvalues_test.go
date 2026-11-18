package docs_test

import (
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
)

// The two files whose tables must name the same dispatcher values (issue #269).
const (
	dispatchSkill = ".claude/skills/dispatch/SKILL.md"
	claudeMD      = "CLAUDE.md"
)

// valueRow is one row of a Markdown table: its trimmed cells and the line it
// sits on.
type valueRow struct {
	line  int
	cells []string
}

// valueTable is a Markdown table's values: the line of its header row and its
// rows with the header and the delimiter row dropped.
type valueTable struct {
	line int
	rows []valueRow
}

// valueTables returns the Markdown tables of data, in the order they appear,
// skipping fenced code blocks, whose lines are commands or prose and not
// tables.
func valueTables(data string) []valueTable {
	var (
		tables  []valueTable
		cur     *valueTable
		inFence bool
		marker  byte
		width   int
	)
	flush := func() {
		if cur != nil {
			tables = append(tables, *cur)
			cur = nil
		}
	}
	for i, line := range strings.Split(data, "\n") {
		n := i + 1
		if inFence {
			if closesFence(line, marker, width) {
				inFence = false
			}
			continue
		}
		if m, w, _, ok := openingFence(line); ok {
			flush()
			inFence, marker, width = true, m, w
			continue
		}
		cells, ok := tableCells(line)
		if !ok {
			flush()
			continue
		}
		if cur == nil {
			cur = &valueTable{line: n}
			continue // the header row
		}
		if len(cur.rows) == 0 && delimiterCells(cells) {
			continue
		}
		cur.rows = append(cur.rows, valueRow{line: n, cells: cells})
	}
	flush()
	return tables
}

// tableCells returns the trimmed cells of a Markdown table row, and false for
// a line that is not one.
func tableCells(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return nil, false
	}
	parts := strings.Split(strings.Trim(trimmed, "|"), "|")
	cells := make([]string, len(parts))
	for i, part := range parts {
		cells[i] = strings.TrimSpace(part)
	}
	return cells, true
}

// delimiterCells reports whether cells are a table's delimiter row: dashes and
// colons only, at least one of them a dash.
func delimiterCells(cells []string) bool {
	dashes := false
	for _, c := range cells {
		if c == "" || strings.Trim(c, "-:") != "" {
			return false
		}
		if strings.Contains(c, "-") {
			dashes = true
		}
	}
	return dashes
}

// nameWords returns the words of a name a table lists, lowercased: `<unit
// limit>` is "unit" and "limit", and "Maintainer notes directory" is
// "maintainer", "notes" and "directory".
func nameWords(name string) []string {
	name = strings.Trim(strings.TrimSpace(name), "`")
	name = strings.TrimSuffix(strings.TrimPrefix(name, "<"), ">")
	words := strings.Fields(name)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return words
}

// namesValue reports whether words name the value whose words are value, by
// the rule TestDispatchSkillNamesTheCLAUDEValues documents: every word is a
// prefix of one of the value's words.
func namesValue(words, value []string) bool {
	for _, w := range words {
		matched := false
		for _, v := range value {
			if strings.HasPrefix(v, w) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return len(words) > 0
}

// valueNames returns the first cell of every row of tb, the names it lists,
// with the backticks of inline code removed.
func valueNames(tb valueTable) []string {
	names := make([]string, len(tb.rows))
	for i, r := range tb.rows {
		names[i] = strings.Trim(r.cells[0], "`")
	}
	return names
}

// readDoc returns the contents of a file of the repository.
func readDoc(t *testing.T, fsys fs.FS, name string) string {
	t.Helper()

	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

// subsection returns the body of the `### <title>` subsection of data and the
// 0-based index of its heading line: the subsection runs from the heading to
// the next heading of the same or a higher level.
func subsection(data, title string) (body []string, start int, ok bool) {
	lines := strings.Split(data, "\n")
	for start, line := range lines {
		if strings.TrimSpace(line) != "### "+title {
			continue
		}
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
				end = i
				break
			}
		}
		return lines[start:end], start, true
	}
	return nil, 0, false
}

// claudeDispatcherValues returns the values of CLAUDE.md's `### Dispatcher`
// table: the first table of that subsection, whose line numbers are the file's.
func claudeDispatcherValues(t *testing.T, fsys fs.FS) valueTable {
	t.Helper()

	const heading = "### Dispatcher"
	body, start, ok := subsection(readDoc(t, fsys, claudeMD), "Dispatcher")
	if !ok {
		t.Fatalf("%s: no %q subsection", claudeMD, heading)
	}
	tables := valueTables(strings.Join(body, "\n"))
	if len(tables) == 0 {
		t.Fatalf("%s: the %q subsection has no table", claudeMD, heading)
	}
	table := tables[0]
	table.line += start
	for i := range table.rows {
		table.rows[i].line += start
	}
	return table
}

// withoutTables returns data with every line of every table in tables dropped,
// so a name that only appears in a table is not counted as prose.
func withoutTables(data string, tables []valueTable) string {
	drop := make(map[int]bool)
	for _, table := range tables {
		if len(table.rows) == 0 {
			continue
		}
		for n := table.line; n <= table.rows[len(table.rows)-1].line; n++ {
			drop[n] = true
		}
	}
	var kept []string
	for i, line := range strings.Split(data, "\n") {
		if !drop[i+1] {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// TestDispatchSkillNamesTheCLAUDEValues fails when the dispatch skill and
// CLAUDE.md's `### Dispatcher` table disagree about the values the skill takes
// from the repository (issue #269): the skill lists each of them by name in its
// first table, and CLAUDE.md gives each its value.
//
// The pairing rule: both tables list the same values in the same order, one
// row each, so the two have the same number of rows and row i of the skill's
// table names row i of CLAUDE.md's. A row "names" another when every word of
// its `<name>`, lowercased, is a prefix of one of the words of the CLAUDE.md
// name: `<unit limit>` names "Unit limit", `<checks>` names "Checks on a
// merged scratch tree" and `<notes dir>` names "Maintainer notes directory".
// No list of the pairs is kept by hand, so a value added on one side only, or
// renamed past recognition, fails.
func TestDispatchSkillNamesTheCLAUDEValues(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)
	skillDoc := readDoc(t, fsys, dispatchSkill)
	skillTables := valueTables(skillDoc)
	if len(skillTables) == 0 {
		t.Fatalf("%s: no values table: the names the skill takes from CLAUDE.md are gone", dispatchSkill)
	}
	names := skillTables[0]
	claude := claudeDispatcherValues(t, fsys)

	t.Run("both tables list the same values in the same order", func(t *testing.T) {
		t.Parallel()

		if len(names.rows) != len(claude.rows) {
			t.Errorf("%s:%d lists %d values, %s:%d lists %d: a value on one side needs its row on the other (issue #269)",
				dispatchSkill, names.line, len(names.rows), claudeMD, claude.line, len(claude.rows))
		}
		for i := 0; i < min(len(names.rows), len(claude.rows)); i++ {
			skillRow, claudeRow := names.rows[i], claude.rows[i]
			if !namesValue(nameWords(skillRow.cells[0]), nameWords(claudeRow.cells[0])) {
				t.Errorf("%s:%d names %s where %s:%d names %q: row %d of the skill's table names row %d of CLAUDE.md's (issue #269)",
					dispatchSkill, skillRow.line, skillRow.cells[0],
					claudeMD, claudeRow.line, claudeRow.cells[0], i+1, i+1)
			}
		}
	})

	t.Run("the review limit is a value that the skill names and uses", func(t *testing.T) {
		t.Parallel()

		const name = "<review limit>"
		words := nameWords(name)
		var (
			row   valueRow
			found bool
		)
		for _, r := range claude.rows {
			if namesValue(words, nameWords(r.cells[0])) {
				row, found = r, true
				break
			}
		}
		if !found {
			t.Fatalf("%s:%d has no row named \"Review limit\": the PRs waiting for the maintainer have a limit too (issue #269)",
				claudeMD, claude.line)
		}
		value := ""
		if len(row.cells) > 1 {
			value = row.cells[1]
		}
		if !strings.HasPrefix(value, "5") {
			t.Errorf("%s:%d: the review limit is %q, want a value that starts with 5 (issue #269)", claudeMD, row.line, value)
		}
		if !slices.Contains(valueNames(names), name) {
			t.Errorf("%s:%d does not name %s, though %s:%d defines it (issue #269)", dispatchSkill, names.line, name, claudeMD, row.line)
		}
		if !strings.Contains(withoutTables(skillDoc, skillTables), name) {
			t.Errorf("%s: %s appears nowhere but the values table: the process must say what the limit allows (issue #269)", dispatchSkill, name)
		}
	})
}
