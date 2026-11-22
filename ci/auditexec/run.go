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
// --open-files-in-pager is git grep's, which runs a program of the caller's.
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
}

// gitOwnOptions are git options of their own that are a prefix of a refused
// one, and so no abbreviation of it.
var gitOwnOptions = map[string]bool{"--text": true}

// gitRefusedShort are the short git options refused anywhere on the line,
// alone or with their value attached: -c sets a configuration value, -O is git
// grep's --open-files-in-pager.
var gitRefusedShort = []string{"-c", "-O"}

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

	switch name {
	case "git":
		for _, arg := range rest {
			if refusedGitOption(arg) {
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
		for _, arg := range rest {
			if refusedSortOption(arg) {
				return nil, arg, false
			}
		}
	}

	return append([]string{path}, rest...), "", true
}

// refusedGitOption reports whether arg is one of git's refused options.
func refusedGitOption(arg string) bool {
	for _, short := range gitRefusedShort {
		if strings.HasPrefix(arg, short) && !strings.HasPrefix(arg, "--") {
			return true
		}
	}

	if !strings.HasPrefix(arg, "--") {
		return false
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

// refusedSortOption reports whether arg makes sort write a file: -o alone, with
// its file attached or in a cluster of short options, and --output in any
// form, abbreviated included (sort has no other long option starting --o).
func refusedSortOption(arg string) bool {
	if strings.HasPrefix(arg, "--o") {
		return true
	}

	return strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "o")
}

// quote returns word cut to maxWord bytes and quoted as a Go string, so the
// stderr line stays one line whatever the word holds.
func quote(word string) string {
	if len(word) > maxWord {
		word = word[:maxWord]
	}

	return fmt.Sprintf("%q", word)
}
