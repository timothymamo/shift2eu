package docs_test

import (
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
//   - a whole command written as an inline code span, such as `<cli> close
//     <unit>` or `git -C <repository root> pull`: the span is not a
//     placeholder, but the arguments it takes are, so `<file>` in `--task-file
//     <file>` and `<message>` in `--text "<message>"` are checked like any
//     other token; the allow list of command metavariables, allowedMetavars,
//     excuses them;
//   - anything whose brackets hold more than lowercase words, such as a URL,
//     a path, or a shell redirection.
//
// A lone HTML tag such as `<br>` reads as a placeholder, but the skills write
// no HTML and a code span that is one is indistinguishable from a name, so the
// rule does not special-case it. A bare `<file>` written in a code span of its
// own counts too, and the allow list does not excuse it: a span that is nothing
// but a placeholder names a value, so it must be a name the skill's tables
// list, and a placeholder that names nothing is what this check exists to
// report.
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
// appear: the tokens of a name table's cell, of an inline code span, or of a
// command in a shell block.
func placeholderTokens(text string) []string {
	return placeholderToken.FindAllString(text, -1)
}

// codeSpan is one `inline code` span and the 1-based line it starts on.
type codeSpan struct {
	line int
	text string
}

// inlineCodeSpans returns the inline code spans of the Markdown in data, in
// document order. drop holds the lines of the document's tables, which are
// never prose.
//
// A span can run past the end of its line: in CommonMark a code span crosses a
// line break inside a paragraph, and the skill wraps its lines, so one span
// there opens near the end of a line and closes on the next (After "PR #N
// merged", step 5). An unclosed backtick therefore carries over to the next
// line of the same paragraph, and text keeps the newline, so a span that closes
// on one line and opens on another is read as one span and not as prose. An
// open span is dropped at a blank line, at a table row and at a fence: those
// end the paragraph, and a backtick left open there is not code.
//
// Every line a span touches contributes its own part, an empty one included: a
// span that opens on the last character of a line has nothing before the
// backtick, and the newline still belongs to the span, so a placeholder on the
// next line is reported there and not on the line the backtick opened.
func inlineCodeSpans(data string, drop map[int]bool) []codeSpan {
	var (
		spans   []codeSpan
		open    bool
		start   int
		text    strings.Builder
		last    int
		inFence bool
		marker  byte
		width   int
	)
	closeOpen := func() {
		open, last = false, 0
		text.Reset()
	}
	for i, line := range strings.Split(data, "\n") {
		n := i + 1
		if inFence {
			if closesFence(line, marker, width) {
				inFence = false
			}
			closeOpen()
			continue
		}
		if m, w, _, ok := openingFence(line); ok {
			inFence, marker, width = true, m, w
			closeOpen()
			continue
		}
		if drop[n] || strings.TrimSpace(line) == "" {
			closeOpen()
			continue
		}
		parts := strings.Split(line, "`")
		for j, part := range parts {
			if j > 0 { // a backtick: closes the open span, or opens one
				if open {
					spans = append(spans, codeSpan{line: start, text: text.String()})
					closeOpen()
				} else {
					open, start = true, n
					text.Reset()
					last = 0
				}
			}
			if !open {
				continue
			}
			if last != 0 && last != n {
				text.WriteByte('\n')
			}
			text.WriteString(part)
			last = n
		}
	}
	return spans
}

// placeholderUse is one placeholder of the skill, the line it sits on, and
// whether it is part of a command rather than a code span that names a value on
// its own: the arguments of a command span and the commands of a shell block
// are inCommand, and the allow list excuses only those.
type placeholderUse struct {
	line      int
	name      string
	inCommand bool
}

// allowedMetavars are the command metavariables the skill really uses, and the
// only placeholders outside its name tables that the check below accepts. Each
// is an argument a command documents for itself, not a value the skill takes
// from CLAUDE.md or from the work, so neither table can have a row for it. Keep
// the list minimal: add an entry only with the command in the skill that needs
// it, and remove it once nothing uses it. TestDispatchSkillNamesTheCLAUDEValues
// holds the list to that rule and fails on an entry no command of the skill
// takes.
var allowedMetavars = map[string]bool{
	"<file>":          true, // <cli> dispatch <name> --task-file <file>, <cli> inbox send --file <file>
	"<path>":          true, // git worktree remove <path>, git worktree unlock <path>
	"<message>":       true, // --text "<message>"
	"<branch>":        true, // git push origin --delete <branch>
	"<issue>":         true, // Closes #<issue>
	"<absolute path>": true, // xargs git -C <repository root> branch -D < <absolute path>
}

// skillPlaceholders returns every placeholder the skill uses outside its name
// tables, in document order: the placeholder tokens of every inline code span,
// commands and the arguments they take included, and every placeholder of a
// command inside a fenced shell block. A span that is nothing but a placeholder
// is a name of the skill's; one that holds more is a command, and its tokens are
// inCommand.
func skillPlaceholders(data string, tables []valueTable) []placeholderUse {
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
					uses = append(uses, placeholderUse{line: n, name: name, inCommand: true})
				}
			}
			continue
		}
		if m, w, info, ok := openingFence(line); ok {
			inFence, marker, width, shell = true, m, w, shellFence(info)
		}
	}
	for _, span := range inlineCodeSpans(data, tableLineNumbers(tables)) {
		if name, ok := placeholderSpan(span.text); ok {
			uses = append(uses, placeholderUse{line: span.line, name: name})
			continue
		}
		for j, part := range strings.Split(span.text, "\n") {
			for _, name := range placeholderTokens(part) {
				uses = append(uses, placeholderUse{line: span.line + j, name: name, inCommand: true})
			}
		}
	}
	slices.SortStableFunc(uses, func(a, b placeholderUse) int { return a.line - b.line })
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

// accepts reports whether the placeholder check accepts use: a name one of the
// skill's name tables lists, or a command metavariable in allowedMetavars that
// the skill uses inside a command. A span that is nothing but a placeholder
// names a value, so the allow list excuses a token only as part of a command:
// a lone `<file>` names nothing the skill lists and is reported.
func accepts(use placeholderUse, listed map[string]bool) bool {
	if listed[use.name] {
		return true
	}
	return use.inCommand && allowedMetavars[use.name]
}

// staleMetavars returns, in name order, the entries of allowed that no use in
// uses takes as a command argument: a metavariable the skill no longer uses,
// which the allow list must not keep, since a stale entry would excuse a
// placeholder no command takes.
func staleMetavars(uses []placeholderUse, allowed map[string]bool) []string {
	used := make(map[string]bool)
	for _, use := range uses {
		if use.inCommand {
			used[use.name] = true
		}
	}
	stale := make([]string, 0, len(allowed))
	for name := range allowed {
		if !used[name] {
			stale = append(stale, name)
		}
	}
	slices.Sort(stale)
	return stale
}

// TestPlaceholderDecision pins the accept/reject decision the skill-placeholder
// check makes for one placeholder use (issue #269): a name one of the skill's
// tables lists is accepted wherever it appears, and a command metavariable only
// inside a command, so a misspelling of a command's argument and a lone `<file>`
// are both rejected.
func TestPlaceholderDecision(t *testing.T) {
	t.Parallel()

	listed := map[string]bool{"<unit limit>": true, "<orchestration>": true}
	for _, tc := range []struct {
		name string
		use  placeholderUse
		want bool
	}{
		{name: "a listed name on its own", use: placeholderUse{name: "<unit limit>"}, want: true},
		{name: "a listed name in a command", use: placeholderUse{name: "<unit limit>", inCommand: true}, want: true},
		{name: "a metavariable in a command", use: placeholderUse{name: "<file>", inCommand: true}, want: true},
		{name: "a lone metavariable", use: placeholderUse{name: "<file>"}, want: false},
		{name: "a misspelling in a command", use: placeholderUse{name: "<orchestraton>", inCommand: true}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := accepts(tc.use, listed); got != tc.want {
				t.Errorf("accepts(%+v) = %t, want %t", tc.use, got, tc.want)
			}
		})
	}
}

// TestStaleMetavars pins what keeps allowedMetavars minimal (issue #269): an
// entry no command of the skill takes is reported, including one the skill uses
// only outside a command, where the allow list does not excuse it.
func TestStaleMetavars(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		uses    []placeholderUse
		allowed map[string]bool
		want    []string
	}{
		{name: "no entries", want: []string{}},
		{
			name:    "an entry the skill takes in a command",
			uses:    []placeholderUse{{name: "<file>", inCommand: true}},
			allowed: map[string]bool{"<file>": true},
			want:    []string{},
		},
		{
			name:    "an entry nothing takes",
			uses:    []placeholderUse{{name: "<file>", inCommand: true}},
			allowed: map[string]bool{"<file>": true, "<path>": true, "<branch>": true},
			want:    []string{"<branch>", "<path>"},
		},
		{
			name:    "an entry the skill uses only outside a command",
			uses:    []placeholderUse{{name: "<file>"}},
			allowed: map[string]bool{"<file>": true},
			want:    []string{"<file>"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, staleMetavars(tc.uses, tc.allowed)); diff != "" {
				t.Errorf("staleMetavars() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPlaceholderRule pins what placeholderSpan reads as a lone placeholder in
// the dispatch skill (issue #269): a name the skill defines, and not a command,
// a command's argument, or a single-letter metavariable. A command's arguments
// are placeholders too, which is why skillPlaceholders checks them against
// allowedMetavars; here only a code span that is nothing but the placeholder
// counts.
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
		{name: "a command", text: "<cli> close <unit>"},
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

// TestSkillPlaceholders pins what the skill-placeholder check sees (issue
// #269): the placeholder token of every inline code span, a span that runs past
// the end of its line and the arguments of a command included, and every
// placeholder of a command in a fenced shell block, but nothing of a table row,
// which is where the skill defines its names.
func TestSkillPlaceholders(t *testing.T) {
	t.Parallel()

	doc := strings.Join([]string{
		"# A synthetic skill", // 1
		"",                    // 2
		"Dispatch with `<cli> dispatch <name> --task-file <file>`.", // 3
		"",                      // 4
		"| Name | Meaning |",    // 5
		"|---|---|",             // 6
		"| `<unit>` | A unit |", // 7
		"",                      // 8
		"A span runs past its line: `git -C <repository root> worktree", // 9
		"remove <path>` (after `git worktree unlock <path>`).",          // 10
		"",        // 11
		"```bash", // 12
		"git -C <repository root> pull --ff-only",                    // 13
		"xargs git -C <repository root> branch -D < <absolute path>", // 14
		"```",                                    // 15
		"",                                       // 16
		"A lone span names a value: `<checks>`.", // 17
		"",                                       // 18
		"A wrapped span opens at the end of a line: `", // 19
		"and closes on the next with <checks>` here.",  // 20
	}, "\n")

	tables := valueTables(doc)
	if len(tables) != 1 {
		t.Fatalf("valueTables() found %d tables in the fixture, want 1: the fixture's table", len(tables))
	}

	// use is a placeholderUse the test can compare: placeholderUse's fields are
	// unexported, so cmp cannot reach them from this package, and its own are
	// exported for cmp.
	type use struct {
		Line      int
		Name      string
		InCommand bool
	}
	want := []use{
		{Line: 3, Name: "<cli>", InCommand: true},
		{Line: 3, Name: "<name>", InCommand: true},
		{Line: 3, Name: "<file>", InCommand: true},
		{Line: 9, Name: "<repository root>", InCommand: true},
		{Line: 10, Name: "<path>", InCommand: true},
		{Line: 10, Name: "<path>", InCommand: true},
		{Line: 13, Name: "<repository root>", InCommand: true},
		{Line: 14, Name: "<repository root>", InCommand: true},
		{Line: 14, Name: "<absolute path>", InCommand: true},
		{Line: 17, Name: "<checks>"},
		{Line: 20, Name: "<checks>", InCommand: true},
	}
	var got []use
	for _, u := range skillPlaceholders(doc, tables) {
		got = append(got, use{Line: u.line, Name: u.name, InCommand: u.inCommand})
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("skillPlaceholders() mismatch (-want +got):\n%s", diff)
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
		uses := skillPlaceholders(skillDoc, skillTables)
		for _, use := range uses {
			if accepts(use, listed) {
				continue
			}
			t.Errorf("%s:%d uses %s, which neither name table lists and which is no command metavariable: a placeholder is a value of the first table, a name from the work in the second, or an entry of allowedMetavars, so using a new one needs its row (issue #269)",
				dispatchSkill, use.line, use.name)
		}
	})

	t.Run("the metavariable allow list has no entry the skill stopped using", func(t *testing.T) {
		t.Parallel()

		uses := skillPlaceholders(skillDoc, skillTables)
		for _, name := range staleMetavars(uses, allowedMetavars) {
			t.Errorf("%s: allowedMetavars holds %s, which no command of the skill takes: a metavariable stays only with the command that needs it, so remove the entry (issue #269)",
				dispatchSkill, name)
		}
	})
}
