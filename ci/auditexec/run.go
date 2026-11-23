package main

import (
	"fmt"
	"io"
	"strings"
)

const (
	// checkout is the repository every git command reads: the audit's copy of
	// the checkout, the wrapper's own -C.
	checkout = "/srv/audit/checkout"

	// checks is the directory of the model-free step's check outputs, which
	// the audit reads.
	checks = "/srv/audit/checks"

	// work is the wrapper's fixed working directory, which it changes into
	// before every exec: the audit's work area, where a relative name on a
	// line that is no git line is read from.
	work = "/srv/audit/work"

	// exitRefused is the status of a refused name, subcommand or option.
	exitRefused = 126

	// execFailedExit is the status of a listed command the wrapper could not
	// change into its working directory for, or replace itself with.
	execFailedExit = 127

	// maxWord is the most of a refused word the stderr line shows, in bytes.
	maxWord = 64
)

// commands maps each name the wrapper runs to its fixed absolute path.
var commands = map[string]string{
	"git":       "/usr/bin/git",
	"jq":        "/usr/bin/jq",
	"sha256sum": "/usr/bin/sha256sum",
	"base64":    "/usr/bin/base64",
	"od":        "/usr/bin/od",
	"cat":       "/usr/bin/cat",
	"head":      "/usr/bin/head",
	"tail":      "/usr/bin/tail",
	"wc":        "/usr/bin/wc",
	"sort":      "/usr/bin/sort",
	"uniq":      "/usr/bin/uniq",
	"cut":       "/usr/bin/cut",
	"tr":        "/usr/bin/tr",
}

// gitSubcommands are the read-only subcommands git runs with.
var gitSubcommands = map[string]bool{
	"log":          true,
	"show":         true,
	"diff":         true,
	"grep":         true,
	"ls-files":     true,
	"ls-tree":      true,
	"cat-file":     true,
	"rev-parse":    true,
	"rev-list":     true,
	"blame":        true,
	"shortlog":     true,
	"describe":     true,
	"for-each-ref": true,
}

// gitRefusedLong are the long git options refused anywhere on the line, alone,
// in their --opt=value form and abbreviated (git's option parser takes a
// unique prefix of a long option): each names another configuration, git
// directory, work tree, namespace, program or output file, or changes how git
// reads the repository or its work tree (--super-prefix, --attr-source,
// --bare, --literal-pathspecs: nothing but what the wrapper adds comes before
// the subcommand, issue #277 round H). --open-files-in-pager is git grep's, which runs a program of the caller's;
// --show-signature verifies a commit's signature, which runs gpg;
// --no-index makes git diff compare two files of its own, no part of the
// checkout.
var gitRefusedLong = []string{
	"--config-env",
	"--exec-path",
	"--git-dir",
	"--work-tree",
	"--namespace",
	"--ext-diff",
	"--textconv",
	"--output",
	"--open-files-in-pager",
	"--show-signature",
	"--no-index",
	"--super-prefix",
	"--attr-source",
	"--bare",
	"--literal-pathspecs",
}

// gitSignatureAtom is for-each-ref's format and sort atom that asks git to
// verify a signature, %(signature...) in a format and signature... as a
// --sort key.
const gitSignatureAtom = "signature"

// gitSignatureModifiers are the bytes git takes between a placeholder's % and
// its letter: %+G, %-G and % G are %G with a line feed added or removed or a
// space added.
const gitSignatureModifiers = "+- "

// gitOwnOptions are git options of their own that are a prefix of a refused
// one, and so no abbreviation of it.
var gitOwnOptions = map[string]bool{"--text": true}

// gitRefusedLetters are the short git options refused anywhere on the line, in
// any single-dash word that holds them: git reads a cluster of short options
// one letter at a time, so -c (a configuration value) and -O (git grep's
// --open-files-in-pager) act in the middle or at the end of a cluster as at
// its start (-iOsh runs sh). -C is refused in every form, separate, attached
// or in a cluster (issue #277, round H): the wrapper's own -C
// /srv/audit/checkout comes first, and git applies its -C options in order, so
// a caller's later -C would run git in a directory of its choosing, the work
// area the model writes in among them, whose planted .git/config names a
// program (core.fsmonitor, diff.external). As a subcommand option (git log's
// copy detection) it is refused all the same: the check fails closed.
const gitRefusedLetters = "cOC"

// jqModuleWords are the words of jq's module system: a jq word that holds one
// is refused, wherever it sits, since a module directive reads any .json file
// it names (issue #277, round H). The audit needs filters alone, so a word
// that merely mentions one is refused too.
var jqModuleWords = []string{"import", "include"}

// jqLibraryLetter is jq's short -L, the library path, refused in any
// single-dash word that holds it.
const jqLibraryLetter = "L"

// jqLibraryLong is jq's long --library-path, refused alone, with its value
// attached after "=" and abbreviated.
const jqLibraryLong = "--library-path"

// sortShortAllowed are the short sort options that only change how sort
// orders and compares its lines.
const sortShortAllowed = "bdfghiMnrRsuVz"

// sortShortValued are the short sort options that take a value, attached or as
// the next argument: -k a key, -t a field separator.
const sortShortValued = "kt"

// sortLongAllowed are the long forms of sort's allowed short options, which
// take no value. sort's long options are taken whole: an abbreviation is
// refused, so no prefix ever reaches an option outside the list.
var sortLongAllowed = map[string]bool{
	"--ignore-leading-blanks": true,
	"--dictionary-order":      true,
	"--ignore-case":           true,
	"--general-numeric-sort":  true,
	"--human-numeric-sort":    true,
	"--ignore-nonprinting":    true,
	"--month-sort":            true,
	"--numeric-sort":          true,
	"--reverse":               true,
	"--random-sort":           true,
	"--stable":                true,
	"--unique":                true,
	"--version-sort":          true,
	"--zero-terminated":       true,
}

// sortLongValued are the long sort options that take their value attached
// after "=": --key and --field-separator.
var sortLongValued = []string{"--key=", "--field-separator="}

// uniqShortValued are uniq's short options that take a value, attached or as
// the next argument: -f, -s and -w each take a count.
const uniqShortValued = "fsw"

// uniqLongValued are uniq's long options that take their value as the next
// argument when no "=" holds it.
var uniqLongValued = map[string]bool{
	"--skip-fields": true,
	"--skip-chars":  true,
	"--check-chars": true,
}

// env is the whole environment every command runs with: nothing of the
// caller's.
var env = []string{
	"PATH=/usr/bin:/bin",
	"HOME=/nonexistent",
	"TMPDIR=/srv/audit/work/tmp",
	"LANG=C.UTF-8",
	"LC_ALL=C.UTF-8",
	"TZ=UTC",
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_PAGER=cat",
	"GIT_OPTIONAL_LOCKS=0",
}

// run maps args to the command it execs through exec, which replaces the
// process and returns only when it could not: syscall.Exec, or a test's fake.
// Before the exec it changes into its fixed working directory, work, through
// chdir: os.Chdir, or a test's fake. It returns 126 for a refused command
// line, after one line on stderr naming the refused word, having changed and
// run nothing, and 127 when chdir or exec failed; 0 is a fake's answer alone.
func run(
	args []string,
	chdir func(dir string) error,
	exec func(path string, argv []string, env []string) error,
	stderr io.Writer,
) int {
	argv, refused, ok := commandLine(args)
	if !ok {
		fmt.Fprintf(stderr, "audit-exec: refused: %s\n", quote(refused))
		return exitRefused
	}

	if err := chdir(work); err != nil {
		fmt.Fprintf(stderr, "audit-exec: cannot change into %s: %s\n", work, quote(err.Error()))
		return execFailedExit
	}

	if err := exec(argv[0], argv, append([]string(nil), env...)); err != nil {
		fmt.Fprintf(stderr, "audit-exec: cannot run %s: %s\n", argv[0], quote(err.Error()))
		return execFailedExit
	}

	return 0
}

// commandLine returns the argument vector args maps to, its program's path
// first, or the word it refuses.
func commandLine(args []string) ([]string, string, bool) {
	if len(args) == 0 {
		return nil, "", false
	}

	name, rest := args[0], args[1:]

	path, ok := commands[name]
	if !ok {
		return nil, name, false
	}

	// The option rules come first, so a refused option is the word named even
	// when its value is a path outside the roots too; the path rule then holds
	// every path word of the line, before git's subcommand is looked at.
	switch name {
	case "git":
		forEachRef := len(rest) > 0 && rest[0] == "for-each-ref"

		for _, arg := range rest {
			if refusedGitOption(arg) || asksForSignature(arg, forEachRef) {
				return nil, arg, false
			}
		}

		if refused, ok := pathLine(name, rest); !ok {
			return nil, refused, false
		}

		if len(rest) == 0 {
			return nil, name, false
		}

		if !gitSubcommands[rest[0]] {
			return nil, rest[0], false
		}

		return append([]string{path, "--no-pager", "-C", checkout}, rest...), "", true
	case "sort":
		if refused, ok := sortLine(rest); !ok {
			return nil, refused, false
		}
	case "uniq":
		if refused, ok := uniqLine(rest); !ok {
			return nil, refused, false
		}
	case "jq":
		if refused, ok := jqLine(rest); !ok {
			return nil, refused, false
		}
	}

	if refused, ok := pathLine(name, rest); !ok {
		return nil, refused, false
	}

	return append([]string{path}, rest...), "", true
}

// allowedRoots are the trees every path word of a line must lie in: the
// checkout, the model-free step's check outputs and the work area, the trees
// the settings' Read allow list names for the audit (ADR 0040, issue #277
// round G).
var allowedRoots = []string{checkout, checks, work}

// endOfOptions is the word after which every word is an operand.
const endOfOptions = "--"

// parentElement is the path element that names a directory's parent.
const parentElement = ".."

// pathLine returns the first word of the line of the command name that holds
// a path word outside allowedRoots, or true when none does: the wrapper's path
// rule (ADR 0040, amended 2026-11-23, issue #277 rounds F and G). Claude Code's
// deny rules match the command's text, so a glob gets past them; the wrapper
// reads the words the shell expanded, and holds them to an allow list, since a
// deny list of roots kept leaking. A path word is
//
//   - a word that is no option (no leading "-") and holds a "/", or is "..";
//     "-" alone is stdin and no path word;
//   - every word after "--", whatever its first character;
//   - an option word's value after its first "=";
//   - the rest of a single-dash word after its first letter, when that rest
//     holds a "/" (git grep -f/sys/x, sort -k/x).
//
// git's -C is no path word here: a caller's -C is refused before this rule
// reads the line (round H), so the wrapper's own -C is the only one git reads.
//
// A relative path word is read from /srv/audit/checkout on a git line, where
// git reads it under the wrapper's -C, and from /srv/audit/work, the
// wrapper's working directory, on any other (allowedPath). The check reads
// every word whatever it belongs to, so it fails closed: a git grep pattern
// holding an absolute path outside the roots is refused too. The refused word
// is the whole word, the option's included.
func pathLine(name string, args []string) (string, bool) {
	base := work
	if name == "git" {
		base = checkout
	}

	operands := false

	for _, arg := range args {
		var paths []string

		switch {
		case operands:
			paths = []string{arg}
		case arg == endOfOptions:
			operands = true
		case arg == "-":
		case strings.HasPrefix(arg, "-"):
			paths = optionPaths(arg)
		case strings.Contains(arg, "/") || arg == parentElement:
			paths = []string{arg}
		}

		for _, p := range paths {
			if !allowedPath(p, base) {
				return arg, false
			}
		}
	}

	return "", true
}

// optionPaths returns the path words an option word holds: its value after
// the first "=", and, for a single-dash word, the rest after its first letter
// when that rest holds a "/".
func optionPaths(arg string) []string {
	var paths []string

	if _, value, ok := strings.Cut(arg, "="); ok {
		paths = append(paths, value)
	}

	if !strings.HasPrefix(arg, endOfOptions) && len(arg) > len("-x") {
		if rest := arg[len("-x"):]; strings.Contains(rest, "/") {
			paths = append(paths, rest)
		}
	}

	return paths
}

// allowedPath reports whether p, read from base when it is relative, lies in
// one of allowedRoots: it holds no ".." element at all, which is refused
// before anything is cleaned (the kernel follows a link such as /dev/fd before
// it takes a "..", so a lexical clean would not show where the path leads),
// and, cleaned of its empty and "." elements, is one of the roots or lies
// under one. "-" alone is stdin.
func allowedPath(p, base string) bool {
	if p == "-" {
		return true
	}

	if !strings.HasPrefix(p, "/") {
		p = base + "/" + p
	}

	var kept []string

	for _, element := range strings.Split(p, "/") {
		switch element {
		case "", ".":
		case parentElement:
			return false
		default:
			kept = append(kept, element)
		}
	}

	cleaned := "/" + strings.Join(kept, "/")

	for _, root := range allowedRoots {
		if cleaned == root || strings.HasPrefix(cleaned, root+"/") {
			return true
		}
	}

	return false
}

// refusedGitOption reports whether arg is one of git's refused options.
func refusedGitOption(arg string) bool {
	if !strings.HasPrefix(arg, "--") {
		return strings.HasPrefix(arg, "-") && strings.ContainsAny(arg[1:], gitRefusedLetters)
	}

	name, _, _ := strings.Cut(arg, "=")
	if gitOwnOptions[name] && !strings.Contains(arg, "=") {
		return false
	}

	for _, long := range gitRefusedLong {
		if name == long || (len(name) > len("--") && strings.HasPrefix(long, name)) {
			return true
		}
	}

	return false
}

// asksForSignature reports whether arg, any word of git's line, could ask git
// to verify a signature, which runs gpg as --show-signature does: a word
// holding %G, with or without one of git's modifiers between the % and the G
// (%G?, %+GS, %-GK, % GG), or for-each-ref's %(signature...) atom; and, on a
// for-each-ref line, any word holding signature, the --sort key attached,
// negated or as the next word. The check reads the word whatever option it
// belongs to, so it fails closed: a grep pattern holding %G is refused too.
func asksForSignature(arg string, forEachRef bool) bool {
	if strings.Contains(arg, "%("+gitSignatureAtom) {
		return true
	}

	if forEachRef && strings.Contains(arg, gitSignatureAtom) {
		return true
	}

	for i := range len(arg) {
		if arg[i] != '%' {
			continue
		}

		next := i + 1
		if next < len(arg) && strings.IndexByte(gitSignatureModifiers, arg[next]) >= 0 {
			next++
		}

		if next < len(arg) && arg[next] == 'G' {
			return true
		}
	}

	return false
}

// sortLine returns the first word of sort's arguments outside its allow list,
// or true when every option is allowed. Short options are the allowed letters,
// alone or in a cluster, with -k and -t taking the rest of their word or the
// next argument as their value; long options are the allowed ones, whole.
// Every other option is refused: those that run a program
// (--compress-program), read another file (--files0-from, --random-source),
// write one (-o, --output) or use another directory (-T,
// --temporary-directory) among them. After "--", every word is a file sort
// reads.
func sortLine(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--":
			return "", true
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			continue
		case strings.HasPrefix(arg, "--"):
			if !sortLongAllowed[arg] && !hasAnyPrefix(arg, sortLongValued) {
				return arg, false
			}
		default:
			takesNext, ok := shortCluster(arg[1:], sortShortAllowed, sortShortValued)
			if !ok {
				return arg, false
			}

			if takesNext {
				i++
			}
		}
	}

	return "", true
}

// uniqLine returns the word that makes uniq write a file, or true when it reads
// only: uniq's second operand is OUT, the file it writes, so a line with more
// than one operand is refused and names that second operand. Options stay
// uniq's own; -f, -s and -w (and their long forms) take the next argument as
// their value when their word holds none.
func uniqLine(args []string) (string, bool) {
	operands := 0

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--":
			for _, operand := range args[i+1:] {
				operands++
				if operands > 1 {
					return operand, false
				}
			}

			return "", true
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			operands++
			if operands > 1 {
				return arg, false
			}
		case strings.HasPrefix(arg, "--"):
			if uniqLongValued[arg] {
				i++
			}
		default:
			if takesNext, _ := shortCluster(arg[1:], "", uniqShortValued); takesNext {
				i++
			}
		}
	}

	return "", true
}

// jqLine returns the first word of jq's arguments that reaches its module
// system, or true when none does: a word holding import or include, wherever
// it sits (a filter, an --arg value, a file name: the check fails closed); a
// single-dash word holding -L, alone, attached or in a cluster; and
// --library-path, alone, in its =value form or abbreviated. The path rule
// still holds every other word.
func jqLine(args []string) (string, bool) {
	for _, arg := range args {
		for _, word := range jqModuleWords {
			if strings.Contains(arg, word) {
				return arg, false
			}
		}

		if !strings.HasPrefix(arg, "--") {
			if strings.HasPrefix(arg, "-") && strings.Contains(arg[1:], jqLibraryLetter) {
				return arg, false
			}

			continue
		}

		name, _, _ := strings.Cut(arg, "=")
		if len(name) > len("--") && strings.HasPrefix(jqLibraryLong, name) {
			return arg, false
		}
	}

	return "", true
}

// shortCluster reads a cluster of short options, the word after its dash. A
// letter in valued takes the rest of the word as its value, or the next
// argument when the word ends with it (takesNext). It reports false when a
// letter is in neither allowed nor valued, unless allowed is empty, which
// takes any letter.
func shortCluster(cluster, allowed, valued string) (takesNext, ok bool) {
	for i, letter := range cluster {
		if strings.ContainsRune(valued, letter) {
			return i == len(cluster)-1, true
		}

		if allowed != "" && !strings.ContainsRune(allowed, letter) {
			return false, false
		}
	}

	return false, true
}

// hasAnyPrefix reports whether word starts with one of prefixes.
func hasAnyPrefix(word string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(word, prefix) {
			return true
		}
	}

	return false
}

// quote returns word cut to maxWord bytes and quoted as a Go string, so the
// stderr line stays one line whatever the word holds.
func quote(word string) string {
	if len(word) > maxWord {
		word = word[:maxWord]
	}

	return fmt.Sprintf("%q", word)
}
