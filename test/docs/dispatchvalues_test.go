package docs_test

import (
	"io/fs"
	"os"
	"regexp"
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

// tableLineNumbers returns the 1-based number of every line that belongs to
// one of tables, their header and delimiter rows included.
func tableLineNumbers(tables []valueTable) map[int]bool {
	drop := make(map[int]bool)
	for _, table := range tables {
		if len(table.rows) == 0 {
			continue
		}
		for n := table.line; n <= table.rows[len(table.rows)-1].line; n++ {
			drop[n] = true
		}
	}
	return drop
}

// withoutTables returns data with every line of every table in tables dropped,
// so a name that only appears in a table is not counted as prose.
func withoutTables(data string, tables []valueTable) string {
	drop := tableLineNumbers(tables)
	var kept []string
	for i, line := range strings.Split(data, "\n") {
		if !drop[i+1] {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// placeholderToken matches a `<name>` placeholder: one or more lowercase words
// of two letters or more, separated by single spaces, in angle brackets.
var placeholderToken = regexp.MustCompile(`<[a-z]{2,}(?: [a-z]{2,})*>`)

// The placeholder rule, shared by the name tables and the skill's prose: a
// placeholder names a value, and is `<` + one or more lowercase words + `>`,
// such as `<unit limit>` or `<unit dir>`. Three things are deliberately not
// placeholders:
//
//   - a command's own metavariable, a single letter such as `<n>` or `<a>`,
//     which no word of a name matches;
//   - a command written as an inline code span, such as `git -C <repository
//     root> pull`: the span is not a placeholder, and neither are its
//     arguments (`--task-file <file>`, `--text "<message>"`); only a span that
//     is nothing but a placeholder counts, which is how the skill names a value
//     in prose;
//   - anything whose brackets hold more than lowercase words, such as a URL,
//     a path, or a shell redirection.
//
// A lone HTML tag such as `<br>` reads as a placeholder, but the skills write
// no HTML and a code span that is one is indistinguishable from a name, so the
// rule does not special-case it. A bare `<file>` written in a code span of its
// own does count: the skill names its values that way, and a placeholder that
// names nothing is what this check exists to report.
//
// placeholderSpan returns the placeholder that span, the content of an inline
// code span, is, and false when span is more than that or not one.
func placeholderSpan(span string) (string, bool) {
	trimmed := strings.TrimSpace(span)
	match := placeholderToken.FindString(trimmed)
	if match == "" || match != trimmed {
		return "", false
	}
	return match, true
}

// placeholderTokens returns the placeholders in text, in the order they
// appear: the tokens of a name table's cell, or of a command in a shell block.
func placeholderTokens(text string) []string {
	return placeholderToken.FindAllString(text, -1)
}

// inlineCodeSpans returns the contents of the `inline code` spans of line. An
// inline code span never runs past the end of its line and the skills wrap
// their lines, so an odd backtick is left open and its text is not code.
func inlineCodeSpans(line string) []string {
	parts := strings.Split(line, "`")
	spans := make([]string, 0, len(parts)/2)
	for i := 1; i+1 < len(parts); i += 2 {
		spans = append(spans, parts[i])
	}
	return spans
}

// placeholderUse is one placeholder of the skill, and the line it sits on.
type placeholderUse struct {
	line int
	name string
}

// skillPlaceholders returns every placeholder the skill uses outside its name
// tables, in document order: the inline code spans of its prose that are
// nothing but a placeholder, and every placeholder of a command inside a
// fenced shell block.
func skillPlaceholders(data string, tables []valueTable) []placeholderUse {
	drop := tableLineNumbers(tables)
	var (
		uses    []placeholderUse
		inFence bool
		shell   bool
		marker  byte
		width   int
	)
	for i, line := range strings.Split(data, "\n") {
		n := i + 1
		if inFence {
			if closesFence(line, marker, width) {
				inFence = false
				continue
			}
			if shell {
				for _, name := range placeholderTokens(line) {
					uses = append(uses, placeholderUse{line: n, name: name})
				}
			}
			continue
		}
		if m, w, info, ok := openingFence(line); ok {
			inFence, marker, width, shell = true, m, w, shellFence(info)
			continue
		}
		if drop[n] {
			continue
		}
		for _, span := range inlineCodeSpans(line) {
			if name, ok := placeholderSpan(span); ok {
				uses = append(uses, placeholderUse{line: n, name: name})
			}
		}
	}
	return uses
}

// listedNames returns the placeholders the name tables of a skill list in
// their first cells: the values it takes from CLAUDE.md and the names it takes
// from the work.
func listedNames(tables []valueTable) map[string]bool {
	listed := make(map[string]bool)
	for _, table := range tables {
		for _, row := range table.rows {
			if len(row.cells) == 0 {
				continue
			}
			for _, name := range placeholderTokens(row.cells[0]) {
				listed[name] = true
			}
		}
	}
	return listed
}

// TestPlaceholderRule pins what counts as a placeholder in the dispatch skill
// (issue #269): a name the skill defines, and not a command, its arguments, or
// a single-letter metavariable.
func TestPlaceholderRule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{name: "a one-word name", text: "<checks>", want: "<checks>"},
		{name: "a two-word name", text: "<unit limit>", want: "<unit limit>"},
		{name: "a three-word name", text: "<absolute path>", want: "<absolute path>"},
		{name: "a name with padding", text: "  <unit dir> ", want: "<unit dir>"},
		{name: "a command", text: "`<cli> close <unit>`"},
		{name: "a command argument", text: "--task-file <file>"},
		{name: "a name with a path", text: "<unit dir>/.worktrees/"},
		{name: "a single-letter metavariable", text: "<n>"},
		{name: "a URL", text: "<https://example.com>"},
		{name: "a redirection", text: "< <absolute path>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, _ := placeholderSpan(tc.text)
			if got != tc.want {
				t.Errorf("placeholderSpan(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
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

	t.Run("every placeholder the skill uses is a name one of its tables lists", func(t *testing.T) {
		t.Parallel()

		if len(skillTables) < 2 {
			t.Fatalf("%s: %d name tables, want the values table and the table of names from the work (issue #269)", dispatchSkill, len(skillTables))
		}
		listed := listedNames(skillTables)
		for _, use := range skillPlaceholders(skillDoc, skillTables) {
			if !listed[use.name] {
				t.Errorf("%s:%d uses %s, which neither name table lists: a placeholder is a value of the first table or a name from the work in the second, so using a new one needs its row (issue #269)",
					dispatchSkill, use.line, use.name)
			}
		}
	})
}
