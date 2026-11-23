package github

import "errors"

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
)
