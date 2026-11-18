package review

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
	_ = lookup // the skeleton's stub (step 1)

	return Settings{}, errNotImplemented
}

// String describes s without its credentials: the repository, number, login
// and switch, and whether each credential is set, never its value.
func (s Settings) String() string {
	return ""
}
