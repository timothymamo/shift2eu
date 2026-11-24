package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// expected returns the job's artifact names and the one it downloads, false
// for an unknown job.
func (j Job) expected() ([]string, string, bool) {
	switch j {
	case JobModel:
		return []string{ArtifactChecksResult}, ArtifactChecksResult, true
	case JobPublish:
		return []string{ArtifactChecksResult, ArtifactModelResult}, ArtifactModelResult, true
	default:
		return nil, "", false
	}
}

// Artifact is the artifact a job downloads, as the checked listing names it:
// its name, its positive ID and its digest, sha256: and 64 lowercase hex. The
// workflow's download step is bound to exactly these (ArtifactFile).
type Artifact struct {
	Name   string `json:"name"`
	ID     int64  `json:"id"`
	Digest string `json:"digest"`
}

// ArtifactFile is the fixed name, under the artifacts subcommand's -tmp
// directory, of the file WriteArtifact writes.
const ArtifactFile = "artifact.json"

// artifactFileMode is the mode of the file WriteArtifact writes.
const artifactFileMode os.FileMode = 0o600

// artifactListing is the part of the run's artifact listing the check reads.
// A pointer field the answer lacks is an answer that is not the listing.
type artifactListing struct {
	TotalCount *int `json:"total_count"`
	Artifacts  *[]struct {
		Name    *string `json:"name"`
		Expired *bool   `json:"expired"`
		ID      *int64  `json:"id"`
		Digest  *string `json:"digest"`
	} `json:"artifacts"`
}

// CheckArtifacts lists the run's artifacts with one call and refuses
// (ErrArtifacts) a list that is not exactly the job's names: a name outside
// them, a name twice, a name missing, a second page (total_count above the
// listed count) or an expired artifact. It returns the artifact the job
// downloads (checks-result for JobModel, model-result for JobPublish) with the
// listing's ID and digest, the values the download is bound to; that entry
// without a positive ID or a sha256:<64 hex> digest is an answer that is not
// the listing. A failed call, a non-2xx status and an answer that is not the
// listing wrap publish.ErrGitHub; a job that is neither JobModel nor
// JobPublish, or no client, wraps publish.ErrConfig. No error names an
// artifact, a URL or any of the answer.
func CheckArtifacts(ctx context.Context, c *Client, job Job) (Artifact, error) {
	want, download, ok := job.expected()
	if !ok {
		return Artifact{}, fmt.Errorf("artifact check: an unknown job: %w", publish.ErrConfig)
	}

	if c == nil {
		return Artifact{}, fmt.Errorf("artifact check: no client: %w", publish.ErrConfig)
	}

	return checkListing(ctx, c, want, download)
}

// checkListing is CheckArtifacts' call and check, for the job's names and the
// one it downloads.
func checkListing(ctx context.Context, c *Client, want []string, download string) (Artifact, error) {
	resp, err := c.Call(ctx, Request{
		Method:   "GET",
		Endpoint: "repos/" + c.scope.Repository + "/actions/runs/" + c.scope.RunID + "/artifacts?per_page=100",
	})
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact check: %w", err)
	}

	if resp.Status/100 != 2 {
		return Artifact{}, fmt.Errorf("artifact check: the listing's status is not 2xx: %w", publish.ErrGitHub)
	}

	var listing artifactListing

	dec := json.NewDecoder(bytes.NewReader(resp.Body))
	if err := dec.Decode(&listing); err != nil || listing.TotalCount == nil || listing.Artifacts == nil {
		return Artifact{}, fmt.Errorf("artifact check: the answer is not the artifact listing: %w", publish.ErrGitHub)
	}

	// One listing and nothing after it: trailing data is no listing either.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Artifact{}, fmt.Errorf("artifact check: data after the artifact listing: %w", publish.ErrGitHub)
	}

	artifacts := *listing.Artifacts

	if *listing.TotalCount != len(artifacts) {
		return Artifact{}, fmt.Errorf("artifact check: the total is not the listed count: %w", ErrArtifacts)
	}

	seen := map[string]bool{}

	var found Artifact

	for _, a := range artifacts {
		if a.Name == nil || a.Expired == nil {
			return Artifact{}, fmt.Errorf("artifact check: the answer is not the artifact listing: %w", publish.ErrGitHub)
		}

		if *a.Expired {
			return Artifact{}, fmt.Errorf("artifact check: an expired artifact: %w", ErrArtifacts)
		}

		if seen[*a.Name] {
			return Artifact{}, fmt.Errorf("artifact check: a name twice: %w", ErrArtifacts)
		}

		seen[*a.Name] = true

		if *a.Name == download {
			if a.ID == nil || *a.ID <= 0 || a.Digest == nil || !imageDigest.MatchString(*a.Digest) {
				return Artifact{}, fmt.Errorf("artifact check: the downloaded artifact has no ID or digest: %w", publish.ErrGitHub)
			}

			found = Artifact{Name: *a.Name, ID: *a.ID, Digest: *a.Digest}
		}
	}

	for _, name := range want {
		if !seen[name] {
			return Artifact{}, fmt.Errorf("artifact check: an expected artifact missing: %w", ErrArtifacts)
		}
	}

	if len(seen) != len(want) {
		return Artifact{}, fmt.Errorf("artifact check: an artifact outside the job's: %w", ErrArtifacts)
	}

	return found, nil
}

// WriteArtifact writes the checked artifact to dir/ArtifactFile as one JSON
// line, {"name":...,"id":...,"digest":...}, opened O_EXCL with mode 0600: a
// path that already exists is refused, never overwritten, and wraps
// publish.ErrConfig, as does a dir that is not absolute or a write or close
// that fails, which also removes the file, so a failed run writes none.
// The workflow's download step reads the ID and digest from that file instead
// of listing the run's artifacts again (review round 1 of PR 5b, finding A4).
func WriteArtifact(dir string, a Artifact) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return fmt.Errorf("artifact file: the directory is not absolute: %w", publish.ErrConfig)
	}

	body, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("artifact file: %w", publish.ErrConfig)
	}

	path := filepath.Join(dir, ArtifactFile)

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, artifactFileMode) //nolint:gosec // G304: the fixed file name under the step's own -tmp directory
	if err != nil {
		return fmt.Errorf("artifact file: cannot create it: %w", publish.ErrConfig)
	}

	_, writeErr := file.Write(append(body, '\n'))
	closeErr := file.Close()

	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)

		return fmt.Errorf("artifact file: cannot write it: %w", publish.ErrConfig)
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
