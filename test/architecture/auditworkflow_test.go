package architecture_test

import (
	"strings"
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

// auditInstalledAge is age as the checks job's row 2 installs it: the binary it
// downloaded, checked against its SHA-256 pin, and installed under the job's
// own bin directory in $RUNNER_TEMP.
const auditInstalledAge = "${RUNNER_TEMP}/bin/age"

// TestScheduledSecurityAuditWorkflowEncryptsWithTheInstalledAge pins that the
// checks job's encrypt step (row 16) runs that binary by its absolute path, not
// the bare word age, which the step's shell resolves through PATH: row 2 appends
// its bin directory to $GITHUB_PATH, so a bare age is whatever the first age on
// the PATH is, and the ciphertext alone leaves the job - a binary that is not the
// one checked against the pin, or a PATH another step wrote, encrypts what the
// model job is to treat as the checks' results (ADR 0040, issues #615, #614).
func TestScheduledSecurityAuditWorkflowEncryptsWithTheInstalledAge(t *testing.T) {
	t.Parallel()

	lines := strings.Split(string(readRepositoryFile(t, auditWorkflow)), "\n")

	const head = `      - name: Encrypt the checks' results to the model key`

	start := -1

	for i, line := range lines {
		if strings.TrimRight(line, " \t\r") == head {
			start = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s holds no %q step: the checks job's encrypt step is the one this test reads", auditWorkflow, strings.TrimSpace(head))
	}

	step := lines[start+1 : auditStepEnd(lines, start)]

	var encrypts []string

	for _, line := range step {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "--encrypt") {
			continue
		}

		encrypts = append(encrypts, trimmed)
	}

	if len(encrypts) != 1 {
		t.Fatalf("%s's encrypt step holds %d lines that encrypt (%q), want the one age command that encrypts the checks' results", auditWorkflow, len(encrypts), encrypts)
	}

	// The command word: the quoted absolute path of the installed age, or a
	// literal absolute path. A bare name is resolved through PATH, which
	// $GITHUB_PATH fills, and is what this test refuses.
	word := strings.Trim(strings.Fields(encrypts[0])[0], `"'`)

	if word != auditInstalledAge && !strings.HasPrefix(word, "/") {
		t.Errorf("%s's encrypt step runs %q, whose command word is %q: want age by its absolute path, %q, the binary row 2 installed and checked against its pin, never a bare age the step's shell resolves through PATH (ADR 0040, issues #615, #614)",
			auditWorkflow, encrypts[0], word, auditInstalledAge)
	}

	if !strings.Contains(encrypts[0], `--encrypt --recipient "${AUDIT_MODEL_PUBLIC_KEY}"`) {
		t.Errorf("%s's encrypt step runs %q, want it to encrypt to the model key's public half, --encrypt --recipient \"${AUDIT_MODEL_PUBLIC_KEY}\"", auditWorkflow, encrypts[0])
	}
}

// auditStepEnd returns the index of the first line after start that ends the
// step's block: the job's next step, or the next job.
func auditStepEnd(lines []string, start int) int {
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(lines[i], "      - ") || indent <= 2 {
			return i
		}
	}

	return len(lines)
}
