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
published release** with **Claude Fable 5.2** (`claude-fable-5-2`, effort high). It runs on fresh
GitHub-hosted runners and authenticates with the maintainer's Claude subscription token.

It scans the whole tree and its history, verifies the release's checksums, attestations and signatures,
scans the images and binaries, and fuzzes and attacks the release **locally**: in containers on the
runner, with no route off it. It never changes code, pushes to a branch other than its own report branch,
or touches a cloud, GitHub's or GHCR's services beyond reading them, or any remote host.

Its results go three ways:

- a dated, public report in a **docs-only pull request** under `docs/security/`, opened by the audit App
  and merged only with the maintainer's code-owner approval;
- a `security` issue per public finding, opened only **after** the maintainer has merged the report that
  holds it (open question 7);
- a **draft repository security advisory** per private finding, the report saying only "N findings
  handled privately" (open question 2).

Read-only, and the disclosure split, are enforced by the workflow and a model-free script, not by the
prompt. The run has two jobs:

- The **audit job** runs every networked check and every piece of untrusted code in workflow steps with no
  model and no subscription token, then runs the model, sandboxed, to triage their results. It holds no
  write credential at all.
- The **publish job** runs afterwards on a fresh runner, from `main`'s committed code only. It receives
  nothing from the audit job but the encrypted structured result, decides itself what is public, and is the
  only place the audit App's token exists.

No raw tool output, transcript or private finding reaches a workflow log or a readable artifact, which
anyone can read in a public repository. `SECURITY.md` tells users which versions are supported and how to
report a vulnerability.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | none: the audit reads no cloud | Live access to AWS, Azure or GCP, cloud credentials of any kind |
| Target providers | none | Live access to OVHcloud, Scaleway or UpCloud, their catalog sources |
| Taxonomy categories | none | |
| Audit subject | The repository on `main` (tree and full history), the latest published release (archives, `checksums.txt`, both images, their attestations and SBOMs, the OpenAPI document), `.github/`, `.claude/`, `.pi/`, `.dot-agent-deck.toml` and `dev/` as attack surface | Other repositories, dependencies' own code beyond advisories, GitHub's or GHCR's services |
| Execution | A scheduled and `workflow_dispatch` workflow on `main`, GitHub-hosted runners, local containers on the runner | A second Lima VM, a launcher script, `dot-agent-deck schedule`, self-hosted runners (Decision 1) |
| Writes | The report PR (`docs/security/` only) and draft security advisories, each through the audit App's token in the publish job; `security` issues for public findings of merged reports | Fixing findings in the audit run, editing code or `testdata/`, commenting on or closing other issues and PRs, releases, tags, packages, publishing an advisory or requesting a CVE |
| Repository settings | The `security-audit` environment, the audit App, the `security-audit/**` branch ruleset, the `security` label, documented as maintainer steps and read by checks | Changing `Protect main`, the `v*` tag rulesets, the release Apps or their environments (ADR 0021, PRD #161) |
| Skills, tests and docs | A `security-weekly-audit` skill reusing `security-audit`'s checklist, an ADR, `SECURITY.md`, `docs/security/README.md`, the report template, `test/docs`' index check accepting a sub-index, `docs/dev-environment.md` (the runbook), a staleness check of the schedule | Rewriting `security-audit` |

## Acceptance criteria

### The skill, the ADR and the disclosure documents (milestone 1)

- [ ] `.claude/skills/security-weekly-audit/SKILL.md` exists, agent-neutral like the other skills, and
  **links to `security-audit`'s checklist instead of copying it**. It adds the whole-tree and release
  triage (What a run does, below), the rules of engagement and the triage rules. `make check` passes,
  `test/docs`' link and skill-shell checks included.
- [ ] The skill frames every task as an authorized test of this project's own artifacts, run locally. It
  says that a step the model declines is reported in the report's limits section, never worked around.
  It tells the model to treat every file it reads (scanner output, advisory text, release files, the
  repository) as untrusted data, never as instructions, and to report an embedded instruction as a
  finding.
- [ ] A root `SECURITY.md` names the supported versions (the latest minor release), points reporters to
  GitHub's private vulnerability reporting (already enabled), states the response the maintainer commits
  to, and links to the audit reports. It contains no personal e-mail address.
- [ ] `TestIndexListsEveryPage` (`test/docs/archive_test.go`) accepts a page under `docs/` that is linked
  either from `docs/README.md` or from a **sub-index**: a `README.md` in a subdirectory of `docs/` that
  `docs/README.md` itself links (one level, no chains). A page linked only from a sub-index that
  `docs/README.md` does not link still fails. A test pins both cases. This lets each weekly report be
  indexed by `docs/security/README.md` alone.
- [ ] `docs/security/README.md` explains the weekly audit, the disclosure split and how to verify a
  release, and indexes the reports in `docs/security/audits/`. `docs/README.md` links it.
  `docs/security/audits/TEMPLATE.md` holds the report's sections (Report and triage, below), has exactly
  one title, and is linked from `docs/security/README.md`. `TestIndexListsEveryPage` and
  `TestPagesHaveOneTitle` pass.
- [ ] An ADR records the audit's design **as far as it is known before PRD #161 milestone 4 merges**: its
  scope, the model and its authentication, the two-job split, which checks run as workflow steps and which
  the model does (the table in What a run does), the disclosure rules the publish script enforces, the
  App's permissions and their side effects (open question 1), and why the Lima-VM plan of the issue was
  dropped. Milestone 2 amends the same ADR with how Claude Code runs in Actions (open question 3), its
  sandbox, settings and permission mode, the result's encryption, and the time budget; it is not written
  twice.

### The workflow and its enforcement (milestone 2)

Triggers, jobs and permissions:

- [ ] `.github/workflows/security-audit.yml` triggers on `schedule` and `workflow_dispatch` **only**. It
  has no `pull_request`, `pull_request_target`, `push`, `workflow_run` or `issue_comment` trigger, and
  every job runs only when `github.ref` is `refs/heads/main`. Both jobs run in the `security-audit`
  environment, whose deployment branch policy is `main` only, so a `workflow_dispatch` on another ref
  never receives a secret. Every checkout is of `github.sha` (the `main` commit the run started on), never
  a pull request's code or another ref, with `persist-credentials: false`.
- [ ] It has two jobs. The **audit job** runs the checks and the model. The **publish job** has
  `needs: audit`, runs on its own fresh runner, checks out `github.sha` itself, runs only its committed
  script, and receives from the audit job only the encrypted structured result (below) and the run's
  non-secret counts. It runs no model, no scanner and nothing from the release.
- [ ] `GITHUB_TOKEN` holds `permissions: {}` at the top level. The audit job has exactly `contents: read`
  and `attestations: read`, and the publish job exactly `contents: read`; neither ever holds a write scope.
  The release's images are public, so they are pulled anonymously and no job holds `packages: read`.
- [ ] The audit App's token is minted with `actions/create-github-app-token` (pinned by SHA) **only in the
  publish job**, narrowed with exactly the `permission-*` inputs the ADR lists. The App's client ID is a
  variable and its private key a secret of the `security-audit` environment only, referenced only in the
  publish job. The audit job references no App secret or variable. No repository-level secret or variable
  holds either. The `release-tag` skill's secrets check lists the environment's secrets as it lists the
  other Apps'.

The result's way from the audit job to the publish job:

- [ ] The model writes one structured result (a JSON file with a schema the publish script owns) under
  `$RUNNER_TEMP`. A model-free step after the model step encrypts it to the publish key's public half (a
  variable of the `security-audit` environment) and uploads **only that ciphertext** as the run's single
  artifact, with a one-day retention. The publish key's private half is a secret of the
  `security-audit` environment referenced only in the publish job; the publish job decrypts the result and
  treats it as untrusted input. The encryption tool is pinned like the other tools (`age`, or an
  equivalent the ADR names).
- [ ] This is chosen over job outputs because a public repository's artifacts are downloadable by anyone
  and a job output is plaintext one `echo`, `set -x` or debug-logged re-run away from the public log;
  ciphertext stays harmless wherever it ends up. The cost is one more secret and one more pinned tool
  (runbook step 2).

The order of steps, and where the subscription token is:

- [ ] Every networked or untrusted-code check runs in a **model-free workflow step before the model
  step**, with no subscription token in its environment, and writes its results under `$RUNNER_TEMP`
  (What a run does, the table). That includes the release-integrity checks with cosign and Sigstore, the
  GHCR image pulls, trivy's database download, osv-scanner, scorecard, gitleaks, the dynamic tests and
  fuzzing. Modules are fetched with `go mod download` in such a step; every later Go command runs with
  `GOFLAGS=-mod=mod GOPROXY=off`. `GITHUB_TOKEN` is passed, through step-level `env:`, only to the steps
  that read GitHub's API, and to no step that runs release code.
- [ ] No release binary is executed outside a container. A model-free step starts the release's
  containers with `--read-only --network none --cap-drop ALL --security-opt no-new-privileges` and no bind
  mounts; the server's HTTP attack runs from a committed harness in a second container on a Docker network
  created with `--internal` (no route off the runner), with the same flags otherwise. The CLI runs on the
  hostile inventories in the release's CLI image the same way, not as a process on the runner (whose
  user has passwordless `sudo`).
- [ ] The model step authenticates with the Claude Code OAuth token from the `security-audit`
  environment's secret alone, never an API key, and runs `claude-fable-5-2` at effort high.
  `secrets.CLAUDE_CODE_OAUTH_TOKEN` appears only in that step's step-level `env:`: never in job or
  workflow `env:`, a `with:` input, or a `run:` script body. The step's environment holds no other
  secret and no `GITHUB_TOKEN`.
- [ ] The model step runs the Claude Code binary by absolute path, verified against its pinned checksum in
  the same step, with a fixed `PATH`, so an earlier step cannot substitute it.

How the model is contained:

- [ ] Claude Code runs with **only the audit's own settings**: it loads no project or local settings
  (the setting sources exclude them, and it runs from a directory outside the checkout, so neither
  `.claude/` nor `CLAUDE.md` loads), uses a strict, empty MCP configuration, and has no hooks. A later
  change to `.claude/settings.json` cannot widen the audit.
- [ ] It runs inside **Claude Code's built-in sandbox**: network only to the model API host and any host
  the ADR shows the CLI truly needs, filesystem writes only under the audit's directory in
  `$RUNNER_TEMP` (not the runner's command files, the checkout or `$HOME`).
- [ ] Its permissions are an **allow list in a non-interactive mode that refuses every tool not on it**
  (the ADR names the mode): reading and searching files, writing under the audit's directory, and, if the
  skill needs a command at all, `Bash` only through the scrubbed-environment wrapper below. It never runs
  with `bypassPermissions` or `--dangerously-skip-permissions`. A deny list (`git push|commit|tag|remote`,
  `gh`, `curl`, `wget`, `nc`, `ssh`, `docker`, `sudo`, `env`, `printenv`, `set`, `/proc/*/environ`) stays
  as defence in depth. The model never runs `docker`. A test pins the allow list, the deny list, the mode
  and the absence of both bypass forms, so dropping or widening any of them fails `make test`.
- [ ] Any command the model runs goes through a committed wrapper that `exec`s it with a scrubbed
  environment (`env -i` plus an allow list of harmless variables). A test, or failing that the first
  run's check (runbook step 7), shows that a Bash tool call in the model step cannot read the OAuth token,
  neither from its own environment nor from `/proc/*/environ` of its ancestors.

Pins, checks and logs:

- [ ] Every action is pinned by full commit SHA with a version comment. Every tool binary (gitleaks,
  osv-scanner, trivy, zizmor, OpenSSF scorecard, cosign, the encryption tool, the Claude Code CLI) is
  pinned by version **and** checksum, with a `# renovate:` comment where Renovate can track it (CLAUDE.md
  rule 7). actionlint and govulncheck reuse the pins `tools/go.mod` and `security.yml` already have. No
  tool is installed from a mutable tag, `@latest` or a `curl | sh`.
- [ ] A check in `test/architecture` (like `check.ReleasePleaseWorkflow`) pins the workflow: its
  triggers, the `main`-only condition, both jobs' permissions and environment, `needs: audit`, the App
  token and key only in the publish job and with exactly its `permission-*` inputs, no App secret in the
  audit job, `secrets.CLAUDE_CODE_OAUTH_TOKEN` only in the model step's step-level `env:`, the publish
  key's private half only in the publish job, the single encrypted artifact and its retention, no
  `actions/cache` of tool output, no `set -x`, the model step after every networked step, the container
  flags, the commit-pinned actions, the time-outs, and Claude Code's flags (its settings file, setting
  sources, strict MCP configuration, no bypass). Changing any of them fails `make check`.
- [ ] Nothing private reaches a log or a readable artifact. Raw tool output, crash inputs, PoCs, Claude
  Code's transcript and its final output stay under `$RUNNER_TEMP`, never printed, never uploaded but
  for the encrypted result. Claude Code's output is redirected to a file, not streamed to the log. The
  step summary and the log show only the audited commit and tag, tool versions and **totals** (public N,
  private N): no per-check outcome, failure or crash count that could point at a private finding. The
  publish job never echoes an API response, advisory URLs and IDs included.

The publish script:

- [ ] The publish script has no model in it and is tested against a fake `gh` with canned API responses
  (as CI's `Release please dry run` job tests the release scripts). It fails closed (publishes nothing
  public) on a result that does not decrypt, does not match the schema, has a field over its length cap,
  has an unknown field, or exceeds the finding limit the ADR sets. Every model-written text is
  Markdown-escaped before it is published: no raw HTML, no images, no links to other hosts.
- [ ] It **derives** public or private itself from each finding's severity and PoC fields: critical,
  high, medium with a PoC, a secret found in history, or a missing or unknown severity is **private**.
  The model's own "public" flag can only make a finding more private, never less. A test covers each row.
- [ ] Before publishing, it runs gitleaks plus patterns for Claude OAuth tokens and GitHub App private keys
  over every text it is about to make public (the report, any issue title and body). On a hit it publishes
  nothing public, and the summary says only "publish refused: see runbook". A test covers it.
- [ ] The PR's title and body, its branch `security-audit/YYYY-MM-DD` and its commit message are fixed
  templates filled only with the date, the `main` SHA, the release tag and counts, never with model text.
- [ ] It opens the report PR only after checking that the branch's diff is exactly
  `docs/security/audits/YYYY-MM-DD.md` and its index line in `docs/security/README.md`, refusing
  anything else or an existing branch. A report it renders from a fixture result passes `test/docs`' title
  and link checks.
- [ ] It creates one draft advisory per private finding. It calls **only a fixed set of endpoints** (GitHub
  reads it needs, creating the `security-audit/*` ref and its two files, creating the PR, searching and
  creating issues, creating a repository security advisory); every `state` it sends is hard-coded `draft`,
  and it never calls the endpoints that publish an advisory, request a CVE, approve or merge a PR, or touch
  a tag, release or other branch. The fake `gh` fails the test on any other endpoint or state.
- [ ] It opens a `security` issue only for a public finding of a report **already merged on `main`** that
  has no open issue yet (matched by the finding's fingerprint), never for the report it is opening
  (open question 7). A test covers both.
- [ ] If GitHub refuses advisory creation to the App, the run fails closed: it publishes only the counts
  and the coverage, and the summary says "N private findings, not filed: see runbook" (open question 2).
- [ ] Values from the run reach the shell only through `env:` and files, never through a `${{ }}`
  expression.

Time and failures:

- [ ] Each step has a `timeout-minutes` and the audit job one below GitHub's 6-hour limit (the ADR gives
  the budget: for example 10 minutes per fuzz target, 60 for dynamic testing). A step that times out is
  recorded in the report's limits, and the run still publishes what it has.
- [ ] A failed run is visible: a run that fails (an expired or revoked subscription token, the
  subscription's usage limit, a tool download whose checksum differs, a refused publish) fails the
  workflow, which GitHub notifies the maintainer of. Its step summary names the cause's kind and the
  runbook step that fixes it, without a secret or finding detail.
- [ ] The maintainer's runbook (below) is in `docs/dev-environment.md`, with a check after each step.

### The first run and the schedule (milestone 3)

- [ ] One `workflow_dispatch` run on `main` against the latest release produces, and nothing else:
  - a docs-only PR opened by `shift2eu-audit[bot]` that adds `docs/security/audits/<date>.md` and its
    index line in `docs/security/README.md`;
  - the draft advisories for its private findings (or none).

  No other branch, tag, release, package, comment, setting or new label changes, and no issue is opened
  (no earlier report is merged yet). The PR body records the run's URL.
- [ ] The report states the `main` SHA, the release tag, the tool versions and the model. It lists every
  check with "clean", "findings" or "not run (why)".
- [ ] After the maintainer has reviewed and merged that report, the `schedule` trigger is enabled (weekly,
  at a time the ADR records). The second scheduled run's report marks each earlier finding as fixed, still
  open or regressed, and its publish job opens the `security` issues for the first report's public
  findings.
- [ ] GitHub silently disables a public repository's scheduled workflows after 60 days without repository
  activity, and a skipped schedule fails nothing. So a **staleness check**, in the `release-tag` skill's
  checks and the dispatch flow, fails when the last scheduled `security-audit.yml` run is more than 8 days
  old, and names runbook step 8. Each report flags a gap when the previous report is more than 8 days
  older, computed by the publish script from `docs/security/audits/`.

## What a run does

The skill holds the triage detail. The workflow's model-free steps do every check that needs the network
or runs untrusted code, before the model starts. The `main` SHA (`github.sha`) and the latest release tag
(`gh release view --json tagName`) are recorded first, and every later step uses those two.

| # | Check | Run by | Network |
|---|---|---|---|
| 1 | Record SHA and release tag | workflow step (audit job) | yes: GitHub reads |
| 2 | Download pinned tools and verify their checksums; `go mod download` for the module and `tools/`; trivy's database | workflow step | yes |
| 3 | govulncheck (source) and osv-scanner on the module and `tools/` | workflow step | yes: vulnerability databases |
| 4 | gitleaks on the full history, `--redact` | workflow step | no |
| 5 | zizmor and actionlint on `.github/workflows` | workflow step | GitHub reads only |
| 6 | OpenSSF scorecard, read-only (no publishing, no `id-token`) | workflow step | yes: GitHub reads |
| 7 | Release download; `checksums.txt`; `gh attestation verify` on every file; `cosign verify` of both images and their provenance and SBOM attestations (Sigstore, GHCR); GitHub's latest release and the images' `latest` tag agree | workflow step | yes |
| 8 | Image pulls from GHCR | workflow step | yes |
| 9 | `govulncheck -mode=binary` on every release binary (read, not run) | workflow step | yes: vulnerability database |
| 10 | trivy on both images and their SBOMs, from the downloaded database | workflow step | no |
| 11 | The images run as non-root, with a read-only root filesystem and no shell where none is expected | workflow step, containers | no |
| 12 | The server attacked by the committed harness: malformed, oversized and deeply nested bodies; slow clients; path traversal; header injection; content-type confusion; every endpoint of the published OpenAPI document | workflow step, containers on an `--internal` network | no |
| 13 | The CLI on hostile inventory files | workflow step, container | no |
| 14 | Time-boxed `go test -fuzz` of the committed `Fuzz*` targets (none yet: open question 5) | workflow step, `GOPROXY=off` | no |
| 15 | Supply-chain reads: action pins and workflow `permissions`, rulesets and environments read the way the `release-tag` skill's step 2 reads them, Renovate's state, dependencies changed since the last report | workflow step | yes: GitHub reads |
| 16 | The `security-audit` checklist across the whole tree, a review of what the agents may run (`.claude/`, `.pi/`, `.dot-agent-deck.toml`, `dev/`), triage of 3 to 15 with a PoC or precise trace for each finding, the status of the last report's findings, the structured result | **model**, sandboxed | model API only |
| 17 | Encrypt the result, upload the ciphertext | workflow step (audit job) | yes: artifact upload |
| 18 | Decrypt, validate, classify, scan, publish | publish job, its script | yes: GitHub writes through the App |

Scorecard's checks that need admin read with a read-only token (Branch-Protection at least; the ADR lists
the rest after the first run) are reported "not run (needs admin read)". The rulesets they would cover
are read in row 15; what a read-only token cannot see there is reported "not run" too. Crash inputs and
PoCs go only into the private result or an advisory, never into `testdata/`.

## Report and triage

- **Report** (`docs/security/audits/YYYY-MM-DD.md`, from the template): summary; scope (commit, tag, tool
  versions, model); findings table (ID, severity critical/high/medium/low, category, status new, open or
  fixed, `path:line` or artifact, scenario, recommended fix); coverage, including what was clean;
  changes since the last audit, and any gap in the schedule; the run's limits.
- **Disclosure split.** The PR is public **the moment it is opened**, before the maintainer reviews it,
  so the split is decided in the run, by the publish script, not the model:

  | Finding | Where it goes |
  |---|---|
  | Critical or high; medium with a working PoC; missing or unknown severity | A draft security advisory (open question 2). The report says only "N findings handled privately". |
  | A secret found in the git history (gitleaks, verified) | Always private: a draft advisory naming the commit and the rule only, never the value. The action is rotation by the maintainer. |
  | Medium without a PoC, low, hardening, informational | The report; after the report is merged, one `security` issue each unless an open issue covers it (open question 7). |
  | Unconfirmed | The report's "needs investigation" list, no issue. |

  The model may mark any finding private; it cannot make a finding public that the table makes private.
- **Signal over noise.** Every finding is confirmed by a PoC or a precise trace. A false positive
  recorded in an earlier report is not reported again.

## The subscription token

- The model step authenticates with a **Claude Code OAuth token** that the maintainer creates with
  `claude setup-token` on their own machine, under the subscription the agents already use. It is stored
  as the secret `CLAUDE_CODE_OAUTH_TOKEN` of the `security-audit` environment **only**, and only the
  model step's step-level `env:` references it. No API key is used.
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

1. [ ] **Skill, ADR, disclosure documents and the docs index.** The `security-weekly-audit` skill, the
   ADR (what is known before PRD #161 milestone 4), `SECURITY.md`, `docs/security/README.md`,
   `docs/security/audits/TEMPLATE.md`, the docs index, and `TestIndexListsEveryPage` accepting a
   sub-index, with its test. No workflow yet. Depends on nothing; can merge before PRD #161 milestone 4.
2. [ ] **The workflow, its two jobs, the pinned tools and their checks.** `security-audit.yml` with
   `workflow_dispatch` only (no `schedule` yet), the committed attack harness, the Claude Code settings,
   sandbox and wrapper and their tests, the result's encryption, the publish script and its fake-`gh`
   tests, the `test/architecture` check of the workflow, the `release-tag` secrets check, the ADR's
   amendment, the runbook. **Depends on 1, on PRD #161 milestone 4 having merged**
   (`feat/161-m4-ai-reviewer`; milestone 3 is `chore/161-m3-remove-maintainer-token`), **and on runbook
   step 3 confirming that the App can create a draft advisory.** It reuses milestone 4's `main`-only
   environment pattern and in-workflow App-token minting, and its way of running Claude Code in Actions
   only as far as open question 3 allows. Runbook steps 1 to 5 come before its merge, step 6 after.
3. [ ] **First run, then the schedule.** One manual run against the latest release, its report PR merged
   by the maintainer, then a PR that adds the `schedule` trigger and the staleness check (and moves this
   PRD to `prds/done/`). Depends on 2 and runbook step 7.

Security-sensitive throughout: **every milestone's PR runs the `security-audit` skill**, and the
workflow, settings, wrapper and publish script get the AI reviewer of PRD #161 milestone 4 like any other
change.

## Maintainer runbook

An agent cannot do these steps. Each one is safe to stop at; the workflow is rolled back by disabling it.

1. **Create the audit App** `shift2eu-audit`. Install it on this repository only, with the permissions
   settled by open question 1, no webhook, and no other repository. Generate its private key. Check: the
   installation lists this repository alone and exactly those permissions.
2. **Create the `security-audit` environment**: deployment branches `main` only, no required reviewers
   (a scheduled run cannot wait for one). Add its variables (the App's client ID, the publish key's public
   half) and secrets (the App's private key, the publish key's private half, made with the encryption
   tool on the maintainer's machine, and the Claude Code OAuth token from `claude setup-token`). Record
   the token's creation date and expiry. Check: `gh api repos/<owner>/<repo>/environments/security-audit`
   shows the branch policy, and the repository-level secrets and variables list none of them.
3. **Create the `security` label** (it does not exist yet), and **confirm advisory creation**: with an
   installation token of the App, `POST /repos/<owner>/<repo>/security-advisories` creates a draft
   advisory; then close it. Check: the advisory existed as a draft and is closed. Milestone 2 is blocked
   until this check passes (open question 2).
4. **Create the `security-audit/**` branch ruleset**: only the audit App, as its one bypass actor, and
   admins may create or update those branches. Check: `gh api repos/<owner>/<repo>/rulesets` lists it
   with the App as its only bypass actor.
5. **Check the other rulesets**: the App is in no bypass list of `Protect main` or the `v*` tag rulesets,
   and `Protect main` still requires the code owner's approval, so an approval by the App does not count
   (as PRD #161 milestone 4 checks). Check: the same `gh api` reads show exactly that.
6. **Merge milestone 2.**
7. **Start the first run** (`workflow_dispatch` on `main`). Read its report PR and advisories. Check that
   the log and the step summary show only totals, that the run's single artifact is ciphertext, and, if
   no test proved it, that a Bash tool call in the model step could not read the OAuth token. Then merge
   the report, then milestone 3's schedule.
8. **Keep the schedule alive.** If the staleness check fails, look at the workflow's runs; if GitHub
   disabled the schedule after 60 days without repository activity, re-enable it
   (`gh workflow enable security-audit.yml`) and start one `workflow_dispatch` run. Check: the next
   scheduled run appears within a week.
9. **Renew the Claude token** before it expires (The subscription token, above), **rotate the App's key
   or the publish key** if either may have leaked, and **rotate any secret** a private finding says is in
   the git history.

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

   Side effects of what is proposed, beyond what the audit needs:
   - **Contents write** also creates releases and tags other than `v*`, deletes unprotected branches (the
     agents' and release-please's), merges into unprotected branches, and sends `repository_dispatch`.
   - **Pull requests write** also submits reviews, `APPROVE` included.
   - **Repository security advisories write** also publishes an advisory and requests a CVE: disclosure.

   *Recommendation:*
   - **Keep Contents write**: GitHub App permissions cannot be narrowed to a path or a branch.
   - **Narrow its use.**
     - The token exists only in the publish job, which has no model and no release code, runs only `main`'s
       committed script, and calls only its fixed set of endpoints, with every advisory `state` hard-coded
       `draft` (milestone 2).
     - The `security-audit/**` ruleset lets only the audit App (its one bypass actor) and admins create or
       update those branches, so another token cannot plant a report (runbook step 4).
     - A CI job, made a **required check**, fails any PR authored by `shift2eu-audit[bot]` that touches
       anything outside `docs/security/`. It passes trivially for other authors, so it costs nothing
       elsewhere.
     - `Protect main` requires the code owner's approval, so an `APPROVE` by the App never counts (as PRD
       #161 milestone 4 checks for its Apps).
     - Consider a tag ruleset on `**` that lets only the release App create tags, which also closes
       non-`v*` tags to every other token.
   - **Never in `Protect main`'s or the `v*` tag rulesets' bypass list.** Its only bypass entry is the
     `security-audit/**` branch ruleset (runbook steps 4 and 5).
   - **Drop Actions read and Attestations read from the App.** The audit's reads use the audit job's
     read-only `GITHUB_TOKEN` (`contents: read`, `attestations: read`), so the App holds write scopes
     only, and only in the publish job. Give it no Workflows permission, so it can never change
     `.github/workflows/`.
   - **No `Administration: read`.** Scorecard's admin-only checks are reported "not run (needs admin
     read)" and the rulesets are read the `release-tag` way instead. Giving them the App's token would put
     an App credential in the audit job, which the two-job split exists to prevent.

2. **Private findings as draft advisories.** Private findings filed as draft repository security
   advisories created by the App, the public report saying only "N findings handled privately".

   *Recommendation:* **Yes, with four conditions.**
   - The model writes private findings only to the encrypted structured result, never to the report. The
     publish script decides what is private (Report and triage) and creates one draft advisory per private
     finding from that result: `state: draft`, no CVE request, no publishing. Only the count reaches the
     report, the log and the summary.
   - Each advisory carries a stable fingerprint (rule, file, sink) so later runs update the "since the
     last audit" status without reading advisory contents back into the model.
   - When the run cannot classify a finding, it is private.
   - **Fallback:** if GitHub refuses advisory creation to the App's installation token, the run fails
     closed: it publishes only the counts and the coverage, and the summary says "N private findings, not
     filed: see runbook". Milestone 2 is blocked until runbook step 3 confirms creation works, so the
     fallback is a guard, not the plan.

3. **How Claude Code runs in Actions.** Options: `anthropics/claude-code-action` pinned by SHA, or the
   Claude Code CLI installed at a pinned version and checksum and run headless (`claude -p`).

   *Recommendation:* **use whatever PRD #161 milestone 4 settles for the reviewer**, so the repository
   has one pinned way to run Claude Code in Actions and one Renovate rule for it, **provided it meets this
   PRD's requirements whatever it is**: only the audit's own settings (no project or local setting
   sources, strict empty MCP configuration, no hooks, run outside the checkout), the built-in sandbox, an
   allow list in a mode that refuses the rest, never a bypass mode, the binary run by absolute path, its
   output kept out of the log, all pinned by the `test/architecture` check. If the reviewer's way cannot
   meet them, the audit uses the pinned CLI and the ADR says why there are two.

4. **A public report PR every week, even with nothing new?**

   *Recommendation:* **yes.** A dated report with no findings is the evidence the audit ran and what it
   covered, and it is one docs-only PR a week to approve. Revisit after the first two months if the
   reviews cost more than they show.

5. **Fuzz targets.** The tree has no `Fuzz*` test today, and the audit must not change code. Fuzzing runs
   in a model-free step before the model, so the model cannot write throwaway harnesses for it. Options:
   committed harnesses that only the audit runs, or a separate issue that adds committed fuzz targets
   (the inventory decoder, the HTTP request decoder, the report renderers) that CI and the audit both run.

   *Recommendation:* **a separate issue for committed fuzz targets**, built test-first like any other
   change. Until it merges, the report lists fuzzing as "not run (no `Fuzz*` targets)" in its limits.

6. **Outbound network from the runner.** GitHub-hosted runners allow any outbound traffic, so a model led
   astray by hostile input (a dependency's advisory text, a scanner's output, a crafted release file)
   could try to send the subscription token or a private finding elsewhere. Options: rely on deny rules
   and containers, add a third-party egress-filtering action, or contain the model step itself.

   *Recommendation:* **contain the model step; no third-party egress action**, which would be a
   privileged action in the job that holds the token.
   - Every networked or untrusted-code check runs in model-free steps before the model step, with no
     token present (What a run does).
   - The model runs in Claude Code's own sandbox, with network only to the model API host and writes
     only under its directory in `$RUNNER_TEMP`, an allow list with default deny, and no `docker`.
   - Any command it runs goes through the scrubbed-environment wrapper, and a test or the first run's
     check shows a Bash call cannot read the token.
   - The token is only in the model step's step-level `env:`, pinned by the `test/architecture` check.
   - Deny rules stay as defence in depth only: Bash patterns are easy to step around.
   - Record in the ADR the residual risk: a sandbox escape or a flaw in the CLI could still leak the
     subscription token, which runbook step 9 rotates.

7. **When public `security` issues are opened.** The issue's plan opens one `security` issue per public
   finding in the run. But the classification is automated and the issues are public at once, while the
   report PR waits for the maintainer's review. Options: the run opens the issues with the PR; the next
   run's publish job opens them from reports the maintainer has merged; the maintainer opens them by hand.

   *Recommendation:* **the next run's publish job opens them from merged reports**, model-free, matched
   by fingerprint so none is opened twice. Nothing becomes an issue that the maintainer has not seen in a
   merged report, no new trigger or workflow is needed, and the delay of up to a week only applies to
   public, low-severity findings. The maintainer can open one sooner by hand.
