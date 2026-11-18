# PRD #175: A weekly read-only security audit of the repository and the latest release

**Status:** Draft
**Issue:** #175

## Problem

shift2eu ships two binaries and two signed images that read other people's cloud estates. Every change is
reviewed: each unit's `security-audit` pass, the AI reviewer of PRD #161 milestone 4, the maintainer's
approval, and gosec, govulncheck and CodeQL in CI. But all of them look at a **diff**. Nothing looks at the
whole tree, its git history and the **released** artifacts as an attacker would. Nothing tests the
running server with hostile input, checks a published release end to end against
[the release verification guide](../docs/guide/verify-releases.md), or notices that a dependency nobody
touched has a new advisory.

The maintainer has no regular, dated record of the project's security posture. Users who run a release
binary or image have no `SECURITY.md` that tells them which versions are supported or how to report a
vulnerability. The repository is public, so a finding written in the wrong place discloses it.

The issue's first plan put the audit in a second Lima VM with a fine-grained personal token, a launcher
script and a `dot-agent-deck` schedule. The maintainer replaced that on 2026-11-18 (Decisions, below): a
scheduled GitHub Actions workflow on GitHub-hosted runners, a GitHub App for the few writes, and keys in a
`main`-only environment, following PRD #161 milestone 4's AI reviewer.

## Outcome

Once a week, and on demand, a GitHub Actions workflow on `main` audits the repository and its **latest
published release** with **Claude Fable 5.2** (`claude-fable-5-2`, effort high). It runs on a fresh
GitHub-hosted runner and authenticates with the maintainer's Claude subscription token.

It scans the whole tree and its history, verifies the release's checksums, attestations and signatures,
scans the images and binaries, and fuzzes and attacks the release **locally**: in containers on that
runner, with no network. It never changes code, pushes to a branch other than its own report branch,
or touches a cloud, GitHub's or GHCR's services beyond reading them, or any remote host.

Its results go three ways:

- a dated, public report in a **docs-only pull request** under `docs/security/`, opened by the audit App
  and merged only with the maintainer's code-owner approval;
- one issue per publishable finding, labelled `security`;
- a **draft repository security advisory** per exploitable unfixed finding, the report saying only
  "N findings handled privately" (open question 2).

Read-only is enforced by the workflow, not by the prompt alone. The model runs with a read-only
`GITHUB_TOKEN` and no write credential at all. The audit App's token is minted only after the model has
finished, in a step with no model in it, which writes only the files and records listed below. No raw tool
output, transcript or private finding reaches a workflow log or artifact, which anyone can read in a public
repository. `SECURITY.md` tells users which versions are supported and how to report a vulnerability.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | none: the audit reads no cloud | Live access to AWS, Azure or GCP, cloud credentials of any kind |
| Target providers | none | Live access to OVHcloud, Scaleway or UpCloud, their catalog sources |
| Taxonomy categories | none | |
| Audit subject | The repository on `main` (tree and full history), the latest published release (archives, `checksums.txt`, both images, their attestations and SBOMs, the OpenAPI document), `.github/`, `.claude/`, `.pi/`, `.dot-agent-deck.toml` and `dev/` as attack surface | Other repositories, dependencies' own code beyond advisories, GitHub's or GHCR's services |
| Execution | A scheduled and `workflow_dispatch` workflow on `main`, GitHub-hosted runners, local containers on that runner | A second Lima VM, a launcher script, `dot-agent-deck schedule`, self-hosted runners (Decision 1) |
| Writes | The report PR (`docs/security/` only), `security` issues, draft security advisories, each through the audit App's token | Fixing findings in the audit run, editing code or `testdata/`, commenting on or closing other issues and PRs, releases, tags, packages |
| Repository settings | The `security-audit` environment, the audit App, the `security` label, documented as maintainer steps and read by checks | Changing `Protect main`, the release Apps or their environments (ADR 0021, PRD #161) |
| Skills and docs | A `security-weekly-audit` skill reusing `security-audit`'s checklist, an ADR, `SECURITY.md`, `docs/security/README.md`, the report template, `docs/dev-environment.md` (or the page PRD #161 milestone 4 documents its App in) | Rewriting `security-audit` |

## Acceptance criteria

### The skill, the ADR and the disclosure documents (milestone 1)

- [ ] `.claude/skills/security-weekly-audit/SKILL.md` exists, agent-neutral like the other skills, and
  **links to `security-audit`'s checklist instead of copying it**. It adds the whole-tree and release
  steps (What a run does, below), the rules of engagement and the triage rules. `make check` passes,
  `test/docs`' link and skill-shell checks included.
- [ ] The skill frames every task as an authorized test of this project's own artifacts, run locally. It
  says that a step the model declines is reported in the report's limits section, never worked around.
- [ ] A root `SECURITY.md` names the supported versions (the latest minor release), points reporters to
  GitHub's private vulnerability reporting (already enabled), states the response the maintainer commits
  to, and links to the audit reports. It contains no personal e-mail address.
- [ ] `docs/security/README.md` explains the weekly audit, the disclosure split and how to verify a
  release, and indexes the reports in `docs/security/audits/`. `docs/security/audits/TEMPLATE.md` holds
  the report's sections (Report and triage, below). Both are listed in the docs index, so
  `TestIndexListsEveryPage` and `TestPagesHaveOneTitle` pass.
- [ ] An ADR records the audit's design: its scope, the model and its authentication, where it runs, the
  read-only enforcement (the token split, the settings, the checks), the disclosure split, and why the
  Lima-VM plan of the issue was dropped.

### The workflow and its enforcement (milestone 2)

- [ ] `.github/workflows/security-audit.yml` triggers on `schedule` and `workflow_dispatch` **only**. It
  has no `pull_request`, `pull_request_target`, `push`, `workflow_run` or `issue_comment` trigger, and
  every job runs only when `github.ref` is `refs/heads/main`. Its job runs in the `security-audit`
  environment, whose deployment branch policy is `main` only, so a `workflow_dispatch` on another ref
  never receives a secret. It never checks out a pull request's code or any ref but `main`'s HEAD.
- [ ] `GITHUB_TOKEN` holds `permissions: {}` at the top level and `contents: read` in the job, plus only
  the read scopes the ADR lists as needed for the release checks (for example `attestations: read`), and
  never a write scope. Every checkout uses `persist-credentials: false`.
- [ ] The audit App's token is minted with `actions/create-github-app-token` (pinned by SHA) **only in
  the publish step(s) after the audit step**. It is narrowed with exactly the `permission-*` inputs the
  ADR lists. No step that runs the model, a scanner or the release's code ever holds it. The App's client
  ID is a variable and its private key a secret of the `security-audit` environment only. No
  repository-level secret or variable holds either. The `release-tag` skill's secrets check lists them
  as it lists the other Apps'.
- [ ] The model step authenticates with the Claude Code OAuth token from the `security-audit`
  environment's secret alone, never an API key, and runs `claude-fable-5-2` at effort high. Its
  environment holds that token and the read-only `GITHUB_TOKEN`, and nothing else secret.
- [ ] Claude Code runs with a settings file in the repository whose permission rules allow only the reads
  and local commands the skill needs. They deny, at least: `git push`, `git commit`, `git tag`,
  `git remote`, and `git config` writes; `gh pr`, `gh release`, `gh issue create|edit|close|comment`,
  `gh label`, `gh secret`, `gh variable`, `gh workflow run`, `gh auth`, and `gh api` with any method but
  `GET`; `env`, `printenv`, `set` and reading `/proc/*/environ`; `curl`, `wget`, `nc`, `ssh` and any
  network tool not on the list; `docker push`, `docker login` and `docker run` without `--network none`
  (or the local bridge the ADR names); `sudo`. A test pins these rules, so dropping one fails
  `make test`.
- [ ] Every action is pinned by full commit SHA with a version comment. Every tool binary (gitleaks,
  osv-scanner, trivy, zizmor, OpenSSF scorecard, cosign, the Claude Code CLI if installed directly) is
  pinned by version **and** checksum, with a `# renovate:` comment where Renovate can track it (CLAUDE.md
  rule 7). actionlint and govulncheck reuse the pins `tools/go.mod` and `security.yml` already have. No
  tool is installed from a mutable tag, `@latest` or a `curl | sh`.
- [ ] A check in `test/architecture` (like `check.ReleasePleaseWorkflow`) pins the workflow: its
  triggers, the `main`-only condition, its permissions, its environment, the steps that may hold the App
  token and its `permission-*` inputs, the absence of `actions/upload-artifact`, `actions/cache` of tool
  output and `set -x`, the commit-pinned actions, the time-outs, and the settings file passed to Claude
  Code. Changing any of them fails `make check`.
- [ ] Nothing private reaches a log or an artifact. Raw tool output, crash inputs, PoCs, Claude Code's
  transcript and its final output are written only under `$RUNNER_TEMP`, never printed, never
  uploaded. The workflow uploads **no** artifact. The step summary and the log show only counts, the
  audited commit and tag, tool versions and step outcomes. Claude Code runs with its output redirected to a
  file, not streamed to the log.
- [ ] The publish step is a script with no model in it, tested against a fake `gh` with canned API
  responses (as CI's `Release please dry run` job tests the release scripts). It reads the run's
  structured result and:
  - opens the report PR from a branch `security-audit/YYYY-MM-DD` whose diff it first checks is exactly
    `docs/security/audits/YYYY-MM-DD.md` and the index line in `docs/security/README.md`, refusing
    anything else;
  - creates the `security` issues after searching the open ones for a duplicate;
  - creates the draft advisories.
  
  It fails closed on any other path, an existing branch, or a malformed result. Values from the run reach
  the shell only through `env:` and files, never through a `${{ }}` expression.
- [ ] Exploits and fuzzing run only against the release's artifacts on the runner. The server runs from
  the release image with `--read-only --network none` (or a bridge only the runner reaches), the CLI runs
  on hostile inventories as an unprivileged process, and nothing is sent to GitHub, GHCR or any other
  host. Each step has a `timeout-minutes` and the job one below GitHub's 6-hour limit (the ADR gives the
  budget: for example 10 minutes per fuzz target, 60 for dynamic testing). A step that times out is
  recorded in the report's limits, and the run still publishes what it has.
- [ ] A failed run is visible: a scheduled run that fails (an expired or revoked subscription token, the
  subscription's usage limit, a tool download whose checksum differs) fails the workflow, which GitHub
  notifies the maintainer of. Its step summary names the cause and the runbook step that fixes it,
  without printing a secret.
- [ ] The maintainer's runbook (below) is in `docs/dev-environment.md`, or the page where PRD #161
  milestone 4 documents the reviewer App, with a check after each step.

### The first run and the schedule (milestone 3)

- [ ] One `workflow_dispatch` run on `main` against the latest release produces, and nothing else:
  - a docs-only PR opened by `shift2eu-audit[bot]` that adds `docs/security/audits/<date>.md` and its
    index line;
  - the `security` issues for its publishable findings (or none);
  - the draft advisories for its private findings (or none).

  No other branch, tag, release, package, label, comment or setting changes. The PR records the run's
  URL, and that the run's log and its (empty) artifact list hold no finding detail.
- [ ] The report states the `main` SHA, the release tag, the tool versions and the model. It lists every
  check with "clean", "findings" or "not run (why)".
- [ ] After the maintainer has reviewed that run, the `schedule` trigger is enabled (weekly, at a time
  the ADR records). The second scheduled run's report marks each earlier finding as fixed, still open or
  regressed.

## What a run does

The skill holds the detail, and the workflow's steps do what needs no model. The `main` SHA and the latest
release tag (`gh release view --json tagName`) are recorded first, and every later step uses those two.

1. **Repository, static.**
   - The `security-audit` checklist across the whole tree, not a diff.
   - govulncheck and osv-scanner on the module and `tools/`.
   - gitleaks on the full git history.
   - zizmor and actionlint on `.github/workflows`.
   - OpenSSF scorecard, read-only: no publishing of results, no `id-token`.
   - A review of what the agents may run (`.claude/`, `.pi/`, `.dot-agent-deck.toml`, `dev/`).
2. **Release integrity.** Everything in [the release verification guide](../docs/guide/verify-releases.md):
   `checksums.txt`, `gh attestation verify` on every file, `cosign verify` on both images and their
   provenance and SBOM attestations, and that GitHub's latest release and the images' `latest` tag agree.
3. **Release contents.**
   - `govulncheck -mode=binary` on every release binary.
   - trivy on both images and their SBOMs.
   - The images run as non-root, with a read-only root filesystem and no shell where none is expected.
4. **Dynamic, local only.**
   - The server from the release image, attacked from the runner: malformed, oversized and deeply nested
     bodies; slow clients; path traversal; header injection; content-type confusion; every endpoint of
     the published OpenAPI document.
   - The CLI on hostile inventory files.
   - Time-boxed `go test -fuzz`. No `Fuzz*` target exists yet (open question 5). Each candidate finding
     is confirmed by a minimal PoC against the local container or binary. Crash inputs go into the report
     or an advisory, never into `testdata/`.
5. **Supply chain.**
   - Action pins and workflow `permissions`.
   - Rulesets and environments, read the way the `release-tag` skill's step 2 reads them.
   - Renovate's state.
   - Dependencies new or changed since the last report.
6. **Since the last audit.** The previous report's findings marked fixed, still open or regressed.

## Report and triage

- **Report** (`docs/security/audits/YYYY-MM-DD.md`, from the template): summary; scope (commit, tag, tool
  versions, model); findings table (ID, severity critical/high/medium/low, category, status new, open or
  fixed, `path:line` or artifact, scenario, recommended fix); coverage, including what was clean;
  changes since the last audit; the run's limits.
- **Disclosure split.** The PR and the issues are public **the moment they are opened**, before the
  maintainer reviews them, so the split is decided in the run:

  | Finding | Where it goes |
  |---|---|
  | Exploitable and unfixed: critical or high, or medium with a working PoC | A draft security advisory (open question 2). The report and every issue say only "N findings handled privately". |
  | Hardening, low, informational, already fixed | The report, and one `security` issue each, unless an open issue already covers it. |
  | Unconfirmed | The report's "needs investigation" list, no issue. |

  When the severity is in doubt, the finding is private.
- **Signal over noise.** Every finding is confirmed by a PoC or a precise trace. A false positive
  recorded in an earlier report is not reported again.

## The subscription token

- The model step authenticates with a **Claude Code OAuth token** that the maintainer creates with
  `claude setup-token` on their own machine, under the subscription the agents already use. It is stored
  as a secret of the `security-audit` environment **only** (the ADR names it; for example
  `CLAUDE_CODE_OAUTH_TOKEN`). No API key is used.
- A run counts against the **maintainer's subscription limits**, shared with the agents in the VM. The
  scheduled time is chosen for when the agents are usually idle. A run stopped by the limit fails visibly
  and can be re-run with `workflow_dispatch`.
- **Expiry and rotation.** The token is long-lived but expires (`claude setup-token` shows the term). The
  runbook records the date it was created and its expiry. An expired or revoked token makes the model
  step fail with an authentication error and the run fail, with the step summary pointing at the runbook.
  To rotate: run `claude setup-token` again, replace the environment secret, revoke the old token where
  the Claude account allows it, and start one `workflow_dispatch` run to check. Rotate at once if the
  token may have leaked.

## Milestones

1. [ ] **Skill, ADR and disclosure documents.** The `security-weekly-audit` skill, the ADR,
   `SECURITY.md`, `docs/security/README.md`, `docs/security/audits/TEMPLATE.md`, the docs index. No
   workflow yet. Depends on nothing; can merge before PRD #161 milestone 4.
2. [ ] **The workflow, its App token, the pinned tools and their checks.** `security-audit.yml` with
   `workflow_dispatch` only (no `schedule` yet), the Claude Code settings file and its test, the publish
   script and its fake-`gh` test, the `test/architecture` check of the workflow, the `release-tag`
   secrets check, the runbook. **Depends on 1, and on PRD #161 milestone 4 having merged**
   (`feat/161-m4-ai-reviewer`; milestone 3 is `chore/161-m3-remove-maintainer-token`). It reuses milestone
   4's `main`-only environment pattern, its in-workflow App-token minting, its way of running Claude Code
   in Actions and its untrusted-input handling, rather than inventing a second set. Runbook steps 1 to 4
   come before its first run.
3. [ ] **First run, then the schedule.** One manual run against the latest release, its report PR merged
   by the maintainer, then a PR that adds the `schedule` trigger (and moves this PRD to `prds/done/`).
   Depends on 2 and runbook step 5.

Security-sensitive throughout: **every milestone's PR runs the `security-audit` skill**, and the
workflow, settings and publish script get the AI reviewer of PRD #161 milestone 4 like any other change.

## Maintainer runbook

An agent cannot do these steps. Each one is safe to stop at; the workflow is rolled back by disabling it.

1. **Create the audit App** `shift2eu-audit`. Install it on this repository only, with the permissions
   settled by open question 1, no webhook, and no other repository. Generate its private key.
2. **Create the `security-audit` environment**: deployment branches `main` only, no required reviewers
   (a scheduled run cannot wait for one). Add its variable (the App's client ID) and secrets (the App's
   private key, and the Claude Code OAuth token from `claude setup-token`). Record the token's creation
   date and expiry. Check: `gh api repos/<owner>/<repo>/environments/security-audit` shows the branch
   policy, and the repository-level secrets list holds neither secret.
3. **Create the `security` label** (it does not exist yet). Private vulnerability reporting is already
   enabled; if open question 2 needs more (draft advisories by an App), check that GitHub allows the App to
   create them.
4. **Merge milestone 2**, then check that the App appears nowhere in a ruleset's bypass list and that
   `Protect main` still requires the code owner's approval.
5. **Start the first run** (`workflow_dispatch` on `main`). Read its report PR, issues and advisories.
   Check that the log and the run's artifacts hold no finding detail. Then merge milestone 3's schedule.
6. **Renew the Claude token** before it expires (The subscription token, above), and **rotate the App's
   key** if it may have leaked.

## Decisions and open questions

**Decided (maintainer, 2026-11-18, in issue #175), replacing the parts of the issue they contradict:**

- **Where:** a scheduled GitHub Actions workflow on GitHub-hosted runners, not a second Lima VM. Each run
  gets a fresh runner; exploit tests run in containers on that runner only. Keys live in a
  `security-audit` environment limited to `main`, following PRD #161 milestone 4's `ai-review`
  environment. No launcher script, no audit VM, no `dot-agent-deck schedule`.
- **Report PR:** the audit App opens the docs-only PR with the dated report. The maintainer's code-owner
  approval is still required to merge it.
- **Model:** Claude Fable 5.2 (`claude-fable-5-2`), effort high, authenticated with the maintainer's
  Claude subscription token (a Claude Code OAuth token, a secret of the `security-audit` environment
  only), not an API key.
- **Tools:** gitleaks, osv-scanner, trivy (images and SBOMs), zizmor and actionlint, OpenSSF scorecard,
  time-boxed `go test -fuzz`. trufflehog, grype and semgrep are dropped as overlaps (gosec, govulncheck
  and CodeQL already run in CI).
- **Plan:** milestones as above, implemented after PRD #161 milestones 3 and 4 merge, reusing milestone
  4's environment and token pattern.
- **The repository stays public.** Private vulnerability reporting is already enabled.

**Open (for the maintainer, each with a recommendation):**

1. **The audit App `shift2eu-audit` and its exact permissions.** Proposed: Contents write and Pull
   requests write (the report PR), Issues write, Repository security advisories write, Actions read and
   Attestations read. Can Contents write be narrowed, for example so the report PR may touch only
   `docs/security/`, enforced by a required check?

   *Recommendation:*
   - **Keep Contents write**: GitHub App permissions cannot be narrowed to a path or a branch.
   - **Narrow its use three ways.**
     - The token is minted only in the publish step, which has no model in it and refuses any diff but
       the report and its index line (milestone 2).
     - A ruleset on `security-audit/**` lets only the audit App and admins create or update those
       branches, so another token cannot plant a report.
     - A CI job, made a **required check**, fails any PR authored by `shift2eu-audit[bot]` that touches
       anything outside `docs/security/`. It passes trivially for other authors, so it costs nothing
       elsewhere.
   - **Never in a bypass list**: `Protect main`'s code-owner approval and the `v*` tag rulesets still
     stop the App landing anything or tagging; it is never added to a bypass list.
   - **Drop Actions read and Attestations read from the App.** The audit's reads use the job's read-only
     `GITHUB_TOKEN` (`contents: read`, `attestations: read`), so the App holds write scopes only, and
     only in the step that needs them. Give it no Workflows permission, so it can never change
     `.github/workflows/`.

2. **Private findings as draft advisories.** Exploitable unfixed findings filed as draft repository
   security advisories created by the App, the public report saying only "N findings handled privately".

   *Recommendation:* **Yes, with three conditions.**
   - The model writes private findings to a structured file under `$RUNNER_TEMP`, never to the report.
     The publish script creates one draft advisory per finding from that file (no CVE request, no
     publishing), and only the count reaches the report, the log and the summary.
   - Each advisory carries a stable fingerprint (rule, file, sink) so later runs update the "since the
     last audit" status without reading advisory contents back into the model.
   - When the run cannot classify a finding, it is private.

   The fallback, if GitHub refuses advisory creation to an App token, is a single draft advisory per run
   created by the maintainer from the run's private findings file. That would need a private channel this
   workflow cannot provide in a public repository, so check App creation in runbook step 3 before
   milestone 2 is built.

3. **How Claude Code runs in Actions.** Options: `anthropics/claude-code-action` pinned by SHA, or the
   Claude Code CLI installed at a pinned version and checksum and run headless (`claude -p` with
   `--settings`).

   *Recommendation:* **use whatever PRD #161 milestone 4 settles for the reviewer**, so the repository
   has one pinned way to run Claude Code in Actions and one Renovate rule for it. If it has a choice, the
   pinned CLI with the settings file is easier to hold to the deny rules and to keep out of the log.

4. **A public report PR every week, even with nothing new?**

   *Recommendation:* **yes.** A dated report with no findings is the evidence the audit ran and what it
   covered, and it is one docs-only PR a week to approve. Revisit after the first two months if the
   reviews cost more than they show.

5. **Fuzz targets.** The tree has no `Fuzz*` test today, and the audit must not change code. Options:
   the run writes throwaway harnesses in its scratch copy only, or a separate issue adds committed fuzz
   targets (the inventory decoder, the HTTP request decoder, the report renderers) that CI and the audit
   both run.

   *Recommendation:* **a separate issue for committed fuzz targets**, built test-first like any other
   change. Until it merges, the audit fuzzes with throwaway harnesses in `$RUNNER_TEMP`, and the report
   says so in its limits.

6. **Outbound network from the runner.** GitHub-hosted runners allow any outbound traffic, so a model led
   astray by hostile input could in principle send the subscription token elsewhere. That input could be
   a dependency's advisory text, a scanner's output or a crafted release file. Options: rely on the deny
   rules and the containers' `--network none`, or add an egress-filtering action.

   *Recommendation:* **no egress-filtering action for now.** It would be a privileged third-party action
   in the one job that holds the token.
   - Do every download in workflow steps **before** the model runs, so the model step needs only the
     model API and GitHub reads.
   - Keep the subscription token out of every step but the model's.
   - Treat everything the model reads as untrusted data, as the reviewer does.
   - Record in the ADR that the residual risk is a leaked subscription token, which runbook step 6
     rotates.
