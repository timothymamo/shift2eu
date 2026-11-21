package architecture_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// This file holds the check that every depguard message in .golangci.yml
// reaches golangci-lint whole (issue #382). A depguard desc: is the message the
// linter prints for the import it refuses, so the rule's reason lives in it. A
// desc: whose value is a plain, unquoted YAML scalar that holds " #" loses
// everything from there on: YAML reads the # and the rest of the line as a
// comment, and the message ends mid-sentence at "(PRD". The older rules of the
// file were written that way, so their reports are cut short; the fix is to
// quote the value, as cli-only-in-named-tests already does, not to drop the
// reference the message carries.
//
// TestDepguardDescsKeepTheirWholeMessage reads .golangci.yml through
// test/internal/yamlread, the reader the other checks of this package share, and
// fails on every such desc: with its line number and its text.
// TestDescProblems pins the helper on small documents, so a document that
// reads no desc: at all cannot make the check pass on nothing: a quoted value
// keeps its #, a # with no space before it is no comment, and a key other than
// desc: is no depguard message. The check reads the file line by line and never
// re-emits it, so it says what the tool reads, not what a scoped reader of
// .golangci.yml's depguard block sees.
//
// The check reads the desc: of a block mapping at the start of its line, after
// a list item's dash too. Every other form it refuses, and says so: a # can sit
// in a flow mapping, in a flow collection, or on a plain value's continuation
// line, where reading one line at a time cannot tell it from a comment. A form
// the check refused is one a later reader has to read by hand, not one it
// silently accepted.

// TestDepguardDescsKeepTheirWholeMessage fails on every desc: value of
// .golangci.yml that is an unquoted YAML scalar holding " #", whose message
// golangci-lint would therefore print only up to the space before the #.
func TestDepguardDescsKeepTheirWholeMessage(t *testing.T) {
	t.Parallel()

	lines, err := yamlread.SplitLines(readRepositoryFile(t, ".golangci.yml"))
	if err != nil {
		t.Fatalf(".golangci.yml: %v", err)
	}

	var descs int

	for _, line := range lines {
		if _, isDesc := descText(line.Text); isDesc {
			descs++
		}
	}

	if descs == 0 {
		t.Fatal(".golangci.yml holds no depguard desc:, so this check would pass on nothing")
	}

	problems := descProblemsOf(lines)

	for _, line := range problems.hash {
		t.Errorf(".golangci.yml:%d: the depguard desc: is a plain, unquoted YAML scalar holding \" #\": YAML reads the # as the start of a comment, "+
			"so the message golangci-lint prints ends before it. Quote the value, as the desc: of cli-only-in-named-tests does (issue #382): %q",
			line.Num, line.Text)
	}

	for _, line := range problems.unread {
		t.Errorf(".golangci.yml:%d: this check does not read the depguard desc: as the message golangci-lint prints whole: its value is a form it does not read one line at a time "+
			"(a flow mapping that holds the key, a value that is a flow collection, or a plain value continued on a more-indented line, where a # cuts the message). "+
			"Write desc: as the key of a block mapping, quoted, as the desc: of cli-only-in-named-tests does (issue #382): %q",
			line.Num, line.Text)
	}
}

// descProblems are the desc: lines of a document the check refuses, by reason.
type descProblems struct {
	// hash are the lines whose desc: value is a plain, unquoted YAML scalar
	// holding " #", which YAML reads as the start of a comment: the message
	// golangci-lint prints ends before it.
	hash []yamlread.Line
	// unread are the lines whose desc: the check does not read as a single-line
	// scalar: a key inside a flow mapping, a value that is a flow collection, or
	// a plain value continued on a more-indented line, where the # that cuts the
	// message can sit. The check reports the form, and how to write it, rather
	// than guess at where such a value ends.
	unread []yamlread.Line
}

// descProblemsOf returns the desc: lines of a document the check refuses, each
// in the order the document writes them: a quoted value keeps every # it holds,
// a # with no space before it, as in issue#382, is no comment, and a key other
// than desc: is no depguard message, so none of them is returned. One
// continuation line is out of its reach: yamlread.SplitLines drops a line that
// holds only a comment, a # first, with the indent that would have shown it to
// continue the key, so a desc: whose value a comment line cuts reads here as one
// that ends its own line.
func descProblemsOf(lines []yamlread.Line) descProblems {
	var problems descProblems

	for i, line := range lines {
		value, isDesc := descText(line.Text)

		if !isDesc {
			if descKeyInFlow(line.Text) {
				problems.unread = append(problems.unread, line)
			}

			continue
		}

		switch {
		case descIsFlow(value):
			problems.unread = append(problems.unread, line)
		case !descIsQuoted(value) && descContinued(lines, i):
			problems.unread = append(problems.unread, line)
		case !descIsQuoted(value) && strings.Contains(value, " #"):
			problems.hash = append(problems.hash, line)
		}
	}

	return problems
}

// descText returns the text of a line's desc: key as written after the colon,
// and whether the line holds a desc: key at all. A key written after a list
// item's dash, as - desc: ..., is read too. The text keeps the space between the
// key and the value, so a value that starts with # is read as the empty value
// the YAML reader sees rather than as a value that holds a #.
func descText(text string) (string, bool) {
	return strings.CutPrefix(strings.TrimPrefix(text, "- "), "desc:")
}

// descKeyInFlow reports whether a line holds a desc: key inside a flow
// collection, which this line-by-line check does not read:
// {pkg: example.com/internal/cli, desc: only the CLI (PRD #297)}. In a flow
// collection a key starts at a {, a [ or a comma, and a plain scalar there may
// not hold ": ", so a desc: after one is a key and no plain value of the line.
func descKeyInFlow(text string) bool {
	rest := text

	for {
		i := strings.Index(rest, "desc:")
		if i < 0 {
			return false
		}

		if before := strings.TrimRight(rest[:i], " "); before != "" {
			switch before[len(before)-1] {
			case '{', '[', ',':
				return true
			}
		}

		rest = rest[i+len("desc:"):]
	}
}

// descIsQuoted reports whether the text descText returns is a quoted YAML
// scalar, single or double quoted, whose # YAML keeps as part of the value. Any
// number of spaces may sit between the key and the value.
func descIsQuoted(value string) bool {
	value = strings.TrimLeft(value, " ")

	return strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'")
}

// descIsFlow reports whether the text descText returns is a flow collection, a
// mapping or a sequence, whose value the check does not read: YAML ends one at
// its closing bracket, which a line-by-line reader does not follow, and keeps a
// # inside it only where a quoted scalar holds it.
func descIsFlow(value string) bool {
	value = strings.TrimLeft(value, " ")

	return strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[")
}

// descContinued reports whether the line after lines[i] continues its desc:
// value, which YAML joins to it: a plain scalar's continuation is indented more
// than the key. A # on that line cuts the message too, where the check sees no #
// on the key's own line.
func descContinued(lines []yamlread.Line, i int) bool {
	return i+1 < len(lines) && lines[i+1].Indent > descKeyIndent(lines[i])
}

// descKeyIndent returns the column desc: starts in: the line's indent, and the
// two of a list item's dash when that comes first.
func descKeyIndent(line yamlread.Line) int {
	if strings.HasPrefix(line.Text, "- ") {
		return line.Indent + len("- ")
	}

	return line.Indent
}

// TestDescProblems pins the helper on small documents: the plain value with a #
// after a space is the one YAML cuts short, in a nested mapping and as a list
// item's first key too, while the quoted forms, the # with no space before it
// and the key other than desc: are read whole. The forms the check does not
// read one line at a time, a key in a flow mapping, a flow collection as the
// value and a plain value continued on an indented line, are reported as such,
// never silently accepted.
func TestDescProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		doc    string
		hash   []int
		unread []int
	}{
		{
			name: "plain value with a hash after a space",
			doc:  "desc: only the site's browser smoke test (PRD #297)\n",
			hash: []int{1},
		},
		{
			name: "double-quoted value",
			doc:  "desc: \"only the test files of internal/cli (issue #345)\"\n",
		},
		{
			name: "single-quoted value",
			doc:  "desc: 'only the test files of internal/cli (issue #345)'\n",
		},
		{
			name: "quoted value with two spaces after the key",
			doc:  "desc:  \"only the test files of internal/cli (issue #345)\"\n",
		},
		{
			name: "hash with no space before it",
			doc:  "desc: see issue#345 for the reason\n",
		},
		{
			name: "quoted value with a real trailing comment",
			doc:  "desc: \"only the test files of internal/cli (issue #345)\" # the rule's reason\n",
		},
		{
			name: "a key other than desc",
			doc:  "text: only the test files of internal/cli (issue #345)\n",
		},
		{
			name: "a plain value nested under its depguard entry",
			doc:  "deny:\n  - pkg: example.com/internal/cli\n    desc: only the CLI's own test files (PRD #297)\n",
			hash: []int{3},
		},
		{
			name: "a desc as the first key of a list item",
			doc:  "- desc: only the CLI's own test files (PRD #297)\n",
			hash: []int{1},
		},
		{
			name: "a quoted desc as the first key of a list item",
			doc:  "- desc: \"only the CLI's own test files (PRD #297)\"\n",
		},
		{
			name:   "a desc inside a flow mapping",
			doc:    "deny:\n  - {pkg: example.com/internal/cli, desc: only the CLI (PRD #297)}\n",
			unread: []int{2},
		},
		{
			name:   "a flow mapping as the desc value",
			doc:    "desc: {reason: only the CLI, ref: PRD #297}\n",
			unread: []int{1},
		},
		{
			name:   "a plain value continued on a more-indented line",
			doc:    "desc: only the test files of internal/cli\n  (issue #345)\n",
			unread: []int{1},
		},
		{
			name: "a quoted value continued on an indented line",
			doc:  "desc: \"only the test files of internal/cli\n  (issue #345)\"\n",
		},
		{
			name: "every cut value of a document, by line",
			doc: "deny:\n  - pkg: example.com/internal/web\n    desc: the driving adapters stay apart (PRD #297)\n" +
				"  - pkg: example.com/internal/site\n    desc: \"the site's glue is the only js caller (PRD #297)\"\n" +
				"  - pkg: example.com/internal/browser\n    desc: only the smoke test drives a browser (PRD #297)\n",
			hash: []int{3, 7},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lines, err := yamlread.SplitLines([]byte(tc.doc))
			if err != nil {
				t.Fatalf("SplitLines(%q): %v", tc.doc, err)
			}

			numbers := func(lines []yamlread.Line) []int {
				var nums []int

				for _, line := range lines {
					nums = append(nums, line.Num)
				}

				return nums
			}

			problems := descProblemsOf(lines)

			if diff := cmp.Diff(tc.hash, numbers(problems.hash)); diff != "" {
				t.Errorf("descProblemsOf() hash lines mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.unread, numbers(problems.unread)); diff != "" {
				t.Errorf("descProblemsOf() unread lines mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
