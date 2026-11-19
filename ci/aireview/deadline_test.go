package main

import (
	"testing"
	"time"
)

// TestRunDeadlineFitsTheJob pins the timing arithmetic of auditor L3: the run's
// deadline leaves a model call its whole timeout, and the run with the one post
// Run makes past it (at most 30 seconds, issue #316) still ends inside the job's
// timeout-minutes of 20 (.github/workflows/ai-review.yml) with minutes to spare
// for the steps before it. Not parallel, as the package's other test is not.
func TestRunDeadlineFitsTheJob(t *testing.T) {
	const (
		jobTimeout        = 20 * time.Minute // timeout-minutes in .github/workflows/ai-review.yml
		detachedPost      = 30 * time.Second // the review saying the answer could not be read
		stepsBeforeTheRun = 3 * time.Minute  // build, install the CLI, mint the token
	)

	if runDeadline <= modelTimeout {
		t.Errorf("runDeadline %s is not longer than modelTimeout %s: one model call could not use its whole timeout", runDeadline, modelTimeout)
	}

	if total := stepsBeforeTheRun + runDeadline + detachedPost; total > jobTimeout {
		t.Errorf("the steps before the run, the run and its detached post take up to %s, over the job's %s: the job would be killed before the run ends", total, jobTimeout)
	}
}
