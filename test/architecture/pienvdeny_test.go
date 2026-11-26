// Pins pi's refusal of the commands that print the environment (CLAUDE.md
// rule 3, issue #540).
//
// The tester runs pi with --approve, so nothing asks it for permission: the
// refusal pinned here is the second layer, after the words of CLAUDE.md rule 3,
// between it and an API key in its pane, not a boundary (ADR 0044).
// On 2026-11-24 a tester ran `env | grep` while looking for its scratchpad
// directory and printed DEEPSEEK_API_KEY, which then had to be rotated.
//
// The extension .pi/extensions/deny-env-printing.ts reads its patterns from
// .pi/deny-env-printing.json and refuses a bash tool call whose command matches
// any of them. The extension cannot run here, so the list is pinned as data and
// held to the two tables below: every command that prints the environment is
// refused, and no command the agents run routinely is.
//
// One property is out of reach of this test: pi matches the patterns with
// JavaScript's regular expressions, and a pattern Go accepts but JavaScript
// does not (a lookahead, a lookbehind, a backreference) would refuse nothing in
// pi and pass every test here. The patterns are therefore written to the
// narrowest common syntax (no lookaround, no backreference), which a reviewer
// has to hold; nothing here can.
package architecture_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
)

// The two files that make pi refuse a command: the pattern list the extension
// reads, and the extension itself.
const (
	piEnvDenyListPath  = ".pi/deny-env-printing.json"
	piEnvDenyExtension = ".pi/extensions/deny-env-printing.ts"
)

// piEnvDenyList is the document .pi/deny-env-printing.json holds: the reason a
// refusal gives the agent, so it can stop instead of looking for another way to
// read the environment, and the regular expressions that make one.
type piEnvDenyList struct {
	Reason   string   `json:"reason"`
	Patterns []string `json:"patterns"`
}

// TestPiEnvDenyListIsARefusalDocument pins the pattern list as a document: it
// decodes, it holds no key the extension does not read (a misspelt `pattern`
// would leave the extension with no pattern at all), it gives a reason and at
// least one pattern, and every pattern is a regular expression Go compiles.
func TestPiEnvDenyListIsARefusalDocument(t *testing.T) {
	t.Parallel()

	data := readRepositoryFile(t, piEnvDenyListPath)

	var list piEnvDenyList

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&list); err != nil {
		t.Fatalf("%s: %v", piEnvDenyListPath, err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		t.Fatalf("%s: the file holds more than one JSON value", piEnvDenyListPath)
	}

	if strings.TrimSpace(list.Reason) == "" {
		t.Errorf("%s: reason is empty, so a refused agent is told nothing about why (issue #540)", piEnvDenyListPath)
	}

	if len(list.Patterns) == 0 {
		t.Fatalf("%s: patterns is empty, so the extension refuses no command at all (issue #540)", piEnvDenyListPath)
	}

	for i, pattern := range list.Patterns {
		if _, err := regexp.Compile(pattern); err != nil {
			t.Errorf("%s: patterns[%d] = %q is no regular expression: %v", piEnvDenyListPath, i, pattern, err)
		}
	}
}

// TestPiEnvDenyListPatternsUseTheNarrowestSyntax keeps every pattern to the
// syntax Go's RE2 and JavaScript read the same way. The extension compiles the
// patterns with JavaScript's `new RegExp`, and a construct the two engines read
// differently (a group extension, a Unicode property, a POSIX character class)
// refuses nothing in pi while the tables below still pass: the tables see what
// Go reads, not what pi does. A pattern a later change writes for Go alone is a
// hole no other test here can see.
func TestPiEnvDenyListPatternsUseTheNarrowestSyntax(t *testing.T) {
	t.Parallel()

	data := readRepositoryFile(t, piEnvDenyListPath)

	var list piEnvDenyList
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("%s: %v", piEnvDenyListPath, err)
	}

	forbidden := []struct {
		name      string
		construct string
	}{
		{name: "a group extension, an inline flag or a named group, which RE2's own syntax does not have", construct: "(?"},
		{name: "a Unicode property escape, which RE2 and JavaScript do not spell the same way", construct: `\p`},
		{name: "a negated Unicode property escape, which RE2 and JavaScript do not spell the same way", construct: `\P`},
		{name: "a literal-quote escape, which RE2 does not have", construct: `\Q`},
		{name: "a start-or-end anchor, which RE2 and JavaScript do not spell the same way", construct: `\z`},
		{name: "a start-of-text anchor, which RE2 and JavaScript do not spell the same way", construct: `\A`},
		{name: "a POSIX character class, which JavaScript does not have", construct: "[[:"},
	}

	for i, pattern := range list.Patterns {
		for _, bad := range forbidden {
			if strings.Contains(pattern, bad.construct) {
				t.Errorf("%s: patterns[%d] = %q holds %s (%q): pi matches with JavaScript's regular expressions, which read that construct differently from Go's RE2 or not at all, so the pattern refuses nothing while the tables below still pass (issue #540)", piEnvDenyListPath, i, pattern, bad.name, bad.construct)
			}
		}
	}
}

// TestPiEnvDenyListPatternsCannotBacktrackWithoutBound keeps every pattern to a
// regular expression pi can evaluate in linear time. pi matches with
// JavaScript's backtracking engine, where an unbounded repeat around a group
// that itself holds an optional part costs exponentially many paths on a
// command that almost matches: the shared command prefix of the first patterns
// nests an optional option value (`-\S*( \S*)?`) inside a repeat over the
// wrappers, which is linear in Go's RE2 and exponential in Node. A pattern that
// is slow rather than wrong still refuses nothing useful: pi stalls on the
// agent's own command, and a refused-but-hung tool call is a refusal the agent
// cannot tell from a broken tool. Patterns are therefore pinned by structure, a
// group repeat having to be bounded — `){0,8}` — because Go's RE2 cannot show
// the cost the way Node does.
func TestPiEnvDenyListPatternsCannotBacktrackWithoutBound(t *testing.T) {
	t.Parallel()

	data := readRepositoryFile(t, piEnvDenyListPath)

	var list piEnvDenyList
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("%s: %v", piEnvDenyListPath, err)
	}

	// The largest bound a group repeat may take: the work then grows with the
	// command's length times the bound, which a long but legal command survives.
	const maxGroupRepeat = 8

	for i, pattern := range list.Patterns {
		if bad := piUnboundedGroupRepeats(pattern, maxGroupRepeat); len(bad) > 0 {
			t.Errorf("%s: patterns[%d] repeats a group without a bound (%s): pi matches with JavaScript's backtracking engine, where an unbounded repeat around a group with an optional part (the shared command prefix's `-\\S*( \\S*)?`) costs exponential time on a command that almost matches, so a long legal command stalls pi instead of running; write each group repeat with a bound of at most %d, `){0,%d}` (issue #540)", piEnvDenyListPath, i, piQuotedFragments(bad), maxGroupRepeat, maxGroupRepeat)
		}
	}
}

// TestPiUnboundedGroupRepeats pins the reader the test above decides with: a
// `)` inside a character class or escaped is no group end, a bounded group
// repeat is no finding, and every unbounded form is one.
func TestPiUnboundedGroupRepeats(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		pattern string
		want    []string
	}{
		{name: "a star on a group", pattern: `(a|b)*`, want: []string{")*"}},
		{name: "a plus on a group", pattern: `(a|b)+`, want: []string{")+"}},
		{name: "a lazy star on a group", pattern: `(a|b)*?`, want: []string{")*?"}},
		{name: "a lazy plus on a group", pattern: `(a|b)+?`, want: []string{")+?"}},
		{name: "a group repeat with no upper bound", pattern: `(a|b){2,}`, want: []string{"){2,}"}},
		{name: "a group repeat above the bound", pattern: `(a|b){0,9}`, want: []string{"){0,9}"}},
		{name: "a group repeat at the bound", pattern: `(a|b){0,8}`},
		{name: "a group repeat of one count", pattern: `(a|b){3}`},
		{name: "a repeat of a class, not a group", pattern: `[ \t]*`},
		{name: "a class repeat after a class that ends with a paren", pattern: `[()]*`},
		{name: "an escaped paren", pattern: `\)*`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := piUnboundedGroupRepeats(tc.pattern, 8)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("piUnboundedGroupRepeats(%q) (-want +got):\n%s", tc.pattern, diff)
			}
		})
	}
}

// piUnboundedGroupRepeats returns the group repeats of pattern that are
// unbounded, each once: a `)` followed by `*` or `+` (lazy or not), and a
// `){n,}` or a `){n,m}` whose m is above max. A `)` inside a character class or
// escaped as a literal is no group end and is skipped.
func piUnboundedGroupRepeats(pattern string, max int) []string {
	var found []string

	seen := map[string]struct{}{}

	add := func(fragment string) {
		if _, ok := seen[fragment]; ok {
			return
		}

		seen[fragment] = struct{}{}
		found = append(found, fragment)
	}

	for i := 0; i < len(pattern); i++ {
		c := pattern[i]

		if c == '\\' {
			// An escaped character, a literal `)` among them: skip it whole.
			i++

			continue
		}

		if c == '[' {
			// A character class runs to its unescaped `]`, and a `)` inside it
			// ends no group.
			for i++; i < len(pattern) && pattern[i] != ']'; i++ {
				if pattern[i] == '\\' {
					i++
				}
			}

			continue
		}

		if c != ')' {
			continue
		}

		rest := pattern[i+1:]

		switch {
		case strings.HasPrefix(rest, "*?"), strings.HasPrefix(rest, "+?"):
			add(pattern[i : i+3])
		case strings.HasPrefix(rest, "*"), strings.HasPrefix(rest, "+"):
			add(pattern[i : i+2])
		case strings.HasPrefix(rest, "{"):
			end := strings.Index(rest, "}")
			if end < 0 {
				continue
			}

			quantifier := rest[:end+1]
			if !piGroupRepeatIsBounded(quantifier, max) {
				add(pattern[i : i+1+len(quantifier)])
			}
		}
	}

	return found
}

// piHoldsAny reports whether text holds any one of the alternatives, for a pin
// whose spelling the extension may choose: the length bound of 32 KB, which a
// writer may spell as a number, a product or a separator.
func piHoldsAny(text string, alternatives ...string) bool {
	for _, alternative := range alternatives {
		if strings.Contains(text, alternative) {
			return true
		}
	}

	return false
}

// piQuotedFragments joins fragments for a failure message, each in quotes so a
// fragment of `)*` is not read as the sentence's own brackets.
func piQuotedFragments(fragments []string) string {
	quoted := make([]string, 0, len(fragments))

	for _, fragment := range fragments {
		quoted = append(quoted, strconv.Quote(fragment))
	}

	return strings.Join(quoted, ", ")
}

// piGroupRepeatIsBounded tells whether the quantifier `{...}` that follows a
// group bounds it to at most max repetitions: `{n}`, `{n,m}`, `{n,m}?` and
// `{n}?` do, `{n,}` does not, and neither does an m above max.
func piGroupRepeatIsBounded(quantifier string, max int) bool {
	body := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(quantifier, "{"), "}"), "?")

	bounds := strings.Split(body, ",")
	if len(bounds) == 2 && strings.TrimSpace(bounds[1]) == "" {
		return false
	}

	upper := bounds[len(bounds)-1]

	n, err := strconv.Atoi(strings.TrimSpace(upper))
	if err != nil {
		// Anything that is no repetition count is no unbounded repeat this test
		// can name; the syntax test holds the patterns to what both engines read.
		return true
	}

	return n <= max
}

// TestPiEnvDenyExtensionHooksTheToolCall pins the extension's wiring: it reads
// the pattern list by name and it hooks pi's `tool_call` event, the one that
// sees a bash command before the command runs. An extension that hooks no event
// (or another one) is loaded and refuses nothing. It also pins what a refusal is
// made of (a block with a reason), the file tools it holds beside bash, the
// `/proc` and `daemon.env` paths a file tool must not read, and the reason the
// fail-closed branch gives; and it holds resolvePath to the path normalisation
// pi applies before a tool reads a file (the leading `@` it strips and the
// `file://` URL it turns into a path), because a guard that resolves the raw
// value sees `/proc/self/environ` where pi reads it. It also keeps the extension off pi's package at run
// time, which it must not import: it is loaded before the tool call it guards,
// so a value import would make the guard depend on the very module whose version
// is the reason it exists (`import type` is erased and names the package alone).
// A `console.` call is refused too: the command it would log is the command that
// holds the key.
//
// Two further pins sit here because the extension cannot run: the two file tools
// that read a whole directory (grep and find) are refused when the path they are
// given is an ancestor of the deck's key directory or of `/proc`, since pi runs
// grep as `rg --hidden` from that path and a grep rooted at `~` prints the deck's
// file of keys; and a bash command longer than 32 KB is refused unread, with a
// reason that points at the write tool, because the prefix patterns cost
// quadratic time on one very long line and pi would stall on the agent's own
// command instead of refusing it.
func TestPiEnvDenyExtensionHooksTheToolCall(t *testing.T) {
	t.Parallel()

	text := string(readRepositoryFile(t, piEnvDenyExtension))

	for _, want := range []struct {
		name string
		text string
	}{
		{name: "the pattern list", text: "deny-env-printing.json"},
		{name: "the tool_call event", text: "tool_call"},
		{name: "the block", text: "block: true"},
		{name: "the read tool", text: `"read"`},
		{name: "the grep tool", text: `"grep"`},
		{name: "the fail-closed reason", text: "could not be loaded"},
		{name: "the /proc path", text: "/proc"},
		{name: "the deck's environment file", text: "daemon.env"},
		{name: "the deck's key directory", text: ".config/dot-agent-deck"},
	} {
		if !strings.Contains(text, want.text) {
			t.Errorf("%s: the extension does not name %s (%q), so it refuses no command (issue #540)", piEnvDenyExtension, want.name, want.text)
		}
	}

	// grep and find read the contents of the directory they are pointed at, and
	// pi runs grep as `rg --hidden`: a grep whose path is `~` reads
	// ~/.config/dot-agent-deck/daemon.env and prints the keys, and one whose path
	// is `/` walks /proc. Neither path lies under the two refused directories, so
	// the checks above allow the call; the extension has to ask the reverse
	// question, whether a refused directory lies under the path it was given, and
	// it has to ask it of the two tools that read a directory's contents alone: a
	// read, an ls or an edit of `~` prints no key.
	for _, want := range []struct {
		name string
		text string
	}{
		{name: "the file tools that read a whole directory", text: "recursiveTools"},
		{name: "the recursive file tools' own check", text: "recursiveTools.has("},
		{name: "the check that the deck's key directory lies under the path", text: "under(keyDirectory, absolute)"},
		{name: "the check that /proc lies under the path", text: `under("/proc", absolute)`},
	} {
		if !strings.Contains(text, want.text) {
			t.Errorf("%s: the extension does not name %s (%q), so a grep or find rooted at an ancestor of a refused directory (path %q, path %q) reads it and prints the keys (issue #540)", piEnvDenyExtension, want.name, want.text, "~", "/")
		}
	}

	// A very long command is refused before it is matched: the prefix patterns
	// repeat with a bound, and every attempt still walks the command from its own
	// starting position, so the match costs time quadratic in the command's
	// length. 100 KB of minified JSON inside an `echo '...'` takes 35 s in Node,
	// which the agent waits out as a tool call that never returns; the extension
	// therefore refuses a bash command over 32 KB (32768 characters), says so in a
	// reason that points at the write tool, and asks before it reaches the
	// patterns, where the time goes.
	if !piHoldsAny(text, "32768", "32 * 1024", "32*1024", "32_768") {
		t.Errorf("%s: the extension names no length bound of 32 KB (`32768` or `32 * 1024`), so a very long bash command is matched against every pattern, which costs quadratic time: 100 KB of minified JSON takes 35 s in Node and the agent waits on a call that never returns (issue #540)", piEnvDenyExtension)
	}

	if !strings.Contains(text, "write tool") {
		t.Errorf("%s: no refusal for a long command points at the write tool (%q), so an agent whose command was refused for its length is told nothing about how to run it (issue #540)", piEnvDenyExtension, "write tool")
	}

	lengthAt := strings.Index(text, "command.length")
	if lengthAt < 0 {
		t.Errorf("%s: the extension checks no bash command's length (`command.length`), so a command over the 32 KB bound is matched against every pattern (issue #540)", piEnvDenyExtension)
	}

	matchAt := strings.Index(text, "patterns.some(")
	if matchAt < 0 {
		t.Errorf("%s: the extension no longer matches a bash command against the patterns with `patterns.some(`, so nothing here can tell whether the length check comes first (issue #540)", piEnvDenyExtension)
	} else if lengthAt >= 0 && lengthAt > matchAt {
		t.Errorf("%s: the extension tries the patterns before it checks the command's length, so a command over the 32 KB bound pays the quadratic match first, which is the 35 s the length check exists to avoid (issue #540)", piEnvDenyExtension)
	}

	// pi normalises a file tool's path before the tool reads it: it strips a
	// leading `@` (its own shorthand for a path), turns a `file://` URL into a
	// path, and maps the Unicode spaces to plain ones. A guard that resolves the
	// raw value sees `/proc/self/environ` where pi reads it, and allows the read.
	resolveBody := piFunctionBody(text, "function resolvePath(")
	if resolveBody == "" {
		t.Fatalf("%s: the extension has no function resolvePath, so no file tool's path is resolved before it is read (issue #540)", piEnvDenyExtension)
	}

	for _, want := range []struct {
		name string
		text string
	}{
		{name: "strip the leading @ that pi strips", text: `startsWith("@")`},
		{name: "turn a file:// URL into a path, as pi does", text: "fileURLToPath("},
	} {
		if !strings.Contains(resolveBody, want.text) {
			t.Errorf("%s: resolvePath does not %s (%q), so `@/proc/self/environ` and `file:///proc/self/environ` are read by pi and allowed here (issue #540)", piEnvDenyExtension, want.name, want.text)
		}
	}

	// A `catch` inside resolvePath has to rethrow: falling back to the raw value
	// would resolve a `file://` URL as a relative path and allow the read.
	if strings.Contains(resolveBody, "catch") && !strings.Contains(resolveBody, "throw") {
		t.Errorf("%s: resolvePath catches an error without rethrowing it, so a path pi cannot resolve is read as one it can and the call is allowed (issue #540); let the error reach the handler's refusal", piEnvDenyExtension)
	}

	for _, statement := range piImportStatements(text) {
		if !strings.Contains(statement, piPackage) {
			continue
		}

		if !strings.HasPrefix(statement, "import type") {
			t.Errorf("%s: the statement %q imports %s for a value at run time, so the guard depends on the package it guards (issue #540); write `import type` instead", piEnvDenyExtension, statement, piPackage)
		}
	}

	if strings.Contains(text, "console.") {
		t.Errorf("%s: the extension calls console., and a command it logs is a command that may hold the key (issue #540)", piEnvDenyExtension)
	}
}

// TestPiEnvDenyExtensionAncestorCheckHoldsAtTheRoot pins the two fail-opens the
// sixth review found in the ancestor check, both of which let the walk through.
//
// The first is `under`, which told whether a path is a directory or below it by
// asking whether the path starts with `${directory}/`: a refused directory of
// `/` then builds the prefix `//`, which no absolute path starts with, so
// `under("/proc", "/")` and `under(keyDirectory, "/")` are both false and a
// grep or find rooted at `/` is allowed. pi runs grep as `rg --hidden`, so that
// walk reads /proc/*/environ and ~/.config/dot-agent-deck/daemon.env and prints
// the keys. A directory that already ends in `/` must be handled as itself.
//
// The second is a grep or find with no `path` at all: pi defaults it to the
// working directory, so from a working directory of `~` the call reads the
// deck's key directory, while the extension skipped every call that named no
// path and refused nothing. The path therefore has to be resolved to `.` (the
// working directory) before the recursive check asks whether the refused
// directories lie under it.
func TestPiEnvDenyExtensionAncestorCheckHoldsAtTheRoot(t *testing.T) {
	t.Parallel()

	text := string(readRepositoryFile(t, piEnvDenyExtension))

	// The extension asks `under` in both directions, and a root-safe answer is
	// the one that treats a directory ending in `/` as that directory: the
	// extension may normalise the trailing slash or compare the root directory
	// itself, and either way the comparison stays a prefix test.
	underBody := piFunctionBody(text, "function under(")
	if underBody == "" {
		t.Fatalf("%s: the extension has no function under, so no file tool's path is compared with a refused directory (issue #540)", piEnvDenyExtension)
	}

	if !strings.Contains(underBody, "startsWith(") {
		t.Errorf("%s: under no longer tests whether the path is below the directory with `startsWith(`, so nothing here can tell whether the ancestor check still asks the question (issue #540)", piEnvDenyExtension)
	}

	if !piHoldsAny(underBody, `endsWith("/")`, `endsWith('/')`, `=== "/"`, `== "/"`) {
		t.Errorf("%s: under handles no directory that ends in `/` (it names none of %q, %q, %q, %q), so `under(\"/proc\", \"/\")` and `under(keyDirectory, \"/\")` ask whether the path starts with `//`, which no absolute path does: a grep or find rooted at `/`, `//`, `/.` or `/..` is allowed, and pi runs grep as `rg --hidden`, so the walk reads /proc/*/environ and ~/%s/%s and prints the keys (issue #540)", piEnvDenyExtension, `endsWith("/")`, `endsWith('/')`, `=== "/"`, `== "/"`, ".config/dot-agent-deck", "daemon.env")
	}

	// pi defaults a file tool's path to the working directory, which is where a
	// grep or find with no path walks: from a working directory of `~` that walk
	// reaches the deck's key directory, so the path has to be given that default
	// before the recursive check sees it.
	pathDefaultAt := -1

	for _, literal := range []string{`?? "."`, `?? '.'`, `|| "."`, `|| '.'`} {
		at := strings.Index(text, literal)
		if at >= 0 && (pathDefaultAt < 0 || at < pathDefaultAt) {
			pathDefaultAt = at
		}
	}

	if pathDefaultAt < 0 {
		t.Errorf("%s: the extension gives a file tool's absent path no default (it names none of %q, %q, %q, %q), so a grep or find with no `path` is skipped and refused nowhere, while pi runs it from the working directory: with a working directory of `~` that walk reads ~/%s/%s and prints the keys (issue #540)", piEnvDenyExtension, `?? "."`, `?? '.'`, `|| "."`, `|| '.'`, ".config/dot-agent-deck", "daemon.env")
	}

	recursiveAt := strings.Index(text, "recursiveTools.has(")

	switch {
	case recursiveAt < 0:
		t.Errorf("%s: the extension no longer names the recursive tools' check (`recursiveTools.has(`), so nothing here can tell whether an absent path is given its default before it (issue #540)", piEnvDenyExtension)
	case pathDefaultAt > recursiveAt:
		t.Errorf("%s: the extension gives a file tool's absent path its default after the recursive check reads the path, so the check sees no path and allows the call (issue #540)", piEnvDenyExtension)
	}
}

// TestPiEnvDenyExtensionHoldsOnlyASCII pins how the extension writes the
// Unicode spaces pi maps to a plain space before it reads a path: as code
// points it builds with `String.fromCodePoint`, never as the invisible
// characters themselves, so that a reader and a diff can see which code points
// the class matches.
//
// Two properties are pinned. The whole file holds no byte above U+007F: a single
// literal U+00A0 in the class, in a comment or anywhere else is invisible in a
// diff and cannot be told from a plain space, so every character above U+007F
// has to be built from its code point. And the declaration `const unicodeSpaces`
// is pinned whole, with its whitespace collapsed: the code points it builds, the
// range's `-` between them and the `g` flag.
//
// An exact pin is what the set needs, because every shorter or reordered class
// still compiles and still normalises most paths: dropping the `-` loses
// U+2001 to U+2009, swapping U+00A0 and U+2000 widens the range to
// U+00A0-U+200A, dropping the `g` replaces only the first space of a path, and a
// code point that survives only in a comment matches nothing. The set is pi's
// own UNICODE_SPACES (U+00A0, U+2000 to U+200A, U+202F, U+205F and U+3000) and
// changes only together with this pin.
//
// The test reads the source text only; it never runs the class (issue #546).
func TestPiEnvDenyExtensionHoldsOnlyASCII(t *testing.T) {
	t.Parallel()

	text := string(readRepositoryFile(t, piEnvDenyExtension))

	// Every character above U+007F, wherever it sits in the file, is reported
	// with its line and code point: a literal one is the character itself and
	// a diff shows no change when it comes back.
	for i, line := range strings.Split(text, "\n") {
		for at := 0; at < len(line); {
			r, size := utf8.DecodeRuneInString(line[at:])

			switch {
			case r == utf8.RuneError && size == 1:
				t.Errorf("%s:%d: the extension holds the byte 0x%02x at column %d, which is no valid UTF-8: this file holds only ASCII (issue #546)", piEnvDenyExtension, i+1, line[at], at+1)
			case r > unicode.MaxASCII:
				t.Errorf("%s:%d: the extension holds the non-ASCII code point U+%04X at column %d: a character above U+007F is invisible in a diff and cannot be told from the ASCII character it resembles, so the file holds only ASCII; spell it as U+%04X in a comment, or build it with `String.fromCodePoint(0x%04x)` in code (issue #546)", piEnvDenyExtension, i+1, r, at+1, r, r)
			}

			at += size
		}
	}

	// The declaration is pinned whole, its whitespace collapsed, so that the code
	// points, the `-` that makes U+2000 to U+200A a range and the `g` flag all
	// have to stay. A substring search would hold neither: it reads a dropped `-`,
	// two code points in the other order, a `g` turned into "" and a code point
	// left only in a comment as the set it pins, and `0x2000` is found inside
	// `0x20000`.
	const declaration = "const unicodeSpaces ="

	// The expected declaration is written as the extension writes it, over
	// several lines, and collapsed by the same rule as the file's text below, so
	// that this pin stays readable while it holds every token of the declaration.
	wantDeclared := strings.Join(strings.Fields(`
const unicodeSpaces = new RegExp(
	"[" +
		String.fromCodePoint(0x00a0, 0x2000) +
		"-" +
		String.fromCodePoint(0x200a, 0x202f, 0x205f, 0x3000) +
		"]",
	"g",
);`), " ")

	start := strings.Index(text, declaration)
	if start < 0 {
		t.Fatalf("%s: the extension no longer declares %q, so nothing here can tell which code points the Unicode space class holds (issue #546)", piEnvDenyExtension, declaration)
	}

	end := strings.IndexByte(text[start:], ';')
	if end < 0 {
		t.Fatalf("%s: the declaration %q has no closing `;`, so nothing here can read the code points the Unicode space class holds (issue #546)", piEnvDenyExtension, declaration)
	}

	declared := strings.Join(strings.Fields(text[start:start+end+1]), " ")

	if declared != wantDeclared {
		t.Errorf("%s: the declaration of the Unicode space class is not the one this pin holds: want %q, got %q. The class has to stay U+00A0, U+2000 to U+200A, U+202F, U+205F and U+3000 with the `g` flag (pi's own UNICODE_SPACES), built from their code points, and it changes only together with this pin (issue #546)", piEnvDenyExtension, wantDeclared, declared)
	}
}

// piPackage is the package a pi extension may name only in a type-only import.
const piPackage = "@earendil-works/pi-coding-agent"

// piImportStatements returns the extension's import and require statements, each
// joined onto one line, so that a statement written over several lines is read
// whole: the package a multi-line import names sits on its closing line, where a
// line-at-a-time reader would not see the `import type` that makes it safe.
func piImportStatements(text string) []string {
	var statements []string

	var current strings.Builder

	joining := false

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)

		if !joining {
			if !strings.HasPrefix(line, "import") && !strings.Contains(line, "require(") {
				continue
			}

			// A statement that names its module on this line is whole.
			if !strings.HasPrefix(line, "import") || strings.Contains(line, " from ") || strings.HasSuffix(line, ";") {
				statements = append(statements, line)

				continue
			}

			joining = true
			current.Reset()
			current.WriteString(line)

			continue
		}

		current.WriteString(" ")
		current.WriteString(line)

		if strings.Contains(line, " from ") || strings.HasSuffix(line, ";") {
			statements = append(statements, current.String())

			joining = false
		}
	}

	if joining {
		statements = append(statements, current.String())
	}

	return statements
}

// piFunctionBody returns the braced body of the first function whose opening
// line starts with signature, or the empty string when the extension holds no
// such function. The braces are counted as text: the extension's functions hold
// none but their own, and a template literal's `${` would be counted as an
// opening brace, so this reads a function's body, not a JavaScript file.
func piFunctionBody(text, signature string) string {
	start := strings.Index(text, signature)
	if start < 0 {
		return ""
	}

	open := strings.IndexByte(text[start:], '{')
	if open < 0 {
		return ""
	}

	open += start

	depth := 0

	for i := open; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--

			if depth == 0 {
				return text[open : i+1]
			}
		}
	}

	return ""
}

// TestPiEnvDenyListRefusesPrintingTheEnvironment pins what the patterns refuse:
// every form here prints, or reads as a file, values CLAUDE.md rule 3 keeps out
// of a pane, whatever the command does around it (a pipeline, a separator, a
// subshell, an absolute path to the binary).
func TestPiEnvDenyListRefusesPrintingTheEnvironment(t *testing.T) {
	t.Parallel()

	patterns := piEnvDenyPatterns(t)

	cases := []string{
		"env",
		"env | grep KEY",
		"env -0",
		"env | sort",
		"/usr/bin/env",
		"ls; env",
		"make check && env",
		"true || printenv",
		"(env)",
		"echo $(env)",
		"printenv",
		"printenv HOME",
		"printenv | grep -i key",
		"set",
		"set | grep KEY",
		"ls && set",
		"export",
		"export -p",
		"declare -p",
		"declare -x",
		"declare -px",
		"typeset -p",
		"compgen -v",
		"compgen -e",
		"cat /proc/self/environ",
		"tr '\\0' '\\n' < /proc/1/environ",
		"strings /proc/$$/environ",
		"xargs -0 < /proc/self/environ",
		// `env` with an assignment and no command prints instead of running a
		// command, and so does a leading assignment in front of a printer.
		"env FOO=bar",
		"env FOO=bar | grep KEY",
		"env A=1 printenv",
		"env X=1 env",
		"FOO=1 env",
		"FOO=1 printenv | grep KEY",
		"LC_ALL=C set",
		// A shell keyword around the printer: pi runs the command as it is given,
		// and bash runs the printer inside the keyword.
		"if true; then env; fi",
		"for i in 1; do printenv; done",
		"true && ! env",
		"{ env; }",
		// Wrappers that run another command as their argument or through exec.
		"timeout 5 env",
		"nice env",
		"stdbuf -oL env",
		"xargs -0 env",
		"sudo -E env",
		"command -p env",
		"time env",
		// Quoting and nesting: the printer is still the command bash runs.
		"bash -c env",
		"bash -c 'env'",
		`sh -c "printenv"`,
		"eval env",
		"echo `env`",
		// A newline separates commands as surely as `;` does.
		"make check\nenv",
		// The shell's other printers: the variables and exports by name, and
		// `export` itself, whose pipeline form prints them too.
		"compgen -A variable",
		"compgen -A export",
		"declare",
		"typeset",
		"export | grep KEY",
		"/bin/printenv",
		// ps prints a process's environment, and make prints the environment of
		// its targets.
		"ps eww $$",
		"ps auxeww",
		"ps e",
		"make -p",
		"make -pn",
		"make --print-data-base",
		// /proc holds the environment of every process, listed or read.
		"cd /proc/self && cat environ",
		"cat /proc/self/env*",
		"ls /proc/1",
		// The file the deck keeps the agents' keys in.
		"cat ~/.config/dot-agent-deck/daemon.env",
		"grep KEY /home/u/.config/dot-agent-deck/daemon.env",
		// The agents App's token file, which `make agents-token` writes under the VM user's home and the gh
		// wrapper reads (ADR 0031, docs/dev-environment.md): printing it hands over the one GitHub credential
		// every agent pushes with, so every spelling of its path is refused, as the deck's keys file is.
		"cat ~/.config/shift2eu/agents-app-token",
		"cat /home/x/.config/shift2eu/agents-app-token",
		"head -c 32 ~/.config/shift2eu/agents-app-token",
		`printf '%s' "$(cat ~/.config/shift2eu/agents-app-token)"`,
		// The file named without its folder, which is how a search or a quoted path reaches it: a walk of the
		// home directory that prints what its name matches reads the token, and so does a spelling of the folder
		// a glob or quoted segment builds (`~/.con*/shift2eu`, `~/.config/'shift2eu'`).
		"find ~ -name agents-app-token -exec cat {} +",
		"cat ~/.con*/shift2eu/agents-app-token",
		"cat ~/.config/'shift2eu'/agents-app-token",
		// The folder, rather than the one file in it: `make agents-token` writes the token and the identity
		// beside it, and a glob, a listing or a cd reads whichever file is there now, so the whole folder is
		// refused as the deck's key directory is.
		"cat ~/.config/shift2eu/agents-app-t*",
		"cd ~/.config/shift2eu && cat agents-app-token",
		"ls ~/.config/shift2eu",
		// The tester's own marker (issue #662): the VM's login profile leaves the agents' keys file unsourced for
		// the tester only while SHIFT2EU_PI_TESTER is set and non-empty, so a command that unsets it and starts a
		// login shell puts every key back in the pane, which is the round trip the marker exists to close.
		"unset SHIFT2EU_PI_TESTER; bash -l",
		"SHIFT2EU_PI_TESTER= bash -l -c true",
		"env -u SHIFT2EU_PI_TESTER bash -l",
		// A login shell started with no environment at all: `env -i`, `--ignore-environment`, `env -`,
		// `exec -c`, `sudo -i` and `su -` each either clear the marker or start a login shell that never saw it,
		// and the VM's login profile then sources the agents' keys file, which is the round trip the marker exists
		// to close (issue #662, round 3's audit, finding SF-1).
		"env -i HOME=/home/x bash -l -c true",
		"env --ignore-environment bash -l",
		"env - bash -l",
		"exec -c bash -l",
		"sudo -i",
		"sudo -iu someone true",
		"su -l someone",
		"su - someone",
		// The same clearing and the same login shell behind the syntax that hides a command word from a
		// matcher anchored on a separator: a quoted `bash -c` argument, a keyword's body (`if`, `for`), a
		// wrapper (`command`, `eval`), an option of `exec` itself, and `su -` ended by a separator rather
		// than by the end of the command. Each still starts the login shell the marker never reached.
		"bash -c 'exec -c bash -l'",
		`sh -c "exec -c bash -l"`,
		"if true; then exec -c bash -l; fi",
		"for x in 1; do exec -c bash; done",
		"command exec -c bash -l",
		"eval exec -c bash -l",
		"exec -a name -c bash -l",
		"su -;true",
		"su -\ntrue",
		// A single key, expanded by name.
		"echo $DEEPSEEK_API_KEY",
		`printf %s "$GH_TOKEN"`,
		"echo ${AWS_SECRET_ACCESS_KEY}",
		// An interpreter that reads the environment itself, in the cheap form an
		// agent would type when the shell's printers are refused.
		`awk 'BEGIN{for(k in ENVIRON) print k}'`,
		`python3 -c 'import os; print(os.environ)'`,
		`node -e 'console.log(process.env)'`,
		// A redirect after a bare printer, numbered or not: the printer writes its
		// values to the redirect's target, or to the pipeline that follows it, so
		// the redirect changes nothing about what the printer prints.
		"env 2>&1 | grep KEY",
		"env 2>/dev/null | grep DEEP",
		"env 1>out.txt",
		"env -0 2>&1",
		"set 2>&1 | grep KEY",
		"export 2>&1",
		"declare 2>&1 | grep KEY",
		"typeset 2>&1",
		"export -p 2>&1",
		// A backslash escapes nothing in the command word: bash still runs `env`.
		`\env`,
		`\printenv`,
		`\env | grep KEY`,
		// A redirect in front of the command word changes nothing: bash runs the
		// printer and writes, or pipes on, what it printed.
		"2>&1 env",
		"</dev/null env",
		">out env",
		"> out env | grep K",
		">x set",
		"2>x export",
		"2>&1 declare -p",
		"2>/dev/null printenv",
		// `env` with an option that takes a value of its own: the value is the
		// option's argument, not a command, so bare `env` prints.
		"env -u FOO",
		"env -u FOO | grep K",
		"env -C /tmp",
		"env --unset FOO",
		// A wrapper the list does not name yet, and a wrapper's option with a
		// value: each runs `env` as the command it was given.
		"setsid env",
		"flock x env",
		"strace env",
		"watch env",
		"script -c env",
		"unbuffer env",
		"chroot / env",
		"su -c env",
		"runuser env",
		"fish -c env",
		"sudo -u x env",
		"timeout -s KILL 5 env",
		"xargs -a f env",
		"stdbuf -o L env",
		"exec -a x env",
		// The deck's key directory, read through bash instead of by a file tool:
		// every file in it is a key, or a file that leads to one.
		"grep -r . ~/.config/dot-agent-deck",
		"cat ~/.config/dot-agent-deck/*",
		"cat ~/.config/dot-agent-deck/daemon.e*",
		// A /proc path that is relative, chdir-relative or globbed reaches the same
		// file as the absolute one.
		"cat ../../../../proc/self/environ",
		"cd / && cat proc/self/environ",
		"cat /pro?/self/environ",
		// `)` ends a case pattern and a subshell before the printer, and the
		// printer is still the command bash runs.
		"case x in x) env;; esac",
		// The deck's key directory, reached through a path bash normalises or a
		// glob that names it: every spelling reads the same directory.
		"cat ~/.config//dot-agent-deck/x",
		"cat ~/.config/./dot-agent-deck/x",
		"ls ~/.config/dot-agent*",
		// The environment as an interpreter spells it, in the cheap form an agent
		// would type when the shell's own printers are refused.
		"ruby -e 'puts ENV.to_a'",
		`ruby -e 'p ENV["X"]'`,
		// The whole environment as an interpreter spells it, with no key named.
		"ruby -e 'p ENV'",
		// `gh` and `jq` print the token and the environment themselves, in the
		// forms the agents' own tooling offers.
		"gh auth token",
		"gh auth status -t",
		"gh auth status --show-token",
		"jq -n env",
		"jq -n '$ENV'",
		"python3 -c 'from os import environ as e; print(e)'",
		// A credential helper prints the token it fills in: `git credential
		// fill` writes the password to stdout, and `gh` answers the same
		// protocol itself, whatever feeds it.
		`printf 'protocol=https\nhost=github.com\n' | git credential fill`,
		"echo host=github.com | gh auth git-credential get",
		"git credential fill < req.txt",
	}

	for _, command := range cases {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			if matched := piEnvDenyMatches(patterns, command); len(matched) == 0 {
				t.Errorf("no pattern of %s refuses the command %q, which prints values CLAUDE.md rule 3 keeps out of a pane (issue #540)", piEnvDenyListPath, command)
			}
		})
	}
}

// TestPiEnvDenyListAllowsTheWork pins what the patterns must leave alone: each
// command here reads no value of the environment, and a refusal of one of them
// would stop the agents' work. `env VAR=value cmd` is the case the patterns have
// to tell from bare `env`, and the assignments, options and arguments of `set`,
// `export` and `go env` are the cases they have to tell from the printing forms.
func TestPiEnvDenyListAllowsTheWork(t *testing.T) {
	t.Parallel()

	patterns := piEnvDenyPatterns(t)

	cases := []string{
		"make check",
		"make test-e2e",
		"go test ./...",
		"go env GOPATH",
		"git -C /x status",
		"git grep -n printenv",
		"grep -rn environment docs/",
		"gh pr view 1",
		"dot-agent-deck work-done --task x",
		"env GOTOOLCHAIN=local make check",
		"set -euo pipefail; make test",
		"set -e",
		"export FOO=bar",
		"cat internal/x/env.go",
		"make vm-env VM_ENV=X",
		"go test -run TestEnv ./...",
		"cd /x && make check",
		// `env` that runs a command: no argument holds an assignment, so it prints
		// nothing, and its options drop or set a variable for that command alone.
		"/usr/bin/env bash dev/x.sh",
		"env -u FOO make test",
		"env -C dir make",
		// ps for a process's start time and arguments, and its bare forms.
		"ps -o lstart= -p 123",
		"ps -o lstart=,args= -p 123",
		"ps -ef",
		"ps aux",
		// The commands whose message and body live in a scratch file.
		"git commit -F .scratch/msg.txt",
		"gh pr create --body-file .scratch/body.md",
		"dot-agent-deck work-done --task-file '.dot-agent-deck/x.md'",
		// make's options that print nothing.
		"make worktree BRANCH=chore/1-x",
		"make -C dir check",
		"make -j4 check",
		"GOTOOLCHAIN=local make check",
		"set -e; make check",
		// The redirects the agents wrap around their own commands, on a printer
		// that runs a command or on a command that prints nothing.
		"env GOTOOLCHAIN=local make check 2>&1",
		"make check 2>&1",
		"go test ./... 2>&1 | tail",
		// A redirect in front of a command that prints nothing, in the forms the
		// agents write them: rule 10's backgrounded run verbatim, and the bare
		// redirects on `make`, on `go`'s own env and on a file.
		"cd /x && { make check < /dev/null > /tmp/log 2>&1 & echo $!; ps -o lstart= -p $!; }",
		"</dev/null make check",
		">log 2>&1 make check",
		"2>/dev/null go env GOPATH",
		"echo x > env",
		"cat < set",
		"sort <env.txt",
		"diff a b > set.txt",
		// A wrapper with an argument of its own in front of a command that prints
		// nothing, and the wrapper's own option with a value.
		"timeout 600 make check",
		"nice -n 10 make check",
		// A long but legal chain of wrappers in front of a command that prints
		// nothing: the patterns must refuse the chain above and allow this one
		// without work that grows with every wrapper.
		"sudo -u x nice -n 1 timeout 5 make check",
		// A path that holds the letters `pro` and the word `environ` without being
		// /proc or a file under it.
		"cat /home/u/projects/shift2eu/docs/dev-environment.md",
		"ls internal/adapter/target/provider/environ_test.go",
		// `gh` and `jq` in the forms that print no value of the environment.
		"gh auth status",
		"gh pr checks 12",
		"jq -r .name file.json",
		// The repository's own deck files: its configuration and the directory the
		// deck writes a task and a report into.
		"cat .dot-agent-deck.toml",
		"ls .dot-agent-deck/",
		// The program that mints the agents App's token, named beside the file it mints into: reading the
		// script hands over no credential, so the refusal of the token file must not reach it.
		"sed -n '1,20p' dev/agents-app-token.sh",
		"shellcheck dev/agents-app-token.sh",
		// The git commands that read configuration or push a branch: the
		// helper's own name and the branch's name hold no token, so the refusal
		// of the credential helper must not reach them.
		"git config --global credential.helper",
		"git push origin chore/540-deny-env-printing",
		// A diff and a test run in the repository.
		"git diff --stat",
		"go test -run TestX ./test/devenv/",
		// A command word after an option that takes a value, in the wrappers the agents type: `find`'s
		// `-exec` predicate and its placeholders, the wrapper's own option and the value it takes, the
		// remote command of `docker exec` and of `ssh`, and a program run through `exec` itself. None of
		// them clears the environment or starts a login shell, so none of them may be refused.
		"find . -exec grep -l foo {} +",
		"find . -name '*.go' -exec gofumpt -l {} \\;",
		"env -u X go test ./...",
		"env -C sub make test",
		"nice -n 10 make test",
		"xargs -0 -I{} ls {}",
		"sudo apt-get install jq",
		"docker exec -it c sh",
		"ssh -i key host true",
		"sudo -E make",
		"su -c id root",
		"exec ./bin/shift2eu --help",
	}

	for _, command := range cases {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			for _, matched := range piEnvDenyMatches(patterns, command) {
				t.Errorf("the pattern %q of %s refuses the command %q, which reads no value of the environment, so the refusal would stop the agents' work (issue #540)", matched, piEnvDenyListPath, command)
			}
		})
	}
}

// piEnvDenyPatterns compiles the patterns of the extension's list, and fails the
// test when the list cannot be read or a pattern is no regular expression: the
// tables above would otherwise pass, refusing nothing, because no pattern
// compiled.
func piEnvDenyPatterns(t *testing.T) []*regexp.Regexp {
	t.Helper()

	data := readRepositoryFile(t, piEnvDenyListPath)

	var list piEnvDenyList
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("%s: %v", piEnvDenyListPath, err)
	}

	patterns := make([]*regexp.Regexp, 0, len(list.Patterns))

	for _, pattern := range list.Patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("%s: %q is no regular expression: %v", piEnvDenyListPath, pattern, err)
		}

		patterns = append(patterns, re)
	}

	return patterns
}

// piEnvDenyMatches returns the patterns that match command, in the order the
// list writes them: the extension refuses command when this is not empty.
func piEnvDenyMatches(patterns []*regexp.Regexp, command string) []string {
	var matched []string

	for _, re := range patterns {
		if re.MatchString(command) {
			matched = append(matched, re.String())
		}
	}

	return matched
}
