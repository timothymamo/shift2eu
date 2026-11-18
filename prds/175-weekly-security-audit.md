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
prompt. The run has three jobs, each on its own fresh runner:

- The **checks job** runs every networked check and every piece of untrusted code, with no model, no
  subscription token and no write credential. It encrypts its raw results to a key only the model job can
  decrypt.
- The **model job** runs no untrusted code. It decrypts the checks' results, runs the model, sandboxed, to
  triage them, and encrypts the structured result to a key only the publish job can decrypt. It is the
  only place the subscription token exists.
- The **publish job** runs `main`'s committed script only. It decrypts the structured result, decides
  itself what is public, and is the only place the audit App's token exists.

Nothing crosses between the jobs but the two ciphertexts. No raw tool output, transcript or private
finding reaches a workflow log or a readable artifact, which anyone can read in a public repository.
`SECURITY.md` tells users which versions are supported and how to report a vulnerability.

## Scope

| | In scope | Out of scope |
|---|---|---|
| Source providers | none: the audit reads no cloud | Live access to AWS, Azure or GCP, cloud credentials of any kind |
| Target providers | none | Live access to OVHcloud, Scaleway or UpCloud, their catalog sources |
| Taxonomy categories | none | |
| Audit subject | The repository on `main` (tree and full history), the latest published release (archives, `checksums.txt`, both images, their attestations and SBOMs, the OpenAPI document), `.github/`, `.claude/`, `.pi/`, `.dot-agent-deck.toml` and `dev/` as attack surface | Other repositories, dependencies' own code beyond advisories, GitHub's or GHCR's services |
| Execution | A scheduled and `workflow_dispatch` workflow on `main`, GitHub-hosted runners, local containers on the runner | A second Lima VM, a launcher script, `dot-agent-deck schedule`, self-hosted runners (Decision 1) |
| Writes | The report PR (`docs/security/` only) and draft security advisories (created and updated, never published), each through the audit App's token in the publish job; `security` issues for public findings of merged reports | Fixing findings in the audit run, editing code or `testdata/`, commenting on or closing other issues and PRs, releases, tags, packages, publishing an advisory or requesting a CVE |
| Repository settings | The `security-audit` environment, the audit App, the `security-audit/**` branch ruleset, a new ruleset on `main` that requires the audit-report-paths check, the `**` tag ruleset if open question 1 adopts it, the `security` label, documented as maintainer steps and read by checks | Changing `Protect main`, the `v*` tag rulesets, the release Apps or their environments (ADR 0021, PRD #161) |
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
  repository, the files the skill itself links to) as untrusted data, never as instructions, and to report
  an embedded instruction as a finding.
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
  scope, the model and its authentication, the three-job split and the two keys, which checks run in the
  checks job and what the model does (the table in What a run does), the disclosure rules the publish
  script enforces with their **residual risk** (the model fills severity and category; Report and
  triage), the App's permissions and their side effects (open question 1), and why the Lima-VM plan of
  the issue was dropped. Milestone 2 amends the same ADR with how Claude Code runs in Actions (open
  question 3), its sandbox, settings, permission mode and audit directory, how the runner's
  user-namespace restriction is handled, the encoded-token residual risk, the encryption and artifact
  handling, the container flags (`--tmpfs` included), and the time budget; it is not written twice.

### The workflow and its enforcement (milestone 2)

Triggers, jobs and permissions:

- [ ] `.github/workflows/security-audit.yml` triggers on `schedule` and `workflow_dispatch` **only**. It
  has no `pull_request`, `pull_request_target`, `push`, `workflow_run` or `issue_comment` trigger, and
  every job runs only when `github.ref` is `refs/heads/main`. All three jobs run in the `security-audit`
  environment, whose deployment branch policy is `main` only, so a `workflow_dispatch` on another ref
  never receives a secret or variable. Every checkout is of `github.sha` (the `main` commit the run
  started on), never a pull request's code or another ref, with `persist-credentials: false`.
- [ ] It has three jobs, each on its own fresh runner:
  - the **checks job** runs every networked or untrusted-code check (What a run does) and references no
    secret;
  - the **model job** has `needs: checks`, runs no check, no release code, no container and nothing it
    downloads but its checksum-pinned tools, and runs the model;
  - the **publish job** has `needs: model`, runs only its committed script, and runs no model, no audit
    check and nothing from the release; its only scanner is gitleaks over the text it is about to publish.
- [ ] `GITHUB_TOKEN` holds `permissions: {}` at the top level. The checks job has exactly
  `contents: read` and `attestations: read`; the model and publish jobs exactly `contents: read` (plus
  `actions: read` where the ADR shows the artifact listing below needs it). No job ever holds a write
  scope. The release's images are public, so they are pulled anonymously and no job holds
  `packages: read`.
- [ ] The audit App's token is minted with `actions/create-github-app-token` (pinned by SHA) **only in the
  publish job**, narrowed with exactly the `permission-*` inputs the ADR lists. The App's client ID is a
  variable and its private key a secret of the `security-audit` environment only, referenced only in the
  publish job. No repository-level secret or variable holds either. The `release-tag` skill's secrets
  check lists the environment's secrets as it lists the other Apps'.

What crosses between the jobs:

- [ ] Two key pairs of the encryption tool (`age`, or an equivalent the ADR names, pinned like the other
  tools): the **model key** and the **publish key**. Each public half is a variable, each private half a
  secret, of the `security-audit` environment. The model key's private half is referenced only in the
  model job's decrypt step, the publish key's only in the publish job's decrypt step.
- [ ] The checks job writes its raw results under its own `$RUNNER_TEMP`, and a last step encrypts them to
  the model key's public half and uploads **only that ciphertext**, as the artifact `checks-result`. The
  model job writes one structured result (a JSON file with a schema the publish script owns), and a
  model-free step after the model step encrypts it to the publish key's public half and uploads only that
  ciphertext, as the artifact `model-result`. Both have a one-day retention.
- [ ] Each job that downloads an artifact downloads it **by exact name**, and first lists the run's
  artifacts: it fails closed if any artifact other than those expected at that point exists, or if a name
  appears twice. Each job treats what it decrypts as untrusted input. A test against the fake `gh`
  (below) covers an extra artifact and a duplicate name for both jobs.
- [ ] Nothing else crosses: no job output, no cache, no other artifact. `github.sha` is the same in every
  job; the release tag travels inside the encrypted results, and the publish script checks that it names
  an existing release.
- [ ] This is chosen over job outputs because a public repository's artifacts are downloadable by anyone
  and a job output is plaintext one `echo`, `set -x` or debug-logged re-run away from the public log;
  ciphertext stays harmless wherever it ends up. Anything in the checks job could forge a ciphertext with
  the public half, which is why the model job reads it as data. The cost is two key pairs and one pinned
  tool (runbook step 2).

The checks job:

- [ ] Every networked or untrusted-code check runs in the checks job (What a run does, the table): the
  release-integrity checks with cosign and Sigstore, the GHCR image pulls, trivy's database download,
  osv-scanner, scorecard, gitleaks, the dynamic tests and fuzzing. Modules are fetched with
  `go mod download` in one step; every later Go command runs with `GOFLAGS=-mod=readonly GOPROXY=off`.
  `GITHUB_TOKEN` is passed, through step-level `env:`, only to the steps that read GitHub's API, and to no
  step that runs release code.
- [ ] No release binary and no fuzz target is executed outside a container. Every such container runs
  with `--read-only --cap-drop ALL --security-opt no-new-privileges`, and a `--tmpfs` only where the ADR
  shows one is needed. The CLI container runs with `--network none`. The server container and the
  committed attack harness's container share one Docker network created with `--internal` (no route off
  the runner) and no other network. Fuzzing runs in a container from a Go image pinned by digest, with
  `--network none` and the module cache copied in.
- [ ] Inputs reach a container one way, and results leave one way: the committed hostile inventories and
  the harness's inputs are copied with `docker cp` into a created, not yet started container; results
  come out on stdout or with `docker cp` from the stopped container. No container has a bind mount of the
  runner's filesystem or the Docker socket.

The model job and where the subscription token is:

- [ ] The model job checks out `github.sha`, downloads and checksum-verifies its pinned tools, downloads
  `checks-result`, and decrypts it into the **audit directory**: a fresh directory the ADR names, outside
  `/home`, `$HOME` and the runner's own directories (on hosted runners `$RUNNER_TEMP` lies under
  `/home/runner`, so it cannot be there without weakening the read deny list below). A model-free step
  copies the checkout into the audit directory, read-only.
- [ ] The model step authenticates with the Claude Code OAuth token from the `security-audit`
  environment's secret alone, never an API key, and runs `claude-fable-5-2` at effort high.
  `secrets.CLAUDE_CODE_OAUTH_TOKEN` appears only in that step's step-level `env:`: never in job or
  workflow `env:`, a `with:` input, or a `run:` script body. The step's environment holds no other secret
  and no `GITHUB_TOKEN`.
- [ ] The model step runs a committed launcher that starts the Claude Code binary by absolute path,
  verified against its pinned checksum in the same step, through `env -i` with an explicit allow list: a
  fresh `HOME` inside the audit directory's own state area, a fixed `PATH`, the token,
  `DISABLE_AUTOUPDATER=1`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, and only what the ADR shows the
  sandbox needs. The launcher fails if a Claude Code managed-settings file (the paths the ADR names)
  exists. A test runs the launcher with a fake Claude Code binary and shows that a variable set through
  `$GITHUB_ENV` in an earlier step (`ANTHROPIC_BASE_URL`, `HTTPS_PROXY`, `NODE_OPTIONS`, `LD_PRELOAD`) is
  not seen, and that a managed-settings file stops it.

How the model is contained:

- [ ] Claude Code runs with **only the audit's own settings file** (`--settings`): its setting sources
  load no user, project or local settings, and it runs from a working directory inside the audit
  directory, so no `.claude/` or `CLAUDE.md` loads. It uses a strict, empty MCP configuration and has no
  hooks. A later change to `.claude/settings.json` cannot widen the audit.
- [ ] The skill's text, read from the `github.sha` checkout, is passed to the model **explicitly** (as
  the prompt or a system-prompt file, the ADR says which); the files it links to are read as data from
  the audit directory's copy of the checkout.
- [ ] It runs inside **Claude Code's sandbox** with `sandbox.enabled: true`,
  `autoAllowBashIfSandboxed: false`, `allowUnsandboxedCommands: false`, no `excludedCommands`, and an
  **empty** network allow list: Bash has no network at all. Bash writes only under the audit directory's
  work area. The Claude Code process itself reaches only the model API, by its settings (the two
  variables above, no `WebFetch` or `WebSearch`).
- [ ] Its permissions are an **allow list in a non-interactive mode that refuses every tool not on it**
  (the ADR names the mode): `Read`, `Grep` and `Glob` only under the audit directory (the checkout copy and
  the checks' results), `Write` and `Edit` only under its work area, and, if the skill needs a command at
  all, `Bash` only through the scrubbed-environment wrapper below. It never runs with `bypassPermissions`
  or `--dangerously-skip-permissions`. A deny list covers `Read`, `Grep` and `Glob` of `/proc`, `/sys`,
  `/etc`, `/home`, `$HOME` and the runner's directories, and keeps the Bash patterns (`git
  push|commit|tag|remote`, `gh`, `curl`, `wget`, `nc`, `ssh`, `docker`, `sudo`, `env`, `printenv`, `set`,
  `/proc/*/environ`) as defence in depth.
- [ ] Any command the model runs goes through a committed wrapper that names the commands it may `exec`
  (the ADR lists them), refuses every other, and `exec`s with a scrubbed environment (`env -i` plus an
  allow list of harmless variables).
- [ ] A test pins the settings file: the setting sources, the sandbox keys above, the empty network
  allow list, the allow list with its path scopes, the deny list, the mode, the absence of `WebFetch`,
  `WebSearch` and both bypass forms. Dropping or widening any of them fails `make test`.
- [ ] Before the model starts, a **sandbox preflight** runs a probe through the same settings: a write
  outside the work area and a TCP connect must both fail. If either succeeds, or bubblewrap cannot start
  (Ubuntu's AppArmor restriction of unprivileged user namespaces), the step fails and its summary names
  the runbook step. Whatever the model-free step changes to let bubblewrap start, the ADR records.
- [ ] A test, or failing that the first run's check (runbook step 8), shows that **no tool call** in the
  model step (`Read`, `Grep`, `Glob` or `Bash`) can read the OAuth token, neither from its own environment
  nor from `/proc/*/environ` of the Claude Code process or its ancestors.

Pins, checks and logs:

- [ ] Every action is pinned by full commit SHA with a version comment. Every tool binary (gitleaks,
  osv-scanner, trivy, zizmor, OpenSSF scorecard, cosign, the encryption tool, the Claude Code CLI, and
  what the sandbox needs if the runner lacks it) is pinned by version **and** checksum, with a
  `# renovate:` comment where Renovate can track it (CLAUDE.md rule 7). actionlint and govulncheck reuse
  the pins `tools/go.mod` and `security.yml` already have. No tool is installed from a mutable tag,
  `@latest` or a `curl | sh`.
- [ ] A check in `test/architecture` (like `check.ReleasePleaseWorkflow`) pins the workflow: its
  triggers, the `main`-only condition, the three jobs' permissions and environment, `needs: checks` and
  `needs: model`, no secret referenced in the checks job, each secret in exactly one job and step (the
  OAuth token in the model step's step-level `env:`, the model key's private half in the model job's
  decrypt step, the publish key's private half and the App key in the publish job), the App token with
  exactly its `permission-*` inputs, the two artifacts by name and their retention, no job outputs, no
  `actions/cache` of tool output, no `set -x`, no check or container step in the model job,
  `GOFLAGS=-mod=readonly GOPROXY=off`, the container flags, the commit-pinned actions, the time-outs, and
  Claude Code's launch (the launcher, its settings file, setting sources, strict MCP configuration, the
  system-prompt or prompt file, no bypass). Changing any of them fails `make check`.
- [ ] Nothing private reaches a log or a readable artifact. Raw tool output, crash inputs, PoCs, Claude
  Code's transcript and its final output stay under the job's temporary or audit directory, never
  printed, never uploaded but encrypted. Claude Code's output is redirected to a file, not streamed to the
  log. The checks and model jobs' summaries and logs show only the audited commit and tag, tool versions,
  and that they finished: no count, per-check outcome, failure or crash count. The publish job computes
  the **totals** (public N, private N) from the decrypted result and prints only them, and never echoes
  an API response, advisory URLs and IDs included.

The publish script:

- [ ] The publish script has no model in it and is tested against a fake `gh` with canned API responses
  (as CI's `Release please dry run` job tests the release scripts). It fails closed (publishes nothing
  public) on a result that does not decrypt, does not match the schema, has a field over its length cap,
  has an unknown field, or exceeds the finding limit the ADR sets. Every model-written text is
  Markdown-escaped before it is published: no raw HTML, no images, no links to other hosts.
- [ ] It **derives** public or private itself, from each finding's severity, confirmation and category
  alone (Report and triage, the table). The model's own "public" flag can only make a finding more
  private, never less. A test covers each row of the table.
- [ ] Before publishing, it scans every text it is about to send (the report, any issue title and body,
  every advisory's text) with gitleaks plus patterns for Claude OAuth tokens and GitHub App private keys,
  and refuses, in public text, any base64 or hex run or high-entropy string over the length the ADR sets
  (longer than a fingerprint). On a hit it publishes nothing, and the summary says only "publish refused:
  see runbook". A test covers a raw token, an encoded token, and a hit in advisory text.
- [ ] The PR's title and body, its branch `security-audit/YYYY-MM-DD` and its commit message are fixed
  templates filled only with the date, the `main` SHA, the release tag, the counts and the run's URL,
  built by the script from `GITHUB_SERVER_URL`, `GITHUB_REPOSITORY` and `GITHUB_RUN_ID`, never with model
  text.
- [ ] It opens the report PR only after checking that the branch's diff is exactly
  `docs/security/audits/YYYY-MM-DD.md` and its index line in `docs/security/README.md`, refusing
  anything else or an existing branch. A report it renders from a fixture result passes `test/docs`' title
  and link checks.
- [ ] Each finding carries a **fingerprint** the script computes (rule, file, sink), written as
  `fp-` and 12 lowercase hex characters in the report's findings table and in each issue and advisory it
  files, so later runs can read it back.
- [ ] It tracks private findings by fingerprint: it lists the repository's advisories, updates a draft
  whose fingerprint recurs, creates a draft for a new one, and marks a draft whose finding this run did
  not reproduce; a recurrence of a finding whose advisory the maintainer closed is filed as a new draft
  marked "regressed". The report shows only counts for private findings (new, still open, not
  reproduced).
- [ ] It calls **only a fixed set of endpoints**: the GitHub reads it needs, creating the
  `security-audit/*` ref and its two files, creating the PR, searching and creating issues, listing,
  creating and updating (`PATCH`) repository security advisories. Every advisory `state` it sends is
  hard-coded `draft`, and it never calls the endpoints that publish an advisory, request a CVE, approve or
  merge a PR, or touch a tag, release or other branch. The fake `gh` fails the test on any other endpoint
  or state.
- [ ] It opens a `security` issue only for a public finding of a report **already merged on `main`**,
  never for the report it is opening (open question 7), and only when no issue, **open or closed**, carries
  that finding's fingerprint. A merged report counts as processed once every public finding in it has
  such an issue, so the work is idempotent and a finding whose issue the maintainer closed is not filed
  again. A test covers an open, a closed, and an unmatched fingerprint, and the report being opened.
- [ ] If GitHub refuses advisory creation to the App, the run fails closed: it publishes only the counts
  and the coverage, and the summary says "N private findings, not filed: see runbook" (open question 2).
- [ ] Values from the run reach the shell only through `env:` and files, never through a `${{ }}`
  expression.

The required path check:

- [ ] A CI job fails any PR authored by `shift2eu-audit[bot]` that touches anything outside
  `docs/security/`, and passes trivially for other authors. It is required by a **new ruleset on `main`**
  (runbook step 5), not by `Protect main`, which stays unchanged.

Time and failures:

- [ ] Each step has a `timeout-minutes`, and each of the three jobs one below GitHub's 6-hour job limit.
  The ADR gives the budget (for example checks 180 minutes, with 10 per fuzz target and 60 for dynamic
  testing; model 120; publish 30). A check that times out is recorded in the report's limits, and the run
  still publishes what it has.
- [ ] A failed run is visible: a run that fails (an expired or revoked subscription token, the
  subscription's usage limit, a tool download whose checksum differs, a failed sandbox preflight, a
  managed-settings file, an unexpected artifact, a refused publish) fails the workflow, which GitHub
  notifies the maintainer of. Its step summary names the cause's kind and the runbook step that fixes it,
  without a secret or finding detail.
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
  at a time the ADR records). The second scheduled run's report marks each earlier public finding as
  fixed, still open or regressed, gives the private counts, and its publish job opens the `security`
  issues for the first report's public findings.
- [ ] GitHub silently disables a public repository's scheduled workflows after 60 days without repository
  activity, and a skipped schedule fails nothing. So a **staleness check**, in the `release-tag` skill's
  checks and the dispatch flow, fails when the last scheduled `security-audit.yml` run is more than 8 days
  old, and names runbook step 9. Each report flags a gap when the previous report is more than 8 days
  older, computed by the publish script from `docs/security/audits/`.

## What a run does

The skill holds the triage detail. The checks job does every check that needs the network or runs
untrusted code; the model job only reads its results. The `main` SHA (`github.sha`) and the latest release
tag (`gh release view --json tagName`) are recorded first, and every later step uses those two.

| # | Check | Job, run by | Network |
|---|---|---|---|
| 1 | Record SHA and release tag | checks, workflow step | yes: GitHub reads |
| 2 | Download pinned tools and verify their checksums; `go mod download` for the module and `tools/`; trivy's database | checks, workflow step | yes |
| 3 | govulncheck (source) and osv-scanner on the module and `tools/` | checks, workflow step | yes: vulnerability databases |
| 4 | gitleaks on the full history, `--redact` | checks, workflow step | no |
| 5 | zizmor and actionlint on `.github/workflows` | checks, workflow step | GitHub reads only |
| 6 | OpenSSF scorecard, read-only (no publishing, no `id-token`) | checks, workflow step | yes: GitHub reads |
| 7 | Release download; `checksums.txt`; `gh attestation verify` on every file; `cosign verify` of both images and their provenance and SBOM attestations (Sigstore, GHCR); GitHub's latest release and the images' `latest` tag agree | checks, workflow step | yes |
| 8 | Image pulls from GHCR | checks, workflow step | yes |
| 9 | `govulncheck -mode=binary` on every release binary (read, not run) | checks, workflow step | yes: vulnerability database |
| 10 | trivy on both images and their SBOMs, from the downloaded database | checks, workflow step | no |
| 11 | The images run as non-root, with a read-only root filesystem and no shell where none is expected | checks, containers | no |
| 12 | The server attacked by the committed harness: malformed, oversized and deeply nested bodies; slow clients; path traversal; header injection; content-type confusion; every endpoint of the published OpenAPI document | checks, server and harness containers on an `--internal` network | no |
| 13 | The CLI on hostile inventory files | checks, container, `--network none` | no |
| 14 | Time-boxed `go test -fuzz` of the committed `Fuzz*` targets (none yet: open question 5) | checks, container, `--network none`, `GOPROXY=off` | no |
| 15 | Supply-chain reads: action pins and workflow `permissions`, rulesets and environments read the way the `release-tag` skill's step 2 reads them, Renovate's state, dependencies changed since the last report | checks, workflow step | yes: GitHub reads |
| 16 | Encrypt the raw results to the model key, upload `checks-result` | checks, workflow step | yes: artifact upload |
| 17 | Check the run's artifacts, download `checks-result`, decrypt into the audit directory, copy the checkout, sandbox preflight | model, workflow steps | yes: tools, artifact download |
| 18 | The `security-audit` checklist across the whole tree, a review of what the agents may run (`.claude/`, `.pi/`, `.dot-agent-deck.toml`, `dev/`), triage of 3 to 15 with a PoC or precise trace for each finding, the status of the last report's public findings, the structured result | model, **Claude Code**, sandboxed | Bash: none; Claude Code: model API only |
| 19 | Encrypt the result to the publish key, upload `model-result` | model, workflow step | yes: artifact upload |
| 20 | Check the run's artifacts, download and decrypt `model-result`, validate, classify, scan, publish | publish, its script | yes: GitHub writes through the App |

Scorecard's checks that need admin read with a read-only token (Branch-Protection at least; the ADR lists
the rest after the first run) are reported "not run (needs admin read)". The rulesets they would cover
are read in row 15; what a read-only token cannot see there is reported "not run" too. Crash inputs and
PoCs go only into the encrypted results or an advisory, never into `testdata/`.

## Report and triage

- **Report** (`docs/security/audits/YYYY-MM-DD.md`, from the template): summary; scope (commit, tag, tool
  versions, model); findings table (fingerprint, severity critical/high/medium/low/informational,
  category, status new, open or fixed, `path:line` or artifact, scenario, recommended fix); counts of
  private findings; coverage, including what was clean; changes since the last audit, and any gap in the
  schedule; the run's limits.
- **Disclosure split.** The PR is public **the moment it is opened**, before the maintainer reviews it,
  so the split is decided in the run by the publish script, from severity, confirmation and category
  alone:

  | Finding | Where it goes |
  |---|---|
  | Critical, high or medium | A draft security advisory (open question 2). The report says only "N findings handled privately". |
  | Unconfirmed, suspected medium or above | A draft advisory, marked unconfirmed. |
  | Category injection, authentication bypass, credential exposure or remote code execution, **whatever the severity** | A draft advisory. |
  | Missing or unknown severity or category | A draft advisory. |
  | A secret found in the git history (gitleaks) | Always private: a draft advisory naming the commit and the rule only, never the value, and stating that triage confirmed it a real secret, not a fixture; it is **never tried against the service**. The action is rotation by the maintainer. |
  | Confirmed low, hardening or informational | The report; after the report is merged, one `security` issue each unless an issue, open or closed, carries its fingerprint (open question 7). |
  | Unconfirmed, suspected low or informational | The report's "needs investigation" list, no issue. |

  The model may mark any finding private; it cannot make a finding public that the table makes private.
- **Residual risk.** The script decides, but the model fills the severity, confirmation and category the
  script reads. A model that mislabels a finding, or is led by hostile input to call a critical one "low,
  hardening", makes it public the moment the PR opens. The category floor and the private default for
  anything unknown narrow this; they do not close it. The PRD and the ADR state it; the maintainer's
  review of each report is the last check, after publication.
- **Signal over noise.** Every finding is confirmed by a PoC or a precise trace, or listed as
  unconfirmed. A false positive recorded in an earlier report is not reported again.

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
2. [ ] **The workflow, its three jobs, the pinned tools and their checks.** `security-audit.yml` with
   `workflow_dispatch` only (no `schedule` yet), the committed attack harness, the Claude Code launcher,
   settings, sandbox preflight and wrapper and their tests, the two encrypted hand-offs, the publish
   script and its fake-`gh` tests, the required path check, the `test/architecture` check of the
   workflow, the `release-tag` secrets check, the ADR's amendment, the runbook. **Depends on 1, on PRD
   #161 milestone 4 having merged** (`feat/161-m4-ai-reviewer`; milestone 3 is
   `chore/161-m3-remove-maintainer-token`), **and on runbook step 3 confirming that the App can create and
   update a draft advisory.** From milestone 4 it reuses the `main`-only environment pattern and
   in-workflow App-token minting; its way of running Claude Code only if milestone 4 runs Claude Code at
   all and that way meets this PRD's requirements (open question 3). Runbook steps 1 to 6 come before its
   merge, step 7 is the merge, step 8 comes after.
3. [ ] **First run, then the schedule.** One manual run against the latest release, its report PR merged
   by the maintainer, then a PR that adds the `schedule` trigger and the staleness check (and moves this
   PRD to `prds/done/`). Depends on 2 and runbook step 8.

Security-sensitive throughout: **every milestone's PR runs the `security-audit` skill**, and the
workflow, settings, launcher, wrapper and publish script get the AI reviewer of PRD #161 milestone 4 like
any other change.

## Maintainer runbook

An agent cannot do these steps. Each one is safe to stop at; the workflow is rolled back by disabling it.

1. **Create the audit App** `shift2eu-audit`. Install it on this repository only, with the permissions
   settled by open question 1, no webhook, and no other repository. Generate its private key. Check: the
   installation lists this repository alone and exactly those permissions.
2. **Create the `security-audit` environment**: deployment branches `main` only, no required reviewers
   (a scheduled run cannot wait for one). Make **two key pairs** with the encryption tool on the
   maintainer's machine, the model key and the publish key. Add the variables (the App's client ID, both
   public halves) and the secrets (the App's private key, both private halves, and the Claude Code OAuth
   token from `claude setup-token`). Record the token's creation date and expiry. Delete the two private
   key files from the machine once stored. Check: `gh api repos/<owner>/<repo>/environments/security-audit`
   shows the branch policy, and the repository-level secrets and variables list none of them.
3. **Create the `security` label** (it does not exist yet), and **confirm advisory creation** with an
   installation token minted by hand. Recommended: the documented local command in the runbook, which
   signs a short-lived JWT with the App's private key file (`openssl`) and exchanges it with
   `POST /app/installations/<id>/access_tokens`; a one-off workflow would itself have to be merged to
   `main` to reach the environment. With that token, `POST /repos/<owner>/<repo>/security-advisories`
   creates a draft advisory, a `PATCH` with `state: draft` updates it, then close it. Then delete the App's
   private key file from the machine (it lives only in the environment secret); the installation token
   expires within an hour. Check: the advisory existed as a draft, was updated, and is closed; the key
   file is gone. Milestone 2 is blocked until this check passes (open question 2).
4. **Create the `security-audit/**` branch ruleset**: only the audit App, as its one bypass actor, and
   admins may create or update those branches. Check: `gh api repos/<owner>/<repo>/rulesets` lists it
   with the App as its only bypass actor.
5. **Create the audit-paths ruleset on `main`**: it requires the CI check that fails an audit PR touching
   anything outside `docs/security/`, with no bypass actor. If open question 1's tag ruleset is adopted,
   create the `**` tag ruleset too, with only the release App as bypass actor. Check: the same `gh api`
   read lists them, and `Protect main` is unchanged.
6. **Check the other rulesets**: the App is in no bypass list of `Protect main`, the audit-paths ruleset
   or the tag rulesets, and `Protect main` still requires the code owner's approval, so an approval by the
   App does not count (as PRD #161 milestone 4 checks). Check: the same `gh api` reads show exactly that.
7. **Merge milestone 2.**
8. **Start the first run** (`workflow_dispatch` on `main`). Read its report PR and advisories. Check that
   the logs and step summaries show only totals in the publish job and "finished" in the others, that the
   run's two artifacts are ciphertext, and, if no test proved it, that no tool call in the model step
   could read the OAuth token. Then merge the report, then milestone 3's schedule.
9. **Keep the schedule alive.** If the staleness check fails, look at the workflow's runs; if GitHub
   disabled the schedule after 60 days without repository activity, re-enable it
   (`gh workflow enable security-audit.yml`) and start one `workflow_dispatch` run. Check: the next
   scheduled run appears within a week.
10. **Renew the Claude token** before it expires (The subscription token, above), **rotate the App's key,
    the model key or the publish key** if any may have leaked, and **rotate any secret** a private
    finding says is in the git history. If the sandbox preflight fails, read the ADR's note on the
    runner's user-namespace restriction before changing anything.

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

("No launcher script" refers to the issue's VM launcher; the model job's committed launcher above is a
step of the workflow.)

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
     - A CI job fails any PR authored by `shift2eu-audit[bot]` that touches anything outside
       `docs/security/`, required through a **new ruleset on `main`** with no bypass actor, so
       `Protect main` stays unchanged (runbook step 5). It passes trivially for other authors, so it costs
       nothing elsewhere.
     - `Protect main` requires the code owner's approval, so an `APPROVE` by the App never counts (as PRD
       #161 milestone 4 checks for its Apps).
     - Consider a tag ruleset on `**` that lets only the release App create tags, which also closes
       non-`v*` tags to every other token (runbook step 5 if adopted).
   - **Never in a bypass list** of `Protect main`, the audit-paths ruleset or the tag rulesets. Its only
     bypass entry is the `security-audit/**` branch ruleset (runbook steps 4 to 6).
   - **Drop Actions read and Attestations read from the App.** The audit's reads use the jobs' read-only
     `GITHUB_TOKEN`, so the App holds write scopes only, and only in the publish job. Give it no Workflows
     permission, so it can never change `.github/workflows/`.
   - **No `Administration: read`.** Scorecard's admin-only checks are reported "not run (needs admin
     read)" and the rulesets are read the `release-tag` way instead. Giving them the App's token would put
     an App credential in the checks job, which the three-job split exists to prevent.

2. **Private findings as draft advisories.** Private findings filed as draft repository security
   advisories created by the App, the public report saying only "N findings handled privately".

   *Recommendation:* **Yes, with five conditions.**
   - The model writes private findings only to the encrypted structured result, never to the report. The
     publish script decides what is private (Report and triage) and creates one draft advisory per private
     finding from that result: `state: draft`, no CVE request, no publishing. Only the counts reach the
     report and the publish job's summary.
   - Each advisory carries the finding's fingerprint. The publish script, not the model, tracks private
     findings across runs: it lists the advisories, updates a recurring one with `PATCH` (state stays
     `draft`), and marks one not reproduced. The model never sees an earlier private finding.
   - When the run cannot classify a finding, it is private.
   - Advisory text is scanned for secrets and encoded runs like public text, because a draft advisory is
     one click from publication.
   - **Fallback:** if GitHub refuses advisory creation to the App's installation token, the run fails
     closed: it publishes only the counts and the coverage, and the summary says "N private findings, not
     filed: see runbook". Milestone 2 is blocked until runbook step 3 confirms creation and update work,
     so the fallback is a guard, not the plan.

3. **How Claude Code runs in Actions.** Options: `anthropics/claude-code-action` pinned by SHA, or the
   Claude Code CLI installed at a pinned version and checksum and run headless (`claude -p`) through the
   committed launcher.

   *Recommendation:* PRD #161 milestone 4 has not decided that its reviewer runs Claude Code at all (its
   own open question: a pinned action or a small script calling the provider's API). **If milestone 4
   runs Claude Code, reuse its way**, so the repository has one pinned way and one Renovate rule,
   **provided it meets this PRD's requirements**: started through the launcher's `env -i` allow list with
   a fresh `HOME`, no managed-settings file, only the audit's settings file (no user, project or local
   setting sources), the skill passed explicitly, strict empty MCP configuration, no hooks, the sandbox
   keys and empty Bash network, path-scoped read tools, an allow list in a mode that refuses the rest,
   never a bypass mode, the binary run by absolute path, its output kept out of the log, all pinned by the
   `test/architecture` check. **Otherwise, or if that way cannot meet them, use the pinned CLI**, and the
   ADR says why. A GitHub Action that hides the launch behind its own inputs is unlikely to meet the
   `env -i` requirement, which favours the CLI.

4. **A public report PR every week, even with nothing new?**

   *Recommendation:* **yes.** A dated report with no findings is the evidence the audit ran and what it
   covered, and it is one docs-only PR a week to approve. Revisit after the first two months if the
   reviews cost more than they show.

5. **Fuzz targets.** The tree has no `Fuzz*` test today, and the audit must not change code. Fuzzing runs
   in a container in the checks job, which has no model, so the model cannot write throwaway harnesses
   for it. Options: committed harnesses that only the audit runs, or a separate issue that adds committed
   fuzz targets (the inventory decoder, the HTTP request decoder, the report renderers) that CI and the
   audit both run.

   *Recommendation:* **a separate issue for committed fuzz targets**, built test-first like any other
   change. Until it merges, the report lists fuzzing as "not run (no `Fuzz*` targets)" in its limits.

6. **Outbound network from the runner.** GitHub-hosted runners allow any outbound traffic, so a model led
   astray by hostile input (a dependency's advisory text, a scanner's output, a crafted release file)
   could try to send the subscription token or a private finding elsewhere. Options: rely on deny rules
   and containers, add a third-party egress-filtering action, or contain the model step itself.

   *Recommendation:* **contain the model step; no third-party egress action**, which would be a
   privileged action in the job that holds the token.
   - The model runs in its own job, on a fresh runner that runs no untrusted code: every networked or
     untrusted-code check runs in the checks job, which holds no token, and reaches the model job only as
     ciphertext.
   - **Bash: no network; the Claude Code process: the model API only, by its settings.** The sandbox has
     an empty network allow list, cannot be bypassed per command, and does not auto-allow; a preflight
     proves it holds before the model starts.
   - Claude Code is started through `env -i` with a fresh `HOME`, so nothing an earlier step wrote to
     `$GITHUB_ENV` or a settings file reaches it, and its read tools see only the audit directory.
   - Any command it runs goes through the wrapper, which names what it may `exec` and scrubs the
     environment; a test or the first run's check shows no tool call can read the token.
   - The token is only in the model step's step-level `env:`, pinned by the `test/architecture` check.
   - Deny rules stay as defence in depth only: Bash patterns are easy to step around.
   - Record in the ADR the residual risks: a sandbox escape or a flaw in the CLI could still leak the
     subscription token; and a token the model encodes in a way the publish scan does not recognise (split
     across fields, re-encoded beyond base64 and hex) could reach the public report. Runbook step 10
     rotates it.

7. **When public `security` issues are opened.** The issue's plan opens one `security` issue per public
   finding in the run. But the classification is automated and the issues are public at once, while the
   report PR waits for the maintainer's review. Options: the run opens the issues with the PR; the next
   run's publish job opens them from reports the maintainer has merged; the maintainer opens them by hand.

   *Recommendation:* **the next run's publish job opens them from merged reports**, model-free, matched
   by the fingerprint in the report's findings table against every issue, open or closed, so none is
   opened twice and one the maintainer closed is not reopened. Nothing becomes an issue that the
   maintainer has not seen in a merged report, no new trigger or workflow is needed, and the delay of up
   to a week only applies to public findings, which are now confirmed low, hardening or informational
   ones. The maintainer can open one sooner by hand.
