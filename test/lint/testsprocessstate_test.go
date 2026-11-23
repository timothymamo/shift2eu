package lint_test

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"
	"testing"
)

// testsNoProcessStateTag is the tag of forbidigo's tests-no-process-state group
// (issue #153): Setenv and Chdir, which no test of this repository and no helper
// of internal/testutil may call. t.Setenv changes the environment of the whole
// test binary — every test in the process then sees the variable, a credential's
// name among them (CLAUDE.md rule 3) — and t.Chdir changes the directory the
// process runs in, which a t.Parallel test shares. A test that needs a value
// passes the case it wants to the code under test, through a fake or a parameter,
// and the CLI takes its inputs from flags.
//
// The group holds for every test of the module and for every other file a test
// could hand the change to, and is dropped in three places, each with its own
// linters.exclusions.rules entry, and once for one call in a fourth (below): test/lint and test/architecture set the go
// command's own variables (GOCACHE, GOFLAGS, HOME) on purpose, in a test binary of
// their own, to run the go command in a known environment, and the AI reviewer's
// claudecli_test.go (PRD #161 milestone 4, ADR 0033) sets ANTHROPIC_API_KEY and a
// variable of its own in the process's environment in the one test that proves
// the Claude Code CLI inherits none of it, with no parallel test beside it. That
// exception is that one file: every other file of the library, its tests
// included, is held to the group. The fourth exclusion is no file's blanket drop
// but one call: ci/auditexec/main.go passes os.Chdir to run (issue #277, round G,
// ADR 0040's amendment), which changes into the audit's fixed work directory
// before it execs a line, so processStateCallException allows that one expression
// in that one file and nothing else, there or anywhere else. Before the audit's
// finding S1 the group was scoped to five areas, so an adapter's or the domain's
// test, and every e2e test, could set a variable with lint green: the likely case
// is a source adapter's test that sets AWS_PROFILE.
//
// Two entries refuse the calls, as the group writes them in .golangci.yml. The
// first matches any selector named Setenv or Chdir, whatever it is called on and
// whichever package it is reached through: a test's own method on *testing.T,
// *testing.B, *testing.F or the testing.TB interface, a struct that embeds a
// *testing.T (whose promoted method forbidigo renders on the embedding type,
// wrap.Setenv, not testing.T.Setenv) and os.Setenv and os.Chdir one package
// further out. The second names the os and syscall calls that write the
// environment or the working directory without the word Setenv or Chdir: the
// os.Unsetenv, os.Clearenv and syscall.Unsetenv and syscall.Clearenv writes, and
// syscall.Fchdir, which changes the directory of the process from an open
// descriptor. The names that only extend a refused one (Setenvs, Chdirs,
// Unsetenvs, Clearenvs, Fchdirs) stay allowed.
//
// forbidigo cannot see every one of those calls: a call through an interface
// literal (var s interface{ Setenv(string, string) } = t) or through a type
// parameter (func f[T interface{ Setenv(string, string) }]) is resolved on no
// named type, so analyze-types renders nothing for it. processStateSelectorFindings
// is the source check that refuses those, on the AST, and
// TestTestsChangeNoProcessStateByNoSelector runs it over the same scope the group
// has. reflect's MethodByName remains beyond both, and docs/architecture.md
// records it under Known gaps.
//
// t.TempDir is deliberately allowed, and so are os.MkdirTemp("", ...) and
// os.CreateTemp("", ...) (TestTestsNoProcessStateProbe holds all three): they read
// $TMPDIR implicitly through os.TempDir and return a fresh temporary path, never a
// home, config or credential path, and t.TempDir itself goes through the same
// read, so refusing them would leave no way to get a temporary directory.
// os.TempDir named directly stays refused where testutil-no-env refuses it today.
const testsNoProcessStateTag = "tests-no-process-state"

// testsNoProcessStateRefused lists the rendered names the group must refuse, as
// forbidigo's analyze-types matching renders them: a package, the type the method
// is declared on or the call is made on, and the method. A name is refused when an
// entry of the group matches its pkg and its rendered name; the wording of a msg
// is not pinned here, the tag is.
var testsNoProcessStateRefused = []rootName{
	{"testing", "testing.T.Setenv"},
	{"testing", "testing.T.Chdir"},
	{"testing", "testing.B.Setenv"},
	{"testing", "testing.B.Chdir"},
	{"testing", "testing.F.Setenv"},
	{"testing", "testing.F.Chdir"},
	{"testing", "testing.TB.Setenv"},
	{"testing", "testing.TB.Chdir"},
	// A local interface with the method declared on it is a named type, so
	// forbidigo renders the call on that type.
	{"github.com/timothymamo/shift2eu/internal/app/analysis_test", "analysis_test.setenver.Setenv"},
	// A struct that embeds a *testing.T promotes Setenv, and forbidigo renders
	// the call on the embedding type: the method is testing.T's, the receiver
	// the source names is the wrapper's. The audit's finding L-1.
	{"github.com/timothymamo/shift2eu/internal/app/analysis_test", "analysis_test.wrapper.Setenv"},
	{"github.com/timothymamo/shift2eu/internal/app/analysis_test", "analysis_test.wrapper.Chdir"},
	// The calls of os, one package further out than a test's own methods, which
	// change the same process state (the review's finding S1).
	{"os", "os.Setenv"},
	{"os", "os.Unsetenv"},
	{"os", "os.Clearenv"},
	{"os", "os.Chdir"},
	// The same writes of syscall, which names neither Setenv nor Chdir, and
	// syscall.Fchdir, which changes the working directory from a descriptor
	// (the round-2 review's finding S1).
	{"syscall", "syscall.Unsetenv"},
	{"syscall", "syscall.Clearenv"},
	{"syscall", "syscall.Fchdir"},
}

// testsNoProcessStateAllowed are names no tests-no-process-state entry may
// refuse: the temporary directory a test takes, which reads $TMPDIR the way
// os.MkdirTemp does and is the one way a test gets a directory, the rest of what
// testing hands a test, the names that only extend a refused one
// (testing.T.Setenvs, testing.T.Chdirs, testing.TB.Setenvs, testing.TB.Chdirs,
// analysis_test.wrapper.Setenvs, os.Unsetenvs, os.Clearenvs, syscall.Unsetenvs,
// syscall.Clearenvs, syscall.Fchdirs) and catch a pattern that lost its $, and a
// call of a package that only has a name in common with os or syscall.
var testsNoProcessStateAllowed = []rootName{
	{"testing", "testing.T.TempDir"},
	{"testing", "testing.B.TempDir"},
	{"testing", "testing.F.TempDir"},
	{"testing", "testing.TB.TempDir"},
	{"testing", "testing.T.Helper"},
	{"testing", "testing.T.Parallel"},
	{"testing", "testing.T.Run"},
	{"testing", "testing.T.Log"},
	{"testing", "testing.T.Logf"},
	{"testing", "testing.T.Cleanup"},
	{"testing", "testing.T.Deadline"},
	{"testing", "testing.T.Context"},
	{"testing", "testing.T.Setenvs"},
	{"testing", "testing.T.Chdirs"},
	{"testing", "testing.TB.Setenvs"},
	{"testing", "testing.TB.Chdirs"},
	{"github.com/timothymamo/shift2eu/internal/app/analysis_test", "analysis_test.wrapper.Setenvs"},
	{"os", "os.Getenv"},
	{"os", "os.Environ"},
	{"os", "os.Setenvs"},
	{"os", "os.Chdirs"},
	{"os", "os.Unsetenvs"},
	{"os", "os.Clearenvs"},
	{"syscall", "syscall.Mkfifo"},
	{"syscall", "syscall.Unsetenvs"},
	{"syscall", "syscall.Clearenvs"},
	{"syscall", "syscall.Fchdirs"},
	{"example.com/othersyscall", "syscall.Unsetenv"},
}

// TestTestsNoProcessStateProbe runs forbidigo with the repository's .golangci.yml
// on a probe module laid out like the repository (testdata/testsprocessstate/mod),
// whose _test.go files hold the lines the group must refuse on each receiver, and
// the lines it must keep allowing.
//
// internal/app/analysis/probe_test.go holds Setenv and Chdir on *testing.T,
// *testing.B, *testing.F and through testing.TB, the method value, the testing
// methods that must pass, a struct that embeds a *testing.T (whose promoted method
// forbidigo renders on the wrapper), and — with no want, because forbidigo cannot
// see them — a call through an interface literal and one through a type
// parameter, which the source check refuses instead. internal/testutil holds the
// same on a *testing.T and a testing.TB and temp.go holds os.MkdirTemp("", ...)
// and os.CreateTemp("", ...); the root's and the driving adapter's _test.go files
// (cmd/shift2eu, internal/cli) hold the two calls on a *testing.T, because the
// limits the root's tests are held to are the ones its driving adapter's tests
// are held to as well, and so are internal/adapter/source/aws's and internal/domain/taxonomy's and test/e2e's,
// which the group reaches since the audit's finding S2: its scope is the module
// except test/lint and test/architecture.
//
// Every line that ends in a want comment must get an issue of the group, and no
// issue may land on a line without one, so a group that stops firing, or one that
// reaches t.TempDir, os.MkdirTemp or os.CreateTemp, fails here.
//
// make lint runs this probe with SHIFT2EU_LINT_PROBES=require on the host, and
// every entry of CI's lint-target matrix runs the step that sets it.
func TestTestsNoProcessStateProbe(t *testing.T) {
	t.Parallel()

	runForbidigoProbe(t, forbidigoProbe{
		dir:  "testsprocessstate",
		tags: []string{testsNoProcessStateTag},
	})
}

// TestTestsNoProcessStateForbidigoPatterns checks the entries of the
// tests-no-process-state group, read from .golangci.yml, offline and without
// golangci-lint: every name a test may not use is refused by an entry of the
// group, no name it may use is refused by one, and every entry refuses a listed
// name. The probe proves how forbidigo renders a name; this pins the patterns and
// their packages over more names than a probe can hold, and it is the pin a
// reviewer reads when the group changes.
func TestTestsNoProcessStateForbidigoPatterns(t *testing.T) {
	t.Parallel()

	entries := groupEntries(t, moduleRoot(t), testsNoProcessStateTag)
	refuses := func(e forbidEntry, n rootName) bool {
		return e.pkg.MatchString(n.pkg) && e.pattern.MatchString(n.name)
	}

	for _, r := range testsNoProcessStateRefused {
		var msgs []string

		for _, e := range entries {
			if refuses(e, r) {
				msgs = append(msgs, e.msg)
			}
		}

		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.HasPrefix(m, testsNoProcessStateTag+": ") }) {
			t.Errorf("%s (pkg %q) must be refused by an entry of group %s; refused by %q", r.name, r.pkg, testsNoProcessStateTag, msgs)
		}
	}

	for _, n := range testsNoProcessStateAllowed {
		for _, e := range entries {
			if refuses(e, n) {
				t.Errorf("%s (pkg %q) must pass a test, but the entry with msg %q refuses it", n.name, n.pkg, e.msg)
			}
		}
	}

	for _, e := range entries {
		if !slices.ContainsFunc(testsNoProcessStateRefused, func(r rootName) bool { return refuses(e, r) }) {
			t.Errorf("the entry with msg %q refuses no name of the group's list; add the names it refuses", e.msg)
		}
	}
}

// msgProcessStateSelector is the message of processStateSelectorFindings, which a
// reader of a failing test sees.
const msgProcessStateSelector = astPrefix + "a call named Setenv, Unsetenv, Clearenv, Chdir or Fchdir changes the environment or the working directory of the whole test binary"

// processStateSelectorNames are the selector names the source check refuses: the
// names the two entries of tests-no-process-state refuse, so the AST check and
// the group agree on what a test may not call.
var processStateSelectorNames = []string{"Setenv", "Unsetenv", "Clearenv", "Chdir", "Fchdir"}

// processStateSelectorFindings returns a finding for every selector named Setenv,
// Unsetenv, Clearenv, Chdir or Fchdir in the Go files of a module or a probe,
// wherever the call is
// made and whatever it is made on. forbidigo's analyze-types matching refuses a
// call on a named type — testing.T.Setenv, a struct that embeds a *testing.T, os
// .Setenv — but it renders nothing for a call through an interface literal
// (var s interface{ Setenv(k, v string) } = t; s.Setenv(...)) or through a type
// parameter (func f[T interface{ Setenv(k, v string) }]), because the type it
// resolves the method on is no named type. Both are a change of the whole test
// binary's state, so the source check refuses the name itself, and the group's
// scope, not the entry's matching, decides where it holds.
//
// It reads the source, not the linted tree: a case is a string, so the pin is a
// fixture (TestProcessStateSelectorFindings).
//
// One call is no finding: the expression processStateCallException names, in the
// file it names, the audit wrapper's os.Chdir, which .golangci.yml's exclusion
// rule drops there too, and only the first of those in that file: a second one is
// a finding (issue #277, round H, finding 4).
func processStateSelectorFindings(t *testing.T, files []goFile) []finding {
	t.Helper()

	var found []finding
	for _, f := range files {
		fset, file := parseGoFile(t, f)
		var excepted int
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || !slices.Contains(processStateSelectorNames, sel.Sel.Name) {
				return true
			}
			expr := exprText(sel)
			if processStateCallException(f.rel, expr, excepted) {
				excepted++
				return true
			}
			found = append(found, finding{
				pos: position(fset, f, sel),
				msg: msgProcessStateSelector,
				detail: fmt.Sprintf("%s is a call that changes the state of the whole test binary: pass the value to the code under test through a fake or a parameter, and take a directory from t.TempDir. forbidigo reads no type through an interface literal or a type parameter, so no pattern refuses this call",
					expr),
			})
			return true
		})
	}
	return found
}

// aiReviewerEnvTestFile is the one file of the AI reviewer's library the group
// tests-no-process-state is dropped in (PRD #161 milestone 4, ADR 0033):
// claudecli_test.go's env test sets ANTHROPIC_API_KEY and a variable of its own
// in the process's environment, to prove that the Claude Code CLI inherits
// neither, and runs no parallel test beside it. The library's other files change
// no process state, so they are held to the group.
const aiReviewerEnvTestFile = "ci/aireview/internal/review/claudecli_test.go"

// processStateScope reports whether the group tests-no-process-state holds for the
// file at rel, a slash-separated path from the module root: it holds everywhere
// but in the two harnesses under test/, which set the go command's own variables
// on purpose, and in the AI reviewer's claudecli_test.go, whose env test sets an
// ANTHROPIC_API_KEY in the process's environment to prove that the Claude Code CLI
// inherits none of it, as the group's linters.exclusions.rules entries say.
func processStateScope(rel string) bool {
	return !strings.HasPrefix(rel, "test/lint/") &&
		!strings.HasPrefix(rel, "test/architecture/") &&
		rel != aiReviewerEnvTestFile
}

// auditExecChdirFile, auditExecChdirExpr and auditExecChdirCalls are the group's
// one call-level exception (issue #277, round G, ADR 0040's amendment).
// ci/auditexec/main.go passes os.Chdir to run, which changes into the audit's
// fixed work directory before it execs a line, so the wrapper's work happens
// there whatever directory the caller was in. The group's Chdir entry refuses
// every call named Chdir, so the source check holds the exclusion rule's drop to
// this one expression: the rest of main.go's process-state calls and every other
// file's stay refused. It is one call, not the file's leave to change its
// directory (issue #277, round H, finding 4): the exec needs a single change into
// the work directory, so the file holds at most auditExecChdirCalls of them, and a
// second one is a finding.
const (
	auditExecChdirFile  = "ci/auditexec/main.go"
	auditExecChdirExpr  = "os.Chdir"
	auditExecChdirCalls = 1
)

// processStateCallException reports whether the group tests-no-process-state is
// dropped for the call expr, as exprText renders it, in the file at rel, a
// slash-separated path from the module root: true for the first auditExecChdirCalls
// os.Chdir calls of ci/auditexec/main.go, and false for every further one there
// and for every other expression anywhere else. previouslyExcepted is the number
// of expressions of that file the check already let pass, which the caller counts
// in source order, so the exception holds for one call however the file is
// written. It compares the rendered expression, so a file that reaches the same
// call under a name of its own is no exception.
func processStateCallException(rel, expr string, previouslyExcepted int) bool {
	return rel == auditExecChdirFile && expr == auditExecChdirExpr && previouslyExcepted < auditExecChdirCalls
}

// TestTestsChangeNoProcessStateByNoSelector fails on every selector that
// processStateSelectorNames holds in the module, in the scope the group has,
// which processStateSelectorFindings returns: the calls forbidigo cannot see, a
// call through an interface literal and a call through a type parameter, are
// refused here, and so is every call it does see, so the two checks agree on
// what a test may not do. The one call the group's exclusion rule drops,
// processStateCallException's os.Chdir in the audit wrapper's main, is no
// finding; every other process-state call of that file is one.
func TestTestsChangeNoProcessStateByNoSelector(t *testing.T) {
	t.Parallel()

	var files []goFile
	for _, f := range moduleGoFiles(t, moduleRoot(t)) {
		if processStateScope(f.rel) {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		t.Fatal("no Go file in the group's scope, so this test would check nothing")
	}

	for _, f := range processStateSelectorFindings(t, files) {
		t.Errorf("%s: %s: %s", f.pos, f.msg, f.detail)
	}
}

// TestProcessStateSelectorFindings pins processStateSelectorFindings, the seam of
// TestTestsChangeNoProcessStateByNoSelector: a call through an interface literal
// and one through a type parameter — which forbidigo renders nothing for — a
// promoted method of an embedded *testing.T (which it renders on the wrapper) and
// the calls of os and syscall are refused, while another testing method, a name
// that only extends a refused one and a declaration are no finding. The group's
// one call-level exception is pinned too: os.Chdir in the audit wrapper's main is
// no finding, and the same call one file over, another process-state call in
// main, and os.Chdir under a name of its own are. The col is dropped from the
// position: the line and the message are the contract.
func TestProcessStateSelectorFindings(t *testing.T) {
	t.Parallel()

	const rel = "internal/app/analysis/analysis_test.go"

	tests := []struct {
		name string
		rel  string // the file the source stands in; rel above when empty
		src  string
		want []string
	}{
		{
			name: "a call on a *testing.T",
			src: `package analysis_test

import "testing"

func TestA(t *testing.T) {
	t.Setenv("SHIFT2EU_TOKEN", "example")
}
`,
			want: []string{rel + ":6: " + msgProcessStateSelector},
		},
		{
			name: "a method value",
			src: `package analysis_test

import "testing"

func TestA(t *testing.T) {
	_ = t.Chdir
}
`,
			want: []string{rel + ":6: " + msgProcessStateSelector},
		},
		{
			// The audit's finding L-1: the type the method is resolved on is
			// the interface literal, and forbidigo renders nothing for it.
			name: "a call through an interface literal",
			src: `package analysis_test

import "testing"

func TestA(t *testing.T) {
	var s interface{ Setenv(k, v string) } = t
	s.Setenv("SHIFT2EU_TOKEN", "example")
}
`,
			want: []string{rel + ":7: " + msgProcessStateSelector},
		},
		{
			// The same finding: a type parameter is no named type either.
			name: "a call through a type parameter",
			src: `package analysis_test

type setenver interface{ Setenv(k, v string) }

func viaConstraint[T setenver](t T) {
	t.Setenv("SHIFT2EU_TOKEN", "example")
}
`,
			want: []string{rel + ":6: " + msgProcessStateSelector},
		},
		{
			// forbidigo renders this one on the wrapper (analysis_test.wrapper.Setenv),
			// which the receiver-agnostic entry catches.
			name: "a promoted method of an embedded struct",
			src: `package analysis_test

import "testing"

type wrapper struct{ *testing.T }

func TestA(t *testing.T) {
	w := wrapper{t}
	w.Setenv("SHIFT2EU_TOKEN", "example")
}
`,
			want: []string{rel + ":9: " + msgProcessStateSelector},
		},
		{
			// The review's finding S1: the same process state, one package
			// further out. The source check reads every name the group's two
			// entries refuse, so os.Unsetenv and os.Clearenv are findings too.
			name: "the calls of os",
			src: `package analysis_test

import "os"

func set() {
	_ = os.Setenv("SHIFT2EU_TOKEN", "example")
	_ = os.Unsetenv("SHIFT2EU_TOKEN")
	os.Clearenv()
	_ = os.Chdir("example")
}
`,
			want: []string{
				rel + ":6: " + msgProcessStateSelector,
				rel + ":7: " + msgProcessStateSelector,
				rel + ":8: " + msgProcessStateSelector,
				rel + ":9: " + msgProcessStateSelector,
			},
		},
		{
			// The round-2 review's finding S1: syscall writes the environment
			// and changes the working directory under names of its own.
			name: "the calls of syscall",
			src: `package analysis_test

import "syscall"

func set() {
	_ = syscall.Unsetenv("SHIFT2EU_TOKEN")
	syscall.Clearenv()
	_ = syscall.Fchdir(3)
}
`,
			want: []string{
				rel + ":6: " + msgProcessStateSelector,
				rel + ":7: " + msgProcessStateSelector,
				rel + ":8: " + msgProcessStateSelector,
			},
		},
		{
			// The group's one call-level exception (issue #277, round G): the
			// audit wrapper's main hands run the call that changes into the
			// audit's work directory before the exec.
			name: "os.Chdir in the audit wrapper's main is the exception",
			rel:  auditExecChdirFile,
			src: `package main

import "os"

func main() {
	_ = os.Chdir
}
`,
		},
		{
			// The exception is one call, not the whole file (issue #277, round
			// H, finding 4): the first os.Chdir of the audit wrapper's main is
			// the one the exec needs, and a second one is a finding, so the file
			// cannot grow another change of the binary's directory beside it.
			name: "a second os.Chdir in the audit wrapper's main is refused",
			rel:  auditExecChdirFile,
			src: `package main

import "os"

func main() {
	_ = os.Chdir
	_ = os.Chdir
}
`,
			want: []string{auditExecChdirFile + ":7: " + msgProcessStateSelector},
		},
		{
			// The same call one file over is no exception: the wrapper's logic
			// takes the change as a parameter, so a test passes a fake.
			name: "os.Chdir in the audit wrapper's logic is refused",
			rel:  "ci/auditexec/run.go",
			src: `package main

import "os"

func change(dir string) error {
	return os.Chdir(dir)
}
`,
			want: []string{"ci/auditexec/run.go:6: " + msgProcessStateSelector},
		},
		{
			// The exception is the expression, not the file's right to change
			// process state: another call of os in main is a finding.
			name: "another call of os in the audit wrapper's main is refused",
			rel:  auditExecChdirFile,
			src: `package main

import "os"

func main() {
	_ = os.Setenv("SHIFT2EU_TOKEN", "example")
}
`,
			want: []string{auditExecChdirFile + ":6: " + msgProcessStateSelector},
		},
		{
			// The exception names the expression os.Chdir, so a file that
			// reaches the same call under a name of its own is no exception.
			name: "os.Chdir under an alias is no exception",
			rel:  auditExecChdirFile,
			src: `package main

import o "os"

func main() {
	_ = o.Chdir
}
`,
			want: []string{auditExecChdirFile + ":6: " + msgProcessStateSelector},
		},
		{
			name: "a name that only extends one is no call",
			src: `package analysis_test

type wrapper struct{}

func (wrapper) Setenvs(k, v string) {}
func (wrapper) Chdirs(dir string)    {}
func (wrapper) Unsetenvs(k string)   {}
func (wrapper) Clearenvs()           {}
func (wrapper) Fchdirs(fd int)       {}

func set(w wrapper) {
	w.Setenvs("SHIFT2EU_TOKEN", "example")
	w.Chdirs("example")
	w.Unsetenvs("SHIFT2EU_TOKEN")
	w.Clearenvs()
	w.Fchdirs(3)
}
`,
		},
		{
			name: "a declaration is no selector",
			src: `package analysis_test

type wrapper struct{}

func (wrapper) Setenv(k, v string) {}

type setenver interface{ Setenv(k, v string) }
`,
		},
		{
			name: "another method of testing is no call",
			src: `package analysis_test

import "testing"

func TestA(t *testing.T) {
	t.TempDir()
	t.Parallel()
}
`,
		},
		{
			name: "a comment that names the call is no call",
			src: `package analysis_test

// t.Setenv would change every test's environment.
var _ = 1
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := tt.rel
			if file == "" {
				file = rel
			}

			var got []string
			for _, f := range processStateSelectorFindings(t, []goFile{{rel: file, src: []byte(tt.src)}}) {
				got = append(got, f.pos[:strings.LastIndex(f.pos, ":")]+": "+f.msg)
			}
			if !slices.Equal(tt.want, got) {
				t.Errorf("processStateSelectorFindings() = %v, want %v", got, tt.want)
			}
		})
	}
}
