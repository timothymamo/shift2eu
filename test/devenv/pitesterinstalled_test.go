// Pins the installed layout of the tester's launch wrapper (issue #692): the VM installs root-owned copies of
// dev/pi-tester.sh and the env-printing refusal's two files from dev/lima.yaml, outside every path an agent can
// write, and .dot-agent-deck.toml's tester command runs the installed wrapper. The wrapper reads the refusal
// from its own installation directory, never from the unit folder, so a branch cannot weaken what the tester
// checks; a project copy that differs is ignored with one line naming both paths and the update step.
package devenv_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The files the VM installs and the paths it installs them to (issue #692), outside every path an agent can
// write. piTesterInstallDir is relative to a test's fake root; the rest are the absolute paths the VM uses.
const (
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

// TestPiTesterIgnoresTheProjectsCopies pins the review point the installed layout exists for (issue #692): the
// wrapper checks the installed denial and hands pi the installed extension, so a weakened or missing copy in the
// unit folder changes neither what is checked nor whether the tester starts. It says so in one line naming both
// paths and the update step, and says nothing when the two copies agree.
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
		wantLine         bool
		// wantInstalled and wantProject name the file the one line must name, for the cases where exactly one
		// project copy differs. Both are nil for the case whose copies are all missing: the line may name any of
		// the three, and only its shape is pinned there.
		wantInstalled func(piTesterInstalled) string
		wantProject   func(project string) string
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
			wantLine:         true,
			wantInstalled:    func(installed piTesterInstalled) string { return installed.list },
			wantProject:      func(project string) string { return filepath.Join(project, denyListFile) },
		},
		{
			name:             "the project extension is empty",
			projectExtension: piFile{present: true},
			projectList:      list,
			projectWrapper:   wrapper,
			wantLine:         true,
			wantInstalled:    func(installed piTesterInstalled) string { return installed.extension },
			wantProject:      func(project string) string { return filepath.Join(project, denyExtensionFile) },
		},
		{
			name:             "the project wrapper is missing",
			projectExtension: extension,
			projectList:      list,
			projectWrapper:   piFile{},
			wantLine:         true,
			wantInstalled:    func(installed piTesterInstalled) string { return installed.script },
			wantProject:      func(project string) string { return filepath.Join(project, piTesterScript) },
		},
		{
			name:          "the project holds none of the copies",
			wantLine:      true,
			wantInstalled: func(installed piTesterInstalled) string { return installed.dir },
			wantProject:   func(project string) string { return project },
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

			lines := piTesterOutputLines(stderr)

			if !tc.wantLine {
				if len(lines) != 0 {
					t.Errorf("the wrapper printed %d line(s) although the project copies are the installed ones, so nothing needs updating (issue #692): %q", len(lines), lines)
				}

				return
			}

			if len(lines) != 1 {
				t.Fatalf("the wrapper printed %d line(s) about the ignored project copies, want exactly one naming both paths and %s (issue #692): %q", len(lines), piTesterUpdateTarget, lines)
			}

			line := lines[0]

			if !strings.Contains(line, "make "+piTesterUpdateTarget) {
				t.Errorf("the wrapper's one line is %q, which does not name the update step make %s: a maintainer reading the pane cannot tell how to update the installed copy (issue #692)", line, piTesterUpdateTarget)
			}

			if want := tc.wantInstalled(installed); !strings.Contains(line, want) {
				t.Errorf("the wrapper's one line is %q, which does not name the installed file %s: a maintainer cannot tell which copy the tester used (issue #692)", line, want)
			}

			if want := tc.wantProject(project); !strings.Contains(line, want) {
				t.Errorf("the wrapper's one line is %q, which does not name the project copy %s: a maintainer cannot tell which copy was ignored (issue #692)", line, want)
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
