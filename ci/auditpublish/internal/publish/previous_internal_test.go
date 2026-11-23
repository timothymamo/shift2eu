// This file is in package publish rather than publish_test on purpose: a
// Previous is meant to come from ParsePreviousReport alone, and the hand-built
// value RenderReport must refuse, one carrying a report's name and date, can be
// built only from inside the package. The black-box test next door pins the
// zero Previous, which carries neither.

package publish

import (
	"errors"
	"testing"
	"time"
)

// TestRenderReportRefusesAHandBuiltPrevious pins that RenderReport renders from
// a previous report ParsePreviousReport returned and from no other: a value
// that carries a report's name and date, but which no parse produced, is
// refused with ErrPreviousReport.
func TestRenderReportRefusesAHandBuiltPrevious(t *testing.T) {
	t.Parallel()

	previous := Previous{
		name: "2026-10-03-18422000001.md",
		date: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
	}

	run, err := NewRun("https://github.com", "example/shift2eu", "18422000001", "2026-11-23")
	if err != nil {
		t.Fatalf("NewRun: %v", err)
	}

	rendered, err := RenderReport(ReportInput{
		Result:   smallestResult(),
		Run:      run,
		Private:  PrivateNone,
		Previous: &previous,
	})
	if err == nil {
		t.Fatalf("RenderReport with a hand-built Previous returned the report %q, want an error wrapping ErrPreviousReport", rendered.Body)
	}

	if !errors.Is(err, ErrPreviousReport) {
		t.Errorf("RenderReport with a hand-built Previous: error %v does not wrap ErrPreviousReport", err)
	}
}

// smallestResult is the smallest result the schema accepts: every check ran and
// no finding, dependency or limit. It is built here so the test needs none of
// the black-box fixtures.
func smallestResult() Result {
	checks := make([]Check, 0, len(checkNames))
	for _, c := range checkNames {
		checks = append(checks, Check{ID: c.id, Ran: true})
	}

	return Result{
		Schema:     SchemaVersion,
		Commit:     "0123456789abcdef0123456789abcdef01234567",
		ReleaseTag: "v0.1.0",
		Tools:      []Tool{},
		Checks:     checks,
		Findings:   []Finding{},
		Changes:    Changes{Dependencies: []string{}},
		Limits:     []string{},
	}
}
