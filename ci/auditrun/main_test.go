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
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
// step answers with the result body and error the test gives it.
type stepRecorder struct {
	ran      []string
	result   []byte
	auditErr error
}

// steps is the recorder as the run's six steps.
func (r *stepRecorder) steps() audit.Steps {
	record := func(step string) error {
		r.ran = append(r.ran, step)

		return nil
	}

	return audit.Steps{
		ManagedSettings: func(string) error { return record("managed settings") },
		Checksum:        func(string, audit.Inputs) error { return record("checksum") },
		Layout:          func(string) error { return record("audit directory") },
		Bubblewrap:      func(context.Context) error { return record("bubblewrap") },
		Preflight:       func(context.Context, string) error { return record("preflight") },
		Audit: func(context.Context, string) ([]byte, error) {
			r.ran = append(r.ran, "audit")

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

// TestRunTakesTheFileCommandPathsOfOneDirectoryWrittenAnotherWay pins the
// comparison the file commands' check makes (issues #615, #614, the auditor's
// N1): the paths are compared as the directories filepath.Clean names, so a
// $GITHUB_PATH or a step summary that names the $GITHUB_ENV file's own directory
// with a redundant separator is the same directory, and the run passes.
func TestRunTakesTheFileCommandPathsOfOneDirectoryWrittenAnotherWay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// pathOf writes the path of name in the directory dir, the way the case
		// spells the same directory.
		pathOf func(dir, name string) string
	}{
		{
			name:   "the directory with a redundant separator",
			pathOf: func(dir, name string) string { return dir + "//" + name },
		},
		{
			name:   "the directory with a . element",
			pathOf: func(dir, name string) string { return dir + "/./" + name },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			env := stepEnvironment(t, dir)
			env["GITHUB_PATH"] = tc.pathOf(dir, "github-path")
			env["GITHUB_STEP_SUMMARY"] = tc.pathOf(dir, "step-summary.md")

			result := runAuditrun(t, stepArgs(dir), env)

			if result.code != audit.ExitOK {
				t.Errorf("the run's exit code is %d, want %d (audit.ExitOK): every file command's path names the $GITHUB_ENV file's own directory, written another way\nstdout: %s\nstderr: %s",
					result.code, audit.ExitOK, result.stdout, result.stderr)
			}
		})
	}
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
var (
	errTestWrite = errors.New("the write failed after part of the body")
	errTestClose = errors.New("the close failed")
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
