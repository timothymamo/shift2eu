package review_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// TestDecideRunOutcomePinsWhichOutcomeWins pins the decision a run of the
// Claude Code CLI makes from plain values, with no process and no clock (ADR
// 0033, issue #274): a run whose result is read uses that result, whatever its
// contexts say afterwards, so a result that arrives just as the deadline passes
// is an answer and not a timeout. Only a run with no usable result reports the
// caller's cancellation, then the run's own deadline, then the run's own
// failure.
func TestDecideRunOutcomePinsWhichOutcomeWins(t *testing.T) {
	t.Parallel()

	noResult := errors.New("the Claude Code CLI printed no result")

	cases := []struct {
		name      string
		answerErr error
		parentErr error
		runErr    error
		want      review.RunOutcome
	}{
		{
			name: "a result before the deadline is the answer",
			want: review.OutcomeAnswer,
		},
		{
			name:   "a result exactly at the deadline is the answer, not a timeout",
			runErr: context.DeadlineExceeded,
			want:   review.OutcomeAnswer,
		},
		{
			name:      "a result after the caller cancelled is the answer",
			parentErr: context.Canceled,
			runErr:    context.Canceled,
			want:      review.OutcomeAnswer,
		},
		{
			name:      "the deadline first is a timeout",
			answerErr: noResult,
			runErr:    context.DeadlineExceeded,
			want:      review.OutcomeTimedOut,
		},
		{
			name:      "a wrapped deadline is a timeout too",
			answerErr: noResult,
			runErr:    fmt.Errorf("wait for the Claude Code CLI: %w", context.DeadlineExceeded),
			want:      review.OutcomeTimedOut,
		},
		{
			name:      "a cancelled caller is a cancellation",
			answerErr: noResult,
			parentErr: context.Canceled,
			runErr:    context.Canceled,
			want:      review.OutcomeCancelled,
		},
		{
			name:      "a cancellation wins over the run's deadline",
			answerErr: noResult,
			parentErr: context.Canceled,
			runErr:    context.DeadlineExceeded,
			want:      review.OutcomeCancelled,
		},
		{
			name:      "a plain failure is the run's own error",
			answerErr: noResult,
			want:      review.OutcomeFailed,
		},
		{
			name:      "a run that failed on its own error is not a timeout",
			answerErr: noResult,
			runErr:    errors.New("exit status 1"),
			want:      review.OutcomeFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := review.DecideRunOutcome(tc.answerErr, tc.parentErr, tc.runErr); got != tc.want {
				t.Errorf("DecideRunOutcome(%v, %v, %v) = %d, want %d", tc.answerErr, tc.parentErr, tc.runErr, got, tc.want)
			}
		})
	}
}
