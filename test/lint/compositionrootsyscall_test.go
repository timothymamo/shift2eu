package lint_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// syscallSignal matches the one kind of name a composition root may take from
// package syscall: a signal constant, such as syscall.SIGTERM or
// syscall.SIGHUP, which is what cmd/shift2eu/main.go uses to cancel a run's
// context (issue #111).
var syscallSignal = regexp.MustCompile(`^SIG[A-Z0-9]+$`)

// syscallNameAllowed reports whether name, the selector of a reference to
// package syscall, is one a composition root may make: a signal constant, or
// the type syscall.Signal, which a signal value is and which starts no process.
// Every other name of package syscall is refused, the process entry points and
// the raw system calls (syscall.Syscall, syscall.AllThreadsSyscall, and the
// syscall.SYS_* numbers) as well as the DLLs (syscall.LazyDLL,
// syscall.NewCallback, syscall.PtracePokeText) and the variables that hold them
// (syscall.DLL, syscall.LazyProc).
//
// A list of refused names cannot say this: every release adds a name, and the
// ones the re-review and the re-audit named (AllThreadsSyscall,
// AllThreadsSyscall6, the LazyDLL and DLL values, NewCallback, the Ptrace calls)
// linted clean beside the named ones. So the rule is the allow list, and only a
// signal constant and the signal type pass.
func syscallNameAllowed(name string) bool {
	return syscallSignal.MatchString(name) || name == "Signal"
}

// syscallProblems returns what is wrong with the references to package syscall
// in src, the content of the Go file at rel, one line each: a reference that is
// no signal constant and no syscall.Signal, a dot import, which hides the
// refused names behind bare ones, a blank import, which runs the package's
// initialisation for nothing a composition root names (signal constants need a
// named import), and a file that imports the package twice, which reaches a
// refused name through a second one. The package is read through every name the
// file imports it as, so an aliased import is held to the rule as os is
// (lookupEnvProblems). A file that imports no package syscall passes, and a
// string literal or a comment that names a refused function is no reference.
//
// It reads the source, not the linted tree: a case is a string, so the pin is a
// fixture (TestSyscallProblems).
func syscallProblems(rel string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	names, positions := importedNames(f, "syscall")
	if len(names) == 0 {
		return nil, nil
	}

	var problems []string
	if len(names) > 1 {
		problems = append(problems, fmt.Sprintf("%s:%d: the file imports syscall %d times; a composition root imports it once, so every name it takes from the package is one reference", rel, fset.Position(positions[len(positions)-1]).Line, len(names)))
	}

	// named is every local name the file reaches package syscall by: a dot
	// import hides its names behind bare ones and a blank import names none.
	named := make([]string, 0, len(names))
	dotted := false
	for i, name := range names {
		switch name {
		case ".":
			dotted = true
			problems = append(problems, fmt.Sprintf("%s:%d: the file dot-imports syscall, which hides every name of the package behind a bare one; import it as syscall and name only SIGTERM or SIGHUP", rel, fset.Position(positions[i]).Line))
		case "_":
			problems = append(problems, fmt.Sprintf("%s:%d: the file blank-imports syscall, which runs its initialisation for no signal the composition root names; import it as syscall", rel, fset.Position(positions[i]).Line))
		default:
			named = append(named, name)
		}
	}
	if dotted {
		return problems, nil
	}

	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || syscallNameAllowed(sel.Sel.Name) {
			return true
		}
		pkg, ok := anyName(sel.X, named)
		if !ok {
			return true
		}

		problems = append(problems, fmt.Sprintf("%s:%d: the composition roots reference package syscall only through a signal constant (syscall.SIGTERM, syscall.SIGHUP) and the type syscall.Signal, and %s.%s is neither; a raw system call, a DLL or a process entry point is no composition root's",
			rel, fset.Position(sel.Pos()).Line, pkg, sel.Sel.Name))

		return true
	})

	return problems, nil
}

// TestCompositionRootsUseSyscallOnlyForSignals checks every Go file of the two
// composition roots, _test.go files included, for a reference to package
// syscall that is neither a signal constant nor the type syscall.Signal
// (syscallProblems). The tree holds two
// references today, syscall.SIGTERM and syscall.SIGHUP, and the test pins that:
// a composition root that made a raw system call, loaded a DLL or took a
// process entry point would run code the operator of a report did not ask for,
// and no forbidigo group can say so. forbidigo's composition-root-no-exec group
// names the calls the design knows of; this allow list is what holds when the
// next release of a platform adds a name.
func TestCompositionRootsUseSyscallOnlyForSignals(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	files := 0
	refused, err := walkGoFiles(root, skipDir, func(rel string, src []byte) error {
		if !slices.ContainsFunc(compositionRootDirs, func(dir string) bool {
			return strings.HasPrefix(rel, dir+"/")
		}) {
			return nil
		}
		files++
		problems, err := syscallProblems(rel, src)
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
		t.Fatalf("no Go file under %v", compositionRootDirs)
	}
}

// TestSyscallProblems pins syscallProblems, the seam of
// TestCompositionRootsUseSyscallOnlyForSignals, on fixtures: the tree names two
// signal constants and nothing else, so the fixtures are what prove the check
// would refuse the names the re-review and the re-audit found linting clean.
func TestSyscallProblems(t *testing.T) {
	t.Parallel()

	const rel = "cmd/shift2eu/probe.go"
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "the signal constants the roots name",
			src: `package main

import "syscall"

var (
	_ = syscall.SIGTERM
	_ = syscall.SIGHUP
)
`,
		},
		{
			name: "the signal type is harmless",
			src: `package main

import "syscall"

var _ = syscall.Signal(15)
`,
		},
		{
			name: "an aliased import is the same package",
			src: `package main

import s "syscall"

var _ = s.SIGTERM
var _ = s.AllThreadsSyscall
`,
			want: []string{
				rel + `:6: the composition roots reference package syscall only through a signal constant (syscall.SIGTERM, syscall.SIGHUP) and the type syscall.Signal, and s.AllThreadsSyscall is neither; a raw system call, a DLL or a process entry point is no composition root's`,
			},
		},
		{
			// The pin: a second entry point of the raw system call, which
			// no group of .golangci.yml names.
			name: "a raw system call on every thread",
			src: `package main

import "syscall"

func start() { _, _, _ = syscall.AllThreadsSyscall(0, 0, 0, 0) }
`,
			want: []string{
				rel + `:5: the composition roots reference package syscall only through a signal constant (syscall.SIGTERM, syscall.SIGHUP) and the type syscall.Signal, and syscall.AllThreadsSyscall is neither; a raw system call, a DLL or a process entry point is no composition root's`,
			},
		},
		{
			// The pin: a DLL built in place, which never names syscall.NewLazyDLL.
			name: "a DLL as a composite literal",
			src: `package main

import "syscall"

var _ = &syscall.LazyDLL{Name: "kernel32.dll", System: true}
`,
			want: []string{
				rel + `:5: the composition roots reference package syscall only through a signal constant (syscall.SIGTERM, syscall.SIGHUP) and the type syscall.Signal, and syscall.LazyDLL is neither; a raw system call, a DLL or a process entry point is no composition root's`,
			},
		},
		{
			// The pin: a variable of a DLL's type, which a later
			// FindProc loads a function through.
			name: "a DLL variable",
			src: `package main

import "syscall"

var d syscall.DLL
`,
			want: []string{
				rel + `:5: the composition roots reference package syscall only through a signal constant (syscall.SIGTERM, syscall.SIGHUP) and the type syscall.Signal, and syscall.DLL is neither; a raw system call, a DLL or a process entry point is no composition root's`,
			},
		},
		{
			// The pin: a callback, which hands Go code to a foreign caller.
			name: "a callback",
			src: `package main

import "syscall"

var _ = syscall.NewCallback(func(uintptr) uintptr { return 0 })
`,
			want: []string{
				rel + `:5: the composition roots reference package syscall only through a signal constant (syscall.SIGTERM, syscall.SIGHUP) and the type syscall.Signal, and syscall.NewCallback is neither; a raw system call, a DLL or a process entry point is no composition root's`,
			},
		},
		{
			// The pin: a tracer writes another process's memory.
			name: "a ptrace call",
			src: `package main

import "syscall"

func write(pid int) { _ = syscall.PtracePokeText(pid, 0, []byte{0}) }
`,
			want: []string{
				rel + `:5: the composition roots reference package syscall only through a signal constant (syscall.SIGTERM, syscall.SIGHUP) and the type syscall.Signal, and syscall.PtracePokeText is neither; a raw system call, a DLL or a process entry point is no composition root's`,
			},
		},
		{
			// The pin: a second name for the package is a second way to
			// reach a refused name, and staticcheck's ST1019 is a nolint
			// away.
			name: "the package imported twice under two names is refused",
			src: `package main

import (
	"syscall"
	sc "syscall"
)

var _ = sc.AllThreadsSyscall
`,
			want: []string{
				rel + `:5: the file imports syscall 2 times; a composition root imports it once, so every name it takes from the package is one reference`,
				rel + `:8: the composition roots reference package syscall only through a signal constant (syscall.SIGTERM, syscall.SIGHUP) and the type syscall.Signal, and sc.AllThreadsSyscall is neither; a raw system call, a DLL or a process entry point is no composition root's`,
			},
		},
		{
			name: "a dot import is refused",
			src: `package main

import . "syscall"

var _ = SIGTERM
`,
			want: []string{
				rel + `:3: the file dot-imports syscall, which hides every name of the package behind a bare one; import it as syscall and name only SIGTERM or SIGHUP`,
			},
		},
		{
			name: "a blank import is refused",
			src: `package main

import _ "syscall"
`,
			want: []string{
				rel + `:3: the file blank-imports syscall, which runs its initialisation for no signal the composition root names; import it as syscall`,
			},
		},
		{
			name: "a file that imports no syscall",
			src: `package main

import "os"

var _ = os.Getpagesize
`,
		},
		{
			// A package of the module imported as syscall is no package
			// syscall: only the standard package's path is read.
			name: "another package imported as syscall",
			src: `package main

import syscall "example.com/other"

var _ = syscall.Exit(0)
`,
		},
		{
			name: "a comment or a literal naming a refused function is no reference",
			src: `package main

// syscall.PtracePokeText writes another process's memory.

var marker = "syscall.PtracePokeText"
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := syscallProblems(rel, []byte(tt.src))
			if err != nil {
				t.Fatalf("syscallProblems(): %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("syscallProblems() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
