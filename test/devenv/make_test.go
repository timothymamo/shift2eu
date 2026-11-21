// Names the make every test of the package runs, and checks the one CI's make 3.81 job runs them with (issue
// #314): the maintainer's Mac runs the host targets with /usr/bin/make, GNU Make 3.81, and the VM and CI with
// make 4.x, which reads some recipes differently (issue #437).
package devenv_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testMakeEnv names the make the tests run, as a path or a name looked up on PATH; unset or empty, they run
// make. CI's make 3.81 job sets it to /usr/bin/make on macOS.
const testMakeEnv = "SHIFT2EU_TEST_MAKE"

// testMakeVersionEnv is the GNU Make version the make testMakeEnv names must report. Unset or empty, the tests
// assert nothing about it; CI's make 3.81 job sets it to 3.81, so a runner whose make is another release fails
// TestTestMakeIsTheVersionNamed instead of passing the suite under the wrong make.
const testMakeVersionEnv = "SHIFT2EU_TEST_MAKE_VERSION"

// makeProgram returns the make the tests run: the one SHIFT2EU_TEST_MAKE names, or make. It fails the test when
// that make is not installed.
func makeProgram(t *testing.T) string {
	t.Helper()

	program := os.Getenv(testMakeEnv)
	if program == "" {
		program = "make"
	}

	path, err := exec.LookPath(program)
	if err != nil {
		t.Fatalf("make is not installed (%s=%q): the repository's commands, and this test, need it: %v", testMakeEnv, program, err)
	}

	return path
}

// skipUnderMake381OnMacOS skips t when it runs in CI's make 3.81 job: on macOS, with SHIFT2EU_TEST_MAKE_VERSION
// set to 3.81. It holds apart a known failure of the Makefile or a host script on the Mac, filed as an issue that
// reason names, so the job can pass until that issue's fix removes the skip. Everywhere else the test runs.
func skipUnderMake381OnMacOS(t *testing.T, reason string) {
	t.Helper()

	if runtime.GOOS == "darwin" && os.Getenv(testMakeVersionEnv) == "3.81" {
		t.Skipf("skipped under make 3.81 on macOS: %s", reason)
	}
}

// TestTestMakeIsTheVersionNamed checks that the make the tests run is the GNU Make release
// SHIFT2EU_TEST_MAKE_VERSION names, and that under 3.81 it reads a recipe as 3.81 does: a # line inside a
// backslash-continued recipe comments out the rest of it (issue #437). That is what makes a passing run of CI's
// make 3.81 job mean the targets work under the Mac's make, and a 3.81-only regression fail it. Without the
// variable the test skips: the default run, under make 4.x, asserts nothing about 3.81.
func TestTestMakeIsTheVersionNamed(t *testing.T) {
	version := os.Getenv(testMakeVersionEnv)
	if version == "" {
		t.Skipf("%s is unset: only CI's make 3.81 job names the GNU Make release the tests must run under", testMakeVersionEnv)
	}

	program := makeProgram(t)

	out, err := exec.Command(program, "--version").Output()
	if err != nil {
		t.Fatalf("%s --version: %v", program, err)
	}

	first, _, _ := strings.Cut(string(out), "\n")
	first = strings.TrimSpace(first)
	t.Logf("the tests run %s, which reports %q", program, first)

	if want := "GNU Make " + version; first != want {
		t.Fatalf("%s --version reports %q, want %q (%s): the tests would not run under the make the job names", program, first, want, testMakeVersionEnv)
	}

	if version != "3.81" {
		return
	}

	// Issue #437's reproduction: make 3.81 prints only "before", make 4.x "before" and "after".
	dir := t.TempDir()
	makefile := filepath.Join(dir, "comment.mk")

	if err := os.WriteFile(makefile, []byte("t:\n\t@echo before; \\\n\t# a comment \\\n\techo after\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", makefile, err)
	}

	cmd := exec.Command(program, "-f", makefile)
	cmd.Dir = dir
	// Only PATH, on purpose: a parent make's MAKEFLAGS and MAKELEVEL stay out, so the canary sees the
	// program's own behaviour.
	cmd.Env = envList(map[string]string{"PATH": os.Getenv("PATH")})

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("%s -f %s: %v\nstdout: %s\nstderr: %s", program, makefile, err, stdout.String(), stderr.String())
	}

	if got := stdout.String(); got != "before\n" {
		t.Errorf("%s ran issue #437's recipe and printed %q, want only %q: make 3.81 ends a recipe at a # line inside a continued command, so this make does not read recipes as the Mac's /usr/bin/make does", program, got, "before\n")
	}
}
