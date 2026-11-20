package lint_test

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestSourcesHideNothingFromDepguard fails on every place, in any Go file
// golangci-lint lints, that would hide code from depguard, or a call from
// forbidigo (see the package documentation). depguard enforces the dependency
// rule and forbidigo the cloud-read-only rule; a violation is fixed in the
// design or the call, never hidden (CLAUDE.md).
func TestSourcesHideNothingFromDepguard(t *testing.T) {
	t.Parallel()

	// go test runs a test in its package's directory, test/lint.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root: %v", err)
	}

	problems, files, err := scanSources(root)
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if files == 0 {
		t.Fatalf("no Go files found under %s", root)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// whyNotRegular is the problem with a Go file that is not a regular file.
const whyNotRegular = "is not a regular file: golangci-lint lints a symbolic link under the link's path, " +
	"with the nolint directives of its target, which can lie where test/lint does not look; make it a regular file"

// scanSources reads every Go file the go command's ./... pattern reaches from
// root, whatever its build constraints, so every file any golangci-lint run of
// make lint lints (natively or for a release target, with every build tag
// set), and returns what in them would hide code from depguard, one line each,
// and the number of Go files it read. walkGoFiles refuses a Go file that is not
// a regular file itself; checkSource does every other check. The lines are in
// walk order, the lexical order of path elements: a refused file's line sits
// among the other files' lines where the walk met it, and the lines of one
// file keep checkSource's order.
func scanSources(root string) ([]string, int, error) {
	type problem struct{ rel, line string }
	var problems []problem
	files := 0
	refused, err := walkGoFiles(root, skipDir, func(rel string, src []byte) error {
		files++
		found, err := checkSource(rel, src)
		if err != nil {
			return err
		}
		for _, line := range found {
			problems = append(problems, problem{rel, line})
		}
		return nil
	})
	for _, line := range refused {
		problems = append(problems, problem{strings.TrimSuffix(line, ": "+whyNotRegular), line})
	}
	// Both lists are in walk order already; a stable sort by path merges them.
	slices.SortStableFunc(problems, func(a, b problem) int {
		return slices.Compare(strings.Split(a.rel, "/"), strings.Split(b.rel, "/"))
	})
	var lines []string
	for _, p := range problems {
		lines = append(lines, p.line)
	}
	return lines, files, err
}

// walkGoFiles is the one walk of Go files in test/lint. It walks dir through
// an os.Root, so no path can reach outside dir, leaves out every directory
// skip reports (none when skip is nil), and calls visit with the
// slash-separated path relative to dir and the content of every regular .go
// file, in lexical order. It never reads a .go file that is not a regular
// file, such as a symbolic link, and returns one problem line for each
// instead (whyNotRegular).
func walkGoFiles(dir string, skip func(r *os.Root, rel, name string) (bool, error),
	visit func(rel string, src []byte) error,
) ([]string, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var refused []string
	// fs.WalkDir walks slash-separated paths relative to dir, as reported.
	err = fs.WalkDir(r.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel == "." || skip == nil {
				return nil
			}
			skipped, err := skip(r, rel, d.Name())
			if err != nil || !skipped {
				return err
			}
			return fs.SkipDir
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if !d.Type().IsRegular() {
			refused = append(refused, rel+": "+whyNotRegular)
			return nil
		}
		src, err := r.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		return visit(rel, src)
	})
	return refused, err
}

// checkSource returns what in src, the content of the Go file at rel, a
// slash-separated path from the module root, would hide code from depguard or
// forbidigo, one line each.
func checkSource(rel string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, group := range f.Comments {
		for _, c := range group.List {
			pos := fset.Position(c.Slash)
			at := fmt.Sprintf("%s:%d:%d: %s", rel, pos.Line, pos.Column, c.Text)
			for _, linter := range guardrails {
				if silences(c.Text, linter) {
					problems = append(problems, at+" would hide "+linter+" issues; fix the design or the call instead (CLAUDE.md)")
				}
			}
			// The go command reads build constraints above the package
			// clause only.
			if c.Pos() < f.Package {
				if why := constraintHides(c.Text); why != "" {
					problems = append(problems, at+" "+why)
				}
			}
		}
	}
	return problems, nil
}

// skipDir reports whether the go command's ./... pattern, which golangci-lint
// lints, leaves the directory out: one whose name starts with . or _, a
// testdata directory, or the root of another module (isModuleRoot, so a
// directory named go.mod hides nothing, issue #207). rel is the directory's
// slash-separated path within r. go.mod is looked up through r, so a symlink
// named go.mod is followed while it stays inside r, and one that escapes r is
// an error.
func skipDir(r *os.Root, rel, name string) (bool, error) {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" {
		return true, nil
	}
	return isModuleRoot(r.FS(), rel)
}

// isModuleRoot reports whether dir, a slash-separated path within fsys, is the
// root of a module as the go command reads one: a directory holding a go.mod
// that is no directory. A directory named go.mod makes no module root. go.mod
// is looked up with fs.Stat, so a symlink named go.mod counts by what it points
// to, in whatever way fsys follows it. It is the one definition of a nested
// module root for the walks of this package (skipDir, docEntries).
func isModuleRoot(fsys fs.FS, dir string) (bool, error) {
	fi, err := fs.Stat(fsys, path.Join(dir, "go.mod"))
	switch {
	case err == nil:
		return !fi.IsDir(), nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func TestScanSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    map[string]string // content by slash-separated path
		symlinks map[string]string // target by slash-separated path
		want     []string
	}{
		{
			name: "nothing hidden",
			files: map[string]string{
				"a/a.go":      "package a\n",
				"a/a_test.go": "//go:build e2e\n\npackage a\n\nvar _ = 1 //nolint:gosec // probe\n",
			},
		},
		{
			name:  "nolint directive",
			files: map[string]string{"a/a.go": "package a\n\nimport \"os\" //nolint:depguard // probe\n\nvar _ = os.Getpagesize\n"},
			want: []string{
				"a/a.go:3:13: //nolint:depguard // probe would hide depguard issues; fix the design or the call instead (CLAUDE.md)",
			},
		},
		{
			name:  "nolint directive for forbidigo",
			files: map[string]string{"a/a.go": "package a\n\nvar _ = 1 //nolint:forbidigo // probe\n"},
			want: []string{
				"a/a.go:3:11: //nolint:forbidigo // probe would hide forbidigo issues; fix the design or the call instead (CLAUDE.md)",
			},
		},
		{
			// The composition roots may read the environment (issue #111), and a
			// directive that silences forbidigo on such a read would hide the rule
			// that says which one and where: the guard is linter-wide, so it
			// covers them.
			name:  "nolint directive on a composition root's environment read",
			files: map[string]string{"cmd/shift2eu/main.go": "package main\n\nimport \"os\"\n\nvar token = os.Getenv(\"TOKEN\") //nolint:forbidigo // probe\n"},
			want: []string{
				"cmd/shift2eu/main.go:5:32: //nolint:forbidigo // probe would hide forbidigo issues; fix the design or the call instead (CLAUDE.md)",
			},
		},
		{
			// The CLI may read no variable either (issue #152), and the guard is
			// linter-wide, so a directive on such a read in internal/cli is
			// refused as well: the guard names no group, it names forbidigo.
			name:  "nolint directive on the CLI's environment read",
			files: map[string]string{"internal/cli/cli.go": "package cli\n\nimport \"os\"\n\nvar token = os.Getenv(\"TOKEN\") //nolint:forbidigo // probe\n"},
			want: []string{
				"internal/cli/cli.go:5:32: //nolint:forbidigo // probe would hide forbidigo issues; fix the design or the call instead (CLAUDE.md)",
			},
		},
		{
			// The CLI's _test.go files are held to the same group as its
			// production code (issue #152), package syscall included, which a test
			// imports for syscall.Mkfifo: a directive on one of those calls would
			// hide what the tests may not do either.
			name:  "nolint directive on the CLI test's syscall call",
			files: map[string]string{"internal/cli/cli_test.go": "package cli_test\n\nimport \"syscall\"\n\n//nolint:forbidigo // probe\nvar _ = syscall.Exec\n"},
			want: []string{
				"internal/cli/cli_test.go:5:1: //nolint:forbidigo // probe would hide forbidigo issues; fix the design or the call instead (CLAUDE.md)",
			},
		},
		{
			name:  "nolint directive on a test's environment write",
			files: map[string]string{"internal/app/analysis/analysis_test.go": "package analysis_test\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { t.Setenv(\"TOKEN\", \"x\") } //nolint:forbidigo // probe\n"},
			want: []string{
				"internal/app/analysis/analysis_test.go:5:53: //nolint:forbidigo // probe would hide forbidigo issues; fix the design or the call instead (CLAUDE.md)",
			},
		},
		{
			name:  "bare nolint directive",
			files: map[string]string{"a/a.go": "package a\n\nvar _ = 1 //nolint // probe\n"},
			want: []string{
				"a/a.go:3:11: //nolint // probe would hide depguard issues; fix the design or the call instead (CLAUDE.md)",
				"a/a.go:3:11: //nolint // probe would hide forbidigo issues; fix the design or the call instead (CLAUDE.md)",
			},
		},
		{
			name:  "negated e2e tag",
			files: map[string]string{"a/a.go": "//go:build !e2e\n\npackage a\n"},
			want:  []string{"a/a.go:1:1: //go:build !e2e " + constraintHides("//go:build !e2e")},
		},
		{
			name:  "negated integration tag in a +build line",
			files: map[string]string{"a/a.go": "// +build !integration\n\npackage a\n"},
			want:  []string{"a/a.go:1:1: // +build !integration " + constraintHides("// +build !integration")},
		},
		{
			name:  "constraint below the package clause",
			files: map[string]string{"a/a.go": "package a\n\n//go:build !e2e\n"},
		},
		{
			name:  "skipped directories",
			files: map[string]string{"_x/a.go": "//go:build !e2e\n\npackage x\n", "a/testdata/a.go": "//go:build !e2e\n\npackage a\n"},
		},
		{
			// A go.mod that is no directory is the root of another module,
			// as the go command's own search reads it, so ./... stops there,
			// and the walk leaves the subtree out with it.
			name: "the root of another module is skipped",
			files: map[string]string{
				"sub/go.mod": "module example.com/sub\n",
				"sub/a.go":   "//go:build !e2e\n\npackage a\n",
			},
		},
		{
			// A symlink named go.mod counts by what it points to, as the go
			// command's own search reads it: one that points at a file is a
			// module root, so the subtree is skipped and the violation in it
			// is left unread. The target is no .go file, so the walk reports
			// nothing for it either.
			name: "a go.mod symlink to a file is a module root",
			files: map[string]string{
				"real.mod": "module example.com/real\n",
				"sub/a.go": "//go:build !e2e\n\npackage a\n",
			},
			symlinks: map[string]string{"sub/go.mod": "../real.mod"},
		},
		{
			// A symlink named go.mod that points at a DIRECTORY is no module
			// root either, so ./... still reaches the subtree: the walk goes
			// into sub and reports the violation in it.
			name: "a go.mod symlink to a directory is no module root",
			files: map[string]string{
				"dir/keep.txt": "\n",
				"sub/a.go":     "//go:build !e2e\n\npackage a\n",
			},
			symlinks: map[string]string{"sub/go.mod": "../dir"},
			want:     []string{"sub/a.go:1:1: //go:build !e2e " + constraintHides("//go:build !e2e")},
		},
		{
			// A DIRECTORY named go.mod is no module root (issue #207), so the
			// subtree of the directory holding it is no root's subtree: ./...
			// still reaches it, and the walk must read the violation in it.
			name: "a directory named go.mod is no module root",
			files: map[string]string{
				"sub/go.mod/keep.go": "//go:build !e2e\n\npackage keep\n",
			},
			want: []string{"sub/go.mod/keep.go:1:1: //go:build !e2e " + constraintHides("//go:build !e2e")},
		},
		{
			// golangci-lint lints the link with the directive of its target,
			// which the walk does not read.
			name:     "symbolic link to a Go file",
			files:    map[string]string{"_hidden/zz.go": "package a\n\nimport \"os\" //nolint: all // probe\n"},
			symlinks: map[string]string{"a/zz.go": "../_hidden/zz.go"},
			want:     []string{"a/zz.go: " + whyNotRegular},
		},
		{
			// The walk visits a/ before a.go and b.go, so a/zz.go's
			// refusal comes between their problems.
			name: "refusals in walk order among other problems",
			files: map[string]string{
				"_hidden/zz.go": "package a\n",
				"a.go":          "//go:build !e2e\n\npackage a\n",
				"b.go":          "//go:build !e2e\n\npackage a\n",
				"0/a.go":        "//go:build !e2e\n\npackage a\n",
			},
			symlinks: map[string]string{"a/zz.go": "../_hidden/zz.go"},
			want: []string{
				"0/a.go:1:1: //go:build !e2e " + constraintHides("//go:build !e2e"),
				"a/zz.go: " + whyNotRegular,
				"a.go:1:1: //go:build !e2e " + constraintHides("//go:build !e2e"),
				"b.go:1:1: //go:build !e2e " + constraintHides("//go:build !e2e"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			mkdir := func(path string) {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range tc.files {
				path := filepath.Join(root, filepath.FromSlash(name))
				mkdir(path)
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for name, target := range tc.symlinks {
				path := filepath.Join(root, filepath.FromSlash(name))
				mkdir(path)
				if err := os.Symlink(filepath.FromSlash(target), path); err != nil {
					t.Fatal(err)
				}
			}

			got, _, err := scanSources(root)
			if err != nil {
				t.Fatalf("scanSources: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("scanSources mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
