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
//     the accepted shape declares, never a write scope and never packages: or
//     id-token. The checks job's are contents, attestations, actions and
//     issues, each read; the model job's are contents and actions, the publish
//     job's contents, actions and attestations.
//   - Each job runs in the security-audit environment on ubuntu-24.04, with a
//     timeout-minutes below 360 and every step's timeout-minutes set. The model
//     job needs checks, the publish job needs model. The checks job refuses a
//     latest release tag that is no vMAJOR.MINOR.PATCH before it records it, as
//     the publish job's attestation step does.
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
//     The artifact's ID and digest are read from the file that check wrote
//     before the guard of their shape runs, so the guard is what no value of
//     another shape passes.
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
//     --source-ref refs/tags/<tag>, before the step that trusts the digest, and
//     for the release's own image, ghcr.io/${GITHUB_REPOSITORY,,}.
//     gitleaks runs the stdin subcommand with --ignore-gitleaks-allow.
//   - No private key is written under /tmp or /run.
//   - Every tool version line has a # renovate: comment above it, and every
//     downloaded tool's SHA-256 line is 64 lower-case hex with a
//     digestVersion= comment of its own.
//   - A document the reader cannot follow, or an empty one, is refused.
//
// The checks job's rows 6 to 15 (PR 5c), added to the shapes above:
//
//   - Nine steps after actionlint, in this order: OpenSSF scorecard; Verify the
//     release's files, signature and attestations; Pull the image anonymously
//     by digest; govulncheck on the release binaries; trivy on the image and its
//     SBOM; Check the image's runtime settings; Run the CLI on hostile inventory
//     files; Fuzz the committed Fuzz targets; Supply-chain reads.
//   - The job's token, as GH_TOKEN in a step-level env:, only on the steps that
//     read GitHub's API: the record of the tag (row 1), zizmor (row 5),
//     scorecard (row 6), the release's verification (row 7) and the supply-chain
//     reads (row 15). No container step holds it, and neither do rows 8, 9 and
//     10, which read no API.
//   - Every container the job creates carries --read-only, --network none,
//     --cap-drop ALL and --security-opt no-new-privileges; row 14's is the only
//     one with a --tmpfs, exactly /scratch:rw,exec,nosuid,size=4g,mode=1777,
//     and it runs from a Go image pinned by @sha256: digest. Nowhere in the
//     workflow is a mount (-v, --volume, --mount), a docker.sock, a
//     --privileged container, --network host or a docker login.
//   - Four guards, each whole: row 7's tag shape, before ${tag} reaches a
//     command line; the image digest's shape in rows 8, 11 and 13; row 14's
//     fuzz target's package and name shape; row 15's ruleset ID shape.
//   - scorecard, cosign and trivy are pinned by version, SHA-256 and a
//     # renovate: comment like every other tool, and scorecard neither
//     publishes nor writes its report anywhere but the results directory; no
//     id-token is asked for.
//   - Every Go command after the module download runs offline: the binary-mode
//     govulncheck of row 9 carries GOFLAGS=-mod=readonly and GOPROXY=off, and
//     the fuzz container of row 14 is handed GOFLAGS and GOPROXY by its
//     docker create.
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
	// no other job shares: the four read scopes rows 6 to 15 read GitHub with
	// (PR 5c). No packages scope, because the image is pulled anonymously, and
	// no id-token: scorecard runs as the CLI, not the action.
	auditChecksPermissions = "    # GITHUB_TOKEN reads only: contents for the checkout, the release tag and the release download;\n" +
		"    # attestations for gh attestation verify (row 7); actions for scorecard's reads of the workflows'\n" +
		"    # runs (row 6); issues for Renovate's Dependency Dashboard (row 15). The image is public, so no\n" +
		"    # packages scope: it is pulled anonymously (row 8). Nothing here writes, and no read needs admin:\n" +
		"    # what the token is refused is recorded \"not run\".\n" +
		"    permissions:\n" +
		"      contents: read\n" +
		"      attestations: read\n" +
		"      actions: read\n" +
		"      issues: read\n"
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
	// auditChecksArtifactShapeGuard and auditPublishArtifactShapeGuard are the
	// two download steps' guards of the ID and digest the artifact check wrote
	// to artifact.json: a value that is no ID or no sha256 digest reaches gh
	// api, sha256sum and unzip otherwise. The message each draws names the
	// artifact's ID.
	auditChecksArtifactShapeGuard = "          if [[ ! \"${id}\" =~ ^[0-9]+$ ]] || [[ ! \"${digest}\" =~ ^sha256:[0-9a-f]{64}$ ]]; then\n" +
		"            echo \"::error title=Audit model::checks-result has no artifact ID and SHA-256 digest: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	auditPublishArtifactShapeGuard = "          if [[ ! \"${id}\" =~ ^[0-9]+$ ]] || [[ ! \"${digest}\" =~ ^sha256:[0-9a-f]{64}$ ]]; then\n" +
		"            echo \"::error title=Audit publish::model-result has no artifact ID and SHA-256 digest: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	// auditArtifactIDRead and auditArtifactDigestRead are the two download
	// steps' reads of the artifact's ID and digest from the file the artifact
	// check wrote, in the order the step runs them: both before the shape guard,
	// so that no value of another shape reaches gh api, sha256sum or unzip. The
	// message a guard moved above the ID read draws names the ID read.
	auditArtifactIDRead     = "          id=\"$(jq -er '.id' \"${artifact}\")\"\n"
	auditArtifactDigestRead = "          digest=\"$(jq -er '.digest' \"${artifact}\")\"\n"
	// auditChecksTagShapeCheck is the checks job's own tag-shape check, in the
	// step that records the latest release tag: the guard the publish job's
	// attestation step holds too, with the checks job's own echo, because the
	// result the audit reasons about names a release only when the tag is one.
	// The message each of its breakages draws names the tag's shape.
	auditChecksTagShapeCheck = "          if [[ ! \"${tag}\" =~ ^v[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then\n" +
		"            echo \"::error title=Audit checks::the latest release's tag is no vMAJOR.MINOR.PATCH: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	// auditImage is the image the release publishes, ghcr.io/<the repository's
	// owner>/<repository>, read from the run's own GITHUB_REPOSITORY, and
	// auditImageLine the attestation step's head and its line that reads it. The
	// verification is about that image alone, so another image's attestation is
	// no provenance for this release's, and the message a case draws names the
	// image. The step's head is part of it because rows 7, 8, 11 and 13 read the
	// same image (PR 5c), and the attestation step is the one that verifies it.
	auditImage     = "ghcr.io/${GITHUB_REPOSITORY,,}"
	auditImageLine = "      - name: Verify the image's provenance attestation\n" +
		"        timeout-minutes: 10\n" +
		"        env:\n" +
		"          GH_TOKEN: ${{ github.token }}\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          image=\"" + auditImage + "\"\n"
	// auditChecksSingleFileCheck and auditPublishSingleFileCheck are the two
	// download steps' single-file checks: the unzipped directory holds the one
	// ciphertext and nothing else, and the branch that refuses anything more.
	auditChecksSingleFileCheck = "          if [ \"$(ls -A \"${RUNNER_TEMP}/checks-result\")\" != checks-result.age ]; then\n" +
		"            echo \"::error title=Audit model::checks-result holds more than its ciphertext: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	auditPublishSingleFileCheck = "          if [ \"$(ls -A \"${RUNNER_TEMP}/model-result\")\" != model-result.age ]; then\n" +
		"            echo \"::error title=Audit publish::model-result holds more than its ciphertext: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	// auditEmptyDigestCheck is the attestation step's branch for a run that
	// pulled no image: a fixed notice and exit 0, the verification skipped.
	auditEmptyDigestCheck = "          if [ -z \"${digest}\" ]; then\n" +
		"            echo \"image not pulled: no provenance attestation to verify\"\n" +
		"            exit 0\n" +
		"          fi\n"
	// auditVerifyFlagLine is the last line of the attestation step's gh
	// verification, whose --deny-self-hosted-runners the case that comments it
	// out leaves in a comment alone.
	auditVerifyFlagLine = "            --deny-self-hosted-runners\n"
	// auditBareVerification is the fixture's verification as the attestation step
	// runs it: bare, so that set -e fails the step for a refused attestation. It
	// has no branch body to break, and the cases of review round 5 that break one
	// put the conditional form below in its place first.
	auditBareVerification = "          gh attestation verify \"oci://${image}@${digest}\" \\\n" +
		"            --repo \"${GITHUB_REPOSITORY}\" \\\n" +
		"            --signer-workflow \"${GITHUB_REPOSITORY}/.github/workflows/release.yml\" \\\n" +
		"            --source-ref \"refs/tags/${tag}\" \\\n" +
		"            --deny-self-hosted-runners\n"
	// auditVerificationRefusal is the refusal the conditional form's branch holds:
	// the named error, then the exit that fails the step. The cases that drop it
	// or weaken it to exit 0 leave the echo alone.
	auditVerificationRefusal = "            echo \"::error title=Audit publish::the image has no provenance attestation from the release workflow at that tag: see runbook step 11\"\n" +
		"            exit 1\n"
	// auditVerificationConditional is the form the repository's workflow runs the
	// verification in, and the check accepts: the condition of an
	// if ! ... > /dev/null; then, whose branch is the refusal above. It is the
	// accepted alternative a pin of the branch body must keep, and the base the
	// cases that break that body change.
	auditVerificationConditional = "          if ! gh attestation verify \"oci://${image}@${digest}\" \\\n" +
		"            --repo \"${GITHUB_REPOSITORY}\" \\\n" +
		"            --signer-workflow \"${GITHUB_REPOSITORY}/.github/workflows/release.yml\" \\\n" +
		"            --source-ref \"refs/tags/${tag}\" \\\n" +
		"            --deny-self-hosted-runners > /dev/null; then\n" +
		auditVerificationRefusal +
		"          fi\n"
	// auditDigestShapeCheck is the attestation step's digest-shape check, the guard
	// the case that drops its exit 1 leaves refusing nothing.
	auditDigestShapeCheck = "          if [[ ! \"${digest}\" =~ ^sha256:[0-9a-f]{64}$ ]]; then\n" +
		"            echo \"::error title=Audit publish::the image digest is no sha256 digest: see runbook step 11\"\n" +
		"            exit 1\n"
	// auditReleaseLookup is the checks job's release lookup, whose text the case
	// of review round 5 hides in a backtick comment.
	auditReleaseLookup = "          tag=\"$(gh api \"repos/${GH_REPO}/releases/latest\" --jq .tag_name)\"\n"
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
		"          image=\"ghcr.io/${GITHUB_REPOSITORY,,}\"\n" +
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
	// auditScorecardStep is the checks job's row 6 step, the shortest of the nine
	// PR 5c adds and the one the cases that remove, rename and reorder a step
	// anchor on.
	auditScorecardStep = "      - name: OpenSSF scorecard\n" +
		"        timeout-minutes: 20\n" +
		"        env:\n" +
		"          GH_TOKEN: ${{ github.token }}\n" +
		"          GH_REPO: ${{ github.repository }}\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          results=\"${RUNNER_TEMP}/results\"\n" +
		"          out=\"${results}/scorecard\"\n" +
		"          mkdir -p \"${out}\"\n" +
		"          checks=\"Binary-Artifacts,CI-Tests,CII-Best-Practices,Code-Review,Contributors,Dangerous-Workflow,Dependency-Update-Tool,Fuzzing,License,Maintained,Packaging,Pinned-Dependencies,SAST,Security-Policy,Signed-Releases,Token-Permissions,Vulnerabilities\"\n" +
		"          rc=0\n" +
		"          timeout 15m scorecard --repo \"github.com/${GH_REPO}\" --checks \"${checks}\" --format json --show-details > \"${out}/scorecard.json\" 2> \"${out}/scorecard.stderr\" || rc=$?\n" +
		"          echo \"${rc}\" > \"${out}/scorecard.exit\"\n" +
		"          if [ \"${rc}\" -eq 124 ]; then\n" +
		"            echo \"scorecard timed-out\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"scorecard timed out after 15 minutes\" >> \"${results}/limits.txt\"\n" +
		"          fi\n" +
		"          printf '%s\\n' Branch-Protection Webhooks > \"${out}/admin-checks.txt\"\n" +
		"          echo \"scorecard-admin needs-admin-read\" >> \"${results}/not-run.txt\"\n" +
		"          echo \"finished\"\n"
	// auditScorecardCommand is that step's scorecard command: the CLI, its report
	// in the results directory, and no --publish, which would put the result
	// somewhere the run does not keep it.
	auditScorecardCommand = "          timeout 15m scorecard --repo \"github.com/${GH_REPO}\" --checks \"${checks}\" --format json --show-details > \"${out}/scorecard.json\" 2> \"${out}/scorecard.stderr\" || rc=$?\n"
	// auditContainerFlags are the flags every container the checks job creates
	// carries (PR 5c): a read-only root filesystem, no network, no capability
	// and no new privilege.
	auditContainerFlags = " --read-only --network none --cap-drop ALL --security-opt no-new-privileges"
	// auditHostileCreateFlags is row 13's container's own limits, which no other
	// docker create holds: the anchor the cases that put a flag, a mount or a
	// tmpfs into a container use.
	auditHostileCreateFlags = " --memory 512m --pids-limit 64 "
	// auditFuzzTmpfs is row 14's one tmpfs, exactly the size, mode and flags its
	// go test needs: the only tmpfs any container of the workflow has.
	auditFuzzTmpfs = "--tmpfs /scratch:rw,exec,nosuid,size=4g,mode=1777"
	// auditGoImage is row 14's Go image, pinned by tag and @sha256: digest.
	auditGoImage = "          GO_IMAGE: \"1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280\"\n"
	// auditImagePull is row 8's pull, the line the case that logs in to the
	// registry hangs its docker login on.
	auditImagePull = "          timeout 10m docker pull --platform linux/amd64 \"${image}@${digest}\" > \"${out}/pull.stdout\" 2> \"${out}/pull.stderr\" || rc=$?\n"
	// auditBinaryGovulncheck is row 9's binary-mode govulncheck, the one Go tool
	// the workflow runs that is no go command, so its offline flags are the
	// step's own.
	auditBinaryGovulncheck = "GOFLAGS=-mod=readonly GOPROXY=off timeout 5m govulncheck -mode=binary"
	// auditFuzzEnvHandoff is the docker create's hand-off of the step's env: to
	// the fuzz container: go test runs offline only with both.
	auditFuzzEnvHandoff = "--env GOFLAGS --env GOPROXY --env GOTOOLCHAIN=local"
	// auditReleaseStepEnv is row 7's env: and the read of the tag it holds the
	// token for, the anchor the case that drops the token uses.
	auditReleaseStepEnv = "          GH_TOKEN: ${{ github.token }}\n" +
		"          GH_REPO: ${{ github.repository }}\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          results=\"${RUNNER_TEMP}/results\"\n" +
		"          tag=\"$(sed -n 's/^release_tag=//p' \"${results}/target.txt\")\"\n"
	// The checks job's four guards, each whole (PR 5c): row 7's tag shape, before
	// the tag reaches a command line; the image digest's shape, the same four
	// lines in rows 8, 11 and 13; row 14's fuzz target shape; and row 15's
	// ruleset ID shape. A case removes one, drops its exit 1 or runs its branch
	// together onto the echo's line.
	auditReleaseTagGuard = "          if [[ ! \"${tag}\" =~ ^v[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then\n" +
		"            echo \"::error title=Audit checks::the recorded release tag is no vMAJOR.MINOR.PATCH: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	auditDigestGuard = "          if [[ ! \"${digest}\" =~ ^sha256:[0-9a-f]{64}$ ]]; then\n" +
		"            echo \"::error title=Audit checks::the recorded image digest is no sha256 digest: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
	auditFuzzShapeGuard = "            if [[ ! \"${name}\" =~ ^Fuzz[A-Za-z0-9_]*$ ]] || [[ ! \"${pkg}\" =~ ^[A-Za-z0-9_][A-Za-z0-9_./-]*$ ]] || [[ \"${pkg}\" == *..* ]]; then\n" +
		"              echo \"::error title=Audit checks::a fuzz target's package or name has an unexpected shape: see runbook step 11\"\n" +
		"              exit 1\n" +
		"            fi\n"
	auditRulesetGuard = "            if [[ ! \"${id}\" =~ ^[0-9]+$ ]]; then\n" +
		"              echo \"::error title=Audit checks::a ruleset ID is no number: see runbook step 11\"\n" +
		"              exit 1\n" +
		"            fi\n"
	// The "image was not pulled" branch each of rows 8, 11 and 13 runs right
	// before its digest guard, one message each: the context that lets a case
	// take the guard out of one row alone, since the three guards are identical.
	auditImagePullBranch = "            echo \"image-pull image-not-pulled\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"finished\"\n            exit 0\n          fi\n"
	auditImageRuntimeBranch = "            echo \"image-runtime image-not-pulled\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"finished\"\n            exit 0\n          fi\n"
	auditHostileNotPulledBranch = "            echo \"hostile-inventories image-not-pulled\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"finished\"\n            exit 0\n          fi\n"
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

// joinedBranch returns guard, a guard's whole header and branch, with the
// branch's lines run together onto the echo's line: the echo, the exit and the
// fi are then words of one command, which prints and carries on - the branch
// refuses nothing while the header still matches.
func joinedBranch(guard string) string {
	lines := strings.Split(strings.TrimSuffix(guard, "\n"), "\n")

	return lines[0] + "\n" + strings.Join(lines[1:], " ") + "\n"
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

	// The fixture's own messages are what added() takes every case's diff from,
	// so a rule the check does not hold yet leaves the cases below decidable one
	// by one instead of all of them failing for that one reason;
	// TestAuditWorkflowAcceptsTheAcceptedShape is what refuses a fixture the
	// check does not accept.
	if len(accepted) != 0 {
		t.Logf("AuditWorkflow(%s) = %q, want no message: every case's added() diff is taken from these", auditWorkflowFixture, accepted)
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
			// PR 5c: row 7 reads the release's attestations, so the checks job holds
			// the scope - and exactly the four scopes its rows read with.
			name:     "no attestations scope in the checks job",
			workflow: mutate(t, acceptedWorkflow, auditChecksPermissions, strings.Replace(auditChecksPermissions, "      attestations: read\n", "", 1)),
			wantIn:   "attestations",
		},
		{
			// Scorecard runs as the CLI, never the action: no step of the workflow
			// asks for an id-token.
			name:     "an id-token scope in the checks job",
			workflow: mutate(t, acceptedWorkflow, auditChecksPermissions, auditChecksPermissions+"      id-token: write\n"),
			wantIn:   "id-token",
		},
		{
			name:     "a write scope in the checks job",
			workflow: mutate(t, acceptedWorkflow, auditChecksPermissions, strings.Replace(auditChecksPermissions, "      issues: read\n", "      issues: write\n", 1)),
			wantIn:   "issues",
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
				auditChecksArtifactShapeGuard+
				"          gh api \"repos/${GITHUB_REPOSITORY}/actions/artifacts/${id}/zip\" > \"${RUNNER_TEMP}/checks-result.zip\"\n",
				"          id=\"$(gh api \"repos/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}/artifacts\" --jq '.artifacts[] | select(.name == \"checks-result\") | .id')\"\n"+
					"          digest=\"$(gh api \"repos/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}/artifacts\" --jq '.artifacts[] | select(.name == \"checks-result\") | .digest')\"\n"+
					auditChecksArtifactShapeGuard+
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
			name:     "the download without the single-file check",
			workflow: mutate(t, acceptedWorkflow, auditChecksSingleFileCheck, ""),
			wantIn:   "ls -A",
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

		// Review round 5 (the auditor's low finding L1, the reviewer's suggestion 1
		// and nit 2): a guard is its branch body, not its [[ ... ]] header alone.
		// A tag or digest check whose header still matches but whose branch no
		// longer refuses the run checks nothing - the echo runs and the script
		// carries on, so a tag of another shape or a digest that is no sha256 digest
		// reaches the verification. The same holds for the verification in its
		// if ! ... > /dev/null; then form: with the exit dropped, or weakened to
		// exit 0, the echo is all the branch does and a refused attestation passes.
		// And a backtick starts a word too, so a # right after one is a shell comment
		// inside a command substitution, where a hidden command's text still stands
		// for the patterns to read.
		{
			// The accepted fixture runs the verification bare under set -e, so it
			// has no branch body of its own; this is the conditional form the
			// repository's workflow runs, which the check accepts and the two cases
			// below break. It draws no message, and must keep drawing none.
			name:     "the real workflow's conditional verification",
			workflow: mutate(t, acceptedWorkflow, auditBareVerification, auditVerificationConditional),
		},
		{
			name: "the verification's branch without its exit 1",
			workflow: mutate(t, mutate(t, acceptedWorkflow, auditBareVerification, auditVerificationConditional),
				auditVerificationRefusal, strings.TrimSuffix(auditVerificationRefusal, "            exit 1\n")),
			wantIn: "gh attestation verify",
		},
		{
			name: "the verification's branch with exit 0",
			workflow: mutate(t, mutate(t, acceptedWorkflow, auditBareVerification, auditVerificationConditional),
				auditVerificationRefusal, strings.Replace(auditVerificationRefusal, "exit 1", "exit 0", 1)),
			wantIn: "gh attestation verify",
		},
		{
			name:     "the tag's shape without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditTagShapeCheck, strings.Replace(auditTagShapeCheck, "            exit 1\n", "", 1)),
			wantIn:   "tag's shape",
		},
		{
			name:     "the digest's shape without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditDigestShapeCheck, strings.Replace(auditDigestShapeCheck, "            exit 1\n", "", 1)),
			wantIn:   "digest's shape",
		},
		{
			// The lookup's text stays in the script, so the releases/latest
			// pattern reads it; the substitution it sits in is a comment, so the
			// lookup runs nowhere and the tag is empty.
			name: "the release lookup hidden in a backtick comment",
			workflow: mutate(t, acceptedWorkflow, auditReleaseLookup,
				"          tag=`#$(gh api \"repos/${GH_REPO}/releases/latest\" --jq .tag_name)`\n"),
			wantIn: "comment",
		},

		// Review round 6 (the reviewer's and the auditor's findings): the guards
		// of round 5 are pinned whole, but three of them still hold their branch
		// with a \s+, which one space satisfies, and the download steps' own
		// guards are pinned by nothing at all. The cases below run a branch's
		// lines together onto its echo's line, drop a branch's exit 1 and drop
		// the download's shape guard of the artifact's ID and digest; each must
		// draw the message the check owes that guard.
		{
			name:     "the digest read's branch joined onto the echo's line",
			workflow: mutate(t, acceptedWorkflow, auditDigestRefusal, joinedBranch(auditDigestRefusal)),
			wantIn:   "read of the digest",
		},
		{
			name:     "the digest read's branch without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditDigestRefusal, strings.Replace(auditDigestRefusal, "            exit 1\n", "", 1)),
			wantIn:   "read of the digest",
		},
		{
			name:     "the empty-digest branch joined onto the echo's line",
			workflow: mutate(t, acceptedWorkflow, auditEmptyDigestCheck, joinedBranch(auditEmptyDigestCheck)),
			wantIn:   "empty digest",
		},
		{
			name:     "the checks download's single-file check without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditChecksSingleFileCheck, strings.Replace(auditChecksSingleFileCheck, "            exit 1\n", "", 1)),
			wantIn:   "ls -A",
		},
		{
			name:     "the publish download's single-file check without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditPublishSingleFileCheck, strings.Replace(auditPublishSingleFileCheck, "            exit 1\n", "", 1)),
			wantIn:   "ls -A",
		},
		{
			name:     "the checks download without the artifact's shape guard",
			workflow: mutate(t, acceptedWorkflow, auditChecksArtifactShapeGuard, ""),
			wantIn:   "artifact's ID",
		},
		{
			name:     "the publish download without the artifact's shape guard",
			workflow: mutate(t, acceptedWorkflow, auditPublishArtifactShapeGuard, ""),
			wantIn:   "artifact's ID",
		},
		{
			name:     "the checks download's shape guard without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditChecksArtifactShapeGuard, strings.Replace(auditChecksArtifactShapeGuard, "            exit 1\n", "", 1)),
			wantIn:   "artifact's ID",
		},
		{
			name:     "the publish download's shape guard without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditPublishArtifactShapeGuard, strings.Replace(auditPublishArtifactShapeGuard, "            exit 1\n", "", 1)),
			wantIn:   "artifact's ID",
		},

		// Round 7, carried from PR #549's review: three things the accepted shape
		// runs and no case pinned. The two download steps read the artifact's ID
		// from artifact.json before the guard of its shape, so the guard stands
		// between a value of another shape and gh api; the checks job refuses a
		// latest release tag of another shape before it records the tag its result
		// names; and the attestation step verifies the release's own image, not
		// another registry path's. A case below moves, drops or breaks each, and
		// must draw the message that names it.
		{
			name: "the checks download's shape guard before the ID read",
			workflow: mutate(t, acceptedWorkflow,
				auditArtifactIDRead+auditArtifactDigestRead+auditChecksArtifactShapeGuard,
				auditArtifactDigestRead+auditChecksArtifactShapeGuard+auditArtifactIDRead),
			wantIn: "the ID from artifact.json",
		},
		{
			name: "the publish download's shape guard before the ID read",
			workflow: mutate(t, acceptedWorkflow,
				auditArtifactIDRead+auditArtifactDigestRead+auditPublishArtifactShapeGuard,
				auditArtifactDigestRead+auditPublishArtifactShapeGuard+auditArtifactIDRead),
			wantIn: "the ID from artifact.json",
		},
		{
			name:     "the checks job's tag shape guard removed",
			workflow: mutate(t, acceptedWorkflow, auditChecksTagShapeCheck, ""),
			wantIn:   "tag's shape",
		},
		{
			name:     "the checks job's tag shape guard without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditChecksTagShapeCheck, strings.Replace(auditChecksTagShapeCheck, "            exit 1\n", "", 1)),
			wantIn:   "tag's shape",
		},
		{
			name:     "the checks job's tag shape guard joined onto the echo's line",
			workflow: mutate(t, acceptedWorkflow, auditChecksTagShapeCheck, joinedBranch(auditChecksTagShapeCheck)),
			wantIn:   "tag's shape",
		},
		{
			name:     "no image line in the attestation step",
			workflow: mutate(t, acceptedWorkflow, auditImageLine, strings.TrimSuffix(auditImageLine, "          image=\""+auditImage+"\"\n")),
			wantIn:   auditImage,
		},
		{
			name:     "the attestation step verifying another image",
			workflow: mutate(t, acceptedWorkflow, auditImageLine, strings.Replace(auditImageLine, auditImage, "ghcr.io/example/other", 1)),
			wantIn:   auditImage,
		},

		// PR 5c: the checks job's rows 6 to 15. Nine steps after actionlint, in
		// order; the job's token only in the step-level env: of a step that reads
		// GitHub's API; every container read-only, networkless, capability-less and
		// unprivileged, with no mount and no socket; four guards held whole; three
		// tool pins; and every Go command offline. Each case changes one of them.
		{
			name:     "no scorecard step",
			workflow: withoutStep(auditScorecardStep),
			wantIn:   "OpenSSF scorecard",
		},
		{
			name:     "scorecard's step under another name",
			workflow: mutate(t, acceptedWorkflow, "      - name: OpenSSF scorecard\n", "      - name: Scorecard run\n"),
			wantIn:   "Scorecard run",
		},
		{
			name:     "scorecard's step after the release's verification",
			workflow: moveStep("      - name: Pull the image anonymously by digest\n", auditScorecardStep),
			wantIn:   "want it before",
		},

		// The job's token is in the step-level env: of the steps that read
		// GitHub's API alone: rows 1, 5, 6, 7 and 15. A container step, or a step
		// that reads no API, holds none.
		{
			name:     "no token on the release's verification",
			workflow: mutate(t, acceptedWorkflow, auditReleaseStepEnv, strings.Replace(auditReleaseStepEnv, "          GH_TOKEN: ${{ github.token }}\n", "", 1)),
			wantIn:   "GH_TOKEN",
		},
		{
			name:     "the job's token on the image pull",
			workflow: mutate(t, acceptedWorkflow, "      - name: Pull the image anonymously by digest\n        timeout-minutes: 15\n        run: |\n", "      - name: Pull the image anonymously by digest\n        timeout-minutes: 15\n        env:\n          GH_TOKEN: ${{ github.token }}\n        run: |\n"),
			wantIn:   "github.token",
		},
		{
			name:     "the job's token on the hostile inventories",
			workflow: mutate(t, acceptedWorkflow, "      - name: Run the CLI on hostile inventory files\n        timeout-minutes: 20\n        run: |\n", "      - name: Run the CLI on hostile inventory files\n        timeout-minutes: 20\n        env:\n          GH_TOKEN: ${{ github.token }}\n        run: |\n"),
			wantIn:   "github.token",
		},
		{
			name:     "the job's token on the fuzz step",
			workflow: mutate(t, acceptedWorkflow, "        env:\n          GOFLAGS: -mod=readonly\n", "        env:\n          GH_TOKEN: ${{ github.token }}\n          GOFLAGS: -mod=readonly\n"),
			wantIn:   "github.token",
		},

		// Every container the checks job creates is read-only, has no network,
		// no capability and no new privilege. Each flag taken out in turn is one
		// case, from the three docker create lines at once.
		{
			name:     "no --read-only on the containers",
			workflow: mutateEvery(t, acceptedWorkflow, auditContainerFlags, " --network none --cap-drop ALL --security-opt no-new-privileges"),
			wantIn:   "--read-only",
		},
		{
			name:     "no --network none on the containers",
			workflow: mutateEvery(t, acceptedWorkflow, auditContainerFlags, " --read-only --cap-drop ALL --security-opt no-new-privileges"),
			wantIn:   "--network none",
		},
		{
			name:     "no --cap-drop ALL on the containers",
			workflow: mutateEvery(t, acceptedWorkflow, auditContainerFlags, " --read-only --network none --security-opt no-new-privileges"),
			wantIn:   "--cap-drop ALL",
		},
		{
			name:     "no --security-opt no-new-privileges on the containers",
			workflow: mutateEvery(t, acceptedWorkflow, auditContainerFlags, " --read-only --network none --cap-drop ALL"),
			wantIn:   "no-new-privileges",
		},
		{
			// Row 14's one tmpfs, at the size and with the flags go test needs: a
			// smaller one fails the fuzz build, and a tmpfs anywhere else is a
			// writable path in a container that needs none.
			name:     "another size on the fuzz container's tmpfs",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTmpfs, "--tmpfs /scratch:rw,exec,nosuid,size=8g,mode=1777"),
			wantIn:   "tmpfs",
		},
		{
			name:     "a tmpfs on the hostile inventories' container",
			workflow: mutate(t, acceptedWorkflow, auditHostileCreateFlags, " "+auditFuzzTmpfs+auditHostileCreateFlags),
			wantIn:   "tmpfs",
		},
		{
			name:     "a --privileged container",
			workflow: mutate(t, acceptedWorkflow, auditHostileCreateFlags, " --privileged"+auditHostileCreateFlags),
			wantIn:   "--privileged",
		},
		{
			name:     "a container with a host network",
			workflow: mutate(t, acceptedWorkflow, auditHostileCreateFlags, " --network host"+auditHostileCreateFlags),
			wantIn:   "--network host",
		},
		{
			name:     "a bind mount into a container",
			workflow: mutate(t, acceptedWorkflow, auditHostileCreateFlags, " -v /etc:/host"+auditHostileCreateFlags),
			wantIn:   "-v",
		},
		{
			name:     "a --volume into a container",
			workflow: mutate(t, acceptedWorkflow, auditHostileCreateFlags, " --volume /etc:/host"+auditHostileCreateFlags),
			wantIn:   "--volume",
		},
		{
			name:     "a --mount into a container",
			workflow: mutate(t, acceptedWorkflow, auditHostileCreateFlags, " --mount type=bind,source=/etc,target=/host"+auditHostileCreateFlags),
			wantIn:   "--mount",
		},
		{
			name:     "the Docker socket in a container",
			workflow: mutate(t, acceptedWorkflow, auditHostileCreateFlags, " -v /var/run/docker.sock:/var/run/docker.sock"+auditHostileCreateFlags),
			wantIn:   "docker.sock",
		},
		{
			name:     "a docker login",
			workflow: mutate(t, acceptedWorkflow, auditImagePull, "          docker login ghcr.io -u nobody --password-stdin <<< \"$(command -v true)\"\n"+auditImagePull),
			wantIn:   "docker login",
		},
		{
			name:     "the fuzz image pinned by tag alone",
			workflow: mutate(t, acceptedWorkflow, auditGoImage, "          GO_IMAGE: \"1.27.4-alpine\"\n"),
			wantIn:   "GO_IMAGE",
		},

		// The four guards, each held whole: removed, with its exit 1 dropped or
		// with its branch run together onto the echo's line, it refuses nothing.
		{
			name:     "row 7's tag shape guard removed",
			workflow: mutate(t, acceptedWorkflow, auditReleaseTagGuard, ""),
			wantIn:   "tag's shape",
		},
		{
			name:     "row 7's tag shape guard without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditReleaseTagGuard, strings.Replace(auditReleaseTagGuard, "            exit 1\n", "", 1)),
			wantIn:   "tag's shape",
		},
		{
			name:     "row 7's tag shape guard joined onto the echo's line",
			workflow: mutate(t, acceptedWorkflow, auditReleaseTagGuard, joinedBranch(auditReleaseTagGuard)),
			wantIn:   "tag's shape",
		},
		{
			name:     "row 8's digest guard removed",
			workflow: mutate(t, acceptedWorkflow, auditImagePullBranch+auditDigestGuard, auditImagePullBranch),
			wantIn:   "image digest",
		},
		{
			name:     "row 11's digest guard removed",
			workflow: mutate(t, acceptedWorkflow, auditImageRuntimeBranch+auditDigestGuard, auditImageRuntimeBranch),
			wantIn:   "image digest",
		},
		{
			name:     "row 13's digest guard removed",
			workflow: mutate(t, acceptedWorkflow, auditHostileNotPulledBranch+auditDigestGuard, auditHostileNotPulledBranch),
			wantIn:   "image digest",
		},
		{
			name:     "the digest guards without their exit 1",
			workflow: mutateEvery(t, acceptedWorkflow, auditDigestGuard, strings.ReplaceAll(auditDigestGuard, "            exit 1\n", "")),
			wantIn:   "image digest",
		},
		{
			name:     "the digest guards joined onto the echo's line",
			workflow: mutateEvery(t, acceptedWorkflow, auditDigestGuard, joinedBranch(auditDigestGuard)),
			wantIn:   "image digest",
		},
		{
			name:     "the fuzz target's shape guard removed",
			workflow: mutate(t, acceptedWorkflow, auditFuzzShapeGuard, ""),
			wantIn:   "fuzz target",
		},
		{
			name:     "the fuzz target's shape guard without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditFuzzShapeGuard, strings.Replace(auditFuzzShapeGuard, "              exit 1\n", "", 1)),
			wantIn:   "fuzz target",
		},
		{
			name:     "the fuzz target's shape guard joined onto the echo's line",
			workflow: mutate(t, acceptedWorkflow, auditFuzzShapeGuard, joinedBranch(auditFuzzShapeGuard)),
			wantIn:   "fuzz target",
		},
		{
			name:     "the ruleset ID guard removed",
			workflow: mutate(t, acceptedWorkflow, auditRulesetGuard, ""),
			wantIn:   "ruleset ID",
		},
		{
			name:     "the ruleset ID guard without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditRulesetGuard, strings.Replace(auditRulesetGuard, "              exit 1\n", "", 1)),
			wantIn:   "ruleset ID",
		},
		{
			name:     "the ruleset ID guard joined onto the echo's line",
			workflow: mutate(t, acceptedWorkflow, auditRulesetGuard, joinedBranch(auditRulesetGuard)),
			wantIn:   "ruleset ID",
		},

		// The three tools PR 5c adds are pinned like the rest: a version under a
		// Renovate comment, and a SHA-256 beside it.
		{
			name:     "scorecard's version without a Renovate comment",
			workflow: mutate(t, acceptedWorkflow, "          # renovate: datasource=github-releases depName=ossf/scorecard\n          SCORECARD_VERSION: v5.6.0\n", "          SCORECARD_VERSION: v5.6.0\n"),
			wantIn:   "renovate",
		},
		{
			name:     "trivy's SHA-256 removed",
			workflow: mutate(t, acceptedWorkflow, "          # renovate: datasource=github-release-attachments depName=aquasecurity/trivy digestVersion=v0.76.2\n          TRIVY_SHA256: \"bc0fc726e017b8927a62b2d3531d7a84ee01769daf5c99ed866dced54089fdbf\"\n", ""),
			wantIn:   "SHA-256",
		},
		{
			// Scorecard's report belongs in the results directory, never on the
			// run's public log, and the CLI publishes nothing.
			name:     "scorecard publishing its result",
			workflow: mutate(t, acceptedWorkflow, auditScorecardCommand, strings.Replace(auditScorecardCommand, "scorecard --repo", "scorecard --publish --repo", 1)),
			wantIn:   "--publish",
		},
		{
			name:     "scorecard's report on the log instead of the results directory",
			workflow: mutate(t, acceptedWorkflow, auditScorecardCommand, strings.Replace(auditScorecardCommand, " > \"${out}/scorecard.json\"", "", 1)),
			wantIn:   "scorecard.json",
		},

		// Every Go command after the module download runs offline, the binary
		// govulncheck of row 9 and the fuzz container's go test included.
		{
			name:     "the binary govulncheck without its offline flags",
			workflow: mutate(t, acceptedWorkflow, auditBinaryGovulncheck, "timeout 5m govulncheck -mode=binary"),
			wantIn:   "GOFLAGS",
		},
		{
			name:     "the fuzz container without --env GOPROXY",
			workflow: mutate(t, acceptedWorkflow, auditFuzzEnvHandoff, "--env GOFLAGS --env GOTOOLCHAIN=local"),
			wantIn:   "GOPROXY",
		},
		{
			name:     "the fuzz step without its GOPROXY env",
			workflow: mutate(t, acceptedWorkflow, "          GOPROXY: \"off\"\n", ""),
			wantIn:   "GOPROXY",
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
