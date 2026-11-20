package lint_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// docRule is the strict depguard rule .golangci.yml holds the test trees' own
// documentation packages under (issue #152): test/architecture,
// test/container, test/devenv, test/docs, test/e2e, test/examples, test/lint
// and test/site, each a package a composition root may import. Its allow
// list is empty, so no import of theirs lints. That closes os/exec, unsafe,
// os/user and every other package there, but it has nothing to say about a file
// that needs no import at all: an assembly file beside a bodyless Go func can
// issue a raw execve or read envp, and neither depguard nor forbidigo sees it
// (issue #194, the audit of #185).
//
// The checks below read the trees of that rule from its files: list, so a tree
// the rule takes on is held by them without a second edit.
const docRule = "test-docs-are-pure"

// nonGoSourceExtensions are the extensions of the files the Go toolchain builds
// beside the Go source of a package, which package source is but Go source is
// not: assembly (.s, .S, .sx), SWIG (.swig, .swigcxx), C (.c, .h), C++ (.cc,
// .cpp, .cxx, .hh, .hpp, .hxx), Objective-C (.m), Fortran (.f, .F, .for, .f90)
// and the compiled system object (.syso). The case of .s beside .S, and of .f
// beside .F, is the toolchain's: the capital letter marks a file its
// preprocessor reads, so both are sources of a package.
var nonGoSourceExtensions = []string{
	".s", ".S", ".sx",
	".c", ".h", ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx",
	".m",
	".f", ".F", ".for", ".f90",
	".syso",
	".swig", ".swigcxx",
}

// docSourceLine returns the problem docSourceProblems reports for rel, a file at
// a non-Go source extension under dir, one of the trees the rule
// test-docs-are-pure holds. It is the one wording the check and the cases below
// share, so a fixture states which files are refused rather than how the check
// words a refusal.
func docSourceLine(rel, dir string) string {
	return fmt.Sprintf("%s: a %s file is a Go-toolchain source that needs no import, so neither "+
		"depguard's %s nor a forbidigo group reads the code it carries; %s is held to Go source "+
		"only, so the file belongs outside the tree (issue #194)",
		rel, path.Ext(rel), docRule, dir)
}

// docSymlinkLine returns the problem docSourceProblems reports for rel, a
// symlink under dir, one of the trees the rule test-docs-are-pure holds. A
// symlink is no file the walk reads, but an import of its path follows it and
// the toolchain builds what it finds at the other end, so it carries a source
// from outside the tree into the tree (issue #194, the auditor's finding).
func docSymlinkLine(rel, dir string) string {
	return fmt.Sprintf("%s: a symlink under %s is no file the walk reads, but an import of the path "+
		"follows it and the toolchain builds the source it finds there, so a file from outside the "+
		"tree reaches a package %s holds to Go source only; the symlink belongs outside the tree "+
		"(issue #194)", rel, dir, docRule)
}

// TestDocPackagesHoldNoNonGoSource checks every entry of every tree the depguard
// rule test-docs-are-pure holds for a non-Go source or a symlink
// (docSourceProblems): the repository holds neither today, and the test pins
// that. An assembly file under test/e2e, or under test/lint itself, would carry
// code into a package a composition root may import, and neither depguard's
// allow list of nothing nor a forbidigo group would read it; a symlink there
// would carry such a file in from outside the walk, which reads the link and
// not what an import of it compiles.
//
// It walks the module root with docEntries, the walk of the paths themselves, so
// a testdata directory, a directory whose name starts with . or _ and the root
// of another module are left out as the go command leaves them out of ./...: a
// non-Go file under testdata is no source of the held package, so a .s file
// there is a fixture, as in testdata/docsources/mod below. A package there is
// still built when it is imported by path, which this check does not cover (a
// follow-up to #194). A symlink is no such file: docEntries still reads every
// one of them, testdata, _x and .x directories included, because an import
// follows a link whatever directory it sits in.
func TestDocPackagesHoldNoNonGoSource(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	held, takenOut := docTreesOf(t, root)

	entries := docEntriesOf(t, root)
	if len(entries) == 0 {
		t.Fatalf("no file under %s", root)
	}

	for _, problem := range docSourceProblems(entries, held, takenOut) {
		t.Error(problem)
	}

	// A tree the rule holds that the walk never reached would pass for the
	// wrong reason: the check would hold nothing there.
	for _, dir := range held {
		if !slices.ContainsFunc(entries, func(e docPath) bool { return underDir(e.rel, dir) }) {
			t.Errorf("the walk of %s found no file under %s, a tree %s holds", root, dir, docRule)
		}
	}
}

// TestDocPackageNonGoSourceProbe runs the check over the probe tree
// testdata/docsources/mod, laid out as the repository's test directory is: an
// assembly file under test/e2e must be refused, and the one under
// test/e2e/testdata, which is no source of the held package, must not even
// reach the check, as the walk of the module root leaves that directory out.
// The repository holds no such file, so without the probe nothing would prove
// the check bites (the pin of issue #194).
func TestDocPackageNonGoSourceProbe(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	held, takenOut := docTreesOf(t, root)

	const dir = "test/lint/testdata/docsources/mod"
	entries := docEntriesOf(t, filepath.Join(root, filepath.FromSlash(dir)))

	// The probe holds both fixtures, one the check refuses and one the walk
	// must leave out with its testdata directory, so a walk that read nothing
	// would fail here rather than pass.
	if !slices.ContainsFunc(entries, func(e docPath) bool {
		return e.rel == "test/e2e/probe_amd64.s" && !e.symlink
	}) {
		t.Fatalf("the probe %s holds no test/e2e/probe_amd64.s, only %v", dir, entries)
	}
	if slices.ContainsFunc(entries, func(e docPath) bool { return e.rel == "test/e2e/testdata/probe_amd64.s" }) {
		t.Fatalf("the walk of %s read test/e2e/testdata/probe_amd64.s, a file under a testdata "+
			"directory the go command leaves out of ./...: %v", dir, entries)
	}

	want := []string{docSourceLine("test/e2e/probe_amd64.s", "test/e2e")}
	if diff := cmp.Diff(want, docSourceProblems(entries, held, takenOut), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("docSourceProblems() on the probe mismatch (-want +got):\n%s", diff)
	}
}

// TestDocPackageSymlinkProbe pins the refusal of a symlink under a held tree.
// A symlinked directory such as test/e2e/x -> ../../elsewhere is no file of any
// source extension, so a check that only read extensions passes it, and yet an
// import of test/e2e/x follows the link and the toolchain builds the .s file it
// finds there: the walk reads one tree and the compiler another (the auditor's
// finding on issue #194).
//
// The fixture is built in a temporary directory rather than committed, because
// a symlink does not survive as a symlink everywhere a checkout lands, and a
// harness that resolved it would hide the very shape the check refuses.
func TestDocPackageSymlinkProbe(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const elsewhere = "elsewhere"

	for _, dir := range []string{"test/e2e", elsewhere} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	assembly := filepath.Join(root, elsewhere, "x_amd64.s")
	if err := os.WriteFile(assembly, []byte("// what an import of test/e2e/x compiles\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// test/e2e/x -> ../../elsewhere, the shape that carries a source the walk
	// never reads past a held tree and into a package a composition root may
	// import.
	link := filepath.Join(root, "test", "e2e", "x")
	if err := os.Symlink(filepath.Join("..", "..", elsewhere), link); err != nil {
		t.Skipf("this platform holds no symlink: %v", err)
	}

	entries := docEntriesOf(t, root)

	// The walk records the link as a symlink rather than following it, and the
	// source at the other end stays outside every held tree.
	if !slices.ContainsFunc(entries, func(e docPath) bool { return e.rel == "test/e2e/x" && e.symlink }) {
		t.Fatalf("the walk of %s read no symlink test/e2e/x, only %v", root, entries)
	}
	if !slices.ContainsFunc(entries, func(e docPath) bool { return e.rel == elsewhere+"/x_amd64.s" }) {
		t.Fatalf("the walk of %s read no %s/x_amd64.s, only %v", root, elsewhere, entries)
	}

	want := []string{docSymlinkLine("test/e2e/x", "test/e2e")}
	if diff := cmp.Diff(want, docSourceProblems(entries, []string{"test/e2e"}, nil), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("docSourceProblems() on the symlink probe mismatch (-want +got):\n%s", diff)
	}
}

// docSourceCase is one case of TestDocSourceProblems: the files of a walked
// directory, the symlinks it holds, the trees the rule holds and the trees it
// takes out, and the problems docSourceProblems must report for them. A case
// that sets neither tree is read against docSourceHeld and docSourceTakenOut,
// and its files are handed over before its symlinks, as one walk would.
type docSourceCase struct {
	name     string
	files    []string
	symlinks []string
	held     []string
	takenOut []string
	want     []string
}

// docSourceHeld are the trees the cases of TestDocSourceProblems hold by
// default, two of the rule's directories: test/e2e, which holds test files and
// doc.go, and test/lint.
var docSourceHeld = []string{"test/e2e", "test/lint"}

// docSourceTakenOut are the trees the cases take out by default, as the rule
// takes test/architecture/internal out: the package check there reads os,
// os/exec and test/internal/releasetarget.
var docSourceTakenOut = []string{"test/architecture/internal"}

// TestDocSourceProblems pins docSourceProblems, the seam of
// TestDocPackagesHoldNoNonGoSource and TestDocPackageNonGoSourceProbe, on the
// entries a walk hands it (docPath): every extension of the toolchain's non-Go
// sources, a symlink of any name and extension, the files that are neither and
// pass, the testdata, _ and . directories whose files are left out of ./... and
// no source of the held package, the tree the rule takes out, and the trees it
// does not hold, whose files are no concern of this rule. Each refusal is one
// line, in the order the walk handed the entries over.
func TestDocSourceProblems(t *testing.T) {
	t.Parallel()

	cases := []docSourceCase{
		{
			name:  "an assembly file beside a bodyless Go func",
			files: []string{"test/e2e/probe_amd64.s"},
			want:  []string{docSourceLine("test/e2e/probe_amd64.s", "test/e2e")},
		},
		{
			name:  "a C source",
			files: []string{"test/e2e/probe.c"},
			want:  []string{docSourceLine("test/e2e/probe.c", "test/e2e")},
		},
		{
			name:  "a compiled system object",
			files: []string{"test/lint/syso_amd64.syso"},
			want:  []string{docSourceLine("test/lint/syso_amd64.syso", "test/lint")},
		},
		{
			// The pin of the whole rule: the file a package's Go source sits
			// beside is no concern of an import list.
			name:  "the files under one tree are each refused",
			files: []string{"test/e2e/a.c", "test/e2e/a.go", "test/e2e/a.s"},
			want:  []string{docSourceLine("test/e2e/a.c", "test/e2e"), docSourceLine("test/e2e/a.s", "test/e2e")},
		},
		{
			name: "the files a doc-only tree may hold",
			files: []string{
				"test/e2e/doc.go",
				"test/e2e/runs_test.go",
				"test/e2e/testdata/inventory.json",
				"test/e2e/README.md",
				"test/lint/probe.sh",
				"test/lint/assess.golden",
				"test/lint/.golangci.yml",
			},
		},
		{
			// A file under testdata is left out of ./... and no source of the held
			// package, so a non-Go source there is a fixture: the e2e tier keeps
			// its inventories and goldens there.
			name: "a file under a testdata directory is exempt",
			files: []string{
				"test/e2e/testdata/probe_amd64.s",
				"test/e2e/runs/testdata/deep/probe.c",
			},
		},
		{
			// The testdata, _ and . exemption of ./... is one exemption, and
			// this check takes it whole: a non-Go file under any of those
			// directories is no source of the held package, as the comment of
			// docSourceProblems says. docEntries drops those files before they
			// reach the check, so the cases below hand them over directly, as
			// a check that kept its own walk would (the reviewer's N1).
			name:  "a non-Go file under a testdata, _ or . directory is exempt",
			files: []string{"test/e2e/testdata/a.s", "test/e2e/_x/a.s", "test/e2e/.x/a.s"},
		},
		{
			// A symlink is the exception the exemption does not reach: an
			// import of the path follows it whatever directory it sits in.
			name:     "a symlink under a _ directory is refused",
			symlinks: []string{"test/e2e/_x/p"},
			want:     []string{docSymlinkLine("test/e2e/_x/p", "test/e2e")},
		},
		{
			name:  "a directory that only starts with testdata is no testdata directory",
			files: []string{"test/e2e/mytestdata/probe_amd64.s"},
			want:  []string{docSourceLine("test/e2e/mytestdata/probe_amd64.s", "test/e2e")},
		},
		{
			name:     "a tree the rule takes out is no tree",
			held:     []string{"test/architecture"},
			takenOut: []string{"test/architecture/internal"},
			files:    []string{"test/architecture/internal/check/probe_amd64.s"},
		},
		{
			name:     "a directory that only starts with a tree the rule takes out is held by it",
			held:     []string{"test/architecture"},
			takenOut: []string{"test/architecture/internal"},
			files:    []string{"test/architecture/internalcheck/probe_amd64.s"},
			want: []string{docSourceLine("test/architecture/internalcheck/probe_amd64.s",
				"test/architecture")},
		},
		{
			name:  "a tree the rule does not hold is no concern of this rule",
			files: []string{"test/architecture/probe_amd64.s", "internal/cli/probe.c", "testdata/probe.s"},
		},
		{
			// A symlinked directory has no extension at all, so nothing but a
			// look at the entry type refuses it, and an import of the path
			// compiles whatever source sits at the other end.
			name:     "a symlinked directory under a held tree",
			symlinks: []string{"test/e2e/probe"},
			want:     []string{docSymlinkLine("test/e2e/probe", "test/e2e")},
		},
		{
			// The link carries a name the check would otherwise pass as Go
			// source: what a package builds is what the link points at.
			name:     "a symlink named as Go source",
			symlinks: []string{"test/lint/probe.go"},
			want:     []string{docSymlinkLine("test/lint/probe.go", "test/lint")},
		},
		{
			// A file under testdata is left out of ./... and no source of the
			// held package, but an import of a path that reaches it through a
			// symlink still compiles: the exemption of a file there is no
			// exemption of a link to it. This case pins docSourceProblems on
			// entries the walk must hand it, not the walk itself: docEntries
			// is what keeps reading testdata directories for symlinks
			// (TestDocEntries and TestDocPackageSkippedDirSymlinkProbe).
			name:     "a symlink under a testdata directory is refused too",
			symlinks: []string{"test/e2e/testdata/probe", "test/e2e/testdata"},
			want: []string{
				docSymlinkLine("test/e2e/testdata/probe", "test/e2e"),
				docSymlinkLine("test/e2e/testdata", "test/e2e"),
			},
		},
		{
			// The whole held tree is a link. underDir("test/e2e",
			// "test/e2e") is false, so a check of what lies under a tree
			// passes it, and the reach check of
			// TestDocPackagesHoldNoNonGoSource would be the only thing to
			// fail, naming no symlink at all.
			name:     "a symlink that is a held tree",
			symlinks: []string{"test/e2e"},
			want:     []string{docSymlinkLine("test/e2e", "test/e2e")},
		},
		{
			// An ancestor of a held tree is no file under it either, yet
			// every path under the link is a link. The first held tree the
			// entry is an ancestor of names the refusal.
			name:     "a symlink that is an ancestor of a held tree",
			symlinks: []string{"test"},
			want:     []string{docSymlinkLine("test", "test/e2e")},
		},
		{
			// Only a symlink is refused for its own path: an entry that is
			// no link and carries no non-Go source extension passes, whatever
			// its name.
			name:  "a non-symlink entry equal to a held tree",
			files: []string{"test/e2e", "test"},
		},
		{
			// test/e2e2 holds no path of test/e2e, so it is no tree of it and
			// no ancestor of it.
			name:     "a symlink that only starts with a held tree",
			symlinks: []string{"test/e2e2"},
		},
		{
			// A taken-out tree refuses nothing, and its ancestors are no
			// reason to refuse a link either: the whole-tree rule counts the
			// trees the rule holds, in both directions, and test/slow is an
			// ancestor of test/slow/fixtures only.
			name:     "a symlink that is an ancestor of only a tree the rule takes out",
			held:     []string{"test/e2e"},
			takenOut: []string{"test/slow/fixtures"},
			symlinks: []string{"test/slow"},
		},
		{
			name:     "a symlink under a tree the rule takes out is no tree",
			held:     []string{"test/architecture"},
			takenOut: []string{"test/architecture/internal"},
			symlinks: []string{"test/architecture/internal/check/probe"},
		},
		{
			name:     "a symlink under no tree the rule holds is no concern of this rule",
			symlinks: []string{"test/e2e2/probe", "internal/cli/probe", "testdata/probe"},
		},
		{
			name:  "a directory that only starts with a held tree is no tree of it",
			files: []string{"test/e2e2/probe_amd64.s", "test/e2ex/y/probe.s"},
		},
		{
			name:     "no tree held holds nothing",
			files:    []string{"test/e2e/probe_amd64.s"},
			held:     []string{},
			takenOut: []string{},
		},
	}
	for _, ext := range nonGoSourceExtensions {
		rel := "test/e2e/probe_amd64" + ext
		cases = append(cases, docSourceCase{
			name:  "a " + ext + " file, which the toolchain builds beside the Go source",
			files: []string{rel},
			want:  []string{docSourceLine(rel, "test/e2e")},
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			held, takenOut := tc.held, tc.takenOut
			if held == nil {
				held = docSourceHeld
			}
			if takenOut == nil {
				takenOut = docSourceTakenOut
			}

			entries := make([]docPath, 0, len(tc.files)+len(tc.symlinks))
			for _, rel := range tc.files {
				entries = append(entries, docPath{rel: rel})
			}
			for _, rel := range tc.symlinks {
				entries = append(entries, docPath{rel: rel, symlink: true})
			}

			got := docSourceProblems(entries, held, takenOut)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("docSourceProblems() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDocTrees pins docTrees, which reads the trees of the rule from
// .golangci.yml rather than a list kept here: the rule's files: list is what
// holds those packages, so the check must hold the same trees, and a tree the
// rule takes on is covered without a second edit. It reads only the shapes the
// rule writes today, a held directory as **/<dir>/** and one taken out as
// !**/<dir>/**, and !$test, which only takes the _test.go files out and names
// no directory; anything else is an error, so a list written another way is
// never silently read as no tree at all.
func TestDocTrees(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		doc      string
		held     []string
		takenOut []string
		wantErr  bool
	}{
		{
			name: "the rule as .golangci.yml writes it",
			doc:  docRuleDoc(`"**/test/e2e/**"`, `"!$test"`),
			held: []string{"test/e2e"},
		},
		{
			name:     "a tree the rule takes out",
			doc:      docRuleDoc(`"**/test/architecture/**"`, `"!**/test/architecture/internal/**"`, `"!$test"`),
			held:     []string{"test/architecture"},
			takenOut: []string{"test/architecture/internal"},
		},
		{
			name:     "the order the rule writes",
			doc:      docRuleDoc(`"**/test/lint/**"`, `"**/test/e2e/**"`, `"!**/test/lint/testdata/**"`),
			held:     []string{"test/lint", "test/e2e"},
			takenOut: []string{"test/lint/testdata"},
		},
		{
			name:    "a rule .golangci.yml does not hold",
			doc:     "linters:\n  settings:\n    depguard:\n      rules:\n        domain-is-pure:\n          list-mode: strict\n          files:\n            - \"**/internal/domain/**\"\n",
			wantErr: true,
		},
		{
			name:    "a files list that names no directory",
			doc:     docRuleDoc(`"**/test/e2e/**"`, `"$all"`),
			wantErr: true,
		},
		{
			name:    "a files list that names a file glob",
			doc:     docRuleDoc(`"**/test/e2e/**/*_test.go"`),
			wantErr: true,
		},
		{
			name:    "a rule with no files list",
			doc:     "linters:\n  settings:\n    depguard:\n      rules:\n        test-docs-are-pure:\n          list-mode: strict\n          allow: []\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			held, takenOut, err := docTrees([]byte(tt.doc))
			if (err != nil) != tt.wantErr {
				t.Fatalf("docTrees() error = %v, want an error: %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if diff := cmp.Diff(tt.held, held, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("docTrees() held mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.takenOut, takenOut, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("docTrees() takenOut mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// docRuleDoc returns a .golangci.yml whose depguard rules hold the one rule
// test-docs-are-pure, its files: list the entries files, each written as the
// rule writes it (quotes included). It is the shape .golangci.yml has on the
// path docTrees reads.
func docRuleDoc(files ...string) string {
	var b strings.Builder
	b.WriteString("linters:\n  settings:\n    depguard:\n      rules:\n        test-docs-are-pure:\n" +
		"          list-mode: strict\n          files:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "            - %s\n", f)
	}
	b.WriteString("          allow: []\n          deny:\n            - pkg: os\n" +
		"              desc: a doc-only package imports nothing\n")

	return b.String()
}

// docTreesOf returns the trees of the depguard rule test-docs-are-pure, as the
// .golangci.yml at root writes them (docTrees): the directories the checks of
// this file hold, and the ones they take out.
func docTreesOf(t *testing.T, root string) (held, takenOut []string) {
	t.Helper()

	doc, err := os.ReadFile(filepath.Join(root, ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}

	held, takenOut, err = docTrees(doc)
	if err != nil {
		t.Fatalf("read depguard rule %s: %v", docRule, err)
	}
	if len(held) == 0 {
		t.Fatalf(".golangci.yml holds no tree for depguard rule %s", docRule)
	}

	return held, takenOut
}

// TestDocEntries pins docEntries, the walk TestDocPackagesHoldNoNonGoSource,
// the probes and TestDocPackageSkippedDirSymlinkProbe read: the files the go
// command's ./... pattern reaches, which skipDir leaves out of ./... (a
// testdata directory, a directory whose name starts with . or _, the root of
// another module, which is a go.mod that is no directory, symlinks and FIFOs
// included), and, beside them, every symlink the tree holds at any depth,
// testdata, _ and . directories included. A non-Go file under testdata is no
// source of a held package, but a symlink there is one: an import of its path
// follows it and the toolchain builds the source it finds, so the exemptions of
// ./... reach the extension rule and no symlink (issue #194).
func TestDocEntries(t *testing.T) {
	t.Parallel()

	const elsewhere = "elsewhere"

	tests := []struct {
		name     string
		files    map[string]string // content by slash-separated path
		symlinks map[string]string // target by slash-separated path
		want     []docPath
	}{
		{
			name: "the files the go command's ./... pattern reaches",
			files: map[string]string{
				"test/e2e/doc.go":                   "package e2e\n",
				"test/e2e/testdata/inventory.json":  "{}\n",
				"test/lint/probe.sh":                "#!/bin/sh\n",
				"test/e2e/mytestdata/probe_amd64.s": "\n",
				"testdata/probe.s":                  "\n",
				"_x/probe.go":                       "package x\n",
				".x/probe.go":                       "package x\n",
			},
			want: []docPath{
				{rel: "test/e2e/doc.go"},
				{rel: "test/e2e/mytestdata/probe_amd64.s"},
				{rel: "test/lint/probe.sh"},
			},
		},
		{
			// A symlink is no file any extension tells, and the walk reads
			// the link rather than what it points at: no entry of elsewhere
			// stands under test/e2e/x, and elsewhere's own files appear at
			// their own paths.
			name: "a symlink is recorded and not followed",
			files: map[string]string{
				"test/e2e/doc.go":        "package e2e\n",
				"elsewhere/a/x.go":       "package a\n",
				elsewhere + "/x_amd64.s": "\n",
			},
			symlinks: map[string]string{"test/e2e/x": "../../" + elsewhere},
			want: []docPath{
				{rel: "elsewhere/a/x.go"},
				{rel: "elsewhere/x_amd64.s"},
				{rel: "test/e2e/doc.go"},
				{rel: "test/e2e/x", symlink: true},
			},
		},
		{
			// The pin of the gap: a symlink under a directory ./... leaves
			// out is still a path an import follows, so the walk reads it,
			// at any depth, while the files beside it stay left out.
			name: "a symlink inside a directory ./... leaves out",
			files: map[string]string{
				"test/e2e/testdata/inventory.json":    "{}\n",
				"test/e2e/_x/probe.go":                "package x\n",
				"test/e2e/.x/probe.go":                "package x\n",
				"test/e2e/testdata/held/fixtures/a.c": "\n",
			},
			symlinks: map[string]string{
				"test/e2e/testdata/p":               "../../../" + elsewhere,
				"test/e2e/_x/p":                     "../../../" + elsewhere,
				"test/e2e/.x/p":                     "../../../" + elsewhere,
				"test/e2e/testdata/held/fixtures/p": "../../../../" + elsewhere,
				"testdata/p":                        "../" + elsewhere,
				"_x/p":                              "../" + elsewhere,
				".x/p":                              "../" + elsewhere,
			},
			want: []docPath{
				{rel: ".x/p", symlink: true},
				{rel: "_x/p", symlink: true},
				{rel: "test/e2e/.x/p", symlink: true},
				{rel: "test/e2e/_x/p", symlink: true},
				{rel: "test/e2e/testdata/held/fixtures/p", symlink: true},
				{rel: "test/e2e/testdata/p", symlink: true},
				{rel: "testdata/p", symlink: true},
			},
		},
		{
			// The root of another module is no tree of this one, and ./...
			// stops there: neither its files nor a symlink under it is a
			// source of a package this module's rule holds.
			name: "nothing under the root of another module",
			files: map[string]string{
				"test/e2e/doc.go":                "package e2e\n",
				"test/e2e/vendored/go.mod":       "module example.com/vendored\n",
				"test/e2e/vendored/x_amd64.s":    "\n",
				"test/e2e/vendored/testdata/x.c": "\n",
			},
			symlinks: map[string]string{
				"test/e2e/vendored/p":             "../../../" + elsewhere,
				"test/e2e/vendored/testdata/p.go": "../../../../" + elsewhere,
			},
			want: []docPath{{rel: "test/e2e/doc.go"}},
		},
		{
			// A go.mod that is no directory is the root of another module,
			// as the go command's own search reads it, so a DIRECTORY named
			// go.mod hides no subtree: ./... still reaches the package beside
			// it, and the file it holds under that name. The walk must read
			// the directory's contents and its siblings, not skip them.
			name: "a directory named go.mod is no module root",
			files: map[string]string{
				"test/e2e/sub/x_amd64.s":   "\n",
				"test/e2e/sub/go.mod/keep": "\n",
			},
			want: []docPath{
				{rel: "test/e2e/sub/go.mod/keep"},
				{rel: "test/e2e/sub/x_amd64.s"},
			},
		},
		{
			// A symlink named go.mod counts by what it points to, as the go
			// command's own search reads it: one that points at a file is a
			// module root, so nothing under it is returned, the link itself
			// included, and the target's own path stays out of ./... under
			// testdata.
			name: "a go.mod symlink to a file is a module root",
			files: map[string]string{
				"test/e2e/testdata/real.mod": "module example.com/real\n",
			},
			symlinks: map[string]string{"test/e2e/sub/go.mod": "../testdata/real.mod"},
		},
		{
			// A symlink named go.mod that points at a DIRECTORY is no module
			// root either, so the walk goes into the subtree and returns the
			// link, which is no .go file and counts as a link, beside the
			// sources of the package it sits in.
			name: "a go.mod symlink to a directory is no module root",
			files: map[string]string{
				"test/e2e/sub/testdata/held/keep": "\n",
				"test/e2e/sub/x_amd64.s":          "\n",
			},
			symlinks: map[string]string{"test/e2e/sub/go.mod": "testdata/held"},
			want: []docPath{
				{rel: "test/e2e/sub/go.mod", symlink: true},
				{rel: "test/e2e/sub/x_amd64.s"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for rel, content := range tc.files {
				p := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for rel, target := range tc.symlinks {
				p := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.FromSlash(target), p); err != nil {
					t.Skipf("this platform holds no symlink: %v", err)
				}
			}

			if diff := cmp.Diff(tc.want, docEntriesOf(t, root),
				cmpopts.EquateEmpty(), cmpopts.EquateComparable(docPath{})); diff != "" {
				t.Errorf("docEntries() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDocPackageSkippedDirSymlinkProbe pins the same gap at the level the check
// runs at: a symlink under a REAL testdata directory of a held tree, and one
// under a _x and a .x directory beside it, must reach docSourceProblems and be
// refused. The walk of the module root leaves those directories out for the
// extension rule only, so a check that walked them out whole would never see
// the link, while an import of its path follows it and the toolchain builds the
// source at the other end (the reviewer's S1 and the auditor's first finding on
// issue #194).
//
// The fixture is built in a temporary directory rather than committed, because
// a symlink does not survive as a symlink everywhere a checkout lands, and a
// harness that resolved it would hide the very shape the check refuses.
func TestDocPackageSkippedDirSymlinkProbe(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const elsewhere = "elsewhere"

	for _, dir := range []string{"test/e2e/testdata", "test/e2e/_x", "test/e2e/.x", elsewhere} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, elsewhere, "x_amd64.s"),
		[]byte("// what an import of test/e2e/testdata/p compiles\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The order the walk reads them in: .x before _x before testdata.
	dirs := []string{"test/e2e/.x", "test/e2e/_x", "test/e2e/testdata"}
	for _, dir := range dirs {
		link := filepath.Join(root, filepath.FromSlash(dir), "p")
		if err := os.Symlink(filepath.Join("..", "..", "..", elsewhere), link); err != nil {
			t.Skipf("this platform holds no symlink: %v", err)
		}
	}

	entries := docEntriesOf(t, root)
	if !slices.ContainsFunc(entries, func(e docPath) bool {
		return e.rel == elsewhere+"/x_amd64.s" && !e.symlink
	}) {
		t.Fatalf("the walk of %s read no %s/x_amd64.s, only %v", root, elsewhere, entries)
	}

	want := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		rel := dir + "/p"
		if !slices.ContainsFunc(entries, func(e docPath) bool { return e.rel == rel && e.symlink }) {
			t.Fatalf("the walk of %s read no symlink %s, only %v", root, rel, entries)
		}
		want = append(want, docSymlinkLine(rel, "test/e2e"))
	}

	if diff := cmp.Diff(want, docSourceProblems(entries, []string{"test/e2e"}, nil),
		cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("docSourceProblems() on the skipped-directory symlink probe mismatch (-want +got):\n%s", diff)
	}
}

// docEntriesOf returns the entries docEntries reads from the directory tree at
// root, failing the test on a walk error: the seam the checks of this file read
// the paths themselves through, rather than a walk kept here that could drift
// from the one they run.
func docEntriesOf(t *testing.T, root string) []docPath {
	t.Helper()

	entries, err := docEntries(os.DirFS(root))
	if err != nil {
		t.Fatalf("docEntries(%s): %v", root, err)
	}

	return entries
}
