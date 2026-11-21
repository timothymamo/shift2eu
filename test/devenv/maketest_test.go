// Pins how make test reports a failing run (issue #253): go's own output stays whole, and a failing run ends with a
// summary that names every failing package and test, so the next unexplained FAIL - the one PRs #239 and #250 saw
// and could not trace - names its package and test in the log's last lines. A run whose exit status is lost, and a
// test that times out and so never prints a --- FAIL: line, fail and name their test too.
//
// Every case runs the Makefile's test recipe against a fake go first on PATH: no case starts the real test suite.
package devenv_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// testSummaryHeader is the line a failing make test prints before its summary, exactly (issue #253).
const testSummaryHeader = "make test: failing packages and tests:"

// goTestArgs are the arguments make test must keep handing go: the whole module, with the race detector, in a run
// that caches nothing (Makefile, make test).
var goTestArgs = []string{"test", "-race", "-count=1", "./..."}

// failingGoTestOutput is what go test prints when one test in one package fails and the other packages pass: the
// failing test's line and its detail, the bare FAIL go prints before the per-package lines, the failing package's
// line, a passing package after it and the trailing FAIL. The tabs are go's own field separators (issue #253).
const failingGoTestOutput = "ok  \texample/pkg/a\t0.1s\n" +
	"--- FAIL: TestX (0.00s)\n" +
	"    x_test.go:10: boom\n" +
	"FAIL\n" +
	"FAIL\texample/pkg/b\t0.2s\n" +
	"ok  \texample/pkg/c\t0.3s\n" +
	"FAIL\n"

// timeoutGoTestOutput is what go test prints when a test does not finish within -timeout: the panic line, the
// tests that were running when it struck, indented one tab deeper than the block's own indentation, an empty line,
// the stack of the alarm the timeout raised and the failing package's line. No --- FAIL: line names the hung test,
// so only the running tests' block and the package line do (issue #253).
const timeoutGoTestOutput = "panic: test timed out after 1s\n" +
	"\trunning tests:\n" +
	"\t\tTestSlow (1s)\n" +
	"\n" +
	"goroutine 5 [running]:\n" +
	"testing.(*M).startAlarm.func1()\n" +
	"\t/usr/local/go/src/testing/testing.go:2484 +0x30c\n" +
	"created by time.goFunc\n" +
	"\t/usr/local/go/src/time/sleep.go:215 +0x38\n" +
	"FAIL\texample/pkg/e\t1.010s\n"

// fakeGoRecordsTmpDir is what every fake go below does before it does anything else: it writes the entries the
// recipe's temporary directory left in TMPDIR into the file FAKE_GO_TMPDIR names. A case reads that file back to
// know that make test made its directory under the TMPDIR the case gave it, so the case's check that the TMPDIR is
// empty afterwards pins the recipe's cleanup and not a directory that was never there (issue #253).
const fakeGoRecordsTmpDir = `ls -A "$TMPDIR" > "$FAKE_GO_TMPDIR"
`

// fakeGoScript is the fake go the cases below put first on PATH: it appends its arguments to the file FAKE_GO_ARGV
// names, prints the canned output of FAKE_GO_OUTPUT and exits with FAKE_GO_EXIT. The Makefile runs go in recipes
// only, never at parse time, so the fake answers every call make test makes (issue #253).
const fakeGoScript = `#!/bin/sh
printf '%s\n' "$@" >> "$FAKE_GO_ARGV"
` + fakeGoRecordsTmpDir + `cat "$FAKE_GO_OUTPUT"
exit "$FAKE_GO_EXIT"
`

// killingGoScript is a fake go that loses the exit status the recipe reads before that status exists: it prints
// go's output and then kills the subshell make runs it in, which is its parent ($PPID), before the subshell writes
// the status file. KILL catches no trap, and only that subshell dies: not make, not this process, so the recipe
// shell goes on with a status file that stayed empty (issue #253).
const killingGoScript = `#!/bin/sh
printf '%s\n' "$@" >> "$FAKE_GO_ARGV"
` + fakeGoRecordsTmpDir + `cat "$FAKE_GO_OUTPUT"
kill -KILL "$PPID"
`

// termingGoScript is a fake go that interrupts the run while go still runs: it prints go's output and then sends
// TERM to the recipe's shell, the signal make test traps to exit 143 on. That shell is not $PPID - which is the
// subshell the pipeline element `{ go; echo $? > status; }` runs in - but that subshell's parent, so the fake reads
// the shell's pid from ps (issue #253).
const termingGoScript = `#!/bin/sh
printf '%s\n' "$@" >> "$FAKE_GO_ARGV"
` + fakeGoRecordsTmpDir + `cat "$FAKE_GO_OUTPUT"
kill -TERM "$(ps -o ppid= -p "$PPID" | tr -d ' ')"
exit "$FAKE_GO_EXIT"
`

// TestMakeTestReportsTheFailingPackageAndTest pins what make test prints when it fails (issue #253): a failing run
// still prints go's own output whole and ends with a summary that repeats the failing packages' and tests' lines
// under the header testSummaryHeader, in go's order and without the bare FAIL lines; a passing run prints nothing
// of its own after go's output.
func TestMakeTestReportsTheFailingPackageAndTest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		output     string
		exitCode   int
		wantFailed bool
		// script overrides the fake go's script, for a go that does more than print and exit; empty means
		// fakeGoScript.
		script string
		// wantTail is every line make test prints after go's own output, the summary's header included: an empty
		// slice pins that go's output is the whole of it, a header on its own pins that the summary is printed
		// even when go names no package and no test, and nil pins nothing of the tail.
		wantTail []string
		// wantNoStderr is a text make test must not print on stderr, empty for none: a status that is not a
		// number - the status file stayed empty because the shell running go died before it wrote one - is
		// still a failure the run reports itself, so the summary must not come as the shell's error about a
		// file it cannot read instead of, or beside, the summary callers read. The recipe creates that file
		// empty before go runs, so the text of the older design, the shell reading a file nothing created,
		// must not come back (issue #253).
		wantNoStderr string
		// wantStderr is a text make test must print on stderr, empty for none: an interrupted run ends in
		// make's own report of the status the recipe exited with, so a run that dies never looks like a pass
		// (issue #253).
		wantStderr string
	}{
		{
			name:       "a failing test",
			output:     failingGoTestOutput,
			exitCode:   1,
			wantFailed: true,
			wantTail: []string{
				testSummaryHeader,
				"--- FAIL: TestX (0.00s)",
				"FAIL\texample/pkg/b\t0.2s",
			},
		},
		{
			// go prints a failing subtest as its parent's line, then the subtest's own line indented by four
			// spaces, then its detail line indented once more; only the two FAIL lines name a test (issue #253).
			name: "a failing subtest",
			output: "--- FAIL: TestY (0.00s)\n" +
				"    --- FAIL: TestY/sub (0.00s)\n" +
				"        y_test.go:5: boom\n" +
				"FAIL\n" +
				"FAIL\texample/pkg/d\t0.1s\n",
			exitCode:   1,
			wantFailed: true,
			wantTail: []string{
				testSummaryHeader,
				"--- FAIL: TestY (0.00s)",
				"    --- FAIL: TestY/sub (0.00s)",
				"FAIL\texample/pkg/d\t0.1s",
			},
		},
		{
			name:       "a package that does not build",
			output:     "FAIL\texample/pkg/c [build failed]\n",
			exitCode:   1,
			wantFailed: true,
			wantTail:   []string{testSummaryHeader, "FAIL\texample/pkg/c [build failed]"},
		},
		{
			name:       "a passing run",
			output:     "ok  \texample/pkg/a\t0.1s\nok  \texample/pkg/b\t0.2s\n",
			exitCode:   0,
			wantFailed: false,
			wantTail:   []string{},
		},
		{
			// Nothing matches the summary's patterns here, so the header alone is the tail: make test still ends
			// with it, says nothing after it, and keeps go's failure (issue #253).
			name:       "a go that fails without a FAIL line",
			output:     "go: downloading example.com/dep v1.0.0\nfatal error: the go command crashed\n",
			exitCode:   1,
			wantFailed: true,
			wantTail:   []string{testSummaryHeader},
		},
		{
			// The subshell that runs go dies before it writes go's status, so the status file stays empty and
			// the recipe reads no number to test: it must fail the run all the same, not read the empty file
			// as success and exit zero over the failing suite (issue #253). The summary is owed here most of all,
			// because go's own FAIL line is all the log has: the pipeline has finished and tee has written the
			// whole file before the recipe reads the status, so the summary is exactly go's lines.
			name:       "a go whose status is lost",
			output:     "FAIL\texample/pkg/b\t0.1s\n",
			exitCode:   1,
			wantFailed: true,
			script:     killingGoScript,
			wantTail: []string{
				testSummaryHeader,
				"FAIL\texample/pkg/b\t0.1s",
			},
			wantNoStderr: "No such file or directory",
		},
		{
			// The run itself is interrupted: TERM reaches the recipe's shell while go still runs, so the
			// recipe's TERM trap exits 143 and its EXIT trap still removes the temporary directory. make
			// reports the status the recipe exited with, and the interrupted run prints no summary, which the
			// empty tail pins (issue #253).
			name:       "a run make test is interrupted with TERM",
			output:     "ok  \texample/pkg/a\t0.1s\n",
			exitCode:   0,
			wantFailed: true,
			script:     termingGoScript,
			wantTail:   []string{},
			wantStderr: "Error 143",
		},
		{
			// A timeout prints no --- FAIL: line: the panic names the tests that were running instead, one tab in
			// from the block's own indentation, and the package line follows. The summary repeats those test lines
			// indented by two tabs, so the test that hung is named too (issue #253).
			name:       "a test that times out",
			output:     timeoutGoTestOutput,
			exitCode:   1,
			wantFailed: true,
			wantTail: []string{
				testSummaryHeader,
				"panic: test timed out after 1s",
				"\t\tTestSlow (1s)",
				"FAIL\texample/pkg/e\t1.010s",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			extra, argvFile := fakeGoOnPath(t, tc.script, tc.output, tc.exitCode)

			// The recipe makes its one temporary directory under TMPDIR and removes it on every exit, interrupted
			// runs included, so a case gives make a TMPDIR of its own and finds it as empty after the run as
			// before: nothing of a make test run, neither go's output nor its status, is left behind (issue #253).
			tmpDir := t.TempDir()
			extra["TMPDIR"] = tmpDir

			stdout, stderr, err := runMake(t, []string{"test"}, makeEnv(t, extra))

			if tc.wantFailed && err == nil {
				t.Errorf("make test exited zero although the run failed and go exited %d, so make check would pass over a failing suite (issue #253)\nstdout:\n%s\nstderr:\n%s",
					tc.exitCode, stdout, stderr)
			}
			if !tc.wantFailed && err != nil {
				t.Fatalf("make test exited non-zero although go exited zero: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}

			gotArgs := splitLines(readFileString(t, argvFile))
			if diff := cmp.Diff(goTestArgs, gotArgs); diff != "" {
				t.Errorf("make test does not hand go exactly go test -race -count=1 ./... (-want +got):\n%s\nstdout:\n%s\nstderr:\n%s",
					diff, stdout, stderr)
			}

			lines := splitLines(stdout)
			goLines := splitLines(tc.output)

			at := indexOfRun(lines, goLines)
			if at < 0 {
				t.Fatalf("make test's stdout does not print go test's own output unchanged, so a failing run hides what go said (issue #253)\nwant go's output:\n%s\nstdout:\n%s\nstderr:\n%s",
					tc.output, stdout, stderr)
			}

			if tc.wantNoStderr != "" && strings.Contains(stderr, tc.wantNoStderr) {
				t.Errorf("make test prints %q on stderr where a lost go status must fail the run and still print the summary of the failing packages and tests (issue #253)\nstdout:\n%s\nstderr:\n%s",
					tc.wantNoStderr, stdout, stderr)
			}

			if tc.wantStderr != "" && !strings.Contains(stderr, tc.wantStderr) {
				t.Errorf("make test does not report the run's failure with %q on stderr, so a run that dies loses its status (issue #253)\nstdout:\n%s\nstderr:\n%s",
					tc.wantStderr, stdout, stderr)
			}

			if tc.wantTail != nil {
				after := lines[at+len(goLines):]
				if after == nil {
					after = []string{}
				}

				if diff := cmp.Diff(tc.wantTail, after); diff != "" {
					t.Errorf("make test does not end with the summary of the failing packages and tests, the last lines after go's own output (-want +got):\n%s\nstdout:\n%s\nstderr:\n%s",
						diff, stdout, stderr)
				}
			}

			// macOS's mktemp -d makes its directory in the user's own temporary directory
			// (_CS_DARWIN_USER_TEMP_DIR) whatever TMPDIR says, so only GNU mktemp can be steered into the case's
			// TMPDIR; the recipe runs in the VM, which is Linux (issue #314).
			if runtime.GOOS != "linux" {
				return
			}

			// The fake go saw the recipe's temporary directory in that TMPDIR while it ran; a run that made its
			// directory elsewhere would leave the check below nothing to say (issue #253).
			if seen := splitLines(readFileString(t, extra["FAKE_GO_TMPDIR"])); len(seen) == 0 {
				t.Errorf("make test made no temporary directory under the TMPDIR it was given while go ran, so this case cannot tell whether the recipe removes it (issue #253)")
			}

			if left := dirEntries(t, tmpDir); len(left) != 0 {
				t.Errorf("make test leaves %q in its TMPDIR after the run, so every run leaves its temporary directory behind (issue #253)",
					left)
			}
		})
	}
}

// fakeGoOnPath writes a fake go that runs script, a fake printing output and exiting with exitCode unless it does
// more, into a bin directory of its own, and returns the environment that puts it first on PATH - ahead of the
// machine's own go, so no case runs the real test suite - with the rest of the machine's tools behind it, plus the
// file the fake records its arguments in. An empty script is fakeGoScript.
func fakeGoOnPath(t *testing.T, script, output string, exitCode int) (map[string]string, string) {
	t.Helper()

	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	outputFile := filepath.Join(dir, "output")
	binDir := filepath.Join(dir, "bin")

	if script == "" {
		script = fakeGoScript
	}

	writeFileMode(t, filepath.Join(binDir, "go"), []byte(script), 0o700)
	writeFileMode(t, outputFile, []byte(output), 0o600)

	return map[string]string{
		"PATH":           binDir + ":/usr/local/bin:/usr/bin:/bin",
		"FAKE_GO_ARGV":   argvFile,
		"FAKE_GO_OUTPUT": outputFile,
		"FAKE_GO_EXIT":   strconv.Itoa(exitCode),
		"FAKE_GO_TMPDIR": filepath.Join(dir, "tmpdir"),
	}, argvFile
}

// dirEntries returns the names in dir, failing the test when it cannot be read.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// splitLines splits text into the lines the terminal shows: the newline that ends the last one is dropped and an
// empty text is no line at all.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// indexOfRun returns where run appears in lines as a contiguous block, and -1 when it does not.
func indexOfRun(lines, run []string) int {
	for i := 0; i+len(run) <= len(lines); i++ {
		if slices.Equal(lines[i:i+len(run)], run) {
			return i
		}
	}

	return -1
}

// readFileString returns the content of the file at path, failing the test when it is not there.
func readFileString(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(body)
}
