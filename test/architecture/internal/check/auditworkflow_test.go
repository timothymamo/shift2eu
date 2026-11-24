package check_test

import (
	"os"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// The scheduled security audit's workflow (PRD #175 milestone 2, ADR 0040), and
// what check.AuditWorkflow reads it for.
//
// The accepted shape is testdata/auditworkflow/valid.yml, the workflow PR 5b
// adds as .github/workflows/scheduled-security-audit.yml: three jobs, two
// ciphertexts, and no secret where it does not belong. AuditWorkflow returns
// one message per problem and none for that shape. It reads the document with
// the strict line reader (test/internal/yamlread), as check.ReleasePleaseWorkflow
// and check.ReleaseWorkflow do, and refuses what it does not follow rather than
// guess at it. What it must hold:
//
//   - on: is a plain block whose only trigger is workflow_dispatch:, with no
//     inputs:. No schedule (milestone 3 adds it), no push, pull_request,
//     pull_request_target, workflow_run or issue_comment.
//   - The one guard condition, textually identical in every job's if: and in
//     the concurrency group:
//     github.ref == 'refs/heads/main' && github.run_attempt == 1 &&
//     (github.event_name == 'schedule' || github.triggering_actor ==
//     github.repository_owner). The group falls through to
//     format('skipped-{0}', github.run_id) for a run whose jobs will be
//     skipped, and cancel-in-progress is false.
//   - permissions: {} at the top level. Each job's scopes are exactly the ones
//     the accepted shape declares, never a write scope and never packages:.
//   - Each job runs in the security-audit environment on ubuntu-24.04, with a
//     timeout-minutes below 360 and every step's timeout-minutes set. The model
//     job needs checks, the publish job needs model.
//   - Every checkout is of ${{ github.sha }} with persist-credentials: false and
//     no sparse-checkout.
//   - Every action is pinned to a 40-hex commit with a version comment, except
//     the App token step's action, which is pinned the same way.
//   - No job declares outputs:, no step uses actions/cache, no script runs
//     set -x, and no document holds secrets[, toJSON(secrets) or
//     secrets: inherit.
//   - No ${{ }} reaches a run: body: values reach the shell through env: and
//     files alone.
//   - Each secret is read in exactly one place: CLAUDE_CODE_OAUTH_TOKEN in the
//     model step's step-level env:, AUDIT_MODEL_PRIVATE_KEY in the model job's
//     decrypt step's, AUDIT_PUBLISH_PRIVATE_KEY in the publish job's decrypt
//     step's, AUDIT_APP_PRIVATE_KEY in the publish job's App token step's
//     with:, and AUDIT_FINGERPRINT_KEY in the publish job's step-level env:.
//     The variables AUDIT_MODEL_PUBLIC_KEY, AUDIT_PUBLISH_PUBLIC_KEY and
//     AUDIT_APP_CLIENT_ID are read where the accepted shape reads them. The
//     checks job reads no secret at all, and github.token only in the
//     step-level env: of a step that reads GitHub's API, never in the model
//     job. The model step's env: holds no other secret and no GITHUB_TOKEN.
//   - The artifact hand-offs: the checks job's last steps encrypt its results
//     to vars.AUDIT_MODEL_PUBLIC_KEY and upload checks-result; the model job's
//     last steps encrypt its result to vars.AUDIT_PUBLISH_PUBLIC_KEY and upload
//     model-result. Both carry retention-days: 1, and nothing else is uploaded.
//     Each downloading job runs ci/auditpublish artifacts before it downloads,
//     and fails closed on a name it does not expect or one that appears twice.
//   - The model job holds no check and runs no container: no step runs
//     gitleaks, govulncheck, osv-scanner, zizmor, actionlint, cosign, trivy or
//     scorecard, and none pulls or runs a docker image. The model-free step that
//     stops Docker and removes the runner user's sudo rule runs before the step
//     that starts ci/auditrun, which runs the sandbox preflight and the audit.
//     The model job reads the Claude Code pin file into the outputs its install
//     step's env: names, and that step is required (test/lint's install check,
//     issue #277).
//   - Every Go command after the step that runs go mod download carries
//     GOFLAGS=-mod=readonly GOPROXY=off.
//   - The generate GitHub App token step is in the publish job alone, pinned to
//     a commit, with exactly the four permission-* inputs the accepted shape
//     holds. It runs after the checks job and before the publish step.
//   - The publish job verifies the image's provenance attestation with
//     gh attestation verify, its --signer-workflow release.yml and its
//     --source-ref refs/tags/<tag>, before the step that trusts the digest.
//     gitleaks runs the stdin subcommand with --ignore-gitleaks-allow.
//   - No private key is written under /tmp or /run.
//   - Every tool version line has a # renovate: comment above it, and every
//     downloaded tool's SHA-256 line is 64 lower-case hex with a
//     digestVersion= comment of its own.
//   - A document the reader cannot follow, or an empty one, is refused.
//
// The check recognises each job's steps by name, and the accepted fixture's
// names are the closed list: a step the check's list does not hold is reported
// with its name, as is a step that is missing or out of order. Every message
// names the key, value, step, secret or artifact it is about: the cases below
// assert a part of that name.
//
// TestAuditWorkflow holds that none of that can change unseen: every case is a
// mutation of the accepted shape, and each must draw a message the accepted
// shape does not.

// auditWorkflowFixture is the accepted shape of the scheduled security audit's
// workflow, the file the repository test holds .github/workflows/
// scheduled-security-audit.yml to.
const auditWorkflowFixture = "testdata/auditworkflow/valid.yml"

// auditGuard is the one guard condition: main only, a run's first attempt only,
// the schedule or the owner.
const auditGuard = "github.ref == 'refs/heads/main' && github.run_attempt == 1 && (github.event_name == 'schedule' || github.triggering_actor == github.repository_owner)"

// The texts the cases below replace, each exactly as the accepted fixture
// writes it: a case that mistypes one fails in mutate, not as a case that
// passes for the wrong reason.
const (
	auditConcurrencyGroup = "  group: ${{ " + auditGuard + " && 'scheduled-security-audit' || format('skipped-{0}', github.run_id) }}\n"
	auditCancelProgress   = "  cancel-in-progress: false\n"
	auditDispatchTrigger  = "  workflow_dispatch:\n"
	auditCheckoutPin      = "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1"
	auditCheckoutUses     = "        uses: " + auditCheckoutPin + "\n"
	auditCheckoutRef      = "          ref: ${{ github.sha }}\n"
	auditNoCredentials    = "          persist-credentials: false\n"
	auditArtifactUpload   = "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2"
	auditChecksIf         = "    timeout-minutes: 180\n    if: ${{ " + auditGuard + " }}\n"
	auditModelIf          = "    timeout-minutes: 120\n    if: ${{ " + auditGuard + " }}\n"
	auditPublishIf        = "    timeout-minutes: 30\n    if: ${{ " + auditGuard + " }}\n"
	// auditChecksPermissions is the checks job's permissions block, whose comment
	// no other job shares. No attestations scope: no checks-job row reads one in
	// 5b.
	auditChecksPermissions = "    # GITHUB_TOKEN reads only: contents for the checkout and the release tag.\n" +
		"    # The image is public, so no packages scope, and nothing here writes.\n" +
		"    permissions:\n" +
		"      contents: read\n"
	// auditPublishPermissions is the publish job's permissions block, whose
	// comment no other job shares.
	auditPublishPermissions = "    # artifacts, attestations for the image check. The audit App's token writes.\n" +
		"    permissions:\n" +
		"      contents: read\n" +
		"      actions: read\n" +
		"      attestations: read\n"
	// auditModelPermissions is the model job's permissions block: what it reads
	// is exactly the checkout and the artifact listing.
	auditModelPermissions = "    # GITHUB_TOKEN reads only: contents for the checkout, actions for the\n" +
		"    # artifact listing and the download. No secret is in a job's env:.\n" +
		"    permissions:\n" +
		"      contents: read\n" +
		"      actions: read\n"
	// auditRecordStep is the checks job's first Go and API step, which the cases
	// that need a script anchor on.
	auditRecordStep = "      - name: Record the audited commit and the latest release tag\n" +
		"        timeout-minutes: 5\n"
	// The tools step's gitleaks pin, with its Renovate comment and the human
	// comment above it, and the publish job's copy of the same pin: every copy
	// of a version and digest is equal, so the constants differ only in the
	// context that makes each unique.
	auditChecksGitleaks = "          # Renovate bumps a version and its digest in one PR (CLAUDE.md rule 7).\n" +
		"          # renovate: datasource=github-releases depName=gitleaks/gitleaks\n" +
		"          GITLEAKS_VERSION: v8.30.0\n" +
		"          # renovate: datasource=github-release-attachments depName=gitleaks/gitleaks digestVersion=v8.30.0\n" +
		"          GITLEAKS_SHA256: \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n"
	auditPublishGitleaks = "          # renovate: datasource=github-releases depName=gitleaks/gitleaks\n" +
		"          GITLEAKS_VERSION: v8.30.0\n" +
		"          # renovate: datasource=github-release-attachments depName=gitleaks/gitleaks digestVersion=v8.30.0\n" +
		"          GITLEAKS_SHA256: \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n" +
		"        run: |\n"
	// auditModelKeyVar is the checks job's encryption step, the one place the
	// model key's public half is read, and the anchor the cases that put another
	// name in a checks-job env: use.
	auditModelKeyVar = "          AUDIT_MODEL_PUBLIC_KEY: ${{ vars.AUDIT_MODEL_PUBLIC_KEY }}\n"
	// auditActionlint is the one later Go command, with the flags every Go
	// command after the module download must carry.
	auditActionlint = "GOFLAGS=-mod=readonly GOPROXY=off go -C tools run github.com/rhysd/actionlint/cmd/actionlint ../.github/workflows\n"
	// auditWrapperBuild is the model job's own Go command, the wrapper's build.
	auditWrapperBuild = "GOFLAGS=-mod=readonly GOPROXY=off go build -trimpath -o \"${RUNNER_TEMP}/audit-exec\" ./ci/auditexec\n"
	// auditHardeningStep is the model-free step that hardens the runner: it must
	// run before the sandbox preflight, which is inside the model step.
	auditHardeningStep = "      - name: Stop Docker and remove the runner user's sudo rule\n" +
		"        timeout-minutes: 5\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          sudo systemctl stop docker.service docker.socket\n" +
		"          sudo systemctl disable docker.service docker.socket\n" +
		"          sudo rm -f /etc/sudoers.d/runner\n" +
		"          if sudo -n true 2> /dev/null; then\n" +
		"            echo \"::error title=Sandbox::sudo still works: see runbook step 10\"\n" +
		"            exit 1\n" +
		"          fi\n"
	// auditModelCanary is the model step's env:, whose canary line the cases that
	// widen it append to.
	auditModelCanary = "          SHIFT2EU_AUDIT_CANARY: \"8f14e45fceea167a5a36dedd4bea2543f0e1a2b3c4d5e6f708192a3b4c5d6e7f\"\n"
	// auditDigestComment is the note above auditDigestRefusal in the accepted
	// shape's attestation step.
	auditDigestComment = "          # The digest is read as a string alone, and refused unless it is exactly the empty value (the run\n" +
		"          # pulled no image) or one sha256 digest: any other value, a newline in it included, would reach\n" +
		"          # the empty branch below as a not-pulled image otherwise. The step stands alone: it trusts no\n" +
		"          # earlier step to have read the digest.\n"
	// auditDigestRefusal is the attestation step's read of the digest as a string
	// alone, and its refusal of every value that is neither exactly the empty
	// value nor one sha256 digest, before the empty branch can take such a value
	// for a not-pulled image (review round 2, G3). The message a check owes for
	// the step that drops it names the jq that holds it, and so holds
	// select(type == "string").
	auditDigestRefusal = "          if ! digest=\"$(jq -er '.image_digest | select(type == \"string\") | select(. == \"\" or test(\"^sha256:[0-9a-f]{64}\\\\z\"))' \"${RUNNER_TEMP}/result.json\")\"; then\n" +
		"            echo \"::error title=Audit publish::the image digest is no string, no empty value and no sha256 digest: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	// auditDigestRead is the unchecked read the attestation step held before
	// review round 2's G3, and the step the case that removes the refusal puts
	// in its place: any value, a newline in it included, becomes the digest then.
	auditDigestRead = "          digest=\"$(jq -er '.image_digest' \"${RUNNER_TEMP}/result.json\")\"\n"
	// auditTagShapeCheck is the attestation step's tag-shape check, and
	// auditTagShapeCommented a copy of it under comment lines, which no shell
	// runs: the case that moves the check below the verification leaves the
	// copy where the check stood.
	auditTagShapeCheck = "          if [[ ! \"${tag}\" =~ ^v[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then\n" +
		"            echo \"::error title=Audit publish::the tag's shape is no vMAJOR.MINOR.PATCH: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	auditTagShapeCommented = "          # if [[ ! \"${tag}\" =~ ^v[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then\n" +
		"          #   echo \"::error title=Audit publish::the tag's shape is no vMAJOR.MINOR.PATCH: see runbook step 11\"\n" +
		"          #   exit 1\n" +
		"          # fi\n"
	// auditZipDigestLine is the model job's download step's check of the zip
	// against the digest of artifact.json, the step the case that comments it out
	// reads no longer.
	auditZipDigestLine = "          echo \"${digest#sha256:}  ${RUNNER_TEMP}/checks-result.zip\" | sha256sum --check --strict -\n"
	// auditChecksArtifactDownloadLine is the line right before it, the code line
	// the case that hides the zip's sha256sum in a trailing comment hangs it on:
	// the comment then stands where the check stood, so the only difference the
	// case makes is the # that hides it.
	auditChecksArtifactDownloadLine = "          gh api \"repos/${GITHUB_REPOSITORY}/actions/artifacts/${id}/zip\" > \"${RUNNER_TEMP}/checks-result.zip\"\n"
	// auditVerifyFlagLine is the last line of the attestation step's gh
	// verification, whose --deny-self-hosted-runners the case that comments it
	// out leaves in a comment alone.
	auditVerifyFlagLine = "            --deny-self-hosted-runners\n"
	// auditAttestationStep is the publish job's model-free attestation check: it
	// stands alone, reading the digest as a string alone and refusing anything
	// but exactly the empty value or one sha256 digest before the empty branch
	// (review round 2, G3), the tag's shape is held first, an empty digest skips
	// the verification with a fixed notice (the run pulled no image), and a
	// digest that is not sha256 is refused before gh runs.
	auditAttestationStep = "      - name: Verify the image's provenance attestation\n" +
		"        timeout-minutes: 10\n" +
		"        env:\n" +
		"          GH_TOKEN: ${{ github.token }}\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          image=\"ghcr.io/${GITHUB_REPOSITORY_OWNER,,}/shift2eu\"\n" +
		auditDigestComment +
		auditDigestRefusal +
		"          tag=\"$(jq -er '.release_tag' \"${RUNNER_TEMP}/result.json\")\"\n" +
		"          if [[ ! \"${tag}\" =~ ^v[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then\n" +
		"            echo \"::error title=Audit publish::the tag's shape is no vMAJOR.MINOR.PATCH: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n" +
		"          if [ -z \"${digest}\" ]; then\n" +
		"            echo \"image not pulled: no provenance attestation to verify\"\n" +
		"            exit 0\n" +
		"          fi\n" +
		"          if [[ ! \"${digest}\" =~ ^sha256:[0-9a-f]{64}$ ]]; then\n" +
		"            echo \"::error title=Audit publish::the image digest is no sha256 digest: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n" +
		"          gh attestation verify \"oci://${image}@${digest}\" \\\n" +
		"            --repo \"${GITHUB_REPOSITORY}\" \\\n" +
		"            --signer-workflow \"${GITHUB_REPOSITORY}/.github/workflows/release.yml\" \\\n" +
		"            --source-ref \"refs/tags/${tag}\" \\\n" +
		"            --deny-self-hosted-runners\n"
	// auditAppTokenStep is the publish job's App token step.
	auditAppTokenStep = "      - name: Mint the audit App's token\n" +
		"        timeout-minutes: 5\n" +
		"        id: app\n" +
		"        uses: actions/create-github-app-token@f418cff399f7aef4c2f0ad47e25ca7a6abbdd6fc # v3.4.0\n" +
		"        with:\n" +
		"          client-id: ${{ vars.AUDIT_APP_CLIENT_ID }}\n" +
		"          # The App's key, read from the environment alone, and narrowed here to\n" +
		"          # what ADR 0040's Decision 1 lists.\n" +
		"          private-key: ${{ secrets.AUDIT_APP_PRIVATE_KEY }}\n" +
		"          permission-contents: write\n" +
		"          permission-pull-requests: write\n" +
		"          permission-issues: write\n" +
		"          permission-security-events: write\n"
	// auditGitleaksStdin is the publish job's second guard: gitleaks over the text
	// it is about to send, through the stdin subcommand.
	auditGitleaksStdin = "          gitleaks stdin --ignore-gitleaks-allow --redact --no-banner < \"${RUNNER_TEMP}/result.json\"\n"
	// auditGitleaksScanStep is that guard's whole step, which runs before the App
	// token is minted: a refused result has no write token minted for it.
	auditGitleaksScanStep = "      - name: Scan the text it is about to publish\n" +
		"        timeout-minutes: 10\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		auditGitleaksStdin
	// auditZizmorStep and auditActionlintStep are the checks job's two lint
	// steps: only zizmor's online audits read GitHub's API, so only it holds the
	// job's token.
	auditZizmorStep = "      - name: zizmor\n" +
		"        timeout-minutes: 10\n" +
		"        env:\n" +
		"          # The one read this row makes of GitHub's API.\n" +
		"          GH_TOKEN: ${{ github.token }}\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          zizmor .github/workflows\n"
	auditActionlintStep = "      - name: actionlint\n" +
		"        timeout-minutes: 10\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          " + auditActionlint
	// auditZizmorActionlintStep is the single step the two above replace: the
	// shape a case puts back to show that the split is held.
	auditZizmorActionlintStep = "      - name: zizmor and actionlint\n" +
		"        timeout-minutes: 15\n" +
		"        env:\n" +
		"          # The one read this row makes of GitHub's API.\n" +
		"          GH_TOKEN: ${{ github.token }}\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          zizmor .github/workflows\n" +
		"          " + auditActionlint
	// auditRunCommand is the model step's own command, the one that writes the
	// checked result under $RUNNER_TEMP.
	auditRunCommand = "          ci/auditrun -claude-tarball \"${RUNNER_TEMP}/claude-code/claude-code.tgz\" -claude-integrity \"${CLAUDE_CODE_PIN_INTEGRITY}\" -result-out \"${RUNNER_TEMP}/result.json\"\n"
	// auditEncryptResultCommand is the encrypt step's age command: it reads the
	// file the launcher wrote, never the result's path under /srv/audit.
	auditEncryptResultCommand = "          age --encrypt --recipient \"${AUDIT_PUBLISH_PUBLIC_KEY}\" --output \"${RUNNER_TEMP}/model-result.age\" \"${RUNNER_TEMP}/result.json\"\n"
	// auditModelResultHead is the model job's encryption step, the step that
	// runs right after the model step: the head the case that moves the hardening
	// step past the model step inserts it at.
	auditModelResultHead = "      - name: Encrypt the audit's result to the publish key\n"
	// auditClaudeCodePinStep is the model job's step that reads the Claude Code
	// pin file into the outputs the install step's env: reads: test/lint's
	// install check requires it wherever the CLI is installed (issue #277,
	// round J), so the audit's workflow holds it too.
	auditClaudeCodePinStep = "      - name: Read the Claude Code pin\n" +
		"        id: claude-code-pin\n" +
		"        timeout-minutes: 5\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          pin=.github/claude-code.yaml\n" +
		"          version=\"$(grep -E '^version: [0-9]+\\.[0-9]+\\.[0-9]+$' \"${pin}\" || true)\"\n" +
		"          integrity=\"$(grep -E '^integrity: sha512-[A-Za-z0-9+/]{86}==$' \"${pin}\" || true)\"\n" +
		"          if [ \"$(grep -c '^version:' \"${pin}\")\" != 1 ] || [ \"$(grep -c '^integrity:' \"${pin}\")\" != 1 ] ||\n" +
		"            [ -z \"${version}\" ] || [ -z \"${integrity}\" ]; then\n" +
		"            echo \"::error title=Claude Code pin::${pin} holds no single exact version: line and sha512 integrity: line\"\n" +
		"            exit 1\n" +
		"          fi\n" +
		"          {\n" +
		"            printf 'version=%s\\n' \"${version#version: }\"\n" +
		"            printf 'integrity=%s\\n' \"${integrity#integrity: }\"\n" +
		"          } >> \"${GITHUB_OUTPUT}\"\n"
	// auditPublishCommand is the publish step's one line, the end of the job the
	// case that moves the attestation check past it appends to.
	auditPublishCommand = "          ci/auditpublish publish -result \"${RUNNER_TEMP}/result.json\" -reports docs/security/audits -gh \"$(command -v gh)\" -gitleaks \"$(command -v gitleaks)\" -tmp \"${RUNNER_TEMP}\"\n"
)

// TestAuditWorkflowAcceptsTheAcceptedShape pins that the accepted fixture draws
// no message: every case of TestAuditWorkflow is a mutation of it, and a check
// that refuses it would refuse the workflow itself.
func TestAuditWorkflowAcceptsTheAcceptedShape(t *testing.T) {
	t.Parallel()

	if got := check.AuditWorkflow(auditWorkflowFixtureBytes(t)); len(got) != 0 {
		t.Errorf("AuditWorkflow(%s) = %q, want no message: the fixture is the accepted shape of the scheduled audit's workflow", auditWorkflowFixture, got)
	}
}

// auditWorkflowFixtureBytes returns the accepted shape's bytes.
func auditWorkflowFixtureBytes(t *testing.T) []byte {
	t.Helper()

	src, err := os.ReadFile(auditWorkflowFixture)
	if err != nil {
		t.Fatalf("read %s: %v; the accepted shape of the audit workflow is the file every case mutates", auditWorkflowFixture, err)
	}

	return src
}

// TestAuditWorkflow holds the scheduled security audit's workflow to
// check.AuditWorkflow: the frame, the two ciphertexts and the reach of every
// secret. Each case changes one part of the accepted shape and must draw a
// message the accepted shape does not, whose text names the part the case
// changes (wantIn). A case that draws no new message, or one that does not name
// what it broke, fails.
func TestAuditWorkflow(t *testing.T) {
	t.Parallel()

	acceptedWorkflow := string(auditWorkflowFixtureBytes(t))
	accepted := check.AuditWorkflow([]byte(acceptedWorkflow))

	if len(accepted) != 0 {
		t.Fatalf("AuditWorkflow(%s) = %q, want no message: the fixture is the accepted shape", auditWorkflowFixture, accepted)
	}

	// withStep returns the accepted workflow with step inserted at the head of
	// the job's steps, named head.
	withStep := func(head, step string) string {
		return mutate(t, acceptedWorkflow, head, step+head)
	}

	// withoutStep returns the accepted workflow with step removed.
	withoutStep := func(step string) string {
		return mutate(t, acceptedWorkflow, step, "")
	}

	// moveStep returns the accepted workflow with step taken out of its place and
	// put back before head: withStep(head, withoutStep(step)) is no move, since
	// withStep always mutates the accepted workflow and so pastes a second copy of
	// the whole document in place of head.
	moveStep := func(head, step string) string {
		return mutate(t, withoutStep(step), head, step+head)
	}

	// The model job's upload step, whose head the cases that add a step to the
	// model job anchor on.
	const auditModelUpload = "      - name: Upload the model's ciphertext\n"

	tests := []struct {
		name     string
		workflow string
		// wantIn is a part of a message the check must return for the case, and
		// the accepted shape does not draw; "" means no message at all.
		wantIn string
	}{
		// The accepted shape, and the reading that must not depend on the line
		// ends.
		{name: "the accepted shape", workflow: acceptedWorkflow},
		{name: "CRLF line ends", workflow: strings.ReplaceAll(acceptedWorkflow, "\n", "\r\n")},

		// The triggers: a workflow_dispatch with nothing to fill in, and no other
		// event. Any other trigger lets a run start where no audit belongs, and an
		// input is a value a caller chooses.
		{
			name:     "a push trigger",
			workflow: mutate(t, acceptedWorkflow, auditDispatchTrigger, auditDispatchTrigger+"  push:\n"),
			wantIn:   "push",
		},
		{
			name:     "a pull_request trigger",
			workflow: mutate(t, acceptedWorkflow, auditDispatchTrigger, auditDispatchTrigger+"  pull_request:\n"),
			wantIn:   "pull_request",
		},
		{
			name:     "a pull_request_target trigger",
			workflow: mutate(t, acceptedWorkflow, auditDispatchTrigger, auditDispatchTrigger+"  pull_request_target:\n"),
			wantIn:   "pull_request_target",
		},
		{
			name:     "a workflow_run trigger",
			workflow: mutate(t, acceptedWorkflow, auditDispatchTrigger, auditDispatchTrigger+"  workflow_run:\n"),
			wantIn:   "workflow_run",
		},
		{
			name:     "an issue_comment trigger",
			workflow: mutate(t, acceptedWorkflow, auditDispatchTrigger, auditDispatchTrigger+"  issue_comment:\n"),
			wantIn:   "issue_comment",
		},
		{
			// The schedule is milestone 3's: until then a cron trigger runs the
			// audit before the checks job is complete, and nothing reports that.
			name:     "a schedule trigger",
			workflow: mutate(t, acceptedWorkflow, auditDispatchTrigger, auditDispatchTrigger+"  schedule:\n    - cron: \"0 3 * * 1\"\n"),
			wantIn:   "schedule",
		},
		{
			name: "inputs on the dispatch",
			workflow: mutate(t, acceptedWorkflow, auditDispatchTrigger,
				auditDispatchTrigger+"    inputs:\n      force:\n        description: Accept whatever is there\n"),
			wantIn: "inputs",
		},

		// One guard condition, textually identical in every job's if: and in the
		// concurrency group: main only, a run's first attempt only, the schedule or
		// the owner. A weaker guard in one place opens exactly that path.
		{
			name:     "another guard in the checks job",
			workflow: mutate(t, acceptedWorkflow, auditChecksIf, "    timeout-minutes: 180\n    if: ${{ always() }}\n"),
			wantIn:   "guard",
		},
		{
			name:     "another guard in the model job",
			workflow: mutate(t, acceptedWorkflow, auditModelIf, "    timeout-minutes: 120\n    if: ${{ github.ref == 'refs/heads/main' }}\n"),
			wantIn:   "guard",
		},
		{
			name:     "another guard in the publish job",
			workflow: mutate(t, acceptedWorkflow, auditPublishIf, "    timeout-minutes: 30\n    if: ${{ github.ref == 'refs/heads/main' && github.run_attempt == 1 }}\n"),
			wantIn:   "guard",
		},
		{
			name:     "no guard in the publish job",
			workflow: mutate(t, acceptedWorkflow, auditPublishIf, "    timeout-minutes: 30\n"),
			wantIn:   "guard",
		},
		{
			name:     "another guard in the concurrency group",
			workflow: mutate(t, acceptedWorkflow, auditConcurrencyGroup, "  group: ${{ github.ref == 'refs/heads/main' && 'scheduled-security-audit' || format('skipped-{0}', github.run_id) }}\n"),
			wantIn:   "group",
		},
		{
			// A run the guard skips must neither queue behind nor cancel a real
			// one, so its group is its own run's.
			name:     "a group with no skipped fallthrough",
			workflow: mutate(t, acceptedWorkflow, "format('skipped-{0}', github.run_id)", "github.run_id"),
			wantIn:   "skipped-",
		},
		{
			name:     "a concurrency that cancels the run in progress",
			workflow: mutate(t, acceptedWorkflow, auditCancelProgress, "  cancel-in-progress: true\n"),
			wantIn:   "cancel-in-progress",
		},
		{
			name:     "no cancel-in-progress",
			workflow: mutate(t, acceptedWorkflow, auditCancelProgress, ""),
			wantIn:   "cancel-in-progress",
		},

		// GITHUB_TOKEN: nothing at the top level, and only read scopes in a job.
		// The image is public, so no job needs packages.
		{
			name:     "a top-level permissions with a scope",
			workflow: mutate(t, acceptedWorkflow, "permissions: {}\n", "permissions:\n  contents: read\n"),
			wantIn:   "permissions",
		},
		{
			name:     "contents: write in every job",
			workflow: mutateEvery(t, acceptedWorkflow, "      contents: read\n", "      contents: write\n"),
			wantIn:   "write",
		},
		{
			name:     "an extra scope in the checks job",
			workflow: mutate(t, acceptedWorkflow, auditChecksPermissions, auditChecksPermissions+"      packages: read\n"),
			wantIn:   "packages",
		},
		{
			name:     "an extra scope in the model job",
			workflow: mutate(t, acceptedWorkflow, auditModelPermissions, auditModelPermissions+"      issues: read\n"),
			wantIn:   "issues",
		},
		{
			name:     "an extra scope in the publish job",
			workflow: mutate(t, acceptedWorkflow, auditPublishPermissions, auditPublishPermissions+"      security-events: read\n"),
			wantIn:   "security-events",
		},

		// The environment and the runner are the ones ADR 0040 recorded, for every
		// job: a secret is only limited to main by the environment, and a newer
		// image would change the coreutils and bubblewrap under the audit.
		{
			name:     "no environment on the model job",
			workflow: mutate(t, acceptedWorkflow, "    environment: security-audit\n"+auditModelPermissions, auditModelPermissions),
			wantIn:   "environment",
		},
		{
			name:     "another environment on the publish job",
			workflow: mutate(t, acceptedWorkflow, "    environment: security-audit\n    # GITHUB_TOKEN reads only: contents for the checkout, actions for the\n"+auditPublishPermissions, "    environment: production\n    # GITHUB_TOKEN reads only: contents for the checkout, actions for the\n"+auditPublishPermissions),
			wantIn:   "security-audit",
		},
		{
			name:     "a moving runner image",
			workflow: mutateEvery(t, acceptedWorkflow, "    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-latest\n"),
			wantIn:   "ubuntu-24.04",
		},

		// The jobs run in order: the model job reads the checks' results, the
		// publish job the model's.
		{
			name:     "no needs on the model job",
			workflow: mutate(t, acceptedWorkflow, "    needs: checks\n", ""),
			wantIn:   "needs",
		},
		{
			name:     "no needs on the publish job",
			workflow: mutate(t, acceptedWorkflow, "    needs: model\n", ""),
			wantIn:   "needs",
		},

		// Every checkout is of the main commit the run started on, with no
		// credential kept and no sparse checkout: the audit's copy must be the
		// whole tree its findings quote.
		{
			name:     "a checkout without ref",
			workflow: mutateEvery(t, acceptedWorkflow, auditCheckoutRef, ""),
			wantIn:   "${{ github.sha }}",
		},
		{
			name:     "a checkout keeping the credential",
			workflow: mutateEvery(t, acceptedWorkflow, auditNoCredentials, "          persist-credentials: true\n"),
			wantIn:   "persist-credentials",
		},
		{
			name:     "a checkout without persist-credentials",
			workflow: mutateEvery(t, acceptedWorkflow, auditNoCredentials, ""),
			wantIn:   "persist-credentials",
		},
		{
			name:     "a sparse checkout",
			workflow: mutateEvery(t, acceptedWorkflow, auditNoCredentials, auditNoCredentials+"          sparse-checkout: internal\n"),
			wantIn:   "sparse-checkout",
		},

		// Every step and every job has a timeout, and no job's reaches GitHub's
		// six-hour limit: a hung check or model run ends on the workflow's own
		// terms.
		{
			name:     "a step without a timeout",
			workflow: mutate(t, acceptedWorkflow, auditRecordStep, "      - name: Record the audited commit and the latest release tag\n"),
			wantIn:   "timeout-minutes",
		},
		{
			name:     "a job without a timeout",
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 180\n", ""),
			wantIn:   "timeout-minutes",
		},
		{
			name:     "a job timeout at GitHub's limit",
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 180\n", "    timeout-minutes: 360\n"),
			wantIn:   "timeout-minutes",
		},

		// Every action is pinned to a commit, and says which release that commit
		// is: a tag moves, a commit does not.
		{
			name:     "an action pinned by tag",
			workflow: mutateEvery(t, acceptedWorkflow, auditCheckoutUses, "        uses: actions/checkout@v7.0.1\n"),
			wantIn:   "checkout@v7.0.1",
		},
		{
			name:     "an action pinned without a version comment",
			workflow: mutateEvery(t, acceptedWorkflow, auditCheckoutPin, "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"),
			wantIn:   "version comment",
		},

		// No expression reaches a script: a value from the run reaches the shell
		// through env: and files alone, so no text of it can become a command.
		{
			name: "an expression in a run body",
			workflow: mutate(t, acceptedWorkflow, "          tag=\"$(gh api \"repos/${GH_REPO}/releases/latest\" --jq .tag_name)\"\n",
				"          echo \"${{ github.event_name }}\n          tag=\"$(gh api \"repos/${GH_REPO}/releases/latest\" --jq .tag_name)\"\n"),
			wantIn: "${{",
		},

		// Every environment secret at once is one expression away, and none of the
		// three forms may appear anywhere.
		{
			name:     "a secrets lookup in the bracket form",
			workflow: mutate(t, acceptedWorkflow, auditModelKeyVar, auditModelKeyVar+"          ALL_SECRETS: ${{ secrets[format('{0}', 'AUDIT_APP_PRIVATE_KEY')] }}\n"),
			wantIn:   "secrets[",
		},
		{
			name:     "toJSON(secrets)",
			workflow: mutate(t, acceptedWorkflow, auditModelKeyVar, auditModelKeyVar+"          ALL_SECRETS: ${{ toJSON(secrets) }}\n"),
			wantIn:   "toJSON(secrets)",
		},
		{
			name:     "secrets: inherit",
			workflow: mutate(t, acceptedWorkflow, "    needs: checks\n", "    needs: checks\n    secrets: inherit\n"),
			wantIn:   "inherit",
		},

		// Each secret is read in exactly one place, and each job reads only the
		// one its work needs.
		{
			name:     "the subscription token in the model job's env",
			workflow: mutate(t, acceptedWorkflow, "    needs: checks\n", "    needs: checks\n    env:\n      CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}\n"),
			wantIn:   "CLAUDE_CODE_OAUTH_TOKEN",
		},
		{
			name:     "the subscription token at the workflow level",
			workflow: mutate(t, acceptedWorkflow, "jobs:\n", "env:\n  CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}\n\njobs:\n"),
			wantIn:   "CLAUDE_CODE_OAUTH_TOKEN",
		},
		{
			name:     "the subscription token in a with: input",
			workflow: mutate(t, acceptedWorkflow, "          name: model-result\n", "          name: model-result\n          token: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}\n"),
			wantIn:   "CLAUDE_CODE_OAUTH_TOKEN",
		},
		{
			name: "the subscription token in a run body",
			workflow: mutate(t, acceptedWorkflow, "          sudo chmod -R a-w /srv/audit/checkout\n",
				"          echo \"${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}\" > /dev/null\n          sudo chmod -R a-w /srv/audit/checkout\n"),
			wantIn: "CLAUDE_CODE_OAUTH_TOKEN",
		},
		{
			name: "the subscription token in a second step",
			workflow: withStep(auditModelUpload, "      - name: Print the token\n"+
				"        timeout-minutes: 5\n"+
				"        env:\n"+
				"          CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}\n"+
				"        run: |\n"+
				"          set -euo pipefail\n"+
				"          echo ok\n"),
			wantIn: "CLAUDE_CODE_OAUTH_TOKEN",
		},
		{
			name:     "the model key's private half outside its decrypt step",
			workflow: mutate(t, acceptedWorkflow, "      - name: Copy the checkout into the audit directory\n        timeout-minutes: 10\n", "      - name: Copy the checkout into the audit directory\n        timeout-minutes: 10\n        env:\n          AUDIT_MODEL_PRIVATE_KEY: ${{ secrets.AUDIT_MODEL_PRIVATE_KEY }}\n"),
			wantIn:   "AUDIT_MODEL_PRIVATE_KEY",
		},
		{
			name:     "the publish key's private half outside the publish job",
			workflow: mutate(t, acceptedWorkflow, "          AUDIT_PUBLISH_PUBLIC_KEY: ${{ vars.AUDIT_PUBLISH_PUBLIC_KEY }}\n", "          AUDIT_PUBLISH_PUBLIC_KEY: ${{ vars.AUDIT_PUBLISH_PUBLIC_KEY }}\n          AUDIT_PUBLISH_PRIVATE_KEY: ${{ secrets.AUDIT_PUBLISH_PRIVATE_KEY }}\n"),
			wantIn:   "AUDIT_PUBLISH_PRIVATE_KEY",
		},
		{
			name:     "the App's key outside the publish job",
			workflow: mutate(t, acceptedWorkflow, auditModelKeyVar, auditModelKeyVar+"          AUDIT_APP_PRIVATE_KEY: ${{ secrets.AUDIT_APP_PRIVATE_KEY }}\n"),
			wantIn:   "AUDIT_APP_PRIVATE_KEY",
		},
		{
			name:     "the fingerprint key in the checks job",
			workflow: mutate(t, acceptedWorkflow, auditModelKeyVar, auditModelKeyVar+"          AUDIT_FINGERPRINT_KEY: ${{ secrets.AUDIT_FINGERPRINT_KEY }}\n"),
			wantIn:   "AUDIT_FINGERPRINT_KEY",
		},
		{
			name:     "the fingerprint key in the publish job's env",
			workflow: mutate(t, acceptedWorkflow, "    needs: model\n", "    needs: model\n    env:\n      AUDIT_FINGERPRINT_KEY: ${{ secrets.AUDIT_FINGERPRINT_KEY }}\n"),
			wantIn:   "AUDIT_FINGERPRINT_KEY",
		},
		{
			name:     "a secret in the checks job",
			workflow: mutate(t, acceptedWorkflow, auditModelKeyVar, auditModelKeyVar+"          AUDIT_PUBLISH_PRIVATE_KEY: ${{ secrets.AUDIT_PUBLISH_PRIVATE_KEY }}\n"),
			wantIn:   "AUDIT_PUBLISH_PRIVATE_KEY",
		},
		{
			name:     "github.token in the checks job's env",
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 180\n", "    timeout-minutes: 180\n    env:\n      GH_TOKEN: ${{ github.token }}\n"),
			wantIn:   "github.token",
		},
		{
			name:     "GITHUB_TOKEN in the model step's env",
			workflow: mutate(t, acceptedWorkflow, auditModelCanary, auditModelCanary+"          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n"),
			wantIn:   "GITHUB_TOKEN",
		},

		// The App's token is minted in the publish job alone, from its own client
		// ID and key, narrowed to exactly the four permissions the ADR lists.
		{
			name:     "no App token step",
			workflow: withoutStep(auditAppTokenStep),
			wantIn:   "audit App's token",
		},
		{
			name:     "an App token action pinned by tag",
			workflow: mutate(t, acceptedWorkflow, "actions/create-github-app-token@f418cff399f7aef4c2f0ad47e25ca7a6abbdd6fc # v3.4.0", "actions/create-github-app-token@v3.4.0"),
			wantIn:   "create-github-app-token@v3.4.0",
		},
		{
			name:     "an extra permission input on the App token",
			workflow: mutate(t, acceptedWorkflow, "          permission-security-events: write\n", "          permission-security-events: write\n          permission-metadata: read\n"),
			wantIn:   "permission-metadata",
		},
		{
			name:     "a missing permission input on the App token",
			workflow: mutate(t, acceptedWorkflow, "          permission-issues: write\n", ""),
			wantIn:   "permission-issues",
		},
		{
			name:     "the App token step in the model job",
			workflow: moveStep(auditModelUpload, auditAppTokenStep),
			wantIn:   "audit App's token",
		},

		// Two ciphertexts cross, by exact name, with a one-day retention, and
		// nothing else crosses.
		{
			name:     "another name on the checks' artifact",
			workflow: mutate(t, acceptedWorkflow, "          name: checks-result\n", "          name: checks\n"),
			wantIn:   "checks-result",
		},
		{
			name:     "another name on the model's artifact",
			workflow: mutate(t, acceptedWorkflow, "          name: model-result\n", "          name: audit-result\n"),
			wantIn:   "model-result",
		},
		{
			name:     "another retention",
			workflow: mutateEvery(t, acceptedWorkflow, "          retention-days: 1\n", "          retention-days: 7\n"),
			wantIn:   "retention-days",
		},
		{
			name: "an extra upload in the checks job",
			workflow: mutate(t, acceptedWorkflow, "          if-no-files-found: error\n\n  model:\n",
				"          if-no-files-found: error\n"+
					"      - name: Upload the transcripts\n"+
					"        timeout-minutes: 5\n"+
					"        uses: "+auditArtifactUpload+"\n"+
					"        with:\n"+
					"          name: transcripts\n"+
					"          path: /srv/audit/state/out\n"+
					"          retention-days: 1\n"+
					"\n  model:\n"),
			wantIn: "Upload the transcripts",
		},
		{
			name:     "a job output",
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 180\n", "    timeout-minutes: 180\n    outputs:\n      tag: ${{ steps.record.outputs.tag }}\n"),
			wantIn:   "outputs",
		},
		{
			name: "an Actions cache",
			workflow: withStep("      - name: Download the pinned tools and verify their checksums\n        timeout-minutes: 15\n",
				"      - name: Cache the tools\n"+
					"        timeout-minutes: 5\n"+
					"        uses: actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830 # v4.3.0\n"+
					"        with:\n"+
					"          path: ${{ runner.temp }}/bin\n"+
					"          key: tools\n"),
			wantIn: "actions/cache",
		},
		{
			name:     "a script tracing its commands",
			workflow: mutate(t, acceptedWorkflow, "          set -euo pipefail\n          tag=\"$(gh api", "          set -euo pipefail\n          set -x\n          tag=\"$(gh api"),
			wantIn:   "set -x",
		},

		// The model job runs no check, no container and no release code: what it
		// downloads is its checksum-pinned tools, and the model-free hardening
		// step runs while sudo still works, before the sandbox preflight inside
		// the model step.
		{
			name:     "the hardening step after the model step",
			workflow: moveStep(auditModelResultHead, auditHardeningStep),
			wantIn:   "sudo rule",
		},
		{
			name:     "no hardening step",
			workflow: withoutStep(auditHardeningStep),
			wantIn:   "sudo rule",
		},
		// The step that reads the Claude Code pin file into the outputs the
		// install step's env: names is required: without it the installer takes a
		// version and an integrity from nowhere, or from a pin of its own that
		// ci/claudecodepin never reads (test/lint's install check, issue #277).
		{
			name:     "no Claude Code pin step",
			workflow: withoutStep(auditClaudeCodePinStep),
			wantIn:   "Read the Claude Code pin",
		},
		{
			name:     "a container in the model job",
			workflow: withStep(auditWrapperBuild, "      - name: Inspect the image\n        timeout-minutes: 5\n        run: |\n          set -euo pipefail\n          docker pull ghcr.io/timothymamo/shift2eu:latest\n"),
			wantIn:   "docker",
		},
		{
			name:     "gitleaks in the model job",
			workflow: withStep(auditWrapperBuild, "      - name: Scan the history\n        timeout-minutes: 5\n        run: |\n          set -euo pipefail\n          gitleaks git --redact .\n"),
			wantIn:   "gitleaks",
		},
		{
			name:     "govulncheck in the model job",
			workflow: withStep(auditWrapperBuild, "      - name: Check the modules\n        timeout-minutes: 5\n        run: |\n          set -euo pipefail\n          govulncheck ./...\n"),
			wantIn:   "govulncheck",
		},
		{
			name:     "osv-scanner in the model job",
			workflow: withStep(auditWrapperBuild, "      - name: Check the modules\n        timeout-minutes: 5\n        run: |\n          set -euo pipefail\n          osv-scanner scan source .\n"),
			wantIn:   "osv-scanner",
		},
		{
			name:     "zizmor in the model job",
			workflow: withStep(auditWrapperBuild, "      - name: Lint the workflows\n        timeout-minutes: 5\n        run: |\n          set -euo pipefail\n          zizmor .github/workflows\n"),
			wantIn:   "zizmor",
		},

		// Modules are fetched once, and every later Go command runs offline
		// against what it fetched: a later command that downloads its own module
		// is one no checksum in the workflow pins.
		{
			name:     "a later Go command without GOFLAGS",
			workflow: mutate(t, acceptedWorkflow, auditActionlint, "GOPROXY=off go -C tools run github.com/rhysd/actionlint/cmd/actionlint ../.github/workflows\n"),
			wantIn:   "GOFLAGS",
		},
		{
			name:     "a later Go command without GOPROXY",
			workflow: mutate(t, acceptedWorkflow, auditActionlint, "GOFLAGS=-mod=readonly go -C tools run github.com/rhysd/actionlint/cmd/actionlint ../.github/workflows\n"),
			wantIn:   "GOPROXY",
		},

		// The image digest is trusted only after gh attestation verify accepted
		// the release workflow's provenance for exactly that digest and tag.
		{
			name:     "no attestation check",
			workflow: withoutStep(auditAttestationStep),
			wantIn:   "attestation",
		},
		{
			name:     "an attestation check without --signer-workflow",
			workflow: mutate(t, acceptedWorkflow, "            --signer-workflow \"${GITHUB_REPOSITORY}/.github/workflows/release.yml\" \\\n", ""),
			wantIn:   "signer-workflow",
		},
		{
			name:     "an attestation check without --source-ref",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+"            --deny-self-hosted-runners\n", "            --deny-self-hosted-runners\n"),
			wantIn:   "source-ref",
		},
		{
			name: "the attestation check after the publish step",
			workflow: mutate(t, withoutStep(auditAttestationStep), auditPublishCommand,
				auditPublishCommand+auditAttestationStep),
			wantIn: "attestation",
		},

		// No private key is written where the sandboxed tools can read it:
		// Bash's cat reads any world-readable path, and neither /tmp nor /run is
		// denied to it.
		{
			name: "the model key under /tmp",
			workflow: mutate(t, acceptedWorkflow, "          printf '%s\\n' \"${AUDIT_MODEL_PRIVATE_KEY}\" | age --decrypt",
				"          printf '%s\\n' \"${AUDIT_MODEL_PRIVATE_KEY}\" > /tmp/age.key\n          printf '%s\\n' \"${AUDIT_MODEL_PRIVATE_KEY}\" | age --decrypt"),
			wantIn: "/tmp",
		},
		{
			name: "the publish key under /run",
			workflow: mutate(t, acceptedWorkflow, "          printf '%s\\n' \"${AUDIT_PUBLISH_PRIVATE_KEY}\" | age --decrypt",
				"          printf '%s\\n' \"${AUDIT_PUBLISH_PRIVATE_KEY}\" > /run/age.key\n          printf '%s\\n' \"${AUDIT_PUBLISH_PRIVATE_KEY}\" | age --decrypt"),
			wantIn: "/run",
		},

		// The publish job's own scanner is gitleaks over the text it is about to
		// send, on stdin, and it ignores a .gitleaksignore the repository holds.
		{
			name:     "gitleaks not reading stdin",
			workflow: mutate(t, acceptedWorkflow, auditGitleaksStdin, "          gitleaks git --ignore-gitleaks-allow --redact --no-banner .\n"),
			wantIn:   "stdin",
		},
		{
			name:     "gitleaks honouring the allow file",
			workflow: mutate(t, acceptedWorkflow, "gitleaks stdin --ignore-gitleaks-allow --redact", "gitleaks stdin --redact"),
			wantIn:   "ignore-gitleaks-allow",
		},

		// Every tool is pinned by version and checksum, and Renovate moves a
		// version and its digest together (CLAUDE.md rule 7).
		{
			name:     "a tool version without a Renovate comment",
			workflow: mutate(t, acceptedWorkflow, auditChecksGitleaks, "          GITLEAKS_VERSION: v8.30.0\n          # renovate: datasource=github-release-attachments depName=gitleaks/gitleaks digestVersion=v8.30.0\n          GITLEAKS_SHA256: \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n"),
			wantIn:   "renovate",
		},
		{
			name:     "a downloaded tool without a SHA-256",
			workflow: mutate(t, acceptedWorkflow, auditPublishGitleaks, "          # renovate: datasource=github-releases depName=gitleaks/gitleaks\n          GITLEAKS_VERSION: v8.30.0\n        run: |\n"),
			wantIn:   "SHA-256",
		},

		// Review round 1, findings F1, F2 and F4 to F12: the checks the reviewer and
		// the auditor accepted. Each case changes one part of the accepted shape and
		// must draw a message that names it.
		{
			// F6: no 5b row of the checks job reads an attestation.
			name:     "an attestations scope in the checks job",
			workflow: mutate(t, acceptedWorkflow, auditChecksPermissions, auditChecksPermissions+"      attestations: read\n"),
			wantIn:   "attestations",
		},
		{
			// F5: the split is held, so a step that hands actionlint zizmor's
			// token is refused.
			name:     "zizmor and actionlint in one step",
			workflow: mutate(t, acceptedWorkflow, auditZizmorStep+auditActionlintStep, auditZizmorActionlintStep),
			wantIn:   "zizmor and actionlint",
		},
		{
			name: "the job's token in the actionlint step",
			workflow: mutate(t, acceptedWorkflow, auditActionlintStep, "      - name: actionlint\n"+
				"        timeout-minutes: 10\n"+
				"        env:\n"+
				"          GH_TOKEN: ${{ github.token }}\n"+
				"        run: |\n"+
				"          set -euo pipefail\n"+
				"          "+auditActionlint),
			wantIn: "github.token",
		},
		{
			// F1: the launcher writes the checked bytes under $RUNNER_TEMP, and
			// nothing reads /srv/audit/work/result.json again by its path.
			name: "the encrypt step reading the result under /srv/audit",
			workflow: mutate(t, acceptedWorkflow, auditEncryptResultCommand,
				"          age --encrypt --recipient \"${AUDIT_PUBLISH_PUBLIC_KEY}\" --output \"${RUNNER_TEMP}/model-result.age\" /srv/audit/work/result.json\n"),
			wantIn: "RUNNER_TEMP",
		},
		{
			name: "no -result-out on the model step",
			workflow: mutate(t, acceptedWorkflow, auditRunCommand,
				strings.Replace(auditRunCommand, " -result-out \"${RUNNER_TEMP}/result.json\"", "", 1)),
			wantIn: "-result-out",
		},
		{
			// F2: an empty digest is the documented "image was not pulled"
			// case, skipped with a fixed notice, never a failed run.
			name: "no empty-digest branch",
			workflow: mutate(t, acceptedWorkflow, "          if [ -z \"${digest}\" ]; then\n"+
				"            echo \"image not pulled: no provenance attestation to verify\"\n"+
				"            exit 0\n"+
				"          fi\n", ""),
			wantIn: "image not pulled",
		},
		{
			// The tag's shape is held whatever the digest is: moving it past
			// the empty branch lets an empty digest skip it.
			name: "the tag's shape checked after the empty-digest branch",
			workflow: mutate(t, mutate(t, acceptedWorkflow, "          if [[ ! \"${tag}\" =~ ^v[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then\n"+
				"            echo \"::error title=Audit publish::the tag's shape is no vMAJOR.MINOR.PATCH: see runbook step 11\"\n"+
				"            exit 1\n"+
				"          fi\n", ""),
				"          if [ -z \"${digest}\" ]; then\n"+
					"            echo \"image not pulled: no provenance attestation to verify\"\n"+
					"            exit 0\n"+
					"          fi\n",
				"          if [ -z \"${digest}\" ]; then\n"+
					"            echo \"image not pulled: no provenance attestation to verify\"\n"+
					"            exit 0\n"+
					"          fi\n"+
					"          if [[ ! \"${tag}\" =~ ^v[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then\n"+
					"            echo \"::error title=Audit publish::the tag's shape is no vMAJOR.MINOR.PATCH: see runbook step 11\"\n"+
					"            exit 1\n"+
					"          fi\n"),
			wantIn: "tag's shape",
		},
		{
			// F7: a refused result has no write token minted for it.
			name:     "the scan of the text after the App's token",
			workflow: moveStep(auditPublishCommand, auditGitleaksScanStep),
			wantIn:   "Scan the text it is about to publish",
		},
		{
			// F8: the decrypted archive and its listings live in one place
			// only, the audit directory.
			name:     "no rm -f of the decrypt step's temporary files",
			workflow: mutate(t, acceptedWorkflow, "          rm -f \"${RUNNER_TEMP}/checks.tar\" \"${RUNNER_TEMP}/checks.types\" \"${RUNNER_TEMP}/checks.names\"\n", ""),
			wantIn:   "rm -f",
		},
		{
			// F10: the archive is untrusted, so its entry types are checked.
			name:     "the decrypt step without the tar type check",
			workflow: mutate(t, acceptedWorkflow, "          tar -tvf \"${RUNNER_TEMP}/checks.tar\" > \"${RUNNER_TEMP}/checks.types\"\n", ""),
			wantIn:   "tar -tvf",
		},
		{
			// F10: the archive is unpacked as root, so owners and modes are
			// never the archive's.
			name: "the extract without --no-same-owner",
			workflow: mutate(t, acceptedWorkflow, "          sudo tar -C /srv/audit/checks --no-same-owner --no-same-permissions -xf \"${RUNNER_TEMP}/checks.tar\"\n",
				"          sudo tar -C /srv/audit/checks -xf \"${RUNNER_TEMP}/checks.tar\"\n"),
			wantIn: "--no-same-owner",
		},
		{
			// F4: the download is bound to the artifact the check accepted,
			// through the file it wrote, never a second listing.
			name: "a second listing in the download step",
			workflow: mutate(t, acceptedWorkflow, "          id=\"$(jq -er '.id' \"${artifact}\")\"\n"+
				"          digest=\"$(jq -er '.digest' \"${artifact}\")\"\n"+
				"          gh api \"repos/${GITHUB_REPOSITORY}/actions/artifacts/${id}/zip\" > \"${RUNNER_TEMP}/checks-result.zip\"\n",
				"          id=\"$(gh api \"repos/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}/artifacts\" --jq '.artifacts[] | select(.name == \"checks-result\") | .id')\"\n"+
					"          digest=\"$(gh api \"repos/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}/artifacts\" --jq '.artifacts[] | select(.name == \"checks-result\") | .digest')\"\n"+
					"          gh api \"repos/${GITHUB_REPOSITORY}/actions/artifacts/${id}/zip\" > \"${RUNNER_TEMP}/checks-result.zip\"\n"),
			wantIn: "artifact.json",
		},
		{
			name: "the checked artifact's file under another name",
			workflow: mutate(t, acceptedWorkflow, "          ci/auditpublish artifacts -job model -gh \"$(command -v gh)\" -tmp \"${RUNNER_TEMP}\"\n"+
				"          artifact=\"${RUNNER_TEMP}/artifact.json\"\n",
				"          ci/auditpublish artifacts -job model -gh \"$(command -v gh)\" -tmp \"${RUNNER_TEMP}\"\n"+
					"          artifact=\"${RUNNER_TEMP}/upload.json\"\n"),
			wantIn: "artifact.json",
		},
		{
			// F10: the zip is the listed artifact's, and its digest is checked
			// before unzip reads a byte of it.
			name:     "the download without the artifact's zip endpoint",
			workflow: mutate(t, acceptedWorkflow, "          gh api \"repos/${GITHUB_REPOSITORY}/actions/artifacts/${id}/zip\" > \"${RUNNER_TEMP}/checks-result.zip\"\n", ""),
			wantIn:   "${id}/zip",
		},
		{
			name:     "the download without the digest's sha256sum",
			workflow: mutate(t, acceptedWorkflow, "          echo \"${digest#sha256:}  ${RUNNER_TEMP}/checks-result.zip\" | sha256sum --check --strict -\n", ""),
			wantIn:   "sha256sum",
		},
		{
			name:     "the download without unzip",
			workflow: mutate(t, acceptedWorkflow, "          unzip -q \"${RUNNER_TEMP}/checks-result.zip\" -d \"${RUNNER_TEMP}/checks-result\"\n", ""),
			wantIn:   "unzip",
		},
		{
			name: "the download without the single-file check",
			workflow: mutate(t, acceptedWorkflow, "          if [ \"$(ls -A \"${RUNNER_TEMP}/checks-result\")\" != checks-result.age ]; then\n"+
				"            echo \"::error title=Audit model::checks-result holds more than its ciphertext: see runbook step 11\"\n"+
				"            exit 1\n"+
				"          fi\n", ""),
			wantIn: "ls -A",
		},
		{
			// F11: the history scan ignores no allow comment, and the
			// attestation check refuses a self-hosted runner's provenance.
			name:     "gitleaks on the history honouring the allow file",
			workflow: mutate(t, acceptedWorkflow, "gitleaks git --ignore-gitleaks-allow --redact", "gitleaks git --redact"),
			wantIn:   "--ignore-gitleaks-allow",
		},
		{
			name:     "gitleaks stdin without --redact",
			workflow: mutate(t, acceptedWorkflow, "gitleaks stdin --ignore-gitleaks-allow --redact", "gitleaks stdin --ignore-gitleaks-allow"),
			wantIn:   "--redact",
		},
		{
			name: "no --deny-self-hosted-runners on the attestation check",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+
				"            --deny-self-hosted-runners\n", "            --source-ref \"refs/tags/${tag}\"\n"),
			wantIn: "--deny-self-hosted-runners",
		},
		{
			// F12: Renovate moves every copy of a pin together; a hand edit
			// that moves one alone is refused.
			name:     "the copies of the gitleaks pin differ",
			workflow: mutate(t, acceptedWorkflow, auditPublishGitleaks, strings.ReplaceAll(auditPublishGitleaks, "aaaa", "ffff")),
			wantIn:   "copy",
		},

		// Review round 2, G1: a command the script only mentions in a comment is
		// no command. Every case below leaves the command's text in the step under
		// a # that no shell runs, so a check that reads the comments for its runs
		// (and its order) accepts a step that does nothing.
		{
			name: "a commented-out sha256sum of the zip",
			workflow: mutate(t, acceptedWorkflow, auditZipDigestLine,
				"          # "+strings.TrimPrefix(auditZipDigestLine, "          ")),
			wantIn: "of the zip against the checked digest",
		},
		{
			name: "a commented-out --deny-self-hosted-runners",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+auditVerifyFlagLine,
				"            --source-ref \"refs/tags/${tag}\"\n"+
					"          # --deny-self-hosted-runners: a self-hosted runner's provenance is no provenance\n"),
			wantIn: "--deny-self-hosted-runners",
		},
		{
			name:     "a commented-out auditrun",
			workflow: mutate(t, acceptedWorkflow, auditRunCommand, "          # "+strings.TrimPrefix(auditRunCommand, "          ")),
			wantIn:   "-result-out",
		},
		{
			// The order too is the code's: a commented copy of the tag's shape,
			// standing where the check was, must not satisfy it for a real check
			// that was moved below the verification it must precede.
			name: "a commented copy of the tag's shape standing in for the check moved below the verification",
			workflow: mutate(t, mutate(t, acceptedWorkflow, auditTagShapeCheck, auditTagShapeCommented),
				auditVerifyFlagLine, auditVerifyFlagLine+auditTagShapeCheck),
			wantIn: "after gh attestation verify",
		},
		{
			// Review round 2, G3: the attestation step stands alone. The case puts
			// back the unchecked read it held, so any value but a sha256 digest - a
			// newline in it included - reaches the empty branch and is taken for a
			// not-pulled image. The message the check owes names the refusal's jq,
			// so it holds select(type == "string").
			name:     "the digest read without the string-and-shape refusal",
			workflow: mutate(t, acceptedWorkflow, auditDigestRefusal, auditDigestRead),
			wantIn:   "select(type == \"string\")",
		},

		// Review round 3, H1 (the auditor's finding): a # that starts a word on a
		// code line of a run: script is a shell comment too, and it comments out
		// the rest of that line - a trailing \ included. So
		// `--source-ref "refs/tags/${tag}" # \` swallows the continuation and
		// leaves --deny-self-hosted-runners to run alone (exit 127), and in
		// `if ! A` / `B; then` only B's status counts: a verification that failed
		// passes, while every regex above still matches the text under the #.
		// Such a line is a problem, whichever command its comment holds: the two
		// cases below hang a guard on a trailing comment and must draw a message
		// naming the comment (wantIn "comment"). The # inside a word stays
		// allowed - the accepted shape's ${digest#sha256:} and
		// ${GITLEAKS_VERSION#v} draw no message - and a # after whitespace inside
		// quotes is refused, which is accepted (fail closed).
		{
			name: "a trailing comment on the --source-ref line",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+auditVerifyFlagLine,
				"            --source-ref \"refs/tags/${tag}\" # \\\n"+auditVerifyFlagLine),
			wantIn: "comment",
		},
		{
			name: "the zip's sha256sum hidden in a trailing comment on the download",
			workflow: mutate(t, mutate(t, acceptedWorkflow, auditZipDigestLine, ""),
				auditChecksArtifactDownloadLine,
				strings.TrimSuffix(auditChecksArtifactDownloadLine, "\n")+" # "+strings.TrimPrefix(auditZipDigestLine, "          ")),
			wantIn: "comment",
		},

		// Review round 4 (the reviewer's blocker, the auditor's M1): a # starts a
		// word not only after whitespace but right after a control operator, so
		// `;#`, `&&#` and `|#` begin a comment too, and the cases above passed
		// them. The same finding makes the attestation step's guards separate
		// substring patterns: the verification can be cut with no # at all, by
		// ending the gh command early (`;`) or by dropping the trailing \, which
		// leaves --deny-self-hosted-runners to run alone, and in `if ! A; B; then`
		// only B's status counts, so a failed gh attestation verify passes. And
		// the zip's digest check is an unanchored substring, so it is satisfied by
		// the same command text at the end of another command's line.
		//
		// The three cases below hang the same guard on a comment that begins right
		// after a control operator, and must draw the message naming the comment
		// (wantIn "comment").
		{
			name: "a trailing comment right after a ; on the --source-ref line",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+auditVerifyFlagLine,
				"            --source-ref \"refs/tags/${tag}\";# \\\n"+auditVerifyFlagLine),
			wantIn: "comment",
		},
		{
			name: "a trailing comment right after a && on the --source-ref line",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+auditVerifyFlagLine,
				"            --source-ref \"refs/tags/${tag}\"&&# \\\n"+auditVerifyFlagLine),
			wantIn: "comment",
		},
		{
			name: "a trailing comment right after a | on the --source-ref line",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+auditVerifyFlagLine,
				"            --source-ref \"refs/tags/${tag}\"|# \\\n"+auditVerifyFlagLine),
			wantIn: "comment",
		},
		{
			name: "the zip's sha256sum hidden after a ; on the download",
			workflow: mutate(t, mutate(t, acceptedWorkflow, auditZipDigestLine, ""),
				auditChecksArtifactDownloadLine,
				strings.TrimSuffix(auditChecksArtifactDownloadLine, "\n")+";#"+strings.TrimPrefix(auditZipDigestLine, "          ")),
			wantIn: "comment",
		},
		{
			name: "the zip's sha256sum hidden after a & on the download",
			workflow: mutate(t, mutate(t, acceptedWorkflow, auditZipDigestLine, ""),
				auditChecksArtifactDownloadLine,
				strings.TrimSuffix(auditChecksArtifactDownloadLine, "\n")+"&#"+strings.TrimPrefix(auditZipDigestLine, "          ")),
			wantIn: "comment",
		},

		// The verification is one command: the three cases below break it with no #
		// at all, so only a contiguous check of the whole command - from the gh
		// attestation verify line through every flag's line to the last flag - sees
		// them. The message such a check owes names the command, so wantIn is
		// "gh attestation verify".
		{
			name: "the verification cut after --source-ref by a ;",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+auditVerifyFlagLine,
				"            --source-ref \"refs/tags/${tag}\"; \\\n"+auditVerifyFlagLine),
			wantIn: "gh attestation verify",
		},
		{
			name: "the verification cut before --deny-self-hosted-runners by the dropped continuation",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+auditVerifyFlagLine,
				"            --source-ref \"refs/tags/${tag}\"\n"+auditVerifyFlagLine),
			wantIn: "gh attestation verify",
		},

		// Bash joins a whole comment line to the command a trailing \ continues,
		// and the comment then cuts the command there: the flag below it is no part
		// of the verification any more. The comment lines are dropped before the
		// patterns match, so the cut verification would otherwise look contiguous
		// - this case differs from the accepted shape by that comment line alone,
		// and without the refusal of it the check reads the accepted
		// verification and draws no message at all.
		{
			name: "a whole comment line right after the --source-ref continuation",
			workflow: mutate(t, acceptedWorkflow, "            --source-ref \"refs/tags/${tag}\" \\\n"+auditVerifyFlagLine,
				"            --source-ref \"refs/tags/${tag}\" \\\n          # x\n"+auditVerifyFlagLine),
			wantIn: "continues a",
		},

		// The zip's digest check stands on a line of its own: run at the end of
		// another command's line, it still runs, but as part of that command's
		// conditions, so the case that appends it without a # must draw the
		// message naming the zip digest check.
		{
			name: "the zip's sha256sum appended to the download without a #",
			workflow: mutate(t, mutate(t, acceptedWorkflow, auditZipDigestLine, ""),
				auditChecksArtifactDownloadLine,
				strings.TrimSuffix(auditChecksArtifactDownloadLine, "\n")+";"+strings.TrimPrefix(auditZipDigestLine, "          ")),
			wantIn: "of the zip against the checked digest",
		},

		// A document the reader cannot follow tells the check nothing, so it is
		// refused rather than passed.
		{name: "an empty document", workflow: "", wantIn: "empty"},
		{name: "a document that is no YAML", workflow: "\ton:\n", wantIn: "read"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := check.AuditWorkflow([]byte(tc.workflow))

			if tc.wantIn == "" {
				if len(got) != 0 {
					t.Errorf("AuditWorkflow() = %q, want no message", got)
				}

				return
			}

			fresh := added(accepted, got)
			if len(fresh) == 0 {
				t.Errorf("AuditWorkflow() draws no message for the case, want one naming %q: the accepted shape's messages were %q", tc.wantIn, got)

				return
			}

			if joined := strings.Join(fresh, "\n"); !strings.Contains(joined, tc.wantIn) {
				t.Errorf("AuditWorkflow() = %q, want a message containing %q", fresh, tc.wantIn)
			}
		})
	}
}
