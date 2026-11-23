package lint_test

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// renovateConfigPath is the repository's Renovate configuration (CLAUDE.md rule
// 7): JSON5, whose customManagers decide which pinned version Renovate finds and
// bumps.
const renovateConfigPath = ".github/renovate.json5"

// aiReviewGenericManager is the text of the description of the customManager that
// reads an annotated version pin: the `# renovate: datasource=... depName=...`
// comment and the `KEY: value` line directly below it. It is the manager that must
// find the Claude Code CLI's pin in the AI reviewer's workflow.
const aiReviewGenericManager = "Versions annotated with"

// aiReviewCLIPackage is the npm package both workflows install the Claude Code
// CLI from: the linux-x64 platform package, whose one binary
// .github/scripts/install-claude-code.sh extracts (ADR 0033).
const aiReviewCLIPackage = "@anthropic-ai/claude-code-linux-x64"

// claudeCodePinVersion captures the version the pin file blocks, as the version
// literal its version: line holds.
var claudeCodePinVersion = regexp.MustCompile(`(?m)^version:\s*(\d+\.\d+\.\d+)\s*$`)

// TestClaudeCodePinIsFoundOnce pins how Renovate finds the Claude Code CLI's pin
// (CLAUDE.md rule 7), read from .github/renovate.json5 as Renovate reads it: the
// pin is one committed file, .github/claude-code.yaml, and the generic manager
// that reads a `# renovate:` comment and the `KEY: value` line below it matches
// that file exactly once, naming the CLI's npm package and the version the file
// pins. No other customManager reads the pin file, and no manager reads the AI
// reviewer's workflow any more: the version and the integrity moved out of it,
// so a bump moves one file and both workflows' install steps read that file
// (ADR 0040's milestone-2 amendment, issue #277).
func TestClaudeCodePinIsFoundOnce(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	src, err := os.ReadFile(filepath.Join(root, claudeCodePinFile))
	if err != nil {
		t.Fatalf("read %s: %v; ADR 0040 moves the Claude Code pin into that one file, which Renovate bumps and both workflows read", claudeCodePinFile, err)
	}

	version := claudeCodePinnedVersion(t, src)

	managers := renovateCustomManagers(t, root)

	generic, others := aiReviewGenericManagerIndex(t, managers)

	// The manager only finds the pin in a file its managerFilePatterns select.
	if !renovateManagerScans(t, managers[generic], claudeCodePinFile) {
		t.Fatalf("%s: the manager described %q scans %v, not %s, so it never reads the CLI's pin (CLAUDE.md rule 7)",
			renovateConfigPath, managers[generic].description, managers[generic].filePatterns, claudeCodePinFile)
	}

	type pin struct {
		manager, depName, currentValue string
	}

	var pins []pin

	for _, expr := range managers[generic].matchStrings {
		re := compileRenovateRegexp(t, managers[generic], expr)

		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			pins = append(pins, pin{
				manager:      managers[generic].description,
				depName:      subexp(t, re, m, "depName", managers[generic], expr),
				currentValue: subexp(t, re, m, "currentValue", managers[generic], expr),
			})
		}
	}

	if len(pins) != 1 {
		t.Fatalf("%s: the manager described %q matches %s %d times (%v), want exactly the one pin of the Claude Code CLI: a second match moves a version nothing pins (CLAUDE.md rule 7)",
			renovateConfigPath, managers[generic].description, claudeCodePinFile, len(pins), pins)
	}

	if pins[0].depName != aiReviewCLIPackage {
		t.Errorf("%s: the manager described %q reads the pin file as depName %q, want %q: the package the install step extracts (CLAUDE.md rule 7)",
			renovateConfigPath, pins[0].manager, pins[0].depName, aiReviewCLIPackage)
	}

	if pins[0].currentValue != version {
		t.Errorf("%s: the manager described %q reads the pin file as currentValue %q, want the version literal its version: line holds, %q: anything else is a version Renovate bumps in the wrong place (CLAUDE.md rule 7)",
			renovateConfigPath, pins[0].manager, pins[0].currentValue, version)
	}

	for _, other := range others {
		if renovateManagerScans(t, managers[other], claudeCodePinFile) {
			t.Errorf("%s: the customManager described %q also reads %s; the generic manager reads its one annotated pin, and no second manager bumps the same version (CLAUDE.md rule 7)",
				renovateConfigPath, managers[other].description, claudeCodePinFile)
		}

		for _, expr := range managers[other].matchStrings {
			re := compileRenovateRegexp(t, managers[other], expr)

			if m := re.FindStringSubmatch(string(src)); m != nil {
				t.Errorf("%s: the customManager described %q also matches %s (%q), which the generic manager reads; one pin is read by one manager, or a bump moves two texts (CLAUDE.md rule 7)",
					renovateConfigPath, managers[other].description, claudeCodePinFile, m[0])
			}
		}
	}

	// The pin lives in the one file: a workflow that still carried the
	// annotated version and the integrity would be a second pin, which Renovate
	// and the pin check do not read (ADR 0040, issue #277).
	for _, manager := range managers {
		if renovateManagerScans(t, manager, aiReviewWorkflowPath) {
			t.Errorf("%s: the customManager described %q reads %s; the version and the integrity moved to %s, so no manager reads a pin out of the workflow (CLAUDE.md rule 7, issue #277)",
				renovateConfigPath, manager.description, aiReviewWorkflowPath, claudeCodePinFile)
		}
	}
}

// claudeCodePinnedVersion returns the version literal the pin file's version:
// line holds, and fails when the file holds none or more than one.
func claudeCodePinnedVersion(t *testing.T, pinFile []byte) string {
	t.Helper()

	matches := claudeCodePinVersion.FindAllStringSubmatch(string(pinFile), -1)
	if len(matches) != 1 {
		t.Fatalf("%s: version: is an exact version X.Y.Z on %d lines, want one: the one pin file pins the Claude Code CLI once (CLAUDE.md rule 7)", claudeCodePinFile, len(matches))
	}

	return matches[0][1]
}

// aiReviewGenericManagerIndex returns the index of the customManager that reads an
// annotated version pin, and the indices of the others.
func aiReviewGenericManagerIndex(t *testing.T, managers []renovateManager) (int, []int) {
	t.Helper()

	found, others := -1, []int{}

	for i, m := range managers {
		if !strings.Contains(m.description, aiReviewGenericManager) {
			others = append(others, i)

			continue
		}

		if found >= 0 {
			t.Fatalf("%s: two customManagers are described %q; one manager reads an annotated pin", renovateConfigPath, aiReviewGenericManager)
		}

		found = i
	}

	if found < 0 {
		t.Fatalf("%s: no customManager is described %q, so nothing finds an annotated version pin (CLAUDE.md rule 7)", renovateConfigPath, aiReviewGenericManager)
	}

	return found, others
}

// compileRenovateRegexp compiles one of a manager's matchStrings, which Renovate
// reads as a regular expression with named groups ((?<name>...), which Go's
// regexp reads too).
func compileRenovateRegexp(t *testing.T, m renovateManager, expr string) *regexp.Regexp {
	t.Helper()

	re, err := regexp.Compile(expr)
	if err != nil {
		t.Fatalf("%s: the matchStrings of the manager described %q hold %q, which Go's regexp does not compile: %v", renovateConfigPath, m.description, expr, err)
	}

	return re
}

// subexp returns the named group name of match m of expr, and fails when the
// expression has no such group.
func subexp(t *testing.T, re *regexp.Regexp, m []string, name string, manager renovateManager, expr string) string {
	t.Helper()

	index := re.SubexpIndex(name)
	if index < 0 {
		t.Fatalf("%s: the matchStrings of the manager described %q hold %q, which names no group %q", renovateConfigPath, manager.description, expr, name)
	}

	if index >= len(m) {
		t.Fatalf("%s: the matchStrings of the manager described %q hold %q, whose group %q a match does not fill", renovateConfigPath, manager.description, expr, name)
	}

	return m[index]
}

// renovateManager scans reports whether a manager reads the file at rel, a
// slash-separated path from the repository root: Renovate reads a
// managerFilePatterns entry that is wrapped in slashes as a regular expression
// against the path, and any other as a glob.
func renovateManagerScans(t *testing.T, m renovateManager, rel string) bool {
	t.Helper()

	for _, pattern := range m.filePatterns {
		if inner, ok := strings.CutPrefix(pattern, "/"); ok {
			inner, ok = strings.CutSuffix(inner, "/")
			if !ok {
				t.Fatalf("%s: the manager described %q has the file pattern %q, which starts with a slash but does not end with one", renovateConfigPath, m.description, pattern)
			}

			re, err := regexp.Compile(inner)
			if err != nil {
				t.Fatalf("%s: the manager described %q has the file pattern %q, whose regular expression does not compile: %v", renovateConfigPath, m.description, pattern, err)
			}

			if re.MatchString(rel) {
				return true
			}

			continue
		}

		matches, err := path.Match(pattern, rel)
		if err != nil {
			t.Fatalf("%s: the manager described %q has the file pattern %q, which is no glob: %v", renovateConfigPath, m.description, pattern, err)
		}

		if matches {
			return true
		}
	}

	return false
}

// renovateManager is one entry of customManagers: what it is for, the files it
// reads and the regular expressions it reads them with.
type renovateManager struct {
	description  string
	filePatterns []string
	matchStrings []string
}

// renovateCustomManagers returns the entries of customManagers in the repository's
// Renovate configuration, and fails when the document does not read.
func renovateCustomManagers(t *testing.T, root string) []renovateManager {
	t.Helper()

	doc, err := os.ReadFile(filepath.Join(root, renovateConfigPath))
	if err != nil {
		t.Fatalf("read %s: %v; the file's customManagers decide which pinned versions Renovate bumps (CLAUDE.md rule 7)", renovateConfigPath, err)
	}

	managers, err := parseCustomManagers(string(doc))
	if err != nil {
		t.Fatalf("%s: %v", renovateConfigPath, err)
	}

	if len(managers) == 0 {
		t.Fatalf("%s: customManagers holds no manager", renovateConfigPath)
	}

	return managers
}

// parseCustomManagers reads the customManagers array of a Renovate JSON5 document:
// each entry's description, its managerFilePatterns and its matchStrings. It
// refuses a shape it does not follow rather than guess at it: a document that is
// no object, a customManagers that is no array of objects, and an entry whose
// description, managerFilePatterns or matchStrings is missing or of another type.
func parseCustomManagers(doc string) ([]renovateManager, error) {
	tokens, err := json5Tokens(doc)
	if err != nil {
		return nil, err
	}

	value, next, err := json5Value(tokens, 0)
	if err != nil {
		return nil, err
	}

	if next != len(tokens) {
		return nil, fmt.Errorf("the document holds %d tokens after its one value", len(tokens)-next)
	}

	config, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("the document is no object")
	}

	entries, ok := config["customManagers"].([]any)
	if !ok {
		return nil, errors.New("customManagers is no array")
	}

	managers := make([]renovateManager, 0, len(entries))

	for i, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("customManagers entry %d is no object", i+1)
		}

		manager := renovateManager{}

		if manager.description, ok = fields["description"].(string); !ok {
			return nil, fmt.Errorf("customManagers entry %d holds no description", i+1)
		}

		if manager.filePatterns, err = json5Strings(fields["managerFilePatterns"]); err != nil {
			return nil, fmt.Errorf("customManagers entry %d: managerFilePatterns: %w", i+1, err)
		}

		if manager.matchStrings, err = json5Strings(fields["matchStrings"]); err != nil {
			return nil, fmt.Errorf("customManagers entry %d: matchStrings: %w", i+1, err)
		}

		managers = append(managers, manager)
	}

	return managers, nil
}

// json5Strings reads a JSON5 value as the strings an array holds, and refuses
// anything else.
func json5Strings(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("no array")
	}

	out := make([]string, 0, len(items))

	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, errors.New("an element is no string")
		}

		out = append(out, s)
	}

	return out, nil
}

// json5Tokens splits a JSON5 document into its tokens: a quoted string, decoded,
// a bare word, and one of the punctuation characters { } [ ] : ,. Line comments,
// block comments and white space are skipped.
//
// A string's backslash escapes the character after it, so '\\s' is the two
// characters \s and '\"' one quote: that is how Renovate's regular expressions are
// written. An escape of any other character is kept as written, so a regular
// expression written with a single backslash reaches regexp unchanged rather than
// losing the backslash.
func json5Tokens(doc string) ([]string, error) {
	tokens := []string{}

	for i := 0; i < len(doc); {
		switch {
		case strings.ContainsRune(" \t\r\n", rune(doc[i])):
			i++
		case strings.HasPrefix(doc[i:], "//"):
			for i < len(doc) && doc[i] != '\n' {
				i++
			}
		case strings.HasPrefix(doc[i:], "/*"):
			end := strings.Index(doc[i+2:], "*/")
			if end < 0 {
				return nil, errors.New("a block comment is not closed")
			}

			i += 2 + end + 2
		case doc[i] == '\'' || doc[i] == '"':
			text, next, err := json5String(doc, i)
			if err != nil {
				return nil, err
			}

			tokens = append(tokens, text)

			i = next
		case strings.ContainsRune("{}[]:,", rune(doc[i])):
			tokens = append(tokens, doc[i:i+1])

			i++
		default:
			start := i

			for i < len(doc) && !strings.ContainsRune(" \t\r\n{}[]:,'\"", rune(doc[i])) &&
				!strings.HasPrefix(doc[i:], "//") && !strings.HasPrefix(doc[i:], "/*") {
				i++
			}

			if start == i {
				return nil, fmt.Errorf("the character %q is no part of a JSON5 document this reader follows", doc[i])
			}

			tokens = append(tokens, doc[start:i])
		}
	}

	return tokens, nil
}

// json5String reads the quoted string that starts at doc[start], the quote
// included, and returns it decoded with the index of the character after its
// closing quote.
func json5String(doc string, start int) (string, int, error) {
	quote := doc[start]

	var text strings.Builder

	for i := start + 1; i < len(doc); i++ {
		switch doc[i] {
		case quote:
			return text.String(), i + 1, nil
		case '\\':
			if i+1 >= len(doc) {
				return "", 0, errors.New("a string ends in a backslash")
			}

			switch next := doc[i+1]; next {
			case '\\', '\'', '"':
				text.WriteByte(next)
			default:
				text.WriteByte('\\')
				text.WriteByte(next)
			}

			i++
		default:
			text.WriteByte(doc[i])
		}
	}

	return "", 0, fmt.Errorf("the string that starts at byte %d is not closed", start)
}

// json5Value parses the JSON5 value the tokens from i describe and returns it with
// the index of the first token after it: an object as map[string]any, an array as
// []any, and a string, quoted or bare, as string.
func json5Value(tokens []string, i int) (any, int, error) {
	if i >= len(tokens) {
		return nil, i, errors.New("the document ends before a value")
	}

	switch tokens[i] {
	case "{":
		object := map[string]any{}

		i++

		for {
			if i >= len(tokens) {
				return nil, i, errors.New("an object is not closed")
			}

			if tokens[i] == "}" {
				return object, i + 1, nil
			}

			key := tokens[i]

			i++
			if i >= len(tokens) || tokens[i] != ":" {
				return nil, i, fmt.Errorf("the key %q is followed by no colon", key)
			}

			value, next, err := json5Value(tokens, i+1)
			if err != nil {
				return nil, next, err
			}

			object[key] = value

			i = next
			if i < len(tokens) && tokens[i] == "," {
				i++
			}
		}
	case "[":
		var array []any

		i++

		for {
			if i >= len(tokens) {
				return nil, i, errors.New("an array is not closed")
			}

			if tokens[i] == "]" {
				return array, i + 1, nil
			}

			value, next, err := json5Value(tokens, i)
			if err != nil {
				return nil, next, err
			}

			array = append(array, value)

			i = next
			if i < len(tokens) && tokens[i] == "," {
				i++
			}
		}
	default:
		return tokens[i], i + 1, nil
	}
}
