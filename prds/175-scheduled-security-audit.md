# PRD #175: A scheduled read-only security audit of the repository and the latest release

**Status:** Draft
**Issue:** #175

**Scope note (2026-11-20):** `shift2eu-server`, the HTTP API, is no longer released (ADR 0037, issue
#393). The audit covers what ships: the CLI's release archives and its signed image. The server, its
image, its OpenAPI document and the server attack harness this PRD first named are out of scope; row 12
of the checks is dropped and the other rows keep their numbers.

## Problem

shift2eu ships a binary and a signed image that read other people's cloud estates. Every change is
reviewed: each unit's `security-audit` pass, the AI reviewer of PRD #161 milestone 4, the maintainer's
approval, and gosec, govulncheck and CodeQL in CI. But all of them look at a **diff**. Nothing looks at the
whole tree, its git history and the **released** artifacts as an attacker would. Nothing tests the
released CLI with hostile input, checks a published release end to end against
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

Once a month on a schedule, and whenever the maintainer starts it by hand (`workflow_dispatch`), a GitHub
Actions workflow on `main` audits the repository and its **latest published release** with
**Claude Fable 5.2** (`claude-fable-5-2`, effort high). Only the maintainer, who owns the repository, can
start a run by hand: the workflow takes no inputs, and its jobs run for no other triggering actor. It runs
on fresh GitHub-hosted runners and authenticates with the maintainer's Claude subscription token.

It scans the whole tree and its history, verifies the release's checksums, attestations and signatures,
scans the image and binaries, and fuzzes and attacks the release **locally**: in containers on the
runner, with no route off it. It never changes code, pushes to a branch other than its own report branch,
or touches a cloud, GitHub's or GHCR's services beyond reading them, or any remote host.

Its results go three ways:

- a dated, public report in a **docs-only pull request** under `docs/security/`, opened by the audit App
  and merged only with the maintainer's code-owner approval;
- a `security` issue per public finding, opened only **after** the maintainer has merged the report that
  holds it (Decision 7);
- a **draft repository security advisory** per private finding, the report saying only whether private
  findings exist, never how many (Decision 2).

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
| Audit subject | The repository on `main` (tree and full history), the latest published release (archives, `checksums.txt`, the CLI image, its attestations and SBOM), `.github/`, `.claude/`, `.pi/`, `.dot-agent-deck.toml` and `dev/` as attack surface | Other repositories, dependencies' own code beyond advisories, GitHub's or GHCR's services |
| Execution | A scheduled and `workflow_dispatch` workflow on `main`, GitHub-hosted runners, local containers on the runner | A second Lima VM, a launcher script, `dot-agent-deck schedule`, self-hosted runners (Decisions, Where) |
| Writes | The report PR (`docs/security/` only) and draft security advisories (created and updated, never published), each through the audit App's token in the publish job; `security` issues for public findings of merged reports | Fixing findings in the audit run, editing code or `testdata/`, commenting on or closing other issues and PRs, releases, tags, packages, publishing an advisory or requesting a CVE |
| Repository settings | The `security-audit` environment, the audit App, the `security-audit/**` branch ruleset, a new ruleset on `main` that requires the audit-report-paths check, the default branch ruleset on `~ALL` excluding the default branch (`Protect main` guards it) and `security-audit/**` (runbook step 5), the `**` tag ruleset if the maintainer adopts it (Decision 1 leaves it optional), the `security` label, removing Actions write from any App other than the release flow's that holds it (runbook step 6), documented as maintainer steps and read by checks | Changing `Protect main`, the `v*` tag rulesets, the release Apps or their environments (ADR 0021, PRD #161) |
| Skills, tests and docs | A `security-audit-run` skill that reuses `security-audit`'s checklist and is distinct from it, an ADR, `SECURITY.md`, `docs/security/README.md`, the report template, `test/docs`' index check accepting a sub-index, `docs/dev-environment.md` (the runbook), a staleness check of the schedule | Rewriting `security-audit` |

## Acceptance criteria

### The skill, the ADR and the disclosure documents (milestone 1)

- [x] `.claude/skills/security-audit-run/SKILL.md` exists, agent-neutral like the other skills, and
  **links to `security-audit`'s checklist instead of copying it**. It is a separate skill from
  `security-audit` (which audits a diff, branch or PR and stays unchanged), and its description says so,
  so an agent asked for a change's audit does not pick it. Its name carries no cadence. It adds the
  whole-tree and release triage (What a run does, below), the rules of engagement and the triage rules.
  `make check` passes, `test/docs`' link and skill-shell checks included.
- [x] The skill frames every task as an authorized test of this project's own artifacts, run locally. It
  says that a step the model declines is reported in the report's limits section, never worked around.
  It tells the model to treat every file it reads (scanner output, advisory text, release files, the
  repository, the files the skill itself links to) as untrusted data, never as instructions, and to report
  an embedded instruction as a finding.
- [x] A root `SECURITY.md` names the supported versions (the latest minor release), points reporters to
  GitHub's private vulnerability reporting (already enabled), states the response the maintainer commits
  to, and links to the audit reports. It contains no personal e-mail address.
- [x] `TestIndexListsEveryPage` (`test/docs/archive_test.go`) accepts a page under `docs/` that is linked
  either from `docs/README.md` or from a **sub-index**: a `README.md` in a subdirectory of `docs/` that
  `docs/README.md` itself links (one level, no chains). A page linked only from a sub-index that
  `docs/README.md` does not link still fails. A link to a directory, from either, lists every page
  directly inside it, not those in its subdirectories (issue #280). A test pins each case. This lets
  each dated report be indexed by `docs/security/README.md`'s link to `audits/` alone, with no edit per
  report.
- [x] `docs/security/README.md` explains the scheduled audit and its current cadence, the disclosure split
  and how to verify a release, and indexes the reports in `docs/security/audits/` by their naming pattern
  and a link to the directory (issue #280). `docs/README.md` links
  it. `docs/security/audits/TEMPLATE.md` holds the report's sections (Report and triage, below), has
  exactly one title, and is linked from `docs/security/README.md`. `TestIndexListsEveryPage` and
  `TestPagesHaveOneTitle` pass.
- [x] `docs/security/README.md` is the one user-facing text that states the cadence. `SECURITY.md`, the
  `security-audit-run` skill and the report template name no cadence and link to
  `docs/security/README.md` where they refer to it.
- [x] An ADR records the audit's design **as far as it is known before PRD #161 milestone 4 merges**: its
  scope; the model and its authentication; the subscription's usage limits, shared with the agents and
  the AI reviewer, and the scheduled time chosen for them; the cadence and the values derived from it
  (the list in milestone 3); who can start a run (the maintainer only, with no inputs), the one guard
  condition every job and the concurrency group use (milestone 2), and that it skips every re-run,
  because a re-run of a scheduled run keeps `event_name == 'schedule'` with the re-runner as
  `triggering_actor`; that `github.repository_owner` becomes the organisation if the repository is
  transferred to one, so manual runs then fail closed while scheduled runs still run; the three-job
  split and the two keys; which checks run in the checks job and what the model does (the table in What
  a run does); the disclosure rules the publish script enforces with their **residual risk** (the model
  fills severity and category, Report and triage; the public report's private-findings flag still says
  that something exists, Decision 2); the App's permissions and their side effects (Decision 1); the rule
  that no App installed on the repository other than the release flow's holds Actions write, with its
  reason (runbook step 6); and why the Lima-VM plan of the issue was dropped. Milestone 2 amends the same
  ADR with how Claude Code runs in Actions (Decision 3), its sandbox, settings, permission mode and audit
  directory, how the runner's user-namespace restriction is handled, the sandbox preflight's six probes
  and the model-free step that stops Docker and removes the runner user's sudo rule, the encoded-run
  thresholds (the minimum length L and the entropy N in bits per character, The publish script below) and
  the encoded-token residual risk, the encryption and artifact handling, the Actions-cache residual risk,
  the container flags (`--tmpfs` included), and the time budget; it is not written twice. It also states
  that the Claude subscription is not a Team or Enterprise plan, so no server-managed settings reach the
  model step, or, if it is one, how the launcher checks those settings.

### The workflow and its enforcement (milestone 2)

Triggers, jobs and permissions:

- [ ] `.github/workflows/scheduled-security-audit.yml` triggers on `schedule` and `workflow_dispatch`
  **only**, and its `workflow_dispatch` takes no `inputs:`. It has no `pull_request`,
  `pull_request_target`, `push`, `workflow_run` or `issue_comment` trigger. The workflow has **one guard
  condition**, used verbatim by every job's `if:` and inside the concurrency group below:

  `github.ref == 'refs/heads/main' && github.run_attempt == 1 && (github.event_name == 'schedule' || github.triggering_actor == github.repository_owner)`

  Every job therefore runs only on `main`, only on a run's first attempt, and only for the schedule or
  for the maintainer: the maintainer owns the repository, so only they can start a run by hand, and no
  login is hard-coded. `github.run_attempt == 1` skips every re-run: a re-run of a scheduled run keeps
  `event_name == 'schedule'` with the re-runner as `triggering_actor`, so without it anyone with Actions
  write could re-run a past scheduled run. All three jobs run in the `security-audit` environment, whose
  deployment branch policy is `main` only, so a `workflow_dispatch` on another ref never receives a
  secret or variable. Every checkout is of `github.sha` (the `main` commit the run started on), never a
  pull request's code or another ref, with `persist-credentials: false`.
- [ ] The workflow sets a top-level `concurrency` with `cancel-in-progress: false` and a group that
  applies only to runs whose jobs will run, so a run that would be skipped never queues behind or
  cancels a real one. The group embeds the guard condition verbatim:
  `group: ${{ <guard condition> && 'scheduled-security-audit' || format('skipped-{0}', github.run_id) }}`.
  A run started while another is in progress waits for it instead of racing its publish job: neither of
  two runs is cancelled; with three, GitHub keeps one pending and cancels the older pending run, which
  the maintainer then starts again.
- [ ] It has three jobs, each on its own fresh runner:
  - the **checks job** runs every networked or untrusted-code check (What a run does) and references no
    secret other than `GITHUB_TOKEN` (as `github.token`), and that only in step-level `env:`;
  - the **model job** has `needs: checks`, runs no check, no release code, no container and nothing it
    downloads but its checksum-pinned tools, and runs the model;
  - the **publish job** has `needs: model`, runs only its committed script, and runs no model, no audit
    check and nothing from the release; its only scanner is gitleaks over the text it is about to publish.
- [ ] `GITHUB_TOKEN` holds `permissions: {}` at the top level. The checks job has exactly the read
  scopes the ADR lists after the first run: at least `contents: read` and `attestations: read`, and
  `actions: read` and `issues: read` if scorecard or the supply-chain reads (row 15) need them, never a
  write scope. A read the token is refused is reported "not run", as scorecard's admin checks are. The
  model and publish jobs have exactly `contents: read` (plus `actions: read` where the ADR shows the
  artifact listing below needs it). No job ever holds a write scope. The release's image is public, so
  it is pulled anonymously and no job holds `packages: read`.
- [ ] The audit App's token is minted with `actions/create-github-app-token` (pinned by SHA) **only in the
  publish job**, narrowed with exactly the `permission-*` inputs the ADR lists. The App's client ID is a
  variable and its private key a secret of the `security-audit` environment only, referenced only in the
  publish job. No repository-level secret or variable holds either. The `release-tag` skill's secrets
  check lists the environment's secrets as it lists the other Apps'.

What crosses between the jobs:

- [ ] Two key pairs of the encryption tool (`age`, or an equivalent the ADR names, pinned like the other
  tools): the **model key** and the **publish key**. Each public half is a variable, each private half a
  secret, of the `security-audit` environment. The model key's private half is referenced only in the
  model job's decrypt step, the publish key's only in the publish job's decrypt step. Each decrypt step
  passes the private half to the tool on stdin, or writes it to a file only under that job's
  `$RUNNER_TEMP` (read-denied to the model) and deletes the file in the same step.
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
  shows one is needed. The CLI container runs with `--network none`. Fuzzing runs in a container from a Go image pinned by digest, with
  `--network none` and the module cache copied in.
- [ ] Inputs reach a container one way, and results leave one way: the committed hostile inventories are
  copied with `docker cp` into a created, not yet started container; results
  come out on stdout or with `docker cp` from the stopped container. No container has a bind mount of the
  runner's filesystem or the Docker socket.

The model job and where the subscription token is:

- [ ] The model job checks out `github.sha`, downloads and checksum-verifies its pinned tools, downloads
  `checks-result`, and decrypts it into the **audit directory**: a fresh directory the ADR names, outside
  `/home`, the runner's `$HOME` (`/home/runner`) and the runner's own directories (on hosted runners
  `$RUNNER_TEMP` lies under `/home/runner`, so it cannot be there without weakening the read deny list
  below). A model-free step copies the checkout into the audit directory, read-only.
- [ ] The model step authenticates with the Claude Code OAuth token from the `security-audit`
  environment's secret alone, never an API key, and runs `claude-fable-5-2` at effort high.
  `secrets.CLAUDE_CODE_OAUTH_TOKEN` appears only in that step's step-level `env:`: never in job or
  workflow `env:`, a `with:` input, or a `run:` script body. The step's environment holds no other secret
  and no `GITHUB_TOKEN`.
- [ ] The model step runs a committed launcher that starts the Claude Code binary by absolute path,
  verified against its pinned checksum in the same step, through `env -i` with an explicit allow list: a
  fresh `HOME` inside the audit directory's own state area, a fixed `PATH`, the token,
  `DISABLE_AUTOUPDATER=1`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, and only what the ADR shows the
  sandbox needs, and a dummy canary variable for the preflight's probe 5. The launcher fails if a Claude
  Code managed-settings file (the paths the ADR names) exists; server-managed settings are covered by the
  ADR's statement on the subscription plan. A test runs the launcher with a fake Claude Code binary and
  shows that a variable set through `$GITHUB_ENV` in an earlier step (`ANTHROPIC_BASE_URL`,
  `HTTPS_PROXY`, `NODE_OPTIONS`, `LD_PRELOAD`) is not seen, and that a managed-settings file stops it.

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
  work area. The Claude Code process itself makes no connection but to the model API **by its
  configuration** (the two variables above, no `WebFetch` or `WebSearch`). That is not enforced: the
  sandbox wraps Bash only, and the unsandboxed CLI process could open any connection. It is a residual
  risk (Decision 6), and no test claims otherwise.
- [ ] Its permissions are an **allow list in a non-interactive mode that refuses every tool not on it**
  (the ADR names the mode): `Read`, `Grep` and `Glob` only of the audit directory's checkout copy, the
  checks' results and the work area, `Write` and `Edit` only under the work area, and, if the skill needs
  a command at all, `Bash` only through the scrubbed-environment wrapper below. It never runs with
  `bypassPermissions` or `--dangerously-skip-permissions`. The deny list names **literal paths**, not
  `~` or `$HOME` (in Claude Code's rules `~` is the launcher's fresh `HOME`, inside the audit directory):
  `Read`, `Grep` and `Glob` of `/proc`, `/sys`, `/etc`, `/home` and `/home/runner` (which holds
  `$RUNNER_TEMP`, the workspace and the runner's own directories), and `Read`, `Edit`, `Write`, `Grep` and
  `Glob` of the audit directory's state area (the fresh `HOME`, with Claude Code's own configuration and
  transcripts). It keeps the Bash patterns (`git push|commit|tag|remote`, `gh`, `curl`, `wget`, `nc`,
  `ssh`, `docker`, `sudo`, `env`, `printenv`, `set`, `/proc/*/environ`) as defence in depth.
- [ ] Any command the model runs goes through a committed wrapper that names the commands it may `exec`
  (the ADR lists them), refuses every other, and `exec`s with a scrubbed environment (`env -i` plus an
  allow list of harmless variables).
- [x] A test pins the settings file: the setting sources, the sandbox keys above, the empty network
  allow list, the allow list with its literal path scopes (only the checkout copy, the checks' results and
  the work area), the deny list with its literal paths (`/proc`, `/sys`, `/etc`, `/home`, `/home/runner`
  and the audit directory's state area), the mode, the absence of `WebFetch`, `WebSearch` and both bypass
  forms. Dropping or widening any of them fails `make test`.
- [ ] Before the model step, a **model-free hardening step** stops the Docker service and removes the
  runner user's sudo rule (the model job runs no container). The `test/architecture` check pins that step
  and its place before the preflight.
- [ ] On **every run**, before the model starts, a **sandbox preflight** runs six probes through the same
  launcher, settings and wrapper the model uses, and fails the step unless **all six fail**: (1) a write
  outside the work area, (2) a TCP connect, (3) a connect to `/var/run/docker.sock`, (4) `sudo -n true`,
  (5) a read of a canary variable from `/proc/<launcher pid>/environ`, where the launcher sets a dummy
  canary for this purpose (never the token), and (6) a write to the `$GITHUB_ENV` file's path. Probes 5
  and 6 also run as `Read`, `Grep` and `Glob` tool calls where the tool applies. If a probe succeeds, or
  bubblewrap cannot start (Ubuntu's AppArmor restriction of unprivileged user namespaces), the step fails
  and its summary names the runbook step. Whatever the model-free step changes to let bubblewrap start,
  the ADR records. This makes the check that **no tool call** in the model step (`Read`, `Grep`, `Glob` or
  `Bash`) can read the launcher's environment, and so the OAuth token, part of every run, so a Claude Code
  version that Renovate bumps is checked on its first run. A test runs the preflight's logic against a
  fake that lets each probe in turn succeed and shows the step fails.

Pins, checks and logs:

- [ ] Every action is pinned by full commit SHA with a version comment. Every tool binary (gitleaks,
  osv-scanner, trivy, zizmor, OpenSSF scorecard, cosign, the encryption tool, the Claude Code CLI, and
  what the sandbox needs if the runner lacks it) is pinned by version **and** checksum, with a
  `# renovate:` comment where Renovate can track it (CLAUDE.md rule 7). actionlint and govulncheck reuse
  the pins `tools/go.mod` and `security.yml` already have. No tool is installed from a mutable tag,
  `@latest` or a `curl | sh`.
- [ ] A check in `test/architecture` (like `check.ReleasePleaseWorkflow`) pins the workflow: its
  triggers, a `workflow_dispatch` with no `inputs:`, the guard condition above, **textually identical**
  in every job's `if:` and in the workflow-level `concurrency` group expression (which falls through to
  a per-run group) with `cancel-in-progress: false`, the three jobs' permissions and environment,
  `needs: checks` and `needs: model`, no secret in the checks job other than `github.token` in
  step-level `env:`, each secret in exactly one job and step (the OAuth token in the model step's
  step-level `env:`, the model key's private half in the model job's decrypt step, the publish key's
  private half, the App key and the fingerprint key in the publish job), **no `secrets[`,
  `toJSON(secrets)` or `secrets: inherit`** anywhere in the workflow (each would send every environment
  secret to that runner), the App token with exactly its `permission-*` inputs, the two artifacts by name
  and their retention, no job outputs, no `actions/cache` of tool output, no `set -x`, no check or
  container step in the model job, the hardening step before the sandbox preflight,
  `GOFLAGS=-mod=readonly GOPROXY=off`, the container flags, the commit-pinned actions, the time-outs, and
  Claude Code's launch (the launcher, its settings file, setting sources, strict MCP configuration, the
  system-prompt or prompt file, no bypass). Changing any of them fails `make check`.
- [ ] **The Actions cache.** Anything in the checks job runs as the runner user and could read the
  Actions runtime token and save cache entries scoped to `main`, which other workflows on `main` restore.
  The ADR records this residual risk and the rule that no release-relevant workflow restores an Actions
  cache. The same `test/architecture` check pins `cache: false` (or `skip-cache: true`) on every
  `actions/setup-go` step in `release.yml` (both set `cache: false` today) and on any golangci-lint step
  added there, so a change that turns a cache on there fails `make check`.
- [ ] Nothing private reaches a log or a readable artifact. Raw tool output, crash inputs, PoCs, Claude
  Code's transcript and its final output stay under the job's temporary or audit directory, never
  printed, never uploaded but encrypted. Claude Code's output is redirected to a file, not streamed to the
  log. The checks and model jobs' summaries and logs show only the audited commit and tag, tool versions,
  and that they finished: no count, per-check outcome, failure or crash count. The publish job computes
  the **public total** and whether private findings exist from the decrypted result and prints only
  those, never the number of private findings (Decision 2), and never echoes an API response, advisory
  URLs and IDs included.

The publish script:

- [ ] The publish script has no model in it and is tested against a fake `gh` with canned API responses
  (as CI's `Release please dry run` job tests the release scripts). It fails closed (publishes nothing
  public) on a result that does not decrypt, does not match the schema, has a field over its length cap,
  has an unknown field, or exceeds the finding limit the ADR sets. Every model-written text is
  Markdown-escaped before it is published: no raw HTML, no images, no links to other hosts.
- [ ] It **derives** public or private itself, from each finding's severity, confirmation and category
  alone (Report and triage, the table). The model's own "public" flag can only make a finding more
  private, never less. A test covers each row of the table.
- [ ] It derives each finding's status (new, open or fixed, and whether it regressed) and applies the
  false-positive match itself, from the fingerprints it computes, and only to rows of a merged report:
  a dismissal proposed in a run only adds a row, and the finding stays in that run's findings table. A
  test covers an exact match on a low public finding (dropped), a match on a medium finding and on a
  private-category finding (each reported), and a near-match (reported). The result schema accepts
  exactly the ten categories of ADR 0040, and any other value is unknown and therefore private.
- [ ] Before publishing, it scans every text it is about to send (the report, any issue title and body,
  every advisory's text) with gitleaks plus patterns for Claude OAuth tokens and GitHub App private keys,
  and it **decodes every base64 or hex run and matches the decoded bytes against the same patterns**
  (the encoded-token check). One rule, stated the same way in Decision 2:
  - **Public text** (the report and issues) also gets the **encoded-run refusal**: a run is a token with
    no `/`, `.` or other path separator, at least L characters long, with an entropy of at least N bits
    per character; the ADR sets L (longer than a fingerprint) and N. The refusal applies only to
    **model-written fields, after Markdown escaping**. Values the script inserts itself are exempt: the
    `main` SHA, the release tag, the run URL, the dates, the fingerprints, and the digests and SHAs it
    read itself from GitHub or the release.
  - **Advisory text** gets the token and key patterns and the encoded-token check, but **not** the
    encoded-run refusal: an advisory legitimately holds PoCs and crash inputs, which are often base64.

  On a hit it publishes nothing, and the summary says only "publish refused: see runbook". Tests cover a
  raw token, an encoded token in public text (refused), a token encoded in advisory text (refused), a
  base64 crash input in advisory text (published), and a public finding with a long repository path
  (`internal/adapter/source/inventoryfile/decode.go:42`) in a report that holds the `main` SHA and an
  image digest (published).
- [ ] The PR's title and body, its branch `security-audit/YYYY-MM-DD-<run_id>` and its commit message are
  fixed templates filled only with the run's UTC date, the run ID, the `main` SHA, the release tag, the
  public count, the private-findings flag and the run's URL, built by the script from
  `GITHUB_SERVER_URL`, `GITHUB_REPOSITORY` and `GITHUB_RUN_ID`, never with model text. The branch holds
  **one commit** with the report file, made through the Git data endpoints (blob, tree, commit, ref).
- [ ] It opens the report PR only after checking that the branch's diff is exactly
  `docs/security/audits/YYYY-MM-DD-<run_id>.md`, with the branch's date and run ID, refusing anything
  else or an existing branch of that name. No index edit: `docs/security/README.md` links the directory
  `audits/`, whose listing is the index (issue #280, ADR 0040), so two report PRs from the same `main`
  never conflict. Because the names carry the run ID, a second run on one UTC day, or a run started by
  hand on the day of a scheduled one, adds its own report. A report it renders from a fixture result
  passes `test/docs`' title and link checks.
- [ ] Each finding carries a **fingerprint** the script computes from rule, file and sink, written as
  `fp-` and 12 lowercase hex characters in each issue and advisory it files, and in the report's findings
  table for public findings, so later runs can read it back. A **public** fingerprint is a plain hash: the
  finding is published anyway. A **private** fingerprint is a keyed hash (HMAC) with the
  `AUDIT_FINGERPRINT_KEY` secret of the `security-audit` environment, referenced only in the publish job,
  so nobody can predict it from the committed script.
- [ ] **Only what the App wrote is matched.** It matches, and updates with `PATCH`, only advisories the
  audit App authored, in `draft` or `closed` state, with no credited reporter or collaborator the App did
  not add; it never touches an advisory in `triage` state (a private vulnerability report) or any other.
  It matches issues only if `shift2eu-audit[bot]` authored them. An outsider therefore cannot steer a
  private finding into an advisory they can read, nor stop a public finding's issue with a planted
  fingerprint. The fake-`gh` test covers a `triage`-state advisory and an advisory with a foreign
  collaborator, each holding a matching fingerprint (neither is touched; a new draft is created), and an
  outsider's issue holding a public finding's fingerprint (ignored; the bot's issue is opened).
- [ ] It tracks private findings by fingerprint: it lists the App's advisories, updates a draft whose
  fingerprint recurs, creates a draft for a new one, and marks a draft whose finding this run did not
  reproduce "not reproduced". An open App-authored draft with the fingerprint takes precedence over any
  closed advisory with it: the draft is updated and the closed ones are ignored. Closed advisories matter
  only when no draft has the fingerprint: a recurrence of a finding whose advisory the maintainer closed
  is **"regressed"**, filed as a new draft, only if an earlier run marked that advisory "not reproduced"
  before it was closed (fixed, then back). Otherwise the closed advisory counts as dismissed (a false
  positive or an accepted risk): no new draft. If several closed App-authored advisories carry one
  fingerprint, the most recently closed one is that finding's history for the "regressed" rule. A test
  covers each case, a draft and a closed advisory with one fingerprint included. If it finds two
  App-authored drafts with one fingerprint (two runs that raced despite the concurrency group, or a bug),
  it fails closed and publishes nothing, and its summary names runbook step 11; a fake-`gh` test covers
  that case. These states live only in the advisories: the report says only "private findings: handled
  privately" when this run reproduced any private finding, or "private findings: none", never how many
  are new, still open, not reproduced, regressed or dismissed (Decision 2). A reproduced private finding
  sets the flag even when its advisory was closed as dismissed: the flag then stays steadier from run to
  run, so it signals less.
- [ ] It calls **only a fixed set of endpoints**: the GitHub reads it needs; the Git data endpoints that
  create one blob per file, one tree, one commit and the `security-audit/*` ref; creating the PR;
  searching and creating issues; listing, creating and updating (`PATCH`) repository security
  advisories. Every advisory `state` it sends is hard-coded `draft`, and it never calls the endpoints that
  publish an advisory, request a CVE, approve or merge a PR, or touch a tag, release or other branch. The
  fake `gh` fails the test on any other endpoint or state.
- [ ] It opens a `security` issue only for a public finding of a report **already merged on `main`**,
  never for the report it is opening (Decision 7), and only when no issue authored by
  `shift2eu-audit[bot]`, **open or closed**, carries that finding's fingerprint. A merged report counts as
  processed once every public finding in it has such an issue, so the work is idempotent and a finding
  whose issue the maintainer closed is not filed again. A test covers an open, a closed, and an unmatched
  fingerprint, and the report being opened.
- [ ] If GitHub refuses advisory creation to the App, the run fails closed: it publishes only the public
  count, the private-findings flag and the coverage, with the report saying "private findings: not filed"
  instead of "handled privately" (still one bit: the flag is set either way), and the summary says
  "private findings not filed: see runbook". The private findings are dropped, never written anywhere
  else (Decision 2).
- [ ] The report's per-check coverage is derived by the publish script, not the model: "findings" only
  when the check produced a **public** finding, "run" otherwise (including when its only findings are
  private), or "not run (why)", where the why is one of a fixed vocabulary in the publish script (for
  example "needs admin read", "no `Fuzz*` targets", "timed out"), never the model's text. A fixture test
  renders a result with one private finding from the CLI's hostile inventories (row 13) and shows that row
  exactly as a check with no findings ("run") (Decision 2).
- [ ] Values from the run reach the shell only through `env:` and files, never through a `${{ }}`
  expression.

The required path check:

- [x] A CI job fails any audit PR (authored by `shift2eu-audit[bot]`, or whose event sender is the App, or
  whose head branch is under `security-audit/`) that is not based on `main` or that touches anything
  outside `docs/security/`, a path with a component starting with a dot, or an entry there that is not a
  regular non-executable file (mode 100644), and passes trivially for other PRs. It is required by a
  **new ruleset on `main`** (runbook step 5), not by `Protect main`, which stays unchanged.

Time and failures:

- [ ] Each step has a `timeout-minutes`, and each of the three jobs one below GitHub's 6-hour job limit.
  The ADR gives the budget (for example checks 180 minutes, with 10 per fuzz target and 60 for dynamic
  testing; model 120; publish 30). A check that times out is recorded in the report's limits, and the run
  still publishes what it has.
- [ ] A failed run is visible: a run that fails (an expired or revoked subscription token, the
  subscription's usage limit, a tool download whose checksum differs, a failed sandbox preflight, a
  managed-settings file, an unexpected artifact, a refused publish) fails the workflow, which GitHub
  notifies the maintainer of. Its step summary names the cause's kind and the runbook step that fixes it,
  without a secret or finding detail. Recovery is a **new `workflow_dispatch` run**, and the workflow
  enforces it: the guard condition's `github.run_attempt == 1` skips every job of a re-run of any run,
  so the artifact check never sees an earlier attempt's artifact.
- [ ] The maintainer's runbook (below) is in `docs/dev-environment.md`, with a check after each step. It
  names no cadence and links to `docs/security/README.md` where it refers to it.

### The first run and the schedule (milestone 3)

- [ ] One `workflow_dispatch` run on `main` against the latest release produces, and nothing else:
  - a docs-only PR opened by `shift2eu-audit[bot]` that adds `docs/security/audits/<date>-<run_id>.md`
    and nothing else (the directory link indexes it, issue #280);
  - the draft advisories for its private findings (or none).

  No other branch, tag, release, package, comment, setting or new label changes, and no issue is opened
  (no earlier report is merged yet). The PR body records the run's URL.
- [ ] The report states the `main` SHA, the release tag, the tool versions and the model. It lists every
  check with "run", "findings" or "not run (why)", as the publish script derives them (milestone 2).
- [ ] After the maintainer has reviewed and merged that report, the `schedule` trigger is enabled: monthly,
  with the cron `17 4 3 * *` (04:17 UTC on the 3rd of each month; GitHub evaluates cron in UTC), which
  the ADR records. The cron is the only place the schedule is set, and no name carries the cadence: no
  file, job, step, skill, secret, label or branch name. A later cadence change renames nothing and edits
  the **values derived from the cadence**, all listed here: the cron; the 38-day gap threshold, in the
  staleness check and in the publish script's gap flag; Decision 7's delay; the ADR; and the cadence text
  in `docs/security/README.md`, the one user-facing text that states it (`SECURITY.md`, the skill, the
  report template and the runbook link to it, milestones 1 and 2). The second scheduled run's report
  marks each earlier public finding as fixed, still open or regressed, says whether private findings
  exist, and its publish job opens the `security` issues for the first report's public findings.
- [ ] GitHub silently disables a public repository's scheduled workflows after 60 days without repository
  activity, and a skipped schedule fails nothing. So a **staleness check**, in the `release-tag` skill's
  checks and the `dispatch` skill's flow, names runbook step 9 and fails when:
  - the workflow's state (`gh workflow view scheduled-security-audit.yml`, or the workflows API) is not
    `active` (for example `disabled_inactivity` or `disabled_manually`), so a disable is reported at
    once;
  - the last **counted** run is more than **38 days** old (the longest month, 31 days, plus 7 days of
    slack for a late or delayed run). A counted run is a `scheduled-security-audit.yml` run with
    `event: schedule` whose conclusion is not `cancelled`; a failed run still counts, because GitHub's
    failed-workflow notification reports it, and a `workflow_dispatch` run never counts;
  - there is no counted run yet and the commit on `main` that added the `schedule` trigger is more than
    38 days old (while it is at most 38 days old, the check passes). That commit is the oldest one, on
    `main`, of
    `git log --format=%cI -S'schedule:' -- .github/workflows/scheduled-security-audit.yml`. The check
    fails closed when the workflow has a `schedule` trigger but no such commit is found (a shallow clone,
    a renamed file).

  A test covers each case, the no-counted-run case on both sides of 38 days included. Each report flags a
  gap when the previous report is more than 38 days older, computed by the publish script from the date
  part of the names in `docs/security/audits/` that match `YYYY-MM-DD-<run_id>.md` only (`TEMPLATE.md`
  and any other file are ignored).
- [ ] The gap threshold is **one pinned constant** of the publish script. A check in `test/architecture`
  fails `make check` unless it is at least the longest gap between two consecutive dates the workflow's
  cron names plus 7 days, and unless the staleness check in the `release-tag` and `dispatch` skills
  states the same number of days. The check supports the cron forms `M H D * *` (D a single day of the
  month from 1 to 28, so every month has it) and `M H * * W` (W a single day number from 0, Sunday, to
  6), and fails `make check` on any other form or value.

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
| 7 | Release download; `checksums.txt`; `gh attestation verify` on every file; `cosign verify` of the image and its provenance and SBOM attestations (Sigstore, GHCR); GitHub's latest release and the image's `latest` tag agree | checks, workflow step | yes |
| 8 | Image pulls from GHCR | checks, workflow step | yes |
| 9 | `govulncheck -mode=binary` on every release binary (read, not run) | checks, workflow step | yes: vulnerability database |
| 10 | trivy on the image and its SBOM, from the downloaded database | checks, workflow step | no |
| 11 | The image runs as non-root, with a read-only root filesystem and no shell where none is expected | checks, containers | no |
| 12 | Dropped: the server is no longer released (ADR 0037) | none | no |
| 13 | The CLI on hostile inventory files | checks, container, `--network none` | no |
| 14 | Time-boxed `go test -fuzz` of the committed `Fuzz*` targets (none yet: Decision 5) | checks, container, `--network none`, `GOPROXY=off` | no |
| 15 | Supply-chain reads: action pins and workflow `permissions`, rulesets and environments read the way the `release-tag` skill's step 2 reads them, Renovate's state, dependencies changed since the last report | checks, workflow step | yes: GitHub reads |
| 16 | Encrypt the raw results to the model key, upload `checks-result` | checks, workflow step | yes: artifact upload |
| 17 | Check the run's artifacts, download `checks-result`, decrypt into the audit directory, copy the checkout; stop Docker and remove the runner user's sudo rule; the sandbox preflight's six probes | model, workflow steps | yes: tools, artifact download |
| 18 | The `security-audit` checklist across the whole tree, a review of what the agents may run (`.claude/`, `.pi/`, `.dot-agent-deck.toml`, `dev/`), triage of 3 to 15 with a PoC or precise trace for each finding, the status of the last report's public findings, the structured result | model, **Claude Code**, sandboxed | Bash: none (enforced); Claude Code: model API only by configuration, not enforced (Decision 6) |
| 19 | Encrypt the result to the publish key, upload `model-result` | model, workflow step | yes: artifact upload |
| 20 | Check the run's artifacts, download and decrypt `model-result`, validate, classify, scan, publish | publish, its script | yes: GitHub writes through the App |

Scorecard's checks that need admin read with a read-only token (Branch-Protection at least; the ADR lists
the rest after the first run) are reported "not run (needs admin read)". The rulesets they would cover
are read in row 15; what a read-only token cannot see there is reported "not run" too. Crash inputs and
PoCs go only into the encrypted results or an advisory, never into `testdata/`.

## Report and triage

- **Report** (`docs/security/audits/YYYY-MM-DD-<run_id>.md`, from the template): summary; scope (commit,
  tag, tool versions, model); findings table (fingerprint, severity critical/high/medium/low/informational,
  category, status new, open or fixed, `path:line` or artifact, scenario, recommended fix); whether
  private findings exist ("private findings: handled privately", "not filed" when advisory creation is
  refused, or "none", never a count; Decision 2); coverage, each check "run", "findings" (a public
  finding only) or "not run (why)", as the publish script derives it; changes since the last audit, and
  any gap in the schedule; the false positives (public only, written and carried forward by the
  publish script); the run's limits.
- **Disclosure split.** The PR is public **the moment it is opened**, before the maintainer reviews it,
  so the split is decided in the run by the publish script, from severity, confirmation and category
  alone:

  | Finding | Where it goes |
  |---|---|
  | Critical, high or medium | A draft security advisory (Decision 2). The report says only "private findings: handled privately". |
  | Unconfirmed, suspected medium or above | A draft advisory, marked unconfirmed. |
  | Category injection, authentication bypass, credential exposure or remote code execution, **whatever the severity** | A draft advisory. |
  | Missing or unknown severity or category | A draft advisory. |
  | A secret found in the git history (gitleaks) | Always private: a draft advisory naming the commit and the rule only, never the value, marked **"unconfirmed: maintainer to check"** (the model job has no full-history checkout and gitleaks redacts the value, so the run cannot tell a real secret from a fixture); it is **never tried against the service**. The action is a check, and if real, rotation by the maintainer. |
  | Confirmed low or informational (hardening included) | The report; after the report is merged, one `security` issue each unless an issue, open or closed, carries its fingerprint (Decision 7). |
  | Unconfirmed, suspected low or informational | The report's "needs investigation" list, no issue. |

  The model may mark any finding private; it cannot make a finding public that the table makes private.
- **Residual risk.** The script decides, but the model fills the severity, confirmation and category the
  script reads. A model that mislabels a finding, or is led by hostile input to call a critical one "low,
  hardening", makes it public the moment the PR opens. The category floor and the private default for
  anything unknown narrow this; they do not close it. The PRD and the ADR state it; the maintainer's
  review of each report is the last check, after publication.
- **Signal over noise.** Every finding is confirmed by a PoC or a precise trace, or listed as
  unconfirmed. The publish script drops a finding only when its computed fingerprint matches a row of
  the last report's false positives section exactly, and never one of medium or higher severity or in a
  private category.

## The subscription token

- The model step authenticates with a **Claude Code OAuth token** that the maintainer creates with
  `claude setup-token` on their own machine, under the subscription the agents already use. It is stored
  as the secret `CLAUDE_CODE_OAUTH_TOKEN` of the `security-audit` environment **only**, and only the
  model step's step-level `env:` references it. No API key is used.
- A run counts against the **maintainer's subscription usage limits**, which are shared with the agents
  in the VM and with the AI reviewer of PRD #161 milestone 4 (which runs Claude Code with the same kind of
  subscription token). An audit run can therefore slow the agents or the reviewer, and they can exhaust
  the limit an audit run needs. The scheduled time (early on a UTC morning, on a day the cron names) is chosen
  for when the agents are usually idle; the maintainer picks a quiet time for a run started by hand. A run
  stopped by the limit fails visibly and is recovered by a new `workflow_dispatch` run; a re-run is
  skipped by the workflow's guard condition.
- **Expiry and rotation.** The token is long-lived but expires (`claude setup-token` shows the term). The
  runbook records the date it was created and its expiry. An expired or revoked token makes the model
  step fail with an authentication error and the run fail, with the step summary pointing at the runbook.
  To rotate: run `claude setup-token` again, replace the environment secret, revoke the old token where
  the Claude account allows it, and start one `workflow_dispatch` run to check. Rotate at once if the
  token may have leaked.

## Milestones

1. [x] **Skill, ADR, disclosure documents and the docs index.** The `security-audit-run` skill, the
   ADR (what is known before PRD #161 milestone 4), `SECURITY.md`, `docs/security/README.md`,
   `docs/security/audits/TEMPLATE.md`, the docs index, and `TestIndexListsEveryPage` accepting a
   sub-index, with its test. No workflow yet. Depends on nothing; can merge before PRD #161 milestone 4.
2. [ ] **The workflow, its three jobs, the pinned tools and their checks.** `scheduled-security-audit.yml`
   with `workflow_dispatch` only (no `schedule` yet), the Claude Code
   launcher, settings, hardening step, sandbox preflight (all six probes, on every run) and wrapper and
   their tests, the two encrypted hand-offs, the publish script and its fake-`gh` tests (keyed private
   fingerprints, App-authored matching only, the pinned gap threshold), the required path check, the
   `test/architecture` check of the workflow and of `cache: false` on `release.yml`'s setup-go steps, the
   `release-tag` secrets check, the ADR's amendment, the runbook. **Depends on 1, on PRD #161 milestone 4
   having merged** (`feat/161-m4-ai-reviewer`; milestone 3 is `chore/161-m3-remove-maintainer-token`),
   **and on runbook step 3 confirming that the App can create and update a draft advisory.** From
   milestone 4 it reuses the `main`-only environment pattern and in-workflow App-token minting, and its
   way of running the Claude Code CLI with the subscription token where that way meets this PRD's
   requirements (Decision 3). Runbook steps 1 to 6 come before its merge, step 7 is the merge, step 8
   comes after.
3. [ ] **First run, then the schedule.** One manual run against the latest release, its report PR merged
   by the maintainer, then a PR that adds the `schedule` trigger, the staleness check and the check of the
   gap threshold against the cron (and moves this PRD to `prds/done/`). Depends on 2 and runbook step 8.

Security-sensitive throughout: **every milestone's PR runs the `security-audit` skill**, and the
workflow, settings, launcher, wrapper and publish script get the AI reviewer of PRD #161 milestone 4 like
any other change.

## Maintainer runbook

An agent cannot do these steps. Each one is safe to stop at; the workflow is rolled back by disabling it.

1. **Create the audit App** `shift2eu-audit`. Install it on this repository only, with the permissions
   settled by Decision 1, no webhook, and no other repository. Generate its private key. Check: the
   installation lists this repository alone and exactly those permissions.
2. **Create the `security-audit` environment**: deployment branches `main` only, no required reviewers
   (a scheduled run cannot wait for one). Make **two key pairs** with the encryption tool on the
   maintainer's machine, the model key and the publish key, and a random **fingerprint key** of at least
   32 bytes (for example `openssl rand -hex 32`). Add the variables (the App's client ID, both public
   halves) and the secrets (the App's private key, both private halves, `AUDIT_FINGERPRINT_KEY`, and the
   Claude Code OAuth token from `claude setup-token`). Record the token's creation date and expiry. Delete
   the two private key files from the machine once stored, and keep no copy of the fingerprint key
   (replacing it only makes the next run file existing private findings as new drafts). Check:
   `gh api repos/<owner>/<repo>/environments/security-audit/deployment-branch-policies` lists exactly
   `main`, and the repository-level secrets and variables list none of them.
3. **Create the `security` label** (it does not exist yet), and **confirm advisory creation** with an
   installation token minted by hand. Recommended: the documented local command in the runbook, which
   signs a short-lived JWT with the App's private key file (`openssl`) and exchanges it with
   `POST /app/installations/<id>/access_tokens`; a one-off workflow would itself have to be merged to
   `main` to reach the environment. With that token, `POST /repos/<owner>/<repo>/security-advisories`
   creates a draft advisory, a `PATCH` with `state: draft` updates it, then close it. Then delete the App's
   private key file from the machine (it lives only in the environment secret); the installation token
   expires within an hour. Check: the advisory existed as a draft, was updated, and is closed; the key
   file is gone. Milestone 2 is blocked until this check passes (Decision 2).
4. **Create the `security-audit/**` branch ruleset**: only the audit App, as its one bypass actor, may
   create, update or delete those branches; admins get no implicit bypass. An admin who must write there
   (trial PR 2 in step 5, a failed run's branch in step 11) first adds the **Repository admin** role
   (`RepositoryRole`, actor_id 5, mode `always`) as a temporary bypass actor, removes it afterwards, and
   re-runs this step's bypass check (PASS: the audit App only, mode `always`). Check: the ruleset is
   listed, and its bypass check below passes (the list endpoint returns no bypass actors, so read the
   ruleset itself):

   ```bash
   # The id of the Audit report branches ruleset, the <audit branches id>
   gh api repos/<owner>/<repo>/rulesets --jq '.[] | [.id, .name, .target, .enforcement] | @tsv'
   # PASS: a row Audit report branches  branch  active; FAIL: no such row, or not active

   # Bypass check of this step: the audit App only, mode always
   gh api repos/<owner>/<repo>/rulesets/<audit branches id> --jq '[.bypass_actors[] | [.actor_type, .actor_id, .bypass_mode]] | tojson'
   # PASS: [["Integration",5875190,"always"]]
   # FAIL: any other actor, or another mode (in pull_request mode the publish job
   # could not push its report branch directly)
   ```

5. **Create the audit-paths ruleset on `main`**: it requires the CI check that fails an audit PR touching
   anything outside `docs/security/`, `Audit report paths` (the job of the `Audit paths` workflow,
   `.github/workflows/audit-paths.yml`), with source **GitHub Actions** (`integration_id` 15368), so a
   status of that name from any other integration does not count, and with no bypass actor. Create it with
   enforcement **disabled** first, and make it active only after the trial PRs below. Before you make the
   audit-paths ruleset active (ideally before step 5 starts), create the **branch ruleset for every other
   branch** (active at once): target `~ALL` excluding the default branch (`~DEFAULT_BRANCH`) and
   `security-audit/**`, rules `creation`, `update` and `deletion`, and exactly these bypass actors, each with mode **Always allow** (`always`): the maintainer
   as the **Repository admin** role (`RepositoryRole`, actor_id 5; on a user-owned repository a user
   cannot be a bypass actor, as PRD #297's R9 reads it), and the Apps (`Integration`) `shift2eu-agents`,
   Renovate (`renovate`) and the release App `shift2eu-release`, **not** the audit App
   `shift2eu-audit`. The mode must be `always`: `pull_request` mode would allow those actors only through a
   pull request, and would reject the agents' direct pushes and branch deletions, Renovate's branches and
   release-please's release branch. The App's
   Contents write lets it push to any branch; this ruleset leaves it able to write only `security-audit/**`
   branches. The check's sender key fails only the run of the App's own event: a later event by another
   sender (a push on top, an edit, a reopen) passes the check again with the App's commit still in the PR,
   so without this ruleset the sender key is only a one-shot signal (ADR 0040). The default branch stays
   out of its target: `Protect main` already guards `main`, and with `main` included the `update` rule
   blocked every merge into it (2026-11-22, PRs #460 and #461). If the maintainer adopts
   Decision 1's optional tag ruleset, create the `**` tag ruleset too, with only the release App as bypass
   actor. Check:

   ```bash
   # The ids of the four Apps, for the bypass check below
   for s in shift2eu-agents renovate shift2eu-release shift2eu-audit; do echo "$s $(gh api /apps/$s --jq .id)"; done
   # PASS: shift2eu-agents 5798878, renovate 2740, shift2eu-release 5729776, shift2eu-audit 5875190

   gh api repos/<owner>/<repo>/rulesets --jq '.[] | [.id, .name, .target, .enforcement] | @tsv'
   # the branch ruleset is the <branch id>, the audit-paths ruleset the <id>,
   # the Audit report branches ruleset of step 4 the <audit branches id>

   # Branch ruleset: conditions, rules, bypass actors
   gh api repos/<owner>/<repo>/rulesets/<branch id> --jq '(.conditions.ref_name.include | tojson), (.conditions.ref_name.exclude | sort | tojson)'
   # PASS: ["~ALL"], then ["refs/heads/security-audit/**","~DEFAULT_BRANCH"] (the exclude list sorted,
   # so the order the API returns it in does not matter)
   # FAIL: anything else, main included in particular (an exclude without "~DEFAULT_BRANCH")
   gh api repos/<owner>/<repo>/rulesets/<branch id> --jq '([.rules[].type] | tojson), .enforcement'
   # PASS: holds "creation", "update", "deletion" (any order), then active; FAIL: one missing, or not active
   gh api repos/<owner>/<repo>/rulesets/<branch id> --jq '[.bypass_actors[] | [.actor_type, .actor_id, .bypass_mode]] | tojson'
   # PASS: exactly these four, in any order:
   #   ["RepositoryRole",5,"always"] ["Integration",5798878,"always"]
   #   ["Integration",2740,"always"] ["Integration",5729776,"always"]
   # FAIL: the audit App id 5875190 listed, any other or missing actor, or a mode other than always

   # Audit-paths ruleset: the required check is pinned to GitHub Actions, with no bypass actor
   gh api repos/<owner>/<repo>/rulesets/<id> \
     --jq '.rules[] | select(.type == "required_status_checks") | .parameters.required_status_checks | tojson'
   # PASS: [{"context":"Audit report paths","integration_id":15368}]; FAIL: no integration_id, or another one
   gh api repos/<owner>/<repo>/rulesets/<id> --jq '.bypass_actors | tojson'
   # PASS: []

   # Trial PR 1 (any branch into main, docs only), before the ruleset is made active: the
   # pull_request_target run attaches to the PR head commit, where the ruleset looks for it
   gh pr view <n> --json headRefOid --jq .headRefOid
   # the <head sha>
   gh api repos/<owner>/<repo>/commits/<head sha>/check-runs \
     --jq '.check_runs[] | select(.name == "Audit report paths") | [.name, .app.slug, .app.id, .conclusion] | @tsv'
   # PASS: Audit report paths  github-actions  15368  success; FAIL: no row, or another app

   # Trial PR 2 runs on the admin machine, in the maintainer own clone, as the maintainer:
   # the temporary bypass is the admin role, and a push with the agents App credentials
   # is refused for another reason than the FAIL line below names
   gh auth status
   # PASS: the maintainer account is the active one; FAIL: the agents App or another account

   # The Audit report branches ruleset of step 4 lets only the audit App push or
   # delete security-audit/** (admins get no implicit bypass), so add the admin role to it for
   # this trial only. The ~ALL branch ruleset excludes that pattern (and the default branch,
   # which Protect main guards) and plays no part here.
   # In Settings, Rules, Rulesets, Audit report branches: add the bypass actor Repository admin,
   # mode Always allow, and save. Then re-run step 4 bypass check:
   gh api repos/<owner>/<repo>/rulesets/<audit branches id> --jq '[.bypass_actors[] | [.actor_type, .actor_id, .bypass_mode]] | tojson'
   # PASS: exactly ["Integration",5875190,"always"] and ["RepositoryRole",5,"always"], in any order
   # FAIL: no RepositoryRole entry, any other actor, or a mode other than always

   # Push a branch security-audit/trial-paths that changes README.md, and open its PR into main
   git switch -c security-audit/trial-paths origin/main
   # edit README.md, then:
   git commit -am "test: trial PR 2 for the audit paths check"
   git push origin security-audit/trial-paths
   gh pr create --base main --head security-audit/trial-paths --title "Trial PR 2" --body "Audit paths trial, to close"
   # PASS: the push and the PR succeed, and the PR number is the <n>
   # FAIL: the push is rejected (the bypass actor is missing)

   # The fence fires
   gh pr view <n> --json headRefOid --jq .headRefOid
   # the <head sha>
   gh api repos/<owner>/<repo>/commits/<head sha>/check-runs \
     --jq '.check_runs[] | select(.name == "Audit report paths") | [.name, .app.slug, .app.id, .conclusion] | @tsv'
   # PASS: Audit report paths  github-actions  15368  failure; FAIL: success, no row, or another app

   # Close trial PR 2 and delete its branch
   gh pr close <n> --delete-branch
   git ls-remote origin refs/heads/security-audit/trial-paths
   # PASS: no output; FAIL: the branch still exists

   # Remove the Repository admin bypass actor from Audit report branches again, and save.
   # Then re-run step 4 bypass check:
   gh api repos/<owner>/<repo>/rulesets/<audit branches id> --jq '[.bypass_actors[] | [.actor_type, .actor_id, .bypass_mode]] | tojson'
   # PASS: [["Integration",5875190,"always"]], the audit App only
   # FAIL: any other actor, the admin role included, or another mode

   # Then make the audit-paths ruleset active (enforcement: active), and check Protect main is unchanged
   gh api repos/<owner>/<repo>/rulesets/<id> --jq .enforcement
   # PASS: active
   ```
6. **Check the other rulesets and the Apps**: the App is in no bypass list of `Protect main`, the
   audit-paths ruleset or the tag rulesets, and `Protect main` still requires the code owner's approval,
   so an approval by the App does not count (as PRD #161 milestone 4 checks). No App installed on the
   repository other than the release flow's holds Actions write (not the agents' App, the AI reviewer's
   or Renovate), because Actions write starts a `workflow_dispatch` run, and a run spends the
   subscription limit shared with the agents and the AI reviewer and publishes a report PR and
   advisories. The workflow's guard condition already skips a run that anyone but the maintainer starts,
   and a re-run of any run, whoever starts it; this check keeps a second guard. If such an App does hold
   Actions write, remove it from that App before step 7. Check: the same `gh api` reads show exactly
   that, and the repository's installed GitHub Apps (Settings, GitHub Apps) show Actions write for the
   release flow's App alone. Repeat the App check whenever an App is installed or its permissions change.
7. **Merge milestone 2.**
8. **Start the first run** (`workflow_dispatch` on `main`). Read its report PR and advisories. Check that
   the logs and step summaries show only the public total and the private-findings flag in the publish
   job and "finished" in the others, that the run's two artifacts are ciphertext, and that the sandbox
   preflight's step reports all six probes refused (it runs on every run; this step only reads its
   result). Then merge the report, then milestone 3's schedule.
9. **Keep the schedule alive.** If the staleness check fails, look at the workflow's state and runs; if
   GitHub disabled the schedule after 60 days without repository activity, re-enable it
   (`gh workflow enable scheduled-security-audit.yml`) and start one `workflow_dispatch` run. Check: the
   workflow's state is `active`, and the next scheduled run appears at the next date the cron names (the
   ADR records it).
   To **run an audit outside the schedule**, start a `workflow_dispatch` run on `main` at any time
   (`gh workflow run scheduled-security-audit.yml --ref main`, with no inputs); only the maintainer can,
   since the jobs are skipped for any other triggering actor. A run started while another is in progress
   waits for it (the concurrency group), and each run adds its own report on its own branch,
   `security-audit/<date>-<run_id>`. Check: its report PR opens like a scheduled run's. Two report PRs
   open at once touch only their own report files, so they merge in either order (issue #280). Never use
   Update branch or push to the App's branch: a maintainer commit there cannot be approved
   (`require_last_push_approval`) and breaks the audit PR's docs-only check.
10. **Renew the Claude token** before it expires (The subscription token, above), **rotate the App's key,
    the model key or the publish key** if any may have leaked, and **rotate any secret** a private
    finding says is in the git history, once you have checked that it is real. If the sandbox
    preflight fails, read the ADR's note on the runner's user-namespace restriction before changing
    anything.
11. **Recover a failed run.** Start a **new `workflow_dispatch` run** on `main`. The workflow enforces
    this: its guard condition skips every job of a re-run of any run ("re-run failed jobs" included), so
    the artifact check never sees an earlier attempt's artifact. The new run has its own run ID, so its
    report branch never meets the failed run's. If the publish job created
    `security-audit/<date>-<run_id>` but failed before opening the PR, that branch blocks nothing; delete
    it to keep things tidy, through step 4's temporary bypass (only the audit App may delete it otherwise):

    ```bash
    # This block runs on the admin machine, in the maintainer own clone, as the maintainer:
    # the temporary bypass is the admin role, and a DELETE with the agents App credentials
    # is refused for another reason than the FAIL line below names
    gh auth status
    # PASS: the maintainer account is the active one; FAIL: the agents App or another account

    # In Settings, Rules, Rulesets, Audit report branches: add the bypass actor Repository admin,
    # mode Always allow, and save. Then re-run step 4 bypass check:
    gh api repos/<owner>/<repo>/rulesets/<audit branches id> --jq '[.bypass_actors[] | [.actor_type, .actor_id, .bypass_mode]] | tojson'
    # PASS: exactly ["Integration",5875190,"always"] and ["RepositoryRole",5,"always"], in any order
    # FAIL: no RepositoryRole entry, any other actor, or a mode other than always

    # Delete the failed run branch
    gh api -X DELETE 'repos/<owner>/<repo>/git/refs/heads/security-audit/<date>-<run_id>'
    # PASS: no output; FAIL: an error (the bypass actor is missing, or the name is wrong)
    git ls-remote origin 'refs/heads/security-audit/<date>-<run_id>'
    # PASS: no output; FAIL: the branch still exists

    # Remove the Repository admin bypass actor from Audit report branches again, and save.
    # Then re-run step 4 bypass check:
    gh api repos/<owner>/<repo>/rulesets/<audit branches id> --jq '[.bypass_actors[] | [.actor_type, .actor_id, .bypass_mode]] | tojson'
    # PASS: [["Integration",5875190,"always"]], the audit App only
    # FAIL: any other actor, the admin role included, or another mode
    ```

    If the publish job failed because it found two App-authored draft advisories
    with one fingerprint (its summary names this step), read both drafts, close the duplicate (keep the
    older one), then start the new run. The closed duplicate does not count as a dismissal: the kept draft
    takes precedence over it while the draft is open. Check: the new run opens the report PR.

    The publish job's summary line names the class of a failure, never its detail (ADR 0040, "Exit codes
    and the summary line"). What to do for each, before the new run:

    - `artifacts: unexpected: see runbook step 11` (the artifact check of the model or the publish job):
      exit 3 of the artifact check, not the core's outcome 3 (`result refused`, below). The check
      refuses five things: an artifact the job does not expect, a name twice, an expected artifact
      missing, an expired one, and a total above the listed count (a second page: more than 100
      artifacts). Read the run's artifact list
      (`gh api repos/<owner>/<repo>/actions/runs/<run_id>/artifacts --jq '[.artifacts[] | [.name, .expired]]'`);
      do not download an unexpected one. Check: the new run's list is exactly `checks-result` at the
      model job's check, and `checks-result` and `model-result` at the publish job's.
    - `result refused: see runbook` (exit 3): the model's result did not decrypt or broke the schema, or
      the release tag or image digest it named did not check out against GitHub. Start the new run; if
      it repeats, read the model job's preflight line and the skill before changing anything.
    - `publish refused: see runbook` (exit 4): a text about to be published did not pass the scans.
      Nothing was published. The line does not say which of three causes it was: (1) a scan found a
      token, a key or an encoded run; (2) the text held gitleaks' inline allow comment, which the
      program refuses before gitleaks runs, in any letter case; (3) gitleaks did not start or did not
      finish within its deadline (2 minutes when unset). Look for (3) first, then (2): a publish step
      that ran for about a scan deadline or longer, or a gitleaks binary missing at the path its flag
      names on the runner, points to (3); fix that and start the new run. For (2), start the new run
      once: the model writes the texts afresh, and a refusal that repeats with no sign of (3) is
      treated as (1). A real hit (1), or a refusal neither (2) nor (3) explains, is a possible leak:
      rotate per step 10 (the safe default), then start the new run.
    - `private findings not filed: see runbook` (exit 5): GitHub refused advisory creation to the App;
      the report says "private findings: not filed" and the private findings were dropped. Repeat step
      3's advisory check, then start the new run.
    - `publish refused: duplicate draft advisories: see runbook step 11` (exit 6): the paragraph above.
    - `previous report unreadable: see runbook` (exit 7) has **two causes**, and the line does not say
      which: (1) the newest report in `docs/security/audits/` is unreadable as the publish script renders
      one (a hand edit, a merge that changed a table, a cell outside printable ASCII); (2) the newest
      report is dated **after the run**, or on the run's own date with a run ID **not below the run's
      own** (a hand-made report, or a later run's report merged first). Check: list the directory
      (`ls docs/security/audits/` on `main`) and compare the newest name's date and run ID with the
      failed run's; for (1), diff the newest report against what the publish script wrote in its PR. Fix
      it through a pull request to `main` (for (2), rename or remove the misdated report), then start
      the new run.
    - `GitHub call failed: see runbook` (exit 8): a GitHub call failed or answered unexpectedly; an
      existing `security-audit/<date>-<run_id>` branch lands here too (delete it as above). Check
      GitHub's status, then start the new run.
    - `configuration invalid: see runbook` (exit 9): a variable, flag or secret of the publish step is
      missing or malformed (the fingerprint key shorter than 32 bytes included), or a secret that must
      not be in the step is set there; the program was started with a bad or unknown flag or
      subcommand (`publish` included, until PR 4b adds it); or `GITHUB_STEP_SUMMARY` is unset, not an
      absolute path or cannot be opened. Compare the environment's secrets and variables with step 2,
      and the step's command line with the workflow on `main`.
    - `publish failed: see runbook` (exit 1): any other failure. Start the new run; if it repeats, the
      publish program needs a fix through a pull request.

## Decisions and open questions

Every question is decided. The last one, Decision 2's rule that the public report says only whether
private findings exist, and the per-check coverage rule that follows from it, the maintainer confirmed on
2026-11-21 at PR #444's review.

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
- **Tools:** gitleaks, osv-scanner, trivy (the image and its SBOM), zizmor and actionlint, OpenSSF scorecard,
  time-boxed `go test -fuzz`. trufflehog, grype and semgrep are dropped as overlaps (gosec, govulncheck
  and CodeQL already run in CI).
- **Plan:** milestones as above, implemented after PRD #161 milestones 3 and 4 merge, reusing milestone
  4's environment and token pattern.
- **The repository stays public.** Private vulnerability reporting is already enabled.

("No launcher script" refers to the issue's VM launcher; the model job's committed launcher above is a
step of the workflow.)

**Decided (maintainer, 2026-11-18, on PR #273):**

- **Cadence:** the audit runs **monthly** on a schedule (cron `17 4 3 * *`, UTC), and the maintainer,
  who owns the repository and alone can, starts it by hand at any time (`workflow_dispatch`, no inputs).
  The skill (`security-audit-run`), the workflow (`scheduled-security-audit.yml`) and every other name are
  cadence-neutral. A later cadence change edits only the values derived from the cadence (the list in
  milestone 3). *Reason:* a monthly report is enough to keep a dated record of the posture and costs
  about a quarter of the reviews and subscription usage, and the subscription's usage limits are shared
  with the agents in the VM and the AI reviewer of PRD #161 milestone 4 (The subscription token); a run
  by hand covers a release or an advisory between two scheduled runs.

The PRD's recommendation on each question below, open until then, is **accepted**. Each keeps the
options that were weighed and the reason given, so the decision can be revisited with its context.

1. **The audit App `shift2eu-audit` and its exact permissions.** Proposed: Contents write and Pull
   requests write (the report PR), Issues write, Repository security advisories write, Actions read and
   Attestations read. Can Contents write be narrowed, for example so the report PR may touch only
   `docs/security/`, enforced by a required check?

   Side effects of what is proposed, beyond what the audit needs:
   - **Contents write** also creates releases and tags other than `v*`, deletes unprotected branches (the
     agents' and release-please's), merges into unprotected branches, and sends `repository_dispatch`.
   - **Pull requests write** also submits reviews, `APPROVE` included.
   - **Repository security advisories write** also publishes an advisory and requests a CVE: disclosure.

   *Decision (maintainer, 2026-11-18), with its reasons:*
   - **Keep Contents write**: GitHub App permissions cannot be narrowed to a path or a branch.
   - **Narrow its use.**
     - The token exists only in the publish job, which has no model and no release code, runs only `main`'s
       committed script, and calls only its fixed set of endpoints, with every advisory `state` hard-coded
       `draft` (milestone 2).
     - The `security-audit/**` ruleset lets only the audit App (its one bypass actor) create, update or
       delete those branches, so another token cannot plant a report (runbook step 4). An admin who must
       (trial PR 2 in runbook step 5, a failed run's branch in step 11) first adds the Repository admin role (`RepositoryRole` 5, `always`)
       as a temporary bypass actor and removes it afterwards, re-running step 4's bypass check (PASS: the
       audit App only).
     - A CI job fails any audit PR (authored by `shift2eu-audit[bot]`, or whose event sender is the App, or
       whose head branch is under `security-audit/`) that is not based on `main` or that touches anything
       but regular non-executable files (mode 100644), or their deletions, under `docs/security/`, with
       no path component starting with a dot. It re-runs when a PR is `edited`, so retargeting the base
       branch is checked too, and it passes trivially for other PRs, so it costs nothing elsewhere. It is
       required through a **new ruleset on `main`** with no bypass actor, so `Protect main` stays
       unchanged (runbook step 5). The sender key alone fails only the run of the App's own event; the
       branch ruleset below, which keeps the App to `security-audit/**` branches, makes it more than a
       one-shot signal (ADR 0040).
     - A branch ruleset on `~ALL` excluding the default branch and `security-audit/**` restricts
       creations, updates and deletions to the legitimate pushers (`main` stays out: `Protect main`
       guards it, and including it blocked every merge), not the App, so it can write only `security-audit/**` branches (runbook
       step 5). That ruleset now closes the agents' and release-please's branches to the App.
     - `Protect main` requires the code owner's approval, so an `APPROVE` by the App never counts (as PRD
       #161 milestone 4 checks for its Apps).
     - Optional, the maintainer's choice at runbook step 5: a tag ruleset on `**` that lets only the
       release App create tags, which also closes non-`v*` tags to every other token.
   - **Never in a bypass list** of `Protect main`, the audit-paths ruleset or the tag rulesets. Its only
     bypass entry is the `security-audit/**` branch ruleset (runbook steps 4 to 6).
   - **Advisories and issues it did not write are never touched.** The publish script matches and
     updates only advisories the App authored, in `draft` or `closed` state, never a `triage` one (a
     private vulnerability report) or one with a collaborator the App did not add, and matches only
     issues `shift2eu-audit[bot]` authored (milestone 2).
   - **Drop Actions read and Attestations read from the App.** The audit's reads use the jobs' read-only
     `GITHUB_TOKEN`, with exactly the read scopes the ADR lists, a refused read reported "not run", so
     the App holds write scopes only, and only in the publish job. Give it no Workflows
     permission, so it can never change `.github/workflows/`.
   - **No `Administration: read`.** Scorecard's admin-only checks are reported "not run (needs admin
     read)" and the rulesets are read the `release-tag` way instead. Giving them the App's token would put
     an App credential in the checks job, which the three-job split exists to prevent.

2. **Private findings as draft advisories.** Private findings filed as draft repository security
   advisories created by the App, the public report saying only that findings were handled privately.

   *Decision (maintainer, 2026-11-18):* **yes, with six conditions**, because a public report is public
   the moment its PR opens and a draft advisory is the one place GitHub keeps a finding private to the
   maintainer.
   - The model writes private findings only to the encrypted structured result, never to the report. The
     publish script decides what is private (Report and triage) and creates one draft advisory per private
     finding from that result: `state: draft`, no CVE request, no publishing. Only whether private
     findings exist reaches the report and the publish job's summary (the next condition).
   - **The public report says only whether private findings exist** ("private findings: handled
     privately" or "private findings: none"), never how many are new, still open, not reproduced,
     regressed or dismissed; the PR's title, body and commit message and the publish job's summary follow
     the same rule. A reproduced private finding sets the flag even when its advisory was closed as
     dismissed, so the flag stays steadier from run to run and signals less. The per-check coverage
     follows the same rule: the publish script, not the model, marks a check "findings" only for a public
     finding and "run" otherwise, so a check whose only findings are private reads exactly like a check
     with no findings ("run"); the why of a "not run (why)" comes from a fixed vocabulary in the publish
     script, never the model's text (milestone 2). *Decided by the review rounds on PR #273 (2026-11-18),
     confirmed by the maintainer on 2026-11-21 at PR #444's review.* *Reason:* a public count tied to a
     named release signals an undisclosed serious flaw before a fix exists, and more sharply for a run
     started right after a release; a per-check "findings" next to "handled privately" would say where
     it is. *Residual risk:* the presence flag still says that something exists; the ADR records it.
   - Each advisory carries the finding's fingerprint, a keyed hash (HMAC with `AUDIT_FINGERPRINT_KEY`,
     held only in the publish job) that nobody can predict from the committed script. The publish
     script, not the model, tracks private findings across runs: it lists the advisories **the App
     authored**, in `draft` or `closed` state, never a `triage` one or one with a collaborator or credit
     the App did not add, updates a recurring one with `PATCH` (state stays `draft`), marks one not
     reproduced, and files a recurrence of a closed one as "regressed" only if it was marked not
     reproduced before it was closed (otherwise "known, dismissed"). The model never sees an earlier
     private finding, and an outsider's private report can never receive one.
   - When the run cannot classify a finding, it is private.
   - Advisory text gets the token and key patterns and the encoded-token check (base64 and hex runs
     decoded and matched against those patterns), because a draft advisory is one click from
     publication; it does not get public text's encoded-run refusal, because PoCs and crash inputs are
     often base64 (The publish script).
   - **Fallback:** if GitHub refuses advisory creation to the App's installation token, the run fails
     closed: it publishes only the public count, the private-findings flag and the coverage, the report
     says "private findings: not filed" instead of "handled privately" (still one bit), and the summary
     says "private findings not filed: see runbook". The private findings are dropped, never written
     anywhere else. Milestone 2 is blocked until runbook step 3 confirms creation and update work, so the
     fallback is a guard, not the plan.

3. **How Claude Code runs in Actions.** Options: `anthropics/claude-code-action` pinned by SHA, or the
   Claude Code CLI installed at a pinned version and checksum and run headless (`claude -p`) through the
   committed launcher.

   *Decision (maintainer, 2026-11-18):* the model runs through the **Claude Code CLI** with the
   maintainer's Claude subscription token (a Claude Code OAuth token, no API key), **the same way as the
   AI reviewer of PRD #161 milestone 4** (PR #272), so the repository has one pinned way and one Renovate
   rule, **provided that way meets this PRD's requirements**: started through the launcher's `env -i`
   allow list with a fresh `HOME`, no managed-settings file, only the audit's settings file (no user,
   project or local setting sources), the skill passed explicitly, strict empty MCP configuration, no
   hooks, the sandbox keys and empty Bash network, path-scoped read tools, an allow list in a mode that
   refuses the rest, never a bypass mode, the binary run by absolute path, its output kept out of the log,
   all pinned by the `test/architecture` check. **Where the reviewer's way cannot meet them, the audit
   runs the same pinned CLI through its own launcher**, and the ADR says why. *Reason:* a GitHub Action
   that hides the launch behind its own inputs is unlikely to meet the `env -i` requirement, which
   favours the CLI, and one pinned CLI for both workflows keeps one Renovate rule.

4. **A public report PR on every run, even with nothing new?**

   *Decision (maintainer, 2026-11-18):* **yes: a report PR for every run, scheduled every month or
   started by hand, even when nothing is new**, each run's report its own file, named by its date and run
   ID. *Reason:* a dated report with no findings is the evidence the audit ran and what it covered, and it
   is one docs-only PR a month to approve. Revisit after a few months if the reviews cost more than they
   show.

5. **Fuzz targets.** The tree has no `Fuzz*` test today, and the audit must not change code. Fuzzing runs
   in a container in the checks job, which has no model, so the model cannot write throwaway harnesses
   for it. Options: committed harnesses that only the audit runs, or a separate issue that adds committed
   fuzz targets (the inventory decoder, the report renderers) that CI and the
   audit both run.

   *Decision (maintainer, 2026-11-18):* **a separate issue for committed fuzz targets**, built test-first
   like any other change. *Reason:* targets CI runs too catch a regression on every PR, not once a run,
   and harnesses only the audit ran would be code nobody else tests. Until it merges, the report lists
   fuzzing as "not run (no `Fuzz*` targets)" in its limits.

6. **Outbound network from the runner.** GitHub-hosted runners allow any outbound traffic, so a model led
   astray by hostile input (a dependency's advisory text, a scanner's output, a crafted release file)
   could try to send the subscription token or a private finding elsewhere. Options: rely on deny rules
   and containers, add a third-party egress-filtering action, or contain the model step itself.

   *Decision (maintainer, 2026-11-18):* **contain the model step; no third-party egress action**, which
   would be a privileged action in the job that holds the token.
   - The model runs in its own job, on a fresh runner that runs no untrusted code: every networked or
     untrusted-code check runs in the checks job, which holds no token, and reaches the model job only as
     ciphertext.
   - **Bash: no network, enforced; the Claude Code process: the model API only, by its configuration,
     not enforced.** The sandbox has an empty network allow list, cannot be bypassed per command, and
     does not auto-allow. On every run a model-free step stops Docker and removes the runner user's sudo
     rule, and the preflight proves before the model starts that a write outside the work area, a TCP
     connect, a Docker socket connect, `sudo -n true`, a read of the launcher's canary through `/proc`
     and a write to `$GITHUB_ENV` all fail.
   - Claude Code is started through `env -i` with a fresh `HOME`, so nothing an earlier step wrote to
     `$GITHUB_ENV` or a settings file reaches it, and its read tools see only the checkout copy, the
     checks' results and the work area.
   - Any command it runs goes through the wrapper, which names what it may `exec` and scrubs the
     environment; the preflight's canary probe shows on every run that no tool call can read the
     launcher's environment.
   - The token is only in the model step's step-level `env:`, pinned by the `test/architecture` check.
   - Deny rules stay as defence in depth only: Bash patterns are easy to step around.
   - Record in the ADR the residual risks: the CLI process's own network is limited by configuration
     only, so a flaw in the CLI, or a sandbox escape, could still send the subscription token elsewhere;
     and a token the model encodes in a way the publish scan does not recognise (split across fields,
     re-encoded beyond base64 and hex, or below the encoded-run thresholds) could reach the public report.
     Runbook step 10 rotates it.

7. **When public `security` issues are opened.** The issue's plan opens one `security` issue per public
   finding in the run. But the classification is automated and the issues are public at once, while the
   report PR waits for the maintainer's review. Options: the run opens the issues with the PR; the next
   run's publish job opens them from reports the maintainer has merged; the maintainer opens them by hand.

   *Decision (maintainer, 2026-11-18):* **the next run's publish job opens them from merged reports**,
   model-free, matched by the fingerprint in the report's findings table against every issue
   `shift2eu-audit[bot]` authored, open or closed, so none is opened twice, one the maintainer closed is
   not reopened, and an outsider's issue cannot suppress one. Nothing becomes an issue that the
   maintainer has not seen in a merged report, and no new trigger or workflow is needed. The delay until
   the next run (up to a month, a value derived from the cadence; less when the maintainer starts one by
   hand) only applies to public findings, which are now confirmed low or informational ones (hardening
   included). The maintainer can open one sooner by hand.
