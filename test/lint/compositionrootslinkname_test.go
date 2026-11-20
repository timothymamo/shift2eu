package lint_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// linknameDirective matches a //go:linkname compiler directive, which binds a
// symbol of another package to a name of this one without an import of it. It
// is a line comment at the start of a line, as the go command reads it.
var linknameDirective = regexp.MustCompile(`^//go:linkname(?:\s|$)`)

// linknameProblems returns what is wrong with the compiler directives of src,
// the content of the Go file at rel, one line each: a //go:linkname directive,
// which neither a composition root nor the driving adapter it hands the work to
// holds. The directive is how a file reaches a function of a package it does not
// import, package unsafe's //go:linkname included, so no depguard rule and no
// forbidigo entry can see the call it makes possible (runtime_envs is the
// environment, credentials included; reflect's runtime hooks are the process). It
// needs a blank import of package unsafe beside it, which a test file may make
// today, and UnsafePointer reached through //go:linkname is still a pointer: the
// check is the directive itself. A comment that only mentions the directive in
// prose is no directive, and a //go:build or //go:generate line is no linkname.
//
// It reads the source, not the linted tree: a case is a string, so the pin is a
// fixture (TestLinknameProblems).
func linknameProblems(rel string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	var problems []string

	for _, group := range f.Comments {
		for _, c := range group.List {
			if !linknameDirective.MatchString(c.Text) {
				continue
			}

			problems = append(problems, fmt.Sprintf("%s:%d: the file holds a //go:linkname directive, which binds a symbol of another package to a name of this one without an import of it; a composition root, and the driving adapter it hands the work to, link no foreign symbol, package unsafe's included, and the call it makes possible is the one no import rule sees",
				rel, fset.Position(c.Slash).Line))
		}
	}

	return problems, nil
}

// TestRootsAndCLILinkNoForeignSymbol checks every Go file of the composition
// root and of the packages it hands the work to (environmentPathDirs),
// _test.go files included, for a //go:linkname directive
// (linknameProblems). The tree holds none today, and the test pins that: the
// directive reaches any function of any package, the environment dump of runtime
// and the process entry points of syscall among them, and it needs package
// unsafe, which depguard's rules deny the root and the CLI; the check holds
// the line where an import rule cannot, in a test file under `go test`.
func TestRootsAndCLILinkNoForeignSymbol(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	files := 0
	refused, err := walkGoFiles(root, skipDir, func(rel string, src []byte) error {
		if !slices.ContainsFunc(environmentPathDirs, func(dir string) bool {
			return strings.HasPrefix(rel, dir+"/")
		}) {
			return nil
		}
		files++
		problems, err := linknameProblems(rel, src)
		if err != nil {
			return err
		}
		for _, p := range problems {
			t.Error(p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	for _, p := range refused {
		t.Error(p)
	}
	if files == 0 {
		t.Fatalf("no Go file under %v", environmentPathDirs)
	}
}

// TestLinknameProblems pins linknameProblems, the seam of
// TestRootsAndCLILinkNoForeignSymbol, on fixtures: the tree holds no
// directive, so the fixture is what proves the check would refuse one.
func TestLinknameProblems(t *testing.T) {
	t.Parallel()

	const rel = "cmd/shift2eu/probe_test.go"
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			// The fixture the auditor asked for: package unsafe imported for
			// the directive, and the environment bound to a local name.
			name: "the environment of the process, linked",
			src: `package main

import _ "unsafe"

//go:linkname environ syscall.runtime_envs
func environ() []string
`,
			want: []string{
				rel + `:5: the file holds a //go:linkname directive, which binds a symbol of another package to a name of this one without an import of it; a composition root, and the driving adapter it hands the work to, link no foreign symbol, package unsafe's included, and the call it makes possible is the one no import rule sees`,
			},
		},
		{
			name: "a directive of a _test.go file is held to it too",
			src: `package main

import (
	"testing"
	_ "unsafe"
)

//go:linkname start syscall.Exec
func start()

func TestProbe(t *testing.T) { start() }
`,
			want: []string{
				rel + `:8: the file holds a //go:linkname directive, which binds a symbol of another package to a name of this one without an import of it; a composition root, and the driving adapter it hands the work to, link no foreign symbol, package unsafe's included, and the call it makes possible is the one no import rule sees`,
			},
		},
		{
			name: "a prose mention is no directive",
			src: `package main

// The //go:linkname directive binds a foreign symbol; this comment only names it.

var _ = 1
`,
		},
		{
			name: "the build and generate directives are no linkname",
			src: `package main

//go:build !windows
//go:generate go run ./gen

var _ = 1
`,
		},
		{
			name: "a file with no directive",
			src:  "package main\n\nfunc main() {}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := linknameProblems(rel, []byte(tt.src))
			if err != nil {
				t.Fatalf("linknameProblems(): %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("linknameProblems() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
