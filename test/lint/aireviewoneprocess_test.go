package lint_test

import (
	"slices"
	"strings"
	"testing"
)

// aiReviewerOneProcessTag is the tag of forbidigo's ai-reviewer-one-process group
// (PRD #161 milestone 4, ADR 0033): the calls the AI reviewer's library may not
// make. depguard's ai-reviewer-is-standard-library allows os and os/exec, which
// the library runs the one Claude Code CLI call with and writes its system prompt
// to a temporary file with. os also reads any file of the checkout, which holds
// the instructions the reviewer reads as data and must not be able to read as
// code, and a second process is a second program beside the one the reviewer
// verifies and runs.
//
// It refuses exec.Command, which starts a process with no context, so nothing can
// time it out or cancel it; the library runs its one process with
// exec.CommandContext. It refuses the os calls that read a file: os.ReadFile,
// os.Open, os.OpenFile and os.ReadDir. It keeps os.WriteFile, os.MkdirTemp,
// os.CreateTemp and os.RemoveAll, which write the prompt file and make and remove
// the library's own temporary directories, and every name that only extends a
// refused one (os.ReadFiles, exec.Commands).
//
// Scope: the library's non-test files, as the two linters.exclusions.rules rules
// of the group say and TestForbidigoExclusionsDropOneGroup checks. The library's
// own tests read its fixtures and run the go command the way a test does, which
// is no part of what the library does with the runner's files.
const aiReviewerOneProcessTag = "ai-reviewer-one-process"

// aiReviewerOneProcessRefused lists every name the group must refuse, as
// forbidigo's analyze-types matching renders it: a package, the type the call is
// made on, and the function. A name is refused when an entry of the group matches
// its pkg and its rendered name; the wording of a msg is not pinned here, the tag
// is.
var aiReviewerOneProcessRefused = []rootName{
	{"os/exec", "exec.Command"},
	{"os", "os.ReadFile"},
	{"os", "os.Open"},
	{"os", "os.OpenFile"},
	{"os", "os.ReadDir"},
}

// aiReviewerOneProcessAllowed are names no ai-reviewer-one-process entry may
// refuse: the one process the library runs with a context, what it writes its
// system prompt to a temporary file with, what it removes the directories it made
// with, the reads a reader of its output makes (os.Stdout, os.Stderr), the names
// that only extend a refused one (os.ReadFiles, exec.Commands) and catch a pattern
// that lost its $, and a package named os or os/exec elsewhere.
var aiReviewerOneProcessAllowed = []rootName{
	{"os/exec", "exec.CommandContext"},
	{"os/exec", "exec.Commands"},
	{"os/exec", "exec.ExitError"},
	{"os", "os.WriteFile"},
	{"os", "os.MkdirTemp"},
	{"os", "os.CreateTemp"},
	{"os", "os.RemoveAll"},
	{"os", "os.Remove"},
	{"os", "os.Stat"},
	{"os", "os.Exit"},
	{"os", "os.ReadFiles"},
	{"os", "os.Opens"},
	{"os", "os.OpenFiles"},
	{"os", "os.ReadDirs"},
	{"example.com/otheros", "os.ReadFile"},
	{"example.com/otheros/exec", "exec.Command"},
}

// TestAIReviewerOneProcessProbe runs forbidigo with the repository's .golangci.yml
// on a probe module laid out like the repository (testdata/aireviewoneprocess/mod),
// whose ci/aireview/internal/review holds the calls the group must refuse and the
// ones it must keep allowing, whose _test.go file there holds the calls the
// group's scope leaves to a test, and one package outside the library holds a
// file read that the group must not reach. Every refused line ends in a want
// comment naming the group, and every entry of the group must refuse a line, so a
// group that stops firing, or a group whose scope widens, fails here.
//
// make lint runs this probe with SHIFT2EU_LINT_PROBES=require on the host, and
// every entry of CI's lint-target matrix runs the step that sets it.
func TestAIReviewerOneProcessProbe(t *testing.T) {
	t.Parallel()

	runForbidigoProbe(t, forbidigoProbe{
		dir:  "aireviewoneprocess",
		tags: []string{aiReviewerOneProcessTag},
	})
}

// TestAIReviewerOneProcessForbidigoPatterns checks the entries of the
// ai-reviewer-one-process group, read from .golangci.yml, offline and without
// golangci-lint: every name the library may not use is refused by an entry of the
// group, no name it may use is refused by one, and every entry refuses a listed
// name. The probe proves how forbidigo renders a name and where the group holds;
// this pins the patterns and their packages over more names than a probe can hold,
// and it is the pin a reviewer reads when the group changes.
func TestAIReviewerOneProcessForbidigoPatterns(t *testing.T) {
	t.Parallel()

	entries := groupEntries(t, moduleRoot(t), aiReviewerOneProcessTag)
	refuses := func(e forbidEntry, n rootName) bool {
		return e.pkg.MatchString(n.pkg) && e.pattern.MatchString(n.name)
	}

	for _, r := range aiReviewerOneProcessRefused {
		var msgs []string

		for _, e := range entries {
			if refuses(e, r) {
				msgs = append(msgs, e.msg)
			}
		}

		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.HasPrefix(m, aiReviewerOneProcessTag+": ") }) {
			t.Errorf("%s (pkg %q) must be refused by an entry of group %s; refused by %q", r.name, r.pkg, aiReviewerOneProcessTag, msgs)
		}
	}

	for _, n := range aiReviewerOneProcessAllowed {
		for _, e := range entries {
			if refuses(e, n) {
				t.Errorf("%s (pkg %q) must pass the AI reviewer's library, but the entry with msg %q refuses it", n.name, n.pkg, e.msg)
			}
		}
	}

	for _, e := range entries {
		if !slices.ContainsFunc(aiReviewerOneProcessRefused, func(r rootName) bool { return refuses(e, r) }) {
			t.Errorf("the entry with msg %q refuses no name of the group's list; add the names it refuses", e.msg)
		}
	}
}
