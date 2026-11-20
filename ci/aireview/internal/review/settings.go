package review

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The environment variables ci/aireview reads (ADR 0033), set by the review
// workflow on the one step that runs it.
const (
	EnvRepository     = "GITHUB_REPOSITORY"
	EnvPullRequest    = "AI_REVIEW_PR_NUMBER"
	EnvAppSlug        = "AI_REVIEWER_APP_SLUG"
	EnvGitHubToken    = "AI_REVIEWER_TOKEN"
	EnvModelToken     = "CLAUDE_CODE_OAUTH_TOKEN" //nolint:gosec // the variable's name, not a credential
	EnvRequestChanges = "AI_REVIEW_REQUEST_CHANGES"
	// EnvStepSummary is the file GitHub Actions appends a job's summary to,
	// which the runner sets on every step: the skipped outcome adds one line to
	// it (issue #367).
	EnvStepSummary = "GITHUB_STEP_SUMMARY"
)

// envAnthropicAPIKey is the variable SettingsFromEnv refuses when it is set:
// Claude Code prefers an API key over the subscription's token whenever it
// sees one, which would bill the API instead of the maintainer's subscription
// (ADR 0033). It is only checked, never kept.
const envAnthropicAPIKey = "ANTHROPIC_API_KEY" //nolint:gosec // the variable's name, not a credential

// Settings is a run's settings from the environment. GitHubToken and
// ModelToken are credentials: String leaves them out, and nothing prints them.
// StepSummary is the absolute path of the job summary's file, "" outside
// GitHub Actions.
type Settings struct {
	Repository     string
	Number         int
	BotLogin       string
	GitHubToken    string
	ModelToken     string
	RequestChanges bool
	StepSummary    string
}

// SettingsFromEnv reads the settings through lookup (os.LookupEnv in
// ci/aireview, a map in tests), reading the Env* variables and
// ANTHROPIC_API_KEY, and no other. It returns an error wrapping ErrSettings,
// naming the variable but never its value, when: EnvRepository is not
// owner/name (each part non-empty, of letters, digits, '-', '_' and '.', and
// neither "." nor ".."); EnvPullRequest is not a decimal integer above 0;
// EnvAppSlug is not a GitHub App slug (lower-case letters, digits and '-',
// starting with a letter or digit); EnvGitHubToken or EnvModelToken is unset
// or empty; ANTHROPIC_API_KEY is set to anything but ""; or EnvRequestChanges
// is set to anything but "", "false" or "true"; or EnvStepSummary is set to a
// value that is not an absolute path. BotLogin is the slug followed by "[bot]";
// RequestChanges is true for "true" alone; StepSummary is "" when EnvStepSummary
// is unset or empty, since a run outside GitHub Actions has no job summary.
func SettingsFromEnv(lookup func(string) (string, bool)) (Settings, error) {
	var s Settings

	repository, _ := lookup(EnvRepository)
	if !validRepository(repository) {
		return Settings{}, fmt.Errorf("%w: %s is not owner/name", ErrSettings, EnvRepository)
	}

	s.Repository = repository

	number, err := parsePositive(lookupValue(lookup, EnvPullRequest))
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %s is not a pull request number above 0", ErrSettings, EnvPullRequest)
	}

	s.Number = number

	slug, _ := lookup(EnvAppSlug)
	if !validSlug(slug) {
		return Settings{}, fmt.Errorf("%w: %s is not a GitHub App slug", ErrSettings, EnvAppSlug)
	}

	s.BotLogin = slug + "[bot]"

	if s.GitHubToken = lookupValue(lookup, EnvGitHubToken); s.GitHubToken == "" {
		return Settings{}, fmt.Errorf("%w: %s is unset or empty", ErrSettings, EnvGitHubToken)
	}

	if s.ModelToken = lookupValue(lookup, EnvModelToken); s.ModelToken == "" {
		return Settings{}, fmt.Errorf("%w: %s is unset or empty", ErrSettings, EnvModelToken)
	}

	if lookupValue(lookup, envAnthropicAPIKey) != "" {
		return Settings{}, fmt.Errorf("%w: %s is set; the reviewer runs on the Claude subscription's %s alone, and Claude Code would bill the API instead",
			ErrSettings, envAnthropicAPIKey, EnvModelToken)
	}

	switch lookupValue(lookup, EnvRequestChanges) {
	case "", "false":
		s.RequestChanges = false
	case "true":
		s.RequestChanges = true
	default:
		return Settings{}, fmt.Errorf("%w: %s is neither true nor false (unset or empty is false)", ErrSettings, EnvRequestChanges)
	}

	// A relative path would be appended to a file of the working directory,
	// main's checkout, and a typo would lose the line without a word.
	if s.StepSummary = lookupValue(lookup, EnvStepSummary); s.StepSummary != "" && !strings.HasPrefix(s.StepSummary, "/") {
		return Settings{}, fmt.Errorf("%w: %s is not an absolute path", ErrSettings, EnvStepSummary)
	}

	return s, nil
}

// lookupValue returns name's value through lookup, "" when it is unset.
func lookupValue(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)

	return value
}

// parsePositive parses s as a decimal integer above 0, digits only.
func parsePositive(s string) (int, error) {
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, errNotPositive
	}

	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, errNotPositive
	}

	return n, nil
}

var errNotPositive = errors.New("not a decimal integer above 0")

// validRepository reports whether s is owner/name: two non-empty parts of
// letters, digits, '-', '_' and '.', neither "." nor "..".
func validRepository(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	if !ok {
		return false
	}

	for _, part := range []string{owner, name} {
		if part == "" || part == "." || part == ".." {
			return false
		}

		for _, r := range part {
			if !isASCIILetterOrDigit(r) && r != '-' && r != '_' && r != '.' {
				return false
			}
		}
	}

	return true
}

// validSlug reports whether s is a GitHub App slug: lower-case letters, digits
// and '-', starting with a letter or digit.
func validSlug(s string) bool {
	if s == "" || s[0] == '-' {
		return false
	}

	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}

	return true
}

// isASCIILetterOrDigit reports whether r is an ASCII letter or digit.
func isASCIILetterOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// String describes s without its credentials: the repository, number, login
// and switch, and whether each credential is set, never its value.
func (s Settings) String() string {
	return fmt.Sprintf("repository=%s pull_request=%d login=%s request_changes=%t github_token_set=%t model_token_set=%t",
		s.Repository, s.Number, s.BotLogin, s.RequestChanges, s.GitHubToken != "", s.ModelToken != "")
}
