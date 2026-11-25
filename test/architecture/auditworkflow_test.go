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

// auditInstalledAge is age as each job's install step puts it in place: the
// binary that step downloaded, checked against its SHA-256 pin, installed under
// the job's own bin directory in $RUNNER_TEMP, and that directory appended to
// $GITHUB_PATH.
const auditInstalledAge = "${RUNNER_TEMP}/bin/age"

// auditEncryptStep names one of the workflow's two encrypt steps and the public
// key's variable that step encrypts to.
type auditEncryptStep struct {
	name      string
	job       string
	head      string
	recipient string
}

// auditEncryptSteps are both steps that turn a result into a ciphertext another
// job decrypts: the checks job's, to the model key, and the model job's, to the
// publish key. The model job's matters most: it runs right after the audit, in
// the one job the untrusted model's own session has run in.
var auditEncryptSteps = []auditEncryptStep{
	{
		name:      "checks-results",
		job:       "checks",
		head:      `      - name: Encrypt the checks' results to the model key`,
		recipient: `${AUDIT_MODEL_PUBLIC_KEY}`,
	},
	{
		name:      "model-result",
		job:       "model",
		head:      `      - name: Encrypt the audit's result to the publish key`,
		recipient: `${AUDIT_PUBLISH_PUBLIC_KEY}`,
	},
}

// TestScheduledSecurityAuditWorkflowEncryptsWithTheInstalledAge pins that each of
// those two steps runs the installed age by its absolute path, not the bare word
// age, which the step's shell resolves through PATH: the job's install step
// appends its bin directory to $GITHUB_PATH, so a bare age is whatever the first
// age on the PATH is, and the ciphertext alone leaves the job - a binary that is
// not the one checked against the pin, or a PATH another step wrote, encrypts
// what the next job is to treat as the result (ADR 0040, issues #615, #614). The
// model job's install step runs before the model session, so a session that
// appended to $GITHUB_PATH would pick the age its own encrypt step runs.
func TestScheduledSecurityAuditWorkflowEncryptsWithTheInstalledAge(t *testing.T) {
	t.Parallel()

	lines := strings.Split(string(readRepositoryFile(t, auditWorkflow)), "\n")

	for _, tc := range auditEncryptSteps {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			start := auditStepStart(lines, tc.head)
			if start < 0 {
				t.Fatalf("%s holds no %q step: the %s job's encrypt step is the one this case reads", auditWorkflow, strings.TrimSpace(tc.head), tc.job)
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
				t.Fatalf("%s's %s job holds %d lines that encrypt in its encrypt step (%q), want the one age command that encrypts the result", auditWorkflow, tc.job, len(encrypts), encrypts)
			}

			// The command word: the quoted absolute path of the installed age,
			// or a literal absolute path. A bare name is resolved through PATH,
			// which $GITHUB_PATH fills, and is refused; so is a variable the step
			// does not set, which would expand to an empty or inherited value.
			word := strings.Trim(strings.Fields(encrypts[0])[0], `"'`)

			if word != auditInstalledAge && !strings.HasPrefix(word, "/") {
				t.Errorf("%s's %s job encrypts with %q, whose command word is %q: want age by its absolute path, %q, the binary the install step downloaded and checked against its pin, never a bare age or an unset variable the step's shell resolves through PATH (ADR 0040, issues #615, #614)",
					auditWorkflow, tc.job, encrypts[0], word, auditInstalledAge)
			}

			want := `--encrypt --recipient "` + tc.recipient + `"`

			if !strings.Contains(encrypts[0], want) {
				t.Errorf("%s's %s job encrypts with %q, want it to encrypt to the key its variable holds, %s", auditWorkflow, tc.job, encrypts[0], want)
			}
		})
	}
}

// auditStepStart returns the index of the step whose name line is head, or -1.
func auditStepStart(lines []string, head string) int {
	for i, line := range lines {
		if strings.TrimRight(line, " \t\r") == head {
			return i
		}
	}

	return -1
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
