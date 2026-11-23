package publish_test

import (
	"errors"
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
			// A second run on the run's own date: the gap is 0, so the
			// Changes section says nothing about it.
			name:     "the previous report is from the run's own date",
			previous: testDate,
		},
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

// TestRenderReportRefusesAPreviousReportNotBeforeTheRun pins that a previous
// report not before the run is refused: one dated after the run, and one from
// the run's own date whose run ID is equal to or greater than the run's own,
// which a hand-made or a later run's report can be. The refusal wraps
// ErrPreviousReport with the rule's own line, and the error's text names
// neither date nor run ID (ADR 0040, the core's errors name a rule, never a
// value). A previous report from an earlier date is accepted whatever its run
// ID, and one from the run's own date with a smaller run ID too.
func TestRenderReportRefusesAPreviousReportNotBeforeTheRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		previous string
		id       string
		// wantRule is the rule the error must state; empty when the report
		// must be rendered.
		wantRule string
	}{
		{
			name:     "the previous report is dated one day after the run",
			previous: "2026-11-24",
			id:       "1",
			wantRule: "dated after the run",
		},
		{
			name:     "the previous report is dated a month after the run",
			previous: "2026-12-23",
			id:       "1",
			wantRule: "dated after the run",
		},
		{
			// The PRD's named case: the same UTC date and the run's own run ID.
			name:     "the previous report is from the run's own date with the same run ID",
			previous: testDate,
			id:       testRunID,
			wantRule: "not before the run",
		},
		{
			name:     "the previous report is from the run's own date with a greater run ID",
			previous: testDate,
			id:       "18422000002",
			wantRule: "not before the run",
		},
		{
			// Run IDs grow with time, so a smaller one on the run's own date is
			// an earlier run.
			name:     "the previous report is from the run's own date with a smaller run ID",
			previous: testDate,
			id:       "18422000000",
		},
		{
			name:     "the previous report is from an earlier date with a greater run ID",
			previous: "2026-11-22",
			id:       "18422000002",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			previous := previousReport(t, fixtureResult(), publish.PrivateNone, tc.previous, tc.id)

			_, err := publish.RenderReport(publish.ReportInput{
				Result:   fixtureResult(),
				Run:      testRun(t, testDate, testRunID),
				Private:  publish.PrivateNone,
				Previous: previous,
			})

			if tc.wantRule == "" {
				if err != nil {
					t.Fatalf("RenderReport with the previous report %s returned %v, want the report", previousName(tc.previous, tc.id), err)
				}

				return
			}

			if err == nil {
				t.Fatalf("RenderReport with the previous report %s returned a report, want an error wrapping ErrPreviousReport", previousName(tc.previous, tc.id))
			}

			if !errors.Is(err, publish.ErrPreviousReport) {
				t.Errorf("RenderReport with the previous report %s: error %v does not wrap ErrPreviousReport", previousName(tc.previous, tc.id), err)
			}

			want := "previous report: " + tc.wantRule
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the error %q does not hold %q, want the rule's own line", err, want)
			}

			for _, value := range []string{tc.previous, testDate, tc.id, testRunID} {
				if strings.Contains(err.Error(), value) {
					t.Errorf("the error %q names %s, want no value", err, value)
				}
			}
		})
	}
}

// previousName is the name of the previous report a case renders: its date and
// its run ID, as the report's file name has them.
func previousName(date, id string) string {
	return date + "-" + id + ".md"
}
