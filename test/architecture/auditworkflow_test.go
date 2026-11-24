package architecture_test

import (
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// TestScheduledSecurityAuditWorkflow holds .github/workflows/
// scheduled-security-audit.yml to check.AuditWorkflow (PRD #175 milestone 2, ADR
// 0040): one workflow the maintainer starts by hand on main, whose three jobs
// pass two ciphertexts between them and read each secret in the one place its
// work is done. The check reads the frame (the guard condition, the concurrency
// group, permissions, the environment and the runner), the hand-offs (age, the
// two artifacts and their retention), the exact reach of every secret and the
// variable, the App's token and its permission-* inputs, the model job's
// freedom from checks and containers, the hardening step before the sandbox
// preflight, the attestation check before the image digest is trusted, and the
// pins of every action and tool. The accepted shape is the check package's
// testdata/auditworkflow/valid.yml, every case of its TestAuditWorkflow a
// mutation of it.
func TestScheduledSecurityAuditWorkflow(t *testing.T) {
	t.Parallel()

	for _, message := range check.AuditWorkflow(readRepositoryFile(t, auditWorkflow)) {
		t.Error(message)
	}
}
