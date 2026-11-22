package audit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// gitKeyRule is one entry of the git config allow list: a section, whether it
// takes a subsection and which, and the keys it may hold.
type gitKeyRule struct {
	// section is the section's name, lower-case.
	section string
	// subsection is the rule's subsection: "" for a section without one,
	// anySubsection for a section with any one, else the one subsection
	// allowed, compared exactly, as git compares subsections.
	subsection string
	// keys are the keys the section may hold, lower-case.
	keys []string
}

// anySubsection marks a rule whose section takes any subsection.
const anySubsection = "\x00any"

// gitConfigAllowList is the checkout copy's git config allow list (ADR 0040,
// step 3): the keys a clone actions/checkout makes with persist-credentials:
// false carries, a partial clone's included. Every other key and section is
// refused.
var gitConfigAllowList = []gitKeyRule{
	{section: "core", keys: []string{"repositoryformatversion", "filemode", "bare", "logallrefupdates"}},
	{section: "remote", subsection: "origin", keys: []string{"url", "fetch", "promisor", "partialclonefilter"}},
	{section: "gc", keys: []string{"auto"}},
	{section: "branch", subsection: anySubsection, keys: []string{"remote", "merge"}},
	{section: "extensions", keys: []string{"objectformat"}},
}

// gitRefusedFiles are the files beside .git/config that git reads a
// configuration or a repository out of: a worktree's own configuration, a
// common directory and a gitdir pointer. Each is refused by its presence, in
// any file type.
var gitRefusedFiles = []string{"config.worktree", "commondir", "gitdir"}

// maxGitConfig caps the git config the layout check reads.
const maxGitConfig = 1 << 20

// utf8BOM is the byte order mark git skips at a config's start; the parser
// refuses it instead.
const utf8BOM = "\xef\xbb\xbf"

// checkGitConfig checks the checkout copy's git directory: .git a real
// directory, no file or link; none of config.worktree, commondir and gitdir in
// it; and its config, when there is one, a regular file whose every line is
// blank, a comment, a section header or a key = value line of the allow list
// (gitConfigAllowList), each in the form git writes it.
func checkGitConfig(checkout string) error {
	gitDir := filepath.Join(checkout, ".git")

	info, err := os.Lstat(gitDir)
	if err != nil {
		return fmt.Errorf("the audit directory: the checkout copy's .git: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("the audit directory: the checkout copy's .git is %s, want a directory: %w", info.Mode().Type(), errLayout)
	}

	for _, name := range gitRefusedFiles {
		_, err := os.Lstat(filepath.Join(gitDir, name))

		switch {
		case err == nil:
			return fmt.Errorf("the audit directory: the checkout copy's .git holds %s: %w", name, errLayout)
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("the audit directory: the checkout copy's .git/%s cannot be checked: %w", name, err)
		}
	}

	config := filepath.Join(gitDir, "config")

	info, err = os.Lstat(config)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("the audit directory: the checkout copy's git config: %w", err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("the audit directory: the checkout copy's git config is %s, want a regular file: %w", info.Mode().Type(), errLayout)
	}

	body, err := readCapped(config, maxGitConfig)
	if err != nil {
		return fmt.Errorf("the audit directory: the checkout copy's git config: %w", err)
	}

	if err := checkGitConfigBody(string(body)); err != nil {
		return fmt.Errorf("the audit directory: the checkout copy's git config: %w", err)
	}

	return nil
}

// checkGitConfigBody parses a git config strictly and refuses it unless every
// line is placed and every key is on the allow list. A line is placed when it
// is blank, a comment (# or ;), a section header alone on its line, or a
// key = value line under a header whose value does not continue on the next
// line. A refusal names the line's number and, for a key, its section and
// name; it never repeats a value or a subsection, either of which could hold a
// credential.
func checkGitConfigBody(body string) error {
	if strings.HasPrefix(body, utf8BOM) {
		return fmt.Errorf("it begins with a byte order mark: %w", errLayout)
	}

	var rule *gitKeyRule

	number := 0

	for line := range strings.Lines(body) {
		number++

		line = strings.TrimLeft(line, " \t")
		line = strings.TrimRight(line, "\r\n")
		line = strings.TrimRight(line, " \t")

		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
			continue
		case line[0] == '[':
			section, subsection, err := parseGitHeader(line)
			if err != nil {
				return fmt.Errorf("line %d: %w", number, err)
			}

			rule = allowedGitSection(section, subsection)
			if rule == nil {
				return fmt.Errorf("line %d: the section [%s] is not on the allow list: %w", number, section, errLayout)
			}
		default:
			key, err := parseGitKey(line)
			if err != nil {
				return fmt.Errorf("line %d: %w", number, err)
			}

			if rule == nil {
				return fmt.Errorf("line %d: a key before any section: %w", number, errLayout)
			}

			if !slices.Contains(rule.keys, key) {
				return fmt.Errorf("line %d: the key %s.%s is not on the allow list: %w", number, rule.section, key, errLayout)
			}
		}
	}

	return nil
}

// allowedGitSection returns the allow list's rule for a section, lower-case,
// and its subsection (nil for none), or nil when the list has none.
func allowedGitSection(section string, subsection *string) *gitKeyRule {
	for i := range gitConfigAllowList {
		rule := &gitConfigAllowList[i]
		if rule.section != section {
			continue
		}

		switch {
		case subsection == nil && rule.subsection == "":
			return rule
		case subsection != nil && rule.subsection == anySubsection:
			return rule
		case subsection != nil && rule.subsection != "" && *subsection == rule.subsection:
			return rule
		}
	}

	return nil
}

// parseGitHeader parses a section header line, [section] or
// [section "subsection"], and returns the section's name, lower-case, and the
// subsection, nil for none. The name is git's ([A-Za-z0-9-]+, the old dotted
// form refused); the subsection is quoted, with git's escapes \" and \\ and no
// other, and closed by "]. Nothing may follow the closing ], not even a
// comment: git never writes it there.
func parseGitHeader(line string) (string, *string, error) {
	rest := line[1:]

	end := strings.IndexFunc(rest, func(r rune) bool { return !isGitNameRune(r) })
	if end <= 0 {
		return "", nil, fmt.Errorf("a section header without a name git writes: %w", errLayout)
	}

	section, rest := strings.ToLower(rest[:end]), rest[end:]

	if rest == "]" {
		return section, nil, nil
	}

	if rest[0] != ' ' && rest[0] != '\t' {
		return "", nil, fmt.Errorf("a section header of a form git does not write: %w", errLayout)
	}

	rest = strings.TrimLeft(rest, " \t")
	if rest == "" || rest[0] != '"' {
		return "", nil, fmt.Errorf("a section header whose subsection is not quoted: %w", errLayout)
	}

	subsection, tail, err := parseGitSubsection(rest[1:])
	if err != nil {
		return "", nil, err
	}

	if tail != "]" {
		return "", nil, fmt.Errorf("a section header whose subsection is not closed by \"] alone: %w", errLayout)
	}

	return section, &subsection, nil
}

// parseGitSubsection reads a quoted subsection up to its closing quote, rest
// starting just after the opening one, and returns it and what follows the
// closing quote.
func parseGitSubsection(rest string) (string, string, error) {
	var sub strings.Builder

	for i := 0; i < len(rest); i++ {
		switch c := rest[i]; c {
		case '"':
			return sub.String(), rest[i+1:], nil
		case '\\':
			if i+1 == len(rest) || (rest[i+1] != '"' && rest[i+1] != '\\') {
				return "", "", fmt.Errorf("a subsection with an escape git does not write: %w", errLayout)
			}

			i++
			sub.WriteByte(rest[i])
		default:
			sub.WriteByte(c)
		}
	}

	return "", "", fmt.Errorf("a subsection whose quote is never closed: %w", errLayout)
}

// parseGitKey parses a key = value line and returns the key, lower-case. The
// key is git's ([A-Za-z][A-Za-z0-9-]*), followed by optional blanks and the
// =; a value ending in \ continues on the next line and is refused, whatever
// the line after it holds.
func parseGitKey(line string) (string, error) {
	end := strings.IndexFunc(line, func(r rune) bool { return !isGitNameRune(r) })
	if end < 0 {
		end = len(line)
	}

	key, rest := line[:end], strings.TrimLeft(line[end:], " \t")

	if key == "" || !isGitLetter(key[0]) || !strings.HasPrefix(rest, "=") {
		return "", fmt.Errorf("a line that is neither blank, a comment, a section header nor key = value: %w", errLayout)
	}

	if strings.HasSuffix(rest, `\`) {
		return "", fmt.Errorf("a value continued on the next line: %w", errLayout)
	}

	return strings.ToLower(key), nil
}

// isGitNameRune reports whether r may appear in a git section or key name.
func isGitNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-'
}

// isGitLetter reports whether c is an ASCII letter.
func isGitLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
