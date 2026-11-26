package github

import (
	"errors"
	"fmt"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// refuse is a refusal of class whose text is detail, marked with check: the
// run's summary line names the check's fixed words from the core's list
// (publish.Refuse), never detail. detail names a rule, never a value.
func refuse(check publish.FailedCheck, detail string, class error) error {
	return publish.Refuse(check, fmt.Errorf("%s: %w", detail, class))
}

// The library's own error classes, beside the core's (publish.ErrGitHub,
// publish.ErrConfig and the others), which it wraps where a class of the core
// fits. Like the core's, an error names a rule, never a value.
var (
	// ErrEndpointRefused is a call the endpoint table refuses: a method, an
	// endpoint or a body outside it. It is a programming error, never GitHub's
	// answer, and gh is not run.
	ErrEndpointRefused = errors.New("endpoint refused")

	// ErrArtifacts is a run's artifact list that is not exactly the one the
	// job expects at that point.
	ErrArtifacts = errors.New("artifacts unexpected")

	// ErrBranchExists is a run whose report branch,
	// security-audit/<date>-<run ID>, exists already: the branch lookup
	// answered 200. No write follows. It is no class of the core's, so its
	// outcome is "publish failed" (1), never the GitHub failure's 8.
	ErrBranchExists = errors.New("report branch exists")

	// ErrReportDiff is a report commit whose comparison with the audited
	// commit is not exactly one added file, the run's report: no ref and no
	// pull request follow. Its outcome is "publish failed" (1).
	ErrReportDiff = errors.New("report commit diff refused")
)
