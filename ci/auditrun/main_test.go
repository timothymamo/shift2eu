// Pins ci/auditrun's entry point, the launcher the audit's model step runs
// (ADR 0040's milestone-2 amendment, issue #277, "The audit's programs"): its
// two flags, the four variables it reads from the step's environment, and the
// exit code each refusal ends with - 2 for a usage or input error, 3 for a run
// refused before the CLI started. Its steps are fakes, so nothing here reads a
// tarball, starts a sandbox, runs a model or takes a managed-settings decision;
// ci/internal/audit's tests pin those.
//
// The tests are white-box, package main, as ci/claudecodepin's are: Go refuses
// an import of a main package, so only its own test files reach run. Nothing the
// run prints may hold the token or the canary, which the test invents.
package main

import (
	"bytes"
	"context"
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
	auditToken  = "oauth-sentinel-token-never-printed"
	auditCanary = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	// auditIntegrity is an integrity of the shape -claude-integrity takes: npm's
	// sha512 of the tarball, in base64.
	auditIntegrity = "sha512-" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
)

// allowedVariables are the variables the run may read: the two secrets, the
// two paths it is given, and the API key it refuses.
var allowedVariables = []string{
	"ANTHROPIC_API_KEY",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"GITHUB_ENV",
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

// stepRecorder is the fake Steps: each step records that it ran.
type stepRecorder struct {
	ran []string
}

// steps is the recorder as the run's six steps, which all pass.
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
		Audit:           func(context.Context, string) error { return record("audit") },
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
// its steps being a fake recorder, and reads back everything it wrote.
func runAuditrun(t *testing.T, args []string, env map[string]string) runResult {
	t.Helper()

	lookup := &environ{values: env}
	recorder := &stepRecorder{}

	var stdout, stderr bytes.Buffer

	code := run(args, lookup.lookup, recorder.steps(), &stdout, &stderr)

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

	assertOnlyTheListedVariables(t, result.asked)
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
func TestRunRefusesAnAPIKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{name: "an API key", value: "sk-ant-invented"},
		{name: "an API key set to nothing", value: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			env := stepEnvironment(t, dir)
			env["ANTHROPIC_API_KEY"] = tc.value

			result := runAuditrun(t, stepArgs(dir), env)

			if result.code != audit.ExitRefused {
				t.Errorf("the run's exit code is %d, want %d (audit.ExitRefused): the step holds ANTHROPIC_API_KEY, which the audit never uses\nstdout: %s\nstderr: %s",
					result.code, audit.ExitRefused, result.stdout, result.stderr)
			}

			if len(result.ran) != 0 {
				t.Errorf("the steps %v ran; the API key refuses the run before the CLI starts", result.ran)
			}

			assertNoSecret(t, result)
		})
	}
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

// stepEnvironment is the model step's environment: the two secrets, the
// $GITHUB_ENV file probe 6 watches and the step summary, under dir.
func stepEnvironment(t *testing.T, dir string) map[string]string {
	t.Helper()

	githubEnv := filepath.Join(dir, "github-env")
	writeEnvFile(t, githubEnv, "SOMETHING=else\n")

	return map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": auditToken,
		"SHIFT2EU_AUDIT_CANARY":   auditCanary,
		"GITHUB_ENV":              githubEnv,
		"GITHUB_STEP_SUMMARY":     filepath.Join(dir, "step-summary.md"),
	}
}

// stepArgs is the command line the install step and the pin hand the run: the
// tarball it kept and the integrity it checked.
func stepArgs(dir string) []string {
	return []string{
		"-claude-tarball", filepath.Join(dir, "claude-code.tgz"),
		"-claude-integrity", auditIntegrity,
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
			t.Errorf("the run read %s, which ADR 0040 does not list: its inputs are CLAUDE_CODE_OAUTH_TOKEN, SHIFT2EU_AUDIT_CANARY, GITHUB_ENV and GITHUB_STEP_SUMMARY, and ANTHROPIC_API_KEY alone to refuse it", name)
		}
	}

	for _, name := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "SHIFT2EU_AUDIT_CANARY", "GITHUB_ENV", "GITHUB_STEP_SUMMARY", "ANTHROPIC_API_KEY"} {
		if !slices.Contains(unique, name) {
			t.Errorf("the run never read %s; it is one of the step's inputs (ADR 0040)", name)
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
