package main

// The entry point's refusals name the check that failed (issue #640). The
// program prints exactly one line, and that line is
// "<class line>: <check phrase>: see runbook": fixed words that name the check
// the maintainer's runbook step 11 can look up, never a value of the step's
// environment, a token, a key, a path, a number the run read or a finding's
// text.
//
// This file is the entry point's half of that fixed list: the refusals
// ci/auditpublish's main.go makes, driven through run exactly as the workflow
// runs it, with the one line it prints, its exit code, and the same line in
// the step's summary. The core's and the GitHub side's rows are their own
// packages' tests; this one also pins the classes end to end: a result the
// schema refuses is "result refused" (exit 3), never "configuration invalid"
// (exit 9), and a scan, an unreadable previous report and a failed GitHub call
// print their own class's line with the check that failed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runPaths are the run's own paths and directories, which a case builds its
// command line from.
type runPaths struct {
	gh       string
	gitleaks string
	result   string
	reports  string
	tempDir  string
}

// modelToken returns a token the core's scan refuses, invented here: the shape
// of a GitHub token, in a model field.
func modelToken() string { return "ghp_" + strings.Repeat("z9Y8x7W6", 6) }

// standardArgs returns a subcommand's whole, valid command line for the run's
// paths; an empty subcommand is a run with no command line at all.
func standardArgs(sub string, p runPaths) []string {
	switch sub {
	case "artifacts":
		return artifactArgs(jobModel, p.gh, p.tempDir)
	case "image":
		return withTempDir(imageArgs(p.gh, p.result), p.tempDir)
	case "publish":
		return withTempDir(publishArgs(p.gh, p.gitleaks, p.result, p.reports), p.tempDir)
	default:
		return nil
	}
}

// The fixed lines of the entry point's refusals (issue #640): the one line
// each prints, "<class line>: <check phrase>: see runbook", with the phrase of
// the check that failed. This is the entry point's fixed list; run_test.go's
// refusal tests read the same lines.
const (
	lineConfigSecret    = "configuration invalid: a secret that must not be in this step is set: see runbook"
	lineConfigSummary   = "configuration invalid: the step summary not appendable: see runbook"
	lineConfigUsage     = "configuration invalid: a bad flag or subcommand: see runbook"
	lineConfigToken     = "configuration invalid: GH_TOKEN unset or empty: see runbook"
	lineConfigServerURL = "configuration invalid: GITHUB_SERVER_URL not github.com over https: see runbook"
	lineConfigSHA       = "configuration invalid: GITHUB_SHA not 40 lowercase hex: see runbook"
	lineConfigKey       = "configuration invalid: AUDIT_FINGERPRINT_KEY unset or shorter than 32 bytes: see runbook"
	lineConfigRepo      = "configuration invalid: GITHUB_REPOSITORY not owner/name: see runbook"
	lineConfigDot       = "configuration invalid: GITHUB_REPOSITORY holds a dot segment: see runbook"
	lineConfigRunID     = "configuration invalid: GITHUB_RUN_ID not a decimal of up to 20 digits: see runbook"
	lineConfigGHP       = "configuration invalid: the gh path not absolute: see runbook"
	lineConfigTempDir   = "configuration invalid: the temporary directory not absolute or no directory: see runbook"
	lineConfigArtifact  = "configuration invalid: the artifact file not created: see runbook"
	lineResultSchema    = "result refused: the result does not match the schema: see runbook"
	lineScanReport      = "publish refused: the report: see runbook"
	linePreviousReport  = "previous report unreadable: the newest report not as the core renders one: see runbook"
	lineGitHubTag       = "GitHub call failed: reading the tag's release: see runbook"

	// codeResultRefused is the exit code of the result's class (ADR 0040).
	codeResultRefused = 3
)

// refusal is one row of the entry point's fixed list of lines.
type refusal struct {
	name string
	// source is the refusal's site in the entry point.
	source string
	// sub is the subcommand whose standard command line the run gets; args
	// overrides it when it is set.
	sub  string
	args func(p runPaths) []string
	// env breaks the step's environment, which is whole otherwise.
	env func(t *testing.T, env map[string]string)
	// result is the result file the run reads, the whole one when it is nil.
	result func(t *testing.T) string
	// reports is the reports directory the run reads, an empty one when it is
	// nil.
	reports func(t *testing.T) string
	// answers are the fake gh's, on top of a publish run's own.
	answers []fakeAnswer
	// want is the one line the run prints, and code its exit code.
	want string
	code int
	// leaks are values the case plants in its input, which the line must not
	// hold.
	leaks []string
	// usage says the run also writes the usage text to stderr, the one thing a
	// usage error writes there (ADR 0040).
	usage bool
	// noSummary says the run cannot append its line to the step summary: the
	// summary alone is then what fails the run closed.
	noSummary bool
}

// TestEntryPointRefusalNamesTheFailedCheck pins the entry point's fixed list.
func TestEntryPointRefusalNamesTheFailedCheck(t *testing.T) {
	t.Parallel()

	tests := []refusal{
		{
			name:   "no subcommand",
			source: "main.go: run",
			args:   func(runPaths) []string { return nil },
			want:   lineConfigUsage,
			code:   codeConfigInvalid,
			usage:  true,
		},
		{
			name:   "an unknown subcommand",
			source: "main.go: run",
			args:   func(runPaths) []string { return []string{"deploy-" + valueMarker} },
			want:   lineConfigUsage,
			code:   codeConfigInvalid,
			usage:  true,
		},
		{
			name:   "a flag the subcommand does not know",
			source: "main.go: parseFlags",
			sub:    "artifacts",
			args: func(p runPaths) []string {
				return append(artifactArgs(jobModel, p.gh, p.tempDir), "-bogus", valueMarker)
			},
			want:  lineConfigUsage,
			code:  codeConfigInvalid,
			usage: true,
		},
		{
			name:   "an argument after the flags",
			source: "main.go: parseFlags",
			sub:    "artifacts",
			args: func(p runPaths) []string {
				return append(artifactArgs(jobModel, p.gh, p.tempDir), valueMarker)
			},
			want:  lineConfigUsage,
			code:  codeConfigInvalid,
			usage: true,
		},
		{
			name:   "a job the artifact check does not have",
			source: "main.go: artifacts",
			sub:    "artifacts",
			args: func(p runPaths) []string {
				return artifactArgs(valueMarker, p.gh, p.tempDir)
			},
			want:  lineConfigUsage,
			code:  codeConfigInvalid,
			usage: true,
		},
		{
			name:   "an image without a step summary",
			source: "main.go: run",
			sub:    "image",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "GITHUB_STEP_SUMMARY")
			},
			want:      lineConfigSummary,
			code:      codeConfigInvalid,
			noSummary: true,
		},
		{
			name:   "a publish without a step summary",
			source: "main.go: run",
			sub:    "publish",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "GITHUB_STEP_SUMMARY")
			},
			want:      lineConfigSummary,
			code:      codeConfigInvalid,
			noSummary: true,
		},
		{
			name:   "an artifact check whose step summary cannot be opened",
			source: "main.go: finish",
			sub:    "artifacts",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "GITHUB_STEP_SUMMARY")
			},
			// The check itself passes: a run that would have published prints
			// the configuration's line instead, never the check's verdict.
			answers: []fakeAnswer{{
				Method:   "GET",
				Endpoint: fixtureRepoPrefix + "/actions/runs/" + fixtureRunID + "/artifacts?per_page=100",
				Status:   200,
				Body:     artifactListing(1, artifactChecksName),
			}},
			want:      lineConfigSummary,
			code:      codeConfigInvalid,
			noSummary: true,
		},
		{
			name:   "an artifact check with no GH_TOKEN",
			source: "main.go: artifacts",
			sub:    "artifacts",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "GH_TOKEN")
			},
			want: lineConfigToken,
			code: codeConfigInvalid,
		},
		{
			name:   "an image with an empty GH_TOKEN",
			source: "main.go: readReportEnv",
			sub:    "image",
			env: func(_ *testing.T, env map[string]string) {
				env["GH_TOKEN"] = ""
			},
			want: lineConfigToken,
			code: codeConfigInvalid,
		},
		{
			name:   "a publish of another server",
			source: "main.go: readReportEnv",
			sub:    "publish",
			env: func(_ *testing.T, env map[string]string) {
				env["GITHUB_SERVER_URL"] = "https://github.example.com"
			},
			want: lineConfigServerURL,
			code: codeConfigInvalid,
		},
		{
			name:   "an image without GITHUB_SERVER_URL",
			source: "main.go: readReportEnv",
			sub:    "image",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "GITHUB_SERVER_URL")
			},
			want: lineConfigServerURL,
			code: codeConfigInvalid,
		},
		{
			name:   "a publish with a short GITHUB_SHA",
			source: "main.go: readReportEnv",
			sub:    "publish",
			env: func(_ *testing.T, env map[string]string) {
				env["GITHUB_SHA"] = "0123456"
			},
			want: lineConfigSHA,
			code: codeConfigInvalid,
		},
		{
			name:   "an image without GITHUB_SHA",
			source: "main.go: readReportEnv",
			sub:    "image",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "GITHUB_SHA")
			},
			want: lineConfigSHA,
			code: codeConfigInvalid,
		},
		{
			name:   "a publish without GITHUB_REPOSITORY",
			source: "main.go: readReportEnv",
			sub:    "publish",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "GITHUB_REPOSITORY")
			},
			want: lineConfigRepo,
			code: codeConfigInvalid,
		},
		{
			name:   "an image of a repository with a dot segment",
			source: "main.go: readReportEnv",
			sub:    "image",
			env: func(_ *testing.T, env map[string]string) {
				env["GITHUB_REPOSITORY"] = "example/.."
			},
			want: lineConfigDot,
			code: codeConfigInvalid,
		},
		{
			name:   "a publish without GITHUB_RUN_ID",
			source: "main.go: readReportEnv",
			sub:    "publish",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "GITHUB_RUN_ID")
			},
			want: lineConfigRunID,
			code: codeConfigInvalid,
		},
		{
			name:   "a publish with a short AUDIT_FINGERPRINT_KEY",
			source: "main.go: publishRun",
			sub:    "publish",
			env: func(_ *testing.T, env map[string]string) {
				env["AUDIT_FINGERPRINT_KEY"] = "short-key"
			},
			want: lineConfigKey,
			code: codeConfigInvalid,
		},
		{
			name:   "a publish without AUDIT_FINGERPRINT_KEY",
			source: "main.go: publishRun",
			sub:    "publish",
			env: func(_ *testing.T, env map[string]string) {
				delete(env, "AUDIT_FINGERPRINT_KEY")
			},
			want: lineConfigKey,
			code: codeConfigInvalid,
		},
		{
			name:   "an image with a relative gh path",
			source: "main.go: image",
			sub:    "image",
			args: func(p runPaths) []string {
				return withTempDir(imageArgs("bin/"+valueMarker, p.result), p.tempDir)
			},
			want: lineConfigGHP,
			code: codeConfigInvalid,
		},
		{
			name:   "an artifact check with a relative temporary directory",
			source: "main.go: artifacts",
			sub:    "artifacts",
			args: func(p runPaths) []string {
				return artifactArgs(jobModel, p.gh, "tmp/"+valueMarker)
			},
			want: lineConfigTempDir,
			code: codeConfigInvalid,
		},
		{
			name:   "a publish whose temporary directory does not exist",
			source: "main.go: publishRun",
			sub:    "publish",
			args: func(p runPaths) []string {
				return withTempDir(publishArgs(p.gh, p.gitleaks, p.result, p.reports), p.tempDir+"/"+valueMarker)
			},
			want: lineConfigTempDir,
			code: codeConfigInvalid,
		},
		{
			name: "a result the schema refuses",
			// The audited commit is the result's, and the result is not the
			// schema's: the run refuses the result's content, which is the
			// result's class, never the configuration's.
			source: "main.go: publishRun",
			sub:    "publish",
			result: func(t *testing.T) string {
				t.Helper()

				return writeResultOf(t, map[string]any{"schema": 1})
			},
			want: lineResultSchema,
			code: codeResultRefused,
		},

		// The classes beyond the result's: a scan, the previous report and
		// GitHub's own failure, named by the check that failed.
		{
			name:   "a publish whose previous report the core cannot read",
			source: "main.go: publishRun",
			sub:    "publish",
			reports: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				path := filepath.Join(dir, "2026-12-01-1.md")

				if err := os.WriteFile(path, []byte("not a report "+valueMarker+"\n"), 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}

				return dir
			},
			want:  linePreviousReport,
			code:  7,
			leaks: []string{valueMarker},
		},
		{
			name:   "a publish whose report holds a token",
			source: "main.go: publishRun",
			sub:    "publish",
			result: func(t *testing.T) string {
				t.Helper()

				finding := publicFinding()
				finding["scenario"] = "a scenario holding " + modelToken()

				return writeResultOf(t, resultDocumentWith(finding))
			},
			want:  lineScanReport,
			code:  4,
			leaks: []string{modelToken()},
		},
		{
			name:   "a publish whose release read GitHub fails",
			source: "main.go: publishRun",
			sub:    "publish",
			answers: []fakeAnswer{{
				Method:   "GET",
				Endpoint: fixtureRepoPrefix + "/releases/tags/" + fixtureTag,
				Status:   500,
				Body:     map[string]string{"message": valueMarker},
			}},
			want:  lineGitHubTag,
			code:  8,
			leaks: []string{valueMarker},
		},
	}

	// The four secrets refuse the whole run by being set at all.
	for _, name := range refusedVariables {
		tests = append(tests, refusal{
			name:   "a secret in the step's environment: " + name,
			source: "main.go: run",
			sub:    "artifacts",
			env: func(_ *testing.T, env map[string]string) {
				env[name] = "invented-" + valueMarker
			},
			want: lineConfigSecret,
			code: codeConfigInvalid,
		})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newRoutedFakeGH(t, append(tc.answers, publishAnswers()...)...)
			p := runPaths{
				gh:       fake.Path,
				gitleaks: newFakeGitleaks(t),
				result:   writeResult(t),
				reports:  t.TempDir(),
				tempDir:  t.TempDir(),
			}

			args := standardArgs(tc.sub, p)
			if tc.args != nil {
				args = tc.args(p)
			}

			env := stepEnvironment(t, writeSummary(t))
			if tc.env != nil {
				tc.env(t, env)
			}

			if tc.result != nil {
				p.result = tc.result(t)
				args = standardArgs(tc.sub, p)
			}

			if tc.reports != nil {
				p.reports = tc.reports(t)
				args = standardArgs(tc.sub, p)
			}

			result := runProgram(t, args, env)

			if result.code != tc.code {
				t.Errorf("the run's exit code is %d, want %d (%q)\nstdout: %s\nstderr: %s",
					result.code, tc.code, tc.want, result.stdout, result.stderr)
			}

			if want := tc.want + "\n"; result.stdout != want {
				t.Errorf("the run's stdout is %q, want exactly %q: the run prints one line", result.stdout, want)
			}

			if tc.usage {
				if result.stderr != usageText {
					t.Errorf("the run's stderr is %q, want the usage text: a usage error writes its one constant text", result.stderr)
				}
			} else if result.stderr != "" {
				t.Errorf("the run's stderr is %q, want nothing: a refusal writes no usage text", result.stderr)
			}

			if !tc.noSummary && !strings.HasSuffix(result.summary, tc.want+"\n") {
				t.Errorf("the step summary is %q, want the run's line %q appended", result.summary, tc.want)
			}

			if strings.Contains(result.output(), valueMarker) {
				t.Errorf("the run printed %q, want it to hold no value of the step: an error names a check, never a value",
					result.output())
			}

			for _, leak := range tc.leaks {
				for _, written := range []struct {
					where string
					text  string
				}{
					{"stdout", result.stdout},
					{"the step summary", result.summary},
				} {
					if strings.Contains(written.text, leak) {
						t.Errorf("the run's %s holds the value %q, want the check's own fixed words:\n%s",
							written.where, leak, written.text)
					}
				}
			}
		})
	}
}

// TestEntryPointSummaryHoldsNoInputValue pins the other half of the line's
// rule: a refusal's line holds no value of the step's environment, no token, no
// key, no path and no finding's text, on stdout and in the step's summary. The
// values planted are obviously invented, and each is one a real run carries.
func TestEntryPointSummaryHoldsNoInputValue(t *testing.T) {
	t.Parallel()

	const (
		// fakeKey is a key of 31 bytes: the longest length the run refuses.
		fakeKey = "fake-key-0123456789abcdef012345"
		// fakeToken is a token of the shape GitHub's installation tokens have. It
		// is invented: no credential, and no value a line may hold.
		fakeToken = "ghs_FAKEFAKEFAKE0123456789abcdefghij" //nolint:gosec // G101: an invented token of the shape GitHub's are, never a credential
		// pathMarker and findingMarker are values only a test holds, in a path
		// the run is given and in a finding's text.
		pathMarker    = "LEAKMARKER-gh-path"
		findingMarker = "LEAKMARKER-finding-detail"
	)

	if len(fakeKey) != 31 {
		t.Fatalf("the fake key is %d bytes, want the 31 a refused key has", len(fakeKey))
	}

	tests := []struct {
		name   string
		sub    string
		args   func(p runPaths) []string
		env    func(env map[string]string)
		result func(t *testing.T) string
		code   int
	}{
		{
			name: "a short key",
			sub:  "publish",
			env: func(env map[string]string) {
				env["AUDIT_FINGERPRINT_KEY"] = fakeKey
			},
			code: 9,
		},
		{
			name: "a step summary that cannot be appended to",
			sub:  "publish",
			env: func(env map[string]string) {
				env["GITHUB_STEP_SUMMARY"] = "summary.md"
			},
			code: 9,
		},
		{
			name: "a relative gh path",
			sub:  "image",
			args: func(p runPaths) []string {
				return withTempDir(imageArgs("bin/"+pathMarker, p.result), p.tempDir)
			},
			code: 9,
		},
		{
			name: "a result that is not the schema's",
			sub:  "publish",
			result: func(t *testing.T) string {
				t.Helper()

				return writeResultOf(t, map[string]any{
					"schema":   1,
					"findings": []any{map[string]any{"check": "tree-review", "scenario": findingMarker}},
				})
			},
			code: 3,
		},
		{
			name: "a result of a run at another commit",
			sub:  "publish",
			env: func(env map[string]string) {
				env["GITHUB_SHA"] = strings.Repeat("a", 40)
			},
			result: func(t *testing.T) string {
				t.Helper()

				return writeResultOf(t, resultDocumentWith(privateFinding()))
			},
			code: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newRoutedFakeGH(t, publishAnswers()...)
			p := runPaths{
				gh:       fake.Path,
				gitleaks: newFakeGitleaks(t),
				result:   writeResult(t),
				reports:  t.TempDir(),
				tempDir:  t.TempDir(),
			}

			env := stepEnvironment(t, writeSummary(t))
			env["GH_TOKEN"] = fakeToken

			if tc.env != nil {
				tc.env(env)
			}

			if tc.result != nil {
				p.result = tc.result(t)
			}

			args := standardArgs(tc.sub, p)
			if tc.args != nil {
				args = tc.args(p)
			}

			result := runProgram(t, args, env)

			if result.code != tc.code {
				t.Errorf("the run's exit code is %d, want %d\nstdout: %s\nstderr: %s",
					result.code, tc.code, result.stdout, result.stderr)
			}

			for _, leaked := range []string{fakeKey, fakeToken, pathMarker, findingMarker} {
				for _, written := range []struct {
					where string
					text  string
				}{
					{"stdout", result.stdout},
					{"the step summary", result.summary},
				} {
					if strings.Contains(written.text, leaked) {
						t.Errorf("the run's %s holds %q, want the check's own fixed words only:\n%s",
							written.where, leaked, written.text)
					}
				}
			}
		})
	}
}
