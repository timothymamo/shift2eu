package check_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

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
//     stops Docker, removes the stale Docker socket file the stopped
//     docker.socket leaves on the runner image, and removes the runner user's
//     sudo rule runs before the step that starts ci/auditrun, which runs the
//     sandbox preflight and the audit.
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
// The two causes left unnamed after PR 5c, which PRD #175's "A failed run is
// visible" wants named:
//
//   - Every tool download's checksum check is held whole: the pin is compared
//     with sha256sum under an if !, the branch's echo names the tool, its job
//     and the runbook step that puts it right, and the exit 1 fails the step.
//     Bare, the step fails with sha256sum's own line alone, which names no
//     cause. The three download steps, the checks job's, the model job's and
//     the publish job's, hold one guard per download, none left bare: the two
//     artifacts' zips are no tool pin, and their digest check is held
//     separately.
//   - docker start is called on a run container alone, the one docker create
//     made with the four flags: the staging container that receives the inputs
//     by docker cp and becomes the image by docker commit is never started.
//
// Round E closes the rest of PRD #175's "A failed run is visible" and the
// workflow's own time budget:
//
//   - A verification that fails is a finding about the release, not a check
//     that did not run. Row 7's five verifications - the checksum check, the
//     attestation of each release file, the image's signature, its provenance
//     and SBOM attestations' signatures, and the SBOM's own attestation -
//     record one branch: rc 124 as release-integrity timed-out, rc 1 (a
//     mismatch, a refused attestation, a refused signature) as
//     release-integrity verification-failed in the results, and any other
//     failure as release-integrity tool-failed. Both records in not-run.txt
//     stay what they are, so the model reads a mismatch as the release's
//     finding and not as a check that never ran.
//   - No step's own timeouts can reach its timeout-minutes, because a step's
//     timeout firing skips every later step, the encrypt step included. A
//     loop's `${SECONDS}` budget stops only the iterations that have not begun,
//     so the step's bound is the larger of the timeouts before the loop and the
//     budget plus one iteration's own timeouts, plus the timeouts after the
//     loop, and a step with no loop holds the sum of its `timeout <N>` values.
//     Row 7's 20-minute budget plus the 2 minutes of the iteration it had
//     begun before that budget are 22, past the 11 minutes of timeouts before
//     the loop, so with the 18 minutes after the loop 40 of its 50, row 8's
//     three timeouts 14 of its 15, row 9's 12-minute budget plus one scan's
//     7 minutes 19 of its 20, row 13's 12-minute budget plus one run's 6
//     minutes and the image's removal 19 of its 20, and row 14's 25-minute
//     budget plus one target's 16 minutes and the image's removal 42 of its 45.
//     A shell function's timeout counts once per call, so a function the step
//     calls many times must hold a budget of its own, and then the step's bound
//     is that budget plus one call's timeouts: row 15's read_api is 12 minutes
//     plus one read's 2, 14 of its 15, however many reads it makes. The checks
//     job's steps add up to 348, with the job's 5 minutes of set-up and post
//     steps inside its 355.
//   - Every step that can fail with a cause the box lists ends with an
//     `::error title=Audit <job>::<kind>: see runbook step <N>` line on that
//     failure, naming no secret and no finding's detail: the hardening step's
//     own commands, the model step that runs the launcher (an expired or
//     revoked token, the subscription's usage limit, a failed preflight and a
//     managed-settings path all read alike from its one constant line, so that
//     step names the class and only a real run tells them apart), the two
//     artifact steps (an unexpected artifact) and the publish step (a refused
//     publish). The three download steps' checksums, round D's guards, are the
//     box's tool-checksum cause and stay as they are.
//
// Round K's fixes, and what the checks job's own records mean:
//
//   - A check of several parts is no one verdict. Rows 7, 13 and 14 set
//     `ran_any=0` before their first part, mark `ran_any=1` right after each
//     part's exit is recorded (comments aside), and record the check as one that
//     did not run only when the flag is still 0 at the step's end:
//     `if [ "${ran_any}" -eq 0 ]; then echo "<check> tool-failed" >>
//     "${results}/not-run.txt"; fi`. A part that timed out or failed is a
//     `limits.txt` line of that check with its `.exit` file, inside a loop or
//     outside it, never a `not-run.txt` line: a run that hung the CLI, a target
//     that timed out and a release file whose attestation failed are results the
//     model reads, and the skill reads a check with any finding, and any
//     verification-failed.txt line, as one that ran. Row 7's unresolved image
//     digest is the same: a limits line, never "not run". The step-level
//     download, pull and staging failures before any part ran keep their
//     not-run records, since no part produced a result yet - row 7's download
//     only at the step's end, its why in a shell variable (round L).
//   - Every `not-run.txt` line of the checks job ends in a why id the publish
//     core's `whyNotRun` holds (ci/auditpublish/internal/publish/vocabulary.go),
//     and the check's own list of them is pinned equal to that map. A part the
//     release or the API answers in another shape - a ruleset ID that is no
//     number, a fuzz target of another shape - is a limits line, never a why.
//   - A checksums.txt that is not empty and holds no well-formed line is a
//     finding about the release: `release-integrity verification-failed` with
//     the limits line that says why, never a tool that failed.
//   - Row 15's budget skips go to `read-skipped.txt` with their own limits line,
//     never to `read-refused.txt`: nothing refused them, and the runbook sends
//     the maintainer to a permission only for a refused read.
//   - fit refuses a timeout it cannot read as whole minutes or seconds
//     (`timeout 1h`, `timeout -k 5 50m`, `timeout 1.5m`), a loop whose header it
//     cannot follow (a body on the header's own line, `do` on a line of its
//     own), an unbudgeted inner loop with a timeout inside a budgeted one, and a
//     timeout a command runs once per argument (`xargs`, `find -exec`).
//
// Round L's fixes, and what a part of a check that ran means:
//
//   - Row 7's download is no whole verdict either: its branches record a
//     `limits.txt` line and the why in a shell variable (`why=timed-out`,
//     `why=tool-failed`, `why=results-missing`), and the step's end writes the
//     one not-run line, `echo "release-integrity ${why:-tool-failed}" >>
//     "${results}/not-run.txt"`, only when `ran_any` is still 0. The image
//     half runs whatever the download did, so a download that failed beside a
//     verification-failed.txt line is that finding, never also a check that did
//     not run. A not-run.txt line inside a step's body is refused unless it is
//     right before that step's `exit 0` (a step-level failure before any part
//     ran, as rows 13 and 14 have).
//   - A run or a target counts as a part that ran only once it started: rows 13
//     and 14 set `ran_any=1` after the create's `continue` guard, so a create
//     that failed counts for nothing, and right after the start's exit is
//     recorded, where a run that began is a part of the check whatever its
//     exit. `ran_any=1` right after a `.create.exit` is refused.
//   - fit follows a loop word after `&&` or `;` as well (a header there is one it
//     must read or refuse), and counts a timeout inside a quoted `sh -c`
//     string: `sh -c 'timeout 50m true'` and
//     `find ... -exec sh -c 'timeout 50m true' \;` are timeouts of the step.
//     A timeout `xargs` or `find -exec`/`-execdir` runs once per argument is
//     refused whatever comes between the runner and the timeout.
//   - Row 13's finding-candidate limits line is held whole, as row 7's malformed
//     checksums record is.
//   - The check's list of why ids and the publish core's `whyNotRun` are pinned
//     equal in both directions.
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
	auditChecksIf         = "    timeout-minutes: 355\n    if: ${{ " + auditGuard + " }}\n"
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
	// command after the module download must carry, under row 5's own timeout and
	// with its exit recorded.
	auditActionlint = "GOFLAGS=-mod=readonly GOPROXY=off timeout 5m go -C tools run github.com/rhysd/actionlint/cmd/actionlint ../.github/workflows > \"${out}/actionlint.json\" 2> \"${out}/actionlint.stderr\" || rc=$?\n"
	// auditWrapperBuild is the model job's own Go command, the wrapper's build.
	auditWrapperBuild = "GOFLAGS=-mod=readonly GOPROXY=off go build -trimpath -o \"${RUNNER_TEMP}/audit-exec\" ./ci/auditexec\n"
	// auditWrapperInstall is the model job's installation of the wrapper, root-owned
	// with mode 0555 beside the /srv/audit/tools directory its other tools live in
	// (finding M1 of issue #614's review): the anchor of the cases that install a
	// tool somewhere the runner's user can write.
	auditWrapperInstall = "          sudo install -o root -g root -m 0555 \"${RUNNER_TEMP}/audit-exec\" /srv/audit/bin/audit-exec\n"
	// auditModelSocat is the model job's own dpkg line, the anchor of the case that
	// writes a $GITHUB_PATH entry in the model job: the install step's lines are
	// shared with the checks job, and this one is the model job's alone.
	auditModelSocat = "          sudo dpkg -i \"${RUNNER_TEMP}/socat.deb\"\n"
	// auditModelArtifactsCommand is the model job's artifact check, the one command
	// of the job that runs a program built from main before the model session: the
	// anchor of the cases that run it through a path the shell resolves.
	auditModelArtifactsCommand = "          if ! ci/auditpublish artifacts -job model -gh \"$(command -v gh)\" -tmp \"${RUNNER_TEMP}\"; then\n"
	// auditHardeningStep is the model-free step that hardens the runner: it must
	// run before the sandbox preflight, which is inside the model step, and it
	// records each of its own commands' failure, so that a run fails with an
	// ::error naming the runbook step instead of with set -e's own line. It
	// removes the stale Docker socket file while sudo still works: stopping
	// docker.socket leaves the file on the runner image (issue #574).
	auditHardeningStep = "      - name: Stop Docker and remove the runner user's sudo rule\n" +
		"        timeout-minutes: 5\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          rc=0\n" +
		auditDockerStop +
		"          sudo systemctl disable docker.service docker.socket || rc=$?\n" +
		auditDockerSocketRemoval +
		auditSudoRuleRemoval +
		"          if [ \"${rc}\" -ne 0 ]; then\n" +
		"            echo \"::error title=Audit model::the runner could not be hardened: see runbook step 10\"\n" +
		"            exit 1\n" +
		"          fi\n" +
		"          if sudo -n true 2> /dev/null; then\n" +
		"            echo \"::error title=Audit model::sudo still works: see runbook step 10\"\n" +
		"            exit 1\n" +
		"          fi\n" +
		auditDockerSocketCheck +
		auditDockerActiveCheck
	// auditDockerStop is the hardening step's stopping of Docker, the line the
	// case that moves the daemon check above the stop anchors on.
	auditDockerStop = "          sudo systemctl stop docker.service docker.socket || rc=$?\n"
	// auditDockerSocketRemoval is the hardening step's removal of the stale
	// Docker socket file, which stopping docker.socket leaves behind on the
	// runner image, and auditSudoRuleRemoval the removal of the runner user's
	// sudo rule that follows it (issue #574, ADR 0040). The socket file is
	// removed while sudo still works, and the existence check after both is what
	// still refuses a socket that is there.
	auditDockerSocketRemoval = "          sudo rm -f /var/run/docker.sock || rc=$?\n"
	auditSudoRuleRemoval     = "          sudo rm -f /etc/sudoers.d/runner || rc=$?\n"
	// auditDockerSocketCheck is the hardening step's refusal of a Docker socket
	// still there after the step removed the file, and auditDockerActiveCheck its
	// refusal of a daemon still running: the removal of the file no longer shows
	// the daemon stopped, so the step asks systemd (issue #574, ADR 0040). Both
	// come after the removals and before the sandbox preflight.
	auditDockerSocketCheck = "          if [ -e /var/run/docker.sock ]; then\n" +
		"            echo \"::error title=Audit model::the Docker socket is still there: see runbook step 10\"\n" +
		"            exit 1\n" +
		"          fi\n"
	auditDockerActiveCheck = "          if systemctl is-active --quiet docker.service docker.socket; then\n" +
		"            echo \"::error title=Audit model::Docker is still running: see runbook step 10\"\n" +
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
	// job's token, and each run is under a timeout of its own that records the
	// check "not run" (rows 3 to 5).
	auditZizmorStep = "      - name: zizmor\n" +
		"        timeout-minutes: 10\n" +
		"        env:\n" +
		"          # The one read this row makes of GitHub's API.\n" +
		"          GH_TOKEN: ${{ github.token }}\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          results=\"${RUNNER_TEMP}/results\"\n" +
		"          out=\"${results}/workflows\"\n" +
		"          mkdir -p \"${out}\"\n" +
		"          rc=0\n" +
		"          timeout 4m zizmor --format json .github/workflows > \"${out}/zizmor.json\" 2> \"${out}/zizmor.stderr\" || rc=$?\n" +
		"          echo \"${rc}\" > \"${out}/zizmor.exit\"\n" +
		"          if [ \"${rc}\" -eq 124 ]; then\n" +
		"            echo \"workflows timed-out\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"workflows: the zizmor run timed out after 4 minutes\" >> \"${results}/limits.txt\"\n" +
		"          elif [ \"${rc}\" -ne 0 ] && { [ \"${rc}\" -lt 11 ] || [ \"${rc}\" -gt 14 ]; }; then\n" +
		"            echo \"workflows tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"          fi\n"
	auditActionlintStep = "      - name: actionlint\n" +
		"        timeout-minutes: 10\n" +
		"        run: |\n" +
		"          set -euo pipefail\n" +
		"          results=\"${RUNNER_TEMP}/results\"\n" +
		"          out=\"${results}/workflows\"\n" +
		"          mkdir -p \"${out}\"\n" +
		"          rc=0\n" +
		"          " + auditActionlint +
		"          echo \"${rc}\" > \"${out}/actionlint.exit\"\n" +
		"          if [ \"${rc}\" -eq 124 ]; then\n" +
		"            echo \"workflows timed-out\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"workflows: the actionlint run timed out after 5 minutes\" >> \"${results}/limits.txt\"\n" +
		"          elif [ \"${rc}\" -ne 0 ] && [ \"${rc}\" -ne 1 ]; then\n" +
		"            echo \"workflows tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"          fi\n"
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
	// checked result under $RUNNER_TEMP. Round E wraps it in the guard that
	// names the runbook step a failed run is recovered by, so the case that
	// reads the line is the guard's condition, which holds the command.
	auditRunCommand = "          if ! ci/auditrun -claude-tarball \"${RUNNER_TEMP}/claude-code/claude-code.tgz\" -claude-integrity \"${CLAUDE_CODE_PIN_INTEGRITY}\" -result-out \"${RUNNER_TEMP}/result.json\"; then\n"
	// auditEncryptResultCommand is the encrypt step's age command: it reads the
	// file the launcher wrote, never the result's path under /srv/audit.
	auditEncryptResultCommand = "          /srv/audit/tools/age --encrypt --recipient \"${AUDIT_PUBLISH_PUBLIC_KEY}\" --output \"${RUNNER_TEMP}/model-result.age\" \"${RUNNER_TEMP}/result.json\"\n"
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
	// auditPublishCommand is the publish step's own command line, the line the
	// step that must come before it is inserted at, wrapped in round E's guard
	// that names the runbook step a refused publish is recovered by.
	auditPublishCommand = "          if ! ci/auditpublish publish -result \"${RUNNER_TEMP}/result.json\" -reports docs/security/audits -gh \"$(command -v gh)\" -gitleaks \"$(command -v gitleaks)\" -tmp \"${RUNNER_TEMP}\"; then\n"
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
		"          elif [ \"${rc}\" -ne 0 ]; then\n" +
		"            echo \"scorecard tool-failed\" >> \"${results}/not-run.txt\"\n" +
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
	// auditRow11Create and the two flag groups below are row 11's, row 13's and
	// row 14's own docker create lines, each unique to its row: the anchors of
	// the cases that take a flag out of one row alone, since the four flags are
	// the same text in all three.
	auditRow11Create = "cid=\"$(timeout 2m docker create --read-only --network none --cap-drop ALL --security-opt no-new-privileges --memory 512m --pids-limit 64 \"${image}@${digest}\" version)\" || rc=$?"
	auditRow13Create = " --security-opt no-new-privileges --memory 512m --pids-limit 64 \"audit-hostile:${GITHUB_RUN_ID}\" assess "
	auditRow14Create = " --network none --cap-drop ALL --security-opt no-new-privileges --memory 10g --pids-limit 512 --user 65534:65534"
	// auditHostileCreateFlags is row 13's container's own limits and the image it
	// runs, which no other docker create holds: the anchor the cases that put a
	// flag, a mount or a tmpfs into a container use.
	auditHostileCreateFlags = " --memory 512m --pids-limit 64 \"audit-hostile:${GITHUB_RUN_ID}\" assess "
	// auditFuzzTmpfs is row 14's one tmpfs, exactly the size, mode and flags its
	// go test needs: the only tmpfs any container of the workflow has.
	auditFuzzTmpfs = "--tmpfs /scratch:rw,exec,nosuid,size=4g,mode=1777"
	// auditGoImage is row 14's Go image, pinned by tag and @sha256: digest.
	auditGoImage = "          GO_IMAGE: \"1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280\"\n"
	// auditImagePull is row 8's pull, the line the case that logs in to the
	// registry hangs its docker login on.
	auditImagePull = "          timeout 8m docker pull --platform linux/amd64 \"${image}@${digest}\" > \"${out}/pull.stdout\" 2> \"${out}/pull.stderr\" || rc=$?\n"
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
	auditFuzzShapeGuard = "            if [[ ! \"${name}\" =~ ^Fuzz[A-Z0-9_][A-Za-z0-9_]*$ ]] || [[ ! \"${pkg}\" =~ ^[A-Za-z0-9_][A-Za-z0-9_./-]*$ ]] || [[ \"${pkg}\" == *..* ]]; then\n" +
		"              echo \"fuzzing: a target of an unexpected shape was not run\" >> \"${results}/limits.txt\"\n" +
		"              continue\n" +
		"            fi\n"
	// The ruleset ID's shape is refused by a limits line and a continue, never
	// by exit 1 and never by a not-run record: a read the API answers in another
	// shape records that read in the limits and leaves the step to its other
	// reads (round I of PR 5c; a why id no vocabulary holds, round K).
	auditRulesetGuard = "            if [[ ! \"${id}\" =~ ^[0-9]+$ ]]; then\n" +
		"              echo \"supply-chain: a ruleset ID that is no number was not read\" >> \"${results}/limits.txt\"\n" +
		"              continue\n" +
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
	// auditHostileStaging and auditFuzzStaging are rows 13 and 14's staging
	// containers, each created with no flag and no command: docker cp copies the
	// inputs into it and docker commit turns it into the image the row's runs
	// start from, so it is never started itself. A case that starts one hangs a
	// docker start on its create.
	auditHostileStaging = "          staging=\"$(timeout 1m docker create \"${image}@${digest}\")\" || rc=$?\n"
	auditFuzzStaging    = "          staging=\"$(timeout 2m docker create \"${image}\")\" || rc=$?\n"
	// auditHostileStagingCreate is the head of row 13's staging create: the one
	// docker create of the checks job whose flags the check does not pin
	// verbatim, so a case that hangs a flag on it is caught by the rule about
	// what no container is given alone.
	auditHostileStagingCreate = "          staging=\"$(timeout 1m docker create "

	// Round D's anchors (the review's and the audit's findings of PR 5c): the
	// lines and blocks of the accepted shape a case changes to show that the
	// check holds them. Each is a line of its own, exactly as the fixture writes
	// it, so a case that mistypes one fails in mutate.
	//
	// A release file, an archive or the image is the untrusted party: every
	// command that reads one runs under a timeout and records a failure of its
	// own, never as a step that aborts the run's report.
	auditChecksumFilter  = "            timeout 1m grep -E '^[0-9a-f]{64}  [A-Za-z0-9._+-]+$' \"${rel}/checksums.txt\" > \"${out}/checksums.filtered\" 2> \"${out}/checksums-filter.stderr\" || rc=$?\n"
	auditReleaseChecksum = "            (cd \"${rel}\" && timeout 5m sha256sum --check --strict \"${out}/checksums.filtered\") > \"${out}/checksums.stdout\" 2> \"${out}/checksums.stderr\" || rc=$?\n"
	// auditReleaseLoopBudget is row 7's attestation loop's overall budget, in
	// seconds of the step, which its timeout-minutes (50) has to be above.
	auditReleaseLoopBudget = "              if [ \"${SECONDS}\" -ge 1200 ]; then\n"
	// auditBinaryUnpack is row 9's unpack: the one member, to stdout, through a
	// size cap, under a timeout.
	auditBinaryUnpack = "              *.tar.gz) timeout 2m tar -xOzf \"${archive}\" shift2eu 2> \"${out}/${name}.unpack.stderr\" | head -c 134217728 > \"${dir}/shift2eu\" || rc=$? ;;\n"
	// auditRuntimeExport is row 11's export and listing: under a timeout, with
	// the image's own tar's stderr out of the log.
	auditRuntimeExport = "          timeout 4m docker export \"${cid}\" 2> \"${out}/export.stderr\" | tar -tvf - > \"${out}/filesystem.txt\" 2> \"${out}/filesystem.stderr\" || rc=$?\n"
	// auditRuntimeInspect is row 11's read of the image's configuration.
	auditRuntimeInspect = "          timeout 1m docker image inspect --format '{{json .Config}}' \"${image}@${digest}\" > \"${out}/config.json\" 2> \"${out}/config.stderr\" || rc=$?\n"
	// auditImageSave is row 8's archive of the pulled image, and
	// auditBinaryUnpackZip row 9's zip member.
	auditImageSave       = "          timeout 4m docker save --output \"${RUNNER_TEMP}/image/image.tar\" \"${image}@${digest}\" 2> \"${out}/save.stderr\" || rc=$?\n"
	auditBinaryUnpackZip = "              *.zip) timeout 2m unzip -p \"${archive}\" shift2eu.exe 2> \"${out}/${name}.unpack.stderr\" | head -c 134217728 > \"${dir}/shift2eu.exe\" || rc=$? ;;\n"
	// auditProvenanceVerify and auditCosignVerify are row 7's reads of the
	// release's attestations and its signature.
	auditProvenanceVerify = "              timeout 2m gh attestation verify \"${file}\" \\\n"
	auditCosignVerify     = "            timeout 3m cosign verify \"${ref}\" \\\n"
	// auditHostileCopy and auditHostileCommit are row 13's staging copy and
	// commit, each under a timeout of its own.
	auditHostileCopy   = "          timeout 1m docker cp \"${stage}\" \"${staging}:/work/\" 2> \"${out}/staging-cp.stderr\" || rc=$?\n"
	auditHostileCommit = "          timeout 1m docker commit \"${staging}\" \"audit-hostile:${GITHUB_RUN_ID}\" > \"${out}/staging-commit.stdout\" 2> \"${out}/staging-commit.stderr\" || rc=$?\n"
	// auditCorpusCopy is row 13's copy of the committed corpus: -P, so a link an
	// entry of it is never followed on the runner.
	auditCorpusCopy = "          cp -P .github/security-audit/testdata/hostile-inventories/*.json \"${stage}/\"\n"
	// auditFuzzDiscovery is row 14's reading of the tracked files: the signature
	// alone, so Fuzzy( and FuzzHelper( are no targets.
	auditFuzzDiscovery = "            { grep -oE '^func Fuzz[A-Z0-9_][A-Za-z0-9_]*\\(f \\*testing\\.F\\)' \"${file}\" || true; } | sed -e 's/^func //' -e 's/(f \\*testing\\.F)$//' | while IFS= read -r name; do\n"
	// auditTrivyOffline is row 10's offline flag set, which both scans use.
	auditTrivyOffline = "          offline=(--cache-dir \"${RUNNER_TEMP}/trivy-cache\" --skip-db-update --skip-java-db-update --skip-check-update --offline-scan --skip-version-check --disable-telemetry --no-progress --scanners vuln --format json)\n"
	// auditReadAPI and auditReadAPIFilter are row 15's one read: its endpoint's
	// answer through the jq filter its caller names, and the filter's default.
	auditReadAPI       = "            timeout 2m gh api \"${endpoint}\" --jq \"${filter}\" > \"${out}/api/${name}.json\" 2> \"${out}/api/${name}.stderr\" || rc=$?\n"
	auditReadAPIFilter = "            local name=\"$1\" endpoint=\"$2\" filter=\"${3:-.}\" rc=0\n"
	// auditReadAPIBudget is read_api's budget, at the head of its body (round J of
	// PR 5c): the step's reads number more than 25, each of 2 minutes, far past
	// its 15, so no read starts after its 12th minute. The reads it did not reach
	// are recorded in read-skipped.txt, which the step's end names in the limits
	// (round K: the budget ran out on them, the API refused nothing).
	auditReadAPIBudget = "            # The function's budget: the step's reads number more than 25, each of 2 minutes, far past\n" +
		"            # its 15, so no read starts after 12 minutes of it. The reads it did not reach are recorded in\n" +
		"            # read-skipped.txt, never in read-refused.txt: the budget ran out on them, the API refused\n" +
		"            # nothing, and the step's end names each list in the limits by its own cause.\n" +
		"            if [ \"${SECONDS}\" -ge 720 ]; then\n" +
		"              echo \"${name}\" >> \"${out}/read-skipped.txt\"\n" +
		"              return 0\n" +
		"            fi\n"
	// The four reads that keep names alone: the two variables endpoints and the
	// two secrets endpoints return each entry's value or nothing, so each call
	// passes the filter that keeps the names.
	auditRepositoryVariableNames  = "          read_api repository-variable-names \"repos/${GH_REPO}/actions/variables\" '[.variables[].name]'\n"
	auditEnvironmentVariableNames = "            read_api \"environment-${env}-variable-names\" \"repos/${GH_REPO}/environments/${env}/variables\" '[.variables[].name]'\n"
	auditRepositorySecretNames    = "          read_api repository-secret-names \"repos/${GH_REPO}/actions/secrets\" '[.secrets[].name]'\n"
	auditEnvironmentSecretNames   = "            read_api \"environment-${env}-secret-names\" \"repos/${GH_REPO}/environments/${env}/secrets\" '[.secrets[].name]'\n"

	// The five checks job's rows that read release bytes record a failure of
	// theirs as a check that did not run, not as a step that stops the job. Row
	// 7's download records it at the step's end, from the why its branch sets
	// (round L's K1), and rows 8, 9, 11 and 13 in the branch that failed.
	auditReleaseDownloadTimedOutWhy   = "            why=timed-out\n"
	auditReleaseDownloadToolFailedWhy = "            why=tool-failed\n"
	auditPullToolFailed               = "echo \"image-pull tool-failed\" >> \"${results}/not-run.txt\""
	auditBinaryToolFailed             = "echo \"binary-vulnerabilities tool-failed\" >> \"${results}/not-run.txt\""
	auditRuntimeTimedOut              = "echo \"image-runtime timed-out\" >> \"${results}/not-run.txt\""
	auditHostileTimedOut              = "echo \"hostile-inventories timed-out\" >> \"${results}/not-run.txt\""
)

// Round E's anchors (PRD #175, "A failed run is visible", and the workflow's own
// time budget): the accepted shape's own verification outcome branches, and the
// ::error lines it adds to every step that can fail with one of the box's
// causes. Each is exactly as the fixture writes it, so a case that mistypes one
// fails in mutate.
var (
	// Row 7's five verifications record one shape of outcome: their exit, then
	// the part marked as one that ran (ran_any, round K), the release's refusal - a
	// checksum mismatch, a refused attestation or a refused signature - as the
	// finding verification-failed in the results, and a timeout or any other
	// failure as a limits line of this check, never as the check that did not run.
	// The checksum check's finding is rc 1 only when the filtered list held
	// well-formed lines, and cosign's signature check reads cosign's own refusal
	// exits, 10 to 13, as the same finding (round I of PR 5c).
	auditChecksumOutcome = auditVerificationBranchWhen("            ", `[ "${rc}" -eq 1 ] && [ -s "${out}/checksums.filtered" ]`,
		"the checksum check timed out after 5 minutes", "the checksum check failed with exit ${rc}")
	auditReleaseFileOutcome = auditVerificationBranch("              ", "the attestation of ${name} timed out after 2 minutes",
		"the attestation of ${name} failed with exit ${rc}")
	auditSignatureOutcome = auditVerificationBranchWhen("            ", `[ "${rc}" -eq 1 ] || { [ "${rc}" -ge 10 ] && [ "${rc}" -le 13 ]; }`,
		"the image's signature timed out after 3 minutes", "the image's signature check failed with exit ${rc}")
	auditAttestedSigOutcome = auditVerificationBranch("              ", "the ${kind} attestation's signature timed out after 3 minutes",
		"the ${kind} attestation's signature check failed with exit ${rc}")
	auditSBOMOutcome = auditVerificationBranch("            ", "the SBOM's attestation timed out after 3 minutes",
		"the SBOM's attestation failed with exit ${rc}")

	// The five ::error lines round E adds, one per step that can fail with a
	// cause PRD #175's box lists: the hardening step's own commands, the model
	// step that runs the launcher (an expired or revoked token, the usage
	// limit, a failed preflight and a managed-settings path cannot be told
	// apart from its one line), the two artifact steps (an unexpected artifact)
	// and the publish step (a refused publish).
	auditHardeningError       = "            echo \"::error title=Audit model::the runner could not be hardened: see runbook step 10\"\n"
	auditRunError             = "            echo \"::error title=Audit model::the audit did not run: see runbook step 10\"\n"
	auditModelArtifactError   = "            echo \"::error title=Audit model::the run's artifacts are unexpected: see runbook step 11\"\n"
	auditPublishArtifactError = "            echo \"::error title=Audit publish::the run's artifacts are unexpected: see runbook step 11\"\n"
	auditPublishError         = "            echo \"::error title=Audit publish::the publish was refused: see runbook step 11\"\n"
)

// Round N's anchors (the AI review's points 4 and 10 on PR #565): row 15's
// Dependency Dashboard read and the limits line it writes when no such issue
// answered, and row 9's branch for an archive whose unpack failed. Each is
// exactly as the fixture writes it, so a case that mistypes one fails in
// mutate.
const (
	// auditRenovateDashboardRead is row 15's read of Renovate's Dependency
	// Dashboard: the open issues alone, with the pull requests filtered out, so
	// the dashboard - the oldest item creator=renovate[bot] answers with - cannot
	// fall off a first page of pull requests.
	auditRenovateDashboardRead = "          read_api renovate-dashboard \"repos/${GH_REPO}/issues?state=open&creator=renovate%5Bbot%5D&per_page=100\" '[.[] | select(.pull_request == null)]'\n"
	// auditRenovateDashboardMissing is row 15's limits line for a read that
	// succeeded and found no open dashboard issue at all: a silent empty answer
	// is said out loud, never read as a clean one.
	auditRenovateDashboardMissing = "          if [ -f \"${out}/api/renovate-dashboard.exit\" ] && [ \"$(cat \"${out}/api/renovate-dashboard.exit\")\" = 0 ] && [ \"$(jq 'length' \"${out}/api/renovate-dashboard.json\")\" = 0 ]; then\n" +
		"            echo \"supply-chain: no open Renovate Dependency Dashboard issue was found\" >> \"${results}/limits.txt\"\n" +
		"          fi\n"
	// auditBinaryUnpackFailure is row 9's branch for an archive whose unpack
	// failed: the timeout recorded as the check that timed out and any other
	// failure as one that did not run, a limits line naming the exit and what 141
	// (the 128 MiB cap) and 124 (the 2-minute timeout) stand for, and the continue
	// that keeps the part of a member head wrote out of the scan.
	auditBinaryUnpackFailure = "            if [ \"${rc}\" -ne 0 ]; then\n" +
		"              if [ \"${rc}\" -eq 124 ]; then\n" +
		"                echo \"binary-vulnerabilities timed-out\" >> \"${results}/not-run.txt\"\n" +
		"              else\n" +
		"                echo \"binary-vulnerabilities tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"              fi\n" +
		"              echo \"binary-vulnerabilities: ${name} was not scanned: its unpack exited ${rc} (141: the 128 MiB cap, 124: the 2-minute timeout)\" >> \"${results}/limits.txt\"\n" +
		"              continue\n" +
		"            fi\n"
)

// Round F's anchors (PRD #175, the box's last two causes): rows 3 to 5's tool
// runs, each of them under a timeout of its own, and the model job's install
// step's guard around the install script it shares with the AI reviewer. Each
// is exactly as the fixture writes it, so a case that mistypes one fails in
// mutate.
const (
	// auditGovulncheckRun, auditOSVScanRun, auditGitleaksRun and auditZizmorRun
	// are one run of each of rows 3 to 5's tools, whole lines: the tool under its
	// timeout with its exit recorded, so that a tool that hangs is the check that
	// timed out and the check that failed is recorded as not run. A case takes the
	// timeout out of one of them.
	auditGovulncheckRun = "          GOFLAGS=-mod=readonly GOPROXY=off timeout 8m govulncheck -format json ./... > \"${out}/module.json\" 2> \"${out}/module.stderr\" || rc=$?\n"
	auditOSVScanRun     = "          timeout 8m osv-scanner scan source --recursive . > \"${out}/repository.json\" 2> \"${out}/repository.stderr\" || rc=$?\n"
	auditGitleaksRun    = "          timeout 15m gitleaks git --ignore-gitleaks-allow --redact --no-banner --log-level error --exit-code 2 --report-format json --report-path \"${out}/history.json\" . 2> \"${out}/history.stderr\" || rc=$?\n"
	auditZizmorRun      = "          timeout 4m zizmor --format json .github/workflows > \"${out}/zizmor.json\" 2> \"${out}/zizmor.stderr\" || rc=$?\n"

	// auditGovulncheckOutcome, auditGitleaksOutcome and auditZizmorOutcome are
	// rows 3 and 5's tool-failed branches, with the line above each that makes it
	// that row's alone (the module run's limits line, and gitleaks's and zizmor's
	// own labels): the branch that decides which exit codes are the tool's
	// findings and which are the check that failed (round G of PR 5c).
	auditGovulncheckOutcome = "            echo \"dependencies: the module scan timed out after 8 minutes\" >> \"${results}/limits.txt\"\n" +
		"          elif [ \"${rc}\" -ne 0 ]; then\n"
	auditGitleaksOutcome = "            echo \"history-secrets: the history scan timed out after 15 minutes\" >> \"${results}/limits.txt\"\n" +
		"          elif [ \"${rc}\" -ne 0 ] && [ \"${rc}\" -ne 2 ]; then\n"
	auditZizmorOutcome = "            echo \"workflows: the zizmor run timed out after 4 minutes\" >> \"${results}/limits.txt\"\n" +
		"          elif [ \"${rc}\" -ne 0 ] && { [ \"${rc}\" -lt 11 ] || [ \"${rc}\" -gt 14 ]; }; then\n"

	// auditInstallCall is the model job's install step's call of the shared
	// install script, exactly as it ran before round F.
	auditInstallCall = "          .github/scripts/install-claude-code.sh \"${RUNNER_TEMP}/claude-code\" \"@anthropic-ai/claude-code-linux-x64@${CLAUDE_CODE_PIN_VERSION}\" \"${CLAUDE_CODE_PIN_INTEGRITY}\"\n"
)

var (
	// auditInstallError is the install guard's ::error line: the script itself,
	// shared with the AI reviewer, names no runbook step in either of its two
	// lines, so the workflow names the step that recovers both (a pin the pin
	// check holds, or a managed policy on the runner image).
	auditInstallError = "            echo \"::error title=Audit model::the Claude Code CLI did not install: see runbook step 11\"\n"
	// auditInstallGuard is the call under the whole-branch guard round F wraps it
	// in: the refusalBranch shape, the echo and the exit 1 each on a line of its
	// own. A case removes the branch, drops the exit 1 or runs the branch together
	// onto the echo's line.
	auditInstallGuard = "          if ! " + strings.TrimSuffix(strings.TrimPrefix(auditInstallCall, "          "), "\n") + "; then\n" +
		auditInstallError +
		"            exit 1\n" +
		"          fi\n"
)

// Round I's anchors (the reviewer's and the auditor's findings on PR 5c): the
// accepted shape's fixes, each exactly as the fixture writes it, so a case that
// mistypes one fails in mutate.
const (
	// auditSignatureFinding is row 7's cosign verify branch's finding condition,
	// widened to cosign's own refusal exits: v3 maps a missing signature, a
	// missing tag, no matching signature and no certificate on the signature to
	// 10, 11, 12 and 13, which are a finding about the release, never a tool that
	// failed (B1). auditSignatureFindingBefore is the branch that read only 1.
	auditSignatureFinding       = "            elif [ \"${rc}\" -eq 1 ] || { [ \"${rc}\" -ge 10 ] && [ \"${rc}\" -le 13 ]; }; then\n"
	auditSignatureFindingBefore = "            elif [ \"${rc}\" -eq 1 ]; then\n"

	// auditBinaryLoopBudget is row 9's loop budget: six archives of up to 2m tar
	// and 5m govulncheck are 42 minutes against the step's 20, so no archive's
	// scan starts after its 12th minute (M1).
	auditBinaryLoopBudget = "            if [ \"${SECONDS}\" -ge 720 ]; then\n" +
		"              echo \"binary-vulnerabilities: the budget of 12 minutes ran out before $(basename \"${archive}\")\" >> \"${results}/limits.txt\"\n" +
		"              continue\n" +
		"            fi\n"

	// auditHostileLoopBudget is row 13's run loop's budget: no run starts after
	// 12 minutes of the 20-minute step, so its last run's 6 minutes of timeouts
	// (create, start, kill, inspect and rm) and the image's removal end within it
	// (round J of PR 5c).
	auditHostileLoopBudget = "              if [ \"${SECONDS}\" -ge 720 ]; then\n" +
		"                echo \"hostile-inventories: the budget of 12 minutes ran out before ${name} as ${format}\" >> \"${results}/limits.txt\"\n" +
		"                continue\n" +
		"              fi\n"

	// auditRow8Pull is row 8's pull and the two lines around it: the block the
	// case that retries the pull in a loop with no budget replaces (M1's rule).
	auditRow8Pull = "          rc=0\n" +
		"          timeout 8m docker pull --platform linux/amd64 \"${image}@${digest}\" > \"${out}/pull.stdout\" 2> \"${out}/pull.stderr\" || rc=$?\n" +
		"          echo \"${rc}\" > \"${out}/pull.exit\"\n"

	// auditFuzzTree, auditFuzzStagingCopy and auditFuzzLoopBudget are row 14's
	// staging extraction, its copy of the tree into the staging container and its
	// loop budget: each command under a timeout of its own with its exit
	// recorded, and no target's fuzzing started after the step's 25th minute (M2).
	auditFuzzTree        = "          git archive --format=tar \"${GITHUB_SHA}\" | timeout 2m tar -C \"${src}\" -xf - 2> \"${out}/tree.stderr\" || rc=$?\n"
	auditFuzzStagingCopy = "          timeout 2m docker cp \"${src}\" \"${staging}:/src\" 2> \"${out}/staging-src.stderr\" || rc=$?\n"
	auditFuzzLoopBudget  = "            if [ \"${SECONDS}\" -ge 1500 ]; then\n" +
		"              echo \"fuzzing: the budget of 25 minutes ran out before ${pkg} ${name}\" >> \"${results}/limits.txt\"\n" +
		"              continue\n" +
		"            fi\n"

	// auditRuntimeState is row 11's read of the run container's exit state, under
	// a timeout of its own: the post-run docker commands are no less hangable
	// than the runs (L1).
	auditRuntimeState = "          timeout 1m docker inspect --format '{{.State.ExitCode}} {{.State.OOMKilled}}' \"${cid}\" > \"${out}/version.state\" || rc=$?\n"

	// auditReleaseFileGate and auditReleaseFileGateEnd are row 7's gate around
	// the release's file checks: they run only when the download brought the
	// files and checksums.txt with them, and its failure records a check that did
	// not run, never a checksum finding (L2).
	auditReleaseFileGate    = "          if [ \"${rel_ok}\" -eq 1 ]; then\n"
	auditReleaseFileGateEnd = "            done < <(find \"${rel}\" -maxdepth 1 -type f -print0)\n          fi\n"

	// auditLatestDigestShape and auditLatestReleaseShape are row 7's shape checks
	// of the two values latest.txt holds beside the tag: a registry answer or an
	// API answer of another shape is recorded as no value, never as one (L5).
	auditLatestDigestShape = "          if [[ ! \"${latest_digest}\" =~ ^sha256:[0-9a-f]{64}$ ]]; then\n" +
		"            latest_digest=\"\"\n" +
		"          fi\n"
	auditLatestReleaseShape = "          if [[ ! \"${latest_release}\" =~ ^v[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then\n" +
		"            latest_release=\"\"\n" +
		"          fi\n"

	// auditNoDigestRecord is row 7's else: an image digest it could not resolve
	// leaves the signature and the two attestations unverified, which is a limits
	// line about this check, never a check that did not run (S4, round K).
	auditNoDigestRecord = "          else\n" +
		"            # No digest is no verification: the image's signature and its two attestations were not\n" +
		"            # checked, which is a limits line about this check, never a check that did not run and never\n" +
		"            # a part of it that ran.\n" +
		"            echo \"release-integrity: no image digest, so the image's signature and attestations were not verified\" >> \"${results}/limits.txt\"\n" +
		"          fi\n"

	// auditScorecardToolFailed, auditImagePullToolFailed, auditTrivyImageToolFailed,
	// auditTrivySBOMToolFailed and auditTrivyImageTimedOut are rows 6, 8 and 10's
	// branches for a run that did not time out but failed: scorecard, trivy and
	// docker pull each exit non-zero only on an error, and a check that did not
	// run is recorded in not-run.txt, never left to read as clean (L3).
	auditScorecardToolFailed = "          elif [ \"${rc}\" -ne 0 ]; then\n" +
		"            echo \"scorecard tool-failed\" >> \"${results}/not-run.txt\"\n"
	auditImagePullToolFailed = "            echo \"image-pull timed out after 8 minutes\" >> \"${results}/limits.txt\"\n" +
		"          elif [ \"${rc}\" -ne 0 ]; then\n" +
		"            echo \"image-pull tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"          fi\n"
	auditTrivyImageToolFailed = "              echo \"image-scan: the image scan timed out after 8 minutes\" >> \"${results}/limits.txt\"\n" +
		"            elif [ \"${rc}\" -ne 0 ]; then\n" +
		"              echo \"image-scan tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"            fi\n"
	auditTrivySBOMToolFailed = "              echo \"image-scan: the SBOM scan timed out after 4 minutes\" >> \"${results}/limits.txt\"\n" +
		"            elif [ \"${rc}\" -ne 0 ]; then\n" +
		"              echo \"image-scan tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"            fi\n"
	auditTrivyImageTimedOut = "              echo \"image-scan timed-out\" >> \"${results}/not-run.txt\"\n" +
		"              echo \"image-scan: the image scan timed out after 8 minutes\" >> \"${results}/limits.txt\"\n"

	// auditRulesetExit is the ruleset ID's branch as it read before round I: a
	// shape it does not expect ends the whole step with exit 1, and so the run's
	// report (L6).
	auditRulesetExit = "            if [[ ! \"${id}\" =~ ^[0-9]+$ ]]; then\n" +
		"              echo \"::error title=Audit checks::a ruleset ID is no number: see runbook step 11\"\n" +
		"              exit 1\n" +
		"            fi\n"
)

// Round K's anchors (the reviewer's and the auditor's findings on PR 5c, round
// K): the accepted shape's per-part records (B1), its limits-only records for a
// part that is no whole check (B2), its malformed-checksums record (L4), its
// budget skips (S2), and the shapes the fit tripwire must refuse (L2, S1).
const (
	// The three end-of-step records: a check of several parts is written to
	// not-run.txt only when no part of it produced a result at all, which the
	// flag ran_any counts in shell.
	auditReleaseEndRecord = "          if [ \"${ran_any}\" -eq 0 ]; then\n" +
		"            echo \"release-integrity ${why:-tool-failed}\" >> \"${results}/not-run.txt\"\n" +
		"          fi\n"
	auditHostileEndRecord = "          if [ \"${ran_any}\" -eq 0 ]; then\n" +
		"            echo \"hostile-inventories tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"          fi\n"
	auditFuzzEndRecord = "          if [ \"${ran_any}\" -eq 0 ]; then\n" +
		"            echo \"fuzzing tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"          fi\n"

	// auditHostileRunCreate, auditHostileRunStart, auditFuzzTargetCreate and
	// auditFuzzTargetStart are rows 13 and 14's per-run and per-target outcomes
	// inside their loops, as round K writes them: the run's exit recorded, the part
	// marked as one that ran once it started (round L's K2, so the create's mark
	// follows its continue guard), and its timeout or failure in the limits, never
	// as the whole check that did not run.
	auditHostileRunCreate = "              echo \"${rc}\" > \"${out}/${run}.create.exit\"\n" +
		"              if [ \"${rc}\" -eq 124 ]; then\n" +
		"                echo \"hostile-inventories: the create for ${run} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"              elif [ \"${rc}\" -ne 0 ]; then\n" +
		"                echo \"hostile-inventories: the create for ${run} failed with exit ${rc}\" >> \"${results}/limits.txt\"\n" +
		"              fi\n"
	auditHostileRunStart = "              echo \"${rc}\" > \"${out}/${run}.exit\"\n" +
		"              ran_any=1\n" +
		"              if [ \"${rc}\" -eq 124 ]; then\n" +
		"                timeout 1m docker kill \"${cid}\" > /dev/null 2>&1 || true\n" +
		"                echo \"hostile-inventories: ${run} timed out after 60 seconds, a finding candidate of the run\" >> \"${results}/limits.txt\"\n" +
		"              fi\n"
	auditFuzzTargetCreate = "            echo \"${rc}\" > \"${out}/targets/${n}-${name}.create.exit\"\n" +
		"            if [ \"${rc}\" -eq 124 ]; then\n" +
		"              echo \"fuzzing: the create for ${pkg} ${name} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"            elif [ \"${rc}\" -ne 0 ]; then\n" +
		"              echo \"fuzzing: the create for ${pkg} ${name} failed with exit ${rc}\" >> \"${results}/limits.txt\"\n" +
		"            fi\n"
	auditFuzzTargetStart = "            echo \"${rc}\" > \"${out}/targets/${n}-${name}.exit\"\n" +
		"            ran_any=1\n" +
		"            if [ \"${rc}\" -eq 124 ]; then\n" +
		"              timeout 1m docker kill \"${cid}\" > /dev/null 2>&1 || true\n" +
		"              echo \"fuzzing: ${pkg} ${name} timed out after 12 minutes\" >> \"${results}/limits.txt\"\n" +
		"            fi\n"

	// auditHostileRunCreateWhole and auditFuzzTargetCreateWhole are those two
	// branches as the accepted shape wrote them before round K: a part's timeout
	// and failure written to not-run.txt as the whole check not running, which is
	// B1. A case puts one back.
	auditHostileRunCreateWhole = "              echo \"${rc}\" > \"${out}/${run}.create.exit\"\n" +
		"              if [ \"${rc}\" -eq 124 ]; then\n" +
		"                echo \"hostile-inventories timed-out\" >> \"${results}/not-run.txt\"\n" +
		"                echo \"hostile-inventories: the create for ${run} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"              elif [ \"${rc}\" -ne 0 ]; then\n" +
		"                echo \"hostile-inventories tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"              fi\n"
	auditFuzzTargetCreateWhole = "            echo \"${rc}\" > \"${out}/targets/${n}-${name}.create.exit\"\n" +
		"            if [ \"${rc}\" -eq 124 ]; then\n" +
		"              echo \"fuzzing timed-out\" >> \"${results}/not-run.txt\"\n" +
		"              echo \"fuzzing: the create for ${pkg} ${name} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"            elif [ \"${rc}\" -ne 0 ]; then\n" +
		"              echo \"fuzzing tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"            fi\n"

	// auditReleaseFileOutcomeWhole is one release file's attestation as the
	// accepted shape wrote it before round K: a file's refused attestation read as
	// the whole check that did not run (B1).
	auditReleaseFileOutcomeWhole = "              echo \"${rc}\" > \"${out}/attestations/${name}.exit\"\n" +
		"              if [ \"${rc}\" -eq 124 ]; then\n" +
		"                echo \"release-integrity timed-out\" >> \"${results}/not-run.txt\"\n" +
		"                echo \"release-integrity: the attestation of ${name} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"              elif [ \"${rc}\" -eq 1 ]; then\n" +
		"                echo \"release-integrity verification-failed\" >> \"${results}/verification-failed.txt\"\n" +
		"              elif [ \"${rc}\" -ne 0 ]; then\n" +
		"                echo \"release-integrity tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"              fi\n"

	// auditNoDigestNotRun is row 7's else as it read before round K: an image
	// digest that did not resolve written to not-run.txt as the whole check not
	// running (B1, S4).
	auditNoDigestNotRun = "          else\n" +
		"            echo \"release-integrity tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"release-integrity: no image digest, so the image's signature and attestations were not verified\" >> \"${results}/limits.txt\"\n" +
		"          fi\n"

	// auditMalformedChecksums is row 7's record of a checksums.txt that is no
	// empty and holds no well-formed line: a finding about the release, recorded
	// verification-failed, with the limits line that says why (L4).
	auditMalformedChecksums = "            ran_any=1\n" +
		"            # A checksums.txt that holds no well-formed line names no file of the release: that is a\n" +
		"            # finding about the release, recorded verification-failed with the limits line that says why,\n" +
		"            # never a tool that failed and never a silent pass.\n" +
		"            if [ -s \"${rel}/checksums.txt\" ] && [ ! -s \"${out}/checksums.filtered\" ]; then\n" +
		"              echo \"release-integrity verification-failed\" >> \"${results}/verification-failed.txt\"\n" +
		"              echo \"release-integrity: checksums.txt holds no well-formed line\" >> \"${results}/limits.txt\"\n" +
		"            fi\n"

	// auditWhyIDLine is the one not-run line of the checks job that no other rule
	// of the check pins: row 15's scorecard-admin record. The why id it ends in is
	// what the vocabulary test moves, so the check's list of why ids is pinned
	// against ci/auditpublish/internal/publish/vocabulary.go without any other
	// rule standing in for it.
	auditWhyIDLine = "          echo \"scorecard-admin needs-admin-read\" >> \"${results}/not-run.txt\"\n"

	// auditFuzzTargetRm is row 14's removal of a target's container, with the fi of
	// the branch above it, inside the budgeted loop: the lines the case of an
	// unbudgeted inner loop wraps in one.
	auditFuzzTargetRm = "            fi\n            timeout 1m docker rm -f \"${cid}\" > /dev/null || true\n"
)

// Round L's anchors (the reviewer's K1, K2, K3, K4 and K6 findings and the
// auditor's L1 on PR 5c, round L): row 7's download records the why it did not
// run at the step's end and only there, a run or a target counts only once it
// started, and the shapes fit must follow or refuse are the ones the accepted
// shape writes.
const (
	// auditReleaseDownloadBranches is row 7's download outcome as the accepted
	// shape writes it (K1): each branch records a limits line and the why, and the
	// step's end writes the one not-run line, so that a download that failed
	// beside a finding of the image half is that finding, never also a check that
	// did not run.
	auditReleaseDownloadBranches = "          if [ \"${rc}\" -eq 124 ]; then\n" +
		"            why=timed-out\n" +
		"            echo \"release-integrity: the release download timed out after 5 minutes\" >> \"${results}/limits.txt\"\n" +
		"            rel_ok=0\n" +
		"          elif [ \"${rc}\" -ne 0 ]; then\n" +
		"            why=tool-failed\n" +
		"            echo \"release-integrity: the release download failed with exit ${rc}\" >> \"${results}/limits.txt\"\n" +
		"            rel_ok=0\n" +
		"          elif [ ! -s \"${rel}/checksums.txt\" ]; then\n" +
		"            why=results-missing\n" +
		"            echo \"release-integrity: no checksums.txt, so the release's files were neither checksummed nor attested\" >> \"${results}/limits.txt\"\n" +
		"            rel_ok=0\n" +
		"          fi\n"

	// auditReleaseDownloadNotRunBranches is that outcome as K1 finds it: the
	// not-run line written in the download's own branch, while the image half
	// still runs and may record a finding of the same check.
	auditReleaseDownloadNotRunBranches = "          if [ \"${rc}\" -eq 124 ]; then\n" +
		"            why=timed-out\n" +
		"            echo \"release-integrity timed-out\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"release-integrity: the release download timed out after 5 minutes\" >> \"${results}/limits.txt\"\n" +
		"            rel_ok=0\n" +
		"          elif [ \"${rc}\" -ne 0 ]; then\n" +
		"            why=tool-failed\n" +
		"            echo \"release-integrity tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"release-integrity: the release download failed with exit ${rc}\" >> \"${results}/limits.txt\"\n" +
		"            rel_ok=0\n" +
		"          elif [ ! -s \"${rel}/checksums.txt\" ]; then\n" +
		"            why=results-missing\n" +
		"            echo \"release-integrity results-missing\" >> \"${results}/not-run.txt\"\n" +
		"            echo \"release-integrity: no checksums.txt, so the release's files were neither checksummed nor attested\" >> \"${results}/limits.txt\"\n" +
		"            rel_ok=0\n" +
		"          fi\n"

	// auditHostileRunCreateFixed and auditFuzzTargetCreateFixed are rows 13 and
	// 14's create outcome as the accepted shape writes it (K2): the exit recorded,
	// its limits lines, and the continue that skips the run, which is the only
	// count a create has (round M's M5: the started run's own ran_any=1 is the
	// one that counts it).
	auditHostileRunCreateFixed = "              echo \"${rc}\" > \"${out}/${run}.create.exit\"\n" +
		"              if [ \"${rc}\" -eq 124 ]; then\n" +
		"                echo \"hostile-inventories: the create for ${run} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"              elif [ \"${rc}\" -ne 0 ]; then\n" +
		"                echo \"hostile-inventories: the create for ${run} failed with exit ${rc}\" >> \"${results}/limits.txt\"\n" +
		"              fi\n" +
		"              if [ \"${rc}\" -ne 0 ]; then\n" +
		"                continue\n" +
		"              fi\n"
	auditFuzzTargetCreateFixed = "            echo \"${rc}\" > \"${out}/targets/${n}-${name}.create.exit\"\n" +
		"            if [ \"${rc}\" -eq 124 ]; then\n" +
		"              echo \"fuzzing: the create for ${pkg} ${name} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"            elif [ \"${rc}\" -ne 0 ]; then\n" +
		"              echo \"fuzzing: the create for ${pkg} ${name} failed with exit ${rc}\" >> \"${results}/limits.txt\"\n" +
		"            fi\n" +
		"            if [ \"${rc}\" -ne 0 ]; then\n" +
		"              continue\n" +
		"            fi\n"

	// auditHostileRunCreateFailed and auditFuzzTargetCreateFailed are those
	// outcomes as K2 finds them: ran_any=1 right after the create's exit, before
	// the continue guard, so a create that failed counts as a run that ran and a
	// check whose every create failed reads as one that ran clean.
	auditHostileRunCreateFailed = "              echo \"${rc}\" > \"${out}/${run}.create.exit\"\n" +
		"              ran_any=1\n" +
		"              if [ \"${rc}\" -eq 124 ]; then\n" +
		"                echo \"hostile-inventories: the create for ${run} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"              elif [ \"${rc}\" -ne 0 ]; then\n" +
		"                echo \"hostile-inventories: the create for ${run} failed with exit ${rc}\" >> \"${results}/limits.txt\"\n" +
		"              fi\n" +
		"              if [ \"${rc}\" -ne 0 ]; then\n" +
		"                continue\n" +
		"              fi\n"
	auditFuzzTargetCreateFailed = "            echo \"${rc}\" > \"${out}/targets/${n}-${name}.create.exit\"\n" +
		"            ran_any=1\n" +
		"            if [ \"${rc}\" -eq 124 ]; then\n" +
		"              echo \"fuzzing: the create for ${pkg} ${name} timed out after 2 minutes\" >> \"${results}/limits.txt\"\n" +
		"            elif [ \"${rc}\" -ne 0 ]; then\n" +
		"              echo \"fuzzing: the create for ${pkg} ${name} failed with exit ${rc}\" >> \"${results}/limits.txt\"\n" +
		"            fi\n" +
		"            if [ \"${rc}\" -ne 0 ]; then\n" +
		"              continue\n" +
		"            fi\n"

	// auditHostileRunStartUnmarked and auditFuzzTargetStartUnmarked are the
	// started run's and target's outcome without the line that counts them: a run
	// that began then counts for nothing, and a check whose every run began is
	// recorded as one that did not run at all.
	auditHostileRunStartUnmarked = "              echo \"${rc}\" > \"${out}/${run}.exit\"\n" +
		"              if [ \"${rc}\" -eq 124 ]; then\n" +
		"                timeout 1m docker kill \"${cid}\" > /dev/null 2>&1 || true\n" +
		"                echo \"hostile-inventories: ${run} timed out after 60 seconds, a finding candidate of the run\" >> \"${results}/limits.txt\"\n" +
		"              fi\n"
	auditFuzzTargetStartUnmarked = "            echo \"${rc}\" > \"${out}/targets/${n}-${name}.exit\"\n" +
		"            if [ \"${rc}\" -eq 124 ]; then\n" +
		"              timeout 1m docker kill \"${cid}\" > /dev/null 2>&1 || true\n" +
		"              echo \"fuzzing: ${pkg} ${name} timed out after 12 minutes\" >> \"${results}/limits.txt\"\n" +
		"            fi\n"

	// auditHostileRunFindingCandidate is row 13's finding-candidate limits line,
	// the one the run's own timeout writes (K6).
	auditHostileRunFindingCandidate = "echo \"hostile-inventories: ${run} timed out after 60 seconds, a finding candidate of the run\" >> \"${results}/limits.txt\""
)

// Round M's anchors (the reviewer's S1 and S2 and the auditor's L-1 and L-2 on
// PR 5c, round M): the two step ends the check accepts between a not-run record
// and the exit 0 that follows it, and the shell words that reach the why one of
// them writes.
const (
	// auditHostileStagingCreateEnd is row 13's staging create's own failure
	// record through the copy the step goes on to: the record in its elif, the
	// branch's fi, then the step's own guard, echo "finished" and the exit 0 that
	// ends it, and then the next part. It is the shape M1 breaks, one line at a
	// time, and the copy after it is what tells a record the step went on from.
	auditHostileStagingCreateEnd = "            echo \"hostile-inventories tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"          fi\n" +
		"          if [ \"${rc}\" -ne 0 ]; then\n" +
		"            echo \"finished\"\n" +
		"            exit 0\n" +
		"          fi\n" +
		"          rc=0\n" +
		"          timeout 1m docker cp \"${stage}\" \"${staging}:/work/\""
	// auditFuzzStagingCreateEnd is row 14's, the same shape in another step, so
	// that a rule the check applies to one row alone is caught.
	auditFuzzStagingCreateEnd = "            echo \"fuzzing tool-failed\" >> \"${results}/not-run.txt\"\n" +
		"          fi\n" +
		"          if [ \"${rc}\" -ne 0 ]; then\n" +
		"            echo \"finished\"\n" +
		"            exit 0\n" +
		"          fi\n" +
		"          rc=0\n" +
		"          timeout 2m docker cp \"${src}\" \"${staging}:/src\""
	// auditHostileVerificationFailed is the finding one of row 13's parts writes,
	// which no not-run line may stand beside at the step's end (M1's (a)).
	auditHostileVerificationFailed = "            echo \"hostile-inventories verification-failed\" >> \"${results}/verification-failed.txt\"\n"
	// auditFuzzVerificationFailed is its row 14 counterpart.
	auditFuzzVerificationFailed = "            echo \"fuzzing verification-failed\" >> \"${results}/verification-failed.txt\"\n"
)

// auditWhyNotRunLiteral matches the publish core's whyNotRun map: its
// declaration through its closing brace.
var auditWhyNotRunLiteral = regexp.MustCompile(`(?s)var whyNotRun = map\[string\]string\{\n(.*?)\n\}`)

// auditWhyIDLiteral matches one why id of that map: a quoted key.
var auditWhyIDLiteral = regexp.MustCompile(`"([a-z0-9-]+)":`)

// auditVerificationBranch returns one of row 7's verifications' outcome branch,
// exactly as the accepted fixture writes it (round K): the exit recorded, the part
// marked as one that ran, a timeout and a failure in the limits lines of this
// check, and a refusal of the verification itself recorded as the finding
// verification-failed in the results. A verification whose refusal the tool
// reports with other exit codes passes them as cond (round I of PR 5c).
func auditVerificationBranch(indent, timedOut, failed string) string {
	return auditVerificationBranchWhen(indent, `[ "${rc}" -eq 1 ]`, timedOut, failed)
}

// auditVerificationBranchWhen returns row 7's outcome branch whose finding line
// reads cond, the condition of its elif.
func auditVerificationBranchWhen(indent, cond, timedOut, failed string) string {
	return indent + "ran_any=1\n" +
		indent + "if [ \"${rc}\" -eq 124 ]; then\n" +
		indent + "  echo \"release-integrity: " + timedOut + "\" >> \"${results}/limits.txt\"\n" +
		indent + "elif " + cond + "; then\n" +
		indent + "  echo \"release-integrity verification-failed\" >> \"${results}/verification-failed.txt\"\n" +
		indent + "elif [ \"${rc}\" -ne 0 ]; then\n" +
		indent + "  echo \"release-integrity: " + failed + "\" >> \"${results}/limits.txt\"\n" +
		indent + "fi\n"
}

// auditWithoutFinding returns one of row 7's outcome branches without its
// finding: the elif that reads the release's refusal and the record it writes,
// so that the branch records only a timeout and a tool that failed - the shape
// round E refuses, because the refusal then reads as a check that did not run.
func auditWithoutFinding(branch string) string {
	lines := strings.Split(strings.TrimSuffix(branch, "\n"), "\n")

	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		if strings.Contains(lines[i], `elif [ "${rc}" -eq 1 ]`) {
			i++

			continue
		}

		out = append(out, lines[i])
	}

	return strings.Join(out, "\n") + "\n"
}

// auditChecksum is one tool download of a download step: the job it runs in,
// the name its error gives it, the env: variable that holds its pin and the file
// the step writes the download to, which is the file it checksums.
type auditChecksum struct {
	job, tool, variable, file string
}

// auditChecksums are the accepted fixture's tool downloads, in the order the
// steps run them: the checks job's four, the model job's three and the publish
// job's one. Each download is one guard of its own, so a case per download is
// what holds every one of them: a step whose guard is required once would still
// accept a download left bare. The two artifacts' zips are no tool pin: their
// digest comes from artifact.json, the check's own output, and zipDigest holds
// that check.
var auditChecksums = []auditChecksum{
	{"checks", "gitleaks", "GITLEAKS_SHA256", "gitleaks.tar.gz"},
	{"checks", "osv-scanner", "OSV_SCANNER_SHA256", "osv-scanner"},
	{"checks", "zizmor", "ZIZMOR_SHA256", "zizmor.tar.gz"},
	{"checks", "age", "AGE_SHA256", "age.tar.gz"},
	{"model", "bubblewrap", "BUBBLEWRAP_DEB_SHA256", "bubblewrap.deb"},
	{"model", "socat", "SOCAT_DEB_SHA256", "socat.deb"},
	{"model", "age", "AGE_SHA256", "age.tar.gz"},
	{"publish", "gitleaks", "GITLEAKS_SHA256", "gitleaks.tar.gz"},
}

// auditChecksumGuard returns download d's checksum check as the accepted fixture
// writes it: sha256sum under an if !, an error naming the tool, its job and the
// runbook step that fixes a pin the download no longer matches, and the exit 1
// that fails the step, each on a line of its own.
func auditChecksumGuard(d auditChecksum) string {
	return "          if ! " + auditChecksumCheck(d) + "; then\n" +
		"            echo \"::error title=Audit " + d.job + "::" + d.tool + "'s checksum differs from its pin: see runbook step 11\"\n" +
		"            exit 1\n" +
		"          fi\n"
}

// auditChecksumBare returns download d's checksum check as the fixture wrote it
// before the guard, and as a case that breaks one puts it back: sha256sum
// alone, whose failure is its own line and names no cause.
func auditChecksumBare(d auditChecksum) string {
	return "          " + auditChecksumCheck(d) + "\n"
}

// auditChecksumCheck is the comparison both forms are built from: the pin the
// step's env: holds, the file the step downloaded, and sha256sum --check.
func auditChecksumCheck(d auditChecksum) string {
	return "echo \"${" + d.variable + "}  ${RUNNER_TEMP}/" + d.file + "\" | sha256sum --check --strict -"
}

// checksumsOf returns the downloads of the job's steps, in the order the steps
// run them: the ones a case that breaks a whole download step's guards changes
// together, so that a step is held to guard each check it makes whole and not a
// shape one of them keeps.
func checksumsOf(job string) []auditChecksum {
	var out []auditChecksum

	for _, d := range auditChecksums {
		if d.job == job {
			out = append(out, d)
		}
	}

	return out
}

// breakChecksums returns doc with every download of downloads replaced by what
// change returns for it.
func breakChecksums(t *testing.T, doc string, downloads []auditChecksum, change func(d auditChecksum) string) string {
	t.Helper()

	for _, d := range downloads {
		doc = mutate(t, doc, auditChecksumGuard(d), change(d))
	}

	return doc
}

// auditChecksumBreakages are the three ways every guard of one download step
// stops refusing a download that differs from its pin: put back to the bare
// sha256sum line, left without its exit 1, and run together onto the echo's
// line, whose echo, exit and fi are then words of one command that carries on.
var auditChecksumBreakages = []struct {
	name string
	of   func(d auditChecksum) string
}{
	{"put back to the bare sha256sum line", auditChecksumBare},
	{"left without their exit 1", func(d auditChecksum) string {
		return strings.Replace(auditChecksumGuard(d), "            exit 1\n", "", 1)
	}},
	{"run together onto the echo's line", func(d auditChecksum) string {
		return joinedBranch(auditChecksumGuard(d))
	}},
}

// auditWorkflowCase is one case of TestAuditWorkflow: a mutation of the accepted
// shape, and the part of a message it must draw.
type auditWorkflowCase struct {
	name     string
	workflow string
	// wantIn is a part of a message the check must return for the case, and
	// the accepted shape does not draw; "" means no message at all.
	wantIn string
}

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

// auditLineNumber is a line number the check quotes: a step's "(line N)" and a
// line-ruled message's "line N: ".
var auditLineNumber = regexp.MustCompile(`\(line [0-9]+\)|^line [0-9]+: `)

// auditFresh returns the messages got draws that the accepted shape does not,
// compared without their line numbers: a case that adds or removes a line moves
// every line below it, and the check then reports the same problem at another
// line, which is no complaint of the case's own. A complaint is fresh when the
// accepted shape draws none of its kind; a fixture the check refuses makes every
// case undecidable, so it fails the test here rather than being logged past (the
// reviewer's N2, round K).
func auditFresh(t *testing.T, accepted, got []string) []string {
	t.Helper()

	if len(accepted) != 0 {
		t.Fatalf("the accepted shape %s draws %d messages, want none (%q is the first): every case's fresh diff is taken from them, so a fixture the check refuses decides no case",
			auditWorkflowFixture, len(accepted), accepted[0])
	}

	seen := make(map[string]bool, len(accepted))

	for _, m := range accepted {
		seen[auditLineNumber.ReplaceAllString(m, "(line)")] = true
	}

	out := make([]string, 0, len(got))

	for _, m := range got {
		if !seen[auditLineNumber.ReplaceAllString(m, "(line)")] {
			out = append(out, m)
		}
	}

	return out
}

// TestAuditWorkflowWhyIDsMatchThePublishCore pins the checks job's closed list of
// why ids (round K's B2): every not-run.txt line the checks job writes ends in a
// why id that ci/auditpublish/internal/publish/vocabulary.go's whyNotRun holds,
// read here at test time as the other cross-file pins read theirs, and an id that
// file does not hold is refused. The line the cases move is row 15's
// scorecard-admin record, which no other rule of the check pins: the case's
// message can be the why-id one and no other.
func TestAuditWorkflowWhyIDsMatchThePublishCore(t *testing.T) {
	t.Parallel()

	acceptedWorkflow := string(auditWorkflowFixtureBytes(t))
	accepted := check.AuditWorkflow([]byte(acceptedWorkflow))

	for _, id := range auditPublishWhyIDs(t) {
		t.Run("the why id "+id+" is accepted", func(t *testing.T) {
			t.Parallel()

			doc := mutate(t, acceptedWorkflow, auditWhyIDLine, "          echo \"scorecard-admin "+id+"\" >> \"${results}/not-run.txt\"\n")

			if got := auditFresh(t, accepted, check.AuditWorkflow([]byte(doc))); len(got) != 0 {
				t.Errorf("AuditWorkflow() draws %q for a not-run line whose why id is %q, which vocabulary.go holds: the check's list of why ids is short of it", got, id)
			}
		})
	}

	for _, id := range []string{"unexpected-ruleset", "unexpected-target"} {
		t.Run("the why id "+id+" is refused", func(t *testing.T) {
			t.Parallel()

			doc := mutate(t, acceptedWorkflow, auditWhyIDLine, "          echo \"scorecard-admin "+id+"\" >> \"${results}/not-run.txt\"\n")

			fresh := auditFresh(t, accepted, check.AuditWorkflow([]byte(doc)))

			joined := strings.Join(fresh, "\n")
			if !strings.Contains(joined, id) || !strings.Contains(joined, "is no why id") {
				t.Errorf("AuditWorkflow() = %q, want a message naming %q as no why id: vocabulary.go holds no such id, so a not-run line ending in it is a why the report cannot render", fresh, id)
			}
		})
	}

	// Round L's K7: the cases above prove every why id vocabulary.go holds is
	// accepted, and an id neither holds is refused, but an id the check holds and
	// the publish core does not would pass them. Both lists are the one set.
	t.Run("the check's list is the publish core's, both ways", func(t *testing.T) {
		t.Parallel()

		want := auditPublishWhyIDs(t)
		got := check.AuditWhyIDs()

		slices.Sort(want)
		slices.Sort(got)

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("check.AuditWhyIDs() and vocabulary.go's whyNotRun differ (-publish core +check):\n%s\n"+
				"an id only the check holds is a not-run line it accepts and the core cannot render, which publishes no report, "+
				"and an id only the core holds is one the workflow cannot write (PRD #175 milestone 2, ADR 0040)", diff)
		}
	})
}

// auditPublishWhyIDs returns the why ids ci/auditpublish/internal/publish/
// vocabulary.go's whyNotRun map holds, read from the file at test time.
func auditPublishWhyIDs(t *testing.T) []string {
	t.Helper()

	path := filepath.FromSlash("../../../../ci/auditpublish/internal/publish/vocabulary.go")

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v; the check's list of why ids is pinned against it", path, err)
	}

	m := auditWhyNotRunLiteral.FindSubmatch(src)
	if m == nil {
		t.Fatalf("%s holds no var whyNotRun = map[string]string{...}: the check's list of why ids is pinned against it", path)
	}

	var ids []string

	for _, e := range auditWhyIDLiteral.FindAllSubmatch(m[1], -1) {
		ids = append(ids, string(e[1]))
	}

	if len(ids) == 0 {
		t.Fatalf("%s's whyNotRun holds no why id", path)
	}

	return ids
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

	// Every case's fresh diff is taken from the accepted shape's messages, so a
	// fixture the check refuses decides no case: auditFresh fails the test rather
	// than logging it, and TestAuditWorkflowAcceptsTheAcceptedShape is what
	// refuses the fixture on its own name.

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

	tests := []auditWorkflowCase{
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
			workflow: mutate(t, acceptedWorkflow, auditChecksIf, "    timeout-minutes: 355\n    if: ${{ always() }}\n"),
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
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 355\n", ""),
			wantIn:   "timeout-minutes",
		},
		{
			name:     "a job timeout at GitHub's limit",
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 355\n", "    timeout-minutes: 360\n"),
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
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 355\n", "    timeout-minutes: 355\n    env:\n      GH_TOKEN: ${{ github.token }}\n"),
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
			workflow: mutate(t, acceptedWorkflow, "    timeout-minutes: 355\n", "    timeout-minutes: 355\n    outputs:\n      tag: ${{ steps.record.outputs.tag }}\n"),
			wantIn:   "outputs",
		},
		{
			name: "an Actions cache",
			workflow: withStep("      - name: Download the pinned tools and verify their checksums\n        timeout-minutes: 25\n",
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

		// The model job's tools live in a directory the runner's user cannot write
		// (finding M1 of issue #614's review, ADR 0040): the model session runs
		// between the steps that install the tools and the steps that run them, so
		// the tools are installed root-owned with mode 0555 into /srv/audit/tools
		// and run by that absolute path, never from the job's own bin directory in
		// $RUNNER_TEMP - which the steps appended to $GITHUB_PATH, first on the
		// PATH - and never by a word or a relative path the shell resolves.
		{
			name: "a write to $GITHUB_PATH in the model job",
			workflow: mutate(t, acceptedWorkflow, auditModelSocat,
				auditModelSocat+"          echo \"${RUNNER_TEMP}/bin\" >> \"${GITHUB_PATH}\"\n"),
			wantIn: "GITHUB_PATH",
		},
		{
			name:     "the model job's launcher run from the runner's own bin directory",
			workflow: mutate(t, acceptedWorkflow, auditRunCommand, strings.Replace(auditRunCommand, "ci/auditrun", "\"${RUNNER_TEMP}/bin/auditrun\"", 1)),
			wantIn:   "auditrun",
		},
		{
			name:     "the model job's artifact check run from the runner's own bin directory",
			workflow: mutate(t, acceptedWorkflow, auditModelArtifactsCommand, strings.Replace(auditModelArtifactsCommand, "ci/auditpublish", "\"${RUNNER_TEMP}/bin/auditpublish\"", 1)),
			wantIn:   "auditpublish",
		},
		{
			name: "the model job's launcher installed by a plain install into the runner's own bin directory",
			workflow: mutate(t, acceptedWorkflow, auditWrapperInstall,
				auditWrapperInstall+"          install -m 0755 \"${RUNNER_TEMP}/auditrun\" \"${RUNNER_TEMP}/bin/auditrun\"\n"),
			wantIn: "auditrun",
		},
		{
			name: "the model job's artifact check installed without the root-owned mode",
			workflow: mutate(t, acceptedWorkflow, auditWrapperInstall,
				auditWrapperInstall+"          sudo install -o root -g root -m 0755 \"${RUNNER_TEMP}/auditpublish\" /srv/audit/tools/auditpublish\n"),
			wantIn: "/srv/audit/tools/auditpublish",
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
		// The stale socket file: stopping docker.socket leaves it on the runner
		// image, so the hardening step removes it while sudo still works, after
		// stopping and disabling Docker and before the runner user's sudo rule is
		// removed, and the step's own existence check after both still fails a
		// run whose socket is there (issue #574, ADR 0040). A step that leaves the
		// file behind, removes it once sudo is gone, or removes it without
		// recording its exit hardens nothing, so each case pins the removal rule's
		// own message, never the bare docker.sock that several of the step's
		// messages hold; the two that leave the step without the whole removal
		// line draw that one message and so share its fragment.
		{
			name:     "no removal of the stale Docker socket file",
			workflow: mutate(t, acceptedWorkflow, auditDockerSocketRemoval, ""),
			wantIn:   "runs no sudo rm -f /var/run/docker.sock || rc=$?, on a line of its own, after Docker is stopped and disabled and before the runner user's sudo rule is removed",
		},
		{
			name:     "the stale Docker socket file removed after the runner user's sudo rule",
			workflow: mutate(t, acceptedWorkflow, auditDockerSocketRemoval+auditSudoRuleRemoval, auditSudoRuleRemoval+auditDockerSocketRemoval),
			wantIn:   "after sudo rm -f /etc/sudoers.d/runner || rc=$?, on a line of its own, want it before",
		},
		{
			name:     "the stale Docker socket file removed without recording its exit",
			workflow: mutate(t, acceptedWorkflow, auditDockerSocketRemoval, "          sudo rm -f /var/run/docker.sock\n"),
			wantIn:   "runs no sudo rm -f /var/run/docker.sock || rc=$?, on a line of its own, after Docker is stopped and disabled and before the runner user's sudo rule is removed",
		},
		// The step's own refusals of a socket still there and of a daemon still
		// running: the removal of the file shows nothing about the daemon, so a
		// step that keeps no existence check, or one that asks before it removes
		// the file (and so before the sudoers rule goes), hardens nothing. Each
		// case pins its rule's own message: the refusal made absent ("runs no
		// if ...") or the refusal overtaken ("after if ...").
		{
			name:     "no Docker socket existence check",
			workflow: mutate(t, acceptedWorkflow, auditDockerSocketCheck, ""),
			wantIn:   "runs no if [ -e /var/run/docker.sock ]; then",
		},
		{
			name: "the Docker socket existence check before the stale socket file is removed",
			workflow: mutate(t, withoutStep(auditDockerSocketCheck), auditDockerSocketRemoval,
				auditDockerSocketCheck+auditDockerSocketRemoval),
			wantIn: "after if [ -e /var/run/docker.sock ]; then",
		},
		{
			name:     "no Docker daemon check",
			workflow: mutate(t, acceptedWorkflow, auditDockerActiveCheck, ""),
			wantIn:   "runs no if systemctl is-active --quiet docker.service docker.socket",
		},
		{
			name:     "the Docker daemon check before Docker is stopped",
			workflow: mutate(t, withoutStep(auditDockerActiveCheck), auditDockerStop, auditDockerActiveCheck+auditDockerStop),
			wantIn:   "after if systemctl is-active --quiet docker.service docker.socket; then",
		},
		// A refusal whose condition is weakened refuses nothing: a condition
		// that can never be true leaves the step's whole line in place while
		// the daemon or the socket it must refuse passes. failureBranch holds a
		// refusal's condition whole, through its ; then, so the appended
		// neutraliser is no required line and the case is refused as the line's
		// absence (issue #574, round 3 of the AI review of PR #586).
		{
			name: "the Docker daemon check's condition neutralised",
			workflow: mutate(t, acceptedWorkflow, auditDockerActiveCheck,
				"          if systemctl is-active --quiet docker.service docker.socket && false; then\n"+
					"            echo \"::error title=Audit model::Docker is still running: see runbook step 10\"\n"+
					"            exit 1\n"+
					"          fi\n"),
			wantIn: "runs no if systemctl is-active --quiet docker.service docker.socket; then",
		},
		{
			name: "the Docker socket check's condition neutralised",
			workflow: mutate(t, acceptedWorkflow, auditDockerSocketCheck,
				"          if [ -e /var/run/docker.sock ] && false; then\n"+
					"            echo \"::error title=Audit model::the Docker socket is still there: see runbook step 10\"\n"+
					"            exit 1\n"+
					"          fi\n"),
			wantIn: "runs no if [ -e /var/run/docker.sock ]; then",
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
			workflow: mutate(t, acceptedWorkflow, "          printf '%s\\n' \"${AUDIT_MODEL_PRIVATE_KEY}\" | /srv/audit/tools/age --decrypt",
				"          printf '%s\\n' \"${AUDIT_MODEL_PRIVATE_KEY}\" > /tmp/age.key\n          printf '%s\\n' \"${AUDIT_MODEL_PRIVATE_KEY}\" | /srv/audit/tools/age --decrypt"),
			wantIn: "/tmp",
		},
		{
			name: "the publish key under /run",
			workflow: mutate(t, acceptedWorkflow, "          printf '%s\\n' \"${AUDIT_PUBLISH_PRIVATE_KEY}\" | \"${RUNNER_TEMP}/bin/age\" --decrypt",
				"          printf '%s\\n' \"${AUDIT_PUBLISH_PRIVATE_KEY}\" > /run/age.key\n          printf '%s\\n' \"${AUDIT_PUBLISH_PRIVATE_KEY}\" | \"${RUNNER_TEMP}/bin/age\" --decrypt"),
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
				"          /srv/audit/tools/age --encrypt --recipient \"${AUDIT_PUBLISH_PUBLIC_KEY}\" --output \"${RUNNER_TEMP}/model-result.age\" /srv/audit/work/result.json\n"),
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
			workflow: mutate(t, acceptedWorkflow, auditModelArtifactsCommand+
				auditModelArtifactError+
				"            exit 1\n"+
				"          fi\n"+
				"          artifact=\"${RUNNER_TEMP}/artifact.json\"\n",
				auditModelArtifactsCommand+
					auditModelArtifactError+
					"            exit 1\n"+
					"          fi\n"+
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
		// The four cases above take each flag out of the three creates at once, so
		// they hold the flag of the checks job and not of one row. These three take
		// a flag out of one row alone: every row's spec has to require its own.
		{
			name:     "row 11's container without --read-only",
			workflow: mutate(t, acceptedWorkflow, auditRow11Create, strings.Replace(auditRow11Create, " --read-only", "", 1)),
			wantIn:   "--read-only",
		},
		{
			name:     "row 13's container without --security-opt no-new-privileges",
			workflow: mutate(t, acceptedWorkflow, auditRow13Create, strings.Replace(auditRow13Create, " --security-opt no-new-privileges", "", 1)),
			wantIn:   "no-new-privileges",
		},
		{
			name:     "row 14's container without --network none",
			workflow: mutate(t, acceptedWorkflow, auditRow14Create, strings.Replace(auditRow14Create, " --network none", "", 1)),
			wantIn:   "--network none",
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
		// Rows 13 and 14 bring the inputs into the image through a staging
		// container: created with no flag and no command, copied into, committed
		// and removed. Only the row's run container, the one docker create made
		// with the four flags, is ever started: docker start of the staging
		// container runs a container nothing hardened.
		{
			name:     "a docker start of row 13's staging container",
			workflow: mutate(t, acceptedWorkflow, auditHostileStaging, auditHostileStaging+"          timeout 60 docker start --attach \"${staging}\" > /dev/null 2>&1 || true\n"),
			wantIn:   "staging",
		},
		{
			name:     "a docker start of row 14's staging container",
			workflow: mutate(t, acceptedWorkflow, auditFuzzStaging, auditFuzzStaging+"          timeout 60 docker start --attach \"${staging}\" > /dev/null 2>&1 || true\n"),
			wantIn:   "staging",
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
			name:     "the fuzz target's shape guard without its continue",
			workflow: mutate(t, acceptedWorkflow, auditFuzzShapeGuard, strings.Replace(auditFuzzShapeGuard, "              continue\n", "", 1)),
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
			wantIn:   "the ruleset ID's shape guard",
		},
		{
			name:     "the ruleset ID guard without its record",
			workflow: mutate(t, acceptedWorkflow, auditRulesetGuard, strings.Replace(auditRulesetGuard, "              echo \"supply-chain: a ruleset ID that is no number was not read\" >> \"${results}/limits.txt\"\n", "", 1)),
			wantIn:   "the ruleset ID's shape guard",
		},
		{
			name:     "the ruleset ID guard joined onto the echo's line",
			workflow: mutate(t, acceptedWorkflow, auditRulesetGuard, joinedBranch(auditRulesetGuard)),
			wantIn:   "the ruleset ID's shape guard",
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

		// Round D: the review's and the audit's findings on PR 5c, each case
		// changing one of them back. A release file, an archive and the image are
		// the untrusted party, so a command that reads one runs under a timeout of
		// its own, records a failure as a check that did not run, and never aborts
		// the run's report.
		{
			name:     "row 7's checksum filter without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditChecksumFilter, strings.Replace(auditChecksumFilter, "timeout 1m grep", "grep", 1)),
			wantIn:   "checksums.filtered",
		},
		{
			name:     "row 7's checksum filter keeping lines of any shape",
			workflow: mutate(t, acceptedWorkflow, auditChecksumFilter, strings.Replace(auditChecksumFilter, "'^[0-9a-f]{64}  [A-Za-z0-9._+-]+$'", "'[0-9a-f]{64}'", 1)),
			wantIn:   "checksums.filtered",
		},
		{
			name:     "row 7's sha256sum without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditReleaseChecksum, strings.Replace(auditReleaseChecksum, "timeout 5m sha256sum", "sha256sum", 1)),
			wantIn:   "sha256sum",
		},
		{
			name:     "row 7's sha256sum without its exit code check",
			workflow: mutate(t, acceptedWorkflow, auditReleaseChecksum, strings.Replace(auditReleaseChecksum, " || rc=$?", "", 1)),
			wantIn:   "rc=$?",
		},
		{
			name:     "row 7's attestation loop without its budget",
			workflow: mutate(t, acceptedWorkflow, auditReleaseLoopBudget, ""),
			wantIn:   "no loop budget",
		},
		{
			name:     "row 7's provenance verification without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditProvenanceVerify, strings.Replace(auditProvenanceVerify, "timeout 2m gh attestation verify", "gh attestation verify", 1)),
			wantIn:   "gh attestation verify",
		},
		{
			name:     "row 7's signature check without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditCosignVerify, strings.Replace(auditCosignVerify, "timeout 3m cosign verify", "cosign verify", 1)),
			wantIn:   "runs cosign without its timeout",
		},
		{
			name:     "row 8's image save without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditImageSave, strings.Replace(auditImageSave, "timeout 4m docker save", "docker save", 1)),
			wantIn:   "docker save",
		},
		{
			name:     "row 9's unzip without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditBinaryUnpackZip, strings.Replace(auditBinaryUnpackZip, "timeout 2m unzip", "unzip", 1)),
			wantIn:   "unzip",
		},
		{
			name:     "row 11's config read without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditRuntimeInspect, strings.Replace(auditRuntimeInspect, "timeout 1m docker image inspect", "docker image inspect", 1)),
			wantIn:   "docker image inspect",
		},
		{
			name:     "row 11's container create without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditRow11Create, strings.Replace(auditRow11Create, "timeout 2m docker create", "docker create", 1)),
			wantIn:   "docker create",
		},
		{
			name:     "row 7's attestation loop with a budget past its step's timeout",
			workflow: mutate(t, acceptedWorkflow, auditReleaseLoopBudget, strings.Replace(auditReleaseLoopBudget, "1200", "3600", 1)),
			wantIn:   "budget",
		},
		{
			name:     "row 7's timed-out download recorded nowhere",
			workflow: mutate(t, acceptedWorkflow, auditReleaseDownloadTimedOutWhy, ""),
			wantIn:   "release-integrity",
		},
		{
			name:     "row 7's failed command recorded nowhere",
			workflow: mutate(t, acceptedWorkflow, auditReleaseDownloadToolFailedWhy, ""),
			wantIn:   "release-integrity",
		},
		{
			name:     "row 8's failed image save recorded nowhere",
			workflow: mutateEvery(t, acceptedWorkflow, auditPullToolFailed, ""),
			wantIn:   "image-pull",
		},
		{
			name:     "row 9's tar without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditBinaryUnpack, strings.Replace(auditBinaryUnpack, "timeout 2m tar", "tar", 1)),
			wantIn:   "runs tar without its timeout",
		},
		{
			name:     "row 9's tar without its size cap",
			workflow: mutate(t, acceptedWorkflow, auditBinaryUnpack, strings.Replace(auditBinaryUnpack, " | head -c 134217728", "", 1)),
			wantIn:   "head -c",
		},
		{
			name:     "row 9's failed unpack recorded nowhere",
			workflow: mutateEvery(t, acceptedWorkflow, auditBinaryToolFailed, ""),
			wantIn:   "binary-vulnerabilities",
		},
		{
			name:     "row 11's docker export without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditRuntimeExport, strings.Replace(auditRuntimeExport, "timeout 4m docker export", "docker export", 1)),
			wantIn:   "docker export",
		},
		{
			name:     "row 11's docker export without its exit code check",
			workflow: mutate(t, acceptedWorkflow, auditRuntimeExport, strings.Replace(auditRuntimeExport, " || rc=$?", "", 1)),
			wantIn:   "rc=$?",
		},
		{
			name:     "row 11's timed-out start recorded nowhere",
			workflow: mutateEvery(t, acceptedWorkflow, auditRuntimeTimedOut, ""),
			wantIn:   "image-runtime",
		},
		{
			name:     "row 13's docker cp without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditHostileCopy, strings.Replace(auditHostileCopy, "timeout 1m docker cp", "docker cp", 1)),
			wantIn:   "docker cp",
		},
		{
			name:     "row 13's docker commit without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditHostileCommit, strings.Replace(auditHostileCommit, "timeout 1m docker commit", "docker commit", 1)),
			wantIn:   "docker commit",
		},
		{
			name:     "row 13's docker commit without its exit code check",
			workflow: mutate(t, acceptedWorkflow, auditHostileCommit, strings.Replace(auditHostileCommit, " || rc=$?", "", 1)),
			wantIn:   "docker commit",
		},
		{
			name:     "row 13's timed-out create recorded nowhere",
			workflow: mutateEvery(t, acceptedWorkflow, auditHostileTimedOut, ""),
			wantIn:   "hostile-inventories",
		},
		{
			// The checks job's steps add up to 348 minutes: with the job's 5 minutes
			// of set-up and post steps they stay within its 355, and a step raised by
			// 17 puts them past it.
			name:     "a checks step adding up past the job's timeout",
			workflow: mutate(t, acceptedWorkflow, "      - name: Check the image's runtime settings\n        timeout-minutes: 13\n", "      - name: Check the image's runtime settings\n        timeout-minutes: 30\n"),
			wantIn:   "timeout-minutes",
		},

		// B. A container cannot be loosened after the prefix the check pins: the
		// fuzz create is held whole through its last line, and no line outside the
		// model job gives a container an unconfined security option, an env file,
		// a cgroup namespace, a network but none, a sysctl or another user.
		{
			name:     "a flag added to the fuzz container's create past its pinned prefix",
			workflow: mutate(t, acceptedWorkflow, "--env GOTMPDIR=/scratch --env TMPDIR=/scratch --env HOME=/scratch --env FUZZ_PKG=\"${pkg}\" --env FUZZ_NAME=\"${name}\" \\\n", "--env GOTMPDIR=/scratch --env TMPDIR=/scratch --env HOME=/scratch --env FUZZ_PKG=\"${pkg}\" --env FUZZ_NAME=\"${name}\" --init \\\n"),
			wantIn:   "audit-fuzz",
		},
		{
			name:     "the staging container with an unconfined seccomp profile",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--security-opt seccomp=unconfined "),
			wantIn:   "--security-opt",
		},
		{
			name:     "the staging container with an unconfined AppArmor profile",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--security-opt apparmor=unconfined "),
			wantIn:   "--security-opt",
		},
		{
			name:     "the staging container with SELinux labels disabled",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--security-opt label=disable "),
			wantIn:   "--security-opt",
		},
		{
			name:     "the staging container with unmasked system paths",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--security-opt systempaths=unconfined "),
			wantIn:   "--security-opt",
		},
		{
			name:     "the staging container with an environment file",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--env-file \"${RUNNER_TEMP}/audit.env\" "),
			wantIn:   "--env-file",
		},
		{
			name:     "the staging container in the host's cgroup namespace",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--cgroupns host "),
			wantIn:   "--cgroupns",
		},
		{
			name:     "the staging container on another container's network",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--network container:0123456789ab "),
			wantIn:   "--network",
		},
		{
			name:     "the staging container with a sysctl",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--sysctl net.ipv4.ip_forward=1 "),
			wantIn:   "--sysctl",
		},
		{
			name:     "the staging container as root",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreate, auditHostileStagingCreate+"--user 0:0 "),
			wantIn:   "--user",
		},
		{
			name:     "a container started by docker container run",
			workflow: mutate(t, acceptedWorkflow, "          timeout 4m docker export", "          docker container run --rm \"${image}@${digest}\" version\n          timeout 4m docker export"),
			wantIn:   "container run",
		},
		{
			name:     "a container started by docker --context run",
			workflow: mutate(t, acceptedWorkflow, "          timeout 4m docker export", "          docker --context example run --rm \"${image}@${digest}\" version\n          timeout 4m docker export"),
			wantIn:   "--context",
		},
		{
			name:     "a shell in a container by docker exec",
			workflow: mutate(t, acceptedWorkflow, "          timeout 4m docker export", "          timeout 60 docker exec \"${cid}\" sh -c true || rc=$?\n          timeout 4m docker export"),
			wantIn:   "docker exec",
		},
		{
			name:     "a shell in a container by docker container exec",
			workflow: mutate(t, acceptedWorkflow, "          timeout 4m docker export", "          timeout 60 docker container exec \"${cid}\" sh -c true || rc=$?\n          timeout 4m docker export"),
			wantIn:   "container exec",
		},
		{
			// A create of its own, flagless, whose container is then started: only
			// the container a docker create made with the four flags is ever run.
			name:     "a flagless container created and then started",
			workflow: mutate(t, acceptedWorkflow, auditHostileStaging, auditHostileStaging+"          other=\"$(timeout 1m docker create \"${image}@${digest}\" version)\" || rc=$?\n          timeout 60 docker start --attach \"${other}\" > /dev/null 2>&1 || true\n"),
			wantIn:   "docker start",
		},

		// C. The hostile corpus holds regular files alone, and row 13 copies it
		// without following a link. test/architecture's
		// TestAuditHostileCorpusIsRegularFiles holds the corpus itself.
		{
			name:     "the hostile corpus copied without -P",
			workflow: mutate(t, acceptedWorkflow, auditCorpusCopy, strings.Replace(auditCorpusCopy, "cp -P ", "cp ", 1)),
			wantIn:   "cp -P",
		},

		// D. Row 15 keeps variable and secret names alone: every read that returns
		// values passes the jq filter that keeps the names.
		{
			name:     "the repository's variable names read without the filter",
			workflow: mutate(t, acceptedWorkflow, auditRepositoryVariableNames, strings.Replace(auditRepositoryVariableNames, " '[.variables[].name]'", "", 1)),
			wantIn:   "[.variables[].name]",
		},
		{
			name:     "an environment's variable names read without the filter",
			workflow: mutate(t, acceptedWorkflow, auditEnvironmentVariableNames, strings.Replace(auditEnvironmentVariableNames, " '[.variables[].name]'", "", 1)),
			wantIn:   "[.variables[].name]",
		},
		{
			name:     "the repository's secret names read without the filter",
			workflow: mutate(t, acceptedWorkflow, auditRepositorySecretNames, strings.Replace(auditRepositorySecretNames, " '[.secrets[].name]'", "", 1)),
			wantIn:   "[.secrets[].name]",
		},
		{
			name:     "an environment's secret names read without the filter",
			workflow: mutate(t, acceptedWorkflow, auditEnvironmentSecretNames, strings.Replace(auditEnvironmentSecretNames, " '[.secrets[].name]'", "", 1)),
			wantIn:   "[.secrets[].name]",
		},
		{
			name:     "read_api without its jq filter",
			workflow: mutate(t, acceptedWorkflow, auditReadAPI, strings.Replace(auditReadAPI, " --jq \"${filter}\"", "", 1)),
			wantIn:   "--jq",
		},
		{
			name:     "read_api without its filter's default",
			workflow: mutate(t, acceptedWorkflow, auditReadAPIFilter, strings.Replace(auditReadAPIFilter, " filter=\"${3:-.}\"", "", 1)),
			wantIn:   "filter",
		},

		// E. The containers that read release bytes have resource limits of their
		// own, as row 13's do.
		{
			name:     "row 11's container without its memory limit",
			workflow: mutate(t, acceptedWorkflow, auditRow11Create, strings.Replace(auditRow11Create, " --memory 512m", "", 1)),
			wantIn:   "--memory",
		},
		{
			name:     "row 11's container without its pid limit",
			workflow: mutate(t, acceptedWorkflow, auditRow11Create, strings.Replace(auditRow11Create, " --pids-limit 64", "", 1)),
			wantIn:   "--pids-limit",
		},
		{
			name:     "the fuzz container without its memory limit",
			workflow: mutate(t, acceptedWorkflow, auditRow14Create, strings.Replace(auditRow14Create, " --memory 10g", "", 1)),
			wantIn:   "--memory",
		},
		{
			name:     "the fuzz container without its pid limit",
			workflow: mutate(t, acceptedWorkflow, auditRow14Create, strings.Replace(auditRow14Create, " --pids-limit 512", "", 1)),
			wantIn:   "--pids-limit",
		},

		// F. No text of the image's reaches the public log: the tar that lists its
		// filesystem writes its own warnings to the results directory.
		{
			name:     "row 11's filesystem listing with the image's tar on the log",
			workflow: mutate(t, acceptedWorkflow, auditRuntimeExport, strings.Replace(auditRuntimeExport, " 2> \"${out}/filesystem.stderr\"", "", 1)),
			wantIn:   "filesystem.stderr",
		},

		// G. Fuzz discovery reads the Fuzz signature alone, and a target of an
		// unexpected shape is recorded, not a reason to end the step.
		{
			name:     "row 14's fuzz discovery matching a helper named Fuzzy too",
			workflow: mutate(t, acceptedWorkflow, auditFuzzDiscovery, "            { grep -oE '^func Fuzz[A-Za-z0-9_]*\\(' \"${file}\" || true; } | sed -e 's/^func //' -e 's/($//' | while IFS= read -r name; do\n"),
			wantIn:   "Fuzz[A-Z0-9_]",
		},
		{
			name:     "the fuzz target's shape guard ending the step",
			workflow: mutate(t, acceptedWorkflow, auditFuzzShapeGuard, strings.Replace(auditFuzzShapeGuard, "              continue\n", "              exit 1\n", 1)),
			wantIn:   "a target of an unexpected shape was not run",
		},
		{
			name:     "the fuzz target's shape guard without what it records",
			workflow: mutate(t, acceptedWorkflow, auditFuzzShapeGuard, strings.Replace(auditFuzzShapeGuard, "              echo \"fuzzing: a target of an unexpected shape was not run\" >> \"${results}/limits.txt\"\n", "", 1)),
			wantIn:   "a target of an unexpected shape was not run",
		},

		// H. trivy scans offline: no database, Java database, check bundle or
		// version download, and no telemetry, on both of its scans.
		{
			name:     "trivy without --skip-db-update",
			workflow: mutate(t, acceptedWorkflow, auditTrivyOffline, strings.Replace(auditTrivyOffline, " --skip-db-update", "", 1)),
			wantIn:   "--skip-db-update",
		},
		{
			name:     "trivy without --skip-java-db-update",
			workflow: mutate(t, acceptedWorkflow, auditTrivyOffline, strings.Replace(auditTrivyOffline, " --skip-java-db-update", "", 1)),
			wantIn:   "--skip-java-db-update",
		},
		{
			name:     "trivy without --skip-check-update",
			workflow: mutate(t, acceptedWorkflow, auditTrivyOffline, strings.Replace(auditTrivyOffline, " --skip-check-update", "", 1)),
			wantIn:   "--skip-check-update",
		},
		{
			name:     "trivy without --offline-scan",
			workflow: mutate(t, acceptedWorkflow, auditTrivyOffline, strings.Replace(auditTrivyOffline, " --offline-scan", "", 1)),
			wantIn:   "--offline-scan",
		},
		{
			name:     "trivy without --disable-telemetry",
			workflow: mutate(t, acceptedWorkflow, auditTrivyOffline, strings.Replace(auditTrivyOffline, " --disable-telemetry", "", 1)),
			wantIn:   "--disable-telemetry",
		},

		// Round E's first part: a verification that fails is a finding about the
		// release, not a check that did not run. Row 7's five verifications record
		// rc 1 as release-integrity verification-failed in the results, and a timeout
		// or any other failure in the limits.
		{
			name:     "row 7's checksum mismatch recorded as tool-failed",
			workflow: mutate(t, acceptedWorkflow, auditChecksumOutcome, auditWithoutFinding(auditChecksumOutcome)),
			wantIn:   "without its outcome branch",
		},
		{
			name:     "row 7's refused release-file attestation without its verification-failed record",
			workflow: mutate(t, acceptedWorkflow, auditReleaseFileOutcome, auditWithoutFinding(auditReleaseFileOutcome)),
			wantIn:   "without its outcome branch",
		},
		{
			// Round K's B1: a verification's timeout is the part's own limits line,
			// never the whole check recorded as one that did not run.
			name:     "row 7's SBOM attestation's timeout recorded as the whole check not running",
			workflow: mutate(t, acceptedWorkflow, auditSBOMOutcome, strings.Replace(auditSBOMOutcome, "              echo \"release-integrity: the SBOM's attestation timed out after 3 minutes\"", "              echo \"release-integrity timed-out\" >> \"${results}/not-run.txt\"\n              echo \"release-integrity: the SBOM's attestation timed out after 3 minutes\"", 1)),
			wantIn:   "in not-run.txt after a part ran",
		},
		{
			name:     "row 7's refused image signature without its verification-failed record",
			workflow: mutate(t, acceptedWorkflow, auditSignatureOutcome, auditWithoutFinding(auditSignatureOutcome)),
			wantIn:   "without its outcome branch",
		},
		{
			name:     "row 7's refused attestation signature without its verification-failed record",
			workflow: mutate(t, acceptedWorkflow, auditAttestedSigOutcome, auditWithoutFinding(auditAttestedSigOutcome)),
			wantIn:   "without its outcome branch",
		},

		// Round E's second part: no step's own timeouts reach its
		// timeout-minutes. Row 7's bound is the larger of the 11 minutes of timeouts
		// before its loop and that loop's 20-minute budget plus one iteration's 2,
		// plus the 18 minutes after the loop: 40 of its 50. Row 8's three timeouts
		// add up to 14 of its 15.
		{
			name:     "row 7's download timeout raised past its step's time-outs",
			workflow: mutate(t, acceptedWorkflow, "timeout 5m gh release download", "timeout 30m gh release download"),
			wantIn:   "past its timeout-minutes",
		},
		{
			name: "row 8's image inspect timeout raised past its step's time-outs",
			workflow: mutate(t, acceptedWorkflow, "            timeout 2m docker image inspect \"${image}@${digest}\" > \"${out}/inspect.json\"",
				"            timeout 6m docker image inspect \"${image}@${digest}\" > \"${out}/inspect.json\""),
			wantIn: "past its timeout-minutes",
		},
		{
			name:     "row 8's time-out lowered below its timeouts",
			workflow: mutate(t, acceptedWorkflow, "      - name: Pull the image anonymously by digest\n        timeout-minutes: 15\n", "      - name: Pull the image anonymously by digest\n        timeout-minutes: 13\n"),
			wantIn:   "past its timeout-minutes",
		},

		// Round E's third part: every step that can fail with one of PRD #175's
		// causes ends with an ::error naming the cause's kind and the runbook step
		// that fixes it, and no secret and no finding's detail.
		{
			name:     "no error line when the runner could not be hardened",
			workflow: mutate(t, acceptedWorkflow, auditHardeningError, ""),
			wantIn:   "Audit model::the runner could not be hardened",
		},
		{
			// An expired or revoked token, the usage limit, a failed preflight and
			// a managed-settings path read alike here: the launcher's line is a
			// constant class, so only a real run tells them apart.
			name:     "no error line when the audit did not run",
			workflow: mutate(t, acceptedWorkflow, auditRunError, ""),
			wantIn:   "Audit model::the audit did not run",
		},
		{
			name:     "no error line on the model job's unexpected artifact",
			workflow: mutate(t, acceptedWorkflow, auditModelArtifactError, ""),
			wantIn:   "Audit model::the run's artifacts are unexpected",
		},
		{
			name:     "no error line on the publish job's unexpected artifact",
			workflow: mutate(t, acceptedWorkflow, auditPublishArtifactError, ""),
			wantIn:   "Audit publish::the run's artifacts are unexpected",
		},
		{
			name:     "no error line on a refused publish",
			workflow: mutate(t, acceptedWorkflow, auditPublishError, ""),
			wantIn:   "Audit publish::the publish was refused",
		},

		// Round F's first part: every tool run of the checks job is under a
		// timeout of its own, with its exit recorded. Rows 6 to 15 already were,
		// and rows 3 to 5 - govulncheck, osv-scanner, gitleaks, zizmor and
		// actionlint - now are too: a tool that hangs is the check that timed out,
		// recorded "not run" in the results, never the end of the step and of the
		// run's report. A case takes the timeout out of one run of one row.
		{
			name:     "row 3's govulncheck without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditGovulncheckRun, strings.Replace(auditGovulncheckRun, "timeout 8m ", "", 1)),
			wantIn:   "govulncheck",
		},
		{
			name:     "row 4's osv-scanner without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditOSVScanRun, strings.Replace(auditOSVScanRun, "timeout 8m ", "", 1)),
			wantIn:   "osv-scanner",
		},
		{
			name:     "row 5's gitleaks without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditGitleaksRun, strings.Replace(auditGitleaksRun, "timeout 15m ", "", 1)),
			wantIn:   "gitleaks",
		},
		{
			name:     "row 5's zizmor without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditZizmorRun, strings.Replace(auditZizmorRun, "timeout 4m ", "", 1)),
			wantIn:   "zizmor",
		},
		{
			name:     "row 5's actionlint without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditActionlint, strings.Replace(auditActionlint, "timeout 5m ", "", 1)),
			wantIn:   "actionlint",
		},

		// Round G: rows 3 to 5's exit codes, read as the pinned tools' sources
		// write them. A branch that reads a tool's error as a finding records a
		// check that ran when none did, and one that reads a finding as an error
		// loses what the tool found. gitleaks is told --exit-code 2 because its
		// own default, 1, is an error's exit too: without the flag an error that
		// wrote no report reads as clean. zizmor exits by the highest finding's
		// severity, 11 to 14, and 1 on an error. govulncheck exits 0 with
		// -format json whatever it finds (only its text handler exits 3), so
		// every non-zero exit of it is the tool that failed.
		{
			name:     "row 5's gitleaks without --exit-code 2",
			workflow: mutate(t, acceptedWorkflow, auditGitleaksRun, strings.Replace(auditGitleaksRun, " --exit-code 2", "", 1)),
			wantIn:   "gitleaks",
		},
		{
			name:     "row 5's gitleaks reading an error as a leak",
			workflow: mutate(t, acceptedWorkflow, auditGitleaksOutcome, strings.Replace(auditGitleaksOutcome, `[ "${rc}" -ne 2 ]`, `[ "${rc}" -ne 1 ]`, 1)),
			wantIn:   "gitleaks",
		},
		{
			name:     "row 5's zizmor reading an error as a finding",
			workflow: mutate(t, acceptedWorkflow, auditZizmorOutcome, strings.Replace(auditZizmorOutcome, `{ [ "${rc}" -lt 11 ] || [ "${rc}" -gt 14 ]; }`, `[ "${rc}" -ne 1 ]`, 1)),
			wantIn:   "zizmor",
		},
		{
			name:     "row 3's govulncheck excusing exit 3",
			workflow: mutate(t, acceptedWorkflow, auditGovulncheckOutcome, strings.Replace(auditGovulncheckOutcome, `elif [ "${rc}" -ne 0 ]; then`, `elif [ "${rc}" -ne 0 ] && [ "${rc}" -ne 3 ]; then`, 1)),
			wantIn:   "govulncheck",
		},

		// Round F's second part: the model job's install step names the runbook
		// step a CLI that does not install is recovered by. The shared script's own
		// two ::error lines name none, so the workflow holds the branch whole: the
		// echo, the exit 1 and the fi, each on a line of its own.
		{
			name:     "the CLI install without its guard",
			workflow: mutate(t, acceptedWorkflow, auditInstallGuard, auditInstallCall),
			wantIn:   "the Claude Code CLI did not install",
		},
		{
			name:     "the CLI install guard without its exit 1",
			workflow: mutate(t, acceptedWorkflow, auditInstallGuard, strings.Replace(auditInstallGuard, "            exit 1\n", "", 1)),
			wantIn:   "the Claude Code CLI did not install",
		},
		{
			name:     "the CLI install guard's branch run together onto the echo's line",
			workflow: mutate(t, acceptedWorkflow, auditInstallGuard, joinedBranch(auditInstallGuard)),
			wantIn:   "the Claude Code CLI did not install",
		},

		// Round I: the reviewer's and the auditor's findings on PR 5c, each case
		// changing one of them back. A refusal the tool reports with an exit code
		// that the branch does not read is a finding lost to "tool-failed"; a loop
		// without a budget, and a docker command without a timeout, is a step that
		// its own timeout ends, the encrypt step with it; a check that did not run
		// is recorded in not-run.txt, never left silent; and no content the release
		// or the API answers with may end the step.
		{
			name:     "row 7's cosign verify reading a refused signature as tool-failed",
			workflow: mutate(t, acceptedWorkflow, auditSignatureFinding, auditSignatureFindingBefore),
			wantIn:   `runs "timeout 3m cosign verify`,
		},
		{
			name:     "row 9's binary-vulnerabilities loop without its budget",
			workflow: mutate(t, acceptedWorkflow, auditBinaryLoopBudget, ""),
			wantIn:   "no loop budget",
		},
		{
			// M1's rule holds every loop that runs a command under a timeout, in any
			// step: a retried pull with no budget is a loop the step's timeout ends.
			name: "row 8's pull retried in a loop with a timeout but no budget",
			workflow: mutate(t, acceptedWorkflow, auditRow8Pull, "          rc=0\n"+
				"          for attempt in 1 2 3; do\n"+
				"            timeout 8m docker pull --platform linux/amd64 \"${image}@${digest}\" > \"${out}/pull.stdout\" 2> \"${out}/pull.stderr\" || rc=$?\n"+
				"            [ \"${rc}\" -eq 0 ] && break\n"+
				"          done\n"+
				"          echo \"${rc}\" > \"${out}/pull.exit\"\n"),
			wantIn: "no loop budget",
		},
		{
			name:     "row 14's staging extraction without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTree, strings.Replace(auditFuzzTree, "timeout 2m tar", "tar", 1)),
			wantIn:   "runs tar without its timeout",
		},
		{
			name:     "row 14's staging copy without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditFuzzStagingCopy, strings.Replace(auditFuzzStagingCopy, "timeout 2m docker cp", "docker cp", 1)),
			wantIn:   "runs docker cp without its timeout",
		},
		{
			name:     "row 14's staging copy without its exit code check",
			workflow: mutate(t, acceptedWorkflow, auditFuzzStagingCopy, strings.Replace(auditFuzzStagingCopy, " || rc=$?", "", 1)),
			wantIn:   "runs docker cp without || rc=$?",
		},
		{
			name:     "row 14's fuzz loop without its budget",
			workflow: mutate(t, acceptedWorkflow, auditFuzzLoopBudget, ""),
			wantIn:   "no loop budget",
		},
		{
			name:     "row 11's exit state read without its timeout",
			workflow: mutate(t, acceptedWorkflow, auditRuntimeState, strings.Replace(auditRuntimeState, "timeout 1m docker inspect", "docker inspect", 1)),
			wantIn:   "runs docker inspect without its timeout",
		},
		{
			name: "row 7's checks run though the release download failed",
			workflow: mutate(t, mutate(t, acceptedWorkflow, auditReleaseFileGate, ""),
				auditReleaseFileGateEnd, strings.TrimSuffix(auditReleaseFileGateEnd, "          fi\n")),
			wantIn: `[ "${rel_ok}" -eq 1 ]; then gate`,
		},
		{
			name:     "scorecard's failed run recorded nowhere",
			workflow: mutate(t, acceptedWorkflow, auditScorecardToolFailed, ""),
			wantIn:   "without its outcome branch",
		},
		{
			name:     "row 8's failed image pull recorded nowhere",
			workflow: mutate(t, acceptedWorkflow, auditImagePullToolFailed, "            echo \"image-pull timed out after 8 minutes\" >> \"${results}/limits.txt\"\n          fi\n"),
			wantIn:   "without its outcome branch",
		},
		{
			name:     "row 10's failed image scan recorded nowhere",
			workflow: mutate(t, acceptedWorkflow, auditTrivyImageToolFailed, "              echo \"image-scan: the image scan timed out after 8 minutes\" >> \"${results}/limits.txt\"\n            fi\n"),
			wantIn:   "without its outcome branch",
		},
		{
			name:     "row 10's failed SBOM scan recorded nowhere",
			workflow: mutate(t, acceptedWorkflow, auditTrivySBOMToolFailed, "              echo \"image-scan: the SBOM scan timed out after 4 minutes\" >> \"${results}/limits.txt\"\n            fi\n"),
			wantIn:   "without its outcome branch",
		},
		{
			name:     "row 10's timed-out image scan recorded only in the limits",
			workflow: mutate(t, acceptedWorkflow, auditTrivyImageTimedOut, "              echo \"image-scan: the image scan timed out after 8 minutes\" >> \"${results}/limits.txt\"\n"),
			wantIn:   "without its outcome branch",
		},
		{
			name:     "row 9's binary search counting an empty file",
			workflow: mutate(t, acceptedWorkflow, "-size +0 ", ""),
			wantIn:   "-size +0",
		},
		{
			name:     "row 7's latest image digest read without its shape check",
			workflow: mutate(t, acceptedWorkflow, auditLatestDigestShape, ""),
			wantIn:   "before latest.txt is written",
		},
		{
			name:     "row 7's latest release tag read without its shape check",
			workflow: mutate(t, acceptedWorkflow, auditLatestReleaseShape, ""),
			wantIn:   "before latest.txt is written",
		},
		{
			name:     "row 7's unresolved image digest leaving the image unverified",
			workflow: mutate(t, acceptedWorkflow, auditNoDigestRecord, "          fi\n"),
			wantIn:   "has no else that records what it skipped",
		},
		{
			name:     "the ruleset ID guard ending the step",
			workflow: mutate(t, acceptedWorkflow, auditRulesetGuard, auditRulesetExit),
			wantIn:   "the ruleset ID's shape guard",
		},

		// Round J: a step's own time bound counted the whole way. A loop's
		// `${SECONDS}` budget stops only the iterations that have not begun, so the
		// iteration that began just before it runs to its own timeouts: the step's
		// bound is the larger of the timeouts before the loop and the budget plus
		// one iteration's timeouts, plus the timeouts after it. A shell function's
		// timeout counts once per call, so a function the step calls many times
		// must hold a budget of its own, which then stands in the calls' place.
		{
			name:     "row 13's run loop's budget past its step's last iteration",
			workflow: mutate(t, acceptedWorkflow, auditHostileLoopBudget, strings.Replace(auditHostileLoopBudget, "-ge 720", "-ge 840", 1)),
			wantIn:   "1260",
		},
		{
			name:     "row 9's binary-vulnerabilities budget past its step's last scan",
			workflow: mutate(t, acceptedWorkflow, auditBinaryLoopBudget, strings.Replace(auditBinaryLoopBudget, "-ge 720", "-ge 790", 1)),
			wantIn:   "1210",
		},
		{
			name:     "row 15's read_api without its budget",
			workflow: mutate(t, acceptedWorkflow, auditReadAPIBudget, ""),
			wantIn:   "with no function budget",
		},
		{
			name:     "row 15's read_api's budget past its step's last read",
			workflow: mutate(t, acceptedWorkflow, auditReadAPIBudget, strings.Replace(auditReadAPIBudget, "-ge 720", "-ge 790", 1)),
			wantIn:   "910",
		},
		{
			// The function's budget ends the reads it did not reach, so they are no
			// time at all: the step is held by read_api's 12 minutes and one read's 2,
			// not by 33 reads of 2.
			name: "row 15's read_api called more times than one call's timeouts allow",
			workflow: mutate(t, acceptedWorkflow, auditRepositoryVariableNames, "          read_api repository-labels \"repos/${GH_REPO}/labels\"\n"+
				"          read_api repository-topics \"repos/${GH_REPO}/topics\"\n"+
				"          read_api repository-languages \"repos/${GH_REPO}/languages\"\n"+
				"          read_api repository-contributors \"repos/${GH_REPO}/contributors\"\n"+
				"          read_api repository-branches \"repos/${GH_REPO}/branches\"\n"+
				"          read_api repository-tags \"repos/${GH_REPO}/tags\"\n"+
				"          read_api repository-milestones \"repos/${GH_REPO}/milestones\"\n"+
				"          read_api repository-releases \"repos/${GH_REPO}/releases\"\n"+
				auditRepositoryVariableNames),
		},

		// Round K: the reviewer's B1, B2, L4 and S2 findings, and the fit
		// tripwire's holes (L2, S1). A check of several parts records each part's own
		// outcome in the limits, with its exit file, and the check itself as one that
		// did not run only when no part produced a result at all; a not-run line
		// ends in a why id the publish core's vocabulary holds; a malformed
		// checksums.txt is a finding about the release; a read the step's budget ran
		// out on is no read the API refused; and every timeout, loop and inner loop
		// fit counts is one it can follow.
		{
			name:     "row 13's per-run create timeout written to not-run.txt",
			workflow: mutate(t, acceptedWorkflow, auditHostileRunCreate, auditHostileRunCreateWhole),
			wantIn:   "in not-run.txt inside its loop",
		},
		{
			name:     "row 14's per-target create timeout written to not-run.txt",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTargetCreate, auditFuzzTargetCreateWhole),
			wantIn:   "in not-run.txt inside its loop",
		},
		{
			name:     "row 7's one file's refused attestation written as the whole check not running",
			workflow: mutate(t, acceptedWorkflow, auditReleaseFileOutcome, auditReleaseFileOutcomeWhole),
			wantIn:   "in not-run.txt inside its loop",
		},
		{
			name:     "row 7's unresolved image digest written as the whole check not running",
			workflow: mutate(t, acceptedWorkflow, auditNoDigestRecord, auditNoDigestNotRun),
			wantIn:   "in not-run.txt after a part ran",
		},
		{
			name:     "row 7's no-part-ran record removed",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord, ""),
			wantIn:   "has no record of a check none of whose parts ran",
		},
		{
			name:     "row 13's no-part-ran record removed",
			workflow: mutate(t, acceptedWorkflow, auditHostileEndRecord, ""),
			wantIn:   "has no record of a check none of whose parts ran",
		},
		{
			name:     "row 14's no-part-ran record removed",
			workflow: mutate(t, acceptedWorkflow, auditFuzzEndRecord, ""),
			wantIn:   "has no record of a check none of whose parts ran",
		},
		{
			// B2: unexpected-ruleset is no why id the publish core holds, and a
			// ruleset ID of another shape is no reason to write the check as one that
			// did not run.
			name: "the ruleset ID written to not-run.txt with an id no vocabulary holds",
			workflow: mutate(t, acceptedWorkflow, auditRulesetGuard, "            if [[ ! \"${id}\" =~ ^[0-9]+$ ]]; then\n"+
				"              echo \"supply-chain unexpected-ruleset\" >> \"${results}/not-run.txt\"\n"+
				"              continue\n"+
				"            fi\n"),
			wantIn: "is no why id",
		},
		{
			name:     "row 7's malformed checksums.txt recorded nowhere",
			workflow: mutate(t, acceptedWorkflow, auditMalformedChecksums, "            ran_any=1\n"),
			wantIn:   "for a checksums.txt with no well-formed line",
		},
		{
			// S2: the budget ran out on these reads; nothing refused them, so they
			// are no read the API refused and the maintainer has no permission to
			// look for.
			name:     "row 15's budget skips written to read-refused.txt",
			workflow: mutate(t, acceptedWorkflow, auditReadAPIBudget, strings.Replace(auditReadAPIBudget, "> \"${out}/read-skipped.txt\"", "> \"${out}/read-refused.txt\"", 1)),
			wantIn:   "read-skipped.txt",
		},

		// Fit's holes (round K, the auditor's L2 and the reviewer's S1): a timeout
		// fit cannot read as whole minutes or seconds, a loop whose header it cannot
		// follow, an inner loop in a budgeted body that nothing bounds, and a timeout
		// a command runs once per argument. None of them is counted today, so a step
		// whose own timeouts outrun its timeout-minutes is accepted.
		{
			name:     "row 14's tree extraction under timeout 1h",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTree, strings.Replace(auditFuzzTree, "timeout 2m tar", "timeout 1h tar", 1)),
			wantIn:   "is no whole number of minutes or seconds",
		},
		{
			name:     "row 14's tree extraction under timeout -k 5 50m",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTree, strings.Replace(auditFuzzTree, "timeout 2m tar", "timeout -k 5 50m tar", 1)),
			wantIn:   "is no whole number of minutes or seconds",
		},
		{
			name:     "row 14's tree extraction under timeout 1.5m",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTree, strings.Replace(auditFuzzTree, "timeout 2m tar", "timeout 1.5m tar", 1)),
			wantIn:   "is no whole number of minutes or seconds",
		},
		{
			name: "row 7's loop whose body and header share a line",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          for f in one two; do timeout 1m cat \"${out}/image-digest.txt\" > /dev/null || true; done\n"+
					auditReleaseEndRecord),
			wantIn: "on one line",
		},
		{
			name: "row 7's loop with do on a line of its own",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          for f in one two\n"+
					"          do\n"+
					"            timeout 1m cat \"${out}/image-digest.txt\" > /dev/null || true\n"+
					"          done\n"+
					auditReleaseEndRecord),
			wantIn: "do on a line of its own",
		},
		{
			name: "row 14's unbudgeted inner loop in the budgeted target loop",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTargetRm,
				"            fi\n"+
					"            for attempt in $(seq 1 20); do\n"+
					"              timeout 1m docker rm -f \"${cid}\" > /dev/null || true\n"+
					"            done\n"),
			wantIn: "inner loop with a timeout",
		},
		{
			name: "row 7's timeout run once per argument by xargs",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          printf '%s\\n' one two | xargs -n1 timeout 1m cat > /dev/null || true\n"+
					auditReleaseEndRecord),
			wantIn: "xargs",
		},

		// Round L: the reviewer's K1, K2, K3, K4 and K6 findings and the auditor's
		// L1. A check of several parts is recorded as one that did not run only at
		// its end (K1); a run or a target counts only once it started (K2); and every
		// loop header, timeout per argument, quoted timeout and finding-candidate
		// line the accepted shape writes is one fit follows or refuses (K3, K4/L1,
		// K6).
		{
			name:     "row 7's download not-run line written in its branch again",
			workflow: mutate(t, acceptedWorkflow, auditReleaseDownloadBranches, auditReleaseDownloadNotRunBranches),
			wantIn:   "not-run.txt",
		},
		{
			name:     "row 13's failed create counted as a run that ran",
			workflow: mutate(t, acceptedWorkflow, auditHostileRunCreateFixed, auditHostileRunCreateFailed),
			wantIn:   "ran_any",
		},
		{
			name:     "row 14's failed target create counted as a target that ran",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTargetCreateFixed, auditFuzzTargetCreateFailed),
			wantIn:   "ran_any",
		},
		{
			name:     "row 13's started run left uncounted",
			workflow: mutate(t, acceptedWorkflow, auditHostileRunStart, auditHostileRunStartUnmarked),
			wantIn:   "ran_any",
		},
		{
			name:     "row 14's started target left uncounted",
			workflow: mutate(t, acceptedWorkflow, auditFuzzTargetStart, auditFuzzTargetStartUnmarked),
			wantIn:   "ran_any",
		},
		{
			// K3: the loop word after && is no loop start, so its body was counted
			// as lines that run once.
			name: "row 7's loop header after &&",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          cd \"${out}\" && for f in $(ls); do\n"+
					"            timeout 30 true\n"+
					"          done\n"+
					auditReleaseEndRecord),
			wantIn: "fit cannot follow",
		},
		{
			name: "row 7's loop header after ;",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          cd \"${out}\"; for f in $(ls); do\n"+
					"            timeout 30 true\n"+
					"          done\n"+
					auditReleaseEndRecord),
			wantIn: "fit cannot follow",
		},
		{
			// K4 and the auditor's L1: a timeout xargs or find runs once per
			// argument, and one inside a quoted sh -c string.
			name: "row 7's timeout run once per argument by xargs without -n1",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          printf '%s\\n' one two | xargs timeout 1m cat > /dev/null || true\n"+
					auditReleaseEndRecord),
			wantIn: "xargs",
		},
		{
			name: "row 7's timeout run once per argument by find -exec",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          find . -type f -exec timeout 1m cat {} + > /dev/null || true\n"+
					auditReleaseEndRecord),
			wantIn: "-exec",
		},
		{
			name: "row 7's timeout run once per argument by find -execdir",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          find . -type f -execdir timeout 1m cat {} + > /dev/null || true\n"+
					auditReleaseEndRecord),
			wantIn: "-execdir",
		},
		{
			name: "row 7's timeout in a quoted sh -c string",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          sh -c 'timeout 50m true'\n"+
					auditReleaseEndRecord),
			wantIn: "timeout 50m",
		},
		{
			name: "row 7's timeout in a quoted sh -c string under find -exec",
			workflow: mutate(t, acceptedWorkflow, auditReleaseEndRecord,
				"          find \"${out}\" -type f -exec sh -c 'timeout 50m true' \\;\n"+
					auditReleaseEndRecord),
			wantIn: "timeout 50m",
		},
		{
			// K6: row 13's finding-candidate limits line, held whole as row 7's
			// malformed-checksums record is.
			name:     "row 13's finding-candidate limits line without its text",
			workflow: mutate(t, acceptedWorkflow, auditHostileRunFindingCandidate, "echo \"hostile-inventories: ${run} timed out after 60 seconds\" >> \"${results}/limits.txt\""),
			wantIn:   "a finding candidate of the run",
		},

		// Round M: the reviewer's S1 and S2 and the auditor's L-1 and L-2. A step's
		// not-run record ends the step only in one of the two shapes the accepted
		// shape writes (M1): the record, its branch's fi and then the step's own
		// if [ "${rc}" -ne 0 ]; then, echo "finished" and exit 0, with no other line
		// but a message to the limits or the log between them; and the why such an
		// end writes is a why id the step assigns in why=<why id> and nowhere else
		// (M2).
		{
			name: "row 13's finding beside the staging's not-run line",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreateEnd,
				strings.Replace(auditHostileStagingCreateEnd,
					"            echo \"hostile-inventories tool-failed\" >> \"${results}/not-run.txt\"\n",
					"            echo \"hostile-inventories tool-failed\" >> \"${results}/not-run.txt\"\n"+auditHostileVerificationFailed, 1)),
			wantIn: "verification-failed.txt",
		},
		{
			name: "row 14's finding beside the staging's not-run line",
			workflow: mutate(t, acceptedWorkflow, auditFuzzStagingCreateEnd,
				strings.Replace(auditFuzzStagingCreateEnd,
					"            echo \"fuzzing tool-failed\" >> \"${results}/not-run.txt\"\n",
					"            echo \"fuzzing tool-failed\" >> \"${results}/not-run.txt\"\n"+auditFuzzVerificationFailed, 1)),
			wantIn: "verification-failed.txt",
		},
		{
			name: "row 13's staging guard narrowed to the timeout",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreateEnd,
				strings.Replace(auditHostileStagingCreateEnd,
					"          if [ \"${rc}\" -ne 0 ]; then",
					"          if [ \"${rc}\" -eq 124 ]; then", 1)),
			wantIn: "-eq 124",
		},
		{
			name: "row 13's staging record ending the step in another branch",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreateEnd,
				strings.Replace(auditHostileStagingCreateEnd,
					"          fi\n          if [ \"${rc}\" -ne 0 ]; then",
					"          else\n            exit 0\n          fi\n          if [ \"${rc}\" -ne 0 ]; then", 1)),
			wantIn: "else",
		},
		{
			name: "row 13's staging with a never-taken branch before the step's guard",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreateEnd,
				strings.Replace(auditHostileStagingCreateEnd,
					"          fi\n          if [ \"${rc}\" -ne 0 ]; then",
					"          fi\n          if [ \"${rc}\" -eq 999 ]; then\n            exit 0\n          fi\n          if [ \"${rc}\" -ne 0 ]; then", 1)),
			wantIn: "-eq 999",
		},
		{
			name: "row 13's step guard with a command substitution",
			workflow: mutate(t, acceptedWorkflow, auditHostileStagingCreateEnd,
				strings.Replace(auditHostileStagingCreateEnd,
					"          if [ \"${rc}\" -ne 0 ]; then\n            echo \"finished\"\n            exit 0",
					"          if [ \"${rc}\" -ne \"$(printf 0)\" ]; then\n            echo \"finished\"\n            exit 0", 1)),
			wantIn: "$(printf 0)",
		},
		{
			name:     "row 7's download's stderr read into why",
			workflow: mutate(t, acceptedWorkflow, auditReleaseFileGate, "          read -r why < \"${out}/download.stderr\" || true\n"+auditReleaseFileGate),
			wantIn:   "read -r why",
		},
		{
			name:     "row 7's why set by printf -v",
			workflow: mutate(t, acceptedWorkflow, auditReleaseFileGate, "          printf -v why '%s' x\n"+auditReleaseFileGate),
			wantIn:   "printf -v why",
		},
		{
			name: "row 7's why taken as a loop's variable",
			workflow: mutate(t, acceptedWorkflow, auditReleaseFileGate,
				"          for why in a; do\n"+
					"            echo \"a part that did not run\"\n"+
					"          done\n"+
					auditReleaseFileGate),
			wantIn: "for why in a",
		},
		{
			name:     "row 7's why set by an assignment expansion",
			workflow: mutate(t, acceptedWorkflow, auditReleaseFileGate, "          : \"${why:=tool-failed}\"\n"+auditReleaseFileGate),
			wantIn:   "${why:=tool-failed}",
		},
		{
			name: "row 7's step-level env setting why",
			workflow: mutate(t, acceptedWorkflow, auditReleaseStepEnv,
				strings.Replace(auditReleaseStepEnv, "        run: |\n", "          why: tool-failed\n        run: |\n", 1)),
			wantIn: "is not one variable this reader understands",
		},

		// Round N: the AI review's points 4 and 10 on PR #565. Row 15 reads the open
		// Dependency Dashboard issue alone, its pull requests filtered out, and says
		// so in the limits when only pull requests answered; and row 9 records an
		// archive whose unpack failed as the check that timed out or did not run,
		// names the exit (141, the 128 MiB cap; 124, the 2-minute timeout) in the
		// limits, and goes on to the next archive without scanning a member that was
		// only partly written.
		{
			name:     "row 15's Dependency Dashboard read without state=open",
			workflow: mutate(t, acceptedWorkflow, auditRenovateDashboardRead, strings.Replace(auditRenovateDashboardRead, "issues?state=open&", "issues?state=all&", 1)),
			wantIn:   "state=open",
		},
		{
			name:     "row 15's Dependency Dashboard read without its pull-request filter",
			workflow: mutate(t, acceptedWorkflow, auditRenovateDashboardRead, strings.Replace(auditRenovateDashboardRead, " '[.[] | select(.pull_request == null)]'", "", 1)),
			wantIn:   "select(.pull_request == null)",
		},
		{
			name:     "row 15's Dependency Dashboard read that found no issue recorded nowhere",
			workflow: mutate(t, acceptedWorkflow, auditRenovateDashboardMissing, ""),
			wantIn:   "no open Renovate Dependency Dashboard issue was found",
		},
		{
			name:     "row 9's failed unpack scanning the member it partly wrote",
			workflow: mutate(t, acceptedWorkflow, auditBinaryUnpackFailure, strings.Replace(auditBinaryUnpackFailure, "              continue\n", "", 1)),
			wantIn:   "continue",
		},
		{
			name: "row 9's failed unpack named in no limits line",
			workflow: mutate(t, acceptedWorkflow, auditBinaryUnpackFailure,
				strings.Replace(auditBinaryUnpackFailure, "              echo \"binary-vulnerabilities: ${name} was not scanned: its unpack exited ${rc} (141: the 128 MiB cap, 124: the 2-minute timeout)\" >> \"${results}/limits.txt\"\n", "", 1)),
			wantIn: "was not scanned: its unpack exited",
		},

		// A document the reader cannot follow tells the check nothing, so it is
		// refused rather than passed.
		{name: "an empty document", workflow: "", wantIn: "empty"},
		{name: "a document that is no YAML", workflow: "\ton:\n", wantIn: "read"},
	}

	// Every tool download's checksum check, step by step and download by
	// download: a whole download step's guards put back to the bare sha256sum
	// lines, left without their exit 1 or run together onto the echo's line, and
	// then each step's downloads one by one, so that no download is left
	// reporting sha256sum's own line alone (PRD #175, "A failed run is
	// visible").
	for _, job := range []string{"checks", "model", "publish"} {
		for _, breakage := range auditChecksumBreakages {
			tests = append(tests, auditWorkflowCase{
				name:     "every checksum guard of the " + job + " job " + breakage.name,
				workflow: breakChecksums(t, acceptedWorkflow, checksumsOf(job), breakage.of),
				wantIn:   "sha256sum",
			})
		}
	}

	for _, download := range auditChecksums {
		tests = append(tests, auditWorkflowCase{
			name:     "the " + download.job + " job's " + download.tool + " download without its checksum guard",
			workflow: breakChecksums(t, acceptedWorkflow, []auditChecksum{download}, auditChecksumBare),
			wantIn:   "sha256sum",
		})
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

			fresh := auditFresh(t, accepted, got)
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
