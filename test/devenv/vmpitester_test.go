// Pins the update step for the installed tester wrapper (issue #692): make vm-pi-tester installs the same
// root-owned files into an existing VM, as make vm-gh-wrapper does for the gh wrapper, so a maintainer need not
// rebuild the VM. It is a host target: its recipe runs limactl from the host clone, like vm-gh-wrapper.
//
// dev/vm-pi-tester.sh prints the script the VM runs: one function called on its last line, the three files byte for
// byte inside a here-document each. These tests run it as the host clone does, parse what it prints with sh -n, take
// its here-documents apart, refuse the sources whose here-document would not carry the file, and run the printed
// script against a temporary root with a fake sudo, so no test touches /usr/local/lib of the machine it runs on.
package devenv_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// vmPiTesterGenerator is the generator make vm-pi-tester runs on the host clone: it prints the script the VM runs.
const vmPiTesterGenerator = "dev/vm-pi-tester.sh"

// vmPiTesterCall is the function the printed script is, and the last line that calls it: a stream cut short before
// that line installs nothing (issue #692).
const vmPiTesterCall = "vm_pi_tester"

// vmPiTesterDoneLine is the line the make recipe requires of the printed script.
const vmPiTesterDoneLine = "vm-pi-tester: done"

// vmPiTesterInstalledLine is the line the printed script prints before the done line, naming the directory it
// installed into; the make recipe requires it as it requires the done line (issue #692, round 3), since done alone
// says the run finished, not that it installed into that /usr/local path.
const vmPiTesterInstalledLine = "vm-pi-tester: installed the tester's wrapper and env-printing refusal in " + piTesterInstallParent + "/pi-tester"

// piTesterRootHook is the variable the printed script has to honour, a prefix for the /usr/local/lib/shift2eu/pi-tester
// it installs to (empty by default): a production hook these tests require, since without it the printed script can
// only install to the machine's own /usr/local, which no test may write (issue #692).
const piTesterRootHook = "SHIFT2EU_PI_TESTER_ROOT"

// vmPiTesterHereDocuments are the here-documents the printed script carries, each holding one file byte for byte.
var vmPiTesterHereDocuments = []struct {
	marker string
	source string
}{
	{marker: "SHIFT2EU_PITESTER_WRAPPER", source: piTesterScript},
	{marker: "SHIFT2EU_PITESTER_EXTENSION", source: denyExtensionFile},
	{marker: "SHIFT2EU_PITESTER_LIST", source: denyListFile},
}

// vmPiTesterStream runs the generator for sources and returns the script it prints, which is what make vm-pi-tester
// pipes into limactl. A source given relative to the repository root is resolved against it, since the generator runs
// from the sandbox's home, not from the repository.
func vmPiTesterStream(t *testing.T, sb *sandbox, sources ...string) string {
	t.Helper()

	paths := make([]string, 0, len(sources))

	for _, source := range sources {
		if filepath.IsAbs(source) {
			paths = append(paths, source)

			continue
		}

		paths = append(paths, scriptPath(t, source))
	}

	stdout, stderr, err := sb.run(t, scriptPath(t, vmPiTesterGenerator), paths, sb.baseEnv(), "")
	if err != nil {
		t.Fatalf("%s exited non-zero for the repository's own files: %v\nstderr: %s", vmPiTesterGenerator, err, stderr)
	}

	if strings.TrimSpace(stdout) == "" {
		t.Fatalf("%s printed nothing for the repository's own files: the VM would run an empty script", vmPiTesterGenerator)
	}

	return stdout
}

// vmPiTesterHereDocumentOpen returns the index, in the printed script's lines, of the cat line that opens the
// here-document the script carries for marker: the one whose <<'<marker>' starts it, so a truncation test can cut
// the stream inside the file's body.
func vmPiTesterHereDocumentOpen(t *testing.T, stream, marker string) int {
	t.Helper()

	open := -1

	for i, line := range strings.Split(strings.TrimSuffix(stream, "\n"), "\n") {
		if !strings.Contains(line, "<<'"+marker+"'") {
			continue
		}

		if open >= 0 {
			t.Fatalf("%s prints %d <<'%s' here-documents, want exactly one: this test cannot tell which copy the VM installs (issue #692)", vmPiTesterGenerator, 2, marker)
		}

		open = i
	}

	if open < 0 {
		t.Fatalf("%s prints no <<'%s' here-document, so the three files do not travel inside the script (issue #692):\n%s", vmPiTesterGenerator, marker, stream)
	}

	return open
}

// vmPiTesterHereDocument returns the body of the here-document the printed script carries for marker: the lines
// between the cat that opens it and the marker line that ends it, each with its newline.
func vmPiTesterHereDocument(t *testing.T, stream, marker string) string {
	t.Helper()

	lines := strings.Split(strings.TrimSuffix(stream, "\n"), "\n")

	open := vmPiTesterHereDocumentOpen(t, stream, marker)

	for j := open + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == marker {
			return strings.Join(lines[open+1:j], "\n") + "\n"
		}
	}

	t.Fatalf("%s prints a <<'%s' here-document and never ends it: the shell then reads the rest of the script as its body, and the file it installs is cut short (issue #692)", vmPiTesterGenerator, marker)

	return ""
}

// piTesterRootInstalled returns the three paths the printed script installs to under root, as the root hook prefixes
// the directory the wrapper and the refusal live in.
func piTesterRootInstalled(root string) (script, extension, list string) {
	dir := filepath.Join(root, filepath.FromSlash(piTesterInstallDir))

	return filepath.Join(dir, "pi-tester.sh"),
		filepath.Join(dir, "extensions", "deny-env-printing.ts"),
		filepath.Join(dir, "deny-env-printing.json")
}

// TestVMPiTesterGeneratorPrintsAScriptThatCarriesItsSources pins what make vm-pi-tester sends the VM (issue #692):
// the generator exits 0 on the repository's own files, what it prints parses with sh -n, its last line is the call
// that runs the installation, and each of its here-documents carries its source byte for byte.
func TestVMPiTesterGeneratorPrintsAScriptThatCarriesItsSources(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	stream := vmPiTesterStream(t, sb, piTesterScript, denyExtensionFile, denyListFile)

	cmd := exec.Command("sh", "-n")
	cmd.Env = envList(sb.baseEnv())
	cmd.Stdin = strings.NewReader(stream)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("what %s prints does not parse, so limactl's `sh -s` runs nothing of it: %v\n%s", vmPiTesterGenerator, err, out)
	}

	lines := strings.Split(strings.TrimSuffix(stream, "\n"), "\n")

	if got := lines[len(lines)-1]; got != vmPiTesterCall {
		t.Errorf("the script %s prints ends with %q, want %q: without that last line nothing calls the function that installs, so a whole stream installs nothing (issue #692)", vmPiTesterGenerator, got, vmPiTesterCall)
	}

	for _, doc := range vmPiTesterHereDocuments {
		embedded := vmPiTesterHereDocument(t, stream, doc.marker)
		want := readRepoFile(t, doc.source)

		if diff := cmp.Diff(strings.Split(want, "\n"), strings.Split(embedded, "\n")); diff != "" {
			t.Errorf("the %s here-document of the script %s prints and %s are not the same text: the VM would install a copy that is not the one the tests run (-want +got):\n%s",
				doc.marker, vmPiTesterGenerator, doc.source, diff)
		}
	}
}

// TestVMPiTesterGeneratorRefusesASourceItsHereDocumentCannotCarry pins the other half (issue #692): a source that
// holds its own here-document's delimiter line, does not end with a newline, is empty or is not there would not
// travel byte for byte, so the generator refuses it before it prints anything, and make sends the VM nothing.
func TestVMPiTesterGeneratorRefusesASourceItsHereDocumentCannotCarry(t *testing.T) {
	t.Parallel()

	sources := []string{piTesterScript, denyExtensionFile, denyListFile}

	cases := []struct {
		name string
		// source is the index into sources of the file the case leaves broken.
		source int
		// content is what the case writes into that file; nil leaves it missing.
		content func(source string) string
	}{
		{name: "a source holds its own here-document's delimiter line", source: 1, content: func(source string) string {
			return source + "SHIFT2EU_PITESTER_EXTENSION\n"
		}},
		{name: "a source does not end with a newline", source: 0, content: func(source string) string {
			return strings.TrimSuffix(source, "\n")
		}},
		{name: "a source is empty", source: 2, content: func(string) string { return "" }},
		{name: "a source is missing", source: 2, content: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)

			dir := t.TempDir()
			paths := make([]string, 0, len(sources))

			for i, source := range sources {
				path := filepath.Join(dir, filepath.Base(source))
				paths = append(paths, path)

				content := readRepoFile(t, source)
				if i == tc.source {
					if tc.content == nil {
						continue
					}

					content = tc.content(content)
				}

				writeFileMode(t, path, []byte(content), 0o600)
			}

			stdout, stderr, err := sb.run(t, scriptPath(t, vmPiTesterGenerator), paths, sb.baseEnv(), "")
			if err == nil {
				t.Fatalf("%s exited 0 with %s: a here-document would not carry that file byte for byte, so make would send the VM a script that installs a different file, or one cut short (issue #692)\nstdout: %s\nstderr: %s",
					vmPiTesterGenerator, tc.name, stdout, stderr)
			}

			if strings.TrimSpace(stdout) != "" {
				t.Errorf("%s printed %q on stdout with %s, although it refused: make pipes stdout into the VM, so a part of the script would run there (issue #692)\nstderr: %s",
					vmPiTesterGenerator, stdout, tc.name, stderr)
			}

			if !strings.Contains(stderr, paths[tc.source]) {
				t.Errorf("%s's stderr does not name %s, the file it refused, so a maintainer cannot tell which one to fix (issue #692): %s",
					vmPiTesterGenerator, paths[tc.source], stderr)
			}
		})
	}
}

// TestVMPiTesterScriptInstallsUnderTheRootHook pins what the printed script does in the VM (issue #692): run with
// sh -s as the VM runs it, it installs the three files, the wrapper 0755 and the two refusal files 0644, under the
// root its hook names, and prints the done line the make recipe requires. A fake sudo stands in for the VM user's,
// so no test touches /usr/local/lib of the machine it runs on.
func TestVMPiTesterScriptInstallsUnderTheRootHook(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.useFakeVMSudo(t)

	root := filepath.Join(sb.root, "pi-tester-root")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create %s: %v", root, err)
	}

	stream := vmPiTesterStream(t, sb, piTesterScript, denyExtensionFile, denyListFile)

	env := sb.baseEnv()
	env[piTesterRootHook] = root

	stdout, stderr, err := sb.runStream(t, env, stream)
	if err != nil {
		t.Fatalf("the script %s prints exited non-zero in the VM: %v\nstdout: %s\nstderr: %s", vmPiTesterGenerator, err, stdout, stderr)
	}

	if !strings.Contains(stdout+stderr, vmPiTesterDoneLine) {
		t.Errorf("the script %s prints does not end by printing %q, the line the make recipe requires (issue #692)\nstdout: %s\nstderr: %s",
			vmPiTesterGenerator, vmPiTesterDoneLine, stdout, stderr)
	}

	// What make requires of the output (round 3): the installed line, naming the directory the root hook prefixed,
	// and then the done line as the last line, so a maintainer reads where the run installed and make knows it
	// installed there rather than somewhere else.
	lines := piTesterOutputLines(stdout)
	if len(lines) == 0 {
		t.Fatalf("the script %s prints wrote nothing on stdout, so neither the installed line nor %q reaches make (issue #692, round 3):\nstderr: %s",
			vmPiTesterGenerator, vmPiTesterDoneLine, stderr)
	}

	if last := lines[len(lines)-1]; last != vmPiTesterDoneLine {
		t.Errorf("the last line the script %s prints writes is %q, want %q exactly: make reads that line as the run's end (issue #692, round 3): %q",
			vmPiTesterGenerator, last, vmPiTesterDoneLine, lines)
	}

	wantInstalledLine := "vm-pi-tester: installed the tester's wrapper and env-printing refusal in " + filepath.Join(root, filepath.FromSlash(piTesterInstallDir))

	switch installed := slices.Index(lines, wantInstalledLine); {
	case installed < 0:
		t.Errorf("the script %s prints wrote no line %q, the directory the run installed into under the root %s names: the line is what names an installation, and make requires it as it requires %q (issue #692, round 3): %q",
			vmPiTesterGenerator, wantInstalledLine, piTesterRootHook, vmPiTesterDoneLine, lines)
	case installed == len(lines)-1:
		t.Errorf("the script %s prints writes the installed line after the last line %q: the installed line has to precede it, so the directory is printed even when the done line is the last thing a maintainer sees (issue #692, round 3): %q",
			vmPiTesterGenerator, vmPiTesterDoneLine, lines)
	}

	script, extension, list := piTesterRootInstalled(root)

	files := []struct {
		name   string
		path   string
		mode   os.FileMode
		source string
	}{
		{name: "the wrapper", path: script, mode: 0o755, source: piTesterScript},
		{name: "the deny extension", path: extension, mode: 0o644, source: denyExtensionFile},
		{name: "the deny list", path: list, mode: 0o644, source: denyListFile},
	}

	for _, file := range files {
		info, err := os.Stat(file.path)
		if err != nil {
			t.Fatalf("the script %s prints installed no %s under %s, the root %s names: the VM's paths are under %s of the machine the test runs on, so the printed script has to read that variable as a prefix for %s, empty by default (issue #692): %v\nstdout: %s\nstderr: %s",
				vmPiTesterGenerator, file.name, root, piTesterRootHook, piTesterInstallParent, piTesterInstallParent, err, stdout, stderr)
		}

		got, err := os.ReadFile(file.path)
		if err != nil {
			t.Fatalf("read %s: %v", file.path, err)
		}

		if want := readRepoFile(t, file.source); string(got) != want {
			t.Errorf("the script %s prints installed %s as %q, want %s byte for byte (issue #692)", vmPiTesterGenerator, file.name, string(got), file.source)
		}

		if perm := info.Mode().Perm(); perm != file.mode {
			t.Errorf("the script %s prints installed %s as mode %04o, want %04o: the tester runs the wrapper, and reads the two refusal files (issue #692)",
				vmPiTesterGenerator, file.name, perm, file.mode)
		}
	}
}

// TestVMPiTesterScriptRunsNothingOnATruncatedStream pins the rule the generator's shape exists for (issue #692): the
// printed script is one function called on its last line, so a stream that stops inside a here-document or just
// before that line installs nothing and prints no done line. A stream cut inside a function or a here-document is a
// syntax error, and without the call nothing runs at all.
func TestVMPiTesterScriptRunsNothingOnATruncatedStream(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// cutAt is the line that is matched exactly to find the cut point: the last line to arrive, unless
		// cutBefore says the stream stops just before it.
		cutAt     string
		cutBefore bool
		// insideHereDocument is the marker of the here-document the stream stops inside instead: cutAt is then
		// unused, and what arrives is the here-document's opening line and one line of the file's body, never
		// the marker line that ends it, so the wrapper itself arrives cut short.
		insideHereDocument string
		reason             string
	}{
		{
			name:               "one line into the here-document that carries the wrapper",
			insideHereDocument: vmPiTesterHereDocuments[0].marker,
			reason:             "the here-document the wrapper travels in stops after the wrapper's first line, so its delimiter never comes and the wrapper is cut short",
		},
		{
			name:      "just before the call that runs the installation",
			cutAt:     vmPiTesterCall,
			cutBefore: true,
			reason:    "the script is whole but nothing calls the function",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			sb.useFakeVMSudo(t)

			root := filepath.Join(sb.root, "pi-tester-root")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatalf("create %s: %v", root, err)
			}

			stream := vmPiTesterStream(t, sb, piTesterScript, denyExtensionFile, denyListFile)

			lines := strings.Split(stream, "\n")

			var cut int

			switch {
			case tc.insideHereDocument != "":
				// One line of the file's body: the cut is measured from the here-document's opening line, never
				// from a line of the wrapper, so a change to the wrapper moves the cut with it.
				cut = vmPiTesterHereDocumentOpen(t, stream, tc.insideHereDocument) + 1

				if cut >= len(lines) || strings.TrimSpace(lines[cut]) == tc.insideHereDocument {
					t.Fatalf("the %s here-document of the script %s prints carries no body, so a stream cut one line into it would also carry the marker line that ends it: this test would pin nothing (issue #692)",
						tc.insideHereDocument, vmPiTesterGenerator)
				}
			default:
				cut = lineIndex(t, stream, tc.cutAt)
				if tc.cutBefore {
					cut--
				}
			}

			env := sb.baseEnv()
			env[piTesterRootHook] = root

			stdout, stderr, _ := sb.runStream(t, env, strings.Join(lines[:cut+1], "\n")+"\n")

			script, extension, list := piTesterRootInstalled(root)

			for _, path := range []string{script, extension, list} {
				if _, err := os.Stat(path); err == nil {
					t.Errorf("a stream cut %s installed %s, although %s: a transfer cut short must install nothing (issue #692)\nstdout: %s\nstderr: %s",
						tc.name, path, tc.reason, stdout, stderr)
				}
			}

			if strings.Contains(stdout+stderr, vmPiTesterDoneLine) {
				t.Errorf("a stream cut %s prints %q, although %s: make would take that for a finished installation (issue #692)\nstdout: %s\nstderr: %s",
					tc.name, vmPiTesterDoneLine, tc.reason, stdout, stderr)
			}
		})
	}
}

// TestVMPiTesterScriptInstallsOnlyAWrapperEndingWithItsExecLine pins the second half of ADR 0044's amendment (issue
// #702): the printed script accepts a wrapper only when its last line is the new exec line, and refuses a wrapper
// whose last line is the one from before (`--extension` and pi's own discovery, with project trust) or that lost its
// exec line, like any other stream cut short. A VM never installs a wrapper that loads the unit folder's extensions
// or trusts its settings.
func TestVMPiTesterScriptInstallsOnlyAWrapperEndingWithItsExecLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// lastLine is what the case's wrapper ends with; empty drops the wrapper's last line.
		lastLine string
		refuses  bool
	}{
		{name: "the wrapper ends with the new exec line", lastLine: piTesterExecLine},
		{name: "the wrapper ends with the line from before issue #702", lastLine: piTesterOldExecLine, refuses: true},
		{name: "the wrapper's exec line is gone", refuses: true},
	}

	const truncation = "does not end with its exec line, so it arrived cut short; nothing installed"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			sb.useFakeVMSudo(t)

			root := filepath.Join(sb.root, "pi-tester-root")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatalf("create %s: %v", root, err)
			}

			wrapper := piTesterWrapperWithLastLine(t, tc.lastLine)
			stream := vmPiTesterStream(t, sb, wrapper, denyExtensionFile, denyListFile)

			env := sb.baseEnv()
			env[piTesterRootHook] = root

			stdout, stderr, err := sb.runStream(t, env, stream)

			script, extension, list := piTesterRootInstalled(root)

			if tc.refuses {
				if err == nil {
					t.Fatalf("the script %s prints exited 0 with %s: the wrapper's last line is the one check that tells a whole wrapper from a stream cut short, and the VM's installation then starts a pi that trusts the project (ADR 0044, issue #702)\nstdout: %s\nstderr: %s",
						vmPiTesterGenerator, tc.name, stdout, stderr)
				}

				if !strings.Contains(stdout+stderr, truncation) {
					t.Errorf("the script %s prints did not say %q with %s, so a maintainer whose update stopped reads no reason and cannot tell it from a broken transfer (issue #702):\nstdout: %s\nstderr: %s",
						vmPiTesterGenerator, truncation, tc.name, stdout, stderr)
				}

				for _, path := range []string{script, extension, list} {
					if _, statErr := os.Stat(path); statErr == nil {
						t.Errorf("the script %s prints installed %s although %s: an installation refused at the wrapper's last line installs nothing (issue #702)\nstdout: %s\nstderr: %s",
							vmPiTesterGenerator, path, tc.name, stdout, stderr)
					}
				}

				return
			}

			if err != nil {
				t.Fatalf("the script %s prints exited non-zero with the wrapper ending in its exec line: %v\nstdout: %s\nstderr: %s", vmPiTesterGenerator, err, stdout, stderr)
			}

			got, err := os.ReadFile(script)
			if err != nil {
				t.Fatalf("the script %s prints installed no wrapper at %s: %v\nstdout: %s\nstderr: %s", vmPiTesterGenerator, script, err, stdout, stderr)
			}

			if want := readRepoFile(t, piTesterScript); string(got) != want {
				t.Errorf("the script %s prints installed the wrapper as %q, want %s byte for byte (issue #702)", vmPiTesterGenerator, string(got), piTesterScript)
			}
		})
	}
}

// piTesterWrapperWithLastLine writes dev/pi-tester.sh into a file of its own with its last line replaced by line, or
// with its last line dropped when line is empty, and returns the path: a wrapper whose last line is the one from
// before issue #702, and one whose exec line is gone, are the two shapes the installer has to refuse.
func piTesterWrapperWithLastLine(t *testing.T, line string) string {
	t.Helper()

	body := strings.Split(strings.TrimSuffix(readRepoFile(t, piTesterScript), "\n"), "\n")

	if line == "" {
		body = body[:len(body)-1]
	} else {
		body[len(body)-1] = line
	}

	path := filepath.Join(t.TempDir(), "pi-tester.sh")
	writeFileMode(t, path, []byte(strings.Join(body, "\n")+"\n"), 0o600)

	return path
}

// makefileAssignmentWords returns the words of a makefile's `NAME := ...` assignment, joining its
// backslash-continued lines. It fails the test when the makefile has no such assignment.
func makefileAssignmentWords(t *testing.T, text, name string) []string {
	t.Helper()

	lines := strings.Split(text, "\n")

	start := -1

	for i, line := range lines {
		if strings.HasPrefix(line, name) && strings.Contains(line, ":=") {
			start = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s has no %s := assignment, so this test cannot read the target list it pins (issue #692)", makefilePath, name)
	}

	var parts []string

	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		continued := strings.HasSuffix(line, `\`)
		line = strings.TrimSuffix(line, `\`)

		if i == start {
			line = strings.TrimSpace(strings.SplitN(line, ":=", 2)[1])
		}

		parts = append(parts, line)

		if !continued {
			break
		}
	}

	return strings.Fields(strings.Join(parts, " "))
}

// makefilePhonyTargets returns every target a `.PHONY:` declaration of text names, joining the declarations'
// backslash-continued lines.
func makefilePhonyTargets(text string) []string {
	const prefix = ".PHONY:"

	lines := strings.Split(text, "\n")

	var targets []string

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}

		for ; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			continued := strings.HasSuffix(line, `\`)

			targets = append(targets, strings.Fields(strings.TrimPrefix(strings.TrimSuffix(line, `\`), prefix))...)

			if !continued {
				break
			}
		}
	}

	return targets
}

// TestMakefileHasThePiTesterUpdateTarget pins the update path of issue #692: a VM created before the root-owned
// tester wrapper has it installed by make vm-pi-tester, a host target with a help line like vm-gh-wrapper's, so
// the maintainer does not have to rebuild the VM. The recipe runs the VM's script with the tests' root hook unset,
// and requires both lines the script prints, the one naming the directory it installed into as well as the done
// line (round 3).
func TestMakefileHasThePiTesterUpdateTarget(t *testing.T) {
	t.Parallel()

	text := readRepoFile(t, makefilePath)

	if !slices.Contains(makefileAssignmentWords(t, text, "HOST_TARGETS"), piTesterUpdateTarget) {
		t.Errorf("%s: HOST_TARGETS does not name %s, so make does not refuse it from the workspace although its recipe runs limactl: the update step must run on the host clone, as vm-gh-wrapper does (issue #692, issue #227)", makefilePath, piTesterUpdateTarget)
	}

	if !slices.Contains(makefilePhonyTargets(text), piTesterUpdateTarget) {
		t.Errorf("%s: no .PHONY declaration names %s, so a file or directory with that name would hide the target and the update step would silently do nothing (issue #692)", makefilePath, piTesterUpdateTarget)
	}

	lines := strings.Split(text, "\n")

	rule := -1

	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), piTesterUpdateTarget+":") {
			rule = i

			break
		}
	}

	if rule < 0 {
		t.Fatalf("%s has no %s target, so a VM created before the installed tester wrapper can only be rebuilt (issue #692)", makefilePath, piTesterUpdateTarget)
	}

	if !strings.Contains(lines[rule], "## ") {
		t.Errorf("%s: %s has no ## help line, so it does not appear in make help's vm-* list as vm-gh-wrapper does (issue #692): %s", makefilePath, piTesterUpdateTarget, strings.TrimSpace(lines[rule]))
	}

	if rule+1 >= len(lines) || !strings.HasPrefix(lines[rule+1], "\t") {
		t.Errorf("%s: %s has no recipe line after it, so the target installs nothing (issue #692): %s", makefilePath, piTesterUpdateTarget, strings.TrimSpace(lines[rule]))
	}

	var recipe []string

	for i := rule + 1; i < len(lines) && strings.HasPrefix(lines[i], "\t"); i++ {
		recipe = append(recipe, lines[i])
	}

	// The root hook is the tests' prefix for the install directory, so the VM's run unsets it (round 3): a value in
	// the maintainer's environment would otherwise send the installation outside /usr/local.
	limactl := -1

	for i, line := range recipe {
		if strings.Contains(line, "limactl shell") {
			limactl = i

			break
		}
	}

	switch {
	case limactl < 0:
		t.Errorf("%s: the %s recipe has no limactl line, so the script never reaches the VM (issue #692):\n%s", makefilePath, piTesterUpdateTarget, strings.Join(recipe, "\n"))
	case !strings.Contains(recipe[limactl], "env -u "+piTesterRootHook+" sh -s"):
		t.Errorf("%s: the %s recipe runs %q, which does not run the script with `env -u %s sh -s`: the hook is the tests' prefix for %s, and a value of it in the maintainer's environment would send the VM's installation outside /usr/local (issue #692, round 3):\n%s",
			makefilePath, piTesterUpdateTarget, strings.TrimSpace(recipe[limactl]), piTesterRootHook, piTesterInstallParent, strings.Join(recipe, "\n"))
	}

	// The recipe requires the line the script prints for the directory it installed into, as it requires the done
	// line (round 3): "done" alone says the run finished, not that it installed into the path the recipe means, so a
	// run whose paths changed under the root hook would pass.
	requiresInstalled := false

	for _, line := range recipe {
		if strings.Contains(line, "grep -Fqx") && strings.Contains(line, vmPiTesterInstalledLine) {
			requiresInstalled = true

			break
		}
	}

	if !requiresInstalled {
		t.Errorf("%s: the %s recipe does not check the VM's output with `grep -Fqx %q`, as it checks %q: a run that finished without installing into %s would otherwise pass, for instance after a path change on either side (issue #692, round 3):\n%s",
			makefilePath, piTesterUpdateTarget, vmPiTesterInstalledLine, vmPiTesterDoneLine, piTesterInstallParent+"/pi-tester", strings.Join(recipe, "\n"))
	}
}
