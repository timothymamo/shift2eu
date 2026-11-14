// Pins that the gh wrapper the VM runs is the wrapper these tests run (PRD #161 milestone 1, ADR 0031).
// dev/lima.yaml embeds dev/gh-app-token.sh in its system provision script as a heredoc and installs that copy
// root-owned, so the VM's gh never comes from the workspace mount an agent can write. The embedded text is
// compared byte for byte with the file, so the two copies cannot drift, and the git credential helper is
// pinned to the path the wrapper is installed at.
package devenv_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// limaTemplateRepoPath is dev/lima.yaml relative to the repository root, as readRepoFile wants it.
const limaTemplateRepoPath = "dev/lima.yaml"

// ghWrapperMarker is the heredoc marker dev/lima.yaml wraps its copy of the wrapper in.
const ghWrapperMarker = "GHWRAPPER"

// limaScriptHeader matches a provision script's block scalar header, such as "    script: |".
var limaScriptHeader = regexp.MustCompile(`^ *script: *[|][-+]? *$`)

// shellSingleQuoted matches one shell single-quoted string, which is how dev/lima.yaml passes the value of a
// git config key.
var shellSingleQuoted = regexp.MustCompile(`'([^']*)'`)

// limaBlockContent returns the lines of the block scalar whose header is lines[header], dedented the way YAML
// dedents it: the first non-empty line after the header sets the indentation, a blank line stays empty, and
// the block ends at the first non-empty line indented no deeper than the header. The result is the text the
// provision script's shell reads.
func limaBlockContent(t *testing.T, lines []string, header int) []string {
	t.Helper()

	headerIndent := len(lines[header]) - len(strings.TrimLeft(lines[header], " "))
	indent := -1

	var content []string

	for _, line := range lines[header+1:] {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" {
			content = append(content, "")

			continue
		}

		lineIndent := len(line) - len(trimmed)
		if indent < 0 {
			if lineIndent <= headerIndent {
				t.Fatalf("%s line %d is a block scalar header with no content under it", limaTemplateRepoPath, header+1)
			}

			indent = lineIndent
		}

		if lineIndent < indent {
			break
		}

		content = append(content, line[indent:])
	}

	return content
}

// embeddedGHWrapper returns the text dev/lima.yaml hands the VM as its gh wrapper: the lines between the
// GHWRAPPER marker lines of the heredoc, dedented the way the provision script's YAML block scalar dedents
// them.
func embeddedGHWrapper(t *testing.T, template string) string {
	t.Helper()

	lines := strings.Split(template, "\n")

	opener := -1

	for i, line := range lines {
		if !strings.Contains(line, "<<'"+ghWrapperMarker+"'") {
			continue
		}

		if opener >= 0 {
			t.Fatalf("%s has more than one <<'%s' heredoc, so this test cannot tell which copy the VM installs", limaTemplateRepoPath, ghWrapperMarker)
		}

		opener = i
	}

	if opener < 0 {
		t.Fatalf("%s has no <<'%s' heredoc: the VM's gh must be installed from the template, not from the workspace mount an agent can write (PRD #161 milestone 1, ADR 0031)", limaTemplateRepoPath, ghWrapperMarker)
	}

	header := -1

	for i := opener - 1; i >= 0; i-- {
		if limaScriptHeader.MatchString(lines[i]) {
			header = i

			break
		}
	}

	if header < 0 {
		t.Fatalf("%s's <<'%s' heredoc is not inside a provision script's `script: |` block, so it is not text the VM runs", limaTemplateRepoPath, ghWrapperMarker)
	}

	script := limaBlockContent(t, lines, header)

	start := -1

	for i, line := range script {
		if strings.Contains(line, "<<'"+ghWrapperMarker+"'") {
			start = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s's <<'%s' heredoc is not in the provision script's block scalar", limaTemplateRepoPath, ghWrapperMarker)
	}

	end := -1

	for i := start + 1; i < len(script); i++ {
		if script[i] == ghWrapperMarker {
			end = i

			break
		}
	}

	if end < 0 {
		t.Fatalf("%s has no line that is exactly %s after the heredoc opener, once the provision script's indentation is removed: the shell then never ends the heredoc and the rest of the template is the wrapper the VM installs", limaTemplateRepoPath, ghWrapperMarker)
	}

	return strings.Join(script[start+1:end], "\n") + "\n"
}

// onlyLine returns the one line of lines that contains substr, failing the test when there is none or more
// than one.
func onlyLine(t *testing.T, lines []string, substr string) string {
	t.Helper()

	var found []string

	for _, line := range lines {
		if strings.Contains(line, substr) {
			found = append(found, strings.TrimSpace(line))
		}
	}

	if len(found) != 1 {
		t.Fatalf("%s has %d lines containing %q, want exactly one: the wrapper's installation and the git credential helper that runs it are pinned here", limaTemplateRepoPath, len(found), substr)
	}

	return found[0]
}

// pinnedRealGH is the path the installed wrapper must pin the real gh to. The wrapper the VM runs reads no
// SHIFT2EU_REAL_GH, so an agent that sets it in its own shell cannot make the agents' token go to a binary of
// its choice (PRD #161 milestone 1 review, item 6).
const pinnedRealGH = "/usr/local/libexec/shift2eu/gh"

// ghPinTransform returns the sed script dev/lima.yaml pipes the wrapper through to pin the real gh's path.
// The template's own transform is what the tests apply, so they pin the text the VM installs; the transform
// must be a single sed script on the one line of the template that names ^real=, which is how this test finds
// it (review item 6).
func ghPinTransform(t *testing.T, template string) string {
	t.Helper()

	line := onlyLine(t, strings.Split(template, "\n"), "^real=")

	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`\bsed\s+(?:-e\s+)?'([^']+)'`),
		regexp.MustCompile(`\bsed\s+(?:-e\s+)?"([^"]+)"`),
	} {
		if match := pattern.FindStringSubmatch(line); match != nil {
			return match[1]
		}
	}

	t.Fatalf("%s's wrapper install names ^real= without a sed script this test can read: %s. Pin the real gh with one sed script (or that declares the transform here), so the installed copy can be checked (review item 6)", limaTemplateRepoPath, line)

	return ""
}

// applySed runs one sed script over text, the way a provision script pipes the wrapper through it.
func applySed(t *testing.T, script, text string) string {
	t.Helper()

	cmd := exec.Command("sed", "-e", script)
	cmd.Stdin = strings.NewReader(text)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("run sed %q: %v\nstderr: %s", script, err, stderr.String())
	}

	return stdout.String()
}

// assertPinnedRealGH fails when text does not pin the real gh's path in one assignment that reads nothing from
// the environment.
func assertPinnedRealGH(t *testing.T, where, text string) {
	t.Helper()

	var assignments []string

	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "real=") {
			assignments = append(assignments, trimmed)
		}
	}

	if len(assignments) != 1 {
		t.Fatalf("%s has %d lines that assign real, want exactly one: the installed wrapper pins the real gh's path (review item 6): %v", where, len(assignments), assignments)
	}

	if got := assignments[0]; strings.Contains(got, "${") || !strings.Contains(got, pinnedRealGH) {
		t.Errorf("%s assigns %q, want real=%s: the installed wrapper must pin the path, so SHIFT2EU_REAL_GH cannot point the agents' token at another binary (review item 6)", where, got, pinnedRealGH)
	}
}

// wrapperBlock returns the standard input of the call in a captured limactl log that carried the gh wrapper.
func wrapperBlock(t *testing.T, capture, where string) string {
	t.Helper()

	for _, block := range strings.Split(capture, "\n--- end of call ---\n") {
		if strings.Contains(block, "GH_TOKEN=") && strings.Contains(block, `exec "${real}"`) {
			return block
		}
	}

	t.Fatalf("%s handed limactl no gh wrapper on standard input, so the VM installs nothing; the capture holds:\n%s", where, capture)

	return ""
}

// TestTheInstalledWrapperPinsTheRealGH pins review item 6: the copy of dev/gh-app-token.sh the VM installs no
// longer reads SHIFT2EU_REAL_GH, so an agent cannot point the maintainer's installation token at a gh it wrote.
func TestTheInstalledWrapperPinsTheRealGH(t *testing.T) {
	t.Parallel()

	template := readRepoFile(t, limaTemplateRepoPath)
	installed := applySed(t, ghPinTransform(t, template), embeddedGHWrapper(t, template))

	assertPinnedRealGH(t, limaTemplateRepoPath+"'s transform of the wrapper", installed)

	sb := newSandbox(t)
	marker := filepath.Join(sb.root, "tripwire-ran")
	tripwire := filepath.Join(sb.root, "real-gh-tripwire")
	script := filepath.Join(sb.root, "installed-gh")

	writeFileMode(t, tripwire, []byte("#!/bin/sh\n: > \"${SHIFT2EU_TEST_TRIPWIRE:?}\"\nexit 97\n"), 0o700)
	writeFileMode(t, script, []byte(installed), 0o700)

	env := sb.baseEnv()
	env["HOME"] = sb.vmHome // and no token file: the wrapper runs the real gh
	env["SHIFT2EU_REAL_GH"] = tripwire
	env["SHIFT2EU_TEST_TRIPWIRE"] = marker

	stdout, stderr, _ := sb.run(t, script, []string{"--version"}, env, "")

	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the installed wrapper ran the gh SHIFT2EU_REAL_GH names: an agent that sets it makes the agents' App token go to a binary of its choice (review item 6)\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestLimaTemplateInstallsTheGHWrapper(t *testing.T) {
	t.Parallel()

	template := readRepoFile(t, limaTemplateRepoPath)

	t.Run("the embedded copy is dev/gh-app-token.sh", func(t *testing.T) {
		t.Parallel()

		embedded := embeddedGHWrapper(t, template)
		want := readRepoFile(t, ghWrapperScript)

		if diff := cmp.Diff(strings.Split(want, "\n"), strings.Split(embedded, "\n")); diff != "" {
			t.Errorf("%s's %s heredoc and %s are not the same text. The VM installs the template's copy, which no agent can write, and these tests run the file: change both together (-want +got):\n%s",
				limaTemplateRepoPath, ghWrapperMarker, ghWrapperScript, diff)
		}
	})

	t.Run("the git credential helper runs the installed wrapper", func(t *testing.T) {
		t.Parallel()

		lines := strings.Split(template, "\n")

		install := strings.Fields(onlyLine(t, lines, `install -o root -g root -m 0755 "${tmp}"`))
		if len(install) == 0 {
			t.Fatalf("%s's wrapper installation names no path", limaTemplateRepoPath)
		}

		installedPath := install[len(install)-1]

		helper := onlyLine(t, lines, "credential.https://github.com.helper")

		quoted := shellSingleQuoted.FindAllStringSubmatch(helper, -1)
		if len(quoted) != 1 {
			t.Fatalf("%s's git credential helper is not exactly one single-quoted value: %s", limaTemplateRepoPath, helper)
		}

		want := "!" + installedPath + " auth git-credential"
		if got := quoted[0][1]; got != want {
			t.Errorf("%s's git credential helper is %q, want %q: it must run the wrapper at the path dev/lima.yaml installs it to, not a gh found on PATH and not the real gh that `gh auth setup-git` would name (PRD #161 milestone 1, ADR 0031)", limaTemplateRepoPath, got, want)
		}
	})
}
