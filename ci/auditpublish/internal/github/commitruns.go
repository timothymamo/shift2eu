package github

import "context"

// MaxCommitRuns is the most distinct 40-hex runs CheckCommitRuns looks up:
// more are refused (publish.ErrScanRefused) before any call.
const MaxCommitRuns = 20

// CheckCommitRuns checks every distinct 40-hex run of the public model
// fields (ADR 0040's residual risk of a hex-encoded secret): a maximal run of
// exactly 40 hex digits, either case, read in lower case. Each must be a
// commit of the repository: GET R/git/commits/<run> (row 4) answering 200.
// A 404 and more than MaxCommitRuns distinct runs wrap publish.ErrScanRefused,
// the latter before any call; any other non-2xx status and a failed call
// wrap publish.ErrGitHub. No error names a run. Each distinct run is looked
// up once, in the order of its first appearance.
func CheckCommitRuns(_ context.Context, _ *Client, _ []string) error {
	return errNotImplemented
}
