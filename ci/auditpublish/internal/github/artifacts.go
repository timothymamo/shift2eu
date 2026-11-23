package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
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

// expected returns the job's artifact names, false for an unknown job.
func (j Job) expected() ([]string, bool) {
	switch j {
	case JobModel:
		return []string{ArtifactChecksResult}, true
	case JobPublish:
		return []string{ArtifactChecksResult, ArtifactModelResult}, true
	default:
		return nil, false
	}
}

// artifactListing is the part of the run's artifact listing the check reads.
// A pointer field the answer lacks is an answer that is not the listing.
type artifactListing struct {
	TotalCount *int `json:"total_count"`
	Artifacts  *[]struct {
		Name    *string `json:"name"`
		Expired *bool   `json:"expired"`
	} `json:"artifacts"`
}

// CheckArtifacts lists the run's artifacts with one call and refuses
// (ErrArtifacts) a list that is not exactly the job's names: a name outside
// them, a name twice, a name missing, a second page (total_count above the
// listed count) or an expired artifact. A failed call, a non-2xx status and an
// answer that is not the listing wrap publish.ErrGitHub; a job that is neither
// JobModel nor JobPublish, or no client, wraps publish.ErrConfig. No error
// names an artifact, a URL or any of the answer.
func CheckArtifacts(ctx context.Context, c *Client, job Job) error {
	want, ok := job.expected()
	if !ok {
		return fmt.Errorf("artifact check: an unknown job: %w", publish.ErrConfig)
	}

	if c == nil {
		return fmt.Errorf("artifact check: no client: %w", publish.ErrConfig)
	}

	resp, err := c.Call(ctx, Request{
		Method:   "GET",
		Endpoint: "repos/" + c.scope.Repository + "/actions/runs/" + c.scope.RunID + "/artifacts?per_page=100",
	})
	if err != nil {
		return fmt.Errorf("artifact check: %w", err)
	}

	if resp.Status/100 != 2 {
		return fmt.Errorf("artifact check: the listing's status is not 2xx: %w", publish.ErrGitHub)
	}

	var listing artifactListing

	dec := json.NewDecoder(bytes.NewReader(resp.Body))
	if err := dec.Decode(&listing); err != nil || listing.TotalCount == nil || listing.Artifacts == nil {
		return fmt.Errorf("artifact check: the answer is not the artifact listing: %w", publish.ErrGitHub)
	}

	artifacts := *listing.Artifacts

	if *listing.TotalCount != len(artifacts) {
		return fmt.Errorf("artifact check: the total is not the listed count: %w", ErrArtifacts)
	}

	seen := map[string]bool{}

	for _, a := range artifacts {
		if a.Name == nil || a.Expired == nil {
			return fmt.Errorf("artifact check: the answer is not the artifact listing: %w", publish.ErrGitHub)
		}

		if *a.Expired {
			return fmt.Errorf("artifact check: an expired artifact: %w", ErrArtifacts)
		}

		if seen[*a.Name] {
			return fmt.Errorf("artifact check: a name twice: %w", ErrArtifacts)
		}

		seen[*a.Name] = true
	}

	for _, name := range want {
		if !seen[name] {
			return fmt.Errorf("artifact check: an expected artifact missing: %w", ErrArtifacts)
		}
	}

	if len(seen) != len(want) {
		return fmt.Errorf("artifact check: an artifact outside the job's: %w", ErrArtifacts)
	}

	return nil
}

// errUnclassified stands for an error of no class the artifact check's
// outcome knows, so that its line is the core's "publish failed".
var errUnclassified = errors.New("unclassified")

// ArtifactsOutcome returns the artifact check's one summary line and exit
// code, from the error's class alone: nil "artifacts: as expected" (0),
// ErrArtifacts "artifacts: unexpected: see runbook step 11" (3),
// publish.ErrGitHub and publish.ErrConfig the core's lines and codes ("GitHub
// call failed: see runbook", 8; "configuration invalid: see runbook", 9), any
// other the core's "publish failed: see runbook" (1).
func ArtifactsOutcome(err error) (string, int) {
	var o publish.Outcome

	switch {
	case err == nil:
		return "artifacts: as expected", 0
	case errors.Is(err, ErrArtifacts):
		return "artifacts: unexpected: see runbook step 11", 3
	case errors.Is(err, publish.ErrGitHub), errors.Is(err, publish.ErrConfig):
		o = publish.OutcomeOf(err)
	default:
		o = publish.OutcomeOf(errUnclassified)
	}

	return o.Summary(), o.Code()
}
