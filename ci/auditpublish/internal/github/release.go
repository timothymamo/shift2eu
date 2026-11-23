package github

import (
	"context"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// NewImageClient returns the image subcommand's Client: built as NewClient
// builds one, with the same refusals, but read-only whatever cfg.ReadOnly
// says, like NewArtifactsClient: the client that holds the job's token
// reaches no wider than the attestation listing it reads.
func NewImageClient(_ ClientConfig) (*Client, error) {
	return nil, errNotImplemented
}

// CheckRelease refuses a result whose release tag names no release of the
// repository: one call, GET R/releases/tags/<tag> (endpoint row 2). 200 is a
// release; 404 wraps publish.ErrResultInvalid; a tag the endpoint table
// refuses (not v<major>.<minor>.<patch>) also wraps publish.ErrResultInvalid,
// before gh runs; any other status, an answer whose tag_name is not the tag,
// and a failed call wrap publish.ErrGitHub.
func CheckRelease(_ context.Context, _ *Client, _ string) error {
	return errNotImplemented
}

// CheckImageDigest checks the result's image digest: an empty digest is
// nothing to check and makes no call; otherwise one call, GET
// R/attestations/<digest>?per_page=100 (endpoint row 3), whose answer must
// hold at least one attestation whose DSSE envelope's payload type is
// application/vnd.in-toto+json and whose statement, base64-decoded, has the
// predicate type https://slsa.dev/provenance/v1, a subject whose sha256
// digest is the digest's hex, and a workflow (predicate.buildDefinition.
// externalParameters.workflow) whose ref is refs/tags/<tag> and whose
// repository is https://github.com/<the scope's repository>. No such
// attestation, a 404 and a digest the endpoint table refuses wrap
// publish.ErrResultInvalid; any other non-2xx status, an answer that is not
// the listing and a failed call wrap publish.ErrGitHub.
//
// It runs on the image subcommand's read-only client, with the job's token
// and attestations: read, never with the audit App's token, which has no
// Attestations permission (PRD #175, maintainer decision 2).
func CheckImageDigest(_ context.Context, _ *Client, _, _ string) error {
	return errNotImplemented
}

// ImageOutcome returns the image subcommand's one summary line and exit code,
// from the error's class alone: nil "image digest: as expected" (0),
// publish.ErrResultInvalid, publish.ErrGitHub and publish.ErrConfig the
// core's lines and codes (3, 8, 9), any other the core's "publish failed: see
// runbook" (1).
func ImageOutcome(_ error) (string, int) {
	o := publish.OutcomeOf(errNotImplemented)

	return o.Summary(), o.Code()
}
