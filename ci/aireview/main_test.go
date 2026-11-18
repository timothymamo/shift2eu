// White-box tests of the reviewer's entry point: ci/aireview is a main package,
// so its tests are package main, the only way a test file reaches the package at
// all (Go refuses an import of a main package), as cmd/shift2eu-server's are.
//
// run reads its command line from os.Args and writes its one workflow error
// annotation to the process's stderr (fail), and main.go gives neither as a
// parameter: there is no seam to pass them through without changing the entry
// point, which a test may not do. So a test of the argument check sets os.Args
// and replaces os.Stdout and os.Stderr for the one call and restores them, as
// os does in its own tests; the package holds this one test, so no parallel test
// shares the process while it runs.
package main

import (
	"io"
	"os"
	"testing"
)

// aiReviewUsageAnnotation is the whole of what a command line the entry point
// refuses prints: the usage line of the one command line it accepts, as the
// workflow error annotation fail writes it. The message is the usage line rather
// than a per-argument one because the check is a single condition, and it names
// the accepted shape: the subcommand then the absolute path of the Claude Code
// CLI (ADR 0033). Exactly this and nothing else also pins that no later step ran:
// a settings, instruction, GitHub or model failure would print its own message.
const aiReviewUsageAnnotation = "::error title=AI review failed::usage: aireview <absolute path of the claude CLI>\n"

// TestRunRefusesEveryCommandLineButOneAbsoluteCLIPath pins the entry point's
// argument check: it accepts exactly one argument, the absolute path of the
// Claude Code CLI the workflow installed and verified, and refuses no argument,
// two arguments, an empty argument and a relative path. A refusal exits 1, posts
// nothing and prints the usage annotation, before any setting is read and so
// before anything reaches GitHub or the model.
func TestRunRefusesEveryCommandLineButOneAbsoluteCLIPath(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no argument", args: []string{"aireview"}},
		{name: "two arguments", args: []string{"aireview", "/nonexistent/claude", "/nonexistent/other"}},
		{name: "an empty argument", args: []string{"aireview", ""}},
		{name: "a relative path", args: []string{"aireview", "claude"}},
	}

	for _, tc := range tests {
		// No t.Parallel: the subtests set os.Args, os.Stdout and os.Stderr for
		// the process while run executes, and a parallel test would see them.
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runWithCommandLine(t, tc.args)

			if code != 1 {
				t.Errorf("exit code %d, want 1 for the command line %v: the entry point exits 0 after posting a review and 1 on any failure", code, tc.args)
			}

			if stdout != "" {
				t.Errorf("stdout is %q, want nothing for the command line %v: the check runs before the reviewer posts anything", stdout, tc.args)
			}

			if stderr != aiReviewUsageAnnotation {
				t.Errorf("stderr is %q for the command line %v, want %q: the refusal names the accepted command line, and no settings, GitHub or model failure ran", stderr, tc.args, aiReviewUsageAnnotation)
			}
		})
	}
}

// runWithCommandLine calls run with args as the command line, and returns its
// exit code and what it wrote to stdout and stderr. It sets os.Args and replaces
// os.Stdout and os.Stderr for the call, and restores them before it returns, so
// a failure of one subtest leaves the next the process it expects.
func runWithCommandLine(t *testing.T, args []string) (code int, stdout, stderr string) {
	t.Helper()

	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("make the pipe run's stdout is read through: %v", err)
	}

	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("make the pipe run's stderr is read through: %v", err)
	}

	savedArgs, savedStdout, savedStderr := os.Args, os.Stdout, os.Stderr

	t.Cleanup(func() {
		os.Args, os.Stdout, os.Stderr = savedArgs, savedStdout, savedStderr
	})

	os.Args = args
	os.Stdout = stdoutWrite
	os.Stderr = stderrWrite

	code = run()

	os.Args, os.Stdout, os.Stderr = savedArgs, savedStdout, savedStderr

	stdoutWrite.Close()
	stderrWrite.Close()

	// The two reads come after the writes are closed, so a pipe whose buffer
	// filled has drained: the entry point writes one short annotation at most.
	return code, readPipe(t, stdoutRead, "stdout"), readPipe(t, stderrRead, "stderr")
}

// readPipe reads everything r holds and closes it.
func readPipe(t *testing.T, r *os.File, what string) string {
	t.Helper()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read run's %s: %v", what, err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("close run's %s pipe: %v", what, err)
	}

	return string(data)
}
