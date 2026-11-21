// Pins how make test reports a failing run (issue #253): go's own output stays whole, and a failing run ends with a
// summary that names every failing package and test, so the next unexplained FAIL - the one PRs #239 and #250 saw
// and could not trace - names its package and test in the log's last lines.
//
// Every case runs the Makefile's test recipe against a fake go first on PATH: no case starts the real test suite.
package devenv_test

import (
	"os"
	"path/filepath"
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

// fakeGoScript is the fake go the cases below put first on PATH: it appends its arguments to the file FAKE_GO_ARGV
// names, prints the canned output of FAKE_GO_OUTPUT and exits with FAKE_GO_EXIT. The Makefile runs go in recipes
// only, never at parse time, so the fake answers every call make test makes (issue #253).
const fakeGoScript = `#!/bin/sh
printf '%s\n' "$@" >> "$FAKE_GO_ARGV"
cat "$FAKE_GO_OUTPUT"
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
		// wantTail is every line make test prints after go's own output, the summary's header included: an empty
		// slice pins that go's output is the whole of it, and a header on its own pins that the summary is printed
		// even when go names no package and no test.
		wantTail []string
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			extra, argvFile := fakeGoOnPath(t, tc.output, tc.exitCode)

			stdout, stderr, err := runMake(t, []string{"test"}, makeEnv(t, extra))

			if tc.wantFailed && err == nil {
				t.Errorf("make test exited zero although go exited %d, so make check would pass over a failing suite (issue #253)\nstdout:\n%s\nstderr:\n%s",
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

			after := lines[at+len(goLines):]
			if after == nil {
				after = []string{}
			}

			if diff := cmp.Diff(tc.wantTail, after); diff != "" {
				t.Errorf("make test does not end with the summary of the failing packages and tests, the last lines after go's own output (-want +got):\n%s\nstdout:\n%s\nstderr:\n%s",
					diff, stdout, stderr)
			}
		})
	}
}

// fakeGoOnPath writes a fake go that prints output and exits with exitCode into a bin directory of its own, and
// returns the environment that puts it first on PATH - ahead of the machine's own go, so no case runs the real test
// suite - with the rest of the machine's tools behind it, plus the file the fake records its arguments in.
func fakeGoOnPath(t *testing.T, output string, exitCode int) (map[string]string, string) {
	t.Helper()

	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	outputFile := filepath.Join(dir, "output")
	binDir := filepath.Join(dir, "bin")

	writeFileMode(t, filepath.Join(binDir, "go"), []byte(fakeGoScript), 0o700)
	writeFileMode(t, outputFile, []byte(output), 0o600)

	return map[string]string{
		"PATH":           binDir + ":/usr/local/bin:/usr/bin:/bin",
		"FAKE_GO_ARGV":   argvFile,
		"FAKE_GO_OUTPUT": outputFile,
		"FAKE_GO_EXIT":   strconv.Itoa(exitCode),
	}, argvFile
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
