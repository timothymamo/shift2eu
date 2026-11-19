package review

// The run-outcome API is unexported: only Ask uses it. The black-box tests
// reach it through these aliases, which exist in test builds alone (issue
// #302).

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
