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
// TestUnquotedDescHashes pins the helper on small documents, so a document that
// reads no desc: at all cannot make the check pass on nothing: a quoted value
// keeps its #, a # with no space before it is no comment, and a key other than
// desc: is no depguard message. The check reads the file line by line and never
// re-emits it, so it says what the tool reads, not what a scoped reader of
// .golangci.yml's depguard block sees.

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

	for _, line := range unquotedDescHashes(lines) {
		t.Errorf(".golangci.yml:%d: the depguard desc: is a plain, unquoted YAML scalar holding \" #\": YAML reads the # as the start of a comment, "+
			"so the message golangci-lint prints ends before it. Quote the value, as the desc: of cli-only-in-named-tests does (issue #382): %q",
			line.Num, line.Text)
	}
}

// unquotedDescHashes returns the lines of a document whose desc: value is a
// plain, unquoted YAML scalar holding " #", which YAML reads as the start of a
// comment. A quoted value keeps every # it holds, a # with no space before it,
// as in issue#382, is no comment, and a key other than desc: is no depguard
// message, so none of them is returned.
func unquotedDescHashes(lines []yamlread.Line) []yamlread.Line {
	var cut []yamlread.Line

	for _, line := range lines {
		value, isDesc := descText(line.Text)

		if isDesc && !descIsQuoted(value) && strings.Contains(value, " #") {
			cut = append(cut, line)
		}
	}

	return cut
}

// descText returns the text of a line's desc: key as written after the colon,
// and whether the line holds a desc: key at all. The text keeps the space
// between the key and the value, so a value that starts with # is read as the
// empty value the YAML reader sees rather than as a value that holds a #.
func descText(text string) (string, bool) {
	value, isDesc := strings.CutPrefix(text, "desc:")

	return value, isDesc
}

// descIsQuoted reports whether the text descText returns is a quoted YAML
// scalar, single or double quoted, whose # YAML keeps as part of the value.
func descIsQuoted(value string) bool {
	value = strings.TrimPrefix(value, " ")

	return strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'")
}

// TestUnquotedDescHashes pins the helper on small documents: the plain value
// with a # after a space is the one YAML cuts short, in a nested mapping too,
// while the quoted forms, the # with no space before it and the key other than
// desc: are read whole.
func TestUnquotedDescHashes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		want []int
	}{
		{
			name: "plain value with a hash after a space",
			doc:  "desc: only the site's browser smoke test (PRD #297)\n",
			want: []int{1},
		},
		{
			name: "double-quoted value",
			doc:  "desc: \"only the test files of internal/cli (issue #345)\"\n",
			want: nil,
		},
		{
			name: "single-quoted value",
			doc:  "desc: 'only the test files of internal/cli (issue #345)'\n",
			want: nil,
		},
		{
			name: "hash with no space before it",
			doc:  "desc: see issue#345 for the reason\n",
			want: nil,
		},
		{
			name: "quoted value with a real trailing comment",
			doc:  "desc: \"only the test files of internal/cli (issue #345)\" # the rule's reason\n",
			want: nil,
		},
		{
			name: "a key other than desc",
			doc:  "text: only the test files of internal/cli (issue #345)\n",
			want: nil,
		},
		{
			name: "a plain value nested under its depguard entry",
			doc:  "deny:\n  - pkg: example.com/internal/cli\n    desc: only the CLI's own test files (PRD #297)\n",
			want: []int{3},
		},
		{
			name: "every cut value of a document, by line",
			doc: "deny:\n  - pkg: example.com/internal/web\n    desc: the driving adapters stay apart (PRD #297)\n" +
				"  - pkg: example.com/internal/site\n    desc: \"the site's glue is the only js caller (PRD #297)\"\n" +
				"  - pkg: example.com/internal/browser\n    desc: only the smoke test drives a browser (PRD #297)\n",
			want: []int{3, 7},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lines, err := yamlread.SplitLines([]byte(tc.doc))
			if err != nil {
				t.Fatalf("SplitLines(%q): %v", tc.doc, err)
			}

			var got []int

			for _, line := range unquotedDescHashes(lines) {
				got = append(got, line.Num)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("unquotedDescHashes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
