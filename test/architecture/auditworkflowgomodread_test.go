package architecture_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This file runs the audit workflow's read of go.mod's Go, and the checks that
// follow it, as the checks job runs them (issue #710). The step runs under
// set -euo pipefail in GitHub's bash, so a failing go list inside $(...) ends it
// with no message at all, and a read that gives two lines builds govulncheck
// with a value that is no Go version. The fragment is extracted from the
// workflow and run with a fake go command, so the test pins what the step does
// and not a copy of it, and it never reaches the network.

// auditStepScript returns the lines of a step's run: | block, the shell script
// the step runs: the step's YAML keys and its env: entries are no part of it
// (issue #710).
func auditStepScript(t *testing.T, step auditJobStep) []string {
	t.Helper()

	for i, line := range step.lines {
		if strings.TrimSpace(line) != "run: |" {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		var script []string

		for _, rest := range step.lines[i+1:] {
			if strings.TrimSpace(rest) == "" {
				script = append(script, rest)

				continue
			}

			if len(rest)-len(strings.TrimLeft(rest, " \t")) <= indent {
				break
			}

			script = append(script, rest)
		}

		return script
	}

	t.Fatalf("%s's checks job, step %q, runs no run: | block: the step's shell script is what this test reads (issue #710)",
		auditWorkflow, step.name)

	return nil
}

// auditGoModReadFragment returns the step's script from the line that reads
// go.mod's Go to the line before the install that builds with it, which is the
// fragment this file runs (issue #710). It fails the test when the step reads
// go.mod on the install line alone, and when the fragment holds a line that is
// neither the read, a comment, nor part of an if block that fails the run: the
// fragment runs in this test, so it may download and install nothing.
func auditGoModReadFragment(t *testing.T, script []string) []string {
	t.Helper()

	install := -1

	for i, line := range script {
		if strings.Contains(line, "go install") && strings.Contains(line, auditGovulncheckModule+"@") {
			install = i

			break
		}
	}

	if install < 0 {
		t.Fatalf("%s's checks job runs no go install of %s@: this test runs the read that feeds the install (issue #710)",
			auditWorkflow, auditGovulncheckModule)
	}

	read := -1

	for i := install - 1; i >= 0; i-- {
		if _, ok := auditGoModReadAssignment(script[i]); ok {
			read = i

			break
		}
	}

	if read < 0 {
		t.Fatalf("%s's install step reads go.mod's Go on no line of its own above the install: the line %q reads it inline, or not at all. Read go.mod's go line once into a variable there, with GOWORK=off so no go.work of the checkout answers instead, and check the value in an if block of its own - an ::error and an exit 1 - for no value and for more than one line, so the step fails with a message instead of a bare non-zero exit (issue #710)",
			auditWorkflow, strings.TrimSpace(script[install]))
	}

	fragment := script[read:install]

	for _, line := range fragment {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if _, ok := auditGoModReadAssignment(line); ok {
			continue
		}

		if auditFragmentLineIsCheck(trimmed) {
			continue
		}

		// A plain assignment of the fragment's own, such as a line count
		// kept in a variable, runs only its command substitution: it is no
		// download and no install, and the words below keep it that way.
		if auditGoModGoAssignment.MatchString(trimmed) {
			if word := auditFragmentDownload(trimmed); word != "" {
				t.Fatalf(`%s's install step holds %q, which names %q, between its read of go.mod's Go and the install: this test runs that fragment, so no line of it may download, install or build anything (issue #710)`,
					auditWorkflow, trimmed, word)
			}

			continue
		}

		t.Fatalf(`%s's install step holds %q between its read of go.mod's Go and the install: this test runs that fragment, so it may hold the read, the if blocks that check what it read, and the assignments they use, and nothing else (issue #710)`,
			auditWorkflow, trimmed)
	}

	return fragment
}

// auditFragmentDownloads are the words a line of the fragment may not hold: the
// fragment runs under this test, so no line of it may download, install or
// build anything (issue #710).
var auditFragmentDownloads = []string{"curl", "wget", "go install", "go build", "sha256sum", "mkdir", "sudo", "tar ", "install ", "rm ", "cp ", "mv ", "timeout "}

// auditFragmentDownload returns the word of a line that would download, install
// or build something, and the empty string for a line that reads go.mod and
// checks what it read (issue #710).
func auditFragmentDownload(line string) string {
	for _, word := range auditFragmentDownloads {
		if strings.Contains(line, word) {
			return word
		}
	}

	return ""
}

// auditFragmentLineIsCheck reports whether a trimmed shell line is part of an if
// block: the line that opens one, its then, its body of echoes and exits, and
// the fi, elif or else that closes or continues it (issue #710).
func auditFragmentLineIsCheck(trimmed string) bool {
	if auditOpensIf(trimmed) {
		return true
	}

	for _, word := range []string{"then", "fi", "else"} {
		if trimmed == word {
			return true
		}
	}

	for _, word := range []string{"elif ", "exit ", "echo ", "printf "} {
		if strings.HasPrefix(trimmed, word) {
			return true
		}
	}

	return false
}

// auditFakeGoExitOne and the two bodies below are what the fake go command of a
// case prints: a failed read that gives nothing, a read that gives two versions,
// and the one-line read the checks must let through (issue #710).
const (
	auditFakeGoExitOne  = "exit 1"
	auditFakeGoTwoLines = `printf '%s\n' 1.27.4 1.27.4`
	auditFakeGoOneLine  = `printf '%s\n' 1.27.4`
)

// auditWriteFakeGo writes a go command that answers as body says and records the
// GOWORK it saw in gowork, in the directory the case puts first on PATH (issue
// #710). The paths are single-quoted: a subtest's name holds spaces, and a
// t.TempDir() path carries it.
func auditWriteFakeGo(t *testing.T, dir, body string) {
	t.Helper()

	script := "#!/bin/sh\n" +
		"printf '%s' \"${GOWORK-none}\" > '" + filepath.Join(dir, "gowork") + "'\n" +
		body + "\n"

	path := filepath.Join(dir, "go")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write the fake go command into %s: %v", dir, err)
	}

	// A shell script the fragment's PATH finds needs its execute bit.
	// The write itself stays at 0600, which gosec wants, and the mode comes
	// after it.
	if err := os.Chmod(path, os.FileMode(0o755)); err != nil {
		t.Fatalf("make the fake go command %s executable: %v", path, err)
	}
}

// TestAuditGovulncheckGoModReadFailsTheStepClearly runs the checks job's read of
// go.mod's Go as the step runs it, under set -euo pipefail, against a fake go
// command (issue #710). A read that fails must reach the step's ::error and not
// only its exit status, and a read that gives two lines must fail too: the
// install builds govulncheck with GOTOOLCHAIN=go<value>, and a value that holds
// two versions is no toolchain go can fetch. A read that gives one line passes,
// and the go command it ran saw GOWORK=off, so no go.work file of the checkout
// can answer for another module.
func TestAuditGovulncheckGoModReadFailsTheStepClearly(t *testing.T) {
	t.Parallel()

	steps := auditJobSteps(auditJobLines(t, "checks"))

	step, _, _, ok := auditGovulncheckInstall(steps)
	if !ok {
		t.Fatalf("%s's checks job holds no go install of %s@: the step's read of go.mod's Go is the fragment this test runs (issue #710)",
			auditWorkflow, auditGovulncheckModule)
	}

	fragment := auditGoModReadFragment(t, auditStepScript(t, step))

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("find bash on PATH: %v; the checks job's steps run under bash, and this test runs the step's own fragment with it (issue #710)", err)
	}

	for _, tc := range []struct {
		name      string
		fakeGo    string
		wantError bool
		wantFail  bool
	}{
		{
			name:      "a read that fails reaches the ::error, not only a non-zero exit",
			fakeGo:    auditFakeGoExitOne,
			wantError: true,
			wantFail:  true,
		},
		{
			name:      "a read that gives two lines reaches the ::error",
			fakeGo:    auditFakeGoTwoLines,
			wantError: true,
			wantFail:  true,
		},
		{
			name:   "a read that gives one line passes",
			fakeGo: auditFakeGoOneLine,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			auditWriteFakeGo(t, dir, tc.fakeGo)

			cmd := exec.CommandContext(t.Context(), bash, "-c", "set -euo pipefail\n"+strings.Join(fragment, "\n"))
			// Only what the fragment needs: the fake go command's
			// directory first on PATH, the system ones behind it for the
			// shell's own tools such as wc, and a HOME of the case. The
			// developer's environment, its go, its GOFLAGS and its
			// GOTOOLCHAIN reach neither the step nor the fake.
			cmd.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin",
				"HOME=" + t.TempDir(),
			}

			var stdout, stderr bytes.Buffer

			cmd.Stdout, cmd.Stderr = &stdout, &stderr

			err := cmd.Run()

			if tc.wantFail {
				var exitErr *exec.ExitError
				if err == nil {
					t.Errorf("the step's fragment exited 0 when the go command it ran %s: the step must fail the run, with an ::error and an exit 1 (issue #710)", tc.fakeGo)
				} else if !errors.As(err, &exitErr) {
					t.Fatalf("run bash on the step's fragment: %v", err)
				}
			} else if err != nil {
				t.Errorf("the step's fragment failed when the go command it ran printed one line (stdout %q, stderr %q): %v; a read that gives go.mod's one version is what the install holds to, so the checks must let it through (issue #710)",
					stdout.String(), stderr.String(), err)
			}

			got := strings.Contains(stdout.String(), "::error")
			if got != tc.wantError {
				t.Errorf("the step's fragment printed an ::error: %v, want %v (stdout %q, stderr %q): the checks job's step runs under set -euo pipefail, so a read the fragment checks must fail the run with the workflow command that says which read failed, on stdout, and a read that gave one version must print none (issue #710)",
					got, tc.wantError, stdout.String(), stderr.String())
			}

			recorded, readErr := os.ReadFile(filepath.Join(dir, "gowork"))
			if readErr != nil {
				t.Errorf("the fake go command did not run: %v; the fragment must read go.mod's Go with GOWORK=off go list -m -f '{{.GoVersion}}' (issue #710)", readErr)
			} else if string(recorded) != "off" {
				t.Errorf("the go command the read ran saw GOWORK=%q, want %q: with a go.work file in the checkout the go command answers for another module's go.mod, and the install would build govulncheck with a Go go.mod does not name (issue #710)",
					string(recorded), "off")
			}
		})
	}
}
