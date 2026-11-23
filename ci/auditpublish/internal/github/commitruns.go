package github

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// MaxCommitRuns is the most distinct 40-hex runs CheckCommitRuns looks up:
// more are refused (publish.ErrScanRefused) before any call.
const MaxCommitRuns = 20

// commitRunLength is the length of a run CheckCommitRuns looks up: a commit
// SHA's.
const commitRunLength = 40

// CheckCommitRuns checks every distinct 40-hex run of the public model
// fields (ADR 0040's residual risk of a hex-encoded secret): a maximal run of
// exactly 40 hex digits, either case, read in lower case. Each must be a
// commit of the repository: GET R/git/commits/<run> (row 4) answering 2xx
// with the run as its sha. A 404, a 2xx answer whose sha is absent or another
// and more than MaxCommitRuns distinct runs wrap publish.ErrScanRefused,
// the latter before any call; any other non-2xx status and a failed call
// wrap publish.ErrGitHub. No error names a run. Each distinct run is looked
// up once, in the order of its first appearance.
func CheckCommitRuns(ctx context.Context, c *Client, fields []string) error {
	if c == nil {
		return fmt.Errorf("commit runs: no client: %w", publish.ErrConfig)
	}

	var runs []string

	for _, field := range fields {
		for _, run := range hexRuns(field) {
			if !slices.Contains(runs, run) {
				runs = append(runs, run)
			}
		}
	}

	if len(runs) > MaxCommitRuns {
		return fmt.Errorf("commit runs: more than %d distinct 40-hex runs: %w", MaxCommitRuns, publish.ErrScanRefused)
	}

	for _, run := range runs {
		resp, err := read(ctx, c, "repos/"+c.scope.Repository+"/git/commits/"+run)
		if err != nil {
			return fmt.Errorf("commit runs: %w", err)
		}

		switch {
		case resp.Status == 404:
			return fmt.Errorf("commit runs: a 40-hex run that is no commit: %w", publish.ErrScanRefused)
		case resp.Status/100 != 2:
			return fmt.Errorf("commit runs: the lookup's status is not 2xx: %w", publish.ErrGitHub)
		}

		var commit struct {
			SHA string `json:"sha"`
		}

		// The answer must be the run's own commit: an absent or another sha
		// is no proof that the run is a commit.
		if json.Unmarshal(resp.Body, &commit) != nil || commit.SHA != run {
			return fmt.Errorf("commit runs: the lookup answered another commit: %w", publish.ErrScanRefused)
		}
	}

	return nil
}

// hexRuns returns the maximal runs of exactly 40 hex digits of s, in lower
// case.
func hexRuns(s string) []string {
	var out []string

	for i := 0; i < len(s); {
		if !isHexByte(s[i]) {
			i++

			continue
		}

		j := i
		for j < len(s) && isHexByte(s[j]) {
			j++
		}

		if j-i == commitRunLength {
			out = append(out, strings.ToLower(s[i:j]))
		}

		i = j
	}

	return out
}

// isHexByte reports whether b is a hex digit, either case.
func isHexByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
