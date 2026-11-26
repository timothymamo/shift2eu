// Pins ci/auditrun's entry point, the launcher the audit's model step runs
// (ADR 0040's model-job amendment, issue #277, "The audit's programs"): its
// two flags, the four variables it reads from the step's environment, and the
// exit code each refusal ends with - 2 for a usage or input error, 3 for a run
// refused before the CLI started. Its steps are fakes, so nothing here reads a
// tarball, starts a sandbox, runs a model or takes a managed-settings decision;
// ci/internal/audit's tests pin those.
//
// The tests are white-box, package main, as ci/claudecodepin's are: Go refuses
// an import of a main package, so only its own test files reach run. Nothing the
// run prints may hold the token or the canary, which the test invents.
//
// The result file's writer is pinned at its own seam too: resultWriter opens
// the -result-out path and hands the opened file to writeResult, which takes
// the file as an io.WriteCloser this file's fake answers, so that a test can
// fail the write after part of the body or the close and see that the file is
// gone (doc.go: a failed run writes no file).
//
// The five file commands the step's environment names are pinned as the runner
// leaves them: a table drives every way a step can change one - in place, by a
// link, by another file, by removal, and by creating a path the run recorded as
// absent - and the two a run cannot compare, one still absent after the steps and
// one over the digest's cap; it reads back what the run left at each path, and a
// repair that itself fails has a test of its own, whichever file command of the
// five it fails on, before or after one the run does empty (issues #615, #614).
//
// A file command whose path exists before the run but is no regular file the run
// can open is refused with the usage exit, before any step, and one that does not
// exist at all stays allowed: the run records it as absent (issues #615, #614,
// the auditor's round-4 L1).
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

const (
	// auditToken and auditCanary are invented at test time in place of the
	// step's two secrets: no line the run prints, and no summary, may hold
	// either.
	auditToken  = "oauth-sentinel-token-never-printed" //nolint:gosec // G101: an invented value made at test time, which grants nothing
	auditCanary = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	// auditIntegrity is an integrity of the shape -claude-integrity takes: npm's
	// sha512 of the tarball, in base64.
	auditIntegrity = "sha512-" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="

	// auditResult is the result the fake audit step reads and checks: the bytes
	// -result-out writes, exactly.
	auditResult = "{\"schema\":1,\"release_tag\":\"v0.12.0\",\"image_digest\":\"\"}\n"
)

// allowedVariables are the variables the run may read: the two secrets, the
// paths of the runner's file commands and the step's summary, and the API key it
// refuses. The file commands' paths are read, never their content: a path that
// is not the $GITHUB_ENV file's own directory is a step whose later steps read a
// directory the audit's launch does not digest (issues #615, #614).
var allowedVariables = []string{
	"ANTHROPIC_API_KEY",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"GITHUB_ENV",
	"GITHUB_OUTPUT",
	"GITHUB_PATH",
	"GITHUB_STATE",
	"GITHUB_STEP_SUMMARY",
	"SHIFT2EU_AUDIT_CANARY",
}

// environ is the fake environment: it answers from a map and records every name
// the run asked for, so a test reads which variables the run touches.
type environ struct {
	values map[string]string
	asked  []string
}

// lookup is the os.LookupEnv the run reads its inputs through.
func (e *environ) lookup(name string) (string, bool) {
	e.asked = append(e.asked, name)
	value, ok := e.values[name]

	return value, ok
}

// stepRecorder is the fake Steps: each step records that it ran, and the audit
// step answers with the result body and error the test gives it. duringAudit,
// when set, runs inside that step, before it answers: the seam a test swaps a
// file the run opened - one of the step's file commands - through, as a step
// that writes one could. duringChecksum and checksumErr do the same one step
// earlier, for a run the checksum step refuses, so the audit step never runs.
type stepRecorder struct {
	ran            []string
	result         []byte
	auditErr       error
	duringAudit    func()
	checksumErr    error
	duringChecksum func()
}

// steps is the recorder as the run's six steps.
func (r *stepRecorder) steps() audit.Steps {
	record := func(step string) error {
		r.ran = append(r.ran, step)

		return nil
	}

	return audit.Steps{
		ManagedSettings: func(string) error { return record("managed settings") },
		Checksum: func(string, audit.Inputs) error {
			r.ran = append(r.ran, "checksum")

			if r.duringChecksum != nil {
				r.duringChecksum()
			}

			return r.checksumErr
		},
		Layout:     func(string) error { return record("audit directory") },
		Bubblewrap: func(context.Context) error { return record("bubblewrap") },
		Preflight:  func(context.Context, string) error { return record("preflight") },
		Audit: func(context.Context, string) ([]byte, error) {
			r.ran = append(r.ran, "audit")

			if r.duringAudit != nil {
				r.duringAudit()
			}

			if r.auditErr != nil {
				return nil, r.auditErr
			}

			return r.result, nil
		},
	}
}

// runResult is what one run of the entry point did.
type runResult struct {
	code    int
	stdout  string
	stderr  string
	summary string
	asked   []string
	ran     []string
}

// runAuditrun runs the entry point with args and the environment env answers,
// its steps being a fake recorder that answers with auditResult, and reads back
// everything it wrote.
func runAuditrun(t *testing.T, args []string, env map[string]string) runResult {
	t.Helper()

	return runAuditrunWith(t, args, env, &stepRecorder{result: []byte(auditResult)})
}

// runAuditrunWith is runAuditrun with the recorder the test gives it.
func runAuditrunWith(t *testing.T, args []string, env map[string]string, recorder *stepRecorder) runResult {
	t.Helper()

	lookup := &environ{values: env}

	var stdout, stderr bytes.Buffer

	code := run(args, lookup.lookup, recorder.steps(), "/", &stdout, &stderr)

	result := runResult{
		code:   code,
		stdout: stdout.String(),
		stderr: stderr.String(),
		asked:  lookup.asked,
		ran:    recorder.ran,
	}

	if summary, ok := env["GITHUB_STEP_SUMMARY"]; ok {
		body, err := os.ReadFile(summary)
		if err == nil {
			result.summary = string(body)
		}
	}

	return result
}

// TestRunTakesTheStepsInputs pins the run of a model step whose environment and
// flags are whole: exit 0, the line "finished" on stdout and in the step's
// summary, every one of the six steps run, and only the variables the ADR lists
// read.
func TestRunTakesTheStepsInputs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	env := stepEnvironment(t, dir)

	result := runAuditrun(t, stepArgs(dir), env)

	if result.code != audit.ExitOK {
		t.Fatalf("the run's exit code is %d, want %d (audit.ExitOK): the flags and the environment are whole\nstdout: %s\nstderr: %s",
			result.code, audit.ExitOK, result.stdout, result.stderr)
	}

	if !strings.Contains(result.stdout, "finished") {
		t.Errorf("the run printed %q, want the one line \"finished\" (ADR 0040)", result.stdout)
	}

	if !strings.Contains(result.summary, "finished") {
		t.Errorf("the step's summary holds %q, want the run's line: the summary is what the job shows (ADR 0040)", result.summary)
	}

	want := []string{"managed settings", "checksum", "audit directory", "bubblewrap", "preflight", "audit"}
	if !slices.Equal(result.ran, want) {
		t.Errorf("the steps that ran are %v, want %v: the run's steps come in the ADR's order", result.ran, want)
	}

	assertResultFile(t, resultOutPath(dir), auditResult)

	assertOnlyTheListedVariables(t, result.asked)
}

// elsewhere is what a file a step swaps a file command for holds: a byte of it
// in one of the runner's files would prove the run followed a link, and a byte
// of it in the job's summary would publish a file the audit never wrote.
const elsewhere = "a file the audit's step must never publish\n"

// fileCommandNames are the step's five file commands, named by the variables
// that hold their paths: the one directory the audit digests, and the files
// auditrun is the only legitimate writer of (issues #615, #614).
var fileCommandNames = []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE", "GITHUB_STEP_SUMMARY"}

// TestRunEmptiesAFileCommandItsStepsChanged pins the repair a run makes after
// its steps (issues #615, #614, the auditor's round-3 findings). The step's
// five file commands - $GITHUB_ENV, $GITHUB_PATH, $GITHUB_OUTPUT, $GITHUB_STATE
// and the step summary - are digested as part of the runner's file-command
// directory, but a digest only detects a change; it undoes nothing. The runner
// applies $GITHUB_ENV and $GITHUB_PATH after a step that failed too, and it
// reads the summary by its path after the step and publishes it, so whatever a
// step left in one of those files outlives the run: a step could hand the
// job's post step - actions/checkout's, holding the runtime token that writes
// the Actions cache - a BASH_ENV, and could publish bytes of its own choosing
// as the job's summary.
//
// auditrun is the only legitimate writer of these five files, and the only line
// it writes is the run's own, after the steps. So the run records their
// identity and their content before the steps and compares them with the paths
// after: a file whose path no longer names the regular file the run opened, a
// summary that no longer holds its recorded content followed by exactly the
// line the run printed, or one of the other four that holds any other bytes, is
// a file command the steps changed - and its path is left as an EMPTY regular
// file, a path the steps swapped is replaced without the link ever being
// followed, and a passing run becomes a failure with no result file for the
// encrypt step. A file the steps left alone keeps exactly its bytes, and the
// summary keeps the run's own line.
//
// A path the run recorded as absent is changed when a step creates it, and no
// change at all when it is still absent after the steps: the run creates nothing
// there and says nothing. A file over the digest's 4 MiB cap cannot be compared
// with what the run recorded, so it counts as changed and is emptied too (fail
// closed); a real launch's preflight digest refuses such a directory before any
// step, so this pins what the repair does on its own.
//
// The table drives each change from inside a fake step, so nothing here starts a
// sandbox, runs a model or writes a real step's files.
func TestRunEmptiesAFileCommandItsStepsChanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// before, when set, fills the file commands as the step finds them:
		// what the runner's four files or an earlier step's summary already
		// held when the run started.
		before func(*testing.T, *stepFiles)
		// change, when set, is what a step does to the file commands while the
		// run is under way.
		change func(*testing.T, *stepFiles)
		// refusedBeforeAudit makes the checksum step refuse the run, so that a
		// change made there happens on a run that already has an exit code of
		// its own and never reaches the audit.
		refusedBeforeAudit bool
		// wantCode is the run's exit code.
		wantCode int
		// wantResult is whether the run leaves its -result-out file holding the
		// audit step's bytes.
		wantResult bool
		// wantEmpty names the file commands the run must leave as empty regular
		// files; every other one must keep exactly what it held.
		wantEmpty []string
		// wantAbsent names the file commands the run recorded as absent that
		// must not exist after it either: absent before and after is no change,
		// so the run creates nothing at their paths (issues #615, #614).
		wantAbsent []string
	}{
		{
			name:       "no file command the steps changed",
			before:     writePreExistingLines,
			wantCode:   audit.ExitOK,
			wantResult: true,
		},
		{
			// The round-3 finding: a step appends to the summary's own inode,
			// so its path is still the file the run opened and only a content
			// check can tell - and the runner would publish the step's line.
			name:      "a step summary appended to in place",
			change:    appendTo("GITHUB_STEP_SUMMARY", "a line the step added\n"),
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_STEP_SUMMARY"},
		},
		{
			// The same inode and the same size, other bytes: a check that
			// compared sizes would pass this one.
			name: "a step summary overwritten in place with the same size",
			before: func(t *testing.T, f *stepFiles) {
				t.Helper()

				writeEnvFile(t, f.path("GITHUB_STEP_SUMMARY"), "the job's own summary line\n")
			},
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				path := f.path("GITHUB_STEP_SUMMARY")

				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read the summary %s to overwrite it: %v", path, err)
				}

				overwriteInPlace(t, path, strings.ToUpper(string(body)))
			},
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_STEP_SUMMARY"},
		},
		{
			name: "a step summary renamed over with another regular file",
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				swapForRegularFile(t, f.path("GITHUB_STEP_SUMMARY"))
			},
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_STEP_SUMMARY"},
		},
		{
			name: "a step summary removed during the run",
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				if err := os.Remove(f.path("GITHUB_STEP_SUMMARY")); err != nil {
					t.Fatalf("remove the summary %s the run opened: %v", f.path("GITHUB_STEP_SUMMARY"), err)
				}
			},
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_STEP_SUMMARY"},
		},
		{
			// A line in $GITHUB_PATH is a directory later steps and the
			// job's post step take their programs from.
			name:      "$GITHUB_PATH appended to in place",
			change:    appendTo("GITHUB_PATH", "/tmp/evil\n"),
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_PATH"},
		},
		{
			name: "$GITHUB_PATH swapped for a link to another file",
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				swapForLink(t, f.path("GITHUB_PATH"), f.target)
			},
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_PATH"},
		},
		{
			// A line in $GITHUB_ENV is a variable every later command of the
			// job runs with.
			name:      "$GITHUB_ENV appended to in place",
			change:    appendTo("GITHUB_ENV", "BASH_ENV=/tmp/x\n"),
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_ENV"},
		},
		{
			name: "$GITHUB_OUTPUT swapped for a link to another file",
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				swapForLink(t, f.path("GITHUB_OUTPUT"), f.target)
			},
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_OUTPUT"},
		},
		{
			name:      "$GITHUB_STATE appended to in place",
			change:    appendTo("GITHUB_STATE", "state=the step's own\n"),
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_STATE"},
		},
		{
			// A file command the run recorded as absent, a path an earlier step
			// of the job removed: a step that creates it has changed it, and the
			// run leaves an empty regular file where the step left bytes.
			name:   "a $GITHUB_STATE absent before the run that a step created",
			before: absentBefore("GITHUB_STATE"),
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				writeEnvFile(t, f.path("GITHUB_STATE"), "state=the step's own\n")
			},
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_STATE"},
		},
		{
			// Absent before and after is no change: the run neither creates the
			// path nor reports one, and the run passes with its result.
			name:       "a $GITHUB_STATE absent before the run and still absent after it",
			before:     absentBefore("GITHUB_STATE"),
			wantCode:   audit.ExitOK,
			wantResult: true,
			wantAbsent: []string{"GITHUB_STATE"},
		},
		{
			// Absent is not a refusal: a path that does not exist before the run
			// is recorded as absent, and nothing more (issues #615, #614, the
			// auditor's round-4 L1's other half). The runner lays its four files
			// down before the step, so this is what an earlier step of the job
			// that removed one leaves; the run creates only the summary it appends
			// to, and the run passes with its result.
			name: "every file command absent before the run",
			before: func(t *testing.T, f *stepFiles) {
				t.Helper()

				// The summary is absent already: no earlier step wrote one, so the
				// run creates it. The runner's four files are removed, as an
				// earlier step of the job that cleaned them up leaves them.
				for _, name := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE"} {
					removeEntry(t, f.path(name))
				}
			},
			wantCode:   audit.ExitOK,
			wantResult: true,
			wantAbsent: []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE"},
		},
		{
			// A file over the digest's 4 MiB cap cannot be compared with what the
			// run recorded, so it counts as changed and is emptied (fail closed)
			// even though no step wrote to it.
			name:      "a $GITHUB_ENV over the digest's cap",
			before:    writeOverCapEnvFile("GITHUB_ENV"),
			wantCode:  audit.ExitFailed,
			wantEmpty: []string{"GITHUB_ENV"},
		},
		{
			// A run that already failed keeps its own exit code: the repair
			// empties the file the steps changed, it does not reclassify the
			// failure.
			name:               "a step summary appended to in place on a run the checksum refused",
			refusedBeforeAudit: true,
			change:             appendTo("GITHUB_STEP_SUMMARY", "a line the step added\n"),
			wantCode:           audit.ExitRefused,
			wantEmpty:          []string{"GITHUB_STEP_SUMMARY"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newStepFiles(t)

			if tc.before != nil {
				tc.before(t, f)
			}

			pre := f.contents(t)

			recorder := &stepRecorder{result: []byte(auditResult)}

			if tc.change != nil {
				if tc.refusedBeforeAudit {
					recorder.duringChecksum = func() { tc.change(t, f) }
					recorder.checksumErr = errTestChecksum
				} else {
					recorder.duringAudit = func() { tc.change(t, f) }
				}
			}

			result := runAuditrunWith(t, stepArgs(f.dir), f.env, recorder)

			if result.code != tc.wantCode {
				t.Errorf("the run's exit code is %d, want %d: a file command the steps changed fails the run with the audit's exit, and a run that had already failed keeps its own (issues #615, #614)\nstdout: %s\nstderr: %s",
					result.code, tc.wantCode, result.stdout, result.stderr)
			}

			if len(tc.wantEmpty) > 0 {
				if strings.TrimSpace(result.stderr) == "" {
					t.Errorf("the run printed nothing on stderr, want a note that it emptied %v: the step's log says what the run did to the runner's files (ADR 0040, issues #615, #614)",
						tc.wantEmpty)
				}
			} else if result.stderr != "" {
				t.Errorf("the run printed %q on stderr, want nothing: no file command changed, so there is nothing to report", result.stderr)
			}

			for _, name := range fileCommandNames {
				if slices.Contains(tc.wantAbsent, name) {
					assertFileCommandAbsent(t, f, name)

					continue
				}

				assertFileCommand(t, f, name, pre, tc.wantEmpty, result)
			}

			if tc.wantResult {
				assertResultFile(t, resultOutPath(f.dir), auditResult)
			} else if _, err := os.Stat(resultOutPath(f.dir)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the result file at %s is still there (stat: %v), want it removed: a repair fails the run, and a failed run hands the encrypt step no result (doc.go, issues #615, #614)",
					resultOutPath(f.dir), err)
			}

			// The file a swap pointed a link at is the step's, not the run's:
			// the run replaces the file command's path only.
			kept, err := os.ReadFile(f.target)
			if err != nil {
				t.Fatalf("read the file a link pointed at, %s: %v", f.target, err)
			}

			if string(kept) != elsewhere {
				t.Errorf("the file a link pointed at holds %q, want %q untouched: the run replaces the file command's path, never its target (issues #615, #614)", kept, elsewhere)
			}

			assertOnlyTheFileCommands(t, f, tc.wantAbsent)
			assertNoSecret(t, result)
		})
	}
}

// TestRunFailsWhenAChangedFileCommandCannotBeEmptied pins the repair's own
// failure, the one outcome the table above cannot express (issues #615, #614): a
// file command the steps changed that the run cannot leave empty. A step that
// replaces the path with a directory makes it deterministic and needs no
// permission trick, because os.Rename cannot put a file over a directory. The run
// must then fail - the changed file command was not emptied, so the runner may
// still apply it - with its own line on stderr, naming that file command and
// distinct from the note a successful repair prints; it names neither the step's
// token nor its canary, and leaves the path as the step made it, with no
// temporary file behind.
//
// One file command the run cannot empty stops neither the repair of the ones
// after it nor the report of the ones it did empty (the reviewer's and the
// auditor's round-4 should-fix): a repair that returned at its first failure
// would leave a step that made $GITHUB_ENV a directory early in the five with its
// $GITHUB_PATH line and its summary text for the runner to apply and publish. The
// table pins the failure first, with $GITHUB_PATH and the summary emptied after
// it, and the failure last, with $GITHUB_PATH emptied before it.
func TestRunFailsWhenAChangedFileCommandCannotBeEmptied(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// change is what a step does to the file commands while the run is under
		// way.
		change func(*testing.T, *stepFiles)
		// wantHostile names the one file command whose path the step replaced
		// with a non-empty directory, which no repair can put an empty file
		// over: the run cannot empty it.
		wantHostile string
		// wantEmpty names the file commands the run must still leave as empty
		// regular files, whether the step changed them before or after the one
		// it could not empty.
		wantEmpty []string
	}{
		{
			name: "a file command the run cannot empty and nothing else changed",
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				swapForDirectory(t, f.path("GITHUB_PATH"))
			},
			wantHostile: "GITHUB_PATH",
		},
		{
			name: "a file command the run cannot empty before two it must empty",
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				swapForDirectory(t, f.path("GITHUB_ENV"))
				appendTo("GITHUB_PATH", "/tmp/evil\n")(t, f)
				appendTo("GITHUB_STEP_SUMMARY", "a line the step added\n")(t, f)
			},
			wantHostile: "GITHUB_ENV",
			wantEmpty:   []string{"GITHUB_PATH", "GITHUB_STEP_SUMMARY"},
		},
		{
			name: "a file command the run cannot empty after one it must empty",
			change: func(t *testing.T, f *stepFiles) {
				t.Helper()

				appendTo("GITHUB_PATH", "/tmp/evil\n")(t, f)
				swapForDirectory(t, f.path("GITHUB_STEP_SUMMARY"))
			},
			wantHostile: "GITHUB_STEP_SUMMARY",
			wantEmpty:   []string{"GITHUB_PATH"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newStepFiles(t)
			pre := f.contents(t)

			recorder := &stepRecorder{
				result:      []byte(auditResult),
				duringAudit: func() { tc.change(t, f) },
			}

			result := runAuditrunWith(t, stepArgs(f.dir), f.env, recorder)

			if result.code != audit.ExitFailed {
				t.Errorf("the run's exit code is %d, want %d (audit.ExitFailed): a changed file command the run could not empty leaves the runner its bytes, so the run must not pass\nstdout: %s\nstderr: %s",
					result.code, audit.ExitFailed, result.stdout, result.stderr)
			}

			for _, line := range []string{"could not be emptied", "may be hostile"} {
				if !strings.Contains(result.stderr, line) {
					t.Errorf("the run printed %q on stderr, want a line holding %q: a repair that itself fails says the posted summary or the job's environment may be hostile (ADR 0040, issues #615, #614)",
						result.stderr, line)
				}
			}

			assertRepairNotes(t, result.stderr, tc.wantEmpty, tc.wantHostile)

			if _, err := os.Stat(resultOutPath(f.dir)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the result file at %s is still there (stat: %v), want it removed: a repair that fails fails a passing run and hands the encrypt step no result (doc.go, issues #615, #614)",
					resultOutPath(f.dir), err)
			}

			for _, name := range fileCommandNames {
				if name == tc.wantHostile {
					continue
				}

				assertFileCommand(t, f, name, pre, tc.wantEmpty, result)
			}

			assertHostileDirectory(t, f.path(tc.wantHostile))

			assertOnlyTheFileCommands(t, f, nil)
			assertNoSecret(t, result)
		})
	}
}

// TestRunRefusesAFileCommandItCannotOpen pins the auditor's round-4 L1 (issues
// #615, #614): a file command whose path exists before any step but is no regular
// file the run can open - a directory, a symlink - is refused with the usage exit,
// before the first step. It reverses the earlier expectation that a summary the
// run cannot open is left alone and the run finishes: the run opened the summary
// with O_APPEND|O_CREATE|O_WRONLY, which follows a link, so a step could name a
// file of its own there and have the run append to it, and a path the run cannot
// record is one it cannot compare after the steps either. Fail closed instead: the
// runner only ever creates regular files, so the refusal costs a real run
// nothing. A path that does not exist at all stays allowed, recorded as absent
// (TestRunTakesTheStepsInputs, and the absent rows of the table above).
//
// The entry the step left at the path is left exactly as it is - the run refuses
// before it writes anything, so the directory the step made keeps its file and the
// link's target keeps every byte it held - and no result file is written. The
// runner's other four files are the refusal table's rows
// (TestRunRefusesEveryUsageAndInputError): the summary is the one of the five the
// run opens by itself, which is where following a link would publish a file the
// audit never wrote.
func TestRunRefusesAFileCommandItCannotOpen(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// entry replaces the summary's path, which does not exist before the
		// run, with an entry that is no regular file the run can open.
		entry func(t *testing.T, summary, target string)
	}{
		{
			name: "a step summary that is a directory",
			entry: func(t *testing.T, summary, _ string) {
				t.Helper()

				if err := os.Mkdir(summary, 0o700); err != nil {
					t.Fatalf("make the summary's path %s a directory: %v", summary, err)
				}

				writeEnvFile(t, filepath.Join(summary, "evil"), "/tmp/evil\n")
			},
		},
		{
			name: "a step summary that is a symlink to a regular file",
			entry: func(t *testing.T, summary, target string) {
				t.Helper()

				if err := os.Symlink(target, summary); err != nil {
					t.Fatalf("make the summary's path %s a link to %s: %v", summary, target, err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newStepFiles(t)
			pre := f.contents(t)

			summary := f.path("GITHUB_STEP_SUMMARY")

			tc.entry(t, summary, f.target)

			before, err := os.Lstat(summary)
			if err != nil {
				t.Fatalf("lstat the file command $GITHUB_STEP_SUMMARY's path %s before the run: %v", summary, err)
			}

			result := runAuditrun(t, stepArgs(f.dir), f.env)

			if result.code != audit.ExitUsage {
				t.Errorf("the run's exit code is %d, want %d (audit.ExitUsage): a file command that exists but is no regular file the run can open is refused before any step (issues #615, #614, the auditor's round-4 L1)\nstdout: %s\nstderr: %s",
					result.code, audit.ExitUsage, result.stdout, result.stderr)
			}

			if len(result.ran) != 0 {
				t.Errorf("the steps %v ran; the refusal comes before the first step (issues #615, #614)", result.ran)
			}

			if result.stderr == "" {
				t.Error("the run printed nothing on stderr, want the refusal's line: the step's log says which of its file commands the run would not take")
			}

			after, err := os.Lstat(summary)
			if err != nil {
				t.Fatalf("lstat the file command $GITHUB_STEP_SUMMARY's path %s after the run: %v", summary, err)
			}

			if !os.SameFile(before, after) || before.Mode().Type() != after.Mode().Type() {
				t.Errorf("the file command $GITHUB_STEP_SUMMARY's path %s is another entry after the run (%s, was %s), want the one the step left: the run refuses before it writes anything there (issues #615, #614)",
					summary, after.Mode(), before.Mode())
			}

			if after.IsDir() {
				entries, err := os.ReadDir(summary)
				if err != nil {
					t.Fatalf("read the summary's directory %s after the run: %v", summary, err)
				}

				held := make([]string, 0, len(entries))
				for _, entry := range entries {
					held = append(held, entry.Name())
				}

				if want := []string{"evil"}; !slices.Equal(held, want) {
					t.Errorf("the summary's directory holds %v after the run, want %v: the run writes no summary line there (issues #615, #614)", held, want)
				}
			}

			// The file a link pointed at is the step's, not the run's: nothing the
			// run does follows a file command's link.
			kept, err := os.ReadFile(f.target)
			if err != nil {
				t.Fatalf("read the file a link pointed at, %s: %v", f.target, err)
			}

			if string(kept) != elsewhere {
				t.Errorf("the file a link pointed at holds %q, want %q untouched: the run never follows a file command's link (issues #615, #614)", kept, elsewhere)
			}

			if _, err := os.Lstat(resultOutPath(f.dir)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the result file at %s exists (lstat: %v), want none: the refusal ends the run before the steps, so nothing was checked to write", resultOutPath(f.dir), err)
			}

			for _, name := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE"} {
				assertFileCommand(t, f, name, pre, nil, result)
			}

			assertOnlyTheFileCommands(t, f, nil)
			assertNoSecret(t, result)
		})
	}
}

// stepFiles is one model step's file commands as the run finds them: the
// environment that names them, the one directory they share, and a file
// elsewhere, holding elsewhere, for a case that swaps one of them for a link.
type stepFiles struct {
	env    map[string]string
	dir    string
	target string
}

// newStepFiles returns the file commands of a fresh model step, with the file a
// swap can point at.
func newStepFiles(t *testing.T) *stepFiles {
	t.Helper()

	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere.md")
	writeEnvFile(t, target, elsewhere)

	return &stepFiles{env: stepEnvironment(t, dir), dir: dir, target: target}
}

// path is the path of the file command the variable names.
func (f *stepFiles) path(name string) string {
	return f.env[name]
}

// contents returns what each of the five file commands holds now, and "" for
// one that does not exist yet: the record the run takes before its steps.
func (f *stepFiles) contents(t *testing.T) map[string]string {
	t.Helper()

	held := make(map[string]string, len(fileCommandNames))

	for _, name := range fileCommandNames {
		body, err := os.ReadFile(f.path(name))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("read the file command %s at %s: %v", name, f.path(name), err)
			}

			held[name] = ""

			continue
		}

		held[name] = string(body)
	}

	return held
}

// assertFileCommand fails unless the file command name is left as the run must
// leave it: a regular file that is empty when the steps changed it, and one
// that holds exactly what it held before the run - plus, for the summary, the
// line the run printed - when they did not.
func assertFileCommand(t *testing.T, f *stepFiles, name string, pre map[string]string, wantEmpty []string, result runResult) {
	t.Helper()

	path := f.path(name)

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("the file command %s's path %s is gone after the run: %v; a file command the steps changed is left as an empty regular file (issues #615, #614)",
			name, path, err)
	}

	if !info.Mode().IsRegular() {
		t.Errorf("the file command %s's path %s is %s after the run, want a regular file: a path the steps swapped is replaced, the link never followed (issues #615, #614)",
			name, path, info.Mode())

		return
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file command %s at %s after the run: %v", name, path, err)
	}

	want := pre[name]
	if name == "GITHUB_STEP_SUMMARY" {
		// The run's Out is stdout and the summary: the summary gets the run's
		// line and nothing else, appended to what it held.
		want += result.stdout
	}

	if slices.Contains(wantEmpty, name) {
		want = ""
	}

	if string(body) != want {
		t.Errorf("the file command %s holds %s after the run, want %s: a file the steps changed - another file at its path, other bytes in it, or a summary that is not the recorded content and the run's own line - is emptied, and one they left alone keeps exactly its bytes (issues #615, #614)",
			name, quoted(body), quoted([]byte(want)))
	}
}

// quoted returns body as a test's line prints it: its bytes quoted while they
// are short, and past that only their length, so a file over the digest's cap
// does not print its mebibytes into the log of a failed test.
func quoted(body []byte) string {
	const limit = 200

	if len(body) <= limit {
		return strconv.Quote(string(body))
	}

	return strconv.Quote(string(body[:limit])) + " and " + strconv.Itoa(len(body)-limit) + " bytes more"
}

// assertFileCommandAbsent fails unless the file command's path still names no
// entry at all after the run: a file command absent before the run and left
// absent is no change, so the run creates nothing there and reports nothing
// (issues #615, #614).
func assertFileCommandAbsent(t *testing.T, f *stepFiles, name string) {
	t.Helper()

	path := f.path(name)

	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file command %s's path %s exists after the run (lstat: %v), want no entry at all: absent before the run and after it is no change, and the run creates nothing there (issues #615, #614)",
			name, path, err)
	}
}

// assertRepairNotes fails unless the run's stderr notes exactly the file commands
// in wantEmpty as left an empty file, and the one in wantFailed as a file command
// it could not empty (issues #615, #614): one file command the run cannot empty
// must neither hide the note of one it did empty nor the failure of another, and
// a note names the one the runner may still apply.
func assertRepairNotes(t *testing.T, stderr string, wantEmpty []string, wantFailed string) {
	t.Helper()

	const emptiedNote = "it is left as an empty file"

	emptied := make([]string, 0, len(wantEmpty))
	failed := make([]string, 0, 1)

	for _, line := range strings.Split(stderr, "\n") {
		for _, name := range fileCommandNames {
			if !strings.Contains(line, name) {
				continue
			}

			if strings.Contains(line, emptiedNote) && !slices.Contains(emptied, name) {
				emptied = append(emptied, name)
			}

			if strings.Contains(line, "could not be emptied") && !slices.Contains(failed, name) {
				failed = append(failed, name)
			}
		}
	}

	// Only the set is what the run must report; the lines' order is the repair's
	// own business.
	slices.Sort(emptied)
	slices.Sort(failed)

	want := slices.Clone(wantEmpty)
	slices.Sort(want)

	if !slices.Equal(emptied, want) {
		t.Errorf("the run noted %v as left an empty file, want exactly %v: a file command the run cannot empty stops neither the repair of the ones after it nor the note of the ones it emptied (issues #615, #614, the round-4 should-fix)\nstderr: %s",
			emptied, want, stderr)
	}

	if wantFailedName := []string{wantFailed}; !slices.Equal(failed, wantFailedName) {
		t.Errorf("the run noted %v as a file command it could not empty, want exactly %v: the failure's line names the file command the runner may still apply (ADR 0040, issues #615, #614)\nstderr: %s",
			failed, wantFailedName, stderr)
	}
}

// assertHostileDirectory fails unless path is still the directory the step made
// in a file command's place, holding the step's one file and nothing else: a
// repair that fails leaves the path as the step made it, and no temporary file
// of its own behind (issues #615, #614).
func assertHostileDirectory(t *testing.T, path string) {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat the file command's path %s after the run: %v", path, err)
	}

	if !info.IsDir() {
		t.Errorf("the file command's path %s is %s after the run, want the directory the step made: a repair that failed emptied nothing there",
			path, info.Mode())

		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read the directory the file command's path names, %s: %v", path, err)
	}

	held := make([]string, 0, len(entries))
	for _, entry := range entries {
		held = append(held, entry.Name())
	}

	if want := []string{"evil"}; !slices.Equal(held, want) {
		t.Errorf("the directory at the file command's path holds %v, want %v: the run leaves the path the step made as it is, and no temporary file of its repair behind (issues #615, #614)",
			held, want)
	}
}

// assertOnlyTheFileCommands fails unless the file-command directory holds
// exactly the five file commands and the result file, when the run left one:
// no temporary file of a repair is left behind (issue #615). The file commands
// in wantAbsent were absent before the run, so their paths hold nothing.
func assertOnlyTheFileCommands(t *testing.T, f *stepFiles, wantAbsent []string) {
	t.Helper()

	entries, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatalf("read the file commands' directory %s: %v", f.dir, err)
	}

	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}

	want := make([]string, 0, len(fileCommandNames)+1)
	for _, name := range fileCommandNames {
		if slices.Contains(wantAbsent, name) {
			continue
		}

		want = append(want, filepath.Base(f.path(name)))
	}

	if _, err := os.Lstat(resultOutPath(f.dir)); err == nil {
		want = append(want, filepath.Base(resultOutPath(f.dir)))
	}

	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf("the file commands' directory holds %v, want %v: a repair leaves no temporary file behind (issues #615, #614)", got, want)
	}
}

// writePreExistingLines fills the five file commands as a step finds them: a
// line each, what the runner's four files and an earlier step's summary already
// held when the run started.
func writePreExistingLines(t *testing.T, f *stepFiles) {
	t.Helper()

	for _, name := range fileCommandNames {
		writeEnvFile(t, f.path(name), "a line the step already had\n")
	}
}

// auditFileCommandCap is the audit library's cap on a file command's content
// before the steps (system.go's maxFileCommandBytes, the digest's 4 MiB), over
// which the run cannot compare what it recorded with what it finds.
const auditFileCommandCap = 4 << 20

// absentBefore returns the change that removes the file command the variable
// names before the run records it - an earlier step of the job removed the
// runner's file - so the run records the path as absent.
func absentBefore(name string) func(*testing.T, *stepFiles) {
	return func(t *testing.T, f *stepFiles) {
		t.Helper()

		path := f.path(name)

		if err := os.Remove(path); err != nil {
			t.Fatalf("remove the file command %s at %s before the run records it: %v", name, path, err)
		}
	}
}

// removeEntry removes the entry at path, failing the test when it cannot.
func removeEntry(t *testing.T, path string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}

// replaceWithDirectory returns the entry that removes the file command the
// variable names and makes a directory in its place, holding a file of the
// step's own.
func replaceWithDirectory(name string) func(*testing.T, map[string]string) {
	return func(t *testing.T, env map[string]string) {
		t.Helper()

		path := env[name]

		removeEntry(t, path)

		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("make the file command %s's path %s a directory: %v", name, path, err)
		}

		writeEnvFile(t, filepath.Join(path, "evil"), "/tmp/evil\n")
	}
}

// replaceWithSymlink returns the entry that removes the file command the
// variable names and puts a link to a file elsewhere in its place: the path
// exists, and what it leads to is the step's own file.
func replaceWithSymlink(name string) func(*testing.T, map[string]string) {
	return func(t *testing.T, env map[string]string) {
		t.Helper()

		path := env[name]
		target := filepath.Join(t.TempDir(), "elsewhere.md")

		writeEnvFile(t, target, elsewhere)
		removeEntry(t, path)

		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("swap the file command %s's path %s for a link to %s: %v", name, path, target, err)
		}
	}
}

// replaceWithFIFO returns the entry that removes the file command the variable
// names and puts a named pipe in its place: an entry that exists, is no regular
// file, and that nothing opens, so no test waits for a writer.
func replaceWithFIFO(name string) func(*testing.T, map[string]string) {
	return func(t *testing.T, env map[string]string) {
		t.Helper()

		path := env[name]

		removeEntry(t, path)
		makeFIFO(t, path)
	}
}

// writeOverCapEnvFile returns the change that fills the file command the
// variable names with one byte more than the run caps a file command's content
// at: a file the run cannot compare, which ADR 0040 counts as changed (issues
// #615, #614).
func writeOverCapEnvFile(name string) func(*testing.T, *stepFiles) {
	return func(t *testing.T, f *stepFiles) {
		t.Helper()

		writeEnvFile(t, f.path(name), strings.Repeat("a", auditFileCommandCap+1))
	}
}

// appendTo returns the change that appends line to the file command the
// variable names, in place: the file's own inode, the very file the run opened,
// so nothing but its content can tell the step wrote to it.
func appendTo(name, line string) func(*testing.T, *stepFiles) {
	return func(t *testing.T, f *stepFiles) {
		t.Helper()

		path := f.path(name)

		before, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("lstat the file command %s at %s before the step appends to it: %v", name, path, err)
		}

		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("open the file command %s at %s to append to it: %v", name, path, err)
		}

		if _, err := file.WriteString(line); err != nil {
			t.Fatalf("append %q to the file command %s at %s: %v", line, name, path, err)
		}

		if err := file.Close(); err != nil {
			t.Fatalf("close the file command %s at %s: %v", name, path, err)
		}

		after, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("lstat the file command %s at %s after the step appended to it: %v", name, path, err)
		}

		if !os.SameFile(before, after) {
			t.Fatalf("the file command %s at %s is another file after the append, want the one the run opened: the change is in place (issues #615, #614)", name, path)
		}
	}
}

// overwriteInPlace rewrites path's bytes with text in place: the same inode,
// no truncation, so the file holds other bytes of the same size.
func overwriteInPlace(t *testing.T, path, text string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s to overwrite it in place: %v", path, err)
	}

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	if int(info.Size()) != len(text) {
		t.Fatalf("the replacement is %d bytes, want the file's own %d: the change keeps the size and changes the bytes", len(text), info.Size())
	}

	if _, err := file.WriteAt([]byte(text), 0); err != nil {
		t.Fatalf("overwrite %s in place: %v", path, err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// swapForLink removes path and puts a link to target, a file elsewhere holding
// elsewhere, in its place: what a step wanting its bytes published does.
func swapForLink(t *testing.T, path, target string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s the run opened: %v", path, err)
	}

	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("swap %s for a link to %s: %v", path, target, err)
	}
}

// swapForRegularFile renames a file holding elsewhere over path: the path is
// then a regular file that is not the one the run opened.
func swapForRegularFile(t *testing.T, path string) {
	t.Helper()

	other := filepath.Join(t.TempDir(), "other.md")
	writeEnvFile(t, other, elsewhere)

	if err := os.Rename(other, path); err != nil {
		t.Fatalf("rename %s over %s: %v", other, path, err)
	}
}

// swapForDirectory removes path and makes a directory holding a file in its
// place: the one entry a repair cannot put an empty file over, since a rename
// onto a directory fails whatever the file system allows, so the repair fails
// where the run could not.
func swapForDirectory(t *testing.T, path string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s the run opened: %v", path, err)
	}

	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("make %s a directory: %v", path, err)
	}

	writeEnvFile(t, filepath.Join(path, "evil"), "/tmp/evil\n")
}

// TestRunRefusesEveryUsageAndInputError pins the refusals that end a run before
// any step: a missing or unknown flag, a flag without its value, an integrity
// that is no npm sha512 of the tarball, and a missing token, canary or
// $GITHUB_ENV. Each is a usage or input error, the ADR's exit 2, and none of
// them runs a step.
func TestRunRefusesEveryUsageAndInputError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// env changes the whole environment, when set.
		env func(env map[string]string)
		// entry, when set, replaces the entry the file command the variable
		// names holds - one of the runner's four files - with an entry that is
		// no regular file the run can open.
		entry func(*testing.T, map[string]string)
		// args changes the whole command line, when set.
		args func(dir string, args []string) []string
	}{
		{
			name: "a tarball flag left out",
			args: func(_ string, args []string) []string { return args[2:] },
		},
		{
			name: "an integrity flag left out",
			args: func(_ string, args []string) []string { return args[:2] },
		},
		{
			name: "a result-out flag left out",
			args: func(_ string, args []string) []string { return args[:4] },
		},
		{
			name: "a flag with no value",
			args: func(_ string, args []string) []string { return append(args, "-claude-tarball") },
		},
		{
			name: "a flag the run does not take",
			args: func(_ string, args []string) []string { return append(args, "-claude-model", "claude-fable-5-2") },
		},
		{
			name: "an integrity of another algorithm",
			args: func(_ string, args []string) []string {
				args[3] = "sha1-0000000000000000000000000000000000000000"

				return args
			},
		},
		{
			name: "an integrity whose digest is no base64",
			args: func(_ string, args []string) []string {
				args[3] = "sha512-not a base64 digest"

				return args
			},
		},
		{
			name: "an empty integrity",
			args: func(_ string, args []string) []string {
				args[3] = ""

				return args
			},
		},
		{
			name: "no token",
			env:  func(env map[string]string) { delete(env, "CLAUDE_CODE_OAUTH_TOKEN") },
		},
		{
			name: "an empty token",
			env:  func(env map[string]string) { env["CLAUDE_CODE_OAUTH_TOKEN"] = "" },
		},
		{
			name: "no canary",
			env:  func(env map[string]string) { delete(env, "SHIFT2EU_AUDIT_CANARY") },
		},
		{
			name: "a canary of 63 characters",
			env:  func(env map[string]string) { env["SHIFT2EU_AUDIT_CANARY"] = auditCanary[:63] },
		},
		{
			name: "a canary of 65 characters",
			env:  func(env map[string]string) { env["SHIFT2EU_AUDIT_CANARY"] = auditCanary + "0" },
		},
		{
			name: "a canary in upper-case hex",
			env:  func(env map[string]string) { env["SHIFT2EU_AUDIT_CANARY"] = strings.ToUpper(auditCanary) },
		},
		{
			name: "a canary that is no hex",
			env: func(env map[string]string) {
				env["SHIFT2EU_AUDIT_CANARY"] = strings.Repeat("zz", 32)
			},
		},
		{
			name: "no $GITHUB_ENV",
			env:  func(env map[string]string) { delete(env, "GITHUB_ENV") },
		},
		// The runner's file commands of a step are all paths of the one directory
		// the launch digests (issues #615, #614, the auditor's N1): $GITHUB_ENV,
		// $GITHUB_PATH, $GITHUB_OUTPUT, $GITHUB_STATE and the step summary. A step
		// that writes any of them decides what the job's later steps run with and
		// what it publishes, so a file command outside that directory is one the
		// digest never covers: the run refuses it as it refuses a missing
		// $GITHUB_ENV, before any step. Every one of the four is set on a runner, so
		// one left unset is refused too.
		{
			name: "no $GITHUB_PATH",
			env:  func(env map[string]string) { delete(env, "GITHUB_PATH") },
		},
		{
			name: "a $GITHUB_PATH outside the $GITHUB_ENV file's directory",
			env:  func(env map[string]string) { env["GITHUB_PATH"] = anotherDirectoryOf(env, "github-path") },
		},
		{
			name: "no $GITHUB_OUTPUT",
			env:  func(env map[string]string) { delete(env, "GITHUB_OUTPUT") },
		},
		{
			name: "a $GITHUB_OUTPUT outside the $GITHUB_ENV file's directory",
			env:  func(env map[string]string) { env["GITHUB_OUTPUT"] = anotherDirectoryOf(env, "github-output") },
		},
		{
			name: "no $GITHUB_STATE",
			env:  func(env map[string]string) { delete(env, "GITHUB_STATE") },
		},
		{
			name: "a $GITHUB_STATE outside the $GITHUB_ENV file's directory",
			env:  func(env map[string]string) { env["GITHUB_STATE"] = anotherDirectoryOf(env, "github-state") },
		},
		{
			name: "a step summary outside the $GITHUB_ENV file's directory",
			env:  func(env map[string]string) { env["GITHUB_STEP_SUMMARY"] = anotherDirectoryOf(env, "step-summary.md") },
		},
		// A file command whose path exists before any step but is no regular file
		// the run can open - a directory, a symlink, a FIFO - is refused too, with
		// the same usage exit, before the first step (issues #615, #614, the
		// auditor's round-4 L1): the run cannot record such an entry's content, so
		// it cannot tell after the steps whether anything changed it. The runner
		// only ever lays regular files down, so a real step loses nothing. One row
		// per entry type over the runner's four files; the step summary's own two
		// entries are in TestRunRefusesAFileCommandItCannotOpen.
		{
			name:  "a $GITHUB_ENV that is a directory",
			entry: replaceWithDirectory("GITHUB_ENV"),
		},
		{
			name:  "a $GITHUB_PATH that is a symlink to another file",
			entry: replaceWithSymlink("GITHUB_PATH"),
		},
		{
			name:  "a $GITHUB_OUTPUT that is a FIFO",
			entry: replaceWithFIFO("GITHUB_OUTPUT"),
		},
		{
			name:  "a $GITHUB_STATE that is a directory",
			entry: replaceWithDirectory("GITHUB_STATE"),
		},
		// The runner always sets clean absolute paths, so a run takes nothing
		// else (issue #614, the auditor's round-2 Low 2): a path that is not
		// absolute, or that is not already its own cleaned form, names its
		// directory only after Clean - the very comparison the check makes - so
		// the digest and the runner could read two different files of one
		// spelling. It is refused as every other file-command error is, before
		// any step.
		{
			name: "a $GITHUB_ENV path with a . element",
			env:  func(env map[string]string) { env["GITHUB_ENV"] = filepath.Dir(env["GITHUB_ENV"]) + "/./github-env" },
		},
		{
			name: "a $GITHUB_PATH path with a redundant separator",
			env:  func(env map[string]string) { env["GITHUB_PATH"] = filepath.Dir(env["GITHUB_PATH"]) + "//github-path" },
		},
		{
			name: "a $GITHUB_PATH path with a .. element",
			env: func(env map[string]string) {
				env["GITHUB_PATH"] = filepath.Dir(env["GITHUB_PATH"]) + "/invented/../github-path"
			},
		},
		{
			name: "a $GITHUB_OUTPUT path with a . element",
			env: func(env map[string]string) {
				env["GITHUB_OUTPUT"] = filepath.Dir(env["GITHUB_OUTPUT"]) + "/./github-output"
			},
		},
		{
			name: "a $GITHUB_STATE path with a .. element",
			env: func(env map[string]string) {
				env["GITHUB_STATE"] = filepath.Dir(env["GITHUB_STATE"]) + "/invented/../github-state"
			},
		},
		{
			name: "a step summary with a . element",
			env: func(env map[string]string) {
				env["GITHUB_STEP_SUMMARY"] = filepath.Dir(env["GITHUB_STEP_SUMMARY"]) + "/./step-summary.md"
			},
		},
		// A relative path is refused too, the other half of the rule: the
		// runner sets absolute paths, and a relative one (clean, not absolute)
		// names whatever directory the step happens to run in. The paths name
		// one invented directory of the step's own, so the check's comparison
		// of their directories agrees and only the absolute rule is left to
		// refuse them.
		{
			name: "file-command paths that are relative",
			env: func(env map[string]string) {
				for _, name := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE", "GITHUB_STEP_SUMMARY"} {
					env[name] = "invented/" + filepath.Base(env[name])
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			env := stepEnvironment(t, dir)
			args := stepArgs(dir)

			if tc.env != nil {
				tc.env(env)
			}

			if tc.entry != nil {
				tc.entry(t, env)
			}

			if tc.args != nil {
				args = tc.args(dir, args)
			}

			result := runAuditrun(t, args, env)

			if result.code != audit.ExitUsage {
				t.Errorf("the run's exit code is %d, want %d (audit.ExitUsage): a usage or input error\nstdout: %s\nstderr: %s",
					result.code, audit.ExitUsage, result.stdout, result.stderr)
			}

			if len(result.ran) != 0 {
				t.Errorf("the steps %v ran; a usage or input error ends the run before the first step", result.ran)
			}

			assertNoSecret(t, result)
		})
	}
}

// TestRunRefusesAnAPIKey pins the refusal the reviewer shares: a step that holds
// ANTHROPIC_API_KEY is refused before the CLI runs, with exit 3, because the
// audit draws on the subscription's OAuth token and nothing else. A key that is
// set but empty is set: the variable's presence is what the refusal is about.
// The key is refused before the inputs are read (issue #277, round N, AI 4): a
// step that also lacks another input is still refused for the key, with the
// refusal's own line, and no step runs.
func TestRunRefusesAnAPIKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		// env changes the rest of the step's environment, when set.
		env func(env map[string]string)
	}{
		{name: "an API key", value: "sk-ant-invented"},
		{name: "an API key set to nothing", value: ""},
		{
			name:  "an API key and no token",
			value: "sk-ant-invented",
			env:   func(env map[string]string) { delete(env, "CLAUDE_CODE_OAUTH_TOKEN") },
		},
		{
			name:  "an API key and a canary that is no canary",
			value: "sk-ant-invented",
			env:   func(env map[string]string) { env["SHIFT2EU_AUDIT_CANARY"] = "not-a-canary" },
		},
		{
			name:  "an API key and no $GITHUB_ENV",
			value: "sk-ant-invented",
			env:   func(env map[string]string) { delete(env, "GITHUB_ENV") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			env := stepEnvironment(t, dir)
			env["ANTHROPIC_API_KEY"] = tc.value

			if tc.env != nil {
				tc.env(env)
			}

			args := stepArgs(dir)

			result := runAuditrun(t, args, env)

			if result.code != audit.ExitRefused {
				t.Errorf("the run's exit code is %d, want %d (audit.ExitRefused): the step holds ANTHROPIC_API_KEY, which the audit never uses, and the key's refusal comes before the inputs are read\nstdout: %s\nstderr: %s",
					result.code, audit.ExitRefused, result.stdout, result.stderr)
			}

			if !strings.Contains(result.stdout, lineAPIKey) {
				t.Errorf("the run printed %q, want the refusal's own line %q: a step that holds the key is told so", result.stdout, lineAPIKey)
			}

			if len(result.ran) != 0 {
				t.Errorf("the steps %v ran; the API key refuses the run before the CLI starts", result.ran)
			}

			assertNoSecret(t, result)
		})
	}
}

// TestRunRefusesAnExistingResultPathOrARefusedResult pins the result file's two
// edges: a path that already exists is refused rather than overwritten (O_EXCL),
// its content left as it was, and a result the audit step refused writes no file
// at all. Both end the run with the audit's line.
func TestRunRefusesAnExistingResultPathOrARefusedResult(t *testing.T) {
	t.Parallel()

	t.Run("a result path that already exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := resultOutPath(dir)
		writeEnvFile(t, path, "the file the step already had\n")

		result := runAuditrun(t, stepArgs(dir), stepEnvironment(t, dir))

		if result.code != audit.ExitFailed {
			t.Errorf("the run's exit code is %d, want %d (audit.ExitFailed): -result-out is opened O_EXCL and never overwrites a file\nstdout: %s\nstderr: %s",
				result.code, audit.ExitFailed, result.stdout, result.stderr)
		}

		if !strings.Contains(result.stdout, "the audit failed") {
			t.Errorf("the run printed %q, want the audit's line: the result cannot be written", result.stdout)
		}

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the file the run found: %v", err)
		}

		if string(body) != "the file the step already had\n" {
			t.Errorf("the existing file holds %q, want its content untouched: the run refuses the path, it does not overwrite it", body)
		}

		assertNoSecret(t, result)
	})

	t.Run("a result the audit step refused", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := resultOutPath(dir)

		recorder := &stepRecorder{auditErr: errors.New("the result file is no single JSON value")}
		result := runAuditrunWith(t, stepArgs(dir), stepEnvironment(t, dir), recorder)

		if result.code != audit.ExitFailed {
			t.Errorf("the run's exit code is %d, want %d (audit.ExitFailed): the result check refused the audit's bytes\nstdout: %s\nstderr: %s",
				result.code, audit.ExitFailed, result.stdout, result.stderr)
		}

		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the run wrote a result at %s (stat: %v), want nothing: a result the check refused is never written", path, err)
		}

		assertNoSecret(t, result)
	})
}

// TestResultWriteLeavesNoFileWhenTheWriteOrCloseFails pins doc.go's "a failed
// run writes no file": the result file the run created is removed when the
// write or the close fails, so what stays behind is neither a partial result
// nor a file an encrypt step would read. The file the writer is handed is this
// file's fake, which writes part of the body and then fails, or fails to
// close it, exactly as a full disk or a failing close would.
func TestResultWriteLeavesNoFileWhenTheWriteOrCloseFails(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// after is how many bytes of the body the write takes before it fails:
		// the whole body for a close that fails.
		after    int
		closeErr error
		// want is the error the writer's error must wrap.
		want error
	}{
		{name: "a write that fails after part of the body", after: 7, want: errTestWrite},
		{name: "a close that fails", after: len(auditResult), closeErr: errTestClose, want: errTestClose},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := resultOutPath(dir)

			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				t.Fatalf("open the result file %s: %v", path, err)
			}

			err = writeResult(path, &failingFile{file: file, after: tc.after, closeErr: tc.closeErr}, []byte(auditResult))
			if !errors.Is(err, tc.want) {
				t.Errorf("writeResult(%s) = %v, want an error wrapping %v: a write or a close that fails fails the run", path, err, tc.want)
			}

			if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("the result file at %s is still there (stat: %v), want it removed: a write or a close that fails leaves no file, so no encrypt step reads a partial result (doc.go)", path, statErr)
			}
		})
	}
}

// errTestWrite and errTestClose are the failures the fake result file answers:
// a write that stops after part of the body, and a close that fails.
// errTestChecksum is the checksum step's refusal - a tarball that does not
// match its pin - which ends a run before its audit step.
var (
	errTestWrite    = errors.New("the write failed after part of the body")
	errTestClose    = errors.New("the close failed")
	errTestChecksum = errors.New("the tarball does not match its pin")
)

// failingFile is the open result file writeResult is handed, as a failing disk
// answers one: Write writes at most after bytes of the body and then fails, and
// Close fails when closeErr is set. It stands in for the *os.File resultWriter
// opens, so a test can fail the write or the close where no file system would
// fail on demand.
type failingFile struct {
	file     *os.File
	after    int
	closeErr error
}

// Write writes the first after bytes of body to the file and fails unless that
// is the whole body.
func (f *failingFile) Write(body []byte) (int, error) {
	n := min(f.after, len(body))

	if n > 0 {
		if _, err := f.file.Write(body[:n]); err != nil {
			return 0, err
		}
	}

	if n < len(body) {
		return n, errTestWrite
	}

	return n, nil
}

// Close closes the file, and fails when closeErr is set.
func (f *failingFile) Close() error {
	if f.closeErr != nil {
		_ = f.file.Close()

		return f.closeErr
	}

	return f.file.Close()
}

// TestRunPrintsNoSecret pins the rule every case above leans on: neither the
// token nor the canary is on stdout, on stderr or in the step's summary, on a
// run that finishes and on one that is refused.
func TestRunPrintsNoSecret(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	assertNoSecret(t, runAuditrun(t, stepArgs(dir), stepEnvironment(t, dir)))

	refused := stepEnvironment(t, dir)
	refused["ANTHROPIC_API_KEY"] = "sk-ant-invented"

	assertNoSecret(t, runAuditrun(t, stepArgs(dir), refused))
}

// anotherDirectoryOf returns the path of name in a directory that is not the
// runner's file-command directory of env: a sibling directory nothing else of
// the step's names, so the file command's path is one the digest never covers.
func anotherDirectoryOf(env map[string]string, name string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(env["GITHUB_ENV"])), "another-directory", name)
}

// stepEnvironment is the model step's environment: the two secrets, the paths of
// the runner's file commands - $GITHUB_ENV, $GITHUB_PATH, $GITHUB_OUTPUT,
// $GITHUB_STATE - and the step summary, all of them in the one directory of dir
// the launch digests (issues #615, #614).
func stepEnvironment(t *testing.T, dir string) map[string]string {
	t.Helper()

	githubEnv := filepath.Join(dir, "github-env")
	writeEnvFile(t, githubEnv, "SOMETHING=else\n")

	commands := map[string]string{
		"GITHUB_ENV":    githubEnv,
		"GITHUB_PATH":   filepath.Join(dir, "github-path"),
		"GITHUB_OUTPUT": filepath.Join(dir, "github-output"),
		"GITHUB_STATE":  filepath.Join(dir, "github-state"),
	}

	for _, name := range []string{"GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE"} {
		writeEnvFile(t, commands[name], "")
	}

	return map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": auditToken,
		"SHIFT2EU_AUDIT_CANARY":   auditCanary,
		"GITHUB_ENV":              githubEnv,
		"GITHUB_PATH":             commands["GITHUB_PATH"],
		"GITHUB_OUTPUT":           commands["GITHUB_OUTPUT"],
		"GITHUB_STATE":            commands["GITHUB_STATE"],
		"GITHUB_STEP_SUMMARY":     filepath.Join(dir, "step-summary.md"),
	}
}

// stepArgs is the command line the install step and the pin hand the run: the
// tarball it kept, the integrity it checked, and where the checked result is
// written.
func stepArgs(dir string) []string {
	return []string{
		"-claude-tarball", filepath.Join(dir, "claude-code.tgz"),
		"-claude-integrity", auditIntegrity,
		"-result-out", resultOutPath(dir),
	}
}

// resultOutPath is where stepArgs has the run write the checked result: under
// the model step's $RUNNER_TEMP, which the sandbox can neither read nor write.
func resultOutPath(dir string) string {
	return filepath.Join(dir, "result.json")
}

// assertResultFile fails unless path holds exactly want, as a regular file with
// mode 0600.
func assertResultFile(t *testing.T, path, want string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the run wrote no result at %s: %v; -result-out takes the bytes the audit step checked, exactly", path, err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the result file's mode is %04o, want 0600: the result stays the step's own", mode)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the result file %s: %v", path, err)
	}

	if string(body) != want {
		t.Errorf("the result file holds %q, want exactly the audit step's checked bytes %q", body, want)
	}
}

// writeEnvFile writes body to path.
func writeEnvFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// assertOnlyTheListedVariables fails when the run read a variable outside the
// ADR's list, or read none of them.
func assertOnlyTheListedVariables(t *testing.T, asked []string) {
	t.Helper()

	if len(asked) == 0 {
		t.Error("the run read no variable at all; its inputs come from the step's environment (ADR 0040)")

		return
	}

	unique := slices.Clone(asked)
	slices.Sort(unique)
	unique = slices.Compact(unique)

	for _, name := range unique {
		if !slices.Contains(allowedVariables, name) {
			t.Errorf("the run read %s, which ADR 0040 does not list: its inputs are CLAUDE_CODE_OAUTH_TOKEN, SHIFT2EU_AUDIT_CANARY, GITHUB_ENV, GITHUB_PATH, GITHUB_OUTPUT, GITHUB_STATE and GITHUB_STEP_SUMMARY, and ANTHROPIC_API_KEY alone to refuse it (issues #615, #614)", name)
		}
	}

	for _, name := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "SHIFT2EU_AUDIT_CANARY", "GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE", "GITHUB_STEP_SUMMARY", "ANTHROPIC_API_KEY"} {
		if !slices.Contains(unique, name) {
			t.Errorf("the run never read %s; it is one of the step's inputs, and the runner's file commands are refused when their path is not the $GITHUB_ENV file's own directory (ADR 0040, issues #615, #614)", name)
		}
	}
}

// assertNoSecret fails when what the run printed holds the token or the canary.
func assertNoSecret(t *testing.T, result runResult) {
	t.Helper()

	printed := []struct {
		where string
		text  string
	}{
		{"stdout", result.stdout},
		{"stderr", result.stderr},
		{"the step's summary", result.summary},
	}

	for _, secret := range []string{auditToken, auditCanary} {
		for _, out := range printed {
			if strings.Contains(out.text, secret) {
				t.Errorf("the run printed the step's secret in %s: %q", out.where, out.text)
			}
		}
	}
}
