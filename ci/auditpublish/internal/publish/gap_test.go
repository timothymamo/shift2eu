package publish_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// TestGapThresholdDays pins the one constant the schedule's staleness is
// stated in: the longest month plus seven days of slack.
func TestGapThresholdDays(t *testing.T) {
	t.Parallel()

	if publish.GapThresholdDays != 38 {
		t.Errorf("GapThresholdDays = %d, want 38: the longest month, 31 days, plus 7 days of slack", publish.GapThresholdDays)
	}
}

// daysBetween returns the whole days from one date to another, read from their
// Unix seconds, the way the gap is stated: a time.Duration saturates at about
// 292 years, so a Subtract would not reach the year 0001.
func daysBetween(t *testing.T, from, to string) string {
	t.Helper()

	day := func(date string) time.Time {
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Fatalf("time.Parse(%q): %v", date, err)
		}

		return parsed
	}

	return strconv.Itoa(int((day(to).Unix() - day(from).Unix()) / 86400))
}

// TestGapLine pins the gap line: the Changes section says the previous report
// is <n> days older than this one when its date is more than GapThresholdDays
// whole days before the run's, and says nothing when it is not.
func TestGapLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		previous string
		// wantDays is the gap the line must state; 0 when the report must hold
		// no line at all.
		wantDays string
	}{
		{
			name:     "the previous report is a week old",
			previous: "2026-11-16",
		},
		{
			name:     "the previous report is GapThresholdDays old",
			previous: "2026-10-16",
		},
		{
			name:     "the previous report is one day past the threshold",
			previous: "2026-10-15",
			wantDays: "39",
		},
		{
			name:     "the previous report is a month past the threshold",
			previous: "2026-09-15",
			wantDays: "69",
		},
		{
			// A report from the year 0001 is further back than a
			// time.Duration can hold, about 292 years: the gap is read from
			// Unix seconds, so it is the real 739942 days and no saturation.
			name:     "the previous report is in the year 0001",
			previous: "0001-01-01",
			wantDays: daysBetween(t, "0001-01-01", testDate),
		},
	}

	if daysBetween(t, "0001-01-01", testDate) == "106751" {
		t.Fatalf("the fixture's gap saturates at a time.Duration's 106751 days, so this row cannot pin the Unix-seconds rule")
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			previous := previousReport(t, fixtureResult(), publish.PrivateNone, tc.previous, "1")

			body := renderReport(t, publish.ReportInput{
				Result:   fixtureResult(),
				Run:      testRun(t, testDate, testRunID),
				Private:  publish.PrivateNone,
				Previous: previous,
			}).Body

			changes := sectionText(t, body, "Changes since the last audit")

			if tc.wantDays == "" {
				if strings.Contains(changes, "Gap in the schedule") {
					t.Errorf("the Changes section holds a gap line for a report %s old, want none:\n%s", tc.name, changes)
				}

				return
			}

			want := "Gap in the schedule: the previous report is " + tc.wantDays + " days older than this one."
			if !strings.Contains(changes, want) {
				t.Errorf("the Changes section does not hold %q:\n%s", want, changes)
			}
		})
	}
}
