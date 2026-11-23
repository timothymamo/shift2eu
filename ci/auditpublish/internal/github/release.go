package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

var (
	releaseTag  = regexp.MustCompile(`^` + tagPattern + `$`)
	imageDigest = regexp.MustCompile(`^` + digestPattern + `$`)
)

// The attestation a released image's digest must have: an in-toto statement
// in a DSSE envelope whose predicate is SLSA provenance.
const (
	inTotoPayloadType = "application/vnd.in-toto+json"
	slsaProvenance    = "https://slsa.dev/provenance/v1"
	// workflowServer is the server an attestation's workflow repository
	// names: the one server the publish program publishes to.
	workflowServer = "https://github.com/"
)

// NewImageClient returns the image subcommand's Client: built as NewClient
// builds one, with the same refusals, but read-only whatever cfg.ReadOnly
// says, like NewArtifactsClient: the client that holds the job's token
// reaches no wider than the attestation listing it reads.
func NewImageClient(cfg ClientConfig) (*Client, error) {
	cfg.ReadOnly = true

	return NewClient(cfg)
}

// CheckRelease refuses a result whose release tag names no release of the
// repository: one call, GET R/releases/tags/<tag> (endpoint row 2). 200 is a
// release; 404 wraps publish.ErrResultInvalid; a tag the endpoint table
// refuses (not v<major>.<minor>.<patch>) also wraps publish.ErrResultInvalid,
// before gh runs; any other status, an answer whose tag_name is not the tag,
// and a failed call wrap publish.ErrGitHub.
func CheckRelease(ctx context.Context, c *Client, tag string) error {
	if c == nil {
		return fmt.Errorf("release check: no client: %w", publish.ErrConfig)
	}

	if !releaseTag.MatchString(tag) {
		return fmt.Errorf("release check: the tag is no release tag: %w", publish.ErrResultInvalid)
	}

	resp, err := c.Call(ctx, Request{Method: "GET", Endpoint: "repos/" + c.scope.Repository + "/releases/tags/" + tag})
	if err != nil {
		return fmt.Errorf("release check: %w", err)
	}

	switch {
	case resp.Status == 404:
		return fmt.Errorf("release check: the tag names no release: %w", publish.ErrResultInvalid)
	case resp.Status/100 != 2:
		return fmt.Errorf("release check: the lookup's status is not 2xx: %w", publish.ErrGitHub)
	}

	var release struct {
		TagName *string `json:"tag_name"`
	}

	if json.Unmarshal(resp.Body, &release) != nil || release.TagName == nil || *release.TagName != tag {
		return fmt.Errorf("release check: the answer is not the tag's release: %w", publish.ErrGitHub)
	}

	return nil
}

// attestationListing is the part of the attestation listing the check reads.
type attestationListing struct {
	Attestations *[]struct {
		Bundle struct {
			DSSEEnvelope struct {
				PayloadType string `json:"payloadType"`
				Payload     string `json:"payload"`
			} `json:"dsseEnvelope"`
		} `json:"bundle"`
	} `json:"attestations"`
}

// provenanceStatement is the part of an in-toto statement the check reads.
type provenanceStatement struct {
	PredicateType string `json:"predicateType"`
	Subject       []struct {
		Digest struct {
			SHA256 string `json:"sha256"`
		} `json:"digest"`
	} `json:"subject"`
	Predicate struct {
		BuildDefinition struct {
			ExternalParameters struct {
				Workflow struct {
					Ref        string `json:"ref"`
					Repository string `json:"repository"`
				} `json:"workflow"`
			} `json:"externalParameters"`
		} `json:"buildDefinition"`
	} `json:"predicate"`
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
// It reads the statement only: it does not verify the bundle's Sigstore
// signature or certificate (gh attestation verify would, and is outside the
// gh api contract). What it proves is that GitHub holds a provenance
// attestation of this repository's release workflow for the digest.
//
// It runs on the image subcommand's read-only client, with the job's token
// and attestations: read, never with the audit App's token, which has no
// Attestations permission (PRD #175, maintainer decision 2).
func CheckImageDigest(ctx context.Context, c *Client, tag, digest string) error {
	if digest == "" {
		return nil
	}

	if c == nil {
		return fmt.Errorf("image check: no client: %w", publish.ErrConfig)
	}

	if !imageDigest.MatchString(digest) || !releaseTag.MatchString(tag) {
		return fmt.Errorf("image check: the digest or the tag is outside its pattern: %w", publish.ErrResultInvalid)
	}

	resp, err := c.Call(ctx, Request{Method: "GET", Endpoint: "repos/" + c.scope.Repository + "/attestations/" + digest + "?per_page=100"})
	if err != nil {
		return fmt.Errorf("image check: %w", err)
	}

	switch {
	case resp.Status == 404:
		return fmt.Errorf("image check: the digest has no attestation: %w", publish.ErrResultInvalid)
	case resp.Status/100 != 2:
		return fmt.Errorf("image check: the listing's status is not 2xx: %w", publish.ErrGitHub)
	}

	var listing attestationListing
	if json.Unmarshal(resp.Body, &listing) != nil || listing.Attestations == nil {
		return fmt.Errorf("image check: the answer is not the attestation listing: %w", publish.ErrGitHub)
	}

	hexDigest := strings.TrimPrefix(digest, "sha256:")
	repository := workflowServer + c.scope.Repository

	for _, a := range *listing.Attestations {
		envelope := a.Bundle.DSSEEnvelope
		if envelope.PayloadType != inTotoPayloadType {
			continue
		}

		payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
		if err != nil {
			continue
		}

		if provenanceMatches(payload, hexDigest, "refs/tags/"+tag, repository) {
			return nil
		}
	}

	return fmt.Errorf("image check: no provenance attestation of the release names the digest: %w", publish.ErrResultInvalid)
}

// provenanceMatches reports whether payload is an in-toto statement of SLSA
// provenance with a subject of the digest's hex, built by the workflow ref
// of the repository.
func provenanceMatches(payload []byte, hexDigest, ref, repository string) bool {
	var s provenanceStatement

	dec := json.NewDecoder(bytes.NewReader(payload))
	if dec.Decode(&s) != nil {
		return false
	}

	w := s.Predicate.BuildDefinition.ExternalParameters.Workflow
	if s.PredicateType != slsaProvenance || w.Ref != ref || w.Repository != repository {
		return false
	}

	for _, subject := range s.Subject {
		if subject.Digest.SHA256 == hexDigest {
			return true
		}
	}

	return false
}

// ImageOutcome returns the image subcommand's one summary line and exit code,
// from the error's class alone: nil "image digest: as expected" (0),
// publish.ErrResultInvalid, publish.ErrGitHub and publish.ErrConfig the
// core's lines and codes (3, 8, 9), any other the core's "publish failed: see
// runbook" (1).
func ImageOutcome(err error) (string, int) {
	var o publish.Outcome

	switch {
	case err == nil:
		return "image digest: as expected", 0
	case errors.Is(err, publish.ErrResultInvalid), errors.Is(err, publish.ErrGitHub), errors.Is(err, publish.ErrConfig):
		o = publish.OutcomeOf(err)
	default:
		o = publish.OutcomeOf(errUnclassified)
	}

	return o.Summary(), o.Code()
}
