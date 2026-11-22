# 0040. Audit the repository and the latest release on a schedule, with a model that triages and a script that publishes

- Status: Accepted, Decision 2's presence-flag rule and the coverage rule that follows from it included:
  the maintainer confirmed both on 2026-11-21 at PR #444's review (see Context).
  PRD #175 milestone 2 amends this record with the workflow's implementation (below); its first
  amendment, how Claude Code runs in the model job, is dated 2026-11-22
- Date: 2026-11-21, amended 2026-11-22

## Context

PRD #175 ([`prds/175-scheduled-security-audit.md`](../../prds/175-scheduled-security-audit.md)). Every
change to `shift2eu` is reviewed as a **diff**: each unit's `security-audit` pass, the AI reviewer
([ADR 0033](0033-review-pull-requests-with-an-ai-reviewer-that-only-comments-or-requests-changes.md)),
the maintainer's approval, and gosec, govulncheck and CodeQL in CI. Nothing looks at the whole tree, its
git history and the **released** artifacts as an attacker would, tests the released CLI with hostile
input, checks a release end to end against [the verification guide](../guide/verify-releases.md), or
notices a new advisory against a dependency nobody touched. Users have no `SECURITY.md`.

The forces:

- **The repository is public.** A finding written to a pull request, an issue, a workflow log or an
  artifact is disclosed the moment it is written, before the maintainer reads it.
- **The audit reads hostile input**: advisory text, scanner output, release files, the repository itself.
  A model that reads them can be steered by them.
- **The audit must stay read-only**: it never fixes, never touches a cloud, and writes to GitHub only
  what it must to report.
- **The model's budget is a subscription** shared with the agents and the AI reviewer.

This record holds the design **as far as it is known before milestone 2**, the workflow. PRD #161
milestone 4, the AI reviewer, has merged since the PRD was written: its workflow,
[`.github/workflows/ai-review.yml`](../../.github/workflows/ai-review.yml), and ADR 0033 are the pattern
milestone 2 reuses for the `main`-only environment, the App token minted in the workflow and the pinned
Claude Code CLI on the subscription. The scope note of 2026-11-20 applies throughout: the server is no
longer released ([ADR 0037](0037-retire-the-http-api-and-release-the-cli-alone.md)), so the audit covers
the CLI's release archives and its signed image, and no server, OpenAPI document or server harness.

The maintainer decided where it runs, the model, the tools, the report pull request and the cadence on
2026-11-18 (issue #175 and PR #273), and accepted the PRD's recommendation on Decisions 1 to 7. The
maintainer confirmed one further rule on 2026-11-21 at PR #444's review: Decision 2's, that the
public report says only **whether** private findings exist, and the per-check coverage rule that
follows from it.

## Decision

**A GitHub Actions workflow on `main`, `scheduled-security-audit.yml`, audits the repository and its
latest published release on a schedule and when the maintainer starts it, in three jobs on fresh
GitHub-hosted runners: a checks job with no model and no write credential, a model job that triages the
checks' results with Claude in a sandbox, and a publish job whose committed, model-free script alone
decides what is public and alone holds the audit App's token. The results go to a dated public report in
a docs-only pull request, to draft security advisories for private findings, and, after the maintainer
has merged a report, to `security` issues for its public findings.** The model's instructions are the
`security-audit-run` skill ([`.claude/skills/security-audit-run/SKILL.md`](../../.claude/skills/security-audit-run/SKILL.md)),
which links to `security-audit`'s checklist and is distinct from it.

### Scope

- **Audited:** the repository on `main`, its tree and full history; the latest published release: its
  archives, `checksums.txt`, the CLI's image, its attestations and SBOM; `.github/`, `.claude/`, `.pi/`,
  `.dot-agent-deck.toml` and `dev/` as attack surface.
- **Not audited:** other repositories, dependencies' own code beyond their advisories, GitHub's and
  GHCR's services, any cloud. The audit holds no cloud credential.
- **Writes:** the report pull request (`docs/security/` only) and draft security advisories (created and
  updated, never published), both through the audit App's token in the publish job; `security` issues
  for public findings of merged reports. Nothing else: no fix, no code or `testdata/` change, no comment
  on or closing of other issues and pull requests, no release, tag, package, published advisory or CVE
  request.

### The model and its authentication

- **Claude Fable 5.2** (`claude-fable-5-2`), effort high, through the Claude Code CLI.
- It authenticates with a **Claude Code OAuth token** the maintainer creates with `claude setup-token` on
  their own machine, under the subscription the agents already use, stored as the secret
  `CLAUDE_CODE_OAUTH_TOKEN` of the `security-audit` environment only, and referenced only in the model
  step's step-level `env:`. No API key, as in ADR 0033.
- **Usage limits are shared** with the agents in the VM and with the AI reviewer. An audit run can slow
  them, and they can exhaust the limit a run needs. The scheduled time, early on a UTC morning, is
  chosen for when the agents are usually idle; the maintainer picks a quiet time for a run started by
  hand. A run stopped by the limit fails visibly and is recovered by a new manual run.
- The token expires; the runbook records its creation and expiry, and how to rotate it.

### The cadence and the values derived from it

**Monthly**, with the cron `17 4 3 * *` (04:17 UTC on the 3rd of each month; GitHub evaluates cron in
UTC), enabled only after the first manual run's report is merged (milestone 3). The cron is the only
place the schedule is set, and no name carries the cadence: no file, job, step, skill, secret, label or
branch name. A cadence change edits exactly these values and renames nothing:

- the cron;
- the **38-day gap threshold** (the longest month, 31 days, plus 7 days of slack), in the staleness check
  of the `release-tag` and `dispatch` skills and in the publish script's gap flag;
- Decision 7's delay before a merged report's public findings become issues (up to one cadence);
- this record;
- the cadence text in [`docs/security/README.md`](../security/README.md), the one user-facing text that
  states it. `SECURITY.md`, the skill, the report template and the runbook link to it.

*Why monthly:* a monthly report keeps a dated record of the posture at about a quarter of the reviews and
subscription usage of a weekly one, and a run by hand covers a release or an advisory between two runs.

### Who can start a run

- The workflow triggers on `schedule` and `workflow_dispatch` only, and its `workflow_dispatch` takes no
  inputs. Anyone with Actions write can start a run, but only the maintainer's, who owns the repository,
  does anything: in anyone else's, every job is skipped. No login is hard-coded.
- **One guard condition**, used verbatim by every job's `if:` and inside the concurrency group:

  `github.ref == 'refs/heads/main' && github.run_attempt == 1 && (github.event_name == 'schedule' || github.triggering_actor == github.repository_owner)`

- **It skips every re-run.** A re-run of a scheduled run keeps `event_name == 'schedule'` with the
  re-runner as `triggering_actor`, so without `github.run_attempt == 1` anyone with Actions write could
  re-run a past scheduled run. A failed run is recovered by a new manual run, which also keeps an earlier
  attempt's artifacts out of the artifact check.
- The workflow-level concurrency group embeds the same condition, so a run that would be skipped never
  queues behind or cancels a real one:
  `group: ${{ <guard condition> && 'scheduled-security-audit' || format('skipped-{0}', github.run_id) }}`,
  with `cancel-in-progress: false`. A run started while another is in progress waits for it.
- **If the repository is transferred to an organisation**, `github.repository_owner` becomes the
  organisation, which never triggers a run, so manual runs then fail closed (every job skipped) while
  scheduled runs still run. The guard has to be revisited then; it never opens up by accident.
- All three jobs run in the `security-audit` environment, whose deployment branch policy is `main` only,
  so a run on another ref never receives a secret or variable.

### Three jobs, two keys

| Job | Runs | Holds |
|---|---|---|
| **checks** | every networked check and every piece of untrusted code (release binaries, the image, fuzz targets), in containers without network where it runs release code | `GITHUB_TOKEN` with read scopes only; the model key's **public** half |
| **model** (`needs: checks`) | no check, no release code, no container; the model in Claude Code's sandbox | the subscription token, in the model step only; the model key's private half; the publish key's public half |
| **publish** (`needs: model`) | `main`'s committed publish script only, and gitleaks over the text it is about to publish | the publish key's private half; the audit App's key; the fingerprint key |

- Nothing crosses between the jobs but **two ciphertexts**, encrypted to two key pairs of a pinned
  encryption tool (`age` or an equivalent milestone 2 names): `checks-result`, encrypted to the **model
  key**, and `model-result`, encrypted to the **publish key**, each an artifact with a one-day retention,
  downloaded by exact name after a listing that fails closed on any unexpected or duplicate artifact. No
  job output, cache or other artifact crosses.
- *Why ciphertext and not job outputs:* a public repository's artifacts are downloadable by anyone, and a
  job output is plaintext one `echo`, `set -x` or debug-logged re-run away from the public log;
  ciphertext stays harmless wherever it ends up. Anything in the checks job could forge a ciphertext with
  the public half, so each receiving job treats what it decrypts as untrusted input.
- No raw tool output, transcript or private finding reaches a workflow log or a readable artifact. The
  logs show the audited commit and tag, tool versions and that a job finished; the publish job alone
  prints the public total and whether private findings exist, never their number.

### The checks and what the model does

| # | Check | Job, run by | Network |
|---|---|---|---|
| 1 | Record the `main` SHA and the latest release tag | checks, workflow step | GitHub reads |
| 2 | Download pinned tools, verify their checksums; `go mod download` for the module and `tools/`; trivy's database | checks, workflow step | yes |
| 3 | govulncheck (source) and osv-scanner on the module and `tools/` | checks, workflow step | vulnerability databases |
| 4 | gitleaks on the full history, `--redact` | checks, workflow step | no |
| 5 | zizmor and actionlint on `.github/workflows` | checks, workflow step | GitHub reads only |
| 6 | OpenSSF scorecard, read-only (no publishing, no `id-token`) | checks, workflow step | GitHub reads |
| 7 | The release's `checksums.txt`, `gh attestation verify` on every file, `cosign verify` of the image and its provenance and SBOM attestations; the latest release and the image's `latest` tag agree | checks, workflow step | yes |
| 8 | Image pulls from GHCR | checks, workflow step | yes |
| 9 | `govulncheck -mode=binary` on every release binary (read, not run) | checks, workflow step | vulnerability database |
| 10 | trivy on the image and its SBOM, from the downloaded database | checks, workflow step | no |
| 11 | The image runs as non-root, with a read-only root filesystem and no shell where none is expected | checks, containers | no |
| 12 | Dropped: the server is no longer released (ADR 0037) | none | no |
| 13 | The CLI on hostile inventory files | checks, container, `--network none` | no |
| 14 | Time-boxed `go test -fuzz` of the committed `Fuzz*` targets (none yet: Decision 5) | checks, container, `--network none`, `GOPROXY=off` | no |
| 15 | Supply-chain reads: action pins and workflow `permissions`, rulesets and environments as the `release-tag` skill reads them, Renovate's state, dependencies changed since the last report | checks, workflow step | GitHub reads |
| 16 | Encrypt the raw results to the model key, upload `checks-result` | checks, workflow step | artifact upload |
| 17 | Check the artifacts, download and decrypt `checks-result` into the audit directory, copy the checkout; stop Docker and remove the runner user's sudo rule; the sandbox preflight | model, workflow steps | tools, artifact download |
| 18 | The `security-audit` checklist across the whole tree, a review of what the agents may run, triage of rows 3 to 15 with a PoC or precise trace for each finding, the status of the last report's public findings, the structured result | model, Claude Code, sandboxed | Bash: none (enforced); Claude Code: the model API only, by configuration, not enforced |
| 19 | Encrypt the result to the publish key, upload `model-result` | model, workflow step | artifact upload |
| 20 | Check the artifacts, download and decrypt `model-result`, validate, classify, scan, publish | publish, its script | GitHub writes through the App |

Scorecard's checks that need admin read are reported "not run (needs admin read)"; crash inputs and PoCs
go only into the encrypted results or an advisory, never into `testdata/`. Fuzzing is "not run (no
`Fuzz*` targets)" until a separate issue adds committed fuzz targets that CI runs too (Decision 5).

### The disclosure rules the publish script enforces

The report pull request is public the moment it opens, so the publish script, not the model and not the
maintainer's later review, decides what is public, from each finding's severity, confirmation and
category alone:

- **Private** (one draft advisory each, never published by the audit): critical, high or medium;
  unconfirmed, suspected medium or above; category injection, authentication bypass, credential exposure
  or remote code execution, whatever the severity; a missing or unknown severity or category; and every
  secret gitleaks finds in the history, named by commit and rule only, marked "unconfirmed: maintainer to
  check" and never tried against its service.
- **Public**: confirmed low or informational findings, hardening included, in the report, and after the
  report is merged one `security` issue each (Decision 7); unconfirmed low or informational ones in the
  report's "needs investigation" list.
- The model may make a finding more private, never less. The public report, the pull request's title,
  body and commit message, and the publish job's summary say only **whether** private findings exist
  ("handled privately", "not filed" or "none"), never how many; a check whose only findings are private
  reads "run", like a check with none.
- **The category vocabulary is closed.** A finding's category is exactly one of: `injection`,
  `authentication bypass`, `credential exposure`, `remote code execution` (the four private categories),
  `supply chain`, `dependency vulnerability`, `configuration`, `hardening`, `denial of service` and
  `information disclosure`. Any other value is unknown, so the finding is private. Every gitleaks hit and
  every secret, token or key found anywhere is `credential exposure`; every instruction embedded in an
  input is `injection`. The `security-audit-run` skill gives the model this list, and milestone 2's
  result schema and publish script adopt the same list.
- Every advisory, issue and fingerprint the script matches is one the audit App wrote; it never touches a
  private vulnerability report (`triage` state) or another's issue.

**Residual risks**, accepted:

- **The model fills severity, confirmation and category**, which the script reads. A model that
  mislabels a finding, or is led by hostile input to call a critical one "low, hardening", makes it
  public the moment the pull request opens. The category floor and the private default for anything
  unknown narrow this; they do not close it. The maintainer's review of each report is the last check,
  after publication.
- **The private-findings flag still says that something exists.** It is one bit, kept steadier by being
  set whenever a private finding is reproduced, even one the maintainer dismissed, but a reader can still
  see it change.
- **An earlier report's false positives shape later runs.** The model never skips a finding and never
  writes a fingerprint; it may only propose a dismissal, as a one-line reason on a finding. A proposed
  dismissal only adds a False positives row: the finding stays in that run's findings table and is
  handled as any finding, Decision 7 included. The drop applies only from a later run, after the
  maintainer has merged the report that holds the row: the merge is the human gate on every
  suppression. The publish script computes every fingerprint from rule, file and sink, writes the rows
  and carries them forward, and drops a later finding only when its computed fingerprint matches a row
  exactly and the finding is low or informational in a category that is not private. But the model
  supplies the fingerprint's inputs (rule, file and sink) and the severity the floor tests, so the exact
  match stops accidental drops, not steered ones. The proposed reason is model text, which hostile
  input could steer, so a merged report could hide a low finding from every later run. The real bound
  is the severity and category floor; private false positives are never listed (they stay with the
  advisories), and the maintainer's review of each report, its False positives section included, stays
  the check.
- **The model step's network is limited to the model API by configuration, not enforced** (Decision 6).
  The model's Bash commands have no network, enforced by the sandbox; Claude Code's own connections are
  held to the model API only by its settings, which milestone 2 details.

### The audit App's permissions and their side effects (Decision 1)

The audit App `shift2eu-audit`, installed on this repository alone, holds **Contents write, Pull requests
write, Issues write and Repository security advisories write**, nothing else: no Actions or Attestations
read (the jobs' read-only `GITHUB_TOKEN` reads), no Workflows permission (it can never change
`.github/workflows/`), no Administration read (scorecard's admin checks are "not run" instead, so no App
credential reaches the checks job). Its token is minted only in the publish job.

What those permissions also allow, beyond what the audit needs, and what narrows each:

| Permission | Also allows | Narrowed by |
|---|---|---|
| Contents write (App permissions cannot be narrowed to a path or branch) | creating releases and tags other than `v*`, deleting and merging into unprotected branches, `repository_dispatch` | the token exists only in the publish job, which runs only `main`'s committed script and calls a fixed set of endpoints; the `security-audit/**` branch ruleset (only the App writes there; an admin only through a temporary bypass the runbook adds and removes); a required check that fails an App pull request touching anything outside `docs/security/`, in a new ruleset on `main`; a branch ruleset on `~ALL` excluding the default branch (`Protect main` guards it; including it blocked every merge) and `security-audit/**` that restricts creations, updates and deletions to its bypass actors (the maintainer's admin role, the agents App, Renovate, the release-please App), never the audit App; optionally a `**` tag ruleset that lets only the release App create tags |
| Pull requests write | submitting reviews, `APPROVE` included | `Protect main` requires the code owner's approval, so the App's never counts; the script never calls the review endpoints |
| Repository security advisories write | publishing an advisory and requesting a CVE: disclosure | every advisory `state` the script sends is hard-coded `draft`, and it never calls the publish or CVE endpoints |

The App is in no bypass list but the `security-audit/**` branch ruleset's. The required check is the job
`Audit report paths` of `.github/workflows/audit-paths.yml`, which runs `.github/scripts/audit-paths.sh`
under `pull_request_target`: main's copy of the workflow and the script runs, from a checkout of main, and
the pull request's commits are fetched as git objects and diffed, never checked out or executed. Under
`pull_request` the pull request's own copy of the script would run, and the App, which holds contents write
though not the Workflows permission, could change it and pass its own check. The job's token reads
contents only, it uses no secret, and the repository is public, so the fetch needs no credential.

The fence applies to an audit pull request, keyed on any of three fields of the event: its author is
`shift2eu-audit[bot]`, its sender is (the App's contents write lets it push a commit onto another author's
pull request), or its head branch starts with `security-audit/`. An audit pull request must be based on
`main`, and may add or change only regular, non-executable files under `docs/security/` with no path
component starting with a dot (a `.gitattributes` there could set `export-ignore` or `export-subst` on
the reports the release archives ship). The workflow runs on `opened`, `synchronize`, `reopened` and
`edited`, with no branch or path filter: retargeting a base branch is an `edited` event, so a pull
request that passed into another branch and is then retargeted to `main` is judged again rather than
keeping a stale green check. The ruleset requires `Audit report paths` from the GitHub Actions integration
(`integration_id` 15368), so a status of that name from another App does not count.

Residual risks of the check:

- The App's contents write lets it push to any branch no ruleset protects, including a commit onto
  another author's pull request or a branch with no pull request yet. The sender key fails only the run
  of the App's own event: a later event by another sender (a push on top, an `edited` title or body
  change, a close and reopen, a human opening the pull request later) runs the check again with a
  non-App sender and passes it, with the App's commit still in the pull request. The fix is a branch
  ruleset, part of runbook step 5 by default and created before the audit-paths ruleset goes active: it
  targets `~ALL` excluding the default branch (`~DEFAULT_BRANCH`) and `security-audit/**`, restricts
  creations, updates and deletions (`creation`, `update`, `deletion`), and has as bypass actors every legitimate pusher (the maintainer's admin role (`RepositoryRole` 5), the agents
  App `shift2eu-agents`, Renovate, the release App `shift2eu-release`) and not the audit App, which can
  then write only `security-audit/**` branches. The default branch stays out of its target: `Protect
  main` already guards `main`, and with `main` included the `update` rule blocked every merge into it
  (2026-11-22, PRs #460 and #461). The residual risk is bounded by the fence plus this ruleset; without
  the ruleset, the sender key is only a one-shot signal.
- Denial of service: the App's Pull requests write lets it edit any pull request's title or retarget its
  base, each an `edited` run with the App as sender, so that pull request's required check fails. A
  re-run does not clear it: a re-run of a `pull_request_target` run replays the original event, so the
  sender is still the App and it fails again. It clears on the next event by another sender (the author
  editing the title again, or a close and reopen). It lets nothing in; it only blocks a merge for a while.
- The ruleset pins the integration, not the workflow: another GitHub Actions workflow holding
  `checks: write` could post a check run of the same name from the same integration. The App cannot add
  or change one (it has no Workflows permission); a workflow change that grants `checks: write` is
  reviewed like any other.
- Under `pull_request_target` the workflow file and `github.sha` come from the pull request's base
  branch, not always from `main`. Every pull request into `main`, the only ones the audit-paths ruleset
  requires the check on, runs `main`'s copy of the workflow and of the script; a pull request into another
  branch runs that branch's copy, which could differ. The branch ruleset on `~ALL` (excluding the default
  branch, which `Protect main` guards, and `security-audit/**`) keeps the audit App from changing any
  other branch's copy; without it, the App's contents write could. Retargeting the pull
  request to `main` is an `edited` event that runs `main`'s copy on the same head commit, and that newer
  run decides; until it finishes, the earlier run's result from the other base stands on the head commit.
  Code-owner approval on `main` bounds that window.

### No other App holds Actions write

**No GitHub App installed on the repository other than the release flow's holds Actions write**: not the
agents' App, not the AI reviewer's, not Renovate. Actions write starts a `workflow_dispatch` run, and a
run spends the subscription limit shared with the agents and the AI reviewer and publishes a report pull
request and advisories. The guard condition already skips a run anyone but the maintainer starts, and
every re-run; this rule keeps a second guard. The maintainer checks it before milestone 2 merges and
whenever an App is installed or its permissions change (the runbook's step 6).

### The report index (issue #280)

Each run adds `docs/security/audits/YYYY-MM-DD-<run_id>.md`, its UTC date and run ID, and nothing else
under `docs/`. `docs/security/README.md`, a **sub-index** that `docs/README.md` links, describes that
naming pattern and links the directory `audits/`; names start with the date, so they sort by date, and
the directory listing is the index. `test/docs`' `TestIndexListsEveryPage` accepts it: a page is listed
when `docs/README.md` or a sub-index links it (one level, no chains), and a link to a directory lists
every page directly inside it. `audits/TEMPLATE.md` lives there too, and the publish script and the gap
flag read only names matching the pattern.

*Why not an index line per report:* any index with one line per report, hand-written or generated and
checked, makes two report pull requests from the same `main` edit the same lines, so the second
conflicts once the first is merged, and the conflict cannot be resolved on the App's branch (a maintainer's commit on the
App's branch cannot be approved under `require_last_push_approval` and breaks the App-authored docs-only
check). With the directory link, two reports' pull requests merge in either order.

### What milestone 2 adds to this record

Milestone 2 **amends this record**, not a second one, with: how Claude Code runs in Actions (Decision 3:
the AI reviewer's pinned CLI where it meets the PRD's requirements, the audit's own launcher where it does
not, and why); its sandbox, settings file, permission mode and audit directory; how the runner's
user-namespace restriction is handled; the sandbox preflight's six probes and the model-free step that
stops Docker and removes the runner user's sudo rule; the encoded-run thresholds (the minimum length L and
the entropy N) and the encoded-token residual risk; the encryption tool and the artifact handling; the
Actions-cache residual risk; the container flags; the time budget; the checks job's exact read scopes;
the App token's exact `permission-*` inputs; the finding limit; the publish script's false-positive
matching and its derivation of each finding's status; the closed category list in the result schema;
the wrapper's allowed commands and its scrubbed-environment allow list; and whether the Claude
subscription is a Team or Enterprise plan, so whether server-managed settings could reach the model
step.

The first amendment, below (2026-11-22), covers how Claude Code runs: the shared launcher, the audit's
launcher and its environment, the audit directory, the settings file and its permission mode, the
wrapper, the sandbox preflight, the user-namespace handling and the Claude Code pin. The other items of
the list above come with the pull requests of milestone 2 that build them.

### How Claude Code runs in the model job (milestone 2, amended 2026-11-22)

**Decision 3 settled:** the audit runs the **same pinned Claude Code CLI** as the AI reviewer, installed
by the same script, started by the **same Go launcher library**, `ci/internal/claudecode`, with its own
model, tool list, settings file, directories and environment allow list. The reviewer's launch, `claude
-p` with every tool, setting source, MCP server and slash command off, an empty working directory and
HOME of its own and an environment that inherits nothing, meets the PRD's requirements for a model with
no tools. It cannot carry the audit unchanged: the audit needs tools, the sandbox, a settings file, a
permission mode and fixed directories. So the launch becomes a library with those as parameters, the
reviewer's values its defaults, and the audit adds a program of its own around it (`ci/auditrun`)
for what only the audit does: the managed-settings refusal, the checksum check, the canary and the
preflight. *Why not `anthropics/claude-code-action`:* it hides the launch behind its own inputs, so the
`env -i` allow list, the fresh `HOME` and the absence of a managed-settings file could not be held by a
test of ours (Decision 3's reason).

#### The shared launcher, `ci/internal/claudecode`

- **Path and reach.** `ci/internal/claudecode`, a library under `ci/internal/`: Go's internal rule lets
  only packages under `ci/` import it, so neither release binary can reach it. Its importers are exactly
  `ci/aireview/internal/review` and the audit's library `ci/internal/audit` (below), held by the lax
  depguard rule `claude-code-launcher-importers`, which denies the package in every file but theirs
  (`ci/internal/audit` joins its exceptions in the pull request that adds it) and which
  `test/architecture`'s depguard pin holds. The strict depguard rule `claude-code-launcher` holds the
  package itself to named standard packages
  (from the move: `bytes`, `context`, `encoding/json`, `errors`, `fmt`, `os`, `os/exec`, `strings`,
  `time`; `io` joins them with `Transcript`). The
  forbidigo groups `ai-reviewer-env` and `ai-reviewer-one-process` hold for it as for the reviewer's
  library: it reads no environment variable and no file, locates no home or temp directory through
  `os`, and starts its one process with `exec.CommandContext`.
- **API.** `claudecode.New(claudecode.Config) *CLI` and `(*CLI).Ask(ctx, claudecode.Prompt) (string,
  error)`; `Prompt` is `{System, User string}`: `System` is written to a file of a temporary directory
  of the launcher's own and named by `--system-prompt-file` (or `--append-system-prompt-file`, below),
  `User` reaches the CLI on stdin alone. Every error wraps `claudecode.ErrModel` ("model error"), quotes
  at most 500 bytes of the CLI's text with the token redacted, names an error result by a constant class
  and its subtype only, and tells a timeout, a cancellation and the run's deadline apart, exactly as the
  reviewer's errors did (issues #274, #316, #374). `Config` holds:

  | Field | Meaning | Zero value |
  |---|---|---|
  | `Path` | the CLI's absolute path | refused: a relative path is an error before anything runs |
  | `Token` | the subscription's OAuth token, passed as `CLAUDE_CODE_OAUTH_TOKEN` | refused at the CLI |
  | `Model` | `--model`; letters, digits, `.` and `-` only | refused: no run asks the CLI's default model |
  | `Timeout` | the run's own deadline | |
  | `Tools` | `--tools`, the built-in tools, joined with `,`; each a tool name of letters only | **no tools**: `--tools ""` |
  | `Effort` | `--effort`; one of `low`, `medium`, `high`, `xhigh`, `max` | no flag: the CLI's default |
  | `Settings` | `--settings <absolute path>` | no flag |
  | `PermissionMode` | `--permission-mode`; `dontAsk` or `plan` only, never `bypassPermissions` | no flag |
  | `AppendSystemPrompt` | the system prompt is appended to the CLI's own (`--append-system-prompt-file`) | it replaces it (`--system-prompt-file`) |
  | `SafeModeOff` | leave `--safe-mode` out | `--safe-mode` |
  | `WorkDir` | the CLI's working directory, an absolute path that exists | a fresh empty temporary directory, removed after the run |
  | `Home` | the CLI's `HOME`, an absolute path that exists | a fresh empty temporary directory, removed after the run |
  | `Env` | extra `NAME=VALUE` entries of the allow list; a name the launcher sets itself (`CLAUDE_CODE_OAUTH_TOKEN`, `HOME`, `PATH`, the four switches below) is refused | none |
  | `SearchPath` | the CLI's `PATH` | `/usr/local/bin:/usr/bin:/bin` |
  | `Transcript` | an `io.Writer`: the run uses `--output-format stream-json --verbose`, copies every byte of stdout there, and reads the result from the last `result` element | `--output-format json`, one result, kept in memory up to 4 MiB |

  The arguments are always, in this order: `-p --model <Model>` [`--effort <Effort>`] `--output-format
  json|stream-json` [`--verbose`] [`--safe-mode`] `--tools <list>` `--setting-sources ""`
  [`--settings <file>`] [`--permission-mode <mode>`] `--strict-mcp-config --mcp-config
  {"mcpServers":{}}` `--disable-slash-commands --no-session-persistence --system-prompt-file|
  --append-system-prompt-file <file>`. The environment is always exactly `CLAUDE_CODE_OAUTH_TOKEN`,
  `HOME`, `PATH`, `DISABLE_AUTOUPDATER=1`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`,
  `DISABLE_TELEMETRY=1`, `DISABLE_ERROR_REPORTING=1` and the `Env` entries: nothing is inherited.
  `--mcp-config {"mcpServers":{}}` is new for both callers; the reviewer's launch gains it with the
  audit's fields, in the pull request that adds them, and its test then pins it.
- **What stays the reviewer's.** `review.Model` (`claude-opus-5-5`), `review.NewClaudeCLI(path, token,
  timeout)`, which is `claudecode.New` with that model and every other field at its zero value (no
  tools, safe mode, replaced system prompt, fresh directories, `json`), the `Asker` interface, the
  prompt's text and everything after the answer. `review.ErrModel` is `claudecode.ErrModel`, so its
  text and every `errors.Is` stay as they were. The reviewer's launch is unchanged by the move: same
  flags in the same order, same environment, same model, same errors; its tests in
  `ci/aireview/internal/review/claudecli_test.go` stay where they are and pin it.

#### The audit's programs

- **`ci/auditrun`**, the launcher the model step runs, a Go main package (not a script: it shares the
  library, and a test drives it with a fake `claude`), with its logic in the library
  **`ci/internal/audit`**, which `ci/auditrun` and the wrapper `ci/auditexec` import and nothing else (a
  lax depguard rule like `claude-code-launcher-importers`). Each gets a strict depguard rule of named
  standard packages, as `ci/aireview` has, pinned by `test/architecture`.
- **Its command line:** `auditrun -claude-tarball <path> -claude-integrity <sha512-...>`, both from the
  install step and the one pin (below). Its inputs from the step's environment, read through one
  `os.LookupEnv` passed to the library as the reviewer's `SettingsFromEnv` does: `CLAUDE_CODE_OAUTH_TOKEN`,
  `SHIFT2EU_AUDIT_CANARY` (64 lower-case hex characters), `GITHUB_ENV` (its path, for probe 6) and
  `GITHUB_STEP_SUMMARY`. It reads no other variable, and refuses to run when `ANTHROPIC_API_KEY` is set,
  as the reviewer does.
- **In order**, each step failing the run before the next:
  1. **Managed settings.** It fails if any of these exists, checked with `Lstat`, so a symbolic link
     counts: `/etc/claude-code` (the directory itself, which holds `managed-settings.json`,
     `managed-settings.d/`, `managed-mcp.json` and the policy `CLAUDE.md`), and, though the runner is
     Linux, `/Library/Application Support/ClaudeCode`. The library takes a root that stands in for `/`,
     so a test puts a policy file under a temporary root; `ci/auditrun` passes `/`.
     `CLAUDE_CODE_MANAGED_SETTINGS_PATH`, which points the CLI at another policy file, never reaches
     the CLI (the allow list below). The install script's `/etc/claude-code` refusal stays as a first
     check.
  2. **Checksum.** It computes the sha512 of the tarball the install step kept, compares it, as npm's
     `sha512-<base64>` integrity, with `-claude-integrity`, and only then extracts its one entry
     `package/claude` itself into `/srv/audit/state/bin/claude` (mode 0500) and runs that binary by
     absolute path. The binary is checked in the same step that runs it, against the one pin, with no
     second checksum to keep.
  3. **The audit directory** (below) exists as laid out: `checkout` and `checks` with no write bit,
     `work`, `state/home` and `state/tmp` empty, `bin/audit-exec` owned by root with mode 0555.
  4. **Bubblewrap** starts (the user-namespace check, below).
  5. **The preflight** (below).
  6. **The audit**: one `Ask` with the skill's text as the appended system prompt, read from
     `/srv/audit/checkout/.claude/skills/security-audit-run/SKILL.md`, and a fixed user prompt that names
     the audit directory's paths and the result file `/srv/audit/work/result.json`. The transcript goes to
     `/srv/audit/state/out/audit.jsonl` and the CLI's stderr to `/srv/audit/state/out/audit.stderr`,
     never to the log. The step prints one line, "finished" or the failure's constant class.
- **Exit codes:** 0 the audit finished and wrote the result file; 1 the run failed (the model, a probe
  not refused, no result file); 2 a usage or input error; 3 refused before the CLI ran (a managed
  setting, a checksum mismatch, the audit directory not as laid out); 4 bubblewrap cannot start.
- **The launcher's environment** (`env -i` in effect: `Config.Env` and the launcher's own entries, nothing
  inherited), exactly:

  | Variable | Value | Why |
  |---|---|---|
  | `CLAUDE_CODE_OAUTH_TOKEN` | the token | the subscription |
  | `HOME` | `/srv/audit/state/home` | fresh, inside the state area, which the tools cannot read |
  | `PATH` | `/usr/bin:/bin` | fixed; the sandbox's `bwrap` and `socat` are in `/usr/bin` |
  | `TMPDIR` | `/srv/audit/state/tmp` | the CLI's own temporary files stay in the state area |
  | `DISABLE_AUTOUPDATER` | `1` | no update of the pinned binary |
  | `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` | `1` | the model API only |
  | `DISABLE_TELEMETRY`, `DISABLE_ERROR_REPORTING` | `1` | as the reviewer |
  | `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` | `1` | the pinned CLI (2.1.330) strips its credentials from the environment of the commands it starts and isolates them with bubblewrap |
  | `SHIFT2EU_AUDIT_CANARY` | the step's canary | probe 5's target |

  The **canary** is 32 random bytes in hex, made by the model step's own `run:` line with `openssl rand
  -hex 32` and passed to `ci/auditrun` in its environment beside the token, never printed. That puts it
  in `/proc/<auditrun pid>/environ` from the moment the launcher starts, where the token is, so a probe
  that cannot read it there cannot read the token either; `ci/auditrun` passes it on into the CLI's
  environment, so the CLI's own environment and its children's are probed too. It is a dummy: it grants
  nothing. *Why not made by the launcher itself:* a variable a process sets after it starts never shows
  in its `/proc/<pid>/environ`, which is the file the probe must show unreadable.

#### The audit directory

Outside `/home`, made by a model-free step with `sudo install -d -o runner` before the hardening step
removes sudo, and never under `$RUNNER_TEMP` or the workspace (both under `/home/runner`):

| Path | Holds | Model's access |
|---|---|---|
| `/srv/audit/checkout` | a copy of the `github.sha` checkout, `.git` included, every write bit removed | Read, Grep, Glob |
| `/srv/audit/checks` | the decrypted `checks-result`, every write bit removed | Read, Grep, Glob |
| `/srv/audit/work` | the CLI's working directory; the result file `result.json`; `tmp/`, the commands' `TMPDIR` | Read, Grep, Glob, Write, Edit; Bash writes here only |
| `/srv/audit/state` | `home/` (the fresh `HOME`, with Claude Code's configuration and transcripts), `tmp/` (the CLI's `TMPDIR`), `bin/claude`, `out/` (transcripts and stderr) | none: denied to every tool |
| `/srv/audit/bin/audit-exec` | the wrapper, built from `ci/auditexec`, owned by root, mode 0555 | Bash runs it, nothing else |

The working directory is `/srv/audit/work`, so no `CLAUDE.md` or `.claude/` of the checkout copy is
found from it: they sit in a sibling directory, and the CLI looks for them only in the working directory
and its parents (`/srv`, `/`, which hold none). The skill reaches the model only as the system prompt
`ci/auditrun` hands it.

#### The settings file

**`.github/security-audit/claude-settings.json`**, read from the checkout copy
(`/srv/audit/checkout/.github/security-audit/claude-settings.json`), the only settings the CLI loads:
`--setting-sources ""` loads no user, project or local file, and `--settings` names this one. A later
change to `.claude/settings.json` cannot widen the audit. A test in `test/architecture` pins the file;
dropping or widening any entry below fails `make test`.

- **The audit's launch:** `Model` `claude-fable-5-2`, `Effort` `high`, `Tools` `Read,Grep,Glob,Write,Edit,Bash`
  (no `WebFetch`, `WebSearch`, `Task` or any other), `PermissionMode` `dontAsk`, `SafeModeOff` (the
  settings file must apply, and `--safe-mode` turns customisations off; each thing it would turn off is
  off by another flag or key here), `AppendSystemPrompt` (the default prompt explains the tools the
  audit uses), `WorkDir` `/srv/audit/work`, `Home` `/srv/audit/state/home`, `Transcript` the file above,
  `--strict-mcp-config` with the empty MCP configuration.
- **Permission mode `dontAsk`:** the non-interactive mode in which a tool call no allow rule matches is
  refused, not asked about. `permissions.defaultMode` is `dontAsk` as well, and
  `permissions.disableBypassPermissionsMode` is `disable`. Never `bypassPermissions`,
  `--dangerously-skip-permissions` or `--allow-dangerously-skip-permissions`.
- **Allow list** (`permissions.allow`), literal absolute paths in Claude Code's rule syntax, where `//`
  starts an absolute path (`/` alone would be relative to the settings file, and `~` is the fresh
  `HOME`):
  `Read(//srv/audit/checkout/**)`, `Read(//srv/audit/checks/**)`, `Read(//srv/audit/work/**)`,
  `Edit(//srv/audit/work/**)` and `Bash(/srv/audit/bin/audit-exec:*)`. Claude Code applies a `Read` rule
  to Read, Grep and Glob, and an `Edit` rule to Edit and Write; the file names no `Grep(...)`,
  `Glob(...)` or `Write(...)` rule, because in `-p` mode the CLI ignores a settings file it cannot
  validate, silently, and the preflight's Read, Grep and Glob probes show the rules hold for each tool.
- **Deny list** (`permissions.deny`): `Read(//proc)`, `Read(//proc/**)`, `Read(//sys)`, `Read(//sys/**)`,
  `Read(//etc)`, `Read(//etc/**)`, `Read(//home)`, `Read(//home/**)`, `Read(//home/runner/**)`,
  `Read(//srv/audit/state)`, `Read(//srv/audit/state/**)`, `Edit(//srv/audit/state/**)`, and as defence in
  depth `Bash(git push:*)`, `Bash(git commit:*)`, `Bash(git tag:*)`, `Bash(git remote:*)`, `Bash(gh:*)`,
  `Bash(curl:*)`, `Bash(wget:*)`, `Bash(nc:*)`, `Bash(ssh:*)`, `Bash(docker:*)`, `Bash(sudo:*)`,
  `Bash(env:*)`, `Bash(printenv:*)`, `Bash(set:*)` and `Bash(*/proc/*/environ*)`. A deny rule wins over an
  allow rule.
- **Sandbox:** `sandbox.enabled` `true`, `sandbox.failIfUnavailable` `true` (no unsandboxed fallback when
  bubblewrap cannot start), `sandbox.autoAllowBashIfSandboxed` `false`,
  `sandbox.allowUnsandboxedCommands` `false`, `sandbox.enableWeakerNestedSandbox` `false`, no
  `sandbox.excludedCommands`, `sandbox.network.allowedDomains` `[]`, `sandbox.network.allowUnixSockets`
  `[]`, `sandbox.network.allowLocalBinding` `false`. The sandbox takes its filesystem rules from the
  permission rules and the working directory: Bash writes only under `/srv/audit/work` and reads nothing
  the deny list names. The file sets no `sandbox.filesystem` key.
- **No hooks, no MCP:** `disableAllHooks` `true`, no `hooks` key, `enableAllProjectMcpServers` `false`,
  no `mcpServers`; the CLI gets `--strict-mcp-config --mcp-config {"mcpServers":{}}`. No `env`, `apiKeyHelper`,
  `awsCredentialExport` or `otelHeadersHelper` key: nothing of the file runs a program.

#### The wrapper, `ci/auditexec`

Built in the model job from `main`'s checkout (`go build -trimpath`, `GOFLAGS=-mod=readonly GOPROXY=off`)
and installed as `/srv/audit/bin/audit-exec` before sudo is removed. `audit-exec <name> <args...>` maps
`name` to a fixed absolute path, never through `PATH`, and `exec`s it; every other name, a path given as
the name included, is refused with exit 126 and one line on stderr that names the refused word, escaped
and cut to 64 bytes. The commands it may `exec`:

- `/usr/bin/git` with `--no-pager`, `-C <a directory under /srv/audit/checkout>` and one of the
  subcommands `log`, `show`, `diff`, `grep`, `ls-files`, `ls-tree`, `cat-file`, `rev-parse`, `rev-list`,
  `blame`, `shortlog`, `describe`, `for-each-ref`. It refuses `-c`, `--config-env`, `--exec-path`,
  `--git-dir`, `--work-tree`, `--namespace`, `--ext-diff`, `--textconv`, `--output` and `--output=`
  anywhere on the line.
- `/usr/bin/jq`, `/usr/bin/sha256sum`, `/usr/bin/base64`, `/usr/bin/od`, `/usr/bin/cat`, `/usr/bin/head`,
  `/usr/bin/tail`, `/usr/bin/wc`, `/usr/bin/sort`, `/usr/bin/uniq`, `/usr/bin/cut`, `/usr/bin/tr`: read
  and reshape text, none writes a file of its own or opens a connection (`sort -o` and `--output` are
  refused).

It `exec`s with exactly this environment, nothing inherited: `PATH=/usr/bin:/bin`, `HOME=/nonexistent`,
`TMPDIR=/srv/audit/work/tmp`, `LANG=C.UTF-8`, `LC_ALL=C.UTF-8`, `TZ=UTC`, and for git
`GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_TERMINAL_PROMPT=0`, `GIT_PAGER=cat`,
`GIT_OPTIONAL_LOCKS=0`.

*Settled by the implementation (issue #277):*

- **The git line** is `/usr/bin/git --no-pager -C /srv/audit/checkout <subcommand> ...`: the wrapper knows
  no other repository, and the first word after `git` must be a listed subcommand, so no global option of
  the caller's (`-C`, `-p`, `--paginate`, ...) reaches git before it.
- **Refused options, beyond the list above:** a long option is refused alone, in its `--opt=value` form
  and abbreviated (git's option parser takes a unique prefix of a long option, so `--outp=<file>` is
  `--output`), `--text` excepted, which is an option of its own; a short `-c` with its value attached;
  and, as defence in depth, git grep's `-O`/`--open-files-in-pager`, which runs a program of the
  caller's. For sort, any argument starting `--o` and any short-option cluster that holds `o`
  (`-uo<file>`).
- **The same environment for every command**, the five `GIT_*` included: PRD #175's criterion is one
  scrubbed environment, and a variable only git reads changes nothing for the others.
- **The exec seam.** The logic is `run(args []string, exec func(path string, argv []string, env []string)
  error, stderr io.Writer) int` in `package main` (`run.go`); `main` (`main.go`, `//go:build unix`)
  passes `syscall.Exec`, which has that shape and replaces the wrapper, so no wrapper process outlives
  the exec, and a test passes a fake. On a system without `syscall.Exec` (`main_other.go`, `!unix`, so
  the package builds and is linted for every release target) `main` refuses with exit 126. A refusal is
  exit 126 with one line, `audit-exec: refused: <word>`, the word cut to 64 bytes and quoted as a Go
  string (so a newline, a carriage return or a control character stays one line and no line starts a
  workflow command); an exec that fails is exit 127 with one such line.
- **Its rules:** the strict depguard rule `audit-exec` (`fmt`, `io`, `os`, `strings`, `syscall`; `os/exec`
  and `net` denied) and the forbidigo group `audit-exec-env`, which refuses every read of the environment,
  every process start, library load and raw system call, with one named exception: `syscall.Exec` in
  `ci/auditexec/main.go` alone. `ci/auditexec` has no library yet; `ci/internal/audit` joins it in the
  pull request that adds that library.

#### The sandbox preflight

- **Where:** its logic in `ci/internal/audit` (`Preflight`), run by `ci/auditrun` in the model step,
  before the audit, on every run: the token exists only in that step, and the probes need the model to
  make their tool calls. It uses the same launcher, settings file, wrapper, directories and environment
  as the audit; only its prompt differs, and its transcript goes to `/srv/audit/state/out/preflight.jsonl`.
- **One CLI run.** Its fixed prompt asks the model to make a listed set of tool calls, in order, and
  nothing else. The model judges nothing: every verdict is computed from the transcript's `tool_use`
  and `tool_result` elements and from effects the launcher observes itself (`Judge`, a pure function of
  the plan, the transcript and the effects).
- **The control.** Before the probes, `Preflight` writes a random nonce to
  `/srv/audit/work/preflight-control.txt`, and the first listed call reads it with Read. A transcript
  whose result for it lacks the nonce fails the step: without it, a run whose settings did not load, or
  whose model made no call, would pass for one that refused every probe.
- **The six probes**, each with its tool calls and its observed effect:

  | # | Tool calls | Effect `Preflight` observes itself |
  |---|---|---|
  | 1 | Write of `/srv/audit/state/probe-1`; Bash `/srv/audit/bin/audit-exec cat /srv/audit/work/preflight-control.txt > /srv/audit/state/probe-1` and the same into `/tmp/shift2eu-audit-probe-1` | neither file exists |
  | 2 | Bash `/srv/audit/bin/audit-exec cat < /dev/tcp/127.0.0.1/<port>`, where `<port>` is a TCP listener `Preflight` opened on 127.0.0.1 | the listener accepted no connection |
  | 3 | Bash `socat - UNIX-CONNECT:/var/run/docker.sock` and `/srv/audit/bin/audit-exec cat < /var/run/docker.sock` | `Preflight`'s own connect to `/var/run/docker.sock` fails (Docker is stopped) |
  | 4 | Bash `sudo -n true` and `/srv/audit/bin/audit-exec sudo -n true` | `Preflight`'s own `sudo -n true` fails (the sudo rule is gone) |
  | 5 | Read, Grep (pattern `SHIFT2EU_AUDIT_CANARY`) and Glob of `/proc/<auditrun pid>/environ`; Bash `/srv/audit/bin/audit-exec cat /proc/<auditrun pid>/environ`, `.../cat /proc/$PPID/environ` and `.../cat /proc/$$/environ` | the canary's value appears in no `tool_result` and nowhere else on stdout |
  | 6 | Write and Edit of the `$GITHUB_ENV` file's path; Read, Grep and Glob of it; Bash `/srv/audit/bin/audit-exec cat /srv/audit/work/preflight-control.txt >> <path>` | the file's sha256 is unchanged |

- **A probe fails (good)** when every one of its calls appears in the transcript with a `tool_result` that
  is an error or a refusal, and its effect is not observed. It **succeeds** (the step fails) when an
  effect is observed or a call's result is not an error. It is **not attempted** (the step fails) when a
  call is missing from the transcript. **"All six fail"** means the control passed and all six probes
  failed; anything else fails the step, whose summary names each probe that did not fail by its number
  and short name ("probe 2, TCP connect: not refused") and runbook step 10, and nothing of the
  transcript.
- **The fake:** `Preflight` takes two interfaces, a `Prober`, which runs the CLI with the preflight prompt
  and returns its transcript, and `Effects`, which observes the files, the listener, the Docker socket,
  `sudo` and the `$GITHUB_ENV` digest. The test's fake implements both and lets each probe in turn
  succeed, once through its effect and once through a `tool_result` that is not an error, and also
  drops each call in turn and the control's nonce; each case must fail the step. A test of
  `ci/auditrun` drives the whole launcher with a fake `claude` that records its environment: the
  variables an earlier step could set through `$GITHUB_ENV` (`ANTHROPIC_BASE_URL`, `HTTPS_PROXY`,
  `NODE_OPTIONS`, `LD_PRELOAD`) are not in it, and a managed-settings file under the test's root stops
  the launcher with exit 3 before the fake runs.

#### The runner's user-namespace restriction

GitHub's Ubuntu 24.04 runners set `kernel.apparmor_restrict_unprivileged_userns=1`, so an unprivileged
`bwrap` cannot set up its user namespace. A **model-free step** before the hardening step, while sudo
still works, loads an AppArmor profile for `/usr/bin/bwrap` alone that grants it `userns`
(`.github/security-audit/bwrap.apparmor`, `apparmor_parser -r`), rather than turning the restriction off
for every program with the sysctl. The profile is committed and pinned by `test/architecture`.
`bubblewrap` and `socat` come from the Ubuntu archive at a pinned version, each `.deb` checked against
its pinned sha256 before `dpkg -i`. `ci/auditrun`'s check 4 runs `bwrap --unshare-all --die-with-parent
--ro-bind / / /bin/true` itself; when it fails, the step fails with exit 4 and its summary says
"bubblewrap cannot start: see runbook step 10, the user-namespace note", whatever the cause. With
`sandbox.failIfUnavailable`, the CLI also refuses to run a command unsandboxed.

#### The install script and the pin

**One install script, one pin.** `.github/scripts/install-claude-code.sh` serves both workflows; it
keeps the verified tarball beside the binary (`<directory>/claude-code.tgz`) so `ci/auditrun` can check
it again in the model step. The version and the integrity move out of `ai-review.yml` into one committed
file, `.github/claude-code.yaml` (`version:` under its `# renovate: datasource=npm
depName=@anthropic-ai/claude-code-linux-x64` comment, and `integrity:`), which the install step of each
workflow reads; Renovate's version manager and `ci/claudecodepin` read that file instead of the
workflow, so the pin check covers every pin there is. *Why one pin:* the two workflows run the same CLI
on the same subscription, a bump is reviewed once, and two pins could drift apart without a check that
compares them; the audit's preflight checks each new version on its first run anyway. The pin's move
changes the pin check's behaviour, so it comes test-first with the audit's other pieces, and the
workflow's `Install the Claude Code CLI` step reads the file from the pull request that moves it.

*Settled by the implementation (issue #277):* the pin check reads the file with `pincheck.FromPinFile`
(top-level `version:` and `integrity:` lines, one of each, each of its exact shape) and takes its path as
`-pin`, `.github/claude-code.yaml` the default; `-workflow` is gone. In `ai-review.yml` a step `Read the
Claude Code pin`, before the install, reads the two lines with `grep -E` anchored to their exact shapes,
fails unless each appears once, and writes them to its step outputs; the install step gets them as `env:`
values (`CLAUDE_CODE_PIN_VERSION`, `CLAUDE_CODE_PIN_INTEGRITY`), never as a `${{ }}` in its `run:`, and its
hardening and permissions are unchanged. Renovate's generic manager names the pin file and the workflows
that hold an annotated pin one by one (`ci`, `release`, `security`, `vm-tools`), so no manager reads
`ai-review.yml` any more. The pin check's workflow watches the pin file in its push `paths:` instead of
`ai-review.yml`; its changes step still runs the check on a change to either. The install script copies
the verified tarball to `<directory>/claude-code.tgz` after extracting `package/claude`, so a refused
tarball is never kept.

The launcher's empty MCP configuration is passed by both callers from this pull request: the reviewer's
argument vector gains `--mcp-config {"mcpServers":{}}` after `--strict-mcp-config`, and
`ci/aireview/internal/review/claudecli_test.go` pins it. The launcher refuses, naming the field, before
anything runs: an `Effort` outside the five, a `PermissionMode` other than `dontAsk` or `plan`, a relative
`Settings` path, a `WorkDir` or `Home` that is not an absolute path to a directory that exists, and an
`Env` entry that is no `NAME=VALUE` or names a variable the launcher sets itself. A config's `Home` also
stands in for the fresh HOME in the environment, and a stream whose last result line is over 4 MiB, or a
line after it, fails the run as "printed more than" the cap; a transcript that cannot be written fails
an otherwise successful run.

#### Server-managed settings

Server-managed settings reach a CLI only on a Claude Team or Enterprise plan. The maintainer states the
plan the token belongs to before the audit workflow merges; until then this record says: *to be
confirmed by the maintainer*. If it is a Team or Enterprise plan, the audit workflow does not merge
until this record names how `ci/auditrun` detects and refuses server-managed settings, or the audit
runs under a personal plan's token instead.

#### Residual risks of the launch

- **The CLI process itself is not sandboxed**, only the commands it runs. Its connections are held to
  the model API by configuration (`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, no `WebFetch` or
  `WebSearch`), not enforced (Decision 6). A flaw in the CLI or a sandbox escape could send the token
  elsewhere; runbook step 10 rotates it.
- **A settings file the CLI cannot validate is ignored silently in `-p` mode.** The preflight's control
  and probes 1 and 2 then fail the step, so a bad file stops the run rather than running it
  unsandboxed.
- **Bash rules are text patterns** and easy to step around; they are defence in depth. The containment
  is the sandbox, the wrapper and the read deny list, which the probes test on every run.

## Alternatives considered

- **A second Lima VM with a fine-grained personal token, a launcher script and a `dot-agent-deck`
  schedule** (the issue's first plan). Dropped by the maintainer on 2026-11-18. A long-lived VM keeps
  state between runs, and a hostile input that compromised one run could persist into the next; a fresh
  hosted runner per job starts clean. A personal token acts as the maintainer, with the maintainer's
  reach; an App token minted in the publish job alone has four write permissions and a fixed script.
  The VM's schedule depends on the Mac and the VM being up; GitHub's does not. And the AI reviewer had
  already built the pattern needed: a `main`-only environment, an App token minted in the workflow, and
  the Claude Code CLI on the subscription. Self-hosted runners were out for the same reason as the VM.
- **One job for checks, model and publishing.** Simpler, but the model would share a runner with release
  code and untrusted tool output, and with the App's token. The split keeps the subscription token away
  from untrusted code and the write token away from the model.
- **Job outputs instead of encrypted artifacts.** Plaintext near a public log; above.
- **The run opens `security` issues with its pull request.** Classification is automated and issues are
  public at once, while the report waits for review; the next run opens them from merged reports instead
  (Decision 7).
- **A public count of private findings.** A count tied to a named release signals an undisclosed serious
  flaw before its fix exists; the flag says less (Decision 2).
- **A third-party egress-filtering action.** It would be a privileged action in the job that holds the
  subscription token; the model step is contained instead (Decision 6, detailed by milestone 2).
- **An index line per report**, hand-written or generated. Above, The report index.

## Consequences

- The project gets a dated public record of its security posture, and users get `SECURITY.md`, with the
  supported versions, private reporting and a link to [the reports](../security/README.md).
- Each run is a docs-only pull request the maintainer approves, even when nothing is new (Decision 4),
  and its draft advisories to read. Two report pull requests never conflict.
- The audit draws on the subscription the agents and the AI reviewer use.
- The residual risks above are accepted: a mislabelled finding is public until the maintainer sees it,
  the private-findings flag says that something exists, an earlier merged report's false positives
  shape later triage, bounded by the severity and category floor (the fingerprint match stops only
  accidental drops, since the model supplies its inputs), and the model step's network
  is held to the model API by configuration only.
- The maintainer must create the App, the environment and its keys, the rulesets and the `security`
  label, and check that no other App holds Actions write, before milestone 2 merges (the PRD's runbook,
  which milestone 2 moves into `docs/dev-environment.md`).
- Milestone 2 amends this record; milestone 3 enables the schedule after the first report is merged.
