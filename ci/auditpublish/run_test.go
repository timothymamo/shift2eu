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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// readVariables are the variables every subcommand reads from the step's
// environment (ADR 0040): the token, the server, the repository, this run's ID
// and the step summary. The artifacts subcommand reads no other but the four
// names it is asked for presence alone; the image subcommand also reads
// GITHUB_SHA, and the publish subcommand GITHUB_SHA and
// AUDIT_FINGERPRINT_KEY.
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
// at summaryPath. It carries the variables of every subcommand, the artifacts
// subcommand's GITHUB_SHA and AUDIT_FINGERPRINT_KEY among them, so that a
// subcommand that reads a variable it should not fails the same comparison as
// any other.
func stepEnvironment(t *testing.T, summaryPath string) map[string]string {
	t.Helper()

	return map[string]string{
		"GH_TOKEN":              runToken(t),
		"GITHUB_SERVER_URL":     fixtureServerURL,
		"GITHUB_REPOSITORY":     fixtureRepository,
		"GITHUB_RUN_ID":         fixtureRunID,
		"GITHUB_STEP_SUMMARY":   summaryPath,
		"GITHUB_SHA":            fixtureCommit,
		"AUDIT_FINGERPRINT_KEY": fixtureFingerprintKey,
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

// artifactListing is the run's artifact listing GitHub answers with. Each entry
// carries an ID and a sha256 digest, one per position, so the check has the
// values the download is bound to.
func artifactListing(total int, names ...string) map[string]any {
	artifacts := make([]map[string]any, 0, len(names))

	for i, name := range names {
		artifacts = append(artifacts, map[string]any{
			"name":    name,
			"expired": false,
			"id":      artifactListingID(i),
			"digest":  artifactListingDigest(i),
		})
	}

	return map[string]any{"total_count": total, "artifacts": artifacts}
}

// artifactListingID and artifactListingDigest are the ID and digest of the
// listing's entry at index i: invented, and different per position so that the
// tests can tell the entries apart.
func artifactListingID(i int) int64 { return int64(1000 + i) }

func artifactListingDigest(i int) string { return fmt.Sprintf("sha256:%064x", 1000+i) }

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

// TestRunArtifactsWritesTheCheckedArtifact pins the hand-off review round 1's
// finding A4 accepted: the artifacts subcommand writes the artifact the check
// accepted, with its ID and digest, to the fixed artifact.json under -tmp, and
// the workflow's download step reads them from there instead of listing the
// run's artifacts again. A refusal writes nothing. The file is opened O_EXCL:
// a path that already exists is refused rather than overwritten.
func TestRunArtifactsWritesTheCheckedArtifact(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		job      string
		names    []string
		download string
		index    int
	}{
		{name: "the model job", job: jobModel, names: []string{artifactChecksName}, download: artifactChecksName, index: 0},
		{name: "the publish job", job: jobPublish, names: []string{artifactChecksName, artifactModelName}, download: artifactModelName, index: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(len(tc.names), tc.names...)})
			tempDir := t.TempDir()

			result := runProgram(t, artifactArgs(tc.job, fake.Path, tempDir), stepEnvironment(t, writeSummary(t)))

			if result.code != codeAsExpected {
				t.Fatalf("the run's exit code is %d, want %d\nstdout: %s\nstderr: %s", result.code, codeAsExpected, result.stdout, result.stderr)
			}

			path := filepath.Join(tempDir, "artifact.json")

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("the run wrote no artifact file at %s: %v; the download is bound to the artifact the check accepted", path, err)
			}

			if mode := info.Mode().Perm(); mode != 0o600 {
				t.Errorf("the artifact file's mode is %04o, want 0600", mode)
			}

			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the artifact file %s: %v", path, err)
			}

			want := fmt.Sprintf("{\"name\":%q,\"id\":%d,\"digest\":%q}\n", tc.download, artifactListingID(tc.index), artifactListingDigest(tc.index))
			if string(body) != want {
				t.Errorf("the artifact file holds %q, want exactly %q: the checked artifact's name, ID and digest", body, want)
			}
		})
	}
}

// TestRunArtifactsWritesNothingWhenTheCheckRefusesOrThePathExists pins the two
// edges of that file: a listing the check refuses leaves no file behind, and a
// path that already exists is refused (O_EXCL) with its content untouched.
func TestRunArtifactsWritesNothingWhenTheCheckRefusesOrThePathExists(t *testing.T) {
	t.Parallel()

	t.Run("a listing the check refuses", func(t *testing.T) {
		t.Parallel()

		fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(2, artifactChecksName, "unexpected-"+valueMarker)})
		tempDir := t.TempDir()

		result := runProgram(t, artifactArgs(jobModel, fake.Path, tempDir), stepEnvironment(t, writeSummary(t)))

		if result.code != codeUnexpected {
			t.Fatalf("the run's exit code is %d, want %d\nstdout: %s\nstderr: %s", result.code, codeUnexpected, result.stdout, result.stderr)
		}

		if _, err := os.Stat(filepath.Join(tempDir, "artifact.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the refused run wrote an artifact file (stat: %v), want nothing: only a checked listing is written", err)
		}
	})

	t.Run("a path that already exists", func(t *testing.T) {
		t.Parallel()

		fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(1, artifactChecksName)})
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "artifact.json")

		if err := os.WriteFile(path, []byte("the file the step already had\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}

		result := runProgram(t, artifactArgs(jobModel, fake.Path, tempDir), stepEnvironment(t, writeSummary(t)))

		if result.code != codeConfigInvalid {
			t.Errorf("the run's exit code is %d, want %d (%q): the artifact file is opened O_EXCL\nstdout: %s\nstderr: %s",
				result.code, codeConfigInvalid, lineConfigInvalid, result.stdout, result.stderr)
		}

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the file the run found: %v", err)
		}

		if string(body) != "the file the step already had\n" {
			t.Errorf("the existing file holds %q, want its content untouched: the run refuses the path, it does not overwrite it", body)
		}
	})
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

// TestRunFailsClosedOnASummaryItCannotUse pins the step summary's fail-closed
// rule (review finding 1, ADR 0040): a run whose one line cannot reach the
// step's summary never passes. A run that would have passed prints the
// configuration's line and exits 9; a run that already failed keeps its own
// line and code, so a failed run's verdict is never lost. Either way the line
// goes to stdout alone, exactly once, and nothing reaches stderr.
func TestRunFailsClosedOnASummaryItCannotUse(t *testing.T) {
	t.Parallel()

	for _, summary := range []struct {
		name string
		// unusable changes env's GITHUB_STEP_SUMMARY to one the run cannot
		// append a line to: the runner's variable where it is set, none where
		// it is not.
		unusable func(t *testing.T, env map[string]string)
	}{
		{
			name:     "an unset summary",
			unusable: func(_ *testing.T, env map[string]string) { delete(env, "GITHUB_STEP_SUMMARY") },
		},
		{
			name:     "a relative summary",
			unusable: func(_ *testing.T, env map[string]string) { env["GITHUB_STEP_SUMMARY"] = "summary.md" },
		},
		{
			name: "a summary that does not exist",
			unusable: func(t *testing.T, env map[string]string) {
				t.Helper()

				env["GITHUB_STEP_SUMMARY"] = filepath.Join(t.TempDir(), "no-such-summary.md")
			},
		},
		{
			name: "a summary that is a directory",
			unusable: func(t *testing.T, env map[string]string) {
				t.Helper()

				env["GITHUB_STEP_SUMMARY"] = t.TempDir()
			},
		},
		{
			name: "a summary whose writes fail",
			unusable: func(t *testing.T, env map[string]string) {
				t.Helper()

				if runtime.GOOS != "linux" {
					t.Skip("the device whose writes fail (/dev/full) exists on Linux alone")
				}

				env["GITHUB_STEP_SUMMARY"] = "/dev/full"
			},
		},
	} {
		for _, outcome := range []struct {
			name   string
			answer fakeAnswer
			want   string
			code   int
		}{
			{
				name:   "a passing listing",
				answer: fakeAnswer{Status: 200, Body: artifactListing(1, artifactChecksName)},
				want:   lineConfigInvalid,
				code:   codeConfigInvalid,
			},
			{
				name:   "an extra-artifact listing",
				answer: fakeAnswer{Status: 200, Body: artifactListing(2, artifactChecksName, "unexpected-"+valueMarker)},
				want:   lineUnexpected,
				code:   codeUnexpected,
			},
		} {
			t.Run(summary.name+" and "+outcome.name, func(t *testing.T) {
				t.Parallel()

				fake := newFakeGH(t, outcome.answer)
				env := stepEnvironment(t, writeSummary(t))
				summary.unusable(t, env)

				lookup := &environ{values: env}

				var stdout, stderr bytes.Buffer

				code := run(t.Context(), artifactArgs(jobModel, fake.Path, t.TempDir()), lookup.lookup, fixedClock, &stdout, &stderr)

				if code != outcome.code {
					t.Errorf("the run's exit code is %d, want %d: %s", code, outcome.code, outcome.want)
				}

				if want := outcome.want + "\n"; stdout.String() != want {
					t.Errorf("the run's stdout is %q, want exactly %q: the run prints its one line and nothing else", stdout.String(), want)
				}

				if stderr.String() != "" {
					t.Errorf("the run's stderr is %q, want nothing: an unusable summary is no usage error", stderr.String())
				}
			})
		}
	}
}

// TestRunPublishAndImageFailClosedOnASummaryTheyCannotUse pins the report
// subcommands' half of the summary's fail-closed rule: a step summary the run
// cannot append to refuses the run before the subcommand starts, so a run that
// would have opened a pull request or filed an issue makes no gh call at all.
func TestRunPublishAndImageFailClosedOnASummaryTheyCannotUse(t *testing.T) {
	t.Parallel()

	for _, summary := range []struct {
		name string
		// unusable changes env's GITHUB_STEP_SUMMARY to one the run cannot
		// append a line to.
		unusable func(t *testing.T, env map[string]string)
	}{
		{
			name:     "an unset summary",
			unusable: func(_ *testing.T, env map[string]string) { delete(env, "GITHUB_STEP_SUMMARY") },
		},
		{
			name: "a summary that cannot be opened",
			unusable: func(t *testing.T, env map[string]string) {
				t.Helper()

				env["GITHUB_STEP_SUMMARY"] = t.TempDir()
			},
		},
	} {
		for _, tc := range []struct {
			name string
			// args builds the command line, and answers are the fake gh's: the
			// answers a run with a usable summary would read and write.
			args    func(ghPath, gitleaksPath, resultPath, reportsDir string) []string
			answers []fakeAnswer
		}{
			{
				name:    "the image subcommand",
				args:    func(ghPath, _, resultPath, _ string) []string { return imageArgs(ghPath, resultPath) },
				answers: imageAnswers(t),
			},
			{name: "the publish subcommand", args: publishArgs, answers: publishAnswers()},
		} {
			t.Run(summary.name+" and "+tc.name, func(t *testing.T) {
				t.Parallel()

				fake := newRoutedFakeGH(t, tc.answers...)
				env := stepEnvironment(t, writeSummary(t))
				summary.unusable(t, env)

				tempDir := t.TempDir()
				args := withTempDir(tc.args(fake.Path, newFakeGitleaks(t), writeResult(t), t.TempDir()), tempDir)

				lookup := &environ{values: env}

				var stdout, stderr bytes.Buffer

				code := run(t.Context(), args, lookup.lookup, fixedClock, &stdout, &stderr)

				if code != codeConfigInvalid {
					t.Errorf("the run's exit code is %d, want %d: %s\nstdout: %s\nstderr: %s",
						code, codeConfigInvalid, lineConfigInvalid, stdout.String(), stderr.String())
				}

				if want := lineConfigInvalid + "\n"; stdout.String() != want {
					t.Errorf("the run's stdout is %q, want exactly %q: the run prints its one line and nothing else", stdout.String(), want)
				}

				if stderr.String() != "" {
					t.Errorf("the run's stderr is %q, want nothing: an unusable summary is no usage error", stderr.String())
				}

				if calls := fake.Calls(t); len(calls) != 0 {
					t.Errorf("the run made %d gh call(s) before refusing its summary, want none: the subcommand never starts", len(calls))
				}
			})
		}
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

// Group G of PR 4b: the entry point's other two subcommands, image and
// publish (PRD #175 milestone 2, ADR 0040). image reads the result and the
// digest's attestation with the job's read-only token and prints "image
// digest: as expected"; publish reads the result, the previous report, the run,
// the fingerprint key and the scanner, and prints the run's counts. Both read
// GITHUB_SHA and refuse a server URL that is not exactly https://github.com,
// whose URL the report, the commit and the pull request name; publish reads
// AUDIT_FINGERPRINT_KEY too.
const (
	// fixtureDate is the run's UTC date, which fixedClock gives.
	fixtureDate = "2026-12-03"

	// fixtureCommit is GITHUB_SHA and the result's commit.
	fixtureCommit = "0123456789abcdef0123456789abcdef01234567"

	// The SHAs the Git data API answers a report publication with: each one
	// distinct, as the library's own tests pin them.
	fixtureBaseTreeSHA = "a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0"
	fixtureBlobSHA     = "b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0"
	fixtureTreeSHA     = "c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0"
	fixtureReportSHA   = "d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0"

	// fixtureTag and fixtureDigest are the result's release and image.
	fixtureTag    = "v0.12.0"
	fixtureDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	// fixtureFingerprintKey is an invented AUDIT_FINGERPRINT_KEY of the
	// shortest length the core takes.
	fixtureFingerprintKey = "invented-fingerprint-key-32-bytes"

	// fixtureBranch, fixtureBranchRef and fixtureReportPath are the branch and
	// the file the run's report is published as.
	fixtureBranch      = "security-audit/" + fixtureDate + "-" + fixtureRunID
	fixtureBranchRef   = "refs/heads/" + fixtureBranch
	fixtureReportPath  = "docs/security/audits/" + fixtureDate + "-" + fixtureRunID + ".md"
	fixtureRepoPrefix  = "repos/" + fixtureRepository
	fixtureIssuesQuery = fixtureRepoPrefix + "/issues?creator=shift2eu-audit%5Bbot%5D&state=all&per_page=100&page=1"

	// fixtureAdvisoriesEndpoint is the advisory listing's first page, which
	// every publish run reads (PR 4c).
	fixtureAdvisoriesEndpoint = fixtureRepoPrefix + "/security-advisories?per_page=100"

	// lineImageAsExpected and lineAuditPublished are the two subcommands'
	// lines of a run that passed, and lineNotFiled the private findings' line of
	// a run whose draft advisory GitHub refused (PR 4c).
	lineImageAsExpected = "image digest: as expected"
	lineAuditPublished  = "audit published: public findings 0, private findings: none"
	lineNotFiled        = "private findings not filed: see runbook"

	// codeNotFiled is the exit code of a run whose private findings were not
	// filed (ADR 0040, runbook step 11).
	codeNotFiled = 5
)

// usageTextOfEverySubcommand is what a command line the run cannot parse
// writes to stderr: one line per subcommand, its flags, and no value.
const usageTextOfEverySubcommand = "usage: auditpublish artifacts -job model|publish -gh <path> -tmp <dir>\n" +
	"       auditpublish image -result <path> -gh <path> -tmp <dir>\n" +
	"       auditpublish publish -result <path> -reports <dir> -gh <path> -gitleaks <path> -tmp <dir>\n"

// resultCheckIDs are the fifteen check ids of the result schema, in the
// coverage table's order.
var resultCheckIDs = []string{
	"dependencies", "history-secrets", "workflows", "scorecard", "scorecard-admin",
	"release-integrity", "image-pull", "binary-vulnerabilities", "image-scan", "image-runtime",
	"hostile-inventories", "fuzzing", "supply-chain", "tree-review", "agent-surface",
}

// resultDocument returns the model's result as the publish and image
// subcommands read it: the fixture commit, tag and digest, every check ran and
// no finding.
func resultDocument() map[string]any {
	checks := make([]any, 0, len(resultCheckIDs))

	for _, id := range resultCheckIDs {
		checks = append(checks, map[string]any{"id": id, "ran": true, "not_run": ""})
	}

	return map[string]any{
		"schema":       1,
		"commit":       fixtureCommit,
		"release_tag":  fixtureTag,
		"image_digest": fixtureDigest,
		"tools":        []any{},
		"checks":       checks,
		"findings":     []any{},
		"changes":      map[string]any{"dependencies": []any{}, "narrative": "No dependency changed."},
		"limits":       []any{},
	}
}

// resultDocumentWith returns the result document with findings.
func resultDocumentWith(findings ...map[string]any) map[string]any {
	doc := resultDocument()

	list := make([]any, 0, len(findings))
	for _, f := range findings {
		list = append(list, f)
	}

	doc["findings"] = list

	return doc
}

// publicFinding returns one confirmed public finding of the tree review: a low
// one of a public category, the shape the result schema takes.
func publicFinding() map[string]any {
	return map[string]any{
		"check":     "tree-review",
		"rule":      "G304",
		"file":      "internal/cli/run.go",
		"line":      12,
		"sink":      "os.Open",
		"severity":  "low",
		"confirmed": true,
		"category":  "hardening",
		"private":   false,
		"scenario":  "the scenario of G304",
		"evidence":  "the evidence of G304",
		"fix":       "the fix of G304",
		"dismissal": "",
	}
}

// privateFinding returns one confirmed private finding: a high one of a private
// category, which a run files as a draft advisory.
func privateFinding() map[string]any {
	f := publicFinding()
	f["rule"] = "G302"
	f["severity"] = "high"
	f["category"] = "injection"

	return f
}

// writeResultOf writes the result document to a fresh temporary directory and
// returns its path.
func writeResultOf(t *testing.T, document map[string]any) string {
	t.Helper()

	body, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal the result document: %v", err)
	}

	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write the result %s: %v", path, err)
	}

	return path
}

// writeResult writes the result document to a fresh temporary directory and
// returns its path.
func writeResult(t *testing.T) string {
	t.Helper()

	return writeResultOf(t, resultDocument())
}

// imageArgs returns the image subcommand's command line.
func imageArgs(ghPath, resultPath string) []string {
	return []string{"image", "-result", resultPath, "-gh", ghPath, "-tmp", "TMP"}
}

// publishArgs returns the publish subcommand's command line.
func publishArgs(ghPath, gitleaksPath, resultPath, reportsDir string) []string {
	return []string{
		"publish", "-result", resultPath, "-reports", reportsDir,
		"-gh", ghPath, "-gitleaks", gitleaksPath, "-tmp", "TMP",
	}
}

// withTempDir returns args whose placeholder TMP is tempDir: the tests build
// the command line before they have the run's temporary directory.
func withTempDir(args []string, tempDir string) []string {
	out := slices.Clone(args)

	for i, arg := range out {
		if arg == "TMP" {
			out[i] = tempDir
		}
	}

	return out
}

// imageAnswers returns the image subcommand's answers: the release and the
// digest's provenance attestation.
func imageAnswers(t *testing.T) []fakeAnswer {
	t.Helper()

	statement := map[string]any{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": []any{map[string]any{
			"name":   "ghcr.io/" + fixtureRepository,
			"digest": map[string]any{"sha256": strings.TrimPrefix(fixtureDigest, "sha256:")},
		}},
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": map[string]any{"buildDefinition": map[string]any{"externalParameters": map[string]any{
			"workflow": map[string]any{
				"ref":        "refs/tags/" + fixtureTag,
				"repository": fixtureServerURL + "/" + fixtureRepository,
				"path":       ".github/workflows/release.yml",
			},
		}}},
	}

	payload, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal the attestation's statement: %v", err)
	}

	return []fakeAnswer{
		{
			Method:   "GET",
			Endpoint: fixtureRepoPrefix + "/releases/tags/" + fixtureTag,
			Status:   200,
			Body:     map[string]string{"tag_name": fixtureTag},
		},
		{
			Method:   "GET",
			Endpoint: fixtureRepoPrefix + "/releases/latest",
			Status:   200,
			Body:     map[string]string{"tag_name": fixtureTag},
		},
		{
			Method:   "GET",
			Endpoint: fixtureRepoPrefix + "/attestations/" + fixtureDigest + "?per_page=100",
			Status:   200,
			Body: map[string]any{"attestations": []any{map[string]any{
				"repository_id": 1,
				"bundle": map[string]any{
					"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
					"dsseEnvelope": map[string]any{
						"payloadType": "application/vnd.in-toto+json",
						"payload":     base64.StdEncoding.EncodeToString(payload),
						"signatures":  []any{map[string]string{"sig": "AAAA"}},
					},
				},
			}}},
		},
	}
}

// publishAnswers returns the publish subcommand's answers: the release, the
// issues' one page, the advisory listing's one page and the report's writes.
func publishAnswers() []fakeAnswer {
	return []fakeAnswer{
		{Method: "GET", Endpoint: fixtureRepoPrefix + "/releases/tags/" + fixtureTag, Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
		{Method: "GET", Endpoint: fixtureRepoPrefix + "/releases/latest", Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
		{Method: "GET", Endpoint: fixtureIssuesQuery, Status: 200, Body: []any{}},
		{Method: "GET", Endpoint: fixtureAdvisoriesEndpoint, Status: 200, Body: []any{}},
		{Method: "GET", Endpoint: fixtureRepoPrefix + "/git/ref/heads/" + fixtureBranch, Status: 404, Body: map[string]string{"message": "Not Found"}},
		{
			Method:   "GET",
			Endpoint: fixtureRepoPrefix + "/git/commits/" + fixtureCommit,
			Status:   200,
			Body:     map[string]any{"sha": fixtureCommit, "tree": map[string]string{"sha": fixtureBaseTreeSHA}},
		},
		{Method: "POST", Endpoint: fixtureRepoPrefix + "/git/blobs", Status: 201, Body: map[string]string{"sha": fixtureBlobSHA}},
		{Method: "POST", Endpoint: fixtureRepoPrefix + "/git/trees", Status: 201, Body: map[string]string{"sha": fixtureTreeSHA}},
		{Method: "POST", Endpoint: fixtureRepoPrefix + "/git/commits", Status: 201, Body: map[string]string{"sha": fixtureReportSHA}},
		{
			Method:   "GET",
			Endpoint: fixtureRepoPrefix + "/compare/" + fixtureCommit + "..." + fixtureReportSHA,
			Status:   200,
			Body: map[string]any{
				"status":            "ahead",
				"ahead_by":          1,
				"behind_by":         0,
				"merge_base_commit": map[string]string{"sha": fixtureCommit},
				"files":             []any{map[string]string{"filename": fixtureReportPath, "status": "added"}},
			},
		},
		{Method: "POST", Endpoint: fixtureRepoPrefix + "/git/refs", Status: 201, Body: map[string]any{"ref": fixtureBranchRef}},
		{Method: "POST", Endpoint: fixtureRepoPrefix + "/pulls", Status: 201, Body: map[string]any{"number": 1}},
	}
}

// TestUsageTextNamesEverySubcommand pins the usage text: one line per
// subcommand with its flags, a constant that holds no value of a command line
// or an environment.
func TestUsageTextNamesEverySubcommand(t *testing.T) {
	t.Parallel()

	if diff := cmp.Diff(usageTextOfEverySubcommand, usageText); diff != "" {
		t.Errorf("the usage text (-want +got):\n%s", diff)
	}

	fake := newFakeGH(t, fakeAnswer{Status: 200, Body: artifactListing(1, artifactChecksName)})
	result := runProgram(t, nil, stepEnvironment(t, writeSummary(t)))

	if result.code != codeConfigInvalid {
		t.Errorf("a run with no subcommand exits %d, want %d", result.code, codeConfigInvalid)
	}

	if result.stderr != usageTextOfEverySubcommand {
		t.Errorf("a usage error writes %q to stderr, want the usage text", result.stderr)
	}

	if calls := fake.Calls(t); len(calls) != 0 {
		t.Errorf("a usage error made %d gh call(s), want none", len(calls))
	}
}

// TestRunPublishAndImageReadTheirVariablesAndPrintTheirLine pins each report
// subcommand's whole run: its flags, the variables it reads and no other, its
// exit 0 and its one line, on stdout and in the step's summary.
func TestRunPublishAndImageReadTheirVariablesAndPrintTheirLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// args builds the command line from the run's paths, and answers are
		// the fake gh's. result writes the result file, the run's own when nil.
		args        func(ghPath, gitleaksPath, resultPath, reportsDir string) []string
		answers     []fakeAnswer
		addVariable []string
		result      func(t *testing.T) string
		wantLine    string
	}{
		{
			name:        "the image subcommand",
			args:        func(ghPath, _, resultPath, _ string) []string { return imageArgs(ghPath, resultPath) },
			answers:     imageAnswers(t),
			addVariable: []string{"GITHUB_SHA"},
			wantLine:    lineImageAsExpected,
		},
		{
			name:        "the publish subcommand",
			args:        publishArgs,
			answers:     publishAnswers(),
			addVariable: []string{"GITHUB_SHA", "AUDIT_FINGERPRINT_KEY"},
			wantLine:    lineAuditPublished,
		},
		{
			name:        "the publish subcommand with one public finding",
			args:        publishArgs,
			answers:     publishAnswers(),
			addVariable: []string{"GITHUB_SHA", "AUDIT_FINGERPRINT_KEY"},
			result: func(t *testing.T) string {
				t.Helper()

				return writeResultOf(t, resultDocumentWith(publicFinding()))
			},
			wantLine: "audit published: public findings 1, private findings: none",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newRoutedFakeGH(t, tc.answers...)
			summaryPath := writeSummary(t)
			tempDir := t.TempDir()

			resultPath := writeResult(t)
			if tc.result != nil {
				resultPath = tc.result(t)
			}

			args := withTempDir(tc.args(fake.Path, newFakeGitleaks(t), resultPath, t.TempDir()), tempDir)
			result := runProgram(t, args, stepEnvironment(t, summaryPath))

			if result.code != codeAsExpected {
				t.Fatalf("the run's exit code is %d, want %d: %s\nstdout: %s\nstderr: %s",
					result.code, codeAsExpected, tc.wantLine, result.stdout, result.stderr)
			}

			if want := tc.wantLine + "\n"; result.stdout != want {
				t.Errorf("the run's stdout is %q, want exactly %q: the run prints one line", result.stdout, want)
			}

			if result.stderr != "" {
				t.Errorf("the run's stderr is %q, want nothing", result.stderr)
			}

			if !strings.HasSuffix(result.summary, tc.wantLine+"\n") {
				t.Errorf("the step summary is %q, want the run's line appended", result.summary)
			}

			want := append(append(slices.Clone(readVariables), tc.addVariable...), refusedVariables...)
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

// TestRunPublishReportsAPrivateFindingNotFiled pins PR 4c's not-filed run: the
// run reads the repository's advisories and files a draft for its private
// finding, GitHub refuses the creation, and the report is published all the
// same. The program prints the runbook's not-filed line and exits 5 - its one
// line and no other, on stdout and in the step's summary.
func TestRunPublishReportsAPrivateFindingNotFiled(t *testing.T) {
	t.Parallel()

	answers := append(publishAnswers(), fakeAnswer{
		Method:   "POST",
		Endpoint: fixtureRepoPrefix + "/security-advisories",
		Status:   422,
		Body:     map[string]string{"message": "the creation was refused"},
	})

	fake := newRoutedFakeGH(t, answers...)
	summaryPath := writeSummary(t)
	tempDir := t.TempDir()

	resultPath := writeResultOf(t, resultDocumentWith(privateFinding()))
	args := withTempDir(publishArgs(fake.Path, newFakeGitleaks(t), resultPath, t.TempDir()), tempDir)
	result := runProgram(t, args, stepEnvironment(t, summaryPath))

	if result.code != codeNotFiled {
		t.Fatalf("the run's exit code is %d, want %d: %s\nstdout: %s\nstderr: %s",
			result.code, codeNotFiled, lineNotFiled, result.stdout, result.stderr)
	}

	if want := lineNotFiled + "\n"; result.stdout != want {
		t.Errorf("the run's stdout is %q, want exactly %q: the run prints one line", result.stdout, want)
	}

	if result.stderr != "" {
		t.Errorf("the run's stderr is %q, want nothing", result.stderr)
	}

	if !strings.HasSuffix(result.summary, lineNotFiled+"\n") {
		t.Errorf("the step summary is %q, want the run's line appended", result.summary)
	}

	// The draft is attempted before the report is published, and the report is
	// published whatever GitHub answered the creation.
	var sawListing, sawCreate, sawReport bool

	for _, c := range fake.Calls(t) {
		if len(c.Args) < 9 {
			continue
		}

		method, endpoint := c.Args[2], c.Args[len(c.Args)-1]

		switch {
		case method == "GET" && endpoint == fixtureAdvisoriesEndpoint:
			sawListing = true
		case method == "POST" && endpoint == fixtureRepoPrefix+"/security-advisories":
			sawCreate = true
		case method == "POST" && endpoint == fixtureRepoPrefix+"/git/blobs":
			sawReport = true
		}
	}

	if !sawListing {
		t.Errorf("the run read no advisory listing, want the one page: a private finding is filed as a draft")
	}

	if !sawCreate {
		t.Errorf("the run attempted no draft, want the one create GitHub refuses")
	}

	if !sawReport {
		t.Errorf("the run published no report, want the report after the refused creation")
	}
}

// TestRunPublishAndImageRefuseAConfigurationTheyCannotUse pins the two report
// subcommands' refusals: a server URL other than exactly https://github.com, a
// GITHUB_SHA that is no 40 lowercase hex, a publish without its fingerprint key
// or with a short one, and a missing flag. Each is the configuration's line,
// exit 9, and no gh call.
func TestRunPublishAndImageRefuseAConfigurationTheyCannotUse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		sub  string
		// env changes the run's environment, and args its command line.
		env     func(env map[string]string)
		args    func(ghPath, gitleaksPath, resultPath, reportsDir string) []string
		refused []string
	}{
		{
			name:    "an image of another server",
			sub:     "image",
			env:     func(env map[string]string) { env["GITHUB_SERVER_URL"] = "https://github.example.com" },
			refused: []string{"https://github.example.com"},
		},
		{
			// An empty token names no token at all: it is the configuration's
			// error, never a GitHub call that fails.
			name: "an image with an empty GH_TOKEN",
			sub:  "image",
			env:  func(env map[string]string) { env["GH_TOKEN"] = "" },
		},
		{
			name: "a publish with an empty GH_TOKEN",
			sub:  "publish",
			env:  func(env map[string]string) { env["GH_TOKEN"] = "" },
		},
		{
			name:    "a publish of another server",
			sub:     "publish",
			env:     func(env map[string]string) { env["GITHUB_SERVER_URL"] = "https://github.example.com" },
			refused: []string{"https://github.example.com"},
		},
		{
			name:    "an image of a plain http server",
			sub:     "image",
			env:     func(env map[string]string) { env["GITHUB_SERVER_URL"] = "http://github.com" },
			refused: []string{"http://github.com"},
		},
		{
			name:    "a publish of a server URL with a trailing slash",
			sub:     "publish",
			env:     func(env map[string]string) { env["GITHUB_SERVER_URL"] = "https://github.com/" },
			refused: []string{"https://github.com/"},
		},
		{
			name: "an image without GITHUB_SERVER_URL",
			sub:  "image",
			env:  func(env map[string]string) { delete(env, "GITHUB_SERVER_URL") },
		},
		{
			name: "a publish without GITHUB_SERVER_URL",
			sub:  "publish",
			env:  func(env map[string]string) { delete(env, "GITHUB_SERVER_URL") },
		},
		{
			name:    "an image with a short GITHUB_SHA",
			sub:     "image",
			env:     func(env map[string]string) { env["GITHUB_SHA"] = "0123456" },
			refused: []string{"0123456"},
		},
		{
			name:    "a publish with an upper-case GITHUB_SHA",
			sub:     "publish",
			env:     func(env map[string]string) { env["GITHUB_SHA"] = strings.ToUpper(fixtureCommit) },
			refused: []string{strings.ToUpper(fixtureCommit)},
		},
		{
			name: "a publish without GITHUB_SHA",
			sub:  "publish",
			env:  func(env map[string]string) { delete(env, "GITHUB_SHA") },
		},
		{
			name: "a publish without AUDIT_FINGERPRINT_KEY",
			sub:  "publish",
			env:  func(env map[string]string) { delete(env, "AUDIT_FINGERPRINT_KEY") },
		},
		{
			name:    "a publish with a short AUDIT_FINGERPRINT_KEY",
			sub:     "publish",
			env:     func(env map[string]string) { env["AUDIT_FINGERPRINT_KEY"] = "short-key" },
			refused: []string{"short-key"},
		},
		{
			name: "an image without its result flag",
			sub:  "image",
			args: func(ghPath, _, _, _ string) []string { return []string{"image", "-gh", ghPath, "-tmp", "TMP"} },
		},
		{
			name: "a publish without its reports flag",
			sub:  "publish",
			args: func(ghPath, gitleaksPath, resultPath, _ string) []string {
				return []string{"publish", "-result", resultPath, "-gh", ghPath, "-gitleaks", gitleaksPath, "-tmp", "TMP"}
			},
		},
		{
			name: "a publish without its gitleaks flag",
			sub:  "publish",
			args: func(ghPath, _, resultPath, reportsDir string) []string {
				return []string{"publish", "-result", resultPath, "-reports", reportsDir, "-gh", ghPath, "-tmp", "TMP"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newRoutedFakeGH(t, fakeAnswer{Status: 500, Body: map[string]string{"message": valueMarker}})
			tempDir := t.TempDir()
			env := stepEnvironment(t, writeSummary(t))

			if tc.env != nil {
				tc.env(env)
			}

			build := tc.args
			if build == nil {
				build = func(ghPath, gitleaksPath, resultPath, reportsDir string) []string {
					if tc.sub == "image" {
						return imageArgs(ghPath, resultPath)
					}

					return publishArgs(ghPath, gitleaksPath, resultPath, reportsDir)
				}
			}

			args := withTempDir(build(fake.Path, newFakeGitleaks(t), writeResult(t), t.TempDir()), tempDir)
			result := runProgram(t, args, env)

			if result.code != codeConfigInvalid {
				t.Errorf("the run's exit code is %d, want %d: %s\nstdout: %s\nstderr: %s",
					result.code, codeConfigInvalid, lineConfigInvalid, result.stdout, result.stderr)
			}

			if !strings.Contains(result.output(), lineConfigInvalid) {
				t.Errorf("the run printed %q, want it to name %q", result.output(), lineConfigInvalid)
			}

			if want := lineConfigInvalid + "\n"; result.stdout != want {
				t.Errorf("the run's stdout is %q, want exactly %q: a refused configuration is the run's one line", result.stdout, want)
			}

			if calls := fake.Calls(t); len(calls) != 0 {
				t.Errorf("the run made %d gh call(s), want none: a refused run starts nothing", len(calls))
			}

			for _, refused := range tc.refused {
				if strings.Contains(result.output(), refused) {
					t.Errorf("the run printed %q, want it to hold no value (%q)", result.output(), refused)
				}
			}
		})
	}
}
