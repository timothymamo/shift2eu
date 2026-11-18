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
	EnvModelKey       = "ANTHROPIC_API_KEY"
	EnvRequestChanges = "AI_REVIEW_REQUEST_CHANGES"
)

// Settings is a run's settings from the environment. GitHubToken and ModelKey
// are credentials: String leaves them out, and nothing prints them.
type Settings struct {
	Repository     string
	Number         int
	BotLogin       string
	GitHubToken    string
	ModelKey       string
	RequestChanges bool
}

// SettingsFromEnv reads the settings through lookup (os.LookupEnv in
// ci/aireview, a map in tests), reading the Env* variables and no other. It
// returns an error wrapping ErrSettings, naming the variable but never its
// value, when: EnvRepository is not owner/name (each part non-empty, of
// letters, digits, '-', '_' and '.', and neither "." nor ".."); EnvPullRequest
// is not a decimal integer above 0; EnvAppSlug is not a GitHub App slug
// (lower-case letters, digits and '-', starting with a letter or digit);
// EnvGitHubToken or EnvModelKey is unset or empty; or EnvRequestChanges is
// set to anything but "", "false" or "true". BotLogin is the slug followed by
// "[bot]"; RequestChanges is true for "true" alone.
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

	if s.ModelKey = lookupValue(lookup, EnvModelKey); s.ModelKey == "" {
		return Settings{}, fmt.Errorf("%w: %s is unset or empty", ErrSettings, EnvModelKey)
	}

	switch lookupValue(lookup, EnvRequestChanges) {
	case "", "false":
		s.RequestChanges = false
	case "true":
		s.RequestChanges = true
	default:
		return Settings{}, fmt.Errorf("%w: %s is neither true nor false (unset or empty is false)", ErrSettings, EnvRequestChanges)
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
	return fmt.Sprintf("repository=%s pull_request=%d login=%s request_changes=%t github_token_set=%t model_key_set=%t",
		s.Repository, s.Number, s.BotLogin, s.RequestChanges, s.GitHubToken != "", s.ModelKey != "")
}
