package github

import (
	"context"
	"fmt"
)

// Job is a job of the audit workflow that downloads an artifact.
type Job int

// The two jobs that list the run's artifacts before downloading one.
const (
	// JobModel expects exactly checks-result.
	JobModel Job = iota + 1
	// JobPublish expects exactly checks-result and model-result.
	JobPublish
)

// The artifacts' names (PRD #175 milestone 2).
const (
	ArtifactChecksResult = "checks-result"
	ArtifactModelResult  = "model-result"
)

// CheckArtifacts lists the run's artifacts with one call and refuses
// (ErrArtifacts) a list that is not exactly the job's names: a name outside
// them, a name twice, a name missing, a second page (total_count above the
// listed count) or an expired artifact. A failed call wraps publish.ErrGitHub;
// a job that is neither JobModel nor JobPublish wraps publish.ErrConfig.
func CheckArtifacts(_ context.Context, _ *Client, _ Job) error {
	return fmt.Errorf("artifact check: %w", errNotImplemented)
}

// ArtifactsOutcome returns the artifact check's one summary line and exit
// code, from the error's class alone: nil "artifacts: as expected" (0),
// ErrArtifacts "artifacts: unexpected: see runbook step 11" (3),
// publish.ErrGitHub "GitHub call failed: see runbook" (8), publish.ErrConfig
// "configuration invalid: see runbook" (9), any other "publish failed: see
// runbook" (1).
func ArtifactsOutcome(_ error) (string, int) {
	return "publish failed: see runbook", 1
}
