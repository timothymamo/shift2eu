package claudecode

// The run-outcome API is unexported: only Ask uses it. The black-box tests
// reach it, and the arguments a config gives, through these aliases, which
// exist in test builds alone (issue #302).

// RunOutcome is runOutcome, for the tests.
type RunOutcome = runOutcome

// The outcomes of a run, for the tests.
const (
	OutcomeFailed    = outcomeFailed
	OutcomeAnswer    = outcomeAnswer
	OutcomeCancelled = outcomeCancelled
	OutcomeTimedOut  = outcomeTimedOut
)

// DecideRunOutcome is decideRunOutcome, for the tests.
var DecideRunOutcome = decideRunOutcome

// Args returns the arguments Ask runs the CLI with for cfg, the system prompt
// in systemFile, for the tests.
func Args(cfg Config, systemFile string) []string {
	return New(cfg).args(systemFile)
}

// Env returns the whole environment Ask runs the CLI with for cfg, its HOME
// being home, for the tests: the allow list, nothing inherited.
func Env(cfg Config, home string) []string {
	return New(cfg).env(home)
}
