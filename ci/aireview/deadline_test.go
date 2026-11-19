package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// aiReviewWorkflow is the workflow that runs this program, relative to this
// package's directory: the test is run from there.
const aiReviewWorkflow = "../../.github/workflows/ai-review.yml"

// jobTimeoutFromWorkflow reads the AI review job's timeout-minutes from the
// workflow that runs this program (issue #316): the number the run's deadline
// has to fit in is the one CI enforces, not a copy of it that could drift.
func jobTimeoutFromWorkflow(t *testing.T) time.Duration {
	t.Helper()

	workflow, err := os.ReadFile(aiReviewWorkflow)
	if err != nil {
		t.Fatalf("read %s: %v", aiReviewWorkflow, err)
	}

	var minutes []string

	for _, line := range strings.Split(string(workflow), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "timeout-minutes:"); ok {
			minutes = append(minutes, strings.TrimSpace(rest))
		}
	}

	if len(minutes) != 1 {
		t.Fatalf("%s names %d timeout-minutes (%q), want exactly one: the AI review job's, which the run's deadline must fit in (issue #316)",
			aiReviewWorkflow, len(minutes), minutes)
	}

	value, err := strconv.Atoi(minutes[0])
	if err != nil {
		t.Fatalf("%s's timeout-minutes is %q, want a whole number of minutes: %v", aiReviewWorkflow, minutes[0], err)
	}

	return time.Duration(value) * time.Minute
}

// TestRunDeadlineFitsTheJob pins the timing arithmetic of auditor L3: the run's
// deadline leaves a model call its whole timeout, and the run with the one post
// Run makes past it (bounded by review.UnreadablePostTimeout, issue #316) still
// ends inside the job's timeout-minutes (.github/workflows/ai-review.yml, read
// here rather than copied, so a change to either real value fails this test)
// with minutes to spare for the steps before it. Not parallel, as the package's
// other test is not.
func TestRunDeadlineFitsTheJob(t *testing.T) {
	const stepsBeforeTheRun = 3 * time.Minute // build, install the CLI, mint the token

	jobTimeout := jobTimeoutFromWorkflow(t)
	detachedPost := review.UnreadablePostTimeout

	if runDeadline <= modelTimeout {
		t.Errorf("runDeadline %s is not longer than modelTimeout %s: one model call could not use its whole timeout", runDeadline, modelTimeout)
	}

	if total := stepsBeforeTheRun + runDeadline + detachedPost; total > jobTimeout {
		t.Errorf("the steps before the run, the run and its detached post take up to %s, over the job's %s: the job would be killed before the run ends", total, jobTimeout)
	}
}
