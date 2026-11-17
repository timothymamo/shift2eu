---
name: release-tag
description: Review the release PR that release-please keeps open on main, and after it is merged verify the vX.Y.Z tag, the GoReleaser release run and the signed images it publishes. Use when asked to release, publish a version, review the release PR or check a release.
---

# Cut a release

Releases are proposed, not tagged by hand (issue #142, ADR 0021). On every push to `main`,
`.github/workflows/release-please.yml` keeps one **release PR** open, labelled `autorelease: pending`,
whose version and `CHANGELOG.md` entry release-please computes from the Conventional Commits since the
last release (`release-please-config.json`). Merging that PR is the decision to ship: the same workflow
then creates the `vX.Y.Z` tag on the merge commit with the **release-tag App**'s token, in a job of its
own, and relabels the PR `autorelease: tagged` with the **release-please App**'s token, the App that
opened it (issue #157: two Apps, so the token the third-party release-please action holds can create no
release tag). The tag starts `.github/workflows/release.yml`: GoReleaser publishes the
binaries, then the image, sbom and sign jobs the signed images `ghcr.io/timothymamo/shift2eu` and
`ghcr.io/timothymamo/shift2eu-server`, and the `verify` job checks them (ADR 0015). This skill reviews
the release PR and reports on it, and after the merge verifies the tag, the release run and the
published images. It never chooses a version itself, never edits the release PR, and never creates,
moves or deletes a tag.

1. In the repository root, which always stays on `main` (check
   `git -C <repository root> branch --show-current`, and stop if it is not), run
   `git -C <repository root> pull --ff-only` and `git -C <repository root> fetch --tags`. The tree must
   be clean and CI on `main` green (`gh run list --branch main --limit 3`). This flow commits nothing,
   so it runs here: every git command in it is `git -C <repository root> ...`.
2. Confirm the `v*` tag rulesets and the release PR's branch ruleset before every release; stop if they
   fall short (ADR 0015, ADR 0021).
   The ancestry guard in `release.yml` only catches mistakes: the run uses the tagged commit's workflow
   file, so the rulesets are what keep anyone else from publishing a signed image.
   The two Apps are compared against the identifiers recorded here and in ADR 0021 (amendment), never
   looked up: the agents' token cannot read them (`gh api user/installations` answers 403 to a PAT or
   `gh`'s OAuth token, `gh api apps/<slug>` answers 404 for these private Apps, and environment variables
   answer 403). They are public identifiers, not secrets:

   | App | App ID | Slug | Bot login |
   |---|---|---|---|
   | release-please App | `5729776` | `shift2eu-release` | `app/shift2eu-release` (`gh`), `shift2eu-release[bot]` (REST API) |
   | release-tag App | `5734263` | `shift2eu-release-tag` | `app/shift2eu-release-tag` (`gh`), `shift2eu-release-tag[bot]` (REST API) |

   **Fail closed:** if the release-tag App's ID above still reads `<RELEASE_TAG_APP_ID: ...>`, or is not
   a number, report "the release-tag App's ID is not recorded in the release-tag skill and ADR 0021: a
   maintainer must record it before any release" and stop. If the release-tag App's recorded ID or slug is the
   release-please App's ID or slug in the table above, report "the release-tag App recorded in the release-tag skill and ADR 0021 is the release-please App:
   a maintainer must record the release-tag App's own identifiers" and stop: the App that opens the
   release PR must never be the one that may bypass the tag ruleset. `check.ReleaseAppIdentifiers`
   holds the two records to each other and to these rules in CI.
   From the repository root, list the rulesets with `gh api repos/{owner}/{repo}/rulesets`, then read
   each one with `target` `tag` (`gh api repos/{owner}/{repo}/rulesets/<id>`). Take the **union of the
   active ones** (`enforcement` `active`) whose `conditions.ref_name.include` covers `v*` tags
   (`refs/tags/v*`, `refs/tags/v*.*.*` or `~ALL`) and that exclude no `v*` tag in
   `conditions.ref_name.exclude`:
   - creation, update and deletion (rules `creation`, `update`, `deletion`) must each be restricted
     by at least one of them;
   - a ruleset holding `update` or `deletion` may be bypassed by maintainers only (`bypass_actors`
     of `actor_type` `RepositoryRole` with the admin role, `actor_id` 5, or `OrganizationAdmin`);
   - a ruleset holding only `creation` may also be bypassed by the release-tag App, and by nothing
     else: its `bypass_actors` of `actor_type` `Integration` must be **exactly one**, whose `actor_id`
     is the release-tag App ID recorded above. The release-please App (`5729776`) or any other App
     there, alone or beside the release-tag App, fails the check: while both Apps are bypass actors (the
     maintainer's switch is not finished), the check fails as intended and no release is cut.
   The expected split is `Release tag creation` (creation; Repository admin and the release-tag App) and
   `Release tags` (update, deletion, non_fast_forward; Repository admin only): a bypass list applies to
   every rule of its ruleset, so the App sits only in the one that restricts nothing but creation. Any
   other bypass actor, an App that may bypass update or deletion, or a missing rule fails the check: report
   what you found, that a maintainer must fix it (an agent must not), and stop.
   Then confirm the release PR's branch ruleset (issue #158, ADR 0021): the tag step trusts a merged PR
   from release-please's branch, so only the release-please App may write that branch. Read each ruleset
   of the list with `target` `branch`. There must be **exactly one** that is active (`enforcement`
   `active`) and whose `conditions.ref_name.include` lists `refs/heads/release-please--branches--*`, and
   it must hold all of this:
   - `conditions.ref_name.include` is exactly `["refs/heads/release-please--branches--*"]` and
     `conditions.ref_name.exclude` is empty: a wider pattern (`~ALL`, `refs/heads/*`) would let the
     release-please App bypass it on other branches too, and an exclude could leave its branch out;
   - its `rules` include `creation`, `update` and `deletion` (others, such as `non_fast_forward`, may be
     there too);
   - its `bypass_actors` are **exactly two**, both with `bypass_mode` `always`: Repository admin
     (`actor_type` `RepositoryRole`, `actor_id` 5) and the release-please App (`actor_type`
     `Integration`, `actor_id` the release-please App ID recorded above, `5729776`). The release-tag
     App, any other App, team, role or user, a missing actor, or an empty list (which is also what a
     token that may not read the bypass list sees) fails the check.
   No such ruleset, more than one, or any of these not held: report the rulesets you found (`id`, `name`,
   `enforcement`, `conditions`, `rules`, `bypass_actors`), that a maintainer must create or fix it as ADR
   0021 describes (an agent must not), and stop.
   Then check both environments, `release-please` (the release-please App's key) and `release-tag` (the
   release-tag App's key): for each `<env>`, `gh api repos/{owner}/{repo}/environments/<env>` must show
   `deployment_branch_policy.custom_branch_policies` `true` (and `protected_branches` `false`), and
   `gh api repos/{owner}/{repo}/environments/<env>/deployment-branch-policies --jq '.branch_policies[] | .type + " " + .name'`
   must list exactly one policy, `branch main`. Anything else lets a workflow on another branch or tag
   read an App's key: report it and stop.
   Last, neither App's private key nor client ID may exist at repository level, only in its
   environment: `gh api repos/{owner}/{repo}/actions/secrets --jq '.secrets[].name'` and
   `gh api repos/{owner}/{repo}/actions/variables --jq '.variables[].name'` must list none of
   `RELEASE_APP_PRIVATE_KEY`, `RELEASE_TAG_APP_PRIVATE_KEY`, `RELEASE_APP_CLIENT_ID`,
   `RELEASE_TAG_APP_CLIENT_ID`, `RELEASE_APP_ID`. The agents' token may not be allowed to list them (a
   403): then say in the report that this is a maintainer check you could not make, and go on. If one of
   them is listed, report it and stop.
   **The rulesets' bypass lists after the switch.** The agents App's token cannot read `bypass_actors`
   (it needs Administration write, which the App never gets: ADR 0031, d): GitHub leaves the field out,
   or returns it empty. The checks above that hold a ruleset's `bypass_actors` are then a maintainer
   check you could not make: report the rulesets as you read them, ask the maintainer to confirm their
   bypass lists against this step with their own token, and do not go on to step 5's merge until they
   have. Never count a missing or empty list as passing.
   Then run the `Protect main` gate check from the repository root: `make gate-check`
   (`dev/protect-main-check.sh`; PRD #161). It reads `Protect main` and `main`'s `.github/CODEOWNERS`
   and lists each gap: fewer than 1 required approval, `require_code_owner_review`,
   `dismiss_stale_reviews_on_push` or `require_last_push_approval` not `true`, a bypass actor, or a
   code owner other than the maintainer. Until milestone 3 of PRD #161 turns it off, the Makefile runs
   it with the transition flag (`GATE_TRANSITION=1`, `--transition`), which reports the gaps without
   failing. Act on its exit status:
   - **0**: the gate holds, or the transition flag is on: put every gap line it printed in the report and
     go on;
   - **1**: the gate has gaps and the transition is over: report the gaps and stop the release;
   - **2**: it could not check (no access, an unexpected answer): report its output and stop.
   A gap saying the bypass list could not be read is not yours to settle: the App's token cannot read it
   (above). Hand it to the maintainer to check with their own token, and say so in the report.
3. Find the open release PR:
   `gh pr list --state open --label "autorelease: pending" --json number,title,headRefName,url`.
   There must be exactly one, from the branch `release-please--branches--main--components--shift2eu`
   (exactly; ADR 0021 says why), opened by the release-please App itself, not just any bot. Read the author
   with `gh pr view <n> --json author --jq '.author.is_bot, .author.login'`: it must print `true` and
   exactly `app/shift2eu-release` (`gh`'s form of the REST API's `shift2eu-release[bot]`, which a `gh`
   version may print instead and is equally accepted). Compare the login as a whole string against
   these two; never build a command from it. A human author, any other login, or another bot: report it
   and stop. If there is no open release PR, check for a merged one still waiting for its tag:
   `gh pr list --state merged --label "autorelease: pending" --json number,title,author,headRefName,mergeCommit`.
   Hold it to the same checks as an open one: exactly one, its `headRefName` exactly
   `release-please--branches--main--components--shift2eu`, its author the release-please App (`is_bot`
   `true`, login exactly `app/shift2eu-release` or `shift2eu-release[bot]`). If it passes, go to step 6
   for it; if it fails any check, or there is more than one, report it and stop. If there is neither an
   open nor a merged one, release-please found nothing to release since the last release (only docs,
   test, ci, chore, build, style or refactor commits): report that and stop.
4. Review the release PR against this skill's rules, and report:
   - the last release tag:
     `git -C <repository root> tag -l 'v*.*.*' --merged HEAD | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1`,
     and the commits since it: `git -C <repository root> log --oneline <last>..HEAD`;
   - the version the PR proposes, from its title and from `.release-please-manifest.json` in its diff
     (`gh pr diff <n>`); the two must agree;
   - the version these rules give: a breaking change (`!` or `BREAKING CHANGE`) means minor while on
     `v0.x` and major from `v1`; `feat` means minor; only `fix`, `perf` or `revert` means patch;
     commits that only touch docs, tests, CI or chores need no release. The two versions must agree;
   - the `CHANGELOG.md` entry in the diff: every `feat`, `fix`, `perf`, `revert` and breaking commit
     since the last release in it, and nothing else;
   - that the PR changes only `CHANGELOG.md` and `.release-please-manifest.json`
     (`gh pr diff <n> --name-only`), and that its checks pass (`gh pr checks <n>`);
   - the head commit you reviewed: `gh pr view <n> --json headRefOid --jq .headRefOid`.
   Any disagreement goes in the report with the commits behind it, and you stop: never edit, close or
   relabel the release PR yourself. A maintainer fixes a wrong version by landing a commit with a
   `Release-As: X.Y.Z` footer, or a commit type, on `main`, which release-please picks up.
5. State the version, the reasoning and the PR's URL in your report **and wait for explicit
   confirmation** before anything is merged: merging the release PR publishes binaries and signed
   container images. Merge it only when explicitly told to, as the `pr-create` skill merges a PR (once the
   maintainer has approved that head commit; never approve it yourself), and only the commit you
   reviewed:
   `gh pr merge <n> --squash --delete-branch --match-head-commit <head commit from step 4>` from the
   repository root, which refuses to merge if the branch moved since the review; if it refuses, review
   the PR again from step 4. Usually the maintainer merges it: tell them to confirm, before merging by
   hand, that the PR's head commit is still the one reviewed (the SHA in your report). Release one PR at
   a time: the guard refuses to tag while a `release.yml` run is requested, waiting, pending, queued or
   in progress, so a release PR merged during a release waits for the next run of the workflow.
6. Verify the tag. Find the release-please run on the merge commit
   (`gh pr view <n> --json mergeCommit --jq .mergeCommit.oid`):
   `gh run list --workflow release-please.yml --commit <merge commit> --json databaseId,status,conclusion`,
   and watch it (`gh run watch <id>`). It must succeed: its `Release please`, `Tag` and `Relabel` jobs
   (the last is skipped only when nothing was tagged). If the `Tag` job's step "Wait for no release to be
   queued or in progress" failed because a release was in flight, wait for that `release.yml` run to finish, then
   ask the maintainer to re-run it, once, with the exact command `gh run rerun <id>`. If only the
   `Relabel` job failed after the tag was created, a re-run finds the tag on the merge commit, hands it on
   and finishes the relabelling: ask the maintainer for `gh run rerun <id>`, once. Never re-run a run
   yourself: the agents App has Actions read only (ADR 0031, d), and re-runs are the maintainer's. Report,
   stop, and pick this step up again once the maintainer says the re-run finished. On any
   other failure, report the failed step's log
   (`gh run view <id> --log-failed`) and stop. Then `git -C <repository root> fetch --tags` and check
   that `vX.Y.Z` exists, is the version reviewed in step 4, and points at the merge commit
   (`git -C <repository root> rev-parse 'vX.Y.Z^{commit}'`), and that the PR is now labelled
   `autorelease: tagged` (`gh pr view <n> --json labels`). If not, report what you found and stop:
   never create, move or delete a tag yourself.
7. Watch the release run the tag started
   (`gh run list --workflow release.yml --branch vX.Y.Z --json databaseId,status,conclusion`,
   then `gh run watch <id>`), then report the release URL (`gh release view vX.Y.Z --json url`).
   If a pending run was cancelled, ask the maintainer to re-run it with the exact command
   `gh run rerun <id>` (never run it yourself), and watch it once they have. On failure, report the failed
   step's log and stop; never delete or move a pushed tag.
   GoReleaser uploads to a draft and the run's `publish` job publishes it last, after the archives are
   attested (issues #156 and #163, ADR 0015). Check that it did:
   `gh release view vX.Y.Z --json isDraft,isImmutable,isPrerelease` must give `isDraft` `false`, and
   `isPrerelease` `true` exactly when the tag has a pre-release suffix. Report `isImmutable` too: it is
   `true` once the maintainer has turned release immutability on, and `false` means it is off. A
   release left a draft is a failed release: report it with the `publish` job's log and stop; never
   publish a draft by hand. `publish` refuses a tag that already has a published release, and a draft
   whose assets are not exactly `checksums.txt` and the files it lists, with the digests it records;
   its log says which check failed. It publishes the draft it checked by that draft's id, never by the
   tag. Give the
   maintainer the recovery ADR 0015 states, which only they start: within seven days of the run (the
   archives artifact's retention), "Re-run failed jobs" (`gh run rerun <id> --failed`) re-runs
   `attest-archives` or `publish` on the same draft and archives; after that, or when the draft itself
   is wrong, a full re-run (`gh run rerun <id>`) rebuilds the draft (`replace_existing_draft`) with new
   archives and a new attestation, and rebuilds and re-pushes the images under the same tags. A
   `--failed` re-run does not re-run the successful `latest` job, so it reuses the first attempt's
   `latest` decision (issue #173). `publish` refuses a stale `true` when a higher release is already
   GitHub's "Latest" (`GitHub's latest release is <tag>, higher than <this tag>: the latest decision
   is stale, ...`) and leaves the draft. When that "Latest" is a pre-release tag (`vX.Y.Z-rc.N`, which
   `sort -V` ranks above `vX.Y.Z`), someone un-flagged its release by hand: tell the maintainer to mark
   it a pre-release again (or set the correct "Latest") and re-run the release run. The images'
   `latest` tag has no such guard. So when a higher release tag has been merged into `main` since the
   first attempt, always tell the maintainer to re-run the whole run (`gh run rerun <id>`, without
   `--failed`), which runs `latest` again. Until it is published, the images of the draft release are
   public already (ADR 0015). Then check the release again from the top of this step.
   Report the result of the run's `verify-archives` job, "Verify the published archives", the
   independent check of every published archive and `checksums.txt` against their digests and their
   build provenance, and of GitHub's "Latest" release, which must follow the same highest-release rule
   as the images' `latest` tag (issue #173, ADR 0015). The run's `latest` job decides it once, and
   both the images and `publish` follow that one output; `verify-archives` reads the highest release
   tag again on its own, as an independent check, and fails a pre-release that is GitHub's "Latest"
   without any condition:
   `gh run view <id> --json jobs --jq '.jobs[] | select(.name == "Verify the published archives") | .conclusion'`.
   It must be `success`; a failure is a failed release, handled as a failed `verify` below. When the
   highest release tag merged into `main` has no published release yet, it skips the "Latest" check
   and passes with the notice `the release of <higher tag> is not published yet, ...`. Read its
   notices from its annotations exactly as for `verify` below (its job id by the name "Verify the
   published archives"), and act on each the same way. Confirm GitHub's "Latest" yourself too:
   `gh api repos/timothymamo/shift2eu/releases/latest --jq '.tag_name'` must be the highest `vX.Y.Z`
   tag merged into `main` once its release is published, never an older line's patch.
   Report the result of the run's `verify` job too, the independent check of both published images
   on a fresh GitHub-hosted runner (issue #99, ADR 0015). Find it by its name, "Verify the published
   images", which `check.ReleaseWorkflow` pins:
   `gh run view <id> --json jobs --jq '.jobs[] | select(.name == "Verify the published images") | .conclusion'`.
   It must be `success`. When a higher release tag merged into `main` has no published image, `verify`
   skips the `latest` check and passes with a notice instead (ADR 0015). Read it from the job's
   annotations, never by searching the job's log for `::notice::`: the log echoes the step's script,
   which holds that literal line, so such a search matches on every run. A job's id is its check-run
   id, so list the job's notices with
   `gh api repos/timothymamo/shift2eu/check-runs/<verify job id>/annotations --jq '.[] | select(.annotation_level == "notice") | .message'`
   (the job id is
   `gh run view <id> --json jobs --jq '.jobs[] | select(.name == "Verify the published images") | .databaseId'`).
   Empty output means `verify` raised no notice. A notice must be acted on, not only reported.
   For each `<image>:<higher tag> is not published yet` it names, check that the higher tag has a
   release run queued or in progress:
   `gh run list --workflow release.yml --branch <higher tag> --json status,conclusion,databaseId`.
   If it has one, report the notice and that run. If it has none, or only a failed or cancelled one,
   report the higher tag as stray or unpublished, with that output, and stop: while it stays
   unpublished, `verify` checks `latest` for no older release, and the `latest` job decides
   `latest=false` for every later release, so it holds back both the images' `latest` tag and
   GitHub's "Latest" release (an accepted consequence, ADR 0015). Give the maintainer the two
   recoveries and let them decide which: re-run the higher tag's failed release run
   (`gh run rerun <its run id>`, or, for a run that was replaced in the concurrency group and never
   ran, re-run the release for that tag), so its release is published and becomes "Latest"; or
   delete the stray tag (`git push origin --delete <higher tag>`, then the local copy), so the next
   release is the highest again. The same notice from `verify-archives`, `the release of <higher
   tag> is not published yet, ...`, is handled the same way. Removing the tag or re-running its
   release is the maintainer's decision; never delete or move a pushed tag yourself, and do not
   re-run its release unasked. `verify` runs after the
   images are published, so it detects a bad release, it does not prevent one. A failed `verify` job is
   a failed release: report it with the failed step's log (`gh run view <id> --log-failed`) and stop. Do
   not re-run it to get past the failure, and do not count the VM check below as a substitute for it.
   In the report, give the maintainer the recovery ADR 0015 states, which only they carry out: mark the
   GitHub release as bad, point `latest` (and `X.Y` if wrong) back at the last good release's digest or
   delete the bad image tag, and fix forward with a new patch release.

   Then confirm the release publishes the bundled OpenAPI document (issue #139). CI's snapshot dry run
   (`.github/scripts/verify-archives.sh`) never exercises `release.extra_files`, because GoReleaser
   skips the release pipe under `--snapshot`, but the release itself already holds the asset to account
   (#172): the `publish` job refuses a draft whose assets are not exactly the names `checksums.txt`
   lists plus `checksums.txt`, which lists the document, and `verify-archives` checks its digest and
   attestation. This step is an independent cross-check that the published bytes are the tag's
   committed `api/openapi.bundled.json`. The release must list the asset `shift2eu-server_openapi.json`:
   `gh release view vX.Y.Z --json assets --jq '.assets[].name'`. Its `checksums.txt` must hold exactly
   one line for it, with the sha256 of `api/openapi.bundled.json` at the tag:

   ```bash
   TAG=vX.Y.Z
   gh release download "$TAG" --pattern checksums.txt --output - | grep -E '  shift2eu-server_openapi\.json$'
   git -C <repository root> show "$TAG:api/openapi.bundled.json" | sha256sum
   ```

   The two sums must be equal. A missing asset, a missing or repeated line or a different sum is a
   failed release: report what you found and stop, as for a failed `verify` job. The asset's digest
   and build provenance need no check of their own: `attest-archives` attests it with the archives,
   and `verify-archives`, whose result is reported above, verifies every asset `checksums.txt` lists,
   this one included.
8. Check the published image yourself; do not hand this to the user. The Lima VM has cosign, a `gh`
   with `gh attestation`, `crane` and rootless `nerdctl` (ADR 0017); this is "Verify the image" in
   `docs/guide/verify-releases.md`, in its VM form, for the new tag. Run it from the repository root,
   with no registry login, and call each tool by its absolute path: `~/.local/bin` and `~/go/bin` come first on the `PATH` and the VM
   user can write there, while these binaries are root-owned:

   ```bash
   TAG=vX.Y.Z
   DIGEST="$(/usr/local/bin/crane digest "ghcr.io/timothymamo/shift2eu:$TAG")"
   IMAGE="ghcr.io/timothymamo/shift2eu@$DIGEST"
   /usr/local/bin/cosign verify "$IMAGE" \
     --certificate-identity "https://github.com/timothymamo/shift2eu/.github/workflows/release.yml@refs/tags/$TAG" \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
     --certificate-github-workflow-trigger push
   /usr/local/bin/gh attestation verify "oci://$IMAGE" --repo timothymamo/shift2eu \
     --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
     --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
   /usr/local/bin/gh attestation verify "oci://$IMAGE" --repo timothymamo/shift2eu \
     --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
     --source-ref "refs/tags/$TAG" --deny-self-hosted-runners \
     --predicate-type https://spdx.dev/Document/v2.3
   /usr/local/nerdctl/bin/nerdctl run --rm --read-only --network none "$IMAGE" version   # prints X.Y.Z, without the v
   ```

   Then check one archive and its build provenance, "Verify an archive" in
   `docs/guide/verify-releases.md`, in a new empty directory outside the repository (the scratchpad):

   ```bash
   ARCHIVE="shift2eu_${TAG#v}_linux_amd64.tar.gz"
   /usr/local/bin/gh release download "$TAG" --repo timothymamo/shift2eu --pattern "$ARCHIVE" --pattern checksums.txt
   awk -v a="$ARCHIVE" '$2 == a' checksums.txt > "$ARCHIVE.sha256"
   sha256sum --check "$ARCHIVE.sha256"
   /usr/local/bin/gh attestation verify "$ARCHIVE" --repo timothymamo/shift2eu \
     --signer-workflow timothymamo/shift2eu/.github/workflows/release.yml \
     --source-ref "refs/tags/$TAG" --deny-self-hosted-runners
   ```

   Report the digest and each command's outcome. Every command must exit 0 and `version` must print the
   tag without its `v`; if one fails, report its output and stop (never re-tag). After the very first
   push GHCR has created the package private, so `crane digest` and the pull fail anonymously: report
   that a maintainer must make `ghcr.io/timothymamo/shift2eu` public once in the package settings (an agent
   cannot), and run the checks after that.

   This is a smoke test, not the trust anchor: every agent shares the VM user, which has passwordless
   `sudo`, so any of them could tamper with these binaries or their output (ADR 0017). The independent
   check is the release's `verify` job of step 7.
