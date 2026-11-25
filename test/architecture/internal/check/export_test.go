package check

// AuditWhyIDs returns the why ids a not-run.txt line of the checks job may end
// in, for the test that holds the list to ci/auditpublish/internal/publish/
// vocabulary.go's whyNotRun in both directions (review round L of PR 5c, K7):
// the list is unexported because the check alone reads it, and a test that is no
// part of the module's build is the only caller it has.
func AuditWhyIDs() []string {
	return auditWhyIDs
}
