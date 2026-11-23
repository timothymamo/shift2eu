package main

// Group N of PR 4a: the entry point, ci/auditpublish's run (PRD #175 milestone
// 2, ADR 0040, "The publish program's GitHub side"). PR 4a builds the artifacts
// subcommand alone: the publish subcommand and its two variables land with PR
// 4b, so here every other subcommand is a refusal. What the run reads is the
// ADR's list of variables and no other; four names refuse the whole run by
// being set at all; every outcome is one line, on stdout and in the step's
// summary, with its exit code; and no token, repository, run ID or tool output
// reaches any of them.
//
// The tests are white-box, package main, as ci/auditrun's are: Go refuses an
// import of a main package, so only its own test files reach run. Nothing here
// reads the process's environment (forbidigo audit-publish-main-env): the run's
// lookup is a fake whose answers the test writes.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const (
	// The fixture values a run's environment carries: a sanitised repository,
	// the server's URL, this run's ID, and the line a summary file already
	// holds before the run appends its own.
	fixtureServerURL  = "https://github.com"
	fixtureRepository = "example/shift2eu"
	fixtureRunID      = "1234567890"
	summaryExisting   = "the summary's own first line\n"

	// The four lines the run prints, of the four outcomes PR 4a has (ADR 0040;
	// the artifact check's line and code come from ArtifactsOutcome).
	lineAsExpected     = "artifacts: as expected"
	lineUnexpected     = "artifacts: unexpected: see runbook step 11"
	lineGitHubFailed   = "GitHub call failed: see runbook"
	lineConfigInvalid  = "configuration invalid: see runbook"
	codeAsExpected     = 0
	codeUnexpected     = 3
	codeGitHubFailed   = 8
	codeConfigInvalid  = 9
	jobModel           = "model"
	jobPublish         = "publish"
	artifactChecksName = "checks-result"
	artifactModelName  = "model-result"

	// valueMarker is text only an invented value of a test holds, so a test can
	// show that no argument value reaches the output.
	valueMarker = "MARKER-VALUE-NEVER-PRINTED"
)

// refusedVariables are the variables whose presence alone refuses the run: none
// belongs in the step's environment, set even to nothing (ADR 0040).
var refusedVariables = []string{
	"AUDIT_APP_PRIVATE_KEY",
	"AUDIT_MODEL_PRIVATE_KEY",
	"AUDIT_PUBLISH_PRIVATE_KEY",
	"CLAUDE_CODE_OAUTH_TOKEN",
}

// readVariables are the variables the artifacts subcommand reads from the
// step's environment (ADR 0040): the token, the server, the repository, this
// run's ID and the step summary. GITHUB_SHA and AUDIT_FINGERPRINT_KEY belong to
// the publish subcommand, which is PR 4b's; PR 4a reads neither.
var readVariables = []string{
	"GH_TOKEN",
	"GITHUB_SERVER_URL",
	"GITHUB_REPOSITORY",
	"GITHUB_RUN_ID",
	"GITHUB_STEP_SUMMARY",
}

// environ is the fake environment: it answers from a map and records every name
// the run asked for, so a test reads which variables the run touches. A name in
// the map is set, even to the empty string, as os.LookupEnv's ok says.
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

// runResult is what one run of the entry point did.
type runResult struct {
	code    int
	stdout  string
	stderr  string
	summary string
	asked   []string
}

// output is what the run wrote to its two streams.
func (r runResult) output() string {
	return r.stdout + r.stderr
}

// fixedClock is the run's clock: the run's UTC date is 2026-12-03, which the
// artifacts subcommand does not print but the GitHub side's scope carries.
func fixedClock() time.Time {
	return time.Date(2026, time.December, 3, 9, 0, 0, 0, time.UTC)
}

// runProgram runs the entry point with args and the environment env answers,
// and reads back everything it wrote, the step summary included.
func runProgram(t *testing.T, args []string, env map[string]string) runResult {
	t.Helper()

	lookup := &environ{values: env}

	var stdout, stderr bytes.Buffer

	code := run(t.Context(), args, lookup.lookup, fixedClock, &stdout, &stderr)

	result := runResult{
		code:   code,
		stdout: stdout.String(),
		stderr: stderr.String(),
		asked:  lookup.asked,
	}

	if summary, ok := env["GITHUB_STEP_SUMMARY"]; ok {
		body, err := readSummary(summary)
		if err != nil {
			t.Fatalf("read the step summary %s: %v", summary, err)
		}

		result.summary = body
	}

	return result
}

// runToken returns a token invented for one test.
func runToken(t *testing.T) string {
	t.Helper()

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}

	return "test-token-" + hex.EncodeToString(b)
}

// stepEnvironment returns a whole environment of one run whose step summary is
// at summaryPath.
func stepEnvironment(t *testing.T, summaryPath string) map[string]string {
	t.Helper()

	return map[string]string{
		"GH_TOKEN":            runToken(t),
		"GITHUB_SERVER_URL":   fixtureServerURL,
		"GITHUB_REPOSITORY":   fixtureRepository,
		"GITHUB_RUN_ID":       fixtureRunID,
		"GITHUB_STEP_SUMMARY": summaryPath,
	}
}

// artifactArgs returns the artifacts subcommand's command line for job, against
// ghPath and tempDir.
func artifactArgs(job, ghPath, tempDir string) []string {
	return []string{"artifacts", "-job", job, "-gh", ghPath, "-tmp", tempDir}
}

// writeSummary makes the step's summary file under a temporary directory of its
// own, holding the content a step could already have written, and returns its
// path.
func writeSummary(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, []byte(summaryExisting), 0o600); err != nil {
		t.Fatalf("write the step summary %s: %v", path, err)
	}

	return path
}

// readSummary reads the step's summary file, empty when there is none.
func readSummary(path string) (string, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return string(body), nil
}

// artifactListing is the run's artifact listing GitHub answers with.
func artifactListing(total int, names ...string) map[string]any {
	artifacts := make([]map[string]any, 0, len(names))

	for _, name := range names {
		artifacts = append(artifacts, map[string]any{"name": name, "expired": false})
	}

	return map[string]any{"total_count": total, "artifacts": artifacts}
}

// TestRunArtifactsReadsTheListedVariablesAndNoOther pins the variables the
// artifacts subcommand reads: GH_TOKEN, GITHUB_SERVER_URL, GITHUB_REPOSITORY,
// GITHUB_RUN_ID and GITHUB_STEP_SUMMARY, and no other but the four names it
// asks for presence alone. A whole run is compared, so a variable the step's
// environment carries, GITHUB_SHA and AUDIT_FINGERPRINT_KEY among them, fails
// the same comparison as any other name.
func TestRunArtifactsReadsTheListedVariablesAndNoOther(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		job   string
		names []string
	}{
		{name: "the model job", job: jobModel, names: []string{artifactChecksName}},
		{name: "the publish job", job: jobPublish, names: []string{artifactChecksName, artifactModelName}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(len(tc.names), tc.names...)})
			result := runProgram(t, artifactArgs(tc.job, fake.Path, t.TempDir()), stepEnvironment(t, writeSummary(t)))

			if result.code != codeAsExpected {
				t.Fatalf("the run's exit code is %d, want %d: %s\nstdout: %s\nstderr: %s",
					result.code, codeAsExpected, lineAsExpected, result.stdout, result.stderr)
			}

			want := append(slices.Clone(readVariables), refusedVariables...)
			slices.Sort(want)

			got := slices.Clone(result.asked)
			slices.Sort(got)
			got = slices.Compact(got)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("the variables the run read (-want +got):\n%s", diff)
			}
		})
	}
}

// TestRunRefusesASecretInItsEnvironment pins the four names that refuse the
// whole run by being set at all, even to nothing: exit 9, the configuration's
// one line, nothing on stderr, and no gh call.
func TestRunRefusesASecretInItsEnvironment(t *testing.T) {
	t.Parallel()

	for _, name := range refusedVariables {
		for _, set := range []struct {
			name  string
			value string
		}{
			{name: "a value", value: "invented-" + valueMarker},
			{name: "an empty value", value: ""},
		} {
			t.Run(name+" set to "+set.name, func(t *testing.T) {
				t.Parallel()

				fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(1, artifactChecksName)})
				env := stepEnvironment(t, writeSummary(t))
				env[name] = set.value

				result := runProgram(t, artifactArgs(jobModel, fake.Path, t.TempDir()), env)

				if result.code != codeConfigInvalid {
					t.Errorf("the run's exit code is %d, want %d: %s", result.code, codeConfigInvalid, lineConfigInvalid)
				}

				if want := lineConfigInvalid + "\n"; result.stdout != want {
					t.Errorf("the run's stdout is %q, want %q: a refused configuration is the run's one line", result.stdout, want)
				}

				if result.stderr != "" {
					t.Errorf("the run's stderr is %q, want nothing: only a usage error writes there", result.stderr)
				}

				if calls := fake.Calls(t); len(calls) != 0 {
					t.Errorf("the run made %d gh calls, want none: a refused run starts nothing", len(calls))
				}
			})
		}
	}
}

// TestRunRefusesAConfigurationItCannotUse pins the refusals of a command line or
// an environment the run cannot use: exit 9, the configuration's one line, no
// gh call, and no argument value in anything it writes. The line's one stream is
// left free where ADR 0040 allows a usage error its own constant text; the test
// beside this one pins that text as constant.
func TestRunRefusesAConfigurationItCannotUse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// args and env change the run's command line and environment, when set;
		// the case names the values it passes, none of which may be printed.
		args    func(ghPath, tempDir string) []string
		env     func(env map[string]string)
		refused []string
	}{
		{
			name:    "no GH_TOKEN",
			env:     func(env map[string]string) { delete(env, "GH_TOKEN") },
			refused: []string{"GH_TOKEN"},
		},
		{
			name:    "an empty GH_TOKEN",
			env:     func(env map[string]string) { env["GH_TOKEN"] = "" },
			refused: []string{},
		},
		{
			name: "no GITHUB_REPOSITORY",
			env:  func(env map[string]string) { delete(env, "GITHUB_REPOSITORY") },
		},
		{
			name: "no GITHUB_RUN_ID",
			env:  func(env map[string]string) { delete(env, "GITHUB_RUN_ID") },
		},
		{
			name: "a relative gh path",
			args: func(_, _ string) []string {
				return artifactArgs(jobModel, "bin/"+valueMarker, "")
			},
			refused: []string{"bin/" + valueMarker},
		},
		{
			name: "a relative temporary directory",
			args: func(ghPath, _ string) []string {
				return artifactArgs(jobModel, ghPath, "tmp/"+valueMarker)
			},
			refused: []string{"tmp/" + valueMarker},
		},
		{
			name: "a temporary directory that does not exist",
			args: func(ghPath, tempDir string) []string {
				return artifactArgs(jobModel, ghPath, tempDir+"/"+valueMarker)
			},
			refused: []string{valueMarker},
		},
		{
			name: "an unknown job",
			args: func(ghPath, tempDir string) []string {
				return artifactArgs(valueMarker, ghPath, tempDir)
			},
			refused: []string{valueMarker},
		},
		{
			name: "a repository without an owner",
			env: func(env map[string]string) {
				env["GITHUB_REPOSITORY"] = valueMarker
			},
			refused: []string{valueMarker},
		},
		{
			name: "a run ID that is not a number",
			env: func(env map[string]string) {
				env["GITHUB_RUN_ID"] = "12" + valueMarker
			},
			refused: []string{"12" + valueMarker},
		},
		{
			name: "no subcommand",
			args: func(_, _ string) []string { return nil },
		},
		{
			name: "the publish subcommand, which is PR 4b's",
			args: func(ghPath, tempDir string) []string {
				return []string{jobPublish, "-gh", ghPath, "-tmp", tempDir}
			},
		},
		{
			name:    "an unknown subcommand",
			args:    func(_, _ string) []string { return []string{valueMarker} },
			refused: []string{valueMarker},
		},
		{
			name: "an extra argument",
			args: func(ghPath, tempDir string) []string {
				return append(artifactArgs(jobModel, ghPath, tempDir), valueMarker)
			},
			refused: []string{valueMarker},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(1, artifactChecksName)})
			tempDir := t.TempDir()
			env := stepEnvironment(t, writeSummary(t))

			args := artifactArgs(jobModel, fake.Path, tempDir)
			if tc.args != nil {
				args = tc.args(fake.Path, tempDir)
			}

			if tc.env != nil {
				tc.env(env)
			}

			result := runProgram(t, args, env)

			if result.code != codeConfigInvalid {
				t.Errorf("the run's exit code is %d, want %d: %s\nstdout: %s\nstderr: %s",
					result.code, codeConfigInvalid, lineConfigInvalid, result.stdout, result.stderr)
			}

			if !strings.Contains(result.output(), lineConfigInvalid) {
				t.Errorf("the run printed %q, want it to name %q: the runbook's step 11 is what a refused run points at",
					result.output(), lineConfigInvalid)
			}

			if calls := fake.Calls(t); len(calls) != 0 {
				t.Errorf("the run made %d gh calls, want none: a refused run starts nothing", len(calls))
			}

			for _, forbidden := range append(slices.Clone(tc.refused), env["GH_TOKEN"], fakeOutputMarker) {
				if forbidden == "" {
					continue
				}

				if strings.Contains(result.output(), forbidden) {
					t.Errorf("the run printed %q, want it to hold no argument value (%q): an error names a rule, never a value",
						result.output(), forbidden)
				}
			}
		})
	}
}

// TestRunRefusesWithAConstantText pins the other half of a usage error's text:
// two runs refused for the same reason print the same thing, whatever value the
// command line or the environment carried. ADR 0040 lets a usage error write its
// own text to stderr, which a value must never be part of.
func TestRunRefusesWithAConstantText(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(1, artifactChecksName)})
	tempDir := t.TempDir()

	for _, tc := range []struct {
		name string
		// args and env are the two runs' command lines and environments: they
		// differ in one value alone.
		args [2][]string
		env  func(env map[string]string, i int)
	}{
		{
			name: "an unknown subcommand",
			args: [2][]string{
				{"deploy-" + valueMarker},
				{"a-longer-unknown-subcommand-" + valueMarker},
			},
		},
		{
			name: "an unknown job",
			args: [2][]string{
				artifactArgs("widget-"+valueMarker, fake.Path, tempDir),
				artifactArgs("sprocket-"+valueMarker, fake.Path, tempDir),
			},
		},
		{
			name: "a repository without an owner",
			args: [2][]string{
				artifactArgs(jobModel, fake.Path, tempDir),
				artifactArgs(jobModel, fake.Path, tempDir),
			},
			env: func(env map[string]string, i int) {
				env["GITHUB_REPOSITORY"] = []string{"shift2eu-" + valueMarker, "other-" + valueMarker}[i]
			},
		},
		{
			name: "a run ID that is not a number",
			args: [2][]string{
				artifactArgs(jobModel, fake.Path, tempDir),
				artifactArgs(jobModel, fake.Path, tempDir),
			},
			env: func(env map[string]string, i int) {
				env["GITHUB_RUN_ID"] = []string{"12" + valueMarker, "9" + valueMarker}[i]
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var outputs [2]string

			for i := range tc.args {
				env := stepEnvironment(t, writeSummary(t))
				if tc.env != nil {
					tc.env(env, i)
				}

				result := runProgram(t, tc.args[i], env)
				outputs[i] = result.output()
			}

			if outputs[0] != outputs[1] {
				t.Errorf("the two refusals print %q and %q, want the same text: a usage error's text is constant and holds no value",
					outputs[0], outputs[1])
			}
		})
	}
}

// TestRunArtifactsPrintsOneLineAndTheSummary pins a whole run: exit 0, exactly
// the one line "artifacts: as expected" on stdout, nothing on stderr, and the
// same line appended to the step's summary, whose own content stays.
func TestRunArtifactsPrintsOneLineAndTheSummary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		job   string
		names []string
	}{
		{name: "the model job", job: jobModel, names: []string{artifactChecksName}},
		{name: "the publish job", job: jobPublish, names: []string{artifactChecksName, artifactModelName}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(len(tc.names), tc.names...)})
			summaryPath := writeSummary(t)
			result := runProgram(t, artifactArgs(tc.job, fake.Path, t.TempDir()), stepEnvironment(t, summaryPath))

			if result.code != codeAsExpected {
				t.Fatalf("the run's exit code is %d, want %d\nstdout: %s\nstderr: %s",
					result.code, codeAsExpected, result.stdout, result.stderr)
			}

			if want := lineAsExpected + "\n"; result.stdout != want {
				t.Errorf("the run's stdout is %q, want exactly %q: the run prints one line", result.stdout, want)
			}

			if result.stderr != "" {
				t.Errorf("the run's stderr is %q, want nothing: the one line goes to stdout and the summary", result.stderr)
			}

			if !strings.HasPrefix(result.summary, summaryExisting) {
				t.Errorf("the step summary is %q, want it to keep its own content %q", result.summary, summaryExisting)
			}

			if !strings.HasSuffix(result.summary, lineAsExpected+"\n") {
				t.Errorf("the step summary is %q, want the run's line appended", result.summary)
			}

			if got := strings.Count(result.summary, lineAsExpected); got != 1 {
				t.Errorf("the step summary holds the run's line %d times, want once: %q", got, result.summary)
			}

			if calls := fake.Calls(t); len(calls) != 1 {
				t.Errorf("the run made %d gh calls, want one: the artifact check lists once", len(calls))
			}
		})
	}
}

// TestRunArtifactsPrintsTheLineOfTheChecksVerdict pins the two other endings of
// an artifact check: a listing that is not the job's is "artifacts: unexpected:
// see runbook step 11" (exit 3), and GitHub's own failure is "GitHub call
// failed: see runbook" (exit 8). Each is one line on stdout and nothing on
// stderr.
func TestRunArtifactsPrintsTheLineOfTheChecksVerdict(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		answer fakeAnswer
		want   string
		code   int
	}{
		{
			name:   "a listing with an artifact the job does not download",
			answer: fakeAnswer{Status: 200, Body: artifactListing(2, artifactChecksName, "unexpected-"+valueMarker)},
			want:   lineUnexpected,
			code:   codeUnexpected,
		},
		{
			name:   "a listing with an expected artifact missing",
			answer: fakeAnswer{Status: 200, Body: artifactListing(0)},
			want:   lineUnexpected,
			code:   codeUnexpected,
		},
		{
			name:   "a failed GitHub call",
			answer: fakeAnswer{Status: 500, Body: map[string]string{"message": valueMarker + " " + fakeOutputMarker}},
			want:   lineGitHubFailed,
			code:   codeGitHubFailed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.answer)
			result := runProgram(t, artifactArgs(jobModel, fake.Path, t.TempDir()), stepEnvironment(t, writeSummary(t)))

			if result.code != tc.code {
				t.Errorf("the run's exit code is %d, want %d: %s\nstdout: %s\nstderr: %s",
					result.code, tc.code, tc.want, result.stdout, result.stderr)
			}

			if want := tc.want + "\n"; result.stdout != want {
				t.Errorf("the run's stdout is %q, want exactly %q: the run prints one line", result.stdout, want)
			}

			if result.stderr != "" {
				t.Errorf("the run's stderr is %q, want nothing: no response's text reaches it", result.stderr)
			}
		})
	}
}

// TestRunKeepsTheRunsValuesOutOfItsOutput pins what no run prints, whatever its
// verdict: the token, the repository, this run's ID and the tool's own output
// are in none of the two streams and in no step summary.
func TestRunKeepsTheRunsValuesOutOfItsOutput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		answer fakeAnswer
	}{
		{name: "an accepted listing", answer: fakeAnswer{Status: 200, Body: artifactListing(1, artifactChecksName)}},
		{
			name:   "a failed GitHub call",
			answer: fakeAnswer{Status: 500, Body: map[string]string{"message": valueMarker + " " + fakeOutputMarker}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.answer)
			env := stepEnvironment(t, writeSummary(t))
			result := runProgram(t, artifactArgs(jobModel, fake.Path, t.TempDir()), env)

			for _, forbidden := range []string{env["GH_TOKEN"], fixtureRepository, fixtureRunID, fakeOutputMarker} {
				if forbidden == "" {
					continue
				}

				for _, printed := range []struct{ what, text string }{
					{what: "stdout", text: result.stdout},
					{what: "stderr", text: result.stderr},
					{what: "the step summary", text: result.summary},
				} {
					if strings.Contains(printed.text, forbidden) {
						t.Errorf("the run's %s holds %q: no token, repository, run ID or tool output is printed (%q)",
							printed.what, forbidden, printed.text)
					}
				}
			}
		})
	}
}

// TestFakeGHServesTheIncludeOutput pins the fake group N runs against: it writes
// --include's status line, headers, blank line and body, exits 0 on a 2xx
// status and 1 otherwise, and records the call.
func TestFakeGHServesTheIncludeOutput(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(1, artifactChecksName)})
	result := runProgram(t, artifactArgs(jobModel, fake.Path, t.TempDir()), stepEnvironment(t, writeSummary(t)))

	if result.code != codeAsExpected {
		t.Fatalf("the run against the fake gh exited %d, want %d: %s\nstderr: %s",
			result.code, codeAsExpected, lineAsExpected, result.stderr)
	}

	calls := fake.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("the fake recorded %d calls, want 1", len(calls))
	}

	if want := "repos/" + fixtureRepository + "/actions/runs/" + fixtureRunID + "/artifacts?per_page=100"; !slices.Contains(calls[0].Args, want) {
		t.Errorf("the fake was called with %v, want it to hold the artifact listing's endpoint %q", calls[0].Args, want)
	}
}
