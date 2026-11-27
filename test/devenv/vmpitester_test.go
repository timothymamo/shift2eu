// Pins the update step for the installed tester wrapper (issue #692): make vm-pi-tester installs the same
// root-owned files into an existing VM, as make vm-gh-wrapper does for the gh wrapper, so a maintainer need not
// rebuild the VM. It is a host target: its recipe runs limactl from the host clone, like vm-gh-wrapper.
package devenv_test

import (
	"slices"
	"strings"
	"testing"
)

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
// the maintainer does not have to rebuild the VM.
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
}
