package review_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// anthropicAPIKeyVar is the variable the reviewer refuses: a set
// ANTHROPIC_API_KEY switches Claude Code from the maintainer's subscription to
// pay-as-you-go billing, so the program will not run beside one (ADR 0033).
const anthropicAPIKeyVar = "ANTHROPIC_API_KEY"

// stepSummaryVar is the file GitHub Actions appends a job's summary to. A
// skipped run adds one line there, so the run says the review was skipped away
// from its log too (issue #367).
const stepSummaryVar = "GITHUB_STEP_SUMMARY"

// settingsLookup returns the lookup function the tests pass to
// SettingsFromEnv: a map in place of os.LookupEnv, which the library never calls
// itself.
func settingsLookup(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := env[name]

		return value, ok
	}
}

// validEnv is a complete environment of the reviewer's settings: the
// subscription token, and an ANTHROPIC_API_KEY that is set but empty, which is
// no key at all.
func validEnv() map[string]string {
	return map[string]string{
		review.EnvRepository:     "owner/name",
		review.EnvPullRequest:    "42",
		review.EnvAppSlug:        "shift2eu-reviewer",
		review.EnvGitHubToken:    testToken,
		review.EnvModelToken:     testModelToken,
		review.EnvRequestChanges: "true",
		anthropicAPIKeyVar:       "",
	}
}

// TestSettingsFromEnvReadsTheReviewersSettings pins the environment of ADR
// 0033: the repository, the pull request's number, the reviewer's login
// (<slug>[bot]), the two credentials, the Claude subscription's OAuth token and
// the AI_REVIEW_REQUEST_CHANGES switch.
func TestSettingsFromEnvReadsTheReviewersSettings(t *testing.T) {
	t.Parallel()

	got, err := review.SettingsFromEnv(settingsLookup(validEnv()))
	if err != nil {
		t.Fatalf("SettingsFromEnv: %v", err)
	}

	want := review.Settings{
		Repository:     "owner/name",
		Number:         42,
		BotLogin:       "shift2eu-reviewer[bot]",
		GitHubToken:    testToken,
		ModelToken:     testModelToken,
		RequestChanges: true,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("SettingsFromEnv (-want +got):\n%s", diff)
	}
}

// TestSettingsFromEnvReadsTheSwitch pins the switch's default (ADR 0033):
// change requests are on for "true" alone; unset, empty and "false" keep the
// reviewer at comments, so a typo fails the run instead of reading as either.
func TestSettingsFromEnvReadsTheSwitch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		unset bool
		want  bool
	}{
		{name: "true", value: "true", want: true},
		{name: "false", value: "false", want: false},
		{name: "empty", value: "", want: false},
		{name: "unset", unset: true, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := validEnv()
			if tc.unset {
				delete(env, review.EnvRequestChanges)
			} else {
				env[review.EnvRequestChanges] = tc.value
			}

			got, err := review.SettingsFromEnv(settingsLookup(env))
			if err != nil {
				t.Fatalf("SettingsFromEnv: %v", err)
			}

			if got.RequestChanges != tc.want {
				t.Errorf("RequestChanges = %t for %q, want %t", got.RequestChanges, tc.value, tc.want)
			}
		})
	}
}

// TestSettingsFromEnvAcceptsNoAnthropicAPIKey pins that only a set
// ANTHROPIC_API_KEY fails the run (ADR 0033): Claude Code prefers one over the
// subscription whenever it is set, so an unset or empty one is the state the
// reviewer runs in.
func TestSettingsFromEnvAcceptsNoAnthropicAPIKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{name: "unset", mutate: func(env map[string]string) { delete(env, anthropicAPIKeyVar) }},
		{name: "set but empty", mutate: func(env map[string]string) { env[anthropicAPIKeyVar] = "" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := validEnv()
			tc.mutate(env)

			settings, err := review.SettingsFromEnv(settingsLookup(env))
			if err != nil {
				t.Fatalf("SettingsFromEnv with ANTHROPIC_API_KEY %s: %v; want the settings", tc.name, err)
			}

			if settings.ModelToken != testModelToken {
				t.Errorf("ModelToken = %q, want %q", settings.ModelToken, testModelToken)
			}
		})
	}
}

// TestSettingsFromEnvReadsTheStepSummary pins the job summary's file (issue
// #367, ADR 0033): the skipped outcome appends one line to $GITHUB_STEP_SUMMARY,
// which the workflow sets to a path. It is optional — a run outside Actions has
// none, and a skipped review is no failure there — and a value that is no
// absolute path fails the run with ErrSettings, before anything is read or
// posted, so a typo cannot silently lose the line.
func TestSettingsFromEnvReadsTheStepSummary(t *testing.T) {
	t.Parallel()

	const absolute = "/tmp/ai-review-step-summary"

	cases := []struct {
		name    string
		value   string
		unset   bool
		want    string
		wantErr bool
	}{
		{name: "an absolute path", value: absolute, want: absolute},
		{name: "unset", unset: true, want: ""},
		{name: "a relative path", value: "step-summary.md", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := validEnv()

			if tc.unset {
				delete(env, stepSummaryVar)
			} else {
				env[stepSummaryVar] = tc.value
			}

			settings, err := review.SettingsFromEnv(settingsLookup(env))

			if tc.wantErr {
				if !errors.Is(err, review.ErrSettings) {
					t.Fatalf("SettingsFromEnv with %s = %q = %+v, %v; want an error wrapping ErrSettings", stepSummaryVar, tc.value, settings, err)
				}

				if !strings.Contains(err.Error(), stepSummaryVar) {
					t.Errorf("the error %q does not name the variable %s", err, stepSummaryVar)
				}

				return
			}

			if err != nil {
				t.Fatalf("SettingsFromEnv with %s %s: %v", stepSummaryVar, tc.name, err)
			}

			if settings.StepSummary != tc.want {
				t.Errorf("Settings.StepSummary = %q, want %q", settings.StepSummary, tc.want)
			}
		})
	}
}

// TestSettingsFromEnvRefusesBadSettings pins what fails a run before anything is
// read or posted, and that the failure names the variable without printing a
// credential: a repository that is not owner/name, a pull request number that is
// not a decimal above 0, an invalid App slug, a missing or empty credential, and
// a switch value that is neither "" nor "false" nor "true".
func TestSettingsFromEnvRefusesBadSettings(t *testing.T) {
	t.Parallel()

	set := func(name, value string) func(map[string]string) {
		return func(env map[string]string) { env[name] = value }
	}
	unset := func(name string) func(map[string]string) {
		return func(env map[string]string) { delete(env, name) }
	}

	cases := []struct {
		name    string
		mutate  func(map[string]string)
		wantVar string
	}{
		{name: "a repository without a name", mutate: set(review.EnvRepository, "owner"), wantVar: review.EnvRepository},
		{name: "a repository without an owner", mutate: set(review.EnvRepository, "/name"), wantVar: review.EnvRepository},
		{name: "a repository with three parts", mutate: set(review.EnvRepository, "owner/name/extra"), wantVar: review.EnvRepository},
		{name: "a repository with a space", mutate: set(review.EnvRepository, "own er/name"), wantVar: review.EnvRepository},
		{name: "a repository with a .. part", mutate: set(review.EnvRepository, "owner/.."), wantVar: review.EnvRepository},
		{name: "a repository that is a dot", mutate: set(review.EnvRepository, "."), wantVar: review.EnvRepository},
		{name: "no repository", mutate: unset(review.EnvRepository), wantVar: review.EnvRepository},
		{name: "pull request 0", mutate: set(review.EnvPullRequest, "0"), wantVar: review.EnvPullRequest},
		{name: "a negative pull request", mutate: set(review.EnvPullRequest, "-1"), wantVar: review.EnvPullRequest},
		{name: "a pull request that is not a number", mutate: set(review.EnvPullRequest, "seven"), wantVar: review.EnvPullRequest},
		{name: "a pull request that is a decimal", mutate: set(review.EnvPullRequest, "4.2"), wantVar: review.EnvPullRequest},
		{name: "no pull request", mutate: unset(review.EnvPullRequest), wantVar: review.EnvPullRequest},
		{name: "a slug with an upper-case letter", mutate: set(review.EnvAppSlug, "Shift2EU-Reviewer"), wantVar: review.EnvAppSlug},
		{name: "a slug that starts with a dash", mutate: set(review.EnvAppSlug, "-reviewer"), wantVar: review.EnvAppSlug},
		{name: "a slug with an underscore", mutate: set(review.EnvAppSlug, "shift2eu_reviewer"), wantVar: review.EnvAppSlug},
		{name: "a slug with a space", mutate: set(review.EnvAppSlug, "shift2eu reviewer"), wantVar: review.EnvAppSlug},
		{name: "no slug", mutate: unset(review.EnvAppSlug), wantVar: review.EnvAppSlug},
		{name: "no token", mutate: unset(review.EnvGitHubToken), wantVar: review.EnvGitHubToken},
		{name: "an empty token", mutate: set(review.EnvGitHubToken, ""), wantVar: review.EnvGitHubToken},
		{name: "no model token", mutate: unset(review.EnvModelToken), wantVar: review.EnvModelToken},
		{name: "an empty model token", mutate: set(review.EnvModelToken, ""), wantVar: review.EnvModelToken},
		{name: "an Anthropic API key is set", mutate: set(anthropicAPIKeyVar, testAPIKey), wantVar: anthropicAPIKeyVar},
		{name: "a switch value of yes", mutate: set(review.EnvRequestChanges, "yes"), wantVar: review.EnvRequestChanges},
		{name: "a switch value of TRUE", mutate: set(review.EnvRequestChanges, "TRUE"), wantVar: review.EnvRequestChanges},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := validEnv()
			tc.mutate(env)

			settings, err := review.SettingsFromEnv(settingsLookup(env))
			if !errors.Is(err, review.ErrSettings) {
				t.Fatalf("SettingsFromEnv = %+v, %v; want an error wrapping ErrSettings", settings, err)
			}

			if !strings.Contains(err.Error(), tc.wantVar) {
				t.Errorf("the error %q does not name the variable %s", err, tc.wantVar)
			}

			for _, credential := range []string{testToken, testModelToken, testAPIKey} {
				if strings.Contains(err.Error(), credential) {
					t.Errorf("the error %q prints the credential %q", err, credential)
				}
			}
		})
	}
}

// TestSettingsStringPrintsNoCredential pins that the settings can be logged: the
// description names the repository and never the token, the subscription's OAuth
// token or an API key.
func TestSettingsStringPrintsNoCredential(t *testing.T) {
	t.Parallel()

	settings := review.Settings{
		Repository:     "owner/name",
		Number:         42,
		BotLogin:       testBotLogin,
		GitHubToken:    testToken,
		ModelToken:     testModelToken,
		RequestChanges: true,
	}

	got := settings.String()

	for _, credential := range []string{testToken, testModelToken} {
		if strings.Contains(got, credential) {
			t.Errorf("Settings.String() = %q, which prints the credential %q", got, credential)
		}
	}

	if !strings.Contains(got, settings.Repository) {
		t.Errorf("Settings.String() = %q, which does not name the repository %q", got, settings.Repository)
	}
}
