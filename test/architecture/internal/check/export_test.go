package check

import "slices"

// AuditWhyIDs returns the why ids a not-run.txt line of the checks job may end
// in, for the test that holds the list to ci/auditpublish/internal/publish/
// vocabulary.go's whyNotRun in both directions (review round L of PR 5c, K7):
// the list is unexported because the check alone reads it, and a test that is no
// part of the module's build is the only caller it has. It returns a copy, so a
// caller that sorts it (review round M of PR 5c) moves no slice the check reads.
func AuditWhyIDs() []string {
	return slices.Clone(auditWhyIDs)
}
