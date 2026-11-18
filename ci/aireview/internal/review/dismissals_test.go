package review_test

import (
	"slices"
	"testing"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// TestDismissalsForDismissesTheReviewersOwnChangeRequestsOnly pins ADR 0033's
// dismissal rule: once nothing blocks, the reviewer withdraws its own earlier
// change requests, and nothing else. Another login's change request (the
// maintainer's, another App's) stays, one of its own in another state stays, and
// while a finding blocks it withdraws nothing.
func TestDismissalsForDismissesTheReviewersOwnChangeRequestsOnly(t *testing.T) {
	t.Parallel()

	blocking := review.Finding{Severity: review.SeverityBlocker, Category: review.CategoryCorrectness, Title: "t", Body: "b"}
	chatty := review.Finding{Severity: review.SeveritySuggestion, Category: review.CategoryReadability, Title: "t", Body: "b"}

	reviews := []review.PostedReview{
		{ID: 11, Login: testBotLogin, State: "CHANGES_REQUESTED"},
		{ID: 12, Login: "maintainer", State: "CHANGES_REQUESTED"},
		{ID: 13, Login: "other-app[bot]", State: "CHANGES_REQUESTED"},
		{ID: 14, Login: testBotLogin, State: "COMMENTED"},
		{ID: 15, Login: testBotLogin, State: "APPROVED"},
		{ID: 16, Login: testBotLogin, State: "CHANGES_REQUESTED"},
	}

	cases := []struct {
		name     string
		reviews  []review.PostedReview
		botLogin string
		findings []review.Finding
		want     []int64
	}{
		{
			name:     "the reviewer's own change requests, in the order given",
			reviews:  reviews,
			botLogin: testBotLogin,
			findings: []review.Finding{chatty},
			want:     []int64{11, 16},
		},
		{
			name:     "nothing while a finding is blocking",
			reviews:  reviews,
			botLogin: testBotLogin,
			findings: []review.Finding{chatty, blocking},
			want:     nil,
		},
		{
			name:     "an empty login dismisses nothing",
			reviews:  reviews,
			botLogin: "",
			findings: []review.Finding{chatty},
			want:     nil,
		},
		{
			name:     "another login's change request stays",
			reviews:  []review.PostedReview{{ID: 31, Login: "maintainer", State: "CHANGES_REQUESTED"}},
			botLogin: testBotLogin,
			findings: nil,
			want:     nil,
		},
		{
			name:     "a login that differs in case is another login",
			reviews:  []review.PostedReview{{ID: 41, Login: "Shift2EU-Reviewer[bot]", State: "CHANGES_REQUESTED"}},
			botLogin: testBotLogin,
			findings: nil,
			want:     nil,
		},
		{
			name:     "the reviewer's own review in another state stays",
			reviews:  []review.PostedReview{{ID: 51, Login: testBotLogin, State: "COMMENTED"}, {ID: 52, Login: testBotLogin, State: "DISMISSED"}},
			botLogin: testBotLogin,
			findings: nil,
			want:     nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got []int64

			for _, dismissed := range review.DismissalsFor(tc.reviews, tc.botLogin, tc.findings) {
				got = append(got, dismissed.ID)
			}

			if !slices.Equal(tc.want, got) {
				t.Errorf("DismissalsFor(%+v, %q, %+v) dismissed %v, want %v", tc.reviews, tc.botLogin, tc.findings, got, tc.want)
			}
		})
	}
}
