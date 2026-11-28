package architecture_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestModulesHaveNoReplace pins the auditor's M1 finding: a replace directive
// in the root go.mod lets a fork or a local copy stand in for an import the
// depguard rules allow by prefix, and neither depguard nor the third-party
// exemption of the package check sees the difference, so the release would
// carry code no rule vetted. The same holds for tools/go.mod (issue #164): a
// replace there would point make lint's actionlint at code nobody reviewed with
// the pin. No tracked module outside a testdata directory, where the probes'
// fixtures replace stand-in SDKs on purpose, holds one, and none may be added: a
// fork belongs in an allow entry the rule names, reviewed with it.
func TestModulesHaveNoReplace(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	for _, goMod := range trackedModules(t, root) {
		for _, replace := range readGoMod(t, filepath.Join(root, filepath.FromSlash(goMod))).replaces() {
			t.Errorf("%s holds the replace directive %s: it puts code no rule vetted where an allow entry allows an import by prefix, or where the tools module pins a lint tool",
				goMod, replace)
		}
	}
}

// TestToolsModuleFollowsTheRootModulesGo pins the auditor's LOW-3 finding (issue
// #164): tools/go.mod is a module of its own, which Renovate bumps separately,
// so its go directive, and its toolchain directive when either module has one,
// could drift from the root's. make lint would then build actionlint with a
// different toolchain from the one the product builds with, or ask for a go
// the VM and CI do not have. Both read through the go command's own parser.
func TestToolsModuleFollowsTheRootModulesGo(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	rootMod := readGoMod(t, filepath.Join(root, "go.mod"))
	toolsMod := readGoMod(t, filepath.Join(root, "tools", "go.mod"))

	if rootMod.Go == "" {
		t.Fatal("go.mod has no go directive")
	}

	if toolsMod.Go != rootMod.Go {
		t.Errorf("tools/go.mod has go %q, want go.mod's %q: the tools module builds actionlint with the toolchain the product builds with",
			toolsMod.Go, rootMod.Go)
	}

	if toolsMod.Toolchain != rootMod.Toolchain {
		t.Errorf("tools/go.mod has toolchain %q, want go.mod's %q (empty for none): the tools module builds actionlint with the toolchain the product builds with",
			toolsMod.Toolchain, rootMod.Toolchain)
	}
}

// TestModulesHoldNoToolchainDirective pins issue #710: the module's Go is its go
// line, and that is what the audit's checks job reads at run time to build
// govulncheck (issue #687). A toolchain directive is a second Go version in the
// same go.mod, which `go mod edit -toolchain` and its own Renovate update move on
// their own, so the go line would no longer be the one version the module builds
// with. Both tracked modules are read through the go command's own parser.
func TestModulesHoldNoToolchainDirective(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	for _, goMod := range trackedModules(t, root) {
		if toolchain := readGoMod(t, filepath.Join(root, filepath.FromSlash(goMod))).Toolchain; toolchain != "" {
			t.Errorf("%s holds the toolchain directive %q: the module's Go is its go line, which the audit's checks job reads at run time to build govulncheck, and a second version in the same file drifts from it on its own. Remove the toolchain line and keep the module's one Go in the go line (issue #710)",
				goMod, toolchain)
		}
	}
}

// TestGoModToolchainDirective pins the check above where its input comes from: a
// go.mod text with a toolchain directive is read as one, above or below the go
// line, and a go.mod without one is read as having none, so a parser that lost
// the field would not let the module pin pass silently (issue #710).
func TestGoModToolchainDirective(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		goMod string
		want  string
	}{
		{
			name:  "a go.mod with no toolchain directive holds none",
			goMod: "module example.com/m\n\ngo 1.27.4\n",
		},
		{
			name:  "a toolchain directive below the go line is read",
			goMod: "module example.com/m\n\ngo 1.27.4\n\ntoolchain go1.27.4\n",
			want:  "go1.27.4",
		},
		{
			name:  "a toolchain directive above the go line is read too",
			goMod: "module example.com/m\n\ntoolchain go1.28.0\n\ngo 1.27.4\n",
			want:  "go1.28.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tc.goMod), 0o600); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}

			if diff := cmp.Diff(tc.want, readGoMod(t, filepath.Join(dir, "go.mod")).Toolchain); diff != "" {
				t.Errorf("Toolchain (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTrackedModulesFindsTheRootAndToolsModules pins the walk the two tests
// above trust: a walk that found nothing would let every module through.
func TestTrackedModulesFindsTheRootAndToolsModules(t *testing.T) {
	t.Parallel()

	modules := trackedModules(t, repositoryRoot(t))

	for _, want := range []string{"go.mod", "tools/go.mod"} {
		if !slices.Contains(modules, want) {
			t.Errorf("trackedModules = %q, want it to hold %s", modules, want)
		}
	}

	for _, goMod := range modules {
		if slices.Contains(strings.Split(goMod, "/"), "testdata") {
			t.Errorf("trackedModules holds %s, under a testdata directory, which the go command ignores and whose probes replace on purpose", goMod)
		}
	}
}

// TestGoModReplaces pins the check TestModulesHaveNoReplace trusts, over
// go.mod files written here: a single-line replace directive, a block of them,
// a local directory in place of a module, and the three spellings a hand
// scanner misses - a bare carriage return after the word, with and without a
// block, and a CRLF file - all of which the go command reads as the directives
// they are. The two lines that are none, a module path that merely holds the
// word and a go.mod with no replace at all, hold none.
func TestGoModReplaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		goMod string
		want  []string
	}{
		{
			name:  "a go.mod holding no replace directive holds none",
			goMod: "module example.com/m\n\ngo 1.27.4\n\nrequire (\n\texample.com/lib v1.0.0\n)\n",
		},
		{
			name:  "a module path that merely holds the word is no directive",
			goMod: "module example.com/m\n\nrequire example.com/replace v1.0.0\n",
		},
		{
			name:  "a single-line replace directive is found",
			goMod: "module example.com/m\n\nreplace example.com/lib => example.com/fork v1.2.3\n",
			want:  []string{"example.com/lib => example.com/fork v1.2.3"},
		},
		{
			name:  "a local directory in place of a module is found",
			goMod: "module example.com/m\n\nreplace example.com/lib => ../fork\n",
			want:  []string{"example.com/lib => ../fork"},
		},
		{
			name: "a replace block's directives are found one by one",
			goMod: "module example.com/m\n\nreplace (\n\texample.com/lib => example.com/fork v1.2.3\n" +
				"\texample.com/other => ../other\n)\n",
			want: []string{
				"example.com/lib => example.com/fork v1.2.3",
				"example.com/other => ../other",
			},
		},
		{
			// The go.mod lexer reads a bare carriage return as whitespace,
			// so the go command applies this directive.
			name:  "a bare carriage return after the word does not hide a single-line replace",
			goMod: "module example.com/m\n\nreplace\rexample.com/lib => example.com/fork v1.2.3\n",
			want:  []string{"example.com/lib => example.com/fork v1.2.3"},
		},
		{
			// The same carriage return also separates the word from the
			// parenthesis that opens the block.
			name: "a bare carriage return after the word does not hide a replace block",
			goMod: "module example.com/m\n\nreplace\r(\n\texample.com/lib => example.com/fork v1.2.3\n" +
				"\texample.com/other => ../other\n)\n",
			want: []string{
				"example.com/lib => example.com/fork v1.2.3",
				"example.com/other => ../other",
			},
		},
		{
			name:  "a CRLF file's replace directive is found",
			goMod: "module example.com/m\r\n\r\nreplace example.com/lib => example.com/fork v1.2.3\r\n",
			want:  []string{"example.com/lib => example.com/fork v1.2.3"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tc.goMod), 0o600); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}

			if diff := cmp.Diff(tc.want, readGoMod(t, filepath.Join(dir, "go.mod")).replaces()); diff != "" {
				t.Errorf("replaces(-want +got):\n%s", diff)
			}
		})
	}
}

// TestRepositoryHasNoWorkspaceOrVendor pins the reviewer's N2 finding and the
// auditor's LOW one: a replace directive is not the only way to put unvetted
// code where an allow entry allows an import by prefix. A committed vendor
// directory wins over the module cache, and a go.work or go.work.sum file,
// which .gitignore keeps out of git, makes a directory outside the module stand
// in for a path under it. The release builds the main module alone, so none of
// them may exist, tracked or untracked: the check reads the file system, not
// git ls-files, because go.work is ignored and a force-added vendor directory
// is what a look at git ls-files would not have to catch.
func TestRepositoryHasNoWorkspaceOrVendor(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	// vendor/ is a directory, so any one of them counts: a vendor directory
	// holding no modules.txt is what a half-finished go mod vendor leaves
	// behind, and a single go build -mod=vendor would read it.
	for _, name := range []string{"go.work", "go.work.sum", "vendor"} {
		_, err := os.Stat(filepath.Join(root, name))
		switch {
		case err == nil:
			t.Errorf("%s exists at the repository root: it puts code no rule vetted where an allow entry allows an import by prefix, and the release builds the main module alone",
				name)
		case !errors.Is(err, fs.ErrNotExist):
			t.Errorf("stat %s: %v", name, err)
		}
	}
}

// goMod is what the go command reads from a go.mod: its go and toolchain
// directives and its replace directives.
type goMod struct {
	Go        string
	Toolchain string
	Replace   []struct {
		Old moduleRef
		New moduleRef
	}
}

// readGoMod reads the go.mod at path with the go command's own parser, go mod
// edit -json, rather than parsing the file: go.mod's lexer reads a bare
// carriage return as whitespace, so replace\rexample.com/lib => ... and
// replace\r( + entries + ) are directives the go command applies, and a hand
// scanner that reads only a space, a tab or a parenthesis after the word
// reports neither.
func readGoMod(t *testing.T, path string) goMod {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "mod", "edit", "-json", path)
	cmd.Dir = filepath.Dir(path)
	// No toolchain, proxy or workspace of the developer's may decide what
	// the go command reads: the guard is about the file, and the check runs
	// offline.
	cmd.Env = makeEnv(map[string]string{
		"GOFLAGS":     "-mod=readonly",
		"GOWORK":      "off",
		"GOPROXY":     "off",
		"GOTOOLCHAIN": "local",
	})

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go mod edit -json %s: %v", path, err)
	}

	var parsed goMod

	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("decode go mod edit -json %s: %v", path, err)
	}

	return parsed
}

// replaces returns the module's replace directives, each as "old => new" with
// the version of a module replacement, in the order go.mod holds them, or nil
// for none.
func (mod goMod) replaces() []string {
	if len(mod.Replace) == 0 {
		return nil
	}

	replaces := make([]string, 0, len(mod.Replace))

	for _, replace := range mod.Replace {
		replaces = append(replaces, replace.Old.String()+" => "+replace.New.String())
	}

	return replaces
}

// trackedModules returns the slash-separated path of every go.mod git tracks in
// root outside a testdata directory, which the go command ignores and where the
// probes' fixture modules replace stand-in SDKs on purpose.
func trackedModules(t *testing.T, root string) []string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", "-C", root, "ls-files", "-z", "--", "go.mod", "**/go.mod").Output()
	if err != nil {
		t.Fatalf("git ls-files in %s: %v", root, err)
	}

	var modules []string

	for _, name := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if name == "" || slices.Contains(strings.Split(name, "/"), "testdata") {
			continue
		}

		modules = append(modules, name)
	}

	return modules
}

// moduleRef is a module path and, for a module replacement, the version it
// names. A directory replacement names a path alone.
type moduleRef struct {
	Path    string
	Version string
}

// String renders the reference as go.mod writes it.
func (ref moduleRef) String() string {
	if ref.Version == "" {
		return ref.Path
	}

	return ref.Path + " " + ref.Version
}
