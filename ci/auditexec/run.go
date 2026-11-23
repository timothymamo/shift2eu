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

	// exitRefused is the status of a refused name, subcommand or option.
	exitRefused = 126

	// execFailedExit is the status of a listed command the wrapper could not
	// replace itself with.
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
// directory, work tree, namespace, program or output file.
// --open-files-in-pager is git grep's, which runs a program of the caller's;
// --show-signature verifies a commit's signature, which runs gpg.
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
// its start (-iOsh runs sh).
const gitRefusedLetters = "cO"

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
// It returns 126 for a refused command line, after one line on stderr naming
// the refused word, and 127 when exec failed; 0 is a fake's answer alone.
func run(args []string, exec func(path string, argv []string, env []string) error, stderr io.Writer) int {
	argv, refused, ok := commandLine(args)
	if !ok {
		fmt.Fprintf(stderr, "audit-exec: refused: %s\n", quote(refused))
		return exitRefused
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

	if refused, ok := pathLine(name, rest); !ok {
		return nil, refused, false
	}

	switch name {
	case "git":
		forEachRef := len(rest) > 0 && rest[0] == "for-each-ref"

		for _, arg := range rest {
			if refusedGitOption(arg) || asksForSignature(arg, forEachRef) {
				return nil, arg, false
			}
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
	}

	return append([]string{path}, rest...), "", true
}

// refusedRoots are the trees no path on a line may name: the kernel's views of
// every process (their environments among them), of the system and of the
// devices.
var refusedRoots = []string{"/proc", "/sys", "/dev"}

// gitDirOption is git's own option that names the directory git runs in.
const gitDirOption = "-C"

// pathLine returns the first word of the line of the command name that names
// a path into one of refusedRoots, or true when none does: the wrapper's path
// rule (ADR 0040, amended 2026-11-23, issue #277 round F). Claude Code's deny
// rules match the command's text, so a glob gets past them; the wrapper reads
// the words the shell expanded. A word names a path when it is
//
//   - no option word (no leading "-"; "-" alone is stdin) and holds a "/": a
//     bare word such as a grep pattern, a ref or a jq filter names no path, and
//     a word without a "/" reaches none of the three from the work directory;
//   - an option word holding "=": its value after the first "=";
//   - on a git line, the value of -C, attached (-C/proc/1) or the next word.
//
// The check reads every word whatever it belongs to, so it fails closed: a
// git grep pattern that reads as such a path is refused too. The refused word
// is the whole word, the option's included.
func pathLine(name string, args []string) (string, bool) {
	for i, arg := range args {
		switch {
		case name == "git" && arg == gitDirOption:
			if i+1 < len(args) && refusedPath(args[i+1]) {
				return args[i+1], false
			}
		case name == "git" && strings.HasPrefix(arg, gitDirOption):
			if refusedPath(arg[len(gitDirOption):]) {
				return arg, false
			}
		case arg == "-":
			continue
		case strings.HasPrefix(arg, "-"):
			if _, value, ok := strings.Cut(arg, "="); ok && refusedPath(value) {
				return arg, false
			}
		case strings.Contains(arg, "/"):
			if refusedPath(arg) {
				return arg, false
			}
		}
	}

	return "", true
}

// refusedPath reports whether p, cleaned as filepath.Clean cleans it and read
// from "/" when it is relative, is one of refusedRoots or lies under one. A
// relative path is read from "/" so that it is refused whatever directory it
// runs in: ../../proc/1/environ reaches /proc from the work directory.
func refusedPath(p string) bool {
	cleaned := cleanRooted(p)

	for _, root := range refusedRoots {
		if cleaned == root || strings.HasPrefix(cleaned, root+"/") {
			return true
		}
	}

	return false
}

// cleanRooted returns p read from "/" and cleaned lexically, as
// filepath.Clean("/" + p) does on a unix system: empty and "." elements
// dropped, each ".." removing the element before it, and a ".." at the root
// staying at the root. The wrapper's depguard rule (audit-exec) keeps it to
// the standard packages it imports, which path/filepath is not among.
func cleanRooted(p string) string {
	var kept []string

	for _, element := range strings.Split(p, "/") {
		switch element {
		case "", ".":
		case "..":
			if len(kept) > 0 {
				kept = kept[:len(kept)-1]
			}
		default:
			kept = append(kept, element)
		}
	}

	return "/" + strings.Join(kept, "/")
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
