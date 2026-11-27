// Pins the installed layout of the tester's launch wrapper (issue #692): the VM installs root-owned copies of
// dev/pi-tester.sh and the env-printing refusal's two files from dev/lima.yaml, outside every path an agent can
// write, and .dot-agent-deck.toml's tester command runs the installed wrapper. The wrapper reads the refusal
// from its own installation directory, never from the unit folder, so a branch cannot weaken what the tester
// checks; a project copy that differs is ignored with one line naming both paths and the update step.
package devenv_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The files the VM installs and the paths it installs them to (issue #692), outside every path an agent can
// write. piTesterInstallDir is relative to a test's fake root; the rest are the absolute paths the VM uses.
const (
	piTesterInstallParent      = "/usr/local/lib/shift2eu"
	piTesterInstallDir         = "usr/local/lib/shift2eu/pi-tester"
	piTesterInstalledScript    = "/usr/local/lib/shift2eu/pi-tester/pi-tester.sh"
	piTesterInstalledExtension = "/usr/local/lib/shift2eu/pi-tester/extensions/deny-env-printing.ts"
	piTesterInstalledList      = "/usr/local/lib/shift2eu/pi-tester/deny-env-printing.json"
	piTesterUpdateTarget       = "vm-pi-tester"
)

// piTesterInstalled is one fake root's installed layout: the directory the wrapper is installed in, and the
// three files under it, as the wrapper finds them beside its own path.
type piTesterInstalled struct {
	dir       string
	script    string
	extension string
	list      string
}

// writePiTesterInstalled lays the installed files out under root, as dev/lima.yaml installs them, and returns
// their paths. The wrapper is always a copy of dev/pi-tester.sh; the two deny files are written as the case
// leaves them, so a case can install one that is missing, empty or a directory.
func writePiTesterInstalled(t *testing.T, root string, extension, list piFile) piTesterInstalled {
	t.Helper()

	dir := filepath.Join(root, filepath.FromSlash(piTesterInstallDir))
	installed := piTesterInstalled{
		dir:       dir,
		script:    filepath.Join(dir, "pi-tester.sh"),
		extension: filepath.Join(dir, "extensions", "deny-env-printing.ts"),
		list:      filepath.Join(dir, "deny-env-printing.json"),
	}

	writeFileMode(t, installed.script, []byte(readRepoFile(t, piTesterScript)), 0o755)
	writePiTesterFile(t, installed.extension, extension, 0o644)
	writePiTesterFile(t, installed.list, list, 0o644)

	return installed
}

// writePiTesterFile writes one file of the layout as the case leaves it: absent, a directory, or the content
// with mode.
func writePiTesterFile(t *testing.T, path string, file piFile, mode os.FileMode) {
	t.Helper()

	if !file.present {
		return
	}

	if file.directory {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("create %s: %v", path, err)
		}

		return
	}

	writeFileMode(t, path, []byte(file.content), mode)
}

// writePiTesterUnit creates the directory the deck starts the tester in - the unit's folder - and writes the
// project's copies of the files the installed layout holds, as the case leaves them.
func writePiTesterUnit(t *testing.T, root string, extension, list, wrapper piFile) string {
	t.Helper()

	dir := filepath.Join(root, "project")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}

	writePiTesterFile(t, filepath.Join(dir, denyExtensionFile), extension, 0o644)
	writePiTesterFile(t, filepath.Join(dir, denyListFile), list, 0o644)
	writePiTesterFile(t, filepath.Join(dir, piTesterScript), wrapper, 0o755)

	return dir
}

// piTesterWantArgv is the argv pi starts with: the installed extension first, because the unit folder's copy
// may be weakened or missing (issue #692), then every argument the deck's command passed unchanged.
func piTesterWantArgv(installed piTesterInstalled, args []string) []string {
	return append([]string{"--extension", installed.extension}, args...)
}

// limaHeredoc returns the text dev/lima.yaml hands the VM between the marker lines of the <<'marker' heredoc,
// dedented the way the provision script's YAML block scalar dedents it. It fails the test unless the template
// holds exactly one such heredoc.
func limaHeredoc(t *testing.T, template, marker string) string {
	t.Helper()

	opener := "<<'" + marker + "'"

	var bodies []string

	for _, script := range provisionScripts(t, template) {
		for i, line := range script.script {
			if !strings.Contains(line, opener) {
				continue
			}

			var body []string

			ended := false
			for _, next := range script.script[i+1:] {
				if strings.TrimSpace(next) == marker {
					ended = true

					break
				}

				body = append(body, next)
			}

			if !ended {
				t.Fatalf("%s line %d opens the <<'%s' heredoc without a %s line to end it: the shell then never ends the heredoc and the rest of the template becomes its body (issue #692)",
					limaTemplateRepoPath, script.header, marker, marker)
			}

			bodies = append(bodies, strings.Join(body, "\n")+"\n")
		}
	}

	if len(bodies) != 1 {
		t.Fatalf("%s has %d <<'%s' heredocs, want exactly one: the VM installs the root-owned copy from the template, not from the workspace mount an agent can write (issue #692)",
			limaTemplateRepoPath, len(bodies), marker)
	}

	return bodies[0]
}

// limaInstallLine returns the code line of dev/lima.yaml's provisioning that installs installed, failing the
// test when there is none. Comments are skipped, so a comment naming the path is not an installation.
func limaInstallLine(t *testing.T, template, installed string) string {
	t.Helper()

	for _, script := range provisionScripts(t, template) {
		for _, line := range script.script {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}

			fields := strings.Fields(trimmed)
			if !slices.Contains(fields, "install") {
				continue
			}

			if strings.Contains(trimmed, installed) {
				return trimmed
			}
		}
	}

	t.Fatalf("%s has no install command that installs %s: the VM would run a copy an agent can write, or none at all (issue #692)",
		limaTemplateRepoPath, installed)

	return ""
}

// assertLimaInstalls fails when dev/lima.yaml's install line for path does not give owner root, group root and
// the mode the installed file must have.
func assertLimaInstalls(t *testing.T, template, path, mode string) {
	t.Helper()

	line := limaInstallLine(t, template, path)
	fields := strings.Fields(line)

	for _, flag := range []struct{ name, value string }{
		{name: "-o", value: "root"},
		{name: "-g", value: "root"},
		{name: "-m", value: mode},
	} {
		found := false

		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == flag.name && fields[i+1] == flag.value {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("%s installs %s with %q, which does not carry %s %s: the installed copy has to be root's, so no agent can change what the tester runs (issue #692)",
				limaTemplateRepoPath, path, line, flag.name, flag.value)
		}
	}
}

// TestLimaTemplateInstallsThePiTester pins the provisioning step of issue #692: dev/lima.yaml embeds the
// wrapper and the two deny files verbatim, so the VM's copies cannot drift from the repository's, and installs
// each root-owned, with the mode the wrapper needs to run and the files need to be read.
func TestLimaTemplateInstallsThePiTester(t *testing.T) {
	t.Parallel()

	template := readRepoFile(t, limaTemplateRepoPath)

	cases := []struct {
		name      string
		marker    string
		project   string
		installed string
		mode      string
	}{
		{name: "the wrapper", marker: "PITESTER", project: piTesterScript, installed: piTesterInstalledScript, mode: "0755"},
		{name: "the deny extension", marker: "PIDENYEXT", project: denyExtensionFile, installed: piTesterInstalledExtension, mode: "0644"},
		{name: "the deny list", marker: "PIDENYLIST", project: denyListFile, installed: piTesterInstalledList, mode: "0644"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			embedded := limaHeredoc(t, template, tc.marker)
			want := readRepoFile(t, tc.project)

			if diff := cmp.Diff(strings.Split(want, "\n"), strings.Split(embedded, "\n")); diff != "" {
				t.Errorf("%s's %s heredoc and %s are not the same text. The VM installs the template's copy, which no agent can write, and these tests run the file: change both together (-want +got):\n%s",
					limaTemplateRepoPath, tc.marker, tc.project, diff)
			}

			assertLimaInstalls(t, template, tc.installed, tc.mode)
		})
	}
}

// piTesterSameCopy reports whether a project copy the case left is byte for byte the installed file, which is
// what the wrapper's cmp -s decides: a missing, empty or weakened copy differs.
func piTesterSameCopy(file piFile, installed string) bool {
	return file.present && !file.directory && file.content == installed
}

// TestPiTesterIgnoresTheProjectsCopies pins the review point the installed layout exists for (issue #692): the
// wrapper checks the installed denial and hands pi the installed extension, so a weakened or missing copy in the
// unit folder changes neither what is checked nor whether the tester starts. It says so in one line naming both
// paths and the update step, and says nothing when the two copies agree. The line carries one "(installed: "
// pair per project copy that is missing or differs, and none for a copy that matches, so a maintainer is sent to
// update exactly the files that need it (round 2 of the review and the audit).
//
// The wording is pinned against the audit's finding too: pi still discovers the project's .pi/extensions beside
// the installed extension, so the line may not say the project's copies are ignored.
func TestPiTesterIgnoresTheProjectsCopies(t *testing.T) {
	t.Parallel()

	extension := piFile{present: true, content: readRepoFile(t, denyExtensionFile)}
	list := piFile{present: true, content: readRepoFile(t, denyListFile)}
	wrapper := piFile{present: true, content: readRepoFile(t, piTesterScript)}

	cases := []struct {
		name             string
		projectExtension piFile
		projectList      piFile
		projectWrapper   piFile
	}{
		{
			name:             "the project copies are the installed ones",
			projectExtension: extension,
			projectList:      list,
			projectWrapper:   wrapper,
		},
		{
			name:             "the project list refuses nothing",
			projectExtension: extension,
			projectList:      piFile{present: true, content: `{"reason": "invented for this test", "patterns": []}`},
			projectWrapper:   wrapper,
		},
		{
			name:             "the project extension is empty",
			projectExtension: piFile{present: true},
			projectList:      list,
			projectWrapper:   wrapper,
		},
		{
			name:             "the project wrapper is missing",
			projectExtension: extension,
			projectList:      list,
			projectWrapper:   piFile{},
		},
		{
			name:             "two project copies differ and one matches",
			projectExtension: piFile{present: true, content: extension.content + "\n// weakened by a branch, which the tester must not load\n"},
			projectList:      piFile{present: true, content: `{"reason": "invented for this test", "patterns": []}`},
			projectWrapper:   wrapper,
		},
		{
			name: "the project holds none of the copies",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			homeDir := filepath.Join(root, "home")
			record := filepath.Join(root, "pi-record")

			for _, dir := range []string{binDir, homeDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			writePiTesterBin(t, binDir, record)

			installed := writePiTesterInstalled(t, root, extension, list)
			project := writePiTesterUnit(t, root, tc.projectExtension, tc.projectList, tc.projectWrapper)

			stdout, stderr, err := runPiTester(t, installed.script, project, piTesterArgs, piTesterEnv(binDir, homeDir, piTesterBasePath(t)))
			if err != nil {
				t.Fatalf("the wrapper exited non-zero although the installed files are whole: a missing or weakened copy in the unit folder must not stop the tester (issue #692): %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			got := readPiRecord(t, record)

			if diff := cmp.Diff(piTesterWantArgv(installed, piTesterArgs), got.argv); diff != "" {
				t.Errorf("pi did not start with the installed extension and the deck's arguments: the unit folder's copy must never decide what the tester loads (-want +got):\n%s", diff)
			}

			// The three copies as the wrapper sees them: what it checks or hands pi, what the unit folder holds, and
			// whether the two are byte for byte the same file.
			copies := []struct {
				name      string
				installed string
				project   string
				matches   bool
			}{
				{name: "the wrapper", installed: installed.script, project: filepath.Join(project, piTesterScript), matches: piTesterSameCopy(tc.projectWrapper, wrapper.content)},
				{name: "the deny extension", installed: installed.extension, project: filepath.Join(project, denyExtensionFile), matches: piTesterSameCopy(tc.projectExtension, extension.content)},
				{name: "the deny list", installed: installed.list, project: filepath.Join(project, denyListFile), matches: piTesterSameCopy(tc.projectList, list.content)},
			}

			wantPairs := 0

			for _, copy := range copies {
				if !copy.matches {
					wantPairs++
				}
			}

			lines := piTesterOutputLines(stderr)

			if wantPairs == 0 {
				if len(lines) != 0 {
					t.Errorf("the wrapper printed %d line(s) although the project copies are the installed ones, so nothing needs updating (issue #692): %q", len(lines), lines)
				}

				return
			}

			if len(lines) != 1 {
				t.Fatalf("the wrapper printed %d line(s) about the project copies, want exactly one naming the paths and %s (issue #692): %q", len(lines), piTesterUpdateTarget, lines)
			}

			line := lines[0]

			if !strings.Contains(line, "make "+piTesterUpdateTarget) {
				t.Errorf("the wrapper's one line is %q, which does not name the update step make %s: a maintainer reading the pane cannot tell how to update the installed copy (issue #692)", line, piTesterUpdateTarget)
			}

			// One pair per copy that is missing or differs, no more: a line that named a matching copy would send
			// the maintainer to update a file the tester already uses as it is (issue #692, round 2).
			if got := strings.Count(line, "(installed: "); got != wantPairs {
				t.Errorf("the wrapper's one line names %d pair(s) of copies (\"(installed: \" %d time(s)), want %d, one for each project copy that is missing or differs: the maintainer has to see which copies the tester did not take, and only those (issue #692): %q",
					got, got, wantPairs, line)
			}

			for _, copy := range copies {
				named := strings.Contains(line, copy.project)

				switch {
				case copy.matches && named:
					t.Errorf("the wrapper's one line is %q, which names the project copy %s (%s) although it is byte for byte the installed %s: a line that names a copy which matches sends the maintainer to update a file that needs nothing (issue #692): %q",
						line, copy.name, copy.project, copy.installed, line)
				case !copy.matches && !named:
					t.Errorf("the wrapper's one line is %q, which does not name the project copy %s (%s), missing or not byte for byte the installed %s: a maintainer cannot tell which copy the tester did not take (issue #692)",
						line, copy.name, copy.project, copy.installed)
				case !copy.matches && !strings.Contains(line, copy.installed):
					t.Errorf("the wrapper's one line is %q, which does not name the installed file %s (%s): a maintainer cannot tell which copy the tester used (issue #692)",
						line, copy.name, copy.installed)
				}
			}

			// The audit's finding: pi still discovers the project's .pi/extensions beside the installed extension
			// that --approve trusts, so the line may not claim the project's copies are ignored.
			if strings.Contains(strings.ToLower(line), "ignoring") {
				t.Errorf("the wrapper's one line is %q, whose wording says it ignores the project copies: pi still discovers the project's .pi/extensions, so a maintainer must not be told those copies play no part (issue #692, the audit's round 2)", line)
			}

			if !strings.Contains(line, ".pi/extensions") || !strings.Contains(line, "still loads") {
				t.Errorf("the wrapper's one line is %q, which does not say that pi still loads the project's .pi/extensions: the installed extension is loaded beside the project's, never instead of it, so the line has to name both \".pi/extensions\" and \"still loads\" (issue #692, ADR 0044)", line)
			}

			assertNoSecrets(t, "the wrapper's stdout", stdout, piTesterSecrets())
			assertNoSecrets(t, "the wrapper's stderr", stderr, piTesterSecrets())
		})
	}
}

// piTesterOutputLines returns the lines of a stream, without the trailing newline's empty line: one line of
// output is one entry, so a test can count what the wrapper printed.
func piTesterOutputLines(stream string) []string {
	if stream == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(stream, "\n"), "\n")
}

// TestPiTesterRefusesARelativeInvocation pins the other half of the installed layout (issue #692): the wrapper
// finds the refusal's two files, and the project's copies, from $0, so it has to be started by its absolute path.
// Started from its install directory as `sh pi-tester.sh`, or as `sh ./pi-tester.sh`, it stops with a message on
// stderr and never starts pi.
func TestPiTesterRefusesARelativeInvocation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// arg is the script name the shell is given, so the wrapper's $0 is relative either way.
		arg string
	}{
		{name: "by its name, from its install directory", arg: "pi-tester.sh"},
		{name: "by a relative path", arg: "./pi-tester.sh"},
	}

	extension := piFile{present: true, content: readRepoFile(t, denyExtensionFile)}
	list := piFile{present: true, content: readRepoFile(t, denyListFile)}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			binDir := filepath.Join(root, "bin")
			homeDir := filepath.Join(root, "home")
			record := filepath.Join(root, "pi-record")

			for _, dir := range []string{binDir, homeDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			writePiTesterBin(t, binDir, record)

			installed := writePiTesterInstalled(t, root, extension, list)

			// The unit folder holds the project's copies, as the deck's pane has them: the refusal comes from the
			// relative $0 alone, not from a file that differs.
			writePiTesterProject(t, root, extension, list)

			stdout, stderr, err := runPiTester(t, tc.arg, installed.dir, piTesterArgs, piTesterEnv(binDir, homeDir, piTesterBasePath(t)))
			if err == nil {
				t.Fatalf("the wrapper exited 0 when it was started as %q instead of by its absolute path: it then looks for the installed files beside a path the working directory decides, which is the unit folder an agent writes (issue #692)\nstdout: %s\nstderr: %s", tc.arg, stdout, stderr)
			}

			if got := exitStatusOf(t, err); got != 1 {
				t.Errorf("the wrapper exited %d when it was started as %q, want 1: a relative invocation is a refusal with a message, not a crash (issue #692)\nstdout: %s\nstderr: %s", got, tc.arg, stdout, stderr)
			}

			if strings.TrimSpace(stderr) == "" {
				t.Errorf("the wrapper printed nothing on stderr although it was started as %q, so the tester's pane dies without a word a maintainer could act on (issue #662, issue #692)", tc.arg)
			}

			if !strings.Contains(stderr, "absolute") {
				t.Errorf("the wrapper's stderr does not say that it needs its absolute path, so a maintainer reading the pane cannot tell how to start the tester (issue #692): %s", stderr)
			}

			if _, statErr := os.Stat(record); statErr == nil {
				t.Errorf("the wrapper started pi although it was run as %q: the refusal is meant to stop the tester before pi runs, because the installed files it would check are then the working directory's (issue #692)\nstdout: %s\nstderr: %s", tc.arg, stdout, stderr)
			}

			assertNoSecrets(t, "the wrapper's stdout", stdout, piTesterSecrets())
			assertNoSecrets(t, "the wrapper's stderr", stderr, piTesterSecrets())
		})
	}
}

// piTesterStepHereDocuments are the delimiters of the provisioning step's here-documents, each carrying one of the
// tester's files byte for byte: the step's own text is rewritten for a fake root below, and these bodies must not
// be, or the files it installs would no longer be the repository's.
var piTesterStepHereDocuments = []string{"PITESTER", "PIDENYEXT", "PIDENYLIST"}

// limaPiTesterInstallSubshell returns the lines of the provisioning subshell that installs the tester's three
// files: the `(` before the PITESTER here-document, the three here-documents, and the `)` that closes it. It fails
// the test when dev/lima.yaml has no such subshell, since then there is no step whose boot behaviour can be run.
func limaPiTesterInstallSubshell(t *testing.T, template string) []string {
	t.Helper()

	for _, script := range provisionScripts(t, template) {
		for i, line := range script.script {
			if strings.TrimSpace(line) != "(" {
				continue
			}

			for j := i + 1; j < len(script.script); j++ {
				if strings.TrimSpace(script.script[j]) != ")" {
					continue
				}

				block := script.script[i : j+1]
				if strings.Contains(strings.Join(block, "\n"), "/"+piTesterInstallDir) {
					return block
				}

				break
			}
		}
	}

	t.Fatalf("%s has no subshell that installs the tester's files in %s: this test runs that step against a fake root, and a step it cannot find is a step whose boot guard it cannot pin (issue #692)",
		limaTemplateRepoPath, piTesterInstallDir)

	return nil
}

// piTesterStepUnderRoot returns the step as one script, with the destination it installs to rewritten into root. The
// bodies of the three here-documents are left exactly as they are: they are what the step installs, and a test that
// rewrote them would compare a file with itself.
func piTesterStepUnderRoot(t *testing.T, step []string, root string) string {
	t.Helper()

	destination := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(piTesterInstallParent, "/")))

	var out []string

	inside := ""

	for _, line := range step {
		if inside != "" {
			out = append(out, line)

			if strings.TrimSpace(line) == inside {
				inside = ""
			}

			continue
		}

		for _, marker := range piTesterStepHereDocuments {
			if strings.Contains(line, "<<'"+marker+"'") {
				inside = marker

				break
			}
		}

		if inside != "" {
			out = append(out, line)

			continue
		}

		out = append(out, strings.ReplaceAll(line, piTesterInstallParent, destination))
	}

	return strings.Join(out, "\n") + "\n"
}

// piTesterFakeInstall stands in for install when the provisioning step runs as the user who runs the tests: the
// step installs as root during provisioning, and only root can give a file -o root -g root. It drops those options
// and runs the machine's own install with the rest, which applies the mode and creates the files.
const piTesterFakeInstall = `#!/bin/sh
real='%s'
options=""
while [ "$#" -gt 0 ]; do
  case "$1" in
  -o | -g | -u)
    shift
    [ "$#" -gt 0 ] && shift
    ;;
  -m | -t | -T | -S)
    options="${options} $1 $2"
    shift 2
    ;;
  -*)
    options="${options} $1"
    shift
    ;;
  *) break ;;
  esac
done
exec "${real}" ${options} "$@"
`

// TestLimaPiTesterStepLeavesAnInstalledCopyAlone pins the boot guard of issue #692: Lima runs the template's system
// provisioning on every boot, so a step that installs over its destination would replace the copy the maintainer
// put there with make vm-pi-tester each time the VM starts, undoing it. The step's own shell text is run here
// against a fake root, with the destination it names rewritten into that root and a stand-in install that drops the
// ownership options only root can use: the first run installs the three files, and a run over copies the
// maintainer changed leaves them as they are.
func TestLimaPiTesterStepLeavesAnInstalledCopyAlone(t *testing.T) {
	t.Parallel()

	installPath := realTool(t, "install")
	step := limaPiTesterInstallSubshell(t, readRepoFile(t, limaTemplateRepoPath))

	cases := []struct {
		name string
		// installed changes the three files before the run: the copy a maintainer's make vm-pi-tester left, which
		// a boot must not replace.
		installed bool
	}{
		{name: "a root with none of the files yet"},
		{name: "a root where make vm-pi-tester already installed them", installed: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := newSandboxRoot(t)
			tmpDir := filepath.Join(root, "tmp")
			binDir := filepath.Join(root, "bin")

			for _, dir := range []string{tmpDir, binDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
			}

			writeFileMode(t, filepath.Join(binDir, "install"), []byte(fmt.Sprintf(piTesterFakeInstall, installPath)), 0o700)

			env := map[string]string{
				"PATH":   binDir + ":/usr/local/bin:/usr/bin:/bin",
				"HOME":   root,
				"TMPDIR": tmpDir,
			}

			run := func() (string, error) {
				t.Helper()

				cmd := exec.Command("sh", "-s")
				cmd.Env = envList(env)
				cmd.Dir = root
				cmd.Stdin = strings.NewReader(piTesterStepUnderRoot(t, step, root))

				out, err := cmd.CombinedOutput()

				return string(out), err
			}

			installedDir := filepath.Join(root, filepath.FromSlash(piTesterInstallDir))

			files := []struct {
				name   string
				path   string
				mode   os.FileMode
				source string
			}{
				{name: "the wrapper", path: filepath.Join(installedDir, "pi-tester.sh"), mode: 0o755, source: piTesterScript},
				{name: "the deny extension", path: filepath.Join(installedDir, "extensions", "deny-env-printing.ts"), mode: 0o644, source: denyExtensionFile},
				{name: "the deny list", path: filepath.Join(installedDir, "deny-env-printing.json"), mode: 0o644, source: denyListFile},
			}

			changed := "# installed by make vm-pi-tester, as a later change leaves it\n"

			if tc.installed {
				for _, file := range files {
					writeFileMode(t, file.path, []byte(changed), file.mode)
				}
			}

			out, err := run()
			if err != nil {
				t.Fatalf("the provisioning step exited non-zero: Lima would fail the VM's boot\noutput: %s\nerror: %v", out, err)
			}

			for _, file := range files {
				info, err := os.Stat(file.path)
				if err != nil {
					t.Fatalf("the provisioning step installed no %s at %s, so the tester has none of the files it needs after a VM is created (issue #692): %v\noutput: %s", file.name, file.path, err, out)
				}

				got, err := os.ReadFile(file.path)
				if err != nil {
					t.Fatalf("read %s: %v", file.path, err)
				}

				if tc.installed {
					if string(got) != changed {
						t.Errorf("a run of the provisioning step replaced %s at %s with the repository's copy, although a copy the maintainer installed was already there: Lima runs this step on every boot, so installing over the destination undoes make vm-pi-tester each time the VM starts. The step's install commands have to sit behind a guard that tests the destination first, as `[ -e %s ]` or `[ ! -e %s ]` (issue #692, the review's blocker)",
							file.name, file.path, file.path, file.path)
					}

					continue
				}

				if want := readRepoFile(t, file.source); string(got) != want {
					t.Errorf("the provisioning step installed %s as %q, want %s byte for byte: a VM would run a copy that is not the one the tests run (issue #692)", file.name, string(got), file.source)
				}

				if perm := info.Mode().Perm(); perm != file.mode {
					t.Errorf("the provisioning step installed %s as mode %04o, want %04o: the tester runs the wrapper, and reads the two refusal files (issue #692)", file.name, perm, file.mode)
				}
			}
		})
	}
}
