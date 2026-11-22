// Pins that the Lima VM's readiness probe never runs the gh wrapper (issue #479). The probe runs while the VM
// is being created, before make agents-token has written the agents App's token, and the wrapper refuses
// without it (PRD #161 milestone 3). A probe that ran the wrapper failed every time, so limactl start waited
// its 600 seconds and make vm-create failed on every new VM.
package devenv_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// limaProbeHeader matches the header of the probe's script in dev/lima.yaml, "  - script: |", the only
// script that is a list item of probes:.
var limaProbeHeader = regexp.MustCompile(`^ *- *script: *[|][-+]? *$`)

// shellCommandSeparator splits a shell line into its simple commands, near enough for the probe's lines.
var shellCommandSeparator = regexp.MustCompile(`&&|\|\||[;|&]|\$\(|` + "`")

// limaProbeScript returns the lines of dev/lima.yaml's probe script, dedented the way YAML dedents them.
func limaProbeScript(t *testing.T, template string) []string {
	t.Helper()

	lines := strings.Split(template, "\n")

	probes := slices.Index(lines, "probes:")
	if probes < 0 {
		t.Fatalf("%s has no top-level probes: block, so nothing tells limactl start when the VM is ready", limaTemplateRepoPath)
	}

	for i := probes + 1; i < len(lines); i++ {
		if limaProbeHeader.MatchString(lines[i]) {
			return limaBlockContent(t, lines, i)
		}

		if lines[i] != "" && !strings.HasPrefix(lines[i], " ") {
			break
		}
	}

	t.Fatalf("%s's probes: block holds no `- script: |` item", limaTemplateRepoPath)

	return nil
}

// ghWrapperInstallPath returns the path dev/lima.yaml installs the gh wrapper at.
func ghWrapperInstallPath(t *testing.T, template string) string {
	t.Helper()

	install := strings.Fields(onlyLine(t, strings.Split(template, "\n"), `install -o root -g root -m 0755 "${tmp}"`))
	if len(install) == 0 {
		t.Fatalf("%s's wrapper installation names no path", limaTemplateRepoPath)
	}

	return install[len(install)-1]
}

// commandWords returns the first word and the rest of every simple command on a shell line, comments dropped.
func commandWords(line string) [][]string {
	if hash := strings.Index(line, "#"); hash >= 0 {
		line = line[:hash]
	}

	var commands [][]string

	for _, part := range shellCommandSeparator.Split(line, -1) {
		words := strings.Fields(part)
		for len(words) > 0 && (words[0] == "!" || words[0] == "{" || words[0] == "(" || words[0] == "then" || words[0] == "do") {
			words = words[1:]
		}

		if len(words) > 0 {
			commands = append(commands, words)
		}
	}

	return commands
}

func TestLimaProbeNeverRunsTheGHWrapper(t *testing.T) {
	t.Parallel()

	template := readRepoFile(t, limaTemplateRepoPath)
	probe := limaProbeScript(t, template)
	wrapper := ghWrapperInstallPath(t, template)

	t.Run("no command of the probe runs the wrapper", func(t *testing.T) {
		t.Parallel()

		for _, line := range probe {
			for _, words := range commandWords(line) {
				if words[0] == wrapper || words[0] == "gh" {
					t.Errorf("%s's probe runs %q: %q. That is the agents App wrapper, which refuses without the App's token, and make agents-token writes it only after the VM exists, so the probe never passes and make vm-create times out (issue #479). Run %s instead",
						limaTemplateRepoPath, words[0], strings.TrimSpace(line), pinnedRealGH)
				}
			}
		}
	})

	// The release check (release-tag skill) needs gh's attestation subcommand: the probe checks it on the real gh.
	t.Run("the probe checks the real gh's attestation subcommand", func(t *testing.T) {
		t.Parallel()

		want := []string{pinnedRealGH, "attestation", "--help"}

		for _, line := range probe {
			for _, words := range commandWords(line) {
				if len(words) >= len(want) && slices.Equal(words[:len(want)], want) {
					return
				}
			}
		}

		t.Errorf("%s's probe never runs %s: the release check calls the real gh's attestation subcommand by absolute path",
			limaTemplateRepoPath, strings.Join(want, " "))
	})

	// The wrapper is still checked, without running it: installed, executable, and parsed by sh.
	t.Run("the probe checks the wrapper without running it", func(t *testing.T) {
		t.Parallel()

		for _, want := range [][]string{{"test", "-x", wrapper}, {"sh", "-n", wrapper}} {
			found := false

			for _, line := range probe {
				for _, words := range commandWords(line) {
					if slices.Equal(words, want) {
						found = true
					}
				}
			}

			if !found {
				t.Errorf("%s's probe has no line that is exactly %q, so a VM whose gh wrapper is missing or broken would pass it",
					limaTemplateRepoPath, strings.Join(want, " "))
			}
		}
	})
}
